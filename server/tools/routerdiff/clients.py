"""Discovery and DHCP transactions through the real client-device HTTP API."""
from cases import Case, Ref
from seed import router, DEVICE_PASSWORD


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
                   ('R006-' + preview_entry, 'R006-B06', 'R006-B90', 'H0036', 'H0037'),
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
                   unchanged=False, effect='profile-write-device', foreign=FOREIGN,
                   checks={'success': True},
                   stored_absent=('client_fixed_ips/' + ip,),
                   stored={'device_notes/' + ip: options()['alpha']['device_notes'][ip]},
                   fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2 if index == 0 else 1,
                                 'ikuai/dhcp_static': 1 if index == 2 else 2})



def notes_keep_reservation():
    preview = 'R006/note-retention-add-preview'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
        ('R006-S01',), document=request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '动态设备🧪'),
        prepare=(device_world(),), unchanged=False, effect='device-read', foreign=FOREIGN)
    yield Case('R005/note-retention-add-confirmed', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
        ('R005-S01',), document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')},
        requires=('R006',), unchanged=False, effect='profile-write-device', foreign=FOREIGN,
        fake_checks={'ros/~1ip~1dhcp-server~1lease/0/dynamic': 'false'},
        stored={'client_fixed_ips/10.44.0.21/id': '*B0'})
    base = {'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '改名而不改租约🧪', 'mode': '',
            'previous_name': '动态设备🧪', 'previous_mode': '', 'base_url': ''}
    quiet = ({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0})
    yield Case('R003/note-rename-preserves-real-binding', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S08',), document=base, unchanged=False, effect='profile-write', foreign=FOREIGN,
        retained_fields=('oxidns', 'routers', 'client_fixed_ips'), expected_requests=quiet,
        checks={'devices/0/name': '改名而不改租约🧪', 'devices/0/fixed_ip/id': '*B0'})
    yield Case('R003/note-remove-forgets-binding-keeps-reservation', 'R003', 'PUT', '/api/client-devices', 200,
        ('R003-S07',), document={**base, 'name': '', 'previous_name': base['name']},
        unchanged=False, effect='profile-write', foreign=FOREIGN,
        retained_fields=('oxidns', 'routers'), expected_requests=quiet,
        stored_absent=('device_notes/10.44.0.21', 'client_fixed_ips/10.44.0.21'),
        checks={'devices/0/ip': '10.44.0.22'},
        fake_checks={'ros/~1ip~1dhcp-server~1lease/0/dynamic': 'false',
                     'ros/~1ip~1dhcp-server~1lease/0/address': '10.44.0.21'},
        fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2})

def discovery():
    yield Case('R004/dual-vendor-first', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
               ('T03', 'R004-S01', 'R004-S02', 'R004-S03', 'R004-S08', 'R004-B04'),
               prepare=(device_world(),), unchanged=False, effect='device-read', foreign=FOREIGN,
               checks={'success': True, 'state': 'ok', 'cached': False, 'sources/0/ok': True, 'sources/1/ok': True,
                       'devices/0/presence': 'online', 'devices/0/fixed_ip/eligible': True,
                       'devices/2/presence': 'online', 'devices/2/last_seen': 100}, times=('checked_at',))
    yield Case('R004/dual-vendor-cached', 'R004', 'GET', '/api/client-devices/discovery', 200,
               ('T03', 'R004-S07'), checks={'cached': True, 'checked_at': Ref('R004/dual-vendor-first', '/body/checked_at')},
               cached_times={'checked_at': Ref('R004/dual-vendor-first', '/body/checked_at')},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R004/refresh-literal-true-is-cache', 'R004', 'GET', '/api/client-devices/discovery?refresh=true', 200,
               ('T03', 'R004-S07'), checks={'cached': True},
               cached_times={'checked_at': Ref('R004/dual-vendor-first', '/body/checked_at')},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R003/discovery-rename', 'R003', 'PUT', '/api/client-devices', 200, ('T03', 'R003-S08'),
               document={'profile_id': 'alpha', 'ip': '10.44.0.21', 'name': '缓存也显示新名称🧪', 'mode': '',
                         'previous_name': '动态设备🧪', 'previous_mode': '', 'base_url': ''},
               unchanged=False, effect='profile-write')
    yield Case('R004/cached-renamed', 'R004', 'GET', '/api/client-devices/discovery', 200, ('T03', 'R004-S10'),
               cached_times={'checked_at': Ref('R004/dual-vendor-first', '/body/checked_at')},
               checks={'cached': True, 'devices/0/name': '缓存也显示新名称🧪'},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))





