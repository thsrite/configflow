"""pytest plugin: record every call of the A09 public functions (Docker HA guide,
preflight, enrollment) the Python test suite makes, for the Go replay test
(docker_ha_recorded_test.go).

    A09_RECORD_DIR=/tmp/a09rec PYTHONPATH=.:server/internal/installer/testdata \
        ~/cf-test/.venv/bin/python -m pytest -p a09_record backend -q -n auto -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/a09_record.py merge /tmp/a09rec

Recorded functions: docker_ha_guide.generate_guide / identity_command /
inspection_command, docker_ha_preflight.parse_report / collector_script,
docker_ha_enrollment.issue / enroll / bootstrap_command. Only the outermost A09
call of the main thread is recorded (nested calls are part of its answer).

For issue/enroll the recorder also keeps what the call observed: the files under
DATA_DIR/docker-ha-installations before and after (contents and modes), every
time.time() value, every random byte string (uuid4 and token_urlsafe draw from
os.urandom), datetime.now(), and for enroll the Agent list the manager's
_update_agents handed to the updater (before/after) or the manager's exception.
A call made while a collaborator was monkeypatched (bootstrap_command, the Compose
generator, the preflight parser) is marked 'patched' and skipped by the replay.
"""

import base64
import datetime as _dt
import gzip
import hashlib
import json
import os
import stat
import sys
import threading
import time as _time
import types
import uuid as _uuid
from copy import deepcopy

_RECORDS = []
_TEST = ['']
_STATE = threading.local()
_MAIN = threading.main_thread()
_ORIGINAL = {}
_SKIPPED = {}


class Unsupported(Exception):
    pass


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
    if isinstance(value, str):
        try:
            value.encode('utf-8')
        except UnicodeEncodeError:
            raise Unsupported('lone surrogate') from None
        return value
    if isinstance(value, bool) or value is None or isinstance(value, int):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unsupported('non-finite float')
        return value
    if hasattr(value, '__fspath__'):
        return {'__path__': os.fspath(value)}
    raise Unsupported(f'unsupported {type(value).__name__}')


def _error(exc):
    entry = [type(exc).__name__, str(exc), isinstance(exc, ValueError)]
    status = getattr(exc, 'status', None)
    if isinstance(status, int):
        entry.append(status)
    return entry


def _tree(root):
    """Files under root: relative path -> [kind, mode, hex data | link target]."""
    out = {}
    if not os.path.lexists(root):
        return None
    for directory, dirs, files in os.walk(root):
        for name in sorted(dirs + files):
            path = os.path.join(directory, name)
            rel = os.path.relpath(path, root)
            st = os.lstat(path)
            mode = stat.S_IMODE(st.st_mode)
            if stat.S_ISLNK(st.st_mode):
                out[rel] = ['link', mode, os.readlink(path)]
            elif stat.S_ISDIR(st.st_mode):
                out[rel] = ['dir', mode, '']
            else:
                with open(path, 'rb') as fh:
                    out[rel] = ['file', mode, fh.read().hex()]
    return out


def _active():
    return getattr(_STATE, 'entry', None)


class _TimeShim:
    def time(self):
        value = _time.time()
        entry = _active()
        if entry is not None:
            entry.setdefault('times', []).append(value)
        return value

    def __getattr__(self, name):
        return getattr(_time, name)


def _random(n):
    data = os.urandom(n)
    entry = _active()
    if entry is not None:
        entry.setdefault('random', []).append(data.hex())
    return data


class _UUIDShim:
    def uuid4(self):
        return _uuid.UUID(bytes=_random(16), version=4)

    def __getattr__(self, name):
        return getattr(_uuid, name)


class _SecretsShim:
    def token_urlsafe(self, nbytes):
        return base64.urlsafe_b64encode(_random(nbytes)).rstrip(b'=').decode('ascii')

    def __getattr__(self, name):
        import secrets
        return getattr(secrets, name)


class _DatetimeShim:
    @staticmethod
    def now(*args):
        value = _dt.datetime.now(*args)
        entry = _active()
        if entry is not None:
            entry.setdefault('now', []).append(value.isoformat())
        return value

    def __getattr__(self, name):
        return getattr(_dt.datetime, name)


