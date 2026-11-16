"""pytest plugin: record every call the Python test suite makes to C15's
public functions (subscription_refresh, subscription_health,
scheduled_refresh and the task-row helpers of routes/tasks) for the Go replay
test (recorded_test.go).

    C15_RECORD_DIR=/tmp/c15rec PYTHONPATH=.:server/internal/refresh/testdata \
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 -p c15_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/c15_record.py merge /tmp/c15rec

Each row holds the test id, the function, its bound arguments and the result
or the exception (class, str). Stateful functions also carry the files they
read before the call (`before`: shared-relative path or `task-status.json` ->
text or null) and after it (`after`), the collaborators' answers
(get_subscription_proxies_yaml, the fetch= callable, convert_proxy_string,
uuid4 hex) and the wall time: while a recorded call runs, datetime.now() of
the C15 modules is frozen at its start (`now`) and time.monotonic() of
scheduled_refresh is constant, so Go replays with the same fixed clock.
"""

import datetime as _dt
import gzip
import hashlib
import inspect
import json
import os
import sys
import threading
import types

_RECORDS = []
_TEST = ['']
_LOCAL = threading.local()
_REAL_DATETIME = _dt.datetime


class Unsupported(Exception):
    pass


def _frozen():
    return getattr(_LOCAL, 'now', None)


class FrozenDatetime(_REAL_DATETIME):
    @classmethod
    def now(cls, tz=None):
        frozen = _frozen()
        if frozen is None:
            return _REAL_DATETIME.now(tz)
        return frozen.astimezone(tz) if tz is not None else frozen.astimezone().replace(tzinfo=None)


class _TimeShim(types.ModuleType):
    def __init__(self, real):
        super().__init__('time')
        self._real = real

    def __getattr__(self, name):
        return getattr(self._real, name)

    def monotonic(self):
        if _frozen() is not None:
            return 1000.0
        return self._real.monotonic()


def conv(value):
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
    if isinstance(value, _REAL_DATETIME):
        return {'__datetime__': value.isoformat()}
    if isinstance(value, (bytes, bytearray)):
        return {'__bytes__': bytes(value).hex()}
    raise Unsupported(f'unsupported {type(value).__name__}')


def error_spec(exc):
    """The corpus_test.go buildError form of an exception."""
    from backend.utils.sub_store_client import SubscriptionFetchError
    spec = {'class': type(exc).__name__, 'message': str(exc)}
    status = getattr(getattr(exc, 'response', None), 'status_code', None)
    if isinstance(status, int) and not isinstance(status, bool):
        spec['status'] = status
    if isinstance(exc, SubscriptionFetchError):
        spec['direct'] = error_spec(exc.direct_error) if exc.direct_error is not None else None
        spec['sub_store'] = error_spec(exc.sub_store_error) if exc.sub_store_error is not None else None
    return spec


def _error(exc):
    return {'class': type(exc).__name__, 'text': str(exc)}


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


def _result(value):
    from backend.utils.sub_store_client import SubscriptionResult
    if isinstance(value, SubscriptionResult):
        return {'__result__': [value[0], value[1], value.traffic, value.fetched_at]}
    if isinstance(value, tuple):
        return [_result(v) for v in value]
    return value


def _repo():
    from backend.common.config import get_repository
    return get_repository()


def _read(path):
    try:
        with open(path, 'rb') as handle:
            data = handle.read()
    except OSError:
        return None
    try:
        return data.decode('utf-8')
    except UnicodeDecodeError:
        raise Unsupported('binary file')


def _paths(name, arguments):
    """Files a stateful call reads or writes: label -> absolute path."""
    from backend.utils import subscription_refresh as srf
    repo = _repo()
    out = {}

    def shared(rel):
        out[rel] = str(repo.shared_path(rel))

    sub = arguments.get('sub')
    if isinstance(sub, dict):
        if isinstance(sub.get('id'), str):
            shared('subscribes/' + sub['id'].replace('/', '_').replace('\\', '_') + '.json')
        if isinstance(sub.get('url'), str):
            shared(srf._download_cache_path(sub['url']))
    if name in ('load_health', 'record_fetch', 'forget', 'refresh_subscription'):
        shared('subscription_health.json')
    if name == 'refresh_subscription':
        out['task-status.json'] = os.path.join(str(repo.data_dir), 'task-status.json')
    return out


