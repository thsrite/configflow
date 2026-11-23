# O17 W5/W6 对抗复核 + W7/W8 合入（分支 go-o17-wall）

2026-11-24，Opus 5.5。基线：`go-o17-w@298956202`（go 之上 W0–W6）。本文件记录两件事：
① 把 W7/W8（分支 go-o17-w78，基于 W4）合进 W6 之上；② 对 W5、W6 做对抗复核（Agent 状态机、HA 成对发布、规则同步、DNS 联动唤醒）。

## 1. W7/W8 合入方式

`git cherry-pick` 5 个提交（1f7fa4e66、0d3e6ac19、29e40074c、a653b15aa、2684ac532），新提交号：

| 原提交 | 新提交 | 冲突 |
|---|---|---|
| 1f7fa4e66 W7 convert 只设一次（D2） | fce8a0f74 | 无 |
| 0d3e6ac19 W7 生成链路实例化 | c6dc7ac0c | `server/app.go`（手工合并，见下） |
| 29e40074c W8 rulesource | bba0dfd85 | 无 |
| a653b15aa W8 setup/onboarding | 434a0348b | `server/app.go`（只差 rulesource 两行） |
| 2684ac532 W7/W8 文档 | 861bf6907 | `design/o17-wiring.md` 表格行（W5/W6 取本分支、W7/W8 取对方） |

四个代码提交在 .32 上逐个 `go build ./... && go vet ./internal/server/ ./cmd/...` 通过（每个提交都能编译）。

`app.go` 合并规则（每个注册恰好一次）：

- 构造顺序：`newGenerateServices`（substore、refresh、provider、aggregations）→ `wireAgents(…, gen)`（Manager + 8 个 Agent 子服务）→
  `rulesource` → `registerStartup(rt, gen.substore, m, mt)` → webdav → `wireOtherSubsystems(rt, tk, gen, rs)`（只剩生成链路的 loop/任务行、
  discovery、convert 环境、routers、setup、rulesource 注入）。
- W7 `wireSubsystems` 里与 W6 重复的 `rulesync.WireOn`、`agents.Wire`、`dnsfollow/dnsha.RegisterOn`、`metrics.WireOn/WireTraffic`、
  `monitor.WireOn`、`health.Wire`、`wireIPv6On` 全部去掉：生产路径（有 Repo）由 `wireAgents` 用实例注册一次；只有 `NewApp()` 测试路径
  （无 Repo）仍走这些全局 Wire，同样只一次。
- `deploy.Service` 从 `newGenerateServices` 挪到 `wireAgents` 里 HA/IPv6 实例建好之后：`deploy.New(deploy.Options{Pair: ha.PublishPair 适配, IPv6: ip.EnrollSource})`
  直接注入，并 `deploy.UseService` 供 W10 前的 api/agents 使用。W6 的过渡桥 `deploy.SetPairPublication`、`deploy.SetIPv6Enroll` 从生产路径删除。
- `deploy.Wire()`（`provider.SetLocations` + 全局 `ReceiptMatches`）不再调用：Locations 由 `provider.New` 注入，ReceiptMatches 走
  `agents.Options`；NewApp 路径只补全局 `ReceiptMatches`。
- `refresh.OnRulesRefreshed(rsync.WakeLoop)`（经 `refresh.Default()` 的包级函数）改为 `gen.refresh.OnRulesRefreshed(rsync.WakeLoop)`：
  订阅直接挂在 W7 的 refresh 实例上，不依赖 `UseService` 与 W6 组装的先后。
- `monitor.SetHARulesJSON(ha.PairRulesJSON)` 改为 `monitor.SetHARulesJSON(mon.PairRules)`：见发现 M1。
- `gateway.Wire()` 由 W7 的 `wireConvertOnce`（`convert.SetMihomoIntegrations(gateway.Hooks())`）取代；`apirules.Wire()` 由 W8 的 `wireRuleSource(rs)` 取代。

新增测试（提交 95fa0754c）：`lifecycle.Runtime.Registration()` 按顺序列出启动步骤/周期任务/关停钩子，并记录每次「同名原地替换」。
`server.TestNewRegistersEachLifecycleTaskOnce`：生产组装 `server.New` 之后以及 `RunStartup` 之后，替换列表为空；启动步骤顺序、周期任务集合、
关停钩子集合与 go 一致。go 的基线是在 .32 上对 `git archive go` 的副本加临时探针跑出来的（`registerStartup()` + `NewApp()`，未提交）：

