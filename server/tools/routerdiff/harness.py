"""Independent public-process pair and typed HTTP observations.

Only the seed subprocess imports the public Python repository fixture API. Every
product operation below crosses an actual TCP HTTP listener.
"""
import base64
import json
import os
from pathlib import Path
import select
import shutil
import subprocess
import sys
import tempfile
import time

import jwt

from inventory import ROOT
from seed import ADMIN, PASSWORD, JWT_SECRET
from store_heads import StoreHeads
from policy_revisions import PolicyRevisions

HERE = Path(__file__).resolve().parent


def decode_response(response):
    content = base64.b64decode(response['body'])
    try:
        document = json.loads(content)
        kind = 'json'
    except (ValueError, UnicodeError):
        document, kind = content.decode(errors='replace'), 'text'
    return {**response, 'content': document, 'kind': kind}



class RemoteError(RuntimeError):
    """A namespace RPC failure, carrying transport context without parsing prose."""
    def __init__(self, side, operation, error, trace, error_class=None):
        super().__init__(side + ' RPC: ' + error + '\n' + trace)
        self.side, self.operation, self.remote_error = side, operation, error
        self.error_class = error_class


def observed_startup_exit(side, error, stage):
    """Return a real Go child exit; keep RPC and readiness faults distinct."""
    if not isinstance(error, RemoteError) or error.side != 'go' or error.operation != 'start':
        return None
    status = side.rpc('process_status')
    if status['exit_code'] is None:
        return None
    return {'exit_code': status['exit_code'], 'stage': stage}


def observed_request_failure(side, error, stage):
    """Retain real loopback HTTP client failures without inventing a response.

    Only concrete connection/read/protocol failures from request/join qualify.
    RPC, fixture validation and namespace errors stay fixture errors; exception
    prose is never parsed to guess a class or a server exit.
    """
    transport = {'ConnectionRefusedError', 'ConnectionResetError', 'ConnectionAbortedError', 'BrokenPipeError',
                 'RemoteDisconnected', 'TimeoutError', 'BadStatusLine', 'IncompleteRead', 'LineTooLong'}
    if (not isinstance(error, RemoteError) or error.side != 'go' or error.operation not in ('request', 'join')
            or error.error_class not in transport):
        return None
    status = side.rpc('process_status')
    return {'stage': stage, 'operation': error.operation, 'error_class': error.error_class,
            'exit_code': status['exit_code'], 'running': status['running']}

