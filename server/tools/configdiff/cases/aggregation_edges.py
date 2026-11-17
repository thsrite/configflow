"""Out-of-order real source completion retains declared aggregation order."""
import json
import re
import threading

import yaml

from compare import database, Difference
from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange, configure, capture_notice
from cases.aggregations import AGG
from cases.subscriptions import SUB, require


def order(binary, recorder):
    with Source() as source:
        for name in ('slow', 'fast'):
            source.responses['/' + name] = (200, yaml.safe_dump({'proxies': [
                {'name': name + '-node', 'type': 'socks5', 'server': '192.0.2.10', 'port': 1080}]}).encode(), {})
        fast_sent = threading.Event()
        timed_out, sent = [], []
        def before(path):
            if path == '/slow' and not fast_sent.wait(5):
                timed_out.append('first source did not run concurrently with second source')
        def after(path):
            sent.append(path)
            if path == '/fast':
                fast_sent.set()
        source.before_reply, source.after_reply = before, after
        subs = [{**SUB, 'id': name, 'name': name, 'url': source.url + '/' + name} for name in ('slow', 'fast')]
        opts = {'shared': {'subscriptions': subs, 'subscription_aggregations': [
            {**AGG, 'subscriptions': ['slow', 'fast'], 'nodes': []}]}}
        with Pair(binary, recorder, options=opts) as pair:
            for sub in subs:
                pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/' + sub['id'],
                    body=json_body({**sub, 'interval': 60}), expect=200, writes=True,
                    variant='aggregation-order/' + sub['id'] + '/enable-first-fetch')
                configure(pair, sub['url'])
                for field in ('updated_at', 'metadata.fetched_at'):
                    pair.policy.times.add('files.shared/subscribes/' + sub['id'] + '.json.content.' + field)
            exchange = Exchange(pair, source, expected=2, unordered=True)
            old_before = exchange.before
            def reset(index):
                old_before(index)
                fast_sent.clear()
                sent.clear()
                timed_out.clear()
            exchange.before = reset
            before_db = database(pair.dirs[0])
            def check(reply):
                data = reply.json()
                require(not timed_out and sent == ['/fast', '/slow'], 'actual second source body finishes sending before the first')
                require([node['name'] for node in data['nodes']] == ['slow-node', 'fast-node'] and data['count'] == 2,
                        'preview retains configured subscription ordering despite reversed network completion')
                artifact = yaml.safe_load((pair.dirs[0] / 'profiles/default/providers/agg-a.yaml').read_text())
                require([node['name'] for node in artifact['proxies']] == ['slow-node', 'fast-node'],
                        'persisted provider uses the same stable source order as preview')
                require(database(pair.dirs[0]) == before_db, 'preview/provider publication does not edit any SQL resource or revision')
                for sub in subs:
                    cache = json.loads((pair.dirs[0] / ('shared/subscribes/' + sub['id'] + '.json')).read_text())
                    require(len(cache['nodes']) == 1 and re.fullmatch('node_[0-9a-f]{8}', cache['nodes'][0]['id']),
                            'actual per-source published cache contains one well-formed generated identity')
            def capture(replies, policy):
                out = capture_notice(pair)(replies, policy)
                for sub in subs:
                    ids = []
                    for directory in pair.dirs:
                        try:
                            cache = json.loads((directory / ('shared/subscribes/' + sub['id'] + '.json')).read_text())
                            ids.append(cache['nodes'][0]['id'])
                        except (OSError, ValueError, KeyError, IndexError, TypeError):
                            ids.append(None)
                    if not all(isinstance(value, str) and re.fullmatch('node_[0-9a-f]{8}', value) for value in ids):
                        out.append(Difference('aggregation.source.' + sub['id'] + '.generated-id', 'invalid-cache-identity', ids[0], ids[1]))
                    else:
                        policy.bind(*ids, 'node_[0-9a-f]{8}', 'aggregation-source-node-' + sub['id'])
                return out
            pair.request('R007-B01', 'GET /api/aggregations/<agg_id>/preview', '/api/aggregations/agg-a/preview',
                         expect=200, writes=True, check=check, interaction=exchange,
                         variant='second-real-response-completes-before-first-source', capture=capture)


GROUPS = {'aggregation-out-of-order-source-completion': order}
