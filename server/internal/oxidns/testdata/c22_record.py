"""pytest plugin: record every call of the OxiDNS settings normalizer (C05)
and of the OxiDNS converter / native loader / references / match targets
(C22) the Python test suite makes, plus the rule_dns, device policy and
validation functions this package ports early, for the Go replay test
(recorded_test.go).

    C22_RECORD_DIR=/tmp/c22rec PYTHONPATH=.:server/internal/oxidns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c22_record backend ...
    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/c22_record.py merge /tmp/c22rec

Only top-level calls are recorded (a call made inside another recorded call
is part of it). Rows: {"test", "fn", "args": [...], "out" | "err": [class,
message], "issues"?, "references"?, "unholdable"?}; bytes are
{"$bytes": base64}; a schema object of schemas() is {"$schema": path}.
Calls whose values JSON cannot hold are {"unsupported": reason} rows.
"""

import base64
import gzip
import hashlib
import json
import os
import sys
import types

import yaml

_RECORDS = []
_TEST = ['']
_DEPTH = [0]
_SCHEMA_REFS = {}


def _schema_refs():
    if not _SCHEMA_REFS:
        from backend.common.oxidns_validation import schemas
        global_schema, plugins = schemas()

        def walk(value, path):
            if isinstance(value, dict):
                _SCHEMA_REFS.setdefault(id(value), list(path))
                for key, item in value.items():
                    walk(item, (*path, key))
            elif isinstance(value, list):
                for index, item in enumerate(value):
                    walk(item, (*path, index))
        walk(global_schema, ('global',))
        walk(plugins, ('plugins',))
    return _SCHEMA_REFS


def conv(value, schemas=False, _active=None):
    _active = set() if _active is None else _active
    if schemas and isinstance(value, dict) and id(value) in _schema_refs():
        return {'$schema': _schema_refs()[id(value)]}
    if isinstance(value, (dict, list, tuple)):
        if id(value) in _active:
            raise TypeError('recursive value')
        _active.add(id(value))
        try:
            if isinstance(value, dict):
                out = {}
                for k, v in value.items():
                    if not isinstance(k, str):
                        raise TypeError('non-string key')
                    out[k] = conv(v, schemas, _active)
                return out
            return [conv(v, schemas, _active) for v in value]
        finally:
            _active.discard(id(value))
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise TypeError('non-finite float')
        return value
    if isinstance(value, bytes):
        return {'$bytes': base64.b64encode(value).decode()}
    raise TypeError(f'unsupported {type(value).__name__}')


def _holdable_value(value, active=None):
    active = set() if active is None else active
    if isinstance(value, dict):
        if id(value) in active:
            return False
        active.add(id(value))
        ok = all(isinstance(k, str) for k in value) and all(_holdable_value(v, active) for v in value.values())
        active.discard(id(value))
        return ok
    if isinstance(value, list):
        if id(value) in active:
            return False
        active.add(id(value))
        ok = all(_holdable_value(v, active) for v in value)
        active.discard(id(value))
        return ok
    return not isinstance(value, (set, tuple))


def text_holdable(text):
    if not isinstance(text, str):
        return True
    for candidate in (text, text.strip()):
        try:
            value = yaml.safe_load(candidate)
        except Exception:
            continue
        if not _holdable_value(value):
            return False
    return True


def settings_holdable(config):
    ox = config.get('oxidns') if isinstance(config, dict) else None
    ox = ox if isinstance(ox, dict) else {}
    return all(text_holdable(ox.get(k)) for k in ('custom_config', 'local_dns', 'remote_dns', 'fallback_dns'))


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


