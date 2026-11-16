"""Public fake probe protocol and DNS validator branches (R097–R101, H082+)."""
from compare import Policy
from harness import Pair, json_body, DesignError
from probe import Exchange
from cases.nodes import NODE


def policy_for(pair, names):
    policy = pair.policy
    policy.times.add('body.tested_at')
    for name in names:
        policy.times.add('body.results.' + name + '.tested_at')
        policy.times.add('files.shared/node_latency.json.content.' + name + '.tested_at')
    return policy


def node_probe(binary, recorder):
    modes = [('S01', 'valid', {}, 200), ('S02', 'valid', {'names': [NODE['name']]}, 200),
             ('S03', 'valid', {'keys': ['["node","node-a"]']}, 200),
             ('F18', 'exit-1', {}, 503), ('F18', 'exit-3', {}, 503), ('F18', 'version', {}, 503),
             ('F18', 'invalid-json', {}, 503), ('F18', 'results-type', {}, 503),
             ('F18', 'missing-result', {}, 503), ('F18', 'latency-zero', {}, 503),
             ('F18', 'latency-bool', {}, 503), ('F18', 'latency-missing-error', {}, 503)]
    for case, mode, body, status in modes:
        with Pair(binary, recorder, options={'shared': {'nodes': [NODE]}, 'probe_case': mode}) as pair:
            policy = policy_for(pair, ['["node","node-a"]', NODE['name']])
            pair.request('R098-' + case, 'POST /api/nodes/latency', body=json_body(body),
                         expect=status, writes=True, policy=policy, interaction=Exchange(pair), variant=mode + '/' + case)
            if status == 200:
                pair.request('R097-S02', 'GET /api/nodes/latency', expect=200, writes=True,
                             policy=policy, variant=case)
    for case, options, body, expected in [
        ('F13', {'shared': {'nodes': [NODE]}}, {}, 503),
        ('F14', {'shared': {'nodes': [NODE]}, 'environment': {'CONFIGFLOW_NODE_TEST_URL': 'file:///synthetic'}}, {}, 503),
        ('F15', {}, {'keys': ['missing-' + str(i) for i in range(21)]}, 400),
        ('F16', {'shared': {'nodes': [NODE, {**NODE, 'id': 'node-b'}]}}, {'names': [NODE['name']]}, 400)]:
        with Pair(binary, recorder, options=options) as pair:
            pair.request('R098-' + case, 'POST /api/nodes/latency', body=json_body(body), expect=expected, writes=True)


