# batch-6 日志与 provider 重试归因/修复

分支 `go-fix-batch6-logs` 从指定 `batch-6@f9f3105ee` 新建，独立 worktree；不修改共享 batch worktree，不 push。全部测试/构建在 .32 经 rt；仅定向检查和 GATE_ONLY=compare。本文更正前一份报告把两条日志差异称为可放过的“负载噪声”：那个判断证据不足；客户端取消误报与生成器的配置重复解码都是真问题。

上一轮性能目标已达标：.32 proddiff **new=0 / stale=0，Python/Go 重试均为 0**，没有改 baseline/normalizer；当时后续两个 difftest 失败，synthetic live 未运行。两条失败在父提交 f9f3105ee 同样存在，但都是 batch-6 相对 go 新引入的问题，后续按协调者要求继续修复，最终结果见下段。完整 core 仍由协调者在 .64 跑。

**后续收尾已完成：** 协调者要求继续修复两条 batch-6 difftest；已在 259efd4da 修正测试依据/夹具。最终 .32 整条 GATE_ONLY=compare 退出 0：new=0/stale=0、双方重试 0、21 个 difftest 包全部通过、synthetic live 23/23 实跑且 0 skip。完整来源、判定依据和命令见 [batch6-difftest-followup-zh.md](batch6-difftest-followup-zh.md)。下文的两条“剩余”记录保留为上一轮历史，不再是当前未决事项。

## 原门禁逐行归因

读取的是协调者所指的 .32 run（起始 load `1.47 1.92 3.64`、结束 `8.93 9.63 7.29`，Go retries=84 / Python=0）。只在服务器私有记录分析；下列时间、动态 ID、仓库路径已替换，未把快照内容或凭据写进代码/报告。

`GET /api/logs/tail` **原始响应**中的 `logs[2]`：

```text
Python: <time> - backend.routes.agents - ERROR - Agent binary not found: <repo>/backend/static/agents/configflow-agent-linux-amd64
Go:     <time> - backend.app - ERROR - Exception on /api/agents/<agent_id>/oxidns/rules/update [GET]
Go 下一行: oxidns_ha_publish.rules is not ported yet (A18)
```

需要区分原始数组位置和比较器位置：既有 normalizer 会先去除时间戳，再把并发日志当作多集排序。原门禁实际报出的 **归一化后 body.logs[2]**，Python 仍是上面的 binary-not-found；Go 是 `ERROR - Exception on /api/aggregations/<agg_id>/provider [GET]`（其错误为 context canceled）。最终同一归一化位置两边都是相同的 binary-not-found，已无差异；原始 Go logs[2] 仍可为 A18 异常，属于既有 A18 多集/长度差异。初步只看原始 index2、把新增字段误归 A18 的判断在此更正；试加的 A18 行已撤掉。

`GET /api/logs/info`：Python size=546 bytes / size_mb=0.00；Go size=6656 bytes / size_mb=0.01，相差 **6110 bytes**。这不是舍入实现不同：两边都按 bytes / 1048576 后保留两位小数。

对同次返回的完整日志按实际 logger entry 分组，字节数包括 UTF-8 与换行：

| 类别 | Go entries | 物理行 | bytes | 归属 |
|---|---:|---:|---:|---|
| 二进制缺失 | 3 | 3 | 546 | 两侧相同 |
| A18 未移植异常 | 2 | 4 | 348 | A18 / batch-7 |
| backend.app 的 context canceled | 35 | 70 | 4714 | HTTP 请求已取消，误记为应用异常 |
| backend.routes.subscriptions 的错误记录 | 8 | 8 | 1048 | 专用 provider 日志边界，原日志只记脱敏的异常类型 |
| 合计 | 48 | 85 | 6656 | 与 size 完全相等 |

6110 = 348 + 4714 + 1048，不能把它全部归给 A18，也不能按低/高负载放过。修复同时覆盖 HTTPx 和 subscriptions 的带 ctx 日志边界；真实其他异常仍记录，响应不变。

