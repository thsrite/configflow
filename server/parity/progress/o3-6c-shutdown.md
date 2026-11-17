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
