# Go 测试提速交回审计

状态：全部完成。三包各三对测量、九对覆盖审计、双机三轮稳定性、三处变异检查及完整门禁通过；最终 rebase 后的三个目标包 race、refresh difftest 也通过。仅测试代码和进度文档；未改产品代码、未 push、未启动子 agent。

## 测量方法与结果

正式对照在 `.64`，按每包「原测试 → 改后测试」交替三对。每次运行前后完整记录 `/proc/loadavg`；`go test -race -count=1 -json` 记录包耗时，GNU `/usr/bin/time -v` 记录 user、sys、墙钟、RSS。CPU 为 user+sys，包含 Go 命令开销。以下均为三次中位数。

**oxidns 等量基线仅关闭原有 race 抽样，其他旧测试实现、并行行为、比较规则和 Test 节点不变；两侧均执行完整语料。** 最初 `.32` 的原抽样基线 147.434 秒仅作历史记录，不与完整语料混作等量结论。初始最慢 20 个测试及原因见三个 `test-parallel-<pkg>.md`。

| 包 | 包耗时：原 → 改后（秒） | Go 命令墙钟：原 → 改后（秒） | CPU：原 → 改后（秒） | 最大 RSS 中位数：原 → 改后（KiB） |
|---|---:|---:|---:|---:|
| health | 217.963 → 6.324 | 218.50 → 6.90 | 220.130 → 5.890 | 328472 → 329156 |
| refresh | 218.721 → 85.995 | 219.38 → 86.59 | 223.090 → 84.680 | 349536 → 349692 |
| oxidns | 210.552 → 132.558 | 211.13 → 133.58 | 828.090 → 948.540 | 4882644 → 2330044 |

health/refresh 的 CPU 降低主要来自去掉纯函数行无用的 SQLite 初始化、复用未修改的默认夹具；被测函数调用与断言仍保留。oxidns 墙钟约降低 37%，CPU 约增加 14.5%，RSS 约降低 52%；不能声称其 CPU 工作量减少。并发收益与 CPU 开销一并报告。

| 包 | 三次原包耗时 | 三次改后包耗时 | 三次原 CPU | 三次改后 CPU |
|---|---|---|---|---|
| health | 217.963 / 202.644 / 272.453 | 5.489 / 6.324 / 6.884 | 220.130 / 207.430 / 262.020 | 4.870 / 5.890 / 6.460 |
| refresh | 254.130 / 218.721 / 211.435 | 85.995 / 103.980 / 69.331 | 258.520 / 223.090 / 209.270 | 84.680 / 85.830 / 71.550 |
| oxidns | 216.413 / 203.249 / 210.552 | 143.163 / 125.635 / 132.558 | 833.470 / 828.090 / 804.040 | 932.050 / 948.540 / 952.910 |

完整负载快照：`.ci-results/test-parallel-1-64/loads.log`。每次 JSON、time 输出、覆盖审计及汇总分别为 `<pkg>-<轮次>-<before|after>.json`、`.time`、`<pkg>-<轮次>-audit.json`、`summary.json`。这些本机取回的原始证据被 `.gitignore` 排除，不进入提交。

## 测试节点与语料覆盖

每一对均逐项比较 `(Action, Test完整名称)` 的 Counter，**九对完全相等**。不仅比较总数，也检查缺失/新增名称。按 `go test -json` 的 `Test` 字段统计，包含 fuzz 根节点与种子。

| 包 | 每次原/改后顶层 pass | 每次原/改后子测试 pass | 原/改后 fail | 原/改后 skip |
|---|---:|---:|---:|---:|
| health | 8 / 8 | 0 / 0 | 0 / 0 | 0 / 0 |
| refresh | 41 / 41 | 7 / 7 | 0 / 0 | 0 / 0 |
| oxidns | 40 / 40 | 3025 / 3025 | 0 / 0 | 0 / 0 |

所有下列数量在三对中都逐项相等，来自实际运行日志的函数计数、比较结果计数与现有完整性断言。

- health：录制 2280 行、评审 1017 行、参考 790 行；双方都保留评审 2 条、参考 2 条既有 Unicode 豁免，实际比较分别为 2280、1015、788。全部逐函数计数及 N2 接受分类计数相等，合计实际比较 4083 个用例。
- refresh：录制访问 3150 行，双方既有 collaborator 排除均为 5，实际比较 3145；参考 1210 个外层用例，其中 8 个场景包含 43 次输出比较，即 1245 次参考输出比较。没有新增排除，逐函数计数与既有接受分类相等。
- oxidns：下表按文件、函数逐项聚合 `identical + differ + accepted`，双方都为 106966 行。严格 normalize 另比较 1442 行；C05/C22-C26 严格子用例分别 1778/94；ruleDNS 282 行、148 份最终配置；C27 为 469 个严格用例加 36 个 D2 用例、314 份生成结果；存储 round-trip 实际比较 6304 份文档。原有错误、中文消息、参数后态和所有生成数断言保留。

