#!/usr/bin/env python3
"""Independent C27 adversarial public boundaries, synthetic data only.

Run via rt. Expected results come from the real Python entry points, including
the final delivered OxiDNS document. No implementation collaborator is mocked.
"""
import ast
from collections import Counter
from copy import deepcopy
import gzip
import json
import logging
import os
from pathlib import Path
import sys
import tempfile

logging.disable(logging.CRITICAL)
temporary = tempfile.TemporaryDirectory(prefix='review-c27-')
os.environ['DATA_DIR'] = temporary.name
import yaml
from backend.common.oxidns_editor import prepare_editor
from backend.common.oxidns_settings import normalize_oxidns_settings
from backend.common.rule_dns import (apply_rule_dns, entry_key, explicit_route, hydrate_ruleset,
    invalidate_follow, profile_agents, rule_dns_entries, rule_dns_state, upstream_catalog)
from backend.converters.oxidns import generate_oxidns_config

BASE = 'https://panel.example.com'
ROWS, HITS = [], set()
SOURCE = 'backend/common/rule_dns.py'


def trace(frame, event, arg):
    if event == 'line' and frame.f_code.co_filename.endswith('/' + SOURCE):
        HITS.add(frame.f_lineno)
    return trace


def record(name, fn, args, call):
    args = deepcopy(args)
    row = {'test': name, 'fn': fn, 'args': deepcopy(args)}
    try:
        row['out'] = call(*args)
    except Exception as error:
        row['err'] = [type(error).__name__, str(error)]
        if hasattr(error, 'usages'):
            row['usages'] = error.usages
    row['args_after'] = args
    ROWS.append(row)
    return row


def profile():
    return {'profile_id': 'review', 'system_config': {'default_profile_id': 'review', 'server_domain': BASE,
                                                     'rule_proxy_token': 'c27-synthetic-rule-token'},
            'agents': [], 'nodes': [{'id': 'n', 'name': 'Node', 'enabled': True}],
            'subscriptions': [], 'subscription_aggregations': [], 'rule_library': [], 'mosdns': {},
            'proxy_groups': [{'id': 'g', 'name': 'Group', 'type': 'select', 'manual_nodes': ['DIRECT', 'n']}],
            'rule_configs': [
                {'id': 'a', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'a.example.com', 'policy': 'Group'},
                {'id': 'b', 'itemType': 'rule', 'rule_type': 'DOMAIN-KEYWORD', 'value': 'example', 'policy': 'DIRECT'},
                {'id': 'set', 'itemType': 'ruleset', 'behavior': 'classical', 'source_type': 'content',
                 'content': 'DOMAIN,set.example.com', 'url': 'https://rules.example.com/list', 'policy': 'Group'}],
            'oxidns': normalize_oxidns_settings({'local_dns': 'udp://192.0.2.53', 'remote_dns': 'tls://dns.example.com',
                                               'fallback_dns': 'udp://192.0.2.54', 'extra': {'unknown': [False, 0, '中文']}})}


def native(p):
    p = deepcopy(p)
    p['oxidns'] = prepare_editor(p['oxidns'], p, BASE)
    p['oxidns']['native_config'] = '# retained C27 draft\n' + p['oxidns']['native_config']
    return p


def edit_document(p, edit):
    p = deepcopy(p)
    document = yaml.safe_load(p['oxidns']['native_config'])
    edit(document)
    p['oxidns']['native_config'] = '# retained C27 draft\n' + yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    return p


def boundary(before, after, assignments, strict):
    warnings = apply_rule_dns(before, after, assignments, base_url=BASE, strict=strict)
    generated, rejected = None, False
    try:
        generated = yaml.safe_load(generate_oxidns_config(after, BASE))
    except Exception:
        rejected = True
    return {'warnings': warnings, 'settings': after['oxidns'], 'state': rule_dns_state(after, BASE),
            'generated': generated, 'generation_rejected': rejected}


def write(name, before, assignments=None, mutate=None, strict=True):
    after = deepcopy(before)
    if mutate:
        mutate(after)
    return record(name, 'apply_rule_dns_boundary', [before, after, assignments or {}, strict], boundary)


