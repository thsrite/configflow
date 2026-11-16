"""pytest plugin: record every top-level call of the C25 functions
(oxidns_common_routing, dns_watchdog) the Python test suite makes, with the
answers of the functions of later modules (C26 prepare_editor,
prepare_rule_update, finish_rule_updates) and of the routers hook
rule_ip_presence.classical_ip_lines during the call, for the Go replay test
(c25_test.go).

    C25_RECORD_DIR=/tmp/c25rec PYTHONPATH=.:server/internal/oxidns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c25_record backend ...
    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/c25_record.py merge /tmp/c25rec

Rows: {"test", "fn", "args", "out" | "err", "references"?, "issues"?,
"aliases"?, "args_after"?, "same"?, "collab": [...], "ipl": [...]}.
"aliases" are [first path, other path] pairs of objects shared inside the
argument list (YAML aliases), so the replay can share them again;
"args_after" is the argument list after the call when the call changed it
in place; "same" (migrate_domain_only_routing) tells whether the returned
document/settings are the argument objects themselves. "collab" entries
follow c24_record.py. "ipl" are the classical_ip_lines answers
{"h", "out" | "err"} keyed by the hash of (item, config).
"""

import json
import os
import sys

import c22_record as base
import c24_record as c24

_RECORDS = []
_TEST = ['']


def _top(fn, args, call, out_of=lambda result: result, same=None):
    if c24._DEPTH[0] > 0 or c24._COLLAB_DEPTH[0] > 0:
        return call()
    c24._DEPTH[0] += 1
    row = {'test': _TEST[0], 'fn': fn, 'collab': [], 'ipl': []}
    unsupported = []
    try:
        row['args'] = base.conv(args)
        aliases = c24._aliases(args)
        if aliases:
            row['aliases'] = aliases
    except (TypeError, RecursionError) as exc:
        unsupported.append(f'args: {exc}')
    c24._ACTIVE[0] = row
    try:
        result = call()
    except Exception as exc:
        try:
            c24._error(row, exc)
        except (TypeError, RecursionError) as conv_exc:
            unsupported.append(str(conv_exc))
        raise
    else:
        try:
            row['out'] = base.conv(out_of(result))
            if same is not None:
                row['same'] = same(result)
        except (TypeError, RecursionError) as exc:
            unsupported.append(f'out: {exc}')
        return result
    finally:
        c24._DEPTH[0] -= 1
        c24._ACTIVE[0] = None
        if 'args' in row:
            try:
                after = base.conv(args)
                if after != row['args']:
                    row['args_after'] = after
            except (TypeError, RecursionError) as exc:
                unsupported.append(f'args after: {exc}')
        if 'unsupported' in row:
            unsupported.insert(0, row['unsupported'])
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(row)


def _install():
    from backend.common import oxidns_common as cm
    from backend.common import oxidns_common_routing as rt
    from backend.common import oxidns_common_updates as up
    from backend.common import dns_watchdog as wd
    from backend.common import rule_ip_presence as ip

    # C26 collaborators, recorded at their boundary (c24_record's wrapper).
    cm.prepare_editor = c24._collab(
        'prepare_editor', cm.prepare_editor,
        lambda settings, config, base_url='': [settings, config, base_url])
    for name in ('prepare_rule_update', 'finish_rule_updates'):
        setattr(up, name, c24._collab(
            name, getattr(up, name),
            lambda ctx, changes, config, base_url='': [ctx.doc, ctx.settings, changes, config, base_url],
            out_of=lambda result: None,
            after_of=lambda ctx, changes, config, base_url='': c24._ctx_after(ctx)))

    # Routers hook: the answers are looked up by argument hash.
    def ipl(original):
        def recorded(item, config):
            row = c24._ACTIVE[0]
            if row is None or c24._COLLAB_DEPTH[0] > 0:
                return original(item, config)
            entry = {}
            try:
                entry['h'] = c24._hash([item, config])
            except (TypeError, RecursionError) as exc:
                row.setdefault('unsupported', f'classical_ip_lines args: {exc}')
            try:
                result = original(item, config)
            except Exception as exc:
                entry['err'] = [type(exc).__name__, str(exc)]
                row['ipl'].append(entry)
                raise
            entry['out'] = result
            row['ipl'].append(entry)
            return result
        return recorded
    base._shim(ip, 'classical_ip_lines', 'item, config', 'item, config', ipl)

    def simple(module, name, params, call_args=None, out_of=lambda result: result, same=None):
        names = [p.split('=')[0].strip() for p in params.split(',') if p.strip() and p.strip() != '*']

        def make(original):
            def recorded(*args):
                return _top(name, list(args), lambda: original(*args), out_of,
                            (lambda result: same(args, result)) if same else None)
            return recorded
        base._shim(module, name, params, call_args or ', '.join(names), make)

    simple(rt, 'read_routing', 'document, settings, config')
    simple(rt, 'migrate_domain_only_routing', "document, settings, config, base_url=''",
           out_of=lambda result: list(result),
           same=lambda args, result: [result[0] is args[0], result[1] is args[1]])
    simple(rt, 'remove_rule', "settings, config, identity, base_url=''")
    simple(rt, 'resource_readers', 'document')
    simple(rt, 'resource_is_read', 'path, readers')
    simple(rt, 'owned_chains', 'document')
    simple(rt, 'refresh_rule_helpers', 'document')
    simple(rt, '_prefix_end', 'args')
    simple(rt, '_references', 'value, tag')
    simple(wd, 'ensure_local_dns_watchdog', 'document')

    def downloads(original):
        def recorded(document, settings, config, base_url='', records=None):
            if records is not None:
                return original(document, settings, config, base_url, records)
            return _top('resource_downloads', [document, settings, config, base_url],
                        lambda: original(document, settings, config, base_url))
        return recorded
    base._shim(rt, 'resource_downloads', "document, settings, config, base_url='', records=None",
               'document, settings, config, base_url, records', downloads)

    def apply(original):
        def recorded(document, settings, config, changes, base_url='', before_config=None, notices=None):
            args = [document, settings, config, changes, base_url, before_config, notices]

            def call():
                return original(document, settings, config, changes, base_url,
                                before_config=before_config, notices=notices)
            return _top('apply_routing', args, call,
                        out_of=lambda result: [result[0], result[1], notices])
        return recorded
    base._shim(rt, 'apply_routing',
               "document, settings, config, changes, base_url='', *, before_config=None, notices=None",
               'document, settings, config, changes, base_url, before_config, notices', apply)


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C25_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    base.merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c25_recorded.jsonl.gz'))
