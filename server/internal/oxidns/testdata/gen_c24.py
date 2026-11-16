"""Random corpus for module C24 (OxiDNS common settings), run with real
Python. Synthetic values only (example.test, 192.0.2.0/24, 2001:db8::/32).

    rt --back server/internal/oxidns/testdata/c24_corpus.jsonl.gz -- bash -lc \\
        'PYTHONPATH=.:server/internal/oxidns/testdata ~/cf-test/.venv/bin/python -W ignore \\
         server/internal/oxidns/testdata/gen_c24.py'

Each case builds a template draft with random common inputs, then mutates the
native document (renamed/retyped/duplicated plugins, custom or partial feature
calls, non-dict args, include, environment references, API shapes, TTL /
qtype / prefer_ipv4 / metrics variants, fallback graphs, dynamic domains,
AdGuard and Hosts files, cron jobs, YAML aliases, Unicode, huge integers) and
the parked `_common_disabled` state, and records read_common_settings plus
apply_common_settings with editable fields (valid typed values) and random
fields (malformed values) through c24_record (rows with collaborator answers,
same format as c24_recorded.jsonl.gz).
"""
import copy
import gzip
import json
import os
import random
import sys

import yaml

import c24_record as rec

rec._install()

from backend.common.oxidns_common import COMMON_FIELDS, apply_common_settings, read_common_settings  # noqa: E402
from backend.common.oxidns_common_routing import ROUTING_FIELDS  # noqa: E402
from backend.common.oxidns_editor import prepare_editor  # noqa: E402
from backend.common.oxidns_settings import get_default_oxidns_settings  # noqa: E402

R = random.Random(2424)
SCALE = float(sys.argv[1]) if len(sys.argv) > 1 else 1.0
DEFAULTS = get_default_oxidns_settings()
FIELDS = sorted(COMMON_FIELDS)
ROUTING = sorted(ROUTING_FIELDS)


def n(count):
    return max(1, int(count * SCALE))


def maybe(p=0.5):
    return R.random() < p


def pick(*values):
    return copy.deepcopy(R.choice(values))


def config():
    if maybe(0.6):
        return {}
    return {'system_config': {'server_domain': 'http://config.test', 'rule_proxy_token': 'test-only'},
            'rule_configs': [
                {'id': 'a', 'itemType': 'rule', 'rule_type': 'DOMAIN', 'value': 'a.example.test'},
                {'id': 'b', 'itemType': 'rule', 'rule_type': 'DOMAIN-SUFFIX', 'value': 'b.example.test'},
                {'id': 'ip', 'itemType': 'rule', 'rule_type': 'IP-CIDR', 'value': '192.0.2.0/24'},
                {'id': 'rs', 'itemType': 'ruleset', 'behavior': 'domain', 'url': 'https://example.test/list.txt'},
                {'id': 'rs2', 'itemType': 'ruleset', 'behavior': 'domain', 'url': 'https://example.test/other.txt'},
            ]}


TEXT = ('', 'x', '测试', 'emoji 😀', ' ', '\t', 'a' * 70, '${OXIDNS_ENV}', '0.0.0.0:5353', '[::]:53')
UPSTREAMS = ('', '- addr: udp://192.0.2.53\n', '- addr: https://192.0.2.1/dns-query\n  bootstrap: 192.0.2.2\n',
             'udp://192.0.2.53', '[', ': :', 'null', '~', 'addr: udp://192.0.2.9', '- 1\n', '- {}\n',
             '- addr: ""\n', '- addr: tls://dns.example.test\n  future: [false, 0, null]\n', '[]', '- {1: 2}\n',
             '- addr: udp://192.0.2.7\n- addr: 测试\n')
HOSTS = ('', '192.0.2.1 a.example.test\n', '2001:db8::1 b.example.test c.example.test\n', 'not-an-ip x\n',
         '# comment only\n', '192.0.2.300 bad\n', '  \n', '192.0.2.1\n')
URLS = ('https://example.test/adguard.txt', 'http://example.test/a.txt', 'ftp://example.test/x', 'example.test',
        'https://example.test/测试.txt', 5)


