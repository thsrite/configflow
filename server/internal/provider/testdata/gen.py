"""Reference corpus for internal/provider from the real Python modules.

Run on the test server from the repository root:
  PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/provider/testdata/gen.py
Writes server/internal/provider/testdata/corpus.json.gz (deterministic, synthetic values only).

deployment_locations (A11) is replaced by a fake whose output Go's fakeLocations
reproduces ('fake' messages); the messages through the real A11 functions are
recorded as 'real' for the A11 port to compare against.
"""
import copy
import datetime as dt
import gzip
import itertools
import json
import os
import random
import tempfile
from unittest import mock

import yaml

from backend.agents import deployment_locations
from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository
from backend.converters.mihomo import IndentDumper
from backend.routes import aggregations as aggregations_route
from backend.routes import subscriptions as subscriptions_route
from backend.utils import provider_delivery as pd
from backend.utils import sub_store_client, subscription_cache, subscription_refresh
from backend.utils.strategy_references import StrategyReferenceError
from backend.utils.sub_store_client import SubscriptionResult

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'corpus.json.gz')
NOW = dt.datetime(2026, 11, 1, 0, 0, 0, 250000, tzinfo=dt.timezone.utc)
rows = []

REAL_CTX = deployment_locations.preparation_context
REAL_SRC = deployment_locations.source_error


def fake_ctx(exc, *, name='', path='', kind='规则集'):
    n = str(name) if name else ''
    p = str(path) if path else ''
    return ValueError(f'CTX[{kind}|{n}|{p}]{exc}')


def fake_src(message, text, *, line=None, column=None, payload_index=None, cause=None, source_label=''):
    c = 'None'
    if cause is not None:
        mark = getattr(cause, 'problem_mark', None)
        c = (f"{type(cause).__name__}:{getattr(cause, 'context', None)}:{getattr(cause, 'problem', None)}:"
             f"{'%d,%d' % (mark.line, mark.column) if mark is not None else None}")
    return ValueError(f'SRC[{message}|{line}|{column}|{source_label}|{c}]')


class FixedDatetime(dt.datetime):
    @classmethod
    def now(cls, tz=None):
        return NOW if tz is not None else NOW.replace(tzinfo=None)


class Counter:
    def __init__(self):
        self.n = 0

    def __call__(self):
        self.n += 1
        return mock.Mock(hex=f'{self.n:08x}' + '0' * 24)


def plain(value):
    """JSON-safe form of a result."""
    if isinstance(value, tuple):
        return [plain(v) for v in value]
    if isinstance(value, list):
        return [plain(v) for v in value]
    if isinstance(value, dict):
        return {str(k): plain(v) for k, v in value.items()}
    return value


def classify(exc):
    out = {'error': type(exc).__name__}
    if isinstance(exc, StrategyReferenceError):
        out['message'] = str(exc)
    return out


def run_once(call, fake=True):
    if not fake:
        try:
            return {'output': plain(call())}
        except Exception as exc:  # noqa: BLE001
            return classify(exc)
    with mock.patch.object(deployment_locations, 'preparation_context', fake_ctx), \
            mock.patch.object(deployment_locations, 'source_error', fake_src):
        return run_once(call, fake=False)


def real_of(out):
    return out['message'] if 'message' in out else '!' + out.get('error', 'no error')


def run_both(call):
    """Run call with the fake A11 (outcome) and the real one (message only)."""
    out = run_once(call)
    if 'message' in out:
        out['real'] = real_of(run_once(call, fake=False))
    return out


def add(kind, value, call):
    rows.append({'kind': kind, 'input': value, **run_both(call)})


# ---------------------------------------------------------------- shapes
shape_values = [
    [], {}, None, 'x', 1, [None], [1], ['a'], [{}], [{'name': ''}], [{'name': '  '}], [{'name': '　'}],
    [{'name': 1}], [{'name': None}], [{'name': []}], [{'name': 'A'}], [{'name': 'A', 'type': 'http'}],
    [{'name': 'A', 'type': 1}], [{'name': 'A', 'type': None}], [{'name': 'A', 'proxies': []}],
    [{'name': 'A', 'proxies': ['B']}], [{'name': 'A', 'proxies': ['']}], [{'name': 'A', 'proxies': [' ']}],
    [{'name': 'A', 'proxies': 'B'}], [{'name': 'A', 'proxies': None}], [{'name': 'A', 'proxies': [1]}],
    [{'name': 'A'}, {'name': 'B', 'type': []}], [{'name': 'A'}, 'x'], [{'name': 'A'}, {'type': 'http'}],
    [{'name': '\x85'}], [{'name': '​'}], [{'name': 'A', 'proxies': ['B', None]}],
]
for value in shape_values:
    add('shapes', value, lambda v=value: pd.validate_proxy_shapes(copy.deepcopy(v)))

