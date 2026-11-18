"""Public-process Flask/Go differential harness with synthetic Python seeds."""
from contextlib import contextmanager
from dataclasses import dataclass, asdict
import json
import hashlib
import math
import os
from pathlib import Path
import re
import shutil
import signal
import socket
import sqlite3
import subprocess
import sys
import tempfile
import time
import threading
import urllib.error
import urllib.request

from compare import Policy, Difference, response_diffs, database_diffs, file_diffs, database, files as stored_files
from inventory import declared_states

ROOT = Path(__file__).resolve().parents[3]
USERNAME, PASSWORD, SECRET = 'diff-admin', 'diff-password', 'synthetic-shared-jwt-secret-0123456789'
PYTHON = os.environ.get('CONFIGFLOW_DIFF_PYTHON', str(Path.home() / 'cf-test/.venv/bin/python'))
_port_guard, _used_ports = threading.Lock(), set()


@dataclass
class Reply:
    status: int
    headers: dict
    body: bytes

    def json(self):
        return json.loads(self.body)


def free_port():
    with _port_guard:
        for _ in range(128):
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', 0))
                port = sock.getsockname()[1]
            if port not in _used_ports:
                _used_ports.add(port)
                return port
    raise DesignError('could not allocate a distinct loopback port; active reservations=' + str(len(_used_ports)))


def release_port(port):
    """Release a stopped fixture's reservation so the kernel may reuse it."""
    with _port_guard:
        _used_ports.discard(port)


def send(base, method, path, body=None, headers=None, timeout=30):
    data = body.encode('utf-8') if isinstance(body, str) else body
    request = urllib.request.Request(base + path, data=data, method=method,
                                     headers=headers or {})
    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *_args, **_kwargs):
            return None
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        response = opener.open(request, timeout=timeout)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return Reply(response.code, {k.lower(): v for k, v in response.headers.items()}, response.read())


def json_body(value):
    return json.dumps(value, ensure_ascii=False, separators=(',', ':'))


class DesignError(AssertionError):
    """Reference precondition failed; never misreport a broken fixture as parity."""


class Recorder:
    def __init__(self):
        self.results = []
        self.guard = threading.Lock()

    def record(self, case_id, variant, route, category, state, diffs=(), **extra):
        def evidence(value):
            if isinstance(value, float) and not math.isfinite(value):
                return {'nonfinite_number': 'NaN' if math.isnan(value) else 'Infinity' if value > 0 else '-Infinity'}
            if isinstance(value, str) and len(value) > 100_000:
                raw = value.encode('utf-8', errors='surrogatepass')
                return {'string_characters': len(value), 'utf8_surrogatepass_bytes': len(raw),
                        'utf8_surrogatepass_sha256': hashlib.sha256(raw).hexdigest(),
                        'prefix_utf8_hex': raw[:256].hex(), 'suffix_utf8_hex': raw[-256:].hex()}
            if isinstance(value, str) and any(0xD800 <= ord(c) <= 0xDFFF for c in value):
                return {'surrogate_string_codepoints': [ord(c) for c in value]}
            if isinstance(value, bytes):
                return {'bytes_length': len(value), 'bytes_sha256': hashlib.sha256(value).hexdigest(),
                        'bytes_prefix_hex': value[:256].hex()}
            if isinstance(value, dict):
                return {k: evidence(v) for k, v in value.items()}
            if isinstance(value, (list, tuple)):
                return [evidence(v) for v in value]
            return value
        result = {'case': case_id, 'variant': variant, 'route': route,
                             'category': category, 'state': state,
                             'differences': [evidence(asdict(d)) for d in diffs], **extra}
        with self.guard:
            self.results.append(result)
            print(json.dumps({'case': case_id, 'variant': variant, 'state': state,
                              'diffs': len(diffs)}, ensure_ascii=False), flush=True)


def verify_backup_fixture(backup, allow_backup_fixture=False):
    from urllib.parse import urlsplit
    if backup.get('auto_backup') is False:
        return
    endpoint = urlsplit(backup.get('webdav_url', ''))
    if not allow_backup_fixture or backup.get('auto_backup') is not True or endpoint.scheme != 'http' or endpoint.hostname != '127.0.0.1' or not endpoint.port:
        raise DesignError('default fixture must disable auto_backup; explicit backup fixture requires an allocated loopback DAV endpoint')


