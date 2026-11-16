"""G13: two provider graphs, raw-cache relay recovery, hosts and refresh policy."""
import copy
from datetime import datetime, timezone
import json
import yaml

from artifacts import Observer
from harness import Pair, json_body
from providers import Metadata
from sources import Source
from subscriptions import Exchange
from cases.generator_shapes import project
from cases.subscriptions import SUB, put_file, require

A, B, CROSS = '来源甲🧪', '来源乙🧪', '跨源组🧪'


def node(ident, name, server, dialer=None):
    return {'id': ident, 'name': name, 'type': 'ss', 'server': server, 'port': 8443,
            'params': {'cipher': 'aes-128-gcm', 'password': 'synthetic-ss', **({'dialer-proxy': dialer} if dialer else {})}}


def matrix(binary, recorder):
    for age in ('fresh', 'expired', 'interval-zero'):
        for filtered in (False, True):
            with Source(b'proxies: []\n') as source:
                subs = [{**SUB, 'name': A, 'url': source.url + '/a'},
                        {**SUB, 'id': 'sub-b', 'name': B, 'url': source.url + '/b'}]
                if filtered:
                    subs[0]['regex_filter'] = '^(Exit|Cross)'
                options = {'shared': {'subscriptions': subs}, 'profile': {
                    'proxy_groups': [{'id': 'delivery-outer', 'name': '来源总组🧪', 'type': 'select', 'subscriptions': ['sub-a', 'sub-b']},
                                     {'id': 'delivery-cross', 'name': CROSS, 'type': 'select', 'subscriptions': ['sub-b']}],
                    'rule_configs': [{'id': 'delivery-match', 'itemType': 'rule', 'rule_type': 'MATCH', 'policy': '来源总组🧪'}],
                    'mihomo': {'custom_config': 'dns:\n  enable: true\n  listen: 0.0.0.0:15353\n  proxy-server-nameserver: [https://dns.example/dns-query]\n'}}}
                with Pair(binary, recorder, options=options) as pair:
                    variant = age + ('/filtered' if filtered else '/all')
                    if age != 'interval-zero':
                        for sub in subs:
                            pair.request('G13', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/' + sub['id'],
                                         body=json_body({**sub, 'interval': 60}), expect=200, writes=True,
                                         interaction=Exchange(pair, source, expected=0), variant=variant + '/enable/' + sub['id'])
                    nodes = [[node('delivery-exit', 'Exit-A', 'node-a.example', 'Relay'),
                              node('delivery-cross', 'Cross-A', 'node-a.example', CROSS),
                              node('delivery-relay', 'Relay-A', 'relay.example')],
                             [node('delivery-b', 'Exit-B', 'node-b.example')]]
                    stamp = datetime.now(timezone.utc).isoformat() if age == 'fresh' else '2020-01-01T00:00:00+00:00'
                    original = []
                    for index, sub in enumerate(subs):
                        metadata = {'url': sub['url'], 'subscription_name': sub['name'], 'fetched_at': stamp,
                            'hosts': {'entries': {'node-a.example': 'entry-a.example'} if index == 0 else {'node-b.example': ['192.0.2.88']},
                                      'total': 1, 'skipped_count': 0, 'skipped': []}}
                        if index == 0:
                            metadata['dialers'] = {'targets': {'Relay': {'type': 'select', 'members': ['Relay-A']}}, 'unresolved': []}
                        value = {'subscription_id': sub['id'], 'updated_at': stamp, 'count': len(nodes[index]), 'metadata': metadata, 'nodes': nodes[index]}
                        original.append(value)
                        put_file(pair, 'shared/subscribes/' + sub['id'] + '.json', value)

                    def raw_cache():
                        for sub, expected in zip(subs, original):
                            require(json.loads((pair.dirs[0] / ('shared/subscribes/' + sub['id'] + '.json')).read_text()) == expected,
                                    'raw source cache, including filtered-out relay, hosts and provenance, stays unfiltered and unchanged')

                    def preview_check(reply, service):
                        raw_cache()
                        content = reply.json()['content']
                        require('/subscriptions/sub-a/proxies' in content and '/subscriptions/sub-b/proxies' in content and CROSS in content,
                                'both provider URLs and cross-source policy appear in main output')
                        if service == 'mihomo':
                            main = yaml.safe_load(content)
                            groups = {group['name']: group for group in main['proxy-groups']}
                            hidden = groups[A + '/Relay']
                            require(hidden['hidden'] is True and hidden['type'] == 'select', 'airport Relay gets a namespaced hidden main group')
                            if filtered:
                                require(hidden['proxies'] == [A + '/Relay-A'] and any(p['name'] == A + '/Relay-A' for p in main['proxies']),
                                        'relay removed by filtering recovered from raw cache and copied inline')
                            else:
                                require(hidden['use'] == [A] and hidden['filter'] == r'^(?:Relay\-A)$', 'unfiltered relay selected from its own provider by exact escaped name')
                            require(main['hosts']['node-a.example'] == 'entry-a.example' and main['hosts']['node-b.example'] == '192.0.2.88',
                                    'metadata from both delivered server domains merged into main hosts')

                    for service in ('mihomo', 'surge', 'loon'):
                        for index, sub in enumerate(subs):
                            query = '' if service == 'mihomo' else '?format=' + service
                            status = 400 if service != 'mihomo' and index == 0 else 200
                            observer = Metadata(pair, yaml=service == 'mihomo', served=service == 'mihomo', subscription_id=sub['id'])
                            def check(reply):
                                raw_cache()
                                if status == 400:
                                    expected = ('Surge policy-path 暂不支持 dialer-proxy，不能静默移除拨号配置' if service == 'surge'
                                                else 'Loon 订阅暂不支持 dialer-proxy，不能静默移除拨号配置')
                                    require(reply.json()['message'] == expected, 'unsupported client refuses exact dialer topology rather than silently dropping it')
                                    return
                                observer.reference(reply)
                                if service == 'mihomo':
                                    proxies = {proxy['name']: proxy for proxy in yaml.safe_load(reply.body)['proxies']}
                                    require(set(proxies) == ({'Exit-A', 'Cross-A'} | (set() if filtered else {'Relay-A'})) if index == 0 else set(proxies) == {'Exit-B'},
                                            'provider contains exact post-filter node set')
                                    if index == 0:
                                        require(proxies['Exit-A']['dialer-proxy'] == A + '/Relay' and proxies['Cross-A']['dialer-proxy'] == CROSS,
                                                'airport-local and cross-provider group references retain distinct namespaces')
                            pair.request('G13', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/' + sub['id'] + '/proxies' + query,
                                         expect=status, writes=True, check=check, capture=observer.capture if status == 200 else None,
                                         interaction=Exchange(pair, source, expected=0), variant=variant + '/' + service + '/' + sub['id'])
                    for service in ('mihomo', 'surge', 'loon'):
                        pair.request('G13', 'POST /api/generate/' + service + '/preview', body=json_body({'base_url': 'http://panel.example'}),
                                     expect=200, writes=True, projection=project(service), check=lambda r, s=service: preview_check(r, s),
                                     interaction=Exchange(pair, source, expected=0), variant=variant + '/main/' + service)
                    observer = Observer(pair)
                    def artifact_check(reply):
                        observer.reference(reply)
                        preview_check(reply, 'mihomo')
                    pair.request('G13', 'POST /api/generate/<service>/artifact', '/api/generate/mihomo/artifact',
                                 body=json_body({'base_url': 'http://panel.example'}), expect=200, writes=True, files=False,
                                 check=artifact_check, capture=observer.capture, projection=observer.public,
                                 interaction=Exchange(pair, source, expected=0), variant=variant + '/artifact')


