"""DHCP DNS, IPv6 DNS and DNS-guard publication through public HTTP only."""
from cases import Case, Ref
from seed import AGENT_TOKEN
import clients
import releases

DNS_TOKEN = AGENT_TOKEN + '-dns'
FOREIGN = (('ros', '/ip/route', '.id', '*E1'), ('ros', '/ip/firewall/nat', '.id', '*E2'))


def options():
    value = releases.options()
    value['system']['agents'].append({'id': 'dns', 'name': '合成 DNS🧪', 'service_type': 'oxidns',
        'profile_id': 'alpha', 'host': '10.44.0.53', 'port': 18083, 'status': 'offline', 'version': '1.99.0-go',
        'enabled': True, 'token': DNS_TOKEN})
    value['alpha']['routers']['items'][0]['dns_agent_id'] = 'dns'
    return value


def world():
    value = clients.device_world()
    value['ros']['/ip/dhcp-server/lease'] = []
    value['ros']['/ip/dhcp-server/network'][0].update(**{
        'dns-server': '10.44.0.54', 'dns-none': 'false', 'domain': 'lan.example', 'ntp-server': '10.44.0.55'})
    value['ros']['/ip/route'] = [{'.id': '*E1', 'dst-address': '10.44.0.0/24', 'gateway': 'bridge-lan',
        'immediate-gw': 'bridge-lan', 'routing-table': 'main', 'active': 'true', 'connect': 'true', 'dynamic': 'true'}]
    value['ros']['/ip/firewall/nat'] = [{'.id': '*E2', 'chain': 'dstnat', 'action': 'redirect', 'dst-port': '53',
        'protocol': 'udp', 'to-ports': '5353', 'comment': 'foreign DNS rule', 'disabled': 'false'}]
    value['ros']['/ipv6/nd'] = [{'.id': '*E3', 'interface': 'bridge-lan', 'advertise-dns': 'yes',
        'disabled': 'false', 'ra-interval': '3m20s-10m', 'ra-lifetime': '30m', 'comment': '保留 IPv6 通告🧪'}]
    return value


def online():
    return {'rpc': 'http', 'method': 'POST', 'path': '/api/agents/dns/heartbeat', 'document': {},
            'headers': {'Authorization': 'Bearer ' + DNS_TOKEN}, 'auth': False, 'expected': 200}


def guard():
    for index, (block_dot, failover) in enumerate([(False, False), (False, True), (True, False), (True, True)]):
        label = str(index)
        settings = {'enabled': True, 'block_dot': block_dot, 'failover': failover, 'bypass': []}
        yield Case('R010/dns-guard-settings-' + label, 'R010', 'PUT', '/api/routers/r-ros', 200,
                   ('T08',), document={'dns_guard': settings}, prepare=(world(), online()) if index == 0 else (),
                   unchanged=False, effect='profile-write', foreign=FOREIGN, checks={'success': True})
        preview = 'R012/dns-guard-' + label
        roles = ['bypass:10.44.0.2/32', 'bypass:10.44.0.53/32', 'hairpin-udp', 'hairpin-tcp', 'hijack-udp', 'hijack-tcp']
        roles += ['dot-tcp', 'dot-udp'] if block_dot else []
        roles += ['netwatch'] if failover else []
        yield Case(preview, 'R012', 'POST', '/api/routers/r-ros/plan', 200,
                   ('R012-S04', 'R012-S06', 'R012-S09', 'G13', 'G14', 'G15'),
                   unchanged=False, effect='device-read', foreign=FOREIGN,
                   checks={'plan/blockers': [], 'plan/checks/dns_guard/roles': roles},
                   formats={'plan/fingerprint': r'[0-9a-f]{16}'})
        yield Case('R011/dns-guard-' + label, 'R011', 'POST', '/api/routers/r-ros/apply', 200,
                   ('R011-S06', 'R011-S11', 'R011-S14'),
                   document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, requires=('R012',),
                   unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True, 'result/ok': True},
                   preserved=(('ros', '/ip/firewall/nat', {'comment': 'cf-dnsguard:hijack'}),
                              ('ros', '/ip/firewall/nat', {'comment': 'cf-dnsguard:hairpin'})) if index else (),
                   fake_checks={'ros/~1ip~1dhcp-server~1network/0/dns-server': '10.44.0.53',
                                'ros/~1ip~1dhcp-server~1network/0/ntp-server': '10.44.0.55',
                                'ros/~1ip~1dhcp-server~1network/0/domain': 'lan.example',
                                'ros/~1ipv6~1nd/0/advertise-dns': 'no', 'ros/~1ipv6~1nd/0/ra-lifetime': '30m'},
                   fake_lengths={'ros/~1tool~1netwatch': 1 if failover else 0,
                                 'ros/~1ip~1firewall~1filter': 2 if block_dot else 0,
                                 'ros/~1ip~1firewall~1nat': 5})
        yield Case('R018/dns-guard-synced-' + label, 'R018', 'GET', '/api/routers/r-ros/report', 200,
                   ('R018-S05', 'R018-S06'), unchanged=False, effect='device-read', foreign=FOREIGN,
                   checks={'report/dns_guard/status': 'synced', 'report/dhcp_dns/status': 'synced',
                           'report/ipv6_dns/status': 'synced'})
    yield Case('R010/dns-guard-disable', 'R010', 'PUT', '/api/routers/r-ros', 200, ('T08',),
               document={'dns_guard': {'enabled': False}}, unchanged=False, effect='profile-write')
    yield Case('R012/dns-guard-withdraw', 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('G14',),
               unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/checks/dns_guard/roles': []})
    yield Case('R011/dns-guard-withdraw', 'R011', 'POST', '/api/routers/r-ros/apply', 200, ('R011-S11',),
               document={'fingerprint': Ref('R012/dns-guard-withdraw', '/body/plan/fingerprint')}, requires=('R012',),
               unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True},
               stored_absent=('routers/items/0/dns_guard_state',),
               fake_lengths={'ros/~1tool~1netwatch': 0, 'ros/~1ip~1firewall~1filter': 0, 'ros/~1ip~1firewall~1nat': 1},
               fake_checks={'ros/~1ip~1dhcp-server~1network/0/dns-server': '10.44.0.53',
                            'ros/~1ipv6~1nd/0/advertise-dns': 'no'})


