#!/usr/bin/env python3
"""Generate a19_corpus.jsonl.gz: inputs and what common/linux_gateway.py makes of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/gateway/testdata/gen_a19.py [count]

Every value is synthetic (example.test domains, documentation addresses,
made-up tokens). One JSON object per line:

    {"fn": <name>, "args": {...}, "out": <result>} or {..., "err": [class, detail]}

detail is str(exc) for LinuxGatewayError, the code for RouterSplitError and
str(exc) for any other exception. Rule contents that are not inline come from
the row's "loader" map (rule set id -> text, missing -> ""); the network is
never used. "loads" lists the loader calls of an overlap row in order.
"auth" switches is_auth_enabled for apply/generate rows.
"""

import gzip
import json
import random
import sys
from copy import deepcopy
from pathlib import Path

assert sys.version_info[:2] == (3, 11), sys.version

import logging
logging.disable(logging.CRITICAL)

import yaml

from backend.common import auth as auth_module
from backend.common import linux_gateway as lg
from backend.common.router_split import RouterSplitError
from backend.converters.mihomo import generate_mihomo_config, get_mihomo_ruleset_downloads

COUNT = int(sys.argv[1]) if len(sys.argv) > 1 else 600
R = random.Random(20261117)
OUT = []
BOM = chr(0xFEFF)
REPLACEMENT = chr(0xFFFD)


def error_of(exc):
    if isinstance(exc, RouterSplitError):
        return ['RouterSplitError', exc.code]
    return [type(exc).__name__, str(exc)]


def record(fn, args, call, **extra):
    row = {'fn': fn, 'args': deepcopy(args)}
    row.update(extra)
    try:
        row['out'] = call()
    except Exception as exc:  # noqa: BLE001 - the class is part of the corpus
        row.pop('out', None)
        row['err'] = error_of(exc)
    json.dumps(row)  # must be JSON
    OUT.append(row)
    return row


def loader_of(mapping, calls=None):
    def load(item, library):
        if calls is not None:
            calls.append(item.get('id'))
        return mapping.get(item.get('id'), '')
    return load


def set_auth(flag):
    auth_module.is_auth_enabled = (lambda: flag)


# ---------------------------------------------------------------- settings

SETTINGS_VALUES = [
    None, {}, [], 'x', 5, True,
    {'enabled': True}, {'enabled': False}, {'enabled': 'false'}, {'enabled': 1}, {'enabled': None},
    {'enabled': True, 'rule_set_ids': []}, {'enabled': True, 'rule_set_ids': ['a', 'b', 'a']},
    {'rule_set_ids': 'r'}, {'rule_set_ids': [True]}, {'rule_set_ids': ['\x00']}, {'rule_set_ids': ['']},
    {'rule_set_ids': ['a\x7f']}, {'rule_set_ids': ['a\x1f']}, {'rule_set_ids': ['中文规则', 'emoji-\U0001F600']},
    {'rule_set_ids': ['x' * 256]}, {'rule_set_ids': ['x' * 257]}, {'rule_set_ids': ['é' * 256]},
    {'rule_set_ids': [str(i) for i in range(1024)]}, {'rule_set_ids': [str(i) for i in range(1025)]},
    {'rule_set_ids': None}, {'enabled': False, 'rule_set_ids': [1]}, {'enabled': True, 'extra': 1},
    {'enabled': 'yes', 'rule_set_ids': 5}, {'rule_set_ids': ['a', None]}, {'rule_set_ids': [['a']]},
]


def gen_settings():
    for value in SETTINGS_VALUES:
        record('normalize', {'value': value}, lambda value=value: lg.normalize_gateway_settings(value))


# ---------------------------------------------------------------- fake-ip range

FAKE_RANGES = [
    '198.18.0.1/16', '198.18.0.0/15', '198.19.0.1/24', '10.0.0.0/8', '10.0.0.0/7', '10.1.2.3/30', '10.1.2.3/31',
    '10.1.2.3', '0.0.0.0/8', '0.0.0.0/0', '127.0.0.1/8', '127.0.0.0/9', '120.0.0.0/5', '224.0.0.0/8',
    '224.0.0.0/4', '169.254.0.0/16', '169.254.1.0/24', '169.0.0.0/8', '::/0', 'fd00::/8', 'invalid', '',
    '198.18.0.0/255.255.0.0', '198.18.0.0/0.0.255.255', '198.18.0.0/255.0.255.0', '198.018.0.0/16',
    '198.18.0.0/016', ' 198.18.0.0/16', '198.18.0.0/16 ', '198.18.0.0/ 16', '198.18.0.0/', '/16',
    '100.64.0.0/10', '240.0.0.0/4', '255.255.255.0/30', '1.2.3.4/29', '198.18.0.0/16/1',
    # test_mihomo_base_settings.py (through the FakeIPNetworkError hook)
    '10.10.0.1/16', '198.18.0.1/15', '198.18.0.1/31', '127.0.0.1/16', '224.0.0.1/8', '169.254.0.1/16',
    'fc00::1/64', ' 198.18.0.1/16',
]


