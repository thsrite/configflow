"""Reachable routing guards after the common editor's earlier type checks."""
import copy

from harness import Pair, json_body
from cases.config_backup import message
from cases.oxidns_routes import SCOPES, OPTIONS, BASE, api
from cases.setup import Stable

IDENTITY = 'native-route-rule'
MISSING = '所选规则不在当前空间的可用规则库中，请刷新后重试。'


def guards(binary, recorder):
    action = {'action': 'refused'}
    rows = [
        ('H378', {'direct_rulesets': None}, 'direct_rulesets 必须为规则 ID 列表'),
        ('H378', {'direct_rulesets': [1]}, 'direct_rulesets 必须为规则 ID 列表'),
        ('H379', {'direct_rules': [IDENTITY]}, MISSING),
        ('H379', {'direct_rulesets': ['missing']}, MISSING),
        ('H380', {'rule_actions': []}, 'rule_actions 必须为以规则 ID 为键的对象'),
        ('H381', {'rule_actions': {'missing': action}}, MISSING),
        ('H382', {'rule_dns_follow': [1]}, 'rule_dns_follow 必须为规则 ID 数组'),
        ('H383', {'rule_dns_follow': ['missing']}, MISSING),
        ('H384', {'direct_rulesets': [IDENTITY], 'proxy_rulesets': [IDENTITY]}, '同一规则不能同时选择直连和代理。'),
        ('H385', {'direct_rulesets': [IDENTITY], 'rule_actions': {IDENTITY: action}}, '同一规则不能同时选择多种 DNS 处理。'),
        ('H387', {'rule_actions': {IDENTITY: action}, 'rule_dns_follow': [IDENTITY]},
         '规则 ' + IDENTITY + ' 已设置 DNS 处理「拒绝」，不能同时跟随策略'),
        # null deliberately passes _validate_value's optional-value check and
        # reaches these later guards. Objects/strings would fail earlier.
        ('H389', {'custom_matches': None}, '自定义匹配必须为列表。'),
        ('H390', {'custom_matches': [1]}, '每条自定义匹配必须为对象。'),
        ('H391', {'custom_matches': [{'enabled': False}]}, '常用设置直接编辑执行中的匹配条目，不支持 enabled: false；请移除该条目或改为启用。'),
        ('H392', {'custom_matches': [{'matches': [], 'exec': 'accept'}]}, '自定义匹配需填写非空条件与执行动作。'),
        ('H392', {'custom_matches': [{'matches': ['qname example.com'], 'exec': ''}]}, '自定义匹配需填写非空条件与执行动作。'),
        ('H398', {'default_forward': None}, '默认转发目标无效。'),
        ('H401', {'aggregate_rules': None}, '合并相邻规则必须为布尔值。'),
        ('H402', {'custom_match_position': None}, '自定义匹配执行顺序必须为 head 或 tail。')]
    for template, prefix in SCOPES:
        fixture = copy.deepcopy(OPTIONS)
        fixture['profile']['rule_configs'].append({**fixture['profile']['rule_configs'][0], 'id': IDENTITY + '-second'})
        fixture['profile']['rule_configs'].append({**fixture['profile']['rule_configs'][0],
            'id': IDENTITY + '-disabled', 'enabled': False})
        with Pair(binary, recorder, options=fixture) as pair:
            start = api(pair, template, prefix, 'editor', 'S01', {'settings': {'worker_threads': 2,
                'direct_rulesets': [IDENTITY, IDENTITY + '-second'],
                'custom_matches': [{'matches': ['qname synthetic.example'], 'exec': 'goto china_dns'}]}, 'base_url': BASE},
                variant='routing-late-guards-two-adjacent-rules')['settings']
            for number, (case, changes, expected) in enumerate(rows):
                pair.request(case, 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                    body=json_body({'settings': start, 'changes': changes, 'base_url': BASE}), expect=400,
                    writes=True, interaction=Stable(pair), check=message(expected), variant='late-routing/' + str(number))
            changed = api(pair, template, prefix, 'common-settings/apply', 'S03', {'settings': start,
                'changes': {'rule_actions': {IDENTITY: action}}, 'base_url': BASE},
                variant='late-routing/existing-action-before-selector-conflict')['settings']
            pair.request('H386', 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                body=json_body({'settings': changed, 'changes': {'direct_rulesets': [IDENTITY],
                    'rule_actions': {IDENTITY: action}}, 'base_url': BASE}), expect=400, writes=True,
                interaction=Stable(pair), check=message('同一规则不能同时选择多种 DNS 处理。'), variant='old-action-conflicts-with-new-selector')
            disabled = IDENTITY + '-disabled'
            pair.request('H388', 'POST ' + template + '/common-settings/apply', prefix + '/common-settings/apply',
                body=json_body({'settings': {**start, 'rule_dns_parked': {disabled: {'action': 'direct'}}},
                    'changes': {'rule_dns_follow': [disabled]}, 'base_url': BASE}), expect=400, writes=True,
                interaction=Stable(pair), check=message('规则 ' + disabled + ' 已暂存停用前的 DNS 处理，不能同时跟随策略'),
                variant='inactive-rule-parked-action-conflicts-with-follow')
            for case, patch, error in [('H435', {'template_settings': []}, 'template_settings 必须为模板参数对象'),
                    ('H436', {'template_settings': {}, 'resolutions': {'/runtime/worker_threads': 'discard'}},
                     'resolutions 必须按参数路径选择 current 或 template')]:
                pair.request(case, 'POST ' + template + '/template-preview', prefix + '/template-preview',
                    body=json_body({'settings': start, 'base_url': BASE, **patch}), expect=400, writes=True,
                    interaction=Stable(pair), check=message(error), variant='template-preview-helper-guard')


GROUPS = {'oxidns-common-late-routing-guards': guards}
