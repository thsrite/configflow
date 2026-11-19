# N8-10 agents core-2

状态：进行中。分支 go-n8-10-core-2，基于已交回的 core-1 tip
7a3f1da4ebd606389c0142cf6f9134bc970fe0b5。core-1 报告见 n8-10-core.md；
其 HR 复核结论 verified、无 high；复核报告见 review-n8-10-core.md。
已 cherry-pick ec7c71865 / e871b2fe3 为 c2bd2fd6b / 3dd984086，保留原始
stored identity string 证明和 heartbeat version/config_version 原始 JSON 比较修复。

## 当前顺序

1. 先将 O9-H2 admission / proof matching 迁到 typed claim / model；旧 Record 仅在
   registration_legacy.go 转码。创建／消费同事务、hash-only、403、可选 proof 消费、
   existing Agent token 重注册均不得弱化。Profile existence 用缓存 typed View。
2. 注册／编辑／改绑／删除主体迁到 model.Agent transaction；保留部署、升级、DNS/HA
   busy guards 和 runtime overlay，不把 R5 零值当 identity proof。
3. deployments、remote/client、diagnostics/events/messages/version/time 的动态核心；
   ipv6、rulesync、metrics/traffic 残留 adapter / 私有 scalar helper。monitor/upgrades/
   installer/api-agents 的 S pass 仍是原计划后续步骤，只做必要接线。

## 验证约束

只在 .32/.64 经 rt 做定向 race、相关 Corpus|Reference|Golden / differential、新 fuzz 60s、
gofmt/vet；不跑完整门禁。本机只编辑／git／轻量阅读。不得改旧期望迁就实现，
R5/D2 新期望必须携带 exact raw 输入；设备请求比较真实 body/header 字节。

## 已实现的第一块

- registration.go 不再导入 jsonx。准入接受 typed registrationClaim，验证、profile
  existence、same-transaction one-use consumption 只有一个实现；旧 Record 入口在
  registration_legacy.go 只做 exact-key DTO 转码。profile existence 用 cached typed View，
  不复制／解码配置。
- O9-H2 发号逻辑不变：32-byte 随机值、持久化 SHA-256、24h、100 个服务身份及淘汰规则。
  消费仍修改外层注册事务的 system working copy，失败不会消费；hash 恒时比较。
  existing-token re-registration、JWT+有效 proof、existing token+有效 proof、匿名 403 及
  并发／重启／shutdown／disconnect／备份拒绝复活，原 O9 回归全部保留并通过。
- 不把 R5 string/zero 投影当凭据身份；original stored string token 类型单独检查，
  optional null/错类型不能成为 proof。protocol 数字 port 接受 8080 / 8080.0 / 8.08e3，
  不接受 JSON true 代替端口 1（旧 jsonx 的 Python bool/int 相等），新增真实 HTTP 403
  + proof 未消费 + numeric port 1 成功的独立回归。
- encoding/json 的 case-insensitive tag 匹配不能改协议字段名：先投影精确键名，
  Name/PORT/Profile_ID 等未知别名不覆盖 name/port/profile_id。新增 exact-input table 和
  fuzz：未知 case alias 不改变 claim，且 request 输入不变。

准入第一块提交 a6fab3b42；注册／编辑／改绑／删除主体已进一步迁移（见下文）。
deployments/remote/diagnostics 等仍待迁移，整体仍是部分完成。
没有修改旧 corpus/baseline/期望；没有新依赖、没有 push、没有复核 agent。

## 定向证据（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/agents/registration.go internal/agents/registration_legacy.go internal/agents/registration_native_test.go && GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents && GOMAXPROCS=4 go test -race -p 4 -count=1 -run "RegistrationClaim|RegistrationSessionR5" ./internal/agents && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "^TestO9" ./internal/api/agents'

Exit 0：agents 1.022s；API O9 77.821s（此前两次 O9 分别 73.284s / 88.967s）。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/registration.go internal/agents/registration_fuzz_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzRegistrationClaimExactFields$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0，gofmt/vet 静默；fuzz 320,785 executions，PASS 61.012s。

## core-1 HR 接纳及 L4

- M1/L1 修复完整接纳，没有改原复核期望。后续所有权字段必须读取 original JSON
  token，不能用 R5 text projection 作为证明。