# ---------------------------------------------------------------- parse_provider_proxies
texts = [
    '{}', 'proxies: []', '', '~', 'null', '[]', '- a\n', 'a', '1', 'proxies:', 'proxies: ~',
    'proxies: [{name: Good, type: http, server: example.test, port: 80}]',
    'proxies:\n  - name: Broken\n    type: [http\n',
    'proxies: [ {name: unterminated', '- not-a-root-object', 'proxies: [null]', 'proxies: not-an-array',
    'proxies: [{name: Remote, type: []}]',
    'proxies: wrong\n', 'proxies:\n  - null\n', 'proxies:\n  - name: []\n',
    'proxies:\n  - name: Missing type\n    type: []\n', 'proxies:\n  - name: Empty name\n    proxies: null\n',
    'proxies:\n  - name: Same\n    type: http\n  - name: Same\n    type: []\n',
    'proxies: []\nproxies:\n  - name: Duplicate key\n    type: []\n',
    'base: &base\n  type: []\nproxies:\n  - <<: *base\n    name: Merged\n',
    'base: &b\n  type: []\nother: &o\n  type: http\nproxies:\n  - <<: [*o, *b]\n    name: M\n',
    'proxies:\n  - <<: {type: []}\n    name: M\n    type: http\n',
    'proxies:\n  - name: M\n    <<: {type: []}\n',
    '- not-a-provider\n',
    'proxies: [{name: Broken, type: [], password: secret-password, uuid: secret-uuid}]',
    'proxies:\n  - name: Broken\n    password: "hidden-first\n      hidden-second" broken\n',
    'proxies:\n  - name: Broken\n    uuid: "hidden-first\n      hidden-second" broken\n',
    'proxies: [{name: Broken, password: "hidden-first\n    hidden-second" broken}]\n',
    'proxies: [{name: Broken, password: "hidden-first\n    hidden-second\\q',
    'proxies: [{name: Broken, password: "hidden-first\n    hidden-second",\n    type: []}]\n',
    "proxies: [{name: Broken, password: 'hidden-first\n    hidden-second',\n    type: []}]\n",
    'proxies:\n  - name: Broken\n    password: "hidden-first\n      hidden-second"\n    type: []\n',
    "proxies:\n  - name: Broken\n    password: 'hidden-first\n      hidden-second'\n    type: []\n",
    'proxies:\n - name: Bad\n   type: 11', 'proxies:\n - name: Invalid\n   type: []',
    'proxies: !!str x', 'proxies: !foo x', 'proxies: [!foo x]', '{[a]: b}', 'proxies: {[a]: b}',
    'a: *missing', '%YAML 1.1\n---\nproxies: []', '--- a\n--- b\n', 'proxies: [\t]', "proxies: ['\x07']",
    'proxies: [{name: A, type: http}, {name: B, type: http, dialer-proxy: A}]',
    'proxies:\n  - {name: "A", type: http, proxies: [B, C]}\n',
    'proxies:\n  - {name: "A", type: http, proxies: [B, ""]}\n',
    'proxies:\n  - "x"\n', 'proxies:\n  - 1: a\n    name: A\n', '1: a\nproxies: []\n',
    'proxies: !!binary aGVsbG8=', 'proxies: [&a {name: A}, *a]', 'x: !!timestamp 2026-13-45',
    'proxies:\n  - name: A\n    type: http\n  - name: B\n    type: !!int 3\n',
    'proxies:\n  - name: 中文节点\n    type: []\n', 'proxies:\n  - name: 🇭🇰 香港\n    type: [x]\n',
    'proxies:\r\n  - name: A\r\n    type: []\r\n', 'proxies:\n  - <<: *nope\n',
    'proxies:\n  - <<: [a]\n    name: A\n', 'proxies:\n  - <<: 1\n    name: A\n',
    'base: &b [1]\nproxies:\n  - <<: *b\n    name: A\n',
    'proxies:\n  - =: v\n    name: A\n    type: []\n',
    'proxies:\n  ? name\n  : A\n',
    'proxies:\n  - ? name\n    : A\n    ? type\n    : []\n',
    'proxies:\n  - name: A\n    "type": []\n', "proxies:\n  - name: A\n    !!str type: []\n",
    'proxies:\n  - name: A\n    proxies: [B, [C]]\n',
]
for text in texts:
    for name, path in (('', ''), ('机场 A', './providers/a.yaml'), ('Feed', 'https://user:secret-login@example.test/feed?token=secret-token')):
        add('parse', {'text': text, 'name': name, 'path': path},
            lambda t=text, n=name, p=path: pd.parse_provider_proxies(t, name=n, path=p))
