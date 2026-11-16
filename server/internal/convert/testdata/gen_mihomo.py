#!/usr/bin/env python3
"""Generate mihomo_corpus.jsonl.gz: profiles and what Python's C18 functions make of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/gen_mihomo.py [count]

Every value is synthetic (example.test / invalid domains, documentation
addresses, made-up secrets). One JSON object per line:

    {"fn": "generate", "args": {...}, "caches": {...}, "downloads": {...},
     "out": yaml | "err": [class, message], "report": {...}, "mutated": profile}
    {"fn": "ruleset_downloads" | "provider_downloads", ...}
    {"fn": "validate_base_settings", "args": [...], "out": null | "err": [...]}
    {"fn": "<helper>", "args": [...], "out": ... | "err": [...]}

Sub-Store is fake_convert (mirrored in the Go test). Subscription caches and
download caches are served from the row (load_subscription_cache,
_get_cache_path and the repository read are patched); the hosts-index memo is
cleared per row. Provider preflight is off (provider delivery has its own
corpus); the Linux gateway is never enabled (its hook belongs to the agents
domain). The base-settings hooks of other domains are deterministic fakes
(fake_ip_network, mihomo_dns_loop), mirrored in the Go test.
"""

import datetime
import gzip
import json
import os
import random
import sys
import tempfile
from copy import deepcopy
from pathlib import Path

assert sys.version_info[:2] == (3, 11), sys.version

import logging
logging.disable(logging.CRITICAL)

import yaml

from backend.converters import mihomo
from backend.common import mihomo_base_settings as mbs
from backend.common import linux_gateway, gateway_onboarding
from backend.utils import sub_store_client, subscription_cache, subscription_hosts
from backend.utils.subscription_refresh import _download_cache_path
import backend.common.config as config_module

COUNT = int(sys.argv[1]) if len(sys.argv) > 1 else 4000
R = random.Random(20261116)
OUT = []
TMP = tempfile.mkdtemp(prefix='c18-corpus-')


def fake_convert(proxy_string, target='ClashMeta'):
    """Stand-in for Sub-Store: deterministic on the text (same as gen.py)."""
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

STATE = {'caches': {}, 'downloads': {}}


def fake_cache_path(sub_id):
    return os.path.join(STATE['dir'], str(sub_id))


def set_caches(caches, downloads):
    """Serve this row's caches; a cache file exists exactly for the cached ids."""
    STATE['caches'], STATE['downloads'] = caches, downloads
    STATE['dir'] = tempfile.mkdtemp(dir=TMP)
    for sub_id in caches:
        Path(STATE['dir'], str(sub_id)).write_text('x')
    subscription_hosts._index.clear()


def fake_load_cache(sub_id):
    return deepcopy(STATE['caches'].get(sub_id))


STATE['dir'] = TMP
subscription_cache._get_cache_path = fake_cache_path
subscription_cache.load_subscription_cache = fake_load_cache


class FakeRepository:
    def read_shared_json(self, path):
        if path in STATE['downloads']:
            return deepcopy(STATE['downloads'][path])
        raise FileNotFoundError(path)


config_module.get_repository = lambda *a, **k: FakeRepository()


def fake_ip_network(document):
    value = document['dns']['fake-ip-range']
    if 'bad' in value or '/' not in value:
        raise linux_gateway.LinuxGatewayError('fake')
    return value


def fake_dns_loop(dns, host_addresses=()):
    hosts = ','.join(sorted(str(h) for h in host_addresses))
    for key in ('default-nameserver', 'nameserver', 'fallback', 'proxy-server-nameserver', 'direct-nameserver',
                'nameserver-policy', 'proxy-server-nameserver-policy'):
        values = dns.get(key)
        items = list(values.values()) if isinstance(values, dict) else (values if isinstance(values, list) else [values])
        for value in items:
            if isinstance(value, str) and 'loop' in value:
                return key, f'{value}|{hosts}'
    return None


linux_gateway.fake_ip_network = fake_ip_network
gateway_onboarding.mihomo_dns_loop = fake_dns_loop


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


def record(fn, args, call, **extra):
    entry = {'fn': fn, 'args': deepcopy(args), **extra}
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - the corpus records every exception
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = conv(result)
    OUT.append(entry)
    return entry


# ------------------------------------------------------------------ values

NODE_IDS = [f'n{i}' for i in range(7)]
GROUP_IDS = [f'g{i}' for i in range(5)]
SUB_IDS = ['s0', 's1', 's2']
AGG_IDS = ['a0', 'a1']
NAMES = ['HK 01', '🇭🇰 香港 01', 'US-02', 'JP', 'Relay', 'Entry', 'Exit', 'node: x', 'yes', '日本 #1', "it's", 'a"b', '-dash', '?q']
GROUP_NAMES = ['PROXY', '🚀 节点选择', 'Auto', '♻️ 自动选择', 'Fallback', 'LB', 'Chain', 'Media', 'Final', 'on']
REGEX = ['HK', '(?i)hk|香港', '^(?!.*(US|JP))', '(?<=港)\\d+', 'US`JP', '(', '日本|JP', '(?=.*0)', '']
RULE_TYPES = ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD', 'DOMAIN-WILDCARD', 'DOMAIN-REGEX', 'IP-CIDR', 'IP-CIDR6',
              'IP-SUFFIX', 'IP-ASN', 'GEOIP', 'GEOSITE', 'SRC-IP-CIDR', 'SRC-PORT', 'DST-PORT', 'IN-PORT', 'PROCESS-NAME',
              'NETWORK', 'AND', 'OR', 'NOT', 'RULE-SET', 'MATCH', 'SUB-RULE', 'IN-TYPE', 'DSCP']
