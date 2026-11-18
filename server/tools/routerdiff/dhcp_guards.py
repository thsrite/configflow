"""RouterOS DHCP DNS evidence through reports, plans and blocked applies.

All vectors are synthetic device replies. No application helper is invoked or
patched: the actual public server reads every table over loopback HTTP.
"""
import copy

from cases import Case
import dns

NETWORK = '/ip/dhcp-server/network'
SERVER = '/ip/dhcp-server'
LEASE = '/ip/dhcp-server/lease'
OPTION = '/ip/dhcp-server/option'
SETS = '/ip/dhcp-server/option/sets'
MATCHER = '/ip/dhcp-server/matcher'
READ_ERROR = '无法读取 DHCP DNS 配置：'
NO_SERVER = '未发现启用的 DHCP 服务，无法确认网段 DNS 会被下发'
BAD_NAMES = 'RouterOS DHCP 选项或 DNS 列表格式无法识别'


def base():
    result = dns.world()
    result['ros'].update({OPTION: [], SETS: [], MATCHER: []})
    return result


def vector(label, entries, *, patches=None, tables=None, message='', error=False):
    seed = base()
    seed['ros'].update(copy.deepcopy(tables or {}))
    for path, patch in (patches or {}).items():
        seed['ros'][path][0].update(copy.deepcopy(patch))
    return label, tuple(entries), seed, (READ_ERROR if error else '') + message


