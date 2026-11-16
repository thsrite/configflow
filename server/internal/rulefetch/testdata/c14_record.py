"""pytest plugin: record every call the Python test suite makes to C14's pure
public functions (ruleset_content, website_identity, remote_rule_sources and
the URL/proxy/candidate helpers of rule_fetch) for the Go replay test
(recorded_test.go).

    C14_RECORD_DIR=/tmp/c14rec PYTHONPATH=.:server/internal/rulefetch/testdata \
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 8 -p c14_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/rulefetch/testdata/c14_record.py merge /tmp/c14rec

Each row holds the test id, the function, its bound arguments (defaults
applied), and the result or the exception (class, str, and the errors list of
a RemoteRuleValidationError). Collaborators that are not pure arguments are
recorded as well: the settings and Flask host URL rule_fetch read, and the
answers of remove_rule_payload's `matches` callable. Only top-level calls are
recorded (a recorded function calling another records once).
"""

import datetime
import gzip
import hashlib
import inspect
import json
import os
import sys
import types

_RECORDS = []
_TEST = ['']
_DEPTH = [0]


class Unsupported(Exception):
    pass


def conv(value):
    import yaml
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if not isinstance(k, str):
                raise Unsupported('non-string key')
            out[k] = conv(v)
        return out
    if isinstance(value, (list, tuple)):
        return [conv(v) for v in value]
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        if isinstance(value, str) and any(0xD800 <= ord(c) <= 0xDFFF for c in value):
            raise Unsupported('surrogate')
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unsupported('non-finite float')
        return value
    if isinstance(value, (bytes, bytearray)):
        return {'__bytes__': bytes(value).hex()}
    if isinstance(value, yaml.Node):
        return {'__node__': [type(value).__name__, value.start_mark.index, value.end_mark.index,
                             bool(getattr(value, 'flow_style', False))]}
    if isinstance(value, (datetime.date, datetime.datetime)):
        raise Unsupported('date')
    raise Unsupported(f'unsupported {type(value).__name__}')


def _error(exc):
    out = {'class': type(exc).__name__, 'text': str(exc)}
    errors = getattr(exc, 'errors', None)
    if errors is not None and type(exc).__name__ == 'RemoteRuleValidationError':
        out['errors'] = errors
    return out


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


class _Context:
    def __init__(self):
        self.matches = {}
        self.settings = None
        self.host_url = None


_ACTIVE = [None]


def _record(module_name, name, original, args, kwargs):
    if _DEPTH[0]:
        return original(*args, **kwargs)
    signature = inspect.signature(original)
    bound = signature.bind(*args, **kwargs)
    bound.apply_defaults()
    arguments = dict(bound.arguments)
    ctx = _Context()
    if name == 'remove_rule_payload':
        matches = arguments['matches']

        def recording(line):
            result = matches(line)
            ctx.matches[line] = bool(result)
            return result
        bound.arguments['matches'] = recording
        arguments['matches'] = None
    if module_name == 'backend.utils.rule_fetch' and name in (
            'is_internal_rule_url', 'github_download_candidates', 'get_rule_fetch_proxy', 'rule_request_options'):
        from backend.utils import rule_fetch
        try:
            from flask import has_request_context, request
            ctx.host_url = request.host_url if has_request_context() else None
        except Exception:  # noqa: BLE001
            ctx.host_url = None
        try:
            settings = rule_fetch.rule_fetch_settings(arguments.get('config_data'))
            ctx.settings = settings
        except Exception as exc:  # noqa: BLE001 - the call itself will fail the same way
            ctx.settings = {'__error__': _error(exc)}
        if arguments.get('config_data') is None:
            arguments['config_data'] = {'system_config': ctx.settings}
    entry = {'test': _TEST[0], 'fn': name}
    _DEPTH[0] += 1
    try:
        try:
            result = original(*bound.args, **bound.kwargs)
        except Exception as exc:
            entry['err'] = _error(exc)
            raise
        else:
            entry['ok'] = result
        return result
    finally:
        _DEPTH[0] -= 1
        entry['args'] = arguments
        if ctx.matches:
            entry['matches'] = ctx.matches
        if ctx.host_url is not None:
            entry['host_url'] = ctx.host_url
        try:
            if 'ok' in entry and name == 'validate_rule_url':
                r = entry['ok']
                entry['ok'] = [r.scheme, r.netloc, r.path, r.query, r.fragment]
            _RECORDS.append(json.loads(json.dumps(conv(entry), ensure_ascii=False)))
        except (Unsupported, TypeError, ValueError):
            pass


TARGETS = {
    'backend.utils.ruleset_content': [
        'inspect_domain_content', 'normalize_classical_lines', 'normalize_mihomo_yaml', 'normalize_mihomo_ruleset',
        'classical_line_error', 'validate_domain_content', 'validate_remote_rule_content', 'read_rule_payload',
        'append_rule_lines', 'append_rule_payload', 'remove_rule_payload', 'content_digest',
    ],
    'backend.utils.website_identity': ['site_title', 'clean_text', 'known_service', 'page_identity', '_meta_content'],
    'backend.utils.remote_rule_sources': ['resolve_remote_rule_source'],
    'backend.utils.rule_fetch': [
        'validate_rule_url', 'normalize_rule_fetch_proxy', 'is_internal_rule_url', 'get_rule_fetch_proxy',
        'rule_request_options', 'is_github_url', 'github_download_candidates',
    ],
}


def _install():
    import importlib
    for module_name, names in TARGETS.items():
        module = importlib.import_module(module_name)
        for name in names:
            function = getattr(module, name)
            original = _copy_function(function)
            hook = f'_c14_recorded_{name}'

            def make(module_name=module_name, name=name, original=original):
                def recorded(*args, **kwargs):
                    return _record(module_name, name, original, args, kwargs)
                return recorded
            setattr(module, hook, make())
            namespace = {}
            exec(compile(f'def {name}(*args, **kwargs):\n    return {hook}(*args, **kwargs)\n', 'c14_record', 'exec'),
                 module.__dict__, namespace)
            function.__code__ = namespace[name].__code__
            function.__defaults__ = None
            function.__kwdefaults__ = None


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C14_RECORD_DIR')
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
    data = '\n'.join(json.dumps(e, ensure_ascii=False, sort_keys=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    tests = len({e['test'] for e in rows})
    by_fn = {}
    for e in rows:
        by_fn[e['fn']] = by_fn.get(e['fn'], 0) + 1
    print(f'{len(rows)} distinct calls from {tests} tests -> {output}')
    print(json.dumps(by_fn, sort_keys=True))


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c14_recorded.jsonl.gz'))
