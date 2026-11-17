# O3-6c — 共享提交点响应关机

状态：实现完成；最终 gate 排队超过15分钟按约定终止，交回协调者补完整门禁。分支 go-o3-6c，从 go@f90a156e 建立，现已变基到 C33 合入后的 go@d43cca6e4（CFG-08工具/证据更新）；不派子 agent、不 push、本机不跑测试。
设计裁定见 o3-concurrency.md 的 Clarified in O3-6c。N8-5a 的 24d62bfc 已接受接口与合入顺序，
在此期间不修改 update/writeLocked/commitStore/files.go。

## 输入与已批准边界

- 原型：go-port-a14:server/parity/progress/a14-shared-commit-window.patch。
  以 Store.CommitContext、commitStore(ctx, ...) 和文件提交前检查为起点，适配 6a 的进程内锁。
- 报告：当前 review-c29.md；go-opus-review-a14:review-a14.md 与 a14-shared-commit-window.md；
  go-review-c33:C33-HANDOFF.md；go-port-c06-c11:progress/c06.md 与 design/c06-http-settings.md。
  95eb529e 是 C06 agent ID，不是 git ref，协调者确认使用 go-port-c06-c11。
- 普通 repo 事务不再在拿锁后自动屏蔽取消。BeforeCommit 后紧邻 COMMIT 检查工作 ctx；
  SQLite 自身保证回滚原子性。普通文件在主 rename 前检查并清临时文件。
- 多步审计发现 DeleteProfile（目录移动/导出+DB）、ImportAll/ResetAll（备份+DB）、
  CreateRuleLibraryItem（新缓存+DB）。仅这三处允许 callback 在首个文件副作用前检查 ctx
  并标记 batch，之后最终 DB 完整完成；用 AST 限定标记位置。显式 provider 批处理语义不动。
- 主文件+.bak 也是多步：主 rename 前检查，成功后不因关机中断原有 best-effort 备份。
- WriteSharedTextIfChanged 共用 repo 叶锁。主文件和 .bak 都相同才 touch，Chtimes 前检查；
  否则 replace。删除 rulefetch 锁外 touch，保持 mtime 作为缓存新鲜度的语义。

## 窗口与验收表（commit / regression / red-green 证据）

| 来源/窗口 | 修复提交 | 真实 Runtime 回归 | 状态 |
|---|---|---|---|
| A14/C29/C33/C06：普通 repo 锁等待、回调/序列化后最终 COMMIT | 9fe8b7400 | TestO36cRepoBeforeCommitRuntimeBoundary / TestO36cRepoLockWaitRuntimeBoundary | 定向红→绿 |
| C29/C33/C06：规则缓存 touch/replace，普通文件 rename | c438b8f42 | TestO36cAtomicFileRuntimeBoundary / TestO36cRuleTouchLockRuntimeBoundary / TestO36cRuleTouchAfterShutdown / TestO36cFileOwnerLockRuntimeBoundary | 定向红→绿 |
| C29/C33/C06：taskstatus Record/Forget 锁内提交 | b9b0436a3 | TestO36cStatusLockWaitRuntimeBoundary / TestO36cStatusShutdownDuringRecord | 定向红→绿；C33 adapter由5cefd0b2e接线 |
| C29/C06：MosDNS archive 下游 I/O/cache/status 取消与产物交付 | 4152e375b | TestO36cArchiveClosedRuntime / TestO36cArchiveShutdownAfterFetch / TestO36cArchiveRuntimeIOBoundary | 定向红→绿 |
| 多步例外：DeleteProfile、ImportAll/ResetAll、CreateRuleLibraryItem | 9fe8b7400 / 887b1c618 | TestO36cExplicitBatchRuntimeBoundaries / TestCommitBatchMarkersMatchDesign | race3绿，保留批处理语义的控制组 |
| 主 rename 后关机：成功返回且备份完整 | c438b8f42 | TestO36cAtomicFileRuntimeBoundary/shutdown-after-primary | race3绿，保留已提交成功的控制组 |
| C33 F7/F8：持久marker后重开inline交付、下一次编辑恢复及Forget接线 | 5cefd0b2e | TestShutdownBetweenDBCommitAndCacheRenameIsFailClosed / TestO36cForgetStatusWhileWaiting | 正式合并树race3通过；Forget旧入口红→绿 |

