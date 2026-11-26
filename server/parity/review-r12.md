# R12 HR 复核（release plan and staged execution：Inspect / BuildPlan / Execute / Verify / Publish / StartPublish）

复核者：新的 Opus 会话（未参与移植；R12 由 Codex 移植）。分支 `go-opus-review-r12`，从 `go-port-r12@574c3e89` 分出
（基于 R11 复核分支；R10、R11、O3-6c 合入 go 后再 rebase）。只用 loopback fake 和合成数据，未接触生产、真实路由器、
Agent、DNS 或 PVE；未 push，未开子 agent；按 Leaner verification，没有重跑作者的 22 组完整对照，只读一遍，
跑了自己修复的测试，并按风险点抽查。

范围：完整读了 Python `backend/routers/release.py`（413）、`plan.py`（807）、`routes/routers.py` 的检查服务
（`_agent`、`_dns_settings`、`_inspect(_state)`、`_ipv6_host`、`_return_cleanup`、`_return_routing`）和
`router_apply`/`_log_release`/`_capture_sync`、`rule_sync.capture_manual` 的签名比较部分、
`config_repository.save_router_publication`、`agents/manager.py` 的三个回流调用。对照 Go 的 `inspect.go`、`plan*.go`、
`release*.go`、`publish.go`、`agent_return.go`、`return_advisory.go`（抽查）、`repo/profiles.go` 的 JSON 边界、
`route_editor.go` 中 `RouterOperationLock` 的改动、`router.go` 的 `source`，以及 `ctxx`、`lifecycle.Spawn`、
`repo.update` 的取消门禁。设计按 `design/r12-release.md`（§2.8、§3.4、D-R12-3、Clarified in R12）。

## 结论

**0 条 high，3 条 medium（均已修），2 条 low（已修），另有 5 条 low 记录在案（不改代码）。**
未达到「≥ 2 条 high」的回退门槛。分阶段执行、回滚、补偿 GET 确认、指纹、三态 publication、连接字段 CAS、
锁交接的主体实现正确，与 Python 逐分支一致；问题集中在发布收尾（规则同步基准的输入）、D-R12-3 的进入条件，
以及待集成的 AST 白名单补丁本身。

修复提交：`92b5c81c1`（F1、F2、F4、F5 及测试）、本报告所在提交（F3 补丁修正、文档）。

## 发现

### 中

- **F1 — 发布成功后交给规则同步的路由器缺少本次发布的 IPv6 manifest，自动同步基准会一直建立失败（Go 引入，已修）。**
  场景：一次发布改变了 IPv6 Fake-IP 或 IPv6 规则集路由的归属记录（首次启用、换下一跳、撤回）。Python `_capture_sync`
  先用 driver 上的 `ipv6_fakeip_manifest`/`ipv6_routes_manifest` 覆盖路由器记录，再调 `capture_manual`；后者用
  `controls(config, router)` 与刚保存的记录 `controls(current, live_router)` 比较，签名包含 `ipv6_fakeip_state`。
  Go 的 `captureRelease` 直接传入发布前的 `req.Router`，R14 接线后签名必然不同，`capture_manual` 报
  「发布期间设置有变化，请重新手动预览发布后建立基准」，每次涉及 IPv6 归属的发布之后自动同步都停在 waiting。
  证据：`TestR12ReviewCaptureSeesPublishedIPv6Manifest` 修复前 `capture saw the pre-release manifest: null`。
  修复：`captureRouter` 复制路由器（不改调用方对象），对 `Set` 的两个 IPv6 manifest 按 Python 覆盖（nil → null），
  空计划路径同样经过它（此时值等于存储值）。

- **F2 — D-R12-3 的进入条件只看「已证明」的操作：关机打断第一项写入时，失败记录和那一次本地提交被跳过（Go 引入，已修）。**
  维护者裁定是「设备一旦开始写入」，drain 取消后那一次本地 publication 必须完成。`Result.Wrote` 原来只在
  `finished`（操作已证明或带 undo）时置位。关机若落在第一项写操作内部（RouterOS PUT 已发出、回读或补偿被取消；
  R08 策略替换这种多请求操作最容易落在中间），handler 返回取消错误且没有 undo，`Wrote=false`，Publish 走普通路径，
  被取消拦下，**什么都不记录**：重启后 `last_release` 仍显示上一次成功，设备却已部分改动。
  证据：`TestR12ReviewShutdownDuringFirstWriteRecords`（真实 `Runtime.Start/BindRequest/Shutdown`，在第一个 PUT
  之后关机）修复前 `Err:context canceled … Aborted:true Wrote:false`，修订号不变。
  修复：在每次尝试操作（`applyRelease` 或 `ApplyMany`）之前置 `Wrote=true`。不可中断区仍只包住那一次
  `SaveRouterPublicationJSON`；Inspect/BuildPlan、阶段预检读取中关机仍零提交（原 `shutdown_before_write` 用例不变），
  设备 I/O、补偿、读回照常响应取消（新用例断言关机后设备调用数不再增加）。代价：取消恰好落在第一项操作自身的预读时，
  也会多写一条失败记录（manifest 未变，只是修订号 +1），这比漏记安全。

