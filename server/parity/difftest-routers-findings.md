# Routers Step B 黑盒差分发现

状态：进行中。矩阵审计尚未完成；不能据此宣称 routers 已通过验收。

矩阵：26 条路由、2301 个设计条目。
公开 Go 入口实际注册本领域路由：0 条。
执行配方统计：`{"go-unimplemented": 3054}`。
实际执行世界（统计仅涵盖这些世界）：`agent-scope, auth-off-discovery, auth-off-editors, auth-off-fixed, auth-off-forwarding, auth-off-release, auth-off-sync, client-canonical-names, client-drafts, client-mode-notes, dhcp-ikuai-plain, disconnect-fixed, disconnect-manual, disconnect-release, discovery, discovery-failures, dns-guard, dns-ha, fixed-ip, fixed-ip-64, fixed-ip-batch-rollback, fixed-ip-binding-cap, fixed-ip-expiry, fixed-ip-guards, fixed-ip-long-id, fixed-ip-restart-random, fixed-ip-restart-stable, fixed-ip-stale, fixed-ip-uncertain, forwarding, forwarding-failures, forwarding-uncertain, guards, head-collection-body, head-collection-empty, head-discovery, head-forwarding-body, head-forwarding-empty, head-report-notifications, head-upstream, ipv6-fakeip-borrow-lla, ipv6-fakeip-borrow-ula, ipv6-fakeip-conflicts, ipv6-fakeip-owned, job-locks, legacy-credentials, manual-routes, notes-capacity, notes-keep-reservation, persistence-publication-busy, persistence-readonly, persistence-receipt, persistence-sqlite-busy, policy-view, probe-errors, probe-ipv6, probe-tls, release-ik-plain, release-ik-suffix, release-ros, return-protocol1, return-protocol2, return-rollback, return-safety, return-uncertain, router-creations, router-guard-normalization, routing-methods, rule-sync, rule-sync-download-failures, rule-sync-proxy, runtime-reads, stored, stored-policy-capacity, stored-policy-duplicate-host-prefix, stored-policy-duplicate-ipv6-compression, stored-policy-duplicate-mapped-host, stored-policy-duplicate-network-mask, stored-policy-legacy, stored-policy-mode-array, stored-policy-mode-empty, stored-policy-mode-false, stored-policy-mode-missing, stored-policy-mode-null, stored-policy-mode-number, stored-policy-mode-object, stored-policy-mode-proxy, stored-policy-mode-upper, stored-policy-name-array, stored-policy-name-ascii81, stored-policy-name-control0, stored-policy-name-control1, stored-policy-name-control10, stored-policy-name-control11, stored-policy-name-control12, stored-policy-name-control13, stored-policy-name-control14, stored-policy-name-control15, stored-policy-name-control16, stored-policy-name-control17, stored-policy-name-control18, stored-policy-name-control19, stored-policy-name-control2, stored-policy-name-control20, stored-policy-name-control21, stored-policy-name-control22, stored-policy-name-control23, stored-policy-name-control24, stored-policy-name-control25, stored-policy-name-control26, stored-policy-name-control27, stored-policy-name-control28, stored-policy-name-control29, stored-policy-name-control3, stored-policy-name-control30, stored-policy-name-control31, stored-policy-name-control4, stored-policy-name-control5, stored-policy-name-control6, stored-policy-name-control7, stored-policy-name-control8, stored-policy-name-control9, stored-policy-name-false, stored-policy-name-null, stored-policy-name-number, stored-policy-name-object, stored-policy-name-space81, stored-policy-name-unicode81, stored-policy-row-array, stored-policy-row-extra-key, stored-policy-row-false, stored-policy-row-null, stored-policy-row-number, stored-policy-row-string, stored-policy-shape-513, stored-policy-shape-false, stored-policy-shape-null, stored-policy-shape-number, stored-policy-shape-object, stored-policy-shape-string, sync-settings, unlinked-inspection, upstream-linked, workspace-drafts`。
已有执行配方引用的设计条目：649；逐条变体审计仍未完成。
本次实际执行满足显式变体审计：301；其余2000待补。

