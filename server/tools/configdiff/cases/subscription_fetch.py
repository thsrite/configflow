"""Manual fetch uses real loopback HTTP and observes cache/health/task writes."""
import copy
import json

from compare import database
from harness import Pair, DesignError, json_body
from sources import Source
from subscriptions import configure, Exchange, cached, validate_ids, capture_ids, blocked_error_projection, capture_notice
from cases.subscriptions import SUB, put_file, require
from cases.helpers import deny_writes

FEED = '''proxies:
  - {name: 甲🧪, type: socks5, server: 192.0.2.1, port: 1080}
  - {name: 乙, type: socks5, server: 192.0.2.2, port: 1081}
'''.encode()


def pair_options(url, **sub_updates):
    return {'shared': {'subscriptions': [{**SUB, 'url': url, **sub_updates}]}}


def direct(binary, recorder):
    vectors = [('S01', True, {}, {}, 2), ('S02', False, {}, {}, 2),
               ('S04', True, {}, {'X-Powered-By': 'Sub-Store synthetic'}, 2),
               ('B02', True, {'regex_filter': 'no-node-matches'}, {}, 0),
               ('B03', True, {}, {}, 2)]
    for case, preview, updates, headers, count in vectors:
        with Source(FEED) as source:
            source.headers.update(headers)
            source.headers['Subscription-Userinfo'] = 'upload=1; download=2; total=100; expire=2000000000'
            with Pair(binary, recorder, options=pair_options(source.url + '/feed', **updates)) as pair:
                configure(pair, source.url + '/feed')
                before = database(pair.dirs[0])
                exchange = Exchange(pair, source)
                def check(reply):
                    data = reply.json()
                    require(data.get('count') == count and data.get('cached_count') == count and data.get('total_count') == 2, 'filtered counts after HTTP fetch')
                    require(data.get('from_cache') is False and data.get('fetch_error') is None, 'new download rather than stale fallback')
                    cache = cached(pair.dirs[0])
                    require(cache is not None and len(cache['nodes']) == 2, 'unfiltered raw cache persists')
                    validate_ids(pair.dirs[0])
                    require(cache['metadata']['url'] == source.url + '/feed', 'cache metadata original source')
                    if preview:
                        require(database(pair.dirs[0]) == before, 'preview does not import nodes into SQLite')
                    else:
                        rows = database(pair.dirs[0])['fields']['shared//nodes']['value']
                        require(len(rows) == 2 and data.get('added') == 2, 'non-preview imported nodes')
                pair.request('R218-' + case, 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                             body=json_body({'preview': preview}), expect=200, writes=True, check=check,
                             capture=capture_ids(pair), interaction=exchange)
    with Source(FEED) as source:
        options = pair_options(source.url + '/feed')
        options['shared']['nodes'] = [{'id': 'existing-node', 'name': '甲🧪', 'type': 'socks5', 'server': '192.0.2.99', 'port': 1099}]
        with Pair(binary, recorder, options=options) as pair:
            configure(pair, source.url + '/feed')
            def check(reply):
                require(reply.json().get('added') == 1, 'name-based import deduplication')
                nodes = database(pair.dirs[0])['fields']['shared//nodes']['value']
                require(nodes[0]['id'] == 'existing-node' and nodes[0]['server'] == '192.0.2.99' and len(nodes) == 2, 'existing same-name definition remains unchanged')
                validate_ids(pair.dirs[0])
            pair.request('R218-B01', 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                         body='{}', expect=200, writes=True, check=check, capture=capture_ids(pair), interaction=Exchange(pair, source))


