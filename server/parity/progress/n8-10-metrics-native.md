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
