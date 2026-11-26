# A17 DNS follow 移植交回

状态：**verified：Opus HR 已完成（0 high，2 medium / 3 low 已修），batch-5 合并树集成复验通过**。当前分支 `go-a17-integrate`，集成实现与验收提交 `7f0e9b173`，由 `go-opus-review-a17@f5180a271` 新建，执行 `git rebase --onto 0d43e3f15 b6ce4ac29`，仅回放 10 个 A17 提交，不重复带入旧 A14。行为差异文档的追加冲突保留双方；server/Register/routes 自动合并后逐段核对。原移植分支 `go-port-a17`（主体 `8dec20f39`、验收 `1560b84b0`）的运行记录保留在下文。已具备进入 batch-6 的条件，完整门禁由协调者安排。

## 实现与边界

完整阅读 Python `backend/agents/dns_follow.py`、路由 162–191 行和 `backend/test_dns_follow.py`（70 个函数/参数化定义）；对照 A14 复核/HANDOFF、C26、O3-6 locks、A16–A18 设计及 WORKFLOW Leaner verification。

`internal/agents/dnsfollow` 提供 `Options/Configure/Disable/Retry/Tick`、`ForManager/Wire/Register`。候选发布及 profile CAS 前核对绑定、来源和目标 profile、连接信息、基准、资源摘要与成功发布归属；草稿已提交后的签名重绑只核对 binding id，保留 Opus M2 修复的幂等边界。A14 提供真正的 PrepareContent/Publish、reservation、receipt、Agent 协议和缓存提交；C26 提供文档/生成检查。无自制 YAML/JSON parser，无新依赖。taskstatus 使用真实 store 的默认时钟，不替换生产参数。只有 `adapter.go` 接触旧 registry/repo 的 jsonx API；业务类型使用 model 和 deploy Source。

四条路由全部通过 panel auth：GET/PUT/DELETE `/api/agents/<agent_id>/dns-follow`，POST `/api/agents/<agent_id>/dns-follow/retry`。HEAD 走只读 options；PUT 使用 BindSilent，与 Flask silent JSON 一致；retry 成功 202，拒绝 409。options 不返回私有 job，只投影来源公共字段。绑定索引接受整数且不接受 bool/null/浮点；保存数据里的 R5 错类型回退也不能成为 upstream 0 的所有权证据。

已通过 Paseo 与 A16 对齐：A16 独占 RuleSyncRow/PinnedWithheld、设备 snapshot 与 `agents.HAEnabled/HAPair`，A17 不依赖私有设备快照。不新增重复 HA 导出；A17 独立 `api/agents/dnsfollow.go`，server/routes 合并时保留双方接线。A17 注册 DNSFollowRow 和 dns_follow runner；没有伪造空 RuleSyncRow 来骗过 C07 readiness。`Wire(m)` 是 manager 初始化入口；无参数 `Register()` 是 server 组合入口。

## 并发、恢复和取消

L1 try-lock 按 repository canonical DataDir 共享，跨 Manager/Service 排他，忙冲突在参数校验之前返回；不增加 flock/锁文件。UpdateAgents 和 profile 事务 callback 不做网络；来源/目标网络请求都走 Manager 的实际 HTTP client 和生产超时。boot 首轮零延迟，之后 5 秒；IdleRounds 600 秒复查，稳定结果不重写时间、事件或 taskstatus。

child ID 由 binding/source/attempt 确定；预留后保存私有 job、提交证据，再上传同一 prepared bundle。丢响应保留 submitted 任务，重启只 poll；rollback_failed 非终态，禁止重复 retry/upload；仅 failed/rolled_back/succeeded 为终态。成功后 profile 做 settings CAS，期间新草稿保留并报告 draft_synced=false。finalize 在 profile L3 事务中再读归属，避免核验与写入之间让旧 child 覆盖新归属；重复 finalize 以 json.RawMessage 比较已提交候选，保留 Opus M1 的重启恢复修复。

