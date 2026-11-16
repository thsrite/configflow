"""Random corpus for module C25 (OxiDNS common routing, DNS watchdog), run
with real Python. Synthetic values only (example.test, 192.0.2.0/24,
2001:db8::/32).

    rt --back server/internal/oxidns/testdata/c25_corpus.jsonl.gz -- bash -lc \\
        'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore \\
         server/internal/oxidns/testdata/gen_c25.py'

Routing graphs: a random rule space (single rules of every DNS type and
unconvertible ones, domain / ipcidr / classical rule sets with content or
URL, disabled entries, duplicate ids, Unicode values, policies and proxy
groups for follow routes), a template with random selections, rule actions
(rejections, blackhole, pinned upstreams), custom matches, dynamic domains
and rule updates rendered by prepare_editor, then a mutated native document
(legacy IP groups with and without probe, hand-edited helpers and pinned
chains, renamed / shared / extended providers, reordered or broken
sequence_main, YAML aliases, includes, managed renames with renamed_plugins,
parked and follow state). Recorded through c25_record (same rows as
c25_recorded.jsonl.gz): read_routing, apply_routing (random changes, before
configs, notices), migrate_domain_only_routing, remove_rule,
resource_downloads, owned_chains, refresh_rule_helpers, _prefix_end; plus
DNS watchdog documents, resource_is_read glob patterns, _references tags.
"""
import copy
import gzip
import json
import os
import random
import sys
import tempfile

os.environ.setdefault('DATA_DIR', tempfile.mkdtemp(prefix='c25gen-'))

import yaml  # noqa: E402

import c25_record as rec  # noqa: E402

rec._install()

from backend.common import oxidns_common_routing as rt  # noqa: E402
from backend.common.dns_watchdog import WATCHDOG_RULE, ensure_local_dns_watchdog  # noqa: E402
from backend.common.oxidns_editor import prepare_editor  # noqa: E402
from backend.common.oxidns_native_apply import _rename  # noqa: E402
from backend.converters.oxidns import _rule_path  # noqa: E402

R = random.Random(2525)
SCALE = float(sys.argv[1]) if len(sys.argv) > 1 else 1.0


def n(count):
    return max(1, int(count * SCALE))


def maybe(p=0.5):
    return R.random() < p


def pick(*values):
    return copy.deepcopy(R.choice(values))


VALUES = {'DOMAIN': ('a.example.test', '测试.example.test', 'xn--0zwm56d.example.test', 'A.Example.Test'),
          'DOMAIN-SUFFIX': ('example.test', 'b.example.test', '例子.test'),
          'DOMAIN-KEYWORD': ('ads', '广告', 'k-w'),
          'DOMAIN-REGEX': (r'^ad\d+\.example\.test$', '(', r'\bx'),
          'IP-CIDR': ('192.0.2.0/24', '192.0.2.1/32', 'bad'),
          'IP-CIDR6': ('2001:db8::/32', '2001:db8::1/128'),
          'GEOSITE': ('cn', 'private'),
          'PROCESS-NAME': ('curl',), 'DST-PORT': ('443',)}
CONTENTS = ('IP-CIDR,192.0.2.0/24\nDOMAIN,x.example.test\n', 'DOMAIN-SUFFIX,example.test\n',
            '# only a comment\n', 'IP-CIDR6,2001:db8::/32,no-resolve\n', '', 'DOMAIN,测试.test\nIP-CIDR,192.0.2.9/32\n')
POLICIES = ('DIRECT', 'Proxy', 'REJECT', 'PASS', 'Auto', 'Manual', 'Follow', 'missing', '', None, 5)