archive.go 在 O3-6a 已改用 lifecycle.Detach；本步进一步关闭共享提交和晚到产物窗口，并用真实 Runtime 端到端回归验证。
A14 当前未合入，保留其显式 SKIP；若晚于本任务合入，由 A14 收尾移除 skip 并重跑原回归。

## 验证计划

先在 .64 记录修复前失败，再记录修复后通过；只用合成库、回环 fake。覆盖持锁等待、
BeforeCommit、文件准备/rename、mtime、完整 DB/文件/revision，及请求断开仍完成。
每个可编译步骤明确路径提交。最终通过 server/scripts/gate.sh，.64 使用 -p 6；
排队超过 15 分钟终止排队并交回已有结果，完整门禁由协调者重跑。

## SQLite 核心第一步

已按 A14 原型接入 Store.CommitContext / commitStore(ctx, ...)，普通 update 不再自动屏蔽取消。
增加 beginCommitBatch 请求内标记，三个已批准 callback 在首个文件副作用前检查并打标；
普通提交保留工作 ctx，已标记路径的最终 DB 提交才显式不可中断。旧 Store.Commit 保留兼容入口。
AST TestCommitBatchMarkersMatchDesign 限定三处 marker 调用；多步路径端到端测试见下文。

真实 Runtime 回归位于 repo/shutdown_commit_test.go（外部 repo_test，避免 lifecycle→repo 测试循环）：
TestO36cRepoBeforeCommitRuntimeBoundary、TestO36cRepoLockWaitRuntimeBoundary，均有 shutdown 与
仅 disconnect 对照，检查完整 SQLite Fingerprint（含 revision）和非 DB 文件清单。

- 修复前 .64：`go test -count=1 -run '^TestO36cRepo' ./internal/repo/` 退出1，0.881s；
  BeforeCommit shutdown returned <nil> / cancelled SQLite transaction committed or changed files/revisions。
- 修复后 .64：`go test -race -count=3 -run '^(TestO36cRepo|TestOwnerLifetimeAndLocalBatch)' ./internal/repo/`
  退出0，5.593s；随后 repo/store vet 通过。
- AST 与已有显式 Uninterruptible 契约 race count1 通过1.515s，repo/store vet通过。
  该次 anchored run 只选择 AST 和既有 owner 测试；不冒称它重复运行了前缀 TestO36cRepo。

## taskstatus Context 入口

新增 RecordContext/ForgetContext，入口用 ctxx.Detach 保留工作父级；锁外及锁内检查，最终
临时文件 close 后、rename 前再次检查。失败不触发 OnRecord，临时文件清理；旧入口以 Background
转调，兼容旧测试/调用。迁移当前生产调用：refresh、metrics、discovery、webdav、subscriptions。
当前分支已迁移上述生产调用；C33 adapter 的 ForgetContext 也已在5cefd0b2e接线。

- 红：先加仅转调旧 Record/Forget 的兼容 Context 入口来观察原持久化行为（未提交占位实现）；
  .64 `go test -count=1 -run '^TestO36cStatus' ./internal/taskstatus/` 退出1、0.283s。
  Record/Forget 在真实 Runtime Shutdown 后取得叶锁仍提交，Record 的 Now 回调关机后也提交。
- 绿：.64 `go test -race -count=3 ./internal/taskstatus/` 退出0、8.405s；上述六组调用方和
  taskstatus vet 通过。新回归 TestO36cStatusLockWaitRuntimeBoundary 保留实际 sync.Mutex 等待，
  shutdown 零落盘/零通知，disconnect 完成；TestO36cStatusShutdownDuringRecord 验证中途取消及无遗留临时文件。

## 文件 replace 与规则缓存 touch

普通 shared/profile/generated 写入改传工作 ctx 到 writeAtomicContext/writeJSONContext；
最后检查在主 rename 前，失败删除临时文件。主 rename 成功后继续原有 best-effort .bak，
不再返回取消。仅把真实 stage/rename 作为私有实现的每调用参数，供 _test 导出桥接；无全局 hook。