“Go 尚未实现”单独计数，不算失败，也不算差分通过。路由层 R15/R17 未注册时，
即使底层 R01–R06 已移植，也不能声称 HTTP 行为已比较。

仅使用合成数据；两端独立 DATA_DIR、网络命名空间和设备 fake；未访问生产或真实设备。
比较器会检查状态、语义响应头、类型/键/值/数组顺序、完整逻辑数据库、文件与设备状态。
Python 前置条件失败单列为夹具错误，不算 Go 发现；实际比较为零时不能下“无缺陷”结论。

目前没有已比较的 Go 缺陷结论。Python 参考异常单独记录；“Go 尚未实现”不算差分通过。

## Python bug · medium · AGENT_FORWARD_FILTER_UNHASHABLE

- 用例：forwarding-failures:R025/filter-bug-state-array；矩阵：H0016, R025-F11
- 建议负责模块：A20（Linux gateway）及 R15（forwarding HTTP）。
- 一个 Agent 的 forward_filter.state/source.kind/remedy.source 返回数组或对象时，Python 用不可哈希值查 frozenset，抛出未捕获 TypeError，整个 GET 返回 HTML 500，另一台健康网关的信息也丢失。应隔离这份不可信报告并保留健康条目。
- 请求：`{"method": "GET", "path": "/api/routers/gateway-forwarding", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 500, "headers": {"access-control-allow-origin": ["*"], "content-type": ["text/html; charset=utf-8"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": "<!doctype html>\n<html lang=en>\n<title>500 Internal Server Error</title>\n<h1>Internal Server Error</h1>\n<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>\n"}`
- Python 设备终态：`{"http_status": 500, "unaffected_gateway_hidden": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": ["R025/agent-http-400", "R025/agent-http-409", "R025/agent-http-422", "R025/agent-http-500", "R025/agent-http-502", "R025/agent-http-503", "R025/agent-auth-401", "R025/agent-auth-403", "R025/agent-missing-api", "R025/agent-not-object-0", "R025/agent-not-object-1", "R025/agent-not-object-2", "R025/agent-not-object-3", "R025/agent-not-object-4", "R025/agent-invalid-json", "R025/agent-lost-response", "R025/agent-unicode-error-limit", "R025/forwarding-shape-0", "R025/forwarding-shape-1", "R025/forwarding-shape-2", "R025/forwarding-shape-3", "R025/state-unsupported", "R025/state-foreign", "R025/state-symlink", "R025/state-modified-hook", "R025/state-persisted", "R025/state-bool-current", "R025/state-integer-persisted", "R025/state-system-persisted", "R025/state-bad-system-source", "R025/state-notice", "R026/agent-http-400", "R026/agent-http-409", "R026/agent-http-422", "R026/agent-http-500", "R026/agent-http-503", "R026/agent-auth-401", "R026/agent-auth-403", "R026/agent-missing-api", "R026/agent-shape-0", "R026/agent-shape-1", "R026/agent-shape-2", "R026/agent-shape-3", "R026/agent-invalid-json", "R025/response-byte-limit-524288", "R025/response-byte-limit-524289", "R025/filter-dropped-0", "R025/filter-dropped-1", "R025/filter-dropped-2", "R025/filter-dropped-3", "R025/filter-dropped-4", "R025/filter-dropped-5", "R025/filter-dropped-6", "R025/filter-safe-display-shape", "R025/filter-exact-types"]}`

## Python bug · medium · AGENT_FORWARD_FILTER_UNHASHABLE

