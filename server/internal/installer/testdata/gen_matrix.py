"""Render every installer generator for a broad input matrix with the Python code.

Writes one JSON object per line: {"id", "fn", "kwargs", "ok", "sha256", "length", "error"}
(and "output" with --full). Run from the repository root on the test server:

    ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_matrix.py > server/internal/installer/testdata/matrix.jsonl
    ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_matrix.py --full > /tmp/matrix-full.jsonl

The Go test TestRenderMatrixMatchesPython replays the same kwargs; set
INSTALLER_MATRIX_FULL=/tmp/matrix-full.jsonl to see the first differing line.
"""
import hashlib
import itertools
import json
import sys

sys.path.insert(0, '.')
from backend.agents import gateway_install_script as gateway  # noqa: E402
from backend.agents.core_install_script import generate_core_install_script  # noqa: E402
from backend.agents.go_install_script import generate_go_agent_install_script, generate_go_aio_install_script  # noqa: E402
from backend.agents.install_output import with_install_output, with_root_privileges  # noqa: E402
from backend.agents.install_script import generate_docker_agent_compose, generate_docker_agent_run  # noqa: E402

FULL = '--full' in sys.argv
SHA_A = 'a' * 64
SHA_B = '0123456789abcdef' * 4

FUNCTIONS = {
    'go_agent': generate_go_agent_install_script,
    'aio': generate_go_aio_install_script,
    'core': generate_core_install_script,
    'gateway': gateway.generate_gateway_install_script,
    'compose': generate_docker_agent_compose,
    'run': generate_docker_agent_run,
    'with_install_output': with_install_output,
    'with_root_privileges': with_root_privileges,
}

ODD_URLS = [
    'http://192.168.1.2:5001', 'https://config.example.test/', 'http://[fd00::2]:5001',
    "http://h/it's", 'https://cfg.test/path with space/', 'http://h/{config_json}',
    'http://h/$(id)`x`', 'http://例子.测试:8080', 'http://h/?a=1&b=2#frag', '',
]
NAMES = ['My-Agent', 'a', 'agent_1-x', 'A' * 80, 'bad name', 'ünï', 'x\n', '', '-', 'a.b']

cases = []


def add(fn, **kwargs):
    cases.append((fn, kwargs))


# with_install_output / with_root_privileges on odd scripts
for text in ['#!/bin/sh\necho hi\n', '#!/bin/sh\n# ConfigFlow marker\nbody\n\n\n', '#!/bin/sh',
             '#!/bin/sh\r\nx\r\n', '#!/bin/sh\n# ConfigFlow a\n# ConfigFlow b\nrest  \t\n', 'x # ConfigFlow y\nz\x0b\n',
             '#!/bin/sh\n　\n']:
    add('with_install_output', script=text)
    add('with_root_privileges', script=text)
add('with_root_privileges', script='')

add('core')
add('core', installer_forwarding=True)

# Single Go Agent: the form options
for service, install_core, prepare_ha, forwarding in itertools.product(
        ['mihomo', 'oxidns', 'mosdns', 'aio', ''], [False, True], [False, True], [None, True, False]):
    add('go_agent', server_url='http://10.0.0.2:5001', agent_name='node-1', service_type=service,
        agent_ip='10.0.0.9', binary_download_url='http://10.0.0.2:5001/api/agents/download',
        install_core=install_core, prepare_dns_ha=prepare_ha, manage_ipv4_forwarding=forwarding)
for url in ODD_URLS:
    for service in ['mihomo', 'oxidns']:
        add('go_agent', server_url=url, agent_name='n', service_type=service, agent_ip=url,
            binary_download_url=url + '/api/agents/download///', health_url=url)
for name in NAMES:
    add('go_agent', server_url='http://s', agent_name=name, service_type='mihomo')