RULE_VALUES = {
    'DOMAIN': ['a.example.test', 'xn--fsq.example.test', '例子.example.test'],
    'DOMAIN-SUFFIX': ['example.test', 'invalid'],
    'DOMAIN-KEYWORD': ['google', 'ads'],
    'DOMAIN-WILDCARD': ['*.example.test', 'a?.example.test'],
    'DOMAIN-REGEX': ['^ad\\d+\\.example\\.test$'],
    'IP-CIDR': ['192.0.2.0/24', '198.51.100.7/32', '10.0.0.0/8'],
    'IP-CIDR6': ['2001:db8::/32'],
    'IP-SUFFIX': ['192.0.2.1/8'],
    'IP-ASN': ['13335'],
    'GEOIP': ['CN', 'LAN', 'private'],
    'GEOSITE': ['cn', 'category-ads-all'],
    'SRC-IP-CIDR': ['192.168.1.0/24'],
    'SRC-PORT': ['8080', '1000-2000'],
    'DST-PORT': ['443', '80/443', '1000-2000'],
    'IN-PORT': ['7890'],
    'PROCESS-NAME': ['curl', 'Telegram.exe'],
    'NETWORK': ['udp', 'tcp'],
    'AND': ['((DOMAIN,a.example.test),(NETWORK,udp))', '((SRC-IP-CIDR,10.0.0.0/24),(IP-CIDR,10.0.0.0/24))',
            '((IP-CIDR,192.0.2.0/24,no-resolve),(DST-PORT,443))', '((GEOIP,CN),(NOT,((IP-ASN,13335))))'],
    'OR': ['((DOMAIN-SUFFIX,example.test),(IP-CIDR6,2001:db8::/32))', '((ip-cidr , 198.51.100.0/24))'],
    'NOT': ['((GEOIP,CN))', '((DOMAIN,a.example.test))'],
    'RULE-SET': ['Ads', 'Media'],
    'MATCH': [''],
    'SUB-RULE': ['(NETWORK,tcp)'],
    'IN-TYPE': ['HTTP'],
    'DSCP': ['4'],
}
RULESET_NAMES = ['Ads', 'Media', 'CN', 'Telegram', '国内', 'Local IP']
BASE_URLS = ['', 'http://192.168.1.2:5000', 'https://cf.example.test', 'http://[2001:db8::2]:8080']


def rand_policy(group_names, node_names):
    roll = R.random()
    if roll < 0.7 and group_names:
        return R.choice(group_names)
    if roll < 0.95 or not BAD[0]:
        return R.choice(['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS'])
    if node_names and R.random() < 0.5:
        return R.choice(node_names)
    return R.choice(['Gone Group', 'compatible'])


BAD = [False]


def rand_node(i, nid):
    name = R.choice(NAMES) + ('' if R.random() < 0.08 else f' {i}')
    node = {'id': nid, 'name': name}
    if R.random() < 0.9:
        node['enabled'] = R.random() > 0.1
    roll = R.random()
    if roll < 0.45:
        node['type'] = R.choice(['ss', 'vmess', 'trojan', 'http', 'socks5', 'vless', 'hysteria2', 'SS', 'tuic', 'wireguard'])
        node['server'] = R.choice([f'node{i}.example.test', '203.0.113.7', '2001:db8::7', 'relay.invalid'])
        node['port'] = R.choice([443, 8388, 80, 65535, '443'])
        params = {}
        if node['type'].lower() == 'ss':
            params.update({'cipher': 'aes-128-gcm', 'password': f'pw-{i}'})
        if node['type'].lower() in ('vmess', 'vless', 'tuic'):
            params['uuid'] = f'00000000-0000-4000-8000-00000000000{i}'
        if node['type'].lower() == 'vless':
            params.update(R.choice([{}, {'encryption': ''}, {'reality-opts': {'public-key': 'pk', 'short-id': ''}},
                                    {'reality-opts': {'public-key': 'pk'}}, {'encryption': 'zero', 'flow': 'xtls-rprx-vision'}]))
        if node['type'].lower() == 'trojan':
            params.update({'password': f'tr-{i}', 'sni': 'sni.example.test', 'skip-cert-verify': R.random() < 0.5})
        if R.random() < 0.3:
            params['udp'] = R.random() < 0.7
        if R.random() < 0.15:
            params['ws-opts'] = {'path': '/ws', 'headers': {'Host': 'cdn.example.test'}}
        if DIAL[0] and R.random() < 0.15:
            params['dialer-proxy'] = R.choice(NODE_IDS + GROUP_NAMES + ['DIRECT'])
        if R.random() < 0.08:
            params['ip-version'] = R.choice(['ipv4', 'dual'])
        node['params'] = params
        if R.random() < 0.03:
            del node['type']
    elif roll < 0.7:
        proxy = {'type': R.choice(['ss', 'trojan', 'vless']), 'server': f'json{i}.example.test', 'port': 8443,
                 'password': f'js-{i}'}
        if DIAL[0] and R.random() < 0.2:
            proxy['dialer-proxy'] = R.choice(GROUP_NAMES[:3] + NODE_IDS[:2])
        if proxy['type'] == 'vless':
            proxy['uuid'] = '00000000-0000-4000-8000-0000000000aa'
        if R.random() < 0.5:
            node['proxy_string'] = json.dumps(proxy, ensure_ascii=R.random() < 0.5)
        else:
            node['proxy_string'] = yaml.safe_dump(proxy, sort_keys=False)
    else:
        node['proxy_string'] = R.choice([f'ss://YWVzLTEyOC1nY206cHc@uri{i}.example.test:8388#u{i}',
                                         f'vless://00000000-0000-4000-8000-000000000001@v{i}.example.test:443?x=1',
                                         'fail://x', 'empty://y'] + ([f'trojan://pw@t{i}.example.test:443?dial-g0&x', f'vmess://dial-{R.choice(NODE_IDS)}'] if DIAL[0] else []))
    r = R.random()
    if DIAL[0] and r < 0.3:
        node['dialer_ref'] = R.choice([{'type': 'node', 'id': R.choice(NODE_IDS)}, {'type': 'group', 'id': R.choice(GROUP_IDS)},
                                       None, {'type': 'node', 'id': nid}])
    if R.random() < 0.05:
        node['subscription_id'] = R.choice(SUB_IDS)
    if R.random() < 0.02:
        node['_proxy_chain_id'] = 'c1'
    return node