| oxidns 文件 | 原实际比较行数 | 改后实际比较行数 |
|---|---:|---:|
| corpus.jsonl.gz | 41217 | 41217 |
| recorded.jsonl.gz | 13245 | 13245 |
| c23_corpus.jsonl.gz | 21700 | 21700 |
| c23_recorded.jsonl.gz | 1330 | 1330 |
| c24_corpus.jsonl.gz | 3746 | 3746 |
| c24_recorded.jsonl.gz | 822 | 822 |
| c25_corpus.jsonl.gz | 13404 | 13404 |
| c25_recorded.jsonl.gz | 3525 | 3525 |
| c26_corpus.jsonl.gz | 4551 | 4551 |
| c26_recorded.jsonl.gz | 3072 | 3072 |
| review_s.jsonl.gz | 354 | 354 |

## 改动与有意串行项

- health：只并行原有两个回放测试及独立纯函数/私有仓库测试，保留原测试树；每个回放复用只读默认仓库，带 system 种子的行用私有仓库，纯函数/私有文件存储不建无用仓库。SetEnv 安装、调用、恢复在测试锁内。ReferenceCorpus、取消请求回归（全局 Env）、Agent delete hooks 测试有意串行；OS 子进程取消回归的真实延时保留。
- refresh：默认 SQLite 与目录树正常初始化一次；每条有状态行复制一份后正常 repo.Open，全部 before/after 文件与 shared 种子不变。录制回放及 setup、HTTP Env、lifecycle、task slots 测试有意串行；仅独立纯函数测试并行。
- oxidns：单 producer 只读/解压一次，有界队列与内部 worker 全部 join；不增加 Test 节点。存储及严格 normalize 内部分片，原有独立子测试并行，计数在 join/Cleanup 后保留原断言。CONFIGFLOW_ROOT 的 t.Setenv 测试有意串行；外部 hook 仅 init 安装一次、读私有只读仓库。

没有逐例启动 Python 的问题；没有更改生产时序或单个测试的等待上限。count=3 的外层 Go 包总执行上限用 30m（每轮默认 10m 的三倍），用于共享机器上的三轮全语料验证，不改变单例超时断言。

## 稳定性、变异与门禁

- `.32` 最终三轮 race：health 31.248s、refresh 354.053s、oxidns 1225.492s（均三轮合计）。health/refresh 的组合命令曾同时报告一个 OxiDNS helper 编译错误，已修正；二者包级 pass 有效，OxiDNS 用独立命令最终成功。旧过载/旧调度版本的中断试跑均不用于最终结果。
- `.64` 最终三轮 race：health 19.324s、refresh 259.024s、oxidns 426.881s（均三轮合计，JSON 包事件 Elapsed）。
- `.32` `gofmt -l internal/ tools/ cmd/` 无输出，`go vet ./...` 通过，输出 `FORMAT_VET_PASS`。
- 三处临时副本变异检查通过：三个未变异控制组均通过；三个变异组均 exit=1，出现目标 Test 的 assertion fail，均非 build-fail。原项目产品文件从未修改；临时副本逐文件恢复并 cmp/sha256 检查，随后删除。
- 完整门禁：`.64 server/scripts/gate.sh -p 6`，两槽队列排队后 exit=0、`GATE PASS`。全树格式/vet、两个 build tag、全量 race、proddiff、全部 13 个 difftest 包均通过。proddiff `baseline: new=0 stale=0`、coverage_regressions=0。

| 包 | 临时返回值变异 | 检出的现有测试 | 确切失败证据（截取） |
|---|---|---|---|
| health | NormalizeConfig 的 mode 返回 mutation | TestReferenceCorpus | want `{"mode":"system","servers":[],"bootstrap":[]}`，实际 mode 为 mutation；exit=1 |
| refresh | DownloadCachePath 返回 mutation-downloads/ 前缀 | TestReferenceCorpus | `row 1113 download_cache_path ... got ... mutation-downloads/... want ... subscription-downloads/...`；exit=1 |
| oxidns | NormalizeSettings 返回 `M("mutation", true)` | TestC05ReviewRecordedNormalization | `row 13 ... out.mutation present go=true py=false`；exit=1 |

最终输出：`MUTATION CHECKS PASS: three controls passed; three mutants failed assertions`。原始 JSON、单点 patch 和恢复 SHA 在 `.ci-results/test-parallel-1-mutations/`。

## 确切复现入口

