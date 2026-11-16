"""Random installer inputs rendered by the Python generators (A08 review).

Complements gen_matrix.py with seeded random combinations: shell metacharacters,
newlines and other line breaks, Unicode (CJK, emoji, full-width, NFKC traps),
very long values, boundary and malformed ports, IPv4/IPv6/hostnames, URLs with
credentials, fragments, brackets and percent-encoding, odd paths and subnets.

Writes gzip-compressed JSON lines in the matrix format ({"id", "fn", "kwargs",
"ok", "sha256", "length", "error"}). Run from the repository root on the test
server:

    ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_fuzz.py \
        | gzip -9n > server/internal/installer/testdata/fuzz.jsonl.gz

TestFuzzCorpusMatchesPython replays it with the Go generators.
"""
import hashlib
import json
import random
import sys

sys.path.insert(0, '.')
from backend.agents import gateway_install_script as gateway  # noqa: E402
from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script  # noqa: E402
from backend.agents.install_output import with_install_output, with_root_privileges  # noqa: E402
from backend.agents.install_script import generate_docker_agent_compose, generate_docker_agent_run  # noqa: E402

SEED = 20261116
PER_FUNCTION = int(sys.argv[1]) if len(sys.argv) > 1 else 2500
rng = random.Random(SEED)

FUNCTIONS = {
    'go_agent': generate_go_agent_install_script,
    'aio': generate_go_aio_install_script,
    'gateway': gateway.generate_gateway_install_script,
    'compose': generate_docker_agent_compose,
    'run': generate_docker_agent_run,
    'with_install_output': with_install_output,
    'with_root_privileges': with_root_privileges,
}

META = ["'", '"', '$', '`', ';', '|', '&', '\\', '\n', '\r', '\t', '\x00', '\x7f', '\x0b', '\x0c', '\x1c', '\x85',
        ' ', ' ', ' ', '　', '﻿', ' ', '#', '*', '?', '~', '!', '<', '>', '(', ')', '{', '}',
        '[', ']', '%', '+', '=', ':', ',', '.', '/', '@', '-', '_']
ATOMS = ['', 'a', 'Z', '0', '9', 'mihomo', 'oxidns', 'mosdns', 'aio', 'go', 'shell', 'eth0', 'agent', 'My-Agent',
         '$(id)', '`id`', '${HOME}', "a'b", 'a"b', "'\"'\"'", '{config_json}', '{agent_port}', '{{agent_name}}',
         '{installation_action}', '{aio_script}', '{binary_download_url_shell}', 'é', 'ü', '例子', '测试', '😀',
         'İ', 'ß', 'ſ', 'Ⅻ', '１２', '٣', '⁇', '℀', '／', '：', '＠', '﹖', 'K', '%41', '%2F',
         '%0a', '%zz', 'systemctl restart mihomo', 'rc-service oxidns restart', '/etc/mihomo/config.yaml',
         '/usr/local/bin/mihomo', '/opt/configflow-agent', 'configflow-agent-mihomo', '.service', 'x.service',
         'EOF', 'CONFIGFLOW_EOF', "'CONFIGFLOW'", '__CONFIGFLOW_INIT__', '-v', '--uninstall', '\\n', '\\\\']


def text(max_parts=6):
    parts = []
    for _ in range(rng.randint(0, max_parts)):
        roll = rng.random()
        if roll < 0.45:
            parts.append(rng.choice(ATOMS))
        elif roll < 0.85:
            parts.append(rng.choice(META))
        else:
            parts.append(''.join(chr(rng.choice([rng.randint(32, 126), rng.randint(0x80, 0x2fff),
                                                 rng.randint(0x3000, 0xd7ff), rng.randint(0xe000, 0xffff),
                                                 rng.randint(0x10000, 0x10ffff)]))
                                 for _ in range(rng.randint(1, 4))))
    value = ''.join(parts)
    if rng.random() < 0.02:
        value = value * rng.randint(200, 1200)
    return value


def safe_word():
    alphabet = 'abcXYZ019_-.@'
    return ''.join(rng.choice(alphabet) for _ in range(rng.randint(1, 12)))


def name():
    roll = rng.random()
    if roll < 0.5:
        return ''.join(rng.choice('abcABC019_-') for _ in range(rng.randint(1, 20)))
    if roll < 0.6:
        return safe_word() + '\n'
    if roll < 0.65:
        return 'a' * rng.randint(100, 5000)
    return text(3)


