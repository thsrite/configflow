"""Stored-state public boundary fixtures, never imported product internals."""
import base64
import hashlib
import json

from cases import Case, concrete
from inventory import load
from seed import router


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


def receipt_file(ident):
    # The on-disk name is a public storage contract, not an opaque hash mask.
    payload = json.dumps(['alpha', ident], ensure_ascii=False, sort_keys=True, separators=(',', ':')).encode()
    return 'router-rule-sync/' + hashlib.sha256(payload).hexdigest() + '.json'


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
    yield Case('R023-F12/paused', 'R023', 'POST', '/api/routers/receipt-paused/rule-sync', 409,
               ('R023-F12',), message='同步已暂停：合成控制条件变化。请先「预览发布」并确认，重新建立同步基准')
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
