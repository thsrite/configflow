"""Reference corpus for internal/refresh from the real Python modules.

Run on the test server from the repository root:
  PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/refresh/testdata/gen.py
Writes server/internal/refresh/testdata/corpus.json (deterministic, synthetic values only).
"""
import datetime as dt
import json
import os
import tempfile
from unittest import mock

import requests

from backend.common import config as config_module
from backend.common.config_repository import ProfileRepository, ProfileValidationError
from backend.routes import tasks as tasks_route
from backend.utils import scheduled_refresh as sr
from backend.utils import subscription_health as health
from backend.utils import subscription_refresh as srf
from backend.utils import task_status
from backend.utils.dialer_references import DialerReferenceError
from backend.utils.rule_utils import rule_cache_filename
from backend.utils.ruleset_content import RemoteRuleValidationError
from backend.utils.strategy_references import StrategyReferenceError
from backend.utils.sub_store_client import SubscriptionFetchError
from backend.utils.rule_fetch import RuleFetchError

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'corpus.json')
NOW = dt.datetime(2026, 11, 1, 0, 0, 0, 250000, tzinfo=dt.timezone.utc)

rows = []


def err(exc):
    return {'error': type(exc).__name__}


def add(fn, value, call, **extra):
    try:
        out = {'output': call()}
    except Exception as exc:  # noqa: BLE001
        out = err(exc)
    rows.append({'fn': fn, 'input': value, **extra, **out})


class FixedDatetime(dt.datetime):
    @classmethod
    def now(cls, tz=None):
        return NOW if tz is not None else NOW.replace(tzinfo=None)


def iso(value):
    return value.isoformat() if isinstance(value, dt.datetime) else value


# ---- validate_refresh_interval (default label and a custom one)
intervals = [0, 60, 59, 1, -1, 604800, 604801, 3600, True, False, None, '60', '', 60.0, 60.5, 0.0, 10 ** 40,
             -10 ** 40, [], {}, [60]]
for value in intervals:
    add('validate_refresh_interval', value, lambda v=value: srf.validate_refresh_interval(v))
add('validate_refresh_interval_label', 30, lambda: srf.validate_refresh_interval(30, '更新间隔'))

# ---- subscription_interval / rule_interval
for value in ['missing', None, True, False, 3.0, '60', 10 ** 30, -5, 0, 7, [1]]:
    item = {} if value == 'missing' else {'interval': value, 'update_interval': value}
    add('subscription_interval', item, lambda i=item: sr.subscription_interval(i))
    add('rule_interval', item, lambda i=item: sr.rule_interval(i))

# ---- next_run_at (now always given)
NOW_GIVEN = dt.datetime(2026, 11, 1, 0, 0, 0, tzinfo=dt.timezone.utc)
last_values = ['2026-10-24T00:00:00Z', '2026-10-24T08:00:00+08:00', '2026-10-24T00:00:00.123456',
               '2026-03-29T01:30:00+01:00', '9999-12-31T23:00:00+00:00', '0001-01-01T00:30:00+01:00',
               '0001-01-01T00:00:00Z', '2026-10-24', '2026-W43-1T12:00', 'bad', '', None, 5,
               '２０２６-10-24T00:00:00', '2026-10-24T00:00:00-05:30', '2026-10-24T00:00:00ZZ',
               '2026-W43-1T12:00+02:00', '2026-W43-1T12:00', '2026-10-24T00:00:00+05:30:15', '20261024T000000+0530',
               '2026-10-24T00:00:00,5-01:00', '2026-10-24T00:00-0100', '2026-10-24 00:00:00+14:00',
               '2026-10-24T00:00:00.5+05:30:15.25', '2026-10-24T23:59:59.999999-23:59']
failures = ['missing', 0, 1, 2, 3, 11, 30, 10 ** 30, 2.7, '3', '３', ' 4 ', 'x', True, False, None, [], [1], -5,
            float('inf'), float('nan'), 1e300,
            # review C15: ints beyond int64 in both directions
            -(2 ** 64) + 5, -(2 ** 63) - 1, -(2 ** 63), 2 ** 64 + 1, '-18446744073709551611', -1e19, '٣']
interval_values = [0, 60, 3600, 86400, -60, 10 ** 30, 999999999 * 86400 + 86399, 999999999 * 86400 + 86400, 400,
                   -999999999 * 86400, -999999999 * 86400 - 1, -10 ** 30]