- 启动：load-config、subscription-cache-prune、agent-manager-init、metrics-load-history、webdav-worker-init（go 上 metrics-load-history 与
  subscription-cache-prune 各被注册两次、靠同名替换去重；现在各一次）；
- 周期（9）：agent-rule-sync、dns-follow、oxidns-ha-publish、metrics-history-flush、oxidns-ranking-history、scheduled-refresh、
  domain-discovery-auto、router-rule-sync、ipv6-verification-scheduler（go 上最后一个在 agent-manager-init 里注册，现在构造时注册）；
- 关停（4，RunStartup 后 +webdav-auto-backup）：agent-content-sync-close、metrics-history-flush-first、metrics-history-flush-last、domain-discovery-cache。

## 2. 发现

### 高

无。

### 中

- **M1 monitor 的两阶段 HA 规则 owner 在生产里不生效**（W6a）。`monitor.Service.SetHARules` 装在实例上，但生产唯一的调用路径
  `monitor.CallRuleUpdate`（`agents/rulesync/adapter.go` 自动规则同步、`api/agents/panel.go` 手动更新规则）读的是包级 `haJSON`，由过渡桥
  `monitor.SetHARulesJSON(ha.PairRulesJSON)` 填。实例 setter 只有测试调用，D1「monitor↔dnsha 两阶段」名存实亡；且两份 owner 可以不一致。
  修复（合入提交 c6dc7ac0c）：桥改为 `monitor.SetHARulesJSON(mon.PairRules)`，包级入口经由实例分发，实例的两阶段 owner 是唯一来源，
  未设时实例返回 `ErrHARulesNotPorted`（fail closed，与改前一致）。W10 把 `CallRuleUpdate` 改成 `*monitor.Service` 方法后删桥。
- **M2 已封印的 Manager 仍回退全局钩子**（W5）。`GetDeploymentModel`/`ActivateDeploymentModel`/`RunServiceOperation` 等 8 处在实例字段为 nil 时
  读 `currentHooks()`。生产组装的 Manager 若 PairPublisher 未设，只要进程里有人装过全局 HA owner（`dnsha.RegisterOn`，NewApp 路径会装），
  成对发布就会经 `dnsha.ForManager` 另建一个 HA 服务继续执行，而不是 `ErrHAOwnerMissing`。现有负对照测试要先 `ResetHooks()` 才成立，
  恰好掩盖了这一点。修复 77366a068：`legacyHooks()` 在 `MarkStarted` 之后返回空 Hooks；新增负对照
  `TestSealedManagerIgnoresGlobalHAHooks`（全局 owner 存在时，封印的 Manager 仍 fail closed，未封印的仍走旧回退）。
- **M3（合入时发现）规则刷新唤醒依赖组装先后**。W6 用包级 `refresh.OnRulesRefreshed` 订阅，W7 把它变成 `refresh.Default()` 实例上的列表、
  且 `UseService` 在 `newGenerateServices` 里才装。若按 W6 原顺序先组装 Agent，订阅会挂在被替换掉的默认实例上，规则源刷新后
  规则同步不再被唤醒（只剩 1 分钟的空闲兜底）。合并时改为直接订阅 `gen.refresh`，并由 `TestNewConnectsAgentLoopWakeSources` 覆盖。

### 低

- **L1** `SetPairPublisher(nil)` / `SetHARules(nil)` 在启动前返回 nil 且不算「已设」，之后还能再设一次。server 从不传 nil，未改；W11 可改为拒绝 nil。
- **L2** Manager 的创建（含一次性的旧版 `runtime.sqlite3` 心跳导入）从启动步骤 `agent-manager-init` 提前到 `server.New`，即早于 `load-config`、
  `subscription-cache-prune`。导入只读 Agent 列表、写心跳状态，不依赖 `PrepareConfig` 的默认域名；`agent-manager-init` 在生产里成了空步骤
  （`RunInitHooks` 没有生产注册者）。保留名字以维持启动顺序，W11 删 `Init` 时一起处理。
- **L3** `agents.Get(ctx)` 在 ctx 的仓库与 `UseManager` 装的 Manager 不同时会新建一个**无 Options** 的 Manager 并关闭生产实例
  （OnClose 会注销 dnsha/dnsfollow 实例），`rulesync.Get` 同理会换成无 Pair 钩子的服务。与 go 行为相同（单仓库进程里不会发生），W11 删定位器时消失。
