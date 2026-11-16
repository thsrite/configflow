"""Reference corpus for module A10 (docker_ha_compact / docker_ha_compose_direct /
docker_ha_compose), replayed by docker_ha_compose_corpus_test.go.

    rt --back server/internal/installer/testdata/docker_ha_compose.jsonl.gz -- bash -lc \
      'set -o pipefail; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_docker_ha_compose.py | gzip -9n > server/internal/installer/testdata/docker_ha_compose.jsonl.gz'

Lines: {"id", "kind", ...}
- kind "gen": {"fn", "args": [params, request, enrollment], "out": [text, info] | "err": [class, message]};
  the bundle's content_base64 is stored as its sha256 ("content_sha256").
- kind "deflate": {"input": base64, "sha256", "size"} of zipfile's raw deflate
  (zlib.compressobj(-1, DEFLATED, -15), compress + flush).

Values are synthetic (example/test hosts, documentation addresses).
"""
import base64
import hashlib
import json
import random
import sys
import zlib

sys.path.insert(0, '.')
from backend.agents.docker_ha_compact import generate_compact  # noqa: E402
from backend.agents.docker_ha_compose import generate_bundle  # noqa: E402
from backend.agents.docker_ha_compose_direct import generate_direct  # noqa: E402

SEED = 20261116
rng = random.Random(SEED)
FUNCTIONS = {'generate_compact': generate_compact, 'generate_direct': generate_direct, 'generate_bundle': generate_bundle}
META = ['$', "'", '"', '`', ';', '|', '&', '\\', '$(id)', '${HOME}', '*', '?', '!', '#', '~', '<', '>', ' ', '\t']
ODD = [' ', ' ', '\x85', '\x1c', '\x1d', '\x1e', '\x0b', '\x0c', '\x7f', '\x01', '﻿', '　', '\xa0',
       '目录', '例', '😀', 'Ａ', 'é', '​', '\U0001e4f1', 'İ', 'ß']
SESSION = 'a' * 32
TOKEN = 't' * 43


def word():
    pieces = ['data', 'srv', 'cf', 'oxidns', 'a b', 'x.y', 'my:data', 'v1', '..x', '-n', 'opt'] + META + ODD
    return ''.join(rng.choice(pieces) for _ in range(rng.randint(1, 4))).replace('/', '_').replace(',', '_').replace('\x00', '_')


def data_dir():
    r = rng.random()
    if r < 0.15:
        return rng.choice(['/etc', '/etc/cf', '/usr/x', '/proc', '/run/cf', '/boot', '/var', '/var/lib', '/opt', '/srv', '/home',
                           '/root', '/tmp', '/volume1', '/volume1/docker', '/volume12/docker', '/volume1/docker/cf', '/volume/docker',
                           '/volume٣', '/var/lib/cf', '/opt/cf', '/sys', '/dev/x', '/sbin', '/bin/x', '/etcetera', '/runs'])
    path = '/' + '/'.join(word() for _ in range(rng.randint(1, 3)))
    for bad in ('\r', '\n'):
        path = path.replace(bad, '_')
    return path


def agent_ip():
    return rng.choice(['192.0.2.10', '192.168.10.20', '198.51.100.7', '10.0.0.1', 3232235796, '0.0.0.0', '127.0.0.1',
                       '192.168.10.0', '256.1.1.1', '', 'x', '1.2.3', 3221225994])