新增 WriteSharedTextIfChanged，与 WriteSharedText 共用 owner/叶锁；将旧 rules.TouchUnchangedCache
移动为 repo 锁内私有实现并添加最终 Chtimes 检查，rulefetch 只调新 owner，旧公开 helper/旧测试已移除。
旧测试的场景由 TestSharedTextIfChangedFreshness 和现有 rulefetch.TestUnchangedRefreshOnlyTouches
接住：主+.bak 全匹配才 touch（inode、权限、backup mtime 保留），否则 replace，mtime 前进。

- 红：.64 TestO36cAtomicFileRuntimeBoundary / shutdown-after-stage 复现临时文件完成后关机仍
  写入新主文件；TestO36cRuleTouchLockRuntimeBoundary 复现锁外 touch；整组退出1，1.072s。
  为精确 stage/rename 注入先做了等价旧逻辑的参数抽取，未改变原提交行为。
- 红：TestO36cRuleTouchAfterShutdown，真实 Runtime 已停止后旧函数仍 touch，退出1，0.156s。
- 绿：.64 文件/mtime 组 race count3 通过11.952s；rulefetch 全包 race1通过15.876s；
  rules 全包 race1通过6.301s，repo/rulefetch/rules vet通过。
- 真实 Runtime 还覆盖5类普通文件 owner的叶锁等待、shutdown不写、disconnect完成；
  TestO36cAtomicFileRuntimeBoundary 的 shutdown-after-primary 保留新主+旧内容backup、返回成功，
  stage前取消不改主/backup且不留tmp。

## 显式多步批处理的完整性

TestO36cExplicitBatchRuntimeBoundaries 对 delete/import/reset/create-rule 分别验证三个边界：
实际 L3 锁等待期间关机（marker前）DB/文件/revision全不变；BeforeCommit期间关机（marker后）
一次完整DB提交（generation恰好+1），目录移动+导出、旧DB备份、新缓存分别与最终DB一致；
仅请求断开也完整完成。使用真实默认Runtime，无缩短生产参数或替代取消实现。
.64 `go test -race -count=3 -run '^(TestO36cExplicitBatchRuntimeBoundaries|TestCommitBatchMarkersMatchDesign)$' ./internal/repo/`
退出0，9.904s；repo/store vet通过。这些是保留旧原子批处理语义的控制组，不冒称原实现对此失败。
普通BeforeCommit的红/绿回归与marker后成功回归共同区分两种契约。
审计其余 repo.update callback：通常只改候选JSON；C33 guard-only callback在核验后执行文件动作并中止未变候选（见下文可重放协议）；ImportClientProfile 的 build/各 Updater 按契约
返回候选数据。ConfigurationCommitted 在成功DB提交和L3释放后才通知备份owner，不属于事务回滚域。

## C33 合入衔接补充

已读 go-review-c33:review-c33.md 的 F7/F8（协调者提示 tip0ed0be17）。C33 stage→DB→
L3核验来源→rename/Sync/清marker 是可重放协议；DB后关机允许留下新声明+持久marker，
读侧fail-closed只交付当前DB inline，不能复活旧cache，因此不加入三处不可中断白名单。
已在C33合入后的正式树补重启/下一次编辑恢复回归，保留F7核验与F8外层owner信号，不新增同动作重复检查；
同时替换其adapter的ForgetContext。C33还新增了rulefetch inline迟到写守卫，rebase冲突必须保留。
C33报告另列C37/A24自身DB后首次补cache的崩溃协议属各owner后续，不冒称本次共享取消已解决它们整条原子性。

## MosDNS 归档 owner

archive.go 入口早已由6a换成Detach，本步补取消闸门：入口、source处理、调度/等待、最终ZIP交付。
HTTP下载保留真实生产3s/10s参数；取消直接返回context错误，不再包装成普通ArchiveError。
关闭Runtime不交付ZIP；仅客户端断开仍完成真实HTTP、缓存、status与ZIP。既有8 worker上限和
首次失败不等待其他下载的行为不变，仍有原测试验证；没有残留WithoutCancel。

- 最初夹具误把 readZip 的 [][2]string 当map，编译失败已修正，不计为红证据。
- 红：.64 `go test -count=1 -run '^TestO36cArchive' ./internal/mosdns/` 退出1、0.593s：
  closed Runtime仍交付172字节ZIP，fetch后关机仍交付329字节ZIP，真实HTTP取消变成普通下载错误。