- 用例：forwarding-failures:R025/filter-bug-state-object；矩阵：H0016, R025-F11
- 建议负责模块：A20（Linux gateway）及 R15（forwarding HTTP）。
- 一个 Agent 的 forward_filter.state/source.kind/remedy.source 返回数组或对象时，Python 用不可哈希值查 frozenset，抛出未捕获 TypeError，整个 GET 返回 HTML 500，另一台健康网关的信息也丢失。应隔离这份不可信报告并保留健康条目。
- 请求：`{"method": "GET", "path": "/api/routers/gateway-forwarding", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 500, "headers": {"access-control-allow-origin": ["*"], "content-type": ["text/html; charset=utf-8"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": "<!doctype html>\n<html lang=en>\n<title>500 Internal Server Error</title>\n<h1>Internal Server Error</h1>\n<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>\n"}`
- Python 设备终态：`{"http_status": 500, "unaffected_gateway_hidden": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": ["R025/agent-http-400", "R025/agent-http-409", "R025/agent-http-422", "R025/agent-http-500", "R025/agent-http-502", "R025/agent-http-503", "R025/agent-auth-401", "R025/agent-auth-403", "R025/agent-missing-api", "R025/agent-not-object-0", "R025/agent-not-object-1", "R025/agent-not-object-2", "R025/agent-not-object-3", "R025/agent-not-object-4", "R025/agent-invalid-json", "R025/agent-lost-response", "R025/agent-unicode-error-limit", "R025/forwarding-shape-0", "R025/forwarding-shape-1", "R025/forwarding-shape-2", "R025/forwarding-shape-3", "R025/state-unsupported", "R025/state-foreign", "R025/state-symlink", "R025/state-modified-hook", "R025/state-persisted", "R025/state-bool-current", "R025/state-integer-persisted", "R025/state-system-persisted", "R025/state-bad-system-source", "R025/state-notice", "R026/agent-http-400", "R026/agent-http-409", "R026/agent-http-422", "R026/agent-http-500", "R026/agent-http-503", "R026/agent-auth-401", "R026/agent-auth-403", "R026/agent-missing-api", "R026/agent-shape-0", "R026/agent-shape-1", "R026/agent-shape-2", "R026/agent-shape-3", "R026/agent-invalid-json", "R025/response-byte-limit-524288", "R025/response-byte-limit-524289", "R025/filter-dropped-0", "R025/filter-dropped-1", "R025/filter-dropped-2", "R025/filter-dropped-3", "R025/filter-dropped-4", "R025/filter-dropped-5", "R025/filter-dropped-6", "R025/filter-safe-display-shape", "R025/filter-exact-types", "R025/filter-bug-state-array"]}`

## Python bug · medium · AGENT_FORWARD_FILTER_UNHASHABLE

- 用例：forwarding-failures:R025/filter-bug-source-kind-array；矩阵：H0016, R025-F11
- 建议负责模块：A20（Linux gateway）及 R15（forwarding HTTP）。
- 一个 Agent 的 forward_filter.state/source.kind/remedy.source 返回数组或对象时，Python 用不可哈希值查 frozenset，抛出未捕获 TypeError，整个 GET 返回 HTML 500，另一台健康网关的信息也丢失。应隔离这份不可信报告并保留健康条目。
- 请求：`{"method": "GET", "path": "/api/routers/gateway-forwarding", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 500, "headers": {"access-control-allow-origin": ["*"], "content-type": ["text/html; charset=utf-8"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": "<!doctype html>\n<html lang=en>\n<title>500 Internal Server Error</title>\n<h1>Internal Server Error</h1>\n<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>\n"}`
- Python 设备终态：`{"http_status": 500, "unaffected_gateway_hidden": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": ["R025/agent-http-400", "R025/agent-http-409", "R025/agent-http-422", "R025/agent-http-500", "R025/agent-http-502", "R025/agent-http-503", "R025/agent-auth-401", "R025/agent-auth-403", "R025/agent-missing-api", "R025/agent-not-object-0", "R025/agent-not-object-1", "R025/agent-not-object-2", "R025/agent-not-object-3", "R025/agent-not-object-4", "R025/agent-invalid-json", "R025/agent-lost-response", "R025/agent-unicode-error-limit", "R025/forwarding-shape-0", "R025/forwarding-shape-1", "R025/forwarding-shape-2", "R025/forwarding-shape-3", "R025/state-unsupported", "R025/state-foreign", "R025/state-symlink", "R025/state-modified-hook", "R025/state-persisted", "R025/state-bool-current", "R025/state-integer-persisted", "R025/state-system-persisted", "R025/state-bad-system-source", "R025/state-notice", "R026/agent-http-400", "R026/agent-http-409", "R026/agent-http-422", "R026/agent-http-500", "R026/agent-http-503", "R026/agent-auth-401", "R026/agent-auth-403", "R026/agent-missing-api", "R026/agent-shape-0", "R026/agent-shape-1", "R026/agent-shape-2", "R026/agent-shape-3", "R026/agent-invalid-json", "R025/response-byte-limit-524288", "R025/response-byte-limit-524289", "R025/filter-dropped-0", "R025/filter-dropped-1", "R025/filter-dropped-2", "R025/filter-dropped-3", "R025/filter-dropped-4", "R025/filter-dropped-5", "R025/filter-dropped-6", "R025/filter-safe-display-shape", "R025/filter-exact-types", "R025/filter-bug-state-array", "R025/filter-bug-state-object"]}`

