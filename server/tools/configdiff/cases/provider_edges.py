"""Named provider auth, chain refusal, stale cache and filesystem failure cases."""
import copy
from urllib.parse import quote

import yaml

from harness import Pair, json_body
from providers import Metadata
from seed import CURRENT, RETIRED
from sources import Source
from subscriptions import Exchange, configure, capture_notice
from cases.config_backup import message
from cases.groups import options as group_options, CHAIN
from cases.pull_routes import SourceStable
from cases.setup import Stable
from cases.subscription_fetch import FEED, pair_options
from cases.subscription_providers import ROUTES, cache
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file, require


def authorizations(binary, recorder):
    with Source(b'authorization must reject before source access') as source, Pair(binary, recorder) as pair:
        for case, route, path in [('R008-F15', 'GET /api/aggregations/<agg_id>/provider', '/api/aggregations/no-object/provider'),
                ('R220-F18', ROUTES[0][0], ROUTES[0][1]),
                ('R222-F13', 'GET /api/subscriptions/proxies', '/api/subscriptions/proxies')]:
            for name, token in [('current', CURRENT), ('retired', RETIRED)]:
                pair.request(case, route, path + '?token=' + quote(token, safe=''), auth='admin', expect=401,
                             writes=True, interaction=SourceStable(pair, source), variant='reserved-capability-with-valid-JWT/' + name)
        pair.request('R222-F08', 'GET /api/subscriptions/proxies', '/api/subscriptions/proxies?token=wrong-synthetic',
                     auth='', expect=401, writes=True, interaction=SourceStable(pair, source), variant='named-wrong-query-capability')


def stale(binary, recorder):
    for route, path, rid in ROUTES:
        with Source(FEED) as source:
            source.status = 403
            url = source.url + '/feed'
            with Pair(binary, recorder, options=pair_options(url)) as pair:
                pair.request('R217-S01', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                             body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                             variant=rid + '/enable-real-failing-refresh')
                # A proven-current URL cache is intentionally served without
                # another download. A legacy cache has no URL provenance and
                # genuinely attempts the remote source before falling back.
                put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                    'count': len(CACHE_NODES), 'nodes': CACHE_NODES})
                configure(pair, url)
                pair.policy.times.discard('files.shared/subscribes/sub-a.json.content.updated_at')
                observer = Metadata(pair, fetch_error_status=403)
                def check(reply):
                    observer.reference(reply)
                    require(len(yaml.safe_load(reply.body)['proxies']) == len(CACHE_NODES)
                        and b'from local cache' in reply.body and b'fetch error:' in reply.body,
                        'after a real failed upstream request, response contains the complete previous node cache and failure provenance')
                def capture(replies, policy):
                    return observer.capture(replies, policy) + capture_notice(pair)(replies, policy)
                pair.request(rid + '-S04', route, path, expect=200, writes=True, check=check, capture=capture,
                             interaction=Exchange(pair, source), variant='real-403-then-last-valid-node-cache')


def graph_failures(binary, recorder):
    for route, path, rid in ROUTES:
        opts = group_options()
        opts['shared']['subscriptions'][0]['url'] = ''
        opts['profile']['proxy_groups'].append({'id': 'active-chain', 'name': '链式来源', 'type': 'chain', 'chain': CHAIN})
        with Pair(binary, recorder, options=opts) as pair:
            cache(pair, '')
            for case, service, label in [('F12', 'surge', 'Surge'), ('F13', 'loon', 'Loon')]:
                pair.request(rid + '-' + case, route, path + '?format=' + service, expect=400, writes=True,
                             interaction=Stable(pair), check=message(label + ' 暂不支持代理链，请使用 Mihomo'),
                             variant='actual-active-panel-chain-before-client-render')
        with Pair(binary, recorder, options=pair_options('')) as pair:
            bad = copy.deepcopy(CACHE_NODES)
            bad[0]['params'] = {'dialer-proxy': 'synthetic-missing-target'}
            put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                'count': len(bad), 'nodes': bad, 'metadata': {'url': ''}})
            pair.request(rid + '-F14', route, path, expect=400, writes=True, interaction=Stable(pair),
                         variant='invalid-raw-cache-dialer-graph-is-never-fallback-success')
        with Pair(binary, recorder, options=pair_options('')) as pair:
            cache(pair, '')
            for directory in pair.dirs:
                (directory / 'profiles/default/providers/served/subscription-sub-a.yaml').mkdir(parents=True)
            pair.request(rid + '-F17', route, path, expect=500, writes=True, interaction=Stable(pair),
                         variant='last-good-provider-output-path-is-directory')


GROUPS = {'provider-named-reserved-auth': authorizations, 'provider-real-stale-cache': stale,
          'provider-named-graph-and-file-failures': graph_failures}
