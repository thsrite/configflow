"""Owned OS fault isolation and non-vacuous packet-evidence comparisons."""
import unittest

from cases import Case
from inventory import load
from network_faults import NetworkFaults
from run import execute
from test_runner import ObservationSide, pair


class NetworkFaultTests(unittest.TestCase):
    def test_host_namespace_is_refused_before_any_privileged_command(self):
        with self.assertRaisesRegex(RuntimeError, 'distinct owned network namespace'):
            NetworkFaults().control(True)

    def test_native_no_syn_is_a_difference_with_completed_http_preserved(self):
        class PacketSide(ObservationSide):
            def __init__(self, saw_syn):
                super().__init__()
                self.saw_syn = saw_syn

            def rpc(self, operation, **values):
                if operation == 'network_fault':
                    return {'enabled': values['enabled'], 'saw_syn': False if values['enabled'] else self.saw_syn}
                return super().rpc(operation, **values)

        case = Case('network-evidence', 'R002', 'GET', '/api/client-devices', 200,
            prepare=({'rpc': 'network_fault', 'enabled': True, 'expected': {'enabled': True, 'saw_syn': False}},),
            finalize=({'rpc': 'network_fault', 'enabled': False, 'expected': {'enabled': False, 'saw_syn': True}},))
        routes = load()[0]
        result = execute(pair(PacketSide(True), PacketSide(False)), case, {('GET', case.path)}, routes)
        self.assertEqual(result['status'], 'different')
        self.assertEqual(result['go']['status'], 200)
        self.assertTrue(any('saw_syn' in row['path'] for row in result['differences']))
        self.assertEqual(execute(pair(PacketSide(True), PacketSide(True)), case,
                                 {('GET', case.path)}, routes)['status'], 'compared')


if __name__ == '__main__':
    unittest.main()