def _patched():
    """Collaborators a test replaced (the replay cannot follow them)."""
    from backend.agents import docker_ha_enrollment, docker_ha_preflight, install_script
    names = []
    checks = [(docker_ha_enrollment, 'bootstrap_command'), (docker_ha_preflight, 'parse_report'),
              (docker_ha_preflight, 'collector_script'), (install_script, 'generate_docker_agent_compose'),
              (install_script, 'validate_agent_name')]
    for module_name in ('docker_ha_compact', 'docker_ha_compose_direct', 'docker_ha_compose'):
        try:
            module = __import__('backend.agents.' + module_name, fromlist=['x'])
        except Exception:  # noqa: BLE001
            continue
        for attr in ('generate_compact', 'generate_direct', 'generate_bundle'):
            if hasattr(module, attr):
                checks.append((module, attr))
    for module, attr in checks:
        key = module.__name__ + '.' + attr
        if key in _ORIGINAL and getattr(module, attr) is not _ORIGINAL[key]:
            names.append(key)
    return names


def _record(fn, args_builder, call, around=None):
    if threading.current_thread() is not _MAIN or _active() is not None:
        return call()
    entry = {'test': _TEST[0], 'fn': fn}
    _STATE.entry = entry
    restores = []
    unsupported = None
    try:
        patched = _patched()
        if patched:
            entry['patched'] = patched
        try:
            entry['args'] = args_builder()
        except Unsupported as exc:
            unsupported = str(exc)
        if around is not None:
            restores = around(entry)
        try:
            result = call()
        except Exception as exc:
            entry['err'] = _error(exc)
            raise
        try:
            entry['out'] = conv(result)
        except Unsupported as exc:
            unsupported = str(exc)
        return result
    finally:
        for restore in reversed(restores):
            try:
                restore(entry)
            except Unsupported as exc:
                unsupported = str(exc)
        _STATE.entry = None
        if unsupported is None:
            _RECORDS.append(entry)
        else:
            _SKIPPED[unsupported] = _SKIPPED.get(unsupported, 0) + 1


def _data_dir(repository):
    try:
        return os.fspath(repository.data_dir)
    except Exception:  # noqa: BLE001
        return None


def _io_around(module, repository, manager=None):
    def around(entry):
        restores = []
        data_dir = _data_dir(repository)
        entry['data_dir_exists'] = data_dir is not None and os.path.isdir(data_dir)
        root = os.path.join(data_dir, 'docker-ha-installations') if data_dir else None
        entry['before'] = _tree(root) if root else None
        saved = {name: getattr(module, name) for name in ('time', 'uuid', 'secrets', 'datetime')}
        module.time, module.uuid, module.secrets, module.datetime = _TimeShim(), _UUIDShim(), _SecretsShim(), _DatetimeShim()

        def restore_module(entry):
            for name, value in saved.items():
                setattr(module, name, value)
            entry['after'] = _tree(root) if root else None
        restores.append(restore_module)
        if manager is not None:
            own = '_update_agents' in getattr(manager, '__dict__', {})
            original = manager._update_agents

            def update_agents(updater, profile_id=None):
                manager_entry = {'profile_id': profile_id}
                entry['manager'] = manager_entry

                def wrapped(agents):
                    manager_entry['before'] = conv(deepcopy(agents))
                    try:
                        value = updater(agents)
                    except Exception as exc:
                        manager_entry['updater_err'] = _error(exc)
                        raise
                    manager_entry['after'] = conv(deepcopy(agents))
                    return value
                try:
                    return original(wrapped, profile_id)
                except Exception as exc:
                    if 'updater_err' not in manager_entry:
                        manager_entry['err'] = _error(exc)
                    raise
            manager._update_agents = update_agents

            def restore_manager(entry):
                if own:
                    manager._update_agents = original
                else:
                    del manager._update_agents
            restores.append(restore_manager)
        return restores
    return around


