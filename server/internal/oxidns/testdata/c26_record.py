"""pytest plugin: record every top-level call of the C26 functions
(oxidns_common_updates, oxidns_device_policy, oxidns_editor, and the early
port of device_notes.stored_names) the Python test suite makes, for the Go
replay test (c26_test.go). Everything they call is ported (C22-C25), so no
collaborator answers are recorded: Go runs the whole call.

    C26_RECORD_DIR=/tmp/c26rec PYTHONPATH=.:server/internal/oxidns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c26_record backend ...
    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/c26_record.py merge /tmp/c26rec

Rows: {"test", "fn", "args", "out" | "err", "aliases"?, "args_after"?,
"ctx"?, "ctx_after"?}. "aliases" are [first path, other path] pairs of
objects shared inside the argument list (YAML aliases); "args_after" is the
argument list after the call when the call changed it in place. Functions
taking an oxidns_common._Context (prepare_rule_update, finish_rule_updates,
_stop_pipeline) record args [doc, settings, ...] and "ctx" = the context's
state ({"by_tag": [[tag, index | null, copy?]], "parked_live" | "parked",
"included", "attached"}); "ctx_after" is that state after the call.
_MISSING (oxidns_editor) is {"$missing": true}.
"""

import json
import os
import sys

import c22_record as base
import c24_record as c24

_RECORDS = []
_TEST = ['']
_DEPTH = [0]
_MISSING_MARK = {'$missing': True}


def _ctx_state(ctx):
    plugins = ctx.doc.get('plugins', []) if isinstance(ctx.doc.get('plugins'), list) else []
    by_tag = []
    for tag, plugin in ctx.by_tag.items():
        index = next((i for i, item in enumerate(plugins) if item is plugin), None)
        by_tag.append([tag, index] if index is not None else [tag, None, base.conv(plugin)])
    state = {'by_tag': by_tag, 'included': sorted(ctx.included_tags),
             'attached': ctx.plugins is ctx.doc.get('plugins')}
    if ctx.parked is ctx.settings.get('_common_disabled'):
        state['parked_live'] = True
    else:
        state['parked'] = base.conv(ctx.parked)
    return state


def _top(fn, args, call, out_of=lambda result: result, ctx=None, args_of=None):
    if _DEPTH[0] > 0:
        return call()
    _DEPTH[0] += 1
    row = {'test': _TEST[0], 'fn': fn}
    unsupported = []
    recorded_args = args_of() if args_of else args
    try:
        row['args'] = base.conv(recorded_args)
        aliases = c24._aliases(recorded_args)
        if aliases:
            row['aliases'] = aliases
        if ctx is not None:
            row['ctx'] = _ctx_state(ctx)
    except (TypeError, RecursionError) as exc:
        unsupported.append(f'args: {exc}')
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
        except (TypeError, RecursionError) as exc:
            unsupported.append(f'out: {exc}')
        return result
    finally:
        _DEPTH[0] -= 1
        if 'args' in row:
            try:
                after = base.conv(args_of() if args_of else args)
                if after != row['args']:
                    row['args_after'] = after
                if ctx is not None:
                    state = _ctx_state(ctx)
                    if state != row['ctx']:
                        row['ctx_after'] = state
            except (TypeError, RecursionError) as exc:
                unsupported.append(f'args after: {exc}')
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(row)


def _install():
    from backend.common import oxidns_common_updates as up
    from backend.common import oxidns_device_policy as dp
    from backend.common import oxidns_editor as ed
    from backend.common import device_notes as dn

    def simple(module, name, params, out_of=lambda result: result, args_of=None):
        names = [p.split('=')[0].strip() for p in params.split(',') if p.strip() and p.strip() != '*']

        def make(original):
            def recorded(*args):
                return _top(name, list(args), lambda: original(*args), out_of,
                            args_of=(lambda: args_of(*args)) if args_of else None)
            return recorded
        base._shim(module, name, params, ', '.join(names), make)

    simple(up, 'reconcile_native_withdrawals', 'before, candidate, original_settings, settings, config',
           out_of=lambda result: list(result))
    simple(up, 'rebase_native_rule_sources', 'before, after')
    simple(up, 'prepare_native_rule_updates', "document, settings, config, base_url=''")

    def with_ctx(name, params, call_args):
        def make(original):
            def recorded(ctx, *rest):
                return _top(name, None, lambda: original(ctx, *rest), lambda result: None, ctx=ctx,
                            args_of=lambda: [ctx.doc, ctx.settings, *rest])
            return recorded
        base._shim(up, name, params, call_args, make)
    with_ctx('prepare_rule_update', "ctx, changes, config, base_url=''", 'ctx, changes, config, base_url')
    with_ctx('finish_rule_updates', "ctx, changes, config, base_url=''", 'ctx, changes, config, base_url')
    with_ctx('_stop_pipeline', 'ctx, refresh_only=False', 'ctx, refresh_only')

    simple(dp, 'revision', 'settings')
    simple(dp, 'patch_document', 'document, previous, rows, config=None')
    simple(dp, 'read_policies', 'settings, config')
    simple(dp, 'apply_policies', "settings, config, rows, base_url=''")

    def entry_out(result):
        if not result:
            return {}
        return {'profile_id': result['profile_id'], 'agents': result['agents'],
                'addresses': sorted([list(pair) for pair in result['addresses']]),
                'hosts': sorted(result['hosts'])}
    simple(dp, 'entry_context', 'config', out_of=entry_out)

    def inspect(original):
        from backend.common.oxidns_common_routing import _plugins

        def recorded(document, plugins, config=None):
            # Only the production shape: plugins is _plugins(document).
            derived = _plugins(document)
            if list(derived) != list(plugins) or any(derived[k] is not plugins[k] for k in plugins):
                return original(document, plugins, config)
            return _top('inspect_entries', [document, config], lambda: original(document, plugins, config))
        return recorded
    base._shim(dp, 'inspect_entries', 'document, plugins, config=None', 'document, plugins, config', inspect)

    simple(dn, 'stored_names', 'config')

    simple(ed, 'template_inputs', 'settings, config=None')
    simple(ed, 'prepare_editor', "settings, config, base_url=''")
    simple(ed, 'preview_template', "settings, proposed_inputs, config, base_url='', resolutions=None")

    def mark(value):
        return _MISSING_MARK if value is ed._MISSING else value

    def merge(original):
        def recorded(base_value, current, proposed, path, resolutions, conflicts):
            return _top('_merge', None, lambda: original(base_value, current, proposed, path, resolutions, conflicts),
                        out_of=mark,
                        args_of=lambda: [mark(base_value), mark(current), mark(proposed), list(path), resolutions, conflicts])
        return recorded
    base._shim(ed, '_merge', 'base, current, proposed, path, resolutions, conflicts',
               'base, current, proposed, path, resolutions, conflicts', merge)


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C26_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    base.merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c26_recorded.jsonl.gz'))