STATEFUL = {'fetch_subscription_source', 'download_subscription_nodes', 'remember_source_download',
            'refresh_subscription', 'load_health', 'record_fetch', 'forget', 'subscription_cache_time',
            'is_library_source', 'find_resource'}


class _Wrap:
    """Temporarily wraps a module attribute to log its answers."""

    def __init__(self, module, attr, log):
        self.module, self.attr, self.log = module, attr, log
        self.original = getattr(module, attr)
        owner = self

        def wrapper(*args, **kwargs):
            try:
                value = owner.original(*args, **kwargs)
            except Exception as exc:
                log.append({'args': [conv(a) for a in args], 'err': error_spec(exc)})
                raise
            log.append({'args': [conv(a) for a in args], 'ok': _result(value)})
            return value
        self.wrapper = wrapper
        setattr(module, attr, wrapper)

    def restore(self):
        if getattr(self.module, self.attr) is self.wrapper:
            setattr(self.module, self.attr, self.original)


def _record(module_name, name, original, args, kwargs):
    import uuid
    from backend.utils import sub_store_client
    signature = inspect.signature(original)
    bound = signature.bind(*args, **kwargs)
    bound.apply_defaults()
    arguments = dict(bound.arguments)
    outer = _frozen() is None
    if outer:
        _LOCAL.now = _REAL_DATETIME.now(_dt.timezone.utc)
        _LOCAL.uuids = []
    entry = {'test': _TEST[0], 'fn': name, 'now': _LOCAL.now.isoformat()}
    unsupported = False
    wraps = []
    fetches = []
    downloads = []
    conversions = []
    paths = {}
    try:
        if name in STATEFUL:
            try:
                paths = _paths(name, arguments)
                entry['before'] = {label: _read(path) for label, path in paths.items()}
                if name in ('is_library_source', 'find_resource'):
                    shared = _repo().get_shared()
                    entry['shared'] = {key: shared.get(key) for key in ('subscriptions', 'rule_library') if key in shared}
            except Unsupported:
                unsupported = True
            except Exception:  # noqa: BLE001 - the call itself will fail the same way
                unsupported = True
        if name == 'fetch_subscription_source':
            fetch = arguments['fetch']

            def recording(*a, **k):
                try:
                    value = fetch(*a, **k)
                except Exception as exc:
                    fetches.append({'args': [conv(x) for x in a], 'err': error_spec(exc)})
                    raise
                fetches.append({'args': [conv(x) for x in a], 'ok': _result(value)})
                return value
            bound.arguments['fetch'] = recording
            arguments['fetch'] = None
        if name in ('fetch_subscription_source', 'download_subscription_nodes', 'refresh_subscription'):
            wraps.append(_Wrap(sub_store_client, 'convert_proxy_string', conversions))
            wraps.append(_Wrap(sub_store_client, 'get_subscription_proxies_yaml', downloads))
        if name == 'describe_failure':
            arguments['error'] = error_spec(arguments['error'])
        if name in ('source_cache_metadata', 'remember_source_download'):
            arguments['result'] = _result(arguments['result'])
        if name in STATEFUL:
            patched = [label for label, (module, attr, original_fn) in _COLLABORATORS.items()
                       if getattr(module, attr) is not original_fn]
            if patched:
                entry['patched'] = patched
        uuids_before = len(_LOCAL.uuids)
        try:
            result = original(*bound.args, **bound.kwargs)
        except Exception as exc:
            entry['err'] = _error(exc)
            raise
        else:
            entry['ok'] = _result(result)
        return result
    finally:
        for wrap in reversed(wraps):
            wrap.restore()
        entry['args'] = arguments
        if fetches:
            entry['fetches'] = fetches
        if downloads:
            entry['downloads'] = downloads
        if conversions:
            entry['conversions'] = conversions
        if name in ('download_subscription_nodes', 'refresh_subscription'):
            entry['uuids'] = _LOCAL.uuids[uuids_before:]
        if paths and not unsupported:
            try:
                entry['after'] = {label: _read(path) for label, path in paths.items()}
            except Unsupported:
                unsupported = True
        if outer:
            _LOCAL.now = None
        if not unsupported:
            try:
                _RECORDS.append(json.loads(json.dumps(conv(entry), ensure_ascii=False)))
            except (Unsupported, TypeError, ValueError):
                pass


