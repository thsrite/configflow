# F1 jsonstore owner context 独立复核（Codex）

基线：`go-fix-jsonstore-ctx@4f7d1adfe`；复核分支：`go-review-jsonstore-ctx`。
已读 `AGENT-BRIEF.md`、`PORTING.md`、WORKFLOW 第四常设项及 Leaner verification、
F1 规格、作者报告和 O3-6c。仅做定向检查，未跑作者全套或完整门禁。

结论：**按 O3 的提交前取消检查协议通过；没有未修复的功能 finding。**
发现并修正一项 Low 文档问题：`WriteContext` 的 “within ctx's lifetime” 和
`RemoveContext` 的 “only while ctx is active” 承诺过强。产品逻辑不改。

## 1. 最后一次检查与提交的剩余窗口

最终 `ctx.Err()` 返回 nil 后，goroutine 仍可被抢占；这时 owner 关机，随后恢复执行的
rename/unlink 仍可提交。无法靠再加一次检查消除这个 check/syscall 间隙。
本次新增 `TestReviewCancellationAfterFinalCheckAdmitsOneCommit`，在最终 Err 先捕获 nil、
真实 `Runtime.Shutdown` 完成后返回该快照，分别证明普通写、签名写和删除仍成功。
屏障现场同时确认主文件还是旧内容，写入时 `.tmp` 已有新内容；不是提交后的空屏障。
随后再次使用该 owner 的删除被 Canceled 拒绝。

**上界是操作数量，不是时长**：每个调用至多一个已放行的主文件 rename 或 unlink 尝试；
unlink 返回 EINTR 后重新检查，不会借此次放行持续重试。并发调用则各有自己的放行点。
调度、文件系统调用、以及成功 rename 后的签名读取均没有硬性时间上界，不能承诺 N ms，
也不能承诺 Shutdown 返回之后绝无已放行操作落盘。
这与 `design/o3-concurrency.md` O3-6c 的“primary rename 前检查，成功后完成收尾”一致；
不新增 Uninterruptible，不把整个编码/写临时文件变成不可取消批次。
WORKFLOW 第四项的“关机后不写入”应在此协议下解释为**最终检查观察到取消就拒绝提交**。

## 2. 临时文件和签名

- 打开临时文件后，写入、关闭、签名读取/摘要、取消或 rename 失败均走 defer：关闭描述符，
  未提交则删除 `.tmp`。成功 rename 已消费临时路径，不再删除该路径，避免误删后来者文件。
  编码/打开失败尚未取得临时文件所有权；不清理别人的旧临时文件。
- 清理使用 best-effort `os.Remove`：目录权限被外部更改、文件系统故障、强杀/崩溃不保证清理。
  Store 本来要求调用方串行化同 id 操作，本次没有改变这一约定。
- 签名没有独立磁盘 sidecar。返回值取自本次打开并 rename 的描述符；rename 后不再因 ctx
  取消退出，继续双重元数据/摘要检查。未返回旧主文件签名；验证失败时返回 nil 签名。
  外部替换路径时仍可能返回已写入 inode 的签名，这是既有身份契约；trafficstore 的
  `rememberStatus` 会与当前路径签名比较，拒绝缓存不同版本。外部原地修改返回 nil。
- jsonstore 不维护 `.bak`；作者的备份哨兵不变断言验证的是没有附带修改，并非备份事务。

新增 `TestReviewLegacyWriteFailureCleansTemp`：兼容 Write 的两种模式向带哨兵的目录 rename
失败，错误仍是 `*os.LinkError`，目录/哨兵保留且临时文件消失。用 `4f7d1adfe^` 的原始
jsonstore.go、stat_linux.go、messages.go 和此测试组成临时包运行，两个子例均行为失败：

```text
--- FAIL: TestReviewLegacyWriteFailureCleansTemp/signed=false
    review_cleanup_test.go:31: abandoned temp remains: <nil>
--- FAIL: TestReviewLegacyWriteFailureCleansTemp/signed=true
    review_cleanup_test.go:31: abandoned temp remains: <nil>
FAIL configflow/internal/jsonstore/jsonstore-before-1uo1dahz 0.003s
```

该运行以 `git show 4f7d1adfe^:server/internal/jsonstore/jsonstore.go` 取得原始源码，
由 `RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --no-sync -- bash -lc 'cd server && python -'`
的 stdin 程序建立临时包，在包目录执行
`go test -p 4 -count=1 -timeout=3m -run TestReviewLegacyWriteFailureCleansTemp .`，
断言退出 1 且两次命中上述残留错误，finally 删除临时包。没有只靠编译失败作红灯。

