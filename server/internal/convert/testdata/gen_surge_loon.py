#!/usr/bin/env python3
"""Generate surge_loon_corpus.jsonl.gz: profiles, nodes and groups and what
Python's Surge (C19) and Loon (C20) converters make of them.

Run from the repository root on the test server (CPython 3.11, the backend venv):

    PYTHONPATH=.:server/internal/convert/testdata ~/cf-test/.venv/bin/python \
        server/internal/convert/testdata/gen_surge_loon.py [count]

Profiles come from gen_mihomo.rand_profile (same fakes: Sub-Store is
fake_convert, mirrored in the Go test) and are extended with what only the
Surge/Loon converters read: every node type and params shape they handle,
manual node strings of every scheme (including the ones parse_uri_list drops),
WireGuard sections, smart groups, custom configs with sections, Loon rule
types, blackmatrix7 rule sets. A share of profiles gets malformed values at
random places (crash classes are compared). Every value is synthetic.

One JSON object per line:
    {"fn": "surge" | "loon", "args": {"profile": ..., "base_url": ...}, "out" | "err", "mutated": ...}
    {"fn": "<helper>", "args": [...], "out" | "err"}
"""

import base64
import gzip
import json
import random
import sys
from copy import deepcopy
from pathlib import Path
from urllib.parse import quote

import yaml

import gen_mihomo as gm
from backend.converters import surge, loon
from backend.utils.sub_store_client import proxies_to_nodes

COUNT = int(sys.argv[1]) if len(sys.argv) > 1 else 3000
R = random.Random(20261117)
gm.R.seed(20261118)
OUT = []


def record(fn, args, call, **extra):
    entry = {'fn': fn, 'args': deepcopy(args), **extra}
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001 - the corpus records every exception
        entry['err'] = [type(exc).__name__, str(exc)]
    else:
        entry['out'] = gm.conv(result)
    OUT.append(entry)
    return entry


# ------------------------------------------------------------------ values

WEIRD = [None, 5, 0, 1.5, True, False, '', 'x', [], [1], ['a', 'b'], {}, {'a': 1}, ' ', 'a,b']
NODE_NAMES = ['HK 01', '🇭🇰 香港', 'US=02', 'a,b', ' spaced ', 'JP_Tokyo 3', 'it\'s', 'a"b', '', 'node: x', 'WG Home']
SERVERS = ['s.example.test', 'relay.invalid', '203.0.113.1', '2001:db8::1', '[2001:db8::2]', '']
PORTS = [443, 8388, 0, 65535, '443', None, 1.5]
TYPES = ['ss', 'SS', 'ssr', 'vmess', 'vless', 'trojan', 'hysteria2', 'hysteria', 'anytls', 'http', 'https', 'socks5',
         'snell', 'tuic', 'tuic-v5', 'wireguard', 'wg', 'WireGuard', 'mieru', '']

