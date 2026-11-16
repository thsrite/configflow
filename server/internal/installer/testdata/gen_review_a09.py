"""A09 review corpus: CPython references for inputs the Python suite does not reach
(synthetic values only), in the format of a09_record.py, replayed by
TestDockerHAReviewCorpusMatchesPython.

    PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python \
        server/internal/installer/testdata/gen_review_a09.py [N]

1. generate_guide: new installations (host/macvlan, with and without enrollment) and
   migrations whose free-text values (paths, server URL, names, mounts, image, enrollment
   URL/token) are random strings full of shell metacharacters, quotes, backslashes,
   heredoc markers, Unicode line breaks and C0 controls; verify/inspect with random text.
2. parse_report: valid collector outputs with randomly damaged fields (random base64
   payloads, invalid UTF-8, NUL, ss and /proc/net rows, ip lines with odd interface
   names, Compose versions, JSON payloads) and damaged lines.
3. issue/enroll against hand-edited DATA_DIR/docker-ha-installations states (index and
   session files with every JSON type in every field, invalid UTF-8, directories in
   place of files) and Agent lists (non-dict entries, equal-but-different-type values,
   name/host conflicts), with fake time, randomness and Agent manager.
"""
import base64
import gzip
import json
import os
import random
import shutil
import sys
import tempfile
import time
from pathlib import Path
from types import SimpleNamespace

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import a09_record as rec  # noqa: E402

rec._install()
from backend.agents import docker_ha_enrollment as enrollment  # noqa: E402
from backend.agents import docker_ha_preflight as check  # noqa: E402
from backend.agents.docker_ha_guide import generate_guide  # noqa: E402
from backend.common.config_repository import ProfileRepository  # noqa: E402

N = int(sys.argv[1]) if len(sys.argv) > 1 else 1500
R = random.Random(20261117)
META = list("'\"$`\\;|&<>(){}[]*?!#~%^= \t") + ['$(id)', '`id`', '${HOME}', "'\"'\"'", 'CF_DOCKER_HA_ENROLL', 'EOF',
                                                '\n', '\r', '\x00', '\x85', ' ', '\x7f', '\x1b', 'é', '例', '😀', '﻿', '/', '.', '..']


def text(n=None, alphabet=None):
    n = R.randrange(0, 14) if n is None else n
    pool = alphabet or META + list('abcXYZ019_-')
    return ''.join(R.choice(pool) for _ in range(n))


def path_value():
    roll = R.random()
    if roll < 0.75:
        # Mostly paths that pass _path (absolute, normalised, no , : NUL CR LF).
        parts = [text(R.randrange(1, 8), [c for c in META + list('abc019_-') if c not in (',', ':', '\x00', '\r', '\n', '/', '.', '..')])
                 for _ in range(R.randrange(1, 4))]
        return '/' + '/'.join(p for p in parts if p) or '/x'
    if roll < 0.9:
        return '/' + text()
    return R.choice([None, 5, True, [], {}, '', 'rel/' + text()])


def server_value():
    roll = R.random()
    if roll < 0.7:
        return 'http' + R.choice(['', 's']) + '://' + R.choice(['h.test', 'x.test:8000', '[::1]', '例子.测试']) + '/' + \
            text(R.randrange(0, 12), [c for c in META + list('abc019') if c not in (' ', '\t', '\n', '\r', '\x85', ' ', '﻿')])
    return 'http://' + text()


def name_value():
    return R.choice(['dns-one', 'a_b-9', 'x' * R.randrange(1, 140), text(R.randrange(0, 6)), 'n.' + text(2), 5, None])


SAFE = [c for c in META + list('abc019_-') if c not in (',', ':', '\x00', '\r', '\n', '/', '.', '..')]


def wild_path():
    """A path _path accepts, full of shell metacharacters."""
    parts = [text(R.randrange(1, 8), SAFE) for _ in range(R.randrange(1, 4))]
    return '/' + '/'.join(parts)


def wild_server():
    return 'http' + R.choice(['', 's']) + '://' + R.choice(['h.test', 'x.test:8000', '[::1]', '例子.测试']) + '/' + \
        text(R.randrange(1, 14), [c for c in META + list('abc019@[]') if c not in (' ', '\t', '\n', '\r', '\x85', '\u2028', '\ufeff', '?', '#', '\x00')])


