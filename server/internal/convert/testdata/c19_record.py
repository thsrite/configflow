"""pytest plugin: record every call of the Surge (C19) and Loon (C20) converter
functions the Python test suite makes, for the Go replay test
(surge_loon_recorded_test.go).

    C19_RECORD_DIR=/tmp/c19rec PYTHONPATH=.:server/internal/convert/testdata \
        ~/cf-test/.venv/bin/python -m pytest -p c19_record backend ...
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/c19_record.py merge /tmp/c19rec

Recorded (top-level calls only; calls made inside another recorded call are
part of it): generate_surge_config, generate_loon_config,
convert_node_to_surge, convert_node_to_loon, convert_proxies_to_surge_text,
convert_proxies_to_loon_text, convert_proxy_group_to_surge,
get_proxy_group_nodes, convert_proxy_group_to_loon and loon._build_rules.
Each row holds the test id, the arguments, the Sub-Store conversions made
during the call (reject_surge_dialers converts URI node strings; conversions
reused from an outer node_conversions scope are added), the result or
exception and, for the generators, the profile afterwards. Calls whose
values JSON cannot hold are written as {"unsupported": reason} rows.
"""

import datetime
import gzip
import hashlib
import json
import os
import sys
import types
from copy import deepcopy

_RECORDS = []
_TEST = ['']
_DEPTH = [0]


def conv(value):
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if not isinstance(k, str):
                raise TypeError('non-string key')
            out[k] = conv(v)
        return out
    if isinstance(value, (list, tuple)):
        return [conv(v) for v in value]
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise TypeError('non-finite float')
        return value
    if isinstance(value, bytes):
        return {'__bytes__': value.hex()}
    if isinstance(value, (datetime.date, datetime.datetime)):
        return {'__date__': str(value)}
    raise TypeError(f'unsupported {type(value).__name__}')


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


def _record_call(fn, args, call, mutated_ref=None):
    if _DEPTH[0] > 0:
        return call()
    from backend.utils import sub_store_client
    from backend.converters import mihomo
    _DEPTH[0] += 1
    conversions = {}
    unsupported = []
    original_convert = sub_store_client.convert_proxy_string

    def recording_convert(proxy_string, *a, **k):
        result = original_convert(proxy_string, *a, **k)
        try:
            conversions.setdefault(proxy_string, conv(result))
        except TypeError as exc:
            unsupported.append(str(exc))
        return deepcopy(result)
    sub_store_client.convert_proxy_string = recording_convert
    scope = getattr(mihomo._node_conversions, 'scope', None)
    if scope is not None and scope[1]:
        for text, proxy in scope[0].items():
            try:
                conversions.setdefault(text, conv(proxy))
            except TypeError as exc:
                unsupported.append(str(exc))
    entry = {'test': _TEST[0], 'fn': fn}
    try:
        entry['args'] = conv(args)
    except TypeError as exc:
        unsupported.append(str(exc))
    try:
        result = call()
    except Exception as exc:
        entry['err'] = [type(exc).__name__, str(exc)]
        raise
    else:
        try:
            entry['out'] = conv(result)
        except TypeError as exc:
            unsupported.append(str(exc))
        return result
    finally:
        sub_store_client.convert_proxy_string = original_convert
        _DEPTH[0] -= 1
        if mutated_ref is not None:
            try:
                entry['mutated'] = conv(mutated_ref)
            except TypeError as exc:
                unsupported.append(str(exc))
        entry['env'] = {'conversions': conversions}
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(entry)


def _shim(module, name, signature, call_args, recorder):
    """Route module.name (and every `from … import name` binding) to recorder."""
    function = getattr(module, name)
    original = _copy_function(function)
    hook = f'_c19_recorded_{name}'
    setattr(module, hook, recorder(original))
    namespace = {}
    exec(compile(f'def {name}({signature}):\n    return {hook}({call_args})\n', 'c19_record', 'exec'),
         module.__dict__, namespace)
    function.__code__ = namespace[name].__code__
    function.__defaults__ = namespace[name].__defaults__
    function.__kwdefaults__ = namespace[name].__kwdefaults__


def _install():
    from backend.converters import surge, loon

    def generator(fn):
        def make(original):
            def recorded(config_data, base_url=''):
                return _record_call(fn, {'profile': deepcopy(config_data), 'base_url': base_url},
                                    lambda: original(config_data, base_url=base_url), mutated_ref=config_data)
            return recorded
        return make
    _shim(surge, 'generate_surge_config', 'config_data, base_url=""', 'config_data, base_url', generator('surge'))
    _shim(loon, 'generate_loon_config', 'config_data, base_url=""', 'config_data, base_url', generator('loon'))

    def simple(fn, convert_out=None):
        def make(original):
            def recorded(*args):
                def call():
                    result = original(*args)
                    return result
                result = _record_call(fn, [deepcopy(a) for a in args], call)
                return result
            return recorded
        return make
    _shim(surge, 'convert_node_to_surge', 'node', 'node', simple('node_surge'))
    _shim(loon, 'convert_node_to_loon', 'node', 'node', simple('node_loon'))
    _shim(surge, 'convert_proxies_to_surge_text', 'proxies', 'proxies', simple('proxies_surge'))
    _shim(loon, 'convert_proxies_to_loon_text', 'proxies', 'proxies', simple('proxies_loon'))
    _shim(surge, 'convert_proxy_group_to_surge', 'group, config_data, base_url="", smart_group_map=None',
          'group, config_data, base_url, smart_group_map', simple('group_surge'))
    _shim(surge, 'get_proxy_group_nodes', 'group, config_data', 'group, config_data', simple('group_nodes'))
    _shim(loon, 'convert_proxy_group_to_loon', 'group, members', 'group, members', simple('group_loon'))
    _shim(loon, '_build_rules', 'config_data, effective_base_url', 'config_data, effective_base_url', simple('build_rules'))


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C19_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


def merge(directory, output):
    seen = set()
    rows = []
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                key = dict(entry)
                key.pop('test')
                digest = hashlib.sha256(json.dumps(key, sort_keys=True).encode()).hexdigest()
                if digest in seen:
                    continue
                seen.add(digest)
                rows.append(entry)
    rows.sort(key=lambda e: (e['test'], e['fn']))
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    tests = len({e['test'] for e in rows})
    unsupported = sum(1 for e in rows if 'unsupported' in e)
    print(f'{len(rows)} distinct calls from {tests} tests ({unsupported} unsupported) -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'surge_loon_recorded.jsonl.gz'))