sys.settrace(trace)
choices = ('follow', 'default', 'direct', 'proxy', 'refused', 'nxdomain', 'nodata', 'blackhole')
events = {
    'disable': lambda p: p['rule_configs'][0].update(enabled=False),
    'library-disable': lambda p: p['rule_configs'][2].update(library_enabled=False),
    'reverse': lambda p: p['rule_configs'].reverse(),
    'delete-other': lambda p: p['rule_configs'].pop(1),
    'policy-reject': lambda p: p['rule_configs'][0].update(policy='REJECT'),
    'group-reorder': lambda p: p['proxy_groups'][0].update(manual_nodes=['n', 'DIRECT']),
    'domain-to-ruleset': lambda p: p['rule_configs'][0].update(itemType='ruleset', behavior='domain',
        source_type='content', content='DOMAIN,a.example.com', url='https://rules.example.com/new'),
    'classical-gains-ip': lambda p: p['rule_configs'][2].update(content='DOMAIN,set.example.com\nIP-CIDR,192.0.2.0/24'),
    'classical-to-ip': lambda p: p['rule_configs'][2].update(behavior='ipcidr'),
}
for mode in ('template', 'native'):
    for aggregate in (False, True):
        seed = profile()
        seed['oxidns']['aggregate_rules'] = aggregate
        seed['oxidns'].update(direct_rules=['a'], rule_dns_follow=['a'],
                              rule_actions={'b': {'action': 'refused'}, 'set': {'action': 'blackhole'}})
        if mode == 'native':
            seed = native(seed)
        for event, mutate in events.items():
            write(f'transition/{mode}/{aggregate}/{event}/implicit', seed, mutate=mutate)
            for choice in choices:
                write(f'transition/{mode}/{aggregate}/{event}/{choice}', seed,
                      {'a': choice, 'b': 'default'}, mutate)

# Multi-step writes expose parked/follow restoration after several intervening
# group/type/source/enable edits, rather than only one isolated transition.
for mode in ('template', 'native'):
    p = profile()
    if mode == 'native':
        p = native(p)
    for index, (assignments, mutate) in enumerate([
        ({'a': 'follow', 'set': 'nodata'}, None),
        ({}, events['disable']), ({}, events['group-reorder']),
        ({'a': 'refused'}, None), ({}, lambda p: p['rule_configs'][0].update(enabled=True)),
        ({'a': 'follow'}, None), ({}, events['domain-to-ruleset']),
        ({}, lambda p: p['rule_configs'][2].update(enabled=False)),
        ({'set': 'follow'}, None), ({}, events['group-reorder']),
        ({}, lambda p: p['rule_configs'][2].update(enabled=True)),
        ({'a': 'default', 'set': 'default'}, None),
    ]):
        after = deepcopy(p)
        if mutate:
            mutate(after)
        record(f'sequence/{mode}/{index}', 'apply_rule_dns_boundary', [p, after, assignments, True], boundary)
        try:
            apply_rule_dns(p, after, assignments, base_url=BASE)
            p = after
        except Exception:
            pass

# Native conflicts: unrelated custom structures, owned helper edits, invalid
# added pins, partial validation failures, no-op and strict/lenient paths.
for mutate_name, mutate in [
    ('blackhole-extra', lambda d: next(p for p in d['plugins'] if p['tag'] == 'cf_dns_blackhole')['args'].update(extra=True)),
    ('main-custom', lambda d: next(p for p in d['plugins'] if p['tag'] == 'sequence_main')['args'].insert(0, {'exec': 'sleep 1ms'})),
    ('duplicate-tag', lambda d: d['plugins'].append(deepcopy(d['plugins'][0]))),
    ('invalid-unused', lambda d: d['plugins'].append({'tag': 'unused', 'type': 'http_server', 'args': {}})),
]:
    p = profile()
    p['oxidns']['rule_actions'] = {'b': {'action': 'blackhole'}}
    p = edit_document(native(p), mutate)
    for strict in (False, True):
        for name, assignments, edit in [
            ('noop', {}, None), ('unrelated-edit', {}, lambda p: p['rule_configs'][0].update(remark='changed')),
            ('explicit-edit', {'a': 'proxy'}, None), ('delete', {}, events['delete-other']),
            ('follow-update', {'a': 'follow'}, events['group-reorder']),
        ]:
            write(f'locked/{mutate_name}/{strict}/{name}', p, assignments, edit, strict)

