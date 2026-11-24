# O17 3.7 R 线复核（拆 `internal/routers` 为 13 个分层包）

复核者：Opus 5.5 新会话，未参与 R 线实施。分支 `review-r`（起点 = `go-o17-r@1c423f113`，基线 `go@9fc421b20`，
R0–R10 共 18 个提交）。只追加提交，没有变基、没有推送、没有开子 agent；全部检查在 .32/.64 上跑，只用 loopback fake
与合成数据，没有接触生产或实验网。

## 结论

**0 条 high，1 条 medium（门禁失败，已修复 `4091d7e05`），3 条 low 已修复（`ed41c0fe8`），另有 2 条 low、4 条记录项不改代码。**
没有发现行为差异：发往路由器的请求、带 state 的 JSON、报告段顺序、锁层级与改前一致。修复后 R 线可以合入。

验证方式（下面各节有数字）：

1. 逐提交归一化 diff（去掉包前缀、大小写与空白后的残差行）+ 对「改签名/改分发/收窄」的提交逐行审：R2a、R2、R3a、R5、R6、R8、R9、R10a。
2. **改前改后请求日志对照**：给 `routerfake`（两边相同，未被 R 线改动）临时加只写日志的钩子，在 `go@9fc421b20` 与本分支上逐个跑
   同名测试（`internal/routers/...`、`clients`、`agents/ipv6`、`api/routers` 全部测试，含 55 个真实 Python 边界测试），
   比对每个测试发往假 RouterOS / iKuai 的完整请求（方法、URI、原始 body 字节）。
3. routerdiff 按改动点挑世界，在基线二进制与本分支二进制上各跑一次，比对 Go 侧观测。
4. 非测试代码的字符串字面量多重集、所有带 `json` 标签结构体的标签序列、自定义编解码方法数量，基线 vs 本分支。

## 1. R2 / R8 对抗复核（写真实路由器）

### R2：`RegisterRouterOSHandler` → 显式 `switch`

- 13 种功能操作逐一对照原 6 个 `init()`：`ros_policy_replace/restore`、`nat_list_sync/restore`、`dns_guard_replace/restore`、
  `ipv6_fakeip_replace/restore`、`ipv6_routes_replace/restore`、`ipv6_dns_update`、`ipv6_pd_guard_sync/restore`。
  每种分发到的处理函数、所用解码器（`devhttp.DecodeJSON` 用于策略与 IPv6 fake-ip/routes，其余 `jsondecode.Unmarshal`）、
  成功回执（`{}` 或 undo JSON）完全一致。
- 未知操作：改前落到基础 `Apply` 解 `rosOperation`，`default` 返回 `MessageUnsupportedOperation`；改后 `default → ApplyBase`，同一路径，仍报错不放行。
  `ErrNotPorted` 分支随注册表一起消失（编译期保证）。新增 `TestRouterOSApplyDispatchCoversAllFeatureKinds` 覆盖 13 种。
- `lifecycle.Detach` + `ctx.Err()` 现在在 `Apply` 和 `ApplyBase` 各做一次，幂等，无行为差异。
- `ApplyMany`/`RestoreMany`：改前 `(*RouterOS).ApplyMany` 对非批量操作回调全量 `d.Apply`；改后 `ros.Client.ApplyManyWith(..., fallback)`，
  组合驱动 `RouterOS` 传自己的 `Apply`，结果相同。见 L4。
- 字符串字面量对比：非测试代码里基线独有的只有 13 个操作名（原来在 `case` 列表与注册各出现一次，现在只在 `switch` 一次）、
  两条重复定义的路径常量（`rosNAT`/`rosMangle` 与 `PathNAT`/`PathMangle` 合并，值相同）、一条基线就未使用的消息
  `messageROSPingNoResult`。没有任何发往设备的命令文本丢失或改变。

### R8：`rospolicy`

- `ReadState` 改为 `StateReader` 回调：生产调用点（`driver_routeros.go` 的分发、`release.go` 校验、`rulesync` 两处）都传
  `(*RouterOS).ReadPolicyState`，它就是原来的 `ReadState` 再拆出 `&state.State, state.NatContext`。
  `VerifyROSPolicy` 读不到 `NatContext` 仍返回 `messagePolicyUnreadable`。**回调为 nil 时原实现会 nil-func panic** → L1，已修。
