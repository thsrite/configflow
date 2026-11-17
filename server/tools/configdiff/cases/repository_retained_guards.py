"""Legacy collections, shared shapes and retained router publication receipts."""
import copy
import re

from compare import differences, Difference
from snapshots import ImportSnapshot
from cases.helpers import profile_policy
from cases.subscriptions import require

from harness import Pair, json_body
from sources import Source
from cases.config_backup import message, seed_document
from cases.groups import NODES, CHAIN
from cases.pull_routes import SourceStable


def collections(binary, recorder):
    with Source(b'rejected source shapes never download') as source, Pair(binary, recorder) as pair:
        original = seed_document(pair)
        for kind in ('nodes', 'subscriptions', 'subscription_aggregations', 'rule_library'):
            for number, value in enumerate((None, {}, [None])):
                pair.request('H830', 'POST /api/config/import', body=json_body({kind: value}), expect=400, writes=True,
                    interaction=SourceStable(pair, source), check=message(kind + ' 必须是资源对象数组'),
                    variant='single-legacy-resource-shape/' + kind + '/' + str(number))
        for number, (field, value, expected) in enumerate([
            ('params', None, '节点 params 必须是对象，不能为 null'),
            ('proxy_string', 1, '节点 proxy_string 必须是字符串'),
            ('name', '', '资源 name 必须是非空字符串')]):
            data = copy.deepcopy(original)
            data['shared']['nodes'] = [{**NODES[0], field: value}]
            pair.request('H839', 'POST /api/config/import', body=json_body(data), expect=400, writes=True,
                interaction=SourceStable(pair, source), check=message(expected), variant='shared-shape-before-profile-resolution/' + str(number))


def chains(binary, recorder):
    chain = {'id': 'retained-chain', 'name': '导入链🧪', 'type': 'chain', 'chain': CHAIN}
    variants = [
        ('disabled-entry', [{**NODES[0], 'enabled': False}, NODES[1]], [chain], '代理链依赖不存在或未启用', 'H260'),
        ('subscription-entry', [{**NODES[0], 'subscription_id': 'retained-sub'}, NODES[1]], [chain],
         '代理链仅支持共享手动节点，订阅请通过策略组使用', 'H261'),
        ('same-node', NODES, [{**chain, 'chain': {'entry': CHAIN['entry'], 'exit': CHAIN['entry']}}],
         '代理链前置与落地不能是同一资源', 'H262'),
        ('mixed-ordinary-sources', NODES, [{**chain, 'include_all': True}], '代理链不能同时配置普通策略组来源', 'H263'),
        ('node-group-same-name', NODES, [{**chain, 'name': NODES[0]['name']}],
         '拨号代理要求代理/策略组名称非空、唯一且不与内置策略冲突', 'H184'),
        ('disabled-group-with-a-chain-present', NODES, [chain,
            {'id': 'off-group', 'name': '已停用组', 'type': 'select', 'manual_nodes': ['DIRECT'], 'enabled': False},
            {'id': 'active-group', 'name': '引用组', 'type': 'select', 'include_groups': ['off-group']}],
         '策略组引用不存在或未启用', 'H257')]
    with Source(b'rejected chain imports never fetch or push') as source, Pair(binary, recorder) as pair:
        original = seed_document(pair)
        for variant, nodes, groups, expected, _helper in variants:
            data = copy.deepcopy(original)
            data['shared']['nodes'] = copy.deepcopy(nodes)
            data['shared']['subscriptions'] = [{'id': 'retained-sub', 'name': '合成订阅',
                'url': source.url + '/unused', 'enabled': True, 'interval': 0}]
            data['profiles']['other']['proxy_groups'] = copy.deepcopy(groups)
            pair.request('H846', 'POST /api/config/import', body=json_body(data), expect=409, writes=True,
                interaction=SourceStable(pair, source),
                check=message('配置 ' + original['profiles']['other']['name'] + ': ' + expected), variant='resolved-chain/' + variant)


def rejected_import_errors(before, after):
    """Keep the store exact, permitting only one faithful recovery snapshot."""
    result = differences(before['database'], after['database'], 'rejected-import.database')
    new = set(after['files']) - set(before['files'])
    pattern = r'migrations/[0-9]{8}T[0-9]{6}Z-[a-f0-9]{32}/import/configflow.sqlite3'
    if len(new) != 1 or not all(re.fullmatch(pattern, path) for path in new):
        return result + [Difference('rejected-import.files', 'expected-one-import-snapshot', 1, sorted(new))]
    path = next(iter(new))
    result += differences(before['database'], after['files'][path].get('content'), 'rejected-import.snapshot')
    # Snapshot permissions still participate in the ordinary cross-file comparison.
    retained = {key: value for key, value in after['files'].items() if key != path}
    return result + differences(before['files'], retained, 'rejected-import.files')


class RejectedImport(SourceStable):
    def after(self, index, reply):
        self.after_values[index] = self.snapshot(index)
        self.exchange.after(index, reply)
        if index == 0:
            errors = rejected_import_errors(self.before_values[0], self.after_values[0])
            require(not errors, 'rejected import adds only one exact pre-request SQL snapshot: ' + repr(errors[:4]))

    def compare(self, policy):
        return rejected_import_errors(self.before_values[1], self.after_values[1]) + self.exchange.compare(policy)


def publication_receipts(binary, recorder):
    for field, label in [('ros_policy_state', '策略路由'), ('dns_guard_state', '强制 DNS 规则'),
            ('ipv6_fakeip_state', 'IPv6 Fake-IP 路由'), ('ipv6_routes_state', 'IPv6 规则集路由'),
            ('ipv6_pd_guard_state', 'IPv6 前缀变化保护')]:
        with Source(b'retained router receipt guard never contacts its device') as source:
            router = {'id': 'retained-router', 'name': '带合成发布记录的路由器🧪', 'type': 'routeros', 'endpoint': source.url,
                'username': 'synthetic-user', 'password': 'synthetic-password', 'auto_rule_sync': False,
                field: {'synthetic': 'retained-publication-receipt'}}
            with Pair(binary, recorder, options={'other_profile': {'routers': {'items': [router]}}}) as pair:
                pair.policy = profile_policy(pair)
                original = seed_document(pair)
                for operation in ('delete-router', 'remove-receipt', 'change-type', 'change-endpoint'):
                    data = copy.deepcopy(original)
                    record = data['profiles']['other']['routers']['items'][0]
                    if operation == 'delete-router':
                        data['profiles']['other']['routers']['items'] = []
                    elif operation == 'remove-receipt':
                        record.pop(field)
                    elif operation == 'change-type':
                        record['type'] = 'ikuai'
                    else:
                        record['endpoint'] = source.url + '/another-device'
                    snapshot = ImportSnapshot(pair)
                    pair.request('H764', 'POST /api/config/import', body=json_body(data), expect=409, writes=True,
                        interaction=RejectedImport(pair, source), capture=snapshot.capture,
                        check=message('路由器仍有已发布的' + label + '；请先发布并核对撤回，再修改类型、地址或删除发布记录'),
                        variant=field + '/' + operation)


GROUPS = {'repository-legacy-collections-and-shared-shape-errors': collections,
          'repository-resolved-chain-import-errors': chains,
          'repository-retained-router-publication-guards': publication_receipts}
