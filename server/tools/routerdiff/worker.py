"""Namespace-local stdio RPC; public servers and fake HTTP peers only.

No product module is imported. A worker owns one DATA_DIR and one device world.
stdout is reserved for RPC; public server logs remain in the owned run directory.
"""
import base64
from concurrent.futures import ThreadPoolExecutor
import http.client
import json
import os
import socket
from pathlib import Path
import signal
import sqlite3
import subprocess
import sys
import time
import threading
import traceback

from fakes import World
from filesystem import capture
from osfaults import Faults
from inventory import ROOT
from seed import ADMIN, PASSWORD, JWT_SECRET

ACTIVE = Path('/tmp/routerdiff-active')


def request(method, path, headers=None, body=None, timeout=75, port=5000, sent=None):
    if port not in (5000, 18081, 18082, 18083, 18084, 18085, 18086):
        raise ValueError('RPC HTTP target must be the local API or an owned fake')
    connection = http.client.HTTPConnection('127.0.0.1', port, timeout=timeout)
    try:
        connection.request(method, path, body=base64.b64decode(body) if body is not None else None,
                           headers=headers or {})
        if sent is not None:
            sent.set()
        response = connection.getresponse()
        content = response.read()
        return {'status': response.status, 'headers': response.getheaders(),
                'body': base64.b64encode(content).decode()}
    finally:
        connection.close()


