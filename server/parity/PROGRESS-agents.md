# PROGRESS — domain `agents`

Domain `agents` (WORKFLOW.md): Agent registry/heartbeat, metrics/traffic, OxiDNS/Mihomo monitoring, installers, Docker HA, deployment bundle, push diff, content sync, publishing, upgrades, address follow, rule sync, DNS follow, DNS HA, Linux gateway, gateway onboarding/installation, IPv6 readiness (old packages #20–#31). **69 Python files, 25154 lines, 24 modules** (equal to the `agents` totals in `FILES.md`) — 18 missing, 0 ported, 6 verified (A01, A02, A03, A04, A05, A06). Only reviewers set `verified`.

Planning audit: 2026-11-15, worktree `configflow-go`, branch `go` at 496004d7. The Go code was read directly (`internal/agents`, `internal/api/agents`, `internal/notifications`, `internal/tasks`, `internal/server`); the hand-back in `progress/20-agents.md` was not relied on.

## Conventions

Same as `PROGRESS-config.md`: status `missing` / `partial` (exact list of what is missing) / `ported` (code and tests exist, not reviewed) / `verified` (reviewer only). A test file is listed under the module that owns its subject; cases that need a later module are **deferred** and listed in the section of the module that makes them runnable. Python test counts are `def test*` per file. Routes are rows of `server/parity/routes.tsv` (its "go package" column still carries the old package numbers; porters write the Go package when they set `done`).

`backend/routes/agents.py` (2388 lines) is split by line ranges (function groups, listed per module; ranges are contiguous and cover the whole file). `backend/agents/manager.py` (1787) is split into part A (A01) and part B (A14).

**HR** = high risk: a defect can break a user's network (publishing, rule sync, DNS follow, DNS HA, Linux gateway, installers, Agent upgrade/rollback). HR modules additionally need a **VM lab test after the maintainer agrees** (PVE lab, never production) before the domain is done.

## Module table

| Id | Module | Py lines | Status | HR | Prereqs | Routes | Go package(s) |
|---|---|---|---|---|---|---|---|
| A01 | Agent manager (manager.py part A) | 1642 | verified | HR | C04 | – | internal/agents |
| A02 | registry support: runtime state, diagnostics, events, versions, messages | 1386 | verified | | C02 | – | internal/agents |
| A03 | registry routes (register, heartbeat, create/edit/delete) | 432 | verified | | A01, A02, C06 | 5 (5 done) | internal/api/agents |
| A04 | metrics history and flush | 1163 | verified | | A01, C07 | 3 | internal/agents/metrics (new), api/agents |
| A05 | traffic history and traffic store | 967 | verified | | A04, C37¹ | 8 (8 done) | internal/agents/metrics, internal/agents/trafficstore (new), internal/discovery (new, C37's), api/agents |
| A06 | OxiDNS monitor, dashboard state, metrics | 1364 | verified | | A01 | 5 | internal/agents/monitor (new), api/agents |
| A07 | Mihomo panel, OxiDNS cache/rule update/rankings/publication stats | 1316 | ported | | A06, C14 | 9 | internal/agents/monitor, api/agents |
| A08 | installer script rendering, install command store | 1333 | partial | HR | A02, C02 | 8 (8 done) | internal/installer, internal/api/agents |
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

**Status: `verified`** (review 2026-11-15, commits 2a2c685c, b8f6129d, 622c22fd; HR: VM lab test still due before the domain is done) · HR · prereqs: C04 · Go: internal/agents (manager.go, deployments.go, remote.go, hooks.go, client.go, compat.go; singleton.go is shared with A02)

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

`agents.AgentDeletedStores` fixes the run order (traffic_store, domain_probe, region_check, then any other name) whatever the wiring order; a store error answers 500 with its text after the Agent is gone (as Python). `internal/server.TestDeleteAgentClearsEveryPerAgentStoreInAFullyWiredServer` checks all five on `NewApp()` and is **skipped with `TODO(C16)`** (A04 and A05 are wired): C16's porter removes the `t.Skip`. Each of them should also seed its store files for `agent-1` in `api/agents/difftest` (the differential compares the files under `DATA_DIR`, lock files excluded).

Decisions:
- Light reads: Go's typed jsonx getters cannot raise on `repo.AgentHistoryNotRead` like Python's sentinel (documented on the sentinel); the Go test checks the equivalents: `repo.AgentHistory` refuses it, it is never an object/list to type assertions, every encoder refuses it, copies keep it, saving it fails.
- `test_agent_profiles` snapshot case: Go transactions always read the committed document, so this passes by construction; kept as a regression test.

Pitfalls:
- Hook defaults (unchanged, intended): `ReceiptMatches` nil → activation of every OxiDNS candidate is refused until A14; `AddMetrics` nil → heartbeat metrics dropped until A04; `PublishDeployment` → `ErrNotPorted`. A04/A05 must keep Python's **per-manager** history state: key it by `m.Repository().DataDir()`.
- `remote.go` answers 200-non-object JSON in `_agent_json_request` with a `*RemoteError` carrying the AttributeError text; Python raised AttributeError (not RuntimeError). Callers that distinguish the two (A06/A07/C16 routes) must check.
- Tests in `internal/agents` share process-wide hooks/runtime: every test starts with `newEnv` (resets both).

Review (adversarial, 2026-11-15) — every function of manager.py part A walked against manager.go/deployments.go/remote.go/client.go; reference corpus from CPython 3.11 / requests 2.31 on the test server (`testdata/gen_a01_reference.py` → `testdata/a01_reference.json`: 44 host/port forms against loopback Agents with DNS disabled, json= bodies, 42 record_deployment status cases, 11 DST freshness cases), asserted by `a01_reference_test.go`. Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | med | record_deployment: an Agent answering a list/dict `status` (activate/rollback/poll) was stored; Python raises TypeError at `status in _DEPLOYMENT_RESULTS` and stores nothing. Status tuples were `map[any]bool` lookups that panic on list/dict values where Python's `in` is just False (`receipt`, terminal, HA terminal) | 2a2c685c (`pyStatuses.has`, `unhashable`) | `TestReferenceRecordDeploymentStatuses` |
| 2 | low | heartbeat_fresh: naive heartbeats were compared as instants; Python subtracts naive wall clocks (DST change not seen), truncates now to µs, and an aware heartbeat against a given naive now is a TypeError (stale) | 2a2c685c | `TestReferenceHeartbeatFreshness`, `TestHeartbeatFreshNowNilUsesWallClockAndMicroseconds` |
| 3 | low | `_preparation_running`: lock-file open/flock errors were swallowed as "not running"; Python raises | 2a2c685c (seam returns `(bool, error)`) | `TestPreparationLockErrorsPropagate` |
| 4 | low | Agent deleted mid-operation: rollback/recover sent to `http://None:None` and recorded "unknown"; `_get_deployment` re-read kept the old state. Python raises TypeError/AttributeError (500) | 2a2c685c | `TestAgentDeletedDuringRollbackIsAnErrorNotAnUnknownOutcome` |
| 5 | low | record(): `default_profile_id()` read unconditionally; Python only when status is succeeded and the task is latest | 2a2c685c | existing deployment tests |
| 6 | low | json= bodies with NaN/Infinity were sent; requests 2.31 raises InvalidJSONError (a RequestException, "Out of range float values are not JSON compliant") before sending | b8f6129d (`jsonBody`, simpleCall → "Connection error: …") | `TestReferenceJSONBodiesRefuseNaN` |
| 7 | low | URL preparation differed from requests: `%XX` of unreserved chars not decoded (requote_uri), `user:pass@host` did not replace the Bearer header with Basic auth, glibc numeric hosts (`127.1`, `0x7f.0.0.1`, `2130706433`, octal parts) went to DNS, full-width/compatibility hosts (`１２７.0.0.1`) were mapped by Go's UTS46 where urllib3's strict IDNA refuses them | b8f6129d | `TestReferenceAgentURLs`, `TestInetAtonExactLikeGlibc`, `TestUnquoteUnreservedLikeRequests` |

Charset (limitation handed over from A02): decided **unobservable**, documented on `Response.Text`: the Go Agent answers `application/json` without charset (`routes/utils.go`) or `text/plain; charset=utf-8` (`http.Error`), relayed Mihomo/OxiDNS answers are UTF-8; only a foreign server at an Agent address could declare gbk etc., and then only a failure diagnostic text differs. No `golang.org/x/text`.

Checks performed (no finding): register_agent (name/host match, token check, installation conflict, profile/upgrade/deployment, DNS follow and HA locks incl. port/service comparisons with Python `==` on ints/floats/bools, field copy lists, deep copies, ID/token format), get_*/update_agent/rebind/delete (order of checks, messages byte for byte, hooks), update_heartbeat (mark/record race, version-change writes, metrics hook), begin_deployment (regex, HA conflict, DNS follow states, 30-task bound, flock, finish on error, log line), record/_unchanged_deployment (every branch, failure_text/dns_report sanitising, events, latest copy, success/rollback baseline, finished log), _deployment_request (busy retry 21×/2 s, JSON fallbacks, http_status), get/activate/rollback/recover/_restore/_cancel (all status branches and messages), legacy push, restart, status, logs (quote), uninstall, update, logging config, domain discovery/traffic toggles, _agent_json_request and DNS HA calls, ports/hosts matrix.

Accepted limitations: transport error texts are Go's (requests' include object addresses); requests' default headers (User-Agent, Accept-Encoding gzip/deflate), 30-redirect limit and `.netrc` are not reproduced (Agents never redirect or read them); other strict-IDNA 2008 rejections are not checked (no x/text; such names fail resolution anyway); malformed stored Agent records (e.g. a truthy non-dict `latest_upgrade`, non-dict `deployments`) make Python raise where Go treats them as missing — neither side writes such shapes.

Commands: reference `rt --back server/internal/agents/testdata/a01_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_a01_reference.py > server/internal/agents/testdata/a01_reference.json'`.

## A02 — registry support

**Status: `verified`** (review 2026-11-15, commits 4c4d5079–d550ff86, table below) · prereqs: C02 · Go: internal/agents (runtime_state.go, diagnostics.go, operation_events.go, idle_rounds.go, presence_log.go, version.go, version_changelog.go, messages.go, messages_data.go, singleton.go, pytime.go, pydecode.go) + internal/agents/diag (leaf: failure_text, event_message)

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

Evidence (audit): all defs matched (`get_agent_manager`→`Get`, `init_agent_manager`→`Init` + `OnManagerInit`, `for_data_dir`→`RuntimeFor`, `import_legacy_database`→`ImportLegacyDatabase`, `observe`→`ObservePresence`, `_hide_block_secrets`/`_safe_diagnostic_url`/`failure_text`/`dns_report_fields`/`response_failure`, `get_latest_version`/`compare_versions`/`has_update`, message tables generated from Python by `testdata/gen_*.py`). `LatestAgentVersion` = `1.8.61-go`, 80 changelog entries as in Python. Tests: runtime 28/28, operation_events 5/5, idle_rounds 5/5 (the weak-key GC part now runs, see review), version_sync 7/7 (five `has_update` cases merged into `TestExistingAgentsCanUpdate`). `agents/__init__.py` only re-exports.

Deferred: `test_deployment_messages.py` 3 route cases (`test_display_copy_keeps_protocol_fields_and_both_failure_causes`, `test_list_history_and_poll_explain_failures_without_changing_saved_record`, `test_publish_preflight_errors_are_chinese_without_starting_a_task`) → A13/A14; `test_agent_upgrade_messages.py` 7 manager/route cases → A15.

Review (adversarial, 2026-11-15) — every Python function and branch walked against Go; reference corpus from CPython 3.11.17 / requests 2.31 on the test server (`testdata/gen_a02_reference.py` → `testdata/a02_reference.json`, asserted by `a02_reference_test.go`). Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | med | `failure_text`: `\b` before secret keys / Bearer/Basic was RE2's ASCII `\b`, Python's is Unicode, so Go redacted `密码password=…`, `ätoken: …`, `éBearer …` (known-difference comment removed) | 4c4d5079 (manual word-boundary check, `diag.replaceAtWordStart`) | `TestReferenceFailureText` |
| 2 | low | `(?i)` keywords: Python matches `i` with U+0130/U+0131 (`APİ_KEY=`, `apı_key=`, `BASİC`), RE2 not | 4c4d5079 (`[iİı]`) | `TestReferenceFailureText` |
| 3 | low | decision 1: `notifications/compat.go` copies of `event_message`/`failure_text`/`MESSAGE_LIMIT` | 4c4d5079: leaf package `internal/agents/diag`, used by agents and notifications (no cycle) | notifications tests + `TestReferenceEventMessage` |
| 4 | med | naive ISO times (`_timestamp`, legacy rows) resolved with `time.Date`, not CPython `local_to_seconds(fold=0)`; aware `.timestamp()` double-rounded; `time.time()` not `_PyTime_AsSecondsDouble`, so `last_heartbeat` µs could differ | 69f408a4 (`localToSeconds`, `pyTimestampOf`, `pyTimeFromNanos`) | `TestReferenceTimestampsAndLocalTimes` (America/New_York gap/fold, Asia/Shanghai LMT 1900, UTC) |
| 5 | med | legacy `runtime.sqlite3` import: driver error texts (`SQL logic error: … (1)`) instead of `str(sqlite3.Error)`; invalid-UTF-8 TEXT did not fail the read (`Could not decode to UTF-8 column …`); BLOB `fields` not decoded like `json.loads(bytes)` (BOM/UTF-16/32); `os.Remove` removed empty directories (Python `unlink` warns); OSError text not `[Errno N] …: 'path'`; `Lstat` treated a dangling symlink as present (Python `exists()` follows); state/legacy keys not `Path.resolve()` (non-strict realpath); agents keyed with `str(id)` | 916bcd2d | `TestReferenceLegacyRows`, `TestReferenceLegacyFileRemovalLogsPythonOSErrors`, `TestReferencePathResolveLikePathlib`, `TestADanglingRuntimeSqliteSymlinkIsLeftAlone` |
| 6 | low | `compare_versions` `int()`: whitespace rule (ASCII kept, only non-ASCII `isspace` rewritten: `'\x851'`→1, `'\x1c1'` fails), digit value wrong for back-to-back Nd runs (`𝟘` U+1D7D8 read as 9), no 4300-digit limit | 437991f4 | `TestReferenceCompareVersions` (53 pairs) |
| 7 | low | upgrade messages: `strip()` missed U+001C–U+001F; `lower()` of U+0130 (`İ/o timeout` matched `i/o timeout` in Go only) | cac421bc | `TestReferenceMessages` |
| 8 | med | `response_failure` / `Response.text/.json()` (client.go): BOM stripped for `application/json` (requests fails), invalid UTF-8 rejected (requests replaces), `text/plain` without charset not ISO-8859-1, replacement not per maximal subpart, repeated Content-Type not joined | 3df17cfe (`pydecode.go`) | `TestReferenceResponses` (25 responses: text, json, response_failure) |
| 9 | low | no test tying the generated tables to Python | 1d29659a | `TestVersionTableMatchesPython`, `TestMessageTablesMatchPython` (read `backend/agents/*.py`) |
| 10 | low | weak-key idle-rounds case always skipped (the fixture repository itself was collected) | d550ff86 | `TestRepositoriesAndLoopsDoNotShareIdleState` runs (5×, -race) |

Checks performed (no finding): `RuntimeState` overlay/mark/record/seed/delete (locks, deep copies, deletion counter, missing-field inheritance; non-dict `system_metrics` is rejected by the route first), `import_legacy_database` order (checked-set before I/O, list after read, nothing applied unless removed, version persist failure), `presence_log` (order, `log_text` limits 120/32), `idle_rounds` (head compare, `0 <= elapsed < 600`), singleton `Get`/`Init`, `event_time` (51 ISO edge texts incl. 3.11 any-separator, week dates, `+24:00`, year 1/9999 overflow), `clean/append/merge_events` (dedupe, 80 limit, statuses, sort stability), `dns_report_fields` (24 payloads), `deployment_message`/`public_*` (shallow copies, event messages), failure_text corpus (~250 cases: ANSI/control chars, PEM without END, YAML blocks with `\r\n`/U+2028/tabs/list items, URL schemes, secret variants, truncation at CJK/emoji boundaries for limits 27–16384). Every Chinese message compared via corpus.

Accepted / not reproduced (no finding for A02; for the coordinator):
- `Response.Text()` decodes only the UTF-8, Latin-1, ASCII, UTF-16, UTF-32 codec families; another `charset=` (gbk, cp1252, …) decodes as UTF-8 where Python uses that codec, and requests' `charset_normalizer` guess (no charset at all) is approximated by BOM/NUL pattern else UTF-8. Needs `golang.org/x/text` (PORTING rule 6); no ConfigFlow Agent sends such charsets. `client.go` is A01's file: A01 review to confirm.
- Lone UTF-16 surrogates become U+FFFD (jsonx-wide limitation); Python's set order of equally long secrets is arbitrary (Go: given order); `Path.resolve()` raises on a symlink loop in 3.11 (Go returns the path; such a data dir cannot be opened by the repository); an Agent record without `id` raises KeyError in Python (Go skips it in the legacy import).

Commands: reference corpus `rt --back server/internal/agents/testdata/a02_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_a02_reference.py > server/internal/agents/testdata/a02_reference.json'`; tests `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/notifications/... && go test -count=1 -race ./internal/agents/... ./internal/notifications/... ./internal/api/agents/...'`.

## A03 — registry routes

**Status: `verified`** (review 2026-11-15, commit 719cfba5) · prereqs: A01, A02, C06 · Go: internal/api/agents (agents.go, agents_test.go, difftest/, testdata/)

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

Review (adversarial, 2026-11-15) — routes walked branch by branch against agents.py lines 1–161, 538–727, 953–1033; 33 cases added to the Flask differential (`reviewRequests`), now **132 requests, 0 differences**. Findings, all fixed in 719cfba5:

| # | Sev | Finding | Regression test |
|---|---|---|---|
| 1 | med | heartbeat metric int beyond the float range: Python's `math.isfinite(int)` raises OverflowError → 500 "int too large to convert to float"; Go answered 400 | `TestHeartbeatMetricIntBeyondFloatRangeIsOverflowError`, difftest "heartbeat metrics huge*/halfway/max float" |
| 2 | low | metric checks ran in random map order and mixed key/value checks; Python stops at the first failing section in `_METRIC_FIELDS` order and checks a section's keys before its values (decides 400 vs 500) | same test, difftest "earlier/later section bad", "bad key/value after huge" |
| 3 | low | heartbeat body with an int literal over 4300 digits: `json.loads` raises a plain ValueError (not caught) → 500 with its message; Go answered 400 | `TestHeartbeatIntBeyondDigitLimitIsValueError`, difftest "digit limit*" |
| 4 | low | `X-Forwarded-For` is latin-1 decoded under WSGI (stored host, `strip()` of \xa0/\x85); Go used the raw bytes | `TestRegistrationForwardedForIsLatin1`, difftest "register latin-1 forwarded *" |

Shared change: `httpx.Ctx.JSONForceError()` (json.loads with its error) added for `_read_json_body`.

Checks performed (no finding): Docker bridge `int()` forms (Arabic/full-width digits, spaces, `_`, `+`, leading zeros, hex) via difftest; registration key, installation fields, null/list/str bodies, PermissionError/conflict notifications, log fields; `_authenticated_agent`; `_read_json_body` order (Content-Length, max+1 read, is_json, BOM/UTF-16); reply flags; POST/PUT/DELETE bodies and error texts; delete store hooks.

Accepted limitations: nesting depth — CPython's RecursionError depends on the stack depth (Python 500 for ~1000+ levels, Go parses up to `jsonx.MaxDepth` and answers 400/the type error); lone surrogate escapes (jsonx U+FFFD: Python's `.encode('utf-8')` in the metrics size check would raise); the 500 log line carries the error text instead of a traceback. All three are jsonx/httpx-wide.