def template():
    t = {}
    for field in ('cache_enabled', 'cache_dump_enabled', 'probe_cache_enabled', 'prefer_ipv4', 'metrics_enabled', 'tcp_enabled'):
        if maybe(0.4):
            t[field] = maybe()
    if maybe(0.3):
        t['block_qtypes'] = R.sample([1, 28, 65, 255, 64], R.randint(0, 3))
    if maybe(0.3):
        t['remote_ttl'] = pick(0, 1, 300)
    if maybe(0.3):
        t['api'] = {'enabled': maybe(0.7), 'listen': pick('127.0.0.1:9199', '0.0.0.0:9000'),
                    'username': pick('', 'admin'), 'password': pick('', 'secret'), 'webui_root': pick('', './webui')}
    if maybe(0.3):
        t['log'] = {'level': pick('info', 'debug'), 'file': pick('', './state/oxidns.log'),
                    'rotation': pick('daily', 'never'), 'max_files': pick(3, 7)}
    for name in ('query_recorder', 'ip_selector'):
        if maybe(0.35):
            t[name] = {'enabled': maybe(0.7)}
    for name in ('ros_address_list', 'ros_route'):
        if maybe(0.3):
            t[name] = {'enabled': maybe(0.7), 'args': {'address': 'https://192.0.2.1', 'username': 'u', 'password': 'p'}}
            if name == 'ros_route':
                t[name]['args'].update(routing_table='main', gateway4='192.0.2.254')
    if maybe(0.35):
        t['dynamic_domain_set'] = {'enabled': maybe(0.7), 'route': pick('direct', 'proxy'), 'learn': maybe()}
    if maybe(0.35):
        t['adguard'] = {'enabled': maybe(0.7), 'content': pick('', '||ads.example.test^\n'),
                        'urls': R.sample(['https://example.test/adguard.txt', 'https://example.test/b.txt'], R.randint(0, 2))}
    if maybe(0.3):
        t['custom_hosts'] = pick(*HOSTS[:3])
    if maybe(0.3):
        t['fallback_dns'] = pick('', 'udp://192.0.2.53', 'https://192.0.2.1/dns-query')
    if maybe(0.2):
        t['local_dns'] = pick('udp://192.0.2.10', '- addr: udp://192.0.2.11\n')
    if maybe(0.2):
        t['rule_update'] = {'enabled': maybe(0.7)}
    return t


def plugin(document, tag):
    for item in document.get('plugins', []) if isinstance(document.get('plugins'), list) else []:
        if isinstance(item, dict) and item.get('tag') == tag:
            return item
    return None


def chain(document, tag):
    item = plugin(document, tag)
    return item.get('args') if item and isinstance(item.get('args'), list) else None


MANAGED = ('lazy_cache', 'local_probe_cache', 'forward_local', 'forward_remote', 'forward_fallback', 'fallback',
           'dynamic_domains', 'learn_classified_domain', 'adguard_rules', 'adguard_block', 'adguard_reject',
           'custom_hosts', 'server_udp', 'server_tcp', 'rules_download', 'rules_reload', 'rules_cron', 'rules_refresh',
           'query_recorder', 'ip_selector', 'ros_address_list', 'ros_route', 'sequence_main', 'remote_dns',
           'proxy_dns_seq', 'china_dns', 'direct_ip', 'local_probe', 'fallback_dns')


def odd_value():
    return pick(None, 5, 'text', [], [1, 2], {}, True, 10 ** 30, 1.5, '测试')