- 内部批量：`rosDevice.applyOp` 只认 `nat_list_sync/restore`，其余交给 `ApplyBase`。`PrepareStatic` 只由 `plan.go` 用 NAT 操作 +
  同一策略操作之外的路由操作拼成（`rosPolicy.Ops` 只会有一个 `ROSPolicyReplace`），回滚日志里的 `operation` 步骤只会是这些操作的
  undo（`nat_list_restore` 或基础 `ros_restore`），所以收窄后的分发覆盖全部可达输入；万一出现别的种类会报不支持而不是放行。
- 写入、读回、失败回滚、恢复：请求日志对照（第 5 节）里 `TestR12*`、`TestNAT*Boundary`、`TestRouterOSBoundaryCorpus`、
  `rospolicy_safety`、`r12_safety` 等全部逐字节一致；routerdiff `native-*`、`policy-view`、`release-ros`、`ros-http-writes`
  世界 Go 侧输出一致。

## 2. R6 `RouterState` 拆分

- JSON 成员顺序：`dns_guard`、`ros_policy`、（嵌入 `device.State`）`routes … active_routes`、`nat_context`，与基线一致；
  `PolicyNATRetiring` 仍是 `json:"-"`。用脚本比对基线与本分支所有带 `json` 标签的结构体：除 `RouterState`/`State` 这一处拆分外，
  标签序列完全相同；嵌入字段只有改名（`DNSGuardReport`→`Report`、`*DNSSettings`→`*device.DNSSettings`），平铺结果不变；
  `MarshalJSON`/`UnmarshalJSON`/`Error`/`String` 方法数量不变（15/8/5/1）。
- `state_json_test` 只断言键顺序、不比字节（记录项 N1）；字节级等价由上面的结构体比对 + 请求日志对照 + proddiff 覆盖。
- `BuildReport`：改前 `ReportParts`（只有 `dns_guard_plan.go` 的 `init` 串接一次）→ 策略段 → IPv6 四段；改后显式按同一顺序调用。
  `rospolicy.ReportPart` 返回 `natRetiring`，`BuildReport` 赋给 `state.PolicyNATRetiring`，与原来「先置 nil 再追加」等价，
  且仍在 `BuildROSPolicy` 之后。`TestBuildReportSectionOrder` 覆盖段顺序。
- 原 `TestReportPartsJoinSummaryAndSync`（靠替换 `ReportParts` 钩子）被删除且没有替代 → L3，已补。
- `ipv6.ForwardHint` 收窄为 `ForwardInputs` + `RoutesReport` 后去掉了 `state == nil || report == nil` 判断：唯一调用点
  `inspect.go` 传的都是刚构造的非 nil 值，记录项 N2。

## 3. R5 锁层级

`routeLocks`（`lockx.LevelDevice`, `"router-operation"`，键 `[type, TrimRight(endpoint,"/")]`）与 `routerJobs`
（`LevelJob`, `"router-job"`）原样搬到 `oplock`，全仓只各有一个实例；`RouterOperationLock` 的 detach/10 分钟/冲突消息不变；
`rule_sync.go` 的 `syncWorkers`（`LevelJob`）不变。22 处 `lockx.Violations()` 断言在测试里全部保留并通过（`-race`）。

## 4. R10 `layers_test.go`

允许表与设计 1.2 逐包一致。在 .32 副本上临时加 `dnsguard → fixedip`（同层）和 `clients → routers`（外部）两个违规 import：

```
--- FAIL: TestRouterPackageLayers
    layers_test.go:106: layer violation: internal/routers/dnsguard imports configflow/internal/routers/fixedip (allowed: [ros spec device])
    layers_test.go:116: external layer violation: internal/clients must not import configflow/internal/routers
```

删除后 `ok`。没有检查 `routerfake` 不 import `routers`（它本来就不 import），记录项 N3。

## 5. 改前改后请求日志对照

