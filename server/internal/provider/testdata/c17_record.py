"""pytest plugin: record every top-level call of provider delivery (C17,
backend/utils/provider_delivery.py) the Python test suite makes, for the Go
replay test (recorded_test.go).

    rm -rf /tmp/c17rec; C17_RECORD_DIR=/tmp/c17rec PYTHONPATH=.:server/internal/provider/testdata \\
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 -p c17_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/c17_record.py merge /tmp/c17rec

Recorded (calls made inside another recorded call are part of it):
validate_proxy_shapes, parse_provider_proxies, DeliverySnapshot.capture,
.has_chains, .validate, validate_rendered_bundle, prepare_subscription_provider,
prepare_provider_bundle, commit_provider_bundle, commit_cache_updates.

Collaborators recorded per call so Go can answer them the same way:
deployment_locations.preparation_context / source_error (A11, keyed by their
arguments), every fetch the call makes (label, id, url → result or
exception), Sub-Store convert_proxy_string answers, generate_aggregation_provider
answers (with the fetches it made), uuid4 node ids, the subscription caches and
download caches before the call (and the files after commits), and the
wall-clock window of the call (timestamps inside it are masked on both sides).
"""

import gzip
import hashlib
import json
import os
import sys
import threading
from copy import deepcopy
from datetime import datetime, timezone

_RECORDS = []
_TEST = ['']


class _Local(threading.local):
    depth = 0
    call = None  # the collaborator log of the running top-level call


_STATE = _Local()
_LOCK = threading.Lock()


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
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unsupported('non-finite float')
        return value
    raise Unsupported(f'unsupported {type(value).__name__}')


def _now():
    from backend.utils import subscription_refresh
    try:
        return subscription_refresh.datetime.now(timezone.utc).isoformat()
    except Exception:
        return datetime.now(timezone.utc).isoformat()


def _files(prefixes):
    from backend.common.config import get_repository
    out = {}
    try:
        root = get_repository().data_dir
    except Exception:
        return out
    for prefix in prefixes:
        base = os.path.join(root, prefix)
        if not os.path.isdir(base):
            continue
        for dirpath, _dirs, names in os.walk(base):
            for name in names:
                if name.endswith('.lock') or name.endswith('.bak') or '.tmp' in name:
                    continue
                path = os.path.join(dirpath, name)
                try:
                    with open(path, 'rb') as fh:
                        out[os.path.relpath(path, root).replace(os.sep, '/')] = fh.read().decode('utf-8')
                except (OSError, UnicodeDecodeError):
                    out[os.path.relpath(path, root)] = None
    return out


WORLD = ('shared/subscribes', 'shared/subscription-downloads')


def _result_entry(result):
    try:
        text, source = result
    except Exception:
        raise Unsupported('fetch result is not a pair')
    return {'text': text, 'source': source, 'traffic': conv(getattr(result, 'traffic', None)),
            'fetched_at': getattr(result, 'fetched_at', None), 'tuple': not hasattr(result, 'fetched_at')}


def _error_entry(exc):
    from backend.utils.strategy_references import StrategyReferenceError
    return {'raise': type(exc).__name__, 'message': str(exc),
            'strategy': isinstance(exc, StrategyReferenceError)}


def _recording_fetch(label, fetch):
    # Bound to the running call's log: aggregation providers fetch on worker threads.
    log = _STATE.call

    def recorded(sub_id, url, *args, **kwargs):
        try:
            result = fetch(sub_id, url, *args, **kwargs)
        except Exception as exc:
            if log is not None:
                with _LOCK:
                    log['fetch'].append([label, sub_id, url, _error_entry(exc)])
            raise
        if log is not None:
            with _LOCK:
                log['fetch'].append([label, sub_id, url, _result_entry(result)])
        return result
    return recorded


def _record(fn, args, call, out=lambda v: v, world=False, files_after=None):
    if _STATE.depth > 0:
        return call()
    _STATE.depth += 1
    entry = {'test': _TEST[0], 'fn': fn}
    unsupported = []
    log = {'fetch': [], 'convert': [], 'agg': [], 'ids': [], 'locations': {}, 'progress': []}
    _STATE.call = log
    try:
        entry['args'] = conv(deepcopy(args))
    except Exception as exc:
        unsupported.append(str(exc))
    if world:
        entry['files'] = _files(WORLD + (tuple(files_after(args)) if files_after is not None else ()))
        entry['now'] = [_now()]
    try:
        result = call()
    except Exception as exc:
        entry['err'] = [type(exc).__name__, str(exc)]
        raise
    else:
        try:
            entry['out'] = conv(out(result))
        except Exception as exc:
            unsupported.append(str(exc))
        return result
    finally:
        _STATE.depth -= 1
        _STATE.call = None
        try:
            if world:
                entry['now'].append(_now())
            if files_after is not None:
                entry['files_after'] = _files(files_after(args))
            for key in ('fetch', 'convert', 'agg', 'ids', 'progress'):
                if log[key]:
                    entry[key] = conv(log[key])
            if log['locations']:
                entry['locations'] = log['locations']
            if log.get('unsupported'):
                unsupported.append(log['unsupported'])
        except Exception as exc:
            unsupported.append(f'collaborator: {exc}')
        if unsupported:
            _RECORDS.append({'test': _TEST[0], 'fn': fn, 'unsupported': unsupported[0]})
        else:
            _RECORDS.append(entry)


