"""Random corpus for module C26 (OxiDNS editor, device policy, rule-update
pipeline), run with real Python through the module's public entry points
and recorded with c26_record (same rows as c26_recorded.jsonl.gz, replayed
by TestC26Corpus). Synthetic values only (example.test, 192.0.2.0/24,
198.51.100.0/24, 2001:db8::/32).

    rt --back server/internal/oxidns/testdata/c26_corpus.jsonl.gz -- bash -lc \\
        'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore \\
         server/internal/oxidns/testdata/gen_c26.py'

Cases: device policies (random rows of hosts/CIDRs/IPv6/mapped/invalid
values and modes; native drafts with extra listeners: node-test chains,
isolated side chains, client-port and env listeners, http_server entries,
broken sequence_main / forward_local, includes, duplicate tags, foreign
hooks) through apply_policies, read_policies and a second apply; editor
(template inputs incl. rule selections, parked/inactive rules, hosts and
AdGuard content, hand-edited native documents) through prepare_editor and
preview_template with random resolutions; rule-update pipeline (ruleset
sources with absolute and relative URLs, base URLs, tokens, profiles)
through prepare_native_rule_updates, rebase_native_rule_sources and
apply_common_settings (prepare/finish_rule_updates, reconcile).
"""
import copy
import gzip
import json
import os
import random
import sys
import tempfile

os.environ.setdefault('DATA_DIR', tempfile.mkdtemp(prefix='c26gen-'))

import yaml  # noqa: E402

import c26_record as rec  # noqa: E402

rec._install()

from backend.common.oxidns_common import apply_common_settings  # noqa: E402
from backend.common.oxidns_common_updates import prepare_native_rule_updates, rebase_native_rule_sources  # noqa: E402
from backend.common.oxidns_device_policy import apply_policies, read_policies  # noqa: E402
from backend.common.oxidns_editor import prepare_editor, preview_template  # noqa: E402
from backend.common.oxidns_native import native_document  # noqa: E402
from backend.common.oxidns_settings import OxidnsSettingsError  # noqa: E402

R = random.Random(2626)
SCALE = float(sys.argv[1]) if len(sys.argv) > 1 else 1.0
HOST = '198.51.100.2'


def n(count):
    return max(1, int(count * SCALE))


def maybe(p=0.5):
    return R.random() < p


def call(fn, *args):
    try:
        return fn(*args)
    except Exception:
        return None


def ip_value():
    return R.choice([
        f'198.51.100.{R.randint(0, 255)}', f'192.0.2.{R.randint(1, 254)}/{R.randint(16, 32)}',
        f'2001:db8::{R.randint(1, 9999):x}', f'2001:db8:{R.randint(0, 99):x}::/{R.randint(32, 128)}',
        f'::ffff:198.51.100.{R.randint(1, 254)}', f'::ffff:192.0.2.0/{R.randint(90, 128)}',
        '0.0.0.0', '224.0.0.1', '239.0.0.0/8', '198.51.100.300', ' 198.51.100.7 ', '198.51.100.0/33',
        '1.2.3.4/x', 12, None, '::', '198.51.100.1/24',
    ])


def rows():
    out = []
    for _ in range(R.randint(0, 6)):
        row = {'ip': ip_value(), 'mode': R.choice(['direct', 'smart', 'direct', 'proxy'])}
        if maybe(0.7):
            row['name'] = R.choice(['电视', 'TV', '', ' 客厅 ', 'x' * 81, 'a\x7fb', 'tab\t', 5])
        if maybe(0.03):
            row['extra'] = 1
        out.append(row)
    return out


