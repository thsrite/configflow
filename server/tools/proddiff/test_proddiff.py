"""Synthetic regressions for the acceptance gate (run on the test server)."""
import importlib.util
import io
import json
import zipfile
from pathlib import Path
from unittest import mock
import unittest
import sys
sys.path.insert(0, str(Path(__file__).parent))

spec = importlib.util.spec_from_file_location('proddiff', Path(__file__).with_name('run.py'))
pd = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pd)

JSON = 'application/json'


def reply(body, content_type=JSON, status=200, headers=None):
    return (status, content_type, body if isinstance(body, bytes) else json.dumps(body).encode(), headers or {})


class ComparatorTests(unittest.TestCase):
    def test_mutations_are_detected(self):
        good = reply(b'{"success":true,"data":{"count":3,"name":"a","items":["x","y"],"note":""}}')
        tests = [
            ('status', reply(good[2], status=500), ('status', 'value')),
            ('content-type', reply(good[2], 'text/plain'), ('Content-Type', 'value')),
            ('value', reply(good[2].replace(b'3', b'4')), ('body.data.count', 'value')),
            ('missing key', reply(good[2].replace(b'"count":3,', b'')), ('body.data.count', 'missing-in-go')),
            ('extra key', reply(good[2].replace(b'"count":3', b'"count":3,"success":1')), ('body.data.success', 'extra-in-go')),
            ('type bool', reply(good[2].replace(b'3', b'true')), ('body.data.count', 'type')),
            ('int to string', reply(good[2].replace(b'3', b'"3"')), ('body.data.count', 'type')),
            ('int to float', reply(good[2].replace(b'3', b'3.0')), ('body.data.count', 'int-float')),
            ('list order', reply(good[2].replace(b'["x","y"]', b'["y","x"]')), ('body.data.items[0]', 'value')),
            ('list length', reply(good[2].replace(b'["x","y"]', b'["x"]')), ('body.data.items', 'length')),
            ('empty vs missing', reply(good[2].replace(b',"note":""', b'')), ('body.data.note', 'missing-in-go')),
            ('empty vs null', reply(good[2].replace(b'"note":""', b'"note":null')), ('body.data.note', 'type')),
            ('duplicate key', reply(good[2].replace(b'"count":3', b'"count":3,"count":3')), ('body', 'parse-go')),
            ('one-sided transport', (0, '', b'', {}), ('status', 'value')),
        ]
        for name, bad, expected in tests:
            with self.subTest(name=name):
                self.assertIn(expected, pd.response_diffs(good, bad, '/synthetic', []))

    def test_both_sided_transport_failure_is_a_difference(self):
        failed = (0, '', b'', {})
        self.assertEqual([('status', 'transport-both')], pd.response_diffs(failed, failed, '/synthetic', []))

    def test_headers_are_compared(self):
        base = {'x-configflow-profile': 'one', 'etag': '"a"', 'date': 'd1', 'server': 'waitress', 'content-length': '2'}
        left = reply(b'{}', headers=base)
        tests = [
            ('value', dict(base, **{'x-configflow-profile': 'two'}), ('header.X-ConfigFlow-Profile', 'value')),
            ('missing', {k: v for k, v in base.items() if k != 'etag'}, ('header.ETag', 'missing-in-go')),
            ('extra', dict(base, **{'content-disposition': 'attachment'}), ('header.Content-Disposition', 'extra-in-go')),
        ]
        for name, headers, expected in tests:
            with self.subTest(name=name):
                self.assertIn(expected, pd.response_diffs(left, reply(b'{}', headers=headers), '/synthetic', []))
        transport_only = {'x-configflow-profile': 'one', 'etag': '"a"', 'date': 'd2', 'server': 'go', 'content-length': '3'}
        self.assertEqual([], pd.response_diffs(left, reply(b'{}', headers=transport_only), '/synthetic', []))

    def test_structural_formats(self):
        cases = [
            (reply(b'{"b":2,"a":1}'), reply(b'{"a":1,"b":2}'), '/synthetic'),
            (reply(b'a: 1\nb: [2, 3]\n', 'text/yaml'), reply(b'b:\n- 2\n- 3\na: 1\n', 'text/yaml'), '/synthetic'),
            (reply(b'a\nb\n\n', 'text/plain'), reply(b'a\nb', 'text/plain'), '/synthetic'),
            (reply(b'{"content":"a: 1\\nb: 2\\n"}'), reply(b'{"content":"b: 2\\na: 1\\n"}'), '/api/generate/mihomo/preview'),
            (reply(b'base: &b {x: 1}\nitem:\n  <<: *b\n  x: 2\n', 'text/yaml'), reply(b'base: {x: 1}\nitem: {x: 2}\n', 'text/yaml'), '/synthetic'),
        ]
        for left, right, route in cases:
            with self.subTest(route=route, content_type=left[1], body=left[2]):
                self.assertEqual([], pd.response_diffs(left, right, route, []))

    def test_line_endings_and_duplicate_yaml_keys(self):
        self.assertEqual([('body', 'line-endings')],
                         pd.response_diffs(reply(b'#!/bin/sh\necho\n', 'text/plain'), reply(b'#!/bin/sh\r\necho\r\n', 'text/plain'), '/synthetic', []))
        self.assertIn(('body.content', 'line-endings'),
                      pd.response_diffs(reply({'content': 'a\nb\n'}), reply({'content': 'a\r\nb\r\n'}), '/synthetic', []))
        self.assertIn(('body', 'parse-go'),
                      pd.response_diffs(reply(b'dns: {a: 1}\n', 'text/yaml'), reply(b'dns: {a: 1}\ndns: {a: 1}\n', 'text/yaml'), '/synthetic', []))
        self.assertIn(('body', 'parse-go'),
                      pd.response_diffs(reply(b'a: 1\n', 'text/yaml'), reply(b'a: 1\nb: {c: 1, c: 2}\n', 'text/yaml'), '/synthetic', []))

    def test_invalid_documents_cannot_pass(self):
        bad = reply(b'{')
        self.assertEqual([('body', 'parse-python'), ('body', 'parse-go')], pd.response_diffs(bad, bad, '/synthetic', []))

    def test_zip_members_use_their_own_format(self):
        def archive(content, mode=0o644, crlf=False):
            buffer = io.BytesIO()
            with zipfile.ZipFile(buffer, 'w') as output:
                output.writestr('config.yaml', 'a: 1\n')
                info = zipfile.ZipInfo('install.sh')
                info.external_attr = mode << 16
                output.writestr(info, content.replace('\n', '\r\n') if crlf else content)
            return reply(buffer.getvalue(), 'application/zip')
        self.assertEqual([], pd.response_diffs(archive('a\nb\n'), archive('a\nb'), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb'), archive('a\nc'), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb', 0o755), archive('a\nb', 0o644), '/api/generate/oxidns', []))
        self.assertTrue(pd.response_diffs(archive('a\nb\n'), archive('a\nb\n', crlf=True), '/api/generate/oxidns', []))

    def test_normalization_is_route_and_path_specific(self):
        a = reply(b'{"size":1,"data":{"size":2}}')
        b = reply(b'{"size":3,"data":{"size":4}}')
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.data.size', 'value')], pd.response_diffs(a, b, '/synthetic', rule))
        self.assertEqual(2, len(pd.response_diffs(a, b, '/other', rule)))

    def test_normalization_preserves_type_changes(self):
        rule = [{'route': '/synthetic', 'path': 'size', 'reason': 'test'}]
        self.assertEqual([('body.size', 'type')], pd.response_diffs(reply(b'{"size":1}'), reply(b'{"size":{}}'), '/synthetic', rule))
        self.assertEqual([('body.size', 'missing-in-go')], pd.response_diffs(reply(b'{"size":1}'), reply(b'{}'), '/synthetic', rule))

    def test_pattern_and_tolerance_normalization_stay_narrow(self):
        prefix = [{'route': '/r', 'path': 'message', 'pattern': '(?s)(?<=^Connection error: ).+', 'reason': 't'}]
        same = pd.response_diffs(reply({'message': 'Connection error: a'}), reply({'message': 'Connection error: b'}), '/r', prefix)
        self.assertEqual([], same)
        other = pd.response_diffs(reply({'message': 'Connection error: a'}), reply({'message': 'Agent not found'}), '/r', prefix)
        self.assertEqual([('body.message', 'value')], other)
        lines = [{'route': '/r', 'path': 'logs[]', 'pattern': r'^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d,\d{3}', 'reason': 't'}]
        a = reply({'logs': ['2026-01-01 00:00:00,000 - x - ERROR - a', '2026-01-01 00:00:00,000 - x - ERROR - b']})
        b = reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - a', '2026-01-02 01:00:00,111 - x - ERROR - c']})
        self.assertEqual([('body.logs[1]', 'value')], pd.response_diffs(a, b, '/r', lines))
        unordered = lines + [{'route': '/r', 'path': 'logs', 'unordered': True, 'reason': 't'}]
        swapped = reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - b', '2026-01-02 01:00:00,111 - x - ERROR - a']})
        self.assertEqual([], pd.response_diffs(a, swapped, '/r', unordered))
        self.assertTrue(pd.response_diffs(a, b, '/r', unordered))
        self.assertIn(('body.logs', 'length'), pd.response_diffs(a, reply({'logs': ['2026-01-02 01:00:00,111 - x - ERROR - a']}), '/r', unordered))
        # Lists of different length are still masked element by element (no positional pairing needed).
        longer = reply({'logs': ['2026-01-03 00:00:00,000 - a.b - ERROR - z', 'continuation without timestamp',
                                 '2026-01-03 00:00:00,001 - x - ERROR - a', '2026-01-03 00:00:00,002 - x - ERROR - b']})
        self.assertEqual({('body.logs', 'length'), ('body.logs[0]', 'value'), ('body.logs[1]', 'value')},
                         set(pd.response_diffs(a, longer, '/r', unordered)))
        clock = [{'route': '/r', 'path': 'start', 'tolerance': 60, 'reason': 't'}]
        tests = [
            ({'start': 1000.5}, {'start': 1030.25}, []),
            ({'start': 1000.5}, {'start': 2000.5}, [('body.start', 'value')]),
            ({'start': '2026-01-01T00:00:00.000001'}, {'start': '2026-01-01T00:00:30.000002'}, []),
            ({'start': '2026-01-01T00:00:00.000001'}, {'start': '2026-01-01T00:00:30Z'}, [('body.start', 'value')]),
            ({'start': '2026-01-01T00:00:00'}, {'start': 'not a time'}, [('body.start', 'value')]),
        ]
        for left, right, expected in tests:
            with self.subTest(left=left, right=right):
                self.assertEqual(expected, pd.response_diffs(reply(left), reply(right), '/r', clock))
        header = [{'route': '/r', 'path': 'header.content-disposition', 'pattern': r'\d{8}_\d{6}', 'reason': 't'}]
        left = reply(b'{}', headers={'content-disposition': 'inline; filename="p_20260101_000000.yaml"'})
        self.assertEqual([], pd.response_diffs(left, reply(b'{}', headers={'content-disposition': 'inline; filename="p_20260101_000001.yaml"'}), '/r', header))
        self.assertTrue(pd.response_diffs(left, reply(b'{}', headers={'content-disposition': 'attachment; filename="p_20260101_000001.yaml"'}), '/r', header))

    def test_rule_file_validation(self):
        rows = [['GET', '/r']]
        bad_rules = [
            [{'route': '/r', 'path': 'a'}],
            [{'route': '/missing', 'path': 'a', 'reason': 't'}],
            [{'route': '/r', 'path': 'a*', 'reason': 't'}],
            [{'route': '/r', 'path': 'a', 'bracket': True, 'reason': 't'}],
            [{'route': '/r', 'reason': 't'}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'ignore': True}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'pattern': 'x', 'tolerance': 1}],
            [{'route': '/r', 'path': 'a', 'reason': 't', 'pattern': 'x', 'unordered': True}],
        ]
        for rules in bad_rules:
            with self.subTest(rules=rules), mock.patch.object(pd, 'PARITY', Path('/nonexistent')), \
                    mock.patch.object(Path, 'read_text', return_value=json.dumps(rules)):
                with self.assertRaises(RuntimeError):
                    pd.load_rules(rows)
        with mock.patch.object(Path, 'read_text', return_value=json.dumps([{'route': '/r', 'bracket': True, 'reason': 't'}])):
            self.assertEqual(1, len(pd.load_rules(rows)))

    def test_bracketing_accepts_only_an_adjacent_python_state(self):
        def sequence(values):
            values = iter(values)
            return lambda: next(values)
        same = lambda a, b: [] if a == b else [('body', 'value')]
        # The window moved between the first Python reply and Go: the next Python reply agrees.
        self.assertEqual((2, 2, 1), pd.bracketed(sequence([1, 2]), sequence([2]), same))
        # Go answered before the window moved, Python after: the next Go reply agrees.
        self.assertEqual((2, 2, 1), pd.bracketed(sequence([1, 2]), sequence([0, 2]), same))
        # A Go reply that never equals any Python state is reported, however often it is asked.
        left, right, retries = pd.bracketed(sequence([1, 1, 1, 1]), sequence([9, 9, 9, 9]), same)
        self.assertEqual((0, 1, 9), (retries, left, right))

    def test_phases_put_log_readers_last(self):
        self.assertEqual(0, pd.phase(('GET', '/api/agents', '', None, False)))
        self.assertEqual(1, pd.phase(('POST', '/api/generate/mihomo', '', None, False)))
        self.assertEqual(2, pd.phase(('GET', '/api/logs/tail', '', None, False)))

    def test_unknown_keys_never_escape(self):
        self.assertEqual([('body.<key>', 'value')], pd.differences({'private.example': 1}, {'private.example': 2}))
        self.assertEqual([('header.<key>', 'value')], pd.response_diffs(
            reply(b'{}', headers={'x-private-example': 'a'}), reply(b'{}', headers={'x-private-example': 'b'}), '/r', []))

    def test_scrub_replaces_only_the_own_data_copy(self):
        own = Path('/runs/run-a/copies-b/go')
        scrubbed = pd.scrub((200, JSON, b'{"path":"/runs/run-a/copies-b/go/app.log","other":"/runs/run-a/copies-b/python"}',
                             {'x-file': '/runs/run-a/copies-b/go/x'}), own)
        self.assertEqual(b'{"path":"<DATA_DIR>/app.log","other":"/runs/run-a/copies-b/python"}', scrubbed[2])
        self.assertEqual({'x-file': '<DATA_DIR>/x'}, scrubbed[3])

    def test_enumerates_every_snapshot_entity(self):
        doc = {'system': {'agents': [{'id': 'agent-a'}, {'id': 'agent-b'}]}, 'shared': {
            'subscriptions': [{'id': 'sub-a'}, {'id': 'sub-b'}], 'rule_library': []},
            'profiles': {'one': {'routers': {'items': [{'id': 'router-a'}]}}, 'two': {'routers': {'items': []}}}}
        rows = [['GET', route] for route in ('/api/agents/<agent_id>', '/api/subscriptions/<sub_id>/nodes', '/api/routers/<router_id>/routes')]
        result = list(pd.cases(rows, doc, Path('/nonexistent')))
        # 2 profiles x entities, plus one default-profile (no selector) request per entity of the first profile.
        self.assertEqual(15, len(result))
        self.assertEqual(1, sum(case[-1] for case in result))
        self.assertTrue(any('/router-a/' in case[2] for case in result))
        self.assertEqual(5, sum('profile_id=' not in case[2] for case in result))

    def test_self_check_uses_real_samples_and_rejects_blind_comparators(self):
        from generators import self_check
        samples = {'yaml': (200, 'text/yaml', b'dns:\n  upstreams:\n  - port: 53\nmode: rule\n'),
                   'text': (200, 'text/plain', b'[General]\nloglevel=info\n[Rule]\nFINAL,DIRECT\n'),
                   'headers': reply(b'{}', headers={'x-configflow-profile': 'one', 'date': 'x'}),
                   'http-json': reply({'success': True, 'data': {'items': [{'id': 'a', 'port': 1}, {'id': 'b', 'port': 2}]}})}
        real = reply(b'{"auth_enabled":true}', headers={'date': 'x'})
        checks = self_check(real, samples, pd.response_diffs, pd.safe_key, pd.safe_header)
        for name in ('status', 'header-value', 'header-missing', 'json-real-leaf', 'json-missing-key', 'json-extra-key',
                     'json-null-vs-value', 'json-real-list-order', 'json-int-float', 'json-duplicate-key', 'yaml-leaf',
                     'yaml-duplicate-key', 'control-yaml-redumped', 'text-line', 'text-insert', 'text-line-endings',
                     'transport-both', 'control-json-reencoded'):
            self.assertEqual('PASS', checks.get(name), name)
        for blind in (lambda *args: [], lambda *args: [('body', 'value')]):
            with self.subTest(blind=blind), self.assertRaises(RuntimeError):
                self_check(real, samples, blind, pd.safe_key, pd.safe_header)
        for missing in samples:
            with self.subTest(missing=missing), self.assertRaises(RuntimeError):
                self_check(real, {k: v for k, v in samples.items() if k != missing}, pd.response_diffs, pd.safe_key, pd.safe_header)

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