def _rebind(module, name, wrapper, signature, call_args):
    """Route every reference to module.name (also `from … import name`) through wrapper."""
    function = getattr(module, name)
    original = types.FunctionType(function.__code__, function.__globals__, function.__name__,
                                  function.__defaults__, function.__closure__)
    original.__kwdefaults__ = function.__kwdefaults__
    _ORIGINAL[module.__name__ + '.' + name] = function
    hook = '_a09_recorded_' + name
    setattr(module, hook, wrapper(original))
    namespace = {}
    exec(compile(f'def {name}({signature}):\n    return {hook}({call_args})\n', 'a09_record', 'exec'),
         module.__dict__, namespace)
    function.__code__ = namespace[name].__code__


def _install():
    from backend.agents import docker_ha_guide as guide, docker_ha_preflight as preflight, docker_ha_enrollment as enrollment
    from backend.agents import install_script
    for module_name in ('docker_ha_compact', 'docker_ha_compose_direct', 'docker_ha_compose'):
        try:
            module = __import__('backend.agents.' + module_name, fromlist=['x'])
        except Exception:  # noqa: BLE001
            continue
        for attr in ('generate_compact', 'generate_direct', 'generate_bundle'):
            if hasattr(module, attr):
                _ORIGINAL[module.__name__ + '.' + attr] = getattr(module, attr)
    for attr in ('generate_docker_agent_compose', 'validate_agent_name'):
        _ORIGINAL[install_script.__name__ + '.' + attr] = getattr(install_script, attr)

    _rebind(guide, 'generate_guide', lambda original: (
        lambda request, enrollment=None: _record(
            'generate_guide', lambda: [conv(deepcopy(request)), conv(deepcopy(enrollment))],
            lambda: original(request, enrollment))), 'request, enrollment=None', 'request, enrollment')
    _rebind(guide, 'identity_command', lambda original: (
        lambda path: _record('identity_command', lambda: [conv(deepcopy(path))], lambda: original(path))),
        'path', 'path')
    _rebind(guide, 'inspection_command', lambda original: (
        lambda container_name='': _record('inspection_command', lambda: [conv(deepcopy(container_name))],
                                          lambda: original(container_name))),
        "container_name=''", 'container_name')
    _rebind(preflight, 'parse_report', lambda original: (
        lambda output, network_mode='host': _record(
            'parse_report', lambda: [conv(output), conv(network_mode)], lambda: original(output, network_mode))),
        "output, network_mode='host'", 'output, network_mode')
    _rebind(preflight, 'collector_script', lambda original: (
        lambda: _record('collector_script', lambda: [], original)), '', '')
    _rebind(enrollment, 'bootstrap_command', lambda original: (
        lambda capability, request: _record(
            'bootstrap_command', lambda: [conv(deepcopy(capability)), conv(deepcopy(request))],
            lambda: original(capability, request))), 'enrollment, request', 'enrollment, request')
    _rebind(enrollment, 'issue', lambda original: (
        lambda repository, body, profile_id: _record(
            'issue', lambda: [conv(deepcopy(body)), conv(profile_id)],
            lambda: original(repository, body, profile_id),
            _io_around(enrollment, repository))), 'repository, body, profile_id', 'repository, body, profile_id')
    _rebind(enrollment, 'enroll', lambda original: (
        lambda repository, manager, session_id, token: _record(
            'enroll', lambda: [conv(session_id), conv(token)],
            lambda: original(repository, manager, session_id, token),
            _io_around(enrollment, repository, manager))),
        'repository, manager, session_id, token', 'repository, manager, session_id, token')


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('A09_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')
        fh.write(json.dumps({'_skipped': _SKIPPED}) + '\n')


def merge(directory, output):
    seen = set()
    rows = []
    skipped = {}
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                if '_skipped' in entry:
                    for reason, n in entry['_skipped'].items():
                        skipped[reason] = skipped.get(reason, 0) + n
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
    counts = {}
    for e in rows:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    tests = len({e['test'] for e in rows})
    print(f'{len(rows)} distinct calls from {tests} tests -> {output}: {counts}; not recordable: {skipped}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'docker_ha_recorded.jsonl.gz'))