## Python bug · medium · AGENT_FORWARD_FILTER_UNHASHABLE

- 用例：forwarding-failures:R025/filter-bug-remedy-source-array；矩阵：H0016, R025-F11
- 建议负责模块：A20（Linux gateway）及 R15（forwarding HTTP）。
- 一个 Agent 的 forward_filter.state/source.kind/remedy.source 返回数组或对象时，Python 用不可哈希值查 frozenset，抛出未捕获 TypeError，整个 GET 返回 HTML 500，另一台健康网关的信息也丢失。应隔离这份不可信报告并保留健康条目。
- 请求：`{"method": "GET", "path": "/api/routers/gateway-forwarding", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 500, "headers": {"access-control-allow-origin": ["*"], "content-type": ["text/html; charset=utf-8"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": "<!doctype html>\n<html lang=en>\n<title>500 Internal Server Error</title>\n<h1>Internal Server Error</h1>\n<p>The server encountered an internal error and was unable to complete your request. Either the server is overloaded or there is an error in the application.</p>\n"}`
- Python 设备终态：`{"http_status": 500, "unaffected_gateway_hidden": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": ["R025/agent-http-400", "R025/agent-http-409", "R025/agent-http-422", "R025/agent-http-500", "R025/agent-http-502", "R025/agent-http-503", "R025/agent-auth-401", "R025/agent-auth-403", "R025/agent-missing-api", "R025/agent-not-object-0", "R025/agent-not-object-1", "R025/agent-not-object-2", "R025/agent-not-object-3", "R025/agent-not-object-4", "R025/agent-invalid-json", "R025/agent-lost-response", "R025/agent-unicode-error-limit", "R025/forwarding-shape-0", "R025/forwarding-shape-1", "R025/forwarding-shape-2", "R025/forwarding-shape-3", "R025/state-unsupported", "R025/state-foreign", "R025/state-symlink", "R025/state-modified-hook", "R025/state-persisted", "R025/state-bool-current", "R025/state-integer-persisted", "R025/state-system-persisted", "R025/state-bad-system-source", "R025/state-notice", "R026/agent-http-400", "R026/agent-http-409", "R026/agent-http-422", "R026/agent-http-500", "R026/agent-http-503", "R026/agent-auth-401", "R026/agent-auth-403", "R026/agent-missing-api", "R026/agent-shape-0", "R026/agent-shape-1", "R026/agent-shape-2", "R026/agent-shape-3", "R026/agent-invalid-json", "R025/response-byte-limit-524288", "R025/response-byte-limit-524289", "R025/filter-dropped-0", "R025/filter-dropped-1", "R025/filter-dropped-2", "R025/filter-dropped-3", "R025/filter-dropped-4", "R025/filter-dropped-5", "R025/filter-dropped-6", "R025/filter-safe-display-shape", "R025/filter-exact-types", "R025/filter-bug-state-array", "R025/filter-bug-state-object", "R025/filter-bug-source-kind-array"]}`

