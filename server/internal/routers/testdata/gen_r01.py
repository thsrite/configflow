"""R01 reference corpus from the real Python functions (run on the test server).

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routers/testdata/gen_r01.py \
        server/internal/routers/testdata/r01_corpus.jsonl.gz

Rows {"fn", "in", "out"} or {"fn", "in", "err": [class, message]}. Synthetic values only.
Generated router ids are replaced by "router_GEN".
"""
import copy
import gzip
import ipaddress
import json
import random
import sys

from backend.common.client_fixed_ip import validate_bindings
from backend.routers import desired as desired_mod
from backend.routers.base import DhcpDnsEntry, DhcpDnsState, RouteEntry, StaticRouteIndex
from backend.routers.dhcp_dns import build_dns_report, dns_agent_settings, dns_ha_settings, verify_dns
from backend.routers.lan_clients import ipv4, normalize_mac
from backend.routers.ros_values import comparable_values
from backend.routers.store import normalize, public_view, redact_routers

rng = random.Random(16)
rows = []


def emit(fn, value, call):
    try:
        out = call()
    except Exception as exc:  # noqa: BLE001
        rows.append({'fn': fn, 'in': value, 'err': [type(exc).__name__, str(exc)]})
        return None
    rows.append({'fn': fn, 'in': value, 'out': out})
    return out


def pick(*options):
    return rng.choice(options)


# ---- store ------------------------------------------------------------------------------------
BASE = {'name': '主路由', 'type': 'routeros', 'endpoint': 'http://192.168.88.1', 'username': 'admin', 'password': 'pw'}
ODD = [None, True, False, 0, 1, 60, 60.0, '', 'x', [], ['r1'], ['r1', 'r1', 'r2'], [''], [1], {}, {'enabled': True},
       '60', 15, 1440, 7, -1]


def payload():
    p = {} if rng.random() < 0.3 else dict(BASE)
    for _ in range(rng.randint(0, 4)):
        key = pick('name', 'type', 'endpoint', 'username', 'password', 'verify_tls', 'proxy_gateway_agent_id',
                   'dns_agent_id', 'ip_rule_sets', 'manage_return_routing', 'ros_policy_rule_sets', 'auto_rule_sync',
                   'rule_sync_interval', 'dns_guard', 'ipv6_pd_guard', 'other')
        p[key] = pick(*ODD) if rng.random() < 0.4 else {
            'name': pick(' 主路由 ', '', 'R'), 'type': pick('ikuai', 'routeros', 'openwrt'),
            'endpoint': pick('http://192.168.1.1/', 'https://router.example:8443', 'ftp://x', 'http://h/path',
                             'http://h/;p', 'http://', 'https://[fd00::1]:443/', 'http://h?x=1', ' http://h// '),
            'username': pick(' admin ', ''), 'password': pick('', 'secret'), 'verify_tls': pick(True, False, None),
            'proxy_gateway_agent_id': pick('agent-a', ''), 'dns_agent_id': pick('dns-a', ''),
            'ip_rule_sets': pick(['r1'], ['r1', 'r2', 'r1'], []), 'manage_return_routing': pick(True, False),
            'ros_policy_rule_sets': pick(['r2'], ['r1'], []), 'auto_rule_sync': pick(True, False),
            'rule_sync_interval': pick(15, 30, 60, 1440, 45), 'other': 'ignored',
            'dns_guard': pick(None, {'enabled': True}, {'enabled': False, 'bypass': ['192.168.1.10', ' 10.0.0.0/8 ',
                                                                                     '10.0.0.1/8', '192.168.1.0/255.255.255.0']},
                              {'bypass': ['bad']}, {'x': 1}, {'enabled': 'yes'}, {'bypass': [f'10.0.{i}.0/24' for i in range(65)]},
                              {'block_dot': True, 'failover': False}, {'bypass': 'x'}, {'bypass': [None]}, {'bypass': None}, {'enabled': None}),
            'ipv6_pd_guard': pick(None, {'enabled': True}, {'enabled': False}, {}, {'enabled': 1}, {'x': True}),
        }[key]
    return p