TARGETS = {
    'backend.utils.subscription_refresh': [
        'validate_refresh_interval', 'source_cache_metadata', '_is_fresh', '_download_cache_path',
        'remember_source_download', 'download_subscription_nodes', 'fetch_subscription_source',
    ],
    'backend.utils.subscription_health': ['parse_userinfo', 'load_health', 'record_fetch', 'forget'],
    'backend.utils.scheduled_refresh': [
        'describe_failure', 'rule_interval', 'subscription_interval', 'next_run_at', 'subscription_cache_time',
        'is_library_source', 'find_resource', 'refresh_subscription',
    ],
    'backend.routes.tasks': ['_history', '_host'],
}

FROZEN_MODULES = ['backend.utils.subscription_refresh', 'backend.utils.subscription_health',
                  'backend.utils.scheduled_refresh', 'backend.utils.task_status', 'backend.utils.subscription_cache',
                  'backend.utils.sub_store_client']


# Collaborators a test may monkeypatch: such a call is replayed only when
# none was replaced (recorded as `patched`).
_COLLABORATORS = {}
COLLABORATORS = [
    ('backend.utils.subscription_cache', ['save_subscription_nodes', 'load_subscription_cache']),
    ('backend.utils.subscription_health', ['record_fetch', 'load_health']),
    ('backend.utils.task_status', ['record', 'get', 'load']),
    ('backend.utils.subscription_filter', ['filter_subscription_items']),
    ('backend.utils.sub_store_client', ['parse_proxies_from_yaml', 'proxies_to_nodes']),
    ('backend.utils.subscription_refresh', ['download_subscription_nodes', 'remember_source_download']),
    ('backend.utils.subscription_hosts', ['extract_subscription_hosts']),
    ('backend.utils.subscription_dialers', ['extract_subscription_dialers']),
    ('backend.common.config', ['get_repository']),
]


def _install():
    import importlib
    import uuid
    for module_name, attrs in COLLABORATORS:
        module = importlib.import_module(module_name)
        for attr in attrs:
            _COLLABORATORS[f'{module_name}.{attr}'] = (module, attr, getattr(module, attr))
    for module_name in FROZEN_MODULES:
        module = importlib.import_module(module_name)
        module.datetime = FrozenDatetime
    scheduled = importlib.import_module('backend.utils.scheduled_refresh')
    scheduled.time = _TimeShim(scheduled.time)
    real_uuid4 = uuid.uuid4

    def uuid4():
        value = real_uuid4()
        if _frozen() is not None:
            _LOCAL.uuids.append(value.hex[:8])
        return value
    uuid.uuid4 = uuid4
    for module_name, names in TARGETS.items():
        module = importlib.import_module(module_name)
        for name in names:
            function = getattr(module, name)
            original = _copy_function(function)
            hook = f'_c15_recorded_{name}'

            def make(module_name=module_name, name=name, original=original):
                def recorded(*args, **kwargs):
                    return _record(module_name, name, original, args, kwargs)
                return recorded
            setattr(module, hook, make())
            namespace = {}
            exec(compile(f'def {name}(*args, **kwargs):\n    return {hook}(*args, **kwargs)\n', 'c15_record', 'exec'),
                 module.__dict__, namespace)
            function.__code__ = namespace[name].__code__
            function.__defaults__ = None
            function.__kwdefaults__ = None


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C15_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


TIME_FREE = {'validate_refresh_interval', '_download_cache_path', 'parse_userinfo', 'describe_failure',
             'rule_interval', 'subscription_interval', '_history', '_host', 'source_cache_metadata'}


def merge(directory, output):
    seen = set()
    rows = []
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                key = dict(entry)
                key.pop('test')
                if entry['fn'] in TIME_FREE:
                    key.pop('now', None)
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
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c15_recorded.jsonl.gz'))
