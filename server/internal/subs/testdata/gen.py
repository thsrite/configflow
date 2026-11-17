#!/usr/bin/env python3
"""Generate the differential corpora of package subs from the real Python code.

Run from the repository root on the test server (CPython 3.11, the production
dependencies; nothing here touches the network):

    rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/subs/testdata/gen.py'
    rt --back server/internal/subs/testdata

Every value is synthetic (example.com / RFC 5737 / RFC 3849 addresses, random
UUIDs from a fixed seed). Output: one JSON object per line,
{"fn": name, "in": input, "out": output}; an exception is
{"error": str(exc), "type": ClassName} in "out". Files are written with
ensure_ascii=False; key order is the Python dict order.
"""
import base64
import gzip
import io
import copy
import json
import os
import random
import re
import sys
import tempfile
import uuid
from pathlib import Path

assert sys.version_info[:2] == (3, 11), sys.version
HERE = Path(__file__).parent
R = random.Random(20261115)


def uid():
    return str(uuid.UUID(int=R.getrandbits(128), version=4))


def dump(path, rows):
    if os.environ.get('GEN_ONLY') and not path.startswith(os.environ['GEN_ONLY']):
        return
    path += '.gz'
    with gzip.GzipFile(HERE / path, 'wb', mtime=0) as raw, io.TextIOWrapper(raw, encoding='utf-8') as fh:
        for row in rows:
            fh.write(json.dumps(row, ensure_ascii=False, separators=(',', ':')) + '\n')
    print(path, len(rows))


def call(fn, *args, **kw):
    try:
        return fn(*args, **kw)
    except BaseException as exc:  # noqa: BLE001
        return {'error': str(exc), 'type': type(exc).__name__}


# --------------------------------------------------------------------- parser
from backend.utils import subscription_parser as sp  # noqa: E402

HOSTS = ['example.com', 'node1.example.com', '192.0.2.10', '198.51.100.7', '203.0.113.5', '[2001:db8::1]',
         '2001:db8::2', '例子.test', 'a-b.example.org', 'EXAMPLE.COM', '', 'host with space.example']
PORTS = ['443', '8388', '1', '65535', '0', '70000', '-1', ' 443 ', '4_43', '٤٤٣', '443/', '443?x=1', 'abc', '',
         '+80', '00443', '443.0', '0x1bb', '1' * 30]
NAMES = ['Node 1', 'HK-01', '%E9%A6%99%E6%B8%AF%2001', '香港 01', 'a#b', '', '🚀 fast', 'name%20with%20space',
         'x' * 50, 'tab\there']
METHODS = ['aes-128-gcm', 'chacha20-ietf-poly1305', '2022-blake3-aes-256-gcm', 'none']
PASSWORDS = ['pw', 'p@ss:word', 'pass%40word', '密码', 'a:b:c', '', 'x' * 40, 'with/slash+plus=eq']


def b64(text, mode=None):
    raw = text.encode('utf-8') if isinstance(text, str) else text
    mode = mode or R.choice(['std', 'nopad', 'url', 'urlnopad', 'std', 'nl'])
    if mode == 'std':
        return base64.b64encode(raw).decode()
    if mode == 'nopad':
        return base64.b64encode(raw).decode().rstrip('=')
    if mode == 'url':
        return base64.urlsafe_b64encode(raw).decode()
    if mode == 'urlnopad':
        return base64.urlsafe_b64encode(raw).decode().rstrip('=')
    s = base64.b64encode(raw).decode()
    return s[:10] + '\n' + s[10:] if len(s) > 10 else s


def pick(seq):
    return R.choice(seq)


def qs(items):
    parts = []
    for k, v in items:
        parts.append(f'{k}={v}')
    return '&'.join(parts)


def gen_ss():
    m, p, h, port, n = pick(METHODS), pick(PASSWORDS), pick(HOSTS), pick(PORTS[:6]), pick(NAMES)
    forms = [
        f'ss://{b64(m + ":" + p)}@{h}:{port}#{n}',
        f'ss://{m}:{p}@{h}:{port}#{n}',
        f'ss://{b64(m + ":" + p + "@" + h + ":" + port)}#{n}',
        f'ss://{b64(m + ":" + p + "@" + h + ":" + port + "#" + n)}',
        f'ss://{b64(m + ":" + p)}@{h}:{port}/?plugin=obfs-local%3Bobfs%3Dtls#{n}',
        f'ss://{b64(m + ":" + p)}@{h}:{pick(PORTS)}#{n}',
        f'ss://{b64(m)}@{h}:{port}#{n}',
        f'ss://{b64(m + ":" + p)}@{h}#{n}',
        f'ss://{b64(m + ":" + p)}@{h}:{port}',
        f'ss://{b64(m + ":" + p)}',
        f'ss://{m}:{p}@@{h}:{port}#{n}',
        f'ss://@{h}:{port}#{n}',
        f'ss://{b64(m + ":" + p)}@{h}:{port}#',
        f'ss://{b64(b"".join([bytes([R.randrange(256)]) for _ in range(12)]))}@{h}:{port}#{n}',
    ]
    return pick(forms)


def gen_ssr():
    h, port = pick(HOSTS), pick(PORTS[:6] + [pick(PORTS)])
    proto, method, obfs = pick(['origin', 'auth_aes128_md5']), pick(['aes-256-cfb', 'chacha20']), pick(['plain', 'tls1.2_ticket_auth'])
    pw = b64(pick(PASSWORDS))
    main = f'{h}:{port}:{proto}:{method}:{obfs}:{pw}'
    params = []
    if R.random() < .8:
        params.append(('obfsparam', b64('obfs.example.com')))
    if R.random() < .6:
        params.append(('protoparam', b64('1:pw')))
    if R.random() < .8:
        params.append(('remarks', b64(pick(NAMES))))
    if R.random() < .3:
        params.append(('group', b64('grp')))
    if R.random() < .1:
        params.append(('remarks', 'not base64!'))
    tail = ''
    if params or R.random() < .3:
        tail = '/?' + qs(params)
    body = main + tail
    if R.random() < .1:
        body = main.rsplit(':', 1)[0]
    return 'ssr://' + b64(body)


def gen_vmess():
    d = {'v': '2', 'ps': pick(NAMES), 'add': pick(HOSTS[:6]), 'port': pick([443, '443', 8080, 443.0, '443', None, 'x', True, 0]),
         'id': uid(), 'aid': pick([0, '0', 2, '2', 64, None, '', 1.5]), 'scy': pick(['auto', 'aes-128-gcm', 'none']),
         'net': pick(['tcp', 'ws', 'grpc', 'h2', 'kcp', 'ws']), 'type': 'none', 'host': pick(['', 'example.com']),
         'path': pick(['/', '/ws', '/a b', '']), 'tls': pick(['tls', '', 'none', 'TLS']), 'sni': 'example.com'}
    for key in list(d):
        if R.random() < .15:
            del d[key]
    if R.random() < .05:
        d = pick([[1, 2], 'str', 5, None])
    text = json.dumps(d, ensure_ascii=R.random() < .5)
    return 'vmess://' + b64(text)