def dns_settings(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R100-S01', 'GET /api/nodes/latency/settings', expect=200)
        for case, body in [('S01', {'dns': {'mode': 'system'}}),
                           ('S02', {'dns': {'mode': 'custom', 'servers': ['192.0.2.53']}}),
                           ('S03', {'dns_servers': ['192.0.2.53']}), ('S04', {'dns_servers': []}),
                           ('B01', {'dns_servers': ['192.0.2.53:015353']}),
                           ('B02', {'dns_servers': ['192.0.2.53', '192.0.2.53:53']})]:
            pair.request('R101-' + case, 'POST /api/nodes/latency/settings', body=json_body(body), expect=200, writes=True)
            if case == 'S02':
                pair.request('R100-S02', 'GET /api/nodes/latency/settings', expect=200)
        for case, body in [('F08', []), ('F09', {}), ('F10', {'dns': {'mode': 'invalid'}}),
                           ('F11', {'dns': {'mode': 'custom', 'servers': []}}),
                           ('F12', {'dns': {'mode': 'custom', 'servers': ['https://dns.example/dns-query']}}),
                           ('F13', {'dns_servers': ['192.0.2.53:0']})]:
            pair.request('R101-' + case, 'POST /api/nodes/latency/settings', body=json_body(body), expect=400, writes=True)
    with Pair(binary, recorder, options={'system_config': {'node_test_dns': ['192.0.2.53']}}) as pair:
        pair.request('R100-B01', 'GET /api/nodes/latency/settings', expect=200)


def dns_guard_branches(binary, recorder):
    # Each row changes one guard input; remaining fields use a legal custom mode.
    bad = [
        ('H082', {'servers': ['https://dns.example'], 'bootstrap': [1]}),
        ('H083', {'servers': ['0.0.0.0']}), ('H083', {'servers': ['224.0.0.1']}),
        ('H084', {'servers': ['192.0.2.53:0']}), ('H084', {'servers': ['192.0.2.53:65536']}),
        ('H084', {'servers': ['192.0.2.53:٥٣']}), ('H085', {'servers': ['not-ip']}),
        ('H086', {'servers': [None]}), ('H086', {'servers': ['']}),
        ('H087', {'servers': ['https://192.0.2.53/' + 'x' * 2049]}),
        ('H087', {'servers': ['https://dns.example/a b']}), ('H087', {'servers': ['https://dns.example/?q=1']}),
        ('H087', {'servers': ['https://dns.example/#fragment']}), ('H087', {'servers': ['https://dns.example/\\bad']}),
        ('H088', {'servers': ['http://dns.example']}), ('H088', {'servers': ['https://u:p@dns.example']}),
        ('H088', {'servers': ['https:///missing']}), ('H089', {'servers': ['https://-bad.example']}),
        ('H089', {'servers': ['https://' + 'a' * 64 + '.example']}),
        ('H090', {'servers': ['https://[2001:db8::1]/dns-query']}), ('H090', {'servers': ['https://0.0.0.0']}),
        ('H091', {'servers': ['https://192.0.2.53/%xy']}), ('H092', {'servers': ['https://dns.example:0']}),
        ('H092', {'servers': ['https://dns.example:']}), ('H093', {'servers': ['tls://192.0.2.53/path']}),
        ('H094', {'servers': ['https://[broken']}), ('H095', {'servers': '192.0.2.53'}),
        ('H095', {'servers': ['192.0.2.' + str(i) for i in range(1, 6)]}),
        ('H096', {'mode': 'invalid'}), ('H097', {'servers': []}),
        ('H098', {'servers': ['tls://dns.example'], 'bootstrap': []})]
    with Pair(binary, recorder) as pair:
        for index, (case, changed) in enumerate(bad):
            dns = {'mode': 'custom', 'servers': ['192.0.2.53'], 'bootstrap': ['192.0.2.54'], **changed}
            pair.request(case, 'POST /api/nodes/latency/dns/check', body=json_body({'dns': dns}),
                         variant=str(index), expect=400, writes=True)
        for case, body in [('F08', {}), ('F09', []), ('F10', {'dns': {'mode': 'invalid'}})]:
            pair.request('R099-' + case, 'POST /api/nodes/latency/dns/check', body=json_body(body), expect=400, writes=True)
        pair.request('R099-F11', 'POST /api/nodes/latency/dns/check',
                     body=json_body({'dns': {'mode': 'system'}}), expect=503, writes=True)


def dns_probe(binary, recorder):
    for case, mode, expected in [('S01', 'valid', 200), ('S02', 'valid', 200),
                                 ('F12', 'dns-empty', 503), ('F12', 'dns-fake-ip', 503),
                                 ('F12', 'dns-invalid-ip', 503), ('F12', 'dns-bad-latency', 503),
                                 ('F12', 'invalid-json', 503), ('F12', 'version', 503), ('F12', 'exit-1', 503)]:
        with Pair(binary, recorder, options={'probe_case': mode}) as pair:
            dns = {'mode': 'system'} if case == 'S01' else {'mode': 'custom', 'servers': ['192.0.2.53']}
            pair.request('R099-' + case, 'POST /api/nodes/latency/dns/check', body=json_body({'dns': dns}),
                         variant=mode, expect=expected, writes=True, interaction=Exchange(pair))


GROUPS = {'nodes-probe': node_probe, 'nodes-dns-settings': dns_settings,
          'nodes-dns-guards': dns_guard_branches, 'nodes-dns-probe': dns_probe}
