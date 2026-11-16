"""pytest plugin: record every generate_compact / generate_direct /
generate_bundle call (module A10) the Python test suite makes, for the Go
replay test (docker_ha_compose_recorded_test.go).

    A10_RECORD_DIR=/tmp/a10rec PYTHONPATH=.:server/internal/installer/testdata \
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 -p a10_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/a10_record.py merge /tmp/a10rec

The generators are pure (their only collaborator, generate_docker_agent_compose,
is A08's and is re-run by Go), so each record holds the test id, the
function, its arguments (params, request, enrollment) and the result
(compose text + info dict, bundle base64 included) or exception.
"""

import gzip
import hashlib
import json
import os
import sys
from copy import deepcopy

_RECORDS = []
_TEST = ['']
_UNSUPPORTED = [0]


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
    if isinstance(value, float) and (value != value or value in (float('inf'), float('-inf'))):
        raise TypeError('non-finite float')
    if value is None or isinstance(value, (bool, int, float, str)):
        return value
    raise TypeError(f'unsupported {type(value).__name__}')


def _wrap(module, name):
    original = getattr(module, name)

    def recorded(params, request, enrollment=None):
        entry = {'test': _TEST[0], 'fn': name}
        try:
            entry['args'] = [conv(deepcopy(params)), conv(deepcopy(request)), conv(deepcopy(enrollment))]
        except TypeError:
            _UNSUPPORTED[0] += 1
            return original(params, request, enrollment)
        try:
            result = original(params, request, enrollment)
        except Exception as exc:
            entry['err'] = [type(exc).__name__, str(exc)]
            _RECORDS.append(entry)
            raise
        try:
            entry['out'] = conv(list(result))
        except TypeError:
            _UNSUPPORTED[0] += 1
            return result
        _RECORDS.append(entry)
        return result
    setattr(module, name, recorded)


def pytest_configure(config):
    from backend.agents import docker_ha_compact, docker_ha_compose, docker_ha_compose_direct
    _wrap(docker_ha_compact, 'generate_compact')
    _wrap(docker_ha_compose_direct, 'generate_direct')
    _wrap(docker_ha_compose, 'generate_bundle')


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('A10_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')
        fh.write(json.dumps({'unsupported': _UNSUPPORTED[0]}) + '\n')


def merge(directory, output):
    seen, rows, unsupported = set(), [], 0
    for name in sorted(os.listdir(directory)):
        with open(os.path.join(directory, name)) as fh:
            for line in fh:
                entry = json.loads(line)
                if 'unsupported' in entry:
                    unsupported += entry['unsupported']
                    continue
                key = dict(entry)
                key.pop('test')
                digest = hashlib.sha256(json.dumps(key, sort_keys=True).encode()).hexdigest()
                if digest in seen:
                    continue
                seen.add(digest)
                rows.append(entry)
    rows.sort(key=lambda e: (e['test'], e['fn'], json.dumps(e['args'], sort_keys=True)))
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    tests = len({e['test'] for e in rows})
    files = len({e['test'].split('::')[0] for e in rows})
    print(f'{len(rows)} distinct calls from {tests} tests in {files} files ({unsupported} unsupported) -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'docker_ha_compose_recorded.jsonl.gz'))
