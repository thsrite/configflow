"""Probe-schema recipe and deliberately broken native outcome controls."""
import base64
import copy
from datetime import datetime, timezone
import unittest

from cases import Case
from inventory import load
from probe_schemas import cases, only_failed_probe_metadata
from run import execute
from test_runner import ObservationSide, pair


class SchemaTests(unittest.TestCase):
    def test_nested_schema_vectors_are_real_peer_json_with_own_recovery(self):
        rows = list(cases())
        crashes = [case for case in rows if case.python_bug]
        self.assertEqual(len(rows), 16)
        self.assertEqual(len(crashes), 5)
        for case in crashes:
            self.assertEqual(case.expected, 500)
            self.assertEqual(case.effect, 'device-read')
            self.assertTrue(case.html_error)
            self.assertFalse(case.times)  # A crash never claims to save a probe clock.
            self.assertEqual(len(case.console_patterns), 2)
            index = rows.index(case)
            self.assertEqual(rows[index + 1].expected, 200)
            self.assertEqual(rows[index + 1].effect, 'profile-write')
            self.assertTrue(set(case.entries) <= load()[1].keys())

    def test_safe_go_rejection_requires_review_and_unsafe_matching_crash_never_passes(self):
        class Crash(ObservationSide):
            def request(self, **values):
                result = super().request(**values)
                raw = b'<html>synthetic uncaught failure</html>'
                return {**result, 'status': 500, 'kind': 'text', 'content': raw.decode(),
                        'headers': [('Content-Type', 'text/html'), ('Content-Length', str(len(raw)))],
                        'body': base64.b64encode(raw).decode()}

        case = Case('schema-oracle', 'R017', 'POST', '/api/routers/r-ik/probe', 500,
                    html_error=True, python_bug='IKUAI_PROBE_NESTED_SCHEMA_CRASH',
                    unchanged=False, effect='device-read')
        available, routes = {('POST', '/api/routers/{}/probe')}, load()[0]
        native = ObservationSide(status=502, content={'success': False, 'message': '合成设备响应格式无法识别'})
        result = execute(pair(Crash(), native), case, available, routes)
        self.assertEqual(result['status'], 'reference-bug-observation')
        self.assertTrue(result['go_bug_outcome']['structured_refusal'])
        self.assertFalse(result['python_bug_outcome']['database_changed'])
        self.assertTrue(result['differences'])  # Corrected outcome is review, never equality/pass.
        for native in (Crash(), ObservationSide(content={'success': False, 'message': 'wrong success status'}),
                       ObservationSide(status=502, content={'success': 0, 'message': 'wrong bool type'}),
                       ObservationSide(status=502, content={'success': False, 'message': ''})):
            result = execute(pair(Crash(), native), case, available, routes)
            self.assertEqual(result['status'], 'different')
            self.assertIn('safety', {item['category'] for item in result['differences']})
        for field in ('database', 'files', 'fake'):
            native = ObservationSide(status=502, content={'success': False, 'message': 'safe-looking body'})
            native.after = copy.deepcopy(native.before)
            if field == 'database':
                native.after[field]['fields']['rows'].append(['profile', 'beta', 'changed', 0, 'bad', 2])
            elif field == 'files':
                native.after[field]['unexpected.json'] = 'e30='
            else:
                native.after[field]['transcript'].append({'peer': 'ikuai', 'method': 'POST', 'path': '/Action/call',
                                                         'body': {'action': 'edit'}, 'raw': 'eyJhY3Rpb24iOiJlZGl0In0=', 'headers': {}})
            result = execute(pair(Crash(), native), case, available, routes)
            self.assertEqual(result['status'], 'different', field)
            self.assertIn('safety', {item['category'] for item in result['differences']})

    def test_only_own_fresh_failed_probe_metadata_may_be_added_by_a_safe_fix(self):
        now = datetime.now(timezone.utc)
        before = {'database': {
            'fields': {'columns': ['scope', 'owner', 'field', 'position', 'value', 'revision'], 'rows': [
                ['profile', 'alpha', '_revision', 0, 1, 1],
                ['profile', 'alpha', 'routers', 1, {'items': [{'id': 'r-ik', 'name': '合成路由器'}]}, 1],
                ['profile', 'beta', '_revision', 0, 7, 7]]},
            'sections': {'columns': ['scope', 'owner'], 'rows': [['profile', 'alpha'], ['profile', 'beta']]},
            'meta': {'columns': ['key', 'value'], 'rows': [['store_id', 'synthetic-store'],
                     ['generation', 1], ['commit_id', 'synthetic-prior-commit']]}}}
        after = copy.deepcopy(before)
        after['database']['meta']['rows'][1][1] = 2
        after['database']['meta']['rows'][2][1] = 'synthetic-next-commit'
        after['database']['fields']['rows'][0][4:] = [2, 2]
        after['database']['fields']['rows'][1][5] = 2
        router = after['database']['fields']['rows'][1][4]['items'][0]
        router['last_probe'] = {'at': now.isoformat(), 'ok': False, 'message': '合成设备响应格式无法识别'}
        window = (now.timestamp() - .1, now.timestamp() + .1)
        self.assertTrue(only_failed_probe_metadata(before, after, 'alpha', 'r-ik', window))
        for member, value in (('at', '2000-01-01T00:00:00Z'), ('ok', True), ('ok', 0), ('message', '')):
            wrong = copy.deepcopy(after)
            wrong['database']['fields']['rows'][1][4]['items'][0]['last_probe'][member] = value
            self.assertFalse(only_failed_probe_metadata(before, wrong, 'alpha', 'r-ik', window))
        for change in ('foreign', 'name', 'duplicate', 'revision', 'bad-row', 'bad-list', 'store-id', 'commit', 'generation'):
            wrong = copy.deepcopy(after)
            rows = wrong['database']['fields']['rows']
            if change == 'foreign':
                rows[2][4] = 8
            elif change == 'name':
                rows[1][4]['items'][0]['name'] = 'corrupted'
            elif change == 'duplicate':
                rows[1][4]['items'].append(copy.deepcopy(rows[1][4]['items'][0]))
            elif change == 'bad-row':
                rows[1][4]['items'].append(None)
            elif change == 'bad-list':
                rows[1][4]['items'] = {}
            elif change == 'store-id':
                wrong['database']['meta']['rows'][0][1] = 'another-store'
            elif change == 'generation':
                wrong['database']['meta']['rows'][1][1] = 100
            elif change == 'commit':
                wrong['database']['meta']['rows'][2][1] = 'synthetic-prior-commit'
            else:
                rows[0][4] = 3
            self.assertFalse(only_failed_probe_metadata(before, wrong, 'alpha', 'r-ik', window), change)
        self.assertNotIn('last_probe', before['database']['fields']['rows'][1][4]['items'][0])


if __name__ == '__main__':
    unittest.main()
