"""pytest plugin: record every generate_mihomo_config / get_mihomo_*_downloads /
validate_base_settings call the Python test suite makes, for the Go replay test
(mihomo_recorded_test.go).

    C18_RECORD_DIR=/tmp/c18rec PYTHONPATH=.:server/internal/convert/testdata \
        ~/cf-test/.venv/bin/python -m pytest -p c18_record backend/test_x.py ...
    PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/c18_record.py merge /tmp/c18rec

Each call records the test id, the arguments, what the collaborators of other
packages answered during the call (Sub-Store conversions, subscription caches,
download-cache reads, provider preflight, the Linux gateway step, the gateway
setting, fake_ip_network, mihomo_dns_loop), the result or exception, the
mutated profile and the report. The hosts-index memo is cleared per call so
every cache read is observed.
"""

import datetime
import gzip
import hashlib
import json
import os
import sys
import types
from copy import deepcopy

_RECORDS = []
_TEST = ['']
_DEPTH = [0]


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
    if isinstance(value, bool) or value is None or isinstance(value, (int, str)):
        return value
    if isinstance(value, float):
        if value != value or value in (float('inf'), float('-inf')):
            raise TypeError('non-finite float')
        return value
    if isinstance(value, bytes):
        return {'__bytes__': value.hex()}
    if isinstance(value, (datetime.date, datetime.datetime)):
        return {'__date__': str(value)}
    raise TypeError(f'unsupported {type(value).__name__}')


def _error(exc):
    return [type(exc).__name__, str(exc)]


def _copy_function(f):
    g = types.FunctionType(f.__code__, f.__globals__, f.__name__, f.__defaults__, f.__closure__)
    g.__kwdefaults__ = f.__kwdefaults__
    return g


class _Recorder:
    """Collaborator answers during one top-level call."""

    def __init__(self):
        self.conversions = {}
        self.caches = {}
        self.downloads = {}
        self.preflight = None
        self.gateway = []
        self.gateway_enabled = []
        self.fake_ip = {}
        self.dns_loop = []
        self.unsupported = None


_ACTIVE = [None]


def _patch(module, name, make):
    original = getattr(module, name)
    setattr(module, name, make(original))
    return lambda: setattr(module, name, original)


def _install_collaborators(rec):
    from backend.utils import sub_store_client, subscription_cache, subscription_hosts, provider_delivery
    from backend.common import linux_gateway, gateway_onboarding
    import backend.common.config as config_module
    from backend.utils.subscription_refresh import _download_cache_path  # noqa: F401
    subscription_hosts._index.clear()
    restores = []

    def conversion(original):
        def wrapped(proxy_string, *args, **kwargs):
            result = original(proxy_string, *args, **kwargs)
            try:
                rec.conversions.setdefault(proxy_string, conv(result))
            except TypeError as exc:
                rec.unsupported = str(exc)
            return deepcopy(result)
        return wrapped
    restores.append(_patch(sub_store_client, 'convert_proxy_string', conversion))

    def load(original):
        def wrapped(sub_id, *args, **kwargs):
            result = original(sub_id, *args, **kwargs)
            try:
                if isinstance(sub_id, str):
                    rec.caches.setdefault(sub_id, conv(result))
                else:
                    rec.unsupported = 'non-string subscription id'
            except TypeError as exc:
                rec.unsupported = str(exc)
            return result
        return wrapped
    restores.append(_patch(subscription_cache, 'load_subscription_cache', load))

    def repository(original):
        def wrapped(*args, **kwargs):
            repo = original(*args, **kwargs)
            return _RecordingRepository(repo, rec)
        return wrapped
    restores.append(_patch(config_module, 'get_repository', repository))

    def preflight(original):
        def wrapped(*args, **kwargs):
            try:
                result = original(*args, **kwargs)
            except Exception as exc:
                rec.preflight = _error(exc)
                raise
            rec.preflight = rec.preflight or 'ok'
            return result
        return wrapped
    restores.append(_patch(provider_delivery, 'prepare_provider_bundle', preflight))

    def gateway(original):
        def wrapped(mihomo, config, base_url=''):
            entry = {'before': conv(mihomo), 'base_url': base_url}
            try:
                original(mihomo, config, base_url)
            except Exception as exc:
                entry['err'] = _error(exc)
                rec.gateway.append(entry)
                raise
            entry['after'] = conv(mihomo)
            rec.gateway.append(entry)
        return wrapped
    restores.append(_patch(linux_gateway, 'apply_linux_gateway', gateway))

    def gateway_settings(original):
        def wrapped(config):
            try:
                result = original(config)
            except Exception as exc:
                rec.gateway_enabled.append({'err': _error(exc)})
                raise
            rec.gateway_enabled.append({'out': bool(result.get('enabled'))})
            return result
        return wrapped
    restores.append(_patch(linux_gateway, 'gateway_settings', gateway_settings))

    def fake_ip(original):
        def wrapped(document):
            try:
                value = document['dns']['fake-ip-range']
                if not isinstance(value, str) or len(document) != 1 or len(document['dns']) != 1:
                    raise KeyError
            except Exception:  # noqa: BLE001 - not the base-settings call shape
                return original(document)
            try:
                result = original(document)
            except Exception as exc:
                rec.fake_ip[value] = _error(exc)
                raise
            rec.fake_ip[value] = None
            return result
        return wrapped
    restores.append(_patch(linux_gateway, 'fake_ip_network', fake_ip))

    def dns_loop(original):
        def wrapped(dns, host_addresses=()):
            result = original(dns, host_addresses)
            rec.dns_loop.append({'dns': conv(dns), 'hosts': sorted(str(h) for h in host_addresses),
                                 'out': list(result) if result else None})
            return result
        return wrapped
    restores.append(_patch(gateway_onboarding, 'mihomo_dns_loop', dns_loop))
    return restores


