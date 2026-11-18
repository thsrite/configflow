"""DNS Agent and HA address guards over real report/plan/confirmation HTTP."""
import copy
from dataclasses import replace

from cases import Case
import dns
import ikuai_dhcp_guards


HOST = '所选 DNS Agent 的主机地址必须是客户端可访问的 IPv4 地址'
HA_HOST = '所选 DNS 高可用的虚拟 IP 和两台主机地址必须是客户端可访问的 IPv4 地址'
KINDS = (('null', None), ('false', False), ('true', True), ('integer', 171704373),
         ('number', 1.5), ('array', ['10.44.0.53']), ('object', {'ip': '10.44.0.53'}))
BAD = (('empty', ''), ('text', 'invalid'), ('ipv6', 'fd42:cf::53'), ('mapped', '::ffff:10.44.0.53'),
       ('cidr', '10.44.0.53/32'), ('space', ' 10.44.0.53 '), ('leading-zero', '010.044.0.53'),
       ('unspecified', '0.0.0.0'), ('loopback-start', '127.0.0.0'), ('loopback-end', '127.255.255.255'),
       ('multicast-start', '224.0.0.0'), ('multicast-end', '239.255.255.255'), ('broadcast', '255.255.255.255'))
VALID = (('ordinary', '10.44.0.53'), ('documentation', '192.0.2.53'), ('below-loopback', '126.255.255.255'),
         ('above-loopback', '128.0.0.1'), ('below-multicast', '223.255.255.255'),
         ('above-multicast', '240.0.0.0'), ('below-broadcast', '255.255.255.254'), ('above-unspecified', '0.0.0.1'))
FORBIDDEN = {'unspecified', 'loopback-start', 'loopback-end', 'multicast-start', 'multicast-end', 'broadcast'}


def vectors():
    for label, host in KINDS + BAD + VALID:
        options = dns.ikuai_options()
        options['system']['agents'][1].update(host=host, status='online')
        invalid = label not in dict(VALID)
        entries = ('H0142',) + (('H0140',) if label in dict(KINDS) else ()) + (
            ('H0141',) if label in FORBIDDEN else ())
        if not invalid:
            entries = ('H0140', 'H0141', 'H0142')
        yield 'single-' + label, options, HOST if invalid else '', host if not invalid else '', entries
    for position in ('vip', 'primary', 'peer'):
        for label, host in KINDS + BAD + VALID:
            options = dns.ha_options()
            options['alpha']['routers']['items'][1]['dns_agent_id'] = 'dns'
            primary, peer = options['system']['agents'][1:]
            primary['status'] = peer['status'] = 'online'
            if position == 'vip':
                primary['oxidns_ha']['vip'] = peer['oxidns_ha']['vip'] = host
            else:
                (primary if position == 'primary' else peer)['host'] = host
            invalid = label not in dict(VALID)
            entries = ('H0144',) + (('H0143',) if label in dict(KINDS) else ())
            if not invalid:
                entries = ('H0143', 'H0144')
            target = host if position == 'vip' else '10.44.0.55'
            yield 'ha-' + position + '-' + label, options, HA_HOST if invalid else '', target if not invalid else '', entries


def cases(label, message, target, entries):
    root = '/api/routers/r-ik'
    seed = ikuai_dhcp_guards.base()
    # Evidence-only world retains the default realistic mask suffix. No publish
    # is attempted for valid targets here, and every invalid target refuses.
    status = 'unknown' if message else 'pending'
    yield Case('R018/dns-target-' + label, 'R018', 'GET', root + '/report', 200, entries,
        prepare=(seed,), unchanged=False, effect='device-read', times=('generated_at',),
        checks={'success': True, 'report/dhcp_dns/status': status,
                'report/dhcp_dns/target_dns': target, 'report/dhcp_dns/blockers': [message] if message else []})
    yield Case('R012/dns-target-' + label, 'R012', 'POST', root + '/plan', 200, entries,
        unchanged=False, effect='device-read', checks={'success': True, 'plan/blockers': [message] if message else []})
    if message:
        yield Case('R011/dns-target-' + label, 'R011', 'POST', root + '/apply', 409, entries,
            document={}, unchanged=False, effect='device-read', message=message, checks={'success': False},
            expected_requests=tuple({'match': {'peer': 'ikuai', '/body/action': action}, 'count': 0}
                                    for action in ('add', 'edit', 'del')))


def worlds():
    return [('dns-target-' + label, copy.deepcopy(options),
             (lambda label=label, message=message, target=target, entries=entries:
                 cases(label, message, target, entries),))
            for label, options, message, target, entries in vectors()]


GATEWAYS = (('null', None), ('false', False), ('zero', 0), ('empty', ''), ('text', 'bad'),
            ('ipv6', 'fd42:cf::2'), ('mapped', '::ffff:10.44.0.2'), ('cidr', '10.44.0.2/32'),
            ('space', ' 10.44.0.2 '), ('array', ['10.44.0.2']), ('object', {'host': '10.44.0.2'}))
GATEWAY = '无法确定 DHCP DNS 下发范围：mihomo 网关不是有效 IPv4 地址'


def gateway_cases(label, host):
    root = '/api/routers/r-ik'
    entries = ('H0145', 'H0146', 'H0150')
    yield Case('R018/dns-gateway-' + label, 'R018', 'GET', root + '/report', 200, entries,
        prepare=(ikuai_dhcp_guards.base(),), unchanged=False, effect='device-read', times=('generated_at',),
        checks={'report/dhcp_dns/status': 'unknown', 'report/dhcp_dns/target_dns': '10.44.0.53',
                'report/dhcp_dns/blockers': [GATEWAY], 'report/dhcp_dns/message': GATEWAY})
    message = ('未关联 mihomo Agent，无法确定代理路由的下一跳' if not host else
               '代理路由的下一跳不是有效 IPv4 地址，请检查关联 Agent 的主机地址')
    yield Case('R012/dns-gateway-' + label, 'R012', 'POST', root + '/plan', 200, entries,
        unchanged=False, effect='device-read', checks={'success': True, 'plan/blockers': [message]})
    yield Case('R011/dns-gateway-' + label, 'R011', 'POST', root + '/apply', 409, entries,
        document={}, unchanged=False, effect='device-read', message=message, checks={'success': False},
        expected_requests=tuple({'match': {'peer': 'ikuai', '/body/action': action}, 'count': 0}
                               for action in ('add', 'edit', 'del')))


def gateway_worlds():
    result = []
    for label, host in GATEWAYS:
        options = dns.ikuai_options()
        options['system']['agents'][0]['host'] = host
        options['system']['agents'][1]['status'] = 'online'
        result.append(('dns-gateway-' + label, options, (lambda label=label, host=host: gateway_cases(label, host),)))
    return result


def matching_cases():
    # Select explicit public vectors, not a private matching_networks call.
    selected = ('missing-id', 'server-id-duplicate', 'server-id-duplicate-outside', 'pool-empty')
    for case in ikuai_dhcp_guards.cases():
        if any(case.ident == route + '/ikuai-dhcp-' + label
               for label in selected for route in ('R018', 'R012', 'R011')):
            yield replace(case, entries=case.entries + ('H0150',))


def matching_options():
    value = dns.ikuai_options()
    value['system']['agents'][1]['status'] = 'online'
    return value
