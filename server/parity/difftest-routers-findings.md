# Routers Step B 黑盒差分发现

状态：**按协调方要求收口交回，部分覆盖；不是完整验收通过**。未覆盖条目留在本文件，不再扩展审计方向。最终只跑 `.32` 定向检查，完整门禁由协调方合入批次运行。

## 需要派修的条目（按建议负责模块）

下列都是通过真实 Python HTTP 入口确认的参考缺陷。当前正常 Go 公开清单的本领域26条路由均未注册，**没有已证实的 Go HTTP 回归**；负责模块需要修复这些问题，或提供 Go 已修复的公开边界证据。相同缺陷的多个输入合并派单，不把格式差异、未实现路由或未覆盖项冒充缺陷。

| 建议负责模块 | 严重程度 | 缺陷及派修要求 |
|---|---|---|
| R05（iKuai） | medium | `IKUAI_NETMASK_SUFFIX_ROLLBACK`：真实形态 netmask 显示后缀导致发布502且补偿失败，新增静态路由残留；正确处理读回并保证失败补偿干净。默认 fake 后缀未放宽，另有普通 netmask 成功对照。 |
| R05、R15（probe HTTP） | low | `IKUAI_PROBE_NESTED_SCHEMA_CRASH`：非空标量/数组 verinfo 导致 AttributeError、HTML500且不保存失败探测；应有界拒绝。 |
| R05、R15（probe HTTP） | low | `IKUAI_PROBE_REDIRECT_PARSE_CRASH`：损坏 IPv6 Location 在 requests 预解析下一跳时触发未捕获 ValueError，HTML500且保留旧探测；应捕获不可信响应解析失败并返回结构化错误。 |
| R15（router HTTP） | medium | `HEAD_CREATES_ROUTER`：合法 HEAD body 落入 POST 创建分支，新增路由器并递增 revision；HEAD 必须只读。 |
| A20、R15（forwarding HTTP） | medium | `AGENT_FORWARD_FILTER_UNHASHABLE`：单个 Agent 报告中的数组/对象导致 TypeError，整个网关列表500并隐藏健康网关；应隔离坏报告。 |
| A20、R15（forwarding HTTP） | medium | `HEAD_PERSISTS_FORWARDING`：合法 HEAD body 向 Agent POST 并持久化转发；HEAD 必须只读。 |
| C26、R17（客户端 HTTP） | low | `NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION`：URL 协议名 udp 与主监听 tag 同名时引用扫描误判，合法旁路配置无法编辑；应允许有效独立旁路并保留其内容。 |

完整请求、Python结果、Go尚未实现状态及独立对照见后文证据。所有数据为合成值；未复制私有快照内容。

## 覆盖结论与统计口径

- 矩阵 **2301** 条设计项、**26** 条 method+路径。最新实际公开清单：Go已实现并比较 **0/26**，Go尚未实现 **26/26**；这不是通过或失败。
- 已有 **556** 项执行变体审计定义、**14** 项源码不可达证明、**2** 项 O6 明确替代项。它们有分批定向执行证据，但**没有重新合成一次完整运行**，不能把定义数称作最终整体通过数。
- 其余 **1729** 条没有完成逐项审计归档，全部列于「未覆盖」。部分已有配方或真实请求，仍未满足完整审计要求；保守保留未覆盖。
- 最近一次历史完整组合为 **3054** 请求、**301** 项审计、Go尚未实现3054、原生比较0；后续独立批次不替换这个历史基线。最后重定向批次为6请求、全部Go尚未实现，记录一个新增参考缺陷。
- 所有领域测试使用实际 Python/Go公开进程、独立 DATA_DIR 和隔离 loopback fake；没有读 Go 业务实现、修改产品实现、访问生产/PVE/家中设备。未运行完整门禁或生产快照 proddiff。

## 比较契约尚未闭合的范围

签名预览 token、计划 fingerprint、固定IP随机归属标记/设备分配ID、发布/receipt/任务时钟、自由 YAML 格式与内容保留、只读并发请求序列、并发响应与存储快照线性化，以及安全修复引起的后续 revision 差异，尚未形成完整跨端证明。现有比较保持原值和差异，不做通配删除；未来可能出现需人工分类的严格比较差异。后台周期、跨进程锁/中断、完整固件 session 语义和实机 L5 也没有全部完成。以上是测试覆盖限制，不作为产品派修 finding。

## 历史完整组合与分批发现证据

以下保留历史完整组合与后续独立批次的原始统计，不能将它们相加视作本次整体复跑或 routers 验收通过。

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

## 收口增量：iKuai 重定向解析异常

- 编号：`ikuai-redirect-errors:R017/ikuai/redirect-parser-invalid-bracket`；矩阵关联 R017-F12、X12。严重程度 **low**；建议 R05、R15。
- 请求：`POST /api/routers/r-ik/probe`；隔离 fake 的 `/Action/login` 返回301，`Location: http://[invalid/private`。
- Python：实际 HTML500，局部 traceback 确认 `requests/sessions.py` 与 `ValueError: Invalid IPv6 URL`；数据库及设备表未改变，未保存失败探测。后续健康探测成功。
- Go：实际公开清单未注册 R017，Go尚未实现，HTTP结果为空；不声称已证明 Go 也有此缺陷。
- 根因：`allow_redirects=False` 仍会使实际 requests 预构造下一跳；其 ValueError 不属于 `_post` 捕获的 RequestException。没有替换 requests 或模拟异常。
- 独立对照：文本/越界端口301均正常502；损坏 gzip 实际触发通用通信502；正常探测及恢复均200。六条请求在 `.32` 完成。
- 安全 Go 修复只进入 O12 人工复核，不自动记通过；HTML崩溃、写设备或修改无关配置仍为安全差异。原始HTTP、存储差异全部保留。

## 未覆盖

本清单按当前 `audits.declared()` 和 `reachability.declared()` 对矩阵取补集，**精确列出1729个未完成逐项审计的编号**。原因代码见下表；每个编号链接回矩阵原行。所有编号的Go原生HTTP比较还共同受限于26条领域路由未注册。

`H0209/H0228/H0229` 已有本次重定向批次的真实请求，但未新增审计定义，仍明确列在未覆盖中。全部572个既有定义也未重新做一次完整组合；历史完整组合的2000待补与此1729清单口径不同，不能相减视为本次新通过数。

| 原因代码 | 类别 | 数量 | 未覆盖原因 |
|---|---|---:|---|
| U-R | route | 517 | 无此编号的完整变体审计定义；路由的其他成功/失败配方不能代替本编号。Go处理器亦未注册，尚不能完成原生HTTP差分。 |
| U-H | helper | 1112 | 尚无此编号的正式分支审计定义或公开入口不可达证明。部分已有真实请求，仍未核齐本编号要求；按协调要求停止扩展，不推断通过。 |
| U-D | device | 30 | 已有分散协议请求，但该完整协议契约的顺序、分批、回读、补偿与固件限制尚未收齐审计证据；fake 不等于实机。 |
| U-G | generated | 24 | 该编号的完整生成输出与语义约束未逐项审计；部分计划/脚本成功不足以证明全部生成契约，动态值/内容保留仍有开放范围。 |
| U-T | sequence | 26 | 该编号完整时序、并发、取消/中断与终态证明未收齐；已执行子场景不等于完成整个组合。 |
| U-X | crosscutting | 14 | 已有跨切子场景，尚未收齐该编号涉及的全部路由、锁、后台、存储/文件和跨域副作用契约。 |
| U-F | fixture | 3 | 已有合成种子，但未完整审计本编号所有地址分类、脱敏或receipt变体；fixture存在不直接算覆盖。 |
| U-P | fake-contract | 3 | 已有独立状态化fake与定向负控，完整固件协议、真实session失效/并发或脚本/批次契约未逐条核完；L5未执行。 |

