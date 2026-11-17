"""Public editor/write sequences using each side's own previous HTTP response."""
from cases import Case, Ref
from seed import router, DEVICE_PASSWORD


def client_drafts():
    base = {'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': 'http://panel.example'}
    yield Case('R002-S01/initial', 'R002', 'GET', '/api/client-devices', 200, ('R002-S01', 'R002-B01'),
               checks={'success': True, 'profile_id': 'alpha', 'devices': [], 'agents': []},
               formats={'oxidns_revision': r'[0-9a-f]{64}'})
    previous_name = previous_mode = ''
    for index, (entry, name, mode) in enumerate([
        ('S01', '客厅设备🧪', ''), ('S02', '客厅设备🧪', 'smart'), ('S03', '客厅设备🧪', 'direct'),
        ('S02', '客厅设备🧪', 'smart'), ('S04', '客厅设备🧪', 'direct'), ('S06', '', 'direct'),
        ('S05', '新备注🧪', ''), ('S08', '仅改备注🧪', ''), ('S07', '', '')]):
        body = {**base, 'name': name, 'mode': mode, 'previous_name': previous_name, 'previous_mode': previous_mode}
        yield Case('R003-' + entry + '/' + str(index), 'R003', 'PUT', '/api/client-devices', 200,
                   ('R003-' + entry,), document=body, unchanged=False, effect='profile-write',
                   checks={'success': True, 'profile_id': 'alpha',
                           'devices': [{'ip': base['ip'], 'name': name, 'mode': mode}] if name or mode else []},
                   formats={'oxidns_revision': r'[0-9a-f]{64}'})
        previous_name, previous_mode = name, mode
    yield Case('R002-S02/beta-unchanged', 'R002', 'GET', '/api/client-devices?profile=beta', 200,
               ('R002-S02',), checks={'profile_id': 'beta', 'devices': []})


def client_canonical_names():
    # No policy-mode change: canonical address and display-only names must not
    # regenerate native_config or touch router reservations.
    base = {'profile_id': 'alpha', 'name': '', 'mode': '', 'previous_name': '',
            'previous_mode': '', 'base_url': ''}
    values = (
        ('ipv4-host-prefix', ' 10.44.0.21/32 ', '10.44.0.21'),
        ('ipv4-cidr-mask', '10.44.0.42/24', '10.44.0.0/24'),
        ('ipv6-host-prefix', 'FD42:CF:0000::21/128', 'fd42:cf::21'),
        ('ipv6-cidr-mask', 'fd42:cf::abcd/64', 'fd42:cf::/64'),
        ('mapped-host', '::ffff:10.44.0.22/128', '10.44.0.22'),
        ('mapped-range', '::ffff:10.45.0.42/120', '10.45.0.0/24'),
        ('mapped-default', '::ffff:10.44.0.21/96', '0.0.0.0/0'),
        ('ipv6-default', '::/0', '::/0'),
        ('padded-prefix', '10.46.0.21/032', '10.46.0.21'),
    )
    rows = []
    for label, raw, normalized in values:
        name = '行🧪' * 40  # Exactly80 Unicode characters, deliberately >80 bytes.
        rows.append({'ip': normalized, 'name': name, 'mode': ''})
        yield Case('R003/canonical-' + label, 'R003', 'PUT', '/api/client-devices', 200,
            ('R003-B01', 'R003-B02', 'R003-B03', 'R003-B04'), document={**base, 'ip': raw, 'name': name},
            unchanged=False, effect='profile-write', checks={'devices': list(rows)},
            stored={'device_notes/' + normalized.replace('~', '~0').replace('/', '~1'): name},
            expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    trimmed = '备注 "引号" 🧪'
    rows[0] = {**rows[0], 'name': trimmed}
    yield Case('R003/name-trim-and-own-previous', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-B01',), document={**base, 'ip': '10.44.0.21', 'name': '  ' + trimmed + '  ',
                                'previous_name': '行🧪' * 40},
        unchanged=False, effect='profile-write', checks={'devices': list(rows)})
    rows.pop(0)
    yield Case('R003/remove-note-with-normalized-previous', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S07',), document={**base, 'ip': '::ffff:10.44.0.21', 'previous_name': trimmed + '  '},
        unchanged=False, effect='profile-write', checks={'devices': list(rows)},
        stored_absent=('device_notes/10.44.0.21',))
    yield Case('R002/canonical-names-beta-isolation', 'R002', 'GET', '/api/client-devices?profile=beta', 200,
        ('R002-S02', 'R003-B07'), checks={'devices': [], 'profile_id': 'beta'})