def gen_vless():
    sec = pick(['none', 'tls', 'reality', 'xtls', None, 'tls'])
    typ = pick(['tcp', 'ws', 'grpc', 'h2', 'http', None, 'tcp'])
    items = []
    if sec is not None:
        items.append(('security', sec))
    if typ is not None:
        items.append(('type', typ))
    for key, vals in [('encryption', ['none', 'mlkem']), ('sni', ['example.com', '%E4%BE%8B%E5%AD%90.test']), ('fp', ['chrome', 'firefox']),
                      ('pbk', ['pubkey' + uid()[:8]]), ('sid', ['ab12', '']), ('spx', ['%2F', '/']), ('ech', ['echconfig', '']),
                      ('path', ['%2Fws', '/p', '']), ('host', ['example.com', '']), ('serviceName', ['grpcsvc', '']),
                      ('headerType', ['none', 'http']), ('flow', ['xtls-rprx-vision', '']), ('alpn', ['h2,http/1.1', 'h2', '']),
                      ('extra', ['1', 'a+b', 'a%20b'])]:
        if R.random() < .4:
            items.append((key, pick(vals)))
    if R.random() < .15 and items:
        items.append(pick(items))  # duplicate key -> list value
    if R.random() < .1:
        items.append(('alpn', 'h2'))
        items.append(('alpn', 'http/1.1'))
    R.shuffle(items)
    q = ('?' + qs(items)) if items or R.random() < .1 else ''
    port = pick(PORTS[:5] + [pick(PORTS)])
    h = pick(HOSTS)
    forms = [f'vless://{uid()}@{h}:{port}{q}#{pick(NAMES)}', f'vless://{uid()}@{h}:{port}{q}', f'vless://{uid()}@{h}{q}#{pick(NAMES)}',
             f'vless://{uid()}{h}:{port}{q}', f'vless://{uid()}@{h}:{port}{q}#{pick(NAMES)}']
    return pick(forms)


def gen_trojan():
    items = []
    for key, vals in [('sni', ['example.com', 'a.example.com']), ('allowInsecure', ['true', '1', 'false', '0']), ('type', ['ws']),
                      ('peer', ['example.com'])]:
        if R.random() < .5:
            items.append((key, pick(vals)))
    if R.random() < .1 and items:
        items.append(pick(items))
    q = ('?' + qs(items)) if items else ''
    h, port = pick(HOSTS), pick(PORTS[:5] + [pick(PORTS)])
    forms = [f'trojan://{pick(PASSWORDS)}@{h}:{port}{q}#{pick(NAMES)}', f'trojan://{pick(PASSWORDS)}@{h}:{port}{q}',
             f'trojan://{pick(PASSWORDS)}{h}:{port}', f'trojan://{pick(PASSWORDS)}@{h}{q}#{pick(NAMES)}',
             f'trojan://{pick(PASSWORDS)}@{h}:{port}#{pick(NAMES)}{q}']
    return pick(forms)


def gen_hy():
    v2 = R.random() < .6
    items = []
    for key, vals in [('obfs', ['salamander', '']), ('obfs-password', ['pw']), ('sni', ['example.com']), ('peer', ['peer.example.com']),
                      ('insecure', ['1', '0', 'true']), ('auth', ['x']), ('upmbps', ['100']), ('alpn', ['h3'])]:
        if R.random() < .4:
            items.append((key, pick(vals)))
    if R.random() < .1 and items:
        items.append(pick(items))
    q = ('?' + qs(items)) if items else ''
    h, port = pick(HOSTS), pick(PORTS[:5] + [pick(PORTS)])
    pre = 'hysteria2://' if v2 else 'hysteria://'
    forms = [f'{pre}{pick(PASSWORDS)}@{h}:{port}{q}#{pick(NAMES)}', f'{pre}{h}:{port}{q}#{pick(NAMES)}', f'{pre}{pick(PASSWORDS)}@{h}:{port}{q}',
             f'{pre}{h}{q}', f'{pre}@{h}:{port}/{q}']
    return pick(forms)


def gen_object():
    base = {'name': pick(NAMES) or 'n', 'type': pick(['ss', 'vmess', 'trojan', 'tuic', 'snell', 'wireguard', 'anytls', 'hysteria2', 'http']),
            'server': pick(HOSTS[:6]), 'port': pick([443, 8080, '443', 65535])}
    extra = {'cipher': 'aes-128-gcm', 'password': 'pw', 'udp': True, 'tls': True, 'sni': 'example.com', 'skip-cert-verify': False,
             'alpn': ['h2', 'h3'], 'ws-opts': {'path': '/', 'headers': {'Host': 'example.com'}}, 'dialer-proxy': 'Relay',
             'version': 5, 'psk': 'abcd', 'obfs-opts': {'mode': 'tls', 'host': 'example.com'}, 'weight': 1.5, 'ip': '192.0.2.1',
             'private-key': 'k', 'reserved': [1, 2, 3], 'enabled': 'yes', 'tilde': '~', 'octal': '0o17', 'nullv': None}
    for k in list(extra):
        if R.random() < .35:
            base[k] = extra[k]
    style = R.random()
    if style < .3:
        return json.dumps(base, ensure_ascii=False)
    if style < .6:
        def flow(v):
            if isinstance(v, dict):
                return '{' + ', '.join(f'{k}: {flow(x)}' for k, x in v.items()) + '}'
            if isinstance(v, list):
                return '[' + ', '.join(flow(x) for x in v) + ']'
            if v is None:
                return 'null'
            if isinstance(v, bool):
                return 'true' if v else 'false'
            if isinstance(v, str):
                return json.dumps(v, ensure_ascii=False) if R.random() < .5 else (v if re.fullmatch(r'[A-Za-z0-9_.\-/]+', v) and v not in ('yes', 'on', '~') else json.dumps(v, ensure_ascii=False))
            return str(v)
        text = flow(base)
        return ('- ' + text) if R.random() < .2 else text
    import yaml
    text = yaml.safe_dump(base, allow_unicode=True, sort_keys=False)
    if style < .8:
        return text
    return '- ' + text.replace('\n', '\n  ', text.count('\n') - 1)


JUNK = ['', '   ', '# comment', 'tuic://' + uid() + ':pw@example.com:443?congestion_control=bbr#t', 'snell://x', 'wireguard://x', 'anytls://pw@example.com:443#a',
        'http://example.com', 'plain text', 'proxies:', '- ', '-', '- {', '{}', '{"a":', ']', '﻿ss://x', 'ss:// ', 'vless://', 'trojan://', 'vmess://', 'ssr://',
        'hysteria://', 'hysteria2://', '- ss://', '-  trojan://p@example.com:443#x']
GENS = [gen_ss, gen_ssr, gen_vmess, gen_vless, gen_trojan, gen_hy, gen_object]


def mutate(s):
    if not s:
        return s
    kind = R.randrange(7)
    i = R.randrange(len(s))
    if kind == 0:
        return s[:i]
    if kind == 1:
        return s[:i] + s[i + 1:]
    if kind == 2:
        return s[:i] + pick(['@', ':', '#', '?', '&', '=', '%', '/', ' ', '"', '{', '\n', ' ', '　', '﻿']) + s[i:]
    if kind == 3:
        return s.replace(':', '', 1)
    if kind == 4:
        return s.replace('@', '', 1)
    if kind == 5:
        return s.replace('#', '', 1)
    return s + pick(['=', '==', '\r', '\t', ' ', ' ', '%2', '%zz', '#', '?'])


