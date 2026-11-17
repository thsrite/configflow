"""Discovery and DHCP transactions through the real client-device HTTP API."""
from cases import Case, Ref
from seed import router


def options():
    return {'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]},
                      'device_notes': {'10.44.0.21': '动态设备🧪', '10.44.0.22': '静态设备🧪',
                                       '10.44.0.101': '爱快设备🧪'}, 'client_fixed_ips': {}},
            'beta': {'routers': {'items': [router()]}}}


def device_world():
    return {'action': 'seed', 'ros': {
        '/ip/address': [{'.id': '*A0', 'address': '10.44.0.1/24', 'interface': 'bridge-lan',
                         'disabled': 'false', 'dynamic': 'false'}],
        '/ip/dhcp-server': [{'.id': '*A1', 'name': 'lan', 'interface': 'bridge-lan',
                            'disabled': 'false', 'relay': '0.0.0.0', 'lease-time': '30m'}],
        '/ip/dhcp-server/network': [{'.id': '*A2', 'address': '10.44.0.0/24',
                                    'gateway': '10.44.0.1', 'dns-server': '10.44.0.53'}],
        '/ip/arp': [{'.id': '*A3', 'address': '10.44.0.21', 'mac-address': '02:00:00:00:00:21',
                     'interface': 'bridge-lan', 'status': 'reachable'},
                    {'.id': '*A4', 'address': '10.44.0.22', 'mac-address': '02:00:00:00:00:22',
                     'interface': 'bridge-lan', 'status': 'stale'}],
        '/ip/dhcp-server/lease': [
            {'.id': '*B0', 'address': '10.44.0.21', 'mac-address': '02:00:00:00:00:21',
             'server': 'lan', 'dynamic': 'true', 'disabled': 'false', 'status': 'bound',
             'last-seen': '10s', 'expires-after': '15m', 'comment': '原始备注🧪', 'always-broadcast': 'true'},
            {'.id': '*B1', 'address': '10.44.0.23', 'mac-address': '02:00:00:00:00:23',
             'server': 'lan', 'dynamic': 'false', 'disabled': 'false', 'comment': 'foreign 不得接管'}]},
        'ikuai': {'dhcp_server': [{'id': 1, 'interface': 'lan1', 'enabled': 'yes', 'status': 1,
                                   'check_relay_only': 0, 'addr_pool': '10.44.0.100-10.44.0.200',
                                   'netmask': '255.255.255.0', 'gateway': '10.44.0.1'}],
                  'dhcp_static': [{'id': 70, 'enabled': 'yes', 'tagname': 'foreign-tag', 'interface': 'lan1',
                                   'mac': '02:00:00:00:00:FE', 'gateway': '10.44.0.1', 'dns1': '10.44.0.53',
                                   'dns2': '', 'ip_addr': '10.44.0.254', 'comment': 'foreign 不得接管'}],
                  'monitor_lanip': [{'ip_addr': '10.44.0.101', 'mac': '02:00:00:00:00:65',
                                     'interface': 'lan1', 'comment': '终端备注🧪', 'termname': 'synthetic'}],
                  'dhcp_lease': [{'ip_addr': '10.44.0.101', 'mac': '02:00:00:00:00:65',
                                  'interface': 'lan1', 'start_time': 1000, 'end_time': 2800, 'timeout': 1700}],
                  'lan': [{'id': 1, 'ip_mask': '10.44.0.1/255.255.255.0'}]}}


FOREIGN = (('ros', '/ip/dhcp-server/lease', '.id', '*B1'), ('ikuai', 'dhcp_static', 'id', 70))


def request(operation, ip, mac, router_id, name):
    return {'profile_id': 'alpha', 'operation': operation,
            'devices': [{'ip': ip, 'mac': mac, 'router_id': router_id, 'name': name}]}


