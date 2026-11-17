# A14 HR 复核（Agent 发布：prepare、upload/activate、回滚、恢复、状态轮询、九条路由）

独立 Opus 复核，非移植者；分支 `go-opus-review-a14`，起点 `go-port-a14@4ac0d6e8`（基于 `go@28e6e0c1`）。
无子 agent、无 push、无生产/真实 Agent/设备/PVE 访问；测试只经 `rt` 在测试服务器执行，自写用例只用合成数据。

## 结论

- **A14 自身 high：0 条。** medium 1 条（M1，Python 同样存在），low 3 条（L1、L2 已修；L3 为基线粒度建议）。
- 共享 BeforeCommit 关机窗口（已分派 O3-6c）不计入；已复验报告写清、SKIP 的回归确实复现（见第 4 节）。
- A20 合入后的两项接线（去掉 deploy.Wire 重复注册的 reader、注册 `deploy.SetGatewayGuards`）及其测试，
  按协调者指示在 rebase 到含 A20 的 go 后完成，见第 6 节；在那之前 A14 结论不依赖它们。

## 发现

### medium

- **M1 — 手动回滚预留落盘后提交丢失，任务永久卡在「等待 Agent 确认」（Python 同样存在，已修）。**
  场景：`RollbackDeployment` 先在事务里写 `rolling_back` + `manual_rollback`，再 POST `…/rollback`。
  若关机取消（Detach 保留的关机 parent 在 grace−2s 取消绑定请求）或进程被杀发生在 Agent 读到请求之前，
  记录停在 `rolling_back`；`RecordDeployment(unknown)` 因关机被拒（这是对的，关机后不写）。重启后轮询：
  Agent 仍报 `succeeded`、没有 `manual_rollback`，A01 规则只在 `unknown` 时重发，于是永远返回
  `Rollback submission awaiting Agent confirmation`；该任务非终态，新发布被「unfinished deployment」拒绝，
  再点回滚只读到 `_already_reserved`，恢复只接受 `rollback_failed` —— 设备被锁死，只能改库。
  设计 §3.4 表格声称这一行「poll 即可恢复」，实际不成立。
  证据：`agents.TestReviewA14ManualRollbackWithoutLiveOwnerIsReissued` 用真实全局 Runtime（Configure/Start/
  BindRequest/Stop），在 fake 收到回滚 POST 时关机并以连接重置结束；修复前重启后 POST 次数 0、结果 pending。
  修复：`agents.rollbackKeys`（`lockx.Keyed` LevelJob，进程内）标记本进程正在提交的回滚，从预留前持有到处理完
  Agent 应答；轮询时若状态不是 `unknown` 但**没有活着的持有者**，与 `unknown` 一样重发同一个受 Agent 保护的幂等
  回滚（Agent 对已完成的手动回滚重放 200、对不允许的回滚 4xx → 走既有 `cancelManualRollback`）。持有期间行为与
  Python 相同（测试同时断言提交进行中的轮询仍是 pending、不重复 POST）。已记入 `behaviour-changes.md`。
  提交 `0a13a1b6a`。

### low

- **L1 — provider 准备进度每条都强制写库（已修）。** Python 把 `phase_progress` 直接交给
  `prepare_provider_bundle`，逐个 provider 的「正在准备…/…已准备」不带 `force`，受每秒一次节流；只有
  「正在校验节点来源与拨号引用关系」是 force。Go 的回调丢掉了 force 标志，全部走 `Phase`（强制），
  每个 provider 两次完整 system 文档 SQLite 提交。修复：`Progress` 增加可选 `Message`（不强制、可节流，
  nil 时回落到 Phase），provider 回调按 force 分流，`publishProgress` 把 Message 接到同一个 1 秒节流。
  证据：`TestFirstPushCachesAndStableContents` 新增断言（修复前报 `provider detail forced 正在准备订阅「Feed」`
  等），`TestReviewA14UnforcedProgressIsThrottled`。提交 `6ebd63d0e`。
