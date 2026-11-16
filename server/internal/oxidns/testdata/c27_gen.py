"""C27 public-boundary references; run only on an isolated test server.

All profiles are synthetic. Expected write results and final delivered OxiDNS
documents come from the actual Python public entry points, without patches.
"""
import copy
import gzip
import json
import os
import tempfile
from pathlib import Path

temporary = tempfile.TemporaryDirectory(prefix='oxidns-c27-')
os.environ['DATA_DIR'] = temporary.name

import yaml
from backend.common.oxidns_editor import prepare_editor
from backend.common.oxidns_settings import normalize_oxidns_settings
from backend.common.rule_dns import (apply_rule_dns, check_choice, entry_key,
    explicit_route, hydrate_ruleset, invalidate_follow, profile_agents,
    rule_dns_entries, rule_dns_state, upstream_catalog)
from backend.converters.oxidns import generate_oxidns_config

BASE = 'https://config.example'
rows = []


def record(name, fn, args, call):
    args = copy.deepcopy(args)
    row = {'test': name, 'fn': fn, 'args': copy.deepcopy(args)}
    try:
        row['out'] = call(*args)
    except Exception as exc:
        row['err'] = [type(exc).__name__, str(exc)]
        if hasattr(exc, 'usages'):
            row['usages'] = exc.usages
    row['args_after'] = args
    rows.append(row)


def profile(**settings):
    return {'profile_id': 'audit',
            'system_config': {'default_profile_id': 'audit', 'server_domain': BASE,
                              'rule_proxy_token': 'c27-synthetic'},
            'agents': [], 'mosdns': {},
            'nodes': [{'id': 'node', 'name': 'Proxy node', 'enabled': True}],
            'proxy_groups': [{'id': 'selection', 'name': 'Selection', 'type': 'select',
                              'manual_nodes': ['DIRECT', 'node']}],
            'rule_configs': [
                {'id': 'a', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'a.example', 'policy': 'DIRECT'},
                {'id': 'b', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'b.example', 'policy': 'Selection'},
                {'id': 'c', 'itemType': 'rule', 'rule_type': 'DOMAIN-REGEX', 'value': '^c[0-9]+\\.example$', 'policy': 'Selection'},
                {'id': 'mixed', 'itemType': 'ruleset', 'behavior': 'classical', 'source_type': 'content',
                 'content': 'DOMAIN,mixed.example\nIP-CIDR,192.0.2.0/24', 'url': 'https://rules.example/mixed.txt', 'policy': 'Selection'},
                {'id': 'ip', 'itemType': 'rule', 'rule_type': 'IP-CIDR6', 'value': '2001:db8::/32', 'policy': 'DIRECT'}],
            'oxidns': normalize_oxidns_settings({'local_dns': 'udp://192.0.2.53',
                'remote_dns': 'https://dns.example/dns-query', 'fallback_dns': 'udp://192.0.2.54', **settings})}


def native(config):
    result = copy.deepcopy(config)
    result['oxidns'] = prepare_editor(result['oxidns'], result, BASE)
    return result


def edit_native(config, edit):
    result = copy.deepcopy(config)
    document = yaml.safe_load(result['oxidns']['native_config'])
    edit(document)
    result['oxidns']['native_config'] = '# retained C27 draft\n' + yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    return result


def write_boundary(before, after, assignments, strict=True):
    warnings = apply_rule_dns(before, after, assignments, base_url=BASE, strict=strict)
    generated, rejected = None, False
    try:
        generated = yaml.safe_load(generate_oxidns_config(after, BASE))
    except Exception:
        rejected = True
    return {'warnings': warnings, 'settings': after['oxidns'],
            'state': rule_dns_state(after, BASE), 'generated': generated,
            'generation_rejected': rejected}


def write(name, before, assignments=None, edit=None, strict=True):
    after = copy.deepcopy(before)
    if edit:
        edit(after)
    record(name, 'apply_rule_dns_boundary', [before, after, assignments or {}, strict], write_boundary)


