"""DNS choices, native rebuilding and atomic selector cleanup for profile rules."""
import copy

from compare import database
from harness import json_body
from cases.helpers import profile_pair as Pair
from cases.rules import RULE, options, rules
from cases.subscriptions import require
from cases.config_backup import message


def oxidns(pair):
    return database(pair.dirs[0])['fields']['profile/default/oxidns']['value']


def choices(binary, recorder):
    for native in (False, True):
        opts = options()
        opts['x01_native'] = native
        with Pair(binary, recorder, options=opts) as pair:
            pair.request('R188-' + ('S02' if native else 'S01'), 'GET /api/rules/dns', expect=200, writes=True,
                         check=lambda r: require(r.json()['engine']['mode'] == ('native' if native else 'template') and r.json()['rules']['r-ip']['state'] == 'unsupported',
                                                 'mode and unsupported IP rule are exposed through read model'))
            for case, choice in [('S01', 'direct'), ('S02', 'follow'), ('S03', 'default'),
                                  ('S01', 'proxy'), ('S01', 'refused'), ('S01', 'nxdomain'), ('S01', 'nodata'), ('S01', 'blackhole')]:
                def check(reply):
                    result = reply.json()['rules']['r-a']
                    if choice == 'default':
                        require(result['state'] == 'default' and result['route'] is None, 'default clears explicit DNS choice')
                    elif choice == 'follow':
                        require(result['state'] == 'follow' and result['route'] == 'direct', 'DIRECT policy follow selects direct DNS')
                    else:
                        require(result['state'] == 'explicit' and result['route'] == choice, 'stored choice is exposed without translation loss')
                pair.request('R189-' + case, 'PUT /api/rules/dns',
                             body=json_body({'routes': {'r-a': choice}, 'base_url': 'http://panel.example'}),
                             expect=200, writes=True, check=check, variant=('native/' if native else 'template/') + choice)
            for case, body, status, expected in [
                ('F08', [], 400, '请提供要修改 DNS 处理的规则'), ('F09', {}, 400, '请提供要修改 DNS 处理的规则'),
                ('F10', {'routes': {}}, 400, '请提供要修改 DNS 处理的规则'),
                ('F11', {'routes': {'r-' + str(i): 'direct' for i in range(201)}}, 400, '一次最多修改 200 条规则的 DNS 处理'),
                ('F12', {'routes': {'r-a': 'unknown'}}, 400, '不支持的 DNS 处理：unknown'),
                ('F12', {'routes': {'r-a': 1}}, 400, '不支持的 DNS 处理：1'),
                ('F13', {'routes': {'r-a': 'direct', 'missing': 'direct'}}, 400, '规则不存在：missing，请刷新后重试'),
                ('F15', {'routes': {'r-a': 'direct', 'r-ip': 'direct'}}, 400, 'IP-CIDR 规则不参与 DNS 分流，请把 DNS 设为默认上游')]:
                before = database(pair.dirs[0])
                def check(reply):
                    message(expected)(reply)
                    require(database(pair.dirs[0]) == before, 'rejected DNS batch rolls back valid member and every row revision')
                pair.request('R189-' + case, 'PUT /api/rules/dns', body=json_body(body), expect=status, writes=True, check=check,
                             variant=('native/' if native else 'template/') + (repr(body['routes']['r-a']) if case == 'F12' else ''))
            for case in ('S04', 'B02'):
                pair.request('R189-' + case, 'PUT /api/rules/dns',
                             body=json_body({'routes': {'r-a': 'direct', 'r-ip': 'direct'}, 'skip_unsupported': True, 'base_url': 'http://panel.example'}),
                             expect=200, writes=True, variant='native' if native else 'template',
                             check=lambda r: require(r.json()['skipped'] == ['r-ip'] and 'r-ip' not in r.json()['rules'], 'skip only unsupported selected IP member'))
    with Pair(binary, recorder) as pair:
        pair.request('R188-B01', 'GET /api/rules/dns', expect=200, writes=True,
                     check=lambda r: require(r.json()['rules'] == {}, 'empty rule DNS map'))
    opts = options()
    opts['profile']['rule_configs'] = [{**RULE, 'id': 'limit-' + str(i)} for i in range(200)]
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R189-B01', 'PUT /api/rules/dns', body=json_body({'routes': {'limit-' + str(i): 'direct' for i in range(200)}}),
                     expect=200, writes=True, check=lambda r: require(len(r.json()['rules']) == 200 and len(oxidns(pair)['direct_rules']) == 200,
                                                                     'exactly 200 DNS choices are accepted and stored'))


