# N8-10 agents core-2

状态：部分完成（core-2 本次边界实现和定向验证已完成，待协调者安排 fresh Opus HR）。

本次已完成边界：注册/编辑/改绑/删除的 typed transaction 与 O9-H2 准入；部署保留/回包/
激活/回滚/恢复；direct remote/diagnostics/events/messages/version/time/runtime import；
DNS follow/HA/IPv6/rulesync adapter；metrics/traffic adapter 的 ctx、脱敏、统计 IP/域名
库复用。live heartbeat 继续使用 batch-10 的七个 owned RawMessage，原分配阈值不改。

仍动态或延后的明确清单：monitor；upgrades/address/artifact；installer；api/agents 的
请求/响应 binder、installer/content/client-tools/deploy-proxy；A05 完整 SQL/counter/query
engine、metrics task rows/持久文件兼容入口；tasks/health/deploy 未迁 consumer 的 Record
回调。因此保留 core 的机械 facade codec 与旧 public 错误类型名，不能把 jsonx import 数量
当成所有步骤已完成。gateway/installation 的 guard codec 也保留到其 consumer S pass。
oxidns/legacy.go 的生产 Agent 侧仍有 api/agents/client_tools_adapter.go；tools/proddiff
也保留旧 codec 调用，本次不能删除。

分支 go-n8-10-core-2 初始 rebase 到 go@12150a402。
原始拆分基点为 core-1 tip 7a3f1da4ebd606389c0142cf6f9134bc970fe0b5；core-1 的 HR
verified、无 high，报告见 n8-10-core.md / review-n8-10-core.md。ec7c71865 / e871b2fe3
已由 batch-10 纳入基底，rebase 未重复应用；3380a1c28 的窄 RawMessage runtime 与
12150a402 的精确心跳键名修复完整保留。下文较早的提交 ID 是 rebase 前历史，现行提交链
以本分支 git log 为准。

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

## typed reservation 块

- timeline / exact proof 块提交 a7ab3c909。
- BeginDeployment 仅 legacy JSON codec；ReserveDeployment 用 model.Agent/Deployment transaction。
  HA children、DNS follow child/source IDs 读取原始字符串；HA summary 仅复制公开字段，
  不编码 captured source。校验与 busy 检查顺序、物理 preparation lease、shutdown/disconnect、
  commit-failure/panic cleanup、30 条历史保留、旧 prepared.files 裁剪均保留。
- 返回 task 和 latest/history 各自拥有数据，客户端改返回的 Events 不能改 repository cache。
  shared Map.EntryValid 区分原始 object（包括 {}）与错误 top-level token 的零投影；
  Map.MemberJSON 保留原始 string/number token，仅编码被选 entry，不编码其他 entries。
- API 主差分发现首次写 switch 时 native struct 顺序让 updated_at 跑到 traffic_history_since
  前面；已改实现按原序 SetJSON（since → enabled → updated_at / domain → updated_at），
  旧期望未改。主差分最终所有请求、落库和 history 文件均 PASS。

### 定向验证

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/model/map.go internal/model/agentha.go internal/agents/deployment_reservation.go internal/agents/deployments.go internal/agents/messages.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "Preparation|PreparedOwnership|BeginDeployment|FailedRollbackIsBlocking|OldPrepared|BoundHistory|ProfileGuard|DeploymentHA" ./internal/agents ./internal/deploy ./internal/agents/dnsha ./internal/agents/dnsfollow'

Exit 0：agents 64.067s、deploy 2.561s、dnsfollow 60.321s；dnsha 无匹配测试。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/model/map_members_test.go internal/agents/deployment_reservation_test.go; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "TypedReservation|MapMembers|DeploymentHistory|Preparation|ProfileGuard" ./internal/agents ./internal/model'

Exit 0：agents 31.421s、model 1.011s。包含返回 journal ownership、HA child=7 拒绝 / "7" 正控、
map scalar/empty-object/null/R5 nested field/explicit repair 的固定原始输入期望。

    RT_HOST=sunbroke@192.168.0.32 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; gofmt -w internal/agents/mutations.go; GOMAXPROCS=4 go test -p 4 -count=1 -timeout=20m -run "^TestDifferentialAgainstFlask$" ./internal/api/agents/difftest'

Exit 0：API agents difftest 9.435s。此前两次只 stored key order 不同，修复实现后通过。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/model/map_members_test.go; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzDeploymentMapMemberReads$" -fuzztime=60s -parallel=2 ./internal/model'

Exit 0：1,976,452 executions，PASS 60.103s；getter 原始 token 和 owned bytes、读不改变 map。

### 下一块的比较策略（协调者已裁定）

原 a01 record_statuses 的 list/dict 错误行曾强制 CPython exception bytes。后续删除该仿真时，
原 corpus/输入输出/SHA 不动；只对精确逐行绑定的这些错误行比较 Python error class、Go
拒绝、事务不写。设备坏回包按项目约定结构化 502 + 中文原因（不是裸 500），补真实 HTTP
设备请求原字节及回包回归，并登记 behaviour-changes。同类情况按此标准处理，无需再次问。

## typed deployment record 与 scoped Agent reads

状态：部分完成。RecordDeployment 现在仅选 exact protocol keys 并交给
DeploymentUpdate / RecordDeploymentModel；业务更新直接操作 model.Deployment/Agent。
不接受设备覆盖 id/profile/service/receipt/prepared proof。完成确认的 stored deployment_id、
profile_id、service_type 继续核验原始字符串，不能用 R5 数字投影证明所有权；独立原始
数字/字符串四条正反控制及所有既有状态、历史、late receipt、rollback 检查保留。

- stale/terminal/upload/activation receipt 的判定顺序、submitted/manual rollback 的原始
  boolean、事件、诊断脱敏、DNS report、applied content、配置确认和通知维持原有语义。
  诊断统一 internal/redact；shutdown 用 lifecycle.Detach，txn 提交前仍检查取消。
- 删除原 Record 私有动态 updater 和 CPython unhashable status 仿真。按协调者裁定，
  八条 list/dict 错误行保留原 corpus、原输入输出及 SHA；完整 archive + 每行独立 SHA
  绑定后比较 Python TypeError 类、Go 拒绝、事务 fingerprint 不变。其余行保持原精确期望。
  HTTP 上游此类错误结构化 502 + 中文原因，behaviour-changes 已登记。
- API/agents 仅最小错误映射接线（deploy.go、device_response.go）。两条真实 loopback HTTP
  回归核验实际 method/URI/header/body 原字节、502 JSON、凭据不泄露和 DB 不写入。
- no-op 预览只拥有目标 Agent 元数据和一份 journal，不复制无关 Agent/profile/HA source。
  原失败分配 fixture 保留：每个无关来源 2MiB，整库超过 8MiB 文档缓存上限时原路径
  达 783,466,344 B/op / 178,457 allocs。修复后 empty 1,055,498 B/op / 9,232 allocs，
  large 1,056,414 B/op / 9,228 allocs；固定成本仍较高，已不随无关配置增长。