read_profiles = [profile(), profile(direct_rules=['a'], proxy_rules=['b']),
    profile(direct_rules=['a'], proxy_rules=['b'], rule_dns_follow=['a', 'b']),
    profile(rule_actions={'a': {'action': 'blackhole'}, 'b': {'action': 'refused'}}, direct_rules=['a']),
    profile(custom_matches=[{'matches': ['qname full:cached.example'], 'exec': '$lazy_cache'}], custom_match_position='head'),
    profile(rule_dns_parked={'a': {'action': 'direct'}})]
for config in list(read_profiles):
    read_profiles.append(native(config))
read_profiles[2]['rule_configs'][1]['enabled'] = False
read_profiles[5]['rule_configs'][0]['enabled'] = False
agent_config = profile()
agent_config['agents'] = [
    {'id': 'dns', 'name': 'Synthetic DNS', 'service_type': 'oxidns', 'profile_id': 'audit', 'enabled': True,
     'dns_follow': {'upstream_tag': 'forward_local', 'upstream_index': 0},
     'rule_sync': {'settings': {'library': True, 'rules': False}}},
    {'id': 'other', 'service_type': 'oxidns', 'profile_id': 'other'},
    {'id': 'off', 'service_type': 'oxidns', 'profile_id': 'audit', 'enabled': False},
    {'id': 'legacy', 'service_type': 'mihomo', 'rule_sync': {'settings': {'rules': True}}}]
read_profiles.append(agent_config)
record('explicit empty Agent subset', 'rule_dns_state', [agent_config, BASE, []], rule_dns_state)
record('explicit Agent subset', 'rule_dns_state', [agent_config, BASE, [agent_config['agents'][0]]], rule_dns_state)
bad_actions = profile()
bad_actions['oxidns'].update(rule_actions={'a': {'kind': 'bad'}, 'b': 'reject', 'c': {'action': 'nodata'}},
                              rule_dns_parked={'ip': {'action': 'drop'}})
read_profiles.append(bad_actions)
for index, config in enumerate(read_profiles):
    record(f'read {index}', 'rule_dns_state', [config], rule_dns_state)
    record(f'entries {index}', 'rule_dns_entries', [config, ['a', 'b', 'mixed', 'ip', 'gone']], rule_dns_entries)
    for identity in ('a', 'b', 'mixed'):
        record(f'explicit {index}/{identity}', 'explicit_route', [config, identity], explicit_route)
    record(f'agents {index}', 'profile_agents', [config], profile_agents)

for source in ('local', 'remote', 'fallback'):
    for cache in (False, True):
        for ttl in (0, 60):
            config = profile(cache_enabled=cache, cache_lazy_ttl=0 if ttl == 0 else 300,
                             remote_ttl=ttl, ip_selector={'enabled': True})
            for mode in ('template', 'native'):
                before = native(config) if mode == 'native' else config
                entry = {'local': {'addr': 'udp://192.0.2.53'},
                         'remote': {'addr': 'https://dns.example/dns-query'},
                         'fallback': {'addr': 'udp://192.0.2.54'}}[source]
                write(f'pin {source}/{cache}/{ttl}/{mode}', before,
                      {'a': 'upstream:' + entry_key(entry, source), 'mixed': 'blackhole'})