def mutate(document, settings):
    plugins = document.get('plugins')
    if not isinstance(plugins, list):
        return
    parked = settings.setdefault('_common_disabled', {}) if maybe(0.3) else None
    for _ in range(R.choice((0, 1, 1, 2, 3))):
        kind = R.randrange(26)
        tag = R.choice(MANAGED)
        item = plugin(document, tag)
        if kind == 0 and item:
            new = tag + pick('_2', '_custom')
            item['tag'] = new
            if maybe(0.6):
                settings.setdefault('_common_disabled', {}).setdefault('renamed_plugins', {})[tag] = new
        elif kind == 1 and item:
            item['type'] = pick('sequence', 'forward', 'cache', 'future', 5)
        elif kind == 2 and item:
            item['args'] = odd_value()
        elif kind == 3 and item:
            plugins.append(copy.deepcopy(item))
        elif kind == 4:
            target = chain(document, pick('sequence_main', 'china_dns', 'direct_ip', 'default_direct_ip', 'remote_dns', 'local_probe'))
            if target is not None:
                target.insert(R.randint(0, len(target)), pick(
                    {'exec': '$' + tag}, {'exec': 'jump ' + tag}, {'exec': 'goto ' + tag}, {'exec': '$lazy_cache'},
                    {'matches': ['qtype 65'], 'exec': 'prefer_ipv4'}, {'exec': 'metrics_collector metrics'},
                    {'exec': 'metrics_collector other'}, {'exec': 'prefer_ipv4'}, {'exec': '$query_recorder'},
                    {'matches': 'qtype 1 28', 'exec': 'black_hole nodata short_circuit=true'},
                    {'matches': ['qtype 99999999999999999999'], 'exec': 'reject 0'},
                    {'matches': ['qtype 65 x'], 'exec': 'black_hole nodata short_circuit=true'},
                    {'matches': ['qname $dynamic_domains'], 'exec': 'goto china_dns'},
                    {'matches': ['qname $dynamic_domains', 'qtype 1'], 'exec': 'goto china_dns'},
                    {'matches': ['question $adguard_rules'], 'exec': 'goto adguard_reject'},
                    {'exec': '$custom_hosts'}, {'matches': ['has_resp'], 'exec': 'accept'},
                    {'exec': '$learn_classified_domain'}, {'exec': 'ttl 300'}, {'exec': 'ttl x'},
                    {'exec': 'ttl 30', 'extra': 1}, {'exec': 5}, {'exec': True}, 'not a dict', 7,
                    {'exec': '${OXIDNS_EXEC}'}, {'exec': 'goto 测试'}, {'matches': {'k': 1}, 'exec': 'accept'},
                    {'matches': 5, 'exec': 'accept'}, {'matches': [5], 'exec': 'accept'}))
        elif kind == 5:
            document['include'] = pick(['extra.yaml'], [], 'x.yaml', None)
            if document.get('include') and maybe(0.7):
                settings['additional_files'] = [{'path': 'extra.yaml', 'content': yaml.safe_dump(
                    {'plugins': [{'tag': pick('extra_seq', tag), 'type': 'sequence', 'args': [{'exec': 'accept'}]}]})}]
        elif kind == 6:
            document['api'] = pick('127.0.0.1:9000', {'http': None}, {'http': 'x'}, {},
                                   {'http': {'listen': '127.0.0.1:9199', 'auth': {'type': 'bearer', 'token': 't'}}},
                                   {'http': {'listen': '127.0.0.1:9199', 'auth': {'username': 'a', 'password': 'b'}}},
                                   {'http': {'listen': '127.0.0.1:9199', 'webui': {'root': './w', 'index': 'i.html'}}},
                                   {'http': {'listen': '127.0.0.1:9199', 'ssl': 'x'}},
                                   {'http': {'listen': {'nested': 1}}}, {'http': {'listen': '127.0.0.1:9199'}, 'future': 1})
        elif kind == 7:
            remote = chain(document, 'remote_dns')
            if remote is not None:
                remote[:] = [item for item in remote if not str(item.get('exec', '') if isinstance(item, dict) else '').startswith('ttl ')]
                for _ in range(R.choice((0, 1, 1, 2))):
                    remote.append(pick({'exec': 'ttl 60'}, {'exec': 'ttl 99999999999999999999'}, {'exec': 'ttl 0'},
                                       {'exec': 'ttl'}, {'exec': 'ttl 5', 'matches': ['qtype 1']}))
        elif kind == 8:
            seq = plugin(document, 'proxy_dns_seq')
            if seq:
                seq['args'] = pick([{'exec': '$fallback'}, {'exec': 'accept'}], [{'exec': 'goto remote_dns'}],
                                   [{'exec': '$remote_dns'}, {'exec': 'accept'}], [{'exec': 'goto remote_dns'}, {'exec': 'accept'}],
                                   'x')
        elif kind == 9 and parked is not None:
            parked[pick('feature_calls', 'api_http', 'server_tcp', 'cache_dump_file', 'cache_dump_interval',
                        'rules_cron_job', 'dynamic_domain_set_state', 'api_auth', 'api_webui', 'rules_cron',
                        'forward_fallback', 'fallback_dns', 'fallback', 'lazy_cache', 'adguard_rules',
                        'ros_address_list.args.tls', 'renamed_plugins')] = pick(
                {}, {'listen': '127.0.0.1:9300'}, {'tag': 'server_tcp', 'type': 'tcp_server', 'args': {'listen': ':53', 'entry': 'sequence_main'}},
                {'tag': 'rules_cron', 'type': 'cron', 'args': {'timezone': 'UTC', 'jobs': []}},
                {'name': 'refresh_rules', 'schedule': '0 4 * * *', 'executors': ['$rules_refresh']},
                {'route': 'proxy', 'learn': True}, {'sequence_main': {'items': [{'exec': '$query_recorder'}], 'disabled': ['query_recorder']}},
                {'china_dns': {'items': [{'exec': '$ip_selector'}, {'exec': '$forward_local'}], 'disabled': ['ip_selector']}},
                {'tag': 'forward_fallback', 'type': 'forward', 'args': {'upstreams': [{'addr': 'udp://192.0.2.53'}], 'concurrent': 1}},
                {'tag': 'fallback_dns', 'type': 'sequence', 'args': [{'exec': '$forward_fallback'}, {'matches': ['!has_resp'], 'exec': 'reject 3'}]},
                {'tag': 'fallback', 'type': 'fallback', 'args': {'primary': 'remote_dns', 'secondary': 'fallback_dns', 'threshold': 500}},
                {'tag': 'lazy_cache', 'type': 'cache', 'args': {'size': 77}}, {'server_name': 'ros.example.test'},
                './state/other.dump', 60, None, 'x', [], {'lazy_cache': 'lazy_cache_2'})
        elif kind == 10 and item and isinstance(item.get('args'), dict):
            item['args'][pick('size', 'concurrent', 'upstreams', 'listen', 'entry', 'files', 'jobs', 'mode', 'tls', 'future', 'provider')] = odd_value()
        elif kind == 11:
            learn = {'tag': pick('learn_other', 'learn_classified_domain'), 'type': 'learn_domain',
                     'args': {'provider': pick('dynamic_domains', 'other')}}
            plugins.append(learn)
        elif kind == 12:
            hosts = plugin(document, 'custom_hosts')
            if hosts and isinstance(hosts.get('args'), dict):
                hosts['args']['files'] = pick([], ['./rules/custom_hosts.txt'], ['rules/custom_hosts.txt'], ['./rules/other.txt'], 'x')
                if maybe(0.3):
                    hosts['args']['entries'] = pick([], ['192.0.2.1 a.example.test'], None)
        elif kind == 13:
            rules = plugin(document, 'adguard_rules')
            if rules and isinstance(rules.get('args'), dict):
                rules['args']['files'] = pick([], ['./rules/adguard-custom.txt'], ['./rules/unknown.txt'], [5], 'x')
        elif kind == 14:
            cron = plugin(document, 'rules_cron')
            if cron and isinstance(cron.get('args'), dict):
                cron['args']['jobs'] = pick([], 'x', [{'name': 'refresh_rules', 'schedule': '0 1 * * *', 'executors': ['$rules_refresh']}],
                                            [{'name': 'other', 'schedule': '0 1 * * *', 'executors': ['$rules_refresh']}],
                                            [{'name': 'refresh_rules', 'executors': ['$rules_refresh', '$x']}],
                                            [{'name': 'user', 'schedule': '*/5 * * * *', 'executors': ['$user']}])
        elif kind == 15:
            for name in ('server_udp', 'server_tcp'):
                item = plugin(document, name)
                if item and isinstance(item.get('args'), dict) and maybe():
                    item['args'][pick('listen', 'entry')] = pick('0.0.0.0:5353', 'other_seq', None, 5)
        elif kind == 16 and item:
            plugins.remove(item)
        elif kind == 17:
            # Shared args (YAML alias) between a managed plugin and a copy.
            if item and isinstance(item.get('args'), (dict, list)):
                plugins.append({'tag': 'alias_of_' + tag, 'type': item.get('type'), 'args': item['args']})
        elif kind == 18:
            document['plugins'] = [p for p in plugins if not (isinstance(p, dict) and p.get('tag') == 'sequence_main')]
            return
        elif kind == 19:
            document.setdefault('log', {})['rotation'] = pick({'type': 'daily', 'max_files': 3}, {}, 'x', {'max_files': 2})
        elif kind == 20:
            document['runtime'] = pick({'worker_threads': 10 ** 25}, 'x', {'worker_threads': '测试'})
        elif kind == 21:
            cache = plugin(document, 'lazy_cache')
            if cache and isinstance(cache.get('args'), dict):
                cache['args'].update(pick({'dump_file': './state/c.dump', 'dump_interval': 60}, {'dump_interval': None},
                                          {'dump_file': ''}, {'dump_file': '  '}))
        elif kind == 22:
            ros = plugin(document, pick('ros_address_list', 'ros_route'))
            if ros and isinstance(ros.get('args'), dict):
                ros['args']['tls'] = pick({'server_name': 'ros.example.test', 'insecure': True}, None, 'x', {})
        elif kind == 23:
            plugins.append({'tag': pick('rules_domain_1', 'rules_ip_1'), 'type': pick('domain_set', 'ip_set'),
                            'args': {'files': ['./rules/rule_' + pick('a', 'b') + '.txt']}})
        elif kind == 24:
            plugins.append({'tag': 'user_seq', 'type': 'sequence',
                            'args': [{'exec': pick('$forward_fallback', 'goto fallback', '$adguard_block', '$dynamic_domains',
                                                   'jump rules_reload', '$custom_hosts', '$lazy_cache', '$' + tag)}]})
        elif kind == 25 and item:
            item['args'] = {'nested': {'value': pick('测试', 10 ** 40, -1, 1.25, False)}} if maybe() else item.get('args')


