# N8-10 api/agents S pass

状态：本轮授权边界已完成并提交，定向验证通过，待独立复核，不标 verified。分支 `go-n8-10-api-agents`。
已按协调者要求先接 upgrades HR@82adf451c，再执行
`git rebase --onto origin/go f8df41fb1`；当前基底 `origin/go@5c5998e86`，
分支同时包含 upgrades + HR 修复与本轮 API 提交，monitor 已进入并接 typed 接口。
不 push、不跑完整门禁，不改依赖、go.mod、Dockerfile 或分配阈值。

## 完成边界

- 注册、手动创建、编辑、心跳使用 core native registry/model DTO；body 经 c.Bind /
  jsondecode 精确键名，机器数字/版本仅 ASCII，原始 JSON string 才能授权。
  注册版本由 RegisterIdentity 同一事务在授权之后、消费 proof 之前验证：缺失/null
  默认1.0.0，其余非法版本400；匿名非法版本仍403，失败不消费一次性凭据。
- content/public/client adapters 改用 ReadAgent(s)、ResolveAgentProfile、repo.Config /
  UpdateProfile / UpdateShared、rules.IPTargets / AppendIP 和 native OxiDNS profile。
  public 投影在删除私有任务树前捕获完整凭据，统一经 redact；summary 同样安全。
  单对象坏存储绑定拒绝写入；列表/汇总不因坏版本或一个坏绑定整页失败。
  discovery clear 的 traffic/probe/region 与 traffic history 删除复用已存在的 DeleteContext，
  不再丢掉已 Detach 的 shutdown lifetime；未更改 owner。
- installer、Docker HA 接 typed owner renderer / guide / capability / enroll。
  保留 body limits、capability 在校验之后签发、各场景 HTTP 类、下载与 HEAD 行为。
  monitor 接 TypedFromAgents、ReadOverview/Clients/Queries/Query/ResetStatistics、
  CallPanel/CallRuleUpdate/CallCache；tags 错型400/NoStore且无 Agent 请求/写入。
- 远程日志/操作接 RemoteCall/SetLoggingModel/RunServiceOperation；HA 成员仍经过原租约、
  Detach 和冲突规则；typed 与旧 hook 不会同一事件双触发。设备日志 query 复用 core helper，
  无 API 私有编码器；无效 logging payload 保留既有 endpoint 错误优先级。
- 下载日期改 http.ParseTime，三种标准 HTTP 日期保持；无效条件日期忽略，Range 沿既有416
  裁定。原 pyref entries/input/venv 不变，仅增加 helper source metadata。
- grep 产品 api/agents：仅 traffic.go 仍 import jsonx；无 pystr/PythonClass。
  **本轮不声称 api/agents 整包全 native，也不声称 N8-10 全部完成。**

## 协调者批准的公共 API 增量

- `Hooks.InstallationProfileTyped(ctx, data json.RawMessage, installationID,
  installationToken, fallbackHost string) (profileID string, updated json.RawMessage, err error)`。
- `Hooks.InstallationRegisterTyped(ctx, manager, installationID, installationToken string,
  data json.RawMessage, existingToken string) (json.RawMessage,error)`。
  hook 字段命名参数/注释标明顺序；不同 id/token/existing/fallback 回归及旧 hook 不调用断言。
- `Manager.RunServiceOperation(ctx,id,DirectOperation,http.Header,[]byte)`、
  `Hooks.HAServiceOperationTyped`、`dnsha.ServiceOperationJSON`：原租约 owner，旧入口仅转码委托；
  成员 lease 被占用时 restart/uninstall 仍409，零设备操作。
- `ValidMachineVersion` / `ErrRegistrationVersion`：统一授权后的注册版本校验。
- `AgentLogsPath`：复用既有 query quote；`Manager.RemotePayloadError`：只读既有 endpoint guard。
- `oxidns.Profile.DeviceNotesJSON()`：只读 owner notes accessor，无额外校验或状态机。
  API 的 RegistrationLogFields 现在接收 raw JSON 并返回 native string map。

## Python 证据与新规则

不改旧请求表、旧结果字面量或 pyref entries。新增期望逐行 SHA 绑定完整原输入/旧结果；
独立 Python/Flask 生成 native 对照，不读取 Go 输出：

