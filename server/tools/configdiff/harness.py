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
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

from compare import Policy, Difference, response_diffs, database_diffs, file_diffs, database, files as stored_files
from inventory import declared_states

ROOT = Path(__file__).resolve().parents[3]
USERNAME, PASSWORD, SECRET = 'diff-admin', 'diff-password', 'synthetic-shared-jwt-secret'
PYTHON = os.environ.get('CONFIGFLOW_DIFF_PYTHON', str(Path.home() / 'cf-test/.venv/bin/python'))


@dataclass
class Reply:
    status: int
    headers: dict
    body: bytes

    def json(self):
        return json.loads(self.body)


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


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

    def record(self, case_id, variant, route, category, state, diffs=(), **extra):
        def evidence(value):
            if isinstance(value, float) and not math.isfinite(value):
                return {'nonfinite_number': 'NaN' if math.isnan(value) else 'Infinity' if value > 0 else '-Infinity'}
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
        self.results.append({'case': case_id, 'variant': variant, 'route': route,
                             'category': category, 'state': state,
                             'differences': [evidence(asdict(d)) for d in diffs], **extra})
        print(json.dumps({'case': case_id, 'variant': variant, 'state': state,
                          'diffs': len(diffs)}, ensure_ascii=False), flush=True)


class Pair:
    def __init__(self, binary, recorder, *, mode='login', options=None, tz='UTC'):
        self.binary, self.recorder, self.mode, self.tz = str(binary), recorder, mode, tz
        self.options = dict(options or {})
        self.admin_username = self.options.get('admin_username', USERNAME) if mode == 'login' else ''
        self.admin_password = self.options.get('admin_password', PASSWORD) if mode == 'login' else ''
        self.auth_enabled = bool(self.admin_username and self.admin_password)
        self.options.setdefault('config_token', 'synthetic-public-pull-token' if mode == 'token' else '')
        self.temp = tempfile.TemporaryDirectory(prefix='configdiff-')
        self.root = Path(self.temp.name)
        self.dirs = [self.root / name for name in ('python', 'go')]
        self.processes, self.handles, self.bases, self.tokens = [], [], [], []
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
                   CONFIGDIFF_ROUTES=str(self.root / 'go-routes.tsv'), PYTHONDONTWRITEBYTECODE='1',
                   PYTHONPATH=str(ROOT))
        env.update(CONFIGFLOW_NODE_TEST_URL='http://probe-target.example/generate_204')
        if self.options.get('probe_case'):
            env.update(CONFIGFLOW_NODE_PROBE_BIN=str(self.root / 'synthetic-probe'),
                       CONFIGDIFF_PROBE_INPUT=str(self.root / 'probe-input.jsonl'),
                       CONFIGDIFF_PROBE_CASE=self.options['probe_case'])
        env.update(self.options.get('environment', {}))
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
        seed.mkdir()
        result = subprocess.run([PYTHON, str(Path(__file__).with_name('seed.py')), str(seed),
                                 json_body(self.options)], cwd=ROOT,
                                env=self.environment(seed, 0), capture_output=True, text=True)
        if result.returncode:
            self.temp.cleanup()
            raise DesignError('Python seed failed: ' + result.stderr[-5000:])
        for directory in self.dirs:
            shutil.copytree(seed, directory)
        document = json.loads((seed / 'configdiff-seed.json').read_text())
        if document['system']['backup'].get('auto_backup') is not False:
            raise DesignError('default fixture must disable auto_backup')
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
        for index, directory in enumerate(self.dirs):
            port = free_port()
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
        for line in (self.root / 'go-routes.tsv').read_text().splitlines():
            method, path = line.split('\t')
            self.registered.add((method, path))
        return self

    def close(self):
        for process in self.processes:
            if process.poll() is None:
                os.killpg(process.pid, signal.SIGTERM)
        for process in self.processes:
            try:
                process.wait(timeout=8)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
        for handle in self.handles:
            handle.close()

    def restart(self):
        self.close()
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
                interaction=None, bodies=None, paths=None, projection=None, filename=''):
        nominal_method, nominal_path = route.split(' ', 1)
        method, path = method or nominal_method, path or nominal_path
        policy = policy or self.policy
        registered = self.route_available(route)
        available = registered and not self.unimplemented_mutations
        before_reference = (database(self.dirs[0]), stored_files(self.dirs[0])) if writes and not available else None
        replies = []
        for index, base in enumerate(self.bases):
            if index == 1 and not available:
                replies.append(Reply(0, {}, b''))
                continue
            local_headers = {'X-ConfigFlow-Profile': self.default_profile}
            local_headers.update(headers or {})
            local_headers = {k: v for k, v in local_headers.items() if v is not None}
            if body is not None:
                local_headers.setdefault('Content-Type', 'application/json')
            credential = self.tokens[index] if auth == 'admin' else (auth[index] if isinstance(auth, (list, tuple)) else auth)
            if credential:
                local_headers['Authorization'] = credential if credential.startswith('Bearer ') else 'Bearer ' + credential
            if interaction:
                interaction.before(index)
            replies.append(send(base, method, paths[index] if paths is not None else path,
                                bodies[index] if bodies is not None else body, local_headers))
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
            if before_reference is not None and before_reference != (database(self.dirs[0]), stored_files(self.dirs[0])):
                self.unimplemented_mutations.add(route)
            if registered:
                self.recorder.record(case_id, variant, route, category, 'go-unimplemented-dependency',
                                     python_status=reference.status, go_status=None,
                                     blocked_by=sorted(self.unimplemented_mutations))
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
        diffs += response_diffs(*replies, policy, method=method, projection=projection, filename=filename)
        if writes:
            diffs += database_diffs(*self.dirs, policy)
            if files:
                diffs += file_diffs(*self.dirs, policy)
        self.recorder.record(case_id, variant, route, category, 'different' if diffs else 'compared', diffs,
                             python_status=reference.status, go_status=replies[1].status)
        return replies
