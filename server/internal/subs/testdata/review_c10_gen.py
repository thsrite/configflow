#!/usr/bin/env python3
"""C10 adversarial review: Python reference results for edge inputs.

    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/subs/testdata/review_c10_gen.py \
        server/internal/subs/testdata/review_c10.jsonl.gz

One JSON object per line: {"fn", "in", "out"}; "out" is {"error": type} when
Python raised and {"unrepresentable": repr} when the result holds a value JSON
cannot carry (datetime, bytes, set). Synthetic values only.
"""
import base64
import gzip
import json
import sys
import unicodedata

from backend.utils import subscription_dialers as sd
from backend.utils import subscription_hosts as sh
from backend.utils import subscription_parser as sp

assert sys.version_info[:2] == (3, 11), sys.version
ROWS = []


class Unrepresentable(Exception):
    pass


def plain(value):
    if value is None or isinstance(value, (bool, str)):
        return value
    if isinstance(value, int):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unrepresentable(repr(value))
        return value
    if isinstance(value, list):
        return [plain(v) for v in value]
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if not isinstance(k, str):
                raise Unrepresentable('key ' + repr(k))
            out[k] = plain(v)
        return out
    raise Unrepresentable(type(value).__name__)


def emit(fn, arg, func):
    try:
        result = func(arg)
    except BaseException as exc:  # noqa: BLE001
        ROWS.append({'fn': fn, 'in': arg, 'out': {'error': type(exc).__name__}})
        return
    try:
        out = plain(result)
    except Unrepresentable as exc:
        out = {'unrepresentable': str(exc)}
    ROWS.append({'fn': fn, 'in': arg, 'out': out})


def b64(text, urlsafe=False, pad=True):
    data = text.encode()
    out = base64.urlsafe_b64encode(data) if urlsafe else base64.b64encode(data)
    out = out.decode()
    return out if pad else out.rstrip('=')


# ------------------------------------------------------------------- parser

uri_lines = []
# Every Unicode decimal digit as a port digit (int() semantics).
digits = [c for c in range(0x110000) if not 0xD800 <= c <= 0xDFFF and unicodedata.decimal(chr(c), None) is not None]
for c in digits:
    port = chr(c) + chr(c)
    uri_lines.append(f'trojan://pw@example.com:{port}#d{c:x}')
# Ports: odd forms.
for port in ['443', '0443', '+443', '-443', ' 443', '443 ', '　443', '443\x1c', '4_4_3', '4__43', '_443', '443_', '',
             '99999999999999999999999', '1' * 4300, '1' * 4301, '0x1bb', '4.43e2', '443.0', '٤٤٣', '４４３', '𝟒𝟒𝟑', '¹²³',
             '443\n', '443\t', '443 ', '443\x85', '443\xa0']:
    uri_lines += [f'trojan://pw@example.com:{port}#p', f'ss://{b64("aes-128-gcm:pw")}@example.com:{port}#p',
                  f'vless://u@example.com:{port}?security=tls#p', f'hysteria2://a@example.com:{port}#p']
# Names and fragments.
for name in ['', '香港 01', '🇭🇰 HK', 'İstanbul', 'a%20b', '%E9%A6%99', '%zz', 'a#b#c', 'x' * 500, 'tab\tname', 'nul\x00',
             'ZWJ 👨‍👩‍👧', 'ǅ', '‮RTL']:
    uri_lines += [f'trojan://pw@example.com:443#{name}', f'ss://{b64("aes-128-gcm:pw")}@example.com:443#{name}',
                  f'vless://u@example.com:443#{name}', f'hysteria://a@example.com:443#{name}']