def vectors():
    yield vector('healthy', ('H0849', 'H0850', 'H0851', 'H0852', 'H0853', 'H0854', 'H0855', 'H0856',
                             'H0857', 'H0858', 'H0859', 'H0860', 'H0861', 'H0862', 'H0863'))
    for label, rows in (
        ('missing', []),
        ('disabled-bool', [{'.id': '*A1', 'name': 'lan', 'disabled': True}]),
        ('disabled-upper', [{'.id': '*A1', 'name': 'lan', 'disabled': 'YES'}]),
    ):
        yield vector('servers-' + label, ('H0849',), tables={SERVER: rows}, message=NO_SERVER)
    for label, row, shown in (
        ('missing-name', {'.id': '*A1'}, '*A1'),
        ('empty-name', {'.id': '*A1', 'name': ''}, '*A1'),
        ('null-name', {'.id': '*A1', 'name': None}, '*A1'),
        ('no-name-or-id', {}, '未知'),
        ('invalid-bool', {'.id': '*A1', 'name': 'lan', 'invalid': True}, 'lan'),
        ('invalid-text', {'.id': '*A1', 'name': 'lan', 'invalid': 'YES'}, 'lan'),
    ):
        yield vector('server-' + label, ('H0850',), tables={SERVER: [row]},
                     message='DHCP 服务 ' + shown + ' 信息不完整或无效，无法确认 DNS 下发')
    for index, value in enumerate(('yes', 'unknown', True, 1, None)):
        yield vector('radius-' + str(index), ('H0851',), patches={SERVER: {'use-radius': value}},
                     message='DHCP 服务 lan 启用了 RADIUS 或其状态未知，可能覆盖 DHCP DNS')
    for label, value in (('no', 'no'), ('false', 'false'), ('accounting', 'accounting'), ('upper', 'ACCOUNTING')):
        yield vector('radius-accepted-' + label, ('H0851',), patches={SERVER: {'use-radius': value}})
    for label, patch in (('empty', {'.id': ''}), ('null', {'.id': None}), ('zero', {'.id': 0})):
        yield vector('network-id-' + label, ('H0854',), patches={NETWORK: patch},
                     message='RouterOS DHCP 网段缺少唯一对象 ID', error=True)
    missing_id = base()['ros'][NETWORK]
    missing_id[0].pop('.id')
    yield vector('network-id-missing', ('H0854',), tables={NETWORK: missing_id},
                 message='RouterOS DHCP 网段缺少唯一对象 ID', error=True)
    duplicate = copy.deepcopy(base()['ros'][NETWORK][0])
    yield vector('network-id-duplicate', ('H0854',), tables={NETWORK: [duplicate, copy.deepcopy(duplicate)]},
                 message='RouterOS DHCP 网段缺少唯一对象 ID', error=True)
    for index, value in enumerate(('', None, 'bad', 'fd42:cf::/64', 24, [], {})):
        yield vector('network-address-' + str(index), ('H0855', 'H0856'), patches={NETWORK: {'address': value}},
                     message='RouterOS DHCP 网段地址无法识别', error=True)
    for label, value in (('host-bits', '10.44.0.13/24'), ('netmask', '10.44.0.0/255.255.255.0')):
        yield vector('network-address-accepted-' + label, ('H0855', 'H0856'), patches={NETWORK: {'address': value}})
    yield vector('network-flags-false', ('H0857',), patches={NETWORK: {'dynamic': False, 'invalid': 'no'}})
    for field in ('dynamic', 'invalid'):
        for label, value in (('bool', True), ('text', 'YES')):
            yield vector('network-' + field + '-' + label, ('H0857',), patches={NETWORK: {field: value}},
                         message='该 DHCP 网段为动态或无效对象，不能自动修改 DNS')
    for index, value in enumerate((None, 0, 1, 'False', 'unknown', [], {})):
        yield vector('dns-none-' + str(index), ('H0905',), patches={NETWORK: {'dns-none': value}},
                     message='RouterOS DHCP dns-none 状态无法识别', error=True)
    for label, value in (('bool-true', True), ('bool-false', False), ('true', 'true'), ('false', 'false'), ('yes', 'yes'), ('no', 'no')):
        yield vector('dns-none-accepted-' + label, ('H0905',), patches={NETWORK: {'dns-none': value}})
    for index, value in enumerate(('not-an-IP', 'fd42:cf::53', '10.44.0.53,bad', ['10.44.0.53', 'bad'])):
        yield vector('dns-invalid-' + str(index), ('H0862', 'H0863'), patches={NETWORK: {'dns-server': value}},
                     message='RouterOS DHCP 网段 10.44.0.0/24 的 DNS 地址无法识别', error=True)
    for label, value in (('list', ['10.44.0.54']), ('csv', '10.44.0.54,10.44.0.55'),
                         ('filtered-list', ['', 'none', ' 10.44.0.54 ', ''])):
        yield vector('dns-accepted-' + label, ('H0862', 'H0863'), patches={NETWORK: {'dns-server': value}})
    # Each real invocation position of _dhcp_names, including nested set options.
    positions = ((SERVER, 'dhcp-option'), (SERVER, 'dhcp-option-set'),
                 (NETWORK, 'dhcp-option'), (NETWORK, 'dhcp-option-set'),
                 (LEASE, 'dhcp-option'), (LEASE, 'dhcp-option-set'),
                 (MATCHER, 'option-set'), (SETS, 'options'), (NETWORK, 'dns-server'))
    for position, (path, member) in enumerate(positions):
        for kind, value in (('null', None), ('bool', True), ('number', 6), ('object', {}), ('mixed-list', ['valid', None])):
            seed = base()
            seed['ros'][LEASE] = [{'.id': '*L1', 'address': '10.44.0.21', 'server': 'lan'}]
            seed['ros'][MATCHER] = [{'.id': '*M1', 'name': 'match', 'server': 'lan'}]
            seed['ros'][SETS] = [{'.id': '*S1', 'name': 'set', 'options': ''}]
            if path == SETS:
                seed['ros'][SERVER][0]['dhcp-option-set'] = 'set'
            seed['ros'][path][0][member] = copy.deepcopy(value)
            yield ('names-' + str(position) + '-' + kind, ('H0904',), seed, READ_ERROR + BAD_NAMES)
    for position, (path, member) in enumerate(positions):
        for kind, value in (('empty-list', []), ('ignored-names', ['none', '', ' '])):
            seed = base()
            seed['ros'][LEASE] = [{'.id': '*L1', 'address': '10.44.0.21', 'server': 'lan'}]
            seed['ros'][MATCHER] = [{'.id': '*M1', 'name': 'match', 'server': 'lan'}]
            seed['ros'][SETS] = [{'.id': '*S1', 'name': 'set', 'options': ''}]
            if path == SETS:
                seed['ros'][SERVER][0]['dhcp-option-set'] = 'set'
            seed['ros'][path][0][member] = value
            yield ('names-accepted-' + str(position) + '-' + kind, ('H0904',), seed, '')
    # Option ownership/reference resolution at all four supported layers.
    layers = ((SERVER, 'DHCP 服务 lan', 'H0852'),
              (NETWORK, 'DHCP 网段 10.44.0.0/24', 'H0858'),
              (LEASE, 'DHCP 租约 10.44.0.21', 'H0861'),
              (MATCHER, 'DHCP 匹配器 match', 'H0853'))
    for number, (path, label, helper) in enumerate(layers):
        seed = base()
        seed['ros'][LEASE] = [{'.id': '*L1', 'address': '10.44.0.21', 'server': 'lan'}]
        seed['ros'][MATCHER] = [{'.id': '*M1', 'name': 'match', 'server': 'lan'}]
        if path == MATCHER:
            seed['ros'][path][0]['option-set'] = 'set'
            seed['ros'][SETS] = [{'.id': '*S1', 'name': 'set', 'options': 'dns'}]
        else:
            seed['ros'][path][0]['dhcp-option'] = 'dns,dns'
        seed['ros'][OPTION] = [{'.id': '*O1', 'name': 'dns', 'code': 6, 'value': 'synthetic'}]
        yield ('option6-layer-' + str(number), (helper, 'H0909'), seed,
               label + ' 使用自定义 DNS 选项 6（dns），不能只修改网段 DNS')
    for label, rows in (
        ('missing', []), ('duplicate', [{'name': 'set', 'options': ''}, {'.id': 'set', 'options': ''}]),
        ('no-options', [{'name': 'set'}]),
    ):
        yield vector('set-' + label, ('H0906',), patches={SERVER: {'dhcp-option-set': 'set'}},
                     tables={SETS: rows}, message='DHCP 服务 lan 的选项集 set 无法完整读取，可能覆盖 DHCP DNS')
    for label, rows in (('missing', []), ('duplicate', [{'name': 'opt', 'code': 15}, {'.id': 'opt', 'code': 15}])):
        yield vector('option-' + label, ('H0907',), patches={SERVER: {'dhcp-option': 'opt'}},
                     tables={OPTION: rows}, message='DHCP 服务 lan 的选项 opt 无法唯一识别，可能覆盖 DHCP DNS')
    for label, code in (('null', None), ('false', False), ('true', True), ('zero', 0), ('negative', -1),
                        ('too-large', 255), ('float', 6.0), ('array', []), ('object', {}),
                        ('whitespace', ' 6'), ('sign', '+6'), ('unknown', 'unknown'), ('oversized', '0' * 5000)):
        yield vector('code-' + label, ('H0910',) + (('H0908',) if label == 'oversized' else ()),
                     patches={SERVER: {'dhcp-option': 'opt'}}, tables={OPTION: [{'name': 'opt', 'code': code}]},
                     message='DHCP 服务 lan 的选项 opt 编号未知，可能覆盖 DHCP DNS')
    for label, code in (('six', '6'), ('padded-six', '006')):
        yield vector('code-' + label, ('H0909',), patches={SERVER: {'dhcp-option': 'opt'}},
                     tables={OPTION: [{'name': 'opt', 'code': code}]},
                     message='DHCP 服务 lan 使用自定义 DNS 选项 6（opt），不能只修改网段 DNS')
    for label, code in (('one', 1), ('maximum', 254), ('number-string', '15'), ('vendor', 'vendor-specific')):
        yield vector('code-accepted-' + label, ('H0906', 'H0907', 'H0908', 'H0909', 'H0910'), patches={SERVER: {'dhcp-option-set': '*S1'}},
                     tables={SETS: [{'.id': '*S1', 'name': 'set', 'options': [' *O1 ', '*O1', 'none', '']}],
                             OPTION: [{'.id': '*O1', 'name': 'opt', 'code': code}]})
    # Unknown lease scopes conservatively affect every network; known outside
    # addresses and disabled leases/servers cannot block this LAN's DNS change.
    radius = 'DHCP 租约 {address} 由 RADIUS 管理，可能覆盖 DHCP DNS'
    for label, value, shown, entry in (
        ('invalid', 'bad', 'bad', 'H0859'), ('null', None, '*L1', 'H0859'),
        ('empty', '', '*L1', 'H0859'), ('ipv6', 'fd42:cf::21', 'fd42:cf::21', 'H0859'),
        ('pool', '10.44.0.20-10.44.0.30', '10.44.0.20-10.44.0.30', 'H0859'),
        ('unspecified', '0.0.0.0', '0.0.0.0', 'H0860'), ('inside', '10.44.0.21', '10.44.0.21', 'H0860'),
    ):
        yield vector('lease-radius-' + label, tuple(dict.fromkeys((entry, 'H0860'))),
                     tables={LEASE: [{'.id': '*L1', 'address': value, 'server': 'lan', 'radius': True}]},
                     message=radius.format(address=shown))
    for label, lease in (
        ('outside', {'.id': '*L1', 'address': '10.55.0.21', 'server': 'lan', 'radius': True}),
        ('disabled', {'.id': '*L1', 'address': '10.44.0.21', 'server': 'lan', 'radius': True, 'disabled': 'yes'}),
        ('disabled-server', {'.id': '*L1', 'address': '10.44.0.21', 'server': 'disabled-lan', 'radius': True}),
        ('ordinary-dynamic', {'.id': '*L1', 'address': '10.44.0.21', 'server': 'lan', 'dynamic': True}),
    ):
        seed = base()
        seed['ros'][LEASE] = [lease]
        seed['ros'][SERVER].append({'.id': '*AD', 'name': 'disabled-lan', 'disabled': True})
        yield ('lease-ignored-' + label, ('H0859', 'H0860', 'H0861'), seed, '')
    for label, matcher in (
        ('disabled', {'name': 'match', 'option-set': 'missing', 'disabled': 'yes'}),
        ('disabled-server', {'name': 'match', 'option-set': 'missing', 'server': 'disabled-lan'}),
    ):
        seed = base()
        seed['ros'][MATCHER] = [matcher]
        seed['ros'][SERVER].append({'.id': '*AD', 'name': 'disabled-lan', 'disabled': True})
        yield ('matcher-ignored-' + label, ('H0853',), seed, '')


def cases():
    for index, (label, entries, seed, message) in enumerate(vectors()):
        root = '/api/routers/r-ros'
        prepare = (seed, dns.online()) if index == 0 else (seed,)
        checks = {'success': True, 'report/dhcp_dns/status': 'unknown' if message else 'pending',
                  'report/dhcp_dns/blockers': [message] if message else []}
        if message:
            checks['report/dhcp_dns/message'] = message
        else:
            checks['report/dhcp_dns/items/0/network'] = '10.44.0.0/24'
        yield Case('R018/dhcp-evidence-' + label, 'R018', 'GET', root + '/report', 200,
                   entries, prepare=prepare, unchanged=False, effect='device-read', checks=checks,
                   times=('generated_at',))
        yield Case('R012/dhcp-evidence-' + label, 'R012', 'POST', root + '/plan', 200,
                   entries, unchanged=False, effect='device-read',
                   checks={'success': True, 'plan/blockers': [message] if message else []})
        if message:
            yield Case('R011/dhcp-evidence-' + label, 'R011', 'POST', root + '/apply', 409,
                       entries, unchanged=False, effect='device-read', message=message,
                       checks={'success': False, 'plan/blockers': [message]})
