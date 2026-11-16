"""Reference values for the A15 review from the real Python modules (run from the repo root):

PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/upgrades/testdata/gen_a15_review.py \
    > server/internal/agents/upgrades/testdata/a15_review.json

Synthetic values only; the only network peer is a loopback socket of this script.
"""
import copy
import json
import os
import socket
import stat
import sys
import tempfile
import threading
import time

import requests

from backend.agents import address_follow, upgrades


def error_text(exc):
    return type(exc).__name__ + ': ' + str(exc)


# --- poll() on malformed or edge records -------------------------------------------------------

class FakeManager:
    def __init__(self, agent):
        self.agent = agent

    def get_agent_by_id(self, agent_id):
        return copy.deepcopy(self.agent) if self.agent.get('id') == agent_id else None

    def _update_agents(self, update):
        agents = [self.agent]
        return update(agents)


def unreachable(agent, method, path, **kwargs):
    raise requests.ConnectionError('refused')


upgrades._request = unreachable
NOW = time.time()


def poll_case(name, record, **agent_fields):
    agent = {'id': 'a1', 'name': 'n', 'host': '127.0.0.1', 'port': 1, 'token': 't', 'version': '1.0',
             'last_heartbeat': '2026-11-16T00:00:00', 'latest_upgrade': record}
    agent.update(agent_fields)
    try:
        result = upgrades.poll(FakeManager(agent), 'a1')
        outcome = {'result': {key: value for key, value in result.items() if key not in ('updated_at', 'events')}}
    except Exception as exc:
        outcome = {'error': error_text(exc)}
    stored = agent['latest_upgrade']
    if isinstance(stored, dict):
        stored = {key: value for key, value in stored.items() if key not in ('updated_at', 'events')}
    return {'name': name, 'record': record, 'agent': {k: v for k, v in agent_fields.items()}, **outcome,
            'stored': stored}


base = {'update_id': 'j', 'status': 'queued', 'protocol': 2, 'started_at': 0, 'previous_version': '1.0',
        'target_version': '2.0'}
poll_cases = [
    poll_case('string', 'abc'),
    poll_case('one char', 'a'),
    poll_case('int', 5),
    poll_case('float', 1.5),
    poll_case('true', True),
    poll_case('list of int', [1]),
    poll_case('list of none', [None]),
    poll_case('list of two-char string', ['ab']),
    poll_case('list of three-char string', ['abc']),
    poll_case('list of astral pair', ['\U0001F600x']),
    poll_case('list of short pair', [['a']]),
    poll_case('second element bad', [['status', 'succeeded'], ['x']]),
    poll_case('pair terminal', [['status', 'succeeded']]),
    poll_case('pair duplicate', [['status', 'queued'], ['x', 1], ['status', 'failed']]),
    poll_case('pair unhashable key', [[[1], 2]]),
    poll_case('pair dict key', [[{}, 2]]),
    poll_case('dict element', [{'status': 1, 'failed': 2}]),
    poll_case('dict element short', [{'status': 1}]),
    poll_case('pairs not terminal', [['status', 'queued'], ['protocol', 9], ['update_id', 'j'], ['started_at', NOW]]),
    poll_case('unhashable status', {'status': ['x']}),
    poll_case('terminal without id', {'status': 'succeeded'}),
    poll_case('missing protocol', {'status': 'queued'}),
    poll_case('protocol true is 1', {**base, 'protocol': True}, version='1.0', last_heartbeat='2026-11-16T00:00:00'),
    poll_case('protocol 2.0', {**base, 'protocol': 2.0, 'started_at': NOW + 1000}),
    poll_case('protocol 3', {**base, 'protocol': 3, 'started_at': NOW + 1000}),
    poll_case('started_at str', {**base, 'started_at': 'x'}),
    poll_case('started_at list', {**base, 'started_at': [1]}),
    poll_case('started_at none', {**base, 'started_at': None}),
    poll_case('started_at dict', {**base, 'started_at': {}}),
    poll_case('started_at missing', {k: v for k, v in base.items() if k != 'started_at'}),
    poll_case('started_at huge', {**base, 'started_at': 10 ** 400}),
    poll_case('started_at negative huge', {**base, 'started_at': -10 ** 400}, version='9'),
    poll_case('started_at true', {**base, 'started_at': True}, version='9'),
    poll_case('deadline old heartbeat', {**base}, last_heartbeat='2000-01-01T00:00:00'),
    poll_case('deadline fresh heartbeat', {**base}, last_heartbeat='2099-01-01T00:00:00'),
    poll_case('deadline heartbeat year 1', {**base}, last_heartbeat='0001-01-01T00:00:00'),
    poll_case('deadline heartbeat year 1 day 2', {**base}, last_heartbeat='0001-01-02T12:00:00'),
    poll_case('deadline heartbeat aware', {**base}, last_heartbeat='2099-01-01T00:00:00+08:00'),
    poll_case('deadline heartbeat none', {**base}, last_heartbeat=None),
    poll_case('deadline heartbeat bad', {**base}, last_heartbeat='yesterday'),
    poll_case('deadline other version', {**base}, version='1.5', last_heartbeat='2099-01-01T00:00:00'),
    poll_case('deadline already unknown', {**base, 'status': 'unknown'}, last_heartbeat='2000-01-01T00:00:00'),
    poll_case('previous version missing', {k: v for k, v in base.items() if k != 'previous_version'}),
    poll_case('recovery str', {**base, 'recovery_reported_at': 'x', 'started_at': NOW + 1000}),
    poll_case('recovery list', {**base, 'recovery_reported_at': [1], 'started_at': NOW + 1000}),
    poll_case('recovery heartbeat later', {**base, 'recovery_reported_at': 1, 'started_at': NOW + 1000}),
    poll_case('recovery heartbeat year 1', {**base, 'recovery_reported_at': 1, 'started_at': NOW + 1000},
              last_heartbeat='0001-01-01T00:00:00'),
    poll_case('recovery heartbeat none', {**base, 'recovery_reported_at': 1, 'started_at': NOW + 1000},
              last_heartbeat=None),
    poll_case('recovery other owner', {**base, 'recovery_reported_at': 1, 'started_at': NOW + 1000}),
    poll_case('rollback_failed is polled', {**base, 'status': 'rollback_failed', 'started_at': NOW + 1000}),
]

