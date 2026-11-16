"""pytest plugin: record the calls the Python test suite makes to C28
(backend/utils/mosdns_archive.py, backend/routes/mosdns.py) for the Go
replay (c28_recorded_test.go).

    rm -rf /tmp/c28rec; C28_RECORD_DIR=/tmp/c28rec PYTHONPATH=.:server/internal/mosdns/testdata \\
        ~/cf-test/.venv/bin/python -m pytest backend -q -n 8 --dist=worksteal -p c28_record -p no:cacheprovider
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/mosdns/testdata/c28_record.py merge /tmp/c28rec

Rows (fn):
- build: build_mosdns_zip(config, yaml_content, downloads, custom_files, base_url, validate_references)
  with MAX_FILES/MAX_TOTAL_BYTES, the answers of _fetch_remote_content (whatever is installed),
  the world before (shared rule_library, shared/rules files, task-status.json, default profile)
  and the rule cache files after; output = the archive entries in order.
- proxy: mosdns_rule_proxy() with the request (args, Authorization, host_url, internal call), the
  answers of get_config, _require_rule_proxy_auth, _local_content_rule / apply_github_proxy_domain /
  _validate_remote_url (only when a test replaced them) and _fetch_remote_content; output =
  status, text or JSON, Cache-Control no-store, X-ConfigFlow-Rule-Cache.
- fetch_remote: the real _fetch_remote_content(url, config_data, raw) with the request_rule answer
  (kwargs, status, Content-Length, body chunks or the exception).
- local_content: the real _local_content_rule(url, config) with request.host_url.
- cached_url: _load_cached_rule_content_for_url(url, config) with the world.
- cache_settings: _cache_settings(data).  path: top-level _path(value).
- auth: the real _require_rule_proxy_auth() with its inputs and the verify_token answer.
- validate_url: the real _validate_remote_url(url).
"""

import functools
import gzip
import hashlib
import io
import json
import os
import sys
import threading
import zipfile
from copy import deepcopy

_RECORDS = []
_TEST = ['']
_LOCAL = threading.local()
_INSTALLED = {}


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
        if isinstance(value, str) and any(0xD800 <= ord(c) <= 0xDFFF for c in value):
            raise Unsupported('surrogate')
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise Unsupported('non-finite float')
        return value
    if isinstance(value, (bytes, bytearray)):
        return {'__bytes__': bytes(value).hex()}
    raise Unsupported(f'unsupported {type(value).__name__}')


def error_spec(exc):
    spec = {'class': type(exc).__name__, 'message': str(exc)}
    status = getattr(getattr(exc, 'response', None), 'status_code', None)
    if isinstance(status, int) and not isinstance(status, bool):
        spec['status'] = status
    import requests
    spec['requests'] = isinstance(exc, requests.RequestException)
    spec['value_error'] = isinstance(exc, ValueError)
    return spec


def _depth():
    return getattr(_LOCAL, 'depth', 0)


def _world():
    from backend.common.config import get_repository
    try:
        repository = get_repository()
        root = repository.data_dir
        shared = repository.get_shared()
        default = repository.default_profile_id()
    except Exception as exc:
        raise Unsupported(f'world: {type(exc).__name__}')
    files = {}
    rules = os.path.join(root, 'shared', 'rules')
    if os.path.isdir(rules):
        for name in sorted(os.listdir(rules)):
            path = os.path.join(rules, name)
            if os.path.isfile(path) and '.tmp' not in name and not name.endswith('.lock'):
                with open(path, 'rb') as fh:
                    files['rules/' + name] = conv(fh.read())
    status = None
    try:
        with open(os.path.join(root, 'task-status.json'), encoding='utf-8') as fh:
            status = fh.read()
    except OSError:
        pass
    return {'rule_library': conv(shared.get('rule_library', [])), 'files': files, 'task_status': status,
            'default_profile': default}


def _rule_files():
    from backend.common.config import get_repository
    root = get_repository().data_dir
    files = {}
    rules = os.path.join(root, 'shared', 'rules')
    if os.path.isdir(rules):
        for name in sorted(os.listdir(rules)):
            path = os.path.join(rules, name)
            if os.path.isfile(path) and '.tmp' not in name and not name.endswith('.lock'):
                with open(path, 'rb') as fh:
                    files['rules/' + name] = conv(fh.read())
    return files


def _finish(entry, unsupported):
    if unsupported:
        _RECORDS.append({'test': _TEST[0], 'fn': entry['fn'], 'unsupported': unsupported[0]})
    else:
        _RECORDS.append(entry)


