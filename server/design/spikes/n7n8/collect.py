"""N7/N8 spike, step 1 (Python side). Reads a COPY of a data directory.

Collects every YAML text the server parses from stored data (string values
with a newline that PyYAML loads as a mapping/sequence, plus *.yaml/*.yml
files) and records PyYAML's safe_load result in a normalised form. Also
scans every stored JSON field for values encoding/json cannot read or would
change. Writes only into OUT (a private directory); prints counts only.
"""
import base64, datetime, glob, json, math, os, re, sqlite3, sys
import yaml

data, out = sys.argv[1], sys.argv[2]
os.makedirs(os.path.join(out, 'texts'), exist_ok=True)

def norm(v):
    if v is None: return None
    if isinstance(v, bool): return {'b': v}
    if isinstance(v, int): return {'i': str(v)}
    if isinstance(v, float):
        if math.isnan(v): return {'f': 'nan'}
        if math.isinf(v): return {'f': 'inf' if v > 0 else '-inf'}
        return {'f': '%.17g' % v}
    if isinstance(v, str): return {'s': v}
    if isinstance(v, bytes): return {'bin': base64.b64encode(v).decode()}
    if isinstance(v, (datetime.datetime, datetime.date)): return {'t': v.isoformat()}
    if isinstance(v, list): return [norm(x) for x in v]
    if isinstance(v, dict):
        items = [[norm(k), norm(x)] for k, x in v.items()]
        items.sort(key=lambda p: json.dumps(p[0], sort_keys=True))
        return {'map': items}
    if isinstance(v, (set, tuple)): return {'other': type(v).__name__}
    return {'other': type(v).__name__}

stats = {}
def bump(k, n=1): stats[k] = stats.get(k, 0) + n

# --- JSON fields -----------------------------------------------------------
con = sqlite3.connect('file:%s?mode=ro' % os.path.join(data, 'configflow.sqlite3'), uri=True)
rows = con.execute('SELECT scope, owner, field, value FROM fields').fetchall()
texts = []
def dup_hook(pairs):
    keys = [k for k, _ in pairs]
    if len(keys) != len(set(keys)): bump('json.duplicate_key_objects')
    return dict(pairs)
def walk(v, path):
    if isinstance(v, dict):
        for k, x in v.items(): walk(x, path + [k])
    elif isinstance(v, list):
        for x in v: walk(x, path)
    elif isinstance(v, bool): pass
    elif isinstance(v, int):
        if abs(v) >= 2**63: bump('json.int_beyond_int64')
        elif abs(v) > 2**53: bump('json.int_beyond_2^53')
    elif isinstance(v, float):
        if math.isnan(v) or math.isinf(v): bump('json.nan_or_inf')
        elif v == int(v): bump('json.integral_float')
        else: bump('json.float')
    elif isinstance(v, str):
        if re.search('[\ud800-\udfff]', v): bump('json.lone_surrogate')
        if '\n' in v:
            try:
                y = yaml.safe_load(v)
            except Exception:
                y = None
            if isinstance(y, (dict, list)):
                texts.append(('field', v))
for scope, owner, field, value in rows:
    bump('json.fields')
    bump('json.bytes', len(value))
    if re.search(r'(?<![\w"])(NaN|-?Infinity)(?![\w"])', value): bump('json.nan_token_candidates')
    v = json.loads(value, object_pairs_hook=dup_hook)
    walk(v, [scope, field])
for p in sorted(glob.glob(os.path.join(data, '**', '*.y*ml'), recursive=True)):
    if os.path.getsize(p) > 20 * 1024 * 1024: bump('yaml.files_skipped_large'); continue
    with open(p, encoding='utf-8', errors='surrogateescape', newline='') as f:
        texts.append(('file:' + os.path.relpath(p, data).split(os.sep)[0], f.read()))

# --- YAML texts --------------------------------------------------------------
with open(os.path.join(out, 'py.jsonl'), 'w') as res:
    for i, (origin, text) in enumerate(texts):
        name = '%05d' % i
        with open(os.path.join(out, 'texts', name), 'w', encoding='utf-8', errors='surrogateescape', newline='') as f:
            f.write(text)
        try:
            r = {'value': norm(yaml.safe_load(text))}
        except Exception as e:
            r = {'error': type(e).__name__}
        r['name'] = name
        r['origin'] = origin
        res.write(json.dumps(r) + '\n')
stats['yaml.texts'] = len(texts)
for k in sorted(stats): print('%-34s %s' % (k, stats[k]))
