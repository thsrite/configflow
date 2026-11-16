"""Four real client syntaxes, dry-run immutability, persisted resources and warnings."""
import copy
import re

from compare import database, files, Difference
from harness import Pair, DesignError, json_body
from cases.helpers import profile_policy, profile_capture
from cases.config_backup import message

CLIENTS = ('mihomo', 'surge', 'loon', 'shadowrocket')
MIHOMO = '''proxies:
  - {name: 节点🧪, type: socks5, server: node.example, port: 1080}
proxy-groups:
  - {name: 策略🧪, type: select, proxies: [节点🧪, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.com,策略🧪
'''
INI = '''[Proxy]
节点🧪 = socks5, node.example, 1080
[Proxy Group]
策略🧪 = select, 节点🧪, DIRECT
[Rule]
DOMAIN-SUFFIX,example.com,策略🧪
'''


def document(client):
    return {'client_type': client, 'content': MIHOMO if client == 'mihomo' else INI,
            'name': '导入空间🧪', 'id': 'client-' + client, 'description': '合成描述'}


def imported_ids(pair, pid):
    def capture(replies, policy):
        result = profile_capture(pair)(replies, policy)
        rows = [database(d)['fields'] for d in pair.dirs]
        fields = [('shared//nodes', 'node'), ('shared//subscriptions', 'sub'), ('shared//rule_library', 'lib'),
                  ('profile/' + pid + '/proxy_groups', 'group'), ('profile/' + pid + '/rule_configs', 'rule')]
        for key, prefix in fields:
            collections = [row.get(key, {}).get('value', []) for row in rows]
            if len(collections[0]) != len(collections[1]):
                result.append(Difference('db.fields.' + key, 'different-item-count', len(collections[0]), len(collections[1])))
                continue
            for index, (a, b) in enumerate(zip(*collections)):
                aid, bid = a.get('id'), b.get('id')
                if aid == bid:
                    continue  # Existing fixed seed ID; contents still compared.
                actual_prefix = 'ruleset' if a.get('itemType') == 'ruleset' else prefix
                pattern = actual_prefix + r'_[0-9a-f]{12}'
                if not isinstance(aid, str) or not re.fullmatch(pattern, aid):
                    raise DesignError('Python imported resource ID format changed: ' + repr(aid))
                if not isinstance(bid, str) or not re.fullmatch(pattern, bid):
                    result.append(Difference('db.' + key + f'[{index}].id', 'invalid-import-id', pattern, bid))
                    continue
                policy.bind(aid, bid, pattern, 'import-' + actual_prefix)
        return result
    return capture


def syntax(binary, recorder):
    for index, client in enumerate(CLIENTS):
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            data = document(client)
            before = (database(pair.dirs[0]), files(pair.dirs[0]))
            def dry(reply):
                if before != (database(pair.dirs[0]), files(pair.dirs[0])):
                    raise DesignError('Python dry-run created resources, revisions or files')
                summary = reply.json().get('summary', {})
                if any(summary.get(k) != 1 for k in ('nodes', 'proxy_groups', 'rules')):
                    raise DesignError('Python did not parse one proxy/group/rule from ' + client)
            pair.request(f'R155-S{index + 1:02}', 'POST /api/profiles/import-client',
                         body=json_body({**data, 'dry_run': True}), expect=200, check=dry, writes=True)
            def saved(reply):
                rows = database(pair.dirs[0])['fields']
                pid = data['id']
                if rows['profile/' + pid + '/name']['value'] != data['name']:
                    raise DesignError('Python import did not persist named profile')
                if len(rows['shared//nodes']['value']) != 1 or len(rows['profile/' + pid + '/proxy_groups']['value']) != 1 or len(rows['profile/' + pid + '/rule_configs']['value']) != 1:
                    raise DesignError('Python persisted importer entities differ from dry-run')
                for key, prefix in [('shared//nodes', 'node'), ('profile/' + pid + '/proxy_groups', 'group'),
                                    ('profile/' + pid + '/rule_configs', 'rule')]:
                    ids = [item.get('id') for item in rows[key]['value']]
                    if len(set(ids)) != len(ids) or any(not isinstance(v, str) or not re.fullmatch(prefix + r'_[0-9a-f]{12}', v) for v in ids):
                        raise DesignError('Python imported resource IDs are not unique formatted identities')
            pair.request(f'R155-S{index + 5:02}', 'POST /api/profiles/import-client', body=json_body(data),
                         expect=201, writes=True, check=saved, capture=imported_ids(pair, data['id']))
            pair.restart()
            pair.request('T09', 'GET /api/profiles/<profile_id>/export', '/api/profiles/' + data['id'] + '/export',
                         expect=200, writes=True, capture=profile_capture(pair), variant='client-restart/' + client)


