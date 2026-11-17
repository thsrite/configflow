# R10 Python 测试 → Go 边界映射

按 named test 计数（参数化组合另计）；三个 Python 模块的功能完整移植。标注 R11/R12/R14 的是测试中跨模块的一半，不能由 R10 的 helper 测试冒充完整 HTTP/发布验收。

## test_ipv6_policy.py — 23 named tests

| Python 测试 | Go 边界 / 所属模块 | 说明 |
|---|---|---|
| `test_first_terminating_match_wins_and_reports_id_comment` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_disabled_or_invalid_rules_are_not_effective` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_default_accept_is_explicitly_scoped_to_rule_table` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_jump_return_resumes_caller_and_accept_terminates_caller` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_missing_and_cyclic_jump_targets_are_unknown` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_unknown_condition_is_not_silently_ignored` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_unknown_matching_branch_can_only_merge_identical_outcomes` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_address_lists_and_negation_use_members_not_names` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_partial_prefix_overlap_does_not_sample_one_fake_address` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_interface_list_include_exclude_then_explicit_member_order` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_missing_interface_list_and_read_errors_remain_unknown` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_protocol_ports_and_icmp_types_are_checked` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_nat_exemption_must_precede_translation_and_match_actual_exit` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_unrelated_drop_does_not_make_matching_node_path_warning` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_raw_drop_and_mangle_marks_are_not_hidden_by_filter_accept` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_policy_routes_and_missing_extra_tables_do_not_claim_main_path` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_no_client_or_literal_node_never_invents_endpoints` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_nat_return_and_same_lan_return_are_not_claimed_verified` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_registered_client_address_candidate_is_not_reported_as_observed_flow` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_fake_ip_exit_interface_alone_does_not_prove_correct_mihomo_gateway` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_input_control_classes_are_separate_from_forward_rules` | `TestR10PolicyPythonBoundary` + `TestR09SharedEvaluatorPython` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_added_policy_read_failure_does_not_disable_existing_fake_route_plan` | R10：unread_filter/raw_unread、nil/error environment；完整 readiness.inspect → **R11** | R11 仍需整合逐表读错误，不宣称此 reader 已移植 |
| `test_only_address_lists_referenced_by_rules_are_read` | R10 `TestR10RawWireAndProductionTimeouts`；全 readiness 读表 → **R11** | 已经证明 IPv6 %20 查询 raw bytes；R11 需复用按引用列表读取 |

## test_router_ipv6_fakeip.py — 31 named tests

| Python 测试 | Go 边界 / 所属模块 | 说明 |
|---|---|---|
| `test_missing_route_appears_in_diff_and_release_manifest_without_touching_ipv4` | R10 fake/report_missing_synced + publish_idempotent；阶段顺序 → **R12** | 完整 report/ops/设备表已对照，整体阶段插入不在 R10 |
| `test_managed_route_is_idempotent_and_noop_stage_still_verifies` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_correct_existing_route_is_borrowed_without_comment_adoption` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_disabled_feature_withdraws_only_owned_route_without_online_host` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_both_ipv6_modes_publish_the_route_so_switching_modes_leaves_the_router_alone` | `TestR10SettingsAndYAMLBytesPython` + fake nodes_only/no_tun/withdraw | 读取 YAML 与完整 settings；缺失 range 的 Python 内部异常文本单列修正 |
| `test_tun_is_required_only_while_clients_receive_ipv6_fake_ip` | `TestR10SettingsAndYAMLBytesPython` + fake nodes_only/no_tun/withdraw | 读取 YAML 与完整 settings；缺失 range 的 Python 内部异常文本单列修正 |
| `test_missing_range_is_routed_only_once_clients_need_it` | `TestR10SettingsAndYAMLBytesPython` + fake nodes_only/no_tun/withdraw | 读取 YAML 与完整 settings；缺失 range 的 Python 内部异常文本单列修正 |
| `test_conflicting_or_inactive_existing_routes_block_instead_of_overwriting` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_duplicate_correct_routes_are_not_adopted` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_missing_or_ambiguous_next_hop_is_visible_and_blocks_publish` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_link_local_next_hop_is_scoped_to_unique_ros_lan` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_new_managed_route_prefers_link_local_over_available_ula` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_existing_correct_ula_route_is_preserved_when_link_local_becomes_preferred` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_new_route_blocks_ambiguous_link_local_even_with_unique_ula` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_existing_ula_on_wrong_ros_interface_is_not_preserved` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_lan_overlap_and_missing_tun_are_blockers` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_invalid_saved_range_does_not_fall_back_or_withdraw_previous_route` | `TestR10SettingsAndYAMLBytesPython` + fake nodes_only/no_tun/withdraw | 读取 YAML 与完整 settings；缺失 range 的 Python 内部异常文本单列修正 |
| `test_external_edit_to_owned_route_prevents_removal_or_replacement` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_preview_fingerprint_tracks_ipv6_scope_without_route_writes` | R10 preview_address/actual-interface；总 fingerprint → **R12** | 拒绝实际地址变化，比较完整 plan.evidence |
| `test_apply_rechecks_the_ipv6_snapshot_before_writing` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_apply_rechecks_address_actual_interface_before_writing` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_inactive_created_route_is_rolled_back_and_not_registered` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_lost_write_reply_preserves_unproven_object_without_claiming_ownership` | `TestR10PythonBoundary/fake` | 真实 Python public entry 对照；必要的跨阶段/HTTP 部分由 R12/R15 接线 |
| `test_later_stage_failure_rolls_back_new_route_and_manifest` | fake/rollback + `TestR10RestoreFailureKeepsPublishedOwnership`；跨阶段触发 → **R12** | 相同 driver undo 已测；没有虚构完整 Execute |
| `test_replacing_owned_route_rolls_back_to_remapped_previous_id_on_later_failure` | fake/replace_rollback；跨阶段日志 → **R12** | 严格比较恢复 ID/manifest；整体失败日志由 R12 |
| `test_runtime_route_lifetimes_do_not_change_preview_fingerprint` | R10 preview_lifetime + `TestR10IPv6AddressMathPython` / `IPv6Evidence` | lifetime 排除于 stable，active 不排除 |
| `test_unordered_route_table_does_not_invalidate_preview` | `IPv6Evidence` 按 ID 排序 + R10 evidence；完整 fingerprint → **R12** | 纯路由顺序不作转发顺序，防火墙顺序保留 |
| `test_corrupted_manifest_spec_is_not_silently_accepted_as_a_successful_noop` | `TestR10ManifestSpecAndUnknownFields` | 有效形状但 spec gateway 与 resource 不同也阻断 |
| `test_noop_preview_also_fingerprints_the_owned_manifest` | `TestR10ManifestSpecAndUnknownFields` | check.previous 保留完整 raw；最终总 fingerprint → R12 |
| `test_drop_invalid_on_the_lan_port_is_reported_for_fake_ip_with_one_shared_fix` | fake/routes same_port_drop + R09 forward cases；warning 去重 → **R12** | 完整 fix command 已差分；总 stages/warnings 去重不在 R10 |
| `test_counting_down_address_lifetimes_do_not_invalidate_a_preview` | R10 preview_lifetime + `TestR10IPv6AddressMathPython` | valid/preferred/host bits/actual-interface 逐字段对照 |

## test_router_ipv6_routes.py — 16 named tests

| Python 测试 | Go 边界 / 所属模块 | 说明 |
|---|---|---|
| `test_desired_lists_public_ipv6_separately_and_auto_sync_keeps_it` | 已有 R01 desired / routersplit / R14 rule-sync | 分类/聚合不在三个 Python 模块；R10 只接 typed Desired.IPv6CIDRs |
| `test_publish_batches_routes_to_the_scoped_link_local_next_hop_and_is_then_in_sync` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_ula_is_the_fallback_and_ambiguous_next_hops_skip_only_this_item` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_skipped_item_warns_without_blocking_the_rest_of_the_release` | routes unavailable_host/no_tun/ambiguous_host；整体阶段跳过 → **R12** | plan.Skip/Check:nil/Ops:[] 已差分；不声称整体 release 已实现 |
| `test_turning_mihomo_ipv6_off_withdraws_only_owned_routes_in_one_remove` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_existing_routes_are_borrowed_or_left_alone_and_router_prefixes_are_never_routed_away` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_removed_destinations_are_withdrawn_unless_the_rule_input_is_incomplete` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_new_next_hop_replaces_routes_and_requires_a_manual_release` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_outside_changes_skip_the_item_or_reject_a_stale_preview` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_inactive_new_route_rolls_back_every_write_and_keeps_the_old_manifest` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_failed_readback_restores_removed_routes_under_new_ids_and_remaps_ownership` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_invalid_manifest_is_never_trusted` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_settings_follow_mihomo_default_and_explicit_switch` | `TestR10SettingsAndYAMLBytesPython` + fake nodes_only/no_tun/withdraw | 读取 YAML 与完整 settings；缺失 range 的 Python 内部异常文本单列修正 |
| `test_ikuai_reports_unhandled_ipv6_and_routeros_asks_the_agent_once` | R01 desired / **R12** inspection | R10 Relevant/NeedsHost 提供条件；iKuai 警告及两功能共享一次 Agent 请求由 R12 |
| `test_owned_ipv6_routes_guard_router_edits_and_are_persisted_with_the_release` | repo `TestR10...` + routers Normalize；自动同步持久化 → **R12/R14** | publication 保存/clear/identity 保护两种 IPv6 manifest 均测 |
| `test_drop_invalid_before_a_same_port_accept_is_reported_with_a_copyable_fix` | `TestR10PythonBoundary/routes` | 完整 evidence/manifest 或真实 repo 状态断言 |

## test_router_ipv6_manifest_safety.py — 8 named tests

| Python 测试 | Go 边界 / 所属模块 | 说明 |
|---|---|---|
| `test_every_writer_preserves_ipv6_route_ownership` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_publication_persists_readback_then_withdrawal_without_losing_other_manifests` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_ipv6_publication_rejects_a_changed_connection` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_credentials_can_be_repaired_without_abandoning_ipv6_ownership` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_destructive_changes_wait_for_ipv6_route_withdrawal` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_clone_has_no_authority_over_source_ipv6_routes` | repo `TestR10EveryWriterPreservesIPv6Ownership` / `TestR10IPv6PublicationPersistenceAndWithdrawal` / `TestR10IPv6ConnectionRepairCloneAndDeleteGuards` | 完整 evidence/manifest 或真实 repo 状态断言 |
| `test_settings_cannot_replace_receipt_and_exports_do_not_disclose_it` | 已有 R01 Normalize/Public/RedactRouters corpus；R10 ownership tests | R01 已有私有字段列表含 ipv6_fakeip_state/ipv6_routes_state；R10 不新增公开元数据 |
| `test_settings_require_withdrawal_before_changing_router_identity` | 已有 R01 Normalize；repo `TestR10EveryWriterPreservesIPv6Ownership` | endpoint/type 丢 ownership 拒绝 |


额外 Python bug 修正回归：`TestR10FailedApplyRetainsProvenCreationWhenDeleteRefused`（fake/routes）验证初次部分 apply 的补偿 DELETE 拒绝后保留 birth config/可信 ID；mutation gate 的 recovery 破坏必须触发该测试失败。

额外 borrowed 运行时校验：`TestR10BorrowedRouteMustRemainActive`（fake/routes）验证仅 active 失效、stable config 不变时也拒绝 verify，修复 Python rule-set borrowed 漏检；定向 race 通过。