PARAMS = {
    'cipher': ['aes-128-gcm', 'chacha20-ietf-poly1305', 'auto', 'none', 'aes-128-cfb', 'rc4-md5', '', None],
    'password': ['pw', 'p,w', 'p"w', '', '密码', 123],
    'udp': [True, False, None, 1, 0],
    'udp-relay': [True, False],
    'test-url': ['http://t.example.test/204', ''],
    'uuid': ['00000000-0000-4000-8000-000000000001', ''],
    'tls': [True, False, None, 'true', 0],
    'network': ['tcp', 'ws', 'http', 'grpc', 'h2', '', None, 'WS'],
    'ws-opts': [{'path': '/ws', 'headers': {'Host': 'cdn.example.test'}}, {'path': '/a,b'}, {}, None, {'headers': {}},
                {'path': '', 'headers': {'Host': ''}}, {'headers': {'Host': 'h,1.example.test'}}],
    'ws-config': [{'path': '/cfg', 'headers': {'Host': 'cfg.example.test'}}, {}],
    'path': ['/p', '', None],
    'host': ['host.example.test', ''],
    'http-opts': [{'path': ['/p1', '/p2'], 'headers': {'Host': ['h.example.test']}}, {'path': '/x', 'headers': {'Host': 'h2.example.test'}},
                  {'path': []}, None, {}, {'headers': None}],
    'sni': ['sni.example.test', '', 'a,b.example.test'],
    'servername': ['sn.example.test', ''],
    'skip-cert-verify': [True, False, None],
    'alpn': ['h3', ['h2', 'http/1.1'], [], None, '', 'h2,h3'],
    'reality-opts': [{'public-key': 'pk', 'short-id': 'ab'}, {'public-key': 'p"k'}, {}, None, {'short-id': 'x'}],
    'flow': ['', 'xtls-rprx-vision', 'xtls-rprx-direct', None],
    'security': ['tls', 'reality', 'none', None],
    'grpc-opts': [{'grpc-service-name': 'svc'}, {}],
    'serviceName': ['svc2', ''],
    'psk': ['psk-1', ''],
    'version': [4, 5, 0, None, '3', 3.0],
    'reuse': [True, False],
    'obfs-opts': [{'mode': 'shadow-tls', 'host': 'www.example.test', 'password': 'stpw', 'version': 3},
                  {'mode': 'tls', 'host': 'bing.example.test'}, {'mode': 'http'}, {}, 'x', None, {'mode': 'shadow-tls'}],
    'obfs': ['http', 'tls', 'shadow-tls', 'salamander', 'plain', '', None],
    'obfs-host': ['oh.example.test', ''],
    'obfs-password': ['opw', '', 'o,pw'],
    'token': ['tok', ''],
    'section-name': ['wg-sec', ''],
    'mtu': [1280, 0, None, '1400'],
    'private-key': ['cHJpdmF0ZQ==', ''],
    'privateKey': ['cHJpdjI=', ''],
    'self-ip': ['10.0.0.2', ''],
    'ip': ['10.0.0.3/32', '10.0.0.3', None],
    'address': ['10.0.0.4/24', ''],
    'ipv6': ['fd00::2/128', None, ''],
    'dns': ['1.1.1.1', '1.1.1.1, 2606:4700::1111', ['1.1.1.1', '2606:4700::1111'], [], None, ['', '9.9.9.9'], '::1'],
    'dns-server': ['8.8.8.8', ''],
    'public-key': ['cHVibGlj', ''],
    'publicKey': ['cHViMg==', ''],
    'peer-public-key': ['cGVlcg==', ''],
    'endpoint': ['e.example.test:51820', ''],
    'allowed-ips': ['0.0.0.0/0', ['0.0.0.0/0', '::/0'], None, ''],
    'allowed_ips': ['10.0.0.0/8', ['10.0.0.0/8']],
    'keepalive': [25, 0, ''],
    'persistent-keepalive': [15, None],
    'preshared-key': ['cHNr', ''],
    'presharedKey': ['cHNrMg==', ''],
    'pre-shared-key': ['cHNrMw==', ''],
    'client-id': ['1/2/3', ''],
    'reserved': [[1, 2, 3], [], 'AQID', None],
    'plugin': ['obfs', 'simple-obfs', 'shadow-tls', 'v2ray-plugin', '', None],
    'plugin-opts': [{'mode': 'tls', 'host': 'h.example.test'}, {'password': 'pw', 'host': 's.example.test', 'version': 2}, {},
                    None, {'host': 'a,b'}, {'mode': 'http'}],
    'udp-over-tcp': [True, False],
    'tfo': [True, False, None],
    'protocol': ['auth_aes128_md5', 'origin', ''],
    'protocol-param': ['pp', 'p,p', ''],
    'obfs-param': ['op.example.test', ''],
    'alterId': [0, 1, None, '2'],
    'ports': ['20000-30000', None, '443,8443'],
    'hop-interval': [30, None],
    'down': ['100 Mbps', '１００ Mbps', 'fast', 50, None, ' 20', '٣٠ mbps'],
    'up': ['20 Mbps'],
    'idle-session-timeout': [30, None],
    'username': ['user', '', 'u,ser'],
    'dialer-proxy': ['Relay'],
}
TYPE_KEYS = {
    'ss': ['cipher', 'password', 'udp', 'udp-relay', 'test-url', 'plugin', 'plugin-opts', 'udp-over-tcp', 'tfo'],
    'ssr': ['cipher', 'password', 'protocol', 'protocol-param', 'obfs', 'obfs-param', 'tfo', 'udp'],
    'vmess': ['uuid', 'tls', 'network', 'ws-opts', 'http-opts', 'cipher', 'alterId', 'sni', 'servername', 'skip-cert-verify',
              'alpn', 'udp', 'path', 'host'],
    'vless': ['uuid', 'flow', 'tls', 'security', 'network', 'ws-opts', 'ws-config', 'path', 'host', 'grpc-opts', 'serviceName',
              'sni', 'servername', 'skip-cert-verify', 'reality-opts', 'udp', 'alpn', 'http-opts'],
    'trojan': ['password', 'sni', 'skip-cert-verify', 'udp', 'network', 'ws-opts', 'alpn', 'tfo', 'reality-opts'],
    'hysteria2': ['password', 'sni', 'skip-cert-verify', 'udp', 'obfs', 'obfs-password', 'ports', 'hop-interval', 'down', 'up',
                  'tfo', 'alpn'],
    'anytls': ['password', 'sni', 'skip-cert-verify', 'udp', 'idle-session-timeout', 'alpn'],
    'http': ['username', 'password', 'tls', 'sni', 'skip-cert-verify'],
    'snell': ['psk', 'password', 'version', 'reuse', 'udp', 'udp-relay', 'obfs-opts', 'obfs', 'obfs-host'],
    'tuic': ['uuid', 'password', 'token', 'alpn', 'sni', 'skip-cert-verify', 'udp', 'udp-relay'],
    'wireguard': ['section-name', 'mtu', 'private-key', 'privateKey', 'self-ip', 'ip', 'address', 'ipv6', 'dns', 'dns-server',
                  'public-key', 'publicKey', 'peer-public-key', 'endpoint', 'allowed-ips', 'allowed_ips', 'keepalive',
                  'persistent-keepalive', 'preshared-key', 'presharedKey', 'pre-shared-key', 'client-id', 'reserved', 'udp'],
}
TYPE_KEYS['https'] = TYPE_KEYS['socks5'] = TYPE_KEYS['http']
TYPE_KEYS['tuic-v5'] = TYPE_KEYS['tuic']
TYPE_KEYS['wg'] = TYPE_KEYS['wireguard']


