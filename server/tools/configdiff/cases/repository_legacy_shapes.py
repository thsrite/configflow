"""Schema-2 import reaches shape guards before migration remaps resources."""
import copy

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.groups import NODES
from cases.pull_routes import SourceStable

GROUP = {'id': 'legacy-group', 'name': '旧策略🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}


def document(profile):
    return {'schema_version': 2, 'system': {'schema_version': 2,
        'profiles': [{'id': 'default', 'name': '合成旧空间🧪'}], 'system_config': {}, 'backup': {}, 'agents': []},
        'profiles': {'default': profile}}


def guards(binary, recorder):
    vectors = []
    for key in ('nodes', 'proxy_groups'):
        vectors += [('H176', {key: {}}, key + ' 必须是数组'),
                    ('H177', {key: [None]}, '资源必须是 JSON 对象')]
    for field in ('id', 'name'):
        for value in (None, '', ' ', 1):
            vectors.append(('H178', {'proxy_groups': [{**GROUP, field: value}]}, '资源 ' + field + ' 必须是非空字符串'))
    vectors += [('H179', {'nodes': [{**NODES[0], 'params': None}]}, '节点 params 必须是对象，不能为 null'),
                ('H180', {'nodes': [{**NODES[0], 'proxy_string': 1}]}, '节点 proxy_string 必须是字符串')]
    for field in ('manual_nodes', 'include_groups', 'proxies', 'subscriptions', 'aggregations', 'use'):
        for value in (None, [1], [' ']):
            vectors.append(('H181', {'proxy_groups': [{**GROUP, field: value}]}, '策略组 ' + field + ' 必须是字符串数组'))
    for value in ({}, [None], [{'type': 1, 'id': 'n-a'}], [{'type': 'node', 'id': None}]):
        vectors.append(('H182', {'proxy_groups': [{**GROUP, 'proxies_order': value}]},
            '策略组 proxies_order 必须是含 type/id 字符串的对象数组'))
    vectors += [('H817', [], '旧配置内容必须是对象'),
        ('H819', {'nodes': [{**NODES[0], 'subscription_id': 'missing'}]}, '旧配置 default 的节点引用的订阅不存在'),
        ('H820', {'nodes': [{**NODES[0], 'params': {'dialer-proxy': 1}}]}, '原始 dialer-proxy 必须是非空名称'),
        ('H821', {'nodes': [{**NODES[0], 'params': {'dialer-proxy': 'missing'}}]}, '旧配置 default 的原始拨号目标不存在或名称不唯一：missing'),
        ('H828', {'resource_refs': {}}, '旧配置包含不支持的中间格式字段；未修改原数据'),
        ('H829', {'proxy_groups': [{**GROUP, 'manual_nodes': ['missing']}]}, '旧配置 default 引用的本地资源不存在：nodes/missing')]
    with Source(b'rejected legacy import never fetches sources') as source, Pair(binary, recorder) as pair:
        for number, (case, profile, expected) in enumerate(vectors):
            pair.request(case, 'POST /api/config/import', body=json_body(document(copy.deepcopy(profile))),
                expect=400, writes=True, interaction=SourceStable(pair, source), check=message(expected),
                variant='schema2-before-resource-remap/' + str(number))


GROUPS = {'repository-legacy-pre-migration-shape-guards': guards}
