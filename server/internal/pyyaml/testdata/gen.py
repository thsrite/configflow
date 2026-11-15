"""Reference corpus for internal/pyyaml: PyYAML 6.0.1 pure-Python
yaml.scan / yaml.compose results (tokens, nodes, marks, error text).

Run: PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/pyyaml/testdata/gen.py
Deterministic (seeded); writes corpus.jsonl.gz with gzip mtime 0.
"""
import gzip, io, json, os, random, sys
import yaml

sys.setrecursionlimit(100000)

FRAGMENTS = [
    'payload:', 'payload: ', '"payload":', "'payload':", 'other: keep', 'a: &x v', '*x', '&a ', '!!str ', '! ',
    '!foo ', '!<tag:x> ', '- ', '-', '  ', '\t', '#c', ' # tail', '\n', '\r\n', '\r', '\x85', '\u2028', '[', ']',
    '{', '}', ',', ':', '? ', '"q"', "'s'", '"a\\nb"', '"\\x41\\u00e9\\U0001F600"', '"\\q"', "'it''s'", '|',
    '>', '|-', '>+', '|2', '+.example.com', 'DOMAIN,x.ai', '192.0.2.1', '123', 'true', '~', '', '---', '...',
    '%YAML 1.1', '%TAG !e! tag:e,2000:', '中文', '\ufeff', 'é', '@', '`', '%', 'a:b', 'http://x/y', '"unterminated',
    "'unterminated", '\x07', '\u00a0', 'key: [a, b, ]', '{a: 1, b: [x, y]}', '  - a\n  - b\n', 'x' * 40,
]

def random_doc(rng):
    n = rng.randint(1, 14)
    return ''.join(rng.choice(FRAGMENTS) for _ in range(n))

FIXED = [
    '', '\n', '\ufeff', 'payload: []\n', 'payload:\n  - a.com\n  - b.com # c\n', 'payload: ["a", b, ]\n',
    '{"payload":["x"],"other":"y"}', '\ufeff{"payload":["x"]}\n', '--- {a: 1}\n', '--- a\n--- b\n',
    'payload: ["+.example.com",', 'payload: true', 'payload: [true]', 'payload: []\npayload: []',
    'a: &rules ["+.example.com"]\npayload: *rules', 'payload: [&rule "+.example.com", *rule]',
    'payload:\n  - |\n    example.com\n', 'payload: ["tmdb.\norg"]', '{payload: ["tmdb.org"], other: 1, other: 2}',
    'payload:\n  - !!str &host DOMAIN-SUFFIX,tmdb.org\nother: *host\n', 'payload:\n  - &host !!str v\n',
    'a: b: c', 'a:\n- b\n c', '- a\nb: c', '[a, b', '{a: 1', '"a', "'a", 'a: "\\z"', 'a: !<x', '&', '*',
    '!!', '!e!x y', '%YAML 2.0\n--- a', '%YAML 1.1\n%YAML 1.1\n--- a', '%TAG !e! x\n%TAG !e! y\n--- a',
    'a: *undefined', 'a: &x 1\nb: &x 2', '? a\n: b', '[? a : b]', '{? a}', 'a: |0\n x', 'a: |+\n x\n\n',
    'a: >\n x\n y\n\n z\n', '"\\U00110000"', '"\\uD800"', 'a\tb', '\t- a', 'a: \x01', 'x: %bad', '- - - a',
    '[[[[]]]]', 'a: !!binary aGVsbG8=', 'key: value # 中文注释\n' * 3, ('x' * 200) + ': y', 'a:\n  b:\n    c: d\n  e: f\n',
    'payload: ["DOMAIN-REGEX,(?x)^tmdb # (", "DOMAIN,literal#value"] # note\n', 'a: [b]: c', 'a: {b: c}: d',
    '"payload": [\n  "+.tmdb.org", # suffix\n  "192.0.2.0/24",\n]\nother: keep\n', '!tag%41 x', '!t%e4%b8 x', '!t%ff x',
    '"' + 'y' * 100 + '\n' + 'z' * 100, ('a' * 30 + ' ') * 5 + ':\n',
]

def mark(m):
    return [m.index, m.line, m.column]

def token(t):
    out = {'id': t.id, 's': mark(t.start_mark), 'e': mark(t.end_mark)}
    for attr in ('value', 'plain', 'style'):
        if hasattr(t, attr):
            v = getattr(t, attr)
            if isinstance(v, tuple):
                v = list(v)
            out[attr] = v
    if hasattr(t, 'name'):
        out['name'] = t.name
    return out

def node(n, seen):
    if id(n) in seen:
        return {'ref': seen[id(n)]}
    seen[id(n)] = len(seen)
    out = {'kind': type(n).__name__, 'tag': n.tag, 's': mark(n.start_mark), 'e': mark(n.end_mark)}
    if isinstance(n, yaml.ScalarNode):
        out['value'] = n.value
        out['style'] = n.style
    else:
        out['flow'] = bool(n.flow_style)
        if isinstance(n, yaml.SequenceNode):
            out['items'] = [node(x, seen) for x in n.value]
        else:
            out['pairs'] = [[node(k, seen), node(v, seen)] for k, v in n.value]
    return out

def error(e):
    out = {'class': type(e).__name__, 'text': str(e)}
    pm = getattr(e, 'problem_mark', None)
    out['problem'] = getattr(e, 'problem', None)
    out['line'] = pm.line if pm is not None else None
    return out

def surrogate_free(s):
    try:
        s.encode('utf-8')
        return True
    except UnicodeEncodeError:
        return False

def case(text):
    row = {'text': text}
    try:
        row['tokens'] = [token(t) for t in yaml.scan(text, Loader=yaml.SafeLoader)]
    except (yaml.YAMLError, ValueError) as e:
        row['scan_error'] = error(e)
    try:
        root = yaml.compose(text, Loader=yaml.SafeLoader)
        row['node'] = None if root is None else node(root, {})
    except (yaml.YAMLError, ValueError) as e:
        row['compose_error'] = error(e)
    return row

def main():
    rng = random.Random(14)
    texts = list(FIXED) + [random_doc(rng) for _ in range(6000)]
    out = io.BytesIO()
    with gzip.GzipFile(fileobj=out, mode='wb', mtime=0) as f:
        count = 0
        for text in texts:
            row = case(text)
            line = json.dumps(row, ensure_ascii=False)
            if not surrogate_free(line):
                continue
            f.write(line.encode() + b'\n')
            count += 1
    path = os.path.join(os.path.dirname(__file__), 'corpus.jsonl.gz')
    with open(path, 'wb') as handle:
        handle.write(out.getvalue())
    print(count, 'rows')

main()
