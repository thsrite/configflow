"""Managed group sources, updates and preview through public HTTP only."""
import copy

from compare import database
from harness import Pair, json_body
from cases.subscriptions import SUB, put_file, require
from cases.config_backup import message

NODES = [{'id': 'n-a', 'name': '香港🧪', 'type': 'socks5', 'server': '192.0.2.1', 'port': 1080},
         {'id': 'n-b', 'name': '日本', 'type': 'socks5', 'server': '192.0.2.2', 'port': 1081}]
BASE = [{'id': 'g-a', 'name': '来源甲🧪', 'type': 'select', 'manual_nodes': ['n-a']},
        {'id': 'g-b', 'name': '来源乙', 'type': 'select', 'manual_nodes': ['n-b']},
        {'id': 'g-health', 'name': '测速', 'type': 'url-test', 'manual_nodes': ['n-b']}]
CHAIN = {'entry': {'type': 'node', 'id': 'n-a'}, 'exit': {'type': 'node', 'id': 'n-b'}}


def options():
    return {'shared': {'nodes': copy.deepcopy(NODES), 'subscriptions': [copy.deepcopy(SUB)],
                       'subscription_aggregations': [{'id': 'agg-a', 'name': '聚合🧪', 'nodes': ['n-a'], 'subscriptions': ['sub-a']}]},
            'profile': {'proxy_groups': copy.deepcopy(BASE)}}


def groups(pair):
    return database(pair.dirs[0])['fields']['profile/default/proxy_groups']['value']


def create(binary, recorder):
    for rid, prefix in [(157, '/api/proxy-groups'), (149, '/api/profiles/default/proxy-groups')]:
        route = 'POST ' + ('/api/proxy-groups' if rid == 157 else '/api/profiles/<profile_id>/proxy-groups')
        with Pair(binary, recorder, options=options()) as pair:
            for case, fields in [('S01', {'manual_nodes': ['n-a', 'DIRECT']}), ('S02', {'subscriptions': ['sub-a']}),
                                  ('S03', {'aggregations': ['agg-a']}), ('S04', {'include_groups': ['g-a']}),
                                  ('S05', {'follow_group': 'g-a'}), ('S06', {'type': 'chain', 'chain': CHAIN}),
                                  ('B01', {'manual_nodes': ['REJECT'], 'name': '中文🧪"\\,\n'}),
                                  ('B02', {'type': 'url-test', 'manual_nodes': ['n-a']}),
                                  ('B02', {'type': 'fallback', 'manual_nodes': ['n-a']}),
                                  ('B02', {'type': 'load-balance', 'manual_nodes': ['n-a'], 'strategy': 'round-robin'}),
                                  ('B03', {'type': 'chain', 'chain': CHAIN, 'manual_nodes': ['missing'], 'url': 'bad', 'include_all': True})]:
                data = {'id': 'new-' + str(len(groups(pair))), 'name': '新组' + str(len(groups(pair))), 'type': 'select', **fields}
                def check(reply):
                    saved = next(g for g in groups(pair) if g['id'] == data['id'])
                    require(saved == reply.json()['data'], 'returned managed group exactly equals persisted row')
                    if data['type'] == 'chain':
                        require('manual_nodes' not in saved and 'url' not in saved and 'include_all' not in saved,
                                'chain strips ordinary source and health fields before validation')
                    if data['type'] in ('url-test', 'fallback', 'load-balance'):
                        require(saved['interval'] == 60 and saved['timeout'] == 5000 and saved['lazy'] is False,
                                'creation explicitly initializes health defaults with exact types')
                        require(('tolerance' in saved) == (data['type'] == 'url-test'), 'only url-test gets tolerance')
                pair.request(f'R{rid:03}-' + case, route, prefix, body=json_body(data), expect=200,
                             writes=True, check=check, variant=data['type'] if case == 'B02' else '')
            getrid = 156 if rid == 157 else 148
            getroute = route.replace('POST ', 'GET ')
            for case in ('S01', 'S02', 'S03', 'B02'):
                pair.request(f'R{getrid:03}-' + case, getroute, prefix, expect=200, writes=True)
    with Pair(binary, recorder) as pair:
        for rid, route, path in [(156, 'GET /api/proxy-groups', '/api/proxy-groups'),
                                 (148, 'GET /api/profiles/<profile_id>/proxy-groups', '/api/profiles/default/proxy-groups')]:
            pair.request(f'R{rid:03}-B01', route, path, expect=200, writes=True, check=lambda r: require(r.json() == [], 'empty groups'))
        for case in ('S01', 'B01'):
            pair.request('R161-' + case, 'GET /api/proxy-groups/health-defaults', expect=200,
                         check=lambda r: require(r.json()['group']['tolerance'] == 50 and r.json()['group']['lazy'] is False
                                                 and r.json()['provider']['lazy'] is True, 'exact group/provider health defaults'))