## Python bug · medium · IKUAI_NETMASK_SUFFIX_ROLLBACK

- 用例：release-ik-suffix:R011-S02/real-suffix-python-bug；矩阵：R011-S02
- 建议负责模块：R05（iKuai）。
- 默认 fake 保留真实 static_rt netmask 显示后缀。Python 发布返回 502，新增静态路由的回读和补偿均因后缀不匹配失败，设备留下配置。未修改 fake 来掩盖此问题。
- 请求：`{"method": "POST", "path": "/api/routers/r-ik/apply", "document": {"fingerprint": "bfba4a58e875bf6e"}, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 502, "headers": {"access-control-allow-origin": ["*"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "json", "body": {"message": "阶段「routes」失败，回滚未完成，请检查发布记录", "plan": {"blockers": [], "checks": {"gateway": "10.44.0.2", "ikuai_groups": {"nat_bypass": {"groups": {"cf_nat_1": "a2c2c8777b491796"}, "removed": [], "rules": [{"comment": "cf-mihomo", "func": "nat_rule", "groups": ["cf_nat_1"], "id": null, "keep_foreign": true}]}, "routes": {"groups": {}, "removed": [], "rules": []}}, "nat_bypass": ["198.18.0.0/16"], "nat_not_required": [], "removed_routes": [], "routes": [{"cidr": "198.18.0.0/16", "gateway": "10.44.0.2"}]}, "empty": false, "fingerprint": "bfba4a58e875bf6e", "script": "", "stages": [{"enabled": false, "key": "return_routing", "manual": false, "note": "未启用自动配置回流：本次跳过，已有回流规则保留，不新增、修改或删除。", "ops": [], "title": "mihomo 回流规则"}, {"key": "routes", "manual": false, "ops": [{"cidr": "198.18.0.0/16", "comment": "cf-fake-ip", "gateway": "10.44.0.2", "kind": "route_add", "summary": "新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）"}], "title": "主路由代理路由"}, {"key": "nat_bypass", "manual": false, "ops": [{"comment": "cf-mihomo-nat", "kind": "ikuai_group_add", "name": "cf_nat_1", "role": "nat", "summary": "新建 IP 分组「cf_nat_1」（NAT 免转换使用，1 个网段）", "values": ["198.18.0.0/16"]}, {"comment": "cf-mihomo", "dst": ["198.18.0.0/16"], "groups": ["cf_nat_1"], "kind": "nat_add", "src": ["10.44.0.1-10.44.0.1", "10.44.0.3-10.44.0.254"], "summary": "新增 NAT 免转换规则：源 10.44.0.1-10.44.0.1, 10.44.0.3-10.44.0.254，目的 IP 分组 cf_nat_1（共 1 个网段）"}], "title": "主路由 NAT 免转换"}, {"enabled": false, "key": "dhcp_dns", "manual": false, "note": "未选择 DHCP DNS 服务器：本次保留现有 DNS 配置。", "ops": [], "title": "DHCP DNS"}], "warnings": []}, "result": {"failed_stage": "routes", "log": [{"stage": "routes", "status": "ok", "summary": "新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）"}, {"stage": "routes", "status": "failed", "summary": "iKuai static_rt 已提交但读不回新对象：198.18.0.0/16"}], "ok": false, "rollback": [{"stage": "routes", "status": "failed", "summary": "撤销失败：新增路由 198.18.0.0/16 → 10.44.0.2（备注 cf-fake-ip）（iKuai static_rt 中名称为 cf_rt_a2c2c8777 的对象已被修改，未自动删除，请检查）"}]}, "success": false}}`
- Python 设备终态：`{"created_static_rows_remaining": 1, "static_rt_after": [{"id": 2, "enabled": "yes", "tagname": "cf_rt_a2c2c8777", "ip_type": "4", "interface": "auto", "dst_addr": "198.18.0.0", "gateway": "10.44.0.2", "netmask": "255.255.0.0", "prio": 1, "comment": "cf-fake-ip"}]}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R011", "R012"], "prior_missing_mutations": []}`
- 普通 netmask 的独立变体覆盖 R011-S02 成功路径；默认后缀变体记录这个 Python bug。
- L5 待核：真实固件 static_rt 的 netmask 后缀形态，以及失败后设备补偿是否干净。

