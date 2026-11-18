# 测试提速 B 组交付

实现完成，分支 `go-test-speed-b` 已重放到 `go@37aad7f65`（包含 batch-2 / N73 冻结语料优化）。
只改测试、golden 和共用测试工具，不改产品、不 push、不启动子 agent。完整 gate 由协调者按批次运行。

**正式性能计时待测。** 按维护者最新裁定，必须以新 go 为改动前底座，且两台机器的 1 分钟负载均低于 10
才开始。同机、同命令的正式前后数字及现场 Python 分项耗时，会另用文档提交补充。
本轮旧底座和高负载压力运行的数字全部废弃，不作为基线、收益或退化结论；旧 after 压力运行已停止。
未删测试、减少输入、缩短源码超时或放宽断言。

## 逐包实现与提交

| 包 | 提速内容 | 提交 |
|---|---|---|
| agents | 完整初始仓库只生成一次；复制已 checkpoint 文件到各自 TempDir，仍真实 OpenDefault/New；八个纯 event/message 测试并行 | `cb9ea7b8c` |
| agents/metrics | 三个独立 target-key / Unicode lower / final-sigma 参考测试并行，保留完整 Unicode 遍历 | `527d7138a` |
| agents/monitor | gzip corpus 只解码一次，只 DeepCopy 各测试原来使用的 section；原三个排名 seed 各自 core/store/call log 并行，父级唯一 hook 按 Agent ID 分派 | `e94e58c7f` |
| api/agents | A10 conditional 纯 Python 参考改用带哈希 golden，原日期/请求条件断言保留 | `3800dc344` |
| installer | A10 deflate/ZIP 纯参考改用带哈希 golden；六个纯渲染 corpus 与压缩测试并行 | `7b792e255` |
| ippresence | 每 corpus 初始化一次仓库；每行重置 library（含空值）、cache 目录和 memo；三个 corpus 并行 | `bc80253c7` |
| repo | 七个使用独立输入/DeepCopy 的纯 reference 测试并行；时钟、环境、共享 hook 和事务顺序测试保持串行 | `108aa2719` |

agents 的文件模板不共享 Repository、连接或可写文件；发现 WAL/SHM 就拒绝复制。隔离回归确认实际修改已落盘，
再重新打开另一份仓库核对不变。ippresence 保留三个文件内全部原始行、结果、错误和统计；精确 memo 容量/mtime
测试先串行完成。monitor 每个测试得到独立可写 reference section，三个大 seed 保留原记录量、范围、排名和上限断言。

agents/upgrades、trafficstore、agentfake、diag 未作无依据的改动；没有把共享全局状态测试强行并行。
没有修改、重生成或绕过已有历史真实 corpus，也没有修改 N73 冻结语料优化实现。

## 共用录制器与等价性

两组共用 `internal/testgolden` 与 `tools/pyref`。B 引入 A 的共享提交后只补强同一份审计器，
没有另写录制器；A 私有消费包改动没有混入 B。

新 golden：

- `internal/api/agents/testdata/pyref/TestReviewConditionalMatchesWerkzeug.json`：3000 dates、3000 conditional requests，919 个 Werkzeug refusal。
- `internal/installer/testdata/pyref/TestReviewDeflateAndZipMatchPython.json`：233 个输入、42,197,691 字节、60 个 ZIP；原 digest/size/替代 zlib 断言保留。

