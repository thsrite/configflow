"""Reference corpus of the Docker HA guide and preflight parser (A09).

Runs the Python functions on synthetic inputs and writes gzip-compressed JSON
lines: {"id", "kind", "input", "ok", "result" | "digests" | "error": {"class", "value_error", "message"}}
(guide answers as a digest per top-level key, see below).

  kind "guide":     input {"request": <JSON value>, "enrollment": null | {"url", "token"}}
                    → docker_ha_guide.generate_guide(request, enrollment)
  kind "preflight": input {"output": <JSON value>, "mode": <JSON value> | absent}
                    → docker_ha_preflight.parse_report(output[, mode])

Run from the repository root on the test server:

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_docker_ha.py \
        | gzip -9n > server/internal/installer/testdata/docker_ha.jsonl.gz

TestDockerHACorpusMatchesPython replays it with the Go port. Every value is
synthetic (example/test names, private and documentation addresses).
"""
import base64
import copy
import hashlib
import json
import random
import sys
from pathlib import Path

sys.path.insert(0, '.')
from backend.agents import docker_ha_preflight as check  # noqa: E402
from backend.agents.docker_ha_guide import generate_guide  # noqa: E402

rng = random.Random(20261116)
cases = []
MISSING = object()


def add(kind, value):
    cases.append((kind, json.loads(json.dumps(value))))


# ---------------------------------------------------------------- guide
ENROLL = {'url': 'http://config.test:8000/api/agents/docker-oxidns-ha-enroll/' + 'ab' * 16, 'token': 'T0k-_' * 8 + 'abc'}
ODD_ENROLL = {'url': "http://config.test/it's/$(id)/例/api/agents/docker-oxidns-ha-enroll/" + 'cd' * 16, 'token': 'x' * 43}

BASE = dict(server_url='http://config.test:8000', agent_name='dns-one', agent_ip='192.168.10.20',
            data_dir='/srv/cf/new-data', network_mode='host', ha_host_id_path='/srv/cf/host-id', oxidns_port=8082,
            image='local.test/agent:guide')
MACVLAN = dict(network_mode='macvlan', ha_parent='eth0', ha_subnet='192.168.10.0/24', ha_gateway='192.168.10.1')
MIGRATE = dict(BASE, mode='migrate', container_name='original-oxidns', existing_agent_name='original-dns',
               ha_data_dir='/srv/cf/new-ha', backup_dir='/srv/cf/backup',
               agent_mount={'type': 'bind', 'source': '/srv/cf/old-agent'},
               core_mount={'type': 'bind', 'source': '/srv/cf/old-core'})

PATHS = ['/srv/cf/new-data', 'relative', '/', '/a/../b', '/a//b', '//a', '///a', '/a,b', '/a:b', '/a\nb', '/a\rb',
         '/a\x00b', '/a/', '/a/.', '/a/./b', None, 5, True, [], {}, '/数据/目录', '/a b', "/it's", '/$(touch x)',
         '/a b', '/' + 'p' * 300, '/a\\b', '/-rf', '/.hidden', '/..', '/a/..']
ADDRESSES = ['192.168.10.20', '', '127.0.0.1', '0.0.0.0', '224.0.0.1', '169.254.1.1', '240.0.0.1', '255.255.255.255',
             '192.168.010.20', '192.168.10.256', ' 192.168.10.20', '192.168.10.20\n', '192.168.10.20/24', 3232238100,
             True, False, None, 1.5, [], {}, '::1', '１９２.168.10.20', 0, 2 ** 32, -1, 2130706433,
             '10.0.0.5', '192.168.10.1', '192.168.10.0', '192.168.10.255', '192.168.11.5', 3232238081]
SERVERS = ['http://config.test:8000', 'https://cfg.example.test/', 'https://cfg.example.test///', 'http://user:pw@x.test',
           'http://user@x.test', 'http://:pw@x.test', 'http://@x.test', 'http://x.test?a=1', 'http://x.test?', 'http://x.test#f',
           'http://x.test#', 'http://', 'http:///path', 'ftp://x.test', 'http://x y', 'http://x.test\n', 'http://x.test\n\n',
           'http://x.test\r', 'http://[::1]:5000', 'http://[bad', 'http://[fe80::1%25eth0]/', 'http://例子.测试:8080',
           'HTTP://x.test', 'http://x.test/$NOT_A_VARIABLE', "http://x.test/'q'", None, 5, 'http://x.test　',
           'http://x.test ', 'https://x.test:99999', 'http://x.test:abc', 'httpss://x.test', 'http:/x.test',
           ' http://x.test', 'http://x.test/a b', 'http://x\x7f.test', 'http://X.TEST/Path/', 'http://x.test/ ']