## Python bug · medium · HEAD_CREATES_ROUTER

- 用例：head-collection-empty:R007/HEAD-without-body-is-not-GET；矩阵：X06
- 建议负责模块：R15（router HTTP）。
- GET/POST 共用的路由器集合处理函数只按 method==GET 分支；HEAD 落入创建分支。无 body 返回400，合法 body 会新增路由器并递增配置 revision，响应 body 被 HEAD 协议隐藏。Go HEAD 必须只读且按 GET 返回200。
- 请求：`{"method": "HEAD", "path": "/api/routers", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 400, "headers": {"access-control-allow-origin": ["*"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": ""}`
- Python 设备终态：`{"http_status": 400, "configuration_changed": false, "agent_post_requests": 0, "agent_get_requests": 0}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R007"], "prior_missing_mutations": []}`

## Python bug · medium · HEAD_CREATES_ROUTER

- 用例：head-collection-body:R007/HEAD-creates-router；矩阵：X06
- 建议负责模块：R15（router HTTP）。
- GET/POST 共用的路由器集合处理函数只按 method==GET 分支；HEAD 落入创建分支。无 body 返回400，合法 body 会新增路由器并递增配置 revision，响应 body 被 HEAD 协议隐藏。Go HEAD 必须只读且按 GET 返回200。
- 请求：`{"method": "HEAD", "path": "/api/routers", "document": {"name": "HEAD 不应创建🧪", "type": "routeros", "endpoint": "http://10.44.0.1:18081", "username": "test-admin", "password": "synthetic-device-password-0123456789"}, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 200, "headers": {"access-control-allow-origin": ["*"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": ""}`
- Python 设备终态：`{"http_status": 200, "configuration_changed": true, "agent_post_requests": 0, "agent_get_requests": 0}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R007"], "prior_missing_mutations": []}`

## Python bug · medium · HEAD_PERSISTS_FORWARDING

- 用例：head-forwarding-empty:R025/HEAD-without-body-is-not-GET；矩阵：X06, R025-B90
- 建议负责模块：R15（forwarding HTTP）及 A20（Linux gateway）。
- GET/POST 共用的 forwarding 处理函数只按 method==GET 分支；HEAD 落入持久化分支。无 body 返回400且不读 Agent，合法 agent_id 会向设备 POST 并开启持久化转发。Go HEAD 必须执行只读 GET，不得写设备。
- 请求：`{"method": "HEAD", "path": "/api/routers/gateway-forwarding", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 400, "headers": {"access-control-allow-origin": ["*"], "cache-control": ["no-store"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": ""}`
- Python 设备终态：`{"http_status": 400, "configuration_changed": false, "agent_post_requests": 0, "agent_get_requests": 0}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": []}`

## Python bug · medium · HEAD_PERSISTS_FORWARDING

- 用例：head-forwarding-body:R025/HEAD-persists-forwarding；矩阵：X06, R025-B90
- 建议负责模块：R15（forwarding HTTP）及 A20（Linux gateway）。
- GET/POST 共用的 forwarding 处理函数只按 method==GET 分支；HEAD 落入持久化分支。无 body 返回400且不读 Agent，合法 agent_id 会向设备 POST 并开启持久化转发。Go HEAD 必须执行只读 GET，不得写设备。
- 请求：`{"method": "HEAD", "path": "/api/routers/gateway-forwarding", "document": {"agent_id": "m-a"}, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 200, "headers": {"access-control-allow-origin": ["*"], "cache-control": ["no-store"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "text", "body": ""}`
- Python 设备终态：`{"http_status": 200, "configuration_changed": false, "agent_post_requests": 1, "agent_get_requests": 0}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R025"], "prior_missing_mutations": []}`

