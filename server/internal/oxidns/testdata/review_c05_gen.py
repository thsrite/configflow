#!/usr/bin/env python3
"""Independent C05 settings boundary matrix; execute only on test servers.

Every expected result, rejection and argument post-state comes from the real
Python normalizer. No implementation or validation collaborator is mocked.
"""
import ast
from collections import defaultdict
from copy import deepcopy
import gzip
import json
import logging
import os
from pathlib import Path
import sys
import tempfile

logging.disable(logging.CRITICAL)
temporary = tempfile.TemporaryDirectory(prefix='oxidns-c05-')
os.environ['DATA_DIR'] = temporary.name
from backend.common.oxidns_settings import get_default_oxidns_settings, normalize_oxidns_settings, safe_relative_path

ROWS = []
HITS = set()
SOURCE = 'backend/common/oxidns_settings.py'


def trace(frame, event, arg):
    if event == 'line' and frame.f_code.co_filename.endswith('/' + SOURCE):
        HITS.add(frame.f_lineno)
    return trace


def record(name, value, validate=True):
    args = [deepcopy(value), validate]
    row = {'test': name, 'fn': 'normalize', 'args': deepcopy(args)}
    try:
        row['out'] = normalize_oxidns_settings(args[0], validate_native_arguments=validate)
    except Exception as error:
        row['err'] = [type(error).__name__, str(error)]
        if hasattr(error, 'issues'):
            row['issues'] = error.issues
    row['args_after'] = args
    ROWS.append(row)


sys.settrace(trace)
defaults = get_default_oxidns_settings()
record('defaults', {})
for value in (None, [], '', 1, True):
    record(f'top-level-{value!r}', value)
active = deepcopy(defaults)
for section in ('query_recorder', 'ip_selector', 'dynamic_domain_set', 'adguard',
                'ros_address_list', 'ros_route', 'rule_update'):
    active[section]['enabled'] = True
active['cache_dump_enabled'] = True
active['adguard']['content'] = '||ads.example.com^'
for section in ('ros_address_list', 'ros_route'):
    active[section]['args'].update(address='192.0.2.1:8728', username='synthetic-user', password='synthetic-password')
active['ros_route']['args'].update(routing_table='synthetic-table', gateway4='192.0.2.254')
record('all-template-features', active)


def paths(value, prefix=()):
    for key, child in value.items():
        yield prefix + (key,)
        if isinstance(child, dict):
            yield from paths(child, prefix + (key,))


def changed(base, path, value):
    result = deepcopy(base)
    parent = result
    for key in path[:-1]:
        parent = parent[key]
    parent[path[-1]] = deepcopy(value)
    return result


for path in paths(defaults):
    # Structural nodes and every scalar are challenged in an otherwise valid,
    # fully enabled template, so inactive feature gates cannot hide failures.
    for index, value in enumerate((None, False, 0, 1, -1, 2**31, 10**30, 1.0, '', {}, [])):
        record(f'field/{".".join(path)}/{index}', changed(active, path, value))
for key, minimum, maximum in [('worker_threads', 1, 256), ('cache_size', 1, 2**31-1),
                              ('cache_lazy_ttl', 0, 2**31-1), ('remote_ttl', 0, 2**31-1)]:
    for value in (minimum-1, minimum, maximum, maximum+1):
        record(f'limit/{key}/{value}', {key: value})
for section in ('query_recorder', 'ip_selector', 'dynamic_domain_set', 'adguard',
                'ros_address_list', 'ros_route', 'rule_update'):
    record(f'inactive/{section}', {section: {'enabled': False, 'args': {'path': '../draft', 'queue_size': -1}}})
record('inactive-api', {'api': {'enabled': False, 'ssl': ['draft'], 'username': 'draft'}})
record('unknown-fields-merge', {'extra': {'nested': [False, None, 0, '中文']},
                               'api': {'extra': 7}, 'query_recorder': {'args': {'extra': 'preserved'}}})
for key in ('direct_rules', 'proxy_rules', 'direct_rulesets', 'proxy_rulesets', 'rule_dns_follow'):
    record(f'ordered-dedupe/{key}', {key: ['b', 'a', 'b', '', 'a']})