def cache_options():
    value = options()
    value['alpha']['routers']['items'] = [router()]
    return value


def cache_signature():
    arp = {'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'}, 'count': 1}
    quiet = ({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0})
    yield Case('R004/cache-signature-initial', 'R004', 'GET', '/api/client-devices/discovery', 200,
        ('T03',), prepare=(device_world(),), unchanged=False, effect='device-read',
        times=('checked_at',),
        checks={'cached': False, 'state': 'ok'}, expected_requests=(arp,))
    edits = (('password', {'password': 'synthetic-rotated-but-not-on-device-0123456789'}, 'failed'),
             ('restore', {'password': DEVICE_PASSWORD}, 'ok'), ('tls', {'verify_tls': False}, 'ok'))
    for label, body, state in edits:
        yield Case('R010/cache-signature-edit-' + label, 'R010', 'PUT', '/api/routers/r-ros', 200,
            ('T03',), document=body, unchanged=False, effect='profile-write',
            retained_fields=('oxidns', 'device_notes', 'client_fixed_ips'), expected_requests=quiet)
        yield Case('R004/cache-signature-invalidated-' + label, 'R004', 'GET', '/api/client-devices/discovery', 200,
            ('T03',), unchanged=False, effect='device-read',
            times=('checked_at',),
            checks={'cached': False, 'state': state, 'sources/0/ok': state == 'ok'}, expected_requests=(arp,))
    yield Case('R010/cache-signature-noop-revision-only', 'R010', 'PUT', '/api/routers/r-ros', 200,
        ('T03',), document={'verify_tls': False}, unchanged=False, effect='profile-write',
        retained_fields=('oxidns', 'device_notes', 'client_fixed_ips'), expected_requests=quiet)
    yield Case('R004/cache-signature-noop-keeps-good', 'R004', 'GET', '/api/client-devices/discovery', 200,
        ('T03',),
        cached_times={'checked_at': Ref('R004/cache-signature-invalidated-tls', '/body/checked_at')},
        checks={'cached': True, 'state': 'ok',
                         'checked_at': Ref('R004/cache-signature-invalidated-tls', '/body/checked_at')},
        expected_requests=quiet)

def cache_lru_options():
    def profile(ident):
        return {'routers': {'items': [router()]}, 'device_notes': {'10.44.0.21': '缓存隔离 ' + ident}}
    return {'alpha': profile('alpha'), 'beta': profile('beta'),
            'additional_profiles': {'cache-' + str(number).zfill(3): profile('cache-' + str(number).zfill(3))
                                    for number in range(127)}}