VALID = {
    'server_url': ['http://panel.example', 'https://panel.example/sub', 'http://panel.example/$route', "https://panel.example/it's",
                   'http://例え.test', 'http://panel.example:8443/a$b`c', 'https://[2001:db8::1]:5001'],
    'agent_name': ['compose-dns', 'agent', 'Dns-HA_1', 'x', 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij', 'NAS-01'],
    'agent_ip': ['192.168.10.20', '192.168.10.77', 3232238100],
    'network_mode': ['host', 'macvlan'],
    'oxidns_port': [8082, 1024, 65535, 8083],
    'ha_parent': ['eth0', 'enp1s0', 'br-lan', 'eth0.100'],
    'ha_subnet': ['192.168.10.0/24', '192.168.0.0/16'],
    'ha_gateway': ['192.168.10.1', '', '192.168.10.254'],
}


def valid_params():
    value = {key: rng.choice(choices) for key, choices in VALID.items()}
    data = '/' + '/'.join(word() for _ in range(rng.randint(1, 3)))
    value['data_dir'] = data.replace('\r', '_').replace('\n', '_')
    value['ha_host_id_path'] = rng.choice(['/etc/machine-id', '/srv/host-id', '/' + word().replace('\r', '_').replace('\n', '_')])
    ordered = {k: value[k] for k in ('server_url', 'agent_name', 'agent_ip', 'data_dir', 'network_mode')}
    ordered.update({'enable_mihomo': False, 'enable_oxidns': True, 'enable_dns_ha': True})
    ordered.update({k: value[k] for k in ('oxidns_port', 'ha_parent', 'ha_subnet', 'ha_gateway', 'ha_host_id_path')})
    if rng.random() < 0.3:
        broken = params()
        key = rng.choice(list(broken))
        ordered[key] = broken[key]
    return ordered


def params():
    mode = rng.choice(['host', 'host', 'macvlan', 'macvlan', 'bridge', 'none', 5])
    value = {
        'server_url': rng.choice(['http://panel.example', 'https://panel.example/sub', 'http://panel.example/$route',
                                  "https://panel.example/it's", 'http://例え.test', 'http://panel.example:8443/a$b`c',
                                  'https://[2001:db8::1]:5001', 'http://panel.example/' + word()]),
        'agent_name': rng.choice(['compose-dns', 'agent', 'A' * 40, 'Dns-HA_1', 'x', 'name with space', 'İstanbul', 'é-dns',
                                  'a.b', '-bad', 'agent\n', 5, 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij']),
        'agent_ip': agent_ip(),
        'data_dir': data_dir(),
        'network_mode': mode,
        'enable_mihomo': False, 'enable_oxidns': True, 'enable_dns_ha': True,
        'oxidns_port': rng.choice([8082, 1, 65535, 0, 65536, 53, 8083, -1, 10 ** 20]),
        'ha_parent': rng.choice(['eth0', 'enp1s0', 'br-lan', '', 'eth 0', 'a' * 16, 'eth0.100', 5, None, ['x'], {'a': 1}, 1.5]),
        'ha_subnet': rng.choice(['192.168.10.0/24', '192.0.2.0/25', '10.0.0.0/8', '', '192.168.10.1/24', 'x', '192.168.11.0/24']),
        'ha_gateway': rng.choice(['192.168.10.1', '', '192.168.10.254', '192.168.11.1', 'x', 0, 3232235777, None]),
        'ha_host_id_path': rng.choice(['/etc/machine-id', '/srv/host-id', '', 5, None]),
    }
    data = value['data_dir']
    r = rng.random()
    if r < 0.2:
        value['ha_host_id_path'] = data + '/' + rng.choice(['agent', 'oxidns', 'ha', '.ha-compose', '.ha-install', 'compose.yaml',
                                                           'prepare.sh', 'bootstrap.py', 'enrollment.json', 'README.txt',
                                                           'agent/x', 'host-id', 'agentx', 'ha/id', '.ha-compose/s'])
    elif r < 0.25:
        value['ha_host_id_path'] = data
    elif r < 0.35:
        value['ha_host_id_path'] = '/' + word().replace('\r', '_').replace('\n', '_')
    return value


def request():
    value = {}
    if rng.random() < 0.7:
        value['image'] = rng.choice(['local.test/agent:compose', 'thsrite/config-flow-agent:latest', 'registry.example:5000/a/b@sha256:' + 'f' * 64, 'x'])
    if rng.random() < 0.7:
        value['create_host_identity'] = rng.choice([True, False, True])
    if rng.random() < 0.2:
        value['extra'] = word()
    return value


def enrollment(server):
    r = rng.random()
    if r < 0.15:
        return None
    if r < 0.25:
        return {'url': server + '/api/agents/docker-oxidns-ha-enroll/' + rng.choice(['A' * 32, 'a' * 31, 'g' * 32, '', 'a' * 33]),
                'token': TOKEN}
    if r < 0.35:
        return {'url': server + '/api/agents/docker-oxidns-ha-enroll/' + SESSION, 'token': rng.choice(['t' * 42, 't' * 44, 't' * 42 + '=', ''])}
    session = ''.join(rng.choice('0123456789abcdef') for _ in range(32))
    token = ''.join(rng.choice('ABCxyz019_-') for _ in range(43))
    return {'url': server.rstrip('/') + '/api/agents/docker-oxidns-ha-enroll/' + session, 'token': token, 'expires_at': 1790000000}


def run(fn, args):
    record = {'kind': 'gen', 'fn': fn, 'args': args}
    try:
        text, info = FUNCTIONS[fn](*json.loads(json.dumps(args)))
    except Exception as exc:  # noqa: BLE001 - every class is recorded
        record['err'] = [type(exc).__name__, str(exc)]
        return record
    info = dict(info)
    if 'content_base64' in info:
        info['content_sha256'] = hashlib.sha256(info.pop('content_base64').encode()).hexdigest()
    record['out'] = [text, info]
    return record


def cases():
    fixed = []
    base = {'server_url': 'http://panel.example', 'agent_name': 'compose-dns', 'agent_ip': '192.168.10.20', 'data_dir': '/srv/data',
            'network_mode': 'host', 'enable_mihomo': False, 'enable_oxidns': True, 'enable_dns_ha': True, 'oxidns_port': 8082,
            'ha_parent': 'eth0', 'ha_subnet': '192.168.10.0/24', 'ha_gateway': '192.168.10.1', 'ha_host_id_path': '/srv/host-id'}
    grant = {'url': 'http://panel.example/api/agents/docker-oxidns-ha-enroll/' + SESSION, 'token': TOKEN}
    for mode in ('host', 'macvlan'):
        for create in (False, True):
            for data in ('/srv/data', '/srv/my:data', '/srv/目录 $data', "/srv/it's `x` $(id)", '/srv/a b', '/srv/a\x85b',
                         '/srv/a\x1cb', '/srv/' + 'x' * 900, '/srv/a\tb', '/srv/#x: y', '/srv/- z', '/srv/&a *b'):
                for gateway in ('192.168.10.1', ''):
                    p = dict(base, network_mode=mode, data_dir=data, ha_gateway=gateway)
                    for e in (grant, None):
                        r = {'image': 'local.test/agent:compose', 'create_host_identity': create}
                        for fn in FUNCTIONS:
                            fixed.append((fn, [p, r, e]))
    for fn, args in fixed:
        yield run(fn, args)
    for _ in range(int(sys.argv[1]) if len(sys.argv) > 1 else 1500):
        p, r = (valid_params() if rng.random() < 0.7 else params()), request()
        e = enrollment(p['server_url'] if isinstance(p['server_url'], str) else '')
        for fn in FUNCTIONS:
            yield run(fn, [p, r, e])


def deflate_inputs():
    yield b''
    yield b'a'
    yield b'ab'
    yield b'abc'
    yield b'a' * 258
    yield b'a' * 259
    yield b'a' * 100000
    yield bytes(range(256)) * 300
    text = open('backend/agents/docker_ha_guide.py', 'rb').read()
    yield text
    yield text * 3
    for size in (10, 100, 1000, 16383, 16384, 20000, 32768, 32769, 65274, 65536, 70000, 140000):
        alphabet = rng.choice([b'ab', b'abcd', b'0123456789abcdef', bytes(range(32, 127)), bytes(range(256))])
        yield bytes(rng.choice(alphabet) for _ in range(size))
    for _ in range(120):
        parts = []
        for _ in range(rng.randint(1, 60)):
            r = rng.random()
            if r < 0.3:
                parts.append(rng.choice([b'docker compose up -d\n', b'cf_state=', b'printf "%s\\n" ', '持久目录'.encode(), b'\x00' * rng.randint(1, 600)]))
            elif r < 0.6 and parts:
                start = rng.randrange(len(b''.join(parts)) or 1)
                joined = b''.join(parts)
                parts.append(joined[start:start + rng.randint(3, 400)])
            else:
                parts.append(bytes(rng.randrange(256) for _ in range(rng.randint(1, 300))))
        data = b''.join(parts)
        if rng.random() < 0.2:
            data = data * rng.randint(2, 40)
        yield data[:200000]


def main():
    n = 0
    for record in cases():
        record['id'] = n
        n += 1
        print(json.dumps(record, ensure_ascii=False))
    for data in deflate_inputs():
        compressor = zlib.compressobj(zlib.Z_DEFAULT_COMPRESSION, zlib.DEFLATED, -15)
        out = compressor.compress(data) + compressor.flush()
        print(json.dumps({'id': n, 'kind': 'deflate', 'input': base64.b64encode(data).decode(),
                          'sha256': hashlib.sha256(out).hexdigest(), 'size': len(out)}))
        n += 1


main()