## A04 — metrics history and flush

**Status: `verified`** (review 2026-11-15, commits 724a72ff, ce3e7845, c15b0fb8, d35aba60; table under "Review") · prereqs: A01, C07 · Go: internal/agents/metrics (history.go, stats.go, pyvalue.go, flush.go, wire.go), internal/api/agents/metrics.go

Files:
- `backend/agents/metrics_history.py` (810) — `LEGACY_DIRS` / `adopt_legacy_dirs` not ported (maintainer decision)
- `backend/agents/metrics_flush.py` (260)
- `backend/routes/agents.py` (93) — lines 2007–2099

Routes (3, all `done` in routes.tsv): `GET /api/agents/<agent_id>/metrics`, `/metrics/history`, `/metrics/summary`.

Commits: 26d4ea6c (package, routes, tests), c7b02570 (differential), 557df897 (routes.tsv, server wiring test, delete-store test TODO).

Registered from `metrics.Wire` (one line in `internal/server` `wirePackages`): `agents.RegisterHooks{AddMetrics, DeleteMetricsHistory}`; `agents.OnManagerInit(loadOnce)` = `load_history()` at service start (init_agent_manager runs before the server listens, where app.py loaded), once per data dir; `tasks.SetRows(SlotMetrics, Rows)` (`_metrics_row`); `tasks.SetRunner("metrics_history_flush", runNow)` (StartBackground → `Run(ctx,"manual",true)`); `tasks.SetIntervalSetter("metrics_history_flush", SetInterval)`; `lifecycle.Loop("metrics-history-flush", loop)` (30 s tick, woken by SetInterval) and `lifecycle.OnShutdown` → `FlushOnShutdown(ctx, true)` (lifecycle registrations guarded by sync.Once). Status store: `notifications.TaskStatus(ctx, repo data dir)`.