def cache_lru():
    arp = {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'}
    sequence = [('alpha-first', 'alpha', False), ('beta-first', 'beta', False)]
    sequence += [('fill-' + str(number), 'cache-' + str(number).zfill(3), False) for number in range(126)]
    sequence += [('touch-alpha-at128', 'alpha', True), ('insert129-evicts-beta', 'cache-126', False),
                 ('alpha-recent-survives', 'alpha', True), ('beta-least-recent-rereads', 'beta', False),
                 ('cache001-survives', 'cache-001', True), ('cache000-evicted-by-beta-rereads', 'cache-000', False)]
    fresh_sources = {}
    for index, (label, profile, cached) in enumerate(sequence):
        source = fresh_sources.get(profile)
        if not cached:
            fresh_sources[profile] = 'R004/cache-lru-' + label
        yield Case('R004/cache-lru-' + label, 'R004', 'GET', '/api/client-devices/discovery?profile=' + profile, 200,
            ('T03', 'R004-S07') if cached else ('T03',), profile=profile,
            prepare=(device_world(),) if index == 0 else (), unchanged=cached,
            effect='' if cached else 'device-read',
            times=() if cached else ('checked_at',),
            cached_times={'checked_at': Ref(source, '/body/checked_at')} if cached else {},
            checks={'profile_id': profile, 'cached': cached,
                    'devices/0/name': '缓存隔离 ' + profile},
            expected_requests=({'match': arp, 'count': 0 if cached else 1},))


def cache_coalescing():
    arp = {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'}
    path = '/api/client-devices/discovery?refresh=1'
    first = 'R004/cache-coalesce-owner'
    yield Case(first, 'R004', 'GET', path, 200, ('T03',),
        prepare=(device_world(), {'action': 'fault', 'match': arp, 'gate': 'coalesce-arp',
                                 'apply': True, 'passthrough': True},
                 {'rpc': 'begin', 'ident': 'coalesce-owner', 'method': 'GET', 'path': path},
                 {'action': 'wait', 'gate': 'coalesce-arp'},
                 {'rpc': 'begin', 'ident': 'coalesce-waiter', 'method': 'GET', 'path': path},
                 {'rpc': 'pending_status', 'expected': {'coalesce-owner': False, 'coalesce-waiter': False}},
                 {'action': 'release', 'gate': 'coalesce-arp'}),
        join='coalesce-owner', unchanged=False, effect='device-read',
        checks={'state': 'ok', 'cached': False, 'stale': False}, expected_requests=({'match': arp, 'count': 1},))
    yield Case('R004/cache-coalesce-waiter-same-snapshot', 'R004', 'GET', path, 200, ('T03',),
        join='coalesce-waiter', unchanged=False, effect='device-read',
        checks={'': Ref(first, '/body')})
    yield Case('R004/cache-coalesce-subsequent-explicit-refresh', 'R004', 'GET', path, 200, ('T03', 'R004-S08'),
        unchanged=False, effect='device-read', checks={'cached': False, 'stale': False},
        expected_requests=({'match': arp, 'count': 1},))

def cache_staleness():
    arp = {'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'}, 'count': 1}
    quiet = ({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0})
    initial = 'R004/stale-cache-first'
    yield Case(initial, 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
        ('T03', 'R004-S08'), prepare=(device_world(),), unchanged=False, effect='device-read',
        checks={'cached': False, 'stale': False, 'state': 'ok'}, times=('checked_at',), expected_requests=(arp,))
    yield Case('R004/stale-cache-61s-only-marks-stale', 'R004', 'GET', '/api/client-devices/discovery', 200,
        ('T03', 'R004-S07'),
        cached_times={'checked_at': Ref(initial, '/body/checked_at')},
        age_from=initial, age_seconds=61, age_anchor='ended',
        checks={'cached': True, 'stale': True, 'checked_at': Ref(initial, '/body/checked_at')}, expected_requests=quiet)
    yield Case('R004/stale-cache-true-does-not-refresh', 'R004', 'GET', '/api/client-devices/discovery?refresh=true', 200,
        ('T03', 'R004-S07'),
        cached_times={'checked_at': Ref(initial, '/body/checked_at')},
        checks={'cached': True, 'stale': True,
            'checked_at': Ref(initial, '/body/checked_at')}, expected_requests=quiet)
    yield Case('R004/stale-cache-forced-refresh-clears-stale', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
        ('T03', 'R004-S08'), unchanged=False, effect='device-read',
        checks={'cached': False, 'stale': False}, times=('checked_at',), expected_requests=(arp,))
    yield Case('R004/stale-cache-process-restart-reads-again', 'R004', 'GET', '/api/client-devices/discovery', 200,
        ('T03', 'X14'), prepare=({'rpc': 'restart'},), unchanged=False, effect='device-read',
        checks={'cached': False, 'stale': False}, times=('checked_at',), expected_requests=(arp,))

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
               ('T03', 'R004-S03'), prepare=(device_world(),), unchanged=False, effect='device-read',
               checks={'state': 'ok', 'cached': False}, times=('checked_at',))
    ros_fail = {'action': 'fault', 'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/arp'},
                'status': 500, 'body': {'message': 'synthetic ARP refusal'}}
    ik_fail = {'action': 'fault', 'match': {'peer': 'ikuai', '/body/func_name': 'monitor_lanip'},
               'status': 200, 'body': {'Result': 10000, 'code': 3001, 'message': 'synthetic terminal refusal'}}
    yield Case('R004/failed-refresh-keeps-good', 'R004', 'GET', '/api/client-devices/discovery?refresh=1', 200,
               ('T03', 'R004-S09'),
               cached_times={'checked_at': Ref('R004/outage-prior-good', '/body/checked_at')},
               prepare=(ros_fail, ik_fail), unchanged=False, effect='device-read',
               checks={'state': 'ok', 'cached': True,
                       'checked_at': Ref('R004/outage-prior-good', '/body/checked_at')}, formats={'refresh_error': r'.+'})
    definitions = [
        ('required-arp', (ros_fail,), {'state': 'partial', 'sources/0/ok': False, 'sources/1/ok': True,
                                      'devices/0/presence': 'unknown'}, ('T03', 'R004-S05', 'R004-F11')),
        ('required-terminal', (ik_fail,), {'state': 'partial', 'sources/0/ok': True, 'sources/1/ok': False}, ('T03', 'R004-S05')),
        ('both-required', (ros_fail, ik_fail), {'state': 'failed', 'sources/0/ok': False, 'sources/1/ok': False}, ('T03', 'R004-S06')),
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


def fixed_stale():
    for label in ('tamper', 'cross-profile', 'connection', 'scope', 'source', 'unsaved'):
        world = device_world()
        ip = '10.44.0.24' if label == 'unsaved' else '10.44.0.21'
        if label == 'unsaved':
            world['ros']['/ip/arp'].append({'address': ip, 'mac-address': '02:00:00:00:00:21',
                                          'interface': 'bridge-lan', 'status': 'reachable'})
            world['ros']['/ip/dhcp-server/lease'] = [world['ros']['/ip/dhcp-server/lease'][1]]
        preview = 'R006/stale-' + label
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
                   ('R006-B90',), document=request('add', ip, '02:00:00:00:00:21', 'r-ros', '未必已保存🧪'),
                   prepare=(world,), unchanged=False, effect='device-read', checks={'success': True})
        prepare, profile = (), 'alpha'
        token = Ref(preview, '/body/token')
        if label == 'tamper':
            token = Ref(preview, '/body/token', prefix='x')
            message, entries = '固定 IP 预览无效或已过期，请重新预览确认。', ('R005-F13', 'H0045', 'H0046')
        elif label == 'cross-profile':
            profile = 'beta'
            message, entries = '配置空间已切换，请重新预览。', ('R005-F13', 'R005-B02', 'H0047')
        elif label == 'connection':
            prepare = ({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ros',
                        'document': {'name': '连接记录改变🧪'}, 'expected': 200},)
            message, entries = '路由器或 DHCP 绑定在预览后发生变化，请重新预览确认。', ('R005-F17', 'H0050')
        elif label == 'scope':
            world['ros']['/ip/dhcp-server/network'][0]['gateway'] = '10.44.0.9'
            prepare = (world,)
            message, entries = '路由器或 DHCP 绑定在预览后发生变化，请重新预览确认。', ('R005-F17', 'H0050')
        elif label == 'source':
            world['ros']['/ip/dhcp-server/lease'][0]['mac-address'] = '02:00:00:00:00:99'
            world['ros']['/ip/arp'][0]['mac-address'] = '02:00:00:00:00:99'
            prepare = (world,)
            message, entries = ip + ' 的 MAC 或来源路由器已变化，请重新发现设备。', ('R005-F19', 'H0042')
        else:
            message, entries = '请先保存设备名称或 DNS 策略，再固定 IP。', ('R005-F18', 'H0051')
        yield Case('R005/stale-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply?profile=' + profile,
                   409, entries, document={'profile_id': profile, 'token': token}, requires=('R006',),
                   profile=profile,
                   prepare=prepare, unchanged=False, effect='device-read', message=message, foreign=FOREIGN)


