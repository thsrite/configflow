"""Repository resource closure guards reached by real complete imports."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message, seed_document
from cases.helpers import profile_policy
from cases.nodes import NODE
from cases.pull_routes import SourceStable

UNOBSERVABLE = {
    'H757': 'validate_shapes rejects non-array proxies_order before _collect_resource_refs.',
    'H758': 'validate_shapes rejects non-object proxies_order entries before _collect_resource_refs.',
}


def guards(binary, recorder):
    with Source(b'import validation must not fetch') as source, Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        original = seed_document(pair)
        def call(case, path, value, text, variant, status=400, extra=None):
            data = copy.deepcopy(original)
            target = data
            for key in path[:-1]:
                target = target[key]
            target[path[-1]] = value
            if extra:
                extra(data)
            pair.request(case, 'POST /api/config/import', body=json_body(data), expect=status, writes=True,
                         check=message(text), interaction=SourceStable(pair, source), variant='resource-closure/' + variant)
        for scope, key, label in [('system', 'agents', 'Agent')] + [('shared', key, key) for key in
                    ('nodes', 'subscriptions', 'subscription_aggregations', 'rule_library')]:
            for i, value in enumerate((None, {}, 'synthetic')):
                call('H746', [scope, key], value, label + ' 必须是列表', key + '/type/' + str(i))
            for i, value in enumerate(([None], [{}], [{'id': ''}], [{'id': 1}], [{'id': True}])):
                call('H747', [scope, key], value, label + ' 中每一项必须有 ID', key + '/missing-id/' + str(i))
            call('H748', [scope, key], [{'id': 'duplicate'}, {'id': 'duplicate'}],
                 label + ' 存在重复 ID：duplicate', key + '/duplicate-id')
        for field in ('nodes', 'subscriptions'):
            for i, value in enumerate((None, 'node-a', {}, [1], [None], [{}])):
                call('H749', ['shared', 'subscription_aggregations'], [{'id': 'aggregate', 'name': '聚合🧪', field: value}],
                     '聚合 聚合🧪 的 ' + field + ' 只能包含资源 ID，不能设置资源覆盖', field + '/invalid-list/' + str(i))
            call('H750', ['shared', 'subscription_aggregations'], [{'id': 'aggregate', 'name': '聚合🧪', field: ['duplicate', 'duplicate']}],
                 '聚合 聚合🧪 的 ' + field + ' 含有重复引用', field + '/duplicate-reference')
        group = {'id': 'group', 'name': '合成策略🧪', 'type': 'select'}
        for value in ('', ' '):
            call('H751', ['profiles', 'other', 'proxy_groups'], [{**group, 'proxies_order': [{'type': 'node', 'id': value}]}],
                 '资源引用必须是非空 ID', 'empty-order-reference/' + repr(value))
        for field, kind in [('manual_nodes', 'nodes'), ('subscriptions', 'subscriptions'), ('aggregations', 'subscription_aggregations')]:
            call('H752', ['profiles', 'other', 'proxy_groups'], [{**group, field: ['missing-synthetic']}],
                 '配置 ' + original['profiles']['other']['name'] + ': 引用的共享资源不存在：' + kind + '/missing-synthetic',
                 field + '/missing-resource', 409)
        # This existing group makes the other edge resolvable until the named
        # guard runs; built-in DIRECT is not accepted by chain.select(nodes).
        reference = {'type': 'group', 'id': 'group'}
        for i, value in enumerate((None, [], {}, {'entry': reference}, {'entry': reference, 'exit': reference, 'extra': reference})):
            call('H753', ['profiles', 'other', 'proxy_groups'], [{**group, 'type': 'chain', 'chain': value}],
                 '代理链必须指定前置和落地引用', 'chain-shape/' + str(i))
        for edge in ('entry', 'exit'):
            for i, value in enumerate((None, [], {}, {'type': 'node', 'id': ''}, {'type': 'node', 'id': 1},
                                      {'type': 'unknown', 'id': 'DIRECT'}, {'type': 'node', 'id': 'DIRECT', 'extra': True})):
                chain = {'entry': reference, 'exit': reference, edge: value}
                call('H754', ['profiles', 'other', 'proxy_groups'], [{**group, 'type': 'chain', 'chain': chain}],
                     '代理链引用必须是 node/group type 与非空 id', 'chain/' + edge + '/' + str(i))
            chain = {'entry': reference, 'exit': reference, edge: {'type': 'group', 'id': 'missing-synthetic'}}
            call('H755', ['profiles', 'other', 'proxy_groups'], [{**group, 'type': 'chain', 'chain': chain}],
                 '配置 ' + original['profiles']['other']['name'] + ': 代理链引用的策略组不存在：missing-synthetic',
                 'chain/' + edge + '/missing-group', 409)
        for i, value in enumerate((False, 1, [], {})):
            call('H756', ['profiles', 'other', 'proxy_groups'], [{**group, 'source': value}],
                 '策略组 source 必须是字符串', 'source-type/' + str(i))
        # The earlier shape guard shadows these two specified helper returns.
        # Preserve their public rejection as partial observation, not proof
        # that the later return ran.
        for case, value in [('H757', {}), ('H758', [None])]:
            call('H182', ['profiles', 'other', 'proxy_groups'], [{**group, 'proxies_order': value}],
                 '策略组 proxies_order 必须是含 type/id 字符串的对象数组', case + '/earlier-shape-guard')
        for i, value in enumerate(('', ' ', 1, False)):
            def install_node(data, value=value):
                data['shared']['nodes'] = [{**NODE, 'params': {'dialer-proxy': value}}]
            call('H759', ['profiles', 'other', 'proxy_groups'], [{**group, 'manual_nodes': [NODE['id']]}],
                 '原始 dialer-proxy 必须是非空名称', 'raw-dialer/' + str(i), extra=install_node)
        def missing_target(data):
            data['shared']['nodes'] = [{**NODE, 'params': {'dialer-proxy': 'missing-target'}}]
        call('H760', ['profiles', 'other', 'proxy_groups'], [{**group, 'manual_nodes': [NODE['id']]}],
             '配置 ' + original['profiles']['other']['name'] + ': 原始拨号代理目标不存在或名称不唯一：missing-target',
             'raw-dialer/missing-target', 409, extra=missing_target)


GROUPS = {'repository-resource-closure-guards': guards}