- 396 条下载日期/条件请求政策，4 条人工超范围心跳旧500→400。
- 36 条 HTTP 原结果、7 条完整存储 Agent host 行、5 条真实条件下载旧500行。
- 272 条安装器 HTTP 人工矩阵差异，完整原请求及旧结果压缩保留；第二个 Flask 服务仅注入
  Go whitespace、ASCII integer 和原生 binding 的稳定键顺序，完整比较脚本/headers/body；
  原始完整随机矩阵仍执行，没有缩短 corpus。JSON 字符串转义比较只规范化定界 JSON，
  shell 其余字节保留，负控证明不吞 shell 或配置值变化。
- 原非对象诊断、Unicode query 与4300位上限、日志版号人工输入等旧 unit 期望保留，
  新 policy 明确绑定原行；英文解释器细节改为 native 诊断，HTTP 类保持。
- cache dict tag500→400及注册 version400/匿名403由协调者明确批准；
  traffic-report/client tools 的坏存储绑定在写前用现有 owner 拒绝，禁止落到 default。
  policy peers 坏一条跳过，按Agent十分钟节流并经redact告警。输入/旧结果变化有负控。

具体差异登记 `server/parity/behaviour-changes.md`。

## 保留边界：N8-10 补充，A05 engine

协调者已另排 `go-n8-10-metrics-native`；本分支不改 metrics/trafficstore owner，不在 API
新建 codec/第二引擎。其 native 服务完成后再切换下列消费方，且必须在 N8-11 删除 facade 前完成：

| API 函数 | 当前依赖及原因 |
| --- | --- |
| agentTrafficReport / readLegacyTrafficBody | trafficstore.CleanReport / AddReport 仍接 jsonx；写前新增 ResolveAgentProfile。AddReport 无ctx，Detach/入场/锁后/提交前检查及关机无写缺口由 metrics-native owner 修，再接 API |
| agentTrafficHistoryReport | metrics.CleanTrafficReport / TrafficHistory.AddReport，无ctx旧入口；同上，待 ctx native Add |
| trafficHistoryState | RetentionDays、旧 since/版本投影；待 owner typed state |
| getAgentTrafficHistory | TrafficHistory.Query 及旧结果 envelope；待 QueryTraffic(ctx,...) |
| setAgentTrafficHistory / clearAgentTrafficHistory | 旧请求/状态 envelope；删除已接现有 DeleteContext，待 native 状态服务收口 |
| setTrafficHistoryRetention | SetRetentionDays 旧输入转换；待 SetTrafficRetention(ctx,...) |
| getAgentMetricsHistory / getAgentMetricsSummary | History.GetMetrics / GetMetricsSummary 仍返回旧 owner 的 opaque 值；HTTP registry/envelope/query 已native |
| getAgentTrafficStats / getAgentTrafficTrend | History.GetTrafficStats / GetTrafficTrend 及 opaque 结果；待 owner native query |

## N8-11 删除 jsonx/pystr/legacy 前的邻居清单

这是仍阻止删除的产品消费链，不把只包一层 typed 参数的内部 legacy owner 算完成：

- `deploy/adapter.go`：SourceFromJSON/NewTypedTarget 内部 source/target codec、GetAgentByID、
  RecordDeployment、PushLegacyConfig；`publish.go` BeginDeployment；`orchestrate.go`
  BeginDeployment/RestartAgentService；receipt/publication/task adapter 与 legacy source 操作。
- `gateway/installation/adapter.go`：GetAgentByID/Agents、UpdateAgents、RecordDeployment、
  repository legacy transactions；`publish.go` BeginDeployment；`service.go` 的 Render /
  installer.GatewayOptions / GenerateGatewayInstallScript。注册/wire 已typed，其余另迁。
- `gateway/host/host.go`：GetAgentByID 与 raw config/forwarding bridge；
  `server/setup.go` 的 bridge.Call；`configexport/export.go` Sanitize/sanitize/jsonx.Parse。
- `discovery/legacy.go`：GetAgentByID/GetAllAgents；`api/clientdevices/handlers.go` 同类读取；
  `api/routers/adapters.go` GetAllAgents、`api/routers/agent_return.go` GetAgentByID。
- `agents/rulesync/adapter.go`：monitor.RuleUpdateCall/PanelCall/FromAgents 及 task/deploy envelopes；
  `agents/dnsha/adapter.go`：monitor.SetHARules 的 legacy manager callback、receipt/publication adapter。
  这些调用方尚在，monitor 的 *_legacy.go 不能删。
- core 旧 installation/address/HA service hooks 与 receipt/HA deployment/activation/publication hooks、
  registry/remote/deployment facades；按上述实际消费者逐个迁移再删，typed hook 已优先。
- 测试/proddiff/语料 codec 仍有 legacy 依赖，删除门禁另审；不能按产品 import 计数直接删库。