def workspace_drafts():
    yield Case('R014-S01/initial', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200,
               ('R014-S01', 'R014-B01'), checks={'success': True, 'workspace/profile_id': 'alpha',
                   'workspace/router/id': 'r-ros', 'workspace/source_agents': [], 'workspace/gateways': []},
               formats={'workspace/revision': r'[0-9a-f]{64}', 'workspace/names_revision': r'[0-9a-f]{64}'})
    body = {'revision': Ref('R014-S01/initial', '/body/workspace/revision'),
            'router': {'ip_rule_sets': [], 'ros_policy_rule_sets': [], 'proxy_gateway_agent_id': ''}, 'base_url': ''}
    yield Case('R015-S01/modern', 'R015', 'PUT', '/api/routers/r-ros/policy-settings', 200, ('R015-S01',),
               document=body, unchanged=False, effect='profile-write', checks={'success': True})
    yield Case('R015-S03/noop', 'R015', 'PUT', '/api/routers/r-ros/policy-settings', 200, ('R015-S03',),
               document={**body, 'revision': Ref('R015-S01/modern', '/body/workspace/revision')},
               unchanged=False, effect='profile-write', checks={'success': True})
    yield Case('R015-S02/legacy', 'R015', 'PUT', '/api/routers/r-ros/policy-settings', 200,
               ('R015-S02', 'R015-B01'), document={**body,
                   'revision': Ref('R015-S03/noop', '/body/workspace/revision'),
                   'names_revision': Ref('R015-S03/noop', '/body/workspace/names_revision'), 'policies': []},
               unchanged=False, effect='profile-write', checks={'success': True})


def router_updates():
    yield Case('R007-S01/alpha', 'R007', 'GET', '/api/routers', 200, ('R007-S01', 'R007-B02', 'R007-B03'),
               checks={'success': True, 'routers/0/id': 'r-ros', 'routers/0/has_password': True})
    yield Case('R007-S02/beta', 'R007', 'GET', '/api/routers?profile=beta', 200, ('R007-S02',),
               checks={'success': True, 'routers/0/id': 'r-ros'})
    yield Case('R010-S01/rename', 'R010', 'PUT', '/api/routers/r-ros', 200, ('R010-S01',),
               document={'name': '重命名🧪'}, unchanged=False, effect='profile-write',
               checks={'success': True, 'router/name': '重命名🧪', 'router/has_password': True})
    yield Case('R010-S03/empty-password-retains', 'R010', 'PUT', '/api/routers/r-ros', 200,
               ('R010-S03',), document={'password': ''}, unchanged=False, effect='profile-write',
               checks={'success': True, 'router/has_password': True})
    yield Case('R010-B04/noop', 'R010', 'PUT', '/api/routers/r-ros', 200, ('R010-B04',),
               document={}, unchanged=False, effect='profile-write', checks={'success': True})
    yield Case('R015-F15/real-stale', 'R015', 'PUT', '/api/routers/r-ros/policy-settings', 409,
               ('R015-F15',), document={'revision': Ref('R015-S02/legacy', '/body/workspace/revision'),
                   'router': {'ip_rule_sets': [], 'ros_policy_rule_sets': [], 'proxy_gateway_agent_id': ''}, 'base_url': ''},
               message='配置已在其他页面更新，请重新载入后再保存。')


