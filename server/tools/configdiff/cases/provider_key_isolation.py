"""T07 actual concurrent clients vary profile/format/include/exclude keys."""
import copy
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import urlencode

from compare import database, differences, files, database_diffs, file_diffs
from harness import Pair, json_body, send
from sources import Source
from provider_lifecycle import response_batch_diffs
from cases.subscription_fetch import pair_options
from cases.subscriptions import SUB, CACHE_NODES, STAMP, put_file, require


def names(reply):
    return [line.split('=', 1)[0].strip() for line in reply.body.decode().splitlines()
        if '=' in line and not line.startswith('#')]


def batches(pair, source, recorder, phase, current_names):
    vectors = [{'profile': pid, 'format': kind, **filters}
        for pid in ('default', 'other')
        for kind, filters in [('surge', {'regex': '甲'}), ('surge', {'regex': '乙'}),
                             ('surge', {'exclude_regex': '甲'}), ('loon', {'regex': '甲'})]]
    routes = ('GET /api/subscriptions/<sub_id>/proxies', 'GET /api/profiles/<profile_id>/subscriptions/<sub_id>/proxies')
    available = all(pair.route_available(route) for route in routes) and not pair.unimplemented_mutations
    all_replies, proof = [], []
    for index in range(2 if available else 1):
        baseline = database(pair.dirs[index]), files(pair.dirs[index])
        source.requests.clear()
        barrier = threading.Barrier(len(vectors))
        def fetch(item):
            barrier.wait(timeout=10)
            path = '/api/subscriptions/sub-a/proxies' if item['profile'] == 'default' else '/api/profiles/other/subscriptions/sub-a/proxies'
            query = {key: value for key, value in item.items() if key != 'profile'}
            return send(pair.bases[index], 'GET', path + '?' + urlencode(query), headers={
                'Authorization': 'Bearer ' + pair.tokens[index],
                'X-ConfigFlow-Profile': 'other' if item['profile'] == 'default' else 'default'}, timeout=20)
        with ThreadPoolExecutor(max_workers=len(vectors)) as pool:
            replies = list(pool.map(fetch, vectors))
        observed = {'names': [names(reply) for reply in replies], 'statuses': [reply.status for reply in replies],
            'all_SQL_unchanged': database(pair.dirs[index]) == baseline[0],
            'all_files_unchanged': files(pair.dirs[index]) == baseline[1], 'source_requests': copy.deepcopy(source.requests)}
        expected_names = []
        for item in vectors:
            expected_names.append([name for name in current_names
                if (not item.get('regex') or item['regex'] in name)
                and (not item.get('exclude_regex') or item['exclude_regex'] not in name)])
        if index == 0:
            require(observed == {'names': expected_names, 'statuses': [200] * len(vectors),
                'all_SQL_unchanged': True, 'all_files_unchanged': True, 'source_requests': []},
                'concurrent cached providers preserve each profile/format/include/exclude selection and every row/file: ' + repr(observed))
        all_replies.append(replies)
        proof.append(observed)
    pair.policy.end = time.time()
    diffs = []
    if available:
        diffs += response_batch_diffs(*all_replies, pair.policy)
        diffs += differences(*proof, 'provider.concurrent-key-isolation', pair.policy)
        diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
    recorder.record('T07', phase, routes[0], 'T07',
        'go-unimplemented-dependency' if not available else 'different' if diffs else 'compared', diffs,
        python_status=proof[0]['statuses'], go_status=proof[1]['statuses'] if available else None,
        concurrent_clients=len(vectors), vectors=vectors,
        boundary='client barrier proves output isolation; no private flight-map inspection')


def isolation(binary, recorder):
    with Source(b'cached key-isolation requests must not fetch upstream') as source:
        url = source.url + '/unused'
        options = pair_options(url)
        options['environment'] = {'CONFIGFLOW_HTTP_THREADS': '32'}
        with Pair(binary, recorder, options=options) as pair:
            put_file(pair, 'shared/subscribes/sub-a.json', {'subscription_id': 'sub-a', 'updated_at': STAMP,
                'count': 2, 'metadata': {'url': url}, 'nodes': [{**node, 'type': 'ss',
                    'params': {'cipher': 'aes-128-gcm', 'password': 'synthetic-key-isolation-password'}} for node in CACHE_NODES]})
            batches(pair, source, recorder, 'eight-concurrent-profile-format-and-filter-keys', ['甲🧪', '乙'])
            pair.request('T07', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                body=json_body({**SUB, 'url': url, 'regex_filter': '甲'}), expect=200, writes=True,
                variant='actual-public-config-digest-change-between-client-batches')
            batches(pair, source, recorder, 'same-keys-after-real-config-digest-change', ['甲🧪'])


GROUPS = {'provider-real-concurrent-profile-format-filter-and-digest-isolation': isolation}