def rand_params(node_type, bad):
    keys = TYPE_KEYS.get(node_type.lower(), list(PARAMS)[:8]) if isinstance(node_type, str) else list(PARAMS)[:6]
    params = {}
    for key in keys:
        if R.random() < 0.55:
            params[key] = deepcopy(R.choice(PARAMS[key]))
    if R.random() < 0.15:
        key = R.choice(list(PARAMS))
        params[key] = deepcopy(R.choice(PARAMS[key]))
    if bad and params and R.random() < 0.5:
        params[R.choice(list(params))] = deepcopy(R.choice(WEIRD))
    if node_type in ('wireguard', 'wg') and R.random() < 0.6:
        params.setdefault('private-key', 'cHJpdmF0ZQ==')
        params.setdefault('public-key', 'cHVibGlj')
    return params


def b64(text, urlsafe=False, pad=True):
    raw = (base64.urlsafe_b64encode if urlsafe else base64.b64encode)(text.encode()).decode()
    return raw if pad else raw.rstrip('=')


def rand_uri(i):
    host = R.choice(['u.example.test', '203.0.113.5', '[2001:db8::5]'])
    name = quote(R.choice(NODE_NAMES))
    roll = R.randint(0, 17)
    if roll == 0:
        return f'ss://{b64("aes-128-gcm:pw-" + str(i), urlsafe=True, pad=False)}@{host}:8388#{name}'
    if roll == 1:
        return f'ss://{b64("chacha20-ietf-poly1305:p:w@" + host + ":443")}#{name}'
    if roll == 2:
        return (f'ss://{b64("aes-256-gcm:pw", urlsafe=True)}@{host}:8388/?plugin='
                + quote(R.choice(['obfs-local;obfs=http;obfs-host=o.example.test', 'v2ray-plugin;mode=websocket', 'shadow-tls;host=s.example.test;password=x']))
                + f'#{name}')
    if roll == 3:
        inner = f'{host.strip("[]")}:443:auth_aes128_md5:aes-128-cfb:tls1.2_ticket_auth:{b64("pw", urlsafe=True, pad=False)}'
        inner += f'/?obfsparam={b64("o.example.test", urlsafe=True, pad=False)}&remarks={b64(R.choice(NODE_NAMES), urlsafe=True, pad=False)}'
        return 'ssr://' + b64(inner, urlsafe=True, pad=False)
    if roll in (4, 5):
        data = {'v': '2', 'ps': R.choice(NODE_NAMES), 'add': host.strip('[]'), 'port': R.choice([443, '443', '']),
                'id': '00000000-0000-4000-8000-00000000000a', 'aid': R.choice([0, '0', 2]),
                'net': R.choice(['tcp', 'ws', 'grpc', 'h2', 'http', '']), 'type': 'none',
                'host': R.choice(['h.example.test', '']), 'path': R.choice(['/v', '', '/a,b']),
                'tls': R.choice(['tls', '', 'none']), 'sni': R.choice(['s.example.test', ''])}
        if R.random() < 0.3:
            data['scy'] = R.choice(['auto', 'aes-128-gcm', 'zero'])
        return 'vmess://' + b64(json.dumps(data, ensure_ascii=R.random() < 0.5))
    if roll in (6, 7):
        query = R.choice(['type=ws&security=tls&sni=s.example.test&path=%2Fws&host=h.example.test',
                          'type=tcp&security=reality&pbk=pk&sid=ab&flow=xtls-rprx-vision&sni=r.example.test&fp=chrome',
                          'type=grpc&serviceName=svc&security=tls', 'security=none', 'type=http&path=%2Fh',
                          'encryption=none&type=ws&path=%2Fa%2Cb', ''])
        return f'vless://00000000-0000-4000-8000-00000000000b@{host}:443?{query}#{name}'
    if roll in (8, 9):
        query = R.choice(['sni=t.example.test&allowInsecure=1', 'type=ws&path=%2Ft&host=th.example.test', 'type=grpc&serviceName=g',
                          'peer=p.example.test', ''])
        return f'trojan://tr-pw@{host}:443?{query}#{name}'
    if roll == 10:
        query = R.choice(['sni=h.example.test&obfs=salamander&obfs-password=op&insecure=1', 'mport=20000-30000', 'alpn=h3', ''])
        return f'hysteria2://h2-pw@{host}:443?{query}#{name}'
    if roll == 11:
        return f'hysteria://{host}:443?auth=a&peer=p.example.test&upmbps=10&downmbps=50&alpn=h3#{name}'
    if roll == 12:
        return R.choice([f'tuic://00000000-0000-4000-8000-00000000000c:pw@{host}:443?alpn=h3#{name}',
                         f'anytls://pw@{host}:443?sni=a.example.test#{name}',
                         f'wireguard://cHJpdg==@{host}:51820?publickey=cHVi&address=10.0.0.2#{name}',
                         f'socks5://u:p@{host}:1080#{name}', f'http://u:p@{host}:8080#{name}', f'snell://x@{host}:1#{name}'])
    if roll == 13:
        return R.choice(['fail://x', 'empty://y', 'not a uri', '', '   ', 'ss://%%%', 'vmess://!!!', 'vless://', 'trojan://@:'])
    if roll == 14:
        return f'- {rand_uri(i)}\n{rand_uri(i + 1)}'
    if roll == 15:
        return 'ss://' + b64('aes-128-gcm:pw@' + host + ':bad')
    if roll == 16:
        return f'vmess://dial-{R.choice(gm.NODE_IDS)}' if R.random() < 0.3 else f'trojan://pw@t{i}.example.test:443?dial-g0&x'
    return 'ss://' + b64('aes-128-gcm:pw') + f'@{host}:8388?plugin=obfs-local%3Bobfs%3Dtls#{name}'