for port in [8080, 1, 65535, 0, 65536, -1, 10 ** 30, '8080', ' 8080', '0080', '１２３', '80_80', 'abc', '', '1053', '53',
             '9' * 4301, '1' * 4400 + 'x', '8_0']:
    for install_core in [False, True]:
        for service in ['mihomo', 'oxidns']:
            add('go_agent', server_url='http://s', agent_name='n', service_type=service, agent_port=port,
                install_core=install_core)
for unit in ['mihomo', 'mihomo.service', 'my@unit.service', 'bad unit', '', '.service', 'clash-meta']:
    add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', service_unit=unit)
    add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', service_unit=unit, install_core=True)
for manager in ['', 'systemd', 'openrc', 'command', 'runit']:
    for restart in [None, '', 'systemctl restart mihomo', 'rc-service mihomo restart', 'systemctl restart mihomo.service',
                    'docker restart mihomo']:
        for commands in [('', '', ''), ('stop x', 'start y', "status 'z'")]:
            for forwarding in [None, True, False]:
                add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', service_manager=manager,
                    restart_command=restart, stop_command=commands[0], start_command=commands[1],
                    status_command=commands[2], manage_ipv4_forwarding=forwarding)
    add('go_agent', server_url='http://s', agent_name='n', service_type='oxidns', service_manager=manager,
        prepare_dns_ha=True, stop_command='a', start_command='b', status_command='c')
for config_path in [None, '', '/etc/mihomo/config.yaml', '/opt/x/config.yaml', 'relative.yaml', '/etc/oxidns/config.yaml']:
    for install_core in [False, True]:
        add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', config_path=config_path,
            install_core=install_core)
for binary in ['', 'mihomo', '/usr/local/bin/mihomo', '/opt/mihomo', "it's bin"]:
    for install_core in [False, True]:
        add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', service_binary=binary,
            install_core=install_core)
for timeout in [30, 0, -5, 10 ** 25]:
    add('go_agent', server_url='http://s', agent_name='n', service_type='oxidns', deployment_health_timeout=timeout,
        health_dns_address='127.0.0.1:53', health_dns_name='例子.test', health_url='http://127.0.0.1:9090/"x"')
for port in [8082, '8082', '53', 53, '0053', '65535']:
    add('go_agent', server_url='http://s', agent_name='ha', service_type='oxidns', agent_port=port,
        prepare_dns_ha=True, install_core=True)
    add('go_agent', server_url='http://s', agent_name='ha', service_type='oxidns', agent_port=port, prepare_dns_ha=True)
add('go_agent', server_url='http://s', agent_name='n', service_type='mihomo', agent_ip=' \x7f\x00"\\é😀')

# All-in-One
for install_core, forwarding, tun, non_interactive in itertools.product([False, True], [True, False], [True, False], [False, True]):
    add('aio', server_url='http://10.0.0.2:5001', agent_name='box', agent_ip='10.0.0.3',
        binary_download_url='http://10.0.0.2:5001/api/agents/download', install_core=install_core,
        manage_ipv4_forwarding=forwarding, manage_tun=tun, non_interactive=non_interactive)
for url in ODD_URLS:
    add('aio', server_url=url, agent_name='box', agent_ip=url, binary_download_url=url + '/')
for name in NAMES:
    add('aio', server_url='http://s', agent_name=name)
for ports in [(8080, 8082), ('8080', '8082'), ('8080', '8080'), ('0', '8082'), ('65536', '1'), (' 8080', '8082'),
              ('１', '2'), ('53', '8082'), ('8080', '1053'), ('0008080', '08082'), ('9' * 4301, '1'), (True, 2)]:
    for install_core in [False, True]:
        add('aio', server_url='http://s', agent_name='b', mihomo_agent_port=ports[0], oxidns_agent_port=ports[1],
            install_core=install_core)
