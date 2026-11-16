"""pytest plugin: record every call of the public functions of
backend/common/router_split.py and backend/common/rule_ip_presence.py that the
Python test suite makes, for the Go replay (routersplit/recorded_test.go).
Routers R02; written before R01's shared routers_record.py existed (merge it
there later).

    R02_RECORD_DIR=/tmp/r02rec PYTHONPATH=.:server/internal/routersplit/testdata \
        ~/cf-test/.venv/bin/python -m pytest backend -p r02_record -n 6 --dist=worksteal -q -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/routersplit/testdata/r02_record.py merge /tmp/r02rec

Each top-level call records the test id, the function, its bound arguments
(the repository argument as a marker), the result or exception (class, str,
RouterSplitError code/status, isinstance LinuxGatewayError) and the
collaborators' answers during the call: setup_flow.profile_sessions,
repository.get_profile_agent_bindings / get_profile (in call order), and for
classical_ip_lines a snapshot of the rules directory (relative path -> bytes)
plus the stored shared rule library. rule_ip_presence._MEMO is cleared before
each recorded call so the answer never depends on an earlier test.
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
_SNAPSHOT_LIMIT = 4 * 1024 * 1024

ROUTER_SPLIT = ['read_marker', 'is_full_tun', 'route_rule_set_ids', 'tun_filters', 'full_tun_document',
                'full_tun_base_yaml', 'is_full_tun_document', 'check_generation', 'require_full_tun_generated',
                'legacy_context', 'needs_context', 'base_is_full_tun', 'legacy_source', 'publish_legacy_source',
                'legacy_gateway_agent_ids', 'gateway_was_used', 'mode_from_context', 'router_split_state']
IP_PRESENCE = ['classical_ip_lines']


class Unsupported(Exception):
    pass


class _Repo:
    """Marker for a repository argument."""


def conv(value):
    if isinstance(value, dict):
        out = {}
        for k, v in value.items():
            if not isinstance(k, str):
                raise Unsupported('non-string key')
            out[k] = conv(v)
        return out
    if isinstance(value, list):
        return [conv(v) for v in value]
    if isinstance(value, tuple):
        return {'__tuple__': [conv(v) for v in value]}
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unsupported('non-finite float')
        return value
    if isinstance(value, (bytes, bytearray)):
        return {'__bytes__': bytes(value).hex()}
    if isinstance(value, (datetime.date, datetime.datetime)):
        raise Unsupported('date')
    if isinstance(value, _Repo) or type(value).__name__ in ('ProfileRepository', '_RecordingRepository'):
        return {'__repo__': True}
    raise Unsupported(f'unsupported {type(value).__name__}')


def _error(exc):
    from backend.common.linux_gateway import LinuxGatewayError
    entry = {'class': type(exc).__name__, 'msg': str(exc), 'gateway': isinstance(exc, LinuxGatewayError)}
    if hasattr(exc, 'code') and type(exc).__name__ == 'RouterSplitError':
        entry['code'] = exc.code
        entry['status'] = exc.status
    return entry


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


class _Recorder:
    def __init__(self):
        self.calls = []  # [{'fn': name, 'args': [...], 'out'|'err': ...}]
        self.unsupported = None

    def add(self, name, args, call):
        entry = {'fn': name}
        try:
            entry['args'] = conv(args)
        except Unsupported as exc:
            self.unsupported = str(exc)
        try:
            result = call()
        except Exception as exc:
            entry['err'] = _error(exc)
            self.calls.append(entry)
            raise
        try:
            entry['out'] = conv(result)
        except Unsupported as exc:
            self.unsupported = str(exc)
        self.calls.append(entry)
        return result


class _RecordingRepository:
    def __init__(self, repo, rec):
        self._repo = repo
        self._rec = rec

    def get_profile_agent_bindings(self, profile_id, *args, **kwargs):
        return self._rec.add('get_profile_agent_bindings', [profile_id],
                             lambda: self._repo.get_profile_agent_bindings(profile_id, *args, **kwargs))

    def get_profile(self, profile_id, *args, **kwargs):
        return self._rec.add('get_profile', [profile_id], lambda: self._repo.get_profile(profile_id, *args, **kwargs))

    def __getattr__(self, name):
        return getattr(self._repo, name)


def _unwrap(repository):
    return repository._repo if isinstance(repository, _RecordingRepository) else repository


def _rules_snapshot(rec):
    from backend.utils import rule_utils
    snapshot = {'files': {}, 'library': None}
    try:
        root = rule_utils.get_rules_dir()
    except Exception as exc:  # noqa: BLE001
        snapshot['root_error'] = _error(exc)
        root = None
    total = 0
    if root and os.path.isdir(root):
        for dirpath, _dirs, files in os.walk(root):
            for name in files:
                path = os.path.join(dirpath, name)
                try:
                    with open(path, 'rb') as fh:
                        data = fh.read(_SNAPSHOT_LIMIT + 1)
                except OSError:
                    continue
                total += len(data)
                if total > _SNAPSHOT_LIMIT:
                    rec.unsupported = 'rules directory too large'
                    return snapshot
                snapshot['files'][os.path.relpath(path, root)] = data.hex()
    try:
        from backend.common.config import get_repository
        library = get_repository().get_shared().get('rule_library', [])
        snapshot['library'] = conv(library)
    except Unsupported as exc:
        rec.unsupported = str(exc)
    except Exception as exc:  # noqa: BLE001
        snapshot['library_error'] = _error(exc)
    return snapshot


def _dispatch(module_name, name, original, signature):
    def call(*args, **kwargs):
        if _DEPTH[0] > 0:
            return original(*args, **kwargs)
        try:
            bound = signature.bind(*args, **kwargs)
            bound.apply_defaults()
        except TypeError:
            return original(*args, **kwargs)
        rec = _Recorder()
        _DEPTH[0] += 1
        restores = []
        entry = {'test': _TEST[0], 'fn': name}
        try:
            from backend.common import setup_flow
            sessions_original = setup_flow.profile_sessions

            def sessions(repository, profile_id, *a, **k):
                return rec.add('profile_sessions', [profile_id],
                               lambda: sessions_original(_unwrap(repository), profile_id, *a, **k))
            setup_flow.profile_sessions = sessions
            restores.append(lambda: setattr(setup_flow, 'profile_sessions', sessions_original))
            arguments = dict(bound.arguments)
            if 'repository' in arguments and arguments['repository'] is not None:
                arguments['repository'] = _RecordingRepository(arguments['repository'], rec)
            if module_name == 'rule_ip_presence':
                from backend.common import rule_ip_presence
                rule_ip_presence._MEMO.clear()
                entry['rules'] = _rules_snapshot(rec)
            try:
                entry['args'] = {k: (conv(v) if k != 'repository' else (None if v is None else {'__repo__': True}))
                                 for k, v in arguments.items()}
            except Unsupported as exc:
                rec.unsupported = str(exc)
            try:
                result = original(**arguments)
            except Exception as exc:
                entry['err'] = _error(exc)
                raise
            try:
                entry['out'] = conv(result)
            except Unsupported as exc:
                rec.unsupported = str(exc)
            return result
        finally:
            for restore in reversed(restores):
                restore()
            _DEPTH[0] -= 1
            entry['env'] = rec.calls
            if rec.unsupported is None:
                _RECORDS.append(entry)
            else:
                _RECORDS.append({'test': _TEST[0], 'fn': name, 'unsupported': rec.unsupported})
    return call


def _install():
    from backend.common import router_split, rule_ip_presence
    for module, module_name, names in ((router_split, 'router_split', ROUTER_SPLIT),
                                       (rule_ip_presence, 'rule_ip_presence', IP_PRESENCE)):
        for name in names:
            function = getattr(module, name)
            original = _copy_function(function)
            dispatcher = _dispatch(module_name, name, original, inspect.signature(function))
            hook = f'_r02_recorded_{name}'
            setattr(module, hook, dispatcher)
            namespace = {}
            exec(compile(f'def {name}(*args, **kwargs):\n    return {hook}(*args, **kwargs)\n', 'r02_record', 'exec'),
                 module.__dict__, namespace)
            function.__code__ = namespace[name].__code__
            function.__defaults__ = None
            function.__kwdefaults__ = None


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('R02_RECORD_DIR')
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
    unsupported = {}
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                if 'unsupported' in entry:
                    key = f"{entry['fn']}: {entry['unsupported']}"
                    unsupported[key] = unsupported.get(key, 0) + 1
                    continue
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
    per_fn = {}
    for e in rows:
        per_fn[e['fn']] = per_fn.get(e['fn'], 0) + 1
    print(f'{len(rows)} distinct calls from {tests} tests in {files} files -> {output}')
    print('per function:', json.dumps(dict(sorted(per_fn.items(), key=lambda kv: -kv[1]))))
    print('unsupported (not recorded):', json.dumps(unsupported, ensure_ascii=False))


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'r02_recorded.jsonl.gz'))