def rand_structured(i, bad):
    node_type = R.choice(TYPES)
    proxy = {'name': R.choice(NODE_NAMES), 'type': node_type, 'server': R.choice(SERVERS[:4]), 'port': R.choice([443, 8388])}
    proxy.update(rand_params(node_type, bad))
    for key in list(proxy):
        if proxy[key] is None and R.random() < 0.7:
            del proxy[key]
    if R.random() < 0.05:
        del proxy['type']
    roll = R.random()
    if roll < 0.35:
        return json.dumps(proxy, ensure_ascii=R.random() < 0.5)
    if roll < 0.7:
        return yaml.safe_dump(proxy, allow_unicode=True, sort_keys=False)
    if roll < 0.85:
        return yaml.safe_dump([proxy, {'name': 'second', 'type': 'ss', 'server': 'x.example.test', 'port': 1}], allow_unicode=True,
                              sort_keys=False)
    return '{' + ', '.join(f'{k}: {json.dumps(v)}' for k, v in proxy.items() if not isinstance(v, (dict, list))) + '}'


def rand_node(i, nid, bad):
    node = {'id': nid, 'name': R.choice(NODE_NAMES[:8]) + ('' if R.random() < 0.3 else f' {i}')}
    if R.random() < 0.85:
        node['enabled'] = R.random() > 0.1
    roll = R.random()
    if roll < 0.55:
        node_type = R.choice(TYPES)
        node['type'] = node_type
        node['server'] = R.choice(SERVERS)
        node['port'] = R.choice(PORTS)
        node['params'] = rand_params(node_type, bad)
        if R.random() < 0.03:
            del node['type']
        if bad and R.random() < 0.1:
            node['params'] = R.choice([None, [], 'x', {}])
    elif roll < 0.8:
        node['proxy_string'] = rand_structured(i, bad)
    else:
        node['proxy_string'] = rand_uri(i)
    if bad and R.random() < 0.3:
        node[R.choice(['name', 'server', 'port', 'type', 'proxy_string', 'id', 'enabled'])] = deepcopy(R.choice(WEIRD))
    return node