for mode in ('template', 'native'):
    p = profile()
    pin = {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.53'}}
    p['oxidns']['rule_actions'] = {'a': pin}
    p['agents'] = [{'id': 'dns', 'name': 'DNS', 'service_type': 'oxidns', 'profile_id': 'review',
                    'dns_follow': {'upstream_tag': 'forward_local', 'upstream_index': 0}}]
    if mode == 'native':
        p = native(p)
    key = 'upstream:' + entry_key(pin['upstream'], 'local')
    for strict in (False, True):
        write(f'linked-current-retained/{mode}/{strict}', p, {'a': key}, strict=strict)
        write(f'linked-second-fails-atomically/{mode}/{strict}', p, {'b': 'blackhole', 'set': key}, strict=strict)
        write(f'stale-second-fails-atomically/{mode}/{strict}', p,
              {'a': 'default', 'b': 'upstream:000000000000'}, strict=strict)

# Independent source refresh followed by a DNS transaction and final generation.
for library in [
    {'id': 'lib', 'name': 'Inline', 'source_type': 'content', 'content': 'DOMAIN,source.example.com'},
    {'id': 'lib', 'name': 'Remote', 'url': 'https://rules.example.com/new', 'behavior': 'classical', 'format': 'text'},
    {'id': 'lib', 'name': 'Disabled', 'source_type': 'content', 'content': 'DOMAIN,x.example.com', 'enabled': False},
]:
    for profile_id in ('', 'review'):
        item = {'id': 'set', 'itemType': 'ruleset', 'policy': 'Group', 'library_rule_id': 'lib',
                'name': 'stale', 'format': 'yaml', 'content': 'stale', 'url': 'stale', 'source_type': 'url',
                'behavior': 'domain', 'library_enabled': False, 'unknown': {'retain': [0, False]}}
        record(f'hydrate/{library["name"]}/{profile_id}', 'hydrate_ruleset', [item, library, profile_id], hydrate_ruleset)
        for mode in ('template', 'native'):
            p = profile()
            p['rule_configs'][2].update(library_rule_id='lib')
            p['rule_library'] = [library]
            p['oxidns']['direct_rulesets'] = ['set']
            if mode == 'native':
                p = native(p)
            write(f'hydrated-write/{library["name"]}/{profile_id}/{mode}', p, {'set': 'follow'},
                  lambda p: hydrate_ruleset(p['rule_configs'][2], library, profile_id))

# Catalog semantics: duplicate tags, duplicate complete entries, original index
# binding, same address/different attributes, and parked backups never offered.
for source in ('local', 'remote', 'fallback'):
    for kind in ('empty', 'duplicate-tag', 'wrong-type', 'bad-shape', 'entries'):
        document = {'plugins': [{'tag': 'forward_' + source, 'type': 'forward', 'args': {'upstreams': []}}]}
        plugin = document['plugins'][0]
        if kind == 'duplicate-tag':
            document['plugins'].append(deepcopy(plugin))
        elif kind == 'wrong-type':
            plugin['type'] = 'sequence'
        elif kind == 'bad-shape':
            plugin['args']['upstreams'] = {'addr': 'udp://192.0.2.53'}
        elif kind == 'entries':
            plugin['args']['upstreams'] = [None, 'bad', {'addr': ''}, {'addr': ' '},
                {'addr': 'udp://192.0.2.53', 'bootstrap': '192.0.2.54'},
                {'bootstrap': '192.0.2.54', 'addr': 'udp://192.0.2.53'}, {'addr': 'udp://192.0.2.53'}]
        record(f'catalog/{source}/{kind}', 'upstream_catalog', [
            {'config_mode': 'native', '_common_disabled': {'forward_' + source: plugin}}, document,
            [{'upstream_tag': 'forward_' + source, 'upstream_index': 5}]], upstream_catalog)

# Entry keys are persisted in native helper tags and sent back as choices.
# A valid free-form upstream tag must survive JSON/YAML scalar escaping.
for separator in ('\u2028', '\u2029', r'\u2028'):
    for source in ('local', 'remote', 'fallback'):
        entry = {'addr': 'udp://192.0.2.53', 'tag': 'slot' + separator + 'one'}
        p = profile()
        p['oxidns'][source + '_dns'] = yaml.safe_dump([entry], allow_unicode=True, sort_keys=False)
        native_keys = {
            '\u2028': {'local': '6a83730ec5ea', 'remote': 'b4bb15462234', 'fallback': '776c9ede99e2'},
            '\u2029': {'local': '39ad037190b5', 'remote': '8ced42555070', 'fallback': 'c62eb1bd8dbb'},
        }
        def mark(row, stage):
            if separator in native_keys:
                row['d2'] = {'stage': stage, 'source': source, 'legacy_key': entry_key(entry, source),
                             'native_key': native_keys[separator][source]}
                if stage == 'read':
                    row['d2']['delivered'] = yaml.safe_load(generate_oxidns_config(row['args'][0], BASE))
        for mode in ('template', 'native'):
            before = native(p) if mode == 'native' else p
            mark(record(f'persisted-key/catalog/{separator!r}/{source}/{mode}', 'upstream_catalog',
                        [before['oxidns'], None, []], upstream_catalog), 'catalog')
            mark(write(f'persisted-key/select/{separator!r}/{source}/{mode}', before,
                       {'a': 'upstream:' + entry_key(entry, source)}), 'select')
        pinned = deepcopy(p)
        pinned['oxidns']['rule_actions'] = {'a': {'action': 'upstream', 'source': source, 'upstream': entry}}
        pinned = native(pinned)
        mark(record(f'persisted-key/read-existing/{separator!r}/{source}', 'rule_dns_state', [pinned], rule_dns_state), 'read')
        mark(write(f'persisted-key/rebuild-existing/{separator!r}/{source}', pinned, {'b': 'refused'}), 'rebuild')

for length in (2, 63, 64, 65):
    p = profile()
    p['proxy_groups'] = [{'id': f'g{i}', 'name': f'Group{i}', 'type': 'select',
        **({'include_groups': [f'g{i+1}']} if i+1 < length else {'manual_nodes': ['DIRECT']})} for i in range(length)]
    p['rule_configs'][0]['policy'] = 'Group0'
    record(f'group-depth/{length}', 'rule_dns_state', [p], rule_dns_state)
for agent in ({'id': 'legacy', 'name': 'Legacy'},
              {'id': 'legacy', 'name': None, 'service_type': 'mihomo'},
              {'id': None, 'service_type': 'mihomo'},
              {'id': '', 'name': '', 'service_type': ''},
              {'id': 'dns', 'name': 'DNS', 'service_type': 'oxidns', 'rule_sync': {'settings': {'library': 1, 'rules': True}}}):
    p = profile()
    p['agents'] = [agent]
    record(f'agent-read/{agent!r}', 'rule_dns_state', [p], rule_dns_state)
    record(f'profile-agents/{agent!r}', 'profile_agents', [p], profile_agents)

for mode in ('template', 'native'):
    p = profile()
    p['oxidns'].update(direct_rules=['a'], rule_dns_follow=['a', 'b'])
    p['rule_configs'][1]['enabled'] = False
    if mode == 'native':
        p = native(p)
    replacement = deepcopy(p['oxidns'])
    record(f'invalidate-unchanged/{mode}', 'invalidate_follow', [p, replacement], invalidate_follow)
    changed = deepcopy(p)
    apply_rule_dns(p, changed, {'a': 'proxy'}, base_url=BASE)
    replacement = changed['oxidns']
    replacement['rule_dns_follow'] = ['a', 'b']
    record(f'invalidate-active-only/{mode}', 'invalidate_follow', [p, replacement], invalidate_follow)

sys.settrace(None)
output = Path(__file__).with_name('review_c27.jsonl.gz')
with output.open('wb') as stream, gzip.GzipFile(filename='', fileobj=stream, mode='wb', mtime=0) as compressed:
    for row in ROWS:
        compressed.write((json.dumps(row, ensure_ascii=False, separators=(',', ':')) + '\n').encode())
coverage = {node.name: sorted(HITS & set(range(node.lineno, node.end_lineno+1)))
            for node in ast.walk(ast.parse(Path(SOURCE).read_text())) if isinstance(node, ast.FunctionDef)}
output.with_suffix('').with_suffix('.coverage.json').write_text(json.dumps(coverage, indent=2) + '\n')
generated = sum(r['fn'] == 'apply_rule_dns_boundary' and 'out' in r and not r['out']['generation_rejected'] for r in ROWS)
print(json.dumps({'rows': len(ROWS), 'generated': generated, 'rejections': sum('err' in r for r in ROWS),
                  'functions': dict(Counter(r['fn'] for r in ROWS))}))
