"""nodes.py CRUD/reorder/delete guards; probe and Sub-Store are separate groups."""
import re

from compare import Difference
from harness import Pair, DesignError, json_body
from cases.helpers import exception_policy, profile_policy

NODE = {'id': 'node-a', 'name': '节点🧪', 'type': 'socks5', 'server': '192.0.2.1', 'port': 18083,
        'params': {'username': 'synthetic-user', 'password': 'synthetic-pass'}}


def crud(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R091-S02', 'GET /api/nodes', expect=200)
        pair.request('R092-S01', 'POST /api/nodes', body=json_body(NODE), expect=200, writes=True)
        pair.request('R091-S01', 'GET /api/nodes', expect=200)
        pair.request('R091-B01', 'GET /api/nodes', expect=200)
        def bind(replies, policy):
            values = [r.json().get('data', {}).get('id', '') for r in replies]
            if not re.fullmatch(r'node_[0-9a-f]{8}', values[0]):
                raise DesignError('Python generated node identity format changed')
            if not re.fullmatch(r'node_[0-9a-f]{8}', values[1]):
                return [Difference('body.data.id', 'node-id-format', '<node_hex8>', values[1])]
            policy.bind(*values, r'node_[0-9a-f]{8}', 'node-id')
            return []
        pair.request('R092-S02', 'POST /api/nodes', body=json_body({k: v for k, v in NODE.items() if k != 'id'}),
                     expect=200, writes=True, capture=bind)
        for case, node in [('S03', {**NODE, 'id': 'node-structured', 'type': 'ss', 'cipher': 'aes-128-gcm', 'password': 'synthetic'}),
                           ('B01', {**NODE, 'id': 'node-unicode', 'name': '中文🧪"\\,\n'}),
                           ('B02', {**NODE, 'id': 'node-extra', 'synthetic_extension': {'nested': [1, True, None]}})]:
            pair.request('R092-' + case, 'POST /api/nodes', body=json_body(node), expect=200, writes=True)
        for case, body, status in [('F08', [], 400), ('F09', {**NODE, 'id': 'dialer', 'dialer_ref': None}, 400),
                                   ('F10', NODE, 400), ('F11', {**NODE, 'id': 'bad-sub', 'subscription_id': 'missing'}, 400),
                                   ('B03', {}, 400)]:
            pair.request('R092-' + case, 'POST /api/nodes', body=json_body(body), expect=status, writes=True)
        for case, body in [('S01', {**NODE, 'synthetic_old': 'removed-next'}), ('S02', {k: v for k, v in NODE.items() if k != 'id'}),
                           ('B01', {**NODE, 'name': '改名🧪'}), ('B02', NODE)]:
            pair.request('R094-' + case, 'PUT /api/nodes/<node_id>', '/api/nodes/node-a', body=json_body(body), expect=200, writes=True)
        for case, path, body, status in [('F08', 'node-a', None, 400), ('F09', 'node-a', {**NODE, 'dialer_ref': None}, 400),
                                         ('F10', 'missing', NODE, 404), ('F11', 'node-a', {**NODE, 'id': 'changed'}, 400)]:
            pair.request('R094-' + case, 'PUT /api/nodes/<node_id>', '/api/nodes/' + path,
                         body=json_body(body), expect=status, writes=True)
        pair.request('R093-F08', 'DELETE /api/nodes/<node_id>', '/api/nodes/missing', expect=404, writes=True)
        pair.request('R093-S01', 'DELETE /api/nodes/<node_id>', '/api/nodes/node-a', expect=200, writes=True)
        pair.request('R093-B01', 'DELETE /api/nodes/<node_id>', '/api/nodes/node-a', expect=404, writes=True)


def reorder(binary, recorder):
    with Pair(binary, recorder, options={'shared': {'nodes': [NODE, {**NODE, 'id': 'node-b', 'name': '乙'},
                                                            {**NODE, 'id': 'node-c', 'name': '丙'}]}}) as pair:
        for case, body, status in [('S01', {'ids': ['node-c'], 'position': 'top'}, 200),
                                   ('S02', {'ids': ['node-a'], 'position': 'bottom'}, 200),
                                   ('S03', {'nodes': [{'id': 'node-b', 'name': 'must-not-overwrite'}]}, 200),
                                   ('F08', {'ids': 'node-a'}, 404), ('F09', {'ids': ['node-a', 'node-a']}, 404),
                                   ('F10', {'ids': [1]}, 404), ('F11', {}, 404), ('F12', {'ids': ['missing']}, 404),
                                   ('B01', {'ids': []}, 200), ('B02', {'ids': ['node-a'], 'position': 'anything'}, 200),
                                   ('B03', {'nodes': [{'id': 'node-a', 'name': 'must-not-overwrite'}]}, 200)]:
            pair.request('R103-' + case, 'POST /api/nodes/reorder', body=json_body(body), expect=status, writes=True)
        pair.request('R103-F14', 'POST /api/nodes/reorder', body='[1]', expect=500,
                     policy=exception_policy(pair), writes=True)


def deletion(binary, recorder):
    for referenced in (False, True):
        options = {'shared': {'nodes': [NODE, {**NODE, 'id': 'node-b', 'name': '保留'}]}}
        if referenced:
            options['shared']['subscription_aggregations'] = [{'id': 'agg-a', 'name': '聚合🧪', 'nodes': ['node-a', 'node-b'], 'subscriptions': []}]
            options['profile'] = {'proxy_groups': [{'id': 'group-a', 'name': '策略🧪', 'type': 'select', 'manual_nodes': ['node-a', 'node-b']}]}
        with Pair(binary, recorder, options=options) as pair:
            policy = profile_policy(pair)
            preview = pair.request('R096-S02' if referenced else 'R096-S01', 'POST /api/nodes/delete-preview',
                                   body=json_body({'ids': ['node-a']}), expect=200, writes=True, policy=policy)
            confirmations = [r.json().get('confirmation') if r.status else None for r in preview]
            if not re.fullmatch(r'[0-9a-f]{64}', confirmations[0] or ''):
                raise DesignError('Python node deletion confirmation is not SHA256')
            if referenced:
                pair.request('R093-F09', 'DELETE /api/nodes/<node_id>', '/api/nodes/node-a',
                             expect=409, writes=True, policy=policy, variant='group-and-aggregation')
            for rid, route in [(95, 'delete'), (96, 'delete-preview')]:
                for case, body, status in [('F08', [], 400), ('F09', {}, 400), ('F10', {'ids': [1]}, 400),
                                           ('F11', {'ids': ['missing']}, 404), ('B01', {'ids': []}, 400)]:
                    if route == 'delete' and isinstance(body, dict):
                        body = {**body, 'confirmation': confirmations[0]}
                    pair.request(f'R{rid:03}-' + case, 'POST /api/nodes/' + route, body=json_body(body),
                                 expect=status, writes=True, policy=policy, variant=str(referenced))
            pair.request('R095-F12', 'POST /api/nodes/delete', body=json_body({'ids': ['node-a']}),
                         expect=400, writes=True, policy=policy, variant=str(referenced))
            pair.request('R095-F13', 'POST /api/nodes/delete', body=json_body({'ids': ['node-a'], 'confirmation': '0' * 64}),
                         expect=409, writes=True, policy=policy, variant=str(referenced))
            pair.request('R095-S02' if referenced else 'R095-S01', 'POST /api/nodes/delete',
                         body='{}', bodies=[json_body({'ids': ['node-a'], 'confirmation': c}) for c in confirmations],
                         expect=200, writes=True, policy=policy)


def validation_without_workers(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.request('R097-S01', 'GET /api/nodes/latency', expect=200)
        for case, body in [('F08', []), ('F09', {'names': [1]}), ('F10', {'keys': 'node-a'}),
                           ('F11', {'keys': [], 'names': []})]:
            pair.request('R098-' + case, 'POST /api/nodes/latency', body=json_body(body), expect=400, writes=True)
        pair.policy.times.add('body.tested_at')
        pair.request('R098-S04', 'POST /api/nodes/latency', body='{"keys":[]}', expect=200, writes=True)
        for case, body in [('F08', []), ('F09', {}), ('F10', {'lines': 'not-array'}),
                           ('F11', {'lines': [1]}), ('F12', {'lines': [''] * 501})]:
            pair.request('R102-' + case, 'POST /api/nodes/parse-lines', body=json_body(body), expect=400, writes=True)
        for case, body in [('S03', {'lines': []}), ('B01', {'lines': [''] * 500}), ('B02', {'lines': [' ', '\t']})]:
            pair.request('R102-' + case, 'POST /api/nodes/parse-lines', body=json_body(body), expect=200, writes=True)


GROUPS = {'nodes-crud': crud, 'nodes-reorder': reorder, 'nodes-deletion': deletion,
          'nodes-validation': validation_without_workers}
