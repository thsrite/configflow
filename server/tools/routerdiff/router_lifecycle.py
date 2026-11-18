"""Draft router deletion across profile selectors, with device state retained."""
import copy

from cases import Case
from seed import router
import clients

SELECTORS = ('default', 'profile', 'profile-id', 'header', 'precedence')


def worlds():
    result = []
    for selector in SELECTORS:
        target = 'alpha' if selector in ('default', 'precedence') else 'beta'
        foreign = 'beta' if target == 'alpha' else 'alpha'
        options = {profile: {'routers': {'items': [router(name=profile + ' ROS🧪'),
                                                   router('r-ik', 'ikuai', name=profile + ' iKuai🧪')]}}
                   for profile in ('alpha', 'beta')}
        options[foreign]['routers']['items'][0]['ros_policy_state'] = {'version': 1, 'resources': []}
        if selector == 'header':
            # Empty manifests are false, so they must not block a draft delete.
            for row in options[target]['routers']['items']:
                row.update({field: {} for field in ('ros_policy_state', 'ipv6_fakeip_state', 'ipv6_routes_state',
                                                    'dns_guard_state', 'ipv6_pd_guard_state')})
        query = ('?profile=beta' if selector == 'profile' else '?profile_id=beta' if selector == 'profile-id'
                 else '?profile=alpha&profile_id=beta' if selector == 'precedence' else '')
        headers = {'X-ConfigFlow-Profile': 'beta'} if selector in ('header', 'precedence') else {}
        groups = (lambda selector=selector, target=target, foreign=foreign, query=query, headers=headers:
                  cases(selector, target, foreign, query, headers),)
        result.append(('router-delete-' + selector, copy.deepcopy(options), groups))
    return result


def cases(selector, target, foreign, query, headers):
    common = ('R009-S01', 'R009-B02', 'R009-B03', 'R009-B05')
    prefix = 'R009/lifecycle-' + selector + '-'
    seed = clients.device_world()
    seed['ros']['/ip/route'] = [{'.id': '*MAN', 'dst-address': '10.55.0.0/24',
                               'gateway': '10.44.0.2', 'comment': 'cf-mihomo'}]
    seed['ikuai']['static_rt'] = [{'id': 88, 'dst_addr': '10.55.0.0', 'netmask': '255.255.255.0',
                                 'gateway': '10.44.0.2', 'enabled': 'yes', 'comment': 'cf-mihomo'}]
    retained = ('oxidns', 'device_notes', 'client_fixed_ips')
    no_device = tuple({'match': {'peer': peer}, 'count': 0} for peer in ('ros', 'ikuai', 'agent'))
    yield Case(prefix + 'before', 'R007', 'GET', '/api/routers' + query, 200, common,
               headers=headers, profile=target, prepare=(seed,), unchanged=False, effect='device-read',
               checks={'routers/0/id': 'r-ros', 'routers/0/name': target + ' ROS🧪',
                       'routers/1/id': 'r-ik', 'routers/1/name': target + ' iKuai🧪'},
               header_checks={'X-ConfigFlow-Profile': target}, expected_requests=no_device)
    yield Case(prefix + 'missing', 'R009', 'DELETE', '/api/routers/missing-router' + query, 404,
               ('R009-F10',), headers=headers, profile=target, message='路由器不存在', expected_requests=no_device)
    for ident, label in (('r-ros', 'ros'), ('r-ik', 'ikuai')):
        path = '/api/routers/' + ident + query
        yield Case(prefix + 'delete-' + label, 'R009', 'DELETE', path, 200, common,
                   headers=headers, profile=target, unchanged=False, effect='router-delete',
                   checks={'success': True}, retained_fields=retained,
                   header_checks={'X-ConfigFlow-Profile': target}, expected_requests=no_device)
        yield Case(prefix + 'repeat-' + label, 'R009', 'DELETE', path, 404,
                   ('R009-B01', 'R009-F10'), headers=headers, profile=target,
                   message='路由器不存在', expected_requests=no_device)
        if ident == 'r-ros':
            yield Case(prefix + 'remaining', 'R007', 'GET', '/api/routers' + query, 200, common,
                       headers=headers, profile=target, checks={'routers/0/id': 'r-ik'}, expected_requests=no_device)
    yield Case(prefix + 'empty', 'R007', 'GET', '/api/routers' + query, 200, common,
               headers=headers, profile=target, checks={'routers': []}, expected_requests=no_device)
    yield Case(prefix + 'foreign', 'R007', 'GET', '/api/routers?profile=' + foreign, 200, common,
               profile=foreign, checks={'routers/0/id': 'r-ros', 'routers/0/name': foreign + ' ROS🧪',
                                        'routers/1/id': 'r-ik', 'routers/1/name': foreign + ' iKuai🧪'},
               stored={'routers/items/0/ros_policy_state': {'version': 1, 'resources': []}},
               header_checks={'X-ConfigFlow-Profile': foreign}, expected_requests=no_device)
