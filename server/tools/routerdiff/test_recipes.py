"""Recipe integrity checks, distinct from actual product coverage."""
import unittest
import inspect

import cases
import guards
import workflows
import fixtures
import probes
import ros_http_guards
import probe_workflows
import probe_guards
import releases
import clients
import sync
import upstream
import dns
import dns_targets
import dhcp_guards
import ikuai_dhcp_guards
import ikuai_dhcp_writes
import gateway
import disconnect
import routing
import router_lifecycle
import policy_view
import returns
import persistence
import ipv6
import device_policy
import policy_graph
import policy_owned
import policy_side
import policy_invalid
import split
import split_onboarding
import split_state
import forwarding_guards
import fixed_normalization
import fixed_setup
import fixed_sources
from inventory import load
from bugs import BUGS


class RecipeTests(unittest.TestCase):
    def test_routeros_http_vectors_distinguish_refusal_and_applied_uncertain_write(self):
        reads = list(ros_http_guards.reads())
        writes = list(ros_http_guards.writes())
        self.assertEqual(len(reads), 11)
        self.assertEqual(len(writes), 18)
        self.assertEqual(sum(case.elapsed_min == 9 for case in reads), 1)
        self.assertEqual(sum(case.expected == 200 for case in reads), 1)
        for preview, confirmation in zip(writes[::2], writes[1::2]):
            self.assertEqual(confirmation.document['token'], cases.Ref(preview.ident, '/body/token'))
            self.assertEqual(confirmation.expected, 502)
            self.assertEqual(confirmation.stored, {'client_fixed_ips': {}})
            fault = confirmation.prepare[0]
            self.assertEqual(confirmation.expected_requests[1]['count'], int(fault.get('apply', False)))
            self.assertEqual(confirmation.effect, 'device-only')
            self.assertEqual(confirmation.fake_lengths['ros/~1ip~1dhcp-server~1lease'], 2)
        missing = next(case for case in writes if case.ident == 'R005/ros-http-missing-write')
        self.assertNotIn('H0830', missing.entries)  # GET404 and PUT404 differ.

    def test_dns_target_guards_separate_single_and_every_ha_address_position(self):
        worlds = dns_targets.worlds()
        self.assertEqual(len(worlds), 112)
        _, entries = load()
        for world, options, groups in worlds:
            rows = [case for group in groups for case in group()]
            self.assertEqual(len(rows), 2 if rows[0].checks['report/dhcp_dns/status'] == 'pending' else 3)
            self.assertTrue(all(set(case.entries) <= entries.keys() for case in rows))
            self.assertEqual(rows[0].prepare[0].get('presentation'), None)
            self.assertTrue(all(case.effect == 'device-read' for case in rows))
            self.assertEqual(options['alpha']['routers']['items'][1]['dns_agent_id'], 'dns')
            if len(rows) == 3:
                self.assertEqual(rows[-1].expected, 409)
                self.assertEqual(rows[-1].expected_requests,
                    tuple({'match': {'peer': 'ikuai', '/body/action': action}, 'count': 0}
                          for action in ('add', 'edit', 'del')))

    def test_ikuai_dhcp_writes_use_actual_edit_gate_and_complete_before_image(self):
        _, entries = load()
        worlds = ikuai_dhcp_writes.worlds()
        self.assertEqual(len(worlds), 25)
        for world, options, groups in worlds:
            rows = [case for group in groups for case in group()]
            self.assertEqual(len(rows), 2)
            preview, confirmation = rows
            self.assertEqual(preview.expected, 200)
            self.assertEqual(confirmation.expected, 502)
            self.assertEqual(confirmation.document['fingerprint'], cases.Ref(preview.ident, '/body/plan/fingerprint'))
            self.assertEqual(preview.prepare[0]['presentation'], {'static_mask_suffix': False})
            self.assertEqual(set(confirmation.fake_checks['ikuai/dhcp_server/0']) - {'status', 'available'},
                             set(ikuai_dhcp_guards.FIELDS))
            self.assertTrue(set(confirmation.entries) <= set(entries))
            if confirmation.join:
                controls = confirmation.prepare
                self.assertEqual([row.get('rpc', row.get('action')) for row in controls[:3]],
                                 ['fault', 'begin', 'await_gate'])
                self.assertEqual(controls[1]['document'], confirmation.document)
                self.assertTrue(controls[0]['apply'])
                self.assertTrue(controls[0]['passthrough'])
                self.assertEqual(controls[-1], {'action': 'release', 'gate': confirmation.join})

    def test_router_delete_sequences_preserve_same_id_foreign_profile_and_devices(self):
        _, entries = load()
        worlds = router_lifecycle.worlds()
        self.assertEqual(len(worlds), 5)
        seen = set()
        for world, options, groups in worlds:
            rows = [case for group in groups for case in group()]
            self.assertEqual(len(rows), 9)
            self.assertEqual([case.expected for case in rows], [200, 404, 200, 404, 200, 200, 404, 200, 200])
            target = rows[0].profile
            foreign = 'beta' if target == 'alpha' else 'alpha'
            self.assertTrue(options[foreign]['routers']['items'][0]['ros_policy_state'])
            self.assertEqual(rows[-1].profile, foreign)
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertEqual(len(case.expected_requests), 3)
                self.assertTrue(all(item['count'] == 0 for item in case.expected_requests))
            self.assertEqual([case.effect for case in rows if case.method == 'DELETE' and case.expected == 200],
                             ['router-delete', 'router-delete'])

    def test_ikuai_dhcp_fixture_is_full_verified_form_and_every_request_is_readonly(self):
        _, entries = load()
        self.assertEqual(len(ikuai_dhcp_guards.FIELDS), 43)
        self.assertEqual(len(ikuai_dhcp_guards.INT_FIELDS), 17)
        self.assertEqual(set(dns.ikuai_server()) - {'status', 'available'}, set(ikuai_dhcp_guards.FIELDS))
        self.assertEqual(len(ikuai_dhcp_guards.STATIC_FIELDS), 10)
        seen = set()
        rows = list(ikuai_dhcp_guards.cases())
        self.assertGreater(len(rows), 600)
        for case in rows:
            self.assertNotIn(case.ident, seen)
            seen.add(case.ident)
            self.assertTrue(set(case.entries) <= set(entries))
            self.assertEqual(case.effect, 'device-read')
            if case.route == 'R011':
                self.assertEqual(case.expected, 409)
                self.assertIsNone(case.document)
                self.assertTrue(case.message)
            else:
                self.assertEqual(case.expected, 200)
        # Keep real firmware display defaults. These evidence-only recipes do
        # not relax the previously discovered static-route netmask suffix bug.
        self.assertNotIn('presentation', ikuai_dhcp_guards.base())

    def test_dhcp_evidence_variants_refuse_confirmation_before_any_device_write(self):
        _, entries = load()
        seen = set()
        rows = list(dhcp_guards.cases())
        for case in rows:
            self.assertNotIn(case.ident, seen)
            seen.add(case.ident)
            self.assertEqual(len(case.entries), len(set(case.entries)))
            self.assertTrue(set(case.entries) <= set(entries))
            self.assertEqual(case.effect, 'device-read')
            self.assertFalse(case.unchanged)
            if case.route == 'R011':
                self.assertEqual(case.expected, 409)
                self.assertIsNone(case.document)
                self.assertTrue(case.message)
            elif case.route == 'R018':
                self.assertEqual(case.expected, 200)
                self.assertEqual(case.times, ('generated_at',))
            else:
                self.assertEqual(case.route, 'R012')
                self.assertEqual(case.expected, 200)
        self.assertGreater(len(rows), 400)
        oversized = next(seed for name, _, seed, _ in dhcp_guards.vectors() if name == 'code-oversized')
        self.assertEqual(oversized['ros'][dhcp_guards.OPTION][0]['code'], '0' * 5000)

    def test_capability_refusal_keeps_probe_success_and_iteration_continues_after_late_bad_rows(self):
        failures = [case for group in (probes.routeros, probes.ikuai) for case in group()
                    if 'R017-S03' in case.entries]
        self.assertEqual(len(failures), 51)
        self.assertTrue(all(case.expected == 200 and case.checks['success'] for case in failures))
        self.assertEqual(sum(case.elapsed_min == 9 for case in failures), 7)
        self.assertTrue(all(len(case.checks['probe/capabilities']) >= 2 for case in failures))
        later = list(probe_guards.late_rows())
        self.assertEqual(len(later), 3)
        for case in later:
            self.assertEqual(case.prepare[0]['body'], [{}, None])
            self.assertEqual(case.expected, 200)
            self.assertIn('H0835', case.entries)
            self.assertIn('H0843', case.entries)
        controls = list(probe_guards.controls())
        self.assertEqual(len(controls), 7)
        for case in controls:
            self.assertTrue(set(case.entries) <= load()[1].keys())
            self.assertIn('H1296', case.entries)
        invalid = list(probe_guards.invalid_versions())
        self.assertEqual(len(invalid), 18)
        self.assertTrue(all(case.expected == 502 and case.stored for case in invalid))
        self.assertTrue(all('H1296' in case.entries for case in invalid))

    def test_probe_roundtrip_keeps_raw_own_time_and_editor_revision_references(self):
        rows = list(probe_workflows.roundtrip())
        self.assertEqual(len(rows), 10)
        self.assertEqual(sum(case.route == 'R017' for case in rows), 2)
        seen = {case.ident: case for case in rows}
        for case in rows:
            self.assertTrue(set(case.entries) <= load()[1].keys())
            if case.route == 'R017':
                self.assertIn('probe/at', case.times)
                self.assertEqual(case.effect, 'profile-write')
        stale = seen['R015/probe-clock-name-edit-invalidates-old-editor']
        fresh = seen['R015/probe-clock-modern-editor-retains-last-probe']
        self.assertEqual(stale.expected, 409)
        self.assertEqual(fresh.expected, 200)
        self.assertNotEqual(stale.document['revision'].case, fresh.document['revision'].case)
        self.assertEqual(fresh.checks['workspace/router/last_probe/at'].case, 'R017/routeros/clock-roundtrip')
        worlds = list(probe_workflows.invalidation_worlds({}))
        self.assertEqual(len(worlds), 17)
        self.assertEqual(sum('probe-edit-clear-' in world for world, _, _ in worlds), 5)
        for world, _, groups in worlds:
            actual = [case for group in groups for case in group()]
            self.assertEqual(len(actual), 2)
            self.assertEqual(actual[0].route, 'R017')
            edit = actual[1]
            self.assertEqual(edit.requires, ('R017',))
            if 'probe-edit-clear-' in world:
                self.assertEqual(edit.stored_absent, ('routers/items/0/last_probe',))
                self.assertEqual(edit.checks['router/last_probe'], None)
            else:
                self.assertEqual(edit.checks['router/last_probe/at'].case, actual[0].ident)

    def test_fixed_sources_use_real_vendor_and_binding_prerequisites(self):
        router_cases = [case for _, _, groups in fixed_sources.router_worlds()
                        for group in groups for case in group()]
        self.assertEqual(len(router_cases), 34)
        self.assertEqual(sum(case.expected == 409 for case in router_cases), 32)
        guarded = [case for case in router_cases if 'H0040' in case.entries]
        self.assertEqual(len(guarded), 26)
        self.assertTrue(all(case.unchanged and case.expected_requests for case in guarded))
        rows = [case for _, _, groups in fixed_sources.binding_worlds()
                for group in groups for case in group()]
        self.assertEqual(len(rows), 8)
        for case in rows:
            if case.route == 'R005':
                self.assertIsInstance(case.document['token'], cases.Ref)
                self.assertEqual(case.expected, 200)
                self.assertEqual(case.stored['client_fixed_ips'], {})
                self.assertEqual(case.effect, 'profile-write-device')
                self.assertIn('device_notes', case.retained_fields)
        stale = next(case for case in clients.fixed_stale() if case.ident == 'R005/stale-cross-profile')
        self.assertEqual(stale.profile, stale.document['profile_id'])

    def test_setup_guard_uses_canonical_persistent_sessions_and_public_reopen(self):
        worlds = fixed_setup.worlds()
        rows = []
        for name, options, groups in worlds:
            self.assertEqual(options['file_modes'][fixed_setup.FILE], 0o600)
            self.assertEqual(options['directory_modes']['setup-sessions'], 0o700)
            self.assertIn(fixed_setup.PROFILE, options['additional_profiles'])
            rows.extend(case for group in groups for case in group())
        self.assertEqual(len(rows), 6)
        apply = next(case for case in rows if case.route == 'R005')
        self.assertIsInstance(apply.document['token'], cases.Ref)
        self.assertEqual(apply.prepare[0]['method'], 'PATCH')
        self.assertEqual(apply.prepare[0]['path'], '/api/setup/' + fixed_setup.SESSION)
        self.assertEqual(apply.prepare[0]['absent'], ('abandoned',))
        self.assertFalse(any(control.get('rpc') not in ('http', None) for case in rows for control in case.prepare))

    def test_fixed_normalization_vectors_do_not_use_reference_helpers(self):
        _, entries = load()
        bad = list(fixed_normalization.rejected())
        good = list(fixed_normalization.accepted())
        self.assertEqual(len(bad), 97)
        self.assertEqual(len(good), 12)
        self.assertEqual(len({case.ident for case in bad + good}), 109)
        for case in bad + good:
            self.assertTrue(set(case.entries) <= entries.keys())
            self.assertEqual(case.method, 'POST')
            self.assertEqual(case.route, 'R006')
            self.assertEqual(case.requires, ())
        self.assertTrue(all(case.unchanged and case.expected == 400 and case.expected_requests
                            and not case.prepare for case in bad))
        self.assertTrue(all(case.effect == 'device-read' and not case.unchanged for case in good))
        # The positive duplicate-MAC control must actually be seen by BOTH
        # devices; one MAC on two IPs of one router is a different refusal.
        pair = good[-1]
        self.assertEqual([row['router_id'] for row in pair.document['devices']], ['r-ik', 'r-ros'])
        for table in ('dhcp_lease', 'monitor_lanip'):
            self.assertEqual(pair.prepare[0]['ikuai'][table][0]['mac'], '02:00:00:00:00:21')

    def test_job_lock_worlds_do_not_require_unrelated_sibling_handlers(self):
        options = {'alpha': {'routers': {'items': []}}}
        worlds = workflows.job_lock_worlds(options)
        self.assertEqual(len(worlds), 15)
        seen = set()
        for world, _, groups in worlds:
            rows = [case for group in groups for case in group()]
            self.assertEqual(len(rows), 2)
            busy, finish = rows
            self.assertEqual(busy.requires, ())
            self.assertEqual(busy.expected, 409)
            self.assertTrue(any(control.get('rpc') == 'await_gate' for control in busy.prepare))
            self.assertEqual(finish.requires, (busy.route,))
            self.assertTrue(finish.capture_before_prepare)
            self.assertEqual(finish.join, next(control['ident'] for control in busy.prepare if control.get('rpc') == 'begin'))
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)

    def test_creation_does_not_require_a_future_cleanup_handler(self):
        creates = [case for case in workflows.router_creates() if case.route == 'R008']
        self.assertTrue(creates)
        self.assertTrue(all(not case.requires for case in creates))
        # Every fixture has its own disposable DATA_DIR. Missing DELETE must
        # not prevent comparison of an already implemented POST handler.
        deletes = [case for case in workflows.router_creates() if case.route == 'R009']
        self.assertTrue(all(case.requires == ('R008',) for case in deletes))

    def test_forwarding_guards_use_linked_records_and_real_heartbeat_before_version_checks(self):
        _, entries = load()
        seen = set()
        for world, options, groups in forwarding_guards.worlds():
            self.assertEqual(options['alpha']['routers']['items'][0]['proxy_gateway_agent_id'], 'm-a')
            for group in groups:
                for case in group():
                    self.assertNotIn(case.ident, seen)
                    seen.add(case.ident)
                    self.assertTrue(set(case.entries) <= set(entries))
                    if world.startswith('forwarding-version-') or world.startswith('forwarding-layout-'):
                        self.assertTrue(any(control.get('path') == '/api/agents/m-a/heartbeat' for control in case.prepare))
    def test_unique_variants_and_real_design_references(self):
        routes, entries = load()
        seen = set()
        for module in (cases, guards, workflows, fixtures, probes, releases, clients, sync, upstream, dns, gateway, disconnect, routing, policy_view, returns, persistence, ipv6, device_policy, policy_graph, policy_owned, policy_side):
            for name in module.__dict__:
                value = getattr(module, name)
                if not inspect.isgeneratorfunction(value) or getattr(value, '__module__', '') != module.__name__:
                    continue
                for case in value():
                    with self.subTest(case=case.ident):
                        self.assertFalse(case.ident in seen, 'duplicate recipe: ' + case.ident)
                        seen.add(case.ident)
                        if case.boundary:
                            self.assertEqual(case.route, '')
                            self.assertEqual(case.boundary[0], case.method)
                            self.assertIn('G23', case.entries)
                            self.assertTrue(case.owner)
                        else:
                            self.assertTrue(case.route in routes, 'unknown public route ID: ' + case.route)
                        for reference in case.entries:
                            self.assertTrue(reference in entries, 'unknown matrix ID: ' + reference)
                        if case.python_bug:
                            self.assertTrue(case.python_bug in BUGS, 'unknown reference-bug ID: ' + case.python_bug)

    def test_stored_policy_worlds_have_explicit_sources_and_all_public_callers(self):
        _, entries = load()
        seen = set()
        for world, options, groups in device_policy.worlds() + device_policy.invalid_ip_worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual({case.route for case in rows}, {'R002', 'R003', 'R014'})
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertEqual(case.expected, 400)
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertIsNotNone(case.message)
            self.assertIn('device_policies', options['alpha']['oxidns'])

    def test_native_policy_graphs_cover_readonly_and_atomic_mode_failure(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_graph.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
            self.assertEqual(rows[1].message, rows[0].checks['reason'])
            self.assertTrue(rows[1].unchanged)
            self.assertIn('oxidns', rows[2].retained_fields)
            self.assertEqual(options['alpha']['oxidns']['config_mode'], 'native')

    def test_managed_native_graph_errors_cannot_be_relabelled_as_refuse_helper(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_owned.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertNotIn('H0086', case.entries)
            self.assertTrue(rows[1].unchanged)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_side_listener_variants_use_real_public_import_and_three_http_steps(self):
        _, entries = load()
        seen = set()
        for world, options, groups in policy_side.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 200, 200])
            for case in rows:
                self.assertNotIn(case.ident, seen)
                seen.add(case.ident)
                self.assertTrue(set(case.entries) <= set(entries))
            self.assertIn('oxidns/native_config', rows[1].stored_contains)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_invalid_native_settings_use_real_readonly_and_atomic_refusal_workflow(self):
        _, entries = load()
        for world, options, groups in policy_invalid.worlds():
            rows = [case for group in groups for case in group()]
            self.assertEqual([case.expected for case in rows], [200, 400, 200, 200])
            for case in rows:
                self.assertTrue(set(case.entries) <= set(entries))
                self.assertNotIn('H0086', case.entries)
            self.assertIn('oxidns', rows[2].retained_fields)

    def test_router_split_generation_uses_only_external_public_boundaries(self):
        _, entries = load()
        for world, options, groups in split.worlds() + split_onboarding.worlds() + split_state.worlds():
            for group in groups:
                for case in group():
                    self.assertEqual(case.route, '')
                    self.assertEqual(case.method, case.boundary[0])
                    self.assertTrue(case.owner)
                    self.assertTrue(set(case.entries) <= set(entries))
                    self.assertIn('G23', case.entries)
                    self.assertTrue(case.unchanged)

    def test_response_references_are_side_local(self):
        value = {'revision': cases.Ref('first', '/body/own')}
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'python-token'}}}), {'revision': 'python-token'})
        self.assertEqual(cases.resolve(value, {'first': {'body': {'own': 'go-token'}}}), {'revision': 'go-token'})
        with self.assertRaises(KeyError):
            cases.resolve(value, {})


if __name__ == '__main__':
    unittest.main()
