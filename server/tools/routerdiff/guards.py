"""Field-by-field HTTP guard recipes; exact Python messages prove reachability."""
import copy
from dataclasses import replace

from cases import Case
from seed import router, DEVICE_PASSWORD


def invalid(route, method, path, label, document, message, entries, status=400):
    return Case(route + '/' + label, route, method, path, status, tuple(entries),
                document=document, message=message, reason='each compound condition/loop field is instantiated independently')


def router_settings():
    base = router()
    base.pop('id')
    variants = []
    def add(field, values, message, *entries):
        for index, value in enumerate(values):
            variants.append((field + '-' + str(index), {**base, field: value}, message, entries))
    add('name', ['', ' ', None, False, 0], '请填写名称', 'H1251')
    add('type', ['', None, 'RouterOS', 'routeros7', 'unknown', 7], '路由器类型只支持 iKuai 4.x 或 RouterOS', 'H1252')
    add('endpoint', ['', 'ftp://10.44.0.1', 'http://', 'http://10.44.0.1/rest', '/relative'],
        '管理地址格式应为 http(s)://主机[:端口]', 'H1253')
    add('username', ['', ' ', None, False, 0], '请填写用户名', 'H1254')
    add('manage_return_routing', [None, '', 0, 1, [], {}], '自动配置回流必须为布尔值', 'H1235')
    add('auto_rule_sync', [None, '', 0, 1, [], {}], '远程规则自动同步仅支持 RouterOS，开关必须为布尔值', 'H1236')
    add('rule_sync_interval', [None, '', True, False, 60.0, '60', 0, -1, 14, 16, 1441, [], {}],
        '请选择有效的规则检查间隔', 'H1237')
    for field, message, helper in [('ip_rule_sets', 'IP 路由规则集必须是规则集 ID 列表', 'H1238'),
                                   ('ros_policy_rule_sets', '策略路由规则集必须是规则集 ID 列表', 'H1239')]:
        add(field, ['id', 1, {'id': 1}, [''], [None], [1], [True], [[]], [{}]], message, helper)
    variants += [('overlap', {**base, 'ip_rule_sets': ['same'], 'ros_policy_rule_sets': ['same']},
                  '同一规则集只能选择静态路由或策略路由中的一种方式', ('H1241',)),
                 ('ikuai-policy', {**base, 'type': 'ikuai', 'ros_policy_rule_sets': ['policy']},
                  '按来源策略路由仅支持 RouterOS', ('H1240',)),
                 ('ikuai-auto', {**base, 'type': 'ikuai', 'auto_rule_sync': True},
                  '远程规则自动同步仅支持 RouterOS，开关必须为布尔值', ('H1236',))]
    add('dns_guard', [False, 1, '', [], {'unknown': True}], '强制 DNS 设置格式无效', 'H1243', 'H1244', 'H0154')
    for field in ('enabled', 'block_dot', 'failover'):
        add('dns_guard', [{field: value} for value in (None, 0, 1, 'false', [], {})],
            '强制 DNS 的开关必须为布尔值', 'H1243', 'H1244', 'H0155')
    add('dns_guard', [{'bypass': value} for value in (None, 1, 'ip', {}, [1], [None], [[]], [True], [1.5], [{}])],
        '放行设备必须是 IPv4 地址或网段列表', 'H1243', 'H1244', 'H0156')
    for value in ('bad', '::1', '10.44.0.1/33', '', '  ', '地址🧪', '１９２．０．２．１'):
        add('dns_guard', [{'bypass': [value]}], f'放行设备「{value}」不是有效的 IPv4 地址或网段',
            'H1243', 'H1244', 'H0157', 'H0158')
    add('dns_guard', [{'bypass': ['10.44.0.2'] * 65}], '放行设备最多 64 项', 'H1243', 'H1244', 'H0159')
    add('dns_guard', [{'enabled': True}], '请先选择 DHCP DNS 服务器（OxiDNS Agent），强制 DNS 使用同一台设备',
        'H1243', 'H1244', 'H0161')
    add('ipv6_pd_guard', [True, 0, [], '', {'unknown': False}, {'enabled': None}, {'enabled': 1}, {'enabled': 'true'}],
        'IPv6 前缀变化保护的开关必须为布尔值', 'H1246', 'H1247', 'H0491')
    for route, method, path in [('R008', 'POST', '/api/routers'), ('R010', 'PUT', '/api/routers/r-ros')]:
        for index, (label, body, message, entries) in enumerate(variants):
            yield invalid(route, method, path, str(index) + "-" + label, body, message, entries)
        # Password handling differs between create (required) and edit (retain).
        if route == 'R008':
            for index, value in enumerate((None, '', 0, True, [], {})):
                yield invalid(route, method, path, 'password-' + str(index), {**base, 'password': value},
                              '请填写密码', ('H1255',))