def fixed_uncertain():
    make_static = {'peer': 'ros', 'method': 'POST', 'path': '/rest/ip/dhcp-server/lease/make-static'}
    patch = {'peer': 'ros', 'method': 'PATCH', 'path': '/rest/ip/dhcp-server/lease/*B0'}
    put = {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/dhcp-server/lease'}
    remove = {'peer': 'ros', 'method': 'DELETE', 'path': '/rest/ip/dhcp-server/lease/*B0'}
    ik_add = {'peer': 'ikuai', '/body/func_name': 'dhcp_static', '/body/action': 'add'}
    ik_del = {'peer': 'ikuai', '/body/func_name': 'dhcp_static', '/body/action': 'del'}
    for label, match, rows_left, order, ip, mac, router_id in [
        ('make-static-lost', make_static, 1, (make_static, remove), '10.44.0.21', '02:00:00:00:00:21', 'r-ros'),
        ('patch-lost', patch, 1, (make_static, patch, remove), '10.44.0.21', '02:00:00:00:00:21', 'r-ros'),
        ('put-lost', put, 2, (put, {'peer': 'ros', 'method': 'DELETE'}), '10.44.0.22', '02:00:00:00:00:22', 'r-ros'),
        ('ik-add-lost', ik_add, 2, (ik_add, ik_del), '10.44.0.101', '02:00:00:00:00:65', 'r-ik')]:
        preview = 'R006/uncertain-' + label
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('T07',),
                   document=request('add', ip, mac, router_id, '不确定写入🧪'),
                   prepare=(device_world(),), unchanged=False, effect='device-read')
        yield Case('R005/uncertain-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 502,
                   ('R005-F20', 'T07', 'D26' if router_id == 'r-ik' else 'D17'),
                   document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
                   prepare=({'action': 'fault', 'match': match, 'apply': True, 'disconnect': True},),
                   unchanged=False, effect='device-only', foreign=FOREIGN,
                   formats={'message': r'.+'}, request_order=order,
                   fake_lengths={'ros/~1ip~1dhcp-server~1lease': rows_left, 'ikuai/dhcp_static': 1},
                   stored={'client_fixed_ips': {}})