- 为消除此真实热点，repo.ViewAgents 提供只读 typed Agent 行 generation cache；先复用
  full cache，否则在 SQLite 同一个 read snapshot 读取原始 agents 行。Store.Conn.Field
  只用 bound SQL、检查原始 UTF-8/JSON，不经 JSON1 投影。仅保留一代 Agent 行，包括超出
  文档缓存上限的行，避免大 registry 在每次心跳重解码。冷载入合并，callback 不持锁；
  transaction/WithReadSnapshot 不发布临时数据；R5 警告不重复报告。
- UpdateSystem 的拟议 section 通过 callback scoped ctx 供嵌套 native/legacy readonly
  reader 观察，回调结束就失效，abort 不泄露；没有改变 nested writer/commit 规则。
  凭据缓存先读轻量 Head 再命中，不先解码整库；事务内只读取本 txn 的原始 string tokens。
- 以上是 repository durable snapshot，未改变 runtime entry。已收到 batch-10 的轻量
  runtime 修复通知；等 batch-10 合入 go 后 rebase，并以其七个 owned RawMessage 为准，
  不恢复完整 model.Agent 的 runtime 表示，最终再次核验心跳分配。

### 本块已通过的定向证据

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "DeploymentHistory|OlderAgentStatus|Confirmed|TransportDetails|SupersededHistory|ManualRollback|LateTransport|Preparation" ./internal/agents'

Exit 0：agents 94.331s。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ReferenceRecordDeploymentStatuses" ./internal/agents'

Exit 0：agents 61.407s，包含全部原 status rows 和八条逐行 no-write 证明。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "N810MalformedDeployment" ./internal/api/agents'

Exit 0：api/agents 2.657s。此前真实 HTTP 回归暴露旧 poll 路径把 array/object 静默 pending；
修复 poll guard 实现后通过，原新回归期望未改。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/repo ./internal/store && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "TypedRecordPreviewAllocation" -v ./internal/agents'

Exit 0：agents 93.707s，分配数值见上。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/repo ./internal/store; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "AgentView|RedactionTokens|RotatedTokens|Working|Snapshot" ./internal/repo; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzTypedDeploymentPatchOwnership$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：repo 8.153s；fuzz 757,165 executions，PASS 60.308s。

当前仍未迁完：deployment read/poll/activation/rollback/recovery、remote/client/response codec、
ResponseFailure、runtime legacy importer/time/version/presence、HA public projections，以及
子包其余 adapter。core-2 暂不宣称完成；完整 agents subtree race / cross Corpus|Reference|Golden /
API difftest 与 metadata sources 检查在最终剩余业务块后做。

后续定向复跑（同 .64，Exit 0）：

    GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run 'AgentView|RedactionTokens|RotatedTokens|TokensWrittenByAnother' ./internal/repo
    GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run 'N810MalformedDeployment|TypedDeploymentCompletion|TypedRegistry|TypedHeartbeat' ./internal/agents ./internal/api/agents

repo 3.529s、agents 30.418s、api/agents 2.231s。
最终 gofmt（上述修改的 agents/api/model/repo/store 文件）与 targeted go vet 通过；
`FieldReadsOwned|AgentView|RedactionTokens|RotatedTokens|TokensWrittenByAnother` race：
repo 3.558s、store 1.092s。包含 oversized registry 本身超过 cache budget 的重复读取、
凭据热缓存不解码整库、原始字段类型、cancellation、nested native/legacy txn read、
abort、外部提交与 pinned snapshot、不允许修改共享回调值等独立验证。

## typed deployment actions 块

前块提交 269f9ac6c。本块迁移 deployment read/poll、候选激活、manual rollback reservation、
恢复、明确拒绝后的 journal 复原和 deployment HTTP request。旧 Manager 入口仅 codec；
新入口分别 ReadDeploymentModel、ActivateDeploymentModel、RollbackDeploymentModel、
RecoverDeploymentModel、DeploymentRequestModel。设备 endpoint 只接受原始字符串 host/token，
Authorization header override 规范化后与请求 body 原样传入 transport。尚未改动 runtime 表示。

- 保留 profile/service/task latest ownership、preparation lease、404 上传登记竞态、ambiguous
  rollback 重投、只有精确 busy 的限时重试、明确拒绝与 unknown 的区别、旧版本恢复命令。
  全部 durable identity 都核验原始字符串；HTTP error code 也用 stdlib 数值 type probe，
  不能把 R5 的 `"404"` 投影当 404。新增独立字节 fixture 已先复现这个问题，原期望未改。
- native outcome DTO 强制发送 success:false 的存在性；model.Pending 仅为已有公开 pending
  语义添加 typed 字段。未新增 persisted 字段或修改旧 scalar/corpus 期望。
- 候选与 monitor checkpoint 仍经 deployment_hook_bridge 的纯 codec 接既有 hook；这是
  deploy/monitor 后续 S 边界，不含两份状态机。monitor/upgrades/installer 本块未修改。
- 删除旧 dynamic poll/activate/rollback/recover，以及不用的 merge/status helper。
  nil Agent 的旧 CPython 内部错误按已批准策略保留原调用、原异常文本与 SHA 精确绑定，
  核验原 TypeError 类、native refusal、非 transport error、事务不写入。
- lifecycle regression 核验 request disconnect 后仍能完成 rollback；Runtime shutdown 在
  device reply 返回前发生时保持 rolling_back reservation，拒绝 receipt commit 和后续
  旧 generation 恢复。测试初稿错误地在 shutdown 后才 BindRequest（API 明确会返回无
  generation 的原 ctx），已修正 fixture 在 live generation 先绑定，拒绝期望未放松。

### 本块验证（定向）

首轮 .64 native actions race：deploy 74.997s、dnsha 29.119s PASS；agents 中 pending 的
未声明字段暴露实现缺口，并暴露 nil CPython message 断言。补 typed pending 和批准的
精确 nil reference comparison 后：

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/deployment_actions.go internal/agents/deployment_remote.go internal/agents/deployment_actions_test.go internal/agents/deployments.go internal/agents/a01_reference_test.go internal/agents/status_reference_native_test.go internal/model/agentdeploy.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "TypedDeploymentActions|TypedDeploymentHTTPStatus|AgentDeletedDuringRollback|PollBeforeUpload|ManualRollbackBusy|ReviewA14Manual|PreparationInterruption|Recover|WarningReport|Candidate" ./internal/agents ./internal/deploy'

Exit 0：agents 66.943s、deploy 68.950s。所有原正常回归期望不变。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/deployment_actions_fuzz_test.go; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzDeploymentRouteBinding$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：679,706 executions，PASS 60.056s。核验原始 task/service 证明、大小写别名隔离、
optional 的 absent 与 null 区别、getter 不改变原始 journal。

    RT_HOST=sunbroke@192.168.0.64 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && GOMAXPROCS=4 go test -p 4 -count=1 -timeout=20m -run "^TestPublishHTTPAgainstFlask$|^TestDifferentialAgainstFlask$" ./internal/api/agents/difftest'

