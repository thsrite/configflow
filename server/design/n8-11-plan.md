# N8-11 拆分计划：删除 jsonx / pystr / legacy 过渡层

状态：设计（Opus，2026-11-19），只读盘点于 `go` 系分支 `go-n8-11-plan` @ `9a69b42b5`。
上游：[n7-n8.md](n7-n8.md) §4 表格 N8-11 行（原估 S）、§2.5 repo、§2.6 httpx；O3 并发规则见
[o3-concurrency.md](o3-concurrency.md)。所有数字由 grep 得到（包名.函数名 文本匹配；方法按名字匹配，
同名方法可能有少量误报），派工时每包作者须在自己的基底上重新 grep 确认。

结论先行：N8-11 不是 S。生产代码仍有 **157 个文件**导入 `jsonx`（另 6 个导入 `pystr`），测试有
**602 个文件**导入 `jsonx`。N8-8/9/10 把各包的**核心**改成了 typed，但旧签名的**消费方**（API 处理器、
deploy/gateway/setup 接线、proddiff 工具、Agent 子包互调）大多还走 legacy 入口；所以删除前要先把这些
消费方迁走。拆成 **1 个准备包 + 8 个工作包 + 1 个删除包**，分三波，每波 3–4 个可并行。

---

## 1. 现状清单

### 1.1 总量

| 项 | 生产文件 | 测试文件 | 备注 |
|---|---|---|---|
| import `internal/jsonx` | 157 | 602 | 测试中引用最多：`jsonx.Object` 3233、`Obj` 3068、`Equal` 443、`Repr` 359、`Int` 356、`Parse` 328 |
| import `internal/pystr` | 6 | 1 | `pystr` 本身 5629 行（`tables.go` 5157 行 Unicode 表） |
| import `internal/pyyaml` | 0 | 0 | 包已删；仅 `yamlx/architecture_test.go` 作门禁引用 |
| `PythonClass()` 实现/使用 | 8 文件（19 处） | 13 文件 | weburl.ClassError 接口 + refresh/rulefetch/ippresence/gwbase 实现 |
| `raise(`/`catch(`/`dictGet(`/`mustValue(` | 2 文件 | – | `convert/mihomo_generate_py.go`（除 `raise` 外只自引用，已是死代码）、`convert/legacy.go:1239` 一处 `raise` |
| `PyError`/`pyError` | 5 文件 | – | convert/errors.go、pyhelpers.go、ippresence、deploy/adapter.go:780、gateway/onboarding/adapter.go:285 |
| `*legacy*.go` / `*adapter*.go` 生产文件 | 82 | – | 其中 10 个已不导入 jsonx（见 1.4） |
| 其他 Python 仿真文件 | – | – | `rulefetch/pyhtml.go` 471、`rulefetch/pyerrors.go` 107、`httpx/pyconv.go` 105、`convert/pyhelpers.go` 306、`agents/monitor/pyip.go`+`pydigit_table.go`+`pydecimal_table.go` 73 |

导出的 legacy 顶层函数共 554 个（文件×函数）；其中 **283 个已无生产调用方**（只剩测试或包内调用），
属于 (a) 类，可以随所属包直接删。

### 1.2 按 owner 的生产依赖与真实调用链

分类：**(a)** 只剩薄转码、已无生产调用方，直接删（连同测试改到 typed）；**(b)** 还有生产调用方，要把调用方
迁到已有或待补的 typed/native API；**(c)** 测试/工具依赖，见 1.3。

#### repo（18 文件；被几乎所有消费方调用）

- 核心已 typed（`Profile/Shared/System/Config/View/Update*/UpdateSharedAndProfiles/PublishConfig/
  SaveRouterPublicationJSON` 等），但**仓库内部仍有大量 jsonx**：`importexport.go`（80 处，ExportAll/
  ImportAll/ResetAll/convertImport/scopeLegacyDialers）、`profiles.go`（64）、`scope.go`（32，旧
  `GetConfig/SaveConfig/UpdateConfigTransaction/GetSharedConfig/GetSystemConfig/SafeImportConfig/
  GetResourceConfig/PrepareConfig`）、`system.go`（`Updater` 类型 = `func(ctx, *jsonx.Object)`、
  `SanitizationTokensOf`、`AgentHistory`、`GetSystem`）、`shared.go`（`CreateRuleLibraryItem`、
  `PreviewNodeDeletion/DeleteNodesWithReferences(nodeIDs any)`、`SaveShared`）、`defaults.go`
  （`GetDefaultConfig/GetInitialConfig/GetDefaultOxidnsSettings` 产出 jsonx，n8-5.md:152 已记）、
  `store_rows.go`、`validate.go`、`merge.go`（`DeepMerge`）、`files.go`（`readJSON`/`ReadSharedJSON`/
  `ReadProfileJSON`，且用 `pystr.UTF8DecodeError`）、`legacy.go`（转码层本体）。
- **(b) 缺 typed 对应、阻塞消费方的入口**：
  - `ExportAll/ImportAll/ResetAll` ← api/config/global.go、webdav/adapter.go、gateway/installation/adapter.go
  - `GetInitialConfig/GetDefaultOxidnsSettings` ← setup/presets.go 等
  - `PreviewNodeDeletion/DeleteNodesWithReferences/CompatConfigFrom` ← api/nodes/adapter.go
  - `CreateRuleLibraryItem` ← api/rulelibrary/adapter.go
  - `ReadSharedJSON` ← refresh/owners_legacy.go、health/legacy.go、convert/mihomoenv/legacy.go
  - `CloneProfile/ExportProfile/ImportProfile/ImportClientProfile/UpdateProfileMetadata` ← api/profiles
  - `GetCompatConfig` ← clients/fixed.go、deploy/publish.go、deploy/orchestrate_adapter.go、api/routers/adapters.go、
    api/clientdevices、gateway/host/{host,forwarding}.go、tools/proddiff/baseline
  - `GetPublishSnapshot` ← configgen/candidate.go、deploy、setup、api/ipv6、gateway/{draft,installation,onboarding}
  - `UpdateProfileTransaction`（9 个外部文件）、`UpdateSharedTransaction`、`UpdateSystemTransaction`（6 个）
  - 包级 `GetConfig`（9 个外部文件）、`GetSharedConfig`（5）、`GetSystemConfig`（7）、`UpdateConfigTransaction`、
    `UpdateSharedConfigTransaction`、`UpdateSharedAndConfigTransaction`、`SaveConfig*`、`GetResourceConfig`
