"""Merged provider data, cross-feed graph failures, source fallback and HEAD."""
import copy
import hashlib
import yaml

from harness import Pair, json_body
from sources import Source
from providers import MergedMetadata
from subscriptions import Exchange, configure, validate_ids
from cases.subscriptions import SUBS, CACHE_NODES, STAMP, put_file, require
from cases.subscription_fetch import FEED

ROUTE = 'GET /api/subscriptions/proxies'


def install(pair, subs, nodes, *, legacy=False):
    for sub, items in zip(subs, nodes):
        payload = {'subscription_id': sub['id'], 'nodes': items, 'count': len(items), 'updated_at': STAMP}
        if not legacy:
            payload['metadata'] = {'url': sub['url']}
        put_file(pair, 'shared/subscribes/' + sub['id'] + '.json', payload)


def basic(binary, recorder):
    vectors = [('S01', 'two', 200), ('S01', 'identical-crossfeed', 200), ('S01', 'different-definitions-no-chain', 200), ('S02', 'disabled', 200), ('S04', 'none', 200),
               ('B02', 'none', 200), ('F09', 'collision', 400), ('B01', 'unicode-collision', 400), ('F10', 'dialer', 400)]
    for case, shape, status in vectors:
        subs = copy.deepcopy(SUBS[:2])
        a, b = copy.deepcopy(CACHE_NODES), [{**CACHE_NODES[0], 'id': 'cached-c', 'name': '丙'}]
        details = [(subs[0]['name'], 2), (subs[1]['name'], 1)]
        if shape == 'disabled':
            subs[1]['enabled'] = False
            details = details[:1]
        elif shape == 'none':
            for sub in subs:
                sub['enabled'] = False
            details = []
        elif shape in ('collision', 'unicode-collision', 'identical-crossfeed', 'different-definitions-no-chain'):
            name = 'same-name' if shape == 'collision' else '同名🧪'
            a[0]['name'] = b[0]['name'] = name
            if shape != 'identical-crossfeed':
                b[0]['server'] = '192.0.2.99'  # Same name with a different definition.
            if shape in ('collision', 'unicode-collision'):
                a[1]['params'] = {'dialer-proxy': 'DIRECT'}  # Activate strict emitted-graph validation.
        elif shape == 'dialer':
            a[0]['params'] = {'dialer-proxy': 'missing-raw-target'}
        with Pair(binary, recorder, options={'shared': {'subscriptions': subs}}) as pair:
            install(pair, subs, [a, b])
            observer = MergedMetadata(pair, details)
            def check(reply):
                observer.reference(reply)
                require(len(yaml.safe_load(reply.body)['proxies']) == sum(n for _, n in details), 'merged enabled node count')
            pair.request('R222-' + case, ROUTE, expect=status, writes=True, variant=shape,
                         check=check if status == 200 else None, capture=observer.capture if status == 200 else None)


def remote(binary, recorder):
    for case, scenario in [('S03', 'http403-cache-fallback'), ('F11', 'invalid-source-no-fallback'), ('B91', 'HEAD-first-fetch')]:
        with Source(b'proxies: [bad]\n' if case == 'F11' else FEED) as source:
            if case == 'S03':
                source.status = 403
            sub = {**SUBS[0], 'url': source.url + '/feed'}
            with Pair(binary, recorder, options={'shared': {'subscriptions': [sub]}}) as pair:
                pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                             body=json_body({**sub, 'interval': 60}), expect=200, writes=True,
                             variant='merged/' + scenario + '/enable-refresh')
                if case != 'B91':
                    install(pair, [sub], [CACHE_NODES], legacy=True)
                path = 'shared/subscription-downloads/' + hashlib.sha256(sub['url'].encode()).hexdigest() + '.json'
                pair.policy.times.add('files.' + path + '.content.fetched_at')
                if case == 'B91':
                    configure(pair, sub['url'])
                method = 'HEAD' if case == 'B91' else 'GET'
                observer = MergedMetadata(pair, [(sub['name'], 2)], method=method)
                def check(reply):
                    observer.reference(reply)
                    if case == 'B91':
                        require(not reply.body, 'merged HEAD response is empty')
                        # Merged commit_cache_updates preserves converted nodes;
                        # unlike manual refresh it does not assign node IDs.
                        import json
                        payload = json.loads((pair.dirs[0] / 'shared/subscribes/sub-a.json').read_text())
                        require(payload.get('count') == 2 and len(payload.get('nodes', [])) == 2,
                                'merged HEAD persisted the complete node cache')
                pair.request('R222-' + case, ROUTE, method=method, expect=400 if case == 'F11' else 200,
                             writes=True, variant=scenario, interaction=Exchange(pair, source),
                             check=check if case != 'F11' else None, capture=observer.capture if case != 'F11' else None)


def timezone(binary, recorder):
    subs = copy.deepcopy(SUBS[:1])
    for tz in ('UTC', 'Asia/Shanghai'):
        with Pair(binary, recorder, tz=tz, options={'shared': {'subscriptions': subs}}) as pair:
            install(pair, subs, [CACHE_NODES])
            observer = MergedMetadata(pair, [(subs[0]['name'], 2)])
            pair.request('R222-B90', ROUTE, expect=200, writes=True,
                         check=observer.reference, capture=observer.capture, variant=tz)


GROUPS = {'merged-provider-data': basic, 'merged-provider-remote': remote, 'merged-provider-timezone': timezone}
