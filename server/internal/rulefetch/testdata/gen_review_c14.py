"""Review corpus for C14 (adversarial review, 2026-11-16): edge inputs the
Python suite does not reach, answered by real CPython 3.11 / PyYAML 6.0.1 /
python-zstandard. Synthetic values only.

Run: PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen_review_c14.py
Writes testdata/review_c14.jsonl.gz (replayed by review_c14_test.go).
"""
import base64
import gzip
import html
import json
import os
import random
import struct
import sys
import unicodedata
from urllib.parse import urlsplit, urljoin

import zstandard

from backend.utils import ruleset_content as rc
from backend.utils import website_identity as wi
from backend.utils import rule_fetch as rf

sys.setrecursionlimit(5000)
rng = random.Random(1416)
rows = []


def jsonable(value):
    if isinstance(value, tuple):
        return [jsonable(v) for v in value]
    if isinstance(value, list):
        return [jsonable(v) for v in value]
    if isinstance(value, dict):
        return {k: jsonable(v) for k, v in value.items()}
    if isinstance(value, bytes):
        return {'__bytes__': value.hex()}
    return value


def has_surrogate(value):
    return isinstance(value, str) and any(0xD800 <= ord(c) <= 0xDFFF for c in value)


def add(fn, args, call):
    row = {'fn': fn, 'args': jsonable(args)}
    try:
        result = call()
    except Exception as exc:  # noqa: BLE001
        row['err'] = {'class': type(exc).__name__, 'text': str(exc)}
        errors = getattr(exc, 'errors', None)
        if type(exc).__name__ == 'RemoteRuleValidationError':
            row['err']['errors'] = errors
    else:
        row['ok'] = jsonable(result)
    text = json.dumps(row, ensure_ascii=False)
    if '\\ud8' in text or '\\udc' in text or '\\udd' in text or '\\ude' in text or '\\udf' in text:
        return  # lone surrogates cannot be held by Go strings
    rows.append(row)


# --- html.unescape ---------------------------------------------------------
ENTITY_PARTS = ['&', '&amp', '&amp;', '&AMP', '&lt', '&lt;', '&notin;', '&notit;', '&not', '&copy', '&copyx',
                '&AElig', '&Afr;', '&quot', '&#', '&#;', '&#x', '&#x;', '&#X41;', '&#65', '&#65;', '&#0;', '&#1;',
                '&#9', '&#13;', '&#x80;', '&#x81;', '&#x9f;', '&#127;', '&#xB;', '&#xFFFE;', '&#x10FFFF;',
                '&#xD800;', '&#xDFFF;', '&#1114112;', '&#99999999999999999999;', '&#x0000041;', '&#0000065',
                '&' + 'a' * 31 + ';', '&' + 'a' * 33 + ';', '&amp-x;', '&a.b;', '&lt.', 'x', ' ', ';', '#', '\n',
                '\r', '\t', '\f', '<', '中', '😀', '&中;', '&nbsp', '&nbsp;x', '&NotNestedLessLess;', '&ThickSpace;',
                '&acE;', '&fjlig;', '&#x1F600;', '&#128512;', '&hellip;', '&amp;amp;', '&&amp;']


def random_entity_text():
    return ''.join(rng.choice(ENTITY_PARTS) for _ in range(rng.randint(0, 8)))


for part in ENTITY_PARTS:
    add('unescape', [part], lambda part=part: html.unescape(part))
    add('unescape', [part + 'z;'], lambda part=part: html.unescape(part + 'z;'))
for _ in range(3000):
    text = random_entity_text()
    add('unescape', [text], lambda text=text: html.unescape(text))

