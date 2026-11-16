"""subscriptions.py CRUD, stored cache views, selectors, cleanup and refusal guards."""
import copy
import json
from concurrent.futures import ThreadPoolExecutor

from compare import database, files
from harness import Pair, DesignError, json_body
from cases.config_backup import message
from cases.helpers import deny_writes, exception_policy, profile_policy

SUB = {'id': 'sub-a', 'name': '订阅甲🧪', 'url': 'http://source.example/a', 'interval': 0, 'enabled': True}
SUBS = [SUB, {**SUB, 'id': 'sub-b', 'name': '订阅乙', 'url': 'http://source.example/b'},
        {**SUB, 'id': 'sub-c', 'name': '订阅丙', 'url': 'http://source.example/c', 'enabled': False}]
DERIVED = ('cached_node_count', 'cached_total_count', 'cached_updated_at', 'traffic', 'traffic_at', 'last_fetch')
CACHE_NODES = [{'id': 'cached-a', 'name': '甲🧪', 'type': 'socks5', 'server': 'node.example', 'port': 1080},
               {'id': 'cached-b', 'name': '乙', 'type': 'socks5', 'server': 'node.example', 'port': 1081}]
STAMP = '2026-01-01T00:00:00Z'


def fixture(extra=None):
    value = {'shared': {'subscriptions': copy.deepcopy(SUBS)}}
    value.update(extra or {})
    return value


def put_file(pair, name, data):
    for directory in pair.dirs:
        path = directory / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(json_body(data))


def storage(pair):
    return database(pair.dirs[0])['fields']['shared//subscriptions']['value']


