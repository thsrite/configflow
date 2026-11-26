# N8-10 agents core-1

后续 core-2 边界、余量与定向验证见 [n8-10-core-2.md](n8-10-core-2.md)。本报告的 core-1 verified 不表示 N8-10 五步全部完成；整体状态仍为“部分完成”。

状态：core-1 **verified**（fresh Opus HR 复核，见 parity/review-n8-10-core.md；high 0，2 条修复已提交在 go-opus-review-n8-10-core）。
N8-10 整体仍为“部分完成”，剩余归 core-2。不是完整 N8-10 S pass。
协调者已批准拆分：本分支交 core-1；随后从本 tip 建 go-n8-10-core-2 继续剩余核心。
分支 go-n8-10-core，基于 go@690e97995，已变基到 go@08d8e6ff1。
新增基线只有 HANDOFF / O12 ledger 文档，产品字节与验证树一致。
产品基础提交 702b5b289；补充产品／metadata 提交 5b0afe1f7。
未 push、未开复核 agent、未跑完整 gate、未接触生产。

## 已完成的边界

- registry.go：ReadAgents / ReadAgent / EditAgents 直接接 repo 的缓存 View / typed transaction。
  单 Agent 只复制目标记录；轻读取先 Clear 两类 history；inspect 在 projection 前看原记录。
  旧 GetAgentByID / agentsInspect 为兼容出口，仍给 monitor/upgrades/API 返回旧记录。
- runtime_state.go / runtime_model.go：运行时只持有 owned model.Agent runtime projection。
  新注册时间优先、部分心跳继承、delete generation mark、共享 data-dir 状态保持。
  ObserveHeartbeat 每次只定位缓存中的目标记录并复制 runtime 字段；只有版本变化才持久化。
  runtime_legacy.go 是旧诊断、测试 seam、metrics callback 的单次转码边界，不复制状态机。
- prepared.go：typed deployment map 写入 server-owned prepared content / validation receipt；
  保留 profile/service、submitted/preparing 和任务所属检查。原始 identity DTO 严格 string，
  数字/null 的 model 字符串投影不能成为 ID、HA group/peer/profile 或部署所有权证明。
- DNS follow / HA：读与 owned-field 写事务接 typed registry。删除绑定／HA 字段仍用 Clear，
  未改字段保留 R1/R2/R5 原字节。deploy 的 NewTypedTarget 是必要的旧 Record 接线出口。
  空 HA journal 不算操作，非空未知 journal fail closed，ready 保持 reservation。
  空 enable 对象不算 enabling；坏类型的保留 intent 不能授权编辑 reserved pair。
- metrics：AddSample(ctx, raw) 成为唯一 append owner；生产 heartbeat hook 保留 ctx。
  原 AddMetrics 只转码到该 owner。创建目录／append 前检查 shutdown；请求断开仍完成。
- rulesync：settings/state 写入为 typed transaction；Mihomo / OxiDNS 候选走 native generator。
  Source.ViewConfig 对不可变 captured snapshot 只解码一次，重复读取零分配；不用于心跳。
  本步不再调用 oxidns.NewProfile / ToJSONX。model.DecodeConfig 是共享捕获快照 codec。
- 旧 runtime.sqlite3 导入保留来源 ctx：入口、逐文件删除、seed 前检查；新 Manager 在来源
  shutdown 后不发布。SQLite 读取/旧格式 helper 本身仍列入后续 S pass，未伪称已删除。

## 剩余动态核心与 adapter

- manager.go 的注册、编辑、删除、改绑，以及 registration.go 的旧 Record 调用边界；
  core-2 必须保持 O9-H2 的同事务消费、只存 hash、403 和 authenticated re-registration 语义。
- deployments.go 的部署状态机；remote.go / client.go 的动态响应与请求 facade；
  diagnostics.go / operation_events.go / messages.go / version.go / pydecode.go / pytime.go。
- hooks / record alias / presence / public metadata 的旧出口；runtime.sqlite3 旧读取与私有格式 helper。
- ipv6 adapter；rulesync 剩余 service/worker/task-row 的旧 Record 接线；metrics/traffic/trafficstore
  的其余动态报告、统计和私有标量 helper。core-1 没有宣称这些已完成 S pass。