def runtime_reads():
    for index, query in enumerate(('', '?profile=alpha', '?refresh=1', '?refresh=true')):
        entry = ('S01', 'S03', 'S04', 'S05')[index]
        yield Case('R001-' + entry + '/unlinked', 'R001', 'GET',
                   '/api/agents/missing-agent/upstream-routers' + query, 200, ('R001-' + entry,),
                   checks={'success': True, 'routers': [], 'cached': False}, times=('checked_at',))
    for index, (query, header) in enumerate((('', 'missing-profile'), ('?profile_id=missing-profile', 'beta'))):
        yield Case('R001-F91/global-' + str(index), 'R001', 'GET',
                   '/api/agents/missing-agent/upstream-routers' + query, 200, ('R001-F91',),
                   headers={'X-ConfigFlow-Profile': header}, checks={'success': True, 'routers': []}, times=('checked_at',))
    for vendor, router_id in (('routeros', 'r-ros'), ('ikuai', 'r-ik')):
        entry = 'S01' if vendor == 'routeros' else 'S02'
        yield Case('R019-' + entry + '/empty', 'R019', 'GET', '/api/routers/' + router_id + '/routes', 200,
                   ('R019-B01',), checks={'success': True, 'routes': []}, unchanged=False, effect='device-read')
    yield Case('R025-S03/no-links', 'R025', 'GET', '/api/routers/gateway-forwarding', 200,
               ('R025-S03',), checks={'success': True, 'gateways': []})
    yield Case('R022-S01/waiting', 'R022', 'GET', '/api/routers/r-ros/rule-sync', 200,
               ('R022-S01',), checks={'success': True, 'sync/status': 'waiting', 'sync/ready': False,
                                      'settings/auto_rule_sync': False, 'settings/rule_sync_interval': 60})
    yield Case('R023-F12/no-baseline', 'R023', 'POST', '/api/routers/r-ros/rule-sync', 409,
               ('R023-F12',), message='请开启自动同步，并先手动预览发布一次')


def sync_settings():
    for index, (switch, interval) in enumerate([(True, 60), (False, 60)] +
                                                [(False, value) for value in (15, 30, 60, 180, 360, 720, 1440)]):
        entry = 'S' + format(index + 1, '02')
        yield Case('R024-' + entry + '/save', 'R024', 'PUT', '/api/routers/r-ros/rule-sync', 200,
                   ('R024-' + entry,), document={'auto_rule_sync': switch, 'rule_sync_interval': interval},
                   unchanged=False, effect='profile-write',
                   checks={'success': True, 'settings/auto_rule_sync': switch, 'settings/rule_sync_interval': interval})


