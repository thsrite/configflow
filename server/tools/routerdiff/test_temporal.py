"""Clock pairing must never erase stale clocks or unlisted data changes."""
import copy
from datetime import datetime, timezone
import unittest

from compare import differences
from temporal import fresh_pair


class TemporalTests(unittest.TestCase):
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