def gen_fake_ranges():
    for value in FAKE_RANGES:
        record('fake_ip_range', {'value': value},
               lambda value=value: lg.fake_ip_network({'dns': {'fake-ip-range': value}}))
    docs = [
        {}, {'dns': None}, {'dns': {}}, {'dns': []}, {'dns': ''}, {'dns': 'x'}, {'dns': ['a']},
        {'dns': {'fake-ip-range': None}}, {'dns': {'fake-ip-range': 5}}, {'dns': {'fake-ip-range': ['10.0.0.0/8']}},
        {'dns': {'fake-ip-range': '198.20.0.0/16'}}, {'dns': {'enable': True}},
    ]
    for doc in docs:
        record('fake_ip_network', {'doc': doc}, lambda doc=doc: lg.fake_ip_network(doc))


# ---------------------------------------------------------------- extraction

V4_GOOD = ['1.1.1.0/24', '8.8.8.8', '9.9.9.7/24', '10.0.0.0/8', '100.64.0.0/10', '17.0.0.0/8', '17.1.0.0/16',
           '203.0.113.5/32', '198.51.100.0/24', '192.0.2.1', '1.2.3.0/255.255.255.0', '1.2.3.4/0.0.0.255',
           '1.2.3.4/024', '17.80.0.0/17', '114.114.114.0/24', '9.9.9.128/25']
V4_BAD = ['11.0.0.0/7', '0.0.0.0/1', '128.0.0.0/1', '0.0.0.0/0', '1.02.3.4/24', '1.2.3.999/24', '1.2.3.0/33',
          '224.0.0.0/3', '1.2.3.4/']
V4 = V4_GOOD + V4_BAD
V6 = ['2001:db8::/32', '2001:db8::1', '::ffff:1.2.3.4', 'fe80::1%eth0/64', 'fe80::1%eth0', '2001:db8::1/129',
      '::/0', '2001:0db8:0000::1/64']
WORDS = ['DOMAIN,example.test', 'DOMAIN-SUFFIX,example.test', 'SRC-IP-CIDR,10.0.0.0/8', 'GEOIP,US',
         'AND,((NETWORK,UDP),(IP-CIDR,8.8.8.8/32))', 'DOMAIN-REGEX,^x#y$', 'PROCESS-NAME,curl', 'MATCH']


def random_line(behavior):
    roll = R.random()
    comment = R.choice(['', '', '', ' # note', '#x'])
    if roll < 0.06:
        return R.choice(['', '   ', '# comment', '// comment', '; comment', '\t'])
    if behavior == 'ipcidr':
        if roll < 0.80:
            return R.choice(V4_GOOD) + comment
        if roll < 0.92:
            return R.choice(V6)
        if roll < 0.97:
            return R.choice(V4_BAD)
        return R.choice(['IP-CIDR,1.1.1.0/24', 'garbage', '1.1.1.0/24,no-resolve', 'example.test'])
    if roll < 0.6:
        kind = R.choice(['IP-CIDR', 'IP-CIDR', 'ip-cidr', 'IP-CIDR6', 'Ip-Cidr', ' IP-CIDR'])
        if kind.strip().upper() == 'IP-CIDR':
            value = R.choice(V4_GOOD) if R.random() < 0.93 else R.choice(V4_BAD + V6)
        else:
            value = R.choice(V6) if R.random() < 0.9 else R.choice(V4_GOOD)
        suffix = R.choice(['', '', '', ',no-resolve', ',NO-RESOLVE', ', no-resolve ', ',DIRECT', ',no-resolve,x'])
        return f'{kind},{value}{suffix}{comment}'
    return R.choice(WORDS) + comment


def random_content(behavior):
    lines = [random_line(behavior) for _ in range(R.randint(0, 8))]
    roll = R.random()
    if roll < 0.2:
        return 'payload:\n' + ''.join(f'  - {json.dumps(line, ensure_ascii=False)}\n' for line in lines)
    if roll < 0.25:
        return 'payload:\n' + ''.join(f'- {line}\n' for line in lines)
    if roll < 0.3:
        return json.dumps(lines, ensure_ascii=False)
    sep = R.choice(['\n', '\r\n', '\n', '\r'])
    text = sep.join(lines)
    if R.random() < 0.3:
        text += sep
    if R.random() < 0.1:
        text = BOM + text
    if R.random() < 0.05:
        text = '\n  \t' + text
    return text