for suffix in ('rules', 'rulesets'):
    record(f'overlap/{suffix}', {f'direct_{suffix}': ['r'], f'proxy_{suffix}': ['r']})
for matches in ([], [None], [''], [' ', 'domain example.com'], 'domain example.com\r\n\nqtype A',
                'domain example.com\u2028qtype A', ['domain example.com', 1]):
    for enabled in (True, False, 0, 'yes'):
        record(f'custom-matches/{matches!r}/{enabled!r}', {'custom_matches': [{'matches': matches, 'exec': 'accept', 'enabled': enabled}]})
for value in ([], [1, 65535], [0], [65536], [True], [1.0], [10**30], '65'):
    record(f'qtypes/{value!r}', {'block_qtypes': value})
for rules in ({'r': None}, {'r': {'action': 'direct'}}, {'r': {'action': 'proxy'}},
              {'r': {'action': 'refused'}}, {'r': {'action': 'nxdomain'}}, {'r': {'action': 'nodata'}},
              {'r': {'action': 'blackhole'}},
              {'r': {'action': 'upstream', 'source': 'local', 'upstream': {'addr': 'udp://192.0.2.53', 'extra': True}}},
              {'r': {'action': 'reject'}}, {'r': {'action': 'bad'}}, {'r': 'direct'}):
    for mode in ('template', 'native'):
        record(f'rule-actions/{mode}/{rules!r}', {'config_mode': mode, 'native_config': 'plugins: []', 'rule_actions': rules})
for fields in ({'rule_actions': {'r': {'action': 'refused'}}, 'rule_dns_follow': ['r']},
               {'rule_dns_parked': {'r': {'action': 'proxy'}}, 'rule_dns_follow': ['r']},
               {'rule_actions': {'r': None}, 'rule_dns_follow': ['r', 'r']},
               {'rule_dns_parked': {'r': None}, 'rule_dns_follow': ['r']}):
    record(f'dns-choice/{fields!r}', fields)
for policies in ([{'ip': '192.0.2.7', 'name': '电视', 'mode': 'direct'}],
                 [{'ip': '2001:db8::7/128', 'mode': 'smart'}],
                 [{'ip': '::ffff:192.0.2.7/128', 'mode': 'direct'}],
                 [{'ip': '192.0.2.7', 'mode': 'direct'}, {'ip': '192.0.2.7/32', 'mode': 'smart'}],
                 [{'ip': '192.0.2.7', 'mode': 'bad'}]):
    record(f'device-policies/{policies!r}', {'device_policies': policies})
for tls in (None, False, {}, {'ca': 'ca.pem'}, {'insecure': True}, {'ca': 'ca.pem', 'insecure': True}):
    record(f'routeros-tls/{tls!r}', changed(active, ('ros_route', 'args', 'tls'), tls))
for schedule in ('30 3 * * *', ' 30\t3 * * *\n', '* * * *', '* * * * * *', 123):
    record(f'cron/{schedule!r}', {'rule_update': {'enabled': True, 'args': {'schedule': schedule}}})
for url in ('https://rules.example.com/list', 'http://rules.example.com/list', 'HTTPS://rules.example.com/list', 'file:///rules', 3):
    record(f'adguard-url/{url!r}', {'adguard': {'enabled': True, 'urls': [url]}})
for field, value in [(('api', 'ssl'), {'cert': 'cert.pem'}), (('api', 'password'), 'password'),
                     (('ros_route', 'args', 'max_ttl'), 0), (('ros_route', 'args', 'gateway4'), ''),
                     (('dynamic_domain_set', 'learn_args', 'phase'), 'bad'),
                     (('dynamic_domain_set', 'learn_args', 'error_mode'), 'bad')]:
    record(f'secondary-validation/{".".join(field)}', changed(active, field, value))