def manual_routes():
    for vendor, router_id in (('routeros', 'r-ros'), ('ikuai', 'r-ik')):
        root = '/api/routers/' + router_id
        values = {'cidr': '10.55.0.9/24', 'gateway': '10.44.0.2', 'enabled': True, 'comment': '  手工🧪 "\\$;\n备注  '}
        canonical = {**values, 'cidr': '10.55.0.0/24', 'comment': values['comment'].strip()}
        tag = '/' + vendor
        add_entry, update_entry, delete_entry = ('S01', 'S02', 'S03') if vendor == 'routeros' else ('S04', 'S05', 'S06')
        foreign = (('ros', '/ip/route', '.id', '*F0'),) if vendor == 'routeros' else (
            ('ikuai', 'static_rt', 'id', 9999),)
        seed = {'ros': {'/ip/route': [{'.id': '*F0', 'dst-address': '10.56.0.0/24', 'gateway': '10.44.0.3',
                    'comment': 'foreign路由🧪', 'distance': '9', 'disabled': 'false'}]}} if vendor == 'routeros' else {
                'ikuai': {'static_rt': [{'id': 9999, 'enabled': 'yes', 'tagname': 'foreign', 'ip_type': '4',
                    'interface': 'lan4', 'dst_addr': '10.56.0.0', 'gateway': '10.44.0.3',
                    'netmask': '255.255.255.0', 'prio': 9, 'comment': 'foreign'}]}}
        add_plan = 'R021-' + add_entry + '/add' + tag
        add_apply = 'R020-' + add_entry + '/add' + tag
        yield Case(add_plan, 'R021', 'POST', root + '/routes/plan', 200, ('R021-' + add_entry,),
                   document={'action': 'add', 'values': values}, unchanged=False, effect='device-read',
                   prepare=({'action': 'seed', **seed},), foreign=foreign,
                   checks={'success': True, 'plan/action': 'add', 'plan/empty': False, 'plan/values': canonical},
                   formats={'plan/fingerprint': r'[0-9a-f]{64}'})
        yield Case(add_apply, 'R020', 'POST', root + '/routes/apply', 200, ('R020-' + add_entry,),
                   document={'action': 'add', 'values': values, 'fingerprint': Ref(add_plan, '/body/plan/fingerprint')},
                   unchanged=False, effect='device-only', foreign=foreign,
                   checks={'success': True, 'result/changed': True, 'result/entry/cidr': canonical['cidr'],
                           'result/entry/comment': canonical['comment'], 'result/entry/editable': True},
                   formats={'result/entry/revision': r'[0-9a-f]{64}'})
        yield Case('R019/after-manual-add' + tag, 'R019', 'GET', root + '/routes', 200,
                   ('R019-S01',) if vendor == 'routeros' else ('R019-S02',),
                   unchanged=False, effect='device-read', foreign=foreign, requires=('R020',),
                   checks={'success': True, 'routes/0/editable': True, 'routes/1/editable': True,
                           'routes/1/id': Ref(add_apply, '/body/result/entry/id')})
        changed = {**canonical, 'gateway': '10.44.0.53', 'enabled': False, 'comment': '更新🧪'}
        intent = {'action': 'update', 'id': Ref(add_apply, '/body/result/entry/id'),
                  'revision': Ref(add_apply, '/body/result/entry/revision'), 'values': changed}
        update_plan, update_apply = 'R021-' + update_entry + '/update' + tag, 'R020-' + update_entry + '/update' + tag
        yield Case(update_plan, 'R021', 'POST', root + '/routes/plan', 200, ('R021-' + update_entry,), document=intent,
                   unchanged=False, effect='device-read', foreign=foreign,
                   checks={'success': True, 'plan/empty': False, 'plan/values': changed},
                   formats={'plan/fingerprint': r'[0-9a-f]{64}'})
        yield Case(update_apply, 'R020', 'POST', root + '/routes/apply', 200, ('R020-' + update_entry,),
                   document={**intent, 'fingerprint': Ref(update_plan, '/body/plan/fingerprint')},
                   unchanged=False, effect='device-only', foreign=foreign,
                   checks={'success': True, 'result/changed': True, 'result/entry/gateway': changed['gateway'],
                           'result/entry/enabled': False, 'result/entry/comment': changed['comment']})
        yield Case('R021/stale-revision' + tag, 'R021', 'POST', root + '/routes/plan', 409,
                   ('H0823',), document=intent, unchanged=False, effect='device-read', foreign=foreign,
                   message='路由已被其他操作修改，请刷新列表后重新预览')
        noop = {**intent, 'revision': Ref(update_apply, '/body/result/entry/revision')}
        noop_plan, noop_apply = 'R021-S07/noop' + tag, 'R020-S07/noop' + tag
        yield Case(noop_plan, 'R021', 'POST', root + '/routes/plan', 200, ('R021-S07',), document=noop,
                   unchanged=False, effect='device-read', foreign=foreign,
                   checks={'success': True, 'plan/empty': True})
        yield Case(noop_apply, 'R020', 'POST', root + '/routes/apply', 200, ('R020-S07',),
                   document={**noop, 'fingerprint': Ref(noop_plan, '/body/plan/fingerprint')},
                   unchanged=False, effect='device-read', foreign=foreign,
                   checks={'success': True, 'result/changed': False, 'message': '路由没有变化'})
        delete = {'action': 'delete', 'id': Ref(update_apply, '/body/result/entry/id'),
                  'revision': Ref(update_apply, '/body/result/entry/revision')}
        delete_plan = 'R021-' + delete_entry + '/delete' + tag
        yield Case(delete_plan, 'R021', 'POST', root + '/routes/plan', 200, ('R021-' + delete_entry,), document=delete,
                   unchanged=False, effect='device-read', foreign=foreign,
                   checks={'success': True, 'plan/action': 'delete', 'plan/after': None})
        yield Case('R020-' + delete_entry + '/delete' + tag, 'R020', 'POST', root + '/routes/apply', 200, ('R020-' + delete_entry,),
                   document={**delete, 'fingerprint': Ref(delete_plan, '/body/plan/fingerprint')},
                   unchanged=False, effect='device-only', foreign=foreign,
                   checks={'success': True, 'result/changed': True, 'result/entry': None})


