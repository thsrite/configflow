"""Recipe integrity checks, distinct from actual product coverage."""
import unittest
import inspect

import cases
import guards
import workflows
import fixtures
from inventory import load


class RecipeTests(unittest.TestCase):
    def test_unique_variants_and_real_design_references(self):
        routes, entries = load()
        seen = set()
        for module in (cases, guards, workflows, fixtures):
            for name in module.__dict__:
                value = getattr(module, name)
                if not inspect.isgeneratorfunction(value) or getattr(value, '__module__', '') != module.__name__:
                    continue
                for case in value():
                    with self.subTest(case=case.ident):
                        self.assertFalse(case.ident in seen, 'duplicate recipe: ' + case.ident)
                        seen.add(case.ident)
                        self.assertIn(case.route, routes)
                        for reference in case.entries:
                            self.assertIn(reference, entries)

    def test_response_references_are_side_local(self):
        value = {'revision': cases.Ref('first', '/body/own')}
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'python-token'}}}), {'revision': 'python-token'})
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'go-token'}}}), {'revision': 'go-token'})
        with self.assertRaises(KeyError):
            cases.resolve(value, {})


if __name__ == '__main__':
    unittest.main()
