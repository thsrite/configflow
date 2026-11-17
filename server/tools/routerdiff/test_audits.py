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
import policy_graph
import policy_owned
import policy_side
import policy_invalid
import split
import split_onboarding
import split_state
import gateway
import forwarding_guards


class AuditTests(unittest.TestCase):
    def test_forwarding_guards_and_transport_audits_require_all_real_callers(self):
        actual = {(world, case.ident): case for world, _, groups in forwarding_guards.worlds()
                  for group in groups for case in group()}
        for group in (gateway.reads, gateway.writes, gateway.response_limits,
                      gateway.display_filter, gateway.malformed_filter_bug):
            actual.update({('forwarding-failures', case.ident): case for case in group()})
        for group in (gateway.uncertain, gateway.timeouts):
            actual.update({('forwarding-uncertain', case.ident): case for case in group()})
        counts = {'H0001': 40, 'H0004': 8, 'H0005': 5, 'H0006': 3, 'H0007': 3, 'H0008': 5, 'H0009': 5,
                  'H0010': 2, 'H0011': 4, 'H0012': 14, 'H0013': 14, 'H0014': 10, 'H0015': 36,
                  'H0016': 23, 'H0017': 37, 'H0018': 1, 'H0019': 1, 'H0020': 1, 'H0021': 1, 'H0022': 26,
                  'R025-F10': 24, 'R025-F11': 37, 'R026-F12': 26, 'R026-F13': 38}
        audits = declared()
        for ident, count in counts.items():
            self.assertEqual(len(audits[ident].requirements), count, ident)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in audits[ident].requirements]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')
        self.assertNotIn('H0002', audits)
        self.assertNotIn('H0003', audits)

    def test_split_preparation_and_state_guards_require_their_real_distinct_callers(self):
        audits = declared()
        actual = {(world, case.ident): case
                  for world, _, groups in split_onboarding.worlds() + split_state.worlds()
                  for group in groups for case in group()}
        counts = {'H0102': 7, 'H0103': 11, 'H0104': 8, 'H0105': 3, 'H0106': 3, 'H0107': 8,
                  'H0108': 10, 'H0109': 7, 'H0114': 4, 'H0115': 4}
        for ident, count in counts.items():
            self.assertEqual(len(audits[ident].requirements), count)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
                    self.assertTrue(case.boundary)
                    self.assertTrue(case.unchanged)
                    self.assertIn('gateway', case.path)
                    self.assertNotIn('/generate', case.path)

    def test_split_guards_require_all_external_http_variants(self):
        audits = declared()
        actual = {(world, case.ident): case for world, _, groups in split.worlds()
                  for group in groups for case in group()}
        for ident, count in (('H0110', 60), ('H0111', 4), ('H0112', 30), ('H0113', 12)):
            self.assertEqual(len(audits[ident].requirements), count)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
                    self.assertEqual(case.route, '')
                    self.assertTrue(case.boundary)
                    self.assertTrue(case.unchanged)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in audits[ident].requirements]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')

    def test_auth_audits_require_every_explicit_real_reference_variant(self):
        audits = declared()
        self.assertEqual(len(audits), 26 * 7 + 25 * 3 + 5 + 14 + 12 + 5 + 8 + 23 + 1 + 5 + 4 + 6 + 4 + 4 + 10 + 24)
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

    def test_cache_audit_requires_real_age_lru_overlap_and_signature_controls(self):
        groups = [('discovery', clients.discovery), ('discovery-failures', clients.discovery_failures),
                  ('discovery-signature', clients.cache_signature), ('discovery-lru', clients.cache_lru),
                  ('discovery-coalescing', clients.cache_coalescing), ('discovery-staleness', clients.cache_staleness)]
        actual = {(world, case.ident): case for world, group in groups for case in group()}
        required = declared()['T03'].requirements
        self.assertEqual(len(required), 161)
        for world, name, status in required:
            case = actual[world, name]
            self.assertIn('T03', case.entries)
            self.assertEqual(case.expected, status)
        aged = actual['discovery-staleness', 'R004/stale-cache-61s-only-marks-stale']
        self.assertEqual(aged.age_seconds, 61)
        self.assertEqual(aged.age_anchor, 'ended')
    def test_native_helper_audits_require_every_diagnostic_and_actual_policy_output(self):
        actual = {(world, case.ident): case for world, _, groups in policy_graph.worlds()
                  for group in groups for case in group()}
        actual.update({('native-policy-valid', case.ident): case for case in policy_graph.valid()})
        actual.update({('stored-policy-capacity', case.ident): case for case in device_policy.capacity()})
        audits = declared()
        for number in range(85, 90):
            ident = 'H' + str(number).zfill(4)
            for world, name, status in audits[ident].requirements:
                with self.subTest(entry=ident, world=world, case=name):
                    case = actual[world, name]
                    self.assertIn(ident, case.entries)
                    self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['H0086'].requirements), 50 * 2)
        self.assertEqual(len(audits['H0088'].requirements), 5 * 2)
        self.assertEqual(len(audits['H0089'].requirements), 7 * 2)
        self.assertIn('oxidns/native_config', actual['native-policy-valid', 'R003/native-policy-valid-direct'].stored_contains)
    def test_native_ownership_audits_require_each_guard_and_actual_cleanup(self):
        actual = {(world, case.ident): case for world, _, groups in policy_owned.worlds()
                  for group in groups for case in group()}
        actual.update({('native-owned-valid', case.ident): case for case in policy_owned.valid()})
        actual.update({('native-policy-valid', case.ident): case for case in policy_graph.valid()})
        audits = declared()
        for number in range(96, 100):
            ident = 'H' + str(number).zfill(4)
            for world, name, status in audits[ident].requirements:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
        cleanup = actual['native-owned-valid', 'R003/native-owned-direct-to-smart-withdraws-plugins']
        self.assertEqual(set(cleanup.stored_excludes['oxidns/native_config']), {'cf_device_policy', 'cf_device_direct'})
    def test_side_listener_audits_require_public_context_and_all_wildcard_controls(self):
        actual = {(world, case.ident): case for world, _, groups in policy_side.worlds()
                  for group in groups for case in group()}
        audits = declared()
        for number in range(90, 96):
            ident = 'H' + str(number).zfill(4)
            for world, name, status in audits[ident].requirements:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['H0090'].requirements), 4)
        self.assertEqual(len(audits['H0091'].requirements), (6 + 5) * 2)
        self.assertIn(('native-side-legacy-empty-wildcard-agent-host',
                      'R003/native-side-direct-legacy-empty-wildcard-agent-host', 200), audits['H0091'].requirements)
    def test_native_parent_audits_require_all_graph_and_normalization_families(self):
        worlds = policy_graph.worlds() + policy_owned.worlds() + policy_invalid.worlds()
        actual = {(world, case.ident): case for world, _, groups in worlds for group in groups for case in group()}
        actual.update({('native-policy-valid', case.ident): case for case in policy_graph.valid()})
        actual.update({('native-owned-valid', case.ident): case for case in policy_owned.valid()})
        audits = declared()
        for ident in ('H0100', 'H0101', 'R002-F12', 'R003-F15'):
            for world, name, status in audits[ident].requirements:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
        self.assertEqual(len(audits['R002-F12'].requirements), 120)
        self.assertEqual(len(audits['R003-F15'].requirements), 120)
        self.assertEqual(len(audits['H0100'].requirements), 120 * 2 + 2)


if __name__ == '__main__':
    unittest.main()