SURGE_SECTIONS = [
    '[General]\nloglevel = verbose\nskip-proxy = 127.0.0.1',
    '[General]\nudp-policy-not-supported-behaviour = REJECT\nipv6 = false',
    '[MITM]\nhostname = a.example.test',
    '[Script]\nx = type=http-response,pattern=^https://a.example.test',
    '[Rule]\nDOMAIN,dropped.example.test,DIRECT',
    '[Proxy]\nDropped = direct',
    '[Proxy Group]\nDropped = select, DIRECT',
    '[Host]\nnas.lan = 192.168.1.10',
    '[URL Rewrite]\n^http://a.example.test http://b.example.test 302',
    '  [Header Rewrite]  \n  ^http://h.example.test header-add X 1  ',
    '[[weird]\nline',
    '[a[b]c]\nvalue',
    '[]\nempty name',
    'loose line before sections',
]
LOON_SECTIONS = [
    '[General]\nip-mode = ipv4-only\nskip-proxy = 127.0.0.1',
    '[General]\nudp-fallback-mode = REJECT',
    '[MITM]\nhostname = a.example.test',
    '[Script]\nhttp-response ^https://a.example.test script-path=x.js',
    '[Rule]\nDOMAIN,dropped.example.test,DIRECT',
    '[Remote Proxy]\nDropped = https://x.example.test',
    '[Remote Filter]\nF = NameRegex,A,FilterKey="x"',
    '[Remote Rule]\nhttps://r.example.test/x.list,policy=DIRECT,tag=x,enabled=true',
    '[Proxy]\nDropped = direct',
    '[Plugin]\nhttps://p.example.test/x.plugin, enabled=true',
    '[Host]\nnas.lan = 192.168.1.10',
    '[[weird]\nline',
    'loose line before sections',
]


