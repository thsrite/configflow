"""pytest plugin: record every call of the MosDNS converter (C21,
backend/converters/mosdns.py) the Python test suite makes, for the Go replay
test (recorded_test.go).

    C21_RECORD_DIR=/tmp/c21rec PYTHONPATH=.:server/internal/mosdns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c21_record backend ...
    PYTHONPATH=.:server/internal/mosdns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/mosdns/testdata/c21_record.py merge /tmp/c21rec

Recorded (top-level calls only; calls made inside another recorded call are
part of it): generate_mosdns_config, get_mosdns_ruleset_downloads,
get_mosdns_custom_files, convert_rule_content_for_mosdns,
domain_pattern_for_mosdns, parse_clash_rule_value, parse_dns_upstreams,
_build_rule_proxy_url, _ruleset_files, split_rules_and_rulesets,
_parse_custom_matches, _ruleset_parts. Rows have the format of gen.py's
corpus plus the test id; calls whose values JSON cannot hold are written as
{"unsupported": reason} rows.
"""

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
    raise TypeError(f'unsupported {type(value).__name__}')


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


def _record(fn, args, call, out=lambda v: v, unholdable=False):
    if _DEPTH[0] > 0:
        return call()
    _DEPTH[0] += 1
    unsupported = []
    entry = {'test': _TEST[0], 'fn': fn}
    if unholdable:
        entry['unholdable'] = True
    try:
        entry['args'] = conv(deepcopy(args))
    except TypeError as exc:
        unsupported.append(str(exc))
    try:
        result = call()
    except Exception as exc:
        entry['err'] = [type(exc).__name__, str(exc)]
        raise
    else:
        try:
            entry['out'] = conv(out(result))
        except TypeError as exc:
            unsupported.append(str(exc))
        return result
    finally:
        _DEPTH[0] -= 1
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(entry)


def _shim(module, name, signature, call_args, recorder):
    """Route module.name (and every `from … import name` binding) to recorder."""
    function = getattr(module, name)
    original = _copy_function(function)
    hook = f'_c21_recorded_{name}'
    setattr(module, hook, recorder(original))
    namespace = {}
    exec(compile(f'def {name}({signature}):\n    return {hook}({call_args})\n', 'c21_record', 'exec'),
         module.__dict__, namespace)
    function.__code__ = namespace[name].__code__
    function.__defaults__ = namespace[name].__defaults__
    function.__kwdefaults__ = namespace[name].__kwdefaults__


def _install():
    from backend.converters import mosdns as m
    import gen

    def settings_holdable(config):
        mos = config.get('mosdns') if isinstance(config, dict) else None
        mos = mos if isinstance(mos, dict) else {}
        return all(gen.text_holdable(mos.get(k)) for k in ('custom_config', 'local_dns', 'remote_dns', 'fallback_dns'))

    def config_base(fn, with_base=True):
        def make(original):
            if with_base:
                def recorded(config_data, base_url=''):
                    return _record(fn, [config_data, base_url], lambda: original(config_data, base_url),
                                   unholdable=not settings_holdable(config_data))
            else:
                def recorded(config_data):
                    return _record(fn, [config_data], lambda: original(config_data))
            return recorded
        return make
    _shim(m, 'generate_mosdns_config', 'config_data, base_url=""', 'config_data, base_url', config_base('generate'))
    _shim(m, 'get_mosdns_ruleset_downloads', 'config_data, base_url=""', 'config_data, base_url',
          config_base('downloads'))
    _shim(m, 'get_mosdns_custom_files', 'config_data', 'config_data', config_base('custom_files', False))
    _shim(m, 'split_rules_and_rulesets', 'config_data', 'config_data', config_base('split', False))

    def convert(original):
        def recorded(content, part=None):
            args = [content] if part is None else [content, part]
            return _record('convert', args, lambda: original(content, part),
                           unholdable=not gen.text_holdable(content))
        return recorded
    _shim(m, 'convert_rule_content_for_mosdns', 'content, part=None', 'content, part', convert)

    def simple(fn, out=lambda v: v, holdable_arg=False):
        def make(original):
            def recorded(value):
                return _record(fn, [value], lambda: original(value), out,
                               unholdable=holdable_arg and not gen.text_holdable(value))
            return recorded
        return make
    _shim(m, 'domain_pattern_for_mosdns', 'pattern', 'pattern', simple('domain_pattern'))
    _shim(m, 'parse_clash_rule_value', 'line', 'line', simple('parse_clash', list))
    _shim(m, 'parse_dns_upstreams', 'dns_config', 'dns_config',
          simple('upstreams', lambda v: {'dump': gen.dump(v)}, True))
    _shim(m, '_parse_custom_matches', 'custom_matches', 'custom_matches', simple('custom_matches'))
    _shim(m, '_ruleset_parts', 'behavior', 'behavior', simple('ruleset_parts'))

    def proxy_url(original):
        def recorded(config_data, effective_base_url, url, part=None, source_id=None):
            return _record('proxy_url', [config_data, effective_base_url, url, part, source_id],
                           lambda: original(config_data, effective_base_url, url, part, source_id))
        return recorded
    _shim(m, '_build_rule_proxy_url', 'config_data, effective_base_url, url, part=None, source_id=None',
          'config_data, effective_base_url, url, part, source_id', proxy_url)

    def ruleset_files(original):
        def recorded(config_data, effective_base_url, rule_set, file_stem=None):
            return _record('ruleset_files', [config_data, effective_base_url, rule_set, file_stem],
                           lambda: original(config_data, effective_base_url, rule_set, file_stem))
        return recorded
    _shim(m, '_ruleset_files', 'config_data, effective_base_url, rule_set, file_stem=None',
          'config_data, effective_base_url, rule_set, file_stem', ruleset_files)


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C21_RECORD_DIR')
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
    files = len({e['test'].split('::')[0] for e in rows})
    unsupported = sum(1 for e in rows if 'unsupported' in e)
    counts = {}
    for e in rows:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    print(f'{len(rows)} distinct calls from {tests} tests in {files} files ({unsupported} unsupported) {counts} -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'recorded.jsonl.gz'))