class Pair:
    def __init__(self, binary, recorder, *, mode='login', options=None, tz='UTC', allow_backup_fixture=False, seed_from=None):
        self.seed_from = seed_from
        self.allow_backup_fixture = allow_backup_fixture
        self.binary, self.recorder, self.mode, self.tz = str(binary), recorder, mode, tz
        self.options = dict(options or {})
        self.admin_username = self.options.get('admin_username', USERNAME) if mode == 'login' else ''
        self.admin_password = self.options.get('admin_password', PASSWORD) if mode == 'login' else ''
        self.auth_enabled = bool(self.admin_username and self.admin_password)
        self.options.setdefault('config_token', 'synthetic-public-pull-token' if mode == 'token' else '')
        self.temp = tempfile.TemporaryDirectory(prefix='configdiff-', dir=self.options.get('temporary_root'))
        self.root = Path(self.temp.name)
        self.dirs = [self.root / name for name in ('python', 'go')]
        self.processes, self.handles, self.bases, self.tokens = [], [], [], []
        self.port_leases = []
        self.policy = Policy(start=time.time(), tz=self.tz)
        self.registered = set()
        self.declared = declared_states()
        self.unimplemented_mutations = set()

    def environment(self, directory, port):
        env = dict(os.environ)
        for key in ('HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY', 'http_proxy', 'https_proxy', 'all_proxy',
                    'CONFIGFLOW_FRONTEND_DIR', 'CONFIGFLOW_WEB_PORT'):
            env.pop(key, None)
        env.update(DATA_DIR=str(directory), LOG_FILE=str(directory / 'app.log'),
                   TZ=self.tz, CONFIGFLOW_BACKEND_HOST='127.0.0.1', CONFIGFLOW_BACKEND_PORT=str(port),
                   CONFIGFLOW_FRONTEND_DIR='', JWT_SECRET_KEY=SECRET, LOG_LEVEL='INFO',
                   ADMIN_USERNAME=self.admin_username, ADMIN_PASSWORD=self.admin_password,
                   SUB_STORE_BUILTIN_DIR=str(self.root / 'missing-sub-store'),
                   SUB_STORE_URL='http://127.0.0.1:9', CONFIGFLOW_NODE_PROBE_BIN=str(self.root / 'missing-probe'),
                   PRODDIFF_ROUTES=str(self.root / 'go-routes.tsv'), PYTHONDONTWRITEBYTECODE='1',
                   PYTHONPATH=str(ROOT))
        env.update(CONFIGFLOW_NODE_TEST_URL='http://probe-target.example/generate_204')
        if self.options.get('probe_case'):
            env.update(CONFIGFLOW_NODE_PROBE_BIN=str(self.root / 'synthetic-probe'),
                       CONFIGDIFF_PROBE_INPUT=str(self.root / 'probe-input.jsonl'),
                       CONFIGDIFF_PROBE_CASE=self.options['probe_case'])
        env.update(self.options.get('environment', {}))
        if (self.options.get('web_frontend') and self.options.get('web_port_environment')
                and directory in self.dirs):
            env['CONFIGFLOW_WEB_PORT'] = str(self.options['web_listener_ports'][self.dirs.index(directory)])
        if self.options.get('web_frontend') and directory == self.dirs[1]:
            env['CONFIGFLOW_FRONTEND_DIR'] = str(self.root / 'frontend')
        if 'secret_key' in self.options:
            if self.options['secret_key'] is None:
                env.pop('JWT_SECRET_KEY', None)
            else:
                env['JWT_SECRET_KEY'] = self.options['secret_key']
        return env

    def __enter__(self):
        if self.options.get('probe_case'):
            from probe import install
            install(self.root, PYTHON, self.options['probe_case'])
        seed = self.root / 'seed'
        if self.seed_from is not None:
            # A second execution of the same scenario must start with the
            # identical Python-imported immutable seed, including store IDs.
            # The source seed has no running process or SQLite WAL writer.
            shutil.copytree(self.seed_from, seed)
        else:
            seed.mkdir()
        options_path = self.root / 'seed-options.json'
        # Large history/capacity fixtures exceed Linux's single-argument limit.
        # JSON on disk also preserves escaped Unicode without OS argv encoding.
        options_path.write_text(json.dumps(self.options, ensure_ascii=True), encoding='utf-8')
        result = subprocess.run([PYTHON, str(Path(__file__).with_name('seed.py')), str(seed),
                                 '--options-file', str(options_path)], cwd=ROOT,
                                env=self.environment(seed, 0), capture_output=True, text=True) if self.seed_from is None else None
        if result is not None and result.returncode:
            self.temp.cleanup()
            raise DesignError('Python seed failed: ' + result.stderr[-5000:])
        for directory in self.dirs:
            shutil.copytree(seed, directory)
        if self.options.get('web_frontend'):
            frontend = self.root / 'frontend'
            frontend.mkdir()
            (frontend / 'index.html').write_text('synthetic ConfigFlow frontend\n')
            ports = self.options.get('web_listener_ports')
            if (not isinstance(ports, (list, tuple)) or len(ports) != 2 or ports[0] == ports[1]
                    or any(type(port) is not int or not 1 <= port <= 65535 for port in ports)):
                raise DesignError('complete web deployment requires two assigned listener ports')
            for directory, port in zip(self.dirs, ports):
                saved = directory / 'web-listener.json'
                saved.write_text(json.dumps({'port': port}) + '\n')
                saved.chmod(0o600)
        for ident, ports in self.options.get('agent_port_pairs', {}).items():
            if len(ports) != 2 or len(set(ports)) != 2 or any(type(p) is not int or not 1 <= p <= 65535 for p in ports):
                raise DesignError('fake Agent requires two distinct allocated ports')
            for backend, directory in enumerate(self.dirs):
                with sqlite3.connect(directory / 'configflow.sqlite3') as db:
                    row = db.execute("SELECT value FROM fields WHERE scope='system' AND owner='' AND field='agents'").fetchone()
                    agents = json.loads(row[0])
                    matches = [i for i, agent in enumerate(agents) if agent['id'] == ident]
                    if len(matches) != 1:
                        raise DesignError('fake Agent must match one seeded Agent')
                    position = matches[0]
                    agents[position]['port'] = ports[backend]
                    db.execute("UPDATE fields SET value=? WHERE scope='system' AND owner='' AND field='agents'", (json.dumps(agents),))
                self.policy.allocated_ports[f'db.fields.system//agents.value[{position}].port'] = tuple(ports)
        document = json.loads((seed / 'configdiff-seed.json').read_text())
        verify_backup_fixture(document['system']['backup'], self.allow_backup_fixture)
        for item in document['shared']['subscriptions'] + document['shared']['rule_library']:
            if item.get('enabled', True) and item.get('interval', item.get('update_interval', 0)):
                raise DesignError('X08 default fixture contains a refreshable source')
        self.default_profile = document['system']['system_config'].get('default_profile_id') or 'default'
        try:
            return self.start_servers()
        except BaseException:
            self.close()
            self.temp.cleanup()
            raise

    def start_servers(self):
        from build import manifest_for_server
        self.registered = manifest_for_server(self.binary)
        for index, directory in enumerate(self.dirs):
            port = free_port()
            self.port_leases.append(port)
            command = [PYTHON, '-m', 'backend.app'] if index == 0 else [self.binary]
            handle = open(self.root / f'{index}.log', 'wb')
            self.handles.append(handle)
            process = subprocess.Popen(command, env=self.environment(directory, port),
                                       cwd=self.root if self.options.get('isolated_cwd') else ROOT,
                                       stdout=handle, stderr=handle, start_new_session=True)
            self.processes.append(process)
            base = f'http://127.0.0.1:{port}'
            self.bases.append(base)
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    self.close()
                    raise DesignError(f'{index} server exited: ' + (self.root / f'{index}.log').read_text()[-5000:])
                try:
                    if send(base, 'GET', '/api/auth/status', timeout=1).status == 200:
                        break
                except (OSError, urllib.error.URLError):
                    pass
                time.sleep(.1)
            else:
                self.close()
                raise DesignError(f'{index} server did not become ready')
            if self.auth_enabled:
                reply = send(base, 'POST', '/api/auth/login', json_body({'username': self.admin_username, 'password': self.admin_password}),
                             {'Content-Type': 'application/json'})
                if reply.status != 200 or not reply.json().get('token'):
                    raise DesignError(f'{index} login failed: {reply.status}')
                self.tokens.append(reply.json()['token'])
            else:
                self.tokens.append('')
        return self

    def close(self, *, release_ports=True):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
        for process in self.processes:
            try:
                # The public Go command drains for up to eight seconds. Leave
                # its final flush/exit two seconds before the fixture kills it.
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        for handle in self.handles:
            handle.close()
        if release_ports:
            for port in self.port_leases:
                release_port(port)
            self.port_leases.clear()

    def restart(self):
        # A restarted fixture still references its old endpoint identities.
        # Keep those reserved until the whole fixture has finished.
        self.close(release_ports=False)
        self.processes, self.handles, self.bases, self.tokens = [], [], [], []
        return self.start_servers()

    def __exit__(self, exc_type, exc_value, trace):
        self.close()
        self.temp.cleanup()

    def route_available(self, route):
        method, path = route.split(' ', 1)
        # Flask and Go manifest spelling can differ in parameter syntax.
        def canonical(value):
            value = re.sub(r'<(?:[^:>]+:)?([^>]+)>', r'{\1}', value)
            return re.sub(r'\{([^{}]+)\.\.\.\}', r'{\1}', value)
        return any(method == m and canonical(path) == canonical(p) for m, p in self.registered)

    def request(self, case_id, route, path=None, *, method=None, body=None, auth='admin', headers=None,
                variant='', expect=None, check=None, writes=False, files=True, policy=None, capture=None,
                interaction=None, bodies=None, paths=None, projection=None, filename='', raw_body=False, runtime_writes=False, timeout=30,
                pending_dependency=''):
        nominal_method, nominal_path = route.split(' ', 1)
        method, path = method or nominal_method, path or nominal_path
        policy = policy or self.policy
        registered = self.route_available(route)
        available = registered and not self.unimplemented_mutations and not pending_dependency
        before_reference = (database(self.dirs[0]), stored_files(self.dirs[0])) if writes and not available else None
        replies = []
        for index, base in enumerate(self.bases):
            if index == 1 and not available:
                replies.append(Reply(0, {}, b''))
                continue
            local_headers = {'X-ConfigFlow-Profile': self.default_profile}
            local_headers.update(headers or {})
            local_headers = {k: v for k, v in local_headers.items() if v is not None}
            if body is not None or bodies is not None:
                local_headers.setdefault('Content-Type', 'application/json')
            credential = self.tokens[index] if auth == 'admin' else (auth[index] if isinstance(auth, (list, tuple)) else auth)
            if credential:
                local_headers['Authorization'] = credential if credential.startswith('Bearer ') else 'Bearer ' + credential
            if interaction:
                interaction.before(index)
            replies.append(send(base, method, paths[index] if paths is not None else path,
                                bodies[index] if bodies is not None else body, local_headers, timeout=timeout))
            if interaction:
                interaction.after(index, replies[-1])
        policy.end = time.time()
        reference = replies[0]
        if expect is not None and reference.status not in (expect if isinstance(expect, tuple) else (expect,)):
            raise DesignError(f'{case_id}/{variant}: Python status {reference.status}, intended {expect}; {reference.body[:1000]!r}')
        if check is not None:
            check(reference)
        category = re.search(r'-(S|F|B)\d+', case_id)
        category = category[1] if category else case_id.split('-')[0]
        if not available:
            if runtime_writes or (before_reference is not None and before_reference != (database(self.dirs[0]), stored_files(self.dirs[0]))):
                self.unimplemented_mutations.add(route)
            if registered:
                self.recorder.record(case_id, variant, route, category, 'go-unimplemented-dependency',
                                     python_status=reference.status, go_status=None,
                                     blocked_by=sorted(self.unimplemented_mutations | ({pending_dependency} if pending_dependency else set())))
            elif self.declared.get((nominal_method, nominal_path)) == 'done':
                self.recorder.record(case_id, variant, route, category, 'different',
                                     [Difference('route.registration', 'declared-done-but-unregistered', True, False)],
                                     python_status=reference.status, go_status=None)
            else:
                self.recorder.record(case_id, variant, route, category, 'go-unimplemented',
                                     python_status=reference.status, go_status=None)
            return replies
        captured = capture(replies, policy) if capture else []
        diffs = list(captured or [])
        if interaction:
            diffs += interaction.compare(policy)
        diffs += response_diffs(*replies, policy, method=method, projection=projection, filename=filename, raw_body=raw_body)
        if writes:
            diffs += database_diffs(*self.dirs, policy)
            if files:
                diffs += file_diffs(*self.dirs, policy)
        self.recorder.record(case_id, variant, route, category, 'different' if diffs else 'compared', diffs,
                             python_status=reference.status, go_status=replies[1].status)
        return replies
