"""Profile-local ordinary rules and references to shared library declarations."""
import copy

from compare import database
from harness import json_body
from cases.helpers import profile_pair as Pair
from cases.rule_library import DOMAIN
from cases.subscriptions import require

RULE = {'id': 'r-a', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'example.com', 'policy': 'DIRECT', 'enabled': True}
RULES = [RULE, {**RULE, 'id': 'r-ip', 'rule_type': 'IP-CIDR', 'value': '192.0.2.0/24'},
         {'id': 'rs-a', 'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'enabled': True}]


def options():
    return {'shared': {'rule_library': [copy.deepcopy(DOMAIN)]},
            'profile': {'rule_configs': copy.deepcopy(RULES),
                        'proxy_groups': [{'id': 'g-a', 'name': '策略🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}]}}


def rules(pair):
    return database(pair.dirs[0])['fields']['profile/default/rule_configs']['value']


def create(binary, recorder):
    for rid, path in [(183, '/api/rules'), (152, '/api/profiles/default/rules')]:
        route = 'POST ' + ('/api/rules' if rid == 183 else '/api/profiles/<profile_id>/rules')
        with Pair(binary, recorder, options=options()) as pair:
            for case, fields in [('S01', {'rule_type': 'DOMAIN', 'value': 'second.example'}),
                                  ('S02', {'rule_type': 'IP-CIDR', 'value': '198.51.100.0/24'}),
                                  ('S03', {'rule_type': 'MATCH', 'value': '', 'udp_fallback': 'DIRECT', 'ntp_direct': True}),
                                  ('S04', {'itemType': 'ruleset', 'library_rule_id': 'lib-a', 'name': 'must-remove', 'source_type': 'content', 'content': 'must-remove', 'behavior': 'classical'}),
                                  ('B01', {'rule_type': 'DOMAIN', 'value': '中文.example', 'remark': '说明🧪', 'group_name': '组🧪'}),
                                  ('B02', {'rule_type': 'DOMAIN-REGEX', 'value': '^a,b[.]example$'})]:
                data = {'id': 'new-' + case, 'policy': 'DIRECT', **fields}
                def check(reply):
                    saved = rules(pair)[0]
                    require(saved['id'] == data['id'], 'new rule is inserted at the front')
                    if case == 'S04':
                        require(not any(field in saved for field in ('name', 'source_type', 'content', 'behavior')),
                                'composition strips all shared source presentation fields')
                pair.request(f'R{rid:03}-' + case, route, path, body=json_body(data), expect=200, writes=True, check=check)
            for case, data in [('F08', []), ('F09', {**RULE, 'rule_type': 'UNKNOWN'}), ('F10', {**RULE, 'value': ''}),
                                ('F11', {**RULE, 'value': 'bad domain'}), ('F11', {**RULE, 'rule_type': 'IP-CIDR', 'value': '192.0.2.1'}),
                                ('F12', {'id': 'new', 'itemType': 'ruleset'}), ('F13', {'id': 'new', 'itemType': 'ruleset', 'library_rule_id': 'missing'}),
                                ('F14', {**RULE, 'rule_type': 'MATCH', 'value': '', 'udp_fallback': 'unknown'}),
                                ('F15', {**RULE, 'rule_type': 'MATCH', 'value': '', 'ntp_direct': 1})]:
                pair.request(f'R{rid:03}-' + case, route, path, body=json_body(data), expect=400, writes=True,
                             variant=data.get('rule_type', '') if isinstance(data, dict) and case == 'F11' else '')
            pair.request(f'R{rid:03}-B03', route, path, body=json_body({**RULE, 'id': 'new-S01'}), expect=400, writes=True)
            getrid = 182 if rid == 183 else 151
            for case in ('S01', 'S02'):
                pair.request(f'R{getrid:03}-' + case, route.replace('POST ', 'GET '), path, expect=200, writes=True)
    with Pair(binary, recorder) as pair:
        for rid, route, path in [(182, 'GET /api/rules', '/api/rules'), (151, 'GET /api/profiles/<profile_id>/rules', '/api/profiles/default/rules'),
                                 (178, 'GET /api/rule-sets', '/api/rule-sets')]:
            pair.request(f'R{rid:03}-B01', route, path, expect=200, writes=True, check=lambda r: require(r.json() == [], 'empty rule collection'))


def update(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, patch in [('S01', {'remark': '合并🧪'}), ('S02', {'value': 'updated.example', 'policy': '策略🧪'}),
                             ('S03', {'dns': 'default'}), ('B02', {'remark': ''}), ('B03', {'id': 'must-not-change'})]:
            pair.request('R185-' + case, 'PUT /api/rules/<rule_id>', '/api/rules/r-a', body=json_body(patch), expect=200, writes=True,
                         check=lambda r: require(next(rule for rule in rules(pair) if rule['id'] == 'r-a')['id'] == 'r-a', 'path ID stays stable during partial update'))
        pair.request('R185-S04', 'PUT /api/rules/<rule_id>', '/api/rules/rs-a', body=json_body({'library_rule_id': 'lib-a', 'remark': '组合'}), expect=200, writes=True)
        for case, data, ident, status in [
            ('F08', [], 'r-a', 400), ('F09', {'rule_type': 'UNKNOWN'}, 'r-a', 400), ('F10', {'value': ''}, 'r-a', 400),
            ('F11', {'value': 'bad domain'}, 'r-a', 400), ('F12', {'itemType': 'ruleset', 'library_rule_id': ''}, 'r-a', 400),
            ('F13', {'itemType': 'ruleset', 'library_rule_id': 'missing'}, 'r-a', 400),
            ('F14', {'rule_type': 'MATCH', 'value': '', 'udp_fallback': 'bad'}, 'r-a', 400),
            ('F15', {'rule_type': 'MATCH', 'value': '', 'ntp_direct': 'true'}, 'r-a', 400), ('F16', {}, 'missing', 404)]:
            before = database(pair.dirs[0])
            pair.request('R185-' + case, 'PUT /api/rules/<rule_id>', '/api/rules/' + ident, body=json_body(data), expect=status, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'invalid rule edit leaves all row revisions unchanged'))
        pair.request('R184-S01', 'DELETE /api/rules/<rule_id>', '/api/rules/r-a', expect=200, writes=True)
        pair.request('R184-F08', 'DELETE /api/rules/<rule_id>', '/api/rules/missing', expect=404, writes=True)
    opts = options()
    opts['profile']['rule_configs'][0]['value'] = 'legacy invalid domain'
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R185-B01', 'PUT /api/rules/<rule_id>', '/api/rules/r-a', body=json_body({'remark': '仍能编辑旧值'}), expect=200, writes=True,
                     check=lambda r: require(r.json()['data']['value'] == 'legacy invalid domain', 'unrelated edit preserves legacy invalid rule value'))


def sets(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case in ('S01', 'S02'):
            pair.request('R178-' + case, 'GET /api/rule-sets', expect=200, writes=True,
                         check=lambda r: require(len(r.json()) == 1 and r.json()[0]['itemType'] == 'ruleset', 'ruleset list excludes ordinary rules'))
        for case, data in [('S01', {'id': 'set-new', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}),
                            ('B01', {'id': 'set-forced', 'library_rule_id': 'lib-a', 'policy': 'DIRECT', 'itemType': 'rule'})]:
            pair.request('R179-' + case, 'POST /api/rule-sets', body=json_body(data), expect=200, writes=True,
                         check=lambda r: require(r.json()['data']['itemType'] == 'ruleset', 'dedicated API forces ruleset item type'))
        for case, data, status in [('F08', [1], 500), ('F09', {'id': 'set-bad'}, 400),
                                   ('F10', {'id': 'set-bad', 'library_rule_id': 'missing'}, 400),
                                   ('F11', {'id': 'rs-a', 'library_rule_id': 'lib-a', 'policy': 'DIRECT'}, 400)]:
            pair.request('R179-' + case, 'POST /api/rule-sets', body=json_body(data), expect=status, writes=True)
        pair.request('R181-S01', 'PUT /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a', body=json_body({'policy': 'REJECT'}), expect=200, writes=True)
        pair.request('R181-B01', 'PUT /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a', body=json_body({'id': 'ignored'}), expect=200, writes=True)
        for case, path, body, status in [('F08', 'rs-a', [], 400), ('F09', 'r-a', {}, 404),
                                        ('F10', 'rs-a', {'library_rule_id': 'missing'}, 400)]:
            pair.request('R181-' + case, 'PUT /api/rule-sets/<rule_set_id>', '/api/rule-sets/' + path, body=json_body(body), expect=status, writes=True)
        pair.request('R180-S01', 'DELETE /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a', expect=200, writes=True)
        for case, ident in [('F08', 'missing'), ('F09', 'r-a'), ('B01', 'rs-a')]:
            pair.request('R180-' + case, 'DELETE /api/rule-sets/<rule_set_id>', '/api/rule-sets/' + ident, expect=404, writes=True)


def batch(binary, recorder):
    opts = options()
    opts['profile']['rule_configs'].append({**RULE, 'id': 'rule_1'})
    with Pair(binary, recorder, options=opts) as pair:
        for case, data in [('S01', {'rule_type': 'DOMAIN-SUFFIX', 'domains': ['first.example', 'second.example']}),
                            ('S02', {'rule_type': 'IP-CIDR', 'domains': ['192.0.2.0/24', '198.51.100.0/24']}),
                            ('S03', {'rule_type': 'DOMAIN', 'domains': ['third.example']}),
                            ('B01', {'rule_type': 'DOMAIN', 'domains': ['中文.example']}),
                            ('B02', {'rule_type': 'DOMAIN', 'domains': ['  trimmed.example  ']})]:
            before = {rule['id'] for rule in rules(pair)}
            def check(reply):
                created = reply.json()['rules']
                require(not before.intersection(rule['id'] for rule in created), 'batch allocator skips every occupied stored ID')
                require([rule['value'] for rule in created] == [value.strip() for value in data['domains']], 'batch trims each value and preserves order')
            pair.request('R186-' + case, 'POST /api/rules/batch', body=json_body({**data, 'policy': 'DIRECT'}), expect=200, writes=True, check=check)
        for case, data in [('F08', []), ('F09', {'rule_type': 'DOMAIN'}), ('F10', {'domains': []}), ('F11', {'domains': [1]}),
                            ('F12', {'rule_type': 'UNKNOWN', 'domains': ['example.com']}),
                            ('F13', {'rule_type': 'DOMAIN', 'domains': ['example.com', 'bad domain']})]:
            before = database(pair.dirs[0])
            pair.request('R186-' + case, 'POST /api/rules/batch', body=json_body(data), expect=400, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'invalid second line does not partially insert first line'))


GROUPS = {'rules-create': create, 'rules-update': update, 'rules-sets': sets, 'rules-batch': batch}


def labels_and_policy(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data in [('S01', {'ids': ['r-a'], 'group_name': '标签'}), ('S02', {'ids': ['r-a', 'r-ip'], 'group_name': ''}),
                            ('B01', {'ids': ['r-a'], 'group_name': '中文🧪'}), ('B02', {'ids': ['r-a'], 'group_name': '  trim  '}),
                            ('B03', {'ids': ['r-a'], 'group_name': '长' * 8193})]:
            def check(reply):
                require(all(rule.get('group_name') == data['group_name'].strip() for rule in rules(pair) if rule['id'] in data['ids']),
                        'label is trimmed and stored exactly, including long Unicode input')
            pair.request('R191-' + case, 'POST /api/rules/group-name', body=json_body(data), expect=200, writes=True, check=check)
        for case, data, status in [('F08', [], 400), ('F08', {}, 400), ('F09', {'ids': [], 'group_name': 'x'}, 400),
                                   ('F10', {'ids': ['r-a', 'r-a'], 'group_name': 'x'}, 400), ('F11', {'ids': [1], 'group_name': 'x'}, 400),
                                   ('F12', {'ids': ['r-a', 'missing'], 'group_name': 'must-not-save'}, 404)]:
            before = database(pair.dirs[0])
            pair.request('R191-' + case, 'POST /api/rules/group-name', body=json_body(data), expect=status, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'bad label batch leaves every row unchanged'),
                         variant=repr(data) if case == 'F08' else '')
        for case, data in [('S01', {'ids': ['r-a'], 'policy': 'DIRECT'}), ('S02', {'ids': ['r-a', 'r-ip'], 'policy': '策略🧪'}),
                            ('S03', {'ids': ['r-a'], 'policy': 'REJECT'}), ('S04', {'ids': ['r-a'], 'policy': 'DIRECT', 'group_name': ''})]:
            before = {r['id']: r for r in rules(pair)}
            def check(reply):
                for rule in rules(pair):
                    if rule['id'] not in data['ids']:
                        require(rule == before[rule['id']], 'bulk policy preserves unselected rule')
                    else:
                        require(rule['policy'] == data['policy'], 'bulk policy stores selected target')
                        require(rule.get('group_name') == data.get('group_name', before[rule['id']].get('group_name')), 'omitted label is preserved, explicit empty clears')
            pair.request('R194-' + case, 'PUT /api/rules/policy', body=json_body(data), expect=200, writes=True, check=check)
        for case, data, status in [('F08', [], 400), ('F09', {'ids': [], 'policy': 'DIRECT'}, 400),
                                   ('F10', {'ids': ['r-a', 'r-a'], 'policy': 'DIRECT'}, 400), ('F11', {'ids': [1], 'policy': 'DIRECT'}, 400),
                                   ('F12', {'ids': ['r-a']}, 400), ('F13', {'ids': ['r-a'], 'policy': 'DIRECT', 'group_name': 1}, 400),
                                   ('F14', {'ids': ['r-a'], 'policy': 'missing-policy'}, 400),
                                   ('F15', {'ids': ['r-a', 'missing'], 'policy': 'DIRECT'}, 404)]:
            before = database(pair.dirs[0])
            pair.request('R194-' + case, 'PUT /api/rules/policy', body=json_body(data), expect=status, writes=True,
                         check=lambda r: require(database(pair.dirs[0]) == before, 'bad policy batch rolls back selected valid rule too'))
    opts = options()
    opts['profile']['rule_configs'] = [{**RULE, 'id': 'r-' + str(i)} for i in range(201)]
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R194-B01', 'PUT /api/rules/policy',
                     body=json_body({'ids': ['r-' + str(i) for i in range(201)], 'policy': 'REJECT'}), expect=200, writes=True,
                     check=lambda r: require(len(rules(pair)) == 201 and all(rule['policy'] == 'REJECT' for rule in rules(pair)), '201 policy changes have no artificial 200 cap'))


def reorder(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data, status in [('S01', {'ids': ['r-ip']}, 200), ('S02', {'ids': ['r-ip'], 'position': 'bottom'}, 200),
                                   ('S03', {'rule_configs': [{'id': 'r-a', 'value': 'do-not-store'}]}, 200),
                                   ('B01', {'ids': []}, 200), ('B02', {'rule_configs': [{'id': 'r-a', 'policy': 'do-not-store'}]}, 200),
                                   ('F08', [], 400), ('F08', {}, 400), ('F09', {'rule_configs': [1]}, 400),
                                   ('F10', {'ids': 'r-a'}, 400), ('F11', {'ids': ['missing']}, 404),
                                   ('F12', {'ids': ['r-a', 'r-a']}, 400), ('F13', {'ids': [1]}, 404), ('F13', {'ids': [[1]]}, 500)]:
            before = {r['id']: r for r in rules(pair)}
            pair.request('R195-' + case, 'POST /api/rules/reorder', body=json_body(data), expect=status, writes=True,
                         check=lambda r: require({rule['id']: rule for rule in rules(pair)} == before, 'reorder never overwrites stored rule definitions'),
                         variant=repr(data) if case in ('F08', 'F13') else '')


def concurrent_batch(binary, recorder):
    from concurrent.futures import ThreadPoolExecutor
    import threading
    import time
    from compare import database_diffs, file_diffs, response_diffs
    from harness import DesignError, send
    opts = options()
    opts['profile']['rule_configs'].append({**RULE, 'id': 'rule_1'})
    with Pair(binary, recorder, options=opts) as pair:
        route = 'POST /api/rules/batch'
        available = pair.route_available(route)
        results = []
        for index, base in enumerate(pair.bases):
            if index and not available:
                break
            barrier = threading.Barrier(2)
            def request(_):
                barrier.wait(timeout=5)
                return send(base, 'POST', '/api/rules/batch',
                            json_body({'rule_type': 'DOMAIN', 'domains': ['concurrent.example'], 'policy': 'DIRECT'}),
                            {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'})
            with ThreadPoolExecutor(max_workers=2) as pool:
                replies = list(pool.map(request, range(2)))
                # Requests are simultaneous and identical; allocated ID decides
                # correspondence, rather than which client thread won the lock.
                results.append(sorted(replies, key=lambda r: r.body))
        if [r.status for r in results[0]] != [200, 200]:
            raise DesignError('Python simultaneous allocator did not accept both valid batches')
        created = [r.json()['rules'][0]['id'] for r in results[0]]
        require(set(created) == {'rule_2', 'rule_3'}, 'simultaneous allocator avoids occupied rule_1 and issues two unique stable IDs')
        pair.policy.end = time.time()
        diffs = []
        if available:
            # Sort by the explicit stable allocated ID, preserving body/status
            # errors as ordinary differences if a Go response is not JSON.
            def key(reply):
                try:
                    return reply.json()['rules'][0]['id']
                except (ValueError, KeyError, TypeError, IndexError):
                    return ''
            for left, right in zip(sorted(results[0], key=key), sorted(results[1], key=key)):
                diffs += response_diffs(left, right, pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        recorder.record('R186-B03', 'concurrent-allocation', route, 'B',
                        'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs,
                        python_statuses=[r.status for r in results[0]], go_statuses=[r.status for r in results[1]] if available else None)


GROUPS.update({'rules-labels-policy': labels_and_policy, 'rules-reorder': reorder, 'rules-concurrent-batch': concurrent_batch})
