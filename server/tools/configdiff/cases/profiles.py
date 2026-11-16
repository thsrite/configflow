"""Profile metadata, cloning, defaults, independent imports and Agent bindings."""
import copy

from harness import Pair, DesignError, json_body
from cases.helpers import profile_policy, profile_capture


def prepare(pair):
    pair.policy = profile_policy(pair)
    return profile_capture(pair)


def metadata(binary, recorder):
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        pair.request('R115-S01', 'GET /api/profiles', expect=200, capture=capture, writes=True)
        for case, data in [('S01', {'id': 'created-a', 'name': '空间🧪'}),
                           ('S03', {'id': 'name-default'}), ('S04', {'id': 'cloned-a', 'name': '克隆', 'clone_from': 'default'}),
                           ('B01', {'id': 'unicode', 'name': '中文🧪"\\,\n'}),
                           ('B03', {'id': 'extensions', 'name': '扩展', 'unknown_metadata': {'keep_or_discard': True}})]:
            pair.request('R116-' + case, 'POST /api/profiles', body=json_body(data), expect=201, writes=True, capture=capture)
        pair.request('R116-S02', 'POST /api/profiles', body=json_body({'name': '自动ID🧪'}), expect=201,
                     writes=True, capture=profile_capture(pair, generated=True))
        for case, data, status in [('F08', [1], 500), ('F09', {'id': 'bad-name', 'name': ' '}, 400),
                                   ('F10', {'id': 'bad!id'}, 400), ('F11', {'id': 'default'}, 409),
                                   ('F12', {'id': 'bad-clone', 'clone_from': 'missing-synthetic'}, 404),
                                   ('B02', {'id': 'x' * 65}, 400)]:
            pair.request('R116-' + case, 'POST /api/profiles', body=json_body(data), expect=status, writes=True, capture=capture)
        pair.request('R115-B01', 'GET /api/profiles', expect=200, capture=capture)
        for case in ('S01', 'B01'):
            pair.request('R118-' + case, 'GET /api/profiles/<profile_id>', '/api/profiles/unicode', expect=200, capture=capture)
        for case, body in [('S01', {'name': '改名'}), ('S02', {'description': '说明🧪'}),
                           ('S03', {'name': '同时改', 'description': '说明乙'}), ('B01', {'name': '中文🧪'}),
                           ('B02', {'description': ''})]:
            pair.request('R119-' + case, 'PUT /api/profiles/<profile_id>', '/api/profiles/other',
                         body=json_body(body), expect=200, writes=True, capture=capture)
        for case, body in [('F08', {'unknown': True}), ('F09', {'name': ' '}),
                           ('F10', {'name': 1}), ('F11', {'description': []})]:
            pair.request('R119-' + case, 'PUT /api/profiles/<profile_id>', '/api/profiles/other',
                         body=json_body(body), expect=400, writes=True, capture=capture)
        for case, body in [('S01', {'id': 'clone-explicit', 'name': '克隆🧪'}), ('B01', {'id': 'clone-unicode', 'name': '中文🧪'})]:
            pair.request('R123-' + case, 'POST /api/profiles/<profile_id>/clone', '/api/profiles/default/clone',
                         body=json_body(body), expect=201, writes=True, capture=capture)
        pair.request('R123-S02', 'POST /api/profiles/<profile_id>/clone', '/api/profiles/default/clone',
                     body='{}', expect=201, writes=True, capture=profile_capture(pair, generated=True))
        for case, data, status in [('F08', [1], 400), ('F09', {'id': 'clone-bad', 'name': ' '}, 400),
                                   ('F10', {'id': 'bad!id'}, 400), ('F11', {'id': 'default'}, 409)]:
            pair.request('R123-' + case, 'POST /api/profiles/<profile_id>/clone', '/api/profiles/default/clone',
                         body=json_body(data), expect=status, writes=True, capture=capture)
        for case in ('S01', 'S02', 'B01'):
            pair.request('R124-' + case, 'POST /api/profiles/<profile_id>/default', '/api/profiles/other/default',
                         body='{}', expect=200, writes=True, capture=capture)
        pair.request('R125-S01', 'GET /api/profiles/<profile_id>/export', '/api/profiles/default/export', expect=200, capture=capture, writes=True)
        pair.request('R125-B03', 'GET /api/profiles/<profile_id>/export', '/api/profiles/default/export', expect=200, capture=capture)
        pair.request('R118-B02', 'GET /api/profiles/<profile_id>', '/api/profiles/default?profile=other',
                     headers={'X-ConfigFlow-Profile': 'other'}, expect=200, capture=capture)
        pair.request('R119-B04', 'PUT /api/profiles/<profile_id>', '/api/profiles/default?profile=other',
                     headers={'X-ConfigFlow-Profile': 'other'}, body=json_body({'name': 'URL优先🧪'}),
                     expect=200, writes=True, capture=capture)


def missing_and_selectors(binary, recorder):
    records = [(117, 'DELETE', '', 12, 14), (118, 'GET', '', 8, 10), (119, 'PUT', '', 12, 13),
               (120, 'GET', '/agents', 8, 9), (121, 'POST', '/agents/rebind', None, 18),
               (123, 'POST', '/clone', 13, 14), (124, 'POST', '/default', 8, 10),
               (125, 'GET', '/export', 8, 9), (137, 'POST', '/import', 13, 14)]
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        for rid, method, suffix, missing, first in records:
            route = method + ' /api/profiles/<profile_id>' + suffix
            if missing is not None:
                pair.request(f'R{rid:03}-F{missing:02}', route, '/api/profiles/missing-synthetic' + suffix,
                             body='{}' if method in ('PUT', 'POST') else None, expect=404, writes=True)
            for offset, (name, status) in enumerate([('missing-synthetic', 404), ('bad!id', 400)]):
                pair.request(f'R{rid:03}-F{first+offset:02}', route, '/api/profiles/' + name + suffix,
                             body='{}' if method in ('PUT', 'POST') else None, expect=status, writes=True)
            pair.request(f'R{rid:03}-F90', route, '/api/profiles/missing-synthetic' + suffix, auth='',
                         body='{}' if method in ('PUT', 'POST') else None, expect=404, writes=True)
        for rid, suffix in [(118, ''), (124, '/default')]:
            pair.request(f'R{rid:03}-F09', ('GET' if rid == 118 else 'POST') + ' /api/profiles/<profile_id>' + suffix,
                         '/api/profiles/bad!id' + suffix, expect=400, writes=True)


