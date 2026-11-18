"""Execute concrete recipes; report missing Go routes separately from defects."""
import argparse
import base64
from collections import Counter
from dataclasses import asdict
import json
from pathlib import Path
import re
import sys
import time
from urllib.parse import urlsplit, urlunsplit

import cases
import guards
import workflows
import fixtures
import probes
import probe_workflows
import probe_guards
import probe_schemas
import releases
import clients
import sync
import upstream
import dns
import dns_targets
import dhcp_guards
import ikuai_dhcp_guards
import ikuai_dhcp_writes
import gateway
import disconnect
import routing
import router_lifecycle
import policy_view
import returns
import persistence
import ipv6
import device_policy
import policy_graph
import policy_owned
import policy_side
import policy_invalid
import split
import split_onboarding
import split_state
import forwarding_guards
import fixed_normalization
import fixed_setup
import fixed_sources
from compare import differences, logical_snapshot, response_view, timestamp
from harness import Pair, RemoteError, observed_startup_exit
from harness import observed_request_failure
from harness import decode_response
from inventory import design_json, load
from invariants import configuration_only, profile_write, fields
from seed import router
from fakes import World
from addresses import report_aliases
from tokens import inspect_python_token
from seed import JWT_SECRET
from bugs import BUGS
from audits import evaluate
from identities import RouterIDs
from temporal import ResponseClocks
from probe_clocks import ProbeClocks


def shape(path):
    return re.sub(r'<(?:[^:>]+:)?[^>]+>|\{[^}]+\}', '{}', path)


def auth_off(options):
    return {**options, 'environment': {**options.get('environment', {}), 'ADMIN_USERNAME': '', 'ADMIN_PASSWORD': ''}}


def registered(manifest):
    return {(method, shape(path)) for method, path in
            (line.split('\t', 1) for line in manifest.splitlines())}



def registered_request(method, path, available):
    """Match a concrete auxiliary request against the public route manifest."""
    path = urlsplit(path).path
    for candidate_method, template in available:
        if candidate_method != method:
            continue
        pattern = re.escape(template).replace(r'\{\}', '[^/]+')
        if re.fullmatch(pattern, path):
            return True
    return False

def poll(side, values, expected, seconds, strict=True):
    if values['method'] != 'GET':
        raise ValueError('eventual assertions may only repeat public GETs')
    deadline = time.monotonic() + seconds
    while True:
        response = side.request(**values)
        try:
            ready = all(cases.at(response['content'], path) == value for path, value in expected.items())
        except (KeyError, IndexError, TypeError):
            ready = False
        if response['status'] == 200 and ready:
            return response
        if time.monotonic() >= deadline:
            if strict:
                raise RuntimeError('public steady-state wait expired: ' + repr(response['content']))
            return response  # A Go failure is a compared observation, not a fixture error.
        time.sleep(.1)


def wait_guard(side, path, seconds, strict=True):
    # Retry only the deliberate non-object rejection while L1 is busy. Never
    # retry an accepted write or any other status; all storage remains compared.
    deadline = time.monotonic() + seconds
    while True:
        response = side.request(method='PUT', path=path, document=[])
        if response['status'] != 409 or time.monotonic() >= deadline:
            if strict and response['status'] != 400:
                raise RuntimeError('reference public write guard did not settle to validation refusal')
            return response
        time.sleep(.1)


def wait_age(side, case):
    if not 0 <= case.age_seconds <= 605 or case.age_anchor not in ('started', 'ended'):
        raise ValueError('age contract exceeds the reviewed real-time fixture bounds')
    if case.age_from not in side.request_windows:
        raise RuntimeError(case.ident + ': missing own-side request window: ' + case.age_from)
    anchor = side.request_windows[case.age_from][case.age_anchor]
    deadline = anchor + case.age_seconds
    while (remaining := deadline - time.monotonic()) > 0:
        time.sleep(min(20, remaining))
        print(case.ident + ': own-side ageing ' + str(round(time.monotonic() - anchor))
              + '/' + str(case.age_seconds) + 's', flush=True)
    if case.age_limit and time.monotonic() - anchor >= case.age_limit:
        raise RuntimeError(case.ident + ': real-time fixture window missed; rerun at lower server load')
    return anchor


def transport_snapshot(side, before, reference_after, case_id):
    """Capture bounded evidence only; a failed client does not fence server work."""
    state = {'capture': 'immediate after client failure', 'quiescence_proven': False}
    try:
        after = logical_snapshot(side.rpc('snapshot'))
        state['differences'] = [asdict(item) for item in differences(reference_after, after, '/state-immediate')]
    except RemoteError as error:
        state['snapshot_error_class'] = error.error_class
    if not hasattr(side, 'failed_prerequisites'):
        side.failed_prerequisites = []
    side.failed_prerequisites.append(case_id)
    return state