- **L2 — push-config 的 500 分支不写日志（已修）。** Python 在 `except Exception` 记
  `Agent deployment preparation failed (%s)`（只有异常类名，不含文本）；Go 记录了任务并返回 500，但运维日志
  没有任何一行。修复：同一 logger 名 `backend.routes.agents`，只记 `%T`。同时这是 deploy/api 中**第一个**覆盖
  500 分支的测试：`TestReviewA14UnexpectedPushFailureIsLoggedAndRecorded`（A18 前 HA 成员按设计 fail closed）
  断言 500 外形、`failed`/`preparing` 记录、设备未收到上传、日志有类名且不含错误文本；去掉日志行时失败。
  提交 `6871cd22a`。
- **L3 — 新增的 A18 基线行粒度大于其理由（未改，建议）。** 行
  `POST /api/agents/<agent_id>/generate-config · body.message · value` 的说明是「HA 最终生成按设计拒绝」，但
  proddiff 基线按 route/键路径/类型匹配，会同时掩盖**任何** Agent 在 generate-config 上的 message 值差异（非 HA
  Agent 的回归也看不见），直到 A18 接入后变 stale。该行经协调者批准，且当前 proddiff 证明只有四个 HA 用例触发；
  建议 A18 前若再有 generate-config 相关改动，在 `run.py` 中把这一行限定为「Go message 恰为 A18 拒绝文本」，或
  尽早由 A18 删除。

## 逐项核对（任务重点 1–8）

1. **发布状态机。** 对照 `manager._publish_deployment`/`publish_deployment`/`push_config_to_agent` 与
   `routes/agents.py` 1340–2006 逐行核对。submitted 在 POST 前持久化（`uploading`+`submitted`+`sha256`/
   `source_sha256`/`config_version`/`config_revision`），且 Go 额外检查该写入确实生效（被并发轮询判为失败时不再
   POST，比 Python 安全）。迟到回执经 A01 `UploadReceipt` 不覆盖后续阶段；终态不回退；`manual_rollback` 不被
   轮询清除。重放：已 submitted 时比较内容身份，不同 → 409，相同 → `GetDeployment`；未 submitted 的
   `failed/rolled_back` 直接返回原任务。各步骤间被杀/关机后的恢复：preparing 未提交 → 重启后锁不再 Held →
   `Preparation interrupted…`；uploading 已提交 → Agent 404 → `Agent has no record…` 或 Agent 实际状态；
   Agent 已接收但应答未记录 → 保留 uploading，重启轮询得到 succeeded（既有真实测试）。准备锁改为进程内
   `lockx.Keyed`，在事务内 TryLock、事务失败时释放；`UpdateSystemTransaction` 不重试，不存在泄漏锁。
2. **回滚和恢复。** 设备 I/O 入口均 `lifecycle.Detach` + 入口 `ctx.Err()`；`DeploymentRequest` 每次尝试前检查；
   预留/取消/恢复写入都经 `UpdateAgents` 的事务前后检查。部分成功：恢复在「预留后未送达」时，重启轮询读 Agent
   日志回到 `rollback_failed`，可自愈；手动回滚见 M1（已修）。激活前 OxiDNS checkpoint 前检查 ctx。冻结 Geo 的
   重试身份：final_config 两次都用去掉 `resource` 的内容包 SHA（Python bug 已修，`TestFinalResourceRetryUsesContentIdentity`），
   非 final 的 Mihomo 两次都是加 Geo 之前的包，身份一致；同一 preview 的 deployment_id 唯一，不会把不同 Geo 当成同一内容重放。
3. **凭据脱敏，逐个输出面。** 错误信息：`safeFailure` = `diag.FailureText`（token + 凭据片段），push 响应再经
   `PublicDeployment`（投影前 `CollectCredentials`，再 `SanitizeCredentials`），generate 400 同 Python
   `failure_text`。日志：A14 新增日志只有类名（L2）和 panic 固定文本，无 URL/token。部署状态：进度消息经
   `FailureText(token)`，记录字段经 A01 的 failure_text；公开输出去掉 `prepared_content`/`oxidns_validation_receipt`。
   预览：只发冻结包，不重建 diff 文本（push-diff 属 A12）。归档/回执：私有 0600 目录，内容即用户配置，Python 同。
   `GET /config` 的 500 返回原始错误文本，与 Python 相同；调用方持有该 Agent token、本就取得完整生成配置，
   不构成额外泄露，未改。没有发现第二个私有脱敏器；用户名/名称保持可读。
