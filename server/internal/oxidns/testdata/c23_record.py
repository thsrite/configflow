"""pytest plugin: record every top-level call of the C23 functions
(oxidns_validation, oxidns_plugin_constraints, oxidns_global_constraints,
oxidns_reference_validation) the Python test suite makes, for the Go replay
test (c23_test.go). Same row format as c22_record.py.

    C23_RECORD_DIR=/tmp/c23rec PYTHONPATH=.:server/internal/oxidns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c23_record backend ...
    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/c23_record.py merge /tmp/c23rec
"""

import json
import os
import sys

import c22_record as base

_RECORDS = []
_TEST = ['']
_DEPTH = [0]


def _record(fn, args, call, out=lambda v: v, schemas=False):
    if _DEPTH[0] > 0:
        return call()
    _DEPTH[0] += 1
    unsupported = []
    entry = {'test': _TEST[0], 'fn': fn}
    try:
        entry['args'] = base.conv(args, schemas)
    except (TypeError, RecursionError) as exc:
        unsupported.append(str(exc))
    try:
        result = call()
    except Exception as exc:
        entry['err'] = [type(exc).__name__, str(exc)]
        try:
            if hasattr(exc, 'issues'):
                entry['issues'] = base.conv(exc.issues)
        except (TypeError, RecursionError) as conv_exc:
            unsupported.append(str(conv_exc))
        raise
    else:
        try:
            entry['out'] = base.conv(out(result))
        except (TypeError, RecursionError) as exc:
            unsupported.append(str(exc))
        return result
    finally:
        _DEPTH[0] -= 1
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(entry)


def _install():
    from backend.common import oxidns_validation as vl
    from backend.common import oxidns_plugin_constraints as pc
    from backend.common import oxidns_global_constraints as gc
    from backend.common import oxidns_reference_validation as rv

    def simple(module, name, fn, params, schemas=False):
        names = [p.split('=')[0].strip() for p in params.split(',') if p.strip()]

        def make(original):
            def recorded(*args):
                return _record(fn, list(args), lambda: original(*args), schemas=schemas)
            return recorded
        base._shim(module, name, params, ', '.join(names), make)

    simple(vl, 'validate_native_config', 'validate_native_config', 'document')
    simple(vl, 'require_valid_document', 'require_valid_document', 'document')
    simple(vl, 'validate_expanded_document', 'validate_expanded_document', 'document, sources')

    def settings(original):
        def recorded(settings, config=None, base_url=''):
            return _record('validate_settings', [settings, config, base_url],
                           lambda: original(settings, config, base_url))
        return recorded
    base._shim(vl, 'validate_settings', "settings, config=None, base_url=''", 'settings, config, base_url', settings)

    def references(original):
        def recorded(document, plugin_schemas, dependency_graph=None):
            def call():
                return original(document, plugin_schemas, dependency_graph=dependency_graph)
            given = dependency_graph is not None
            return _record('collect_reference_issues', [document, plugin_schemas, given], call,
                           out=lambda issues: {'issues': issues, 'graph': dependency_graph} if given else issues,
                           schemas=True)
        return recorded
    base._shim(rv, 'collect_reference_issues', 'document, plugin_schemas, *, dependency_graph=None',
               'document, plugin_schemas, dependency_graph', references)
    simple(pc, 'collect_plugin_issues', 'collect_plugin_issues', 'plugin, index, document')
    simple(pc, 'validate_plugin_format', 'validate_plugin_format', 'name, value')
    simple(gc, 'collect_global_issues', 'collect_global_issues', 'document')


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C23_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    base.merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c23_recorded.jsonl.gz'))