def base_settings():
    cfg = config()
    t = template()
    if cfg and maybe(0.6):
        t.update(direct_rulesets=pick(['rs'], ['rs', 'rs2'], []), direct_rules=pick(['a'], [], ['a', 'ip']), proxy_rules=pick([], ['b']))
    try:
        settings = prepare_editor(t, cfg)
    except Exception:
        settings = prepare_editor({}, cfg)
    if maybe(0.8):
        document = yaml.safe_load(settings['native_config'])
        mutate(document, settings)
        settings['native_config'] = yaml.safe_dump(document, allow_unicode=True, sort_keys=False)
    if maybe(0.05):
        settings['native_config'] = pick('', 'plugins: x\n', 'plugins:\n- 5\n', '- a\n', 'plugins:\n- tag: 5\n')
    return settings, cfg


def valid_value(field, current):
    example = DEFAULTS
    for key in field.split('.'):
        example = example.get(key) if isinstance(example, dict) else None
    if field in ('local_dns', 'remote_dns', 'fallback_dns'):
        return pick(*UPSTREAMS[:3], '', '- addr: udp://192.0.2.99\n')
    if field == 'custom_hosts':
        return pick(*HOSTS[:5])
    if field == 'adguard.urls':
        return R.sample(['https://example.test/adguard.txt', 'https://example.test/b.txt', 'http://example.test/c.txt'], R.randint(0, 2))
    if field == 'adguard.mode':
        return pick('nxdomain', 'nodata', 'null', 'refused')
    if field == 'adguard.content':
        return pick('', '||ads.example.test^\n', '测试\n')
    if field == 'dynamic_domain_set.route':
        return pick('direct', 'proxy')
    if field == 'rule_update.args.schedule':
        return pick('0 4 * * *', '*/30 * * * *')
    if field == 'rule_update.args.timezone':
        return pick('UTC', 'Asia/Shanghai')
    if field == 'block_qtypes':
        return R.sample([1, 28, 65, 255, 64], R.randint(0, 3))
    if field.endswith('.tls'):
        return pick({'server_name': 'ros.example.test'}, None, {'insecure': True, 'ca': './ca.pem'})
    if field.endswith('.tls.insecure'):
        return maybe()
    if field in ROUTING_FIELDS:
        return pick([], ['a'], ['rs'], True, False, 'forward_local', 'head', {}, [{'exec': 'accept'}])
    if isinstance(example, bool):
        return maybe()
    if isinstance(example, int):
        return pick(0, 1, 2, 60, 300, 4096, example)
    if isinstance(example, list):
        return copy.deepcopy(example) if maybe() else []
    if isinstance(example, str) or '.tls.' in field or field in ('api.ssl.cert', 'api.ssl.key'):
        if field == 'listen' or field == 'api.listen':
            return pick('0.0.0.0:5353', '127.0.0.1:53', '[::]:53', '')
        return pick(example or 'x', '', './state/x', '测试')
    return current