4. **写 owner 和关机。** 共享 BeforeCommit 窗口：报告与 PROGRESS 均写明已分派 O3-6c、回归为显式 SKIP。复验：在
   .64 上删掉 skip 运行 `TestShutdownAtSQLiteCommitKeepsWholeDocument` → `FAIL: BeforeCommit shutdown returned <nil>`
   / `cancelled SQLite transaction committed`；打上 `progress/a14-shared-commit-window.patch` 后 `ok (0.342s)`；
   远端副本随后还原（未提交任何改动）。A14 自有入口：`PushConfig`/`GenerateFinal`/`Push`/`Publish`/`PublishDirect`/
   `PrepareContent` 经 `RunDetached`（嵌套时保留已跟踪 owner 的取消，`lifecycle.Tracked`）；`BeginDeployment`、
   `RecordDeployment`、`RememberPreparedContent`、`Get/Activate/Rollback/Recover`、`PushLegacyConfig`、
   `Receipts.Capture`、`Previews.Save`、`WriteFileAtomic`、provider 提交均 Detach + 提交点前 `ctx.Err()`；
   `recordUpdate`/`uploadRecord`/进度写入在关机后不写；publish 的 defer 在关机时不补写失败记录。真实 Runtime 测试：
   15 个入口关机后零写入零设备请求、绑定请求断开后完整完成、接收后关机保留不确定提交；本复核另加 M1 的真实
   Runtime 关机 + 重启用例。
5. **延期基线。** 删除的 24 行 = list 三台 × {checked_at, reason, status} 9 行 + detail 3 行 + push-diff 12 行，
   正是 A13 登记的「PrepareFunc 未接线」条目，无通配；真实 PrepareFunc 已经 `WireContentSync` 接线，
   handoff 记录的实际 proddiff `new=0 stale=0`。新增一行 A18 精确 message 行，状态码/其他字段一致的说法与
   `run.py` 比较逻辑相符，A18 删除义务已写入 PROGRESS-agents；粒度问题见 L3。
6. **C36 只读 reader。** 只有一套：`internal/setupflow.ProfileSessions`。与 Python `profile_sessions` 对照：
   文件名 32 位小写 hex、非对象/其他 profile 跳过、按 `updated_at` 倒序、不取向导锁；损坏 JSON/非 UTF-8 fail closed
   为 `ErrInvalidSession` 并计入 400 准备失败（Python 的 JSONDecodeError/UnicodeDecodeError 均为 ValueError）；
   未知字段原样保留在 Raw。发布 guard 回调的注册点是 `deploy.Wire` → `routersplit.SetProfileSessions`。
   A20 合入后按其复核要求改为只由 `setupflow.Wire` 注册（第 6 节）。
7. **路由。** 九条路由：八条 `auth.Require`，`GET /config` 在 `publicEndpoints`（`agents.get_agent_config`），
   自行校验 token 长度与归属，与 Python 白名单一致。HEAD：httpx 与 Werkzeug 一样为 GET 自动加 HEAD 并执行处理函数，
   因此 HEAD 轮询与 Python 相同（会向 Agent 查询并可能更新任务）；预览下载/文件、`/config` 为只读。请求体用
   `JSONSilent`（= `get_json(silent=True)`），jsonx 保留数字字面量，`1.0`/`true` 修订号仍按 Python `type(int)` 拒绝；
   falsy 体按 `or {}` 处理，非对象 400。注册无冲突（全路由表测试通过）。
8. **O14 切换清单。** `parity/O14-CHECKLIST.md` 第一项即 A14 §7.1：切换后每台 Agent 一次性显示 changed、需要统一
   全量推送，并说明 proddiff 测试副本格式迁移不能在生产中清掉这次 changed。

## 其他核对（无发现）

- `IsPreparationFailure` 按 Python 类树复推：StrategyReferenceError 及 Dialer/Listener 子类（provider.Error 经
  Unwrap 覆盖）、DeploymentPreparationError/RuleDownloadError、BaseSettings/EasyTier、LinuxGateway/RouterSplit、
  Oxidns Settings/Validation/Reference、RemoteRuleValidationError、MosdnsArchiveError、repo ValueError 类、
  agents.ValueError、`Refusal`、setupflow 损坏会话；ProfileNotFound、CustomConfigYAMLError、传输错误为 500。