Exit 0：agents API difftest 9.068s。发布比较完整 DB、private files、设备实际原始调用，
其原请求 raw body/header 比较未修改。

真实 HTTP busy 两次请求（中文、2.00、空白、换行 body）及小写 Authorization override
原字节相同，caller header 不修改。首轮该测试已 PASS；同轮 lifecycle fixture 错误已如上修正。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; gofmt -w internal/agents/deployment_lifecycle_test.go; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/model ./internal/deploy; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "^TestTypedRollbackDisconnectAndShutdownCommitBoundary$" ./internal/agents'

Exit 0：agents 10.278s。

## HA public projection / adapter 与 presence 块

状态仍部分完成。部署动作前块提交 56e4bdcd1。

- HASummaryModel / HAAggregateModel 只取公开成员，保持原始字段类型和独立所有权；默认
  pair 任务/激活接线也直接用 model 和原始 child ID。HAPair 旧入口改为 typed pair codec；
  Core 的旧 unhashable helper 已删除。GetDeploymentModel 是含 HA owner dispatch 的
  typed 公开读；ActivateDeploymentModel 是含 HA guard 的公开激活，子任务显式使用
  ActivateCandidateModel（原 typed direct 激活改名）。旧 API 签名不变。
- dnsha adapter 的 poll/activate/reserve/retire/capabilities 使用 typed core；profile source
  使用 typed repo Config/PublishConfig，一份 source 通过已有 SourceFromJSON codec 交给
  deploy 的兼容捕获边界；不改部署文件格式。summary 不再先编码含 captured config 的整份
  journal。事务 proof 比较改用共享 yamlx.Equal，保持 JSON 类型区分，不私造 Python equality。
  routersUsing 从一次 repo snapshot 读取，dns_agent_id 只接受原始字符串，不认 R5 或别名。
  子任务预留也核验原始 child string，非法形状不写事务；source 的 profile 交 core 原始
  binding resolver，不能用 ProfileID 投影选配置。
- model.Pending 进入声明字段后，dnsha 原先从 Fields.Get 读 pending 会漏掉 true；已做最小
  接线修正并增加 true/false/null/quoted true/number 五条独立原始 JSON 控制。其余 HA 状态机
  语义不变。monitor/upgrades/installer 本块未修改。
- presence 的唯一状态 owner 接受 PresenceObservation DTO，只保留脱敏后的 log metadata，
  跟踪和 restart 语义保留。旧观察入口仅选四个展示字段转 DTO；历史非字符串 metadata
  使用原生 JSON 展示，后经 internal/redact，保留 label 并消除嵌套凭据，不复制完整 Agent
  或配置、不再把 any/私有 Python repr 留在 transition state。旧普通名称日志字节不变。

### 定向证据

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/agents/dnsha; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "PendingJournal|Pair|HA|Candidate|Publication|Activation" ./internal/agents ./internal/agents/dnsha'

Exit 0：agents 65.756s、dnsha 167.870s。包含所有权、候选、HA runtime、独立 summary
copy 和分配上限；负载背景有另一份协调者 full gate，本分支只跑上述定向包。

    RT_HOST=sunbroke@192.168.0.32 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "^TestValidationAgainstPython$" ./internal/agents/dnsha; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "^TestTypedHASummaryAllocationIgnoresCapturedSource$" -v ./internal/agents'

Exit 0：dnsha 2.037s、agents 3.683s。summary empty 21,513 B/op / 137 allocs；两份各
2MiB captured sources 时 21,679 B/op / 137 allocs。公开字段名单以外的 source 不被编码。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "Presence|ARestartDoesNotLog" ./internal/agents ./internal/agents/rulesync'

Exit 0：agents 7.078s、rulesync 2.901s。包含未改的 rich metadata / nested credential 回归。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/agents/dnsha ./internal/agents/rulesync ./internal/model; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "RouterBindingsUseOriginal|PendingJournal" ./internal/agents/dnsha'

Exit 0：dnsha 1.874s；router 原始数字/字符串/别名/null 逐条绑定、read fingerprint 不变。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/agents/dnsha; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ChildReservationsReject|PendingJournal|RouterBindingsUseOriginal" ./internal/agents/dnsha'

Exit 0：dnsha 3.166s，原始 child "7" 正控，7/null 拒绝且不预留、不写入。

### 当前剩余边界

未完成项主要为 remote/client/response codec 与 ResponseFailure、runtime legacy importer、
private time/version/messages helpers、ipv6 / rulesync / metrics / traffic 剩余 adapter，及最终
跨包 corpus/reference/golden、agents subtree race、agents difftest、source metadata 刷新。
batch-10 尚未合入本机 go；收到的轻量 heartbeat runtime 修复仍待集成后 rebase，不改回
完整 model.Agent runtime entry。core-2 不能宣称全部 agents core S 完成。

## Agent wire JSON encoder

新增共享 jsonresp.EncodeDevice：标准库 encoding/json 决定完整 JSON 值和成员顺序，
json.Indent 加原生 ASCII escaping 保持设备协议中的 comma/colon 单空格、Unicode surrogate
pairs、DEL 转义。没有自写 scalar decoder、Python coercion 或 nonfinite 遍历；jsonBody
只负责该原生序列化与既有错误提示映射。原设备请求字节比较和 normal expected 全部保留。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/jsonresp ./internal/agents; GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "DeviceJSON|ReferenceJSONBodies|A15ReviewCall|DirectAgentCalls" ./internal/jsonresp ./internal/agents; GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzDeviceJSONPreservesValues$" -fuzztime=60s -parallel=2 ./internal/jsonresp'

Exit 0：jsonresp 1.011s、agents 10.103s；fuzz 741,948 executions，PASS 61.006s。
追加 DEL typed-string 独立字节期望后，DeviceJSON|ReferenceJSONBodies race 再通过：
jsonresp 1.008s、agents 3.933s。ASCII/UTF-16 surrogate、原始 2.00、URL/HTML 字符、
literal backslash-u、空 containers、null、nonfinite 拒绝均有固定 byte fixture。

已收到协调者 batch-10 合入 go@12150a402 通知。下一步先 rebase，保留窄 RawMessage
runtime 及精确 heartbeat keys，再审核其他外部 JSON decoder 的大小写／重复键边界。

## batch-10 rebase、精确 JSON 入口审计、failure DTO 和共享路径

- 已叠在 go@12150a402，heartbeatEntry 继续仅保存七个 owned RawMessage。
  registry 热读接 ViewAgents，未恢复完整 model.Agent runtime。MemberJSON 的 scalar
  native encoder 和 sameStoredJSON 快速返回保留；两个 baseline HR commit 已在 go 中，
  rebase 跳过其重复 patch。合并时补回旧 core-2 manager patch 覆盖掉的 fmt import。
