# PROGRESS — domain `agents`

N7-4 writer follow-up (implementation complete, coordinator verification pending):
installer Compose/guide now use native Nodes/Marshal and the single yamlbridge;
A12 pushdiff's native writer stays intact, with transition/redaction regression.
The writer-only real-Python oracle verifies8623 rows plus4 explicit HTTP NEL
rows; NEL/LS/PS are counted separately and round-trip exactly. The A19 alias
budget change is noted in its section. .64 full gate PASS (race,12diff,9fuzz,
proddiff new0/stale0); post64815165 rebase vet/changed packages and all13diff
pass. Detailed sources, classifications and commands:
[progress/n7-4-writers.md](progress/n7-4-writers.md).

Domain `agents` (WORKFLOW.md): Agent registry/heartbeat, metrics/traffic, OxiDNS/Mihomo monitoring, installers, Docker HA, deployment bundle, push diff, content sync, publishing, upgrades, address follow, rule sync, DNS follow, DNS HA, Linux gateway, gateway onboarding/installation, IPv6 readiness (old packages #20–#31). **69 Python files, 25154 lines, 24 modules** (equal to the `agents` totals in `FILES.md`) — 11 missing, 0 partial, 1 ported, 12 verified (A01, A02, A03, A04, A05, A06, A07, A08, A09, A10, A15, A19). Only reviewers set `verified`.

Planning audit: 2026-11-15, worktree `configflow-go`, branch `go` at 496004d7. The Go code was read directly (`internal/agents`, `internal/api/agents`, `internal/notifications`, `internal/tasks`, `internal/server`); the hand-back in `progress/20-agents.md` was not relied on.

## N7-3c — installer/discovery YAML readers (ported)

Branch `go-n7-3c`. Compose metadata/annotations and custom Mihomo port discovery
use Parse/Node/Decode. Installer metadata refuses composite JSON keys with a
Chinese message; annotations ignore non-string keys by their documented path
schema. Discovery ignores opaque keys while retaining known inbound/listener
ports; tagged integer Decode failures refuse explicitly instead of producing zero.

**8246 input occurrences**: equal 8242; native diagnostics 1; D1 1;
复合映射键：Node 保留 1; 未知标签：Python 拒绝 → Go 接受 1; other categories 0.
8239 Compose inputs include independently regenerated Python artifacts whose
original recorded hashes are verified before extracting reference semantics.
ZIP member names/permissions and non-Compose bytes remain strict; tamper tests
reject changed values/types, shell bytes, permissions and extra members.

Every entry has depth-40 alias amplification/cycle regressions and 10000/10001
plain-depth checks. Compose annotation traversal prunes unknown paths rather than
copying ever-longer paths. Gate counts:
[progress/n7-3c-gate.md](progress/n7-3c-gate.md); commands/final state:
[progress/n7-3c.md](progress/n7-3c.md). No installation or device writes are executed.

Final acceptance: gofmt/vet, scope race, full suite, all 12 live difftest packages,
and eleven 60s fuzz targets pass on .32/.64. Proddiff:
coverage_regressions=0/new=0/stale=0; full command/output record at the link above.

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
| A07 | Mihomo panel, OxiDNS cache/rule update/rankings/publication stats | 1316 | verified | | A06, C14 | 9 | internal/agents/monitor, api/agents |
| A08 | installer script rendering, install command store | 1333 | verified | HR | A02, C02 | 8 (8 done) | internal/installer, internal/api/agents |
| A09 | Docker HA guide, preflight, enrollment | 1083 | verified | HR | A08, A01 | 2 (2 done) | internal/installer, api/agents |
| A10 | Docker HA compose variants, installer routes | 1018 | verified | HR | A08, A09 | 9 (9 done) | internal/installer, api/agents |
| A11 | deployment bundle, locations, Mihomo resources, generated preview | 1264 | ported | HR | A02, A19, C05, C12, C14, C15, C21, C22, C25 | – | internal/deploy (new) |
| A12 | push diff | 1465 | verified | | A11 | – | internal/deploy/pushdiff |
| A13 | content sync, Agent list/detail, push-diff routes | 584 | verified | | A12, A15 | 4 | internal/deploy, api/agents |
| A14 | publishing: transactional content, push-config, deployments, receipts | 1002 | missing | HR | A11, A13, C13, C17, C18, C19, C20, C22, C23 | 9 | internal/deploy, api/agents |
| A15 | upgrades, Agent operations, address follow | 745 | verified | HR | A01, A02 | 15 (15 done) | internal/agents/upgrades (new), api/agents |
| A16 | Agent rule sync, projection, snapshot | 1371 | missing | HR | A07, A14, C25, C27 | 2 | internal/agents/rulesync (new), api/agents |
| A17 | DNS follow | 760 | missing | HR | A14, C26 | 4 | internal/agents/dnsfollow (new), api/agents |
| A18 | DNS HA (keepalived pair, handoff, pair publication) | 1529 | missing | HR | A16, A17, C26, routers (`routers/store.py`) | 4 | internal/agents/dnsha (new), api/agents |
| A19 | Linux gateway core (common/linux_gateway.py) | 518 | verified | HR | C05, C08, routers (`router_split.check_generation`) | – | internal/gateway (new) |
| A20 | Agent gateway bridge, gateway draft, gateway routes | 1110 | missing | HR | A19, A06, C18, routers (`router_split`, `routers/store.py`) | 18 | internal/gateway, api/agents, api/linuxgateway (new) |
| A21 | gateway onboarding | 751 | missing | HR | A19, A20, C18, C22, C23, C24, C05, routers (`router_split`) | 3 | internal/gateway, api/gatewayonboarding (new) |
| A22 | gateway installation | 809 | missing | HR | A08, A14, A20, A21, C17 | 7 | internal/gateway, api/agents |
| A23 | IPv6 readiness, client evidence, verification | 1344 | missing | HR | A06, A13, A14, C16, routers (`ipv6_readiness`, `routeros`, `route_editor`, `rule_sync_store`, `store`, `client_devices`) | 5 | internal/agents/ipv6 (new), api/agents |
| A24 | client tools, OxiDNS device policies, domain-discovery toggles | 198 | verified | | A05, C09, C26, C31, C37 | 8 | api/agents |

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

Proxy follow-up (2026-11-17, `go-review-n8-2`): the historical A07 decision 5
and requests proxy emulation described below are superseded. Shared weburl now
uses native x/net/httpproxy with ALL_PROXY fallback. Loopback always direct;
CGI fails before any request. The actual requests differential has 42 precise
Python/Go outcome assertions; Agent/Sub-Store boundaries cover fallback,
priority, CGI, explicit proxies and redirects. [Accepted differences](behaviour-changes.md),
[review](review-n8-2.md). No new dependency.

Environment proxies (coordinator decision 5, closed by the A07 review, ce75e46a): `agents.HTTPClient` no longer uses Go's `ProxyFromEnvironment` (which bypassed localhost/loopback and ignored ALL_PROXY); `proxy.go` ports requests 2.31's choice for trust_env sessions: `should_bypass_proxies` (no_proxy/NO_PROXY via `os.environ.get(k) or …upper()`, spaces removed, IPv4 hosts by glibc `inet_aton` with `is_valid_cidr`/`address_in_network` incl. `int()` masks and exact-IP entries, other hosts by plain suffix of host or host:port) plus urllib's `proxy_bypass_environment` (`'*'`, stripped/lowercased/dot-trimmed names), `getproxies_environment` (any `*_proxy` name, lowercase wins, empty lowercase removes, REQUEST_METHOD drops HTTP_PROXY, environment re-read per request), `select_proxy` keys (`http://host`, `http`, `all://host`, `all`), `prepend_scheme_if_needed` + urllib3 2.8 `parse_url` (InvalidProxyURL without host, InvalidURL for bad host/port or non-http(s) scheme, InvalidSchema for socks without PySocks, port default/0) and `proxy_headers` (Basic only with a password and a non-empty user, latin-1, `%` decoding as urllib3 re-encodes the userinfo). Regression: `TestReferenceEnvironmentProxies` (994 cases from real requests/urllib3, `testdata/gen_proxy_reference.py`), `TestLoopbackAgentGoesThroughTheEnvironmentProxy`; client differential `TestProxyDifferentialAgainstRequests` (CONFIGFLOW_DIFF=1: real requests vs HTTPClient through a loopback recording proxy to an Agent on 127.0.0.1, 14 environments × 2 hosts: request target, Host, Proxy-Authorization, Authorization identical). No ported route sends a trust_env request yet; the first A14/A15 route that does should add a route-level case to `api/agents/difftest`. Accepted: NO_PROXY is compared with a non-ASCII Agent host in its Unicode form (requests: punycode) and non-ASCII proxy host labels are not IDNA-encoded (no golang.org/x/text); an https proxy is verified with the system roots (requests: certifi); a non-latin-1 proxy credential is a plain error (requests raises UnicodeEncodeError, not a RequestException) — none occur with ConfigFlow's loopback/IP Agents. (Review C13, 2026-11-16: the implementation moved unchanged to the shared `internal/reqproxy` — also used by the Sub-Store client, with `ForRedirect` for requests' redirect-hop choice; `agents/proxy.go` now only binds it to `os.environ`, and the reference/differential tests above still run against it.)

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

N8-6 follow-up (request switch is local; no A04 product code changed):
`api/agents/agents.go` heartbeat uses the legacy body reader, then ValidateSystemMetrics /
Manager.UpdateHeartbeat → metrics.Wire AddMetrics hook → History.AddMetrics → pending JSONL/flush.
A04 must reject or discard non-finite points before memory/file insertion, including finite JSON
exponents that overflow when converted. Also review saved JSONL/readLegacy loading: difftest
seedMetrics/oddMetrics already seed NaN; those data/history/summary responses retain N8-1's explicit
NaN/Infinity→null checks. Do not replace those rows with a broad normalize. When heartbeat's request
DTO migrates, explicitly assert whole-body NaN/Inf→400 in its HTTP differential (N8-7+ rule).

N8-1 follow-up (coordinator, 2026-11-17): reject or discard non-finite metric
points (NaN/±Infinity) at Agent report ingestion before storing them. N8-1
repairs JSON responses to null at the common native exit; it intentionally does
not change ingestion/storage here. See parity/behaviour-changes.md.


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

N8-6 follow-up (deferred A05 governance, no product code changed): audit POST traffic-report and
POST traffic-history (`api/agents/traffic.go`, legacy agents.ReadJSONBody at 256KiB/4MiB), then
traffic history/store insertion and aggregation. Non-finite or conversion-overflow counter/point
values must be rejected or dropped before ingestion; choose the exact point policy with A04/A05.
The eventual binding migration records request NaN/Inf→400 explicitly. Persisted/history seeds
need their own finite-point handling; changing the request decoder alone cannot clean old samples.

N8-1 follow-up (coordinator, 2026-11-17): reject or discard non-finite metric
points (NaN/±Infinity) at Agent report ingestion before storing them. N8-1
repairs JSON responses to null at the common native exit; it intentionally does
not change ingestion/storage here. See parity/behaviour-changes.md.


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

Observation for A01 (not an A06 path, A06 sends with NoProxy): with HTTP_PROXY set, Go's `ProxyFromEnvironment` bypassed `localhost`/loopback hosts while requests (trust_env) proxies them unless NO_PROXY says otherwise — **fixed by the A07 review (ce75e46a, see A01).**

Review commands (run on a private detached worktree of HEAD; the shared tree held other agents' WIP):
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... && go test -count=1 -race ./internal/agents/... ./internal/api/agents/... ./internal/server/...'`
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Oxidns ./internal/api/agents/difftest/'` and separately `-run TestDifferential`
- corpora: `cd server/internal/agents/monitor && rt --back server/internal/agents/monitor/testdata/a06_review.json.gz -- bash -lc 'PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a06_review.py'` (same with `gen_a06_flows.py` / `a06_flows.json.gz`)

## A07 — Mihomo panel, OxiDNS cache/rule update/rankings/publication stats

**Status: `verified`** (review 2026-11-16, commits ce75e46a, 1796ab71, 891ad4f4 + this PROGRESS commit; table under "Review") · prereqs: A06, C14 · Go: internal/agents/monitor (panel.go, cache.go, ruleupdate.go, rankings.go, publication_hooks.go, wire.go, pydigit_table.go), internal/api/agents/panel.go

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

Review (adversarial, 2026-11-16) — mihomo_panel.py, oxidns_cache.py, oxidns_rule_update.py, oxidns_ranking_history.py, oxidns_publication_stats.py checkpoint/complete and routes/agents.py 2199–2285 walked branch by branch against panel.go/cache.go/ruleupdate.go/rankings.go/publication_hooks.go/wire.go/api/agents/panel.go, plus the shared edits (`urlx.Quote`, `agents.HAField`, `rankingSectionRows`). New CPython 3.11.17 references (`monitor/testdata/gen_a07_review.py` → `a07_review.json`, asserted by `a07_review_test.go`). Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | `_newer_release`: `int()` of a release part over 4300 digits raises ValueError (not a MonitorError: 500 page). Python raises from `_usable` before any fetch (cached, not forced) or from the answer after the fetch was stored; Go compared big ints and answered 200 | 1796ab71 (`releaseInts`, errors through `usable`/`cachedLatest`/`CheckCoreUpdate`) | `TestReviewNewerReleaseDigitLimit`, `TestReviewCoreUpdateDigitLimitFollowsPython` (12 current/cached/force combinations: answer, fetch count, cache) |
| 2 | low | porter's difference "no `host_url` for the version fetch" — observable: `request_rule` treats a GitHub mirror under the panel's own origin `/api/` as internal (in-process, never through the download proxy), so the reported failure differs (e.g. `GitHub 返回 HTTP 404` vs `无法连接 GitHub`) | 1796ab71 (`monitor.WithHostURL`, route passes `c.HostURL()`, `rulefetch.Request.HostURL`) | `TestReviewCoreUpdateFetchCarriesTheHostURL` |
| 3 | low | `oxidns_cache.call`: `_agent_origin` runs inside the try block before the token is encoded on sending; Go checked latin-1 first, so a bad host with a non-latin-1 token answered the cache module's AGENT_UNREACHABLE message instead of the monitor's | 1796ab71 | `TestReviewCacheOriginBeforeTokenEncoding`, difftest "review cache * bad host" |
| 4 | low | porter's difference "malformed HA records answer false" — observable: `oxidns_ha_publish.active` raises AttributeError for a truthy non-dict record and TypeError for an unhashable status (POST answers 500); `agents.HAActive` answered false and the update reached the Agent | 1796ab71 (strict `haActive` in ruleupdate.go; the A07-only export `agents.HAActive` removed) | `TestReviewRuleUpdateHAActiveFollowsPython` (11 records), difftest "review rules HA *" |
| 5 | low | collector thread start failures (no repository, lock file not openable) leave Python's thread and go to stderr via threading.excepthook; Go logged them as `OxiDNS ranking collection round failed` on the log page | 1796ab71 (`threadException` → stderr) | `TestReviewRankingLoopStartFailureGoesToStderr` |

Coordinator decision 5 (environment proxies in `agents.HTTPClient`) was done in this review: ce75e46a, see A01.

Porter's listed differences, decided: `host_url` → finding 2; malformed HA records → finding 4; WAL/SHM file presence — accepted: CPython keeps each connection until the cyclic GC runs, so `-wal`/`-shm` exist or not by GC timing; Go closes at once (checkpoint, files removed). Stored data, schema and journal mode are identical (corpus + collector differential); no route or file reader depends on the side files. int64 counters — accepted (≤ 10,000 records × 65535 per row per slot, 90 days: far below 2^63, so no REAL overflow). SQLite driver error texts — accepted (log / 500 page only).

Checks performed (no finding): panel whitelist, resource minimum versions and feature texts, `_agent` (falsy record, service default, compare_versions errors), `_name` (code points, control characters, provider before name, `quote(safe='')`), `_logs_query` (isdigit incl. superscripts/Arabic, 18 code points), `_query` (`or` default URL, prefixes, 2048 code points, `int()` incl. U+3000/`+`/`_`/VT, 4301 digits, 500–30000), `_body` (method/reload, exact key sets, list values), order of agent → path → query → body → origin, timeouts (70/200/40/20), token KeyError/TypeError/latin-1 inside the try, MaxBody before body error before status, 401/403, `raw.strip()` ASCII set, json.loads(bytes) BOM/UTF-16/NaN/4300 digits, 404/<400 non-JSON, `success is False` + unhashable code, message `[:300]`, 504/400/502 mapping, slimming (`{**data}` key positions, history `[-1:]`, extra entries, Compatible providers, non-dict members); core update cache (`_usable` settings `!=`, failure TTL from fetch end, newer-core TTL, `blocking=force or not cached`, reuse when fetched after the request started, store on failure), `_check_failure` (last_error, Timeout/HTTPError/RequestException/ValueError order, size limit ValueError), `release_url` quoting; cache module (request/tag validation order, TypeError of set(), headers before try, 3xx/401 before body, 16 KiB bound, 404 UPGRADE_REQUIRED, flushed `_tags` fallback, `flushed != tags`), rule update (paths validation, paired `is True`, HA busy text, A18 hook default 500, 404/202/200, `_task` incl. bool/float counts and code fallback), routes (args, Content-Length, 1024/16 KiB reads, is_json, `== {}`, `set(body) == {'tags'}`, MonitorError-only handling, `flushed`, no-store); ranking store (schema text, WAL, 30 s busy timeout, implicit BEGIN incl. empty executemany, COMMIT/ROLLBACK, INSERT OR IGNORE rowcount, version bump before upsert, maintain hour gate, unknown Agents by slots only, day merge order and DAILY_TOP sort, cutoff deletes, total_changes, query SQL/cache/LRU/version race, empty answers), collector (`int(time.time())`, settle/slot arithmetic, starts range, budget, newest first per recorder, MonitorError/SlotIncomplete log and stop, pause after each slot, caught_up pop/set, `supported` AttributeError aborting the round, filters order, `_recorders` MonitorError skip, maintain with all ids), loop (flock LOCK_NB before the first 60 s sleep, round errors logged, config context reset), publication checkpoint/complete (AttributeError paths, pending/deployment checks, merged state key order, verify, CAS expected generation, `state != previous`, handled exception set, warning texts).

Accepted (jsonx-wide, as A03/A06): request/Agent JSON nested deeper than CPython's recursion limit (Python RecursionError → 400 in the cache/rule-update routes or 500 in the panel route; Go parses up to jsonx.MaxDepth).

Differential (891ad4f4): +15 review cases in `panel_diff_test.go` (bad host + non-latin-1 token, malformed HA records, `int()`/isdigit edge args, 512/513-character names, list bodies): **127 requests, 0 differences**; ranking collector 24 windows, 112 stored lines identical; Oxidns 69 requests, 0 differences; registry 234 requests, 0 differences.

Review commands (private detached worktree of HEAD; the shared tree held A08's uncommitted WIP in `api/agents`):
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... && go test -count=1 -race ./internal/agents/... ./internal/api/agents/... ./internal/server/...'` — all ok
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Panel ./internal/api/agents/difftest/'`, separately `-run Oxidns`, `-run RankingCollector`, `-run TestDifferential`, and `CONFIGFLOW_DIFF=1 go test -count=1 -v -run ProxyDifferential ./internal/agents/`
- references: `rt --back server/internal/agents/monitor/testdata/a07_review.json -- bash -lc 'cd server/internal/agents/monitor && PYTHONPATH=../../../.. ~/cf-test/.venv/bin/python testdata/gen_a07_review.py > testdata/a07_review.json'`; proxy: see the header of `internal/agents/testdata/gen_proxy_reference.py`

Open findings: none.

## A08 — installer script rendering, install command store

L1 D8: explicit httpx route marker preserves Python's plain-text 404/503 only
for agents.download_install_command; removes error attachment header as approved.
Binary downloads and other Python JSON errors retain JSON. Curl -fsSL/curl|sh,
generic API boundary, storage failure and Flask body/status/type checks pass;
see Step B #3 exception list and progress/l1-installer-fixes.md.

L1 D4: confirmed Python's existing optional registration-key policy; ordinary
Shell/Docker Agent has no registration_key input. Missing-key 403 now explains
the policy and HA enrollment alternative, without exposing the key or relaxing
authentication. Documented in doc/guide/agent-install.md; regression checks both
ordinary deployment methods and unchanged registry; existing HA locked-enrollment
and existing-token tests continue to apply.

L1 D3 follow-up (`go-fix-l1-installer`, ported; independent review pending): native
single/AIO installers journal old binary/config/identity, service definitions and
enabled/active state before replacing them. Failure stops/disables candidates,
restores files before restarting prior services, ignores cancellation during
rollback, and retains private backups with an explicit failure if compensation
cannot finish. Fingerprint `f08339646f7d4db4d1cfe14ad314942c` invalidates older
unsafe links. Fixed approved source deltas preserve strict A08 byte comparisons;
stateful shell regressions and real Debian/systemd + Alpine/OpenRC failure
injection are tracked in [L1 repair report](progress/l1-installer-fixes.md).

**Status: `verified`** (review 2026-11-16, commit 8cf0dab8 + this PROGRESS commit; see "Review" below; HR: VM lab test still due after the maintainer agrees) · HR · prereqs: A02 (`LATEST_AGENT_VERSION`), C02 (resource) · Go: internal/installer (new), internal/api/agents/installer.go

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
- Continuation (second porter, 2026-11-16): a55c5312 `testdata/reviewed_deltas.json` generated by `testdata/gen_reviewed_deltas.py` (imports the Python test modules; never hand-copied) + `deltas_test.go` (`withoutReviewedInstallerDeltas`, router-split `single_service_script_sha256` equal); `core_harness_test.go` = harnesses of test_single_agent_core_install.py (`newSingleHarness`) and test_aio_agent_core_install.py (`newAIOCoreHarness`, via new `newAIOHarnessSource`), fake core injected by `withFakeCore` (replaces the rendered core body, = monkeypatching generate_core_install_script), plus their non-route cases (single 11 functions, AIO 10; route/client cases stay A10). 033df151 `test_core_install_script.py` → core_install_test.go (all 49 functions, parallel subtests; `runOptions.setsid` = start_new_session).
  `test_gateway_core_install.py` → gateway_core_test.go (34 functions; 3 Python type-only params ruled out by Go types: `tun_prepare_assume_enabled=1`, `full_tun='yes'`, duplicate `full_tun=False`; CORE_RELEASES monkeypatch = swapping `CoreReleases`, so these tests are not parallel; 0ef9e89 baselines read from backend/testdata).
  `test_agent_install_dependencies.py` → install_dependencies_test.go (all 21 functions; `isolateDependencies` returns the new env; `installerHarness` reuses newSingleHarness/newAIOHarness/newAIOCoreHarness).
  `test_standalone_forwarding_install.py` → standalone_forwarding_test.go (16 generator/shell functions; `test_forwarding_option_must_be_a_boolean` is a Python type check ruled out by `Forwarding`) + internal/api/agents/installer_forwarding_test.go (5 API functions).
  `test_installer_forwarding_context.py` → forwarding_context_test.go (all 13; the `__file__` monkeypatch is swapping the embedded `installCoresTemplate`). `test_aio_network_prerequisites.py` → aio_network_test.go (all 5; `shellFunction` extracts from the embedded scripts). `test_shell_ha_prepare.py` → shell_ha_prepare_test.go (10) + internal/api/agents/installer_ha_test.go (route case). Installer parts of `test_router_split_regression.py`: `TestSingleServiceScriptsMatchThePreviousRelease` (+ reused by gateway/standalone baselines); its non-installer cases (gateway goal drafts, unmarked Mihomo, router setup) belong to the routers/config modules.
  Final run 2026-11-16: `go vet` clean; `go test ./internal/installer/` ok (115 s, incl. TestRenderMatrixMatchesPython, TestGeneratorFingerprintMatchesPython), `./internal/api/agents/` ok; differential `installer routes: 295 comparisons equal, 0 differ`.
  Fixes: none needed — every ported case passed against the Go renderers. Matrix (`TestRenderMatrixMatchesPython`) and Flask differential re-run after the ports (see commands).
  A10 handoff: harnesses of test_single_agent_core_install.py / test_aio_agent_core_install.py live in core_harness_test.go with their non-route cases; the remaining route cases (client fixture: `test_combined_api_reuses_selected_canonical_installer…`, `test_omitted_and_false_preserve_agent_only_api`, `test_api_rejects_unsupported_combined_settings…`, `test_combined_aio_api_embeds_canonical_core_phase…`, `test_invalid_combined_settings_rejected…`) stay with A10.
- Left: nothing for A08 (reviewer next). Deferred elsewhere: 4 Docker entrypoint cases of test_agent_transaction_install.py and 4 of test_oxidns_docker_install.py → finish #40; single/AIO core route cases → A10.
- Pitfalls: the shared tree may not build (another agent's uncommitted x/net/idna import broke internal/api/agents on 2026-11-16) — verify on `git worktree add --detach ~/program/configflow-go-a08 HEAD`. `rt -- … go test ./internal/installer/` takes ~30–50 s (Python stub processes in the AIO harness). Run `gofmt` after regenerating constants_gen.go. The Python reference run `rt -- … pytest backend/test_x.py` printed "no tests ran" from the repo root here; run it from the worktree root with `-p no:cacheprovider` and check the rootdir if it happens again.

Review (adversarial, 2026-11-16) — install_output.py, install_script.py, go_install_script.py, core_install_script.py, gateway_install_script.py, install_command_store.py, the templates and routes/agents.py 246–537 + 1270–1339 walked branch by branch against internal/installer and api/agents/installer.go. Findings, all fixed in 8cf0dab8:

| # | Sev | Finding | Regression test |
|---|---|---|---|
| 1 | low | `int()` of a port string (install_core / AIO / gateway ports, `type=int` query args) used Go's Unicode tables (Unicode 15 digits such as U+1E4F1 accepted, CPython 3.11 refuses) and the full repr instead of `%.200R` in the ValueError text. Now `pystr.Int` (decision 8; private `leadingDigitRun`/`httpx.PyParseInt` use removed) | `TestReviewIntOfPortFollowsCPython`, fuzz corpus |
| 2 | low | private urlsplit copy accepted a bracketed IPv6 scope id containing `%` (`[::ffff:1.2.3.4%%]`); ipaddress refuses it. Installer now uses `urlx.Split` (decision 8); `urlx` bracket errors now carry ip_address's text (`'x' does not appear to be an IPv4 or IPv6 address`; was the netip / "Invalid IPv6 address" text) | `TestReviewBracketedScopeIDFollowsIPAddress`, fuzz corpus (2500 URL-focused cases) |
| 3 | med | (httpx, found by the HTTP fuzz) `request.args`: Werkzeug 3 decodes with `errors="werkzeug.url_quote"`, so an invalid UTF-8 escape stays `%C0%AF`; Go produced U+FFFD → different bytes in rendered scripts (e.g. `agent_ip`, `server_url`, `health_*`) and in every other route reading query args | `httpx.TestQueryInvalidUTF8IsRequotedLikeWerkzeug` (Werkzeug 3.1.8 references), HTTP fuzz |
| 4 | low | (httpx, found by the HTTP fuzz) `request.host`/`url_root`: Werkzeug 3.1 `get_host` removes the default port and answers "" for a Host with invalid characters (e.g. `x_y.test`), `uri_to_iri` lower-cases the host and normalises the port; Go used the raw Host. Affects install-script's fallback server URL (no `server_url`, no X-Forwarded-*) and `HostURL` users | `httpx.TestHostAndURLRootFollowWerkzeug` (18 Host values) |
| 5 | low | `agent_type.lower()` used Go's `strings.ToLower` (+ İ special case); now `pystr.Lower`; `strip`/`rstrip` now `pystr` (were `diag` copies on Go tables) | fuzz corpus, HTTP fuzz |
| 6 | low | store: `Path.mkdir(mode=0o700, parents=True)` gives missing parents the default mode; Go created them 0700 | code review (modes of install-commands itself unchanged, differential checks 0700/0600) |
| 7 | low | gateway `local_agent_checksums`: an empty `AGENTS_STATIC_DIR` is `Path('')` = `.` (exists); Go used the default directory (the download route's `os.path.exists('')` is False, so `AgentsStaticDir` stays as is) | code review |

Checks performed: every generator branch, validation order and error text (agent name incl. `re.match` `$`-before-newline in Docker, ports bool/str/int/big, units, binaries, config paths incl. `//`, `/.`, `..`, Agent directory aliases, installation id/token, verified Agent path, forwarding/TUN/HA/core combinations, Docker HA network/subnet/netmask/hostmask/gateway/identity, gateway URLs/enrollment origin/checksums); quoting (`shlex.quote` ASCII set, `_shell_word`, docker run words, YAML via yamlx, JSON heredocs with quoted delimiters, single-pass placeholder substitution); `with_install_output`/`with_root_privileges` with every `splitlines` separator; store (TTL, cap 128 → 503 with existing links valid, expired-row delete, generator fingerprint/stale/NULL rows, pre-generator schema migration, token regex, file modes, BEGIN IMMEDIATE + busy timeout, no DB created on lookup); download/script headers and status codes; auth exemptions (public script/probe/core/`i/<token>`, login for install-command/core-install-command/docker-*).
- **Fuzz (function level):** `testdata/gen_fuzz.py` (seed 20261116) → `testdata/fuzz.jsonl.gz`, 20000 cases (2500 each: go_agent, aio, gateway, compose, run, with_install_output, with_root_privileges, URL-focused gateway) with shell metacharacters `'"$\`;|&`, newlines and all `splitlines` breaks, C0/DEL, Unicode (CJK, emoji, full-width, NFKC traps, Unicode 15 digits), values up to ~5000 chars, boundary/malformed ports (4300/4301 digits, underscores, signs), IPv4/IPv6 (zones, embedded IPv4, `::` forms), URLs with credentials, fragments, brackets, percent-encoding; replayed by `TestFuzzCorpusMatchesPython`: 0 differences (7793+ rendered, rest refused with identical text).
- **Fuzz (HTTP):** `difftest/installer_fuzz_test.go` `TestInstallerFuzzAgainstFlask`, seeded: 4538 Flask↔Go comparisons (install-script 1500 incl. raw/invalid escapes, repeated/empty keys, forwarded headers, Host variants; compose/run 700 each; install-command 1000 incl. every JSON type per field, 4095–4097 code points, NUL, + download of each link; core-install-command 150): 0 differ. Existing `TestInstallerDifferentialAgainstFlask`: 295 equal, 0 differ. Matrix (949 cases): 0 differences.
- **shellcheck** (koalaman/shellcheck:stable 0.11.0, `-f gcc`): 218 scripts (every 4th rendered shell case of the matrix + every 40th of the fuzz corpus), Python and Go renderings byte-identical and the finding sets identical (1506 lines each; SC2329 875, SC2086 134, SC2015 94, SC2031 68 …; parse errors SC1072/1073 only in random `with_install_output` bodies). Commands: `testdata/shellcheck_sample.py DIR`, `INSTALLER_SAMPLE_DIR=DIR go test -run TestWriteShellcheckSample`, then `sudo docker run --rm -v DIR:/w koalaman/shellcheck:stable -f gcc /w/{py,go}/*.sh`.

Accepted differences: (a) jsonx-wide (as A03/A06/A07): a lone surrogate escape in an install-command body is U+FFFD in Go (Python keeps the surrogate: `\ud800` in the config JSON, or a 500 "无法保存安装命令" when it reaches the shell-quoted URL), and JSON nested deeper than CPython's recursion limit; (b) url_root parts where Python raises (500): Host port above 65535, an invalid bracketed IPv6 Host, IDNA decoding of `xn--` labels (Go keeps the punycode) — reachable only without the Edge (it always sets X-Forwarded-Proto/Host, which install-script prefers) and without `server_url`; (c) the gateway probe script is embedded, so its 503 "file missing" branch cannot occur; (d) `GeneratorFingerprint` is a constant (porter's decision; `TestGeneratorFingerprintMatchesPython` guards drift).

**Decision 6 done** (A10 porter, 61f82de3, 2026-11-16): `ValidateAgentName` now has fullmatch semantics — `agent_name="agent\n"` is refused with the usual "Invalid agent name: …" text (Python accepts it and writes a line break into the Docker snippets). Regression `TestAgentNameTrailingNewlineIsRefused`; deliberate rows: matrix 2, fuzz 105, installer differential 2 (`compose/run name newline`, Python 200 / Go 400), A09 corpus 13. A08 stays verified.

VM lab test to run once the maintainer agrees (PVE lab, never production; Go server serving the links): Debian 12 systemd + Alpine 3.24 OpenRC guests; for each: `install-command` link for Mihomo single (forwarding default / opt-out), OxiDNS single with `install_core` and with `prepare_dns_ha`, MosDNS custom lifecycle (stop/start/status commands with quotes and `$`), AIO with and without `install_core`; run `curl …/api/agents/i/<token> | sh` as root and via sudo; verify the Agents register and heartbeat, deployment publish + rollback, forwarding persisted (`/etc/sysctl.d/99-configflow-*-forward.conf`) and kept on uninstall, re-run idempotency, uninstall; a stale link (fingerprint changed) prints the refusal and changes nothing; Docker run + compose (host, bridge, macvlan HA) on a Docker guest; the gateway installer as part of A22's lab.

Review commands (private detached worktree `~/program/configflow-go-a08r` of HEAD + changed files; the shared tree did not build because of another agent's WIP in internal/convert):
`rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/... ./internal/httpx/... ./internal/urlx/ && go test -count=1 ./internal/installer/ ./internal/api/agents/ ./internal/httpx/... ./internal/urlx/ && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/httpx/difftest/ && CONFIGFLOW_DIFF=1 go test -count=1 -v -run Installer ./internal/api/agents/difftest/'`; regenerate the corpus: `rt --back server/internal/installer/testdata/fuzz.jsonl.gz -- bash -lc 'set -o pipefail; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_fuzz.py 2500 | gzip -9n > server/internal/installer/testdata/fuzz.jsonl.gz'`.
Pitfall: a killed difftest run can leave its Flask child alive holding the rt lock (fd 9 → `~/rt/.locks/<project>`); `rt` then waits forever — find it with `ls -l /proc/<pid>/fd` and kill it.

Open findings: none.

Installer locale follow-up (2026-11-17; [report and commands](progress/installer-locale.md)):
the test harness now starts shell children with `LANG=C.UTF-8 LC_ALL=C.UTF-8`,
after both inherited and explicitly supplied environments. Generated scripts remain
byte-identical to Python (coordinator decision).
**Known runtime limitation:** Arch dash `0.5.13.4-1` (.32) and
`0.5.13.5-1` (.64) corrupt Chinese variable assignments when started in the C
locale (LANG/LC_* absent, or LC_ALL=C). This corrupts terminal reports **and
persistent report logs**, rather than only rendering glyphs incorrectly.
Setting locale inside the script does not repair .13.4; .13.5 can switch at runtime.
The tested generated Agent JSON uses ASCII escapes and stayed byte-identical,
including a Chinese config path; no config-corruption defect is established by
this probe, and it does not certify every installer path. Debian 12 officially
ships [dash 0.5.12-2](https://packages.debian.org/bookworm/amd64/dash);
the checksum-verified Debian binary passed the same report/config probe with
absent locale, C and C.UTF-8 on .32. Full Debian guest install under absent LANG
is still L1's independent check; if it proves configuration corruption, track
that as a separate defect.

## A09 — Docker HA guide, preflight, enrollment

L1 D6 follow-up: verification selects dig/nslookup/BusyBox nslookup, always
queries the validated VIP, preserves query failures; missing tools gives an
explicit dnsutils/bind-tools hint and exit127. No getent fallback to a different
resolver. Unit/corpus + Flask239/0 pass. Real clients each resolved an isolated
fixture VIP; actual minimal Debian12 gave the tool-install hint (.64,7.067s).

L1 D5 follow-up (ported, review pending): Host script probes UDP/TCP53 in daemon
namespace before starting a new/stopped installation, skips already-running owned
main container on retry; all new/migration scripts verify actual OxiDNS supervisor
RUNNING before success and refuse FATAL/EXITED/STOPPED/timeout. Pure shell cases,
old corpora and Flask239 comparisons pass. Real namespace-isolated socket and
Supervisor RUNNING/FATAL tests pass on .64 (21.221s); no host53 bindings.
Exact approved HA source deltas are recorded in installertest/l1-ha-deltas.json.

**Status: `verified`** (review 2026-11-16, commits 2b9a0c88, 5a17282e, f43a0525, a379e23a + this PROGRESS commit; see "Review" below; HR: VM lab test still due after the maintainer agrees) · HR · prereqs: A08, A01, C05 · Go: internal/installer (`docker_ha_guide.go`, `docker_ha_preflight.go`, `docker_ha_enrollment.go`), internal/api/agents/docker_ha.go (+1 line in `Register`)

Files:
- `backend/agents/docker_ha_guide.py` (475)
- `backend/agents/docker_ha_preflight.py` (376)
- `backend/agents/docker_ha_enrollment.py` (165)
- `backend/routes/docker_ha_installation.py` (67)

Tests: `test_docker_ha_guide.py` (19), `test_docker_ha_preflight.py` (32), `test_docker_ha_enrollment.py` (7)

Routes (2, both `done` in routes.tsv): `POST /api/agents/docker-oxidns-ha-guide` (auth), `POST /api/agents/docker-oxidns-ha-enroll/<session_id>` (public, Bearer capability; gate and token-redaction whitelist entries already existed).

Port log (A09 porter, 2026-11-16):
- Commits: bdddd04e (port + routes), 864ca8c1 (corpus; base64 fixes), 748f5fa6 (guide/preflight tests), 93f568d9 (route tests, routes.tsv), 651a62e2 (Flask differential), b274a618 (deliberate server_url fix).
- Go API: `GenerateGuide(request any, *Enrollment)`, `ParsePreflightReport(output, mode any)`, `CollectorScript()`, `IdentityCommand`, `InspectionCommand`, `IssueEnrollment(dataDir, body, profileID)`, `Enroll(ctx, dataDir, AgentUpdater, sessionID, token, present)`, `BootstrapCommand`; errors `*ValueError` (400), `*EnrollmentError{Status}`, `*TypeError` (= any other Python exception → 500 page). `guideCompose(params)` = `yaml.safe_load(generate_docker_agent_compose(**params))` with Python's handling of non-string JSON values (ints accepted by ipaddress for agent_ip/ha_gateway, TypeError for non-str agent_name/ha_parent, everything else an invalid address); A10 reuses it.
- **A10 hook:** `generateCompact`, `generateDirect`, `generateBundle` (package vars in docker_ha_guide.go, nil until A10) — until then the three Compose methods return `ErrComposeVariantNotPorted` (500). A10 assigns them, then the corpus rows counted "skipped (A10)" (241) and the 3 "pending A10" differential requests must match; remove those skips.
- Tests ported: test_docker_ha_guide.py → installer/docker_ha_guide_test.go (18 functions; shell scenarios with bash and the same Python fake docker; `bootstrapCommand` var = the monkeypatch); `test_live_environment_requires_admin_and_never_exposes_extra_payload` is the `oxidns-ha-environment` route → **deferred to A18**. test_docker_ha_preflight.py → installer/docker_ha_preflight_test.go (29 functions, collector run with bash + stub docker/ip/ss/systemd-detect-virt, /proc/net fixtures) + api/agents/docker_ha_test.go (3 API functions). test_docker_ha_enrollment.py → api/agents/docker_ha_test.go (all 7). Extra: `TestPreflightCollectorMatchesPython` (embedded collector == Python source), `TestServerURLTrailingNewlineIsRefused`.
- Reference corpus: `testdata/gen_docker_ha.py` → `testdata/docker_ha.jsonl.gz` (3279 cases: guide over read-only steps, new host/macvlan ± enrollment, migrate bind/volume, every field with boundary/malformed/non-string values, 400 seeded random combinations — guide answers stored as per-key digests; preflight over all test-file outputs plus realistic/damaged ones: CRLF, markdown backslashes, Unicode line breaks, BOM, truncation, duplicates, ss/proc variants, digit limits, 700 random; base64 padding cases). `TestDockerHACorpusMatchesPython`: 1809 answered + 1043 refused + 180 base64 equal, 6 deliberate, 241 skipped (A10), **0 differ**.
- Flask differential `TestDockerHADifferentialAgainstFlask` (difftest/docker_ha_diff_test.go): **200 comparisons equal, 0 differ, 3 pending (A10)** — body limits/malformed bodies, check/inspect/verify, generate refusals, migrate, new installs (session/token/owner replaced per side), profile header/query, enroll refusals/success/replay/changed identity/expiry/renewal/takeover/other profile, 16 hand-edited session records, 4 index-record edits; installation files (names, masked contents, modes; 61) and stored Agents equal.
- Commands: `rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/ && go test -count=1 ./internal/installer/ ./internal/api/agents/ ./internal/server/'` (2026-11-16: all ok, installer 127 s); differential `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run DockerHA ./internal/api/agents/difftest/'`; corpus `rt --back server/internal/installer/testdata/docker_ha.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_docker_ha.py | gzip -9n > server/internal/installer/testdata/docker_ha.jsonl.gz'`.
- Decisions: **deliberate fix of a Python validation bug** (maintainer, 2026-11-16): Python validates server_url with `re.match(r'^https?://[^\s]+$')`, whose `$` lets one trailing "\n" into the guide/compose; Go behaves like `re.fullmatch` and answers the usual `请填写完整的 ConfigFlow HTTP(S) 地址` (400). Corpus rows with such input are counted "deliberate" (6), the differential checks python 200 / go 400 explicitly, regression test `TestServerURLTrailingNewlineIsRefused`.
- Accepted / known differences (for the reviewer): a lone-surrogate escape in `output` (`"\ud800"`) — Python 400 `检查输出包含无效文本编码`, Go 200 unknown report (jsonx decodes U+FFFD; jsonx-wide limitation; the Go function treats invalid UTF-8 as that case); a hand-edited session file whose `token` is not a string → Go TypeError (Python would echo it). TypeError/KeyError texts are log-only.
- Python quirks reproduced: CPython 3.11.17 `b64decode(validate=True)` (leading padding refused, "Excess data after padding" also outside strict mode, pads at quad positions 0/1 ignored), posixpath.normpath/commonpath (`//a` kept), str.isdigit table (`pyIsDigitExtra`), `\d`/`\s` Unicode in the ip-address regex, int() digit limit in the Compose version (400 with its text), Path.exists stat-error rule, UnicodeDecodeError text of an unreadable index file (400).
- Shared-code requests: `internal/pystr` should export `IsDigit` (private table `pyIsDigitExtra` here, also copies in subs/convert/monitor) and a strict-UTF-8 decode-error text (`pyUTF8DecodeError` copied from repo/files.go). No edits to A08's files.
- Pitfall: the shared git index can hold another agent's staged files — commit with `git commit -- <paths>` (one of my commits swept them in and was redone as b274a618; their files were left staged).
- Left: review; A10 wiring of the three Compose generators; A18 case above; HR lab test with A08–A10.

Review (adversarial, 2026-11-16) — docker_ha_guide.py, docker_ha_preflight.py, docker_ha_enrollment.py and routes/docker_ha_installation.py walked branch by branch against docker_ha_{guide,preflight,enrollment}.go and api/agents/docker_ha.go (validation order and every Chinese text, network modes, int/bool/non-str values through ipaddress/re, paths/normpath/commonpath, quoting of every generated word, preflight parser incl. regex backtracking, base64, splitlines, Unicode \d/\s, digit limit, enrollment locking, renewal, compare_digest, every rejection path, the route's body limits and exception mapping). Findings (all fixed):

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | issue(): a session file holding JSON `null` is `session is None` in Python → a new session is created (index keeps the old id), 200; Go answered a TypeError (500) | f43a0525 | `TestReviewIssueNullSessionStartsANewSession`, review corpus |
| 2 | low | issue(): missing `agent_ip` is `body['agent_ip']` KeyError in Python; Go stored host None (unreachable through the guide, which validates agent_ip first) | f43a0525 | `TestReviewIssueWithoutAgentIPIsAKeyError` |
| 3 | low | issue(): hand-edited session `id` follows pathlib (absolute id replaces root, name `.json` → `.json.tmp`, no lexical `..` cleaning) and an embedded NUL is os.open's ValueError (400 "embedded null byte"); Go used filepath.Join and answered 500 | f43a0525 | `TestReviewSessionPathsFollowPathlib`, review corpus |
| 4 | info | enroll(): time.time() is read before `session['expires']` (only the order of clock reads; no visible effect) | f43a0525 | review corpus (clock replay) |

Shared-code requests done: `pystr.IsDigit` (str.isdigit table generated from CPython by `pystr/testdata/gen_tables.py`; `isdigit` references in ref.json) replaces A09's `pyIsDigitExtra`; `pystr.UTF8DecodeError` (moved from repo/files.go, 330 `decode` references in ref.json) used by repo and installer (5a17282e). **For their owners:** private str.isdigit copies remain in `subs/pyconv.go` (uses Go's Unicode 15 `unicode.IsDigit` and misses U+1369–1371, U+19DA, U+24EA…: real divergences), `convert/pyhelpers.go`, `agents/monitor/panel.go`; UTF-8 decode-error copies in `substore/pystr.go` and `pyyaml/scanner.go` — switch them to `pystr`.

Checks performed:
- **Record & replay** (PORTING rule 4): `testdata/a09_record.py` records every call of generate_guide / identity_command / inspection_command / parse_report / collector_script / bootstrap_command / issue / enroll during the whole Python suite (12552 passed) incl. DATA_DIR files before/after with modes, time.time(), os.urandom draws, datetime.now() and the Agent list handed to `_update_agents`; `TestDockerHARecordedCallsMatchPython` replays all 331 calls (fake clock/randomness/manager, rebuilt DATA_DIR): **0 differ** (6 skipped: test monkeypatched bootstrap_command — covered by the hand-ported shell scenarios; the 129 Compose-method guide calls now compare against A10's generators, 0 differ).
- **Review corpus** `testdata/gen_review_a09.py` → `docker_ha_review.jsonl.gz`, 5169 CPython references replayed by `TestDockerHAReviewCorpusMatchesPython`: 2800 guide calls (1840 answered) with shell metacharacters, quotes, backslashes, `$(…)`, heredoc markers, C0/U+2028/BOM/emoji in every free-text value (paths, server URL, image, mounts, names, enrollment URL/token), verify/inspect with random text; 2000 damaged preflight outputs; 207 issue + 162 enroll calls over hand-edited index/session files (every JSON type per field, invalid UTF-8, directories/files in place of each other) and Agent lists (non-dict entries, 8082 vs 8082.0 vs True, conflicts, manager errors): **0 differ** (2 deliberate trailing-"\n" server URLs, 12 accepted non-str tokens). `bash -n` of all 4433 generated scripts in the corpus: all parse.
- **Differential**: added Authorization spellings (lower-case scheme, double space, trailing tab/space — accepted by both as OWS —, padded 44-char and latin-1 tokens, trailing slash, longer session id, profile header on enroll): 209 equal, 0 differ before A10 wired its generators.
- Coverage map: test_docker_ha_guide.py 19 = 18 hand-ported + 1 deferred to A18 (now also listed there); test_docker_ha_preflight.py 32 = 29 + 3 API; test_docker_ha_enrollment.py 7 = 7; all three additionally replayed through the recording. Confirmed.

Accepted differences (with reason): (a) server_url with one trailing "\n" refused (coordinator decision 6); (b) jsonx-wide: lone surrogate escapes (U+FFFD; the `"\ud800"` check output is Python 400 / Go 200 unknown report), JSON nested beyond CPython's recursion limit inside a preflight field (Python treats it as unparsable); (c) a hand-edited, unexpired session whose `token` is not a string: Python echoes it into a broken install command (200), Go answers 500 — server-side corrupted data only, and the `Enrollment.Token` type is shared with A10.

Open findings: none for A09. **Note for A10/coordinator:** at a379e23a (A10's commit ec17cd14 wired the generators) the committed `TestDockerHADifferentialAgainstFlask` shows 3 DIFFs for the Compose methods because it compares bodies containing per-side random capabilities; Resolved by A10 in 61f82de3 (`composeBodies`/`compareComposeAnswer`): 239 equal, 0 differ, 0 pending.

Decision 6 for Agent names done by A10 (61f82de3): `ValidateAgentName` refuses one trailing "\n"; A09's corpus counts 13 rows "deliberate (agent name trailing newline)".

VM lab test (PVE lab, never production; with A08/A10): Docker guests (Debian 12 + Alpine 3.24, rootful Docker, Compose ≥ 2.30): run the preflight collector and paste the output (pass/warning paths, a busy port 53, macvlan); new install script host mode and macvlan (with gateway / without), with enrollment: Agent registers once, heartbeats, re-run of the same command resumes without a second Agent, a changed command is refused, an expired capability is renewed by regenerating; identity command with an existing file refuses; migration of a single OxiDNS container (bind + named volume) and the rollback script; verify command against the VIP; enrollment with registration locked.

Review commands (private detached worktree of HEAD; the shared tree held A10's WIP): `rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/ && go test -count=1 ./internal/installer/ ./internal/api/agents/ ./internal/pystr/... ./internal/repo/... && CONFIGFLOW_DIFF=1 go test -count=1 -v -run DockerHA ./internal/api/agents/difftest/'`; re-record: `rt -- bash -lc 'rm -rf /tmp/a09rec && A09_RECORD_DIR=/tmp/a09rec PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python -m pytest -p a09_record backend -q -n auto --dist=worksteal -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/a09_record.py merge /tmp/a09rec'` then `rt --no-sync --back server/internal/installer/testdata/docker_ha_recorded.jsonl.gz -- true`; review corpus: `rt --back server/internal/installer/testdata/docker_ha_review.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_review_a09.py 2000'`. Pitfall: a failed difftest run left its Flask child holding the rt lock (kill it, see A08).

## A10 — Docker HA compose variants, installer routes

**Status: `verified`** (review 2026-11-16, commits 0f8231b4, 42d9c27e + this PROGRESS commit; see "Review" below) · HR (VM lab test with A08/A09 after the maintainer agrees) · prereqs: A08, A09 · Go: internal/installer (`docker_ha_compact.go`, `docker_ha_compose.go`, `docker_ha_compose_direct.go`, `docker_ha_compose_text.go` (generated), `docker_ha_compose_zip.go`, `docker_ha_compose_deflate.go`), internal/api/agents (`downloads.go`, `downloads_range.go`, `downloads_date.go` (review), +1 line in `Register`)

Files:
- `backend/agents/docker_ha_compact.py` (182)
- `backend/agents/docker_ha_compose.py` (346)
- `backend/agents/docker_ha_compose_direct.py` (96)
- `backend/routes/agents.py` (394) — lines 246–537, 921–952, 1270–1339 (all routes except `download/<filename>` were ported and verified with A08)

Tests: `test_docker_ha_compact.py` (12), `test_docker_ha_compose.py` (13), `test_docker_ha_compose_direct.py` (13), `test_agent_install_commands.py` (18), `test_core_install_api.py` (4), `test_aio_agent_core_install.py` (13), `test_single_agent_core_install.py` (14), `test_agent_native_aio_api.py` (4), `test_gateway_network_probe.py` (9)

Routes (9, all `done` in routes.tsv): the 8 installer routes of A08 plus `GET /api/agents/download/<filename>` (public; gate entry already existed).

Port log (A10 porter, 2026-11-16). Commits: ec17cd14 (generators, zip, record & replay, corpus), 61f82de3 (download route, Compose differential, A09 skips removed, decision 6), 09900f40 (bundle/probe shell scenarios, routes.tsv), f2689329 (store clock/fault cases).
- Go API: `GenerateCompact`, `GenerateDirect`, `GenerateBundle(params, request, *Enrollment)`; an `init` assigns A09's `generateCompact/generateDirect/generateBundle`, so `ErrComposeVariantNotPorted` can no longer occur (left in A09's file, dead). Constants `ComposeLabel`, `ComposeRuntime`, `ComposeIdentityVolume`.
- **Bundle zip is byte-identical**: `docker_ha_compose_deflate.go` ports zlib 1.3 raw deflate (level 6, memLevel 8: deflate_slow, longest_match, fill_window incl. high-water zeroing and window slide, trees.c), `docker_ha_compose_zip.go` CPython 3.11 zipfile's local headers / central directory / end record. Go's compress/flate would give a valid but different `content_base64`. Verified against zlib 1.3.2 (CPython 3.11.17 on the test server): 142 deflate inputs 0–200 KB (empty, 258/259 runs, block boundaries 16383/16384, window slide 65274+, random/binary/repetitive) + every bundle in replay and corpus.
- `docker_ha_compose_text.go` is generated by `testdata/gen_compose_text.py` (ast extraction of BOOTSTRAP, the five raw-string blocks of `_prepare`, the README parts); `TestComposeBootstrapMatchesPython` fails on drift.
- Compact: yamlx `SharedString{Anchor: "agent_image"}` = ImageName, `FlowMap`/`FlowList`, width 1000; `_annotate_compose` via `pyyaml.Compose` marks + `pySplitLines` + `pystr.IsSpace` lstrip.
- Download route: `os.getenv('AGENTS_STATIC_DIR', default)` + `os.path.exists` (empty value falls back to backend/static/agents, unlike `upgrades.ArtifactDirectory`'s `Path('')`), relative directories joined with the app root like `flask.send_file`, OSErrors (directory, EACCES) → 500 `{"message": "[Errno N] …: '<path>'"}`; `downloads_range.go` reproduces Werkzeug 3.1's `RequestedRangeNotSatisfiable` decision (parse_range_header, range_for_length, If-Range branch of is_resource_modified, empty file) because the view's `except Exception` turns it into 500 `416 Requested Range Not Satisfiable: …`.

Verification (all on the test server, CPython 3.11.17):
- **Record & replay**: `testdata/a10_record.py` (pytest plugin) during the whole backend suite (12552 passed) → `testdata/docker_ha_compose_recorded.jsonl.gz`: 129 distinct calls from 108 tests in 3 files (0 unsupported); `TestComposeRecordedCallsMatchPython`: 115 answered + 14 refused, **0 differ** (texts, info dicts, zip bytes).
- **Random corpus**: `testdata/gen_docker_ha_compose.py` (seed 20261116) → `docker_ha_compose.jsonl.gz`, 5218 cases: 576 fixed (host/macvlan × identity × 12 data dirs incl. `$`, quotes, backticks, `:`, CJK, U+2028, U+0085, U+001C, tab, 900 chars × gateway × enrollment/None × 3 functions) + 1500 seeded random requests × 3 functions (70 % mostly-valid params with one broken field, 30 % fully random: bad modes/names/ports/IPs/types, identity paths aliasing every managed part, malformed enrollments) + 142 deflate inputs. `TestComposeCorpusMatchesPython`: 3387 answered, 1606 refused, 83 deliberate (decision 6), 142 deflate, **0 differ**.
- **Flask↔Go differential**: Docker HA routes **239 equal, 0 differ, 0 pending** (A09's 3 pending/3 DIFFs resolved: each side's Compose answer is compared with `installer.GenerateGuide` given that side's capability — Python's answer byte-for-byte against the Go generators; 15 Compose bodies × 2 requests; installation records of the Compose methods are no longer filtered); installer routes 293 equal, 0 differ (+2 deliberate, decision 6); installer fuzz 4538 equal; download route (`downloads_diff_test.go`, 3 server pairs: present / missing / empty `AGENTS_STATIC_DIR`) **66 equal, 0 differ, 10 known httpx differences** (see Findings).
- A09's corpus after wiring: 1957 answered, 1123 refused, 180 base64, 6 + 13 deliberate, **0 skipped, 0 differ** (was 241 skipped).

Python test file → coverage:
| File | Coverage |
|---|---|
| test_docker_ha_compose.py | generator halves (topology/permissions ×2, script default, identity aliasing ×6) → replay; route halves (method validation before issue, generation with registration locked) → Docker HA differential (+A09 enrollment tests); bootstrap.py runs (first enrollment/offline restart, expired first attempt, foreign/corrupt state ×7, no redirect ×2) and prepare.sh runs with the fake docker (initialise ×2, reuse owned project ×2, refusals ×8, missing identity, root-level identity) → hand-ported `docker_ha_compose_shell_test.go` (Go-rendered bundle, same FAKE_DOCKER, loopback origin) |
| test_docker_ha_compact.py | generator assertions (isolation ×2, literal values, different scopes, colon dirs) → replay; route cases (image upgrade keeps project/scope, invalid requests ×5) → Docker HA differential + replay of the refused calls; runtime cases of `docker/docker-compose-compact.py` (registration retry, concurrency, data refusals ×8, restart refusals ×12, redirects ×2, interrupted init ×5) → **finish #40** (image runtime; its input, the generated environment, is covered by replay) |
| test_docker_ha_compose_direct.py | generator assertions (4 topologies, dollars/legacy) → replay; route cases (invalid ×7, locked registration) → differential + replay; runtime cases of `docker/docker-compose-install.py` → **finish #40** |
| test_agent_install_commands.py | creation auth, snapshot = preview, core selection, capacity/recovery, invalid parameters, size bounds, exact public endpoint, stale/NULL generator → A08 differential (295 + 4538 fuzz); fingerprint → `TestGeneratorFingerprintMatchesPython`; HA bool → installer_ha_test.go; legacy shell agent → A08 differential; clock/store cases (24 h expiry + prune, scoped earlier expiry, invalid lifetimes 0/−1/86401 — `True`/`'later'` ruled out by int64, new store instance + modes, unsafe tokens) → hand-ported install_command_store_a10_test.go; storage failures → hand-ported installer_store_a10_test.go (non-database file = sqlite3.DatabaseError, checked against Python) |
| test_core_install_api.py | public/shell-valid/no secrets/auth exactness → A08 differential (bytes equal to Python's, whose test runs `sh -n`); exception without server paths → not applicable (embedded template cannot fail, A08 accepted difference (c)) |
| test_agent_native_aio_api.py | all 4 → A08 differential (install-script/install-command with AIO and single options, refusals) |
| test_aio_agent_core_install.py, test_single_agent_core_install.py | non-route cases → A08 core_harness_test.go; the 5 Flask-client route cases → A08 differential (install-command with install_core combinations and refusals) |
| test_gateway_network_probe.py | 6 shell functions (43 cases) + syntax → hand-ported gateway_probe_test.go (embedded script, stub `ip`; `_spec` (A19) replaced by the exact JSON); 2 download functions → A08 differential (+ embedded script cannot be missing, A08 (c)) |

Decisions: zlib-exact deflate instead of compress/flate (strict parity of `content_base64`); decision 6 applied to `ValidateAgentName` (below).

Decision 6 (coordinator, done here for A08): `ValidateAgentName` uses fullmatch semantics; `deliberate_agent_name_test.go` (`TestAgentNameTrailingNewlineIsRefused`, helper `deliberateAgentNameFix`) — rows with exactly one trailing "\n" in `agent_name`/`existing_agent_name` refused by Go with the usual text are counted as deliberate: matrix 2, fuzz 105, A09 corpus 13, A10 corpus 83, installer differential 2 (`compose/run name newline`: Python 200, Go 400).

Findings for other modules (not fixed here; shared code):
- **httpx send_file deviations from Werkzeug 3.1** (found by the download differential; affect every `httpx.File` route): (1) a 304 drops `Content-Disposition` and keeps `Last-Modified` (Werkzeug removes entity headers incl. Last-Modified/Content-Type and keeps Content-Disposition); (2) the Range request is processed before the 304 check in Werkzeug (Range + matching If-None-Match → 206; httpx 304); (3) If-Range `W/"tag"` matches in Werkzeug (unquoted comparison), not in httpx; (4) a failed If-Match → 412 in Werkzeug, ignored by httpx; (5) spaces in `Range: bytes = 1 - 2` accepted by Werkzeug; (6) Range on an empty file ignored by Werkzeug (httpx 416); multi-range / malformed Range → Werkzeug 416, httpx 200 (masked in this route by the 500 conversion); (7) ETag mtime float: Werkzeug uses CPython's `st_mtime` (sec + nsec·1e-9 as a double), httpx a differently rounded float (`…6141193` vs `…614119`). The differential lists them as "known httpx differences".
- Left on the test server: an orphaned Flask child (pid 2494094, `~/rt/program__configflow-go-a10`, from a difftest run killed by `go test`'s 10-min timeout) — the sandbox refused `kill`; it only holds that worktree's rt lock (the worktree is deleted). Kill it manually.

Commands:
- tests: `rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/... ./internal/server/ && go test -count=1 ./internal/installer/ ./internal/api/agents/ ./internal/server/'` (installer ~150 s)
- differentials: `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -timeout 800s -v -run "DockerHA|Installer|Download" ./internal/api/agents/difftest/'` (~25 s)
- re-record: `rt --back server/internal/installer/testdata/docker_ha_compose_recorded.jsonl.gz -- bash -lc 'rm -rf /tmp/a10rec; A10_RECORD_DIR=/tmp/a10rec PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python -m pytest backend -q -n 6 --dist=worksteal -p a10_record -p no:cacheprovider; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/a10_record.py merge /tmp/a10rec'` (~4 min)
- corpus: `rt --back server/internal/installer/testdata/docker_ha_compose.jsonl.gz -- bash -lc 'set -o pipefail; PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_docker_ha_compose.py | gzip -9n > server/internal/installer/testdata/docker_ha_compose.jsonl.gz'`
- text constants: `python server/internal/installer/testdata/gen_compose_text.py && gofmt -w server/internal/installer/docker_ha_compose_text.go`

Pitfalls: the shared tree did not build (another agent's WIP in internal/rulefetch) — verified in private detached worktrees of HEAD. Name clashes with existing test helpers in internal/installer (`readJSONLines`, `firstDifference`, `readJSONFile`, `snapshotFiles`, `pySplitLines`): prefix new helpers. Run difftests with `-timeout` and output to a file; a `go test` timeout leaves the Flask child alive holding the rt lock. `httpxtest` routes use `repo.DataDir()` = `$DATA_DIR`: `t.Setenv` it.

Left (porter's view, superseded by the review below): HR VM lab test with A08/A09 (Compose bundle prepare + up, direct, compact on Docker guests); httpx findings above to the httpx owner; finish #40 runtime cases.

Review (adversarial, 2026-11-16) — docker_ha_compact.py, docker_ha_compose.py (incl. BOOTSTRAP, `_prepare`, README), docker_ha_compose_direct.py and routes/agents.py `download_agent_binary` walked branch by branch against docker_ha_compact.go / docker_ha_compose.go / docker_ha_compose_direct.go / docker_ha_compose_text.go / docker_ha_compose_zip.go / docker_ha_compose_deflate.go and downloads*.go (validation order, system-root/Synology checks, identity aliasing, owner/project hashes (`sort_keys`, separators, ensure_ascii), key order after `pop`/assignment, `_volume` shapes, anchors (`agent_image`, shared objects after safe_load), `$`→`$$`, annotation marks/splitlines/lstrip, every `q()`-quoted word of prepare.sh, env `split('=', 1)` → dict, zip header fields, Werkzeug send_file/make_conditional). Findings:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | download route, Range bounds: Werkzeug parses them as Python ints; Go used int64, so `bytes=0-99999999999999999999` / `bytes=0-9223372036854775807` (Python: satisfiable → 206) answered the view's 500 `416 Requested Range Not Satisfiable…` | 0f8231b4 (`math/big` bounds) | `TestReviewConditionalFollowsWerkzeug`, live `TestReviewConditionalMatchesWerkzeug`, differential `review range begin/end beyond int64` |
| 2 | low | download route, header text: WSGI decodes Latin-1 and Werkzeug uses `str.strip` / re `\s`, so U+00A0, U+0085, U+001C–U+001F around Range items and in ETag lists are white space; Go used `strings.TrimSpace` / ASCII `\s` on raw bytes (e.g. `bytes=0-9\xa0` → Go 500, Python 206) | 0f8231b4 (`latin1` + `pystr.Strip`, Latin-1 `\s` class) | same tests (`latin-1 nbsp/NEL`, `etag list with latin-1 space`) |
| 3 | low | download route, dates: Werkzeug's `parse_date` is `email.utils.parsedate_to_datetime` (no weekday, RFC 850, asctime, numeric/named zones, `.` clock separators, 2-digit years, −0000); Go used `http.ParseTime`, so e.g. `If-Range: 01 Nov 2026 12:30:15 +0000` was treated as an ETag (unsatisfiable Range: Python 500, Go 200). And parse_date lets **OverflowError** through (absurd year/day/hour or zone in If-Range / If-Modified-Since: `signed integer is greater than maximum`, `Python int too large to convert to C long/int`, `days=…; must have magnitude <= 999999999`), which the view answers with 500 `{"message": str(e)}`; Go served the file | 0f8231b4 (`downloads_date.go`: port of `_parsedate_tz` + `parsedate_to_datetime` + datetime/timedelta argument checks in CPython's order; `werkzeugConditionalError` follows make_conditional: range step only for size > 0, If-Range present ⇒ is_resource_modified(ignore_if_range=False), 304 step only when not 206) | `TestReviewParseDateFollowsEmailUtils` (17 references), `TestReviewConditionalFollowsWerkzeug` (11), live fuzz, differential `review if-modified-since … overflow`, `review if-range overflow`, `review if-range date without weekday` |

No defects found in the generators, the zip/deflate port or the text extraction.

Checks performed (all references from CPython 3.11.17 / Werkzeug 3.1.8 on the test server):
- **Deflate/zip (live differential)** `TestReviewDeflateAndZipMatchPython` + `testdata/review_a10_deflate.py`: Go generates the inputs, real `zlib.compressobj(-1, DEFLATED, -15)` and real `zipfile` digest them. Default run 233 inputs / 42 MB + 60 archives; review run `A10_DEFLATE_FUZZ=4000`: **4113 inputs, 820 MB, 0 differ**; inputs: empty, 1–4 bytes, every boundary around 258/259 (MAX_MATCH), 16383/16384 (sym buffer), 32506/32507 (MAX_DIST), 32768/32769, 65274/65275 (window slide), 64 KB–1.2 MB, runs of 0x00/'x' up to 1 MB (blocks whose start slid out of the window), 1 MB incompressible (stored blocks), periods 1…40000 around 4096/32768, length-3 matches at distances 4093–4098 (TOO_FAR), compressible prefix + random suffix and the reverse, prepare.sh/BOOTSTRAP text with 10–5000-char data directories of CJK/`$`/backticks/quotes, seeded literal/copy/run/CJK compositions incl. copies at the window edge. Archives: the bundle's five names, UTF-8 names (flag 0x800), duplicate names, a name ending in `/`, every permission pattern 0–0o7777, empty and 1 MB members: **60 equal**. Unreachable in the bundle but checked: names/date_time are fixed in generate_bundle (`ZipInfo(name)` = 1980-01-01, ASCII names), no ZIP64 (bundle < 2 MB at the 16 KB body limit).
- **zlib versions:** the test server has only zlib 1.3.2 (system and every uv CPython); building 1.2.13 / 1.3.1 from source (the zlib1g of `python:3.11-slim` bookworm/trixie) was not permitted in this session. `A10_ZLIB_LIBS="1.3.1=/path/libz.so:…"` makes the same test compare those builds (ctypes) — see lab test.
- **Review corpus** `testdata/gen_review_a10.py` → `docker_ha_compose_review.jsonl.gz` (format of a09_record.py; zip stored as `content_sha256`, `digestRecordedBundle` in the replay): **3000 generate_guide calls** for the three Compose methods (the porter's corpus calls the generators directly; A09's review corpus had no Compose methods): valid-biased with 1–3 wild fields + 25 % one broken field; data dirs/identity paths with shell metacharacters, quotes, `$(…)`, heredoc words, `{{…}}`, YAML indicators (`- `, `: `, `#`, `&`, `*`, `!!str`), U+0085/U+2028/U+2029/C0/C1/BOM/emoji, colons for compact, 15 % long paths 79–3000 chars with spaces (YAML folding at width 80/1000; 213 answered), identity paths aliasing every managed part/prefix; YAML-ambiguous images (`true`, `null`, `1e3`, `0x1A`, `12:30`, `.inf`, 255/256 chars); upper-case/128-char names (compact project `lower()[:32]`); boundary ports; macvlan parent/subnet/gateway variants; malformed capabilities. `TestComposeReviewCorpusMatchesPython`: **1331 answered + 1666 refused, 3 deliberate (decision 6 agent name), 0 differ**. `bash -n` of all 439 Python-generated prepare.sh in the corpus: 0 failed; every compose.yaml loads and every zip passes `testzip`.
- **Shell scenario** `TestReviewBundlePrepareKeepsMetacharactersLiteral`: the Go-rendered prepare.sh (bash, fake docker, temp dirs) with `$(touch …)`, backticks, quotes, `;|&<>*?!#~\`, U+2028/U+0085, `${HOME}` in data dir and identity path (bundle outside and inside the data dir, two rounds): dirs created at the literal paths, identity created once, no substitution executed.
- **Download route**: Python vs Go walked (env default/empty/missing directory, `os.path.exists` semantics, `os.path.join`, send_file's root_path join for relative paths, stat/open OSErrors `[Errno N] …: '<path>'`, directory → EISDIR, EACCES, symlink/dangling symlink, 416→500). Differential additions: `..`, `.`, `%2e%2e`, `%2e%2e%2f…`, `../…`, `%252F`, `%5C`, percent-encoded allowed name (`%63onfigflow-agent-linux-amd%364` → served by both), `%2F/etc/passwd`, empty segment, query, `;`, Unicode, invalid UTF-8, trailing space, 4000-char name, no segment, OPTIONS, plus the conditional cases above: **120 equal, 0 differ**, 12–16 known httpx differences (count varies with the ETag float rounding of each run's mtime). Live fuzz `TestReviewConditionalMatchesWerkzeug` (`A10_CONDITIONAL_FUZZ=40000`): **40000 parse_date values + 40000 make_conditional requests (12822 raise in Werkzeug), 0 differ** (random RFC 822/850/asctime dates with Latin-1 white space, 2-digit/huge/negative/underscored/superscript numbers, every zone form; Range with Latin-1 spaces, units, `=`, separators, suffix/open/closed/huge bounds; If-Range dates/ETags; If-Modified-Since; If-None-Match; If-Match; GET/HEAD/POST; sizes 0/1/70000).
- Existing suites re-run: record & replay 129 calls 0 differ, porter corpus 5218 0 differ, A09 corpora 0 differ.
- **Coverage map** (PORTING rule 4) checked against the 9 Python test files: test_docker_ha_compose.py 13 = 3 generator/route functions → replay + differential, 10 shell functions → docker_ha_compose_shell_test.go (spot-checked fault list/assertions against Python: identical); test_docker_ha_compact.py 12 / test_docker_ha_compose_direct.py 13 = generator + route functions → replay/differential, image-runtime functions (`docker/docker-compose-compact.py`, `docker/docker-compose-install.py`) → finish #40 (there is no PROGRESS-finish yet: **the finish planner must pick these up** — 6 compact + 9 direct runtime functions); installer test files → A08 differential/hand-ports as listed. Confirmed.

Accepted differences (with reason): (a) httpx send_file deviations (PROGRESS-config item 15, owned by C06; the review added three more there: huge Range bounds, Latin-1 white space in Range, email.utils date formats — httpx serves 200 where Werkzeug serves 206); (b) a non-UTF-8 `AGENTS_STATIC_DIR` (surrogate-escaped in Python): Werkzeug's ETag `adler32(path.encode())` raises UnicodeEncodeError → 500, Go serves the file — deployment-time environment value, the image fixes it to an ASCII path; (c) relative `AGENTS_STATIC_DIR`: the 500 text names the path joined with the resource locator's `backend` directory (`/app/backend` in the Python image; the Go image's locator root) — same rule as every resource lookup of the port; (d) as A08/A09: decision 6, jsonx lone surrogates.

Open findings: none.

VM lab test (with A08/A09; PVE lab, never production; Go server serving the guide): on Debian 12 and Alpine 3.24 Docker guests (rootful Docker, Compose 2.20 and ≥ 2.30): (1) Compose bundle, host and macvlan (with/without gateway), with `create_host_identity`: unzip outside and inside the data dir, `sudo bash prepare.sh` twice, `docker compose up -d`, first enrollment, restart offline (capability expired), Agent heartbeats once; a second bundle for the same data dir refused; data dir with a space and `$` in its name; (2) compose_direct with identity file and with the Docker identity volume; (3) compose_compact host/macvlan and a data dir containing `:`; (4) `content_base64` of the same request generated by the Python image (`python:3.11-slim`, its zlib) and by the Go server must be identical (closes the zlib-version check above); (5) Agent binary download through the Edge: full, Range, If-None-Match.

Review commands (shared worktree builds): `rt -- bash -lc 'cd server && go vet ./internal/installer/ ./internal/api/agents/... && go test -count=1 ./internal/installer/ ./internal/api/agents/ && CONFIGFLOW_DIFF=1 go test -count=1 -timeout 800s -v -run "DockerHA|Installer|Download" ./internal/api/agents/difftest/'` → `ok configflow/internal/installer 145.161s`, `ok configflow/internal/api/agents 1.995s`, difftest `docker HA routes: 239 comparisons equal, 0 differ`, `download route: 120 comparisons equal, 0 differ, 12 known httpx differences`, `installer fuzz: 4538 comparisons equal, 0 differ`, `installer routes: 293 comparisons equal, 0 differ`, `ok …/difftest 20.587s`. Big runs: `rt -- bash -lc 'cd server && A10_DEFLATE_FUZZ=4000 go test -count=1 -timeout 900s -run TestReviewDeflateAndZipMatchPython -v ./internal/installer/'`; `rt -- bash -lc 'cd server && A10_CONDITIONAL_FUZZ=40000 go test -count=1 -run TestReviewConditionalMatchesWerkzeug -v ./internal/api/agents/'`. Regenerate the review corpus: `rt --back server/internal/installer/testdata/docker_ha_compose_review.jsonl.gz -- bash -lc 'PYTHONPATH=.:server/internal/installer/testdata ~/cf-test/.venv/bin/python server/internal/installer/testdata/gen_review_a10.py 3000'`.

L1 D1 follow-up (2026-11-17): compact now rejects colon-bearing data directories
before rendering/issuing capability, matching other installation methods; removed
the ineffective long-bind special case. Four recording files explicitly classify
the named Python difference; HTTP refusal/no-write regression, live Flask
differential, and real Compose 5.6.1 mount tests pass on .32. See
[L1 fixes](progress/l1-installer-fixes.md) and the behaviour ledger.

## A11 — deployment bundle, locations, Mihomo resources, generated preview

**Status: `verified`** · HR · Codex port; fresh-Opus HR review 2026-11-17 (**[review-a11.md](review-a11.md)**): 9 findings fixed (0 high, 2 medium: JSON/quoted-key rule sources emptied by a private payload detector, credentials continuing on an excerpted line shown; 7 low), **0 open**. Branch `go-opus-review-a11` (author branch `go-port-a11`), rebased onto `go` `62427c41`. No push, subagent or production access. Scope is design A11-1–4; A12/A13/A14 orchestration/routes remain their owners.

HR review (2026-11-17): fixes `e47a4baf` (F1–F8) and `cadcdbc3` (F9 `RuleDownloadError` unwraps to its `*PreparationError` — A14 classification); Python oracle `testdata/gen_a11_review.py` → `review.json` (24 conversions, 19 excerpts, 10 Geo, 4 fingerprints, 9 Build cases), `review_test.go`, fuzz targets `FuzzConvertMosDNSRules`, `FuzzSourceExcerpt`, `FuzzOpenArchive`. Codex-port quality: 0 high → the "≥ 2 high" fallback is not triggered. Requirements for A12/A14/A16 and the added L2 item are in review-a11.md. Final `.32` acceptance after the review (exit 0 throughout):
```sh
# one nohup'd shell on .32 in the rt copy (cd server):
gofmt -l internal/ tools/; go vet ./...
go test -race -count=1 ./internal/deploy ./internal/agents ./internal/agents/diag
CONFIGFLOW_DIFF=1 go test -race -count=1 ./internal/api/subscriptions/difftest
go test -p=4 -count=1 ./...
CONFIGFLOW_DIFF=1 go test -p=4 -count=1 $(go list ./... | grep difftest)
for f in FuzzConvertMosDNSRules FuzzSourceExcerpt FuzzOpenArchive; do go test -run '^$' -fuzz "^$f\$" -fuzztime 60s ./internal/deploy/; done
(cd ../backend/agents/go-agent && CONFIGFLOW_SERVER_ARCHIVES=$PWD/../../../server/internal/deploy/testdata/archives go test -count=1 -run ServerArchive ./routes/)
(cd .. && ~/cf-test/.venv/bin/python -m unittest server/tools/proddiff/test_proddiff.py && server/scripts/proddiff.sh)
```
```text
gofmt-output:[]   vet-exit-0
ok configflow/internal/deploy 8.392s (race)   ok configflow/internal/agents 28.497s (race)   race-exit 0
ok configflow/internal/api/subscriptions/difftest 102.104s (race, CONFIGFLOW_DIFF=1)
full-exit 0 (go test -p=4 -count=1 ./...)
difftest-exit 0: api/agents 120.731s, api/subscriptions 97.836s, discovery 2.165s, health 13.759s, httpx 1.145s,
  mosdns 1.505s, refresh 3.627s, subs 1.857s, substore 6.246s, webdav 20.407s
fuzz 60 s: FuzzConvertMosDNSRules 319878 execs PASS; FuzzSourceExcerpt 263886 PASS; FuzzOpenArchive 420480 PASS
ok agent/routes 1.074s (ServerArchive)   proddiff unit: Ran 32 tests OK
proddiff.sh exit 0: self_check PASS, coverage_regressions 0, unmapped_routes 0,
  agent-deployment-bundle compared=3 success=2 partial=1 (C31 frozen aggregation provider), baseline: new=0 stale=0
```
The run above was on `go` `a44b343b`; after the rebase onto `62427c41` (only `internal/routers` and docs changed)
`.32` repeated gofmt (empty), `go vet ./...` (ok), race deploy 9.753s / agents 38.067s, `go test -p=4 -count=1 ./...`
(exit 0), all ten CONFIGFLOW_DIFF packages (exit 0) and proddiff.sh (exit 0; self_check PASS, coverage_regressions 0,
unmapped_routes 0, agent-deployment-bundle compared=3 success=2 partial=1, new=0 stale=0).

Port hand-back (author) follows.

Design: `server/design/a14-publish.md`. Complete design map, **one row per HR issue class for every step**, Python test-file mapping and L2-only list: **[A11-HANDOFF.md](A11-HANDOFF.md)**.

| Step | Main commit | Completed |
|---|---|---|
| A11-1 | `bed570e8` | Typed immutable bundles, native deterministic PAX/gzip, strict bounded archive reads, Python-compatible content fingerprint/digests, retarget/comparison, Source/Target adapter, fake Agent receiver, real Agent extractor conformance. |
| A11-2 | `ad0c78de` | Source coordinates/secret hiding/artifact context, provider locations hook, MosDNS conversion, captured library/rule-proxy/gateway callbacks, recursion bound 3, last-good rule sources/download remedies. |
| A11-3 | `c88f50a5` | Complete Mihomo/MosDNS/OxiDNS graphs, pins/resources/watchdog, gateway hostile-provider guard, eight joined workers + per-build URL single flight/LRU, Geo resources, actual production bundle comparison. |
| A11-4 | `21765054` | Private atomic receipts/previews, retention/quota/expiry/current identities, frozen ZIP/file/final-preview, OxiDNS schema/build evidence, default-profile fix, public task projection. |

Follow-ups: `20b7f4ea` acceptance adapter formatting/status, `17336d4b` fake transcript file labels, `8909ced4` test hook cleanup, `a2da6dd6` per-attempt rule-download timeout correction. No extra total deadline shortens Python's (10 s connect, 60 s read) retry/candidate semantics. Existing shared transports remain unchanged.

Python files: `deployment_bundle.py`, `deployment_locations.py`, `mihomo_resources.py`, `generated_preview.py`; design A11-4 also provides `rule_sync_store.py` and `oxidns_validation_receipt.py` primitives needed by later A14/A16. Routes: none. C34 helpers are Pack/Open and Previews Save/Load/Public/Zip/File.

Boundary evidence:
- Real Python: 31 manifests + 17 path cases and Python PAX readback; 64 conversions, 23 excerpts, 9 contexts, 7 callbacks; 41 bundle/failure cases + 10 Geo selections; Python-written receipt/preview directories and public files. Generated main YAML compared structurally; verbatim rule/provider/custom bytes exact. Frozen v1 fingerprint encoding compared byte-for-byte to Python independently from native archive encoding.
- Concurrency/bounds: lazy pack once/drop raw content, immutable copies, URL sharing within one build, no cross-build reuse, ≤8 workers, caller completion callbacks, raw/result byte/entry limits (including empty and oversized responses), failure retry, alias depth-64/cycle providers, aggregate limits, strict flags. Receipts acknowledge only an unchanged applied pointer; preview metadata/source bytes count toward quota.
- C30 deferred diagnostics **closed**: deploy.Wire installs provider.SetLocations; isolated C30 fixture installs/restores it explicitly. `deferA11Locations` removed. Full source/file/line/column/excerpt 400 messages and all stored effects compare strictly.
- OxiDNS content identity and validation identity are separate: current schema/build proof accepted; changed/old/extra-field/foreign proof refused. Actual A01 activation gate returns exact Chinese re-push diagnostic and sends no activation POST. Profile-less Agents use the resolved non-default-named default profile.

Approved clarifications/dependencies:
- convert.Download optional Behavior/Format are populated on convert's side and read only in deploy/adapter.go. Existing Object/public download DTO unchanged; all downloads generators retain successful outputs with no differences. Contract note in a14-publish.md; N8/S-convert ownership note in HANDOFF 3b.
- Read-only oxidns.SchemaFingerprint; RulesFingerprint follows native schema/build design and intentionally invalidates Python-era ready candidates (O12 ledger updated). Content Fingerprint remains frozen to Python encoding for unchanged bytes.
- Production bundle generator: **3 compared, 2 independent generation successes, 1 partial** labelled `partial: A11 packaging only, aggregation provider frozen (C31 pending)` in summary and coverage, excluded from end-to-end success. Frozen real Python aggregation bytes stay mode 0600 in private temporary test-server reports. Aggregation-provider generator remains Go 尚未实现. C31 unfreeze task is in PROGRESS-config. No snapshot data or baseline normalization committed; coverage floors protect complete and partial output counts.

Final authoritative `.32` command (exit 0):
```sh
rt -- bash -lc 'set -e; cd server; gofmt -l internal/ tools/; test -z "$(gofmt -l internal/ tools/)"; go vet ./...; go test -race -count=1 ./internal/deploy ./internal/gateway ./internal/provider; go test -p=4 -count=1 ./...; CONFIGFLOW_DIFF=1 go test -p=4 -count=1 $(go list ./... | rg /difftest); cd ../backend/agents/go-agent; CONFIGFLOW_SERVER_ARCHIVES=$PWD/../../../server/internal/deploy/testdata/archives go test -count=1 -run ServerArchive ./routes/; cd ../../..; ~/cf-test/.venv/bin/python -m unittest server/tools/proddiff/test_proddiff.py'
```
Results/tails:
```text
gofmt: no output; go vet: exit 0
ok configflow/internal/deploy 10.365s       (race)
ok configflow/internal/gateway 33.992s      (race)
ok configflow/internal/provider 110.743s    (race)
full suite: exit 0; final lines:
ok configflow/internal/weburl 0.006s
ok configflow/internal/yamlx 2.304s
?  configflow/tools/proddiff/generator [no test files]
all 9 CONFIGFLOW_DIFF packages: ok
  api/agents 122.243s; api/subscriptions 81.608s; discovery 2.638s;
  health 14.103s; httpx 1.433s; mosdns 1.693s; refresh 3.837s;
  substore 6.897s; webdav 21.334s
ok agent/routes 0.340s                    (15 real-Agent archive cases)
Ran 32 tests in 0.018s — OK              (proddiff comparator regressions)
```
After the late per-attempt timeout correction and test-only follow-ups, `.32` reran whole-tree gofmt/vet and focused race (exit 0):
```sh
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/ tools/)" && go vet ./... && go test -race -count=1 ./internal/deploy ./internal/agents/agentfake'
```
```text
ok configflow/internal/deploy 7.069s
ok configflow/internal/agents/agentfake 1.062s
```
Final proddiff after that correction (exit 0): `rt -- server/scripts/proddiff.sh`:
```text
self_check: PASS; coverage_regressions: 0; unmapped_routes: 0
agent-deployment-bundle: compared=3 success=2 partial=1
baseline: new=0 stale=0
```
The earlier .63-only rulefetch proxy_refused timeout difference did not reproduce in the authoritative `.32` full run (`ok configflow/internal/rulefetch 9.574s`). No open implementation findings are claimed; this is not a substitute for the required fresh-Opus HR review. L2 real core/service/recovery validation remains as listed in A11-HANDOFF.md and design 6.7.

## A12 — push diff

**Status: `verified`** · 独立 Codex 复核 2026-11-17（非作者）；**0 未决**。
复核修复 `eb065a54`，分支 `go-review-a12`，最终 rebase 到 `go` `038b52ae`。
5 项发现已修复（4 中：反斜杠 URL 漏标、provider 锚点丢失导致整份 raw 未标记、深度 64 身份截断漏报、JSON 转义改变分页预算；
1 低：数字拼写制造多余变更）。独立 Python oracle 77 行/180 分页比较，新增回归与原语料通过。
共享 yamlx ValueHash/MaterializeAliases 和 jsonresp.EncodeUTF8；AllPairs 采用 go 的唯一实现，
ProjectJSONKeys 不用于会丢键类型的身份比较。无私有 helper 副本、无 Python 模拟、无新增依赖。
完整发现、修复前失败、确切 rt 命令与输出见 [review-a12.md](review-a12.md)。

最终门禁：`.64`、`GOMAXPROCS=4`，gofmt 空、vet 全仓通过、改动包 `-race -p=4`、
全量 `go test -p=4 -count=1 ./...`、全部 12 个 CONFIGFLOW_DIFF 包、四个 fuzz 各 60 秒均通过，
proddiff `self_check=PASS new=0 stale=0`（bundle compared=3 success=3）。
O3/R07/C31/原版 A12 合入后，在 d7c481c7 合并树再次通过 gofmt/vet、改动包 race、全量、全部 12 difftest 与 proddiff；
最后 038b52ae 仅增加实验报告，无源码变化。一次 .32 集成补验 SSH 挂起、退出 255，未计为通过；
所有最终门禁结果均取 .64。早期 .32 的 oracle/installer 交叉检查正常通过。
无 push/PR、无生产/PVE 访问、无子 agent。A13 的缓存/路由测试仍归 A13。

作者 ported 阶段记录（N7-3a rebase 复验全通过，复核前状态） · prereqs: A11 verified · Go: `internal/deploy/pushdiff` · branch `go-port-a12` · 实现提交 `44f409e9`；深层路径修正 `f001fc9f`；空区间修正 `476143c4`；既有验收记录 `659fdb8e` / `ad03c2a4`（本轮先 rebase 到 N7-3a `8303397c`，最终基线 `ac1f2377`）。

范围：`backend/agents/push_diff.py` 全部纯比较能力。实现按 `design/a14-publish.md` A12-1：typed `Diff`/`Page`/`Text`、逐键 YAML、命名/标量列表、带敏感片段的统一 diff、文件排序/计数、节点重复名/未命名配对、规则多重集合、行/字节/文件/条目预算、zlib 压缩分页、搜索、stored-token 局部替换。无 HTTP 注册（A13/A14）、无仓库/网络/设备访问，无 push、无子 agent。

决定/接口：
- `Build(AppliedView, *deploy.Bundle, *deploy.Receipt, map[string]*FileEntries)`；nil `AppliedView.Files` 才表示摘要缺失，空 map 是有效基线；nil receipt 明确表示无保存 bundle，调用者负责 Load。
- 复用 `deploy.Digests`/`ComparisonManifest`；基线比较摘要读取 **Receipt.ComparisonConfig**，详情读取 receipt 的 live bytes。仅 `Bundle.Files(paths...)` 读取列出的变化文件，空路径集不调用会读取全量的 Files()。
- `diag.CredentialSpans` / `IsCredentialKey` 放在共享诊断包，URL 识别直接复用 FailureText 的 `diag.URLPattern`，没有复制它的私有模式。URL/userinfo/query/opaque 各分支有单独测试。
- 所有 YAML 入口使用 `yamlx.Parse`；直接使用 N7-3a 已提供的 `yamlx.AllPairs`，保留有效映射键的类型和 merge 次序；A12 原来新增的 13 行已在 rebase 中删除，`internal/yamlx` 相对 go 无改动。`ProjectJSONKeys` 用于改写临时 JSON 投影；本包需要保留键标签做差异比较，不使用该变换。无 `yamlx.Clone`。布尔/数字值通过 facade 解码或 ToJSON 处理。
- `Text` 保持 string / [string, {secret:string}] wire union；`WithoutStored` 接受 `func(string) []redact.Range`（现有 redact.StoredSecretFinder 是返回该函数的工厂，并非类型）。分页不可变，可并发读；Page 对无效 change 返回空页，A13 在调用前验证 change/offset/limit/query（常量供其复用）。

与 Python 的差异/修正：
- 正常语料除 raw 排版外的 JSON 字段/值 **无差异**。raw 使用 yamlx/yaml.v3 原生输出和 patience unified diff（不是 Python difflib 的移植）；语料只排除 `raw` 的文本/行数/切分，独立 patch 测试验证能还原完整新文档，单测验证标记和所有边界。原生 YAML 排版/引号、native diff 的合法 hunk 选择属于 PORTING 已接受的格式差异。
- `reformatted` 使用原生 canonical byte delta 加输入排版变化证据（style/indent/comments/blank lines），避免 Go 列表缩进带来的大列表普通编辑误报。512-byte 附近的格式提示可能与 CSafeDumper 不同（原设计已接受），不影响 changes/status。
- **修复 Python bug（中：敏感信息标记）**：JSON 展示值里的 mapping key 原先未标记其 URL 凭据，raw 的普通 mapping key 同样漏掉。Go 对两者标记；固定 string 类型的 row.key/path 使用 `[REDACTED]` 消除凭据，保留分组 schema，原文仍在有标记的 raw/display 中。`TestCredentialFieldsAndSpans` 有 scalar→map 和逐键两种回归。
- **修复 Python bug（低：规则摘要）**：原先 rule_entries 用行正则，JSON/quoted payload/flow sequence 被当作普通文本或空列表，且 inline comment/escape 仍被当成规则。Go 复用 rulesetfmt.HasYAMLPayload 并真正解析 YAML；无效 YAML/非标量 payload 回退 size，不给错误规则计数；UTF-8 BOM 正确移除。
- D1 重复键拒绝、alias 预算/自引用拒绝、yaml.v3 的 YAML 1.2/Unicode/tag 行为统一继承 N7（包括 lone surrogate 退回 size），不模拟 PyYAML/CPython。Go url.Parse 对非法 percent escape 更严格，标记整个 URL，fail closed。

证据：
- `testdata/gen_a12.py` 在测试服务器用真实 Python build_push_diff 生成 `python.json.gz`；执行原测试 32 个无 Checker 的函数（scalar 6 参数），产生 **67 个边界行，其中 A11-3 bundle 配对 20 行**，含针对性 credentials/CJK 和 101000 条规则分页。所有期待值均由 Python 计算，原测试输入中私网地址先转为 TEST-NET 后重新计算 oracle；没有生产数据。正常结果 JSON 解码后严格比较，分页样本也逐字段比较。
- `TestYAMLEntrypoints`：Config/nodeItems/RuleEntries 三个 YAML 入口各有深度 40 的指数 alias 链（拒绝）、深度 40 线性链（接受）、自引用、duplicate、多文档测试。`FuzzConfig`、`FuzzRuleEntries`、`FuzzNodeItems`、`FuzzCredentialSpans` 各有相关种子。
- `TestBundleLimitsAndImmutability`、`TestPagingBudgetsAndConcurrency`：500 文件、行/字节限制、100000 retained quota 公平分配、全局/文件字节耗尽、页大小/字节/末尾、zlib 压缩、并发无变异。`TestUnifiedPatchAndMarkers`：块文本/单双引号/tag/alias/URL/bare userinfo，凭据消失且普通 host/port 留存。
- `test_deployment_bundle_lazy_archive.py`：核心 7 个 Bundle/retarget/archive 用例由 A11 既有 `core_test.go`/`review_test.go` 覆盖；`test_listing_changes_reads_files_without_compressing_either_side` 对应本包 Build 只走 Files + `TestBundleLimitsAndImmutability`（packing 前后同结果）；`test_a_content_check_and_its_change_list_compress_nothing` 属 A13 Checker，保留给 A13。

`test_agent_push_diff.py` 每个测试的归属/覆盖（39 个函数，7 个属于 A13）：

| Python 测试 | Go 覆盖 |
|---|---|
| `test_main_yaml_is_compared_by_key_path_with_old_and_new_values` | `TestPythonCorpus`（真实 Python oracle） |
| `test_named_lists_match_entries_by_name_and_list_member_changes` | `TestPythonCorpus`（真实 Python oracle） |
| `test_reordered_rules_are_reported_even_without_additions` | `TestPythonCorpus`（真实 Python oracle） |
| `test_long_lists_are_counted_exactly_but_truncated` | `TestPythonCorpus`（真实 Python oracle） |
| `test_rows_values_and_raw_diff_are_bounded` | `TestPythonCorpus` + `TestRawAndValueBounds` |
| `test_values_are_returned_with_every_credential_marked` | `TestPythonCorpus`（真实 Python oracle） |
| `test_raw_diff_marks_credentials_exactly_where_yaml_writes_them` | `TestPythonCorpus` + `TestUnifiedPatchAndMarkers`（Go 原生 YAML 排版） |
| `test_display_texts_escape_and_mark_credentials_inside_json` | `TestPythonCorpus`（真实 Python oracle） |
| `test_stored_tokens_become_hidden_redactions_inside_display_texts` | `TestStoredRedactionAndSearch`、`TestStoredRangeUnionAndUnicode` |
| `test_secret_detection_keeps_domains_and_data_keys` | `TestCredentialFieldsAndSpans` |
| `test_credential_spans_inside_text` | `TestCredentialFieldsAndSpans`、`FuzzCredentialSpans` |
| `test_formatting_only_changes_are_not_reported_as_items` | `TestPythonCorpus`（真实 Python oracle） |
| `test_unparsable_yaml_falls_back_to_file_level` | `TestPythonCorpus`（真实 Python oracle） |
| `test_rule_entries_read_text_and_yaml_payloads` | `TestRulePayloadForms`（修正 Python 的行匹配缺陷） |
| `test_files_are_listed_by_digest_with_role_summaries` | `TestPythonCorpus`（真实 Python oracle） |
| `test_status_follows_comparison_digest_when_live_main_file_differs` | `TestPythonCorpus`（真实 Python oracle） |
| `test_without_saved_bundle_only_digests_are_compared` | `TestPythonCorpus`（真实 Python oracle） |
| `test_large_rule_sets_are_counted_quickly_and_keep_the_response_small` | `TestPythonCorpus`（真实 Python oracle） |
| `test_large_inline_rules_in_the_main_file_stay_bounded` | `TestPythonCorpus`（真实 Python oracle） |
| `test_subscription_details_list_nodes_and_their_field_changes` | `TestPythonCorpus`（真实 Python oracle） |
| `test_removed_nodes_carry_type_and_address_like_added_ones` | `TestPythonCorpus`（真实 Python oracle） |
| `test_repeated_and_unnamed_nodes_are_matched_in_file_order` | `TestPythonCorpus`（真实 Python oracle） |
| `test_unchanged_or_unparsable_subscriptions_have_no_details_and_odd_names_are_listed` | `TestPythonCorpus`（真实 Python oracle） |
| `test_rule_set_details_list_added_and_removed_rules` | `TestPythonCorpus`（真实 Python oracle） |
| `test_the_response_lists_the_first_page_of_every_group` | `TestPythonCorpus`（真实 Python oracle） |
| `test_one_response_carries_a_bounded_amount_of_details` | `TestPythonCorpus`（6000-byte oracle）+ `TestPagingBudgetsAndConcurrency` |
| `test_large_subscriptions_and_rule_sets_are_detailed_quickly` | `TestPythonCorpus`（真实 Python oracle） |
| `test_node_details_are_display_texts_for_stored_token_redaction` | `TestStoredRedactionAndSearch` |
| `test_scalar_display` | `TestPythonCorpus`（真实 Python oracle） |
| `test_change_list_shares_the_status_worker_and_cache` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_the_device_list_check_leaves_the_change_list_ready` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_a_device_found_in_sync_gets_no_change_list_and_drops_an_old_one` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_a_list_pushed_out_by_newer_ones_is_prepared_again_on_request` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_synced_and_unavailable_answers_skip_the_change_list` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_failed_listing_keeps_the_status_and_retries_soon` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_only_a_few_change_lists_are_kept` | **A13 所属**：Checker 缓存/调度/重试/淘汰，待 A13 的 checker/路由测试；A12 不实现 Checker |
| `test_agent_cleaned_provider_paths_are_not_changes` | `TestPythonCorpus`（真实 Python oracle） |
| `test_paths_that_name_another_file_still_count` | `TestPythonCorpus`（真实 Python oracle） |
| `test_same_writing_style_is_not_called_reformatting` | `TestPythonCorpus`（真实 Python oracle） |

新增文件行数（`internal/deploy/pushdiff/`）：`build.go` 263；`config.go` 609；`corpus_test.go` 195；`credentials_test.go` 68；`entries.go` 341；`fuzz_test.go` 89；`json.go` 90；`lists.go` 308；`messages.go` 8；`raw.go` 372；`safety_test.go` 436；`text.go` 303；`types.go` 166。
共享新增 `internal/agents/diag/credentials.go` 124 行；`yamlx/native_node.go` 无 A12 增补（改用 N7-3a 的 AllPairs）；语料生成器 `testdata/gen_a12.py` 175 行、门禁 `testdata/gate.sh` 20 行、Python oracle gzip 1761756 bytes。无新增依赖。

命令（均从 worktree 根目录）：
```sh
rt --back server/internal/deploy/pushdiff/testdata/python.json.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/pushdiff/testdata/gen_a12.py'
rt -- bash -lc 'cd server && go test -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx'
rt -- bash server/internal/deploy/pushdiff/testdata/gate.sh
```
最终完整门禁（`.64`，`GOMAXPROCS=4`，实现树 `e127a116` / base `8bf1b189`）：**全通过，exit 0**。确切命令：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -o pipefail; bash server/internal/deploy/pushdiff/testdata/gate.sh 2>&1 | tee /tmp/configflow-a12-gates.log'
```
`gate.sh` 顺序执行 `gofmt -l internal/ tools/`（空且强制检查）、`go vet ./...`、`go test -race -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx`、`go test -count=1 ./...`、`for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done`、四个 fuzz 各 60 秒、`server/scripts/proddiff.sh`。

最后输出摘要（各命令完整输出在测试服务器 `/tmp/configflow-a12-gates.log`，该日志不入库）：
```text
gofmt-output: []   go vet: exit 0
ok configflow/internal/deploy/pushdiff 44.883s (race)
ok configflow/internal/yamlx 12.971s (race); diag [no test files]（其逻辑由 pushdiff 的 corpus/fuzz/race 覆盖）
full go test -count=1 ./...: exit 0; pushdiff 3.213s; cmd/configflow 96.782s; installer 174.535s; lifecycle 0.050s
CONFIGFLOW_DIFF all 10: agents 110.389s, subscriptions 67.488s, discovery 1.789s, health 13.546s,
 httpx 1.058s, mosdns 1.388s, refresh 3.125s, subs 1.204s, substore 4.491s, webdav 14.800s — all ok
FuzzConfig          60s 307048 execs PASS  (61.048s)
FuzzRuleEntries     60s 293678 execs PASS  (61.040s)
FuzzNodeItems       60s 563710 execs PASS  (61.023s)
FuzzCredentialSpans 60s 125972 execs PASS  (61.025s)
proddiff.sh: self_check PASS, coverage_regressions 0, unmapped_routes 0
agent-deployment-bundle compared=3 success=2 partial=1（沿用 C31 frozen aggregation 的已有显式 partial）
baseline: new=0 stale=0
```

此前迭代：`.64` Python oracle `A12 Python: 32 tests / 67 boundary rows; 28433991 JSON bytes`。收尾发现并修正 Go 端两项低问题：walk 错把显示 MAX_DEPTH 用于路径截断（80 层路径测试已补）；首次凭据 fuzz 找到空 userinfo 的零长度区间，合并前过滤空区间，保留 `testdata/fuzz/FuzzCredentialSpans/e07ee712a01e982a` 和显式测试。修正后上面的四个 fuzz 均跑满 60 秒通过；没有未解决问题。

门禁完成后 `go` 合入 N8-3 复核（`6b50151c`）；最终 `git rebase go` 到该基线后补跑以下集成检查，**全部 exit 0**。A12 源码不变，不重复上述已通过的 difftest/fuzz；完整门禁和本轮补测都只在 `.64` 上跑。
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'set -euo pipefail; cd server; test -z "$(gofmt -l internal/ tools/)"; go vet ./...; go test -race -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx; go test -count=1 ./...; cd ..; server/scripts/proddiff.sh'
```
```text
gofmt-output []   go vet exit 0
race: pushdiff 50.130s; yamlx 11.570s; diag [no test files]
full go test -count=1 ./... exit 0; pushdiff 8.326s; model 0.021s; repo 20.273s
ok configflow/internal/yamlx 4.715s
? configflow/tools/proddiff/generator [no test files]
proddiff: coverage_regressions=0 unmapped_routes=0 self_check=PASS
baseline: new=0 stale=0
```
分支 `go-port-a12`，无 push/PR、无生产访问、无子 agent、无新增依赖。纯 A12 功能无未移植项；A13 的 7 个 Checker 测试归属见上表。

最后一次 `git rebase go` 纳入 `8489da36`，相对已补测的 `6b50151c` **仅 WORKFLOW.md 增加 4 行**，无源码变化，不重复测试。最终工作树干净。

N7-3a 合入后的冲突复验：唯一冲突 `internal/yamlx/native_node.go` 保留 go 版本；没有新增/复制共享 API，A12 实现源码无其他改动。pushdiff/yamlx、四个 60 秒 fuzz、全部 difftest 和 proddiff 复验均通过（`.64`，exit 0），没有新的实现差异或遗留问题。

本轮确切命令（worktree 根目录，`rt` 自动选择 `.64`）：
```sh
rt -e GOMAXPROCS=4 -- bash -lc 'set -euo pipefail; set -o pipefail; { cd server; test -z "$(gofmt -l internal/deploy/pushdiff internal/yamlx internal/agents/diag)"; go test -race -count=1 ./internal/deploy/pushdiff ./internal/yamlx; for target in FuzzConfig FuzzRuleEntries FuzzNodeItems FuzzCredentialSpans; do go test -run "^$" -fuzz "^${target}$" -fuzztime=60s ./internal/deploy/pushdiff; done; for d in $(go list ./... | grep difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d"; done; cd ..; server/scripts/proddiff.sh; } 2>&1 | tee /tmp/configflow-a12-n73a-rebase.log'
```
```text
ok configflow/internal/deploy/pushdiff 35.065s (race; Python corpus included)
ok configflow/internal/yamlx 9.216s (race)
FuzzConfig          60s 244263 execs PASS (61.042s)
FuzzRuleEntries     60s 328254 execs PASS (61.032s)
FuzzNodeItems       60s 228981 execs PASS (61.061s)
FuzzCredentialSpans 60s 176694 execs PASS (61.054s)
all 12 CONFIGFLOW_DIFF packages: agents 117.365s; aggregations 24.155s; subscriptions 76.717s;
 devicenotes 0.505s; discovery 2.146s; health 13.633s; httpx 1.354s; mosdns 1.705s;
 refresh 3.654s; subs 1.835s; substore 6.173s; webdav 18.065s — all ok
proddiff: self_check=PASS coverage_regressions=0 unmapped_routes=0 compared=435
agent-deployment-bundle compared=3 success=3（新 go 已解除旧 C31 frozen partial）
baseline: new=0 stale=0
```

最终 `git rebase go` 到 `ac1f2377`；相对本轮已验证的 `8303397c`，仅 installer 测试的 UTF-8 locale 修正和文档，无 A12/yamlx 或其他生产源码变化，不重复验证。工作树干净，未 push，尚未开始 A13。

## A13 — content sync, Agent list/detail, push-diff routes

**Status: `verified`** · 独立复核0未决；完整合并门禁按协调者2026-11-18指示改由其预留 cf-gate 槽执行，尚未完成。prereqs: A12, A15 (`upgrades.artifacts` for `upgrade_available`) · Go: internal/deploy, api/agents

独立对抗复核完成（分支 `go-review-a13`，[review-a13.md](review-a13.md)）；最终 rebase go@895f5e21（包含0dace4ae）。
F1 中严重度（真实 Spawn 丢失 Close 取消）和 F3 高严重度（公共投影丢失 credential 来源）已修复。
F3 使用 shared CollectCredentials 和唯一 publicAgentWithCredentials projector；summary 通过
repo.GetAgentsInspect / Manager 透传在同一 view、轻读前收集独立 Secrets，collector 不构造历史副本。
四种 GET 读法的 journal→deployment.api_key、HA 引用、HTTP error/panic 日志、cycle/depth、
同代 snapshot/并发 commit、runtime overlay 均通过定向 race；repo 补验 1.543s，API 8.507s。
**N8-5 迁移时需保留此 inspection 能力**；若 O3-6a 先合入，保持其锁结构并把 callback 接回原 read view。
F2 高严重度（环境注册 key 从 diff/entries/search 泄露）已修复：A12 唯一 mapTexts 元数据兜底 +
shared finder 环境快照；合并树实际 HTTP 明文/编码/节点名/字典键/分页/搜索、error/panic 日志全通过。
定向 race：redact 1.354s / pushdiff 64.166s / contentsync 4.126s / API 17.507s；真实 Flask 两组差分 3.405s。
新增验收时序修复：仅 proddiff tag / Flask launcher bounded settle（120s），超时明确 harness 错误，不分类 new/stale。
协调者批准原20条外再加4条稳定后的 checked_at 精确 deferral，共24；proof必须逐项证明24，A14接线后24条全部消失。
harness 39 tests、tagged 慢Checker/假钟超时 race 自检通过；24条因果证明21 HTTP/3 actual bundles通过。
最终 gate.sh -p 6 + 相关 fuzz 在 .64 前台排队约25分钟；协调者明确要求停止排队并交回，完整门禁改由其预留 cf-gate 槽执行。
合并前仍需全量race、所有difftest、proddiff new=0/stale=0（24条逐项观察）、五个相关60秒fuzz；本次不声称它们已全部通过。
本轮受测树39个harness/3个真实Python安全oracle/24条proof已复验通过。
基线36 tests、原Python六文件101 tests、三项安全oracle通过；原20项因果证明21 HTTP/3 actual bundles通过。
复核提交：f7f75d80（F1）、972ff692（F2 metadata）、4864ab65（baseline）、9d772f8f（F3 shared projector/inspection）、58e86bc1（F2 registration）、a1a07e2c（F4 observer/proof）；交回为本提交。
独立复核 verified，0 未决实现发现；完整门禁结果由协调者补录。两次临时 worktree、临时 branch 和 rt 两机副本均已清理。

**Design:** `server/design/a14-publish.md` (Opus, 2026-11-17) covers the whole chain A11→A14 — package map, types, Agent protocol, state machine, locks, failure table, steps A13-1, A13-2, acceptance. Port to it; contract changes need the designer's or coordinator's agreement.

Files:
- `backend/agents/content_sync.py` (460)
- `backend/routes/agents.py` (124) — lines 216–245, 1633–1726 (plus the GET branches of `handle_agents`/`handle_agent_item`, whose lines are counted in A03)

Tests: `test_agent_content_sync.py` (42), `test_agent_content_api.py` (10), `test_agent_push_diff_api.py` (7), `test_agent_push_diff_paging.py` (9), `test_agent_first_push_sync.py` (6); deferred from A01/A03 (list/detail cases)

Routes (4): `GET /api/agents`; `GET /api/agents/<agent_id>`; `GET /api/agents/<agent_id>/push-diff`; `GET /api/agents/<agent_id>/push-diff/entries`

A24 clarification: reuse the same-package `publicAgent(*agents.Record) *agents.Record` helper in `api/agents/public_agent_adapter.go`; do not add another serializer. It delegates deployment, upgrade and HA sanitization to their existing owners and omits unread history. A24 review additionally uses shared `redact.SanitizeCredentials` to mask legacy credential fields, Basic-auth hosts and references to known tokens / the environment registration key. Real Python field-by-field differential: `TestPublicAgentPython` (24 cases); the Python credential leak is a separate security correction, documented in `behaviour-changes.md`.

Evidence: GET branches extend the existing `createAgent`/`handleAgentItem` handlers, sharing endpoint registration. `_public_agent` is supplied by A24's shared helper. C13 `subscription_cache` invalidation is registered here and broadcasts to all live Manager-owned Checkers.

Branch `go-port-a13`, original base `631a4815`, rebased onto `go@4d8a665a` (A24, A12 review fixes, N8-4, O3-5). Coordinator decisions are recorded in design/a14-publish.md “Clarified in A13”. A13-1 commit `e07211a2`: opaque input keys, bounded Checker, Manager lifetime cleanup, and api/agents dispatcher wiring. A13-2 commit `d4a3f737` implements all four GET routes. Causality proof / exact deferred baseline / stale enforcement: `f55cb76d`; O3-5 fixture isolation: `9bcd2cf3`. A24 helper was initially cherry-picked; after A24 merged, the duplicate commit was skipped, retaining upstream's expanded publicAgent tests and empty-HA fix. A14 production preparation remains injected. All tests run through rt; no subagents, pushes or production-device access.


A13-1 decisions and evidence:
- api/agents owns one Checker per Manager, avoiding deploy→contentsync cycles. Manager.Close/OnClose unregisters the Checker; singleton replacement closes the old owner outside singletonMu; PhaseClose closes composition-owned Managers.
- Shared Source projection has two explicit Python policies: content-sync includes profile_name; preview additionally ignores it (generated_preview.py:33). No legacy object escapes the adapter. Existing Source snapshots are immutable. The Checker takes its extra worker snapshot only after admission; A11's NewTarget still performs the existing boundary snapshot before the Checker call.
- Default 2 workers, 900s/30s TTL, 512 statuses, 8 details and 48MiB are exercised with fake clocks. Keys/slots release exactly once on rejection, panic, completion, cancellation and Close. Invalidate replaces active entry identity while keeping its slot; CompareNow checks generation.
- Preparation runs in a fresh task Scope; Detach is in owners, keeping Runtime shutdown cancellation. The shutdown test advances the default eight-second grace to its six-second job-cancellation phase, without shortening production parameters.
- Real Python corpus: 9 histories / 2001 observations. Status/reason, timestamp presence, version stability/replacement, job count, preparation count and cache sizes compare strictly. Random IDs and wall-clock timestamps are compared by relationships/presence.
- Python repository-object switching is replaced by Manager/Checker ownership and cleanup. SystemExit/deepcopy classes are not emulated: Go uses typed snapshots and panic-safe cleanup.
- First shutdown-test attempt blocked because the fake clock was not advanced; corrected the test driver, retained production grace. No unresolved implementation issue.

Commands (worktree root; all through rt):
```sh
rt --back server/internal/deploy/contentsync/testdata/checker.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/deploy/contentsync/testdata/gen_a13.py'
rt -- bash -lc 'cd server && go test -count=1 ./internal/deploy/contentsync ./internal/deploy && go test -race -count=1 ./internal/deploy/contentsync && go vet ./internal/deploy/... ./internal/api/agents ./internal/agents'
rt -- bash -lc 'cd server && go test -race -count=1 -run "TestContent|TestManagerClose|TestSingletonReplacement" ./internal/agents ./internal/api/agents'
```
`.64`: Python Checker 9 histories / 2001 observations; contentsync unit+corpus PASS (0.083s), race PASS (1.804s), deploy PASS (1.552s), vet PASS. Manager/pool lifetime tests PASS.

Python test_agent_content_sync.py mapping (42 functions):

| Python | Go |
|---|---|
| `test_list_only_schedules_and_repeated_polls_share_one_preparation` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_only_rule_file_change_is_detected_with_unchanged_main_config` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_runtime_metadata_and_heartbeat_do_not_invalidate_cache` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_nested_config_fields_are_not_silently_removed_from_cache_key` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_remote_changes_are_rechecked_only_after_success_ttl` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_changed_input_never_publishes_old_worker_result` | TestOldWorkersInvalidationAndReturnToOriginal; TestInputProjectionAndPreconditions; TestPythonCheckerHistories |
| `test_new_successful_baseline_invalidates_in_flight_comparison` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_returning_to_original_input_still_discards_obsolete_worker` | TestOldWorkersInvalidationAndReturnToOriginal; TestInputProjectionAndPreconditions; TestPythonCheckerHistories |
| `test_generation_system_settings_and_base_url_invalidate_cached_result` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_incomplete_deployment_invalidates_in_flight_success` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_failed_deployment_can_still_compare_confirmed_prior_baseline` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_invalid_baseline_fingerprint_never_starts_preparation` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_binding_service_or_reported_version_mismatch_is_unknown` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_missing_baseline_is_not_backfilled_from_current_config` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_error_is_secret_free_and_uses_short_ttl` | TestErrorTTLRejectPanicAndCancellation; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_workers_are_bounded_without_queued_jobs` | TestProductionTTLsAndNoQueue; TestConcurrentPollsAndIsolatedScopes; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_repository_scope_isolates_identical_agent_ids` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_repository_change_before_worker_starts_skips_preparation` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_repository_change_during_preparation_never_publishes_success` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_same_scope_does_not_reuse_another_repository_object_result` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_uninitialized_repository_is_not_created_for_standalone_checker` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_stale_request_repository_is_rejected_before_scheduling` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_custom_installed_filename_has_same_content_fingerprint` | TestInputProjectionAndPreconditions; TestProductionTTLsAndNoQueue; TestPythonCheckerHistories |
| `test_worker_resets_reused_config_context_before_and_after` | TestClockFromBoundRequestAndFreshPreparationScope |
| `test_system_exit_is_not_converted_to_unknown_and_releases_slot` | TestErrorTTLRejectPanicAndCancellation; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_rejected_executor_submission_does_not_leak_error_or_slot` | TestErrorTTLRejectPanicAndCancellation; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_real_executor_runs_preparation_off_request_thread` | TestProductionTTLsAndNoQueue; TestConcurrentPollsAndIsolatedScopes; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_concurrent_list_requests_schedule_one_job` | TestProductionTTLsAndNoQueue; TestConcurrentPollsAndIsolatedScopes; TestRuntimeShutdownCancelsAdmittedJobAndReleasesSlot |
| `test_compare_now_answers_in_the_callers_thread_and_shares_the_cache` | TestSynchronousCompareCacheAndGeneration; TestCompareNowReusesListAndDoesNotCachePreparationErrors; TestPythonCheckerHistories |
| `test_compare_now_reuses_a_fresh_list_result_until_it_expires` | TestSynchronousCompareCacheAndGeneration; TestCompareNowReusesListAndDoesNotCachePreparationErrors; TestPythonCheckerHistories |
| `test_compare_now_metadata_alone_is_still_synced` | TestSynchronousCompareCacheAndGeneration; TestCompareNowReusesListAndDoesNotCachePreparationErrors; TestPythonCheckerHistories |
| `test_compare_now_never_claims_a_match_without_a_confirmed_baseline` | TestSynchronousCompareCacheAndGeneration; TestCompareNowReusesListAndDoesNotCachePreparationErrors; TestPythonCheckerHistories |
| `test_compare_now_preparation_failure_is_secret_free_unknown` | TestSynchronousCompareCacheAndGeneration; TestCompareNowReusesListAndDoesNotCachePreparationErrors; TestPythonCheckerHistories |
| `test_compare_now_rejects_another_repository` | TestConcurrentPollsAndIsolatedScopes; TestSingletonReplacementClosesOldManager; TestContentPoolFollowsManagerLifetime (owner isolation replaces process-global repository switching) |
| `test_a_kept_answer_and_a_running_check_copy_nothing` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_an_unavailable_comparison_copies_nothing` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_the_list_never_changes_what_it_was_given` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_the_worker_keeps_the_values_of_the_request_that_started_it` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_devices_of_one_profile_get_separate_copies_of_it` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_inputs_that_cannot_be_copied_start_no_worker_and_keep_no_slot` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_requests_arriving_while_the_copies_are_made_start_no_second_worker` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |
| `test_random_histories_answer_as_when_every_request_copied_first` | TestRequestSnapshotsAndRetarget; TestContentInputProjection; TestConcurrentPollsAndIsolatedScopes (typed immutable snapshots replace Python deepcopy) |

A13-2 route coverage and decisions:
- `content_routes_adapter.go` is the legacy-record HTTP boundary. Full/history=0 reads retain private deployment history for pending-state checks; summary=1 does not load profile snapshots, artifacts or a Checker. Public output always uses A24's `publicAgent`.
- Preserve the applied publication origin; never substitute the UI request host. Source snapshots are reused within a list request by profile. Missing-profile errors affect only that Agent. Repository transactions reject broken profile bindings, so the stale-profile branch is tested at the adapter boundary with a stale record, matching Python's monkeypatched Manager scenario.
- Push-diff and page responses use `WithoutStored` + `redact.StoredSecretFinder`; stored credentials cannot escape or match search. Validate page arguments before looking up the Agent; preserve no-store headers and stale precedence. Version IDs have Python's 16-hex-character shape (an initial 32-character implementation was corrected during boundary testing).
- `TestContentReadsAgainstFlask`: 33 requests, no response fields masked. `TestContentChangesAgainstFlask`: real synthetic on-disk receipt, real Flask/Go Checker and pushdiff, only A14 preparation injected; complete changed JSON, 81-rule paging, Unicode search, stored-token search exclusion, missing file/group, replaced version, cached list/detail. Only independent random version IDs and checked_at timestamps are masked; IDs are captured per side, shape/presence and cached stability are separately asserted.
- `content_http_test.go`: authentication for all four routes, shared list/detail/diff worker, history omission, pending detection, preparation failure with secret-free output, absent receipt fallback, Sub-Store hosts invalidation, paging/search/redaction, missing Agents and page bounds. `FuzzContentPageQuery` covers the new argument parser. A13 adds no YAML parser entry; YAML depth-40 aliases and self-references remain covered at the delegated A12 entry points.
- Independent review records two Python credential-leak security corrections (F2/F3) in behaviour-changes.md. Native Manager ownership replaces Python's global repository-object switching, and typed immutable snapshots replace deepcopy mechanics. **Current limitation:** until A14 wires PrepareFunc, valid-baseline `content_sync` and push-diff requests return `unknown/check_failed`. A14 still owns real preparation and publishing/activation/receipt advancement.
- Coordinator-authorized gate deferral (2026-11-17): exactly 24 schema triplets in proddiff-baseline.txt carry `deferred → A14：PrepareFunc 未接线，接线后必须消失`; no wildcards or comparison-rule changes. `rt -- server/scripts/proddiff.sh --a13-proof` verifies the same snapshot-derived routes using real Python preparation: 21 HTTP cases, 3 real candidate bundles; all 24 differences are reproduced without preparation and resolved with injected immutable bundles. Whole responses compare, with independently generated timestamps/nonces checked for shape, one-to-one identity and cache stability. Prepared bytes stay in the server's protected temporary directory. The standard differential remains unchanged.
- Fixed gate defect: stale baseline entries previously printed a warning but did not fail. proddiff now returns failure for stale entries, enforcing the coordinator's `new=0 stale=0` requirement and A14 cleanup obligation. Initial proof setup lacked a launcher environment variable; fixed the harness before any successful comparison. No production response was normalized to hide a difference.
- O3-5 integration found a low-severity test-fixture isolation issue: earlier API tests now activate lazy shared jobs, so A13's Configure could return ErrRunning in the full package. The fixture stops the previous Runtime before installing its private Registry, retaining the production eight-second grace. Full api/agents then passed plain (33.894s) and race (169.821s); no production behavior changed.

Additional Python test mapping (all function names):

| Python test | Go coverage |
|---|---|
| `test_agent_content_api.py::test_both_services_compare_full_contents_and_hide_private_baselines` | TestContentHTTPHistorySummaryAndPending, TestContentChangesAgainstFlask; service-neutral Checker tests. Actual both-service publishing is A14. |
| `test_legacy_baseline_does_not_guess_from_revisions_or_fetch` | TestContentHTTPUnavailableProfileLegacyAndPreparation, TestInputProjectionAndPreconditions |
| `test_ready_does_not_create_baseline_and_successful_repush_clears_changes` | TestContentHTTPHistorySummaryAndPending, TestInputProjectionAndPreconditions; ready/successful publishing transition is A14 |
| `test_missing_profile_is_unknown_without_breaking_other_agents` | TestContentHTTPUnavailableProfileLegacyAndPreparation (stale route-adapter record; normal writes reject missing bindings) |
| `test_summary_listing_skips_content_comparison_and_hides_private_baselines` | TestContentHTTPHistorySummaryAndPending, TestContentReadsAgainstFlask |
| `test_summary_listing_leaves_out_the_history_and_never_copies_it` | TestContentHTTPHistorySummaryAndPending, existing Manager light-read tests; Go typed ownership replaces deepcopy assertions |
| `test_single_agent_and_full_listing_still_carry_the_history` | TestContentHTTPHistorySummaryAndPending, TestContentReadsAgainstFlask |
| `test_first_check_after_activation_sees_inputs_changed_since_preparation` | TestOldWorkersInvalidationAndReturnToOriginal, TestRequestSnapshotsAndRetarget; activation write side A14 |
| `test_listing_without_history_keeps_the_comparison_and_drops_only_the_history` | TestContentHTTPHistorySummaryAndPending, TestContentChangesAgainstFlask |
| `test_listing_without_history_still_sees_an_unfinished_deployment` | TestContentHTTPHistorySummaryAndPending, TestContentReadsAgainstFlask |
| `test_agent_push_diff_api.py::test_unpushed_and_legacy_agents_explain_the_missing_baseline` | TestContentHTTPAuthAndUnavailableStates, TestContentHTTPUnavailableProfileLegacyAndPreparation, TestContentReadsAgainstFlask |
| `test_synced_after_push_then_real_changes_are_listed_with_credentials_marked` | TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation, TestContentChangesAgainstFlask; initial push write A14 |
| `test_list_check_and_change_list_share_one_worker` | TestContentHTTPHistorySummaryAndPending, TestProductionTTLsAndNoQueue |
| `test_mosdns_without_saved_bundle_lists_files_by_digest` | TestContentHTTPMissingReceiptUsesDigests, TestPythonCorpus (A12 MosDNS corpus); real MosDNS publish A14 |
| `test_pending_deployment_and_failed_preparation_are_reported` | TestContentHTTPHistorySummaryAndPending, TestContentHTTPUnavailableProfileLegacyAndPreparation |
| `test_pull_token_is_shown_and_marked_and_stored_tokens_never_leave` | TestContentChangesAgainstFlask, TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation, A12 TestCredentialFieldsAndSpans |
| `test_every_node_and_rule_is_paged_and_searched_through_the_api` | TestContentChangesAgainstFlask, TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation; A12 node/paging corpus |
| `test_agent_push_diff_paging.py::test_the_first_page_is_listed_and_every_rule_kept_in_order` | A12 TestPagingBudgetsAndConcurrency, TestPythonCorpus; TestContentChangesAgainstFlask |
| `test_page_bounds_offsets_past_the_end_and_the_byte_limit` | A12 TestPagingBudgetsAndConcurrency; TestContentHTTPPageValidation, TestContentChangesAgainstFlask |
| `test_nodes_are_searched_by_name_info_and_field_but_never_by_credentials` | A12 TestStoredRedactionAndSearch, TestPythonCorpus |
| `test_rules_are_searched_without_credential_pieces_or_stored_tokens` | A12 TestStoredRedactionAndSearch; TestContentChangesAgainstFlask |
| `test_search_results_page_like_the_list` | A12 TestPagingBudgetsAndConcurrency; TestSharedDetailsExpiryPagingAndSearch |
| `test_kept_entries_are_bounded_per_file_and_per_comparison` | A12 TestPagingBudgetsAndConcurrency; TestDetailCountByteQuotasAndSyncedCleanup |
| `test_large_lists_are_kept_compact_and_read_back_quickly` | A12 TestPagingBudgetsAndConcurrency (100k entries), immutable compressed FileEntries |
| `test_pages_come_from_the_shown_change_list_and_expire_with_it` | TestSharedDetailsExpiryPagingAndSearch, TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation |
| `test_kept_lists_are_bounded_in_bytes_across_agents` | TestDetailCountByteQuotasAndSyncedCleanup |
| `test_agent_first_push_sync.py::test_first_push_without_a_node_cache_is_synced_right_away` | A13 read half: TestProductionTTLsAndNoQueue, TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation; real first-push/download/commit remains A14 |
| `test_first_push_delivers_the_subscription_hosts_with_policy_and_filter` | A14 preparation/publish; A13 observes invalidation via TestContentHTTPDiffPagingStoredRedactionAndSubstoreInvalidation |
| `test_a_push_with_a_current_node_cache_is_unchanged` | A14 publication/cache commit; A13 cache comparison covered by TestProductionTTLsAndNoQueue |
| `test_a_failed_download_without_a_cache_keeps_the_existing_error` | A14 preparation; A13 preparation error response covered by TestContentHTTPUnavailableProfileLegacyAndPreparation |
| `test_a_failed_download_keeps_the_earlier_cache_and_is_not_retried` | A14 preparation/Sub-Store cache interaction; A13 does not own downloads or cache writes |
| `test_rendering_again_asks_sub_store_once_per_uri_node` | A14 preparation; no production PrepareFunc in A13 by coordinator decision |

A12's seven deferred Checker tests map to TestSharedDetailsExpiryPagingAndSearch (shared worker and prepared list), TestDetailCountByteQuotasAndSyncedCleanup (synced cleanup, eviction and rebuilding), TestDetailFailuresRetryWithoutDiscardingStatus (short detail retry), and TestInputProjectionAndPreconditions (synced/unavailable skips listing). Deferred A01/A03/A15 GET privacy/history/localization cases use TestContentReadsAgainstFlask, TestContentChangesAgainstFlask, TestContentHTTPHistorySummaryAndPending and A24 TestPublicAgentPython; write-side failure/upgrade/deployment setup remains with the respective owner.

Final acceptance (2026-11-18, **.64**, `-p 4`, server gate-slot limit observed):

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'GATE_LOGS=/tmp/configflow-a13-complete-gate server/scripts/gate.sh -p 4 FuzzContentPageQuery:./internal/api/agents'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'server/scripts/proddiff.sh --a13-proof'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=server/tools/proddiff ~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p test_proddiff.py && bash -n server/scripts/proddiff.sh'
```

```text
gofmt -l internal/ tools/ cmd/       no output
go vet ./...                        PASS
configflow_proddiff/configflow_difftest: build ./..., vet + lifecycle tests PASS
full go test -race -p=4 -count=1 ./...: 87 packages PASS, no FAIL/data races
  agents 376.816s; api/agents 233.645s; deploy 39.031s; contentsync 1.966s; pushdiff 42.296s
baseline: new=0 stale=0
12 difftest packages PASS (api/agents 131.510s)
FuzzContentPageQuery ok  configflow/internal/api/agents 61.040s (60s fuzz)
GATE PASS
A13 real preparation: 21 HTTP cases, 3 actual bundles; all 20 deferred fields reproduced without preparation and resolved with it; PASS
Ran 34 tests in 0.029s — OK (proddiff harness); bash -n PASS
```

Gate logs: `/tmp/configflow-a13-complete-gate` on .64. Final rebase after the passing gate adds only `design/r12-release.md` (6 insertions/4 deletions), no compiled source or test change; the tested source is code-equivalent to `9bcd2cf3`. The real-preparation proof was repeated successfully on the final source copy. Per the updated Brief, the full race gate replaces a redundant full non-race run (a plain full suite had also passed earlier before the O3-5 integration). No new Python bug fixes. The original asynchronous 20 deferrals are superseded by the reviewer-approved 24 settled-response deferrals above; independent review acceptance is recorded in review-a13.md.

## A14 — publishing: transactional content, push-config, deployments, receipts

**Required merge-gate task from A13:** wire the real production PrepareFunc through `api/agents.SetContentPrepare`; remove all **24** `deferred → A14：PrepareFunc 未接线，接线后必须消失` lines from `parity/proddiff-baseline.txt`; rerun `server/scripts/gate.sh` and require `proddiff new=0 stale=0`. Do not keep or broaden these deferrals after wiring. A13 proved causality with real Python preparation on the identical production-snapshot route cases (`proddiff.sh --a13-proof`: 21 HTTP cases / 3 real bundles / 24 resolved schema differences). **24 条全部要消失**，包括新增的四条稳定后 checked_at；原20条必须仍逐项被证明，不得放宽。A14 also completes the six first-push write/download/cache-commit cases mapped in A13.

**Status: `missing`** · HR · prereqs: A11, A13, A01, C13 (`sub_store_client.get_subscription_proxies_yaml`), C17 (`provider_delivery.prepare/commit_provider_bundle`, `commit_cache_updates`), C18/C19/C20/C21/C22 (generators via `config_generator`), C12 (`node_conversions`), C23 (`oxidns_validation.require_valid_document`), C26 (`oxidns_device_policy.revision`), A19 (`gateway_settings`), routers (`router_split` in `_prepare_transactional_content`) · Go: internal/deploy, api/agents

**Contract additions from `server/design/a16-a18-sync-follow-ha.md` 1.3** (additive, agreed): export `deploy.RunDetached`, `deploy.WriteFileAtomic`, `deploy.Refusal` (counted by `IsPreparationFailure`), `deploy.Source` raw accessors/copy-on-write setters, and `deploy.CanonicalDigest` (decision D-A17-1, which would replace the Go-native `SettingsDigest` encoding); `lifecycle.Tracked`. Whichever of A14-x / A16-0 lands later adds them.

**Design:** `server/design/a14-publish.md` (Opus, 2026-11-17) covers the whole chain A11→A14 — package map, types, Agent protocol, state machine, locks, failure table, steps A14-1…A14-4, acceptance. Port to it; contract changes need the designer's or coordinator's agreement.

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

**Status: `verified`** (adversarial review 2026-11-16: 4769f418, 009f4997, be064848, 7dd36632 + the commit carrying this review block; porter: d8f4df19, 3d341e9c, c1df7eec; HR: VM lab test below due before the domain is done) · prereqs: A01, A02 · Go: internal/agents/upgrades (new), internal/api/agents/upgrades.go

Files:
- `backend/agents/upgrades.py` (310) → `upgrades/upgrades.go` (`Start`, `Poll`, `ReportLegacy`, `save`, `request`), `upgrades/artifacts.go` (`Arches`, `ArtifactDirectory`, `Artifacts`, digest cache; **A13 uses `upgrades.Artifacts()` for `upgrade_available`, A10 `ArtifactDirectory()` for the download route**)
- `backend/agents/address_follow.py` (221) → `upgrades/address.go` (`Usable`, `Follow`, `Confirmed`, `Describe`, `Change`, constants `HASteps`/`HAEdit`/`EnvEdit`/`RouterHint`, `RetrySeconds`)
- `backend/routes/agents.py` (214) — lines 1034–1049, 1050–1232, 2171–2185 → `api/agents/upgrades.go` (`registerUpgrades`, one line in `Register`); routes.tsv 15 rows `api/agents done`
- Python errors are `*upgrades.PyError{Class, Message}`; `IsValueError` also accepts `agents.DeploymentConflict`/`agents.ValueError`/repo ValueErrors (Python `except ValueError` catches AgentDeploymentConflict); `ErrorClass` for the report route's `(ValueError, TypeError, AttributeError)` → 400.
- Small exports added to `internal/agents` (`upgrade_support.go`: `AgentLabel`, `UpgradeBusy`, `NowTimestamp`, `ISOTimestamp`). A01's `ClearAgentLog(logPath, source any)`, `ValidateAgentLogPath(logPath any)`, `SetLoggingConfig(enabled any)` now take any JSON value (the routes pass request-body values unchecked; a truthy non-string `source` is sent as is).

Hooks/loops: `upgrades.Wire` (in `internal/server` `wirePackages`) registers `agents.Hooks{AddressFollow, AddressConfirmed}`. The follow check runs in a goroutine with `context.Background()` (Python daemon thread); attempts/pending-notice state are package globals like the module globals (`ResetAddressState` for tests). No loop.

Tests ported:
- `test_agent_upgrades.py` 12/12 → `upgrades/upgrades_test.go` (`requestHook` replaces `_request`, `SetArtifactsForTest`).
- `test_agent_artifacts.py` 16/17 → `upgrades/artifacts_test.go` (Path.open/readinto interception = `openHook`/`readHook`). Not ported: `test_windows_streams_without_reusing_or_storing_digest_cache` (os.name == 'nt' branch; the Go server does not run on Windows).
- `test_agent_address_follow.py` 23/23 → `api/agents/address_test.go` (21, inline thread + `SetReachesForTest`/`SetAskForTest`) and `upgrades/address_test.go` (`_reaches`/`_ask` against loopback fake Agents instead of monkeypatched requests).
- `test_agent_upgrade_auth.py` 9/11 defs → `api/agents/upgrades_test.go` (`newUpgradeApp` = `authenticated_app`, login enabled). Deferred to **A10**: `test_agent_binary_download_does_not_require_admin_jwt` (3), `test_public_download_keeps_filename_allowlist`, and the download half of `test_admin_starts_upgrade_and_both_agent_protocols_can_download`; to **A13**: GET `/api/agents` / `/api/agents/<id>` parts of `test_admin_can_still_read_agents_and_upgrade_status` and of `test_upgrade_exemptions_do_not_open_agent_administration` (`GET ""`, `GET /{id}`); to **A14**: push-config parts of the exemptions and of `test_reregister_preserves_upgrade_record_and_pending_operation_lock`. `authenticated_app` for A18/A20: copy `newUpgradeApp` (it lives in the external test package).
- Deferred from A02, `test_agent_upgrade_messages.py` 7 route cases: 5 ported (`TestRejectedUpdate…`, `TestLiveRollbackFailure…`, `TestLegacyMigrationFailure…`, `TestMissingAgentAndUpdateRecordErrors…`, progress + own-read part of `test_list_detail_and_status_localize…` ×3 as `TestStatusLocalizesOldRecordsWithoutRewritingThem`); list/detail part → **A13**.
- New: `TestReportBodyLimitAndMalformedReports`, `TestLatestVersionRoute`, `TestReferenceUsable` (49 inputs from real `address_follow.usable`, `testdata/a15_reference.json`; the same corpus records CPython's TypeError/AttributeError texts used by the routes).

Differential (`difftest/upgrades_diff_test.go`): `TestUpgradesDifferentialAgainstFlask` — 98 requests over all 15 routes (success and failure each: unknown Agent, unauthenticated, busy update, unreachable Agent, Agent 500/404/non-JSON/list/string/null answers, odd request bodies, Unicode/huge `lines`, full update → poll → succeeded, legacy protocol, rejection, report variants, address describe/change/HA/docker, heartbeat follow adopted / HA member reported / token refused / bridge address ignored), fake Agent on 127.0.0.1 and on this host's LAN address (same port) so both servers follow the same way; compared bodies, Agent requests, stored Agents, notifications.json, DATA_DIR files: **0 differences**. `TestUpgradesProxyDifferentialAgainstFlask` (coordinator decision 5): HTTP_PROXY → loopback recording proxy; logs/sources/restart/address check/follow go through it, upgrades `_request` (trust_env=False) does not: **0 differences**. Registry differential re-run: 234 requests, 0 differences.

Differences / accepted (for the reviewer):
- Transport error texts in `Connection error: …` / `error_details` are Go's (A01's accepted limitation); masked in three difftest cases.
- `.oxidns-ha-publications/<hash>/lock` files that Python's `oxidns_ha_publish.service_operation` creates on restart/uninstall are A18's (the default service operation has no pair lock); excluded from the difftest file list.
- `poll` on a truthy non-dict `latest_upgrade`: Python's `dict(x)` raises ValueError (str → 404 with that text) or TypeError; Go answers a TypeError (500). Malformed record only.
- Windows digest-cache branch not ported (see tests).

Pitfalls: `Usable` ignores loopback, so a follow test needs a non-loopback local address (the difftest binds the fake Agent on the host's LAN IP; it skips if none). `upgrades` tests share globals: `ResetArtifactCache`, `ResetAddressState`, restore functions of the `Set…ForTest` seams. The difftest fake Agent tells requests and Go apart by User-Agent to echo each server's own update id.

Commands:
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... ./internal/server/ && go test -count=1 -race ./internal/agents/... ./internal/api/agents/... ./internal/server/'` — all ok
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run UpgradesDifferential ./internal/api/agents/difftest/'` and separately `-run UpgradesProxyDifferential`
- corpus: `rt --back server/internal/agents/upgrades/testdata/a15_reference.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/upgrades/testdata/gen_a15_reference.py > server/internal/agents/upgrades/testdata/a15_reference.json'`

Review (adversarial, 2026-11-16) — every function and branch of upgrades.py, address_follow.py, the A15 routes of routes/agents.py and the manager methods they call (restart/status/logs/sources/clear/validate/logging/uninstall) walked against Go; new references from real CPython 3.11 / requests 2.31 / Werkzeug on the test server (`upgrades/testdata/gen_a15_review.py` → `a15_review.json`: 50 poll records, 20 ISO times × 5 zones, Bearer bytes of 6 tokens × 3 call sites, 25 `lines` values, pathlib permission errors, CPython error texts). Findings, all fixed:

| # | Sev | Finding | Fix | Regression test |
|---|---|---|---|---|
| 1 | low | porter's difference "non-dict upgrade record → 500" — observable: poll's `dict(latest_upgrade)` raises ValueError for a str (404 with its text), TypeError for int/float/bool, and reads a list as key/value pairs (`[["status","succeeded"]]` answers 200; bad elements ValueError/TypeError by index) | 4769f418 (`pyDict`) | `TestReviewPollReference` (19 malformed records), difftest "review upgrade string/pairs/int record" |
| 2 | low | Bearer header was sent as UTF-8: http.client sends latin-1 (`tök` → byte 0xF6) and raises UnicodeEncodeError (a ValueError, not a RequestException) for characters above U+00FF: start answers 409 "无法连接…", poll falls to the deadline, `_follow` only logs (Go reported "连不上新地址" + notification), describe/change and the operation routes raise (500 / 500 JSON with the codec text) | 4769f418 (`Request.Latin1Headers`, `agents.EncodeHeaderLatin1`, `CallError`) | `TestReviewLatin1Tokens` (raw header bytes vs Python), `TestA15ReviewCallRaisesLikePython`, difftest "review * latin-1/cjk token" |
| 3 | low | operation routes on a record without host/port/token: Python KeyError → 500 `{"message": "'host'"}` before any request (also before requests' NaN InvalidJSONError); Go called `http://None:…` and answered "Connection error" | 4769f418 (`agentKeys` in `Call`/`simpleCall`, non-transport errors propagate in restart/uninstall/simpleCall) | `TestA15ReviewCallRaisesLikePython`, difftest "review * without host" |
| 4 | low | `Path.exists()`/`is_file()` (Python 3.11) raise stat errors other than ENOENT/ENOTDIR/EBADF/ELOOP; with an unsearchable artifact directory Python answers 500, Go said "更新文件未准备就绪" (409) | 4769f418 (`ignoredStatError`, `ArtifactDirectory() (string, error)`, called per arch like Python) | `TestReviewArtifactStatErrorsAreRaised` |
| 5 | low | `datetime.fromisoformat(naive).timestamp()` raises ValueError when CPython's `local_to_seconds` needs a local time outside years 1–9999 (first day of year 1 in every zone; last hours of 9999 east of UTC, e.g. Asia/Shanghai from 9999-12-31T16:00): poll's recovery/deadline checks then skip the heartbeat; Go compared a timestamp (9999 heartbeats → "更新未生效" instead of "unknown"). Same in `runtime_state._timestamp` (A02 code, -inf) | 4769f418 (`ISOTimestamp` range replay), be064848 (`timestampOf`) | `TestA15ReviewISOTimestampYearRange`, `TestA15ReviewRuntimeTimestampYearRange` |
| 6 | low | start read the snapshot's previous record before the reservation; Python reads it inside `reserve()` after the DNS HA check (busy pair + malformed record: 409 HA message, Go 500) | 4769f418 | `TestReviewStartReadsPreviousRecordInsideReservation` |
| 7 | low | decision 8: `lines` used `httpx.PyParseInt` (Go Unicode tables: Kawi/Nag Mundari digits of Unicode 15 accepted, Python 3.11 refuses → 100) — `argBigInt` (shared with A04/A05) now `pystr.Int` | 4769f418 (`pyArgInt`) | `TestA15ReviewArgIntLikeWerkzeug`, difftest "review logs Unicode 15 digit / zeros over digit limit" |
| 8 | cosmetic | TypeError texts of `started_at + 600` / `confirmed > recovery_reported_at` (log of the 500) | 4769f418 | `TestReviewPollReference` ("started_at str/list/none/dict", "recovery str/list") |

Porter's differences, decided: Go transport texts — accepted (A01 limitation; only inside "Connection error: …"/`error_details`). `.oxidns-ha-publications/<sha256>/lock` files of restart/uninstall — accepted for now, A18 must create them in `service_operation` and then drop the exclusion in `upgrades_diff_test.go`. Non-dict record — fixed (finding 1). Windows digest-cache branch — accepted (server never runs on Windows).

Accepted differences (reviewer): a list-of-pairs record with a non-string key is a TypeError at `dict()` (Python builds the dict, but every later step fails: KeyError on 'status' or jsonify cannot sort mixed keys — 500 either way, only the log line differs). `~/.netrc` entries (requests with trust_env replaces the Bearer header with Basic auth for `_reaches`/`_ask`/manager calls) are not read: the image has no .netrc. Redirects of Agent answers follow Go's rules (requests 30 hops, auth stripping on host change in both). Shared-code changes made here: `agents.Call` (KeyError/latin-1, used by A01/A14/A18 paths too: an unencodable token now fails before sending, as in Python), `HTTPClient.Do` `Latin1Headers`, `api/agents.argBigInt` (A04/A05 routes now use pystr).

Checks performed: state machine of start/poll/_save/report_legacy (terminal sets, only_queued, ownership, agent_updated_at ordering, event merge, rollback_failed polled, deadline 600 s with heartbeat-after rule, protocol 1/2/True/2.0/3), arch selection (unknown/missing/non-string arch, legacy needs all three, stuck `queued` reservation after KeyError like Python), artifacts (stat-keyed cache, ELF magic, size limit, change detection, per-arch directory), 4096 B report limit and token-or-JWT on the status route, every operation route's body handling (difftest), address follow (Usable corpus 49 + IPv6/zone/bridge/full-width via difftest, same-name records, HA notify-only with HA_STEPS, retry window per (host, member), confirmed_host, describe/change in modes agent/env/record/docker/404/unknown/ha, rejects 400/502, notifications and resolution), CPython error texts. Differentials re-run: `UpgradesDifferential` 130 requests (98 + 32 review cases), 0 differences; `UpgradesProxyDifferential` 6, 0; registry `TestDifferentialAgainstFlask` 234, 0.

Pitfall (difftest): the Go server runs in the test process, so the follow attempts of one differential survived into the next (retry window → no follow in the proxy case when both ran in one `go test`); both tests now call `upgrades.ResetAddressState()`.

Open findings: none.

VM lab test (HR, after the maintainer agrees; PVE lab only): (1) web update of a shell Agent (systemd and OpenRC) from the previous release to the current artifacts: start → poll through downloading/verifying/replacing/restarting → succeeded with version + migration_ready; (2) forced failure (corrupt artifact SHA, migration failure) → rolled_back / rollback_failed and the lock released, retry works; (3) legacy protocol-1 Agent (no /api/upgrade-info) update + report; (4) Docker Agent refused; (5) Agent unreachable mid-update → checking/unknown → recovers on heartbeat after the 600 s deadline; (6) address follow on a real renumbering (DHCP change of the Agent VM): heartbeat with new address → verified with the token → record updated, notice shown, confirmed_host written back; an HA member renumbered → notify only with the steps; the 修改地址 dialog in agent mode (writes agent_ip) and record mode.

Review commands (private detached worktree of HEAD because the shared tree did not build — others' convert WIP):
- `rt -- bash -lc 'cd server && go vet ./internal/agents/... ./internal/api/agents/... ./internal/server/ && go test -count=1 -race ./internal/agents/... ./internal/api/agents/... ./internal/server/'`
- `rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v -run "UpgradesDifferential|UpgradesProxyDifferential|TestDifferentialAgainstFlask" ./internal/api/agents/difftest/'`
- references: `rt --back server/internal/agents/upgrades/testdata/a15_review.json -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/upgrades/testdata/gen_a15_review.py > server/internal/agents/upgrades/testdata/a15_review.json'`

## A16 — Agent rule sync, projection, snapshot

**C32 hook contract (coordinator approved): A16 必须注册 PinnedWithheld hook，注册后 C32 的 pinned_withheld 字段恢复。** `agents.Hooks.PinnedWithheld(ctx, profileID, ruleIDs) (bool,error)` owns the baseline/projection calculation. C32 returns false when no eligible auto-syncing OxiDNS exists; when projection is required but unavailable/fails, it logs one redacted warning and omits only this best-effort field after the save. Deferred → A16; do not copy projection into config routes.

**Status: `missing`** · HR · prereqs: A07 (`mihomo_panel.call`, `oxidns_rule_update`), A06 (`MonitorError`), A14 (`rule_sync_store`, transactional content), A11, C18/C22 (generators, `get_oxidns_ruleset_downloads`), C25 (`oxidns_common_routing._analyze/_references/apply_routing`, `dns_watchdog`), C27 (`rule_dns.profile_agents` and projection helpers), C05 (`OxidnsSettingsError`), C14 (`is_internal_rule_url`) · Go: internal/agents/rulesync (new), api/agents

**Design:** `server/design/a16-a18-sync-follow-ha.md` (Opus, 2026-11-17) covers A16, A17 and A18 — package map, stored types, Agent protocol, loops, locks, failure/rollback, steps A16-0…A18-4, acceptance (fake pair, Python transcript differential, F1 closure, L3). Port to it; contract changes need the designer's or coordinator's agreement. Open maintainer decisions: D-A17-1, D-A18-1.

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

**Design:** `server/design/a16-a18-sync-follow-ha.md` (Opus, 2026-11-17) covers A16, A17 and A18 — package map, stored types, Agent protocol, loops, locks, failure/rollback, steps A16-0…A18-4, acceptance (fake pair, Python transcript differential, F1 closure, L3). Port to it; contract changes need the designer's or coordinator's agreement. Open maintainer decisions: D-A17-1, D-A18-1.

Files:
- `backend/agents/dns_follow.py` (730)
- `backend/routes/agents.py` (30) — lines 162–191

Tests: `test_dns_follow.py` (70)

Routes (4): `GET|PUT|DELETE /api/agents/<agent_id>/dns-follow`; `POST /api/agents/<agent_id>/dns-follow/retry`

Evidence: no Go code.

Hooks to provide: `agents.SetDNSFollowRow` (dns_follow row; with A16's row this completes the `agent` task slot), `tasks.SetRunner("dns_follow", dns_follow.retry)`, `agents.OnManagerInit` (`dns_follow.start_loop`, registered first: Python order dns_follow → HA publication → IPv6 verification). `settings_digest` is used by A14 `_remember_deployment_content` (export it; A14 needs it → port the digest helper in A14 or give A14 a hook).

## A18 — DNS HA

**Status: `missing`** · HR · prereqs: A16 (`prepare_candidate`, `settings_for`, `fetch_snapshot`), A17, A14, A11, C26 (`prepare_editor`), C22 (`load_native_yaml`), routers (`routers/store.routers_of`) · Go: internal/agents/dnsha (new), api/agents

**Design:** `server/design/a16-a18-sync-follow-ha.md` (Opus, 2026-11-17) covers A16, A17 and A18 — package map, stored types, Agent protocol, loops, locks, failure/rollback, steps A16-0…A18-4, acceptance (fake pair, Python transcript differential, F1 closure, L3). Port to it; contract changes need the designer's or coordinator's agreement. Open maintainer decisions: D-A17-1, D-A18-1.

Files:
- `backend/agents/oxidns_ha.py` (625)
- `backend/agents/oxidns_ha_handoff.py` (167)
- `backend/agents/oxidns_ha_publish.py` (676)
- `backend/routes/agents.py` (61) — lines 192–215, 1233–1269

Deferred from A09: `test_docker_ha_guide.py::test_live_environment_requires_admin_and_never_exposes_extra_payload` (`oxidns-ha-environment` route).

Tests: `test_oxidns_ha.py` (37), `test_oxidns_ha_publish.py` (28), `test_oxidns_ha_docker.py` (9), `test_oxidns_ha_unchanged.py` (6), `test_oxidns_vip_handoff.py` (13), `test_oxidns_ha_identity.py` (8), `test_oxidns_ha_compact_identity.py` (7), `test_oxidns_ha_guards.py` (3)

Routes (4): `GET|PUT|DELETE /api/agents/<agent_id>/oxidns-ha`; `GET /api/agents/<agent_id>/oxidns-ha-environment`

Evidence: no Go code; pure pieces copied in `internal/agents/compat.go`.

Hooks to provide: `agents.RegisterHooks{HAServiceOperation, HADeployment, HAActivate, PublishDeployment (publish wrapper around A14's function)}`, `agents.OnManagerInit` (`oxidns_ha_publish.start_loop`), A07's HA rule-update hook; then delete `agents/compat.go` and switch the registry to the real functions (pair lock file included).

## A19 — Linux gateway core

N7-4 follow-up (coordinator-approved): `TestGenerateWithAliasChain` retains a
small shared graph success check; its 16-level binary graph now requires
`ErrAliasBudget` and empty output. The native writer applies the same expansion
budget as Mihomo, which could not read that old compact-but-expansive output.
No budget relaxation; editor/selection checks remain.

**Status: `verified`** (Opus porter, 2026-11-17; HR review by a fresh Opus (O4) 2026-11-17, `parity/review-a19.md`: F1 medium, F2/F3 low, all fixed, 0 open; lab batch L4 still due) · HR · prereqs: C05 (`append_url_query` = existing `convert.AppendURLQuery`), C08 (`rulefetch.RuleCacheReadPath`), C06 (`auth.IsAuthEnabled`), C32 (`get_ruleset_content`, hook below), routers (`routersplit.CheckGeneration`) · Go: `internal/gateway` (+ existing leaf `internal/gateway/gwbase`)

Files:
- `backend/common/linux_gateway.py` (518) → `internal/gateway`: `settings.go` (Settings, NormalizeSettings, Error), `config.go` (typed view: Config, Rule, Reason), `extract.go` (content lines, ExtractCIDRs), `fakeip.go`, `provider.go` (ProviderName/Path/Payload/YAML, SourceYAML, ContentLoader), `apply.go` (Apply, RoutingMarkPath), `overlap.go` (DirectOverlapWarnings), `editor.go` (EditorState, ValidateRuleSetIDs, ValidateSelection), `node.go` (value predicates on the shared yamlx node helpers), `adapter.go` (the only jsonx/legacy-yamlx file), `wire.go`, `messages.go`. 1866 non-test lines; tests 754 + `testdata/gen_a19.py` 638.

Commits (branch `go-port-a19`): `7511b705` core + `server.wirePackages` line, `a1b7f81f` corpus/boundary tests/fuzz, `fad46113` router_split + base-settings rows, `eba72710` shared yamlx node helpers (N7-1), `bb90a277` downloads rows, plus this PROGRESS commit.

Routes: none (A20 owns the 18 gateway routes). Wiring: `gateway.Wire` in `server.wirePackages` registers `convert.MihomoHooks{ApplyLinuxGateway, GatewayEnabled, FakeIPNetworkError}` (C18's declared interface, decision 3); `IsFullTun` stays with routers (`ippresence.Wire`).

### Public API (for C18, A11, A20–A22, C36, routers)

Documents are `*yaml.Node` (`yamlx` conventions: content nodes, no document wrapper). The profile configuration enters through the adapter until N8:

- `NewConfig(*jsonx.Object) *Config` — repo's compat config (profile + `rule_library` + `system_config` + `nodes`); immutable; `SettingsOf(*jsonx.Object) (Settings, error)` reads only the setting (cheap, for hot paths).
- `(*Config).Settings() (Settings, error)` = `gateway_settings` (malformed stored value → `*Error`); `Enabled()`; `ProfileID()`; `RuleSet(id) *Rule` = `_rule`; `Library(*Rule) *Rule` = `_library`; `Reason(*Rule) string` = `_reason`; `BaseDocument() (*yaml.Node, error)` = `_base_config`.
- `NormalizeSettings(json.RawMessage) (Settings, error)` = `normalize_gateway_settings` (A20 request bodies). `Settings{Enabled bool; RuleSetIDs []string}` marshals as `{"enabled", "rule_set_ids"}` (never null).
- `ParseFakeIPRange(string)`, `FakeIPNetwork(*yaml.Node) (netip.Prefix, error)` = `fake_ip_network`.
- `ExtractCIDRs(content, behavior) ([]netip.Prefix, error)` = `extract_gateway_cidrs`.
- `(*Config).ProviderPayload / ProviderYAML / SourceYAML(ctx, ruleID, ContentLoader)` = `gateway_provider_payload/_yaml`, `gateway_source_yaml`. `ContentLoader func(ctx, item, library *Rule) (string, error)`; nil = default reader (below). **A11** passes a loader that reads its own publication snapshot (`Rule.URL()`, `Rule.JSONX()` give the declaration) and maps `*Error` to `DeploymentPreparationError(str)`.
- `ProviderName(id)`, `(*Config).ProviderPath(id, source bool)` (A11 matches internal callback paths against it), `ProviderPrefix`, `FakeProvider`, `RefreshInterval`, `MarkerKey`, `DefaultFakeIPRange`.
- `(*Config).Apply(doc *yaml.Node, baseURL)` = `apply_linux_gateway` (runs `routersplit.CheckGeneration` first); `ApplyJSONX(mihomo, config *jsonx.Object, baseURL)` is the convert hook (patches in place only on success; off = no conversion at all).
- `RoutingMarkPath(*yaml.Node, root)` / `RoutingMarkPathJSONX(any, root)` = `_routing_mark_path` (**A11**'s provider-file check).
- `(*Config).DirectOverlapWarnings(ctx, ContentLoader)`, `EditorState(includeGuidance) (EditorState, error)` (`Candidate`, JSON = Python's dict; missing selections omit `description`), `ValidateRuleSetIDs(ids, previous []string) error`, `ValidateSelection(json.RawMessage) (Settings, error)` (**A20** draft, **A21** onboarding, **C36** setup flow).
- Errors: `gateway.Error` = `gwbase.LinuxGatewayError` (user message); `routersplit.RouterSplitError` also matches it with `errors.As` (check RouterSplitError first for its code/status).
- Hook for **C32**: `SetRulesetContent(func(ctx, item, library, config *jsonx.Object) (string, error))` = `routes.rules.get_ruleset_content(item, library, config_data=config)` ("" when unreadable). Until set, default reads fail closed with `ErrRulesetContentUnavailable` (only provider routes/overlap with remote sets and no loader need it; generation does not). With the reader set, `ProviderPayload/SourceYAML` accept an empty result only when `rulefetch.RuleCacheReadPath` names an existing regular file (Python's `is_file()`), else `*Error` 无法读取.
- No locks, goroutines or background work (o3-concurrency: nothing to register). Functions that may read rule content take `ctx`; callers that write state detach per PORTING 4b.

### Decisions and findings

- **Typed view instead of dicts.** Stored members are read as absent / string / other (other reads as "" — model rule R5) and flags as absent / JSON truthiness, so `.get(k, default)` and `enabled: null` (= off in Python) behave as in Python. Non-string ids/names/library ids that Python would compare or `str()` are not matched (malformed data only).
- **Mihomo documents as yaml.Node.** The convert hook converts the generated jsonx document to nodes (only when the gateway is on), patches, and writes back; untouched scalars come back as the original Go values (number forms, timestamps, binary), the TUN/DNS copies share scalars (`cloneShared`). Proven by `TestApplyKeepsUntouchedValues` and exact `jsonx.Compact` equality (order + types) in every `apply` corpus row.
- **User YAML** (base config, YAML rule sets) is still read with the legacy `yamlx.LoadWith` (PyYAML port, non-string keys and recursive aliases kept) so every reader of the same text agrees until N7-3 migrates readers; provider files are written with `yamlx.SafeDumpUnicode` (PyYAML bytes). Line splitting uses the shared `convert.PySplitlines` (`\v`, `\f` split as in Python — corpus row).
- **Python bug fixed:** a scoped IPv6 /128 in an original rule set (`fe80::1%eth0`) was written to the source provider as `fe80::1%eth0/128`, which Mihomo cannot parse; Go writes `fe80::1/128` (severity low; only scoped link-local entries).
- **No longer chased:** IPv6 text forms in source providers (`::ffff:1.2.3.4` vs Python `::ffff:102:304`; same network); `str.strip`/`upper`/`\s` Unicode edge cases; Python crash classes (AttributeError for a non-mapping custom provider, ValueError for a bad bracketed server_domain) are Go errors that are not `*Error` (both 500); invalid UTF-8 content (impossible in Python) is refused as binary.
- **For A20 (routes):** Python routes return `str(LinuxGatewayError)` as 400 and `RouterSplitError` with its own status/code — map `errors.As` in that order.

### Python tests → coverage

A19 has no test file of its own. Oracle: `testdata/gen_a19.py` runs the real Python functions (7494 rows since the review, synthetic values, loaders instead of the network; `auth` patched) and `TestPythonCorpus` compares every row (Chinese messages exact, RouterSplitError by code, crash classes as "refused but not a gateway error", configs must stay unmodified): normalize 31, fake_ip_range/network 57, routing_mark 14, extract ~1850 (fixed + random), provider payload/yaml/source ~610 each, provider_path/name ~1800, overlap ~310 (with loader call order), editor_state ~310, validate_ids ~270, validate_selection ~290, apply ~380 (byte-exact jsonx), generate 134 (full `generate_mihomo_config` vs `convert.GenerateMihomoConfig` with the real hooks), downloads 134 (`get_mihomo_ruleset_downloads`).
- `test_linux_gateway.py` (27; file owned by A20): A19 halves covered by corpus rows built from the same inputs — empty-cache rename → `TestDefaultContentReader`; disabled no-op/no mutation → generate/downloads/apply rows + `TestApplyKeepsUntouchedValues`; selective output, fake-only/deleted, rename/disable, classical extraction, unsafe rules, conflicts on the base, candidates/MRS, comments, single IP, classical non-IP, rule mode, TUN filters, disabling after type change, huge sources, auth token, overlap (2), router split markers (2) → the matching fixed rows. Routes/bundle/draft cases (`test_api_*` 2, `test_runtime_tokens_*`, `test_bundle_*` 2, `test_draft_preview_*`) → A20/A11.
- `test_router_split.py::test_rule_set_id_validation_is_shared_with_gateway_selection` → validate_ids/validate_selection fixed rows.
- `test_mihomo_base_settings.py` fake-ip-range cases (12) → fake_ip_range rows + `TestHooks` (through `FakeIPNetworkError`); the integer value is C18's own check.
- `test_setup_flow.py`, `test_linux_gateway_draft.py`, `test_gateway_onboarding.py`, `test_gateway_publish_compatibility.py`, `test_gateway_installation.py` exercise A19 only through A20/A21/A22/C36 → their modules.
- Extra: `TestSettingsDefaults` (raw JSON forms), `FuzzExtractCIDRs` (capture invariants: IPv4, ≥/8, sorted, disjoint, never all of IPv4). Purpose-broken check: flipping `ipv6`, the /8 limit and the overlap sort made 249 rows differ.

### HR review (O4, 2026-11-17) — verified

`parity/review-a19.md`. **F1 (medium)** shared YAML containers (aliases) were expanded by the jsonx
boundary, `cloneShared` and `RoutingMarkPath` — exponential generation/editor/validation time and
memory for an alias chain in the base configuration or a third-party provider file (A11's routing-mark
check), endless recursion on a cyclic node graph; now one node per shared container and Python's
`seen` set. **F2 (low)** `Apply`/`RoutingMarkPath` silently did nothing on a yaml.v3 document wrapper
or a non-mapping; the wrapper is transparent, anything else an error. **F3 (low)** non-ASCII white
space around `payload:` made a classical set's capture silently empty (Python reads YAML); the
detection takes Unicode white space, 62 corpus rows added (7494). New tests for the content limits,
all TUN filter keys and scalar tun/dns. Debt: N7-3 (`LoadWith` vs convert's `Load` acceptance),
N3/N8 (`PySplitlines`, `jsonx.Str`), N9 (`PythonClass` in gwbase); C32 must install
`SetRulesetContent` before A20's provider routes ship. C32 now installs it via `api/rules.Wire` in server startup: validated cache → inline → bounded C14 download, using the passed immutable publish snapshot. `server.TestNewAppWiresRuleContentForGatewayProviders` proves default loader availability and canceled-caller completion; no gateway logic is copied.

### Only lab batch L4 can prove

The corpus proves the generated Mihomo configuration; it cannot prove what the host does with it: TUN auto-route/auto-redirect nftables rules capturing exactly fake-IP + selected IPv4 networks (and nothing else) on a real Linux gateway; `route-address-set` updates when Mihomo refreshes `/linux-gateway/rules/<id>` every 300 s with the token (and keeps the old provider when the panel answers 503); `routing-mark` conflict relevance; fake-ip DNS on 127.0.0.1:1053 with OxiDNS upstreams via IP DoH not hijacked; IPv6 off end to end; clients on the LAN routed through the gateway (selective, full_tun via router split, legacy) and persistence across reboot — all A20–A22 host operations.

### Commands

- Corpus: `RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/gateway/testdata/a19_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/gateway/testdata/gen_a19.py'`
- Tests: `rt -- bash -lc 'cd server && go vet ./internal/gateway/ && go test -count=1 -race ./internal/gateway/'`; fuzz `go test -run x -fuzz FuzzExtractCIDRs -fuzztime 60s ./internal/gateway/`.

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

Independent adversarial review **verified**, **0 open**, branch `go-review-a24`, fix `c106af0d` (base `642e84db`, rebased onto `go@9a208c6c`). F1 high: legacy/public metadata credentials and unlabelled environment registration key leaked; shared redactor now masks them. F2 high: stopped-Runtime toggle/cleanup still wrote; all six write owners now check cancellation after Detach. Seven privacy cases, two HTTP exits, six stopped-Runtime owners, 24 real Python public-Agent records, complete Flask/storage and C37 guard regressions pass. Final `.64` queued `gate.sh -p 4`: gofmt/vet/two tags PASS, full race 85 tested packages / 95 entries PASS, 12 enabled difftest PASS, eight 60-second fuzz PASS, proddiff new=0/stale=0 and bundle 3/3 (no partial). Environment-key delta: `.32` consumer race + real Python PASS. After O3-5 rebase, `.64` C37/deploy/pushdiff/redact race and enabled A24 HTTP/Python regressions PASS. Exact commands, output tails, severities and accepted Python safety correction: [review-a24.md](review-a24.md). No sub-agents, pushes or production access.

**Status: `verified`** · prereqs: A05, C09 (`connection_rules`, ready), C26 (`oxidns_device_policy.read_policies/revision/normalize_client_range`), C31 (`device_notes`), C37 (`domain_discovery_service`, `provider_revisions`), C05 (`OxidnsSettingsError`) · Go: api/agents

Files:
- `backend/routes/client_tools.py` (105) — registers on the agents blueprint (`agents.*` endpoints)
- `backend/routes/agents.py` (93) — lines 751–780, 2326–2388

Tests: `test_client_tools.py` (15)

Routes (8): `GET|PUT /api/agents/<agent_id>/client-notes`; `GET /api/agents/<agent_id>/mihomo/rule-targets`; `POST /api/agents/<agent_id>/mihomo/rule-targets/apply`; `PUT /api/agents/<agent_id>/domain-discovery`; `POST /api/agents/<agent_id>/domain-discovery/clear`; `GET|PUT /api/agents/<agent_id>/oxidns/device-policies`

Implemented all eight methods in `api/agents/client_tools*.go`; exact method checks preserve read-only HEAD. Literal rule-targets registration/dispatch is exercised with the existing A07 catch-all present. Writes detach in their owner (`clientTools` and manager.SetDomainDiscovery), never in handlers. JSONx is confined to the legacy adapter; notes, policies, rule eligibility/cache and discovery semantics delegate to shared owners.

Commits: `78b796af` shared public Agent helper (A13 notified), `c87983b5` C37 guard/query. HTTP/storage differential `TestA24FlaskBoundaries` exercises every route's success/failure, admin-vs-Agent auth, binding isolation, previous-name conflicts, settings/notes revision conflicts, canonical CIDRs, duplicate/domain rejection, cached rule files, and cleanup preserving traffic_history. After every call it compares complete exported DB documents plus all JSON/YAML files. Guard/cancellation/runtime tests: `TestClientToolsRejectStaleBindingsAndNames`, `TestClientNoteAndPolicyCancelledOwners`, `TestClientDiscoveryReturnsRuntimeOverlay`; C37 guard rollback/pass-equivalence tests. `TestPublicAgentPython` checks ten real Python field-by-field full/unread records with secrets, deployment, upgrade, HA and future fields. Domain parser fuzz target: `FuzzClientConnectionEntries`.

Accepted formatting class: generated `native_config` / `template_base_config` YAML is compared structurally. Its settings revision may change with YAML formatting; each response revision is checked against that side's exact stored settings, and subsequent edits use that side's issued revision. No semantic field is ignored. Only nondeterministic created_at/updated_at/applied_at strings are normalized. C09's legacy validation errors are translated to repo validation errors (400) at the API boundary.

Python test mapping (all 15 functions retained through route/storage checks or existing shared-owner boundary suites):

| `backend/test_client_tools.py` cases | Go coverage |
|---|---|
| shared notes with DNS; hidden legacy names; template-preserving name edits | `TestA24FlaskBoundaries`, `TestDeviceNotesBoundary` |
| policy removal preserves notes; removed/hidden legacy rows and capacity | `TestA24FlaskBoundaries`, `devicenotes.TestLegacyNamesAndPolicyPlan`, real Python `TestDeviceNotesBoundary` capacity/removal cases |
| names renamed elsewhere; unrelated notes do not invalidate editor; required names revision; empty new-row name inherits latest name | `TestA24FlaskBoundaries`, `TestClientToolsConcurrentEditors` |
| bound validation/conflict | `TestA24FlaskBoundaries`, `TestClientToolsRejectStaleBindingsAndNames` |
| note clone/export roundtrip | existing C31 `repo.TestProfileCloneDoesNotCopyRouterAccess` (asserts retained device_notes), shared notes storage/differential; A24 changes no clone/import code |
| bound defaults / duplicates; six non-domain values; defaults change at write lock | `TestA24FlaskBoundaries`, `TestClientToolsRejectStaleBindingsAndNames/domain-default`; C37 `TestApplyDomainsTransactionGuard` proves callback runs with the transaction context before any write, `TestApplyDomainsGuardPassMatchesDefault` checks unchanged passing behavior |
| admin auth | `TestA24FlaskBoundaries` all eight methods reject no JWT / Agent bearer |

Additional Python files: `test_oxidns_device_policy.py` API binding/stale/duplicate/admin cases → `TestA24FlaskBoundaries` plus concurrent writers; remaining algorithm tests belong to C26. `test_agent_runtime_state.py` discovery switch/live overlay → `TestClientDiscoveryReturnsRuntimeOverlay`; discovery report switch behavior remains in A05 traffic/difftest. A24 adds no Agent controller calls or publication paths.

Legacy label clarification: Python Agent registration/edit accepts arbitrary JSON names. Policy peer labels must preserve nonempty stored JSON values (and use ID for empty ones), rather than narrowing them to string. The adapter uses json.RawMessage; real HTTP differential includes number, null, bool, empty list, object and nonempty list labels.

Final integration acceptance on **go d3124353** (N7-3b shared reader/append APIs included), .64: complete gofmt/vet PASS; six related race packages PASS (api/agents 99.942s, agents/difftest 188.201s, discovery 8.590s, discovery/difftest 8.126s, registry 148.136s, deploy 19.071s); all 94 ordinary package entries PASS; all 12 enabled real Python difftest packages PASS. Proddiff: 75 GET routes / 2831 cases; self-check PASS; coverage_regressions=0; unmapped_routes=0; new=0; stale=0 (11 existing differences unchanged). Start loadavg 4.82 4.91 5.83. Parser fuzz PASS: 502,767 executions / 60s. No routes/features/open questions remain; no intentional functional difference or newly fixed Python bug. Only the existing accepted YAML-format/derived-revision class is normalized. Exact Chinese handback, severity-ranked closed findings, commands, outputs and file counts: `A24-HANDOFF.md`. Original separately notified public helper commit: 78b796af (rebased helper 1af82bee). No push; independent review sets verified later.

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
   **Closed (A07 review, 2026-11-16, ce75e46a):** `agents/proxy.go` (requests 2.31 / urllib3 2.8 proxy
   choice), 994 reference cases, loopback regression test, client-level differential against real requests
   (no ported route sends trust_env requests yet; details in A01's section).
6. **Deliberate fixes of a Python validation bug (maintainer, 2026-11-16):** three Python checks use
   `re.match(r'^…$')`, whose `$` accepts one trailing "\n": the Agent name in `install_script.py`
   (`validate_agent_name`, A08), the server URL in `docker_ha_guide.py` (A09), and the EasyTier URL in
   `common/mihomo_easytier.py` (C12). The Python version will not be fixed (it is being retired); **the Go
   version rejects such input** (`fullmatch` semantics, same error text as any other invalid value). These
   are the only accepted behaviour differences of this kind: corpora/differentials mark exactly the rows
   with a single trailing "\n" as expected differences, with a regression test. A09: done by its porter.
   A08 (verified) and C12 (verified): small follow-up by the next agent touching `internal/installer` / **A08/A09 part done by A10 (61f82de3, see A10 section).**
   `internal/convert` easytier code; the modules stay verified with this listed.