# --- HTMLParser through _meta_content ---------------------------------------
META = [
    '', '<meta content="Example">', '<meta property="og:site_name" content="Site &amp; Co">',
    '<meta property="og:site_name" content="' + 'x' * 180, '<meta content=Example>', "<meta content='Example'>",
    '<META CONTENT="Upper">', '<meta content="a" content="b">', '<meta content>', '<meta content="">',
    '<meta content = "spaced">', '<meta content= "x">', "<meta content= 'x>", "<meta content ='x>", "<meta content='x>",
    '<meta content=\'x\' name=y>', '<meta/content=x>', '<meta content="x"/>', '<meta content="x" />', '<meta content=x/>',
    '<meta content="a>b">', '<meta content="a"b">', '<meta "content"="x">', '<meta content="x"', '<meta content="x',
    '<!-- <meta content="c"> --><meta content="real">', '<script><meta content="s"></script><meta content="after">',
    '<script><meta content="s">', '<title><meta content="t"></title>', '<title><meta content="t">',
    '<textarea><meta content="ta"></textarea><meta content="z">', '<xmp><meta content="x"></xmp >',
    '<plaintext><meta content="p">', '<style><meta content="s"></STYLE><meta content="up">',
    '<meta content="one"><meta content="two">', '<meta content="one"><meta name="x">', '<metax content="no">',
    '<meta\tcontent="tab">', '<meta\ncontent="nl">', '<meta\x0bcontent="vt">', '<meta content="&lt;b&gt;">',
    '<meta content="&#1;x">', '<meta content="&#x80;">', '<meta content="a&b">', '<meta content="&notit;">',
    '<!doctype html><meta content="d">', '<!DOCTYPE<meta content="d2">', '<![CDATA[<meta content="cd">]]><meta content="e">',
    '<![if x]><meta content="f">', '<?php <meta content="pi"> ?><meta content="g">', '</meta><meta content="h">',
    '</><meta content="i">', '</ x><meta content="j">', '<!x><meta content="k">', '<!--x--!><meta content="l">',
    '<!---><meta content="m">', '<!--><meta content="n">', '<!--', '<', 'a < b <meta content="o">',
    '<meta content="  multi\n\nline  ">', '<meta content="' + 'é' * 250 + '">', '<meta name="description" content="' + 'd' * 300,
    '<meta content=a=b>', '<meta content==x>', '<meta =x content=y>', "<meta a='1'b='2' content=c>",
    '<meta content="x"content="y">', '<meta content=\'"\'>', '<meta content="\'">', '<meta content=`x`>',
    '<meta content="x" / >', '<meta content="x"//>', '<meta / content="x">', '<meta content="x" /x>',
    '<İmeta content="x">', '<meta İcontent="x">', '<meta CONTENT="K">', '<meta content="x" <b>',
    '<meta content="&amp;amp;">', '<meta content="&nbsp;&#160;&#8203;">', '<scriPt>x</script ><meta content="q">',
    '<script>x</script<meta content="r">', '<script>x</scriptx><meta content="s"></script><meta content="t">',
]
ATTRS = [' content="v"', " content='v'", ' content=v', ' content', ' content= ', ' CONTENT="V"', ' name="og:site_name"',
         ' property=og:description', ' x="y"', ' content="a&amp;b"', ' content="&#x41;"', '/', ' /', '\t', '\n',
         " content='", ' content="', ' =', '"', "'", ' content=">"']
TAGS = ['<meta', '<META', '<Meta', '<script>', '</script>', '<title>', '</title>', '<!--', '-->', '<!doctype x>', '<?',
        '?>', '<b>', '</b>', '<', '>', ' ', 'text', '&amp;', '<plaintext>', '<br/>']


def random_html():
    out = []
    for _ in range(rng.randint(1, 6)):
        if rng.random() < 0.5:
            out.append('<meta' if rng.random() < 0.8 else '<META')
            out.extend(rng.choice(ATTRS) for _ in range(rng.randint(0, 4)))
            if rng.random() < 0.85:
                out.append(rng.choice(['>', '/>', ' >', '']))
        else:
            out.append(rng.choice(TAGS))
    text = ''.join(out)
    if rng.random() < 0.2:
        text = text.encode()[:rng.randint(0, len(text.encode()))].decode('utf-8', 'ignore')
    return text


for value in META:
    add('meta_content', [value], lambda value=value: wi._meta_content(value))
for _ in range(6000):
    value = random_html()
    add('meta_content', [value], lambda value=value: wi._meta_content(value))

# --- clean_text, site_title, page_identity ----------------------------------
CLEAN = ['', '  a  ', '<b>bold</b> &amp; x', 'a\u3000b\u2028c\x85d', 'x' * 250, '<a href="x>y">t</a>', '&#1;&#x7f;z',
         '<<b>>', 'a<b', '&notit; &copy &copyx', 'é' * 201]
for value in CLEAN:
    add('clean_text', [value, 200], lambda value=value: wi.clean_text(value))
    add('clean_text', [value, 5], lambda value=value: wi.clean_text(value, 5))
for _ in range(1500):
    value = random_entity_text() + random_html()
    add('clean_text', [value, 200], lambda value=value: wi.clean_text(value))