class Side:
    def __init__(self, name, directory, binary, options):
        self.name, self.directory = name, Path(directory)
        self.options = options
        (self.directory / 'options.json').write_text(json.dumps(options))
        self.stderr = (self.directory / 'worker.log').open('wb')
        self.process = subprocess.Popen(['bash', str(HERE / 'namespace.sh'), str(self.directory),
            sys.executable, str(HERE / 'worker.py'), name, str(binary)], cwd=ROOT,
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.stderr, text=True, bufsize=1)
        self.token = None
        self.observations = {}
        self.request_windows = {}
        self.pending = {}
        self.missing_effects = []
        self.failed_prerequisites = []

    def rpc(self, operation, **values):
        self.process.stdin.write(json.dumps({'operation': operation, **values}) + '\n')
        self.process.stdin.flush()
        if not select.select([self.process.stdout], [], [], 100)[0]:
            raise TimeoutError(self.name + ' namespace RPC timed out')
        line = self.process.stdout.readline()
        if not line:
            raise RuntimeError(self.name + ' namespace exited; inspect ' + str(self.directory / 'worker.log'))
        reply = json.loads(line)
        if not reply['ok']:
            raise RemoteError(self.name, operation, reply['error'], reply.get('traceback', ''), reply.get('error_class'))
        return reply['result']

    def request(self, method, path, document=None, raw=None, headers=None, auth=True, **kwargs):
        headers = dict(headers or {})
        if document is not None:
            if raw is not None:
                raise ValueError('choose document or raw body')
            raw = json.dumps(document, ensure_ascii=False, separators=(',', ':')).encode()
            headers.setdefault('Content-Type', 'application/json')
        if auth and self.token:
            headers.setdefault('Authorization', 'Bearer ' + self.token)
        values = dict(method=method, path=path, headers=headers,
                      body=base64.b64encode(raw).decode() if raw is not None else None, **kwargs)
        return decode_response(self.rpc('request', **values))

    def start(self):
        self.rpc('start')
        status = self.request('GET', '/api/auth/status', auth=False)
        if status['content'].get('authEnabled') is False:
            self.token = None
            return
        started = time.time()
        response = self.request('POST', '/api/auth/login', document={'username': ADMIN, 'password': PASSWORD}, auth=False)
        if response['status'] != 200 or response['content'].get('success') is not True:
            raise RuntimeError(self.name + ' public login failed')
        self.token = response['content']['token']
        if not isinstance(self.token, str) or not 0 < len(self.token) <= 8192 or self.token.count('.') != 2:
            raise RuntimeError(self.name + ' returned an invalid JWT shape')
        secret = self.options.get('environment', {}).get('JWT_SECRET_KEY', JWT_SECRET)
        claims = jwt.decode(self.token, secret, algorithms=['HS256']) if secret else jwt.decode(
            self.token, options={'verify_signature': False}, algorithms=['HS256'])
        for key in ('iat', 'exp'):
            if type(claims.get(key)) not in (int, float):
                raise RuntimeError(self.name + ' returned an invalid JWT numeric date')
        if not started - 1 <= claims['iat'] <= time.time() + 1 or abs(claims['exp'] - claims['iat'] - response['content']['expiresIn']) > 1:
            raise RuntimeError(self.name + ' JWT dates do not match its lifetime')
        if claims.get('username', claims.get('sub')) != ADMIN:
            raise RuntimeError(self.name + ' JWT does not identify the synthetic admin')
        if self.request('GET', '/api/auth/verify')['status'] != 200:
            raise RuntimeError(self.name + ' rejected its own fresh admin token')

    def close(self):
        if self.process.poll() is None:
            self.process.stdin.close()
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.terminate()
                self.process.wait(timeout=5)
        self.process.stdout.close()
        self.stderr.close()


class Pair:
    def __init__(self, binary, options=None, keep=None):
        self.root = Path(keep) if keep else Path(tempfile.mkdtemp(prefix='routerdiff-pair-'))
        self.root.mkdir(parents=True, exist_ok=True)
        self.keep = bool(keep)
        self.sides = {}
        self.go_startup_failure = None
        self.store_heads = StoreHeads()
        self.policy_revisions = PolicyRevisions()
        options = options or {}
        seed_dir = self.root / 'seed'
        option_path = self.root / 'options.json'
        option_path.write_text(json.dumps(options))
        try:
            subprocess.run([sys.executable, str(HERE / 'seed.py'), str(seed_dir), '--options', str(option_path)],
                           cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
            for name in ('python', 'go'):
                directory = self.root / name
                directory.mkdir()
                shutil.copytree(seed_dir, directory / 'data')
                side = Side(name, directory, binary, options)
                self.sides[name] = side
                try:
                    side.start()
                except RemoteError as error:
                    if name != 'go':
                        raise
                    failure = observed_startup_exit(side, error, 'public native process startup')
                    # Live/slow listeners and namespace faults remain fixture
                    # problems, never manufactured product findings.
                    if failure is None:
                        raise
                    self.go_startup_failure = failure
        except BaseException:
            self.close()
            raise

    def close(self):
        for side in self.sides.values():
            side.close()
        if not self.keep:
            shutil.rmtree(self.root)

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    @property
    def python(self):
        return self.sides['python']

    @property
    def go(self):
        return self.sides['go']
