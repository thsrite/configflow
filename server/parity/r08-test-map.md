# R08 Python tests → Go boundary coverage

按 Python 命名测试逐项映射（计数不含 pytest 参数展开）。B = `TestR08PythonBoundary`；所有输入均为合成数据。
补充 `B/report_policy_nat_retirement` 比较完整 report；`B/static_nat_list_nested_rollback` 比较真实 NAT 列表准备和整体回滚后的新 ID/no-op。
HTTP、全 release/Agent 编排未伪造替代入口，仍归 R12/R15；C33 的四项不认领。

## test_ros_policy.py (32)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_policy_view_reads_only_recorded_ids_and_survives_external_changes` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_policy_view_api_is_authenticated_read_only_and_type_scoped` | R15 HTTP/auth/profile/no-store（R08 只提供只读 ReadPolicyObjects）；view 数据由 publish_noop_view/live_drift_* 比较。 |
| `test_longest_prefix_sources_exclude_direct_and_gateway` | TestR08PythonBoundary/longest_prefix_sources。 |
| `test_mixed_report_publish_noop_and_clear_to_static` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_exact_managed_main_route_migrates_after_activation` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_readable_names_share_one_group_and_all_references_resolve` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_names_occupied_by_definitions_or_references_reserve_the_whole_group` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_updates_keep_old_group_until_activation_and_reuse_only_freed_numbers` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_exhausted_readable_groups_block_without_mutating_foreign_objects` | TestR08NamesGatewayAndReportIntegration。 |
| `test_legacy_hashed_names_migrate_through_guarded_release` | TestR08PythonBoundary/legacy_publish、legacy_rollback；raw previous 包括未知字段。 |
| `test_unsafe_plans_never_write` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_partial_create_rolls_back` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_failure_removing_old_bundle_restores_ids_and_order` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_external_edit_reference_and_name_collision_are_preserved` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_changed_preview_and_inactive_next_hop_never_activate` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_only_selected_real_ip_sets_and_saved_device_policies_are_read` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_store_accepts_independent_modes_rejects_duplicates_and_keeps_manifest_private` | 已有 TestR01Corpus（normalize/public_view）；R08 不重写存储。 |
| `test_legacy_selection_reads_and_edits_keep_effective_modes_without_mutating_original` | 已有 TestR01Corpus/TestR01ReviewModelHelpers；R08 使用 Router.StaticRuleSets。 |
| `test_live_drift_cannot_be_reported_as_synchronized` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_later_nat_failure_undoes_complete_policy_bundle` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_api_persists_independent_modes_and_selector_changes_invalidate_preview` | R15/R12：HTTP、SQLite 持久化和总 fingerprint；R08 证据由 TestR08PreviewAndOwnershipGuards/inspect_* 比较。 |
| `test_manual_main_routes_coexist_and_survive_publish_verify_and_withdraw` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_aggregate_policy_preserves_the_reported_eight_manual_routes` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_selected_static_destination_is_preserved_when_it_overlaps_policy` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_main_route_added_after_publish_does_not_invalidate_policy` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_policy_nat_uses_its_gateway_egress_instead_of_main_destination_egress` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_policy_gateway_lookup_still_requires_an_unambiguous_connected_route` | TestR08NamesGatewayAndReportIntegration（missing/inactive/exception/ambiguous）。 |
| `test_unknown_or_lower_mangle_priority_blocks_without_writes` | TestR08PythonBoundary/order_*。 |
| `test_missing_policy_rules_names_required_version_without_writes` | TestR08PythonBoundary/missing_policy_rules_*。 |
| `test_priority_changes_invalidate_preview` | TestR08PreviewAndOwnershipGuards/lookup_order_after_preview。 |
| `test_actual_policy_egress_is_checked_before_mangle_activation` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |
| `test_unrelated_and_dynamic_list_members_neither_load_nor_stale_the_preview` | B/publish_noop_view、replace_withdraw_*、foreign_route、static_overlap、route_migration、occupied、create_*、old_delete_failure_*、live_drift_*；TestR08NamesGatewayAndReportIntegration/TestR08ReadbackBeforeActivation。 |

