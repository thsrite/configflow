#!/usr/bin/env python3
"""Independent C18 public-boundary references; run only on the test servers.

Synthetic mode uses the existing offline Sub-Store/cache and A19/onboarding
collaborator fixtures. It does not emulate the converter or base-form validator.
Snapshot mode uses real public Python generation on a disposable private copy,
requires a loopback-only namespace, and prints static source coverage only.
"""
import ast
from collections import defaultdict
from copy import deepcopy
import gzip
import json
import logging
import os
from pathlib import Path
import shutil
import socket
import sqlite3
import sys
import tempfile
from unittest.mock import patch

logging.disable(logging.CRITICAL)
HITS = defaultdict(set)
ROWS = []
SOURCES = ('backend/converters/mihomo.py', 'backend/common/mihomo_base_settings.py')


def trace(frame, event, arg):
    if event == 'line':
        for source in SOURCES:
            if frame.f_code.co_filename.endswith('/' + source):
                HITS[source].add(frame.f_lineno)
    return trace


def coverage():
    result = {}
    for source in SOURCES:
        funcs = {}
        for node in ast.parse(Path(source).read_text()).body:
            if isinstance(node, (ast.FunctionDef, ast.ClassDef)):
                funcs[node.name] = sorted(HITS[source] & set(range(node.lineno, node.end_lineno + 1)))
        result[source] = funcs
    return result


def snapshot_coverage(snapshot):
    if {name for _, name in socket.if_nameindex()} != {'lo'}:
        raise SystemExit('snapshot coverage requires a loopback-only network namespace')
    with tempfile.TemporaryDirectory(prefix='c18-coverage-') as tmp:
        data = Path(tmp) / 'data'
        shutil.copytree(snapshot, data)
        os.environ.update(DATA_DIR=str(data), LOG_FILE=str(Path(tmp) / 'trace.log'), LOG_LEVEL='ERROR',
                          SUB_STORE_BUILTIN_DIR='/nonexistent')
        db = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
        profiles = [row[0] for row in db.execute("SELECT DISTINCT owner FROM fields WHERE scope='profile'")]
        db.close()
        from backend.common.config import get_config, get_system_config, reset_config_context
        from backend.converters import mihomo
        from backend.utils.provider_delivery import DeliverySnapshot, prepare_subscription_provider
        from backend.utils.proxy_server_dns import reported_core_version
        import yaml
        system = get_system_config()
        default = system.get('system_config', {}).get('default_profile_id') or 'default'
        counts = defaultdict(int)

        def offline(*args, **kwargs):
            raise ConnectionError('offline')

        sys.settrace(trace)
        try:
            for profile_id in profiles:
                cfg = get_config(profile_id)
                main = yaml.safe_load(mihomo.generate_mihomo_config(cfg, base_url='http://127.0.0.1:5001', preflight_providers=False))
                counts['main'] += 1
                mihomo.get_mihomo_provider_downloads(cfg, 'http://127.0.0.1:5001')
                mihomo.get_mihomo_ruleset_downloads(cfg, 'http://127.0.0.1:5001')
                counts['download-pair'] += 1
                snapshot_view = DeliverySnapshot.capture(profile_id, main, profile=cfg)
                for sub in cfg.get('subscriptions', []):
                    prepare_subscription_provider(sub, cfg, snapshot_view, fetch=offline, source_prepared=True)
                    counts['subscription'] += 1
                for agent in system.get('agents', []):
                    if (agent.get('profile_id') or default) == profile_id and agent.get('service_type', 'mihomo') == 'mihomo':
                        mihomo.generate_mihomo_config(cfg, base_url='http://127.0.0.1:5001', preflight_providers=False,
                                                     for_agent=True, region_probe=bool(agent.get('domain_discovery_enabled')),
                                                     mihomo_core_version=reported_core_version(agent))
                        counts['agent'] += 1
                reset_config_context()
        finally:
            sys.settrace(None)
        print(json.dumps({'counts': dict(counts), 'source_lines': coverage()}, sort_keys=True))