# Multiple faults make first-error/short-circuit order observable.
for fields in ({'worker_threads': 0, 'listen': ''},
               {'device_policies': None, 'config_mode': 'bad'},
               {'config_mode': 'bad', 'native_config': []},
               {'native_config': [], 'additional_files': None},
               {'additional_files': None, 'rule_dns_follow': None},
               {'dynamic_domain_set': {'enabled': True, 'route': 'bad', 'learn_args': None}},
               {'adguard': {'enabled': True, 'content': 3, 'urls': [3]}},
               {'api': {'username': 'u', 'ssl': None}}):
    record(f'first-error/{fields!r}', fields)

documents = [('', []), ('{}', []), ('plugins: []', []), ('plugins: null', []),
             ('plugins: [bad]', []), ('plugins: [{tag: main, type: sequence, args: []}]', []),
             ('plugins: [{tag: main, type: sequence, args: [{exec: "$missing"}]}]', []),
             ('plugins: [{tag: server, type: udp_server, args: {listen: ":53", entry: missing}}]', []),
             ('plugins: [{tag: f, type: forward, args: {upstreams: [{addr: "udp://192.0.2.53"}]}}]', []),
             ('plugins: [{tag: x, type: not_a_plugin, args: {}}]', []),
             ('plugins: []\nruntime: {worker_threads: 0}', []),
             ('plugins: []\ninclude: [child.yaml]', []),
             ('include: [child.yaml]\nplugins: []', [{'path': 'child.yaml', 'content': 'plugins: [{tag: main, type: sequence, args: []}]'}]),
             ('include: [child.yaml]\nplugins: []', [{'path': 'child.yaml', 'content': 'plugins: [{tag: main, type: sequence, args: [{exec: "$missing"}]}]'}]),
             ('include: [../child.yaml]\nplugins: []', []),
             ('include: [child.yaml]\nplugins: []', [{'path': 'child.yaml', 'content': 'include: [config.yaml]\nplugins: []'}]),
             ('plugins: [', []), ('plugins: []\nplugins: []', []), ('[1, 2]', []),
             ('plugins: []\nextra: !!python/object:unknown {}', [])]
for index, (text, attachments) in enumerate(documents):
    for validate in (False, True):
        record(f'native/{index}/{validate}', {'config_mode': 'native', 'native_config': text,
                                             'additional_files': attachments, 'worker_threads': 'inactive draft'}, validate)
for attachments in (None, {}, [None], [{'path': '../escape', 'content': ''}],
                    [{'path': 'config.yaml', 'content': 'plugins: []'}],
                    [{'path': 'a', 'content': 'x'}, {'path': 'a', 'content': 'y'}]):
    record(f'attachments/{attachments!r}', {'config_mode': 'native', 'native_config': 'plugins: []', 'additional_files': attachments}, False)
for path in ('./state/a', 'state/a', '', '.', './', '../a', '/a', 'a//b', 'a/./b', 'a/../b',
             'a\\b', 'a\0b', 'a\nb', '目录/中文.txt', 'a\x7fb', None, False):
    for label in ('文件路径', '缓存文件'):
        row = {'test': f'path/{path!r}/{label}', 'fn': 'safe_relative_path', 'args': [path, label], 'args_after': [path, label]}
        try:
            row['out'] = safe_relative_path(path, label)
        except Exception as error:
            row['err'] = [type(error).__name__, str(error)]
        ROWS.append(row)
sys.settrace(None)
output = Path(__file__).with_name('review_c05.jsonl.gz')
with output.open('wb') as stream, gzip.GzipFile(filename='', fileobj=stream, mode='wb', mtime=0) as compressed:
    for row in ROWS:
        compressed.write((json.dumps(row, ensure_ascii=False, separators=(',', ':')) + '\n').encode())
coverage = {node.name: sorted(HITS & set(range(node.lineno, node.end_lineno+1)))
            for node in ast.walk(ast.parse(Path(SOURCE).read_text())) if isinstance(node, ast.FunctionDef)}
output.with_suffix('').with_suffix('.coverage.json').write_text(json.dumps(coverage, indent=2) + '\n')
print(json.dumps({'rows': len(ROWS), 'rejections': sum('err' in row for row in ROWS), 'native_rows': sum(row['test'].startswith('native/') for row in ROWS)}))