for last in last_values:
    for interval in interval_values:
        for status in ['success', 'failed', None]:
            entry = {'last_run_at': last}
            if status:
                entry['last_status'] = status
                entry['consecutive_failures'] = 3
            add('next_run_at', {'entry': entry, 'interval': interval, 'baseline': None},
                lambda e=entry, i=interval: iso(sr.next_run_at(e, i, None, NOW_GIVEN)))
for count in failures:
    entry = {'last_run_at': '2026-10-24T00:00:00Z', 'last_status': 'failed'}
    if count != 'missing':
        entry['consecutive_failures'] = count
    for interval in [86400, 600, 10 ** 30]:
        add('next_run_at', {'entry': entry, 'interval': interval, 'baseline': None},
            lambda e=entry, i=interval: iso(sr.next_run_at(e, i, None, NOW_GIVEN)))
for baseline in [None, '2026-10-24T01:00:00Z', 'x', 7, '2026-10-24T01:00:00+02:00', '']:
    for entry in [{}, {'last_run_at': 'x'}, {'last_status': 'failed'}]:
        add('next_run_at', {'entry': entry, 'interval': 3600, 'baseline': baseline},
            lambda e=entry, b=baseline: iso(sr.next_run_at(e, 3600, b, NOW_GIVEN)))
for entry in [[], 'x', 5, [1], {'a': 1}, True]:
    for interval in [0, 60]:
        add('next_run_at', {'entry': entry, 'interval': interval, 'baseline': None},
            lambda e=entry, i=interval: iso(sr.next_run_at(e, i, None, NOW_GIVEN)))
for status in ['failed', 'FAILED', 'skipped', 1]:
    entry = {'last_run_at': '2026-10-24T00:00:00Z', 'last_status': status, 'consecutive_failures': 2}
    add('next_run_at', {'entry': entry, 'interval': 86400, 'baseline': None},
        lambda e=entry: iso(sr.next_run_at(e, 86400, None, NOW_GIVEN)))

# ---- _is_fresh (now = NOW)
with mock.patch.object(srf, 'datetime', FixedDatetime):
    stamps = ['2026-11-01T00:00:00.250000Z', '2026-11-01T00:00:00.250001Z', '2026-10-31T23:59:00.250000Z',
              '2026-10-31T23:59:00.250001Z', '2026-10-31T23:59:00.249999Z', '2026-10-31T23:59:00',
              '2026-11-01T01:00:00+01:00', '2026-11-01T08:59:00+09:00', 'bad', '', None, 5, '2026-11-01',
              '0001-01-01T00:00:00Z', '9999-12-31T00:00:00Z', '2026-10-31T23:59:00.250000+00:00']
    for stamp in stamps:
        for interval in [60, 3600, 1]:
            add('is_fresh', {'timestamp': stamp, 'interval': interval},
                lambda s=stamp, i=interval: srf._is_fresh(s, i))

# ---- _download_cache_path
for url in ['https://airport.test/sub?token=private', '', 'https://例子.测试/订阅', 'http://a/ ', 'x' * 300]:
    add('download_cache_path', url, lambda u=url: srf._download_cache_path(u))

# ---- describe_failure / safe_exception_details


def response(status):
    r = requests.Response()
    r.status_code = status
    return r


requests_classes = ['RequestException', 'HTTPError', 'ConnectionError', 'ProxyError', 'SSLError', 'Timeout',
                    'ConnectTimeout', 'ReadTimeout', 'URLRequired', 'TooManyRedirects', 'MissingSchema',
                    'InvalidSchema', 'InvalidURL', 'InvalidHeader', 'InvalidProxyURL', 'InvalidJSONError',
                    'ChunkedEncodingError', 'ContentDecodingError', 'StreamConsumedError', 'RetryError',
                    'UnrewindableBodyError']
failures_in = []
for name in requests_classes:
    cls = getattr(requests.exceptions, name)
    failures_in.append(({'class': name, 'message': 'https://x.test?token=private'}, cls('https://x.test?token=private')))
    failures_in.append(({'class': name, 'status': 502, 'message': 'm'}, cls('m', response=response(502))))
