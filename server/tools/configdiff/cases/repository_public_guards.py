"""Reachable repository guards through profile edits/imports and library POST."""
from compare import database
from composition import cache_name
from harness import Pair, json_body
from sources import Source
from cases.config_backup import message
from cases.helpers import profile_policy
from cases.pull_routes import SourceStable
from cases.rule_library import DOMAIN


def metadata(binary, recorder):
    with Source(b'profile validation must not download') as source, Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        for index, value in enumerate((True, 1, [1], {'synthetic': True})):
            pair.request('H771', 'POST /api/profiles', body=json_body({'id': 'rejected-profile', 'name': value}),
                expect=400, writes=True, check=message('配置名称不能为空'),
                interaction=SourceStable(pair, source), variant='metadata-name-type/' + str(index))
        for index, value in enumerate(([1], 'synthetic', True, 1)):
            pair.request('H777', 'PUT /api/profiles/<profile_id>', '/api/profiles/other', body=json_body(value),
                expect=400, writes=True, check=message('只能修改配置名称和说明'),
                interaction=SourceStable(pair, source), variant='metadata-update-nonobject/' + str(index))
        for field in ('name', 'description'):
            for index, value in enumerate((None, False, 0, [], {})):
                pair.request('H778', 'PUT /api/profiles/<profile_id>', '/api/profiles/other', body=json_body({field: value}),
                    expect=400, writes=True, check=message('配置名称和说明必须是字符串，名称不能为空'),
                    interaction=SourceStable(pair, source), variant='metadata-string/' + field + '/' + str(index))


def removed_policy(binary, recorder):
    group = {'id': 'guard-group', 'name': '引用策略🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}
    rule = {'id': 'guard-rule', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'selected.example', 'policy': group['name']}
    with Source(b'independent import must not download') as source, Pair(binary, recorder,
            options={'profile': {'proxy_groups': [group], 'rule_configs': [rule]}}) as pair:
        pair.policy = profile_policy(pair)
        name = database(pair.dirs[0])['fields']['profile/default/name']['value']
        pair.request('H785', 'POST /api/profiles/<profile_id>/import', '/api/profiles/default/import',
            body=json_body({'proxy_groups': []}), expect=409, writes=True,
            check=message('配置 ' + name + ' 的规则仍引用被删除策略组：' + group['name']),
            interaction=SourceStable(pair, source), variant='independent-import-removes-referenced-policy')


def occupied_cache(binary, recorder):
    for suffix in ('', '.bak'):
        library = {**DOMAIN, 'id': 'unregistered-cache-identity'}
        with Source(b'content import must not download') as source, Pair(binary, recorder) as pair:
            for directory in pair.dirs:
                path = directory / (cache_name(library) + suffix)
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(b'keep.synthetic.example\n')
            pair.request('H794', 'POST /api/rule-library', body=json_body(library), expect=409, writes=True,
                check=message('规则缓存已存在，请使用新的规则 ID'), interaction=SourceStable(pair, source),
                variant='orphan-existing-cache/' + ('backup-only' if suffix else 'main'))


GROUPS = {'repository-public-metadata-guards': metadata, 'repository-removed-policy-guard': removed_policy,
          'repository-existing-cache-guard': occupied_cache}
