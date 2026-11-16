#!/usr/bin/env python3
"""Generate review_c12.jsonl.gz: edge inputs of the C12 adversarial review.

Run from the repository root on the test server (CPython 3.11, backend venv):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/review_c12_gen.py

Same line format as gen.py ({"fn", "args", "out"|"err", "mutated"}); the
fake Sub-Store converter is the same. Functions whose order matters record it
(listen_ports_ordered: list(owners.items()) with repr() keys; used_ports_repr:
the set in iteration order with repr()). Every value is synthetic.
"""

import datetime
import gzip
import json
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
from backend.utils.proxy_utils import fix_proxy_fields

OUT = []


def fake_convert(proxy_string, target='ClashMeta'):
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


def conv(value, seen=None):
    seen = seen or set()
    if isinstance(value, (dict, list)) and id(value) in seen:
        return '<recursive>'
    if isinstance(value, dict):
        seen = seen | {id(value)}
        return {(k if isinstance(k, str) else '<key>' + json.dumps(k) if isinstance(k, (int, float, bool, type(None)))
                 else '<key>' + repr(k)): conv(v, seen) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        seen = seen | {id(value)}
        return [conv(v, seen) for v in value]
    if isinstance(value, (set, frozenset)):
        return {'__set__': sorted((conv(v) for v in value), key=lambda v: json.dumps(v, sort_keys=True))}
    if isinstance(value, bytes):
        return {'__bytes__': value.hex()}
    if isinstance(value, (datetime.date, datetime.datetime)):
        return {'__date__': str(value)}
    if isinstance(value, float) and (value != value or value in (float('inf'), float('-inf'))):
        return {'__float__': repr(value)}
    return value


def record(fn, args, call, *, mutated=None):
    entry = {'fn': fn, 'args': deepcopy(args)}
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - every exception is recorded
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = conv(result)
    if mutated is not None:
        entry['mutated'] = conv(mutated)
    OUT.append(entry)


# ------------------------------------------------------------------ common

LOGIC = ['((ıN-PORT,80/443))', '((İN-PORT,80/443))', '((DſT-PORT,1,2))', '((ſRC-PORT,1/2))', '((NETWORK,udp))',
         '((NETWOR\u212a,udp))', '((network , TCP ),(DEST-PORT , 1,2))', '((NETWORK,\u2028udp\u2029))',
         '((NETWORK,\u180eudp))', '((NETWORK,\u200budp))', '((IN-PORT,\u3000 80/81 ))', '((dEsT-pOrT,8,9))',
         '((NETWORK,uDP),(NETWORK,TcP))', '((NETWORK,udpx))', '((NETWORK,\x1cudp\x1f))']
for value in LOGIC:
    for rule_type in ('AND', 'OR', 'NOT', 'DST-PORT', 'NETWORK'):
        record('network_as_protocol', [rule_type, value], lambda: common.network_as_protocol(rule_type, value))
        record('has_port_list', [rule_type, value], lambda: common.has_port_list(rule_type, value))
for value in ['ß', 'ǰ', 'ﬀ', 'ΐ', 'ᾳ', 'ᾀ', 'ŉ', 'İ', 'ı', 'ﬆudp', 'ʼn', 'ꭰ', '\U00010428', 'ǅ', '\u1e9e',
              '\u2c65', 'udp\u3000', '\u0345', 'ﬓ', 'ffi\ufb03', '\U0001e922', '\u10d0', 'ᏸ']:
    record('network_as_protocol', ['NETWORK', value], lambda: common.network_as_protocol('NETWORK', value))

for section in ['[General]\nipv6 = true\u3000', '[General]\n\u2028ipv6 = true', '[General]\nloglevel=x\x1c\x1d',
                '[General]\n\u0085ipv6=true', 'a\r\nipv6 = 1', '[General]\nIPV6 = true']:
    for key in ['ipv6', 'a+b', '(x)', 'ı', 'skip-proxy']:
        record('set_general_option', [section, key, 'v'], lambda: common.set_general_option(section, key, 'v'))

for text in ['example.test 192.0.2.1', '192.0.2.1\u00a0example.test', 'example.test\u3000fe80::1%eth0',
             'example.test ::1%', 'example.test ::1%a%b', 'example.test １９２.0.2.1', 'example.test 192.0.2.01',
             'example.test ::ffff:192.0.2.1', 'example.test 1:2:3:4:5:6:7:192.0.2.1', 'example.test ::192.0.2.1',
             'example.test fe80::1%' + 'z' * 60, '# c\nexample.test 192.0.2.1 # x', 'a\x1cb 192.0.2.2\x1d',
             '192.0.2.3 x.test\u2028y.test 192.0.2.4', 'x.test 1::2::3', 'x.test 12345::1', 'x.test ::',
             'x.test 0:0:0:0:0:0:0:0', 'x.test 1:2:3:4:5:6:7:8:9', 'x.test :1:2:3:4:5:6:7', 'x.test 1:2:3:4:5:6:7:',
             'x.test ::1:2:3:4:5:6:7', 'x.test 1:2:3:4:5:6:7::', 'x.test 1.2.3.4.5', 'x.test 256.1.1.1',
             'x.test \U0001d7cf.2.3.4']:
    record('parse_hosts_pairs', [text], lambda: common.parse_hosts_pairs(text))

for url in ['https://example.test/A.YAML\u0130', 'https://example.test/İ.yaml', 'https://example.test/a.\u212aMRS',
            'https://example.test/a.MRS', 'https://ex\u2100ample.test/a.yaml', 'https://[fe80::1%25eth0]/a.yml',
            'https://[::1]x/a.yaml', '\x00 https://example.test/a.yaml', 'https://example.test/a.yaml\t']:
    rule = {'name': 'r', 'url': url}
    record('ruleset_format', [rule, {}, None], lambda: common.ruleset_format(rule, {}, None))

# ------------------------------------------------------------------ nodes

PROXIES = ['https://gh.example.test', '\u3000gh.example.test\u2028', 'gh.example.test/', 'http://gh.example.test', '']
URLS = ['https://GİTHUB.COM/a', 'https://GITHUB.COM/a', 'https://github.com./a', 'https://user@github.com:443/a',
        'https://raw.githubusercontent.com/a', 'https://[::1]/a', 'https://github.com\u2100/a', 'https://github.com%41/a',
        'https://ap\u0130.github.com/a', 'https://api.github.com/x?u=https://github.com/', 'https://[github.com]/a',
        'https://gist.githubusercontent.com', '//github.com/a', 'github.com/a']
for proxy in PROXIES:
    for url in URLS:
        config = {'system_config': {'github_proxy_domain': proxy}}
        record('apply_github_proxy_domain', [url, config], lambda: mihomo.apply_github_proxy_domain(url, config))


def listen_ordered(document):
    return [[repr(k), v] for k, v in mihomo.mihomo_listen_ports(document).items()]


def used_ordered(document):
    return [repr(p) for p in mihomo.mihomo_used_ports(document)]


PORT_DOCS = [
    {'port': True, 'socks-port': 1},
    {'port': 1, 'socks-port': True},
    {'port': False},
    {'mixed-port': 7890, 'listeners': [{'name': 'a', 'port': True}]},
    {'listeners': [{'name': 'a', 'port': 7895}, {'name': 'b', 'port': 7896}]},
    {'listeners': [{'name': 'a', 'port': 7896}, {'name': 'b', 'port': 7895}]},
    {'listeners': [{'name': 'a', 'port': p} for p in (9090, 7890, 7891, 53, 1053, 7894)]},
    {'port': 7890, 'listeners': [{'name': 'a', 'port': p} for p in (8, 16, 24, 1, 9, 17)]},
    {'port': 7890.0, 'socks-port': 7890},
    {'port': 10 ** 30, 'external-controller': '0.0.0.0:' + '9' * 25},
    {'external-controller': ':９０９０'},
    {'external-controller': ':²'},
    {'external-controller': '127.0.0.1:١٢٣'},
    {'external-controller': ':\U00011f50'},
    {'external-controller': ':\U0001e4f1'},
    {'external-controller': ':\u2460'},
    {'external-controller': ':7890', 'port': 7890},
    {'external-controller': '[::1]:9090', 'listeners': [{'name': 'x', 'port': 9090}]},
    {'listeners': [{'name': 'configflow-region-probe', 'port': 17999}, {'port': '7890'}, 'x', {'port': 7.0}]},
]
import random
R = random.Random(20261116)
for _ in range(600):
    document = {}
    for key in ('port', 'socks-port', 'mixed-port', 'redir-port', 'tproxy-port'):
        if R.random() < 0.4:
            document[key] = R.choice([R.randint(1, 65535), R.randint(7890, 7899), R.randint(1, 64), True, 7890.0])
    if R.random() < 0.8:
        pool = R.choice([(1, 65535), (7890, 7960), (1, 40), (53, 60)])
        document['listeners'] = [{'name': f'l{i}', 'port': R.randint(*pool)} for i in range(R.randint(0, 40))]
    if R.random() < 0.3:
        document['external-controller'] = f'0.0.0.0:{R.randint(1, 65535)}'
    PORT_DOCS.append(document)
for document in PORT_DOCS:
    record('listen_ports_ordered', [document], lambda: listen_ordered(document))
    record('used_ports_repr', [document], lambda: used_ordered(document))

NODE_STRINGS = [
    'type: ss\n1: a\nname: x', 'type: ss\nyes: 1', 'type: ss\n~: 1', 'type: ss\nopts: {1: a, b: c}',
    'type: ss\n1.5: a', 'type: ss\nx: &a [*a]', 'type: ss\nx: !!set {a, b}', 'type: ss\nx: !!binary aGk=',
    'type: ss\nx: 2001-02-03', 'type: ss\nx: 2001-02-03T04:05:06Z',
    'type: vless\nws-opts: &o {path: /}\nreality-opts: *o', 'type: vless\nreality-opts: &o {a: 1}\nother: *o',
    'type: ss\nport: 123456789012345678901234567890', 'type: ss\nport: 0x1F', 'type: ss\nport: 1_000',
    'type: ss\nport: 0o17', 'type: ss\nport: 017', 'type: ss\nport: 1:30', 'type: ss\nport: .inf', 'type: ss\nport: .nan',
    '{"type": "ss", "port": 1e400}', '{"type": "ss", "port": NaN}', '{"type": "ss", "a": 1, "a": 2}',
    '{"type": "ss", "name": "\\ud83d\\ude00"}', '{"type": "", "x": 1}', '{"type": 0}', '{"type": [1]}', '[1, 2]',
    '{"type": "ss",}', '{type: ss, name: İı}', 'name: 🚀 节点\ntype: ss', 'type: ss\nname: "\\u0130"',
    'a: b://c\ntype: ss', 'ss://YWJj@h.test:1#x', 'SS://abc', 'x y://abc\ntype: ss', 'x:y://z', 'ss://\ntype: ss',
    '  type: ss\n  name: indented', 'type: ss\n\tname: tab', 'type: ss\nname: [unclosed', 'type: ss\n---\ntype: vmess',
    '- type: ss', 'type', '', '   ', '\ufefftype: ss', 'type: ss\u2028name: x', 'type: ss\x85name: x',
    'type: ss\nname: x\x00', 'type: !!str 5', 'type: !!int "5"', 'type: !!float 1', 'type: !!bool yes', 'type: null',
    'type: ~', 'type: ss\nname: &n x\nalias: *n', 'type: ss\n<<: {a: 1}', 'type: ss\n<<: [{a: 1}, {b: 2}]',
    'type: ss\nport: 1e3', 'type: ss\nport: +5', 'type: ss\nport: -0', 'type: ss\nport: 0b101', 'type: ss\nport: 190:20:30',
    'type: ss\nx: "\\x41\\u00e9"', "type: ss\nx: 'it''s'", 'type: ss\nx: |\n  a\n  b\n', 'type: ss\nx: >-\n  a\n  b\n',
    'type: hysteria2\npassword: "p:w"', '{"type":"ss","port":1}\n', 'type: ss\nx: {a: 1, a: 2}', 'type: ss\nType: x',
]
for text in NODE_STRINGS:
    record('parse_structured_proxy_string', [text], lambda: mihomo._parse_structured_proxy_string(text))
    node = {'name': 'Node İ', 'proxy_string': text}
    record('convert_node_to_mihomo', [node], lambda: mihomo.convert_node_to_mihomo(node))
    rnode = {'id': 'n', 'name': 'n', 'proxy_string': text}
    record('raw_dialer', [rnode], lambda: dr.raw_dialer(rnode))

STRUCTURED = [
    {'name': 'a', 'type': 'İPSEC'}, {'name': 'a', 'type': 'SOC\u212aS5'}, {'name': 'a', 'type': '\u1e9eS'},
    {'name': 'a', 'type': 'ΣS'}, {'name': 'a', 'type': 'ss', 'params': ['ab']},
    {'name': 'a', 'type': 'ss', 'params': [{'x': 1, 'y': 2}]}, {'name': 'a', 'type': 'ss', 'params': [[1, 'x']]},
    {'name': 'a', 'type': 'ss', 'params': [[True, 'x'], [None, 'y'], [1.5, 'z']]},
    {'name': 'a', 'type': 'ss', 'params': [['a', 1], ['a', 2]]}, {'name': 'a', 'type': 'ss', 'params': [[['k'], 1]]},
    {'name': 'a', 'type': 'ss', 'params': ['abc']}, {'name': 'a', 'type': 'ss', 'params': 'ab'},
    {'name': 'a', 'type': 'ss', 'params': ''}, {'name': 'a', 'type': 'ss', 'params': []},
    {'name': 'a', 'type': 'ss', 'params': [[]]}, {'name': 'a', 'type': 'ss', 'params': [{}]},
    {'name': 'a', 'type': 'ss', 'params': [{'k': 1}]}, {'name': 'a', 'type': 'ss', 'params': 5},
    {'name': 'a', 'type': 'ss', 'params': None}, {'name': 'a', 'type': 'ss', 'params': True},
    {'name': 'a', 'type': 'ss', 'params': 1.5}, {'name': 'a', 'type': 'ss', 'params': ['ab', 'cd', [1]]},
    {'name': 'a', 'type': 'vless', 'params': {'encryption': False}},
    {'name': 'a', 'type': 'vless', 'params': {'encryption': 0, 'reality-opts': {'short-id': None}}},
    {'name': 'a', 'type': 'VLESS', 'params': {'encryption': 'zero', 'reality-opts': []}},
    {'type': 'ss', 'server': None, 'port': None, 'params': {'name': 'over', 'type': 'x'}},
    {'name': 5, 'type': 'ss'}, {'name': None, 'type': 'ss', 'proxy_string': ''},
    {'name': 'a', 'proxy_string': 5}, {'name': 'a', 'proxy_string': ['x']}, {'name': 'a', 'proxy_string': 'vless://x'},
    {'name': 'a', 'proxy_string': 'empty://x'}, {'name': 'a', 'proxy_string': 'fail://x'},
]
for node in STRUCTURED:
    record('convert_node_to_mihomo', [node], lambda: mihomo.convert_node_to_mihomo(node))

for proxy in [{'type': 'vless', 'encryption': False}, {'type': 'vless', 'encryption': 0}, {'type': 'vless', 'encryption': []},
              {'type': 'VLESS'}, {'type': 'vless', 'reality-opts': {'short-id': None}}, {}, {'type': 'vless', 'encryption': 'zero '}]:
    p = deepcopy(proxy)
    record('fix_proxy_fields', [proxy], lambda: fix_proxy_fields(p), mutated=p)

# ------------------------------------------------------------------ dialers


def node(i, **extra):
    item = {'id': f'n{i}', 'name': f'N{i}'}
    item.update(extra)
    return item


def group(i, **extra):
    item = {'id': f'g{i}', 'name': f'G{i}'}
    item.update(extra)
    return item


CHAIN = {'type': 'chain', 'chain': {'entry': {'type': 'node', 'id': 'n1'}, 'exit': {'type': 'node', 'id': 'n2'}}}
DEEP = 3000
PROFILES = {
    'deep_include': {'nodes': [node(1), node(2, dialer_ref={'type': 'group', 'id': 'g0'})],
                     'proxy_groups': [group(i, include_groups=[f'g{i + 1}']) for i in range(DEEP)] +
                                     [group(DEEP, manual_nodes=['n1'])]},
    'deep_cycle': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g0'})],
                   'proxy_groups': [group(i, include_groups=[f'g{(i + 1) % 500}']) for i in range(500)]},
    'self_group': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                   'proxy_groups': [group(1, include_groups=['g1'])]},
    'mixed_sort': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                   'proxy_groups': [group(1, use=['S1', 'S2'])],
                   'subscriptions': [{'id': 5, 'name': 'S1'}, {'id': 's', 'name': 'S2'}]},
    'num_sort': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                 'proxy_groups': [group(1, use=['S1', 'S2', 'S3'])],
                 'subscriptions': [{'id': 10, 'name': 'S1'}, {'id': 9, 'name': 'S2'}, {'id': 9.5, 'name': 'S3'}]},
    'str_sort': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                 'proxy_groups': [group(1, subscriptions=['b', 'B', 'a', 'é', 'z', '\U0001f600', '\uffff'])],
                 'subscriptions': [{'id': x, 'name': x} for x in ['b', 'B', 'a', 'é', 'z', '\U0001f600', '\uffff']]},
    'follow_unhashable_late': {'nodes': [node(1)],
                               'proxy_groups': [group(1, follow_group='g9'), dict(group(9), **CHAIN),
                                                group(2, follow_group=[1])]},
    'follow_unhashable_early': {'nodes': [node(1), node(2)],
                                'proxy_groups': [group(2, follow_group=[1]), group(1, follow_group='g9'),
                                                 dict(group(9), **CHAIN)]},
    'follow_unhashable_dup': {'nodes': [node(1), node(2)],
                              'proxy_groups': [group(2, follow_group=[1]), group(2), dict(group(9), **CHAIN)]},
    'follow_unhashable_member_err': {'nodes': [node(1), node(2)],
                                     'proxy_groups': [group(1, include_groups=['gx']), group(2, follow_group={}),
                                                      dict(group(9), **CHAIN)]},
    'raw_dialer_names': {'nodes': [node(1, params={'dialer-proxy': 'N2'}), node(2), node(3, params={'dialer-proxy': 'DIRECT'})]},
    'raw_dialer_yaml': {'nodes': [node(1, proxy_string='type: ss\ndialer-proxy: N2\n1: x'), node(2)]},
    'raw_dialer_recursive': {'nodes': [node(1, proxy_string='type: ss\ndialer-proxy: N2\nx: &a [*a]'), node(2)]},
    'unicode_names': {'nodes': [node(1, name='İ', params={'dialer-proxy': 'i̇'}), node(2, name='i̇')]},
    'use_unhashable_ambiguous': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                                 'proxy_groups': [group(1, use=['S'])],
                                 'subscriptions': [{'id': [1], 'name': 'S'}, {'id': [2], 'name': 'S'}]},
    'use_unhashable_single': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                              'proxy_groups': [group(1, use=['S'])],
                              'subscriptions': [{'id': [1], 'name': 'S'}]},
    'agg_ids': {'nodes': [node(1, dialer_ref={'type': 'group', 'id': 'g1'})],
                'proxy_groups': [group(1, aggregations=['a1'])],
                'subscription_aggregations': [{'id': 'a1', 'name': 'A', 'nodes': [10, 9, True], 'subscriptions': [1.0]}]},
}
for label, profile in PROFILES.items():
    record('validate_dialers', [profile], lambda: dr.validate_dialers(profile))
    record('dependency_graph', [profile], lambda: [[list(k), [list(c) for c in v]] for k, v in dr.dependency_graph(profile).items()])
    record('incoming_dialers', [profile, 'group', 'g1'], lambda: dr.incoming_dialers(profile, 'group', 'g1'))
    record('dependency_node_ids', [profile, ['n1'], False], lambda: dr.dependency_node_ids(profile, ['n1'], False))

