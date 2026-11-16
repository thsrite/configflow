"""G success recipes use only public generator previews and synthetic nodes."""
import base64
import copy
import json

import yaml

from compare import structure
from harness import Pair, json_body
from cases.generate import FILENAMES
from cases.subscriptions import require

UUID = '01234567-89ab-4cde-8f01-23456789abcd'


def node(kind, params):
    return {'id': 'shape-node', 'name': '外层' + kind + '🧪', 'type': kind, 'server': '192.0.2.7', 'port': 1443, 'params': params}


def fixture(value):
    return {'shared': {'nodes': [value]}, 'profile': {'proxy_groups': [
        {'id': 'shape-group', 'name': '生成组🧪', 'type': 'select', 'manual_nodes': ['shape-node', 'DIRECT']}]}}


def project(service):
    def parse(value, index):
        if isinstance(value, dict) and isinstance(value.get('content'), str):
            value = copy.deepcopy(value)
            value['content'] = structure(value['content'].encode(), filename=FILENAMES[service])
        return value
    return parse


def preview(pair, case, service, label, name):
    route = 'POST /api/generate/' + service + '/preview'
    def check(reply):
        content = reply.json()['content']
        require(name in content, 'selected synthetic node is present in generated output rather than silently skipped')
        require('inner-name-must-be-overridden' not in content, 'outer node name overrides parsed JSON/YAML name')
    pair.request(case, route, body=json_body({'base_url': 'http://panel.example'}), headers={'Host': 'panel.example'},
                 expect=200, writes=True, projection=project(service), check=check, variant=label + '/' + service)


def node_inputs(binary, recorder):
    flat = {'name': 'inner-name-must-be-overridden', 'type': 'ss', 'server': '192.0.2.7', 'port': 1443,
            'cipher': 'aes-128-gcm', 'password': 'synthetic-credential'}
    for shape in ('structured', 'json', 'yaml'):
        value = node('ss', {key: item for key, item in flat.items() if key in ('cipher', 'password')})
        if shape != 'structured':
            value['proxy_string'] = json.dumps(flat) if shape == 'json' else yaml.safe_dump(flat, sort_keys=False)
        for service in ('mihomo', 'surge', 'loon'):
            with Pair(binary, recorder, options=fixture(value)) as pair:
                preview(pair, 'G01', service, shape, value['name'])


def protocols():
    tls = {'sni': 'tls.example', 'skip-cert-verify': True, 'udp': True}
    ws = {'network': 'ws', 'ws-opts': {'path': '/ws?x=1&y=2', 'headers': {'Host': 'ws.example'}}}
    return {
        'ss': {'cipher': 'aes-128-gcm', 'password': 'synthetic-ss', 'udp': True},
        'ssr': {'cipher': 'aes-128-cfb', 'password': 'synthetic-ssr', 'protocol': 'auth_sha1_v4', 'obfs': 'tls1.2_ticket_auth'},
        'vmess': {'uuid': UUID, 'alterId': 0, 'tls': True, **tls, **ws},
        'vless': {'uuid': UUID, 'tls': True, **tls, **ws},
        'trojan': {'password': 'synthetic-trojan', **tls, **ws},
        'hysteria2': {'password': 'synthetic-hy2', **tls},
        'http': {'username': 'synthetic-user', 'password': 'synthetic-password'},
        'https': {'username': 'synthetic-user', 'password': 'synthetic-password', **tls},
        'socks5': {'username': 'synthetic-user', 'password': 'synthetic-password', 'tls': True, **tls},
        'snell': {'psk': 'synthetic-snell', 'version': 4, 'reuse': True, 'obfs-opts': {'mode': 'http', 'host': 'obfs.example'}, **tls},
        'tuic': {'token': 'synthetic-tuic-token', 'alpn': 'h3', **tls},
        'tuic-v5': {'uuid': UUID, 'password': 'synthetic-tuic-password', 'alpn': 'h3', **tls},
        'anytls': {'password': 'synthetic-anytls', **tls},
        'wireguard': {'private-key': base64.b64encode(bytes([1]) * 32).decode(), 'public-key': base64.b64encode(bytes([2]) * 32).decode(),
                      'ip': '192.0.2.9/32', 'ipv6': '2001:db8::9/128', 'dns': ['192.0.2.53', '2001:db8::53'], 'mtu': 1420,
                      'allowed-ips': ['0.0.0.0/0', '::/0'], 'persistent-keepalive': 25},
    }


def client_protocols(binary, recorder):
    kinds = {'surge': ('ss', 'vmess', 'trojan', 'hysteria2', 'http', 'https', 'snell', 'tuic', 'tuic-v5', 'vless', 'anytls', 'wireguard', 'wg'),
             'loon': ('ss', 'ssr', 'vmess', 'vless', 'trojan', 'hysteria2', 'anytls', 'http', 'https', 'socks5', 'wireguard', 'wg')}
    for service, selected in kinds.items():
        for kind in selected:
            params = protocols()['wireguard' if kind == 'wg' else kind]
            value = node(kind, params)
            with Pair(binary, recorder, options=fixture(value)) as pair:
                preview(pair, 'G03' if service == 'surge' else 'G04', service, kind, value['name'])


GROUPS = {'generator-node-inputs': node_inputs, 'generator-client-protocols': client_protocols}