def rule_space():
    items = []
    for index in range(R.randint(2, 9)):
        identity = pick('r%d' % index, 'r%d' % index, '规则%d' % index, 'r0' if index else 'r0')
        if maybe(0.6):
            kind = R.choice(list(VALUES))
            item = {'id': identity, 'itemType': 'rule', 'rule_type': kind, 'value': R.choice(VALUES[kind])}
        else:
            behavior = pick('domain', 'domain', 'ipcidr', 'classical', 'classical', None)
            item = {'id': identity, 'itemType': 'ruleset', 'url': pick('https://example.test/%s.txt' % index,
                                                                       '/api/rules/%d' % index, '', 5)}
            if behavior is not None:
                item['behavior'] = behavior
            if behavior == 'classical' and maybe(0.7):
                item['source_type'] = 'content'
                item['content'] = R.choice(CONTENTS)
            if maybe(0.1):
                item['format'] = pick('mrs', 'text', 'yaml')
        if maybe(0.15):
            item['enabled'] = maybe(0.3)
        if maybe(0.1):
            item['library_enabled'] = False
        if maybe(0.85):
            item['policy'] = R.choice(POLICIES)
        if maybe(0.05):
            item['id'] = pick(5, None, '')
        if maybe(0.3):
            item['name'] = pick('名称', 'Rule name', '')
        items.append(item)
    groups = [{'name': 'Proxy', 'id': 'g1', 'type': 'select', 'proxies': ['node-a']},
              {'name': 'Auto', 'id': 'g2', 'type': pick('url-test', 'select'), 'proxies': [pick('DIRECT', 'Proxy', 'REJECT')]},
              {'name': 'Manual', 'id': 'g3', 'type': 'select', 'proxies': [pick('DIRECT', 'Auto', 'Manual', 'PASS')],
               'enabled': pick(True, False, None)},
              {'name': 'Follow', 'id': 'g4', 'type': 'select', 'follow_group': pick('g3', 'g2', 'gx', 'g4')}]
    cfg = {'system_config': {'server_domain': 'http://config.test', 'rule_proxy_token': 'test-only'},
           'rule_configs': items, 'proxy_groups': groups if maybe(0.7) else pick([], None, 'x')}
    if maybe(0.1):
        cfg['rule_configs'] = []
        cfg['rules'] = [{'id': 'legacy', 'rule_type': 'DOMAIN', 'value': 'l.example.test', 'policy': 'DIRECT'}]
    return cfg


def ids(cfg):
    return [item.get('id') for item in cfg.get('rule_configs') or [] if isinstance(item, dict)]


def suffix_ids(cfg, ruleset):
    return [item['id'] for item in cfg.get('rule_configs') or []
            if isinstance(item.get('id'), str) and (item.get('itemType') == 'ruleset') == ruleset]


def action():
    return pick({'action': 'refused'}, {'action': 'nxdomain'}, {'action': 'nodata'}, {'action': 'blackhole'},
                {'action': 'upstream', 'source': pick('local', 'remote', 'fallback'),
                 'upstream': {'addr': pick('udp://192.0.2.53', 'https://192.0.2.1/dns-query', 'tls://dns.example.test',
                                           'udp://[2001:db8::53]:53')}},
                {'action': 'direct'}, {'action': 'proxy'}, {'action': 'bogus'}, {'action': 'refused', 'x': 1},
                {'action': 'upstream', 'source': 'local', 'upstream': {'addr': ' '}}, None, 'refused')


def template(cfg):
    t = {}
    rules, sets = suffix_ids(cfg, False), suffix_ids(cfg, True)
    chosen = set()
    for key, pool in (('direct_rules', rules), ('proxy_rules', rules), ('direct_rulesets', sets), ('proxy_rulesets', sets)):
        values = [value for value in R.sample(pool, R.randint(0, len(pool))) if value not in chosen]
        chosen.update(values)
        t[key] = values
    if maybe(0.5):
        t['rule_actions'] = {identity: action() for identity in R.sample(rules + sets, R.randint(0, min(3, len(rules + sets))))
                             if identity not in chosen}
        t['rule_actions'] = {k: v for k, v in t['rule_actions'].items() if isinstance(v, dict) and v.get('action') in
                             ('refused', 'nxdomain', 'nodata', 'blackhole', 'upstream') and (v['action'] != 'upstream' or v['upstream']['addr'].strip())
                             and (v['action'] == 'upstream' or len(v) == 1)}
    for field in ('cache_enabled', 'prefer_ipv4', 'metrics_enabled', 'aggregate_rules'):
        if maybe(0.4):
            t[field] = maybe()
    if maybe(0.4):
        t['default_forward'] = pick('forward_local', 'forward_remote')
    if maybe(0.3):
        t['custom_matches'] = pick([{'matches': ['qname full:c.example.test'], 'exec': 'reject 3'}],
                                   [{'matches': 'qtype 28', 'exec': 'reject 0'}, {'matches': ['qname $x'], 'exec': 'accept'}])
        t['custom_match_position'] = pick('head', 'tail')
    if maybe(0.3):
        t['dynamic_domain_set'] = {'enabled': True, 'route': pick('direct', 'proxy'), 'learn': maybe()}
    if maybe(0.5):
        t['rule_update'] = {'enabled': maybe(0.8)}
    if maybe(0.3):
        t['remote_ttl'] = pick(0, 1, 300)
    for name in ('ip_selector', 'ros_address_list'):
        if maybe(0.2):
            t[name] = {'enabled': True}
            if name == 'ros_address_list':
                t[name]['args'] = {'address': 'https://192.0.2.1', 'username': 'u', 'password': 'p'}
    if maybe(0.3):
        t['adguard'] = {'enabled': True, 'urls': R.sample(['https://example.test/ad.txt', 'https://example.test/b.txt'], R.randint(0, 2))}
    return t