for site in ['<meta property="og:site_name" content="' + 'S' * 190 + '">',
             '<meta property="og:site_name" content="' + 'S' * 210,
             '<meta property="og:site_name" content="Trunc', '<meta property="og:site_name" content="A &amp; B">']:
    for description in ['<meta name="description" content="' + 'd' * 300, '<meta name="description" content="Short">', None]:
        markers = {'title': 'Fallback Title | Home', 'site': site}
        if description is not None:
            markers['description'] = description
        result = {'status': 200, 'markers': markers, 'final_url': 'https://Final.Example:8443/p?q'}
        add('page_identity', [result], lambda result=result: wi.page_identity(result, 'example.com', 'now'))
for final_url in ['http://[::1', 'http://[v1.x]/', 'https://user@[fe80::1%eth0]:9/x', '//no-scheme/x', 'https://ＡＢ.example/',
                  'https://a\uff0fb/', 'http://Ex\u0130.com/', 'https://h:99999/']:
    result = {'status': 204, 'markers': {'title': 'T'}, 'final_url': final_url}
    add('page_identity', [result], lambda result=result: wi.page_identity(result, 'example.com', 'now'))

# --- URLs -------------------------------------------------------------------
URLS = ['http://h/a', 'http://h:0/', 'http://h:65535/', 'http://h:65536/', 'http://h:+1/', 'http://h:1_0/',
        'http://h:١/', 'http://h:/', 'http://h:80:80/', "http://h:a'b/", 'http://[::1]:8/', 'http://[::1]x/',
        'http://x[::1]/', 'http://[::1/', 'http://::1]/', 'http://[1.2.3.4]/', 'http://[v1.a]/', 'http://[v1.]/',
        'http://[vz.a]/', 'http://[fe80::1%eth0]/', 'http://[fe80::1%]/', 'http://[fe80::1%a%b]/', 'http://[::ffff:1.2.3.4]/',
        'http://[::1.2.3.04]/', 'http://[1:2:3:4:5:6:7:8:9]/', 'http://u:p@h/', 'http://u@[::1]/', 'http://a\uff0fb/',
        'http://a\uff1fb/', 'http://a\u2100b/', 'http://a\ufe55b/', 'http://a\u00e9b/', 'http://ａ.com/', 'HTTP://H/',
        'https://H:443/api/x', 'https://config.test/api/x', 'https://config.test:443/api/x', 'https://config.test:444/api/x',
        'https://config.test/apix', 'https://CONFIG.test/api/', 'https://config.test/cf/api/x', 'http://config.test/api/x',
        ' https://config.test/api/x', '\x00http://h/', 'http://h/\x7f', 'http://h\\x/', 'http://h x/', 'http://h/ x',
        'ftp://h/', 'http:///x', 'http://:80/', 'http://h#frag', 'http://h?q', '', 'h/x', '//h/x', 'https://github.com/a/b',
        'https://raw.githubusercontent.com/a/b', 'https://GITHUB.COM/a', 'https://github.com:99999/a',
        'https://mirror.test/https://github.com/a/b', 'https://mirror.test/https://raw.githubusercontent.com/x',
        'https://mirror.test/http://github.com/a', 'https://[github.com]/a']
CONFIGS = [{'system_config': {}}, {'system_config': {'server_domain': 'https://config.test'}},
           {'system_config': {'server_domain': 'https://config.test/cf/'}},
           {'system_config': {'server_domain': 'https://config.test:99999'}},
           {'system_config': {'server_domain': 'http://[bad'}},
           {'system_config': {'github_proxy_domain': 'mirror.test'}},
           {'system_config': {'github_proxy_domain': 'https://mirror.test/', 'server_domain': 'https://config.test'}},
           {'system_config': {'rule_fetch_proxy': 'http://u:p@proxy.test:8080'}},
           {'system_config': {'rule_fetch_proxy': 'http://proxy.test/path'}}]
for url in URLS:
    add('validate_rule_url', [url], lambda url=url: list(rf.validate_rule_url(url)))
    add('is_github_url', [url], lambda url=url: rf.is_github_url(url))
    add('urlsplit_error', [url], lambda url=url: list(urlsplit(url)))
    for config in CONFIGS:
        add('is_internal_rule_url', [url, config], lambda url=url, config=config: rf.is_internal_rule_url(url, config))
        add('github_download_candidates', [url, config],
            lambda url=url, config=config: rf.github_download_candidates(url, config))
        add('rule_request_options', [url, config], lambda url=url, config=config: rf.rule_request_options(url, config))
