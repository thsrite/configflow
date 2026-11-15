"""Differential corpus for internal/rulefetch (ruleset_content, website_identity,
remote_rule_sources, rule_fetch URL helpers). Synthetic values only.

Run: PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/gen.py
"""
import base64, gzip, io, json, os, random, struct, sys
import zstandard

from backend.utils import ruleset_content as rc
from backend.utils import website_identity as wi
from backend.utils import remote_rule_sources as rs
from backend.utils import rule_fetch as rf

sys.setrecursionlimit(5000)

LINES = ['example.com', '+.example.com', '.example.com', '*.example.com', '*', '+.*', 'foo..test', 'bad-.test',
         'DOMAIN,x.ai', 'DOMAIN-SUFFIX,x.ai', 'DOMAIN-KEYWORD,google', 'IP-CIDR,192.0.2.0/24', 'IP-CIDR,192.0.2.0/33',
         '192.0.2.1', '192.0.2.0/24', '2001:db8::1', '2001:db8::/32', '::ffff:192.0.2.1', 'fe80::1%eth0',
         '192.0.2.999', 'full:example.com', 'regexp:^a', 'MATCH', 'RULE-SET,x', 'DOMAIN,', 'DOMAIN,a,,b',
         'AND,((DOMAIN,a.com),(NETWORK,tcp))', 'domaın,x.com', 'xn--fsqu00a.xn--0zwm56d', 'a' * 64 + '.example',
         '# comment', '// note', '', '  ', '\t+.tab.test ', 'https://example.com', 'example.com # inline', '中文.com',
         'PROCESS-NAME,app.exe', 'NETWORK,tcp', '+.192.0.2.1', '_srv.example.com', 'DOMAIN-REGEX,a&b']
SEPS = ['\n', '\r\n', '\r', '\u2028', '\x85', '\v']

YAMLS = [
    'payload: []\n', 'payload:\n  - a.com\n  - b.com # c\n', 'payload: ["a.com", b.com, ]\n',
    '{"payload":["x.com"],"other":"y"}', '\ufeff{"payload":["x.com"]}\n', '--- {other: keep, payload: ["+.a.org"]}\n',
    'payload: ["+.example.com",', 'payload: true', 'payload: [true]', 'payload: []\npayload: []',
    'a: &rules ["+.example.com"]\npayload: *rules', 'payload: [&rule "+.example.com", *rule]',
    'payload:\n  - |\n    example.com\n', 'payload: ["tmdb.\norg"]', '{payload: ["tmdb.org"], other: 1, other: 2}',
    '{payload: ["tmdb.org"], metadata: {key: 1, key: 2}}', 'payload:\n  - !!str &host DOMAIN-SUFFIX,tmdb.org\nother: *host\n',
    '# header\n{other: keep, # metadata\npayload: ["+.tmdb.org", "192.0.2.0/24"]} # tail\n',
    '"payload": [\n  "+.tmdb.org", # suffix\n  "192.0.2.0/24",\n]\nother: keep\n',
    'payload: ["DOMAIN-REGEX,(?x)^tmdb # (", "DOMAIN,literal#value"] # note\n', 'payload: example.com\n',
    'payload: [example.com\n', 'payload:\n  - 123\n', 'payload:\n  - [example.com]\n', 'example.com\n',
    "# keep\n'payload':\n  - '+.tmdb.org' # suffix\n", 'note: &note "a&b"\ncopy: *note\npayload: ["+.tmdb.org", "DOMAIN-REGEX,a&b"]\n',
    'payload:\n- x.com\n- y.com\nother: z\n', '\ufeff"payload":\r\n  - "+.tmdb.org"\r\n', 'payload: [x.com, "\\x01"]\n',
    'a: "\\q"\npayload: []', 'payload:\n  - "a\\U0010FFFFb"\n', '{payload: [a.com], 1: x}', 'payload: [' + '[' * 30 + ']' * 30 + ']',
    'payload:\n  - a\x07\n', 'payload: [{a: b}]', 'payload: ["a.com"]\n---\nb: c\n', '%YAML 1.1\n--- {payload: [a.com]}',
]

def random_text(rng):
    n = rng.randint(0, 8)
    sep = rng.choice(SEPS)
    body = sep.join(rng.choice(LINES) for _ in range(n))
    if rng.random() < 0.3:
        body += sep
    if rng.random() < 0.1:
        body = '\ufeff' + body
    return body