def digits_text():
    roll = rng.random()
    if roll < 0.3:
        return str(rng.randint(0, 70000))
    choices = [lambda: ' ' + str(rng.randint(1, 65535)) + ' ', lambda: '0' * rng.randint(1, 5) + str(rng.randint(1, 999)),
               lambda: '+' + str(rng.randint(1, 65535)), lambda: '-' + str(rng.randint(0, 99)),
               lambda: '１２３', lambda: '٨٠٨٠', lambda: '8_080', lambda: '8__0', lambda: '_80', lambda: '80_',
               lambda: '9' * rng.choice([4300, 4301, 5000]), lambda: '1' * 4301 + 'x', lambda: '0x50', lambda: '',
               lambda: ' ', lambda: ' 443　', lambda: '53', lambda: '1053', lambda: '65535', lambda: '65536',
               lambda: '8080\n', lambda: 'x' * rng.randint(150, 400), lambda: '1e3', lambda: '8080.0',
               lambda: '\U0001e4f1', lambda: '\U00011f52', lambda: text(2)]
    return rng.choice(choices)()


def port_value(allow_str=True, allow_bool=False):
    roll = rng.random()
    if allow_bool and roll < 0.05:
        return rng.choice([True, False])
    if roll < 0.45 or not allow_str:
        return rng.choice([rng.randint(-5, 70000), rng.randint(1, 65535), 0, 1, 53, 1053, 65535, 65536, 1023, 1024,
                           10 ** rng.randint(5, 40), -(10 ** rng.randint(1, 30))])
    return digits_text()


def octet():
    return rng.choice([str(rng.randint(0, 255)), str(rng.randint(0, 999)), '0' + str(rng.randint(0, 99)), '', 'x',
                       '٣', '1' * rng.randint(4, 6), str(rng.randint(0, 255)), str(rng.randint(0, 255))])


def ipv4():
    roll = rng.random()
    if roll < 0.4:
        return '.'.join(str(rng.choice([rng.randint(0, 255), 192, 10, 127, 169, 224, 240, 255, 0])) for _ in range(4))
    if roll < 0.8:
        return '.'.join(octet() for _ in range(rng.choice([3, 4, 4, 4, 5])))
    return rng.choice(['', ' 1.2.3.4', '1.2.3.4 ', '::1', 'fe80::1', '1.2.3.4/32', '1.2.3.4%x', '١.٢.٣.٤', text(2)])


def ipv6_host():
    groups = [rng.choice(['0', '1', 'ff', 'fe80', 'FFFF', '00000', 'g', '', '1.2.3.4', '01.2.3.4', 'abcd'])
              for _ in range(rng.randint(1, 9))]
    host = ':'.join(groups)
    if rng.random() < 0.3:
        host = rng.choice(['::', '::1', '1::', '1:2:3:4:5:6:7::', '::ffff:1.2.3.4', ':1::2', '1::2::3', 'fe80::1',
                           '1:2:3:4:5:6:7:8', '1:2:3:4::5:6:7:8'])
    if rng.random() < 0.3:
        host += rng.choice(['%', '%eth0', '%a%b', '%25eth0', '%é', '%%'])
    return host


def host():
    roll = rng.random()
    if roll < 0.25:
        return ipv4()
    if roll < 0.45:
        return '[' + ipv6_host() + ']'
    if roll < 0.5:
        return rng.choice(['[v1.x]', '[vz.x]', '[v.x]', '[1.2.3.4]', '[]', '[', ']', 'a[::1]', '[::1]x', '[::1]]',
                           '[[::1]]'])
    if roll < 0.75:
        return rng.choice(['s', 'S', 'panel.test', 'config.example.test', '例子.测试', 'ｓ', 'a℀b', 'x／y',
                           'h﹖', 'xn--fsq', 'host_name', '-', '.', ''])
    return text(3)


def url(allow_empty=True):
    if allow_empty and rng.random() < 0.03:
        return ''
    if rng.random() < 0.1:
        return text(5)
    scheme = rng.choice(['http', 'https', 'HTTP', 'HtTpS', 'ftp', 'file', '', 'h1+x', '1http', 'http '])
    out = scheme + rng.choice(['://', '://', '://', ':', ':/', ':///', ''])
    if rng.random() < 0.2:
        out += rng.choice(['user@', 'u:p@', ':@', '@', ':p@', 'u:@', 'a@b@', 'us%40er@', '[::1]@'])
    out += host()
    if rng.random() < 0.4:
        out += rng.choice([':', ':0', ':80', ':5001', ':65535', ':65536', ':99999', ':abc', ':-1', ':８０', '::'])
    if rng.random() < 0.5:
        out += rng.choice(['/', '//', '/path', '/a b', "/it's", '/$(id)', '/%2F%0a', '/{config_json}', '/../x', '/中文'])
    if rng.random() < 0.15:
        out += '?' + rng.choice(['a=1&b=2', '', 'x=$(id)', 'q=%zz'])
    if rng.random() < 0.15:
        out += '#' + rng.choice(['', 'frag', '#', 'a b'])
    if rng.random() < 0.15:
        out += rng.choice(['/', '///', ' ', '\n', '\x7f', '\t'])
    return out