add('parse', {'text': 'proxies: [{name: A, type: []}]', 'name': 0, 'path': None},
    lambda: pd.parse_provider_proxies('proxies: [{name: A, type: []}]', name=0, path=None))
add('parse', {'text': 'proxies: [{name: A, type: []}]', 'name': 7, 'path': 'p\nq'},
    lambda: pd.parse_provider_proxies('proxies: [{name: A, type: []}]', name=7, path='p\nq'))

# ---------------------------------------------------------------- snapshot capture / has_chains / validate
rng = random.Random(17)


def proxy(name, **extra):
    return {'name': name, 'type': 'http', 'server': 'example.test', 'port': 80, **extra}


mains = [
    {'proxies': [], 'proxy-groups': []},
    {'proxies': [proxy('Entry', **{'dialer-proxy': 'Upstream'}), proxy('Upstream')], 'proxy-groups': []},
    {'proxies': [proxy('Main')], 'proxy-groups': [{'name': 'G', 'type': 'select', 'proxies': ['Main'], 'use': ['Feed']}],
     'proxy-providers': {'Feed': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s.yaml'},
                         'Alias': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/alias.yaml'},
                         'Other': {'type': 'http', 'url': '/subscriptions/o/proxies', 'path': './providers/o.yaml'}}},
    {'proxies': [proxy('Main')], 'proxy-groups': [{'name': 'Relay', 'type': 'select', 'proxies': ['Main']}],
     'proxy-providers': {'Feed': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s.yaml',
                                  'override': {'dialer-proxy': 'Relay'}}}},
    {'proxies': [proxy('Main')], 'proxy-groups': [{'name': 'Relay', 'type': 'select', 'proxies': ['Main']}],
     'proxy-providers': {'Feed': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s.yaml',
                                  'override': {'dialer-proxy': ''}}}},
    {'proxies': [proxy('A', **{'dialer-proxy': None})], 'proxy-groups': []},
    {'proxies': [proxy('A', **{'dialer-proxy': 'G'})], 'proxy-groups': [{'name': 'G', 'type': 'select', 'proxies': []}]},
    {'proxies': [proxy('A', **{'dialer-proxy': 'B'}), proxy('B', **{'dialer-proxy': 'A'})], 'proxy-groups': []},
    {'proxies': [proxy('Dup'), proxy('Dup')], 'proxy-groups': []},
    {'proxies': [proxy('A')], 'proxy-groups': [{'name': 'Chain', 'type': 'select', 'proxies': ['A']}],
     'proxy-providers': {'Feed': {'type': 'http', 'url': '/aggregations/agg/provider', 'path': './providers/agg.yaml'}}},
    {'proxies': 'x', 'proxy-groups': []},
    {'proxies': [1], 'proxy-groups': []},
    {'proxies': [], 'proxy-groups': [], 'proxy-providers': {'F': 1}},
    {'proxies': [], 'proxy-groups': [], 'proxy-providers': {'F': {'override': 1}}},
    None,
]
profiles = [None, {}, {'proxy_groups': [{'name': 'Chain', 'type': 'chain'}]},
            {'proxy_groups': [{'name': 'Chain', 'type': 'chain', 'enabled': False}, {'name': 'G', 'type': 'select'}]},
            {'proxy_groups': [{'name': 'Chain', 'type': 'chain', 'enabled': 0}]},
            {'proxy_groups': [{'name': 1, 'type': 'chain'}]},
            {'proxy_groups': [{'type': 'chain'}]}, {'proxy_groups': ['x']}, {'proxy_groups': [{'name': ['x'], 'type': 'chain'}]}]
payloads = [[], [proxy('X')], [proxy('Chain')], [proxy('Main')], [proxy('X', **{'dialer-proxy': 'Main'})],
            [proxy('X', **{'dialer-proxy': 'Missing'})], [proxy('X', **{'dialer-proxy': 'Relay'})],
            [proxy('X'), proxy('X')], [proxy('Upstream')], [proxy('Entry')], 'x', [{'name': ''}],
            [proxy('X', **{'dialer-proxy': 'G'})], [proxy('A')], [proxy('Y', **{'dialer-proxy': 'X'}), proxy('X')]]