class _RecordingRepository:
    def __init__(self, repo, rec):
        self._repo = repo
        self._rec = rec

    def read_shared_json(self, path, *args, **kwargs):
        try:
            result = self._repo.read_shared_json(path, *args, **kwargs)
        except Exception as exc:
            if 'subscription-downloads' in str(path) or 'download' in str(path):
                self._rec.downloads.setdefault(path, None)
            raise
        if 'download' in str(path):
            try:
                self._rec.downloads.setdefault(path, conv(result))
            except TypeError as exc:
                self._rec.unsupported = str(exc)
        return result

    def __getattr__(self, name):
        return getattr(self._repo, name)


def _record_call(fn, args, call, mutated_ref=None, report_ref=None):
    if _DEPTH[0] > 0:
        return call()
    rec = _Recorder()
    _ACTIVE[0] = rec
    _DEPTH[0] += 1
    try:
        restores = _install_collaborators(rec)
    except Exception:
        _DEPTH[0] -= 1
        raise
    entry = {'test': _TEST[0], 'fn': fn}
    from backend.converters import mihomo
    # Conversions reused from an outer node_conversions scope never reach Sub-Store.
    scope = getattr(mihomo._node_conversions, 'scope', None)
    if scope is not None and scope[1]:
        for text, proxy in scope[0].items():
            try:
                rec.conversions.setdefault(text, conv(proxy))
            except TypeError as exc:
                rec.unsupported = str(exc)
    if mihomo.convert_node_to_mihomo is not _ORIGINAL['convert_node_to_mihomo']:
        entry['patched'] = 'convert_node_to_mihomo'
    try:
        entry['args'] = conv(args)
    except TypeError as exc:
        rec.unsupported = str(exc)
    try:
        result = call()
    except Exception as exc:
        entry['err'] = _error(exc)
        raise
    else:
        try:
            entry['out'] = conv(result)
        except TypeError as exc:
            rec.unsupported = str(exc)
        return result
    finally:
        for restore in reversed(restores):
            restore()
        _DEPTH[0] -= 1
        _ACTIVE[0] = None
        try:
            if mutated_ref is not None:
                entry['mutated'] = conv(mutated_ref)
            if report_ref is not None:
                entry['report'] = conv(report_ref)
        except TypeError as exc:
            rec.unsupported = str(exc)
        entry['env'] = {'conversions': rec.conversions, 'caches': rec.caches, 'downloads': rec.downloads,
                        'preflight': rec.preflight, 'gateway': rec.gateway, 'gateway_enabled': rec.gateway_enabled,
                        'fake_ip': rec.fake_ip, 'dns_loop': rec.dns_loop}
        if rec.unsupported is None:
            _RECORDS.append(entry)


_ORIGINAL = {}


