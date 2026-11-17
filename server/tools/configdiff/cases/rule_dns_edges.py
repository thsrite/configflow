"""DNS/native transaction boundaries with real rule and group writes."""
import copy
import hashlib

import yaml

from compare import database
from harness import json_body
from cases.helpers import profile_pair as Pair
from cases.config_backup import message
from cases.groups import NODES
from cases.rules import options, RULE
from cases.setup import Stable
from cases.subscriptions import require

REMOVE = ('OxiDNS 还在用这条规则，而且那部分配置是手动写的，没法自动移除。'
          '请先在 OxiDNS 设置的 YAML 中删掉对它的引用，再删除。')


def groups(binary, recorder):
    group = {'id': 'g-a', 'name': '跟随组', 'type': 'select', 'manual_nodes': ['DIRECT']}
    profile = {'proxy_groups': [], 'rule_configs': [{**RULE, 'policy': group['name']}],
               'oxidns': {'config_mode': 'native', 'native_config': 'plugins: []\n', 'rule_dns_follow': ['r-a']}}
    for case, route, path in [('R157-F19', 'POST /api/proxy-groups', '/api/proxy-groups'),
            ('R149-F19', 'POST /api/profiles/<profile_id>/proxy-groups', '/api/profiles/default/proxy-groups')]:
        with Pair(binary, recorder, options={'shared': {'nodes': NODES}, 'profile': profile}) as pair:
            pair.request(case, route, path, body=json_body(group), expect=409, writes=True,
                         interaction=Stable(pair), variant='new-direct-group-moves-existing-follow-rule-in-custom-native')
    updated = copy.deepcopy(profile)
    updated['proxy_groups'] = [group]
    with Pair(binary, recorder, options={'shared': {'nodes': NODES}, 'profile': updated}) as pair:
        pair.request('R159-F18', 'PUT /api/proxy-groups/<group_id>', '/api/proxy-groups/g-a',
                     body=json_body({'manual_nodes': ['n-a']}), expect=409, writes=True,
                     interaction=Stable(pair), variant='group-default-direct-to-proxy-cannot-rewrite-native')


def rule_failures(binary, recorder):
    for case, template, path, identity, args in [
            ('R184-F09', 'DELETE /api/rules/<rule_id>', '/api/rules/r-a', 'r-a', {'exps': ['full:example.com']}),
            ('R180-F10', 'DELETE /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a', 'rs-a',
             {'files': ['./rules/rule_' + hashlib.sha256(b'rs-a').hexdigest()[:16] + '_domain.txt']})]:
        opts = options()
        opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': yaml.safe_dump({'plugins': [
            {'tag': 'handwritten_provider', 'type': 'domain_set', 'args': args}]}, sort_keys=False)}
        with Pair(binary, recorder, options=opts) as pair:
            pair.request(case, template, path, expect=400, writes=True, interaction=Stable(pair),
                         check=message(REMOVE), variant='real-handwritten-domain-provider-still-uses-' + identity)
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: []\n'}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R185-F20', 'PUT /api/rules/<rule_id>', '/api/rules/r-a',
                     body=json_body({'value': 'changed.example', 'dns': 'direct'}), expect=409, writes=True,
                     interaction=Stable(pair), variant='explicit-DNS-write-requires-safe-native-rebuild')
    with Pair(binary, recorder, options=options()) as pair:
        pair.request('R185-F19', 'PUT /api/rules/<rule_id>', '/api/rules/r-ip', body=json_body({'dns': 'direct'}),
                     expect=400, writes=True, interaction=Stable(pair),
                     check=message('IP-CIDR 规则不参与 DNS 分流，请把 DNS 设为默认上游'), variant='explicit-DNS-on-existing-IP-rule')
    opts = options()
    opts['shared']['rule_library'][0].update(behavior='ipcidr', content='192.0.2.0/24\n')
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R181-F11', 'PUT /api/rule-sets/<rule_set_id>', '/api/rule-sets/rs-a',
                     body=json_body({'dns': 'direct'}), expect=400, writes=True, interaction=Stable(pair),
                     check=message('ipcidr 规则不参与 DNS 分流，请把 DNS 设为默认上游'), variant='explicit-DNS-on-IP-only-ruleset')


def reads(binary, recorder):
    opts = options()
    opts['shared']['rule_library'][0]['enabled'] = False
    with Pair(binary, recorder, options=opts) as pair:
        for case, route, path in [('R182-B02', 'GET /api/rules', '/api/rules'),
                ('R151-B02', 'GET /api/profiles/<profile_id>/rules', '/api/profiles/default/rules')]:
            def check(reply):
                reference = next(item for item in reply.json() if item['id'] == 'rs-a')
                require(reference['library_enabled'] is False, 'disabled shared declaration stays visible with its precise hydrated flag')
            pair.request(case, route, path, expect=200, writes=True, interaction=Stable(pair), check=check,
                         variant='disabled-shared-source-remains-listed')
    opts = options()
    opts['profile']['rule_configs'][0]['enabled'] = False
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: []\n',
                               'rule_dns_parked': {'r-a': {'action': 'refused'}}}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('R188-B02', 'GET /api/rules/dns', expect=200, writes=True, interaction=Stable(pair),
                     check=lambda reply: require(reply.json()['rules']['r-a'] == {'state': 'disabled', 'route': 'refused'},
                         'disabled native rule shows its exact parked DNS choice without losing it'), variant='disabled-native-parked-refused')