def plugin(document, tag):
    for item in document.get('plugins', []) if isinstance(document.get('plugins'), list) else []:
        if isinstance(item, dict) and item.get('tag') == tag:
            return item
    return None


def main_args(document):
    item = plugin(document, 'sequence_main')
    return item.get('args') if item and isinstance(item.get('args'), list) else None


def legacy_ip(document, cfg, settings):
    """Old managed IP routing for selected sets/rules."""
    args = main_args(document)
    if args is None or not args:
        return
    entries, providers = [], []
    pool = [item for item in cfg.get('rule_configs') or [] if isinstance(item.get('id'), str) and (
        item.get('behavior') in ('ipcidr', 'classical') or item.get('rule_type') in ('IP-CIDR', 'IP-CIDR6'))]
    for index, item in enumerate(R.sample(pool, R.randint(0, min(3, len(pool))))):
        tag = pick('rules_ip_%d' % (index + 1), 'rules_ip_%d' % (index + 1), 'rules_ip_%d_2' % (index + 1), 'user_ip')
        if item.get('itemType') == 'ruleset':
            args_value = {'files': [_rule_path(item, 'ip')]}
            key = 'direct_rulesets' if maybe() else 'proxy_rulesets'
        else:
            args_value = {'ips': [pick(item.get('value'), '192.0.2.0/24')]}
            key = 'direct_rules' if maybe() else 'proxy_rules'
        if isinstance(settings.get(key), list) and item['id'] not in settings[key] and maybe(0.7):
            settings[key].append(item['id'])
        route = key.split('_')[0]
        providers.append({'tag': tag, 'type': pick('ip_set', 'ip_set', 'domain_set'), 'args': args_value})
        entries.append({'matches': ['resp_ip $' + tag], 'exec': pick('goto direct_ip' if route == 'direct' else 'goto ip_requery_proxy',
                                                                       'reject 3', 'goto direct_ip')})
    if not entries:
        return
    final = args.pop()
    if maybe(0.7):
        args.append({'exec': 'jump local_probe'})
    args.extend(entries)
    if maybe(0.7):
        default = final.get('exec') == 'goto china_dns' if isinstance(final, dict) else False
        args.append({'matches': ['has_resp'], 'exec': 'goto default_direct_ip'} if default or maybe(0.2) else {'exec': 'drop_resp'})
    args.append(final)
    plugins = document['plugins']
    at = next((i for i, p in enumerate(plugins) if isinstance(p, dict) and p.get('tag') == 'sequence_main'), len(plugins))
    plugins[at:at] = providers
    reload = plugin(document, 'rules_reload')
    if reload and isinstance(reload.get('args'), list) and maybe(0.7):
        reload['args'].extend('$' + p['tag'] for p in providers)
    download = plugin(document, 'rules_download')
    if download and isinstance(download.get('args'), dict) and isinstance(download['args'].get('downloads'), list) and maybe(0.6):
        for p in providers:
            for path in p['args'].get('files', []):
                download['args']['downloads'].append({'url': 'http://config.test/x', 'dir': 'rules', 'filename': path.split('/')[-1]})


