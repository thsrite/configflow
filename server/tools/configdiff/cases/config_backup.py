"""config.py global backup import/export/reset and actual recovery snapshots."""
import copy
import json
import re
from urllib.parse import quote

from compare import database, Difference, files as stored_files
from downloads import capture_headers, validate_headers
from harness import Pair, DesignError, json_body
from seed import CURRENT, RETIRED
from snapshots import ImportSnapshot
from cases.helpers import profile_capture, profile_policy, deny_writes, exception_policy


def seed_document(pair):
    return json.loads((pair.dirs[0] / 'configdiff-seed.json').read_text())


def settings(directory):
    return database(directory)['fields']['system//system_config']['value']


def export_capture(pair):
    def capture(replies, policy):
        result = profile_capture(pair)(replies, policy) + capture_headers(replies, policy)
        for pid in replies[0].json().get('profiles', {}):
            for field in ('created_at', 'updated_at'):
                policy.times.add('body.profiles.' + pid + '.' + field)
        return result
    return capture


def exports(binary, recorder):
    with Pair(binary, recorder, options={'x01_location': 'node', 'x01_payload': '合成🧪'}) as pair:
        pair.policy = profile_policy(pair)
        original = seed_document(pair)
        for case, value in [('S01', 'false'), ('S02', 'true'), ('S03', 'TRUE'), ('B01', '1'), ('B02', 'false')]:
            def check(reply):
                payload = reply.json()
                secret = payload['system']['system_config']
                sanitized = value.lower() == 'true'
                if sanitized:
                    if any(key in secret for key in ('config_token', 'rule_proxy_token', 'retired_rule_proxy_tokens')):
                        raise DesignError('Python sanitized export retained capabilities')
                    if payload['system']['agents'] or payload['system']['backup']:
                        raise DesignError('Python sanitized export retained Agent/backup')
                    node = payload['shared']['nodes'][0]
                    if node['server'] != '[REDACTED]' or node['password'] != '[REDACTED]' or node['params'] != {}:
                        raise DesignError('Python sanitized node credentials were not scrubbed')
                elif payload != original:
                    raise DesignError('Python raw send_file backup is not restorable original document')
            pair.request('R027-' + case, 'GET /api/config/export', '/api/config/export?desensitize=' + value,
                         expect=200, check=check, capture=export_capture(pair), writes=True)
        full = pair.request('R027-B90', 'GET /api/config/export', variant='full-before-conditional',
                            expect=200, capture=export_capture(pair))[0]
        def fragment(reply):
            if reply.body != full.body[:4] or reply.headers.get('content-range') != f'bytes 0-3/{len(full.body)}':
                raise DesignError('Python Range did not return exact four-byte export fragment')
        pair.request('R027-B90', 'GET /api/config/export', headers={'Range': 'bytes=0-3'},
                     variant='GET-Range', expect=206, check=fragment, writes=True)
        pair.request('R027-B90', 'GET /api/config/export', headers={'Range': 'bytes=999999999-'},
                     variant='GET-unsatisfiable-Range', expect=416, writes=True)
        pair.request('R027-B90', 'GET /api/config/export', headers={'If-None-Match': '*'},
                     variant='BytesIO-no-ETag-If-None-Match', expect=200, capture=export_capture(pair), writes=True)
        pair.request('R027-B90', 'GET /api/config/export', headers={'If-Modified-Since': 'Tue, 01 Jan 2030 00:00:00 GMT'},
                     variant='BytesIO-no-LastModified', expect=200, capture=export_capture(pair), writes=True)


def bind_capability(pair, expected=None):
    def capture(replies, policy):
        values = [settings(d)['rule_proxy_token'] for d in pair.dirs]
        if expected is None:
            valid = [bool(re.fullmatch(r'[A-Za-z0-9_-]{43}', v)) and v not in (CURRENT, RETIRED) for v in values]
            if not valid[0]:
                raise DesignError('Python replacement capability was not newly generated token_urlsafe(32)')
            if not valid[1]:
                return [Difference('db.rule_proxy_token', 'not-new-capability', True, values[1])]
            policy.bind(*values, r'[A-Za-z0-9_-]{43}', 'new-rule-capability')
        elif values[0] != expected:
            raise DesignError('Python current-schema import unexpectedly rotated supplied capability')
        return []
    return capture