def rand_custom(pool):
    roll = R.random()
    if roll < 0.25:
        return R.choice(['', None, '   \n  '])
    parts = R.sample(pool, R.randint(1, 4))
    if R.random() < 0.15:
        parts.append(R.choice(pool))  # repeated section names
    text = '\n\n'.join(parts)
    if R.random() < 0.1:
        text = text.replace('\n', '\r\n')
    if R.random() < 0.1:
        text = '\n  ' + text + '\n\n'
    return text


EXTRA_RULES = [
    ('URL-REGEX', '^https?://ads\\.example\\.test'), ('USER-AGENT', 'Instagram*'), ('PROTOCOL', 'QUIC'), ('DEST-PORT', '8443'),
    ('SRC-IP', '192.168.1.5'), ('SRC-PORT', '1000,2000'), ('NETWORK', ' UDP '), ('NETWORK', 'tcp'), ('DST-PORT', '80,443'),
    ('AND', '((PROCESS-NAME,curl),(DOMAIN,a.example.test))'), ('OR', '((DST-PORT,80/443),(DOMAIN,b.example.test))'),
    ('AND', '((NETWORK,UDP),(DST-PORT,443))'), ('NOT', '((dest-port,22))'), ('AND', '((SRC-IP-CIDR,10.0.0.0/8),(DST-PORT,53))'),
    ('OR', '(( GEOIP , CN ),(IP-ASN,13335))'), ('IP-ASN', '13335'), ('DOMAIN-SET', 'x'), ('RULE-SET', 'Ads'),
]


def corrupt(value, depth=0):
    """Replace one random value somewhere in a profile with a malformed one."""
    containers = []

    def walk(v, d):
        if isinstance(v, dict):
            for k in v:
                containers.append((v, k))
                walk(v[k], d + 1)
        elif isinstance(v, list):
            for i in range(len(v)):
                containers.append((v, i))
                walk(v[i], d + 1)
    walk(value, 0)
    if containers:
        container, key = R.choice(containers)
        container[key] = deepcopy(R.choice(WEIRD))