def stored(router):
    r = copy.deepcopy(router)
    for key in rng.sample(['last_probe', 'last_release', 'ros_policy_state', 'dns_guard_state', 'ipv6_fakeip_state',
                           'ipv6_routes_state', 'ipv6_pd_guard_state', 'rule_sync', 'custom'], rng.randint(0, 3)):
        r[key] = pick({'ok': True, 'at': '2026-11-16T10:00:00'}, {'resources': [{'path': '/ip/route', 'id': '*1'}]}, None, 'x')
    return r


def mask(router):
    if isinstance(router, dict) and str(router.get('id', '')).startswith('router_') and len(router['id']) == 19:
        router = {**router, 'id': 'router_GEN'}
    return router


records = []
for _ in range(2500):
    p = payload()
    existing = rng.choice(records) if records and rng.random() < 0.5 else None
    if existing is not None and rng.random() < 0.3:
        existing = stored(existing)
    if existing is not None and rng.random() < 0.2:
        existing = {**existing, 'ip_rule_sets': ['r1', 'r2'], 'ros_policy_rule_sets': ['r2']}
    out = emit('normalize', {'payload': p, 'existing': existing}, lambda: mask(normalize(p, existing)))
    if out:
        records.append(out)
for record in records[:800]:
    r = stored(record)
    emit('public_view', r, lambda: public_view(r))
    emit('stored', r, lambda: r)
for _ in range(60):
    doc = {'items': [stored(rng.choice(records)) for _ in range(rng.randint(0, 3))] + pick([], [1, None]), 'x': 1}
    emit('redact_routers', doc, lambda: redact_routers(copy.deepcopy(doc)))

# ---- desired ----------------------------------------------------------------------------------
NETS = ['91.108.4.0/22', '91.108.8.0/22', '149.154.160.0/20', '10.0.0.0/8', '1.0.1.0/24', '1.0.0.0/24',
        '2001:b28:f23d::/48', '2001:67c:4e8::/48', 'fd00::/8', '0.0.0.0/0', '8.8.8.8', '100.64.0.0/10', '198.18.0.0/15',
        '192.0.0.9/32', '2001:20::/28']


def content():
    lines = []
    for _ in range(rng.randint(0, 12)):
        net = rng.choice(NETS)
        kind = 'IP-CIDR6' if ':' in net else 'IP-CIDR'
        lines.append(pick(f'{kind},{net}', f'{kind},{net},no-resolve', f'DOMAIN,t.me', f'# {net}', net))
    return '\n'.join(lines)


for _ in range(600):
    names = ['Telegram', 'Netflix', 'Google', 'Extra']
    rules = [{'id': f'r{i}', 'itemType': pick('ruleset', 'ruleset', 'rule'), 'name': pick(name, ''),
              'policy': pick('Proxy', 'DIRECT', 'REJECT', 'Direct only', ''), **pick({}, {'enabled': False},
                                                                                     {'library_rule_id': 'lib1'},
                                                                                     {'library_rule_id': 'gone'},
                                                                                     {'library_enabled': False})}
             for i, name in enumerate(names)]
    profile = {'mihomo': {'custom_config': pick('', 'dns:\n  fake-ip-range: 198.18.0.1/16\n',
                                                'dns: {fake-ip-range: 28.0.0.1/8}', 'dns: {fake-ip-range: bad}',
                                                'dns: [1]', ': bad: yaml: [', 'dns: {fake-ip-range: 10.0.0.0/255.0.0.0}')},
               'proxy_groups': [{'name': 'Direct only', 'manual_nodes': pick(['DIRECT'], ['DIRECT', 'Node']),
                                 'include_groups': pick([], ['g'])}],
               'rule_configs': rules,
               'rule_library': pick([], [{'id': 'lib1', 'enabled': pick(True, False)}])}
    texts = {r['id']: content() for r in rules}
    selected = rng.sample([r['id'] for r in rules] + ['gone'], rng.randint(0, 5))
    gateway = pick('192.168.1.4', '', None)
    emit('compute_desired', {'profile': profile, 'gateway': gateway or '', 'rule_sets': selected, 'contents': texts},
         lambda: desired_mod.compute_desired(profile, gateway, selected,
                                             content_loader=lambda item, _lib: texts[item['id']]))

