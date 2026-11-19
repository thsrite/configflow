# N8-10 agents core

分支 `go-n8-10-core`，基于 `go@690e97995`。协调者已批准拆为 core-1 / core-2；本报告为 core-1，
状态 **部分完成**，不是完整 S pass / verified 声明。不创建复核 agent、不 push、不跑完整 gate。

## 当前已实现

- typed `ReadAgents/ReadAgent/EditAgents`，通过 repo 缓存 View 读记录；单 Agent
  读取只复制目标记录，轻读取先去除两类 history。原始 inspect 在 projection 前运行。
- runtime 内部持有 owned `model.Agent` runtime projection，只有兼容诊断接口转码；
  newer-registration 优先、heartbeat merge、delete generation mark 维持原语义。
- `ObserveHeartbeat` 每次只读取缓存视图中目标 Agent 的 runtime 字段；版本变化才
  typed 写事务。新增无关配置 0/2MiB 分配基准与上界测试，待最终记录数字。
- prepared content 通过 typed 部署 map 写入；原 profile/service、submitted/preparing
  条件保持；原始 identity 用严格 DTO 读取，R5 不能成为所有权证明。
- DNS follow / DNS HA 的读取与 owned-field 事务接到 typed registry。
  deploy 尚未 S pass，其 Record 仅在 `NewTypedTarget` 这个接线边界转码。
- metrics 原生 `AddSample(ctx, raw)` 在内存 append 前再次检查来源 shutdown；
  生产 heartbeat hook 保留 ctx；旧 AddMetrics 仅转码到同一 append owner。
- rulesync 的 state/settings 写入为 typed 事务；候选生成接原生 Mihomo / OxiDNS；
  新增共享 `model.DecodeConfig` 只用于捕获的 receipt/snapshot；`Source.ViewConfig`
  按 immutable Source 缓存一次，重复生成不重复解码，绝不用于每次心跳。

## 仍未完成

注册/编辑/删除业务、部署状态机、远端调用/诊断/events 仍有旧动态实现，不能因增加
native registry 就宣布迁移完。其余 ipv6、rulesync/metrics/traffic 的剩余 adapter、
私有 Python scalar/时间/Unicode 仿真仍待按原任务迁移。monitor/upgrades/installer/
api-agents 未实施 S pass；只保留必要接线。

oxidns legacy 不能删：除了 api/agents/client_tools_adapter.go，configgen BuildZIP、
proddiff generator/tooling，以及 oxidns 包内暂存 rules callback 也还在使用。
本步 rulesync 已不再调用 NewProfile / ToJSONX。

## 风险类处理

- 关机／请求断开：typed 写入口 Detach 后检查 ctx；事务 callback 和 runtime/metrics
  append 点再次检查；repo 仍执行自己的最终 commit 检查。真实 Runtime 回归已增加。
- 凭据：本步不复制新的脱敏规则；metrics 警告只在错误路径收集原记录私有凭据，
  以 raw JSON 进入 internal/redact，既不泄漏 proof 密码，也保留正常用户名。
  新回归最初暴露 typed struct 未经 raw JSON 进入 visitor 的泄漏，已修正。
- 设备原始请求：未改变 client 请求编码；最终跑现有 raw body/header 回归。
- R5/D2：旧冻结语料、baseline、Python 期望均未改。automation enabled 只有
  omitted 默认 true；显式 null/错误类型为 off（R5），7 条输入和独立 bool 期望逐行绑定。
  新增 copy 回归断言 null/坏类型/
  原数字文本和未知字段保真；没有引入新的 persisted identity 编码。
- 性能：cached View 只定位 Agent，runtime projection 从 MemberJSON 取单字段；
  MemberJSON 避免 history/整份配置编码；原始 snapshot decoder 只在候选生成使用。

## 已运行（.32，非最终验证）

- 原 core `go test -count=1 ./internal/agents`：PASS 118.912s。
- typed registry/heartbeat/preparation 定向：PASS 4.789s。
- DNS follow / DNS HA：PASS 174.856s / 95.750s。
- 一次 rulesync 编译发现 HA Enable 为指针、Settings 为值的接线错误；修正后 PASS 47.677s。
- 全部命令通过 `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && …'`；
  gofmt 也在服务器执行，格式化文件取回。本机没有运行测试／构建／vet／lint。

## 下一步

协调者已接受 core-1：registry/heartbeat/prepared ownership、DNS follow/HA 接线、
metrics ctx、rulesync 接线。core-1 定向通过后交回，由协调者派 fresh Opus HR 复核。
随后从 core-1 tip 新建 go-n8-10-core-2，迁移其余核心，保持 O9-H2 的凭据原子性、
只存 hash 和 403 语义；core-1 复核修复在 core-2 或叠批树接纳。
最终交回前记录 race、跨包 Corpus|Reference|Golden、agents differential、fuzz 60s、
分配数字、gofmt/vet、分支及提交 ID。

## 分配证据与负控（.32）

修正了 benchmark seed 漏 profile 的初始化错误，新增 `N != 0` 检查，防止 setup Fatal
产生 0 B/op 的空通过。真实 warmed repo View/heartbeat（不是 mock owner）：

```text
BenchmarkTypedHeartbeatUnrelatedConfig/empty-12  132485 ns/op 19190 B/op 287 allocs/op
BenchmarkTypedHeartbeatUnrelatedConfig/2MiB-12   123273 ns/op 19048 B/op 286 allocs/op
```

临时只在 ObserveHeartbeat 插入 `repo.View → model.Marshal(document) → json.Unmarshal(document)`，
未修改测试，原上界测试失败：0=11,556,031 B/op，2MiB=156,018,988 B/op，
`heartbeat cost scales with unrelated configuration`。远端 trap 已恢复原文件；不提交 mutation。
