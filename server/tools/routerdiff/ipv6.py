"""IPv6 Fake-IP ownership and next-hop evidence through report/plan/apply HTTP."""
from cases import Case, Ref
from fixtures import heartbeat
from seed import AGENT_TOKEN
import releases


BASE = '/api/routers/r-ros'
CIDR = 'fdfe:dcba::/64'
LAN = 'bridge-lan'
LINK = 'fe80::2%' + LAN
ULA = 'fd42:cf:44::2%' + LAN
CUSTOM = ('POST', '/api/custom-config/mihomo')
FOREIGN = (('ros', '/ipv6/route', '.id', '*E0'),)


def configuration(enabled=True, clients=False):
    return ('ipv6: ' + str(enabled).lower() + '\ntun:\n  enable: true\ndns:\n  enable: true\n'
            '  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.1/16\n  ipv6: ' + str(clients).lower()
            + '\n  fake-ip-range6: ' + CIDR + '\n')


def options():
    value = releases.options()
    value['alpha']['mihomo'] = {'custom_config': configuration()}
    return value


def host():
    return {'success': True, 'interface': 'eth-upstream', 'addresses': [
        {'address': 'fe80::2/64', 'interface': 'eth-upstream', 'usable': False},
        {'address': 'fd42:cf:44::2/64', 'interface': 'eth-upstream', 'usable': True},
        {'address': 'fe80::99/64', 'interface': 'unrelated-nic', 'usable': True}]}


def world(borrowed=False, gateway=LINK):
    foreign = {'.id': '*E0', 'dst-address': 'fd42:cf:99::/64', 'gateway': 'fe80::99%' + LAN,
               'active': 'true', 'disabled': 'false', 'comment': 'foreign IPv6 route'}
    routes = [foreign]
    if borrowed:
        routes.append({'.id': '*E1', 'dst-address': CIDR, 'gateway': gateway, 'routing-table': 'main',
                       'active': 'true', 'disabled': 'false', 'comment': 'foreign valid route, never adopt'})
    return {'action': 'seed', 'ros': {
        '/ip/address': [{'.id': '*A0', 'address': '10.44.0.1/24', 'interface': LAN, 'disabled': 'false'}],
        '/ipv6/address': [{'.id': '*A1', 'address': 'fd42:cf:44::1/64', 'interface': LAN,
                           'disabled': 'false', 'dynamic': 'true', 'valid': '1h', 'preferred': '30m'}],
        '/ipv6/route': routes, '/ipv6/firewall/filter': [], '/ipv6/firewall/address-list': []},
        'agent': {'10.44.0.2:18083': {'token': AGENT_TOKEN + '-gateway',
                  'endpoints': {'/api/ipv6-readiness': host()}}}}


def save_configuration(**values):
    return {'rpc': 'http', 'method': 'POST', 'path': CUSTOM[1],
            'document': {'config': configuration(**values)}, 'expected': 200}