# --- datetime.fromisoformat(value).timestamp() in several zones (naive = local wall clock) -------

iso_values = ['0001-01-01T00:00:00', '0001-01-01T23:59:59', '0001-01-02T00:00:00', '0001-01-02T23:59:59',
              '0001-01-03T00:00:00', '9999-12-30T23:59:59', '9999-12-31T00:00:00', '9999-12-31T15:59:59',
              '9999-12-31T16:00:00', '9999-12-31T23:59:59', '0001-01-01T00:00:00+00:00',
              '9999-12-31T23:59:59-12:00', '2026-03-08T02:30:00', '2026-11-01T01:30:00', '1900-01-01T00:00:00',
              '2026-11-16', '0001-01-01', '9999-12-31', 'x', '']
iso = {}
for zone in ('UTC', 'Asia/Shanghai', 'America/New_York', 'Pacific/Kiritimati', 'Etc/GMT+12'):
    os.environ['TZ'] = zone
    time.tzset()
    rows = []
    for value in iso_values:
        from datetime import datetime
        try:
            rows.append({'value': value, 'timestamp': datetime.fromisoformat(value).timestamp()})
        except Exception as exc:
            rows.append({'value': value, 'error': type(exc).__name__})
    iso[zone] = rows
os.environ['TZ'] = 'UTC'
time.tzset()

# --- Authorization header bytes of _request / _reaches / _ask -----------------------------------


def capture(call):
    listener = socket.socket()
    listener.bind(('127.0.0.1', 0))
    listener.listen(1)
    port = listener.getsockname()[1]
    seen = {}

    def serve():
        listener.settimeout(3)
        try:
            conn, _ = listener.accept()
        except OSError:
            return
        data = b''
        conn.settimeout(3)
        while b'\r\n\r\n' not in data:
            chunk = conn.recv(4096)
            if not chunk:
                break
            data += chunk
        for line in data.split(b'\r\n'):
            if line.lower().startswith(b'authorization:'):
                seen['authorization'] = line.split(b':', 1)[1].strip().hex()
        conn.sendall(b'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 16\r\n'
                     b'Connection: close\r\n\r\n{"writable": 1}')
        conn.close()

    thread = threading.Thread(target=serve)
    thread.start()
    try:
        result = call(port)
        outcome = {'result': result}
    except Exception as exc:
        outcome = {'error': type(exc).__name__}
    listener.close()
    thread.join()
    outcome['authorization'] = seen.get('authorization')
    return outcome


