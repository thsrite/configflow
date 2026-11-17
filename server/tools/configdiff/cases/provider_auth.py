"""D1–D8 on default/scoped subscription and aggregation delivery endpoints."""
import jwt
import copy
from urllib.parse import quote

from harness import Pair, SECRET, USERNAME
from seed import CURRENT, RETIRED, PULL
from sources import Source
from subscriptions import Exchange
from providers import Metadata
from cases.nodes import NODE
from cases.pull_routes import SourceStable
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file

ROUTES = [
    ('R008', 'GET /api/aggregations/<agg_id>/provider', '/api/aggregations/auth-aggregate/provider'),
    ('R122', 'GET /api/profiles/<profile_id>/aggregations/<agg_id>/provider', '/api/profiles/other/aggregations/auth-aggregate/provider'),
    ('R220', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies'),
    ('R154', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies', '/api/profiles/other/subscriptions/sub-a/proxies'),
]


def environments(binary, recorder):
    expired = jwt.encode({'username': USERNAME, 'exp': 1}, SECRET, algorithm='HS256')
    vectors = [('', '', 1), ('?token=wrong-synthetic', '', 2), ('', expired, 3),
               ('?token=' + quote(CURRENT, safe=''), '', 4), ('?token=' + quote(RETIRED, safe=''), '', 5),
               ('', 'Bearer  malformed', 6), ('?token=' + 'x' * 8193, '', 7)]
    opts = {'shared': {'nodes': [NODE], 'subscriptions': [{**SUB, 'url': ''}],
            'subscription_aggregations': [{'id': 'auth-aggregate', 'name': '认证聚合🧪', 'enabled': True,
                                          'nodes': [NODE['id']], 'subscriptions': []}]}}
    for mode in ('login', 'token', 'open'):
        for rid, route, path in ROUTES:
            with Source(b'auth delivery must not download') as source, Pair(binary, recorder, mode=mode, options=opts) as pair:
                metadata = Metadata(pair, profile='other' if rid == 'R154' else 'default') if rid in ('R154', 'R220') else None
                put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                    'nodes': CACHE_NODES, 'count': len(CACHE_NODES), 'metadata': {'url': ''}})
                for query, auth, number in vectors:
                    status = 200 if mode == 'open' and number not in (4, 5) else 401
                    pair.request(f'{rid}-F{number:02}', route, path + query, auth=auth, expect=status, writes=True,
                        policy=copy.deepcopy(pair.policy) if metadata and status == 200 else None,
                        capture=metadata.capture if metadata and status == 200 else None,
                        check=metadata.reference if metadata and status == 200 else None,
                        interaction=SourceStable(pair, source) if status == 401 else Exchange(pair, source, expected=0),
                        variant=mode + '/provider-D' + str(number))
                for token in (CURRENT, RETIRED):
                    pair.request('D8', route, path + '?token=' + quote(token, safe=''), auth='admin' if mode == 'login' else '',
                        expect=401, writes=True, interaction=SourceStable(pair, source),
                        headers={'X-ConfigFlow-Internal': 'synthetic-spoof'},
                        variant=mode + '/provider-reserved-token/' + rid + '/' + ('current' if token == CURRENT else 'retired'))
                pair.request(rid + '-F08', route, path + '?token=wrong-synthetic', auth='',
                    expect=200 if mode == 'open' else 401, writes=True,
                    policy=copy.deepcopy(pair.policy) if metadata and mode == 'open' else None,
                    capture=metadata.capture if metadata and mode == 'open' else None,
                    check=metadata.reference if metadata and mode == 'open' else None,
                    interaction=Exchange(pair, source, expected=0) if mode == 'open' else SourceStable(pair, source),
                    variant=mode + '/named-provider-authorization-guard')
                pair.request(rid + '-S01', route, path + ('?token=' + PULL if mode == 'token' else ''),
                    auth='admin' if mode == 'login' else '', expect=200, writes=True,
                    policy=copy.deepcopy(pair.policy) if metadata else None,
                    capture=metadata.capture if metadata else None, check=metadata.reference if metadata else None,
                    interaction=Exchange(pair, source, expected=0), variant=mode + '/authorized-provider')


GROUPS = {'provider-auth-environments': environments}