def changes(binary, recorder):
    opts = options()
    opts['profile']['oxidns'] = {'direct_rules': ['r-a']}
    with Pair(binary, recorder, options=opts) as pair:
        def explicit(reply):
            fields = database(pair.dirs[0])['fields']
            rule = next(item for item in fields['profile/default/rule_configs']['value'] if item['id'] == 'r-a')
            require(rule['policy'] == 'REJECT' and fields['profile/default/oxidns']['value']['direct_rules'] == ['r-a'],
                    'explicit direct DNS remains direct while connection policy changes to REJECT')
        pair.request('R194-B03', 'PUT /api/rules/policy', body=json_body({'ids': ['r-a'], 'policy': 'REJECT'}),
                     expect=200, writes=True, check=explicit, variant='explicit-DNS-does-not-follow-policy-edit')
    opts = options()
    opts['profile']['rule_configs'] = [copy.deepcopy(RULE), {**RULE, 'id': 'r-b', 'value': 'second.example'}]
    opts['profile']['oxidns'] = {'direct_rules': ['r-a', 'r-b']}
    opts['x01_native'] = True
    with Pair(binary, recorder, options=opts) as pair:
        def reordered(reply):
            fields = database(pair.dirs[0])['fields']
            require([item['id'] for item in fields['profile/default/rule_configs']['value']] == ['r-b', 'r-a'], 'actual rule order is changed')
            native = fields['profile/default/oxidns']['value']['native_config']
            require(native.index('full:second.example') < native.index('full:example.com'), 'recognized native provider atoms are rebuilt in the new priority order')
        pair.request('R195-B03', 'POST /api/rules/reorder', body=json_body({'ids': ['r-b'], 'position': 'top'}),
                     expect=200, writes=True, check=reordered, variant='real-recognized-native-provider-order-rebuild')


def guards(binary, recorder):
    with Pair(binary, recorder, options=options()) as pair:
        allowed = '规则 DNS 处理无效：refused、nxdomain、nodata、blackhole、upstream'
        for case, field, value, text in [
                ('H904', 'rule_actions', 'invalid', allowed),
                ('H904', 'rule_actions', {'action': 'invalid'}, allowed),
                ('H905', 'rule_dns_parked', {'action': 'direct', 'extra': True}, '规则 DNS 处理「直连 DNS」不能包含其他参数'),
                ('H906', 'rule_actions', {'action': 'upstream', 'source': 'invalid', 'upstream': {'addr': 'udp://192.0.2.53'}},
                 '指定上游需要 source（local、remote 或 fallback）和 upstream'),
                ('H907', 'rule_actions', {'action': 'upstream', 'source': 'local', 'upstream': {'addr': ''}}, '指定上游需要非空的 addr 地址')]:
            for template, prefix in [('POST /api/oxidns/render', '/api/oxidns'),
                    ('POST /api/profiles/<profile_id>/oxidns/render', '/api/profiles/default/oxidns')]:
                pair.request(case, template, prefix + '/render', body=json_body({'settings': {field: {'r-a': value}}}),
                             expect=400, writes=True, interaction=Stable(pair), check=message(text), variant='normalize-action/' + repr(value))
        pair.request('H910', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'upstream:000000000000'}}),
                     expect=400, writes=True, interaction=Stable(pair), variant='well-formed-key-no-longer-in-real-catalog',
                     check=message('所选上游已不在 OxiDNS 上游设置中，请刷新后重选'))
    opts = options()
    opts['profile']['oxidns'] = {'config_mode': 'native', 'native_config': 'plugins: [\n'}
    with Pair(binary, recorder, options=opts) as pair:
        pair.request('H914', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'direct'}}), expect=409,
                     writes=True, interaction=Stable(pair), variant='actual-unparseable-native-draft-cannot-be-rebuilt')
    opts = options()
    opts['agent_updates'] = {'service_type': 'oxidns', 'dns_follow': {'upstream_tag': 'forward_local', 'upstream_index': 0}}
    with Pair(binary, recorder, options=opts) as pair:
        # CFG-04: this historical seed establishes only the catalog/guard, not
        # a valid running DNS-follow binding. Do not compare its accidental
        # Python startup failure against the unported Go A17 background owner.
        # Restore ONLY after A17 exists and a complete binding/fake replaces
        # this partial seed; route registration alone is not sufficient.
        dependency = 'CFG-04 / A17: complete DNS-follow binding and background owner pending; restore fixture after A17'
        replies = pair.request('R188-S01', 'GET /api/rules/dns', expect=200, writes=True, interaction=Stable(pair),
                               pending_dependency=dependency,
                               variant='real-DNS-follow-binding-catalog')
        entry = next(item for item in replies[0].json()['upstreams'] if item['source'] == 'local' and item['linked'])
        pair.request('H911', 'PUT /api/rules/dns', body=json_body({'routes': {'r-a': 'upstream:' + entry['key']}}),
                     expect=400, writes=True, interaction=Stable(pair), check=message('已绑定 DNS 联动，请改选直连 DNS'),
                     pending_dependency=dependency,
                     variant='actual-public-catalog-key-is-bound-to-DNS-follow')


GROUPS = {'rule-dns-group-strict-boundaries': groups, 'rule-dns-native-write-boundaries': rule_failures,
          'rule-dns-disabled-reads': reads, 'rule-dns-explicit-and-reorder': changes, 'rule-dns-extra-guard-branches': guards}