# Base64 variants for ss user info / whole ss / ssr / vmess.
for urlsafe in (False, True):
    for pad in (False, True):
        for creds in ['aes-128-gcm:pw', 'chacha20-ietf-poly1305:p@ss:w?rd>>', '2022-blake3-aes-256-gcm:密码/+=', 'nocolon', 'm:']:
            uri_lines.append(f'ss://{b64(creds, urlsafe, pad)}@example.com:8388#b')
            uri_lines.append(f'ss://{b64(creds + "@example.com:8388", urlsafe, pad)}#whole')
            uri_lines.append(f'ss://{b64(creds + "@example.com:8388#inner", urlsafe, pad)}')
        for remarks in ['SSR 节点', '>>>???', 'a/b']:
            body = f'example.com:443:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:{b64("p>w?", urlsafe, pad)}' \
                   f'/?remarks={b64(remarks, urlsafe, pad)}&protoparam={b64("1:x", urlsafe, pad)}&obfsparam=&group={b64("g", urlsafe, pad)}'
            uri_lines.append('ssr://' + b64(body, urlsafe, pad))
        for vm in [{'v': '2', 'ps': '香港', 'add': 'example.com', 'port': '443', 'id': 'u', 'aid': '0', 'net': 'ws', 'tls': 'tls',
                    'path': '/p', 'host': 'h.example.com'},
                   {'ps': 1, 'add': ['x'], 'port': 443.9, 'aid': True, 'net': 'ws', 'tls': True},
                   {'port': ' ٤٤٣ ', 'aid': '1_0', 'scy': None, 'net': None},
                   {'port': None}, {'port': [443]}, {'port': 1e400}, {'aid': 'x'}, {}, {'port': -1, 'aid': -2},
                   {'port': 1.5e20, 'net': 'grpc'}, {'tls': 'TLS'}, {'port': 'NaN'}]:
            text = json.dumps(vm, ensure_ascii=False).replace('Infinity', '1e400')
            uri_lines.append('vmess://' + b64(text, urlsafe, pad))
uri_lines += ['vmess://' + b64('[1,2]'), 'vmess://' + b64('"x"'), 'vmess://' + b64('{"port": NaN}'),
              'vmess://' + b64('﻿{"ps": "bom"}'), 'vmess://' + b64('{"ps": "a", "ps": "b"}'),
              'vmess://' + base64.b64encode(b'\xff\xfe{"ps":"x"}').decode(), 'vmess://' + b64('{"port": 443}') + '\x00',
              'vmess://' + b64('{"port": 443}')[:-1], 'vmess://' + b64('{"port": 443}') + '====', 'vmess://e30', 'vmess://e30=']
# Query strings.
for query in ['security=tls&sni=a.example&fp=chrome&alpn=h2,h3', 'security=reality&pbk=k&sid=&spx=%2F&type=grpc&serviceName=s',
              'security=tls&ech=AAA%3D&type=ws&path=%2Fws%3Fed%3D2048&host=h.example', 'type=tcp&headerType=http',
              'type=tcp&headerType=none', 'type=tcp', 'headerType=http', 'security=tls&security=reality', 'alpn=h2&alpn=h3',
              'flow=xtls-rprx-vision&flow=', 'sni=%E4%BD%A0%E5%A5%BD', 'sni=%ff%fe', 'sni=a+b', 'sni=a%2Bb', 'sni=%zz',
              'a=1;b=2', '&&&', '=x', 'type=ws&path=', 'type=WS', 'encryption=aes&encryption=none', 'security=TLS&sni=x',
              'type=grpc&serviceName=%E6%9C%8D%E5%8A%A1', 'security=tls&ech=', 'alpn=', 'alpn=,']:
    uri_lines += [f'vless://uuid@example.com:443?{query}#q', f'trojan://pw@example.com:443?{query}#q',
                  f'hysteria2://a@example.com:443?{query}&obfs=salamander&insecure=1&peer=p.example#q',
                  f'trojan://pw@example.com:443?allowInsecure=true&{query}#q', f'hysteria://a@example.com:443?{query}#q']
# Hosts: IPv6 literals and zones, odd separators.
for host in ['[2001:db8::1]', '2001:db8::1', '[fe80::1%25eth0]', '[fe80::1%eth0]', '例子.测试', 'xn--fsqu00a.xn--0zwm56d',
             'EXAMPLE.COM', 'a:b', '', 'user@host']:
    uri_lines += [f'vless://u@{host}:443#h', f'trojan://pw@{host}:443#h', f'hysteria2://a@{host}:443#h',
                  f'ss://{b64("aes-128-gcm:pw")}@{host}:443#h']
