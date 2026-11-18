# R11 Python 测试映射

所有新用例为合成输入、loopback fake。`TestR11PythonBoundary` 真实执行三份 Python 模块，比较
全部计划/报告/undo、错误、逐项设备表与顺序写请求；仅显式规范化可选空内部指针、Go 新增 PD config、
一个 DNS list 内部 repr/native JSON 案例、IPv6 state list-scoped KnownIDs（非route恢复不消费这些ID）及 readiness opaque fingerprint，不删业务字段或设备差异。
另用真实 Python 比较 PD 模板原始 source 字节。四条常设检查用真实 devhttp 和 Runtime。

| Python 文件 | Go 验收入口 | 归属与边界 |
|---|---|---|
| `test_router_ipv6_pd_guard.py`（8 个命名测试） | `TestR11TemplateBytes`、`TestR11PythonBoundary/pd/*`、既有 R01 `NormalizePDGuard` 测试 | 模板、派生、禁用 factory、多客户端/多LAN/池/地址/64/advertise/安全名称、归一化识别、hook/scheduler 状态。settings/public/连接禁止抛弃记录由 R01 与 repo 用例覆盖，R11不复制 validator。 |
| `test_router_ipv6_pd_guard_integration.py`（11） | `TestR11PythonBoundary/pd/{publish_idempotent,report,manual,update,foreign_*,no_client,withdraw,withdraw_rollback,preview_client_hook,refuse_*}`、`TestR11PDRuntimeSweepAndIDNamespaces`、`TestR11PDCountersAndLeaseEvidence` | 安装、沿用、更新接管、自己的 rollback helper 保留、相同跨表ID、二次清扫、钩子冲突、无PD、失败补偿、预览漂移、计数器稳定、外部修改。整体 release stage skipped/warning/log 顺序属于 R12-5b。 |
| `test_router_ipv6_dns.py`（22） | `TestR11PythonBoundary/dns/*`、`TestR11DNSFinalReadAndRollbackProtectsRA`、`TestR11DNSUnknownAndNoopEvidence`、`TestR11ReportsAndManifestCopies` | yes/self/no、空/停用/all、专用覆盖与共享保留、命令引号、两种 DNS 字段及 list、未知状态/错误服务器/重复条目、actual-interface、多LAN、DHCP pending/目标漂移、最后事务外改、读回漂移/no-op、受保护undo、报告无内部数据且无别名。 |
| `test_router_ipv6_dns_integration.py`（12） | `TestR11PythonBoundary/dns/{report,publish_idempotent,preview_*,dhcp_pending,dhcp_ambiguous,all}`、`TestR11BuildReportSections`、`TestR11DNSFinalReadAndRollbackProtectsRA` | DHCP先应用再IPv6DNS的驱动前提在R11实测；release 中前后 stage 的回滚链、skip/summary/log/order、再次 `_verify(dhcp_dns)` 属 R12-5b。三个自动同步暂停/跳过/漂移恢复测试归 R14，不能把 package 测试当其验收。 |
| `test_ipv6_readiness.py`（41） | `TestR11PythonBoundary/readiness/*`、`TestR11ReadinessReusesSnapshots`、`TestR11ReadinessRouteReceipts`、`TestR11RawWireAndProductionTimeouts`；R10 的 Gateway/Policy 边界矩阵 | RouterOS `_list/_get` 路由报告、无写预览、LLA/ULA/状态/接口/旧下一跳、TUN/重叠/冲突、只读 DNS/NAT、same-port 复用/失败/规则集、add/undo/丢回复和外改；R10已有完整候选/三值规则矩阵继续跑。该文件的 configuration、policy client 候选构造、部署配置比较、鉴权、缓存、check_token、prepare/probe API 与持久化均 A23，未在R11声称完成。 |

新增安全回归：

- `TestR11RuntimeOwners`：PD/DNS/readiness × request disconnect、shutdown apply/restore/empty、快照后与最终回读后关机，真实 Runtime，不假造 Detach 父级。
- `TestR11PDLostCompensationDelete`：F1 用新 GET 证明已删才继续；`TestR11PDPartialRestoreRemapsProvenIDs`：部分成功、table-qualified remap、拒绝同名外部恢复。
- `TestR11PDRefusedCompensationKeepsBirthProof`：内部/整stage补偿失败保留创建证明，外改后不刷新；`TestR11PDRejectsCorruptOwnershipAndCopies`：损坏/重复/跨表/非法ID/config/外改/复制。
- `TestR11LegacyAndUnknownManifestRoundTrip`：旧 ID-only、危险同名 legacy 被拒、未来字段/新config通过 model 保存；`TestR11PDPublicationStorage/AllWritersProtectProof/AbsentJobLockFailsClosed`：真实 SQLite，显式 publication/metadata/clear、所有repo写入口保护、无hook拒绝。
- `TestR11DeviceErrorsRedactOnlyCredentials`：共享脱敏，普通 username/api/rest-api 保留；原始请求体/headers/10s透传在 `TestR11RawWireAndProductionTimeouts`。
- `FuzzR11PDInput`、`FuzzR11DNSRow`：不可信 stored proof/模板参数/ND flags/server-list/CLI，纯解析无设备I/O。
- `scripts/r11-mutation-gate.py`：result/journal/request/device-table/recovery，编译失败不算检出，finally还原。
