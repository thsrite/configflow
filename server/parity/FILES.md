# Python file assignment (all domains)

Every non-test Python file under `backend/` (excluding `backend/test_*.py`, `backend/**/test_*.py` and
`backend/agents/go-agent/**`), with its line count (`wc -l`) and the one domain it belongs to. Domains are
defined in `server/WORKFLOW.md`; `config` modules are defined in `server/parity/PROGRESS-config.md`.
Files of the other domains get their module ids from their own planning step.

Rules used: shared files go to the domain whose modules use them most (other users are noted); a file only
partly not-ported stays in its domain with the excluded functions noted; `not-ported` holds only the
maintainer decisions listed in `packages.md`. Test helpers that are not named `test_*.py` (`conftest.py`,
`native_test_helpers.py`, `routeros_test_helpers.py`) are counted here and go to the domain of their tests.

Command used: `find backend -name '*.py' -not -name 'test_*' -not -path 'backend/agents/go-agent/*' | xargs wc -l`.

## Totals

| Domain | Files | Python lines |
|---|---|---|
| config | 119 | 39628 |
| agents | 69 | 25154 |
| routers | 52 | 17574 |
| finish | 5 | 1799 |
| not-ported | 6 | 473 |
| **total** | **251** | **84628** |

## Files

| File | Lines | Domain | Module | Notes |
|---|---|---|---|---|
| `backend/__init__.py` | 1 | config | C02 |  |
| `backend/agents/__init__.py` | 10 | agents |  |  |
| `backend/agents/address_follow.py` | 221 | agents |  |  |
| `backend/agents/config_generator.py` | 47 | agents |  | generation dispatcher used by routes/agents; needs config converters |
| `backend/agents/content_sync.py` | 460 | agents |  |  |
| `backend/agents/core_install_script.py` | 40 | agents |  |  |
| `backend/agents/deployment_bundle.py` | 834 | agents |  |  |
| `backend/agents/deployment_locations.py` | 106 | agents |  |  |
| `backend/agents/deployment_messages.py` | 97 | agents |  |  |
| `backend/agents/diagnostics.py` | 137 | agents |  |  |
| `backend/agents/dns_follow.py` | 730 | agents |  |  |
| `backend/agents/docker_ha_compact.py` | 182 | agents |  |  |
| `backend/agents/docker_ha_compose.py` | 346 | agents |  |  |
| `backend/agents/docker_ha_compose_direct.py` | 96 | agents |  |  |
| `backend/agents/docker_ha_enrollment.py` | 165 | agents |  |  |
| `backend/agents/docker_ha_guide.py` | 475 | agents |  |  |
| `backend/agents/docker_ha_preflight.py` | 376 | agents |  |  |
| `backend/agents/gateway_install_script.py` | 176 | agents |  |  |
| `backend/agents/gateway_installation.py` | 727 | agents | A22 | internal/gateway/installation |
| `backend/agents/generated_preview.py` | 159 | agents |  |  |
| `backend/agents/go_install_script.py` | 525 | agents |  |  |
| `backend/agents/idle_rounds.py` | 46 | agents |  |  |
| `backend/agents/install_command_store.py` | 99 | agents |  |  |
| `backend/agents/install_output.py` | 45 | agents |  |  |
| `backend/agents/install_script.py` | 448 | agents |  |  |
| `backend/agents/ipv6_client_evidence.py` | 176 | agents |  |  |
| `backend/agents/ipv6_readiness.py` | 492 | agents |  |  |
| `backend/agents/ipv6_verification.py` | 613 | agents |  |  |
| `backend/agents/linux_gateway.py` | 668 | agents |  |  |
| `backend/agents/manager.py` | 1787 | agents |  |  |
| `backend/agents/metrics_flush.py` | 260 | agents |  |  |
| `backend/agents/metrics_history.py` | 810 | agents |  | `LEGACY_DIRS` handling not ported (maintainer decision) |
| `backend/agents/mihomo_panel.py` | 375 | agents |  |  |
| `backend/agents/mihomo_resources.py` | 165 | agents |  |  |
| `backend/agents/node_test_dns_cleanup.py` | 147 | not-ported |  | one-time cleanup from #569 (maintainer decision) |
| `backend/agents/operation_events.py` | 84 | agents |  |  |
| `backend/agents/oxidns_cache.py` | 134 | agents |  |  |
| `backend/agents/oxidns_dashboard_state.py` | 240 | agents |  |  |
| `backend/agents/oxidns_ha.py` | 625 | agents |  |  |
| `backend/agents/oxidns_ha_handoff.py` | 167 | agents |  |  |
| `backend/agents/oxidns_ha_publish.py` | 676 | agents |  |  |
| `backend/agents/oxidns_metrics.py` | 338 | agents |  |  |
| `backend/agents/oxidns_monitor.py` | 727 | agents |  |  |
| `backend/agents/oxidns_publication_stats.py` | 158 | agents |  |  |
| `backend/agents/oxidns_ranking_history.py` | 402 | agents |  |  |
| `backend/agents/oxidns_rule_update.py` | 166 | agents |  |  |
| `backend/agents/oxidns_validation_receipt.py` | 76 | agents |  |  |
| `backend/agents/presence_log.py` | 36 | agents |  |  |
| `backend/agents/push_diff.py` | 1465 | agents |  |  |
| `backend/agents/rule_sync.py` | 643 | agents |  |  |
| `backend/agents/rule_sync_projection.py` | 608 | agents |  |  |
| `backend/agents/rule_sync_snapshot.py` | 98 | agents |  |  |
| `backend/agents/rule_sync_store.py` | 147 | agents |  |  |
| `backend/agents/runtime_state.py` | 204 | agents |  |  |
| `backend/agents/traffic_history.py` | 504 | agents |  |  |
| `backend/agents/traffic_store.py` | 265 | agents |  |  |
| `backend/agents/upgrade_messages.py` | 147 | agents |  |  |
| `backend/agents/upgrades.py` | 310 | agents |  |  |
| `backend/agents/version.py` | 597 | agents |  |  |
| `backend/app.py` | 206 | config | C06 | starts agents/routers loops; `node_test_dns_cleanup.migrate` call not ported |
| `backend/common/__init__.py` | 1 | config | C02 |  |
| `backend/common/agent_manager.py` | 28 | agents |  | also used by: config (notifications, routes/tasks, routes/config, domain discovery, setup) |
| `backend/common/auth.py` | 171 | config | C06 |  |
| `backend/common/client_devices.py` | 63 | routers |  | R16 |
| `backend/common/client_discovery.py` | 204 | routers |  |  |
| `backend/common/client_discovery_cache.py` | 88 | routers |  |  |
| `backend/common/client_fixed_ip.py` | 281 | routers |  | R17 |
| `backend/common/config.py` | 345 | config | C04 | also used by: every domain |
| `backend/common/config_export.py` | 290 | config | C34 |  |
| `backend/common/config_repository.py` | 2604 | config | C03,C04 | split by function groups: part A → C03, part B → C04 (see PROGRESS-config.md). Not ported: legacy pre-SQLite migration (`_migrate_to_store_locked`, `_legacy_*`, `_import_changed_legacy_locked`, `_read_legacy_json`, `_read_profile_layout`, `_migrate_profile_cache`, `_migrate_raw_cache`, `_create_migration_snapshot_dir`, `_snapshot` for legacy sources) and `_rebuild_corrupt_locked` (rebuild from config.json; see PROGRESS-config C03). Also used by: every domain. |
| `backend/common/config_store.py` | 564 | config | C01 |  |
| `backend/common/device_notes.py` | 105 | config | C31 | also used by: agents (routes/agents, client_tools), routers (policy_workspace, client_devices, client_fixed_ip); config_repository validates with it |
| `backend/common/dns_watchdog.py` | 94 | config | C25 | also used by: agents (deployment_bundle, rule_sync); placed in config because oxidns_common_routing (config) needs it |
| `backend/common/gateway_onboarding.py` | 660 | agents |  | also used by: config (mihomo_base_settings, setup_flow) |
| `backend/common/internal_call.py` | 22 | config | C02 | also used by: mcp_server (finish) |
| `backend/common/json_response.py` | 94 | config | C02 |  |
| `backend/common/linux_gateway.py` | 518 | agents |  | also used by: config (converters/mihomo, mihomo_base_settings, setup_flow) |
| `backend/common/linux_gateway_draft.py` | 165 | agents |  |  |
| `backend/common/mihomo_base_settings.py` | 370 | config | C18 | calls gateway_onboarding, linux_gateway (agents), router_split (routers), setup_flow |
| `backend/common/mihomo_easytier.py` | 332 | config | C12 |  |
| `backend/common/node_deletion.py` | 85 | config | C30 |  |
| `backend/common/oxidns_common.py` | 1175 | config | C24 |  |
| `backend/common/oxidns_common_routing.py` | 1192 | config | C25 |  |
| `backend/common/oxidns_common_updates.py` | 465 | config | C26 |  |
| `backend/common/oxidns_device_policy.py` | 386 | config | C26 |  |
| `backend/common/oxidns_editor.py` | 338 | config | C26 |  |
| `backend/common/oxidns_global_constraints.py` | 188 | config | C23 |  |
| `backend/common/oxidns_match_targets.py` | 61 | config | C22 |  |
| `backend/common/oxidns_native.py` | 264 | config | C22 |  |
| `backend/common/oxidns_native_apply.py` | 520 | config | C29 |  |
| `backend/common/oxidns_plugin_constraints.py` | 676 | config | C23 |  |
| `backend/common/oxidns_reference_validation.py` | 368 | config | C23 |  |
| `backend/common/oxidns_references.py` | 64 | config | C22 |  |
| `backend/common/oxidns_settings.py` | 289 | config | C05 | also used by: agents (deployment_bundle, linux_gateway, gateway_onboarding) |
| `backend/common/oxidns_validation.py` | 310 | config | C23 |  |
| `backend/common/profile_context.py` | 208 | config | C05 | also used by: converters, routes of all domains |
| `backend/common/resource.py` | 63 | config | C02 |  |
| `backend/common/router_split.py` | 302 | routers | R02 | also used by: config (mihomo_base_settings, setup_flow, setup_completion), agents |
| `backend/common/rule_dns.py` | 885 | config | C27 | calls agents.rule_sync.settings_for (agents) and common.rule_ip_presence (routers) |
| `backend/common/rule_ip_presence.py` | 79 | routers | R02 | also used by: config (rule_dns) |
| `backend/common/setup_abandon.py` | 90 | config | C36 | calls agents.gateway_installation |
| `backend/common/setup_completion.py` | 236 | config | C36 | calls routers.plan/desired/base, agents.oxidns_monitor, agent_manager |
| `backend/common/setup_flow.py` | 600 | config | C36 | calls linux_gateway, gateway_onboarding (agents), router_split, routers.store (routers) |
| `backend/common/setup_presets.py` | 319 | config | C36 |  |
| `backend/common/utils.py` | 33 | config | C02 |  |
| `backend/conftest.py` | 54 | config | C06 | pytest fixtures; Go equivalent is internal/httpx/httpxtest and per-package test helpers |
| `backend/converters/__init__.py` | 1 | config | C02 |  |
| `backend/converters/common.py` | 118 | config | C12 |  |
| `backend/converters/loon.py` | 764 | config | C20 |  |
| `backend/converters/mihomo.py` | 1164 | config | C12,C18 | split by function groups: node conversion → C12, config generation → C18. Calls common.linux_gateway (agents domain). Also used by: agents |
| `backend/converters/mosdns.py` | 1261 | config | C21 |  |
| `backend/converters/oxidns.py` | 490 | config | C22 |  |
| `backend/converters/surge.py` | 1129 | config | C19 |  |
| `backend/http_queue.py` | 85 | not-ported |  | waitress queue (maintainer decision) |
| `backend/http_server.py` | 116 | not-ported |  | waitress server (maintainer decision); keep CONFIGFLOW_BACKEND_HOST/PORT semantics |
| `backend/mcp_server/__init__.py` | 8 | finish |  |  |
| `backend/mcp_server/auth.py` | 72 | finish |  |  |
| `backend/mcp_server/invoker.py` | 108 | finish |  |  |
| `backend/mcp_server/server.py` | 169 | finish |  |  |
| `backend/mcp_server/tools.py` | 1442 | finish |  |  |
| `backend/models/__init__.py` | 1 | not-ported |  | no importer (maintainer decision) |
| `backend/models/config.py` | 68 | not-ported |  | no importer (maintainer decision) |
| `backend/native_test_helpers.py` | 34 | config | C22 | test helper (Mihomo native YAML checks); port as Go test helpers |
| `backend/notifications.py` | 296 | config | C07 | calls agents.operation_events, routers.pending/store, agent_manager |
| `backend/routeros_test_helpers.py` | 109 | routers | R07 | test helper; port as Go test helpers |
| `backend/routers/__init__.py` | 1 | routers |  | R01 |
| `backend/routers/base.py` | 371 | routers |  |  |
| `backend/routers/desired.py` | 163 | routers |  |  |
| `backend/routers/dhcp_dns.py` | 158 | routers |  |  |
| `backend/routers/dns_guard.py` | 589 | routers |  | R09 |
| `backend/routers/forward_hint.py` | 184 | routers |  | R09 |
| `backend/routers/ikuai.py` | 1179 | routers |  | R05 |
| `backend/routers/ikuai_clients.py` | 142 | routers |  | R16 |
| `backend/routers/ikuai_fixed_ip.py` | 261 | routers |  | R17 |
| `backend/routers/ikuai_groups.py` | 684 | routers |  | R03 |
| `backend/routers/ikuai_routes.py` | 237 | routers |  | R06 |
| `backend/routers/ipv6_dns.py` | 232 | routers |  | R11 |
| `backend/routers/ipv6_fakeip.py` | 348 | routers |  | R10 |
| `backend/routers/ipv6_pd_guard.py` | 635 | routers |  | R11 |
| `backend/routers/ipv6_policy.py` | 529 | routers |  | R10 |
| `backend/routers/ipv6_readiness.py` | 294 | routers |  | R11 |
| `backend/routers/ipv6_routes.py` | 370 | routers |  | R10 |
| `backend/routers/lan_clients.py` | 93 | routers |  |  |
| `backend/routers/pending.py` | 260 | routers |  | R13 |
| `backend/routers/plan.py` | 807 | routers |  | R12 |
| `backend/routers/policy_workspace.py` | 52 | routers |  | R15 |
| `backend/routers/release.py` | 413 | routers |  | R12 |
| `backend/routers/report.py` | 306 | routers |  | R03 |
| `backend/routers/ros_address_lists.py` | 297 | routers |  | R07 |
| `backend/routers/ros_clients.py` | 289 | routers |  | R16 |
| `backend/routers/ros_policy.py` | 520 | routers |  | R08 |
| `backend/routers/ros_policy_view.py` | 47 | routers |  | R08 |
| `backend/routers/ros_policy_write.py` | 782 | routers |  | R08 |
| `backend/routers/ros_return_advisory.py` | 210 | routers |  | R13 |
| `backend/routers/ros_values.py` | 27 | routers |  |  |
| `backend/routers/route_editor.py` | 164 | routers |  | R06 |
| `backend/routers/routeros.py` | 816 | routers |  | R04 |
| `backend/routers/routeros_batch.py` | 599 | routers |  | R04 |
| `backend/routers/routeros_fixed_ip.py` | 382 | routers |  | R17 |
| `backend/routers/routeros_nat.py` | 509 | routers |  | R03 |
| `backend/routers/routeros_nat_list.py` | 1016 | routers |  | R07 |
| `backend/routers/routeros_routes.py` | 283 | routers |  | R06 |
| `backend/routers/rule_sync.py` | 474 | routers |  | R14 |
| `backend/routers/rule_sync_rules.py` | 214 | routers |  |  |
| `backend/routers/rule_sync_store.py` | 90 | routers |  |  |
| `backend/routers/rule_sync_write.py` | 265 | routers |  |  |
| `backend/routers/store.py` | 156 | routers |  |  |
| `backend/routes/__init__.py` | 113 | config | C06 |  |
| `backend/routes/agents.py` | 2388 | agents |  |  |
| `backend/routes/aggregations.py` | 617 | config | C31 |  |
| `backend/routes/auth.py` | 83 | config | C06 |  |
| `backend/routes/client_devices.py` | 153 | routers |  | R17 |
| `backend/routes/client_tools.py` | 105 | agents |  | registers on the agents blueprint (`agents.*` endpoints) |
| `backend/routes/config.py` | 497 | config | C34 |  |
| `backend/routes/docker_ha_installation.py` | 67 | agents |  |  |
| `backend/routes/domain_discovery.py` | 174 | config | C37 |  |
| `backend/routes/gateway_installations.py` | 82 | agents | A22 | internal/api/agents |
| `backend/routes/gateway_onboarding.py` | 91 | agents |  |  |
| `backend/routes/generate.py` | 320 | config | C34 |  |
| `backend/routes/ipv6_readiness.py` | 63 | agents |  |  |
| `backend/routes/linux_gateway.py` | 203 | agents |  |  |
| `backend/routes/linux_gateway_agents.py` | 74 | agents |  |  |
| `backend/routes/logs.py` | 179 | config | C07 |  |
| `backend/routes/mosdns.py` | 594 | config | C28 |  |
| `backend/routes/nodes.py` | 213 | config | C30 |  |
| `backend/routes/notifications.py` | 33 | config | C07 |  |
| `backend/routes/oxidns.py` | 191 | config | C29 |  |
| `backend/routes/profiles.py` | 336 | config | C34 |  |
| `backend/routes/proxy_groups.py` | 436 | config | C33 |  |
| `backend/routes/routers.py` | 820 | routers |  | R13 |
| `backend/routes/rule_library.py` | 540 | config | C33 |  |
| `backend/routes/rules.py` | 1107 | config | C32 |  |
| `backend/routes/settings.py` | 401 | config | C06 |  |
| `backend/routes/setup.py` | 127 | config | C36 |  |
| `backend/routes/stats.py` | 109 | config | C07 |  |
| `backend/routes/subscriptions.py` | 696 | config | C30 |  |
| `backend/routes/tasks.py` | 365 | config | C07 | row builders for router/agent/metrics slots are provided by the routers/agents domains through tasks.SetRows |
| `backend/routes/upstream_router_pending.py` | 27 | routers |  | R15 |
| `backend/startup.py` | 56 | not-ported |  | Python runtime specifics (maintainer decision) |
| `backend/utils/__init__.py` | 1 | config | C02 |  |
| `backend/utils/commented_mihomo.py` | 112 | config | C11 |  |
| `backend/utils/config_importer.py` | 1148 | config | C35 |  |
| `backend/utils/connection_rules.py` | 84 | config | C09 |  |
| `backend/utils/dependencies.py` | 358 | config | C13 |  |
| `backend/utils/dialer_references.py` | 473 | config | C12 |  |
| `backend/utils/domain_discovery.py` | 175 | config | C37 |  |
| `backend/utils/domain_discovery_service.py` | 1309 | config | C37 | calls agents.traffic_store / agents.version / agent_manager (agents domain) |
| `backend/utils/domain_matcher_cache.py` | 237 | config | C09 |  |
| `backend/utils/domain_probe.py` | 197 | config | C16 |  |
| `backend/utils/fast_yaml.py` | 77 | config | C02 |  |
| `backend/utils/group_candidates.py` | 303 | config | C09 |  |
| `backend/utils/health_check.py` | 82 | config | C16 |  |
| `backend/utils/json_store.py` | 77 | config | C02 |  |
| `backend/utils/local_http.py` | 140 | config | C02 |  |
| `backend/utils/logger.py` | 58 | config | C02 |  |
| `backend/utils/mihomo_listeners.py` | 87 | config | C11 |  |
| `backend/utils/mosdns_archive.py` | 244 | config | C28 |  |
| `backend/utils/node_dns.py` | 189 | config | C16 |  |
| `backend/utils/node_latency.py` | 254 | config | C16 |  |
| `backend/utils/notification_store.py` | 103 | config | C07 |  |
| `backend/utils/oxidns_archive.py` | 28 | config | C29 |  |
| `backend/utils/provider_delivery.py` | 424 | config | C17 | calls agents.deployment_locations / agents.diagnostics (agents domain) |
| `backend/utils/provider_response.py` | 116 | config | C17 |  |
| `backend/utils/proxy_chains.py` | 262 | config | C11 |  |
| `backend/utils/proxy_server_dns.py` | 394 | config | C11 | also used by: agents (deployment, outdated-core warnings) |
| `backend/utils/proxy_utils.py` | 14 | config | C12 |  |
| `backend/utils/region_check.py` | 367 | config | C16 |  |
| `backend/utils/remote_rule_sources.py` | 45 | config | C14 |  |
| `backend/utils/reorder.py` | 57 | config | C09 |  |
| `backend/utils/rule_fetch.py` | 346 | config | C14 | also used by: agents (deployment bundle, rule sync) |
| `backend/utils/rule_matcher.py` | 690 | config | C08 |  |
| `backend/utils/rule_syntax.py` | 216 | config | C08 |  |
| `backend/utils/rule_utils.py` | 195 | config | C08 |  |
| `backend/utils/ruleset_content.py` | 722 | config | C14 | also used by: agents, routers |
| `backend/utils/scheduled_refresh.py` | 347 | config | C15 |  |
| `backend/utils/strategy_references.py` | 27 | config | C09 |  |
| `backend/utils/sub_store_client.py` | 453 | config | C13 | also used by: agents, mcp |
| `backend/utils/sub_store_runtime.py` | 233 | config | C13 |  |
| `backend/utils/subscription_cache.py` | 149 | config | C13 | calls agents.content_sync.invalidate_content_sync (agents domain) |
| `backend/utils/subscription_dialers.py` | 299 | config | C10 |  |
| `backend/utils/subscription_filter.py` | 50 | config | C10 |  |
| `backend/utils/subscription_health.py` | 114 | config | C15 |  |
| `backend/utils/subscription_hosts.py` | 465 | config | C10 |  |
| `backend/utils/subscription_parser.py` | 428 | config | C10 | `parse_subscription`, `parse_clash_yaml` not ported (no caller outside tests; noted in progress/5-subs.md) |
| `backend/utils/subscription_refresh.py` | 139 | config | C15 |  |
| `backend/utils/task_status.py` | 126 | config | C02 |  |
| `backend/utils/url_utils.py` | 123 | config | C02 |  |
| `backend/utils/web_listener.py` | 308 | config | C07 |  |
| `backend/utils/webdav_backup.py` | 227 | config | C35 |  |
| `backend/utils/website_identity.py` | 89 | config | C14 |  |
| `backend/version.py` | 11 | config | C02 |  |
