"""Setup planning and validation; generation only, no host TUN or real devices."""
import hashlib
import json

import yaml

from compare import database
from harness import Pair
from sources import Source
from subscriptions import Exchange
from cases.setup import (PID, NODE, SUB, PATH, ACTION, configure, call, row, session, source_options)
from cases.config_backup import message
from cases.subscriptions import require, put_file

PREPARE = {'local_dns': 'https://192.0.2.53/dns-query', 'domain_rules': {'direct': [], 'proxy': []},
           'controller_secret': 'synthetic-controller-secret'}


def options(goal='router', *, selected=True, profile=None, changes=None):
    value = source_options()
    fixture = value['setup_sessions'][0]
    fixture['goal'] = goal
    fixture['session'] = {'node_ids': [NODE['id']] if selected else [], 'step': 2, 'max_step': 2, **(changes or {})}
    if profile:
        fixture['profile'] = profile
        # A compatibility fixture starts from the profile revision it owns.
        fixture['session'].setdefault('profile_revision', 1)
    return value


def digest(profile, base=True):
    values = {key: profile.get(key) for key in ('proxy_groups', 'rule_configs', 'mihomo', 'oxidns')}
    if not base:
        values['mihomo'] = {k: v for k, v in values['mihomo'].items() if k != 'custom_config'}
    return hashlib.sha256(json.dumps(values, sort_keys=True, ensure_ascii=False).encode()).hexdigest()


def check_plan(pair, reply, dns, traffic, *, full_tun=True, query_recorder=True, local_dns=None):
    value, saved = reply.json(), row(pair)
    before_serialize = {key: item for key, item in value.items() if key != 'default_profile'}
    require(before_serialize == session(pair) and value['prepared'] is True and value['step'] == value['max_step'] == 3
        and value['completed'] is False and value['node_count'] == 1, 'prepared response equals session file and unlocks installation with one usable node')
    require(value['owned_digest'] == digest(saved) and value['owned_digest_without_base'] == digest(saved, False)
        and value['profile_revision'] == saved['_revision'], 'session ownership digests and revision describe the exact committed fields')
    fields = database(pair.dirs[0])['fields']
    require(value['config_revision'] == saved['_revision'] + fields['shared//_revision']['value'], 'public session revision matches profile plus shared SQL revisions')
    require(saved['oxidns']['default_forward'] == ('forward_remote' if dns == 'mihomo' else 'forward_local')
        and saved['oxidns']['local_dns'] == (local_dns or PREPARE['local_dns']).strip()
        and saved['oxidns']['query_recorder']['enabled'] is query_recorder
        and saved['oxidns']['remote_dns'] == saved['oxidns']['fallback_dns'] == '127.0.0.1:1053'
        and saved['rule_configs'][-1]['rule_type'] == 'MATCH'
        and saved['rule_configs'][-1]['policy'] == ('🐟 漏网之鱼' if traffic == 'proxy' else 'DIRECT'), 'DNS and traffic fallback choices drive independently stored native inputs and final Mihomo policy')
    base = yaml.safe_load(saved['mihomo']['custom_config'])
    require(base['secret'] == PREPARE['controller_secret'] and base['dns']['listen'] == '127.0.0.1:1053'
        and base['dns']['enhanced-mode'] == 'fake-ip' and value['controller_secret_saved'] is True
        and PREPARE['controller_secret'] not in json.dumps(value, ensure_ascii=False), 'controller secret is persisted but never returned in public setup session')
    if full_tun:
        require(all(base['tun'][key] is True for key in ('enable', 'auto-route', 'auto-redirect', 'auto-detect-interface'))
            and base['tun']['dns-hijack'] == ['any:53', 'tcp://any:53'] and saved['mihomo']['linux_gateway']['enabled'] is False
            and saved['mihomo']['router_split']['takeover'] == 'full_tun', 'new router plan selects complete TUN generation, never executes network setup')
    else:
        require('router_split' not in saved['mihomo'] and not base.get('tun', {}).get('enable'), 'gateway plan leaves TUN activation to later installation flow')


def plans(binary, recorder):
    for case, dns, traffic in [('S04', 'mihomo', 'proxy'), ('S05', 'direct', 'direct'), ('S06', 'mihomo', 'direct'), ('S07', 'direct', 'proxy')]:
        for goal in ('router', 'gateway'):
            with Source(b'synthetic upstream unavailable') as outbound:
                outbound.status = 503
                fixture = options(goal)
                fixture['system_config'] = {'rule_fetch_proxy': outbound.url}
                with Pair(binary, recorder, options=fixture) as pair:
                    configure(pair)
                    body = {**PREPARE, 'dns_fallback': dns, 'traffic_fallback': traffic}
                    call(pair, 'R211-' + case, ACTION, PATH + '/prepare', body=body,
                         check=lambda r: check_plan(pair, r, dns, traffic, full_tun=goal == 'router'), variant=goal,
                         interaction=Exchange(pair, outbound, expected=0))
                    committed = database(pair.dirs[0])
                    call(pair, 'R211-' + case, ACTION, PATH + '/prepare', body=body,
                         check=lambda r: (check_plan(pair, r, dns, traffic, full_tun=goal == 'router'),
                                          require(database(pair.dirs[0]) == committed, 'identical preparation updates only the session receipt, never recommits configuration')),
                         variant=goal + '/repeat', interaction=Exchange(pair, outbound, expected=0))


