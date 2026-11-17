"""Synthetic supervisor executable for managed dependency public API tests."""
from contextlib import contextmanager
import json
import os

from dependency_tls import require_native_network
from harness import Pair, PYTHON
from sources import Source


def events(pair):
    path = pair.root / 'supervisor-events.jsonl'
    return [json.loads(line) for line in path.read_text().splitlines()] if path.exists() else []


@contextmanager
def managed(binary, recorder, *, fail=False, health_status=None, controller_status='RUNNING', unreadable_version=False, github=None, allow_restart=False, restart_failure='', readiness='ready', stdin_node_check=False, controller_missing=False):
    require_native_network()
    with Source(json.dumps({'data': {'version': '2.40.0'}}).encode()) as health:
        health.headers = {'Content-Type': 'application/json'}
        health.record_body = True
        def control_version(path):
            if path != '/_configdiff/version':
                return
            row = health.requests[-1]
            try:
                value = json.loads(bytes.fromhex(row['body_hex']))
            except (ValueError, UnicodeError):
                value = None
            if (not allow_restart or row['method'] != 'POST'
                    or row['authorization'] != 'Bearer synthetic-controller-capability'
                    or value not in ({'version': '2.40.0'}, {'version': '2.41.0'}, {'version': '2.42.1'})):
                health.responses[path] = (403, b'controller fixture rejected', {})
                return
            health.body = json.dumps({'data': {'version': '2.40.0'} if readiness == 'old-version' else value}).encode()
            if readiness == 'offline':
                health.status = 503
            health.responses[path] = (200, b'{"ok":true}', {'Content-Type': 'application/json'})
        health.before_reply = control_version
        health.status = health_status if health_status is not None else 503 if fail else 200
        pair = Pair(binary, recorder)
        control = pair.root / 'control-runtime'
        control.mkdir()
        builtin = pair.root / 'builtin'
        builtin.mkdir()
        (builtin / 'sub-store.bundle.js').write_text('// synthetic builtin; never executed\n')
        if unreadable_version:
            (builtin / 'VERSION').mkdir()
        else:
            (builtin / 'VERSION').write_text('2.40.0\n')
        tools = pair.root / 'tools'
        tools.mkdir()
        executable = tools / 'supervisorctl'
        executable.write_text('#!' + PYTHON + '\n' + '''import json, os, sys
from pathlib import Path
args = sys.argv[1:]
event = {'args': args}
fd = os.open(os.environ['CONFIGDIFF_SUPERVISOR_LOG'], os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
try:
    os.write(fd, (json.dumps(event) + '\\n').encode())
finally:
    os.close(fd)
prefix = ['-c', '/etc/supervisor/conf.d/supervisord.conf']
if args == prefix + ['restart', 'sub-store'] and os.environ.get('CONFIGDIFF_ALLOW_RESTART') == '1':
    import hashlib
    marker = Path(os.environ['TMPDIR']) / ('restart-' + hashlib.sha256(os.environ['DATA_DIR'].encode()).hexdigest())
    failure = os.environ.get('CONFIGDIFF_RESTART_FAILURE', '')
    if failure and not marker.exists():
        marker.write_text('first restart attempted')
        if failure == 'stdout':
            print('synthetic-controller-rejected')
        elif failure == 'stderr':
            print('synthetic-controller-rejected', file=sys.stderr)
        sys.exit(1)
    from urllib.request import Request, build_opener, ProxyHandler
    version_file = Path(os.environ['DATA_DIR']) / 'sub-store/runtime/VERSION'
    if not version_file.is_file():
        version_file = Path(os.environ['SUB_STORE_BUILTIN_DIR']) / 'VERSION'
    version = version_file.read_text().strip()
    request = Request(os.environ['CONFIGDIFF_CONTROLLER_URL'],
        json.dumps({'version': version}).encode(),
        {'Authorization': 'Bearer synthetic-controller-capability', 'Content-Type': 'application/json'})
    with build_opener(ProxyHandler({})).open(request, timeout=2) as response:
        if response.status != 200:
            sys.exit(96)
    print('synthetic sub-store restarted')
    sys.exit(0)
if args != prefix + ['status', 'sub-store']:
    print('synthetic fake refuses every mutating or unknown command', file=sys.stderr)
    sys.exit(97)
if os.environ.get('CONFIGDIFF_SUPERVISOR_FAIL') == '1':
    print('synthetic-controller-rejected', file=sys.stderr)
    sys.exit(1)
print('sub-store ' + os.environ['CONFIGDIFF_SUPERVISOR_STATE'] + ' pid 1, uptime 0:00:01')
''')
        executable.chmod(0o755)
        if stdin_node_check:
            from dependency_node import install
            install(pair)
        pair.options['environment'] = {'SUB_STORE_BUILTIN_DIR': str(builtin), 'SUB_STORE_URL': health.url,
            'TMPDIR': str(control),
            'SUB_STORE_BACKEND_API_PORT': str(health.server.server_port),
            'PATH': str(tools) + os.pathsep + os.environ.get('PATH', ''),
            'CONFIGDIFF_SUPERVISOR_LOG': str(pair.root / 'supervisor-events.jsonl'),
            'CONFIGDIFF_SUPERVISOR_STATE': controller_status,
            'CONFIGDIFF_ALLOW_RESTART': '1' if allow_restart else '0',
            'CONFIGDIFF_RESTART_FAILURE': restart_failure,
            'CONFIGDIFF_CONTROLLER_URL': health.url + '/_configdiff/version',
            'CONFIGDIFF_NODE_LOG': str(pair.root / 'node-check-events.jsonl'),
            'CONFIGDIFF_SUPERVISOR_FAIL': '1' if fail else '0'}
        if controller_missing:
            # A real exec failure, with no possibility of PATH falling through
            # to the test host's actual supervisor controller.
            executable.unlink()
            pair.options['environment']['PATH'] = str(tools)
        if github is not None:
            pair.options['environment'].update(REQUESTS_CA_BUNDLE=str(github.cert), SSL_CERT_FILE=str(github.cert),
                HTTP_PROXY=github.url, HTTPS_PROXY=github.url, NO_PROXY='127.0.0.1,::1')
            pair.options['system_config'] = {'rule_fetch_proxy': github.url}
        with pair:
            yield pair, health