## test_ros_policy_activation.py (6)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_legacy_runtime_flags_allow_upgrade_withdrawal_and_rollback` | TestR08PythonBoundary/legacy_*、replace_withdraw_*。 |
| `test_runtime_normalization_still_blocks_actual_external_edits` | TestR08PythonBoundary/live_drift_{comment,unknown-field,fib,moved-list}。 |
| `test_short_invalid_transition_is_waited_for_and_not_recorded_as_config` | TestR08ActivationWaitsOnlyForRuntimeFlags/ready。 |
| `test_persistently_invalid_bundle_fails_and_rolls_back` | TestR08ActivationWaitsOnlyForRuntimeFlags/stuck。 |
| `test_external_edit_during_activation_is_preserved` | TestR08ActivationWaitsOnlyForRuntimeFlags/edit。 |
| `test_preview_ignores_invalid_but_checks_runtime_health` | TestR08PythonBoundary/live_drift_invalid；stable 证据不含 invalid，health 阻止。 |

## test_ros_policy_advisories.py (9)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_non_proxy_advisory_allows_selected_ips_to_publish_without_changing_source_scope` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_advisory_remains_visible_when_withdrawing_policy_to_selected_static_routes` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_rule_name_does_not_determine_whether_a_warning_blocks_publish` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_mixed_selection_create_failures_still_restore_previous_router_configuration` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_mixed_selection_replacement_failure_restores_the_working_bundle` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_failed_readback_does_not_delete_a_new_route_edited_by_another_writer` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_mixed_selection_does_not_overwrite_external_edits` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_incomplete_selection_stays_blocked_and_does_not_withdraw_existing_bundle` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |
| `test_static_route_selection_retains_its_existing_warning_only_behavior` | B/inspect_{Proxy,DIRECT,REJECT,REJECT-DROP,PASS,COMPATIBLE,Only direct}_{none,missing,disabled,deleted}；B/create_*、old_delete_failure_*、live_drift_*；完整 static plan 编排属于 R12。 |

## test_ros_mode_transitions.py (13)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_static_to_policy_moves_the_nat_list_member_without_exempting_direct_wan` | R08 policy NAT member migrations + 共享 TestNATListHelperBoundary/TestNATListPlanBoundary；R12 负责完整流量/回流阶段。 |
| `test_static_to_policy_keeps_foreign_or_dynamic_list_members` | 共享 TestNATListHelperBoundary 的 policy_migrations；R08 使用同一函数，不能接管 foreign/dynamic。 |
| `test_conflicting_static_replacement_cannot_withdraw_working_policy` | R12 预览冲突阻止；R08 static_replacement_* + static_checks 运行期保护。 |
| `test_partial_aggregate_switch_removes_only_policy_coverage` | B/nat_migration、route_migration、batched_route_migration_rollback、list_nat、static_replacement_*；R12 持有替代静态计划和跨阶段编排。 |
| `test_broader_managed_nat_preserves_other_destinations_and_matchers` | B/nat_migration、route_migration、batched_route_migration_rollback、list_nat、static_replacement_*；R12 持有替代静态计划和跨阶段编排。 |
| `test_transition_failure_restores_old_aggregate_nat_and_rule_order` | B/nat_migration、route_migration、batched_route_migration_rollback、list_nat、static_replacement_*；R12 持有替代静态计划和跨阶段编排。 |
| `test_inactive_static_replacement_keeps_original_policy_until_it_can_forward` | B/nat_migration、route_migration、batched_route_migration_rollback、list_nat、static_replacement_*；R12 持有替代静态计划和跨阶段编排。 |
| `test_inactive_partial_aggregate_replacement_keeps_original_static_coverage` | R08 static_replacement_*_inactive 和 batched_route_migration_rollback；聚合 replacement 构造由 R12。 |
| `test_retained_nat_destinations_keep_return_coverage_and_converge_on_first_publish` | R08 nat_migration 比较保留范围；Agent return_routing 编排/首次收敛属于 R12。 |
| `test_list_backed_nat_is_not_silently_materialized_during_policy_migration` | B/nat_migration、route_migration、batched_route_migration_rollback、list_nat、static_replacement_*；R12 持有替代静态计划和跨阶段编排。 |
| `test_skipped_conflict_preserves_existing_return_protection` | R12（只涉及 Agent 回流集合，无 R08 实现）。 |
| `test_policy_withdrawal_preserves_source_nat_protection_at_every_write` | R08 static_replacement_nat_true_* 对比准备顺序、journal、所有写请求和最终表；R12 将计划注入 PrepareStatic。 |
| `test_withdrawal_requires_full_old_source_destination_nat_proof` | R08 static_replacement_nat_true_{complete,wrong-nat-source,protocol-nat,late-nat}。 |

