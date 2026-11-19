# N8-10 metrics / trafficstore native 服务

任务分支 `go-n8-10-metrics-native`，起点 `5c5998e86`，batch-13 后变基到 `go c8208b60f`（包含 `cf5ec02e7`）。按协调者补充授权切换 api/agents；未改公共 core API、依赖或持久格式；不 push、不跑完整门禁。

## 实现边界

- metrics 报告精确键解码、typed query DTO、native 计数器（int64 → big.Int；兼容 SQLite REAL）、typed 保留天数写入。
- A04 原始 JSONL 样本、typed summary/stats/trend、任务运行/rows/settings；生产 `pyvalue.go` 删除，旧签名迁到仅 codec/delegate adapter。
- trafficstore `NewNative/GetNative/DecodeReport/Add/LoadDocument/StatusJSON`；已发布文件结构、未知成员与数字文本保留，LRU 仍只缓存有限 metadata。旧 Store 的测试 I/O hooks 仅留在兼容 facade。
- owner write 接收 ctx、Detach、在内存/SQLite/file commit 前检查 shutdown；心跳使用精确 Select，不解析整个配置。

## 协调者裁定（2026-11-19）

A04 坏组件/坏数值逐条跳过，HTTP 200，按对象/字段节流 warning，走 redact。
A05 present wrong-type days 整文件拒写；坏 hosts 桶只拒绝该 Agent 的该日期，上报失败保留原文件。
列表读取隔离损坏对象；拒写 warning 节流，不重试、不影响独立心跳。
新期望逐 fixture 保留原文本并 SHA256 绑定，旧 corpus/pyref entries 不改。
恢复使用现有 `POST /api/agents/<id>/domain-discovery/clear` 的 DeleteContext；坏日期到期也会随其他正常日期的成功上报清理。

## 初步验证

仅测试服务器 .64：`go test ./internal/agents/metrics ./internal/agents/trafficstore -count=1` 通过（4.583s / 0.221s）。后续定向检查见以下记录。

## API 切换接口

- SQL history：`DecodeTrafficReport(raw, at) *CleanedTraffic`；`AddTraffic(ctx,id,report) error`；`QueryTraffic(ctx,id,range,by,at) (*TrafficQuery,error)`。
- 设置：`TrafficRetention(*model.System) (int64,error)`、`SetTrafficRetention(ctx,manager,int64)`；`SupportedTrafficVersion(string,minimum)`。
- discovery store：`DecodeReport(raw) (*Report,error)`、`GetNative(ctx) (*NativeStore,error)`、`Add(ctx,id,*Report,at) (*Status,error)`。
- A04：`Samples/Summarize/TrafficStatistics/TrafficTrend`；`FlushNow/SaveOnShutdown/LoadSavedHistory/BuildRows/SetFlushInterval`。

## Owner 验证增量（2026-11-19）

提交 `cc28d45b9` 为初版；本轮补上锁等待后取消检查、真实 native 写文件的 Python 双向互通、旧文件缺失可选计数器的读取兼容和可变 host/route/port 索引（保留原顺序，避免每次 item 更新复制全部 host 集合）。

执行与最后输出：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/agents/metrics internal/agents/trafficstore)" && go vet ./... && go test -count=1 -race -p 4 ./internal/agents/metrics ./internal/agents/trafficstore ./internal/agents ./internal/api/agents ./internal/discovery ./internal/deploy ./internal/configgen ./internal/server ./cmd/configflow'
```

metrics 16.306s、trafficstore 1.629s、api/agents 471.431s、deploy 399.639s、configgen 2.550s、server 15.190s、cmd/configflow 121.164s PASS。agents 到默认 10m 超时，正在执行不相关的 `TestOptionalAgentProfilesIgnoreOversizedUnrelatedConfig`（16MiB 配置 benchmark；无 race 报告），不是功能断言失败。discovery 两项失败指出读取把“缺失可选计数器”误当成损坏；已修复，保留缺失字段，仍拒绝 present wrong-type，不改原 consumer 测试。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -w internal/agents/metrics/*.go internal/agents/trafficstore/*.go && go test -count=1 -race ./internal/agents/metrics ./internal/agents/trafficstore && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/discovery/difftest && go test -count=1 ./tools/pyref -run TestAuditRepository && go vet ./...'
```

