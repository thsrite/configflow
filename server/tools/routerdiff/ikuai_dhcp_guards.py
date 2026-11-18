"""Complete iKuai DHCP evidence and per-client DNS overrides over real HTTP."""
import copy

from cases import Case
import dns

FORMAT = 'iKuai DHCP 字段与已验证的 4.x 格式不兼容，无法保留完整配置（缺少：{missing}；未知：{unknown}）'
TYPE = 'iKuai DHCP 字段 {field} 类型无法识别'
ENABLED = 'iKuai DHCP 启用状态无法识别'
STATUS = 'iKuai DHCP 服务未生效或无法确认运行状态'
RELAY = 'iKuai DHCP 仅响应中继请求，无法确认该网段客户端的 DNS 覆盖'
PHYSICAL = 'iKuai DHCP 限制了物理接口，无法确认完整网段的 DNS 覆盖'
POOL = 'iKuai DHCP 地址池、掩码或网关无法确定唯一网段'
DNS = 'iKuai DHCP DNS 包含无法识别的自动或继承配置'
STATIC_IP = 'iKuai 静态 DHCP 绑定缺少有效地址，无法排除 DNS 覆盖'
STATIC_SHAPE = 'iKuai 静态 DHCP 绑定信息不完整，无法排除 DNS 覆盖'
STATIC_DNS = '该网段存在静态 DHCP 绑定的独立 DNS，请先移除绑定中的 DNS 覆盖'
STATIC_UNKNOWN = 'iKuai 静态 DHCP 绑定含未验证字段，无法排除 DNS 覆盖'
BAD_NETWORK = 'DHCP 配置包含无法识别的网段，无法安全确定 DNS 下发范围：'
BAD_ID = 'DHCP 配置缺少唯一标识，无法安全下发 DNS'
NO_MATCH = '未找到包含所选 mihomo 网关的已启用 DHCP 网段，不会自动创建 DHCP 配置'
MULTIPLE = '多个已启用 DHCP 网段包含所选 mihomo 网关，无法确定唯一 DNS 下发范围'
FIELDS = ('id', 'enabled', 'tagname', 'interface', 'phy_ifnames', 'addr_pool', 'exclude_pool', 'netmask', 'gateway',
          'next_server', 'dns1', 'dns2', 'domain', 'wins1', 'wins2', 'lease', 'delay', 'check_addr_valid', 'check_relay_only',
          'opt_type15', 'opt_type28', 'opt_type43', 'opt_type60', 'opt_type66', 'opt_type67', 'opt_type80',
          'opt_type119', 'opt_type125', 'opt_type128', 'opt_type138', 'opt_type121',
          'opt15', 'opt28', 'opt43', 'opt60', 'opt66', 'opt67', 'opt80', 'opt119', 'opt125', 'opt128', 'opt138', 'opt121')
INT_FIELDS = ('id', 'lease', 'delay', 'check_addr_valid', 'check_relay_only', 'opt_type15', 'opt_type28', 'opt_type43',
              'opt_type60', 'opt_type66', 'opt_type67', 'opt_type80', 'opt_type119', 'opt_type125', 'opt_type128',
              'opt_type138', 'opt_type121')
STATIC_FIELDS = ('id', 'enabled', 'tagname', 'interface', 'mac', 'gateway', 'dns1', 'dns2', 'ip_addr', 'comment')


def base():
    seed = dns.world()
    seed['ikuai']['dhcp_server'] = [dns.ikuai_server()]
    seed['ikuai']['dhcp_static'][0].update(dns1='', dns2='')
    return seed


def item(label, entries, seed=None, messages=(), *, bad_network=False, status='pending'):
    # Expected diagnostics are explicit fixture literals, not a validator or a
    # shadow of the application. The actual Python boundary proves each vector.
    message = '；'.join(messages)
    if bad_network:
        message = BAD_NETWORK + message
    return label, tuple(entries), seed or base(), message, ('unknown' if message else status)