GROUPS = {'delivery-graph': matrix}


def collisions(binary, recorder):
    from providers import MergedMetadata
    from cases.merged_provider import install
    for shape in ('same-name-different-definition', 'same-name-identical', 'same-name-active-chain'):
        subs = [{**SUB, 'name': A, 'url': ''}, {**SUB, 'id': 'sub-b', 'name': B, 'url': ''}]
        a = [node('same-a', '同名🧪', '192.0.2.1')]
        b = [node('same-b', '同名🧪', '192.0.2.1' if shape == 'same-name-identical' else '192.0.2.2')]
        if shape == 'same-name-active-chain':
            a.append(node('activate-a', '激活拨号🧪', '192.0.2.3', 'DIRECT'))
        options = {'shared': {'subscriptions': subs}, 'profile': {'proxy_groups': [
            {'id': 'same-group', 'name': '同名来源组🧪', 'type': 'select', 'subscriptions': ['sub-a', 'sub-b']}]}}
        with Pair(binary, recorder, options=options) as pair:
            install(pair, subs, [a, b])
            status = 400 if shape == 'same-name-active-chain' else 200
            observer = MergedMetadata(pair, [(A, len(a)), (B, len(b))])
            def check(reply):
                observer.reference(reply)
                proxies = yaml.safe_load(reply.body)['proxies']
                require(len(proxies) == 2 and {proxy['server'] for proxy in proxies} == {a[0]['server'], b[0]['server']},
                        'legacy same-name collections retain both source definitions without activating a chain')
            pair.request('G13', 'GET /api/subscriptions/proxies', expect=status, writes=True,
                         check=check if status == 200 else None, capture=observer.capture if status == 200 else None, variant=shape + '/merged')
            pair.request('G13', 'POST /api/generate/mihomo/preview', body=json_body({'base_url': 'http://panel.example'}),
                         expect=200, writes=True, projection=project('mihomo'), variant=shape + '/main')
            artifact = Observer(pair)
            pair.request('G13', 'POST /api/generate/<service>/artifact', '/api/generate/mihomo/artifact',
                         body=json_body({'base_url': 'http://panel.example'}), expect=200, writes=True, files=False,
                         check=artifact.reference, capture=artifact.capture, projection=artifact.public, variant=shape + '/artifact')


GROUPS['delivery-graph-collisions'] = collisions