def _answer_log(log, key, inner):
    """Wrap a collaborator: log [args..., result|error] under log[key]."""
    @functools.wraps(inner)
    def wrapper(*args, **kwargs):
        try:
            result = inner(*args, **kwargs)
        except Exception as exc:
            log.setdefault(key, []).append({'args': _args_of(key, args, kwargs), 'err': error_spec(exc)})
            raise
        log.setdefault(key, []).append({'args': _args_of(key, args, kwargs), 'ok': result})
        return result
    return wrapper


def _args_of(key, args, kwargs):
    if key == 'fetch':
        return [args[0], 'config_data' in kwargs and kwargs['config_data'] is not None, bool(kwargs.get('raw'))]
    if key in ('local', 'github', 'validate'):
        return [args[0]] if args else []
    return []


def _swap(module, names, log):
    saved = {name: getattr(module, name) for name in names}
    for name, key in names.items():
        setattr(module, name, _answer_log(log, key, saved[name]))
    return saved


def _restore(module, saved):
    for name, value in saved.items():
        setattr(module, name, value)


def _install():
    import flask
    import requests
    from backend.routes import mosdns
    from backend.utils import mosdns_archive

    # --- build_mosdns_zip --------------------------------------------------------------
    real_build = mosdns_archive.build_mosdns_zip

    def build_mosdns_zip(config, yaml_content, downloads, custom_files, *, base_url='', validate_references=True):
        if _depth() > 0:
            return real_build(config, yaml_content, downloads, custom_files, base_url=base_url,
                              validate_references=validate_references)
        _LOCAL.depth = 1
        entry = {'test': _TEST[0], 'fn': 'build'}
        unsupported = []
        log = {}
        try:
            entry['args'] = conv([deepcopy(config), yaml_content, list(downloads), list(custom_files), base_url,
                                  validate_references])
            entry['limits'] = [mosdns_archive.MAX_FILES, mosdns_archive.MAX_TOTAL_BYTES]
            entry['world'] = _world()
        except Exception as exc:
            unsupported.append(str(exc))
        saved = _swap(mosdns, {'_fetch_remote_content': 'fetch'}, log)
        try:
            result = real_build(config, yaml_content, downloads, custom_files, base_url=base_url,
                                validate_references=validate_references)
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            data = result.getvalue()
            with zipfile.ZipFile(io.BytesIO(data)) as archive:
                entry['out'] = [[name, conv(archive.read(name))] for name in archive.namelist()]
            return result
        finally:
            _restore(mosdns, saved)
            _LOCAL.depth = 0
            try:
                entry['answers'] = conv(log)
                entry['after'] = _rule_files()
            except Exception as exc:
                unsupported.append(f'collaborator: {exc}')
            _finish(entry, unsupported)
    mosdns_archive.build_mosdns_zip = build_mosdns_zip
    for module_name in ('backend.routes.generate', 'backend.utils.oxidns_archive'):
        module = sys.modules.get(module_name) or __import__(module_name, fromlist=['x'])
        if getattr(module, 'build_mosdns_zip', None) is real_build:
            module.build_mosdns_zip = build_mosdns_zip

    # --- leaf functions (recorded whenever the real one runs) ---------------------------
    real_fetch = mosdns._fetch_remote_content

    class RecordedResponse:
        def __init__(self, response, note):
            self._response = response
            self._note = note
            self.status_code = response.status_code
            self.headers = response.headers

        def iter_content(self, size):
            chunks = self._note.setdefault('chunks', [])
            try:
                for chunk in self._response.iter_content(size):
                    chunks.append(conv(chunk))
                    yield chunk
            except Exception as exc:
                self._note['iter_err'] = error_spec(exc)
                raise

        def close(self):
            self._response.close()

    class RequestRuleProxy:
        def __init__(self, inner):
            self.inner = inner

        def __call__(self, url, **kwargs):
            note = getattr(_LOCAL, 'fetch_note', None)
            if note is None:
                return self.inner(url, **kwargs)
            note['request'] = {'url': url, 'timeout': list(kwargs.get('timeout') or []),
                               'stream': kwargs.get('stream'), 'max_redirects': kwargs.get('max_redirects'),
                               'config': kwargs.get('config_data') is not None}
            try:
                response = self.inner(url, **kwargs)
            except Exception as exc:
                note['err'] = error_spec(exc)
                raise
            length = response.headers.get('Content-Length') if response.headers is not None else None
            note['response'] = {'status': response.status_code, 'content_length': length}
            return RecordedResponse(response, note)

    @functools.wraps(real_fetch)
    def fetch_remote(url, config_data=None, *, raw=False):
        if not isinstance(mosdns.request_rule, RequestRuleProxy):
            mosdns.request_rule = RequestRuleProxy(mosdns.request_rule)
        previous = getattr(_LOCAL, 'fetch_note', None)
        note = {}
        _LOCAL.fetch_note = note
        entry = {'test': _TEST[0], 'fn': 'fetch_remote'}
        unsupported = []
        try:
            entry['args'] = [url, conv(config_data.get('system_config') if isinstance(config_data, dict) else None),
                             config_data is not None, raw]
            result = real_fetch(url, config_data=config_data, raw=raw)
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            entry['out'] = conv(result)
            return result
        finally:
            _LOCAL.fetch_note = previous
            try:
                entry['note'] = conv(note)
            except Exception as exc:
                unsupported.append(str(exc))
            if 'response' not in note and 'err' not in note:
                unsupported.append('request_rule not reached')
            _finish(entry, unsupported)
    mosdns._fetch_remote_content = fetch_remote
    _INSTALLED['_fetch_remote_content'] = fetch_remote

    def leaf(name, fn, args_of, needs_request=False):
        real = getattr(mosdns, name)

        @functools.wraps(real)
        def wrapper(*args, **kwargs):
            entry = {'test': _TEST[0], 'fn': fn}
            unsupported = []
            try:
                entry['args'] = conv(args_of(*args, **kwargs))
            except Exception as exc:
                unsupported.append(str(exc))
            try:
                result = real(*args, **kwargs)
            except Exception as exc:
                entry['err'] = error_spec(exc)
                raise
            else:
                try:
                    entry['out'] = conv(result)
                except Exception as exc:
                    unsupported.append(str(exc))
                return result
            finally:
                _finish(entry, unsupported)
        setattr(mosdns, name, wrapper)
        _INSTALLED[name] = wrapper

    def local_args(url, config):
        if not flask.has_request_context():
            raise Unsupported('no request context')
        return [url, deepcopy(config), flask.request.host_url]
    leaf('_local_content_rule', 'local_content', local_args)
    leaf('_cache_settings', 'cache_settings', lambda data: [deepcopy(data)])
    leaf('_validate_remote_url', 'validate_url', lambda url: [url])

    real_cached = mosdns._load_cached_rule_content_for_url

    @functools.wraps(real_cached)
    def cached_url(original_url, config_data=None):
        entry = {'test': _TEST[0], 'fn': 'cached_url'}
        unsupported = []
        try:
            if config_data is None:
                raise Unsupported('config from get_config')
            entry['args'] = conv([original_url, deepcopy(config_data)])
            entry['world'] = _world()
        except Exception as exc:
            unsupported.append(str(exc))
        try:
            result = real_cached(original_url, config_data)
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            entry['out'] = result
            return result
        finally:
            _finish(entry, unsupported)
    mosdns._load_cached_rule_content_for_url = cached_url
    _INSTALLED['_load_cached_rule_content_for_url'] = cached_url

    real_path = mosdns_archive._path

    def path(value):
        if _depth() > 0:
            return real_path(value)
        entry = {'test': _TEST[0], 'fn': 'path'}
        unsupported = []
        try:
            entry['args'] = conv([value])
        except Exception as exc:
            unsupported.append(str(exc))
        try:
            result = real_path(value)
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            entry['out'] = result
            return result
        finally:
            _finish(entry, unsupported)
    mosdns_archive._path = path
    from backend.utils import oxidns_archive
    if oxidns_archive._path is real_path:
        oxidns_archive._path = path

    # --- _require_rule_proxy_auth ---------------------------------------------------------
    real_auth = mosdns._require_rule_proxy_auth

    @functools.wraps(real_auth)
    def require_auth():
        from backend.common import auth as auth_module
        from backend.common.config import get_repository
        from backend.common.internal_call import is_internal_call
        entry = {'test': _TEST[0], 'fn': 'auth'}
        unsupported = []
        verified = []
        real_verify = auth_module.verify_token

        def verify(token):
            result = real_verify(token)
            verified.append(deepcopy(result))
            return result
        try:
            entry['args'] = {
                'internal': is_internal_call(),
                'system_config': conv(mosdns.get_config().get('system_config', {})),
                'authorization': flask.request.headers.get('Authorization'),
                'token': flask.request.args.get('token'),
                'internal_tokens': sorted(get_repository().rule_proxy_tokens_for_sanitization()),
            }
        except Exception as exc:
            unsupported.append(f'inputs: {type(exc).__name__}')
        auth_module.verify_token = verify
        try:
            result = real_auth()
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            entry['out'] = result
            return result
        finally:
            auth_module.verify_token = real_verify
            try:
                entry['verified'] = conv(verified)
            except Exception as exc:
                unsupported.append(str(exc))
            _finish(entry, unsupported)
    mosdns._require_rule_proxy_auth = require_auth
    _INSTALLED['_require_rule_proxy_auth'] = require_auth

    # --- mosdns_rule_proxy ----------------------------------------------------------------
    real_proxy = mosdns.mosdns_rule_proxy

    @functools.wraps(real_proxy)
    def rule_proxy(*args, **kwargs):
        if _depth() > 0:
            return real_proxy(*args, **kwargs)
        _LOCAL.depth = 1
        from backend.common.internal_call import is_internal_call
        entry = {'test': _TEST[0], 'fn': 'proxy'}
        unsupported = []
        log = {}
        names = {'get_config': 'config', '_require_rule_proxy_auth': 'auth', '_fetch_remote_content': 'fetch'}
        patched = {}
        for name, key in (('_local_content_rule', 'local'), ('apply_github_proxy_domain', 'github'),
                          ('_validate_remote_url', 'validate')):
            installed = _INSTALLED.get(name, _ORIGINAL.get(name))
            if getattr(mosdns, name) is not installed:
                names[name] = key
                patched[key] = True
        try:
            entry['request'] = {
                'args': {key: flask.request.args.get(key) for key in ('url', 'part', 'source_id', 'token')},
                'authorization': flask.request.headers.get('Authorization'),
                'host_url': flask.request.host_url, 'internal': is_internal_call(),
            }
            entry['patched'] = patched
            entry['world'] = _world()
        except Exception as exc:
            unsupported.append(str(exc))
        saved = _swap(mosdns, names, log)
        try:
            result = real_proxy(*args, **kwargs)
        except Exception as exc:
            entry['err'] = error_spec(exc)
            raise
        else:
            try:
                entry['out'] = _proxy_out(result)
            except Exception as exc:
                unsupported.append(f'out: {exc}')
            return result
        finally:
            _restore(mosdns, saved)
            _LOCAL.depth = 0
            try:
                entry['answers'] = conv(log)
                entry['after'] = _rule_files()
            except Exception as exc:
                unsupported.append(f'collaborator: {exc}')
            _finish(entry, unsupported)
    mosdns.mosdns_rule_proxy = rule_proxy

    real_add = flask.Flask.add_url_rule

    def add_url_rule(self, rule, endpoint=None, view_func=None, *args, **kwargs):
        if endpoint == 'mosdns.mosdns_rule_proxy' and view_func is real_proxy:
            view_func = rule_proxy
        return real_add(self, rule, endpoint, view_func, *args, **kwargs)
    flask.Flask.add_url_rule = add_url_rule