## 取消日志：负控与修复

`App.internalError` 原来把所有返回 error 都无条件记录为 `Exception on ...`。真实 TCP/HTTP 客户端进入 handler 后取消，handler 返回包裹的 context.Canceled，修复前明确失败：

```text
FAIL TestRequestCancellationIsNotAnApplicationException/disconnected_wrapped_cancellation
application exception logged=true, want false
finish synthetic request: context canceled
```

改为只抑制与 **HTTP 请求本身已结束的 cause 匹配**的错误。请求仍存活时的内部 cancellation、断连后的其他错误、独立 DeadlineExceeded、panic 都继续走原异常日志；500 响应及现有中文提示不变。subscriptions 的失败日志增加工作 ctx，匹配取消时不记错误；已经 Detach 的 owner 不因原请求断开而隐藏其真实故障，也不改变后台写入/渲染寿命。

回归覆盖真实 HTTP 断连、包裹错误、活请求内部取消、断连后真实错误/内部超时，以及 detached provider owner。定向 .32 race：HTTPx 45.930s、subscriptions 4.621s；vet 通过。

## 重试：参照 Go 与 N8-8a 回退

另建独立 detached 参照 worktree，在**未修改的** `go@11b2ca19c` 跑一次 .32 GATE_ONLY=compare：完整阶段通过，proddiff new=0/stale=0，自检 23/23 PASS，Python=0 / Go=0 retries。开始 load `2.53 0.73 1.76`，结束 `2.77 1.42 1.89`（proddiff 摘要）；之后所有 difftest 与 23 个 required live entries 也通过。故 batch-6 的 Go-only 重试不是 Go 一直如此的既有现象。

代码差分定位到 N8-8a 的 legacy codec：`OverlayDialer` 每个生成节点都调用 `configFromLegacy`，而后者分别把完整配置序列化/解码为 model.Profile、model.Shared、model.System。即使节点没有 dialer，也先处理所有其他节点/规则/未知成员；一个 N 节点的生成把整份配置重解 N 遍。旧 Go 无 dialer 时直接返回。

合成 128 节点 + 512 规则的一批直接 overlay 分配约 3.05 GB，CPU profile 的主要热点是 JSON 与 GC。为避免只测内部实现，新增稳定的公开入口基准 `BenchmarkMihomoGenerationProfile`，32/128 节点都含同样的 512 条合成规则，真实调用 GenerateMihomoConfig，检查最后一个节点存在；不设墙钟断言、不缩减原始输入。另有完整输出节点顺序、数量、输入不变回归。

| 版本 | 32 nodes：ns/op / B/op | 128 nodes：ns/op / B/op |
|---|---|---|
| 旧 Go 11b2ca19c | 2,362,742 / 2,946,840 | 6,163,775 / 9,834,056 |
| batch-6 f9f3105ee（修复前） | 756,374,455 / 800,290,688 | 3,098,274,950 / 3,205,179,752 |
| 只复用 overlay 的中间修复 | 173,694,171 / 173,134,648 | 219,749,114 / 212,672,304 |
| 复用稳定 lowered view（中间） | 87,550,023 / 92,102,672 | 125,782,981 / 117,793,448 |
| 最终：复用 + 窄 codec | 46,459,868 / 53,277,608 | 72,510,587 / 77,159,944 |

最终 128 节点累计分配较 batch-6 降低 **97.6%**，耗时降低约 **97.7%**；allocs/op 从 40,131,148 降至 763,421（旧 Go 32,589）。不声称已恢复旧 Go 的 9.8 MB：77.2 MB 仍有差距。首次修复只复用 overlay，中间修复又复用依赖/规则集 view，均不足以消除全部重试；后续单请求采样才定位到其他调用方也逐规则集进入同一个整配置 codec。