# Truncate representative links at every position.
samples = [
    f'ss://{b64("aes-128-gcm:pa:ss")}@example.com:8388#SS',
    'ssr://' + b64('example.com:443:origin:aes-256-cfb:plain:' + b64('pw') + '/?remarks=' + b64('R')),
    'vmess://' + b64(json.dumps({'ps': 'V', 'add': 'example.com', 'port': 443, 'id': 'u', 'net': 'ws', 'path': '/'})),
    'vless://u@[2001:db8::1]:443?security=reality&pbk=k&type=grpc&serviceName=s&alpn=h2#V',
    'trojan://p@example.com:443?sni=s&allowInsecure=true#T',
    'hysteria2://a@example.com:443?obfs=o&peer=p&insecure=1#H',
    '{name: y, type: ss, server: example.com, port: 443}',
    '{"name": "j", "type": "ss", "server": "example.com", "port": 443}',
]
for sample in samples:
    for i in range(len(sample) + 1):
        uri_lines.append(sample[:i])
for line in uri_lines:
    emit('parse_uri_list', line, sp.parse_uri_list)

# Whole documents (YAML/JSON objects, lists, multi-line).
documents = [
    'name: a\nname: b\ntype: ss\n', '{name: a, name: b}', '{"name": "a", "name": "b"}', 'name: x\n1: y\n', '{1: 2, name: x}',
    'name: x\nport: 0443\n', 'name: x\nport: 0o17\n', 'name: x\nport: 1_000\n', 'name: x\nport: 0x1BB\n', 'name: x\nport: 1:30\n',
    'name: x\nport: 4.43e+2\n', 'name: x\nudp: yes\ntls: off\nx: ~\n', 'name: x\nd: 2001-12-14\n', 'name: x\nt: 2001-12-14t21:59:43.10-05:00\n',
    'name: x\nb: !!binary aGVsbG8=\n', 'name: x\ns: !!set {a, b}\n', 'name: x\no: !!omap [a: 1]\n', 'name: &a x\nother: *a\n',
    'base: &b {type: ss}\nname: x\n<<: *b\n', 'name: x\nk: .inf\nn: .nan\n', 'name: x\nv: 12345678901234567890123\n',
    '{"name": "x", "port": 1e400}', '{"name": "x", "v": NaN}', '{"name": "x", "big": 123456789012345678901234567890}',
    '{"name": "x", "f": 1.0, "e": 1E2, "neg": -0}', '﻿{"name": "bom"}', '{"name": "x"} trailing', "{'name': 'single'}",
    '- name: a\n- name: a\n  <<: {type: ss}\n', '- name: a\n  x: 2001-01-01\n', '- name: a\n- !!binary aGk=\n',
    '- type: ss\n  name: [1, 2]\n', '- name: x\n  name: y\n', 'xx name: y', 'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaname: z',
    'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaname: z', '香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香港香name: z',
    'name: x\r\ntype: ss\r\n', 'name:\tx\n', 'name: "a\\u00e9\\U0001F600"\n', "name: 'it''s'\n", 'name: |\n  multi\n  line\n',
    'name: x\n# comment\n', '---\nname: x\n', '---\nname: x\n---\nname: y\n', 'name: x\n...\n', '%YAML 1.1\n---\nname: x\n',
    'name: !custom x\n', 'name: x\n? complex\n: v\n', 'name: x\n"quoted key": 1\n', 'name: x\nnull: 1\n', 'name: x\n~: 1\n',
    'name: x\ntrue: 1\n', 'name: x\n[a]: 1\n', 'name: [unclosed\n', 'name: x\n\tbad: tab\n', 'name: x\nport: 08\n', 'name: x\nport: 09.5\n',
    'name: x\nport: +12\n', 'name: x\nport: -0x1A\n', 'name: x\nport: 0b101\n', 'name: x\nport: 190:20:30\n', 'name: x\nv: 1,000\n',
    'name: x\nv: [1,000]\n', 'name: x\nv: {a: 1,000}\n', 'name: x\nv: 6.8523015e+5\n', 'name: x\nv: 685.230_15e+03\n',
    'name: x\nv: -.inf\n', 'name: x\nv: .NaN\n', 'name: x\nv: Yes\n', 'name: x\nv: Y\n', 'name: x\nv: NO\n', 'name: x\nv: =\n',
    'name: x\nv: 2002-12-14 21:59:43.10 -5\n', 'name: x\nv: 2001-13-14\n',
]
for doc in documents:
    emit('parse_uri_list', doc, sp.parse_uri_list)
    emit('parse_yaml_object', doc, sp.parse_yaml_object)