def guide_case():
    mode = R.choice(['host', 'macvlan', 'migrate'])
    base = dict(server_url='http://config.test:8000', agent_name='dns-one', agent_ip='192.168.10.20', data_dir='/srv/cf/data',
                network_mode='host', ha_host_id_path='/srv/cf/host-id', oxidns_port=8082)
    if mode == 'macvlan':
        base.update(network_mode='macvlan', ha_parent='eth0', ha_subnet='192.168.10.0/24', ha_gateway=R.choice(['192.168.10.1', '']))
    enroll = None
    if mode == 'migrate':
        base.update(mode='migrate', container_name='old-oxidns', existing_agent_name='old-dns', ha_data_dir='/srv/cf/ha',
                    backup_dir='/srv/cf/backup', agent_mount={'type': 'bind', 'source': '/srv/cf/agent'},
                    core_mount={'type': 'volume', 'source': 'old-core'})
        wild = ['server_url', 'data_dir', 'ha_host_id_path', 'image', 'ha_data_dir', 'backup_dir', 'agent_mount', 'core_mount',
                'existing_agent_name', 'container_name']
    else:
        wild = ['server_url', 'data_dir', 'ha_host_id_path', 'image', 'enroll_url', 'enroll_token', 'create_host_identity']
        if R.random() < 0.7:
            enroll = {'url': 'http://config.test:8000/api/agents/docker-oxidns-ha-enroll/' + 'ab' * 16, 'token': 'T' * 43,
                      'expires_at': 1800000000}
    for key in R.sample(wild, R.randrange(1, 4)):
        if key == 'server_url':
            base[key] = wild_server() if R.random() < 0.9 else server_value()
        elif key in ('data_dir', 'ha_host_id_path', 'ha_data_dir', 'backup_dir'):
            base[key] = wild_path() if R.random() < 0.9 else path_value()
        elif key == 'image':
            base[key] = 'a' + text(R.randrange(0, 20), list('abcXYZ019._:/@-')) if R.random() < 0.8 else text(5)
        elif key in ('agent_mount', 'core_mount'):
            base[key] = {'type': 'bind', 'source': wild_path()} if R.random() < 0.6 else {'type': 'volume', 'source': 'v' + text(5, list('abc019_.-'))}
        elif key == 'existing_agent_name':
            base[key] = R.choice(['old-dns', 'old_dns-2', 'x' * R.randrange(1, 70), text(3)])
        elif key == 'container_name':
            base[key] = 'c' + text(R.randrange(0, 20), list('abc019_.-'))
        elif key == 'create_host_identity':
            base[key] = R.choice([True, False])
        elif enroll is not None and key == 'enroll_url':
            enroll['url'] = wild_server() + '/api/agents/docker-oxidns-ha-enroll/' + 'cd' * 16
        elif enroll is not None and key == 'enroll_token':
            enroll['token'] = text(R.randrange(1, 60))
    return base, enroll


def call(label, function, *args):
    rec._TEST[0] = label
    try:
        function(*args)
    except Exception:  # noqa: BLE001 - recorded
        pass


for i in range(N):
    request, enroll = guide_case()
    call(f'review/guide/{i}', generate_guide, request, enroll)