后续仍须补齐：其余 H 公开可达性与复合 guard 变体、设备失败/补偿、完整生成形态、
Agent/备份/代理、后台轮询、断连/重启、动态值内容绑定和 O12 分类。详见进度文件与执行 ledger。

## 新增独立批次证据（不包含于上面的3054条全组合统计）

native-side 批次71条请求、6条显式审计通过；Go领域路由仍未注册。本节仅追加新发现，未将独立批次冒充一次新的全组合。

## Python bug · low · NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION

- 用例：native-side-protocol-collision:R002/native-side-udp-protocol-collision；矩阵：R002-F12
- 建议负责模块：C26（OxiDNS 设备策略/引用扫描）及 R17（客户端 HTTP）。
- 合法 native 配置把主 UDP 监听器命名为 udp，旁路独立转发器使用 udp:// 上游地址时，Python 的引用扫描把 URL 协议名误当插件引用，经主监听器走到 sequence_main，错误地把整份设备策略标为不可编辑；正常模式修改被400拒绝。
- 请求：`{"method": "GET", "path": "/api/client-devices", "document": null, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 200, "headers": {"access-control-allow-origin": ["*"], "cache-control": ["no-store"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "json", "body": {"agents": [], "devices": [{"ip": "10.44.0.21", "mode": "", "name": "现存备注🧪"}], "editable": false, "oxidns_revision": "839ac1ade1e6be6c3a972d314bf3a7cb1972079d0bb87dc100fba80d3c09428f", "profile_id": "alpha", "profile_name": "空间甲🧪", "reason": "监听入口 side_udp（127.0.0.1:5353） 的处理链 side 会间接调用主处理链 sequence_main，看不出设备策略是否生效，无法安全插入设备策略；请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链。", "side_entries": [], "success": true}}`
- Python 设备终态：`{"http_status": 200, "valid_native_edit_incorrectly_blocked": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R002"], "prior_missing_mutations": []}`
- 仅把主监听 tag 改为 client_dns_listener 的独立对照可读、可修改策略并保留旁路字段；没有修改设备 fake。
- Go 应允许该有效旁路并保留 side_entries 提示；修复结果单列复核，不自动算差分通过。

## Python bug · low · NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION

- 用例：native-side-protocol-collision:R003/native-side-udp-protocol-mode-refused；矩阵：R003-F15
- 建议负责模块：C26（OxiDNS 设备策略/引用扫描）及 R17（客户端 HTTP）。
- 合法 native 配置把主 UDP 监听器命名为 udp，旁路独立转发器使用 udp:// 上游地址时，Python 的引用扫描把 URL 协议名误当插件引用，经主监听器走到 sequence_main，错误地把整份设备策略标为不可编辑；正常模式修改被400拒绝。
- 请求：`{"method": "PUT", "path": "/api/client-devices", "document": {"profile_id": "alpha", "ip": "10.44.0.21", "name": "现存备注🧪", "mode": "direct", "previous_name": "现存备注🧪", "previous_mode": "", "base_url": ""}, "raw": null, "headers": {}, "auth": true}`
- Python：`{"status": 400, "headers": {"access-control-allow-origin": ["*"], "cache-control": ["no-store"], "content-type": ["application/json"], "x-configflow-profile": ["alpha"]}, "kind": "json", "body": {"message": "监听入口 side_udp（127.0.0.1:5353） 的处理链 side 会间接调用主处理链 sequence_main，看不出设备策略是否生效，无法安全插入设备策略；请在 OxiDNS 完整参数中把它的 entry 直接改为 sequence_main，或让它不再调用主处理链。", "success": false}}`
- Python 设备终态：`{"http_status": 400, "valid_native_edit_incorrectly_blocked": true}`
- Go：Go 尚未实现（公开路由或前序写入依赖未注册，不能通过 HTTP 检查）：`{"missing_routes": ["R003"], "prior_missing_mutations": []}`
- 仅把主监听 tag 改为 client_dns_listener 的独立对照可读、可修改策略并保留旁路字段；没有修改设备 fake。
- Go 应允许该有效旁路并保留 side_entries 提示；修复结果单列复核，不自动算差分通过。