def parser_rows():
    rows = []
    fixtures = [
        'ss://YWVzLTEyOC1nY206cGFzc3dvcmQ@example.com:8388#Test%20Node',
        'ss://YWVzLTEyOC1nY206cGFzc3dvcmQ=@example.com:8388#n',
        'trojan://password@example.com:443?sni=example.com&allowInsecure=1#T',
        'hysteria2://auth@example.com:443?sni=example.com&insecure=1#H2',
        'name: x\ntype: ss\nserver: example.com\nport: 443\ncipher: aes-128-gcm\npassword: p\n',
        '- name: a\n  type: ss\n  server: example.com\n  port: 1\n- name: b\n  type: trojan\n',
        '- name: a\n  type: ss\n- 5\n- [1]\n',
        '- name:\n',
        '{"name":"x","type":"ss","server":"example.com","port":1}',
        '{name: x, type: ss, server: example.com, port: 443}\n{name: y, type: ss, server: example.com, port: 444}',
        '[1,2,3]', '{"a":1}', 'a: [', '{\n  "name": "j",\n  "type": "vmess"\n}',
    ]
    inputs = list(fixtures)
    for _ in range(2200):
        parts = []
        for _ in range(R.choice([1, 1, 1, 2, 3, 5])):
            r = R.random()
            if r < .12:
                parts.append(pick(JUNK))
            else:
                g = pick(GENS)
                s = g()
                if R.random() < .25:
                    s = mutate(s)
                if R.random() < .1:
                    s = '- ' + s
                if R.random() < .1:
                    s = '  ' + s + pick(['  ', '\t', ''])
                parts.append(s)
        sep = pick(['\n', '\n', '\r\n', '\n\n'])
        text = sep.join(parts)
        if R.random() < .05:
            text = '\n' + text + '\n'
        inputs.append(text)
    for text in inputs:
        rows.append({'fn': 'parse_uri_list', 'in': text, 'out': call(sp.parse_uri_list, text)})
    # per-scheme single lines (more weight on parameter variations)
    for g in GENS[:6]:
        for _ in range(250):
            s = g()
            if R.random() < .3:
                s = mutate(s)
            rows.append({'fn': 'parse_uri_list', 'in': s, 'out': call(sp.parse_uri_list, s)})
    # base64 decoding semantics (CPython 3.11 binascii)
    alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'
    for _ in range(2500):
        n = R.randrange(0, 14)
        chars = [pick(alphabet) if R.random() < .85 else pick(['=', '-', '_', ' ', '\n', '\r', '.', '!', 'é', ' ']) for _ in range(n)]
        s = ''.join(chars)
        try:
            out = {'hex': base64.b64decode(s).hex()}
        except BaseException as exc:  # noqa: BLE001
            out = {'error': type(exc).__name__}
        rows.append({'fn': 'b64decode', 'in': s, 'out': out})
    # str.strip / int() semantics through the parsers' helpers
    for s in ['', ' 12 ', '1_000', '1__0', '_1', '1_', '+5', '-5', '٣٤', '１２', '0x10', '1e3', '  1　', '1\x1c', '9' * 4301, '9' * 40, '-0', '+', '١٢٣', '1 2', '²']:
        try:
            out = int(s)
        except BaseException as exc:  # noqa: BLE001
            out = {'error': type(exc).__name__}
        rows.append({'fn': 'int', 'in': s, 'out': out})
    return rows


dump('parser.jsonl', parser_rows())


# --------------------------------------------------------------------- filter
from backend.utils import subscription_filter as sf  # noqa: E402

PATTERNS = ['香港|HK', '^(?!.*剩余).*$', '(', '[a-z', '(?i)^hk', r'\d+', '(?<=a)b', r'(a)\1', '*', 'a{2,1}', '(?P<n>x)', '^(?!.*(过期|流量)).*$',
            ' x ', '\\', '(?x) a b', '.*', 'a++', '(?>a)', r'\p{L}', '日本|JP', '^HK', 'HK$', r'(?i)jp\s*\d', '(?!x)', '(?<!a)b', 'x{3}', 'a{1,2}?',
            '[^\\w]', r'\bhk\b', '(?s).', '^$', '', '   ', '(?P<a>x)(?P=a)', '[[:alpha:]]', '\\Qa\\E', '(?i)香港', 'a|', '|', '(?:)', '^(?=.*香港)(?!.*IPLC).*$',
            'x{1001}', '(?#c)x', r'\N{DIGIT ONE}', '(?(1)a|b)', 'a**', '+', '?', '(?', '(?P<1>a)', r'\8', r'[\d-x]', '(a)(b)', '(?i:a)b', '(?-i:A)']
NAMES_F = ['香港 01', '日本 01', 'HK-02', 'hk 03', 'JP 5', '剩余流量 100G', '过期时间 2026', 'IPLC 香港', 'a', 'b', 'ab', 'x', 'xxx', 'axb', '', ' ',
           'Name With Spaces', '123', 'abcabc', 'ÀÉ', 'ǅ', '\n', 'a\nb', 'AbA', '_', '😀']


def filter_rows():
    rows = []
    values = [None, 123, 1.5, True, [], {}, '', ' ', '\t']
    for p in PATTERNS:
        values += [p, ' ' + p + ' ', p + '\n']
    for v in values:
        rows.append({'fn': 'validate_regex_filter', 'in': v, 'out': call(sf.validate_regex_filter, v)})
    for p in PATTERNS:
        for variant in (p, ' ' + p + ' '):
            for sub in ({'regex_filter': variant, 'name': 'S'}, ):
                items = [{'name': n} for n in NAMES_F] + ['str', None, 5, {'nom': 'x'}, {'name': 5}, {'name': None}, {'name': ['香港']}]
                R.shuffle(items)
                rows.append({'fn': 'filter_subscription_items', 'in': {'sub': sub, 'items': items},
                             'out': call(sf.filter_subscription_items, sub, items)})
    for sub in (None, 5, 'x', [], {}, {'regex_filter': 5}, {'regex_filter': None}, {'regex_filter': ['a']}):
        items = [{'name': 'a'}, 'x']
        rows.append({'fn': 'filter_subscription_items', 'in': {'sub': sub, 'items': items}, 'out': call(sf.filter_subscription_items, sub, items)})
    rows.append({'fn': 'filter_subscription_items', 'in': {'sub': {'regex_filter': 'a'}, 'items': None}, 'out': call(sf.filter_subscription_items, {'regex_filter': 'a'}, None)})
    return rows


dump('filter.jsonl', filter_rows())

# ---------------------------------------------------------------------- hosts
from backend.utils import subscription_hosts as sh  # noqa: E402

DOMS = ['jp-node.example.net', 'hk-node.example.net', 'Node.Example.Org.', 'a.test', 'b.test', 'c.test', 'x.example.com', 'node_1.test',
        'bad_label-.test', 'localhost', '198.51.100.1', 'example', '例子.test', '-a.test', 'a' * 64 + '.test', ('a.' * 130) + 'com', '1.2.3']
VALS = ['198.51.100.12', '2001:db8::12', '2001:DB8:0:0:0:0:0:12', '::ffff:192.0.2.1', 'fe80::1%eth0', '0.0.0.0', '::', '224.0.0.1', 'ff02::1',
        'jp-entry.example.org', 'lan', 'Entry.Example.ORG.', '192.0.2.1,192.0.2.2', '010.0.0.1', '1.2.3', ' 192.0.2.5 ', 'node.test', '~', '']


def hosts_value():
    r = R.random()
    if r < .55:
        return pick(VALS)
    if r < .8:
        return [pick(VALS) for _ in range(R.choice([0, 1, 1, 2, 3, 9]))]
    return pick([{'a': 1}, 5, None, True, [1, 2], [None], [['x']], 1.5])


def yaml_scalar(v):
    import yaml
    return yaml.safe_dump(v, default_flow_style=True, allow_unicode=True).strip().removesuffix('\n...').strip()


def hosts_text():
    n = R.choice([0, 1, 2, 3, 5, 12])
    keys = []
    for _ in range(n):
        k = pick(DOMS + ['*.example.net', '+.example.net', '.example.net', 'google.com', 'ok.test', 'dup.test', '"quoted.test"', "'single.test'"])
        keys.append(k)
    style = R.randrange(5)
    key_word = pick(['hosts', 'hosts', 'hosts', '"hosts"', "'hosts'", 'hosts '])
    pre = pick(['proxies: []\n', 'mixed-port: 7890\nproxies:\n  - {name: a, type: http, server: a.test, port: 80}\n', '', '# hosts: x\n', '\n',
                'proxies:\n  - name: n\n    type: http\n    hosts: ignored\n', 'dns:\n  hosts: nope\n'])
    post = pick(['', 'proxy-groups: []\n', 'rules: []\n', '# trailing\n', 'x: 1\n'])
    nl = pick(['\n', '\n', '\n', '\r\n', ' ', '\x1c'])
    if style == 0:
        body = key_word + ': {' + ', '.join(f'{k}: {yaml_scalar(hosts_value())}' for k in keys) + '}\n'
    elif style == 1:
        body = key_word + ':\n' + ''.join(f'  {k}: {yaml_scalar(hosts_value())}\n' for k in keys)
    elif style == 2:
        body = key_word + ':\n' + ''.join(f'  {k}:\n' + ''.join(f'  - {pick(VALS)}\n' for _ in range(R.choice([1, 2]))) for k in keys)
    elif style == 3:
        body = key_word + ':\n' + ''.join(f'  # c\n  {k}: {pick(VALS)}\n\n' for k in keys)
    else:
        body = key_word + pick([': [198.51.100.1]\n', ': 5\n', ':\n', ': ~\n', ': "text"\n', ':\n  a.test: [unclosed\n', ': {a: 1\n', ':\n\t bad: tab\n'])
    text = pre + body + post
    if nl != '\n':
        text = text.replace('\n', nl)
    return text