def random_yaml(rng):
    items = [rng.choice(LINES) for _ in range(rng.randint(0, 6))]
    style = rng.randint(0, 3)
    if style == 0:
        return 'payload:\n' + ''.join('  - ' + json.dumps(i, ensure_ascii=False) + (' # c' if rng.random() < .2 else '') + '\n' for i in items)
    if style == 1:
        return 'payload: [' + ', '.join(json.dumps(i, ensure_ascii=False) for i in items) + (',' if rng.random() < .5 else '') + ']\n'
    if style == 2:
        return json.dumps({'other': 'keep', 'payload': items}, ensure_ascii=False)
    return '# h\npayload:\n' + ''.join('- ' + i + '\n' for i in items if i and not i.startswith(('#', '*', '&', '!', '|', '>', '%', '@', '`', '[', '{', '"', "'", '-', '?', ':', ',')) and ': ' not in i and ' #' not in i)

def err(e):
    return {'class': type(e).__name__, 'text': str(e), 'errors': getattr(e, 'errors', None) if isinstance(e, rc.RemoteRuleValidationError) else None}

def call(fn, *args, **kw):
    try:
        return {'ok': fn(*args, **kw)}
    except (rc.ProfileValidationError, ValueError, RecursionError, TypeError, AttributeError, IndexError, KeyError) as e:
        return {'err': err(e)}

def mrs(behavior=0, count=1, extra=0, body=b'body', magic=b'MRS\x01'):
    return zstandard.ZstdCompressor(write_checksum=True).compress(magic + bytes([behavior]) + struct.pack('>qq', count, extra) + body)

def b64(b):
    return base64.b64encode(b).decode()