## 3. 兼容 API 和生产接线

`Write` / `Remove` 传 Background；legacy Save / Update 同样传 Background 后委托 Context
adapter，再委托 typed Context 入口，转码/校验/错误链不变。无 ctx 调用不新增取消来源。
health 四处写/删把原 ctx 交给叶层；discovery 的两个 adapter 保留原 owner ctx，未复制业务。
普通成功、签名、缺失文件删除和目录删除语义未变。**失败后不再残留 `.tmp` 是任务要求的
有意变化，所以不能称兼容调用的所有可观察副作用完全不变。**

## 4. 屏障负控

仅一次针对性负控：在远端临时移除紧挨两处 rename 和一处 unlink 的最终检查，保留入口检查、
真实 I/O 和原屏障；运行下列叶层用例（不运行别的 mutation）。finally 还原源码。

```sh
go test -p 4 -race -count=1 -timeout=3m -run 'TestContextWritePublicationLifetime/signed=.*/before-rename/shutdown=true|TestContextRemovePublicationLifetime/shutdown=true' ./internal/jsonstore
```

由同样的固定 `.32`、GOMAXPROCS=4 的 `rt ... -- bash -lc 'cd server && python -'` 执行。
普通与签名写均报 `shutdown write: ... error=<nil>`、`main file: {"value":"new"} <nil>`；
删除报 `shutdown remove: <nil>` 且主文件不存在。末行：
`FAIL configflow/internal/jsonstore 0.021s`。恢复源码后同一组屏障用例通过。
这证明最终检查确实在屏障之后、提交之前，并非仅由入口检查令用例空过。

## 验证（全部 .32，GOMAXPROCS=4，-p 4）

首次定向验证（退出 0；gofmt、全仓 vet 无输出）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/jsonstore/jsonstore.go --back server/internal/jsonstore/context_test.go --back server/internal/jsonstore/review_cleanup_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/jsonstore/jsonstore.go internal/jsonstore/context_test.go internal/jsonstore/review_cleanup_test.go; test -z "$(gofmt -l internal/jsonstore)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=3m -run "TestReview|TestContextWritePublicationLifetime/signed=.*/before-rename/shutdown=true|TestContextRemovePublicationLifetime|TestContextSignedWriteCompletesVerificationAfterRename|TestExternalInPlaceEditDuringVerifiedWriteIsNotSigned" ./internal/jsonstore; go test -p 4 -race -count=1 -timeout=3m -run "TestReviewN89LeafCommitLifetime/probe-save/shutdown|TestReviewN89LeafCommitLifetime/region-delete/disconnect" ./internal/health/check; go test -p 4 -race -count=1 -timeout=3m -run "TestDiscoveryJSONStoreOwnerLifetime/probe/shutdown-before-leaf|TestDiscoveryJSONStoreOwnerLifetime/region/disconnect-before-leaf" ./internal/discovery'
```

```text
ok configflow/internal/jsonstore 1.036s
ok configflow/internal/health/check 1.023s
ok configflow/internal/discovery 1.501s
```

负控还原后确认（退出 0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --no-sync -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/jsonstore)"; go test -p 4 -race -v -count=1 -timeout=3m -run "TestReview|TestContextWritePublicationLifetime/signed=.*/before-rename/shutdown=true|TestContextRemovePublicationLifetime|TestContextSignedWriteCompletesVerificationAfterRename|TestVerifiedWriteReturnsTheSignatureOfTheWrittenFile|TestRemoveIgnoresMissingFiles|TestWriteFailureLeavesTheOldFile" ./internal/jsonstore'
```

末行：`ok configflow/internal/jsonstore 1.052s`；新增的 2 个清理子例和 3 个最终检查子例均 PASS。

旧 API 的实际签名消费者/目录删除抽查（退出 0）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --no-sync -- bash -lc 'cd server && go test -p 4 -race -count=1 -timeout=3m -run "TestExternalReplacementDuringAtomicWriteDoesNotChangeWrittenIdentity|TestExternalInPlaceEditDuringAtomicWriteIsNotCached|TestDeleteRefusesDirectoryWithPythonText" ./internal/agents/trafficstore'
```

末行：`ok configflow/internal/agents/trafficstore 1.025s`。

完整门禁交协调者 batch；未新增依赖、业务入口、超时或 fuzz target，未改 Python 参考数据。
复核提交是本报告所在提交；作者提交为 `4f7d1adfe`。