def rand_profile():
    profile = gm.rand_profile()
    bad = R.random() < 0.12
    nodes = profile.get('nodes', [])
    for i, node in enumerate(nodes):
        if R.random() < 0.6:
            replaced = rand_node(i, node['id'], bad)
            for key in ('dialer_ref', 'subscription_id'):
                if key in node:
                    replaced[key] = node[key]
            nodes[i] = replaced
    groups = profile.get('proxy_groups', [])
    surge_settings = {}
    if R.random() < 0.7:
        surge_settings['custom_config'] = rand_custom(SURGE_SECTIONS)
    if groups and R.random() < 0.35:
        surge_settings['smart_groups'] = [
            {'group_id': R.choice([g['id'] for g in groups] + ['gone']), **({'policy_priority': R.choice(['HK:1.5;US:0.8', '', None])}
                                                                            if R.random() < 0.8 else {})}
            for _ in range(R.randint(1, 3))]
    if surge_settings or R.random() < 0.3:
        profile['surge'] = surge_settings
    if R.random() < 0.7:
        profile['loon'] = {'custom_config': rand_custom(LOON_SECTIONS)}
    for group in groups:
        if R.random() < 0.1:
            group['type'] = R.choice(['smart', 'ssid', 'subnet', 'select', 'load-balance'])
        if R.random() < 0.1:
            group['strategy'] = R.choice(['consistent-hashing', 'sticky-sessions', 'round-robin', '', None, 'other'])
        if R.random() < 0.08:
            group['url'] = None
    rules = profile.get('rule_configs')
    if isinstance(rules, list):
        for i in range(R.randint(0, 3)):
            rtype, value = R.choice(EXTRA_RULES)
            item = {'id': f'x{i}', 'itemType': 'rule', 'rule_type': rtype, 'value': value,
                    'policy': R.choice(['DIRECT', 'REJECT', 'REJECT-TINYGIF', 'REJECT-IMG'] +
                                       [g['name'] for g in groups if g.get('enabled', True)])}
            if R.random() < 0.3:
                item['no_resolve'] = True
            rules.insert(R.randint(0, len(rules)), item)
        if R.random() < 0.2:
            rules.insert(R.randint(0, len(rules)), {
                'id': 'bm', 'itemType': 'ruleset', 'name': 'Apple', 'policy': 'DIRECT',
                'url': R.choice(['https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/Apple/Apple.list',
                                 'https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/Apple/Apple.yaml',
                                 '/api/rules/local/apple'])})
    library = profile.get('rule_library')
    if isinstance(library, list) and R.random() < 0.3:
        library.append({'id': 'libbm', 'name': 'Apple', 'enabled': R.random() < 0.9,
                        'url': 'https://raw.githubusercontent.com/blackmatrix7/ios_rule_script/master/rule/Clash/Apple/Apple.list'})
    if isinstance(rules, list) and not bad:
        valid = ['DIRECT', 'REJECT'] + [g['name'] for g in groups if g.get('enabled', True)]
        for item in rules:
            if item.get('policy') not in valid and R.random() < 0.85:
                item['policy'] = R.choice(valid)
    if bad:
        for _ in range(R.randint(1, 2)):
            corrupt(profile)
    return profile


# ------------------------------------------------------------------ runs

def run(fn, generate, profile, base_url):
    work = deepcopy(profile)
    gm.set_caches({}, {})
    entry = {'fn': fn, 'args': {'profile': deepcopy(profile), 'base_url': base_url}}
    try:
        entry['out'] = generate(work, base_url=base_url)
    except Exception as exc:  # noqa: BLE001
        entry['err'] = [type(exc).__name__, str(exc)]
    entry['mutated'] = gm.conv(work)
    OUT.append(entry)
    return entry


def gen_profiles():
    ok = {'surge': 0, 'loon': 0}
    for _ in range(COUNT):
        profile = rand_profile()
        base_url = R.choice(gm.BASE_URLS)
        for fn, generate in (('surge', surge.generate_surge_config), ('loon', loon.generate_loon_config)):
            if 'out' in run(fn, generate, profile, base_url):
                ok[fn] += 1
    return ok


def gen_fixed():
    template = json.loads(Path('backend/config_template.json').read_text())
    from backend.common.config import get_default_config
    profiles = [template, dict(template, profile_id='default'), {}]
    try:
        default = get_default_config()
        if isinstance(default, dict):
            profiles.append(default)
    except Exception:  # noqa: BLE001
        pass
    for profile in profiles:
        for base_url in ('', 'http://192.168.1.2:5000'):
            run('surge', surge.generate_surge_config, profile, base_url)
            run('loon', loon.generate_loon_config, profile, base_url)


def gen_nodes(count):
    for i in range(count):
        bad = R.random() < 0.15
        node = rand_node(i, f'n{i}', bad)
        if R.random() < 0.05:
            node = R.choice([{}, {'proxy_string': None}, {'name': 'x', 'proxy_string': 5}, {'type': 'ss'}, {'name': 'n', 'type': None}])
        record('node_surge', [node], lambda: list(surge.convert_node_to_surge(deepcopy(node))))
        record('node_loon', [node], lambda: loon.convert_node_to_loon(deepcopy(node)))