def synthetic():
    import gen_mihomo as gm
    import yaml
    from backend.converters import mihomo
    from backend.common import mihomo_base_settings as mbs
    from backend.utils import sub_store_client, subscription_hosts, subscription_dialers

    def record(name, fn, args, call, **extra):
        row = {'case': name, 'fn': fn, 'args': deepcopy(args), **deepcopy(extra)}
        try:
            row['out'] = gm.conv(call())
        except Exception as exc:
            row['err'] = [type(exc).__name__, str(exc)]
        ROWS.append(row)
        return row

    base = {'profile_id': 'edge', 'system_config': {'server_domain': 'https://panel.example.com', 'config_token': 'synthetic+token&x'},
            'nodes': [{'id': 'n', 'name': 'Node', 'type': 'http', 'server': '192.0.2.1', 'port': 443, 'params': {}}],
            'proxy_groups': [{'id': 'g', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['n', 'DIRECT']}],
            'subscriptions': [], 'subscription_aggregations': [], 'rule_configs': [], 'rule_library': []}

    def render(name, profile=None, caches=None, downloads=None, **options):
        profile = deepcopy(base if profile is None else profile)
        caches, downloads = caches or {}, downloads or {}
        args = dict(profile=profile, base_url='https://fallback.example.com', sync_lan_hosts=False,
                    region_probe=False, core_version=None, for_agent=False)
        args.update(options)
        gm.set_caches(caches, downloads)
        report, work = {}, deepcopy(profile)
        row = record(name, 'generate', args,
                     lambda: mihomo.generate_mihomo_config(work, args['base_url'], args['sync_lan_hosts'], preflight_providers=False,
                                                          region_probe=args['region_probe'], report=report,
                                                          mihomo_core_version=args['core_version'], for_agent=args['for_agent']),
                     caches=caches, downloads=downloads)
        row['mutated'] = gm.conv(work)
        row['report'] = gm.conv(report)
        if 'out' in row:
            row['document'] = gm.conv(yaml.safe_load(row['out']))
        for fn, call in [('provider_downloads', mihomo.get_mihomo_provider_downloads), ('ruleset_downloads', mihomo.get_mihomo_ruleset_downloads)]:
            record(name, fn, [profile, args['base_url']], lambda: call(deepcopy(profile), args['base_url']), caches=caches, downloads=downloads)

    def changed(name, **fields):
        render(name, dict(deepcopy(base), **deepcopy(fields)))

    def custom(name, document, **options):
        config = deepcopy(base)
        config['mihomo'] = {'custom_config': yaml.safe_dump(document, sort_keys=False, allow_unicode=True)}
        render(name, config, **options)

    def rule(name, **fields):
        return dict({'id': 'r', 'itemType': 'rule', 'rule_type': name, 'value': '', 'policy': 'DIRECT'}, **fields)

    def cache_for(sub, document):
        text = yaml.safe_dump(document, sort_keys=False)
        return {'nodes': sub_store_client.proxies_to_nodes(deepcopy(document.get('proxies', []))),
                'metadata': {'url': sub['url'], 'hosts': subscription_hosts.extract_subscription_hosts(text),
                             'dialers': subscription_dialers.extract_subscription_dialers(text)}, 'updated_at': '2026-11-17T00:00:00'}

    sys.settrace(trace)
    try:
        render('base')
        changed('empty', nodes=[], proxy_groups=[], system_config={})
        changed('unscoped', profile_id='', system_config={})
        changed('fallback-domain', system_config={'server_domain': '  '})
        for kind in ['ss', 'ssr', 'vmess', 'vless', 'trojan', 'hysteria', 'hysteria2', 'tuic', 'wireguard', 'snell', 'socks5', 'http', 'https', 'anytls']:
            params = {'password': 'synthetic-password', 'uuid': 'synthetic-uuid', 'cipher': 'aes-128-gcm', 'tls': True,
                      'sni': 'sni.example.com', 'network': 'ws', 'ws-opts': {'path': '/ws', 'headers': {'Host': 'ws.example.com'}},
                      'alpn': ['h2', 'http/1.1'], 'private-key': 'private', 'public-key': 'public', 'ip': '192.0.2.2',
                      'ipv6': '2001:db8::2', 'udp': True}
            changed('protocol-' + kind, nodes=[dict(base['nodes'][0], type=kind, params=params)])
            raw = dict(params, type=kind, name='Inner', server='2001:db8::1', port=443)
            changed('raw-' + kind, nodes=[dict(id='n', name='Outer', proxy_string=json.dumps(raw))])
        for text in ['http://user:synthetic@192.0.2.1:443#inner', 'vless://synthetic@192.0.2.1:443',
                     'unknown://empty', 'unknown://fail', 'type: http\nserver: 192.0.2.1\nport: 443\n']:
            changed('node-string-' + str(len(ROWS)), nodes=[dict(id='n', name='Outer', proxy_string=text)])
        changed('unknown-policy', rule_configs=[rule('MATCH', policy='Missing')])
        changed('disabled-node', nodes=[dict(base['nodes'][0], enabled=False)])
        changed('unused-node', proxy_groups=[dict(base['proxy_groups'][0], manual_nodes=['DIRECT'])])
        changed('no-groups', proxy_groups=[])
        changed('default-groups-after-missing-follow', proxy_groups=[dict(base['proxy_groups'][0], follow_group='missing')])
        render('lan-hosts-empty', sync_lan_hosts=True)
        for kind in ['select', 'url-test', 'fallback', 'load-balance', 'relay']:
            group = dict(base['proxy_groups'][0], type=kind, url='https://probe.example.com/204', interval=61,
                         tolerance=17, lazy=False, strategy='round-robin', timeout=1500, hidden=True,
                         **{'max-failed-times': 3, 'disable-udp': True, 'expected-status': '204',
                            'icon': 'https://icons.example.com/icon.png', 'exclude-filter': 'Skip', 'exclude-type': 'ss'})
            changed('group-' + kind, proxy_groups=[group])
            follower = dict(id='follow', name='Follower', type='url-test', follow_group='g', url='https://ignored.example.com', tolerance=45)
            changed('follow-' + kind, proxy_groups=[group, follower])
        changed('follow-missing-disabled', proxy_groups=[dict(base['proxy_groups'][0], enabled=False),
                                                        dict(id='f', name='Missing', type='select', follow_group='missing')])
        for mode in ['nodes_first', 'strategies_first', 'precise', 'legacy-node', 'legacy-strategy']:
            group = dict(base['proxy_groups'][0], include_groups=['other'], proxy_order=mode)
            if mode == 'precise':
                group['proxies_order'] = [{'type': 'strategy', 'id': 'other'}, {'type': 'node', 'id': 'DIRECT'}]
            if mode.startswith('legacy'):
                group = dict(id='g', name='Proxy', type='select', source='node' if mode.endswith('node') else 'strategy', proxies=['n'] if mode.endswith('node') else ['other'])
            changed('order-' + mode, proxy_groups=[group, dict(id='other', name='Other', type='select', manual_nodes=['DIRECT'])])
        sub = {'id': 's', 'name': 'Feed', 'url': 'https://feed.example.com/nodes?token=synthetic-feed-token'}
        agg = dict(id='a', name='Aggregate', subscriptions=['s'], nodes=['n'])
        for health in [{}, {'health_check_url': '  https://probe.example.com/204  '}, {'health_check': {'enable': False}},
                       {'health_check': {'enable': True, 'url': 'https://probe.example.com/204', 'interval': 120, 'lazy': False,
                                         'timeout': 1500, 'expected-status': '204'}}, {'health_check': {'interval': 1}}]:
            group = dict(base['proxy_groups'][0], subscriptions=['s'])
            changed('provider-health-' + str(len(ROWS)), proxy_groups=[group], subscriptions=[dict(sub, **health)])
        for interval in [0, 60, 604800, 59, True, '3600']:
            changed('provider-interval-' + str(interval), mihomo={'provider_update_interval': interval},
                    subscriptions=[sub], proxy_groups=[dict(base['proxy_groups'][0], subscriptions=['s'])])
        groups = [dict(base['proxy_groups'][0], subscriptions=['s'], aggregations=['a'], regex='^Keep', exclude_regex='Skip',
                       aggregation_regex='Agg', aggregation_exclude_regex='Other'),
                  dict(id='other', name='Other', type='select', subscriptions=['s'], exclude_regex='Different')]
        changed('provider-source-isolation', proxy_groups=groups, subscriptions=[sub], subscription_aggregations=[agg])
        changed('aggregation-only', proxy_groups=[dict(id='g', name='Proxy', type='select', aggregations=['a'])],
                subscriptions=[sub], subscription_aggregations=[agg])
        second = dict(id='b', name='Relay', type='http', server='198.51.100.1', port=8443, params={})
        dialed = dict(base['nodes'][0], dialer_ref={'type': 'node', 'id': 'b'})
        changed('dialer-dependency', nodes=[dialed, second])
        changed('dialer-group', nodes=[dict(base['nodes'][0], dialer_ref={'type': 'group', 'id': 'relay'}), second],
                proxy_groups=[deepcopy(base['proxy_groups'][0]), dict(id='relay', name='Relay Group', type='select', manual_nodes=['b'])])
        changed('dialer-missing-reject', nodes=[dict(base['nodes'][0], dialer_ref={'type': 'node', 'id': 'missing'})])
        chain = dict(id='chain', name='Chain', type='chain', chain={'entry': {'type': 'node', 'id': 'b'}, 'exit': {'type': 'node', 'id': 'n'}})
        changed('chain-rule-only', nodes=[deepcopy(base['nodes'][0]), second], proxy_groups=[chain], rule_configs=[rule('MATCH', policy='Chain')])
        for kind in ['select', 'url-test', 'fallback', 'load-balance']:
            dynamic = dict(id='exit', name='Exit', type=kind, subscriptions=['s'], regex='^Keep', url='https://probe.example.com', interval=37, lazy=False, tolerance=23, strategy='round-robin')
            dynamic_chain = deepcopy(chain)
            dynamic_chain['chain']['exit'] = {'type': 'group', 'id': 'exit'}
            changed('dynamic-chain-' + kind, nodes=[deepcopy(base['nodes'][0]), second], proxy_groups=[dynamic_chain, dynamic], subscriptions=[sub])
        changed('chain-cycle', nodes=[deepcopy(base['nodes'][0]), second], proxy_groups=[dict(chain, chain={'entry': {'type': 'group', 'id': 'chain'}, 'exit': {'type': 'node', 'id': 'n'}})])
        for value in [None, '', False, 0, [], {}]:
            rs = dict(id='r', itemType='ruleset', name='Rules', library_rule_id='lib', url='/fallback.yaml', policy='DIRECT')
            changed('empty-library-url-' + type(value).__name__, rule_library=[dict(id='lib', name='Library', url=value)], rule_configs=[rs])
        for fmt, suffix in [('', 'yaml'), ('', 'YML?x=1#fragment'), ('', 'mrs'), ('', 'list'), ('text', 'yaml'), ('yaml', 'txt'), ('mrs', 'txt')]:
            rs = dict(id='r', itemType='ruleset', name='Rules', url='https://rules.example.com/r.' + suffix, format=fmt, policy='DIRECT', no_resolve=True)
            changed('ruleset-' + str(len(ROWS)), rule_configs=[rs])
            changed('library-ruleset-' + str(len(ROWS)), rule_configs=[dict(rs, library_rule_id='lib')], rule_library=[dict(id='lib', name='Library', url=rs['url'], format=fmt)])
        for content in ['DOMAIN,example.com', 'payload:\n  - example.com']:
            changed('content-' + str(len(ROWS)), rule_configs=[dict(id='r', itemType='ruleset', name='Content', url='/content', policy='DIRECT')],
                    rule_library=[dict(name='Content', source_type='content', content=content)])
        changed('github-proxy', system_config={'github_proxy_domain': 'https://proxy.example.com/', 'server_domain': 'https://panel.example.com'},
                rule_configs=[dict(id='r', itemType='ruleset', name='Rules', url='https://raw.githubusercontent.com/example/rules/main/a.yaml', policy='DIRECT')])
        for kind, value in [('MATCH', ''), ('RULE-SET', 'Rules'), ('DOMAIN', 'example.com'), ('NETWORK', 'udp'),
                            ('AND', '((SRC-IP-CIDR,192.0.2.0/24),(IP-CIDR,198.51.100.0/24))'),
                            ('OR', '((IP-CIDR6,2001:db8::/32),(GEOIP,CN,no-resolve))'), ('NOT', '((IP-ASN,64512))')]:
            changed('rule-' + kind, rule_configs=[rule(kind, value=value, no_resolve=True)])
        changed('udp-ntp', rule_configs=[rule('MATCH', udp_fallback='REJECT', ntp_direct=True), rule('DOMAIN', value='after.example.com')])
        changed('disabled-rules', rule_configs=[dict(rule('MATCH'), enabled=False), dict(rule('DOMAIN'), library_enabled=False)])
        full = {'ipv6': True, 'find-process-mode': False, 'mixed-port': 7890,
                'tun': {'enable': True, 'stack': 'mixed', 'mtu': 1500, 'auto-route': True, 'dns-hijack': ['any:53']},
                'dns': {'enable': True, 'ipv6': False, 'listen': '127.0.0.1:1053', 'nameserver': ['192.0.2.53'],
                        'nameserver-policy': {'+.example.com': 'https://dns.example.com/query'}, 'fake-ip-range': '198.18.0.1/16'},
                'sniffer': {'enable': True, 'sniff': {'HTTP': {'ports': [80, '8080-8880'], 'override-destination': True}, 'TLS': {'ports': [443]}}},
                'hosts': {'node.example.com': '192.0.2.7'},
                'listeners': [{'name': 'Custom', 'type': 'mixed', 'listen': '::1', 'port': 17890, 'proxy': 'Proxy'}]}
        custom('base-all-sections', full)
        for path in ['ipv6', 'dns.ipv6']:
            for value in [True, False, 'false', None, 0]:
                doc = deepcopy(full)
                if path == 'ipv6': doc[path] = value
                else: doc['dns']['ipv6'] = value
                custom('ipv6-' + path + '-' + str(value), doc)
        for value in [' off ', True, False, ' strict ', 'invalid']:
            custom('process-mode-' + str(value), {'find-process-mode': value})
        for text in ['secret: [synthetic-secret', '[]', 'null', '   ', 'listeners:\n  - name: bad\n    proxy: Missing\n']:
            changed('custom-rejection-' + str(len(ROWS)), mihomo={'custom_config': text})
        for port in [17999, 7890, '18001', 17890]:
            cfg = deepcopy(base)
            cfg.update(mihomo={'custom_config': yaml.safe_dump(full)}, domain_discovery={'region_probe_port': port})
            render('probe-' + str(port), cfg, region_probe=True)
        replacement = deepcopy(full)
        replacement['listeners'].append({'name': 'configflow-region-probe', 'type': 'mixed', 'port': 18009, 'proxy': 'Proxy'})
        cfg = dict(deepcopy(base), mihomo={'custom_config': yaml.safe_dump(replacement)})
        render('probe-replaces-own-listener', cfg, region_probe=True)
        cfg = deepcopy(base)
        cfg.update(mihomo={'custom_config': yaml.safe_dump(full)}, mosdns={'custom_hosts': 'node.example.com 192.0.2.8\n192.0.2.9 extra.example.com'})
        render('lan-hosts-enabled', cfg, sync_lan_hosts=True)
        render('lan-hosts-disabled', cfg)
        cfg['mihomo']['custom_config'] = yaml.safe_dump({'hosts': ['legacy'], 'dns': {'enable': True}})
        render('lan-hosts-replaces-non-mapping', cfg, sync_lan_hosts=True)
        host_doc = {'proxies': [{'name': 'Keep Node', 'type': 'http', 'server': 'node.example.com', 'port': 443}],
                    'hosts': {'node.example.com': 'alias.example.com', 'unrelated.example.com': '192.0.2.10'}}
        cfg = deepcopy(base)
        cfg.update(subscriptions=[sub], proxy_groups=[dict(base['proxy_groups'][0], subscriptions=['s'])])
        cache = cache_for(sub, host_doc)
        for version in [None, 'v1.19.19', 'v1.19.20', 'v1.19.31']:
            render('cached-hosts-' + str(version), cfg, {'s': cache}, core_version=version)
        combined = deepcopy(cfg)
        combined.update(mihomo={'custom_config': yaml.safe_dump(full)}, mosdns={'custom_hosts': 'node.example.com 192.0.2.8\n192.0.2.9 extra.example.com'})
        render('hosts-three-source-priority', combined, {'s': cache}, sync_lan_hosts=True)
        render('hosts-no-lan-injection', combined, {'s': cache}, sync_lan_hosts=False)
        stale = deepcopy(cache)
        stale['metadata']['url'] = 'https://other.example.com/source'
        render('cached-hosts-stale-url', cfg, {'s': stale})
        host_doc['proxies'][0]['dialer-proxy'] = 'Airport Relay'
        host_doc['proxy-groups'] = [{'name': 'Airport Relay', 'type': 'select', 'proxies': ['DIRECT']}]
        render('cached-dialer-group', cfg, {'s': cache_for(sub, host_doc)})
        for scope in ['subscription', 'agent', 'all']:
            settings = {'enabled': True, 'apply_to': scope, 'name': 'EasyTier', 'routes': ['192.0.2.0/24'],
                        'options': {'network-name': 'synthetic', 'network-secret': 'synthetic-secret', 'peers': ['tcp://198.51.100.1:11010']},
                        'keepalive': {'enabled': True, 'url': 'http://192.0.2.2/', 'interval': 60}, 'magic_dns': True}
            for for_agent in [False, True]:
                cfg = dict(deepcopy(base), mihomo={'easytier': settings})
                render('easytier-' + scope + '-' + str(for_agent), cfg, for_agent=for_agent, core_version='v1.19.31')
        for version in ['v1.19.30', 'unknown']:
            cfg = dict(deepcopy(base), mihomo={'easytier': dict(settings, apply_to='all')})
            render('easytier-core-version-' + version, cfg, for_agent=True, core_version=version)
        supplied = {'proxy-providers': {'Kept': {'type': 'file', 'url': '/artifact', 'path': './providers/kept.yaml'}, 'Empty': {'url': ''}}}
        record('supplied-main', 'provider_downloads', [base, '', supplied], lambda: mihomo.get_mihomo_provider_downloads(base, main_config=supplied))
        # Every form field is checked when changed, grandfathered unchanged,
        # and removable. External fake-range/DNS-loop hooks use corpus fixtures.
        def validate(name, doc, previous=None, reserved=None, local=None):
            record(name, 'validate_base_settings', [doc, previous, {str(k): v for k, v in (reserved or {}).items()}, local or []],
                   lambda: mbs.validate_base_settings(doc, previous, reserved_ports=reserved, local_addresses=local or []))
        def path_doc(path, value):
            out, target = {}, None
            target = out
            for key in path[:-1]: target = target.setdefault(key, {})
            target[path[-1]] = value
            return out
        for path in mbs.BOOLEANS:
            for value in [True, False, 'true', 1, None]:
                doc = path_doc(path, value)
                validate('boolean-' + '.'.join(path) + '-' + str(value), doc)
                validate('unchanged-' + '.'.join(path) + '-' + str(value), doc, deepcopy(doc))
        for path, (_, low, high) in mbs.INTEGERS.items():
            for value in [low - 1, low, high, high + 1, True, str(low)]:
                doc = path_doc(path, value)
                validate('integer-' + '.'.join(path) + '-' + str(value), doc)
                validate('unchanged-integer-' + '.'.join(path) + '-' + str(value), doc, deepcopy(doc))
        for path, (_, choices) in mbs.ENUMS.items():
            for value in list(choices) + ['INVALID', False, None]:
                validate('enum-' + '.'.join(path) + '-' + str(value), path_doc(path, value))
        for path in list(mbs.STRING_LISTS) + list(mbs.TEXTS):
            for value in [None, '', 'value', [], ['value'], [''], [1]]:
                doc = path_doc(path, value)
                validate('text-' + '.'.join(path) + '-' + str(value), doc)
                validate('unchanged-text-' + '.'.join(path) + '-' + str(value), doc, deepcopy(doc))
        for path in list(mbs.BOOLEANS) + list(mbs.INTEGERS) + list(mbs.ENUMS) + list(mbs.STRING_LISTS) + list(mbs.TEXTS):
            validate('removed-' + '.'.join(path), {}, path_doc(path, 'legacy'))
        for host in ['127.0.0.1', '[::1]', '0.0.0.0', '[::]', 'localhost', '192.0.2.1', '::1', '[::ffff:127.0.0.1]', 'bad', '[::1']:
            for secret in [None, '', '123456', 'short', 'valid-synthetic-secret', 12345678]:
                doc = {'external-controller': host + ':9090'}
                if secret is not None: doc['secret'] = secret
                validate('controller-' + host + '-' + str(secret), doc)
        for controller in ['9090', 9090, '', None]:
            validate('controller-shape-' + str(controller), {'external-controller': controller})
        for secret in ['123456', 12345678]:
            old = {'external-controller': '0.0.0.0:9090', 'secret': secret, 'log-level': 'info'}
            validate('old-controller-unrelated-edit-' + str(secret), dict(old, **{'log-level': 'debug'}), old)
        for bind in ['*', '192.0.2.1', '[2001:db8::1]', 'bad', 1, None]:
            validate('bind-' + str(bind), {'bind-address': bind})
        for url in ['https://ui.example.com/file.zip', 'ftp://ui.example.com/file.zip', 'https://[bad', '', None]:
            validate('ui-url-' + str(url), {'external-ui-url': url})
        for value in [None, 'legacy']:
            validate('fingerprint-' + str(value), {'global-client-fingerprint': value})
            validate('fingerprint-unchanged-' + str(value), {'global-client-fingerprint': value}, {'global-client-fingerprint': value})
        for ports in [[80, '8080-8880'], [], [True], ['0'], ['65536'], ['443-80'], ['443'], [' 443 - 8443 '], [10 ** 100], [str(10 ** 100)], ['１-６５５３５'], ['²'], ['abc'], '443']:
            validate('sniff-ports-' + str(ports), {'sniffer': {'sniff': {'HTTP': {'ports': ports}}}})
        for sniff in ['invalid', None, {'UNKNOWN': {'ports': [443]}}, {'TLS': {'ports': [443], 'override-destination': 'true'}}]:
            validate('sniff-shape-' + str(sniff), {'sniffer': {'sniff': sniff}})
            validate('sniff-shape-unchanged-' + str(sniff), {'sniffer': {'sniff': sniff}}, {'sniffer': {'sniff': sniff}})
        for doc in [{'mixed-port': 7890, 'socks-port': 7890}, {'mixed-port': 9090, 'external-controller': '127.0.0.1:9090'},
                    {'mixed-port': 1053, 'dns': {'listen': '127.0.0.1:1053'}},
                    {'mixed-port': 1053, 'dns': {'enable': False, 'listen': '127.0.0.1:1053'}},
                    {'mixed-port': 7890, 'listeners': [{'name': 'Custom', 'port': 7890}]},
                    {'mixed-port': 7890, 'listeners': [{'name': 'configflow-region-probe', 'port': 7890}]}]:
            validate('port-conflict-' + str(len(ROWS)), doc)
            validate('old-conflict-unchanged-' + str(len(ROWS)), doc, deepcopy(doc))
        validate('agent-reserved-port', {'mixed-port': 17890}, reserved={17890: 'Synthetic Agent'})
        for dns in [{'enable': True, 'nameserver': []}, {'respect-rules': True}, {'respect-rules': True, 'proxy-server-nameserver': ['192.0.2.53']},
                    {'nameserver': ['https://loop.example.com/query']}, {'fake-ip-range': 'bad/range'}, {'fake-ip-range': 1}, {'fake-ip-range': '198.18.0.1/16'}]:
            validate('dns-requirement-' + str(len(ROWS)), {'dns': dns}, local=['192.0.2.1', '2001:db8::1', 'bad'])
        validate('non-document', [], {})
        from backend.common import linux_gateway, router_split
        for enabled, broken, full_tun in [(False, False, False), (False, False, True), (True, False, True), (True, False, False), (False, True, False), (False, True, True)]:
            def gateway(_cfg):
                if broken: raise RuntimeError('synthetic gateway failure')
                return {'enabled': enabled}
            with patch.object(linux_gateway, 'gateway_settings', gateway), patch.object(router_split, 'is_full_tun', lambda _cfg: full_tun):
                record('takeover-' + str(len(ROWS)), 'takeover', [enabled, broken, full_tun], lambda: mbs.takeover_mode({}))
        def apply_gateway(main, _cfg, _url):
            providers = main.setdefault('rule-providers', {})
            providers['Gateway'] = {'type': 'http', 'url': '/gateway.rules', 'path': './ruleset/gateway.list', 'behavior': 'classical', 'format': 'text'}
            providers['File'] = {'type': 'file', 'url': '/ignored', 'path': './ruleset/local.list'}
            providers['Empty'] = {'type': 'http', 'url': '', 'path': './ruleset/empty.list'}
        with patch.object(linux_gateway, 'gateway_settings', lambda _cfg: {'enabled': True}), patch.object(linux_gateway, 'apply_linux_gateway', apply_gateway):
            record('gateway-derived-downloads', 'ruleset_downloads', [base, 'https://fallback.example.com'],
                   lambda: mihomo.get_mihomo_ruleset_downloads(deepcopy(base), 'https://fallback.example.com'), gateway_fixture=True)
        agents = {
            'm': {'id': 'm', 'name': 'Main', 'host': '192.0.2.1', 'port': 17890, 'service_type': 'mihomo'},
            'o': {'id': 'o', 'name': 'DNS', 'host': '192.0.2.1', 'port': 17891, 'service_type': 'oxidns'},
            'away': {'id': 'away', 'host': '192.0.2.2', 'port': 17892, 'service_type': 'oxidns'},
            'docker': {'id': 'docker', 'host': '192.0.2.1', 'port': 17893, 'service_type': 'mihomo', 'deployment_method': 'docker'},
            'duplicate': {'id': 'duplicate', 'host': '192.0.2.1', 'port': 17890},
        }
        for bindings, fail in [(list(agents) + ['gone'], False), (['o'], False), (['m', 'm', 'o'], False), (['m'], True)]:
            source = gm.FakeAgents(bindings, agents, fail)
            record('agent-context-' + str(len(ROWS)), 'agent_context', [bindings, agents, fail], lambda: list(mbs.agent_context(source, source, 'edge')))
    finally:
        sys.settrace(None)
        shutil.rmtree(gm.TMP)
    target = Path(__file__).with_name('review_mihomo.jsonl.gz')
    with gzip.open(target, 'wt') as output:
        for row in ROWS: output.write(json.dumps(row, ensure_ascii=False) + '\n')
    if path := os.getenv('C18_REVIEW_COVERAGE_PATH'):
        Path(path).write_text(json.dumps(coverage(), indent=2) + '\n')
    print(json.dumps({'rows': len(ROWS), 'rejections': sum('err' in row for row in ROWS)}, sort_keys=True))


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--snapshot-coverage':
        try:
            snapshot_coverage(Path(sys.argv[2]))
        except Exception:
            raise SystemExit('snapshot coverage failed (details suppressed)') from None
    else:
        synthetic()
