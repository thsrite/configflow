"""Default/scoped subscription provider formats and real HEAD side effects."""
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import quote
import copy

from harness import Pair, json_body
from sources import Source
from providers import Metadata
from subscriptions import Exchange, configure, validate_ids
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file, require
from cases.subscription_fetch import FEED, pair_options

ROUTES = [('GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies', 'R220'),
          ('GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies', '/api/profiles/default/subscriptions/sub-a/proxies', 'R154')]


def cache(pair, url):
    put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP, 'count': 2,
              'nodes': CACHE_NODES, 'metadata': {'url': url}})


def formats(binary, recorder):
    vectors = [(route, path, rid, suffix, query) for route, path, rid in ROUTES for suffix, query in [
        ('S01', ''), ('S02', '?format=surge'), ('S03', '?format=loon'),
        ('S05', '?format=surge&regex=' + quote('甲', safe='')), ('S05', '?format=loon&exclude_regex=' + quote('甲', safe='')),
        ('B01', '?format=surge&regex=no-node'), ('B02', '?format=loon'), ('B03', '?format=unknown')]]
    def execute(vector):
        route, path, rid, suffix, query = vector
        with Source(FEED) as source:
            url = source.url + '/feed'
            with Pair(binary, recorder, options=pair_options(url)) as pair:
                cache(pair, url)
                yaml = query in ('', '?format=unknown')
                metadata = Metadata(pair, yaml=yaml, served=not query)
                pair.request(rid + '-' + suffix, route, path + query, expect=200, writes=True,
                             check=metadata.reference, capture=metadata.capture, interaction=Exchange(pair, source, expected=0),
                             variant=query or 'default')
    with ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(execute, vectors))


def fresh_and_head(binary, recorder):
    for route, path, rid in ROUTES:
        for method in ('GET', 'HEAD'):
            with Source(FEED) as source:
                url = source.url + '/feed'
                with Pair(binary, recorder, options=pair_options(url)) as pair:
                    pair.request('T04', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                                 body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                                 variant=rid + '/' + method + '/enable-first-fetch')
                    configure(pair, url)
                    metadata = Metadata(pair, method=method)
                    def check(reply):
                        metadata.reference(reply)
                        validate_ids(pair.dirs[0])
                    pair.request(rid + ('-B91' if method == 'HEAD' else '-S01'), route, path,
                                 method=method, expect=200, writes=True, check=check, capture=metadata.capture,
                                 interaction=Exchange(pair, source), variant=method + '/first-download')


def no_url_and_validation(binary, recorder):
    for route, path, rid in ROUTES:
        for suffix, kind, status in [('S06', 'cached', 200), ('F15', 'missing', 404), ('F16', 'empty', 404)]:
            with Pair(binary, recorder, options=pair_options('')) as pair:
                if kind != 'missing':
                    cache(pair, '')
                    if kind == 'empty':
                        put_file(pair, 'shared/subscribes/sub-a.json', {'nodes': [], 'count': 0, 'updated_at': STAMP})
                metadata = Metadata(pair)
                pair.request(rid + '-' + suffix, route, path, expect=status, writes=True,
                             check=metadata.reference if status == 200 else None,
                             capture=metadata.capture if status == 200 else None)
        with Source(FEED) as source:
            with Pair(binary, recorder, options=pair_options(source.url + '/feed')) as pair:
                for variant, query in [('format', '?regex=x'), ('regex', '?format=surge&regex=%5B'), ('exclude', '?format=loon&exclude_regex=%5B')]:
                    pair.request(rid + '-F11', route, path + query, expect=400, writes=True,
                                 interaction=Exchange(pair, source, expected=0), variant=variant)
                pair.request(rid + '-F09', route, path.replace('sub-a', 'missing'), expect=404, writes=True,
                             interaction=Exchange(pair, source, expected=0))


def timezone(binary, recorder):
    for route, path, rid in ROUTES:
        for tz in ('UTC', 'Asia/Shanghai'):
            with Pair(binary, recorder, tz=tz, options=pair_options('')) as pair:
                cache(pair, '')
                metadata = Metadata(pair)
                pair.request(rid + '-B90', route, path, expect=200, writes=True,
                             check=metadata.reference, capture=metadata.capture, variant=tz)


def explicit_selector(binary, recorder):
    with Pair(binary, recorder, options=pair_options('')) as pair:
        cache(pair, '')
        metadata = Metadata(pair, profile='default')
        pair.request('R154-B04', ROUTES[1][0], ROUTES[1][1] + '?profile=other',
                     headers={'X-ConfigFlow-Profile': 'other'}, expect=200, writes=True,
                     check=metadata.reference, capture=metadata.capture, variant='URL-over-query-header')


GROUPS = {'subscription-provider-formats': formats, 'subscription-provider-head': fresh_and_head,
          'subscription-provider-validation': no_url_and_validation, 'subscription-provider-timezone': timezone}

GROUPS['subscription-provider-selector'] = explicit_selector