FIXED_CONTENT = [
    ('0.0.0.0/0', 'ipcidr'), ('0.0.0.0/1\n128.0.0.0/1', 'ipcidr'), ('1.1.1.999/24', 'ipcidr'),
    ('1.1.1.0/33', 'ipcidr'), ('IP-CIDR,1.1.1.0/24,DIRECT', 'classical'), ('SRC-IP-CIDR,1.1.1.0/24', 'ipcidr'),
    ('payload: [1, 2]', 'ipcidr'), ('payload: [{IP: test}]', 'ipcidr'), ('\x00MRS', 'ipcidr'),
    ('payload: &a [*a]', 'ipcidr'), ('payload: []', 'ipcidr'), ('2001:db8::/32\n1.0.0.0/24', 'ipcidr'),
    ('payload:\n- IP-CIDR,1.1.1.7/24,no-resolve\n- IP-CIDR6,2001:db8::/32,no-resolve\n- SRC-IP-CIDR,10.0.0.0/8\n'
     '- DOMAIN,example.test\n- GEOIP,US\n- AND,((NETWORK,UDP),(IP-CIDR,8.8.8.8/32))\n'
     '- OR,((IP-CIDR,9.9.9.9/32),(DST-PORT,443))\n', 'classical'),
    ('IP-CIDR6,8.8.8.8/32', 'classical'), ('IP-CIDR,2001:db8::/32', 'classical'),
    ('0.0.0.0/1', 'ipcidr'), ('128.0.0.0/1', 'ipcidr'), ('128.0.0.0/2', 'ipcidr'), ('0.0.0.0/7', 'ipcidr'),
    ('\n'.join(f'{i}.0.0.0/8' for i in range(256)), 'ipcidr'), ('\n'.join(f'{i}.0.0.0/8' for i in range(255)), 'ipcidr'),
    ('1.1.1.0/24', 'domain'), ('', 'ipcidr'), ('', 'classical'), ('payload:', 'ipcidr'), ('---\n', 'ipcidr'),
    ('[1.1.1.0/24, 8.8.8.8]', 'ipcidr'), ('- 1.1.1.0/24\n- 8.8.8.8', 'ipcidr'), ('- 2001:0:0:1', 'ipcidr'),
    ('payload: [a', 'ipcidr'), ('payload:\n  - 1.1.1.0/24\n1: x', 'ipcidr'), ('x\npayload: [1.1.1.0/24]', 'ipcidr'),
    ('  payload : ["9.9.9.0/24"]', 'ipcidr'), ('payloads: [1.1.1.0/24]', 'ipcidr'), (REPLACEMENT, 'ipcidr'),
    (BOM + '1.1.1.0/24', 'ipcidr'), ('1.1.1.0/24 # a,b', 'ipcidr'), ('1.1.1.0/24,', 'ipcidr'),
    ('IP-CIDR,1.1.1.0/24,no-resolve # x', 'classical'), ('IP-CIDR', 'classical'), ('IP-CIDR,', 'classical'),
    ('IP-CIDR6,fe80::1%eth0/64', 'classical'), ('fe80::1%eth0', 'ipcidr'), ('fe80::1%/64', 'ipcidr'),
    ('1.1.1.1%eth0', 'ipcidr'), ('//1.1.1.0/24\n;2.2.2.0/24\n3.3.3.0/24', 'ipcidr'),
    ('1.1.1.0/24\x0b2.2.2.0/24', 'ipcidr'), ('1.1.1.0/24\x0c2.2.2.0/24', 'ipcidr'),
    ('\n'.join(['8.8.8.8'] * 1000), 'ipcidr'),
    ('payload\n: [x]\n', 'classical'), ('1.1.1.0/24\rpayload: x', 'classical'),
] + [
    # White space around "payload" that is not ASCII (review F3): YAML for
    # Python, so an error rather than an empty capture.
    (text, behavior)
    for ws in ('\u00a0', '\u3000', '\x0b', '\x0c', '\x85', '\u2028', '\x1c', '\x1f', ' ', '\t')
    for text in (ws + 'payload:\n  - IP-CIDR,1.2.3.0/24\n', 'payload' + ws + ':\n  - IP-CIDR,1.2.3.0/24\n',
                 '# x\n' + ws + 'payload: ["IP-CIDR,1.2.3.0/24"]\n')
    for behavior in ('classical', 'ipcidr')
]


def gen_extract(count):
    for content, behavior in FIXED_CONTENT:
        record('extract', {'content': content, 'behavior': behavior},
               lambda c=content, b=behavior: lg.extract_gateway_cidrs(c, b))
    for _ in range(count):
        behavior = R.choice(['ipcidr', 'ipcidr', 'classical', 'classical'] * 4 + ['domain'])
        content = random_content(behavior if behavior != 'domain' else 'ipcidr')
        record('extract', {'content': content, 'behavior': behavior},
               lambda c=content, b=behavior: lg.extract_gateway_cidrs(c, b))


# ---------------------------------------------------------------- configurations