所有写 owner 通过 deploy.RunDetached；已绑定请求断连会完成工作，Runtime 关闭则拒绝新工作。真实 Runtime 用例在 DNS 响应/Agent 接受上传后停机，比较完整 DB 文档与非 SQLite 辅助文件，确认晚到响应不提交状态/job/profile/taskstatus；已接受的上传保留不确定证据以待下次恢复。无裸后台 goroutine，ForManager 在 Manager.Close 时移除 pool entry；回环测试的 goroutine 只用于触发真实并发。

O3-6c 集成：`recordOutcome` 已改为 `notifications.TaskStatus(...).RecordContext(owner ctx, ...)`，将 owner 的关机取消传到共享 leaf lock/原子替换。合并树同时适配 `tasks.History` 的错误返回（错误不吞掉）及 `redact.SanitizeCredentials` 的 RawMessage 返回（在既有 adapter 边界转换回 task row）。没有修改共享驱动、生产超时或比较器。

新增 `TestTaskStatusRetainsOwnerLifetimeAfterPrecheck`：真实 Runtime Start/BindRequest，暂停在 owner 已读到 nil Err、但尚未进入 taskstatus leaf 的位置。关机后释放时，要求 context.Canceled 且完整 DB/辅助文件不变；浏览器断连正控要求 event 成功并且 run_count 只增一次。暂停只控制调度，保留真实 context.Value/owner lifetime，不缩短生产 I/O 参数。切换前的旧 `Record` **负控明确失败**（shutdown 子例返回 `<nil>`，2.049s），断连子例通过；避免只测试入口处早退而漏掉内部窗口。另补 `TestTaskRowPropagatesInvalidHistory` 验证 shared History 错误确实上抛。

## 脱敏与行为差异

状态、事件、options、task runner/HTTP error 使用共享 redact/FailureText，包含所有当前 Agent token 和私有 proof 中的凭据；日志 label 也用同一组 secrets。读取凭据失败时只给通用错误，不回传未经脱敏的原始 error。private job 和原始凭据保留在磁盘，不出现在公共视图；更新只改 A17 的三个成员，其他 Agent 字段和已知成员中的未来字段不丢。

- **D-A17-1 已裁定 B**：原生 `deploy.CanonicalDigest/SettingsDigest`；不复刻 CPython JSON 序列化。切换时所有既有 DNS binding 需重新选择一次，按设计列入 O12/O14。YAML 字节排版不追平，配置结构和实际文件语义一致。
- **R05-3**：空 identity/proof 不授权提交；错类型/null 索引不按 0 处理。损坏记录拒绝并保留现场。
- finalize 增加同事务归属检查，封住 Python 先核验再写的竞态；并发 draft 编辑仍保持 Python 的成功带警告行为。扩展凭据脱敏沿用 A24 安全合同。
- 不改比较器来抹去业务差异。差分只归一化 A17 具名 binding UUID、entry digest、确定性 child ID 和合法时间；任意其他 hex 字符串与地址仍精确比较。负控改变地址、无关 64 位字符串均能检出。

## 测试映射（按 Leaner verification 分组）

真实差分 `difftest.TestPythonRoutesAndArtifacts`：7 个隔离合成场景，实际 Flask、ProfileRepository、AgentManager、A14 prepare/publish，Agent 为同一 Go 协议 fake 的两个独立回环实例，真实 HTTP，未替换 Python 业务函数，仅固定 `_now`。正常、地址未变、未发布草稿、监听停用、peer 错误、null index、非对象输入。对照所有 4 条路由的结果、HEAD/auth、binding/state/events、草稿、实际归档的每个 files 成员与上传次数。两边摘要由各自原生算法验证，不比较 YAML/JSON 排版。Python HTTP guard 禁止回环之外的网络。

