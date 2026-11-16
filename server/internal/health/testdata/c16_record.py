"""pytest plugin: record every call the Python test suite makes to C16's
public functions (health_check, node_dns, node_latency, region_check,
domain_probe) for the Go replay test (recorded_test.go).

    C16_RECORD_DIR=/tmp/c16rec PYTHONPATH=.:server/internal/health/testdata \
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 10 -p c16_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/health/testdata/c16_record.py merge /tmp/c16rec

Each row holds the test id, the function, its bound arguments and the result
or the exception (class, str). Functions that mutate an argument record it
after the call (`mutated`). The worker calls (_run_probe, check_settings)
carry the probe binary lookup and the subprocess.run call (`worker`: argv,
stdin, timeout, GOMAXPROCS/GOMEMLIMIT, and the answer or exception);
settings/load_config/check_settings the environment variables and the
system_config fields they read; the stores (RegionStore/ProbeStore) the file
before and after and the frozen naive datetime.now(); RegionStore.update the
data its mutate callable left.
"""

import copy
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
ENV_KEYS = ('CONFIGFLOW_NODE_TEST_URL', 'CONFIGFLOW_NODE_TEST_TIMEOUT_MS', 'CONFIGFLOW_NODE_TEST_UNIFIED_DELAY')


class Unsupported(Exception):
    pass


class FrozenDatetime(_REAL_DATETIME):
    @classmethod
    def now(cls, tz=None):
        frozen = getattr(_LOCAL, 'now', None)
        if frozen is None:
            return _REAL_DATETIME.now(tz)
        return frozen if tz is None else frozen.astimezone(tz)


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


def _error(exc):
    return {'class': type(exc).__name__, 'text': str(exc)}


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


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


def _system_fields():
    from backend.common.config import get_system_config
    system = get_system_config().get('system_config', {})
    if not isinstance(system, dict):
        raise Unsupported('system_config')
    return {key: system[key] for key in ('node_test_dns_config', 'node_test_dns') if key in system}


MUTATING = {'initialize_group_health': 'group', 'initialize_provider_health': 'source'}
WORKER = {'_run_probe', 'check_settings'}
SYSTEM = {'settings', 'load_config', 'check_settings', 'effective_settings', 'describe_settings'}
STORES = {'RegionStore.update', 'RegionStore.load', 'ProbeStore.save', 'ProbeStore.load'}


def _record(name, original, args, kwargs):
    import subprocess
    from backend.utils import node_latency, sub_store_client
    signature = inspect.signature(original)
    bound = signature.bind(*args, **kwargs)
    bound.apply_defaults()
    arguments = dict(bound.arguments)
    store = arguments.pop('self', None)
    outer = getattr(_LOCAL, 'now', None) is None
    if outer:
        _LOCAL.now = _REAL_DATETIME.now()
    entry = {'test': _TEST[0], 'fn': name, 'now': _LOCAL.now.isoformat()}
    unsupported = False
    restore = []
    worker = {}
    conversions = []
    mutated = []
    path = None
    try:
        try:
            if name in MUTATING:
                arguments[MUTATING[name]] = copy.deepcopy(arguments[MUTATING[name]])
            if name in SYSTEM:
                entry['env'] = {key: os.environ[key] for key in ENV_KEYS if key in os.environ}
                entry['system'] = _system_fields()
            if name in STORES:
                path = store._path(arguments['agent_id'])
                entry['before'] = _read(path)
            if name == 'RegionStore.update':
                mutate = arguments['mutate']

                def recording(data):
                    result = mutate(data)
                    mutated.append(copy.deepcopy(data))
                    return result
                bound.arguments['mutate'] = recording
                arguments['mutate'] = None
        except Unsupported:
            unsupported = True
        except Exception:  # noqa: BLE001 - the call itself will fail the same way
            unsupported = True
        if name in WORKER:
            original_binary = node_latency._probe_binary

            def binary():
                try:
                    value = original_binary()
                except Exception as exc:
                    worker['binary_err'] = _error(exc)
                    raise
                worker['binary'] = value
                return value
            node_latency._probe_binary = binary
            restore.append((node_latency, '_probe_binary', binary, original_binary))
            original_run = subprocess.run

            def run(argv, **kw):
                env = kw.get('env') or {}
                worker['argv'] = list(argv)
                worker['input'] = kw.get('input')
                worker['timeout'] = kw.get('timeout')
                worker['env'] = {key: env.get(key) for key in ('GOMAXPROCS', 'GOMEMLIMIT')}
                try:
                    value = original_run(argv, **kw)
                except subprocess.TimeoutExpired as exc:
                    worker['err'] = 'TimeoutExpired'
                    raise
                except OSError as exc:
                    worker['err'] = 'OSError'
                    raise
                except Exception as exc:
                    worker['err'] = type(exc).__name__
                    raise
                out = getattr(value, 'stdout', None)
                err = getattr(value, 'stderr', None)
                worker['returncode'] = getattr(value, 'returncode', None)
                worker['stdout'] = out.decode('latin-1') if isinstance(out, bytes) else out
                worker['stderr'] = err.decode('latin-1') if isinstance(err, bytes) else err
                return value
            subprocess.run = run
            restore.append((subprocess, 'run', run, original_run))
        if name in ('_proxy_of',):
            original_convert = sub_store_client.convert_proxy_string

            def convert(proxy_string, *a, **k):
                value = original_convert(proxy_string, *a, **k)
                conversions.append({'args': [proxy_string], 'ok': copy.deepcopy(value)})
                return value
            sub_store_client.convert_proxy_string = convert
            restore.append((sub_store_client, 'convert_proxy_string', convert, original_convert))
        try:
            result = original(*bound.args, **bound.kwargs)
        except Exception as exc:
            entry['err'] = _error(exc)
            raise
        else:
            entry['ok'] = result
        return result
    finally:
        for module, attr, wrapper, previous in reversed(restore):
            if getattr(module, attr) is wrapper:
                setattr(module, attr, previous)
        if outer:
            _LOCAL.now = None
        entry['args'] = arguments
        if name in MUTATING:
            entry['mutated'] = bound.arguments[MUTATING[name]]
        if worker:
            entry['worker'] = worker
        if conversions:
            entry['conversions'] = conversions
        if mutated:
            entry['mutated'] = mutated[-1]
        if path is not None and not unsupported:
            try:
                entry['after'] = _read(path)
            except Unsupported:
                unsupported = True
        if not unsupported:
            try:
                _RECORDS.append(json.loads(json.dumps(conv(entry), ensure_ascii=False)))
            except (Unsupported, TypeError, ValueError):
                pass


