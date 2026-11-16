"""Aggregation CRUD, usage, cache-only counts and ordering through public HTTP."""
import copy
import re

from compare import Difference, database
from harness import Pair, json_body
from cases.groups import NODES
from cases.helpers import deny_writes
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file, require

AGG = {'id': 'agg-a', 'name': '聚合甲🧪', 'subscriptions': ['sub-a'], 'nodes': ['n-a'], 'enabled': True}
DERIVED = ('node_count', 'total_node_count', 'subscription_node_counts', 'loading_count')


def options(aggregations=None):
    return {'shared': {'subscriptions': [{**SUB, 'url': ''}], 'nodes': copy.deepcopy(NODES) + [
        {'id': 'disabled-node', 'name': '停用', 'type': 'socks5', 'server': '192.0.2.9', 'port': 1080, 'enabled': False}],
        'subscription_aggregations': copy.deepcopy([AGG] if aggregations is None else aggregations)}}


def rows(pair):
    return database(pair.dirs[0])['fields']['shared//subscription_aggregations']['value']


def configure(pair):
    for field in ('created_at', 'updated_at'):
        for prefix in ('body.', 'body.data.'):
            pair.policy.times.add(prefix + field)
    for index, item in enumerate(rows(pair)):
        for field in ('created_at', 'updated_at'):
            if field in item:
                pair.policy.times.add(f'db.fields.shared//subscription_aggregations.value[{index}].' + field)
                pair.policy.times.add(f'body[{index}].' + field)


def created(pair):
    def capture(replies, policy):
        left = replies[0].json()['data']['id']
        payload = replies[1].json()
        data = payload.get('data') if isinstance(payload, dict) else None
        right = data.get('id') if isinstance(data, dict) else None
        if not isinstance(right, str) or not re.fullmatch(r'agg_[0-9a-f]{8}', right):
            return [Difference('body.data.id', 'invalid-aggregation-id', '<agg_8hex>', right)]
        try:
            policy.bind(left, right, r'agg_[0-9a-f]{8}', 'created-aggregation')
        except (AssertionError, ValueError):
            return [Difference('body.data.id', 'reused-aggregation-id', left, right)]
        return []
    return capture


def crud(binary, recorder):
    with Pair(binary, recorder, options=options([])) as pair:
        pair.request('R001-S02', 'GET /api/aggregations', expect=200, writes=True,
                     check=lambda r: require(r.json() == [], 'aggregation list starts empty'))
        vectors = [('S01', {'subscriptions': ['sub-a'], 'nodes': []}), ('S02', {'subscriptions': [], 'nodes': ['n-a']}),
                   ('S03', {'subscriptions': ['sub-a'], 'nodes': ['n-a']}), ('B01', {'name': '中文🧪"\\,'}),
                   ('B02', {key: 'must-remove' for key in DERIVED}), ('B03', {'subscriptions': [], 'nodes': []})]
        for case, fields in vectors:
            body = {**AGG, 'name': '新聚合-' + case, 'id': 'caller-id-must-be-replaced', **fields}
            def check(reply):
                item = reply.json()['data']
                require(re.fullmatch(r'agg_[0-9a-f]{8}', item['id']) and item in rows(pair), 'generated identity is newly allocated and response equals persisted object')
                require(not any(key in item for key in DERIVED) and item['health_check']['enable'] is True,
                        'creation removes derived stats and initializes provider health')
                configure(pair)
            pair.request('R002-' + case, 'POST /api/aggregations', body=json_body(body), expect=200, writes=True,
                         check=check, capture=created(pair))
        pair.request('R001-S01', 'GET /api/aggregations', expect=200, writes=True)
    legacy = {**AGG, **{field: 42 for field in DERIVED}, 'extension': {'中文🧪': [False, 0, None]}}
    with Pair(binary, recorder, options=options([legacy])) as pair:
        pair.request('R001-B01', 'GET /api/aggregations', expect=200, writes=True,
                     check=lambda r: require(r.json()[0]['node_count'] == 42, 'list preserves legacy stored statistics'))
        for case in ('S01', 'B01'):
            pair.request('R004-' + case, 'GET /api/aggregations/<agg_id>', '/api/aggregations/agg-a', expect=200, writes=True,
                         check=lambda r: require(not any(key in r.json() for key in DERIVED[:3]) and r.json()['loading_count'] == 42,
                                                 'item read strips three count fields, retaining loading_count and extensions'))
        for case, fields in [('S01', {'name': '整对象替换'}), ('B01', {'id': 'wrong-id'}), ('B02', {key: 9 for key in DERIVED})]:
            data = {**AGG, **fields}
            def check(reply):
                item = rows(pair)[0]
                require(item['id'] == 'agg-a' and 'extension' not in item and not any(key in item for key in DERIVED),
                        'PUT replaces the whole object, preserving only path identity and stripping stats')
                configure(pair)
            pair.request('R005-' + case, 'PUT /api/aggregations/<agg_id>', '/api/aggregations/agg-a', body=json_body(data),
                         expect=200, writes=True, check=check)
        for rid, method in [(4, 'GET'), (5, 'PUT')]:
            pair.request(f'R{rid:03}-' + ('F08' if rid == 4 else 'F09'), method + ' /api/aggregations/<agg_id>',
                         '/api/aggregations/missing', body='{}' if method == 'PUT' else None, expect=404, writes=True)
        for case, ident in [('S01', 'agg-a'), ('S02', 'missing'), ('B01', 'agg-a')]:
            pair.request('R003-' + case, 'DELETE /api/aggregations/<agg_id>', '/api/aggregations/' + ident, expect=200, writes=True)


