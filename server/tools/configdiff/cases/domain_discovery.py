"""Domain-discovery settings and profile-owned ignore/website-note storage."""
import copy

from compare import database
from harness import Pair, json_body
from cases.config_backup import message
from cases.helpers import profile_policy
from cases.rule_library import DOMAIN
from cases.subscriptions import require

DIRECT = {**DOMAIN, 'id': 'discovery-direct', 'name': '域名直连🧪', 'behavior': 'classical',
          'content': '# keep comment\nDOMAIN,keep.example\n'}
PROXY = {**DOMAIN, 'id': 'discovery-proxy', 'name': '域名代理', 'content': '+.old.example\n'}
GROUP = {'id': 'discovery-group', 'name': '代理🧪', 'type': 'select', 'manual_nodes': ['DIRECT']}


def options():
    return {'shared': {'rule_library': [copy.deepcopy(DIRECT), copy.deepcopy(PROXY),
        {**DOMAIN, 'id': 'discovery-url', 'name': 'URL源', 'source_type': 'url', 'url': 'https://source.example/rules', 'content': ''},
        {**DOMAIN, 'id': 'discovery-ip', 'name': 'IP源', 'behavior': 'ipcidr', 'content': '192.0.2.0/24\n'}]},
        'profile': {'proxy_groups': [copy.deepcopy(GROUP)], 'rule_configs': [
            {'id': 'discovery-direct-ref', 'itemType': 'ruleset', 'library_rule_id': DIRECT['id'], 'policy': 'DIRECT'},
            {'id': 'discovery-proxy-ref', 'itemType': 'ruleset', 'library_rule_id': PROXY['id'], 'policy': GROUP['name']},
            {'id': 'discovery-match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': 'DIRECT'}],
            'domain_discovery': {'direct_ruleset': DIRECT['id'], 'proxy_ruleset': PROXY['id'], 'auto_probe': False, 'auto_apply': False}}}


def stored(pair, profile='default'):
    return database(pair.dirs[0])['fields']['profile/' + profile + '/domain_discovery']['value']


def settings(binary, recorder):
    with Pair(binary, recorder) as pair:
        pair.policy = profile_policy(pair)
        pair.request('R057-S01', 'GET /api/domain-discovery/settings', expect=200, writes=True)
        pair.request('R057-B02', 'GET /api/domain-discovery/settings', expect=200, writes=True,
                     check=lambda r: require(r.json()['policies'] == [], 'empty profile has no offered policies'))
    with Pair(binary, recorder, options=options()) as pair:
        pair.policy = profile_policy(pair)
        for case in ('S02',):
            pair.request('R057-' + case, 'GET /api/domain-discovery/settings', expect=200, writes=True,
                         check=lambda r: require(r.json()['direct']['active'] is True and r.json()['proxy']['active'] is True,
                                                 'both selected writable libraries are referenced before MATCH'))
        for case, body in [('S01', {'direct_ruleset': DIRECT['id'], 'proxy_ruleset': PROXY['id']}),
              ('S02', {'auto_probe': True, 'auto_apply': True}), ('S02', {'auto_probe': False, 'auto_apply': False}),
              ('S03', {'auto_apply_min_confidence': 80}), ('S04', {'auto_apply_min_confidence': 90}),
              ('S05', {'auto_apply_min_confidence': 95}), ('S06', {'region_probe_port': 18082}),
              ('S07', {'direct_ruleset': None, 'proxy_ruleset': None}), ('B01', {'region_probe_port': 1024}),
              ('B02', {'region_probe_port': 65535}), ('B03', {'synthetic_extension': 'must-ignore'}),
              ('B03', {'auto_apply_min_confidence': 90.0})] + [
              ('S07', {'direct_ruleset': value, 'proxy_ruleset': value}) for value in ('', False, 0, [], {})]:
            def check(reply):
                value = stored(pair)
                require('synthetic_extension' not in value, 'unknown settings are ignored rather than persisted')
                for key, expected in body.items():
                    if key != 'synthetic_extension':
                        if key.endswith('_ruleset') and not expected:
                            expected = None
                        require(type(value[key]) is type(expected) and value[key] == expected, 'saved setting preserves the Python-accepted scalar type/value')
            pair.request('R058-' + case, 'PUT /api/domain-discovery/settings', body=json_body(body), expect=200,
                         writes=True, check=check, variant=repr(body))
    vectors = [('F08', [], '请求必须是 JSON 对象'),
        ('F09', {'direct_ruleset': 'missing'}, '默认规则集必须是内容型、classical 或 domain 格式的规则库条目'),
        ('F09', {'direct_ruleset': 'discovery-url'}, '默认规则集必须是内容型、classical 或 domain 格式的规则库条目'),
        ('F09', {'direct_ruleset': 'discovery-ip'}, '默认规则集必须是内容型、classical 或 domain 格式的规则库条目'),
        ('F10', {'auto_probe': 1}, 'auto_probe 必须是布尔值'), ('F11', {'auto_apply': 'false'}, 'auto_apply 必须是布尔值'),
        ('F12', {'auto_apply_min_confidence': 85}, '自动采纳阈值只能是 80、90 或 95'),
        ('F13', {'region_check_enabled': 1}, 'region_check_enabled 必须是布尔值')]
    for case, body, expected in vectors:
        with Pair(binary, recorder, options=options()) as pair:
            pair.policy = profile_policy(pair)
            helper = {'F08': 'H045', 'F09': 'H046', 'F10': 'H047', 'F11': 'H047', 'F12': 'H048', 'F13': 'H049'}[case]
            for ident in ('R058-' + case, helper):
                pair.request(ident, 'PUT /api/domain-discovery/settings', body=json_body(body), expect=400,
                             writes=True, check=message(expected), variant=repr(body))
    for case, value in [('F14', True), ('F14', 18082.0), ('F14', '18082'), ('F15', 1023), ('F16', 65536), ('F17', 18082)]:
        opts = options()
        opts['profile']['mihomo'] = {'custom_config': 'mixed-port: 18082\n'}
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            for ident in ('R058-' + case, 'H051' if case == 'F17' else 'H050'):
                pair.request(ident, 'PUT /api/domain-discovery/settings', body=json_body({'region_probe_port': value}),
                             expect=400, writes=True, variant=repr(value))
    opts = options()
    opts['profile']['domain_discovery']['direct_ruleset'] = 'missing'
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy = profile_policy(pair)
        pair.request('R057-B01', 'GET /api/domain-discovery/settings', expect=200, writes=True,
                     check=lambda r: require(r.json()['direct']['problem'] == 'missing', 'stale selection is reported as a problem instead of silently cleared'))

    legacy = options()
    legacy['profile']['domain_discovery']['synthetic_legacy_extension'] = {'flag': False, 'values': []}
    with Pair(binary, recorder, options=legacy) as pair:
        pair.policy = profile_policy(pair)
        pair.request('R058-B03', 'PUT /api/domain-discovery/settings', body='{}', expect=200, writes=True,
                     check=lambda r: require('synthetic_legacy_extension' not in stored(pair), 'settings rewrite normalizes away unknown stored legacy fields'),
                     variant='stored-legacy-extension')