def snapshot(*, enabled=True, selected=None):
    return {
        'profile_id': 'home',
        'system_config': {'server_domain': 'https://config.example', 'config_token': 'a+b&c'},
        'mihomo': {'custom_config': 'mixed-port: 7890\n',
                   'linux_gateway': {'enabled': enabled, 'rule_set_ids': ['r'] if selected is None else selected}},
        'rule_configs': [{'id': 'r', 'itemType': 'ruleset', 'name': 'Proxy IPs',
                          'library_rule_id': 'lib', 'behavior': 'ipcidr', 'policy': 'DIRECT', 'enabled': True}],
        'rule_library': [{'id': 'lib', 'name': 'Proxy IPs', 'behavior': 'ipcidr',
                          'source_type': 'content', 'content': '1.1.1.0/24\n8.8.8.8', 'enabled': True}],
    }


def overlap_snapshot():
    config = snapshot(selected=['media'])

    def ruleset(rule_id, name, policy, content, behavior='ipcidr', **extra):
        config['rule_configs'].append({'id': rule_id, 'itemType': 'ruleset', 'name': name,
                                       'library_rule_id': 'lib-' + rule_id, 'behavior': behavior,
                                       'policy': policy, 'enabled': True})
        config['rule_library'].append({'id': 'lib-' + rule_id, 'name': name, 'behavior': behavior,
                                       'source_type': 'content', 'content': content, 'enabled': True, **extra})
    config['rule_configs'], config['rule_library'] = [], []
    ruleset('early', '前置直连', 'DIRECT', '17.1.0.0/16')
    ruleset('media', 'Apple', 'Proxy', 'DOMAIN-SUFFIX,apple.com\nIP-CIDR,17.0.0.0/8,no-resolve\nIP-CIDR,9.9.9.0/24',
            'classical')
    ruleset('proxy-late', '其他代理', 'Proxy', '17.2.0.0/16')
    ruleset('china', 'ChinaMax', 'DIRECT', '17.80.0.0/17\n17.81.0.0/16\n114.114.114.0/24', 'classical')
    ruleset('lan-set', 'Lan', 'DIRECT', 'IP-CIDR,224.0.0.0/3,no-resolve\nIP-CIDR,9.9.9.9/32,no-resolve', 'classical')
    config['rule_configs'].append({'id': 'lan', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '9.9.9.128/25',
                                   'policy': 'DIRECT', 'enabled': True})
    config['rule_configs'].append({'id': 'match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'Proxy',
                                   'enabled': True})
    return config


CUSTOM_CONFIGS = [
    'mixed-port: 7890\n', '', '   \n', 'mode: global\n',
    'dns: {fake-ip-range: 198.19.0.1/24, nameserver: [223.5.5.5]}\ntun:\n  mtu: 1400\n  route-address: [198.18.0.0/16]\n'
    '  inet4-route-address: [0.0.0.0/1]\n  route-exclude-address-set: [old]\n',
    'ipv6: true\ndns: {listen: "0.0.0.0:53", enhanced-mode: redir-host}\ntun: {enable: false, route-address: [10.0.0.0/8]}\n',
    'routing-mark: 100\n', 'routing-mark: 0\n', 'routing-mark: ""\n', 'routing-mark: false\n', 'routing-mark: null\n',
    'routing-mark: []\n', 'routing-mark: {}\n', 'routing-mark: "0"\n', 'routing-mark: 0.0\n',
    'proxies: [{name: a, type: direct, routing-mark: 100}]\n',
    'proxy-providers: {test: {override: {routing-mark: 100}}}\n',
    'dns: {fake-ip-range: 0.0.0.0/0}\n', 'dns: {fake-ip-range: "::/0"}\n', 'dns: {fake-ip-range: 127.0.0.1/8}\n',
    'dns: {fake-ip-range: invalid}\n', 'dns: x\n', 'dns: []\n', 'dns: {fake-ip-range: 10.0.0.0/8}\n',
    'tun: invalid\n', 'tun: []\n', 'tun: [1]\n', 'tun: {}\n', 'tun: {stack: gvisor, device: utun9, dns-hijack: [any:53]}\n',
    'tun: {stack: null}\n', 'tun: {exclude-interface: [eth0]}\n', 'tun: {include-interface: [eth1]}\n',
    'tun: {exclude-mac-address: ["00:11:22:33:44:55"]}\n', 'tun: {include-uid: [0]}\n', 'tun: {include-uid: []}\n',
    'tun: {include-package: ""}\n', 'tun: {exclude-package: [x]}\n', 'tun: {include-android-user: [0]}\n',
    'rule-providers: {cf_gateway_x: {type: inline, behavior: ipcidr, payload: []}}\n',
    'rule-providers: null\n', 'rule-providers: []\n',
    'rule-providers: {Proxy IPs: {type: http, behavior: ipcidr, url: "https://rules.example/ip.txt", format: text, '
    'interval: 86400, path: ./ruleset/ip.list}}\n',
    'rule-providers: {Proxy IPs: null}\n', 'rule-providers: {Proxy IPs: x}\n',
    '[1, 2]\n', 'a: [\n', 'just text\n', '1: x\ndns: {fake-ip-range: 198.20.0.0/16}\n',
    'x: &a {routing-mark: 5}\ny: *a\n', 'when: 2024-01-02\nraw: !!binary aGVsbG8=\n',
    'mode: rule\nproxies: [{name: p, type: http, server: proxy.example.test, port: 8080}]\n',
]