class Worker:
    def __init__(self, side, binary, options):
        if os.environ.get('ROUTERDIFF_ISOLATED') != '1':
            raise RuntimeError('worker requires its own native network/mount/PID namespace')
        self.side, self.binary, self.options = side, binary, options
        self.world = World(tls_directory=ACTIVE / 'fake-tls' if options.get('fake_tls') else None,
                           ipv6=options.get('fake_ipv6', False))
        self.pool = ThreadPoolExecutor(max_workers=16)
        self.pending = {}
        self.open_clients = {}
        self.process = None
        self.log = None
        self.faults = Faults(ACTIVE / "data")

    def start(self):
        if self.process is not None:
            raise RuntimeError('server already started')
        environment = {key: value for key, value in os.environ.items()
                       if key.lower() not in ('http_proxy', 'https_proxy', 'all_proxy', 'no_proxy')}
        environment.update(DATA_DIR=str(ACTIVE / 'data'), LOG_FILE=str(ACTIVE / 'public.log'),
            LOG_LEVEL='INFO', ADMIN_USERNAME=ADMIN, ADMIN_PASSWORD=PASSWORD, JWT_SECRET_KEY=JWT_SECRET,
            PYTHONDONTWRITEBYTECODE='1', CONFIGFLOW_BACKEND_HOST='127.0.0.1', CONFIGFLOW_BACKEND_PORT='5000',
            CONFIGFLOW_FRONTEND_DIR='', SUB_STORE_URL='http://127.0.0.1:9',
            SUB_STORE_BUILTIN_DIR=str(ACTIVE / 'no-sub-store'), TZ='UTC',
            ROUTERDIFF_MANIFEST=str(ACTIVE / 'manifest.tsv'))
        for key, value in self.options.get('environment', {}).items():
            if value is None:
                environment.pop(key, None)
            else:
                environment[key] = value
        self.log = (ACTIVE / 'console.log').open('ab')
        command = [sys.executable, '-m', 'backend.app'] if self.side == 'python' else [self.binary]
        self.process = subprocess.Popen(command, cwd=ROOT, env=environment,
            stdin=subprocess.DEVNULL, stdout=self.log, stderr=self.log, start_new_session=True)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if self.process.poll() is not None:
                raise RuntimeError(f'{self.side} exited {self.process.returncode}; inspect owned console.log')
            try:
                response = request('GET', '/api/auth/status', timeout=1)
                if response['status'] == 200:
                    return {'ready': True, 'side': self.side, 'pid': self.process.pid}
            except OSError:
                pass
            time.sleep(.05)
        raise TimeoutError(f'{self.side} public listener not ready')

    def stop(self, kill=False):
        if self.process is None:
            return None
        if self.process.poll() is None:
            os.killpg(self.process.pid, signal.SIGKILL if kill else signal.SIGTERM)
        try:
            self.process.wait(timeout=12)
        except subprocess.TimeoutExpired:
            os.killpg(self.process.pid, signal.SIGKILL)
            self.process.wait(timeout=3)
        result = self.process.returncode
        self.process = None
        self.log.close()
        return result

    def snapshot(self):
        # Logical storage, not SQLite pages, WAL layout or Python object helpers.
        database = ACTIVE / 'data/configflow.sqlite3'
        with sqlite3.connect(f'file:{database}?mode=ro', uri=True) as db:
            db.execute('BEGIN')
            tables = {}
            for name in ('meta', 'sections', 'fields'):
                columns = [row[1] for row in db.execute(f'PRAGMA table_info({name})')]
                rows = list(db.execute(f'SELECT * FROM {name}'))
                tables[name] = {'columns': columns, 'rows': sorted(rows, key=repr)}
        files, metadata = capture(ACTIVE / 'data')
        return {'database': tables, 'files': files, 'filesystem': metadata, 'fake': self.world.snapshot()}

    def dispatch(self, operation, **values):
        if operation == 'start':
            return self.start()
        if operation == 'stop':
            return self.stop(**values)
        if operation == 'request':
            return request(**values)
        if operation == 'open_request':
            ident = values.pop('ident')
            if ident in self.open_clients:
                raise ValueError('duplicate open public HTTP client')
            if not values['path'].startswith('/api/'):
                raise ValueError('open client is restricted to the owned public API')
            connection = http.client.HTTPConnection('127.0.0.1', 5000, timeout=75)
            try:
                connection.request(values['method'], values['path'], headers=values.get('headers', {}),
                    body=base64.b64decode(values['body']) if values.get('body') is not None else None)
            except Exception:
                connection.close()
                raise
            self.open_clients[ident] = connection
            return {'sent': ident}
        if operation == 'abandon':
            connection = self.open_clients.pop(values['ident'])
            try:
                if connection.sock is not None:
                    connection.sock.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass  # The server may already have closed its end.
            connection.close()
            return {'closed': values['ident']}
        if operation == 'async':
            ident = values.pop('ident')
            if ident in self.pending:
                raise ValueError('duplicate async request')
            sent = threading.Event()
            self.pending[ident] = self.pool.submit(request, **values, sent=sent)
            if not sent.wait(timeout=5):
                raise TimeoutError('async client did not send request headers within fixture window')
            return {'started': ident, 'headers_sent': True}
        if operation == 'pending_status':
            # Observe actual client futures after both sets of headers have
            # crossed TCP. No application state or clock is patched.
            time.sleep(.25)
            return {ident: future.done() for ident, future in self.pending.items()}
        if operation == 'join':
            return self.pending.pop(values['ident']).result(timeout=values.get('timeout', 80))
        if operation == 'snapshot':
            return self.snapshot()
        if operation == 'filesystem_fault':
            return self.faults.control(**values)
        if operation == 'fake':
            return self.world.control(**values)
        if operation == 'process_status':
            return {'exit_code': self.process.poll() if self.process is not None else None}
        if operation == 'manifest':
            return (ACTIVE / 'manifest.tsv').read_text() if self.side == 'go' else ''
        if operation == 'log_position':
            return (ACTIVE / 'console.log').stat().st_size
        if operation == 'logs':
            with (ACTIVE / 'console.log').open('rb') as handle:
                handle.seek(values.get('start', 0))
                return handle.read().decode(errors='replace')[-values.get('limit', 4000):]
        raise ValueError('unknown RPC operation')

    def close(self):
        self.world.release_all()
        self.faults.close()
        for connection in self.open_clients.values():
            connection.close()
        self.stop()
        self.pool.shutdown(wait=True, cancel_futures=True)
        self.world.close()


def main():
    options = json.loads((ACTIVE / 'options.json').read_text())
    worker = Worker(sys.argv[1], sys.argv[2], options)
    try:
        for line in sys.stdin:
            try:
                result = worker.dispatch(**json.loads(line))
                reply = {'ok': True, 'result': result}
            except Exception as error:
                reply = {'ok': False, 'error': type(error).__name__ + ': ' + str(error),
                         'traceback': traceback.format_exc()}
            print(json.dumps(reply, ensure_ascii=False), flush=True)
    finally:
        worker.close()


if __name__ == '__main__':
    main()
