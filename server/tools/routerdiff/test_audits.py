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
import fixed_normalization
import fixed_setup
import fixed_sources
import probe_workflows
import probe_guards
import probes
import ros_http_guards
import ikuai_http_guards
import router_lifecycle
import dhcp_guards
import ikuai_dhcp_guards
import ikuai_dhcp_writes
import dns
import dns_targets


class AuditTests(unittest.TestCase):
    def test_ikuai_http_audits_need_each_distinct_diagnostic_and_real_tls_retry_controls(self):
        actual = {(world, case.ident): case for world, group in (
            ('ikuai-http-reads', ikuai_http_guards.reads), ('ikuai-http-tls', ikuai_http_guards.tls)) for case in group()}
        counts = {'H0211': 2, 'H0212': 2, 'H0213': 13, 'H0214': 2, 'H0215': 2, 'H0216': 3,
                  'H0217': 3, 'H0218': 3, 'H0219': 4, 'H0220': 3, 'H0221': 3,
                  'H0224': 2, 'H0225': 2, 'H0226': 2, 'H0227': 2, 'H0230': 15,
                  'H0231': 3, 'H0232': 2, 'H0233': 2, 'H0234': 3, 'H0235': 4, 'H0236': 3, 'H0237': 11}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                self.assertEqual(actual[world, name].expected, status)
                self.assertIn(ident, actual[world, name].entries)
            observations = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                             'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(observations)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(observations[:-1])[ident]['audit'], 'variants-pending')
        for ident in ('H0209', 'H0210', 'H0222', 'H0223', 'H0228', 'H0229'):
            self.assertNotIn(ident, declared())

    def test_routeros_http_audits_require_error_class_and_write_safety_not_just_probe(self):
        actual = {(world, case.ident): case for world, group in (
            ('ros-http-reads', ros_http_guards.reads), ('ros-http-writes', ros_http_guards.writes),
            ('ros-http-normal', ros_http_guards.normal)) for case in group()}
        counts = {'H0826': 5, 'H0827': 5, 'H0828': 4, 'H0829': 4, 'H0830': 3,
                  'H0831': 4, 'H0832': 11, 'H0833': 4, 'H0834': 4}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                self.assertEqual(actual[world, name].expected, status)
                self.assertIn(ident, actual[world, name].entries)
            observations = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                             'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(observations)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(observations[:-1])[ident]['audit'], 'variants-pending')

    def test_dns_target_audits_need_every_json_kind_and_both_sides_of_address_boundaries(self):
        actual = {(world, case.ident): case for world, _, groups in dns_targets.worlds()
                  for group in groups for case in group()}
        for ident, count in {'H0140': 37, 'H0141': 34, 'H0142': 76, 'H0143': 111, 'H0144': 228}.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status, (ident, name))
                self.assertIn(ident, case.entries)
            observations = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                             'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(observations)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(observations[:-1])[ident]['audit'], 'variants-pending')

    def test_dhcp_matching_catch_audit_needs_all_reachable_error_families(self):
        actual = {(world, case.ident): case for world, _, groups in dns_targets.gateway_worlds()
                  for group in groups for case in group()}
        actual.update({('dns-matching-failures', case.ident): case for case in dns_targets.matching_cases()})
        actual.update({('dhcp-ikuai-plain', case.ident): case for case in dns.ikuai_dhcp()})
        for ident, count in {'H0145': 34, 'H0146': 34, 'H0150': 46}.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                self.assertEqual(actual[world, name].expected, status)
                self.assertIn(ident, actual[world, name].entries)
            observations = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                             'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(observations)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(observations[:-1])[ident]['audit'], 'variants-pending')

    def test_ikuai_dhcp_write_audits_need_independent_readback_and_compensation_evidence(self):
        actual = {(world, case.ident): case for world, _, groups in ikuai_dhcp_writes.worlds()
                  for group in groups for case in group()}
        actual.update({('dhcp-ikuai-plain', case.ident): case for case in dns.ikuai_dhcp()})
        counts = {'H0151': 6, 'H0152': 2, 'H0153': 3,
                  'H0264': 3, 'H0265': 3, 'H0268': 2, 'H0273': 17, 'H0274': 2, 'H0275': 2,
                  'H0276': 2, 'H0277': 16, 'H0278': 5, 'H0279': 5}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status, (ident, name))
                self.assertIn(ident, case.entries)
                self.assertEqual(case.effect, 'profile-write-device')
            observations = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                             'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(observations)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(observations[:-1])[ident]['audit'], 'variants-pending')
        for ident in ('H0266', 'H0267', 'H0269', 'H0270', 'H0271', 'H0272'):
            self.assertNotIn(ident, declared())  # Corrupt operations are not a public HTTP input.

    def test_router_deletion_audits_need_actual_lifecycle_and_every_manifest_operand(self):
        actual = {(world, case.ident): case for world, _, groups in router_lifecycle.worlds()
                  for group in groups for case in group()}
        actual.update({('stored', case.ident): case for case in fixtures.stored_guards()})
        actual.update({(world, case.ident): case for world, _, groups in workflows.job_lock_worlds({})
                       for group in groups for case in group()})
        actual.update({('empty-manifests', case.ident): case for case in fixtures.empty_manifests()})
        counts = {'R009-S01': 25, 'R009-F10': 15, 'R009-B01': 10, 'R009-B02': 10,
                  'R009-B03': 15, 'R009-B05': 25, 'R009-F11': 4, 'R010-F11': 6, 'R010-F14': 20, 'R010-S04': 10, 'R010-B01': 15,
                  **{'R009-F' + str(number): 2 for number in range(13, 18)}}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status, (ident, name))
                self.assertIn(ident, case.entries, (ident, name))
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')
        for ident in ('R009-F12', 'R009-F18', 'R009-B04', 'R010-F15', 'R010-B06'):
            self.assertNotIn(ident, declared())  # No claim about waiting-lock/repository races.

    def test_ikuai_dhcp_audits_require_complete_form_each_type_and_scoped_static_overrides(self):
        actual = {case.ident: case for case in ikuai_dhcp_guards.cases()}
        counts = {'H0247': 140, 'H0248': 212, 'H0249': 14, 'H0250': 29, 'H0251': 11, 'H0252': 17,
                  'H0253': 15, 'H0254': 48, 'H0255': 48, 'H0256': 45, 'H0257': 45, 'H0258': 32,
                  'H0259': 53, 'H0260': 53, 'H0261': 67, 'H0262': 17, 'H0263': 11,
                  'H0147': 15, 'H0148': 48, 'H0149': 48}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[name]
                self.assertEqual(world, 'dhcp-ikuai-evidence')
                self.assertEqual(case.expected, status, (ident, name))
                self.assertIn(ident, case.entries, (ident, name))
                self.assertEqual(case.effect, 'device-read')
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')
        self.assertNotIn('H0264', {ident: value for ident, value in declared().items()
                                  if any(world == 'dhcp-ikuai-evidence' for world, _, _ in value.requirements)})

    def test_dhcp_evidence_audits_require_real_report_plan_and_refused_confirmation(self):
        actual = {case.ident: case for case in dhcp_guards.cases()}
        counts = {'H0849': 11, 'H0850': 20, 'H0851': 23, 'H0852': 5, 'H0853': 9, 'H0854': 17,
                  'H0855': 27, 'H0856': 27, 'H0857': 16, 'H0858': 5, 'H0859': 23, 'H0860': 31,
                  'H0861': 13, 'H0862': 18, 'H0863': 18, 'H0904': 171, 'H0905': 33, 'H0906': 17,
                  'H0907': 14, 'H0908': 5, 'H0909': 26, 'H0910': 47}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[name]
                self.assertEqual(world, 'dhcp-evidence-guards')
                self.assertEqual(case.expected, status)
                self.assertIn(ident, case.entries, (ident, name))
                self.assertEqual(case.effect, 'device-read')
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')
        # Successful DNS publication and hardware permission acceptance are
        # deliberately not granted by this evidence-only audit increment.
        self.assertNotIn('R011-S06', declared())

    def test_probe_guard_audits_require_fatal_persistence_and_per_capability_downgrades(self):
        actual = {('probe-errors', case.ident): case for group in (probes.routeros, probes.ikuai) for case in group()}
        actual.update({('probe-guard-controls', case.ident): case for group in
                       (probe_guards.controls, probe_guards.late_rows, probe_guards.invalid_versions) for case in group()})
        actual.update({('probe-tls', case.ident): case for case in probes.tls()})
        counts = {'H0835': 10, 'H0841': 13, 'H0842': 23, 'H0843': 35, 'H0241': 15, 'H0242': 26,
                  'H1296': 110, 'R017-S01': 5, 'R017-S02': 3, 'R017-S03': 54, 'R017-F13': 54, 'R017-B04': 7}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status, (ident, name))
                self.assertIn(ident, case.entries, (ident, name))
                self.assertEqual(case.effect, 'profile-write')
                self.assertEqual(case.times, ('probe/at',))
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')
        self.assertNotIn('R017-F12', declared())  # Broader outer/schema/accepted-crash audit remains separate.

    def test_probe_invalidation_audit_needs_each_actual_probe_and_each_connection_operand(self):
        actual = {(world, case.ident): case for world, _, groups in probe_workflows.invalidation_worlds({})
                  for group in groups for case in group()}
        required = declared()['R010-B03'].requirements
        self.assertEqual(len(required), 34)
        for world, name, status in required:
            case = actual[world, name]
            self.assertEqual(case.expected, status)
            self.assertIn('R010-B03', case.entries)
        evidence = [{'world': world, 'case': name, 'entries': ['R010-B03'], 'python': {'status': status},
                     'status': 'go-unimplemented'} for world, name, status in required]
        self.assertEqual(evaluate(evidence)['R010-B03']['audit'], 'variants-audited')
        self.assertEqual(evaluate(evidence[:-1])['R010-B03']['audit'], 'variants-pending')

    def test_fixed_source_audits_require_each_operand_and_both_recovery_transactions(self):
        actual = {(world, case.ident): case for factory in (fixed_sources.router_worlds, fixed_sources.binding_worlds)
                  for world, _, groups in factory() for group in groups for case in group()}
        for world, group in (('fixed-ip-guards', clients.fixed_preview_guards),
                             ('fixed-ip-stale', clients.fixed_stale), ('fixed-ip', clients.fixed_ip)):
            actual.update({(world, case.ident): case for case in group()})
        counts = {'H0040': 26, 'R006-F18': 26, 'H0042': 10, 'R006-F19': 7,
                  'H0043': 10, 'R006-F22': 6, 'R005-S07': 4, 'R006-S04': 5}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status)
                self.assertIn(ident, case.entries)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')

    def test_setup_guard_audit_requires_saved_states_and_real_valid_token_reopen(self):
        actual = {(world, case.ident): case for world, _, groups in fixed_setup.worlds()
                  for group in groups for case in group()}
        for ident, count in (('H0035', 6), ('R006-F17', 4), ('R005-F14', 2)):
            required = declared()[ident].requirements
            self.assertEqual(len(required), count)
            for world, name, status in required:
                case = actual[world, name]
                self.assertEqual(case.expected, status)
                self.assertIn(ident, case.entries)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')

    def test_fixed_normalizer_audits_require_actual_named_compound_operands(self):
        actual = {('fixed-ip-normalization', case.ident): case for group in
                  (fixed_normalization.rejected, fixed_normalization.accepted) for case in group()}
        actual.update({('guards', case.ident): case for case in guards.fixed_ip_shape()})
        counts = {'H0038': 63, 'H0039': 16, 'H0592': 21, 'H0593': 2, 'R006-F14': 51,
                  'R006-F15': 4, 'R006-B01': 7, 'R006-B02': 3, 'R006-B03': 2}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')

    def test_fixed_wrapper_audits_require_exact_shapes_and_real_64_row_controls(self):
        actual = {('guards', case.ident): case for group in
                  (cases.body_guards, guards.fixed_ip_shape, guards.fixed_apply_shape) for case in group()}
        for world, group in (('fixed-ip', clients.fixed_ip), ('fixed-ip-64', clients.batch64)):
            actual.update({(world, case.ident): case for case in group()})
        counts = {'H0036': 32, 'H0037': 14, 'H1263': 11, 'H1270': 11, 'H1271': 11,
                  'R005-F10': 11, 'R005-F11': 11, 'R006-F10': 12, 'R006-F11': 8,
                  'R006-F12': 9, 'R006-F13': 11, 'R006-F16': 11}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            self.assertEqual(evaluate(evidence[:-1])[ident]['audit'], 'variants-pending')

    def test_client_settings_error_parent_requires_stored_contexts_and_atomic_growth(self):
        actual = {('guards', case.ident): case for case in guards.client_draft()}
        for world, group in (('client-drafts', workflows.client_drafts), ('notes-capacity', fixtures.notes_capacity),
                             ('stored-policy-capacity', device_policy.capacity)):
            actual.update({(world, case.ident): case for case in group()})
        factories = (device_policy.worlds, device_policy.invalid_ip_worlds, policy_graph.worlds,
                     policy_owned.worlds, policy_invalid.worlds)
        for factory in factories:
            actual.update({(world, case.ident): case for world, _, groups in factory()
                           for group in groups for case in group()})
        for ident in ('H1261', 'H1262'):
            required = declared()[ident].requirements
            self.assertEqual(len(required), 348)
            for world, name, status in required:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)
                if status == 400:
                    self.assertTrue(case.unchanged)
            evidence = [{'world': world, 'case': name, 'entries': [ident], 'python': {'status': status},
                         'status': 'go-unimplemented'} for world, name, status in required]
            self.assertEqual(evaluate(evidence)[ident]['audit'], 'variants-audited')
            without_growth = [row for row in evidence if 'growth' not in row['case']]
            self.assertEqual(evaluate(without_growth)[ident]['audit'], 'variants-pending')

    def test_client_wrapper_audits_require_true_method_scope_and_every_shape(self):
        actual = {('guards', case.ident): case for group in (guards.client_draft, cases.body_guards) for case in group()}
        for world, group in (('agent-scope', fixtures.agent_scope), ('client-drafts', workflows.client_drafts),
                             ('client-agent-query-writes', workflows.client_agent_query_writes)):
            actual.update({(world, case.ident): case for case in group()})
        counts = {'H1256': 13, 'H1257': 22, 'H1258': 12, 'H1259': 3, 'H1260': 3,
                  'R003-F10': 21, 'R003-F11': 11, 'R003-F12': 127, 'R003-F13': 1, 'R003-F14': 1,
                  'R002-S03': 2, 'R004-S04': 1, 'R003-B06': 2}
        for ident, count in counts.items():
            required = declared()[ident].requirements
            self.assertEqual(len(required), count, ident)
            for world, name, status in required:
                case = actual[world, name]
                self.assertIn(ident, case.entries)
                self.assertEqual(case.expected, status)

    def test_job_lock_audit_needs_each_guard_and_its_real_probe_completion(self):
        actual = {(world, case.ident): case for world, _, groups in workflows.job_lock_worlds({})
                  for group in groups for case in group()}
        required = declared()['H1278'].requirements
        self.assertEqual(len(required), 30)
        for world, name, status in required:
            case = actual[world, name]
            self.assertIn('H1278', case.entries)
            self.assertEqual(case.expected, status)
        evidence = [{'world': world, 'case': name, 'entries': ['H1278'], 'python': {'status': status},
                     'status': 'go-unimplemented'} for world, name, status in required]
        self.assertEqual(evaluate(evidence)['H1278']['audit'], 'variants-audited')
        self.assertEqual(evaluate(evidence[:-1])['H1278']['audit'], 'variants-pending')

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
        self.assertEqual(len(audits), 554)  # Named declarations; matrix parsing grants no coverage.
        self.assertFalse(set(audits) - set(load()[1]))
        self.assertEqual({value['audit'] for value in evaluate([]).values()},
                         {'variants-pending', 'source-proof-controls-pending', 'review-proof-controls-pending'})
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
        self.assertEqual(len(audits['R001-F91'].requirements), 5)

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
        actual.update({('stored-policy-capacity', case.ident): case for case in device_policy.capacity()})
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
