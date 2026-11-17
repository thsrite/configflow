"""Coverage cannot be obtained by parsing IDs or executing a convenient subset."""
import copy
import unittest

from audits import declared, evaluate
from inventory import load
import guards
import device_policy
import workflows
import fixtures
import clients
import cases


class AuditTests(unittest.TestCase):
    def test_auth_audits_require_every_explicit_real_reference_variant(self):
        audits = declared()
        self.assertEqual(len(audits), 26 * 7 + 25 * 3 + 5 + 14 + 12 + 5 + 8 + 23)
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

    def test_settings_audit_requires_real_guard_ids_and_all_boolean_loop_fields(self):
        audits = declared()
        actual = {case.ident: case for group in (guards.router_settings, guards.router_settings_ikuai,
                                                guards.guard_vendor_support) for case in group()}
        identifiers = ['H' + str(number).zfill(4) for number in range(154, 162)]
        identifiers += ['H0491', 'H0492', 'H1243', 'H1244', 'H1246', 'H1247']
        for ident in identifiers:
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, case=name):
                    self.assertEqual(world, 'guards')
                    self.assertIn(name, actual)
                    self.assertIn(ident, actual[name].entries)
                    self.assertEqual(actual[name].expected, status)
        self.assertEqual(len(audits['H0155'].requirements), 3 * 6 * 4)
        self.assertEqual(len(audits['H0161'].requirements), 2)
        self.assertEqual(len(audits['H0160'].requirements), 3)
        self.assertGreater(len(audits['H1243'].requirements), len(audits['H0155'].requirements))

    def test_client_normalizers_require_each_reviewed_input_and_translation(self):
        audits = declared()
        actual = {case.ident: case for case in guards.client_draft()}
        identifiers = ['H0023', 'H0024', 'H0068']
        identifiers += ['H' + str(number).zfill(4) for number in range(71, 80)]
        for ident in identifiers:
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, case=name):
                    self.assertEqual(world, 'guards')
                    self.assertIn(name, actual)
                    self.assertIn(ident, actual[name].entries)
                    self.assertEqual(actual[name].expected, status)
        self.assertEqual(len(audits['H0068'].requirements), 82)
        self.assertEqual(len(audits['H0023'].requirements), 14)
        self.assertEqual(len(audits['H0024'].requirements), 2)
        self.assertGreater(len(audits['H0078'].requirements), len(audits['H0073'].requirements))

    def test_stored_policy_guards_require_all_inputs_on_three_public_paths(self):
        audits = declared()
        actual = {(world, case.ident): case for world, _, groups in device_policy.worlds()
                  for group in groups for case in group()}
        for number in range(80, 85):
            ident = 'H' + str(number).zfill(4)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['H0083'].requirements), 40 * 3)
        self.assertEqual(len(audits['H0084'].requirements), 9 * 3)

    def test_client_success_audit_requires_persistent_and_real_device_variants(self):
        actual = {('client-drafts', case.ident): case for case in workflows.client_drafts()}
        for world, group in (('client-mode-notes', workflows.client_mode_notes),
                             ('client-canonical-names', workflows.client_canonical_names),
                             ('notes-capacity', fixtures.notes_capacity),
                             ('stored-policy-legacy', device_policy.legacy),
                             ('notes-keep-reservation', clients.notes_keep_reservation)):
            actual.update({(world, case.ident): case for case in group()})
        audits = declared()
        for number in range(1, 9):
            ident = 'R003-S' + str(number).zfill(2)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['R003-S07'].requirements), 4)
        self.assertEqual(len(audits['R003-S08'].requirements), 5)

    def test_store_guard_audit_preserves_all_named_sources_and_vendor_precedence(self):
        groups = [('guards', cases.body_guards), ('guards', guards.router_settings),
                  ('guards', guards.router_settings_ikuai), ('guards', guards.guard_vendor_support),
                  ('router-guard-normalization', guards.normalized_router_guards),
                  ('legacy-credentials', fixtures.legacy_credentials), ('stored', fixtures.stored_guards),
                  ('empty-manifests', fixtures.empty_manifests)]
        actual = {(world, case.ident): case for world, group in groups for case in group()}
        identifiers = ['H' + str(number).zfill(4) for number in range(1234, 1243)]
        identifiers += ['H1245', 'H1248', 'H1249', 'H1250']
        identifiers += ['H' + str(number).zfill(4) for number in range(1251, 1256)]
        identifiers += ['H' + str(number).zfill(4) for number in range(1289, 1294)]
        self.assertEqual(len(identifiers), 23)
        audits = declared()
        for ident in identifiers:
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['H1234'].requirements), 24)
        self.assertEqual(len(audits['H1237'].requirements), 13 * 4 + 14)
        self.assertEqual(len(audits['H1241'].requirements), 2)


if __name__ == '__main__':
    unittest.main()