# ---- DHCP DNS ---------------------------------------------------------------------------------
HOSTS = ['192.168.0.2', '127.0.0.1', '0.0.0.0', '224.0.0.1', '255.255.255.255', 'dns.example', '::1', '192.168.0.12', '']


def agent(i):
    a = {'id': f'dns-{i}', 'host': rng.choice(HOSTS), 'service_type': pick('oxidns', 'oxidns', 'mihomo'),
         'status': pick('online', 'online', 'offline')}
    if rng.random() < 0.8:
        a['oxidns_ha'] = {'enabled': pick(True, True, False), 'group_id': pick('g1', 'g1', ''),
                          'peer_id': f'dns-{1 - i}', 'vip': pick('192.168.0.250', '192.168.0.250', '0.0.0.0')}
    if rng.random() < 0.8:
        a['oxidns_ha_state'] = {'status': pick('succeeded', 'succeeded', 'failed')}
    if rng.random() < 0.5:
        a['oxidns_ha_publication'] = {'status': pick(None, 'succeeded', 'failed')}
    return a


for _ in range(400):
    a, b = agent(0), agent(1)
    agent_id, chosen = pick('dns-0', ''), pick(a, None)
    emit('dns_agent_settings', {'id': agent_id, 'agent': chosen}, lambda: dns_agent_settings(agent_id, chosen))
    peer = pick(b, None)
    emit('dns_ha_settings', {'id': 'dns-0', 'agent': a, 'peer': peer}, lambda: dns_ha_settings('dns-0', a, peer))


def entry():
    return {'id': pick('*1', '*2', '*3', ''), 'network': pick('192.168.0.0/24', '192.168.0.0/25', '10.0.0.0/24',
                                                              'bad', '192.168.0.1/24'),
            'dns': pick(['192.168.0.2'], ['1.1.1.1', '8.8.8.8'], []), 'enabled': pick(True, True, False),
            'attributes': {'gateway': '192.168.0.1'}, 'blockers': pick([], [], ['DHCP option 6 覆盖 DNS']),
            'dns_explicit': pick(True, True, False)}


for _ in range(1500):
    entries = [entry() for _ in range(rng.randint(0, 3))]
    state = DhcpDnsState([DhcpDnsEntry(**e) for e in entries], {}) if rng.random() < 0.9 else None
    dns = {'dns_agent_id': pick('dns-0', ''), 'dns_server': pick('192.168.0.2', ''),
           'dns_error': pick('', '', '所选 DNS Agent 离线，无法下发 DHCP DNS')}
    gateway = pick('192.168.0.3', '10.0.0.9', 'bad', '')
    emit('build_dns_report', {'dns': dns, 'gateway': gateway, 'entries': entries if state else None},
         lambda: build_dns_report({**dns, 'proxy_gateway': gateway}, state))
    if state is not None:
        check = {'id': pick('*1', '*2'), 'network': pick('192.168.0.0/24', '10.0.0.0/24'), 'gateway': gateway,
                 'dns': pick(['192.168.0.2'], ['1.1.1.1', '8.8.8.8'])}
        emit('verify_dns', {'check': check, 'entries': entries}, lambda: verify_dns(check, state))