ENTRIES = ({'exec': 'accept'}, {'matches': ['qname full:x.example.test'], 'exec': 'reject 3'},
           {'matches': 'qtype 65', 'exec': 'reject 0'}, {'matches': [], 'exec': 'accept'}, {'matches': [' '], 'exec': 'accept'},
           {'matches': ['qname $rules_domain_1'], 'exec': 'goto china_dns'}, {'matches': ['qname $dynamic_domains'], 'exec': 'goto proxy_dns_seq'},
           {'exec': 'jump local_probe'}, {'exec': 'jump cf_device_policy'}, copy.deepcopy(WATCHDOG_RULE),
           {'exec': '$custom_hosts'}, {'matches': ['has_resp'], 'exec': 'accept'}, {'exec': 'prefer_ipv4'},
           {'exec': 'metrics_collector m'}, {'matches': ['qtype 1 ٢٨'], 'exec': 'reject 0'},
           {'matches': ['qtype 1　 28'], 'exec': 'black_hole nodata short_circuit=true'},
           {'matches': ['question $adguard_rules'], 'exec': 'goto adguard_reject'}, {'exec': ' '}, {'exec': 5},
           {'matches': ['qname $rules_domain_9'], 'exec': 'REJECT 3'}, {'matches': ['qname $cf_dns_x'], 'exec': '$cf_dns_blackhole'},
           {'matches': ['qname $rules_domain_1'], 'exec': 'goto cf_dns_up_0123456789ab'}, {'matches': ['resp_ip $rules_ip_1'], 'exec': 'goto direct_ip'},
           {'matches': ['qname  $rules_domain_1'], 'exec': 'accept'}, {'matches': ['qname $rules_domain_1', 'qtype 1'], 'exec': 'accept'},
           {'matches': ['qname $rules_domain_1'], 'exec': 'reject nxdomain'}, {'matches': ['qname $rules_domain_1'], 'exec': 'reject 7'},
           'not a dict', {'enabled': False, 'matches': ['x'], 'exec': 'accept'})