最终方案按两次裁定：稳定 lowered profile 的节点类型化 view 在最早的 DependencyNodeIDs/shape 校验处建立，每次 render 内复用；规则格式只投影 rule_library 并走 model.Shared 的原 R5 解码；dialer 只投影 nodes/subscriptions/subscription_aggregations/proxy_groups；规则输出和 UDP/NTP 只投影 rule_configs；GitHub URL 只解 system_config；EasyTier 只解 mihomo.easytier。各自继续调用既有 native 函数，不复制业务逻辑，没有全局缓存或跨请求复用，原整配置 configFromLegacy 已删除。Mihomo 内按原读取点分别建立格式 view 与规则输出 view，EasyTier 仍读取 logical profile。

对最终 128 节点运行 alloc_space profile：约 23.66 MB 归于 validateGenerationRules，12.64 MB 归于只解 rule_configs 的 rulesConfigFromLegacy；主要累计热点是 model.DecodeRecord / EncodeRecord / Marshal、RuleConfig 的 R5 校验和记录本身的编解码，未再经过整配置/Agent snapshot codec。本轮不改 model 框架或削弱 R5 校验。N8-8b/c 在 batch-7 自行解决此处冲突。

为从完整 compare 中留下可核验的重试原因，harness 新增私有 `transport-attempts.jsonl`：只记 side/method/路由模板/status/timeout/elapsed/异常类/errno；不记展开路径、请求正文、凭据或异常文本。响应 tuple、重试次数/超时策略、比较器、normalizer 全部不变；新增测试证明传输失败 callback 不改变返回值与 timeout，proddiff 的 53 个单测通过。

## A18 基线范围

baseline 原生支持精确 route + JSON path + difference kind，不能按日志文本附加条件。曾暂加 `GET /api/logs/tail body.logs[2] value → A18` 供复验；取消日志修复后该差异消失，compare 将这行报 stale，已按裁定**撤掉**。size_mb 也已无差异。因此最终此分支不改 baseline/normalizer，也不增加 A18 例外；此前的 A18 F1 与 R14 延期行保持原样。

## 定向命令与输出

### 取消日志（修复前/后）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/httpx/cancellation_log_test.go -- bash -lc 'cd server && gofmt -w internal/httpx/cancellation_log_test.go && go test -race -count=1 -timeout=10m -run "^TestRequestCancellationIsNotAnApplicationException$" ./internal/httpx'
```

修复前 0.960s，预期断连子例失败（上方已给出）。修复后：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/httpx/app.go --back server/internal/httpx/doc.go --back server/internal/httpx/cancellation_log_test.go --back server/internal/api/subscriptions/adapter.go --back server/internal/api/subscriptions/providers.go --back server/internal/api/subscriptions/cancellation_log_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/httpx/app.go internal/httpx/doc.go internal/httpx/cancellation_log_test.go internal/api/subscriptions/adapter.go internal/api/subscriptions/providers.go internal/api/subscriptions/cancellation_log_test.go; go vet ./internal/httpx ./internal/api/subscriptions; go test -race -count=1 -timeout=15m ./internal/httpx ./internal/api/subscriptions; cd ..; ~/cf-test/.venv/bin/python -m unittest discover -s server/tools/proddiff -p test_proddiff.py'
```

```text
ok configflow/internal/httpx 45.930s
ok configflow/internal/api/subscriptions 4.621s
Ran 53 tests in 0.294s
OK
```

### 旧 Go 的对照

```sh
# 独立 reference worktree，detached HEAD 11b2ca19c，不改共享 go / batch worktree。
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e GATE_ONLY=compare -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/batch6-logs-go-reference -- bash -lc 'cat /proc/loadavg; server/scripts/gate.sh -p 4'
```

```text
baseline: new=0 stale=0
transport_retries: python=0 go=0
coverage_regressions=0 unmapped_routes=0 self_check=PASS
SYNTHETIC LIVE PASS: 23 required entries ran and passed; 0 skips
GATE STAGE PASS: compare
```

### 公开入口基准