def main():
    rng = random.Random(1214)
    rows = []
    texts = [random_text(rng) for _ in range(1500)]
    yamls = list(YAMLS) + [random_yaml(rng) for _ in range(800)]
    for content in texts + yamls:
        for behavior in ('domain', 'ipcidr', 'classical'):
            for fmt in (None, 'text', 'yaml'):
                rows.append({'fn': 'inspect', 'args': [content, behavior, fmt], **call(rc.inspect_domain_content, content, behavior, fmt)})
        rows.append({'fn': 'normalize_classical', 'args': [content], **call(lambda c: list(rc.normalize_classical_lines(c)), content)})
        rows.append({'fn': 'normalize_yaml', 'args': [content], **call(lambda c: list(rc.normalize_mihomo_yaml(c)), content)})
        for behavior, fmt in (('classical', 'yaml'), ('domain', 'text'), ('classical', 'text'), ('ipcidr', 'yaml')):
            rows.append({'fn': 'normalize_ruleset', 'args': [content, behavior, fmt], **call(lambda *a: list(rc.normalize_mihomo_ruleset(*a)), content, behavior, fmt)})
        for behavior in ('domain', 'ipcidr', 'classical'):
            for fmt in ('text', 'yaml'):
                rule = {'name': '测试', 'behavior': behavior, 'format': fmt, 'source_type': 'url'}
                rows.append({'fn': 'validate_remote', 'args': [content, rule], **call(rc.validate_remote_rule_content, content, rule)})
                data = content.encode()
                rows.append({'fn': 'validate_remote_bytes', 'args': [b64(data), rule], **call(rc.validate_remote_rule_content, data, rule)})
        rows.append({'fn': 'read_payload', 'args': [content], **call(lambda c: rc.read_rule_payload(c)[0], content)})
        def append(c):
            lines, payload = rc.read_rule_payload(c)
            return rc.append_rule_lines(c, ['+.new.test', 'x"y'], payload)
        rows.append({'fn': 'append', 'args': [content], **call(append, content)})
        rows.append({'fn': 'remove', 'args': [content], **call(lambda c: list(rc.remove_rule_payload(c, lambda line: 'x' in line or line == 'example.com')), content)})
        rows.append({'fn': 'validate_domain', 'args': [{'source_type': 'content', 'behavior': 'domain', 'content': content}], **call(rc.validate_domain_content, {'source_type': 'content', 'behavior': 'domain', 'content': content})})
    # binary / metadata cases
    for data in [b'example.com\n\xff', b'\xff', b'example.com\r\xff', 'example.com\u2028'.encode() + b'\xff', b'a\xe4\xb8', b'a\xe4\xb8A\n',
                 b'\xed\xa0\x80', b'\xf0\x9f\x98', b'ok\x00\n', b'\xc3\x28']:
        rows.append({'fn': 'validate_remote_bytes', 'args': [b64(data), {'name': 7, 'behavior': 'domain', 'format': 'text'}], **call(rc.validate_remote_rule_content, data, {'name': 7, 'behavior': 'domain', 'format': 'text'})})
    for data in [b'', b'example.com', mrs(), mrs()[:-1], mrs() + b'bad', mrs() + mrs(), mrs(magic=b'BAD!'), mrs(behavior=2), mrs(behavior=1),
                 mrs(count=-1), mrs(extra=-1), mrs(extra=1000), mrs(extra=4), mrs(body=b''), mrs(extra=2, body=b'xyz'), b'\x28\xb5\x2f\xfd',
                 zstandard.ZstdCompressor().compress(b'MRS\x01\x00' + struct.pack('>qq', 3, 0) + b'z' * 100000)]:
        for behavior in ('domain', 'ipcidr', 'classical'):
            rule = {'name': 'm', 'behavior': behavior, 'format': 'mrs'}
            rows.append({'fn': 'validate_remote_bytes', 'args': [b64(data), rule], **call(rc.validate_remote_rule_content, data, rule)})
    rows.append({'fn': 'validate_remote', 'args': ['text', {'behavior': 'domain', 'format': 'mrs'}], **call(rc.validate_remote_rule_content, 'text', {'behavior': 'domain', 'format': 'mrs'})})
    for rule in [{'behavior': 'domain-x'}, {'format': 'json'}, {'url': 'https://r.test/a.yaml'}, {'url': 'https://r.test/a.mrs'}, {'name': 'n', 'id': 'i'},
                 {'id': 3, 'behavior': 'classical', 'format': 'mrs'}, {}]:
        for content in ['payload: [example.com]', 'example.com\n' + 'x' * 300 + ',\n', 'DOMAIN,a\nDOMAIN,b\n']:
            rows.append({'fn': 'validate_remote', 'args': [content, rule], **call(rc.validate_remote_rule_content, content, rule)})
    for rule in [{'source_type': 'url', 'behavior': 'x'}, {'source_type': 'file'}, {'source_type': 'content', 'format': 'mrs'},
                 {'source_type': 'content', 'content': 5}, {'source_type': 'content', 'content': 'DOMAIN,x'}, {'source_type': 'content', 'behavior': 'classical', 'format': 'mrs'}]:
        rows.append({'fn': 'validate_domain', 'args': [rule], **call(rc.validate_domain_content, rule)})
    # website identity
    titles = ['HHCLUB :: 登录 - Powered by NexusPHP', 'Login - GitHub', '首页 - 某站 - Powered by Discuz!', 'Wi-Fi Home', '登录', '',
              '  Example &amp; docs  ', '<b>Bold</b> | Home', 'Access Denied', '404 Not Found', 'Errors are fine', 'home - x - sign in',
              'A\u3000B\n\tC', 'x' * 300, 'Sign In · GitLab', 'Café &eacute; &#233; &#x1F600; &bogus;', 'Home', 'a :: b :: 首页']
    metas = ["<meta content='Example &amp; Co' property='og:site_name'>", '<META NAME="description" CONTENT="Docs &amp; help">',
             '<meta property="og:site_name">', '<meta property="og:site_name" content>', '<meta content="a" content="b" name="description">',
             '<meta name=description content=bare>', '<meta property="og:site_name" content="  多  空格 ">', '']
    for t in titles:
        rows.append({'fn': 'site_title', 'args': [t], 'ok': wi.site_title(t)})
        rows.append({'fn': 'clean_text', 'args': [t], 'ok': wi.clean_text(t)})
    for host in ['api.openai.com', 'youtube.com', 'notopenai.com', 'openai.com.evil.test', 'x.nflxvideo.net', 'claude.ai', '']:
        rows.append({'fn': 'known_service', 'args': [host], 'ok': wi.known_service(host)})
    for result in [None, {'error': 'timeout'}, {'status': 403, 'markers': {'title': 'E'}}, {'status': 200, 'markers': {'title': 'E', 'challenge': 'cf-chl-'}},
                   {'status': 200, 'markers': {}}, {'status': 299.5, 'markers': {'title': 'T'}}, {'status': 0}, {'status': 200, 'markers': {'title': 'T'}, 'final_url': 'https://Docs.Example.com/p?token=s'},
                   {'status': 200, 'markers': {'title': 'T'}, 'final_url': 'http://[::1'}] + \
                  [{'status': 200, 'markers': {'title': t, 'site': m, 'description': m}} for t in titles[:6] for m in metas]:
        rows.append({'fn': 'page_identity', 'args': [result], 'ok': wi.page_identity(result, 'example.com', 'now')})
    # remote sources
    config = {'system_config': {'server_domain': 'https://cf.test/'}, 'rule_library': [
        {'id': 'a', 'url': 'https://r.test/a.txt', 'behavior': 'domain'}, {'id': 'b', 'url': '/api/rules/b', 'behavior': 'classical'},
        {'id': 'c', 'url': 'https://r.test/a.txt', 'behavior': 'ipcidr'}, {'id': 'd', 'source_type': 'content', 'url': 'https://r.test/d'},
        {'id': 'e', 'url': 'https://r.test/e.yaml'}],
        'rule_configs': [{'id': 'r1', 'itemType': 'ruleset', 'library_rule_id': 'e', 'url': 'https://r.test/e.yaml'},
                         {'id': 'r2', 'itemType': 'ruleset', 'url': 'https://r.test/own.list', 'behavior': 'domain'}],
        'rule_sets': [{'id': 's1', 'url': 'https://r.test/s', 'format': 'yaml'}]}
    for url in ['https://r.test/a.txt', 'https://cf.test/api/rules/b', '/api/rules/b', 'https://r.test/e.yaml', 'https://r.test/own.list', 'https://r.test/s', 'https://r.test/none', 'https://r.test/d']:
        for sid in [None, 'a', 'b', 'd', 'r1', 'r2', 's1', 'zz']:
            rows.append({'fn': 'resolve_source', 'args': [url, config, sid], **call(rs.resolve_remote_rule_source, url, config, sid)})
    # URL helpers
    urls = ['https://example.com', 'http://example.com:8080/a?b#c', 'ftp://x', 'https://', 'https://exa mple.com', 'https://a\\b@c',
            'https://[::1]:443/x', 'https://[::1', 'https://x:0', 'https://x:65536', 'https://x:abc', ' https://x', 'https://x\x7f',
            'https://user:pw@host/p', 'HTTPS://Host.Example/', 'https://ｅxample.com', 'https://x／y', 'http://x/', 'http://x/p', 'http://x?q', '']
    for u in urls:
        rows.append({'fn': 'validate_rule_url', 'args': [u], **call(lambda v: list(rf.validate_rule_url(v)), u)})
        rows.append({'fn': 'normalize_proxy', 'args': [u], **call(rf.normalize_rule_fetch_proxy, u)})
    rows.append({'fn': 'normalize_proxy', 'args': [5], **call(rf.normalize_rule_fetch_proxy, 5)})
    bases = ['http://a/b/c/d;p?q', 'https://h.test/api/rules?token=t', 'http://a']
    locs = ['g', './g', 'g/', '/g', '//g', '?y', 'g?y', '#s', 'g;x', '', '.', '..', '../g', '../../../g', '/./g', 'g/../h', 'https://o.test/x',
            'http:g', 'file:///etc/hosts', 'http://[::1', ';x', '..;x', 'a/./b/../c', '//[v1.x]/p', 'mailto:a@b']
    for b in bases:
        for l in locs:
            rows.append({'fn': 'urljoin', 'args': [b, l], **call(rf.urljoin, b, l)})
    out = io.BytesIO()
    with gzip.GzipFile(fileobj=out, mode='wb', mtime=0) as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False, default=lambda o: list(o) if isinstance(o, tuple) else str(o)).encode() + b'\n')
    with open(os.path.join(os.path.dirname(__file__), 'corpus.jsonl.gz'), 'wb') as h:
        h.write(out.getvalue())
    print(len(rows), 'rows')

main()
