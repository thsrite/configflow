"""Reference corpus for internal/substore from the real Python modules.

Run on the test server from the repository root:
  PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/substore/testdata/gen.py
Writes server/internal/substore/testdata/corpus.json (deterministic, synthetic values only).
"""
import json
import os
import sys
from unittest import mock

import requests

from backend.utils import dependencies as deps
from backend.utils import sub_store_client as client
from backend.utils.subscription_health import parse_userinfo

rows = []


def err(exc):
    return {'error': type(exc).__name__, 'message': str(exc)}


def add(fn, value, call):
    try:
        out = {'output': call()}
    except Exception as exc:  # noqa: BLE001
        out = err(exc)
    rows.append({'fn': fn, 'input': value, **out})


# ---- parse_userinfo
headers = [
    '', 'upload=1; download=2; total=100', 'upload=1; download=2; total=3; expire=4',
    'UPLOAD=5; Download = 6 ;TOTAL=7', 'upload=1.9; download=2e3; total=1E+2', 'upload=.5; download=5.; total=+3',
    'upload=1e400', 'upload=x; total=7', 'total=1.2.3; expire=9', 'upload==1', 'upload=; total=1',
    'xupload=1; total=2', '我upload=1; total=2', 'upload_x=1', 'upload=1,download=2', 'total=123456789012345678901234567890',
    'total=1e20', 'total=1e16', 'total=9007199254740993', 'expire=1700000000; total=0', 'a=1; b=2', 'total\t=\t5',
    'total\u3000=\u30005', 'total\x1c=1', 'upload=1; upload=9', 'total=e5', 'total=+', 'total=.', 'expire=1e-5',
    'téotal=1; total=4', 'TOTAL=1;total=2', 'up\u0130load=1', 'total=12abc', 'total=1e5e5', 'total=0001',
    'download=99999999999999999999999.5', 'k=1 total=2', 'total=1 2', 'Σtotal=1', 'total = 1.0e+300',
]
for h in headers:
    add('parse_userinfo', h, lambda h=h: parse_userinfo(h))

# ---- parse_version / is_newer
versions = ['2.42.3', 'v2.42.3', 'V2.42', ' 2.42.3 ', '2.42.3\n', 'bad', '', '2..3', '2.42.x', '２.４２', '2. 3',
            '2.\x1c3', '2.\xa03', '2.\x853', '2.-1', '2.+1', '2.1_0', '2.1__0', '2._1', '2.1_', 'vv2.1', '2.42.3.0',
            '𝟘.1', '٣.٤', '2.' + '9' * 30, '2.' + '9' * 4301, '\x1c2.1', '2.1\u2028', '0', '-0', '1.2.3-beta', None, 5]
for v in versions:
    add('parse_version', v, lambda v=v: list(deps.parse_version(v)) if deps.parse_version(v) is not None else None)
pairs = [('2.42.3', '2.42.2'), ('v2.43.0', '2.42.10'), ('2.42.3', '2.42.3'), ('bad', '2.42.3'), ('2.42.3', ''),
         ('2.42', '2.42.0'), ('2.42.0', '2.42'), ('2.42.1', 'bad'), ('', ''), ('10.0', '9.9'), ('２.43', '2.42'),
         ('2.-1', '2.0'), ('1.0.0', '1')]
for a, b in pairs:
    add('is_newer', [a, b], lambda a=a, b=b: deps.is_newer(a, b))

# ---- builtin_sub_store_url
ports = ['3001', '', '0', '1', '65535', '65536', '-1', '+3001', '30.01', '3001/path', '３００１', ' 3001 ', '03001',
         '003001', '\xa03001', '3001\x1c', '1e3', '0x10', '٣٠٠١', '99999', '15802']
for p in ports:
    def url(p=p):
        with mock.patch.dict(os.environ, {'SUB_STORE_BACKEND_API_PORT': p}):
            return deps.builtin_sub_store_url()
    add('builtin_sub_store_url', p, url)

# ---- _is_yaml_response
texts = ['proxies: []', '  proxies:\n  - a', '<!DOCTYPE html>', '<html><body>', 'x' * 10 + '<head>', 'x' * 600 + '<head>',
         'mixed-port: 1\nproxies: []', '{"proxies": []}', 'a: 1', '[1, 2]', '', '   ', 'ss://abc', 'x' * 300 + '\nproxies: []',
         'proxies', '{proxies: 1}', ': : :', 'a: [', '\ufeffproxies: []', '#c\nproxies:\n', '中' * 199 + 'proxies:',
         '中' * 201 + 'proxies: []', 'Proxies: []', '\x1cproxies: []']
for t in texts:
    add('is_yaml_response', t, lambda t=t: client._is_yaml_response(t))