def snapshot_calls(main, profile):
    snap = pd.DeliverySnapshot.capture('default', copy.deepcopy(main), profile=copy.deepcopy(profile))
    out = [{'chain_names': sorted(map(str, snap.chain_names))}]
    for call in [('has_chains',)] + [('validate', p, f, n) for p in payloads for f in (False, True)
                                    for n in (None, 'Feed', 'Other', 'Nope', 'Alias')]:
        try:
            if call[0] == 'has_chains':
                out.append({'output': snap.has_chains})
            else:
                snap.validate(copy.deepcopy(call[1]), force_chains=call[2], provider_name=call[3])
                out.append({'output': None})
        except Exception as exc:  # noqa: BLE001
            out.append(classify(exc))
    return out


for main in mains:
    for profile in profiles:
        add('snapshot', {'main': main, 'profile': profile, 'payloads': payloads}, lambda m=main, p=profile: snapshot_calls(m, p))

# ---------------------------------------------------------------- validate_rendered_bundle
bundle_cases = [
    (mains[0], None, [{'name': 'First', 'local_path': './providers/first.yaml', 'content': 'proxies: []'},
                      {'name': 'Second', 'local_path': './providers/second.yaml', 'content': 'proxies:\n - name: Bad\n   type: 11'}]),
    (mains[1], None, [{'name': 'Feed', 'local_path': './providers/feed.yaml',
                       'content': 'proxies: [{name: Bad, type: http, dialer-proxy: Missing}]'}]),
    (mains[2], None, [{'name': 'Feed', 'local_path': './providers/s.yaml', 'content': yaml.safe_dump({'proxies': [proxy('X')]})},
                      {'name': 'Alias', 'local_path': './providers/alias.yaml', 'content': yaml.safe_dump({'proxies': [proxy('X')]})}]),
    (mains[2], None, [{'name': 'Feed', 'local_path': './providers/s.yaml', 'content': ''}]),
    (mains[3], None, [{'name': 'Feed', 'local_path': './providers/s.yaml', 'content': ''}]),
    (mains[3], None, [{'name': 'Feed', 'local_path': './providers/s.yaml', 'content': yaml.safe_dump({'proxies': [proxy('X')]})}]),
    (mains[3], None, [{'name': 'Feed', 'local_path': './providers/s.yaml', 'content': 'proxies: []'}]),
    (mains[0], None, [{'name': 'P', 'local_path': './p.yaml', 'content': yaml.safe_dump({'proxies': [proxy('X', **{'dialer-proxy': 'Y'}), proxy('Y')]})},
                      {'name': 'Q', 'local_path': './q.yaml', 'content': ''}]),
    (mains[0], None, [{'local_path': './p.yaml', 'content': 'proxies: [{name: A, type: []}]'}]),
    (mains[0], None, [{'content': 'proxies: x'}]),
    (mains[0], {'proxy_groups': [{'name': 'X', 'type': 'chain'}]},
     [{'name': 'P', 'local_path': './p.yaml', 'content': yaml.safe_dump({'proxies': [proxy('X')]})}]),
    (mains[2], None, [{'name': 'Other', 'local_path': './o.yaml', 'content': yaml.safe_dump({'proxies': [proxy('Main')]})}]),
    (mains[9], None, [{'name': 'Feed', 'local_path': './agg.yaml', 'content': yaml.safe_dump({'proxies': [proxy('B', **{'dialer-proxy': 'Chain'})]})}]),
    (mains[0], None, [{'name': 'P', 'local_path': './p.yaml', 'content': '- x'}]),
    (mains[0], None, [{'name': 'P', 'local_path': './p.yaml', 'content': 'proxies: [ {name: x'}]),
]
for main, profile, providers in bundle_cases:
    add('rendered_bundle', {'main': main, 'profile': profile, 'providers': providers},
        lambda m=main, p=profile, b=providers: pd.validate_rendered_bundle(
            pd.DeliverySnapshot.capture('default', copy.deepcopy(m), profile=copy.deepcopy(p)), copy.deepcopy(b)))


# ---------------------------------------------------------------- repository scenarios
def dump_files(root):
    files = {}
    for base in ('shared', 'profiles'):
        top = os.path.join(root, base)
        for directory, _, names in os.walk(top):
            for name in sorted(names):
                if name.endswith('.lock') or name.endswith('.bak'):
                    continue
                path = os.path.join(directory, name)
                rel = os.path.relpath(path, root)
                if not (rel.startswith('shared/subscribes/') or rel.startswith('shared/subscription-downloads/')
                        or '/providers/' in rel):
                    continue
                with open(path, encoding='utf-8') as fh:
                    files[rel] = fh.read()
    return dict(sorted(files.items()))