def rand_group(gid, name, node_ids, group_ids, sub_ids, agg_ids):
    gtype = R.choice(['select', 'select', 'url-test', 'fallback', 'load-balance', 'relay', 'select'] + (['chain'] if DIAL[0] else []))
    group = {'id': gid, 'name': name, 'type': gtype}
    if R.random() < 0.9:
        group['enabled'] = R.random() > 0.08
    if gtype == 'chain':
        def ref():
            if R.random() < 0.6:
                return {'type': 'node', 'id': R.choice(node_ids or ['missing'])}
            return {'type': 'group', 'id': R.choice(group_ids)}
        group['chain'] = {'entry': ref(), 'exit': ref()}
        return group
    for key, values in (('manual_nodes', node_ids + ['DIRECT', 'REJECT']), ('include_groups', [g for g in group_ids if g != gid]),
                        ('subscriptions', sub_ids), ('aggregations', agg_ids)):
        if values and R.random() < 0.45:
            group[key] = R.sample(values, R.randint(0, min(3, len(values))))
    if R.random() < 0.15:
        group['proxies_order'] = [{'type': R.choice(['node', 'strategy', 'subscription', 'aggregation']),
                                   'id': R.choice(node_ids + group_ids + sub_ids + agg_ids + ['DIRECT'])}
                                  for _ in range(R.randint(0, 4))]
    if R.random() < 0.08:
        group['source'] = R.choice(['node', 'strategy'])
        group['proxies'] = R.sample(node_ids + group_ids, min(2, len(node_ids + group_ids)))
    if R.random() < 0.08:
        group['proxy_order'] = 'strategies_first'
    if R.random() < 0.07:
        others = [g for g in group_ids if g != gid]
        if others:
            group['follow_group'] = R.choice(others + ['gone'])
    for key, values in (('regex', REGEX), ('aggregation_regex', REGEX), ('exclude_regex', REGEX[:-1]),
                        ('aggregation_exclude_regex', REGEX[:-1])):
        if R.random() < 0.12:
            group[key] = R.choice(values)
    if R.random() < 0.3:
        group['url'] = R.choice(['https://www.gstatic.com/generate_204', 'http://cp.example.test/204', ''])
    if R.random() < 0.3:
        group['interval'] = R.choice([300, 0, 600, '300'])
    for key, values in (('tolerance', [50, 150]), ('lazy', [True, False, None]), ('strategy', ['consistent-hashing', 'round-robin', '']),
                        ('timeout', [5000]), ('max-failed-times', [3]), ('expected-status', ['204', '200/302']),
                        ('disable-udp', [True]), ('hidden', [True, False]), ('icon', ['https://icons.example.test/a.png']),
                        ('exclude-filter', ['US', '(?i)test']), ('exclude-type', ['vmess|ss'])):
        if R.random() < 0.08:
            group[key] = R.choice(values)
    if R.random() < 0.05:
        group['include_all'] = True
    return group


def rand_subscription(sid, name):
    sub = {'id': sid, 'name': name, 'url': R.choice([f'https://air.example.test/{sid}', f'https://air.example.test/{sid}?token=t-1',
                                                    ''])}
    if R.random() < 0.85:
        sub['enabled'] = R.random() > 0.1
    if R.random() < 0.2:
        sub['health_check_url'] = R.choice(['http://www.baidu.example.test', '  ', None, 'https://h.example.test/204'])
    if R.random() < 0.15:
        sub['health_check'] = R.choice([
            {'enable': True, 'url': 'https://www.gstatic.com/generate_204', 'interval': 600, 'lazy': True},
            {'enable': False},
            {'url': 'http://h.example.test/204', 'interval': 120, 'timeout': 3000, 'expected-status': '204'},
            {'interval': 5},
            {'bogus': 1},
            'x',
        ])
    if R.random() < 0.15:
        sub['regex'] = R.choice(REGEX)
    if R.random() < 0.1:
        sub['exclude_regex'] = R.choice(REGEX[:-1])
    return sub


def subscription_body(sid, names):
    proxies = []
    groups = []
    for i, name in enumerate(names):
        proxy = {'name': name, 'type': R.choice(['ss', 'trojan']), 'server': R.choice([f'{sid}-{i}.air.example.test', f'cdn{i}.example.test', '203.0.113.9']),
                 'port': 443, 'password': f'p{i}'}
        if R.random() < 0.25:
            proxy['dialer-proxy'] = R.choice(['Airport Relay', '中转', 'Gone', names[0]])
        proxies.append(proxy)
    if R.random() < 0.6:
        groups.append({'name': 'Airport Relay', 'type': R.choice(['select', 'url-test']), 'proxies': names[:2] or ['DIRECT'],
                       'url': 'http://cp.example.test/204', 'interval': 300})
    if R.random() < 0.3:
        groups.append({'name': '中转', 'type': 'select', 'use': ['x']})
    doc = {'proxies': proxies}
    if groups:
        doc['proxy-groups'] = groups
    if R.random() < 0.5:
        hosts = {}
        if R.random() < 0.4 and names:
            hosts[f'{sid}-0.air.example.test'] = R.choice(['alias.example.test', 'cdn.example.test', '198.51.100.8'])
        for i in range(R.randint(1, 3)):
            hosts[R.choice([f'{sid}-{i}.air.example.test', f'cdn{i}.example.test', '*.air.example.test', '+.example.test',
                            'other.example.test'])] = R.choice(['198.51.100.5', ['198.51.100.6', '2001:db8::6'], 'alias.example.test',
                                                                'localhost', '0.0.0.0', 'bad value'])
        doc['hosts'] = hosts
    return yaml.safe_dump(doc, allow_unicode=True, sort_keys=False), proxies


