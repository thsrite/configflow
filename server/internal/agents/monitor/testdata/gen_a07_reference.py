"""CPython reference corpus for A07 (mihomo_panel, oxidns_cache,
oxidns_rule_update, oxidns_ranking_history).

Synthetic values only; no network (requests.Session.request and
oxidns_monitor._request are replaced). Writes a07_reference.json.gz next to
this file:

    rt -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a07_reference.py'

Results are json.dumps(value, sort_keys=True) text so the Go test compares
bytes. The ranking scenario lists its operations; the Go test replays them
and compares the answers and every stored row (with rowid), sqlite_master,
the journal mode and the header's file format bytes.
"""
import base64
import copy
import gzip
import json
import os
import sqlite3
import tempfile
from collections import Counter

import requests
from werkzeug.datastructures import MultiDict

from backend.agents import mihomo_panel as panel
from backend.agents import oxidns_cache as caches
from backend.agents import oxidns_monitor as monitor
from backend.agents import oxidns_ranking_history as history
from backend.agents import oxidns_rule_update as updates

OUT = {}


def dump(value):
    return json.dumps(value, sort_keys=True)


def error_of(exc):
    if isinstance(exc, monitor.MonitorError):
        out = {'error': exc.code, 'status': exc.status, 'message': exc.message}
        if hasattr(exc, 'flushed'):
            out['flushed'] = exc.flushed
        return out
    return {'exception': type(exc).__name__}


class Manager:
    def __init__(self, agent):
        self.agent = agent

    def get_agent_by_id(self, agent_id, history=True):
        return copy.deepcopy(self.agent) if self.agent and agent_id == self.agent.get('id') else None


class Reply:
    def __init__(self, status, body):
        self.status_code = status
        self.body = body

    def __enter__(self):
        return self

    def __exit__(self, *args):
        pass

    def iter_content(self, size):
        for start in range(0, len(self.body), size):
            yield self.body[start:start + size]


SEEN = []
REPLY = []


def fake_request(session, method, url, params=None, data=None, headers=None, timeout=None, **kwargs):
    prepared = requests.Request(method, url, params=params or {}).prepare()
    SEEN.append({'method': method, 'url': prepared.url, 'data': data,
                 'headers': {k: v for k, v in (headers or {}).items()},
                 'timeout': list(timeout) if timeout else None,
                 'trust_env': session.trust_env, 'allow_redirects': kwargs.get('allow_redirects'),
                 'stream': kwargs.get('stream')})
    status, body = REPLY[0]
    if status == 'raise':
        raise requests.ConnectionError('refused')
    return Reply(status, body)


requests.Session.request = fake_request


def enc(value):
    return value if isinstance(value, bytes) else json.dumps(value).encode()


def run(case, fn):
    SEEN.clear()
    REPLY[:] = [case['reply']]
    try:
        result = fn()
        outcome = {'ok': result}
    except Exception as exc:  # noqa: BLE001
        outcome = error_of(exc)
    return {'outcome': dump(outcome), 'seen': dump(SEEN)}


MIHOMO = {'id': 'gw', 'name': 'Gateway', 'host': '127.0.0.1', 'port': 8080, 'service_type': 'mihomo',
          'version': '1.8.60-go', 'status': 'online', 'token': 'agent-token'}

NODE = {'name': 'HK', 'type': 'Trojan', 'udp': True, 'xudp': True, 'alive': True, 'id': 'x',
        'history': [{'time': 't1', 'delay': 1}, {'time': 't2', 'delay': 2}],
        'extra': {'u1': {'alive': False, 'history': [{'delay': 3}, {'delay': 4}]}, 'u2': {'history': 'x'},
                  'u3': 'not a dict', 'u4': {}}}