- 注册 claim 不再使用“精确筛选后解匿名结构体”；直接 Fields.Get 已知键，再用标准库
  解到 string / shared model.Int 等标量。注册／编辑／deployment patch 外部 DTO 统一
  model.Fields（精确 key、拒绝真实重复键，包括 escaped 同名和嵌套重复）。Session proof
  通过 shared MemberJSON 读取原始文本；未改变 quoted port 仅通过 shape helper、实际 admission
  仍拒绝的既有独立期望。O9-H2 hash-only / 403 / 原子消费不变。
- 其余 core 新写的 json.Unmarshal 到 model.Agent/Deployment/DNSCheck 等都调用共享
  DecodeRecord（精确 key、重复检测），不是 Go tag 的默认 struct decoder。内部 owned copy
  和 native redactor 回包转 DTO 亦保持该 codec。测试 oracle struct、JSON encoding、标量／
  RawMessage／Fields 不是 Agent 外部 tag decoder。现存 dnsha 的 server-owned payload 文件
  decoder 和其他遗留 adapter 仍纳入下一块审计；本报告不据此宣称整个 core 已 S 完成。
- 成功的部署 HTTP 回包如果不是有效唯一-key object（含 duplicate status、嵌套 duplicate、
  非 JSON、null），返回 INVALID_RESPONSE 中文 502，不把坏回包降为 pending。原状态
  list/object 的两个独立用例期望未改；扩展到六条真实 loopback HTTP 回归，都比较实际
  method/URI/header/空 body 原字节、脱敏和 DB fingerprint 不变。HTTP error response 的
  既有不可用／pending 处理保留。behaviour-changes 同步。
- ResponseFailureModel 使用严格字段与 native diagnostics；旧 ResponseFailure 仅 codec。
  frozen Responses / legacy reason / text-vs-HTML 全部原期望通过。新增 case alias、数值诊断、
  duplicate message 和 token 原文 fixture，非 string 不通过 R5 投影制造诊断。
- 删除 runtime 私有 posixpath walker，复用 repo.ResolveDataDir 作为同一物理目录的 key。
  冻结 Pathlib 语料期望不改；测试旧 helper 名仅在 _test.go 做别名。Repository 不支持的
  symlink loop 在直接诊断 RuntimeFor 调用时只取稳定 absolute fallback；不复制另一套路径算法。

### rebase 后证据

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e; cd server; GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/model ./internal/repo; GOMAXPROCS=4 go test -p 4 -count=1 -timeout=15m -run "Batch10.*Alloc|ReviewB10" -v ./internal/agents'

Exit 0：agents 6.338s。原 threshold 不改：public 6,801/6,773 B/op，154 allocs；native
7,196/7,176 B/op，161 allocs（empty / 2MiB）；所有 B10 exact keys 与 byte projection 通过。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "NativeProtocol|RegistrationClaim|RegistrationSession|RegistrationAuthorization|RegistrationProof|RegistrationToken|TypedRegistration|RegistrationProfile|N810Registration|NativeDeployment|TypedDeployment" ./internal/agents ./internal/api/agents'

Exit 0：agents 34.993s、api/agents 7.112s；targeted go vet 通过。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "NativeProtocol|N810MalformedDeployment|TypedDeploymentBusyRetry|PostRetriesOnlyExactBusy|BusyRetriesBounded" ./internal/agents ./internal/api/agents'

Exit 0：agents 9.561s、api/agents 6.044s；targeted go vet 通过。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzTypedDeploymentPatchOwnership$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：630,531 executions，PASS 60.110s。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzRegistrationClaimExactFields$" -fuzztime=60s -parallel=2 ./internal/agents && GOMAXPROCS=4 go test -p 4 -count=1 -timeout=15m -run "^TestBatch10HeartbeatAllocationCeilings$" -v ./internal/agents'

Exit 0：251,574 executions，PASS 61.019s；ceilings PASS 5.660s（public 6,801/6,772 B/op，
154 allocs；native 7,194/7,176 B/op，161 allocs）。一次前置 fuzz 同步撞上 helper 移动窗口，
少同步测试别名而 build failed；补齐完整快照后按同命令重跑，不是删除或放松测试。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ReferencePathResolve|EveryManagerForADataDirectory|DanglingRuntime" ./internal/agents'

Exit 0：agents 3.863s。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ReferenceResponses|TypedResponseFailure|LegacyErrors|PlainTextLegacy|ResponseFailure" ./internal/agents'

Exit 0：agents 13.535s，targeted go vet 与上述修改的远端 gofmt 通过。

## 原生时间和机器版本（部分完成）

删除 pytime.go 的 CPython local_to_seconds 重放，time.go 以 Go time.Date/Format、共享
FromISOFormat 和明确的 zone offset 选择处理 gap/fold。存储时间、微秒、offset 秒与范围
边界的原冻结期望全部不改；新增真实 Python zoneinfo 对照，逐行绑定 zone/text 输入，
覆盖 NY/Berlin 两个 DST 边界、Lord Howe 半小时变化和 Apia 跳日。旧 helper 名仅留测试别名。

CompareVersions 删除私有 Unicode Nd / 4300 位 int 仿真，改共享 taskstatus.Count。机器值
证据和协调裁定已登记 behaviour-changes。九条变化使用独立 literal policy fixture，完整
a02 archive SHA、原 compact row SHA、Python want 与 native want 四项绑定，未动旧语料。

    RT_HOST=sunbroke@192.168.0.64 rt -e CONFIGFLOW_DIFF=1 -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -run "LocalWallZoneTransitions|ReferenceTimestamps|A15ReviewISO|A15ReviewRuntime|ReferenceHeartbeatFreshness|ReferenceEventTime" ./internal/agents && GOMAXPROCS=4 go test -p 4 -count=1 -run "^TestBatch10HeartbeatAllocationCeilings$" -v ./internal/agents'

Exit 0：agents 1.078s；allocation ceilings 5.640s，public 6,810/6,774 B/op、153 allocs，
native 7,192/7,175 B/op、160 allocs（empty / 2MiB），阈值原样保留。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go vet -p 4 ./internal/agents && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ReferenceCompareVersions|NativeVersionBusinessRules|LocalWallZoneTransitions|ReferenceTimestamps|A15ReviewISO|A15ReviewRuntime" ./internal/agents'

Exit 0：agents 1.185s；上述源文件远端 gofmt 完成。live oracle 已在 .64 的 CONFIGFLOW_DIFF=1
命令执行；.32 未设变量时该 oracle 正常 skip。版本 fuzz 60s 结果后补。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzNativeVersionComparison$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：351,732 executions，PASS 61.010s；验证反对称、自反、suffix 不变（混合数字/文本
业务比较本身不保证传递性，未制造这项断言）。

## 旧心跳库导入转 typed registry（部分完成）

