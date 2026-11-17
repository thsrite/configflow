"""G08 public subscription rendering of Mihomo DNS and EasyTier settings."""
import copy

import yaml

from harness import Pair, json_body
from cases.generator_shapes import fixture, node, project
from cases.subscriptions import require
from sources import Source
from cases.config_backup import message
from cases.pull_routes import SourceStable

EASYTIER = {'enabled': True, 'name': '虚拟网🧪', 'apply_to': 'subscription', 'routes': ['192.0.2.0/24'], 'magic_dns': True,
            'keepalive': {'enabled': False, 'url': 'http://192.0.2.50:18080/health', 'interval': 120},
            'options': {'network-name': 'synthetic-net', 'network-secret': 'synthetic-net-secret',
                        'peers': ['tcp://192.0.2.20:11010'], 'udp': True, 'tld-dns-zone': 'overlay.test'}}


def dns_fields(binary, recorder):
    for ipv6 in (False, True):
        opts = fixture(node('socks5', {'username': 'synthetic', 'password': 'synthetic'}))
        document = {'ipv6': ipv6, 'hosts': {'static.example': '192.0.2.11'}, 'dns': {
            'enable': True, 'ipv6': ipv6, 'enhanced-mode': 'fake-ip', 'fake-ip-range': '198.18.0.1/16',
            'nameserver': ['https://192.0.2.53/dns-query'], 'proxy-server-nameserver': ['https://192.0.2.54/dns-query']}}
        opts['profile']['mihomo'] = {'custom_config': yaml.safe_dump(document, sort_keys=False)}
        with Pair(binary, recorder, options=opts) as pair:
            def check(reply):
                generated = yaml.safe_load(reply.json()['content'])
                require(generated['ipv6'] is ipv6 and generated['dns']['ipv6'] is ipv6, 'both IPv6 fields keep boolean type/value')
                require(generated['hosts']['static.example'] == '192.0.2.11', 'explicit hosts survives generation')
                require(generated['dns']['proxy-server-nameserver'] == document['dns']['proxy-server-nameserver'], 'proxy DNS upstreams survive generation')
            pair.request('G08', 'POST /api/generate/mihomo/preview', body='{}', expect=200, writes=True,
                         projection=project('mihomo'), check=check, variant='dns-ipv6/' + str(ipv6))


def easytier(binary, recorder):
    for scope in ('subscription', 'agent', 'all'):
        for keepalive in (False, True):
            settings = copy.deepcopy(EASYTIER)
            settings['apply_to'], settings['keepalive']['enabled'] = scope, keepalive
            opts = fixture(node('socks5', {'username': 'synthetic', 'password': 'synthetic'}))
            opts['profile']['mihomo'] = {'custom_config': 'dns:\n  enable: true\n', 'easytier': settings}
            with Pair(binary, recorder, options=opts) as pair:
                def check(reply):
                    generated = yaml.safe_load(reply.json()['content'])
                    proxies = [proxy for proxy in generated.get('proxies', []) if proxy.get('type') == 'easytier']
                    require(bool(proxies) is (scope != 'agent'), 'public subscription preview honors EasyTier apply_to scope')
                    if scope == 'agent':
                        require(all('虚拟网🧪' not in rule for rule in generated.get('rules', [])), 'agent-only overlay adds no subscription rules')
                        return
                    require(len(proxies) == 1 and proxies[0]['network-secret'] == 'synthetic-net-secret', 'exactly one configured outbound retains required secret in generated config')
                    target = '虚拟网🧪 保活' if keepalive else '虚拟网🧪'
                    require(generated['rules'][:2] == ['IP-CIDR,192.0.2.0/24,' + target + ',no-resolve', 'DOMAIN-SUFFIX,overlay.test,' + target],
                            'overlay and MagicDNS rules precede normal user rules')
                    require(generated['dns']['nameserver-policy']['+.overlay.test'] == 'et://虚拟网🧪', 'MagicDNS is scoped to configured overlay zone')
                    keep_groups = [group for group in generated.get('proxy-groups', []) if group['name'] == '虚拟网🧪 保活']
                    require(bool(keep_groups) is keepalive, 'keepalive group exists only when enabled')
                    if keepalive:
                        require(keep_groups[0]['url'] == settings['keepalive']['url'] and keep_groups[0]['interval'] == 120, 'keepalive probe values remain exact')
                pair.request('G08', 'POST /api/generate/mihomo/preview', body=json_body({'base_url': 'http://panel.example'}),
                             expect=200, writes=True, projection=project('mihomo'), check=check,
                             variant='easytier/' + scope + '/keepalive-' + str(keepalive))
    for variant in ('disabled', 'chain-dialer'):
        settings = copy.deepcopy(EASYTIER)
        opts = fixture(node('socks5', {'username': 'synthetic', 'password': 'synthetic'}))
        if variant == 'disabled':
            settings['enabled'] = False
        else:
            settings['options']['dialer-proxy'] = '前置链🧪'
            opts['shared']['nodes'].append({**opts['shared']['nodes'][0], 'id': 'exit-node', 'name': '落地🧪', 'server': '192.0.2.8'})
            opts['profile']['proxy_groups'].append({'id': 'chain-a', 'name': '前置链🧪', 'type': 'chain',
                'chain': {'entry': {'type': 'node', 'id': 'shape-node'}, 'exit': {'type': 'node', 'id': 'exit-node'}}})
        opts['profile']['mihomo'] = {'custom_config': 'dns:\n  enable: true\n', 'easytier': settings}
        with Pair(binary, recorder, options=opts) as pair:
            def check(reply):
                generated = yaml.safe_load(reply.json()['content'])
                proxies = [proxy for proxy in generated.get('proxies', []) if proxy.get('type') == 'easytier']
                require(bool(proxies) is (variant != 'disabled'), 'disabled overlay omits output')
                if proxies:
                    require(proxies[0]['dialer-proxy'] == '前置链🧪', 'EasyTier may reference the public lowered chain name')
            pair.request('G08', 'POST /api/generate/mihomo/preview', body='{}', expect=200, writes=True,
                         projection=project('mihomo'), check=check, variant='easytier/' + variant)


GROUPS = {'generator-mihomo-dns': dns_fields, 'generator-easytier': easytier}


def easytier_rejections(binary, recorder):
    for case in ('H342', 'H343'):
        settings = copy.deepcopy(EASYTIER)
        outbound = node('socks5', {'username': 'synthetic', 'password': 'synthetic'})
        if case == 'H342':
            settings['name'] = outbound['name']
            error = 'EasyTier 节点名「' + settings['name'] + '」与已有节点或策略组重名，请在 EasyTier 设置里改名'
        else:
            settings['options']['dialer-proxy'] = '不存在的拨号策略🧪'
            error = 'EasyTier 的 dialer-proxy「不存在的拨号策略🧪」不在生成的节点或策略组里'
        opts = fixture(outbound)
        opts['profile']['mihomo'] = {'custom_config': 'dns: {enable: true}\n', 'easytier': settings}
        with Source(b'rejected local EasyTier topology never downloads') as source, Pair(binary, recorder, options=opts) as pair:
            for template, prefix in [('/api/generate', '/api/generate'),
                    ('/api/profiles/<profile_id>/generate', '/api/profiles/default/generate')]:
                for suffix in ('/mihomo', '/mihomo/preview'):
                    pair.request(case, 'POST ' + template + suffix, prefix + suffix, body='{}', expect=500,
                        writes=True, interaction=SourceStable(pair, source), check=message(error),
                        variant='real-EasyTier-render/' + template + suffix)


GROUPS['generator-easytier-live-topology-rejections'] = easytier_rejections
