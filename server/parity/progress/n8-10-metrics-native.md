# N8-10 metrics / trafficstore native 服务

任务分支 `go-n8-10-metrics-native`，起点 `5c5998e86`。不改 api/agents、公共 core API、依赖或持久格式；不 push、不跑完整门禁。

## 实现边界（进行中）

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

仅测试服务器 .64：`go test ./internal/agents/metrics ./internal/agents/trafficstore -count=1` 通过（4.583s / 0.221s）。最终定向 race/vet/reference/difftest/audit/fuzz 尚待执行。

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

协调者确定：等待 batch-13（upgrades + api/agents）先合入 `go`，随后本分支 rebase 到新 `go`；由本分支切 `api/agents/traffic.go` 到上述 native 接口并删对应无生产调用 adapter，给前四项加精确 SHA/fixture 绑定比较器，第五项改结构比较。**这一步尚未开始，不能把 owner 完成称作整个补充任务完成。**