def _record(fn, args, call, out=lambda v: v, unholdable=False, schemas=False):
    if _DEPTH[0] > 0:
        return call()
    _DEPTH[0] += 1
    unsupported = []
    entry = {'test': _TEST[0], 'fn': fn}
    if unholdable:
        entry['unholdable'] = True
    try:
        entry['args'] = conv(args, schemas)
    except (TypeError, RecursionError) as exc:
        unsupported.append(str(exc))
    try:
        result = call()
    except Exception as exc:
        entry['err'] = [type(exc).__name__, str(exc)]
        try:
            if hasattr(exc, 'issues'):
                entry['issues'] = conv(exc.issues)
            if hasattr(exc, 'references'):
                entry['references'] = conv(exc.references)
        except (TypeError, RecursionError) as conv_exc:
            unsupported.append(str(conv_exc))
        raise
    else:
        try:
            entry['out'] = conv(out(result))
        except (TypeError, RecursionError) as exc:
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
    hook = f'_c22_recorded_{name}'
    setattr(module, hook, recorder(original))
    namespace = {}
    exec(compile(f'def {name}({signature}):\n    return {hook}({call_args})\n', 'c22_record', 'exec'),
         module.__dict__, namespace)
    function.__code__ = namespace[name].__code__
    function.__defaults__ = namespace[name].__defaults__
    function.__kwdefaults__ = namespace[name].__kwdefaults__


def _simple(module, name, fn, params, defaults='', out=lambda v: v, holdable=None, schemas=False):
    """Shim a function whose parameters are recorded positionally."""
    names = [p.split('=')[0].lstrip('*').strip() for p in params.split(',') if p.strip() and p.strip() != '*']

    def make(original):
        def recorded(*args):
            values = list(args)
            return _record(fn, values, lambda: original(*args), out,
                           unholdable=bool(holdable) and not holdable(*values), schemas=schemas)
        return recorded
    signature = params
    call = ', '.join(names)
    _shim(module, name, signature, call, make)