def validation(binary, recorder):
    vectors = [('F08', [], 400, 'nonobject'), ('F09', {**AGG, 'name': ''}, 200, 'empty-name-accepted'),
               ('F09', {**AGG}, 200, 'duplicate-name-accepted'), ('F10', {**AGG, 'subscriptions': ['missing']}, 400, ''),
               ('F11', {**AGG, 'nodes': ['missing']}, 400, ''),
               ('F12', {**AGG, 'health_check': {'interval': 0}}, 400, ''),
               ('F13', {**AGG, 'regex_filter': '['}, 200, 'invalid-stored-filter-accepted')]
    for case, body, expected, variant in vectors:
        with Pair(binary, recorder, options=options()) as pair:
            def check(reply):
                if expected == 200:
                    require(reply.json()['data'] in rows(pair), 'legacy creation does not validate names or stored aggregation regex')
                    configure(pair)
            pair.request('R002-' + case, 'POST /api/aggregations', body=json_body(body), expect=expected, writes=True,
                         check=check, capture=created(pair) if expected == 200 else None, variant=variant)
    for case, body, expected in [('F08', [], 400), ('F10', {**AGG, 'nodes': ['missing']}, 400)]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.request('R005-' + case, 'PUT /api/aggregations/<agg_id>', '/api/aggregations/agg-a', body=json_body(body), expect=expected, writes=True)
    with Pair(binary, recorder, options=options()) as pair:
        with deny_writes(pair):
            pair.request('R005-F11', 'PUT /api/aggregations/<agg_id>', '/api/aggregations/agg-a', body=json_body({**AGG, 'name': '存储失败'}),
                         expect=500, writes=True)
    for count in (0, 1, 2):
        opts = options()
        group = {'id': 'usage-group', 'name': '用途组🧪', 'type': 'select', 'aggregations': ['agg-a']}
        if count:
            opts['profile'] = {'proxy_groups': [group]}
        if count == 2:
            opts['other_profile'] = {'proxy_groups': [group]}
        with Pair(binary, recorder, options=opts) as pair:
            pair.request('R009-' + {0: 'S03', 1: 'S01', 2: 'S02'}[count], 'GET /api/aggregations/<resource_id>/health-impact',
                         '/api/aggregations/agg-a/health-impact', expect=200, writes=True)
            if count:
                pair.request('R003-F08', 'DELETE /api/aggregations/<agg_id>', '/api/aggregations/agg-a', expect=409, writes=True, variant='profiles-' + str(count))
            pair.request('R009-F08', 'GET /api/aggregations/<resource_id>/health-impact', '/api/aggregations/missing/health-impact',
                         expect=200, writes=True, variant='profiles-' + str(count))


def count_and_order(binary, recorder):
    for shape, case in [('normal', 'S01'), ('legacy-count', 'S02'), ('empty', 'B01'), ('same-names', 'B02'), ('bad-cache', 'F09')]:
        agg = {**AGG, 'nodes': ['n-a', 'disabled-node']}
        if shape == 'empty':
            agg.update(subscriptions=[], nodes=[])
        with Pair(binary, recorder, options=options([agg])) as pair:
            cache = {'count': 5} if shape == 'legacy-count' else {'nodes': CACHE_NODES, 'count': 999}
            if shape == 'same-names':
                cache['nodes'] = [{**node, 'name': '同名🧪'} for node in CACHE_NODES]
            if shape == 'bad-cache':
                cache = {'count': 'invalid-count', 'nodes': None}
            put_file(pair, 'shared/subscribes/sub-a.json', cache)
            expected = 0 if shape == 'empty' else 6 if shape == 'legacy-count' else 3
            pair.request('R006-' + case, 'GET /api/aggregations/<agg_id>/count', '/api/aggregations/agg-a/count',
                         expect=500 if shape == 'bad-cache' else 200, writes=True, variant=shape,
                         check=None if shape == 'bad-cache' else lambda r: require(r.json()['total_count'] == expected, 'cache count includes enabled manual nodes and never deduplicates names'))
            pair.request('R006-F08', 'GET /api/aggregations/<agg_id>/count', '/api/aggregations/missing/count', expect=404, writes=True, variant=shape)
    aggs = [AGG, {**AGG, 'id': 'agg-b', 'name': '聚合乙'}]
    with Pair(binary, recorder, options=options(aggs)) as pair:
        for case, body, expected in [('S01', {'ids': ['agg-b']}, 200), ('S02', {'ids': ['agg-a'], 'position': 'bottom'}, 200),
             ('S03', {'aggregations': [{**AGG, 'name': '不可覆盖'}]}, 200), ('B01', {'ids': []}, 200),
             ('B02', {'aggregations': [{'id': 'agg-a', 'nodes': ['missing'], 'name': '不得写回'}]}, 200),
             ('F08', {'ids': ['agg-a', 'agg-a']}, 404), ('F09', {'ids': [1]}, 404), ('F10', {'ids': 'agg-a'}, 404),
             ('F11', {'ids': ['missing']}, 404), ('F13', [1], 500)]:
            pair.request('R010-' + case, 'POST /api/aggregations/reorder', body=json_body(body), expect=expected, writes=True,
                         check=lambda r: require({a['name'] for a in rows(pair)} == {'聚合甲🧪', '聚合乙'}, 'legacy reorder objects supply order only'))


GROUPS = {'aggregation-crud': crud, 'aggregation-validation': validation, 'aggregation-count-order': count_and_order}
