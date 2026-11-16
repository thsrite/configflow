"""Python references for review_c21_test.go (values jsonx cannot hold that
Python drops or skips). Run: PYTHONPATH=. ~/cf-test/.venv/bin/python <this file>"""
import json
from backend.converters import mosdns as m

out = {}
for text in ['plugins: !!set {a}\nextra: 1', 'servers: &r [*r]\nlog: !!omap [{a: 1}]\nkeep: x',
             '!!set {a}', '&r [*r]', 'api: !!pairs [{a: 1}]\ndata_providers: !!set {b}',
             '{1: x, extra: y}', 'extra: {1: x}', 'servers: 1\n2: two\nlog: {}\nplugins: [a]\nk: v\nnull: n\n1.5: f',
             '? !!binary aGk=\n: b\nz: 1\napi: {x: 1}']:
    try:
        out['generate:' + text] = ['ok', m.generate_mosdns_config({'mosdns': {'custom_config': text}})]
    except Exception as e:
        out['generate:' + text] = [type(e).__name__, str(e)]
for text in ['{1: x, extra: y, log: {a: 1}, api: 2}', 'servers: 1\n2: two\nlog: {}\nplugins: [a]\nk: v']:
    try:
        out['generate-nolog:' + text] = ['ok', m.generate_mosdns_config(
            {'mosdns': {'custom_config': text, 'log_enabled': False, 'api_enabled': False}})]
    except Exception as e:
        out['generate-nolog:' + text] = [type(e).__name__, str(e)]
for text in ['x: !!set {a}\npayload:\n  - DOMAIN,a.com\n  - !!set {b}\n  - +.b.org\n',
             'payload: &p [*p, "DOMAIN-SUFFIX,c.net"]\n']:
    for part in (None, 'domain', 'ip'):
        out[f'convert:{part}:' + text] = m.convert_rule_content_for_mosdns(text, part)
for text in ['- addr: 1.1.1.1\n- !!set {a}\n- 8.8.8.8', '!!set {a}', '&r [*r, 9.9.9.9]']:
    out['upstreams:' + text] = m.parse_dns_upstreams(text)
print(json.dumps(out, ensure_ascii=False, indent=1))
