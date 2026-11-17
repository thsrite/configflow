"""HTTP method dispatch, including HEAD entering a combined GET/POST view."""
from collections import defaultdict

from cases import Case, concrete
from inventory import load
import clients
from fixtures import agent_peers, heartbeat
from seed import DEVICE_PASSWORD


def head_collection_empty():
    yield Case('R007/HEAD-without-body-is-not-GET', 'R007', 'HEAD', '/api/routers', 400, ('X06',),
               python_bug='HEAD_CREATES_ROUTER', unchanged=False, effect='device-read')


def head_collection_body():
    yield Case('R007/HEAD-creates-router', 'R007', 'HEAD', '/api/routers', 200, ('X06',),
               document={'name': 'HEAD 不应创建🧪', 'type': 'routeros', 'endpoint': 'http://10.44.0.1:18081',
                         'username': 'test-admin', 'password': DEVICE_PASSWORD},
               python_bug='HEAD_CREATES_ROUTER', unchanged=False, effect='profile-write',
               stored={'routers/items/2/name': 'HEAD 不应创建🧪'},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))


def head_forwarding_empty():
    yield Case('R025/HEAD-without-body-is-not-GET', 'R025', 'HEAD', '/api/routers/gateway-forwarding', 400,
               ('X06', 'R025-B90'), prepare=({'action': 'seed', 'agent': agent_peers()}, heartbeat('m-a'), heartbeat('m-b')),
               python_bug='HEAD_PERSISTS_FORWARDING', unchanged=False, effect='device-read',
               expected_requests=({'match': {'peer': 'agent'}, 'count': 0},))


def head_forwarding_body():
    yield Case('R025/HEAD-persists-forwarding', 'R025', 'HEAD', '/api/routers/gateway-forwarding', 200,
               ('X06', 'R025-B90'), document={'agent_id': 'm-a'},
               prepare=({'action': 'seed', 'agent': agent_peers()}, heartbeat('m-a'), heartbeat('m-b')),
               python_bug='HEAD_PERSISTS_FORWARDING', unchanged=False, effect='device-only',
               fake_checks={'agent/10.44.0.2:18083/endpoints/~1api~1linux-gateway~1status/ipv4_forwarding/persisted': True},
               expected_requests=({'match': {'peer': 'agent', 'method': 'POST', 'path': '/api/linux-gateway/forwarding'}, 'count': 1},
                                  {'match': {'peer': 'agent', 'method': 'GET'}, 'count': 0}))


def options_and_dispatch():
    routes, _ = load()
    by_path = defaultdict(list)
    for route in routes.values():
        by_path[route.path].append(route)
    for pattern, group in by_path.items():
        route = group[0]
        path = concrete(pattern)
        allowed = {item.method for item in group} | {'OPTIONS'}
        if 'GET' in allowed:
            allowed.add('HEAD')
        if pattern == '/api/routers/gateway-forwarding':
            # OPTIONS includes the overlapping /<router_id> PUT/DELETE rule.
            allowed.update(('PUT', 'DELETE'))
        for auth in (False, True):
            yield Case(route.ident + '/OPTIONS-valid-auth-' + str(auth), route.ident, 'OPTIONS', path, 200 if auth else 401,
                       ('X06',), auth=auth, allow_methods=tuple(allowed) if auth else (),
                       expected_requests=({'match': {'peer': 'ros'}, 'count': 0},
                                          {'match': {'peer': 'ikuai'}, 'count': 0},
                                          {'match': {'peer': 'agent'}, 'count': 0}))
        for profile, status in (('bad!profile', 400), ('missing-profile', 404)):
            for selector in ('profile', 'profile_id', 'header'):
                headers = {'X-ConfigFlow-Profile': profile} if selector == 'header' else {}
                query = '' if selector == 'header' else '?' + selector + '=' + profile
                expected = 401 if route.ident == 'R001' else status
                yield Case(route.ident + '/OPTIONS-' + profile + '-' + selector, route.ident,
                           'OPTIONS', path + query, expected, ('X06', 'X02'), auth=False, headers=headers,
                           allow_methods=tuple(allowed) if expected == 200 else ())
    for method, path, route, status, allowed in (
        ('GET', '/api/routers/r-ros', 'R010', 405, ('PUT', 'DELETE', 'OPTIONS')),
        ('POST', '/api/routers/r-ros', 'R010', 405, ('PUT', 'DELETE', 'OPTIONS')),
        ('GET', '/api/routers/r-ros/apply', 'R011', 405, ('POST', 'OPTIONS')),
        ('PUT', '/api/routers', 'R007', 405, ('GET', 'HEAD', 'POST', 'OPTIONS')),
        ('DELETE', '/api/client-devices', 'R002', 405, ('GET', 'HEAD', 'PUT', 'OPTIONS')),
        ('POST', '/api/routers/r-ros/report', 'R018', 405, ('GET', 'HEAD', 'OPTIONS')),
        ('GET', '/api/routers/', 'R007', 404, ()),
        ('POST', '/api/routers/', 'R008', 404, ()),
    ):
        for auth in (False, True):
            yield Case(route + '/dispatch-' + method + '-' + path.rsplit('/', 1)[-1] + '-' + str(auth),
                       route, method, path, status if auth else 401, ('X09',), auth=auth, allow_methods=allowed if auth else ())
    for method, route in (('PUT', 'R010'), ('DELETE', 'R009')):
        yield Case(route + '/reserved-gateway-forwarding', route, method, '/api/routers/gateway-forwarding',
                   404, ('X09',), document={'invalid': ['must not reach body validation']}, message='路由器不存在')