# ---- static route index -----------------------------------------------------------------------
for _ in range(1500):
    base = rng.choice(['198.18.', '91.108.', '10.20.'])
    routes = []
    for i in range(rng.randint(0, 8)):
        prefix = rng.choice([15, 16, 17, 18, 24, 32])
        dst = str(ipaddress.ip_network(f'{base}{rng.randint(0, 3) * 64}.0/{prefix}', strict=False))
        routes.append({'id': f'*{i}', 'dst': pick(dst, dst, 'bad'), 'gateway': pick('192.168.1.4', '192.168.1.4', '192.168.1.9'),
                       'enabled': pick(True, True, False), 'comment': pick('cf', 'cf-x', 'manual', ''),
                       'table': pick('main', 'main', 'other'), 'active': pick(None, True, False),
                       'distance': pick(None, 1, 2)})
    if rng.random() < 0.5:
        for r in routes:
            r['active'] = r['distance'] = None
    cidr = rng.choice([f'{base}0.0/16', f'{base}0.0/17', f'{base}64.0/18', f'{base}0.0/15'])
    gateway = pick('192.168.1.4', '')
    index = StaticRouteIndex([RouteEntry(**r) for r in routes])
    sel = index.select(cidr, gateway)
    emit('select', {'routes': routes, 'cidr': cidr, 'gateway': gateway},
         lambda: {'candidates': [r.id for r in sel.candidates], 'selected': [r.id for r in sel.selected],
                  'covered': sel.covered, 'conflict': sel.conflict})

# ---- small helpers ----------------------------------------------------------------------------
for path in ['/routing/table', '/ip/firewall/mangle', '/ip/route', '/ip/address']:
    for value in [True, False, 'true', 'yes', 'false', 'no', '', 'maybe', None]:
        row = {'fib': value, 'passthrough': value, 'gateway': pick('192.168.1.4@main', '192.168.1.4', 'ether1', '192.168.1.4@other', '010.1.1.1'), 'x': 1}
        emit('comparable_values', {'path': path, 'row': row}, lambda: comparable_values(path, row))
for text in ['AA:BB:CC:DD:EE:FF', 'aa-bb-cc-dd-ee-ff', 'a:b:c:d:e:f', ' aa:bb:cc:dd:ee:ff ', 'AA:BB:CC:DD:EE', 'AAA:BB:CC:DD:EE:FF',
             'GG:BB:CC:DD:EE:FF', 'aa:bb:cc:dd:ee:f ', '', 'aa::cc:dd:ee:ff']:
    emit('normalize_mac', text, lambda: normalize_mac(text))
for text in ['192.168.0.1', ' 192.168.0.1 ', '192.168.0.01', '::1', '1.2.3.4/32', '', '256.1.1.1']:
    emit('ipv4', text, lambda: ipv4(text))
good = {'router_id': 'r1', 'vendor': 'routeros', 'id': '*1', 'ip': '192.168.0.10', 'mac': 'AA:BB:CC:DD:EE:01',
        'comment': 'cf-client: phone'}
for doc in [{'192.168.0.10': good}, {'192.168.0.10': {**good, 'vendor': 'x'}}, {'192.168.0.10': {**good, 'mac': 'aa:bb:cc:dd:ee:01'}},
            {'192.168.0.10': {**good, 'extra': 'x'}}, {'192.168.0.10': {**good, 'server': 5}}, {'192.168.0.11': good},
            {'192.168.0.10': {k: v for k, v in good.items() if k != 'comment'}}, [], {}, {'192.168.0.10': {**good, 'comment': 'x\n'}}]:
    emit('validate_bindings', doc, lambda: validate_bindings(doc))

