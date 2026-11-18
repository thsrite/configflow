# 测试提速 B 组

分支 `go-test-speed-b`，起点 `go@a9d6ce86c`。仅改测试与测试工具；不改产品、不 push、不启动子 agent。
共同使用 A 组的 `internal/testgolden`、`tools/pyref`；共享基础提交 `05ccbb133`、`d96f216cc`，不另写录制器。
按最新 `AGENT-BRIEF` 只做定向检查，完整 gate 由协调者在批次合并树执行。

## Python 参考与现场边界

仅把纯输入→输出参考计算迁成 golden：api/agents 的 A10 conditional，installer 的 A10 deflate/ZIP。
原始用例、拒绝规则、结果断言全部保留；输入与消费包测试源码、oracle 脚本、backend 源码和 venv 依赖均有哈希。
普通模式不调用 Python callback；哈希变化直接失败并提示重录。显式 check 现场重跑且只读；更新须 record+accept。

协调者已明确批准逐调用点保留：installer 的被测 bootstrap/migration Python 与 shell 依赖 stub，
repo 的显式跨语言 SQLite/锁/crash，api/agents 的 `CONFIGFLOW_DIFF` 现场 HTTP。root agents 的代理网络差分
也原本只在 `CONFIGFLOW_DIFF=1` 执行，继续保留真实网络观测。没有把现有 corpus 重新生成或放宽比较。
登记在同一审计工具读取的 `tools/pyref/live-exceptions-b.json`，按 file/function/callee/count 限定，不能整包放行。

## ippresence

原先每个有效 row 都新建 SQLite 仓库。现在每个 corpus 只初始化一次；逐行替换 library（包括空值）、
删除并重建 cache 目录、继续 ClearMemo。每条原有结果/错误文本/分类与计数断言保留；三个 corpus 互不共享仓库，
用 `t.Parallel`。memo 的精确容量/mtime 测试仍串行，先于并行 corpus 完成。

`.64` 初步定向 race：三个完整 corpus 8.030s PASS；新增 reset 隔离回归 1.431s PASS。
状态泄漏负控实际临时删掉 `os.RemoveAll(f.root)`，回归以 `previous row files leaked` 失败；恢复后通过。
没有把编译失败算负控成功；命令返回非零且必须含该断言文本。真实负控日志保留 `.ci-results/test-speed-b-check/`。

## agents/monitor

三组 gzip corpus 只解压/解析一次，按原测试读取的字段取独立 DeepCopy；避免每个测试复制其从未使用的整个语料。
三 seed 的慢排名测试仍执行原来全部数据与计数/排序/上限断言；每个 seed 独立 core/store/call log，
父测试只装一个 hook，按 Agent ID 分派，子测试并行；清理 hook 在全部子测试后执行。
新增小型隔离回归；实际故意把全部 seed 分派到第一个 core，会因 `parallel seed received another fixture` 失败。
修复后的完整原三 seed 定向 race 82.584s PASS；小型隔离回归亦通过。最终同机前后耗时待补。

## 耗时测量（待收齐）

基线 `.32`，启动负载 1.26/6.22/16.39，`GOMAXPROCS=4`，命令原样用于前后：

```sh
go test -race -p=4 -count=1 -timeout=60m -json ./internal/agents/... ./internal/api/agents ./internal/installer ./internal/ippresence ./internal/repo
```

JSONL 和前后负载在 `.ci-results/test-speed-b-before/`。性能记录使用同机、同命令，不把 `.64` 试验当作可比收益。
后续包的最终验证、逐包耗时、现场 Python 耗时和提交映射在交回前补齐。

## agents

newEnv 的完整初始仓库只生成一次，读取已 checkpoint 的私有文件镜像；每个测试复制到自己的 TempDir，
仍调用真实 OpenDefault/New。没有共享 Repository、连接或可写文件，也没有改变已有 openRepo 的重开/迁移测试。
模板出现 WAL/SHM 时直接失败，防止复制未 checkpoint 的 SQLite。新增双仓库修改隔离断言通过 race。
八个只处理本地 event/message 输入的测试并行；改全局 hooks、runtime、环境、时钟的测试保持串行。

## agents/metrics

只把 target key、全 Unicode lower/final-sigma 三个独立纯参考测试并行。数据保持原来的静态 Python corpus，
不启动 Python；修改 now/环境与数据库事务的测试保持串行，没有降低 Unicode 遍历范围。