def panel_cases():
    cases = []

    def add(resource, method, args, body=None, reply=(200, {'ok': True}), agent=None):
        cases.append({'agent': agent or MIHOMO, 'resource': resource, 'method': method, 'args': args, 'body': body,
                      'reply': [reply[0], base64.b64encode(enc(reply[1])).decode()]})

    names = ['HK', '节点/香港 01', '🇯🇵 JP · 50%?#', 'a' * 512, 'a' * 513, '', 'tab\tname', 'del\x7fname', '\u0085x',
             'ß', '~._-', ' lead']
    for name in names:
        add('proxy', 'PUT', [['name', name]], {'name': 'HK'})
        add('proxy', 'DELETE', [['name', name], ['name', 'second']])
        add('providers/proxy/delay', 'GET', [['provider', name], ['name', 'B']])
    for after in ['0', '7', '١٢', '²', '①', '½', '-1', '1e3', '9' * 18, '9' * 19, '', ' 1', '０']:
        add('logs', 'GET', [['after', after]])
    for level in ['debug', 'info', 'warning', 'error', 'trace', 'INFO', '']:
        add('logs', 'GET', [['level', level]])
    for timeout in ['5000', ' 600 ', '+700', '1_000', '٣٠٠٠', '０５０００', '　5000', '499', '30001', '30000',
                    '500', '1e3', '', '1__0', '\x1c600', '5 0', '1' * 4301, '-600', '00000600']:
        add('proxy/delay', 'GET', [['name', 'HK'], ['timeout', timeout]])
    for url in ['http://x', 'https://x/y?z=1&a=b', 'HTTP://x', 'ftp://x', 'http://' + 'a' * 2041, 'http://' + 'a' * 2042,
                '', 'https://例子.测试/路径?q=值']:
        add('group/delay', 'GET', [['name', 'Auto'], ['url', url]])
    for body in [{'name': 'HK'}, {'name': ''}, {'name': 'a' * 513}, {'name': 5}, {'name': 'HK', 'x': 1}, [], None, 'HK',
                 {'name': '节点\n'}]:
        add('proxy', 'PUT', [['name', 'G']], body)
    for body in [{'mode': 'rule'}, {'mode': 'Rule'}, {'mode': ['rule']}, {'mode': 'global', 'tun': {}}, {}, None]:
        add('configs', 'PATCH', [], body)
    for resource, method in [('providers/proxy', 'PUT'), ('providers/rule', 'PUT'), ('configs/reload', 'PUT'),
                             ('upgrade', 'POST'), ('cache/fakeip/flush', 'POST'), ('cache/dns/flush', 'POST'),
                             ('connections', 'DELETE'), ('connection', 'DELETE'), ('flow', 'GET'), ('rules', 'GET'),
                             ('providers/rules', 'GET'), ('version', 'GET'), ('configs', 'GET'), ('restart', 'POST'),
                             ('proxies', 'POST'), ('version', 'HEAD'), ('proxies/', 'GET'), ('providers/proxy/healthcheck', 'GET')]:
        add(resource, method, [['name', 'P']], {'x': 1})
    for status, raw in [(204, b''), (200, b' \n\t'), (200, b'not json'), (404, b'404 page not found'), (404, b'{}'),
                        (500, b'oops'), (500, {'message': ''}), (500, {'message': 5}), (408, {'message': 'slow'}),
                        (418, {'message': '长' * 400}), (502, {'success': False, 'code': 'CORE_UNREACHABLE'}),
                        (409, {'success': False, 'code': 'CONTROLLER_UNAVAILABLE'}),
                        (502, {'success': False, 'code': 'UNKNOWN', 'message': 'core text'}),
                        (502, {'success': False, 'code': ['x']}), (502, {'success': 0, 'code': 'CORE_UNREACHABLE'}),
                        (401, b'x'), (403, {'ok': True}), (200, b'\xef\xbb\xbf{"a": 1}'), (200, '{"a": 1}'.encode('utf-16')),
                        (200, b'{"a": NaN, "b": 1e400, "c": -0.0}'), (200, b'[1, 2]'), (200, b'"text"'), (200, b'{"a": "\xff"}'),
                        (200, ('{"n": ' + '1' * 4301 + '}').encode()), (302, {'ok': True}), ('raise', b'')]:
        add('version', 'GET', [], reply=(status, raw))
    add('proxies', 'GET', [], reply=(200, {'proxies': {'HK': NODE, 'G': {**NODE, 'now': 'HK', 'all': ['HK'], 'fixed': '',
                                                                          'testUrl': 'u', 'hidden': True, 'icon': 'i'},
                                                        'odd': 'x', 'list': [1]}, 'other': 1}))
    add('proxies', 'GET', [], reply=(200, {'proxies': ['not a dict']}))
    add('providers/proxies', 'GET', [], reply=(200, {'providers': {
        'sub': {'name': 'sub', 'vehicleType': 'HTTP', 'proxies': [NODE, 'odd', {'name': 'x'}]},
        'grp': {'name': 'grp', 'vehicleType': 'Compatible', 'proxies': [NODE, {'type': 'Direct'}, 'odd']},
        'nolist': {'vehicleType': 'Compatible', 'proxies': {'a': 1}}, 'str': 'x', 'none': None}}))
    add('proxy', 'PUT', [['name', 'G']], {'name': 'HK'}, reply=(200, {'proxies': {'HK': NODE}}))
    for version in ['1.8.14-go', '1.8.15-go', None, 5, '', 'abc']:
        agent = {**MIHOMO, 'version': version}
        add('version', 'GET', [], agent=agent)
        add('logs', 'GET', [], agent=agent)
    for changes in [{'service_type': 'oxidns'}, {'service_type': None}, {'token': None}, {'token': '令牌'},
                    {'host': 'a/b'}, {'port': 0}, {'host': '::1'}, {'host': '[fe80::1]'}, {'host': 'Example.Test'}]:
        add('version', 'GET', [], agent={**MIHOMO, **changes})
    agent = dict(MIHOMO)
    del agent['service_type']
    add('version', 'GET', [], agent=agent)
    del agent['token']
    add('version', 'GET', [], agent=agent)
    return cases


