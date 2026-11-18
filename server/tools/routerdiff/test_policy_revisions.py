"""Dependency-bound revisions must expose broken hashes without Python emulation."""
import copy
import unittest

from cases import Case
from compare import differences
from inventory import load
from policy_revisions import PolicyRevisions
from run import execute
from test_runner import ObservationSide, pair


def state(settings=None, profile='alpha'):
    return {'database': {'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'],
        'rows': [['profile', profile, '_revision', 0, 1, 1],
                 ['profile', profile, 'oxidns', 1, {} if settings is None else settings, 1]]}}}


def view(token, profile='alpha'):
    return {'status': 200, 'kind': 'json', 'headers': {},
            'body': {'profile_id': profile, 'oxidns_revision': token, 'success': True, 'devices': []}}


def inputs(left=None, right=None):
    snapshots = {side: {'after': value} for side, value in
                 (('python', left or state()), ('go', right or state()))}
    observations = {'python': view('a' * 64), 'go': view('b' * 64)}
    return observations, snapshots


def workspace_state(router_id='r-ros'):
    policy = {'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': 'direct'}
    value = state({'device_policies': [policy]})
    value['database']['fields']['rows'].append(['profile', 'alpha', 'routers', 2,
        {'items': [{'id': router_id, 'name': '合成路由器🧪', 'password': 'synthetic-private-password'}]}, 1])
    return value


def workspace_view(native=False, router_id='r-ros'):
    tokens = ('d', 'e', 'f') if native else ('a', 'b', 'c')
    return {'status': 200, 'kind': 'json', 'headers': {}, 'body': {'success': True, 'workspace': {
        'profile_id': 'alpha', 'router': {'id': router_id, 'name': '合成路由器🧪'},
        'sources': {'policies': [{'ip': '10.44.0.21', 'name': '现存备注🧪', 'mode': 'direct'}]},
        'oxidns_revision': tokens[0] * 64, 'revision': tokens[1] * 64, 'names_revision': tokens[2] * 64}}}