for paths in [('/etc/mihomo/config.yaml', '/etc/oxidns/config.yaml'), ('/etc/a/c.yaml', '/etc/a/d.yaml'),
              ('//etc//mihomo/./config.yaml', '/etc/oxidns/config.yaml'), ('/etc/m/../c.yaml', '/etc/o/c.yaml'),
              ('etc/m.yaml', '/etc/o/c.yaml'), ('/etc/m/', '/etc/o/c.yaml'), ('/', '/etc/o/c.yaml'), ('/.', '/x/y'),
              ('/opt/configflow-agent/m.yaml', '/etc/o/c.yaml'), ('/opt//configflow-agent', '/etc/o/c.yaml'),
              ('/opt/configflow-agentx/m.yaml', '/etc/o/c.yaml'), ('/etc/m/c\n.yaml', '/etc/o/c.yaml'),
              ('/etc/m/c.yaml', '/etc/m/c.yaml'), ('/etc/m/c.yaml', '//etc/m/c.yaml'), ("/etc/it's/c.yaml", '/etc/o p/c.yaml'),
              ('/etc/m/c.yaml\x7f', '/etc/o/c.yaml'), ('/etc/中文/c.yaml', '/etc/o/c.yaml')]:
    for install_core in [False, True]:
        add('aio', server_url='http://s', agent_name='b', mihomo_config_path=paths[0], oxidns_config_path=paths[1],
            install_core=install_core)
for units in [('mihomo', 'oxidns'), ('mihomo.service', 'oxidns.service'), ('mihomo', 'mihomo.service'),
              ('configflow-agent-mihomo', 'oxidns'), ('.service', 'oxidns'), ('-bad', 'oxidns'), ('m@x.service', 'o_y'),
              ('', 'oxidns'), ('clash', 'oxidns')]:
    for install_core in [False, True]:
        add('aio', server_url='http://s', agent_name='b', mihomo_service_unit=units[0], oxidns_service_unit=units[1],
            install_core=install_core)
for binaries in [('mihomo', 'oxidns'), ('/usr/local/bin/mihomo', '/usr/local/bin/oxidns'), ('', 'oxidns'),
                 ('/opt/configflow-agent/../configflow-agent/m', 'o'), ('/opt/x/../configflow-agent/m', 'o'),
                 ('m\tx', 'o'), ("my bin", "o'x"), ('/usr/bin/mihomo', 'oxidns')]:
    for install_core in [False, True]:
        add('aio', server_url='http://s', agent_name='b', mihomo_service_binary=binaries[0],
            oxidns_service_binary=binaries[1], install_core=install_core)
for vpath, ni, iid, token, tun, assume in [
        ('$GATEWAY_STAGE/configflow-agent', True, '', '', True, True),
        ('$GATEWAY_STAGE/configflow-agent', False, '', '', True, False),
        ('/tmp/agent', True, '', '', True, False),
        ('', True, '', '', True, True),
        ('$GATEWAY_STAGE/configflow-agent', True, 'i' * 16, 't' * 16, False, True),
        ('$GATEWAY_STAGE/configflow-agent', True, 'i' * 16, 't.k-_' * 4, True, False),
        ('', False, 'short', 't' * 16, True, False),
        ('', False, 'i' * 16, '', True, False),
        ('', False, 'i' * 81, 't' * 16, True, False),
        ('', False, 'i' * 16, 't' * 513, True, False),
        ('', False, 'i' * 16, 't t' * 8, True, False)]:
    add('aio', server_url='http://s', agent_name='b', verified_agent_path=vpath, non_interactive=ni,
        installation_id=iid, installation_token=token, manage_tun=tun, tun_prepare_assume_enabled=assume)
for description in ["it's 安装", '', 'plain']:
    add('aio', server_url='http://s', agent_name='b', installation_description=description)

# Linux gateway
for full_tun, iid in itertools.product([False, True], ['', 'g' * 20]):
    add('gateway', server_url='http://192.168.1.2:5001', agent_name='gw', agent_ip='192.168.1.9',
        enrollment_url='http://192.168.1.2:5001/api/agents/gateway-installations/abc/enroll',
        enrollment_token='tok-' + 'x' * 20, agent_checksums={'linux-amd64': SHA_A, 'linux-arm64': SHA_B},
        installation_id=iid, full_tun=full_tun)
