"""Optional real-core acceptance: OXIDNS_TEST_BINARY=/path/to/oxidns pytest ..."""
import os
import socket
import struct
import subprocess
import threading
import time

import pytest
import yaml

from backend.common.oxidns_device_policy import apply_policies
from backend.converters.oxidns import generate_oxidns_config


@pytest.mark.skipif(not os.environ.get('OXIDNS_TEST_BINARY'), reason='Set OXIDNS_TEST_BINARY for real-core acceptance')
@pytest.mark.parametrize(('ranges', 'direct4'), [
    ([('127.0.0.1', 'direct'), ('::1', 'smart')], True),
    ([('127.0.0.0/8', 'direct'), ('::/64', 'smart')], True),
    ([('127.0.0.0/8', 'direct'), ('127.0.0.1', 'smart'), ('::/64', 'smart'), ('::1', 'direct')], False),
    ([('127.0.0.0/8', 'smart'), ('127.0.0.1', 'direct'), ('::/64', 'direct'), ('::1', 'smart')], True),
    ([('127.0.0.0/8', 'direct'), ('127.0.0.0/16', 'smart'), ('::/64', 'smart'), ('::/96', 'direct')], False),
    ([('127.0.0.0/8', 'smart'), ('127.0.0.0/16', 'direct'), ('::/64', 'direct'), ('::/96', 'smart')], True),
    ([('0.0.0.0/0', 'direct'), ('127.0.0.0/8', 'smart'), ('::/0', 'smart'), ('::/64', 'direct')], False),
    ([('::ffff:127.0.0.1/104', 'direct'), ('::/64', 'smart')], True),
])
def test_direct_and_smart_clients_resolve_same_domain_independently(tmp_path, ranges, direct4):
    binary = os.environ['OXIDNS_TEST_BINARY']
    servers = []
    stop = threading.Event()

    def upstream(ip):
        sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        sock.bind(('127.0.0.1', 0))
        sock.settimeout(.1)
        servers.append(sock)

        def serve():
            while not stop.is_set():
                try:
                    request, peer = sock.recvfrom(4096)
                except socket.timeout:
                    continue
                answer = request[:2] + struct.pack('!5H', 0x8180, 1, 1, 0, 0) + request[12:]
                answer += b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 60, 4) + socket.inet_aton(ip)
                sock.sendto(answer, peer)
        thread = threading.Thread(target=serve, daemon=True)
        thread.start()
        return sock.getsockname()[1], thread

    def free_port(family, host):
        with socket.socket(family, socket.SOCK_DGRAM) as sock:
            sock.bind((host, 0))
            return sock.getsockname()[1]

    direct_port, direct_thread = upstream('203.0.113.51')
    proxy_port, proxy_thread = upstream('198.18.0.50')
    port4 = free_port(socket.AF_INET, '127.0.0.1')
    port6 = free_port(socket.AF_INET6, '::1')
    config = {'oxidns': {
        'listen': f'127.0.0.1:{port4}', 'tcp_enabled': False, 'prefer_ipv4': False,
        'local_dns': f'udp://127.0.0.1:{direct_port}', 'remote_dns': f'udp://127.0.0.1:{proxy_port}',
        'query_recorder': {'enabled': True}, 'log': {'level': 'warn'}, 'api': {'enabled': False},
        'custom_config': yaml.safe_dump({'plugins': [{'tag': 'server_v6', 'type': 'udp_server',
            'args': {'listen': f'[::1]:{port6}', 'entry': 'sequence_main'}}]}),
    }}
    config['oxidns'] = apply_policies(config['oxidns'], config, [
        {'ip': ip, 'mode': mode} for ip, mode in ranges])
    expected4 = '203.0.113.51' if direct4 else '198.18.0.50'
    expected6 = '198.18.0.50' if direct4 else '203.0.113.51'
    path = tmp_path / 'config.yaml'
    path.write_text(generate_oxidns_config(config))
    checked = subprocess.run([binary, 'check', '-c', str(path), '-d', str(tmp_path)], capture_output=True, text=True, timeout=10)
    assert checked.returncode == 0, checked.stdout + checked.stderr
    process = None
    try:
        with (tmp_path / 'core.log').open('w') as log:
            process = subprocess.Popen([binary, 'start', '-c', str(path), '-d', str(tmp_path)], stdout=log, stderr=log)

            def query(family, host, port, name):
                question = b''.join(bytes([len(label)]) + label.encode() for label in name.split('.')) + b'\0' + struct.pack('!HH', 1, 1)
                packet = struct.pack('!6H', 1234, 0x0100, 1, 0, 0, 0) + question
                with socket.socket(family, socket.SOCK_DGRAM) as client:
                    client.settimeout(.3)
                    client.sendto(packet, (host, port))
                    result = client.recv(4096)
                assert struct.unpack('!H', result[6:8])[0] == 1, result.hex()
                return socket.inet_ntoa(result[-4:])

            deadline = time.monotonic() + 5
            while True:
                try:
                    assert query(socket.AF_INET, '127.0.0.1', port4, 'same.test') == expected4
                    break
                except socket.timeout:
                    if time.monotonic() > deadline:
                        pytest.fail((tmp_path / 'core.log').read_text())
            assert query(socket.AF_INET6, '::1', port6, 'same.test') == expected6
            assert query(socket.AF_INET6, '::1', port6, 'reverse.test') == expected6
            assert query(socket.AF_INET, '127.0.0.1', port4, 'reverse.test') == expected4
            assert query(socket.AF_INET, '127.0.0.1', port4, 'same.test') == expected4
    finally:
        if process:
            process.terminate()
            process.wait(timeout=5)
        stop.set()
        direct_thread.join(timeout=1)
        proxy_thread.join(timeout=1)
        for server in servers:
            server.close()