def reject(binary, recorder):
    vectors = [('F08', [1], None), ('F09', {'delete_fields': []}, 'delete_fields 仅适用于更新'),
               ('F10', {'manual_nodes': ['missing']}, '候选来源必须引用有效启用资源'),
               ('F11', {'manual_nodes': 'n-a'}, 'manual_nodes 必须是数组'),
               ('F12', {'follow_group': 1}, 'follow_group 必须是 ID 字符串'),
               ('F13', {'regex': 1}, 'regex 必须是字符串或 null'),
               ('F14', {'exclude_regex': '['}, 'exclude_regex 正则表达式无效'),
               ('F15', {'manual_nodes': []}, '策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT'),
               ('F16', {'proxies_order': [{'type': 'node', 'id': 'n-b'}]}, '排序项必须与已选择的受支持来源一致'),
               ('F17', {'name': 'DIRECT'}, '策略组名称必须唯一，且不能使用 DIRECT 或 REJECT'),
               ('F18', {'type': 'chain', 'chain': {'entry': {'type': 'group', 'id': 'new-group'},
                                                'exit': {'type': 'node', 'id': 'n-b'}}}, None)]
    for rid, path in [(157, '/api/proxy-groups'), (149, '/api/profiles/default/proxy-groups')]:
        with Pair(binary, recorder, options=options()) as pair:
            for case, fields, expected in vectors:
                data = {'id': 'new-group', 'name': '新组', 'type': 'select', 'manual_nodes': ['n-a'], **fields} if isinstance(fields, dict) else fields
                pair.request(f'R{rid:03}-' + case,
                             'POST ' + ('/api/proxy-groups' if rid == 157 else '/api/profiles/<profile_id>/proxy-groups'),
                             path, body=json_body(data), expect=409 if case == 'F18' else 400,
                             writes=True, check=message(expected) if expected else None)