def deletion(binary, recorder):
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        pair.request('R117-S01', 'DELETE /api/profiles/<profile_id>', '/api/profiles/other', expect=204, writes=True, capture=capture)
        pair.request('R115-S02', 'GET /api/profiles', expect=200, capture=capture)
        pair.request('R117-F08', 'DELETE /api/profiles/<profile_id>', '/api/profiles/default', expect=409, writes=True, capture=capture)
    for case, query, body, expected in [('S02', '?new_default=other', {}, 204),
                                        ('S03', '', {'new_default': 'other'}, 204),
                                        ('F09', '', {}, 409), ('F10', '?new_default=missing', {}, 400),
                                        ('B01', '?new_default=default', {}, 400),
                                        ('B02', '?new_default=other', {'new_default': 'missing'}, 204)]:
        with Pair(binary, recorder, options={'agents': []}) as pair:
            capture = prepare(pair)
            pair.request('R117-' + case, 'DELETE /api/profiles/<profile_id>', '/api/profiles/default' + query,
                         body=json_body(body), expect=expected, writes=True, capture=capture)
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        pair.request('R117-F11', 'DELETE /api/profiles/<profile_id>', '/api/profiles/default?new_default=other',
                     expect=409, writes=True, capture=capture)


def bindings(binary, recorder):
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        for case, path in [('S01', 'default'), ('S03', 'other'), ('B02', 'default')]:
            pair.request('R120-' + case, 'GET /api/profiles/<profile_id>/agents', '/api/profiles/' + path + '/agents',
                         expect=200, capture=capture, writes=True)
        good = {'target_profile_id': 'other', 'agent_ids': ['agent-synthetic']}
        for case, body, status in [('F08', [], 400), ('F09', {'agent_ids': ['agent-synthetic']}, 400),
                                   ('F10', {**good, 'target_profile_id': 'missing-synthetic'}, 404),
                                   ('F11', {**good, 'target_profile_id': 'default'}, 400),
                                   ('F12', {'target_profile_id': 'other', 'agent_ids': []}, 400),
                                   ('F13', {**good, 'agent_ids': ['agent-synthetic', 'agent-synthetic']}, 400),
                                   ('F14', {**good, 'agent_ids': ['unexpected']}, 409)]:
            pair.request('R121-' + case, 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
                         body=json_body(body), expect=status, writes=True, capture=capture)
        pair.policy.times.add('db.fields.system//agents.value[0].updated_at')
        pair.request('R121-S01', 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
                     body=json_body(good), expect=200, writes=True, capture=capture)
    for case, updates in [('F15', {'deployments': {'synthetic': {'status': 'preparing'}}}),
                           ('F16', {'latest_upgrade': {'status': 'running'}})]:
        with Pair(binary, recorder, options={'agent_updates': updates}) as pair:
            capture = prepare(pair)
            pair.request('R121-' + case, 'POST /api/profiles/<profile_id>/agents/rebind', '/api/profiles/default/agents/rebind',
                         body=json_body({'target_profile_id': 'other', 'agent_ids': ['agent-synthetic']}),
                         expect=409, writes=True, capture=capture)


def independent_import(binary, recorder):
    with Pair(binary, recorder) as pair:
        capture = prepare(pair)
        exported = pair.request('R137-S02', 'POST /api/profiles/<profile_id>/import', '/api/profiles/default/import',
                                body=json_body({'mihomo': {'custom_config': 'dns:\n  enable: true\n'}}),
                                expect=200, writes=True, capture=capture)
        def readback(reply):
            if reply.json().get('mihomo', {}).get('custom_config') != 'dns:\n  enable: true\n':
                raise DesignError('Python independent profile import was not observable through export')
        pair.request('T09', 'GET /api/profiles/<profile_id>/export', '/api/profiles/default/export',
                     variant='independent-import-readback', expect=200, check=readback, capture=capture, writes=True)
        pair.request('R137-S01', 'POST /api/profiles/<profile_id>/import', '/api/profiles/default/import',
                     body=json_body({'config': {'mosdns': {'api_enabled': True, 'api_address': ':18081'}}}),
                     expect=200, writes=True, capture=capture)
        for case, body, expected in [('F08', [], 400), ('F09', {'nodes': []}, 400),
                                     ('F10', {'resource_refs': {}}, 400), ('F11', {'_revision': -1}, 409),
                                     ('F12', {'proxy_groups': [{'id': 'g', 'name': '坏引用', 'type': 'select', 'manual_nodes': ['missing']}]}, 409),
                                     ('B01', {}, 200), ('B02', {'mihomo': {'custom_config': '', 'unknown_extension': '保留🧪'}}, 200)]:
            pair.request('R137-' + case, 'POST /api/profiles/<profile_id>/import', '/api/profiles/default/import',
                         body=json_body(body), expect=expected, writes=True, capture=capture)


GROUPS = {'profiles-metadata': metadata, 'profiles-missing': missing_and_selectors,
          'profiles-deletion': deletion, 'profiles-bindings': bindings,
          'profiles-independent-import': independent_import}