- 绿：.64 Archive Runtime + 现有并发上限/首次失败行为 race count3 通过7.718s；
  mosdns/oxidns/api-oxidns vet通过。三个新测试：TestO36cArchiveClosedRuntime、
  TestO36cArchiveShutdownAfterFetch、TestO36cArchiveRuntimeIOBoundary；最后一条用真实回环HTTP，
  验证取消在生产read timeout之前到达I/O、完整DB/files/revision不变，disconnect则生成cache/status和ZIP。

## 当前检查点

正式分支产品/测试检查点5cefd0b2e，base d43cca6e4；C33产品代码来自已合入的go。
A14原SKIP仍等其合入后移除。N8-5a已避开提交核心，
N8-7（097eba4e）也确认会等RecordContext/ForgetContext合入才迁移taskstatus，完成后需通知两者。

### 完整相关包及正常请求差分（base a08c4cc27，.64）

前台session54143已退出0：12个完整受影响包race全部通过。
`go test -race -p=6 -count=1 -timeout=30m ./internal/repo/ ./internal/store/ ./internal/taskstatus/ ./internal/rulefetch/ ./internal/rules/ ./internal/mosdns/ ./internal/refresh/ ./internal/agents/metrics/ ./internal/discovery/ ./internal/webdav/ ./internal/api/subscriptions/ ./internal/api/oxidns/`
日志 `/home/sunbroke/.cache/rt-tmp/o36c-targeted/race.log`：repo241.518s、store20.152s、
taskstatus4.977s、rulefetch31.618s、rules11.310s、mosdns928.975s、refresh210.307s、
metrics16.756s、discovery800.221s、webdav8.891s、subscriptions6.025s、api/oxidns3.039s。

随后gofmt全目录无输出、全仓vet、configflow_proddiff/configflow_difftest全仓build及两种tag的
lifecycle测试通过（0.182s/0.185s）。正常请求live race：
`CONFIGFLOW_DIFF=1 go test -race -p=6 -count=1 -timeout=20m ./internal/mosdns/difftest/ ./internal/refresh/difftest/ ./internal/api/subscriptions/difftest/ ./internal/api/oxidns/difftest/`
全部通过17.886s /17.519s /182.720s /37.785s。

### C33临时集成验证（不是正式合入）

协调者明确允许临时树定向race、不允许临时树fullgate。独立worktree
`/tmp/configflow-o36c-c33-check`，分支go-o36c-c33-check，合并检查点aec8712ac、后续ba796e47e。
合并时保留C33 writeCurrentInline的声明/marker守卫，其write闭包改调WriteSharedTextIfChanged；
严禁先无条件写入再进guard。F8未加新的重复检查，原有owner视图在6c上继续通过。

新增Forget锁等待回归在旧Store.Forget调用上真实红：race1 1.893s、关机后仍删除/返回nil；
改为ForgetContext(ctx,...)后三轮race通过：rulelibrary7.133s、proxygroups3.225s。
扩展原TestShutdownBetweenDBCommitAndCacheRenameIsFailClosed：新repo实例+新Runtime三种内容出口
只交付已提交inline、旧cache不改、marker保留；下一次SaveDraft修复cache并清marker/tmp。
F2/F7/崩溃恢复控制race1通过8.088s，相关vet通过。临时后续ba796e47e仅包含两个测试改动及
ForgetContext的单行接线，现已在正式树cherry-pick成5cefd0b2e，并重新验证（见下文）。

最终全仓gate待开始；C33合入后的变基和定向验证已完成。每台机器最多1个agent gate，排队15分钟上限不变。

## SQLite内部锁的额外控制

新增TestO36cSQLiteLockRuntimeBoundary，直接在真实SQLite事务的BeforeCommit持写锁，
另一个Store.CommitContext等待BEGIN IMMEDIATE，区分repo L3与SQLite内部锁。
Runtime关机后放行holder回滚，候选不提交（Fingerprint/文件不变）；仅请求断开则metadata提交。
这条是现有修复的额外控制，不冒称新的原实现红证据；.64 race count3通过4.414s，repo/store vet通过。

## CFG-09/11 联验约束（不冒称本任务修复）

