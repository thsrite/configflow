"""Stored-state public boundary fixtures, never imported product internals."""
import base64
import copy
import hashlib
import json

from cases import Case, concrete
from inventory import load
from seed import router, AGENT_TOKEN, DEVICE_PASSWORD, CONFIG_TOKEN


MANIFESTS = (
    ('ros_policy_state', 'H1242', 'H1289', '请先清空策略路由选择并发布撤回，再删除路由器记录',
     '请先清空策略路由选择并发布撤销，再修改路由器类型或管理地址'),
    ('ipv6_fakeip_state', 'H1249', 'H1290', '请先关闭 IPv6 Fake-IP 并发布撤销，再删除路由器记录',
     '请先关闭 IPv6 Fake-IP 并发布撤销，再修改路由器类型或管理地址'),
    ('ipv6_routes_state', 'H1250', 'H1291', '请先关闭 Mihomo IPv6 或取消相关规则集，并发布撤回 IPv6 规则集路由，再删除路由器记录',
     '请先撤回 IPv6 规则集路由并发布，再修改路由器类型或管理地址'),
    ('dns_guard_state', 'H1245', 'H1292', '请先关闭强制 DNS 并发布撤销，再删除路由器记录',
     '请先关闭强制 DNS 并发布撤销，再修改路由器类型或管理地址'),
    ('ipv6_pd_guard_state', 'H1248', 'H1293', '请先关闭 IPv6 前缀变化保护并发布撤回，再删除路由器记录',
     '请先关闭 IPv6 前缀变化保护并发布撤回，再修改路由器类型或管理地址'))


def authentication_options(base=None):
    value = copy.deepcopy(base or {})
    value['system'] = {'agents': [{'id': 'auth-agent', 'name': '合成鉴权 Agent',
        'service_type': 'oxidns', 'profile_id': 'alpha', 'host': '10.44.0.53', 'port': 18083,
        'version': '1.99.0-go', 'status': 'offline', 'enabled': True, 'token': AGENT_TOKEN}],
        'system_config': {'config_token': CONFIG_TOKEN}}
    return value


def receipt_file(ident):
    # The on-disk name is a public storage contract, not an opaque hash mask.
    payload = json.dumps(['alpha', ident], ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
    return 'router-rule-sync/' + hashlib.sha256(payload).hexdigest() + '.json'


def legacy_credentials_options():
    items = []
    for vendor in ('routeros', 'ikuai'):
        for form in ('empty', 'missing'):
            value = router('legacy-' + vendor + '-' + form, vendor, password='',
                           name='legacy credential repair', legacy_hint='keep synthetic unknown field')
            if form == 'missing':
                value.pop('password')
            items.append(value)
    return {'alpha': {'routers': {'items': items}}}


def legacy_credentials():
    items = legacy_credentials_options()['alpha']['routers']['items']
    yield Case('R007/legacy-empty-and-missing-credentials-open', 'R007', 'GET', '/api/routers', 200,
        ('H1255',), checks={'routers/' + str(index) + '/has_password': False for index in range(4)},
        absent=tuple('routers/' + str(index) + '/password' for index in range(4)))
    for index, item in enumerate(items):
        ident = item['id']
        path = '/api/routers/' + ident
        for label, document in (('rename', {'name': 'must not persist'}), ('null', {'password': None}),
                                ('empty', {'password': ''})):
            yield Case('R010/legacy-no-password-' + ident + '-' + label, 'R010', 'PUT', path, 400,
                ('H1255',), document=document, message='请填写密码')
        yield Case('R010/legacy-credential-repaired-' + ident, 'R010', 'PUT', path, 200,
            ('R010-S02', 'H1255'), document={'password': DEVICE_PASSWORD}, unchanged=False, effect='profile-write',
            checks={'router/has_password': True, 'router/name': 'legacy credential repair'}, absent=('router/password',),
            stored={'routers/items/' + str(index) + '/password': DEVICE_PASSWORD,
                    'routers/items/' + str(index) + '/legacy_hint': 'keep synthetic unknown field'},
            expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))



