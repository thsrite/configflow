"""Competing profile writes through independent simultaneous HTTP clients."""
from concurrent.futures import ThreadPoolExecutor
import threading
import time

from compare import (Difference, differences, database, database_diffs, file_diffs,
                     response_diffs, timestamp)
from harness import Pair, Reply, json_body, send
from sources import Source
from cases.subscriptions import require


def simultaneous(pair, index, method, path, bodies):
    barrier = threading.Barrier(2)
    def request(body):
        barrier.wait(timeout=5)
        return send(pair.bases[index], method, path, json_body(body),
                    {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json'})
    with ThreadPoolExecutor(max_workers=2) as pool:
        futures = [pool.submit(request, body) for body in bodies]
        return [future.result(timeout=15) for future in futures]


def record(pair, case, route, variant, available, registered, diffs, batches):
    if available:
        state = 'different' if diffs else 'compared'
    elif registered:
        state = 'go-unimplemented-dependency'
    elif pair.declared.get(tuple(route.split(' ', 1))) == 'done':
        state = 'different'
        diffs = [Difference('route.registration', 'declared-done-but-unregistered', True, False)]
    else:
        state = 'go-unimplemented'
    pair.recorder.record(case, variant, route, 'B', state, diffs,
        python_statuses=[reply.status for reply in batches[0]],
        go_statuses=[reply.status for reply in batches[1]] if available else None)


def metadata_batch_errors(replies, initial, policy):
    """Accept exactly either legal source serialization, never lost updates."""
    if len(replies) != 2:
        return [Difference('metadata.batch', 'wrong-response-count', 2, len(replies))]
    new_name, new_description = '并发改名🧪', '并发说明🧪'
    expected = {key: initial[key] for key in ('id', 'name', 'description', 'created_at')}
    a, b = {**expected, 'name': new_name}, {**expected, 'description': new_description}
    final = {**expected, 'name': new_name, 'description': new_description}
    bodies, out = [], []
    for index, reply in enumerate(replies):
        out += differences(200, reply.status, f'metadata.response[{index}].status')
        try:
            body = reply.json()
        except (ValueError, UnicodeError):
            out.append(Difference(f'metadata.response[{index}]', 'invalid-json', '<object>', reply.body))
            continue
        if not isinstance(body, dict) or not timestamp(body.get('updated_at'), policy):
            out.append(Difference(f'metadata.response[{index}].updated_at', 'invalid-current-time', '<valid timestamp>', body))
            continue
        bodies.append({key: value for key, value in body.items() if key != 'updated_at'})
    if len(bodies) == 2 and differences([a, final], bodies) and differences([final, b], bodies):
        out.append(Difference('metadata.batch.responses', 'not-a-legal-serialization', [[a, final], [final, b]], bodies))
    return out


def metadata(binary, recorder):
    route = 'PUT /api/profiles/<profile_id>'
    with Pair(binary, recorder) as pair:
        registered = pair.route_available(route)
        available = registered and not pair.unimplemented_mutations
        pair.policy.times.add('db.fields.profile/other/updated_at.value')
        batches, violations = [], []
        for index in range(2 if available else 1):
            before = database(pair.dirs[index])
            initial = {key: before['fields']['profile/other/' + key]['value']
                       for key in ('id', 'name', 'description', 'created_at')}
            replies = simultaneous(pair, index, 'PUT', '/api/profiles/other',
                                   [{'name': '并发改名🧪'}, {'description': '并发说明🧪'}])
            pair.policy.end = time.time()
            after = database(pair.dirs[index])
            own = lambda key: after['fields']['profile/other/' + key]['value']
            errors = metadata_batch_errors(replies, initial, pair.policy)
            errors += differences('并发改名🧪', own('name'), 'metadata.final.name')
            errors += differences('并发说明🧪', own('description'), 'metadata.final.description')
            errors += differences(before['fields']['profile/other/_revision']['value'] + 2,
                                  own('_revision'), 'metadata.final.revision')
            errors += differences(before['meta']['generation'] + 2, after['meta']['generation'], 'metadata.final.generation')
            foreign = lambda db: {key: value for key, value in db['fields'].items() if not key.startswith('profile/other/')}
            errors += differences(foreign(before), foreign(after), 'metadata.foreign-fields')
            batches.append(replies)
            violations.append(errors)
            if index == 0:
                require(not errors, 'both concurrent metadata changes survive with two commits and one of exactly two serial response histories: ' + repr(errors))
        diffs = []
        if available:
            diffs += violations[1]
            # Bodies are checked completely against the two legal source
            # serializations above. Independent races need not pick the same
            # winner; headers/status and the final entire DB/files stay strict.
            for left, right in zip(*batches):
                diffs += response_diffs(Reply(left.status, left.headers, b''), Reply(right.status, right.headers, b''), pair.policy, method='HEAD')
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        record(pair, 'R119-B03', route, 'simultaneous-name-and-description', available, registered, diffs, batches)


def rebind(binary, recorder):
    route = 'POST /api/profiles/<profile_id>/agents/rebind'
    with Source(b'rebinding must never contact an Agent') as source, Pair(binary, recorder,
            options={'agent_updates': {'port': source.server.server_port}}) as pair:
        registered = pair.route_available(route)
        available = registered and not pair.unimplemented_mutations
        pair.policy.times.add('db.fields.system//agents.value[0].updated_at')
        batches, violations = [], []
        for index in range(2 if available else 1):
            before, offset = database(pair.dirs[index]), len(source.requests)
            body = {'target_profile_id': 'other', 'agent_ids': ['agent-synthetic']}
            replies = simultaneous(pair, index, 'POST', '/api/profiles/default/agents/rebind', [body, body])
            replies.sort(key=lambda reply: reply.status)
            after = database(pair.dirs[index])
            errors = differences([200, 409], [reply.status for reply in replies], 'rebind.concurrent-statuses')
            errors += differences('other', after['fields']['system//agents']['value'][0].get('profile_id'), 'rebind.target')
            errors += differences(before['fields']['system//_revision']['value'] + 1,
                                  after['fields']['system//_revision']['value'], 'rebind.single-commit')
            errors += differences(before['meta']['generation'] + 1, after['meta']['generation'], 'rebind.single-generation')
            errors += differences([], source.requests[offset:], 'rebind.unsolicited-Agent-request')
            if replies[1].status == 409:
                errors += differences({'success': False, 'message': 'Agent 绑定已变化，请刷新列表后重新确认'}, replies[1].json(), 'rebind.loser')
            foreign = lambda db: {key: value for key, value in db['fields'].items() if key.startswith(('profile/', 'shared/'))}
            errors += differences(foreign(before), foreign(after), 'rebind.unchanged-profiles-and-shared')
            batches.append(replies)
            violations.append(errors)
            if index == 0:
                require(not errors, 'two simultaneous exact-set rebinds have one winner, one stale-set 409 and one commit: ' + repr(errors))
        pair.policy.end = time.time()
        diffs = []
        if available:
            diffs += violations[1]
            for left, right in zip(*batches):
                diffs += response_diffs(left, right, pair.policy, method='POST')
            diffs += database_diffs(*pair.dirs, pair.policy) + file_diffs(*pair.dirs, pair.policy)
        record(pair, 'R121-B02', route, 'two-simultaneous-confirmed-binding-writes', available, registered, diffs, batches)


GROUPS = {'profile-concurrent-metadata': metadata, 'profile-concurrent-rebind': rebind}