for mode in ('template', 'native'):
    for aggregate in (False, True):
        before = profile(aggregate_rules=aggregate, direct_rules=['a'], proxy_rules=['b'], rule_dns_follow=['a'])
        if mode == 'native':
            before = native(before)
        for choice in ('direct', 'proxy', 'refused', 'nxdomain', 'nodata', 'blackhole', 'follow', 'default'):
            write(f'choice {mode}/{aggregate}/{choice}', before, {'a': choice, 'mixed': choice})
        for choice in ('drop', 'upstream:abc', 'upstream:000000000000'):
            write(f'invalid {mode}/{choice}', before, {'a': choice})
        write('missing rule', before, {'gone': 'proxy'})
        write('IP assignment refused', before, {'ip': 'blackhole'})
        write('IP default permitted', before, {'ip': 'default'})
        write('group default change', before, edit=lambda p: p['proxy_groups'][0].update(manual_nodes=['node', 'DIRECT']))
        write('follow assignment', before, {'b': 'follow'})
        write('policy change', before, edit=lambda p: p['rule_configs'][0].update(policy='Selection'))
        for field, value in (('enabled', False), ('library_enabled', False), ('value', 'changed.example')):
            write(f'rule field {mode}/{field}', before, edit=lambda p, field=field, value=value: p['rule_configs'][0].update({field: value}))
        write('reorder', before, edit=lambda p: p['rule_configs'].reverse())
        write('delete', before, edit=lambda p: p['rule_configs'].pop(0))
        write('remark only', before, edit=lambda p: p['rule_configs'][0].update(remark='only metadata'))

write('clean malformed stored fields', bad_actions)
write('validate choices before rule lookup', profile(), {'gone': 'proxy', 'a': 'drop'})
for choice in ('follow', 'default', 'direct', 'proxy', 'refused', 'nxdomain', 'nodata', 'blackhole',
               'upstream:0123456789ab', 'upstream:ABCDEF012345', '', 'drop'):
    record('choice validation', 'check_choice', [choice], check_choice)

linked = copy.deepcopy(agent_config)
write('linked upstream refused', linked, {'b': 'upstream:' + entry_key({'addr': 'udp://192.0.2.53'}, 'local')})
for strict in (True, False):
    locked = native(profile(rule_actions={'b': {'action': 'blackhole'}}))
    locked = edit_native(locked, lambda d: next(p for p in d['plugins'] if p['tag'] == 'cf_dns_blackhole')['args'].update(mode='nxdomain'))
    write(f'locked {strict}', locked, {'a': 'refused'}, strict=strict)
    write(f'locked policy edit {strict}', locked, edit=lambda p: p['rule_configs'][0].update(policy='Selection'), strict=strict)
    write(f'locked explicit group edit {strict}', locked, edit=lambda p: p['proxy_groups'][0].update(manual_nodes=['node', 'DIRECT']), strict=strict)
    invalid = profile()
    invalid['oxidns'].update(config_mode='native', native_config='plugins: wrong')
    write(f'unreadable native {strict}', invalid, {'a': 'blackhole'}, strict=strict)

for config in (profile(direct_rules=['a'], rule_dns_follow=['a']),
               native(profile(direct_rules=['a'], rule_dns_follow=['a']))):
    replacement = copy.deepcopy(config['oxidns'])
    if replacement['config_mode'] == 'native':
        other = copy.deepcopy(config)
        apply_rule_dns(config, other, {'a': 'proxy'}, base_url=BASE)
        replacement = other['oxidns']
        replacement['rule_dns_follow'] = ['a']
    else:
        replacement.update(direct_rules=[], proxy_rules=['a'])
    record('manual replacement invalidates follow', 'invalidate_follow', [config, replacement], invalidate_follow)