- **(a)** 无外部调用：`GetCompatConfigWithSource`、`ReadProfileJSON`、`SaveRouterSettings`（旧版）。
- 不受影响、保留：`repo/errors.go` 的 `IsValueError/NewInUse/NewValidationError`、`ValidateProfileID`、
  `WithRepository/WithScope/ResolveProfileID/DataDir` 等（这些文件只需去掉对 jsonx 的零星引用）。

#### agents 核心 + 子包（32 文件；HR）

- 核心 `Manager` 上仍是 jsonx 签名的方法及外部调用方：
  - `GetAgentByID` ← routers/agent_return.go、deploy/adapter.go、discovery/legacy.go、monitor/cache_legacy.go、
    rulesync/{worker,service}.go、api/clientdevices、gateway/host/host.go、gateway/installation/adapter.go
  - `GetAllAgents` ← discovery、dnsfollow、monitor/rankings_legacy、rulesync/service、api/routers、api/clientdevices
  - `Agents` ← notifications/{legacy,notifications}.go、gateway/installation
  - `BeginDeployment` ← deploy/{publish,orchestrate}.go、rulesync/adapter.go、gateway/installation/publish.go
  - `RecordDeployment` ← deploy/adapter.go、gateway/installation/adapter.go；`DeploymentRequest` ← deploy、dnsfollow、ipv6；
    `GetDeployment/ActivateDeployment/RollbackDeployment/RecoverDeployment` ← api/agents/deploy.go（同名 typed
    方法可能已存在，派工时核对）、gateway/installation、ipv6/wire、dnsha/hooks（`*Direct` 变体）
  - `PushLegacyConfig` ← deploy/adapter.go；`RestartAgentService` ← deploy/orchestrate.go；`RebindProfileAgents` ← api/profiles
  - `ProbeDomains/RegionTargets/RegionCheck` ← discovery/legacy.go；`ReturnRouting{Status,Apply,Remove}` ← routers/agent_return.go
  - `Call` ← server/setup.go（bridge.Call）、gateway/host、gateway/installation/network.go、api/agents/gateway.go、routers/ikuai
- **(a) 无生产调用方**：`UninstallAgent`、`UpdateAgentVersion`、`AgentJSONRequest`、`OxidnsHA{Request,Status,Preview,
  Apply,Remove,Handoff}`、`GetAllAgentsInspect`、`GetAgentByToken`、`UpdateAgent`、`UpdateHeartbeat`、
  `SetDomainDiscovery`、`SetTrafficHistory`、`RuntimeState.{Overlay,RecordHeartbeat,Seed}`；旧 hook
  `AddressFollow/AddressConfirmed/InstallationProfile/InstallationRegister` 只剩注册入口，`HAServiceOperation`
  旧 hook 只作回落（review-n8-10-api-agents L1、review-n8-10-upgrades L2：删 hook 时同步 `agents/doc.go`
  并让 `RegisterHooks` 不再"只能新增"）。
- monitor 的 9 个 `*_legacy.go`（1004 行）：外部生产调用只剩 `PanelCall/RuleUpdateCall/FromAgents` ← rulesync/adapter.go、
  `SetHARules` ← dnsha/adapter.go、`SupportsPaths` ← rulesync/worker.go、`AgentOrigin` ← rulesync/snapshot.go、
  ipv6/adapter.go、gateway/host/host.go。其余是测试 codec。
- rulesync/dnsha/dnsfollow/ipv6 的 `adapter.go`：调用 `deploy.SourceFromJSON/NewTypedTarget/NewTarget/NewSource`、
  `tasks.History/Row`、monitor legacy、`agents.SanitizePublicMetadata/ObservePresence`。
- trafficstore/legacy.go 剩余 facade（n8-10-metrics-native.md「N8-11 留存」）：`Load/LoadStatus` ← discovery/legacy.go；
  `StatusOf/Get` ← configgen/generate.go、deploy/adapter.go、tools/proddiff/generator/main.go、
  api/agents/client_tools_adapter.go（仅 DeleteContext，可直接换 GetNative）。
- 同簇的早期消费者 legacy：notifications/legacy.go（`SetSources` ← agents/singleton、server；`RecordRouter` ← api/routers）、
  tasks/legacy.go（`SetRows` ← agents/singleton；`History/Row` ← dnsfollow、rulesync；`AvailableTasks` ← server）、
  taskstatus/legacy.go（`ParseTime` ← routers/rule_sync*.go、refresh/timing.go）、weblistener/legacy.go。
- 已无 jsonx、但仍是过渡层：`agents/events_legacy.go`、`agents/registration_legacy.go`（`RegisterAgent` 等旧注册
  入口）、`installer/options_legacy.go`（`GenerateGatewayInstallScript` 等 ← gateway/installation/service.go；
  n8-10-installer.md「随 N8-11 删除」）。`installer.identityJSON` 是保留的 D2 身份编码，**不删**。
- api/agents 生产代码已不导入 jsonx；只剩 `client_tools_adapter.go` 调 trafficstore facade。

#### 资源 API 簇：subscriptions / aggregations / nodes（15 文件）

调用链：api/subscriptions/adapter.go（539 行）、api/aggregations/adapter.go、aggregations/adapter.go、
api/nodes/{adapter,latency}.go → 以下 legacy 入口：

- `subs`：`FilterSubscriptionItems`（6 个调用方，含 refresh/owners_legacy、health/legacy、setup/generation_adapter、
  api/proxygroups）、`ValidateRegexFilter`、`ExtractSubscriptionHosts/Dialers`（← refresh/owners_legacy）、
  `ParseURIList`（← convert/loon.go、surge.go）、`ReportedCoreVersion`（← deploy、proddiff）、`OutdatedAgents`（← configgen）、
  `RelinkGroupListeners`（← api/proxygroups）。