def hosts_rows():
    rows = []
    texts = ['no hosts here', '', 'hosts', 'c3M6Ly9ZV1Z6TFRJMU5pMW5ZMjA2Y0dGemMzZHZjbVE9QDE5OC41MS4xMDAuMTo4Mzg4I2hvc3Rz',
             'proxies: []\nhosts: {node.test: 198.51.100.4}\n', 'hosts:\n  node.test:\n  - 198.51.100.4\nproxies: []\n']
    texts += [hosts_text() for _ in range(900)]
    big = {f'n{i}.test': '198.51.100.1' for i in range(1030)}
    import yaml
    texts.append(yaml.safe_dump({'proxies': [], 'hosts': big}))
    texts.append(yaml.safe_dump({'proxies': [], 'hosts': {'a.test': 'b.test', 'b.test': 'a.test'}}))
    records = []
    for t in texts:
        out = call(sh.extract_subscription_hosts, t)
        records.append(out)
        rows.append({'fn': 'extract_subscription_hosts', 'in': t, 'out': out})
    good = [r for r in records if isinstance(r, dict) and 'entries' in r]
    for _ in range(300):
        a, b = R.choice(good + [None, {}]), R.choice(good + [None, {}])
        rows.append({'fn': 'hosts_digest', 'in': a, 'out': call(sh.hosts_digest, a)})
        rows.append({'fn': 'describe_change', 'in': {'previous': a, 'current': b}, 'out': call(sh.describe_change, a, b)})
    for v in ['*.a.test', '+.a.test', '.a.test', 'a.test', 'A.TEST.', 'x.*.test', '*.*.test', '+.', '.', '', 5, None, 'a.test '] :
        for d in ['a.test', 'x.a.test', 'y.x.a.test', 'b.test', 'x.b.test', 'a.x.test']:
            rows.append({'fn': '_matches', 'in': {'pattern': v, 'domain': d}, 'out': call(sh._matches, v, d)})
    rows += merge_rows(good)
    return rows


def merge_rows(good):
    rows = []
    rich = [r for r in good if r['entries']]
    for case in range(1600):
        nsub = R.choice([1, 1, 2, 3])
        subs, caches = [], {}
        for i in range(nsub):
            sid = f's{i}'
            rec = R.choice(rich) if rich and R.random() < .9 else None
            servers = []
            if rec:
                servers = list(rec['entries'])
            nodes = []
            for j in range(R.choice([1, 2, 4])):
                server = pick(servers) if servers and R.random() < .75 else pick(['other.example.com', '203.0.113.9', 'jp-node.example.net', ''])
                nodes.append({'name': pick(['HK 01', 'JP 01', 'SG 01', 'X', f'n{j}']), 'type': 'socks5', 'server': server, 'port': 1})
            if R.random() < .1:
                nodes.append('junk')
            url = f'https://sub.test/{sid}' if R.random() < .9 else None
            sub = {'id': sid, 'name': pick(['机场', 'Air', f'Sub {i}', '']), 'url': url, 'enabled': R.random() < .9}
            if R.random() < .3:
                sub['regex_filter'] = pick(['HK', 'JP|HK', '^(?!.*SG).*$', '(', ' ', 'zzz'])
            subs.append(sub)
            meta = {'url': url if R.random() < .9 else 'https://sub.test/rotated'}
            if rec is not None:
                meta['hosts'] = rec
            caches[sid] = {'nodes': nodes, 'metadata': meta} if R.random() < .95 else None
        aggs = []
        if R.random() < .4:
            aggs.append({'id': 'agg', 'name': 'Agg', 'enabled': R.random() < .9, 'subscriptions': [s['id'] for s in subs if R.random() < .7],
                         'nodes': ['imp'] if R.random() < .5 else [], 'regex_filter': pick(['', 'JP|Imported', '(', 'HK', None])})
        cfg_nodes = [{'id': 'imp', 'name': 'Imported', 'enabled': True, 'server': pick(['jp-node.example.net', 'x.example.com']),
                      'subscription_id': pick([s['id'] for s in subs])},
                     {'id': 'own', 'name': 'Own', 'enabled': True, 'server': 'google.com'}]
        config = {'subscriptions': subs, 'subscription_aggregations': aggs, 'nodes': cfg_nodes}
        providers = {}
        for s in subs:
            if R.random() < .85:
                q = pick(['', '?token=t', '?regex=HK', '?regex=%E9%A6%99&exclude_regex=SG', '?regex=(&x=1', '?exclude_regex=JP', '?regex=+'])
                providers[s['name'] or s['id']] = {'type': 'http', 'url': f"https://panel.test/api/subscriptions/{s['id']}/proxies{q}"}
        for a in aggs:
            if R.random() < .85:
                q = pick(['', '?regex=JP', '?exclude_regex=HK'])
                providers['Agg'] = {'type': 'http', 'url': f"https://panel.test/api/aggregations/{a['id']}/proxies{q}"}
        if R.random() < .1:
            providers['Odd'] = pick([5, {'url': 5}, {'url': 'https://x/api/subscriptions/nope/proxies'}, {'url': 'https://x/%73ubscriptions/s0'}])
        main = {'proxy-providers': providers}
        if R.random() < .6:
            main['hosts'] = pick([{'jp-node.example.net': '192.0.2.10', '+.wild.example.net': '192.0.2.11'}, {'+.example.org': 'jp-node.example.net'},
                                  {}, 'notdict', {'a': ['1.1.1.1']}])
        inline = []
        for n in cfg_nodes:
            if R.random() < .5:
                inline.append({'node': n, 'proxy': R.choice([None, {'server': 'hk-node.example.net'}, {}, {'server': 'jp-node.example.net'}])})
        user_keys = [pick(['jp-node.example.net', '+.wild.example.net', '*.example.net', 'x.example.com'])] if R.random() < .3 else []
        mosdns_keys = [pick(['hk-node.example.net', 'x.example.com'])] if R.random() < .3 else []
        scenario = {'main': main, 'config': config, 'caches': caches, 'user_keys': user_keys, 'mosdns_keys': mosdns_keys, 'inline': inline}
        rows.append({'fn': 'merge_subscription_hosts', 'in': scenario, 'out': run_merge(scenario)})
    return rows


def run_merge(sc):
    import backend.utils.subscription_cache as sc_mod
    tmp = tempfile.mkdtemp()
    paths = {}
    for sid in sc['caches']:
        p = os.path.join(tmp, sid)
        open(p, 'w').write(sid)
        paths[sid] = p
    saved = (sc_mod._get_cache_path, sc_mod.load_subscription_cache)
    sc_mod._get_cache_path = lambda sid: paths.get(sid, os.path.join(tmp, 'missing-' + str(sid)))
    sc_mod.load_subscription_cache = lambda sid: copy.deepcopy(sc['caches'].get(sid))
    sh._index.clear()
    main = copy.deepcopy(sc['main'])
    merged = {}
    try:
        inline = [(copy.deepcopy(i['node']), copy.deepcopy(i['proxy'])) for i in sc['inline']]
        report = call(sh.merge_subscription_hosts, main, copy.deepcopy(sc['config']), user_keys=sc['user_keys'],
                      mosdns_keys=sc['mosdns_keys'], inline_proxies=inline, merged_out=merged)
    finally:
        sc_mod._get_cache_path, sc_mod.load_subscription_cache = saved
        sh._index.clear()
    return {'report': report, 'main': main, 'merged': merged}