# ---- review R01 (2026-11-16): boundary edges ------------------------------------------------------
# custom_config YAML forms: the Mihomo generator reads them with PyYAML (duplicate keys: last wins).
for text in ['dns:\n  fake-ip-range: 28.0.0.1/8\n  fake-ip-range: 29.0.0.1/8\n',
             'dns:\n  fake-ip-range: 28.0.0.1/8\nmode: rule\nmode: global\n',
             'dns:\n  enable: true\ndns:\n  fake-ip-range: 28.0.0.1/8\n',
             'dns:\n  fake-ip-range: 28.0.0.1/8\n---\na: 1\n', 'dns:\n  fake-ip-range: 28.0.0.1/8\n1: x\n',
             'dns:\n  fake-ip-range: 28.0.0.1/8\nx: !!set {a, b}\n', 'base: &b\n  fake-ip-range: 28.0.0.1/8\ndns:\n  <<: *b\n',
             'dns:\n\tfake-ip-range: 28.0.0.1/8\n', 'dns:\n  fake-ip-range: 1\n', 'dns:\n  fake-ip-range: "28.0.0.1/8"\n',
             'dns:\n  fake-ip-range: 28.0.0.1/8\nyes: 1\n', '  \n', 'dns: null\n']:
    profile = {'mihomo': {'custom_config': text}, 'rule_configs': [], 'rule_library': [],
               'proxy_groups': [1, None, {'name': 'D', 'manual_nodes': ['DIRECT']}]}
    emit('compute_desired', {'profile': profile, 'gateway': '192.168.1.4', 'rule_sets': [], 'contents': {}},
         lambda: desired_mod.compute_desired(profile, '192.168.1.4', [], content_loader=lambda item, _lib: ''))
# Records written before auto_rule_sync/rule_sync_interval/ros_policy_rule_sets/manage_return_routing existed.
OLD = {'id': 'router_old', 'name': 'R', 'type': 'routeros', 'endpoint': 'http://192.168.88.1', 'username': 'admin',
       'password': 'pw', 'verify_tls': False, 'proxy_gateway_agent_id': 'a', 'dns_agent_id': '', 'ip_rule_sets': ['r1'],
       'last_probe': {'ok': True}}
emit('public_view', OLD, lambda: public_view(OLD))
emit('stored', OLD, lambda: OLD)
for p in [{}, {'auto_rule_sync': True}, {'rule_sync_interval': 6e1}, {'ros_policy_rule_sets': ['r1']},
          {'dns_guard': {'bypass': ['10.0.0.1', None]}}, {'ipv6_pd_guard': {'enabled': True, 'x': 1}},
          {'dns_guard': {'enabled': True, 'x': 1, 'block_dot': 'no'}}, {'type': 'ikuai'}, {'password': None}]:
    emit('normalize', {'payload': p, 'existing': OLD}, lambda: normalize(p, OLD))
guarded = {**OLD, 'dns_agent_id': 'dns-a', 'dns_guard': {'enabled': True, 'block_dot': False, 'failover': True,
                                                         'bypass': ['10.0.0.0/8']}, 'dns_guard_state': {'x': 1},
           'ipv6_pd_guard': {'enabled': True}, 'ros_policy_state': {'y': 1}}
for p in [{'type': 'ikuai'}, {'endpoint': 'http://192.168.88.2'}, {'dns_agent_id': ''}, {'dns_guard': None},
          {'name': 'R2'}, {'ipv6_pd_guard': None, 'dns_guard': {'enabled': False}}]:
    emit('normalize', {'payload': p, 'existing': guarded}, lambda: normalize(p, guarded))
for doc in [{'192.168.0.10': {**good, 'server': None}}, {'192.168.0.10': {**good, 'comment': 'cf-client:' + 'x' * 502}},
            {'192.168.0.10': {**good, 'comment': 'cf-client:' + 'x' * 503}}, {'192.168.0.10': 1}, None,
            {f'10.0.{i // 256}.{i % 256}': 1 for i in range(2049)}]:
    emit('validate_bindings', doc, lambda: validate_bindings(doc))

with open(sys.argv[1], 'wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
    fh.write(''.join(json.dumps(r, ensure_ascii=False) + '\n' for r in rows).encode())
counts = {}
for r in rows:
    counts[r['fn']] = counts.get(r['fn'], 0) + 1
print(len(rows), 'rows', counts)
