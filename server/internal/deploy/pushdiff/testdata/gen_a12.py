"""Synthetic Python oracle. Run from the repo root on an rt test server only.

Captures public build_push_diff and config/list boundaries exercised by the
upstream tests, plus bundle pairs from the A11-3 oracle and targeted inputs.
All private address examples are rewritten before computing oracle outputs.
"""
import base64
import copy
import gzip
import hashlib
import inspect
import json
from pathlib import Path
import re
import pytest
import yaml
from backend.agents import push_diff as p
from backend.agents.deployment_bundle import DeploymentBundle, deployment_content_digests
from backend import test_agent_push_diff as tests

root = Path(__file__).parent
rows = []
case = ''
recording = True
original_config, original_build = p.diff_config, p.build_push_diff
original_nodes, original_rules = p._node_change, p._rule_change


def clean(data):
    if data is None:
        return None
    return re.sub(rb'192\.168\.\d+\.(\d+)', rb'192.0.2.\1', data).replace(b'10.0.0.', b'198.51.100.')


def bytestr(value):
    return None if value is None else base64.b64encode(value).decode()


def record_config(old, new, **kw):
    global recording
    if recording:
        a, b = clean(old), clean(new)
        rows.append(dict(kind='config', name=case, old=bytestr(a), new=bytestr(b),
                         old_name=kw.get('old_name', 'config.yaml'), new_name=kw.get('new_name', 'config.yaml'),
                         expected=original_config(a, b, **kw)))
    return original_config(old, new, **kw)


def snapshot(bundle):
    if bundle is None:
        return None
    manifest = copy.deepcopy(bundle.manifest)
    files = {name: clean(data) for name, data in bundle.files().items()}
    for f in manifest['files']:
        f['size'] = len(files[f['path']])
        f['sha256'] = hashlib.sha256(files[f['path']]).hexdigest()
    comparison = copy.deepcopy(bundle.comparison_config)
    return dict(manifest=manifest, files={k: bytestr(v) for k, v in files.items()}, comparison=comparison)


def from_snapshot(s):
    if s is None:
        return None
    files = {f['path']: (base64.b64decode(s['files'][f['path']]), f['role']) for f in s['manifest']['files']}
    return DeploymentBundle.from_files(s['manifest'], files, '', s.get('comparison'))


def add_build(agent, candidate, receipt, name):
    global recording
    a, b = snapshot(receipt['bundle']) if receipt else None, snapshot(candidate)
    normalized = copy.deepcopy(agent)
    if a is not None:
        mapping = {x['sha256']: y['sha256'] for x, y in zip(receipt['manifest']['files'], a['manifest']['files'])}
        files = normalized.get('applied_content', {}).get('files')
        if files is not None:
            normalized['applied_content']['files'] = {k: mapping.get(v, v) for k, v in files.items()}
    comp = copy.deepcopy(receipt.get('comparison_config')) if receipt else None
    r = dict(manifest=a['manifest'], bundle=from_snapshot(a), comparison_config=comp) if a else None
    old_recording = recording
    recording = False
    try:
        lists = {}
        expected = original_build(normalized, from_snapshot(b), None, receipt=r, lists=lists)
        pages = {}
        for path, entries in lists.items():
            pages[path] = {change: [entries.page(change, offset, 50, p.search_query(query))
                                   for offset, query in [(0, ''), (20, ''), (0, 'example'), (0, '香港'), (0, 'password')]]
                           for change in entries.groups}
        rows.append(dict(kind='build', name=name, agent=normalized, before=a, after=b, comparison=comp,
                         expected=expected, pages=pages, detail_budget=p.MAX_DETAIL_BYTES))
    finally:
        recording = old_recording


def record_build(agent, bundle, repository, *, receipt=None, lists=None):
    global recording
    if recording:
        add_build(agent, bundle, receipt, case)
    old_recording = recording
    recording = False
    try:
        return original_build(agent, bundle, repository, receipt=receipt, lists=lists)
    finally:
        recording = old_recording