## 新基底定向验证

所有命令只经 rt 在192.168.0.32/.64执行，GOMAXPROCS=4、-p 4；不在Mac测试。

已确认的宽 race：core840.336s、gateway/installation77.763s、dnsha418.338s、deploy405.120s PASS。
同轮 API/OxiDNS 暴露旧英文/数字期望和source metadata；保留旧期望、绑定新规则后重检，
不把首次失败算通过。前期 installer+O9 race92.292s、注册/版本授权/A13/HA租约等定向组通过。

最终定向结果：

- .64：`PYREF_MODE=record PYREF_ACCEPT=1 go test -p 4 -count=1 -run
  "^TestReviewConditionalMatchesWerkzeug$|^TestStoredLegacySettingsAgainstPython$|^TestBoundaryRoutingCorpus$|^TestBoundaryGenerationCorpus$|^TestC05ReviewRecordedNormalization$|^TestBoundaryGenerationRecorded$|^TestC05ReviewSettingsReferences$" ./internal/api/agents ./internal/oxidns`
  仅刷新 source metadata；随后相同选择 `PYREF_MODE=check` PASS，API0.241s、OxiDNS11.798s。
  以 origin/go 原文件独立核对9个旧 pyref archive，entries、input_sha256、venv_dependencies_sha256全未变。
- .64：`go test -race -p 4 -count=1 -timeout=10m -run
  "N810|A15ReviewArgInt|RegistrationLogsOnly|ManualAgentCreate|AgentItemUpdate|Client.*|PublicAgent.*|Installation.*|O9" ./internal/api/agents`：PASS140.027s。
- .64：最后 DeleteContext 接线后的
  `go test -race -p 4 -count=1 -timeout=10m -run
  "ClientNoteAndPolicyCancelledOwners|ReviewA24ShutdownRejectsWrites|N810Client|N810PolicyPeers|TrafficHistory|TrafficReport" ./internal/api/agents`：PASS12.072s。
  断线继续完成、真实 Runtime shutdown 拒绝写入的既有回归保持。
- .32：两个新增 fuzz 分别 `go test -p 4 -count=1 -run "^$" -fuzz
  "^FuzzN810InstallCommandBinding$"` / `"^FuzzN810HeartbeatBinding$"`，均 `-fuzztime=60s
  -parallel=2 ./internal/api/agents`：PASS61.033s/60.053s，365763/495438 executions。
- .32：`go vet -p 4 ./...` 静默；`go test -count=1 -run "^TestAuditRepository$"
  ./tools/pyref` PASS0.870s；`go test -count=1 -run "^TestBatch10HeartbeatAllocationCeilings$"
  ./internal/agents` PASS6.277s，原阈值未改。
- .32：`CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=10m -run
  "^TestPublicAgentPython$|^TestA24FlaskBoundaries$|^TestClient.*|^TestN810Client.*|^TestN810PolicyPeers.*" ./internal/api/agents`：PASS6.903s。
- .64：DeleteContext 最终接线后再次 `go vet -p 4 ./...` 静默，`gofmt -l .` 无输出。

- .32：`CONFIGFLOW_DIFF=1 go test -p 4 -count=1 -timeout=15m -run "^TestDifferentialAgainstFlask$|^TestUpgradesDifferentialAgainstFlask$|^TestPanelDifferentialAgainstFlask$|^TestDockerHADifferentialAgainstFlask$|^TestInstallerDifferentialAgainstFlask$|^TestDownloadDifferentialAgainstFlask$|^TestContent.*|^TestOxidnsDifferentialAgainstFlask$|^TestRankingCollectorDifferentialAgainstFlask$|^TestN810InstallerHTTPBindingRejectsChangedInput$" ./internal/api/agents/difftest`：PASS127.984s。
  原始 normal/error/HEAD/设备请求 trace 与存储后效均经对应套件比较。
- .32：安装器完整随机矩阵独立 native Flask 录证运行 PASS130.570s；无缩短phase/iterations。
- .64：冻结后 `CONFIGFLOW_DIFF=1 go test -v -p 4 -count=1 -timeout=10m -run "^TestInstallerFuzzAgainstFlask$|^TestN810InstallerHTTPBindingRejectsChangedInput$" ./internal/api/agents/difftest`：PASS104.782s；4538 equal / 0 differ，完整使用272条冻结原行（245 command + 27 script）。原矩阵和phase数量未改。

最终 .64 gofmt -l . 无输出、go vet -p 4 ./... 静默、TestAuditRepository 再确认PASS0.691s。
完整门禁和独立复核由协调者排队。