- L4：HA guard allocation bound 同时断言 B/op 和 allocs/op；0 与 2MiB
  captured_source 的阈值均不随 payload 增长。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/registry_native_test.go; GOMAXPROCS=4 go test -race -p 4 -count=1 -run "Prepared.*Stored|Heartbeat.*Stored|Malformed.*Disable|TypedHAEnablePresenceAndAllocationBound|N810" ./internal/agents ./internal/agents/dnsha'

Exit 0：agents 18.756s，dnsha 2.511s。

## typed registry / enrollment 块

- typed AgentEdits/EditAgent，管理开关 Model 入口、原始 token 单记录查询、DeleteAgent，
  原 Record 入口仅做 DTO codec。busy guards 读取原始 member，port 数字不同表示等值，
  bool/int 不互认；显式管理编辑可修复无进行中任务的 R5 profile，进行中任务则拒绝。
- 注册主体迁到 RegistrationInput / Options / Result，原子性由同一个 editRegistry 事务拥有。
  普通 token 重注册不回显 token；安装重放核验原始 marker 和全部 identity；unknown 和
  managed member 原始 JSON 保留，注册前端字段仍按原有稳定顺序写入。
- typed RebindProfiles 只允许精确 reviewed original string IDs，不让 R5 字符串投影认领
  绑定；profile metadata 从 cached View 取小型 DTO，未复制配置。结果仍不含凭据。
- 协调者已批准有条件收紧 malformed profile request。已静态核实当前及仓库保留历史
  Go Agent RegisterRequest 不含 profile_id，shell/AIO/Docker/HA 的受支持入口不发送
  false/0；网关/HA 会话注入前均验证 string。证据及 exact HTTP 独立样例已登记
  behaviour-changes。唯一 api/agents 接线是该 typed type-refusal 的 400 映射与测试。
- 注册第一轮仅旧字段顺序回归失败，已修复实现的字段顺序，旧期望未改；第二轮 root
  注册 43.803s、API O9 181.065s 均 PASS。更完整回归的唯一失败是新增测试误用 SetJSON 写 unknown member，已改为
  Fields.Set；其固定期望不变。该测试随后 PASS；API O9+N810 全部 PASS 131.248s。
  新增 R5 rebind 负例由原始 SQLite row 导入，避免被正常 write validator 提前拒绝。


## 本块补充验证（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "TypedAgent|TypedToken|AgentMutations|RegistrationKeeps|ProfileGuard|InvalidRebind|RebindIs|OneBusy|ChangedBinding|ManagerDistinguishes|ManagerRejects|DomainDiscovery|TrafficHistory" ./internal/agents'

Exit 0，agents 141.541s（注册主体迁移前的 native edits/delete/switch/token 块）。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "Registration|Register|AgentMutations|ProfileGuard|^TestO9" ./internal/agents ./internal/api/agents'

Exit 0，agents 43.803s，API 181.065s（注册主体迁移及 stable order 修复后）。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/enrollment_test.go internal/agents/enrollment_fuzz_test.go internal/api/agents/n810_proof_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/api/agents; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "TypedEnrollmentKeeps|N810Stored" ./internal/agents ./internal/api/agents; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzRegistrationInputOwnershipBoundary$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0，agents 3.253s，API 6.520s；fuzz 804,516 executions，PASS 60.013s。
该 HTTP 回归直接篡改 stored proof 的 name=7 / port="8080"，逐行验证 403、未消费、无 Agent；
恢复正确原始类型后同一 proof 得到 200 并原子消费。既有 session shape 解码测试的期望不改。

## 接着做

1. deployments、remote/client、diagnostics/events/messages/version/time 的 typed 主体。
2. ipv6 / rulesync / metrics / traffic adapters 的其余 scalar/dynamic 入口。
3. 结束时再统一做 agents 子树 race、跨包 Corpus|Reference|Golden、agents difftest；model
   source hash 改动引起的 pyref metadata 要按 Python check → metadata record → check 审计，
   不能改 entry 的 input/output，不能更新旧期望来迁就实现。此处还未声称完整 S 或 verified。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/enrollment_control_test.go; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "TypedEnrollmentExact|TypedRebindRequires|RegistrationCapabilitiesAndAtomicEnrollment|WronglyTypedAgentMembers|AutoHostPin" ./internal/agents ./internal/gateway/installation'