def panel_reference():
    out = []
    for case in panel_cases():
        reply = [case['reply'][0], base64.b64decode(case['reply'][1])]
        manager = Manager(case['agent'])
        result = run({'reply': reply}, lambda: panel.call(manager, case['agent'].get('id'), case['resource'],
                                                          case['method'], MultiDict(case['args']), case['body']))
        out.append({**case, **result})
    OUT['panel'] = out
    OUT['newer_release'] = [[a, b, panel._newer_release(a, b)] for a, b in [
        ('v1.19.33', 'v1.19.32'), ('v1.19.32', 'v1.19.32'), ('1.20.0', 'v1.19.99'), ('v１.19.40', 'v1.19.33'),
        ('v1.19.33-rc', 'v1.19.32'), ('alpha-1', 'v1.0.0'), ('v1.19.100', 'v1.19.99'), ('vv1.2.3', 'v1.2.2'),
        ('v1.2', 'v1.1.9'), ('v01.02.03', 'v1.2.2'), ('v' + '9' * 30 + '.0.0', 'v1.0.0'), ('V1.2.3', 'v1.2.2'),
        ('v1.2.3\n', 'v1.2.2')]]
    version_texts = ['v1.19.33', 'v1.19.33\n', '  alpha-abc  ', '<html>', '', 'v' * 64, 'v' * 65, 'a b', '\xff\xfe',
                     ' v1 ', '-v1', 'v1.2+build_3', 'ｖ1']
    out = []
    for text in version_texts:
        version = text.encode('latin-1', 'replace') if text == '\xff\xfe' else text.encode()
        if text == '\xff\xfe':
            version = b'\xff\xfe'
        decoded = version.decode('utf-8', 'replace').strip()
        out.append([base64.b64encode(version).decode(), decoded if panel.CORE_VERSION_PATTERN.fullmatch(decoded) else None])
    OUT['core_version_texts'] = out


DNS = {'id': 'dns', 'name': 'DNS', 'host': '127.0.0.1', 'port': 8082, 'service_type': 'oxidns',
       'version': '1.8.60-go', 'token': 'dns-token'}


def cache_reference():
    out = []

    def add(method, tags, reply=(200, {'success': True, 'flushed': ['probe']}), agent=None):
        case = {'agent': agent or DNS, 'method': method, 'tags': tags,
                'reply': [reply[0], base64.b64encode(enc(reply[1])).decode()]}
        manager = Manager(case['agent'])
        result = run({'reply': [reply[0], enc(reply[1])]},
                     lambda: caches.call(manager, case['agent'].get('id'), method, tags))
        out.append({**case, **result})

    for tags in [['probe'], ['a.b-c_d', 'x'], ['_x'], ['x_'], ['a..b'], ['a' * 64], ['a' * 65], ['a.' * 31 + 'bb'],
                 ['a.' * 32 + 'b'], ['é'], ['a\n'], ['probe', 'probe'], [1, 1], [1, True], ['a', 1], [], ['x'] * 64,
                 [f't{i}' for i in range(65)], 'probe', None, [[1]], [{}], ['a', ['b']]]:
        add('POST', tags)
    add('GET', None, reply=(200, {'success': True, 'caches': ['main', 'probe']}))
    add('GET', ['probe'])
    add('DELETE', None)
    for status, body in [(200, {'success': True, 'flushed': ['other']}), (200, {'success': True, 'flushed': ['probe', 'probe']}),
                         (200, {'success': 1, 'flushed': ['probe']}), (201, {'success': True, 'flushed': ['probe']}),
                         (404, b'404'), (404, {'success': False, 'code': 'NOPE'}), (404, {'success': False, 'code': 'CACHE_UNKNOWN'}),
                         (404, {'success': False, 'code': ['x']}), (500, {'success': False, 'code': ['x']}),
                         (503, {'success': False, 'code': 'CORE_BUSY', 'flushed': ['probe']}),
                         (409, {'success': False, 'flushed': None}), (400, {'code': 'API_NOT_CONFIGURED'}),
                         (504, {'success': False, 'code': 'CORE_UNREACHABLE', 'flushed': ['../x']}),
                         (200, {'success': False, 'code': 'SERVICE_BUSY', 'flushed': ['probe']}),
                         (502, {'success': False, 'code': 'CACHE_FLUSH_FAILED', 'flushed': 'probe'}),
                         (200, b''), (200, b'[]'), (200, b' ' * 16385), (301, b'{}'), (403, b'{}'), (200, b'\xef\xbb\xbf{}'),
                         ('raise', b'')]:
        add('POST', ['probe'], reply=(status, body))
    for changes in [{'version': '1.8.35-go'}, {'service_type': 'mihomo'}, {'token': None}, {'token': '令牌'}, {'host': 'a/b'}]:
        add('POST', ['probe'], agent={**DNS, **changes})
    agent = dict(DNS)
    del agent['token']
    add('POST', ['probe'], agent=agent)
    OUT['cache'] = out