- monitor、upgrades、installer、api/agents 为原计划后续步骤，没有实施其 S pass。
- oxidns legacy 仍不能删：api/agents/client_tools_adapter、configgen BuildZIP、proddiff 工具和
  oxidns 包内旧 rules callback 还在使用。没有为了清 import 而隐藏这些调用方。

## 风险类与独立期望

- 第四常设项：Detach 后入口检查；repo callback、runtime/metrics 内存提交、逐文件 unlink、
  legacy seed 前再次检查。真实 Runtime Start/BindRequest/Shutdown 覆盖晚取消；disconnect
  正控覆盖版本/metrics/导入写入继续完成。repo 自身最终 commit 检查保留。
- 凭据统一 internal/redact。metrics 警告仅在错误路径收集原记录 private proof 凭据，
  以 raw JSON 进入 visitor，再用 logx 限长／去控制字符；不在每个心跳上准备全历史脱敏。
  回归曾抓到直接传 typed struct 不被 visitor 遍历的问题，已修正；正常用户名保留。
- 设备请求编码未修改；RawClientRequestsAndMalformedReplies、DNSRequestsCarryExactDeploymentQuery
  及原子包回归比较实际 body/header/query 字节。没有新增覆盖生产 timeout 的 fake。
- R5：automation enabled 只有 omitted 默认 on，显式 null/坏类型 off；7 个 exact raw input
  与独立 bool 期望同一行绑定。身份、HA、null/zero/Clear 的期望也为显式 raw 行；没有通配豁免。
- D2：没有迁移任何持久身份编码。原 corpus / baseline / native expectation 索引未改。
  12 份 pyref 共 18,444 个 exact input/output entries 完全不变，venv/input 元数据也不变；
  仅 agents.go、agentha.go、member.go、新 config_decode.go 四个来源 SHA 变化。
  新来源在 18 个声明组补入；无 product 边界豁免。真实 check → record metadata → check，
  sources audit 与回放均通过；不是用 Go 输出重造 Python 期望。

## 性能与负控

真实 warmed repo View + ObserveHeartbeat，不是 mock registry：

| 无关配置 | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| 0 | 132485 | 19190 | 287 |
| 2 MiB | 123273 | 19048 | 286 |

HA guard 的 captured payload 为 0 / 2 MiB 时均 0 allocs/op；Source cached typed view 也为 0。
修正了 benchmark seed 漏 profile 的 setup 失败，并检查 N 非零，避免 0 B/op 空通过。
负控仅临时在 ObserveHeartbeat 加整份 document marshal/decode，原上界测试即失败：
0=11,556,031 B/op，2 MiB=156,018,988 B/op；远端 trap 已恢复，未提交 mutation。
本基准覆盖 native heartbeat owner；API 的旧鉴权/响应 DTO 迁移仍属于后续步骤。

## 定向验证

以下均仅在 .32/.64 经 rt 执行，GOMAXPROCS=4。本机仅编辑、git、读取／审阅数据。
没有新增依赖。第一次跨包回放因新来源未声明而失败，已通过来源审计和 metadata 刷新关闭，
未重复已通过的其他包。较晚的 HA/关机改动另做 focused race / HTTP / fuzz。

.64，gofmt/vet 静默，完整 agents 子树及直接 model/deploy caller race：

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; export GOMAXPROCS=4; test -z "$(gofmt -l internal/agents internal/model internal/deploy)"; go vet -p 4 ./...; go test -race -p 4 -count=1 -timeout=25m ./internal/agents/... ./internal/model ./internal/deploy'

Exit 0。agents 518.695s；dnsfollow 891.768s；dnsha 408.038s；ipv6 19.211s；
metrics 10.138s；monitor 247.896s；rulesync 141.780s；trafficstore 1.210s；
upgrades 42.493s；model 8.593s；deploy 387.472s；agentfake / 两个子 difftest 包亦通过。

.64，最终 presence/identity/shutdown/HA guard 变更的补充检查：

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; export GOMAXPROCS=4; test -z "$(gofmt -l internal/agents internal/model internal/deploy)"; go vet -p 4 ./...; go test -race -p 4 -count=1 -timeout=15m -run "Typed|HARecordPresence|PreparedOwnership|HAValidation|MemberIdentity|RawClientRequestsAndMalformedReplies|DNSRequestsCarry|Pair" ./internal/agents ./internal/model ./internal/agents/dnsha ./internal/agents/dnsfollow ./internal/agents/rulesync; go test -p 4 -count=1 -run "^$" -fuzz "^FuzzTypedRuntimeProjection$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：agents 33.143s、model 1.019s、dnsha 103.916s、dnsfollow 28.565s、
rulesync 5.411s。fuzz 500,449 executions，PASS 61.016s，包含 HA presence 对原生 JSON 的
property、pointer removal/Clear 和输入不变。此前 legacy importer focused race 68.694s 亦通过。

