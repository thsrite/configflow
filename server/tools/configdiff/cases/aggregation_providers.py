"""Aggregation delivery/preview, real source refresh and scoped artifacts."""
import copy
from urllib.parse import quote

import yaml

from harness import Pair, json_body
from sources import Source
from subscriptions import Exchange, configure, capture_ids, capture_notice
from cases.aggregations import AGG, options
from cases.subscription_fetch import FEED
from cases.subscriptions import CACHE_NODES, STAMP, put_file, require

ROUTES = [('GET /api/aggregations/<agg_id>/provider', '/api/aggregations/agg-a/provider', 'R008', 'default'),
          ('GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider', '/api/profiles/other/aggregations/agg-a/provider', 'R122', 'other')]


def delivery_options(aggregations=None):
    value = options(aggregations)
    for node in value['shared']['nodes']:
        node.update(type='ss', params={'cipher': 'aes-128-gcm', 'password': 'synthetic-provider-password'})
    return value


def install(pair, nodes=CACHE_NODES, url='', legacy=False):
    payload = {'subscription_id': 'sub-a', 'updated_at': STAMP, 'nodes': copy.deepcopy(nodes), 'count': len(nodes)}
    if not legacy:
        payload['metadata'] = {'url': url}
    put_file(pair, 'shared/subscribes/sub-a.json', payload)


def basic(binary, recorder):
    for route, path, rid, profile in ROUTES:
        for case, query in [('S01', ''), ('S02', '?format=surge'), ('S03', '?format=loon'),
                            ('S04', '?format=surge&regex=' + quote('needle', safe='')),
                            ('S04', '?format=loon&exclude_regex=' + quote('needle', safe='')),
                            ('B02', '?format=loon')]:
            with Pair(binary, recorder, options=delivery_options()) as pair:
                nodes = copy.deepcopy(CACHE_NODES)
                for node in nodes:
                    node.update(type='ss', params={'cipher': 'aes-128-gcm', 'password': 'synthetic-provider-password'})
                if case == 'S04':
                    nodes[0]['name'] = 'unique-needle'
                install(pair, nodes)
                def check(reply):
                    require((pair.dirs[0] / ('profiles/' + profile + '/providers/agg-a.yaml')).exists(), 'the aggregation snapshot is committed in the URL-selected profile')
                    if not query:
                        require(len(yaml.safe_load(reply.body)['proxies']) == 3, 'provider contains selected subscription and manual nodes')
                    elif 'exclude_regex' in query:
                        require(len([line for line in reply.body.decode().splitlines() if line and not line.startswith('#')]) == 2, 'Loon exclusion filter operates on emitted candidates')
                    elif 'regex=' in query:
                        require(len([line for line in reply.body.decode().splitlines() if line and not line.startswith('#')]) == 1, 'Surge inclusion filter operates on emitted candidates')
                    else:
                        require(len([line for line in reply.body.decode().splitlines() if line and not line.startswith('#')]) == 3, 'all selected supported SS nodes are really rendered in the text provider')
                pair.request(rid + '-' + case, route, path + query, headers={'X-ConfigFlow-Profile': 'default'},
                             expect=200, writes=True, check=check, variant=query or 'default')
        for case, shape in [('B01', 'empty'), ('B03', 'duplicate-names')]:
            opts = delivery_options([{**AGG, 'subscriptions': [], 'nodes': []}]) if shape == 'empty' else delivery_options()
            if shape == 'duplicate-names':
                opts['shared']['nodes'][0]['name'] = CACHE_NODES[0]['name']
            with Pair(binary, recorder, options=opts) as pair:
                nodes = copy.deepcopy(CACHE_NODES)
                install(pair, nodes)
                pair.request(rid + '-' + case, route, path, expect=200, writes=True,
                             check=lambda r: require(len(yaml.safe_load(r.body)['proxies']) == (0 if shape == 'empty' else 2), 'empty sources or duplicate names produce the exact deduplicated collection'))
        for case, ident, query, expected in [('F09', 'missing', '', 404), ('F12', 'agg-a', '?regex=%5B', 400),
                                            ('F12', 'agg-a', '?exclude_regex=%5B', 400), ('F12', 'agg-a', '?format=surge&regex=%5B', 400),
                                            ('F12', 'agg-a', '?format=loon&exclude_regex=%5B', 400)]:
            with Pair(binary, recorder, options=delivery_options()) as pair:
                install(pair)
                pair.request(rid + '-' + case, route, path.replace('agg-a', ident) + query,
                             expect=expected, writes=True, variant=ident + query)
        with Pair(binary, recorder, options=delivery_options()) as pair:
            pair.request(rid + '-F08', route, path, auth='', expect=401, writes=True)
        with Pair(binary, recorder, options=delivery_options([{**AGG, 'enabled': False}])) as pair:
            pair.request(rid + '-F10', route, path, expect=403, writes=True)
        with Pair(binary, recorder, options=delivery_options()) as pair:
            bad = copy.deepcopy(CACHE_NODES)
            bad[0]['params'] = {'dialer-proxy': 'missing-raw-target'}
            install(pair, bad)
            pair.request(rid + '-F13', route, path, expect=400, writes=True, variant='invalid-cached-dialer')
        with Pair(binary, recorder, options=delivery_options()) as pair:
            install(pair)
            for directory in pair.dirs:
                (directory / ('profiles/' + profile + '/providers/agg-a.yaml')).mkdir(parents=True)
            pair.request(rid + '-F14', route, path, expect=500, writes=True, variant='artifact-path-is-directory')
    for case, shape, expected in [('S01', 'normal', 200), ('B02', 'empty', 200), ('F08', 'missing', 404),
                                   ('F09', 'invalid-cached-dialer', 400), ('F10', 'artifact-path-is-directory', 500)]:
        opts = delivery_options([{**AGG, 'subscriptions': [], 'nodes': []}]) if shape == 'empty' else options()
        with Pair(binary, recorder, options=opts) as pair:
            nodes = copy.deepcopy(CACHE_NODES)
            if shape == 'invalid-cached-dialer':
                nodes[0]['params'] = {'dialer-proxy': 'missing-raw-target'}
            install(pair, nodes)
            if shape == 'artifact-path-is-directory':
                for directory in pair.dirs:
                    (directory / 'profiles/default/providers/agg-a.yaml').mkdir(parents=True)
            pair.request('R007-' + case, 'GET /api/aggregations/<agg_id>/preview',
                         '/api/aggregations/' + ('missing' if shape == 'missing' else 'agg-a') + '/preview',
                         expect=expected, writes=True, variant=shape,
                         check=None if expected != 200 else lambda r: require(r.json()['count'] == (0 if shape == 'empty' else 3)
                             and len(r.json()['nodes']) == r.json()['count'], 'preview reads the actual committed YAML collection'))