def path():
    roll = rng.random()
    if roll < 0.2:
        return rng.choice(['/etc/mihomo/config.yaml', '/etc/oxidns/config.yaml', '/etc/m/c.yaml', '/etc/o/c.yaml'])
    if roll < 0.3:
        return text(4)
    components = [rng.choice(['', '.', '..', 'etc', 'opt', 'configflow-agent', 'configflow-agentx', 'mihomo', 'oxidns',
                              'config.yaml', 'a b', "it's", '中文', 'x\n', 'y\x7f', '$(id)', 'usr', 'bin', 'm', 'o'])
                  for _ in range(rng.randint(0, 5))]
    lead = rng.choice(['/', '/', '//', '///', ''])
    tail = rng.choice(['', '', '/', '/.'])
    return lead + '/'.join(components) + tail


def unit():
    return rng.choice(['mihomo', 'oxidns', 'mosdns', 'mihomo.service', 'oxidns.service', 'clash', 'm@x.service', 'o_y',
                       '', '.service', '-bad', '_ok', 'configflow-agent-mihomo', 'configflow-x', 'a.service.service',
                       'bad unit', 'é', safe_word(), text(2)])


def binary():
    return rng.choice(['mihomo', 'oxidns', '/usr/local/bin/mihomo', '/usr/local/bin/oxidns', '', 'm\tx', "my bin",
                       "o'x", '/opt/configflow-agent/m', '/opt/x/../configflow-agent/m', '/opt//configflow-agent',
                       path(), text(3)])


def maybe(kwargs, key, make, p=0.5):
    if rng.random() < p:
        kwargs[key] = make()


def go_agent():
    k = {'server_url': url(), 'agent_name': name() if rng.random() < 0.3 else 'node-1',
         'service_type': rng.choice(['mihomo', 'mihomo', 'oxidns', 'oxidns', 'mosdns', 'aio', '', 'Mihomo'])}
    maybe(k, 'agent_port', lambda: port_value(), 0.6)
    maybe(k, 'agent_ip', lambda: rng.choice([ipv4(), text(3), url()]), 0.5)
    maybe(k, 'config_path', lambda: rng.choice([None, '', '/etc/' + k['service_type'] + '/config.yaml', path(), text(3)]))
    maybe(k, 'restart_command', lambda: rng.choice([None, '', 'systemctl restart ' + k['service_type'],
                                                    'rc-service ' + k['service_type'] + ' restart',
                                                    'systemctl restart mihomo.service', text(4)]))
    maybe(k, 'binary_download_url', lambda: url() + rng.choice(['', '/', '///', '/api/agents/download']), 0.8)
    maybe(k, 'service_manager', lambda: rng.choice(['', 'systemd', 'openrc', 'command', 'runit', 'Systemd']), 0.4)
    maybe(k, 'service_unit', unit, 0.4)
    maybe(k, 'service_binary', binary, 0.4)
    commands = rng.random() < 0.35
    for key in ('stop_command', 'start_command', 'status_command'):
        if commands or rng.random() < 0.1:
            k[key] = rng.choice([text(5), 'stop x', "status 'z'", ''])
    for key in ('health_url', 'health_dns_address', 'health_dns_name'):
        maybe(k, key, lambda: rng.choice([text(4), url(), '127.0.0.1:53', '例子.test']), 0.25)
    maybe(k, 'deployment_health_timeout', lambda: rng.choice([30, 0, -5, 10 ** 25, rng.randint(-100, 10000)]), 0.3)
    maybe(k, 'install_core', lambda: rng.random() < 0.5, 0.6)
    maybe(k, 'manage_ipv4_forwarding', lambda: rng.choice([None, True, False]), 0.6)
    maybe(k, 'prepare_dns_ha', lambda: rng.random() < 0.5, 0.4)
    return k


