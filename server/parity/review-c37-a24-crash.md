# C37/A24 DB → rule cache 独立复核

2026-11-19，非作者 Codex 复核；作者提交 `9a0b4dfcd`，分支
`go-review-c37-a24-crash`。结论：**verified，0 项未结发现**。本轮只新增定向交错回归及本报告，
不修改生产实现、Agent core 或用户入口；未跑完整门禁、未推送。`git rebase go` 确认已包含当前
本地 `go@bcbb6e573`。

## 崩溃顺序与恢复

逐行核对 discovery 的 ApplyDomains / UndoDomain、Agent applyClientIP、共享 StageCache / Commit / Discard
以及 rules / rulefetch 的 invalid reader。三条更新路径均在事务回调内完成 temp 写入/Sync、receipt
Sync 和目录 Sync，事务返回成功后才调用 Commit；defer 顺序保证 private temp 的清理先于 identity lease 释放。

| 中断点 | 重启状态及交付 |
| --- | --- |
| receipt 前 | DB 尚未提交，旧 DB 与旧 cache 仍一致；遗留 private temp 在下次 StageCache 清理 |
| receipt 后、DB commit 前 | DB 仍旧，receipt 拒绝稳定/legacy cache，内容出口回退到旧 DB inline |
| DB commit 后、cache 写入前 | DB 已新，receipt 拒绝旧文件，内容出口交付新 DB inline |
| rename 后、receipt 清除前 | 新文件已发布，receipt 仍拒绝读 cache，继续交付新 DB inline；目录 Sync 在移除 receipt 之前 |
| receipt 清除后 | 稳定文件为新内容；末尾目录 Sync 完成 receipt 移除的持久化 |

本轮不是物理断电测试；SIGKILL 验证进程中止/真实 SQLite 重开。所有阶段均有可用的 DB inline 内容。
receipt 不由普通 reader 清理，这是原 C33 协议；下一次成功编辑或手动缓存会重新发布并清理 receipt/temp，
无需删除 DATA_DIR、改 DB 或重建库。没有新增自动后台恢复要求。

作者的四种 SIGKILL 场景（双库采纳、同库双目标、撤销、真实 Agent HTTP IP 写入）均通过。
测试先等待 `rule-db-committed/entered`，再检查子进程确实死于 SIGKILL；另一个新进程验证 DB 已新、
磁盘 cache 仍旧、ReadCache 拒绝、三条 HTTP GET 交付新内容且 HEAD 无 body，然后 C33 操作修复。
这排除了进程已经完整完成后才被杀，以及只测了内存对象的假阳性。

## C33 提取与并发边界

- 对照提取前 `api/rulelibrary/cache.go`：temp / marker / backup 路径、文件写入和 fsync 次序、
  rename 后不可取消区、错误时 fail-closed 均保留；薄适配层没有改写 prepared bytes。
- 声明比较仍只覆盖 `url/source_type/behavior/format/content`，缺失按 null 比较；model 保留存储成员原值。
  metadata 修改继续允许发布，cache-only 哨兵继续中止未变 DB 候选，不增加 revision/hooks。
  定向实测 C33 staged/committed/renamed/complete、stale stage、metadata/prepared bytes、abort baseline 均通过。
- C33、discovery、Agent IP 共用同一按绝对 cache identity 路径索引的 L2 lease。
  多库获取只 TryLock 全集，失败释放已拿的所有 lease 后才等待；不会持一把 L2 阻塞另一把 L2。
  同库 direct/proxy 只 stage 最终声明一次。L3 内再次核对声明，独立写者不能迟到发布过期 stage。
- 新增 `TestReviewInlinePublicationInterleaving`：采纳→采纳、采纳→撤销、撤销→采纳。
  第一个真实编辑在 DB commit 后暂停，核对它仍持有 identity lease；第二个编辑同时启动时，
  第一个 private temp 与 receipt 不变。释放 barrier 后两者均完成，最终 DB/cache 完整字节一致且 receipt 清除。
  用例使用独立 DATA_DIR，失败时也释放 barrier 并等待 writer 退出。