def guard_vendor_support():
    for route, method, path in (('R008', 'POST', '/api/routers'), ('R010', 'PUT', '/api/routers/r-ros'),
                                ('R010', 'PUT', '/api/routers/r-ik')):
        base = router(type='ikuai')
        base.pop('id')
        suffix = '-existing-iKuai' if path.endswith('/r-ik') else ''
        yield invalid(route, method, path, 'iKuai-cannot-enable-DNS-guard' + suffix,
            {**base, 'dns_guard': {'enabled': True}}, '强制局域网 DNS 走 OxiDNS 仅支持 RouterOS',
            ('H0160', 'H1243', 'H1244'))
        yield invalid(route, method, path, 'iKuai-cannot-enable-PD-guard' + suffix,
            {**base, 'ipv6_pd_guard': {'enabled': True}}, 'IPv6 前缀变化保护仅支持 RouterOS',
            ('H0492', 'H1246', 'H1247'))
        yield invalid(route, method, path, 'iKuai-overlap-stops-at-vendor-check' + suffix,
            {**base, 'ip_rule_sets': ['same'], 'ros_policy_rule_sets': ['same']},
            '按来源策略路由仅支持 RouterOS', ('H1240',))


def router_settings_ikuai():
    # Exercise common guards on an actual existing iKuai record as well as
    # creation. A valid vendor does not exist for a create's invalid type value.
    for case in router_settings():
        vendor = case.document['type']
        if vendor != 'routeros' and case.route == 'R008':
            continue
        if 'H1241' in case.entries or 'H0161' in case.entries:
            # Both need enabled RouterOS-only policy/guard settings. iKuai is
            # stopped earlier; the explicit vendor cases prove that predecessor.
            continue
        document = {**case.document, **({'type': 'ikuai'} if vendor == 'routeros' else {})}
        path = '/api/routers/r-ik' if case.route == 'R010' else case.path
        yield replace(case, ident=case.route + '/iKuai-shared-' + case.ident.split('/', 1)[1],
                      document=document, path=path)


def normalized_router_guards():
    defaults = {'enabled': False, 'block_dot': False, 'failover': True, 'bypass': []}
    for ident, index in (('r-ros', 0), ('r-ik', 1)):
        path = '/api/routers/' + ident
        values = [' 10.44.0.21/24 ', '10.44.0.0/24', '10.44.0.2', '10.44.0.2/32']
        normalized = {**defaults, 'bypass': ['10.44.0.0/24', '10.44.0.2/32']}
        yield Case('R010/guard-trim-mask-and-stable-dedup-' + ident, 'R010', 'PUT', path, 200,
            ('G14',), document={'dns_guard': {'bypass': values}}, unchanged=False, effect='profile-write',
            checks={'router/dns_guard': normalized}, stored={'routers/items/' + str(index) + '/dns_guard': normalized})
        for label, values in (('64-duplicates', ['10.44.0.2'] * 64),
                              ('64-unique', ['10.44.1.' + str(number) for number in range(1, 65)])):
            normalized = {**defaults, 'bypass': list(dict.fromkeys(value + '/32' for value in values))}
            yield Case('R010/guard-exact-' + label + '-' + ident, 'R010', 'PUT', path, 200,
                ('G14',), document={'dns_guard': {'bypass': values}}, unchanged=False, effect='profile-write',
                checks={'router/dns_guard': normalized})
        yield Case('R010/guard-null-restores-defaults-' + ident, 'R010', 'PUT', path, 200,
            ('G14',), document={'dns_guard': None, 'ipv6_pd_guard': None}, unchanged=False, effect='profile-write',
            checks={'router/dns_guard': defaults, 'router/ipv6_pd_guard': {'enabled': False}},
            stored={'routers/items/' + str(index) + '/dns_guard': defaults,
                    'routers/items/' + str(index) + '/ipv6_pd_guard': {'enabled': False}})


    for ident in ('r-ros', 'r-ik'):
        for interval in (15, 30, 60, 180, 360, 720, 1440):
            yield Case('R010/interval-enum-' + ident + '-' + str(interval), 'R010', 'PUT', '/api/routers/' + ident, 200,
                ('H1237', 'R010-S05'), document={'rule_sync_interval': interval},
                unchanged=False, effect='profile-write', checks={'router/rule_sync_interval': interval})
        flags = {'manage_return_routing': True, 'auto_rule_sync': ident == 'r-ros'}
        yield Case('R010/native-boolean-flags-' + ident, 'R010', 'PUT', '/api/routers/' + ident, 200,
            ('H1235', 'H1236'), document=flags, unchanged=False, effect='profile-write',
            checks={'router/' + key: value for key, value in flags.items()})
    for index, ident in enumerate(('r-ros', 'r-ik')):
        for number, value in enumerate((None, 0, False, True, [], {})):
            yield Case('R010/ignored-password-' + ident + '-' + str(number), 'R010', 'PUT', '/api/routers/' + ident, 200,
                ('R010-B02', 'H1255'), document={'password': value}, unchanged=False, effect='profile-write',
                checks={'router/has_password': True}, stored={'routers/items/' + str(index) + '/password': DEVICE_PASSWORD},
                absent=('router/password',), expected_requests=({'match': {'peer': 'ros'}, 'count': 0},
                                                              {'match': {'peer': 'ikuai'}, 'count': 0}))

