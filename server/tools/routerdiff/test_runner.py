"""Runner control-flow self-checks, not substitutes for HTTP product recipes."""
import base64
import copy
import json
import time
from types import SimpleNamespace
import unittest

from cases import Case, Ref
from harness import RemoteError
from inventory import load
from run import execute, poll, shape, wait_guard, registered_request


class ObservationSide:
    def __init__(self, status=200, before=None, after=None, content=None):
        self.observations, self.pending, self.missing_effects, self.requests = {}, {}, [], []
        self.status, self.snapshots = status, 0
        self.content = content
        self.request_windows = {}
        self.before = before or {'database': {'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'],
            'rows': []}}, 'files': {}, 'fake': {'ros': {}, 'ikuai': {'static_rt': []}, 'agent': {}, 'transcript': [], 'errors': []}}
        self.after = after or self.before

    def rpc(self, operation, **values):
        if operation != 'snapshot':
            raise AssertionError('runner self-check unexpectedly calls ' + operation)
        value = self.before if self.snapshots == 0 else self.after
        self.snapshots += 1
        return copy.deepcopy(value)

    def request(self, **values):
        self.requests.append(values)
        content = self.content if self.content is not None else {'success': self.status < 400}
        if values.get('method') == 'HEAD':
            content = ''
            raw = b''
            kind = 'text'
        else:
            raw = json.dumps(content).encode()
            kind = 'json'
        return {'status': self.status, 'headers': [('Content-Type', 'application/json'), ('Content-Length', str(len(raw)))],
                'body': base64.b64encode(raw).decode(), 'content': content, 'kind': kind}


def pair(python=None, go=None):
    sides = {'python': python or ObservationSide(), 'go': go or ObservationSide()}
    return SimpleNamespace(sides=sides, **sides)