## test_ros_table_flags.py (12)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_present_fib_flag_matches_enabled` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_missing_disabled_or_unknown_fib_cannot_pass` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_normalization_is_scoped_to_table_fib_and_keeps_other_fields_strict` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_real_table_flag_can_publish_verify_replace_and_withdraw` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_flag_table_failure_compensates_and_preserves_original_bundle` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_actual_fib_disable_after_publish_is_not_hidden_by_normalization` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_policy_view_shows_enabled_fib_without_rewriting_ownership` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_equivalent_gateway_and_boolean_forms_work_during_release_and_rollback` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_only_default_gateway_lookup_alias_is_equivalent` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_different_gateway_lookup_interface_or_multiple_next_hops_are_rejected` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_missing_or_enabled_passthrough_cannot_match_disabled` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |
| `test_unset_fib_stops_before_mangle_and_reports_specific_field` | TestR08TableFlagsAndNetworkSubtraction；B/publish_noop_view、replace_withdraw_*、create_*_lost_*、live_drift_fib；TestR08ReadbackBeforeActivation。 |

## test_ros_preview_freshness.py (2)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_traffic_or_counter_reset_allows_previewed_publish` | B/live_drift_counter、live_drift_*；TestR08PreviewAndOwnershipGuards。HTTP 409/总 fingerprint 属于 R12/R15。 |
| `test_actual_configuration_or_egress_change_still_rejects_before_write` | B/live_drift_counter、live_drift_*；TestR08PreviewAndOwnershipGuards。HTTP 409/总 fingerprint 属于 R12/R15。 |

## test_policy_editors.py (6)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_content_edit_preserves_metadata_profiles_and_rejects_stale_edits` | C33 rule-library draft-content，非 R08。 |
| `test_content_endpoint_requires_authentication` | C33 rule-library draft-content，非 R08。 |
| `test_other_source_changes_are_not_overwritten` | C33 rule-library draft-content，非 R08。 |
| `test_invalid_content_edit_is_atomic` | C33 rule-library draft-content，非 R08。 |
| `test_editor_roles_use_manifest_references_even_if_live_names_changed` | TestR08PreviewAndOwnershipGuards；Python boundary live_drift_moved-list + view。 |
| `test_edited_bundle_stays_owned_and_can_be_withdrawn_without_touching_manual_objects` | TestR08PythonBoundary/replace_withdraw_*、live_drift_*；ReadPolicyObjects roles 按 manifest。 |

## test_ros_policy_bulk.py (16)

| Python test | Go 覆盖 / 边界 |
|---|---|
| `test_thousands_of_destinations_cost_batches_not_one_request_per_member` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_batches_stay_under_the_script_limit_and_escape_values` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_host_bits_and_single_addresses_match_router_normalization` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_reported_failures_stop_and_journal_only_created_members` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_script_without_completion_marker_is_a_failure` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_lost_reply_waits_for_the_batch_to_settle_before_journaling` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_list_that_keeps_growing_after_a_lost_reply_is_left_for_inspection` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_rows_nobody_requested_are_never_journaled` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_bulk_delete_guards_every_member_before_removing_any` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_partial_bulk_delete_recreates_removed_members_and_remaps_the_manifest` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_withdrawal_removes_members_in_batches_after_the_entry_rule` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_sweep_subtract_matches_pairwise_subtraction` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_preview_with_thousands_of_previous_and_current_destinations_is_fast` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_failed_readback_after_a_batch_still_rolls_back_every_written_member` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_one_transient_read_failure_is_retried` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |
| `test_rollback_keeps_new_ids_of_members_recreated_before_a_failure` | R07 共享 TestAddressWriteBoundary/TestAddressListBoundary/TestAddressBatchLimitsUseUTF8Bytes/TestAddressRestoreRetainsPartialNewIDs 等；R08 B/large_bundle_{200,201,4096}、batched_route_migration_rollback。 |

