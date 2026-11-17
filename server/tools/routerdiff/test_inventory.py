"""Mechanical matrix/route checks. These do not count as HTTP coverage."""
from collections import Counter
import unittest

from inventory import cells, declared_route_table, load


class InventoryTests(unittest.TestCase):
    def test_reviewed_entry_count_and_signatures(self):
        routes, entries = load()
        self.assertEqual(len(routes), 26)
        self.assertEqual(len(entries), 2301)
        self.assertEqual({(r.method, r.path) for r in routes.values()}, set(declared_route_table()))
        self.assertEqual(Counter(entry.kind for entry in entries.values()), {
            'route': 844, 'helper': 1356, 'device': 30, 'generated': 24,
            'sequence': 27, 'crosscutting': 14, 'fixture': 3, 'fake-contract': 3})
        for ident, entry in entries.items():
            self.assertTrue(entry.description, ident)
            self.assertGreater(entry.source_line, 0)
            if entry.route:
                self.assertIn(entry.route, routes)
        self.assertEqual(routes['R001'].go_path, '/api/agents/{agent_id}/upstream-routers')

    def test_markdown_pipe_and_missing_separator_space(self):
        self.assertEqual(cells('| a | `x | y` | z |'), ['a', '`x | y`', 'z'])
        self.assertEqual(cells('| a | b| c |'), ['a', 'b', 'c'])
        self.assertEqual(cells(r'| a | x\|y | z |'), ['a', r'x\|y', 'z'])

    def test_o6_and_helper_sentinels_remain_distinct(self):
        _, entries = load()
        for ident in ('R001-F08', 'R001-S90', 'R026-F90', 'R016-F93', 'H0078',
                      'H1356', 'D18', 'G24', 'T27', 'X14', 'fake-routeros'):
            self.assertIn(ident, entries)


if __name__ == '__main__':
    unittest.main()