NAMES = ['dns-one', 'bad name', 'x\n', '', 'A' * 200, 5, None, 'ünï', 'a_b-C9', 'x.y', '-lead', 'dns-one\n\n']
PORTS = [8082, 9092, 1023, 1024, 65535, 65536, '8082', True, False, 8082.0, 10 ** 30, -1, None, 0]
IMAGES = [MISSING, 'repo/image:latest;touch bad', '', 'a' * 255, 'a' * 256, 'Local/Img@sha256:abc', 5, None,
          '-image', 'image\n', 'registry.test:5000/ns/img:tag', '镜像']
PARENTS = ['eth0', 'enp1s0', 'bad parent', '', 'a' * 15, 'a' * 16, '_x', '.x', 5, None, 'eth0.100', 'br-lan']
SUBNETS = ['192.168.10.0/24', '192.168.10.0/255.255.255.0', '192.168.10.0/0.0.0.255', '192.168.10.1/24', '10.0.0.0/31',
           '10.0.0.0/30', '/24', '', 3232238080, None, '192.168.10.0', '0.0.0.0/8', '127.0.0.0/8', '192.168.10.0/24/1',
           '192.168.10.0/033', '192.168.10.0/24 ', '169.254.0.0/16', '224.0.0.0/4']
GATEWAYS = ['192.168.10.1', '', '192.168.10.20', '192.168.10.0', '192.168.10.255', '10.0.0.1', 3232238081, True, 0,
            None, [], {}, 1.5, '192.168.10.254', 'gw', 2 ** 40, False]


def guide(request, enrollment=None):
    add('guide', {'request': request, 'enrollment': enrollment})


def variants(base, key, values):
    for value in values:
        request = dict(base)
        if value is MISSING:
            request.pop(key, None)
        else:
            request[key] = value
        yield request


# Non-dict requests and the read-only steps.
for request in [[], 'x', None, 5, True, {}]:
    guide(request)
for action in ['check', 'inspect', 'verify', 'generate', 'bogus', None, 1, '', 'CHECK']:
    guide({'action': action})
for name in ['', 'existing', 'x;echo bad', 0, 1, None, [], [1], {}, 'a' * 128, 'a' * 129, '.x', 'ok.name_1-2', 'x\n', False, 1.5]:
    guide({'action': 'inspect', 'container_name': name})
for vip in ['192.168.10.8', '127.0.0.1', '0.0.0.0', '224.0.0.5', '169.254.0.1', '240.0.0.1', '255.255.255.255', '::1', None,
            3232238088, True, False, 1.5, '192.168.1.2;echo', '01.2.3.4', -1, 2 ** 32, 2 ** 32 - 2, [], {}, '', ' 10.0.0.1',
            '10.0.0.1\n', '１.2.3.4', 1, MISSING]:
    request = {'action': 'verify'}
    if vip is not MISSING:
        request['vip'] = vip
    guide(request)
# check with network modes and odd outputs is covered by the preflight cases;
# a few route-shaped ones here.
for mode in [MISSING, 'host', 'macvlan', 'bridge', None, 1]:
    request = {'action': 'check', 'output': 'old text'}
    if mode is not MISSING:
        request['network_mode'] = mode
    guide(request)
guide({'action': 'check'})
guide({'action': 'check', 'output': 5})

