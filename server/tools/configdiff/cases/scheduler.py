"""T27 real production tick with explicit due/fresh/backoff/disabled inputs."""
from collections import Counter
from datetime import datetime, timezone, timedelta
import hashlib
import json
import time
import re

from compare import database_diffs, file_diffs, differences
from harness import Pair, DesignError, json_body
from sources import Source


def tick(binary, recorder):
    with Source() as left, Source() as right:
        with Pair(binary, recorder) as pair:
            policy = pair.policy
            # Retained .bak is the immediately preceding commit. Fix completion
            # order at the source boundary so it is the same snapshot on both
            # sides, rather than masking a nondeterministic backup wholesale.
            previous = {'/retry-due': 'due', '/retry-capped': 'retry-due', '/cache-url-mismatch': 'retry-capped'}
            def gate(directory, path):
                key = previous.get(path)
                if not key:
                    return
                deadline = time.monotonic() + 8
                while time.monotonic() < deadline:
                    try:
                        row = json.loads((directory / 'task-status.json').read_text()).get('subscription:' + key, {})
                        if row.get('last_status') == 'success':
                            return
                    except (OSError, ValueError):
                        pass
                    time.sleep(.01)
                raise RuntimeError('predecessor scheduler job did not finish')
            left.before_reply = lambda path: gate(pair.dirs[0], path)
            right.before_reply = lambda path: gate(pair.dirs[1], path)
            configs = [('due', 60, True), ('fresh', 60, True), ('backoff', 3600, True),
                       ('retry-due', 3600, True), ('retry-capped', 60, True), ('off', 0, True),
                       ('disabled', 60, False), ('cache-fresh', 60, True), ('cache-url-mismatch', 60, True)]
            for name, interval, enabled in configs:
                urls = [fake.url + '/' + name for fake in (left, right)]
                policy.bind(*urls, r'http://127\.0\.0\.1:[0-9]+/[a-z-]+', 'scheduler-source-url')
                pair.request('T27', 'POST /api/subscriptions', body='{}',
                             bodies=[json_body({'id': name, 'name': '合成-' + name, 'url': url,
                                               'interval': interval, 'enabled': enabled}) for url in urls],
                             expect=200, writes=True, variant='create/' + name)
                paths = ['shared/subscription-downloads/' + hashlib.sha256(url.encode()).hexdigest() + '.json' for url in urls]
                policy.bind(*paths, r'shared/subscription-downloads/[a-f0-9]{64}\.json', 'transport-cache-path')
                for path in paths[:1]:
                    policy.times.add('files.' + path + '.content.fetched_at')
                for field in ('updated_at', 'metadata.fetched_at'):
                    policy.times.add('files.shared/subscribes/' + name + '.json.content.' + field)
                for field in ('last_run_at', 'last_finished_at', 'last_success_at', 'cache_updated_at'):
                    policy.times.add('files.task-status.json.content.subscription:' + name + '.' + field)
                policy.durations.add('files.task-status.json.content.subscription:' + name + '.last_duration_ms')
                policy.times.add('files.shared/subscription_health.json.content.' + name + '.history[0].at')
                policy.times.add('files.shared/subscription_health.json.bak.content.' + name + '.history[0].at')
            now = datetime.now(timezone.utc).replace(microsecond=0)
            stamp = lambda offset: (now - timedelta(seconds=offset)).isoformat().replace('+00:00', 'Z')
            statuses = {
                'subscription:fresh': {'last_run_at': stamp(0), 'last_status': 'success'},
                'subscription:backoff': {'last_run_at': stamp(500), 'last_status': 'failed', 'consecutive_failures': 3},
                'subscription:retry-due': {'last_run_at': stamp(1300), 'last_status': 'failed', 'consecutive_failures': 3},
                'subscription:retry-capped': {'last_run_at': stamp(61), 'last_status': 'failed', 'consecutive_failures': 10}}
            # Fixture values are fixed before the tick and are not timestamp-masked.
            policy.times.discard('files.task-status.json.content.subscription:fresh.last_run_at')
            policy.times.discard('files.task-status.json.content.subscription:backoff.last_run_at')
            policy.times.discard('files.shared/subscribes/cache-fresh.json.content.updated_at')
            for field in ('version', 'first_seen_at'):
                policy.times.add('files.notifications.json.content.items.task:subscription:backoff.' + field)
            for index, directory in enumerate(pair.dirs):
                (directory / 'task-status.json').write_text(json.dumps(statuses))
                cache_dir = directory / 'shared/subscribes'
                cache_dir.mkdir(parents=True, exist_ok=True)
                for name in ('cache-fresh', 'cache-url-mismatch'):
                    url = (left if index == 0 else right).url + '/' + name if name == 'cache-fresh' else 'http://other-source.example/feed'
                    (cache_dir / (name + '.json')).write_text(json.dumps({'subscription_id': name,
                        'updated_at': stamp(0), 'count': 1, 'nodes': [{'name': 'cached-node', 'type': 'socks5', 'server': '192.0.2.2', 'port': 1080}],
                        'metadata': {'url': url}}))
            expected = Counter({'/due': 1, '/retry-due': 1, '/retry-capped': 1, '/cache-url-mismatch': 1})
            deadline = time.monotonic() + 40
            while time.monotonic() < deadline:
                if all(Counter(r['path'] for r in f.requests) == expected for f in (left, right)):
                    break
                time.sleep(.1)
            time.sleep(.3)
            policy.end = time.time()
            actual = [Counter(r['path'] for r in f.requests) for f in (left, right)]
            if actual[0] != expected:
                raise DesignError('Python scheduler tick did not select exactly expected due sources: ' + repr(actual[0]))
            if time.time() - pair.policy.start >= 60:
                raise DesignError('scheduler fixture missed its <=60-second contract')
            seen = [set(), set()]
            for path in expected:
                name = path.removeprefix('/')
                nodes = [json.loads((pair.dirs[0] / 'shared/subscribes' / (name + '.json')).read_text())['nodes']]
                try:
                    nodes.append(json.loads((pair.dirs[1] / 'shared/subscribes' / (name + '.json')).read_text())['nodes'])
                except (OSError, ValueError, KeyError):
                    nodes.append([])
                if len(nodes[0]) != 1:
                    raise DesignError('Python scheduler fixture expected one cached node')
                if len(nodes[1]) == 1:
                    identities = [row[0].get('id') for row in nodes]
                    if not all(isinstance(v, str) and re.fullmatch('node_[a-f0-9]{8}', v) for v in identities):
                        # Invalid Go identity remains an ordinary file difference.
                        if not isinstance(identities[0], str) or not re.fullmatch('node_[a-f0-9]{8}', identities[0]):
                            raise DesignError('Python cached node id format invalid')
                    elif all(v not in s for v, s in zip(identities, seen)):
                        policy.bind(*identities, r'node_[a-f0-9]{8}', 'cached-node-id')
                        for v, s in zip(identities, seen):
                            s.add(v)
            # Ordering across different jobs is allowed by ThreadPoolExecutor;
            # within each source the ordered request transcript remains exact.
            def grouped(fake):
                return {path: [r for r in fake.requests if r['path'] == path] for path in sorted({r['path'] for r in fake.requests})}
            diffs = differences(grouped(left), grouped(right), path='scheduler.requests', policy=policy)
            diffs += database_diffs(*pair.dirs, policy) + file_diffs(*pair.dirs, policy)
            recorder.record('T27', 'real-tick/due-and-backoff', 'startup', 'T27',
                            'different' if diffs else 'compared', diffs)


GROUPS = {'scheduler-tick': tick}