def rand_cache(sub):
    names = R.sample(['Air HK 1', 'Air US 2', 'Air JP 3', 'Air SG 4', '香港 IPLC'], R.randint(0, 4))
    text, proxies = subscription_body(sub['id'], names)
    nodes = sub_store_client.proxies_to_nodes(deepcopy(proxies))
    metadata = {'url': sub.get('url') if R.random() < 0.9 else 'https://other.example.test/'}
    hosts = subscription_hosts.extract_subscription_hosts(text)
    if hosts is not None:
        metadata['hosts'] = hosts
    from backend.utils.subscription_dialers import extract_subscription_dialers
    dialers = extract_subscription_dialers(text)
    if R.random() < 0.7:
        metadata['dialers'] = dialers
    elif sub.get('url') and R.random() < 0.7:
        STATE['downloads'][_download_cache_path(sub['url'])] = {'text': text, 'url': sub['url']}
    return {'nodes': nodes, 'metadata': metadata, 'updated_at': '2026-11-16T00:00:00'}


# ------------------------------------------------------------- custom config

def base_document():
    doc = mihomo._default_mihomo_template() if R.random() < 0.6 else {'mixed-port': R.choice([7890, 7891]), 'mode': 'rule'}
    roll = R.random
    if roll() < 0.3:
        doc['allow-lan'] = R.choice([True, False])
    if roll() < 0.2:
        doc['ipv6'] = R.choice([True, False, False, 'yes', 'false'] if roll() < 0.3 else [True, False])
    if roll() < 0.2:
        doc['find-process-mode'] = R.choice(['off', 'strict', ' Always ', True, False, 'OFF', 'bogus', None])
    if roll() < 0.2:
        doc['log-level'] = R.choice(['info', 'debug', 'silent'])
    if roll() < 0.2:
        doc['external-controller'] = R.choice(['0.0.0.0:9090', '127.0.0.1:9090', '[::1]:9090'])
        doc['secret'] = R.choice(['s3cret-pass', '123456', ''])
    if roll() < 0.25:
        doc['hosts'] = {R.choice(['nas.lan', 'router.lan', 'cdn0.example.test']): R.choice(['192.168.1.10', ['192.168.1.11']]),
                        'air-0.example.test': '198.51.100.1'}
    if roll() < 0.45:
        dns = doc.setdefault('dns', {'enable': True})
        if roll() < 0.4:
            dns['nameserver-policy'] = {'geosite:cn': ['223.5.5.5'], '+.example.test': 'https://dns.example.test/dns-query'}
        if roll() < 0.5:
            dns['proxy-server-nameserver'] = ['https://223.5.5.5/dns-query']
        if roll() < 0.2:
            dns['use-hosts'] = R.choice([True, False])
        if roll() < 0.2:
            dns['ipv6'] = R.choice([True, False, 'no'])
        if roll() < 0.2:
            dns['respect-rules'] = True
        if roll() < 0.15:
            dns['fake-ip-filter-mode'] = 'blacklist'
    if roll() < 0.25:
        doc['tun'] = {'enable': True, 'stack': R.choice(['mixed', 'gvisor']), 'auto-route': True, 'dns-hijack': ['any:53'],
                      'route-exclude-address': ['192.168.0.0/16']}
    if roll() < 0.2:
        doc['listeners'] = [{'name': 'in-socks', 'type': 'socks', 'port': R.choice([7892, 17999, 7890]), 'listen': '0.0.0.0',
                             'proxy': R.choice(['DIRECT', 'PROXY', 'Missing'])}]
        if roll() < 0.3:
            doc['listeners'].append({'name': 'configflow-region-probe', 'type': 'mixed', 'port': 17999})
    if roll() < 0.2:
        doc['sniffer'] = {'enable': True, 'sniff': {'HTTP': {'ports': [80, '8080-8880']}, 'TLS': {'ports': [443]}}}
    if roll() < 0.1:
        doc['profile'] = {'store-selected': True, 'store-fake-ip': True}
    if roll() < 0.1:
        doc['rules'] = ['DOMAIN,should.be.replaced,DIRECT']
    if roll() < 0.08:
        doc['proxy-groups'] = [{'name': 'Base Group', 'type': 'select', 'proxies': ['DIRECT']}]
    if roll() < 0.08:
        doc['proxies'] = [{'name': 'base-proxy', 'type': 'http', 'server': 'b.example.test', 'port': 1}]
    if roll() < 0.1:
        doc['global-ua'] = 'clash.meta'
    if roll() < 0.05:
        doc['text'] = R.choice(['multi\nline\ntext', 'yes', 'null', '0123', 'a: b', '  lead', '#hash', '中文 ✓', 'x' * 120,
                                'tab\there', '"quoted"', "it's", '- dash', '@at', '%pct', '!bang', '1e3', '0x1F', '12:30'])
    if roll() < 0.05:
        doc['number'] = R.choice([1.5, 1e20, -0.0, 10 ** 20, float('inf')])
    return doc