Exit 0，agents 11.530s，gateway/installation 4.887s；包括 exact installation replay 正控、
原始 SQLite R5 ID/profile 负例及原网关 atomic enrollment / text port / integral float 回归。

## typed timeline / DNS report 与 original proof 收尾

- registry/enrollment 块已提交 17a3e7e19。
- Clean/Append/MergeOperationEvents 业务只接受 model.OperationEvent；events_legacy.go
  仅转码。无效原项仍占用 last-80 窗口，unknown 非有限字段不丢弃有效事件；original
  status/at/message 类型逐行绑定，输入不可变。Native DNSReport 只读原始 check strings，
  限制 32 项，保留空数组/absent 区分，坏项输出原中文完整性警告，未知字段不外泄。
- native 诊断入口统一 internal/redact.DiagnosticText/EventText；底层共享 diag formatter
  不复制 credential policy。保留原 Failure/Event 参考所有期望。小于截断标记的 byte budget
  原代码可能负切片 panic；现在保持 UTF-8 并严格限长，1..127 的独立单测通过。
- model 单成员 getter 共用 recordMemberJSON，校验 provenance owner + exact schema key，
  只编码选中的 typed member。部署、prepared、event、DNS、HA identity 均复用；没有
  私有 scalar parser，没有每个 Agent/心跳解码整份配置。新增原始 null/number/bool、
  unknown、Clear、copy ownership 及 0/2MiB opaque payload B/op+alloc bound。
- 额外 M 级 integrity 风险已先复现后修复：旧 deploymentProfileJSON 用 std struct 解码
  整份 journal，Profile_ID 可覆盖原始 profile_id=7 的 RawMessage；HA Group_ID/Peer_ID
  也可能被当作证明，或覆盖正确小写字段。现在从 shared model 读取 exact original member。
  另 submitted="true"/"false" 的 R5 pointer=nil 曾允许 RememberPreparedContent；现在仅
  absent/null/original false 是可准备状态。六条 prepared 原行含 true 拒绝与 false/null 正控、
  三条 HA alias 原行均固定期望，不改任何旧期望。失败事务 head 保持不变。

### 证据（.64）

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/diag/diag.go internal/agents/diag/messages.go internal/agents/events_native_test.go internal/agents/dns_reports_test.go internal/redact/diagnostic_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/redact ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "Event|ReferenceFailure|DNSReport|DNSFields|FailureText|DiagnosticTextSmall" ./internal/agents ./internal/redact; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzTypedOperationEvents$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：agents 11.288s，redact 1.132s；fuzz 768,362 executions，PASS 60.156s。
此前原 Event/Failure/Credential/Redact/Member 定向 race 同时覆盖 notifications：
agents 15.526s、redact 1.021s、model 1.033s、notifications 1.022s，全部 PASS。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -w internal/agents/ownership_alias_test.go && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=10m -run "PreparedOwnershipOriginalMember|HAPairIdentityUsesExactStoredKeys" ./internal/agents'

修复前独立期望复现（21.187s）：profile_id=7 + Profile_ID="7"、submitted="true"、
submitted="false" 均错误 accepted=true；仅大写 Group_ID/Peer_ID 也错误 valid=true。
测试保留，不替换期望。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/model/member_native_test.go internal/model/agentdeploy.go internal/model/agentha.go internal/agents/prepared.go internal/agents/ha_model.go internal/agents/messages.go internal/agents/events_native_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/redact ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "PreparedOwnership|HAPairIdentity|TypedHAPair|ReviewN810|JournalMember|Event|DNSReport|DiagnosticTextSmall" ./internal/agents ./internal/model ./internal/redact ./internal/agents/dnsha'

Exit 0：agents 40.962s，model 3.466s，redact 1.140s，dnsha 2.072s。

### 当前边界

完整 N8-10 仍部分完成。尚余 deployment 主状态机、remote/client 与 response decode、
diagnostics.ResponseFailure、messages/version/time、runtime legacy importer/private codec、
HA 其余 public projection、presence，以及各子包残留 adapter。只有 typed timeline 与 DNS
report 业务本轮完成，不能据此宣称全部 diagnostics / agents core 已 S。
