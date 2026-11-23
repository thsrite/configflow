# O17 1.5 显式组装 W0–W4、W7、W8 复核（Opus 5.5，2026-11-24）

分支 `review-w0-8`（起点 = `go-o17-w78@2684ac532`，含 W0–W4 的 7 个提交 + W7/W8 的 5 个提交）。
复核修复只追加提交，未变基、未改写已有提交。测试全部在 `.64` 上用 `rt` 跑。

## 结论

- 发现 **高 1、中 5、低 4**。高/中全部已修（6 个追加提交），低项记录待 W9–W11 处理。
- 生产组装路径：周期循环、启动步骤、关停钩子的名单**和顺序**、任务槽位/runner 与 go 基线（`0a5d08ca1`）逐项一致
  （临时探针对比，未提交）；proddiff `new=0 stale=0`。
- 新增 `TestProductionAssemblyLeavesNoDependencyUnset`：按 `cmd/configflow.run` 的方式从清空的钩子开始组装，
  逐个断言被 W0–W8 挪动的依赖非 nil（D9 类事故在测试里就会失败）。

## 发现与修复

| 级别 | 位置（引入提交） | 问题 | 修复 |
|---|---|---|---|
| 高 | `provider/service.go` `DeliveryEnv()`（W7 `0d3e6ac19`） | 生产里 `provider.New(Options{SubStore: sub})` 把转换器设成裸 `sub.Client`，绕过了原默认环境的 `convert.OptionalNodeConverter`：Sub-Store 转换失败的**单个**订阅节点会让整个 provider 交付报错，原来是跳过该节点。proddiff 没有 Sub-Store，覆盖不到 | `4369869cb`：包回 `OptionalNodeConverter`；测试带正对照（裸 client 必失败，交付转换器跳过） |
| 中 | `repo/node_delete.go`（W2 `36ed68477`） | 搬家时 `httpx.HTTPError{404, "部分节点已不存在，请刷新节点列表"}` 改成 `repo.notFound(...)`，经仓库错误处理器变成 `"Profile not found: 部分节点已不存在…"`（中文提示逐字不一致）。go 基线上新测试通过，本分支失败 | `ec28b1e50`：新增错误种类 `repo.ErrNodesGone`，nodes 路由按原样答 404 |
| 中 | `server.NewApp`（W1 `9b56978ea`） | W1 声称「NewApp 行为不变」，实际让它也注册了 cmd 专属的 `load-config`、`agent-manager-init`。差分测试 `startGoWith` 在 `lifecycle.Start` 里才初始化 Manager，晚于 `holdIPv6Scheduler`，真 IPv6 调度器写入 `ipv6_verifications`：`api/agents/difftest TestPublishHTTPAgainstFlask` 稳定失败（二分定位到 W1）。生产不受影响 | `97bc72176`：`New`（cmd 路径）照旧注册全部步骤；`NewApp` 只注册 W1 之前就有的 `subscription-cache-prune`、`metrics-load-history`、`webdav-worker-init` |
| 中 | `agents/rulesync/wire.go`（W0 `11a1435f5` + W7） | 规则刷新唤醒改成 `wakeOnce.Do(refresh.OnRulesRefreshed(WakeLoop))`；W7 后事件挂在 refresh **实例**上，Once 只订阅到第一次调用时的默认实例。生产今天顺序碰巧正确，但第二个 App、或调用顺序变化就静默失去唤醒 | `88994a12c`：删 Once，由 `server.New` 对本 App 的 `gen.refresh` 显式订阅；测试对两个 App 各验证一次唤醒 |
| 中 | `deploy.Service`（W7 `0d3e6ac19`） | `deploy.New(deploy.Options{})` 不带 HA 发布者/IPv6 登记，靠 `dnsha.RegisterOn`、`agentipv6.WireOn` 经 `Default()` 间接装上，且没有 `Validate`。漏装时所有 HA 成员发布 `ErrPairPublisherMissing`、IPv6 登记被静默跳过——正是 D9 那类「生产从未注册」 | `88994a12c`：`deploy.Service.Validate()`（缺任一返回哨兵错误），`server.New` 在组装后调用；负对照测试 |
| 中 | `api/agents/agents_test.go`（W0 `11a1435f5`） | W0 改写的 `TestHeartbeatReportedHostUsesAddressFollow` 在作者 HEAD 上就失败：存储的 host 是 `127.0.0.1`，上报 `192.0.2.10` 触发真实地址跟随（整包跑时 500「runtime is stopping」），preflight 不过 | `871edc8d8`：注册 host 为 `192.0.2.10` 的 Agent，并补「回环地址不确认、不改存储」的对照 |
| 低 | `server/server.go`（W1–W8） | `wirePackages` 已不被生产使用（生产走 `wireSubsystems`），`ipv6_wire_test` 仍断言它的末项，给出虚假保证 | `88994a12c`：删除死列表与断言，更新引用它的注释；`wireIPv6On` 在 `wireSubsystems` 末尾并注明须最后 |
| 低 | `server/generate.go` `wireConvertOnce`、`gateway.Wire`、`mihomoenv.Wire`（W7） | 吞掉 `ErrMihomo*AlreadySet`：同一进程多次 `New`（只有测试）时 convert 环境仍绑定第一个 App 的 substore/provider 实例；`gateway.Wire`/`mihomoenv.Wire` 对其他错误 `panic`（现在只有测试调用） | 未改。生产只 `New` 一次且第一次设置；W11（或 W12）随包级环境一起处理 |
| 低 | `repo/transaction.go`（W2 `412dd4e1d`） | 提交观察者同时调用实例 `OnConfigurationCommitted` 与全局 `ConfigurationCommittedRecords`；两者都装时 WebDAV 自动备份会触发两次。生产只走 `webdav.Subscribe(r)`，测试只走全局 | 未改，W11 删全局钩子时消失 |
| 低 | `server.NewApp` | `New` 出错时 `app, _ :=` 后空指针 | `88994a12c`：带错误信息退出（仅测试辅助） |