def vectors():
    yield item('healthy', tuple('H' + str(number).zfill(4) for number in range(247, 264)) + ('H0147', 'H0148', 'H0149'))
    for field in FIELDS:
        seed = base()
        del seed['ikuai']['dhcp_server'][0][field]
        messages = [FORMAT.format(missing=field, unknown='无')]
        if field == 'enabled':
            messages.append(ENABLED)
        elif field == 'phy_ifnames':
            messages.append(PHYSICAL)
        elif field == 'check_relay_only':
            messages.append(RELAY)
        elif field in ('addr_pool', 'netmask', 'gateway'):
            messages.append(POOL)
        elif field in ('dns1', 'dns2'):
            messages.append(DNS)
        if field == 'id':
            yield item('missing-' + field, ('H0247', 'H0147'), seed, (BAD_ID,))
        else:
            yield item('missing-' + field, ('H0247',), seed, messages,
                       bad_network=field in ('addr_pool', 'netmask', 'gateway'))
    seed = base()
    row = seed['ikuai']['dhcp_server'][0]
    row.pop('domain')
    row.pop('wins1')
    row.update(new_z='keep', new_a='keep')
    yield item('missing-and-unknown', ('H0247',), seed,
               (FORMAT.format(missing='domain,wins1', unknown='new_a,new_z'),))
    for label, patch in (('unknown', {'opaque_field': 'keep'}), ('multiple-unknown', {'new_z': 1, 'new_a': True})):
        seed = base()
        seed['ikuai']['dhcp_server'][0].update(patch)
        yield item(label, ('H0247',), seed, (FORMAT.format(missing='无', unknown=','.join(sorted(patch))),))
    # Every full-form field is separately wrong. Extra diagnostics below are
    # explicit consequences at later public evidence layers for these literals.
    for field in FIELDS:
        seed = base()
        seed['ikuai']['dhcp_server'][0][field] = None
        messages = [TYPE.format(field=field)]
        if field == 'enabled':
            messages.append(ENABLED)
        elif field == 'phy_ifnames':
            messages.append(PHYSICAL)
        elif field == 'check_relay_only':
            messages.append(RELAY)
        elif field in ('addr_pool', 'netmask', 'gateway'):
            messages.append(POOL)
        elif field in ('dns1', 'dns2'):
            messages.append(DNS)
        yield item('type-null-' + field, ('H0248',), seed, messages,
                   bad_network=field in ('addr_pool', 'netmask', 'gateway'))
    for field in INT_FIELDS:
        seed = base()
        seed['ikuai']['dhcp_server'][0][field] = False
        yield item('type-bool-' + field, ('H0248',), seed, (TYPE.format(field=field),))
    for field, values in (('lease', ('120', 120.0, [], {}, True)), ('tagname', (False, 1, 1.5, [], {}))):
        for index, value in enumerate(values):
            seed = base()
            seed['ikuai']['dhcp_server'][0][field] = value
            yield item('type-other-' + field + '-' + str(index), ('H0248',), seed, (TYPE.format(field=field),))
    for label, value in (('empty', ''), ('case', 'YES'), ('unknown', 'unknown')):
        seed = base()
        seed['ikuai']['dhcp_server'][0]['enabled'] = value
        yield item('enabled-' + label, ('H0249',), seed, (ENABLED,))
    seed = base()
    seed['ikuai']['dhcp_server'][0]['enabled'] = 'no'
    yield item('enabled-no', ('H0249',), seed, (NO_MATCH,))
    for index, value in enumerate((None, True, False, 1.0, 0, 2, '1', [], {})):
        seed = base()
        seed['ikuai']['dhcp_server'][0]['status'] = value
        yield item('status-' + str(index), ('H0250',), seed, (STATUS,))
    for value in (1, -1, 2):
        seed = base()
        seed['ikuai']['dhcp_server'][0]['check_relay_only'] = value
        yield item('relay-' + str(value), ('H0251',), seed, (RELAY,))
    for index, value in enumerate(('', 'lan1', 'ALL', 'all,lan1', 'eth0')):
        seed = base()
        seed['ikuai']['dhcp_server'][0]['phy_ifnames'] = value
        yield item('physical-' + str(index), ('H0252',), seed, (PHYSICAL,))
    for label, patch, guarded in (
        ('reversed', {'addr_pool': '10.44.0.200-10.44.0.100'}, True),
        ('outside-end', {'addr_pool': '10.44.0.100-10.45.0.200'}, True),
        ('outside-gateway', {'gateway': '10.55.0.1'}, True),
        ('empty', {'addr_pool': ''}, False), ('invalid-start', {'addr_pool': 'bad-10.44.0.200'}, False),
        ('invalid-end', {'addr_pool': '10.44.0.100-bad'}, False),
        ('empty-start', {'addr_pool': '-10.44.0.200'}, False), ('empty-end', {'addr_pool': '10.44.0.100-'}, False),
        ('ipv6', {'addr_pool': 'fd42:cf::1-fd42:cf::2'}, False),
        ('extra-delimiter', {'addr_pool': '10.44.0.100-10.44.0.200-extra'}, False),
        ('bad-mask', {'netmask': 'bad'}, False), ('noncontiguous-mask', {'netmask': '255.0.255.0'}, False),
        ('bad-gateway', {'gateway': 'bad'}, False), ('ipv6-gateway', {'gateway': 'fd42:cf::1'}, False),
    ):
        seed = base()
        seed['ikuai']['dhcp_server'][0].update(patch)
        yield item('pool-' + label, ('H0254', 'H0255', 'H0148', 'H0149') + (('H0253',) if guarded else ()),
                   seed, (POOL,), bad_network=True)
    for label, patch in (('singleton', {'addr_pool': '10.44.0.100-10.44.0.100'}), ('prefix-mask', {'netmask': '24'})):
        seed = base()
        seed['ikuai']['dhcp_server'][0].update(patch)
        yield item('pool-valid-' + label, ('H0253', 'H0254', 'H0255', 'H0148', 'H0149'), seed)
    for field in ('dns1', 'dns2'):
        for index, value in enumerate(('auto', 'fd42:cf::53', '999.1.2.3', '10.44.0.53,10.44.0.54', '<auto>')):
            seed = base()
            seed['ikuai']['dhcp_server'][0][field] = value
            yield item('dns-' + field + '-' + str(index), ('H0256', 'H0257'), seed, (DNS,))
    seed = base()
    seed['ikuai']['dhcp_server'][0].update(dns1='auto', dns2='bad')
    yield item('dns-both-invalid', ('H0256', 'H0257'), seed, (DNS,))
    for label, first, second, status in (
        ('first', '10.44.0.54', '', 'pending'), ('second', '', '10.44.0.54', 'pending'),
        ('duplicate', '10.44.0.54', '10.44.0.54', 'pending'),
        ('target-inherited', '10.44.0.53', '', 'pending'), ('target-explicit', '10.44.0.53', '10.44.0.53', 'synced')):
        seed = base()
        seed['ikuai']['dhcp_server'][0].update(dns1=first, dns2=second)
        yield item('dns-valid-' + label, ('H0256', 'H0257'), seed, status=status)
    for label, value in (('null', None), ('false', False), ('true', True), ('integer', 1), ('number', 1.5),
                         ('array', []), ('object', {})):
        seed = base()
        seed['ikuai']['dhcp_static'][0]['ip_addr'] = value
        yield item('binding-ip-type-' + label, ('H0258', 'H0259', 'H0260'), seed, (STATIC_IP,))
    for index, value in enumerate(('', 'bad', 'fd42:cf::21', '10.44.0.0/24', '10.44.0.20-10.44.0.30', ' 10.44.0.21 ', '010.044.0.21')):
        seed = base()
        seed['ikuai']['dhcp_static'][0]['ip_addr'] = value
        yield item('binding-ip-value-' + str(index), ('H0259', 'H0260'), seed, (STATIC_IP,))
    seed = base()
    seed['ikuai']['dhcp_static'][0].pop('ip_addr')
    yield item('binding-ip-missing', ('H0258', 'H0259', 'H0260'), seed, (STATIC_IP,))
    for field in STATIC_FIELDS:
        if field == 'ip_addr':
            continue  # Earlier explicit address guard; tested separately above.
        for kind in ('missing', 'wrong-type'):
            seed = base()
            row = seed['ikuai']['dhcp_static'][0]
            if kind == 'missing':
                del row[field]
            else:
                row[field] = None
            # Static id is not type-checked by this read-only DNS reader.
            if field == 'id' and kind == 'wrong-type':
                yield item('binding-shape-' + field + '-' + kind, ('H0261',), seed)
            else:
                yield item('binding-shape-' + field + '-' + kind, ('H0261',), seed, (STATIC_SHAPE,))
    for label, patch in (('unknown-enabled', {'enabled': 'unknown'}), ('upper-enabled', {'enabled': 'YES'})):
        seed = base()
        seed['ikuai']['dhcp_static'][0].update(patch)
        yield item('binding-shape-' + label, ('H0261',), seed, (STATIC_SHAPE,))
    for label, patch in (('first', {'dns1': '10.44.0.53'}), ('second', {'dns2': '10.44.0.53'}),
                         ('both', {'dns1': '10.44.0.53', 'dns2': '10.44.0.54'})):
        seed = base()
        seed['ikuai']['dhcp_static'][0].update(patch)
        yield item('binding-dns-' + label, ('H0262',), seed, (STATIC_DNS,))
    seed = base()
    seed['ikuai']['dhcp_static'][0]['opaque_field'] = 'unverified'
    yield item('binding-unknown', ('H0263',), seed, (STATIC_UNKNOWN,))
    for label in ('disabled', 'outside', 'runtime'):
        seed = base()
        if label == 'disabled':
            seed['ikuai']['dhcp_static'] = [{'enabled': 'no', 'ip_addr': None, 'opaque': 'ignored'}]
        elif label == 'outside':
            seed['ikuai']['dhcp_static'] = [{'enabled': 'unknown', 'ip_addr': '10.55.0.21', 'opaque': 'ignored'}]
        else:
            seed['ikuai']['dhcp_static'][0].update(termname='合成终端🧪', hostname='client.example', ip_addr_int=123)
        yield item('binding-ignored-' + label, ('H0258', 'H0259', 'H0260', 'H0261', 'H0262', 'H0263'), seed)
    for label in ('duplicate', 'duplicate-outside', 'duplicate-disabled', 'unique-outside'):
        seed = base()
        second = copy.deepcopy(seed['ikuai']['dhcp_server'][0])
        if label in ('duplicate-outside', 'unique-outside'):
            second.update(addr_pool='10.55.0.100-10.55.0.200', gateway='10.55.0.1')
        if label == 'unique-outside':
            second['id'] = 2
        if label == 'duplicate-disabled':
            second['enabled'] = 'no'
        seed['ikuai']['dhcp_server'].append(second)
        message = BAD_ID if label in ('duplicate', 'duplicate-outside') else ''
        yield item('server-id-' + label, ('H0147',), seed, (message,) if message else ())
    seed = base()
    second = copy.deepcopy(seed['ikuai']['dhcp_server'][0])
    second['id'] = 2
    seed['ikuai']['dhcp_server'].append(second)
    yield item('server-multiple-matching', (), seed, (MULTIPLE,))


def cases():
    for index, (label, entries, seed, message, status) in enumerate(vectors()):
        root = '/api/routers/r-ik'
        yield Case('R018/ikuai-dhcp-' + label, 'R018', 'GET', root + '/report', 200, entries,
                   prepare=(seed, dns.online()) if index == 0 else (seed,), unchanged=False, effect='device-read',
                   times=('generated_at',), checks={'success': True, 'report/dhcp_dns/status': status,
                       'report/dhcp_dns/blockers': [message] if message else [],
                       **({'report/dhcp_dns/message': message} if message else {})})
        yield Case('R012/ikuai-dhcp-' + label, 'R012', 'POST', root + '/plan', 200, entries,
                   unchanged=False, effect='device-read', checks={'success': True, 'plan/blockers': [message] if message else []})
        if message:
            yield Case('R011/ikuai-dhcp-' + label, 'R011', 'POST', root + '/apply', 409, entries,
                       unchanged=False, effect='device-read', message=message, checks={'success': False, 'plan/blockers': [message]})