GROUPS = {'setup-prepare-plans': plans}


def plan_forms(binary, recorder):
    forms = [('tls-upstream', 'tls://192.0.2.53:853', False),
             ('YAML-upstream', '- addr: https://192.0.2.53/dns-query\n', True)]
    for variant, local_dns, query_recorder in forms:
        with Source(b'synthetic upstream unavailable') as outbound:
            outbound.status = 503
            fixture = options()
            fixture['system_config'] = {'rule_fetch_proxy': outbound.url}
            with Pair(binary, recorder, options=fixture) as pair:
                configure(pair)
                body = {**PREPARE, 'local_dns': local_dns, 'query_recorder': query_recorder}
                def check(reply):
                    check_plan(pair, reply, 'mihomo', 'proxy', local_dns=local_dns, query_recorder=query_recorder)
                call(pair, 'R211-S04', ACTION, PATH + '/prepare', body=body, check=check, variant=variant,
                     interaction=Exchange(pair, outbound, expected=0))
                before = database(pair.dirs[0])
                body.pop('controller_secret')
                call(pair, 'R211-S04', ACTION, PATH + '/prepare', body=body,
                     check=lambda r: (check(r), require(database(pair.dirs[0]) == before, 'saved secret can be reused without a password field or another configuration commit')),
                     variant=variant + '/omit-existing-secret', interaction=Exchange(pair, outbound, expected=0))
    with Source(b'synthetic upstream unavailable') as outbound:
        outbound.status = 503
        fixture = options()
        fixture['system_config'] = {'rule_fetch_proxy': outbound.url}
        library = {'id': 'wizard-domain', 'name': '工作域名🧪', 'behavior': 'domain', 'format': 'text',
                   'source_type': 'content', 'content': '+.rules.example\n', 'enabled': True, 'update_interval': 0}
        fixture['shared']['rule_library'] = [library]
        with Pair(binary, recorder, options=fixture) as pair:
            configure(pair)
            choice = 'library:' + library['id']
            body = {**PREPARE, 'domain_rules': {'direct': [choice, choice], 'proxy': []}}
            def check(reply):
                check_plan(pair, reply, 'mihomo', 'proxy')
                rules = [r for r in row(pair)['rule_configs'] if r.get('itemType') == 'ruleset']
                require(reply.json()['domain_rules'] == {'direct': [choice], 'proxy': []}
                    and len(rules) == 1 and rules[0]['library_rule_id'] == library['id']
                    and row(pair)['oxidns']['direct_rulesets'] == [rules[0]['id']], 'selected owned domain source deduplicates to one profile reference and one OxiDNS direct selector')
            call(pair, 'R211-S04', ACTION, PATH + '/prepare', body=body, check=check, variant='owned-domain-library-reference',
                 interaction=Exchange(pair, outbound, expected=0))


GROUPS['setup-prepare-forms'] = plan_forms


