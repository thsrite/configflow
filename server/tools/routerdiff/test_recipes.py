"""Recipe integrity checks, distinct from actual product coverage."""
import unittest
import inspect

import cases
import guards
import workflows
import fixtures
import probes
import releases
import clients
import sync
import upstream
import dns
import gateway
import disconnect
import routing
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
from inventory import load
from bugs import BUGS


class RecipeTests(unittest.TestCase):
    def test_job_lock_worlds_do_not_require_unrelated_sibling_handlers(self):
        options = {'alpha': {'routers': {'items': []}}}
        worlds = workflows.job_lock_worlds(options)
        self.assertEqual(len(worlds), 15)
        seen = set()
        for world, _, groups in worlds:
            rows = [case for group in groups for case in group()]
            self.assertEqual(len(rows), 2)
            busy, finish = rows
            self.assertEqual(busy.requires, ())
            self.assertEqual(busy.expected, 409)
            self.assertTrue(any(control.get('rpc') == 'await_gate' for control in busy.prepare))
            self.assertEqual(finish.requires, (busy.route,))
            self.assertTrue(finish.capture_before_prepare)
            self.assertEqual(finish.join, next(control['ident'] for control in busy.prepare if control.get('rpc') == 'begin'))
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)

    def test_creation_does_not_require_a_future_cleanup_handler(self):
        creates = [case for case in workflows.router_creates() if case.route == 'R008']
        self.assertTrue(creates)
        self.assertTrue(all(not case.requires for case in creates))
        # Every fixture has its own disposable DATA_DIR. Missing DELETE must
        # not prevent comparison of an already implemented POST handler.
        deletes = [case for case in workflows.router_creates() if case.route == 'R009']
        self.assertTrue(all(case.requires == ('R008',) for case in deletes))

    def test_forwarding_guards_use_linked_records_and_real_heartbeat_before_version_checks(self):
        _, entries = load()
        seen = set()
        for world, options, groups in forwarding_guards.worlds():
            self.assertEqual(options['alpha']['routers']['items'][0]['proxy_gateway_agent_id'], 'm-a')
            for group in groups:
                for case in group():
                    self.assertNotIn(case.ident, seen)
                    seen.add(case.ident)
                    self.assertTrue(set(case.entries) <= set(entries))
                    if world.startswith('forwarding-version-') or world.startswith('forwarding-layout-'):
                        self.assertTrue(any(control.get('path') == '/api/agents/m-a/heartbeat' for control in case.prepare))
    def test_unique_variants_and_real_design_references(self):
        routes, entries = load()
        seen = set()
        for module in (cases, guards, workflows, fixtures, probes, releases, clients, sync, upstream, dns, gateway, disconnect, routing, policy_view, returns, persistence, ipv6, device_policy, policy_graph, policy_owned, policy_side):
            for name in module.__dict__:
                value = getattr(module, name)
                if not inspect.isgeneratorfunction(value) or getattr(value, '__module__', '') != module.__name__:
                    continue
                for case in value():
                    with self.subTest(case=case.ident):
                        self.assertFalse(case.ident in seen, 'duplicate recipe: ' + case.ident)
                        seen.add(case.ident)
                        if case.boundary:
                            self.assertEqual(case.route, '')
                            self.assertEqual(case.boundary[0], case.method)
                            self.assertIn('G23', case.entries)
                            self.assertTrue(case.owner)
                        else:
                            self.assertTrue(case.route in routes, 'unknown public route ID: ' + case.route)
                        for reference in case.entries:
                            self.assertTrue(reference in entries, 'unknown matrix ID: ' + reference)
                        if case.python_bug:
                            self.assertTrue(case.python_bug in BUGS, 'unknown reference-bug ID: ' + case.python_bug)

    def test_stored_policy_worlds_have_explicit_sources_and_all_public_callers(self):
        _, entries = load()
        seen = set()
        for world, options, groups in device_policy.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual({case.route for case in rows}, {'R002', 'R003', 'R014'})
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertEqual(case.expected, 400)
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertIsNotNone(case.message)
            self.assertIn('device_policies', options['alpha']['oxidns'])

    def test_native_policy_graphs_cover_readonly_and_atomic_mode_failure(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_graph.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
            self.assertEqual(rows[1].message, rows[0].checks['reason'])
            self.assertTrue(rows[1].unchanged)
            self.assertIn('oxidns', rows[2].retained_fields)
            self.assertEqual(options['alpha']['oxidns']['config_mode'], 'native')

    def test_managed_native_graph_errors_cannot_be_relabelled_as_refuse_helper(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_owned.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertNotIn('H0086', case.entries)
            self.assertTrue(rows[1].unchanged)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_side_listener_variants_use_real_public_import_and_three_http_steps(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_side.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
            self.assertIn('oxidns/native_config', rows[1].stored_contains)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_invalid_native_settings_use_real_readonly_and_atomic_refusal_workflow(self):
        _, entries = load()
        for world, options, groups in policy_invalid.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertNotIn('H0086', case.entries)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_router_split_generation_uses_only_external_public_boundaries(self):
        _, entries = load()
        for world, options, groups in split.worlds() + split_onboarding.worlds() + split_state.worlds():
            for group in groups:
                for case in group():
                    self.assertEqual(case.route, '')
                    self.assertEqual(case.method, case.boundary[0])
                    self.assertTrue(case.owner)
                    self.assertTrue(set(case.entries) <= set(entries))
                    self.assertIn('G23', case.entries)
                    self.assertTrue(case.unchanged)

    def test_response_references_are_side_local(self):
        value = {'revision': cases.Ref('first', '/body/own')}
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'python-token'}}}), {'revision': 'python-token'})
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'go-token'}}}), {'revision': 'go-token'})
        with self.assertRaises(KeyError):
            cases.resolve(value, {})


if __name__ == '__main__':
    unittest.main()