def local_heads():
    for route, path in (('R002', '/api/client-devices?agent=missing-agent'),
                        ('R014', '/api/routers/r-ros/policy-settings'),
                        ('R022', '/api/routers/r-ros/rule-sync')):
        yield Case(route + '/HEAD-local-read', route, 'HEAD', path, 200,
                   ('X06', 'R002-B92') if route == 'R002' else ('X06',),
                   expected_requests=({'match': {'peer': 'ros'}, 'count': 0},
                                      {'match': {'peer': 'ikuai'}, 'count': 0}))
    for route, suffix in (('R018', 'report'), ('R019', 'routes')):
        yield Case(route + '/HEAD-live-read', route, 'HEAD', '/api/routers/r-ros/' + suffix,
                   200, ('X06', route + '-B90'), unchanged=False, effect='device-read',
                   expected_requests=({'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/route'}, 'count': 1},))


def discovery_head():
    yield Case('R004/HEAD-probes-LAN', 'R004', 'HEAD', '/api/client-devices/discovery?refresh=1', 200,
               ('X06', 'R004-B90'), prepare=(clients.device_world(),), unchanged=False, effect='device-read',
               expected_requests=({'match': {'peer': 'ros', 'method': 'POST', 'path': '/rest/ping'}, 'count': 1},))
    yield Case('R004/GET-reuses-HEAD-cache', 'R004', 'GET', '/api/client-devices/discovery', 200,
               ('X06', 'R004-S07'), checks={'cached': True},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))


def upstream_head():
    path = '/api/agents/gateway/upstream-routers'
    yield Case('R001/HEAD-reads-linked-routers', 'R001', 'HEAD', path, 200,
               ('X06', 'R001-B92'), unchanged=False, effect='device-read',
               expected_requests=({'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/route'}, 'count': 2},
                                  {'match': {'peer': 'ikuai', '/body/func_name': 'static_rt'}, 'count': 1}))
    yield Case('R001/GET-reuses-HEAD-cache', 'R001', 'GET', path, 200,
               ('X06', 'R001-S06'), checks={'cached': True},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))


def reserved_agents():
    # Read the public agents decorators/route table, not any Go dispatcher.
    for reserved, external in (
        ('gateway-installations', '/api/agents/gateway-installations/<session_id>'),
        ('i', '/api/agents/i/<token>'),
        ('download', '/api/agents/download/<filename>'),
    ):
        for auth in (True, False):
            status = 401 if reserved == 'gateway-installations' and not auth else 404
            message = ('安装会话不存在。' if reserved == 'gateway-installations' else 'Invalid filename')
            yield Case('R001/reserved-' + reserved + '-auth-' + str(auth), 'R001', 'GET',
                       '/api/agents/' + reserved + '/upstream-routers', status,
                       ('X09', 'R001-B91'), auth=auth, manifest_requires=(('GET', external),),
                       message=(message if status == 404 and reserved != 'i' else None),
                       header_checks={'Cache-Control': 'no-store'} if reserved == 'i' else {})