class Scenario:
    def __init__(self, spec):
        self.spec = spec
        self.calls = []

    def fetcher(self, label):
        def fetch(sub_id, url, *args, **kwargs):
            self.calls.append([label, sub_id, url])
            entry = self.spec.get('fetch', {}).get(f'{sub_id}|{url}')
            if entry is None:
                raise ConnectionError('no fake answer')
            if 'raise' in entry:
                raise {'ConnectionError': ConnectionError, 'ValueError': ValueError,
                       'StrategyReferenceError': StrategyReferenceError}[entry['raise']](entry['message'])
            if entry.get('tuple'):
                return (entry['text'], entry['source'])
            return SubscriptionResult(entry['text'], entry['source'], entry.get('traffic'), entry.get('fetched_at'))
        return fetch

    def fake_agg(self, agg, *, config=None, main_config=None, persist=True, subscription_fetch=None, source_prepared=False):
        self.calls.append(['agg', agg['id'], persist, source_prepared, main_config is not None])
        proxies = []
        for sid in agg.get('subscriptions', []):
            sub = next(s for s in config['subscriptions'] if s['id'] == sid)
            try:
                text, _source = subscription_fetch(sid, sub['url'])
                proxies.extend((yaml.safe_load(text) or {}).get('proxies', []))
            except StrategyReferenceError:
                raise
            except Exception as exc:  # noqa: BLE001
                self.calls.append(['agg-fetch-error', sid, type(exc).__name__])
        for nid in agg.get('nodes', []):
            proxies.append(proxy(f'node-{nid}'))
        content = agg['fake_content'] if 'fake_content' in agg else yaml.dump(
            {'proxies': proxies}, Dumper=IndentDumper, allow_unicode=True, default_flow_style=False, sort_keys=False, indent=2)
        return {'content': content, 'cache_updates': [], 'total_count': len(proxies)}

    def run(self, call):
        out = self.run_in(call, True)
        if 'message' in out:
            out['real'] = real_of(Scenario(self.spec).run_in(call, False))
        return out

    def run_in(self, call, fake):
        with tempfile.TemporaryDirectory() as root:
            repository = ProfileRepository(root)
            for sub_id, cache in self.spec.get('caches', {}).items():
                repository.write_shared_json(f'subscribes/{sub_id}.json', cache)
            for url, doc in self.spec.get('downloads', {}).items():
                repository.write_shared_json(subscription_refresh._download_cache_path(url), doc)
            for rel, text in self.spec.get('profile_files', {}).items():
                repository.write_profile_text('default', rel, text)
            with mock.patch.object(config_module, '_repository', repository), \
                    mock.patch.object(sub_store_client, 'datetime', FixedDatetime), \
                    mock.patch.object(subscription_refresh, 'datetime', FixedDatetime), \
                    mock.patch.object(subscription_cache, 'datetime', FixedDatetime), \
                    mock.patch('uuid.uuid4', Counter()), \
                    mock.patch.object(sub_store_client, 'get_subscription_proxies_yaml', self.fetcher('default')), \
                    mock.patch.object(subscriptions_route, 'get_subscription_proxies_yaml', self.fetcher('subscriptions')), \
                    mock.patch.object(aggregations_route, 'get_subscription_proxies_yaml', self.fetcher('aggregations')), \
                    mock.patch.object(aggregations_route, 'generate_aggregation_provider', self.fake_agg), \
                    mock.patch.object(sub_store_client, 'convert_proxy_string', lambda *a, **k: None):
                config_module.reset_config_context()
                out = run_once(lambda: call(repository), fake)
                out['calls'] = self.calls
                self.calls = []
                out['files'] = dump_files(root)
                config_module.reset_config_context()
            return out


URL = 'https://air.example.test/sub?token=private'
PROXIES = [proxy('Remote-1'), proxy('Remote-2', server='198.51.100.2')]
CONTENT = yaml.safe_dump({'proxies': PROXIES})
NODES = sub_store_client.proxies_to_nodes(copy.deepcopy(PROXIES))
CACHE = {'nodes': NODES, 'count': 2, 'updated_at': '2026-10-31T00:00:00+00:00', 'metadata': {'url': URL}}
AIRPORT = yaml.safe_dump({'proxies': [proxy('Relay-KR'), proxy('KR 01', **{'dialer-proxy': 'Relay'})],
                          'proxy-groups': [{'name': 'Relay', 'type': 'select', 'proxies': ['Relay-KR']}]}, allow_unicode=True)

