"""HTTP method dispatch, including HEAD entering a combined GET/POST view."""
from cases import Case
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