- **L4** `health.Wire()` 在生产里与 `Options.DeletedStores` 重复登记（全局那份因 DeletedStores 非空从不执行）。已改为只在 NewApp 路径调用（fccd8b75b）。
- **L5** `wireConvertOnce` 吞掉 `ErrMihomoEnvironmentAlreadySet`：同进程第二个 `server.New`（只有测试）沿用第一个 App 的 substore/provider 环境。
  W12 或 W11 处理，生产只构造一次。
- **L6（测试回归）** W5 删除 `agents.SetRuleSyncRow/SetDNSFollowRow` 后，走旧全局 Wire 的
  `notifications.TestO10T3NotificationSnapshotSkipsHistoryRedactsAndInvalidatesRouterCache` 缺 agent 任务槽而失败（作者只跑了子集）。
  生产不受影响（`wireAgents` 用 agenttasks 装槽）。修复：测试自己经 agenttasks 装槽（提交见第 5 节）。

### X1、X2（W0/W1 引入，协调者指派在本分支修复）

- **X1** `api/agents/difftest.TestPublishHTTPAgainstFlask`：Go 一侧多出 `configflow.sqlite3-journal`（go、W0 通过，W1 9b56978ea 起失败）。
  根因是**夹具**，不是生产：W1 起 `NewApp()` 经 `server.New` 注册启动步骤，其中 `agent-manager-init` 会注册真实的 IPv6 验证调度器。
  ① 夹具在 `holdIPv6Scheduler`（把调度器换成空轮次，对应 Python 夹具的 `CONFIGFLOW_DIFF_HOLD_IPV6_SCHEDULER`）之后才 `lifecycle.Start`，
  Start 里跑启动步骤又把真调度器注册回来；② 夹具复用前一个测试已 Stop 的进程级 runtime，先跑的 `RunStartup` 看到已关闭的那一代全部跳过。
  结果 Go 在 stage 之后跑了一轮真实 IPv6 验证：经 `GetDeploymentModel` 读设备回复，把部署记录的 `config_version` 写成完整 sha
  （Python 仍是 8 位），并记录任务状态 → 通知异步刷新，快照恰好拍到写事务中的 journal（等写完再比就暴露出 `config_version` 的真实差异）。
  最终修法（协调者裁定二选一）：采用 review-w0-8 的 97bc72176（本分支 d7052830b，按合并后的 `registerStartup(rt, sub, m, mt, process)` 结构改写）：
  `NewApp()` 测试辅助恢复 W1 之前的注册（不含 load-config、agent-manager-init），`server.New` 生产路径不变。我的 5f9b21b60（夹具改顺序 + 换新 runtime）
  已 revert（e8e4b712a）：它只修了这一个用例，而 NewApp 多注册进程级启动步骤会改变所有 NewApp + `lifecycle.Start` 的夹具语义。
- **X2** `api/agents.TestHeartbeatReportedHostUsesAddressFollow`（W0 11a1435f5 起失败）：根因是**测试**。W0 把钩子换成真实的
  `upgrades.FollowModel`/`ConfirmedModel`，但共用夹具存的 host 是 `127.0.0.1`、上报的是 `192.0.2.10`——两者不同，FollowModel 会启动
  地址检查后台任务（默认 runtime 被前面测试停掉时 500「runtime is stopping」），ConfirmedModel 也不会回 `confirmed_host`。
  生产行为与 go 相同（go 生产里 `upgrades.Wire` 装的正是这两个函数；关停中的 runtime 上拒绝启动后台任务也是 go 的既有语义）。
  最终采用 review-w0-8 的 871edc8d8（本分支 be497d687，另含回环地址负对照），我的 2f16f9161 已 revert（d1227c093）。

## 3. 逐项结论

### 3.1 D1 两阶段 SetPairPublisher

- Start 前未设：`m.pair == nil` → 不再读全局（M2 修复后）→ `defaultHADeploymentModel`/`defaultHAActivateModel` 对活跃 HA 返回
  `ErrHAOwnerMissing`，`RunServiceOperation` 返回 409 拒绝；deploy 侧 `Options.Pair == nil` 时 `pairRequired` 目标返回 `ErrPairPublisherMissing`。
- 只能设一次：第二次 `ErrPairPublisherAlreadySet`，`MarkStarted` 后 `ErrManagerStarted`，都记日志（`TestManagerPairPublisherFailClosedAndTwoPhase`、
  `app_test` 断言 New 之后再设返回 `ErrManagerStarted`）。nil 见 L1。