prepare_cases = []
base_sub = {'id': 's', 'name': 'Feed', 'url': URL}
for sub_extra in ({}, {'interval': 0}, {'interval': 259200}, {'regex_filter': 'Remote-1'}, {'url': ''}, {'interval': -1}):
    for fetch in ({}, {f's|{URL}': {'text': CONTENT, 'source': 'rendered_yaml', 'fetched_at': '2026-10-31T12:00:00+00:00'}},
                  {f's|{URL}': {'text': CONTENT, 'source': 'cache'}},
                  {f's|{URL}': {'text': CONTENT, 'source': 'direct_yaml', 'tuple': True}},
                  {f's|{URL}': {'raise': 'ConnectionError', 'message': 'Connection refused https://user:secret-login@example.test/sub?token=secret-token'}},
                  {f's|{URL}': {'text': 'proxies: [{name: Bad, type: []}]', 'source': 'rendered_yaml'}},
                  {f's|{URL}': {'text': 'proxies: [ {name: x', 'source': 'rendered_yaml'}},
                  {f's|{URL}': {'text': 'proxies: []', 'source': 'rendered_yaml'}},
                  {f's|{URL}': {'text': AIRPORT, 'source': 'rendered_yaml'}},
                  {f's|{URL}': {'raise': 'StrategyReferenceError', 'message': '订阅内容不可用'}}):
        for caches in ({}, {'s': CACHE}, {'s': {**CACHE, 'metadata': {'url': URL + '&old'}}}, {'s': {'nodes': []}}):
            for opts in ({}, {'allow_transport_fallback': True}, {'source_prepared': True, 'source_path': './providers/s.yaml'}):
                prepare_cases.append({'sub': {**base_sub, **sub_extra}, 'fetch': fetch, 'caches': caches, 'opts': opts,
                                      'main': mains[0], 'profile': None})
# chains / snapshot-dependent variants
for main_index, profile in ((1, None), (3, None), (0, {'proxy_groups': [{'name': 'Remote-1', 'type': 'chain'}]})):
    for fetch in ({}, {f's|{URL}': {'text': CONTENT, 'source': 'rendered_yaml'}}):
        for opts in ({'allow_transport_fallback': True}, {}):
            prepare_cases.append({'sub': base_sub, 'fetch': fetch, 'caches': {}, 'opts': opts,
                                  'main': mains[main_index], 'profile': profile})
prepare_cases.append({'sub': {'id': 's', 'url': URL}, 'fetch': {}, 'caches': {'s': CACHE}, 'opts': {}, 'main': mains[0], 'profile': None})
prepare_cases.append({'sub': {'name': 'Feed', 'url': URL}, 'fetch': {}, 'caches': {}, 'opts': {'source_prepared': True}, 'main': mains[0], 'profile': None})
prepare_cases.append({'sub': {'id': 's', 'name': 'Feed'}, 'fetch': {}, 'caches': {'s': {'nodes': 'x'}}, 'opts': {}, 'main': mains[0], 'profile': None})
prepare_cases.append({'sub': {'id': 's', 'name': 'Feed'}, 'fetch': {}, 'caches': {'s': {'nodes': [{'name': 'N', 'type': 'http', 'server': 'n.test', 'port': 1}, {'name': 'Bad'}]}}, 'opts': {}, 'main': mains[0], 'profile': None})
prepare_cases.append({'sub': {'id': 's', 'name': 'Feed'}, 'fetch': {}, 'caches': {'s': {'nodes': [{'name': 'Same', 'type': 'http', 'server': 'a.test', 'port': 80}, {'name': 'Same', 'type': 'http', 'server': 'b.test', 'port': 80}]}}, 'opts': {}, 'main': mains[0], 'profile': None})
# a chained subscription with a dialer record (subscription_dialers delivery)
record_cache = {'nodes': sub_store_client.proxies_to_nodes([proxy('Relay-KR'), proxy('KR 01', **{'dialer-proxy': 'Relay'})]),
                'metadata': {'url': URL, 'dialers': {'targets': {'Relay': {'type': 'select', 'members': ['Relay-KR']}}, 'unresolved': []}}}