元数据包括 backend Python 源码、oracle 脚本、消费包测试源码、准确输入和 venv dist-info
METADATA/RECORD/WHEEL 的哈希。录制和依赖哈希统一使用 `testgolden.Python()`，避免两个解释器选择器不一致。
首次录制由任务授权；后续源码调整只更新 provenance，已直接比较确认 `entries` 字节不变。
两个包在最终底座分别现场 `check`，均报告 `PYREF equal`、输出 diff 0。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc '
cd server
PYREF_MODE=check go test -count=1 -v -run "^(TestReviewConditionalMatchesWerkzeug|TestReviewDeflateAndZipMatchPython)$" ./internal/api/agents ./internal/installer
'
```

普通回放不调用 Python callback。`check` 只读且列差异；`record` 的覆盖另需 `PYREF_ACCEPT=1`，
原断言失败时不写。参考输出出现差异需人工审阅后接受，CI 不设置 accept。
其余包已使用静态历史 reference，没有假称又完成了一次现场重录。

## 保留的现场边界

共用工具读取 `tools/pyref/scopes-b.json` 与 `live-exceptions-b.json`。
登记为 **28 组精确 file/function/callee/count，覆盖 30 个 constructor 引用**，逐项有 guard/kind/reason，不能整包豁免。

- agents：`CONFIGFLOW_DIFF=1` 的真实代理 HTTP 差分。
- api/agents：`CONFIGFLOW_DIFF=1` 的 public-agent/client-tools HTTP/JSONL 会话；difftest 仍现场运行。
- installer：被测 bootstrap/migration Python，以及真实 shell 所依赖的 Python stub；不能替成静态答案。
- repo：显式 `CONFIGFLOW_PY`/`CONFIGFLOW_REPO` 的 SQLite 读写、互通、锁和 crash；私人快照入口保留，任务中不启用。
- Go 自进程和原生 shell 检查也逐点登记，不能用它们的登记解释新的 Python oracle。

## 自动审计与缓存

`tools/pyref.TestAuditRepository` 是普通测试中的实际仓库审计入口，原生读取 scope、登记表及源码。
它检查整个 scope 子树的 `.go`，包含非 `_test.go` 的 helper；新增/消失的调用、引用数变化或登记表缺少 manifest 均失败。
import alias、函数值、package initializer、exec.Cmd 字面量均检查；进程包 dot import 拒绝。
B 的 helper-source 补强提交为 `6ad7a8cac`，登记提交为 `6c7349487`。

backend/venv 位于 Go module 外，单靠测试内读取不足以保证缓存失效。
共用 wrapper 原生计算 fingerprint，测试无条件读取并核验 `PYREF_INPUT_FINGERPRINT`，使其进入 Go cache key：

```sh
cd server
go run ./tools/pyref -test -- -race ./internal/api/agents ./internal/installer
# 门禁可使用同一原生计算入口；B 没改 gate.sh。
go run ./tools/pyref -fingerprint
```

未提供 fingerprint 的缓存模式直接失败；显式正数 `-count` 已禁用缓存，可以直接运行并检查实际哈希。
源码/输入/venv 漂移均提示重新录制，不能静默沿用结果。

已实际验证以下负控，均检查预期失败原因，不把编译错误当成功：

| 防止空通过的位置 | 实际负控与结果 |
|---|---|
| ippresence reset | 临时省略 cache RemoveAll，回归以 `previous row files leaked` 失败；恢复后通过 |
| monitor seed 隔离 | 临时把所有 seed 分派到第一份 core，以 `parallel seed received another fixture` 失败；恢复后通过 |
| exec 登记 | 新增 aliased constructor 被拒绝；普通 `.go` helper 的 constructor 也被实际源码审计拒绝 |
| 审计自身的缓存 | 实际 `TestAuditRepository` 第二次 cached；新增 ippresence 普通 helper 文件后不再 cached，并明确报未登记；删除后恢复通过 |
| golden 缓存 | 同一 wrapper 命令第二次 cached；临时改 backend 源码后不再 cached，报哈希不符；恢复后再次 cached |
| 普通入口 | 缺 fingerprint 的普通缓存调用失败；strace 的新纯 oracle/显式 guard 默认路径没有 Python exec |

共用工具还保留源码/input/venv 漂移、未 accept、原断言失败不写等负控。
普通纯 oracle 的零 exec 结论不涵盖已批准保留的产品脚本或显式现场对照。

## 最终底座的定向验证

`.64`，验证树为 `go@5264ba80e` + B；最后重放到 `37aad7f65` 的差异仅 progress/proddiff 文档，Go 源码相同，按 Leaner verification 不重复运行。以下不是正式性能前后测量。
`gofmt -l internal/ tools/ cmd/` 无输出，`go vet ./...` 通过；七个改动包的定向 race 和共享工具 race 通过。
最终原始日志在本 worktree `.ci-results/test-speed-b-rebased/`（未提交）。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 -- bash -lc '
set -e
cd server
pattern="^Test(Reference|Review|StoredPeriodsAddUpToTheExactCountsOfEveryRecord|ParallelSeedFixturesAreIsolated|SeededAgentRepositoriesAreIndependent|EventHistoryIsBoundedAndAdjacentUnchangedProgressDoesNotDuplicate|ExternalEventsRejectInvalidShapesTimesAndStatusAndStripSecrets|RepeatedWorkerJournalMergesWithoutReplayingStages|EventTimesSortWholeSecondsBeforeFractionalSeconds|PublicHistoryLocalizesErrorsWithoutMutatingSavedEvents|KnownFailuresKeepTheExactOriginal|UnknownCoreAndSourceErrorsAreNotTranslated|UpgradeFailuresHaveActionableChineseMessages|A05ReferenceTargetKeys|A05ReferenceLowerEveryCodePoint|A05ReferenceFinalSigmaContext|RecordedPythonTests|Corpus|EdgeCorpus|ReplayRepositoryDoesNotLeakInputs|CacheMemoIsKeyedByPathMtimeAndSize|ContentMemoLimit|WireRegistersIsFullTun|ComposeRecordedCallsMatchPython|ComposeCorpusMatchesPython|ComposeReviewCorpusMatchesPython|DockerHACorpusMatchesPython|FuzzCorpusMatchesPython|RenderMatrixMatchesPython)"
go test -race -p=2 -count=1 -timeout=60m -run "$pattern" ./internal/agents ./internal/agents/metrics ./internal/agents/monitor ./internal/api/agents ./internal/installer ./internal/ippresence ./internal/repo
go test -race -count=1 ./internal/testgolden ./tools/pyref
go vet ./...
'
```

