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
def managed(binary, recorder, *, fail=False, health_status=None, controller_status='RUNNING', unreadable_version=False, github=None):
    require_native_network()
    with Source(json.dumps({'data': {'version': '2.40.0'}}).encode()) as health:
        health.headers = {'Content-Type': 'application/json'}
        health.record_body = True
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
if args != ['-c', '/etc/supervisor/conf.d/supervisord.conf', 'status', 'sub-store']:
    print('synthetic fake refuses every mutating or unknown command', file=sys.stderr)
    sys.exit(97)
if os.environ.get('CONFIGDIFF_SUPERVISOR_FAIL') == '1':
    print('synthetic-controller-rejected', file=sys.stderr)
    sys.exit(1)
print('sub-store ' + os.environ['CONFIGDIFF_SUPERVISOR_STATE'] + ' pid 1, uptime 0:00:01')
''')
        executable.chmod(0o755)
        pair.options['environment'] = {'SUB_STORE_BUILTIN_DIR': str(builtin), 'SUB_STORE_URL': health.url,
            'TMPDIR': str(control),
            'SUB_STORE_BACKEND_API_PORT': str(health.server.server_port),
            'PATH': str(tools) + os.pathsep + os.environ.get('PATH', ''),
            'CONFIGDIFF_SUPERVISOR_LOG': str(pair.root / 'supervisor-events.jsonl'),
            'CONFIGDIFF_SUPERVISOR_STATE': controller_status,
            'CONFIGDIFF_SUPERVISOR_FAIL': '1' if fail else '0'}
        if github is not None:
            pair.options['environment'].update(REQUESTS_CA_BUNDLE=str(github.cert), SSL_CERT_FILE=str(github.cert),
                HTTP_PROXY=github.url, HTTPS_PROXY=github.url, NO_PROXY='127.0.0.1,::1')
            pair.options['system_config'] = {'rule_fetch_proxy': github.url}
        with pair:
            yield pair, health
