# C37/A24 DB → rule cache 崩溃间隙

## Brief

修复 discovery.ApplyDomains / UndoDomain 与 Agent IP 规则写入在 DB 提交、缓存保存之间的崩溃窗口。基于 go@690e97995，分支 `go-fix-c37-a24-crash`；仅处理持久失效/发布，保留 N8-10 agents core 迁移边界。所有检查固定测试服务器 .32，无完整门禁，无推送/PR。

## Done / decisions

- 将 C33 已有的 staged bytes → fsync `.invalid` + 目录 → DB commit → 声明核对 → rename → 目录 fsync → backup → 清除 receipt 协议从 `api/rulelibrary/cache.go` 提取到 `rulefetch/publication.go`，使用 native model/repo API；C33 保留薄适配层，未复制一套协议。
- C33、discovery、Agent IP 共用 identity cleanup lease。多目标先 TryLock 全集，遇忙释放已拿的 lease 再等待，避免持有一个 L2 阻塞另一个 L2；同库的 direct/proxy 只 stage 最终版本一次。
- discovery 两条更新已有内容的路径、Agent IP updater 都在事务回调内准备并持久化 receipt，事务成功后发布；不确定 DB 结果只清理 private temp，保留 receipt，读者从当前 DB inline 内容恢复。关机取消保留失效状态；事务内 stage 使用 owner lifetime。
- 新增 O3-8 `RuleDBCommitted` (`rule-cache` / `rule-db-committed`)；生产 Point 仍为空函数。同步 `o3-concurrency.md` 和 fault 工具说明，无新增用户操作入口或截图。
- `publication_crash_test.go`：4 场景（双库采纳、同库双目标采纳、撤销、真实 Agent HTTP IP 写入）× 正常 / SIGKILL = 8 cases。写进程在真实 DB 提交后的 barrier 被 SIGKILL；另一个进程重新打开同一 DATA_DIR，核对 DB 新字节、旧 cache 文件仍在但 ReadCache 明确拒绝、三条 GET 返回新内容、HEAD 空 body，随后 C33 cache repair 清 receipt/temp。正常路径核对完整字节及 HTTP。
- 现有 C33 崩溃/声明竞争/故障/backup 测试继续覆盖提取后的共享实现。无依赖新增；修复 Python 风格的 DB/cache 两步写入窗口，正常路径字节、HTTP 状态及 payload 不变。

## N8-10 重叠文件 / 文件规模

`server/internal/api/agents/client_tools_adapter.go` 仅改 imports 第 11、14、18 行，以及 `applyClientIP` 第 210–260 行（lease、原事务中的 stage、提交后 publication）。现有 client body、targets、policy、agent core 和 HTTP payload 适配均保留；N8-10 若替换为 native `rules.IPResult.Updated`，只需保留相同 StageCache 调用顺序及 model 声明。

新增 `rulefetch/publication.go` 435 行（主要从 C33 移动），`rulefetch/publication_crash_test.go` 278 行；原 C33 cache.go 从 337 行缩为 61 行。其他为小范围调用点、phase 注册、说明与本报告。

## Verification

.32：gofmt -l 无输出，默认/configflow_difftest 两种 tag 的 go vet ./... 均通过；相关 7 包 race 已通过。Agent 全包 race 达到 Go 默认 10 分钟总时限（`TestReregisterPreservesUpgradeRecordAndPendingOperationLock` 当时仅运行 6s，栈在 model/repo fresh fixture 初始化），因此不宣称 Agent 全包通过；按任务范围另跑 A24 定向 race 和 Flask 对比。未扩大修复范围或重跑全量 Agent。已通过专用故障测试：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/discovery/write.go --back server/internal/rulefetch/publication.go --back server/internal/api/rulelibrary/cache.go --back server/internal/rulefetch/publication_crash_test.go -- bash -lc 'cd server && gofmt -w internal/discovery/write.go internal/rulefetch/publication.go internal/api/rulelibrary/cache.go internal/rulefetch/publication_crash_test.go && go test -race -tags configflow_difftest ./internal/rulefetch -run "^TestInlinePublication" -count=1 -v'
```

尾部：`PASS`；`ok configflow/internal/rulefetch 28.479s`；8/8 正常/崩溃场景通过。

完整定向命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 PYREF_MODE=check && gofmt -l internal/rulefetch/publication.go internal/rulefetch/publication_crash_test.go internal/api/rulelibrary/cache.go internal/api/agents/client_tools_adapter.go internal/discovery/ruleowners.go internal/discovery/write.go internal/faultprobe/points.go internal/faultprobe/boundary_test.go && go vet -p 4 ./... && go vet -p 4 -tags configflow_difftest ./... && go test -race -p 4 ./internal/rulefetch ./internal/api/rulelibrary ./internal/discovery ./internal/api/agents ./internal/faultprobe ./internal/api/rules ./internal/api/domaindiscovery ./internal/lifecycle && CONFIGFLOW_DIFF=1 go test -p 4 ./internal/discovery/difftest ./internal/api/rulelibrary/difftest ./internal/api/agents -run "TestHTTPAndStorageParity|TestApplyNormalization|TestCFG06|TestRuleLibraryEditAndChecksHTTPParity|TestRuleLibraryReadCreateHTTPParity|TestA24FlaskBoundaries" -count=1'
```

上面命令的实际输出（`&&` 后的 Flask 对比因 Agent timeout 未执行，另用下列定向命令完成）：

```text
ok configflow/internal/rulefetch 65.390s
ok configflow/internal/api/rulelibrary 59.597s
ok configflow/internal/discovery 19.525s
FAIL configflow/internal/api/agents 600.046s  # test timed out after 10m0s
ok configflow/internal/faultprobe 1.009s
ok configflow/internal/api/rules 84.795s
ok configflow/internal/api/domaindiscovery 9.210s
ok configflow/internal/lifecycle 4.991s
```

A24 / Flask / fault boundary 定向命令（全部通过）：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 PYREF_MODE=check CONFIGFLOW_DIFF=1 && go test -race -p 4 -timeout=8m ./internal/api/agents ./internal/discovery/difftest ./internal/api/rulelibrary/difftest -run "TestReviewA24|TestClient|TestA24FlaskBoundaries|TestHTTPAndStorageParity|TestApplyNormalization|TestCFG06|TestRuleLibraryEditAndChecksHTTPParity|TestRuleLibraryReadCreateHTTPParity" -count=1 && go test -race -tags configflow_difftest ./internal/faultprobe ./internal/lifecycle -run "TestFault|TestConcurrencyFacadeBoundary" -count=1'
```

最后输出：

```text
ok configflow/internal/api/agents 43.552s
ok configflow/internal/discovery/difftest 22.048s
ok configflow/internal/api/rulelibrary/difftest 9.598s
ok configflow/internal/faultprobe 1.014s
ok configflow/internal/lifecycle 3.364s
```

## Status / Next

实现与本报告为同一提交（hash 见交接消息）。已 rebase 到最新本地 go@bcbb6e573（相较原基底仅新增 HANDOFF.md），无未完成实现或已知功能差异。全部本任务定向检查通过；Agent 全包 timeout 的限制见上，未称完整门禁通过。无新增 fuzz target。协调者负责 review / batch gate / 合入。