def owned():
    preview = 'R012/fake6-create'
    yield Case('R018/fake6-missing-LLA-preferred', 'R018', 'GET', BASE + '/report', 200,
        ('G16',), prepare=(world(), heartbeat('gateway')), unchanged=False, effect='device-read', foreign=FOREIGN,
        checks={'report/ipv6_fakeip/enabled': True, 'report/ipv6_fakeip/status': 'missing',
                'report/ipv6_fakeip/items/0/gateway': LINK})
    yield Case(preview, 'R012', 'POST', BASE + '/plan', 200, ('G16',),
        unchanged=False, effect='device-read', foreign=FOREIGN,
        checks={'plan/blockers': [], 'plan/stages/2/key': 'ipv6_fakeip',
                'plan/stages/2/ops/0/kind': 'ipv6_fakeip_replace', 'plan/checks/ipv6_fakeip/spec': {'cidr': CIDR, 'gateway': LINK}})
    yield Case('R011/fake6-create', 'R011', 'POST', BASE + '/apply', 200, ('R011-S12', 'G16'),
        document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, requires=('R012',),
        unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True},
        stored={'routers/items/0/ipv6_fakeip_state/spec': {'cidr': CIDR, 'gateway': LINK},
                'routers/items/0/ipv6_fakeip_state/resources/0/path': '/ipv6/route'},
        fake_lengths={'ros/~1ipv6~1route': 2},
        request_order=({'peer': 'ros', 'method': 'PUT', 'path': '/rest/ip/route'},
                       {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ipv6/route'}))
    yield Case('R018/fake6-owned-synced', 'R018', 'GET', BASE + '/report', 200, ('G16',),
        unchanged=False, effect='device-read', foreign=FOREIGN, checks={'report/ipv6_fakeip/status': 'synced'})
    baseline = 'R012/fake6-stable-before-lifetime-tick'
    yield Case(baseline, 'R012', 'POST', BASE + '/plan', 200, ('G16', 'R011-B04'),
        unchanged=False, effect='device-read', checks={'plan/empty': True})
    addresses = world()['ros']['/ipv6/address']
    addresses[0].update(valid='59m59s', preferred='29m59s', packets='12345', bytes='67890')
    yield Case('R012/fake6-lifetime-tick-still-same-plan', 'R012', 'POST', BASE + '/plan', 200,
        ('G16', 'R011-B04'), prepare=({'action': 'seed', 'ros': {'/ipv6/address': addresses}},),
        unchanged=False, effect='device-read', checks={'plan/empty': True,
                    'plan/fingerprint': Ref(baseline, '/body/plan/fingerprint')})
    yield Case('R011/fake6-lifetime-tick-accepts-old-preview', 'R011', 'POST', BASE + '/apply', 200,
        ('G16', 'R011-B04'), document={'fingerprint': Ref(baseline, '/body/plan/fingerprint')}, requires=('R012',),
        unchanged=False, effect='profile-write', foreign=FOREIGN, checks={'success': True})
    yield Case('R012/fake6-dns-mode-switch-keeps-route', 'R012', 'POST', BASE + '/plan', 200,
        ('G16',), prepare=(save_configuration(clients=True),), manifest_requires=(CUSTOM,),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/empty': True})
    withdrawal = 'R012/fake6-disable-withdrawal'
    yield Case(withdrawal, 'R012', 'POST', BASE + '/plan', 200, ('G16',),
        prepare=(save_configuration(enabled=False),), manifest_requires=(CUSTOM,),
        unchanged=False, effect='device-read', checks={'plan/blockers': [], 'plan/stages/2/key': 'ipv6_fakeip',
                'plan/stages/2/ops/0/spec': None})
    yield Case('R011/fake6-withdraw-owned-only', 'R011', 'POST', BASE + '/apply', 200,
        ('R011-S12', 'G16'), requires=('R012',), document={'fingerprint': Ref(withdrawal, '/body/plan/fingerprint')},
        unchanged=False, effect='profile-write-device', foreign=FOREIGN, checks={'success': True},
        stored_absent=('routers/items/0/ipv6_fakeip_state',), fake_lengths={'ros/~1ipv6~1route': 1})


def borrowed_flow(gateway):
    label = 'LLA' if gateway == LINK else 'ULA'
    preview = 'R012/fake6-borrow-' + label
    return [Case(preview, 'R012', 'POST', BASE + '/plan', 200, ('G16',),
        prepare=(world(True, gateway), heartbeat('gateway')), unchanged=False, effect='device-read',
        checks={'plan/blockers': [], 'plan/stages/2/ops': [],
                'plan/checks/ipv6_fakeip/borrowed/id': '*E1', 'plan/checks/ipv6_fakeip/spec/gateway': gateway}),
        Case('R011/fake6-borrow-' + label, 'R011', 'POST', BASE + '/apply', 200, ('G16',), requires=('R012',),
        document={'fingerprint': Ref(preview, '/body/plan/fingerprint')}, unchanged=False, effect='profile-write-device',
        foreign=FOREIGN + (('ros', '/ipv6/route', '.id', '*E1'),), checks={'success': True},
        stored_absent=('routers/items/0/ipv6_fakeip_state',),
        expected_requests=({'match': {'peer': 'ros', 'method': 'PUT', 'path': '/rest/ipv6/route'}, 'count': 0},))]


def borrowed_lla():
    yield from borrowed_flow(LINK)


def borrowed_ula():
    yield from borrowed_flow(ULA)


def conflicts():
    for field, value in (('disabled', 'true'), ('active', 'false'), ('gateway', 'fe80::99%' + LAN),
                         ('blackhole', 'true'), ('unreachable', 'true'), ('prohibit', 'true'),
                         ('invalid', 'true'), ('dynamic', 'true'), ('connect', 'true')):
        state = world(True)
        state['ros']['/ipv6/route'][1][field] = value
        yield Case('R018/fake6-conflict-' + field, 'R018', 'GET', BASE + '/report', 200, ('G16',),
            prepare=(state, heartbeat('gateway')), unchanged=False, effect='device-read',
            foreign=FOREIGN + (('ros', '/ipv6/route', '.id', '*E1'),),
            checks={'report/ipv6_fakeip/status': 'conflict'},
            formats={'report/ipv6_fakeip/blockers/0': r'.+'})
    for flag in ('temporary', 'deprecated', 'tentative', 'dad_failed', 'invalid'):
        state = world()
        report = state['agent']['10.44.0.2:18083']['endpoints']['/api/ipv6-readiness']
        report['addresses'] = [{**report['addresses'][0], flag: True}]
        yield Case('R018/fake6-unusable-host-' + flag, 'R018', 'GET', BASE + '/report', 200, ('G16',),
            prepare=(state, heartbeat('gateway')), unchanged=False, effect='device-read', foreign=FOREIGN,
            checks={'report/ipv6_fakeip/status': 'conflict', 'report/ipv6_fakeip/blockers': [
                '未确认 Mihomo 唯一、稳定的同网段 IPv6 下一跳，请先检查设备 IPv6 网络']})


def head_notifications():
    state = world()
    report = state['agent']['10.44.0.2:18083']['endpoints']['/api/ipv6-readiness']
    report['addresses'] = [{**report['addresses'][0], 'tentative': True}]
    key = 'items/router:alpha:r-ros:ipv6_fakeip'
    yield Case('R018/HEAD-actually-persists-pending-notification', 'R018', 'HEAD', BASE + '/report', 200,
        ('R018-B90', 'X06'), prepare=(state, heartbeat('gateway')), unchanged=False, effect='device-read',
        expected_requests=({'match': {'peer': 'agent', 'method': 'GET', 'path': '/api/ipv6-readiness'}, 'count': 1},),
        file_checks={'notifications.json': {key + '/state': 'pending', key + '/profile_id': 'alpha',
                                            key + '/source': 'router-check'}})
    yield Case('R018/recovered-report-clears-that-notification', 'R018', 'GET', BASE + '/report', 200,
        ('R018-B90', 'X06'), prepare=(world(),), unchanged=False, effect='device-read',
        checks={'report/ipv6_fakeip/status': 'missing'},
        file_checks={'notifications.json': {'items/router:alpha:r-ros:changes/state': 'pending'}},
        file_absent={'notifications.json': (key,)})