main_relay = {'proxies': [], 'proxy-groups': [{'name': 'Feed/Relay', 'type': 'select', 'hidden': True, 'use': ['Feed'], 'filter': '^(?:Relay\\-KR)$'}],
              'proxy-providers': {'Feed': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s.yaml'}}}
for caches in ({'s': record_cache}, {'s': {**record_cache, 'metadata': {'url': URL}}}):
    for downloads in ({}, {URL: {'text': AIRPORT, 'fetched_at': '2026-10-31T00:00:00+00:00'}}):
        prepare_cases.append({'sub': {**base_sub, 'url': '', 'interval': 0}, 'fetch': {}, 'caches': caches, 'downloads': downloads,
                              'opts': {}, 'main': main_relay, 'profile': None})
        prepare_cases.append({'sub': {**base_sub, 'interval': 0}, 'fetch': {}, 'caches': caches, 'downloads': downloads,
                              'opts': {}, 'main': main_relay, 'profile': None})


def prepare_call(case):
    def call(_repository):
        snap = pd.DeliverySnapshot.capture('default', copy.deepcopy(case['main']), profile=copy.deepcopy(case['profile']))
        kwargs = dict(case['opts'])
        if kwargs.pop('source_prepared', False):
            kwargs['source_prepared'] = True
            kwargs['fetch'] = sub_store_client.get_subscription_proxies_yaml
        result = pd.prepare_subscription_provider(copy.deepcopy(case['sub']), {}, snap, **kwargs)
        return result
    return call


for case in prepare_cases:
    rows.append({'kind': 'prepare_sub', 'input': case, **Scenario(case).run(prepare_call(case))})

# ---------------------------------------------------------------- prepare_provider_bundle / commit_provider_bundle
S2 = 'https://other.example.test/sub'
subs_config = [{'id': 's', 'name': 'Feed', 'url': URL, 'interval': 0}, {'id': 'o', 'name': 'Other', 'url': S2}]
bundle_main = {
    'proxies': [proxy('Main')], 'proxy-groups': [{'name': 'G', 'type': 'select', 'proxies': ['Main'], 'use': ['Feed', 'Other', 'Agg']}],
    'proxy-providers': {
        'Feed': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s.yaml'},
        'Feed copy': {'type': 'http', 'url': '/subscriptions/s/proxies', 'path': './providers/s-copy.yaml'},
        'Other': {'type': 'http', 'url': '/api/profiles/default/subscriptions/o/proxies?token=x', 'path': './providers/o.yaml'},
        'Agg': {'type': 'http', 'url': '/aggregations/agg/provider', 'path': './providers/agg.yaml'},
    }}
chained_main = copy.deepcopy(bundle_main)
chained_main['proxies'].append(proxy('Hop', **{'dialer-proxy': 'Main'}))
bundle_config = {'profile_id': 'default', 'subscriptions': subs_config,
                 'subscription_aggregations': [{'id': 'agg', 'name': 'Agg', 'subscriptions': ['s', 'o'], 'nodes': ['n1']}],
                 'nodes': [{'id': 'n1', 'name': 'N1', 'proxy_string': 'ss://YWVzLTEyOC1nY206cA@198.51.100.9:1#N1'},
                           {'id': 'n2', 'name': 'N2', 'proxy_string': '{"name": "N2", "type": "http", "server": "n.test", "port": 1}'}]}
fetch_ok = {f's|{URL}': {'text': CONTENT, 'source': 'rendered_yaml', 'fetched_at': '2026-10-31T12:00:00+00:00'},
            f'o|{S2}': {'text': yaml.safe_dump({'proxies': [proxy('O-1')]}), 'source': 'sub_store', 'fetched_at': '2026-10-31T12:00:00+00:00'}}
bundle_cases = []
for main in (bundle_main, chained_main):
    for config in (bundle_config, {**bundle_config, 'nodes': []}, {**bundle_config, 'profile_id': None}):
        for fetch in ({}, fetch_ok, {**fetch_ok, f'o|{S2}': {'raise': 'ConnectionError', 'message': 'down'}},
                      {**fetch_ok, f'o|{S2}': {'text': 'proxies: [{name: Bad, type: []}]', 'source': 'rendered_yaml'}},
                      {**fetch_ok, f'o|{S2}': {'text': yaml.safe_dump({'proxies': [proxy('Main')]}), 'source': 'rendered_yaml'}}):
            for opts in ({}, {'render_all': True}, {'allow_transport_fallback': True},
                         {'requested': ['subscription', {'id': 'o', 'name': 'Other'}]},
                         {'requested': ['aggregation', {'id': 'agg', 'name': 'Agg'}]},
                         {'requested': ['subscription', {'id': 'new', 'name': 'New', 'url': URL}]},
                         {'prepared': {'Feed': {'content': yaml.safe_dump({'proxies': [proxy('Prepared')]}), 'cache_updates': []}}, 'render_all': True}):
                for caches in ({}, {'s': CACHE}):
                    bundle_cases.append({'config': config, 'main': main, 'fetch': fetch, 'opts': opts, 'caches': caches})
# alias + prepared failure (test_prepared_provider_failure_keeps_alias_and_real_output_path)
bundle_cases.append({'config': {'profile_id': 'default', 'subscriptions': [{'id': 's', 'name': 'Source name'}]},
                     'main': {'proxy-providers': {'Provider alias': {'url': '/subscriptions/s/proxies', 'path': './providers/actual.yaml'},
                                                  'Other alias': {'url': '/subscriptions/s/proxies', 'path': './providers/other.yaml'}}},
                     'fetch': {}, 'caches': {}, 'opts': {'render_all': True, 'prepared': {
                         'Provider alias': {'content': 'proxies:\n - name: Invalid\n   type: []', 'cache_updates': []}}}})
# unknown provider source, missing path, provider-only chain activation, aggregation content with chains
bundle_cases.append({'config': bundle_config, 'main': {'proxies': [], 'proxy-groups': [], 'proxy-providers': {
    'Ext': {'url': 'https://elsewhere.example.test/p.yaml', 'path': './providers/ext.yaml'}}}, 'fetch': {}, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': bundle_config, 'main': {'proxies': [], 'proxy-groups': [], 'proxy-providers': {
    'Feed': {'url': '/subscriptions/s/proxies'}}}, 'fetch': fetch_ok, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': bundle_config, 'main': {'proxies': [], 'proxy-groups': [], 'proxy-providers': {
    'Feed': {'url': '/subscriptions/missing/proxies', 'path': './p.yaml'}}}, 'fetch': {}, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': {**bundle_config, 'subscription_aggregations': [
    {'id': 'agg', 'name': 'Agg', 'subscriptions': [], 'nodes': [], 'fake_content': yaml.safe_dump({'proxies': [proxy('A1', **{'dialer-proxy': 'Main'})]})}]},
    'main': bundle_main, 'fetch': fetch_ok, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': {**bundle_config, 'subscription_aggregations': [
    {'id': 'agg', 'name': 'Agg', 'subscriptions': [], 'nodes': ['n1'], 'fake_content': yaml.safe_dump({'proxies': [proxy('A1', **{'dialer-proxy': 'Main'})]})}]},
    'main': bundle_main, 'fetch': fetch_ok, 'caches': {}, 'opts': {}})
