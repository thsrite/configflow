"""Fixed-IP router/source/binding guards through real HTTP and device state."""
import copy

from cases import Case, Ref
from clients import device_world, options as client_options, request, FOREIGN


QUIET = ({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0})
MISSING_ROUTER = '发现这台设备的路由器已不存在或不支持写入，请刷新设备列表。'
MISSING_BINDING = '没有这台设备由 ConfigFlow 创建的固定 IP 记录，不会删除其他静态租约。'


def router_worlds():
    variants = (('unknown', 'unknown'), ('case', 'RouterOS'), ('empty', ''), ('null', None),
                ('false', False), ('true', True), ('number', 1), ('array', ['routeros']),
                ('object', {'type': 'routeros'}), ('missing', None))
    for label, value in variants:
        options = client_options()
        if label == 'missing':
            options['alpha']['routers']['items'][0].pop('type')
        else:
            options['alpha']['routers']['items'][0]['type'] = value
        # The repository deliberately permits legacy router objects. The
        # public preview must diagnose them before selecting any driver.
        def refused(label=label):
            for operation in ('add', 'delete'):
                yield Case('R006/source-router-' + label + '-' + operation, 'R006', 'POST',
                    '/api/client-devices/fixed-ip/preview', 409, ('H0040', 'R006-F18'),
                    document=request(operation, '10.44.0.21', '02:00:00:00:00:21', 'r-ros', ''),
                    message=MISSING_ROUTER, expected_requests=QUIET)
        yield 'fixed-source-router-' + label, options, (refused,)
    yield 'fixed-source-missing-router', client_options(), (missing_router,)
    yield 'fixed-source-unseen', client_options(), (unseen,)


def missing_router():
    for ident in ('missing', 'r-ROS', ' r-ros '):
        for operation in ('add', 'delete'):
            yield Case('R006/source-missing-' + ident + '-' + operation, 'R006', 'POST',
                '/api/client-devices/fixed-ip/preview', 409, ('H0040', 'R006-F18'),
                document=request(operation, '10.44.0.21', '02:00:00:00:00:21', ident, ''),
                message=MISSING_ROUTER, expected_requests=QUIET)


def unseen():
    for vendor, ident, known_ip, unknown_ip, known_mac in (
            ('routeros', 'r-ros', '10.44.0.21', '10.44.0.29', '02:00:00:00:00:21'),
            ('ikuai', 'r-ik', '10.44.0.101', '10.44.0.102', '02:00:00:00:00:65')):
        for label in ('mac', 'ip', 'empty'):
            ip = unknown_ip if label == 'ip' else known_ip
            mac = '02:00:00:00:00:99' if label == 'mac' else known_mac
            state = device_world()
            if label == 'empty':
                if vendor == 'routeros':
                    state['ros']['/ip/dhcp-server/lease'] = []
                    state['ros']['/ip/arp'] = []
                else:
                    state['ikuai']['dhcp_lease'] = []
                    state['ikuai']['monitor_lanip'] = []
            yield Case('R006/source-unseen-' + vendor + '-' + label, 'R006', 'POST',
                '/api/client-devices/fixed-ip/preview', 409, ('H0042', 'R006-F19'),
                document=request('add', ip, mac, ident, ''), prepare=(state,),
                unchanged=False, effect='device-read',
                message=ip + ' 的 MAC 或来源路由器已变化，请重新发现设备。')
        yield Case('R006/source-seen-' + vendor, 'R006', 'POST',
            '/api/client-devices/fixed-ip/preview', 200, ('H0042',),
            document=request('add', known_ip, known_mac, ident, ''), prepare=(device_world(),),
            unchanged=False, effect='device-read', foreign=FOREIGN,
            checks={'success': True, 'actions/0/ip': known_ip, 'actions/0/router_id': ident})


def binding_worlds():
    for vendor, ident, other, ip, mac in (('routeros', 'r-ros', 'r-ik', '10.44.0.21', '02:00:00:00:00:21'),
                                         ('ikuai', 'r-ik', 'r-ros', '10.44.0.101', '02:00:00:00:00:65')):
        for field, value in (('router_id', other), ('mac', '02:00:00:00:00:99')):
            options = client_options()
            binding = {'router_id': ident, 'vendor': vendor, 'id': '*B0' if vendor == 'routeros' else '71',
                       'ip': ip, 'mac': mac, 'comment': 'cf-client:合成原有记录🧪',
                       **({'server': 'lan'} if vendor == 'routeros' else {'interface': 'lan1', 'gateway': '10.44.0.1'})}
            binding[field] = value
            options['alpha']['client_fixed_ips'] = {ip: binding}
            def rejected(vendor=vendor, ident=ident, ip=ip, mac=mac, field=field):
                yield Case('R006/source-binding-' + vendor + '-' + field, 'R006', 'POST',
                    '/api/client-devices/fixed-ip/preview', 409, ('H0043', 'R006-F22'),
                    document=request('delete', ip, mac, ident, ''), message=MISSING_BINDING,
                    expected_requests=QUIET)
            yield 'fixed-source-binding-' + vendor + '-' + field, options, (rejected,)
    for vendor in ('routeros', 'ikuai'):
        def recovered(vendor=vendor):
            ip, mac, ident = ('10.44.0.21', '02:00:00:00:00:21', 'r-ros') if vendor == 'routeros' else (
                '10.44.0.101', '02:00:00:00:00:65', 'r-ik')
            state = device_world()
            if vendor == 'routeros':
                state['ros']['/ip/dhcp-server/lease'][0].update(dynamic='false', comment='cf-client:原有预留🧪')
            else:
                original = state['ikuai']['dhcp_static'][0]
                state['ikuai']['dhcp_static'].append({**copy.deepcopy(original), 'id': 71,
                    'ip_addr': ip, 'mac': mac, 'comment': 'cf-client:原有预留🧪', 'tagname': 'cfip_saved1'})
            preview = 'R006/source-recover-owned-' + vendor
            yield Case(preview, 'R006', 'POST', '/api/client-devices/fixed-ip/preview', 200,
                ('H0043', 'R005-S07', 'R006-S04'), document=request('delete', ip, mac, ident, ''),
                prepare=(state,), unchanged=False, effect='device-read', foreign=FOREIGN,
                checks={'success': True, 'actions/0/ip': ip, 'actions/0/router_id': ident},
                stored={'client_fixed_ips': {}})
            yield Case('R005/source-recover-owned-' + vendor, 'R005', 'POST',
                '/api/client-devices/fixed-ip/apply', 200, ('H0043', 'R005-S07'),
                document={'profile_id': 'alpha', 'token': Ref(preview, '/body/token')}, requires=('R006',),
                unchanged=False, effect='profile-write-device', foreign=FOREIGN,
                checks={'success': True}, stored={'client_fixed_ips': {}},
                retained_fields=('device_notes', 'oxidns', 'routers'),
                fake_lengths={'ros/~1ip~1dhcp-server~1lease': 1 if vendor == 'routeros' else 2,
                              'ikuai/dhcp_static': 1})
        yield 'fixed-source-recover-' + vendor, client_options(), (recovered,)