## 修复前负控

仅在本 worktree 的 .32 远端副本临时换回 `9a0b4dfcd^:server/internal/discovery/write.go`，
只补 `faultprobe` import，并在原 `for _, item := range updated` / SaveRuleToLocal 之前插入
`faultprobe.Point(ctx, faultprobe.RuleDBCommitted)`。这是原本 DB-first 路径，加同一中间点观测，
未改动测试断言或预期；其他文件保持作者版本。用 trap 在结束时恢复原文件。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --no-sync -- bash -lc 'cd server && go test -race -p 4 -tags configflow_difftest ./internal/rulefetch -run "^(TestInlinePublicationCrashRecovery|TestReviewInlinePublicationInterleaving)$/^(adopt|adopt-adopt)$/^sigkill$" -count=1 -v'
```

负控 Go 测试退出 1，且检查日志必须同时出现以下两项（不是编译错误、timeout 或没有到达 barrier）：

```text
publication_crash_test.go:171: stale cache was admitted: "+.old.example\n+.remove.example\n" <nil>
review_publication_test.go:88: first editor lost stage/identity lease: [] <nil>
FAIL configflow/internal/rulefetch 4.558s
```

因此原代码会在真实 DB 已提交、cache 尚旧的 SIGKILL 点复活旧 cache；新增交错用例也在修复前失败。
临时旧源码未提交，本地生产代码从未修改。

## Agent 重叠范围

`git diff 9a0b4dfcd^ 9a0b4dfcd -- server/internal/api/agents/client_tools_adapter.go`
仅有三项 import，以及 `applyClientIP` 内取得 lease、事务内 stage、提交后 publication 的修改，
与作者列出的新文件第 11/14/18 行、210–260 行一致。没有改 client body、target/policy 选择、
响应 payload、路由或 Agent core。`rules.AppendIP` 每次最多更新一个库，单个 stage 变量不会丢失多库 stage。

## 定向验证

全部固定 .32，`GOMAXPROCS=4` / `-p 4`。没有重复作者全包 race、Flask 矩阵或完整门禁。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && go test -race -p 4 -tags configflow_difftest ./internal/rulefetch -run "^TestInlinePublicationCrashRecovery/(adopt|adopt-shared|undo|agent-ip)/sigkill$" -count=1 -v && go test -race -p 4 ./internal/api/rulelibrary -run "^TestReview(CacheCrashRecovery|StageRejectsNewerCommittedDeclaration|StageAllowsMetadataAndPreservesPreparedBytes|AbortClearsOnlyItsUnchangedBaseline)$" -count=1 -v'
```

```text
PASS
ok configflow/internal/rulefetch 14.336s
PASS
ok configflow/internal/api/rulelibrary 7.235s
```

新增测试远端 gofmt 后单独 race：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/rulefetch/review_publication_test.go -e GOMAXPROCS=4 -- bash -lc 'cd server && gofmt -w internal/rulefetch/review_publication_test.go && go test -race -p 4 -tags configflow_difftest ./internal/rulefetch -run "^TestReviewInlinePublicationInterleaving$" -count=1 -v'
```

```text
PASS
ok configflow/internal/rulefetch 11.279s
```

负控恢复后的最终检查命令与结果：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && test -z "$(gofmt -l internal/rulefetch/review_publication_test.go)" && go vet -p 4 ./... && go vet -p 4 -tags configflow_difftest ./internal/rulefetch && go test -race -p 4 -tags configflow_difftest ./internal/rulefetch -run "^TestReviewInlinePublicationInterleaving$" -count=1 -v'
```

```text
gofmt / default vet / rulefetch tagged vet: 无输出，退出 0
PASS
ok configflow/internal/rulefetch 7.690s
```

无新的功能差异、依赖或 fuzz target；完整集成门禁由协调者执行。
