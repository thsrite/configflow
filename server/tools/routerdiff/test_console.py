"""Closed console grammar contract; execution must really mutate owned tables."""
import unittest

from console import execute, UnsupportedScript


class ConsoleTests(unittest.TestCase):
    def test_route_batch_mutates_before_returning_ids(self):
        records = []
        def add(path, values):
            ident = '*' + format(len(records) + 1, 'X')
            records.append((path, values))
            return ident
        commands = []
        for index in range(4):
            commands.append(':if ($f=0) do={:onerror e in={:put ("cf-id:' + str(index) + ':" . '
                '[/ip/route/add dst-address="10.55.' + str(index) + '.0/24" gateway="10.44.0.2" '
                'comment="引号\\\"\\$\\n🧪" disabled=no])} do={:set f 1; :put ("cf-err:' + str(index) + ':" . $e)}}')
        script = ':local f 0\n' + '\n'.join(commands) + '\n:put "cf-done"'
        result = execute(script, [], add)
        self.assertEqual(len(records), 4)
        self.assertEqual(records[0], ('/ip/route', {'dst-address': '10.55.0.0/24', 'gateway': '10.44.0.2',
            'comment': '引号"$\n🧪', 'disabled': 'false'}))
        self.assertEqual(result['ret'], 'cf-id:0:*1\r\ncf-id:1:*2\r\ncf-id:2:*3\r\ncf-id:3:*4\r\ncf-done')
        broken = script.replace('gateway="10.44.0.2"', 'unsupported=bare', 1)
        with self.assertRaises(UnsupportedScript):
            execute(broken, [], add)
        self.assertEqual(len(records), 4)

    def test_address_batch_iterates_all_members(self):
        records = []
        script = ':local n 0\n:local m ""\n:foreach a in={"10.44.0.21";"10.44.0.22"} do={:onerror e in={' + \
            '/ip/firewall/address-list/add list="cf-test" comment="合成🧪" disabled=no address=$a}' + \
            ' do={:set n ($n+1); :if ($n=1) do={:set m ($a . " " . $e)}}}\n:put ("cf-done:" . $n . ":" . $m)'
        result = execute(script, [], lambda path, values: records.append((path, values)))
        self.assertEqual(result, {'ret': 'cf-done:0:'})
        self.assertEqual([values['address'] for _, values in records], ['10.44.0.21', '10.44.0.22'])
        self.assertTrue(all(path == '/ip/firewall/address-list' for path, _ in records))


if __name__ == '__main__':
    unittest.main()
