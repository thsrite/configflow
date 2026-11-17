"""Recorded ownership versus live RouterOS objects at the public read boundary."""
import copy

from cases import Case
from fixtures import agent_options
from seed import router


RESOURCES = (
    ('/ip/firewall/address-list', '*A1', {'list': 'CF_Source_Proxy_IP_1', 'address': '10.44.0.21'}),
    ('/ip/firewall/address-list', '*A2', {'list': 'CF_Destination_Proxy_IP_1', 'address': '10.55.0.0/24'}),
    ('/ip/firewall/mangle', '*A3', {'chain': 'prerouting', 'action': 'jump', 'jump-target': 'cf-policy-chain',
                                 'src-address-list': 'CF_Source_Proxy_IP_1',
                                 'dst-address-list': 'CF_Destination_Proxy_IP_1', 'passthrough': 'yes'}),
    ('/routing/table', '*A4', {'name': 'cf-policy-table', 'fib': ''}),
    ('/ip/route', '*A5', {'dst-address': '0.0.0.0/0', 'gateway': '10.44.0.2@main',
                        'routing-table': 'cf-policy-table', 'unknown-native-setting': 'keep-in-baseline'}),
    ('/ip/firewall/nat', '*A7', {'chain': 'srcnat', 'action': 'accept',
                              'src-address-list': 'CF_Source_Proxy_IP_1'}),
)


def options():
    value = agent_options()
    manifest = {'version': 2, 'resources': [dict(path=path, id=ident, config=copy.deepcopy(config))
                                          for path, ident, config in RESOURCES]}
    value['alpha']['routers']['items'] = [router(ros_policy_state=manifest)]
    value['beta']['routers']['items'] = [router()]
    return value


def device_world():
    tables = {}
    for path, ident, config in RESOURCES:
        tables.setdefault(path, []).append({'.id': ident, **copy.deepcopy(config)})
    # Same name and comment never establish ownership. Live table order also
    # deliberately differs from the manifest's stable UI order.
    tables['/ip/firewall/address-list'].insert(0, {'.id': '*FF', 'list': 'CF_Source_Proxy_IP_1',
                                                'address': '10.44.0.99', 'comment': 'foreign 同名不得冒充'})
    tables['/ip/firewall/address-list'].reverse()
    return {'action': 'seed', 'ros': tables}


FOREIGN = (('ros', '/ip/firewall/address-list', '.id', '*FF'),)


def reads():
    path = '/api/routers/r-ros/policy'
    expected = [{'id': ident, 'kind': kind, 'status': 'matched', 'editor': editor,
                 'configured_address': config.get('address', ''),
                 'fields': {key: ('true' if key == 'fib' else value)
                            for key, value in config.items() if key != 'unknown-native-setting'}}
                for (_, ident, config), kind, editor in zip(RESOURCES,
                    ('addresses', 'addresses', 'mangle', 'tables', 'routes', 'nat'),
                    ('source', 'destination', 'settings', 'settings', 'settings', 'settings'))]
    sources = [{'id': 'o-a', 'name': '合成设备🧪 o-a'}]
    yield Case('R013/recorded-order-and-roles', 'R013', 'GET', path, 200,
               ('R013-S01', 'R013-S03', 'R013-B02', 'R013-B04', 'R013-B05'),
               prepare=(device_world(),), unchanged=False, effect='device-read', foreign=FOREIGN,
               checks={'success': True, 'policy/objects': expected, 'policy/source_agents': sources},
               header_checks={'Cache-Control': 'no-store'}, times=('generated_at',))
    yield Case('R013/HEAD-reads-recorded-objects', 'R013', 'HEAD', path, 200,
               ('X06', 'R013-B90'), unchanged=False, effect='device-read', foreign=FOREIGN,
               expected_requests=tuple({'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest' + table}, 'count': 1}
                                       for table in dict.fromkeys(item[0] for item in RESOURCES)))
    for label in ('external-source', 'missing-source', 'runtime-only', 'unknown-field'):
        world = device_world()
        tables = world['ros']
        rows = tables['/ip/firewall/address-list']
        source = next(row for row in rows if row['.id'] == '*A1')
        shown = copy.deepcopy(expected)
        if label == 'external-source':
            source.update(list='external-renamed', address='10.44.0.22')
            shown[0]['status'] = 'changed'
            shown[0]['fields'].update(list='external-renamed', address='10.44.0.22')
        elif label == 'missing-source':
            rows.remove(source)
            shown[0]['status'] = 'missing'
        elif label == 'runtime-only':
            tables['/ip/route'][0].update(active='true', invalid='false', packets='901', bytes='1001')
            shown[4]['fields'].update(active='true', invalid='false', packets='901', bytes='1001')
        else:
            tables['/ip/route'][0]['unknown-native-setting'] = 'external-change'
            shown[4]['status'] = 'changed'
        yield Case('R013/' + label, 'R013', 'GET', path, 200,
                   ('R013-S01', 'R013-B02', 'R013-B04'), prepare=(world,), unchanged=False,
                   effect='device-read', foreign=FOREIGN, checks={'policy/objects': shown})
    yield Case('R013/beta-no-claimed-objects', 'R013', 'GET', path + '?profile=beta', 200,
               ('R013-S02', 'R013-S03', 'R013-B01', 'R013-B06'), profile='beta',
               checks={'policy/objects': [], 'policy/source_agents': [{'id': 'o-b', 'name': '合成设备🧪 o-b'}]},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0},))
    yield Case('R013/live-read-refused', 'R013', 'GET', path, 502,
               ('R013-F13', 'H1318', 'H1319'), unchanged=False, effect='device-read',
               prepare=({'action': 'fault', 'match': {'peer': 'ros', 'method': 'GET',
                         'path': '/rest/ip/firewall/address-list'},
                         'status': 500, 'body': {'message': 'synthetic policy read refusal'}},),
               formats={'message': r'.+synthetic policy read refusal.+?'})