def execute(pair, case, available, routes):
    request = dict(method=case.method, path=case.path, document=case.document, raw=case.raw,
                   headers=case.headers, auth=case.auth)
    observations, snapshots, flow_requests, flow_checks, setup_observations = {}, {}, {}, {}, {}
    response_windows = {}
    response_elapsed = {}
    resolved_requests = {}
    request_failure = None
    transport_state = None
    if case.boundary:
        if case.route or len(case.boundary) != 2 or case.boundary[0] != case.method or not isinstance(case.path, str):
            raise ValueError('external boundary must declare its actual method/path with no routers route ID')
        method, template = case.boundary
        missing_routes = [] if registered_request(method, case.path, available) else [method + ' ' + template]
    else:
        missing_routes = [case.route] if (routes[case.route].method, shape(routes[case.route].path)) not in available else []
    missing_routes += [ident for ident in case.requires
                       if (routes[ident].method, shape(routes[ident].path)) not in available]
    missing_routes += [method + ' ' + path for method, path in case.manifest_requires
                       if not registered_request(method, path, available)]
    for control in case.prepare:
        operation = control.get('rpc')
        if operation not in ('http', 'assert_http', 'begin', 'open_request', 'wait_guard'):
            continue
        method = control.get('method', 'PUT' if operation == 'wait_guard' else 'GET')
        path = control.get('path')
        if isinstance(path, str) and not registered_request(method, path, available):
            missing_routes.append(method + ' ' + path)
    startup_failure = getattr(pair, 'go_startup_failure', None)
    implemented = not startup_failure and not missing_routes and not pair.go.missing_effects
    prior_failed = getattr(pair.go, 'failed_prerequisites', [])
    blocked_reference = 'Go prior HTTP/device-gate prerequisite failed: ' + ', '.join(prior_failed) if prior_failed else None
    for name, side in pair.sides.items():
        if name == 'go' and (not implemented or blocked_reference):
            continue
        try:
            preparation = cases.resolve(list(case.prepare), side.observations)
        except (KeyError, IndexError, TypeError) as error:
            if name != 'go':
                raise
            blocked_reference = 'Go preparation lacks a required own-side HTTP reference: ' + str(error)
            break
        before_preparation = logical_snapshot(side.rpc('snapshot')) if case.capture_before_prepare else None
        flow_requests[name] = []
        flow_checks[name] = []
        setup_observations[name] = []
        preparation_before = (before_preparation if before_preparation is not None else
                              logical_snapshot(side.rpc('snapshot'))) if preparation else None
        try:
            for control in preparation:
                if control.get('rpc') == 'restart':
                    side.rpc('stop', kill=control.get('kill', False))
                    try:
                        side.start()  # Same production entry and fresh public login.
                    except RemoteError as error:
                        failure = observed_startup_exit(side, error, 'public native process restart') if name == 'go' else None
                        if failure is None:
                            raise
                        startup_failure = pair.go_startup_failure = failure
                        implemented = False
                        break
                elif control.get('rpc') == 'filesystem_fault':
                    side.rpc(**{key: value for key, value in control.items() if key != 'rpc'}, operation='filesystem_fault')
                elif control.get('rpc') == 'http':
                    checks = control.get('checks', {'success': True})
                    if not isinstance(checks, dict) or not checks:
                        raise ValueError('public HTTP prerequisite needs explicit nonempty body checks')
                    values = {key: value for key, value in control.items() if key not in ('rpc', 'expected', 'checks', 'absent')}
                    reply = side.request(**values)
                    setup_observations[name].append({'request': values, 'response': response_view(reply)})
                    valid = reply['status'] == control['expected'] and isinstance(reply['content'], dict)
                    if valid:
                        try:
                            valid = all(not differences(cases.at(reply['content'], path), expected)
                                        for path, expected in checks.items())
                        except (KeyError, IndexError, TypeError, ValueError):
                            valid = False
                        for path in control.get('absent', ()):
                            try:
                                cases.at(reply['content'], path)
                            except (KeyError, IndexError, TypeError, ValueError):
                                pass
                            else:
                                valid = False
                    if not valid:
                        if name == 'python':
                            raise RuntimeError(case.ident + ': public HTTP fixture setup failed on python')
                        blocked_reference = 'Go public HTTP prerequisite returned an unexpected response'
                        if values['method'] not in ('GET', 'HEAD', 'OPTIONS'):
                            if not hasattr(side, 'failed_prerequisites'):
                                side.failed_prerequisites = []
                            side.failed_prerequisites.append(case.ident)
                        break
                elif control.get('rpc') == 'open_request':
                    values = {key: value for key, value in control.items() if key not in ('rpc', 'document')}
                    document = control.get('document')
                    flow_requests[name].append({'method': values['method'], 'path': values['path'], 'document': document})
                    values['headers'] = {'Content-Type': 'application/json', **values.get('headers', {})}
                    if side.token:
                        values['headers'].setdefault('Authorization', 'Bearer ' + side.token)
                    values['body'] = base64.b64encode(json.dumps(document, ensure_ascii=False, separators=(',', ':')).encode()).decode() \
                                     if document is not None else None
                    side.rpc('open_request', **values)
                elif control.get('rpc') == 'abandon':
                    side.rpc('abandon', ident=control['ident'])
                elif control.get('rpc') == 'assert_http':
                    values = {key: value for key, value in control.items() if key not in ('rpc', 'expected')}
                    reply = side.request(**values)
                    flow_checks[name].append({'request': values, 'response': response_view(reply)})
                    if name == 'python' and reply['status'] != control['expected']:
                        raise RuntimeError(case.ident + ': reference flow guard reached the wrong HTTP status')
                elif control.get('rpc') == 'pending_status':
                    reply = side.rpc('pending_status')
                    flow_checks[name].append({'client_future_done': reply})
                    if name == 'python' and differences(reply, control['expected']):
                        raise RuntimeError(case.ident + ': reference concurrent-request overlap missed; rerun at lower load')
                elif control.get('rpc') == 'wait_guard':
                    reply = wait_guard(side, control['path'], control.get('seconds', 30), strict=name == 'python')
                    flow_checks[name].append({'request': {'method': 'PUT', 'path': control['path'], 'document': []},
                                              'response': response_view(reply)})
                elif control.get('rpc') == 'begin':
                    values = {key: value for key, value in control.items() if key != 'rpc'}
                    if 'document' in values:
                        if 'body' in values:
                            raise ValueError('async public request cannot specify both document and encoded body')
                        values['body'] = base64.b64encode(json.dumps(values.pop('document'), ensure_ascii=False,
                            separators=(',', ':')).encode()).decode()
                        values['headers'] = {'Content-Type': 'application/json', **values.get('headers', {})}
                    ident = values['ident']
                    side.pending[ident] = {'before': logical_snapshot(side.rpc('snapshot')), 'started': time.time(),
                                           'started_monotonic': time.monotonic(),
                                           'request': {'method': values['method'], 'path': values['path']}}
                    values['headers'] = {'Authorization': 'Bearer ' + side.token, **values.get('headers', {})}
                    values.setdefault('body', None)
                    side.rpc('async', **values)
                elif control.get('rpc') == 'await_gate':
                    ident = control['ident']
                    reply = side.rpc('await_gate', **{key: value for key, value in control.items() if key != 'rpc'})
                    observation = {'request': side.pending[ident]['request'], 'gate': control['gate'],
                                   'state': reply['state']}
                    if 'response' in reply:
                        observation['response'] = response_view(decode_response(reply['response']))
                    for key in ('error_class', 'exit_code', 'seconds'):
                        if key in reply:
                            observation[key] = reply[key]
                    setup_observations[name].append(observation)
                    if reply['state'] != 'device-entered':
                        if name == 'python':
                            raise RuntimeError(case.ident + ': Python asynchronous prerequisite did not reach its device gate: '
                                               + reply['state'])
                        blocked_reference = 'Go asynchronous prerequisite did not reach its device gate'
                        if not hasattr(side, 'failed_prerequisites'):
                            side.failed_prerequisites = []
                        side.failed_prerequisites.append(case.ident)
                        break
                else:
                    side.rpc('fake', **control)
        except RemoteError as error:
            failure = observed_request_failure(side, error, 'preparatory public HTTP request') if name == 'go' else None
            if failure is None:
                raise
            request_failure = {**failure, 'request': {'method': control.get('method', 'PUT' if control.get('rpc') == 'wait_guard' else 'GET'),
                                                     'path': control.get('path')}}
            transport_state = transport_snapshot(side, preparation_before, snapshots['python']['after'], case.ident)
            break
        if name == 'go' and (blocked_reference or startup_failure):
            break
        before = (before_preparation if before_preparation is not None else
                  side.pending[case.join]['before'] if case.join else logical_snapshot(side.rpc('snapshot')))
        try:
            own_request = {**request, 'document': cases.resolve(case.document, side.observations),
                           'path': cases.resolve(case.path, side.observations)}
        except (KeyError, IndexError, TypeError) as error:
            if name != 'go':
                raise
            blocked_reference = 'Go previous HTTP response lacks a required own-side reference: ' + str(error)
            break
        log_position = side.rpc('log_position') if name == 'python' and case.console_patterns else None
        resolved_requests[name] = own_request
        age_anchor = wait_age(side, case) if case.age_from else None
        started = side.pending[case.join]['started'] if case.join else time.time()
        request_started = side.pending[case.join]['started_monotonic'] if case.join else time.monotonic()
        try:
            response = (decode_response(side.rpc('join', ident=case.join)) if case.join else
                        poll(side, own_request, case.wait_until, case.wait_seconds, strict=name == 'python') if case.wait_until else
                        side.request(**own_request))
            response_ended = time.monotonic()
            response_received_wall = time.time()
        except RemoteError as error:
            failure = observed_request_failure(side, error, 'main public HTTP request') if name == 'go' else None
            if failure is None:
                raise
            request_failure = {**failure, 'request': {'method': own_request['method'], 'path': own_request['path']}}
        finally:
            for control in case.finalize:
                if control.get('rpc') == 'filesystem_fault':
                    side.rpc(**{key: value for key, value in control.items() if key != 'rpc'}, operation='filesystem_fault')
                else:
                    side.rpc('fake', **control)
        if request_failure:
            transport_state = transport_snapshot(side, before, snapshots['python']['after'], case.ident)
            break
        if case.age_limit and response_ended - age_anchor >= case.age_limit:
            raise RuntimeError(case.ident + ': response exceeded real-time fixture window; rerun at lower server load')
        side.request_windows[case.ident] = {'started': request_started, 'ended': response_ended}
        response_elapsed[name] = response_ended - request_started
        response_windows[name] = (started, response_received_wall)
        try:
            settled = []
            for control in case.after_poll:
                settling_request = {'method': 'GET', 'path': control['path']}
                settled.append(response_view(poll(side, settling_request, control['expect'],
                                                  control.get('seconds', 30), strict=name == 'python')))
            if case.route == 'R023' and case.method == 'POST' and case.after_poll:
                # A terminal receipt precedes task-status/notification persistence.
                # Python sync() holds the same public L1 lock through both writes.
                parts = urlsplit(own_request['path'])
                guard_path = urlunsplit(parts._replace(path=parts.path.removesuffix('/rule-sync')))
                settling_request = {'method': 'PUT', 'path': guard_path}
                guard = wait_guard(side, guard_path, 30, strict=name == 'python')
                flow_checks[name].append({'request': {'method': 'PUT', 'path': guard_path, 'document': []},
                                          'response': response_view(guard)})
        except RemoteError as error:
            failure = observed_request_failure(side, error, 'post-response public settling request') if name == 'go' else None
            if failure is None:
                raise
            request_failure = {**failure, 'request': settling_request}
            # This main HTTP response really completed. Keep it as evidence,
            # while refusing to call its background work settled or successful.
            response['method'] = case.method
            observations[name] = response_view(response)
            if settled:
                observations[name]['settled_reads'] = settled
            transport_state = transport_snapshot(side, before, snapshots['python']['after'], case.ident)
            break
        if case.join:
            del side.pending[case.join]
        response['method'] = case.method
        observations[name] = response_view(response)
        if settled:
            observations[name]['settled_reads'] = settled
        if flow_checks[name]:
            observations[name]['flow_checks'] = flow_checks[name]
        side.observations[case.ident] = observations[name]
        after = logical_snapshot(side.rpc('snapshot'))
        if name == 'python' and after['fake']['errors']:
            raise RuntimeError(case.ident + ': fake contract error: ' + repr(after['fake']['errors']))
        snapshots[name] = {'before': before, 'after': after}
        if name == 'python':
            if response['status'] != case.expected:
                raise RuntimeError(f'{case.ident}: Python precondition expected {case.expected}, got '
                                   f'{response["status"]}: {response["content"]!r}')
            if case.console_patterns:
                output = side.rpc('logs', start=log_position, limit=8192)
                if not all(re.search(pattern, output) for pattern in case.console_patterns):
                    raise RuntimeError(case.ident + ': current reference exception does not match its expected source guard')
            actual_headers = {key.lower(): value for key, value in response['headers']}
            for key, expected in case.header_checks.items():
                if actual_headers.get(key.lower()) != expected:
                    raise RuntimeError(case.ident + ': Python header precondition failed: ' + key)
            if case.allow_methods:
                actual = observations[name]['headers'].get('allow')
                expected = [', '.join(sorted(case.allow_methods))]
                if actual != expected:
                    raise RuntimeError(case.ident + ': Python Allow method set differs: ' + repr(actual))
            if case.method == 'HEAD' and response['content'] != '':
                raise RuntimeError(case.ident + ': HEAD unexpectedly returned a response body')
            if case.message is not None and response['content'].get('message') != case.message:
                raise RuntimeError(case.ident + ': wrong Python guard reached: ' + repr(response['content']))
            if case.route == 'R006' and case.method == 'POST' and response['status'] == 200:
                inspect_python_token(response['content']['token'], own_request['document']['profile_id'],
                                     side.options.get('environment', {}).get('JWT_SECRET_KEY', JWT_SECRET))
            for path in case.absent:
                try:
                    cases.at(response['content'], path)
                except KeyError:
                    pass
                else:
                    raise RuntimeError(case.ident + ': private/unknown field leaked at ' + path)
            if case.html_error:
                content_type = dict((key.lower(), value) for key, value in response['headers']).get('content-type', '')
                if response['kind'] != 'text' or not content_type.startswith('text/html'):
                    raise RuntimeError(case.ident + ': expected the actual uncaught HTML error boundary')
            for path, expected in case.checks.items():
                actual = cases.at(response['content'], path)
                if differences(actual, cases.resolve(expected, side.observations)):
                    raise RuntimeError(case.ident + ': Python precondition at ' + path + ': ' + repr(actual))
            for path, pattern in case.formats.items():
                actual = cases.at(response['content'], path)
                if not isinstance(actual, str) or not re.fullmatch(pattern, actual):
                    raise RuntimeError(case.ident + ': invalid Python identity/hash at ' + path)
            for path in case.times:
                timestamp(cases.at(response['content'], path), started - 1, response_received_wall + 1)
            if not hasattr(pair, 'response_clocks'):
                pair.response_clocks = ResponseClocks()
            pair.response_clocks.reference(case, observations[name], response_windows[name])
            if not hasattr(pair, 'probe_clocks'):
                pair.probe_clocks = ProbeClocks()
            pair.probe_clocks.reference(case, own_request, observations[name], after, response_windows[name])
            if response_elapsed[name] < case.elapsed_min:
                raise RuntimeError(case.ident + ': expected real transport wait did not occur')
            transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
            for requirement in case.expected_requests:
                count = sum(World.matches(item, requirement['match']) for item in transcript)
                if count != requirement['count']:
                    raise RuntimeError(case.ident + ': outbound protocol count mismatch: ' + repr(requirement['match']))
            cursor = 0
            for match in case.request_order:
                while cursor < len(transcript) and not World.matches(transcript[cursor], match):
                    cursor += 1
                if cursor == len(transcript):
                    raise RuntimeError(case.ident + ': outbound transaction order mismatch: ' + repr(match))
                cursor += 1
            if case.effect in ('profile-write', 'profile-write-device', 'router-create', 'router-delete'):
                profile_write(before, after, case.profile)
                if case.effect != 'profile-write-device':
                    configuration_only(before, after)
            for path in case.retained_fields:
                original = cases.at(fields(before, 'profile', case.profile), path)
                current = cases.at(fields(after, 'profile', case.profile), path)
                if differences(original, current):
                    raise RuntimeError(case.ident + ': unrelated profile field changed at ' + path)
            for path, expected in case.stored.items():
                actual = cases.at(fields(after, 'profile', case.profile), path)
                if differences(actual, cases.resolve(expected, side.observations)):
                    raise RuntimeError(case.ident + ': stored precondition failed at ' + path + ': ' + repr(actual))
            for path, fragments in case.stored_contains.items():
                actual = cases.at(fields(after, 'profile', case.profile), path)
                if not isinstance(actual, str) or not all(fragment in actual for fragment in fragments):
                    raise RuntimeError(case.ident + ': stored text lacks required evidence at ' + path)
            for path, fragments in case.stored_excludes.items():
                actual = cases.at(fields(after, 'profile', case.profile), path)
                if not isinstance(actual, str) or any(fragment in actual for fragment in fragments):
                    raise RuntimeError(case.ident + ': stale stored text evidence remains at ' + path)
            for path in case.stored_absent:
                try:
                    cases.at(fields(after, 'profile', case.profile), path)
                except KeyError:
                    pass
                else:
                    raise RuntimeError(case.ident + ': ignored input unexpectedly persisted at ' + path)
            for path, expected in case.fake_checks.items():
                if differences(cases.at(after['fake'], path), expected):
                    raise RuntimeError(case.ident + ': fake device postcondition failed at ' + path)
            for path, expected in case.fake_lengths.items():
                if len(cases.at(after['fake'], path)) != expected:
                    raise RuntimeError(case.ident + ': fake device length postcondition failed at ' + path)
            for pattern, expected in case.file_contents.items():
                matching = [value for path, value in after['files'].items() if re.fullmatch(pattern, path)]
                if len(matching) != 1 or base64.b64decode(matching[0]) != expected.encode('utf-8'):
                    raise RuntimeError(case.ident + ': generated/cache file content postcondition failed: ' + pattern)
            for filename, requirements in case.file_checks.items():
                document = json.loads(base64.b64decode(after['files'][filename]))
                for path, expected in requirements.items():
                    if differences(cases.at(document, path), expected):
                        raise RuntimeError(case.ident + ': generated JSON file postcondition failed: ' + filename + ':' + path)
            for filename, paths in case.file_absent.items():
                document = json.loads(base64.b64decode(after['files'][filename]))
                for path in paths:
                    try:
                        cases.at(document, path)
                    except (KeyError, IndexError):
                        pass
                    else:
                        raise RuntimeError(case.ident + ': stale generated JSON member remains: ' + filename + ':' + path)
            if case.effect == 'router-create':
                old = fields(before, 'profile', case.profile)['routers']['items']
                new = fields(after, 'profile', case.profile)['routers']['items']
                if len(new) != len(old) + 1 or differences(new[:-1], old) or new[-1]['id'] != response['content']['router']['id']:
                    raise RuntimeError(case.ident + ': create did not append exactly the returned router')
                if len({row['id'] for row in new}) != len(new):
                    raise RuntimeError(case.ident + ': create generated duplicate router identity')
            if case.effect == 'router-delete':
                old = fields(before, 'profile', case.profile)['routers']['items']
                new = fields(after, 'profile', case.profile)['routers']['items']
                removed = own_request['path'].split('?')[0].rsplit('/', 1)[-1]
                if len(old) != len(new) + 1 or differences([row for row in old if row['id'] != removed], new):
                    raise RuntimeError(case.ident + ': delete did not remove exactly the selected record')
            if case.effect == 'device-only':
                if differences(before['database'], after['database']):
                    raise RuntimeError(case.ident + ': manual device operation modified configuration storage')
            if case.effect == 'device-read':
                if differences(before['database'], after['database']):
                    raise RuntimeError(case.ident + ': device read modified configuration storage')
                configuration_only(before, after)
            for peer, table, identity_key, identity in case.foreign:
                def objects(snapshot):
                    return [row for row in snapshot['fake'][peer][table] if row.get(identity_key) == identity]
                if not objects(before) or differences(objects(before), objects(after)):
                    raise RuntimeError(case.ident + ': foreign device object changed or disappeared')
            for peer, table, criteria in case.preserved:
                def selected(snapshot):
                    return [row for row in snapshot['fake'][peer][table] if World.matches(row, criteria)]
                if not selected(before) or differences(selected(before), selected(after)):
                    raise RuntimeError(case.ident + ': expected retained device objects changed or disappeared')
            if getattr(pair, 'store_heads', None) is not None:
                pair.store_heads.reference(before, after)
            if getattr(pair, 'policy_revisions', None) is not None:
                pair.policy_revisions.reference(case, observations[name], after)
            if case.unchanged:
                changed = differences(before, after)
                if changed:
                    raise RuntimeError(case.ident + ': Python supposedly rejected/read-only request changed state: '
                                       + repr([asdict(item) for item in changed[:5]]))
    found = (differences(setup_observations['python'], setup_observations['go'], '/setup-http')
             if 'go' in setup_observations else [])
    if startup_failure:
        found.append({'path': '/public-process/startup', 'python': {'state': 'public listener ready; HTTP request completed'},
                      'go': startup_failure, 'category': 'process'})
    if request_failure:
        found.append({'path': '/public-process/request', 'python': {'http_status': observations['python']['status']},
                      'go': request_failure, 'category': 'transport'})
    if 'go' in response_elapsed and response_elapsed['go'] < case.elapsed_min:
        found.append({'path': '/contract/http/minimum-elapsed-seconds',
                      'python': {'minimum': case.elapsed_min, 'observed': response_elapsed['python']},
                      'go': response_elapsed['go'], 'category': 'timing'})
    go_bug_outcome = None
    if implemented and not blocked_reference and not request_failure:
        if not hasattr(pair, 'router_ids'):
            pair.router_ids = RouterIDs()
        if (case.route == 'R008' and case.method == 'POST' and case.effect == 'router-create'
                and observations['go']['status'] == observations['python']['status'] == 200):
            try:
                pair.router_ids.created(case.profile, observations, snapshots)
            except ValueError as error:
                found.append({'path': '/safety/generated-router-identity',
                              'python': 'fresh URL-safe identity bound to exactly the appended stored router',
                              'go': str(error), 'category': 'safety'})
        if case.html_error and observations['go']['status'] == 500 and observations['go']['kind'] == 'text':
            # X03 explicitly excludes implementation-specific HTML prose only
            # for these individually named Python uncaught-exception cases.
            for observation in observations.values():
                observation['body'] = '<HTML 500 body excluded by reviewed X03>'
        if not hasattr(pair, 'response_clocks'):
            pair.response_clocks = ResponseClocks()
        timed_observations, clock_differences = pair.response_clocks.pair(case, observations, response_windows)
        found += clock_differences
        found += pair.probe_clocks.record(case, resolved_requests, observations, snapshots, response_windows)
        projected = {side: pair.probe_clocks.response(side, case.profile, case.route, timed_observations[side])
                     for side in ('python', 'go')}
        projected = {side: pair.router_ids.response(side, case.profile, case.route, projected[side])
                     for side in ('python', 'go')}
        projected_states = {side: pair.router_ids.snapshot(side,
                            pair.probe_clocks.snapshot(side, snapshots[side]['after'])) for side in ('python', 'go')}
        if getattr(pair, 'policy_revisions', None) is not None:
            projected, revision_differences = pair.policy_revisions.pair(case, observations, snapshots, projected, projected_states)
            found += revision_differences
        found += differences(projected['python'], projected['go'], '/http')
        if getattr(pair, 'store_heads', None) is not None:
            projected_states, head_differences = pair.store_heads.pair(snapshots, projected_states)
            found += head_differences
        found += differences(projected_states['python'], projected_states['go'], '/state')
        if case.unchanged:
            found += differences(snapshots['go']['before'], snapshots['go']['after'], '/go-unexpected-write')
        if case.python_bug == 'IKUAI_PROBE_NESTED_SCHEMA_CRASH':
            body = observations['go']['body']
            before, after = snapshots['go']['before'], snapshots['go']['after']
            reply = observations['go']
            rejected = (400 <= reply['status'] < 600 and reply['kind'] == 'json' and isinstance(body, dict)
                        and body.get('success') is False and isinstance(body.get('message'), str)
                        and bool(body['message'].strip()))
            device_changed = any(differences(before['fake'][key], after['fake'][key]) for key in ('ros', 'ikuai', 'agent'))
            writes = [item for item in after['fake']['transcript'][len(before['fake']['transcript']):]
                      if not (item['peer'] == 'ikuai' and item['method'] == 'POST'
                              and (item['path'] == '/Action/login' or
                                   item['path'] == '/Action/call' and isinstance(item['body'], dict)
                                   and item['body'].get('action') == 'show'))]
            own_metadata = probe_schemas.only_failed_probe_metadata(before, after, case.profile,
                                                                    'r-ik', response_windows['go'])
            files_changed = bool(differences(before['files'], after['files']) or
                                 differences(before.get('filesystem', {}), after.get('filesystem', {})))
            go_bug_outcome = {'http_status': reply['status'], 'structured_refusal': rejected,
                              'only_failed_probe_metadata': own_metadata, 'files_changed': files_changed,
                              'device_changed': bool(device_changed), 'unexpected_device_requests': len(writes)}
            if not rejected or device_changed or writes or not own_metadata or files_changed:
                found.append({'path': '/safety/untrusted-probe-schema',
                              'python': 'bounded structured refusal without device mutation; exact policy requires O12 review',
                              'go': go_bug_outcome, 'category': 'safety'})
        if case.python_bug == 'NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION':
            body = observations['go']['body']
            expected_side = [{'tags': ['side_udp'], 'listen': '127.0.0.1:5353', 'entry': 'side'}]
            usable = (observations['go']['status'] == 200 and isinstance(body, dict)
                      and body.get('success') is True and body.get('editable') is True
                      and body.get('reason') == '' and not differences(body.get('side_entries'), expected_side))
            if case.method == 'PUT':
                stored = fields(snapshots['go']['after'], 'profile', case.profile).get('oxidns', {})
                native = stored.get('native_config') if isinstance(stored, dict) else None
                usable = (usable and isinstance(native, str)
                          and all(fragment in native for fragment in ('cf_device_policy', 'cf_device_direct',
                              'jump cf_device_policy', 'side_forward', 'opaque_side_setting', 'side_custom_keep🧪'))
                          and not differences(stored.get('device_policies'),
                              [{'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': 'direct'}]))
            go_bug_outcome = {'http_status': observations['go']['status'], 'valid_side_listener_editable': usable}
            if not usable:
                found.append({'path': '/safety/valid-isolated-side-DNS-must-remain-editable',
                              'python': 'Go must reject the false URL-scheme reference and preserve the side chain',
                              'go': go_bug_outcome, 'category': 'safety'})
        if case.python_bug == 'IKUAI_NETMASK_SUFFIX_ROLLBACK':
            before_ids = {row['id'] for row in snapshots['go']['before']['fake']['ikuai']['static_rt']}
            created = [row for row in snapshots['go']['after']['fake']['ikuai']['static_rt'] if row['id'] not in before_ids]
            go_bug_outcome = {'http_status': observations['go']['status'], 'created_static_rows_remaining': len(created)}
            if observations['go']['status'] >= 400 and created:
                found.append({'path': '/safety/failed-release-created-static-routes', 'python': 'Go failure must compensate cleanly',
                              'go': len(created), 'category': 'safety'})
        if case.python_bug == 'AGENT_FORWARD_FILTER_UNHASHABLE':
            body = observations['go']['body']
            gateways = body.get('gateways') if isinstance(body, dict) else None
            usable = (observations['go']['status'] == 200 and isinstance(gateways, list) and len(gateways) == 2
                      and body.get('success') is True and all(isinstance(entry, dict) for entry in gateways)
                      and gateways[0].get('agent_id') == 'm-a' and gateways[1].get('agent_id') == 'm-b'
                      and gateways[1].get('state') == 'needs_persist')
            go_bug_outcome = {'http_status': observations['go']['status'], 'unaffected_gateway_preserved': usable}
            if not usable:
                found.append({'path': '/safety/malformed-Agent-metadata-hides-unaffected-gateway',
                              'python': 'Go must contain one malformed Agent reply and preserve the healthy gateway',
                              'go': observations['go']['status'], 'category': 'safety'})
        if case.python_bug in ('HEAD_CREATES_ROUTER', 'HEAD_PERSISTS_FORWARDING'):
            before, after = snapshots['go']['before'], snapshots['go']['after']
            changed = bool(differences(before['database'], after['database'])) or any(
                differences(before['fake'][key], after['fake'][key]) for key in ('ros', 'ikuai', 'agent'))
            transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
            posts = sum(item['method'] == 'POST' and item['peer'] == 'agent' for item in transcript)
            reads = sum(item['method'] == 'GET' and item['peer'] == 'agent' for item in transcript)
            go_bug_outcome = {'http_status': observations['go']['status'], 'configuration_or_device_changed': changed,
                              'agent_post_requests': posts, 'agent_get_requests': reads}
            if changed or observations['go']['status'] != 200 or posts or (case.route == 'R025' and reads != 2):
                found.append({'path': '/safety/HEAD-must-execute-read-only-GET',
                              'python': 'Go HEAD must read, never execute the POST branch',
                              'go': go_bug_outcome, 'category': 'safety'})
    # Auth values are synthetic but reports don't need bearer credentials.
    request['document'] = cases.resolve(case.document, pair.python.observations)
    request['path'] = cases.resolve(case.path, pair.python.observations)
    request['headers'] = {key: '<synthetic credential>' if key.lower() == 'authorization' else value
                          for key, value in request['headers'].items()}
    request['raw'] = request['raw'].decode(errors='replace') if request['raw'] is not None else None
    if flow_requests.get('python'):
        request['preceding_abandoned_requests'] = flow_requests['python']
    safety = any((item.get('category') if isinstance(item, dict) else item.category) == 'safety' for item in found)
    status = ('different' if startup_failure or request_failure else 'blocked-by-difference' if blocked_reference else 'go-unimplemented' if not implemented else 'different' if safety else
              'reference-bug-observation' if case.python_bug else 'different' if found else 'compared')
    unimplemented_reason = {'missing_routes': sorted(set(missing_routes)),
                            'prior_missing_mutations': [item['case'] for item in pair.go.missing_effects]}
    mutating_setup = any(control.get('rpc') in ('http', 'open_request')
                         and control.get('method', 'GET') not in ('GET', 'HEAD', 'OPTIONS') for control in case.prepare)
    if not implemented and (mutating_setup or case.effect in
            ('profile-write', 'profile-write-device', 'router-create', 'router-delete', 'device-only')):
        pair.go.missing_effects.append({'case': case.ident, 'missing_routes': sorted(set(missing_routes))})
    python_bug_outcome = None
    if case.python_bug == 'IKUAI_PROBE_NESTED_SCHEMA_CRASH':
        before, after = snapshots['python']['before'], snapshots['python']['after']
        python_bug_outcome = {'http_status': observations['python']['status'],
                              'database_changed': bool(differences(before['database'], after['database'])),
                              'device_changed': any(bool(differences(before['fake'][key], after['fake'][key]))
                                                    for key in ('ros', 'ikuai', 'agent'))}
    elif case.python_bug == 'NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION':
        python_bug_outcome = {'http_status': observations['python']['status'], 'valid_native_edit_incorrectly_blocked': True}
    elif case.python_bug == 'IKUAI_NETMASK_SUFFIX_ROLLBACK':
        python_bug_outcome = {'created_static_rows_remaining': len(snapshots['python']['after']['fake']['ikuai']['static_rt']),
                              'static_rt_after': snapshots['python']['after']['fake']['ikuai']['static_rt']}
    elif case.python_bug == 'AGENT_FORWARD_FILTER_UNHASHABLE':
        python_bug_outcome = {'http_status': observations['python']['status'], 'unaffected_gateway_hidden': True}
    elif case.python_bug in ('HEAD_CREATES_ROUTER', 'HEAD_PERSISTS_FORWARDING'):
        before, after = snapshots['python']['before'], snapshots['python']['after']
        transcript = after['fake']['transcript'][len(before['fake']['transcript']):]
        python_bug_outcome = {'http_status': observations['python']['status'],
            'configuration_changed': bool(differences(before['database'], after['database'])),
            'agent_post_requests': sum(item['method'] == 'POST' and item['peer'] == 'agent' for item in transcript),
            'agent_get_requests': sum(item['method'] == 'GET' and item['peer'] == 'agent' for item in transcript)}
    return {'case': case.ident, 'route': case.route, 'boundary': list(case.boundary), 'owner': case.owner,
            'scope': 'cross-domain' if case.boundary else 'routers', 'entries': list(case.entries), 'request': request,
            'status': status,
            'unimplemented_reason': unimplemented_reason if not implemented else None,
            'blocked_reference': blocked_reference,
            'setup_observations': setup_observations,
            'go_process': request_failure or startup_failure, 'go_transport_state': transport_state,
            'response_elapsed_seconds': response_elapsed if case.elapsed_min else None,
            'python': observations['python'], 'go': observations.get('go'),
            'python_bug': case.python_bug, 'python_bug_outcome': python_bug_outcome, 'go_bug_outcome': go_bug_outcome,
            'differences': [asdict(item) if not isinstance(item, dict) else item for item in found]}