def import_request(pair, case, data, *, variant='', expected_token=None):
    snapshot = ImportSnapshot(pair)
    def check(reply):
        snapshot.check(reply)
        value = settings(pair.dirs[0])
        if expected_token is not None and value.get('rule_proxy_token') != expected_token:
            raise DesignError('Python did not preserve imported capability')
        if expected_token is None and not re.fullmatch(r'[A-Za-z0-9_-]{43}', value.get('rule_proxy_token', '')):
            raise DesignError('Python did not generate missing/colliding capability')
        retired = value.get('retired_rule_proxy_tokens', [])
        if retired.count(CURRENT) != (0 if value['rule_proxy_token'] == CURRENT else 1) or retired.count(RETIRED) != 1:
            raise DesignError('Python retired capability merge lost or duplicated history')
    def capture(replies, policy):
        return snapshot.capture(replies, policy) + bind_capability(pair, expected_token)(replies, policy)
    return pair.request(case, 'POST /api/config/import', body=json_body(data), variant=variant,
                        expect=200, writes=True, check=check, capture=capture)


def imports(binary, recorder):
    for case, shape in [('S01', 'current'), ('S02', 'legacy'), ('S03', 'schema2'), ('B01', 'empty'),
                         ('B02', 'unicode'), ('B03', 'unknown'), ('B04', 'shared-resource')]:
        with Pair(binary, recorder) as pair:
            pair.policy = profile_policy(pair)
            data = seed_document(pair)
            expected = CURRENT
            if shape in ('legacy', 'empty'):
                data = {} if shape == 'empty' else {'name': '原版🧪', 'mihomo': {'custom_config': '# legacy\n'}}
                expected = None
            elif shape == 'schema2':
                data = {'schema_version': 2, 'system': {'schema_version': 2, 'profiles': [
                    {'id': 'default', 'name': '旧默认🧪'}, {'id': 'other', 'name': '旧空间乙'}],
                    'system_config': {}, 'backup': {}, 'agents': []},
                    'profiles': {'default': {}, 'other': {}}}
                expected = None
            elif shape == 'unicode':
                data['profiles']['other'].update(name='中文🧪\"\\', description='描述\n第二行')
            elif shape == 'unknown':
                data['profiles']['other']['synthetic_unknown'] = {'nested': ['可保留🧪']}
                data['system']['system_config']['synthetic_extension'] = 'visible'
            elif shape == 'shared-resource':
                data['shared']['nodes'] = [{'id': 'shared-node', 'name': '共享🧪', 'type': 'socks5', 'server': 'node.example', 'port': 1080}]
                for p in data['profiles'].values():
                    p['proxy_groups'] = [{'id': 'local-group', 'name': '选择', 'type': 'select', 'manual_nodes': ['shared-node']}]
            import_request(pair, 'R028-' + case, data, variant=shape, expected_token=expected)
            # Restart reads the actual committed store, not an in-process cache.
            pair.restart()
            def readback(reply):
                payload = reply.json()
                if shape == 'unknown' and payload['profiles']['other'].get('synthetic_unknown') != {'nested': ['可保留🧪']}:
                    raise DesignError('Python unknown profile field did not survive restart')
                if shape == 'shared-resource' and len(payload['shared']['nodes']) != 1:
                    raise DesignError('Python shared resource did not survive restart')
            pair.request('T09', 'GET /api/config/export', variant='restart/' + shape,
                         expect=200, writes=True, check=readback, capture=export_capture(pair))


def message(expected):
    def check(reply):
        if reply.json().get('message') != expected:
            raise DesignError('Python import validation reached another branch: ' + repr(reply.body[:1000]))
    return check