在当前树先加基准、保持旧实现取得 before，然后再改 lowered config 复用取得 after。旧 Go 只临时放入完全相同的合成基准文件，不改实现/不提交，运行结束后已删除该文件，reference worktree 恢复干净。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/convert/batch6_generation_bench_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/convert/batch6_generation_bench_test.go; go test -run "^$" -count=1 -bench "^BenchmarkMihomoGenerationProfile$" -benchtime=1x -benchmem ./internal/convert'
# 旧 Go 的 reference worktree 上同样命令（无需 --back / gofmt）：
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'cd server && go test -run "^$" -count=1 -bench "^BenchmarkMihomoGenerationProfile$" -benchtime=1x -benchmem ./internal/convert'
```

before / after / 旧 Go 的具体数字在上表。这里的 B/op 是累计分配而非峰值 RSS。早期探查 CPU profile 的临时工具已删除，不作为产品代码/基准提交。


### 完整复用的定向检查

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/convert/legacy.go --back server/internal/convert/mihomo_generate.go --back server/internal/convert/mihomo_downloads.go --back server/internal/convert/common_native_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/convert/legacy.go internal/convert/mihomo_generate.go internal/convert/mihomo_downloads.go internal/convert/common_native_test.go; go vet ./internal/convert; go test -run "^$" -count=1 -bench "^BenchmarkMihomoGenerationProfile$" -benchtime=1x -benchmem ./internal/convert; go test -race -count=1 -timeout=10m -run "^(TestGenerationRetainsAllNodesAndSource|TestN88ReviewByteSamples|TestBatch6R5.*|TestTyped.*|TestCommonLegacyOnlyCallsTypedEntries|TestInvalidChainMetadataIsRejectedBeforeConversion|TestConversionFailureCannotSilentlyRemoveChainTopology|TestProviderPreflightGetsLogicalProfileAndFinishedDocument|TestGenerateMihomoDocumentLoadsTheGeneratedText)$" ./internal/convert'
```

```text
BenchmarkMihomoGenerationProfile/32-4  1 87550023 ns/op 92102672 B/op 1022577 allocs/op
BenchmarkMihomoGenerationProfile/128-4 1 125782981 ns/op 117793448 B/op 1279185 allocs/op
PASS
ok configflow/internal/convert 0.221s
ok configflow/internal/convert 22.821s
```

中间尝试记录（不隐去失败）：只复用 overlay 后，加入整个 TestMihomoCorpus 和 TestMihomoRecordedPythonTests 的 race 命令在 1200.044s 超时；栈停在 DependencyNodeIDs → shapeConfigFromLegacy → configFromLegacy 的 JSON 编解码，没有断言失败。另一轮 compare 在 content-sync observer 120s 截止前失败，尚未分类差异：私有计时记录 50 次 HTTP 尝试、0 次传输失败，但 Go Agent 读取耗时 62–122s。此后才补齐上述稳定 view 复用；没有删/缩减语料，没有提高超时。完整 core 按协调者安排在 .64 上复跑，此分支只做定向检查与 compare。

