"""Node deletion blockers, chain validation and cross-profile confirmations."""
import copy

from harness import Pair, json_body
from cases.config_backup import message
from cases.helpers import profile_policy
from cases.nodes import NODE
from cases.setup import Stable
from cases.subscriptions import require


def options(kind):
    result = {'shared': {'nodes': [NODE, {**NODE, 'id': 'node-b', 'name': '另一节点🧪'}]}}
    if kind == 'chain':
        result['profile'] = {'proxy_groups': [{'id': 'node-chain', 'name': '代理链🧪', 'type': 'chain',
            'chain': {'entry': {'type': 'node', 'id': 'node-a'}, 'exit': {'type': 'node', 'id': 'node-b'}}}]}
    else:
        result['shared']['subscription_aggregations'] = [{'id': 'node-aggregate', 'name': '只含节点的聚合🧪',
            'nodes': ['node-a'], 'subscriptions': []}]
    return copy.deepcopy(result)


def blockers(binary, recorder):
    for kind in ('chain', 'aggregation'):
        with Pair(binary, recorder, options=options(kind)) as pair:
            pair.policy = profile_policy(pair)
            pair.request('R093-F10' if kind == 'chain' else 'R093-F11', 'DELETE /api/nodes/<node_id>',
                '/api/nodes/node-a', expect=409, writes=True, interaction=Stable(pair),
                check=lambda reply: require(reply.json()['message'].startswith('资源 节点🧪 仍被引用：'),
                                            'single deletion reaches shared reference guard'), variant=kind)
            if kind != 'chain':
                continue
            replies = pair.request('R096-S02', 'POST /api/nodes/delete-preview', body=json_body({'ids': ['node-a']}),
                expect=200, writes=True, interaction=Stable(pair), variant='chain-blocker-visible')
            require(len(replies[0].json()['blockers']) == 1 and '是代理链' in replies[0].json()['blockers'][0],
                    'preview exposes explicit chain blocker without changing storage')
            confirmations = [reply.json()['confirmation'] if reply.status else '' for reply in replies]
            pair.request('R095-F14', 'POST /api/nodes/delete', body='{}',
                bodies=[json_body({'ids': ['node-a'], 'confirmation': value}) for value in confirmations],
                expect=409, writes=True, interaction=Stable(pair),
                check=lambda reply: require(reply.json()['message'].startswith('无法删除：') and '是代理链' in reply.json()['message'],
                                            'even a current confirmation cannot bypass chain blocker'), variant='confirmed-chain-blocker')
            pair.request('R094-F12', 'PUT /api/nodes/<node_id>', '/api/nodes/node-a',
                body=json_body({**NODE, 'params': {'dialer-proxy': 'missing-synthetic-dialer'}}),
                expect=409, writes=True, interaction=Stable(pair), variant='chain-node-invalid-raw-dialer',
                check=lambda reply: require('missing-synthetic-dialer' in reply.json()['message'],
                                            'replacing a chain node reaches raw dialer target validation'))


def cross_profile_confirmation(binary, recorder):
    opts = options('aggregation')
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy = profile_policy(pair)
        replies = pair.request('R096-S02', 'POST /api/nodes/delete-preview', body=json_body({'ids': ['node-a']}),
            expect=200, writes=True, interaction=Stable(pair), variant='before-foreign-profile-reference')
        confirmations = [reply.json()['confirmation'] if reply.status else '' for reply in replies]
        pair.request('R157-S01', 'POST /api/proxy-groups', '/api/proxy-groups?profile=other',
            body=json_body({'id': 'foreign-node-user', 'name': '另一空间引用🧪', 'type': 'select', 'manual_nodes': ['node-a']}),
            expect=200, writes=True, variant='node-delete-confirmation-concurrent-reference')
        pair.request('R096-B02', 'POST /api/nodes/delete-preview', body=json_body({'ids': ['node-a']}), expect=200,
            writes=True, interaction=Stable(pair), variant='foreign-profile-reference-visible',
            check=lambda reply: require(any(value['kind'] == 'profile' and value['id'] == 'other' for value in reply.json()['usages'])
                                        and reply.json()['confirmation'] != confirmations[0],
                                        'a new foreign-profile dependency changes exact deletion confirmation'))
        pair.request('R095-B02', 'POST /api/nodes/delete', body='{}',
            bodies=[json_body({'ids': ['node-a'], 'confirmation': value}) for value in confirmations],
            expect=409, writes=True, interaction=Stable(pair),
            check=message('节点或引用关系已变化，请重新查看删除影响并确认'), variant='foreign-profile-reference-invalidates-confirmation')


GROUPS = {'node-reference-blockers': blockers, 'node-cross-profile-confirmation': cross_profile_confirmation}