failures_in += [
    ({'class': 'JSONDecodeError', 'message': 'Expecting value'}, requests.exceptions.JSONDecodeError('Expecting value', '', 0)),
    ({'class': 'ValueError', 'message': '订阅没有返回任何节点，已保留原有缓存'}, ValueError('订阅没有返回任何节点，已保留原有缓存')),
    ({'class': 'StrategyReferenceError', 'message': '策略不存在'}, StrategyReferenceError('策略不存在')),
    ({'class': 'DialerReferenceError', 'message': '资源必须是 JSON 对象'}, DialerReferenceError('资源必须是 JSON 对象')),
    ({'class': 'RemoteRuleValidationError', 'message': '第 1 行：不支持的规则'}, RemoteRuleValidationError('第 1 行：不支持的规则')),
    ({'class': 'StaleRuleCache', 'message': '连接上游超时'}, sr.StaleRuleCache('连接上游超时')),
    ({'class': 'RuntimeError', 'message': 'https://x.test'}, RuntimeError('https://x.test')),
    ({'class': 'KeyError', 'message': "'name'"}, KeyError('name')),
    ({'class': 'TypeError', 'message': 'x'}, TypeError('x')),
    ({'class': 'AttributeError', 'message': 'x'}, AttributeError('x')),
    ({'class': 'PermissionError', 'message': 'x'}, PermissionError(13, 'Permission denied')),
    ({'class': 'ProfileValidationError', 'message': 'bad'}, ProfileValidationError('bad')),
    ({'class': 'RuleFetchError', 'message': 'x'}, RuleFetchError([('源地址', requests.ConnectionError('x'))])),
    ({'class': 'SubscriptionFetchError', 'sub_store': {'class': 'RuntimeError', 'message': 'x'},
      'direct': {'class': 'Timeout', 'message': 'y'}},
     SubscriptionFetchError(RuntimeError('x'), requests.Timeout('y'))),
    ({'class': 'SubscriptionFetchError', 'sub_store': {'class': 'ConnectionError', 'message': 'x'}},
     SubscriptionFetchError(requests.ConnectionError('x'), None)),
    ({'class': 'SubscriptionFetchError', 'direct': {'class': 'HTTPError', 'status': 429, 'message': 'y'}},
     SubscriptionFetchError(None, requests.HTTPError('y', response=response(429)))),
    ({'class': 'SubscriptionFetchError'}, SubscriptionFetchError(None, None)),
]
for spec, exc in failures_in:
    add('describe_failure', spec, lambda e=exc: sr.describe_failure(e))

# ---- fromtimestamp(st_mtime) whole seconds (rule_cache_time)
for sec, nsec in [(1761955200, 0), (1761955200, 999999499), (1761955200, 999999500), (1761955200, 999999501),
                  (1761955200, 999999999), (1761955200, 500), (1761955200, 499), (1761955200, 1500),
                  (1761955200, 2500), (0, 999999500), (1, 0), (4102444799, 999999600), (1761955201, 999999500)]:
    value = sec + nsec * 1e-9
    add('mtime_iso', [sec, nsec], lambda v=value: dt.datetime.fromtimestamp(v, dt.timezone.utc)
        .isoformat(timespec='seconds').replace('+00:00', 'Z'))

# ---- routes.tasks._host
for url in ['https://rules.test/list.txt?key=private', 'HTTPS://Rules.TEST:8443/x', 'http://[::1]:80/x',
            'http://user:pw@host.test/x', 'rules.test/list', '', None, 'http://[bad/x', 'http://例子.测试/x',
            'http://host.test:abc/', '//host.test/x', 'http://%41.test/']:
    add('host', url, lambda u=url: tasks_route._host(u))

# ---- scenarios on a repository: record_fetch, rows, _due


def scenario(name, setup, calls):
    with tempfile.TemporaryDirectory() as tmp:
        repo = ProfileRepository(tmp)
        config_module.set_repository(repo)
        config_module.reset_config_context()
        files = setup(repo)
        out = []
        with mock.patch.object(health, 'datetime', FixedDatetime), \
                mock.patch.object(sr, 'datetime', FixedDatetime):
            for label, call in calls:
                try:
                    out.append({'call': label, 'output': call(repo)})
                except Exception as exc:  # noqa: BLE001
                    out.append({'call': label, **err(exc)})
                config_module.reset_config_context()
        rows.append({'fn': 'scenario', 'input': name, 'files': files, 'output': out})


def health_setup(text):
    def setup(repo):
        if text is not None:
            repo.write_shared_text(health.HEALTH_FILE, text)
        return {'health': text}
    return setup


