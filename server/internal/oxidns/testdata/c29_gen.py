"""Capture actual C29 public inputs, then recompute them with real Python.

Run only through rt. Uses synthetic tests, isolated DATA_DIR and no device I/O.
"""
import copy
import gzip
import inspect
import json
import os
from pathlib import Path
import sys
import tempfile

private = tempfile.TemporaryDirectory(prefix='c29-python-')
os.environ['DATA_DIR'] = private.name
os.environ['PYTHONDONTWRITEBYTECODE'] = '1'
import pytest
import backend.common.oxidns_native_apply as native

original = native.apply_native_settings
signature = inspect.signature(original)
inputs = []
current = ''

def capture(*args, **kwargs):
    bound = signature.bind(*args, **kwargs)
    bound.apply_defaults()
    values = copy.deepcopy(bound.arguments)
    inputs.append((current, values))
    return original(*args, **kwargs)

class Recorder:
    def pytest_collection_modifyitems(self, items):
        for item in items:
            module = item.module
            for name, value in list(vars(module).items()):
                if value is original:
                    setattr(module, name, capture)
        native.apply_native_settings = capture

    def pytest_runtest_setup(self, item):
        global current
        current = item.nodeid

result = pytest.main(['-q', '-p', 'no:cacheprovider', '-k', 'not api',
    'backend/test_oxidns_native_apply.py', 'backend/test_oxidns_native_restore.py',
    'backend/test_oxidns_native_cleanup.py', 'backend/test_oxidns_hosts_cleanup.py'], plugins=[Recorder()])
if result:
    raise SystemExit(result)
# Pytest monkeypatches have been undone. Recompute through the original real
# implementation, independently of any test assertions or captured outputs.
def synthetic(value):
    if isinstance(value, str):
        for old, new in [('223.5.5.5', '192.0.2.53'), ('119.29.29.29', '192.0.2.54'),
                         ('1.1.1.1', '192.0.2.55'), ('8.8.8.8', '192.0.2.56')]:
            value = value.replace(old, new)
        return value
    if isinstance(value, list):
        return [synthetic(item) for item in value]
    if isinstance(value, dict):
        return {key: synthetic(item) for key, item in value.items()}
    return value

rows = []
for name, values in inputs:
    values = synthetic(values)
    before = copy.deepcopy(values)
    row = {'fn': 'apply_native_settings', 'test': name,
           'args': [before[key] for key in ('settings', 'native_config', 'config', 'base_url', 'renames', 'standard_add')]}
    try:
        row['out'] = original(**values)
    except Exception as exc:
        row['err'] = [type(exc).__name__, str(exc)]
        if hasattr(exc, 'references'):
            row['references'] = exc.references
    row['args_after'] = [values[key] for key in ('settings', 'native_config', 'config', 'base_url', 'renames', 'standard_add')]
    # Round-trip the actual Python JSON boundary before recording expectations.
    text = json.dumps(row, ensure_ascii=True)
    if any(value in text for value in ('223.5.5.5', '119.29.29.29', '1.1.1.1', '8.8.8.8')):
        raise AssertionError('reference retained a non-synthetic default address')
    rows.append(json.loads(text))
with gzip.open(sys.argv[1], 'wt', encoding='utf-8') as handle:
    for row in rows:
        handle.write(json.dumps(row, ensure_ascii=True) + '\n')
print('C29 native public references:', len(rows))