def aio():
    k = {'server_url': url(), 'agent_name': name() if rng.random() < 0.3 else 'box'}
    maybe(k, 'agent_ip', lambda: rng.choice([ipv4(), text(3)]))
    maybe(k, 'binary_download_url', lambda: url() + rng.choice(['', '/', '///']), 0.8)
    maybe(k, 'mihomo_agent_port', lambda: port_value(allow_bool=True), 0.5)
    maybe(k, 'oxidns_agent_port', lambda: port_value(allow_bool=True), 0.5)
    maybe(k, 'mihomo_config_path', path, 0.4)
    maybe(k, 'oxidns_config_path', path, 0.4)
    maybe(k, 'mihomo_service_unit', unit, 0.3)
    maybe(k, 'oxidns_service_unit', unit, 0.3)
    maybe(k, 'mihomo_service_binary', binary, 0.3)
    maybe(k, 'oxidns_service_binary', binary, 0.3)
    maybe(k, 'install_core', lambda: rng.random() < 0.5, 0.5)
    maybe(k, 'non_interactive', lambda: rng.random() < 0.5, 0.3)
    maybe(k, 'verified_agent_path', lambda: rng.choice(['$GATEWAY_STAGE/configflow-agent', '', '/tmp/a', text(2)]), 0.25)
    if rng.random() < 0.3:
        k['installation_id'] = rng.choice(['i' * 16, 'g' * 80, 'g' * 81, 'short', 'a.b' * 6, safe_word() * 3, ''])
        k['installation_token'] = rng.choice(['t' * 16, 't.k-_' * 4, 't' * 512, 't' * 513, 't t' * 8, ''])
    maybe(k, 'manage_ipv4_forwarding', lambda: rng.random() < 0.5, 0.4)
    maybe(k, 'manage_tun', lambda: rng.random() < 0.5, 0.4)
    maybe(k, 'installation_description', lambda: text(5), 0.3)
    maybe(k, 'tun_prepare_assume_enabled', lambda: rng.random() < 0.5, 0.2)
    return k


SHA_A = 'a' * 64
SHA_B = '0123456789abcdef' * 4


def gateway_kwargs():
    server = url()
    k = {'server_url': server, 'agent_name': name() if rng.random() < 0.2 else 'gw'}
    maybe(k, 'agent_ip', ipv4, 0.5)
    maybe(k, 'binary_download_url', lambda: rng.choice(['', url(), server + '/api/agents/download/']), 0.3)
    maybe(k, 'mihomo_agent_port', lambda: port_value(allow_bool=True), 0.2)
    maybe(k, 'oxidns_agent_port', lambda: port_value(allow_bool=True), 0.2)
    if rng.random() < 0.5:
        k['enrollment_url'] = rng.choice([server + '/api/agents/gateway-installations/x/enroll', url(), '',
                                          server.upper() + '/e', server + '#f'])
        k['enrollment_token'] = rng.choice(['tok-' + 'x' * 20, 'x' * 16, 'x' * 512, 'x' * 513, 'short', 'a b' * 8, '',
                                            'x' * 15 + '\n'])
    if rng.random() < 0.95:
        checksums = {}
        for arch in rng.sample(['linux-amd64', 'linux-arm64', 'linux-armv7', ''], rng.randint(0, 3)):
            checksums[arch] = rng.choice([SHA_A, SHA_B, SHA_A.upper(), 'abc', SHA_A + '\n', ''])
        if rng.random() < 0.7:
            checksums = {'linux-amd64': SHA_A, 'linux-arm64': SHA_B} if rng.random() < 0.5 else {'linux-amd64': SHA_A}
        k['agent_checksums'] = checksums
    maybe(k, 'installation_id', lambda: rng.choice(['g' * 20, '', 'short', 'g' * 81]), 0.3)
    maybe(k, 'full_tun', lambda: rng.random() < 0.5, 0.5)
    return k