# resolve_dialer laziness, overlay, reject_surge laziness
for profile, nd in [({'nodes': []}, {'id': 'n', 'dialer_ref': {'type': 'node'}}),
                    ({'nodes': [{'id': 'x', 'name': 'X'}]}, {'id': 'n', 'dialer_ref': {'type': 'node'}}),
                    ({'proxy_groups': []}, {'id': 'n', 'dialer_ref': {'type': 'group'}}),
                    ({'nodes': 'ab'}, {'id': 'n', 'dialer_ref': {'type': 'node', 'id': 'a'}}),
                    ({'nodes': [{'id': 'a'}]}, {'id': 'n', 'dialer_ref': {'type': 'node', 'id': 'a'}})]:
    record('resolve_dialer', [profile, nd], lambda: dr.resolve_dialer(profile, nd))

SURGE = [
    {'proxy_groups': [dict(group(1), **CHAIN), 'bad']},
    {'proxy_groups': [{'type': 'chain', 'enabled': False}, 'bad']},
    {'nodes': [node(1, params={'dialer-proxy': 'x'}), 'bad']},
    {'nodes': [node(1, proxy_string='dial-x://a'), 'bad']},
    {'nodes': [node(1, enabled=False), 'bad']},
    {'nodes': [node(1, proxy_string='vless://ok'), node(2, proxy_string='fail://x')]},
]
for profile in SURGE:
    record('reject_surge_dialers', [profile, 'Loon'], lambda: dr.reject_surge_dialers(profile, 'Loon'))