TARGETS = {
    'backend.utils.health_check': ['initialize_group_health', 'initialize_provider_health', 'validate_health',
                                   'provider_health_check'],
    'backend.utils.node_dns': ['_ipv4_server', '_server', 'normalize_dns_servers', '_needs_bootstrap',
                               'normalize_config', 'load_config', 'effective_settings', 'describe_settings',
                               'check_settings'],
    'backend.utils.node_latency': ['settings', '_proxy_of', '_needs_sub_store', '_run_probe'],
    'backend.utils.region_check': ['region_of_name', 'region_label', 'service_requests', 'evaluate_services',
                                   'exit_info', 'public_services', 'domain_request', 'classify_domain_result',
                                   'summarize_domain', 'pick_targets', 'RegionStore.update', 'RegionStore.load'],
    'backend.utils.domain_probe': ['probe_url', 'path_healthy', 'judge', 'domain_suggestion', 'ProbeStore.save',
                                   'ProbeStore.load'],
}
FROZEN_MODULES = ['backend.utils.region_check', 'backend.utils.domain_probe']


def _install():
    import importlib
    for module_name in FROZEN_MODULES:
        importlib.import_module(module_name).datetime = FrozenDatetime
    for module_name, names in TARGETS.items():
        module = importlib.import_module(module_name)
        for name in names:
            owner = module
            attr = name
            if '.' in name:
                cls_name, attr = name.split('.')
                owner = getattr(module, cls_name)
            function = getattr(owner, attr)
            original = _copy_function(function)
            hook = f'_c16_recorded_{name.replace(".", "_")}'

            def make(name=name, original=original):
                def recorded(*args, **kwargs):
                    return _record(name, original, args, kwargs)
                return recorded
            setattr(module, hook, make())
            namespace = {}
            exec(compile(f'def {attr}(*args, **kwargs):\n    return {hook}(*args, **kwargs)\n', 'c16_record', 'exec'),
                 module.__dict__, namespace)
            function.__code__ = namespace[attr].__code__
            function.__defaults__ = None
            function.__kwdefaults__ = None


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C16_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


TIME_DEPENDENT = {'RegionStore.update', 'RegionStore.load', 'ProbeStore.save', 'ProbeStore.load'}


def merge(directory, output):
    seen = set()
    rows = []
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                key = dict(entry)
                key.pop('test')
                if entry['fn'] not in TIME_DEPENDENT:
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
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c16_recorded.jsonl.gz'))