def crud(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R213-B01', 'GET /api/subscriptions', expect=200, writes=True, check=lambda r: require(r.json() == [], 'empty subscriptions'))
        for case, changes in [('S01', {}), ('S02', {'url': 'http://source.example/a\nhttp://source.example/b'}),
                               ('S03', {}), ('B01', {'interval': 0}), ('B02', {'interval': 60}),
                               ('B03', {'interval': 604800}), ('B04', {key: {'synthetic': True} for key in DERIVED})]:
            data = {**SUB, 'id': 'created-' + case, **changes}
            def check(reply):
                saved = next(item for item in storage(pair) if item['id'] == data['id'])
                if any(field in saved for field in DERIVED):
                    raise DesignError('Python stored derived subscription fields')
                if saved.get('health_check', {}).get('enable') is not True:
                    raise DesignError('Python creation did not initialize provider health')
            pair.request('R214-' + case, 'POST /api/subscriptions', body=json_body(data), expect=200, writes=True, check=check)
        pair.request('R213-S02', 'GET /api/subscriptions', expect=200, writes=True)
    with Pair(binary, recorder, options=fixture()) as pair:
        for case, data in [('S01', {**SUB, 'id': 'must-ignore', 'name': '替换🧪'}),
                           ('B01', {**SUB, 'name': '中文🧪'}), ('B02', {**SUB, **{k: 'derived-must-not-persist' for k in DERIVED}})]:
            def check(reply):
                saved = next(item for item in storage(pair) if item['id'] == 'sub-a')
                if saved.get('id') != 'sub-a' or any(field in saved for field in DERIVED):
                    raise DesignError('Python PUT changed stable ID or retained derived fields')
            pair.request('R217-' + case, 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                         body=json_body(data), expect=200, writes=True, check=check)
        pair.request('R217-F18', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/missing', body='{}', expect=404, writes=True)
        pair.request('R216-S02', 'DELETE /api/subscriptions/<sub_id>', '/api/subscriptions/missing', expect=200, writes=True)
        pair.request('R216-B02', 'DELETE /api/subscriptions/<sub_id>', '/api/subscriptions/sub-b', expect=200, writes=True)
        pair.request('R216-B01', 'DELETE /api/subscriptions/<sub_id>', '/api/subscriptions/sub-b', expect=200, writes=True)
        pair.policy.times.add('body.timestamp')
        pair.request('R224-S01', 'GET /api/subscriptions/test', expect=200, writes=True)
        pair.request('R224-B01', 'GET /api/subscriptions/test', expect=200, writes=True)


def validation(binary, recorder):
    vectors = []
    for rid, method in ((214, 'POST'), (217, 'PUT')):
        for case, data, expected, variant in [
            ('F08', [], '订阅请求必须是对象', ''),
            ('F09', {k: v for k, v in SUB.items() if k != 'name'}, '订阅 name 不能为空', ''),
            ('F10', {**SUB, 'name': ' \t'}, '订阅 name 不能为空', ''),
            ('F11', {k: v for k, v in SUB.items() if k != 'url'}, '订阅 url 不能为空', ''),
            ('F12', {**SUB, 'url': ' '}, '订阅 url 不能为空', '')]:
            vectors.append((rid, method, case, data, expected, variant))
        for field, case in [('name', 'F13'), ('url', 'F14')]:
            for value in (None, True, 1, [], {}):
                vectors.append((rid, method, case, {**SUB, field: value}, f'订阅 {field} 不能为空', repr(value)))
        for value in (1, True, [], {}):
            vectors.append((rid, method, 'F15', {**SUB, 'regex_filter': value}, '节点筛选正则必须是字符串', repr(value)))
        for value in (-1, 59, 604801, 2592000, True, 60.0, '60', None):
            vectors.append((rid, method, 'F16', {**SUB, 'interval': value}, '订阅源更新间隔必须为 0（关闭自动更新）或 60 ~ 604800 秒的整数', repr(value)))
    def execute(vector):
        rid, method, case, data, expected, variant = vector
        with Pair(binary, recorder, options=fixture()) as pair:
            before = (database(pair.dirs[0]), files(pair.dirs[0]))
            def check(reply):
                message(expected)(reply)
                if before != (database(pair.dirs[0]), files(pair.dirs[0])):
                    raise DesignError('Python invalid subscription write changed committed storage')
            pair.request(f'R{rid:03}-' + case, method + ' /api/subscriptions' + ('/<sub_id>' if method == 'PUT' else ''),
                         '/api/subscriptions/sub-a' if method == 'PUT' else '/api/subscriptions',
                         body=json_body(data), variant=variant, expect=400, writes=True, check=check)
    with ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(execute, vectors))
    with Pair(binary, recorder, options=fixture()) as pair:
        pair.request('R214-F17', 'POST /api/subscriptions', body=json_body(SUB), expect=400, writes=True)
        pair.request('R214-B03', 'POST /api/subscriptions', body=json_body({**SUB, 'id': 'max-overflow', 'interval': 2592000}),
                     variant='original-matrix-value-is-invalid', expect=400, writes=True)
        for rid, method, path in [(214, 'POST', '/api/subscriptions'), (217, 'PUT', '/api/subscriptions/sub-a')]:
            policy = exception_policy(pair, '节点筛选正则无效: ')
            pair.request(f'R{rid:03}-F15', method + ' /api/subscriptions' + ('/<sub_id>' if method == 'PUT' else ''), path,
                         body=json_body({**SUB, 'regex_filter': '['}), variant='regex-syntax', expect=400, writes=True, policy=policy)


def caches(binary, recorder):
    options = fixture({'shared': {'subscriptions': [{**SUB, 'regex_filter': '甲'}], 'nodes': [
        {'id': 'imported-a', 'name': '已导入', 'type': 'socks5', 'server': 'node.example', 'port': 1080, 'subscription_id': 'sub-a'}]}})
    with Pair(binary, recorder, options=options) as pair:
        pair.request('R219-S01', 'GET /api/subscriptions/<sub_id>/nodes', '/api/subscriptions/sub-a/nodes', expect=200, writes=True)
        pair.request('R219-S03', 'GET /api/subscriptions/<sub_id>/nodes', '/api/subscriptions/sub-a/nodes?source=cache', expect=200, writes=True)
        pair.request('R219-F08', 'GET /api/subscriptions/<sub_id>/nodes', '/api/subscriptions/missing/nodes', expect=404, writes=True)
        payload = {'subscription_id': 'sub-a', 'updated_at': STAMP, 'count': 2, 'nodes': CACHE_NODES, 'metadata': {'url': SUB['url']}}
        put_file(pair, 'shared/subscribes/sub-a.json', payload)
        put_file(pair, 'shared/subscription_health.json', {'sub-a': {'history': [{'at': STAMP, 'ok': True}],
                                                                  'traffic': {'upload': 1, 'download': 2}, 'traffic_at': STAMP}})
        pair.request('R213-S01', 'GET /api/subscriptions', expect=200, writes=True,
                     check=lambda r: require(r.json()[0]['cached_node_count'] == 1 and r.json()[0]['cached_total_count'] == 2, 'filtered/raw cache counts'))
        pair.request('R221-S01', 'GET /api/subscriptions/health', expect=200, writes=True)
        pair.request('R219-S02', 'GET /api/subscriptions/<sub_id>/nodes', '/api/subscriptions/sub-a/nodes?source=cache',
                     expect=200, writes=True, check=lambda r: require(r.json()['count'] == 1 and r.json()['cache_available'], 'cached preview'))
        for case, changes in [('S04', {'metadata': {'url': 'http://other.example'}}),
                               ('B01', {'nodes': [], 'count': 0}), ('B02', {'metadata': None})]:
            put_file(pair, 'shared/subscribes/sub-a.json', {**payload, **changes})
            pair.request('R219-' + case, 'GET /api/subscriptions/<sub_id>/nodes', '/api/subscriptions/sub-a/nodes?source=cache',
                         expect=200, writes=True)
        put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'count': 12, 'updated_at': STAMP})
        pair.request('R213-B02', 'GET /api/subscriptions', expect=200, writes=True,
                     check=lambda r: require(r.json()[0]['cached_total_count'] == 12, 'legacy count-only cache'))
    with Pair(binary, recorder, options=fixture()) as pair:
        pair.request('R221-S02', 'GET /api/subscriptions/health', expect=200, writes=True)
        pair.request('R221-B02', 'GET /api/subscriptions/health', expect=200, writes=True)
    with Pair(binary, recorder) as pair:
        pair.request('R221-B01', 'GET /api/subscriptions/health', expect=200, writes=True)