def job_locks():
    busy = '此路由器正在发布或同步，请稍后重试'
    endpoints = [('R009', 'DELETE', ''), ('R010', 'PUT', ''), ('R011', 'POST', '/apply'),
                 ('R015', 'PUT', '/policy-settings'), ('R017', 'POST', '/probe'),
                 ('R020', 'POST', '/routes/apply'), ('R024', 'PUT', '/rule-sync')]
    required = tuple(route for route, _, _ in endpoints)
    yield Case('R019/job-lock-prior-login', 'R019', 'GET', '/api/routers/r-ik/routes', 200,
               (), unchanged=False, effect='device-read', checks={'success': True, 'routes': []})
    for vendor, router_id, peer, path in (('routeros', 'r-ros', 'ros', '/rest/system/resource'),
                                         ('ikuai', 'r-ik', 'ikuai', '/Action/login')):
        gate, pending = 'job-' + vendor, 'probe-' + vendor
        first = True
        for route, method, suffix in endpoints:
            prepare = ()
            if first:
                body = {'version': '7.24.5', 'board-name': 'synthetic-gated-router'} if vendor == 'routeros' else {
                    'Result': 10000, 'code': 1005, 'message': 'synthetic gated refusal'}
                prepare = ({'action': 'fault', 'match': {'peer': peer, 'path': path},
                            'gate': gate, 'status': 200, 'body': body},
                           {'rpc': 'begin', 'ident': pending, 'method': 'POST', 'path': '/api/routers/' + router_id + '/probe'},
                           {'action': 'wait', 'gate': gate})
                first = False
            yield Case(route + '/busy-before-body-' + vendor, route, method, '/api/routers/' + router_id + suffix,
                       409, ('H1278',), document=['invalid-before-lookup'], message=busy,
                       prepare=prepare, requires=required)
        if vendor == 'routeros':
            yield Case('R010/busy-shared-connection-cross-profile', 'R010', 'PUT', '/api/routers/r-ros?profile=beta',
                       409, ('H1278',), document=[], message=busy, requires=required)
        yield Case('R017/gated-probe-finish-' + vendor, 'R017', 'POST', '/api/routers/' + router_id + '/probe',
                   200 if vendor == 'routeros' else 502, ('R017-S01',) if vendor == 'routeros' else ('R017-S90',),
                   prepare=({'action': 'release', 'gate': gate},), join=pending, unchanged=False, effect='profile-write',
                   checks={'success': vendor == 'routeros', 'probe/ok': vendor == 'routeros'}, times=('probe/at',), requires=required)