原测试取自 `26b97da6`；改后测试快照与本分支提交对应，均只包含三个范围内的 `*_test.go`。远端原项目产品代码在每个配对前后相同。脚本及输入快照放在远端 `/tmp/go-test-parallel-1/`，交回时同时取回到证据目录。

```sh
RT_HOST=sunbroke@192.168.0.64 rt --no-sync --back .ci-results/test-parallel-1-64 -- bash /tmp/go-test-parallel-1/pairs64.sh
# 每包依次 before/after 三次；内部实际测试命令：
LC_ALL=C /usr/bin/time -v -o <pkg>-<n>-<phase>.time go test -race -count=1 -json ./internal/<pkg>
# 每一对之后执行：
~/cf-test/.venv/bin/python /tmp/go-test-parallel-1/audit.py <before.json> <after.json>
# 九次均输出：AUDIT <pkg> <n>: identical test identities/actions and corpus comparison counts

RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server; go test -race -count=3 -timeout=30m -json ./internal/oxidns > /tmp/go-test-parallel-1/final-oxidns32.json'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -- bash -lc 'cd server; go test -race -count=3 -timeout=30m -json ./internal/oxidns > /tmp/go-test-parallel-1/final-oxidns64.json'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync --back .ci-results/test-parallel-1-mutations -- bash /tmp/go-test-parallel-1/mutations.sh
```

每个包独立提交，分支 `go-test-parallel-1`；结束前执行 `git rebase go`，最终 SHA 在交回消息与证据 manifest 中记录，未 push。

## 完整门禁与最终 rebase 基线（分别记录）

- 配对测量产品基线：`8c5d2835`，两侧完全相同；原测试来自 `26b97da6`（oxidns 仅关闭原抽样以等量比较）。最终 27 个测试快照文件与分支逐字节一致。
- **完整门禁基线：`go@0f943623`**，被验分支当时 HEAD 为 `513da0cf`；日志 `.ci-results/test-parallel-1-gate/`。本轮 race 的三个包分别为 health 6.664s、refresh 68.435s、oxidns 116.515s。
- **最终 rebase 基线：`go@93fdcc62`**。门禁期间上游合入 N8-6 HTTP 绑定与 R08；按协调 agent 明确要求不重排完整门禁，补跑三个目标包 race 和 refresh difftest，均通过。协调 agent 合并前另用 cf-gate 完整复验。
- 最终合并验证：health 6.484s、refresh 103.747s、oxidns 116.178s；refresh difftest 3.929s，全部 exit=0。日志 `.ci-results/test-parallel-1-merged/`。该次测试名称/结果计数再次与正式单轮审计完全相等。

完整门禁的确切命令：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back .ci-results/test-parallel-1-gate -- bash -lc 'mkdir -p .ci-results/test-parallel-1-gate; export GATE_LOGS="$PWD/.ci-results/test-parallel-1-gate"; set -o pipefail; { echo "BEGIN GATE $(cat /proc/loadavg)"; server/scripts/gate.sh -p 6; rc=$?; echo "END GATE $(cat /proc/loadavg)"; exit "$rc"; } 2>&1 | tee "$GATE_LOGS/console.log"'
```

最终输出截取：

```text
baseline: new=0 stale=0
== difftest
ok configflow/internal/health/difftest 13.763s
ok configflow/internal/refresh/difftest 3.392s
... 全部 13 包通过 ...
GATE PASS
END GATE 3.03 6.88 10.22 2/400 469479
```

最终 rebase 后的确切验证命令与结果：

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back .ci-results/test-parallel-1-merged -- bash -lc 'mkdir -p .ci-results/test-parallel-1-merged; echo "MERGED BEGIN $(cat /proc/loadavg)"; cd server; go test -race -p=6 -count=1 -json ./internal/health ./internal/refresh ./internal/oxidns > ../.ci-results/test-parallel-1-merged/race.json; rc=$?; tail -3 ../.ci-results/test-parallel-1-merged/race.json; echo "MERGED END $(cat /proc/loadavg)"; exit "$rc"'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync --back .ci-results/test-parallel-1-merged -- bash -lc 'echo "DIFF BEGIN $(cat /proc/loadavg)"; cd server; CONFIGFLOW_DIFF=1 go test -count=1 -json ./internal/refresh/difftest > ../.ci-results/test-parallel-1-merged/refresh-diff.json; rc=$?; tail -3 ../.ci-results/test-parallel-1-merged/refresh-diff.json; echo "DIFF END $(cat /proc/loadavg)"; exit "$rc"'
# package pass Elapsed: health 6.484 / refresh 103.747 / oxidns 116.178
# refresh/difftest: PASS, ok configflow/internal/refresh/difftest 3.929s
```