# New installation, one factor at a time, with and without enrollment.
for method in ['script', 'compose', 'compose_direct', 'compose_compact']:
    base = dict(BASE, install_method=method)
    requests = [base, dict(base, **MACVLAN)]
    for key, values in [('data_dir', PATHS), ('ha_host_id_path', PATHS), ('agent_ip', ADDRESSES), ('server_url', SERVERS),
                        ('agent_name', NAMES), ('oxidns_port', PORTS), ('image', IMAGES),
                        ('create_host_identity', [True, False, 'true', 1, None, MISSING]),
                        ('network_mode', ['host', 'macvlan', 'bridge', '', None, 1, MISSING]),
                        ('mode', ['new', 'migrate', 'x', None, MISSING]), ('action', ['generate', MISSING, 'bogus'])]:
        if method != 'script' and key in ('data_dir', 'ha_host_id_path', 'server_url', 'agent_ip'):
            values = values[:12]
        requests += list(variants(base, key, values))
    if method == 'script':
        for key, values in [('ha_parent', PARENTS), ('ha_subnet', SUBNETS), ('ha_gateway', GATEWAYS), ('agent_ip', ADDRESSES)]:
            requests += list(variants(dict(base, **MACVLAN), key, values))
        for key, values in [('ha_parent', PARENTS[:6]), ('ha_subnet', SUBNETS[:6]), ('ha_gateway', GATEWAYS)]:
            requests += list(variants(base, key, values))  # unused in host mode, but hashed into the plan
        requests.append(dict(base, create_host_identity=True, ha_host_id_path='/srv/cf/host-$(touch x)'))
        requests.append(dict(base, extra_field={'nested': [1, 2.5, None, '例']}, oxidns_port=10 ** 20))
        requests.append({'server_url': 'http://config.test', 'agent_ip': '192.168.10.20', 'data_dir': '/srv/cf/d'})
    for request in requests:
        guide(request)
        if method == 'script' or rng.random() < 0.3:
            guide(request, ENROLL)
    guide(base, ODD_ENROLL)
    guide(dict(base, **MACVLAN), ODD_ENROLL)
for method in [None, 'other', 5, '']:
    guide(dict(BASE, install_method=method))

# Migration.
VOLUMES = dict(MIGRATE, agent_mount={'type': 'volume', 'source': 'old_agent'}, core_mount={'type': 'volume', 'source': 'old_core'})
for base in [MIGRATE, VOLUMES, dict(MIGRATE, **MACVLAN), dict(VOLUMES, **MACVLAN)]:
    guide(base)
    guide(dict(base, server_url=base['server_url'] + '/$NOT_A_VARIABLE'))
    guide(dict(base, create_host_identity=True))
    guide(base, ENROLL)
mounts = [{'type': 'bind', 'source': 'relative'}, {'type': 'volume', 'source': '--option'}, {'type': 'volume', 'source': 'ok_vol'},
          {'type': 'tmpfs', 'source': '/x'}, 'bind', None, {'type': 'bind'}, {'type': 'bind', 'source': '/srv/cf/new-ha/x'},
          {'type': 'bind', 'source': '/srv/cf/old-core'}, {'type': 'bind', 'source': '//srv/cf/old-agent'},
          {'type': ['bind'], 'source': '/x'}, {'type': 'volume', 'source': 'a' * 129}, {'type': 'bind', 'source': '/srv/cf/backup2'},
          {'type': 'bind', 'source': '/srv'}, {'type': 'volume', 'source': 'old_core'}]
for key, values in [('container_name', ['original-oxidns', 'bad;name', '', None, 5, 'a' * 129, 'x\n']),
                    ('existing_agent_name', ['original-dns', 'bad name', 'x\n', '', None, 5, 'ünï']),
                    ('agent_mount', mounts), ('core_mount', mounts),
                    ('ha_data_dir', PATHS[:16] + ['/srv/cf/backup', '/srv/cf/backup/x', '/srv/cf', '/srv/cf/old-agent']),
                    ('backup_dir', PATHS[:16] + ['/srv/cf/new-ha', '/srv/cf/new-ha/sub', '//srv/cf/b']),
                    ('server_url', SERVERS[:20]), ('oxidns_port', PORTS), ('image', IMAGES), ('agent_name', NAMES),
                    ('agent_ip', ADDRESSES[:20]), ('ha_host_id_path', PATHS[:16]),
                    ('install_method', ['script', 'compose', 'compose_direct', 'compose_compact', MISSING])]:
    for request in variants(MIGRATE, key, values):
        guide(request)
guide(dict(MIGRATE, ha_data_dir='/tmp/x', backup_dir='/tmp/x/backup'))
guide(dict(MIGRATE, ha_data_dir='//a', backup_dir='//a/b'))
guide(dict(VOLUMES, core_mount={'type': 'volume', 'source': 'old_agent'}))
guide(dict(MIGRATE, existing_agent_name="it's\n"))

# Seeded random combinations.
POOLS = {'data_dir': PATHS, 'ha_host_id_path': PATHS, 'agent_ip': ADDRESSES, 'server_url': SERVERS, 'agent_name': NAMES,
         'oxidns_port': PORTS, 'image': IMAGES, 'ha_parent': PARENTS, 'ha_subnet': SUBNETS, 'ha_gateway': GATEWAYS,
         'network_mode': ['host', 'macvlan', 'macvlan', 'bridge'], 'create_host_identity': [True, False, False],
         'mode': ['new', 'new', 'migrate'], 'install_method': ['script', 'script', 'script', 'compose', 'compose_compact', 'compose_direct']}
