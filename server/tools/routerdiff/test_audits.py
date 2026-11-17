"""Coverage cannot be obtained by parsing IDs or executing a convenient subset."""
import copy
import unittest

from audits import declared, evaluate
from inventory import load


class AuditTests(unittest.TestCase):
    def test_auth_audits_require_every_explicit_real_reference_variant(self):
        audits = declared()
        self.assertEqual(len(audits), 26 * 7)
        self.assertFalse(set(audits) - set(load()[1]))
        self.assertEqual({value['audit'] for value in evaluate([]).values()}, {'variants-pending'})
        ident = 'R005-F01'
        requirements = audits[ident].requirements
        rows = [{'world': world, 'case': case, 'entries': [ident], 'python': {'status': status},
                 'status': 'go-unimplemented'} for world, case, status in requirements]
        self.assertEqual(evaluate(rows)[ident]['audit'], 'variants-audited')
        self.assertEqual(len(requirements), 7)
        for changed in (rows[:-1], [{**row, 'world': 'unrelated-world'} for row in rows]):
            self.assertEqual(evaluate(changed)[ident]['audit'], 'variants-pending')
        for key, value in (('entries', []), ('python', {'status': 200})):
            changed = copy.deepcopy(rows)
            changed[0][key] = value
            self.assertEqual(evaluate(changed)[ident]['audit'], 'variants-pending')
        # A completed auth audit cannot imply a whole route or domain passed.
        self.assertNotIn('R005-S01', audits)
        self.assertNotIn('X06', audits)


if __name__ == '__main__':
    unittest.main()