for url in ['http://s', 'https://s:8443/', 'ftp://s', 'http://', 'http://user@s', 'http://:@s', 'http://@s', 'http://u:p@s',
            'http://s#f', 'http://s/#', 'HTTP://S', ' http://s', 'http://s\x7f', 'http://[::1]:5001', 'http://[::1', 'http://::1]',
            'http://[v1.x]', 'http://[vz.x]', 'http://[1.2.3.4]', 'http://[fe80::1%eth0]', 'http://[x]', 'http://a[::1]',
            'http://[::1]x', 'http://s／x', 'http://ｓ', 'http://s:notaport', 'http:/s', 'http:s', '//s', 'http://s/a/../']:
    add('gateway', server_url=url, agent_name='gw', agent_checksums={'linux-amd64': SHA_A})
for download in ['', 'http://s/api/agents/download/', 'https://mirror/x', 'file:///x']:
    add('gateway', server_url='http://s', agent_name='gw', binary_download_url=download, agent_checksums={'linux-arm64': SHA_B})
for ip in ['', '192.168.1.9', '127.0.0.1', '224.0.0.1', '0.0.0.0', '169.254.1.1', '240.0.0.1', '192.168.01.9', '1.2.3',
           '::1', '192.168.1.9 ', '255.255.255.255']:
    add('gateway', server_url='http://s', agent_name='gw', agent_ip=ip, agent_checksums={'linux-amd64': SHA_A})
for enroll, token in [('http://s/e', ''), ('', 'x' * 16), ('http://s/e', 'short'), ('http://other/e', 'x' * 16),
                      ('https://s/e', 'x' * 16), ('http://s:80/e', 'x' * 16), ('http://S/e', 'x' * 16), ('http://s/e/', 'x' * 16)]:
    add('gateway', server_url='http://s', agent_name='gw', enrollment_url=enroll, enrollment_token=token,
        agent_checksums={'linux-amd64': SHA_A})
for checksums in [{}, {'linux-armv7': SHA_A}, {'linux-amd64': SHA_A.upper()}, {'linux-amd64': 'abc'},
                  {'linux-arm64': SHA_B}, {'linux-amd64': SHA_A, 'linux-arm64': SHA_B}]:
    add('gateway', server_url='http://s', agent_name='gw', agent_checksums=checksums)
for name in ['gw', 'bad name', '']:
    add('gateway', server_url='http://s', agent_name=name, agent_checksums={'linux-amd64': SHA_A}, mihomo_agent_port=9000)
add('gateway', server_url='http://s', agent_name='gw', agent_checksums={'linux-amd64': SHA_A}, installation_id='g' * 20)

# Docker Agent
DOCKER = dict(server_url='http://192.168.1.2:5001')
for mihomo, mosdns, oxidns in itertools.product([True, False], repeat=3):
    for mode in ['host', 'bridge', 'macvlan', 'my net']:
        for fn in ['compose', 'run']:
            add(fn, **DOCKER, enable_mihomo=mihomo, enable_mosdns=mosdns, enable_oxidns=oxidns, network_mode=mode,
                agent_ip='192.168.1.50' if mihomo else '')