def rejected_draft_readback():
    # Extra sequence controls: DB snapshots alone cannot observe a Go handler
    # accidentally mutating a cached in-memory record before returning400.
    expected = {}
    for index, vendor in enumerate(('routeros', 'ikuai')):
        prefix = 'routers/' + str(index)
        expected.update({prefix + '/id': 'r-ros' if index == 0 else 'r-ik',
                         prefix + '/name': '合成路由器🧪 ' + vendor,
                         prefix + '/username': 'test-admin', prefix + '/verify_tls': True,
                         prefix + '/manage_return_routing': False, prefix + '/auto_rule_sync': False,
                         prefix + '/rule_sync_interval': 60, prefix + '/ip_rule_sets': [],
                         prefix + '/ros_policy_rule_sets': [], prefix + '/has_password': True})
    yield Case('R007/after-all-rejected-drafts-runtime-records-unchanged', 'R007', 'GET', '/api/routers', 200,
        checks=expected)
    yield Case('R002/after-all-rejected-drafts-runtime-devices-unchanged', 'R002', 'GET', '/api/client-devices', 200,
        checks={'devices': [], 'profile_id': 'alpha'})
    yield Case('R014/after-all-rejected-drafts-runtime-workspace-unchanged', 'R014', 'GET',
        '/api/routers/r-ros/policy-settings', 200,
        checks={'workspace/router/ip_rule_sets': [], 'workspace/router/ros_policy_rule_sets': [],
                'workspace/router/proxy_gateway_agent_id': ''})


def client_draft():
    base = {'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '设备🧪', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': ''}
    def case(label, body, message, *entries, status=400):
        if 'R003-F12' in entries:
            entries += ('H1261', 'H1262')
        return invalid('R003', 'PUT', '/api/client-devices', label, body, message, entries, status)
    for field in base:
        body = dict(base)
        body.pop(field)
        yield case('missing-' + field, body, '客户端设备请求格式无效。', 'R003-F10', 'H1257')
    yield case('extra', {**base, 'extra': None}, '客户端设备请求格式无效。', 'R003-F10', 'H1257')
    for value in (None, 0, False, [], {}):
        yield case('base_url-' + repr(value), {**base, 'base_url': value}, '客户端设备请求格式无效。', 'R003-F10', 'H1257')
    for value in ('beta', '', None, 0, False, True, [], {}, 1.5, ' alpha ', 'ALPHA'):
        yield case('profile-' + repr(value), {**base, 'profile_id': value},
                   '配置空间已切换，请重新载入。', 'R003-F11', 'H1258', status=409)
    # Each normalizer guard is attached only to values that reach it. The
    # enclosing ValueError translation is a separate, all-families audit.
    ranges = [
        (('H0071',), (None, 0, False, [], {})),
        (('H0072',), ('', 'bad', '10.44.0.1/1/2', '10.44.0.1%lo', '地址', '１０.４４.０.１')),
        (('H0073',), ('10.44.0.1/', '10.44.0.1/-1', '10.44.0.1/+1', '10.44.0.1/3a',
                       '10.44.0.1/0001', '10.44.0.1/ 24', '10.44.0.1/２４')),
        (('H0074',), ('10.44.0.1/33', 'fd42:cf::1/129')),
        (('H0075',), ('::ffff:10.44.0.1/95',)),
        (('H0076',), ('224.0.0.1', '224.0.0.1/24', 'ff02::1', 'ff02::1/64')),
        (('H0077',), ('0.0.0.0', '0.0.0.0/32', '::', '::/128')),
        ((), ('10.44.0.999', '1:2:3:4:5:6:7:8:9')),
    ]
    for helpers, values in ranges:
        for value in values:
            yield case('ip-' + repr(value), {**base, 'ip': value},
                '请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。',
                'R003-F12', *helpers, 'H0078', 'H0079')
    for field in ('name', 'previous_name'):
        values = [None, 0, False, [], {}, 'x' * 81, '行🧪' * 40 + '尾', ' ' * 81]
        values += ['control' + chr(number) for number in range(32)] + ['del\x7f']
        for value in values:
            yield case(field + '-' + repr(value), {**base, field: value},
                       '设备备注最多 80 个字符，不能包含换行或控制字符。', 'R003-F12', 'H0068')
    for field in ('mode', 'previous_mode'):
        for value in (None, 0, False, [], {}, 'SMART', 'proxy'):
            yield case(field + '-' + repr(value), {**base, field: value},
                       'DNS 策略只能是默认、智能分流或直连。', 'R003-F12', 'H0023')
    for field, value, entry in (('previous_name', 'old-name', 'R003-F13'),
                                ('previous_mode', 'direct', 'R003-F14')):
        yield case(field + '-stale', {**base, field: value},
                   '此设备已在其他页面修改，请重新载入后再保存。', entry, 'H0024', 'H1259', 'H1260', status=409)