dump('hosts.jsonl', hosts_rows())


# -------------------------------------------------------------------- dialers
from backend.utils import subscription_dialers as sd  # noqa: E402

PROXY_NAMES = ['HK 01', 'HK 02', 'JP 01', 'SG 01', 'US 01', 'Relay Node', 'x']
GROUP_NAMES = ['Relay', 'Hidden', 'Chain', '中转', 'a`b', 'Auto', 'proxy']


def airport_yaml():
    proxies = [{'name': n, 'type': 'ss', 'server': 'example.com', 'port': 1} for n in R.sample(PROXY_NAMES, R.randrange(1, 6))]
    names = [p['name'] for p in proxies]
    groups = []
    for g in R.sample(GROUP_NAMES, R.randrange(1, 5)):
        members = [pick(names + [x['name'] for x in groups] + GROUP_NAMES[:3] + [5, None]) for _ in range(R.randrange(0, 4))]
        group = {'name': g, 'type': pick(['select', 'url-test', 'fallback', 'load-balance', 'relay', 5]), 'proxies': members}
        for key, val in [('url', 'http://example.com/204'), ('interval', 300), ('tolerance', 50), ('lazy', True), ('timeout', 5000),
                         ('expected-status', 204), ('strategy', 'round-robin'), ('max-failed-times', 3), ('url', ['x']), ('interval', None),
                         ('lazy', 1.5)]:
            if R.random() < .25:
                group[key] = val
        if R.random() < .15:
            group.pop('proxies')
            group[pick(['use', 'filter', 'include-all'])] = pick([['p'], 'HK', True])
        if R.random() < .05:
            group['proxies'] = pick(['abc', {'HK 01': 1}, 5, None])
        groups.append(group)
    for p in proxies:
        if R.random() < .6:
            p['dialer-proxy'] = pick(GROUP_NAMES + names + [5, None, 'DIRECT'])
    doc = {'proxies': proxies, 'proxy-groups': groups}
    if R.random() < .05:
        doc['proxies'] = pick(['x', {'a': 1}, 5, [1, 'a']])
    import yaml
    text = yaml.safe_dump(doc, allow_unicode=True, sort_keys=False)
    if R.random() < .05:
        text += pick(['  bad: [unclosed\n', '\t\n'])
    return text


def conv(node):
    if node.get('type') == 'bad':
        return None
    out = {'name': node['name'], 'type': node.get('type', 'ss'), 'server': node.get('server'), 'port': node.get('port')}
    out.update(copy.deepcopy(node.get('params') or {}))
    return out


def run_add_dialer(sc):
    import backend.utils.subscription_cache as sc_mod
    import backend.converters.mihomo as cm
    saved = (sc_mod.load_subscription_cache, cm.convert_node_to_mihomo)
    sc_mod.load_subscription_cache = lambda sid: copy.deepcopy(sc['caches'].get(sid))
    cm.convert_node_to_mihomo = lambda n: conv(copy.deepcopy(n))
    main = copy.deepcopy(sc['main'])
    try:
        report = call(sd.add_dialer_groups, main, copy.deepcopy(sc['config']))
    finally:
        sc_mod.load_subscription_cache, cm.convert_node_to_mihomo = saved
    return {'report': report, 'main': main}


def dialers_rows():
    rows = []
    texts = [airport_yaml() for _ in range(700)]
    records = []
    for t in texts + ['proxies: []', 'dialer-proxy', '- a\n- dialer-proxy', 'dialer-proxy: x']:
        out = call(sd.extract_subscription_dialers, t)
        records.append(out)
        rows.append({'fn': 'extract_subscription_dialers', 'in': t, 'out': out})
    # many targets
    proxies = [{'name': f'P{i}', 'type': 'ss', 'dialer-proxy': f'G{i:03d}'} for i in range(60)]
    groups = [{'name': f'G{i:03d}', 'type': 'select', 'proxies': ['P0', 'P1']} for i in range(60)]
    import yaml
    t = yaml.safe_dump({'proxies': proxies, 'proxy-groups': groups})
    rows.append({'fn': 'extract_subscription_dialers', 'in': t, 'out': call(sd.extract_subscription_dialers, t)})
    deep = {'proxies': [{'name': 'A', 'dialer-proxy': 'G1'}], 'proxy-groups': [{'name': 'G1', 'proxies': ['G2']}, {'name': 'G2', 'proxies': ['G1', 'A']}]}
    t = yaml.safe_dump(deep)
    rows.append({'fn': 'extract_subscription_dialers', 'in': t, 'out': call(sd.extract_subscription_dialers, t)})
    good = [r for r in records if isinstance(r, dict) and 'targets' in r]
    for _ in range(500):
        record = R.choice(good + [None])
        sub = pick(['Air', '机场', 'a/b'])
        proxies = []
        for _ in range(R.randrange(0, 5)):
            p = {'name': pick(PROXY_NAMES), 'type': 'ss'}
            r = R.random()
            if r < .6:
                p['dialer-proxy'] = pick(GROUP_NAMES + [5, None, f'{sub}/Relay'])
            proxies.append(p)
        proxies += [pick(['x', 5, None])] if R.random() < .1 else []
        names = [{'name': pick(PROXY_NAMES + GROUP_NAMES + [f'{sub}/Relay', f'{sub}/Hidden'])} for _ in range(R.randrange(0, 4))] + ([{}] if R.random() < .1 else [])
        main = pick([None, {}, {'proxies': names[:2], 'proxy-groups': names[2:]}])
        chains = R.random() < .8
        sc = {'subscription': sub, 'proxies': proxies, 'main': main, 'record': record, 'chains': chains}
        rows.append({'fn': 'adapt_subscription_proxies', 'in': sc,
                     'out': call(sd.adapt_subscription_proxies, sub, copy.deepcopy(proxies), copy.deepcopy(main), copy.deepcopy(record), chains=chains)})
    # add_dialer_groups scenarios
    for _ in range(900):
        nsub = R.choice([1, 1, 2])
        subs, caches = [], {}
        for i in range(nsub):
            sid = f's{i}'
            body = airport_yaml()
            record = call(sd.extract_subscription_dialers, body)
            record = None if isinstance(record, dict) and 'error' in record and 'targets' not in record else record
            names = [pick(PROXY_NAMES) for _ in range(R.randrange(1, 6))]
            nodes = []
            for n in dict.fromkeys(names):
                params = {'cipher': 'aes-128-gcm'}
                if R.random() < .6:
                    params['dialer-proxy'] = pick(GROUP_NAMES + ['DIRECT', 5, 'Relay'])
                node = {'name': n, 'type': pick(['ss', 'ss', 'bad']), 'server': 'example.com', 'port': 1, 'params': params}
                if R.random() < .1:
                    node['proxy_string'] = pick(['dialer-proxy: Relay\ntype: ss', 'ss://x', '{"type":"ss","dialer-proxy":"Hidden"}'])
                if R.random() < .05:
                    node['params'] = None
                nodes.append(node)
            nodes += [pick([{'name': ''}, 'junk', {'name': 5}])] if R.random() < .1 else []
            meta = {'url': 'https://sub.test/' + sid}
            if R.random() < .85:
                meta['dialers'] = record if R.random() < .9 else None
            sub = {'id': sid, 'name': pick(['Air', '机场', 'Air2']), 'url': 'https://sub.test/' + sid, 'enabled': R.random() < .95}
            if R.random() < .3:
                sub['regex_filter'] = pick(['^HK', '^(?!.*SG).*$', 'JP', '('])
            subs.append(sub)
            caches[sid] = {'nodes': nodes, 'metadata': meta}
        direct = [s for s in subs if R.random() < .7]
        providers = {}
        for s in direct:
            providers[s['name']] = {'type': 'http', 'url': f"https://panel.test/api/subscriptions/{s['id']}/proxies"}
        aggs = []
        if R.random() < .4:
            aggs.append({'id': 'agg', 'name': 'Agg', 'enabled': True, 'subscriptions': [s['id'] for s in subs]})
            providers['Agg'] = {'type': 'http', 'url': 'https://panel.test/api/aggregations/agg/proxies'}
        main = {'proxy-providers': providers}
        if R.random() < .5:
            main['proxies'] = [{'name': pick(['Air/Relay', 'user', 'Air/HK 01', '机场/Hidden'])}]
        if R.random() < .5:
            main['proxy-groups'] = [{'name': pick(['Air/Relay', 'Relay', 'Hidden', 'Air/Hidden', '机场/Hidden', 'Air2/Relay'])}]
        # a node name may equal a group name used as dialer
        sc = {'main': main, 'config': {'subscriptions': subs, 'subscription_aggregations': aggs}, 'caches': caches}
        rows.append({'fn': 'add_dialer_groups', 'in': sc, 'out': run_add_dialer(sc)})
    rows += aligned_dialer_rows()
    return rows


