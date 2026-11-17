"""Stored device policies exercised only through public import and router HTTP."""
from cases import Case
from seed import router


ADDRESS_MESSAGE = '请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。'
NAME_MESSAGE = '设备备注最多 80 个字符，不能包含换行或控制字符。'


def invalid_variants():
    row = {'ip': '10.44.0.21', 'name': '合成策略🧪', 'mode': 'smart'}
    variants = []
    for label, value in (('null', None), ('false', False), ('number', 0), ('string', ''), ('object', {}),
                         ('513', [{**row, 'ip': '10.49.' + str(number // 256) + '.' + str(number % 256)}
                                  for number in range(513)])):
        variants.append(('shape-' + label, value, 'H0080', '设备策略必须为列表，最多 512 条策略。'))
    for label, value in (('null', None), ('false', False), ('number', 0), ('string', ''), ('array', []),
                         ('extra-key', {**row, 'extra': True})):
        variants.append(('row-' + label, [value], 'H0081', '设备策略仅支持 IP / 网段、备注和策略。'))
    for label, ips, canonical in (
        ('host-prefix', ('10.44.0.21', '10.44.0.21/32'), '10.44.0.21'),
        ('mapped-host', ('::ffff:10.44.0.21', '10.44.0.21'), '10.44.0.21'),
        ('ipv6-compression', ('FD42:CF:0000::21', 'fd42:cf::21/128'), 'fd42:cf::21'),
        ('network-mask', ('10.44.0.21/24', '10.44.0.42/24'), '10.44.0.0/24')):
        variants.append(('duplicate-' + label, [{**row, 'ip': ip} for ip in ips], 'H0082',
                         '客户端 IP / 网段重复：' + canonical))
    names = [('null', None), ('false', False), ('number', 0), ('array', []), ('object', {}),
             ('ascii81', 'x' * 81), ('unicode81', '行🧪' * 40 + '尾'), ('space81', ' ' * 81)]
    names += [('control' + str(number), 'name' + chr(number)) for number in range(32)]
    for label, value in names:
        variants.append(('name-' + label, [{**row, 'name': value}], 'H0083', NAME_MESSAGE))
    for label, value in (('null', None), ('false', False), ('number', 0), ('array', []), ('object', {}),
                         ('empty', ''), ('upper', 'SMART'), ('proxy', 'proxy')):
        variants.append(('mode-' + label, [{**row, 'mode': value}], 'H0084', '设备策略必须为智能分流或直连。'))
    variants.append(('mode-missing', [{key: value for key, value in row.items() if key != 'mode'}],
                     'H0084', '设备策略必须为智能分流或直连。'))
    return variants


def invalid_cases(label, entry, message):
    body = {'profile_id': 'alpha', 'ip': '10.44.0.22', 'name': 'must not persist', 'mode': '',
            'previous_name': '', 'previous_mode': '', 'base_url': ''}
    return tuple(Case(route + '/stored-policy-' + label, route, method, path, 400,
        (entry,) + (('H1261', 'H1262') if route == 'R003' else ()),
        document=body if method == 'PUT' else None, message=message)
        for route, method, path in (('R002', 'GET', '/api/client-devices'),
                                   ('R003', 'PUT', '/api/client-devices'),
                                   ('R014', 'GET', '/api/routers/r-ros/policy-settings')))


def worlds():
    result = []
    for label, rows, entry, message in invalid_variants():
        options = {'alpha': {'routers': {'items': [router()]}, 'oxidns': {'device_policies': rows}}}
        items = invalid_cases(label, entry, message)
        result.append(('stored-policy-' + label, options, (lambda items=items: items,)))
    return result


def invalid_ip_worlds():
    row = {'ip': '10.44.0.21', 'name': '合法旧备注🧪', 'mode': 'smart'}
    groups = (
        ('H0071', (None, 0, False, [], {})),
        ('H0072', ('', 'bad', '10.44.0.1/1/2', '10.44.0.1%lo', '地址', '１０.４４.０.１')),
        ('H0073', ('10.44.0.1/', '10.44.0.1/-1', '10.44.0.1/+1', '10.44.0.1/3a',
                   '10.44.0.1/0001', '10.44.0.1/ 24', '10.44.0.1/２４')),
        ('H0074', ('10.44.0.1/33', 'fd42:cf::1/129')),
        ('H0075', ('::ffff:10.44.0.1/95',)),
        ('H0076', ('224.0.0.1', '224.0.0.1/24', 'ff02::1', 'ff02::1/64')),
        ('H0077', ('0.0.0.0', '0.0.0.0/32', '::', '::/128')),
        ('H0078', ('10.44.0.999', '1:2:3:4:5:6:7:8:9')),
    )
    values = [('missing', {key: value for key, value in row.items() if key != 'ip'}, 'H0071')]
    values += [(entry + '-' + str(index), {**row, 'ip': value}, entry)
               for entry, candidates in groups for index, value in enumerate(candidates)]
    result = []
    for label, bad_row, entry in values:
        label = 'ip-' + label
        options = {'alpha': {'routers': {'items': [router()]}, 'oxidns': {'device_policies': [bad_row]}}}
        items = invalid_cases(label, entry,
            '请输入有效的客户端 IPv4、IPv6 或 CIDR 网段，例如 192.168.50.0/24。')
        result.append(('stored-policy-' + label, options, (lambda items=items: items,)))
    return result


def legacy_options():
    rows = [{'ip': '::ffff:10.44.0.21', 'name': '\x7f旧名\x7f ', 'mode': 'smart'},
            {'ip': 'FD42:CF:0000::21/128', 'name': '行🧪' * 40, 'mode': 'smart'},
            {'ip': '10.45.0.42/24', 'name': '网段', 'mode': 'smart'}]
    return {'alpha': {'oxidns': {'device_policies': rows},
                      'device_notes': {'10.44.0.21': '覆盖名称', 'fd42:cf::21': ''}}}


def legacy():
    expected = [{'ip': '10.44.0.21', 'name': '覆盖名称', 'mode': 'smart'},
                {'ip': 'fd42:cf::21', 'name': '', 'mode': 'smart'},
                {'ip': '10.45.0.0/24', 'name': '网段', 'mode': 'smart'}]
    yield Case('R002/stored-policy-legacy-del-and-name-precedence', 'R002', 'GET', '/api/client-devices', 200,
        ('R002-S01', 'R002-B03', 'R002-B04'), checks={'devices': expected, 'editable': True})
    yield Case('R003/stored-policy-legacy-rename-keeps-raw-rows', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S08',), document={'profile_id': 'alpha', 'ip': '10.45.0.42/24', 'name': '新网段备注🧪',
            'mode': 'smart', 'previous_name': '网段', 'previous_mode': 'smart', 'base_url': ''},
        unchanged=False, effect='profile-write', retained_fields=('oxidns', 'routers', 'client_fixed_ips'),
        checks={'devices/2/name': '新网段备注🧪'}, stored={'device_notes/10.45.0.0~124': '新网段备注🧪'})


def capacity_options():
    return {'alpha': {'oxidns': {'device_policies': [
        {'ip': '10.49.' + str(number // 256) + '.' + str(number % 256), 'name': '策略🧪', 'mode': 'smart'}
        for number in range(512)]}}}


def capacity():
    yield Case('R002/stored-policy-512-accepted', 'R002', 'GET', '/api/client-devices', 200,
        ('R002-S01', 'H0085'), checks={'editable': True, 'devices/511': {'ip': '10.49.1.255', 'name': '策略🧪', 'mode': 'smart'}})
    yield Case('R003/policy-513-growth-refused-atomically', 'R003', 'PUT', '/api/client-devices', 400,
        ('H0080', 'H1261', 'H1262'), document={'profile_id': 'alpha', 'ip': '10.77.0.250',
        'name': '不得半存🧪', 'mode': 'direct', 'previous_name': '', 'previous_mode': '', 'base_url': ''},
        message='设备策略必须为列表，最多 512 条策略。')
