# N8-10 agents core-2

状态：部分完成（进行中）。分支 go-n8-10-core-2 已 rebase 到 go@12150a402。
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