def ignored(binary, recorder):
    for rid, method in [(50, 'POST'), (49, 'DELETE')]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.policy = profile_policy(pair)
            for case, values in [('S01', ['example.com']), ('S02', ['example.com', 'example.com']),
                                 ('B02', ['xn--fiqs8s.example'])]:
                pair.request(f'R{rid:03}-' + case, method + ' /api/domain-discovery/ignore', body=json_body({'values': values}),
                             expect=200, writes=True, check=lambda r: require(len(r.json()['ignored']) == len(set(r.json()['ignored'])), 'ignore set keeps first occurrence without duplicate rows'))
            if method == 'DELETE':
                pair.request('R049-S03', 'DELETE /api/domain-discovery/ignore', body=json_body({'values': ['missing.example']}), expect=200, writes=True)
        capacity = ['x' + str(i) + '.example' for i in range(2000)]
        opts = options()
        if method == 'DELETE':
            opts['profile']['domain_discovery']['ignored'] = capacity
        with Pair(binary, recorder, options=opts) as pair:
            pair.policy = profile_policy(pair)
            pair.request(f'R{rid:03}-B01', method + ' /api/domain-discovery/ignore', body=json_body({'values': capacity}), expect=200, writes=True)
        for case, body in [('F08', {}), ('F08', {'values': 'example.com'}), ('F09', {'values': ['192.0.2.1']}),
                           ('B02', {'values': ['中国.example']}), ('B03', {'values': []})]:
            with Pair(binary, recorder, options=options()) as pair:
                pair.policy = profile_policy(pair)
                pair.request(f'R{rid:03}-' + case, method + ' /api/domain-discovery/ignore', body=json_body(body),
                             expect=400, writes=True, variant=repr(body))
    opts = options()
    opts['profile']['domain_discovery']['ignored'] = ['x' + str(i) + '.example' for i in range(2000)]
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy = profile_policy(pair)
        pair.request('R050-F10', 'POST /api/domain-discovery/ignore', body=json_body({'values': ['overflow.example']}), expect=400, writes=True)


def notes(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        pair.policy = profile_policy(pair)
        for case, note in [('S01', '  中文备注🧪  '), ('S03', '修改备注'), ('B01', '中🧪' * 100), ('S02', ' \t ' )]:
            pair.request('R061-' + case, 'PUT /api/domain-discovery/website/note', body=json_body({'domain': 'EXAMPLE.COM.', 'note': note}),
                         expect=200, writes=True, check=lambda r: require(r.json()['domain'] == 'example.com' and r.json()['note'] == note.strip(), 'domain normalization and trimmed note are reflected in response/storage'))
    for case, body in [('F08', []), ('F09', {'domain': '192.0.2.1', 'note': 'x'}),
                        ('F10', {'domain': 'example.com', 'note': 1}), ('F11', {'domain': 'example.com', 'note': 'x' * 201})]:
        with Pair(binary, recorder, options=options()) as pair:
            pair.policy = profile_policy(pair)
            for ident in ('R061-' + case, 'H054' if case == 'F08' else 'H055'):
                pair.request(ident, 'PUT /api/domain-discovery/website/note', body=json_body(body), expect=400, writes=True, variant=repr(body))
    opts = options()
    opts['profile']['domain_discovery']['website_notes'] = {'x' + str(i) + '.example': 'existing' for i in range(2000)}
    with Pair(binary, recorder, options=opts) as pair:
        pair.policy = profile_policy(pair)
        pair.request('R061-F12', 'PUT /api/domain-discovery/website/note', body=json_body({'domain': 'new.example', 'note': 'overflow'}), expect=400, writes=True)
        pair.request('H056', 'PUT /api/domain-discovery/website/note', body=json_body({'domain': 'new.example', 'note': 'overflow'}), expect=400, writes=True)
        pair.request('R061-B02', 'PUT /api/domain-discovery/website/note', body=json_body({'domain': 'x0.example', 'note': '可更新🧪'}),
                     expect=200, writes=True, check=lambda r: require(len(stored(pair)['website_notes']) == 2000, 'existing note can be updated at capacity'))


GROUPS = {'discovery-settings': settings, 'discovery-ignored': ignored, 'discovery-notes': notes}