已回复两位owner：CFG-11只在C32 adapter保留未变顶层raw，6c不改逐字段diff/no-op/revision合同；
CFG-09由C14/C28分类层处理，Context持久化保持native错误不归一。补充
TestRecordContextPreservesNativeStorageErrors，以真实活跃Runtime+真实文件系统制造rename目标目录
和不可建目录，验证errors.As穿透*os.LinkError/*os.PathError、错误不误判为取消、tmp清理。
.64 race count3通过1.153s、taskstatus vet通过。CFG-09路由500/400与CFG-11字段revision仍由其owner联验。

## C33 正式合入后的复核（base9c88c4390，.64）

正式分支只在rulefetch/cache.go发生冲突：保留C33的writeCurrentInline声明/marker守卫，
其write闭包调用WriteSharedTextIfChanged；未在守卫之前写文件。5cefd0b2e带入已验证的
ForgetContext接线、status锁等待测试和重启恢复扩展；未从临时分支混入C33产品历史。

前台session37944退出0，gofmt全目录无输出、全仓vet通过：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server &&
 test -z "$(gofmt -l internal tools cmd)" && go vet ./... &&
 go test -race -p=6 -count=3 -run "TestO36c|TestSharedTextIfChangedFreshness|TestCommitBatchMarkersMatchDesign|TestRecordContextPreservesNativeStorageErrors|TestShutdownBetweenDBCommitAndCacheRenameIsFailClosed|TestReviewTransaction|TestReviewStage|TestReviewAbort|TestReviewOldInline|TestReviewInlineFallback|TestReviewOtherInline|TestReviewColdInline|TestReviewCacheCrashRecovery" ./internal/repo/ ./internal/taskstatus/ ./internal/mosdns/ ./internal/api/rulelibrary/ ./internal/api/proxygroups/'
```
最终输出：repo91.083s、taskstatus2.788s、mosdns11.112s、rulelibrary71.721s、proxygroups5.847s全部ok。
A14原回归文件当前尚未进入go；合入后需移除其显式skip，已有等价共享窗口回归正常运行。

### 最新 go 接入与最终门禁启动

定向复核结束后go新增d43cca6e4，仅更新CFG-08 configdiff工具/测试/证据，不含Go生产修改。
第一次gate在07:43:22（北京时间）开始排队；未入槽时终止自己的gate并无冲突rebase go。
现在HEAD c2dfb6050，Go产品及测试与已验证树一致；没有把CFG-08更新回退。
最终前台gate使用：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/o36c-final-d43-20261118 -- bash -lc 'set -o pipefail; unset GATE_ONLY GATE_SLOTS; mkdir -p "$GATE_LOGS"; ( echo "$BASHPID" > "$GATE_LOGS/gate.pid"; exec server/scripts/gate.sh -p 6 ) 2>&1 | tee "$GATE_LOGS/console.log"'
```
07:44:30输出`1 gate(s) already running ... waiting for a slot`。15分钟排队上限仍从首轮07:43:22计，
不通过重启延长。仅在入槽后才会运行测试；未执行的最终全仓步骤不能冒称通过。

CFG09 owner额外回报隔离联合树4d3dfa7a2（其分类修复+等价6c产品）定向race3及vet通过，
真实Runtime四个子例均执行无skip：关机不落receipt、Canceled仍归500、断开请求仍完成、
native LinkError/PathError链保留。此联合证据不替代最终全仓gate，也不把CFG09分类修复带入6c。

## N8-5a 独立复核 F4：六场景及移植清单

已读go-review-n8-5a:server/parity/review-n8-5a.md及progress/n8-5a-shutdown.patch。
该报告F4是既有共享取消策略阻断；其typed Update尚未进入本分支，不能声称typed入口已验证。
本分支新增TestO36cRepoOwnerRuntimeMatrix，通过当前公开UpdateSystemTransaction逐项覆盖同一六场景，
比原补丁多检查完整非DB文件清单。旧TestO36cRepoBeforeCommitRuntimeBoundary另有相同COMMIT屏障回归。

| N8-5a场景 | O3-6c当前公开入口回归 | 移植后要求 |
|---|---|---|
| before-owner/shutdown | TestO36cRepoOwnerRuntimeMatrix/before-owner/shutdown | typed补丁相同场景 |
| before-owner/disconnect | TestO36cRepoOwnerRuntimeMatrix/before-owner/disconnect | typed补丁相同场景 |
| updater/shutdown | TestO36cRepoOwnerRuntimeMatrix/updater/shutdown | typed补丁相同场景 |
| updater/disconnect | TestO36cRepoOwnerRuntimeMatrix/updater/disconnect | typed补丁相同场景 |
| before-commit/shutdown | TestO36cRepoOwnerRuntimeMatrix/before-commit/shutdown；TestO36cRepoBeforeCommitRuntimeBoundary/shutdown | typed补丁相同场景 |
| before-commit/disconnect | TestO36cRepoOwnerRuntimeMatrix/before-commit/disconnect；TestO36cRepoBeforeCommitRuntimeBoundary/disconnect | typed补丁相同场景 |

六场景及既有BeforeCommit对照的定向race3通过（.64，7.107s），repo vet通过；这是现有修复的覆盖补强，没有修改产品代码。
updater/BeforeCommit中途关机原实现失败的证据由N8-5a复核保留；本分支普通BeforeCommit也已有独立红证据。

N8-5a rebase明确冲突/人工核对清单：

- repository.go的writeOptions.batch、beginCommitBatch移到transaction.go；legacy.go:update只做转换，
  最终进入transact。transact入口Detach后检查ctx.Err，不能保留普通事务自动Uninterruptible。
- writeLocked的起始检查、callback后的batch判断迁入transactLocked。仅标记后的三处batch才切换
  ctxx.Uninterruptible；普通typed/legacy两路都把工作ctx传到最终提交。
- store_rows.go:commitStore改为(ctx,base,*model.Document,meta)，保留encodeModelRows，不回退jsonx序列化；
  最终调用Store.CommitContext。openLocked/createStore和其他commitStore调用点补齐ctx，不能因改名漏掉。
- store/store.go:CommitContext与BeforeCommit后紧邻COMMIT检查保持本步版本；Store.Commit兼容包装保留。
- profiles.go:DeleteProfile、importexport.go:ImportAll/ResetAll、shared.go:CreateRuleLibraryItem，
  逐段合并typed merge改动与首个文件副作用前的batch标记。files.go的最终rename/touch检查及已完成rename后的成功契约不变。
- multi_profile_test.go里私有commitStore调用签名同步typed文档及ctx；缺JobLock仍fail-closed，不因集成放宽。
- 应用N8-5a的原typed六场景补丁，并保留本分支真实Runtime锁等待、BeforeCommit、三类batch及文件/status/archive回归。
  两套入口通过后才能关闭F4，不以本分支legacy入口通过替代typed集成验收。

### 最终 gate：达到排队上限

北京时间07:43:22首次排队，到07:58:36累计15分14秒仍未入槽。
07:58:36核对自己的PID3677710、cwd及console中不存在`gate: slot`后，仅SIGTERM该排队runner；
rt前台退出143。旧runner3676320已在重新rebase前退出143，两者均不是测试失败。
**本轮最终gate没有执行gofmt/vet、tags、race、proddiff或difftest阶段，不能标为全绿。**
不再重排，完整门禁由协调者cf-gate；正式C33树定向race3与既有完整受影响包/差分证据见上文。
当前gate日志目录：`.64:/home/sunbroke/.cache/rt-tmp/o36c-final-d43-20261118`。

C33 owner另开go-fix-c33-bak补cache.go的.list.bak轮转，不在本步混入；双方已确认
主rename前检查、成功后best-effort backup契约，后续集成需保留本步runtime/Forget回归。

### 六场景最后定向结果

因rt对同一项目串行执行，本命令先等待自己的gate释放项目锁；gate退出143后继续正常运行：
```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/repo/shutdown_commit_test.go && go vet ./internal/repo/ && go test -race -count=3 -run "^(TestO36cRepoOwnerRuntimeMatrix|TestO36cRepoBeforeCommitRuntimeBoundary)$" ./internal/repo/'
```
退出0，`ok configflow/internal/repo 7.107s`。六子例×三轮全部运行，没有skip。
N8-5a仍须在typed事务移植完成后应用原patch复验；这项集成阻断不能提前写成closed。