def fixed_batch_rollback():
    body = request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '第一项🧪')
    body['devices'].extend([
        request('add', '10.44.0.101', '02:00:00:00:00:65', 'r-ik', '第二项🧪')['devices'][0],
        request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '第三项🧪')['devices'][0]])
    preview = 'R006/mixed-batch-preview'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
               ('R006-S05',), document=body, prepare=(device_world(),), unchanged=False, effect='device-read',
               checks={'actions/0/ip': '10.44.0.21', 'actions/1/ip': '10.44.0.101', 'actions/2/ip': '10.44.0.22'})
    make_static = {'peer': 'ros', 'method': 'POST', 'path': '/rest/ip/dhcp-server/lease/make-static'}
    ik_add = {'peer': 'ikuai', '/body/func_name': 'dhcp_static', '/body/action': 'add'}
    ik_del = {'peer': 'ikuai', '/body/func_name': 'dhcp_static', '/body/action': 'del'}
    put = {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/dhcp-server/lease'}
    delete = {'peer': 'ros', 'method': 'DELETE', 'path': '/rest/ip/dhcp-server/lease/*B0'}
    yield Case('R005/mixed-batch-third-refused', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 502,
               ('R005-F20', 'T06', 'H0053', 'H0059'),
               document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
               prepare=({'action': 'fault', 'match': put, 'status': 400,
                         'body': {'message': 'synthetic third lease refused'}},),
               unchanged=False, effect='device-only', foreign=FOREIGN,
               formats={'message': r'RouterOS PUT .+synthetic third lease refused.+客户端续租恢复动态 DHCP，IP 可能变化。'},
               request_order=(make_static, ik_add, put, ik_del, delete),
               expected_requests=tuple({'match': match, 'count': 1} for match in (make_static, ik_add, put, ik_del, delete)),
               fake_lengths={'ros/~1ip~1dhcp-server~1lease': 1, 'ikuai/dhcp_static': 1}, stored={'client_fixed_ips': {}})


def batch_options():
    value = options()
    value['alpha']['device_notes'] = {row['ip']: row['name'] for row in batch_request()['devices']}
    return value


def batch_request():
    rows = []
    for index in range(1, 33):
        for vendor, subnet, router_id in [('ros', 1, 'r-ros'), ('ik', 2, 'r-ik')]:
            rows.append({'ip': f'10.44.{subnet}.{index}', 'mac': f'02:AA:00:00:{subnet:02X}:{index:02X}',
                         'router_id': router_id, 'name': '批次🧪' + vendor + str(index)})
    return {'profile_id': 'alpha', 'operation': 'add', 'devices': rows}


def batch64():
    body = batch_request()
    world = device_world()
    world['ros']['/ip/address'] = [{'.id': '*A0', 'address': '10.44.1.254/24', 'interface': 'bridge-lan'}]
    world['ros']['/ip/dhcp-server/network'] = [{'.id': '*A2', 'address': '10.44.1.0/24', 'gateway': '10.44.1.254'}]
    world['ros']['/ip/dhcp-server/lease'] = [world['ros']['/ip/dhcp-server/lease'][1]]
    world['ros']['/ip/arp'] = [{'address': row['ip'], 'mac-address': row['mac'], 'interface': 'bridge-lan',
                               'status': 'reachable'} for row in body['devices'] if row['router_id'] == 'r-ros']
    world['ikuai']['dhcp_server'][0].update(addr_pool='10.44.2.100-10.44.2.200', gateway='10.44.2.254')
    world['ikuai']['dhcp_lease'] = []
    world['ikuai']['monitor_lanip'] = [{'ip_addr': row['ip'], 'mac': row['mac'], 'interface': 'lan1'}
                                     for row in body['devices'] if row['router_id'] == 'r-ik']
    checks = {'success': True, **{'actions/' + str(index) + '/ip': row['ip']
                                 for index, row in enumerate(body['devices'])}}
    for operation in ('add', 'delete'):
        preview = 'R006/mixed-64-' + operation
        yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
                   ('R006-S05', 'R006-B04', 'T05', 'H0036', 'H0037'), document={**body, 'operation': operation},
                   prepare=(world,) if operation == 'add' else (), unchanged=False, effect='device-read',
                   checks=checks, formats={'token': r'.{1,1000000}'}, foreign=FOREIGN)
        yield Case('R005/mixed-64-' + operation, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
                   ('R005-S06', 'R005-B01', 'T05'),
                   document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
                   unchanged=False, effect='profile-write-device', foreign=FOREIGN,
                   checks={'success': True, **{'devices/' + str(index) + '/name': row['name']
                           for index, row in enumerate(body['devices'])}},
                   fake_lengths={'ros/~1ip~1dhcp-server~1lease': 33 if operation == 'add' else 1,
                                 'ikuai/dhcp_static': 33 if operation == 'add' else 1},
                   stored={('client_fixed_ips/' + row['ip'] + '/mac'): row['mac'] for row in body['devices']}
                          if operation == 'add' else {'client_fixed_ips': {}},
                   stored_absent=() if operation == 'add' else tuple('client_fixed_ips/' + row['ip'] for row in body['devices']))