def require(condition, description):
    if not condition:
        raise DesignError('Python subscription fixture did not demonstrate ' + description)


def cleanup(binary, recorder):
    with Pair(binary, recorder, options=fixture()) as pair:
        receipt = {'subscription:sub-a': {'last_status': 'success', 'run_count': 4}, 'unrelated:synthetic': {'run_count': 7}}
        put_file(pair, 'task-status.json', receipt)
        put_file(pair, 'shared/subscription_health.json', {'sub-a': {'history': [{'at': STAMP, 'ok': False}]}, 'sub-b': {'history': []}})
        put_file(pair, 'shared/subscribes/sub-a.json', {'nodes': CACHE_NODES, 'metadata': {'url': SUB['url']}, 'count': 2})
        pair.request('R217-S02', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a', body=json_body(SUB),
                     expect=200, writes=True, check=lambda r: require(json.loads((pair.dirs[0] / 'task-status.json').read_text()) == receipt, 'unchanged-URL task retention'))
        pair.request('R217-S03', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                     body=json_body({**SUB, 'url': 'http://source.example/changed'}), expect=200, writes=True,
                     check=lambda r: require(json.loads((pair.dirs[0] / 'task-status.json').read_text()) == {'unrelated:synthetic': {'run_count': 7}}, 'changed-URL task cleanup'))
        def deleted(reply):
            require(not (pair.dirs[0] / 'shared/subscribes/sub-a.json').exists(), 'cache deletion')
            health = json.loads((pair.dirs[0] / 'shared/subscription_health.json').read_text())
            require('sub-a' not in health and 'sub-b' in health, 'only selected health removed')
        pair.request('R216-S01', 'DELETE /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a', expect=200, writes=True, check=deleted)


def references(binary, recorder):
    for case, shape in [('F08', 'default-group'), ('F09', 'aggregation'), ('F10', 'other-group')]:
        options = fixture()
        group = {'id': 'group-ref', 'name': '引用🧪', 'type': 'select', 'subscriptions': ['sub-a']}
        if shape == 'aggregation':
            options['shared']['subscription_aggregations'] = [{'id': 'agg-ref', 'name': '引用聚合', 'subscriptions': ['sub-a'], 'nodes': []}]
        else:
            options['profile' if shape == 'default-group' else 'other_profile'] = {'proxy_groups': [group]}
        with Pair(binary, recorder, options=options) as pair:
            pair.policy = profile_policy(pair)
            pair.request('R215-' + ('S01' if shape == 'default-group' else 'B01'), 'GET /api/subscriptions/<resource_id>/health-impact',
                         '/api/subscriptions/sub-a/health-impact', variant=shape, expect=200, writes=True)
            pair.request('R216-' + case, 'DELETE /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a', expect=409, writes=True)
    with Pair(binary, recorder, options=fixture({'profile': {'proxy_groups': [group]}, 'other_profile': {'proxy_groups': [group]}})) as pair:
        pair.request('R215-S02', 'GET /api/subscriptions/<resource_id>/health-impact', '/api/subscriptions/sub-a/health-impact',
                     expect=200, writes=True, check=lambda r: require(len(r.json()) == 2, 'two profile usages'))
        for case, identity in [('S03', 'sub-b'), ('F08', 'missing')]:
            pair.request('R215-' + case, 'GET /api/subscriptions/<resource_id>/health-impact', '/api/subscriptions/' + identity + '/health-impact',
                         expect=200, writes=True, check=lambda r: require(r.json() == [], 'empty resource usages'))


def reorder(binary, recorder):
    with Pair(binary, recorder, options=fixture()) as pair:
        for case, data in [('S01', {'ids': ['sub-c', 'sub-a'], 'position': 'top'}),
                           ('S02', {'ids': ['sub-c'], 'position': 'bottom'}),
                           ('S03', {'subscriptions': [{'id': 'sub-b'}, {'id': 'sub-a'}]}),
                           ('B01', {'ids': []}), ('B02', {'subscriptions': [{'id': 'sub-a', 'name': 'must-not-overwrite', 'cached_node_count': 123}]})]:
            pair.request('R223-' + case, 'POST /api/subscriptions/reorder', body=json_body(data), expect=200, writes=True)
        for case, data in [('F08', {'ids': ['sub-a', 'sub-a']}), ('F09', {'ids': [1]}), ('F10', {'ids': 'sub-a'}), ('F11', {'ids': ['missing']})]:
            pair.request('R223-' + case, 'POST /api/subscriptions/reorder', body=json_body(data), expect=404, writes=True)
        pair.request('R223-F12', 'POST /api/subscriptions/reorder', body='[1]', expect=500, writes=True)


GROUPS = {'subscriptions-crud': crud, 'subscriptions-validation': validation, 'subscriptions-caches': caches,
          'subscriptions-cleanup': cleanup, 'subscriptions-references': references, 'subscriptions-reorder': reorder}