def notes_capacity_options():
    return {'alpha': {'device_notes': {'10.47.' + str(number // 256) + '.' + str(number % 256): '备注🧪'
                                      for number in range(2048)}}}


def notes_capacity():
    base = {'profile_id': 'alpha', 'ip': '10.48.0.21', 'name': '新备注🧪', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': ''}
    yield Case('R003/notes-2049-refused-atomically', 'R003', 'PUT', '/api/client-devices', 400,
        ('H0069',), document=base, message='设备备注必须为对象，最多 2048 条。')
    yield Case('R003/notes-2048-replace-existing', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S08',), document={**base, 'ip': '10.47.0.0', 'previous_name': '备注🧪'},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
        stored={'device_notes/10.47.0.0': '新备注🧪'})
    yield Case('R003/notes-2048-remove-one', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S07',), document={**base, 'ip': '10.47.0.0', 'name': '', 'previous_name': '新备注🧪'},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
        stored_absent=('device_notes/10.47.0.0',))
    yield Case('R003/notes-2048-freed-slot-accepted', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S01',), document=base, unchanged=False, effect='profile-write',
        retained_fields=('oxidns', 'routers', 'client_fixed_ips'), stored={'device_notes/10.48.0.21': '新备注🧪'})

def stored_options():
    items = [router(), router('r-ik', 'ikuai')]
    for index, (field, *_) in enumerate(MANIFESTS):
        items.append(router('protected-' + str(index), **{field: {'version': 1, 'resources': []}}))
    items.append({'id': 'legacy', 'name': '旧记录🧪', 'type': 'routeros', 'endpoint': 'http://10.44.0.1:18081',
                  'username': 'test-admin', 'password': 'synthetic-legacy-password',
                  'ip_rule_sets': ['static-a', 'shared', 'static-b'], 'ros_policy_rule_sets': ['shared'],
                  'opaque': {'must_survive': '未知🧪'}})
    values = {'array': [], 'scalar': 'text', 'broken': b'{',
        'running': {'status': 'running', 'message': '合成重启遗留任务', 'baseline': {'rules': {}}},
        'paused': {'status': 'paused', 'message': '合成控制条件变化', 'baseline': {'rules': {}}},
        'versions': {'status': 'ok', 'baseline': {'rules': {'rule-a': {'version': 'a' * 64}}},
                     'rules': {'rule-a': {'name': '规则🧪', 'version': 'b' * 64, 'source_type': 'inline',
                                          'ip_count': 1, 'domain_count': 2, 'ignored_count': 3, 'invalid_count': 4}}},
        'missing-version': {'status': 'ok', 'rules': {'rule-a': {'name': '坏记录🧪'}}}}
    files = {}
    for label, value in values.items():
        ident = 'receipt-' + label
        items.append(router(ident, auto_rule_sync=label in ('running', 'paused')))
        content = value if isinstance(value, bytes) else json.dumps(value, ensure_ascii=False).encode()
        files[receipt_file(ident)] = base64.b64encode(content).decode()
    return {'alpha': {'routers': {'items': items}}, 'beta': {'routers': {'items': [router()]}}, 'files': files}


def receipts():
    # First requests precede the real 30s scheduler round. T26 separately tests
    # that round; there is no test-only lifecycle switch here.
    yield Case('R023-F91/restarted-running', 'R023', 'POST', '/api/routers/receipt-running/rule-sync', 409,
               ('R023-F91',), message='合成重启遗留任务')
    yield Case('R023-F13/paused', 'R023', 'POST', '/api/routers/receipt-paused/rule-sync', 409,
               ('R023-F13',), message='同步已暂停：合成控制条件变化。请先「预览发布」并确认，重新建立同步基准')
    for label in ('array', 'scalar'):
        yield Case('R022-B90/' + label, 'R022', 'GET', '/api/routers/receipt-' + label + '/rule-sync', 200,
                   ('R022-B90',), checks={'sync/status': 'waiting', 'sync/ready': False})
    yield Case('R022-BAD/broken-json', 'R022', 'GET', '/api/routers/receipt-broken/rule-sync', 200,
               (), checks={'sync/status': 'paused', 'sync/ready': False,
                           'sync/message': '同步记录无法读取，请手动预览发布后重新建立基准'})
    yield Case('R022-VERSION/truncated', 'R022', 'GET', '/api/routers/receipt-versions/rule-sync', 200,
               (), checks={'sync/rules/0/version': 'b' * 12, 'sync/rules/0/synced_version': 'a' * 12,
                           'sync/rules/0/name': '规则🧪', 'sync/rules/0/ip_count': 1})
    yield Case('R022-F91/missing-version', 'R022', 'GET', '/api/routers/receipt-missing-version/rule-sync', 500,
               ('R022-F91',), html_error=True)


def stored_guards():
    for index, (field, update_helper, delete_helper, delete_message, update_message) in enumerate(MANIFESTS):
        path = '/api/routers/protected-' + str(index)
        yield Case('R009/' + field, 'R009', 'DELETE', path, 409, (delete_helper,), message=delete_message)
        for key, value in (('type', 'ikuai'), ('endpoint', 'http://10.44.0.2:18081')):
            yield Case('R010/' + field + '/' + key, 'R010', 'PUT', path, 400, (update_helper,),
                       document={key: value}, message=update_message)
    yield Case('R007-S03/legacy-record', 'R007', 'GET', '/api/routers', 200,
               ('R007-S03', 'R007-S04', 'R007-B04', 'R007-B05'),
               checks={'routers/7/id': 'legacy', 'routers/7/auto_rule_sync': False,
                       'routers/7/manage_return_routing': False, 'routers/7/rule_sync_interval': 60,
                       'routers/7/ip_rule_sets': ['static-a', 'static-b'], 'routers/7/ros_policy_rule_sets': ['shared']},
               absent=('routers/7/password', 'routers/7/opaque', 'routers/2/ros_policy_state'))
    for route, suffix, method, message in (
        ('R013', 'policy', 'GET', '策略路由查看仅支持 RouterOS'),
        ('R014', 'policy-settings', 'GET', '策略路由设置仅支持 RouterOS'),
        ('R015', 'policy-settings', 'PUT', '策略路由设置仅支持 RouterOS'),
        ('R016', 'policy-settings/preview', 'POST', '策略路由设置仅支持 RouterOS'),
        ('R022', 'rule-sync', 'GET', '远程规则自动同步仅支持 RouterOS'),
        ('R023', 'rule-sync', 'POST', '远程规则自动同步仅支持 RouterOS'),
        ('R024', 'rule-sync', 'PUT', '远程规则自动同步仅支持 RouterOS')):
        yield Case(route + '/unsupported-vendor', route, method, '/api/routers/r-ik/' + suffix, 400,
                   (), document={'targets': []} if route == 'R016' else None, message=message)
    routes, _ = load()
    for route in routes.values():
        if '<router_id>' not in route.path:
            continue
        yield Case(route.ident + '/absent-stored-router', route.ident, route.method,
                   concrete(route.path, router_id='missing-router'), 404, ('H1280',),
                   document={'targets': []} if route.ident == 'R016' else None, message='路由器不存在')
    for route, method, body in (('R008', 'POST', {'name': 'synthetic', 'type': 'routeros',
        'endpoint': 'http://[::1', 'username': 'admin', 'password': 'synthetic'}),
        ('R010', 'PUT', {'endpoint': 'http://[::1'})):
        yield Case(route + '/malformed-ipv6-url', route, method, '/api/routers' + ('/r-ros' if route == 'R010' else ''),
                   500, (route + '-F91',) if route == 'R008' else ('R010-F92',), document=body, html_error=True)
    for body in (['nonempty'], 'nonempty'):
        yield Case('R011/structural-body-' + type(body).__name__, 'R011', 'POST', '/api/routers/r-ros/apply', 500,
                   (), document=body, html_error=True)
    for index, (field, *_) in enumerate(MANIFESTS):
        ident = 'protected-' + str(index)
        root = 'routers/items/' + str(index + 2)
        yield Case('R010-B01/preserve-' + field, 'R010', 'PUT', '/api/routers/' + ident, 200,
                   ('R010-B01', MANIFESTS[index][1]), document={'id': 'injected-id', field: None, 'injected_unknown': 'must-not-persist'},
                   unchanged=False, effect='profile-write', checks={'success': True, 'router/id': ident},
                   stored={root + '/id': ident, root + '/' + field: {'version': 1, 'resources': []}},
                   stored_absent=(root + '/injected_unknown',))



def empty_manifest_options():
    return {'alpha': {'routers': {'items': [router('empty-' + str(index), **{field: {}})
        for index, (field, *_) in enumerate(MANIFESTS)]}}}


def empty_manifests():
    for index, (field, update_helper, delete_helper, *_) in enumerate(MANIFESTS):
        path = '/api/routers/empty-' + str(index)
        for label, document in (('endpoint', {'endpoint': 'http://10.44.0.1:18082'}), ('type', {'type': 'ikuai'})):
            yield Case('R010/empty-' + field + '-' + label, 'R010', 'PUT', path, 200, (update_helper, 'R010-S04'),
                document=document, unchanged=False, effect='profile-write',
                stored={'routers/items/0/' + field: {}},
                checks={'router/' + key: value for key, value in document.items()},
                expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
        yield Case('R009/empty-' + field + '-delete', 'R009', 'DELETE', path, 200, (delete_helper, 'R009-S01'),
            unchanged=False, effect='router-delete')

def agent_options():
    agents = []
    for ident, service, profile, host in (
        ('m-a', 'mihomo', 'alpha', '10.44.0.2'), ('m-b', 'mihomo', 'beta', '10.44.0.3'),
        ('o-a', 'oxidns', 'alpha', '10.44.0.53'), ('o-b', 'oxidns', 'beta', '10.44.0.54')):
        agent = {'id': ident, 'name': '合成设备🧪 ' + ident, 'service_type': service, 'host': host, 'port': 18083,
                 'token': AGENT_TOKEN + '-' + ident, 'enabled': True, 'status': 'offline',
                 'version': '1.99.0-go', 'deployment_method': 'shell'}
        if profile is not None:
            agent['profile_id'] = profile
        agents.append(agent)
    return {'system': {'agents': agents}, 'alpha': {'routers': {'items': [router(proxy_gateway_agent_id='m-a'),
            router('r-ik', 'ikuai', proxy_gateway_agent_id='m-a'), router('r-cross', proxy_gateway_agent_id='m-b')]}},
            'beta': {'routers': {'items': []}}}


def agent_peers():
    peers = {}
    for ident, host in (('m-a', '10.44.0.2'), ('m-b', '10.44.0.3')):
        peers[host + ':18083'] = {'token': AGENT_TOKEN + '-' + ident, 'endpoints': {
            '/api/linux-gateway/status': {'success': True, 'supported': True,
                'ipv4_forwarding': {'supported': True, 'current': '0', 'persisted': False, 'config': 'absent',
                                    'hook_script': 'absent', 'startup_hook': '', 'layout': 'standalone'},
                'managed': {'active': False, 'preserves_forwarding': False}}}}
    return peers


def heartbeat(ident):
    return {'rpc': 'http', 'method': 'POST', 'path': '/api/agents/' + ident + '/heartbeat', 'document': {},
            'headers': {'Authorization': 'Bearer ' + AGENT_TOKEN + '-' + ident}, 'expected': 200, 'auth': False}


def agent_scope():
    for agent_id, profile in (('o-a', 'alpha'), ('o-b', 'beta')):
        yield Case('R002-S03/' + agent_id, 'R002', 'GET', '/api/client-devices?agent=' + agent_id, 200,
                   ('R002-S03',) + (('R002-B91',) if profile == 'beta' else ()), checks={'success': True, 'profile_id': profile},
                   header_checks={'X-ConfigFlow-Profile': 'alpha', 'Cache-Control': 'no-store'})
    yield Case('R002-B90/agent-header-missing', 'R002', 'GET', '/api/client-devices?agent=o-b', 404,
               ('R002-B90',), headers={'X-ConfigFlow-Profile': 'missing-profile'},
               message='Profile not found: missing-profile')
    for route, path in (('R002', '/api/client-devices'), ('R004', '/api/client-devices/discovery')):
        for agent_id in ('missing-agent', '', 'm-a'):
            yield Case(route + '/invalid-agent-' + agent_id, route, 'GET', path + '?agent=' + agent_id, 404,
                       ('H1256',), message='这台 OxiDNS Agent 已不存在，请返回 Agent 页刷新。')
    yield Case('R002-B92/head-ignores-missing-agent', 'R002', 'HEAD', '/api/client-devices?agent=missing-agent',
               200, ('R002-B92',), header_checks={'X-ConfigFlow-Profile': 'alpha', 'Cache-Control': 'no-store'})
    yield Case('R004-S04/pinned-empty-profile', 'R004', 'GET', '/api/client-devices/discovery?agent=o-b', 200,
               ('R004-S04',), checks={'success': True, 'profile_id': 'beta', 'state': 'unsupported', 'sources': [],
                                      'devices': [], 'discovered': []}, header_checks={'X-ConfigFlow-Profile': 'alpha'},
               times=('checked_at',))
    yield Case('R004/agent-header-missing', 'R004', 'GET', '/api/client-devices/discovery?agent=o-b', 404,
               ('X02',), headers={'X-ConfigFlow-Profile': 'missing-profile'},
               message='Profile not found: missing-profile')
    yield Case('R014-B90/all-gateways-profile-sources', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200,
               ('R014-B90', 'R014-S02', 'R014-S03'), checks={
                   'workspace/gateways': [{'id': ident, 'name': '合成设备🧪 ' + ident, 'host': host}
                                         for ident, host in (('m-a', '10.44.0.2'), ('m-b', '10.44.0.3'))],
                   'workspace/source_agents': [{'id': ident, 'name': '合成设备🧪 ' + ident, 'host': host, 'status': 'offline'}
                                              for ident, host in (('o-a', '10.44.0.53'),)]})


def forwarding():
    yield Case('R025-S01/live-status', 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
               ('R025-S01', 'R025-S02', 'R025-S05', 'R025-B02'), unchanged=False, effect='device-read',
               prepare=({'action': 'seed', 'agent': agent_peers()}, heartbeat('m-a'), heartbeat('m-b')),
               checks={'success': True, 'gateways/0/agent_id': 'm-a', 'gateways/0/state': 'needs_persist',
                       'gateways/0/can_persist': True, 'gateways/0/routers': [
                           {'id': 'r-ros', 'name': '合成路由器🧪 routeros'}, {'id': 'r-ik', 'name': '合成路由器🧪 ikuai'}],
                       'gateways/1/agent_id': 'm-b', 'gateways/1/state': 'needs_persist'})
    for index, (ident, entry) in enumerate((('m-a', 'S01'), ('m-a', 'S02'), ('m-b', 'S03'))):
        yield Case('R026-' + entry + '/persist-' + ident, 'R026', 'POST', '/api/routers/gateway-forwarding', 200,
                   ('R026-' + entry,), document={'agent_id': ident}, unchanged=False, effect='device-only',
                   checks={'success': True, 'gateway/agent_id': ident, 'gateway/state': 'persisted',
                           'gateway/current': '1', 'gateway/persisted': True}, requires=('R025',))
        yield Case('R025/confirm-persist-' + str(index), 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
                   (), unchanged=False, effect='device-read',
                   checks={'gateways/' + ('0' if ident == 'm-a' else '1') + '/state': 'persisted'})