For A05: `metrics.SetTrafficFlush(fn)` is the traffic-history half of `run()` (retain_agents + flush(retention_days(system)) → rows); errors are wrapped as `TrafficHistoryFlushError` (`流量记录：…`); until set, `run()` result has no `traffic_rows`. `History.GetTrafficStats(agentID, period)` and `GetTrafficTrend(agentID, hours, intervalMinutes *big.Int)` are ported here (tested by test_api_shapes) for A05's traffic/stats and traffic/trend routes; route `hours`/`interval_minutes` should use the same `argBigInt` (4300-digit limit) as `api/agents/metrics.go`.

Tests (`test_agent_metrics_history.py`, 25): ported 17 in `history_test.go`/`flush_test.go` (memory window, append-only, compaction atomic, failed append rewrite, truncated line, migration, API shapes, heartbeat+delete, task row run/failure, interval choices, failed record no rerun, import drop, data dir, building manager untouched, startup load (without adoption), compact lines/fresh dicts (tracemalloc half N/A), shutdown save); Go equivalents for 3 (`stop_signal_during_a_shutdown_save` → shutdown save concurrent with a running save; `real_startup_path_saves_on_sigterm` → in-process Init + lifecycle.Start/Stop; `stop_signal_handler` second-signal half N/A); not ported 5 = the 4 LEGACY_DIRS tests + `test_real_startup_path_saves_on_ctrl_c…`/`test_debug_mode_stop…` (Python signal/reloader mechanics; counted in the N/A above). Extra Go tests: non-dict system_metrics AttributeError, describe_os_error texts, undeletable file, `{"metrics": 5}` ends the load, route tests in `api/agents/metrics_test.go` (401/404, hours parsing incl. Arabic digit, `1_0`, 4301 digits → default, overflow texts → 500), `server.TestNewAppWiresTheMetricsHistory`. No deferred cases were recorded for A04.

Reference values: `testdata/probe.py` (timedelta overflow texts, int() digit limit, `in`/index TypeErrors, sum/max/min/NaN) run on the test server, results encoded in the tests.

Differential (`api/agents/difftest`, extended): seeded `metrics/` (agent-1 JSONL with expired/recent/truncated lines, agent-busy legacy JSON to migrate, deleted Agent file, `.tmp` leftover), +27 requests (heartbeats with metrics incl. 2**64-1 values, the three routes: success, 401, 404, hours variants, three overflow 500s), then graceful stop of both (Python SIGTERM, Go lifecycle.Stop) and comparison of file names (Agent ids normalized), metrics files byte for byte (timestamps masked) and the task-status `metrics_history_flush` run. Result: **101 requests, 0 differences**, 2 metrics files identical.

Differences from Python (documented, accepted):
- State per data dir (`metrics.For(dataDir)`), not per manager object: a second AgentManager for the same repository keeps the memory (Python built a new empty one; only at start in production).
- Points a heartbeat adds before `Load` runs are kept after the loaded points (Python loaded before serving; Go loads in the init hook, also before listening, so this is only a guard).
- Shutdown: superseded by review finding 3 — Go now runs both saves of the signal path (`shutdownSave`). Second-Ctrl+C force-quit and "exit without waiting for busy threads" belong to main (#3).
- Directory listing order: Go sorted, Python os.listdir order (affects only the order of load warnings/migrations).
- Temp-file names: `os.CreateTemp(".<id>*.tmp")` (decimal suffix) vs mkstemp's 8 letters; both mode 0600, removed on failure.
- Temp-dir fallback uses `os.TempDir()` (Python gettempdir() candidates); a non-permission MkdirAll error is logged instead of failing the manager.
- `parse_time` offset seconds (e.g. `+05:30:15`) are not printed by `tasks.ISO`.
- Lone surrogates from hand-edited files (Python: UnicodeEncodeError at save) not reproduced.

Review (adversarial, 2026-11-15) — metrics_history.py, metrics_flush.py, routes/agents.py 2007–2099, app.py start/stop, routes/tasks `_metrics_row`/run/interval, task_status.record walked branch by branch against Go. Reference corpus from CPython 3.11.17 on the test server (`testdata/gen_a04_reference.py` → `testdata/a04_reference.json`, asserted by `a04_reference_test.go`): 16 saved JSON Lines files through `_read_file` (iso variants incl. week dates, compact, `,` fractions, 7-digit fractions, hour 24, full-width digits, aware/`Z`; Unicode-space and blank lines; universal newlines; BOM; invalid UTF-8; NaN/Infinity/-0.0/1e400/4300- and 4301-digit ints; duplicate keys and timestamps; truncated last line), 19 old JSON files through `_read_legacy` (non-iterable `metrics` TypeErrors, BOM, UTF-16, invalid UTF-8, syntax errors), 43 summary, 28 traffic-stats and 23 trend cases (bools, strings/None/list/dict values, `in`/index TypeErrors on non-dict sections, huge ints and their float conversions, 3.11 `sum` int→float switch and rounding, NaN/Infinity/-0.0 in max/min, hours windows/zero/negative, today/week on Sunday and Monday, counter resets, backwards and equal times, intervals 0/negative/10**30, far-future points). Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | traffic stats/trend durations used `time.Duration`, which saturates after 292 years: a saved point dated far ahead (e.g. 9999, 2400) gave other speeds than `timedelta.total_seconds()` | 724a72ff (`secondsBetween`: exact µs difference, correctly rounded division) | `TestReferenceTrafficStats` (year 9999/2400 end), `TestReferenceTrafficTrend` (year 9999) |
| 2 | low | reading a saved file decoded invalid UTF-8 with `strings.ToValidUTF8` (one U+FFFD per run of bad bytes); Python's `errors='replace'` writes one per maximal subpart, so returned points and compacted files differed (`a\xff\xfeb` → 2, `\xed\xa0\x80` → 3 replacements) | ce3e7845 (`agents.DecodeUTF8Replace`, exported from A02's `pydecode.go`) | `TestReferenceReadSavedLines` ("invalid utf-8 in strings"), difftest agent-odd file compared after stop |
| 3 | low | shutdown ran one save (record_idle=True); app.py's stop-signal path runs the handler's save (record_idle=True) and, after serve() returns, a second one (record_idle=False). A save that fails at shutdown (full disk, read-only dir) is therefore recorded twice in Python (`run_count`, `failure_count`, `consecutive_failures` +2), Go +1 | c15b0fb8 (`shutdownSave` runs both) | `TestShutdownFailureIsTriedAndRecordedTwiceLikeTheSignalPath`, `TestStartAndStopPathLoadsSavesAndRecords` (success still +1) |

Differential (`api/agents/difftest`, d35aba60): a fourth seeded Agent `agent-odd` with a saved file holding a BOM line, invalid UTF-8, a lone CR break, NaN, a 31-digit int, 1e308 speeds, a string usage 3 h back, a 9999 point, a repeated and an aware timestamp and unreadable lines (compacted at the final save); 10 route cases (history all/1 h, summary 1 h/2 h/6 h = TypeError 500, full-width/spaced/empty/repeated `hours`, latest). **142 requests, 0 differences**, 3 metrics files identical after stop.

Porter's listed differences, decided:
- Two shutdown saves → finding 3, fixed (Go now runs both; the only remaining difference is which of the two runs saves heartbeats that finished while waitress drained — timing-dependent, not reproducible).
- Heartbeats before `Load` kept, not replaced: **unobservable** — `repo.LoadConfig` → `agents.Init` → `OnManagerInit(loadOnce)` runs in `main` before `net.Listen`, so no heartbeat can arrive first (Python loaded before `run_http_server` too). Kept as a guard.
- State per data dir instead of per manager: unobservable — Python rebuilds the manager only when `get_repository()` changes, which happens once at start.
- Directory listing order (only the order of load warnings/migration writes), temp-file names (transient; both `.<id>…tmp`, mode 0600, removed by the next load), temp-dir fallback candidates, `+HH:MM:SS` offsets in hand-edited task-status, lone surrogates: accepted as listed.