| Python test_dns_follow.py 行/行为组 | Go 证据 |
|---|---|
| 149–195 options、地址建议；494 candidates | live diff 初始 options、auth、HEAD；BindingValidation、NetworkAndHASelection |
| 196–259 发布触发、只改一项、幂等、rule-sync 基准 | ConfirmedFollowChangesOnlySelectedUpstream；live diff success 归档；沿用 A14 applied_content 基准 |
| 260–296 丢响应重启、失败重试 | LostAcceptedReplyRestartAndRollbackFailedNeverUploadTwice；FailedChildRetryCreatesOneDistinctAttempt |
| 297–420 草稿/生成/规则漂移、设备基准、并发新草稿 | DraftDriftAndConcurrentEditPreserved、OwnershipChangedDuringVerificationRejectsChild；live diff drift；生成/资源分支复用 A14/C26 的真实 prepare/digests |
| 445–617 auto/network/HA、监听、绑定变化、pinned/parked、禁用 | NetworkAndHASelection、BindingValidationIsStrictAndDoesNotWrite、RetryNullSourceAndPrivateProjection；live diff wrong-peer/disabled-listener；C26 pinned/source 行为 |
| 618–998 noop 不准备、不下载、证据及归属 | UnchangedVerifiedAddressSkipsPreparation；OwnershipChangedDuringVerificationRejectsChild；PrepareUsesProductionOptionsAndRechecksOwnership；live diff unchanged |
| 999–1080 凭据、private job、auth、rollback_failed、注册保留 | OutcomeRedactsCredentialsAndPreservesUnknownFields、RetryNullSourceAndPrivateProjection、LostAcceptedReplyRestartAndRollbackFailedNeverUploadTwice；live diff auth；api/agents 的 EveryRouteRequiresPanelAuthAndHEADIsReadOnly 覆盖 GET/HEAD/PUT/DELETE/POST 未登录和 Agent token 拒绝 |
| 1081–1289 来源/目标变化、资源和 finalization 竞争 | PrepareUsesProductionOptionsAndRechecksOwnership；BindingChangedAfterUploadCannotFinalizeDraft；DraftDriftAndConcurrentEditPreserved；A02/A14 共享发布操作 guards |
| 1290–1341 跨 worker lock、loop/init 一次 | BusyOwnerAcrossManagersPrecedesValidation；C07RunnerAndManagerOwnerLifecycle；named lifecycle periodic 去重合同 |
| 1342–1752 空闲、broken 修复、pending polling、去重、重查 | SettledAndBrokenRoundsRestWithoutWrites；StateDedupeIncludesFieldsButIgnoresTime；LostAcceptedReplyRestartAndRollbackFailedNeverUploadTwice；真实 shared IdleRounds |
| Flask 之外 O3 写 owner 合同 | RuntimeStoppedRejectsEveryFollowWrite；DisconnectedBoundRequestFinishesBindingAndFollow；ShutdownDuringFollowIOLeavesNoLateCommit；ShutdownBetweenRetryCommitsRetainsCompleteWaitingState |

未逐项重复 Python 参数笛卡尔积、CPython/PyYAML 内部异常文本、既有共享层测试；本轮不跑模块 mutation suite 或完整 gate。A17 不负责 A16 snapshot/投影和 A18 HA pair，HA pair 最终执行仍依赖 A18。


## 自查修复