- 并发：`pair`/`pairSet`/`started` 都在 `optsMu` 下读写；其余 Options 字段只在 `New` 里赋值、之后只读。新增 `TestSetPairPublisherConcurrentOnce`
  （8 个 setter + 读者 + MarkStarted 并发，`-race`，至多一个成功）。

### 3.2 Hooks → Options 逐字段（生产 `server/agents.go`）

| 字段 | 生产填的值 | nil 时行为（与改前钩子未装一致） |
|---|---|---|
| `Metrics` | `metrics.New(dataDir, ts)` | 不记录指标、删 Agent 不清历史（原钩子未装同样） |
| `Publication` | `monitor.New(dataDir)` | 检查点/完成统计为 no-op |
| `ReceiptMatches` | `deploy.ReceiptMatches` | false（fail closed） |
| `Pair`（两阶段） | `dnsha.New(m)`，`MarkStarted` 前设 | `ErrHAOwnerMissing`/409（fail closed） |
| `DeletedStores` | traffic_store、domain_probe、region_check（顺序 = `AgentDeletedStores`，`app_test` 断言） | 回退全局 `AgentDeletedHooks()` |
| `Client` | nil → `DefaultClient()` | 与 go 相同 |
| `RuntimeState` | nil → `RuntimeFor(dataDir)` | 与 go 相同 |
| notifications Agent 源 | `nt.SetAgents(m.NotificationAgents)`（D6） | `Validate` 报缺槽 |
| 任务行 SlotAgents | `agenttasks.BuildRows(m, rulesync.TaskRow, dnsfollow.TaskRow)` | `Validate` 报缺槽 |
| dns_follow 运行器 | `follow.Runner()` | — |
| rulesync Pair 钩子 | `Advance: ha.Advance`、`FailureMessage: ha.PairFailureMessage` | 回退包级 `advancePair`/`pairFailureMessage` |
| monitor HA 规则（两阶段） | `mon.SetHARules(ha.PairRulesJSON)` + 桥 `SetHARulesJSON(mon.PairRules)`（M1） | `ErrHARulesNotPorted` |

### 3.3 唤醒源（原连接 → 现连接）

| loop | go 上的连接 | 现在的连接 | 测试 |
|---|---|---|---|
| agent-rule-sync | 包级 `loop.Watch(m, Config\|State, presence)`（rulesync.Get）；`server.wireLoopWakes`: `refresh.OnRulesRefreshed(rulesync.WakeLoop)` | `rulesync.New` 内 `w.Watch(m, Config\|State, true)`；`gen.refresh.OnRulesRefreshed(rsync.WakeLoop)` | 包内 `TestServiceLoopWakesOnConfigStatePresenceAndRefresh`；生产组装 `TestNewConnectsAgentLoopWakeSources` |
| dns-follow | `dnsfollow.Wire(m)`（OnManagerInit）: `loop.Watch(m, Config\|State, true)` | `dnsfollow.New` 内实例 waker `Watch(…, true)` | 同上两层 |
| oxidns-ha-publish | `dnsha.ForManager`（OnManagerInit）: `loop.Watch(m, Config\|State, true)` | `dnsha.New` 内实例 waker `Watch(…, true)` | 同上两层 |
| ipv6-verification-scheduler | OnManagerInit: `schedulerLoop.Watch(m, Config\|State, false)` | `ipv6.NewService` 内实例 waker `Watch(…, false)` | 同上两层 |

loop 的 `Done(busy)` 节奏：dnsfollow/dnsha 在 go 上已经是 `s.waker.Done`，现在 waker 是实例的；rulesync `done()` 同时通知实例 waker 与包级 loop
（包级那份在生产里不驱动任何 loop，无害）；ipv6 `Loop()` 用实例 waker。新增的生产组装测试直接对注册的实例取 `Wake` 信号，
逐个事件断言「该醒的醒、不该醒的不醒」（ipv6 不跟 Agent 上线、只有规则同步跟规则刷新），规则刷新走 `refresh.Default()`（refresh 轮次实际调用的路径）。

### 3.4 过渡层只被测试和未迁移包使用

