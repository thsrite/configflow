"""Real preview boundary for fixed-IP canonicalization and compound guards.

These are literal request vectors, not a copy of the reference normalizers.
Positive previews read the same independently seeded DHCP/ARP state; they do not
write a reservation or infer that a client can reach a real device.
"""
from cases import Case
from clients import device_world, request, FOREIGN


QUIET = ({'match': {'peer': 'ros'}, 'count': 0},
         {'match': {'peer': 'ikuai'}, 'count': 0})


def rejected():
    values = (
        ('ip', (('null', None), ('false', False), ('true', True), ('zero', 0), ('integer', 1),
                ('fraction', 1.5), ('array', ['10.44.0.21']), ('object', {'ip': '10.44.0.21'}),
                ('empty', ''), ('blank', '  '), ('garbage', 'bad'), ('ipv6', 'fd42:cf::21'),
                ('mapped-v6', '::ffff:10.44.0.21'), ('cidr', '10.44.0.21/32'),
                ('leading-zero', '010.44.0.21'), ('unicode-digit', '１０.４４.０.２１'),
                ('octet-overflow', '10.44.0.256'), ('extra-octet', '10.44.0.0.21'),
                ('sign', '+10.44.0.21'), ('zone', '10.44.0.21%lo'), ('newline-inside', '10.44.\n0.21'))),
        ('mac', (('null', None), ('false', False), ('true', True), ('zero', 0), ('integer', 1),
                 ('fraction', 1.5), ('array', ['02:00:00:00:00:21']), ('object', {'mac': '02:00:00:00:00:21'}),
                 ('empty', ''), ('blank', '  '), ('five', '02:00:00:00:21'),
                 ('seven', '02:00:00:00:00:00:21'), ('dot', '0200.0000.0021'),
                 ('no-separator', '020000000021'), ('long-octet', '002:00:00:00:00:21'),
                 ('empty-octet', '02::00:00:00:21'), ('non-hex', '02:00:00:00:00:GG'),
                 ('bad-hex-prefix', '02:00:00:00:00:0x'))),
        ('router_id', (('null', None), ('false', False), ('true', True), ('zero', 0), ('integer', 1),
                       ('fraction', 1.5), ('array', ['r-ros']), ('object', {'id': 'r-ros'}), ('empty', ''))),
    )
    for field, variants in values:
        for label, value in variants:
            body = request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '设备🧪')
            body['devices'][0][field] = value
            entries = ['H0038', 'R006-F14']
            if field == 'ip':
                entries.append('H0592')
            if field == 'mac' and label in ('non-hex', 'bad-hex-prefix'):
                entries.append('H0593')
            yield Case('R006/normalize-' + field + '-' + label, 'R006', 'POST',
                '/api/client-devices/fixed-ip/preview', 400, tuple(entries), document=body,
                message='固定 IP 只支持有 MAC 和明确来源路由器的单个 IPv4，且不能重复选择。',
                expected_requests=QUIET)
    name_values = [('null', None), ('false', False), ('true', True), ('zero', 0), ('fraction', 1.5),
                   ('array', ['name']), ('object', {'name': 'value'}), ('ascii81', 'x' * 81),
                   ('unicode81', '设备🧪' * 27), ('blank81', ' ' * 81)]
    name_values += [('control' + str(code), '保留名称🧪' + chr(code)) for code in range(32)]
    name_values += [('del', '保留名称🧪\x7f')]
    for label, value in name_values:
        body = request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', value)
        yield Case('R006/normalize-name-' + label, 'R006', 'POST', '/api/client-devices/fixed-ip/preview',
            400, ('H0068',), document=body,
            message='设备备注最多 80 个字符，不能包含换行或控制字符。', expected_requests=QUIET)
    row = request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '')['devices'][0]
    duplicates = (
        ('ip-same-router', row), ('ip-foreign-router', {**row, 'router_id': 'r-ik'}),
        ('ip-trimmed', {**row, 'ip': ' \t10.44.0.21\n', 'mac': '02:00:00:00:00:22'}),
        ('mac-hyphen', {**row, 'ip': '10.44.0.22', 'mac': '02-00-00-00-00-21'}),
        ('mac-short-octets', {**row, 'ip': '10.44.0.22', 'mac': '2:0:0:0:0:21'}),
        ('mac-trimmed', {**row, 'ip': '10.44.0.22', 'mac': ' \t02:00:00:00:00:21\n'}),
    )
    for label, second in duplicates:
        is_ip = label.startswith('ip-')
        yield Case('R006/normalize-duplicate-' + label, 'R006', 'POST',
            '/api/client-devices/fixed-ip/preview', 400,
            ('H0038', 'R006-F14', 'R006-B02') if is_ip else ('H0039', 'R006-F15'),
            document={'profile_id': 'alpha', 'operation': 'add', 'devices': [row, second]},
            message=('固定 IP 只支持有 MAC 和明确来源路由器的单个 IPv4，且不能重复选择。' if is_ip
                     else '同一路由器上的同一 MAC 只能选择一个固定 IP。'), expected_requests=QUIET)