def guards(binary, recorder):
    vectors = [('H1033', {'unexpected': True}, '漏网处理参数无效', 400),
               ('H1034', {'template': 'proxy'}, '代理优先模式已移除，请刷新向导并分别选择 DNS 与连接的漏网处理', 400)]
    for field in ('dns_fallback', 'traffic_fallback'):
        for value in (None, 'invalid', True):
            vectors.append(('H1035', {field: value}, '请选择有效的 DNS 与连接漏网处理方式', 400))
    for value in (None, 1, 'false', []):
        vectors.append(('H1036', {'query_recorder': value}, 'DNS 查询记录开关必须为布尔值', 400))
    for value in (None, [], {'direct': []}, {'direct': [], 'proxy': [], 'extra': []}):
        vectors.append(('H1038', {'domain_rules': value}, '请选择有效的直连和代理域名规则集', 400))
    for value in (None, True, [1], [''], ['x' * 257], ['x'] * 101):
        vectors.append(('H1039', {'domain_rules': {'direct': value, 'proxy': []}}, '域名规则集必须是有效的选项列表', 400))
    vectors += [('H1040', {'domain_rules': {'direct': ['missing'], 'proxy': ['missing']}}, '同一个域名规则集不能同时设为直连和代理', 400),
                ('H1041', {'domain_rules': {'direct': ['missing'], 'proxy': []}}, '所选域名规则集已删除、停用或不支持，请刷新后重选', 409),
                ('H1037', {'local_dns': '['}, 'DNS 上游 YAML 语法无效', 400),
                ('H1037', {'local_dns': '[{}]'}, 'DNS 上游 YAML 每项需要 addr 地址', 400),
                ('H1037', {'local_dns': '\n'.join('- addr: https://192.0.2.53/dns-query' for _ in range(17))}, '直连 DNS 最多支持 16 个上游', 400)]
    invalid_dns = ['http://192.0.2.53/dns-query', 'https://dns.example/dns-query', 'https://[2001:db8::53]/dns-query',
                   'https://127.0.0.1/dns-query', 'https://0.0.0.0/dns-query', 'https://224.0.0.1/dns-query',
                   'https://169.254.1.1/dns-query', 'https://192.0.2.53:53/dns-query', 'tls://192.0.2.53:1053',
                   'https://u:p@192.0.2.53/dns-query', 'https://192.0.2.53',
                   '- addr: https://192.0.2.53/dns-query\n  socks5: 127.0.0.1:7890\n',
                   '- addr: https://192.0.2.53/dns-query\n  outbound: proxy\n']
    for value in invalid_dns:
        vectors.append(('H1037', {'local_dns': value}, '直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口', 400))
    with Pair(binary, recorder, options=options()) as pair:
        configure(pair)
        for index, (helper, changes, text, status) in enumerate(vectors):
            for case in ('R211-F18', helper):
                call(pair, case, ACTION, PATH + '/prepare', body={**PREPARE, **changes}, expect=status,
                     check=message(text), readonly=True, variant=str(index))
        for index, value in enumerate((None, '', ' ', '中' * 6000)):
            for case in ('R211-F18', 'H1037', 'H952'):
                call(pair, case, ACTION, PATH + '/prepare', body={**PREPARE, 'local_dns': value}, expect=400, readonly=True,
                     check=lambda r: require(r.json()['message'].startswith('请填写直连 DNS，使用 IP 地址的 DoH / DoT，例如 '),
                                             'missing or byte-oversized DNS uses the unique required-DNS guard; full returned message is still compared'), variant='required-dns/' + str(index))
        for index, (helper, value, text) in enumerate([
            ('H953', '[', 'DNS 上游 YAML 语法无效'), ('H953', '[{}]', 'DNS 上游 YAML 每项需要 addr 地址'),
            ('H954', '\n'.join('- addr: https://192.0.2.53/dns-query' for _ in range(17)), '直连 DNS 最多支持 16 个上游'),
            ('H955', 'http://192.0.2.53/dns-query', '直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口'),
            ('H956', 'https://192.0.2.53', '直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口'),
            ('H957', 'https://[bad', '直连 DNS 必须使用 IPv4 地址的 DoH / DoT，不能指向本机、53 / 1053 端口或代理出口')]):
            call(pair, helper, ACTION, PATH + '/prepare', body={**PREPARE, 'local_dns': value}, expect=400,
                 check=message(text), readonly=True, variant=str(index))
    for case, changes, cache in [('H1030', {'node_ids': ['deleted-node']}, None),
                                ('H1031', {'node_ids': [], 'subscription_ids': [SUB['id']]}, {'metadata': {'url': SUB['url'] + '/changed'}, 'nodes': [NODE]}),
                                ('H1032', {'node_ids': [], 'subscription_ids': [SUB['id']]}, None)]:
        with Pair(binary, recorder, options=options(changes=changes)) as pair:
            configure(pair)
            if cache is not None:
                put_file(pair, 'shared/subscribes/' + SUB['id'] + '.json', cache)
            text, status = {'H1030': ('所选来源已删除或停用，请返回检查节点来源', 409),
                            'H1031': ('订阅链接已变化，请重新读取订阅', 400),
                            'H1032': ('当前没有可用节点，请先读取订阅或选择支持的节点', 400)}[case]
            for name in ('R211-F19', case):
                call(pair, name, ACTION, PATH + '/prepare', body=PREPARE, expect=status, check=message(text), readonly=True, variant=case)
    with Source(b'synthetic upstream unavailable') as outbound:
        outbound.status = 503
        fixture = options()
        fixture['system_config'] = {'rule_fetch_proxy': outbound.url}
        with Pair(binary, recorder, options=fixture) as pair:
            configure(pair)
            call(pair, 'R145-S03', 'POST /api/profiles/<profile_id>/oxidns/settings', '/api/profiles/' + PID + '/oxidns/settings',
                 body={'worker_threads': 8}, variant='setup-external-edit-precondition')
            before_profile, before_session = row(pair), session(pair)
            before_shared_revision = database(pair.dirs[0])['fields']['shared//_revision']['value']
            def rejected(reply):
                message('该空间已在其他页面修改，向导不会覆盖；请新建向导或前往配置生成继续')(reply)
                require(row(pair) == before_profile and session(pair) == before_session, 'conflict preserves edited profile and owned session')
                fields = database(pair.dirs[0])['fields']
                require(bool(fields['shared//rule_library']['value'])
                    and fields['shared//_revision']['value'] == before_shared_revision + 1,
                    'first prepare initializes built-in shared rule catalog before detecting profile conflict')
            call(pair, 'R211-F20', ACTION, PATH + '/prepare', body=PREPARE, expect=409, check=rejected,
                 interaction=Exchange(pair, outbound, expected=0))
            call(pair, 'H1042', ACTION, PATH + '/prepare', body=PREPARE, expect=409, readonly=True,
                 check=message('该空间已在其他页面修改，向导不会覆盖；请新建向导或前往配置生成继续'))


GROUPS['setup-prepare-guards'] = guards