def record(sub_id, **kwargs):
    def call(repo):
        health.record_fetch(sub_id, **kwargs)
        return repo.read_shared_json(health.HEALTH_FILE)
    return (json.dumps([sub_id, kwargs], ensure_ascii=False), call)


def forget(sub_id):
    def call(repo):
        health.forget(sub_id)
        return json.loads(open(repo.shared_path(health.HEALTH_FILE)).read())
    return ('forget ' + sub_id, call)


scenario('health fresh', health_setup(None), [
    record('s', ok=True, count=3),
    record('s', ok=False, from_cache=True, count=None),
    record('s', ok=False, count='4'),
    record('s', ok=True, count=2.9, traffic={'upload': 1}),
    record('s', ok=True, count=True, traffic={}),
    record('s', ok=True, from_cache=True, count=[]),
    record('s', ok=False, count='x'),
    record('', ok=True),
    record('t', ok=True, count=10 ** 20),
    forget('s'),
    forget('missing'),
])
scenario('health edited', health_setup('{"s": {"history": "ab", "traffic": 1}, "t": {"history": {"x": 1}}, "u": 5, '
                                      '"v": {"history": 7}, "w": null, "x": {"history": [' +
                                      ','.join(str(i) for i in range(30)) + ']}}'), [
    record('s', ok=True), record('t', ok=True), record('u', ok=True), record('v', ok=True),
    record('x', ok=True, count=1), forget('w'), forget('x'),
])
scenario('health not object', health_setup('[1, 2]'), [record('s', ok=True)])
# review C15: odd counts and history lengths around HISTORY_SIZE
scenario('health review', health_setup('{"a": {"history": [' + ','.join(str(i) for i in range(23)) + ']}, '
                                      '"b": {"history": [' + ','.join(str(i) for i in range(24)) + ']}, '
                                      '"c": {"history": null, "traffic": {"total": 1}}, "d": {"history": []}, '
                                      '"e": {"history": "é😀"}, "f": {"history": false}, "g": {"history": 1.5}}'), [
    record('a', ok=True), record('b', ok=False, from_cache=True), record('c', ok=True, traffic=None),
    record('d', ok=True, count=float('nan')), record('d', ok=True, count=float('inf')), record('d', ok=True, count=-1e300),
    record('d', ok=True, count=' 7 '), record('d', ok=True, count='٧'), record('d', ok=True, count='7.0'),
    record('d', ok=True, count={}), record('d', ok=True, count={'a': 1}), record('e', ok=True), record('f', ok=True),
    record('g', ok=True), record('订阅', ok=True, traffic={'upload': 0}), forget('订阅'), forget('c'),
])
scenario('health broken', health_setup('{broken'), [record('s', ok=True)])

RULE_URL = 'https://rules.test/list.txt?key=private'
SUB_URL = 'https://airport.test/sub?token=private'


def rows_setup(shared, statuses, caches, rule_files):
    def setup(repo):
        repo.save_shared(shared)
        with open(os.path.join(tmpdir(repo), 'task-status.json'), 'w') as handle:
            handle.write(statuses)
        for sub_id, value in caches.items():
            repo.write_shared_json(f'subscribes/{sub_id}.json', value)
        written = []
        for rule, (content, ns) in rule_files:
            name = rule_cache_filename(rule)
            path = repo.shared_path('rules/' + name)
            repo.write_shared_text('rules/' + name, content)
            os.utime(path, ns=(ns, ns))
            written.append({'name': name, 'content': content, 'mtime_ns': ns})
        return {'shared': shared, 'statuses': statuses, 'caches': caches, 'rule_files': written}
    return setup


def tmpdir(repo):
    return str(repo.data_dir)


def rows_call(repo):
    statuses = task_status.load()
    shared = repo.get_shared()
    return tasks_route._subscription_rows(shared, statuses) + tasks_route._rule_rows(shared, statuses)


def due_call(repo):
    return [key for key, _, _ in sr._due(dt.datetime(2026, 11, 1, tzinfo=dt.timezone.utc))]