def _serve(ip, stop, sockets):
    """A loopback DNS upstream that answers every A query with one address."""
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind(('127.0.0.1', 0))
    sock.settimeout(.1)
    sockets.append(sock)

    def serve():
        while not stop.is_set():
            try:
                request, peer = sock.recvfrom(4096)
            except (socket.timeout, OSError):
                continue
            answer = request[:2] + struct.pack('!5H', 0x8180, 1, 1, 0, 0) + request[12:]
            answer += b'\xc0\x0c' + struct.pack('!HHIH', 1, 1, 60, 4) + socket.inet_aton(ip)
            sock.sendto(answer, peer)
    thread = threading.Thread(target=serve, daemon=True)
    thread.start()
    return sock.getsockname()[1], thread


def _query(port, name, source):
    question = b''.join(bytes([len(label)]) + label.encode() for label in name.split('.')) + b'\0' + struct.pack('!HH', 1, 1)
    packet = struct.pack('!6H', 4321, 0x0100, 1, 0, 0, 0) + question
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as client:
        client.bind((source, 0))
        client.settimeout(.3)
        client.sendto(packet, ('127.0.0.1', port))
        result = client.recv(4096)
    assert struct.unpack('!H', result[6:8])[0] == 1, result.hex()
    return socket.inet_ntoa(result[-4:])