# filter / provider names
CAND = [{'name': 'HK 01', 'type': 'İSS'}, {'name': 'hk 02', 'type': 'ss'}, {'name': 'JP', 'type': 'VMESS'},
        {'name': 'x', 'type': 'SOC\u212aS5'}]
for include, exclude, etype in [('', '', 'i̇ss'), ('', '', 'İSS'), ('', '', 'socks5'), ('(?i)hk', '', ''),
                                ('HK`JP', '02', 'vmess|'), ('a{4294967295}', '', ''), ('[', '', ''), ('', '(', ''),
                                ('\\N{LATIN SMALL LETTER H}', '', ''), ('', '', '|'), ('ĸ', '', '')]:
    record('filter_provider_candidates', [CAND, include, exclude, etype],
           lambda: dr.filter_provider_candidates(deepcopy(CAND), include, exclude, etype))
for proxy, override in [({'name': 5}, {'proxy-name': [{'pattern': '[', 'target': 'x'}]}),
                        ({'name': 5}, {}), ({'name': 'a'}, {'proxy-name': [{'pattern': 5, 'target': 'x'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(b)', 'target': '<$١>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(b)', 'target': '<$\U00011f51>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(b)', 'target': '<$１>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(b)', 'target': '<$2>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(b)', 'target': '<$99999999999999999999>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '(x)?b', 'target': '<$1>'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': '', 'target': '-'}]}),
                        ({'name': 'abc'}, {'proxy-name': {'a': 1}}), ({'name': 'abc'}, {'proxy-name': 'xy'}),
                        ({'name': 'abc'}, {'proxy-name': 7}), ({'name': 'abc'}, {'proxy-name': None}),
                        ({'name': 'abc'}, {'additional-prefix': 1}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': 'b'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': 'z'}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': 'b', 'target': 5}]}),
                        ({'name': 'abc'}, {'proxy-name': [{'pattern': 'a{99999999999}', 'target': 'x'}]})]:
    record('provider_proxy_name', [proxy, override], lambda: dr.provider_proxy_name(proxy, override))

EMITTED = [
    ({'proxies': [{'name': 'A', 'dialer-proxy': 'Missing'}], 'proxy-providers': {'F': 1}}, True, None, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': {'F': 1}}, True, None, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': {'F': 1}}, False, None, False),
    ({'proxies': [{'name': 'A', 'dialer-proxy': 'B'}, 'x']}, False, None, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': {'F': {}}}, False, {'F': [{'name': 'p', 'dialer-proxy': 'A'}, 'bad']}, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': {'F': {}}}, True, {'F': [{'name': 'p'}, 'bad']}, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': {'F': {}}}, True, {'F': [{'name': [1]}, {}]}, False),
    ({'proxies': [{'name': 'A'}], 'proxy-providers': []}, True, None, False),
    ({'proxies': [{'name': 'A', 'dialer-proxy': 'A'}], 'proxy-providers': []}, False, None, False),
    ({'proxies': [{'name': 'A', 'dialer-proxy': 'X'}], 'proxy-groups': [], 'proxy-providers': {'F': {'override': {'dialer-proxy': [1]}}}},
     False, None, False),
    ({'proxies': [{'name': 'A'}], 'proxy-groups': [{'name': 'G', 'proxies': ['A'], 'use': ['F']}],
      'proxy-providers': {'F': {'override': {'dialer-proxy': 'A'}}}}, False, {'F': [{'name': 'p'}]}, False),
    ({'proxies': [{'name': 'A'}], 'proxy-groups': [{'name': 'G', 'proxies': ['DIRECT'], 'exclude-type': 'direct'}],
      'proxy-providers': {}}, True, None, True),
]
for config, force, payloads, require in EMITTED:
    record('validate_emitted', [config, force, payloads, require],
           lambda: dr.validate_emitted(config, force_chains=force, provider_proxies=payloads, require_providers=require))

# ------------------------------------------------------------------ easytier

BASE = {'enabled': True, 'options': {'network-name': 'n', 'peers': ['tcp://192.0.2.1:11010']}}


def with_(**changes):
    value = deepcopy(BASE)
    for key, item in changes.items():
        if key.startswith('o_'):
            value['options'][key[2:].replace('_', '-')] = item
        else:
            value[key] = item
    return value


SETTINGS = [
    with_(name='İ' * 64), with_(name='🚀' * 64), with_(name='🚀' * 65), with_(name='\u3000x\u2028'), with_(name='a\x7f'),
    with_(name='a,b'), with_(name=''), with_(name=None), with_(name=5),
    with_(routes=['１０.0.0.0/8']), with_(routes=['10.0.0.0/０８']), with_(routes=['10.0.0.0/255.0.0.0']),
    with_(routes=['10.0.0.0/0.0.0.255']), with_(routes=['10.1.2.3/0']), with_(routes=['10.1.2.3/00000024']),
    with_(routes=['10.1.2.3/255.0.255.0']), with_(routes=['::/0']), with_(routes=['fe80::1%eth0/64']),
    with_(routes=['10.0.0.0/8/8']), with_(routes=['10.0.0.0/']), with_(routes=['10.0.0.0/33']), with_(routes=['10.0.0.0/-1']),
    with_(routes=['10.0.0.0/ 8']), with_(routes=['10.0.0.0/8', '10.0.0.0/8', '']), with_(routes=['10.0.0.0->10.1.0.0/16']),
    with_(routes=['010.0.0.0/8']), with_(routes=['10.0.0.0/8\u3000']), with_(routes=['10.0.0.0/255.255.255.255']),
    with_(routes=['10.0.0.0/0.0.0.0']), with_(routes=['10.0.0.0/255.255.0.255']), with_(routes=['10.0.0.0/+8']),
    with_(routes=['10.0.0.0/8_0']), with_(routes=['10.0.0.0/٨']),
    with_(o_ipv4='10.144.144.10/24'), with_(o_ipv4='10.144.144.10/255.255.255.0'), with_(o_ipv4='fe80::1'),
    with_(o_ipv4='10.144.144.256'), with_(o_ipv4='１0.1.1.1'), with_(o_ipv4='10.1.1.1%x'),
    with_(o_hostname='İ'), with_(o_hostname='a' * 63), with_(o_hostname='a' * 64), with_(o_hostname='-a'), with_(o_hostname='a_b'),
    with_(o_mtu=True), with_(o_mtu=1000.0), with_(o_mtu=575), with_(o_mtu=9000), with_(o_mtu=10 ** 30),
    with_(o_routing_mark=2 ** 32 - 1), with_(o_routing_mark=2 ** 32), with_(o_routing_mark=-1),
    with_(o_peers=['FA\u212aETCP://x']), with_(o_peers=['TCP://x']), with_(o_peers=['tcp://']), with_(o_peers=['tcpx']),
    with_(o_peers=['ftp://x']), with_(o_peers=['ws://x'] * 70), with_(o_peers='tcp://x'), with_(o_peers=[None, 'tcp://x']),
    with_(o_peers=[5]), with_(o_exit_nodes=['10.0.0.1%eth0']), with_(o_exit_nodes=['::1']), with_(o_exit_nodes=['10.0.0.1']),
    with_(o_proxy_networks=['192.168.1.5/24']), with_(o_ip_version='IPV4'), with_(o_ip_version=['dual']),
    with_(o_udp=1), with_(o_udp=None), with_(o_network_secret='s' * 4096), with_(o_network_secret='s' * 4097),
    with_(o_unknown=1, o_zeta=2, o_Alpha=3), with_(options={'é': 1, 'z': 2, 'E': 3}),
    with_(keepalive={'enabled': True, 'url': 'https://x.test/\u3000'}), with_(keepalive={'enabled': True, 'url': 'https://x.test/\u2028a'}),
    with_(keepalive={'enabled': True, 'url': 'HTTPS://x.test'}), with_(keepalive={'enabled': True, 'url': 'https://x.test', 'interval': 10.0}),
    with_(keepalive={'enabled': True, 'url': 'https://x.test', 'interval': True}), with_(keepalive={'enabled': True, 'url': 'https://x.test', 'interval': 86401}),
    with_(keepalive=0), with_(keepalive=[]), with_(keepalive=[1]), with_(keepalive={'enabled': 1}),
    with_(o_tld_dns_zone='...'), with_(o_tld_dns_zone='.ET.NET.'), with_(magic_dns=True, o_tld_dns_zone='..a..'),
    with_(apply_to='ALL'), with_(apply_to=['all']), with_(enabled=1), with_(o_listeners=['tcp://0.0.0.0:1'], o_no_listener=True),
    with_(o_peers=[], o_no_listener=False), with_(o_local_public_key='k'),
]
for value in SETTINGS:
    record('normalize_settings', [value], lambda: et.normalize_settings(value))
    record('redact_settings', [value], lambda: et.redact_settings(value))

APPLY = [
    ({'proxies': [{'name': 5}], 'proxy-groups': []}, with_(name='5'), False, None),
    ({'proxies': [{'name': [1]}]}, with_(), False, None),
    ({'proxies': {'EasyTier': 1}}, with_(), False, None),
    ({'proxies': 'EasyTier'}, with_(), False, None),
    ({'proxies': None}, with_(), False, None),
    ({'proxies': [], 'proxy-groups': {'x': 1}}, with_(keepalive={'enabled': True, 'url': 'https://x.test'}), False, None),
    ({'rules': {'a': 1}}, with_(routes=['10.0.0.0/8']), False, None),
    ({'rules': 'ab'}, with_(routes=['10.0.0.0/8']), False, None),
    ({'rules': 5}, with_(routes=['10.0.0.0/8']), False, None),
    ({'dns': {'nameserver-policy': None}}, with_(magic_dns=True), False, None),
    ({'dns': {'nameserver-policy': []}}, with_(magic_dns=True), False, None),
    ({'dns': {'nameserver-policy': {'+.et.net': 'x'}}}, with_(magic_dns=True), False, None),
    ({}, with_(apply_to='agent'), True, 'v1.19.30'), ({}, with_(apply_to='agent'), True, 'v1.19.31'),
    ({}, with_(apply_to='agent'), True, 'alpha-123'), ({}, with_(apply_to='agent'), True, 5),
    ({}, with_(apply_to='agent'), True, 'v1.18.999'), ({}, with_(apply_to='agent'), True, 'v2.0.0'),
    ({}, with_(apply_to='agent'), True, 'v１.19.30'), ({}, with_(apply_to='agent'), True, 'v1.19.30-beta'),
    ({'proxies': [{'name': 'D'}]}, with_(o_dialer_proxy='D'), False, None),
    ({'proxies': []}, with_(o_dialer_proxy='DIRECT'), False, None),
    ({'proxies': []}, with_(o_dialer_proxy='REJECT'), False, None),
]
for document, settings, for_agent, core in APPLY:
    doc = deepcopy(document)
    config = {'mihomo': {'easytier': settings}}
    record('apply_easytier', [document, config, for_agent, core],
           lambda: et.apply_easytier(doc, config, for_agent=for_agent, core_version=core), mutated=doc)

target = Path(sys.argv[1] if len(sys.argv) > 1 else 'server/internal/convert/testdata/review_c12.jsonl.gz')
with gzip.GzipFile(target, 'wb', mtime=0) as fh:
    for entry in OUT:
        fh.write((json.dumps(entry, ensure_ascii=False, sort_keys=False, allow_nan=False) + '\n').encode())
print(len(OUT), 'rows ->', target)