for _ in range(400):
    request = dict(rng.choice([BASE, dict(BASE, **MACVLAN), MIGRATE, dict(MIGRATE, **MACVLAN)]))
    for key in rng.sample(sorted(POOLS), rng.randint(1, 4)):
        value = rng.choice(POOLS[key])
        if value is MISSING:
            request.pop(key, None)
        else:
            request[key] = value
    guide(request, rng.choice([None, ENROLL]))

# ------------------------------------------------------------- preflight
def evidence(**changes):
    values = {
        'kernel': (0, 'Linux'), 'docker_endpoint': (0, 'local-unix'),
        'docker_version': (0, '{"client":"29.9.2","server":"29.9.2"}'),
        'daemon_os': (0, 'linux'), 'daemon_platform': (0, '{"os":"Arch Linux","kernel":"6.18.3"}'),
        'daemon_security': (0, '["name=seccomp,profile=builtin","name=cgroupns"]'),
        'compose': (0, '2.30.3'), 'ipv4': (0, '2: eth0    inet 192.168.10.20/24 brd 192.168.10.255 scope global eth0\n'),
        'udp53': (0, ''), 'tcp53': (0, ''), 'nesting': (0, 'none'), 'machine_fingerprint': (0, 'a' * 64),
    }
    values.update(changes)
    return values


def block(values=None, sep='\t'):
    values = values if values is not None else evidence()
    lines = [check.BEGIN]
    for key, (code, value) in values.items():
        lines.append('CFHA1' + sep + key + sep + str(code) + sep + base64.b64encode(value.encode()).decode())
    return '\n'.join(lines + [check.END]) + '\n'


def preflight(output, mode=MISSING):
    value = {'output': output}
    if mode is not MISSING:
        value['mode'] = mode
    add('preflight', value)


def both(output):
    preflight(output)
    preflight(output, 'macvlan')


