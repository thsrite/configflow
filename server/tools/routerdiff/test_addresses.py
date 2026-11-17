"""Report privacy must not weaken exact address comparisons."""
import ipaddress
import base64
import unittest

from addresses import network, report_aliases
from compare import differences


class AddressTests(unittest.TestCase):
    def test_runtime_global_values_are_stable_private_report_aliases_only(self):
        for version in (4, 6):
            cidr = network('privacy-proof', version)
            self.assertEqual(network('privacy-proof', version), cidr)
            value = ipaddress.ip_network(cidr)
            self.assertEqual(value.version, version)
            self.assertTrue(value.is_global)
            self.assertFalse(value.network_address.is_multicast)
            self.assertFalse(value.network_address.is_reserved)
            document = {cidr: {'line': 'IP-CIDR,' + cidr, 'address': str(value.network_address),
                               'bytes': base64.b64encode(cidr.encode()).decode()}}
            self.assertNotIn(cidr, repr(report_aliases(document)))
            self.assertNotIn(str(value.network_address), repr(report_aliases(document)))
            self.assertNotIn(base64.b64encode(cidr.encode()).decode(), repr(report_aliases(document)))
            self.assertIn(cidr, document)
            self.assertTrue(differences(document, {cidr: {'line': 'wrong', 'address': str(value.network_address)}}))


if __name__ == '__main__':
    unittest.main()