rules = [
    {'id': 'r1', 'name': 'One', 'source_type': 'url', 'url': RULE_URL, 'behavior': 'domain', 'format': 'text'},
    {'id': 'r2', 'name': 'Two', 'url': 'https://Rules.TEST:8443/two', 'behavior': 'domain', 'format': 'text',
     'update_interval': 3600, 'enabled': False},
    {'id': 'r3', 'name': 'Three', 'source_type': 'url', 'url': 'https://three.test/x', 'behavior': 'domain',
     'format': 'text', 'update_interval': 0},
    {'id': 'r4', 'name': 'Four', 'source_type': 'content', 'content': '+.a.test', 'behavior': 'domain', 'format': 'text'},
    {'id': 'r5', 'name': 'Five', 'source_type': 'url', 'url': 'https://five.test/x', 'behavior': 'domain',
     'format': 'text', 'update_interval': 60, 'enabled': 0},
    {'id': 'r6', 'name': 'Six', 'source_type': 'url', 'url': 'https://six.test/x', 'behavior': 'domain',
     'format': 'text', 'update_interval': True},
]
subscriptions = [
    {'id': 's1', 'name': 'Feed', 'url': SUB_URL, 'enabled': True, 'interval': 86400},
    {'id': 's2', 'name': '', 'url': 'https://two.test/sub', 'enabled': False},
    {'id': 's3', 'name': 'NoURL', 'url': '', 'interval': 3600},
    {'id': 's4', 'name': 'Manual', 'url': 'https://four.test/sub', 'interval': 0},
    {'id': 's5', 'name': 'Zero', 'url': 'https://five.test/sub', 'enabled': 0, 'interval': 600},
    {'id': 's6', 'name': 'Moved', 'url': 'https://six.test/new', 'interval': 600},
    {'id': 's7', 'name': 'East', 'url': 'https://seven.test/sub', 'interval': 3600},
]
statuses = json.dumps({
    'subscription:s1': {'last_run_at': '2026-10-31T00:00:00Z', 'last_status': 'failed', 'last_error': '上游返回 HTTP 403',
                        'consecutive_failures': 2, 'run_count': 5, 'failure_count': 2.0, 'cache_retained': True,
                        'cache_updated_at': '2026-10-30T00:00:00Z', 'last_trigger': 'schedule'},
    'subscription:s4': {'last_run_at': '2026-10-01T00:00:00+08:00', 'last_status': 'success', 'run_count': '3'},
    'rule_source:r1': {'last_run_at': '2026-10-31T23:00:00Z', 'last_status': 'success', 'run_count': True},
    'rule_source:r6': [],
    'subscription:s5': 0,
    'subscription:s7': {'last_run_at': '2026-10-31T08:00:00+08:00', 'last_status': 'failed', 'consecutive_failures': '2',
                        'failure_count': '1'},
}, ensure_ascii=False)
caches = {
    's2': {'subscription_id': 's2', 'updated_at': '2026-10-01T00:00:00Z', 'count': 1, 'nodes': [{'name': 'a'}],
           'metadata': {'url': 'https://two.test/sub'}},
    's6': {'subscription_id': 's6', 'updated_at': '2026-10-31T23:55:00Z', 'count': 1, 'nodes': [{'name': 'a'}],
           'metadata': {'url': 'https://six.test/old'}},
    's5': {'subscription_id': 's5', 'updated_at': '2026-10-31T23:55:00Z', 'count': 1, 'nodes': [{'name': 'a'}],
           'metadata': {'url': 'https://five.test/sub'}},
}
rule_files = [(rules[0], ('+.one.test\n', 1761955200999999500)), (rules[1], ('+.two.test\n', 1761868800000000000)),
              (rules[5], ('+.six.test\n', 1761951600123456789))]
scenario('rows', rows_setup({'subscriptions': subscriptions, 'rule_library': rules, 'nodes': [],
                              'subscription_aggregations': []}, statuses, caches, rule_files),
         [('rows', rows_call), ('due', due_call)])

statuses_bad = json.dumps({'rule_source:r1': {'last_run_at': '2026-10-31T00:00:00Z', 'last_status': 'failed',
                                              'consecutive_failures': 'x'}})
scenario('rows bad failures', rows_setup({'subscriptions': [], 'rule_library': rules[:1]}, statuses_bad, {}, []),
         [('rows', rows_call), ('due', due_call)])
scenario('rows non-dict entry', rows_setup({'subscriptions': subscriptions[:1], 'rule_library': []},
                                           json.dumps({'subscription:s1': [1]}), {}, []),
         [('rows', rows_call), ('due', due_call)])

with open(OUT, 'w', encoding='utf-8') as handle:
    json.dump(rows, handle, ensure_ascii=False, indent=0, allow_nan=True)
    handle.write('\n')
print(len(rows), 'rows')
