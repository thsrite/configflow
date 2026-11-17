"""Retained values that pass repository loading but fail inside public renders."""
from harness import Pair
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable


def merged(binary, recorder):
    with Source(b'bad saved Mihomo text fails before every source fetch') as source, Pair(binary, recorder,
            options={'profile': {'mihomo': {'custom_config': 1}}}) as pair:
        pair.request('R222-F12', 'GET /api/subscriptions/proxies', expect=500, writes=True,
            interaction=SourceStable(pair, source), check=message('获取订阅代理列表失败'),
            variant='retained-Mihomo-text-integer-reaches-merged-generic-catch')


def oxidns(binary, recorder):
    from cases.config_backup import seed_document, import_request
    from cases.helpers import profile_policy
    with Source(b'bad retained origin fails inside OxiDNS URL emission') as source, Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        data = seed_document(pair)
        data['system']['system_config']['server_domain'] = 1
        # Seeding this value before process startup fails in load_config, which
        # is NOT the intended render branch. Public import after startup is the
        # supported retained-state boundary and passes repository validation.
        import_request(pair, 'R028-B03', data, expected_token=data['system']['system_config']['rule_proxy_token'],
            variant='runtime-origin-import-after-successful-startup')
        for case, route, path in [('R032-F10', 'GET /api/config/oxidns', '/api/config/oxidns'),
                ('R025-F10', 'GET /api/config/<profile_id>/oxidns', '/api/config/default/oxidns')]:
            pair.request(case, route, path, expect=500, writes=True, interaction=SourceStable(pair, source),
                variant='retained-origin-integer-reaches-runtime-non-ValueError')


def emitted_names(binary, recorder):
    from cases.groups import NODES
    for variant, second_name in [('duplicate-emitted-name', 'same-emitted-name'), ('builtin-emitted-name', 'DIRECT')]:
        nodes = [{**NODES[0], 'params': {'name': second_name, 'dialer-proxy': 'DIRECT'}},
                 {**NODES[1], 'params': {'name': second_name}}]
        options = {'shared': {'nodes': nodes}, 'profile': {'proxy_groups': [
            {'id': 'emitted-name-root', 'name': '逻辑名称保持唯一', 'type': 'select', 'manual_nodes': ['n-a', 'n-b']}]}}
        with Source(b'logical names are valid but converted names collide') as source, Pair(binary, recorder, options=options) as pair:
            for route, path in [('POST /api/generate/mihomo/preview', '/api/generate/mihomo/preview'),
                    ('POST /api/profiles/<profile_id>/generate/mihomo/preview', '/api/profiles/default/generate/mihomo/preview')]:
                pair.request('H208', route, path, body='{}', expect=400, writes=True,
                    interaction=SourceStable(pair, source), check=message('生成的代理/策略组名称重复或与内置策略冲突'), variant=variant)


GROUPS = {'merged-provider-real-retained-config-generic-failure': merged,
          'oxidns-pull-real-retained-origin-runtime-failure': oxidns,
          'generator-real-converted-parameter-name-collisions': emitted_names}