def fixed_ip():
    records = [('dynamic', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '预览名称不同于保存🧪', 'S01', 'S01'),
               ('new', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '静态设备🧪', 'S02', 'S02'),
               ('ikuai', '10.44.0.101', '02:00:00:00:00:65', 'r-ik', '爱快设备🧪', 'S03', 'S03')]
    for index, (label, ip, mac, router_id, name, preview_entry, apply_entry) in enumerate(records):
        preview = 'R006/fixed-add-' + label
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
                   ('R006-' + preview_entry, 'R006-B06', 'R006-B90'),
                   document=request('add', ip, mac.lower().replace(':', '-'), router_id, name),
                   prepare=(device_world(),) if index == 0 else (), unchanged=False, effect='device-read',
                   checks={'success': True, 'actions/0/ip': ip, 'actions/0/router_id': router_id},
                   formats={'token': r'.{1,1000000}'}, foreign=FOREIGN)
        binding = 'client_fixed_ips/' + ip
        fake_checks = {'ros/~1ip~1dhcp-server~1lease/0/dynamic': 'false'} if label == 'dynamic' else {}
        yield Case('R005/fixed-add-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
                   ('R005-' + apply_entry, 'R005-S90', 'R005-B06'),
                   document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')},
                   requires=('R006',), unchanged=False, effect='profile-write-device', foreign=FOREIGN,
                   checks={'success': True, 'devices/' + str(index) + '/ip': ip,
                           'devices/' + str(index) + '/fixed_ip/router_id': router_id},
                   stored={binding + '/ip': ip, binding + '/mac': mac, binding + '/router_id': router_id,
                           'device_notes/' + ip: options()['alpha']['device_notes'][ip]},
                   fake_checks=fake_checks,
                   fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2 if label == 'dynamic' else 3,
                                 'ikuai/dhcp_static': 2 if label == 'ikuai' else 1})
    yield Case('R002/fixed-bindings-visible', 'R002', 'GET', '/api/client-devices', 200, ('R002-S05',),
               checks={'devices/0/fixed_ip/vendor': 'routeros', 'devices/2/fixed_ip/vendor': 'ikuai'})
    for index, (label, ip, mac, router_id, name, _, _) in enumerate(records):
        preview = 'R006/fixed-delete-' + label
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('R006-S04',),
                   document=request('delete', ip, mac, router_id, name), unchanged=False, effect='device-read', foreign=FOREIGN,
                   checks={'success': True, 'actions/0/ip': ip}, formats={'token': r'.{1,1000000}'})
        yield Case('R005/fixed-delete-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
                   ('R005-S05',) if router_id == 'r-ik' else ('R005-S04',),
                   document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
                   unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True},
                   stored_absent=('client_fixed_ips/' + ip,),
                   stored={'device_notes/' + ip: options()['alpha']['device_notes'][ip]},
                   fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2 if index == 0 else 1,
                                 'ikuai/dhcp_static': 1 if index == 2 else 2})


def discovery():
    yield Case('R004/dual-vendor-first', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
               ('R004-S01', 'R004-S02', 'R004-S03', 'R004-S08', 'R004-B04'),
               prepare=(device_world(),), unchanged=False, effect='device-read', foreign=FOREIGN,
               checks={'success': True, 'state': 'ok', 'cached': False, 'sources/0/ok': True, 'sources/1/ok': True,
                       'devices/0/presence': 'online', 'devices/0/fixed_ip/eligible': True,
                       'devices/2/presence': 'online', 'devices/2/last_seen': 100}, times=('checked_at',))
    yield Case('R004/dual-vendor-cached', 'R004', 'GET', '/api/client-devices/discovery', 200,
               ('R004-S07',), checks={'cached': True, 'checked_at': Ref('R004/dual-vendor-first', '/body/checked_at')},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R004/refresh-literal-true-is-cache', 'R004', 'GET', '/api/client-devices/discovery?refresh=true', 200,
               ('R004-S07',), checks={'cached': True},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R003/discovery-rename', 'R003', 'PUT', '/api/client-devices', 200, ('R003-S08',),
               document={'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '缓存也显示新名称🧪', 'mode': '',
                         'previous_name': '动态设备🧪', 'previous_mode': '', 'base_url': ''},
               unchanged=False, effect='profile-write')
    yield Case('R004/cached-renamed', 'R004', 'GET', '/api/client-devices/discovery', 200, ('R004-S10',),
               checks={'cached': True, 'devices/0/name': '缓存也显示新名称🧪'},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))