def ikuai_options():
    value = options()
    value['alpha']['routers']['items'][1]['dns_agent_id'] = 'dns'
    return value


def ikuai_server():
    row = {'id': 1, 'enabled': 'yes', 'tagname': 'synthetic-dhcp', 'interface': 'lan1', 'phy_ifnames': 'all',
           'addr_pool': '10.44.0.100-10.44.0.200', 'exclude_pool': '', 'netmask': '255.255.255.0',
           'gateway': '10.44.0.1', 'next_server': '', 'dns1': '10.44.0.54', 'dns2': '',
           'domain': 'lan.example', 'wins1': '10.44.0.55', 'wins2': '', 'lease': 120, 'delay': 1,
           'check_addr_valid': 1, 'check_relay_only': 0, 'status': 1, 'available': 100}
    for number in (15, 28, 43, 60, 66, 67, 80, 119, 125, 128, 138, 121):
        row['opt_type' + str(number)] = 0
        row['opt' + str(number)] = ''
    return row


def ikuai_dhcp():
    seed = world()
    seed['presentation'] = {'static_mask_suffix': False}  # Explicit independent plain-mask success variant.
    seed['ikuai']['dhcp_server'] = [ikuai_server()]
    seed['ikuai']['dhcp_static'][0].update(dns1='', dns2='')
    preview = 'R012/iKuai-dhcp-plain-mask'
    yield Case(preview, 'R012', 'POST', '/api/routers/r-ik/plan', 200, ('R012-S04', 'G13'),
               prepare=(seed, online()), unchanged=False, effect='device-read',
               checks={'plan/blockers': [], 'plan/checks/dhcp_dns/dns': ['10.44.0.53']})
    yield Case('R011/iKuai-dhcp-plain-mask', 'R011', 'POST', '/api/routers/r-ik/apply', 200, ('R011-S06', 'G13'),
               document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, requires=('R012',),
               unchanged=False, effect='profile-write-device', checks={'success': True},
               foreign=(('ikuai', 'dhcp_static', 'id', 70),),
               fake_checks={'ikuai/dhcp_server/0': {**ikuai_server(), 'dns1': '10.44.0.53', 'dns2': '10.44.0.53'}})
    yield Case('R018/iKuai-dhcp-plain-mask-synced', 'R018', 'GET', '/api/routers/r-ik/report', 200,
               ('R018-S05',), unchanged=False, effect='device-read',
               checks={'report/dhcp_dns/status': 'synced', 'report/dhcp_dns/target_dns': '10.44.0.53'})


def ha_options():
    value = options()
    primary = value['system']['agents'][1]
    primary.update(oxidns_ha={'enabled': True, 'group_id': 'synthetic-ha', 'peer_id': 'dns-peer', 'vip': '10.44.0.55'},
                   oxidns_ha_state={'status': 'succeeded'}, oxidns_ha_publication={'status': 'succeeded'})
    value['system']['agents'].append({**primary, 'id': 'dns-peer', 'host': '10.44.0.54', 'token': DNS_TOKEN + '-peer',
        'oxidns_ha': {**primary['oxidns_ha'], 'peer_id': 'dns'}})
    return value


def ha():
    seed = world()
    heartbeat = {**online(), 'path': '/api/agents/dns-peer/heartbeat',
                 'headers': {'Authorization': 'Bearer ' + DNS_TOKEN + '-peer'}}
    yield Case('R010/ha-guard-settings', 'R010', 'PUT', '/api/routers/r-ros', 200, ('G14',),
               document={'dns_guard': {'enabled': True, 'block_dot': True, 'failover': True, 'bypass': []}},
               prepare=(seed, heartbeat), unchanged=False, effect='profile-write')
    yield Case('R012/ha-vip-guard', 'R012', 'POST', '/api/routers/r-ros/plan', 200, ('R012-S04', 'G13', 'G14', 'G15'),
               unchanged=False, effect='device-read',
               checks={'plan/blockers': [], 'plan/checks/dhcp_dns/dns': ['10.44.0.55']})
    yield Case('R011/ha-vip-guard', 'R011', 'POST', '/api/routers/r-ros/apply', 200, ('R011-S07',),
               document={'fingerprint': Ref('R012/ha-vip-guard', '/body/plan/fingerprint')}, requires=('R012',),
               unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True},
               fake_checks={'ros/~1ip~1dhcp-server~1network/0/dns-server': '10.44.0.55',
                            'ros/~1tool~1netwatch/0/host': '10.44.0.55'})
    yield Case('R018/ha-vip-synced', 'R018', 'GET', '/api/routers/r-ros/report', 200, ('R018-S05',),
               unchanged=False, effect='device-read', checks={'report/dhcp_dns/status': 'synced',
                    'report/dhcp_dns/target_dns': '10.44.0.55', 'report/dns_guard/status': 'synced'})