def random_key_options():
    return {**options(), 'environment': {'JWT_SECRET_KEY': None}}


def _restart(stable):
    label = 'stable-key' if stable else 'random-key'
    preview = 'R006/restart-' + label
    return [Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('T05',),
                 document=request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '重启令牌🧪'),
                 prepare=(device_world(),), unchanged=False, effect='device-read', checks={'success': True}),
            Case('R005/restart-' + label, 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200 if stable else 409,
                 ('R005-B92',) if not stable else ('T05',),
                 document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
                 prepare=({'rpc': 'restart'},), unchanged=False,
                 effect='profile-write-device' if stable else 'device-read', foreign=FOREIGN,
                 message=None if stable else '固定 IP 预览无效或已过期，请重新预览确认。',
                 stored={'client_fixed_ips/10.44.0.22/mac': '02:00:00:00:00:22'} if stable else {'client_fixed_ips': {}},
                 fake_lengths={'ros/~1ip~1dhcp-server~1lease': 3 if stable else 2, 'ikuai/dhcp_static': 1})]


def fixed_restart_stable():
    yield from _restart(True)


def fixed_restart_random():
    yield from _restart(False)


def fixed_expiry():
    preview = 'R006/real-time-expiry-preview'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('T05',),
               document=request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '真实时间临界🧪'),
               prepare=(device_world(),), unchanged=False, effect='device-read', checks={'success': True})
    yield Case('R005/token-near-600-seconds', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
               ('R005-B07', 'T05'), document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')},
               requires=('R006',), age_from=preview, age_seconds=598, age_limit=599.5, unchanged=False, effect='profile-write-device', foreign=FOREIGN,
               checks={'success': True}, stored={'client_fixed_ips/10.44.0.22/mac': '02:00:00:00:00:22'},
               fake_lengths={'ros/~1ip~1dhcp-server~1lease': 3, 'ikuai/dhcp_static': 1})
    yield Case('R005/token-past-600-seconds', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 409,
               ('R005-B07', 'R005-F13', 'H0045', 'H0046', 'T05'),
               document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
               age_from=preview, age_seconds=604, age_anchor='ended', message='固定 IP 预览无效或已过期，请重新预览确认。',
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))