HAND_WRITTEN = [
    'mixed-port: 7890\nmode: rule\n# comment\ndns:\n  enable: true\n  nameserver: &ns\n    - 223.5.5.5\n  fallback: *ns\n',
    'base: &b {a: 1, b: [x, y]}\nother: *b\nmode: rule\n',
    'mixed-port: 7890\nfind-process-mode: off\nipv6: yes\n',
    'mixed-port: 7890\nipv6: "true"\n',
    'mixed-port: 7890\ndns: {enable: true, ipv6: on}\n',
    'date: 2026-11-16\nstamp: 2026-11-16 10:00:00+08:00\nmixed-port: 7890\n',
    'bin: !!binary aGVsbG8=\nmixed-port: 7890\n',
    'mixed-port: 7890\nmode: rule\ndns:\n  enable: true\n  nameserver-policy:\n    "geosite:cn": 223.5.5.5\n  bad indent\n',
    '- just\n- a list\n',
    'just a string\n',
    '# only a comment\n',
    '   \n',
    'mixed-port: 7890\nhosts:\n  "*.lan": 192.168.1.1\n  nas.lan: [192.168.1.2, 192.168.1.3]\n',
    'mixed-port: 7890\nlog-level: info\nexternal-controller: 0.0.0.0:9090\nsecret: ""\nproxies: []\nrules: []\n',
    'mixed-port: 0o17\nport: 0x1F\nsocks-port: 1_000\nredir-port: 1:30\n',
    'anchors:\n  - &x [1, 2]\n  - *x\n  - *x\nmode: rule\n',
    'mixed-port: 7890\n"key with: colon": v\n"yes": key\n',
    'mixed-port: 7890\nsniffer:\n  enable: true\n  sniff:\n    QUIC: {ports: [443]}\n',
    '{mixed-port: 7890, mode: rule, dns: {enable: true, listen: "0.0.0.0:1053"}}',
    'mixed-port: 7890\nlong: >\n  folded text\n  continues\nlit: |\n  line1\n  line2\n',
]


def rand_custom_config():
    roll = R.random()
    if roll < 0.15:
        return R.choice(['', None, '   '])
    if roll < 0.3:
        return R.choice(HAND_WRITTEN)
    doc = base_document()
    return yaml.safe_dump(doc, allow_unicode=R.random() < 0.7, sort_keys=R.random() < 0.2)


EASYTIER_VALID = {'enabled': True, 'name': 'EasyTier', 'routes': ['10.144.144.0/24'],
                  'options': {'network-name': 'home', 'network-secret': 's3cret', 'peers': ['tcp://203.0.113.10:11010'], 'udp': True}}


# ------------------------------------------------------------------ profiles

DIAL = [False]