- 守卫顺序（Agent 404 → 体 400 → generation 409 → guard 400/409 → 快照 409 → 绑定 409 → OxiDNS 草稿 409 →
  Surge → 服务 400 → restart 400 → 预留）与 Python 一致；Surge `restart:null` 视为 false 与 Python 一致。
- 能力探测超时 (5,5)、上传 (10,120)、final-preview (30,30)；忙重试仅限精确 busy 409。
- HA：无 A18 时 `oxidns_ha.enabled` 或未结束的 HA 日志 → `ErrPairPublisherMissing`（500/generate 400），符合设计。

## 与 C34 的交接（2026-11-18 通知）

C34 负责修 A11 `Previews` 的共享提交检查（O3-6c 后在取得叶锁后、不可中断段前检查 `ctx.Err()`；同一 DATA_DIR 的
多个实例共用 canonical keyed 叶锁）。A14 侧核对：A14 只通过 `previewsFor(m)`（每个 Manager 一个实例）调用
`Save`/`Load`/`RequireCurrent`，以及 `deploy.LoadPreview`；单例替换期间新旧 Manager 会各有一个实例，正是 C34
要修的双实例交错，A14 不另做私有锁。只要 Save/Load 业务 API 与冻结产物语义不变，A14 无需改动；C34 合入后 A14
的 generate/push-by-generation 测试应在合并树上重跑。

## 第 6 节：A20 合入后的待办（本分支 rebase 后完成）

1. 删除 `deploy.Wire` 中的 `routersplit.SetProfileSessions` 注册，只保留 `setupflow.Wire`。
2. 在 server wiring 中注册 `deploy.SetGatewayGuards`，回调用 `host.Bridge{…}.RequireForwardingKept` /
   `RequireGatewayOffKept`。
3. 补测试：两个 guard 都能真正阻止发布；进程中只有一套 reader 注册。

## 验证命令与结果

全部在 192.168.0.64（经 `rt`）执行：

```sh
# 共享窗口 SKIP 复现（远端副本删 skip / 打原型 patch，未提交）
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && sed -i "/t.Skip(\"等 O3-6c/d" internal/deploy/runtime_boundary_test.go && go test -count=1 -run "^TestShutdownAtSQLiteCommitKeepsWholeDocument$" ./internal/deploy; cd .. && patch -p1 < server/parity/progress/a14-shared-commit-window.patch && cd server && go test -count=1 -run "^TestShutdownAtSQLiteCommitKeepsWholeDocument$" ./internal/deploy'
```
```text
--- FAIL: TestShutdownAtSQLiteCommitKeepsWholeDocument (0.29s)
    runtime_boundary_test.go:137: BeforeCommit shutdown returned <nil>
    runtime_boundary_test.go:140: cancelled SQLite transaction committed
== with prototype
ok  	configflow/internal/deploy	0.342s
```

```sh
# M1 修复前
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -count=1 -run "^TestReviewA14" ./internal/agents'
```
```text
--- FAIL: TestReviewA14ManualRollbackWithoutLiveOwnerIsReissued (0.48s)
    review_a14_test.go:82: posts 0 result … {status rolling_back} … {message Rollback submission awaiting Agent confirmation} {pending true}
```

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -l internal/agents; go test -race -count=1 ./internal/agents'
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=6 -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && test -z "$(gofmt -l internal/agents internal/deploy internal/api/agents)" && go vet ./internal/agents/... ./internal/deploy/... ./internal/api/agents/... && go test -race -count=1 -p 3 ./internal/deploy ./internal/api/agents && go test -race -count=1 -run "^(TestContentChangesAgainstFlask|TestPublishHTTPAgainstFlask|TestPublishLegacyProxyAgainstFlask)$" ./internal/api/agents/difftest && go test -race -count=1 -run "^(TestPythonPublishTranscripts|TestFinalResourceRetryUsesContentIdentity)$" ./internal/deploy'
```
```text
ok  	configflow/internal/agents	440.926s
VET-OK
ok  	configflow/internal/deploy	310.686s
ok  	configflow/internal/api/agents	267.957s
ok  	configflow/internal/api/agents/difftest	11.793s
ok  	configflow/internal/deploy	45.625s
```

L1/L2 的「修复前失败」分别以远端撤回 adapter.go 补丁、删除日志行复现（输出见各条）。完整 gate 未在本复核运行，
由协调者在合并树上执行 cf-gate。
