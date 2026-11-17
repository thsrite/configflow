"""Concurrent task interval and source URL writes without losing either value."""
from concurrent.futures import ThreadPoolExecutor
import threading
import time

from compare import differences, database, database_diffs, file_diffs, response_diffs
from harness import Pair, json_body, send
from sources import Source
from cases.profile_concurrency import record
from cases.subscriptions import SUB, require
from cases.tasks import deadline_check


def expected_errors(before, after, replies, value):
    out = differences([200, 200], [reply.status for reply in replies], 'task-interval.race.statuses')
    out += differences([value], after['fields']['shared//subscriptions']['value'], 'task-interval.race.exact-source')
    out += differences(before['meta']['generation'] + 2, after['meta']['generation'], 'task-interval.race.two-commits')
    out += differences(before['fields']['shared//_revision']['value'] + 2,
                       after['fields']['shared//_revision']['value'], 'task-interval.race.two-revisions')
    foreign = lambda document: {key: item for key, item in document['fields'].items()
                               if key not in ('shared//subscriptions', 'shared//_revision')}
    out += differences(foreign(before), foreign(after), 'task-interval.race.foreign-rows')
    out += differences(before['sections'], after['sections'], 'task-interval.race.sections')
    try:
        out += differences({'success': True, 'data': value}, replies[1].json(), 'task-interval.race.URL-response')
    except (ValueError, UnicodeError):
        out += differences('<valid JSON>', replies[1].body, 'task-interval.race.URL-response')
    return out


def race(binary, recorder):
    route = 'PUT /api/tasks/interval'
    with Source() as source, Pair(binary, recorder, options={'shared': {'subscriptions': [{**SUB, 'url': source.url + '/old'}]}}) as pair:
        registered = pair.route_available(route)
        available = registered and pair.route_available('PUT /api/subscriptions/<sub_id>')
        changed = {**SUB, 'url': source.url + '/changed', 'interval': 60}
        batches, violations = [], []
        pair.policy.times.add('body.task.next_run_at')
        for index in range(2 if available else 1):
            before, offset = database(pair.dirs[index]), len(source.requests)
            barrier = threading.Barrier(2)
            headers = {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'}
            def client(item):
                path, body = item
                barrier.wait(timeout=5)
                return send(pair.bases[index], 'PUT', path, json_body(body), headers)
            with ThreadPoolExecutor(max_workers=2) as pool:
                replies = list(pool.map(client, [('/api/tasks/interval', {'key': 'subscription:sub-a', 'interval_seconds': 60}),
                                               ('/api/subscriptions/sub-a', changed)]))
            pair.policy.end = time.time()
            errors = expected_errors(before, database(pair.dirs[index]), replies, changed)
            errors += differences([], source.requests[offset:], 'task-interval.race.no-unsolicited-fetch')
            batches.append(replies)
            violations.append(errors)
            if index == 0:
                require(not errors, 'both simultaneous writes commit exactly once and retain the changed URL plus interval: ' + repr(errors))
                deadline_check(pair)(replies[0])
        diffs = []
        if available:
            diffs += violations[1]
            for left, right in zip(*batches):
                diffs += response_diffs(left, right, pair.policy, method='PUT')
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        record(pair, 'R226-B03', route, 'two-real-HTTP-clients-change-interval-and-URL', available, registered, diffs, batches)


GROUPS = {'tasks-interval-concurrent-URL-retention': race}