def rand_profile():
    DIAL[0] = R.random() < 0.2
    BAD[0] = R.random() < 0.15
    node_ids = NODE_IDS[:R.randint(0, len(NODE_IDS))]
    nodes = [rand_node(i, nid) for i, nid in enumerate(node_ids)]
    if DIAL[0] and len(nodes) >= 2 and R.random() < 0.7:
        # A valid topology: node 0 is a plain relay, others dial through it.
        relay = nodes[0]
        relay.update({'enabled': True, 'name': 'Relay Node'})
        relay.pop('dialer_ref', None)
        relay.pop('proxy_string', None)
        relay.update({'type': 'ss', 'server': 'relay.example.test', 'port': 8388,
                      'params': {'cipher': 'aes-128-gcm', 'password': 'relay-pw'}})
        for node in nodes[1:]:
            node.pop('dialer_ref', None)
            if isinstance(node.get('params'), dict):
                node['params'].pop('dialer-proxy', None)
            if isinstance(node.get('proxy_string'), str) and 'dial' in node['proxy_string']:
                node['proxy_string'] = 'ss://YWVzOnB3@plain.example.test:1#p'
            if R.random() < 0.5:
                node['dialer_ref'] = {'type': 'node', 'id': relay['id']}
    group_ids = GROUP_IDS[:R.randint(0, len(GROUP_IDS))]
    names = R.sample(GROUP_NAMES, len(group_ids))
    sub_ids = SUB_IDS[:R.randint(0, len(SUB_IDS))]
    agg_ids = AGG_IDS[:R.randint(0, len(AGG_IDS))] if sub_ids or node_ids else []
    groups = [rand_group(gid, name, node_ids, group_ids, sub_ids, agg_ids) for gid, name in zip(group_ids, names)]
    subs = [rand_subscription(sid, R.choice(['Sub ' + sid, '机场' + sid, sid])) for sid in sub_ids]
    aggs = []
    for aid in agg_ids:
        agg = {'id': aid, 'name': R.choice(['Agg ' + aid, '聚合' + aid]), 'subscriptions': R.sample(sub_ids, R.randint(0, len(sub_ids))),
               'nodes': R.sample(node_ids, R.randint(0, min(2, len(node_ids))))}
        if R.random() < 0.85:
            agg['enabled'] = R.random() > 0.1
        if R.random() < 0.15:
            agg['health_check_url'] = 'http://agg.example.test/204'
        aggs.append(agg)
    profile = {'nodes': nodes, 'proxy_groups': groups}
    if subs or R.random() < 0.5:
        profile['subscriptions'] = subs
    if aggs or R.random() < 0.3:
        profile['subscription_aggregations'] = aggs

    group_names = [g['name'] for g in groups if g.get('enabled', True)]
    node_names = [n['name'] for n in nodes]
    library = []
    for i, name in enumerate(R.sample(RULESET_NAMES, R.randint(0, 4))):
        item = {'id': f'lib{i}', 'name': name, 'behavior': R.choice(['domain', 'ipcidr', 'classical'])}
        roll = R.random()
        if roll < 0.5:
            item['url'] = R.choice([f'https://raw.githubusercontent.com/example/rules/main/{i}.yaml',
                                    f'https://github.com/example/rules/raw/main/{i}.mrs',
                                    f'https://rules.example.test/{i}.list', f'https://rules.example.test/{i}.txt?x=1'])
        elif roll < 0.7:
            item['source_type'] = 'content'
            item['content'] = R.choice(['payload:\n  - example.test\n', 'DOMAIN,a.example.test\n', ''])
        if R.random() < 0.3:
            item['format'] = R.choice(['yaml', 'text', 'mrs', 'bogus'])
        library.append(item)
    rule_configs = []
    for i in range(R.randint(0, 10)):
        if R.random() < 0.3:
            item = {'id': f'rs{i}', 'itemType': 'ruleset', 'name': R.choice(RULESET_NAMES) + ('' if R.random() < 0.7 else f' {i}'),
                    'policy': rand_policy(group_names, node_names), 'behavior': R.choice(['domain', 'ipcidr', 'classical'])}
            lib = [l for l in library if l['name'] == item['name']]
            if lib and R.random() < 0.8:
                item['library_rule_id'] = lib[0]['id']
                item['url'] = f'/api/rule-library/content/{lib[0]["id"]}'
            elif R.random() < 0.3:
                item['library_rule_id'] = 'lib-gone'
            else:
                item['url'] = R.choice(['https://raw.githubusercontent.com/example/r/main/x.yaml', '/api/rules/local/x',
                                        'https://rules.example.test/y.mrs', '', 'rules/rel.list'])
            if R.random() < 0.2:
                item['format'] = R.choice(['yaml', 'text', 'mrs'])
            if R.random() < 0.2:
                item['no_resolve'] = True
            if R.random() < 0.1:
                item['source_type'] = 'content'
                item['content'] = 'payload:\n  - x.example.test\n'
        else:
            rtype = R.choice(RULE_TYPES)
            item = {'id': f'r{i}', 'itemType': 'rule', 'rule_type': rtype, 'value': R.choice(RULE_VALUES[rtype]),
                    'policy': rand_policy(group_names, node_names)}
            if R.random() < 0.3:
                item['no_resolve'] = R.choice([True, False])
            if rtype == 'MATCH':
                if R.random() < 0.5:
                    item['udp_fallback'] = R.choice(['DIRECT', 'REJECT', 'bogus'])
                if R.random() < 0.3:
                    item['ntp_direct'] = R.choice([True, False, 'yes'])
        if R.random() < 0.9:
            item['enabled'] = R.random() > 0.1
        if R.random() < 0.05:
            item['library_enabled'] = False
        rule_configs.append(item)
    if R.random() < 0.8:
        profile['rule_configs'] = rule_configs
    elif R.random() < 0.5:
        profile['rules'] = [r for r in rule_configs if r['itemType'] == 'rule']
        profile['rule_sets'] = [r for r in rule_configs if r['itemType'] == 'ruleset']
    if library or R.random() < 0.5:
        profile['rule_library'] = library

    system = {}
    if R.random() < 0.6:
        system['server_domain'] = R.choice(['', 'https://cf.example.test', '  http://cf.example.test:5000  ', 'http://cf.example.test/sub'])
    if R.random() < 0.5:
        system['config_token'] = R.choice(['', 'tok-123', 'a b/c?d=e&f', '令牌'])
    if R.random() < 0.3:
        system['github_proxy_domain'] = R.choice(['', 'ghproxy.example.test', 'https://gh.example.test/', '  gh.example.test  '])
    if system or R.random() < 0.5:
        profile['system_config'] = system
    mihomo_settings = {}
    custom = rand_custom_config()
    if custom is not None or R.random() < 0.5:
        mihomo_settings['custom_config'] = custom
    if R.random() < 0.2:
        mihomo_settings['provider_update_interval'] = R.choice([3600, 0, 60, 86400, 604800] + ([59, '3600', True, 3600.0, 10 ** 12] if BAD[0] else []))
    if R.random() < 0.15:
        settings = deepcopy(EASYTIER_VALID)
        settings['apply_to'] = R.choice(['subscription', 'agent', 'all'])
        if R.random() < 0.3:
            settings['keepalive'] = {'enabled': True, 'url': 'http://10.144.144.2/', 'interval': 60}
        if R.random() < 0.2:
            settings['magic_dns'] = True
        if R.random() < 0.1:
            settings['options']['network-name'] = ''
        mihomo_settings['easytier'] = settings
    if R.random() < 0.05:
        mihomo_settings['linux_gateway'] = None
    if mihomo_settings or R.random() < 0.5:
        profile['mihomo'] = mihomo_settings
    if R.random() < 0.2:
        profile['mosdns'] = {'custom_hosts': R.choice(['nas.lan 192.168.1.10\n192.168.1.11 router.lan\n# c\n',
                                                       'cdn0.example.test 192.168.1.20', '', 'bad line'])}
    if R.random() < 0.15:
        profile['domain_discovery'] = R.choice([{'region_probe_port': 18000}, {'region_probe_port': None}, {},
                                                {'region_probe_port': 7890}, {'region_probe_port': '18001'}])
    if R.random() < 0.4:
        profile['profile_id'] = R.choice(['default', 'p-1', '家里'])
    return profile


def run_generate(profile, caches, downloads, kwargs):
    set_caches(caches, downloads)
    report = {}
    work = deepcopy(profile)
    entry = {'fn': 'generate', 'args': {'profile': deepcopy(profile), **kwargs}, 'caches': deepcopy(caches),
             'downloads': deepcopy(downloads)}
    try:
        out = mihomo.generate_mihomo_config(work, kwargs['base_url'], kwargs['sync_lan_hosts'], preflight_providers=False,
                                            region_probe=kwargs['region_probe'], report=report,
                                            mihomo_core_version=kwargs['core_version'], for_agent=kwargs['for_agent'])
    except Exception as exc:  # noqa: BLE001
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = out
        entry['report'] = conv(report)
    entry['mutated'] = conv(work)
    OUT.append(entry)
    return entry


def rand_kwargs():
    return {'base_url': R.choice(BASE_URLS), 'sync_lan_hosts': R.random() < 0.3, 'region_probe': R.random() < 0.15,
            'core_version': R.choice([None, None, 'v1.19.19', 'v1.19.31', '1.19.20', 'alpha-abc', '']),
            'for_agent': R.random() < 0.4}


