#!/usr/bin/env python3
"""Independent C19/C20 boundary references, derived from both Python converters.

Run on the test server from the repository root; no device/network calls.
--snapshot-coverage traces successful public generators on a disposable snapshot
copy in a network namespace, printing only static source line numbers and counts.
The default mode writes synthetic JSONL references for public generators and
subscription renderers, plus static source coverage (never real snapshot data).
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
import sqlite3
import socket
import sys
import tempfile

logging.disable(logging.CRITICAL)
FILES = ('surge.py', 'loon.py')
HITS = defaultdict(set)
ROWS = []


def trace(frame, event, arg):
    if event == 'line' and frame.f_code.co_filename.endswith(tuple('/converters/' + f for f in FILES)):
        HITS[Path(frame.f_code.co_filename).name].add(frame.f_lineno)
    return trace


def coverage():
    result = {}
    for filename in FILES:
        source = Path('backend/converters') / filename
        functions = {}
        tree = ast.parse(source.read_text())
        for node in tree.body:
            if isinstance(node, (ast.FunctionDef, ast.ClassDef)):
                lines = sorted(HITS[filename] & set(range(node.lineno, node.end_lineno + 1)))
                functions[node.name] = lines
        result[filename] = functions
    return result


def snapshot_coverage(snapshot):
    # The shell must already provide the same network isolation as proddiff.
    interfaces = {name for _, name in socket.if_nameindex()}
    if interfaces != {'lo'}:
        raise SystemExit('snapshot coverage requires a loopback-only network namespace')
    with tempfile.TemporaryDirectory(prefix='c19-c20-coverage-') as tmp:
        data = Path(tmp) / 'data'
        shutil.copytree(snapshot, data)
        os.environ.update(DATA_DIR=str(data), LOG_FILE=str(Path(tmp) / 'coverage.log'), LOG_LEVEL='ERROR',
                          SUB_STORE_BUILTIN_DIR='/nonexistent')
        db = sqlite3.connect(f'file:{data / "configflow.sqlite3"}?mode=ro', uri=True)
        profiles = [r[0] for r in db.execute("SELECT DISTINCT owner FROM fields WHERE scope='profile'")]
        db.close()
        from backend.common.config import get_config, reset_config_context
        from backend.converters import surge, loon, mihomo
        from backend.utils.provider_delivery import DeliverySnapshot, prepare_subscription_provider
        import yaml
        counts = defaultdict(int)

        def offline(*args, **kwargs):
            raise ConnectionError('offline')

        sys.settrace(trace)
        try:
            for profile_id in profiles:
                cfg = get_config(profile_id)
                for client in (surge, loon):
                    getattr(client, 'generate_' + client.__name__.rsplit('.', 1)[1] + '_config')(cfg, 'http://127.0.0.1:5001')
                    counts[client.__name__.rsplit('.', 1)[1] + '-config'] += 1
                main = yaml.safe_load(mihomo.generate_mihomo_config(cfg, base_url='http://127.0.0.1:5001', preflight_providers=False))
                snapshot_view = DeliverySnapshot.capture(profile_id, main, profile=cfg)
                for sub in cfg.get('subscriptions', []):
                    rendered = prepare_subscription_provider(sub, cfg, snapshot_view, fetch=offline, source_prepared=True)
                    proxies = yaml.safe_load(rendered['content'])['proxies']
                    surge.convert_proxies_to_surge_text(proxies)
                    loon.convert_proxies_to_loon_text(proxies)
                    counts['subscription-pair'] += 1
                reset_config_context()
        finally:
            sys.settrace(None)
        print(json.dumps({'counts': dict(counts), 'source_lines': coverage()}, sort_keys=True))


def emit(name, fn, args, call):
    row = {'case': name, 'fn': fn, 'args': deepcopy(args)}
    try:
        row['out'] = call()
    except Exception as exc:
        row['err'] = [type(exc).__name__, str(exc)]
    ROWS.append(row)


def synthetic():
    # Reuse only the existing deterministic offline Sub-Store collaborator;
    # these cases/expected outputs are independently derived from the sources.
    import gen_mihomo  # installs the fake mirrored by corpusEnv in Go
    from backend.converters import surge, loon
    base = {
        'profile_id': 'edge',
        'system_config': {'server_domain': 'https://panel.example.com', 'config_token': 'synthetic+token&x'},
        'nodes': [{'id': 'n', 'name': 'Node', 'type': 'ss', 'server': '192.0.2.1', 'port': 443,
                   'params': {'password': 'pw'}}],
        'proxy_groups': [{'id': 'g', 'name': 'Proxy', 'type': 'select', 'manual_nodes': ['n', 'DIRECT']}],
        'subscriptions': [], 'subscription_aggregations': [], 'rule_configs': [], 'rule_library': [],
    }

    def profile(name, **changes):
        config = dict(deepcopy(base), **deepcopy(changes))
        for fn, call in [('surge', surge.generate_surge_config), ('loon', loon.generate_loon_config)]:
            cfg = deepcopy(config)
            emit(name, fn, {'profile': cfg, 'base_url': 'https://fallback.example.com'},
                 lambda: call(cfg, 'https://fallback.example.com'))

    def rules(name, entries, **extra):
        profile(name, rule_configs=[dict({'id': f'r{i}', 'enabled': True, 'policy': 'DIRECT'}, **item)
                                   for i, item in enumerate(entries)], **extra)

    sys.settrace(trace)
    try:
        profile('empty', nodes=[], proxy_groups=[], system_config={})
        profile('base')
        profile('fallback-domain', system_config={'server_domain': '  '})
        profile('unscoped', profile_id='', system_config={})
        for kind in ['ss', 'ssr', 'vmess', 'vless', 'trojan', 'hysteria2', 'http', 'https', 'socks5',
                     'snell', 'tuic', 'tuic-v5', 'anytls', 'wireguard', 'wg', 'unsupported']:
            params = {'cipher': 'aes-128-gcm', 'password': 'p,w"x', 'uuid': 'synthetic-uuid',
                      'udp': True, 'tfo': True, 'tls': True, 'sni': 'sni.example.com',
                      'servername': 'preferred.example.com', 'skip-cert-verify': True, 'alpn': ['h2', 'http/1.1'],
                      'username': 'user', 'test-url': 'https://probe.example.com/204', 'psk': 'synthetic-psk',
                      'version': 4, 'reuse': True, 'token': 'synthetic-token', 'private-key': 'private',
                      'public-key': 'public', 'self-ip': '192.0.2.2/32', 'ipv6': '2001:db8::2/128',
                      'dns': ['192.0.2.53', '2001:db8::53'], 'mtu': 1280, 'allowed-ips': ['192.0.2.0/24', '2001:db8::/32'],
                      'keepalive': 25, 'preshared-key': 'shared', 'reserved': [1, 2, 3],
                      'protocol': 'origin', 'protocol-param': 'p,p', 'obfs': 'salamander', 'obfs-param': 'o,p',
                      'obfs-password': 'obfs,pw', 'ports': '4000-5000', 'hop-interval': 15,
                      'down': '\u3000１２３ Mbps', 'idle-session-timeout': 30}
            node = dict(base['nodes'][0], type=kind, params=params)
            profile('protocol-' + kind, nodes=[node])
            raw = dict(params, name='Raw', type=kind, server='2001:db8::1', port=443)
            profile('raw-' + kind, nodes=[dict(id='n', name='Outer', proxy_string=json.dumps(raw))])
            for fn, call in [('proxies_surge', surge.convert_proxies_to_surge_text), ('proxies_loon', loon.convert_proxies_to_loon_text)]:
                emit('subscription-' + kind, fn, [[raw]], lambda: call([deepcopy(raw)]))
        for params in [
            {'plugin': 'simple-obfs', 'plugin-opts': {'mode': 'tls', 'host': 'obfs.example.com'}},
            {'plugin': 'shadow-tls', 'plugin-opts': {'password': 'shadow', 'version': 2, 'host': 'shadow.example.com'}},
            {'plugin': 'unsupported'}, {'obfs-opts': {'mode': 'shadow-tls', 'password': 'shadow', 'version': 3}},
            {'obfs': 'tls', 'obfs-host': 'legacy.example.com'},
        ]:
            for kind in ['ss', 'snell']:
                profile('plugin-' + kind + '-' + str(len(ROWS)), nodes=[dict(base['nodes'][0], type=kind, params=params)])
        for kind in ['vmess', 'vless', 'trojan']:
            for network in ['tcp', 'ws', 'http', 'grpc', 'h2']:
                params = {'uuid': 'u', 'password': 'p', 'network': network, 'tls': True,
                          'ws-opts': {'path': '/a,b', 'headers': {'Host': 'ws.example.com'}},
                          'http-opts': {'path': ['/first', '/second'], 'headers': {'Host': ['http.example.com']}},
                          'grpc-opts': {'grpc-service-name': 'grpc'},
                          'flow': 'xtls-rprx-vision', 'reality-opts': {'public-key': 'pk', 'short-id': 'ab'}}
                profile(kind + '-' + network, nodes=[dict(base['nodes'][0], type=kind, params=params)])
        profile('http-without-auth', nodes=[dict(base['nodes'][0], type='http', params={})])
        profile('wireguard-without-keys', nodes=[dict(base['nodes'][0], type='wireguard', params={})])
        profile('no-node-type', nodes=[dict(base['nodes'][0], type='')])
        profile('http-transport-scalar-headers', nodes=[dict(base['nodes'][0], type='vmess', params={'network': 'http', 'http-opts': {'path': '/scalar', 'headers': {'Host': 'http.example.com'}}})])
        for node in [dict(name='Parse error', proxy_string=1), dict(name='Missing type')]:
            for fn, call in [('node_surge', surge.convert_node_to_surge), ('node_loon', loon.convert_node_to_loon)]:
                emit('node-rejection-' + str(len(ROWS)), fn, [node], lambda: call(deepcopy(node)))
        profile('unsupported-vless-flow', nodes=[dict(base['nodes'][0], type='vless', params={'flow': 'unsupported'})])
        for uri in ['', 'unknown://node', 'trojan://pw@192.0.2.1:443#inner', '{invalid']:
            profile('uri-' + str(len(ROWS)), nodes=[dict(id='n', name='Outer', proxy_string=uri)])
        profile('disabled-node', nodes=[dict(base['nodes'][0], enabled=False)])
        profile('unselected-node', proxy_groups=[dict(id='g', name='Proxy', type='select', manual_nodes=['DIRECT'])])
        for kind in ['select', 'url-test', 'fallback', 'load-balance', 'smart', 'unsupported']:
            for strategy in ['round-robin', 'consistent-hashing', 'sticky-sessions', 'unknown'] if kind == 'load-balance' else ['']:
                profile('group-' + kind + strategy, proxy_groups=[dict(base['proxy_groups'][0], type=kind, strategy=strategy)])
        profile('smart-override', surge={'smart_groups': [{'group_id': 'g', 'policy_priority': 'Node:1'}]})
        profile('legacy-source-node', proxy_groups=[dict(id='g', name='Proxy', type='select', source='node', proxies=['n'])])
        groups = [dict(base['proxy_groups'][0], include_groups=['empty'], proxy_order='strategies_first'),
                  dict(id='empty', name='Empty', type='select'),
                  dict(id='f', name='Follow', type='url-test', follow_group='g'),
                  dict(id='missing', name='Missing', type='select', follow_group='absent')]
        profile('follow-prune-order', proxy_groups=groups)
        groups[0]['proxies_order'] = [{'type': 'node', 'id': 'DIRECT'}, {'type': 'strategy', 'id': 'empty'}]
        profile('precise-order-appends-manual', proxy_groups=groups)
        groups[0]['proxies_order'] = [{'type': 'node', 'id': 'n'}, {'type': 'strategy', 'id': 'empty'}]
        profile('precise-order-resolves-node', proxy_groups=groups)
        profile('legacy-source-strategy', proxy_groups=[dict(id='parent', name='Parent', type='select', source='strategy', proxies=['g']), deepcopy(base['proxy_groups'][0])])
        profile('disabled-group', proxy_groups=[dict(base['proxy_groups'][0], enabled=False)])
        profile('cycle-groups', proxy_groups=[dict(id='a', name='A', type='select', include_groups=['b']),
                                              dict(id='b', name='B', type='select', include_groups=['a'])])
        resources = [dict(id='s', name='DIRECT'), dict(id='off', name='Off', enabled=False)]
        aggs = [dict(id='a', name='DIRECT', nodes=['n'])]
        groups = [dict(base['proxy_groups'][0], subscriptions=['s', 'off', 'missing'], aggregations=['a'],
                       regex='include"', exclude_regex='exclude', aggregation_regex='agg', aggregation_exclude_regex='other'),
                  dict(id='other', name='Other', type='select', subscriptions=['s'], aggregations=['a']),
                  dict(id='again', name='Again', type='select', subscriptions=['s'], exclude_regex='exclude')]
        profile('remote-isolation-and-collisions', proxy_groups=groups, subscriptions=resources, subscription_aggregations=aggs)
        profile('remote-cache-delimiter', subscriptions=[dict(id='a', name='First'), dict(id='a\x01b', name='Second')],
                proxy_groups=[dict(id='one', name='One', type='select', subscriptions=['a'], exclude_regex='b\x01c'),
                              dict(id='two', name='Two', type='select', subscriptions=['a\x01b'], exclude_regex='c')])
        profile('remote-cache-missing-source', subscriptions=[dict(id='a\x01b', name='Found')],
                proxy_groups=[dict(id='one', name='One', type='select', subscriptions=['a'], exclude_regex='b\x01c'),
                              dict(id='two', name='Two', type='select', subscriptions=['a\x01b'], exclude_regex='c')])
        for custom in ['', '[General]\nip-mode = ipv4-only\n[MITM]\nhostname = example.com',
                       '[General]\nloglevel = verbose\n[General]\nloglevel = notify\n[Host]\nx = 192.0.2.1\n[Host]\ny = 192.0.2.2',
                       '[Proxy]\nignored = DIRECT\n[Rule]\nFINAL,REJECT\n[Remote Rule]\nignored\n[Script]\nx = y']:
            profile('custom-' + str(len(ROWS)), surge={'custom_config': custom}, loon={'custom_config': custom})
        rules('custom-fallback-wins', [dict(itemType='rule', rule_type='MATCH', udp_fallback='REJECT')],
              surge={'custom_config': '[General]\nudp-policy-not-supported-behaviour = DIRECT'},
              loon={'custom_config': '[General]\nudp-fallback-mode = DIRECT'})
        for fallback in [None, 'DIRECT', 'REJECT']:
            rules('final-ntp-' + str(fallback), [dict(itemType='rule', rule_type='MATCH', udp_fallback=fallback, ntp_direct=True),
                                               dict(itemType='rule', rule_type='DOMAIN', value='after.example.com'),
                                               dict(itemType='ruleset', name='After', url='/after.txt')])
        for kind, value in [('DOMAIN', 'example.com'), ('NETWORK', ' udp '), ('SRC-IP-CIDR', '192.0.2.0/24'),
                            ('DST-PORT', '443'), ('SRC-PORT', '1/2'), ('IN-PORT', '1,2'),
                            ('PROCESS-NAME', 'app'), ('RULE-SET', 'https://rules.example.com/plain.txt'),
                            ('AND', '((NETWORK,udp),(DST-PORT,123))'),
                            ('OR', '((PROCESS-NAME,app),(DOMAIN,example.com))'),
                            ('NOT', '((DOMAIN,example.com))'), ('AND', '((DST-PORT,80/443))'),
                            ('AND', '((\u3000PROCESS-NAME\u3000,app),(DOMAIN,example.com))')]:
            rules('rule-' + kind + str(len(ROWS)), [dict(itemType='rule', rule_type=kind, value=value, no_resolve=True)])
        rules('disabled-rules', [dict(itemType='rule', rule_type='MATCH', library_enabled=False),
                                 dict(itemType='rule', rule_type='NETWORK', value='udp', library_enabled=False)])
        for format_value, url in [('', '/rules.txt'), ('', '/rules.YAML?x=1#fragment'), ('', '/rules.mrs'),
                                  ('text', '/rules.yaml'), ('yaml', '/rules.txt'), ('mrs', '/rules.txt')]:
            rules('ruleset-' + str(len(ROWS)), [dict(itemType='ruleset', name='Rule,=Name', url=url, format=format_value, no_resolve=True)])
        for content in ['DOMAIN,example.com', 'payload:\n  - example.com']:
            rules('content-' + str(len(ROWS)), [dict(itemType='ruleset', name='Content', url='/content')],
                  rule_library=[dict(name='Content', source_type='content', content=content)])
        rules('library-url-fallback', [dict(itemType='ruleset', name='Library', url='')],
              rule_library=[dict(name='Library', url='/library.yaml')])
        rules('blackmatrix-url', [dict(itemType='ruleset', name='Rule', url='https://rules.example.com/blackmatrix7/ios_rule_script/rule/Clash/x.list')])
        rules('empty-remote-rule', [dict(itemType='ruleset', name='Empty', url='')])
        rules('unknown-policy', [dict(itemType='rule', rule_type='MATCH', policy='Missing')], proxy_groups=[], nodes=[])
        for change in [dict(proxy_groups=[dict(id='chain', name='Chain', type='chain', hops=[])]),
                       dict(nodes=[dict(base['nodes'][0], dialer_ref={'type': 'node', 'id': 'absent'})])]:
            profile('rejected-topology-' + str(len(ROWS)), **change)
        for field, invalid in [('nodes', None), ('proxy_groups', None), ('surge', None), ('loon', None), ('rule_configs', None)]:
            profile('malformed-' + field, **{field: invalid})
        proxies = [dict(name='Broken', type='ss', server=[], port=443, cipher='aes-128-gcm', password='p'),
                   dict(name='Good', type='ss', server='192.0.2.1', port=443, cipher='aes-128-gcm', password='p')]
        for fn, call in [('proxies_surge', surge.convert_proxies_to_surge_text), ('proxies_loon', loon.convert_proxies_to_loon_text)]:
            emit('skip-failing-node', fn, [proxies], lambda: call(deepcopy(proxies)))
            dialers = [dict(proxies[1], **{'dialer-proxy': 'Other'})]
            emit('reject-subscription-dialer', fn, [dialers], lambda: call(deepcopy(dialers)))
    finally:
        sys.settrace(None)
    target = Path(__file__).with_name('review_clients.jsonl.gz')
    with gzip.open(target, 'wt') as output:
        for row in ROWS:
            output.write(json.dumps(row, ensure_ascii=False) + '\n')
    if path := os.getenv('C19_C20_COVERAGE_PATH'):
        Path(path).write_text(json.dumps(coverage(), indent=2) + '\n')
    print(json.dumps({'rows': len(ROWS), 'errors': sum('err' in row for row in ROWS)}, sort_keys=True))


if __name__ == '__main__':
    if len(sys.argv) == 3 and sys.argv[1] == '--snapshot-coverage':
        snapshot_coverage(Path(sys.argv[2]))
    else:
        synthetic()