追加提交（按顺序）：`ec28b1e50`、`4369869cb`、`88994a12c`、`97bc72176`、`c10d9eb44`（mosdns pyref 重录：仅
`convert/mihomo_integrations.go` 源哈希一项变化，5 份黄金 entries 逐项相同）、`871edc8d8`。

## 重点 1：被删/改直调的钩子，生产里由谁提供

| 原钩子 | go 基线生产由谁填 | 本分支生产由谁提供 | 核对 |
|---|---|---|---|
| `repo.Hooks.InitAgentManager` | `agents.Wire`（`LoadConfig` 无生产调用方） | 删除；启动步骤 `agent-manager-init` | 死代码，正确 |
| `RecordHooks.RedactEasytierJSON` | `server.wireCore` | `repo` 直调 `convert.RedactEasyTierJSON` | 字段保留为已弃用空字段（作者登记，W9 删） |
| `RecordHooks.SanitizeExternalDocument/RedactOxidnsJSON/RedactRoutersJSON` | `configexport.Wire`、`routers.Wire` | `repo.Options.Export = configexport.Exporter{RedactRouters: routers.RedactRouters}`（`server.RepoOptions`） | 新测试：清空全局钩子后脱敏导出仍成功、无路由器密码 |
| `RecordHooks.JobLockRecord` | `routers.Wire` | 仍是 `routers.Wire` 全局钩子（`Options.RouterJobLock` 为 nil 时回退），W9 挪 | 新测试：生产组装后改路由器连接被放行（未装时 `ErrInUse`） |
| `RecordHooks.PlanNodeDeletionRecords` | `apinodes.Wire` | 下沉为 `repo.planNodeDeletion` 直调 | 见上「中」：错误映射已修 |
| `RecordHooks.ConfigurationCommittedRecords` | `webdav.Wire` | `webdav.Subscribe(r)` 实例观察者 | 生产只装实例这一份 |
| `notifications.Inputs.Rows/MissingTasks` | `server.wireCore` | `notifications` 直调 `tasks`（W4 后是实例 `tk`） | 槽位一致 |
| `agents.Hooks.PinnedWithheld` | `rulesync.Wire` | `api/rules` 直调 `rulesync.PinnedWithheld` | 测试改为 `Service.pinnedCheck` 接缝，best-effort 语义保留 |
| `agents.Hooks.AddressFollowTyped/AddressConfirmedTyped` | `upgrades.Wire` | `api/agents` 直调 `upgrades.FollowModel/ConfirmedModel` | 测试已修（见上） |
| `agents.Hooks.InstallationProfileTyped/RegisterTyped` | `installation.Wire` | `api/agents` 直调 `installation.Service` + `WrapError` | 错误包装与原 `hookError` 逐行相同 |
| `agents.Hooks.PublicationCheckpoint`（deploy 侧） | `monitor.Wire` | `deploy` 直调 `monitor.CheckpointStatistics` | agents 侧读取仍走钩子（monitor 仍注册） |
| `deploy.SetGatewayGuards` | `server.wireGatewayGuards` | `deploy` 直接构造 `host.Bridge` | 与原组合函数逐行相同 |
| `routersplit.SetSessionReader` | `setupflow.Wire` | 读路径直调 `setupflow.ProfileSessions`；生产无 `SetSessionReader` 调用 | 只剩测试接缝，W11 可删 |
| `oxidns.SetNativeClassicalIPLines`、`ippresence` 的 `FullTun` | `ippresence.Wire`（`FullTun` 随后被 `gateway.Wire` 覆盖） | `oxidns` 直调 `ippresence.PeekTypedClassicalIPLines`；`FullTun` 只留 gateway 一份 | 两份 `FullTun` 实现逐行相同 |
| `metrics` traffic flush、`monitor` ranking history、`api/agents` contentPrepare、`discovery` collaborators | 各包 `Wire`/`init` | 同包直调 | 生产原本都已装上，等价 |
| `wireLoopWakes` | `server` | `server.New` 显式订阅（本复核修正） | 见上「中」 |
| `apirules.Wire`（`RulesetContent`、`SetRuleSourceReader`） | `server` | `server.wireRuleSource(rs)`，读取逻辑搬到 `internal/rulesource` | 搬家逐行对照等价（见重点 5） |
| setup 三个 `RegisterExternalOwners`、`VerifyRouter`（D9） | `server` | 仍由 `server.New` 注册；DNS 改为 `setup.DefaultDNSOwner()`（D4） | 新测试与 `TestSetupOwnersAllRegisteredByApp` |
| `provider.SetLocations`、`SetAggregationRenderer`、`agents.Hooks.ReceiptMatches` | `deploy.Wire`、`aggregations.Wire` | `provider.New(Locations: deploy.Locations{})` + 两阶段 `SetAggregations` + `Validate`；`ReceiptMatches` 在 `newGenerateServices` 注册 | 新测试断言 `agents.Hooks` 全部字段非 nil |

