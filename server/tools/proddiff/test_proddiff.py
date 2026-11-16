"""Synthetic regressions for the acceptance gate (run on the test server)."""
import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('proddiff', Path(__file__).with_name('run.py'))
pd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pd)


class ComparatorTests(unittest.TestCase):
    def test_mutations_are_detected(self):
        good = (200, 'application/json', b'{"success":true,"data":{"count":3}}')
        for name, bad, expected in [
            ('status', (500, good[1], good[2]), ('status', 'value')),
            ('content-type', (200, 'text/plain', good[2]), ('Content-Type', 'value')),
            ('value', (200, good[1], b'{"success":true,"data":{"count":4}}'), ('body.data.count', 'value')),
            ('missing', (200, good[1], b'{"success":true,"data":{}}'), ('body.data.count', 'missing-in-go')),
            ('transport', (0, '', b''), ('status', 'value')),
        ]:
            with self.subTest(name=name):
                self.assertIn(expected, pd.response_diffs(good, bad, '/synthetic', []))

    def test_structural_formats(self):
        cases = [
            ((200, 'application/json', b'{"b":2,"a":1}'), (200, 'application/json', b'{"a":1,"b":2}'), '/synthetic'),
            ((200, 'text/yaml', b'a: 1\nb: [2, 3]\n'), (200, 'text/yaml', b'b:\n- 2\n- 3\na: 1\n'), '/synthetic'),
            ((200, 'text/plain', b'a\r\nb\r\n\r\n'), (200, 'text/plain', b'a\nb'), '/synthetic'),
            ((200, 'application/json', b'{"content":"a: 1\\nb: 2\\n"}'),
             (200, 'application/json', b'{"content":"b: 2\\na: 1\\n"}'), '/api/generate/mihomo/preview'),
        ]
        for left, right, route in cases:
            with self.subTest(route=route, content_type=left[1]):
                self.assertEqual([], pd.response_diffs(left, right, route, []))

    def test_normalization_is_route_and_path_specific(self):
        a = (200, 'application/json', b'{"size":1,"data":{"size":2}}')
        b = (200, 'application/json', b'{"size":3,"data":{"size":4}}')
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.data.size', 'value')], pd.response_diffs(a, b, '/synthetic', rule))
        self.assertEqual(2, len(pd.response_diffs(a, b, '/other', rule)))

    def test_unknown_keys_never_escape(self):
        self.assertEqual([('body.<key>', 'value')], pd.differences({'private.example': 1}, {'private.example': 2}))

    def test_enumerates_every_snapshot_entity(self):
        doc = {'system': {'agents': [{'id': 'agent-a'}, {'id': 'agent-b'}]}, 'shared': {
            'subscriptions': [{'id': 'sub-a'}, {'id': 'sub-b'}], 'rule_library': []},
            'profiles': {'one': {'routers': {'items': [{'id': 'router-a'}]}}, 'two': {'routers': {'items': []}}}}
        rows = [['GET', route] for route in ('/api/agents/<agent_id>', '/api/subscriptions/<sub_id>/nodes', '/api/routers/<router_id>/routes')]
        result = list(pd.cases(rows, doc, Path('/nonexistent')))
        self.assertEqual(10, len(result))
        self.assertEqual(1, sum(case[-1] for case in result))
        self.assertTrue(any('/router-a/' in case[2] for case in result))


if __name__ == '__main__':
    unittest.main()