def gen_profiles():
    seen_ok = 0
    for _ in range(COUNT):
        STATE['downloads'] = {}
        profile = rand_profile()
        caches = {}
        for sub in profile.get('subscriptions', []):
            if R.random() < 0.75:
                caches[sub['id']] = rand_cache(sub)
        downloads = dict(STATE['downloads'])
        entry = run_generate(profile, caches, downloads, rand_kwargs())
        if 'out' in entry:
            seen_ok += 1
        if R.random() < 0.15:
            base_url = R.choice(BASE_URLS)
            set_caches(caches, downloads)
            record('ruleset_downloads', [deepcopy(profile), base_url],
                   lambda: mihomo.get_mihomo_ruleset_downloads(deepcopy(profile), base_url),
                   caches=deepcopy(caches), downloads=deepcopy(downloads))
        if R.random() < 0.1:
            base_url = R.choice(BASE_URLS)
            set_caches(caches, downloads)
            record('provider_downloads', [deepcopy(profile), base_url],
                   lambda: mihomo.get_mihomo_provider_downloads(deepcopy(profile), base_url),
                   caches=deepcopy(caches), downloads=deepcopy(downloads))
    return seen_ok


def gen_fixed():
    template = json.loads(Path('backend/config_template.json').read_text())
    for pid in (None, 'default'):
        profile = deepcopy(template)
        if pid:
            profile['profile_id'] = pid
        for kwargs in ({'base_url': '', 'sync_lan_hosts': False, 'region_probe': False, 'core_version': None, 'for_agent': False},
                       {'base_url': 'http://192.168.1.2:5000', 'sync_lan_hosts': True, 'region_probe': True,
                        'core_version': 'v1.19.31', 'for_agent': True}):
            run_generate(profile, {}, {}, kwargs)
    from backend.common.config import get_default_config
    try:
        default = get_default_config()
    except Exception:  # noqa: BLE001
        default = None
    if isinstance(default, dict):
        run_generate(default, {}, {}, {'base_url': '', 'sync_lan_hosts': False, 'region_probe': False,
                                       'core_version': None, 'for_agent': False})
    run_generate({}, {}, {}, {'base_url': '', 'sync_lan_hosts': False, 'region_probe': False, 'core_version': None,
                              'for_agent': False})


# ------------------------------------------------------------------ helpers

def gen_helpers():
    for value in ['AND,((SRC-IP-CIDR,10.0.0.0/24),(IP-CIDR,10.0.0.0/24)),DIRECT', '((ip-cidr6 , 2001:db8::/32 ))',
                  '((GEOIP,CN,no-resolve))', '((IP-ASN,1,NO-RESOLVE))', '((DOMAIN,x)', '(()', '()', '((IP-SUFFIX,1.2.3.4/8),(x))',
                  '(ＩＰ-CIDR,1)', '(ip-cidr)', '((Ip-Cidr,1),(GEOIP,cn))', 'no parens', '', '(a(b)c)', '((IP-CIDR,1)))']:
        record('apply_no_resolve_to_logic_rule', [value], lambda: mihomo.apply_no_resolve_to_logic_rule(value))
    for value in [None, True, False, 'off', ' Strict ', 'ALWAYS', 'bogus', 1, 0, '', 'ｏｆｆ']:
        doc = {'find-process-mode': value} if value is not None or R.random() < 2 else {}
        record('normalize_find_process_mode', [deepcopy(doc)], lambda: (mihomo.normalize_find_process_mode(doc), doc)[1])
    for doc in [{}, {'ipv6': True}, {'ipv6': 'true'}, {'ipv6': 1}, {'dns': {'ipv6': False}}, {'dns': {'ipv6': None}},
                {'dns': 'x', 'ipv6': False}, 'x', None, {'ipv6': None}]:
        record('validate_ipv6_flags', [doc], lambda: mihomo.validate_ipv6_flags(doc))
    for source in [{}, {'health_check_url': ' http://a.example.test '}, {'health_check_url': None}, {'health_check_url': 0},
                   {'health_check_url': 5}, {'health_check_url': ['x']}, {'health_check': {'enable': False}},
                   {'health_check': {'interval': 10}}, {'health_check': None}, {'health_check': {'url': 'ftp://x'}},
                   {'health_check_url': '　'}]:
        record('build_provider_health_check', [source], lambda: mihomo.build_provider_health_check(source))
    for config in [{}, {'rule_configs': []}, {'rule_configs': [{'itemType': 'rule'}, {'itemType': 'ruleset', 'enabled': False},
                                                               {'itemType': 'ruleset'}, {'itemType': 'x'}, {'library_enabled': 0}]},
                   {'rules': [1], 'rule_sets': [2]}, {'rule_configs': [], 'rules': ['a']}]:
        record('split_rules_and_rulesets', [config], lambda: list(mihomo.split_rules_and_rulesets(config)))
    for custom in ['nas.lan 192.168.1.1', '192.168.1.2 router.lan\n\n# x\nbad', '', 'a.lan 2001:db8::1 extra']:
        for mc in [{}, {'hosts': {'nas.lan': '10.0.0.1'}}, {'hosts': 'x', 'dns': {}}, {'dns': {'use-hosts': False}}]:
            mc = deepcopy(mc)
            cfg = {'mosdns': {'custom_hosts': custom}}
            record('sync_mosdns_hosts', [deepcopy(mc), cfg], lambda: (mihomo.sync_mosdns_hosts(mc, cfg), mc)[1])
    for mc in [{}, {'proxy-groups': [{'name': 'A'}, {'name': 'ConfigFlow-Region-Probe'}, {}]},
               {'mixed-port': 17999}, {'listeners': [{'name': 'configflow-region-probe', 'port': 17999}, 'x', {'port': 1}]},
               {'listeners': 'x', 'proxy-groups': None}]:
        for port in [17999, 18000]:
            mc = deepcopy(mc)
            record('inject_region_probe', [deepcopy(mc), port], lambda: [mihomo.inject_region_probe(mc, port), mc])


# ------------------------------------------------------------ base settings

