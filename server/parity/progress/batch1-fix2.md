# batch-1 / batch-4 定向门禁修复

分支 `go-batch1-fix2`，按协调者更正从 `batch-4@3721ec69e` 分出；不修改 batch-1/batch-4。
所有测试、格式及vet通过rt在.32执行；不跑完整gate，不push，不用子agent，不访问生产。

## discovery 测试顺序故障

根因是fixture隔离：C07的真实Runtime测试最后Stop了进程共享默认Runtime，而jobFixture原来只
重建repository和jobState，后续restart测试的DefaultSpawner仍看到已停止的Runtime并拒绝任务。
这不是taskbridge保留ctx/generation有误；C07的shutdown语义保留，产品代码不改。

新增`TestJobFixtureStartsFreshRuntimeAfterShutdown`先停止前一个owner，再通过新fixture启动异步任务，
并检查旧generation的ctx仍被拒绝。修复前该回归和原restart测试均报`lifecycle: runtime is stopping`。
首次复现发生在更正前的778d8f8e基底；已用git diff确认相关C37测试、jobs/taskbridge及lifecycle实现
与更正后的3721ec69完全一致。下面最终全部验收都在3721ec69加本次两个修复的树上执行。
jobFixture现拥有空registry的新默认Runtime，cleanup先Stop/drain并恢复空闲、可接单的默认Runtime，
再恢复job metadata；手动任务测试复用fixture配置，不重复Configure。
Configure只在idle时成功；若worker刚释放busy key但Runtime仍在收尾，fixture有界等待其结束，不丢弃活任务。
首次只在cleanup Stop而不恢复idle的尝试使非jobFixture测试的matcher cleanup被拒；完整count3已明确检出，
没有放宽原Clock断言，已补齐退出fixture的空闲默认状态。四条相关回归race+shuffle count3通过（8.829s）。

## rulelibrary backup API 集成

原测试两处仍调用O3-6c已删除的rules.TouchUnchangedCache，导致测试包无法编译。
改为真实repo.WriteSharedTextIfChanged：旧backup时检查primary确实被替换且backup更新为当前内容；
两者一致时检查primary inode不变而mtime变新，并检查backup inode/mtime均不变。
路径从repo.SharedPath根计算，不恢复旧helper、不复制unchanged比较逻辑。
单条race回归通过（1.604s），提交f94aef170。两项修复按协调要求分别提交。

## 最终验证：全部通过

以下一次前台组合通过rt在.32执行，完整退出0；没有跑完整gate。gofmt检查无输出，
普通全仓vet和同时开启configflow_proddiff/configflow_difftest的全仓vet均通过。
CONFIGFLOW_DIFF=1使相关Flask边界比较实际执行，没有跳过差分或放宽原断言。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/discovery/lifecycle_test.go internal/discovery/review_c37_test.go internal/api/rulelibrary/backup_test.go)"; go vet ./...; go vet -tags configflow_proddiff,configflow_difftest ./...; CONFIGFLOW_DIFF=1 go test -race -p=4 -count=3 -timeout=40m ./internal/discovery/...; go test -race -shuffle=on -count=1 -timeout=40m ./internal/discovery; CONFIGFLOW_DIFF=1 go test -race -p=4 -count=1 -timeout=40m ./internal/tasks/... ./internal/api/tasks ./internal/webdav/... ./internal/api/domaindiscovery; CONFIGFLOW_DIFF=1 go test -race -p=4 -count=1 -timeout=40m ./internal/api/rulelibrary/... ./internal/repo/...'
```

```text
ok configflow/internal/discovery                 44.782s  # count3
ok configflow/internal/discovery/difftest         59.946s  # count3
ok configflow/internal/discovery                 15.216s  # shuffle=on
ok configflow/internal/tasks                      1.344s
ok configflow/internal/tasks/difftest            13.316s
ok configflow/internal/api/tasks                  5.801s
ok configflow/internal/webdav                     6.609s
ok configflow/internal/webdav/difftest            32.108s
ok configflow/internal/api/domaindiscovery         7.755s
ok configflow/internal/api/rulelibrary            58.900s
ok configflow/internal/api/rulelibrary/difftest   83.277s
ok configflow/internal/repo                     277.849s
```

交回：本分支只包含两个测试修复提交（均带Co-Authored-By）；没有修改lifecycle/ctxx、C37 jobs/taskbridge
或其他产品代码，也没有回退O3-6c owner取消语义。工作区收尾干净，提交号见交回消息。
