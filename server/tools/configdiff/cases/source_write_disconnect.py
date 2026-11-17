"""T05 real aborted create/compose/fetch, compared to a completed control."""
import copy
import http.client
import json
import re
import socket
import threading
import time
from types import SimpleNamespace
from urllib.parse import urlsplit

from compare import database, database_diffs, file_diffs, differences, Difference
from composition import cache_name, capture as composition_capture
from harness import Pair, json_body
from sources import Source
from subscriptions import configure, Exchange, cached, capture_ids, capture_notice
from cases.compose import VALUES
from cases.helpers import profile_policy
from cases.rule_library import DOMAIN
from cases.subscription_fetch import FEED
from cases.subscriptions import SUB, require
from task_lifecycle import configure as configure_task
from providers import Metadata, comments


def control_policy(base):
    """Keep validated windows, but build a new identity map for this pairing."""
    policy = copy.deepcopy(base)
    policy.identities, policy.reverse = {}, {}
    return policy


def new_composition(directory):
    db = database(directory)['fields']
    return (db['shared//rule_library']['value'], db['profile/default/rule_configs']['value'])


def bind_composition(left, right, policy):
    """Bind only complete, uniquely allocated IDs and their exact cache paths."""
    errors = []
    for kind, rows in [('library', (left[0], right[0])), ('rules', (left[1], right[1]))]:
        if any(not isinstance(side, list) or any(not isinstance(row, dict) for row in side) for side in rows):
            errors.append(Difference('cancelled.compose.' + kind, 'invalid-collection', rows[0], rows[1]))
            continue
        if len(rows[0]) != len(rows[1]):
            errors.append(Difference('cancelled.compose.' + kind, 'count', len(rows[0]), len(rows[1])))
            continue
        ids = [[row.get('id') for row in side] for side in rows]
        if any(any(not isinstance(value, str) for value in values) or len(values) != len(set(values)) for values in ids):
            errors.append(Difference('cancelled.compose.' + kind, 'duplicate-ID', ids[0], ids[1]))
            continue
        for a, b in zip(*rows):
            prefix = 'lib' if kind == 'library' else 'ruleset' if a.get('itemType') == 'ruleset' else 'rule'
            pattern = prefix + r'_[0-9a-f]{12}'
            if any(not isinstance(row.get('id'), str) or not re.fullmatch(pattern, row['id']) for row in (a, b)):
                errors.append(Difference('cancelled.compose.' + kind, 'invalid-ID', a.get('id'), b.get('id')))
                continue
            policy.bind(a['id'], b['id'], pattern, 'aborted-composition-' + kind)
            if kind == 'library':
                policy.bind(cache_name(a), cache_name(b), r'shared/rules/by-source/[0-9a-f]{64}\.list', 'aborted-composition-cache')
    return errors


def effects(control, aborted, kind, policy):
    errors = []
    view = SimpleNamespace(dirs=[control, aborted])
    if kind == 'compose':
        errors += bind_composition(new_composition(control), new_composition(aborted), policy)
    if kind in ('fetch', 'provider'):
        errors += capture_ids(view)([], policy) + capture_notice(view)([], policy)
    if kind == 'provider':
        path = 'profiles/default/providers/served/subscription-sub-a.yaml'
        try:
            errors += differences(comments((control / path).read_bytes(), policy), comments((aborted / path).read_bytes(), policy),
                'cancelled.provider-complete-comments', policy)
        except (OSError, ValueError) as error:
            errors.append(Difference('cancelled.provider-complete-comments', 'missing-or-invalid', 'valid completed file', type(error).__name__))
    if kind == 'generate':
        errors += capture_notice(view, 'rule_source:cancelled-source-created')([], policy)
    return errors + database_diffs(control, aborted, policy) + file_diffs(control, aborted, policy)