# Managed group ordering and enable/disable lifecycles around a real follow mark.
for mode in ('template', 'native'):
    base = profile(direct_rules=['a', 'b'], rule_dns_follow=['b'], rule_actions={'c': {'action': 'blackhole'}})
    if mode == 'native':
        base = native(base)
    write(f'follow group reorder {mode}', base, edit=lambda p: p['proxy_groups'][0].update(manual_nodes=['node', 'DIRECT']))
    write(f'follow policy reject {mode}', base, edit=lambda p: p['rule_configs'][1].update(policy='REJECT'))
    off = copy.deepcopy(base)
    off['rule_configs'][1]['enabled'] = False
    write(f'disable followed {mode}', base, edit=lambda p: p['rule_configs'][1].update(enabled=False))
    apply_rule_dns(base, off, {}, base_url=BASE)
    write(f'reenable followed {mode}', off, edit=lambda p: p['rule_configs'][1].update(enabled=True))
    write(f'change disabled group {mode}', off, edit=lambda p: p['proxy_groups'][0].update(manual_nodes=['node', 'DIRECT']))
    on = copy.deepcopy(off)
    on['proxy_groups'][0]['manual_nodes'] = ['node', 'DIRECT']
    write(f'enable after group change {mode}', on, edit=lambda p: p['rule_configs'][1].update(enabled=True))
    explicit = profile(rule_actions={'a': {'action': 'nodata'}, 'b': {'action': 'refused'}, 'c': {'action': 'blackhole'}})
    if mode == 'native':
        explicit = native(explicit)
    write(f'delete multiple actions {mode}', explicit, edit=lambda p: p.update(rule_configs=p['rule_configs'][2:]))
    disabled = copy.deepcopy(explicit)
    disabled['rule_configs'][0]['enabled'] = False
    apply_rule_dns(explicit, disabled, {}, base_url=BASE)
    write(f'restore parked explicit {mode}', disabled, edit=lambda p: p['rule_configs'][0].update(enabled=True))

group_fields = [
    {'manual_nodes': ['DIRECT', 'node']}, {'manual_nodes': ['node', 'DIRECT']},
    {'manual_nodes': ['REJECT', 'DIRECT']},
    {'manual_nodes': ['DIRECT', 'node'], 'proxies_order': [{'type': 'node', 'id': 'node'}, {'type': 'node', 'id': 'DIRECT'}]},
    {'manual_nodes': ['node'], 'include_groups': ['direct'], 'proxy_order': 'strategies_first'},
    {'manual_nodes': ['node'], 'include_groups': ['direct']},
    {'source': 'node', 'proxies': ['DIRECT']}, {'source': 'strategy', 'proxies': ['direct']},
    {'subscriptions': ['subscription']}, {'manual_nodes': ['DIRECT'], 'subscriptions': ['subscription']},
    {'manual_nodes': ['DIRECT'], 'regex': 'Proxy`DIRECT'}, {'manual_nodes': ['DIRECT'], 'exclude-filter': 'DIRECT'},
    {'manual_nodes': ['DIRECT'], 'exclude-type': 'direct'},
    *[{'type': kind, 'manual_nodes': ['DIRECT']} for kind in ('url-test', 'fallback', 'load-balance', 'chain', 'unknown')],
    {'follow_group': 'direct'}, {'follow_group': 'missing'}, {'enabled': False, 'manual_nodes': ['DIRECT']}]
for index, fields in enumerate(group_fields):
    config = profile()
    config['subscriptions'] = [{'id': 'subscription', 'name': 'Synthetic subscription', 'enabled': True}]
    config['proxy_groups'] = [
        {'id': 'direct', 'name': 'Direct group', 'type': 'select', 'manual_nodes': ['DIRECT']},
        {'id': 'selection', 'name': 'Selection', 'type': 'select', 'enabled': True, **fields}]
    write(f'group default branch {index}', config, {'b': 'follow', 'mixed': 'follow'})

pin = {'action': 'upstream', 'source': 'remote', 'upstream': {'addr': 'https://dns.example/dns-query'}}
config = native(profile(rule_actions={'b': pin}, ip_selector={'enabled': True}, remote_ttl=0))
key = entry_key(pin['upstream'], 'remote')
config = edit_native(config, lambda d: next(p for p in d['plugins'] if p['tag'] == 'cf_dns_fwd_' + key)['args']['upstreams'][0].update(addr='udp://192.0.2.99'))
write('preserve hand-edited remote pin', config, {'c': 'refused'})
edited_key = entry_key({'addr': 'udp://192.0.2.99'}, 'remote')
write('retain current pin absent from catalog', config, {'b': 'upstream:' + edited_key})