def report(output, results, available, announce=True, error=None):
    routes, entries = load()
    referenced = {ident for result in results for ident in result['entries']}
    unknown = referenced - entries.keys()
    if unknown:
        raise ValueError('recipes reference nonexistent design IDs: ' + repr(sorted(unknown)))
    counts = Counter(result['status'] for result in results)
    # Pending remains visible. A design entry may require further variants even
    # after a concrete recipe references it; completion is a separate audit.
    ledger = {ident: {'kind': entry.kind, 'source_line': entry.source_line,
                     'recipes': [item.get('world', '<self-check>') + ':' + item['case'] for item in results if ident in item['entries']],
                     'audit': 'variants-in-progress' if ident in referenced else 'pending'}
              for ident, entry in entries.items()}
    for ident, verdict in evaluate(results).items():
        ledger[ident].update(verdict)
    audited = sum(item['audit'] == 'variants-audited' for item in ledger.values())
    source_unreachable = sum(item['audit'] == 'source-unreachable' for item in ledger.values())
    review_superseded = sum(item['audit'] == 'review-superseded' for item in ledger.values())
    remaining = len(entries) - audited - source_unreachable - review_superseded
    data = {'matrix_entries': len(entries), 'matrix_routes': len(routes),
        'implemented_routes': (sum((route.method, shape(route.path)) in available for route in routes.values())
                               if available is not None else None),
        'native_inventory_observed': available is not None,
        'recipe_counts': dict(counts),
        'domain_recipe_counts': dict(Counter(item['status'] for item in results if item.get('scope', 'routers') == 'routers')),
        'cross_domain_recipe_counts': dict(Counter(item['status'] for item in results if item.get('scope') == 'cross-domain')),
        'referenced_entries': len(referenced),
        'executed_worlds': sorted({item.get('world', '<self-check>') for item in results}),
        'python_bugs': sorted({item['python_bug'] for item in results if item.get('python_bug')}),
        'complete': remaining == 0 and error is None, 'remaining_audits': remaining,
        'audited_entries': audited, 'source_unreachable_entries': source_unreachable,
        'review_superseded_entries': review_superseded,
        'ledger': ledger, 'results': results}
    if error:
        data['fixture_error'] = error
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = output.with_suffix(output.suffix + '.tmp')
    temporary.write_text(json.dumps(report_aliases(data), ensure_ascii=False, indent=2) + '\n')
    temporary.replace(output)
    if announce:
        print(json.dumps({key: value for key, value in data.items() if key not in ('ledger', 'results')}, ensure_ascii=False))
    return data