def fixed_ip_shape():
    row = {'ip': '10.44.0.21', 'mac': '02:00:00:00:00:21', 'router_id': 'r-ros', 'name': '设备🧪'}
    base = {'profile_id': 'alpha', 'operation': 'add', 'devices': [row]}
    def case(label, body, message, *entries, status=400):
        return invalid('R006', 'POST', '/api/client-devices/fixed-ip/preview', label, body, message, entries, status)
    outer = '请选择 1 至 64 台设备预览固定 IP 操作。'
    for field in base:
        body = dict(base)
        body.pop(field)
        yield case('missing-' + field, body, outer, 'R006-F10', 'H0036')
    yield case('extra', {**base, 'extra': False}, outer, 'R006-F10', 'H0036')
    for value in ('ADD', 'unknown', '', None, 1, False, [], {}):
        yield case('operation-' + repr(value), {**base, 'operation': value}, outer, 'R006-F11', 'H0036')
    for label, value in (('null', None), ('number', 0), ('bool', False), ('empty-string', ''),
                         ('string', 'nonempty'), ('empty-object', {}), ('object', {'row': row}),
                         ('empty', []), ('65', [row] * 65)):
        yield case('devices-' + label,
                   {**base, 'devices': value}, outer, 'R006-F12', 'H0036')
    for label, value in (('null', None), ('number', 0), ('bool', False), ('string', ''),
                         ('array', []), ('empty-object', {}), ('extra', {**row, 'extra': ''})):
        yield case('row-' + label, {**base, 'devices': [value]},
                   '固定 IP 请求需要 IP、MAC、来源路由器和设备名称。', 'R006-F13', 'H0037')
    for field in row:
        value = dict(row)
        value.pop(field)
        yield case('row-missing-' + field, {**base, 'devices': [value]},
                   '固定 IP 请求需要 IP、MAC、来源路由器和设备名称。', 'R006-F13', 'H0037')
    message = '固定 IP 只支持有 MAC 和明确来源路由器的单个 IPv4，且不能重复选择。'
    for field, values in [('ip', (None, 0, 'bad', '::1', '10.44.0.1/24')),
                          ('mac', (None, 0, '', 'invalid', '02:00:00:00:00', '02:00:00:00:00:GG')),
                          ('router_id', (None, 0, '', [], {}))]:
        for value in values:
            yield case(field + '-' + repr(value), {**base, 'devices': [{**row, field: value}]},
                       message, 'R006-F14', 'H0038')
    for second in (row, {**row, 'router_id': 'r-ik'}):
        yield case('same-ip-' + second['router_id'], {**base, 'devices': [row, second]},
                   message, 'R006-B90', 'H0038')
    yield case('same-router-mac', {**base, 'devices': [row, {**row, 'ip': '10.44.0.22'}]},
               '同一路由器上的同一 MAC 只能选择一个固定 IP。', 'H0039', 'R006-F15')
    for value in ('beta', '', None, 0, False, True, [], {}, 1.5, ' alpha ', 'ALPHA'):
        yield case('profile-' + repr(value), {**base, 'profile_id': value},
                   '配置空间已切换，请重新载入。', 'R006-F16', 'H1263', status=409)
    for value in (None, 0, False, [], {}, '', 'x' * 1_000_001):
        yield invalid('R005', 'POST', '/api/client-devices/fixed-ip/apply',
                      'token-' + str(type(value)) + '-' + str(len(value) if isinstance(value, (str, list)) else value),
                      {'profile_id': 'alpha', 'token': value}, '缺少固定 IP 预览确认，请重新预览。', ('H0044', 'R005-F12'), 409)
    for value in ('broken-signature', 'x' * 1_000_000):
        yield invalid('R005', 'POST', '/api/client-devices/fixed-ip/apply', 'invalid-token-' + str(len(value)),
                      {'profile_id': 'alpha', 'token': value}, '固定 IP 预览无效或已过期，请重新预览确认。',
                      ('H0045', 'H0046', 'R005-B90'), 409)