def selectors_and_types(binary, recorder):
    for native in (False, True):
        opts = options()
        opts['x01_native'] = native
        with Pair(binary, recorder, options=opts) as pair:
            pair.request('R189-S01', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'direct'}, 'base_url': 'http://panel.example'}),
                         expect=200, writes=True, variant='cleanup-prepare/' + str(native))
            def check(reply):
                settings = oxidns(pair)
                require(all('r-a' not in settings.get(key, []) for key in ('direct_rules', 'proxy_rules', 'direct_rulesets', 'proxy_rulesets', 'rule_dns_follow')),
                        'deleting ordinary rule clears every selector and follow list')
                require('r-a' not in settings.get('rule_actions', {}) and 'r-a' not in settings.get('rule_dns_parked', {}), 'deleting ordinary rule clears DNS action and parked state')
            pair.request('R184-' + ('S02' if native else 'S01'), 'DELETE /api/rules/<rule_id>', '/api/rules/r-a',
                         expect=200, writes=True, check=check, headers={'Host': 'panel.example'}, variant='dns-cleanup/' + str(native))
    opts = options()
    opts['profile']['mosdns'] = {'direct_rules': ['r-a'], 'proxy_rulesets': ['rs-a']}
    with Pair(binary, recorder, options=opts) as pair:
        before = database(pair.dirs[0])
        pair.request('R185-F17', 'PUT /api/rules/<rule_id>', '/api/rules/r-a',
                     body=json_body({'rule_type': 'DST-PORT', 'value': '443'}), expect=400, writes=True,
                     check=lambda r: require(database(pair.dirs[0]) == before, 'type change cannot strand existing MosDNS selection'))
        pair.request('R184-S01', 'DELETE /api/rules/<rule_id>', '/api/rules/r-a', expect=200, writes=True, variant='mosdns-selector',
                     check=lambda r: require(database(pair.dirs[0])['fields']['profile/default/mosdns']['value']['direct_rules'] == [], 'delete clears MosDNS ordinary selector'))
        pair.request('R180-S01', 'DELETE /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a', expect=200, writes=True, variant='mosdns-selector',
                     check=lambda r: require(database(pair.dirs[0])['fields']['profile/default/mosdns']['value']['proxy_rulesets'] == [], 'delete clears MosDNS ruleset selector'))
    with Pair(binary, recorder, options=options()) as pair:
        pair.request('R189-S01', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'direct'}}), expect=200, writes=True, variant='type-guard-prepare')
        before = database(pair.dirs[0])
        pair.request('R185-F18', 'PUT /api/rules/<rule_id>', '/api/rules/r-a', body=json_body({'rule_type': 'DST-PORT', 'value': '443'}),
                     expect=400, writes=True, check=lambda r: require(database(pair.dirs[0]) == before, 'explicit OxiDNS route blocks unsupported type change'))
        pair.request('R185-S03', 'PUT /api/rules/<rule_id>', '/api/rules/r-a', body=json_body({'rule_type': 'DST-PORT', 'value': '443', 'dns': 'default'}),
                     expect=200, writes=True, variant='clear-route-with-type', check=lambda r: require('r-a' not in oxidns(pair)['direct_rules'], 'explicit default permits type change and removes route'))


def native_locked(binary, recorder):
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: []\n'}
    with Pair(binary, recorder, options=opts) as pair:
        before = database(pair.dirs[0])
        pair.request('R189-F14', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'direct'}}), expect=409, writes=True,
                     check=lambda r: require(database(pair.dirs[0]) == before, 'unmanaged native graph is never partially rewritten'))
        pair.request('R194-B02', 'PUT /api/rules/policy', body=json_body({'ids': ['r-a'], 'policy': 'REJECT'}), expect=200, writes=True,
                     check=lambda r: require(bool(r.json().get('warnings')) and oxidns(pair)['native_config'] == 'plugins: []\n',
                                             'non-strict policy update warns and preserves unmanaged native draft'))
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: [\n'}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R184-B01', 'DELETE /api/rules/<rule_id>', '/api/rules/r-a', expect=200, writes=True,
                     check=lambda r: require(oxidns(pair)['native_config'] == 'plugins: [\n', 'unreadable native draft remains intact when removing rule'))


GROUPS = {'rule-dns-choices': choices, 'rule-dns-selectors-types': selectors_and_types, 'rule-dns-native-locked': native_locked}