.32，全仓跨包名字过滤；除上面已关闭的 DNS metadata 失败外所有包通过：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -p 4 -count=1 -timeout=25m -run "Corpus|Reference|Golden" ./...'

DNS 最终修复后的同名过滤退出 0：mosdns 25.673s、oxidns 27.591s。所有相关 pyref 组的真实
check 最终 equal，audit 输出 “3 registered call sites, 2 scopes; no unregistered process constructors/live oracles”。
复现 metadata 刷新／检查的 exact selector：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 PYREF_MODE=check go test -v -p 2 -count=1 -timeout=25m -run "^(TestCorpus|TestRecordedPythonTests|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestN73SharedUpstreamMetadataMatchesPython|TestNativeRuleTextAgainstPython|TestBoundary.*(Recorded|Corpus)|TestC05ReviewSettingsReferences|TestC05ReviewRecordedNormalization|TestStoredLegacySettingsAgainstPython)$" ./internal/mosdns ./internal/oxidns'
    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go run ./tools/pyref -scopes server/internal/mosdns,server/internal/oxidns && go test -p 2 -count=1 -run "Corpus|Reference|Golden" ./internal/mosdns ./internal/oxidns'

.32，原全 Agent HTTP differential：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && CONFIGFLOW_DIFF=1 go test -p 3 -count=1 -timeout=25m ./internal/api/agents/difftest ./internal/agents/dnsfollow/difftest ./internal/agents/dnsha/difftest'

Exit 0：API 315.616s、DNS follow 42.993s、DNS HA 31.986s。最终 guard 的补跑也退出 0：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && export GOMAXPROCS=4 && CONFIGFLOW_DIFF=1 go test -p 3 -count=1 -timeout=15m -run "RuleSyncHTTPAgainstFlask|HTTP|Flask" ./internal/api/agents/difftest ./internal/agents/dnsfollow/difftest ./internal/agents/dnsha/difftest'
    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 CONFIGFLOW_DIFF=1 go test -p 2 -count=1 -timeout=15m -run "^TestPythonRoutesAndArtifacts$" ./internal/agents/dnsfollow/difftest'

API 265.566s、DNS HA 22.403s；第一个名字过滤没有命中 DNS follow 测试，第二条明确补跑
其真实 Flask／Go route/artifact 比较，PASS 22.231s，不将 no-tests 当成验证通过。

## 覆盖映射与新增文件

既有 Python 对照／Go 测试保留，未因本步删除旧期望或跳过既有用例：

| Python 测试 | Go 边界 |
| --- | --- |
| test_agent_runtime_state / version_sync / light_reads | agents/runtime_state_test、registry_native_test；API differential |
| test_agent_content_preparation / deployment_writes / deployments | prepared/registry 回归、原 deployments 测试、API publish differential |
| test_dns_follow | dnsfollow 原 unit/loopback + TestPythonRoutesAndArtifacts |
| test_oxidns_ha / publish / identity / compact_identity / guards | dnsha 原 runtime/fault/ownership/edge + HA HTTP differential |
| test_agent_metrics_history | metrics 原历史测试 + sample_test |
| test_agent_rule_sync | rulesync 原 service/worker + RuleSyncHTTPAgainstFlask |

新增源码 registry.go 284 行、ha_model.go 107 行、runtime_model.go 26 行、
runtime_legacy.go 124 行、metrics/sample.go 56 行、model/config_decode.go 63 行。
新增测试 registry_native_test.go 507 行、sample_test.go 35 行、source_model_test.go 54 行、
config_decode_test.go 44 行、presence_test.go 45 行。无调试／临时 testdata 文件。

## 交回

core-1 的授权边界实现完成，整体 N8-10 状态仍为“部分完成”，无本步未关闭的已知失败。
产品提交 702b5b289、5b0afe1f7；本报告所在提交为最终交回 tip。
core-2 从本分支最终 tip 创建，继续上述动态核心；core-1 HR 修复在 core-2 或叠批树接纳。