def fixed_apply_shape():
    base = {'profile_id': 'alpha', 'token': 'synthetic-unverified-token'}
    for field in base:
        value = dict(base)
        value.pop(field)
        yield invalid('R005', 'POST', '/api/client-devices/fixed-ip/apply', 'missing-' + field,
                      value, '固定 IP 确认请求格式无效。', ('H1270', 'R005-F10'))
    yield invalid('R005', 'POST', '/api/client-devices/fixed-ip/apply', 'extra',
                  {**base, 'extra': False}, '固定 IP 确认请求格式无效。', ('H1270', 'R005-F10'))
    for value in ('beta', '', None, 0, False, True, [], {}, 1.5, ' alpha ', 'ALPHA'):
        yield invalid('R005', 'POST', '/api/client-devices/fixed-ip/apply', 'profile-' + repr(value),
                      {**base, 'profile_id': value}, '配置空间已切换，请重新载入。', ('H1271', 'R005-F11'), 409)


def route_edit_shape():
    values = {'cidr': '10.55.0.1/24', 'gateway': '10.44.0.2', 'enabled': True, 'comment': '手工路由🧪'}
    add = {'action': 'add', 'values': values}
    variants = []
    def append(label, body, message, *helpers):
        variants.append((label, body, message, helpers))
    for body in ([], 'text', 0, False, {**add, 'unknown': 1}):
        append('outer-' + repr(body), body, '路由操作参数格式错误', 'H0816')
    for value in (None, '', 0, False, [], {}, 'ADD', 'remove'):
        append('action-' + repr(value), {**add, 'action': value}, '只支持添加、编辑或删除单条路由', 'H0817')
    for action in ('update', 'delete'):
        for value in (None, '', 0, False, [], {}, 'x' * 129, 'id\x00', 'id\n'):
            append(action + '-id-' + repr(value), {'action': action, 'id': value, 'revision': 'a' * 64},
                   '请选择有效的路由记录', 'H0818')
        for value in (None, '', 0, False, [], {}, 'a' * 63, 'a' * 65, 'A' * 64, 'g' * 64):
            append(action + '-revision-' + repr(value), {'action': action, 'id': '*A', 'revision': value},
                   '缺少路由原始版本，请刷新列表后重试', 'H0819')
    for field in ('id', 'revision'):
        append('add-with-' + field, {**add, field: 'nonempty'}, '新增路由不能指定已有路由的身份', 'H0820')
    append('delete-with-values', {'action': 'delete', 'id': '*A', 'revision': 'a' * 64, 'values': {}},
           '删除操作不能同时修改路由字段', 'H0821')
    for value in (None, [], 'text', 0, False, {}, {**values, 'extra': 1}):
        append('values-' + repr(value), {**add, 'values': value}, '请提供网段、下一跳、启用状态和备注，不能包含其他字段', 'H0809')
    for field in values:
        body = dict(values)
        body.pop(field)
        append('values-missing-' + field, {**add, 'values': body},
               '请提供网段、下一跳、启用状态和备注，不能包含其他字段', 'H0809')
    for field in ('cidr', 'gateway'):
        for value in (None, 0, False, [], {}):
            append(field + '-type-' + repr(value), {**add, 'values': {**values, field: value}},
                   '网段和下一跳必须为 IPv4 地址文本', 'H0810')
        for value in ('bad', '::1', '10.44.0.1/33', ''):
            append(field + '-invalid-' + repr(value), {**add, 'values': {**values, field: value}},
                   '网段必须为有效 IPv4 网段，下一跳必须为单个 IPv4 地址', 'H0811', 'H0812')
    for value in ('0.0.0.0', '127.0.0.1', '224.0.0.1', '255.255.255.255'):
        append('gateway-unicast-' + value, {**add, 'values': {**values, 'gateway': value}},
               '下一跳必须为可用的单播 IPv4 地址', 'H0813')
    for value in (None, 0, 1, '', 'true', [], {}):
        append('enabled-' + repr(value), {**add, 'values': {**values, 'enabled': value}},
               '启用状态必须为 true 或 false', 'H0814')
    for value in (None, 0, False, [], {}, 'x' * 513, 'nul\x00'):
        append('comment-' + repr(value), {**add, 'values': {**values, 'comment': value}},
               '备注最多 512 个字符，且不能包含空字符', 'H0815')
    for route, suffix in (('R020', 'apply'), ('R021', 'plan')):
        for label, body, message, helpers in variants:
            yield invalid(route, 'POST', '/api/routers/r-ros/routes/' + suffix, label, body, message, helpers)