def config(profile='default'):
    servers = [f'{HOST}:{R.choice([15353, 53, 5353])}', f'198.51.100.{R.randint(2, 9)}', 'https://dns.example.test/q']
    system = {'node_test_dns': R.sample(servers, R.randint(0, 2)), 'rule_proxy_token': 'test-only',
              'server_domain': R.choice(['http://config.test', 'https://config.test:8443', ''])}
    if maybe(0.3):
        system['node_test_dns_config'] = {'mode': R.choice(['custom', 'auto']), 'servers': R.sample(servers, 1)}
    if maybe(0.3):
        system['retired_rule_proxy_tokens'] = ['old-token', 3]
    value = {'oxidns': {'query_recorder': {'enabled': maybe()}}, 'profile_id': profile, 'system_config': system,
             'agents': [{'id': 'dns', 'service_type': 'oxidns', 'profile_id': R.choice([profile, 'other']),
                         'host': R.choice([HOST, '198.51.100.9'])}],
             'rule_configs': rule_space()}
    if maybe(0.3):
        value['device_notes'] = {R.choice(['198.51.100.21', '192.0.2.0/24']): R.choice(['', '书房', 'n'])}
    return value


def rule_space():
    space = []
    for index in range(R.randint(0, 5)):
        kind = R.choice(['ruleset', 'rule'])
        if kind == 'ruleset':
            space.append({'id': f'set{index}', 'itemType': 'ruleset', 'behavior': R.choice(['domain', 'ipcidr']),
                          'url': R.choice([f'https://example.test/r{index}.txt', f'/api/files/r{index}.txt', '']),
                          'enabled': maybe(0.85)})
        else:
            space.append({'id': f'rule{index}', 'itemType': 'rule', 'rule_type': R.choice(['DOMAIN', 'DOMAIN-SUFFIX', 'IP-CIDR']),
                          'value': R.choice([f'site{index}.example.test', '192.0.2.0/24']), 'enabled': maybe(0.85)})
    return space