def task(status='running', **fields):
    return {'id': 'a' * 32, 'status': status, 'phase': 'downloading' if status == 'running' else 'done',
            'downloaded': 0, 'download_total': 2, 'reloaded': 0, 'reload_total': 1, **fields}


def rule_update_reference():
    out = []

    def add(method, paths=None, reply=(202, {'success': True, 'task': task()}), agent=None):
        case = {'agent': agent or DNS, 'method': method, 'paths': paths,
                'reply': [reply[0], base64.b64encode(enc(reply[1])).decode()]}
        manager = Manager(case['agent'])
        result = run({'reply': [reply[0], enc(reply[1])]},
                     lambda: updates.call(manager, case['agent'].get('id'), method, paths))
        out.append({**case, **result})

    for paths in [None, ['a'], [], [''], [1], 'a', ['a'] * 1000, ['a'] * 1001, ['規則/甲.txt']]:
        add('POST', paths)
    add('GET', ['a'])
    add('PUT')
    add('GET')
    tasks = [task(), task('idle', id='', phase='idle', download_total=0, reload_total=0), task('idle'),
             task('succeeded', downloaded=2, reloaded=1), task('succeeded'), task('succeeded', downloaded=2, reloaded=1, phase='reloading'),
             task('failed', code='DOWNLOAD_FAILED'), task('failed', code='NOPE'), task('failed', code=['x']), task('failed'),
             task(downloaded=True), task(downloaded=1.0), task(downloaded=-1), task(downloaded=1_000_001, download_total=1_000_001),
             task(download_total=1_000_000), task(id='x' * 96), task(id='x' * 97), task(id='a b'), task(id=5),
             task(status='paused'), task(phase='verifying'), 'task', None, {**task(), 'extra': 'secret'}]
    for value in tasks:
        add('GET', reply=(200, {'success': True, 'task': value}))
    for status, body in [(202, {'success': True, 'task': task()}), (201, {'success': True, 'task': task()}),
                         (200, {'success': 1, 'task': task()}), (404, {'success': True, 'task': task()}), (404, b'x'),
                         (409, {'success': False, 'code': 'RULE_UPDATE_BUSY'}), (409, {'success': False, 'code': ['x']}),
                         (500, {'message': 'x'}), (503, {'success': False, 'code': 'CORE_BUSY'}),
                         (200, {'success': False, 'code': 'SNAPSHOT_CHANGED'}), (418, {'success': False}),
                         (200, b'[]'), (200, b'x' * 16385), (302, b'{}'), (401, b'{}'), ('raise', b'')]:
        add('GET', reply=(status, body))
    for changes in [{'version': '1.8.29-go'}, {'version': '1.8.34-go'}, {'service_type': 'mihomo'}, {'token': None},
                    {'oxidns_ha': 'yes'}, {'oxidns_ha': {'enabled': 1}},
                    {'oxidns_ha_publication': {'status': 'publishing'}},
                    {'oxidns_ha_publication': {'status': 'succeeded'}}]:
        add('POST', agent={**DNS, **changes})
        add('POST', ['a'], agent={**DNS, **changes})
    OUT['rule_update'] = out