def record_list(kind, original, old, new, budget, keep=None):
    if recording:
        a, b = clean(old), clean(new)
        if a is None or b is None or b'\\ud800' not in a+b:
            summary, details = original(a, b, p._DetailBudget())
            rows.append(dict(kind=kind, name=case, old=bytestr(a), new=bytestr(b), expected=[summary, details]))
    return original(old, new, budget, keep)


p.diff_config = tests.diff_config = record_config
tests.build_push_diff = record_build
p._node_change = lambda *args, **kw: record_list('nodes', original_nodes, *args, **kw)
p._rule_change = lambda *args, **kw: record_list('rules', original_rules, *args, **kw)
# Tests requiring checker/executor fixtures belong to A13, not the pure A12 package.
executed = []
for name, test in vars(tests).copy().items():
    if not name.startswith('test_'):
        continue
    params = inspect.signature(test).parameters
    if 'setup' in params or name == 'test_scalar_display':
        continue
    case = name
    with pytest.MonkeyPatch.context() as monkeypatch:
        test(**({'monkeypatch': monkeypatch} if 'monkeypatch' in params else {}))
    executed.append(name)
for value, shown in [('plain', 'plain'), (7, '7'), (1.5, '1.5'), (True, 'true'), (None, 'null'), ({'a': [1]}, '{"a": [1]}')]:
    case = 'test_scalar_display'
    tests.test_scalar_display(value, shown)
executed.append(case)

# A11-3 successful bundle snapshots, paired within service types. Bytes are
# rehashed because that corpus replaces its loopback port with @RULE_SERVER@.
source = json.loads((root.parents[1] / 'testdata/build.json').read_text())
by_service = {}
for row in source['rows']:
    if 'manifest' not in row:
        continue
    manifest = copy.deepcopy(row['manifest'])
    for f in manifest['files']:
        content = base64.b64decode(row['files'][f['path']])
        f['size'], f['sha256'] = len(content), hashlib.sha256(content).hexdigest()
    b = from_snapshot(dict(manifest=manifest, files=row['files']))
    prev = by_service.get(row['service'])
    if prev is not None:
        add_build({'service_type': row['service'], 'applied_content': {'files': deployment_content_digests(prev.manifest)}},
                  b, dict(manifest=prev.manifest, bundle=prev), 'a11-' + row['name'])
    by_service[row['service']] = b

# Keys, userinfo, opaque paths/query names, CJK and reformatting.
case = 'targeted-credentials-cjk'
record_config(b'hosts: {}\n', yaml.safe_dump({
    'hosts': {'token.example.com': '192.0.2.9'},
    'secret': 'controller-value', 'private_key': 'private-value',
    'authentication': ['admin:synthetic-password'],
    'url': 'https://user:synthetic-password@rules.example.com/Q2xhc2hTdWJzY3JpcHRpb25QYXRoVG9rZW4wMDg?api_key=opaque-value',
    'dialer': 'user:synthetic-password@192.0.2.8:1080',
    'unicode': '中文 日本語 한국어 😀',
}, allow_unicode=True, sort_keys=False).encode())

# A large paging corpus at the public Build boundary.
case = 'targeted-paging-100k'
old = tests.bundle({'mode': 'rule'}, rules={'rules/large': ''.join(f'+.old-{i}.example.com\n' for i in range(1000))})
new = tests.bundle({'mode': 'rule'}, rules={'rules/large': ''.join(f'+.new-{i}.example.com\n' for i in range(101000))})
add_build(tests.agent_for(old), new, tests.receipt_for(old), case)

payload = json.dumps(dict(executed=executed, rows=rows), ensure_ascii=True, separators=(',', ':')).encode()
with gzip.GzipFile(filename=str(root / 'python.json.gz'), mode='wb', mtime=0) as f:
    f.write(payload)
print(f'A12 Python: {len(executed)} tests / {len(rows)} boundary rows; {len(payload)} JSON bytes')