### 中间 compare：证明只修 Mihomo 不足

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e GATE_ONLY=compare -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/batch6-logs-fixed-final -- bash -lc 'cat /proc/loadavg; server/scripts/gate.sh -p 4'
```

```text
baseline: new=1 stale=1
NEW POST /api/generate/oxidns status transport-one-sided
transport_retries: python=0 go=95
self_check=PASS coverage_regressions=0 unmapped_routes=0
GATE FAIL: proddiff
```

起始 load `4.19 6.83 5.73`，结束 `4.31 9.64 9.36`。私有计时：OxiDNS POST 的 8 次尝试全超时，最长 160.101s；Surge/Loon 和 OxiDNS 预览/设置路由也有超时。这里的 stale 正是后来撤掉的 logs[2] 临时行，不能用更新 baseline 吞掉新的传输失败。


## 单请求隔离采样与三点来源对照

协调者批准后，在 .32 用 compare 同一份快照的私有可写副本，只启动一个真实 Go 服务，只发一个目标请求。复用 proddiff 的 cases、认证、隔离证明、只读 bind mount 和源快照前后摘要；每个 case 重新启动服务/复制数据，无请求重试。没有 Python 服务/外部设备假服务，只有 loopback。pprof 由未提交的临时 build overlay 注入，仅此私有二进制在 netns 内监听 127.0.0.1:15003；正式二进制与生产路由没有增加 debug 入口。超过 2s 的请求抓 goroutine dump 和 15s CPU profile；源码、栈、profile 与诊断二进制仅留在服务器私有目录，临时源码已从所有 worktree 删除。

每列为两个配置空间，均 HTTP 200：

| 版本 | OxiDNS POST 秒 | Loon POST 秒 | Surge POST 秒 | 当时 load 的范围（1 分钟） |
|---|---|---|---|---|
| 8ec68fd4d，8a 复核修复前 | 29.066 / 28.496 | 7.117 / 7.050 | 7.112 / 7.097 | 4.86 → 1.76 |
| f9f3105ee，8a 复核修复后 | 29.549 / 29.965 | 7.311 / 7.219 | 7.277 / 7.238 | 1.01 → 1.19 |
| f2666fd23，仅稳定 Mihomo view 修复 | 48.877 / 43.462 | 13.568 / 13.579 | 14.393 / 13.794 | 9.50 → 12.15 |
| e5cbeb6be，最终窄 codec | 0.813 / 0.808 | 0.303 / 0.296 | 0.293 / 0.289 | 1.41 → 1.35 |

这些是带采样的实际墙钟，不把不同负载下的时间直接当作精确倍数。特别是 f2666fd23 的高负载不能解释为该提交让未改动的 OxiDNS 变慢。`8ec68fd4d` 是 **8a 复核修复前**，已经包含 N8-8a 的移植；整配置 decoder 在本地 batch 历史 `7441cd04a`（N8-8a common typed records，原历史等价提交 f7c6f6f1a）引入，随后 dialer/EasyTier 也复用它。故根因属于 N8-8a 移植的 codec 边界，非 A16/A17/A21 的锁等待；也不能把它说成后来 R5 复核补丁新增的回退。

OxiDNS 请求 goroutine 为 runnable，核心调用链：

```text
configgen.Build/Render → oxidns.Generate → catalog/hasIPPart
→ ippresence.ClassicalIPLines → rulefetch.ReadValidatedRuleCache
→ ValidateRemoteRuleContent → convert.RulesetFormat → configFromLegacy
→ model.Shared/Profile/System JSON
```

15s CPU profile 中 RulesetFormat/configFromLegacy 累计约 77%；Surge 的 surgeRules → rulesetFormatAny → 同一 codec 约 80%。没有等 repo/owner 锁的主请求栈。原格式查询甚至在内容校验缓存命中之前就解整份配置，所以缓存不能抵消它。

私有诊断 runner 已归档（不是产品代码），三点与最终的准确重放命令结构如下，各自二进制由相应自有 detached worktree 用同一个临时 pprof overlay 构建；所有构建均经 rt：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -e GOMAXPROCS=4 -- bash -lc 'bash ~/cf-prod-snapshot/diagnostics/batch6-logs/probe.sh ~/cf-prod-snapshot/diagnostics/batch6-logs/before8a before8a'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -e GOMAXPROCS=4 -- bash -lc 'bash ~/cf-prod-snapshot/diagnostics/batch6-logs/probe.sh ~/cf-prod-snapshot/diagnostics/batch6-logs/after8a after8a'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -e GOMAXPROCS=4 -- bash -lc 'bash ~/cf-prod-snapshot/diagnostics/batch6-logs/probe.sh ~/cf-prod-snapshot/diagnostics/batch6-logs/after-fix after-fix'
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -e GOMAXPROCS=4 -- bash -lc 'bash ~/cf-prod-snapshot/diagnostics/batch6-logs/probe.sh ~/cf-prod-snapshot/diagnostics/batch6-logs/narrow narrow'
```

四轮均结束于 `snapshot unchanged`。源快照不写；临时副本结束时清理；仅保留私有采样文件。