def random_rules(policies=('DIRECT', 'DIRECT', 'Proxy', 'REJECT')):
    """A random profile: rule sets (ipcidr/classical/domain, inline/url, MRS, disabled, missing library)."""
    config = {'profile_id': R.choice(['home', '', 'p-1', None]),
              'system_config': {}, 'mihomo': {}, 'rule_configs': [], 'rule_library': []}
    if R.random() < 0.9:
        config['system_config']['server_domain'] = R.choice(
            ['https://config.example', 'https://config.example///', '', 'http://192.0.2.10:5000/sub/'])
    if R.random() < 0.8:
        config['system_config']['config_token'] = R.choice(['a+b&c', '', 'tök en', 't'])
    if config['profile_id'] is None:
        del config['profile_id']
    ids = []
    for index in range(R.randint(0, 7)):
        rule_id = R.choice([f'r{index}', f'规则 {index}', f'r/{index}?x#y', f'r%{index}'])
        ids.append(rule_id)
        behavior = R.choice(['ipcidr', 'ipcidr', 'classical', 'domain', None])
        item = {'id': rule_id, 'itemType': R.choice(['ruleset'] * 8 + ['rule', 'group']),
                'name': R.choice([f'Set {index}', f'集合{index}', '']),
                'policy': R.choice(policies),
                'enabled': R.choice([True, True, True, False])}
        if R.random() < 0.15:
            item['library_enabled'] = R.choice([True, False])
        if behavior is not None:
            item['behavior'] = behavior
        if item['itemType'] == 'rule':
            item['rule_type'] = R.choice(['IP-CIDR', 'IP-CIDR', 'DOMAIN', 'MATCH'])
            item['value'] = R.choice(V4 + ['example.test', ' 9.9.9.0/24 '])
        source = item
        if R.random() < 0.6:
            library = {'id': 'lib-' + rule_id, 'name': R.choice([f'Lib {index}', f'库{index}', item['name']]),
                       'enabled': R.choice([True, True, True, False])}
            if R.random() < 0.8:
                library['behavior'] = R.choice(['ipcidr', 'classical', 'domain'])
            item['library_rule_id'] = library['id']
            if R.random() < 0.92:
                config['rule_library'].append(library)
            source = library
        if R.random() < 0.6:
            source['source_type'] = 'content'
            source['content'] = random_content(R.choice(['ipcidr', 'classical']))
        else:
            source['source_type'] = 'url'
            source['url'] = R.choice(['https://rules.example.test/ip.txt', 'https://rules.example.test/ip.mrs?x=1',
                                      'https://rules.example.test/IP.MRS', 'https://ip.mrs', '/api/local/rule'])
            if R.random() < 0.1:
                source['format'] = 'mrs'
        config['rule_configs'].append(item)
    loader = {}
    for rule_id in ids:
        if R.random() < 0.8:
            loader[rule_id] = random_content(R.choice(['ipcidr', 'classical']))
    selected = [rule_id for rule_id in ids if R.random() < 0.5]
    if R.random() < 0.2:
        selected.append('deleted')
    config['mihomo']['custom_config'] = R.choice(CUSTOM_CONFIGS[:8])
    config['mihomo']['linux_gateway'] = {'enabled': R.random() < 0.85, 'rule_set_ids': selected}
    if R.random() < 0.05:
        config['mihomo']['linux_gateway'] = R.choice([None, {'enabled': 'x'}, []])
    return config, ids + ['deleted', 'unknown'], loader