def rand_base_doc():
    doc = {}
    pick = R.random
    choices = {
        'mixed-port': [7890, 0, 65536, '7890', True, 7890.0, 9090],
        'port': [7891, 7890, 53],
        'socks-port': [7892],
        'allow-lan': [True, 'true', 1, None],
        'unified-delay': [False, 'no'],
        'log-level': ['info', 'INFO ', 'verbose', 1],
        'find-process-mode': ['off', True, 'never', ' strict'],
        'external-controller': ['0.0.0.0:9090', '127.0.0.1:9090', '[::1]:9090', '[::]:9090', 'localhost:9090', '192.168.1.2:9090',
                                ':9090', '9090', 'host:99999', '[::1:9090', '::1:9090', 5, None, '0.0.0.0:7890', '127.0.0.1:９０'],
        'secret': ['s3cret-pass', '123456', '', 'short', 123, None, 'has space in it', 'x' * 65, '密码密码密码密码'],
        'external-ui': ['ui', '', 5],
        'external-ui-url': ['https://ui.example.test/gh-pages.zip', 'ftp://x', 'http://', 'http://[::1', 5],
        'bind-address': ['*', '192.168.1.2', '[::1]', ' 10.0.0.1 ', 'bogus', 5],
        'global-client-fingerprint': ['chrome'],
        'global-ua': ['clash', ' '],
        'keep-alive-idle': [30, 0, 86401],
        'tun': [{'mtu': 1500, 'stack': 'mixed'}, {'mtu': 100}, {'stack': 'lwip'}, 'x'],
        'profile': [{'store-selected': True}, {'store-fake-ip': 'yes'}],
        'sniffer': [{'enable': True, 'sniff': {'HTTP': {'ports': [80, '8080-8880']}, 'tls': {'ports': [443], 'override-destination': True}}},
                    {'sniff': {'SSH': {}}}, {'sniff': 'x'}, {'sniff': {'QUIC': {'ports': []}}}, {'sniff': {'TLS': {'ports': ['70000']}}},
                    {'sniff': {'HTTP': {'ports': [80], 'override-destination': 'true'}}}, {'sniff': None},
                    {'sniff': {'HTTP': {'ports': ['10-5']}}}, {'sniff': {'HTTP': {'ports': [' 1 - 2 ']}}}, {'sniff': {'HTTP': 'x'}},
                    {'enable': 'yes', 'skip-domain': ['a.example.test', ''], 'force-domain': 'x'}],
        'dns': [{'enable': True, 'listen': '0.0.0.0:53', 'nameserver': ['223.5.5.5']},
                {'enable': True, 'nameserver': []}, {'enable': False, 'listen': '0.0.0.0:7890'},
                {'nameserver': ['127.0.0.1:loop'], 'default-nameserver': ['x']}, {'respect-rules': True},
                {'respect-rules': True, 'proxy-server-nameserver': ['https://1.1.1.1/dns-query']},
                {'fake-ip-range': '198.18.0.1/16'}, {'fake-ip-range': 'bad/16'}, {'fake-ip-range': 5},
                {'nameserver-policy': {'a': 'loop-a'}}, {'fallback': ['ok', ''], 'cache-algorithm': 'lfu'},
                {'listen': '127.0.0.1:9090', 'enable': True}, 'x', {'prefer-h3': 'true'}],
        'listeners': [[{'name': 'in', 'port': 7890}], [{'port': 7893}], [{'name': 'configflow-region-probe', 'port': 7890}],
                      [{'name': 'a', 'port': 8000}, {'name': 'b', 'port': 8000}], 'x', [{'port': '8000'}]],
    }
    for k, values in choices.items():
        if pick() < 0.22:
            doc[k] = deepcopy(R.choice(values))
    return doc


def mutate(doc):
    prev = deepcopy(doc)
    if not prev or R.random() < 0.3:
        return rand_base_doc()
    for k in R.sample(list(prev), R.randint(0, len(prev))):
        if R.random() < 0.5:
            del prev[k]
        else:
            prev[k] = deepcopy(rand_base_doc().get(k, 'changed'))
    return prev


def gen_base_settings(count):
    for _ in range(count):
        doc = rand_base_doc()
        roll = R.random()
        if roll < 0.3:
            previous = None
        elif roll < 0.6:
            previous = deepcopy(doc)
            for k in R.sample(list(doc), min(len(doc), R.randint(0, 2))):
                previous.pop(k, None)
        else:
            previous = mutate(doc)
        reserved = R.choice([None, {}, {9090: 'Mihomo Agent「a」的端口'}, {7890: 'OxiDNS Agent「d」的端口', 53: 'x'}])
        local = R.choice([(), ['192.168.1.2'], ['192.168.1.2', 'bogus', '2001:db8::2', None, 3232235778], ['fe80::1%eth0']])
        record('validate_base_settings', [doc, previous, reserved and {str(k): v for k, v in reserved.items()}, list(local)],
               lambda: mbs.validate_base_settings(doc, previous, reserved_ports=reserved, local_addresses=local))
    for value in [80, 0, 65535, 65536, '80', ' 8080-8880 ', '10-5', '1-65535', '1-65536', '٣', '²', 'a-b', '', '-', '80-', True, 1.5, None]:
        record('sniff_port_error', [value], lambda: mbs.sniff_port_error(value))
    for value in ['0.0.0.0:9090', ' [::1]:80 ', '::1:80', 'h:0', 'h:65535', 'h:٣', 'h:²', ':1', '[x:1', 'a]:1', 5, None, 'h:+1', 'h: 1']:
        record('split_host_port', [value], lambda: mbs.split_host_port(value))
    for host in ['', '0.0.0.0', '::', 'LOCALHOST', '127.1.2.3', '::1', '::1%eth0', '::ffff:127.0.0.1', '192.168.1.1', 'bogus', '0:0::0']:
        record('controller_scope', [host], lambda: mbs.controller_scope(host))


def main():
    gen_fixed()
    ok = gen_profiles()
    gen_helpers()
    gen_base_settings(COUNT)
    path = Path(__file__).with_name('mihomo_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(entry, ensure_ascii=False, sort_keys=False) for entry in OUT) + '\n'
    with open(path, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    print(f'{len(OUT)} rows, {ok} generated configurations -> {path}')


if __name__ == '__main__':
    main()