## 窄 codec 的 R5 与分配证据

`TestRulesetFormatNarrowCodecPreservesR5` 覆盖坏 library 形状/坏数组元素、ID/name/URL 类型、显式格式优先于 URL、首 ID/name 匹配及坏尾记录不能覆盖已命中的声明。仍使用 model.Shared 原 R5 解码 + 原 RuleFormat；旧 R5 回归、native dialer/EasyTier 与共同边界测试也通过。

按协调者裁定，格式 codec 不再遍历无关字段里的非 JSON 瞬态 Go 值，因此这种值以前偶然触发的 Marshal 错误不保留。新测试在 agents/oxidns 放入 func/channel 证明无关值不被读取；持久化 JSON 的相关 R5 格式语义不变。

`BenchmarkRulesetFormatUnrelatedConfig` 的规则/库声明完全相同，只把无关 Agent history 从 0 增到 1/16 MiB；准备输入在计时外，调用公开 RulesetFormat，不设墙钟断言。同一文件在未改的 f9f3105ee 上临时运行取得 before（运行后已删除，worktree 干净）：

| 无关字段大小 | before B/op / ns/op | after B/op / ns/op |
|---|---|---|
| 0 | 91,096 / 328,314 | 43,400 / 261,592 |
| 1 MiB | 69,494,096 / 29,008,583 | 39,800 / 86,638 |
| 16 MiB | 1,124,373,744 / 473,002,009 | 40,840 / 114,844 |

after 的冷启动/池波动不随无关 payload 大小增长；16 MiB 的超大分配已消失。

最终代码的定向命令：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 --back server/internal/convert/legacy.go --back server/internal/convert/common_native_test.go --back server/internal/convert/mihomo_generate.go --back server/internal/convert/batch6_r5_test.go --back server/internal/convert/dialers_native_test.go --back server/internal/convert/rule_format_codec_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/convert/legacy.go internal/convert/common_native_test.go internal/convert/mihomo_generate.go internal/convert/batch6_r5_test.go internal/convert/dialers_native_test.go internal/convert/rule_format_codec_test.go; go vet ./internal/convert ./internal/rulefetch ./internal/ippresence ./internal/configgen; go test -run "^$" -count=1 -bench "^(BenchmarkMihomoGenerationProfile|BenchmarkRulesetFormatUnrelatedConfig)$" -benchtime=1x -benchmem ./internal/convert; go test -race -count=1 -timeout=10m -run "^(TestGenerationRetainsAllNodesAndSource|TestN88ReviewByteSamples|TestBatch6R5.*|TestTyped.*|TestCommon.*|TestRulesetFormat.*|TestMihomoReviewEmptyLibraryURLUsesRuleFormat|TestInvalidChainMetadataIsRejectedBeforeConversion|TestConversionFailureCannotSilentlyRemoveChainTopology|TestProviderPreflightGetsLogicalProfileAndFinishedDocument|TestGenerateMihomoDocumentLoadsTheGeneratedText)$" ./internal/convert; go test -race -count=1 -timeout=15m ./internal/rulefetch ./internal/configgen'
```

```text
ok configflow/internal/convert 16.129s
ok configflow/internal/rulefetch 28.147s
ok configflow/internal/configgen 2.940s
```

静态 codec 保护测试更新后单独补跑通过（0.012s）；完整 core 由协调者在 .64 跑，本 agent 未跑完整门禁。

## 最终验收 compare

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e GATE_ONLY=compare -e GATE_LOGS=/home/sunbroke/.cache/rt-tmp/batch6-logs-narrow-final --back server/internal/convert/common_native_test.go -- bash -lc 'set -e; cd server; gofmt -w internal/convert/common_native_test.go; test -z "$(gofmt -l internal/convert/legacy.go internal/convert/mihomo_generate.go internal/convert/common_native_test.go internal/convert/batch6_r5_test.go internal/convert/dialers_native_test.go internal/convert/rule_format_codec_test.go)"; go test -count=1 -run "^TestCommon(CoreHasNoLegacyDependency|LegacyOnlyCallsTypedEntries)$" ./internal/convert; cd ..; cat /proc/loadavg; server/scripts/gate.sh -p 4'
```