def group_shapes(binary, recorder):
    for kind in ('select', 'url-test', 'fallback', 'load-balance', 'follow', 'chain'):
        for service in ('mihomo', 'surge', 'loon'):
            value = node('socks5', {'username': 'synthetic-user', 'password': 'synthetic-password'})
            opts = fixture(value)
            opts['shared']['nodes'].append({**value, 'id': 'second-node', 'name': '另一个节点🧪', 'server': '192.0.2.8'})
            source = {'id': 'g-source', 'name': '普通来源组', 'type': 'select', 'manual_nodes': ['second-node']}
            group = {'id': 'shape-group', 'name': '形态' + kind + '🧪', 'type': kind, 'manual_nodes': ['shape-node'],
                     'include_groups': ['g-source'], 'proxies_order': [{'type': 'strategy', 'id': 'g-source'}, {'type': 'node', 'id': 'shape-node'}]}
            if kind in ('url-test', 'fallback', 'load-balance'):
                group.update(url='https://probe.example/204', interval=120, timeout=3456, lazy=False, **{'expected-status': '200-299/304'})
                if kind == 'url-test':
                    group['tolerance'] = 75
                if kind == 'load-balance':
                    group['strategy'] = 'round-robin'
            elif kind == 'follow':
                group = {'id': 'shape-group', 'name': '形态follow🧪', 'type': 'url-test', 'follow_group': 'g-source'}
            elif kind == 'chain':
                group = {'id': 'shape-group', 'name': '形态chain🧪', 'type': 'chain',
                         'chain': {'entry': {'type': 'node', 'id': 'shape-node'}, 'exit': {'type': 'node', 'id': 'second-node'}}}
            opts['profile']['proxy_groups'] = [source, group]
            with Pair(binary, recorder, options=opts) as pair:
                if kind == 'chain' and service != 'mihomo':
                    pair.request('G05', 'POST /api/generate/' + service + '/preview', body='{}', expect=400, writes=True,
                                 check=lambda r: require('代理链' in r.json()['message'], 'unsupported client rejects active chain explicitly'),
                                 variant=kind + '/' + service)
                else:
                    preview(pair, 'G05', service, kind, group['name'])


GROUPS['generator-group-shapes'] = group_shapes


def uri_inputs(binary, recorder):
    from substore import SubStore, NodeExchange
    ss = 'ss://' + base64.b64encode(b'aes-128-gcm:synthetic').decode() + '@192.0.2.7:1443#uri-name'
    vmess = 'vmess://' + base64.b64encode(json.dumps({'v': '2', 'ps': 'uri-name', 'add': '192.0.2.7', 'port': '1443', 'id': UUID, 'aid': '0', 'net': 'tcp', 'tls': 'tls'}).encode()).decode()
    ssr = 'ssr://' + base64.b64encode(('192.0.2.7:1443:origin:aes-128-cfb:plain:' + base64.b64encode(b'synthetic').decode()
                                    + '/?remarks=' + base64.b64encode(b'uri-name').decode()).encode()).decode()
    links = {'ss': ss, 'ssr': ssr, 'vmess': vmess, 'vless': 'vless://' + UUID + '@192.0.2.7:1443?security=tls&type=ws#uri-name',
             'trojan': 'trojan://synthetic@192.0.2.7:1443?sni=tls.example#uri-name',
             'hysteria': 'hysteria://synthetic@192.0.2.7:1443?upmbps=10&downmbps=20#uri-name',
             'hysteria2': 'hysteria2://synthetic@192.0.2.7:1443?sni=tls.example#uri-name',
             'base64-list': base64.b64encode((ss + '\n').encode()).decode()}
    output = yaml.safe_dump({'proxies': [{'name': 'converted-inner', 'type': 'ss', 'server': '192.0.2.7', 'port': 1443,
                                         'cipher': 'aes-128-gcm', 'password': 'synthetic-converted'}]}, sort_keys=False).encode()
    for kind, link in links.items():
        for service in ('mihomo', 'surge', 'loon'):
            with SubStore(output) as store:
                value = node('ss', {})
                value['proxy_string'] = link
                opts = fixture(value)
                opts['environment'] = {'SUB_STORE_URL': store.url}
                with Pair(binary, recorder, options=opts) as pair:
                    def check(reply):
                        text = reply.json()['content']
                        require('converted-inner' not in text, 'outer name overrides fake/preflight proxy name')
                        unsupported = service != 'mihomo' and (kind == 'base64-list' or kind == 'hysteria' or (kind == 'ssr' and service == 'surge'))
                        if unsupported:
                            section = text.split('[Proxy]', 1)[1].split('\n[', 1)[0] if '[Proxy]' in text else ''
                            require(value['name'] + ' =' not in section, 'client-local parser omits unsupported URI type instead of claiming fake output is its native renderer')
                        else:
                            require(value['name'] in text, 'supported URI renders with its outer name')
                    pair.request('G02', 'POST /api/generate/' + service + '/preview',
                                 body=json_body({'base_url': 'http://panel.example'}), headers={'Host': 'panel.example'},
                                 expect=200, writes=True, projection=project(service), interaction=NodeExchange(store, link),
                                 check=check, variant=kind + '/' + service)


GROUPS['generator-uri-inputs'] = uri_inputs