def gen_texts(count):
    for i in range(count):
        proxies = []
        for j in range(R.randint(0, 5)):
            node_type = R.choice(TYPES)
            proxy = {'name': R.choice(NODE_NAMES), 'type': node_type, 'server': R.choice(SERVERS), 'port': R.choice(PORTS)}
            proxy.update(rand_params(node_type, R.random() < 0.15))
            if R.random() < 0.05:
                del proxy['name']
            if R.random() < 0.03:
                proxy['dialer-proxy'] = R.choice(['Relay', None])
            proxies.append(proxy)
        if R.random() < 0.03:
            proxies.append(R.choice(['x', 5, {'type': 5}]))
        record('proxies_surge', [proxies], lambda: surge.convert_proxies_to_surge_text(deepcopy(proxies)))
        record('proxies_loon', [proxies], lambda: loon.convert_proxies_to_loon_text(deepcopy(proxies)))


def gen_groups(count):
    for _ in range(count):
        profile = rand_profile()
        groups = profile.get('proxy_groups')
        if not isinstance(groups, list) or not groups:
            continue
        group = R.choice(groups)
        smart = None
        if R.random() < 0.4 and isinstance(group, dict):
            smart = {str(group.get('id')): R.choice(['HK:1', '', 'x'])} if R.random() < 0.7 else {'other': 'y'}
        base_url = R.choice(gm.BASE_URLS)
        record('group_surge', [group, profile, base_url, smart],
               lambda: surge.convert_proxy_group_to_surge(deepcopy(group), deepcopy(profile), base_url, smart))
        members = R.sample(['A', 'B 1', 'DIRECT', 'Feed', '香港'], R.randint(0, 3))
        record('group_loon', [group, members], lambda: loon.convert_proxy_group_to_loon(deepcopy(group), list(members)))
        record('group_nodes', [group, profile], lambda: surge.get_proxy_group_nodes(deepcopy(group), deepcopy(profile)))
        record('build_rules', [profile, base_url], lambda: [list(x) for x in loon._build_rules(deepcopy(profile), base_url)])
    for name in WEIRD + NODE_NAMES + [' a = b , c ', '　全角　']:
        record('sanitize', [name], lambda: loon._sanitize_name(name))
    for nodes, group in [
        ([{'name': n, 'subscription_id': 'sub'} for n in ('HK 01', 'HK 到期', 'JP 01')],
         {'subscriptions': ['sub'], 'regex': 'HK', 'exclude_regex': '到期', 'proxies': ['HK 到期']}),
        ([{'name': 'A', 'subscription_id': 's', 'enabled': False}, {'name': 'B', 'subscription_id': 's'}, {'name': 'B'}],
         {'subscriptions': ['s'], 'proxies': ['B', 'C', 'C']}),
        ([{'name': 'A', 'subscription_id': 's'}], {'subscriptions': 's', 'regex': '('}),
        ([{'name': 'A', 'subscription_id': ['s']}], {'subscriptions': ['s']}),
        ([], {'proxies': [1, 1.0, True, 'x']}),
    ]:
        record('group_nodes', [group, {'nodes': nodes}], lambda: surge.get_proxy_group_nodes(deepcopy(group), {'nodes': deepcopy(nodes)}))


def main():
    gen_fixed()
    ok = gen_profiles()
    gen_nodes(COUNT)
    gen_texts(COUNT // 3)
    gen_groups(COUNT // 3)
    path = Path(__file__).with_name('surge_loon_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(entry, ensure_ascii=False, sort_keys=False) for entry in OUT) + '\n'
    with open(path, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    print(f'{len(OUT)} rows, generated configurations {ok} -> {path}')


if __name__ == '__main__':
    main()
