"""Coverage cannot be obtained by parsing IDs or executing a convenient subset."""
import copy
import unittest

from audits import declared, evaluate
from inventory import load


class AuditTests(unittest.TestCase):
    def test_auth_audits_require_every_explicit_real_reference_variant(self):
        audits = declared()
        self.assertEqual(len(audits), 26 * 7 + 25 * 3 + 5)
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

    def test_profile_auth_order_requires_both_failure_kinds_and_all_selectors(self):
        audits = declared()
        ident = 'R004-F90'
        required = audits[ident].requirements
        self.assertEqual(len(required), 6)
        self.assertEqual(sorted(status for _, _, status in required), [400] * 3 + [404] * 3)
        rows = [{'world': world, 'case': case, 'entries': [ident], 'python': {'status': status},
                 'status': 'go-unimplemented'} for world, case, status in required]
        self.assertEqual(evaluate(rows)[ident]['audit'], 'variants-audited')
        self.assertEqual(evaluate(rows[:-1])[ident]['audit'], 'variants-pending')
        self.assertNotIn('R001-F08', audits)
        self.assertNotIn('R001-F09', audits)
        self.assertEqual(len(audits['R001-F91'].requirements), 3)


if __name__ == '__main__':
    unittest.main()