- **F3 — 待集成的 AST 白名单补丁在合并树上让 canonical 测试失败（交接材料问题，已修）。**
  我在临时树上合并了本分支与 `go-o3-6c@426f6edd0`（只有 behaviour-changes.md 文档冲突），应用
  `progress/r12-batch-allowlist.patch` 后，`TestCommitBatchMarkersMatchDesign` 失败：
  `unapproved batch marker repo:method:DeleteProfile:beginCommitBatch`（另外两处同样）、`markers=0`。
  原因：补丁给所有带接收者的函数加 `method:` 前缀，而 O3-6c 的三处 batch 恰好都是 `*Repository` 方法。
  按原补丁集成会直接让协调者的 batch gate 失败。
  修复：`method:` 前缀只用于 routers 包（防止 routers 里的方法冒充获批的 `saveAbortedPublication`），repo 沿用
  O3-6c 原有的函数名。在合并树上验证：四条各恰好一次且都在设计中登记，测试通过；反向对照（在 routers 加一个
  `(*RouterOS).saveAbortedPublication` 方法调用 `ctxx.Uninterruptible`）报
  `unapproved batch marker routers:method:saveAbortedPublication`。补丁已重新生成，并确认能干净应用到
  `go-o3-6c` 的 `batch_paths_test.go`。同一合并树上 R12 的 Runtime、publication、StartPublish 测试（`-race`）通过；
  repo 全包除这条外均通过（修正后该条也通过）。

### 低（已修）

- **F4 — 回流写操作执行前不复核 Agent 现状：共用同一 Agent 的另一台路由器在预览后改了受管规则，会被本次删除或覆盖（Python 同，已修）。**
  回流规则由 Agent 持有，可被多台路由器共用（`_return_cleanup` 的 `shared_with`），没有 Agent 级别的锁。路由器 A
  关闭回流、预览时没有其他路由器在用，于是计划在最后一个阶段移除；在此之前用户给路由器 B 开启回流并发布（另一把 L1/L2），
  A 的清理阶段照样调用 Agent 删除，B 刚建立的回流被撤掉，B 的端口分流/来源策略回包中断。同理，`return_routing_apply`
  的 `previous` 是预览时的规格，执行时若 Agent 已是别的规格，会被覆盖，回滚时又恢复成过时的 `previous`。
  这是本模块里「用户对象只拥有一部分」的那一类（R11 F1 同类）：Agent 的回流对象是多台路由器共享的。
  证据：`TestR12ReviewReturnOpsRequirePreviewedAgentSpec` 修复前 4 个子用例（他人规格、已被删除、新增前已被他人应用、
  替换前已变化）都照常发出 PUT/DELETE。
  修复：前向执行（非补偿）时，`returnPreviousHolds` 要求 Agent 当前受管规格（table_present 时）与计划中的
  `previous` 相同：未知字段逐值比较，`proxy_dst` 按集合比较；不一致时以 Python 已有文案
  `回流资源分配在预览后发生变化，请重新预览后发布` 拒绝，阶段失败并按逆序回滚。回滚中的补偿不做此检查（那时的规格就是
  本次写入的）。检查放在 Python 原有的握手/分配检查之后，原有消息优先。22 组 Python 对照与全部 R12 测试在修复后通过。

- **F5 — `StartPublish` 吞掉 panic，不留日志（Go 引入，已修）。** 内层 `recover` 把 panic 变成 `ErrPanic` 交付，
  `lifecycle.Spawn` 自己的 recover 因此不再触发，日志里什么都没有，违反 AGENTS.md「后台 goroutine 顶层 recover 并通过
  logx 记录」。修复：记录 `主路由 %s 发布任务异常终止，发布结果未记录`（只含路由器 ID，不含 panic 内容，内容可能带凭据）。
  `TestR12ReviewStartPublishPanicIsLogged`（独立子进程）同时验证：只交付一次结果、L1 交付前已释放、L2 已释放、
  日志不含密码。修复前日志为空。

