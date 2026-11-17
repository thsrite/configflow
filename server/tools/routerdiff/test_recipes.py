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
from inventory import load
from bugs import BUGS


class RecipeTests(unittest.TestCase):
    def test_unique_variants_and_real_design_references(self):
        routes, entries = load()
        seen = set()
        for module in (cases, guards, workflows, fixtures, probes, releases, clients, sync, upstream, dns, gateway, disconnect, routing, policy_view, returns, persistence, ipv6, device_policy, policy_graph):
            for name in module.__dict__:
                value = getattr(module, name)
                if not inspect.isgeneratorfunction(value) or getattr(value, '__module__', '') != module.__name__:
                    continue
                for case in value():
                    with self.subTest(case=case.ident):
                        self.assertFalse(case.ident in seen, 'duplicate recipe: ' + case.ident)
                        seen.add(case.ident)
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

    def test_response_references_are_side_local(self):
        value = {'revision': cases.Ref('first', '/body/own')}
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'python-token'}}}), {'revision': 'python-token'})
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'go-token'}}}), {'revision': 'go-token'})
        with self.assertRaises(KeyError):
            cases.resolve(value, {})


if __name__ == '__main__':
    unittest.main()