`routerfake.Fake`/`Ikuai` 的 `ServeHTTP` 加一行 `reviewLog(...)`（只在 `ROUTERFAKE_LOG` 设置时追加 `方法 URI 原始body`），
两边打同一补丁；`go test -c` 后每个测试单独运行、单独日志。

| 项 | 数 |
|---|---|
| 两边共同的测试 | 407（基线独有 1 = 被删的 `TestReportPartsJoinSummaryAndSync`；本分支新增 5） |
| 记录的设备请求 | 55 281 |
| 请求序列逐字节相同 | 384 |
| 只差 crypto/rand 归属标记（`cf-ipv6-readiness:<24hex>`、`cf-client:<名>:<32hex>`、iKuai `RT<13hex>`/`cfip_<10hex>`） | 18 |
| 只差顺序（并发子测试 / Python 侧字典序） | 5 |
| 真实差异 | 0 |
| 退出码不一致 | 0（407 个两边都通过） |

## 6. routerdiff 抽样

R0 基线 `routerdiff-r0.json` 在 `rule-sync` 世界的 Python 前置条件（`R022/enrolled: sync/status 'failed'`）处中止，
只跑了 53 个世界；作者报告的「重叠 2685 条 status_changed=0」因此**完全不含** DNS 守卫、IPv6、策略视图、ros 写入、回程回滚等
R 线改动点（N4）。本次在 .32 上用 `go@9fc421b20` 与本分支各构建一次 routerdiff 二进制（同一 overlay），按世界跑：

- 第一组 16 个世界（`release-ros`、`release-ik-plain`、`release-ik-suffix`、`fixed-ip*`（4）、`manual-routes`、`native-*`（3）、
  `empty-manifests`、`runtime-reads`、`forwarding`、`unlinked-inspection`、`rule-sync`）：123 条。
- 第二组 15 个世界（`dns-guard`、`dns-ha`、`dns-matching-failures`、`ipv6-fakeip-*`（4）、`ros-http-writes`、`ros-http-normal`、
  `policy-view`、`return-rollback`、`return-protocol2`、`rule-sync-proxy`、`dhcp-ikuai-plain`、`disconnect-release`）：108 条。

231 条共同配方 Go 侧观测（去掉签名 token）比对：status 全部相同；只有 `rule-sync`/`rule-sync-proxy` 的 5 条
`fingerprint`/`version`/`synced_version` 不同——同一基线二进制连跑两次这几个字段也不同（基线对基线还多出 `routes`/`nat_not_required`
内容差异），属于该世界本身的不确定性，与 R 线无关。每次运行都在 45 分钟上限内（单次约 2 分钟）。

## 发现

### 已修复

- **M1 全量测试失败：`notifications.TestO10T3NotificationSnapshotSkipsHistoryRedactsAndInvalidatesRouterCache`**（`4091d7e05`）。
  R10a 把路由器规则同步的任务槽（`tasks.SlotRouters` 的行构造器与执行器）和 `CaptureManual` 从 `routers.Wire()` 挪到
  `routers/rulesync.Wire()`；这个按旧方式手工接线的测试只调 `routers.Wire()`（它里面的 `rulesync.Wire()` 是同名的 `agents/rulesync`），
  于是 `SnapshotItems: 任务模块尚未就绪：router_rule_sync`。生产 `server/app.go` 两个都调，不受影响；作者没有跑 `notifications` 包，
  preflight 抓到。测试补调 `routerrulesync.Wire()`。其他调 `routers.Wire()` 的测试（`clients`、`api/clientdevices`、`api/config`）
  不依赖该槽，preflight 全量通过。

以下三条在 `ed41c0fe8`：

- **L1 rospolicy：`StateReader` 为 nil 时 panic。** 4 处 `d.readState(ctx)` 直接调用回调；导出入口 `ApplyROSPolicy`/`ApplyPolicyWrite`/
  `VerifyROSPolicy` 接收调用方传入的回调，传 nil 会在写入中途 nil-func panic（请求路径禁止 panic）。现在统一走 `readFacts`：
  nil 回调或读回 `facts == nil` 都返回 `messagePolicyUnreadable`（与读不到 `NatContext` 同一阻断）。生产调用点都传非 nil，
  所以不改变现有行为。新增表驱动 `rospolicy.TestReadFactsFailsClosed`。