# ------------------------------------------------------------------- hosts

host_bodies = [
    'hosts:\n  a.example.com: 1.2.3.4\n  a.example.com: 5.6.7.8\n',
    'hosts:\n  123: 1.2.3.4\n  b.example.com: 1.2.3.4\n',
    'hosts:\n  1.5: 1.2.3.4\n  true: 1.2.3.4\n  ~: 1.2.3.4\n  c.example.com: c2.example.com\n',
    'hosts:\n  K.example.com: 1.2.3.4\n  İ.example.com: 1.2.3.4\n  ß.example.com: 1.2.3.4\n',
    'hosts:\n  a.example.com: ::ffff:1.2.3.4\n  b.example.com: ::ffff:224.0.0.1\n  c.example.com: ::ffff:0.0.0.0\n  d.example.com: ::ffff:127.0.0.1\n',
    'hosts:\n  a.example.com: fe80::1%eth0\n  b.example.com: "fe80::1%"\n  c.example.com: FE80::A\n  d.example.com: 001.2.3.4\n',
    'hosts:\n  a.example.com: [1.2.3.4, 1.2.3.4, "2001:db8::1"]\n  b.example.com: []\n  c.example.com: [x.example.com]\n',
    'hosts:\n  a.example.com: [1.2.3.4, x.example.com]\n  b.example.com: 1\n  c.example.com: {a: 1}\n  d.example.com: null\n',
    'hosts:\n  a.example.com: A.Example.Com.\n  b.example.com: b.example.com\n  c.example.com: " c2.example.com "\n',
    'hosts:\r\n  a.example.com: 1.2.3.4\r\n  b.example.com: 1.2.3.5\r\nproxies: []\r\n',
    'hosts:\n\ta.example.com: 1.2.3.4\n', '"hosts" :\n  a.example.com: 1.2.3.4\n', "'hosts':\n  a.example.com: 1.2.3.4\n",
    'hosts: {a.example.com: 1.2.3.4, b.example.com: 2.3.4.5}\n', 'hosts:\n  a.example.com: 1.2.3.4\n# c\n\n  b.example.com: 1.2.3.5\nx: 1\n',
    'proxies: []\nhosts:\n  a.example.com: 1.2.3.4\n', 'xhosts:\n  a: b\n', ' hosts:\n  a.example.com: 1.2.3.4\n', 'hosts:\n',
    'hosts: 5\n', 'hosts: [a]\n', 'hosts:\n  a.example.com: [1.2.3.4\n', 'hosts:\n  "*.example.com": 1.2.3.4\n  +.e.com: 1.1.1.1\n  .e.com: 1.1.1.1\n',
    'hosts:\n  a..example.com: 1.2.3.4\n  -a.example.com: 1.2.3.4\n  a-.example.com: 1.2.3.4\n  1.2.3.4: 5.6.7.8\n  1.2: 3.4.5.6\n',
    'hosts:\n  ' + 'a' * 64 + '.com: 1.2.3.4\n  ' + 'a' * 63 + '.com: 1.2.3.4\n  ' + '.'.join(['a'] * 127) + ': 1.2.3.4\n',
    'hosts:\n  lan: 1.2.3.4\n  a_b.example.com: 1.2.3.4\n  例子.example.com: 1.2.3.4\n  xn--fsqu00a.example.com: 1.2.3.4\n',
    'hosts:\n  a.example.com: [1.1.1.1, 1.1.1.2, 1.1.1.3, 1.1.1.4, 1.1.1.5, 1.1.1.6, 1.1.1.7, 1.1.1.8, 1.1.1.9]\n',
    'hosts:\n' + ''.join(f'  h{i}.example.com: 10.0.{i // 256}.{i % 256}\n' for i in range(1005)),
    'hosts:\n' + ''.join(f'  *.w{i}.example.com: 1.2.3.4\n' for i in range(25)),
    'hosts:\n  a.example.com: 2001-12-14\n  b.example.com: !!binary aGk=\n',
    'hosts:\n  a.example.com: 1.2.3.4\n  <<: {b.example.com: 1.2.3.5}\n',
    'hosts:\n  a.example.com: &x 1.2.3.4\n  b.example.com: *x\n',
    'hosts:\n  a.example.com: 1.2.3.4\n\x85  b.example.com: 1.2.3.5\n',
    'hosts:\n  a.example.com: 1.2.3.4\n   b.example.com: 1.2.3.5\n',
    'hosts:\n  a.example.com:\n  - 1.2.3.4\n  - 1.2.3.5\nnext: 1\n',
    'hosts:\n  ' + 'x' * 300 + ': 1.2.3.4\n',
]
for body in host_bodies:
    emit('extract_subscription_hosts', body, sh.extract_subscription_hosts)