class RevisionTests(unittest.TestCase):
    def setUp(self):
        self.case = Case('revision-control', 'R002', 'GET', '/api/client-devices', 200)

    def test_pairing_requires_equal_settings_and_entire_response_and_keeps_raw_refs(self):
        model = PolicyRevisions()
        observations, snapshots = inputs()
        original = copy.deepcopy(observations)
        paired, found = model.pair(self.case, observations, snapshots, observations)
        self.assertEqual(found, [])
        self.assertEqual(differences(paired['python'], paired['go']), [])
        self.assertEqual(observations, original)
        self.assertEqual(model.pair(self.case, observations, snapshots, observations), (paired, []))
        for kind in ('response', 'settings', 'profile'):
            observations, snapshots = inputs()
            if kind == 'response':
                observations['go']['body']['devices'] = [{'ip': '10.44.0.21', 'name': 'wrong'}]
            elif kind == 'settings':
                snapshots['go']['after']['database']['fields']['rows'][1][4] = {'opaque': 'wrong'}
            else:
                observations['go']['body']['profile_id'] = 'beta'
                snapshots['go']['after'] = state(profile='beta')
            paired, found = PolicyRevisions().pair(self.case, observations, snapshots, observations)
            self.assertEqual(found, [])
            self.assertTrue(differences(paired['python'], paired['go']), kind)
            self.assertEqual(paired, observations)

    def test_malformed_stale_changed_or_reused_native_hash_is_not_an_alias(self):
        for value in (None, False, 64, '', 'not-a-hash', 'A' * 64, 'b' * 63):
            observations, snapshots = inputs()
            observations['go']['body']['oxidns_revision'] = value
            _, found = PolicyRevisions().pair(self.case, observations, snapshots, observations)
            self.assertEqual(found[0].category, 'revision', value)
        for change in ('changed-same-settings', 'reused-different-settings'):
            model = PolicyRevisions()
            observations, snapshots = inputs()
            self.assertEqual(model.pair(self.case, observations, snapshots, observations)[1], [])
            if change == 'changed-same-settings':
                observations['go']['body']['oxidns_revision'] = 'c' * 64
            else:
                observations['python']['body']['oxidns_revision'] = 'c' * 64
                for side in snapshots:
                    snapshots[side]['after']['database']['fields']['rows'][1][4] = {'opaque': 'changed'}
            _, found = model.pair(self.case, observations, snapshots, observations)
            self.assertEqual(found[0].category, 'revision', change)
        observations, snapshots = inputs()
        observations['python']['body']['oxidns_revision'] = False
        with self.assertRaises(ValueError):
            PolicyRevisions().reference(self.case, observations['python'], snapshots['python']['after'])

    def test_head_and_options_have_no_observable_body_revision_to_bind(self):
        for method in ('HEAD', 'OPTIONS'):
            case = Case('bodyless-' + method, 'R002', method, '/api/client-devices', 200)
            model = PolicyRevisions()
            observations = {side: {'status': 200, 'kind': 'text', 'headers': {}, 'body': ''} for side in ('python', 'go')}
            snapshots = {side: {'after': state()} for side in ('python', 'go')}
            model.reference(case, observations['python'], snapshots['python']['after'])
            self.assertEqual(model.pair(case, observations, snapshots, observations), (observations, []))
            self.assertEqual(model.records, {'python': [], 'go': []})

    def test_dependency_excludes_notes_bindings_router_and_physical_revision(self):
        model = PolicyRevisions()
        observations, snapshots = inputs()
        model.pair(self.case, observations, snapshots, observations)
        for side in snapshots:
            table = snapshots[side]['after']['database']['fields']
            table['rows'][0][4:] = [2, 2]
            table['rows'][1][5] = 2
            table['rows'].append(['profile', 'alpha', 'device_notes', 2, {'10.44.0.21': '改名🧪'}, 2])
        observations['python']['body']['devices'] = observations['go']['body']['devices'] = [{'ip': '10.44.0.21', 'name': '改名🧪'}]
        _, found = model.pair(self.case, observations, snapshots, observations)
        self.assertEqual(found, [])
        self.assertEqual(len(model.records['python']), 1)
        # Actual effective profile from a pinned Agent response, not necessarily
        # the request/header Case.profile, scopes its own settings dependency.
        observations, snapshots = inputs(state(profile='beta'), state(profile='beta'))
        for observation in observations.values():
            observation['body']['profile_id'] = 'beta'
        _, found = model.pair(self.case, observations, snapshots, observations)
        self.assertEqual(found, [])
        self.assertEqual(len(model.records['python']), 2)

    def test_own_content_provenance_preserves_signed_zero_and_changed_settings(self):
        model = PolicyRevisions()
        observations, snapshots = inputs(state({'opaque': -0.0}), state({'opaque': -0.0}))
        model.pair(self.case, observations, snapshots, observations)
        for side, token in (('python', 'c'), ('go', 'd')):
            snapshots[side]['after'] = state({'opaque': 0.0})
            observations[side]['body']['oxidns_revision'] = token * 64
        self.assertEqual(model.pair(self.case, observations, snapshots, observations)[1], [])
        self.assertEqual(len(model.records['python']), 2)

    def test_workspace_hashes_pair_together_with_distinct_source_dependencies(self):
        case = Case('workspace', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200)
        model = PolicyRevisions()
        observations = {side: workspace_view(side == 'go') for side in ('python', 'go')}
        snapshots = {side: {'after': workspace_state()} for side in ('python', 'go')}
        raw = copy.deepcopy(observations)
        paired, found = model.pair(case, observations, snapshots, observations)
        self.assertEqual(found, [])
        self.assertEqual(differences(paired['python'], paired['go']), [])
        self.assertEqual(len(model.aliases), 3)
        self.assertEqual(observations, raw)
        # A shared client read must expose the SAME own settings token as the
        # workspace; the API path is not a different hash namespace.
        clients = {side: view(('d' if side == 'go' else 'a') * 64) for side in ('python', 'go')}
        self.assertEqual(model.pair(self.case, clients, snapshots, clients)[1], [])
        self.assertEqual(len(model.records['python']), 3)
        # Name-only edits change only the names dependency. Profile counters,
        # legacy names saved inside OxiDNS and router config stay untouched.
        for side, letter in (('python', '0'), ('go', '1')):
            observations[side]['body']['workspace']['sources']['policies'][0]['name'] = '新备注🧪'
            observations[side]['body']['workspace']['names_revision'] = letter * 64
            snapshots[side]['after']['database']['fields']['rows'].append(
                ['profile', 'alpha', 'device_notes', 3, {'10.44.0.21': '新备注🧪'}, 2])
        paired, found = model.pair(case, observations, snapshots, observations)
        self.assertEqual(found, [])
        self.assertEqual(differences(paired['python'], paired['go']), [])
        self.assertEqual(len(model.records['python']), 4)
        # Changing only the selected router invalidates workspace revision,
        # with independent OxiDNS/names tokens retained.
        for side, letter in (('python', '2'), ('go', '3')):
            snapshots[side]['after']['database']['fields']['rows'][2][4]['items'][0]['name'] = '改路由器名字🧪'
            observations[side]['body']['workspace']['router']['name'] = '改路由器名字🧪'
            observations[side]['body']['workspace']['revision'] = letter * 64
        self.assertEqual(model.pair(case, observations, snapshots, observations)[1], [])
        self.assertEqual(len(model.records['python']), 5)

    def test_workspace_revision_excludes_probe_and_uses_independently_projected_ids(self):
        case = Case('workspace', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200)
        model = PolicyRevisions()
        observations = {side: workspace_view(side == 'go', side + '-router') for side in ('python', 'go')}
        snapshots = {side: {'after': workspace_state(side + '-router')} for side in ('python', 'go')}
        copies = copy.deepcopy(observations)
        states = {side: copy.deepcopy(value['after']) for side, value in snapshots.items()}
        for side in states:
            copies[side]['body']['workspace']['router']['id'] = '<independently proven resource identity>'
            states[side]['database']['fields']['rows'][2][4]['items'][0]['id'] = '<independently proven resource identity>'
        paired, found = model.pair(case, observations, snapshots, copies, states)
        self.assertEqual(found, [])
        self.assertEqual(differences(paired['python'], paired['go']), [])
        for side in snapshots:
            snapshots[side]['after']['database']['fields']['rows'][2][4]['items'][0]['last_probe'] = {
                'ok': True, 'at': side + '-real-request-clock'}
        # Probe is excluded from this token's dependency. It still has its own
        # raw storage/public clock contract in ProbeClocks, never masked here.
        self.assertEqual(model.pair(case, observations, snapshots, copies, states)[1], [])
        self.assertEqual(len(model.records['python']), 3)
        self.assertNotEqual(observations['python']['body']['workspace']['router']['id'],
                            observations['go']['body']['workspace']['router']['id'])

    def test_workspace_wrong_hash_owner_names_or_stale_token_never_get_paired(self):
        case = Case('workspace', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200)
        for wrong in ('name-token', 'missing-token', 'profile', 'router', 'policy-row', 'private-setting'):
            observations = {side: workspace_view(side == 'go') for side in ('python', 'go')}
            snapshots = {side: {'after': workspace_state()} for side in ('python', 'go')}
            native = observations['go']['body']['workspace']
            if wrong == 'name-token':
                native['names_revision'] = False
            elif wrong == 'missing-token':
                del native['revision']
            elif wrong == 'profile':
                native['profile_id'] = 'missing-profile'
            elif wrong == 'router':
                native['router']['id'] = 'missing-router'
            elif wrong == 'policy-row':
                native['sources']['policies'][0]['name'] = 1
            else:
                snapshots['go']['after']['database']['fields']['rows'][2][4]['items'][0]['password'] = 'corrupted'
            paired, found = PolicyRevisions().pair(case, observations, snapshots, observations)
            self.assertTrue(found or differences(paired['python'], paired['go']), wrong)
        for changed in ('names', 'workspace', 'oxidns'):
            model = PolicyRevisions()
            observations = {side: workspace_view(side == 'go') for side in ('python', 'go')}
            snapshots = {side: {'after': workspace_state()} for side in ('python', 'go')}
            model.pair(case, observations, snapshots, observations)
            path = {'names': 'names_revision', 'workspace': 'revision', 'oxidns': 'oxidns_revision'}[changed]
            observations['go']['body']['workspace'][path] = '9' * 64
            _, found = model.pair(case, observations, snapshots, observations)
            self.assertEqual(found[0].category, 'revision', changed)

    def test_execute_workspace_keeps_each_sides_real_optimistic_tokens(self):
        class WorkspaceSide(ObservationSide):
            def __init__(self, native=False, break_token=False):
                super().__init__()
                self.native, self.break_token = native, break_token
                self.before['database']['fields'] = workspace_state()['database']['fields']
                self.before['database']['sections'] = {'columns': ['scope', 'owner'], 'rows': [['profile', 'alpha']]}
                self.after = copy.deepcopy(self.before)

            def request(self, **values):
                expected = workspace_view(self.native)['body']
                if values['method'] == 'PUT':
                    for key in ('revision', 'names_revision'):
                        if values['document'][key] != expected['workspace'][key]:
                            raise AssertionError('comparison projection leaked into the own HTTP write')
                    self.after = copy.deepcopy(self.after)
                    self.after['database']['fields']['rows'][0][4:] = [2, 2]
                    if self.break_token:
                        expected['workspace']['revision'] = '9' * 64
                self.content = expected
                return super().request(**values)

        from cases import Ref
        routes, _ = load()
        available = {('GET', '/api/routers/{}/policy-settings'), ('PUT', '/api/routers/{}/policy-settings')}
        read = Case('workspace-read', 'R014', 'GET', '/api/routers/r-ros/policy-settings', 200)
        write = Case('workspace-own-write', 'R015', 'PUT', read.path, 200, requires=('R014',),
                     document={'revision': Ref(read.ident, '/body/workspace/revision'),
                               'names_revision': Ref(read.ident, '/body/workspace/names_revision')},
                     unchanged=False, effect='profile-write')
        for broken in (False, True):
            world = pair(WorkspaceSide(), WorkspaceSide(True, broken))
            world.policy_revisions = PolicyRevisions()
            self.assertEqual(execute(world, read, available, routes)['status'], 'compared')
            result = execute(world, write, available, routes)
            self.assertEqual(result['status'], 'different' if broken else 'compared')
            self.assertEqual(world.python.requests[1]['document']['revision'], 'b' * 64)
            self.assertEqual(world.go.requests[1]['document']['revision'], 'e' * 64)
            self.assertEqual(world.go.requests[1]['document']['names_revision'], 'f' * 64)
            if broken:
                self.assertIn('revision', {item['category'] for item in result['differences']})

    def test_execute_pairs_different_native_hash_encoding_but_retains_own_body_references(self):
        class PolicySide(ObservationSide):
            def __init__(self, native=False, break_hash=False):
                super().__init__()
                self.before['database']['fields'] = state()['database']['fields']
                self.after = self.before
                self.native, self.break_hash = native, break_hash

            def request(self, **values):
                token = ('c' if self.requests and self.break_hash else 'b') if self.native else 'a'
                self.content = view(token * 64)['body']
                return super().request(**values)

        for broken in (False, True):
            world = pair(PolicySide(), PolicySide(True, broken))
            world.policy_revisions = PolicyRevisions()
            available, routes = {('GET', self.case.path)}, load()[0]
            result = execute(world, self.case, available, routes)
            self.assertEqual(result['status'], 'compared')
            self.assertEqual(result['go']['body']['oxidns_revision'], 'b' * 64)
            self.assertEqual(world.go.observations[self.case.ident]['body']['oxidns_revision'], 'b' * 64)
            following = execute(world, Case('later', 'R002', 'GET', self.case.path, 200), available, routes)
            self.assertEqual(following['status'], 'different' if broken else 'compared')
            if broken:
                self.assertIn('revision', {item['category'] for item in following['differences']})
        # Go absence cannot excuse a broken reference revision contract.
        world = pair(PolicySide(break_hash=True), PolicySide(True))
        world.python.content = None
        world.policy_revisions = PolicyRevisions()
        execute(world, self.case, set(), load()[0])
        world.python.native = True  # Purpose-broken observation only, not the real reference.
        with self.assertRaises(ValueError):
            execute(world, Case('reference-broken', 'R002', 'GET', self.case.path, 200), set(), load()[0])


if __name__ == '__main__':
    unittest.main()