def aligned_dialer_rows():
    import yaml
    rows = []
    for _ in range(900):
        nsub = R.choice([1, 1, 2])
        subs, caches = [], {}
        for i in range(nsub):
            sid = f's{i}'
            names = R.sample(['HK 01', 'HK 02', 'JP 01', 'SG 01', 'US 01', 'Relay A', 'Relay B', 'Entry'], R.randrange(3, 7))
            groups = []
            gnames = R.sample(['Relay', 'Hidden', '中转', 'Chain'], R.randrange(1, 4))
            for gi, g in enumerate(gnames):
                members = R.sample(names, R.randrange(1, 4)) + ([gnames[gi - 1]] if gi and R.random() < .5 else [])
                group = {'name': g, 'type': pick(['select', 'url-test', 'fallback', 'relay']), 'proxies': members}
                if R.random() < .5:
                    group.update(pick([{'url': 'http://example.com/204', 'interval': 300}, {'tolerance': 20, 'lazy': True}, {'strategy': 'consistent-hashing'}]))
                groups.append(group)
            proxies = []
            for n in names:
                p = {'name': n, 'type': 'ss', 'server': 'example.com', 'port': 1}
                if R.random() < .7:
                    p['dialer-proxy'] = pick(gnames + ['Elsewhere', 'DIRECT'])
                proxies.append(p)
            body = yaml.safe_dump({'proxies': proxies, 'proxy-groups': groups}, allow_unicode=True, sort_keys=False)
            record = sd.extract_subscription_dialers(body)
            nodes = [{'name': p['name'], 'type': 'ss', 'server': 'example.com', 'port': 1,
                      'params': {k: v for k, v in p.items() if k not in ('name', 'type', 'server', 'port')}} for p in proxies]
            if R.random() < .1:
                nodes[0]['type'] = 'bad'
            meta = {'url': 'https://sub.test/' + sid}
            if R.random() < .9:
                meta['dialers'] = record
            sub = {'id': sid, 'name': pick(['Air', '机场', 'Air2']), 'url': 'https://sub.test/' + sid, 'enabled': True}
            if R.random() < .4:
                sub['regex_filter'] = pick(['^HK', '^(?!.*Relay).*$', 'HK|Entry|JP', 'SG'])
            subs.append(sub)
            caches[sid] = {'nodes': nodes, 'metadata': meta}
        direct = [s for s in subs if R.random() < .6]
        providers = {s['name']: {'type': 'http', 'url': f"https://panel.test/api/subscriptions/{s['id']}/proxies"} for s in direct}
        aggs = []
        if R.random() < .4:
            aggs.append({'id': 'agg', 'name': 'Agg', 'enabled': True, 'subscriptions': [s['id'] for s in subs]})
            providers['Agg'] = {'type': 'http', 'url': 'https://panel.test/api/aggregations/agg/proxies'}
        main = {'proxy-providers': providers}
        taken = [f"{s['name']}/{n}" for s in subs for n in ('Relay', 'Hidden', 'HK 01', '中转')]
        if R.random() < .15:
            main['proxy-groups'] = [{'name': pick(taken)}]
        if R.random() < .1:
            main['proxies'] = [{'name': pick(taken)}]
        sc = {'main': main, 'config': {'subscriptions': subs, 'subscription_aggregations': aggs}, 'caches': caches}
        rows.append({'fn': 'add_dialer_groups', 'in': sc, 'out': run_add_dialer(sc)})
    return rows


dump('dialers.jsonl', dialers_rows())


# --------------------------------------------------------------------- chains
from backend.utils import proxy_chains as pc  # noqa: E402
from backend.utils.dialer_references import validate_dialers  # noqa: E402

NODE_IDS = ['a', 'b', 'c', 'd', 'e']


def rand_ref(chains_so_far, groups_ids, allow_chain=True):
    r = R.random()
    if r < .55:
        return {'type': 'node', 'id': pick(NODE_IDS + ([] if R.random() < .95 else ['missing']))}
    pool = groups_ids + (chains_so_far if allow_chain else [])
    return {'type': 'group', 'id': pick(pool) if pool else 'nogroup'}


def chain_profile():
    nodes = []
    for nid in NODE_IDS[:R.randrange(2, 6)]:
        node = {'id': nid, 'name': nid.upper(), 'type': 'http', 'server': f'{nid}.example.test', 'port': 80}
        if R.random() < .15:
            node['enabled'] = R.random() < .5
        if R.random() < .1:
            node['subscription_id'] = 'feed'
        if R.random() < .15:
            node['params'] = {'dialer-proxy': pick(['A', 'B', 'Chain', 'Entry', 'DIRECT', 'nope', 5, ''])}
        if R.random() < .1:
            node['proxy_string'] = pick(['type: http\nserver: x\ndialer-proxy: B', '{"type":"http","dialer-proxy":"A"}', 'ss://abc'])
        if R.random() < .1:
            node['params'] = {'headers': {'User-Agent': 'shared'}}
        if R.random() < .12:
            node['dialer_ref'] = rand_ref([], [])
        nodes.append(node)
    groups = []
    gids = ['entry', 'pool', 'dyn', 'follow', 'chooser']
    for gid in R.sample(gids, R.randrange(0, 5)):
        g = {'id': gid, 'name': gid.title(), 'type': pick(['select', 'select', 'url-test'])}
        r = R.random()
        if r < .35:
            g['manual_nodes'] = R.sample(NODE_IDS, R.randrange(1, 4))
        elif r < .5:
            g['proxies_order'] = [{'type': 'node', 'id': pick(NODE_IDS)}] + ([{'type': 'builtin', 'id': 'DIRECT'}] if R.random() < .3 else [])
        elif r < .6:
            g['manual_nodes'] = ['DIRECT'] if R.random() < .5 else [pick(NODE_IDS)]
        elif r < .7:
            g['subscriptions'] = ['feed']
        elif r < .78:
            g['aggregations'] = ['agg']
        elif r < .85:
            g['use'] = [pick(['Feed', 'Agg', 'Nope'])]
            g['manual_nodes'] = ['a']
        elif r < .9:
            g['source'] = pick(['node', 'strategy'])
            g['proxies'] = [pick(NODE_IDS)]
        if gid == 'follow' and R.random() < .8:
            g['follow_group'] = pick(gids + ['missing'])
        if R.random() < .1:
            g['enabled'] = False
        if R.random() < .1:
            g['proxy_order'] = 'strategies_first'
        groups.append(g)
    ids = [g['id'] for g in groups]
    chain_ids = []
    for cid in R.sample(['chain', 'outer', 'third'], R.randrange(0, 4)):
        chain = {'id': cid, 'name': cid.title(), 'type': 'chain',
                 'chain': {'entry': rand_ref(chain_ids, ids), 'exit': rand_ref(chain_ids, ids)}}
        if R.random() < .1:
            chain['enabled'] = False
        r = R.random()
        if r < .04:
            chain['chain'] = pick([[], None, {'entry': chain['chain']['entry']}, {'entry': None, 'exit': None}])
        elif r < .08:
            chain['chain']['entry']['extra'] = 1
        elif r < .1:
            chain['manual_nodes'] = ['a']
        chain_ids.append(cid)
        groups.append(chain)
    if chain_ids:
        for g in list(groups):
            if g.get('type') != 'chain' and R.random() < .5:
                if R.random() < .5:
                    g.setdefault('include_groups', []).append(pick(chain_ids))
                elif R.random() < .5:
                    g.setdefault('proxies_order', []).append({'type': 'strategy', 'id': pick(chain_ids)})
                else:
                    g['source'] = 'strategy'
                    g['proxies'] = [pick(chain_ids)]
        if R.random() < .3:
            groups.append({'id': 'g_chooser', 'name': 'Choose', 'type': 'select', 'manual_nodes': ['b'], 'include_groups': [pick(chain_ids)]})
    if R.random() < .1 and groups:
        groups.append(copy.deepcopy(pick(groups)))
    if R.random() < .05 and groups:
        groups[0]['name'] = pick(['B', 'DIRECT', 'Entry', ''])
    profile = {'profile_id': 'default', 'nodes': nodes, 'proxy_groups': groups, 'rule_configs': []}
    if R.random() < .8:
        profile['subscriptions'] = [{'id': 'feed', 'name': 'Feed', 'url': 'https://feed.example.test', 'enabled': R.random() < .9}]
        profile['subscription_aggregations'] = [{'id': 'agg', 'name': 'Agg', 'subscriptions': ['feed'], 'nodes': ['b']}]
    if R.random() < .03:
        profile['nodes'] = pick([None, 'x', [1], [{'id': ''}]])
    return profile