# --- ranking history scenario ------------------------------------------------------

DAY, HOUR, SLOT = history.DAY, history.HOUR, history.SLOT
T0 = 1_793_000_000 - 1_793_000_000 % DAY


class FakeAgent:
    def __init__(self, records, cap=10000):
        self.records = sorted(records)
        self.cap = cap

    def window(self, since, until, blocked=False):
        inside = [r for r in self.records if since <= r[0] < until and (not blocked or r[3])]
        return inside[-self.cap:] if self.cap else inside

    def section(self, records, index, rows):
        counts = Counter(r[index] for r in records)
        top = sorted(counts.items(), key=lambda item: (-item[1], item[0]))[:rows]
        return {'sample_size': len(records), 'at_limit': len(records) == self.cap,
                'rows': [{'key': k, 'count': c, 'share': 0} for k, c in top]}

    def __call__(self, agent, endpoint, params=None, **kwargs):
        since, until = int(params['since_ms']) // 1000, int(params['until_ms']) // 1000
        rows = int(params['rows'])
        sample = self.window(since, until)
        return {'success': True, 'recorder': params['recorder'], 'retention_days': 7,
                'clients': self.section(sample, 1, rows), 'domains': self.section(sample, 2, rows),
                'blocked': {'available': True, **self.section(self.window(since, until, True), 2, rows)}}


def answer_dump(answer):
    return dump({**answer, 'rows': {k: [[r['key'], r['count']] for r in v] for k, v in answer['rows'].items()}})


def db_dump(path):
    connection = sqlite3.connect(path)
    try:
        out = {'journal_mode': connection.execute('PRAGMA journal_mode').fetchone()[0],
               'master': [list(r) for r in connection.execute('SELECT type, name, tbl_name, sql FROM sqlite_master ORDER BY name')]}
        for table in ('ranking_periods', 'ranking_rows', 'ranking_slots'):
            out[table] = [list(r) for r in connection.execute(f'SELECT rowid, * FROM {table} ORDER BY rowid')]
    finally:
        connection.close()
    with open(path, 'rb') as handle:
        header = handle.read(100)
    out['header_18_20'] = list(header[18:20])
    out['files'] = sorted(os.listdir(os.path.dirname(path)))
    return dump(out)


def records_for(seed_base, start, count, clients, domains):
    out = []
    for i in range(count):
        n = (i * 7919 + seed_base) % 100003
        out.append([start + (n * 37) % SLOT + (i % 4) * SLOT, f'192.0.2.{n % clients + 1}',
                    f'd{(n * 31) % domains}.例子.test.' if n % 5 == 0 else f'd{(n * 31) % domains}.example.test.',
                    n % 3 == 0])
    return out


