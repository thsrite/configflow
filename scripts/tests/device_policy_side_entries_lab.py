#!/usr/bin/env python3
"""Device policies next to node-test side entries, on a real OxiDNS in the PVE lab.

Run on the test server from the task worktree (it reaches PVE with `ssh pve`):

    python scripts/tests/device_policy_side_entries_lab.py --ct <CTID> --ip 10.97.0.41

The CT is an owned, disposable Debian guest on the isolated `cfgt` bridge with
no default route, so nothing in it can reach the internet or the home network.
All OxiDNS upstreams are fake responders on the CT's loopback; every answer
names the path that produced it. ConfigFlow runs here in an isolated temporary
database: the API sets, changes and revokes policies, and the resulting native
configuration is copied into the CT and run by the real core. Publishing via
an Agent is not part of this test.
"""
import argparse
import importlib.util
import json
import subprocess
import sys
import tempfile
from copy import deepcopy
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT))

from backend.agents.node_test_dns import _plugins as managed_plugins  # noqa: E402
from backend.common.agent_manager import get_agent_manager  # noqa: E402
from backend.common.oxidns_editor import prepare_editor  # noqa: E402
from backend.test_qa_integrity import make_app  # noqa: E402

LAB_DIR = '/root/cf-gwip-lab/devpol-side'
ANSWERS = {'direct': '203.0.113.51', 'proxy': '198.18.0.50', 'legacy': '192.0.2.53',
           'managed': '192.0.2.54', 'debug': '192.0.2.55', 'fallback': '203.0.113.99'}
PORTS = {'direct': 5301, 'proxy': 5302, 'legacy': 5303, 'managed': 5304, 'debug': 5305, 'fallback': 5306}
FAKE = '''import socket, threading
ANSWERS = %r
def serve(port, ip):
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.bind(("127.0.0.1", port))
    while True:
        req, peer = s.recvfrom(4096)
        n = 12
        while req[n]:
            n += req[n] + 1
        qtype = int.from_bytes(req[n + 1:n + 3], "big")
        question = req[12:n + 5]
        if qtype == 1:
            body = bytes.fromhex("c00c00010001") + (1).to_bytes(4, "big") + bytes([0, 4]) + socket.inet_aton(ip)
            resp = req[:2] + bytes.fromhex("818000010001") + bytes(4) + question + body
        else:
            resp = req[:2] + bytes.fromhex("818000010000") + bytes(4) + question
        s.sendto(resp, peer)
for port, ip in ANSWERS.items():
    threading.Thread(target=serve, args=(port, ip)).start()
''' % {PORTS[name]: ip for name, ip in ANSWERS.items()}


def ssh(command, data=None, check=True):
    result = subprocess.run(['ssh', '-o', 'BatchMode=yes', 'pve', command], input=data,
                            capture_output=True, timeout=120)
    if check and result.returncode:
        raise RuntimeError(f'{command}: {result.stdout.decode()} {result.stderr.decode()}')
    return result.stdout.decode()


def bash(script):
    return ssh('bash -s', script.encode())


def prepare(ct, oxidns):
    ssh(f'mkdir -p {LAB_DIR} && cat > {LAB_DIR}/fake_upstreams.py', FAKE.encode())
    unit = ('[Service]\nWorkingDirectory=/var/lib/oxidns-lab\n'
            'ExecStart=/opt/oxidns/oxidns start -c /etc/oxidns-lab/config.yaml -d /var/lib/oxidns-lab\n')
    fake_unit = '[Service]\nExecStart=/usr/bin/python3 /opt/fake_upstreams.py\n'
    ssh(f'cat > {LAB_DIR}/oxidns-lab.service', unit.encode())
    ssh(f'cat > {LAB_DIR}/fake-upstreams.service', fake_unit.encode())
    bash(f'''set -e
pct exec {ct} -- mkdir -p /opt/oxidns /etc/oxidns-lab /var/lib/oxidns-lab
pct push {ct} {oxidns} /opt/oxidns/oxidns --perms 0755
pct push {ct} {LAB_DIR}/fake_upstreams.py /opt/fake_upstreams.py
pct push {ct} {LAB_DIR}/oxidns-lab.service /etc/systemd/system/oxidns-lab.service
pct push {ct} {LAB_DIR}/fake-upstreams.service /etc/systemd/system/fake-upstreams.service
pct exec {ct} -- systemctl daemon-reload
pct exec {ct} -- systemctl restart fake-upstreams
pct exec {ct} -- /opt/oxidns/oxidns --version
''')


