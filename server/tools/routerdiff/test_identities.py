"""Wrong identities, payloads and arbitrary ID-like strings must remain visible."""
import copy
import unittest

from compare import differences
from identities import RouterIDs


def snapshot(items, profile='alpha'):
    return {'database': {'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'],
            'rows': [['profile', profile, 'routers', 0, {'items': copy.deepcopy(items)}, 1]]}},
            'fake': {'ros': {}, 'transcript': []}, 'files': {}}


def creation(left='router_000000000001', right='942c1129-5413-425c-a119-6a1c9dc3fbeb'):
    views, states = {}, {}
    for side, ident in (('python', left), ('go', right)):
        router = {'id': ident, 'name': '合成新路由🧪'}
        views[side] = {'status': 200, 'body': {'success': True, 'router': router}}
        states[side] = {'before': snapshot([{'id': 'seed', 'name': '原路由'}]),
                        'after': snapshot([{'id': 'seed', 'name': '原路由'}, router])}
    return views, states


class IdentityTests(unittest.TestCase):
    def test_only_actual_fresh_persisted_pair_is_projected_without_mutation(self):
        views, states = creation()
        original = copy.deepcopy((views, states))
        ids = RouterIDs()
        ids.created('alpha', views, states)
        self.assertFalse(differences(ids.response('python', 'alpha', 'R008', views['python']),
                                     ids.response('go', 'alpha', 'R008', views['go'])))
        self.assertFalse(differences(ids.snapshot('python', states['python']['after']),
                                     ids.snapshot('go', states['go']['after'])))
        self.assertEqual((views, states), original)
        listing = {side: {'body': {'routers': [view['body']['router']]}} for side, view in views.items()}
        self.assertFalse(differences(ids.response('python', 'alpha', 'R007', listing['python']),
                                     ids.response('go', 'alpha', 'R007', listing['go'])))
        self.assertTrue(differences(ids.response('python', 'beta', 'R008', views['python']),
                                    ids.response('go', 'beta', 'R008', views['go'])))
        for route in ('R014', 'R015'):
            workspaces = {side: {'body': {'workspace': {'router': view['body']['router']}}} for side, view in views.items()}
            self.assertEqual(differences(ids.response('python', 'alpha', route, workspaces['python']),
                                         ids.response('go', 'alpha', route, workspaces['go'])), [])
            for side in workspaces:
                workspaces[side]['body']['workspace']['source_agents'] = [{'id': views[side]['body']['router']['id']}]
            self.assertTrue(differences(ids.response('python', 'alpha', route, workspaces['python']),
                                        ids.response('go', 'alpha', route, workspaces['go'])))

    def test_missing_wrong_reused_or_unsafe_persistence_cannot_establish_alias(self):
        mutations = (
            lambda views, states: views['go']['body']['router'].update(id='not-stored'),
            lambda views, states: states['go']['after']['database']['fields']['rows'][0][4]['items'].pop(),
            lambda views, states: states['go']['after']['database']['fields']['rows'][0][4]['items'][0].update(name='foreign changed'),
            lambda views, states: states['go']['after']['database']['fields']['rows'][0][4]['items'].append({'id': 'extra'}),
        )
        for mutate in mutations:
            views, states = creation()
            mutate(views, states)
            ids = RouterIDs()
            with self.assertRaises(ValueError):
                ids.created('alpha', views, states)
            self.assertEqual(ids.records, [])
        for ident in ('', '.', '..', '../escape', 'a/b', 'a?query', '%2f', 'white space', '行', 'control\n'):
            views, states = creation(right=ident)
            with self.assertRaises(ValueError):
                RouterIDs().created('alpha', views, states)
        views, states = creation()
        ids = RouterIDs()
        ids.created('alpha', views, states)
        with self.assertRaisesRegex(ValueError, 'not injective'):
            ids.created('alpha', views, states)

    def test_id_like_data_hashes_filenames_and_foreign_profiles_are_never_rewritten(self):
        views, states = creation()
        ids = RouterIDs()
        ids.created('alpha', views, states)
        for side in ('python', 'go'):
            ident = views[side]['body']['router']['id']
            views[side]['body'].update(message=ident, revision=ident, fingerprint=ident, token=ident)
            views[side]['body']['router'].update(name=ident, last_probe={'id': ident})
            after = states[side]['after']
            after['database']['fields']['rows'][0][4]['items'][-1].update(name=ident, unknown_id=ident)
            after['database']['fields']['rows'] += snapshot([{'id': ident}], 'beta')['database']['fields']['rows']
            after['fake']['ros'] = {'/ip/route': [{'.id': ident, 'comment': ident}]}
            after['files'] = {'router-rule-sync/' + ident + '.json': ident}
        left = ids.response('python', 'alpha', 'R008', views['python'])
        right = ids.response('go', 'alpha', 'R008', views['go'])
        paths = {item.path for item in differences(left, right)}
        self.assertEqual(paths, {'/body/message', '/body/revision', '/body/fingerprint', '/body/token',
                                 '/body/router/name', '/body/router/last_probe/id'})
        for side in ('python', 'go'):
            projected = ids.snapshot(side, states[side]['after'])
            self.assertEqual(projected['fake'], states[side]['after']['fake'])
            self.assertEqual(projected['files'], states[side]['after']['files'])
            self.assertEqual(projected['database']['fields']['rows'][1], states[side]['after']['database']['fields']['rows'][1])


if __name__ == '__main__':
    unittest.main()