最后结果：

```text
ok configflow/internal/agents
ok configflow/internal/agents/metrics
ok configflow/internal/agents/monitor
ok configflow/internal/api/agents
ok configflow/internal/installer
ok configflow/internal/ippresence
ok configflow/internal/repo
ok configflow/internal/testgolden
ok configflow/tools/pyref
```

## 正式耗时后补

基线必须包含 batch-2；旧底座数据不用。两机 1 分钟负载均低于 10 后，在同一台固定机器上、同一命令、
同一产品底座分别跑 before/after，记录主机、Go/依赖版本、GOMAXPROCS、命令及实际起止负载。
保留 uncached 的实际执行测量，不把第二次 cache 命中算作代码提速。

| 包/现场组 | before race | after race | 起止负载/主机 |
|---|---|---|---|
| agents、metrics、monitor、api/agents、installer、ippresence、repo（逐包） | 待测 | 待测 | 等低负载窗口 |
| 未改 agents 子包对照 | 待测 | 待测 | 同上 |
| agents proxy 现场 | 待测 | 待测 | 单独列出 |
| api/agents 现场 HTTP | 待测 | 待测 | 单独列出 |
| repo 合成 SQLite/锁/crash | 待测 | 待测 | 单独列出；先处理既有失败 |
| installer 产品 Python / shell stub | 待测 | 待测 | 按登记点对应测试单列 |

包级命令保留为同一条：

```sh
go test -race -p=4 -count=1 -timeout=60m -json ./internal/agents/... ./internal/api/agents ./internal/installer ./internal/ippresence ./internal/repo
```

## 既有现场失败与交回边界

合成现场差分中，未改底座与 B 都有同样三项 repo 失败，B 没有改这些 helper 或断言：

- `TestCrossCheckOperations`、`TestCrossCheckInterchange`：layout 第 22 行，Python 有 `profiles/.copy.operation.lock`，Go 下一项为 `shared/`。
- `TestCrossCheckC04Operations`：layout 第 28 行，Python 有 `profiles/.ghost.operation.lock`，Go 下一项为 `profiles/work/`。

`TestCrossCheckLockExcludesPythonAndGo` 与 writer-killed-before-commit 实际通过，不能把上述文件布局差异说成根锁互斥失败。
协调者已交 `6e5c7760` 分诊，原始合成日志已提供；不把 repo live 套件称为全绿。
代理、API HTTP、直接产品 bootstrap/migration 的保留现场验证通过。上述旧底座现场数字同样不进入正式耗时表。

一次现场 after 任务与后续 rt 同目录同步冲突，日志被覆盖，该次作废；随后在独立不可变 worktree 完整重跑，
确认和未改底座相同的三项失败集合。所有源码负控已恢复，没有留下 probe/调试文件。
交回实现、golden 等价性和定向验证；正式计时按约定后补，完整批次 gate 由协调者运行。
