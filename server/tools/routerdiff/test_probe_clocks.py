"""A persisted clock is an owned response binding, never a field-name mask."""
import copy
from datetime import datetime, timezone
import unittest

from cases import Case
from compare import differences
from probe_clocks import ProbeClocks
from test_identities import snapshot


def observations():
    case = Case('probe', 'R017', 'POST', '/api/routers/r-ros/probe', 200, times=('probe/at',))
    start = datetime(2026, 1, 1, tzinfo=timezone.utc).timestamp()
    views, states, requests = {}, {}, {}
    for side, clock in (('python', '2026-01-01T00:00:00'), ('go', '2026-01-01T00:00:02.25Z')):
        views[side] = {'body': {'probe': {'at': clock, 'ok': True}}, 'status': 200}
        states[side] = {'after': snapshot([{'id': 'r-ros', 'last_probe': {'at': clock, 'ok': True}}])}
        requests[side] = {'path': case.path}
    windows = {'python': (start, start + .1), 'go': (start + 2, start + 2.5)}
    return case, requests, views, states, windows


class ProbeClockTests(unittest.TestCase):
    def test_only_exact_owned_probe_slots_are_paired_and_raw_data_stays_untouched(self):
        case, requests, views, states, windows = observations()
        raw = copy.deepcopy((views, states))
        clocks = ProbeClocks()
        self.assertEqual(clocks.record(case, requests, views, states, windows), [])
        projected = {side: clocks.snapshot(side, states[side]['after']) for side in states}
        self.assertEqual(differences(projected['python'], projected['go']), [])
        self.assertEqual((views, states), raw)
        foreign = copy.deepcopy(states)
        for side in foreign:
            foreign[side]['after']['database']['fields']['rows'][0][1] = 'beta'
        self.assertTrue(differences(clocks.snapshot('python', foreign['python']['after']),
                                    clocks.snapshot('go', foreign['go']['after'])))
        listing = {side: {'body': {'routers': copy.deepcopy(states[side]['after']['database']['fields']['rows'][0][4]['items'])}}
                   for side in views}
        self.assertEqual(differences(clocks.response('python', 'alpha', 'R007', listing['python']),
                                     clocks.response('go', 'alpha', 'R007', listing['go'])), [])
        for route, profile in (('R007', 'beta'), ('R019', 'alpha')):
            self.assertTrue(differences(clocks.response('python', profile, route, listing['python']),
                                        clocks.response('go', profile, route, listing['go'])))
        for side in states:
            row = states[side]['after']['database']['fields']['rows'][0]
            row[4]['items'][0]['last_probe']['message'] = views[side]['body']['probe']['at']
            states[side]['after']['files']['raw-time.txt'] = views[side]['body']['probe']['at']
        projected = {side: clocks.snapshot(side, states[side]['after']) for side in states}
        paths = {item.path for item in differences(projected['python'], projected['go'])}
        self.assertIn('/database/fields/rows/0/4/items/0/last_probe/message', paths)
        self.assertIn('/files/raw-time.txt', paths)

    def test_bad_native_storage_or_clock_cannot_establish_any_binding(self):
        for mutation in ('wrong-clock', 'stale-response', 'missing-router', 'duplicate-router', 'missing-at'):
            case, requests, views, states, windows = observations()
            items = states['go']['after']['database']['fields']['rows'][0][4]['items']
            if mutation == 'wrong-clock':
                items[0]['last_probe']['at'] = views['python']['body']['probe']['at']
            elif mutation == 'stale-response':
                views['go']['body']['probe']['at'] = items[0]['last_probe']['at'] = '2000-01-01T00:00:00Z'
            elif mutation == 'missing-router':
                items[0]['id'] = 'other'
            elif mutation == 'duplicate-router':
                items.append(copy.deepcopy(items[0]))
            else:
                items[0]['last_probe'].pop('at')
            clocks = ProbeClocks()
            found = clocks.record(case, requests, views, states, windows)
            self.assertEqual(len(found), 1, mutation)
            self.assertEqual(found[0].category, 'clock-storage')
            self.assertEqual(clocks.records, {'python': {}, 'go': {}})
        case, requests, views, states, windows = observations()
        states['python']['after']['database']['fields']['rows'][0][4]['items'][0]['last_probe']['at'] = 'wrong'
        with self.assertRaises(ValueError):
            ProbeClocks().reference(case, requests['python'], views['python'], states['python']['after'], windows['python'])

    def test_only_explicit_fresh_probe_can_bind_and_unknown_replacement_times_stay_strict(self):
        from dataclasses import replace
        case, requests, views, states, windows = observations()
        clocks = ProbeClocks()
        self.assertEqual(clocks.record(replace(case, times=()), requests, views, states, windows), [])
        self.assertEqual(clocks.records, {'python': {}, 'go': {}})
        self.assertEqual(clocks.record(case, requests, views, states, windows), [])
        states['go']['after']['database']['fields']['rows'][0][4]['items'][0]['last_probe']['at'] = '2026-01-01T00:00:02Z'
        self.assertTrue(differences(clocks.snapshot('python', states['python']['after']),
                                    clocks.snapshot('go', states['go']['after'])))


if __name__ == '__main__':
    unittest.main()