# 同类补充：过滤信息中的对象值

本节来自独立的169条 forwarding 增量，不改动前面最后一次完整组合的统计。
严重程度 medium；建议负责模块 A20（网关 Agent 协议）与 R15（路由 HTTP）。

- 用例 `forwarding-failures:R025/filter-bug-source-kind-object`（H0016、R025-F11）。
  请求 `GET /api/routers/gateway-forwarding`；合成 Agent 的
  `forward_filter.sources[0].kind` 为对象。Python 实际返回 HTML500，局部日志
  确认 `TypeError: unhashable type: 'dict'`，使另一个正常网关也无法显示。
  Go：公开清单未注册 R025，Go尚未实现，尚无对应 HTTP 结果。
- 用例 `forwarding-failures:R025/filter-bug-remedy-source-object`（H0016、R025-F11）。
  同一 GET；合成 Agent 的 `forward_filter.remedies[0].source` 为对象。
  Python 实际 HTML500，同样由局部 traceback 确认不可哈希对象错误，正常网关
  被隐藏。Go：公开清单未注册 R025，Go尚未实现。

这两项属于已有 `AGENT_FORWARD_FILTER_UNHASHABLE` 类别。未修改产品实现或
放宽 fake；匹配 Python 崩溃的 Go 结果仍会按安全性差异处理，不能算通过。

# 增量发现：iKuai 探测的嵌套版本结构未校验

本节来自独立 `probe-nested-schema` 世界的16条真实 HTTP 请求，不替换前面
最后一次完整组合的3054条统计。新增参考缺陷类别
`IKUAI_PROBE_NESTED_SCHEMA_CRASH`；严重程度 **low**；建议负责模块
**R05（iKuai）与 R15（probe HTTP）**；矩阵关联 **R017-F12、X12**。

下列每项均请求 `POST /api/routers/r-ik/probe`，合成 iKuai 的
`/Action/call`、`func_name=sysstat` 回 `code:0`，`results.verinfo` 如表。

| 用例（世界均为 probe-nested-schema） | verinfo | Python 的真实结果 | Go 的实际结果 |
|---|---|---|---|
| R017/ikuai/nested-verinfo-true | `true` | HTML 500，局部日志确认 bool 的 AttributeError | 公开清单未注册 R017，Go尚未实现 |
| R017/ikuai/nested-verinfo-integer | `1` | HTML 500，局部日志确认 int 的 AttributeError | 同上 |
| R017/ikuai/nested-verinfo-number | `1.5` | HTML 500，局部日志确认 float 的 AttributeError | 同上 |
| R017/ikuai/nested-verinfo-text | `"4.0.314"` | HTML 500，局部日志确认 str 的 AttributeError | 同上 |
| R017/ikuai/nested-verinfo-list | `[{"version":"4.0.314"}]` | HTML 500，局部日志确认 list 的 AttributeError | 同上 |

根因是 `backend/routers/ikuai.py:300` 只以 `or {}` 处理空值，随后直接调用
`verinfo.get`。这些非空值穿过了空值回退；异常不是 RouterError，R017未捕获，
不会保存失败探测。五项均验证数据库和设备表不变，之后正常版本探测仍能
成功，表明错误没有破坏进程和会话。另有 null/false/0/空字符串/空列表/空对象
六个对照，均真实返回502并保存失败探测。

未修改产品实现或 fake 来掩盖问题。将来 Go 的安全拒绝进入 O12 人工复核，
不能自动算通过；重复 HTML 崩溃、成功接受、写设备、改其他配置/文件都不能
通过安全性检查。允许 Go 修复额外保存自己的失败探测，但必须只改请求的
路由器 last_probe，使用自身请求时间内的时间戳、单次配置 revision/generation
递增与新 commit_id，其他空间与元数据不变。具体响应/存储差异始终保留。
