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