- Medium：旧/导入 Agent 的 task row 名称可能含另一 Agent 的 token，原始 row 不使用所有 Agent 的凭据集合清理。负控实际检出 `synthetic-source-token`，已在 taskRow 使用共享 CollectCredentials/SanitizeCredentials；state/events/options/error/log label 同集合检查，未知非敏感字段和磁盘私有 job 保留。回归 `TestOutcomeRedactsCredentialsAndPreservesUnknownFields`。
- Medium：JSON null 索引/标签不能落到 Go 零值；存储中的 R5 错类型/null 索引也不能成为 upstream 0 证据。回归 BindingValidation 与 StoredIndexFallback；合法整数 0 正控保留。
- Low：retry 在尚无来源发布时需显式写入 source_deployment_id:null，避免遗漏字段；删除中的 target 不再进入 nil 解引用。回归 RetryNullSourceAndPrivateProjection、CancelledContextDoesNotEnterOwner。null 状态解码还曾清掉预设 waiting，已把状态赋值放到解码后；真实 Runtime 在 reset 提交后、queued event 提交前停机，断言中间记录完整 waiting/attempt/retry/null，且无晚到 taskstatus 或辅助文件写入（ShutdownBetweenRetryCommitsRetainsCompleteWaitingState）。
- 测试夹具修复没有修改生产 timeout/PrepareOptions、比较器业务字段或 repo 校验；没有以替身返回 ready 来绕过真实 A14。

## 定向结果

早期用 rt 在 `.64`，收到协调者计时保留要求后切到 `.32`；最终命令固定 `.32`。所有用例与 fixture 只含合成数据/回环设备，无生产访问；不 push，不开子 agent。

- 初轮 `go test -race -count=1 ./internal/agents/dnsfollow` 两个正常路径通过（51.915s）。
- 组合编译 `go test -count=1 -run '^$' ./internal/agents/dnsfollow/...` 通过。
- `CONFIGFLOW_DIFF=1 go test -race -count=1 -timeout=12m ./internal/agents/dnsfollow/...`：live difftest **ok 88.685s**；parent 回归暴露两项夹具错误（规则使用 type 而不是 itemType；fake 把已失败 child 当作当前 active），修正后 .32 复验通过，见下面最终结果。Runtime 各边界在该轮均执行通过。
- 策略 pin fixture 曾连续被真实 repo 校验挡住，已按 AGENT-BRIEF 发送 escalation packet，协调者要求改为使用真实 Python helper。现通过 `testdata/pin_reference.py` 在 .32 的真实 ProfileRepository 调用 `backend.test_dns_follow.pin_rule` 生成 `testdata/pinned.json`，不再手写行或 action 字段。Go 只把生成记录接到既有默认 profile，保留原规则引用；live diff 再运行同一 helper 验证 fixture 未过期。
- `.32`：`go test -race -count=1 -run 'TestBindingValidation|TestOutcomeRedacts' ./internal/agents/dnsfollow` **ok 29.074s**。
- `.64` 的收尾命令收到指令后中止（143），不算通过；其中 vet 已先完成，最终仍在 .32 重跑。
- 最终 `.32`，gofmt 无输出、`go vet ./...` 退出 0：

  ```sh
  RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; go test -race -p 4 -count=1 -timeout=12m ./internal/agents/dnsfollow/... ./internal/api/agents ./internal/server'
  ```

  `ok configflow/internal/agents/dnsfollow 440.750s`；`ok .../dnsfollow/difftest 151.565s`；`ok .../api/agents 358.929s`；`ok .../server 1.836s`。包含 22 个本包测试、7 个 live 场景及归一化负控、真实 Python pin fixture 新鲜度检查，API/server 接线回归。
- 随后只修改 retry 的中间状态赋值顺序、取消后日志检查并新增对应 Runtime/逐路由 auth 用例，按 Leaner verification 只复验重叠路径；最终再次 gofmt 无输出、全仓 vet 退出 0：

  ```sh
  RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; go vet ./...; go test -race -count=1 -run "TestShutdownBetweenRetry|TestRetryNull|TestOutcomeRedacts" ./internal/agents/dnsfollow; go test -race -count=1 -run TestDNSFollowEveryRoute ./internal/api/agents; go test -race -count=1 -run "TestPythonRoutesAndArtifacts/(drift|disabled-listener)$" ./internal/agents/dnsfollow/difftest'
  ```

  依次 `ok .../dnsfollow 31.931s`、`ok .../api/agents 1.465s`、`ok .../dnsfollow/difftest 35.706s`。当前本包共 23 个测试函数（另含参数化子例），全为合成数据。新 Runtime 用例最初过度限定了取消后的纯时钟读取次数，已删除该实现细节断言；仍严格检查真实取消、提交数和 durable waiting 状态。

