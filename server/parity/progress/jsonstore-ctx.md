# jsonstore owner context / health F1

独立 Codex 复核已通过（O3 提交前检查协议）；详见
[review-jsonstore-ctx.md](../review-jsonstore-ctx.md)。最终检查与系统调用之间仍允许每个调用
一次已放行操作，无硬性时间上界；兼容 API 的失败临时文件清理是有意行为变化。
复核新增定向回归和负控，修正文档承诺，未改产品逻辑。

## 范围与结果

任务：闭合 `review-n8-9-health.md` 的 F1，共享 JSON 文件叶层的关机提交窗口，
以及 discovery 的 probe / region 两个生产 adapter。基于 `go@d4fab6441`，
分支 `go-fix-jsonstore-ctx`。不改业务、磁盘格式、生产超时或调度；不跑完整门禁。

- `jsonstore.Store.WriteContext`：入口、普通/签名两种模式的最终 rename 前检查 ctx。
  打开临时文件后失败或取消均关闭描述符并清理 `.tmp`；主文件不变。
  rename 已成功则完成签名验证，不把已提交结果改报为取消。
- `RemoveContext`：入口、每次 unlink（含 EINTR 重试）前检查 ctx；保留缺失文件成功、
  目录 EISDIR 的语义。旧 `Write` / `Remove` 委托新 API；trafficstore 与既有兼容测试仍使用旧 API。
- health/check 的 probe/region 写入与删除共四处接到叶层 Context API。
  legacy `SaveContext` / `UpdateContext` 仅转码并委托 typed Context 入口；旧方法委托新方法，
  既有健康检查 corpus/replay 测试仍有调用。AST 包装层检查同时覆盖两层委托。
- discovery 的 `legacySaveProbes` / `legacyEditRegion` 保留原 owner ctx，调用上述 Context adapter。
  没有复制业务、替换生产 collaborator、缩短预算或新增依赖。

## 回归证据

所有新增生命周期用例均使用真正的 `Runtime.Start / BindRequest / Shutdown`。

- jsonstore：普通/签名 × 临时文件写完/最终 rename 前 × 关机/请求断开，8 案例；
  关机错误链为 Canceled、无签名、主文件字节与 identity signature 不变、备份不变、临时文件清理；
  断开后真实 rename 成功，签名模式得到实际文件签名。另测 unlink 前关机/断开、
  已停 owner 不创建目录、rename 后关机仍成功验证、两种模式 rename 失败清理临时文件。
  私有实例 hook 只暂停真实 I/O，`export_test.go` 将其暴露给外部测试包以避免 lifecycle 导入环。
- health/check：四条实际 store 入口 × 关机/断开，8 案例。在最后一次 health Err 已捕获 nil 后
  设置确定性抢占点，叶层必须自己重新检查；未替换 writer。
- discovery：调用原始 `saveProbes` / `editRegion`，实际落盘读取验证；每条各测已关机、
  health→叶层之间关机、此处断开。断开写入成功并保留未知顶层兄弟字段；备份不变，无残留临时文件。

先红后绿（全部 `.32`，`GOMAXPROCS=4`，`-p 4`）：

1. 先仅建立新 API 与 I/O 屏障、不加 ctx 检查，运行下列叶层回归返回1：
   四个关机写入均写出 `{"value":"new"}`，signed 模式甚至返回新签名；unlink 删除原文件；
   已关机写入仍创建目录。此阶段 API 能编译，失败是行为失败。

   ```sh
   RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/jsonstore/context_test.go --back server/internal/jsonstore/export_test.go --back server/internal/jsonstore/jsonstore.go -- bash -lc 'cd server && gofmt -w internal/jsonstore/{jsonstore.go,context_test.go,export_test.go} && go test -p 4 -race -count=1 -timeout=3m -run "^TestContext" ./internal/jsonstore'
   ```

   末行：`FAIL configflow/internal/jsonstore 0.034s`。