### route（517项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| R001-B01 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B02 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B03 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B04 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B05 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B06 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B07 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-B91 | [矩阵 L226](difftest-routers.md#L226) | U-R |
| R001-B92 | [矩阵 L226](difftest-routers.md#L226) | U-R |
| R001-B93 | [矩阵 L226](difftest-routers.md#L226) | U-R |
| R001-F10 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-F11 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-F12 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-F15 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S01 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S02 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S03 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S04 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S05 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S06 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S07 | [矩阵 L53](difftest-routers.md#L53) | U-R |
| R001-S90 | [矩阵 L226](difftest-routers.md#L226) | U-R |
| R002-B01 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B02 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B03 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B04 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B05 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B06 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-B90 | [矩阵 L227](difftest-routers.md#L227) | U-R |
| R002-B91 | [矩阵 L227](difftest-routers.md#L227) | U-R |
| R002-B92 | [矩阵 L227](difftest-routers.md#L227) | U-R |
| R002-F10 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-F11 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-S01 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-S02 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-S04 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R002-S05 | [矩阵 L54](difftest-routers.md#L54) | U-R |
| R003-B01 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-B02 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-B03 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-B04 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-B05 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-B07 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-F16 | [矩阵 L55](difftest-routers.md#L55) | U-R |
| R003-F91 | [矩阵 L228](difftest-routers.md#L228) | U-R |
| R003-S90 | [矩阵 L228](difftest-routers.md#L228) | U-R |
| R004-B01 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B02 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B03 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B04 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B05 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B06 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B07 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B08 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B09 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B10 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-B90 | [矩阵 L229](difftest-routers.md#L229) | U-R |
| R004-B91 | [矩阵 L229](difftest-routers.md#L229) | U-R |
| R004-B92 | [矩阵 L229](difftest-routers.md#L229) | U-R |
| R004-F10 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-F11 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-F12 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-F13 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-F14 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-F15 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S01 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S02 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S03 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S05 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S06 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S07 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S08 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S09 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R004-S10 | [矩阵 L56](difftest-routers.md#L56) | U-R |
| R005-B01 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B02 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B03 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B04 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B05 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B06 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B07 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B08 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B09 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-B90 | [矩阵 L230](difftest-routers.md#L230) | U-R |
| R005-B91 | [矩阵 L230](difftest-routers.md#L230) | U-R |
| R005-B92 | [矩阵 L230](difftest-routers.md#L230) | U-R |
| R005-F12 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F13 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F15 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F16 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F17 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F18 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F19 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F20 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F21 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F22 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F23 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F24 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F25 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-F26 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S01 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S02 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S03 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S04 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S05 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S06 | [矩阵 L57](difftest-routers.md#L57) | U-R |
| R005-S90 | [矩阵 L230](difftest-routers.md#L230) | U-R |
| R006-B04 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-B05 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-B06 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-B07 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-B90 | [矩阵 L231](difftest-routers.md#L231) | U-R |
| R006-F20 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-F21 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-F23 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-S01 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-S02 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-S03 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R006-S05 | [矩阵 L58](difftest-routers.md#L58) | U-R |
| R007-B01 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B02 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B03 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B04 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B05 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B06 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-B90 | [矩阵 L232](difftest-routers.md#L232) | U-R |
| R007-F10 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-F11 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-S01 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-S02 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-S03 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R007-S04 | [矩阵 L59](difftest-routers.md#L59) | U-R |
| R008-B01 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B02 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B03 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B04 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B05 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B06 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B07 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B08 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-B90 | [矩阵 L233](difftest-routers.md#L233) | U-R |
| R008-B91 | [矩阵 L233](difftest-routers.md#L233) | U-R |
| R008-F10 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F11 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F12 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F13 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F14 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F15 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F16 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F17 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F18 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F19 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F20 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F21 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F22 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F23 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-F91 | [矩阵 L233](difftest-routers.md#L233) | U-R |
| R008-S01 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S02 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S03 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S04 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S05 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S06 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S07 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S08 | [矩阵 L60](difftest-routers.md#L60) | U-R |
| R008-S90 | [矩阵 L233](difftest-routers.md#L233) | U-R |
| R009-B04 | [矩阵 L61](difftest-routers.md#L61) | U-R |
| R009-B90 | [矩阵 L234](difftest-routers.md#L234) | U-R |
| R009-F12 | [矩阵 L61](difftest-routers.md#L61) | U-R |
| R009-F18 | [矩阵 L61](difftest-routers.md#L61) | U-R |
| R009-F91 | [矩阵 L234](difftest-routers.md#L234) | U-R |
| R010-B02 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-B04 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-B05 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-B06 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-B07 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-B90 | [矩阵 L235](difftest-routers.md#L235) | U-R |
| R010-F10 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-F12 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-F13 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-F15 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-F91 | [矩阵 L235](difftest-routers.md#L235) | U-R |
| R010-F92 | [矩阵 L235](difftest-routers.md#L235) | U-R |
| R010-S01 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-S02 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-S03 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-S05 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R010-S06 | [矩阵 L62](difftest-routers.md#L62) | U-R |
| R011-B01 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B02 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B03 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B04 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B05 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B06 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B07 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B08 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B09 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-B90 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R011-B91 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R011-B92 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R011-F10 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F11 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F12 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F13 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F14 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F15 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F16 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F17 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F18 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F19 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F20 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F21 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-F91 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R011-S01 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S02 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S03 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S04 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S05 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S06 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S07 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S08 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S09 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S10 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S11 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S12 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S13 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S14 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S15 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S16 | [矩阵 L63](difftest-routers.md#L63) | U-R |
| R011-S90 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R011-S91 | [矩阵 L236](difftest-routers.md#L236) | U-R |
| R012-B01 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B02 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B03 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B04 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B05 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B06 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B07 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-B90 | [矩阵 L237](difftest-routers.md#L237) | U-R |
| R012-B91 | [矩阵 L237](difftest-routers.md#L237) | U-R |
| R012-F10 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-F11 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-F12 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-F13 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-F14 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S01 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S02 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S03 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S04 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S05 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S06 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S07 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S08 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S09 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S10 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S11 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R012-S12 | [矩阵 L64](difftest-routers.md#L64) | U-R |
| R013-B01 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B02 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B03 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B04 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B05 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B06 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-B90 | [矩阵 L238](difftest-routers.md#L238) | U-R |
| R013-B91 | [矩阵 L238](difftest-routers.md#L238) | U-R |
| R013-F10 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-F11 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-F12 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-F13 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-S01 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-S02 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R013-S03 | [矩阵 L65](difftest-routers.md#L65) | U-R |
| R014-B01 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-B02 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-B03 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-B04 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-B05 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-B90 | [矩阵 L239](difftest-routers.md#L239) | U-R |
| R014-F10 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-F11 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-F12 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-S01 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-S02 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R014-S03 | [矩阵 L66](difftest-routers.md#L66) | U-R |
| R015-B01 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-B02 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-B03 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-B04 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-B05 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-B06 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F10 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F11 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F12 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F13 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F14 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F15 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F16 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F17 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F18 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-F91 | [矩阵 L240](difftest-routers.md#L240) | U-R |
| R015-F92 | [矩阵 L240](difftest-routers.md#L240) | U-R |
| R015-S01 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-S02 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R015-S03 | [矩阵 L67](difftest-routers.md#L67) | U-R |
| R016-B01 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B02 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B03 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B04 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B05 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B06 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-B07 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F10 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F11 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F12 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F13 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F14 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F15 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F16 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-F91 | [矩阵 L241](difftest-routers.md#L241) | U-R |
| R016-F92 | [矩阵 L241](difftest-routers.md#L241) | U-R |
| R016-F93 | [矩阵 L241](difftest-routers.md#L241) | U-R |
| R016-S01 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-S02 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-S03 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R016-S04 | [矩阵 L68](difftest-routers.md#L68) | U-R |
| R017-B01 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-B02 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-B03 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-B05 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-B06 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-B90 | [矩阵 L242](difftest-routers.md#L242) | U-R |
| R017-B91 | [矩阵 L242](difftest-routers.md#L242) | U-R |
| R017-F10 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-F11 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-F12 | [矩阵 L69](difftest-routers.md#L69) | U-R |
| R017-S90 | [矩阵 L242](difftest-routers.md#L242) | U-R |
| R018-B01 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B02 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B03 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B04 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B05 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B06 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-B90 | [矩阵 L243](difftest-routers.md#L243) | U-R |
| R018-F10 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-F11 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-F12 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-F13 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-F14 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S01 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S02 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S03 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S04 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S05 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S06 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S07 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R018-S08 | [矩阵 L70](difftest-routers.md#L70) | U-R |
| R019-B01 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B02 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B03 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B04 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B05 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B06 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-B90 | [矩阵 L244](difftest-routers.md#L244) | U-R |
| R019-F10 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-F11 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-F12 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-F13 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-S01 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-S02 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R019-S03 | [矩阵 L71](difftest-routers.md#L71) | U-R |
| R020-B01 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B02 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B03 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B04 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B05 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B06 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B07 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B08 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B09 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-B90 | [矩阵 L245](difftest-routers.md#L245) | U-R |
| R020-F10 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F11 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F12 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F13 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F14 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F15 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F16 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F17 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F18 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F19 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F20 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F21 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F22 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F23 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F24 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F25 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F26 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F27 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-F91 | [矩阵 L245](difftest-routers.md#L245) | U-R |
| R020-S01 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S02 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S03 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S04 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S05 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S06 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S07 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R020-S08 | [矩阵 L72](difftest-routers.md#L72) | U-R |
| R021-B01 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B02 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B03 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B04 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B05 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B06 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B07 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B08 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-B90 | [矩阵 L246](difftest-routers.md#L246) | U-R |
| R021-F10 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F11 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F12 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F13 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F14 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F15 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F16 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F17 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F18 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F19 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F20 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F21 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F22 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-F23 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S01 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S02 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S03 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S04 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S05 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S06 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S07 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R021-S08 | [矩阵 L73](difftest-routers.md#L73) | U-R |
| R022-B01 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-B02 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-B03 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-B04 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-B05 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-B90 | [矩阵 L247](difftest-routers.md#L247) | U-R |
| R022-F10 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-F11 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-F12 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-F91 | [矩阵 L247](difftest-routers.md#L247) | U-R |
| R022-S01 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-S02 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R022-S03 | [矩阵 L74](difftest-routers.md#L74) | U-R |
| R023-B01 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B02 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B03 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B04 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B05 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B06 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-B90 | [矩阵 L248](difftest-routers.md#L248) | U-R |
| R023-F10 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F11 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F12 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F13 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F14 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F15 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F16 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F17 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F18 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-F91 | [矩阵 L248](difftest-routers.md#L248) | U-R |
| R023-F92 | [矩阵 L248](difftest-routers.md#L248) | U-R |
| R023-S01 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-S02 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R023-S03 | [矩阵 L75](difftest-routers.md#L75) | U-R |
| R024-B01 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-B02 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-B03 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-B04 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-B05 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-B06 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F10 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F11 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F12 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F13 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F14 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F15 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F16 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-F91 | [矩阵 L249](difftest-routers.md#L249) | U-R |
| R024-S01 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S02 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S03 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S04 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S05 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S06 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S07 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S08 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S09 | [矩阵 L76](difftest-routers.md#L76) | U-R |
| R024-S90 | [矩阵 L249](difftest-routers.md#L249) | U-R |
| R025-B01 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-B02 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-B03 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-B04 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-B05 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-B90 | [矩阵 L250](difftest-routers.md#L250) | U-R |
| R025-S01 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-S02 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-S03 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-S04 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R025-S05 | [矩阵 L77](difftest-routers.md#L77) | U-R |
| R026-B01 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-B02 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-B03 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-B04 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-B90 | [矩阵 L251](difftest-routers.md#L251) | U-R |
| R026-F10 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-F11 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-S01 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-S02 | [矩阵 L78](difftest-routers.md#L78) | U-R |
| R026-S03 | [矩阵 L78](difftest-routers.md#L78) | U-R |

### helper（1112项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| H0025 | [矩阵 L321](difftest-routers.md#L321) | U-H |
| H0026 | [矩阵 L322](difftest-routers.md#L322) | U-H |
| H0027 | [矩阵 L323](difftest-routers.md#L323) | U-H |
| H0028 | [矩阵 L324](difftest-routers.md#L324) | U-H |
| H0029 | [矩阵 L325](difftest-routers.md#L325) | U-H |
| H0030 | [矩阵 L331](difftest-routers.md#L331) | U-H |
| H0031 | [矩阵 L332](difftest-routers.md#L332) | U-H |
| H0032 | [矩阵 L338](difftest-routers.md#L338) | U-H |
| H0033 | [矩阵 L339](difftest-routers.md#L339) | U-H |
| H0044 | [矩阵 L350](difftest-routers.md#L350) | U-H |
| H0045 | [矩阵 L351](difftest-routers.md#L351) | U-H |
| H0046 | [矩阵 L352](difftest-routers.md#L352) | U-H |
| H0047 | [矩阵 L353](difftest-routers.md#L353) | U-H |
| H0048 | [矩阵 L354](difftest-routers.md#L354) | U-H |
| H0049 | [矩阵 L355](difftest-routers.md#L355) | U-H |
| H0050 | [矩阵 L356](difftest-routers.md#L356) | U-H |
| H0051 | [矩阵 L357](difftest-routers.md#L357) | U-H |
| H0052 | [矩阵 L358](difftest-routers.md#L358) | U-H |
| H0053 | [矩阵 L359](difftest-routers.md#L359) | U-H |
| H0054 | [矩阵 L360](difftest-routers.md#L360) | U-H |
| H0055 | [矩阵 L361](difftest-routers.md#L361) | U-H |
| H0056 | [矩阵 L362](difftest-routers.md#L362) | U-H |
| H0057 | [矩阵 L363](difftest-routers.md#L363) | U-H |
| H0058 | [矩阵 L364](difftest-routers.md#L364) | U-H |
| H0059 | [矩阵 L365](difftest-routers.md#L365) | U-H |
| H0060 | [矩阵 L366](difftest-routers.md#L366) | U-H |
| H0062 | [矩阵 L368](difftest-routers.md#L368) | U-H |
| H0063 | [矩阵 L374](difftest-routers.md#L374) | U-H |
| H0064 | [矩阵 L375](difftest-routers.md#L375) | U-H |
| H0065 | [矩阵 L376](difftest-routers.md#L376) | U-H |
| H0066 | [矩阵 L377](difftest-routers.md#L377) | U-H |
| H0067 | [矩阵 L378](difftest-routers.md#L378) | U-H |
| H0069 | [矩阵 L385](difftest-routers.md#L385) | U-H |
| H0070 | [矩阵 L386](difftest-routers.md#L386) | U-H |
| H0116 | [矩阵 L442](difftest-routers.md#L442) | U-H |
| H0117 | [矩阵 L443](difftest-routers.md#L443) | U-H |
| H0118 | [矩阵 L444](difftest-routers.md#L444) | U-H |
| H0119 | [矩阵 L450](difftest-routers.md#L450) | U-H |
| H0120 | [矩阵 L451](difftest-routers.md#L451) | U-H |
| H0121 | [矩阵 L452](difftest-routers.md#L452) | U-H |
| H0122 | [矩阵 L453](difftest-routers.md#L453) | U-H |
| H0123 | [矩阵 L454](difftest-routers.md#L454) | U-H |
| H0124 | [矩阵 L455](difftest-routers.md#L455) | U-H |
| H0125 | [矩阵 L456](difftest-routers.md#L456) | U-H |
| H0126 | [矩阵 L457](difftest-routers.md#L457) | U-H |
| H0127 | [矩阵 L463](difftest-routers.md#L463) | U-H |
| H0132 | [矩阵 L473](difftest-routers.md#L473) | U-H |
| H0133 | [矩阵 L474](difftest-routers.md#L474) | U-H |
| H0134 | [矩阵 L475](difftest-routers.md#L475) | U-H |
| H0135 | [矩阵 L476](difftest-routers.md#L476) | U-H |
| H0136 | [矩阵 L477](difftest-routers.md#L477) | U-H |
| H0137 | [矩阵 L478](difftest-routers.md#L478) | U-H |
| H0138 | [矩阵 L479](difftest-routers.md#L479) | U-H |
| H0139 | [矩阵 L480](difftest-routers.md#L480) | U-H |
| H0162 | [矩阵 L513](difftest-routers.md#L513) | U-H |
| H0163 | [矩阵 L514](difftest-routers.md#L514) | U-H |
| H0164 | [矩阵 L515](difftest-routers.md#L515) | U-H |
| H0165 | [矩阵 L516](difftest-routers.md#L516) | U-H |
| H0166 | [矩阵 L517](difftest-routers.md#L517) | U-H |
| H0167 | [矩阵 L518](difftest-routers.md#L518) | U-H |
| H0168 | [矩阵 L519](difftest-routers.md#L519) | U-H |
| H0169 | [矩阵 L520](difftest-routers.md#L520) | U-H |
| H0170 | [矩阵 L521](difftest-routers.md#L521) | U-H |
| H0171 | [矩阵 L522](difftest-routers.md#L522) | U-H |
| H0172 | [矩阵 L523](difftest-routers.md#L523) | U-H |
| H0173 | [矩阵 L524](difftest-routers.md#L524) | U-H |
| H0174 | [矩阵 L525](difftest-routers.md#L525) | U-H |
| H0175 | [矩阵 L526](difftest-routers.md#L526) | U-H |
| H0176 | [矩阵 L527](difftest-routers.md#L527) | U-H |
| H0177 | [矩阵 L528](difftest-routers.md#L528) | U-H |
| H0178 | [矩阵 L529](difftest-routers.md#L529) | U-H |
| H0179 | [矩阵 L530](difftest-routers.md#L530) | U-H |
| H0180 | [矩阵 L531](difftest-routers.md#L531) | U-H |
| H0181 | [矩阵 L532](difftest-routers.md#L532) | U-H |
| H0182 | [矩阵 L533](difftest-routers.md#L533) | U-H |
| H0183 | [矩阵 L534](difftest-routers.md#L534) | U-H |
| H0184 | [矩阵 L535](difftest-routers.md#L535) | U-H |
| H0185 | [矩阵 L536](difftest-routers.md#L536) | U-H |
| H0186 | [矩阵 L537](difftest-routers.md#L537) | U-H |
| H0187 | [矩阵 L538](difftest-routers.md#L538) | U-H |
| H0188 | [矩阵 L539](difftest-routers.md#L539) | U-H |
| H0189 | [矩阵 L540](difftest-routers.md#L540) | U-H |
| H0190 | [矩阵 L541](difftest-routers.md#L541) | U-H |
| H0191 | [矩阵 L542](difftest-routers.md#L542) | U-H |
| H0192 | [矩阵 L543](difftest-routers.md#L543) | U-H |
| H0193 | [矩阵 L544](difftest-routers.md#L544) | U-H |
| H0194 | [矩阵 L545](difftest-routers.md#L545) | U-H |
| H0195 | [矩阵 L546](difftest-routers.md#L546) | U-H |
| H0196 | [矩阵 L547](difftest-routers.md#L547) | U-H |
| H0197 | [矩阵 L548](difftest-routers.md#L548) | U-H |
| H0198 | [矩阵 L549](difftest-routers.md#L549) | U-H |
| H0199 | [矩阵 L555](difftest-routers.md#L555) | U-H |
| H0200 | [矩阵 L556](difftest-routers.md#L556) | U-H |
| H0201 | [矩阵 L557](difftest-routers.md#L557) | U-H |
| H0202 | [矩阵 L558](difftest-routers.md#L558) | U-H |
| H0203 | [矩阵 L559](difftest-routers.md#L559) | U-H |
| H0204 | [矩阵 L560](difftest-routers.md#L560) | U-H |
| H0205 | [矩阵 L561](difftest-routers.md#L561) | U-H |
| H0206 | [矩阵 L562](difftest-routers.md#L562) | U-H |
| H0207 | [矩阵 L563](difftest-routers.md#L563) | U-H |
| H0208 | [矩阵 L564](difftest-routers.md#L564) | U-H |
| H0209 | [矩阵 L570](difftest-routers.md#L570) | U-H |
| H0210 | [矩阵 L571](difftest-routers.md#L571) | U-H |
| H0228 | [矩阵 L589](difftest-routers.md#L589) | U-H |
| H0229 | [矩阵 L590](difftest-routers.md#L590) | U-H |
| H0238 | [矩阵 L599](difftest-routers.md#L599) | U-H |
| H0239 | [矩阵 L600](difftest-routers.md#L600) | U-H |
| H0240 | [矩阵 L601](difftest-routers.md#L601) | U-H |
| H0243 | [矩阵 L604](difftest-routers.md#L604) | U-H |
| H0244 | [矩阵 L605](difftest-routers.md#L605) | U-H |
| H0245 | [矩阵 L606](difftest-routers.md#L606) | U-H |
| H0246 | [矩阵 L607](difftest-routers.md#L607) | U-H |
| H0267 | [矩阵 L628](difftest-routers.md#L628) | U-H |
| H0280 | [矩阵 L641](difftest-routers.md#L641) | U-H |
| H0281 | [矩阵 L642](difftest-routers.md#L642) | U-H |
| H0282 | [矩阵 L643](difftest-routers.md#L643) | U-H |
| H0283 | [矩阵 L644](difftest-routers.md#L644) | U-H |
| H0284 | [矩阵 L645](difftest-routers.md#L645) | U-H |
| H0285 | [矩阵 L646](difftest-routers.md#L646) | U-H |
| H0286 | [矩阵 L647](difftest-routers.md#L647) | U-H |
| H0287 | [矩阵 L648](difftest-routers.md#L648) | U-H |
| H0288 | [矩阵 L649](difftest-routers.md#L649) | U-H |
| H0289 | [矩阵 L650](difftest-routers.md#L650) | U-H |
| H0290 | [矩阵 L651](difftest-routers.md#L651) | U-H |
| H0291 | [矩阵 L652](difftest-routers.md#L652) | U-H |
| H0292 | [矩阵 L653](difftest-routers.md#L653) | U-H |
| H0293 | [矩阵 L654](difftest-routers.md#L654) | U-H |
| H0294 | [矩阵 L655](difftest-routers.md#L655) | U-H |
| H0295 | [矩阵 L656](difftest-routers.md#L656) | U-H |
| H0296 | [矩阵 L657](difftest-routers.md#L657) | U-H |
| H0297 | [矩阵 L658](difftest-routers.md#L658) | U-H |
| H0298 | [矩阵 L659](difftest-routers.md#L659) | U-H |
| H0299 | [矩阵 L660](difftest-routers.md#L660) | U-H |
| H0300 | [矩阵 L661](difftest-routers.md#L661) | U-H |
| H0301 | [矩阵 L662](difftest-routers.md#L662) | U-H |
| H0302 | [矩阵 L663](difftest-routers.md#L663) | U-H |
| H0303 | [矩阵 L664](difftest-routers.md#L664) | U-H |
| H0304 | [矩阵 L665](difftest-routers.md#L665) | U-H |
| H0305 | [矩阵 L666](difftest-routers.md#L666) | U-H |
| H0306 | [矩阵 L667](difftest-routers.md#L667) | U-H |
| H0307 | [矩阵 L668](difftest-routers.md#L668) | U-H |
| H0308 | [矩阵 L669](difftest-routers.md#L669) | U-H |
| H0309 | [矩阵 L670](difftest-routers.md#L670) | U-H |
| H0310 | [矩阵 L671](difftest-routers.md#L671) | U-H |
| H0311 | [矩阵 L672](difftest-routers.md#L672) | U-H |
| H0312 | [矩阵 L673](difftest-routers.md#L673) | U-H |
| H0313 | [矩阵 L674](difftest-routers.md#L674) | U-H |
| H0314 | [矩阵 L675](difftest-routers.md#L675) | U-H |
| H0315 | [矩阵 L676](difftest-routers.md#L676) | U-H |
| H0316 | [矩阵 L677](difftest-routers.md#L677) | U-H |
| H0317 | [矩阵 L678](difftest-routers.md#L678) | U-H |
| H0318 | [矩阵 L679](difftest-routers.md#L679) | U-H |
| H0319 | [矩阵 L680](difftest-routers.md#L680) | U-H |
| H0320 | [矩阵 L681](difftest-routers.md#L681) | U-H |
| H0321 | [矩阵 L682](difftest-routers.md#L682) | U-H |
| H0322 | [矩阵 L683](difftest-routers.md#L683) | U-H |
| H0323 | [矩阵 L684](difftest-routers.md#L684) | U-H |
| H0324 | [矩阵 L685](difftest-routers.md#L685) | U-H |
| H0325 | [矩阵 L686](difftest-routers.md#L686) | U-H |
| H0326 | [矩阵 L692](difftest-routers.md#L692) | U-H |
| H0327 | [矩阵 L693](difftest-routers.md#L693) | U-H |
| H0328 | [矩阵 L694](difftest-routers.md#L694) | U-H |
| H0329 | [矩阵 L695](difftest-routers.md#L695) | U-H |
| H0330 | [矩阵 L696](difftest-routers.md#L696) | U-H |
| H0331 | [矩阵 L697](difftest-routers.md#L697) | U-H |
| H0332 | [矩阵 L698](difftest-routers.md#L698) | U-H |
| H0333 | [矩阵 L704](difftest-routers.md#L704) | U-H |
| H0334 | [矩阵 L705](difftest-routers.md#L705) | U-H |
| H0335 | [矩阵 L706](difftest-routers.md#L706) | U-H |
| H0336 | [矩阵 L707](difftest-routers.md#L707) | U-H |
| H0337 | [矩阵 L708](difftest-routers.md#L708) | U-H |
| H0338 | [矩阵 L709](difftest-routers.md#L709) | U-H |
| H0339 | [矩阵 L710](difftest-routers.md#L710) | U-H |
| H0340 | [矩阵 L711](difftest-routers.md#L711) | U-H |
| H0341 | [矩阵 L712](difftest-routers.md#L712) | U-H |
| H0342 | [矩阵 L713](difftest-routers.md#L713) | U-H |
| H0343 | [矩阵 L714](difftest-routers.md#L714) | U-H |
| H0344 | [矩阵 L715](difftest-routers.md#L715) | U-H |
| H0345 | [矩阵 L716](difftest-routers.md#L716) | U-H |
| H0346 | [矩阵 L717](difftest-routers.md#L717) | U-H |
| H0347 | [矩阵 L718](difftest-routers.md#L718) | U-H |
| H0348 | [矩阵 L719](difftest-routers.md#L719) | U-H |
| H0349 | [矩阵 L720](difftest-routers.md#L720) | U-H |
| H0350 | [矩阵 L721](difftest-routers.md#L721) | U-H |
| H0351 | [矩阵 L722](difftest-routers.md#L722) | U-H |
| H0352 | [矩阵 L723](difftest-routers.md#L723) | U-H |
| H0353 | [矩阵 L724](difftest-routers.md#L724) | U-H |
| H0354 | [矩阵 L725](difftest-routers.md#L725) | U-H |
| H0355 | [矩阵 L726](difftest-routers.md#L726) | U-H |
| H0356 | [矩阵 L727](difftest-routers.md#L727) | U-H |
| H0357 | [矩阵 L728](difftest-routers.md#L728) | U-H |
| H0358 | [矩阵 L729](difftest-routers.md#L729) | U-H |
| H0359 | [矩阵 L730](difftest-routers.md#L730) | U-H |
| H0360 | [矩阵 L731](difftest-routers.md#L731) | U-H |
| H0361 | [矩阵 L732](difftest-routers.md#L732) | U-H |
| H0362 | [矩阵 L733](difftest-routers.md#L733) | U-H |
| H0363 | [矩阵 L734](difftest-routers.md#L734) | U-H |
| H0364 | [矩阵 L735](difftest-routers.md#L735) | U-H |
| H0365 | [矩阵 L736](difftest-routers.md#L736) | U-H |
| H0366 | [矩阵 L737](difftest-routers.md#L737) | U-H |
| H0367 | [矩阵 L738](difftest-routers.md#L738) | U-H |
| H0368 | [矩阵 L739](difftest-routers.md#L739) | U-H |
| H0369 | [矩阵 L740](difftest-routers.md#L740) | U-H |
| H0370 | [矩阵 L741](difftest-routers.md#L741) | U-H |
| H0371 | [矩阵 L742](difftest-routers.md#L742) | U-H |
| H0372 | [矩阵 L748](difftest-routers.md#L748) | U-H |
| H0373 | [矩阵 L749](difftest-routers.md#L749) | U-H |
| H0374 | [矩阵 L750](difftest-routers.md#L750) | U-H |
| H0375 | [矩阵 L751](difftest-routers.md#L751) | U-H |
| H0376 | [矩阵 L752](difftest-routers.md#L752) | U-H |
| H0377 | [矩阵 L753](difftest-routers.md#L753) | U-H |
| H0378 | [矩阵 L754](difftest-routers.md#L754) | U-H |
| H0379 | [矩阵 L755](difftest-routers.md#L755) | U-H |
| H0380 | [矩阵 L756](difftest-routers.md#L756) | U-H |
| H0381 | [矩阵 L757](difftest-routers.md#L757) | U-H |
| H0382 | [矩阵 L763](difftest-routers.md#L763) | U-H |
| H0383 | [矩阵 L764](difftest-routers.md#L764) | U-H |
| H0384 | [矩阵 L765](difftest-routers.md#L765) | U-H |
| H0385 | [矩阵 L766](difftest-routers.md#L766) | U-H |
| H0386 | [矩阵 L767](difftest-routers.md#L767) | U-H |
| H0387 | [矩阵 L768](difftest-routers.md#L768) | U-H |
| H0388 | [矩阵 L769](difftest-routers.md#L769) | U-H |
| H0389 | [矩阵 L770](difftest-routers.md#L770) | U-H |
| H0390 | [矩阵 L771](difftest-routers.md#L771) | U-H |
| H0391 | [矩阵 L772](difftest-routers.md#L772) | U-H |
| H0392 | [矩阵 L773](difftest-routers.md#L773) | U-H |
| H0393 | [矩阵 L774](difftest-routers.md#L774) | U-H |
| H0394 | [矩阵 L775](difftest-routers.md#L775) | U-H |
| H0395 | [矩阵 L776](difftest-routers.md#L776) | U-H |
| H0396 | [矩阵 L777](difftest-routers.md#L777) | U-H |
| H0397 | [矩阵 L778](difftest-routers.md#L778) | U-H |
| H0398 | [矩阵 L779](difftest-routers.md#L779) | U-H |
| H0399 | [矩阵 L780](difftest-routers.md#L780) | U-H |
| H0400 | [矩阵 L781](difftest-routers.md#L781) | U-H |
| H0401 | [矩阵 L782](difftest-routers.md#L782) | U-H |
| H0402 | [矩阵 L783](difftest-routers.md#L783) | U-H |
| H0403 | [矩阵 L784](difftest-routers.md#L784) | U-H |
| H0404 | [矩阵 L785](difftest-routers.md#L785) | U-H |
| H0405 | [矩阵 L786](difftest-routers.md#L786) | U-H |
| H0406 | [矩阵 L787](difftest-routers.md#L787) | U-H |
| H0407 | [矩阵 L788](difftest-routers.md#L788) | U-H |
| H0408 | [矩阵 L789](difftest-routers.md#L789) | U-H |
| H0409 | [矩阵 L790](difftest-routers.md#L790) | U-H |
| H0410 | [矩阵 L791](difftest-routers.md#L791) | U-H |
| H0411 | [矩阵 L792](difftest-routers.md#L792) | U-H |
| H0412 | [矩阵 L793](difftest-routers.md#L793) | U-H |
| H0413 | [矩阵 L794](difftest-routers.md#L794) | U-H |
| H0414 | [矩阵 L795](difftest-routers.md#L795) | U-H |
| H0415 | [矩阵 L796](difftest-routers.md#L796) | U-H |
| H0416 | [矩阵 L797](difftest-routers.md#L797) | U-H |
| H0417 | [矩阵 L798](difftest-routers.md#L798) | U-H |
| H0418 | [矩阵 L799](difftest-routers.md#L799) | U-H |
| H0419 | [矩阵 L800](difftest-routers.md#L800) | U-H |
| H0420 | [矩阵 L801](difftest-routers.md#L801) | U-H |
| H0421 | [矩阵 L802](difftest-routers.md#L802) | U-H |
| H0422 | [矩阵 L803](difftest-routers.md#L803) | U-H |
| H0423 | [矩阵 L809](difftest-routers.md#L809) | U-H |
| H0424 | [矩阵 L810](difftest-routers.md#L810) | U-H |
| H0425 | [矩阵 L811](difftest-routers.md#L811) | U-H |
| H0426 | [矩阵 L812](difftest-routers.md#L812) | U-H |
| H0427 | [矩阵 L813](difftest-routers.md#L813) | U-H |
| H0428 | [矩阵 L814](difftest-routers.md#L814) | U-H |
| H0429 | [矩阵 L815](difftest-routers.md#L815) | U-H |
| H0430 | [矩阵 L816](difftest-routers.md#L816) | U-H |
| H0431 | [矩阵 L817](difftest-routers.md#L817) | U-H |
| H0432 | [矩阵 L818](difftest-routers.md#L818) | U-H |
| H0433 | [矩阵 L819](difftest-routers.md#L819) | U-H |
| H0434 | [矩阵 L820](difftest-routers.md#L820) | U-H |
| H0435 | [矩阵 L821](difftest-routers.md#L821) | U-H |
| H0436 | [矩阵 L822](difftest-routers.md#L822) | U-H |
| H0437 | [矩阵 L823](difftest-routers.md#L823) | U-H |
| H0438 | [矩阵 L824](difftest-routers.md#L824) | U-H |
| H0439 | [矩阵 L825](difftest-routers.md#L825) | U-H |
| H0440 | [矩阵 L826](difftest-routers.md#L826) | U-H |
| H0441 | [矩阵 L827](difftest-routers.md#L827) | U-H |
| H0442 | [矩阵 L828](difftest-routers.md#L828) | U-H |
| H0443 | [矩阵 L829](difftest-routers.md#L829) | U-H |
| H0444 | [矩阵 L830](difftest-routers.md#L830) | U-H |
| H0445 | [矩阵 L831](difftest-routers.md#L831) | U-H |
| H0446 | [矩阵 L832](difftest-routers.md#L832) | U-H |
| H0447 | [矩阵 L833](difftest-routers.md#L833) | U-H |
| H0448 | [矩阵 L834](difftest-routers.md#L834) | U-H |
| H0449 | [矩阵 L835](difftest-routers.md#L835) | U-H |
| H0450 | [矩阵 L836](difftest-routers.md#L836) | U-H |
| H0451 | [矩阵 L837](difftest-routers.md#L837) | U-H |
| H0452 | [矩阵 L843](difftest-routers.md#L843) | U-H |
| H0453 | [矩阵 L844](difftest-routers.md#L844) | U-H |
| H0454 | [矩阵 L845](difftest-routers.md#L845) | U-H |
| H0455 | [矩阵 L846](difftest-routers.md#L846) | U-H |
| H0456 | [矩阵 L847](difftest-routers.md#L847) | U-H |
| H0457 | [矩阵 L848](difftest-routers.md#L848) | U-H |
| H0458 | [矩阵 L849](difftest-routers.md#L849) | U-H |
| H0459 | [矩阵 L850](difftest-routers.md#L850) | U-H |
| H0460 | [矩阵 L851](difftest-routers.md#L851) | U-H |
| H0461 | [矩阵 L852](difftest-routers.md#L852) | U-H |
| H0462 | [矩阵 L853](difftest-routers.md#L853) | U-H |
| H0463 | [矩阵 L854](difftest-routers.md#L854) | U-H |
| H0464 | [矩阵 L855](difftest-routers.md#L855) | U-H |
| H0465 | [矩阵 L856](difftest-routers.md#L856) | U-H |
| H0466 | [矩阵 L857](difftest-routers.md#L857) | U-H |
| H0467 | [矩阵 L858](difftest-routers.md#L858) | U-H |
| H0468 | [矩阵 L859](difftest-routers.md#L859) | U-H |
| H0469 | [矩阵 L860](difftest-routers.md#L860) | U-H |
| H0470 | [矩阵 L861](difftest-routers.md#L861) | U-H |
| H0471 | [矩阵 L862](difftest-routers.md#L862) | U-H |
| H0472 | [矩阵 L863](difftest-routers.md#L863) | U-H |
| H0473 | [矩阵 L864](difftest-routers.md#L864) | U-H |
| H0474 | [矩阵 L865](difftest-routers.md#L865) | U-H |
| H0475 | [矩阵 L866](difftest-routers.md#L866) | U-H |
| H0476 | [矩阵 L867](difftest-routers.md#L867) | U-H |
| H0477 | [矩阵 L868](difftest-routers.md#L868) | U-H |
| H0478 | [矩阵 L869](difftest-routers.md#L869) | U-H |
| H0479 | [矩阵 L870](difftest-routers.md#L870) | U-H |
| H0480 | [矩阵 L871](difftest-routers.md#L871) | U-H |
| H0481 | [矩阵 L872](difftest-routers.md#L872) | U-H |
| H0482 | [矩阵 L873](difftest-routers.md#L873) | U-H |
| H0483 | [矩阵 L874](difftest-routers.md#L874) | U-H |
| H0484 | [矩阵 L875](difftest-routers.md#L875) | U-H |
| H0485 | [矩阵 L876](difftest-routers.md#L876) | U-H |
| H0486 | [矩阵 L877](difftest-routers.md#L877) | U-H |
| H0487 | [矩阵 L878](difftest-routers.md#L878) | U-H |
| H0488 | [矩阵 L879](difftest-routers.md#L879) | U-H |
| H0489 | [矩阵 L880](difftest-routers.md#L880) | U-H |
| H0490 | [矩阵 L881](difftest-routers.md#L881) | U-H |
| H0493 | [矩阵 L889](difftest-routers.md#L889) | U-H |
| H0494 | [矩阵 L890](difftest-routers.md#L890) | U-H |
| H0495 | [矩阵 L891](difftest-routers.md#L891) | U-H |
| H0496 | [矩阵 L892](difftest-routers.md#L892) | U-H |
| H0497 | [矩阵 L893](difftest-routers.md#L893) | U-H |
| H0498 | [矩阵 L894](difftest-routers.md#L894) | U-H |
| H0499 | [矩阵 L895](difftest-routers.md#L895) | U-H |
| H0500 | [矩阵 L896](difftest-routers.md#L896) | U-H |
| H0501 | [矩阵 L897](difftest-routers.md#L897) | U-H |
| H0502 | [矩阵 L898](difftest-routers.md#L898) | U-H |
| H0503 | [矩阵 L899](difftest-routers.md#L899) | U-H |
| H0504 | [矩阵 L900](difftest-routers.md#L900) | U-H |
| H0505 | [矩阵 L901](difftest-routers.md#L901) | U-H |
| H0506 | [矩阵 L902](difftest-routers.md#L902) | U-H |
| H0507 | [矩阵 L903](difftest-routers.md#L903) | U-H |
| H0508 | [矩阵 L904](difftest-routers.md#L904) | U-H |
| H0509 | [矩阵 L905](difftest-routers.md#L905) | U-H |
| H0510 | [矩阵 L906](difftest-routers.md#L906) | U-H |
| H0511 | [矩阵 L907](difftest-routers.md#L907) | U-H |
| H0512 | [矩阵 L908](difftest-routers.md#L908) | U-H |
| H0513 | [矩阵 L909](difftest-routers.md#L909) | U-H |
| H0514 | [矩阵 L910](difftest-routers.md#L910) | U-H |
| H0515 | [矩阵 L911](difftest-routers.md#L911) | U-H |
| H0516 | [矩阵 L912](difftest-routers.md#L912) | U-H |
| H0517 | [矩阵 L913](difftest-routers.md#L913) | U-H |
| H0518 | [矩阵 L914](difftest-routers.md#L914) | U-H |
| H0519 | [矩阵 L915](difftest-routers.md#L915) | U-H |
| H0520 | [矩阵 L916](difftest-routers.md#L916) | U-H |
| H0521 | [矩阵 L917](difftest-routers.md#L917) | U-H |
| H0522 | [矩阵 L918](difftest-routers.md#L918) | U-H |
| H0523 | [矩阵 L919](difftest-routers.md#L919) | U-H |
| H0524 | [矩阵 L920](difftest-routers.md#L920) | U-H |
| H0525 | [矩阵 L921](difftest-routers.md#L921) | U-H |
| H0526 | [矩阵 L922](difftest-routers.md#L922) | U-H |
| H0527 | [矩阵 L923](difftest-routers.md#L923) | U-H |
| H0528 | [矩阵 L924](difftest-routers.md#L924) | U-H |
| H0529 | [矩阵 L925](difftest-routers.md#L925) | U-H |
| H0530 | [矩阵 L931](difftest-routers.md#L931) | U-H |
| H0531 | [矩阵 L932](difftest-routers.md#L932) | U-H |
| H0532 | [矩阵 L933](difftest-routers.md#L933) | U-H |
| H0533 | [矩阵 L934](difftest-routers.md#L934) | U-H |
| H0534 | [矩阵 L935](difftest-routers.md#L935) | U-H |
| H0535 | [矩阵 L936](difftest-routers.md#L936) | U-H |
| H0536 | [矩阵 L937](difftest-routers.md#L937) | U-H |
| H0537 | [矩阵 L938](difftest-routers.md#L938) | U-H |
| H0538 | [矩阵 L939](difftest-routers.md#L939) | U-H |
| H0539 | [矩阵 L940](difftest-routers.md#L940) | U-H |
| H0540 | [矩阵 L941](difftest-routers.md#L941) | U-H |
| H0541 | [矩阵 L942](difftest-routers.md#L942) | U-H |
| H0542 | [矩阵 L943](difftest-routers.md#L943) | U-H |
| H0543 | [矩阵 L944](difftest-routers.md#L944) | U-H |
| H0544 | [矩阵 L945](difftest-routers.md#L945) | U-H |
| H0545 | [矩阵 L946](difftest-routers.md#L946) | U-H |
| H0546 | [矩阵 L947](difftest-routers.md#L947) | U-H |
| H0547 | [矩阵 L948](difftest-routers.md#L948) | U-H |
| H0548 | [矩阵 L954](difftest-routers.md#L954) | U-H |
| H0549 | [矩阵 L955](difftest-routers.md#L955) | U-H |
| H0550 | [矩阵 L956](difftest-routers.md#L956) | U-H |
| H0551 | [矩阵 L957](difftest-routers.md#L957) | U-H |
| H0552 | [矩阵 L958](difftest-routers.md#L958) | U-H |
| H0553 | [矩阵 L959](difftest-routers.md#L959) | U-H |
| H0554 | [矩阵 L960](difftest-routers.md#L960) | U-H |
| H0555 | [矩阵 L961](difftest-routers.md#L961) | U-H |
| H0556 | [矩阵 L962](difftest-routers.md#L962) | U-H |
| H0557 | [矩阵 L963](difftest-routers.md#L963) | U-H |
| H0558 | [矩阵 L964](difftest-routers.md#L964) | U-H |
| H0559 | [矩阵 L965](difftest-routers.md#L965) | U-H |
| H0560 | [矩阵 L966](difftest-routers.md#L966) | U-H |
| H0561 | [矩阵 L967](difftest-routers.md#L967) | U-H |
| H0562 | [矩阵 L968](difftest-routers.md#L968) | U-H |
| H0563 | [矩阵 L969](difftest-routers.md#L969) | U-H |
| H0564 | [矩阵 L970](difftest-routers.md#L970) | U-H |
| H0565 | [矩阵 L971](difftest-routers.md#L971) | U-H |
| H0566 | [矩阵 L972](difftest-routers.md#L972) | U-H |
| H0567 | [矩阵 L978](difftest-routers.md#L978) | U-H |
| H0568 | [矩阵 L979](difftest-routers.md#L979) | U-H |
| H0569 | [矩阵 L980](difftest-routers.md#L980) | U-H |
| H0570 | [矩阵 L981](difftest-routers.md#L981) | U-H |
| H0571 | [矩阵 L982](difftest-routers.md#L982) | U-H |
| H0572 | [矩阵 L983](difftest-routers.md#L983) | U-H |
| H0573 | [矩阵 L984](difftest-routers.md#L984) | U-H |
| H0574 | [矩阵 L985](difftest-routers.md#L985) | U-H |
| H0575 | [矩阵 L986](difftest-routers.md#L986) | U-H |
| H0576 | [矩阵 L987](difftest-routers.md#L987) | U-H |
| H0577 | [矩阵 L988](difftest-routers.md#L988) | U-H |
| H0578 | [矩阵 L989](difftest-routers.md#L989) | U-H |
| H0579 | [矩阵 L990](difftest-routers.md#L990) | U-H |
| H0580 | [矩阵 L991](difftest-routers.md#L991) | U-H |
| H0581 | [矩阵 L992](difftest-routers.md#L992) | U-H |
| H0582 | [矩阵 L993](difftest-routers.md#L993) | U-H |
| H0583 | [矩阵 L994](difftest-routers.md#L994) | U-H |
| H0584 | [矩阵 L995](difftest-routers.md#L995) | U-H |
| H0585 | [矩阵 L996](difftest-routers.md#L996) | U-H |
| H0586 | [矩阵 L997](difftest-routers.md#L997) | U-H |
| H0587 | [矩阵 L998](difftest-routers.md#L998) | U-H |
| H0588 | [矩阵 L999](difftest-routers.md#L999) | U-H |
| H0589 | [矩阵 L1000](difftest-routers.md#L1000) | U-H |
| H0590 | [矩阵 L1001](difftest-routers.md#L1001) | U-H |
| H0591 | [矩阵 L1002](difftest-routers.md#L1002) | U-H |
| H0594 | [矩阵 L1015](difftest-routers.md#L1015) | U-H |
| H0595 | [矩阵 L1016](difftest-routers.md#L1016) | U-H |
| H0596 | [矩阵 L1017](difftest-routers.md#L1017) | U-H |
| H0597 | [矩阵 L1018](difftest-routers.md#L1018) | U-H |
| H0598 | [矩阵 L1024](difftest-routers.md#L1024) | U-H |
| H0599 | [矩阵 L1025](difftest-routers.md#L1025) | U-H |
| H0600 | [矩阵 L1026](difftest-routers.md#L1026) | U-H |
| H0601 | [矩阵 L1027](difftest-routers.md#L1027) | U-H |
| H0602 | [矩阵 L1028](difftest-routers.md#L1028) | U-H |
| H0603 | [矩阵 L1029](difftest-routers.md#L1029) | U-H |
| H0604 | [矩阵 L1030](difftest-routers.md#L1030) | U-H |
| H0605 | [矩阵 L1031](difftest-routers.md#L1031) | U-H |
| H0606 | [矩阵 L1032](difftest-routers.md#L1032) | U-H |
| H0607 | [矩阵 L1033](difftest-routers.md#L1033) | U-H |
| H0608 | [矩阵 L1034](difftest-routers.md#L1034) | U-H |
| H0609 | [矩阵 L1035](difftest-routers.md#L1035) | U-H |
| H0610 | [矩阵 L1036](difftest-routers.md#L1036) | U-H |
| H0611 | [矩阵 L1037](difftest-routers.md#L1037) | U-H |
| H0612 | [矩阵 L1038](difftest-routers.md#L1038) | U-H |
| H0613 | [矩阵 L1039](difftest-routers.md#L1039) | U-H |
| H0614 | [矩阵 L1040](difftest-routers.md#L1040) | U-H |
| H0615 | [矩阵 L1041](difftest-routers.md#L1041) | U-H |
| H0616 | [矩阵 L1042](difftest-routers.md#L1042) | U-H |
| H0617 | [矩阵 L1043](difftest-routers.md#L1043) | U-H |
| H0618 | [矩阵 L1044](difftest-routers.md#L1044) | U-H |
| H0619 | [矩阵 L1045](difftest-routers.md#L1045) | U-H |
| H0620 | [矩阵 L1046](difftest-routers.md#L1046) | U-H |
| H0621 | [矩阵 L1047](difftest-routers.md#L1047) | U-H |
| H0622 | [矩阵 L1048](difftest-routers.md#L1048) | U-H |
| H0623 | [矩阵 L1049](difftest-routers.md#L1049) | U-H |
| H0624 | [矩阵 L1050](difftest-routers.md#L1050) | U-H |
| H0625 | [矩阵 L1051](difftest-routers.md#L1051) | U-H |
| H0626 | [矩阵 L1057](difftest-routers.md#L1057) | U-H |
| H0627 | [矩阵 L1063](difftest-routers.md#L1063) | U-H |
| H0628 | [矩阵 L1064](difftest-routers.md#L1064) | U-H |
| H0629 | [矩阵 L1065](difftest-routers.md#L1065) | U-H |
| H0630 | [矩阵 L1066](difftest-routers.md#L1066) | U-H |
| H0631 | [矩阵 L1067](difftest-routers.md#L1067) | U-H |
| H0632 | [矩阵 L1068](difftest-routers.md#L1068) | U-H |
| H0633 | [矩阵 L1069](difftest-routers.md#L1069) | U-H |
| H0634 | [矩阵 L1070](difftest-routers.md#L1070) | U-H |
| H0635 | [矩阵 L1071](difftest-routers.md#L1071) | U-H |
| H0636 | [矩阵 L1072](difftest-routers.md#L1072) | U-H |
| H0637 | [矩阵 L1073](difftest-routers.md#L1073) | U-H |
| H0638 | [矩阵 L1074](difftest-routers.md#L1074) | U-H |
| H0639 | [矩阵 L1075](difftest-routers.md#L1075) | U-H |
| H0640 | [矩阵 L1076](difftest-routers.md#L1076) | U-H |
| H0641 | [矩阵 L1077](difftest-routers.md#L1077) | U-H |
| H0642 | [矩阵 L1078](difftest-routers.md#L1078) | U-H |
| H0643 | [矩阵 L1079](difftest-routers.md#L1079) | U-H |
| H0644 | [矩阵 L1080](difftest-routers.md#L1080) | U-H |
| H0645 | [矩阵 L1081](difftest-routers.md#L1081) | U-H |
| H0646 | [矩阵 L1082](difftest-routers.md#L1082) | U-H |
| H0647 | [矩阵 L1083](difftest-routers.md#L1083) | U-H |
| H0648 | [矩阵 L1084](difftest-routers.md#L1084) | U-H |
| H0649 | [矩阵 L1085](difftest-routers.md#L1085) | U-H |
| H0650 | [矩阵 L1086](difftest-routers.md#L1086) | U-H |
| H0651 | [矩阵 L1087](difftest-routers.md#L1087) | U-H |
| H0652 | [矩阵 L1088](difftest-routers.md#L1088) | U-H |
| H0653 | [矩阵 L1089](difftest-routers.md#L1089) | U-H |
| H0654 | [矩阵 L1090](difftest-routers.md#L1090) | U-H |
| H0655 | [矩阵 L1091](difftest-routers.md#L1091) | U-H |
| H0656 | [矩阵 L1092](difftest-routers.md#L1092) | U-H |
| H0657 | [矩阵 L1093](difftest-routers.md#L1093) | U-H |
| H0658 | [矩阵 L1099](difftest-routers.md#L1099) | U-H |
| H0659 | [矩阵 L1105](difftest-routers.md#L1105) | U-H |
| H0660 | [矩阵 L1106](difftest-routers.md#L1106) | U-H |
| H0661 | [矩阵 L1107](difftest-routers.md#L1107) | U-H |
| H0662 | [矩阵 L1108](difftest-routers.md#L1108) | U-H |
| H0663 | [矩阵 L1114](difftest-routers.md#L1114) | U-H |
| H0664 | [矩阵 L1115](difftest-routers.md#L1115) | U-H |
| H0665 | [矩阵 L1116](difftest-routers.md#L1116) | U-H |
| H0666 | [矩阵 L1117](difftest-routers.md#L1117) | U-H |
| H0667 | [矩阵 L1118](difftest-routers.md#L1118) | U-H |
| H0668 | [矩阵 L1119](difftest-routers.md#L1119) | U-H |
| H0669 | [矩阵 L1120](difftest-routers.md#L1120) | U-H |
| H0670 | [矩阵 L1121](difftest-routers.md#L1121) | U-H |
| H0671 | [矩阵 L1122](difftest-routers.md#L1122) | U-H |
| H0672 | [矩阵 L1123](difftest-routers.md#L1123) | U-H |
| H0673 | [矩阵 L1124](difftest-routers.md#L1124) | U-H |
| H0674 | [矩阵 L1125](difftest-routers.md#L1125) | U-H |
| H0675 | [矩阵 L1126](difftest-routers.md#L1126) | U-H |
| H0676 | [矩阵 L1127](difftest-routers.md#L1127) | U-H |
| H0677 | [矩阵 L1133](difftest-routers.md#L1133) | U-H |
| H0678 | [矩阵 L1134](difftest-routers.md#L1134) | U-H |
| H0679 | [矩阵 L1135](difftest-routers.md#L1135) | U-H |
| H0680 | [矩阵 L1136](difftest-routers.md#L1136) | U-H |
| H0681 | [矩阵 L1137](difftest-routers.md#L1137) | U-H |
| H0682 | [矩阵 L1138](difftest-routers.md#L1138) | U-H |
| H0683 | [矩阵 L1139](difftest-routers.md#L1139) | U-H |
| H0684 | [矩阵 L1140](difftest-routers.md#L1140) | U-H |
| H0685 | [矩阵 L1141](difftest-routers.md#L1141) | U-H |
| H0686 | [矩阵 L1142](difftest-routers.md#L1142) | U-H |
| H0687 | [矩阵 L1143](difftest-routers.md#L1143) | U-H |
| H0688 | [矩阵 L1144](difftest-routers.md#L1144) | U-H |
| H0689 | [矩阵 L1145](difftest-routers.md#L1145) | U-H |
| H0690 | [矩阵 L1146](difftest-routers.md#L1146) | U-H |
| H0691 | [矩阵 L1147](difftest-routers.md#L1147) | U-H |
| H0692 | [矩阵 L1148](difftest-routers.md#L1148) | U-H |
| H0693 | [矩阵 L1149](difftest-routers.md#L1149) | U-H |
| H0694 | [矩阵 L1150](difftest-routers.md#L1150) | U-H |
| H0695 | [矩阵 L1151](difftest-routers.md#L1151) | U-H |
| H0696 | [矩阵 L1152](difftest-routers.md#L1152) | U-H |
| H0697 | [矩阵 L1153](difftest-routers.md#L1153) | U-H |
| H0698 | [矩阵 L1154](difftest-routers.md#L1154) | U-H |
| H0699 | [矩阵 L1155](difftest-routers.md#L1155) | U-H |
| H0700 | [矩阵 L1156](difftest-routers.md#L1156) | U-H |
| H0701 | [矩阵 L1157](difftest-routers.md#L1157) | U-H |
| H0702 | [矩阵 L1158](difftest-routers.md#L1158) | U-H |
| H0703 | [矩阵 L1159](difftest-routers.md#L1159) | U-H |
| H0704 | [矩阵 L1160](difftest-routers.md#L1160) | U-H |
| H0705 | [矩阵 L1161](difftest-routers.md#L1161) | U-H |
| H0706 | [矩阵 L1162](difftest-routers.md#L1162) | U-H |
| H0707 | [矩阵 L1163](difftest-routers.md#L1163) | U-H |
| H0708 | [矩阵 L1164](difftest-routers.md#L1164) | U-H |
| H0709 | [矩阵 L1165](difftest-routers.md#L1165) | U-H |
| H0710 | [矩阵 L1166](difftest-routers.md#L1166) | U-H |
| H0711 | [矩阵 L1167](difftest-routers.md#L1167) | U-H |
| H0712 | [矩阵 L1168](difftest-routers.md#L1168) | U-H |
| H0713 | [矩阵 L1169](difftest-routers.md#L1169) | U-H |
| H0714 | [矩阵 L1170](difftest-routers.md#L1170) | U-H |
| H0715 | [矩阵 L1171](difftest-routers.md#L1171) | U-H |
| H0716 | [矩阵 L1177](difftest-routers.md#L1177) | U-H |
| H0717 | [矩阵 L1178](difftest-routers.md#L1178) | U-H |
| H0718 | [矩阵 L1179](difftest-routers.md#L1179) | U-H |
| H0719 | [矩阵 L1180](difftest-routers.md#L1180) | U-H |
| H0720 | [矩阵 L1181](difftest-routers.md#L1181) | U-H |
| H0721 | [矩阵 L1182](difftest-routers.md#L1182) | U-H |
| H0722 | [矩阵 L1183](difftest-routers.md#L1183) | U-H |
| H0723 | [矩阵 L1184](difftest-routers.md#L1184) | U-H |
| H0724 | [矩阵 L1185](difftest-routers.md#L1185) | U-H |
| H0725 | [矩阵 L1186](difftest-routers.md#L1186) | U-H |
| H0726 | [矩阵 L1187](difftest-routers.md#L1187) | U-H |
| H0727 | [矩阵 L1188](difftest-routers.md#L1188) | U-H |
| H0728 | [矩阵 L1189](difftest-routers.md#L1189) | U-H |
| H0729 | [矩阵 L1190](difftest-routers.md#L1190) | U-H |
| H0730 | [矩阵 L1191](difftest-routers.md#L1191) | U-H |
| H0731 | [矩阵 L1192](difftest-routers.md#L1192) | U-H |
| H0732 | [矩阵 L1193](difftest-routers.md#L1193) | U-H |
| H0733 | [矩阵 L1194](difftest-routers.md#L1194) | U-H |
| H0734 | [矩阵 L1195](difftest-routers.md#L1195) | U-H |
| H0735 | [矩阵 L1196](difftest-routers.md#L1196) | U-H |
| H0736 | [矩阵 L1197](difftest-routers.md#L1197) | U-H |
| H0737 | [矩阵 L1198](difftest-routers.md#L1198) | U-H |
| H0738 | [矩阵 L1199](difftest-routers.md#L1199) | U-H |
| H0739 | [矩阵 L1200](difftest-routers.md#L1200) | U-H |
| H0740 | [矩阵 L1201](difftest-routers.md#L1201) | U-H |
| H0741 | [矩阵 L1202](difftest-routers.md#L1202) | U-H |
| H0742 | [矩阵 L1203](difftest-routers.md#L1203) | U-H |
| H0743 | [矩阵 L1204](difftest-routers.md#L1204) | U-H |
| H0744 | [矩阵 L1205](difftest-routers.md#L1205) | U-H |
| H0745 | [矩阵 L1206](difftest-routers.md#L1206) | U-H |
| H0746 | [矩阵 L1207](difftest-routers.md#L1207) | U-H |
| H0747 | [矩阵 L1208](difftest-routers.md#L1208) | U-H |
| H0748 | [矩阵 L1209](difftest-routers.md#L1209) | U-H |
| H0749 | [矩阵 L1210](difftest-routers.md#L1210) | U-H |
| H0750 | [矩阵 L1211](difftest-routers.md#L1211) | U-H |
| H0751 | [矩阵 L1212](difftest-routers.md#L1212) | U-H |
| H0752 | [矩阵 L1213](difftest-routers.md#L1213) | U-H |
| H0753 | [矩阵 L1214](difftest-routers.md#L1214) | U-H |
| H0754 | [矩阵 L1215](difftest-routers.md#L1215) | U-H |
| H0755 | [矩阵 L1216](difftest-routers.md#L1216) | U-H |
| H0756 | [矩阵 L1217](difftest-routers.md#L1217) | U-H |
| H0757 | [矩阵 L1218](difftest-routers.md#L1218) | U-H |
| H0758 | [矩阵 L1219](difftest-routers.md#L1219) | U-H |
| H0759 | [矩阵 L1220](difftest-routers.md#L1220) | U-H |
| H0760 | [矩阵 L1221](difftest-routers.md#L1221) | U-H |
| H0761 | [矩阵 L1222](difftest-routers.md#L1222) | U-H |
| H0762 | [矩阵 L1223](difftest-routers.md#L1223) | U-H |
| H0763 | [矩阵 L1224](difftest-routers.md#L1224) | U-H |
| H0764 | [矩阵 L1225](difftest-routers.md#L1225) | U-H |
| H0765 | [矩阵 L1226](difftest-routers.md#L1226) | U-H |
| H0766 | [矩阵 L1227](difftest-routers.md#L1227) | U-H |
| H0767 | [矩阵 L1228](difftest-routers.md#L1228) | U-H |
| H0768 | [矩阵 L1229](difftest-routers.md#L1229) | U-H |
| H0769 | [矩阵 L1230](difftest-routers.md#L1230) | U-H |
| H0770 | [矩阵 L1231](difftest-routers.md#L1231) | U-H |
| H0771 | [矩阵 L1232](difftest-routers.md#L1232) | U-H |
| H0772 | [矩阵 L1233](difftest-routers.md#L1233) | U-H |
| H0773 | [矩阵 L1234](difftest-routers.md#L1234) | U-H |
| H0774 | [矩阵 L1235](difftest-routers.md#L1235) | U-H |
| H0775 | [矩阵 L1236](difftest-routers.md#L1236) | U-H |
| H0776 | [矩阵 L1237](difftest-routers.md#L1237) | U-H |
| H0777 | [矩阵 L1238](difftest-routers.md#L1238) | U-H |
| H0778 | [矩阵 L1239](difftest-routers.md#L1239) | U-H |
| H0779 | [矩阵 L1240](difftest-routers.md#L1240) | U-H |
| H0780 | [矩阵 L1241](difftest-routers.md#L1241) | U-H |
| H0781 | [矩阵 L1242](difftest-routers.md#L1242) | U-H |
| H0782 | [矩阵 L1243](difftest-routers.md#L1243) | U-H |
| H0783 | [矩阵 L1244](difftest-routers.md#L1244) | U-H |
| H0784 | [矩阵 L1245](difftest-routers.md#L1245) | U-H |
| H0785 | [矩阵 L1246](difftest-routers.md#L1246) | U-H |
| H0786 | [矩阵 L1247](difftest-routers.md#L1247) | U-H |
| H0787 | [矩阵 L1248](difftest-routers.md#L1248) | U-H |
| H0788 | [矩阵 L1249](difftest-routers.md#L1249) | U-H |
| H0789 | [矩阵 L1250](difftest-routers.md#L1250) | U-H |
| H0790 | [矩阵 L1251](difftest-routers.md#L1251) | U-H |
| H0791 | [矩阵 L1252](difftest-routers.md#L1252) | U-H |
| H0792 | [矩阵 L1253](difftest-routers.md#L1253) | U-H |
| H0793 | [矩阵 L1254](difftest-routers.md#L1254) | U-H |
| H0794 | [矩阵 L1260](difftest-routers.md#L1260) | U-H |
| H0795 | [矩阵 L1261](difftest-routers.md#L1261) | U-H |
| H0796 | [矩阵 L1262](difftest-routers.md#L1262) | U-H |
| H0797 | [矩阵 L1263](difftest-routers.md#L1263) | U-H |
| H0798 | [矩阵 L1264](difftest-routers.md#L1264) | U-H |
| H0799 | [矩阵 L1265](difftest-routers.md#L1265) | U-H |
| H0800 | [矩阵 L1266](difftest-routers.md#L1266) | U-H |
| H0801 | [矩阵 L1267](difftest-routers.md#L1267) | U-H |
| H0802 | [矩阵 L1268](difftest-routers.md#L1268) | U-H |
| H0803 | [矩阵 L1269](difftest-routers.md#L1269) | U-H |
| H0804 | [矩阵 L1270](difftest-routers.md#L1270) | U-H |
| H0805 | [矩阵 L1271](difftest-routers.md#L1271) | U-H |
| H0806 | [矩阵 L1277](difftest-routers.md#L1277) | U-H |
| H0807 | [矩阵 L1283](difftest-routers.md#L1283) | U-H |
| H0808 | [矩阵 L1284](difftest-routers.md#L1284) | U-H |
| H0809 | [矩阵 L1285](difftest-routers.md#L1285) | U-H |
| H0810 | [矩阵 L1286](difftest-routers.md#L1286) | U-H |
| H0811 | [矩阵 L1287](difftest-routers.md#L1287) | U-H |
| H0812 | [矩阵 L1288](difftest-routers.md#L1288) | U-H |
| H0813 | [矩阵 L1289](difftest-routers.md#L1289) | U-H |
| H0814 | [矩阵 L1290](difftest-routers.md#L1290) | U-H |
| H0815 | [矩阵 L1291](difftest-routers.md#L1291) | U-H |
| H0816 | [矩阵 L1292](difftest-routers.md#L1292) | U-H |
| H0817 | [矩阵 L1293](difftest-routers.md#L1293) | U-H |
| H0818 | [矩阵 L1294](difftest-routers.md#L1294) | U-H |
| H0819 | [矩阵 L1295](difftest-routers.md#L1295) | U-H |
| H0820 | [矩阵 L1296](difftest-routers.md#L1296) | U-H |
| H0821 | [矩阵 L1297](difftest-routers.md#L1297) | U-H |
| H0822 | [矩阵 L1298](difftest-routers.md#L1298) | U-H |
| H0823 | [矩阵 L1299](difftest-routers.md#L1299) | U-H |
| H0824 | [矩阵 L1300](difftest-routers.md#L1300) | U-H |
| H0825 | [矩阵 L1301](difftest-routers.md#L1301) | U-H |
| H0836 | [矩阵 L1317](difftest-routers.md#L1317) | U-H |
| H0837 | [矩阵 L1318](difftest-routers.md#L1318) | U-H |
| H0838 | [矩阵 L1319](difftest-routers.md#L1319) | U-H |
| H0839 | [矩阵 L1320](difftest-routers.md#L1320) | U-H |
| H0840 | [矩阵 L1321](difftest-routers.md#L1321) | U-H |
| H0844 | [矩阵 L1325](difftest-routers.md#L1325) | U-H |
| H0845 | [矩阵 L1326](difftest-routers.md#L1326) | U-H |
| H0846 | [矩阵 L1327](difftest-routers.md#L1327) | U-H |
| H0847 | [矩阵 L1328](difftest-routers.md#L1328) | U-H |
| H0848 | [矩阵 L1329](difftest-routers.md#L1329) | U-H |
| H0864 | [矩阵 L1345](difftest-routers.md#L1345) | U-H |
| H0865 | [矩阵 L1346](difftest-routers.md#L1346) | U-H |
| H0866 | [矩阵 L1347](difftest-routers.md#L1347) | U-H |
| H0867 | [矩阵 L1348](difftest-routers.md#L1348) | U-H |
| H0868 | [矩阵 L1349](difftest-routers.md#L1349) | U-H |
| H0869 | [矩阵 L1350](difftest-routers.md#L1350) | U-H |
| H0870 | [矩阵 L1351](difftest-routers.md#L1351) | U-H |
| H0871 | [矩阵 L1352](difftest-routers.md#L1352) | U-H |
| H0872 | [矩阵 L1353](difftest-routers.md#L1353) | U-H |
| H0873 | [矩阵 L1354](difftest-routers.md#L1354) | U-H |
| H0874 | [矩阵 L1355](difftest-routers.md#L1355) | U-H |
| H0875 | [矩阵 L1356](difftest-routers.md#L1356) | U-H |
| H0876 | [矩阵 L1357](difftest-routers.md#L1357) | U-H |
| H0877 | [矩阵 L1358](difftest-routers.md#L1358) | U-H |
| H0878 | [矩阵 L1359](difftest-routers.md#L1359) | U-H |
| H0879 | [矩阵 L1360](difftest-routers.md#L1360) | U-H |
| H0880 | [矩阵 L1361](difftest-routers.md#L1361) | U-H |
| H0881 | [矩阵 L1362](difftest-routers.md#L1362) | U-H |
| H0882 | [矩阵 L1363](difftest-routers.md#L1363) | U-H |
| H0883 | [矩阵 L1364](difftest-routers.md#L1364) | U-H |
| H0884 | [矩阵 L1365](difftest-routers.md#L1365) | U-H |
| H0885 | [矩阵 L1366](difftest-routers.md#L1366) | U-H |
| H0886 | [矩阵 L1367](difftest-routers.md#L1367) | U-H |
| H0887 | [矩阵 L1368](difftest-routers.md#L1368) | U-H |
| H0888 | [矩阵 L1369](difftest-routers.md#L1369) | U-H |
| H0889 | [矩阵 L1370](difftest-routers.md#L1370) | U-H |
| H0890 | [矩阵 L1371](difftest-routers.md#L1371) | U-H |
| H0891 | [矩阵 L1372](difftest-routers.md#L1372) | U-H |
| H0892 | [矩阵 L1373](difftest-routers.md#L1373) | U-H |
| H0893 | [矩阵 L1374](difftest-routers.md#L1374) | U-H |
| H0894 | [矩阵 L1375](difftest-routers.md#L1375) | U-H |
| H0895 | [矩阵 L1376](difftest-routers.md#L1376) | U-H |
| H0896 | [矩阵 L1377](difftest-routers.md#L1377) | U-H |
| H0897 | [矩阵 L1378](difftest-routers.md#L1378) | U-H |
| H0898 | [矩阵 L1379](difftest-routers.md#L1379) | U-H |
| H0899 | [矩阵 L1380](difftest-routers.md#L1380) | U-H |
| H0900 | [矩阵 L1381](difftest-routers.md#L1381) | U-H |
| H0901 | [矩阵 L1382](difftest-routers.md#L1382) | U-H |
| H0902 | [矩阵 L1383](difftest-routers.md#L1383) | U-H |
| H0903 | [矩阵 L1384](difftest-routers.md#L1384) | U-H |
| H0911 | [矩阵 L1397](difftest-routers.md#L1397) | U-H |
| H0912 | [矩阵 L1398](difftest-routers.md#L1398) | U-H |
| H0913 | [矩阵 L1399](difftest-routers.md#L1399) | U-H |
| H0914 | [矩阵 L1400](difftest-routers.md#L1400) | U-H |
| H0915 | [矩阵 L1401](difftest-routers.md#L1401) | U-H |
| H0916 | [矩阵 L1402](difftest-routers.md#L1402) | U-H |
| H0917 | [矩阵 L1403](difftest-routers.md#L1403) | U-H |
| H0918 | [矩阵 L1404](difftest-routers.md#L1404) | U-H |
| H0919 | [矩阵 L1405](difftest-routers.md#L1405) | U-H |
| H0920 | [矩阵 L1406](difftest-routers.md#L1406) | U-H |
| H0921 | [矩阵 L1407](difftest-routers.md#L1407) | U-H |
| H0922 | [矩阵 L1408](difftest-routers.md#L1408) | U-H |
| H0923 | [矩阵 L1409](difftest-routers.md#L1409) | U-H |
| H0924 | [矩阵 L1410](difftest-routers.md#L1410) | U-H |
| H0925 | [矩阵 L1411](difftest-routers.md#L1411) | U-H |
| H0926 | [矩阵 L1412](difftest-routers.md#L1412) | U-H |
| H0927 | [矩阵 L1413](difftest-routers.md#L1413) | U-H |
| H0928 | [矩阵 L1414](difftest-routers.md#L1414) | U-H |
| H0929 | [矩阵 L1415](difftest-routers.md#L1415) | U-H |
| H0930 | [矩阵 L1416](difftest-routers.md#L1416) | U-H |
| H0931 | [矩阵 L1417](difftest-routers.md#L1417) | U-H |
| H0932 | [矩阵 L1418](difftest-routers.md#L1418) | U-H |
| H0933 | [矩阵 L1419](difftest-routers.md#L1419) | U-H |
| H0934 | [矩阵 L1420](difftest-routers.md#L1420) | U-H |
| H0935 | [矩阵 L1421](difftest-routers.md#L1421) | U-H |
| H0936 | [矩阵 L1422](difftest-routers.md#L1422) | U-H |
| H0937 | [矩阵 L1423](difftest-routers.md#L1423) | U-H |
| H0938 | [矩阵 L1424](difftest-routers.md#L1424) | U-H |
| H0939 | [矩阵 L1425](difftest-routers.md#L1425) | U-H |
| H0940 | [矩阵 L1426](difftest-routers.md#L1426) | U-H |
| H0941 | [矩阵 L1427](difftest-routers.md#L1427) | U-H |
| H0942 | [矩阵 L1428](difftest-routers.md#L1428) | U-H |
| H0943 | [矩阵 L1429](difftest-routers.md#L1429) | U-H |
| H0944 | [矩阵 L1430](difftest-routers.md#L1430) | U-H |
| H0945 | [矩阵 L1431](difftest-routers.md#L1431) | U-H |
| H0946 | [矩阵 L1432](difftest-routers.md#L1432) | U-H |
| H0947 | [矩阵 L1433](difftest-routers.md#L1433) | U-H |
| H0948 | [矩阵 L1434](difftest-routers.md#L1434) | U-H |
| H0949 | [矩阵 L1435](difftest-routers.md#L1435) | U-H |
| H0950 | [矩阵 L1436](difftest-routers.md#L1436) | U-H |
| H0951 | [矩阵 L1437](difftest-routers.md#L1437) | U-H |
| H0952 | [矩阵 L1438](difftest-routers.md#L1438) | U-H |
| H0953 | [矩阵 L1439](difftest-routers.md#L1439) | U-H |
| H0954 | [矩阵 L1440](difftest-routers.md#L1440) | U-H |
| H0955 | [矩阵 L1446](difftest-routers.md#L1446) | U-H |
| H0956 | [矩阵 L1447](difftest-routers.md#L1447) | U-H |
| H0957 | [矩阵 L1448](difftest-routers.md#L1448) | U-H |
| H0958 | [矩阵 L1449](difftest-routers.md#L1449) | U-H |
| H0959 | [矩阵 L1450](difftest-routers.md#L1450) | U-H |
| H0960 | [矩阵 L1451](difftest-routers.md#L1451) | U-H |
| H0961 | [矩阵 L1452](difftest-routers.md#L1452) | U-H |
| H0962 | [矩阵 L1453](difftest-routers.md#L1453) | U-H |
| H0963 | [矩阵 L1454](difftest-routers.md#L1454) | U-H |
| H0964 | [矩阵 L1455](difftest-routers.md#L1455) | U-H |
| H0965 | [矩阵 L1456](difftest-routers.md#L1456) | U-H |
| H0966 | [矩阵 L1457](difftest-routers.md#L1457) | U-H |
| H0967 | [矩阵 L1458](difftest-routers.md#L1458) | U-H |
| H0968 | [矩阵 L1459](difftest-routers.md#L1459) | U-H |
| H0969 | [矩阵 L1460](difftest-routers.md#L1460) | U-H |
| H0970 | [矩阵 L1461](difftest-routers.md#L1461) | U-H |
| H0971 | [矩阵 L1462](difftest-routers.md#L1462) | U-H |
| H0972 | [矩阵 L1463](difftest-routers.md#L1463) | U-H |
| H0973 | [矩阵 L1464](difftest-routers.md#L1464) | U-H |
| H0974 | [矩阵 L1465](difftest-routers.md#L1465) | U-H |
| H0975 | [矩阵 L1466](difftest-routers.md#L1466) | U-H |
| H0976 | [矩阵 L1467](difftest-routers.md#L1467) | U-H |
| H0977 | [矩阵 L1468](difftest-routers.md#L1468) | U-H |
| H0978 | [矩阵 L1469](difftest-routers.md#L1469) | U-H |
| H0979 | [矩阵 L1470](difftest-routers.md#L1470) | U-H |
| H0980 | [矩阵 L1471](difftest-routers.md#L1471) | U-H |
| H0981 | [矩阵 L1472](difftest-routers.md#L1472) | U-H |
| H0982 | [矩阵 L1473](difftest-routers.md#L1473) | U-H |
| H0983 | [矩阵 L1474](difftest-routers.md#L1474) | U-H |
| H0984 | [矩阵 L1475](difftest-routers.md#L1475) | U-H |
| H0985 | [矩阵 L1476](difftest-routers.md#L1476) | U-H |
| H0986 | [矩阵 L1477](difftest-routers.md#L1477) | U-H |
| H0987 | [矩阵 L1478](difftest-routers.md#L1478) | U-H |
| H0988 | [矩阵 L1479](difftest-routers.md#L1479) | U-H |
| H0989 | [矩阵 L1480](difftest-routers.md#L1480) | U-H |
| H0990 | [矩阵 L1481](difftest-routers.md#L1481) | U-H |
| H0991 | [矩阵 L1482](difftest-routers.md#L1482) | U-H |
| H0992 | [矩阵 L1483](difftest-routers.md#L1483) | U-H |
| H0993 | [矩阵 L1484](difftest-routers.md#L1484) | U-H |
| H0994 | [矩阵 L1485](difftest-routers.md#L1485) | U-H |
| H0995 | [矩阵 L1486](difftest-routers.md#L1486) | U-H |
| H0996 | [矩阵 L1487](difftest-routers.md#L1487) | U-H |
| H0997 | [矩阵 L1488](difftest-routers.md#L1488) | U-H |
| H0998 | [矩阵 L1489](difftest-routers.md#L1489) | U-H |
| H0999 | [矩阵 L1495](difftest-routers.md#L1495) | U-H |
| H1000 | [矩阵 L1496](difftest-routers.md#L1496) | U-H |
| H1001 | [矩阵 L1497](difftest-routers.md#L1497) | U-H |
| H1002 | [矩阵 L1498](difftest-routers.md#L1498) | U-H |
| H1003 | [矩阵 L1499](difftest-routers.md#L1499) | U-H |
| H1004 | [矩阵 L1500](difftest-routers.md#L1500) | U-H |
| H1005 | [矩阵 L1501](difftest-routers.md#L1501) | U-H |
| H1006 | [矩阵 L1502](difftest-routers.md#L1502) | U-H |
| H1007 | [矩阵 L1503](difftest-routers.md#L1503) | U-H |
| H1008 | [矩阵 L1504](difftest-routers.md#L1504) | U-H |
| H1009 | [矩阵 L1505](difftest-routers.md#L1505) | U-H |
| H1010 | [矩阵 L1506](difftest-routers.md#L1506) | U-H |
| H1011 | [矩阵 L1507](difftest-routers.md#L1507) | U-H |
| H1012 | [矩阵 L1508](difftest-routers.md#L1508) | U-H |
| H1013 | [矩阵 L1509](difftest-routers.md#L1509) | U-H |
| H1014 | [矩阵 L1510](difftest-routers.md#L1510) | U-H |
| H1015 | [矩阵 L1511](difftest-routers.md#L1511) | U-H |
| H1016 | [矩阵 L1512](difftest-routers.md#L1512) | U-H |
| H1017 | [矩阵 L1513](difftest-routers.md#L1513) | U-H |
| H1018 | [矩阵 L1514](difftest-routers.md#L1514) | U-H |
| H1019 | [矩阵 L1515](difftest-routers.md#L1515) | U-H |
| H1020 | [矩阵 L1516](difftest-routers.md#L1516) | U-H |
| H1021 | [矩阵 L1517](difftest-routers.md#L1517) | U-H |
| H1022 | [矩阵 L1518](difftest-routers.md#L1518) | U-H |
| H1023 | [矩阵 L1519](difftest-routers.md#L1519) | U-H |
| H1024 | [矩阵 L1520](difftest-routers.md#L1520) | U-H |
| H1025 | [矩阵 L1521](difftest-routers.md#L1521) | U-H |
| H1026 | [矩阵 L1522](difftest-routers.md#L1522) | U-H |
| H1027 | [矩阵 L1523](difftest-routers.md#L1523) | U-H |
| H1028 | [矩阵 L1529](difftest-routers.md#L1529) | U-H |
| H1029 | [矩阵 L1530](difftest-routers.md#L1530) | U-H |
| H1030 | [矩阵 L1531](difftest-routers.md#L1531) | U-H |
| H1031 | [矩阵 L1532](difftest-routers.md#L1532) | U-H |
| H1032 | [矩阵 L1533](difftest-routers.md#L1533) | U-H |
| H1033 | [矩阵 L1534](difftest-routers.md#L1534) | U-H |
| H1034 | [矩阵 L1535](difftest-routers.md#L1535) | U-H |
| H1035 | [矩阵 L1536](difftest-routers.md#L1536) | U-H |
| H1036 | [矩阵 L1537](difftest-routers.md#L1537) | U-H |
| H1037 | [矩阵 L1538](difftest-routers.md#L1538) | U-H |
| H1038 | [矩阵 L1539](difftest-routers.md#L1539) | U-H |
| H1039 | [矩阵 L1540](difftest-routers.md#L1540) | U-H |
| H1040 | [矩阵 L1541](difftest-routers.md#L1541) | U-H |
| H1041 | [矩阵 L1542](difftest-routers.md#L1542) | U-H |
| H1042 | [矩阵 L1543](difftest-routers.md#L1543) | U-H |
| H1043 | [矩阵 L1544](difftest-routers.md#L1544) | U-H |
| H1044 | [矩阵 L1545](difftest-routers.md#L1545) | U-H |
| H1045 | [矩阵 L1546](difftest-routers.md#L1546) | U-H |
| H1046 | [矩阵 L1547](difftest-routers.md#L1547) | U-H |
| H1047 | [矩阵 L1548](difftest-routers.md#L1548) | U-H |
| H1048 | [矩阵 L1549](difftest-routers.md#L1549) | U-H |
| H1049 | [矩阵 L1550](difftest-routers.md#L1550) | U-H |
| H1050 | [矩阵 L1551](difftest-routers.md#L1551) | U-H |
| H1051 | [矩阵 L1552](difftest-routers.md#L1552) | U-H |
| H1052 | [矩阵 L1553](difftest-routers.md#L1553) | U-H |
| H1053 | [矩阵 L1554](difftest-routers.md#L1554) | U-H |
| H1054 | [矩阵 L1555](difftest-routers.md#L1555) | U-H |
| H1055 | [矩阵 L1556](difftest-routers.md#L1556) | U-H |
| H1056 | [矩阵 L1557](difftest-routers.md#L1557) | U-H |
| H1057 | [矩阵 L1558](difftest-routers.md#L1558) | U-H |
| H1058 | [矩阵 L1559](difftest-routers.md#L1559) | U-H |
| H1059 | [矩阵 L1560](difftest-routers.md#L1560) | U-H |
| H1060 | [矩阵 L1561](difftest-routers.md#L1561) | U-H |
| H1061 | [矩阵 L1562](difftest-routers.md#L1562) | U-H |
| H1062 | [矩阵 L1563](difftest-routers.md#L1563) | U-H |
| H1063 | [矩阵 L1564](difftest-routers.md#L1564) | U-H |
| H1064 | [矩阵 L1565](difftest-routers.md#L1565) | U-H |
| H1065 | [矩阵 L1566](difftest-routers.md#L1566) | U-H |
| H1066 | [矩阵 L1567](difftest-routers.md#L1567) | U-H |
| H1067 | [矩阵 L1568](difftest-routers.md#L1568) | U-H |
| H1068 | [矩阵 L1569](difftest-routers.md#L1569) | U-H |
| H1069 | [矩阵 L1570](difftest-routers.md#L1570) | U-H |
| H1070 | [矩阵 L1571](difftest-routers.md#L1571) | U-H |
| H1071 | [矩阵 L1572](difftest-routers.md#L1572) | U-H |
| H1072 | [矩阵 L1573](difftest-routers.md#L1573) | U-H |
| H1073 | [矩阵 L1574](difftest-routers.md#L1574) | U-H |
| H1074 | [矩阵 L1575](difftest-routers.md#L1575) | U-H |
| H1075 | [矩阵 L1576](difftest-routers.md#L1576) | U-H |
| H1076 | [矩阵 L1577](difftest-routers.md#L1577) | U-H |
| H1077 | [矩阵 L1578](difftest-routers.md#L1578) | U-H |
| H1078 | [矩阵 L1579](difftest-routers.md#L1579) | U-H |
| H1079 | [矩阵 L1580](difftest-routers.md#L1580) | U-H |
| H1080 | [矩阵 L1581](difftest-routers.md#L1581) | U-H |
| H1081 | [矩阵 L1582](difftest-routers.md#L1582) | U-H |
| H1082 | [矩阵 L1583](difftest-routers.md#L1583) | U-H |
| H1083 | [矩阵 L1584](difftest-routers.md#L1584) | U-H |
| H1084 | [矩阵 L1585](difftest-routers.md#L1585) | U-H |
| H1085 | [矩阵 L1586](difftest-routers.md#L1586) | U-H |
| H1086 | [矩阵 L1587](difftest-routers.md#L1587) | U-H |
| H1087 | [矩阵 L1588](difftest-routers.md#L1588) | U-H |
| H1088 | [矩阵 L1594](difftest-routers.md#L1594) | U-H |
| H1089 | [矩阵 L1595](difftest-routers.md#L1595) | U-H |
| H1090 | [矩阵 L1596](difftest-routers.md#L1596) | U-H |
| H1091 | [矩阵 L1597](difftest-routers.md#L1597) | U-H |
| H1092 | [矩阵 L1598](difftest-routers.md#L1598) | U-H |
| H1093 | [矩阵 L1599](difftest-routers.md#L1599) | U-H |
| H1094 | [矩阵 L1600](difftest-routers.md#L1600) | U-H |
| H1095 | [矩阵 L1601](difftest-routers.md#L1601) | U-H |
| H1096 | [矩阵 L1602](difftest-routers.md#L1602) | U-H |
| H1097 | [矩阵 L1603](difftest-routers.md#L1603) | U-H |
| H1098 | [矩阵 L1604](difftest-routers.md#L1604) | U-H |
| H1099 | [矩阵 L1605](difftest-routers.md#L1605) | U-H |
| H1100 | [矩阵 L1606](difftest-routers.md#L1606) | U-H |
| H1101 | [矩阵 L1607](difftest-routers.md#L1607) | U-H |
| H1102 | [矩阵 L1608](difftest-routers.md#L1608) | U-H |
| H1103 | [矩阵 L1609](difftest-routers.md#L1609) | U-H |
| H1104 | [矩阵 L1610](difftest-routers.md#L1610) | U-H |
| H1105 | [矩阵 L1611](difftest-routers.md#L1611) | U-H |
| H1106 | [矩阵 L1612](difftest-routers.md#L1612) | U-H |
| H1107 | [矩阵 L1613](difftest-routers.md#L1613) | U-H |
| H1108 | [矩阵 L1614](difftest-routers.md#L1614) | U-H |
| H1109 | [矩阵 L1615](difftest-routers.md#L1615) | U-H |
| H1110 | [矩阵 L1616](difftest-routers.md#L1616) | U-H |
| H1111 | [矩阵 L1617](difftest-routers.md#L1617) | U-H |
| H1112 | [矩阵 L1618](difftest-routers.md#L1618) | U-H |
| H1113 | [矩阵 L1619](difftest-routers.md#L1619) | U-H |
| H1114 | [矩阵 L1620](difftest-routers.md#L1620) | U-H |
| H1115 | [矩阵 L1621](difftest-routers.md#L1621) | U-H |
| H1116 | [矩阵 L1622](difftest-routers.md#L1622) | U-H |
| H1117 | [矩阵 L1623](difftest-routers.md#L1623) | U-H |
| H1118 | [矩阵 L1624](difftest-routers.md#L1624) | U-H |
| H1119 | [矩阵 L1625](difftest-routers.md#L1625) | U-H |
| H1120 | [矩阵 L1626](difftest-routers.md#L1626) | U-H |
| H1121 | [矩阵 L1627](difftest-routers.md#L1627) | U-H |
| H1122 | [矩阵 L1628](difftest-routers.md#L1628) | U-H |
| H1123 | [矩阵 L1629](difftest-routers.md#L1629) | U-H |
| H1124 | [矩阵 L1630](difftest-routers.md#L1630) | U-H |
| H1125 | [矩阵 L1631](difftest-routers.md#L1631) | U-H |
| H1126 | [矩阵 L1632](difftest-routers.md#L1632) | U-H |
| H1127 | [矩阵 L1633](difftest-routers.md#L1633) | U-H |
| H1128 | [矩阵 L1634](difftest-routers.md#L1634) | U-H |
| H1129 | [矩阵 L1635](difftest-routers.md#L1635) | U-H |
| H1130 | [矩阵 L1641](difftest-routers.md#L1641) | U-H |
| H1131 | [矩阵 L1642](difftest-routers.md#L1642) | U-H |
| H1132 | [矩阵 L1643](difftest-routers.md#L1643) | U-H |
| H1133 | [矩阵 L1644](difftest-routers.md#L1644) | U-H |
| H1134 | [矩阵 L1645](difftest-routers.md#L1645) | U-H |
| H1135 | [矩阵 L1646](difftest-routers.md#L1646) | U-H |
| H1136 | [矩阵 L1647](difftest-routers.md#L1647) | U-H |
| H1137 | [矩阵 L1648](difftest-routers.md#L1648) | U-H |
| H1138 | [矩阵 L1649](difftest-routers.md#L1649) | U-H |
| H1139 | [矩阵 L1650](difftest-routers.md#L1650) | U-H |
| H1140 | [矩阵 L1651](difftest-routers.md#L1651) | U-H |
| H1141 | [矩阵 L1652](difftest-routers.md#L1652) | U-H |
| H1142 | [矩阵 L1653](difftest-routers.md#L1653) | U-H |
| H1143 | [矩阵 L1654](difftest-routers.md#L1654) | U-H |
| H1144 | [矩阵 L1655](difftest-routers.md#L1655) | U-H |
| H1145 | [矩阵 L1656](difftest-routers.md#L1656) | U-H |
| H1146 | [矩阵 L1657](difftest-routers.md#L1657) | U-H |
| H1147 | [矩阵 L1658](difftest-routers.md#L1658) | U-H |
| H1148 | [矩阵 L1659](difftest-routers.md#L1659) | U-H |
| H1149 | [矩阵 L1660](difftest-routers.md#L1660) | U-H |
| H1150 | [矩阵 L1661](difftest-routers.md#L1661) | U-H |
| H1151 | [矩阵 L1662](difftest-routers.md#L1662) | U-H |
| H1152 | [矩阵 L1663](difftest-routers.md#L1663) | U-H |
| H1153 | [矩阵 L1664](difftest-routers.md#L1664) | U-H |
| H1154 | [矩阵 L1665](difftest-routers.md#L1665) | U-H |
| H1155 | [矩阵 L1666](difftest-routers.md#L1666) | U-H |
| H1156 | [矩阵 L1667](difftest-routers.md#L1667) | U-H |
| H1157 | [矩阵 L1668](difftest-routers.md#L1668) | U-H |
| H1158 | [矩阵 L1669](difftest-routers.md#L1669) | U-H |
| H1159 | [矩阵 L1670](difftest-routers.md#L1670) | U-H |
| H1160 | [矩阵 L1671](difftest-routers.md#L1671) | U-H |
| H1161 | [矩阵 L1672](difftest-routers.md#L1672) | U-H |
| H1162 | [矩阵 L1673](difftest-routers.md#L1673) | U-H |
| H1163 | [矩阵 L1674](difftest-routers.md#L1674) | U-H |
| H1164 | [矩阵 L1675](difftest-routers.md#L1675) | U-H |
| H1165 | [矩阵 L1676](difftest-routers.md#L1676) | U-H |
| H1166 | [矩阵 L1677](difftest-routers.md#L1677) | U-H |
| H1167 | [矩阵 L1678](difftest-routers.md#L1678) | U-H |
| H1168 | [矩阵 L1679](difftest-routers.md#L1679) | U-H |
| H1169 | [矩阵 L1680](difftest-routers.md#L1680) | U-H |
| H1170 | [矩阵 L1681](difftest-routers.md#L1681) | U-H |
| H1171 | [矩阵 L1682](difftest-routers.md#L1682) | U-H |
| H1172 | [矩阵 L1683](difftest-routers.md#L1683) | U-H |
| H1173 | [矩阵 L1689](difftest-routers.md#L1689) | U-H |
| H1174 | [矩阵 L1690](difftest-routers.md#L1690) | U-H |
| H1175 | [矩阵 L1691](difftest-routers.md#L1691) | U-H |
| H1176 | [矩阵 L1692](difftest-routers.md#L1692) | U-H |
| H1177 | [矩阵 L1693](difftest-routers.md#L1693) | U-H |
| H1178 | [矩阵 L1694](difftest-routers.md#L1694) | U-H |
| H1179 | [矩阵 L1695](difftest-routers.md#L1695) | U-H |
| H1180 | [矩阵 L1696](difftest-routers.md#L1696) | U-H |
| H1181 | [矩阵 L1697](difftest-routers.md#L1697) | U-H |
| H1182 | [矩阵 L1698](difftest-routers.md#L1698) | U-H |
| H1183 | [矩阵 L1699](difftest-routers.md#L1699) | U-H |
| H1184 | [矩阵 L1700](difftest-routers.md#L1700) | U-H |
| H1185 | [矩阵 L1701](difftest-routers.md#L1701) | U-H |
| H1186 | [矩阵 L1702](difftest-routers.md#L1702) | U-H |
| H1187 | [矩阵 L1703](difftest-routers.md#L1703) | U-H |
| H1188 | [矩阵 L1704](difftest-routers.md#L1704) | U-H |
| H1189 | [矩阵 L1705](difftest-routers.md#L1705) | U-H |
| H1190 | [矩阵 L1706](difftest-routers.md#L1706) | U-H |
| H1191 | [矩阵 L1707](difftest-routers.md#L1707) | U-H |
| H1192 | [矩阵 L1708](difftest-routers.md#L1708) | U-H |
| H1193 | [矩阵 L1709](difftest-routers.md#L1709) | U-H |
| H1194 | [矩阵 L1710](difftest-routers.md#L1710) | U-H |
| H1195 | [矩阵 L1711](difftest-routers.md#L1711) | U-H |
| H1196 | [矩阵 L1712](difftest-routers.md#L1712) | U-H |
| H1197 | [矩阵 L1713](difftest-routers.md#L1713) | U-H |
| H1198 | [矩阵 L1714](difftest-routers.md#L1714) | U-H |
| H1199 | [矩阵 L1715](difftest-routers.md#L1715) | U-H |
| H1200 | [矩阵 L1716](difftest-routers.md#L1716) | U-H |
| H1201 | [矩阵 L1717](difftest-routers.md#L1717) | U-H |
| H1202 | [矩阵 L1718](difftest-routers.md#L1718) | U-H |
| H1203 | [矩阵 L1719](difftest-routers.md#L1719) | U-H |
| H1204 | [矩阵 L1720](difftest-routers.md#L1720) | U-H |
| H1205 | [矩阵 L1721](difftest-routers.md#L1721) | U-H |
| H1206 | [矩阵 L1727](difftest-routers.md#L1727) | U-H |
| H1207 | [矩阵 L1728](difftest-routers.md#L1728) | U-H |
| H1208 | [矩阵 L1729](difftest-routers.md#L1729) | U-H |
| H1209 | [矩阵 L1730](difftest-routers.md#L1730) | U-H |
| H1210 | [矩阵 L1736](difftest-routers.md#L1736) | U-H |
| H1211 | [矩阵 L1737](difftest-routers.md#L1737) | U-H |
| H1212 | [矩阵 L1738](difftest-routers.md#L1738) | U-H |
| H1213 | [矩阵 L1739](difftest-routers.md#L1739) | U-H |
| H1214 | [矩阵 L1740](difftest-routers.md#L1740) | U-H |
| H1215 | [矩阵 L1741](difftest-routers.md#L1741) | U-H |
| H1216 | [矩阵 L1742](difftest-routers.md#L1742) | U-H |
| H1217 | [矩阵 L1743](difftest-routers.md#L1743) | U-H |
| H1218 | [矩阵 L1744](difftest-routers.md#L1744) | U-H |
| H1219 | [矩阵 L1745](difftest-routers.md#L1745) | U-H |
| H1220 | [矩阵 L1746](difftest-routers.md#L1746) | U-H |
| H1221 | [矩阵 L1747](difftest-routers.md#L1747) | U-H |
| H1222 | [矩阵 L1748](difftest-routers.md#L1748) | U-H |
| H1223 | [矩阵 L1749](difftest-routers.md#L1749) | U-H |
| H1224 | [矩阵 L1750](difftest-routers.md#L1750) | U-H |
| H1225 | [矩阵 L1751](difftest-routers.md#L1751) | U-H |
| H1226 | [矩阵 L1752](difftest-routers.md#L1752) | U-H |
| H1227 | [矩阵 L1753](difftest-routers.md#L1753) | U-H |
| H1228 | [矩阵 L1754](difftest-routers.md#L1754) | U-H |
| H1229 | [矩阵 L1755](difftest-routers.md#L1755) | U-H |
| H1230 | [矩阵 L1756](difftest-routers.md#L1756) | U-H |
| H1231 | [矩阵 L1757](difftest-routers.md#L1757) | U-H |
| H1232 | [矩阵 L1758](difftest-routers.md#L1758) | U-H |
| H1233 | [矩阵 L1759](difftest-routers.md#L1759) | U-H |
| H1264 | [矩阵 L1800](difftest-routers.md#L1800) | U-H |
| H1265 | [矩阵 L1801](difftest-routers.md#L1801) | U-H |
| H1266 | [矩阵 L1802](difftest-routers.md#L1802) | U-H |
| H1267 | [矩阵 L1803](difftest-routers.md#L1803) | U-H |
| H1268 | [矩阵 L1804](difftest-routers.md#L1804) | U-H |
| H1269 | [矩阵 L1805](difftest-routers.md#L1805) | U-H |
| H1272 | [矩阵 L1808](difftest-routers.md#L1808) | U-H |
| H1273 | [矩阵 L1809](difftest-routers.md#L1809) | U-H |
| H1274 | [矩阵 L1810](difftest-routers.md#L1810) | U-H |
| H1275 | [矩阵 L1811](difftest-routers.md#L1811) | U-H |
| H1276 | [矩阵 L1812](difftest-routers.md#L1812) | U-H |
| H1277 | [矩阵 L1813](difftest-routers.md#L1813) | U-H |
| H1279 | [矩阵 L1820](difftest-routers.md#L1820) | U-H |
| H1280 | [矩阵 L1821](difftest-routers.md#L1821) | U-H |
| H1281 | [矩阵 L1822](difftest-routers.md#L1822) | U-H |
| H1282 | [矩阵 L1823](difftest-routers.md#L1823) | U-H |
| H1283 | [矩阵 L1824](difftest-routers.md#L1824) | U-H |
| H1284 | [矩阵 L1825](difftest-routers.md#L1825) | U-H |
| H1285 | [矩阵 L1826](difftest-routers.md#L1826) | U-H |
| H1286 | [矩阵 L1827](difftest-routers.md#L1827) | U-H |
| H1287 | [矩阵 L1828](difftest-routers.md#L1828) | U-H |
| H1288 | [矩阵 L1829](difftest-routers.md#L1829) | U-H |
| H1294 | [矩阵 L1835](difftest-routers.md#L1835) | U-H |
| H1295 | [矩阵 L1836](difftest-routers.md#L1836) | U-H |
| H1297 | [矩阵 L1838](difftest-routers.md#L1838) | U-H |
| H1298 | [矩阵 L1839](difftest-routers.md#L1839) | U-H |
| H1299 | [矩阵 L1840](difftest-routers.md#L1840) | U-H |
| H1300 | [矩阵 L1841](difftest-routers.md#L1841) | U-H |
| H1301 | [矩阵 L1842](difftest-routers.md#L1842) | U-H |
| H1302 | [矩阵 L1843](difftest-routers.md#L1843) | U-H |
| H1303 | [矩阵 L1844](difftest-routers.md#L1844) | U-H |
| H1304 | [矩阵 L1845](difftest-routers.md#L1845) | U-H |
| H1305 | [矩阵 L1846](difftest-routers.md#L1846) | U-H |
| H1306 | [矩阵 L1847](difftest-routers.md#L1847) | U-H |
| H1307 | [矩阵 L1848](difftest-routers.md#L1848) | U-H |
| H1308 | [矩阵 L1849](difftest-routers.md#L1849) | U-H |
| H1309 | [矩阵 L1850](difftest-routers.md#L1850) | U-H |
| H1310 | [矩阵 L1851](difftest-routers.md#L1851) | U-H |
| H1311 | [矩阵 L1852](difftest-routers.md#L1852) | U-H |
| H1312 | [矩阵 L1853](difftest-routers.md#L1853) | U-H |
| H1313 | [矩阵 L1854](difftest-routers.md#L1854) | U-H |
| H1314 | [矩阵 L1855](difftest-routers.md#L1855) | U-H |
| H1315 | [矩阵 L1856](difftest-routers.md#L1856) | U-H |
| H1316 | [矩阵 L1857](difftest-routers.md#L1857) | U-H |
| H1317 | [矩阵 L1858](difftest-routers.md#L1858) | U-H |
| H1318 | [矩阵 L1859](difftest-routers.md#L1859) | U-H |
| H1319 | [矩阵 L1860](difftest-routers.md#L1860) | U-H |
| H1320 | [矩阵 L1861](difftest-routers.md#L1861) | U-H |
| H1321 | [矩阵 L1862](difftest-routers.md#L1862) | U-H |
| H1322 | [矩阵 L1863](difftest-routers.md#L1863) | U-H |
| H1323 | [矩阵 L1864](difftest-routers.md#L1864) | U-H |
| H1324 | [矩阵 L1865](difftest-routers.md#L1865) | U-H |
| H1325 | [矩阵 L1866](difftest-routers.md#L1866) | U-H |
| H1326 | [矩阵 L1867](difftest-routers.md#L1867) | U-H |
| H1327 | [矩阵 L1868](difftest-routers.md#L1868) | U-H |
| H1328 | [矩阵 L1869](difftest-routers.md#L1869) | U-H |
| H1329 | [矩阵 L1870](difftest-routers.md#L1870) | U-H |
| H1330 | [矩阵 L1871](difftest-routers.md#L1871) | U-H |
| H1331 | [矩阵 L1872](difftest-routers.md#L1872) | U-H |
| H1332 | [矩阵 L1873](difftest-routers.md#L1873) | U-H |
| H1333 | [矩阵 L1874](difftest-routers.md#L1874) | U-H |
| H1334 | [矩阵 L1875](difftest-routers.md#L1875) | U-H |
| H1335 | [矩阵 L1876](difftest-routers.md#L1876) | U-H |
| H1336 | [矩阵 L1877](difftest-routers.md#L1877) | U-H |
| H1337 | [矩阵 L1878](difftest-routers.md#L1878) | U-H |
| H1338 | [矩阵 L1879](difftest-routers.md#L1879) | U-H |
| H1339 | [矩阵 L1880](difftest-routers.md#L1880) | U-H |
| H1340 | [矩阵 L1881](difftest-routers.md#L1881) | U-H |
| H1341 | [矩阵 L1882](difftest-routers.md#L1882) | U-H |
| H1342 | [矩阵 L1883](difftest-routers.md#L1883) | U-H |
| H1343 | [矩阵 L1884](difftest-routers.md#L1884) | U-H |
| H1344 | [矩阵 L1885](difftest-routers.md#L1885) | U-H |
| H1345 | [矩阵 L1886](difftest-routers.md#L1886) | U-H |
| H1346 | [矩阵 L1887](difftest-routers.md#L1887) | U-H |
| H1347 | [矩阵 L1888](difftest-routers.md#L1888) | U-H |
| H1348 | [矩阵 L1889](difftest-routers.md#L1889) | U-H |
| H1349 | [矩阵 L1890](difftest-routers.md#L1890) | U-H |
| H1350 | [矩阵 L1891](difftest-routers.md#L1891) | U-H |
| H1351 | [矩阵 L1892](difftest-routers.md#L1892) | U-H |
| H1352 | [矩阵 L1893](difftest-routers.md#L1893) | U-H |
| H1353 | [矩阵 L1894](difftest-routers.md#L1894) | U-H |
| H1354 | [矩阵 L1895](difftest-routers.md#L1895) | U-H |
| H1355 | [矩阵 L1896](difftest-routers.md#L1896) | U-H |
| H1356 | [矩阵 L1902](difftest-routers.md#L1902) | U-H |

### device（30项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| D01 | [矩阵 L88](difftest-routers.md#L88) | U-D |
| D02 | [矩阵 L89](difftest-routers.md#L89) | U-D |
| D03 | [矩阵 L90](difftest-routers.md#L90) | U-D |
| D04 | [矩阵 L91](difftest-routers.md#L91) | U-D |
| D05 | [矩阵 L92](difftest-routers.md#L92) | U-D |
| D06 | [矩阵 L93](difftest-routers.md#L93) | U-D |
| D07 | [矩阵 L94](difftest-routers.md#L94) | U-D |
| D08 | [矩阵 L95](difftest-routers.md#L95) | U-D |
| D09 | [矩阵 L96](difftest-routers.md#L96) | U-D |
| D10 | [矩阵 L97](difftest-routers.md#L97) | U-D |
| D11 | [矩阵 L98](difftest-routers.md#L98) | U-D |
| D12 | [矩阵 L99](difftest-routers.md#L99) | U-D |
| D13 | [矩阵 L100](difftest-routers.md#L100) | U-D |
| D14 | [矩阵 L101](difftest-routers.md#L101) | U-D |
| D15 | [矩阵 L102](difftest-routers.md#L102) | U-D |
| D16 | [矩阵 L103](difftest-routers.md#L103) | U-D |
| D17 | [矩阵 L104](difftest-routers.md#L104) | U-D |
| D18 | [矩阵 L105](difftest-routers.md#L105) | U-D |
| D19 | [矩阵 L106](difftest-routers.md#L106) | U-D |
| D20 | [矩阵 L107](difftest-routers.md#L107) | U-D |
| D21 | [矩阵 L108](difftest-routers.md#L108) | U-D |
| D22 | [矩阵 L109](difftest-routers.md#L109) | U-D |
| D23 | [矩阵 L110](difftest-routers.md#L110) | U-D |
| D24 | [矩阵 L111](difftest-routers.md#L111) | U-D |
| D25 | [矩阵 L112](difftest-routers.md#L112) | U-D |
| D26 | [矩阵 L113](difftest-routers.md#L113) | U-D |
| D27 | [矩阵 L114](difftest-routers.md#L114) | U-D |
| D28 | [矩阵 L115](difftest-routers.md#L115) | U-D |
| D29 | [矩阵 L116](difftest-routers.md#L116) | U-D |
| D30 | [矩阵 L117](difftest-routers.md#L117) | U-D |

### generated（24项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| G01 | [矩阵 L139](difftest-routers.md#L139) | U-G |
| G02 | [矩阵 L140](difftest-routers.md#L140) | U-G |
| G03 | [矩阵 L141](difftest-routers.md#L141) | U-G |
| G04 | [矩阵 L142](difftest-routers.md#L142) | U-G |
| G05 | [矩阵 L143](difftest-routers.md#L143) | U-G |
| G06 | [矩阵 L144](difftest-routers.md#L144) | U-G |
| G07 | [矩阵 L145](difftest-routers.md#L145) | U-G |
| G08 | [矩阵 L146](difftest-routers.md#L146) | U-G |
| G09 | [矩阵 L147](difftest-routers.md#L147) | U-G |
| G10 | [矩阵 L148](difftest-routers.md#L148) | U-G |
| G11 | [矩阵 L149](difftest-routers.md#L149) | U-G |
| G12 | [矩阵 L150](difftest-routers.md#L150) | U-G |
| G13 | [矩阵 L151](difftest-routers.md#L151) | U-G |
| G14 | [矩阵 L152](difftest-routers.md#L152) | U-G |
| G15 | [矩阵 L153](difftest-routers.md#L153) | U-G |
| G16 | [矩阵 L154](difftest-routers.md#L154) | U-G |
| G17 | [矩阵 L155](difftest-routers.md#L155) | U-G |
| G18 | [矩阵 L156](difftest-routers.md#L156) | U-G |
| G19 | [矩阵 L157](difftest-routers.md#L157) | U-G |
| G20 | [矩阵 L158](difftest-routers.md#L158) | U-G |
| G21 | [矩阵 L159](difftest-routers.md#L159) | U-G |
| G22 | [矩阵 L160](difftest-routers.md#L160) | U-G |
| G23 | [矩阵 L161](difftest-routers.md#L161) | U-G |
| G24 | [矩阵 L162](difftest-routers.md#L162) | U-G |

### sequence（26项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| T01 | [矩阵 L168](difftest-routers.md#L168) | U-T |
| T02 | [矩阵 L169](difftest-routers.md#L169) | U-T |
| T04 | [矩阵 L171](difftest-routers.md#L171) | U-T |
| T05 | [矩阵 L172](difftest-routers.md#L172) | U-T |
| T06 | [矩阵 L173](difftest-routers.md#L173) | U-T |
| T07 | [矩阵 L174](difftest-routers.md#L174) | U-T |
| T08 | [矩阵 L175](difftest-routers.md#L175) | U-T |
| T09 | [矩阵 L176](difftest-routers.md#L176) | U-T |
| T10 | [矩阵 L177](difftest-routers.md#L177) | U-T |
| T11 | [矩阵 L178](difftest-routers.md#L178) | U-T |
| T12 | [矩阵 L179](difftest-routers.md#L179) | U-T |
| T13 | [矩阵 L180](difftest-routers.md#L180) | U-T |
| T14 | [矩阵 L181](difftest-routers.md#L181) | U-T |
| T15 | [矩阵 L182](difftest-routers.md#L182) | U-T |
| T16 | [矩阵 L183](difftest-routers.md#L183) | U-T |
| T17 | [矩阵 L184](difftest-routers.md#L184) | U-T |
| T18 | [矩阵 L185](difftest-routers.md#L185) | U-T |
| T19 | [矩阵 L186](difftest-routers.md#L186) | U-T |
| T20 | [矩阵 L187](difftest-routers.md#L187) | U-T |
| T21 | [矩阵 L188](difftest-routers.md#L188) | U-T |
| T22 | [矩阵 L189](difftest-routers.md#L189) | U-T |
| T23 | [矩阵 L190](difftest-routers.md#L190) | U-T |
| T24 | [矩阵 L191](difftest-routers.md#L191) | U-T |
| T25 | [矩阵 L216](difftest-routers.md#L216) | U-T |
| T26 | [矩阵 L217](difftest-routers.md#L217) | U-T |
| T27 | [矩阵 L218](difftest-routers.md#L218) | U-T |

### crosscutting（14项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| X01 | [矩阵 L199](difftest-routers.md#L199) | U-X |
| X02 | [矩阵 L200](difftest-routers.md#L200) | U-X |
| X03 | [矩阵 L201](difftest-routers.md#L201) | U-X |
| X04 | [矩阵 L202](difftest-routers.md#L202) | U-X |
| X05 | [矩阵 L203](difftest-routers.md#L203) | U-X |
| X06 | [矩阵 L204](difftest-routers.md#L204) | U-X |
| X07 | [矩阵 L205](difftest-routers.md#L205) | U-X |
| X08 | [矩阵 L206](difftest-routers.md#L206) | U-X |
| X09 | [矩阵 L207](difftest-routers.md#L207) | U-X |
| X10 | [矩阵 L208](difftest-routers.md#L208) | U-X |
| X11 | [矩阵 L209](difftest-routers.md#L209) | U-X |
| X12 | [矩阵 L210](difftest-routers.md#L210) | U-X |
| X13 | [矩阵 L211](difftest-routers.md#L211) | U-X |
| X14 | [矩阵 L212](difftest-routers.md#L212) | U-X |

### fixture（3项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| fixture-ADDR | [矩阵 L25](difftest-routers.md#L25) | U-F |
| fixture-RECEIPT | [矩阵 L27](difftest-routers.md#L27) | U-F |
| fixture-SECRET | [矩阵 L26](difftest-routers.md#L26) | U-F |

### fake-contract（3项）

| 矩阵编号 | 原行 | 原因 |
|---|---|---|
| fake-download | [矩阵 L259](difftest-routers.md#L259) | U-P |
| fake-ikuai | [矩阵 L257](difftest-routers.md#L257) | U-P |
| fake-routeros | [矩阵 L258](difftest-routers.md#L258) | U-P |


## R17 port 回填（不改变上文历史 Step B 覆盖统计）

`NATIVE_SIDE_REFERENCE_PROTOCOL_COLLISION`：C26 已修，R17 的
`internal/api/clientdevices/handlers_test.go:TestR17UDPProtocolSideEntryHTTP`
通过正式 GET/PUT：主监听 tag=udp、独立旁路的 udp:// 上游不被当成引用，
页面投影可编辑，保存策略后旁路插件仍保留。产品未再复制协议解析规则。

R17 自身另有 `TestR17FlaskHTTPAndStoredState` 的 15 请求真实 Flask↔Go 差分，
覆盖五路由成功与代表性失败，比较每步完整 profile、HTTP 状态/JSON/no-store 与最终设备表；
单独回归 HEAD 只读、Agent pinning、跨设备凭据引用和坏发现行隔离。
这是作者定向证据，不能替代独立 Step B 全部未覆盖项、fresh Opus HR 复核或 L5。