for i in range(N // 5):
    call(f'review/verify/{i}', generate_guide, {'action': 'verify', 'vip': R.choice([text(), '10.0.0.' + text(2), '192.168.1.2' + text(1)])})
    call(f'review/inspect/{i}', generate_guide, {'action': 'inspect', 'container_name': text(R.randrange(1, 10), list('abc019_.-') + META)})


# ---------------------------------------------------------------- preflight
def b64(data):
    return base64.b64encode(data).decode() or '-'


GOOD = {
    'kernel': (0, b'Linux'),
    'docker_endpoint': (0, b'local-unix'),
    'docker_version': (0, b'{"client":"27.3.1","server":"27.3.1"}'),
    'daemon_os': (0, b'linux'),
    'daemon_platform': (0, b'{"os":"Debian GNU/Linux 12 (bookworm)","kernel":"6.1.0-26-amd64"}'),
    'daemon_security': (0, b'["name=apparmor","name=seccomp,profile=builtin","name=cgroupns"]'),
    'compose': (0, b'2.29.7'),
    'ipv4': (0, b'1: lo    inet 127.0.0.1/8 scope host lo\\       valid_lft forever preferred_lft forever\n'
                b'2: eth0    inet 192.168.10.20/24 brd 192.168.10.255 scope global eth0\\       valid_lft forever\n'),
    'udp53': (0, b'source=ss\n'),
    'tcp53': (0, b'source=proc-net-v1\ntables=tcp,tcp6\n'),
    'nesting': (0, b'none'),
    'machine_fingerprint': (0, b'ab' * 32),
}


def random_bytes():
    roll = R.random()
    if roll < 0.4:
        return text(R.randrange(0, 30)).encode('utf-8', 'surrogatepass')
    if roll < 0.6:
        return bytes(R.randrange(256) for _ in range(R.randrange(0, 12)))
    if roll < 0.8:
        return json.dumps(R.choice([{'client': text(5), 'server': '1.2.3' + text(2)}, [text(3) for _ in range(R.randrange(0, 40))],
                                    {'os': text(4) + R.choice(['', ' Docker Desktop', 'LinuxKit']), 'kernel': text(3)}, 5, None])).encode()
    return R.choice([b'v2.30.0', b'2.30.0-desktop.1', b'v' + str(R.randrange(10 ** 6)).encode() + b'.30.1', b'2.' + b'9' * R.randrange(1, 30) + b'.0',
                     b'UNCONN 0 0 0.0.0.0:53 0.0.0.0:*', b'source=ss\nudp UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:*',
                     b'source=ss\nLISTEN \xd9\xa1 0 [::]:53 [::]:*', b'source=proc-net-v1\ntables=udp,udp6\nudp 4 0100007F:0035 07',
                     b'source=proc-net-v1\ntables=tcp,tcp6\ntcp 6 ' + b'0' * 32 + b':0035 0A', b'source=other'])


def report(fields):
    lines = [check.BEGIN]
    for key, (code, data) in fields.items():
        lines.append(f'CFHA1\t{key}\t{code}\t{b64(data)}')
    lines.append(check.END)
    return '\n'.join(lines) + '\n'


IFNAMES = ['eth0', 'br-lan', 'enp1s0@if5', 'wlan0:1', 'ünï0', 'a' * 60, '$(id)', "it's", 'x;y', '例', '😀', 'veth\x01', 'a b']
for i in range(N):
    fields = dict(GOOD)
    for _ in range(R.randrange(1, 4)):
        key = R.choice(list(fields))
        if key == 'ipv4' and R.random() < 0.5:
            rows = []
            for _ in range(R.randrange(0, 11)):
                rows.append(f'{R.choice(["1", "2", "١٢", "x"])}:{R.choice([" ", "  ", chr(0x3000), chr(0x85)])}{R.choice(IFNAMES)}'
                            f'{R.choice([" ", "    "])}{R.choice(["inet", "inet6", "inetx"])} '
                            f'{R.choice(["192.168.1.5/24", "10.0.0.1/255.0.0.0", "10.0.0.1/0.0.0.255", "127.0.0.1/8", "169.254.1.1/16", "10.0.0.1/33", "10.0.0.1/032", "1.2.3.4", "01.2.3.4/8", "240.0.0.1/4"])}'
                            f'{R.choice([" brd x", " ", ""])}')
            fields[key] = (0, '\n'.join(rows).encode())
        else:
            fields[key] = (R.choice([0, 0, 0, 1, 125, 126, 127, 255]), random_bytes())
    output = report(fields)
    lines = output.split('\n')
    if R.random() < 0.3:
        j = R.randrange(len(lines))
        lines[j] = R.choice([lines[j] + '\\', lines[j] + '\r', '  ' + lines[j], lines[j][:R.randrange(len(lines[j]) + 1)],
                             lines[j].replace('\t', ' '), lines[j] + '=', ''])
    if R.random() < 0.1:
        lines.insert(R.randrange(len(lines)), R.choice(['CFHA1', 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V2', text()]))
    sep = R.choice(['\n', '\n', '\r\n', ' ', '\x0b'])
    call(f'review/preflight/{i}', check.parse_report, sep.join(lines), R.choice(['host', 'host', 'macvlan']))


# ---------------------------------------------------------------- enrollment
PROFILE = 'default'
BODY = {'server_url': 'https://panel.test/', 'agent_name': 'new-dns', 'agent_ip': '192.168.50.20', 'data_dir': '/srv/oxidns',
        'oxidns_port': 8082, 'network_mode': 'host', 'action': 'generate'}


class Repository:
    validate_profile_id = staticmethod(ProfileRepository.validate_profile_id)

    def __init__(self, data_dir):
        self.data_dir = Path(data_dir)


class Manager:
    def __init__(self, agents, error=None):
        self.agents = agents
        self.error = error

    def _update_agents(self, updater, profile_id=None):
        if self.error is not None:
            raise self.error
        return updater(self.agents)


def spec_key(body):
    spec = {**body, 'mode': 'new', 'server_url': body['server_url'].rstrip('/'), 'profile_id': PROFILE}
    spec.pop('action', None)
    if spec.get('install_method') == 'compose_compact':
        for ignored in ('image', 'ha_host_id_path', 'create_host_identity'):
            spec.pop(ignored, None)
    import hashlib
    return hashlib.sha256(json.dumps(spec, sort_keys=True).encode()).hexdigest()


SID = 'ab' * 16
NOW = time.time()


def write(root, name, data):
    root.mkdir(parents=True, exist_ok=True)
    path = root / name
    if data is DIRECTORY:
        path.mkdir()
        return
    if isinstance(data, str):
        data = data.encode('utf-8', 'surrogatepass')
    path.write_bytes(data)
    os.chmod(path, 0o600)


DIRECTORY = object()
VALUES = [None, True, False, 0, 1, -1, 2 ** 70, 1.5, '', 'x', [], {}, [1], {'a': 1}, 'é']


def session_doc(**changes):
    doc = {'id': SID, 'profile_id': PROFILE, 'name': 'new-dns-oxidns', 'host': '192.168.50.20', 'port': 8082,
           'token': 'T' * 43, 'token_hash': __import__('hashlib').sha256(b'T' * 43).hexdigest(), 'expires': int(NOW) + 3600}
    for key, value in changes.items():
        if value is MISSING:
            doc.pop(key, None)
        else:
            doc[key] = value
    return doc


MISSING = object()
issue_cases = []
# Index states (index content, session file content or None).
for index in [SID, SID + '\n', 'AB' * 16, 'zz', '', b'\xff\xfe', DIRECTORY]:
    issue_cases.append((BODY, {'index': index, 'session': json.dumps(session_doc(expires=0))}))
for raw in ['null', '[]', '"x"', '5', 'true', '{', '', '﻿{}', b'\xc3', '{"id": "' + SID + '"}', 'NaN', DIRECTORY]:
    issue_cases.append((BODY, {'index': SID, 'session': raw}))
for key in ['id', 'expires', 'token', 'profile_id', 'name', 'host', 'port', 'token_hash']:
    for value in VALUES + [MISSING]:
        issue_cases.append((BODY, {'index': SID, 'session': json.dumps(session_doc(**{key: value}))}))
        if key in ('id', 'token'):
            issue_cases.append((BODY, {'index': SID, 'session': json.dumps(session_doc(**{key: value, 'expires': 0}))}))
for ident in ['', 'a/b', '../escape', 'a.b', '.', 'x\x00y', 'with space', '例']:
    issue_cases.append((BODY, {'index': SID, 'session': json.dumps(session_doc(id=ident, expires=0)), 'mkdir': ['a']}))
for expires in [NOW - 1, NOW + 1, int(NOW), 2 ** 80, -(2 ** 80), 1.7976931348623157e308, True, 0.0]:
    issue_cases.append((BODY, {'index': SID, 'session': json.dumps(session_doc(expires=expires))}))
issue_cases.append((BODY, {'index': SID, 'session': json.dumps(dict(session_doc(), extra={'keep': [1, 'é']}, expires=0))}))
issue_cases.append((BODY, {'root_file': True}))
issue_cases.append((BODY, {'lock_dir': True}))
for body in [{k: v for k, v in BODY.items() if k != 'agent_ip'}, {k: v for k, v in BODY.items() if k != 'agent_name'},
             dict(BODY, agent_name=5), dict(BODY, agent_name=None), dict(BODY, agent_ip=5, oxidns_port=1.5),
             {k: v for k, v in BODY.items() if k != 'oxidns_port'}, dict(BODY, server_url='https://p.test///'),
             dict(BODY, install_method='compose_compact', image='x', ha_host_id_path='/y', create_host_identity=True),
             dict(BODY, nested={'b': [1, {'z': 1, 'a': 'é'}], 'a': None}, mode='migrate', profile_id='other')]:
    issue_cases.append((body, {}))

for i, (body, state) in enumerate(issue_cases):
    tmp = Path(tempfile.mkdtemp())
    try:
        data = tmp / 'data'
        data.mkdir()
        root = data / 'docker-ha-installations'
        if state.get('root_file'):
            (data / 'docker-ha-installations').write_text('x')
        if state.get('lock_dir'):
            (root / '.issue.lock').mkdir(parents=True)
        if 'index' in state:
            name = spec_key(body) + '.index'
            write(root, name, state['index'] if state['index'] is DIRECTORY else state['index'])
        if state.get('session') is not None:
            write(root, SID + '.json', state['session'])
        for d in state.get('mkdir', []):
            (root / d).mkdir(parents=True, exist_ok=True)
        call(f'review/issue/{i}', enrollment.issue, Repository(data), json.loads(json.dumps(body)), PROFILE)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

AGENT = {'id': 'agent_1', 'name': 'new-dns-oxidns', 'host': '192.168.50.20', 'port': 8082, 'profile_id': PROFILE,
         'service_type': 'oxidns', 'deployment_method': 'docker', 'token': 'agent-token', 'docker_ha_installation_id': SID}
enroll_cases = []
GOOD_TOKEN = 'T' * 43
for token in [GOOD_TOKEN, None, 'T' * 42, 'T' * 44, 'T' * 42 + '=', 'U' * 43, 'T' * 42 + 'é']:
    enroll_cases.append((SID, token, json.dumps(session_doc()), [], None))
for sid in ['', 'AB' * 16, 'ab' * 15, 'ab' * 16 + '\n', '../' + 'a' * 29]:
    enroll_cases.append((sid, GOOD_TOKEN, json.dumps(session_doc()), [], None))
for raw in ['null', '[]', '"x"', '5', '{', '', b'\xe4\xb8', None, DIRECTORY, '{"expires": 1e400}']:
    enroll_cases.append((SID, GOOD_TOKEN, raw, [], None))
for key in ['expires', 'token_hash', 'profile_id', 'name', 'host', 'port']:
    for value in VALUES + [MISSING, 'Invalid/../x', 'abc' * 30, 'ü' * 64]:
        enroll_cases.append((SID, GOOD_TOKEN, json.dumps(session_doc(**{key: value})), [dict(AGENT)], None))
for expires in [NOW - 1, NOW + 100, 2 ** 80, True, 1.0]:
    enroll_cases.append((SID, GOOD_TOKEN, json.dumps(session_doc(expires=expires)), [], None))
agent_lists = [
    [], [dict(AGENT)], [dict(AGENT, port=8082.0)], [dict(AGENT, port=True)], [dict(AGENT, port=8083)], [dict(AGENT, profile_id='x')],
    [dict(AGENT, service_type='mihomo')], [{k: v for k, v in AGENT.items() if k != 'token'}], [{k: v for k, v in AGENT.items() if k != 'id'}],
    ['not-a-dict', dict(AGENT)], [dict(AGENT), 'not-a-dict'], [5], [dict(AGENT, docker_ha_installation_id='other')],
    [dict(AGENT, docker_ha_installation_id=None, id='agent_2')], [dict(AGENT, docker_ha_installation_id=None, host='192.168.50.21')],
    [{'name': 'new-dns-oxidns'}], [{'host': '192.168.50.20'}], [{}],
]
for agents in agent_lists:
    enroll_cases.append((SID, GOOD_TOKEN, json.dumps(session_doc()), agents, None))
from backend.common.config_repository import ProfileNotFound, ProfileInUse  # noqa: E402
from backend.agents.manager import AgentDeploymentConflict  # noqa: E402
for error in [ProfileNotFound('gone'), AgentDeploymentConflict('部署进行中'), RuntimeError('boom')]:
    enroll_cases.append((SID, GOOD_TOKEN, json.dumps(session_doc()), [], error))

for i, (sid, token, raw, agents, error) in enumerate(enroll_cases):
    tmp = Path(tempfile.mkdtemp())
    try:
        data = tmp / 'data'
        root = data / 'docker-ha-installations'
        root.mkdir(parents=True)
        if raw is not None:
            write(root, SID + '.json', raw)
        call(f'review/enroll/{i}', enrollment.enroll, Repository(data), Manager(json.loads(json.dumps(agents)), error), sid, token)
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

rows = rec._RECORDS
out = Path(__file__).with_name('docker_ha_review.jsonl.gz')
with open(out, 'wb') as raw_file:
    with gzip.GzipFile(fileobj=raw_file, mode='wb', mtime=0) as fh:
        fh.write(('\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n').encode())
counts = {}
for e in rows:
    counts[e['fn']] = counts.get(e['fn'], 0) + 1
print(f'{len(rows)} calls -> {out}: {counts}; not recordable: {rec._SKIPPED}')