def fixed_preview_guards():
    for label, operation, ip, mac, router_id, message, entry in [
        ('missing-router', 'add', '10.44.0.21', '02:00:00:00:00:21', 'missing',
         '发现这台设备的路由器已不存在或不支持写入，请刷新设备列表。', 'F18'),
        ('unseen-mac', 'add', '10.44.0.21', '02:00:00:00:00:99', 'r-ros',
         '10.44.0.21 的 MAC 或来源路由器已变化，请重新发现设备。', 'F19'),
        ('unseen-ip', 'add', '10.44.0.29', '02:00:00:00:00:21', 'r-ros',
         '10.44.0.29 的 MAC 或来源路由器已变化，请重新发现设备。', 'F19'),
        ('wrong-vendor-source', 'add', '10.44.0.21', '02:00:00:00:00:21', 'r-ik',
         '10.44.0.21 的 MAC 或来源路由器已变化，请重新发现设备。', 'F19'),
        ('delete-foreign-ros', 'delete', '10.44.0.23', '02:00:00:00:00:23', 'r-ros',
         '没有这台设备由 ConfigFlow 创建的固定 IP 记录，不会删除其他静态租约。', 'F22'),
        ('delete-foreign-ik', 'delete', '10.44.0.254', '02:00:00:00:00:FE', 'r-ik',
         '没有这台设备由 ConfigFlow 创建的固定 IP 记录，不会删除其他静态租约。', 'F22')]:
        yield Case('R006/' + label, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 409,
                   ('R006-' + entry, 'H0040' if entry == 'F18' else 'H0042' if entry == 'F19' else 'H0043'),
                   document=request(operation, ip, mac, router_id, '检查🧪'), prepare=(device_world(),),
                   unchanged=False, effect='device-read', message=message, foreign=FOREIGN)
    definitions = []
    no_scope = 'IP 不在该路由器任何已启用且可确认的 DHCP 网段内'
    for label, table, field, value in [
        ('server-disabled', '/ip/dhcp-server', 'disabled', 'true'),
        ('server-invalid', '/ip/dhcp-server', 'invalid', True),
        ('server-relayed', '/ip/dhcp-server', 'relay', '10.44.0.9'),
        ('server-interface-empty', '/ip/dhcp-server', 'interface', ''),
        ('address-disabled', '/ip/address', 'disabled', True),
        ('address-invalid', '/ip/address', 'invalid', 'yes'),
        ('address-malformed', '/ip/address', 'address', 'bad/33'),
        ('network-disabled', '/ip/dhcp-server/network', 'disabled', 'true'),
        ('network-invalid', '/ip/dhcp-server/network', 'invalid', True),
        ('network-malformed', '/ip/dhcp-server/network', 'address', 'bad/33'),
        ('lease-wrong-server', '/ip/dhcp-server/lease', 'server', 'other')]:
        world = device_world()
        world['ros'][table][0][field] = value
        definitions.append(('ros-' + label, world, 'r-ros', no_scope, 'F20'))
    world = device_world()
    world['ros']['/ip/dhcp-server'].append({**world['ros']['/ip/dhcp-server'][0], '.id': '*AC', 'name': 'other'})
    # No current lease: both servers remain eligible and ambiguity must be visible.
    world['ros']['/ip/dhcp-server/lease'] = [world['ros']['/ip/dhcp-server/lease'][1]]
    definitions.append(('ros-ambiguous-scope', world, 'r-ros', 'IP 对应多个 DHCP 服务，无法唯一确定固定 IP 的目标', 'F20'))
    for label, values, message in [
        ('static-other-mac', {'mac-address': '02:00:00:00:00:99', 'dynamic': 'false'}, '这个 IP 已静态绑定给其他 MAC，不能覆盖'),
        ('static-own-mac', {'dynamic': 'false'}, '这个 MAC 已有静态租约，保留原设置，不覆盖'),
        ('dynamic-other-ip', {'address': '10.44.0.24'}, '当前 DHCP 租约的 IP 与 MAC 已变化，请重新发现设备'),
        ('dynamic-disabled', {'disabled': 'true'}, '该动态租约由 RADIUS 管理、被禁用或被阻止，不能自动固定'),
        ('dynamic-radius', {'radius': True}, '该动态租约由 RADIUS 管理、被禁用或被阻止，不能自动固定'),
        ('dynamic-blocked', {'blocked': 'yes'}, '该动态租约由 RADIUS 管理、被禁用或被阻止，不能自动固定'),
        ('bad-id', {'.id': '*ZZ'}, 'RouterOS DHCP 租约缺少有效对象 ID')]:
        world = device_world()
        world['ros']['/ip/dhcp-server/lease'][0].update(values)
        definitions.append(('ros-' + label, world, 'r-ros', message, 'F21'))
    for label, same_id, message in [('duplicate-id', True, 'RouterOS DHCP 租约 ID 不唯一'),
                                    ('duplicate-dynamic', False, '存在多条匹配的动态租约，无法安全确定目标')]:
        world = device_world()
        world['ros']['/ip/dhcp-server/lease'].append({**world['ros']['/ip/dhcp-server/lease'][0],
                                                   '.id': '*B0' if same_id else '*BE'})
        definitions.append(('ros-' + label, world, 'r-ros', message, 'F21'))
    no_scope = 'IP 不在该路由器任何已启用且运行中的 DHCP 网段内，或是网关地址'
    for label, field, value in [('disabled', 'enabled', 'no'), ('status-string', 'status', '1'),
        ('status-bool', 'status', True), ('status-off', 'status', 0), ('relayed', 'check_relay_only', 1),
        ('interface-empty', 'interface', ''), ('pool-reversed', 'addr_pool', '10.44.0.200-10.44.0.100'),
        ('pool-cross-subnet', 'addr_pool', '10.44.0.100-10.45.0.200'), ('mask-bad', 'netmask', 'bad'),
        ('gateway-outside', 'gateway', '10.45.0.1'), ('gateway-self', 'gateway', '10.44.0.101')]:
        world = device_world()
        world['ikuai']['dhcp_server'][0][field] = value
        definitions.append(('ik-' + label, world, 'r-ik', no_scope, 'F20'))
    world = device_world()
    world['ikuai']['dhcp_server'].append({**world['ikuai']['dhcp_server'][0], 'id': 2})
    definitions.append(('ik-ambiguous-scope', world, 'r-ik', 'IP 对应多个 DHCP 服务，无法唯一确定固定 IP 的目标', 'F20'))
    for label, values, message in [
        ('wrong-lease-mac', {'mac': '02:00:00:00:00:99'}, '当前 DHCP 租约的 IP 与 MAC 已变化，请重新发现设备'),
        ('wrong-lease-ip', {'ip_addr': '10.44.0.102'}, '当前 DHCP 租约的 IP 与 MAC 已变化，请重新发现设备'),
        ('wrong-lease-interface', {'interface': 'other'}, '当前租约接口与 DHCP 网段不一致，请重新发现设备')]:
        world = device_world()
        world['ikuai']['dhcp_lease'][0].update(values)
        definitions.append(('ik-' + label, world, 'r-ik', message, 'F21'))
    for field, value in [('enabled', 'maybe'), ('dns1', 1), ('id', True), ('id', '70'),
                         ('mac', 'bad'), ('ip_addr', 'bad'), ('unverified', 'must refuse')]:
        world = device_world()
        world['ikuai']['dhcp_static'][0][field] = value
        definitions.append(('ik-static-' + field + '-' + str(value), world, 'r-ik',
                            'iKuai dhcp_static列表缺少唯一有效 ID，无法安全编辑' if field == 'id' else
                            'iKuai 静态 DHCP 配置字段不完整或与已验证格式不同，无法安全修改。', 'F21'))
    for label, world, router_id, message, entry in definitions:
        ikuai = router_id == 'r-ik'
        yield Case('R006/' + label, 'R006', 'POST', '/api/client-devices/fixed-ip/preview',
                   409 if ikuai and not label.startswith('ik-static-') else 502,
                   ('R006-' + entry,), document=request('add', '10.44.0.101' if ikuai else '10.44.0.21',
                      '02:00:00:00:00:65' if ikuai else '02:00:00:00:00:21', router_id, '检查🧪'),
                   prepare=(world,), unchanged=False, effect='device-read', message=message)


