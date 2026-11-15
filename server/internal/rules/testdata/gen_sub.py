"""Reference cases for PyRegex.Sub / SubTemplate / Match (CPython 3.11 re).

Run: PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rules/testdata/gen_sub.py
"""
import gzip, io, json, os, random, re

PATTERNS = [
    r'x*', r'a|', r'(a)|b', r'(?P<n>\d+)', r'(\w+)@(\w+)', r'^', r'$', r'\b', r'\B', r'(?<=a)b', r'a(?=b)',
    r'(a)(b)?', r'(?i)A', r'\s+', r'(?:ab)+?', r'.', r'(?s).', r'(a*)*?', r'(?m)^\w', r'x*?', r'(|a)+',
    r'(?P<w>[^|:]+)\s*$', r'(?i)(?:\s*::\s*|\s+[|\-–—·]\s+)(?:用户登录|登录|首页|home|powered\s+by\s+[^|:]+)\s*$',
    r'<[^>]*>', r'\$(\d+)', r'(a)(?P<x>b)(c)', r'(.)\1', r'[一-鿿]+', r'(?<!\d)\d', r'\Z', r'\A', r'',
]
TEXTS = ['', 'a', 'abxd', 'aab ab', 'foo@bar x@y', 'a\nb\n', 'HHCLUB :: 登录 - Powered by NexusPHP', '12 345',
         'abcabc', 'aa bb', '<b>x</b>', 'node-$1-$2', 'Login - GitHub', '中文abc中', '  ', 'xx', 'aXa']
TEMPLATES = ['-', r'\1', r'\g<0>', r'\g<1>', r'\g<n>', r'\g<x>', r'[\2]', r'\0', r'\012', r'\08', r'\400', r'\q',
             r'\n\t\\', r'\$', r'\g<', r'\g<>', r'\g<1', r'\g<a b>', r'\g<-1>', r'\g<٣>', r'\g< 1 >', r'\11',
             r'\g<99999999999>', '\\', r'\g<unknown>', r'$1', r'x\g<0>y', r'\7', r'\g<1_0>', 'é\\1']

def err(e):
    return {'class': type(e).__name__, 'text': str(e)}

def main():
    rows = []
    for p in PATTERNS:
        try:
            compiled = re.compile(p)
        except re.error:
            continue
        for t in TEXTS:
            for count in (0, 1):
                row = {'p': p, 't': t, 'count': count}
                try:
                    row['fn'] = compiled.sub(lambda m: '<' + m.group(0) + '|' + ((m.group(1) or '') if compiled.groups and m.group(1) is not None else '-') + '>', t, count)
                except Exception as e:
                    row['fn_err'] = err(e)
                rows.append(row)
            m = compiled.search(t)
            rows.append({'p': p, 't': t, 'search': None if m is None else [list(m.span(i)) for i in range(compiled.groups + 1)]})
        for tpl in TEMPLATES:
            for t in TEXTS[:8]:
                row = {'p': p, 't': t, 'tpl': tpl, 'count': 0}
                try:
                    row['out'] = compiled.sub(tpl, t)
                except Exception as e:
                    row['err'] = err(e)
                rows.append(row)
    out = io.BytesIO()
    with gzip.GzipFile(fileobj=out, mode='wb', mtime=0) as f:
        for row in rows:
            f.write(json.dumps(row, ensure_ascii=False).encode() + b'\n')
    with open(os.path.join(os.path.dirname(__file__), 'sub_reference.jsonl.gz'), 'wb') as h:
        h.write(out.getvalue())
    print(len(rows), 'rows')

main()