```text
self-check: 23/23 PASS
baseline: new=0 stale=0
transport_retries: python=0 go=0
coverage_regressions=0 unmapped_routes=0
== difftest (uncached)
FAIL configflow/internal/api/agents/difftest 282.810s
FAIL configflow/internal/api/config/difftest 85.830s
GATE FAIL: difftest
```

proddiff 起始 load `7.19 3.39 5.34`，结束 `15.35 9.61 7.45`；私有 run 的每次 HTTP 尝试没有 status=0 传输失败。并行比较中 Go POST 的最长时间：OxiDNS 2.279s，Surge 1.471s，Loon 0.598s。

最终日志：Python 597 bytes / 3 物理行；Go 945 bytes / 7 物理行；双方 size_mb=0.00。差 **348 bytes / 4 行**，正好只剩 2 条既有 A18 异常（每条两行）；context canceled 行双方均为 0。两边 3 条 binary-not-found 对齐；实际归一化 logs[2] 也相等。当前绝对字节数比原门禁略长，来自不同 worktree 的二进制文件路径；同一轮两边的额外字节仍精确是 A18 的 348。没有新增 A18/logs 例外。

直接调用方 ippresence 另补：

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -e GOMAXPROCS=4 -- bash -lc 'cd server && go test -race -count=1 -timeout=5m -run "^(TestNativeFormatErrorsRemainUnknown|TestCacheMemoIsKeyedByPathMtimeAndSize)$" ./internal/ippresence'
```

`ok configflow/internal/ippresence 1.373s`。

### 两条父提交可复现的 difftest 失败

1. `TestUpgradesDifferentialAgainstFlask`：130 个 HTTP/Agent 请求及存储 Agent 均一致，唯一 `notifications.json` 字符串不同；解析成 JSON 后两边结构完全相等，只是对象字段顺序不同。
2. `TestSetupFlaskHTTPStateAndAuthentication`：测试断言 missing-owner prepare 必须 503 / 等待首次使用向导模块；实际 400 / 当前没有可用节点，请先读取订阅或选择支持的节点。

在自有、未改动的 detached f9f3105ee worktree 定向复验，二者同样失败：

```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && go test -count=1 -timeout=10m -run "^(TestUpgradesDifferentialAgainstFlask|TestSetupFlaskHTTPStateAndAuthentication)$" ./internal/api/agents/difftest ./internal/api/config/difftest'
```

```text
--- FAIL: TestUpgradesDifferentialAgainstFlask (19.58s)
    upgrades_diff_test.go:596: DIFF notifications.json:
--- FAIL: TestSetupFlaskHTTPStateAndAuthentication (1.51s)
    setup_test.go:57: missing owner prepare: Status:400
```

父提交和本分支的 notifications 差异都另经 json.loads 结构比较确认相等。未改这两个范围外的测试或产品 owner；交由协调者安排。其余 difftest 包通过，包括 HTTPx、subscriptions、rules、rulelibrary、OxiDNS、provider、C31、tasks。synthetic live 因 difftest 阶段失败未启动，不能把整条 compare 说成全绿。

## 交回

分支 `go-fix-batch6-logs`，从指定 f9f3105ee 起步，没有推送，没有修改共享 batch-6 worktree。产品/测试提交：3aa7c917b（取消日志）、d74eb1dc2（安全的传输诊断）、af7315893/f2666fd23（稳定 view 复用）、e5cbeb6be（最终窄 codec 与 R5/分配回归）。本报告和行为说明另有文档提交。

已关闭：P2 整配置重复解码引起的传输重试/超时；P3 取消误记应用异常的日志问题。A18 仅保留原有延期项，没有新基线。剩余事项是上述两条在父提交可复现的 difftest 与协调者的 .64 core；本任务没有未声明的行为降级。