def discovery_failures():
    yield Case('R004/outage-prior-good', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
               ('R004-S03',), prepare=(device_world(),), unchanged=False, effect='device-read',
               checks={'state': 'ok', 'cached': False}, times=('checked_at',))
    ros_fail = {'action': 'fault', 'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'},
                'status': 500, 'body': {'message': 'synthetic ARP refusal'}}
    ik_fail = {'action': 'fault', 'match': {'peer': 'ikuai', '/body/func_name': 'monitor_lanip'},
               'status': 200, 'body': {'Result': 10000, 'code': 3001, 'message': 'synthetic terminal refusal'}}
    yield Case('R004/failed-refresh-keeps-good', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
               ('R004-S09',), prepare=(ros_fail, ik_fail), unchanged=False, effect='device-read',
               checks={'state': 'ok', 'cached': True,
                       'checked_at': Ref('R004/outage-prior-good', '/body/checked_at')}, formats={'refresh_error': r'.+'})
    definitions = [
        ('required-arp', (ros_fail,), {'state': 'partial', 'sources/0/ok': False, 'sources/1/ok': True,
                                      'devices/0/presence': 'unknown'}, ('R004-S05', 'R004-F11')),
        ('required-terminal', (ik_fail,), {'state': 'partial', 'sources/0/ok': True, 'sources/1/ok': False}, ('R004-S05',)),
        ('both-required', (ros_fail, ik_fail), {'state': 'failed', 'sources/0/ok': False, 'sources/1/ok': False}, ('R004-S06',)),
        ('ros-enrich', ({'action': 'fault', 'match': {'peer': 'ros', 'path': '/rest/ip/dhcp-server/network'},
                        'status': 500, 'body': {'message': 'synthetic reservation denial'}},),
         {'state': 'ok', 'devices/0/presence': 'online', 'devices/0/fixed_ip/eligible': False}, ('R004-F15', 'H0062')),
        ('optional-lease', ({'action': 'fault', 'match': {'peer': 'ros', 'path': '/rest/ip/dhcp-server/lease'},
                            'status': 500, 'body': {'message': 'synthetic lease denial'}},),
         {'state': 'ok', 'sources/0/ok': True, 'devices/0/presence': 'online'}, ('R004-F12',)),
        ('optional-ik-lease', ({'action': 'fault', 'match': {'peer': 'ikuai', '/body/func_name': 'dhcp_lease'},
                               'status': 200, 'body': {'Result': 10000, 'code': 3001}},),
         {'state': 'ok', 'sources/1/ok': True, 'devices/2/last_seen': None}, ('R004-F12',)),
        ('optional-ik-lan', ({'action': 'fault', 'match': {'peer': 'ikuai', '/body/func_name': 'lan'},
                             'status': 200, 'body': {'Result': 10000, 'code': 3001}},),
         {'state': 'ok', 'sources/1/ok': True}, ('R004-F12',))]
    for index, (label, faults, checks, entries) in enumerate(definitions):
        # Editing via HTTP changes the actual router signature, invalidating
        # the previous good cache without touching process-private state.
        rename = {'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ros',
                  'document': {'name': '发现来源变体🧪' + str(index)}, 'expected': 200}
        yield Case('R004/' + label, 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
                   entries, prepare=(device_world(), rename, *faults), unchanged=False, effect='device-read',
                   checks={**checks, 'cached': False}, foreign=FOREIGN, times=('checked_at',))