metrics 24.516s、trafficstore 1.892s、discovery/difftest 14.313s、pyref 0.845s PASS；vet 无输出。Python live oracle 条目未改。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/agents/metrics/*.go internal/agents/trafficstore/*.go && go test -count=1 -race ./internal/agents/trafficstore ./internal/discovery && go test -run "^$" -bench "^BenchmarkNativeDomainReport$" -benchmem -benchtime=1x ./internal/agents/trafficstore && go vet ./...'
```

最后一轮 trafficstore 1.701s、discovery 17.072s PASS，vet 无输出。最大 2000 items 两种形状：2000 hosts 74.0ms / 50,588,632 B / 983,393 alloc；同 host 2000 routes 45.2ms / 30,606,880 B / 585,468 alloc。批量索引避免随同一文件的 host 数反复复制；未设置或放宽分配阈值。

另运行 `.64 go test -count=1 -race -run "Metrics|Heartbeat|Runtime|Cancellation" ./internal/agents`，PASS 76.538s。原 `TestBatch10HeartbeatAllocationCeilings` 在 .32 原样 PASS 5.998s。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test ./internal/agents/metrics -run "^$" -fuzz "^FuzzNativeTrafficReport$" -fuzztime=60s && GOMAXPROCS=4 go test ./internal/agents/trafficstore -run "^$" -fuzz "^FuzzNativeDiscoveryReport$" -fuzztime=60s && go test -count=1 ./internal/agents/metrics -run "Corpus|Reference|Golden" && go test -count=1 ./internal/agents -run "^TestBatch10HeartbeatAllocationCeilings$"'
```

两个 fuzz PASS：metrics 1,203,915 exec / 60.443s；trafficstore 787,799 exec / 61.016s。A04/A05 Reference（含 Unicode17 原绑定）PASS 1.548s。

## API 差分与后续顺序（协调者补充任务）

`.32 CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest`：相关 `TestDifferentialAgainstFlask` 234 requests 有且只有 5 项差异，其他 API 差分组全部通过：

1. metrics summary odd 6h：坏字符串样本隔离，500→200。
2. review traffic report edited no items：坏 status/hosts 拒写，200→500。
3. review traffic report edited keep version：同上，200→500。
4. traffic/agent-edit.json：拒写保留原文件，Python 修改文件。
5. traffic/agent-traffic.json：仅 JSON 对象字段顺序不同，解析后值相同。

协调者确定：等待 batch-13（upgrades + api/agents）先合入 `go`，随后本分支 rebase 到新 `go`；由本分支切 `api/agents/traffic.go` 到上述 native 接口并删对应无生产调用 adapter，给前四项加精确 SHA/fixture 绑定比较器，第五项改结构比较。该步骤现已完成，详见下节；原 owner 提交变基后为 `e5333b76e`、`6bc803ebd`、`df2ab27a4`。

### SQLite 损坏值写入防护增量

收尾检查发现 SQLite 的动态列类型会在 `SUM` / `up + excluded.up` 中把人工写入的 TEXT 静默当零；仅把 query DTO 换 native 不能阻止后续 compaction 覆盖坏记录。因此同一 owner 增加原 SQL 扫描内的类型/有限性聚合证据，以及 conflict-update WHERE 防护 + affected-row 校验；无新表、无额外全库扫描，正常 INTEGER→REAL 溢出累计仍兼容。非有限 REAL 不能再次绑定写入。坏值/REAL SUM 溢出回滚原事务、恢复 pending，不改已有行；现有 per-Agent clear 后健康 Agent 的 pending 能再次保存。

新增真实 SQLite 回归覆盖 usage/targets compaction、同主键坏计数器 upsert、REAL SUM 溢出及 clear 恢复。`.32 rt -- bash -lc 'cd server && gofmt -w internal/agents/metrics/*.go && go test -count=1 -race ./internal/agents/metrics && go vet ./...'` 最后 PASS metrics 24.118s，vet 无输出（含原 A04/A05 reference）。人工数据行为差异已登记 ledger。

owner 提交：`cc28d45b9`、`2e18d081b`，以及本段对应的 SQL 防护提交；API 集成已在 batch-13 后完成。

## batch-13 后 native HTTP 集成（2026-11-19）

- `git rebase go` 完成，基底 `c8208b60f` 含 `cf5ec02e7`，行为变更文档保留双方条目。
- `traffic.go` 全部读取精确 JSON/native DTO：上述 report/write/query/retention 接口已接入，带 ctx 的 Add/AddTraffic 避免旧 bg facade 绕过 shutdown。HTTP state 用 typed envelope 嵌入 TrafficQuery；不改缺失 service_type 默认，null/错型仍拒绝。`metrics.go` 的 history/summary 同步接到 Samples/Summarize。
- 集成发现原成功语料含重复 port 字段。此前 jsonx 解码先取最后成员；现在 DecodeReport 用 jsondecode 的 native JSON last-complete-member 规则先规范 Agent 对象（json.Number 保留数值文本），再进入 Fields。存量文件依然严格，不放宽坏文件策略。新真实 Add 回归覆盖重复 schema/port/fail-kind。
- 无生产调用的 metrics `history/stats/flush/traffic_legacy.go` 全部移为 `_test.go`，生产 metrics 不再导入 jsonx/jsonresp/pyvalue；旧 AddMetrics hook 仅 json.Marshal→AddSample 委托。对应 API、cmd/configflow 和 tasks/difftest 测试改用 native 接口，原断言不变。trafficstore 无生产调用的 CleanReport/AddReport/SplitRouteKey/CachedStatus 及 decodeCleanedReport 迁到测试文件；A05 跨包 corpus harness 改用 native decoder/Add + 测试 codec，比对原期望。
- 四行 `testdata/n810_metrics_http_native.jsonl` 每行 original 原文 SHA256 绑定完整请求、旧 fixture 源码和 Python 结果；live oddMetrics/edited file seed 另做精确验证。HTTP native body/status/header 严格检查；文件拒写逐字节保留（只归一两处动态日期）。变更请求、原行 SHA、无关 native 错误、数组/计数器变化有负控。第五个 agent-traffic.json 仅按 JSON 结构比较，旧语料和 pyref entries 未变。

### N8-11 留存 facade 清单

trafficstore `legacy.go` 的 Store/New/Get/Load/LoadStatus/StatusOf/Delete（及 OSError）仍有调用方：

- `internal/discovery/legacy.go`：Load/LoadStatus。
- `internal/configgen/generate.go`、`internal/deploy/adapter.go`、`tools/proddiff/generator/main.go`：StatusOf→subs 的旧 core-version helper。
- `internal/api/agents/client_tools_adapter.go`：仅 DeleteContext，复用嵌入 owner；可在 N8-11 直接换 GetNative。

这些仅转码/委托；NativeStore 不依赖这些 facade，生产上报不走其测试 WriteJSON hook。无 legacy metrics 生产 adapter 留存。

### 新集成定向验证

`.32`：`go test -count=1 ./internal/api/agents/difftest -run "^TestN810MetricsHTTPPolicyBinding$"` PASS 0.014s；`CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest -run "^TestDifferentialAgainstFlask$"` 最后输出：

```text
same traffic_history.sqlite3 (23 lines)
same traffic/agent-edit.json → bound native refusal preserves seed bytes
same traffic/agent-traffic.json (985 bytes)
234 requests + stored records + DATA_DIR files + files after stop, 0 differences
--- PASS: TestDifferentialAgainstFlask (8.92s)
ok configflow/internal/api/agents/difftest 8.932s
```

协调者已复核原整包 agents race 默认 10m 超时来自包本身耗时（基底 771s、本分支 729s），非本改动回归。本轮整包 race 使用 `-timeout=30m`，全部通过，输出见文末。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -count=1 ./internal/agents/metrics -run "Corpus|Reference|Golden" && CONFIGFLOW_DIFF=1 go test -count=1 ./internal/discovery/difftest ./internal/tasks/difftest && go test -count=1 ./tools/pyref -run "^TestAuditRepository$" && GOMAXPROCS=4 go test ./internal/agents/trafficstore -run "^$" -fuzz "^FuzzNativeDiscoveryReport$" -fuzztime=60s && go test -count=1 ./internal/agents -run "^TestBatch10HeartbeatAllocationCeilings$"'
```

最后输出：metrics Reference PASS 1.540s、discovery/difftest PASS 10.155s、tasks/difftest PASS 9.952s、pyref PASS 0.822s；trafficstore 更新解码器后的 fuzz PASS 61.023s / 355,791 exec；原心跳阈值 PASS 6.306s。metrics 的 fuzz 在 owner 轮已跑 60 秒且解码器未改。无阈值放宽。

### 最终人工 REAL 查询溢出回归

收尾发现有限 REAL 汇总可使 up/down 溢出，排序用的 up+down 进一步成为 NaN，不能传给 big.Float。新增真实 SQLite 查询测试（两个极值链路 + 健康 source），missing 总值放在数值总值后、再按 key 稳定排序；DTO 继续按 native 既有策略输出 null，并通过现有限流器记 aggregate 告警，原始数据库行不变。只涉及人工极值，不改变机器报告数值限制。一次新测试因把 SQLite INTEGER affinity 的健康 REAL `1.0` 计入 REAL 行数而失败，修正为检查全部原行数；没有改原期望或语料。

`.32 rt -- bash -lc 'cd server && gofmt -w internal/agents/metrics/native_service_test.go && go test -count=1 -race ./internal/agents/metrics && go vet ./... && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest -run "^TestDifferentialAgainstFlask$"'` 最后 metrics PASS 21.069s，vet 无输出，HTTP 主差分再次 234 requests / 0 differences。大包并行 race 的快照为 `2c353f574` 的代码；最后这一局部查询防护另用本命令在最终代码上验证，没有重复无关长测。

## 最终交回

最终代码提交 `b7db27f14`（HTTP 集成 `2c353f574`；验证文档 `71a67cea8`），加本报告收尾提交。分支 `go-n8-10-metrics-native`，工作区干净，未 push。owner、HTTP 接口切换、四行 SHA 绑定与第五项结构比较均完成，可进入 HR 复核。N8-11 仅留上述仍有生产读取调用方的 trafficstore facade。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w cmd/configflow/service_test.go internal/agents/metrics/errors.go && test -z "$(gofmt -l internal/agents/metrics internal/agents/trafficstore internal/api/agents/traffic.go internal/api/agents/metrics.go internal/api/agents/traffic_test.go internal/api/agents/difftest/n810_metrics_policy_test.go internal/api/agents/difftest/diff_test.go internal/api/agents/difftest/traffic_test.go internal/tasks/difftest/c07_test.go cmd/configflow/service_test.go)" && go vet ./... && go test -count=1 -race -timeout=30m -p 4 ./internal/agents/metrics ./internal/agents/trafficstore ./internal/agents ./internal/api/agents ./internal/discovery ./internal/deploy ./internal/configgen ./internal/server ./cmd/configflow ./internal/tasks/difftest'
```

最后输出（退出码 0；gofmt/vet 无输出）：

```text
ok configflow/internal/agents/metrics 16.799s
ok configflow/internal/agents/trafficstore 1.841s
ok configflow/internal/agents 861.301s
ok configflow/internal/api/agents 541.365s
ok configflow/internal/discovery 15.049s
ok configflow/internal/deploy 417.753s
ok configflow/internal/configgen 2.525s
ok configflow/internal/server 15.477s
ok configflow/cmd/configflow 121.082s
ok configflow/internal/tasks/difftest 1.017s
```

最后局部查询防护在最终代码上追加 metrics race/vet/HTTP 主差分（见上），最后主差分 PASS 8.724s、234 requests / 0 differences。未跑完整门禁，未改依赖、持久格式、旧 corpus、Python reference 或 pyref registry。