def gen_providers(count):
    fixed = []
    config = snapshot()
    fixed.append((config, ['r'], {}))
    for edit in ('rename', 'disable-item', 'disable-lib', 'off', 'no-rules', 'mrs', 'domain', 'comments', 'single',
                 'classical', 'classical-regex', 'url-empty', 'url', 'nullcontent', 'numcontent'):
        config = snapshot()
        loader = {}
        if edit == 'rename':
            config['rule_library'][0].update(name='Renamed', content='9.9.9.0/24')
        elif edit == 'disable-item':
            config['rule_configs'][0]['enabled'] = False
        elif edit == 'disable-lib':
            config['rule_library'][0]['enabled'] = False
        elif edit == 'off':
            config['mihomo']['linux_gateway']['enabled'] = False
        elif edit == 'no-rules':
            config['rule_configs'] = []
        elif edit == 'mrs':
            config['rule_library'][0]['url'] = 'https://rules.test/ip.mrs?foo=1'
        elif edit == 'domain':
            config['rule_library'][0]['behavior'] = 'domain'
        elif edit == 'comments':
            config['rule_library'][0]['content'] = '# comments\n1.1.1.0/24 # comment\n8.8.8.8\n'
        elif edit == 'single':
            config['rule_library'][0]['content'] = '8.8.8.8\n2001:db8::1\n9.9.9.7/24\n'
        elif edit == 'classical':
            config['rule_library'][0].update(behavior='classical', content='IP-CIDR,8.8.8.8,no-resolve\nIP-CIDR6,2001:db8::1\n')
        elif edit == 'classical-regex':
            config['rule_library'][0].update(behavior='classical', content='DOMAIN-REGEX,^x#y$\nIP-CIDR,1.1.1.0/24 # comment\n'
                                             'IP-CIDR ,2.2.2.0/24\n ip-cidr , 3.3.3.3 , no-resolve\nIP-CIDR6,::ffff:1.2.3.4\n')
        elif edit in ('url-empty', 'url'):
            source = config['rule_library'][0]
            source.update(source_type='url', url='https://rules.example/ip.txt')
            source.pop('content')
            if edit == 'url':
                loader = {'r': '5.5.5.0/24\n6.6.6.6'}
        elif edit == 'nullcontent':
            config['rule_library'][0]['content'] = None
        elif edit == 'numcontent':
            config['rule_library'][0]['content'] = 5
        fixed.append((config, ['r', 'deleted'], loader))
    for _ in range(count):
        config, ids, loader = random_rules()
        fixed.append((config, R.sample(ids, min(len(ids), 3)), loader))
    for config, ids, loader in fixed:
        for rule_id in ids:
            args = {'config': config, 'rule_id': rule_id, 'loader': loader}
            record('provider_payload', args, lambda: lg.gateway_provider_payload(config, rule_id,
                                                                                content_loader=loader_of(loader)))
            record('provider_yaml', args, lambda: lg.gateway_provider_yaml(config, rule_id,
                                                                          content_loader=loader_of(loader)))
            record('source_yaml', args, lambda: lg.gateway_source_yaml(config, rule_id,
                                                                      content_loader=loader_of(loader)))
            for source in (False, True):
                record('provider_path', {'config': config, 'rule_id': rule_id, 'source': source},
                       lambda: lg.provider_path(config, rule_id, source=source))
            record('provider_name', {'rule_id': rule_id}, lambda: lg.provider_name(rule_id))


def gen_overlap(count):
    configs = []
    config = overlap_snapshot()
    config['rule_library'][3]['content'] = 'IP-CIDR,17.80.0.0/17\nIP-CIDR,17.81.0.0/16\nIP-CIDR,114.114.114.0/24'
    configs.append((config, {}))
    config = overlap_snapshot()
    configs.append((config, {}))
    config = overlap_snapshot()
    config['rule_library'][3]['content'] = 'IP-CIDR,114.114.114.0/24'
    config['rule_library'][4]['content'] = 'IP-CIDR,224.0.0.0/3,no-resolve'
    config['rule_configs'] = [item for item in config['rule_configs'] if item['id'] != 'lan']
    configs.append((config, {}))
    config = overlap_snapshot()
    config['mihomo']['linux_gateway']['enabled'] = False
    configs.append((config, {}))
    config = overlap_snapshot()
    config['rule_library'][4]['content'] = 'IP-CIDR,224.0.0.0/3,no-resolve'
    config['rule_configs'] = [item for item in config['rule_configs'] if item['id'] != 'lan']
    config['rule_library'][3].update(source_type='url', url='https://rules.example/china.list')
    configs.append((config, {}))
    config = overlap_snapshot()
    config['rule_library'][3].update(source_type='url', url='https://rules.example/china.list')
    configs.append((config, {'china': 'IP-CIDR,17.0.0.0/8\nIP-CIDR,17.0.0.0/8'}))
    config = overlap_snapshot()
    config['rule_configs'].append({'id': 'same', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '17.0.0.0/8',
                                   'policy': 'DIRECT'})
    config['rule_configs'].append({'id': 'wide', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '16.0.0.0/6',
                                   'policy': 'DIRECT'})
    configs.append((config, {}))
    config = overlap_snapshot()
    config['rule_configs'].insert(0, config['rule_configs'].pop(-1))
    config['mihomo']['linux_gateway']['rule_set_ids'] = ['media', 'china', 'early']
    configs.append((config, {}))
    for _ in range(count):
        config, ids, loader = random_rules()
        configs.append((config, loader))
    for config, loader in configs:
        calls = []
        record('overlap', {'config': config, 'loader': loader},
               lambda: lg.direct_overlap_warnings(config, content_loader=loader_of(loader, calls)), loads=calls)