### 低（记录，不改代码）

- **F6 — 发布已全部成功、恰好在保存前关机：记录为「中止」。** `Publish` 在 `work.Err() != nil && Wrote` 时把已 OK 的结果
  改成 Aborted 并走不可中断提交，`last_release.ok=false`，消息为「服务正在停止…请重新预览后发布」。设备与 manifest
  都正确，下一次预览为空，用户只多看到一次失败提示。比不记录好，不改。
- **F7 — 关机发生在写入前时，`PublishOutcome` 的 `Status` 仍是零值 `PublishOK`、同时带 `Err`。** 设计 §2.8 规定
  `Err` 优先；R15 映射时必须先看 `Err`（R15 的检查项）。
- **F8 — 回流 apply 先读 Agent 状态，再校验 spec 的 `safety_version`/`adopt_legacy`（Python 相反）。** 计划只会生成合法 spec，
  只有手工构造的非法计划在 Agent 不可达时报的文字不同。不改。
- **F9 — Agent 回报 `table_present: true` 但缺少 `spec` 键、且为协议 1 时，回读失败文字不同。** Python 报
  「Agent 上的回流网段与计划不一致」，Go 报「Agent 上的回流路由或安全握手与计划不一致」。两边都失败并回滚，只有畸形
  Agent 响应会触发。不改。
- **F10 — iKuai 单个操作开始后不响应关机（O3 2.5 的既定设计）。** `Ikuai.Apply`/`Write` 与批次末尾的 `confirmAdded`
  用 `WithoutCancel`，一个已开始的操作会跑完（每个请求自带 10 s 超时），可能超出取消后约 0.5 s 的退出窗口，这时进程
  先退出、记录丢失（设计 §3.4「进程先死则同 Python」）。R12 自己在操作边界检查取消，RouterOS 侧全部 Detach。列入 L5。

## 重点核对（未发现其他问题）

1. **中途失败、部分成功与回滚。** `finished` 以原始字节匹配批次中第一个未登记的操作（RouterOS `ApplyMany` 的 record
   回调传回同一字节切片，已核对 `routeros_batch.go`）；带 `UndoOf` 的失败在阶段失败前登记、不写 ok 日志；回滚逆序、跳过
   nil/null/{} undo、连续可批量恢复的 route undo 走 `RestoreMany`（结果按条，缺失按未知）、批次本身失败归到 `done[0]` 的
   阶段；`KindRollback` 先写一条回滚失败。F-R12-1：回流 PUT/DELETE 的传输错误（非 `*agents.RemoteError`，经
   `returnAgentError.Unwrap` 判断）保留 undo；补偿时传输错误用一次 GET 确认：删除要求 managed 不再 active/table_present，
   恢复要求 table_present 且规格（含未知字段）一致；读失败或外部改动都不算成功。iKuai 批次的 `Batch(rollback=true)`
   和末尾证明照原样执行。
2. **D-R12-3 的范围。** `ctxx.Uninterruptible` 只出现在 `saveAbortedPublication`，只包住一次 `SaveRouterPublicationJSON`；
   区内的输入（last_release、`Manifests()` 副本）都在内存中算好，没有设备或 Agent I/O。`repo.update` 在 `withLock` 等待
   `.config.lock` 时响应取消、提交后不再返回取消错误，因此「普通提交被取消 → 改走一次本地提交」不会重复提交。
   `Execute` 全程 `lifecycle.Detach`，在阶段前、每个操作/批次前、回读前检查取消；中止时不回滚。真实 Runtime 用例覆盖
   写设备前关机（零提交）、写过设备后关机（恰好一次修订+失败记录+manifest、关机后无设备调用、重启后预览认出归属）、
   请求断开（照常完成并提交），本次补上「第一项写入内部关机」（F2）。
3. **预览与执行一致、TOCTOU。** 执行前在 L2 下重新读配置、整条记录（含未知字段，按原始字节语义比较）与 R15 交来的
   记录比对，重新 Inspect + BuildPlan，空或不等的 fingerprint 一律 409；指纹覆盖 stages（不含 `checked_dhcp_dns`）
   与按 Python 顺序嵌套的 evidence，canonical JSON、UseNumber、排序键，跨进程稳定（作者有子进程用例）。设备侧每个操作
   仍由驱动复证；DHCP DNS 写入前在同一次读取上复核路由与 NAT，RouterOS 首个 op 携带那次读取。Agent 侧的缺口见 F4。