- `substore`：`LoadSubscriptionCache`（7 个调用方）、`Converter`（6）、`ProxiesToNodes`（4）、`SaveSubscriptionNodes/
  ParseProxiesFromYAML/ParseUserinfo`（← refresh/owners_legacy）。
- `provider`：`Capture/ParseProviderProxies/CommitCacheUpdates/PrepareProviderBundle/CommitProviderBundle/ServeProvider/
  PrepareSubscriptionProvider`（← api/subscriptions、api/aggregations、aggregations、deploy、gateway/installation/publish、
  convert/mihomoenv、proddiff、yamldiagnosticoracle）。
- `refresh`：owners_legacy（`LoadHealth/RecordFetch/Forget/SourceCacheMetadata/FetchSubscriptionSource` ← api/subscriptions、
  setup/abandon）、scheduled_legacy（`RefreshSubscription/SubscriptionKey` ← api/subscriptions；`RefreshRuleSource/
  RuleSourceKey` ← api/rulelibrary；`ReadRuleSource` ← mosdns、deploy、api/rules；`DescribeFailure` ← mosdns、api/rulelibrary）、
  interval_legacy（`ValidateRefreshInterval` ← api/tasks、api/config/custom、api/subscriptions、mihomoenv）、env_legacy。
- `health/legacy.go`：6 个入口全部只被 api/nodes/latency.go 调用。
- `health/check/legacy.go`：`ProviderDefaults/GroupDefaults/InitializeGroupHealth/ValidateHealth` ← api/proxygroups、
  repo/legacy；`InitializeProviderHealth` ← api/subscriptions、api/aggregations；`ProviderHealthCheck` ← convert/legacy；
  其余 12 个只被 discovery/legacy.go 调用。
- `auth/legacy.go`：`ValidateTokenOrJWT` ← api/config/live、api/linuxgateway、api/subscriptions、api/aggregations。

#### 规则簇：rules / rulefetch / api rules / rulelibrary / proxygroups（15 文件）

- api/rules/{adapter,read_adapter,compose_adapter}.go、api/rulelibrary/adapter.go（643 行）、api/proxygroups/adapter.go →
  `rules.ResolveNewOrder`（也被 api/nodes、api/subscriptions、api/aggregations 调）、`FilterCandidates`（← setup/strategy、
  api/subscriptions、api/aggregations）、`RenderMembers`（← oxidns/legacy）、`RuleCacheFilename`（← repo/shared.go）；
  `rulefetch` cache/content/fetch/website legacy（`ReadValidatedRuleCache/RuleCacheReadPath` 也被 ippresence、
  gateway/adapter 调；`NormalizeMihomoRuleset/ValidateRemoteRuleContent/ResolveRemoteRuleSource/RequestRule/
  IsInternalRuleURL/GithubDownloadCandidates` 被 deploy、mosdns、setup/presets 调；`NormalizeRuleFetchProxy` ←
  substore/dependencies、api/settings/compat）。
- rulefetch 另有 `transport.go`、`pyhtml.go` 用 `pystr`，`pyerrors.go`/`fetch.go` 有 `PythonClass`；n8-9-dns.md:86
  记录 `archive.go:safeDetails`、`ruleproxy.go:isBadRequest` 两处 PythonClass 判别暂留。

#### 配置/档案/设置 API 簇（26 文件）