class RunnerTests(unittest.TestCase):
    def test_concrete_manifest_requirement_accepts_an_actual_generic_handler(self):
        routes, _ = load()
        case = Case('generic-prerequisite', 'R002', 'GET', '/api/client-devices', 200,
                    manifest_requires=(('POST', '/api/custom-config/mihomo'),))
        available = {('GET', shape(routes['R002'].path)), ('POST', '/api/custom-config/{}')}
        world = pair()
        self.assertEqual(execute(world, case, available, routes)['status'], 'compared')
        self.assertEqual(len(world.go.requests), 1)
        for missing in ({('GET', shape(routes['R002'].path)), ('GET', '/api/custom-config/{}')},
                        {('GET', shape(routes['R002'].path)), ('POST', '/api/custom-config/surge')}):
            world = pair()
            result = execute(world, case, missing, routes)
            self.assertEqual(result['status'], 'go-unimplemented')
            self.assertEqual(world.go.requests, [])

    def test_actual_create_comparison_uses_scoped_storage_bound_ids_and_retains_raw_references(self):
        def created_side(ident, name='合成新路由🧪', wrong_stored_id=False):
            before = ObservationSide().before
            before['database']['sections'] = {'columns': ['scope', 'owner'], 'rows': []}
            before['database']['fields']['rows'] = [
                ['profile', 'alpha', '_revision', 0, 1, 1],
                ['profile', 'alpha', 'routers', 1, {'items': [{'id': 'seed'}]}, 1]]
            after = copy.deepcopy(before)
            after['database']['fields']['rows'][0][4:] = [2, 2]
            after['database']['fields']['rows'][1][5] = 2
            router = {'id': ident, 'name': name}
            after['database']['fields']['rows'][1][4]['items'].append({**router,
                **({'id': 'wrong-store'} if wrong_stored_id else {})})
            return ObservationSide(200, before, after, {'success': True, 'router': router})
        case = Case('create', 'R008', 'POST', '/api/routers', 200,
                    unchanged=False, effect='router-create')
        world = pair(created_side('router_000000000001'), created_side('native-id-1'))
        result = execute(world, case, {('POST', '/api/routers')}, load()[0])
        self.assertEqual(result['status'], 'compared')
        self.assertEqual(result['python']['body']['router']['id'], 'router_000000000001')
        self.assertEqual(result['go']['body']['router']['id'], 'native-id-1')
        self.assertEqual(world.go.observations['create']['body']['router']['id'], 'native-id-1')
        for wrong_name, wrong_id in (('错误名称', False), ('合成新路由🧪', True)):
            world = pair(created_side('router_000000000001'), created_side('native-id-1', wrong_name, wrong_id))
            result = execute(world, case, {('POST', '/api/routers')}, load()[0])
            self.assertEqual(result['status'], 'different')
            self.assertTrue(result['differences'])
            if wrong_id:
                self.assertIn('/safety/generated-router-identity', {row['path'] for row in result['differences']})

    def test_retained_profile_fields_reject_note_only_dns_rewrite(self):
        routes, _ = load()
        before = ObservationSide().before
        before['database']['fields']['rows'] = [
            ['profile', 'alpha', 'oxidns', 0, json.dumps({'native_config': 'synthetic original'}), 1]]
        case = Case('note-only', 'R003', 'PUT', '/api/client-devices', 200,
                    unchanged=False, retained_fields=('oxidns',))
        self.assertEqual(execute(pair(ObservationSide(200, before, before)), case, set(), routes)['status'],
                         'go-unimplemented')
        changed = copy.deepcopy(before)
        changed['database']['fields']['rows'][0][4] = json.dumps({'native_config': 'unexpected rewrite'})
        with self.assertRaisesRegex(RuntimeError, 'unrelated profile field changed at oxidns'):
            execute(pair(ObservationSide(200, before, changed)), case, set(), routes)

    def test_auxiliary_http_failure_is_observed_and_blocks_dependent_mutations(self):
        routes, _ = load()
        class SetupSide(ObservationSide):
            def request(self, **values):
                if values['path'] == '/api/routers/r-ros':
                    return super().request(**values)
                # If an incorrect Go setup is accepted, reaching this main
                # request makes the test fail rather than inventing its result.
                if self.status != 200:
                    raise AssertionError('main HTTP ran after a failed Go prerequisite')
                return super().request(**values)
        for status, content in ((502, {'success': False, 'message': 'real synthetic refusal'}),
                                (200, {'success': False}), (500, 'synthetic HTML error')):
            world = pair(SetupSide(), SetupSide(status=status, content=content))
            case = Case('setup-failure', 'R004', 'GET', '/api/client-devices/discovery', 200,
                        prepare=({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ros',
                                  'document': {'name': 'synthetic change'}, 'expected': 200},))
            available = {('GET', shape(routes['R004'].path)), ('PUT', '/api/routers/{}')}
            result = execute(world, case, available, routes)
            self.assertEqual(result['status'], 'blocked-by-difference')
            self.assertEqual(result['setup_observations']['go'][0]['response']['status'], status)
            self.assertTrue(result['differences'])
            self.assertIsNone(result['go'])
            follow = Case('later', 'R004', 'GET', '/api/client-devices/discovery', 200)
            self.assertEqual(execute(world, follow, available, routes)['status'], 'blocked-by-difference')
            self.assertEqual(len(world.go.requests), 1)

    def test_missing_auxiliary_route_is_classified_from_the_public_manifest(self):
        routes, _ = load()
        world = pair()
        case = Case('aux-not-ported', 'R004', 'GET', '/api/client-devices/discovery', 200,
                    prepare=({'rpc': 'http', 'method': 'PUT', 'path': '/api/routers/r-ros',
                              'document': {'name': 'synthetic'}, 'expected': 200},))
        result = execute(world, case, {('GET', shape(routes['R004'].path))}, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertIn('PUT /api/routers/r-ros', result['unimplemented_reason']['missing_routes'])
        self.assertEqual(world.go.requests, [])
        available = {('POST', '/api/agents/{}/heartbeat')}
        self.assertTrue(registered_request('POST', '/api/agents/alpha/heartbeat?profile=beta', available))
        self.assertFalse(registered_request('GET', '/api/agents/alpha/heartbeat', available))
        self.assertFalse(registered_request('POST', '/api/agents/alpha/nested/heartbeat', available))

    def test_native_startup_exit_is_a_process_difference_without_invented_http(self):
        world = pair()
        world.go_startup_failure = {'exit_code': 7, 'stage': 'public native process startup'}
        case = Case('startup', 'R002', 'GET', '/api/client-devices', 200)
        result = execute(world, case, set(), load()[0])
        self.assertEqual(result['status'], 'different')
        self.assertEqual(result['go_process']['exit_code'], 7)
        self.assertIsNone(result['go'])
        self.assertEqual(world.go.requests, [])
        self.assertEqual(len(world.python.requests), 1)
        self.assertEqual(result['differences'][0]['category'], 'process')

    def test_real_exit_during_restart_retains_the_process_observation(self):
        class RestartSide(ObservationSide):
            def __init__(self, exit_code=None):
                super().__init__()
                self.exit_code = exit_code

            def start(self):
                if self.exit_code is not None:
                    raise RemoteError('go', 'start', 'child exited', '')

            def rpc(self, operation, **values):
                if operation == 'stop':
                    return 0
                if operation == 'process_status':
                    return {'exit_code': self.exit_code}
                return super().rpc(operation, **values)
        world = pair(RestartSide(), RestartSide(7))
        case = Case('restart', 'R004', 'GET', '/api/client-devices/discovery', 200,
                    prepare=({'rpc': 'restart'},))
        result = execute(world, case, {('GET', '/api/client-devices/discovery')}, load()[0])
        self.assertEqual(result['status'], 'different')
        self.assertEqual(result['go_process'], {'exit_code': 7, 'stage': 'public native process restart'})
        self.assertIsNone(result['go'])
        self.assertEqual(world.go.requests, [])

    def test_native_policy_evidence_cannot_pass_without_both_plugins_and_hook(self):
        routes, _ = load()
        before = ObservationSide().before
        required = ('cf_device_policy', 'cf_device_direct', 'jump cf_device_policy')
        case = Case('native-proof', 'R003', 'PUT', '/api/client-devices', 200,
                    unchanged=False, stored_contains={'oxidns/native_config': required})
        def observation(text):
            after = copy.deepcopy(before)
            after['database']['fields']['rows'] = [['profile', 'alpha', 'oxidns', 0,
                json.dumps({'native_config': text}), 1]]
            return pair(ObservationSide(200, before, after))
        self.assertEqual(execute(observation(' '.join(required)), case, set(), routes)['status'], 'go-unimplemented')
        for text in (' '.join(required[:-1]), '', None, {'cf_device_policy': True}):
            with self.assertRaisesRegex(RuntimeError, 'stored text lacks required evidence'):
                execute(observation(text), case, set(), routes)

    def test_withdrawn_native_policy_markers_cannot_be_left_in_storage(self):
        routes, _ = load()
        before = ObservationSide().before
        case = Case('native-withdraw-proof', 'R003', 'PUT', '/api/client-devices', 200,
                    unchanged=False, stored_excludes={'oxidns/native_config': ('cf_device_policy', 'cf_device_direct')})
        def observation(text):
            after = copy.deepcopy(before)
            after['database']['fields']['rows'] = [['profile', 'alpha', 'oxidns', 0,
                json.dumps({'native_config': text}), 1]]
            return pair(ObservationSide(200, before, after))
        self.assertEqual(execute(observation('sequence_main'), case, set(), routes)['status'], 'go-unimplemented')
        for text in ('cf_device_policy', 'cf_device_direct', None, {'sequence_main': True}):
            with self.assertRaisesRegex(RuntimeError, 'stale stored text evidence remains'):
                execute(observation(text), case, set(), routes)

    def test_matching_native_protocol_false_reference_is_never_a_parity_pass(self):
        routes, _ = load()
        broken = {'success': True, 'editable': False, 'side_entries': [], 'reason': 'false reference'}
        fixed = {'success': True, 'editable': True, 'reason': '', 'side_entries': [
            {'tags': ['side_udp'], 'listen': '127.0.0.1:5353', 'entry': 'side'}]}
        case = Case('protocol-collision', 'R002', 'GET', '/api/client-devices', 200,
                    python_bug='NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION')
        for content, expected in ((broken, 'different'), (fixed, 'reference-bug-observation')):
            world = pair(ObservationSide(content=broken), ObservationSide(content=content))
            result = execute(world, case, {('GET', '/api/client-devices')}, routes)
            self.assertEqual(result['status'], expected)

    def test_external_generator_uses_its_own_manifest_without_claiming_a_router_route(self):
        routes, _ = load()
        case = Case('G23/external', '', 'POST', '/api/generate/mihomo/preview', 200, ('G23',),
                    document={}, boundary=('POST', '/api/generate/mihomo/preview'), owner='R02/config generator')
        world = pair(ObservationSide(), ObservationSide(status=500))
        actual = {('POST', '/api/generate/{}/preview')}
        result = execute(world, case, actual, routes)
        self.assertEqual(result['status'], 'different')
        self.assertEqual(result['scope'], 'cross-domain')
        self.assertEqual(result['route'], '')
        self.assertEqual(result['owner'], 'R02/config generator')
        self.assertEqual(len(world.go.requests), 1)
        missing = pair()
        result = execute(missing, case, {('POST', '/api/routers/{}/plan')}, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertEqual(missing.go.requests, [])
        contradictory = Case('G23/bad-meta', 'R012', 'POST', case.path, 200, boundary=case.boundary)
        with self.assertRaisesRegex(ValueError, 'external boundary must declare'):
            execute(pair(), contradictory, actual, routes)

    def test_notification_prerequisites_cannot_pass_missing_or_stale_evidence(self):
        routes, _ = load()
        before = ObservationSide().before
        def observed(items):
            after = copy.deepcopy(before)
            after['files']['notifications.json'] = base64.b64encode(json.dumps({'items': items}).encode()).decode()
            return pair(ObservationSide(200, before, after))
        pending = Case('HEAD-notification-proof', 'R018', 'GET', '/api/routers/test/report', 200,
                       unchanged=False, effect='device-read',
                       file_checks={'notifications.json': {'items/check/state': 'pending'}})
        self.assertEqual(execute(observed({'check': {'state': 'pending'}}), pending, set(), routes)['status'], 'go-unimplemented')
        with self.assertRaisesRegex(RuntimeError, 'file postcondition failed'):
            execute(observed({'check': {'state': 'resolved'}}), pending, set(), routes)
        recovered = Case('notification-clear-proof', 'R018', 'GET', '/api/routers/test/report', 200,
                         unchanged=False, effect='device-read',
                         file_checks={'notifications.json': {'items/other/state': 'pending'}},
                         file_absent={'notifications.json': ('items/check',)})
        self.assertEqual(execute(observed({'other': {'state': 'pending'}}), recovered, set(), routes)['status'], 'go-unimplemented')
        with self.assertRaisesRegex(RuntimeError, 'stale generated JSON member remains'):
            execute(observed({'other': {'state': 'pending'}, 'check': {'state': 'pending'}}), recovered, set(), routes)

    def test_matching_HEAD_mutations_never_pass_as_parity(self):
        routes, _ = load()
        for route, bug in [('R007', 'HEAD_CREATES_ROUTER'), ('R025', 'HEAD_PERSISTS_FORWARDING')]:
            before = ObservationSide().before
            after = copy.deepcopy(before)
            if route == 'R007':
                after['database']['fields']['rows'] = [['profile', 'alpha', 'routers', 0, '{"items":[{"id":"new"}]}', 1]]
            else:
                after['fake']['agent'] = {'persisted': True}
            case = Case('unsafe-HEAD', route, 'HEAD', routes[route].path, 200, unchanged=False, python_bug=bug)
            available = {('GET', shape(routes[route].path))}
            copied = pair(ObservationSide(200, before, after), ObservationSide(200, before, after))
            self.assertEqual(execute(copied, case, available, routes)['status'], 'different')
            safe = copy.deepcopy(before)
            if route == 'R025':
                safe['fake']['transcript'] = [{'peer': 'agent', 'method': 'GET', 'path': '/api/linux-gateway/status',
                    'headers': {}, 'body': None, 'raw': ''} for _ in range(2)]
            fixed = pair(ObservationSide(200, before, after), ObservationSide(200, before, safe))
            self.assertEqual(execute(fixed, case, available, routes)['status'], 'reference-bug-observation')

    def test_guard_wait_never_retries_an_accepted_write(self):
        side = ObservationSide(status=200)
        self.assertEqual(wait_guard(side, '/api/routers/test', 0, strict=False)['status'], 200)
        self.assertEqual(len(side.requests), 1)
        self.assertEqual(side.requests[0]['document'], [])
        with self.assertRaisesRegex(RuntimeError, 'guard did not settle'):
            wait_guard(ObservationSide(status=200), '/api/routers/test', 0)
        self.assertEqual(wait_guard(ObservationSide(status=400), '/api/routers/test', 0)['status'], 400)

    def test_missing_go_preparation_reference_never_uses_python_value(self):
        class FlowSide(ObservationSide):
            token = None

            def rpc(self, operation, **values):
                if operation == 'open_request':
                    return {'sent': values['ident']}
                return super().rpc(operation, **values)
        routes, _ = load()
        world = pair(FlowSide(), FlowSide())
        world.python.observations['preview'] = {'body': {'token': 'python-only-token'}}
        case = Case('flow', 'R001', 'GET', '/api/agents/test/upstream-routers', 200,
                    prepare=({'rpc': 'open_request', 'ident': 'owned', 'method': 'POST', 'path': '/api/test',
                              'document': {'token': Ref('preview', '/body/token')}},))
        result = execute(world, case, {('GET', shape(routes['R001'].path)), ('POST', '/api/test')}, routes)
        self.assertEqual(result['status'], 'blocked-by-difference')
        self.assertEqual(world.go.requests, [])
        self.assertEqual(result['request']['preceding_abandoned_requests'][0]['document']['token'], 'python-only-token')

    def test_matching_reference_metadata_crash_is_not_a_pass(self):
        routes, _ = load()
        healthy = {'success': True, 'gateways': [{'agent_id': 'm-a', 'state': 'needs_persist', 'forward_filter': None},
                                                {'agent_id': 'm-b', 'state': 'needs_persist'}]}
        for status, content, expected in [(500, None, 'different'),
                (200, {'success': True, 'gateways': [None, None]}, 'different'),
                (200, healthy, 'reference-bug-observation')]:
            with self.subTest(status=status, expected=expected):
                world = pair(ObservationSide(status=500), ObservationSide(status=status, content=content))
                case = Case('bad-Agent-metadata', 'R025', 'GET', '/api/routers/gateway-forwarding', 500,
                            python_bug='AGENT_FORWARD_FILTER_UNHASHABLE')
                result = execute(world, case, {('GET', shape(routes['R025'].path))}, routes)
                self.assertEqual(result['status'], expected)

    def test_age_boundaries_use_own_side_windows_and_fail_closed_when_missed(self):
        routes, _ = load()
        for missing in (False, True):
            world = pair()
            now = time.monotonic()
            world.python.request_windows['preview'] = {'started': now, 'ended': now}
            if not missing:
                world.go.request_windows['preview'] = {'started': now - 20, 'ended': now - 20}
            case = Case('age-boundary', 'R001', 'GET', '/api/agents/test/upstream-routers', 200,
                        age_from='preview', age_seconds=0, age_limit=10)
            reason = 'missing own-side request window' if missing else 'fixture window missed'
            with self.assertRaisesRegex(RuntimeError, reason):
                execute(world, case, {('GET', shape(routes['R001'].path))}, routes)
            self.assertEqual(len(world.python.requests), 1)
            self.assertEqual(world.go.requests, [])

    def test_go_poll_failure_stays_a_compared_observation(self):
        failed = ObservationSide(status=502)
        response = poll(failed, {'method': 'GET', 'path': '/api/routers/test/rule-sync'}, {'success': True}, 0, strict=False)
        self.assertEqual(response['status'], 502)
        with self.assertRaisesRegex(RuntimeError, 'steady-state wait expired'):
            poll(failed, {'method': 'GET', 'path': '/api/routers/test/rule-sync'}, {'success': True}, 0)
        with self.assertRaisesRegex(ValueError, 'only repeat public GETs'):
            poll(failed, {'method': 'POST', 'path': '/api/routers/test/rule-sync'}, {}, 0)

    def test_transaction_order_precondition_rejects_forward_undo(self):
        original = ObservationSide().before
        for actions, valid in ((['add-one', 'add-two', 'undo-two', 'undo-one'], True),
                               (['add-one', 'add-two', 'undo-one', 'undo-two'], False)):
            with self.subTest(actions=actions):
                after = copy.deepcopy(original)
                after['fake']['transcript'] = [{'method': 'POST', 'path': action, 'body': {}, 'headers': {}, 'raw': ''}
                                              for action in actions]
                world = pair(ObservationSide(200, original, after))
                case = Case('rollback-order', 'R005', 'POST', '/api/client-devices/fixed-ip/apply', 200,
                            unchanged=False, request_order=tuple({'path': action} for action in
                            ('add-one', 'add-two', 'undo-two', 'undo-one')))
                if valid:
                    self.assertEqual(execute(world, case, set(), load()[0])['status'], 'go-unimplemented')
                else:
                    with self.assertRaisesRegex(RuntimeError, 'transaction order mismatch'):
                        execute(world, case, set(), load()[0])

    def test_missing_go_response_reference_never_falls_back_to_python(self):
        routes, _ = load()
        world = pair()
        world.python.observations['previous'] = {'body': {'token': 'python-only'}}
        case = Case('dependent', 'R001', 'GET', '/api/agents/test/upstream-routers', 200,
                    document={'token': Ref('previous', '/body/token')})
        result = execute(world, case, {('GET', shape(routes['R001'].path))}, routes)
        self.assertEqual(result['status'], 'blocked-by-difference')
        self.assertEqual(world.python.requests[0]['document']['token'], 'python-only')
        self.assertEqual(world.go.requests, [])

    def test_go_protocol_violation_is_a_difference_not_a_fixture_abort(self):
        routes, _ = load()
        before = ObservationSide().before
        broken = copy.deepcopy(before)
        broken['fake']['errors'].append('synthetic malformed Go device write')
        world = pair(ObservationSide(200, before, before), ObservationSide(502, before, broken))
        case = Case('device-protocol-violation', 'R020', 'POST', '/api/routers/test/routes/apply', 200,
                    unchanged=False, effect='device-only')
        result = execute(world, case, {('POST', shape(routes['R020'].path))}, routes)
        self.assertEqual(result['status'], 'different')
        self.assertTrue(any(item['path'].startswith('/state/fake/errors') for item in result['differences']))
        with self.assertRaisesRegex(RuntimeError, 'fake contract error'):
            execute(pair(ObservationSide(200, before, broken)), case, set(), routes)

    def test_missing_mutating_setup_blocks_later_reads_even_when_main_request_is_GET(self):
        routes, _ = load()
        world = pair()
        case = Case('read-after-missing-save', 'R001', 'GET', '/api/agents/test/upstream-routers', 200,
                    prepare=({'rpc': 'http', 'method': 'POST', 'path': '/api/custom-config/mihomo',
                              'document': {}, 'expected': 200},), effect='device-read', unchanged=False,
                    manifest_requires=(('POST', '/api/custom-config/mihomo'),))
        available = {('GET', shape(routes['R001'].path)), ('GET', shape(routes['R007'].path))}
        self.assertEqual(execute(world, case, available, routes)['status'], 'go-unimplemented')
        later = Case('later', 'R007', 'GET', '/api/routers', 200)
        result = execute(world, later, available, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertEqual(result['unimplemented_reason']['prior_missing_mutations'], ['read-after-missing-save'])
        self.assertEqual(world.go.requests, [])

    def test_missing_external_overlap_handler_is_not_a_business_404_oracle(self):
        routes, _ = load()
        world = pair(ObservationSide(status=404), ObservationSide(status=404))
        case = Case('external-overlap', 'R001', 'GET', '/api/agents/i/upstream-routers', 404,
                    manifest_requires=(('GET', '/api/agents/i/<token>'),))
        result = execute(world, case, {('GET', shape(routes['R001'].path))}, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertEqual(world.go.requests, [])
        self.assertIn('GET /api/agents/i/<token>', result['unimplemented_reason']['missing_routes'])

    def test_unimplemented_write_cannot_poison_a_later_implemented_read(self):
        routes, _ = load()
        world = pair()
        available = {('GET', shape(routes['R007'].path))}
        write = Case('missing-write', 'R020', 'POST', '/api/routers/test/routes/apply', 200,
                     unchanged=False, effect='device-only')
        self.assertEqual(execute(world, write, available, routes)['status'], 'go-unimplemented')
        read = Case('later-read', 'R007', 'GET', '/api/routers', 200)
        result = execute(world, read, available, routes)
        self.assertEqual(result['status'], 'go-unimplemented')
        self.assertEqual(result['unimplemented_reason']['prior_missing_mutations'], ['missing-write'])
        self.assertEqual(world.go.requests, [])

    def test_reproducing_python_residue_is_a_go_safety_failure(self):
        routes, _ = load()
        original = ObservationSide().before
        residue = copy.deepcopy(original)
        residue['fake']['ikuai']['static_rt'] = [{'id': 1, 'dst_addr': '198.18.0.0'}]
        world = pair(ObservationSide(502, original, residue), ObservationSide(502, original, residue))
        case = Case('suffix-bug', 'R011', 'POST', '/api/routers/test/apply', 502,
                    unchanged=False, effect='device-only', python_bug='IKUAI_NETMASK_SUFFIX_ROLLBACK')
        result = execute(world, case, {('POST', shape(routes['R011'].path))}, routes)
        self.assertEqual(result['status'], 'different')
        self.assertTrue(any(item['category'] == 'safety' for item in result['differences']))

    def test_clean_failure_on_python_bug_is_recorded_for_review_not_passed(self):
        routes, _ = load()
        original = ObservationSide().before
        residue = copy.deepcopy(original)
        residue['fake']['ikuai']['static_rt'] = [{'id': 1, 'dst_addr': '198.18.0.0'}]
        world = pair(ObservationSide(502, original, residue), ObservationSide(502, original, original))
        case = Case('suffix-clean', 'R011', 'POST', '/api/routers/test/apply', 502,
                    unchanged=False, effect='device-only', python_bug='IKUAI_NETMASK_SUFFIX_ROLLBACK')
        result = execute(world, case, {('POST', shape(routes['R011'].path))}, routes)
        self.assertEqual(result['status'], 'reference-bug-observation')
        self.assertEqual(result['go_bug_outcome']['created_static_rows_remaining'], 0)
        self.assertTrue(result['differences'])


if __name__ == '__main__':
    unittest.main()