for base in ['http://h/a/b', 'http://h/a/b?q#f', 'https://h', 'http://[::1]/x/']:
    for ref in ['//other/x', '/abs', 'rel', '../up', '../../../x', '?q2', '#f2', 'http://[bad', 'https://x\uff0f/',
                'g;x?y#s', '.', '..', './', 'HTTP://h2/', 'ftp://x/', ' sp ace', '%zz', 'é']:
        add('urljoin', [base, ref], lambda base=base, ref=ref: urljoin(base, ref))

# netloc characters urlsplit refuses under NFKC (any code point).
forbidden = []
for code in range(0x80, 0x110000):
    c = chr(code)
    if 0xD800 <= code <= 0xDFFF:
        continue
    n = unicodedata.normalize('NFKC', c)
    if n != c and any(x in n for x in '/?#@:'):
        forbidden.append(code)
rows.append({'fn': 'nfkc_forbidden', 'args': [], 'ok': forbidden})

# --- ruleset content: encodings, framing, sizes ------------------------------
TEXTS = ['\ufeffexample.com\n', '\ufeff\ufeffexample.com\n', 'a.com\r\nb.com\r\n', 'a.com\rb.com', 'a.com\n\rb.com',
         'x' * 300 + '\n', 'DOMAIN,' + 'x' * 250, '\u2028a.com', 'a.com\x85b.com', 'a.com\x00', '\ufeff# c\n+.b',
         '\ufeffpayload:\n  - a.com\n', ' \ufeffa.com', 'a.com\u3000', '\u200ba.com']
for text in TEXTS:
    for behavior in ['domain', 'classical', 'ipcidr']:
        for fmt in ['text', 'yaml', None]:
            rule = {'name': 'r', 'behavior': behavior, 'format': fmt} if fmt else {'name': 'r', 'behavior': behavior}
            add('validate_remote', [text, rule], lambda text=text, rule=rule: rc.validate_remote_rule_content(text, rule))
            add('inspect', [text, behavior, fmt], lambda text=text, behavior=behavior, fmt=fmt:
                rc.inspect_domain_content(text, behavior, fmt))
            add('normalize_ruleset', [text, behavior, fmt], lambda text=text, behavior=behavior, fmt=fmt:
                list(rc.normalize_mihomo_ruleset(text, behavior, fmt)))
BYTES = [b'\xef\xbb\xbfexample.com\n', b'\xff\xfee\x00x\x00', b'\xfe\xff\x00e', b'a.com\n\xc3', b'a.com\n\xc3\x28',
         b'a.com\r\n\xe2\x82', b'a.com\r\xed\xa0\x80', b'\xf4\x90\x80\x80', b'\x80', b'line1\x1cline2\xc0\xaf',
         b'a\xe2\x80\xa8b\n\xff', b'a.com\n' * 1000 + b'\xfe', b'payload:\n  - a.com\n  - \xe9\n', b'\x00\x01\x02',
         ('DOMAIN,' + 'é' * 300).encode()]
for data in BYTES:
    for behavior in ['domain', 'classical']:
        for fmt in ['text', 'yaml']:
            rule = {'name': 'b', 'behavior': behavior, 'format': fmt}
            add('validate_remote_bytes', [base64.b64encode(data).decode(), rule],
                lambda data=data, rule=rule: rc.validate_remote_rule_content(data, rule))


def mrs(behavior=0, count=1, extra=0, body=b'body', magic=b'MRS\x01', **kwargs):
    return zstandard.ZstdCompressor(**kwargs).compress(magic + bytes([behavior]) + struct.pack('>qq', count, extra) + body)


