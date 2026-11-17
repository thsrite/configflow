# O3-6c — 共享提交点响应关机

状态：implementing。分支 go-o3-6c，从 go@f90a156e 建立，已无冲突变基到 go@97580425b（N7-3d + configdiff测试/工具；后一批无Go生产修改）；不派子 agent、不 push、本机不跑测试。
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

## 窗口与验收表（实现后填写 commit / regression / red-green 证据）

| 来源/窗口 | 修复提交 | 真实 Runtime 回归 | 状态 |
|---|---|---|---|
| A14/C29/C33/C06：普通 repo 锁等待、回调/序列化后最终 COMMIT | 2697f0b1d | TestO36cRepoBeforeCommitRuntimeBoundary / TestO36cRepoLockWaitRuntimeBoundary | 定向红→绿 |
| C29/C33/C06：规则缓存 touch/replace，普通文件 rename | bd412ed22 | TestO36cAtomicFileRuntimeBoundary / TestO36cRuleTouchLockRuntimeBoundary / TestO36cRuleTouchAfterShutdown / TestO36cFileOwnerLockRuntimeBoundary | 定向红→绿 |
| C29/C33/C06：taskstatus Record/Forget 锁内提交 | 7a9b43e98 | TestO36cStatusLockWaitRuntimeBoundary / TestO36cStatusShutdownDuringRecord | 定向红→绿；C33 adapter待合入接线 |
| C29/C06：MosDNS archive 下游 I/O/cache/status 取消与产物交付 | dd8ffe907 | TestO36cArchiveClosedRuntime / TestO36cArchiveShutdownAfterFetch / TestO36cArchiveRuntimeIOBoundary | 定向红→绿 |
| 多步例外：DeleteProfile、ImportAll/ResetAll、CreateRuleLibraryItem | 2697f0b1d / bb5c00dae | TestO36cExplicitBatchRuntimeBoundaries / TestCommitBatchMarkersMatchDesign | race3绿，保留批处理语义的控制组 |
| 主 rename 后关机：成功返回且备份完整 | bd412ed22 | TestO36cAtomicFileRuntimeBoundary/shutdown-after-primary | race3绿，保留已提交成功的控制组 |
| C33 F7/F8：持久marker后重开inline交付、下一次编辑恢复及Forget接线 | 待合入 | 草稿 /tmp/o36c-c33-recovery.patch、/tmp/o36c-c33-forget-context-test.go | 尚未应用、不能算通过 |

当前 archive.go 已在 O3-6a 改用 lifecycle.Detach，没有剩余入口 WithoutCancel；仍需共享层修复和
真实 Runtime 端到端回归，不能据此把窗口标成已修。
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
AST TestCommitBatchMarkersMatchDesign 限定三处 marker 调用；多步路径端到端测试仍待补。

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
当前分支已无 taskstatus.Store.Record/Forget 的生产调用；C33 尚未合入，其调用方待合入接线。

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
审计其余 repo.update callback：均只改候选JSON；ImportClientProfile 的 build/各 Updater 按契约
返回候选数据。ConfigurationCommitted 在成功DB提交和L3释放后才通知备份owner，不属于事务回滚域。

## C33 合入衔接补充

已读 go-review-c33:review-c33.md 的 F7/F8（协调者提示 tip0ed0be17）。C33 stage→DB→
L3核验来源→rename/Sync/清marker 是可重放协议；DB后关机允许留下新声明+持久marker，
读侧fail-closed只交付当前DB inline，不能复活旧cache，因此不加入三处不可中断白名单。
待C33合入后补重启/下一次编辑恢复回归，保留F7核验与F8外层owner信号，不新增同动作重复检查；
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

正式分支当前9f423a7df（代码截止dd8ffe907），均已提交，工作区干净。C33当前仍未合入go，
正式树未加入C33产品代码；A14原SKIP仍等其合入后移除。N8-5a已避开提交核心，
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
ForgetContext的单行接线，C33进go后再带入正式6c并重验；当前不能算正式基线已通过。

最终全仓gate尚未开始。等待C33合入通知后rebase、完成上述集成与最终gate；排队15分钟上限不变。