- api/config/custom.go（22 处；`repo.GetConfig/SaveConfig/SaveConfigWithPublishGuard/UpdateConfigTransaction`、
  `convert.EasyTierDefaultSettings/NormalizeEasyTierSettings/ValidateBaseSettings/TakeoverMode/AgentContext/
  ValidateIPv6Flags/DefaultMihomoTemplate`、`refresh.ValidateRefreshInterval`）、api/config/global.go（ExportAll/ImportAll/
  ResetAll）、api/profiles/profiles.go（26 处；repo profile 旧方法、`configexport.Sanitize`、`agents.RebindProfileAgents`）、
  api/settings/*（settings.go 30 处，`repo.GetSystemConfig/SaveSystemConfig`、webdav、`httpx.PyTypeName`）、
  api/generate、api/clientdevices/handlers.go、api/routers/adapters.go、api/tasks/legacy.go、api/auth/auth.go、
  auth/legacy.go、webdav/adapter.go、configexport/export.go、clients/fixed_repo.go。
- httpx：`ctx.go` 的旧请求出口 `JSON()/JSONSilent()/JSONObject()`（jsonx.Parse）仍有 **10 个生产文件、15 处**调用：
  api/aggregations(2)、api/clientdevices(1)、api/nodes/crud(1)、api/nodes/latency(3)、api/proxygroups(2)、
  api/routers/handlers(1)、api/rulelibrary(1)、api/rules/adapter(2)、api/subscriptions/models(1)；`JSONForce`/
  `JSONObject` 已无生产调用。`httpx.JSON/JSONStatus/Message`（响应侧，45 个文件）**不是**删除对象：它们已走
  N8-1 的单一 native 出口，只需去掉 `response.go` 中 `jsonx.Obj`/`jsonx.Float` 的内部引用。`errors.go` 的
  `itoa` 用 `jsonx.Int`、`pyconv.go` 的 `PyParseInt/PyTypeName`（← auth/auth.go、api/settings/settings.go）、
  `jsonvalue.go` 对 `*jsonx.Object` 的分支，均随最后删除。`httpx/httpxtest` 也导入 jsonx。

#### 部署/网关/setup 簇（25 文件；HR）

- `deploy/adapter.go`（1011 行、40 处）是**collaborator 接线中枢**：`Wire()` 通过 convert（`GenerateMihomoConfig/
  GenerateSurgeConfig/GetMihomo*Downloads/RulesetFormat/NewConversionResults/WithNodeConversions`）、mosdns legacy、
  oxidns、provider、refresh、routersplit（`ReadMarker/IsFullTun/RequireFullTunGenerated/GatewayWasUsed/
  PublishLegacySource`）、rulefetch、trafficstore、`agents.Manager` 旧方法接线。`NewSource/NewTarget`（jsonx）仍被
  rulesync/worker、ipv6、gateway/installation/publish、dnsfollow、proddiff 调用；`SourceFromJSON/NewTypedTarget/
  CanonicalDigest` 已是 RawMessage/typed，**保留**。`content_adapter.go`、`orchestrate_adapter.go` 同理。
- gateway：adapter.go（31 处；`NewConfig/SettingsOf` ← deploy、setup、api/linuxgateway、draft/host/onboarding）、
  draft/adapter.go、host/{host,forwarding}.go、installation/adapter.go、onboarding/{adapter,service}.go、wire.go、config.go；
  `gwbase.LinuxGatewayError.PythonClass`。onboarding/adapter.go:285 用 `convert.PyErrorClass`。
- setup（10 文件）：`generation_adapter.go`（convert/substore/subs 旧入口）、`prepare.go/service.go/completion.go/
  action.go/abandon.go/base.go/strategy.go/digest.go/repository.go`（repo 旧事务、routersplit legacy、gateway 旧配置）；
  setupflow/wire.go；server/setup.go 的 `bridge.Call`。
- routersplit/legacy.go 的 19 个入口只被 deploy、setup、gateway、ippresence/wire、setupflow 调用；routers/legacy_wire.go。

#### 生成/导出簇（13 文件 + 工具）

- convert/legacy.go（1577 行、124 处）：外部调用见上（api/config/custom、api/subscriptions、api/aggregations、
  aggregations、api/proxygroups、api/nodes、setup、gateway、deploy、configgen、mosdns、ippresence、repo/legacy、
  repo/importexport、server、proddiff）；n8-8b.md「Temporary adapter inventory」和 n8-8c.md 表是删除清单。
  convert/mihomoenv/legacy.go、mihomo_generate_py.go（死代码，`raise` 只剩 legacy.go:1239 一处）、pyhelpers.go（`pystr`、
  `pyTypeName/pyTruthy/hashID/asObject/iterList`，只被 errors.go 的 `attributeError` 与旧入口用）、errors.go 的
  `PyError/pyError/PyErrorClass`（← deploy/adapter.go:780、gateway/onboarding/adapter.go:285、ippresence）。
- configgen/{generate,adapter}.go：`Render/Build` ← api/config/live、api/generate；内部调 convert/mosdns 旧生成、
  `oxidns.BuildZIP`、`subs.OutdatedAgents`、`trafficstore.Get/StatusOf`。
- mosdns/legacy.go（`ConfigFromJSON/GenerateMosDNSConfig/GetMosDNSRulesetDownloads/GetMosDNSCustomFiles` ←
  configgen、deploy、proddiff）；oxidns/legacy.go（`NewProfile` ← proddiff；`SetClassicalIPLines` ← ippresence；
  `BuildZIP` ← configgen）。
- ippresence（17 处，`PyError`/`PythonClass`/`pystr`）、yamlbridge（`Encode/Value` 仍处理 `*jsonx.Object`，
  14 个生产文件用 yamlbridge——jsonx 分支随最后的调用方一起删）。

### 1.3 测试 / proddiff / 语料 / 工具（(c) 类）

| 范围 | 文件数 | 依赖性质 |
|---|---|---|
| 各包单元/回放测试 | ~530 | 两类：(1) 调用将被删除的 legacy 入口 → 随入口删除或改 typed；(2) 只用 `jsonx.Obj/Parse/Equal` 构造夹具、比较结构 → 机械替换 |
| difftest 包（Flask↔Go） | ~70 | 解析 Flask/Go 响应、结构比较、构造请求体（api/agents/difftest 24、api/rules 6、api/rulelibrary 5、httpx 4 …） |
| `jsonx.Repr` 断言 | 359 处 | Python `repr()` 文本比较（monitor 53、installer 45、substore 41、refresh 36 …）——属"不再追的 Python 内部文本" |
| `internal/yamlx/yamlxtest`（非 _test 文件，测试辅助包） | 7 | Python oracle 输出解析、值图比较 |
| tools/proddiff/{generator,baseline} | 3 生产 + 2 测试 | **生成器直接调用 legacy 生成入口**（convert/mosdns/oxidns/provider/substore/aggregations/trafficstore/subs） |
| tools/faults/fixture、tools/yamldiagnosticoracle | 2 | 构造夹具、解析 oracle 输出 |
| pyref 归档 / testdata 语料 | 数据文件 | 不动；只换读取它们的 Go 代码 |

**建议：允许一个独立的测试用 JSON 包 `internal/testjson`，但它不是改名后的 jsonx。**

理由：(1) ~600 个测试文件全部手写成 `map[string]any` + `reflect.DeepEqual` 会丢掉成员顺序（部分请求/存储夹具依赖
顺序）且把 1 与 1.0 的比较语义散落到每个测试里，工作量和出错面都很大；(2) 结构比较本身不是 Python 仿真，
encoding/json 的 token 流就能实现；(3) 门禁能把它限定在 `_test.go` 和测试辅助包里，产品代码不可能再回到动态文档。

`testjson` 只允许：有序对象构造器（实现 `json.Marshaler`，按插入顺序编码，用于请求体/存储夹具）、`Parse`
（`encoding/json` + `UseNumber`）、结构相等 `Equal`（对象无序、数组有序、数字按数值比较并可选区分整数/小数字面量）、
`Path` 取值、`Diff` 输出。**禁止**：`Repr/Str`、Python 风格编码（`ensure_ascii`、`separators`、浮点 repr）、
`Truthy`、`GetOr/SetDefault/Pop` 一类 dict 语义、`printable_table`。`jsonx.Repr` 断言改为对解析后的值做结构比较，
或（比较的是 Python 内部异常/repr 文本时）只比较类别/状态码——这已是 PORTING「No longer chased」允许的。
proddiff 生成器必须改走 typed 生成入口（`configgen` / `convert.Generate*` native），与产品同一条路径；baseline
不得出现新差异。

### 1.4 已经不导入 jsonx 但仍属过渡层的文件

`agents/events_legacy.go`、`agents/registration_legacy.go`、`installer/options_legacy.go`、`refresh/env_legacy.go`、
`routers/rule_sync_adapter.go`、`oxidns/adapter.go`、`mosdns/yaml_adapter.go`、`api/agents/{content_routes,
public_agent,client_tools}_adapter.go`。前四个是过渡层，需删；后六个按 n8-9/n8-10 报告是 typed 边界适配
（名字带 adapter，但没有旧形态），**保留**，门禁不以文件名 `adapter` 判定。

---

## 2. 工作包

### 2.0 共同规则（写进每个派工简报）

- **包所有权**：下表每个生产文件只属于一个工作包。工作包只改自己拥有的文件；需要别的包里**新增** typed 入口
  时，先问协调者（通常由拥有该包的工作包加，或放到 P0）。调用别包已有的 typed API 不需许可。
- **删除规则**：工作包把自己拥有的 legacy 入口在**它 rebase 时已无生产调用方**的部分删掉（连同只测这些入口的测试，
  或把测试改到 typed）。仍被其他未完成工作包使用的入口留下，由 W9 统一删除。
- **棘轮门禁**（P0 引入）：门禁里有一个"现存违例清单"；任何工作包不得新增违例，完成一项就从清单删一项；
  W9 结束时清单为空。这让并行包互不回退。
- **验收**沿用 Leaner verification：迁移的每个调用方跑它的 Flask 差分组/回放组（正常路径 + 每个错误类一例）；
  不重录冻结语料、不改 pyref 期望；新的 native 差异逐条登记 `parity/behaviour-changes.md`。每包至少一个负控
  （见各包）。作者只跑定向测试；完整门禁由协调者按 batch 跑。
- HR 包：Codex 实施，fresh Opus 复核；非 HR：Codex 实施 + 独立复核。四条常设检查项（测试替身不覆盖生产参数、
  原始请求字节对比、共享驱动路径在合并树上测、Detach 后提交点查 `ctx.Err()`）照旧。
- 迁移 legacy 调用方**不是重写业务**：业务只有 typed 一份；迁移就是把 `*jsonx.Object` 进/出换成 model/DTO，
  不得在调用方新造 codec。

### 2.1 包一览与依赖

| 包 | 名称 | 规模 | HR | 依赖 | 波次 |
|---|---|---|---|---|---|
| P0 | testjson + 棘轮门禁 + ClassError 过渡接口 | S | 否 | – | 0（单独先合） |
| W1 | repo typed 补全与内部去 jsonx | M | **HR**（存储核心） | P0 | 1 |
| W2 | agents 簇对外 typed API + 内部 adapter | L | **HR** | P0 | 1 |
| W3 | 资源 API 簇（subscriptions/aggregations/nodes + subs/substore/provider/refresh/health） | M+ | 否（provider 投递/令牌路径 Opus 抽查） | P0；nodes 删除部分需 W1 | 1 |
| W4 | 规则簇（rules/rulefetch/api rules/rulelibrary/proxygroups） | M | 否 | P0；rulelibrary 新建条目需 W1 | 1 |
| W5 | 配置/档案/设置 API 簇 + httpx 请求侧 | M | 否（导入导出/令牌路径 Opus 抽查） | W1；clientdevices/routers/profiles 中 Agent 调用需 W2 | 2 |
| W6 | 部署/网关/setup 簇 | L | **HR** | W1、W2 | 2 |
| W7 | 生成/导出簇 + proddiff/工具 | M | 否（Mihomo 生成 Opus 抽查） | W1；W3/W4 的 typed 入口已合 | 2 |
| W8 | Agent 侧测试迁移（api/agents、installer、metrics、upgrades 测试） | M | 否 | W2 | 2 |
| W9 | 删除 jsonx/pystr/legacy + 门禁清零 | M | 协调者门禁 + Opus O7 | 全部 | 3 |

并行：**波 1 = W1 ∥ W2 ∥ W3 ∥ W4**（4 个）；**波 2 = W5 ∥ W6 ∥ W7 ∥ W8**（4 个，W6 最长）；波 3 = W9 单独。
W3 的 api/nodes 删除子步、W4 的 rulelibrary 新建子步放在各自包的最后，W1 合入后再做（同一分支内顺序执行，不改别人的文件）。
若只有 3 个 Codex，波 1 先 W1 ∥ W2 ∥ W3，W4 接在 W3 之后；波 2 把 W8 推到 W5 完成后。

### 2.2 P0 — testjson、棘轮门禁、ClassError 过渡（S，非 HR）

- 范围：新建 `internal/testjson`（1.3 所列 API，带自测）；新建门禁测试 `internal/lifecycle/legacy_gate_test.go`
  （或 `internal/archgate`，与 `TestConcurrencyFacadeBoundary`、`TestYAMLFacadeBoundary` 同风格的 AST 扫描），检查项见 W9，
  当前违例写进同目录 `legacy_gate_allow.txt`（文件:符号 粒度）；`weburl.ClassError` 增加 `ErrorClass() string`，
  `SafeErrorDetails` 先认 `ErrorClass` 再认 `PythonClass`（日志标签字符串不变）。
- 不改任何其他包。
- 验收：testjson 自测（含整数/小数字面量、重复键末值、深度）；门禁对一个合成违例源文件报错（负控），
  对当前树通过；`weburl` 定向测试。

### 2.3 W1 — repo typed 补全与内部去 jsonx（M，HR）

- 拥有：`internal/repo/*`（含 legacy.go，但本包**不删** legacy.go 的对外旧 API）；需要时 `internal/model`（协调者许可）。
- 新增/改 typed：`ExportAll` → 返回 `json.RawMessage`（或 typed 导出结构 + 编码）；`ImportAll(ctx, raw json.RawMessage)`、
  `ResetAll`；defaults 改为产出 `model` 记录（`DefaultConfig/InitialConfig/DefaultOxidnsSettings` typed 版）；
  `PreviewNodeDeletion/DeleteNodesWithReferences(ctx, ids []string, ...)`（请求层校验 id 形状）；`CreateRuleLibraryItem(ctx,
  model.RuleLibraryItem, content)`；`SanitizationTokens(model.System)`；`AgentHistory` typed；`ReadSharedJSON` 改为
  `ReadSharedBytes`+调用方自解码（已存在 ReadSharedBytes）；profile 的 Clone/Export/Import/ImportClient/UpdateMetadata typed；
  `GetCompatConfig`/`GetPublishSnapshot` 的 typed 等价（多数已有 `Config/PublishConfig`，核对后补齐）；
  `Updater` 不再新增使用。内部 `importexport.go/profiles.go/system.go/shared.go/store_rows.go/validate.go/merge.go/
  defaults.go/files.go` 改为 typed 实现，旧 API 只在 legacy.go 转码。`files.go` 去掉 `pystr.UTF8DecodeError`
  （`utf8.Valid` + 保留中文/英文消息常量；消息文本属"Python 内部异常文本"，状态码不变即可）。
- 迁移的接口给 W3/W4/W5/W6/W7 用；W1 合入后才启动波 2。
- 验收：repo 全部定向测试 + `TestModelR5StoredValues*`、Python↔Go 存储互通（`raw_crosscheck.py audit-repository`）、
  导入/导出/重置的 Flask 差分组（api/config/difftest）、webdav difftest；负控：导入一个含未知成员和大整数的文档，
  导出后逐字节保留（证明没有经过 jsonx 重编码）。

### 2.4 W2 — agents 簇对外 typed API + 内部 adapter（L，HR）

- 拥有：`internal/agents/*.go`（核心）、`agents/monitor/*_legacy.go`、`agents/{rulesync,dnsha,dnsfollow,ipv6}/adapter.go`、
  `agents/trafficstore/legacy.go`、`notifications/legacy.go`、`tasks/legacy.go`、`taskstatus/legacy.go`、
  `weblistener/legacy.go`、`discovery/legacy.go`、`api/agents/client_tools_adapter.go`。
- 步骤：(1) 为 1.2 列出的外部调用补 typed 方法（`Agent(ctx,id) (*model.Agent, bool, error)`、typed agents 快照——可用现有
  `repo.ViewAgents`、typed deployment begin/record/request/get/activate、typed `ProbeDomains/RegionCheck/ReturnRouting*`、
  typed `Call` 请求/响应）；(2) 迁 Agent 内部子包（rulesync/dnsha/dnsfollow/ipv6 adapter、monitor 外部入口、
  notifications/tasks/taskstatus 的 Agent 侧调用）和 discovery/legacy.go、client_tools_adapter（GetNative）；
  (3) 删 (a) 类方法和旧 hook（同步 `agents/doc.go`、`RegisterHooks` 语义），删 monitor 中只剩测试的 legacy 文件
  （测试 codec 改到 testjson 或 `_test.go`），删 `events_legacy.go/registration_legacy.go`（若注册旧入口已无生产调用）；
  (4) trafficstore 的 `Load/LoadStatus` 迁完 discovery 后删，`StatusOf/Get` 留给 W6/W7 的调用方迁移后由 W9 删。
- deploy/gateway/routers/api 对 `Manager` 旧方法的调用**不在本包改**（W5/W6 改）。
- 验收：api/agents/difftest 全组（`TestDifferentialAgainstFlask`、Upgrades/Panel/DockerHA/Installer/Download/Oxidns/
  RankingCollector）、monitor/rulesync/dnsha/dnsfollow/discovery difftest、`TestBatch10HeartbeatAllocationCeilings`
  阈值不变；真实 Runtime shutdown/断线回归；负控：只注册旧 hook 的测试现在编译失败或明确报错（不再静默不调用）。

### 2.5 W3 — 资源 API 簇（M+，非 HR）

- 拥有：api/subscriptions/adapter.go、api/subscriptions/models.go、api/aggregations/adapter.go、aggregations/adapter.go、
  api/nodes/{adapter,latency,crud}.go、subs/legacy.go、subs/legacy_references.go、substore/legacy.go、provider/legacy.go、
  refresh/{owners,scheduled,interval,rows,env}_legacy.go、health/legacy.go、health/check/legacy.go、auth/legacy.go。
- 做法：处理器改用 DTO + `c.Bind`（顺带消掉它们的 `c.JSON()/JSONSilent()` 调用，非有限数请求按 N8-6 规则改 400 并在差分里断言），
  调 subs/substore/provider/refresh/health 的 typed 入口（N8-8d/N8-9 已建）；refresh 的错误改为 `ErrorClass()` 或 typed 错误判别；
  `auth.ValidateTokenOrJWT` typed（读 `model.System`）。删除本包拥有的、迁移后无生产调用方的入口。
  `health/check/legacy.go` 中只被 discovery 调用的 12 个入口等 W2 迁完 discovery 后删（W2 先合则本包删，否则 W9 删）。
- 最后子步（W1 合入后）：api/nodes 的节点删除预览/删除改用 W1 typed 入口。
- 验收：subscriptions/aggregations/nodes/subs/substore/refresh/provider/health difftest 组；provider 投递字节比较组；
  负控：订阅 ETag/令牌链接的一例旧期望在不改冻结数据下仍通过；Opus 抽查 provider 投递与 token 校验。

### 2.6 W4 — 规则簇（M，非 HR）

- 拥有：api/rules/{adapter,read_adapter,compose_adapter}.go、api/rulelibrary/adapter.go、api/proxygroups/adapter.go、
  rules/*_legacy.go、rulefetch/*_legacy.go、rulefetch/{transport,pyhtml,pyerrors,fetch}.go、rulesetfmt（若有残留）。
- 做法：处理器 DTO + `c.Bind`；改调 rules/rulefetch typed（`CacheOptions`、`ReadCacheWithFormat`、`PeekCacheWithFormat` 等）。
  `rulefetch/transport.go` 的字符集解析改 `mime.ParseMediaType`+`strings`；`pyhtml.go`（HTMLParser/unescape 仿真，N11）改
  `golang.org/x/net/html`（已在 go.mod）+ `html.UnescapeString`；`RuleFetchError` 改 `ErrorClass()`，`pyerrors.go` 改普通 typed 错误；
  `archive.go:safeDetails`、`ruleproxy.go:isBadRequest`（属 mosdns，见 W7）的 PythonClass 判别改 `errors.As`。
  proxygroups 调 subs/health/check 的 typed 入口（不动 W3 的文件）。
- 最后子步（W1 合入后）：rulelibrary 新建条目改用 W1 typed `CreateRuleLibraryItem`。
- 验收：api/rules、api/rulelibrary、rules、rulefetch difftest 组；网页规则源（HTML meta 刷新/charset）回放组；
  负控：一个 Latin-1/charset 头和一个 HTML 实体样例在新解析下结果不变。

### 2.7 W5 — 配置/档案/设置 API 簇 + httpx 请求侧（M，非 HR）

- 拥有：api/config/{custom,global,live}.go、api/profiles/profiles.go、api/settings/*、api/clientdevices/handlers.go、
  api/routers/{adapters,handlers}.go、api/tasks/legacy.go、api/auth/auth.go、webdav/adapter.go、configexport/export.go、
  clients/fixed_repo.go、routers/agent_return.go 中的 Agent 调用。
- 做法：改用 W1 的 typed 导入/导出/重置、profile、system API；Agent 调用改 W2 typed；convert 设置校验改 convert typed
  入口（`EasyTier`/BaseSettings/IPv6 flags 的 typed 版本若缺，向协调者申请由 W7 补，W5 等它）；`httpx.PyTypeName/PyParseInt`
  在 auth、settings 中的使用换成 DTO + `httpx.Number`/`taskstatus.Count` 一类已定入口；剩余 `c.JSON()` 调用全部改 `Bind`。
- 验收：api/config、webdav、tasks、settings、profiles、routers、clientdevices difftest 组；登录/JWT 组；负控：
  导出带 desensitize 的文档里令牌仍为 `[REDACTED]`（证明脱敏没被换路绕过）。

### 2.8 W6 — 部署/网关/setup 簇（L，HR）

- 拥有：deploy/{adapter,content_adapter,orchestrate_adapter,publish,orchestrate}.go、gateway/{adapter,config,wire}.go、
  gateway/draft/adapter.go、gateway/host/{host,forwarding}.go、gateway/installation/{adapter,publish,service,network}.go、
  gateway/onboarding/{adapter,service}.go、gwbase、setup/*、setupflow/wire.go、server/setup.go、routersplit/legacy.go、
  routers/legacy_wire.go、installer/options_legacy.go（迁完 gateway/installation 的调用后删除）。
- 做法：deploy `Wire()` 改为注册 typed collaborator（convert/mosdns/oxidns/provider/refresh/routersplit 的 typed 入口，n8-8b 表的
  "Removal owner = N8-10/gateway" 行在此收口）；`NewSource/NewTarget`（jsonx）调用方改 `SourceFromJSON/NewTypedTarget`；
  Agent 部署调用改 W2 typed；routersplit 19 个旧入口的调用方改 typed（Profile/Marker/Context/Session）；
  `convert.PyErrorClass`（deploy:780、onboarding:285）改 typed 错误判别；`gwbase.PythonClass` 改 `ErrorClass()`。
- 拆分建议（同一 Codex 顺序提交，便于复核）：(a) deploy；(b) gateway 四子包；(c) setup/setupflow/routersplit。
- 验收：deploy、gateway/installation、gatewayonboarding、linuxgateway、routersplit/routers difftest 组，setup 流程回放；
  O3：Detach 后每个提交点（repo 事务、文件替换、设备写入）查 ctx，真实 Runtime shutdown 回归；负控：缺一个必需 collaborator
  时 `Wire` 后的首次生成返回 `HookMissingError`，而不是静默跳过。

### 2.9 W7 — 生成/导出簇 + proddiff/工具（M，非 HR）

- 拥有：convert/legacy.go、convert/mihomoenv/legacy.go、convert/mihomo_generate_py.go、convert/pyhelpers.go、convert/errors.go
  的 PyError 部分、configgen/*、api/generate、mosdns/legacy.go（及 archive.go/ruleproxy.go 的 PythonClass 判别）、
  oxidns/legacy.go、ippresence/*、yamlbridge、tools/proddiff/{generator,baseline}、tools/faults/fixture、
  tools/yamldiagnosticoracle、internal/yamlx/yamlxtest。
- 做法：configgen 改调 typed 生成（Mihomo/Surge/Loon/MosDNS/OxiDNS `BuildZIP` typed）；`subs.OutdatedAgents`、
  `trafficstore.StatusOf` 改 typed（W2/W3 提供）；ippresence 去掉 `PyError/PythonClass/pystr`（`strings.TrimSpace`、
  `unicode.IsSpace`），旧 any 入口删除；`mihomo_generate_py.go` 整个删除（legacy.go:1239 的 `raise` 改返回 error）；
  `pyhelpers.go` 中仍被 typed 代码用的（`parseIPv4/parseIPv6/validIPv6`，被 ipaddr.go 用）挪到 `netipx`/typed 文件，其余删；
  proddiff 生成器改走 configgen/typed 入口；yamlxtest/工具改用 testjson 或 encoding/json。
  convert/legacy.go 中被 W3–W6 调用的入口：等对应包迁完再删；本包合入时删已无调用方的部分。
- 验收：Mihomo/Loon/Surge 精确计数字节门（2353/1064/1131，n8-8b/8c）不变；mosdns/oxidns difftest；**proddiff 无新差异**
  （生成器换路后 baseline 自检必须为 0 new / 0 stale）；ippresence 回放组；负控：proddiff 生成器对一个改过的合成 profile
  确实报差异（证明没有比较空集）。

### 2.10 W8 — Agent 侧测试迁移（M，非 HR）

- 拥有：api/agents（36 测试文件）、api/agents/difftest（24）、installer（28）、agents/metrics（15）、agents/upgrades（11）
  的测试文件——这些包生产代码已无 jsonx。
- 做法：调用已删 legacy 入口的测试改 typed 或删除（typed 已有边界测试时删除，在报告中列出对应关系）；夹具/比较换 testjson；
  `jsonx.Repr` 断言改结构比较或类别比较。不改生产代码、不改冻结语料和 pyref 期望。
- 验收：这些包 `go test -race` 全绿；difftest 组结果与迁移前一致（迁移前后各一次，用同一台服务器，记录用例数一致）；
  负控：随机挑 3 个改写的断言，故意改期望值确认测试失败。

### 2.11 W9 — 删除与门禁清零（M，协调者门禁 + Opus O7）

- 删除：`internal/jsonx`（含 testdata）、`internal/pystr`；`repo/legacy.go` 与 scope.go/shared.go/system.go/profiles.go 里所有
  jsonx 签名的旧 API、`Updater` 类型；`httpx` 的 `JSON()/JSONSilent()/JSONForce()/JSONObject()`、`jsonvalue.go` 的 jsonx 分支、
  `pyconv.go`、`errors.go`/`response.go` 中对 jsonx 的引用、httpxtest 的 jsonx；各包剩余 `*_legacy.go`（n8-8b、n8-8c、
  n8-10 各表"Removal owner"列为 N8-11 的行逐行核对）；trafficstore facade；`weburl.ClassError` 的 `PythonClass` 回落；
  剩余测试文件（store 5、server 5、cmd/configflow 2、redact 2、importer 2、各小 difftest 包约 30）迁 testjson。
- 门禁（AST，P0 已建，清单清空后改为硬性）：
  1. 任何 `.go` 不得 import `configflow/internal/{jsonx,pystr,pyyaml}`；这三个目录不得存在。
  2. `configflow/internal/testjson` 只能被 `_test.go` 和 `*test` 辅助包（`yamlxtest`、`httpxtest`、`testgolden`、difftest 包）导入。
  3. 非测试代码不得声明或调用：`PythonClass`、`PyError`/`pyError`/`PyErrorClass`、`raise`、`catch`、`must`、`mustValue`、
     `dictGet`、`dictItem`、`PyTypeName`、`PyParseInt`、`pyTruthy`；不得有名为 `legacy.go` 或 `*_legacy.go` 的非测试文件。
  4. 函数引用也算（照 `TestYAMLFacadeBoundary`：赋值给变量不能绕过）。
  5. 负控：门禁对 testdata 里的一个违例样例源报出每一类违例。
- 验收：完整 batch 门禁（gofmt/vet 两套 tag、`-race` 全量、proddiff、全部 difftest）；报告非测试行数前后对比。

---

## 3. 风险与需要裁定的问题

### 3.1 风险

| 风险 | 触发 | 缓解 |
|---|---|---|
| 并行包改同一文件 | 迁移中发现缺 typed 入口，顺手在别人包里加 | 2.0 所有权规则；缺入口先报协调者；棘轮门禁只允许减少 |
| legacy 迁移顺手改了业务 | 调用方把 jsonx 逻辑"翻译"成新 codec | 复核检查"调用方无新 codec"；n8-8b 规则：业务只有 typed 一份 |
| 存储字节变化 | W1 导入/导出改 typed 后重编码未改字段 | W1 负控（未知成员/大整数逐字节保留）+ 存储互通审计 |
| proddiff 假绿 | 生成器换路后比较空集或用错入口 | W7 负控 + baseline 自检 0 new / 0 stale |
| 测试大批删除导致覆盖下降 | 把"测 legacy 入口"的测试整片删掉 | 删除需在报告中列出对应 typed/边界测试；无对应则改写而非删除 |
| 请求非有限数行为变化 | 剩余 `c.JSON()` 调用改 `Bind` | N8-6 已定：改 400 并在差分中显式断言、登记 |
| Agent 旧 hook 静默失效 | 删除旧 hook 后测试仍注册旧名 | W2 负控（编译失败或显式错误） |
| 波 2 依赖 W1/W2 两个 HR 包，复核排队拖长关键路径 | – | W1、W2 优先进复核；W3/W4 不依赖它们的部分先合 |

### 3.2 需要维护者/协调者裁定

1. **测试用 JSON 包**：是否同意新增 test-only 的 `internal/testjson`（只含有序构造、Parse、结构 Equal/Diff，不含 Repr/Python
   编码/Truthy），由门禁限制为只能被测试代码导入？**建议同意**。不同意的话，W8/W9 的测试迁移工作量大约翻倍，
   而且每个测试都要自己处理顺序和数字比较。
2. **门禁范围**：N8-11 门禁是否也覆盖 jsonx 之外的 Python 仿真残留——`agents/monitor/{pyip,pydigit_table,
   pydecimal_table}.go`、`input_text.go` 的 `pyDecimalValue`（Agent 输入的非 ASCII 数字处理）、`convert` 中
   "Non-ASCII bandwidth digits"（n8-8c）？**建议**：本轮门禁覆盖 2.11 所列项；`rulefetch/pyhtml.go`、`pyerrors.go`、
   `httpx/pyconv.go` 本轮删（已分到 W4/W9）；monitor 的 Unicode 数字表另立 N10 尾项，需要维护者先决定 Agent 上报里的
   非 ASCII 数字是否继续接受。
3. **`PythonClass` 改名后的日志标签**：`SafeErrorDetails` 输出的 `exception_type=RuleFetchError` 等标签按原字符串保留
   （只改 Go 方法名为 `ErrorClass()`），还是改成 Go 风格名？**建议保留字符串**（日志检索不变，不属于仿真）。
   只影响日志，若无异议按建议执行。
4. **批次与复核负担**：波 1 有两个 HR 包（W1、W2），波 2 有一个（W6），都要 fresh Opus 复核。是否接受波 2 在 W1、W2
   **复核通过**后才开工（更稳），还是在它们**实现交回**后就开工、复核意见再由波 2 吸收（更快但可能返工）？
   **建议前者**。

### 3.3 裁定（协调者，2026-11-20）

1. **同意** test-only `internal/testjson`（有序构造、Parse、结构 Equal/Diff；无 Repr/Python 编码/Truthy），门禁限制只能被
   `_test.go` 与测试工具导入。
2. **本轮一起删**：monitor 的 `pyip`/`pydigit_table`/`pydecimal_table`、`pyDecimalValue` 和 convert 的非 ASCII 带宽数字处理
   归入 W2（monitor）/W7（convert），不另立 N10。依据：batch-11 已有裁定"机器文本/数字用原生 ASCII 规则（无 CPython 表）"，
   且硬规则禁止 Python 仿真；Agent 是我们自己的 Go 二进制，只发 ASCII 数字。非 ASCII 数字按对应字段的现有错误类处理
   （400/跳过，与同字段其他非法值一致），登记 behaviour-changes。
3. **保留**日志标签字符串（`exception_type=RuleFetchError` 等），只改 Go 方法名为 `ErrorClass()`。
4. **波 2 等 W1、W2 复核通过（含修复合入 go）后开工**。W3/W4 不依赖 W1/W2 的部分可先合。