| 过渡层 | 生产里的使用者 | 删除时机 |
|---|---|---|
| `agents.Hooks`/`RegisterHooks`/`currentHooks` | 生产 Manager 已封印，不再读（M2）；`NewApp()` 测试路径装 | W11 |
| `agents.UseManager`/`agents.Get` | api/agents 等 W10 包、metrics flush、monitor rankings、rulesync 行投影、discovery、server/setup.go（经 UseManager 拿到同一实例） | W10/W11 |
| `agents.OnManagerInit`/`RunInitHooks`/`Init` | 只有 RegisterOn/WireOn（NewApp 路径）注册；生产 `RunInitHooks` 无事可做（L2） | W11 |
| `dnsha.ForManager`/`dnsfollow.ForManager` | api/agents/dnsha.go、dnsfollow.go（W10），经 `RegisterInstance` 返回 server 实例 | W10 |
| `rulesync.UseService`/`rulesync.Get` | api/agents/rulesync.go（W10）、`TaskRow` | W10 |
| `deploy.SetPairPublication`/`SetIPv6Enroll` | 生产不再调用（直接注入）；dnsha `installHooks`、ipv6 `WireOn`（NewApp 路径）与测试 | W11 |
| `monitor.SetHARulesJSON` | 生产桥（经 `mon.PairRules`，M1） | W10 |
| `routers.IPv6HostRequest` | 生产赋值（W9 改 `routers.Options{IPv6Hosts}`） | W9 |
| `lifecycle.UseRuntime`/`Current` 桥 | `routers/rule_sync_tasks.go` 的 `router-rule-sync` 仍用包级 `lifecycle.RegisterPeriodic`（W9）；其余 `Current()` 只在 rt 为 nil 的回退分支 | W9/W11 |

结论：W5/W6 范围内（Agent 子领域）的生产路径不依赖任何全局钩子或按 Manager 的 map；剩下的依赖都在 W9/W10 才迁移的包里，W9/W11 可删。

### 3.5 monitor 全量 -race

`go test -race ./internal/agents/...` 覆盖 monitor 整包（不再只跑作者的子集），结果见第 4 节。

## 4. 验证（.32，`RT_HOST=sunbroke@192.168.0.32`，rt 目录 `.paseo__worktrees__302rx1dg__go-o17-wall`）

- `gofmt -l .` 无输出；`go vet ./...` 通过。
- `go test -race -count=1 -p 6 ./internal/agents/... ./internal/server/... ./cmd/...`：exit 0（含 monitor 整包、新增的 D1/唤醒/注册测试）。
- `CONFIGFLOW_DIFF=1 go test -count=1 -p 6` api/agents/difftest、agents/dnsfollow/difftest、agents/dnsha/difftest：dnsfollow ok 35.4s、dnsha ok 33.9s；
  api/agents/difftest 仅 `TestPublishHTTPAgainstFlask` 失败（X1，W1 起已有）。
- `PYREF_MODE=check go test ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/routersplit`：oxidns 7 份录制因 lifecycle 源哈希变化
  失配，已重录（只有 metadata 成员变化，逐文件核对），重录后四包全部 ok。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`：`baseline: new=0 stale=0`、self_check PASS、`GATE STAGE PASS: proddiff`；
  `go_startup_once` = agent-manager-init、load-config、metrics-load-history、subscription-cache-prune、webdav-worker-init。
- `/tmp/preflight.sh … -p 6`：第一次失败两项——notifications O10 T3（L6，已修）与 X2；修复后重跑只剩 X2：
  `--- FAIL: TestHeartbeatReportedHostUsesAddressFollow … start Agent address check: lifecycle: runtime is stopping` / `PREFLIGHT FAIL tests`（W0 起已有，转交 review-w0-8）。

### 4.1 X1/X2 修复后（.32）

- `go test -race -count=1 -p 6 ./internal/api/agents/...`：exit 0（`ok configflow/internal/api/agents 324.907s`）。
- `CONFIGFLOW_DIFF=1 go test -count=1 -p 6 ./internal/api/agents/difftest`：`ok configflow/internal/api/agents/difftest 272.590s`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__go-o17-wall -p 6`：`PREFLIGHT PASS`。

## 5. 提交

fce8a0f74、c6dc7ac0c、bba0dfd85、434a0348b、861bf6907（W7/W8 cherry-pick，c6dc7ac0c 含 M1/M3 与组装合并）、
95fa0754c（注册恰好一次测试）、77366a068（M2）、fccd8b75b（唤醒矩阵测试、L4）、87e44facd（oxidns pyref 重录）、
ab5f6f4e0（L6）；X1/X2 最终为 d7052830b、be497d687（review-w0-8 的修法），本文件提交见 `git log`。