4. **三态 publication 与连接 CAS。** 只有 `Set` 的 manifest 才传；`Set` 且值为空/null/假值 → 删除字段（Python 同）；
   空计划只写 `last_release`；`releaseRouterRecord` 保留存储记录原有的缺省字段，不把 `verify_tls` 等运行时默认值变成
   虚构的连接字段（作者用例 + 我核对了 `fieldsDiffer` 的比较方式）。
5. **锁。** `Publish` 只取 `RouterOperationLock`（L2，写路径 Detach、10 分钟上限）；Inspect/Execute/保存路径不再取 L1；
   repo 提交守卫通过 `lockedRouters` 跳过本路由器。`StartPublish` 在 Spawn 被拒时立即释放 L1；正常时交付顺序为
   driver.Close → L2 → L1 → 交付（F5 用例验证 L2 已释放、L1 交付前已释放）。R14 接线 `CaptureManual` 时不得再取 L1。
6. **原始请求、脱敏、只读。** A01 GET/PUT/DELETE 的原始字节、Bearer、GET/DELETE 无 body（作者 raw 用例）；Agent 错误经
   `diag.FailureText` 去掉 token；`last_release.message` 与发布日志经 `diag.EventMessage` 去掉密码，用户名保留；
   预览零写入（Runtime 用例断言预览后设备写调用为 0）。
7. **只拥有一部分的对象。** R12 自己不持久化任何跨发布的整行证明：`dhcp_dns_update.expected`、`nat_update.expected`
   都在执行前于同一把 L2 下重新读取生成，用户改动 DHCP 网段或 NAT 规则的其他字段只会让指纹失效、要求重新预览，不会
   卡死（R11 F1 那类问题不出现）。持久化的只有 R08–R11 的 manifest，各自的复核已处理。共享的 Agent 回流规则见 F4。

## 需要 L5 实机验证

1. RouterOS CHR：发布进行到第一项写入（尤其是 R08 来源策略替换）时向面板发 SIGTERM，确认重启后 `last_release`
   显示中止、预览能接上（F2）。
2. 两台路由器共用一个 Mihomo Agent：A 关闭回流并发布、B 同时开启回流并发布，确认 A 的清理阶段以「回流资源分配在预览后
   发生变化」失败并回滚，B 的回流保留（F4）。
3. R14 接线后：一次改变 IPv6 Fake-IP 归属的手动发布之后，自动同步基准为 synced（F1）。
4. iKuai 长批次（300+ 路由）中途 SIGTERM，记录进程退出前能否写入记录（F10）。
5. 设计 §6.7 原有各项。

## 验证命令与输出

```sh
# 修复后，全部 R12 测试（含 22 组 Python 对照，无跳过）
rt -- bash -lc 'cd server && go vet ./internal/routers && go test -count=1 -race ./internal/routers -run "TestR12" -timeout 20m'
# .32: ok configflow/internal/routers 20.138s（非 race 复跑 25 个顶层用例 PASS，无 SKIP）

# 反向对照：新测试放到 574c3e896 的代码上
rt -- bash -lc 'cd server && go test -count=1 ./internal/routers -run TestR12Review'
# .32: 4 个测试全部 FAIL（输出见上文各条）

# 合并树（本分支 + go-o3-6c，应用补丁）
rt -- bash -lc 'cd server && go vet ./internal/repo ./internal/routers && go test -count=1 -race ./internal/routers -run "TestR12Runtime|TestR12Review|TestR12Publication|TestR12Legacy|TestR12StartPublish|TestOwnerLifetime|TestMissingJobLock" && go test -count=1 -race ./internal/repo'
# .32: routers ok 5.339s；repo 仅 TestCommitBatchMarkersMatchDesign 失败（F3），修正补丁后该测试 PASS，反向对照 FAIL

# 共用 Execute/Publish 的 R04–R12 定向回归
rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/routers)" && go vet ./internal/routers/... && go test -count=1 -p 4 ./internal/routers -run "TestR12|TestR0[4-9]|TestR1[01]|TestGroup|TestRoute|TestRouterOS|TestOwnerLifetime|TestMissingJobLock|Test.*Publication" -timeout 30m'
# .32: gofmt-clean；ok configflow/internal/routers 140.356s
```

未跑完整 gate、proddiff、全仓 race（按简报由协调者在批次树执行）。

## 提交

- `92b5c81c1` go(routers): R12 review — capture overlay, write-started abort record, Agent spec re-proof, logged owner panic
- 本报告所在提交：修正 `progress/r12-batch-allowlist.patch`（F3）、本报告、PROGRESS R12 状态。