提交：`8dec20f39` 为主体；后续边界修复、差分、真实 Runtime 和交回说明在本报告所在的验收提交。工作树最终保持干净，未 push。

## batch-5 合并树集成（通过）

固定 `.32`，本轮不跑完整门禁。`recordOutcome` 传 owner ctx、History 错误与 RawMessage consumer 适配及测试控制修复在本次集成提交。

集成首轮：gofmt、全仓 vet 通过；live difftest `ok 189.637s`。本包 race `1084.796s` 未全绿：`TestShutdownDuringFollowIOLeavesNoLateCommit//api/deployments` 的 10s 到达点观察期限先到，失败路径没有释放测试屏障，遗留 Runtime owner，随后 5 组用例连带报 stopping/running。已修测试控制：观察期限为 60s、所有退出路径释放屏障并等待 owner，owner 提前结束直接报告；HTTP 生产连接/读取与上传 timeout 不改。本包在干净进程重跑，已通过的 live diff 不重复。Opus 的 M1/M2/L1、其他 Runtime/脱敏/lock-order 回归都包含在本包 race 中；live difftest 在含 A14/O3-6c 的同一树执行。待通过后交协调者进入 batch-6，实机 L3 项仍按独立复核报告保留。


最终命令（全部前台，`RT_HOST=sunbroke@192.168.0.32`）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/agents/dnsfollow internal/api/agents/dnsfollow*)"; go vet ./...; go test -race -p 4 -count=1 -timeout=20m ./internal/agents/dnsfollow/...'
```

该轮 gofmt / 全仓 vet 通过，live difftest `ok 189.637s`；本包的测试控制失败如上，不能将该轮包 race 记为通过。修正后只重跑本包与尚未执行的接线/共享检查，不重复已通过的 live suite：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/agents/dnsfollow)"; go vet ./internal/agents/dnsfollow/...; go test -race -v -count=1 -timeout=30m ./internal/agents/dnsfollow; go test -race -count=1 -run TestDNSFollow ./internal/api/agents; go test -race -count=1 -run TestO36cStatus ./internal/taskstatus; go test -count=1 -run "^$" ./internal/server'
```

实际将 `-v` race 输出重定向到测试服务器 `$TMPDIR/a17-integrate-race.log` 并在成功后打印尾部，仍为前台运行；上面保留等价的直接命令。最终：

- `ok configflow/internal/agents/dnsfollow 1121.876s`（全部作者及 Opus 回归、TestMain 锁序、关机/断连）
- `TestTaskStatusRetainsOwnerLifetimeAfterPrecheck` 的 shutdown/disconnect 两子例 **PASS 1.99s**；坏 History 错误上抛 **PASS**
- `TestShutdownDuringFollowIOLeavesNoLateCommit` **PASS 51.57s**，后续用例没有 Runtime 污染
- `ok configflow/internal/api/agents 1.499s`
- `ok configflow/internal/taskstatus 1.253s`（共享 leaf lock 等待、写中关机及断连正控）
- `ok configflow/internal/server 0.008s [no tests to run]`

本轮无新功能差异、无未修复发现；O3-6c 在 A17 的 taskstatus 入口衔接窗口已闭合。`git range-diff` 核对 10 个复核后提交：9 个补丁完全相同，另 1 个只因 behaviour-changes 文档追加合并发生上下文差异。没有重复带入旧 A14，不改共享驱动，不 push，不访问生产，不开子 agent。A17 已在 PROGRESS 标 verified；batch-6 完整门禁与设计 L3 实机项仍由协调者安排。