good = mrs()
skippable = struct.pack('<II', 0x184D2A50, 3) + b'abc'
chunked = zstandard.ZstdCompressor(write_content_size=False)
stream = chunked.compressobj()
streamed = stream.compress(b'MRS\x01\x00' + struct.pack('>qq', 1, 0) + b'B' * 5000) + stream.flush()
MRS = [good, good + skippable, skippable + good, skippable, good + skippable[:5], good[:10] + good[10:], good + b'\x28',
       good + b'\x28\xb5', good + b'\x28\xb5\x2f', good + b'\x50\x2a', good + b'\x00\x00\x00\x00',
       mrs(body=b'z' * 200000), mrs(body=b'z' * 131051), mrs(body=b'z' * 131052), mrs(body=b'z' * 300000, write_content_size=False),
       mrs(write_checksum=True), mrs(write_checksum=True)[:-1] + b'\x00', streamed, streamed[:-3],
       zstandard.ZstdCompressor().compress(b'MRS'), zstandard.ZstdCompressor().compress(b'MRS\x01\x00') +
       zstandard.ZstdCompressor().compress(struct.pack('>qq', 1, 0) + b'x'),
       zstandard.ZstdCompressor().compress(b'') + good, mrs(count=0, extra=0, body=b'x'), mrs(count=2**63 - 1),
       mrs(extra=2**63 - 1), mrs(extra=4, body=b'abcd'), mrs(extra=3, body=b'abcd'), mrs(behavior=1), mrs(behavior=255),
       zstandard.ZstdCompressor(level=19).compress(b'MRS\x01\x01' + struct.pack('>qq', 5, 0) + bytes(range(256)) * 600)]
for _ in range(200):
    data = bytearray(rng.choice(MRS))
    for _ in range(rng.randint(1, 3)):
        if data:
            data[rng.randrange(len(data))] = rng.randrange(256)
    MRS.append(bytes(data))
for data in MRS:
    for behavior in ['domain', 'ipcidr']:
        rule = {'name': 'm', 'behavior': behavior, 'format': 'mrs'}
        add('validate_remote_bytes', [base64.b64encode(data).decode(), rule],
            lambda data=data, rule=rule: rc.validate_remote_rule_content(data, rule))

# --- payload editing and YAML layouts with keys jsonx cannot hold ------------
YAMLS = ['payload: [a.com]\n1: x\n', '{payload: [a.com], 1: x}', '{1: x, payload: [a.com, b.com]}',
         '{payload: [a.com], true: x, ~: y}', '{payload: [a.com], 1.5: x}', '{payload: [a.com], 2001-01-01: d}',
         '{payload: [a.com], meta: {1: a, 2: b}}', '{payload: [a.com], 1: x, 1: y}', '{payload: [a.com], 1: x, "1": y}',
         '{payload: [a.com], ? [1, 2] : x}', '{payload: [a.com], !!binary aGk=: x}', '{payload: [a.com], other: !!set {a, b}}',
         '{payload: [a.com], other: !!omap [a: 1]}', '{payload: [a.com], other: !!binary aGk=}',
         '{payload: [a.com], other: 2001-01-01}', '{payload: [a.com], <<: {m: 1}}', '{payload: [a.com], <<: {1: 2}}',
         'payload: x\npayload: [a.com]\n', 'payload: [a.com]\npayload: x\n', '<<: {payload: [a.com]}\n',
         '1: x\npayload:\n  - a.com\n  - x.com\n', 'payload: [a.com, x.com]\nyes: 1\n', 'payload:\n  - a.com\n2: [x.com]\n',
         'payload: [!!bool maybe]\n', 'payload: [!!int ""]\n', 'payload: [a.com, !!float x]\n']
for text in YAMLS:
    add('read_payload', [text], lambda text=text: rc.read_rule_payload(text)[0])
    add('append', [text], lambda text=text: rc.append_rule_lines(text, ['+.new.test', 'x"y'], rc.read_rule_payload(text)[1]))
    add('remove', [text], lambda text=text: list(rc.remove_rule_payload(text, lambda line: 'x' in line or line == 'example.com')))
    add('normalize_yaml', [text], lambda text=text: list(rc.normalize_mihomo_yaml(text)))
    add('inspect', [text, 'domain', 'yaml'], lambda text=text: rc.inspect_domain_content(text, 'domain', 'yaml'))
    add('validate_remote', [text, {'name': 'y', 'behavior': 'domain', 'format': 'yaml'}],
        lambda text=text: rc.validate_remote_rule_content(text, {'name': 'y', 'behavior': 'domain', 'format': 'yaml'}))

out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'review_c14.jsonl.gz')
with open(out, 'wb') as raw:
    with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
        fh.write(('\n'.join(json.dumps(r, ensure_ascii=False) for r in rows) + '\n').encode())
counts = {}
for r in rows:
    counts[r['fn']] = counts.get(r['fn'], 0) + 1
print(len(rows), 'rows', json.dumps(counts, sort_keys=True))