2. 叶层已修但 discovery 还保留旧接线时，以下回归返回1：两条入口都在已关机后返回 nil 并改写主文件。

   ```sh
   RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/discovery/jsonstore_context_test.go -- bash -lc 'cd server && gofmt -w internal/discovery/jsonstore_context_test.go && go test -p 4 -race -count=1 -timeout=3m -run "^TestDiscoveryJSONStoreOwnerLifetime/.*/already-stopped$" ./internal/discovery'
   ```

   末行：`FAIL configflow/internal/discovery 0.477s`。

## 定向验证

以下首次合并代码运行：gofmt、全仓 vet、jsonstore/trafficstore race 成功。
health/check 仅旧 AST 期望仍要求无 ctx 的 typed 方法而失败；已扩展为检查旧 wrapper→Context wrapper→typed Context 两层。
同轮 discovery 定向测试成功（4.092s）。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/jsonstore/jsonstore.go --back server/internal/jsonstore/context_test.go --back server/internal/health/check/legacy.go --back server/internal/health/check/probe.go --back server/internal/health/check/region.go --back server/internal/health/check/review_n89_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/jsonstore/{jsonstore.go,context_test.go} internal/health/check/{legacy.go,probe.go,region.go,review_n89_test.go}; test -z "$(gofmt -l internal/jsonstore internal/health/check internal/discovery/legacy.go internal/discovery/jsonstore_context_test.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m ./internal/jsonstore ./internal/agents/trafficstore; go test -p 4 -race -count=1 -timeout=5m -run "TestReviewN89|TestStoresTreatCorruptFilesAsEmpty|TestProbeStore|TestR5FallbacksAndStoreValidation|TestCheckLegacyWrappersOnlyCallTypedEntries|TestNativeProbeAndRegionFilesInteroperateWithPython|TestDiscoveryJSONStoreOwnerLifetime|TestReviewRestartDropsPollingJobsButKeepsProbeStore" ./internal/health/check ./internal/discovery'
```

```text
ok configflow/internal/jsonstore 1.047s
ok configflow/internal/agents/trafficstore 1.331s
```

最终验证（退出0，gofmt / 全仓 vet 无输出）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/health/check/typed_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/health/check/typed_test.go; test -z "$(gofmt -l internal/jsonstore internal/health/check internal/discovery/legacy.go internal/discovery/jsonstore_context_test.go)"; go vet -p 4 ./...; go test -p 4 -race -count=1 -timeout=5m -run "TestReviewN89|TestStoresTreatCorruptFilesAsEmpty|TestProbeStore|TestR5FallbacksAndStoreValidation|TestCheckLegacyWrappersOnlyCallTypedEntries|TestNativeProbeAndRegionFilesInteroperateWithPython" ./internal/health/check; go test -p 4 -race -count=1 -timeout=5m ./internal/discovery; go test -p 4 -race -count=1 -timeout=5m -run "TestReferenceCorpus|TestReviewCorpus|TestReviewN89" ./internal/health'
```

```text
ok configflow/internal/health/check 17.611s
ok configflow/internal/discovery 19.718s
ok configflow/internal/health 17.429s
```

## 交接

F1 已完成，无遗留功能差异或未修复 finding。全部测试均在 `.32` 执行。
提交为本报告所在的 `go(jsonstore): honor owner context at file publication`，
可用 `git log -1 -- server/parity/progress/jsonstore-ctx.md` 定位。

新增文件：`jsonstore/context_test.go` 187 行、`jsonstore/export_test.go` 17 行、
`discovery/jsonstore_context_test.go` 137 行，及本报告。
未新增功能，未重录/改写 Python 参考，未新增 fuzz target；既有共享 JSON/Python 互通测试已覆盖。
完整 gate 留给协调者的 batch。batch-8 尚未合入 go；合入后按最新 go 变基，discovery rules 接线以 batch-8 为准。