# ------------------------------------------------------------------- dialers

dialer_bodies = [
    'proxies:\n- {name: A, type: ss, dialer-proxy: R}\n- {name: B, type: ss}\nproxy-groups:\n- {name: R, type: select, proxies: [B]}\n- {name: R, type: fallback, proxies: [A, B]}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\n- {name: A, dialer-proxy: S}\nproxy-groups:\n- {name: R, proxies: [A]}\n- {name: S, proxies: [R, R, A]}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\nproxy-groups:\n- {name: R, proxies: [S]}\n- {name: S, proxies: [R]}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\n- {name: B}\nproxy-groups:\n- {name: R, type: url-test, url: u, interval: 300, lazy: true, tolerance: 1.5, timeout: [1], strategy: null, expected-status: 204, max-failed-times: "3", proxies: [B]}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\nproxy-groups:\n- {name: R, proxies: 5}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\nproxy-groups:\n- {name: R, proxies: "AB"}\n- {name: R2}\n',
    'proxies: 5\ndialer-proxy: x\n', 'proxies: abc\ndialer-proxy: x\n', 'proxy-groups: 7\nproxies: []\ndialer-proxy: x\n',
    'proxies:\n- {name: A, dialer-proxy: R}\n1: 2\nproxy-groups:\n- {name: R, proxies: [A]}\n',
    'proxies:\n- {name: A, dialer-proxy: 中转}\n- {name: B}\nproxy-groups:\n- {name: 中转, type: weird, proxies: [B, DIRECT]}\n',
    'proxies:\n' + ''.join(f'- {{name: N{i}, dialer-proxy: G{i:03d}}}\n' for i in range(60)) + 'proxy-groups:\n' +
    ''.join(f'- {{name: G{i:03d}, proxies: [N{(i + 1) % 60}]}}\n' for i in range(60)),
    'proxies:\n- {name: A, dialer-proxy: R}\n' + ''.join(f'- {{name: M{i}}}\n' for i in range(120)) +
    'proxy-groups:\n- {name: R, proxies: [' + ', '.join(f'M{i}' for i in range(120)) + ']}\n',
    'proxies:\n- {name: A, dialer-proxy: R}\n- {name: B}\nproxy-groups:\n- name: R\n  proxies: [B]\n  url: !!binary aGk=\n',
    'proxies:\n- {name: A, dialer-proxy: R}\n- {name: B}\nproxy-groups:\n- {name: R, proxies: [B], interval: 2001-01-01}\n',
    'proxies: [{name: A, dialer-proxy: R}, {name: B}]\nproxy-groups: [{name: R, use: [p], proxies: []}]\n',
    'dialer-proxy: [\n', '- dialer-proxy\n',
]
for body in dialer_bodies:
    emit('extract_subscription_dialers', body, sd.extract_subscription_dialers)

with gzip.GzipFile(sys.argv[1], 'wb', mtime=0) as handle:
    for row in ROWS:
        handle.write((json.dumps(row, ensure_ascii=True, separators=(',', ':')) + '\n').encode())
print(len(ROWS), 'rows', len(digits), 'digits')