def _install():
    from backend.common import oxidns_settings as st
    from backend.common import profile_context as pc
    from backend.converters import oxidns as cv
    from backend.common import oxidns_native as nv
    from backend.common import oxidns_references as rf
    from backend.common import oxidns_match_targets as mt
    from backend.common import rule_dns as rd
    from backend.common import oxidns_device_policy as dp
    from backend.common import oxidns_validation as vl

    # C05
    def normalize(original):
        def recorded(value=None, validate_native_arguments=True):
            return _record('normalize', [value, validate_native_arguments],
                           lambda: original(value, validate_native_arguments=validate_native_arguments))
        return recorded
    _shim(st, 'normalize_oxidns_settings', 'value=None, *, validate_native_arguments=True',
          'value, validate_native_arguments', normalize)
    _simple(st, 'safe_relative_path', 'safe_relative_path', "value, label='文件路径'")
    _simple(st, '_merge', 'merge', 'defaults, value')

    def append_query(original):
        def recorded(url, params):
            items = list(params.items()) if isinstance(params, dict) else list(params)
            return _record('append_url_query', [url, items], lambda: original(url, params))
        return recorded
    _shim(pc, 'append_url_query', 'url, params', 'url, params', append_query)

    # C22 converter
    _simple(cv, 'generate_oxidns_config', 'generate', "config_data, base_url=''", holdable=lambda c, b='': settings_holdable(c))
    _simple(cv, 'get_oxidns_ruleset_downloads', 'downloads', "config_data, base_url=''",
            holdable=lambda c, b='': settings_holdable(c))
    _simple(cv, 'get_oxidns_custom_files', 'custom_files', 'config_data', holdable=settings_holdable)

    def with_list(fn, name):
        def make(original):
            def recorded(config, paths):
                items = list(paths)
                return _record(fn, [config, items], lambda: original(config, items))
            return recorded
        _shim(cv, name, 'config, paths', 'config, paths', make)
    with_list('unavailable_rulesets', 'unavailable_rulesets')
    _simple(cv, 'ruleset_parts', 'ruleset_parts', 'item, config')

    def routing(original):
        def recorded(config, settings, legacy=False, items=None):
            return _record('routing_groups', [config, settings, legacy, items],
                           lambda: original(config, settings, legacy=legacy, items=items))
        return recorded
    _shim(cv, '_routing_groups', 'config, settings, *, legacy=False, items=None', 'config, settings, legacy, items', routing)
    _simple(cv, '_hosts_file_content', 'hosts', 'text')
    _simple(cv, '_forward', 'forward', 'text, defaults', holdable=lambda t, d: text_holdable(t))
    _simple(cv, '_fallback_upstreams', 'fallback', 'text', holdable=text_holdable)
    _simple(cv, '_items', 'items', 'config')
    _simple(cv, '_base', 'base', 'config, base_url')
    _simple(cv, '_tag', 'tag', 'prefix, identity')
    _simple(cv, '_rule_path', 'rule_path', 'item, part')

    # C22 native
    _simple(nv, 'load_native_yaml', 'load_native_yaml', "text, label='完整配置'")
    _simple(nv, 'managed_files', 'managed_files', 'value')

    def native(original):
        def recorded(settings, validate_arguments=True, sources=None):
            def call():
                result = original(settings, validate_arguments=validate_arguments, sources=sources)
                return result
            return _record('native_document', [settings, validate_arguments, sources is not None],
                           call, out=lambda document: {'document': document, 'sources': sources})
        return recorded
    _shim(nv, 'native_document', 'settings, *, validate_arguments=True, sources=None',
          'settings, validate_arguments, sources', native)
    _simple(nv, 'resource_references', 'resource_references', 'document')
    _simple(nv, 'relative_resource', 'relative_resource', 'value')

    def resources(original):
        def recorded(document, paths):
            items = list(paths)
            return _record('validate_resource_files', [document, items], lambda: original(document, items))
        return recorded
    _shim(nv, 'validate_resource_files', 'document, paths', 'document, paths', resources)

    # C22 references
    def locations(original):
        def recorded(document, tags=(), dynamic=True):
            items = list(tags)
            return _record('reference_locations', [document, items, dynamic],
                           lambda: original(document, items, dynamic=dynamic))
        return recorded
    _shim(rf, 'reference_locations', 'document, tags=(), *, dynamic=True', 'document, tags, dynamic', locations)
    _simple(rf, 'reference_reason', 'reference_reason', "references, prefix='功能仍被自定义执行链引用，请在完整参数中调整'")

    original_init = rf.OxidnsReferenceError.__init__

    def reference_init(self, message, references):
        holder = []

        def call():
            original_init(self, message, references)
            holder.append(str(self))
            return str(self)
        _record('reference_error', [message, references], call)
    rf.OxidnsReferenceError.__init__ = reference_init

    # C22 match targets
    _simple(mt, 'read_match_targets', 'read_match_targets', 'document, settings')

    # Early parts of C27 / C26 / C23 ported with C22.
    def action(original):
        def recorded(value, parked=False):
            return _record('normalize_action', [value, parked], lambda: original(value, parked=parked))
        return recorded
    _shim(rd, 'normalize_action', 'value, *, parked=False', 'value, parked', action)
    _simple(rd, 'route_key', 'route_key', 'action')
    _simple(rd, 'route_exec', 'route_exec', 'route, part')
    _simple(rd, 'entry_key', 'entry_key', 'upstream, source')

    def helpers(original):
        def recorded(routes, actions, settings):
            items = list(routes)
            return _record('helper_plugins', [items, actions, settings], lambda: original(items, actions, settings))
        return recorded
    _shim(rd, 'helper_plugins', 'routes, actions, settings', 'routes, actions, settings', helpers)
    _simple(dp, 'normalize_client_range', 'normalize_client_range', 'raw')
    _simple(dp, 'normalize_policies', 'normalize_policies', 'value')

    def schema(original):
        def recorded(value, schema, path=()):
            return _record('validate_schema', [value, schema, list(path)], lambda: original(value, schema, path),
                           schemas=True)
        return recorded
    _shim(vl, 'validate_schema', 'value, schema, path=()', 'value, schema, path', schema)
    _simple(vl, 'format_error', 'format_error', 'name, value')


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C22_RECORD_DIR')
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
