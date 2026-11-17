# O3-6c — 共享提交点响应关机

状态：implementing。分支 go-o3-6c，从 go@f90a156e 建立；不派子 agent、不 push、本机不跑测试。
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
| A14/C29/C33/C06：普通 repo 锁等待、回调/序列化后最终 COMMIT | 待填 | 待填 | 未修 |
| C29/C33/C06：规则缓存 touch/replace，普通文件 rename | 待填 | 待填 | 未修 |
| C29/C33/C06：taskstatus Record/Forget 锁内提交 | 待填 | 待填 | 未修 |
| C29/C06：MosDNS archive 下游 I/O/cache/status 取消与产物交付 | 待填 | 待填 | 未修 |
| 多步例外：DeleteProfile、ImportAll/ResetAll、CreateRuleLibraryItem | 待填 | 标记前零副作用、标记后完整提交 | 未验 |
| 主 rename 后关机：成功返回且备份完整 | 待填 | 待填 | 未验 |

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