def remote(binary, recorder):
    for route, path, rid, profile in ROUTES:
        for shape in ('fresh', 'failed-source-valid-cache', 'HEAD-first-fetch'):
            with Source(FEED) as source:
                opts = delivery_options()
                sub = opts['shared']['subscriptions'][0]
                sub.update(url=source.url + '/feed', interval=0)
                if shape == 'failed-source-valid-cache':
                    source.status = 403
                with Pair(binary, recorder, options=opts) as pair:
                    sub['interval'] = 60
                    pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                                 body=json_body(sub), expect=200, writes=True, variant='aggregation/' + profile + '/' + shape + '/enable-refresh')
                    configure(pair, sub['url'])
                    if shape == 'failed-source-valid-cache':
                        install(pair, url=sub['url'], legacy=True)
                    def check(reply):
                        cached = pair.dirs[0] / 'shared/subscribes/sub-a.json'
                        require(cached.exists() and (pair.dirs[0] / ('profiles/' + profile + '/providers/agg-a.yaml')).exists(),
                                'real refresh/HEAD materializes the source cache and scoped aggregation artifact')
                        if shape == 'HEAD-first-fetch':
                            require(reply.body == b'', 'HEAD executes refresh but sends no body')
                        else:
                            require(len(yaml.safe_load(reply.body)['proxies']) == 3, 'source refresh or last-known-good fallback retains all selected nodes')
                    def capture(replies, policy):
                        return capture_ids(pair)(replies, policy) + capture_notice(pair)(replies, policy)
                    pair.request(rid + '-' + ('S05' if shape == 'failed-source-valid-cache' else 'B91' if shape == 'HEAD-first-fetch' else 'S01'),
                                 route, path, method='HEAD' if shape == 'HEAD-first-fetch' else None, expect=200, writes=True,
                                 interaction=Exchange(pair, source), check=check, capture=capture, variant=shape)
    with Source(FEED) as source:
        source.status = 403
        opts = delivery_options()
        sub = opts['shared']['subscriptions'][0]
        sub.update(url=source.url + '/feed', interval=0)
        with Pair(binary, recorder, options=opts) as pair:
            sub['interval'] = 60
            pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                         body=json_body(sub), expect=200, writes=True, variant='aggregation-preview/enable-refresh')
            install(pair, url=sub['url'], legacy=True)
            configure(pair, sub['url'])
            pair.request('R007-S02', 'GET /api/aggregations/<agg_id>/preview', '/api/aggregations/agg-a/preview',
                         expect=200, writes=True, interaction=Exchange(pair, source), capture=capture_notice(pair),
                         check=lambda r: require(r.json()['count'] == 3, 'preview uses prior valid subscription cache when real transport fails'))


GROUPS = {'aggregation-provider-formats': basic, 'aggregation-provider-refresh': remote}