def workspace_shape():
    fields = {'ip_rule_sets': [], 'ros_policy_rule_sets': [], 'proxy_gateway_agent_id': ''}
    base = {'revision': 'stale', 'router': fields, 'base_url': ''}
    for field in base:
        body = dict(base)
        body.pop(field)
        yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'missing-' + field, body,
                      '策略路由设置格式无效', ('R015-F13',))
    for field in fields:
        body = dict(fields)
        body.pop(field)
        yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'router-missing-' + field,
                      {**base, 'router': body}, '策略路由设置格式无效', ('R015-F13',))
    for value in (None, 0, False, [], '', {**fields, 'extra': None}):
        yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'router-' + repr(value),
                      {**base, 'router': value}, '策略路由设置格式无效', ('R015-F13',))
    for value in (None, 0, False, [], {}):
        yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'base_url-' + repr(value),
                      {**base, 'base_url': value}, '策略路由设置格式无效', ('R015-F13',))
    for body in ({**base, 'extra': 1}, {**base, 'policies': []}, {**base, 'names_revision': 'stale'}):
        yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'keys-' + str(list(body)),
                      body, '策略路由设置格式无效', ('R015-F13',))
    yield invalid('R015', 'PUT', '/api/routers/r-ros/policy-settings', 'stale', base,
                  '配置已在其他页面更新，请重新载入后再保存。', ('R015-F15',), 409)
    preview = {'targets': [], 'base_url': '', 'revision': 'stale'}
    for value in (None, '', 0, False, {}, [1], [None], [[]], ['same', 'same']):
        yield invalid('R016', 'POST', '/api/routers/r-ros/policy-settings/preview', 'targets-' + repr(value),
                      {**preview, 'targets': value}, '发布预览参数无效', ('R016-F10',))
    for value in (None, 0, False, [], {}):
        yield invalid('R016', 'POST', '/api/routers/r-ros/policy-settings/preview', 'base_url-' + repr(value),
                      {**preview, 'base_url': value}, '发布预览参数无效', ('R016-F10',))
    yield invalid('R016', 'POST', '/api/routers/r-ros/policy-settings/preview', 'stale', preview,
                  '配置已更新，请重新载入后预览。', ('R016-F13',), 409)


def rule_sync_shape():
    base = {'auto_rule_sync': False, 'rule_sync_interval': 60}
    for field in base:
        body = dict(base)
        body.pop(field)
        yield invalid('R024', 'PUT', '/api/routers/r-ros/rule-sync', 'missing-' + field, body,
                      '请提供同步开关和检查间隔', ('R024-F13',))
    yield invalid('R024', 'PUT', '/api/routers/r-ros/rule-sync', 'extra', {**base, 'extra': False},
                  '请提供同步开关和检查间隔', ('R024-F13', 'R024-B04'))
    for field, values, message, helper in (
        ('auto_rule_sync', (None, 0, 1, 'false', [], {}), '远程规则自动同步仅支持 RouterOS，开关必须为布尔值', 'H1236'),
        ('rule_sync_interval', (None, 0, 14, 16, 1441, True, False, 60.0, '60', [], {}), '请选择有效的规则检查间隔', 'H1237')):
        for value in values:
            yield invalid('R024', 'PUT', '/api/routers/r-ros/rule-sync', field + '-' + repr(value),
                          {**base, field: value}, message, ('R024-F14', helper))