def random_value(field):
    return pick(None, True, False, 0, -1, 10 ** 30, 1.5, '', 'x', '测试 😀', [], [1, 'a'], [65, 255], {}, {'a': 1},
                *UPSTREAMS, *HOSTS, *URLS, ['https://example.test/x.txt', 5], 'proxy', 'nodata')


def changes_for(read):
    capabilities = read.get('capabilities', {}) if isinstance(read, dict) else {}
    editable = [field for field, cap in capabilities.items() if isinstance(cap, dict) and cap.get('editable') and field in COMMON_FIELDS]
    values = read.get('values', {}) if isinstance(read, dict) else {}
    changes = {}
    for _ in range(R.choice((1, 1, 2, 2, 3, 4))):
        r = R.random()
        if editable and r < 0.75:
            field = R.choice(editable)
            current = values
            for key in field.split('.'):
                current = current.get(key) if isinstance(current, dict) else None
            changes[field] = valid_value(field, current)
        elif r < 0.93:
            field = R.choice(FIELDS)
            changes[field] = random_value(field) if maybe(0.5) else valid_value(field, None)
        elif r < 0.97:
            field = R.choice(ROUTING)
            changes[field] = valid_value(field, None)
        else:
            changes[pick('unknown_field', 'api', 'zz.测试')] = 1
    if maybe(0.02):
        return pick({}, [], None, 'x')
    return changes