def gen_editor(count):
    configs = [snapshot(), snapshot(selected=['deleted']), snapshot(enabled=False), overlap_snapshot()]
    config = snapshot()
    config['rule_library'][0]['url'] = 'https://rules.test/ip.mrs?foo=1'
    configs.append(config)
    for custom in CUSTOM_CONFIGS:
        config = snapshot()
        config['mihomo']['custom_config'] = custom
        configs.append(config)
    for _ in range(count):
        configs.append(random_rules()[0])
    for config in configs:
        for guidance in (True, False):
            record('editor_state', {'config': config, 'guidance': guidance},
                   lambda: lg.gateway_editor_state(config, include_guidance=guidance))


def gen_validation(count):
    cases = []
    # test_router_split.py::test_rule_set_id_validation_is_shared_with_gateway_selection
    profile = {'mihomo': {'custom_config': 'mixed-port: 7890\n'}, 'rule_configs': [
        {'id': 'ip', 'itemType': 'ruleset', 'behavior': 'ipcidr', 'source_type': 'content', 'content': '1.1.1.0/24'},
        {'id': 'domain', 'itemType': 'ruleset', 'behavior': 'domain'}]}
    for ids, previous in ((['ip'], []), (['domain', 'gone'], ['domain', 'gone']), (['domain'], []), (['gone'], [])):
        record('validate_ids', {'config': profile, 'ids': ids, 'previous': previous},
               lambda: lg.validate_rule_set_ids(profile, ids, set(previous)))
        cases.append((profile, {'enabled': True, 'rule_set_ids': ids}))
    config = snapshot()
    config['rule_library'][0]['url'] = 'https://rules.test/ip.mrs?foo=1'
    config['mihomo']['linux_gateway']['rule_set_ids'] = []
    cases += [(config, {'enabled': True, 'rule_set_ids': ['r']}), (config, {'enabled': True, 'rule_set_ids': ['other-profile']})]
    config = snapshot()
    config['rule_library'][0]['behavior'] = 'domain'
    cases += [(config, {'enabled': False, 'rule_set_ids': ['r']}), (config, {'enabled': True, 'rule_set_ids': ['r']})]
    for custom in CUSTOM_CONFIGS:
        config = snapshot()
        config['mihomo']['custom_config'] = custom
        cases.append((config, config['mihomo']['linux_gateway']))
    for marker in ({'version': 1, 'takeover': 'full_tun', 'route_rule_set_ids': [], 'origin': 'setup'}, {'version': 2}):
        config = snapshot()
        config['mihomo']['router_split'] = deepcopy(marker)
        cases.append((config, config['mihomo']['linux_gateway']))
        cases.append((config, {'enabled': False}))
    for value in SETTINGS_VALUES:
        cases.append((snapshot(), value))
    for _ in range(count):
        config, ids, _ = random_rules()
        value = {'enabled': R.random() < 0.8, 'rule_set_ids': R.sample(ids, min(len(ids), R.randint(0, 3)))}
        cases.append((config, value))
    for config, value in cases:
        record('validate_selection', {'config': config, 'value': value},
               lambda: lg.validate_gateway_selection(config, value))
        try:
            settings = lg.normalize_gateway_settings(value)
        except lg.LinuxGatewayError:
            continue
        previous = R.sample(settings['rule_set_ids'], R.randint(0, len(settings['rule_set_ids'])))
        record('validate_ids', {'config': config, 'ids': settings['rule_set_ids'], 'previous': previous},
               lambda: lg.validate_rule_set_ids(config, settings['rule_set_ids'], set(previous)))