bundle_cases.append({'config': {**bundle_config, 'subscription_aggregations': [
    {'id': 'agg', 'name': 'Agg', 'subscriptions': [], 'nodes': [], 'fake_content': 'proxies: [{name: X, type: []}]'}]},
    'main': bundle_main, 'fetch': fetch_ok, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': {**bundle_config, 'subscription_aggregations': [
    {'id': 'agg', 'name': 'Agg', 'subscriptions': [], 'nodes': [], 'fake_content': ''}]},
    'main': chained_main, 'fetch': fetch_ok, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': bundle_config, 'main': {'proxies': [], 'proxy-groups': [], 'proxy-providers': {
    'Feed': {'url': '/subscriptions/s%2Fx/proxies', 'path': './p.yaml'}}}, 'fetch': {}, 'caches': {}, 'opts': {'render_all': True}})
bundle_cases.append({'config': bundle_config, 'main': {'proxies': [], 'proxy-groups': [], 'proxy-providers': {
    'Feed': {'url': 'http://[::1/subscriptions/s/proxies', 'path': './p.yaml'}}}, 'fetch': {}, 'caches': {}, 'opts': {'render_all': True}})


def bundle_call(case):
    def call(repository):
        progress = []
        kwargs = dict(case['opts'])
        if 'requested' in kwargs:
            kwargs['requested'] = tuple(copy.deepcopy(kwargs['requested']))
        if 'prepared' in kwargs:
            kwargs['prepared'] = copy.deepcopy(kwargs['prepared'])
        config, main = copy.deepcopy(case['config']), copy.deepcopy(case['main'])
        before = (json.dumps(config, sort_keys=True), json.dumps(main, sort_keys=True))
        bundle = pd.prepare_provider_bundle(config, main,
                                            progress=lambda message, force=False: progress.append([message, force]), **kwargs)
        assert before == (json.dumps(config, sort_keys=True), json.dumps(main, sort_keys=True))
        pd.commit_provider_bundle('default', bundle)
        return {'bundle': bundle, 'progress': progress}
    return call


for case in bundle_cases:
    rows.append({'kind': 'bundle', 'input': case, **Scenario(case).run(bundle_call(case))})

data = json.dumps(rows, ensure_ascii=False).encode()
with open(OUT, 'wb') as fh:
    with gzip.GzipFile(fileobj=fh, mode='wb', mtime=0) as gz:
        gz.write(data)
print(len(rows), 'rows')
