"""pytest plugin: record every top-level read_common_settings /
apply_common_settings call (oxidns_common, module C24) the Python test suite
makes, together with what the functions of later modules answered during the
call, for the Go replay test (c24_test.go).

    C24_RECORD_DIR=/tmp/c24rec PYTHONPATH=.:server/internal/oxidns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest -p c24_record backend ...
    PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python \\
        server/internal/oxidns/testdata/c24_record.py merge /tmp/c24rec

Rows: {"test", "fn", "args", "out" | "err", "references"?, "issues"?,
"collab": [...]}. Each collaborator entry is {"fn", "h", "out" | "err",
"after"?}: "h" is the first 16 hex digits of sha256 over
json.dumps(args, ensure_ascii=False, separators=(',', ':')) so the replay can
check that Go asked the same question; "after" is the state an in-place
collaborator left behind (refresh_rule_helpers: the document;
prepare_rule_update / finish_rule_updates: the context's document, settings,
tag map and parked dict). Collaborators called inside another collaborator
are part of it and not recorded. Calls whose values JSON cannot hold are
{"unsupported": reason} rows.
"""

import hashlib
import json
import os
import sys

import c22_record as base

_RECORDS = []
_TEST = ['']
_DEPTH = [0]
_COLLAB_DEPTH = [0]
_ACTIVE = [None]


def _hash(args):
    text = json.dumps(base.conv(args), ensure_ascii=False, separators=(',', ':'))
    return hashlib.sha256(text.encode()).hexdigest()[:16]


def _error(entry, exc):
    entry['err'] = [type(exc).__name__, str(exc)]
    if hasattr(exc, 'references'):
        entry['references'] = base.conv(exc.references)
    if hasattr(exc, 'issues'):
        entry['issues'] = base.conv(exc.issues)


def _aliases(value):
    """[first path, other path] for every dict/list reached twice (YAML
    aliases), so the replay can restore the shared objects."""
    seen, out = {}, []

    def walk(item, path):
        if isinstance(item, (dict, list)):
            if id(item) in seen:
                out.append([seen[id(item)], list(path)])
                return
            seen[id(item)] = list(path)
            for key, child in (item.items() if isinstance(item, dict) else enumerate(item)):
                walk(child, (*path, key))
    walk(value, ())
    return out


def _doc_after(document):
    return {'doc': base.conv(document), 'aliases': _aliases(document)}


def _ctx_after(ctx):
    plugins = ctx.doc.get('plugins', []) if isinstance(ctx.doc.get('plugins'), list) else []
    by_tag = []
    for tag, plugin in ctx.by_tag.items():
        index = next((i for i, item in enumerate(plugins) if item is plugin), None)
        by_tag.append([tag, index] if index is not None else [tag, None, base.conv(plugin)])
    after = {**_doc_after(ctx.doc), 'settings': base.conv(ctx.settings), 'by_tag': by_tag}
    if ctx.parked is ctx.settings.get('_common_disabled'):
        after['parked_live'] = True
    else:
        after['parked'] = base.conv(ctx.parked)
    return after


def _collab(name, original, args_of, out_of=lambda v: v, after_of=None):
    def wrapped(*args, **kwargs):
        row = _ACTIVE[0]
        if row is None or _COLLAB_DEPTH[0] > 0:
            return original(*args, **kwargs)
        entry = {'fn': name}
        try:
            entry['h'] = _hash(args_of(*args, **kwargs))
            if os.environ.get('C24_RECORD_ARGS'):
                entry['a'] = base.conv(args_of(*args, **kwargs))
        except (TypeError, RecursionError) as exc:
            row.setdefault('unsupported', f'{name} args: {exc}')
        _COLLAB_DEPTH[0] += 1
        try:
            result = original(*args, **kwargs)
        except Exception as exc:
            try:
                _error(entry, exc)
            except (TypeError, RecursionError) as conv_exc:
                row.setdefault('unsupported', f'{name} error: {conv_exc}')
            raise
        else:
            try:
                entry['out'] = base.conv(out_of(result))
                if after_of is not None:
                    entry['after'] = after_of(*args, **kwargs)
            except (TypeError, RecursionError) as exc:
                row.setdefault('unsupported', f'{name} answer: {exc}')
            return result
        finally:
            _COLLAB_DEPTH[0] -= 1
            row['collab'].append(entry)
    return wrapped


def _top(fn, args, call):
    if _DEPTH[0] > 0 or _COLLAB_DEPTH[0] > 0:
        return call()
    _DEPTH[0] += 1
    row = {'test': _TEST[0], 'fn': fn, 'collab': []}
    unsupported = []
    try:
        row['args'] = base.conv(args)
    except (TypeError, RecursionError) as exc:
        unsupported.append(str(exc))
    _ACTIVE[0] = row
    try:
        result = call()
    except Exception as exc:
        try:
            _error(row, exc)
        except (TypeError, RecursionError) as conv_exc:
            unsupported.append(str(conv_exc))
        raise
    else:
        try:
            row['out'] = base.conv(result)
        except (TypeError, RecursionError) as exc:
            unsupported.append(str(exc))
        return result
    finally:
        _DEPTH[0] -= 1
        _ACTIVE[0] = None
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

    cm.prepare_editor = _collab(
        'prepare_editor', cm.prepare_editor,
        lambda settings, config, base_url='': [settings, config, base_url])
    rt.read_routing = _collab(
        'read_routing', rt.read_routing,
        lambda document, settings, config: [document, settings, config])
    rt.owned_chains = _collab('owned_chains', rt.owned_chains, lambda document: [document])
    rt.apply_routing = _collab(
        'apply_routing', rt.apply_routing,
        lambda document, settings, config, changes, base_url='', **kw: [document, settings, config, changes, base_url],
        out_of=lambda result: list(result))
    rt.resource_downloads = _collab(
        'resource_downloads', rt.resource_downloads,
        lambda document, settings, config, base_url='', records=None: [document, settings, config, base_url])
    rt.refresh_rule_helpers = _collab(
        'refresh_rule_helpers', rt.refresh_rule_helpers, lambda document: [document],
        out_of=lambda result: None, after_of=lambda document: _doc_after(document))
    for name in ('prepare_rule_update', 'finish_rule_updates'):
        setattr(up, name, _collab(
            name, getattr(up, name),
            lambda ctx, changes, config, base_url='': [ctx.doc, ctx.settings, changes, config, base_url],
            out_of=lambda result: None,
            after_of=lambda ctx, changes, config, base_url='': _ctx_after(ctx)))

    def read(original):
        def recorded(settings, config, base_url=''):
            return _top('read_common_settings', [settings, config, base_url],
                        lambda: original(settings, config, base_url))
        return recorded
    base._shim(cm, 'read_common_settings', "settings, config, base_url=''", 'settings, config, base_url', read)

    def apply(original):
        def recorded(settings, changes, config, base_url=''):
            return _top('apply_common_settings', [settings, changes, config, base_url],
                        lambda: original(settings, changes, config, base_url))
        return recorded
    base._shim(cm, 'apply_common_settings', "settings, changes, config, base_url=''",
               'settings, changes, config, base_url', apply)


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C24_RECORD_DIR')
    if not directory:
        return
    os.makedirs(directory, exist_ok=True)
    worker = os.environ.get('PYTEST_XDIST_WORKER', 'main')
    with open(os.path.join(directory, f'{worker}-{os.getpid()}.jsonl'), 'w') as fh:
        for entry in _RECORDS:
            fh.write(json.dumps(entry, ensure_ascii=False) + '\n')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    base.merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c24_recorded.jsonl.gz'))