New 直接调用 native importer，使用 ViewAgents 的原始字符串 ID 和当前 live 版本判断；
legacy.fields 解为 model.Agent，版本 patch 用 model.Fields，saveReportedModels 在单一
EditAgents 事务写入且验证原始 ID。旧 ImportLegacyDatabase / saveReportedFields 只做
callback/JSON codec，兼容后续包和冻结测试。SQL query 绑定 shutdown context。

旧 runtime.sqlite3 的历史 fields column 允许 duplicate key。首轮严格 Fields 迁移遗漏
冻结 dupkeys 行；没有改期望，改用标准 map[string]RawMessage 在一次性存储迁移中保留
最后值，再进共享 model。它只承载 runtime/version，不承载身份所有权；实时协议仍用
严格 Fields。原始 shapes 行（含 UTF16/32 BLOB、重复 ID、duplicate keys）全部保留。
原生 decoder/driver 诊断替换仍待下一块，当前不声称 runtime 的全部仿真已移除。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "ReferenceLegacyRows|LegacyDatabase|LegacyRuntime|LegacyVersion|RuntimeTimestamp|TypedRuntime|RuntimeShutdown|DeletedAgentIsNot|HeartbeatFresh" ./internal/agents'

Exit 0：agents 5.587s。前一轮 Legacy|Runtime|SaveReported|Heartbeat|Registration（116.052s）
只失败在上述 dupkeys 行；修复后原行重新通过。远端 gofmt、agents vet 已执行。

## 原生 Unicode transport 和旧库错误（进行中）

删除 pydecode.go 手写 UTF8 maximal-subpart、UTF16 surrogate、UTF32 decoder 和 Python codec
alias 表，改 x/text 的 Unicode / IANA codec；MIME 参数用标准 mime.ParseMediaType。
旧 UTF8/16/32 JSON BLOB 继续接受，解码工作由库负责，完整性只检查 code-unit 长度、UTF8
合法性和 UTF32 范围。25 条原 Responses、LegacyRows、native failure DTO 期望不变通过。

删去 sqliteFailure、textDecodeError、pyOSError；driver 保留原生错误链，坏 SQLite TEXT
只返回 ErrLegacyTextEncoding 与 column 名，不把数据库原文写日志。七条旧错误行通过
真实 Python sqlite3 的 OperationalError / DatabaseError 类别生成独立 metadata；a02
整档 SHA 和各完整原行 SHA 绑定，比较 native 拒绝与 all-or-nothing 无部分行。另用真实
repository Head 验证导入错误不调用 persist、不改变事务或 runtime，并仍删除旧库。
删除警告的两条原始日志 SHA 也绑定，改核验原 errno21、两次 native 警告与目录未删除。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "ReferenceResponses|ReferenceLegacyRows|PlainTextLegacy|TypedResponseFailure|LegacyErrors|Decode|Runtime" ./internal/agents'

Exit 0：agents 30.325s，远端 gofmt / agents vet 通过；响应值与字节期望全部沿用原档。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_n810_runtime_native.py'

Exit 0：七条真实 Python SQLite error-class oracle，未生成或覆盖任何原 a02 输出。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=10m -run "NativeLegacyReadErrors|ReferenceLegacyRows|ReferenceLegacyFileRemoval|ReferenceResponses|ProfileFilteringKeepsLiteralDefault" ./internal/agents ./internal/agents/rulesync'

agents Exit 0：8.425s。新增 importer fixture 必须在安装旧库后 ResetRuntime 模拟重启，
因为 newEnv 的 New 已做过一次性检查；最初 fixture 未模拟重启导致旧库按设计不再扫描，
不是取消 once-only 规则。此前失败原因已修正。该联合命令 rulesync 的空 profile 过滤
失败在下一项修复；原 native error 行、事务 no-write 与删除语义通过。

## DNS follow / IPv6 / rule-sync adapter 和 HA proof（进行中）

DNS follow Config/PublishConfig、poll/reserve/failed patch 走 typed repo/core；secret/error
路径使用 AgentTokensForSanitization 和 internal/redact；Options 不再往返 jsonx，events
直接调用 AppendOperationEvent。IPv6 的 snapshot/read/list/EditAgents 改 typed repo/model，
编辑目标 ID 用新共享 AgentMatchesID 原始 string 证明。保留 freshness/status 和未知字段。

Rule-sync 的源读取、profile filtering、worker lookup、pair settings edit、poll/failed patch
改 typed repo/core，pair 编辑在同一事务验证双方原始 ID、reciprocal HA identities、profile
和原配置快照，然后同时写 settings。空 profile 仍使用当前 default，literal default 不
跟随默认切换；首次遗漏此规则，保持原测试期望修正后通过。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=10m -run "ProfileFilteringKeepsLiteralDefault|Configure|Settings|Presence" ./internal/agents/rulesync'

Exit 0：rulesync 11.201s。此前整包 109.895s 只失败原空 profile 行；其余未出现差异。

HA payload / bundle metadata、deploy Manifest/File 使用 Fields 精确键，不再 default struct
case folding，重复 key 拒绝。deploy 修改是嵌套 ownership 元数据所需的最小共享接线。
child ID 与 journal ID 均读原始 string token；原 numeric projection 无法组成合法 journal。
pair/profile checks 复用 core 的原始身份判断，adapter 的 selected 也验证原始 Agent ID。
Agent HA 读取走 CallAgent 和严格 Fields；成功的 malformed JSON / duplicate 回包拒绝，
HTTP 层只补 DeviceResponseError → structured 502 接线。新 ownership 原文 fixture 待最终跑。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "Payload|Manifest|Bundle|Reference|Corpus|Golden|Typed" ./internal/agents/dnsha ./internal/deploy'

Exit 0：dnsha 14.673s、deploy 4.624s；后续含 HA proof 的联合检查 dnsha 16.163s、deploy
4.727s 通过（agents 在当时未修正的新 importer fixture 上失败，已另修并重新验证）。

DNS follow / IPv6 整包 race 已在 .32 执行（20m timeout），待收尾。为查慢因在 .64 跑过
60s 的短诊断，超时栈显示正常 model.Marshal/commitStore 的 Runnable，非互相等待；
该短命令不算验证通过，也没有用其时间限制或修改期望裁剪原套件。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -count=1 -run "^$" -fuzz "^FuzzNativeUnicodeTransport$" -fuzztime=60s -parallel=2 ./internal/agents'

Exit 0：130,415 executions，PASS 61.010s。UTF8-BOM、UTF16 LE/BE、UTF32 LE/BE 均验证
JSON 编码前后原字节保持；不会把独立 HTTP 请求比对改为结构相等。heartbeatTimestamp
现在只收 string，旧 any 形状只在冻结测试的 timestampOf codec 中保留。

