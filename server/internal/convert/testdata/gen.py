#!/usr/bin/env python3
"""Generate corpus.jsonl.gz: inputs and what the Python C12 functions make of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen.py

Every value is synthetic (example/test/invalid domains, documentation
addresses). Each line is one JSON object:

    {"fn": name, "args": [...], "out": result}           normal return
    {"fn": name, "args": [...], "err": [class, message]}  exception

Mutating functions record the mutated argument next to the result. Sets are
written as sorted lists, tuples as lists, dependency graphs as
[[kind, id], [[kind, id], ...]] pairs in dict order. The deterministic
Sub-Store stand-in (fake_convert) is mirrored by the Go test.
"""

import datetime
import gzip
import json
import math
import random
import sys
from copy import deepcopy
from pathlib import Path

assert sys.version_info[:2] == (3, 11), sys.version

import logging
logging.disable(logging.CRITICAL)

from backend.converters import common, mihomo
from backend.common import mihomo_easytier as et
from backend.utils import dialer_references as dr
from backend.utils import sub_store_client

R = random.Random(20261115)
OUT = []


def fake_convert(proxy_string, target='ClashMeta'):
    """Stand-in for Sub-Store: deterministic on the text."""
    if 'fail' in proxy_string:
        return None
    if 'empty' in proxy_string:
        return {}
    proxy = {'name': 'from-uri', 'type': 'http', 'server': 'fixture.test', 'port': 80}
    if 'dial-' in proxy_string:
        proxy['dialer-proxy'] = proxy_string.split('dial-', 1)[1].split('&', 1)[0]
    if 'vless' in proxy_string:
        proxy['type'] = 'vless'
        proxy['reality-opts'] = {'public-key': 'k'}
    return proxy


sub_store_client.convert_proxy_string = fake_convert


