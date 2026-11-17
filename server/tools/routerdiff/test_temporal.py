"""Clock pairing must never erase stale clocks or unlisted data changes."""
import copy
from dataclasses import replace
from datetime import datetime, timezone
import unittest

from compare import differences
from cases import Case, Ref
from temporal import fresh_pair, ResponseClocks


class TemporalTests(unittest.TestCase):
    def test_cached_clock_requires_exact_own_side_fresh_source_without_mutating_raw_values(self):
        start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
        observations = {'python': {'body': {'at': '2026-01-01T00:00:00', 'literal': 'untouched'}},
                        'go': {'body': {'at': '2026-01-01T00:00:02.000Z', 'literal': 'untouched'}}}
        raw = copy.deepcopy(observations)
        clocks = ResponseClocks()
        fresh = Case('read', 'R004', 'GET', '/api/client-devices/discovery', 200, times=('at',))
        windows = {'python': (start, start + .1), 'go': (start + 2, start + 2.1)}
        clocks.pair(fresh, observations, windows)
        cached = Case('cache', 'R004', 'GET', fresh.path, 200, cached_times={'at': Ref('read', '/body/at')})
        result, found = clocks.pair(cached, observations, {})
        self.assertEqual(found, [])
        self.assertEqual(differences(result['python'], result['go']), [])
        self.assertEqual(observations, raw)
        for value in ('2026-01-01T00:00:00', '2026-01-01T00:00:02Z', '2026-01-01T00:00:03Z',
                      None, False, [], {}):
            changed = copy.deepcopy(observations)
            changed['go']['body']['at'] = value
            result, found = clocks.pair(cached, changed, {})
            self.assertEqual(found[0].category, 'clock-cache')
            self.assertEqual(result, changed)
        changed = copy.deepcopy(observations)
        changed['go']['body']['literal'] = observations['go']['body']['at']
        result, found = clocks.pair(cached, changed, {})
        self.assertEqual(found, [])
        self.assertEqual([item.path for item in differences(result['python'], result['go'])], ['/body/literal'])

    def test_cache_clock_cannot_use_wrong_scope_undeclared_source_or_a_bad_fresh_clock(self):
        start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
        source = {'python': {'body': {'at': '2026-01-01T00:00:00'}},
                  'go': {'body': {'at': '1999-01-01T00:00:00Z'}}}
        clocks = ResponseClocks()
        fresh = Case('read', 'R004', 'GET', '/api/client-devices/discovery', 200, times=('at',))
        _, found = clocks.pair(fresh, source, {'python': (start, start), 'go': (start, start)})
        self.assertEqual(found[0].category, 'clock')
        cached = Case('cache', 'R004', 'GET', fresh.path, 200, cached_times={'at': Ref('read', '/body/at')})
        _, found = clocks.pair(cached, source, {})
        self.assertEqual(found[0].category, 'clock-cache')
        for kwargs in ({'profile': 'beta'}, {'route': 'R001'},
                       {'cached_times': {'at': Ref('missing', '/body/at')}},
                       {'cached_times': {'at': Ref('read', '/body/other')}},
                       {'cached_times': {'at': Ref('read', '/body/at', prefix='altered')}},
                       {'cached_times': {'at': Ref('read', '/headers/at')}},
                       {'times': ('at',)}):
            with self.assertRaises(ValueError):
                clocks.pair(replace(cached, **kwargs), source, {})
        source['python']['body']['at'] = '2026-01-01T00:00:01'
        with self.assertRaisesRegex(ValueError, 'reference cached timestamp changed'):
            clocks.pair(cached, source, {})

    def test_pointer_scope_does_not_pair_an_adjacent_time_field(self):
        start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
        observations = {side: {'body': {'rows': [{'a/b': value}, {'a/b': value}]}}
                        for side, value in (('python', '2026-01-01T00:00:00Z'), ('go', '2026-01-01T00:00:02Z'))}
        projected, issues = fresh_pair(observations,
            {'python': (start, start + .1), 'go': (start + 2, start + 2.1)}, ('rows/0/a~1b',))
        self.assertEqual(issues, [])
        self.assertEqual([item.path for item in differences(projected['python'], projected['go'])], ['/body/rows/1/a~1b'])

    def test_only_declared_fields_in_own_windows_are_projected_on_copies(self):
        observations = {'python': {'body': {'generated_at': '2026-01-01T00:00:00', 'other': 'literal'}},
                        'go': {'body': {'generated_at': '2026-01-01T00:00:02.123456789Z', 'other': 'literal'}}}
        start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
        windows = {'python': (start, start + .1), 'go': (start + 2, start + 2.5)}
        original = copy.deepcopy(observations)
        projected, issues = fresh_pair(observations, windows, ('generated_at',))
        self.assertEqual(issues, [])
        self.assertEqual(differences(projected['python'], projected['go']), [])
        self.assertEqual(observations, original)
        observations['go']['body']['other'] = '2026-01-01T00:00:02Z'
        projected, issues = fresh_pair(observations, windows, ('generated_at',))
        self.assertEqual(issues, [])
        self.assertEqual([item.path for item in differences(projected['python'], projected['go'])], ['/body/other'])

    def test_stale_future_wrong_type_or_missing_go_time_remains_a_difference(self):
        start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
        windows = {'python': (start, start + .1), 'go': (start + 10, start + 10.5)}
        for value in ('2026-01-01T00:00:00Z', '2026-01-01T00:00:30Z', '2026-01-01',
                      'invalid', '', None, False, 1767225610, [], {}):
            observations = {'python': {'body': {'at': '2026-01-01T00:00:00Z'}}, 'go': {'body': {'at': value}}}
            projected, issues = fresh_pair(observations, windows, ('at',))
            self.assertEqual(len(issues), 1)
            self.assertEqual(projected, observations)
        observations['go']['body'].clear()
        projected, issues = fresh_pair(observations, windows, ('at',))
        self.assertEqual(issues[0].go, '<missing>')
        observations['python']['body']['at'] = 'invalid'
        with self.assertRaises(ValueError):
            fresh_pair(observations, windows, ('at',))


if __name__ == '__main__':
    unittest.main()