def update(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data in [('S01', {'remark': '说明🧪'}), ('S03', {'type': 'url-test'}),
                            ('S02', {'delete_fields': ['timeout', 'lazy']}), ('B01', {'name': '改名中文🧪'}),
                            ('S05', {'type': 'chain', 'chain': CHAIN})]:
            pair.request('R159-' + case, 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                         body=json_body(data), expect=200, writes=True)
        for case, data, status in [('F08', [1], 400), ('F09', {}, 404), ('F10', {'id': 'changed'}, 409),
                                   ('F11', {'name': '测速'}, 409), ('F15', {'type': 'select', 'manual_nodes': ['missing']}, 400),
                                   ('F17', {'url': 'https://probe.example/204', 'delete_fields': ['url']}, 400)]:
            pair.request('R159-' + case, 'PUT /api/proxy-groups/<group_id>',
                         '/api/proxy-groups/' + ('missing' if case == 'F09' else 'g-b'),
                         body=json_body(data), expect=status, writes=True)
        pair.request('R158-S01', 'DELETE /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a', expect=200, writes=True)
        pair.request('R158-F08', 'DELETE /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a', expect=404, writes=True)
        pair.request('R158-B01', 'DELETE /api/proxy-groups/<group_id>', '/api/proxy-groups/g-health', expect=200, writes=True,
                     check=lambda r: require('listener_changes' not in r.json(), 'no empty listener_changes field'))


def batch_health(binary, recorder):
    opts = options()
    opts['profile']['proxy_groups'] += [{'id': 'g-follow', 'name': '跟随', 'type': 'select', 'follow_group': 'g-health'},
        {'id': 'g-fallback', 'name': '备用', 'type': 'fallback', 'manual_nodes': ['n-a']},
        {'id': 'g-balance', 'name': '均衡', 'type': 'load-balance', 'manual_nodes': ['n-a']}]
    with Pair(binary, recorder, options=opts) as pair:
        for case, data, status in [
            ('S01', {'ids': ['g-health', 'g-fallback', 'g-balance'], 'url': 'https://probe.example/204'}, 200),
            ('B02', {'ids': ['g-health'], 'url': 'https://probe.example/%E4%B8%AD?x=%25&y=1'}, 200),
            ('F08', [1], 400), ('F09', {'url': 'https://probe.example'}, 400),
            ('F09', {'ids': [], 'url': 'https://probe.example'}, 400),
            ('F10', {'ids': ['g-health', 'g-health'], 'url': 'https://probe.example'}, 400),
            ('F11', {'ids': [' '], 'url': 'https://probe.example'}, 400),
            ('F11', {'ids': [1], 'url': 'https://probe.example'}, 400),
            ('F12', {'ids': ['g-health'], 'url': 'http://u:p@probe.example'}, 400),
            ('F13', {'ids': ['missing'], 'url': 'https://probe.example'}, 404),
            ('F14', {'ids': ['g-follow'], 'url': 'https://probe.example'}, 400),
            ('F15', {'ids': ['g-a'], 'url': 'https://probe.example'}, 400),
            ('B01', {'ids': ['g-health', 'missing'], 'url': 'https://probe.example/should-not-save'}, 404)]:
            before = database(pair.dirs[0])
            def check(reply):
                if status != 200:
                    require(database(pair.dirs[0]) == before, 'batch health failure rolls back every row and revision')
                else:
                    old = before['fields']['profile/default/proxy_groups']['value']
                    expected = [{**g, **({'url': data['url']} if g['id'] in data['ids'] else {})} for g in old]
                    require(groups(pair) == expected, 'batch health changes selected URLs only')
            pair.request('R160-' + case, 'PUT /api/proxy-groups/batch-health-url', body=json_body(data),
                         expect=status, writes=True, check=check, variant=repr(data) if case in ('F09', 'F11') else '')


def reorder(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        for case, data, status in [('S01', {'ids': ['g-b']}, 200), ('S02', {'ids': ['g-b'], 'position': 'bottom'}, 200),
                                   ('S03', {'groups': [{'id': 'g-b', 'name': 'do-not-store'}]}, 200),
                                   ('B01', {'ids': []}, 200), ('B02', {'groups': [{'id': 'g-a', 'type': 'wrong'}]}, 200),
                                   ('F08', {}, 404), ('F09', {'ids': ['g-a', 'g-a']}, 404),
                                   ('F09', {'ids': [1]}, 404), ('F10', {'ids': 'g-a'}, 404),
                                   ('F11', {'ids': ['missing']}, 404), ('F13', [1], 500)]:
            before = {g['id']: g for g in groups(pair)}
            def check(reply):
                require({g['id']: g for g in groups(pair)} == before, 'reorder never overwrites existing definitions')
            pair.request('R163-' + case, 'POST /api/proxy-groups/reorder', body=json_body(data), expect=status,
                         writes=True, check=check, variant=repr(data) if case == 'F09' else '')


GROUPS = {'groups-create': create, 'groups-reject': reject, 'groups-update': update,
          'groups-batch-health': batch_health, 'groups-reorder': reorder}


def preview(binary, recorder):
    opts = options()
    opts['shared']['subscriptions'].append({**SUB, 'id': 'sub-b', 'name': '另一来源'})
    opts['shared']['subscription_aggregations'][0]['nodes'] = ['n-b']
    opts['shared']['nodes'][0]['subscription_id'] = 'sub-a'
    opts['shared']['nodes'].append({**NODES[1], 'id': 'imported-b', 'subscription_id': 'sub-b'})
    with Pair(binary, recorder, options=opts) as pair:
        for ident in ('sub-a', 'sub-b'):
            put_file(pair, 'shared/subscribes/' + ident + '.json', {'subscription_id': ident,
                'updated_at': '2026-01-01T00:00:00Z', 'nodes': [NODES[0]], 'count': 1})
        route = 'POST /api/proxy-groups/preview-regex'
        for case, data, expected in [
            ('S01', {'regex': '香港'}, ['香港🧪']), ('S02', {'source': 'aggregation', 'aggregations': ['agg-a'], 'regex': '香港|日本'}, ['香港🧪', '日本']),
            ('S03', {'regex': '', 'exclude_regex': '香港'}, ['日本']),
            ('S04', {'regex': '日本`香港'}, ['日本', '香港🧪']),
            ('S05', {'regex': '香港', 'deduplicate_by_source': True}, ['香港🧪', '香港🧪']),
            ('B01', {'regex': '🧪'}, ['香港🧪']),
            ('B02', {'subscriptions': ['missing'], 'regex': '.'}, []),
            ('B03', {'regex': '香港'}, ['香港🧪'])]:
            body = {'source': 'subscription', 'subscriptions': ['sub-a', 'sub-b'], **data}
            pair.request('R162-' + case, route, body=json_body(body), expect=200, writes=True,
                         check=lambda r: require([n['name'] for n in r.json()['nodes']] == expected, 'preview preserves bucket order and intended source deduplication'))
        for case, data, expected in [('F08', [1], '预览请求必须是 JSON 对象'),
                                     ('F09', {'regex': 1}, '正则表达式必须是字符串'),
                                     ('F10', {'exclude_regex': []}, '排除正则必须是字符串'),
                                     ('F11', {'source': 'unknown'}, 'Invalid preview source'),
                                     ('F12', {'regex': '', 'exclude_regex': ''}, '请输入正则表达式'),
                                     ('F13', {'regex': '['}, None), ('F14', {'exclude_regex': '['}, None),
                                     ('F16', {'subscriptions': 1}, "'int' object is not iterable")]:
            body = {'source': 'subscription', 'subscriptions': ['sub-a'], 'regex': '.', **data} if isinstance(data, dict) else data
            pair.request('R162-' + case, route, body=json_body(body), expect=500 if case == 'F16' else 400,
                         writes=True, check=message(expected) if expected else None)


def references(binary, recorder):
    listener = '# preserve comment\nlisteners:\n  - name: inbound\n    type: mixed\n    port: 18080\n    proxy: 来源甲🧪 # fixed policy\n    rule: retained-rule\n'
    for operation in ('rename', 'delete'):
        opts = options()
        opts['profile']['mihomo'] = {'custom_config': listener}
        if operation == 'rename':
            opts['profile']['rule_configs'] = [{'id': 'rule-a', 'itemType': 'rule', 'rule_type': 'DOMAIN',
                'value': 'example.com', 'policy': '来源甲🧪'}]
        with Pair(binary, recorder, options=opts) as pair:
            def check(reply):
                data = database(pair.dirs[0])['fields']
                source = data['profile/default/mihomo']['value']['custom_config']
                require('# preserve comment' in source and '# fixed policy' in source and 'retained-rule' in source,
                        'listener edit preserves unrelated comments and rule selection')
                require(reply.json()['listener_changes'] == ['listeners[0].proxy'], 'listener correspondence reports exact changed path')
                if operation == 'rename':
                    require(data['profile/default/rule_configs']['value'][0]['policy'] == '新名称🧪', 'renaming relinks rule policy')
                    require('新名称🧪' in source, 'renaming relinks listener scalar')
                else:
                    require('proxy:' not in source, 'deletion removes fixed policy without replacing it')
            pair.request('R159-S04' if operation == 'rename' else 'R158-S02',
                         ('PUT' if operation == 'rename' else 'DELETE') + ' /api/proxy-groups/<group_id>',
                         '/api/proxy-groups/g-a', body=json_body({'name': '新名称🧪'}) if operation == 'rename' else None,
                         expect=200, writes=True, check=check)
    for kind in ('rule', 'include', 'follow', 'chain'):
        opts = options()
        if kind == 'rule':
            opts['profile']['rule_configs'] = [{'id': 'rule-a', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'example.com', 'policy': '来源甲🧪'}]
        else:
            dep = {'id': 'dependent', 'name': '引用者', 'type': 'select', **({'include_groups': ['g-a']} if kind == 'include' else {'follow_group': 'g-a'})}
            if kind == 'chain':
                dep = {'id': 'dependent', 'name': '引用链', 'type': 'chain', 'chain': {'entry': {'type': 'group', 'id': 'g-a'}, 'exit': {'type': 'node', 'id': 'n-b'}}}
            opts['profile']['proxy_groups'].append(dep)
        with Pair(binary, recorder, options=opts) as pair:
            for rid, case, method, body in [(158, {'rule': 'F09', 'include': 'F10', 'follow': 'F10', 'chain': 'F11'}[kind], 'DELETE', None),
                                             (159, {'rule': 'F12', 'include': 'F13', 'follow': 'F13', 'chain': 'F14'}[kind], 'PUT', {'enabled': False})]:
                before = database(pair.dirs[0])
                pair.request(f'R{rid:03}-' + case, method + ' /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                             body=json_body(body) if body else None, expect=409, writes=True, variant=kind,
                             check=lambda r: require(database(pair.dirs[0]) == before, 'blocked group removal leaves every field/revision unchanged'))


def candidate_guards(binary, recorder):
    vectors = []
    for field in ('regex', 'aggregation_regex', 'exclude_regex', 'aggregation_exclude_regex'):
        vectors.append(('H225', {field: 1}, field + ' 必须是字符串或 null', field))
    for field in ('exclude_regex', 'aggregation_exclude_regex'):
        vectors.append(('H226', {field: '['}, field + ' 正则表达式无效', field))
    for field in ('manual_nodes', 'subscriptions', 'aggregations', 'include_groups', 'proxies', 'proxies_order'):
        vectors.append(('H227', {field: None}, field + ' 必须是数组', field))
    vectors += [('H228', {'follow_group': []}, 'follow_group 必须是 ID 字符串', ''),
                ('H229', {'follow_group': 'missing'}, '跟随来源必须是有效启用的普通来源组', ''),
                ('H231', {'subscriptions': ['missing']}, '候选来源必须引用有效启用资源', ''),
                ('H233', {'proxies_order': [{}]}, '排序项必须包含且仅包含 type 和 id', ''),
                ('H234', {'proxies_order': [{'type': 'node', 'id': 'n-b'}]}, '排序项必须与已选择的受支持来源一致', ''),
                ('H235', {'proxies_order': [{'type': 'node', 'id': 'n-a'}] * 2}, '排序项不能重复', ''),
                ('H236', {'include_groups': ['g-b'], 'proxies_order': [{'type': 'node', 'id': 'n-a'}]}, '排序必须包含所有手动节点和引用策略组', ''),
                ('H237', {'manual_nodes': []}, '策略组必须选择实际消费的候选来源，不能隐式使用 DIRECT', '')]
    with Pair(binary, recorder, options=options()) as pair:
        for case, patch, expected, variant in vectors:
            data = {'id': 'new-group', 'name': '校验组', 'type': 'select', 'manual_nodes': ['n-a'], **patch}
            pair.request(case, 'POST /api/proxy-groups', body=json_body(data), expect=400, writes=True,
                         check=message(expected), variant=variant)
        for case, patch, expected in [('H238', {'delete_fields': ['url', 'url']}, 'delete_fields 必须是无重复的健康检查字段数组'),
                                      ('H239', {'delete_fields': ['url'], 'url': 'https://probe.example'}, '同一字段不能同时赋值和删除')]:
            pair.request(case, 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-health', body=json_body(patch),
                         expect=400, writes=True, check=message(expected))
        for case, field, value, expected in [
            ('H242', 'lazy', 1, 'lazy 必须为布尔值'), ('H243', 'interval', True, 'interval 必须为 1–86400 的整数'),
            ('H243', 'timeout', 600001, 'timeout 必须为 1–600000 的整数'), ('H243', 'tolerance', -1, 'tolerance 必须为 0–600000 的整数'),
            ('H244', 'url', 'https://probe.example/has space', '探测 URL 必须是有效 HTTP(S) 地址'),
            ('H245', 'url', 'http://u:p@probe.example', '探测 URL 必须是无凭据的 HTTP(S) 地址'),
            ('H246', 'expected-status', 'abc', 'expected-status 格式应为 204 或 200-299/304'),
            ('H247', 'expected-status', '599-100', 'expected-status 范围必须在 100–599 内且递增')]:
            # Health validation occurs in the repository, after managed sources.
            pair.request(case, 'POST /api/proxy-groups', body=json_body({'id': 'bad-health', 'name': '校验健康', 'type': 'url-test', 'manual_nodes': ['n-a'], field: value}),
                         expect=400, writes=True, check=message(expected), variant=field)


GROUPS.update({'groups-preview': preview, 'groups-references': references, 'groups-candidate-guards': candidate_guards})


def listener_guards(binary, recorder):
    for case, source, expected in [
        ('H248', 'listeners: {}\n', 'Mihomo listeners 必须是列表，无法确认入口的固定策略引用'),
        ('H249', 'listeners: [1]\n', 'Mihomo listeners[0] 必须是对象'),
        ('H250', 'listeners: [{proxy: 1}]\n', 'Mihomo listeners[0].proxy 必须是策略名称或留空'),
        ('H251', 'listeners: [\n', 'Mihomo 基础配置无法解析，未修改策略组或入口引用'),
        ('H252', '- not-an-object\n', 'Mihomo 基础配置必须是对象，未修改策略组或入口引用'),
        ('H253', 'listeners: [{proxy: "${GROUP}"}]\n', 'Mihomo listeners[0].proxy 含未解析变量，无法安全联动此策略组；原配置已保留')]:
        opts = options()
        opts['profile']['mihomo'] = {'custom_config': source}
        with Pair(binary, recorder, options=opts) as pair:
            for method in ('PUT', 'DELETE'):
                before = database(pair.dirs[0])
                def check(reply):
                    message(expected)(reply)
                    require(database(pair.dirs[0]) == before, 'unsafe listener draft blocks group change atomically')
                pair.request(case, method + ' /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                             body=json_body({'name': '新名字'}) if method == 'PUT' else None,
                             expect=400, writes=True, check=check, variant=method)
            pair.request('R159-F16', 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                         body=json_body({'name': '改名失败'}), expect=400, writes=True, check=message(expected), variant=case)
            pair.request('R158-F12', 'DELETE /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                         expect=400, writes=True, check=message(expected), variant=case)
    opts = options()
    opts['profile']['proxy_groups'][0]['regex'] = 1
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R159-B02', 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                     body=json_body({'remark': '保留旧坏值'}), expect=200, writes=True,
                     check=lambda r: require(r.json()['data']['regex'] == 1, 'untouched legacy nonstring filter remains editable'))
    opts = options()
    opts['shared']['subscription_aggregations'][0]['regex_filter'] = '['
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R162-F15', 'POST /api/proxy-groups/preview-regex',
                     body=json_body({'source': 'aggregation', 'aggregations': ['agg-a'], 'regex': '.'}),
                     expect=400, writes=True, check=message('正则表达式无效'))


GROUPS['groups-listener-guards'] = listener_guards