real_request = upgrades.__dict__.get('_real_request')
import importlib
importlib.reload(upgrades)  # undo the _request patch for the header cases
tokens = {'latin1': 'tök', 'cjk': '密钥', 'nel': 'tok\u0085', 'none': None, 'int': 7, 'newline': 'a\nb'}
headers = {}
for name, token in tokens.items():
    def agent_for(port):
        return {'id': 'a1', 'host': '127.0.0.1', 'port': port, 'token': token}
    headers[name] = {
        'request': capture(lambda port: upgrades._request(agent_for(port), 'GET', '/api/upgrade-info').status_code),
        'reaches': capture(lambda port: address_follow._reaches(agent_for(port), '127.0.0.1')),
        'ask': capture(lambda port: list(address_follow._ask(agent_for(port), '127.0.0.1'))),
    }

# --- werkzeug request.args.get('lines', 100, type=int) ------------------------------------------
from werkzeug.datastructures import MultiDict

line_values = ['50', ' 7 ', '+5', '-5', '1_0', '1__0', '_1', '١٢', '１２', '\U0001D7D9',
               '\U00011F51', '\U0001E4F1', '\U00011950', ' 5', '5 ', '\x1c5', '\x855', '0x10', '1e3',
               '', ' ', '1' * 4300, '1' * 4301, '١' * 4301, '0' * 4301]
lines = [{'value': v, 'lines': str(MultiDict({'lines': v}).get('lines', 100, type=int))} for v in line_values]

# --- pathlib errors on an unreadable artifact directory -----------------------------------------
paths = {}
if os.geteuid() != 0:
    root = tempfile.mkdtemp()
    noexec = os.path.join(root, 'noexec')
    os.mkdir(noexec)
    open(os.path.join(noexec, 'configflow-agent-linux-amd64'), 'wb').write(b'\x7fELF')
    os.chmod(noexec, stat.S_IRUSR | stat.S_IWUSR)
    for name, configured in (('directory without search permission', noexec),
                             ('below a directory without search permission', os.path.join(noexec, 'sub'))):
        os.environ['AGENTS_STATIC_DIR'] = configured
        try:
            paths[name] = {'artifacts': upgrades.artifacts()}
        except Exception as exc:
            paths[name] = {'error': type(exc).__name__}
    os.chmod(noexec, stat.S_IRWXU)

# --- TypeError texts of the deadline / recovery arithmetic --------------------------------------


def error(fn):
    try:
        fn()
    except Exception as exc:
        return error_text(exc)
    return None


arithmetic = {
    'str_plus': error(lambda: 'x' + 600), 'list_plus': error(lambda: [1] + 600),
    'none_plus': error(lambda: None + 600), 'dict_plus': error(lambda: {} + 600),
    'float_gt_str': error(lambda: 1.0 > 'x'), 'float_gt_list': error(lambda: 1.0 > [1]),
    'float_gt_dict': error(lambda: 1.0 > {}),
}

# --- UnicodeEncodeError texts of header values ----------------------------------------------------
encode_errors = {}
for value in ('Bearer \u5bc6\u94a5', 'Bearer \u5bc6x', 'Bearer t\u00f6\U0001F600', 'Bearer a\u5bc6\u94a5b\u5bc6'):
    try:
        value.encode('latin-1')
    except UnicodeEncodeError as exc:
        encode_errors[value] = str(exc)

# --- start(): the HA check comes before the malformed previous record is read ---------------------


class Response:
    def __init__(self, status, payload=None):
        self.status_code = status
        self.payload = payload

    def json(self):
        return self.payload

    def raise_for_status(self):
        pass


starts = {}
upgrades.artifacts = lambda: {arch: {'sha256': 'a' * 64, 'size': 1} for arch in upgrades.ARCHES}
upgrades._request = lambda agent, method, path, **kwargs: Response(404)
for name, extra in (('malformed previous, HA active', {'oxidns_ha_publication': {'status': 'running'}}),
                    ('malformed previous', {})):
    agent = {'id': 'a1', 'name': 'n', 'host': '127.0.0.1', 'port': 1, 'token': 't', 'version': '1.0',
             'deployment_method': 'shell', 'latest_upgrade': 'abc', **extra}
    try:
        upgrades.start(FakeManager(agent), 'a1')
        starts[name] = None
    except Exception as exc:
        starts[name] = error_text(exc)

print(json.dumps({'now': NOW, 'encode_errors': encode_errors, 'starts': starts, 'poll': poll_cases, 'iso': iso, 'headers': headers, 'lines': lines, 'paths': paths,
                  'arithmetic': arithmetic}, ensure_ascii=False, indent=1, default=str))