def failures(binary, recorder):
    for case, cache_kind, preview, status in [('S03', 'normal', True, 200), ('F09', None, True, 500),
                                              ('F12', 'malformed', False, 500)]:
        with Source(FEED) as source:
            source.status = 403  # Must not retry a blocked upstream through Sub-Store.
            options = pair_options(source.url + '/feed')
            if cache_kind == 'malformed':
                options['shared']['nodes'] = [{'id': 'existing-synthetic', 'name': '现有节点',
                    'type': 'socks5', 'server': '192.0.2.9', 'port': 1080}]
            with Pair(binary, recorder, options=options) as pair:
                configure(pair, source.url + '/feed')
                if cache_kind:
                    nodes = [{'id': 'legacy-cached', 'name': '旧缓存', 'type': 'socks5', 'server': '192.0.2.5', 'port': 1080}] if cache_kind == 'normal' else [1]
                    put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': '2026-01-01T00:00:00Z',
                        'nodes': nodes, 'count': 1, 'metadata': {'url': source.url + '/feed'}})
                    # Existing cache timestamps are fixed input, never time-masked.
                    for field in ('updated_at', 'metadata.fetched_at'):
                        pair.policy.times.discard('files.shared/subscribes/sub-a.json.content.' + field)
                    pair.policy.times.discard('body.cached_updated_at')
                    pair.policy.times.discard('files.task-status.json.content.subscription:sub-a.cache_updated_at')
                def check(reply):
                    task = json.loads((pair.dirs[0] / 'task-status.json').read_text())['subscription:sub-a']
                    require(task.get('last_status') == 'failed' and task.get('last_error') == '上游返回 HTTP 403', 'failed task receipt with HTTP reason')
                    if case == 'S03':
                        require(reply.json().get('from_cache') is True and reply.json().get('count') == 1, 'stale node cache fallback')
                pair.request('R218-' + case, 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                             body=json_body({'preview': preview}), expect=status, writes=True, check=check,
                             interaction=Exchange(pair, source), projection=blocked_error_projection, capture=capture_notice(pair))
    with Source(FEED) as source:
        with Pair(binary, recorder, options=pair_options(source.url + '/feed')) as pair:
            for case, path, body, status in [('F08', '/api/subscriptions/missing/fetch', '{}', 404),
                                            ('F10', '/api/subscriptions/sub-a/fetch', '{bad', 400),
                                            ('F11', '/api/subscriptions/sub-a/fetch', '[1]', 500)]:
                pair.request('R218-' + case, 'POST /api/subscriptions/<sub_id>/fetch', path, body=body,
                             expect=status, writes=True, interaction=Exchange(pair, source, expected=0))
    with Source(FEED) as source:
        with Pair(binary, recorder, options=pair_options(source.url + '/feed')) as pair:
            configure(pair, source.url + '/feed')
            with deny_writes(pair):
                pair.request('R218-F13', 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                             body='{}', expect=500, writes=True, capture=capture_ids(pair), interaction=Exchange(pair, source))


def conversions(binary, recorder):
    from substore import SubStore, Exchange as StoreExchange
    for case, scenario in [('S05', 'convert-downloaded-body'), ('S06', 'fragment-direct-fallback')]:
        upstream = b'ss://synthetic-opaque-link' if case == 'S05' else FEED
        with Source(upstream) as source, SubStore(FEED, download_status=200 if case == 'S05' else 502) as store:
            url = source.url + '/feed' + ('#synthetic-options' if case == 'S06' else '')
            options = pair_options(url)
            options['environment'] = {'SUB_STORE_URL': store.url}
            with Pair(binary, recorder, options=options) as pair:
                configure(pair, url)
                exchange = StoreExchange(source, store, url, upstream.decode() if case == 'S05' else None,
                                         ['source', 'POST', 'GET', 'DELETE'] if case == 'S05' else ['POST', 'GET', 'DELETE', 'source'])
                def check(reply):
                    require(reply.json().get('count') == 2 and reply.json().get('from_cache') is False, 'Sub-Store/fallback returns materialized nodes')
                    validate_ids(pair.dirs[0])
                pair.request('R218-' + case, 'POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch',
                             body=json_body({'preview': True}), expect=200, writes=True, variant=scenario,
                             check=check, capture=capture_ids(pair), interaction=exchange)


GROUPS = {'subscription-fetch-direct': direct, 'subscription-fetch-failures': failures}

GROUPS['subscription-fetch-substore'] = conversions