def _location_key(*parts):
    return json.dumps(parts, ensure_ascii=False)


def _cause_key(cause):
    if cause is None:
        return None
    mark = getattr(cause, 'problem_mark', None)
    return [type(cause).__name__, getattr(cause, 'context', None) or '', getattr(cause, 'problem', None) or '',
            [mark.line, mark.column] if mark is not None else None]


def _snap(snapshot):
    return [snapshot.main_json, sorted(snapshot.chain_names, key=repr)]


def _install():
    import uuid
    from backend.agents import deployment_locations as dl
    from backend.routes import aggregations as agg_route
    from backend.routes import subscriptions as sub_route
    from backend.utils import provider_delivery as pd
    from backend.utils import sub_store_client

    real_ctx, real_src = dl.preparation_context, dl.source_error

    def preparation_context(exc, *, name='', path='', kind='规则集'):
        result = real_ctx(exc, name=name, path=path, kind=kind)
        log = _STATE.call
        if log is not None:
            log['locations'][_location_key('ctx', str(exc), str(name) if name else '', str(path) if path else '',
                                           kind)] = str(result)
        return result

    def source_error(message, text, *, line=None, column=None, payload_index=None, cause=None, source_label=''):
        result = real_src(message, text, line=line, column=column, payload_index=payload_index, cause=cause,
                          source_label=source_label)
        log = _STATE.call
        if log is not None:
            if payload_index is not None:
                log['unsupported'] = 'source_error payload_index'
            log['locations'][_location_key('src', message, text, line, column, _cause_key(cause),
                                           source_label)] = str(result)
        return result
    dl.preparation_context, dl.source_error = preparation_context, source_error

    real_convert = sub_store_client.convert_proxy_string

    def convert_proxy_string(proxy_string, target='ClashMeta'):
        result = real_convert(proxy_string, target)
        log = _STATE.call
        if log is not None:
            log['convert'].append([proxy_string, target, deepcopy(result)])
        return result
    sub_store_client.convert_proxy_string = convert_proxy_string

    real_uuid4 = uuid.uuid4

    def uuid4():
        value = real_uuid4()
        log = _STATE.call
        if log is not None and sys._getframe(1).f_code.co_name == '_prepare_subscription_provider':
            log['ids'].append(f'node_{value.hex[:8]}')
        return value
    uuid.uuid4 = uuid4

    def recording_aggregation(real_agg):
      def generate_aggregation_provider(agg, *args, subscription_fetch=None, **kwargs):
          log = _STATE.call
          if log is None or subscription_fetch is None:
              return real_agg(agg, *args, subscription_fetch=subscription_fetch, **kwargs)
          calls = []

          def fetch(sub_id, url):
              with _LOCK:
                  calls.append([sub_id, url])
              return subscription_fetch(sub_id, url)
          try:
              value = real_agg(agg, *args, subscription_fetch=fetch, **kwargs)
          except Exception as exc:
              log['agg'].append([agg.get('id'), calls, _error_entry(exc)])
              raise
          log['agg'].append([agg.get('id'), calls, deepcopy(value)])
          return value
      return generate_aggregation_provider

    # validate_proxy_shapes / parse_provider_proxies / validate_rendered_bundle
    real_shapes = pd.validate_proxy_shapes
    pd.validate_proxy_shapes = lambda proxies: _record('shapes', [proxies], lambda: real_shapes(proxies))

    real_parse = pd.parse_provider_proxies

    def parse_provider_proxies(text, *, name='', path=''):
        return _record('parse', [text, name, path], lambda: real_parse(text, name=name, path=path))
    pd.parse_provider_proxies = parse_provider_proxies

    real_bundle_validate = pd.validate_rendered_bundle

    def validate_rendered_bundle(snapshot, providers):
        return _record('rendered_bundle', [_snap(snapshot), providers],
                       lambda: real_bundle_validate(snapshot, providers))
    pd.validate_rendered_bundle = validate_rendered_bundle

    # DeliverySnapshot
    cls = pd.DeliverySnapshot
    real_capture = cls.capture.__func__

    def capture(klass, profile_id, main, *, profile=None):
        return _record('capture', [profile_id, main, profile],
                       lambda: real_capture(klass, profile_id, main, profile=profile),
                       lambda s: [s.profile_id, json.loads(s.main_json), sorted(s.chain_names, key=repr)])
    cls.capture = classmethod(capture)
    real_has_chains = cls.has_chains.fget
    cls.has_chains = property(lambda self: _record('has_chains', [_snap(self)], lambda: real_has_chains(self)))
    real_validate = cls.validate

    def validate(self, proxies, *, force_chains=False, provider_name=None):
        return _record('validate', [_snap(self), proxies, force_chains, provider_name],
                       lambda: real_validate(self, proxies, force_chains=force_chains, provider_name=provider_name))
    cls.validate = validate

    # prepare_subscription_provider
    real_prepare = pd.prepare_subscription_provider

    def prepare_subscription_provider(sub, config, snapshot, *, fetch=None, allow_transport_fallback=False,
                                      source_path='', source_prepared=False):
        if _STATE.depth > 0:
            return real_prepare(sub, config, snapshot, fetch=fetch, allow_transport_fallback=allow_transport_fallback,
                                source_path=source_path, source_prepared=source_prepared)
        effective = fetch or sub_store_client.get_subscription_proxies_yaml
        opts = {'allow_transport_fallback': allow_transport_fallback, 'source_path': source_path,
                'source_prepared': source_prepared}
        if not isinstance(snapshot, pd.DeliverySnapshot):
            _RECORDS.append({'test': _TEST[0], 'fn': 'prepare_sub', 'unsupported': 'snapshot is a test double'})
            return real_prepare(sub, config, snapshot, fetch=fetch, allow_transport_fallback=allow_transport_fallback,
                                source_path=source_path, source_prepared=source_prepared)
        snap = _snap(snapshot)
        return _record('prepare_sub', [sub, snap, opts],
                       lambda: real_prepare(sub, config, snapshot, fetch=_recording_fetch('fetch', effective),
                                            allow_transport_fallback=allow_transport_fallback,
                                            source_path=source_path, source_prepared=source_prepared),
                       world=True)
    pd.prepare_subscription_provider = prepare_subscription_provider

    # prepare_provider_bundle
    real_bundle = pd.prepare_provider_bundle

    def prepare_provider_bundle(config, main, *, requested=None, render_all=False, allow_transport_fallback=False,
                                prepared=None, subscription_fetch=None, progress=None):
        if _STATE.depth > 0:
            return real_bundle(config, main, requested=requested, render_all=render_all,
                               allow_transport_fallback=allow_transport_fallback, prepared=prepared,
                               subscription_fetch=subscription_fetch, progress=progress)
        opts = {'requested': list(requested) if requested else None, 'render_all': render_all,
                'allow_transport_fallback': allow_transport_fallback, 'prepared': prepared,
                'subscription_fetch': subscription_fetch is not None, 'progress': progress is not None}

        def run():
            saved = (sub_route.get_subscription_proxies_yaml, agg_route.get_subscription_proxies_yaml,
                     agg_route.generate_aggregation_provider)
            sub_route.get_subscription_proxies_yaml = _recording_fetch('subscriptions', saved[0])
            agg_route.get_subscription_proxies_yaml = _recording_fetch('aggregations', saved[1])
            agg_route.generate_aggregation_provider = recording_aggregation(saved[2])
            log = _STATE.call

            def recorded_progress(message, force=False):
                log['progress'].append([message, force])
                return progress(message, force=force) if force else progress(message)
            try:
                return real_bundle(config, main, requested=requested, render_all=render_all,
                                   allow_transport_fallback=allow_transport_fallback, prepared=prepared,
                                   subscription_fetch=_recording_fetch('option', subscription_fetch)
                                   if subscription_fetch is not None else None,
                                   progress=recorded_progress if progress is not None else None)
            finally:
                (sub_route.get_subscription_proxies_yaml, agg_route.get_subscription_proxies_yaml,
                 agg_route.generate_aggregation_provider) = saved
        return _record('bundle', [config, main, opts], run, world=True)
    pd.prepare_provider_bundle = prepare_provider_bundle

    def profile_files(profile_id):
        return ['shared/subscribes', f'profiles/{profile_id}/providers']

    real_commit = pd.commit_provider_bundle

    def commit_provider_bundle(profile_id, bundle):
        # A test may replace commit_cache_updates: the replay then commits only the artifacts.
        patched = pd.commit_cache_updates is not commit_cache_updates
        return _record('commit_bundle', [profile_id, bundle, patched], lambda: real_commit(profile_id, bundle),
                       world=True, files_after=lambda args: profile_files(args[0]))
    pd.commit_provider_bundle = commit_provider_bundle

    real_commit_updates = pd.commit_cache_updates

    def commit_cache_updates(updates):
        updates = list(updates)
        return _record('commit_updates', [updates], lambda: real_commit_updates(updates),
                       world=True, files_after=lambda args: ['shared/subscribes'])
    pd.commit_cache_updates = commit_cache_updates


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C17_RECORD_DIR')
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
                key.pop('now', None)
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
    unsupported = {}
    for e in rows:
        if 'unsupported' in e:
            unsupported[e['unsupported']] = unsupported.get(e['unsupported'], 0) + 1
    counts = {}
    for e in rows:
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    print(f'{len(rows)} distinct calls from {tests} tests in {files} files (unsupported {unsupported}) {counts} -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'recorded.jsonl.gz'))