def binding_limit_options():
    value = options()
    bindings = {}
    for index in range(1, 2049):
        ip = '10.45.' + str(index // 256) + '.' + str(index % 256)
        bindings[ip] = {'router_id': 'r-ros', 'vendor': 'routeros', 'id': '*' + format(index + 1, 'X'),
                        'ip': ip, 'mac': f'02:BB:00:00:{index // 256:02X}:{index % 256:02X}',
                        'comment': 'cf-client:合成已有记录🧪', 'server': 'lan'}
    value['alpha']['client_fixed_ips'] = bindings
    return value


def binding_limit():
    preview = 'R006/binding-cap-preview'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('T06',),
               document=request('add', '10.44.0.22', '02:00:00:00:00:22', 'r-ros', '第2049条🧪'),
               prepare=(device_world(),), unchanged=False, effect='device-read')
    yield Case('R005/binding-cap-transaction-rejected', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 502,
               ('H0032', 'R005-F22', 'T06'),
               document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
               unchanged=False, effect='device-only', foreign=FOREIGN,
               message='固定 IP 记录保存失败，已撤销本次路由器变更，请重试。',
               fake_lengths={'ros/~1ip~1dhcp-server~1lease': 2},
               request_order=({'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/dhcp-server/lease'},
                              {'peer': 'ros', 'method': 'DELETE'}))


def long_binding_id():
    seed = device_world()
    seed['ros']['/ip/dhcp-server/lease'][0]['.id'] = '*' + 'A' * 513
    preview = 'R006/long-device-id-preview'
    yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200, ('D17',),
               document=request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '长设备标识🧪'),
               prepare=(seed,), unchanged=False, effect='device-read')
    yield Case('R005/long-binding-id-transaction-rejected', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 502,
               ('H0033', 'R005-F22', 'T06'),
               document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
               unchanged=False, effect='device-only', foreign=FOREIGN,
               message='固定 IP 记录保存失败，已撤销本次路由器变更，请重试。；RouterOS 已移除本次静态预留，客户端续租恢复动态 DHCP，IP 可能变化。',
               stored={'client_fixed_ips': {}}, fake_lengths={'ros/~1ip~1dhcp-server~1lease': 1},
               request_order=({'peer': 'ros', 'method': 'POST', 'path': '/rest/ip/dhcp-server/lease/make-static'},
                              {'peer': 'ros', 'method': 'PATCH'}, {'peer': 'ros', 'method': 'DELETE'}))