for strict in (True, False):
    config = edit_native(native(profile()), lambda d: next(p for p in d['plugins'] if p['tag'] == 'forward_remote')['args']['upstreams'][0].update(addr='not an upstream'))
    bad_key = entry_key({'addr': 'not an upstream'}, 'remote')
    write(f'new validation error {strict}', config, {'a': 'upstream:' + bad_key}, strict=strict)
config = edit_native(native(profile(direct_rules=['a'])), lambda d: d['plugins'].append({'tag': 'legacy', 'type': 'http_server', 'args': {}}))
write('unchanged legacy validation error allowed', config, {'a': 'proxy'})

for mode in ('template', 'native'):
    for cache in (False, True):
        for features in (False, True):
            fields = {'cache_enabled': cache, 'cache_lazy_ttl': 300 if cache else 0, 'remote_ttl': 60 if features else 0}
            if features:
                router = {'address': '192.0.2.1:8728', 'username': 'audit', 'password': 'synthetic'}
                fields.update(ip_selector={'enabled': True},
                              ros_address_list={'enabled': True, 'args': router},
                              ros_route={'enabled': True, 'args': {**router, 'routing_table': 'main', 'gateway4': '192.0.2.254'}})
            config = profile(**fields)
            assignments = {'a': 'refused', 'b': 'nxdomain', 'c': 'nodata', 'mixed': 'blackhole'}
            for source, address in (('local', 'udp://192.0.2.53'), ('remote', 'https://dns.example/dns-query'), ('fallback', 'udp://192.0.2.54')):
                identity = 'pin_' + source
                config['rule_configs'].append({'id': identity, 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': identity + '.example', 'policy': 'DIRECT'})
                assignments[identity] = 'upstream:' + entry_key({'addr': address}, source)
            config['rule_configs'].append({'id': 'pin_duplicate', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'duplicate.example'})
            assignments['pin_duplicate'] = assignments['pin_fallback']
            if mode == 'native':
                config = native(config)
            write(f'all actions {mode}/{cache}/{features}', config, assignments)

for config in (profile(), native(profile())):
    record('upstream catalog', 'upstream_catalog', [config['oxidns'], None, []], upstream_catalog)
document = {'plugins': [{'tag': 'forward_local', 'type': 'forward', 'args': {'upstreams': [
    {'addr': 'udp://192.0.2.53', 'custom': {'keep': False}}, {'addr': 'udp://192.0.2.53', 'custom': {'keep': False}}, 'bad']}},
    {'tag': 'forward_remote', 'type': 'forward', 'args': {'upstreams': [{'addr': 'tls://dns.example', 'bootstrap': '192.0.2.54'}]}}]}
record('catalog deduplicates and links', 'upstream_catalog', [{}, document, [{'upstream_tag': 'forward_local', 'upstream_index': 1}]], upstream_catalog)
for library in ({'id': 'library', 'source_type': 'content', 'content': 'DOMAIN,a.example', 'enabled': False},
                {'id': 'library', 'url': 'https://rules.example/list', 'behavior': 'classical'}):
    record('hydrate source', 'hydrate_ruleset', [{'id': 'reference', 'policy': 'DIRECT', 'url': 'stale', 'format': 'yaml', 'future': True}, library, 'audit'], hydrate_ruleset)

path = Path(__file__).with_name('c27_public.jsonl.gz')
with gzip.open(path, 'wt') as output:
    for row in rows:
        output.write(json.dumps(row, ensure_ascii=False) + '\n')
generated = sum(row['fn'] == 'apply_rule_dns_boundary' and not row.get('err') and not row['out']['generation_rejected'] for row in rows)
print(f'{len(rows)} public references; {generated} successful final generated configurations -> {path.name}')
temporary.cleanup()