def gen_apply(count):
    cases = []
    for custom in CUSTOM_CONFIGS:
        for auth in (False, True):
            config = snapshot()
            config['mihomo']['custom_config'] = custom
            cases.append((config, auth, ''))
    nodes_cases = [[{'name': 'n', 'routing-mark': 7}], [{'name': 'n', 'routing-mark': 0}], {'x': {'routing-mark': 1}},
                   None, [], [[{'routing-mark': 'x'}]]]
    for nodes in nodes_cases:
        config = snapshot()
        config['nodes'] = nodes
        cases.append((config, False, ''))
    for edit in ('no-token', 'empty-ids', 'no-domain', 'no-profile', 'slashes', 'deleted', 'off', 'v6host', 'badhost'):
        config = snapshot()
        base = 'http://192.0.2.10:5000/'
        if edit == 'no-token':
            config['system_config']['config_token'] = ''
        elif edit == 'empty-ids':
            config['system_config']['config_token'] = ''
            config['mihomo']['linux_gateway']['rule_set_ids'] = []
        elif edit == 'no-domain':
            config['system_config']['server_domain'] = ''
        elif edit == 'no-profile':
            del config['profile_id']
        elif edit == 'slashes':
            config['system_config']['server_domain'] = 'https://config.example/sub///'
        elif edit == 'deleted':
            config['mihomo']['linux_gateway']['rule_set_ids'] = ['r', 'deleted']
        elif edit == 'off':
            config['mihomo']['linux_gateway']['enabled'] = False
        elif edit == 'v6host':
            config['system_config']['server_domain'] = 'http://[2001:db8::1]:5000'
        elif edit == 'badhost':
            config['system_config']['server_domain'] = 'http://[2001:db8::1:5000'
        for auth in (False, True):
            cases.append((config, auth, base))
    for _ in range(count):
        config, _, _ = random_rules()
        config['mihomo']['custom_config'] = R.choice(CUSTOM_CONFIGS)
        cases.append((config, R.random() < 0.3, R.choice(['', 'http://192.0.2.10:5000/'])))
    for config, auth, base in cases:
        try:
            doc = yaml.safe_load(config['mihomo'].get('custom_config') or '{}')
        except yaml.YAMLError:
            continue
        if not isinstance(doc, dict):
            continue
        try:
            json.dumps(doc)
        except (TypeError, ValueError):
            continue
        if any(not isinstance(key, str) for key in doc):
            continue
        set_auth(auth)
        work = deepcopy(doc)
        record('apply', {'doc': doc, 'config': config, 'base_url': base, 'auth': auth},
               lambda: (lg.apply_linux_gateway(work, config, base), work)[1])
    set_auth(False)


def gen_generate(count):
    cases = []
    for custom in CUSTOM_CONFIGS[:12] + ['tun: {exclude-interface: [eth0]}\n']:
        for enabled in (True, False):
            config = snapshot(enabled=enabled)
            config['mihomo']['custom_config'] = custom
            cases.append((config, False))
    for marker in ({'version': 1, 'takeover': 'full_tun', 'route_rule_set_ids': [], 'origin': 'setup'}, {'version': 2}):
        for enabled in (True, False):
            config = snapshot(enabled=enabled)
            config['mihomo']['router_split'] = deepcopy(marker)
            cases.append((config, False))
    config = snapshot()
    config['system_config']['config_token'] = ''
    cases.append((config, True))
    config = snapshot()
    config['system_config']['config_token'] = ''
    config['mihomo']['linux_gateway']['rule_set_ids'] = []
    cases.append((config, True))
    for selected in ([], ['deleted']):
        cases.append((snapshot(selected=selected), False))
    for _ in range(count):
        config, _, _ = random_rules(policies=('DIRECT', 'DIRECT', 'REJECT'))
        for item in config['rule_configs']:
            if item.get('itemType') == 'rule':
                item['itemType'] = 'ruleset'
        cases.append((config, R.random() < 0.2))
    for config, auth in cases:
        set_auth(auth)
        record('generate', {'config': config, 'auth': auth},
               lambda: generate_mihomo_config(deepcopy(config), preflight_providers=False))
        record('downloads', {'config': config, 'auth': auth},
               lambda: get_mihomo_ruleset_downloads(deepcopy(config)))
    set_auth(False)


def gen_routing_mark():
    values = [{'routing-mark': 1}, {'a': [{'b': {'routing-mark': 'x'}}]}, [{'routing-mark': 0}, {'routing-mark': 2}],
              {'routing-mark': 0.0}, {'routing-mark': False}, {'routing-mark': ''}, {'routing-mark': None},
              {'routing-mark': []}, {'routing-mark': {}}, {'x': {'routing-mark': 0, 'y': {'routing-mark': 3}}},
              'text', 5, None, {'proxies': [{'name': 'a'}, {'name': 'b', 'routing-mark': -1}]}]
    for value in values:
        record('routing_mark', {'value': value, 'root': '节点'}, lambda: lg._routing_mark_path(value, '节点'))


def main():
    gen_settings()
    gen_fake_ranges()
    gen_routing_mark()
    gen_extract(COUNT * 3)
    gen_providers(COUNT // 3)
    gen_overlap(COUNT // 2)
    gen_editor(COUNT // 6)
    gen_validation(COUNT // 3)
    gen_apply(COUNT // 2)
    gen_generate(COUNT // 6)
    path = Path(__file__).with_name('a19_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(entry, ensure_ascii=False) for entry in OUT) + '\n'
    with open(path, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts = {}
    for entry in OUT:
        key = entry['fn'] + (':err' if 'err' in entry else ':ok')
        counts[key] = counts.get(key, 0) + 1
    print(f'{len(OUT)} rows -> {path}')
    for key in sorted(counts):
        print(f'  {key}: {counts[key]}')


if __name__ == '__main__':
    main()