def listeners():
    prefix = R.choice(['node_test_real', 'side', 'cf_node_test_0123'])
    listen = R.choice([f'{HOST}:15353', ':15353', '0.0.0.0:5353', f'{HOST}:53', '${LISTEN}', '[::]:15353', ''])
    chain = [
        {'tag': prefix + '_forward', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://198.51.100.53:53'}]}},
        {'tag': prefix + '_sequence', 'type': 'sequence', 'args': [
            {'exec': '$' + prefix + '_forward'}, {'matches': ['!has_resp'], 'exec': 'reject 2'}, {'exec': 'accept'}]},
        {'tag': prefix + '_udp', 'type': R.choice(['udp_server', 'udp_server', 'http_server']),
         'args': {'listen': listen, 'entry': prefix + '_sequence'}},
    ]
    if chain[2]['type'] == 'http_server':
        chain[2]['args'] = {'listen': listen, 'entries': [{'path': '/dns-query', 'exec': prefix + '_sequence'}]}
    if maybe(0.5):
        chain.append({'tag': prefix + '_tcp', 'type': 'tcp_server', 'args': {'listen': listen, 'entry': prefix + '_sequence'}})
    if maybe(0.15):
        chain[1]['args'].insert(0, {'exec': R.choice(['goto sequence_main', '$missing_plugin', '${ENV}', '$forward_local'])})
    if maybe(0.1):
        chain[2]['args']['entry'] = R.choice(['sequence_main', '', 5])
    return chain


def mutate_policy_document(document):
    plugins = document['plugins']
    if maybe(0.5):
        plugins.extend(listeners())
    main = next((p for p in plugins if p.get('tag') == 'sequence_main'), None)
    roll = R.random()
    if roll < 0.05 and main:
        main['args'][-1] = {'exec': R.choice(['accept', 'goto proxy_dns_seq']), 'matches': ['qtype 1']}
    elif roll < 0.1 and main:
        main['args'].append({'exec': 'accept', 'x': 1})
    elif roll < 0.13 and main:
        main['type'] = 'fallback'
    elif roll < 0.16:
        document['plugins'] = [p for p in plugins if p.get('tag') != 'forward_local']
    elif roll < 0.19:
        plugins.append(copy.deepcopy(plugins[0]))
    elif roll < 0.22 and main:
        main['args'].insert(0, {'exec': 'jump cf_device_policy'})
    elif roll < 0.25:
        document['include'] = ['extra.yaml']
    elif roll < 0.28 and main:
        main['args'].insert(0, {'exec': '$cf_device_direct'})


def native_settings(cfg, base, extra_mutate=None):
    settings = call(prepare_editor, cfg['oxidns'], cfg, base)
    if settings is None:
        return None
    document = yaml.safe_load(settings['native_config'])
    if extra_mutate:
        extra_mutate(document)
    settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    return settings


def device_policy_cases():
    for _ in range(n(500)):
        cfg = config(R.choice(['default', 'office']))
        settings = native_settings(cfg, '', mutate_policy_document if maybe(0.8) else None)
        if settings is None:
            continue
        cfg['oxidns'] = settings
        first = call(apply_policies, settings, cfg, rows())
        target = first if first is not None else settings
        cfg['oxidns'] = target
        call(read_policies, target, cfg)
        call(apply_policies, target, cfg, rows() if maybe(0.7) else copy.deepcopy(target.get('device_policies', [])))
        if maybe(0.2):
            # A hand edit of the owned chain after patching.
            document = yaml.safe_load(target['native_config'])
            for plugin in document.get('plugins', []):
                if plugin.get('tag') == 'cf_device_policy':
                    plugin['args'].insert(0, {'exec': 'return'})
            target = {**target, 'native_config': yaml.safe_dump(document, allow_unicode=True, sort_keys=False)}
            call(apply_policies, target, cfg, rows())
        call(read_policies, {'device_policies': rows()}, cfg)


def template_inputs(cfg):
    ids = [item['id'] for item in cfg['rule_configs']]
    value = {}
    for key in ('direct_rules', 'proxy_rules', 'direct_rulesets', 'proxy_rulesets'):
        if maybe(0.5) and ids:
            value[key] = R.sample(ids, R.randint(0, len(ids)))
    if maybe(0.3):
        value['custom_hosts'] = R.choice(['', 'example.test 192.0.2.1\n', 'a.example.test 192.0.2.9\n# c\n'])
    if maybe(0.3):
        value['adguard'] = {'enabled': maybe(), 'urls': R.sample(['https://example.test/ag1.txt', 'https://example.test/ag2.txt'], R.randint(0, 2)),
                            'content': R.choice(['', '||ads.example.test^\n'])}
    if maybe(0.3):
        value['worker_threads'] = R.randint(1, 8)
    if maybe(0.3):
        value['rule_update'] = {'enabled': maybe()}
    if maybe(0.2) and ids:
        value['rule_actions'] = {R.choice(ids): R.choice([{'action': 'reject'}, {'action': 'direct'}, {'action': 'proxy'}])}
    if maybe(0.15) and ids:
        value['rule_dns_parked'] = {R.choice(ids): {'action': R.choice(['direct', 'proxy', 'reject'])}}
    return value


def edit_document(document):
    plugins = document.get('plugins', [])
    roll = R.random()
    if roll < 0.2 and plugins:
        R.choice(plugins)['x_user'] = 'kept'
    elif roll < 0.35 and plugins:
        plugins.insert(R.randint(0, len(plugins)), {'tag': 'user_seq', 'type': 'sequence', 'args': [{'exec': 'accept'}]})
    elif roll < 0.45 and len(plugins) > 2:
        plugins.pop(R.randrange(len(plugins)))
    elif roll < 0.55 and len(plugins) > 2:
        i, j = R.sample(range(len(plugins)), 2)
        plugins[i], plugins[j] = plugins[j], plugins[i]
    elif roll < 0.65:
        for plugin in plugins:
            if plugin.get('type') == 'forward':
                plugin['args']['upstreams'] = [{'addr': 'udp://192.0.2.53'}]


def editor_cases():
    for _ in range(n(500)):
        cfg = config()
        inputs = template_inputs(cfg)
        base = R.choice(['', 'http://config.test'])
        if maybe(0.3):
            inputs['native_config'] = R.choice(['', 'plugins: []\n', 'plugins: [{tag: a, type: sequence, args: []}]\n'])
        current = call(prepare_editor, inputs, cfg, base)
        if current is None:
            continue
        if maybe(0.6):
            document = yaml.safe_load(current['native_config'])
            edit_document(document)
            current['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
        if maybe(0.15):
            current['template_base_config'] = R.choice(['', yaml.safe_load(current['template_base_config'])])
        proposed = template_inputs(cfg)
        resolutions = None
        if maybe(0.5):
            resolutions = {R.choice(['/plugins/forward_local/args/upstreams', '/resources/custom_hosts',
                                     '/resources/adguard/content', '/log']): R.choice(['current', 'template'])}
        if maybe(0.03):
            resolutions = {'/x': 'other'}
        if maybe(0.03):
            proposed = ['not', 'a', 'dict']
        call(preview_template, current, proposed, cfg, base, resolutions)
        if maybe(0.3):
            call(prepare_editor, current, cfg, base)


def pipeline_cases():
    for _ in range(n(400)):
        cfg = config(R.choice(['default', 'office', None]))
        ids = [item['id'] for item in cfg['rule_configs'] if item['itemType'] == 'ruleset']
        base = R.choice(['', 'http://config.test', 'https://other.test:9443'])
        inputs = {'direct_rulesets': R.sample(ids, R.randint(0, len(ids))), 'rule_update': {'enabled': maybe()}}
        if maybe(0.3):
            inputs['adguard'] = {'enabled': True, 'urls': ['https://example.test/ag1.txt']}
        settings = call(prepare_editor, inputs, cfg, R.choice(['', 'http://config.test']))
        if settings is None:
            continue
        if maybe(0.3):
            settings = native_settings({'oxidns': settings}, '', edit_document) or settings
        document = call(lambda s: native_document(s, validate_arguments=False), settings)
        if document is not None:
            call(prepare_native_rule_updates, document, settings, cfg, base)
        after = copy.deepcopy(cfg)
        after['oxidns'] = copy.deepcopy(settings)
        for item in after['rule_configs']:
            if item['itemType'] == 'ruleset' and maybe(0.5):
                item['url'] = R.choice(['https://example.test/moved.txt', '/api/files/moved.txt', item['url']])
        call(rebase_native_rule_sources, cfg, after)
        changes = {}
        if ids and maybe(0.7):
            changes['direct_rulesets'] = R.sample(ids, R.randint(0, len(ids)))
        if maybe(0.4):
            changes['rule_update.enabled'] = maybe()
        if maybe(0.2):
            changes['adguard.urls'] = R.sample(['https://example.test/ag1.txt', 'https://example.test/ag2.txt'], R.randint(0, 2))
        if changes:
            call(apply_common_settings, settings, changes, cfg, base)


def main():
    device_policy_cases()
    editor_cases()
    pipeline_cases()
    rows_out = [row for row in rec._RECORDS if 'unsupported' not in row]
    seen, unique = set(), []
    for row in rows_out:
        key = json.dumps({k: v for k, v in row.items() if k != 'test'}, sort_keys=True)
        if key not in seen:
            seen.add(key)
            unique.append(row)
    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c26_corpus.jsonl.gz')
    with open(out, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(('\n'.join(json.dumps(row, ensure_ascii=False) for row in unique) + '\n').encode())
    counts = {}
    for row in unique:
        counts[row['fn']] = counts.get(row['fn'], 0) + 1
    errors = sum(1 for row in unique if 'err' in row)
    print(f'{len(unique)} rows ({errors} errors) {counts} -> {out}')


if __name__ == '__main__':
    main()
