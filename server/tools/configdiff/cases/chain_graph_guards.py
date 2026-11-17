"""Import-time graph guards and actual generated-chain failures via public HTTP."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import seed_document, message
from cases.groups import NODES, CHAIN
from cases.pull_routes import SourceStable


def guards(binary, recorder):
    chain = {'id': 'guard-chain', 'name': '真实图拒绝🧪', 'type': 'chain', 'chain': copy.deepcopy(CHAIN)}
    entry = {'id': 'entry-group', 'name': '前置来源', 'type': 'select'}
    exit_group = {'id': 'exit-group', 'name': '落地来源', 'type': 'select'}
    def group_chain(endpoint, group):
        value = copy.deepcopy(chain)
        value['chain'][endpoint] = {'type': 'group', 'id': group['id']}
        return [value, group]
    variants = [
        ('H191', 'entry-include-all', NODES, group_chain('entry', {**entry, 'include_all': True, 'manual_nodes': ['n-a']}), [],
         '拨号拓扑不支持 include-all 隐式全局来源，请显式选择来源'),
        ('H192', 'entry-unknown-use', NODES, group_chain('entry', {**entry, 'use': ['没有这个来源']}), [],
         '策略组 use 引用不存在或名称不唯一'),
        ('H193', 'entry-disabled-subscription', NODES, group_chain('entry', {**entry, 'subscriptions': ['graph-sub']}), [],
         '拨号策略组来源不存在或未启用'),
        ('H194', 'entry-empty', NODES, group_chain('entry', entry), [], '拨号策略组不能为空'),
        ('H197', 'exit-group-DIRECT', NODES, group_chain('exit', {**exit_group, 'manual_nodes': ['DIRECT']}), [],
         '代理链落地组不能包含 DIRECT/REJECT 等无法施加前置代理的候选'),
        ('H198', 'exit-disabled-member', [NODES[0], {**NODES[1], 'enabled': False}],
         group_chain('exit', {**exit_group, 'manual_nodes': ['n-b']}), [], '代理链落地候选不存在或未启用'),
        ('H199', 'exit-subscription-member', [NODES[0], {**NODES[1], 'subscription_id': 'graph-sub'}],
         group_chain('exit', {**exit_group, 'manual_nodes': ['n-b']}), [], '订阅节点请通过策略组 Provider 使用'),
        ('H200', 'exit-empty-aggregation', NODES, group_chain('exit', {**exit_group, 'aggregations': ['graph-agg']}),
         [{'id': 'graph-agg', 'name': '空落地聚合', 'nodes': [], 'subscriptions': []}], '代理链落地聚合不能为空'),
        ('H202', 'entry-empty-aggregation', NODES, group_chain('entry', {**entry, 'aggregations': ['graph-agg']}),
         [{'id': 'graph-agg', 'name': '空前置聚合', 'nodes': [], 'subscriptions': []}], '拨号策略组聚合来源不能为空'),
        ('H203', 'raw-two-node-cycle', [{**NODES[0], 'params': {'dialer-proxy': NODES[1]['name']}},
         {**NODES[1], 'params': {'dialer-proxy': NODES[0]['name']}}], [], [], '拨号代理依赖存在循环'),
    ]
    with Source(b'invalid graph imports never contact sources') as source, Pair(binary, recorder) as pair:
        original = seed_document(pair)
        for case, variant, nodes, groups, aggregations, expected in variants:
            data = copy.deepcopy(original)
            data['shared'].update(nodes=copy.deepcopy(nodes), subscription_aggregations=copy.deepcopy(aggregations),
                subscriptions=[{'id': 'graph-sub', 'name': '停用来源', 'url': source.url + '/unused', 'enabled': False, 'interval': 0}])
            # Shared manual nodes enter a resolved profile only when selected.
            data['profiles']['other']['proxy_groups'] = copy.deepcopy(groups or [
                {'id': 'raw-roots', 'name': '原始拨号根', 'type': 'select', 'manual_nodes': ['n-a', 'n-b']}])
            pair.request(case, 'POST /api/config/import', body=json_body(data), expect=409, writes=True,
                interaction=SourceStable(pair, source), check=message('配置 ' + original['profiles']['other']['name'] + ': ' + expected),
                variant=variant)


GROUPS = {'repository-real-dialer-dependency-graph-guards': guards}


def emitted(binary, recorder):
    chain = {'id': 'emitted-chain', 'name': '真实生成链', 'type': 'chain', 'chain': copy.deepcopy(CHAIN)}
    cases = [
        ('H205', 'native-chain-exclude-invalid', {'shared': {'nodes': NODES}, 'profile': {'proxy_groups': [
            {**chain, 'chain': {**CHAIN, 'exit': {'type': 'group', 'id': 'native-exclude'}}},
            {'id': 'native-exclude', 'name': '原生排除参数', 'type': 'select', 'manual_nodes': ['n-b'], 'exclude-filter': '['}]}},
            'Provider 正则过滤无效或不受支持'),
        ('H215', 'ordinary-member-conversion-omitted', {'shared': {'nodes': [
            {**NODES[0], 'params': {'dialer-proxy': 'DIRECT'}}, {**NODES[1], 'type': ''}]},
            'profile': {'proxy_groups': [{'id': 'missing-emitted', 'name': '未生成候选', 'type': 'select', 'manual_nodes': ['n-a', 'n-b']}]}},
            '拨号拓扑中的策略组引用了未生成的代理或策略组'),
        ('H217', 'disabled-provider-definition', {'shared': {'nodes': [{**NODES[0], 'params': {'dialer-proxy': 'DIRECT'}}],
            'subscriptions': [{'id': 'disabled-emitted-sub', 'name': '停用来源', 'url': '', 'enabled': False, 'interval': 0}]},
            'profile': {'proxy_groups': [{'id': 'disabled-provider-group', 'name': '缺失Provider', 'type': 'select',
                'manual_nodes': ['n-a'], 'subscriptions': ['disabled-emitted-sub']}]}}, '策略组引用了未生成的 Provider'),
        ('H219', 'dialer-target-filtered-to-empty', {'shared': {'nodes': [
            {**NODES[0], 'params': {'dialer-proxy': '被全部过滤的前置'}}, NODES[1]]},
            'profile': {'proxy_groups': [{'id': 'all-excluded', 'name': '被全部过滤的前置', 'type': 'select',
                'manual_nodes': ['n-b'], 'exclude-filter': '.*'},
                {'id': 'dialer-activation', 'name': '实际拓扑根', 'type': 'select', 'manual_nodes': ['n-a']}]}},
            '拨号策略组过滤后没有可用候选'),
        ('H204', 'empty-type-participating-entry', {'shared': {'nodes': [{**NODES[0], 'type': ''}, NODES[1]]},
            'profile': {'proxy_groups': [chain]}}, '拨号拓扑参与节点转换失败，不能省略节点'),
        ('H264', 'provider-name-collision', {'shared': {'nodes': NODES,
            'subscriptions': [{'id': 'same-sub', 'name': '同名Provider', 'url': '', 'enabled': True, 'interval': 0}],
            'subscription_aggregations': [{'id': 'same-agg', 'name': '同名Provider', 'nodes': [], 'subscriptions': []}]},
            'profile': {'proxy_groups': [chain, {'id': 'provider-roots', 'name': '实际选择来源', 'type': 'select',
                'subscriptions': ['same-sub'], 'aggregations': ['same-agg']}]}}, '订阅与聚合 Provider 名称必须唯一'),
        ('H265', 'unrelated-follow-cycle', {'shared': {'nodes': NODES}, 'profile': {'proxy_groups': [chain,
            {'id': 'cycle-a', 'name': '循环甲', 'type': 'select', 'follow_group': 'cycle-b'},
            {'id': 'cycle-b', 'name': '循环乙', 'type': 'select', 'follow_group': 'cycle-a'}]}},
            '跟随策略组依赖不存在或存在循环'),
    ]
    for case, variant, options, expected in cases:
        with Source(b'generator rejection must precede every download') as source, Pair(binary, recorder, options=options) as pair:
            for route, path in [('POST /api/generate/mihomo/preview', '/api/generate/mihomo/preview'),
                    ('POST /api/profiles/<profile_id>/generate/mihomo/preview', '/api/profiles/default/generate/mihomo/preview')]:
                pair.request(case, route, path, body='{}', expect=400, writes=True,
                    interaction=SourceStable(pair, source), check=message(expected), variant=variant)


GROUPS['generator-real-provider-name-and-follow-chain-guards'] = emitted


def unsupported_clients(binary, recorder):
    import yaml
    from substore import SubStore
    from cases.node_parse_lines import Conversion
    opaque = 'synthetic-opaque-converter-node'
    converted = yaml.safe_dump({'proxies': [{'name': 'converted-inner', 'type': 'socks5',
        'server': '192.0.2.44', 'port': 1080, 'dialer-proxy': 'DIRECT'}]}).encode()
    for service, client in [('surge', 'Surge'), ('loon', 'Loon'), ('mihomo', 'Mihomo')]:
        for opaque_source in (False, True):
            if service == 'mihomo' and not opaque_source:
                continue
            node = {**NODES[0], **({'proxy_string': opaque} if opaque_source else {'params': {'dialer-proxy': 'DIRECT'}})}
            payload = converted.replace(b'DIRECT', b'missing-converted-dialer') if service == 'mihomo' else converted
            with SubStore(payload) as store:
                opts = {'shared': {'nodes': [node]}, 'profile': {'proxy_groups': [
                    {'id': 'raw-client-root', 'name': '实际拨号来源', 'type': 'select', 'manual_nodes': ['n-a']}]},
                    'environment': {'SUB_STORE_URL': store.url}}
                with Pair(binary, recorder, options=opts) as pair:
                    # Each opaque conversion is memoized by the real process;
                    # use one request per fresh pair so the full wire is observed.
                    expected = (client + ' 暂不支持转换结果中的 dialer-proxy' if opaque_source else
                        client + ' 暂不支持 Mihomo 拨号代理拓扑，请使用 Mihomo 或显式移除拨号配置')
                    if service == 'mihomo':
                        expected = '生成的 dialer-proxy 引用了不存在的代理或策略组'
                    pair.request('H216' if service == 'mihomo' else 'H222' if opaque_source else 'H221', 'POST /api/generate/' + service + '/preview',
                        body='{}', expect=400, writes=True, check=message(expected),
                        interaction=Conversion(pair, store, opaque) if opaque_source else SourceStable(pair, store),
                        variant=service + ('/converted-opaque-dialer' if opaque_source else '/raw-stored-dialer'))


GROUPS['generator-real-unsupported-raw-and-converted-dialers'] = unsupported_clients
