"""Complete import invariants not reachable by ordinary single-resource forms."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import seed_document, message
from cases.groups import NODES, CHAIN
from cases.helpers import profile_policy
from cases.pull_routes import SourceStable
from cases.rules import RULE


def guards(binary, recorder):
    with Source(b'rejected import must not fetch or push') as source, Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        original = seed_document(pair)
        name = original['profiles']['other']['name']
        def call(case, profile, text, *, status=400, variant='', shared=None, system=None):
            data = copy.deepcopy(original)
            data['profiles']['other'].update(profile)
            if shared:
                data['shared'].update(shared)
            if system:
                data['system'].update(system)
            pair.request(case, 'POST /api/config/import', body=json_body(data), expect=status, writes=True,
                         interaction=SourceStable(pair, source), check=message(text), variant=variant)
        group = {'id': 'edge-a', 'name': '导入策略', 'type': 'select', 'manual_nodes': ['DIRECT']}
        call('R028-F10', {'id': 'mismatched-content-id'}, '配置内容与 ID 不一致', variant='current-schema-profile-key-ID-mismatch')
        call('R028-F12', {}, 'Agent 合成引用 绑定的配置不存在', variant='agent-binds-nonexistent-profile',
             system={'agents': [{'id': 'absent-profile-agent', 'name': '合成引用', 'profile_id': 'missing-profile'}]})
        call('H855', {'proxy_groups': [group, {**group, 'id': 'edge-b'}]}, '策略组名称必须唯一，且不能使用 DIRECT 或 REJECT', variant='duplicate-name')
        for reserved in ('DIRECT', 'REJECT'):
            call('H855', {'proxy_groups': [{**group, 'name': reserved}]}, '策略组名称必须唯一，且不能使用 DIRECT 或 REJECT', variant='reserved/' + reserved)
        for field in ('include_groups', 'follow_group'):
            for target in ('edge-a', 'missing'):
                call('H859', {'proxy_groups': [{**group, field: [target] if field == 'include_groups' else target}]},
                     '配置 ' + name + ': 策略组“导入策略” 引用了无效的策略组：' + target, status=409, variant=field + '/' + target)
        chain = {'id': 'edge-chain', 'name': '导入链', 'type': 'chain', 'chain': CHAIN}
        call('H860', {'proxy_groups': [chain, {**group, 'follow_group': 'edge-chain'}]},
             '代理链请通过引用策略使用，不能作为跟随目标', shared={'nodes': NODES})
        for kind, target in [('unsupported', 'edge-a'), ('strategy', 'edge-a'), ('strategy', 'missing')]:
            call('H861', {'proxy_groups': [{**group, 'proxies_order': [{'type': kind, 'id': target}]}]},
                 '配置 ' + name + ': 策略组“导入策略” 的排序列表包含无效引用：' + target, status=409, variant=kind + '/' + target)
        call('H862', {'rule_configs': [{**RULE, 'itemType': 'unknown'}]}, '规则类型必须为 rule 或 ruleset')
        call('H863', {'rule_configs': [{'id': 'missing-source-rule', 'itemType': 'ruleset', 'library_rule_id': 'missing'}]},
             '规则集必须引用全局规则仓库，不能单独设置来源')
        for engine, label in [('mosdns', 'MosDNS'), ('oxidns', 'OxiDNS')]:
            for field in ('direct_rules', 'proxy_rulesets'):
                call('H870', {engine: {**original['profiles']['other'].get(engine, {}), field: ['missing']}},
                     '配置 ' + name + ': ' + label + ' ' + field + ' 仍引用不存在的规则 missing，请先取消选择', status=409,
                     variant=engine + '/' + field)
        for field in ('rule_actions', 'rule_dns_parked', 'rule_dns_follow'):
            for value in ('wrong-type', [1]):
                call('H871', {'oxidns': {field: value}}, 'OxiDNS ' + field + ' 必须以规则 ID 为键', variant=field + '/' + repr(value))
            call('H872', {'oxidns': {field: ['missing'] if field == 'rule_dns_follow' else {'missing': {'action': 'refused'}}}},
                 '配置 ' + name + ': OxiDNS DNS 处理仍引用不存在的规则 missing，请先取消', status=409, variant=field)
        for field in ('rule_actions', 'rule_dns_parked'):
            call('H873', {'rule_configs': [RULE], 'oxidns': {field: {'r-a': {'action': 'refused', 'extra': True}}}},
                 'OxiDNS ' + field + ' 中规则 r-a 的 DNS 处理无效：规则 DNS 处理「拒绝」不能包含其他参数', variant=field)
        call('H874', {'surge': {'custom_config': '', 'smart_groups': [{'group_id': 'missing'}]}},
             '配置 ' + name + ': Surge Smart 设置引用的策略组不存在', status=409)


GROUPS = {'repository-complete-import-edge-invariants': guards}