def _install():
    from backend.converters import mihomo
    from backend.common import mihomo_base_settings as mbs
    _ORIGINAL['convert_node_to_mihomo'] = mihomo.convert_node_to_mihomo

    generate = mihomo.generate_mihomo_config
    original_generate = _copy_function(generate)

    def recorded_generate(config_data, base_url='', sync_lan_hosts=False, *, preflight_providers=True,
                          region_probe=False, report=None, mihomo_core_version=None, for_agent=False):
        kwargs = {'base_url': base_url, 'sync_lan_hosts': sync_lan_hosts, 'preflight_providers': preflight_providers,
                  'region_probe': region_probe, 'report': report is not None,
                  'mihomo_core_version': mihomo_core_version, 'for_agent': for_agent}
        own_report = {} if report is not None else None

        def call():
            result = original_generate(config_data, base_url, sync_lan_hosts, preflight_providers=preflight_providers,
                                       region_probe=region_probe, report=own_report, mihomo_core_version=mihomo_core_version,
                                       for_agent=for_agent)
            return result
        before = deepcopy(config_data)
        try:
            return _record_call('generate', {'profile': before, **kwargs}, call, mutated_ref=config_data,
                                report_ref=own_report)
        finally:
            if report is not None and own_report is not None:
                report.update(own_report)

    # Rebind the code object so names imported with `from ... import` see the recorder too.
    mihomo._c18_recorded_generate = recorded_generate
    shim = compile('def generate_mihomo_config(config_data, base_url="", sync_lan_hosts=False, *, preflight_providers=True,'
                   ' region_probe=False, report=None, mihomo_core_version=None, for_agent=False):\n'
                   '    return _c18_recorded_generate(config_data, base_url, sync_lan_hosts,'
                   ' preflight_providers=preflight_providers, region_probe=region_probe, report=report,'
                   ' mihomo_core_version=mihomo_core_version, for_agent=for_agent)\n', 'c18_record', 'exec')
    namespace = {}
    exec(shim, mihomo.__dict__, namespace)
    generate.__code__ = namespace['generate_mihomo_config'].__code__

    ruleset = mihomo.get_mihomo_ruleset_downloads
    original_ruleset = _copy_function(ruleset)

    def recorded_ruleset(config_data, base_url=''):
        return _record_call('ruleset_downloads', [deepcopy(config_data), base_url],
                            lambda: original_ruleset(config_data, base_url))
    mihomo._c18_recorded_ruleset = recorded_ruleset
    namespace = {}
    exec(compile('def get_mihomo_ruleset_downloads(config_data, base_url=""):\n'
                 '    return _c18_recorded_ruleset(config_data, base_url)\n', 'c18_record', 'exec'), mihomo.__dict__, namespace)
    ruleset.__code__ = namespace['get_mihomo_ruleset_downloads'].__code__

    validate = mbs.validate_base_settings
    original_validate = _copy_function(validate)

    def recorded_validate(document, previous=None, *, reserved_ports=None, local_addresses=()):
        reserved = {str(k): v for k, v in (reserved_ports or {}).items()} if isinstance(reserved_ports, dict) else reserved_ports
        return _record_call('validate_base_settings',
                            [deepcopy(document), deepcopy(previous), reserved, list(local_addresses or ())],
                            lambda: original_validate(document, previous, reserved_ports=reserved_ports,
                                                      local_addresses=local_addresses))
    mbs._c18_recorded_validate = recorded_validate
    namespace = {}
    exec(compile('def validate_base_settings(document, previous=None, *, reserved_ports=None, local_addresses=()):\n'
                 '    return _c18_recorded_validate(document, previous, reserved_ports=reserved_ports,'
                 ' local_addresses=local_addresses)\n', 'c18_record', 'exec'), mbs.__dict__, namespace)
    validate.__code__ = namespace['validate_base_settings'].__code__


def pytest_configure(config):
    _install()


def pytest_runtest_setup(item):
    _TEST[0] = item.nodeid


def pytest_unconfigure(config):
    directory = os.environ.get('C18_RECORD_DIR')
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
    rows.sort(key=lambda e: e['test'])
    data = '\n'.join(json.dumps(e, ensure_ascii=False) for e in rows) + '\n'
    with open(output, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    tests = len({e['test'] for e in rows})
    print(f'{len(rows)} distinct calls from {tests} tests -> {output}')


if __name__ == '__main__' and len(sys.argv) >= 3 and sys.argv[1] == 'merge':
    merge(sys.argv[2], os.path.join(os.path.dirname(os.path.abspath(__file__)), 'mihomo_recorded.jsonl.gz'))