def main():
    for _ in range(n(1600)):
        settings, cfg = base_settings()
        base_url = pick('', 'http://192.0.2.10:5000')
        try:
            read = read_common_settings(copy.deepcopy(settings), cfg, base_url)
        except Exception:
            read = {}
        for _ in range(R.choice((1, 1, 2))):
            changes = changes_for(read)
            try:
                apply_common_settings(copy.deepcopy(settings), changes, cfg, base_url)
            except Exception:
                pass
    rows = rec._RECORDS
    out = os.path.join(os.path.dirname(os.path.abspath(__file__)), 'c24_corpus.jsonl.gz')
    data = '\n'.join(json.dumps(row, ensure_ascii=False) for row in rows) + '\n'
    with open(out, 'wb') as raw:
        with gzip.GzipFile(fileobj=raw, mode='wb', mtime=0) as fh:
            fh.write(data.encode())
    counts = {}
    errors = {}
    for row in rows:
        counts[row['fn']] = counts.get(row['fn'], 0) + 1
        if 'err' in row:
            errors[row['err'][0]] = errors.get(row['err'][0], 0) + 1
    unsupported = sum(1 for row in rows if 'unsupported' in row)
    print(f'{len(rows)} rows {counts}; errors {errors}; {unsupported} unsupported -> {out}')


if __name__ == '__main__':
    main()