# The cases of test_docker_ha_preflight.py.
both(block())
both('root@nas:~# pasted command\n' + block() + '\nold mounts: /persistent/core\n')
for output in ['Linux\nDocker version 29.9.2\n', '', block().replace(check.END, ''), block() + block(),
               block().replace(check.BEGIN, 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V2_BEGIN'),
               block({k: v for k, v in evidence().items() if k != 'udp53'}),
               block().replace('CFHA1\tudp53\t0\t', 'CFHA1\tipv4\t0\t'),
               block().replace('CFHA1\tudp53\t0\t', 'CFHA1\tunexpected\t0\t'),
               block().replace('CFHA1\tudp53\t0\t', 'CFHA1\tudp53\t999\t'),
               block().replace('CFHA1\tudp53\t0\t', 'CFHA1\tudp53\t0\t!invalid!'),
               'CFHA1\ttcp53\t0\t\n' + block(), block(evidence(ipv4=(0, 'x' * (check.MAX_FIELD_BYTES + 1)))),
               block().replace('CFHA1\tudp53\t0\t', 'CFHA1\tudp53\t0\t/w=='),
               block(evidence(daemon_security=(0, '[' * 2000 + '0' + ']' * 2000))),
               block(evidence(daemon_platform=(0, '{"a":' * 1500 + '1' + '}' * 1500)))]:
    both(output)
for output in [None, [], 'x' * (check.MAX_OUTPUT_BYTES + 1), 5, {}, 'x' * check.MAX_OUTPUT_BYTES, '例' * 32768, '例' * 32769]:
    preflight(output)
for mode in [None, 'bridge', 1, '', 'HOST', ['host']]:
    preflight(block(), mode)
for change in [{'kernel': (0, 'Darwin')}, {'docker_endpoint': (0, 'remote')},
               {'docker_version': (1, 'Cannot connect to the Docker daemon')}, {'docker_version': (127, 'docker: command not found')},
               {'daemon_os': (0, 'windows')}, {'daemon_platform': (0, '{"os":"Docker Desktop","kernel":"linuxkit"}')},
               {'daemon_security': (0, '["name=rootless"]')}]:
    both(block(evidence(**change)))
for field in ['ipv4', 'udp53', 'tcp53', 'docker_endpoint', 'daemon_security', 'kernel', 'daemon_os', 'compose', 'nesting',
              'machine_fingerprint', 'daemon_platform', 'docker_version']:
    both(block(evidence(**{field: (127, 'missing tool')})))
    both(block(evidence(**{field: (126, '')})))
    both(block(evidence(**{field: (255, '-')})))
for field, line in [('udp53', 'UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:*'), ('udp53', 'UNCONN 0 0 [::]:53 [::]:*'),
                    ('tcp53', 'LISTEN 0 4096 127.0.0.54:53 0.0.0.0:*')]:
    both(block(evidence(**{field: (0, line)})))
for line in ['ss: not supported', 'UNCONN 0 0 127.0.0.53:53', 'LISTEN 0 1 *:53 *:*', 'udp UNCONN 0 0 *:53 *:*',
             'tcp LISTEN 0 1 *:53 *:*', 'UNCONN ² 0 *:53 *:*', 'UNCONN ١ 0 *:53 *:*', 'UNCONN 0 0 *:53 *:*\n\n  \n',
             'UNCONN　0　0　*:53　*:*', 'UNCONN 0 0 *:530 *:*', 'UNCONN 0 0 *:53 *:* extra',
             'UNCONN 0 0 *:53 *:*\nUNCONN 0 0 [::]:53 [::]:*', 'source=ss\nUNCONN 0 0 *:53 *:*', 'source=ss\n', 'source=ss',
             'UNCONN 0 0 *:53 *:*\x85UNCONN 0 0 *:53 *:*', 'UNCONN ① 0 *:53 *:*', 'UNCONN \U0001f100 0 *:53 *:*']:
    both(block(evidence(udp53=(0, line))))
    both(block(evidence(tcp53=(0, line.replace('UNCONN', 'LISTEN')))))
for version in [(127, ''), (0, '2.29.9'), (0, 'nonsense'), (0, 'v2.30.0'), (0, '2.30.0-desktop.1'), (0, ' 2.31.1 \n'),
                (0, 'vv2.30.0'), (0, '10.0.0'), (0, '2.30'), (0, '2.30.0.1'), (0, '２.30.0'), (0, '3.0.0+x'),
                (0, '2.' + '9' * 4301 + '.0'), (0, '9' * 4300 + '.0.0'), (0, '2.30.' + '1' * 5000), (0, '02.030.000'),
                (0, '2.30.0 '), (0, '2.30.0　'), (0, 'V2.30.0')]:
    both(block(evidence(compose=version)))
both(block(evidence(machine_fingerprint=(1, ''), nesting=(0, 'container'))))
both(block(evidence(ipv4=(0, '$(touch /tmp/should-not-exist)<script>alert(1)</script>'))))
both(block(evidence(ipv4=(0, '2: enp3s0 inet 192.168.10.20/24 scope global enp3s0\n3: docker0 inet 172.17.0.1/16 scope global docker0\n'))))
both(block(evidence(ipv4=(0, ''.join(f'{n+1}: eth{n} inet 192.168.10.{n+1}/24 scope global eth{n}\n' for n in range(10))))))
both(block(evidence(ipv4=(0, f'2: {"eth<script>" + chr(27) + "[31m" + "x" * 100} inet 192.168.10.20/24 scope global interface\n'))))
synology = Path('server/internal/installer/testdata/docker_ha_preflight_synology_reconstructed.txt').read_text()
both(synology)
both('\n'.join(line + '\\' for line in synology.splitlines()) + '\n')
for separator in [' ', '   ', '\t ', ' \t']:
    output = block().replace('\t0\t\n', '\t0\t-\n').replace('\t', separator)
    both(output)
    both(output.replace('CFHA1' + separator + 'udp53' + separator + '0' + separator + '-',
                        'CFHA1' + separator + 'udp53' + separator + '127'))
for row in ['CFHA1 udp53 0', 'CFHA1 udp53 0   ', 'CFHA1\tudp53\t0', 'CFHA1 udp53 0 -', 'CFHA1 udp53 0 --',
            'CFHA1 udp53 0 -\\\\', 'CFHA1 udp53 0 \\', 'CFHA1 udp53 0 c291\\\ncmNl', 'CFHA1 udp53 0 !!!!\\', 'CFHA1 udp53 00 -',
            'CFHA1 udp53 0255 -', 'CFHA1 udp53 255 -', 'CFHA1 udp53 256 -', 'CFHA1 udp53 1000 -', 'CFHA1udp53 0 -',
            'CFHA1 UDP53 0 -', 'CFHA1 udp53 0 QUJD=', 'CFHA1 udp53 0 QUJ', 'CFHA1 udp53 0 QQ', 'CFHA1 udp53 0 QQ=',
            'CFHA1 udp53 0 Q===', 'CFHA1 udp53 0 QR==', 'CFHA1 udp53 0 Q', 'CFHA1 udp53 0 =', 'CFHA1 udp53 0 ==',
            'CFHA1 udp53 0 QUJDRA', 'CFHA1 udp53 0 QUJDRA=', '  CFHA1 udp53 0 -', 'CFHA1 udp53 0 - ', 'CFHA1 udp53 0\t-\t',
            'CFHA1 udp53 0 -\\', 'CFHA1 udp53 0 -  \\  ', 'CFHA1 udp53 0 7w==', 'CFHA1 udp53 0 AA==', 'CFHA1 udp53 0 7aCA']:
    output = block().replace('CFHA1\tudp53\t0\t', row)
    both(output)
output = block().replace('\t0\t\n', '\t0\t-\n').replace('\t', '  ')
for changed in (output.replace('tcp53', 'udp53'), output.replace('tcp53', 'unexpected'), output + 'CFHA1 udp53 127\n'):
    both(changed)
for proto, rows in [('udp', ''), ('tcp', ''), ('udp', 'udp 4 0100007F:0035 07\n'),
                    ('udp', 'udp 6 00000000000000000000000000000000:0035 01\n'), ('tcp', 'tcp 4 00000000:0035 0A\n'),
                    ('tcp', 'tcp 4 00000000:0035 01\n'), ('udp', 'udp 4 0100007f:0035 07\n'), ('udp', 'udp  4 0100007F:0035 07\n'),
                    ('udp', 'udp 4 0100007F:0035 07 \n'), ('udp', 'udp 4 0100007F:0035 07\n\n'), ('tcp', 'udp 4 00000000:0035 0A\n')]:
    payload = f'source=proc-net-v1\ntables={proto},{proto}6\n' + rows
    both(block(evidence(**{proto + '53': (0, payload)})))
for payload in ['source=proc-net-v1', 'source=proc-net-v1\ntables=udp', 'source=proc-net-v1\ntables=tcp,tcp6',
                'source=proc-net-v1\ntables=udp,udp6\nudp 4 00000000:0035 FF', 'source=proc-net-v1\ntables=udp,udp6\nudp 6 00000000:0035 07',
                'source=proc-net-v1\ntables=udp,udp6\nudp 4 00000000:0036 07', 'source=unknown', 'source=', 'source=proc-net-v1\r\ntables=udp,udp6',
                ' source=ss\n', 'source=proc-net-v1\ntables=udp,udp6 udp 4 00000000:0035 07']:
    both(block(evidence(udp53=(0, payload))))
# ipv4 variety.
for line in ['2: eth0 inet 192.168.10.20 scope global eth0', '2: eth0 inet 192.168.10.20/255.255.255.0 scope x',
             '2: eth0 inet 192.168.10.20/0.0.0.255 scope x', '2: eth0 inet 192.168.10.20/0.255.0.255 scope x',
             '2: eth0 inet 192.168.10.20/33 scope x', '2: eth0 inet 192.168.10.20/024 scope x', '2: eth0 inet 192.168.10.20/ scope x',
             '2: eth0 inet 192.168.10.20/24/1 scope x', '2: eth0 inet 127.0.0.1/8 scope host lo', '2: eth0 inet 169.254.3.4/16 scope x',
             '2: eth0 inet 0.0.0.0/0 scope x', '2: eth0 inet 224.0.0.1/4 scope x', '2: eth0 inet 250.1.1.1/4 scope x',
             '2: eth0 inet 192.168.10.20/24', '2: eth0 inet 192.168.10.20/24 ', '٢: eth0 inet 10.0.0.1/8 scope x',
             '2:eth0 inet 10.0.0.1/8 scope x', '2: eth0  inet  10.0.0.1/8  x', '2: eth0　inet　10.0.0.1/8　x',
             '2: eth0 inet6 fe80::1/64 scope link', '2: 网卡 inet 10.0.0.1/8 scope x', '2: ' + 'n' * 60 + ' inet 10.0.0.1/8 x',
             '2: ' + 'n' * 48 + ' inet 10.0.0.1/8 x', '2: <>&"\' inet 10.0.0.1/8 x', '2: eth0 inet 10.0.0.1/8x scope',
             '2: eth0 inet 010.0.0.1/8 scope x', '2: eth0 inet 10.0.0.1/' + '2' * 5000 + ' scope x', 'garbage',
             '   ', '2: eth0 inetx 10.0.0.1/8 scope', '2: eth0 inet 10.0.0.1/8\x1fscope', '2: e:t@h.0-_ inet 10.0.0.1/8 x',
             '2: eth0 inet 10.0.0.1/8 scope x\n\n3: eth1 inet 10.0.0.2/8 scope x', '2: eth0 inet 10.0.0.1/+8 scope x']:
    both(block(evidence(ipv4=(0, line))))
# version/platform/security JSON variety.
for version in ['{"client":"29.9.2","server":"29.9.2"}', '{}', '[]', '{"client":"29.9.2"}', '{"client":"29.9.2","server":29}',
                '{"client":"v29.9.2","server":"29.9.2"}', '{"client":"29.9.2-ce","server":"29.9.2+dfsg"}', 'null',
                '﻿{"client":"1.2.3","server":"1.2.3"}', '{"client":"1.2.3","server":"1.2.3"} ', ' {"client":"1.2.3","server":"1.2.3"}',
                '{"client":"1.2.3","server":"1.2.3","client":"x"}', '{"client":"1.2.3","server":"1.2"}', 'NaN', '{"client":"1.2.3",',
                '{"client":"１.2.3","server":"1.2.3"}', '{"client":"1.2.3\\n","server":"1.2.3"}', '1' * 5000]:
    both(block(evidence(docker_version=(0, version))))
    both(block(evidence(docker_version=(126, version))))
for platform in ['{"os":"Arch Linux","kernel":"6.18.3"}', '{"os":" ","kernel":"6"}', '{"os":"x","kernel":""}', '{"os":"x"}',
                 '{"os":"DOCKER DESKTOP","kernel":"6"}', '{"os":"x","kernel":"6.6-LinuxKit"}', '{"os":"docKer desktop","kernel":"6"}',
                 '{"os":"docker-desktop","kernel":"6"}', '[]', '{"os":1,"kernel":"6"}', '{"os":"　","kernel":"6"}', '{}']:
    both(block(evidence(daemon_platform=(0, platform))))
for security in ['[]', '["name=rootless"]', '["name=ROOTLESS"]', '[1]', '{}', '["x"]' , '[' + ','.join(['"x"'] * 33) + ']',
                 '[' + ','.join(['"x"'] * 32) + ']', '["rootİless"]', 'null', '"rootless"']:
    both(block(evidence(daemon_security=(0, security))))
for kernel in ['Linux', ' Linux\n', 'linux', 'Linux　', 'Linux\x00', '']:
    both(block(evidence(kernel=(0, kernel))))
for endpoint in ['local-unix', 'remote', ' remote ', 'local-unix\n', 'LOCAL-UNIX', '']:
    both(block(evidence(docker_endpoint=(0, endpoint))))
for nested in ['none', 'none\n', 'container', '', 'None']:
    both(block(evidence(nesting=(0, nested))))
for fingerprint in ['a' * 64, 'A' * 64, 'a' * 63, 'a' * 65, 'a' * 64 + '\n', 'g' * 64, '']:
    both(block(evidence(machine_fingerprint=(0, fingerprint))))
# Whole-output transformations.
for transform in [lambda s: s.replace('\n', '\r\n'), lambda s: s.replace('\n', '\r'), lambda s: s.replace('\n', ' '),
                  lambda s: s.replace('\n', '\x85'), lambda s: s.replace('\n', '\x0b'), lambda s: '﻿' + s,
                  lambda s: s.replace(check.BEGIN, '  ' + check.BEGIN + '  '), lambda s: s.replace(check.END, check.END + '　'),
                  lambda s: s.replace(check.BEGIN, check.BEGIN + 'X'), lambda s: s[:len(s) // 2], lambda s: s[:-5],
                  lambda s: s.replace('\n', '\n\n'), lambda s: '例\U0001f600\n' + s + '\U0001f600',
                  lambda s: s + 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_NOTE\n', lambda s: s + ' CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_NOTE\n',
                  lambda s: s + 'CFHA1\n', lambda s: s + 'CFHA1x\n', lambda s: s + '\tCFHA1\tfoo\n', lambda s: s.replace('\r', ''),
                  lambda s: s.replace('CFHA1\t', 'CFHA1\\\n'), lambda s: s.replace('\n', '\\\n'), lambda s: s.replace('\n', ' \\ \n'),
                  lambda s: check.END + '\n' + s.replace(check.END, ''), lambda s: s.replace('\n', '\r\r\n')]:
    both(transform(block()))
# Seeded random field values and line damage.
FIELD_VALUES = {
    'kernel': ['Linux', 'Darwin', '', 'Linux\n'], 'docker_endpoint': ['local-unix', 'remote', 'x'],
    'docker_version': ['{"client":"29.9.2","server":"29.9.2"}', '{}', 'x', '{"client":"1.0.0","server":"1.0"}'],
    'daemon_os': ['linux', 'windows', ''], 'daemon_platform': ['{"os":"Arch Linux","kernel":"6.18.3"}', '{"os":"Docker Desktop","kernel":"x"}', '[]'],
    'daemon_security': ['[]', '["name=rootless"]', '[1]', 'x'], 'compose': ['2.30.3', '2.29.0', 'v2.40.1', ''],
    'ipv4': ['', '2: eth0 inet 10.0.0.2/8 scope global eth0\n', 'junk', '2: lo inet 127.0.0.1/8 x\n3: e inet 192.0.2.4/24 y\n'],
    'udp53': ['', 'UNCONN 0 0 *:53 *:*', 'source=proc-net-v1\ntables=udp,udp6\n', 'source=x'],
    'tcp53': ['', 'LISTEN 0 1 *:53 *:*', 'source=proc-net-v1\ntables=tcp,tcp6\ntcp 4 00000000:0035 0A\n'],
    'nesting': ['none', 'container'], 'machine_fingerprint': ['a' * 64, '', 'b' * 64]}
CODES = [0, 0, 0, 0, 1, 2, 125, 126, 127, 255]
for _ in range(700):
    values = {key: (rng.choice(CODES), rng.choice(pool)) for key, pool in FIELD_VALUES.items()}
    keys = list(values)
    rng.shuffle(keys)
    values = {key: values[key] for key in keys}
    output = block(values, rng.choice(['\t', '\t', ' ', '  ']))
    lines = output.split('\n')
    for _ in range(rng.choice([0, 0, 0, 1, 2])):
        i = rng.randrange(len(lines))
        op = rng.choice(['drop', 'dup', 'cut', 'ws', 'bs', 'crlf', 'junk'])
        if op == 'drop':
            lines.pop(i)
        elif op == 'dup':
            lines.insert(i, lines[i])
        elif op == 'cut':
            lines[i] = lines[i][:rng.randrange(len(lines[i]) + 1)]
        elif op == 'ws':
            lines[i] = rng.choice([' ', '\t', ' ', '　']) + lines[i] + rng.choice(['', ' ', '\t'])
        elif op == 'bs':
            lines[i] += '\\'
        elif op == 'crlf':
            lines[i] += '\r'
        else:
            lines.insert(i, rng.choice(['$ ', '例子', 'CFHA1', '\U0001f600', 'CONFIGFLOW_DOCKER_HA_PREFLIGHT_V1_X']))
        if not lines:
            break
    preflight('\n'.join(lines), rng.choice([MISSING, 'host', 'macvlan']))

# base64.b64decode(payload, validate=True) of payloads matching the row regex.
for length in range(0, 10):
    for pads in range(0, 3):
        for _ in range(6):
            add('b64', ''.join(rng.choice('QUJDRA7wa/+9') for _ in range(length)) + '=' * pads)

# ------------------------------------------------------------------ run
out = sys.stdout
for number, (kind, value) in enumerate(cases):
    record = {'id': number, 'kind': kind, 'input': value}
    try:
        if kind == 'b64':
            result = base64.b64decode(value, validate=True).hex()
        elif kind == 'guide':
            result = generate_guide(copy.deepcopy(value['request']), enrollment=value['enrollment'])
        else:
            args = [value['output']] + ([value['mode']] if 'mode' in value else [])
            result = check.parse_report(*args)
        record['ok'] = True
        if kind == 'guide':
            # Guide answers are large: one digest per top-level key (of its
            # json.dumps(sort_keys=True, ensure_ascii=False, compact) text).
            record['digests'] = {key: hashlib.sha256(json.dumps(item, sort_keys=True, ensure_ascii=False,
                                                                separators=(',', ':')).encode()).hexdigest()[:24]
                                 for key, item in result.items()}
        else:
            record['result'] = result
    except Exception as error:  # noqa: BLE001 - the class is the reference
        record['ok'] = False
        record['error'] = {'class': type(error).__name__, 'value_error': isinstance(error, ValueError), 'message': str(error)}
    out.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + '\n')
sys.stderr.write(f'{len(cases)} cases, sha256 of inputs '
                 f'{hashlib.sha256(json.dumps([c[1] for c in cases], sort_keys=True).encode()).hexdigest()[:16]}\n')
