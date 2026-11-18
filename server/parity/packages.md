# Work packages

Dependency-ordered. Lines are approximate Python lines (tests excluded). HR = a defect can break a
user's network; these are verified in the VM lab after the Go tests pass. Status: `todo`, `wip`,
`done` (Go tests ported and passing), `lab` (also verified in the lab).

| # | Package | Python files | Lines | Needs | Status |
|---|---|---|---|---|---|
| 0 | jsonx, store | `common/config_store.py` | 0.6k | – | done |
| 1 | infra | `utils/logger`, `json_store`, `fast_yaml`, `local_http`, `url_utils`, `task_status`, `common/resource`, `common/utils`, `json_response`, `version` | 1.2k | 0 | done |
| 2 | repo | `common/config_repository`, `config`, `profile_context`, `oxidns_settings` | 4.0k | 0 | done |
| 3 | httpx, auth, settings | `common/auth`, `routes/auth`, `routes/__init__`, `settings`, `logs`, `stats`, `tasks`, `notifications`, `app.py` | 2.0k | 2 | done |
| 4 | rule engine | `rule_syntax`, `rule_matcher`, `rule_utils`, `domain_matcher_cache`, `group_candidates`, `reorder`, `strategy_references`, `connection_rules` | 1.8k | 1 | done |
| 5 | subscription parsing (only `parse_uri_list` for manual node strings; subscriptions themselves go through Sub-Store, #13) | `subscription_parser`, `hosts`, `filter`, `dialers`, `proxy_chains`, `commented_mihomo`, `mihomo_listeners`, `proxy_server_dns` | 2.4k | 1 | wip |
| 6 | mihomo converter | `converters/common`, `converters/mihomo`, `dialer_references`, `mihomo_base_settings`, `mihomo_easytier` | 2.5k | 4, 5 | todo |
| 7 | surge, loon | `converters/surge`, `converters/loon` | 1.9k | 6 | todo |
| 8 | mosdns | `converters/mosdns`, `mosdns_archive`, `routes/mosdns` | 2.1k | 2, 12 | todo |
| 9 | oxidns model | `converters/oxidns`, `oxidns_native`, `oxidns_validation`, `reference_validation`, `references`, `plugin_constraints`, `global_constraints`, `match_targets` | 2.4k | 2 | todo |
| 10 | oxidns common | `oxidns_common`, `oxidns_common_routing`, `oxidns_common_updates` | 2.8k | 9 | todo |
| 11 | oxidns editor, rule DNS | `oxidns_device_policy`, `oxidns_editor`, `oxidns_native_apply`, `rule_dns`, `oxidns_archive`, `routes/oxidns` | 2.4k | 10 | todo |
| 12 | remote rules, providers | `rule_fetch`, `ruleset_content`, `remote_rule_sources`, `provider_delivery`, `provider_response`, `website_identity` | 1.7k | 2, 4 | todo |
| 13 | subscription fetch (HTTP client of the bundled Sub-Store; Sub-Store itself is kept, not ported) | `sub_store_client`, `sub_store_runtime`, `subscription_cache`, `refresh`, `health`, `scheduled_refresh`, `dependencies` | 1.8k | 5 | todo |
| 14 | health, latency | `health_check`, `node_latency`, `node_dns`, `region_check`, `domain_probe` (`web_listener` was done in #3) | 1.4k | 13 | todo |
| 15 | resource routes A | `routes/subscriptions`, `aggregations`, `nodes`, `node_deletion`, `device_notes` | 1.7k | 2, 13 | todo |
| 16 | resource routes B | `routes/rules`, `rule_library`, `proxy_groups` | 2.1k | 4, 12 | todo |
| 17 | generate, profiles, import/export | `routes/generate`, `config`, `profiles`, `config_export`, `config_importer`, `webdav_backup` | 2.8k | 6–11 | todo |
| 18 | setup flow | `setup_flow`, `presets`, `completion`, `abandon`, `routes/setup` | 1.4k | 17 | todo |
| 19 | domain discovery | `domain_discovery`, `domain_discovery_service`, `routes/domain_discovery` | 1.7k | 12, 14 | todo |
| 20 | agent registry | `agents/manager`, `common/agent_manager`, `runtime_state`, `diagnostics`, `operation_events`, `idle_rounds`, `presence_log`, `agents/version`, `*_messages` | 3.1k | 2 | wip |
| 21 | metrics, traffic | `metrics_history`, `metrics_flush`, `traffic_history`, `traffic_store`, `oxidns_metrics`, `dashboard_state`, `ranking_history`, `publication_stats` | 3.0k | 20 | todo |
| 22 | installer rendering (HR) | `install_output`, `install_script`, `go_install_script`, `core_install_script`, `gateway_install_script`, `install_command_store`, `agents/scripts/*.sh` | 1.3k + 3.1k sh | 20 | todo |
| 23 | docker HA rendering | `docker_ha_*` | 1.6k | 22 | todo |
| 24 | deployment bundle (HR) | `deployment_bundle`, `content_sync`, `deployment_locations`, `generated_preview`, `mihomo_resources` | 1.8k | 6–12, 20 | todo |
| 25 | push diff | `push_diff` | 1.5k | 24 | todo |
| 26 | agent routes, upgrades (HR) | `routes/agents`, `upgrades` | 2.7k | 20–25 | todo |
| 27 | agent rule sync (HR) | `agents/rule_sync`, `rule_sync_projection`, `snapshot`, `store` | 1.5k | 24 | todo |
| 28 | DNS follow (HR) | `dns_follow`, `address_follow`, `oxidns_rule_update`, `oxidns_cache`, `oxidns_validation_receipt`, `node_test_dns_cleanup` | 1.5k | 24 | todo |
| 29 | DNS HA (HR) | `oxidns_ha`, `oxidns_ha_publish`, `oxidns_ha_handoff`, `oxidns_monitor`, `mihomo_panel` | 2.6k | 24 | todo |
| 30 | linux gateway (HR) | `common/linux_gateway`, `_draft`, `gateway_onboarding`, `agents/linux_gateway`, `gateway_installation`, routes | 3.2k | 22, 24 | todo |
| 31 | IPv6 readiness | `agents/ipv6_readiness`, `verification`, `client_evidence`, `routes/ipv6_readiness` | 1.3k | 20, 35 | todo |
| 32 | router core | `routers/base`, `store`, `desired`, `report`, `plan`, `pending`, `route_editor`, `forward_hint`, `policy_workspace`, `lan_clients` | 2.6k | 2 | todo |
| 33 | iKuai (HR) | `ikuai`, `ikuai_groups`, `routes`, `clients`, `fixed_ip` | 2.5k | 32 | todo |
| 34 | RouterOS core (HR) | `routeros`, `routeros_batch`, `routeros_routes`, `routeros_nat`, `routeros_nat_list`, `ros_values` | 3.3k | 32 | todo |
| 35 | RouterOS policy (HR) | `ros_policy`, `_write`, `_view`, `ros_address_lists`, `ros_return_advisory`, `ros_clients`, `routeros_fixed_ip` | 2.5k | 34 | todo |
| 36 | router DNS, IPv6 (HR) | `dns_guard`, `dhcp_dns`, `ipv6_dns`, `fakeip`, `pd_guard`, `policy`, `routes`, `readiness` | 3.2k | 34 | todo |
| 37 | router publish, rule sync (HR) | `release`, `routers/rule_sync*`, `routes/routers`, `upstream_router_pending` | 2.3k | 33–36 | todo |
| 38 | clients, split | `client_devices`, `client_discovery`, `_cache`, `client_fixed_ip`, `router_split`, `rule_ip_presence`, client routes | 1.4k | 32–35 | todo |
| 39 | MCP (HR) | `mcp_server/*` | 1.8k | all routes | verified — [handoff](progress/mcp.md), [Opus HR review](review-mcp.md) (0 high) |
| 40 | image, entrypoint | `docker/*`, nginx replacement or config, supervisord, auto-update | – | all | todo |

## Not ported (maintainer's decision, 2026-11-14)

- `backend/models/` (69 lines): no importer anywhere.
- `startup.py`, `http_server.py`, `http_queue.py`: Python-runtime specifics (malloc tuning, waitress).
  Use Go's own means instead (GC settings, `net/http`); keep the `CONFIGFLOW_BACKEND_HOST/PORT` semantics.
- Legacy storage migration (pre-SQLite `config.json`, split `system.json` + `profiles/*/config.json`
  layout, the every-start import of an edited `config.json`, cache migration) and
  `metrics_history.LEGACY_DIRS`. The Go server requires a data directory that already holds
  `configflow.sqlite3`, or an empty one (fresh install); with legacy files and no database it must
  refuse to start with a message telling the user to upgrade to the last Python release first.
- `agents/node_test_dns_cleanup.py`: one-time cleanup added in #569.

Anything else that looks unused still has to be reported in the hand-back, not dropped silently.

## Follow-ups after parity (behaviour changes, deliberately not part of the port)

- Surge/Loon manual node strings: the port keeps the built-in `parse_uri_list` (ss, ssr, vmess, vless,
  trojan, hysteria/hysteria2, inline YAML) exactly as Python, including silently dropping other schemes
  (tuic, anytls, wireguard, …). After the Go server passes parity, switch Surge/Loon to Sub-Store
  conversion like Mihomo and delete the built-in parser (maintainer agreed, 2026-11-14).
- Commented Mihomo base-config editor (C11): CRLF documents with comments or blank lines between entries
  are refused (Python edits them but writes mixed `\r\r\n`/`\n`). Deferred by the maintainer
  (2026-11-15); decide near the end of the rewrite whether to reproduce it (est. 1–2 agent days).
- Scheduled refresh (C15): replace the 30 s polling tick with a timer that wakes exactly when the next
  subscription/rule source is due and is recomputed immediately when intervals or sources change.
  Keep the flock (protects against two instances on one DATA_DIR). Behaviour change in timing and in the
  task list's "next run" display, so only after parity (maintainer agreed, 2026-11-15).