def wrappers(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        good = document('mihomo')
        inputs = [('F08', [], '请求数据必须是 JSON 对象'), ('F09', {k: v for k, v in good.items() if k != 'name'}, '请填写配置名称'),
                  ('F10', {**good, 'name': ' '}, '请填写配置名称'), ('F11', {**good, 'default_subscription_ids': 'bad'}, 'default_subscription_ids 必须是数组'),
                  ('F12', {**good, 'client_type': 'bad'}, '不支持的配置类型：bad'), ('F13', {**good, 'content': ''}, '配置内容不能为空'),
                  ('F14', {**good, 'content': '[]'}, 'Mihomo 配置必须是 YAML 对象'),
                  ('F15', {**document('surge'), 'content': 'no sections'}, '未找到任何 [Section]，请确认配置类型是否正确'),
                  ('F16', {**good, 'content': '{}'}, '未从配置中解析到节点、策略组或规则，请确认配置类型是否正确')]
        for case, data, expected in inputs:
            pair.request('R155-' + case, 'POST /api/profiles/import-client', body=json_body(data), expect=400, writes=True, check=message(expected))
        pair.request('R155-F17', 'POST /api/profiles/import-client', body=json_body({**good, 'id': 'default'}), expect=409, writes=True)
    # Missing default_subscription_ids are intentionally filtered, not rejected.
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        data = document('mihomo')
        data['default_subscription_ids'] = ['missing-synthetic']
        pair.request('R155-F18', 'POST /api/profiles/import-client', body=json_body(data), expect=201, writes=True,
                     variant='oracle-correction-missing-default-filtered', capture=imported_ids(pair, data['id']))


def warnings(binary, recorder):
    with Pair(binary, recorder) as pair:
        good = {**document('surge'), 'dry_run': True}
        # A valid rule keeps an invalid proxy line from becoming the no-entities
        # guard. Each named parser branch must remain visible in warnings.
        cases = [('H268', 'surge', '坏节点 = socks5, node.example, 0', '端口无效：0'),
                 ('H269', 'surge', '坏节点没有等号', '缺少 “=”'),
                 ('H272', 'surge', '坏节点 = socks5', '缺少服务器或端口'),
                 ('H273', 'surge', '坏节点 = unknown, node.example, 1080', '不支持的节点类型 unknown'),
                 ('H274', 'surge', '坏节点 = wireguard, section-name=missing', '未找到 [WireGuard missing]'),
                 ('H275', 'loon', '坏节点 = vmess, node.example, 443, auto, synthetic-uuid, transport=invalid', '不支持的传输方式 invalid'),
                 ('H276', 'loon', '坏节点 = socks5', '缺少服务器或端口'),
                 ('H277', 'loon', '坏节点 = unknown, node.example, 1080', '不支持的节点类型 unknown')]
        for hid, client, line, expected in cases:
            data = {**good, 'client_type': client, 'content': '[Proxy]\n' + line + '\n[Rule]\nDOMAIN,example.com,DIRECT\n'}
            def check(reply):
                warnings = reply.json().get('warnings', [])
                if len(warnings) != 1 or expected not in warnings[0]:
                    raise DesignError('Python importer warning did not identify ' + hid + ': ' + repr(warnings))
            pair.request(hid, 'POST /api/profiles/import-client', body=json_body(data), expect=200, writes=True, check=check)
        for hid, client, content, expected in [('H271', 'mihomo', '[]', 'Mihomo 配置必须是 YAML 对象'),
                                               ('H278', 'surge', 'plain text', '未找到任何 [Section]，请确认配置类型是否正确'),
                                               ('H279', 'loon', 'plain text', '未找到任何 [Section]，请确认配置类型是否正确'),
                                               ('H280', 'unknown', 'x', '不支持的配置类型：unknown'),
                                               ('H281', 'mihomo', ' ', '配置内容不能为空'),
                                               ('H282', 'mihomo', '{}', '未从配置中解析到节点、策略组或规则，请确认配置类型是否正确')]:
            pair.request(hid, 'POST /api/profiles/import-client', body=json_body({**good, 'client_type': client, 'content': content}),
                         expect=400, writes=True, check=message(expected))
        from cases.helpers import exception_policy
        pair.request('H270', 'POST /api/profiles/import-client', body=json_body({**good, 'client_type': 'mihomo', 'content': 'proxies: [bad'}),
                     expect=400, writes=True, policy=exception_policy(pair, 'YAML 解析失败：'))
        pair.request('R155-B03', 'POST /api/profiles/import-client',
                     body=json_body({**good, 'content': '[Proxy]\n坏节点 = unknown, node.example, 1080\n[Rule]\nDOMAIN,example.com,DIRECT\n'}), expect=200, writes=True)
        pair.request('R155-B02', 'POST /api/profiles/import-client',
                     body=json_body({**good, 'content': '# ' + '长🧪' * 32768 + '\n' + INI}), expect=200, writes=True)
        pair.request('R155-B01', 'POST /api/profiles/import-client', body=json_body(good), expect=200, writes=True)


GROUPS = {'client-import-syntax': syntax, 'client-import-validation': wrappers, 'client-import-warnings': warnings}
