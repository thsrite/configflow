# PROGRESS — domain `agents`

Domain `agents` (WORKFLOW.md): Agent registry/heartbeat, metrics/traffic, OxiDNS/Mihomo monitoring, installers, Docker HA, deployment bundle, push diff, content sync, publishing, upgrades, address follow, rule sync, DNS follow, DNS HA, Linux gateway, gateway onboarding/installation, IPv6 readiness (old packages #20–#31). **69 Python files, 25154 lines, 24 modules** (equal to the `agents` totals in `FILES.md`) — 21 missing, 2 partial, 1 ported. `verified`: 0 (only reviewers set it).

Planning audit: 2026-11-15, worktree `configflow-go`, branch `go` at 496004d7. The Go code was read directly (`internal/agents`, `internal/api/agents`, `internal/notifications`, `internal/tasks`, `internal/server`); the hand-back in `progress/20-agents.md` was not relied on.

## Conventions

Same as `PROGRESS-config.md`: status `missing` / `partial` (exact list of what is missing) / `ported` (code and tests exist, not reviewed) / `verified` (reviewer only). A test file is listed under the module that owns its subject; cases that need a later module are **deferred** and listed in the section of the module that makes them runnable. Python test counts are `def test*` per file. Routes are rows of `server/parity/routes.tsv` (its "go package" column still carries the old package numbers; porters write the Go package when they set `done`).

`backend/routes/agents.py` (2388 lines) is split by line ranges (function groups, listed per module; ranges are contiguous and cover the whole file). `backend/agents/manager.py` (1787) is split into part A (A01) and part B (A14).

**HR** = high risk: a defect can break a user's network (publishing, rule sync, DNS follow, DNS HA, Linux gateway, installers, Agent upgrade/rollback). HR modules additionally need a **VM lab test after the maintainer agrees** (PVE lab, never production) before the domain is done.

## Module table

| Id | Module | Py lines | Status | HR | Prereqs | Routes | Go package(s) |
|---|---|---|---|---|---|---|---|
| A01 | Agent manager (manager.py part A) | 1642 | partial | HR | C04 | – | internal/agents |
| A02 | registry support: runtime state, diagnostics, events, versions, messages | 1386 | ported | | C02 | – | internal/agents |
| A03 | registry routes (register, heartbeat, create/edit/delete) | 432 | partial | | A01, A02, C06 | 5 (5 done) | internal/api/agents |
| A04 | metrics history and flush | 1163 | missing | | A01, C07 | 3 | internal/agents/metrics (new), api/agents |
| A05 | traffic history and traffic store | 967 | missing | | A04, C37¹ | 8 | internal/agents/metrics, api/agents |
| A06 | OxiDNS monitor, dashboard state, metrics | 1364 | missing | | A01 | 5 | internal/agents/monitor (new), api/agents |
| A07 | Mihomo panel, OxiDNS cache/rule update/rankings/publication stats | 1316 | missing | | A06, C14 | 9 | internal/agents/monitor, api/agents |
| A08 | installer script rendering, install command store | 1333 | missing | HR | A02, C02 | – | internal/installer (new) |
| A09 | Docker HA guide, preflight, enrollment | 1083 | missing | HR | A08, A01 | 2 | internal/installer, api/agents |
| A10 | Docker HA compose variants, installer routes | 1018 | missing | HR | A08, A09 | 9 | internal/installer, api/agents |
| A11 | deployment bundle, locations, Mihomo resources, generated preview | 1264 | missing | HR | A02, A19, C05, C12, C14, C15, C21, C22, C25 | – | internal/deploy (new) |
| A12 | push diff | 1465 | missing | | A11 | – | internal/deploy |
| A13 | content sync, Agent list/detail, push-diff routes | 584 | missing | | A12, A15 | 4 | internal/deploy, api/agents |
| A14 | publishing: transactional content, push-config, deployments, receipts | 1002 | missing | HR | A11, A13, C13, C17, C18, C19, C20, C22, C23 | 9 | internal/deploy, api/agents |
| A15 | upgrades, Agent operations, address follow | 745 | missing | HR | A01, A02 | 15 | internal/agents/upgrades (new), api/agents |
| A16 | Agent rule sync, projection, snapshot | 1371 | missing | HR | A07, A14, C25, C27 | 2 | internal/agents/rulesync (new), api/agents |
| A17 | DNS follow | 760 | missing | HR | A14, C26 | 4 | internal/agents/dnsfollow (new), api/agents |
| A18 | DNS HA (keepalived pair, handoff, pair publication) | 1529 | missing | HR | A16, A17, C26, routers (`routers/store.py`) | 4 | internal/agents/dnsha (new), api/agents |
| A19 | Linux gateway core (common/linux_gateway.py) | 518 | missing | HR | C05, C08, routers (`router_split.check_generation`) | – | internal/gateway (new) |
| A20 | Agent gateway bridge, gateway draft, gateway routes | 1110 | missing | HR | A19, A06, C18, routers (`router_split`, `routers/store.py`) | 18 | internal/gateway, api/agents, api/linuxgateway (new) |
| A21 | gateway onboarding | 751 | missing | HR | A19, A20, C18, C22, C23, C24, C05, routers (`router_split`) | 3 | internal/gateway, api/gatewayonboarding (new) |
| A22 | gateway installation | 809 | missing | HR | A08, A14, A20, A21, C17 | 7 | internal/gateway, api/agents |
| A23 | IPv6 readiness, client evidence, verification | 1344 | missing | HR | A06, A13, A14, C16, routers (`ipv6_readiness`, `routeros`, `route_editor`, `rule_sync_store`, `store`, `client_devices`) | 5 | internal/agents/ipv6 (new), api/agents |
| A24 | client tools, OxiDNS device policies, domain-discovery toggles | 198 | missing | | A05, C09, C26, C31, C37 | 8 | api/agents |

¹ A05 needs only `utils/domain_discovery.registrable_domain` (C37's file; a thin wrapper over `subs.ExtractDomain`). Ask the coordinator to have it exported early from the C37 package, or port A05 after C37.

Totals: 24 modules, 25154 Python lines (= FILES.md `agents`: 69 files, 25154 lines). Routes: 120 rows of routes.tsv (81 `routes.agents`, 4 `routes.client_tools`, 2 `routes.docker_ha_installation`, 7 `routes.gateway_installations`, 3 `routes.gateway_onboarding`, 5 `routes.ipv6_readiness`, 14 `routes.linux_gateway`, 4 `routes.linux_gateway_agents`); 5 done.

Module sizes outside 300–1500: A01 1642 (part of a file larger than 1500 lines; allowed), A18 1529 (three tightly coupled HA files), A24 198 (kept separate because its config prerequisites C26/C31/C37 come last; merging it would block an otherwise early module).

## File → module list (all 69 files)

| File | Lines | Module |
|---|---|---|
| `backend/agents/__init__.py` | 10 | A02 |
| `backend/agents/address_follow.py` | 221 | A15 |
| `backend/agents/config_generator.py` | 47 | A14 |
| `backend/agents/content_sync.py` | 460 | A13 |
| `backend/agents/core_install_script.py` | 40 | A08 |
| `backend/agents/deployment_bundle.py` | 834 | A11 |
| `backend/agents/deployment_locations.py` | 106 | A11 |
| `backend/agents/deployment_messages.py` | 97 | A02 |
| `backend/agents/diagnostics.py` | 137 | A02 |
| `backend/agents/dns_follow.py` | 730 | A17 |
| `backend/agents/docker_ha_compact.py` | 182 | A10 |
| `backend/agents/docker_ha_compose.py` | 346 | A10 |
| `backend/agents/docker_ha_compose_direct.py` | 96 | A10 |
| `backend/agents/docker_ha_enrollment.py` | 165 | A09 |
| `backend/agents/docker_ha_guide.py` | 475 | A09 |
| `backend/agents/docker_ha_preflight.py` | 376 | A09 |
| `backend/agents/gateway_install_script.py` | 176 | A08 |
| `backend/agents/gateway_installation.py` | 727 | A22 |
| `backend/agents/generated_preview.py` | 159 | A11 |
| `backend/agents/go_install_script.py` | 525 | A08 |
| `backend/agents/idle_rounds.py` | 46 | A02 |
| `backend/agents/install_command_store.py` | 99 | A08 |
| `backend/agents/install_output.py` | 45 | A08 |
| `backend/agents/install_script.py` | 448 | A08 |
| `backend/agents/ipv6_client_evidence.py` | 176 | A23 |
| `backend/agents/ipv6_readiness.py` | 492 | A23 |
| `backend/agents/ipv6_verification.py` | 613 | A23 |
| `backend/agents/linux_gateway.py` | 668 | A20 |
| `backend/agents/manager.py` | 1787 | A01 (1–1012, 1158–1787 = 1642), A14 (1013–1157 = 145) |
| `backend/agents/metrics_flush.py` | 260 | A04 |
| `backend/agents/metrics_history.py` | 810 | A04 (`LEGACY_DIRS` handling not ported — maintainer decision) |
| `backend/agents/mihomo_panel.py` | 375 | A07 |
| `backend/agents/mihomo_resources.py` | 165 | A11 |
| `backend/agents/operation_events.py` | 84 | A02 |
| `backend/agents/oxidns_cache.py` | 134 | A07 |
| `backend/agents/oxidns_dashboard_state.py` | 240 | A06 |
| `backend/agents/oxidns_ha.py` | 625 | A18 |
| `backend/agents/oxidns_ha_handoff.py` | 167 | A18 |
| `backend/agents/oxidns_ha_publish.py` | 676 | A18 |
| `backend/agents/oxidns_metrics.py` | 338 | A06 |
| `backend/agents/oxidns_monitor.py` | 727 | A06 |
| `backend/agents/oxidns_publication_stats.py` | 158 | A07 |
| `backend/agents/oxidns_ranking_history.py` | 402 | A07 |
| `backend/agents/oxidns_rule_update.py` | 166 | A07 |
| `backend/agents/oxidns_validation_receipt.py` | 76 | A14 |
| `backend/agents/presence_log.py` | 36 | A02 |
| `backend/agents/push_diff.py` | 1465 | A12 |
| `backend/agents/rule_sync.py` | 643 | A16 |
| `backend/agents/rule_sync_projection.py` | 608 | A16 |
| `backend/agents/rule_sync_snapshot.py` | 98 | A16 |
| `backend/agents/rule_sync_store.py` | 147 | A14 |
| `backend/agents/runtime_state.py` | 204 | A02 |
| `backend/agents/traffic_history.py` | 504 | A05 |
| `backend/agents/traffic_store.py` | 265 | A05 |
| `backend/agents/upgrade_messages.py` | 147 | A02 |
| `backend/agents/upgrades.py` | 310 | A15 |
| `backend/agents/version.py` | 597 | A02 |
| `backend/common/agent_manager.py` | 28 | A02 |
| `backend/common/gateway_onboarding.py` | 660 | A21 |
| `backend/common/linux_gateway.py` | 518 | A19 |
| `backend/common/linux_gateway_draft.py` | 165 | A20 |
| `backend/routes/agents.py` | 2388 | split, see below |
| `backend/routes/client_tools.py` | 105 | A24 |
| `backend/routes/docker_ha_installation.py` | 67 | A09 |
| `backend/routes/gateway_installations.py` | 82 | A22 |
| `backend/routes/gateway_onboarding.py` | 91 | A21 |
| `backend/routes/ipv6_readiness.py` | 63 | A23 |
| `backend/routes/linux_gateway.py` | 203 | A20 |
| `backend/routes/linux_gateway_agents.py` | 74 | A20 |

`backend/routes/agents.py` line ranges:

| Lines | Functions | Module |
|---|---|---|
| 1–161 | imports, constants, `_bounded_log_value`, `_registration_log_fields`, `_valid_metric_number`, `_validate_system_metrics`, `_validate_heartbeat_payload`, `_constant_time_ascii_equal`, `_supports_self_restart`, `_public_agent` | A03 |
| 162–191 | `agent_dns_follow`, `retry_agent_dns_follow` | A17 |
| 192–215 | `agent_oxidns_ha` | A18 |
| 216–245 | `_public_deployment`, `_agent_with_content_sync` | A13 |
| 246–537 | `get_core_install_script`, `get_gateway_network_probe`, install-command constants, `_require_go_agent`, `_install_command_params`, `_save_install_command`, `create_install_command`, `create_core_install_command`, `download_install_command`, `get_install_script`, `_install_script_response` | A10 |
| 538–727 | `_registration_rejected`, `register_agent`, `_authenticated_agent`, `_read_json_body`, `_domain_discovery_enabled`, `_traffic_history_enabled`, `agent_heartbeat` | A03 |
| 728–750 | `agent_traffic_report` | A05 |
| 751–780 | `set_agent_domain_discovery`, `clear_agent_domain_discovery` | A24 |
| 781–884 | `agent_traffic_history_report`, `_traffic_history_state`, `_mihomo_agent_or_error`, `get/set/clear_agent_traffic_history`, `set_traffic_history_retention` | A05 |
| 885–920 | `get_agent_config` | A14 |
| 921–952 | `download_agent_binary` | A10 |
| 953–1033 | `handle_agents`, `handle_agent_item` (POST/PUT/DELETE here; their GET branches are A13's — one handler per endpoint, see A13) | A03 |
| 1034–1049 | `agent_address` | A15 |
| 1050–1232 | `restart_agent`, `get_agent_status`, `get_agent_logs`, `get_agent_log_sources`, `clear_agent_log`, `validate_log_path`, `get/set_logging_config`, `uninstall_agent`, `update_agent_version`, `get_agent_upgrade`, `report_agent_upgrade` | A15 |
| 1233–1269 | `agent_oxidns_ha_environment` | A18 |
| 1270–1339 | `get_docker_agent_compose`, `get_docker_agent_run` | A10 |
| 1340–1632 | `_profile_revision`, `_generated_config_failure`, `_store_subscription_downloads`, `_saved_subscriptions_only`, `_prepare_transactional_content`, `_prepare_content_bundle`, `generate_final_agent_config`, `download_final_agent_config`, `read_final_agent_file` | A14 |
| 1633–1726 | `agent_push_diff`, `agent_push_diff_entries` | A13 |
| 1727–1748 | `agent_rule_sync` | A16 |
| 1749–2006 | `push_config_to_agent`, `_deployment_response`, `get/activate/rollback/recover_agent_deployment` | A14 |
| 2007–2099 | `get_agent_metrics`, `get_agent_metrics_history`, `get_agent_metrics_summary` | A04 |
| 2100–2170 | `get_agent_traffic_stats`, `get_agent_traffic_trend` | A05 |
| 2171–2185 | `get_latest_agent_version` | A15 |
| 2186–2198 | `_oxidns_monitor_response` | A06 |
| 2199–2215 | `mihomo_panel` | A07 |
| 2216–2221 | `get_oxidns_overview` | A06 |
| 2222–2250 | `update_oxidns_rules` | A07 |
| 2251–2285 | `oxidns_cache` | A07 |
| 2286–2325 | `reset_oxidns_dashboard`, `get_oxidns_clients`, `get_oxidns_queries`, `get_oxidns_query` | A06 |
| 2326–2388 | `oxidns_device_policies`, `PolicyConflict` | A24 |

## Tests owned elsewhere that need agents modules (deferred cases)

Config-owned test files (see PROGRESS-config.md) with cases that run only when an agents module exists: `test_operation_logs.py` (C02: presence, agent rule sync, DNS follow, deployment-result logs → A02/A16/A17/A14), `test_safe_url_queries.py` (C02: generated Go installer → A08), `test_profile_agent_bindings.py` (C04: route cases with C34), `test_save_publish_guard.py` (C04: push → A14), `test_subscription_hosts.py` (C10: content sync → A13), `test_proxy_server_dns.py` (C11: outdated core warnings → A13/A14), `test_dns_watchdog_config.py`, `test_dns_watchdog_core.py` (C25: bundle → A11, receipt → A14), `test_mihomo_base_settings.py` (C18: gateway onboarding → A21), `test_setup_flow.py` (C36: A19–A22), `test_domain_discovery.py`, `test_domain_background_lifecycle.py` (C37: traffic store → A05), `test_region_check.py`, `test_domain_probe.py` (C16: config generator → A14), `test_agent_log_redaction.py`, `test_profile_generation.py` (C34: routes/agents → A14), deployment-content tests `test_domain_wildcard_*`, `test_mosdns_rule_fields.py`, `test_mihomo_classical_delivery.py`, `test_ruleset_domain_format.py`, `test_remote_rule_consumers.py`, `test_udp_fallback.py`, `test_oxidns_*` (config C22–C29: bundle → A11, routes → A14). Routers-owned files that import agents code: `test_router_split.py` (A20 constants), `test_router_ipv6_integration.py` (A23), `test_client_devices.py`, `test_client_discovery.py` (agent manager only). `test_node_probe_packaging.py` tests `docker/auto_update.py` → finish domain.

## A01 — Agent manager (manager.py part A)

**Status: `ported`** (2026-11-15, porter; not reviewed) · HR · prereqs: C04 · Go: internal/agents (manager.go, deployments.go, remote.go, hooks.go, client.go, compat.go; singleton.go is shared with A02)

Files:
- `backend/agents/manager.py` (1642) — lines 1–1012 and 1158–1787: `_agent_label`, `_constant_time_ascii_equal`, `heartbeat_fresh`, `online_status`, `is_online`, `is_usable`, `AgentDeploymentConflict`, `_NothingToSave`, `_upgrade_busy`, `_oxidns_ha_enabled`, `_oxidns_ha_busy`, `_dns_follow_busy`; `AgentManager.__init__` … `_cancel_manual_rollback` (registry, heartbeat, preparation, begin/record/unchanged/get/activate/rollback/recover deployment) and `push_config_to_agent` … `region_check` (legacy push, restart, status, logs, uninstall, update, logging config, domain discovery and traffic toggles, probe/return-routing/region/OxiDNS HA Agent calls). Part B (1013–1157: `_remember_deployment_content`, `publish_deployment`, `_publish_deployment`) is A14.

Tests: `test_agent_deployments.py` (50), `test_agent_deployment_writes.py` (2), `test_agent_failure_details.py` (10), `test_agent_dns_reports.py` (6), `test_agent_failure_response_states.py` (7), `test_agent_profiles.py` (6), `test_agent_light_reads.py` (9)

Routes: none (routes are A03/A14/A15).

Commits (porter, 2026-11-15): 3b39c317 (test gaps, logging-config fix, notifications gate, ordered delete hooks), 8507a6f4 (one `ConstantTimeASCIIEqual`), 14cc7e3f (DNS report cases), plus earlier ee00305e/1dd4e700/70bd31df (#20).

Tests ported (Go file → Python cases):
- `failure_states_test.go`: `test_agent_failure_response_states.py` 6/7 (`TestLegacySuccessHTTPRequiresAJSONObject` 2×4, `TestLegacyInvalidJSONKeepsSafePlainTextExplanation`, `TestConfirmedAgentStageDiscardsOldUnknownTransportDetails` ×4, `TestConfirmedFailureReplaces…`, `TestRollbackRetryClearsOldRejectionDetailsBeforeRequest`, `TestDeploymentBusyRetryIsUnchanged…`); `test_agent_deployment_writes.py` 2/2; `test_agent_failure_details.py::test_yaml_secret_block_does_not_hide_sibling_diagnostics`; `test_agent_dns_reports.py::test_dns_report_is_bounded_filtered_and_redacted_at_each_boundary` (both boundaries; manager only, not A14 as planned) and `test_malformed_or_absent_report_does_not_erase_previous_result`.
- `deployments_test.go` (existing): `test_agent_deployments.py` 22, failure details 6, DNS reports 2 + `TestDNSReportFieldsBoundsAndIncompleteness`. `test_profile_binding_route_reports_deployment_conflict_as_409` is in `api/agents.TestAgentItemUpdateAndDelete` (A03).
- `registry_test.go`: `test_agent_profiles.py` 4/6 (new: `TestAgentRegistrationDoesNotOverwriteSystemAgentsFromProfileSnapshot`, request scope with `X-ConfigFlow-Profile: alpha` and a cached snapshot); `test_agent_light_reads.py` 4/9 (`TestRecordsWithoutHistoryEqualTheFullOnesInEveryOtherField`, `TestManagerReadsLay…` + `TestManagerListsLay…`, `TestEveryUseOfUnreadHistoryFails` and `TestUnreadHistorySurvivesCopiesButIsNeverSerialized` + `TestUnreadHistoryIsNeverSaved` — Go equivalents, see decisions), `TestConstantTimeASCIIEqual`. The heartbeat half of `test_heartbeat_and_agent_reports_never_copy_the_history` is `api/agents.TestHeartbeatKeepsThePublicationHistory`.
- `remote_test.go` (new; Python has no tests): status/log sources/logs URL quoting/uninstall/update/logging config/`_agent_json_request`/DNS HA calls, expected values from running manager.py (`testdata/remote_ref.py`, run with `rt -- bash -lc '~/cf-test/.venv/bin/python -I -c "import sys; sys.path.insert(0, \".\"); exec(open(\"server/internal/agents/testdata/remote_ref.py\").read())"'`).

Deferred (each listed in the named module's section by reference "deferred from A01"):
- **A11** (bundle): `test_agent_deployments.py` `test_panel_geo_*` (2), `test_bundle_*` (3), `test_inline_rule_*` (2), `test_failed_required_download_*`, `test_mosdns_*` (7), `test_classical_ip_part_*`, `test_oxidns_names_each_unavailable_ruleset_*`, `test_rule_download_failure_*`, `test_custom_installed_filename_*`.
- **A14** (publish/routes): `test_agent_deployments.py` `test_upload_ready_does_not_mark_version_*`, `test_old_agents_fail_closed_*`, `test_unknown_upload_keeps_queryable_identity`, `test_late_upload_acceptance_*`, `test_route_client_id_persisted_*`, `test_route_preparation_progress_*`, `test_manual_rollback_route_*`; `test_agent_failure_response_states.py::test_immediate_rejections_are_redacted_like_recorded_history` (deployment routes); `test_agent_failure_details.py` `test_capability_connection_failure_*`, `test_preparation_exception_*`; `test_agent_dns_reports.py` `test_upload_report_reaches_panel_*`, `test_older_agent_without_dns_report_remains_unknown` (publish_deployment, then GET list → A13); `test_agent_profiles.py` `test_agent_config_endpoint_uses_bound_profile`, `test_push_config_uses_bound_profile_*`.
- **A13** (list/detail): `test_agent_failure_details.py::test_failure_details_survive_restart_history_and_public_api`; `test_agent_light_reads.py::test_agent_list_and_detail_still_return_the_publication_records`.
- **A16**: `test_agent_light_reads.py` `test_idle_rule_sync_rounds_*`, `test_rule_sync_publication_works_from_a_record_without_history`. **A07/A06**: `test_panel_lookups_do_not_copy_the_history_*`. **A05**: the traffic-report half of `test_heartbeat_and_agent_reports_never_copy_the_history`.

Bugs fixed (Python behaviour reproduced): `set_logging_config` — a 200 JSON answer that is not an object raised AttributeError (`'list' object has no attribute 'get'`), and an existing non-dict `logging_config` raised TypeError (`'NoneType' object does not support item assignment` / `list indices must be integers or slices, not str`); Go silently returned the value / replaced the field. `_constant_time_ascii_equal` had two Go copies; now one exported `agents.ConstantTimeASCIIEqual` used by manager and routes.

**Agent delete — per-Agent stores (must not be lost when the owners are ported).** Python always removes these when an Agent is deleted; in Go each is a hook registered by its owner from its wire function (listed in `internal/server` `wirePackages`):

| Python | Go hook | Registered by |
|---|---|---|
| `AgentManager.delete_agent` → `metrics_history.delete_agent_history` | `agents.RegisterHooks(Hooks{DeleteMetricsHistory})` | **A04** |
| `AgentManager.delete_agent` → `traffic_history.delete` (warning `删除 Agent %s 的流量记录失败：%s` is logged by the registry) | `Hooks{DeleteTrafficHistory}` | **A05** |
| route → `get_traffic_store().delete` | `agents.OnAgentDeleted("traffic_store", fn)` | **A05** |
| route → `get_probe_store().delete` | `agents.OnAgentDeleted("domain_probe", fn)` | **C16** |
| route → `get_region_store().delete` | `agents.OnAgentDeleted("region_check", fn)` | **C16** |

`agents.AgentDeletedStores` fixes the run order (traffic_store, domain_probe, region_check, then any other name) whatever the wiring order; a store error answers 500 with its text after the Agent is gone (as Python). `internal/server.TestDeleteAgentClearsEveryPerAgentStoreInAFullyWiredServer` checks all five on `NewApp()` and is **skipped with `TODO(A05, C16, A04)`: the porter of the last of A04/A05/C16 removes the `t.Skip`**. Each of them should also seed its store files for `agent-1` in `api/agents/difftest` (the differential compares the files under `DATA_DIR`, lock files excluded).

Decisions:
- Light reads: Go's typed jsonx getters cannot raise on `repo.AgentHistoryNotRead` like Python's sentinel (documented on the sentinel); the Go test checks the equivalents: `repo.AgentHistory` refuses it, it is never an object/list to type assertions, every encoder refuses it, copies keep it, saving it fails.
- `test_agent_profiles` snapshot case: Go transactions always read the committed document, so this passes by construction; kept as a regression test.

Pitfalls:
- Hook defaults (unchanged, intended): `ReceiptMatches` nil → activation of every OxiDNS candidate is refused until A14; `AddMetrics` nil → heartbeat metrics dropped until A04; `PublishDeployment` → `ErrNotPorted`. A04/A05 must keep Python's **per-manager** history state: key it by `m.Repository().DataDir()`.
- `remote.go` answers 200-non-object JSON in `_agent_json_request` with a `*RemoteError` carrying the AttributeError text; Python raised AttributeError (not RuntimeError). Callers that distinguish the two (A06/A07/C16 routes) must check.
- Tests in `internal/agents` share process-wide hooks/runtime: every test starts with `newEnv` (resets both).

## A02 — registry support

**Status: `ported`** · prereqs: C02 · Go: internal/agents (runtime_state.go, diagnostics.go, operation_events.go, idle_rounds.go, presence_log.go, version.go, version_changelog.go, messages.go, messages_data.go, singleton.go, pytime.go)

Files:
- `backend/agents/__init__.py` (10)
- `backend/common/agent_manager.py` (28)
- `backend/agents/runtime_state.py` (204)
- `backend/agents/diagnostics.py` (137)
- `backend/agents/operation_events.py` (84)
- `backend/agents/idle_rounds.py` (46)
- `backend/agents/presence_log.py` (36)
- `backend/agents/version.py` (597)
- `backend/agents/deployment_messages.py` (97)
- `backend/agents/upgrade_messages.py` (147)

Tests: `test_agent_runtime_state.py` (28), `test_operation_events.py` (5), `test_deployment_messages.py` (5), `test_agent_upgrade_messages.py` (10), `test_agent_version_sync.py` (7), `test_idle_rounds.py` (5)

Routes: none.

Evidence (audit): all defs matched (`get_agent_manager`→`Get`, `init_agent_manager`→`Init` + `OnManagerInit`, `for_data_dir`→`RuntimeFor`, `import_legacy_database`→`ImportLegacyDatabase`, `observe`→`ObservePresence`, `_hide_block_secrets`/`_safe_diagnostic_url`/`failure_text`/`dns_report_fields`/`response_failure`, `get_latest_version`/`compare_versions`/`has_update`, message tables generated from Python by `testdata/gen_*.py`). `LatestAgentVersion` = `1.8.61-go`, 80 changelog entries as in Python. Tests: runtime 28/28, operation_events 5/5, idle_rounds 5/5 (GC part of the weak-key case skipped: `pure_test.go:433`), version_sync 7/7 (five `has_update` cases merged into `TestExistingAgentsCanUpdate`). `agents/__init__.py` only re-exports.

Deferred: `test_deployment_messages.py` 3 route cases (`test_display_copy_keeps_protocol_fields_and_both_failure_causes`, `test_list_history_and_poll_explain_failures_without_changing_saved_record`, `test_publish_preflight_errors_are_chinese_without_starting_a_task`) → A13/A14; `test_agent_upgrade_messages.py` 7 manager/route cases → A15.

Reviewer notes:
- `internal/notifications/compat.go` still holds copies of `operation_events.event_message`, `MESSAGE_LIMIT` and `diagnostics.failure_text` (TODO(port) → #20). They cannot simply import `internal/agents` (agents imports notifications → cycle). Move the pure helpers to a leaf package both import (e.g. `internal/agents/diag`) or keep the copy with a test that runs both on the same corpus. Decide in the review.
- Regenerating `version_changelog.go`/`messages_data.go` when Python's `version.py` changes is manual (see `progress/20-agents.md` Commands); the review should add a test that fails when the Python table and the Go table differ (the existing `TestGoAgentVersionMatchesLatestAgentVersion` covers only the version).

## A03 — registry routes

**Status: `ported`** (2026-11-15, porter; not reviewed) · prereqs: A01, A02, C06 · Go: internal/api/agents (agents.go, agents_test.go, difftest/, testdata/)

Files:
- `backend/routes/agents.py` (432) — lines 1–161, 538–727, 953–1033 (see range table)

Tests: `test_agent_registration_security.py` (10), `test_agent_heartbeat_security.py` (13)

Routes (5, 5 done): `POST /api/agents/register` [done]; `POST /api/agents/<agent_id>/heartbeat` [done]; `POST /api/agents` [done]; `PUT /api/agents/<agent_id>` [done]; `DELETE /api/agents/<agent_id>` [done]. (`GET /api/agents`, `GET /api/agents/<agent_id>` are A13; `_public_agent` is ported with A13.)

Commits (porter, 2026-11-15): 3b39c317, 2d49a9e6 (differential), 8507a6f4, f2777e0a; earlier 1dd4e700/277c7c53/70bd31df (#20).

Tests ported (`agents_test.go`):
- `test_agent_registration_security.py` 9/10: the 8 earlier ones + `TestDuplicateRegistrationAttackIsRejectedAfterRepositoryRestart` (second test server on the same data directory, runtime reset). `test_manager_*` cases are in `internal/agents/registry_test.go`.
- `test_agent_heartbeat_security.py` 11/13: non-ASCII/malformed bearer is merged into `TestHeartbeatRejectsUnauthorizedWithoutMutatingSystem` (all 9 header forms); malformed JSON / oversized merged into `TestHeartbeatRejectsMalformedJSONAndOversizedBody`; chunked into `TestHeartbeatWithoutContentLengthReadsOnlyMaxPlusOneBytes`; new `TestHeartbeatRejectsMalformedEncodingOrJSONWithoutMutation` (streamed, no Content-Length); `test_manual_agent_create_does_not_return_token_even_without_auth` = `TestManualAgentCreateDoesNotReturnToken`.
- `test_agent_deployments.py::test_profile_binding_route_reports_deployment_conflict_as_409` in `TestAgentItemUpdateAndDelete`.
- New (audit): `TestHeartbeatAcceptsBOMAndUTF16Bodies` (json.loads detects them), `TestAgentItemUpdateWithNonObjectBodies`, `TestAgentDeleteRunsStoreHooksInPythonOrder`, `TestRegistrationLogFieldsMatchPythonGolden` (`_registration_log_fields` on Zs/Cf/Zl/Zp/C1/private use/Unicode-15-only code points, length bounds, number texts; golden `testdata/registration_log_fields.json` from `testdata/gen_registration_log_fields.py`, regenerate with `rt --back server/internal/api/agents/testdata -- bash -lc '~/cf-test/.venv/bin/python server/internal/api/agents/testdata/gen_registration_log_fields.py'`), `TestHeartbeatKeepsThePublicationHistory`.

Deferred: `test_agent_list_and_item_never_expose_token`, `test_agent_token_is_redacted_from_ordinary_outputs_and_still_authorizes_heartbeat` (→ A13 GET routes; the export part → C34), `test_authenticated_global_backup_preserves_secrets_for_recovery` (→ C34 export). Heartbeat `host` follow (`address_follow.follow/confirmed`) runs through the A15 hooks; registration of gateway installations through the A22 hooks (501 until then; not in the differential).

Bugs fixed: PUT with a JSON string/list body — Python's `'profile_id' in updates` is a substring/membership test, so `"xprofile_idx"` fails with `string indices must be integers, not 'str'` and `["profile_id"]` with `list indices must be integers or slices, not str` (Go answered the `.get` AttributeError for every string/list). The registration-key check is skipped exactly when `installation_profile is None` (Go used "an installation id or token was sent").

Differential (`difftest/diff_test.go`, `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/'`): 74 requests (+21: non-ASCII/CJK/tab/trailing-field bearers on heartbeat and registration, oversized bearer, `\xff` bodies, UTF-8 BOM and UTF-16 heartbeats, NaN metric, registration and PUT 409 for an unfinished deployment, edit/delete 409 for a pending upgrade, string/list/bool PUT bodies), stored Agent records and the file list under `DATA_DIR` (lock files excluded: Flask starts every background loop): **0 differences**.

Decisions:
- `register_agent` 500 path: Python logs `错误详情: <traceback>`; Go logs the error text on that line (no Python traceback to reproduce). Log-only difference, accepted.
- 500 bodies use Go error texts where Python uses `str(e)` for non-repository errors (repository errors are ported texts); the differential covers every 500 the routes can produce from request data.

Pitfalls:
- Endpoint reuse: `httpx.Router` panics when an endpoint name is registered twice with different handlers, so A13 must extend `createAgent`/`handleAgentItem` (one handler per endpoint `agents.handle_agents` / `agents.handle_agent_item`) instead of registering a second GET handler.
- `_bounded_log_value` relies on `logx.IsPrintable` (CPython 3.11 table); the golden test fails if it drifts.

## A04 — metrics history and flush

**Status: `missing`** · prereqs: A01, C07 · Go: internal/agents/metrics (new), api/agents

Files:
- `backend/agents/metrics_history.py` (810) — `LEGACY_DIRS` handling not ported (maintainer decision)
- `backend/agents/metrics_flush.py` (260)
- `backend/routes/agents.py` (93) — lines 2007–2099

Tests: `test_agent_metrics_history.py` (25)

Routes (3): `GET /api/agents/<agent_id>/metrics`; `GET /api/agents/<agent_id>/metrics/history`; `GET /api/agents/<agent_id>/metrics/summary`

Evidence: no Go code.

Hooks to provide: `agents.RegisterHooks{AddMetrics, DeleteMetricsHistory}`; `tasks.SetRows(tasks.SlotMetrics, …)` (`routes/tasks._metrics_row`), `tasks.SetRunner("metrics_history_flush", …)` (via `tasks.StartBackground`), `tasks.SetIntervalSetter("metrics_history_flush", metrics_flush.set_interval)`; `lifecycle.Loop` (`metrics_flush.start_loop`), `metrics_flush.load_history` at start, `lifecycle.OnShutdown` (`flush_on_shutdown(record_idle=False)`); Python's signal handlers become the Go shutdown path (document). Task status store via `notifications.TaskStatus`.

## A05 — traffic history and traffic store

**Status: `missing`** · prereqs: A04 (shared `metrics/traffic_history.sqlite3` flush), C37 (`registrable_domain` only, see note ¹) · Go: internal/agents/metrics, api/agents

Files:
- `backend/agents/traffic_history.py` (504)
- `backend/agents/traffic_store.py` (265)
- `backend/routes/agents.py` (198) — lines 728–750, 781–884, 2100–2170

Tests: `test_traffic_history.py` (13), `test_domain_status_cache.py` (17)

Routes (8): `POST /api/agents/<agent_id>/traffic-report`; `POST|GET|PUT|DELETE /api/agents/<agent_id>/traffic-history`; `PUT /api/agents/traffic-history/retention`; `GET /api/agents/<agent_id>/traffic/stats`; `GET /api/agents/<agent_id>/traffic/trend`

Evidence: no Go code.

Hooks to provide: `agents.RegisterHooks{DeleteTrafficHistory}`; `agents.OnAgentDeleted("traffic_store", …)` first in order (then C16 registers `domain_probe`, `region_check`). For other domains: `traffic_store` is read by C37 domain discovery (`get_traffic_store`) — C37 may import the A05 package directly or declare an interface (decision 3); agents must not import C37 except `registrable_domain`. Public endpoints (`traffic-report`, `POST traffic-history` are Agent-authenticated): check `internal/auth` gate list against `routes/auth.py`.

## A06 — OxiDNS monitor, dashboard state, metrics

**Status: `missing`** · prereqs: A01 · Go: internal/agents/monitor (new), api/agents

Files:
- `backend/agents/oxidns_monitor.py` (727)
- `backend/agents/oxidns_dashboard_state.py` (240)
- `backend/agents/oxidns_metrics.py` (338)
- `backend/routes/agents.py` (59) — lines 2186–2198, 2216–2221, 2286–2325

Tests: `test_oxidns_monitor.py` (24), `test_oxidns_metrics.py` (23), `test_oxidns_dashboard.py` (35)

Routes (5): `GET /api/agents/<agent_id>/oxidns/overview`; `POST /api/agents/<agent_id>/oxidns/dashboard/reset`; `GET /api/agents/<agent_id>/oxidns/clients`; `GET /api/agents/<agent_id>/oxidns/queries`; `GET /api/agents/<agent_id>/oxidns/queries/<record_id>`

Evidence: no Go code. `MonitorError` and `_agent_origin` are imported by A07, A16, A20, A22, A23 and by routers (`routes/routers.py`): export them from this package.

## A07 — Mihomo panel, OxiDNS cache/rule update/rankings/publication stats

**Status: `missing`** · prereqs: A06, C14 (`rule_fetch.request_rule` in mihomo_panel) · Go: internal/agents/monitor, api/agents

Files:
- `backend/agents/mihomo_panel.py` (375)
- `backend/agents/oxidns_cache.py` (134)
- `backend/agents/oxidns_rule_update.py` (166)
- `backend/agents/oxidns_ranking_history.py` (402)
- `backend/agents/oxidns_publication_stats.py` (158)
- `backend/routes/agents.py` (81) — lines 2199–2215, 2222–2250, 2251–2285

Tests: `test_mihomo_panel.py` (30), `test_oxidns_cache.py` (8), `test_oxidns_rule_update.py` (11), `test_oxidns_ranking_history.py` (21), `test_oxidns_publication_stats.py` (14), `test_oxidns_publication_core.py` (1), `test_oxidns_publication_deployment.py` (3)

Routes (9): `GET|PUT|PATCH|DELETE|POST /api/agents/<agent_id>/mihomo/<path:resource>`; `GET|POST /api/agents/<agent_id>/oxidns/rules/update`; `GET /api/agents/<agent_id>/oxidns/cache`; `POST /api/agents/<agent_id>/oxidns/cache/flush`

Evidence: no Go code.

Hooks to provide: `agents.RegisterHooks{PublicationComplete, PublicationCheckpoint}`; `lifecycle.Loop` for `oxidns_ranking_history.start_loop` (app.py starts it in the service process). Needs from A18: `oxidns_ha_publish.FIELD/active` (already in `agents/compat.go`) and `oxidns_ha_publish.rules` (pair rule update) — declare a hook here with an `ErrNotPorted` default for HA pairs; A18 registers it (breaks the rule_sync → rule_update → HA → rule_sync cycle).

## A08 — installer script rendering, install command store

**Status: `missing`** · HR · prereqs: A02 (`LATEST_AGENT_VERSION`), C02 (resource) · Go: internal/installer (new)

Files:
- `backend/agents/install_output.py` (45)
- `backend/agents/install_script.py` (448)
- `backend/agents/go_install_script.py` (525)
- `backend/agents/core_install_script.py` (40)
- `backend/agents/gateway_install_script.py` (176)
- `backend/agents/install_command_store.py` (99)

Tests: `test_install_output.py` (12), `test_agent_install_dependencies.py` (21), `test_agent_native_aio_install.py` (31), `test_agent_transaction_install.py` (13), `test_core_install_script.py` (49), `test_gateway_core_install.py` (34), `test_installer_forwarding_context.py` (13), `test_standalone_forwarding_install.py` (21), `test_aio_network_prerequisites.py` (5), `test_docker_agent_cli_output.py` (1), `test_oxidns_docker_install.py` (11), `test_shell_ha_prepare.py` (11), `test_agent_shell_output.py` (7), `test_agent_offline_update.py` (3), `test_gateway_agent_uninstall.py` (3)

Routes: none (served by A10).

Evidence: no Go code. Several tests execute the generated shell scripts and `backend/agents/scripts/*.sh` in a sandbox (`subprocess`); `test_agent_shell_output.py`, `test_agent_offline_update.py` test only the shell scripts (no Python under test) — port them as Go tests that run the same scripts against the Go-rendered output, or classify as script tests that stay with the scripts (decide with the coordinator). Byte-identical script output is the acceptance criterion: generate goldens from Python for every parameter combination used in the tests. Deferred case from C02: `test_safe_url_queries.py::test_generated_go_installer_does_not_print_binary_download_url`.

## A09 — Docker HA guide, preflight, enrollment

**Status: `missing`** · HR · prereqs: A08, A01, C05 (`resolve_profile_id`, ported) · Go: internal/installer, api/agents

Files:
- `backend/agents/docker_ha_guide.py` (475)
- `backend/agents/docker_ha_preflight.py` (376)
- `backend/agents/docker_ha_enrollment.py` (165)
- `backend/routes/docker_ha_installation.py` (67)

Tests: `test_docker_ha_guide.py` (19), `test_docker_ha_preflight.py` (32), `test_docker_ha_enrollment.py` (7)

Routes (2): `POST /api/agents/docker-oxidns-ha-enroll/<session_id>` (Bearer, public in auth gate — check); `POST /api/agents/docker-oxidns-ha-guide`

Evidence: no Go code. `docker_ha_guide` lazily imports the A10 compose modules: same Go package, port A09 and A10 back to back (A09 may stub the three generators until A10).

## A10 — Docker HA compose variants, installer routes

**Status: `missing`** · HR · prereqs: A08, A09 · Go: internal/installer, api/agents

Files:
- `backend/agents/docker_ha_compact.py` (182)
- `backend/agents/docker_ha_compose.py` (346)
- `backend/agents/docker_ha_compose_direct.py` (96)
- `backend/routes/agents.py` (394) — lines 246–537, 921–952, 1270–1339

Tests: `test_docker_ha_compact.py` (12), `test_docker_ha_compose.py` (13), `test_docker_ha_compose_direct.py` (13), `test_agent_install_commands.py` (18), `test_core_install_api.py` (4), `test_aio_agent_core_install.py` (13), `test_single_agent_core_install.py` (14), `test_agent_native_aio_api.py` (4), `test_gateway_network_probe.py` (9)

Routes (9): `POST /api/agents/core-install-command`; `GET /api/agents/core-install-script`; `GET /api/agents/docker-agent-compose`; `GET /api/agents/docker-agent-run`; `GET /api/agents/download/<filename>`; `GET /api/agents/gateway-network-probe.sh`; `GET /api/agents/i/<token>`; `POST /api/agents/install-command`; `GET /api/agents/install-script`

Evidence: no Go code. Several of these are public (install script, `i/<token>`, download, probe): check the `internal/auth` gate list.

## A11 — deployment bundle, locations, Mihomo resources, generated preview

**Status: `missing`** · HR · prereqs: A02, A19 (`linux_gateway` routing mark, gateway bundle), C05 (`oxidns_settings.safe_relative_path`, `OxidnsSettingsError`), C12 (`converters.common.ruleset_format`), C14 (`rule_fetch`, `ruleset_content`, `remote_rule_sources`), C15 (`scheduled_refresh.read_rule_source`), C21 (`converters.mosdns` rule parts), C22 (`oxidns_native`, `converters.oxidns.unavailable_rulesets`), C25 (`dns_watchdog.ensure_local_dns_watchdog`), C11 (`subs.ReportedCoreVersion`, ready) · Go: internal/deploy (new)

Files:
- `backend/agents/deployment_bundle.py` (834)
- `backend/agents/deployment_locations.py` (106)
- `backend/agents/mihomo_resources.py` (165)
- `backend/agents/generated_preview.py` (159)

Tests: `test_agent_content_manifest.py` (8), `test_agent_content_preparation.py` (9), `test_agent_content_baseline.py` (8), `test_agent_deployment_downloads.py` (18), `test_classical_deployment.py` (10), `test_deployment_error_locations.py` (10), `test_mihomo_resources.py` (8), `test_generated_preview.py` (9), `test_agent_publish_caches.py` (6); deferred bundle cases of `test_agent_deployments.py` (listed in A01) and of the config-owned deployment-content tests (see "Tests owned elsewhere").

Routes: none.

Evidence: no Go code. Provides for config: `deployment_locations` (`preparation_context`, `source_error`, `DeploymentPreparationError`) used by C17 `provider_delivery`, and `_pack` used by C34 `routes/generate.py` — register through interfaces declared in C17/C34 (decision 3). `generated_preview` uses `rule_sync_store._write/bundle_files` (A14): put `rule_sync_store` before it in the same package or port A11 and A14's store file together.

## A12 — push diff

**Status: `missing`** · prereqs: A11 · Go: internal/deploy

Files:
- `backend/agents/push_diff.py` (1465)

Tests: `test_agent_push_diff.py` (39), `test_deployment_bundle_lazy_archive.py` (9)

Routes: none (A13).

Evidence: no Go code. Uses `yamlx.Marked`/`OnMarked`; the CSafeDumper size measurement is a known accepted difference (PORTING.md) — document where it changes a visible size/threshold.

## A13 — content sync, Agent list/detail, push-diff routes

**Status: `missing`** · prereqs: A12, A15 (`upgrades.artifacts` for `upgrade_available`) · Go: internal/deploy, api/agents

Files:
- `backend/agents/content_sync.py` (460)
- `backend/routes/agents.py` (124) — lines 216–245, 1633–1726 (plus the GET branches of `handle_agents`/`handle_agent_item`, whose lines are counted in A03)

Tests: `test_agent_content_sync.py` (42), `test_agent_content_api.py` (10), `test_agent_push_diff_api.py` (7), `test_agent_push_diff_paging.py` (9), `test_agent_first_push_sync.py` (6); deferred from A01/A03 (list/detail cases)

Routes (4): `GET /api/agents`; `GET /api/agents/<agent_id>`; `GET /api/agents/<agent_id>/push-diff`; `GET /api/agents/<agent_id>/push-diff/entries`

Evidence: no Go code. Port `_public_agent` (A03 lines) here. Extend the existing `createAgent`/`handleAgentItem` handlers with the GET branch (router panics on a second handler for the same endpoint). Provides for config: `content_sync.invalidate_content_sync` used by C13 `subscription_cache` (interface declared in C13, registered here).

## A14 — publishing: transactional content, push-config, deployments, receipts

**Status: `missing`** · HR · prereqs: A11, A13, A01, C13 (`sub_store_client.get_subscription_proxies_yaml`), C17 (`provider_delivery.prepare/commit_provider_bundle`, `commit_cache_updates`), C18/C19/C20/C21/C22 (generators via `config_generator`), C12 (`node_conversions`), C23 (`oxidns_validation.require_valid_document`), C26 (`oxidns_device_policy.revision`), A19 (`gateway_settings`), routers (`router_split` in `_prepare_transactional_content`) · Go: internal/deploy, api/agents

Files:
- `backend/agents/manager.py` (145) — lines 1013–1157: `_remember_deployment_content`, `publish_deployment`, `_publish_deployment`
- `backend/agents/oxidns_validation_receipt.py` (76)
- `backend/agents/rule_sync_store.py` (147)
- `backend/agents/config_generator.py` (47)
- `backend/routes/agents.py` (587) — lines 885–920, 1340–1632, 1749–2006

Tests: `test_generated_config_error_locations.py` (10), `test_oxidns_deployment_receipt.py` (11), `test_oxidns_rule_source_edit.py` (4), `test_service_domain_publish.py` (2); deferred from A01 (upload/route cases), A02 (deployment messages routes), config `test_save_publish_guard.py`, `test_profile_generation.py`, `test_agent_log_redaction.py`

Routes (9): `GET /api/agents/<agent_id>/config`; `POST /api/agents/<agent_id>/generate-config`; `GET /api/agents/<agent_id>/generated-config/<preview_id>/download`; `GET /api/agents/<agent_id>/generated-config/<preview_id>/file`; `POST /api/agents/<agent_id>/push-config`; `GET /api/agents/<agent_id>/deployments/<deployment_id>`; `POST …/activate`; `POST …/rollback`; `POST …/recover`

Evidence: no Go code (registry side of deployments is in A01).

Hooks to provide: `agents.RegisterHooks{PublishDeployment (the non-pair _publish_deployment; A18 wraps it with oxidns_ha_publish.publish), ReceiptMatches}`. **`_prepare_transactional_content` and `_prepare_content_bundle` must live in `internal/deploy`, not in the api package**: A17 `dns_follow`, A18 `oxidns_ha_publish`, A22 `gateway_installation` and A23 `ipv6_readiness` call them. For config C34: artifacts (`download_final_agent_config`, `read_final_agent_file`) through C34's interface. `config_export.stored_secret_finder` → `internal/redact.StoredSecretFinder` (exists).

## A15 — upgrades, Agent operations, address follow

**Status: `missing`** · HR (upgrade/uninstall/restart of the Agent) · prereqs: A01, A02 · Go: internal/agents/upgrades (new), api/agents

Files:
- `backend/agents/upgrades.py` (310)
- `backend/agents/address_follow.py` (221)
- `backend/routes/agents.py` (214) — lines 1034–1049, 1050–1232, 2171–2185

Tests: `test_agent_upgrades.py` (12), `test_agent_upgrade_auth.py` (11), `test_agent_artifacts.py` (17), `test_agent_address_follow.py` (23); deferred from A02 (`test_agent_upgrade_messages.py` 7 cases)

Routes (15): `GET|PUT /api/agents/<agent_id>/address`; `POST …/restart`; `GET …/status`; `GET …/logs`; `GET …/logs/sources`; `POST …/logs/clear`; `POST …/logs/validate`; `GET|POST …/config/logging`; `POST …/uninstall`; `POST …/update`; `GET …/upgrade`; `POST …/upgrade/report`; `GET /api/agents/latest-version`

Evidence: no Go code (manager methods for restart/status/logs/uninstall/update/logging exist in A01).

Hooks to provide: `agents.RegisterHooks{AddressFollow, AddressConfirmed}` (address_follow; HA members only notify). `upgrade/report` is Agent-authenticated (check auth gate). `test_agent_upgrade_auth.py` also builds the `authenticated_app` fixture used by A18/A20 tests — port its helper first.

## A16 — Agent rule sync, projection, snapshot

**Status: `missing`** · HR · prereqs: A07 (`mihomo_panel.call`, `oxidns_rule_update`), A06 (`MonitorError`), A14 (`rule_sync_store`, transactional content), A11, C18/C22 (generators, `get_oxidns_ruleset_downloads`), C25 (`oxidns_common_routing._analyze/_references/apply_routing`, `dns_watchdog`), C27 (`rule_dns.profile_agents` and projection helpers), C05 (`OxidnsSettingsError`), C14 (`is_internal_rule_url`) · Go: internal/agents/rulesync (new), api/agents

Files:
- `backend/agents/rule_sync.py` (643)
- `backend/agents/rule_sync_projection.py` (608)
- `backend/agents/rule_sync_snapshot.py` (98)
- `backend/routes/agents.py` (22) — lines 1727–1748

Tests: `test_agent_rule_sync.py` (51), `test_rule_sync_projection.py` (35), `test_rule_sync_snapshot.py` (23); deferred from config `test_udp_fallback.py`, `test_oxidns_domain_only_native.py`, `test_operation_logs.py` (rule sync logs)

Routes (2): `GET|PUT /api/agents/<agent_id>/rule-sync`

Evidence: no Go code.

Hooks to provide: `agents.SetRuleSyncRow` (`routes/tasks._agent_rows` rule-sync row), `lifecycle.Loop` for `rule_sync.start_loop` (app.py `start_agent_rule_sync`; heartbeats also start it lazily — keep that behaviour), C27's interface for `rule_sync.settings_for` (decision 3). Note the idle-CPU history (5 s loop, IdleRounds): keep the idle-round gating exactly.

## A17 — DNS follow

**Status: `missing`** · HR · prereqs: A14 (`_prepare_transactional_content`), A11, A02 (IdleRounds, events), C26 (`oxidns_editor.prepare_editor`), C22 (`load_native_yaml`, `generate_oxidns_config`) · Go: internal/agents/dnsfollow (new), api/agents

Files:
- `backend/agents/dns_follow.py` (730)
- `backend/routes/agents.py` (30) — lines 162–191

Tests: `test_dns_follow.py` (70)

Routes (4): `GET|PUT|DELETE /api/agents/<agent_id>/dns-follow`; `POST /api/agents/<agent_id>/dns-follow/retry`

Evidence: no Go code.

Hooks to provide: `agents.SetDNSFollowRow` (dns_follow row; with A16's row this completes the `agent` task slot), `tasks.SetRunner("dns_follow", dns_follow.retry)`, `agents.OnManagerInit` (`dns_follow.start_loop`, registered first: Python order dns_follow → HA publication → IPv6 verification). `settings_digest` is used by A14 `_remember_deployment_content` (export it; A14 needs it → port the digest helper in A14 or give A14 a hook).

## A18 — DNS HA

**Status: `missing`** · HR · prereqs: A16 (`prepare_candidate`, `settings_for`, `fetch_snapshot`), A17, A14, A11, C26 (`prepare_editor`), C22 (`load_native_yaml`), routers (`routers/store.routers_of`) · Go: internal/agents/dnsha (new), api/agents

Files:
- `backend/agents/oxidns_ha.py` (625)
- `backend/agents/oxidns_ha_handoff.py` (167)
- `backend/agents/oxidns_ha_publish.py` (676)
- `backend/routes/agents.py` (61) — lines 192–215, 1233–1269

Tests: `test_oxidns_ha.py` (37), `test_oxidns_ha_publish.py` (28), `test_oxidns_ha_docker.py` (9), `test_oxidns_ha_unchanged.py` (6), `test_oxidns_vip_handoff.py` (13), `test_oxidns_ha_identity.py` (8), `test_oxidns_ha_compact_identity.py` (7), `test_oxidns_ha_guards.py` (3)

Routes (4): `GET|PUT|DELETE /api/agents/<agent_id>/oxidns-ha`; `GET /api/agents/<agent_id>/oxidns-ha-environment`

Evidence: no Go code; pure pieces copied in `internal/agents/compat.go`.

Hooks to provide: `agents.RegisterHooks{HAServiceOperation, HADeployment, HAActivate, PublishDeployment (publish wrapper around A14's function)}`, `agents.OnManagerInit` (`oxidns_ha_publish.start_loop`), A07's HA rule-update hook; then delete `agents/compat.go` and switch the registry to the real functions (pair lock file included).

## A19 — Linux gateway core

**Status: `missing`** · HR · prereqs: C05 (`append_url_query` — missing in C05, port it there or here per coordinator), C08 (`RuleCacheReadPath`, ready), C06 (`auth.IsAuthEnabled`), C32 (`routes.rules.get_ruleset_content`, lazy), routers (`router_split.check_generation`) · Go: internal/gateway (new)

Files:
- `backend/common/linux_gateway.py` (518)

Tests: none of its own (cases in `test_linux_gateway.py` → A20, config `test_setup_flow.py`, `test_mihomo_base_settings.py`).

Routes: none.

Evidence: no Go code. Provides for config C18 (`apply_linux_gateway`, `gateway_settings`, `fake_ip_network`) and routers (`router_split` imports `LinuxGatewayError`, `TUN_FILTER_KEYS`): C18 declares an interface (decision 3) registered here; for routers agree an interface with the routers planner (cycle `router_split` ↔ `linux_gateway`).

## A20 — Agent gateway bridge, gateway draft, gateway routes

**Status: `missing`** · HR · prereqs: A19, A06 (`MonitorError`, `_agent_origin`), A02 (`compare_versions`), C18 (`generate_mihomo_config` in the draft), routers (`router_split` MESSAGES/state/markers, `routers/store.routers_of`) · Go: internal/gateway, api/agents, api/linuxgateway (new; endpoints `custom_config.*` and `profiles.*`)

Files:
- `backend/agents/linux_gateway.py` (668)
- `backend/routes/linux_gateway_agents.py` (74)
- `backend/common/linux_gateway_draft.py` (165)
- `backend/routes/linux_gateway.py` (203)

Tests: `test_linux_gateway.py` (27), `test_linux_gateway_draft.py` (27), `test_linux_gateway_agent_bridge.py` (35), `test_agent_gateway_forwarding.py` (9), `test_router_gateway_forwarding.py` (25), `test_gateway_publish_compatibility.py` (18)

Routes (18): `GET|POST /api/agents/<agent_id>/linux-gateway/<operation>`; `GET|POST /api/agents/gateway-forwarding`; `GET|POST /api/custom-config/linux-gateway`; `POST /api/custom-config/linux-gateway/preview`; `POST …/restore`; `POST …/restore/preview`; `GET /api/custom-config/linux-gateway/rules/<path:rule_id>`; `GET /api/custom-config/linux-gateway/source/<path:rule_id>`; `GET|POST /api/profiles/<profile_id>/custom-config/linux-gateway`; `POST …/preview`; `POST …/restore`; `POST …/restore/preview`; `GET /api/profiles/<profile_id>/linux-gateway/rules/<path:rule_id>`; `GET /api/profiles/<profile_id>/linux-gateway/source/<path:rule_id>`

Evidence: no Go code. The rules/source provider routes use `validate_token_or_jwt` (config pulls, public in the gate). Routers' `routes/routers.py` imports `agents.linux_gateway` and `linux_gateway_draft.RESTORE_KEY`.

## A21 — gateway onboarding

**Status: `missing`** · HR · prereqs: A19, A20, C05 (`normalize_oxidns_settings`), C18 (`generate_mihomo_config`, `_default_mihomo_template`), C22 (`_forward`, `generate_oxidns_config`, `native_document`, `load_native_yaml`), C23 (`validate_settings`), C24 (`apply/read_common_settings`), C25 (`_references`), C36 (`setup_flow.adopt_owned_write`, lazy in the route), routers (`router_split`) · Go: internal/gateway, api/gatewayonboarding (new; endpoint `profiles.gateway_onboarding`)

Files:
- `backend/common/gateway_onboarding.py` (660)
- `backend/routes/gateway_onboarding.py` (91)

Tests: `test_gateway_onboarding.py` (50); deferred from config `test_mihomo_base_settings.py`

Routes (3): `GET|POST /api/profiles/<profile_id>/gateway-onboarding`; `POST /api/profiles/<profile_id>/gateway-onboarding/preview`

Evidence: no Go code. Provides for config: `mihomo_dns_loop` (C18 `mihomo_base_settings`), `assert_gateway_onboarding_ready` (A22), setup flow helpers (C36) — via interfaces declared in C18/C36. The route's lazy call into C36 `setup_flow.adopt_owned_write` needs an interface here (C36 comes later).

## A22 — gateway installation

**Status: `missing`** · HR · prereqs: A08 (`gateway_install_script`, `install_command_store`, `install_output`), A14 (`_prepare_transactional_content`), A20, A21, A06 (`MonitorError`), C17 (`commit_provider_bundle`), C18 (`_default_mihomo_template`, `mihomo_listen_ports`), routers (`router_split.is_full_tun`) · Go: internal/gateway, api/agents

Files:
- `backend/agents/gateway_installation.py` (727)
- `backend/routes/gateway_installations.py` (82)

Tests: `test_gateway_installation.py` (54), `test_router_split_regression.py` (4)

Routes (7): `GET|POST /api/agents/gateway-installations`; `GET /api/agents/gateway-installations/<session_id>`; `POST …/enroll`; `POST …/network`; `POST …/publish`; `GET …/script`

Evidence: no Go code.

Hooks to provide: `agents.RegisterHooks{InstallationProfile, InstallationRegister}` (registration route currently answers `InstallationError` 501 for gateway installs). For C36: `setup_abandon`/`setup_flow` call `gateway_installation` (interface in C36).

## A23 — IPv6 readiness, client evidence, verification

**Status: `missing`** · HR · prereqs: A06, A13 (`compare_now`, `get_content_sync`), A14 (`_prepare_content_bundle`), A16 (`rule_sync_store.load`), A02 (IdleRounds), C16 (`health_check.validate_health`, `GROUP_DEFAULTS`), C18 (`_default_mihomo_template`), routers (`routers/ipv6_readiness`, `routers/base.RouterError`, `route_editor.router_operation_lock`, `routeros.RouterOSDriver`, `routers/rule_sync_store.job_lock`, `routers/store.routers_of`, `common/client_devices.list_devices`) · Go: internal/agents/ipv6 (new), api/agents

Files:
- `backend/agents/ipv6_readiness.py` (492)
- `backend/agents/ipv6_client_evidence.py` (176)
- `backend/agents/ipv6_verification.py` (613)
- `backend/routes/ipv6_readiness.py` (63)

Tests: `test_ipv6_readiness.py` (41), `test_ipv6_verification.py` (39), `test_ipv6_client_evidence.py` (7)

Routes (5): `GET /api/agents/<agent_id>/ipv6-readiness`; `POST /api/agents/<agent_id>/ipv6-readiness/<operation>`; `GET|PUT …/ipv6-readiness/verification-settings`; `GET …/ipv6-readiness/verify`

Evidence: no Go code.

Hooks to provide: `agents.OnManagerInit` (`ipv6_verification.start_loop`, last); `ipv6_verification.enroll` is called from A14 `_publish_deployment` (A14 declares a hook with a no-op default, registered here). Routers' `routes/routers.py` imports `agents.ipv6_readiness` (interface for the routers domain).

## A24 — client tools, OxiDNS device policies, domain-discovery toggles

**Status: `missing`** · prereqs: A05, C09 (`connection_rules`, ready), C26 (`oxidns_device_policy.read_policies/revision/normalize_client_range`), C31 (`device_notes`), C37 (`domain_discovery_service`, `provider_revisions`), C05 (`OxidnsSettingsError`) · Go: api/agents

Files:
- `backend/routes/client_tools.py` (105) — registers on the agents blueprint (`agents.*` endpoints)
- `backend/routes/agents.py` (93) — lines 751–780, 2326–2388

Tests: `test_client_tools.py` (15)

Routes (8): `GET|PUT /api/agents/<agent_id>/client-notes`; `GET /api/agents/<agent_id>/mihomo/rule-targets`; `POST /api/agents/<agent_id>/mihomo/rule-targets/apply`; `PUT /api/agents/<agent_id>/domain-discovery`; `POST /api/agents/<agent_id>/domain-discovery/clear`; `GET|PUT /api/agents/<agent_id>/oxidns/device-policies`

Evidence: no Go code (manager `SetDomainDiscovery` exists in A01). Note: `mihomo/rule-targets` must register before the `mihomo/<path:resource>` catch-all of A07 or rely on Werkzeug rule ordering — check `httpx.Router` ordering against Flask's.

## Cross-domain interfaces (decision 3: interfaces/hooks, no copies)

Agents needs from config (call config packages directly; they do not import agents): C04/C05 repo and profile context (ready), C07 tasks/notifications (ready), C08/C09 rules (ready), C11 subs (ready), and the generation modules listed per module (C12–C27). Missing-in-config items agents modules need early: `profile_context.append_url_query` (C05, A19), `utils/domain_discovery.registrable_domain` (C37, A05).

Config modules that call agents code — each declares an interface in its own package; the agents module registers the implementation:

| Config module | Calls | Provided by |
|---|---|---|
| C07 notifications | `operation_events.event_message`, `diagnostics.failure_text` (copies in `notifications/compat.go`), `get_all_agents` (`notifications.SetSources{Agents}`, done by `agents.Wire`) | A02 (decide leaf package vs. copy, see A02), A01 |
| C07 tasks | `_agent_rows` (slot `agent`), `_metrics_row` (slot `metrics_history`), runners `dns_follow`, `metrics_history_flush`, interval `metrics_history_flush` | A16 + A17 (`SetRuleSyncRow`, `SetDNSFollowRow`), A04 |
| C13 subscription cache | `content_sync.invalidate_content_sync` | A13 |
| C16 domain probe / region check | delete stores on Agent delete (`agents.OnAgentDeleted`), `agents.config_generator` in region check | C16 registers; A14 |
| C17 provider delivery | `deployment_locations.preparation_context/source_error`, `diagnostics.failure_text` | A11, A02 |
| C18 Mihomo generation | `linux_gateway.apply_linux_gateway/gateway_settings/fake_ip_network`, `gateway_onboarding.mihomo_dns_loop` | A19, A21 |
| C27 rule DNS | `agents.rule_sync.settings_for` | A16 |
| C34 generate/profiles | `deployment_bundle._pack`, `download_final_agent_config`, `read_final_agent_file`, `AgentDeploymentConflict`, manager rebind | A11, A14, A01 (ready) |
| C36 setup flow | `gateway_installation`, `linux_gateway`, `gateway_onboarding`, `oxidns_monitor`, agent manager | A22, A19, A21, A06, A01 |
| C37 domain discovery | `traffic_store`, `version.compare_versions`, agent manager | A05, A02 (ready), A01 (ready) |

Agents ↔ routers (routers has no PROGRESS file yet; names are Python files): agents needs `router_split` (A14, A19, A20, A21, A22), `routers/store.routers_of` (A18, A20, A23), IPv6/RouterOS pieces (A23). Routers needs `agents.linux_gateway` (A20), `linux_gateway.LinuxGatewayError/TUN_FILTER_KEYS` (A19), `linux_gateway_draft.RESTORE_KEY` (A20), `agents.ipv6_readiness` (A23), `oxidns_monitor.MonitorError` (A06), `operation_events.event_message` (A02), agent manager (A01). `router_split` ↔ `linux_gateway` is a cycle: one side declares the interface; agree with the routers planner.

Registry extension points (already in `internal/agents`, defaults documented in `hooks.go`): `AddMetrics`/`DeleteMetricsHistory` (A04), `DeleteTrafficHistory` (A05), `PublicationComplete`/`PublicationCheckpoint` (A07), `AddressFollow`/`AddressConfirmed` (A15), `ReceiptMatches` (A14), `HAServiceOperation`/`HADeployment`/`HAActivate` (A18), `PublishDeployment` (A14, wrapped by A18), `InstallationProfile`/`InstallationRegister` (A22); `OnAgentDeleted` (A05 then C16); `OnManagerInit` (A17, A18, A23 in that order); `SetRuleSyncRow` (A16), `SetDNSFollowRow` (A17). Loops (`lifecycle.Loop`): rule sync (A16), metrics flush (A04), OxiDNS ranking history (A07), DNS follow / HA publication / IPv6 verification via `OnManagerInit`. Every module that adds routes to `internal/api/agents` adds its own file there and one line to `Register`.

## Open item from the registry port (owned by C07)

`GET /api/notifications` must not register before every task-row slot exists. `notifications.Complete()` checks only `ListTasks` and `Agents`; `agents.Wire` works around it by installing the `Agents` source only if `tasks.Complete()` is already true **at wire time**. Because `wirePackages` currently runs `agents.Wire` before any task-row provider exists, the `Agents` source is never installed, and it stays uninstalled even after A04/A16/A17/C15/C37/routers provide their slots unless `agents.Wire` happens to be listed last. **Intended fix (C07):** add `tasks.Complete()` to the notifications route gate (`notifications.Complete() && tasks.Complete()`); then `agents.Wire` installs the `Agents` source unconditionally (remove the workaround and its doc comment in `singleton.go`). Until then GET `/api/notifications` cannot register at all.

## Next steps (coordinator; at most 2 agents in this domain at a time)

Porting order (one fresh porter per module; reviewer follows each finished module):
1. **Now, no generation needed:** review **A02** (ported). Porter: finish **A01** + **A03** test gaps listed as "runnable now" (one porter, both modules; small), then reviewers for A01 and A03 (A01 is HR: lab test of activate/rollback/recover comes with A14's publish lab test).
2. **Early, independent of config generation:** **A15** (upgrades + address follow; only registry deps) → **A04** metrics → **A05** traffic (needs `registrable_domain` from C37: ask for an early export) → **A06** OxiDNS monitor → **A07** panels (after C14 is ported; config porting order has C14 second) → **A08** installers → **A09** → **A10** (A09/A10 back to back, same package).
3. **A19** Linux gateway core after C05's `append_url_query` exists and an interface for `router_split.check_generation` is agreed with routers.
4. After C14, C15, C21, C22, C25 (config): **A11** → **A12** → **A13**.
5. After C13, C17, C18–C20, C23, C26: **A14** → **A17** → **A16** (needs C27) → **A18**.
6. After C18 and the routers `router_split`/store modules: **A20** → **A21** (needs C24) → **A22** → **A23** (needs routers IPv6/RouterOS modules).
7. **A24** last (C26, C31, C37).

Modules that can start before the config generation modules (C18 etc.) exist: A01, A02, A03, A04, A05 (with `registrable_domain`), A06, A07 (with C14), A08, A09, A10, A15, A19 (with `append_url_query`). Everything from A11 on needs config generation.

HR lab tests (after the maintainer agrees; PVE lab only): A01+A14 publish/activate/rollback/recover; A08–A10 installers (Alpine/OpenRC + Debian/systemd containers, see installer container lab notes); A15 upgrade/rollback; A16 rule sync; A17 DNS follow; A18 DNS HA pair (keepalived VIP); A19–A22 Linux gateway modes; A23 IPv6 readiness against a lab RouterOS.

Questions for the coordinator/maintainer:
- `notifications/compat.go` copies of `event_message`/`failure_text`: leaf package or keep copies with a cross-check test? (A02 review)
- Shell-only test files (`test_agent_shell_output.py`, `test_agent_offline_update.py`): port as Go-driven script tests or leave with the scripts? (A08)
- `registrable_domain` early export from C37 for A05.

## Coordinator decisions (2026-11-15)

1. `notifications/compat.go` copies of `event_message` / `failure_text`: move them to a small shared leaf
   package that both `internal/agents` and `internal/notifications` import (no copies, no cycle). Done as
   part of the A02 review.
2. Python tests that only exercise shell scripts: port them as Go tests that render the script with the
   Go code and run it with `sh` on the test server (the same way the Python tests do), in the installer
   modules (A08–A10).
3. `registrable_domain`: the porter of A05 adds it to the domain-discovery package (`internal/discovery`
   or the name C37 will use, built on `subs.ExtractDomain`), with its Python tests; C37 later extends that
   package instead of re-porting it. Record it in PROGRESS-config C37 as "ported early by A05".
4. Notifications gate: C07 adds `tasks.Complete()` to the `GET /api/notifications` route gate and
   `agents.Wire` installs the Agents source unconditionally — done by the A01/A03 test-gap porter now
   (small, touches `internal/api/notifications` and `internal/agents`), with a test that the route
   registers once all task slots are provided.