# ---- parse_proxies_from_yaml + proxies_to_nodes
docs = [
    'proxies:\n  - {name: A, type: SS, server: s, port: 1, cipher: c, password: p}\n',
    'proxies:\n  - {name: V, type: vless, server: s, port: 443, uuid: u, reality-opts: {public-key: k}}\n',
    'proxies:\n  - {name: V, type: vless, encryption: zero}\n', 'proxies: []', 'a: 1', '[1]', 'proxies: null',
    'proxies: 5', 'proxies: ""', 'proxies: abc', 'proxies: {}', 'proxies: {a: 1}', 'proxies: [null, 0, ""]',
    'proxies: [x]', 'proxies:\n  - {type: Ünïcode, name: n}\n', 'proxies:\n  - {type: 5}\n', 'proxies:\n  - {}\n',
    'proxies:\n  - {name: A, port: 1, extra: [1, 2], server: s}\n', 'a: [', '', 'proxies:\n  - {type: VMESS, z: 1, a: 2}\n',
]
for d in docs:
    def parse(d=d):
        proxies = client.parse_proxies_from_yaml(d)
        return proxies if isinstance(proxies, list) else {'non_list': proxies}
    add('parse_proxies_from_yaml', d, parse)

    def nodes(d=d):
        proxies = client.parse_proxies_from_yaml(d)
        return client.proxies_to_nodes(proxies)
    add('proxies_to_nodes', d, nodes)

# ---- proxy type of the conversion log line
schemes = ['vless://x', 'SS://x', 'ss+obfs://x', 'hysteria2://x', 'a.b-c://x', '://x', 'no-scheme', 'Ünïcode://x',
           'İ://x', 'ΣΑΣ://x', 'a b://x', 'x_y://x', 'a' * 40 + '://x', '中文://x', '１２://x', 'ss://a://b', '++://x']
for s in schemes:
    def proxy_type(s=s):
        lines = []
        with mock.patch.object(client, '_get_base_url', lambda: 'http://127.0.0.1:9'), \
                mock.patch.object(client, '_create_subscription', lambda *a, **k: True), \
                mock.patch.object(client, '_delete_subscription', lambda *a, **k: None), \
                mock.patch.object(client.requests, 'get', mock.Mock(side_effect=ValueError('x'))), \
                mock.patch.object(client.logger, 'info', lambda msg, *a: lines.append(msg % a)), \
                mock.patch.object(client.logger, 'warning', lambda *a: None), \
                mock.patch('backend.utils.dependencies.is_builtin_available', lambda: False):
            client._convert_proxy_string(s, 'ClashMeta')
        return lines[0]
    add('proxy_type_line', s, proxy_type)

# ---- requests URL preparation (GET with params)
urls = [
    ('http://127.0.0.1:3001/download/_cf_tmp_x/ClashMeta', [['noFlow', 'true']]),
    ('http://127.0.0.1:3001/download/a%2Fb/ClashMeta', [['content', 'vless://u@h:1?a=b&c=d#frag ment']]),
    ('http://h/sub?token=1', [['noFlow', 'true']]),
    ('http://h/sub?', [['a', 'b']]),
    ('http://h/sub#frag', [['a', 'b']]),
    ('http://h/a b/中文?q=1 2&x=%41%2F%zz', []),
    ('http://h/%7e%41%2f', []),
    ('http://h/p', [['k', '中 文+&=/']]),
    ('http://h/p?x=[1]', []),
    ('https://h/p|q', []),
    ('HTTP://Example.TEST:8080/a/./b/../c?x=1#f g', [['k', 'v']]),
    ('http://h/a/..', []), ('http://h', [['a', '1']]), ('http://h?x', []), ('http://h/%zz%41?q=%4a%zz', []),
    ('http://user:p w@h/p', []), ('http://h/p?q=a^b`c{d}', []), ('http://[::1]:3001/p', []), ('http://h:/p', []),
]
for u, params in urls:
    add('prepare_url', [u, params],
        lambda u=u, params=params: requests.Request('GET', u, params=[tuple(p) for p in params]).prepare().url)

# ---- response.text / response.json()
bodies = [
    ('text/plain', 'proxies: 中文'.encode()), ('text/plain; charset=utf-8', 'proxies: 中文'.encode()),
    ('', 'proxies: 中文'.encode()), ('application/json', '{"a": "中"}'.encode()), ('application/json', b'\xef\xbb\xbf{"a": 1}'),
    ('', b'\xef\xbb\xbf{"a": 1}'), ('text/yaml; charset="UTF-8"', b'a: \xe2\x82'), ('', b'a: \xe2\x82x\xff'),
    ('text/plain; charset=latin-1', b'caf\xe9'), ('text/plain; charset=ascii', b'caf\xe9'), ('application/octet-stream', b'abc'),
    ('', b''), ('application/json', b'not json'), ('', b'{"version": "2.40.0"}'),
]
for ctype, body in bodies:
    def make(ctype=ctype, body=body):
        r = requests.models.Response()
        r._content = body
        r.status_code = 200
        if ctype:
            r.headers['Content-Type'] = ctype
        # What HTTPAdapter.build_response does.
        r.encoding = requests.utils.get_encoding_from_headers(r.headers)
        return r
    key = [ctype, list(body)]
    add('response_text', key, lambda make=make: make().text)
    add('response_json', key, lambda make=make: make().json())

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'corpus.json')
with open(out, 'w', encoding='utf-8') as f:
    json.dump(rows, f, ensure_ascii=False, indent=1)
    f.write('\n')
print(len(rows), 'rows', file=sys.stderr)
