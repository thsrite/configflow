"""Public domain adoption/undo with shared source bytes and profile history."""
import json
import time

from compare import Difference, database
from composition import cache_name
from harness import Pair, json_body
from cases.config_backup import message
from cases.domain_discovery import DIRECT, PROXY, options, stored
from cases.helpers import profile_policy
from cases.subscriptions import require

STAMP = '2026-01-01T00:00:00'


def libraries(pair):
    return {item['id']: item for item in database(pair.dirs[0])['fields']['shared//rule_library']['value']}


def history_times(pair):
    history = stored(pair).get('history', [])
    for index, entry in enumerate(history):
        if entry.get('applied_at') != STAMP:
            pair.policy.times.add(f'db.fields.profile/default/domain_discovery.value.history[{index}].applied_at')
    for index, entry in enumerate(sorted(history, key=lambda item: item.get('applied_at') or '', reverse=True)):
        if entry.get('applied_at') != STAMP:
            pair.policy.times.add(f'body.items[{index}].applied_at')


def apply_and_undo(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        pair.policy = profile_policy(pair)
        other = {key: value for key, value in database(pair.dirs[0])['fields'].items() if key.startswith('profile/other/')}
        vectors = [('S01', [{'value': 'direct.example', 'target': 'direct'}], 1, 0),
                   ('S02', [{'value': ' PROXY.EXAMPLE. ', 'rule_type': 'DOMAIN', 'target': 'proxy'}], 1, 0),
                   ('S04', [{'value': 'keep.example', 'rule_type': 'DOMAIN', 'target': 'direct'}], 0, 1),
                   ('B03', [{'value': 'duplicate.example', 'target': 'direct'}] * 3, 1, 2),
                   ('B02', [{'value': 'xn--fiqs8s.example', 'target': 'direct'}], 1, 0)]
        for case, items, added, skipped in vectors:
            def check(reply):
                require(len(reply.json()['added']) == added and len(reply.json()['skipped']) == skipped, 'adoption inserts new rules exactly once and reports covered/duplicate skips')
                data = libraries(pair)
                for target in {item['target'] for item in items}:
                    ident = DIRECT['id'] if target == 'direct' else PROXY['id']
                    library = data[ident]
                    require((pair.dirs[0] / cache_name(library)).read_bytes() == library['content'].encode(), 'published rule cache is the exact committed source content')
                require({key: value for key, value in database(pair.dirs[0])['fields'].items() if key.startswith('profile/other/')} == other,
                        'adoption history changes only the selected profile')
                history_times(pair)
            pair.request('R046-' + case, 'POST /api/domain-discovery/apply', body=json_body({'items': items}), expect=200,
                         writes=True, check=check, variant='public-storage')
            # History sorts second-resolution timestamps. Start the next write
            # after both replies are in an earlier second, so clock offsets do
            # not manufacture different tie groups across the two processes.
            time.sleep(max(0, int(time.time()) + 1.1 - time.time()))
        pair.request('R048-S01', 'GET /api/domain-discovery/history', expect=200, writes=True,
                     check=lambda r: require(r.json()['items'] and all(item['source'] == 'manual' for item in r.json()['items']), 'manual history includes adopted values, targets and source library names'))
        for case, target, value in [('S01', 'direct', 'direct.example'), ('S02', 'proxy', 'proxy.example'), ('S03', 'direct', 'missing.example'),
                                    ('B01', 'direct', 'duplicate.example')]:
            def check(reply):
                if case == 'S03':
                    require(reply.json()['removed'] == [], 'undo of an absent value is idempotent')
                else:
                    require(reply.json()['removed'], 'undo removes the actual matching source line')
                require(libraries(pair)[DIRECT['id']]['content'].startswith(DIRECT['content']), 'unrelated original source lines and comments survive undo')
                history_times(pair)
            pair.request('R059-' + case, 'POST /api/domain-discovery/undo', body=json_body({'value': value, 'target': target}),
                         expect=200, writes=True, check=check)
    for shape, case in [('500-items', 'B01'), ('history-capacity', 'B04')]:
        opts = options()
        if shape == 'history-capacity':
            opts['profile']['domain_discovery']['history'] = [
                {'value': f'old{i}.example', 'target': 'direct', 'ruleset_id': DIRECT['id'], 'rule_type': 'DOMAIN',
                 'source': 'auto' if i % 2 else 'manual', 'applied_at': STAMP} for i in range(1000)]
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            items = [{'value': 'new' + str(i) + '.example', 'target': 'direct'} for i in range(500 if shape == '500-items' else 1)]
            def check(reply):
                require(len(reply.json()['added']) == len(items) and len(stored(pair)['history']) == (500 if shape == '500-items' else 1000),
                        'maximum batch/history retention has exact cardinality')
                if shape == 'history-capacity':
                    require(stored(pair)['history'][0]['value'] == 'old1.example', 'oldest history record is evicted')
                history_times(pair)
            pair.request('R046-' + case, 'POST /api/domain-discovery/apply', body=json_body({'items': items}), expect=200, writes=True, check=check)
            if shape == 'history-capacity':
                pair.request('R048-B01', 'GET /api/domain-discovery/history', expect=200, writes=True,
                             check=lambda r: require(len(r.json()['items']) == 1000, 'history endpoint returns the retained thousand entries'))
    with Pair(binary, recorder) as pair:
        pair.request('R048-S02', 'GET /api/domain-discovery/history', expect=200, writes=True,
                     check=lambda r: require(r.json()['items'] == [], 'initial history is empty'))
    opts = options()
    opts['profile']['domain_discovery']['history'] = [
        {'value': value, 'target': 'direct', 'ruleset_id': DIRECT['id'], 'rule_type': 'DOMAIN', 'source': source, 'applied_at': STAMP}
        for value, source in [('manual.example', 'manual'), ('auto.example', 'auto'), ('unicode.example', '自动🧪')]]
    with Pair(binary, recorder, options=opts) as pair:
        for case in ('S01', 'B02'):
            pair.request('R048-' + case, 'GET /api/domain-discovery/history', expect=200, writes=True, variant='manual-auto-fixture',
                         check=lambda r: require({item['source'] for item in r.json()['items']} == {'manual', 'auto', '自动🧪'}, 'history preserves manual, auto and Unicode legacy sources'))


def mixed_target_order(binary, recorder):
    opts = options()
    opts['environment'] = {'PYTHONHASHSEED': '0'}
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy = profile_policy(pair)
        items = [{'value': 'mixed-direct.example', 'target': 'direct'}, {'value': 'mixed-proxy.example', 'target': 'proxy'}]
        def check(reply):
            require(len(reply.json()['added']) == 2 and len(stored(pair)['history']) == 2, 'both mixed targets are really adopted')
            history_times(pair)
        def capture(replies, policy):
            left, right = [reply.json().get('added') for reply in replies]
            if isinstance(left, list) and isinstance(right, list) and left != right:
                canonical = lambda rows: sorted(json.dumps(row, sort_keys=True, ensure_ascii=False) for row in rows)
                if canonical(left) == canonical(right):
                    return [Difference('body.added.order', 'python-set-order', [row['target'] for row in left], [row['target'] for row in right])]
            return []
        pair.request('R046-S03', 'POST /api/domain-discovery/apply', body=json_body({'items': items}), expect=200, writes=True,
                     check=check, capture=capture, variant='mixed-targets/hashseed-0')


def guards(binary, recorder):
    vectors = [('F08', {}, 'items 必须是 1-500 条', 'H060'), ('F08', {'items': []}, 'items 必须是 1-500 条', 'H060'),
        ('F09', {'items': [{'value': f'x{i}.example', 'target': 'direct'} for i in range(501)]}, 'items 必须是 1-500 条', 'H060'),
        ('F10', {'items': [1]}, 'items 中每一项必须是对象', 'H061')]
    for case, item in [('F11', {'value': '192.0.2.1', 'target': 'direct'}),
                       ('F12', {'value': 'example.com', 'target': 'direct', 'rule_type': 'IP-CIDR'}),
                       ('F13', {'value': 'example.com', 'target': 'invalid'}), ('B02', {'value': '中国.example', 'target': 'direct'})]:
        vectors.append((case, {'items': [item]}, '每一项需要有效域名、DOMAIN-SUFFIX/DOMAIN 类型和 direct/proxy 目标', 'H062'))
    for case, body, expected, helper in vectors:
        with Pair(binary, recorder, options=options()) as pair:
            before = database(pair.dirs[0])
            def check(reply):
                message(expected)(reply)
                require(database(pair.dirs[0]) == before, 'invalid adoption leaves the whole committed store unchanged')
            for ident in ('R046-' + case, helper):
                pair.request(ident, 'POST /api/domain-discovery/apply', body=json_body(body), expect=400, writes=True,
                             check=check, variant=case + '/' + ('missing' if 'items' not in body else str(len(body['items']))))
    for target in ('direct', 'proxy'):
        opts = options()
        opts['profile']['domain_discovery'][target + '_ruleset'] = None
        with Pair(binary, recorder, options=opts) as pair:
            for ident in ('R046-F14', 'H063'):
                pair.request(ident, 'POST /api/domain-discovery/apply', body=json_body({'items': [{'value': 'example.com', 'target': target}]}),
                             expect=400, writes=True, variant=target)
    for case, body in [('F08', {'value': '192.0.2.1', 'target': 'direct'}), ('F09', {'value': 'example.com', 'target': 'bad'})]:
        with Pair(binary, recorder, options=options()) as pair:
            for ident in ('R059-' + case, 'H065'):
                pair.request(ident, 'POST /api/domain-discovery/undo', body=json_body(body), expect=400, writes=True,
                             check=message('需要有效域名和规则集目标'), variant=case)


GROUPS = {'discovery-adopt-undo': apply_and_undo, 'discovery-write-guards': guards, 'discovery-mixed-order': mixed_target_order}