_ORIGINAL = {}


def _proxy_out(result):
    from flask import Response
    status, headers, body = 200, {}, result
    if isinstance(result, tuple):
        body = result[0]
        if len(result) > 1:
            status = result[1]
        if len(result) > 2:
            headers = dict(result[2])
    out = {'status': status}
    if isinstance(body, Response):
        out['json'] = conv(body.get_json())
        headers = {**dict(body.headers), **headers}
    else:
        out['text'] = body
    out['no_store'] = headers.get('Cache-Control') == 'no-store'
    out['stale'] = headers.get('X-ConfigFlow-Rule-Cache') == 'stale'
    return out


def pytest_configure(config):
    from backend.routes import mosdns
    import backend.converters.mihomo  # noqa: F401 (apply_github_proxy_domain import)
    _ORIGINAL['apply_github_proxy_domain'] = mosdns.apply_github_proxy_domain
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C28_RECORD_DIR')
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
    unsupported, counts = {}, {}
    for e in rows:
        if 'unsupported' in e:
            unsupported[e['unsupported']] = unsupported.get(e['unsupported'], 0) + 1
        counts[e['fn']] = counts.get(e['fn'], 0) + 1
    print(f'{len(rows)} distinct calls from {tests} tests in {files} files (unsupported {unsupported}) {counts} -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c28_recorded.jsonl.gz'))
