# batch-1 / batch-4 定向门禁修复

分支 `go-batch1-fix2`，按协调者更正从 `batch-4@3721ec69e` 分出；不修改 batch-1/batch-4。
所有测试、格式及vet通过rt在.32执行；不跑完整gate，不push，不用子agent，不访问生产。

## discovery 测试顺序故障

根因是fixture隔离：C07的真实Runtime测试最后Stop了进程共享默认Runtime，而jobFixture原来只
重建repository和jobState，后续restart测试的DefaultSpawner仍看到已停止的Runtime并拒绝任务。
这不是taskbridge保留ctx/generation有误；C07的shutdown语义保留，产品代码不改。

新增`TestJobFixtureStartsFreshRuntimeAfterShutdown`先停止前一个owner，再通过新fixture启动异步任务，
并检查旧generation的ctx仍被拒绝。修复前该回归和原restart测试均报`lifecycle: runtime is stopping`。
jobFixture现拥有空registry的新默认Runtime，cleanup先Stop/drain再恢复job metadata；手动任务测试
复用fixture配置，不重复Configure。batch-4上的三条相关测试race count5通过（12.910s）。

## rulelibrary backup API 集成

原测试两处仍调用O3-6c已删除的rules.TouchUnchangedCache，导致测试包无法编译。
改为真实repo.WriteSharedTextIfChanged：旧backup时检查primary确实被替换且backup更新为当前内容；
两者一致时检查primary inode不变而mtime变新，并检查backup inode/mtime均不变。
路径从repo.SharedPath根计算，不恢复旧helper、不复制unchanged比较逻辑。
单条race回归通过（1.604s）。两项修复按协调要求分别提交。

## 验证进行中

已启动一次前台组合：全仓vet、双tag vet，discovery/... race count3，整个discovery shuffle，
taskbridge相关包，以及rulelibrary/...和repo/... race/live。精确命令及最终输出待运行完成补齐。