.32 的 DNS follow 整包 race 20m 到时未完成，最后仍在 commitStore/model.Marshal，
不能记为通过；同条命令 IPv6 PASS 24.208s。后续需要给 DNS follow 足够时间完成，
并核实 profile Source 的重复序列化成本；未调整测试期望或缩减该包的测试集合。

## 原生 remote protocol、display DTO 和生命周期（进行中）

remote.go 现在只留下旧 API 的参数/JSON codec 和 HA hook bridge；业务在 remote_model.go：
RemoteRequest 保存原始 wire bytes，RemoteCall/RunDirectOperation/AgentRequestModel/
SetLoggingModel/PushLegacyConfigModel 使用 typed registry、精确 Fields、strict success
boolean、原 token 和原 profile/service 证明。正常 request body 的 D2 数值类型/UTF8/ASCII
separator 通过 shared jsonresp 原生 device encoder 保持；新 EncodeDeviceUTF8 专供旧 config
endpoint，不用另一份 Python serializer。CallError、PythonClass、私有 Latin-1 exception
renderer、pyTypeName 已删。Call/AgentURL 只转旧 record 到 typed owner；upgrades 的四处
必要接线将 ErrHeaderEncoding 接回其已有的 deferred error classification，未重做 upgrades。

成功的 malformed response 走 DeviceResponseError 502；null/list stored logging_config 为
明确中文 validation refusal，不写入。model.Agent 新增 LoggingConfig/AgentLoggingConfig，
MemberJSON 同步补该声明，避免 unknown Fields 与 declared-field presence 分叉。日志设置
与 direct operations Detach 浏览器断线但保留 Runtime shutdown，CallAgent 再检查 ctx。

PublicDeploymentMessagesModel / PublicUpgradeStateModel 用 native journal/events/redact，旧
函数只 codec；text explainer 现在只收 string，私有 lower 特判改为 x/text cases.Lower。
共享 model.JSONHasContent 用标准 JSON scalars/containers 和 IEEE754 omission 判断旧的
optional display/protocol 内容，不用于身份、所有权或鉴权。dnsfollow/dnsha 的原 Truthy
入口也调用此 helper，没有新增 Python bool/int/float parser 或值比较层。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/remote_ref.py >server/internal/agents/testdata/n810_remote_python.json'
    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_n810_remote_native.py'
    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/agents/testdata/gen_n810_call_preparation.py'

均 Exit 0。原 remote_ref.py 与所有旧 corpus 保留；比较只对九条具体 remote 行、37 条
已有 A15 preparation 行、upgrades 原 archive 四条 encode 行精确 SHA 绑定。曾因新增
LoggingConfig 未同步 MemberJSON 的 switch，使 stored null 被误认 absent；补回共享
member 接线后原 TypeError 两行的拒绝/no-write 通过，没有把期望改成成功。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "DirectAgentCalls|SetLoggingConfig|AgentJSONAndDNSHARequests|JSONBodies|PlainTextLegacy|LegacyErrors|OriginalHAIdentity|ProtocolPayload|MemberJSON" ./internal/agents ./internal/agents/dnsha ./internal/model ./internal/jsonresp'

Exit 0：agents 21.893s、dnsha 1.046s、model 1.028s、jsonresp 1.009s（无匹配 tests）。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go vet -p 4 ./internal/agents ./internal/agents/upgrades ./internal/agents/dnsfollow ./internal/agents/dnsha ./internal/model && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "A15ReviewCall|ReviewLatin1Tokens|DirectAgentCalls|SetLoggingConfig|AgentJSONAndDNSHARequests|JSONBodies|ReferenceMessages|Message|ResponseCopy|OriginalHAIdentity|ProtocolPayload|R04|MalformedListener|UnsupportedListener" ./internal/agents ./internal/agents/upgrades ./internal/agents/dnsfollow ./internal/agents/dnsha ./internal/model'

Exit 0：agents 10.875s、upgrades 18.137s、dnsfollow 115.557s、dnsha 1.040s、model 1.017s
（无匹配 tests）。旧 message / pure response / Latin-1 actual Authorization 期望不改。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "N810MalformedLogging|NativeLoggingDisconnect|JSONHasContent|MemberJSON|A15ReviewCall|ProtocolPayload|OriginalHAIdentity" ./internal/agents ./internal/api/agents ./internal/agents/dnsha ./internal/model'

Exit 0：agents 10.310s、api/agents 4.239s、dnsha 1.050s、model 1.034s。真实 HTTP 六条坏
logging reply 比较原始 POST/path/Bearer/Content-Type/`{"enabled": true}` bytes 和全库
fingerprint；同时覆盖 disconnect、shutdown-in-reply 和旧 generation 再次发起拒绝。

## adapters / shutdown / Unicode 收尾

dnsfollow、ipv6、rulesync 从 ReadAgent(s)/EditAgents、PublishConfig 和 typed deploy
source 读取和提交；不再以 legacy full-config callback 持有核心状态。任务展示和未迁移
monitor、health、deploy hook 的 Record 仅为边界转码。rulesync 的 oxidns consumer 已使用
NewTypedProfile；oxidns/legacy.go 仍由后续 api/agents/client_tools_adapter.go 调用，不能删除。
DNS follow 的 optional profile binding 批量解析默认值，一轮只查一次 default，不按每个
Agent 解码配置；明确 profile 字符串不触及 default 文档读取。

HA owner/journal/child/profile/service/host 原始 JSON 字符串逐项核对；数组、bool、数值
的 R5 投影不能变成身份。原 numeric port 8082 / 8082.0 / exponent 等价继续保留，使用
共享 model.Int 比较实际 JSON number；quoted port/bool/fraction 不冒充 numeric proof。
payload、bundle manifest、rulesync task 使用 exact-key Fields，拒绝重复键，不走标准
结构体的大小写折叠。未知 envelope 字段仍保留。外部 JSON 自查：model record 自定义
codec 以精确字段读取；新增 plain task 入口已改为 exact-key 自定义 decoder。

metrics/traffic 写链传 ctx 到 SQLite connection/query/BEGIN/COMMIT，删除辅助文件也
检查 work lifetime；Detach 只忽略浏览器断线，Runtime shutdown 仍停止未完成写入。
SQLite 回滚使用 5s bounded cleanup，不存 context。新增真实 SQLite 锁等待回归，取消
后 errors.Is(context.Canceled)、无部分 row、pending 增量恢复，随后重试只持久化一次。
metrics/trafficstore 取消删除回归保留原文件。旧 helper wrapper 只服务延后入口，未把整个
A05 SQL/counter/query engine 冒称为已完成 S pass。

DNS follow 首轮整包 race 抓到 typed struct 直接传 redact visitor 后未收集凭据，修复为
owned RawMessage snapshot 进入 internal/redact；FailureMessage 同样收集全部私有凭据，
不只 token registry。原 OutcomeRedactsCredentialsAndPreservesUnknownFields 与
PreparationFailureRecordHidesOtherAgentCredentials 均原样通过；没有放宽测试。