`TestProductionAssemblyLeavesNoDependencyUnset` 断言：App 各服务非 nil 且与各包 `Default()`/`lifecycle.Current()` 是同一实例；
tasks/notifications/provider/deploy 的 `Validate()`；setup 四个 owner；onboarding 采纳回调；gateway 规则源读取器；
`routers.RulesetContent/IPv6HostRequest`；`agents.Hooks` 全字段；`convert` integrations 全字段；Mihomo 环境 nil 字段集合与
生产构造器相同且关键 6 项非 nil；导出脱敏走仓库实例；作业锁已装；规则刷新唤醒对每个 App 生效。
（为此加了三个只读访问函数：`convert.CurrentMihomoIntegrations`、`onboarding.RegisteredAdoptOwnedWrite`、
`gateway.RegisteredRuleSourceReader`，随对应包级钩子在 W11 删除。）

## 重点 2：fail closed 负对照

| 位置 | 未接时 | 负对照测试 |
|---|---|---|
| 导出脱敏 | `repo.Options.Export` 与全局都缺 → `ErrRepository`「脱敏」；`Exporter.RedactRouters` 为 nil → 错误 | `repo.TestOptionsExportSanitizerAndFailClosed`、`configexport.TestExporterFailsClosedWithoutRouterRedactor` |
| 路由器作业锁 | 实例与全局都缺 → `ErrInUse msgRouterJobBusy` | `repo.TestOptionsRouterJobLockAndFailClosed` |
| HA 成对发布 | `deploy.Service` 无 pair publisher → `ErrPairPublisherMissing`；新增 `Validate` 让启动即失败 | `deploy.TestServicePublishFailsClosedWithoutPairPublisher`、`server.TestDeployValidateRejectsMissingCollaborators` |
| provider 聚合 | 未设 → `ErrAggregationProviderMissing`；两阶段二次设置/启动后设置拒绝 | `provider.TestServiceFailClosedWhenAggregationsUnset`、`TestServiceSetAggregationsTwoPhaseEnforcement` |
| 通知 Agent 源 | 启动后/二次 `SetAgents` 拒绝 | `server.TestNewRegistersStartupAndPeriodicNames` |
| 向导 owners | `setup.NewWithOptions(r, Options{})` → 503 | `setup.TestNewWithOptionsNilOwnersFailClosed` |
| onboarding 采纳 | `onboarding.New` 无 adopter 且有待完成向导 → `ErrSetupUnavailable`（不回退全局） | `onboarding.TestNewNilAdopterFailsClosedDespiteGlobalHook` |
| gateway 规则源 | `NewConfigWithReader(cfg, nil)` → `ErrRulesetContentUnavailable` | `gateway.TestNewConfigWithReaderNilFailsClosed` |
| convert 集成/环境 | 只能设一次 | `convert.TestSetMihomoEnvironmentSetOnce`、`TestSetMihomoIntegrationsSetOnce` |

W0 里改成直调的钩子（安装注册 501、网关守卫 `ErrGatewayGuardMissing`、`ErrPreparationUnavailable`、routersplit
`HookMissingError`、`ErrHistoryNotPorted`）不再可能为 nil，原 fail closed 分支随之消失，不是变成 fail open。