def finished(directory, kind, body):
    libraries, rules = new_composition(directory)
    if kind in ('fetch', 'provider'):
        value = cached(directory)
        if kind == 'provider':
            return bool(value and len(value.get('nodes', [])) == 2
                and (directory / 'profiles/default/providers/served/subscription-sub-a.yaml').is_file())
        task = json.loads((directory / 'task-status.json').read_text()).get('subscription:sub-a', {})
        return bool(value and len(value.get('nodes', [])) == 2
            and task.get('last_status') == 'success' and task.get('run_count') == 1)
    selected = [row for row in libraries if row.get('name') == '取消写入对照🧪']
    return len(selected) == 1 and (directory / cache_name(selected[0])).read_bytes() == body and (kind != 'compose' or len(rules) == 1)


def scenario(binary, recorder, kind):
    payload = FEED if kind in ('fetch', 'provider') else b'+.cancelled-source.example\n'
    with Source(payload) as source:
        url = source.url + '/held-source'
        library = {**DOMAIN, 'id': 'cancelled-source-created', 'name': '取消写入对照🧪', 'source_type': 'url', 'url': url}
        library.pop('content')
        route, path, body = {'create': ('POST /api/rule-library', '/api/rule-library', library),
            'compose': ('POST /api/rules/compose', '/api/rules/compose', {**VALUES, 'source': {'kind': 'new_library', 'library': library}}),
            'fetch': ('POST /api/subscriptions/<sub_id>/fetch', '/api/subscriptions/sub-a/fetch', {'preview': False}),
            'provider': ('GET /api/subscriptions/<sub_id>/proxies', '/api/subscriptions/sub-a/proxies', None),
            'generate': ('POST /api/generate/mosdns', '/api/generate/mosdns', {})}[kind]
        options = {'shared': {'subscriptions': [{**SUB, 'url': url}]}} if kind in ('fetch', 'provider') else {}
        if kind == 'generate':
            options = {'shared': {'rule_library': [library]}, 'profile': {'rule_configs': [
                {'id': 'cancelled-generated-rule', 'itemType': 'ruleset', 'library_rule_id': library['id'], 'policy': 'DIRECT', 'enabled': True}],
                'mosdns': {'direct_rulesets': ['cancelled-generated-rule']}}}
        with Pair(binary, recorder, options=options) as control:
            control.policy = profile_policy(control)
            if kind in ('fetch', 'provider'): configure(control, url)
            if kind == 'generate': configure_task(control, 'rule_source:cancelled-source-created')
            if kind == 'provider':
                control.request('T05', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                    body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                    variant=kind + '/control-enable-real-first-download')
            metadata = Metadata(control) if kind == 'provider' else None
            exchange = Exchange(control, source)
            def capture(replies, policy):
                if kind == 'compose': return composition_capture(control)(replies, policy)
                if kind == 'fetch': return capture_ids(control)(replies, policy) + capture_notice(control)(replies, policy)
                if kind == 'provider': return metadata.capture(replies, policy) + capture_notice(control)(replies, policy)
                if kind == 'generate': return capture_notice(control, 'rule_source:cancelled-source-created')(replies, policy)
                return []
            control.request('T05', route, path, body=json_body(body) if body is not None else None, expect=200, writes=True,
                interaction=exchange, capture=capture, check=metadata.reference if metadata else None,
                filename='mosdns-config.zip' if kind == 'generate' else '', variant=kind + '/completed-control-from-identical-seed')
            require(finished(control.dirs[0], kind, payload), 'ordinary completed source control commits its complete result')
            with Pair(binary, recorder, options=options, seed_from=control.root / 'seed') as aborted:
                aborted.policy = profile_policy(aborted)
                aborted.policy.start = control.policy.start
                if kind in ('fetch', 'provider'): configure(aborted, url)
                if kind == 'generate': configure_task(aborted, 'rule_source:cancelled-source-created')
                if kind == 'provider':
                    aborted.request('T05', 'PUT /api/subscriptions/<sub_id>', '/api/subscriptions/sub-a',
                        body=json_body({**SUB, 'url': url, 'interval': 60}), expect=200, writes=True,
                        variant=kind + '/aborted-enable-real-first-download')
                available = aborted.route_available(route)
                proofs, local_errors = [], []
                for index in range(2 if available else 1):
                    source.requests.clear()
                    entered, release = threading.Event(), threading.Event()
                    timeouts = []
                    def gate(_path):
                        entered.set()
                        if not release.wait(15): timeouts.append('source gate timed out')
                    source.before_reply = gate
                    endpoint = urlsplit(aborted.bases[index])
                    client = http.client.HTTPConnection(endpoint.hostname, endpoint.port, timeout=5)
                    client.connect()
                    transport = client.sock
                    try:
                        client.request(route.split(' ', 1)[0], path, body=json_body(body).encode() if body is not None else None, headers={
                            'Content-Type': 'application/json', 'Authorization': 'Bearer ' + aborted.tokens[index]})
                        reached = entered.wait(10)
                        transport.shutdown(socket.SHUT_RDWR)
                    finally:
                        client.close()
                        release.set()
                    deadline, done = time.monotonic() + 15, False
                    while time.monotonic() < deadline:
                        try: done = finished(aborted.dirs[index], kind, payload)
                        except (OSError, ValueError, KeyError): pass
                        if done:
                            probe_policy = control_policy(aborted.policy)
                            probe_policy.end = time.time()
                            if not effects(control.dirs[index], aborted.dirs[index], kind, probe_policy): break
                        time.sleep(.03)
                    source.before_reply = lambda path: None
                    policy = control_policy(aborted.policy)
                    policy.end = time.time()
                    errors = effects(control.dirs[index], aborted.dirs[index], kind, policy)
                    proof = {'source_gate_reached': reached, 'gate_errors': timeouts,
                        'socket_closed_before_response': transport.fileno() == -1, 'complete_result_committed': done,
                        'source_requests': copy.deepcopy(source.requests)}
                    errors += differences(exchange.transcripts[index], proof['source_requests'], 'cancelled.same-wire-as-control')
                    if index == 0:
                        require(reached and not timeouts and proof['socket_closed_before_response'] and done and not errors,
                            'actual aborted source request matches the completed control in every row/file/receipt: '
                            + repr([(item.path, item.kind) for item in errors[:8]]) + '; proof=' + repr(proof))
                    local_errors.append(errors)
                    proofs.append(proof)
                aborted.policy.end = time.time()
                diffs = []
                if available:
                    diffs += local_errors[1] + differences(*proofs, 'cancelled.source-write-proof', aborted.policy)
                    if kind == 'compose': diffs += bind_composition(new_composition(aborted.dirs[0]), new_composition(aborted.dirs[1]), aborted.policy)
                    if kind in ('fetch', 'provider'): diffs += capture_ids(aborted)([], aborted.policy) + capture_notice(aborted)([], aborted.policy)
                    if kind == 'provider':
                        served = 'profiles/default/providers/served/subscription-sub-a.yaml'
                        diffs += differences(comments((aborted.dirs[0] / served).read_bytes(), aborted.policy),
                            comments((aborted.dirs[1] / served).read_bytes(), aborted.policy), 'cancelled.provider-cross-comments', aborted.policy)
                    if kind == 'generate': diffs += capture_notice(aborted, 'rule_source:cancelled-source-created')([], aborted.policy)
                    diffs += database_diffs(*aborted.dirs, aborted.policy) + file_diffs(*aborted.dirs, aborted.policy)
                recorder.record('T05', kind + '/real-socket-abort-at-source-gate-matches-completed-control', route, 'T05',
                    'go-unimplemented' if not available else 'different' if diffs else 'compared', diffs,
                    python_status=None, go_status=None, original_response='unobserved: socket shutdown before source response',
                    identical_Python_seed=True, source_control_status=200)


GROUPS = {'source-write-real-browser-disconnect-' + kind: (lambda binary, recorder, selected=kind: scenario(binary, recorder, selected))
          for kind in ('create', 'compose', 'fetch', 'provider', 'generate')}