def accepted():
    variants = (
        ('canonical', '10.44.0.21', '02:00:00:00:00:21', '设备🧪'),
        ('hyphen', '10.44.0.21', '02-00-00-00-00-21', '设备🧪'),
        ('short-octets', '10.44.0.21', '2:0:0:0:0:21', '设备🧪'),
        ('mixed-separators', '10.44.0.21', '02-00:00-00:00-21', '设备🧪'),
        ('outer-whitespace', ' \t10.44.0.21\n', ' \t02:00:00:00:00:21\n', ' 设备🧪 '),
        ('empty-name', '10.44.0.21', '02:00:00:00:00:21', ''),
        ('blank-name', '10.44.0.21', '02:00:00:00:00:21', ' \u3000 '),
        ('ascii80', '10.44.0.21', '02:00:00:00:00:21', 'x' * 80),
        ('unicode80', '10.44.0.21', '02:00:00:00:00:21', '设备🧪' * 26 + '边界'),
    )
    for index, (label, ip, mac, name) in enumerate(variants):
        yield Case('R006/normalize-accepted-' + label, 'R006', 'POST',
            '/api/client-devices/fixed-ip/preview', 200,
            ('H0038', 'H0039', 'R006-B01', 'R006-B03'),
            document=request('add', ip, mac, 'r-ros', name), prepare=(device_world(),) if index == 0 else (),
            unchanged=False, effect='device-read', foreign=FOREIGN,
            checks={'success': True, 'actions/0/ip': '10.44.0.21', 'actions/0/router_id': 'r-ros'},
            formats={'token': r'.{1,1000000}'})
    for label, mac in (('lowercase', '02:00:00:00:00:af'), ('mixed-case', '02:00:00:00:00:aF')):
        state = device_world()
        state['ros']['/ip/arp'][0]['mac-address'] = '02:00:00:00:00:AF'
        state['ros']['/ip/dhcp-server/lease'][0]['mac-address'] = '02:00:00:00:00:AF'
        yield Case('R006/normalize-accepted-' + label, 'R006', 'POST',
            '/api/client-devices/fixed-ip/preview', 200, ('H0038', 'H0039', 'R006-B01'),
            document=request('add', '10.44.0.21', mac, 'r-ros', ''), prepare=(state,),
            unchanged=False, effect='device-read', foreign=FOREIGN,
            checks={'success': True, 'actions/0/ip': '10.44.0.21', 'actions/0/router_id': 'r-ros'})
    state = device_world()
    for table in ('dhcp_lease', 'monitor_lanip'):
        state['ikuai'][table][0]['mac'] = '02:00:00:00:00:21'
    yield Case('R006/normalize-accepted-same-MAC-different-router', 'R006', 'POST',
        '/api/client-devices/fixed-ip/preview', 200, ('H0038', 'H0039'),
        prepare=(state,), document={'profile_id': 'alpha', 'operation': 'add', 'devices': [
            request('add', '10.44.0.101', '02:00:00:00:00:21', 'r-ik', '')['devices'][0],
            request('add', '10.44.0.21', '02:00:00:00:00:21', 'r-ros', '')['devices'][0]]},
        unchanged=False, effect='device-read', foreign=FOREIGN,
        checks={'success': True, 'actions/0/ip': '10.44.0.101', 'actions/0/router_id': 'r-ik',
                'actions/1/ip': '10.44.0.21', 'actions/1/router_id': 'r-ros'})