Unicode 按协调者最终裁定只保留 UCD17：n810_unicode17_native.json，SHA
9407e75fcbec0ab213813acb038bb8176834e861181e4358ef642ec26676f268；原 A05 SHA
0e2e8016db1a1976c3af5f4d0c9c3d1dae3e7befe6bf190985df44235a17cca7 不变。55 lower +
307 sigma 行精确绑定旧原文、独立官方 UCD raw evidence/hash，负控逐条证明旧值与伪造
原值不能通过；未绑定的所有码点仍比较原 A05。生产直接 cases.Lower，不带兼容表。
之前已在 .64 GOTOOLCHAIN=go1.26.11 验证 Unicode15 分支 PASS3.896s，随后依最终裁定
删除该 fixture/分支；当前硬断言版本17，工具链升级归 batch-11 集成者，不改依赖版本。

定向收尾证据（所有命令经 rt，未跑完整门禁）：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go vet -p 4 ./... && GOMAXPROCS=4 go test -race -p 4 -count=1 -run "ResolveAgentProfiles|OutcomeRedactsCredentials|PreparationFailureRecordHides|NativeLoggingDisconnect|DirectAgentCalls|AgentJSONAndDNSHA|WorkCancellation|DeleteContextPreserves" ./internal/agents ./internal/agents/dnsfollow ./internal/agents/metrics ./internal/agents/trafficstore && GOMAXPROCS=4 go test -count=1 -run "^TestBatch10HeartbeatAllocationCeilings$" ./internal/agents && GOMAXPROCS=4 go test -run "^$" -bench "^BenchmarkResolveAgentProfiles$" -benchmem -benchtime=1s ./internal/agents && GOMAXPROCS=4 go test -run "^$" -fuzz "^FuzzNativeTrafficDomainCase$" -fuzztime=60s ./internal/agents/metrics'

Exit0：vet；agents9.145s、dnsfollow48.781s、metrics1.142s、trafficstore1.020s；原 batch10
心跳阈值 PASS6.077s；100 Agent 显式 binding 批量解析 empty 29,061B/601alloc、2MiB
29,001B/601alloc，不按无关配置增长；traffic fuzz 497,510 exec /61.014s PASS。

首次全 agents/core race：agents712.485s 失败，定位为 CallAgent 使用 a.Port 投影改变
float/slash URL，以及 legacy 两条坏回包仍要求旧失败对象。端口已改为原始 JSON number
或原始 string 文本构造 native URL，保留原 A01 wire 期望；没有改 corpus。legacy 十条
HTTP200 非对象/plain-text 原文由真实 Python 独立记录，fixture SHA
 ae7cec5313879b171fc86d5cb966cef1a654a29ccd99250f9e86047ee05bfc81，原断言文本不动，
仅这些 exact method/body/status/result 行验证 native ErrInvalidAgentReply + no-write。
原 API 操作 wrapper 已映射中文 INVALID_RESPONSE/502；新增 restart 真实 HTTP 核对
空 body、POST/path/Bearer 和持久化 fingerprint。其余全包首次结果：dnsha376.707s、
ipv6 19.318s、rulesync138.949s、metrics12.633s、trafficstore1.203s PASS。

runtime 删除增加与 heartbeat 相同的锁内 ctx 检查；锁等待期间关机，不移除 runtime entry
或推进 deletion tombstone。metrics memory 删除同样在 flushMu 与 mu 后检查，逐文件
unlink 前检查。ipv6 设备回包用 model.Fields 拒绝重复顶层/嵌套键，避免 map 最后值覆盖。

跨包真实 Python check → record metadata 已通过：mosdns52.038s、oxidns45.786s；本机
仅轻量比较 gzip JSON，12份18,444 exact entries、input SHA 均完全不变。sources.json
补入编译依赖 model/content、jsonresp/device，源码指纹按实际依赖更新，无 Go 造期望。

## 最终风险类与验证增量

- Python 仿真：删除 private time/Unicode Nd/4300位数字阈值、Unicode14 case 表、traffic
  IP parser、urllib3 码点表、核心异常 class/text renderer。标准库/x/text/taskstatus/rules
  负责协议；保留 facade 名称不等于保留第二套核心业务实现。完整 A05 engine 的旧 SQL/
  counters/query 是明确后续边界，未掩饰成 adapter 已全清。
- R5/身份：stored id/profile/service/HA journal child/owner 必须 original string；端口
  身份只比较原始 numeric token 的 integral value，协议发送保留原始 number/string 文本。
  大小写别名不覆盖精确键名，重复键拒绝；独立 raw 输入与预期逐行绑定。
- O9-H2：注册凭据生成/消费同事务，只持久 hash，one-use；错误403及回滚后不可复活
  保留。register optional profile 的机器来源审计与400裁定见前文/behaviour-changes。
- 关机与并发：Detach 保留 shutdown owner；写前、锁内、SQLite commit、逐文件写检查。
  取消SQLite写不丢 pending；runtime删除不跨shutdown推进 tombstone；prepared、租约、
  overlay 与 batch10 owned RawMessage 表示不变。未添加广泛 uninterruptible 窗口。
- 脱敏：统一 internal/redact，所有 private credential 从 RawMessage 进入共享visitor；
  新 direct/HA/HTTP拒绝不回显token。正常心跳不收集全Agent历史或全配置凭据。
- 协议/独立期望：真实 loopback HTTP 核对 method/path/query/header/body原字节；原
  corpus/input/output/SHA不改。UCD、真实 Python SQLite/remote/HTTP输出是独立来源，
  不是Go实现反向生成答案；bound负控保证没有跳过比较。持久化指纹拒绝写入检查保留。
- 分配：batch10 窄runtime/阈值保留；每Agent配置解码改为 registry cached view 或批量
  optional profile default lookup，100Agent2MiB unrelated profile benchmark不增长。

A15 原 differential 十条错误呈现行，在 .32 的真实 Python HTTP 重跑上冻结完整原文与
逐行SHA：n810_operation_error_rows.json（SHA
37564c69e6be56757e22ff114af1d2e470871efbb657233acea9dfb14f0f31cd）。两条 notjson→中文
502；四条 Latin-1→原生 header拒绝；三条缺host→原生 endpoint拒绝；一条NaN请求→
原生失败类别，均不写入，设备请求比较仍执行。原 requests表与 Python 响应原文不改。
closed row whitelist、输入伪造负控和运行时10行执行计数同时检查，未匹配行严格原对照。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 CONFIGFLOW_DIFF=1 go test -p 2 -count=1 -timeout=15m -run "TestUpgradesDifferentialAgainstFlask|TestN810HTTPPolicyRowsCannotBeSkipped" ./internal/api/agents/difftest'