## 重点 3：各步不变量

- **W2**：导出脱敏读 `r.options.Export`（导出所用仓库实例），全局钩子清空后仍脱敏（新测试）；生产只有 `cmd` 开的一个仓库，
  没有其他 `repo.Open` 生产调用。作业锁仍走 `routers.Wire` 全局钩子（W9 挪），实例字段优先。
- **W3**：`NewProcessRuntime` 用新 Registry（`Configure` 保留旧 Registry），已确认没有 `init()` 期的 lifecycle 注册会因此丢失；
  `RequestSeen`/`Spawn` 走 `lifecycle.Current()`，`UseRuntime` 后即本 rt；`ReportReady` 从包级 `Start` 挪到 `rt.ReportReady()`，
  cmd 调用一次。周期 8 项、启动 5 项、静态关停钩子 4 项的顺序与 go 基线逐项相同。
- **W4**：任务行顺序由固定 `slotOrder` 决定；生产组装后 `tasks.Missing()`、`notifications.MissingSources()` 为空，
  12 个 runner 类别的有无与基线相同。
- **W7**：proddiff `new=0`；`PYREF_MODE=check` 四包通过；set-once 有测试。provider 转换器问题见「高」。
- **W8**：`rulesource` 与原 `api/rules` 读取逻辑逐行对照等价（缓存→内联→离线→下载→备用 URL 回退、超时与截止时间、
  `ReadGateway` 的配置解析）；setup/onboarding/gateway 负对照齐全；`api/gatewayonboarding`、`api/linuxgateway`、
  `gateway/installation` difftest 通过。

## 重点 4：过渡层

`tasks/notifications/substore/refresh/provider/deploy/rulesource` 的 `Default()`/`UseService` 与 `lifecycle.Current()/UseRuntime`
**生产路径仍在用**：未迁移的调用方（agents、routers、api 包、`refresh.notifyRulesRefreshed`、`deploy.Publish` 等包级函数）
经它们拿到实例。它们是过渡而非独立状态：`server.New` 让每个 `Default()` 指向本 App 的实例（新测试断言同一指针）。
W11 删除时须先把这些包级入口的调用方改为注入（W5/W6/W9/W10）。`routersplit.SetSessionReader` 生产已不调用，W11 可直接删。

## 重点 5：审阅方式

`36ed68477`（planDeletion）、`29e40074c`（rulesource）用 `git diff -M` 对照搬家，非改名改动只有上面「中」的错误映射；
其余改签名/两阶段提交逐行审。

## 验证（全部 `.64`，`RT_HOST=sunbroke@192.168.0.64`，HEAD = `871edc8d8`）

```
# 受影响包 -race（第一轮修复后）
go test -count=1 -race ./internal/server/ ./internal/api/nodes/ ./internal/provider/ ./internal/deploy/ ./internal/agents/rulesync/ \
  ./internal/repo/ ./internal/gateway/... ./internal/convert/ ./internal/refresh/ ./internal/notifications/ ./internal/tasks/ \
  ./internal/lifecycle/ ./cmd/...        → 全部 ok（convert 456s、deploy 258s、repo 245s …）
# 最后两处改动后补跑
go test -count=1 -race -p 6 ./internal/server/ ./internal/api/agents/ ./cmd/configflow/ ./internal/agents/rulesync/ ./internal/api/nodes/ ./internal/provider/
  → ok server 61.9s / api/agents 260.7s / cmd/configflow 120.5s / rulesync 60.0s / api/nodes 20.8s / provider 93.3s
# difftest（CONFIGFLOW_DIFF=1，真 Python）21 个包
gatewayonboarding linuxgateway gateway/installation api/config api/subscriptions api/aggregations api/rules api/rulelibrary
provider refresh substore tasks webdav api/agents dnsha dnsfollow mosdns httpx mcp health discovery → 全部 ok
# pyref
PYREF_MODE=check go test -count=1 ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/routersplit
  → ok installer 117.2s / mosdns 50.6s / oxidns 64.8s / routersplit 2.8s
# proddiff
GATE_ONLY=proddiff server/scripts/gate.sh -p 6
  → "new": 0, "stale": 0, "self_check": "PASS"; baseline: new=0 stale=0; GATE STAGE PASS: proddiff
# preflight
/tmp/preflight.sh .paseo__worktrees__302rx1dg__review-w0-8 -p 6  → == vet / == full non-race tests (-p 6) / PREFLIGHT PASS
```

第一次 preflight 失败于 `TestHeartbeatReportedHostUsesAddressFollow`（作者 HEAD 同样失败），修复后通过。