def mutate(document, settings, cfg):
    plugins = document.get('plugins')
    if not isinstance(plugins, list):
        return
    for _ in range(R.choice((0, 1, 1, 2, 3, 4))):
        kind = R.randrange(24)
        args = main_args(document)
        if kind == 0:
            legacy_ip(document, cfg, settings)
        elif kind == 1 and args:
            args.insert(R.randint(0, len(args)), pick(*ENTRIES))
        elif kind == 2 and args and len(args) > 1:
            i, j = R.randrange(len(args)), R.randrange(len(args))
            args[i], args[j] = args[j], args[i]
        elif kind == 3:
            chain = next((p for p in plugins if isinstance(p, dict) and str(p.get('tag')).startswith('cf_dns_up_')), None)
            if chain and isinstance(chain.get('args'), list) and chain['args']:
                step = R.randrange(5)
                if step == 0:
                    chain['args'].insert(1, {'exec': pick('$ip_selector', '$ros_route', '$ros_address_list', '$bogus')})
                elif step == 1:
                    chain['args'].insert(-1, {'exec': pick('ttl 60', 'ttl 0', 'ttl 99999999999999999999')})
                elif step == 2:
                    chain['args'].pop(R.randrange(len(chain['args'])))
                elif step == 3:
                    fwd = plugin(document, 'cf_dns_fwd_' + chain['tag'][10:])
                    if fwd and isinstance(fwd.get('args'), dict):
                        fwd['args']['upstreams'] = pick([{'addr': 'udp://192.0.2.99'}], [], [{'addr': ''}], [{'addr': 'udp://192.0.2.1'}, {'addr': 'udp://192.0.2.2'}])
                else:
                    cache = plugin(document, 'cf_dns_cache_' + chain['tag'][10:])
                    if cache and isinstance(cache.get('args'), dict):
                        cache['args'] = pick({'size': 1024, 'short_circuit': True}, {'size': 2048, 'short_circuit': True},
                                             {'size': 1024, 'lazy_cache_ttl': True, 'short_circuit': True}, {'size': 1024})
        elif kind == 4:
            hole = plugin(document, 'cf_dns_blackhole')
            if hole:
                hole['args'] = pick({'mode': None, 'short_circuit': True}, {'mode': 'null', 'short_circuit': 1}, {'mode': 'null'})
            else:
                plugins.insert(0, {'tag': 'cf_dns_blackhole', 'type': 'black_hole', 'args': pick({'mode': 'null', 'short_circuit': True}, {})})
        elif kind == 5:
            provider = next((p for p in plugins if isinstance(p, dict) and str(p.get('tag')).startswith('rules_')), None)
            if provider:
                step = R.randrange(5)
                if step == 0:
                    provider['future'] = 1
                elif step == 1 and isinstance(provider.get('args'), dict):
                    provider['args'][pick('files', 'exps', 'ips', 'other')] = pick(['./rules/unknown.txt'], [], 'x', ['full:z.example.test'], [5])
                elif step == 2:
                    plugins.append({'tag': 'user_seq', 'type': 'sequence', 'args': [{'matches': ['qname $' + provider['tag']], 'exec': 'accept'}]})
                elif step == 3:
                    document.setdefault('x-ref', '$' + provider['tag'])
                else:
                    plugins.append({'tag': provider['tag'] + '_copy', 'type': provider.get('type'), 'args': provider.get('args')})
        elif kind == 6 and args:
            args[-1] = pick({'exec': 'goto china_dns'}, {'exec': 'goto proxy_dns_seq'}, {'exec': 'accept'},
                            {'matches': ['qtype 1'], 'exec': 'goto china_dns'})
        elif kind == 7:
            document['include'] = pick(['extra.yaml'], [], None)
            if document.get('include'):
                settings['additional_files'] = [{'path': 'extra.yaml', 'content': 'plugins:\n- tag: extra_seq\n  type: sequence\n  args:\n  - exec: accept\n'}]
        elif kind == 8:
            tag = pick('sequence_main', 'china_dns', 'proxy_dns_seq', 'rules_download', 'rules_reload', 'local_probe')
            item = plugin(document, tag)
            if item:
                new = tag + pick('_2', '_x', '.renamed')
                item['tag'] = new
                _rename(document, tag, new)
                if maybe(0.8):
                    settings.setdefault('_common_disabled', {}).setdefault('renamed_plugins', {})[tag] = pick(new, new, new, 'qs.exec.x', 5, '')
        elif kind == 9:
            item = plugin(document, pick('rules_download', 'rules_reload'))
            if item:
                if maybe():
                    item['args'] = pick('x', {}, [], {'downloads': 'x'}, None)
                else:
                    plugins.remove(item)
                    if item['tag'] == 'rules_reload' and maybe():
                        settings.setdefault('_common_disabled', {})['rules_reload'] = item
        elif kind == 10:
            for key in ('rule_dns_parked', 'rule_dns_follow'):
                if maybe():
                    known = ids(cfg)
                    if key == 'rule_dns_parked':
                        settings[key] = pick({}, {R.choice(known or ['x']): action()}, 'x', {'missing': {'action': 'proxy'}})
                    else:
                        settings[key] = pick([], R.sample([i for i in known if isinstance(i, str)] or ['x'], 1), 'x', [5], ['dup', 'dup'])
        elif kind == 11:
            # YAML aliases: share providers' args / entries.
            providers = [p for p in plugins if isinstance(p, dict) and str(p.get('tag')).startswith('rules_')]
            if len(providers) > 1:
                providers[1]['args'] = providers[0]['args']
            elif args and len(args) > 2:
                args.insert(0, args[1])
        elif kind == 12 and args:
            dyn = {'matches': ['qname $dynamic_domains'], 'exec': pick('goto china_dns', 'goto proxy_dns_seq')}
            args.insert(R.randint(0, len(args) - 1), dyn)
        elif kind == 13:
            for name in ('local_probe', 'default_direct_ip', 'direct_ip', 'china_dns', 'proxy_dns_seq'):
                item = plugin(document, name)
                if item and maybe(0.3):
                    if maybe():
                        plugins.remove(item)
                    else:
                        item['type'] = 'forward'
        elif kind == 14:
            china = plugin(document, 'china_dns')
            if china and isinstance(china.get('args'), list):
                china['args'].insert(0, {'exec': pick('$ip_selector', '$ros_address_list', '$ros_route', '$lazy_cache')})
        elif kind == 15:
            remote = plugin(document, 'remote_dns')
            if remote and isinstance(remote.get('args'), list):
                remote['args'].append({'exec': pick('ttl 60', 'ttl 99999999999999999999', 'ttl 01')})
        elif kind == 16:
            lazy = plugin(document, 'lazy_cache')
            if lazy and isinstance(lazy.get('args'), dict):
                lazy['args']['lazy_cache_ttl'] = pick(0, -1, 10 ** 20, True, 1.5, '60')
        elif kind == 17 and args:
            for entry in args:
                if isinstance(entry, dict) and isinstance(entry.get('matches'), list) and entry['matches'] and \
                        str(entry['matches'][0]).startswith('qname $rules_') and maybe(0.5):
                    entry['exec'] = pick('reject', 'reject 0', 'reject 3', 'REJECT NXDOMAIN', '$cf_dns_blackhole',
                                         'goto cf_dns_up_0123456789ab', 'goto proxy_dns_seq', 'jump china_dns')
        elif kind == 18:
            download = plugin(document, 'rules_download')
            if download and isinstance(download.get('args'), dict) and isinstance(download['args'].get('downloads'), list):
                download['args']['downloads'].append(pick(
                    {'url': 'http://config.test/y', 'dir': './rules/', 'filename': 'rule_0000000000000000_domain.txt'},
                    {'url': 'http://config.test/y', 'dir': 'other', 'filename': 'f.txt'}, 'x', {'dir': 5},
                    {'url': 'http://config.test/z', 'dir': 'rules', 'filename': 'user.txt', 'extra': True}))
                if maybe(0.4):
                    plugins.append({'tag': 'reader', 'type': 'domain_set', 'args': {'files': [pick(
                        './rules/*.txt', 'rules/rule_?*', '/opt/oxidns/rules/[a-z]*', '${RULES}/x', './rules/[!x]*.txt', 'rules/[')]}})
        elif kind == 19:
            plugins.append(pick({'tag': 'cf_dns_fwd_0123456789ab', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://192.0.2.5'}]}},
                                {'tag': 'cf_dns_up_0123456789ab', 'type': 'sequence', 'args': [{'exec': 'drop_resp'}, {'exec': '$cf_dns_fwd_0123456789ab'},
                                                                                               {'matches': ['!has_resp'], 'exec': 'reject 3'}, {'exec': 'accept'}]},
                                {'tag': 'cf_dns_cache_0123456789ab', 'type': 'cache', 'args': {'size': 1024, 'short_circuit': True}}))
        elif kind == 20 and args:
            args.insert(0, pick({'exec': 'jump cf_dns_x'}, {'exec': '$cf_dns_blackhole'}, {'exec': 'goto cf_dns_up_0123456789ab'}))
        elif kind == 21:
            plugins.append(pick('x', 5, {'tag': 5}, {'tag': 'sequence_main', 'type': 'sequence', 'args': []}))
        elif kind == 22:
            document['plugins'] = pick('x', {}, None)
            return
        elif kind == 23 and args:
            for entry in args:
                if isinstance(entry, dict) and maybe(0.2):
                    entry['extra'] = pick(1, '测试')


def base_case():
    cfg = rule_space()
    t = template(cfg)
    try:
        settings = prepare_editor(t, cfg)
    except Exception:
        try:
            settings = prepare_editor({}, cfg)
        except Exception:
            return None
    document = yaml.safe_load(settings['native_config'])
    if maybe(0.85):
        mutate(document, settings, cfg)
    try:
        text = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    except Exception:
        return None
    settings['native_config'] = text
    document = yaml.safe_load(text)  # aliases become shared objects again
    return document, settings, cfg


def before_config(cfg):
    if maybe(0.5):
        return cfg
    before = copy.deepcopy(cfg)
    items = before.get('rule_configs') or []
    if items:
        step = R.randrange(5)
        item = R.choice(items)
        if step == 0:
            items.remove(item)
        elif step == 1:
            item['enabled'] = not item.get('enabled', True)
        elif step == 2:
            R.shuffle(items)
        elif step == 3:
            item['url'] = 'https://example.test/changed.txt'
        else:
            item['policy'] = pick('DIRECT', 'Proxy', 'Manual')
    return before


def changes_for(read, cfg):
    caps = read.get('capabilities', {}) if isinstance(read, dict) else {}
    values = read.get('values', {}) if isinstance(read, dict) else {}
    known = [i for i in ids(cfg) if isinstance(i, str)] + ['missing']
    changes = {}
    fields = [f for f, c in caps.items() if c.get('editable')] if maybe(0.85) else list(rt.ROUTING_FIELDS)
    for _ in range(R.choice((1, 1, 2, 3))):
        if not fields:
            break
        field = R.choice(sorted(fields))
        current = copy.deepcopy(values.get(field))
        if field in rt.SELECTORS:
            pool = R.sample(known, R.randint(0, min(4, len(known))))
            value = pick(pool, (current or []) + pool[:1], [x for x in (current or []) if maybe()], 'x', [5])
        elif field == 'rule_actions':
            value = {identity: action() for identity in R.sample(known, R.randint(0, min(3, len(known))))}
            if isinstance(current, dict) and maybe():
                value = {**current, **value}
            value = pick(value, value, value, [], 'x')
        elif field == 'rule_dns_follow':
            value = pick(R.sample(known, R.randint(0, min(2, len(known)))), [], 'x', [5])
        elif field == 'aggregate_rules':
            value = pick(True, False, None, 1)
        elif field == 'default_forward':
            value = pick('forward_local', 'forward_remote', 'x', None)
        elif field == 'custom_matches':
            value = pick([], [{'matches': ['qname full:n.example.test'], 'exec': 'accept'}], [{'matches': 'qtype 1', 'exec': 'reject 0', 'enabled': True}],
                         [{'enabled': False, 'matches': ['x'], 'exec': 'accept'}], [{'matches': [], 'exec': 'accept'}], 'x', [5],
                         (current or []) + [{'matches': ['qname $rules_domain_1'], 'exec': 'reject 3'}])
        else:
            value = pick('head', 'tail', 'middle', None)
        changes[field] = value
    if maybe(0.05):
        changes['unknown'] = 1
    return changes


def routing_cases():
    for _ in range(n(1500)):
        case = base_case()
        if case is None:
            continue
        document, settings, cfg = case
        base_url = pick('', 'http://192.0.2.10:5000')

        def call(fn, *args, **kwargs):
            try:
                return fn(*args, **kwargs)
            except Exception:
                return None
        read = call(rt.read_routing, document, settings, cfg) or {}
        for _ in range(R.choice((1, 1, 2, 3))):
            changes = changes_for(read, cfg)
            kwargs = {}
            if maybe(0.25):
                kwargs['before_config'] = before_config(cfg)
                if maybe(0.3):
                    changes = {}
            if maybe(0.5):
                kwargs['notices'] = pick([], ['earlier'])
            call(rt.apply_routing, document, settings, cfg, changes, base_url, **kwargs)
        if maybe(0.5):
            call(rt.migrate_domain_only_routing, document, settings, cfg, base_url)
        if maybe(0.4):
            call(rt.resource_downloads, document, settings, cfg, base_url)
        if maybe(0.4):
            call(rt.owned_chains, document)
        if maybe(0.3):
            call(rt.refresh_rule_helpers, copy.deepcopy(document))
        if maybe(0.3):
            known = ids(cfg)
            if known and isinstance(R.choice(known), str):
                call(rt.remove_rule, settings, cfg, pick(*[i for i in known if isinstance(i, str)] or ['x'], 'missing'), base_url)
        if maybe(0.3):
            args = main_args(document)
            if args is not None:
                call(rt._prefix_end, args)
        if maybe(0.2):
            call(rt.resource_readers, document)


def default_forward_cases():
    """IP routing that the editor cannot rebuild: only the fallback and the
    IP block are rewritten for a default_forward change."""
    for _ in range(n(600)):
        cfg = rule_space()
        try:
            settings = prepare_editor(template(cfg), cfg)
        except Exception:
            continue
        document = yaml.safe_load(settings['native_config'])
        legacy_ip(document, cfg, settings)
        for item in document.get('plugins', []):
            if isinstance(item, dict) and str(item.get('tag')).startswith(('rules_ip', 'user_ip')) and maybe(0.6):
                step = R.randrange(3)
                if step == 0:
                    item['future'] = True
                elif step == 1 and isinstance(item.get('args'), dict):
                    item['args'] = {'ips': ['198.51.100.0/24']}
                else:
                    document['x-ref'] = '$' + item['tag']
        for name in ('local_probe', 'default_direct_ip', 'china_dns', 'proxy_dns_seq'):
            if maybe(0.1):
                item = plugin(document, name)
                if item:
                    document['plugins'].remove(item)
        if maybe(0.3):
            mutate(document, settings, cfg)
        try:
            text = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
        except Exception:
            continue
        settings['native_config'] = text
        document = yaml.safe_load(text)
        for _ in range(R.choice((1, 2))):
            try:
                rt.apply_routing(document, settings, cfg, {'default_forward': pick('forward_local', 'forward_remote')})
            except Exception:
                pass


def watchdog_cases():
    servers = ('udp_server', 'tcp_server', 'quic_server', 'http_server')
    for _ in range(n(2000)):
        plugins = []
        tags = ['main', 'seq_b', 'fwd', '测试', 'cf_watchdog_x', '${ENV}']
        for _ in range(R.randint(0, 5)):
            tag = pick(*tags, None, '', 5)
            kind = pick('sequence', 'sequence', 'forward', 'cache')
            args = pick([{'exec': 'accept'}], [copy.deepcopy(WATCHDOG_RULE), {'exec': 'accept'}],
                        [{'exec': 'accept'}, copy.deepcopy(WATCHDOG_RULE)], {}, 'x', [])
            item = {'type': kind, 'args': args}
            if tag is not None or maybe():
                item['tag'] = tag
            plugins.append(item)
        for _ in range(R.randint(0, 4)):
            kind = R.choice(servers)
            if kind == 'http_server':
                entries = [{'path': '/dns-query', 'exec': pick(*tags, 'missing', 5, None)} for _ in range(R.randint(0, 3))]
                args = pick({'entries': entries}, {'entries': entries}, {}, {'entries': 'x'}, {'entries': [5]}, 'x')
            else:
                args = pick({'listen': ':53', 'entry': pick(*tags, 'missing', '${OTHER}', 5)}, 'x', {})
            item = {'tag': pick('srv', 'srv2', None, ''), 'type': kind, 'args': args}
            plugins.insert(R.randint(0, len(plugins)), item)
        if maybe(0.1):
            # Wrapper name collisions.
            import hashlib
            for tag in ('fwd', '测试'):
                plugins.append({'tag': 'cf_watchdog_' + hashlib.sha256(tag.encode()).hexdigest()[:16], 'type': 'sequence', 'args': []})
        document = {'plugins': plugins}
        if maybe(0.1):
            document = pick({'include': ['x.yaml'], 'plugins': 5}, {'plugins': 'x'}, {}, {'plugins': [5]}, {'include': [], 'plugins': []})
        if maybe(0.2) and isinstance(document.get('plugins'), list) and document['plugins']:
            document['plugins'].append(document['plugins'][0])  # alias
        try:
            ensure_local_dns_watchdog(document)
        except Exception:
            pass


GLOB = 'ab/*?[]!-^\\.&~|cz0' + 'é测'


def glob_cases():
    for _ in range(n(4000)):
        pattern = ''.join(R.choice(GLOB) for _ in range(R.randint(0, 10)))
        if maybe(0.3):
            pattern = pick('./', '/', '/opt/x/', '', '${A}') + pattern
        path = ''.join(R.choice('ab/c.z-é测!^[]') for _ in range(R.randint(0, 8)))
        readers = [pattern] if maybe(0.8) else [pattern, ''.join(R.choice(GLOB) for _ in range(R.randint(0, 6)))]
        try:
            rt.resource_is_read(path, readers)
        except Exception:
            pass


TAG_CHARS = 'ab.-_$1٣测é́ \t/x' + '²Ⅰ'


def reference_cases():
    for _ in range(n(3000)):
        tag = ''.join(R.choice(TAG_CHARS) for _ in range(R.randint(0, 5)))
        def text():
            parts = [''.join(R.choice(TAG_CHARS) for _ in range(R.randint(0, 4))) for _ in range(3)]
            return parts[0] + (pick('$', '', '$$') + tag if maybe(0.7) else '') + parts[1]
        value = pick(text(), [text(), 5, None], {'k': text(), 'n': {'m': [text()]}}, 5, None)
        try:
            rt._references(value, tag)
        except Exception:
            pass


def prefix_cases():
    for _ in range(n(1500)):
        args = [pick(*ENTRIES) for _ in range(R.randint(0, 6))]
        args = [a for a in args if isinstance(a, dict)]
        try:
            rt._prefix_end(args)
        except Exception:
            pass


def main():
    routing_cases()
    default_forward_cases()
    watchdog_cases()
    glob_cases()
    reference_cases()
    prefix_cases()
    rows = rec._RECORDS
    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c25_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(row, ensure_ascii=False) for row in rows) + '\n'
    with open(out, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts, errors = {}, {}
    for row in rows:
        counts[row['fn']] = counts.get(row['fn'], 0) + 1
        if 'err' in row:
            errors[row['err'][0]] = errors.get(row['err'][0], 0) + 1
    unsupported = sum(1 for row in rows if 'unsupported' in row)
    collab = sum(len(row.get('collab', ())) for row in rows)
    ipl = sum(len(row.get('ipl', ())) for row in rows)
    print(f'{len(rows)} rows {counts}; errors {errors}; {unsupported} unsupported; collab {collab}; ipl {ipl} -> {out}')


if __name__ == '__main__':
    main()
