"""T02 independent profile writers with conflicting URL/query/header selectors."""
import copy
import re
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from urllib.parse import unquote, urlsplit

import yaml

from compare import database, files, differences, Difference, response_diffs, timestamp
from harness import Pair, json_body, send
from sources import Source
from cases.helpers import profile_policy
from cases.pull_routes import SourceStable
from cases.rule_library import DOMAIN
from cases.rules import RULE
from cases.subscriptions import require


def canonical_profile_writes(before, after, changes, policy):
    """Accept only the two valid serial orders of independent profile commits."""
    expected, canonical = copy.deepcopy(before), copy.deepcopy(after)
    errors, observed_generations = [], []
    if set(before) != set(after) or set(before['fields']) != set(after.get('fields', {})) or set(before['meta']) != set(after.get('meta', {})):
        return canonical, [Difference('profile-concurrency.exact-SQL', 'changed-SQL-shape', before, after)]
    generations = set(range(before['meta']['generation'] + 1, before['meta']['generation'] + len(changes) + 1))
    for pid, values in changes.items():
        prefix = 'profile/' + pid + '/'
        revision = after['fields'][prefix + '_revision']['revision']
        observed_generations.append(revision)
        if type(revision) is not int or revision not in generations:
            errors.append(Difference(prefix, 'invalid-transaction-generation', sorted(generations), revision))
        for field, value in {**values, '_revision': before['fields'][prefix + '_revision']['value'] + 1}.items():
            expected['fields'][prefix + field]['value'] = copy.deepcopy(value)
            expected['fields'][prefix + field]['revision'] = revision
            canonical['fields'][prefix + field]['revision'] = '<transaction-for-' + pid + '>'
        updated = after['fields'][prefix + 'updated_at']['value']
        expected['fields'][prefix + 'updated_at'].update(value=updated, revision=revision)
        canonical['fields'][prefix + 'updated_at']['revision'] = '<transaction-for-' + pid + '>'
        if timestamp(updated, policy):
            canonical['fields'][prefix + 'updated_at']['value'] = '<validated-live-update>'
        else:
            errors.append(Difference(prefix + 'updated_at', 'not-current-request-time', 'request window', updated))
    if any(type(value) is not int for value in observed_generations) or sorted(observed_generations) != sorted(generations):
        errors.append(Difference('meta.generation', 'not-one-commit-per-profile', sorted(generations), observed_generations))
    expected['meta']['generation'] += len(changes)
    commit = after['meta']['commit_id']
    if isinstance(commit, str) and re.fullmatch('[0-9a-f]{32}', commit) and commit != before['meta']['commit_id']:
        canonical['meta']['commit_id'] = '<validated-new-commit>'
    else:
        errors.append(Difference('meta.commit_id', 'not-a-fresh-commit', 'new hex identity', commit))
    expected['meta']['commit_id'] = commit
    errors += differences(expected, after, 'profile-concurrency.exact-SQL')
    return canonical, errors


def parallel(pair, recorder, phase, requests, changes):
    available = all(pair.route_available(item['route']) for item in requests) and not pair.unimplemented_mutations
    observations, replies = [], []
    for index, base in enumerate(pair.bases):
        if index and not available:
            break
        before_db, before_files = database(pair.dirs[index]), files(pair.dirs[index])
        barrier = threading.Barrier(2)
        def request(item):
            barrier.wait(timeout=10)
            return send(base, 'POST', item['path'], json_body(item['body']),
                {'Authorization': 'Bearer ' + pair.tokens[index], 'Content-Type': 'application/json',
                 'X-ConfigFlow-Profile': item['header']}, timeout=20)
        with ThreadPoolExecutor(max_workers=2) as pool:
            batch = list(pool.map(request, requests))
        pair.policy.end = time.time()
        canonical, violations = canonical_profile_writes(before_db, database(pair.dirs[index]), changes, pair.policy)
        stored = files(pair.dirs[index])
        violations += differences(before_files, stored, 'profile-concurrency.immutable-files')
        observations.append({'database': canonical, 'files': stored, 'violations': [vars(item) for item in violations]})
        replies.append(batch)
        if index == 0:
            require([reply.status for reply in batch] == [200, 200] and not violations,
                'two real profile writers preserve exact changes and all other rows/files: '
                + repr([(item.path, item.kind) for item in violations[:5]]) + '; statuses=' + repr([reply.status for reply in batch]))
    diffs = []
    if available:
        diffs += differences(*observations, 'profile-concurrency', pair.policy)
        for left, right in zip(*replies):
            diffs += response_diffs(left, right, pair.policy, method='POST')
    else:
        pair.unimplemented_mutations.update(item['route'] for item in requests)
    recorder.record('T02', phase, requests[0]['route'], 'T02',
        'go-unimplemented-dependency' if not available else 'different' if diffs else 'compared', diffs,
        python_status=[reply.status for reply in replies[0]],
        go_status=[reply.status for reply in replies[1]] if available else None,
        http_clients_per_backend=2, blocked_by=sorted(pair.unimplemented_mutations) if not available else [])