Exit0：18.569s。此前全 agents difftest 的其他 groups 均通过（api总264.104s，只有上述
十条呈现差异；dnsha25.142s、dnsfollow33.037s PASS），十条对照修订后单独重跑通过。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "LegacyDNSGuard|AdapterRejectsDuplicate|RuntimeDeleteQueued|NativeTaskReads|NativeUnicodePolicy|ReferenceAgentURLs|LegacySuccessHTTPRequires|LegacyInvalidJSONKeeps|N810MalformedRestart" ./internal/agents ./internal/agents/ipv6 ./internal/agents/metrics ./internal/agents/rulesync ./internal/api/agents ./internal/gateway/installation'

Exit0：agents87.907s、ipv6 1.462s、metrics1.051s、rulesync1.041s、api/agents2.175s、
gateway/installation1.042s（仅编译）。gateway deferred guard桥转到原始字符串证明，
额外 raw id/source_agent_id 负控覆盖。删去 manager 的私有pyStatuses/无调用旧helpers，
仅 frozen tests 的findAgent helper留在_test.go，不给生产引入第二套实现。

批次依赖：最终只支持 cases.UnicodeVersion=17.0.0；batch-11集成者另提交 go1.27.0 /
Go1.27.4与发行镜像升级，并将既有x/text v0.43.0标direct。本分支go.mod/Dockerfile不动。
不得在旧1.26工具链上绕过版本断言来静默更换域名统计键。

末轮跨包验证：

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go vet -p 4 ./... && test -z "$(gofmt -l internal/agents internal/api/agents internal/deploy internal/model internal/rules)" && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m -run "Corpus|Reference|Golden|NativeUnicodePolicy|NativeTaskReads|AdapterRejectsDuplicate|LegacyDNSGuard|RuntimeDeleteQueued|WorkCancellation|N810Malformed" ./internal/agents ./internal/agents/rulesync ./internal/agents/metrics ./internal/agents/dnsha ./internal/agents/ipv6 ./internal/api/agents ./internal/model ./internal/repo ./internal/deploy ./internal/jsonresp ./internal/rules ./internal/gateway/installation && GOMAXPROCS=4 go test -v -count=1 -run "^TestBatch10HeartbeatAllocationCeilings$" ./internal/agents'

Exit0：vet/gofmt静默；agents133.841s、rulesync1.041s、metrics7.716s、dnsha2.583s、ipv6
1.502s、api/agents11.048s、repo19.653s、deploy3.360s、rules25.613s；model/jsonresp/gateway
编译通过。心跳原阈值 PASS6.299s：public empty/2MiB 6,818/6,783B，153alloc；native
7,218/7,182B，160alloc；没有用放宽门槛迁就新增代码。

同 selector 的 mosdns/oxidns 真实 Python metadata刷新后check回放已通过52.268/45.869s；
其后首次gofmt检查发现稍后新增bridge未格式化，已远端gofmt且以上末轮检查静默通过。

最终 agents 全子包 race（定向域范围，并非完整门禁）：

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=6 go test -race -p 6 -count=1 -timeout=60m ./internal/agents/...'

Exit0：agents799.806s、agentfake1.040s、dnsfollow941.147s、dnsha472.408s、ipv6 23.158s、
metrics14.516s、monitor260.182s、rulesync135.135s、trafficstore1.216s、upgrades46.468s；
其下 difftest 默认跳过 live 项（真实 Python 另跑），无race。开始此运行后只补了机械 facade
对原生 guard 的接线及 HTTP 对照行；其 targeted race、A01 raw wire、真实 A15 HTTP 和
upgrades 全包race46.147s另通过。没有重复跑全仓门禁。

最后的默认配置审计发现 rulesync 等逐Agent optional binding 仍可触发超大文档的重复
DefaultProfileID 读取。最终用 repo.AgentDefaultProfileID 的每generation coalesced标量
缓存解决，manager入口统一接入；事务proposal/pinned snapshot不发布到全局缓存。
冷读取仍经过既有 readEntry 的全库结构校验/R5诊断，一代最多读取一次，不能用只取
system_config 行来绕过 default profile existence 等校验。未新增私有 JSON scalar parser。

新增独立raw/want包含 absent/null/string/numeric/default root坏型，使用store原始导入
fixture（正常writer本来拒绝坏类型/不存在的默认profile，不能拿writer制造非法存储）；
合法profile预先建好。另8个并发冷读、default切换后一代再读，要求每代恰好一次full-load。
16MiB unrelated配置超过全库缓存预算，100个optional Agent真实热路径分配为7,017B /
115alloc，empty为7,031B /115alloc；原+32KiB/+100alloc回归阈值原样通过。心跳也再跑：
public6,804/6,774B153alloc，native7,197/7,175B160alloc，原阈值通过。

    RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "AgentDefaultProfileUsesScopedRowView|ResolveAgentProfilesUsesOriginalBindings" ./internal/repo ./internal/agents && GOMAXPROCS=4 go test -v -count=1 -run "OptionalAgentProfiles|Batch10HeartbeatAllocationCeilings" ./internal/agents && GOMAXPROCS=4 go vet -p 4 ./... && test -z "$(gofmt -l internal/agents internal/api/agents internal/deploy internal/model internal/rules internal/repo)"'

Exit0：repo2.225s、agents2.544s；分配回归与心跳阈值9.948s；vet/gofmt静默。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=20m ./internal/agents/upgrades && GOMAXPROCS=4 CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=30m ./internal/api/agents/difftest'

Exit0：upgrades46.147s、全API agents difftest262.904s；device request与存储/文件终态
严格比较，前述十条错误类别只有精确绑定比较，无额外差异。

    RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && GOMAXPROCS=4 go test -race -p 4 -count=1 -timeout=15m -run "AgentDefaultProfile|ResolveAgentProfiles|OptionalAgentProfiles|Registration|Register|AgentMutations|ProfileGuard|LegacyDNSGuard" ./internal/repo ./internal/agents ./internal/agents/rulesync ./internal/agents/dnsha && GOMAXPROCS=4 go run ./tools/pyref -scopes server/internal/mosdns,server/internal/oxidns && GOMAXPROCS=4 go vet -p 4 ./... && test -z "$(gofmt -l internal/agents internal/api/agents internal/deploy internal/model internal/rules internal/repo)"'

Exit0：repo3.794s（含每代8并发冷读只加载一次）、agents127.771s（含16MiB分配、注册/
编辑/原始绑定证明），rulesync1.039s、dnsha1.050s编译；pyref audit无未登记oracle或
source遗漏，vet/gofmt静默。此前全agents子包race及全API difftest结果保留；默认值收尾
只增加有代际校验的读取memo，事务/固定snapshot分支仍直接读取其已有model。

交回：分支 go-n8-10-core-2；最后rebase到本地go@d9515f7fe（相对12150a402仅协调
handoff文档提交，无代码变化）。现行提交id以git log为准，所有提交保留指定Co-Authored-By。
状态仍为“部分完成”，等待协调者另派fresh Opus HR。未push、未开PR、未访问生产，未跑
完整门禁。明确的工具链依赖与后续动态边界见报告开头和“批次依赖”。