def import_guards(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        original = seed_document(pair)
        def call(case, data, expected, status=400, variant=''):
            before = (database(pair.dirs[0]), stored_files(pair.dirs[0]))
            def check(reply):
                message(expected)(reply)
                if (database(pair.dirs[0]), stored_files(pair.dirs[0])) != before:
                    raise DesignError('Rejected Python import changed database/files')
            pair.request(case, 'POST /api/config/import', body=json_body(data), variant=variant,
                         expect=status, check=check, writes=True)
        call('R028-F08', [], '请求数据必须是 JSON 对象')
        for value in (None, True, 1, 'text'):
            call('R028-F08', value, '请求数据必须是 JSON 对象', variant=repr(value))
        unsupported = '仅支持原版单配置、schema 2 完整多配置备份或当前格式；未修改原数据'
        for key in ('profiles', 'shared', 'resource_refs', 'node_dialers'):
            call('H825', {key: {}}, unsupported, variant=key)
        call('R028-F09', {'schema_version': 999}, unsupported)
        for key in ('system', 'profiles'):
            data = {'schema_version': 2, 'system': {}, 'profiles': {}}
            data[key] = []
            call('H823', data, '旧多配置备份必须包含 system 索引和全部 profiles 配置内容', variant=key)
        call('H824', {'schema_version': 2, 'system': {'schema_version': 2, 'profiles': [{'id': 'default'}]}, 'profiles': {}},
             '旧多配置备份的索引与配置内容不一致')
        call('H826', {'system_config': []}, '系统设置必须是对象')
        # These edits preserve all earlier invariants and target a named guard.
        recipes = [
            ('H834', ('system', 'system_config', 'default_profile_id'), 1, '默认配置空间 ID 必须是字符串'),
            ('H835', ('system', 'system_config', 'default_profile_id'), 'absent', '必须保留默认配置'),
            ('H836', ('system', 'extra'), {}, 'system 节只接受全局设置、备份与 Agent'),
            ('H837', ('system', 'backup'), [], '系统设置和备份设置必须是对象'),
            ('H840', ('shared', 'nodes'), [{'id': 'node-bad', 'name': '节点', 'dialer_ref': {}}], '共享节点不能保存拨号引用，请在策略组中创建代理链'),
            ('H841', ('shared', 'nodes'), [{'id': 'node-bad', 'name': '节点', 'subscription_id': 'absent'}], '节点引用的订阅不存在'),
            ('H842', ('shared', 'rule_library'), [{'id': 'rule-bad', 'name': '规则', 'policy': 'DIRECT'}], '规则仓库不包含目标策略，请在规则配置中设置'),
            ('H843', ('shared', 'rule_library'), [{'id': 'rule-bad', 'name': '同名'}, {'id': 'rule-bad2', 'name': '同名'}], '共享规则仓库名称必须非空且唯一'),
            ('H844', ('shared', 'subscription_aggregations'), [{'id': 'agg-bad', 'name': '聚合', 'nodes': ['absent']}], '聚合 聚合 引用了不存在的资源：absent'),
            ('H845', ('profiles', 'other', 'id'), 'wrong', '配置内容与 ID 不一致'),
            ('H848', ('system', 'agents'), [{'id': 'agent-bad', 'name': '合成', 'profile_id': 'absent'}], 'Agent 合成 绑定的配置不存在'),
            ('H850', ('profiles', 'other', 'nodes'), [], '独立配置不能保存共享资源副本'),
            ('H851', ('profiles', 'other', 'name'), ' ', '配置名称不能为空'),
            ('H852', ('profiles', 'other', 'resource_refs'), {}, '独立配置不接受资源白名单或节点拨号覆盖字段'),
            ('H865', ('profiles', 'other', 'loon'), [], 'loon 参数必须是对象'),
            ('H866', ('profiles', 'other', 'domain_discovery'), [], 'domain_discovery 参数必须是对象'),
            ('H868', ('profiles', 'other', 'routers'), {'items': {}}, 'routers 参数必须是包含 items 列表的对象'),
            ('H869', ('profiles', 'other', 'oxidns'), [], 'oxidns 参数必须是对象')]
        for hid, path, value, expected in recipes:
            data = copy.deepcopy(original)
            target = data
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            call(hid, data, expected)
        for key in ('system', 'shared', 'profiles'):
            data = copy.deepcopy(original)
            data[key] = []
            call('H832', data, '缺少配置节：' + key, variant=key)
        data = copy.deepcopy(original)
        data['system']['system_config'] = []
        call('H833', data, '系统设置和备份设置必须是对象')
        data = copy.deepcopy(original)
        del data['profiles']['default']
        call('R028-F11', data, '必须保留默认配置')
        for scope in ('system', 'shared', 'profile'):
            for value in (-1, '0', None):
                data = copy.deepcopy(original)
                target = data['profiles']['other'] if scope == 'profile' else data[scope]
                target['_revision'] = value
                call('H849', data, '配置修订号无效', variant=scope + '/' + repr(value))
        for engine in ('mihomo', 'surge', 'mosdns'):
            data = copy.deepcopy(original)
            data['profiles']['other'][engine] = []
            call('H864', data, engine + ' 参数必须是对象', variant=engine)
        pair.request('R028-F15', 'POST /api/config/import', body='{bad', expect=500,
                     policy=exception_policy(pair), writes=True)
        with deny_writes(pair):
            pair.request('R028-F14', 'POST /api/config/import', body=json_body(original),
                         expect=500, policy=exception_policy(pair), writes=True, capture=profile_capture(pair))


def lifecycle(binary, recorder):
    for mode in ('login', 'open'):
        for operation in ('missing', 'collision', 'current'):
            with Pair(binary, recorder, mode=mode) as pair:
                pair.policy = profile_policy(pair)
                data = seed_document(pair)
                cfg = data['system']['system_config']
                if operation == 'missing':
                    cfg.pop('rule_proxy_token')
                elif operation == 'collision':
                    cfg['config_token'] = CURRENT
                cfg['retired_rule_proxy_tokens'] = [RETIRED, RETIRED, '', 123]
                import_request(pair, 'R028-S90', data, variant=mode + '/' + operation,
                               expected_token=CURRENT if operation == 'current' else None)
                values = [settings(d).get('rule_proxy_token') for d in pair.dirs]
                for token_kind in (('current', 'retired', 'old-current') if operation != 'current' else ('current', 'retired')):
                    tokens = values if token_kind == 'current' else ([CURRENT, CURRENT] if token_kind == 'old-current' else [RETIRED, RETIRED])
                    paths = ['/api/mosdns/rule-proxy?token=' + quote(t, safe='') for t in tokens]
                    pair.request('T28', 'GET /api/mosdns/rule-proxy', paths=paths, auth='',
                                 expect=400 if token_kind == 'current' else 401, writes=True,
                                 variant=mode + '/' + operation + '/' + token_kind)
                # A new config-token may not embed a current or retired capability.
                for token_kind, token in [('current', values[0]), ('retired', RETIRED)]:
                    pair.request('T28', 'POST /api/config-token',
                                 bodies=[json_body({'token': 'prefix-' + t}) for t in (values if token_kind == 'current' else [token, token])],
                                 expect=400, writes=True, variant=mode + '/' + operation + '/reject-config-token/' + token_kind)


def reset(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        for case, body in [('S01', None), ('S02', '{}'), ('B01', '{}'), ('B02', '{not JSON')]:
            snapshot = ImportSnapshot(pair)
            old = settings(pair.dirs[0])['rule_proxy_token']
            def check(reply):
                snapshot.check(reply)
                rows = database(pair.dirs[0])['fields']
                if any(key.startswith('profile/other/') for key in rows):
                    raise DesignError('Python reset retained old profile')
                if old not in settings(pair.dirs[0]).get('retired_rule_proxy_tokens', []):
                    raise DesignError('Python reset did not retire previous capability')
            def capture(replies, policy):
                return snapshot.capture(replies, policy) + bind_capability(pair)(replies, policy)
            pair.request('R033-' + case, 'POST /api/config/reset', body=body, expect=200,
                         writes=True, check=check, capture=capture)
        pair.request('R033-S90', 'POST /api/config/reset', body='{}', expect=200, writes=True,
                     check=ImportSnapshot(pair).check, capture=bind_capability(pair))
    with Pair(binary, recorder) as pair:
        with deny_writes(pair):
            pair.request('R033-F09', 'POST /api/config/reset', body='{}', expect=500,
                         writes=True, policy=exception_policy(pair), capture=profile_capture(pair))


def protected_routers(binary, recorder):
    for field, label in [('ros_policy_state', '策略路由'), ('dns_guard_state', '强制 DNS 规则'),
                         ('ipv6_fakeip_state', 'IPv6 Fake-IP 路由'), ('ipv6_routes_state', 'IPv6 规则集路由'),
                         ('ipv6_pd_guard_state', 'IPv6 前缀变化保护')]:
        router = {'id': 'router-synthetic', 'name': '受保护路由器', 'type': 'routeros',
                  'endpoint': 'http://192.0.2.1:18080', field: {'synthetic_ownership': 'must-retain'}}
        for operation in ('import', 'reset'):
            with Pair(binary, recorder, options={'profile': {'routers': {'items': [router]}}}) as pair:
                pair.policy = profile_policy(pair)
                data = seed_document(pair)
                data['profiles']['default']['routers']['items'] = []
                pair.request('R028-F13' if operation == 'import' else 'R033-F08',
                             'POST /api/config/' + operation, body=json_body(data) if operation == 'import' else '{}',
                             expect=409, writes=True, capture=profile_capture(pair), variant=field,
                             check=message(f'路由器仍有已发布的{label}；请先发布并核对撤回，再修改类型、地址或删除发布记录'))


GROUPS = {'config-export': exports, 'config-import': imports, 'config-import-guards': import_guards,
          'config-capability-lifecycle': lifecycle, 'config-reset': reset}

GROUPS['config-protected-router-import'] = protected_routers