def router_creates():
    base = router()
    base.pop('id')
    variants = [('ros-minimal', {}, {}, 'R008-S01'), ('ikuai-minimal', {'type': 'ikuai'}, {'type': 'ikuai'}, 'R008-S02')]
    for field, pairs in (
        ('name', [(True, 'True'), (123, '123'), (['x'], "['x']"), ({'k': 1}, "{'k': 1}"), ('  中文🧪  ', '中文🧪')]),
        ('username', [(True, 'True'), (123, '123'), (['x'], "['x']"), ({'k': 1}, "{'k': 1}")]),
        ('verify_tls', [(False, False), (None, True), (0, True), ('false', True), ([], True), ({}, True)]),
        ('dns_agent_id', [(None, ''), (0, ''), (False, ''), (123, '123')]),
        ('proxy_gateway_agent_id', [(None, ''), (0, ''), (False, ''), (123, '123')]),
        ('ip_rule_sets', [(None, []), (False, []), (0, []), ('', []), ({}, []), (['b', 'a', 'b'], ['b', 'a'])]),
        ('ros_policy_rule_sets', [(None, []), (False, []), (0, []), ('', []), ({}, []), (['b', 'a', 'b'], ['b', 'a'])])):
        for index, (value, expected) in enumerate(pairs):
            variants.append((field + '-' + str(index), {field: value}, {field: expected}, 'R008-B90'))
    variants += [('unvalidated-port', {'endpoint': 'http://10.44.0.1:not-a-port'},
                  {'endpoint': 'http://10.44.0.1:not-a-port'}, 'R008-B90'),
                 ('query-fragment', {'endpoint': 'http://10.44.0.1/?q=synthetic#fragment'},
                  {'endpoint': 'http://10.44.0.1/?q=synthetic#fragment'}, 'R008-B90'),
                 ('unknown-ignored', {'unknown': {'opaque': 'synthetic'}}, {}, 'R008-B90')]
    for label, patch, expected, entry in variants:
        ident = 'R008/create-' + label
        checks = {'success': True, 'router/has_password': True,
                  **{'router/' + field: value for field, value in expected.items()}}
        stored = {'routers/items/-1/password': DEVICE_PASSWORD,
                  **{'routers/items/-1/' + field: value for field, value in expected.items()}}
        yield Case(ident, 'R008', 'POST', '/api/routers', 200, (entry,), document={**base, **patch},
                   unchanged=False, effect='router-create', checks=checks, stored=stored,
                   absent=('router/password', 'router/unknown'), formats={'router/id': r'router_[0-9a-f]{12}'},
                   requires=('R009',))
        yield Case('R009/delete-' + label, 'R009', 'DELETE', Ref(ident, '/body/router/id', prefix='/api/routers/'),
                   200, ('R009-S01',), unchanged=False, effect='router-delete', checks={'success': True}, requires=('R008',))


def unlinked_inspection():
    yield Case('R013-S02/no-manifest', 'R013', 'GET', '/api/routers/r-ros/policy', 200,
               ('R013-S02', 'R013-B01'), checks={'success': True, 'policy/objects': [], 'policy/source_agents': []},
               times=('generated_at',))
    for vendor, router_id in (('routeros', 'r-ros'), ('ikuai', 'r-ik')):
        entry = 'S01' if vendor == 'routeros' else 'S02'
        root = '/api/routers/' + router_id
        yield Case('R018-' + entry + '/unlinked-' + vendor, 'R018', 'GET', root + '/report', 200,
                   ('R018-' + entry,), unchanged=False, effect='device-read',
                   checks={'success': True, 'report/return_routing/status': 'disabled'}, times=('generated_at',))
        plan = 'R012/unlinked-plan-' + vendor
        yield Case(plan, 'R012', 'POST', root + '/plan', 200, ('R012-S12',), unchanged=False, effect='device-read',
                   checks={'success': True, 'plan/blockers/0': '未关联 mihomo Agent，无法确定代理路由的下一跳',
                           'plan/fingerprint': '', 'plan/stages': [], 'plan/script': ''}, absent=('plan/sync_baseline',))
        yield Case('R011/unlinked-blocked-' + vendor, 'R011', 'POST', root + '/apply', 409, ('H1308',),
                   document={'fingerprint': Ref(plan, '/body/plan/fingerprint')}, unchanged=False, effect='device-read',
                   checks={'success': False, 'plan/blockers/0': '未关联 mihomo Agent，无法确定代理路由的下一跳'}, requires=('R012',))
    yield Case('R014/preview-current', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200,
               ('R014-S01',), checks={'success': True}, formats={'workspace/revision': r'[0-9a-f]{64}'})
    yield Case('R016-S01/no-targets', 'R016', 'POST', '/api/routers/r-ros/policy-settings/preview', 200,
               ('R016-S01', 'R016-B01', 'R016-B05'), document={
                   'revision': Ref('R014/preview-current', '/body/workspace/revision'), 'targets': [], 'base_url': ''},
               unchanged=False, effect='device-read', checks={'success': True, 'preview/dns_content': '', 'preview/targets': []},
               requires=('R014',))