def ranking_reference():
    ops = []
    dense = [[T0 + i * 60 // 12_000, '192.0.2.1', f'busy{i % 3}.test.', i % 2 == 0] for i in range(12_000)]
    split = [[T0 + SLOT + i * SLOT // 25_000, f'192.0.2.{i % 7 + 1}', f'd{i % 40}.example.test.', i % 9 == 0] for i in range(25_000)]
    wide = [[T0 + 2 * SLOT + i * 840 // 2_100, f'192.0.2.{i % 200 + 1}', f'w{i}.test.', i % 4 == 0] for i in range(2_100)]
    ops.append({'op': 'collect', 'agent': 'dns', 'recorder': 'records', 'records': dense + split + wide,
                'now': T0 + 4 * SLOT + 120, 'budget': 10, 'retention': 1})
    ops.append({'op': 'collect', 'agent': 'dns', 'recorder': 'records', 'records': records_for(1, T0 + DAY, 6000, 50, 900),
                'now': T0 + DAY + 4 * SLOT + 120, 'budget': 10, 'retention': 1})
    ops.append({'op': 'add', 'agent': 'dns', 'recorder': 'other', 'start': T0 + 3 * HOUR,
                'slot': {'records': 3, 'blocked': None, 'partial': True,
                         'rows': {'clients': [['192.0.2.9', 3]], 'domains': [['z.test.', 2], ['ä.test.', 1], ['A.test.', 1]]}}})
    ops.append({'op': 'add', 'agent': 'dns', 'recorder': 'other', 'start': T0 + 3 * HOUR, 'slot': {
        'records': 1, 'blocked': 1, 'partial': False, 'rows': {'clients': [['192.0.2.9', 1]]}}})
    ops.append({'op': 'add', 'agent': 'gone', 'recorder': 'records', 'start': T0, 'slot': {
        'records': 1, 'blocked': 0, 'partial': False, 'rows': {'blocked': [['b.test.', 1]]}}})
    for since, until in [(T0, T0 + HOUR), (T0, T0 + 2 * DAY), (T0 + HOUR, T0 + DAY + HOUR), (0, 1), (T0 + 30, T0 + 3600 + 30)]:
        ops.append({'op': 'query', 'agent': 'dns', 'recorder': 'records', 'since': since, 'until': until})
    ops.append({'op': 'query', 'agent': 'dns', 'recorder': 'other', 'since': T0, 'until': T0 + DAY})
    ops.append({'op': 'has_any', 'agent': 'dns', 'recorder': 'other'})
    ops.append({'op': 'has_any', 'agent': 'dns', 'recorder': 'nothing'})
    ops.append({'op': 'fetched', 'agent': 'dns', 'recorder': 'records', 'starts': [T0, T0 + SLOT, T0 + 5 * SLOT, T0 + DAY]})
    ops.append({'op': 'maintain', 'agents': ['dns'], 'now': T0 + 5 * DAY})
    ops.append({'op': 'dump'})
    ops.append({'op': 'maintain', 'agents': ['dns'], 'now': T0 + 9 * DAY + 10})
    for since, until in [(T0, T0 + 2 * DAY), (T0 + DAY, T0 + 2 * DAY), (T0, T0 + DAY + HOUR)]:
        ops.append({'op': 'query', 'agent': 'dns', 'recorder': 'records', 'since': since, 'until': until})
    ops.append({'op': 'dump'})
    ops.append({'op': 'add', 'agent': 'dns', 'recorder': 'records', 'start': T0 + 2 * HOUR, 'slot': {
        'records': 5, 'blocked': 2, 'partial': False, 'rows': {'domains': [['w5.test.', 9], ['new.test.', 4]]}}})
    ops.append({'op': 'maintain', 'agents': ['dns'], 'now': T0 + 9 * DAY + HOUR})
    ops.append({'op': 'query', 'agent': 'dns', 'recorder': 'records', 'since': T0, 'until': T0 + 2 * DAY})
    ops.append({'op': 'maintain', 'agents': ['dns'], 'now': T0 + 92 * DAY})
    ops.append({'op': 'query', 'agent': 'dns', 'recorder': 'records', 'since': 0, 'until': T0 + 100 * DAY})
    ops.append({'op': 'dump'})

    with tempfile.TemporaryDirectory() as directory:
        store = history.RankingHistory(directory)
        results = []
        for op in ops:
            kind = op['op']
            if kind == 'collect':
                monitor._request = FakeAgent([tuple(r) for r in op['records']])
                stored = history.collect_agent(store, {'id': op['agent'], 'name': op['agent'],
                                                       'version': history.HISTORY_AGENT_VERSION},
                                               [op['recorder']], now=op['now'], budget=op['budget'],
                                               retention_days=op['retention'], pause=0)
                results.append(dump([stored, store.caught_up.get(op['agent'])]))
            elif kind == 'add':
                slot = dict(op['slot'])
                slot['rows'] = {k: dict(v) for k, v in slot['rows'].items()}
                results.append(dump(store.add_slot(op['agent'], op['recorder'], op['start'], slot)))
            elif kind == 'query':
                results.append(answer_dump(store.query(op['agent'], op['recorder'], op['since'], op['until'])))
            elif kind == 'has_any':
                results.append(dump(store.has_any(op['agent'], op['recorder'])))
            elif kind == 'fetched':
                results.append(dump(sorted(store.fetched(op['agent'], op['recorder'], op['starts']))))
            elif kind == 'maintain':
                store.maintain(op['agents'], now=op['now'])
                results.append(dump(store._version))
            elif kind == 'dump':
                results.append(db_dump(store.path))
        OUT['ranking'] = {'ops': ops, 'results': results}
    OUT['last_complete_slot_end'] = [[n, history.last_complete_slot_end(n)] for n in
                                     [0, 59, 60, 61, 959, 960, T0, T0 + 59, T0 + 60, T0 + 961, -1, -900]]


panel_reference()
cache_reference()
rule_update_reference()
ranking_reference()
path = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'a07_reference.json.gz')
with gzip.open(path, 'wt', encoding='utf-8') as handle:
    json.dump(OUT, handle, sort_keys=True)
print('panel', len(OUT['panel']), 'cache', len(OUT['cache']), 'rule_update', len(OUT['rule_update']),
      'ranking ops', len(OUT['ranking']['ops']))