def conv(value):
    if isinstance(value, dict):
        return {(k if isinstance(k, str) else repr(k)): conv(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [conv(v) for v in value]
    if isinstance(value, (set, frozenset)):
        return sorted((conv(v) for v in value), key=lambda v: json.dumps(v, sort_keys=True))
    if isinstance(value, bytes):
        return {'__bytes__': value.hex()}
    if isinstance(value, (datetime.date, datetime.datetime)):
        return {'__date__': str(value)}
    return value


def record(fn, args, call, *, mutated=None):
    entry = {'fn': fn, 'args': deepcopy(args)}
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - the corpus records every exception
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = conv(result)
    if mutated is not None:
        entry['mutated'] = conv(mutated)
    OUT.append(entry)


def graph_out(graph):
    return [[list(k), [list(c) for c in v]] for k, v in graph.items()]


# ---------------------------------------------------------------- common.py

RULE_TYPES = ['MATCH', 'DOMAIN', 'NETWORK', 'AND', 'OR', 'NOT', 'DST-PORT', 'SRC-PORT', 'IN-PORT', 'IP-CIDR']


def rand_rule():
    item = {'itemType': R.choice(['rule', 'rule', 'ruleset', 'other']), 'rule_type': R.choice(RULE_TYPES)}
    for key, values in (('enabled', [True, False, None, 0, 1]), ('udp_fallback', ['DIRECT', 'REJECT', 'direct', None, 1]),
                        ('ntp_direct', [True, False, 1, 'true', None]), ('name', ['set-a', '规则', 3]),
                        ('policy', ['DIRECT', 'Proxy', None]), ('value', ['example.test', '((NETWORK,udp))', 5])):
        if R.random() < 0.5:
            item[key] = R.choice(values)
    return item


for _ in range(300):
    config = {'rule_configs': [rand_rule() for _ in range(R.randint(0, 4))]}
    if R.random() < 0.05:
        config = {'rule_configs': R.choice([None, 'x', [1], {}])}
    record('udp_fallback', [config], lambda: common.udp_fallback(config))
    record('ntp_direct', [config], lambda: common.ntp_direct(config))
    record('emitted_rules', [config], lambda: common.emitted_rules(config))

for _ in range(100):
    item = rand_rule()
    record('after_match_comment', [item], lambda: common.after_match_comment(item))

LOGIC_VALUES = ['((NETWORK,udp),(DST-PORT,123))', '((network , TCP ),(DOMAIN,example.test))', '(NETWORK,Udp)',
                '((NETWORK,quic))', '((DST-PORT,80/443),(NETWORK,tcp))', '((SRC-PORT, 1000,2000))', '((DEST-PORT,8080))',
                '((IN-PORT,7890))', '(( dst-port ,1-100))', '((NETWORK,\u3000udp\u3000))', '((NETWORK\x0b,udp))', '']
for value in LOGIC_VALUES + ['80/443', '80,443', '1000-2000', ' udp ', 'tcp', 'ß']:
    for rule_type in RULE_TYPES:
        record('network_as_protocol', [rule_type, value], lambda: common.network_as_protocol(rule_type, value))
        record('has_port_list', [rule_type, value], lambda: common.has_port_list(rule_type, value))

SECTIONS = ['[General]\nloglevel = notify', '[General]\n  skip-proxy = a\n', '[General]\nipv6=false',
            '[General]\nskip-proxy-x = 1', '', '\n\n', '[General]\r\nipv6 = true\r\n', '[General]\n\u3000ipv6 = true']
for section in SECTIONS:
    for key in ['ipv6', 'skip-proxy', 'a.b', 'loglevel']:
        record('set_general_option', [section, key, 'true'], lambda: common.set_general_option(section, key, 'true'))

URLS = ['https://example.test/rules/a.yaml', 'https://example.test/a.YML?x=1', 'https://example.test/a.mrs#f',
        'https://example.test/a.txt', 'example.test/a.yaml', 'https://[::1/a.yaml', 'https://[v1.x]/a.mrs', '', None, 5,
        'HTTPS://EXAMPLE.TEST/A.YAML', 'ftp://example.test/b.list', '/local/c.yaml']
for _ in range(300):
    rule = {}
    library = []
    for _ in range(R.randint(0, 3)):
        entry = {}
        for key, values in (('id', ['l1', 'l2']), ('name', ['set-a', 'set-b']), ('format', ['yaml', 'text', 'mrs', 'json', None]),
                            ('url', URLS), ('source_type', ['content', 'url']),
                            ('content', ['payload:\n  - a', 'a.example.test', '  {"payload": []}', '\ufeffpayload :', 5, '"payload" : x'])):
            if R.random() < 0.4:
                entry[key] = R.choice(values)
        library.append(entry)
    for key, values in (('library_rule_id', ['l1', 'l2', '', None]), ('name', ['set-a', 'set-b']),
                        ('format', ['yaml', 'text', 'mrs', 'bad']), ('url', URLS), ('source_type', ['content', 'url']),
                        ('content', ['payload:\n- x', 'x\n payload: y', 'domain', None])):
        if R.random() < 0.4:
            rule[key] = R.choice(values)
    source_url = R.choice([None, None, 'https://example.test/s.yaml', 'https://example.test/s.mrs', ''])
    config = {'rule_library': library}
    record('ruleset_format', [rule, config, source_url], lambda: common.ruleset_format(rule, config, source_url))

HOSTS = ['example.test 192.0.2.1', '192.0.2.2 legacy.test', 'v6.test 2001:db8::1', '2001:db8::2 v6legacy.test',
         '# comment', 'a.test 192.0.2.3 # trailing', 'only-one', 'x.test y.test', '01.2.3.4 lead.test',
         'zone.test fe80::1%eth0', '\u3000wide.test\u3000192.0.2.9', 'a.test\t192.0.2.4\tz', '192.0.2.5 192.0.2.6',
         'mapped.test ::ffff:192.0.2.7', 'bad.test 2001:db8::01.2.3.4', 'big.test 256.1.1.1']
for _ in range(80):
    text = R.choice(['\n', '\r\n', '\x85', '\u2028']).join(R.sample(HOSTS, R.randint(0, 6)))
    record('parse_hosts_pairs', [text], lambda: common.parse_hosts_pairs(text))

# ---------------------------------------------------------- mihomo.py nodes

for _ in range(200):
    proxy = {}
    for key, values in (('type', ['vless', 'vmess', 'VLESS', None]), ('encryption', ['', 'zero', None, 'none', 'aes', 0, False]),
                        ('reality-opts', [{}, {'short-id': 'ab'}, {'public-key': 'k'}, None, 'x'])):
        if R.random() < 0.6:
            proxy[key] = R.choice(values)
    if R.random() < 0.05:
        proxy = {}
    before = deepcopy(proxy)
    from backend.utils.proxy_utils import fix_proxy_fields
    record('fix_proxy_fields', [before], lambda: fix_proxy_fields(proxy), mutated=proxy)

GH = ['https://github.com/a/b', 'https://raw.githubusercontent.com/a/b/c.yaml', 'https://GitHub.com/x',
      'https://gist.githubusercontent.com/x', 'https://api.github.com/repos', 'https://example.test/?u=https://github.com/a',
      'http://user@github.com:443/x', 'https://ghproxy.example/https://github.com/a', 'https://[::1]/x', 'https://[bad/x',
      'https://gİthub.com/x', 'github.com/a', '', 'https://github.com.example.test/a', 'https://[v1.a]/x',
      'https://[192.0.2.1]/x', 'https://\u212a.test/x']
for _ in range(250):
    url = R.choice(GH)
    config = R.choice([{}, None, {'system_config': {}}, {'system_config': {'github_proxy_domain': R.choice(
        ['ghproxy.example', 'https://ghproxy.example/', ' http://ghproxy.example ', '', None, 5])}},
        {'system_config': None}, {'system_config': 'x'}, {'other': 1}])
    record('apply_github_proxy_domain', [url, config], lambda: mihomo.apply_github_proxy_domain(url, config))

# Bools are ints in Python (True is port 1); Go reports them as 1/0, so they
# are left out here and covered by a unit test.
PORT_VALUES = [7890, 7891, 9090, '7890', 7890.0, None, 65536, 10 ** 20, [1]]
for _ in range(250):
    doc = {}
    for key in ['port', 'socks-port', 'mixed-port', 'redir-port', 'tproxy-port']:
        if R.random() < 0.3:
            doc[key] = R.choice(PORT_VALUES[:-1] if R.random() < 0.95 else PORT_VALUES)
    if R.random() < 0.5:
        doc['listeners'] = [R.choice([{'name': 'l', 'port': R.choice(PORT_VALUES[:-1])},
                                      {'name': 'configflow-region-probe', 'port': 17999}, 'x', {'port': 9090}])
                            for _ in range(R.randint(0, 3))]
    if R.random() < 0.5:
        doc['external-controller'] = R.choice(['127.0.0.1:9090', ':9091', '9092', '0.0.0.0:', '[::]:9093', 'host:abc',
                                               '1:\u0661\u0662', '1:\u00b2', 5, None])
    record('mihomo_used_ports', [doc], lambda: mihomo.mihomo_used_ports(doc))
    record('mihomo_listen_ports', [doc], lambda: sorted(mihomo.mihomo_listen_ports(doc).items()))

PROXY_STRINGS = [
    'vless://sample@proxy.example:443?type=ws', 'ss://sample@proxy.example:443', '', 'null', '~',
    'name: sample\nserver: proxy.example', 'type: false', 'type: 0', 'type: []', 'type: {}', '- type: ss', 'type: [',
    '{type: ss, port: 443}', 'server: https://proxy.example\ntype: ss', 'type: ss\ndialer-proxy: DIRECT\nport: 443',
    '{"type":"ss","n":123456789012345678901234567890}', '{"type":false}', 'type: ss\nflag: yes', 'type: ss\nflag: on',
    'type: ss\nvalue: 1e3', 'type: ss\nvalue: 0777', 'type: ss\nvalue: 0o777', 'type: ss\nvalue: 1:20',
    'type: ss\nvalue: 08', 'type: ss\nport: 80\nport: 443', 'type: ss\n---\ntype: trojan',
    'base: &base {server: proxy.example, port: 443}\ntype: ss\n<<: *base', '{"type":"ss","value":NaN}',
    'type: ss\na: &shared [item]\nb: *shared', '  {"type": "vless", "name": "x"}  ', 'a:://b\ntype: ss',
    'key: vless://x\ntype: ss', 'my scheme://x\ntype: ss', 'x:y://z', 'type: vless\nencryption: zero',
    '{"type": "vless", "reality-opts": {}}', 'type: ss\nvalue: 2026-10-17', 'type: !!binary ""',
    'type: ss # c\nname: 节点', '\u3000type: ss\u3000', '{"type": "ss", "a": 1, "a": 2}', 'type: ss\nvalue: .nan',
]
for text in PROXY_STRINGS:
    record('parse_structured_proxy_string', [text], lambda: mihomo._parse_structured_proxy_string(text))

NODE_STRINGS = ['vless://uuid@proxy.example:443', 'ss://x@proxy.example:1?dial-relay', 'trojan://fail@proxy.example:1',
                'hy2://empty@proxy.example:1', 'vless://vless@proxy.example:2', '{"type": "ss", "server": "a.test"}',
                'type: vless\nserver: b.test', 'type: trojan\ndialer-proxy: relay', 'base64-blob-dial-g1']
for _ in range(300):
    node = {}
    for key, values in (('name', ['Home', '节点', 5, None, '']), ('proxy_string', NODE_STRINGS + ['', None, 7]),
                        ('type', ['SS', 'vless', '', 5]), ('server', ['c.test']), ('port', [443, '443']),
                        ('params', [{}, {'udp': True, 'name': 'inner'}, {'encryption': 'zero'}, None, [['a', 1]], 'x'])):
        if R.random() < 0.55:
            node[key] = R.choice(values)
    record('convert_node_to_mihomo', [node], lambda: mihomo.convert_node_to_mihomo(node))

# conversions scope: reuse answers from the record, failures included
for strings in ([NODE_STRINGS[0], NODE_STRINGS[0], NODE_STRINGS[2]], [NODE_STRINGS[1], NODE_STRINGS[3]]):
    results = {}
    outs = []
    with mihomo.node_conversions(results):
        for s in strings:
            outs.append(mihomo.convert_node_to_mihomo({'name': 'n', 'proxy_string': s}))
    with mihomo.node_conversions(results, reuse=True):
        for s in strings:
            outs.append(mihomo.convert_node_to_mihomo({'name': 'again', 'proxy_string': s}))
    OUT.append({'fn': 'node_conversions', 'args': [strings], 'out': conv([outs, results])})

# ------------------------------------------------------- dialer_references.py

IDS = ['n0', 'n1', 'n2', 'n3', 'n4']
GIDS = ['g0', 'g1', 'g2', 'g3']


def rand_ref():
    if R.random() < 0.08:
        return R.choice([{'type': 'node'}, {'type': 'x', 'id': 'n1'}, {'type': 'node', 'id': ''}, 'n1', None,
                         {'type': 'node', 'id': 'n1', 'x': 1}, {'type': 'group', 'id': 5}])
    if R.random() < 0.6:
        return {'type': 'node', 'id': R.choice(IDS + ['gone'])}
    return {'type': 'group', 'id': R.choice(GIDS + ['missing'])}


def rand_profile():
    nodes = []
    for i, nid in enumerate(IDS[:R.randint(1, 5)]):
        node = {'id': nid, 'name': R.choice([nid, nid.upper(), 'Same', 'DIRECT']) if R.random() < 0.2 else nid,
                'enabled': R.random() > 0.15, 'type': 'http', 'server': 'example.test', 'port': 80}
        roll = R.random()
        if roll < 0.25:
            node['dialer_ref'] = rand_ref()
        elif roll < 0.4:
            node['params'] = {'dialer-proxy': R.choice(IDS + GIDS + ['G0', 'DIRECT', 'REJECT', '', 5, 'gone'])}
        elif roll < 0.5:
            node['proxy_string'] = R.choice(['{"type": "http", "dialer-proxy": "n1"}', 'type: http\ndialer-proxy: g0',
                                             'vless://x@proxy.example:1?dial-n2', 'type: http\nserver: a.test'])
        if R.random() < 0.1:
            node['subscription_id'] = 's0'
        if R.random() < 0.15:
            node['_proxy_chain_id'] = 'c'
        if R.random() < 0.03:
            node.update(R.choice([{'params': None}, {'proxy_string': 123}, {'id': ['x']}, {'name': ''}, {'name': ['N']}]))
        nodes.append(node)
    groups = []
    for gid in GIDS[:R.randint(0, 4)]:
        group = {'id': gid, 'name': gid.upper(), 'type': R.choice(['select', 'select', 'url-test', 'chain']),
                 'enabled': R.random() > 0.15}
        if group['type'] == 'chain':
            if R.random() < 0.9:
                group['chain'] = {'entry': rand_ref(), 'exit': rand_ref()}
            else:
                group['chain'] = R.choice([None, {'entry': rand_ref()}, 'x'])
            if R.random() < 0.1:
                group['manual_nodes'] = ['n0']
        else:
            for key, values in (('manual_nodes', IDS + ['DIRECT']), ('include_groups', GIDS), ('subscriptions', ['s0', 's1']),
                                ('aggregations', ['a0', 'a1']), ('use', ['Sub0', 'Agg0', 'Nope'])):
                if R.random() < 0.35:
                    group[key] = R.sample(values, R.randint(0, 2))
            if R.random() < 0.2:
                group['proxies_order'] = [{'type': R.choice(['node', 'strategy', 'subscription', 'aggregation', 'x']),
                                           'id': R.choice(IDS + GIDS + ['s0', 'a0'])} for _ in range(R.randint(0, 3))]
            if R.random() < 0.1:
                group['source'] = R.choice(['node', 'strategy'])
                group['proxies'] = R.sample(IDS + GIDS, 2)
            if R.random() < 0.08:
                group['follow_group'] = R.choice(GIDS + ['gone', ['g0']])
            if R.random() < 0.05:
                group['include_all'] = True
            if R.random() < 0.03:
                group.update(R.choice([{'proxies_order': [5]}, {'manual_nodes': 'n0'}, {'use': [1]}]))
        groups.append(group)
    profile = {'nodes': nodes, 'proxy_groups': groups}
    if R.random() < 0.7:
        profile['subscriptions'] = [{'id': 's0', 'name': 'Sub0', 'enabled': R.random() > 0.2},
                                    {'id': 's1', 'name': R.choice(['Sub1', 'Sub0'])}]
    if R.random() < 0.6:
        profile['subscription_aggregations'] = [{'id': 'a0', 'name': 'Agg0', 'nodes': R.sample(IDS, R.randint(0, 2)),
                                                 'subscriptions': R.sample(['s0', 's1'], R.randint(0, 1)),
                                                 'enabled': R.random() > 0.2}]
    if R.random() < 0.02:
        profile['nodes'] = R.choice([None, 'x', {}])
    return profile


for _ in range(2500):
    profile = rand_profile()
    record('validate_dialers', [profile], lambda: dr.validate_dialers(profile))
    record('validate_shapes', [profile, False], lambda: dr.validate_shapes(profile, require_ids=False))
    record('dependency_graph', [profile], lambda: graph_out(dr.dependency_graph(profile)))
    kind, ident = R.choice([('node', R.choice(IDS)), ('group', R.choice(GIDS))])
    record('incoming_dialers', [profile, kind, ident], lambda: [list(k) for k in dr.incoming_dialers(profile, kind, ident)])
    selected = R.sample(IDS, R.randint(0, 2))
    include = R.random() < 0.6
    record('dependency_node_ids', [profile, selected, include],
           lambda: dr.dependency_node_ids(profile, selected, include_roots=include))
    client = R.choice(['Surge', 'Loon'])
    record('reject_surge_dialers', [profile, client], lambda: dr.reject_surge_dialers(profile, client))
    nodes = profile.get('nodes') if isinstance(profile.get('nodes'), list) else []
    if nodes and isinstance(nodes[0], dict):
        node = deepcopy(R.choice(nodes))
        proxy = R.choice([None, {}, {'name': 'converted', 'type': 'http'}])
        proxy = deepcopy(proxy)
        record('overlay_dialer', [profile, node, deepcopy(proxy)], lambda: dr.overlay_dialer(profile, node, proxy))
        record('raw_dialer', [node], lambda: dr.raw_dialer(node))
        record('resolve_dialer', [profile, node], lambda: dr.resolve_dialer(profile, node))
        record('dialer_target', [profile, node], lambda: dr.dialer_target(profile, node))
    groups = profile.get('proxy_groups') or []
    if groups:
        group = R.choice(groups)
        record('group_members', [group], lambda: [list(k) for k in dr.group_members(group)])

NAMES = ['HK 01', 'hk-02', 'US 01', 'JP', '香港 A', 'Relay', 'Entry', 'Exit', 'DIRECT']
FILTERS = ['', 'HK', '(?i)hk', 'US|JP', 'HK`US', '^香港', '(', '[a-', 'A{2,}', '(?=H)H']
TYPES = ['http', 'ss', 'vless', 'HTTP', '']


def rand_proxies(prefix=''):
    proxies = []
    for name in R.sample(NAMES, R.randint(0, 4)):
        proxy = {'name': prefix + name, 'type': R.choice(TYPES)}
        if R.random() < 0.3:
            proxy['dialer-proxy'] = R.choice(['Relay', 'Entry', 'gone', 'DIRECT', 5, 'Dyn'])
        proxies.append(proxy)
    if R.random() < 0.05 and proxies:
        proxies.append(dict(proxies[0]))
    return proxies


for _ in range(400):
    proxies = rand_proxies()
    include, exclude, exclude_type = R.choice(FILTERS), R.choice(FILTERS), R.choice(['', 'http', 'SS|vless', 'x|'])
    record('filter_provider_candidates', [proxies, include, exclude, exclude_type],
           lambda: dr.filter_provider_candidates(proxies, include, exclude, exclude_type))

for _ in range(300):
    pattern = R.choice(['HK', 'US|JP', '(H)(K)', '^香港', '(?:x)?', '('])
    rank = R.randint(0, 2)
    import re as _re
    try:
        groups = _re.compile(pattern).groups
    except _re.error:
        groups = 0
    tag = '排序_'
    rule = {'pattern': f'^(?!{_re.escape(tag)})(?=[\\s\\S]*?(?:{pattern}))([\\s\\S]*)$',
            'target': f'{tag}{rank + 1}_${groups + 1}'}
    override = {}
    if R.random() < 0.7:
        override['proxy-name'] = R.choice([[rule], [rule, rule], [{'pattern': '(a)', 'target': 'x$5'}],
                                           [{'pattern': '(?P<n>H)', 'target': '$1$0$١'}], []])
    for key in ('additional-prefix', 'additional-suffix'):
        if R.random() < 0.3:
            override[key] = R.choice(['pre-', '-suf', ''])
    proxy = {'name': R.choice(NAMES + ['排序_1_HK', 'H\nK'])}
    record('provider_proxy_name', [proxy, override], lambda: dr.provider_proxy_name(proxy, override))


def rand_emitted():
    proxies = rand_proxies()
    groups = []
    for name in R.sample(['Relay', 'Entry', 'Dyn', 'Outer'], R.randint(0, 3)):
        group = {'name': name, 'type': 'select'}
        if R.random() < 0.6:
            group['proxies'] = R.sample([p['name'] for p in proxies] + ['DIRECT', 'Relay', 'Dyn', 'gone'], R.randint(0, 2))
        if R.random() < 0.4:
            group['use'] = R.sample(['Feed', 'Other', 'Missing'], R.randint(1, 2))
        for key, values in (('filter', FILTERS), ('exclude-filter', FILTERS), ('exclude-type', ['', 'http'])):
            if R.random() < 0.2:
                group[key] = R.choice(values)
        if R.random() < 0.25:
            group['dialer-proxy'] = R.choice(['Relay', 'Entry', 'Dyn', 'gone', 'DIRECT'])
        groups.append(group)
    providers = {}
    payloads = {}
    for name in R.sample(['Feed', 'Other'], R.randint(0, 2)):
        definition = {}
        for key, values in (('filter', FILTERS), ('exclude-filter', FILTERS), ('exclude-type', ['', 'ss'])):
            if R.random() < 0.2:
                definition[key] = R.choice(values)
        if R.random() < 0.4:
            override = {}
            if R.random() < 0.5:
                override['dialer-proxy'] = R.choice(['Relay', 'Entry', 'gone', None])
            if R.random() < 0.4:
                override['additional-prefix'] = R.choice(['p-', ''])
            definition['override'] = override
        if R.random() < 0.1:
            definition['dialer-proxy'] = R.choice(['Relay', 'gone'])
        providers[name] = definition
        if R.random() < 0.8:
            payloads[name] = rand_proxies(R.choice(['', 'r-']))
    config = {'proxies': proxies, 'proxy-groups': groups}
    if providers or R.random() < 0.5:
        config['proxy-providers'] = providers
    return config, payloads


for _ in range(2500):
    config, payloads = rand_emitted()
    force = R.random() < 0.3
    require = R.random() < 0.4
    use_payloads = R.random() < 0.8
    record('validate_emitted', [config, force, payloads if use_payloads else None, require],
           lambda: dr.validate_emitted(config, force_chains=force, provider_proxies=payloads if use_payloads else None,
                                       require_providers=require))

# --------------------------------------------------------- mihomo_easytier.py

OPTION_VALUES = {
    'network-name': ['home', ' home ', '', 'x' * 513, 'a\x01', 5, None],
    'network-secret': ['s3cret', 'y' * 600, ''],
    'hostname': ['phone-1', '-bad', 'a' * 64, 'ok_no', ' ', '节点'],
    'ipv4': ['10.144.144.10', '10.144.144.10/24', '10.0.0.1/255.255.255.0', '2001:db8::1', 'x', '010.1.1.1', '10.1.1.1/0.0.0.255'],
    'dhcp': [True, False, 'yes', 1],
    'peers': [['tcp://203.0.113.10:11010'], ['udp://[2001:db8::1]:11010', 'udp://[2001:db8::1]:11010'],
              ['203.0.113.10:11010'], ['TCP://a.test:1'], ['tcp://'], [], 'tcp://x', [' ', 'ws://a.test'], [5]],
    'listeners': [['tcp://0.0.0.0:11010'], [], ['quic://[::]:11012']],
    'no-listener': [True, False],
    'mapped-listeners': [['tcp://203.0.113.20:11010'], ['bad']],
    'exit-nodes': [['10.144.144.1'], ['2001:db8::1'], ['node-a'], ['10.0.0.256']],
    'proxy-networks': [['192.168.50.0/24'], ['192.168.50.7/24'], ['192.168.0.0/24->10.0.0.0/24'], ['2001:db8::/64'],
                       ['10.0.0.0/33'], ['10.0.0.0/255.255.0.0'], ['10.0.0.0/0.0.255.255'], ['10.0.0.0/255.0.255.0'],
                       ['10.0.0.0/'], ['10.0.0.0/24/1'], ['10.0.0.0/024']],
    'udp': [True, False, 'yes', None],
    'mtu': [1380, 100, 9001, 576, 9000, 1380.0, True, '1380', 10 ** 30],
    'routing-mark': [0, 6666, 2 ** 32 - 1, 2 ** 32, -1],
    'ip-version': ['dual', 'ipv4-prefer', 'v4', None, 4],
    'tld-dns-zone': ['et.net.', '...', 'lan.example', ''],
    'dialer-proxy': ['DIRECT', 'Relay', 'missing', ''],
    'local-private-key': ['cHJpdg==', ''],
    'local-public-key': ['cHVi', ''],
    'bogus': [1],
    'state-dir': ['./easytier'],
}
ROUTES = ['10.144.144.0/24', '10.200.0.5/24', '2001:db8::/64', '192.168.0.0/24->10.0.0.0/24', 'bad', '10.0.0.0/8', ' ']


def valid_settings():
    value = {'enabled': True, 'name': R.choice(['EasyTier', 'ET 1', 'Relay']), 'routes': R.sample(ROUTES[:2] + ['10.0.0.0/8'], 2),
             'options': {'network-name': 'home', 'network-secret': 's3cret', 'peers': ['tcp://203.0.113.10:11010'], 'udp': True}}
    if R.random() < 0.5:
        value['apply_to'] = R.choice(['subscription', 'agent', 'all'])
    if R.random() < 0.4:
        value['magic_dns'] = True
    if R.random() < 0.4:
        value['keepalive'] = {'enabled': True, 'url': 'http://10.144.144.2/', 'interval': 60}
    for key in R.sample(['hostname', 'ipv4', 'dhcp', 'mtu', 'tld-dns-zone', 'dialer-proxy', 'proxy-networks', 'exit-nodes',
                         'routing-mark', 'ip-version', 'listeners', 'no-listener'], R.randint(0, 4)):
        value['options'][key] = OPTION_VALUES[key][0]
    return value


def rand_settings():
    if R.random() < 0.4:
        return valid_settings()
    value = {'enabled': R.choice([True, True, False, 'yes', None]) if R.random() < 0.95 else 1}
    if R.random() < 0.8:
        value['name'] = R.choice(['EasyTier', 'ET 1', 'a,b', '', None, 'x' * 65, 5, 'Relay', 'EasyTier 保活'])
    if R.random() < 0.5:
        value['apply_to'] = R.choice(['subscription', 'agent', 'all', 'phones'])
    if R.random() < 0.7:
        value['routes'] = R.sample(ROUTES, R.randint(0, 3)) if R.random() < 0.95 else R.choice(['x', None, [1]])
    if R.random() < 0.4:
        value['magic_dns'] = R.choice([True, False, 'on'])
    if R.random() < 0.4:
        value['keepalive'] = R.choice([
            {'enabled': True, 'url': 'http://10.144.144.2/', 'interval': 60},
            {'enabled': True, 'url': '', 'interval': 300}, {'enabled': False, 'url': 'ftp://x'},
            {'enabled': True, 'url': 'https://a.test/x y'}, {'interval': 5}, {'interval': 86401}, {'interval': True},
            {'enabled': 'yes'}, None, [], 'x', {'url': 5}, {'interval': 60.0}])
    if R.random() < 0.9:
        options = {}
        for key in R.sample(list(OPTION_VALUES), R.randint(0, 7)):
            if key == 'bogus' and R.random() < 0.7:
                continue
            options[key] = R.choice(OPTION_VALUES[key])
        if R.random() < 0.7:
            options.setdefault('network-name', 'home')
            options.setdefault('peers', ['tcp://203.0.113.10:11010'])
        value['options'] = options if R.random() < 0.97 else R.choice([None, [], 'x'])
    return value


for _ in range(1500):
    value = rand_settings() if R.random() < 0.97 else R.choice([None, [], 'x', 5])
    record('normalize_settings', [value], lambda: et.normalize_settings(value))
    record('redact_settings', [value], lambda: et.redact_settings(value))
    document = R.choice([
        {'proxies': [{'name': 'Relay', 'type': 'ss'}], 'proxy-groups': [{'name': 'G', 'type': 'select'}],
         'rules': ['MATCH,DIRECT'], 'dns': {'enable': True}},
        {'dns': {'nameserver-policy': {'+.lan.example': '192.0.2.53'}}},
        {'proxies': [{'name': 'EasyTier'}], 'rules': None},
        {'mixed-port': 7890, 'dns': {'nameserver-policy': None}, 'rules': []},
        {'proxies': None},
        {'proxy-groups': [{'name': 'EasyTier 保活'}], 'dns': {'nameserver-policy': 'x'}},
        {'proxies': {'a': 1}, 'proxy-groups': 'xy'},
        {'proxies': [{'name': ['unhashable']}]},
    ])
    document = deepcopy(document)
    for_agent = R.random() < 0.5
    version = R.choice([None, 'v1.19.30', 'v1.19.31', 'v1.19.34', 'alpha-abc', 'v1.20.0', 'v0.99.99', 5, 'v2.0.0'])
    config = R.choice([{'mihomo': {'easytier': value}}, {'mihomo': None}, None, 'x', {'mihomo': {'easytier': value}}])
    record('apply_easytier', [document, config, for_agent, version],
           lambda: (lambda r: r)(et.apply_easytier(document, config, for_agent=for_agent, core_version=version)),
           mutated=document)


path = Path(__file__).with_name('corpus.jsonl.gz')
text = ''.join(json.dumps(entry, ensure_ascii=False, allow_nan=True) + '\n' for entry in OUT)
with open(path, 'wb') as raw, gzip.GzipFile(fileobj=raw, mode='wb', compresslevel=9, mtime=0, filename='') as handle:
    handle.write(text.encode('utf-8'))
print(len(OUT), 'entries')