def docker():
    k = {'server_url': url()}
    maybe(k, 'agent_name', name, 0.5)
    maybe(k, 'agent_ip', lambda: rng.choice([ipv4(), text(3), '192.168.1.53', '192.168.1.53']), 0.6)
    maybe(k, 'data_dir', lambda: rng.choice([path(), text(4), './agent_data', '/srv/cf data', '']), 0.5)
    maybe(k, 'network_mode', lambda: rng.choice(['host', 'bridge', 'macvlan', 'my net', '', text(2)]), 0.7)
    for key in ('enable_mihomo', 'enable_mosdns', 'enable_oxidns'):
        maybe(k, key, lambda: rng.random() < 0.5, 0.7)
    for key in ('mihomo_port', 'mosdns_port', 'oxidns_port'):
        maybe(k, key, lambda: port_value(allow_str=False), 0.4)
    if rng.random() < 0.5:
        k['enable_dns_ha'] = rng.random() < 0.8
        if rng.random() < 0.6:
            k['enable_mihomo'], k['enable_mosdns'], k['enable_oxidns'] = False, False, True
        maybe(k, 'ha_parent', lambda: rng.choice(['eth0', 'enp1s0', 'a' * 15, 'a' * 16, '_x', '.x', 'e th', '', 'é',
                                                   safe_word(), text(2)]), 0.8)
        prefix = rng.choice(['/24', '/16', '/30', '/31', '/32', '/0', '/255.255.255.0', '/0.0.0.255', '/255.0.255.0',
                             '/024', '/', '', '/24/1', '/ 24', '/２４', '/33', '/-1', '/+24', '/2_4', '/0x18'])
        maybe(k, 'ha_subnet', lambda: rng.choice(['192.168.1.0', '10.0.0.0', '192.168.1.5', ipv4()]) + prefix, 0.8)
        maybe(k, 'ha_gateway', lambda: rng.choice(['', '192.168.1.1', '192.168.1.53', '192.168.1.255', ipv4()]), 0.6)
        maybe(k, 'ha_host_id_path', lambda: rng.choice(['/etc/machine-id', 'etc/machine-id', '/a,b', '/a:b', '/a\nb',
                                                        '/with space', "/it's", '/$(id)', path(), text(2)]), 0.5)
    return k


def script_text():
    lines = [rng.choice(['#!/bin/sh', '# ConfigFlow x', '# ConfigFlow', '#ConfigFlow', 'echo hi', '', '  ', text(3)])
             for _ in range(rng.randint(1, 6))]
    out = ''
    for line in lines:
        out += line + rng.choice(['\n', '\n', '\r\n', '\r', '\x0b', '\x0c', '\x1c', '\x1d', '\x1e', '\x85', ' ',
                                  ' ', ''])
    return out + rng.choice(['', ' \t\n', '　', '\x85'])


GENERATORS = {
    'go_agent': go_agent,
    'aio': aio,
    'gateway': gateway_kwargs,
    'compose': docker,
    'run': docker,
    'with_install_output': lambda: {'script': script_text()},
    'with_root_privileges': lambda: {'script': script_text()},
}


def run(fn, kwargs):
    try:
        return True, FUNCTIONS[fn](**kwargs)
    except ValueError as exc:
        return False, 'ValueError: ' + str(exc)
    except Exception as exc:  # noqa: BLE001
        return False, type(exc).__name__ + ': ' + str(exc)


def url_focus():
    """URL parsing (urlsplit, brackets, IPv6, NFKC netloc check) through _http_url."""
    server = rng.choice(['http', 'https', 'HTTP', 'ftp']) + '://'
    if rng.random() < 0.2:
        server += rng.choice(['u@', ':@', 'u:p@', '[x]@', '%40@'])
    roll = rng.random()
    if roll < 0.6:
        server += '[' + ipv6_host() + ']'
    elif roll < 0.8:
        server += host()
    else:
        server += rng.choice(['ｓ', 'a⁇b', 'b℀', 'c︓', 'd＃', 'é', 'é:80', 'é@x', 'ª', '⑴'])
    server += rng.choice(['', '', ':80', ':', ':x', '/', '/p', '?q', '#f', '#'])
    k = {'server_url': server, 'agent_name': 'gw', 'agent_checksums': {'linux-amd64': SHA_A}}
    if rng.random() < 0.3:
        k['enrollment_url'] = rng.choice([server + '/e', server.replace('http', 'https', 1) + '/e', url()])
        k['enrollment_token'] = 'x' * 16
    return k


GENERATORS_EXTRA = [('gateway', url_focus)]

index = 0
for fn, make in [*GENERATORS.items(), *GENERATORS_EXTRA]:
    for _ in range(PER_FUNCTION):
        kwargs = make()
        if fn == 'with_install_output' and not kwargs['script']:
            kwargs['script'] = '#!/bin/sh\n'
        ok, result = run(fn, kwargs)
        record = {'id': index, 'fn': fn, 'kwargs': kwargs, 'ok': ok}
        if ok:
            data = result.encode('utf-8', 'surrogatepass')
            record['sha256'] = hashlib.sha256(data).hexdigest()
            record['length'] = len(data)
        else:
            record['error'] = result
        print(json.dumps(record, ensure_ascii=True))
        index += 1
