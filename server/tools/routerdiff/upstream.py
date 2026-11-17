"""Linked-router reminders, real reads, cache invalidation and lost Agents."""
from cases import Case, Ref
from seed import router
import releases


def options():
    value = releases.options()
    value['beta']['routers']['items'] = [router(proxy_gateway_agent_id='gateway')]
    return value


def linked():
    path = '/api/agents/gateway/upstream-routers'
    yield Case('R001/linked-first', 'R001', 'GET', path, 200, ('R001-S02', 'R001-B05'),
               unchanged=False, effect='device-read', times=('checked_at',),
               checks={'success': True, 'cached': False, 'agent_profile_id': 'alpha',
                       'routers/0/id': 'r-ros', 'routers/0/profile_id': 'alpha', 'routers/0/state': 'pending',
                       'routers/1/id': 'r-ik', 'routers/1/profile_id': 'alpha',
                       'routers/2/id': 'r-ros', 'routers/2/profile_id': 'beta'},
               header_checks={'Cache-Control': 'no-store'})
    yield Case('R001/linked-cached', 'R001', 'GET', path, 200, ('R001-S06',),
               checks={'cached': True, 'checked_at': Ref('R001/linked-first', '/body/checked_at')},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    for value, entry in [('1', 'S04'), ('true', 'S05')]:
        yield Case('R001/linked-refresh-' + value, 'R001', 'GET', path + '?refresh=' + value, 200, ('R001-' + entry,),
                   unchanged=False, effect='device-read', times=('checked_at',), checks={'cached': False})
    yield Case('R001/linked-profile-alpha', 'R001', 'GET', path + '?profile=alpha', 200, ('R001-S03',),
               unchanged=False, effect='device-read', checks={'routers/0/profile_id': 'alpha', 'routers/1/profile_id': 'alpha'})
    yield Case('R001/linked-one-unknown', 'R001', 'GET', path + '?profile=alpha&refresh=1', 200,
               ('R001-F11', 'R001-B01'), unchanged=False, effect='device-read',
               prepare=({'action': 'fault', 'match': {'peer': 'ros', 'method': 'GET', 'path': '/rest/ip/route'},
                         'status': 500, 'body': {'message': 'synthetic reminder read refusal'}},),
               checks={'cached': False, 'routers/0/state': 'unknown', 'routers/1/state': 'pending'},
               formats={'routers/0/message': r'.+synthetic reminder read refusal.+?'} )
    yield Case('R001/unknown-not-reused', 'R001', 'GET', path + '?profile=alpha', 200,
               ('R001-B01',), unchanged=False, effect='device-read', checks={'cached': False, 'routers/0/state': 'pending'})
    yield Case('R001/known-now-cached', 'R001', 'GET', path + '?profile=alpha', 200,
               ('R001-S06',), checks={'cached': True},
               expected_requests=({'match': {'peer': 'ros'}, 'count': 0}, {'match': {'peer': 'ikuai'}, 'count': 0}))
    yield Case('R001/public-edit-invalidates', 'R001', 'GET', path + '?profile=alpha', 200, ('R001-B03',),
               prepare=({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ros',
                         'document': {'name': '公开编辑后的提醒名称🧪'}, 'expected': 200},),
               unchanged=False, effect='device-read', checks={'cached': False, 'routers/0/name': '公开编辑后的提醒名称🧪'})
    yield Case('R001/linked-header-does-not-scope', 'R001', 'GET', path, 200, ('R001-B04', 'R001-F91'),
               headers={'X-ConfigFlow-Profile': 'missing-profile'}, unchanged=False, effect='device-read',
               checks={'routers/2/profile_id': 'beta'})
    yield Case('R001/deleted-agent-still-linked', 'R001', 'GET', path, 200, ('R001-S07', 'R001-B06'),
               prepare=({'rpc': 'http', 'method': 'DELETE', 'path': '/api/agents/gateway', 'expected': 200},),
               unchanged=False, effect='device-read', checks={'success': True, 'cached': False,
                    'agent_profile_id': 'alpha', 'routers/0/state': 'blocked', 'routers/2/profile_id': 'beta'})