@pytest.mark.skipif(not os.environ.get('OXIDNS_TEST_BINARY'), reason='Set OXIDNS_TEST_BINARY for real-core acceptance')
def test_side_node_test_entry_keeps_its_upstream_while_main_clients_follow_policies(tmp_path):
    """Insert, change and revoke policies next to a node-test side entry on a real core."""
    from backend.common.oxidns_device_policy import read_policies

    binary = os.environ['OXIDNS_TEST_BINARY']
    stop, sockets, threads = threading.Event(), [], []
    ports = {}
    for name, ip in (('direct', '203.0.113.51'), ('proxy', '198.18.0.50'), ('side', '192.0.2.53')):
        ports[name], thread = _serve(ip, stop, sockets)
        threads.append(thread)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
        probe.bind(('127.0.0.1', 0))
        main_port = probe.getsockname()[1]
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as probe:
        probe.bind(('127.0.0.1', 0))
        side_port = probe.getsockname()[1]
    side = [
        {'tag': 'node_test_real_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': f'udp://127.0.0.1:{ports["side"]}'}]}},
        {'tag': 'node_test_real_sequence', 'type': 'sequence', 'args': [
            {'exec': '$query_recorder'}, {'exec': '$node_test_real_forward'},
            {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        {'tag': 'node_test_real_udp', 'type': 'udp_server',
         'args': {'listen': f'127.0.0.1:{side_port}', 'entry': 'node_test_real_sequence'}},
    ]
    config = {'profile_id': 'default', 'agents': [], 'system_config': {'node_test_dns': [f'127.0.0.1:{side_port}']},
              'oxidns': {
                  'listen': f'127.0.0.1:{main_port}', 'tcp_enabled': False, 'prefer_ipv4': False,
                  'local_dns': f'udp://127.0.0.1:{ports["direct"]}', 'remote_dns': f'udp://127.0.0.1:{ports["proxy"]}',
                  'query_recorder': {'enabled': True}, 'log': {'level': 'warn'}, 'api': {'enabled': False},
                  'custom_config': yaml.safe_dump({'plugins': side})}}
    process = None

    def start():
        nonlocal process
        if process:
            process.terminate()
            process.wait(timeout=5)
        path = tmp_path / 'config.yaml'
        path.write_text(generate_oxidns_config(config))
        checked = subprocess.run([binary, 'check', '-c', str(path), '-d', str(tmp_path)],
                                 capture_output=True, text=True, timeout=10)
        assert checked.returncode == 0, checked.stdout + checked.stderr
        log = (tmp_path / 'core.log').open('a')
        process = subprocess.Popen([binary, 'start', '-c', str(path), '-d', str(tmp_path)], stdout=log, stderr=log)
        deadline = time.monotonic() + 5
        while True:
            try:
                _query(side_port, 'ready.test', '127.0.0.1')
                _query(main_port, 'ready.test', '127.0.0.1')
                return yaml.safe_load(path.read_text())
            except socket.timeout:
                if time.monotonic() > deadline:
                    pytest.fail((tmp_path / 'core.log').read_text())

    def side_plugins(document):
        return [p for p in document['plugins'] if p['tag'].startswith('node_test_real')]

    try:
        config['oxidns'] = apply_policies(config['oxidns'], config, [
            {'ip': '127.0.0.2', 'mode': 'direct'}, {'ip': '127.0.0.3', 'mode': 'smart'}])
        state = read_policies(config['oxidns'], config)
        assert state['editable'] and state['side_entries'] == []
        document = start()
        assert side_plugins(document) == side
        assert _query(main_port, 'same.test', '127.0.0.2') == '203.0.113.51'   # direct hook -> local upstream
        assert _query(main_port, 'same.test', '127.0.0.3') == '198.18.0.50'    # smart -> default proxy route
        assert _query(main_port, 'same.test', '127.0.0.4') == '198.18.0.50'    # no policy
        for source in ('127.0.0.2', '127.0.0.3', '127.0.0.4'):
            assert _query(side_port, 'same.test', source) == '192.0.2.53'      # side entry ignores policies

        config['oxidns'] = apply_policies(config['oxidns'], config, [
            {'ip': '127.0.0.0/8', 'mode': 'direct'}, {'ip': '127.0.0.3', 'mode': 'smart'}])
        document = start()
        assert side_plugins(document) == side
        assert _query(main_port, 'other.test', '127.0.0.2') == '203.0.113.51'
        assert _query(main_port, 'other.test', '127.0.0.4') == '203.0.113.51'
        assert _query(main_port, 'other.test', '127.0.0.3') == '198.18.0.50'
        assert _query(side_port, 'other.test', '127.0.0.2') == '192.0.2.53'

        config['oxidns'] = apply_policies(config['oxidns'], config, [])
        document = start()
        assert side_plugins(document) == side
        assert all(p['tag'] not in ('cf_device_policy', 'cf_device_direct') for p in document['plugins'])
        assert _query(main_port, 'last.test', '127.0.0.2') == '198.18.0.50'
        assert _query(side_port, 'last.test', '127.0.0.2') == '192.0.2.53'
    finally:
        if process:
            process.terminate()
            process.wait(timeout=5)
        stop.set()
        for thread in threads:
            thread.join(timeout=1)
        for sock in sockets:
            sock.close()
