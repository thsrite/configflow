"""Synthetic regressions for the acceptance gate (run on the test server)."""
import importlib.util
import io
import zipfile
from pathlib import Path
import unittest
import sys
sys.path.insert(0, str(Path(__file__).parent))

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
            ('type', (200, good[1], b'{"success":true,"data":{"count":true}}'), ('body.data.count', 'type')),
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

    def test_invalid_documents_cannot_pass(self):
        bad = (200, 'application/json', b'{')
        self.assertEqual([('body', 'parse-python'), ('body', 'parse-go')], pd.response_diffs(bad, bad, '/synthetic', []))

    def test_zip_members_use_their_own_format(self):
        def archive(content):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as output:
                output.writestr('config.yaml', 'a: 1\n')
                output.writestr('rules.txt', content)
            return (200, 'application/zip', buffer.getvalue())
        self.assertEqual([], pd.response_diffs(archive('a\r\nb\r\n'), archive('a\nb'), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb'), archive('a\nc'), '/api/generate/oxidns', []))

    def test_normalization_preserves_type_changes(self):
        a = (200, 'application/json', b'{"size":1}')
        b = (200, 'application/json', b'{"size":{}}')
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.size', 'type')], pd.response_diffs(a, b, '/synthetic', rule))

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

    def test_four_mandatory_mutations(self):
        from generators import self_check
        samples = {'yaml': (200, 'text/yaml', b'dns:\n  upstreams:\n  - port: 53\n'),
                   'text': (200, 'text/plain', b'[General]\nloglevel=info\n')}
        checks = self_check((200, 'application/json', b'{}'), samples, pd.response_diffs)
        self.assertEqual({'status', 'json-leaf', 'yaml-leaf', 'text-line'}, set(checks))
        self.assertTrue(all(result == 'PASS' for result in checks.values()))
        with self.assertRaises(RuntimeError):
            self_check((200, 'application/json', b'{}'), samples, lambda *args: [])

    def test_generator_inventory_and_http_takeover(self):
        from generators import inventory
        doc = {'system': {'system_config': {'default_profile_id': 'one'},
                          'agents': [{'id': 'agent-a', 'service_type': 'mihomo'}]},
               'shared': {'subscriptions': [{'id': 'sub-a'}]},
               'profiles': {'one': {'routers': {'items': []}}, 'two': {'routers': {'items': []}}}}
        jobs, coverage = inventory(doc, set())
        self.assertEqual(2, coverage['GENERATOR mihomo']['cases'])
        self.assertEqual(2, coverage['GENERATOR subscription-loon']['cases'])
        self.assertEqual(1, coverage['GENERATOR agent-mihomo']['cases'])
        self.assertEqual(list(range(len(jobs))), [job['index'] for job in jobs])
        jobs, coverage = inventory(doc, {('GET', '/api/config/<profile_id>/mihomo')})
        self.assertEqual('http', coverage['GENERATOR mihomo']['mode'])
        self.assertFalse(any(job['kind'] == 'mihomo' for job in jobs))

    def test_successful_output_floor_survives_http_takeover(self):
        from generators import successful_output_regressions
        previous = {'GENERATOR mihomo': {'success': 2, 'mode': 'generator'}}
        self.assertEqual(set(), successful_output_regressions(previous, {'GENERATOR mihomo': {'success': 2, 'mode': 'http'}}))
        self.assertEqual({'GENERATOR mihomo successful-output-count'}, successful_output_regressions(previous, {'GENERATOR mihomo': {'success': 1}}))

    def test_http_subscription_takeover_requests_all_formats(self):
        doc = {'system': {'agents': []}, 'shared': {'subscriptions': [{'id': 'sub-a'}]},
               'profiles': {'one': {'routers': {'items': []}}}}
        cases = list(pd.cases([['GET', '/api/profiles/<profile_id>/subscriptions/<sub_id>/proxies']], doc, Path('/nonexistent')))
        self.assertEqual(3, len(cases))
        self.assertEqual({'mihomo', 'surge', 'loon'}, {pd.urllib.parse.parse_qs(pd.urllib.parse.urlsplit(case[2]).query)['format'][0] for case in cases})


if __name__ == '__main__':
    unittest.main()