def findings(path, data):
    data = report_aliases(data)
    lines = ['# Routers Step B 黑盒差分发现', '',
        '状态：进行中。矩阵审计尚未完成；不能据此宣称 routers 已通过验收。', '',
        f'矩阵：{data["matrix_routes"]} 条路由、{data["matrix_entries"]} 个设计条目。',
        f'公开 Go 入口实际注册本领域路由：{data["implemented_routes"]} 条。',
        '执行配方统计：`' + json.dumps(data['recipe_counts'], ensure_ascii=False, sort_keys=True) + '`。',
        '领域配方：`' + json.dumps(data.get('domain_recipe_counts', {}), ensure_ascii=False) + '`；跨域G23配方：`'
        + json.dumps(data.get('cross_domain_recipe_counts', {}), ensure_ascii=False) + '`（不算26条领域路由已实现/通过）。',
        '实际执行世界（统计仅涵盖这些世界）：`' + ', '.join(data['executed_worlds']) + '`。',
        f'已有执行配方引用的设计条目：{data["referenced_entries"]}；逐条变体审计仍未完成。',
        f'本次实际执行满足显式变体审计：{data["audited_entries"]}；源码证明公开入口不可达：'
        f'{data.get("source_unreachable_entries", 0)}；O6 已明确更正的旧项：'
        f'{data.get("review_superseded_entries", 0)}（后二者单列，不算执行通过）；其余{data["remaining_audits"]}待补。', '',
        '“Go 尚未实现”单独计数，不算失败，也不算差分通过。路由层 R15/R17 未注册时，',
        '即使底层 R01–R06 已移植，也不能声称 HTTP 行为已比较。', '',
        '仅使用合成数据；两端独立 DATA_DIR、网络命名空间和设备 fake；未访问生产或真实设备。',
        '比较器会检查状态、语义响应头、类型/键/值/数组顺序、完整逻辑数据库、文件与设备状态。',
        'Python 前置条件失败单列为夹具错误，不算 Go 发现；实际比较为零时不能下“无缺陷”结论。', '']
    changed = [result for result in data['results']
               if result['status'] == 'different' or (result['status'] == 'blocked-by-difference' and result['differences'])]
    if not changed:
        lines += ['目前没有已比较的 Go 缺陷结论。Python 参考异常单独记录；“Go 尚未实现”不算差分通过。', '']
    for result in (item for item in data['results'] if item.get('python_bug')):
        bug = BUGS[result['python_bug']]
        lines += ['## Python bug · ' + bug.severity + ' · ' + result['python_bug'], '',
                  '- 用例：' + result.get('world', '<self-check>') + ':' + result['case'] + '；矩阵：' + ', '.join(result['entries']),
                  '- 建议负责模块：' + bug.owner + '。', '- ' + bug.summary,
                  '- 请求：`' + json.dumps(result['request'], ensure_ascii=False) + '`',
                  '- Python：`' + json.dumps(result['python'], ensure_ascii=False) + '`',
                  '- Python 设备终态：`' + json.dumps(result['python_bug_outcome'], ensure_ascii=False) + '`',
                  '- Go：' + (('Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`' +
                              json.dumps(result.get('unimplemented_reason'), ensure_ascii=False) + '`')
                             if result['status'] == 'go-unimplemented' else
                             'Go 公开进程启动退出，未虚构 HTTP 响应：`' + json.dumps(result['go_process'], ensure_ascii=False) + '`'
                             if result.get('go_process') else
                             '前置 Go 响应差异阻止后续请求，未使用 Python 的值代替 Go：`' + str(result['blocked_reference']) + '`'
                             if result['go'] is None else
                             '`' + json.dumps({'response': result['go'], 'outcome': result['go_bug_outcome']}, ensure_ascii=False) + '`'),
                  *['- ' + item for item in bug.followups], '']
    for index, result in enumerate(changed, 1):
        severity = 'P1' if result['request']['method'] != 'GET' else 'P2'
        owner = result.get('owner') or ('R17（客户端 HTTP）' if result['route'] in ('R002', 'R003', 'R004', 'R005', 'R006') else 'R15（路由 HTTP）')
        if result.get('python_bug'):
            owner = BUGS[result['python_bug']].owner
        if result['python']['status'] == 401 and result['go'] and result['go']['status'] < 400:
            severity = 'P1'
        lines += [f'## F{index:03} · {severity} · ' + result.get('world', '<self-check>') + ':' + result['case'], '',
                  '- 用例编号：' + ', '.join(result['entries']),
                  '- 建议负责模块：' + owner + '；设备传输差异另交 R04/R05，发布与同步交 R12/R14。',
                  '- 请求：`' + json.dumps(result['request'], ensure_ascii=False) + '`',
                  '- Python：`' + json.dumps(result['python'], ensure_ascii=False) + '`',
                  '- Go：`' + json.dumps(result['go'], ensure_ascii=False) + '`',
                  '- Go 公开进程：`' + json.dumps(result.get('go_process'), ensure_ascii=False) + '`',
                  *(['- 传输失败后的即时状态（未证明任务静止或补偿完成）：`'
                     + json.dumps(result['go_transport_state'], ensure_ascii=False) + '`']
                    if result.get('go_transport_state') else []),
                  '- 前序阻断原因：`' + json.dumps(result.get('blocked_reference'), ensure_ascii=False) + '`',
                  '- 前置公开 HTTP 实际响应：`' + json.dumps(result.get('setup_observations', {}), ensure_ascii=False) + '`', '',
                  '| 路径 | Python | Go | 类别 |', '|---|---|---|---|']
        for difference in result['differences']:
            escape = lambda value: json.dumps(value, ensure_ascii=False).replace('|', '&#124;').replace('\n', '<br>')
            lines.append('| ' + ' | '.join(escape(difference[key]) for key in ('path', 'python', 'go', 'category')) + ' |')
        lines.append('')
    lines += ['后续仍须补齐：其余 H 公开可达性与复合 guard 变体、设备失败/补偿、完整生成形态、',
              'Agent/备份/代理、后台轮询、断连/重启、动态值内容绑定和 O12 分类。详见进度文件与执行 ledger。', '']
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text('\n'.join(lines))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--keep', type=Path)
    parser.add_argument('--findings', type=Path)
    parser.add_argument('--allow-incomplete', action='store_true',
                        help='iteration only: allow pending coverage audits; default gate exits 2')
    parser.add_argument('--world', action='append', help='iteration only: run these named isolated worlds')
    parser.add_argument('--world-prefix', action='append', help='iteration only: select all isolated worlds with this prefix')
    args = parser.parse_args()
    routes, _ = load()
    results = []
    options = {'alpha': {'routers': {'items': [router(), router('r-ik', 'ikuai')]}},
               'beta': {'routers': {'items': [router()]}}}
    guard_options = fixtures.authentication_options(options)
    worlds = [('guards', guard_options, (cases.authentication, cases.query_credentials, cases.profile_selection_failures, cases.body_guards,
                      guards.router_settings, guards.router_settings_ikuai, guards.guard_vendor_support,
                      guards.client_draft, guards.fixed_ip_shape, guards.fixed_apply_shape,
                      guards.route_edit_shape, guards.workspace_shape, guards.rule_sync_shape, guards.rejected_draft_readback)),
              ('client-drafts', options, (workflows.client_drafts,)),
              ('client-agent-query-writes', fixtures.agent_options(), (workflows.client_agent_query_writes,)),
              ('client-canonical-names', options, (workflows.client_canonical_names,)),
              ('notes-capacity', fixtures.notes_capacity_options(), (fixtures.notes_capacity,)),
              ('client-mode-notes', options, (workflows.client_mode_notes,)),
              ('native-side-protocol-collision', policy_side.collision_options(), (policy_side.protocol_collision,)),
              ('native-owned-valid', policy_owned.options(policy_owned.document(), policy_owned.ROWS), (policy_owned.valid,)),
              ('native-policy-valid', policy_graph.options(policy_graph.document()), (policy_graph.valid,)),
              ('stored-policy-legacy', device_policy.legacy_options(), (device_policy.legacy,)),
              ('stored-policy-capacity', device_policy.capacity_options(), (device_policy.capacity,)),
              ('notes-keep-reservation', clients.options(), (clients.notes_keep_reservation,)),
              ('empty-manifests', fixtures.empty_manifest_options(), (fixtures.empty_manifests,)),
              ('legacy-credentials', fixtures.legacy_credentials_options(), (fixtures.legacy_credentials,)),
              ('router-guard-normalization', options, (guards.normalized_router_guards,)),
              ('workspace-drafts', options, (workflows.workspace_drafts, workflows.router_updates)),
              ('runtime-reads', options, (workflows.runtime_reads,)),
              ('sync-settings', options, (workflows.sync_settings,)),
              ('manual-routes', options, (workflows.manual_routes,)),
              ('router-creations', options, (workflows.router_creates,)),
              ('unlinked-inspection', options, (workflows.unlinked_inspection,)),
              ('agent-scope', fixtures.agent_options(), (fixtures.agent_scope,)),
              ('forwarding', fixtures.agent_options(), (fixtures.forwarding,)),
              ('forwarding-failures', fixtures.agent_options(),
                    (gateway.reads, gateway.writes, gateway.response_limits, gateway.display_filter, gateway.malformed_filter_bug)),
              ('forwarding-uncertain', fixtures.agent_options(), (gateway.uncertain, gateway.timeouts)),
              ('probe-errors', options, (probes.routeros, probes.ikuai)),
              ('probe-nested-schema', options, (probe_schemas.cases,)),
              ('probe-clock-roundtrip', options, (probe_workflows.roundtrip,)),
              ('probe-guard-controls', options, (probe_guards.controls, probe_guards.late_rows, probe_guards.invalid_versions)),
              ('probe-tls', probes.tls_options(), (probes.tls,)),
              ('probe-ipv6', probes.ipv6_options(), (probes.ipv6,)),
              ('release-ros', releases.options(), (releases.routeros_basic,)),
              ('release-ik-suffix', releases.options(), (releases.ikuai_suffix_bug,)),
              ('release-ik-plain', releases.options(), (releases.ikuai_plain,)),
              ('fixed-ip', clients.options(), (clients.fixed_ip,)),
              ('fixed-ip-normalization', clients.options(), (fixed_normalization.rejected, fixed_normalization.accepted)),
              ('fixed-ip-guards', clients.options(), (clients.fixed_preview_guards,)),
              ('fixed-ip-stale', clients.options(), (clients.fixed_stale,)),
              ('fixed-ip-uncertain', clients.options(), (clients.fixed_uncertain,)),
              ('fixed-ip-batch-rollback', clients.options(), (clients.fixed_batch_rollback,)),
              ('fixed-ip-64', clients.batch_options(), (clients.batch64,)),
              ('fixed-ip-restart-stable', clients.options(), (clients.fixed_restart_stable,)),
              ('fixed-ip-restart-random', clients.random_key_options(), (clients.fixed_restart_random,)),
              ('fixed-ip-expiry', clients.options(), (clients.fixed_expiry,)),
              ('fixed-ip-binding-cap', clients.binding_limit_options(), (clients.binding_limit,)),
              ('fixed-ip-long-id', clients.options(), (clients.long_binding_id,)),
              ('discovery', clients.options(), (clients.discovery,)),
              ('discovery-signature', clients.cache_options(), (clients.cache_signature,)),
              ('discovery-coalescing', clients.cache_options(), (clients.cache_coalescing,)),
              ('discovery-lru', clients.cache_lru_options(), (clients.cache_lru,)),
              ('discovery-staleness', clients.cache_options(), (clients.cache_staleness,)),
              ('discovery-failures', clients.options(), (clients.discovery_failures,)),
              ('rule-sync', sync.options(), (sync.manual_refresh,)),
              ('rule-sync-download-failures', sync.options(), (sync.download_failures,)),
              ('rule-sync-proxy', sync.proxy_options(), (sync.proxy_refresh,)),
              ('upstream-linked', upstream.options(), (upstream.linked,)),
              ('dns-guard', dns.options(), (dns.guard,)),
              ('dhcp-evidence-guards', dns.options(), (dhcp_guards.cases,)),
              ('dhcp-ikuai-evidence', dns.ikuai_options(), (ikuai_dhcp_guards.cases,)),
              ('dhcp-ikuai-plain', dns.ikuai_options(), (dns.ikuai_dhcp,)),
              ('dns-matching-failures', dns_targets.matching_options(), (dns_targets.matching_cases,)),
              ('dns-ha', dns.ha_options(), (dns.ha,)),
              ('disconnect-release', releases.options(), (disconnect.release,)),
              ('disconnect-fixed', clients.options(), (disconnect.fixed,)),
              ('disconnect-manual', options, (disconnect.manual,)),
              ('ipv6-fakeip-owned', ipv6.options(), (ipv6.owned,)),
              ('ipv6-fakeip-borrow-lla', ipv6.options(), (ipv6.borrowed_lla,)),
              ('ipv6-fakeip-borrow-ula', ipv6.options(), (ipv6.borrowed_ula,)),
              ('ipv6-fakeip-conflicts', ipv6.options(), (ipv6.conflicts,)),
              ('head-report-notifications', ipv6.options(), (ipv6.head_notifications,)),
              ('persistence-sqlite-busy', persistence.busy_options(), (persistence.sqlite_busy,)),
              ('persistence-publication-busy', persistence.busy_options(), (persistence.sqlite_publication_busy,)),
              ('persistence-readonly', options, (persistence.readonly_store,)),
              ('persistence-receipt', releases.options(), (persistence.receipt_rename_failure,)),
              ('return-protocol1', returns.options(), (returns.protocol1,)),
              ('return-protocol2', returns.options(), (returns.protocol2,)),
              ('return-rollback', returns.options(), (returns.route_failure_rolls_back_return,)),
              ('return-uncertain', returns.options(), (returns.uncertain_route_failure,)),
              ('return-safety', returns.options(), (returns.invalid_safety, returns.valid_allocation_boundaries)),
              ('policy-view', policy_view.options(), (policy_view.reads,)),
              ('routing-methods', options, (routing.options_and_dispatch, routing.local_heads, routing.reserved_agents)),
              ('head-discovery', clients.options(), (routing.discovery_head,)),
              ('head-upstream', upstream.options(), (routing.upstream_head,)),
              ('head-collection-empty', options, (routing.head_collection_empty,)),
              ('head-collection-body', options, (routing.head_collection_body,)),
              ('head-forwarding-empty', fixtures.agent_options(), (routing.head_forwarding_empty,)),
              ('head-forwarding-body', fixtures.agent_options(), (routing.head_forwarding_body,)),
              ('auth-off-editors', auth_off(options), (workflows.client_drafts, workflows.workspace_drafts,
                    workflows.router_updates, workflows.runtime_reads, workflows.sync_settings,
                    workflows.manual_routes, workflows.router_creates, workflows.unlinked_inspection, probes.auth_disabled)),
              ('auth-off-release', auth_off(releases.options()), (releases.routeros_basic, releases.ikuai_plain)),
              ('auth-off-fixed', auth_off(clients.options()), (clients.fixed_ip,)),
              ('auth-off-discovery', auth_off(clients.options()), (clients.discovery,)),
              ('auth-off-forwarding', auth_off(fixtures.agent_options()), (fixtures.forwarding,)),
              ('auth-off-sync', auth_off(sync.options()), (sync.manual_refresh,)),
              ('stored', fixtures.stored_options(), (fixtures.receipts, fixtures.stored_guards))]
    worlds += device_policy.worlds()
    worlds += router_lifecycle.worlds()
    worlds += ikuai_dhcp_writes.worlds()
    worlds += dns_targets.worlds()
    worlds += dns_targets.gateway_worlds()
    worlds += fixed_setup.worlds()
    worlds += fixed_sources.router_worlds()
    worlds += fixed_sources.binding_worlds()
    worlds += probe_workflows.invalidation_worlds(options)
    worlds += device_policy.invalid_ip_worlds()
    worlds += policy_graph.worlds()
    worlds += policy_owned.worlds()
    worlds += policy_side.worlds()
    worlds += policy_invalid.worlds()
    worlds += split.worlds()
    worlds += split_onboarding.worlds()
    worlds += split_state.worlds()
    worlds += forwarding_guards.worlds()
    worlds += workflows.job_lock_worlds(options)
    if args.world or args.world_prefix:
        names = {item[0] for item in worlds}
        unknown = set(args.world or ()) - names
        if unknown:
            parser.error('unknown isolated world: ' + ', '.join(sorted(unknown)))
        for prefix in args.world_prefix or ():
            if not prefix or not any(name.startswith(prefix) for name in names):
                parser.error('unknown or empty isolated-world prefix: ' + prefix)
        worlds = [item for item in worlds if item[0] in (args.world or ())
                  or any(item[0].startswith(prefix) for prefix in args.world_prefix or ())]
    available = None
    try:
        for name, options, groups in worlds:
            keep = args.keep / name if args.keep else None
            with Pair(args.binary, options, keep) as pair:
                if not pair.go_startup_failure:
                    current = registered(pair.go.rpc('manifest'))
                    if available is not None and current != available:
                        raise RuntimeError('public route inventory changed between synthetic worlds')
                    available = current
                for group in groups:
                    started = time.monotonic()
                    for case in group():
                        result = execute(pair, case, available or set(), routes)
                        result['world'] = name
                        results.append(result)
                        if len(results) % 50 == 0:
                            print(name + ': ' + str(len(results)) + ' cumulative recipes; last=' + case.ident, flush=True)
                    report(args.output, results, available, announce=False)
                    print(name + '/' + group.__name__ + ': ' + str(len(results)) + ' cumulative recipes, '
                          + format(time.monotonic() - started, '.2f') + 's', flush=True)
    except Exception as error:
        report(args.output, results, available, error=str(error))
        # Runtime global classification fixtures must not escape through a
        # diagnostic traceback while their exact values still drive comparisons.
        raise RuntimeError(report_aliases(str(error))) from None
    data = report(args.output, results, available)
    if args.findings:
        findings(args.findings, data)
    if data['recipe_counts'].get('different'):
        return 1
    if data['recipe_counts'].get('reference-bug-observation'):
        return 2  # Recorded separately: requires outcome review, never a false pass.
    if data['recipe_counts'].get('blocked-by-difference'):
        return 1
    return 0 if args.allow_incomplete or data['complete'] else 2


if __name__ == '__main__':
    sys.exit(main())