def deploy(ct, text, label):
    ssh(f'cat > {LAB_DIR}/{label}.yaml', text.encode())
    out = bash(f'''set -e
pct push {ct} {LAB_DIR}/{label}.yaml /etc/oxidns-lab/config.yaml
pct exec {ct} -- /opt/oxidns/oxidns check -c /etc/oxidns-lab/config.yaml -d /var/lib/oxidns-lab
pct exec {ct} -- systemctl restart oxidns-lab
for i in $(seq 1 50); do
  pct exec {ct} -- dig @127.0.0.1 -p 53 ready.example.net +short +time=1 +tries=1 >/dev/null 2>&1 && break
  sleep 0.2
done
pct exec {ct} -- systemctl is-active oxidns-lab
''')
    return out.strip().splitlines()[-1]


def query(ct, ip, where, port, name, source, tcp=False):
    flags = f'-p {port} -b {source} {name} A +short +time=2 +tries=1' + (' +tcp' if tcp else '')
    prefix = '' if where == 'pve' else f'pct exec {ct} -- '
    return ssh(f'{prefix}dig @{ip} {flags}', check=False).strip()


def side_plugins(ip):
    managed = managed_plugins('0123456789abcdef', {
        'listen_address': ip, 'port': 15354, 'endpoint': f'{ip}:15354',
        'upstream': f'udp://127.0.0.1:{PORTS["managed"]}', 'bootstrap': []})
    legacy = [
        {'tag': 'node_test_real_forward', 'type': 'forward',
         'args': {'upstreams': [{'addr': f'udp://127.0.0.1:{PORTS["legacy"]}'}]}},
        {'tag': 'node_test_real_sequence', 'type': 'sequence', 'args': [
            {'exec': '$query_recorder'}, {'exec': '$node_test_real_forward'},
            {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        *[{'tag': 'node_test_real_' + proto, 'type': proto + '_server',
           'args': {'listen': f'{ip}:15353', 'entry': 'node_test_real_sequence'}} for proto in ('udp', 'tcp')],
    ]
    debug = [
        {'tag': 'debug_dns_forward', 'type': 'forward',
         'args': {'upstreams': [{'addr': f'udp://127.0.0.1:{PORTS["debug"]}'}]}},
        {'tag': 'debug_dns_sequence', 'type': 'sequence', 'args': [{'exec': '$debug_dns_forward'}, {'exec': 'accept'}]},
        {'tag': 'debug_dns_udp', 'type': 'udp_server', 'args': {'listen': f'{ip}:5300', 'entry': 'debug_dns_sequence'}},
    ]
    return legacy, managed, debug


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--ct', required=True, type=int)
    parser.add_argument('--ip', required=True)
    parser.add_argument('--host-ip', default='10.97.0.1', help='PVE address on cfgt, used as a second client')
    parser.add_argument('--oxidns', default='/root/cf-gwip-lab/oxidns/oxidns')
    parser.add_argument('--before', help='Previous oxidns_device_policy.py, to show the original refusal')
    parser.add_argument('--out', default=str(ROOT / '.ci-results/device-policy-side-lab.json'))
    args = parser.parse_args()
    ct, ip, host = args.ct, args.ip, args.host_ip
    evidence = {'ct': ct, 'ip': ip, 'rounds': []}
    prepare(ct, args.oxidns)
    legacy, managed, debug = side_plugins(ip)
    side_tags = {p['tag'] for p in legacy + managed + debug}

    with tempfile.TemporaryDirectory() as scratch:
        app, repo = make_app(Path(scratch))
        client = app.test_client()
        agent = get_agent_manager().register_agent({'name': 'lab-oxidns', 'host': ip, 'port': 18081,
                                                    'service_type': 'oxidns', 'profile_id': 'default'})
        settings = prepare_editor({
            'listen': '0.0.0.0:53', 'tcp_enabled': True, 'prefer_ipv4': False,
            'local_dns': f'udp://127.0.0.1:{PORTS["direct"]}', 'remote_dns': f'udp://127.0.0.1:{PORTS["proxy"]}',
            'fallback_dns': f'udp://127.0.0.1:{PORTS["fallback"]}', 'query_recorder': {'enabled': True},
        }, {})
        document = yaml.safe_load(settings['native_config'])
        document['plugins'] += deepcopy(legacy + managed + debug)
        settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
        seed = deepcopy(document)
        repo.update_profile_transaction('default', lambda profile: profile.update(oxidns=deepcopy(settings)))
        record = {'agent_id': agent['id'], 'endpoint_id': '0123456789abcdef', 'endpoint': f'{ip}:15354',
                  'listen_address': ip, 'port': 15354, 'upstream': f'udp://127.0.0.1:{PORTS["managed"]}',
                  'bootstrap': [], 'profile_id': 'default', 'agent_host': ip, 'plugins': deepcopy(managed),
                  'status': 'ready', 'installed': True, 'message': '专用 DNS 接口已发布并通过检查'}

        def system(value):
            target = next(a for a in value['agents'] if a['id'] == agent['id'])
            target['node_test_dns_endpoints'] = {'0123456789abcdef': record}
            value['system_config'].update(node_test_dns=[f'{ip}:15353'], node_test_dns_config={
                'mode': 'custom', 'servers': [f'{ip}:15353'], 'bootstrap': [], 'managed': {}})
        repo.update_system_transaction(system)

        if args.before:
            spec = importlib.util.spec_from_file_location('before_policy', args.before)
            before = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(before)
            try:
                before.patch_document(deepcopy(seed), [], [{'ip': host, 'name': 'lab', 'mode': 'direct'}])
                evidence['before'] = 'accepted'
            except Exception as error:  # noqa: BLE001 - evidence of the original refusal
                evidence['before'] = str(error)
            print('before:', evidence['before'])

        state = client.get('/api/client-devices?profile=default').get_json()
        assert state['editable'], state['reason']
        assert state['side_entries'] == [{'tags': ['debug_dns_udp'], 'listen': f'{ip}:5300',
                                          'entry': 'debug_dns_sequence'}], state['side_entries']
        evidence['api'] = {'editable': state['editable'], 'side_entries': state['side_entries']}

        def put(target, name, mode, previous_name, previous_mode):
            response = client.put('/api/client-devices?profile=default', json={
                'profile_id': 'default', 'ip': target, 'name': name, 'mode': mode,
                'previous_name': previous_name, 'previous_mode': previous_mode, 'base_url': ''})
            assert response.status_code == 200, response.get_json()
            return response.get_json()

        rounds = [
            ('insert', [(host, 'PVE 实验主机', 'direct', '', ''), (ip, '实验 CT', 'smart', '', '')],
             {'pve': 'direct', 'ct': 'proxy'}),
            ('change', [(host, 'PVE 实验主机', 'smart', 'PVE 实验主机', 'direct'),
                        (ip, '实验 CT', 'direct', '实验 CT', 'smart')], {'pve': 'proxy', 'ct': 'direct'}),
            ('revoke', [(host, '', '', 'PVE 实验主机', 'smart'), (ip, '', '', '实验 CT', 'direct')],
             {'pve': 'proxy', 'ct': 'proxy'}),
        ]
        for index, (label, changes, expected) in enumerate(rounds, 1):
            for change in changes:
                put(*change)
            text = repo.get_profile('default')['oxidns']['native_config']
            document = yaml.safe_load(text)
            sides = [p for p in document['plugins'] if p['tag'] in side_tags]
            assert sides == [p for p in seed['plugins'] if p['tag'] in side_tags], 'side chain changed'
            main = next(p for p in document['plugins'] if p['tag'] == 'sequence_main')['args']
            hooks = main.count({'exec': 'jump cf_device_policy'})
            assert hooks == (0 if label == 'revoke' else 1)
            status = deploy(ct, text, f'round{index}-{label}')
            name = f'probe-{label}.example.net'
            results = {
                'main_from_pve': query(ct, ip, 'pve', 53, name, host),
                'main_from_ct': query(ct, ip, 'ct', 53, name, ip),
                'main_from_ct_tcp': query(ct, ip, 'ct', 53, 'tcp-' + name, ip, tcp=True),
            }
            for where, source in (('pve', host), ('ct', ip)):
                results[f'legacy_from_{where}'] = query(ct, ip, where, 15353, name, source)
                results[f'legacy_from_{where}_tcp'] = query(ct, ip, where, 15353, 'tcp-' + name, source, tcp=True)
                results[f'managed_from_{where}'] = query(ct, ip, where, 15354, name, source)
                results[f'debug_from_{where}'] = query(ct, ip, where, 5300, name, source)
            want = {'main_from_pve': ANSWERS[expected['pve']], 'main_from_ct': ANSWERS[expected['ct']],
                    'main_from_ct_tcp': ANSWERS[expected['ct']]}
            for key in results:
                for kind in ('legacy', 'managed', 'debug'):
                    if key.startswith(kind):
                        want[key] = ANSWERS[kind]
            evidence['rounds'].append({'round': label, 'service': status, 'hooks': hooks,
                                       'side_chain_unchanged': True, 'results': results, 'expected': want})
            print(label, json.dumps(results, ensure_ascii=False))
            assert results == want, (label, results, want)
    Path(args.out).parent.mkdir(parents=True, exist_ok=True)
    Path(args.out).write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + '\n')
    print('PASS: insert, change and revoke with legacy, managed and debug side entries on a real core')


if __name__ == '__main__':
    main()