for fn in ['compose', 'run']:
    for name in NAMES:
        add(fn, **DOCKER, agent_name=name)
    for url in ODD_URLS:
        add(fn, server_url=url, agent_ip=url)
    for data_dir in ['./agent_data', '/srv/cf data', "/it's", '/a:b', '数据', '$HOME/x', '']:
        add(fn, **DOCKER, data_dir=data_dir, enable_oxidns=True, enable_mihomo=True, network_mode='bridge')
    for ports in [(8080, 8081, 8082), (1, 2, 3), (10 ** 20, -1, 0), (7890, 53, 9090)]:
        add(fn, **DOCKER, mihomo_port=ports[0], mosdns_port=ports[1], oxidns_port=ports[2], enable_mosdns=True,
            network_mode='bridge')
        add(fn, **DOCKER, mihomo_port=ports[0], oxidns_port=ports[2], enable_oxidns=True, network_mode='bridge')
    # DNS HA
    for mode in ['host', 'macvlan', 'bridge']:
        for ip in ['192.168.1.53', '127.0.0.1', '240.0.0.1', '169.254.2.2', '0.0.0.0', '224.1.1.1', '', 'x', '192.168.1.0',
                   '192.168.1.255', '192.168.2.5']:
            add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=False, enable_oxidns=True, network_mode=mode, agent_ip=ip,
                ha_parent='eth0', ha_subnet='192.168.1.0/24', ha_gateway='192.168.1.1')
    for parent in ['eth0', 'enp1s0', 'a' * 15, 'a' * 16, '_x', '.x', 'e th', '', 'eth0.100', 'é']:
        add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=False, enable_oxidns=True, network_mode='macvlan',
            agent_ip='192.168.1.53', ha_parent=parent, ha_subnet='192.168.1.0/24')
    for subnet, gw in [('192.168.1.0/24', ''), ('192.168.1.0/24', '192.168.1.1'), ('192.168.1.0/24', '192.168.1.53'),
                       ('192.168.1.0/24', '192.168.2.1'), ('192.168.1.0/24', '192.168.1.255'), ('192.168.1.0/24', '192.168.1.0'),
                       ('192.168.1.5/24', ''), ('192.168.1.0/31', ''), ('192.168.1.0/30', ''), ('192.168.0.0/16', ''),
                       ('192.168.1.0/255.255.255.0', ''), ('192.168.1.0/0.0.0.255', ''), ('192.168.1.0/255.0.255.0', ''),
                       ('192.168.1.0/024', ''), ('192.168.1.0/', ''), ('192.168.1.0', ''), ('192.168.1.0/24/1', ''),
                       ('0.0.0.0/0', ''), ('127.0.0.0/8', ''), ('192.168.1.0/ 24', ''), ('192.168.1.0/２４', ''),
                       ('192.168.1.0/24', 'x'), ('192.168.1.0/33', ''), ('192.168.1.48/28', '192.168.1.49')]:
        add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=False, enable_oxidns=True, network_mode='macvlan',
            agent_ip='192.168.1.53', ha_parent='eth0', ha_subnet=subnet, ha_gateway=gw)
    for path in ['/etc/machine-id', '/var/lib/dbus/machine-id', 'etc/machine-id', '/a,b', '/a:b', '/a\nb', '/with space', "/it's"]:
        for mode in ['host', 'macvlan']:
            add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=False, enable_oxidns=True, network_mode=mode,
                agent_ip='192.168.1.53', ha_parent='eth0', ha_subnet='192.168.1.0/24', ha_host_id_path=path,
                data_dir='/srv/ha data')
    for port in [8082, 1023, 1024, 65535, 65536]:
        add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=False, enable_oxidns=True, network_mode='host',
            agent_ip='192.168.1.53', oxidns_port=port)
    for mihomo, mosdns, oxidns in [(True, False, True), (False, True, True), (False, False, False), (False, True, False)]:
        add(fn, **DOCKER, enable_dns_ha=True, enable_mihomo=mihomo, enable_mosdns=mosdns, enable_oxidns=oxidns,
            network_mode='host', agent_ip='192.168.1.53')


def run(fn, kwargs):
    try:
        return True, FUNCTIONS[fn](**kwargs)
    except ValueError as exc:
        return False, 'ValueError: ' + str(exc)
    except Exception as exc:  # noqa: BLE001
        return False, type(exc).__name__ + ': ' + str(exc)


for index, (fn, kwargs) in enumerate(cases):
    ok, result = run(fn, kwargs)
    record = {'id': index, 'fn': fn, 'kwargs': kwargs, 'ok': ok}
    if ok:
        data = result.encode('utf-8', 'surrogatepass')
        record['sha256'] = hashlib.sha256(data).hexdigest()
        record['length'] = len(data)
        if FULL:
            record['output'] = result
    else:
        record['error'] = result
    print(json.dumps(record, ensure_ascii=False))