def clean_profile():
    nodes = [{'id': nid, 'name': nid.upper(), 'type': 'http', 'server': f'{nid}.example.test', 'port': 80} for nid in 'abcd']
    if R.random() < .3:
        nodes[1]['params'] = {'headers': {'User-Agent': 'shared'}}
    if R.random() < .2:
        nodes[2]['enabled'] = True
    if R.random() < .3:
        nodes[3]['dialer_ref'] = {'type': 'node', 'id': pick('abc')}
    if R.random() < .15:
        nodes[2]['dialer_ref'] = {'type': 'node', 'id': pick('ab')}
    groups = [{'id': 'entry', 'name': 'Entry', 'type': 'select', 'manual_nodes': R.sample('abcd', R.randrange(1, 4))},
              {'id': 'pool', 'name': 'Pool', 'type': pick(['select', 'url-test']), 'manual_nodes': R.sample('abcd', R.randrange(1, 4))}]
    if R.random() < .6:
        dyn = {'id': 'dyn', 'name': 'Dyn', 'type': 'select', 'manual_nodes': ['a']}
        dyn.update(pick([{'subscriptions': ['feed']}, {'aggregations': ['agg']}, {'use': ['Feed']}, {'use': ['Agg']}]))
        groups.append(dyn)
    if R.random() < .4:
        groups.append({'id': 'follow', 'name': 'Follow', 'type': 'select', 'follow_group': pick(['entry', 'pool']), 'enabled': R.random() < .8})
    if R.random() < .3:
        groups.append({'id': 'ordered', 'name': 'Ordered', 'type': 'select',
                       'proxies_order': [{'type': 'node', 'id': pick('abcd')}, {'type': 'strategy', 'id': 'pool'}]})
    gids = [g['id'] for g in groups]
    chains = []
    for cid in ['chain', 'outer', 'third'][:R.randrange(1, 4)]:
        entry = pick([{'type': 'node', 'id': pick('ab')}, {'type': 'group', 'id': pick(gids)}] + ([{'type': 'group', 'id': chains[-1]}] if chains else []))
        exit_ = pick([{'type': 'node', 'id': pick('bcd')}, {'type': 'group', 'id': pick(['pool', 'entry'])}] + ([{'type': 'group', 'id': chains[0]}] if chains and R.random() < .5 else []))
        chain = {'id': cid, 'name': cid.title(), 'type': 'chain', 'chain': {'entry': entry, 'exit': exit_}}
        if R.random() < .1:
            chain['enabled'] = False
        chains.append(cid)
        groups.append(chain)
    chooser = {'id': 'chooser', 'name': 'Choose', 'type': 'select', 'manual_nodes': [pick('abcd')]}
    r = R.random()
    if r < .4:
        chooser['include_groups'] = R.sample(chains, R.randrange(1, len(chains) + 1))
    elif r < .6:
        chooser['proxies_order'] = [{'type': 'strategy', 'id': pick(chains)}, {'type': 'node', 'id': 'a'}]
    elif r < .75:
        chooser = {'id': 'chooser', 'name': 'Choose', 'type': 'select', 'source': 'strategy', 'proxies': [pick(chains)]}
    elif r < .85:
        chooser['proxy_order'] = 'strategies_first'
        chooser['include_groups'] = [pick(chains)]
    groups.append(chooser)
    profile = {'profile_id': 'default', 'nodes': nodes, 'proxy_groups': groups, 'rule_configs': [],
               'subscriptions': [{'id': 'feed', 'name': 'Feed', 'url': 'https://feed.example.test', 'enabled': True}],
               'subscription_aggregations': [{'id': 'agg', 'name': 'Agg', 'subscriptions': ['feed'], 'nodes': ['b']}]}
    return profile


def build_main(runtime):
    """A stand-in for the emitted Mihomo document: one proxy per node, one group per group, providers per source."""
    names = {n['id']: n['name'] for n in runtime.get('nodes', []) if isinstance(n, dict)}
    proxies = [{'name': n['name'], 'type': 'http'} for n in runtime.get('nodes', []) if isinstance(n, dict) and n.get('enabled', True)]
    providers = {}
    for p in [*runtime.get('subscriptions', []), *runtime.get('subscription_aggregations', [])]:
        providers[p['name']] = {'type': 'http', 'url': f"https://panel.test/{p['id']}", 'path': f"./providers/{p['id']}.yaml",
                                'filter': pick(['', '', 'HK', 'JP`HK']), 'exclude-filter': pick(['', 'x', 'y']), 'interval': 3600}
    groups = []
    for g in runtime.get('proxy_groups', []):
        if not isinstance(g, dict) or not g.get('enabled', True):
            continue
        entry = {'name': g['name'], 'type': g.get('type', 'select')}
        members = [names.get(i) for i in g.get('manual_nodes', [])]
        entry['proxies'] = [m for m in members if m]
        if g.get('_chain_dialer'):
            use = [p for p in providers if R.random() < .6]
            if use:
                entry['use'] = use
            flt = pick(['', 'HK', 'a`b', 'HK`JP`SG', '(US)`(JP)', 'x`', '`', '(?:a)|b`c'])
            if flt:
                entry['filter'] = flt
            if R.random() < .3:
                entry['exclude-filter'] = pick(['x', 'a`b', '('])
            if R.random() < .2:
                entry['exclude-type'] = pick(['http', 'ss|http', ''])
            if R.random() < .2 and g.get('manual_nodes'):
                entry['proxies'].append('Ghost')
        groups.append(entry)
    return {'proxies': proxies, 'proxy-groups': groups, 'proxy-providers': providers}