def isolation(binary, recorder):
    libraries = [{**DOMAIN, 'id': 'lib-' + pid, 'name': '来源-' + pid, 'content': '+.' + pid + '.example\n'}
                 for pid in ('default', 'other')]
    profiles = {pid: {'rule_configs': [{'id': 'rs-' + pid, 'itemType': 'ruleset', 'library_rule_id': 'lib-' + pid,
        'policy': 'DIRECT', 'enabled': True}]} for pid in ('default', 'other')}
    with Source(b'concurrent profile isolation never fetches an upstream') as source, Pair(binary, recorder,
            options={'shared': {'rule_library': libraries}, 'profile': profiles['default'], 'other_profile': profiles['other']}) as pair:
        pair.policy = profile_policy(pair)
        additions = {pid: {**RULE, 'id': 'same-profile-local-ID', 'value': pid + '.example'} for pid in profiles}
        requests = [{'route': 'POST /api/rules', 'path': '/api/rules?profile=default', 'header': 'other', 'body': additions['default']},
            {'route': 'POST /api/profiles/<profile_id>/rules', 'path': '/api/profiles/other/rules?profile=default',
             'header': 'default', 'body': additions['other']}]
        changes = {pid: {'rule_configs': [additions[pid]] + profiles[pid]['rule_configs']} for pid in profiles}
        parallel(pair, recorder, 'real-concurrent-rules-query-versus-explicit-URL', requests, changes)
        documents = {pid: '# 并发配置 ' + pid + '\nipv6: false\n' for pid in profiles}
        requests = [{'route': 'POST /api/custom-config/mihomo', 'path': '/api/custom-config/mihomo?profile=' + pid,
            'header': 'other' if pid == 'default' else 'default', 'body': {'config': documents[pid]}} for pid in profiles]
        current = database(pair.dirs[0])['fields']
        changes = {pid: {'mihomo': {**current['profile/' + pid + '/mihomo']['value'], 'custom_config': documents[pid]}} for pid in profiles}
        parallel(pair, recorder, 'real-concurrent-Mihomo-settings-conflicting-headers', requests, changes)
        for pid in profiles:
            other = 'other' if pid == 'default' else 'default'
            def generated(reply):
                providers = yaml.safe_load(reply.body)['rule-providers']
                paths = [unquote(urlsplit(item['url']).path) for item in providers.values()]
                expected = '/api/profiles/' + pid + '/rules/local/来源-' + pid
                require(paths == [expected],
                    'generated provider URL belongs to its own committed profile despite conflicting selectors')
            pair.request('R023-S01', 'GET /api/config/<profile_id>/mihomo', '/api/config/' + pid + '/mihomo?profile=' + other,
                headers={'X-ConfigFlow-Profile': other}, expect=200, writes=True, interaction=SourceStable(pair, source),
                check=generated, variant='T02/actual-provider-URL-after-concurrent-writers/' + pid)


GROUPS = {'profiles-real-concurrent-rules-settings-and-provider-URLs': isolation}