- **L2 routeedit：`ReadRoutes`/`ExecuteRoutePlan` 参数从 `Driver` 变成 `any`。** 违反「不用 any 参数」规则、丢掉编译期检查，
  设计 1.3 写的是小接口。改为 `RouteEditor`，三个调用方（`management.go` 两处、`api/routers/handlers.go`）做一次断言，nil 仍返回
  `messageEditUnsupported`；两个测试调用点跟着改。
- **L3 测试缺口：`TestReportPartsJoinSummaryAndSync` 删除无替代。** 补 `TestBuildReportJoinsPartSummaryAndSync`：用真实 IPv6 段验证
  状态并入 summary、in_sync 门控、段按键序列化、无 `RetireExtra` 时 extra 路由保留。

### 记录项（不改代码）

- **L4** `ros.Client` 自己的 `ApplyMany`/`RestoreMany` 回退到 `ApplyBase`，对功能操作会报「不支持」。所有生产路径都经过组合驱动
  `RouterOS`（覆盖了这两个方法）或 `rospolicy.rosDevice`（自带 `nat_list_*`），所以今天不可达；将来若有人把裸 `*ros.Client`
  当 `BatchDriver` 用，功能操作会被拒绝（fail closed，不会误写）。建议 W 线保持「只有组合驱动实现 `BatchDriver`」。
- **L5** `CaptureManual` 由 `rulesync.Wire()` 填，`production_wiring_test` 没有断言它已被替换（默认返回 `ErrNotPorted` 只记日志，
  不会误判基线成功）。`app.go` 已调用；W9 收进编排服务构造参数时一并消掉。
- **N1** `state_json_test` 只比键顺序不比字节（见第 2 节，已由其他手段覆盖）。
- **N2** `ForwardHint` 去掉了对 state/report 的 nil 判断，入参已是值类型，唯一调用点非 nil。
- **N3** `layers_test` 不检查 `routerfake`。
- **N4** 作者的 routerdiff 重叠对比不含改动点世界（见第 6 节）；本次抽样已补。`rule-sync` 世界的 Python 前置条件 R022 会随机失败
  （基线连跑两次都中止，本分支一次跑完），完整 routerdiff 应把它放到最后或单独跑，避免中止后面所有世界。

## 验证命令（最后几行）

```
# 目标包 -race（.64，修复前）
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -l ./internal/routers ./internal/server ./internal/agents ./internal/clients; go vet ./internal/routers/... ./internal/api/routers/... ./internal/clients/... ./internal/agents/ipv6/... ./internal/setup/... ./internal/server/... && go test -race -count=1 -p 6 ./internal/routers/... ./internal/api/routers/... ./internal/clients/... ./internal/agents/ipv6/... ./internal/setup/... ./internal/server/...'
ok  configflow/internal/routers 395.336s
ok  configflow/internal/routers/{clientlist,ikuai,oplock,ros,rulesync,spec}
ok  configflow/internal/api/routers 36.384s / clients 21.648s / agents/ipv6 18.054s / setup 27.741s / server 68.300s
（gofmt 无输出，vet 通过）

# 修复后（.64）：gofmt/vet + rospolicy、api/routers -race 全量 + routers 相关用例 -race
ok  configflow/internal/routers/rospolicy 1.030s
ok  configflow/internal/api/routers 41.784s
ok  configflow/internal/routers 66.704s

# proddiff（.64）
GATE_ONLY=proddiff server/scripts/gate.sh -p 6
"cases": 2831, "new": 0, "stale": 0, "self_check": "PASS"
GATE STAGE PASS: proddiff

# notifications 修复后（.64）
go test -race -count=1 ./internal/notifications/
ok  configflow/internal/notifications 16.132s

# preflight（.64，修复后）
/tmp/preflight.sh .paseo__worktrees__302rx1dg__review-r -p 6
== vet
== full non-race tests (-p 6)
PREFLIGHT PASS
（修复前同一命令：--- FAIL: TestO10T3NotificationSnapshotSkipsHistoryRedactsAndInvalidatesRouterCache … PREFLIGHT FAIL tests）
```