def chains_rows():
    rows = []
    for _ in range(1800):
        profile = clean_profile() if R.random() < .65 else chain_profile()
        before = copy.deepcopy(profile)
        validated = call(validate_dialers, profile)
        lowered = call(pc.lower_proxy_chains, profile)
        mutated = profile != before
        row = {'fn': 'chains', 'in': before,
               'out': {'validate': validated, 'lower': lowered, 'input_mutated': mutated}}
        if isinstance(lowered, dict) and 'error' not in lowered or (isinstance(lowered, dict) and 'nodes' in lowered):
            main = build_main(lowered)
            row['out']['providers_in'] = copy.deepcopy(main)
            pr = call(pc.lower_chain_providers, lowered, main)
            row['out']['providers_result'] = pr
            row['out']['providers_main'] = main
        rows.append(row)
    return rows


dump('chains.jsonl', chains_rows())


# ------------------------------------------------------------ proxy_server_dns
from backend.utils import proxy_server_dns as psd  # noqa: E402

SUFFIXES = ['com', 'org', 'net', 'co.uk', 'github.io', 'blogspot.com', 'com.cn', 'cn', 'invalid', 'lab.invalid', 'test', 'jp', 'co.jp', 'xyz', 'ck',
            'www.ck', 'kobe.jp', 'city.kobe.jp', 'appspot.com', 'cloudfront.net', 'pages.dev', 'workers.dev', 'herokuapp.com', 'local', 'localhost', 'io', 'museum']
LABELS = ['a', 'b', 'node', 'hk1', 'jp2', 'example', 'other', 'x-y', 'www', 'sub', 'deep', '1', 'xn--fiq228c', '例子', 'UPPER']


def rand_domain():
    labels = [pick(LABELS) for _ in range(R.randrange(0, 4))]
    return '.'.join(labels + [pick(['example', 'node', 'air', 'shared'])] + [pick(SUFFIXES)])


def dns_main(mode=None):
    dns = {'enable': True, 'listen': pick(['0.0.0.0:1053', '127.0.0.1:1053', '[::]:53', ':7874', '192.0.2.2:53', '[::1]:1053'])}
    if R.random() < .9:
        dns['proxy-server-nameserver'] = ['https://223.5.5.5/dns-query']
    if R.random() < .75:
        dns['enhanced-mode'] = 'fake-ip'
    if R.random() < .4:
        dns['fake-ip-filter-mode'] = pick(['blacklist', 'whitelist', 'rule', 'other', None, ''])
    if R.random() < .5:
        dns['fake-ip-filter'] = pick([['+.lan', 'time.*.com', 'geosite:cn', 'rule-set:x', 'dns.msftncsi.com'], [], None, 'text', ['+.example.org', '*.node.test'],
                                      ['DOMAIN,a.example.net,real-ip', 'DOMAIN-SUFFIX,lan,real-ip'], [5, 'x.test']])
    if R.random() < .35:
        dns[psd.POLICY_KEY] = pick([{'hk1.example.net': 'udp://127.0.0.1:1053'}, {'+.example.net': '8.8.8.8'}, {'geosite:cn': '223.5.5.5'},
                                    {'a.example.org, b.example.org': ['127.0.0.1:1053']}, {}, 'x', {'*.example.org': 'tcp://localhost:1053'},
                                    {'jp1.example.net': ['udp://[::1]:1053', 'udp://127.0.0.1:1053']}, {'hk1.example.net': '0.0.0.0:1053'},
                                    {'hk1.example.net': 'udp://user@127.0.0.1:99999'}, {'hk1.example.net': '127.0.0.1:0'}])
    r = R.random()
    if r < .05:
        dns['enable'] = pick([False, None, 'true', 1])
    elif r < .08:
        dns['use-hosts'] = False
    elif r < .1:
        dns['listen'] = pick([None, '', 'x', 'localhost:53', '0.0.0.0'])
    return dns


def dns_rows():
    rows = []
    for _ in range(900):
        domains = [rand_domain() for _ in range(R.randrange(0, 7))]
        if R.random() < .4 and domains:
            base = domains[0]
            domains += [pick(LABELS) + '.' + base.split('.', 1)[-1] for _ in range(R.randrange(1, 3))]
        rows.append({'fn': 'compress', 'in': sorted(set(domains)), 'out': call(psd.compress, set(domains))})
    for s in ['0.0.0.0:1053', '[::]:53', ':7874', '127.0.0.1:7874', '192.168.1.2:53', '[::1]:1053', 'localhost:53', '0.0.0.0', '0.0.0.0:0', '', None, 5,
              ' 127.0.0.1:53 ', '[fe80::1%eth0]:53', '224.0.0.1:53', '[ff02::1]:53', '::1:53', '1.2.3.4:65535', '1.2.3.4:65536', '1.2.3.4:٥٣', '1.2.3.4:+53',
              '1.2.3.4:5_3', '[::ffff:192.0.2.1]:53', '192.0.2.1:053', '256.1.1.1:53', 'host:53', '[]:53', '[::', ':::53']:
        rows.append({'fn': 'listener_address', 'in': s, 'out': call(lambda x: list(psd.listener_address(x)), s)})
    for s in ['v1.19.33', 'v1.19.20', 'v1.19.19', 'Mihomo Meta v1.18.10 linux amd64', '1.20.0', 'alpha-1a2b3c4', '', None, 5, 'V1.19.20', 'mihomo v1.19.20',
              'meta 1.19.0', '  v2.0.0', 'v١.١٩.٢٠', 'v1.19', 'v1.19.20-alpha', 'vv1.1.1', 'mihomometa 1.1.1', 'v01.019.020', 'v1.19.99999999999999999999']:
        rows.append({'fn': 'parse_core_version', 'in': s, 'out': call(lambda x: (list(psd.parse_core_version(x)) if psd.parse_core_version(x) else None), s)})
        rows.append({'fn': 'core_too_old', 'in': s, 'out': call(psd.core_too_old, s)})
    for _ in range(400):
        hosts = {}
        for _ in range(R.randrange(0, 7)):
            key = pick(['a.test', 'b.test', 'c.test', 'x.test', '+.wild.test', '*.wild.test', '.dot.test', 'n.wild.test', 'W.Test.', ' sp.test '])
            hosts[key] = pick(['b.test', 'c.test', '198.51.100.1', ['198.51.100.2'], ['n.wild.test'], 'n.wild.test', 'a.test', 'x.test', ['a.test', 'b.test'], 5, None,
                               'C.TEST.', [], '::1', ['2001:db8::1'], 'y.dot.test'])
        for d in ['a.test', 'b.test', 'x.test', 'w.wild.test', 'missing.test', 'z.dot.test']:
            rows.append({'fn': 'final_target', 'in': {'hosts': hosts, 'domain': d}, 'out': call(psd.final_target, hosts, d)})
    for _ in range(1400):
        main = {'dns': dns_main()}
        if R.random() < .9:
            main['hosts'] = {}
        merged = {}
        for _ in range(R.randrange(1, 5)):
            d = pick(['hk1.example.net', 'jp1.example.net', 'a.example.org', 'b.example.org', 'c.example.com', 'de1.example.net', 'n.test', 'x.github.io', 'y.github.io'])
            target = pick(['hk1.example.org', 'entry.example.org', 'a.entry.example.org', 'b.entry.example.org', ['198.51.100.9'], 'x.example.co.uk', 'y.example.co.uk',
                           'e.lab.invalid', 'f.lab.invalid', 'z.github.io'])
            merged[d] = target
            if 'hosts' in main:
                main['hosts'][d] = target[0] if isinstance(target, list) and len(target) == 1 and R.random() < .5 else target
        if R.random() < .15 and 'hosts' in main:
            main['hosts']['hk1.example.org'] = pick(['final.example.org', '198.51.100.1', 'a.example.org'])
        core = pick([None, None, 'v1.19.33', 'v1.19.19', 'alpha-x', 'v1.19.20', '1.18.0'])
        m = copy.deepcopy(main)
        report = call(psd.apply_node_dns, m, copy.deepcopy(merged), core_version=core)
        rows.append({'fn': 'apply_node_dns', 'in': {'main': main, 'merged': merged, 'core_version': core}, 'out': {'report': report, 'main': m}})
    return rows


dump('dns.jsonl', dns_rows())