Accepted differences added by the review:
- `metrics` directory cannot be created for a reason other than permission (path occupied by a file, read-only file system on first start): Python's `MetricsHistory.__init__` raises, so `AgentManager` cannot be built and every Agent route answers 500 until restart; Go logs and reports the error at the next save. Broken data directory only; the repository itself fails first on a read-only data dir.
- An Agent id starting with `/` (only via an imported backup; registration generates ids): Python's `os.path.join` would put the file outside DATA_DIR/metrics; Go keeps it under the directory. Ids containing `/` elsewhere make `os.CreateTemp` fail with its own text where mkstemp fails with ENOENT.
- Hand-edited `task-status.json` (non-dict entry, `last_run_at` within an interval of year 9999): Python raises in `_metrics_row`/`next_run_at`, Go treats the entry as empty / prints year 10000. Neither side writes such values.
- Deep JSON nesting in a saved line (RecursionError aborts Python's load) — jsonx-wide limitation as in A03.

Checks performed (no finding): `_prune`/`add_metrics`/`get_metrics` (deque cap, pending clamp, two `now()` reads, cutoff before prune order), `_cutoff` overflow texts and floor normalisation of negative hours, `_parse_lines` (count of non-blank lines, dedupe only of kept points, aware → TypeError skip), `load` (tmp removal, suffix order, `$` before newline, deleted Agents, legacy + new file, OSError vs ValueError vs uncaught TypeError partial state, migration flush under the lock), `delete_agent_history`/`retain_agents` key sets, `_append` (newline repair, fsync), `_replace` (empty → remove, temp cleanup), `_flush_agents` (compaction ratio, rewrite after failure, empty-points pop, legacy removal, counts), `flush` error text, `describe_os_error`, `metrics_flush.run` (failure order and texts, traffic slot, record_idle), `next_run_at` (first maximal, offsets, `_iso`), `run_due`, loop, `validate_interval`/`interval_seconds`/`set_interval` (`type(value) is int`: floats and bools refused), `_metrics_row`, run-now, the three routes (404/500 texts, non-dict `system_metrics`, `hours` via `int()` incl. full-width/space/sign/empty/repeated/4301 digits).

Commands: reference `rt --back server/internal/agents/metrics/testdata/a04_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/metrics/testdata/gen_a04_reference.py > server/internal/agents/metrics/testdata/a04_reference.json'`. The review ran the tests on a private export of HEAD (A05's uncommitted WIP in `internal/agents/metrics`, `trafficstore`, `api/agents` did not compile at the time): `go vet ./internal/agents/ ./internal/agents/metrics/ ./internal/api/agents/... && go test -count=1 -race ./internal/agents/ ./internal/agents/metrics/... ./internal/api/agents/... ./internal/server/...` all ok; `CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/` 142 requests, 0 differences.

Pitfalls: tests share process-wide hooks/flush state — start with `fresh(t)` (metrics.Reset + agents.ResetRuntime/ResetHooks + Wire). Test seams: `now` (naive clock), `utcNow`, `appendLines`, `replaceFile`, `recordStatus`. `ResetFlushState`/`SetStartedAt`/`WakeIsSet`/`ClearWake` are test helpers. The shared worktree sometimes holds other agents' non-compiling WIP (convert, rules); the commands below patch only the remote copy when needed.

Commands:
- `rt -- bash -lc 'cd server && gofmt -l internal/agents/metrics; go vet ./internal/agents/... ./internal/api/agents/... ./internal/server/... && go test -count=1 ./internal/agents/... ./internal/api/agents/... ./internal/server/...'` (last run with `-race`: metrics, api/agents, difftest, server ok; `internal/agents` failed only in the untracked `a01_reference_test.go` of the A01 reviewer's WIP)
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/'`
- `rt -- bash -lc '~/cf-test/.venv/bin/python -I server/internal/agents/metrics/testdata/probe.py'`

## A05 — traffic history and traffic store

**Status: `verified`** (review 2026-11-15, commits 6b3cd8d9, a449aa0c, 0b149290, 084b8416, 548348df; table under "Review") · prereqs: A04, C37 (`registrable_domain`, ported early here) · Go: internal/agents/metrics (traffic.go, traffic_wire.go, traffic_compat.go), internal/agents/trafficstore (new: store.go, stat_*.go), internal/discovery (new; C37's package: discovery.go), internal/api/agents/traffic.go

Files:
- `backend/agents/traffic_history.py` (504) → `metrics/traffic.go` (`TrafficHistory`, `CleanTrafficReport`, `RetentionDays`, `SetRetentionDays`, `AgentSupported`, `ChainLayers`; state per data dir: `TrafficFor(dataDir)` / `TrafficForManager(m)` = manager.traffic_history)
- `backend/agents/traffic_store.py` (265) → `trafficstore` (`CleanReport`, `RouteKey`, `SplitRouteKey`, `Store` with `AddReport`/`LoadStatus`/`StatusOf`/`Load`/`Delete`, `Get(ctx)` = get_traffic_store(); package-level lock and LRU status cache keyed by absolute path, like Python's module globals). Separate package so C37 can import it (discovery must not import metrics).
- `backend/routes/agents.py` (198) — lines 728–750, 781–884, 2100–2170 → `api/agents/traffic.go` (`registerTraffic`, one line in `Register`)
- Early from C37: `utils/domain_discovery.registrable_domain` → `discovery.RegistrableDomain` (on `subs.ExtractDomain`, no private suffixes), and `utils/domain_discovery_service.provider_revisions` → `discovery.ProviderRevisions(ctx, profileID)` (needed by the traffic-report answer; includes the parts of `read_settings`, `ruleset_problem`, `_references`/`ruleset_info` and `_served_rule_content` it uses). C37 extends this package.

Routes (8, all `done` in routes.tsv, `api/agents`): `POST /api/agents/<agent_id>/traffic-report`; `POST|GET|PUT|DELETE /api/agents/<agent_id>/traffic-history`; `PUT /api/agents/traffic-history/retention`; `GET /api/agents/<agent_id>/traffic/stats`; `GET /api/agents/<agent_id>/traffic/trend`. The two Agent-token POSTs are public in `internal/auth` already (as routes/auth.py); body limits 256 KiB / 4 MiB via `ReadJSONBody`; Agent auth via `authenticatedAgent` (light read). Errors outside Python's try blocks return the 500 page (traffic report/history routes), stats/trend answer `{'success': False, 'message'}` 500 like Python.

Commits: 5fb71710 (code, wiring), 473eb166 (tests, reference corpus), 7b6afce5 (differential), + this PROGRESS/routes.tsv commit.

Registered from `metrics.WireTraffic` (one line in `internal/server` `wirePackages`): `agents.RegisterHooks{DeleteTrafficHistory}` (delete_agent → traffic_history.delete), `agents.OnAgentDeleted("traffic_store", …)` (route → get_traffic_store().delete; first in `AgentDeletedStores`), `metrics.SetTrafficFlush(flushTraffic)` (metrics_flush.run: retain_agents + flush(retention_days(system)) → `traffic_rows`; also at shutdown through A04's shutdown run). The traffic history has no loop or task row of its own. `internal/server.TestDeleteAgentClearsEveryPerAgentStoreInAFullyWiredServer` now skips only for C16; new `TestNewAppWiresTheTrafficStores`.

Tests:
- `test_traffic_history.py` (13): all ported — `metrics/traffic_test.go` (clean report ×2, in-memory + queries, legacy migration, failed flush, compaction/pruning, delete/retain, flush task, delete agent) and `api/agents/traffic_test.go` (switch/reports/heartbeat, unsupported + retention, login enabled, `test_every_agent_token_route_bypasses_the_panel_login` → Go equivalent: every Agent-token route passes the panel login). Extra: traffic failure text `流量记录：…`, retention/choices, agent_supported, unknown/bad payloads, body limit.
- `test_domain_status_cache.py` (17 defs, 26 cases): `trafficstore/store_test.go` ports 14 defs. Not ported: `test_other_json_stores_keep_close_before_replace_without_digest` (= `jsonstore.TestOtherStoresKeepCloseBeforeReplaceWithoutSignature`), the two Windows-only tests. The two "during atomic write" cases monkeypatched os.replace inside json_store: jsonstore covers the detection; the store tests simulate a write that returns no signature.
- Deferred to A05 from elsewhere, now ported: `test_domain_discovery.py` (C37's file) `test_clean_report_*` (4 defs, 19 cases), `test_store_merges_reports_and_expires_old_days`, `test_store_caps_hosts_per_day_keeping_heaviest`, `test_registrable_domain_uses_public_suffix_list` (`discovery_test.go`), route cases `test_report_requires_token_and_enabled_switch` (switch via manager; PUT domain-discovery is A24), `test_report_with_login_enabled_rejects_invalid_agent_credentials` (7), `…_retains_switch_and_payload_checks` (2), `…_rejects_unknown_agent`; A01's traffic-report half of `test_heartbeat_and_agent_reports_never_copy_the_history` (`TestTrafficReportKeepsThePublicationHistory`). Still with C37/A24: the other `test_domain_discovery.py` route cases (need `/api/domain-discovery/*` or `PUT …/domain-discovery`).
- Reference corpus: `metrics/testdata/gen_traffic_reference.py` → `traffic_reference.json` (45 `_target_key` inputs incl. IPv6/scoped/mapped, IDNA, İ/Σ lowering, URL-like; 37 history clean_report cases; 16 store clean_report cases incl. unhashable TypeErrors; fromtimestamp rounding; float floor division; a query scenario with two flushes, every range × aggregate, compaction and pruning) — `traffic_reference_test.go`, all equal.
- Provider revisions: `api/agents.TestTrafficReportReturnsProviderRevisions` (cache file vs content, regional, after-MATCH and missing rulesets skipped).

Differential (`api/agents/difftest/traffic_test.go`, hooked into diff_test.go with 5 one-line edits: `seedTraffic`, `trafficRequests()`, mask `traffic_history_since` in stored agents, skip `.sqlite3` in the byte compare, `compareTraffic` after stop): seeds 3 Agents (domain discovery / traffic history / oxidns), discovery rulesets + a rule cache file, a saved traffic store file (expired + yesterday), a 1.8.51-go `traffic_history.sqlite3` (legacy tables, a deleted Agent's rows); 80 A05 requests (all 8 routes, success and failure: 401/404/409/400/413/500 pages, schema true, unhashable status, item without rule, legacy reports, retention variants, clear, switch off+clear, Agent delete with both stores). After SIGTERM / lifecycle.Stop: SQLite schema, journal mode and all rows equal; traffic/*.json equal (timestamps masked). Result: **222 requests, 0 differences**.

Differences from Python (documented):
- State per data dir (`TrafficFor`), not per manager object (as A04).
- Pending counters keep Python int semantics (int64, big.Int on overflow); a sum beyond int64 fails the save with Python's `Python int too large to convert to SQLite INTEGER`.
- `provider_revisions`: non-string ruleset names become JSON keys like json.dumps (null/true/numbers) but the TypeError of sort_keys on mixed key types is not reproduced; unhashable (list/dict) ruleset ids (Python TypeError) are compared with == instead. Only hand-edited configs can hit either.
- Traffic store merge on hand-edited files (non-dict day/hosts/stats): errors are returned (500 page as in Python), texts may differ from Python's exception texts (not visible to clients; only the "Exception on …" log).
- `host == host.lower()` uses ASCII lowering (non-ASCII hosts never pass is_valid_domain, same outcome).
- Windows (status cache disabled, close-before-replace) not applicable.
- Lone surrogates in reports: jsonx limitation (U+FFFD).
- `metrics/traffic_compat.go`: private copies of `rules` ipaddress (`ip_address` + str) and `str.lower` — TODO(port) if rules exports them.

Pitfalls: tests share process-wide state — reset `metrics.Reset()`, `metrics.ResetTraffic()`, `metrics.SetTrafficFlush(nil)`, `trafficstore.ResetCache()` (see `newTrafficEnv` / `wireTrafficForTest`). Test seams: `SetTrafficNow`, `TrafficHistory.SetBrokenConnect`, `Store.ReadJSON/WriteJSON`, `trafficstore.SetNow/SetCacheLimit/CachedPaths/CachedStatus`, `MaxHostsPerDay` (var). The difftest locates `backend/` at `../../../../..`: a scratch copy needs `backend` linked next to `server`.

Commands:
- `rt -- bash -lc 'cd server && gofmt -l internal/agents/metrics internal/agents/trafficstore internal/discovery internal/api/agents; go vet ./internal/agents/metrics/ ./internal/agents/trafficstore/ ./internal/discovery/ ./internal/api/agents/... ./internal/server/... && go test -count=1 ./internal/agents/metrics/ ./internal/agents/trafficstore/ ./internal/discovery/ ./internal/api/agents/... ./internal/server/...'` (last run also with `-race`: all ok)
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/'`
- `rt --back server/internal/agents/metrics/testdata/traffic_reference.json -- bash -lc '~/cf-test/.venv/bin/python -I server/internal/agents/metrics/testdata/gen_traffic_reference.py > server/internal/agents/metrics/testdata/traffic_reference.json'`

Review (adversarial, 2026-11-15) — traffic_history.py, traffic_store.py, routes/agents.py 728–750 / 781–884 / 2100–2170, metrics_flush traffic half, `registrable_domain`, `provider_revisions` (+ read_settings / ruleset_info / _served_rule_content / read_validated_rule_cache) walked branch by branch against Go. New reference corpus from CPython 3.11.17 / SQLite 3.53.1 on the test server (`metrics/testdata/gen_a05_reference.py` → `a05_reference.json`, asserted by `metrics/a05_reference_test.go`): 113 more `_target_key` inputs (IPv6 scope ids with spaces/case/non-ASCII/`%`/`/`, embedded IPv4, compression edges, full-width/Arabic digits, IDN, CJK, emoji, İ/ı/K/ß/Σ, ideographic/full-width dots, URL-like, private suffixes), `str.lower()` of every code point, final-sigma context of every code point, an int64-overflow scenario (pending sums beyond int64, REAL counters after `up + excluded.up`, every aggregate, compaction and trim with REAL rows, SUM() `integer overflow`), compaction/prune/query boundaries (rows at exactly 7 d / 2 d / 30 d, now at and just before midnight, clean_report hours at `now ± 2 d / 1 h` with an integral now), 20 hand-edited traffic store files, MAX_HOSTS_PER_DAY ranking with float/equal weights, 32 report hosts (non-ASCII, lengths, labels), ROLLBACK without a transaction. Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | med | SQLite turns an int64 overflow of `up + excluded.up` into a REAL; Python reads it back as a float (query JSON shows its repr, compaction/trim keep working with int+float arithmetic). Go scanned counters into int64: every query of that Agent answered 500 and every later flush failed (trim_day scan), so 「监控历史保存」 failed for good. Reachable by an Agent reporting counters near 2**62 three times | 6b3cd8d9 (`pyInt` float mode: exact int/float comparison, float(int) correctly rounded, REAL bound back; `tripleFromSQL`) | `TestA05ReferenceInt64OverflowInSQLite`, difftest "review traffic history query real/big *" + rows after stop |
| 2 | low | ROLLBACK paths: flush's `if connection.in_transaction` was Go's own flag (after SQLite ended the transaction itself, Go returned "cannot rollback - no transaction is active" instead of the real error); `_migrate_legacy` / `_delete_agents` raise the ROLLBACK error when it fails, Go swallowed it | 6b3cd8d9 | `TestA05ReferenceRollbackWithoutTransaction` |
| 3 | low | `_target_key`: ip_address accepts any IPv6 scope id without `%` (spaces, tabs, case kept: `fe80::1%A B`); Go refused whitespace anywhere and lowered the text as a domain | a449aa0c | `TestA05ReferenceTargetKeys`, difftest "review traffic history report odd targets" |
| 4 | low | `str.lower()` copy used Go's Unicode tables (newer than Python 3.11's 14.0: 55 letters such as U+A7CB, U+1C89, U+10D50… were lowered) and IsUpper/IsLower for "cased" (missing Other_Lowercase/Other_Uppercase: ª º Ⅰ Ⓐ 🄰 …) and a partial case-ignorable list in the final-sigma rule (474 code points differed) | a449aa0c (`traffic_unicode_tables.go` generated from CPython by `testdata/gen_a05_unicode_tables.py`) | `TestA05ReferenceLowerEveryCodePoint`, `TestA05ReferenceFinalSigmaContext` |
| 5 | low | traffic store on hand-edited files: `hosts` a list/str and a report without items succeeds in Python (len() works, setdefault never called), Go answered 500; non-dict/None `hosts` must be the len() TypeError; `ports.get(k, 0) + conns + fails` adds left to right (float rounding); host weights with floats were read as 0 (`bigOf`) | 0b149290 (`pyAdd`, `pyCompare`) | `TestA05ReferenceHandEditedStoreFiles`, `TestA05ReferenceStoreCapWithFloatWeights`, difftest "review traffic report edited *" + file after stop |
| 6 | low | PUT traffic-history: `except ValueError` comes before `except AgentDeploymentConflict`, and AgentDeploymentConflict is a ValueError → Python answers 400, Go 409; repository ValueErrors (ProfileExists etc.) answer 400 with str(exc) in both PUT routes (also retention) | 084b8416 (`setTrafficHistoryError`) | `TestSetTrafficHistoryErrorsFollowPythonExceptOrder` |

Porter's listed differences, decided:
- State per data dir: unobservable (as A04: the manager is rebuilt only when the repository changes, once at start).
- `provider_revisions` sort_keys TypeError on mixed key types and unhashable ruleset ids: **unreachable** — `_validate_document` requires every shared `rule_library` item to have a non-empty str id and a non-empty, unique str name, so keys are always distinct strings and the `library` dict never sees an unhashable id; list/None/True ruleset ids in `domain_discovery` never equal a str id (`missing` → skipped in both).
- Error texts of hand-edited store files: only the "Exception on …" log line (500 page either way); outcomes (error vs success, nothing written) now match (finding 5) and the error class is asserted. Remaining text differences (e.g. `'list' object is not subscriptable` vs `list indices must be integers…`) accepted.
- ASCII `host == host.lower()`: confirmed — all 14 non-ASCII hosts in the corpus (ä, İ, ı, K, ß, full-width, emoji, CJK) fail `is_valid_domain` in Python, so the outcome is decided by ASCII lowering.
- Windows, lone surrogates: accepted as listed.

Accepted differences added by the review: `float(int)` beyond 1e308 (OverflowError in Python) cannot occur for counters (< 2**62 per report); NaN host weights (hand edit) sort arbitrarily in Python; a non-INTEGER/REAL counter in a hand-edited database fails with a TypeError text of its own.

Checks performed (no finding): clean_report of both modules (schema `in`/`==` with True/1.0, item order of checks deciding TypeError vs drop, `_count`/`_text` limits in code points, chain rules, hour `%`/freshness with big ints), add_report/pending/flush restore-on-failure, executemany order (additive; errors roll back everything), `_compact` (floor division, int truncation, SQLite integer division, set of (agent, day)), `_trim_day` (ranking by (-total, target) per source, stable order, merge into `*`), `_prune` (`int(cutoff)`), delete/retain (memory filter, file existence, removed set), query (memory then DB order — int sums exact, float additions in the same SQL order; trend range; fromtimestamp rounding; omitted/part_count), retention_days/set_retention_days (bool/float/huge ints, validated dict system_config), agent_supported, traffic store `_read`/LRU status cache/signatures/load/load_status/delete (unlink EISDIR text), `_prune` with odd day keys, timestamps (naive local, seconds truncated; no DST dependence), routes (order of auth/body/switch/clean, 401/404/400/409/413/500, `_traffic_history_state`, stats/trend wrappers), `provider_revisions` ordering of read_settings errors, cache file vs content fallback, 404 for an unknown bound profile.

Differential: 12 review cases (REAL and over-int64 counters seeded next to the legacy tables, all aggregates, odd targets, hand-edited store file) — **234 requests, 0 differences**, traffic_history.sqlite3 (23 lines) and traffic/agent-edit.json identical after stop.

Review commands: `rt --back server/internal/agents/metrics/testdata/a05_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/metrics/testdata/gen_a05_reference.py > server/internal/agents/metrics/testdata/a05_reference.json'`; tables: see the header of `testdata/gen_a05_unicode_tables.py`. Last run: `go vet …` + `go test -count=1 -race ./internal/agents/metrics/... ./internal/agents/trafficstore/... ./internal/discovery/... ./internal/api/agents/... ./internal/server/...` all ok (server ok after the C13 route-list commit 37f1237f; before it `TestNewAppRegistersImplementedRoutes` failed on `settings.list_third_party_dependencies`, unrelated); `CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/` 234 requests, 0 differences.

## A06 — OxiDNS monitor, dashboard state, metrics

**Status: `verified`** (review 2026-11-15, commits 84e31b06–f8b84a42 + this PROGRESS commit, table below) · prereqs: A01 · Go: `internal/agents/monitor` (new), `internal/api/agents/oxidns.go`

Files:
- `backend/agents/oxidns_monitor.py` (727) → `monitor/monitor.go` (MonitorError = `*monitor.Error`, `_agent_origin` = `monitor.AgentOrigin`, exported for A07/A16/A20/A22/A23/routers; `api/agents.MonitorErrorResponse` is the shared JSON error answer)
- `backend/agents/oxidns_dashboard_state.py` (240) → `monitor/state.go` (`StateStore`, `ValidateState`, `StateError`, `ErrStateConflict`; 64 in-process semaphores + flock on `<digest>.lock`, both 5 s)
- `backend/agents/oxidns_metrics.py` (338) → `monitor/metrics.go`; Python number semantics in `pynum.go`, Unicode 14 decimals in generated `pydecimal_table.go` (`testdata/gen_decimal.py`)
- `backend/routes/agents.py` 2186–2198, 2216–2221, 2286–2325 → `api/agents/oxidns.go` (5 routes, `registerOxidns` in `Register`); routes.tsv rows set `api/agents done`
- Also ported here (needed by the overview): the pure part of `oxidns_publication_stats.py` — `advance`, `totals`, `_task`, `_new_state`, `PublicationPending` → `monitor/publication.go`. **A07 adds `checkpoint`/`complete` to the same package.**
- `oxidns_ranking_history` read side is a hook: `monitor.RankingHistory` (`HasAny`, `Query`) installed with `monitor.SetRankingHistory` (A07 registers the real SQLite store). Default: Python's answer when `metrics/oxidns_rankings.sqlite3` is absent (nothing stored), `ErrHistoryNotPorted` (500) when it exists. Constants `HistoryAgentVersion`, `HistoryRanges`, `HistorySlot`, … and `HistorySupported` live in `monitor/history.go` for A07 to reuse.

Commits: 073ea979 (code), 4c132564 (ported tests), fe9712df (CPython reference corpus), + difftest/PROGRESS commit.

Shared edits (small, additive): `agents.Request` gains `NoProxy` / `NoRedirects` / `MaxBody` and `Response.BodyError` (requests' `trust_env=False`, `allow_redirects=False`, `stream=True` + 5 MiB bound; status is checked before the body as in Python), `agents.Manager.Client()`, `agents.DecodeJSONBytes` (json.loads(bytes)); `urlx.Split/SplitResult/Unsplit` export the existing urlsplit port. No loop and no wire function (A06 has no background work; `_rankings_kept` is an in-memory cache with Python's 60 s reuse / 180 s backoff on `time.monotonic`).

Tests: `test_oxidns_metrics.py` 23/23 → `metrics_test.go` (test_nontext_input_is_rejected replaced by the Go type + `TestOverviewRejectsNonTextMetrics`); `test_oxidns_monitor.py` 24/24 → `monitor_test.go` (fake `agents.Client` instead of the patched Session) + 2 route tests in `api/agents/oxidns_test.go`; `test_oxidns_dashboard.py` 35/35 → `dashboard_test.go` (`doRequest` replaced like the patched `_request`; the cross-process lock test re-runs the test binary 4×) + reset route test in `oxidns_test.go`. No deferred cases.

Reference corpus: `testdata/gen_a06_reference.py` → `a06_reference.json.gz` (CPython 3.11.17; 122 metrics texts incl. Unicode digits/whitespace/line breaks, 7 baseline sets, 49 upstream labels, 48 ip forms, 29 origins, 69 param cases, 59 state texts, 40 ranking sections, 16 records, 313 fromtimestamp values, 33 Agent answers through `_request`, 1472 advance/totals transitions); `a06_reference_test.go` compares sorted JSON bytes: all equal.

Differential: `difftest/oxidns_diff_test.go` — real Flask and Go both pointed at one loopback fake OxiDNS Agent (answer chosen per phase): 61 requests (success + failure for all 5 routes, Agent 401/302/404/oversize/BOM/invalid UTF-8/NaN/code list, unreachable port), plus the requests the fake Agent received (path, raw query, Authorization, Accept, X-ConfigFlow-Query-Sources), dns-monitor state + lock files (generation masked) and DATA_DIR file list: **0 differences**. Agent versions in the differential stay below 1.8.55 so Python's ranking-history collector never touches the fake Agent.

Commands:
- `rt -- bash -lc 'cd server && go vet ./internal/agents/monitor/ ./internal/api/agents/... && go test -count=1 ./internal/agents/monitor/ ./internal/api/agents/'`
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Oxidns ./internal/api/agents/difftest/'` (run separately from the registry differential: lifecycle starts once per process)
- corpus: `cd server/internal/agents/monitor && rt --back server/internal/agents/monitor/testdata/a06_reference.json.gz -- bash -lc 'PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_reference.py'`

Decisions / known differences (for the reviewer):
- Lone surrogates (`"\ud800"` escapes, or UTF-8-encoded surrogates accepted by json.loads' `surrogatepass`) are the jsonx deviation: Go sees U+FFFD / rejects the bytes. Python would answer INVALID_RESPONSE for such a `metrics` text (both do), pass other such strings through, or 500 when re-writing a hand-edited state file. Not producible by the Go Agent (encoding/json never emits them).
- Agent JSON nested deeper than Python's recursion limit: Python 500 (RecursionError not caught in `_request`), Go parses up to jsonx.MaxDepth. Reset bodies (≤ 2048 bytes, up to ~1024 levels): Python's RecursionError threshold depends on the stack depth, Go accepts — the answer is then INVALID_QUERY with the other message. Not exercised.
- Token header: sent latin-1 like http.client; a non-latin-1 token is AGENT_UNREACHABLE in both. Control characters in a token: Go's transport refuses (AGENT_UNREACHABLE), http.client sends some of them. Tokens are server-generated.
- Content-Encoding: requests decodes gzip/deflate; Go's transport only gzip (Agents never compress).
- ip_address uses netip plus Python's scope-id rules; corpus confirms the 48 forms incl. `::ffff:1.2.3.4` (3.11.17 prints it mapped).

Not part of A06: `checkpoint`/`complete` hooks and the ranking-history store/loop (A07).

Review (adversarial, 2026-11-15) — every function and branch of oxidns_monitor.py, oxidns_dashboard_state.py, oxidns_metrics.py, the pure part of oxidns_publication_stats.py and the five routes walked against monitor.go/state.go/metrics.go/pynum.go/pyip.go/publication.go/history.go/oxidns.go, plus the shared `agents.Request` edits. New CPython 3.11.17 references (synthetic values), all asserted by `a06_review_test.go`:
- `testdata/gen_a06_review.py` → `a06_review.json.gz` (randomised, 18k cases): 2500 metrics texts (Unicode digits/whitespace incl. U+3000/U+001F/U+FEFF edges, all 10 `splitlines` breaks, label escapes `\"` `\\` `\n` and invalid ones, duplicate/alias series with 1 vs 1.0, NaN/Inf/1e400/309-digit/4301-digit values, timestamps), 700 baseline/carried triples (counter resets, huge ints, -0.0, mixed int/float sums beyond the float range), 2500 ip_address strings (random IPv6 compression/embedded IPv4/zones/mutations), 2006 upstream labels through urlsplit, 4800 query/client/dashboard parameter sets, 1500 ranking sections, 1200 clients / 800 queries / 500 query answers and 1200 overview snapshots and 1500 live-ranking answers after `_request` (odd JSON values everywhere: bools, -0.0, 1e308, inf/nan, 2^53, lists/dicts), 2500 mutated dashboard states, 29 raw state files (BOM, UTF-16/32, duplicate keys, 4300/4301 digits, deep nesting), 300 target digests.
- `testdata/gen_a06_flows.py` → `a06_flows.json.gz`: 400 random overview/reset sequences (2917 steps) on one data dir with a scripted monotonic clock and numbered generations — counters rising/restarting, core/configuration/runtime changes, clock regressions, token and version (incl. ≥ 1.8.55 history status) changes, rankings failing/timing out/changing/answering another recorder: every answer, the Agent calls and the **exact state file bytes** after each step equal Python's.

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | An Agent registered with a scoped IPv6 host (`fe80::…%eth0`, `::1%1`; `_agent_origin` keeps the scope) was unreachable in Go: `net/url` refuses `%eth0`. requests reaches it: `requote_uri`'s InvalidURL fallback turns every `%` of the URL into `%25` (so the query escapes are double-encoded, as Python sends them), urllib3 hands `fe80::1%eth0` to glibc, which accepts an interface name only for link-local/multicast addresses and a decimal zone ≤ 2^32−1 for any address (`::1%lo` fails resolution). Shared `agents.HTTPClient` (manager calls to bracketed zone hosts had the same defect); A01's `%zz` expectation was wrong and is corrected | 84e31b06 (`unquoteUnreserved` fallback, `encodeZone`, `glibcScopeOK` in the dialer) | `TestRequoteURIFallbackAndZonesLikeRequests`, `TestGlibcScopeRules`, `TestScopedIPv6AgentIsReachedLikeRequests`, `TestScopedAgentHostIsReachedLikePython`, differential `zone *` cases |

Differential extended (806ea210): fake Agent also on `[::1]`; Agents with host `::1%1` (clients, queries with a CJK/`:` query) and `[::1%lo]` (unreachable, no Agent call in either), truncated body (Content-Length > sent, close), > 5 MiB chunked body, gzip body (decoded by both), gzip expanding beyond 5 MiB, Dashboard Agent whose rankings endpoint answers 404 text: **69 requests, 0 differences**. Registry differential after the client change: 234 requests, 0 differences.

Porter's listed differences, decided (all accepted, none observable from a real Agent or the UI):
- Lone surrogates (jsonx-wide): the Go Agent's encoding/json never emits them and OxiDNS metrics are Rust strings; only a hand-edited state file can hold one (the two such raw-state cases are skipped in `TestReviewStateFiles` with this reason).
- JSON deeper than Python's recursion limit: Agent answers are encoding/json output of bounded core data; a reset body of ~900–1024 nested levels (≤ 2048 bytes) is 400 INVALID_QUERY in both, only the message differs (Python's threshold depends on its stack depth).
- Control characters in an Agent token: tokens are `secrets.token_urlsafe(24)` from registration.
- `Content-Encoding: deflate`: Agents never compress; gzip verified equal in the differential.
- ip_address via netip + scope rules: 2548 forms equal (corpus + review fuzz).

Checks performed (no finding): `_agent` (falsy agent, service type, version errors), `_agent_origin` (bracket stripping, hostname regex, bool/float/huge ports), `_request` (header latin-1, X-ConfigFlow-Query-Sources only for `queries`, params order/encoding, trust_env, no redirects, status before body, 5 MiB bound incl. chunked/gzip, body read errors, json.loads(bytes) BOM/UTF-16/32/surrogatepass/4300 digits, `success`/`ok` `is False`, unhashable `code`, status mapping, 404 upgrade texts), `_number`/`_text`, all parameter validators, `_display_upstream`/`_public_upstreams` (digests, address lookup), `_overview_snapshot` (all dashboard checks), `_rankings_unavailable` texts, `_ranking_section`, `_kept_rankings` (60 s reuse, 180 s backoff, key, manual, previous answer on timeout, `time.monotonic` regression `0 <=`), `_dashboard_rankings`, `_history_rankings`/`_history_status` (floor arithmetic, A07 store hook), `_dashboard_overview` (order, except mapping, CAS expected/None, statistics publication_count truthiness, fromtimestamp rounding), queries/query/clients, parse_metrics/summaries/baseline/zero/add (exact int/float comparisons, OverflowError paths, sort keys), state validation/read/CAS/locks (64 in-process locks + flock, 5 s, `.lock` file, mkstemp/fsync/replace/dir fsync, temp cleanup), advance/totals/_task, the five routes (bounded reset body, args, Content-Length, is_json, no-store, error JSON), `Request.NoProxy` (`TestNoProxyIgnoresTheEnvironment`, f8b84a42). Time zones/DST: not applicable (A06 uses only UTC timestamps).

Observation for A01 (not an A06 path, A06 sends with NoProxy): with HTTP_PROXY set, Go's `ProxyFromEnvironment` bypasses `localhost`/loopback hosts while requests (trust_env) proxies them unless NO_PROXY says otherwise — only manager calls to a loopback Agent through an environment proxy differ.

Review commands (run on a private detached worktree of HEAD; the shared tree held other agents' WIP):
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... && go test -count=1 -race ./internal/agents/... ./internal/api/agents/... ./internal/server/...'`
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Oxidns ./internal/api/agents/difftest/'` and separately `-run TestDifferential`
- corpora: `cd server/internal/agents/monitor && rt --back server/internal/agents/monitor/testdata/a06_review.json.gz -- bash -lc 'PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_review.py'` (same with `gen_a06_flows.py` / `a06_flows.json.gz`)

## A07 — Mihomo panel, OxiDNS cache/rule update/rankings/publication stats

**Status: `ported`** (porter 2026-11-15, not reviewed) · prereqs: A06, C14 · Go: internal/agents/monitor (panel.go, cache.go, ruleupdate.go, rankings.go, publication_hooks.go, wire.go, pydigit_table.go), internal/api/agents/panel.go

Files:
- `backend/agents/mihomo_panel.py` (375) → `monitor/panel.go` (`PanelCall`, `CheckCoreUpdate`, `PanelRoutes` whitelist, `slim`/`slimProxy`, core-version cache `cachedLatest` with per-channel `TryLock`; seams `requestRule`, `latestCoreVersion`, `downloadSettings`, shared `monotonic`)
- `backend/agents/oxidns_cache.py` (134) → `monitor/cache.go` (`CacheCall`, `ValidRequestTags`, `CacheFail`, `FlushError{Err, Flushed}` = MonitorError with `flushed`)
- `backend/agents/oxidns_rule_update.py` (166) → `monitor/ruleupdate.go` (`RuleUpdateCall(ctx, m, id, method, paths)` for A16 too, `SupportsPaths`; HA pair hook `SetHARules` with default `ErrHARulesNotPorted` (500) until A18; `*HABusyError` = rules' ValueError → SERVICE_BUSY 409)
- `backend/agents/oxidns_ranking_history.py` (402) → `monitor/rankings.go` (`RankingStore` = RankingHistory, `GetRankingStore`, `CollectAgent`, `CollectRound`, `LastCompleteSlotEnd`, loop `rankingLoop`)
- `backend/agents/oxidns_publication_stats.py` (158): `checkpoint`/`complete` → `monitor/publication_hooks.go` (`Checkpoint`, `Complete`); the pure part was A06's `publication.go`
- `backend/routes/agents.py` 2199–2215, 2222–2250, 2251–2285 → `api/agents/panel.go` (`registerPanel`, one line in `Register`); routes.tsv 9 rows `api/agents done`

Wiring (`monitor.Wire`, one line in `internal/server` wirePackages): `SetRankingHistory(GetRankingStore)`, `agents.RegisterHooks{PublicationComplete, PublicationCheckpoint}`, `lifecycle.Loop("oxidns-ranking-history")` once per process (flock `DATA_DIR/.oxidns-rankings.lock` LOCK_NB, then a round every 60 s). No task row (Python has none).

Shared edits (additive): `urlx.Quote(s, safe)`; `agents.HAField`/`agents.HAActive` exports in `agents/compat.go`; `monitor.rankingSection` → wrapper of new `rankingSectionRows(value, kind, limit)` (rows=ROWS for the history).

Commits: 05f5216c (code), 0d7a130e (panel/cache/rule-update tests), fd24d620 (ranking tests), bafd9209 (publication tests), cc108f97 (route tests), 17406a4b (CPython corpus), 21c7d253 (differentials), 85852810 (gofmt).

Tests: `test_mihomo_panel.py` 30/30 (`panel_test.go`; 2 route cases in `api/agents/panel_test.go`); `test_oxidns_cache.py` 8/8 (5 call-level in `cache_test.go`, 3 route); `test_oxidns_rule_update.py` 11/11 (9 + 2 route); `test_oxidns_ranking_history.py` 21/21 (`rankings_test.go`; random records from Go's PCG — expectations are exact counts of the same records); `test_oxidns_publication_stats.py` 14/14 (`publication_test.go`); deferred A01 case `test_panel_lookups_do_not_copy_the_history_*` → `TestPanelLookupsDoNotCopyTheHistoryButPublicationStatisticsDo`. **Deferred:** `test_oxidns_publication_core.py` (1, skipped unless OXIDNS_TEST_BINARY: real core) → finish/lab; `test_oxidns_publication_deployment.py` (3: `publish_deployment`, `oxidns_env`, `ha_publish`) → A14 (2) and A18 (1). Extra Go tests: Python int()/isdigit rules, non-JSON/oversized/unhashable answers, cache status mapping and KeyError token, HA branches, slot answer validation, non-str version AttributeError in collect_round, complete/checkpoint guards.

Decisions / differences (for the reviewer):
- `_latest_core_version` passes no `host_url` (flask.request.host_url) to request_rule: it only matters when the panel itself is served at github.com.
- Ranking counters are int64 (SUM/upsert overflow to REAL is unreachable: ≤ 10,000 records and ≤ 6.6e8 per row per slot).
- `active(agent)` with a truthy non-dict FIELD / unhashable status: Python raises, `agents.HAActive` answers false (registry-wide compat behaviour).
- SQLite driver error texts are the driver's (only in logs / 500 page).

Reference corpus (`testdata/gen_a07_reference.py` → `a07_reference.json.gz`, CPython 3.11.17, asserted by `a07_reference_test.go`): 169 `mihomo_panel.call` cases (names incl. CJK/emoji/512/513/control chars, log cursors with Unicode digits/superscripts, int() timeouts incl. Arabic/full-width digits/underscores/4301 digits, test URLs, bodies, every whitelisted pair and some outside, 26 Agent answers: empty/whitespace/non-JSON/404/BOM/UTF-16/NaN/1e400/4301-digit/redirect/transport error, slimming of proxies and providers, versions/service/token/host variants), 54 `oxidns_cache.call`, 68 `oxidns_rule_update.call` (each compared with the request that reached the Agent: method, prepared URL, body, Authorization/Accept/Content-Type, timeout, trust_env, redirects, stream), `_newer_release`, version.txt rule, `last_complete_slot_end`, and a ranking scenario of 27 operations (collect with slot splitting and partial slots, add/upsert, query cache, has_any, fetched, maintain with day merge, DAILY_TOP cut, re-merge into an existing day, expiry) with every stored row (rowid), sqlite_master, journal mode and header bytes 18–19: **all equal**. Corpus note: the generator's patched Session.request never reaches http.client, so a non-latin-1 token "succeeds" there; the Go test expects AGENT_UNREACHABLE (what real requests does; confirmed by the differential's A06 counterpart).

Differential (`api/agents/difftest/panel_diff_test.go`): real Flask and Go against one loopback fake Agent (phase per request), rule download proxy seeded as a closed loopback port (the core update check never leaves the host): **112 requests** (success and failure of all 9 routes; auth 401s, HEAD/OPTIONS, 404 NOT_ALLOWED, slimming, Unicode names/digits, Agent 401/302/404/non-JSON/16 MiB+1/NaN/BOM/unhashable code, core update cached/forced/alpha, cache partial flush/[{}] 500/oversize/BOM, rule update tasks/HA busy/old Agents) + the requests the fake Agent received + DATA_DIR files: **0 differences**. `TestRankingCollectorDifferentialAgainstFlask`: both servers' collector loops run their first round (60 s) against one fake OxiDNS Agent (1.8.60-go, plus a 1.8.54-go one that must be skipped): 24 windows each asked once per server, `metrics/oxidns_rankings.sqlite3` rows (rowid), sqlite_master and journal mode **identical** (112 lines). A06's differential re-run after the store was wired: 69 requests, 0 differences.

Known difference found by the corpus: CPython keeps each sqlite3 connection until the cyclic GC collects it, so its `-wal`/`-shm` files exist or not depending on GC timing; Go closes every connection at once (the last close checkpoints and removes them). Data is the same; only transient file presence differs.

Last run: `go vet` + `go test -count=1 -race ./internal/agents/monitor/ ./internal/api/agents/ ./internal/server/ ./internal/agents/` all ok; differentials as above.

Commands:
- `rt -- bash -lc 'cd server && go vet ./internal/agents/monitor/ ./internal/api/agents/ && go test -count=1 ./internal/agents/monitor/ ./internal/api/agents/ ./internal/server/'`
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Panel ./internal/api/agents/difftest/'` and, separately (≈70 s), `-run RankingCollector` (lifecycle starts once per process)
- corpus: `rt --back server/internal/agents/monitor/testdata/a07_reference.json.gz -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a07_reference.py'`
- digit table: `rt --back server/internal/agents/monitor/pydigit_table.go -- bash -lc 'cd server/internal/agents/monitor && ~/cf-test/.venv/bin/python testdata/gen_digit.py > pydigit_table.go'`

## A08 — installer script rendering, install command store

**Status: `partial`** (code, routes and differentials complete; 7 of 15 Python test files ported, 8 left — see "Left") · HR · prereqs: A02 (`LATEST_AGENT_VERSION`), C02 (resource) · Go: internal/installer (new), internal/api/agents/installer.go

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

Port log (A08 porter, 2026-11-15/16):
- Commits: cd535d46 (renderers, store, embedded scripts), 75b71b0b (gofmt), dac0c164 (render matrix), b1edca5d (routes + Flask differential, routes.tsv 8 rows `done`).
- Go: `internal/installer` — `output.go` (with_install_output/with_root_privileges), `goagent.go`, `aio.go`, `core.go`, `gateway.go`, `docker.go` (compose via yamlx with shared logging/identity objects → same anchors), `store.go` (SQLite snapshots, BEGIN IMMEDIATE, 0700/0600), `fingerprint.go`, Python-semantics helpers `pyint.go` (int() incl. 4300-digit limit message), `pyip.go` (ipaddress IPv4Address/IPv4Network strict, netmask/hostmask), `pyurl.go` (urlsplit 3.11.17 incl. bracket checks and NFKC netloc check, 19-char table), `templates.go` (go:embed `scripts/*.sh`, shlex.quote, single-pass placeholder substitution). `constants_gen.go` generated by `testdata/gen_constants.py` (run gofmt after regenerating).
- Routes (internal/api/agents/installer.go, one line in Register): core-install-script, gateway-network-probe.sh, install-command, core-install-command, i/<token>, install-script, docker-agent-compose, docker-agent-run. `GET /api/agents/download/<filename>` left for A10. Auth gate entries already existed.
- Decision: `GeneratorFingerprint` is a constant (Python hashes backend/agents/*install*.py + docker_ha_guide.py, which the Go image does not ship). `TestGeneratorFingerprintMatchesPython` recomputes Python's algorithm from backend/agents + agents.LatestAgentVersion and fails on drift; `TestScriptsMatchBackend` fails if the embedded copy drifts. When Python files change, update the constant (and copy scripts).
- Byte identity: `TestRenderMatrixMatchesPython` replays `testdata/matrix.jsonl` (949 cases from `gen_matrix.py`: 507 rendered, 442 refusals incl. exact ValueError texts) — 0 differences. HTTP differential `TestInstallerDifferentialAgainstFlask`: 295 comparisons equal, 0 differ (scripts with every form option, forwarded headers, latin-1 header, odd ports/names/URLs, install-command → download bytes, core-install-command, malformed bodies, 413, digit-limit 500, stale/NULL-generator/expired/unknown/malformed tokens, store full 503 and recovery, pre-generator schema migration, file modes).
- Commands: `rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/ && go test -count=1 ./internal/installer/ ./internal/api/agents/ ./internal/server/'`; differential `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Installer ./internal/api/agents/difftest/'`; regenerate matrix `rt --back server/internal/installer/testdata/matrix.jsonl -- bash -lc '~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_matrix.py > server/internal/installer/testdata/matrix.jsonl'` (`--full` + `INSTALLER_MATRIX_FULL=` for line diffs).
- Tests ported (Go files in internal/installer unless noted; shell scenarios run with sh/dash/bash on the test server with stub PATH, sandboxed /run/systemd/system, Python stub commands as in the Python harness): `test_install_output.py` → install_output_test.go (all 12); `test_agent_shell_output.py` + `test_docker_agent_cli_output.py` → shell_output_test.go (all 8, cli-output.sh run from the embedded copy, decision 2); `test_agent_offline_update.py` → offline_update_test.go (all 3, update-agent.sh); `test_agent_native_aio_install.py` → native_aio_test.go (all 31; Python-only type cases `manage_ipv4_forwarding/manage_tun=None/1`, float port are ruled out by Go types); `test_agent_transaction_install.py` → transaction_install_test.go (9 native cases; the 4 Docker entrypoint cases test docker/docker-agent-entrypoint-service.sh → finish domain #40); `test_gateway_agent_uninstall.py` → gateway_uninstall_test.go (all 3); `test_oxidns_docker_install.py` → oxidns_docker_test.go (6 generator cases) + internal/api/agents/installer_test.go (API case); 4 entrypoint/supervisor/keepalived cases → finish #40. Deferred C02 case `test_safe_url_queries.py::test_generated_go_installer_does_not_print_binary_download_url` → oxidns_docker_test.go. Helpers in harness_test.go (`runShell`, `shells`, `ok`, `pythonExe`), native harnesses `newAIOHarness`/`newNativeHarness` reusable by the remaining ports.
- Left (next porter, same pattern): `test_core_install_script.py` (49, harness with gzip/tar fixtures + Python stubs), `test_gateway_core_install.py` (34, needs `without_reviewed_installer_deltas`), `test_agent_install_dependencies.py` (21, imports harnesses of A10's `test_aio_agent_core_install.py`/`test_single_agent_core_install.py` — port those harnesses here or with A10), `test_installer_forwarding_context.py` (13), `test_standalone_forwarding_install.py` (21, also imports `test_agent_install_commands` fixtures and regression digests), `test_aio_network_prerequisites.py` (5), `test_shell_ha_prepare.py` (11, uses the single-agent harness and the install-script route), and the installer parts of `test_router_split_regression.py` (`single_service_script_digests` vs `backend/testdata/router_split_regression_0ef9e89.json`; generate the delta tables `_NATIVE_HA_DEPENDENCY_CASES`, `_GATEWAY_DEPENDENCY_CASES`, `_MANUAL_INSTALL_COMMAND`, `_GATEWAY_DEPENDENCY_REWORDS`, `_OPENRC_STOP_RETRY_DELTAS`, `_RESOLVED_STUB_PROMPT_DELTAS`, `_INSTALLER_HARDENING_DELTAS`, `_UNIFIED_CLEANUP` pieces into a JSON golden with a gen script instead of hand-copying). Byte identity of every rendered script is already proven by the matrix, so these ports exercise behaviour only.
- Pitfalls: the shared tree may not build (another agent's uncommitted x/net/idna import broke internal/api/agents on 2026-11-16) — verify on `git worktree add --detach ~/program/configflow-go-a08 HEAD`. `rt -- … go test ./internal/installer/` takes ~30–50 s (Python stub processes in the AIO harness). Run `gofmt` after regenerating constants_gen.go. The Python reference run `rt -- … pytest backend/test_x.py` printed "no tests ran" from the repo root here; run it from the worktree root with `-p no:cacheprovider` and check the rootdir if it happens again.

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
5. **Open item for A01 (found by the A06 reviewer, 2026-11-15):** with `HTTP_PROXY`/`HTTPS_PROXY` set,
   Go's `agents.HTTPClient` bypasses the proxy for localhost/loopback Agents (Go's
   `ProxyFromEnvironment` rule), while requests 2.31 proxies them unless `NO_PROXY` excludes them. Fix in
   the shared client with requests' `should_bypass_proxies` semantics plus a regression test and a
   differential case; assigned to the next agent that touches `agents/client.go` (A07 reviewer). A01 stays
   `verified` with this item listed; it must be closed before the domain differential.
