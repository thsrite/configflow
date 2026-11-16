"""Fresh public-boundary references for the C22-C26 coverage audit.

Run only via rt on the test server. Inputs are synthetic, DATA_DIR is temporary,
and expected outputs come from the Python implementation without monkeypatches.
"""
import copy
import gzip
import hashlib
import json
import os
import tempfile
from pathlib import Path

temporary = tempfile.TemporaryDirectory(prefix='oxidns-c22c26-')
os.environ['DATA_DIR'] = temporary.name

import yaml
from backend.common.dns_watchdog import WATCHDOG_RULE, ensure_local_dns_watchdog
from backend.common.oxidns_common import apply_common_settings, read_common_settings
from backend.common.oxidns_device_policy import apply_policies, read_policies
from backend.common.oxidns_editor import prepare_editor, preview_template
from backend.common.oxidns_native import load_native_yaml
from backend.common.oxidns_validation import validate_native_config

rows = []


def record(name, fn, args, call):
    args = copy.deepcopy(args)
    row = {'test': name, 'fn': fn, 'args': copy.deepcopy(args)}
    try:
        row['out'] = call(*args)
    except Exception as exc:
        row['err'] = [type(exc).__name__, str(exc)]
        if hasattr(exc, 'issues'):
            row['issues'] = exc.issues
    row['args_after'] = args
    rows.append(row)


for text in [
    'plugins: []', 'plugins: [{tag: main, type: sequence, args: []}]',
    'plugins: []\nextra: {flag: false, zero: 0, value: null}',
    'plugins: []\nvalue: |\n \n 行',
    'plugins: [{tag: main, type: sequence, args: [{exec: "$missing"}]}]',
    'plugins: [{tag: x, type: udp_server, args: {listen: ":53", entry: missing}}]',
    'plugins: [{tag: a, type: sequence, args: [{exec: "$b"}]}, {tag: b, type: sequence, args: [{exec: "$a"}]}]',
]:
    record('native loader', 'load_native_yaml', [text], load_native_yaml)
    record('native validation', 'validate_native_config', [text], validate_native_config)


def watchdog_doc(kind='udp_server', entry='main', target='sequence'):
    args = [{'exec': 'reject 0'}] if target == 'sequence' else {'upstreams': [{'addr': 'udp://192.0.2.53'}]}
    server_args = {'listen': ':5300', 'entry': entry}
    if kind == 'http_server':
        server_args = {'listen': ':8053', 'entries': [{'path': '/dns-query', 'exec': entry}]}
    return {'plugins': [{'tag': 'main', 'type': target, 'args': args},
                        {'tag': 'server', 'type': kind, 'args': server_args}]}


documents = [{}, {'include': ['external.yaml'], 'plugins': None}, {'plugins': None},
             {'plugins': ['bad']}, {'plugins': [{'tag': ['bad']}]},
             {'plugins': [{'tag': 'x'}, {'tag': 'x'}]}]
for kind in ('udp_server', 'tcp_server', 'quic_server', 'http_server'):
    for target in ('sequence', 'forward'):
        document = watchdog_doc(kind, target=target)
        documents += [document, ensure_local_dns_watchdog(document)]
    documents += [watchdog_doc(kind, '${ENTRY}'), watchdog_doc(kind, 'missing')]
    broken = watchdog_doc(kind)
    broken['plugins'][-1]['args'] = None
    documents.append(broken)
for entries in (None, ['bad'], [], [{'exec': 'main'}, {'exec': 'main'}]):
    document = watchdog_doc('http_server')
    document['plugins'][-1]['args']['entries'] = entries
    documents.append(document)
document = watchdog_doc()
document['plugins'][0]['args'] = [copy.deepcopy(WATCHDOG_RULE), {'exec': 'reject 0'}, copy.deepcopy(WATCHDOG_RULE)]
documents.append(document)
document = watchdog_doc(target='forward')
collision = 'cf_watchdog_' + hashlib.sha256(b'main').hexdigest()[:16]
document['plugins'].insert(1, {'tag': collision, 'type': 'sequence', 'args': []})
document['plugins'].append({'tag': 'second', 'type': 'tcp_server', 'args': {'listen': ':5300', 'entry': 'main'}})
documents.append(document)
document = watchdog_doc()
document['plugins'][0]['args'] = {}
documents.append(document)
for index, document in enumerate(documents):
    record(f'watchdog branch {index}', 'ensure_local_dns_watchdog', [document], ensure_local_dns_watchdog)

profile = {'profile_id': 'audit', 'system_config': {}, 'rule_configs': [],
           'agents': [], 'device_notes': {}}
settings = {'local_dns': 'udp://192.0.2.53', 'remote_dns': 'udp://192.0.2.54'}
draft = prepare_editor(settings, profile)
for changes in ({'cache_enabled': False}, {'cache_enabled': True}, {'prefer_ipv4': True},
                {'cache_size': 0}, {'listen': '127.0.0.1:5300'},
                {'custom_hosts': '打印机.example 192.0.2.10'}, {'unknown': True},
                {'cache_enabled': 'yes'}, None):
    record('common patch', 'apply_common_settings', [draft, changes, profile], apply_common_settings)
record('common read', 'read_common_settings', [draft, profile], read_common_settings)

direct = [{'ip': '192.0.2.10', 'name': '电视', 'mode': 'direct'}]
with_policy = apply_policies(draft, profile, direct)
mixed = direct + [{'ip': '192.0.2.0/24', 'name': '网段', 'mode': 'direct'},
                  {'ip': '192.0.2.11', 'name': '例外', 'mode': 'smart'}]
for source in (draft, with_policy):
    for policies in ([], direct, mixed, [{'ip': '::ffff:192.0.2.10/128', 'mode': 'smart'}],
                     direct + direct, [{'ip': '192.0.2.10', 'mode': 'bad'}]):
        record('policy lifecycle', 'apply_policies', [source, profile, policies], apply_policies)
    record('policy read', 'read_policies', [source, profile], read_policies)

for listen, entry in ((':53', 'side'), (':5300', 'side'), ('${LISTEN}', 'side'), (':5300', 'sequence_main')):
    current = copy.deepcopy(draft)
    document = yaml.safe_load(current['native_config'])
    document['plugins'] += [{'tag': 'side', 'type': 'sequence', 'args': [{'exec': 'reject 0'}]},
                            {'tag': 'side_server', 'type': 'udp_server', 'args': {'listen': listen, 'entry': entry}}]
    current['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    record('policy side listener', 'apply_policies', [current, profile, direct], apply_policies)
    record('policy side report', 'read_policies', [current, profile], read_policies)

start = prepare_editor({**settings, 'worker_threads': 2}, profile)
document = yaml.safe_load(start['native_config'])
document['runtime']['worker_threads'] = 5
current = {**start, 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
proposed = {**start['template_settings'], 'worker_threads': 7}
for resolutions in (None, {}, {'/runtime/worker_threads': 'current'},
                    {'/runtime/worker_threads': 'template'}, [], {'/runtime': 'bad'}):
    record('editor conflict choice', 'preview_template', [current, proposed, profile, '', resolutions], preview_template)
record('native open inactive broken template', 'prepare_editor',
       [{**current, 'template_settings': {'listen': []}}, profile], prepare_editor)

path = Path(__file__).with_name('review_c22c26.jsonl.gz')
with gzip.open(path, 'wt') as output:
    for row in rows:
        output.write(json.dumps(row, ensure_ascii=False) + '\n')
print(f'{len(rows)} fresh references -> {path.name}')
temporary.cleanup()
