"""Long provider requests use the production timer and actual loopback workers."""
from harness import Pair, json_body
from sources import Source
from provider_lifecycle import Fallback
from providers import Metadata
from subscriptions import configure, capture_ids, validate_ids, Exchange
from cases.subscription_fetch import FEED, pair_options
from cases.subscriptions import SUB, require

OLD = b'# Synthetic fixed last-good fixture\nproxies:\n  - {name: last-good-only, type: socks5, server: 192.0.2.55, port: 1080}\n'


def fallback(binary, recorder):
    with Source(FEED) as source:
        url = source.url + '/feed'
        with Pair(binary, recorder, options=pair_options(url)) as pair:
            pair.request('T06', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                         body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                         variant='enable-first-delivery')
            for directory in pair.dirs:
                path = directory / 'profiles/default/providers/served/subscription-sub-a.yaml'
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes(OLD)
            configure(pair, url)
            interaction = Fallback(pair, source)
            def check(reply):
                require(reply.headers.get('x-configflow-provider') == 'last-good' and reply.body == OLD,
                        '12-second response is exactly previous served bytes')
                require(len(interaction.observed[0]['requests']) == 1, 'one underlying fetch for fallback/background work')
                validate_ids(pair.dirs[0])
            pair.request('T06', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies',
                         expect=200, writes=True, check=check, capture=capture_ids(pair), interaction=interaction,
                         variant='12-second-last-good-and-background-finish')
            metadata = Metadata(pair)
            pair.request('T06', 'GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies',
                         expect=200, writes=True, check=metadata.reference, capture=metadata.capture,
                         interaction=Exchange(pair, source, expected=0), variant='next-poll-is-fresh')


GROUPS = {'provider-last-good-lifecycle': fallback}


def concurrent_clients(binary, recorder):
    from concurrent.futures import ThreadPoolExecutor
    import copy
    import json
    import threading
    import time
    from compare import differences, database_diffs, file_diffs
    from harness import send, DesignError
    from provider_lifecycle import response_batch_diffs
    with Source(FEED) as source:
        url = source.url + '/feed'
        options = pair_options(url)
        # Production-supported setting; leave all lifecycle hooks enabled and
        # admit all 20 requests while the one real source is held at a barrier.
        options['environment'] = {'CONFIGFLOW_HTTP_THREADS': '32'}
        with Pair(binary, recorder, options=options) as pair:
            pair.request('T06', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                         body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                         variant='20-client-enable-first-delivery')
            configure(pair, url)
            batches, observations = [], []
            for index, base in enumerate(pair.bases):
                entered, release = threading.Event(), threading.Event()
                source.requests.clear()
                def gate(path):
                    entered.set()
                    release.wait(5)
                source.before_reply = gate
                barrier = threading.Barrier(20)
                headers = {'Authorization': 'Bearer ' + pair.tokens[index], 'X-ConfigFlow-Profile': 'default'}
                def client(_):
                    barrier.wait(timeout=10)
                    return send(base, 'GET', '/api/subscriptions/sub-a/proxies', headers=headers)
                with ThreadPoolExecutor(max_workers=20) as pool:
                    pending = [pool.submit(client, slot) for slot in range(20)]
                    if not entered.wait(8):
                        release.set()
                        raise DesignError('provider source did not reach concurrency barrier')
                    time.sleep(1)
                    release.set()
                    batch = [future.result(timeout=15) for future in pending]
                batches.append(batch)
                health_path = pair.dirs[index] / 'shared/subscription_health.json'
                health = json.loads(health_path.read_text()).get('sub-a', {}) if health_path.exists() else {}
                observation = {'source_requests': copy.deepcopy(source.requests),
                               'all_status_200': all(reply.status == 200 for reply in batch),
                               'one_shared_body': len({reply.body for reply in batch}) == 1,
                               'history_entries': len(health.get('history', []))}
                observations.append(observation)
                if index == 0:
                    if not (len(source.requests) == 1 and observation['all_status_200'] and observation['one_shared_body'] and observation['history_entries'] == 1):
                        raise DesignError('Python 20-client fixture did not share one actual provider flight: ' + repr(observation))
                    validate_ids(pair.dirs[0])
            pair.policy.end = time.time()
            metadata = Metadata(pair)
            metadata.reference(batches[0][0])
            replies = [batch[0] for batch in batches]
            diffs = metadata.capture(replies, pair.policy)
            diffs += differences(*observations, 'provider.concurrent', pair.policy)
            diffs += response_batch_diffs(*batches, pair.policy)
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
            recorder.record('T06', '20-clients-one-flight', 'GET /api/subscriptions/<sub_id>/proxies',
                            'T06', 'different' if diffs else 'compared', diffs,
                            python_status=[reply.status for reply in batches[0]],
                            go_status=[reply.status for reply in batches[1]], http_clients_per_backend=20)


GROUPS['provider-concurrent-clients'] = concurrent_clients
