# ConfigFlow Go 后端：启动组装 / 钩子 / 全局变量清单（O17 wiring 迁移设计输入）

基线：worktree `go-o17-wiring-plan` @ 15ae7cb3c，`server/`（模块 `configflow`）。只读分析，没有编译或运行。
数据来源：``design/o17-wiring/globals.txt``（类型检查得出的含函数包级变量）、``design/o17-wiring/hooks.txt``（补充了它漏掉的 `OnAgentDeleted`/`OnManagerInit`/`OnRulesRefreshed`/`SetIPv6Enroll`：原 grep 的正则不匹配数字和 `On*` 前缀）、非测试文件的 import 块（``design/o17-wiring/imports.json``，按文件统计，脚本在 ``design/o17-wiring/``）。

**环判定方法**：「T 能否直接 import F」= 在去掉 F 里只做注册的文件（wire.go 等）以后，F 的 import 闭包里还有没有 T。文中「无环」都经过 `reach2.py` 核实；「有环」都写出了具体的链路。

类别：**A** 跨包钩子；**B** 进程单例；**C** 只给测试替换的接缝；**D** 常量性质的函数值或缓存（不动 / 改成普通函数）；**E** 进程级可变缓存或状态（应归属某个服务结构体）；**W** wirePackages 条目；**I** init()；**X** 误报（不是钩子）。

---

## 0. 结论速览

1. **生产里仓库和 Manager 在整个进程生命周期里唯一**。`repo.SetRepository` 只在 `cmd/configflow/service.go:131` 调用一次（另外两处在 `tools/proddiff`，不属于生产）。导入、恢复、WebDAV 都是在同一个 `*Repository` 上提交事务，不会换仓库。所有生产里的 `repo.WithRepository(ctx, X)` 传进去的 X 不是 `m.Repository()` 就是这个默认仓库。`agents.Init` 只在启动步骤 `agent-manager-init` 里跑一次；「仓库变了就重建 Manager」的分支只有测试会走到（测试会给不同的 ctx 绑不同的仓库）。
2. **不少钩子其实没有循环导入，可以直接 import 或直接调用**（详见第 1、3 节）：
   - `repo.Hooks.RedactEasytierJSON`：`repo` **已经 import 了 `convert`**（input_errors.go、import_conversion.go、resolve.go、validation.go），所以 server 里那句「repo cannot import convert」的注释已经过时，可以直接调用 `convert.RedactEasyTierJSON`。
   - `notifications.Inputs.Rows/MissingTasks`：`notifications` **已经 import 了 `tasks`**，可以直接调用 `tasks.Available/Missing`。
   - `agents.Hooks` 里有 5 个字段 **`agents` 包自己从来不读**：`PinnedWithheld`（只有 api/rules 读）、`AddressFollowTyped`/`AddressConfirmedTyped`/`InstallationProfileTyped`/`InstallationRegisterTyped`（只有 api/agents 读）。读取方已经 import 了提供方，或者可以无环地 import（api/rules→agents/rulesync、api/agents→agents/upgrades、api/agents→gateway/installation），所以这几个字段直接删掉、改成直接调用即可。
   - `agents.Hooks.PublicationCheckpoint` 在 deploy 里也有读取，`deploy` 可以无环地 import `agents/monitor`。
   - `deploy.SetGatewayGuards`：`deploy` 可以无环地 import `gateway/host`（注释里说的是反方向 host 不能 import deploy）。
   - `setup` 和 `gateway/onboarding`、`gateway/installation`、`gateway/host`、`api/rules` **互相都没有 import 关系**，setup.go 里的 3 个 wire 函数都不是为了避环。
   - `routersplit.SetSessionReader`：setupflow 只在 wire.go 里 import routersplit，所以 routersplit 可以直接调用 setupflow。
   - `oxidns.SetNativeClassicalIPLines`：ippresence 只在 wire.go 里 import oxidns，所以 oxidns 可以直接 import ippresence。
   - `agents.OnAgentDeleted` 里 health 的两项：health 只在 wire.go 里 import agents。
   - 本包自己装自己的钩子：`metrics.trafficFlush`、`monitor.historyFactory`、`api/agents.contentPrepare`、`discovery.collaboration`、`routers.CaptureManual`/`ReportParts`。这些完全不需要钩子，改成直接调用。
   - `rulesync` 可以无环地 import `refresh`，所以 `wireLoopWakes` 不需要放在 server 里。
3. **死钩子和重复注册**：
   - `repo.Hooks.InitAgentManager` 在生产里永远不会触发（只有 `repo.LoadConfig` 读它，而 LoadConfig 在生产里没有调用方，只有 7 个测试文件用）。
   - `convert.MihomoIntegrations.FullTun` 被 `ippresence.Wire` 和 `gateway.Wire` 各装一次，代码一模一样，后装的覆盖前装的。
   - `metrics.RegisterStartup` 和 `substore.RegisterStartup` 在 cmd 的 `registerStartup()` 和各自的 `Wire()` 里各注册一次，靠 lifecycle 按名字去重才没出问题。
   - `setup.ExternalOwners.VerifyRouter` 在生产里**从来没有注册**，setup 走到这一步时一律返回 unavailable（completion.go:286）。
4. **真正有环、需要保留注入的**集中在这几处：`agents` ← 子包（metrics/monitor/dnsha/deploy/installation，都依赖 `*agents.Manager`）；`repo` ← 上层（redact/configexport、routers、webdav、api/nodes）；`convert` ← mihomoenv/gateway/ippresence/subs；`provider` ← deploy/aggregations；`tasks`/`notifications` ← 各行和运行器提供方；`deploy`/`rulesync`/`monitor` ← dnsha、agents/ipv6；`routers` ← agents/ipv6。
5. **定位器统计**（非测试代码）：`repo.GetRepository(` 158 处 / 46 包，repo 包内部另有 9 处；`agents.Get(`（含别名 `core.Get`）56 处 / 13 包，其中 api/agents 36 处，agents 包内部另有 2 处；`substore.Default(` 10 处 / 7 包；`webdav.Default(` 1 处。会隐式取默认仓库的包级函数：`repo.ResolveProfileID` 27 处 / 16 包，`repo.ResolveProfile` 3 处，`repo.GetDefaultProfileID` 3 处，`repo.Config` 1 处，另外还有 `trafficstore.GetNative` 6 处、`health.GetProbeStore/GetRegionStore` 6 处、`notifications.TaskStatus` 8 处、`rulefetch.GetRulesDir` 1 处、`rulesync.Get` 1 处。

---

## 1. wirePackages 逐项（server.go:124 起 32 项，另加 cmd 里的 3 项）

| 编号 | 位置 | 类别 | 注册了什么（谁填 → 目标包） | 为何当初用钩子（import 核实） | 改成什么 | 构造步骤（见第 9 节） | 测试影响 |
|---|---|---|---|---|---|---|---|
| W01 | `deploy.Wire` deploy/adapter.go:159 | W/A | `provider.SetLocations(deploy.Locations{})`；`agents.Hooks.ReceiptMatches = deploy.ReceiptMatches` | 有环：deploy 已经 import provider（adapter/orchestrate/prepare）和 agents（12 个文件），所以 provider、agents 都不能 import deploy | provider：`provider.New(Options{Locations: deploy.Locations{}})` 构造参数。agents：`agents.Options{Receipts: ReceiptMatcher}` 小接口，由 server 传入 `deploy.ReceiptMatches` | S6 provider；S8 Manager | 8 个测试文件调用 `provider.SetLocations`，改成给 provider 构造参数；ReceiptMatches 在测试里用 agents.Options 注入 |
| W02 | `configexport.Wire` configexport/export.go:107 | W/A | `repo.RecordHooks.SanitizeExternalDocument`、`RedactOxidnsJSON` | 有环：configexport→redact→repo，redact 本身 import repo，所以 repo 不能 import configexport | `repo.Options{Exporter: configexport.Exporter{}}`，定义小接口 `repo.ExportSanitizer{Sanitize(ctx,*Document,SystemConfig); RedactOxiDNS(raw)}` | S1 打开仓库时传入（纯函数，没有状态） | 25 个文件用 `repo.SetHooks`，改成在 `repo.Open(dir, Options{...})` 里给 fake |
| W03 | `rulesync.Wire` agents/rulesync/wire.go:45 | W/A | `agents.SetRuleSyncRow`；`agents.Hooks.PinnedWithheld`；`lifecycle.RegisterPeriodic("agent-rule-sync")` | Row：agents 不能 import rulesync（rulesync→agents 有 45+ 处）。PinnedWithheld：**agents 自己不读**，只有 api/rules 读（5 处），api/rules→rulesync 无环 | Row：把 agent 任务行的拼装从 agents 包搬出去（见 H08）。PinnedWithheld：api/rules 的处理器结构体持有 `*rulesync.Service` 直接调用。周期任务：`rulesync.New(m, rt, Options{Pair: ha})` 在构造时向 `rt` 注册 | S10 | api/rules 测试改成构造 handler 时给 fake 接口 |
| W04 | `apiagents.WireContentSync` api/agents/content_wire.go:96 | W/A | 本包的 `SetContentPrepare(deploy.PrepareContentBundle 包装)`；`substore.SetContentSyncInvalidator`；`lifecycle.RegisterShutdown("agent-content-sync-close")` | contentPrepare：**自己装自己**，api/agents 已经 import deploy（13 处），没有环。substore 的失效回调有环：api/agents→gateway/installation→provider→substore | contentPrepare 改成直接调用（checker 的 Prepare 就用 `deploy.PrepareContentBundle(ctx, m, …)`）。失效：`substore.Service` 提供 `OnContentChange(func)` 事件订阅，由 contentsync 的 checker 池在构造时订阅。checker 池按 Manager 建的 map 改成一个固定的 `*contentsync.Checker` 字段 | S11 api 层之前建 `contentsync.Checker`（依赖 m、deploy） | 2 个测试文件用 SetContentPrepare，改成给 Checker 传 `Options.Prepare` |
| W05 | `wireCore` server.go:189 | W/A | `notifications.SetInputs{Rows: tasks.Available, MissingTasks: tasks.Missing}`；`repo.Hooks.DefaultServerDomain`；`repo.RecordHooks.RedactEasytierJSON = convert.RedactEasyTierJSON` | ① **无环**：notifications 已经 import tasks。② DefaultServerDomain：repo 不能 import server（属于组合层的知识）。③ **无环**：repo 已经 import convert，注释过时 | ① notifications 直接持有 `*tasks.Registry`。② 改成 `repo.PrepareConfig(ctx, PrepareOptions{DefaultServerDomain: fn})` 的参数，或启动步骤里由 server 先算好再传值。③ repo 内部直接调用 `convert.RedactEasyTierJSON` | ①S3；②S12 启动步骤；③不需要构造 | Rows/MissingTasks 相关的 `notifications.SetInputs` 测试改为构造 `notifications.New(repo, tasks)` |
| W06 | `webdav.Wire` webdav/adapter.go:128 | W/A+B | `tasks.SetRunner(SlotBackup)`；`repo.RecordHooks.ConfigurationCommittedRecords`（提交后安排自动备份）；`lifecycle.RegisterStartup("webdav-worker-init")`，并在启动时动态注册 Shutdown；`automaticWorker` 包级单例；`init()` 里 `service.Store(NewService(nil))` | 有环：webdav→lifecycle→repo、webdav→notifications→tasks，所以 repo、tasks 都不能 import webdav | `webdav.New(repo, rt, tasks, notif)` 返回 `*Service`（自带 worker）。构造时 `repo.Subscribe(CommitObserver)`（事件订阅，取代单字段 hook），`tasks.Register(SlotBackup, runner)`，`rt.RegisterStartup` | S7 | 9 个测试文件用 `webdav.SetDefault`，改成 `webdav.New(...)` 后直接拿实例；api/settings 的 handler 持有 `*webdav.Service` |
| W07 | `apinodes.Wire` api/nodes/adapter.go:104 | W/A | `repo.RecordHooks.PlanNodeDeletionRecords`（planDeletion） | 有环：api/nodes→repo 用了 12 处，repo 不能 import api 包。不过 planDeletion 只依赖 repo.ConfigFrom、convert、yamlx、model，**repo 已经 import convert** | **把 planDeletion 下沉到 repo**（或者 `repo/nodedelete` 子包），repo 直接调用，删掉这个钩子 | 无 | 用 SetHooks 替换 PlanNodeDeletion 的测试改成直接测 repo 的函数 |
| W08 | `aggregations.Wire` aggregations/adapter.go:180 | W/A | `provider.SetAggregationRenderer(RenderProvider)` | 有环：aggregations→provider（adapter、native_delivery、render） | `provider.New(Options{Aggregations: aggregations.Renderer})` 构造参数，或者两阶段 `p.SetAggregationRenderer` 只在 server 里调用一次（见裁定 J3） | S6 | 3 个测试文件，改成传构造参数 |
| W09 | `agents.Wire` agents/singleton.go:156 | W/A | `repo.Hooks.InitAgentManager`；`notifications.SetInputs{Agents: NotificationAgents}` | ① 有环（agents→repo），但**生产里是死钩子**（LoadConfig 没有生产调用方）。② 有环：agents→notifications（deployment_record.go、singleton.go） | ① 删除。② `notifications.New(..., Agents: AgentLister)` 小接口 `ListAgents(ctx) ([]model.Agent, error)`，由 `*agents.Manager` 实现；构造顺序上 notifications 早于 Manager，所以要用两阶段 `notif.SetAgents(m)` 或延迟闭包（裁定 J3） | S8 后接上 | 12 个测试文件用 notifications.SetInputs，改成构造参数 |
| W10 | `dnsfollow.Register` agents/dnsfollow/wire.go:39 | W/A | `agents.OnManagerInit(Wire)`；`agents.SetDNSFollowRow`；`tasks.SetRunner("dns_follow")`；`lifecycle.RegisterPeriodic("dns-follow")`；包级 `services.byManager` 缓存 | 有环：dnsfollow→agents（16 处），agents 不能 import 它 | Manager 唯一，所以 OnManagerInit 改成 server 在 S9 直接 `follow := dnsfollow.New(m, rt, tasks)`。构造函数里注册 runner、周期任务和行，`byManager` map 删掉 | S9 | 不再需要 `agents.ResetHooks`；测试直接 `dnsfollow.New(fakeM, …)` |
| W11 | `dnsha.Register` agents/dnsha/hooks.go:39 + adapter.go:316 | W/A | `agents.OnManagerInit`；`rulesync.SetPairAdvance`；`rulesync.SetPairFailureMessage`；`deploy.SetPairPublication`；`agents.Hooks.{HADeployment, HAActivate, HAServiceOperationTyped}`；`monitor.SetHARulesJSON`；`RegisterPeriodic("oxidns-ha-publish")` | 都是真环：dnsha 已经 import agents、deploy（9 个文件）、rulesync（4 个文件里调用无状态函数）、monitor。另外 monitor 即使去掉 adapter.go，仍有 dnsha→rulesync→monitor 的链路 | `ha := dnsha.New(m, rt)`，把它作为小接口传给：`rulesync.New(..., Pair: ha)`（Advance、FailureMessage）、`deploy.New(..., PairPublisher: ha)`、`monitor.New(..., HARules: ha)`。Manager 的 HA 三个方法要么两阶段 `m.SetHA(ha)`，要么保留现状「钩子函数带 *Manager 参数」的签名，作为 Options 字段由 server 传入闭包（裁定 J1） | S9（在 rulesync、deploy 服务之前） | 1–2 个测试文件用 SetPairAdvance/SetPairPublication/SetHARulesJSON，改成构造参数 |
| W12 | `metrics.Wire` agents/metrics/wire.go:110 | W/A | `agents.Hooks.{AddMetricsJSON, DeleteMetricsHistoryContext}`；再调一次 `RegisterStartup()`（cmd 已经注册过）；`tasks.SetRowBuilder(SlotMetrics)`、`SetRunner`、`SetIntervalEditor`；`RegisterPeriodic(flush)`；2 个 Shutdown | 有环：metrics→agents（6 个文件） | `metrics.New(dataDir, rt, tasks)`，`*History` 不再按 dataDir 存进全局 map。Manager 收到 `agents.Options{Metrics: MetricsSink}`，小接口 `AddSample/DeleteAgent`（现有签名已经带 *Manager，所以 metrics 不需要先有 Manager） | S8 之前（S7b） | 3 个测试文件用 `metrics.Reset`，各测试自己 `New` 实例即可 |
| W13 | `metrics.WireTraffic` agents/metrics/traffic_wire.go:83 | W/A | `agents.Hooks.DeleteTrafficHistoryContext`；`agents.OnAgentDeleted("traffic_store")`；`SetNativeTrafficFlush(flushTraffic)`（**自己装自己**） | agents 部分有环，同 W12。trafficFlush 同包，没有环 | 与 W12 合并到 `metrics.New`。trafficFlush 直接调用。`agents.Options{DeletedStores: []NamedDeleter{traffic, probe, region}}` | S7b | 2 个测试文件用 SetNativeTrafficFlush，改成字段注入 |
| W14 | `monitor.Wire` agents/monitor/wire.go | W/A | `SetRankingHistory(rankingStoreFactory)`（**自己装自己**）；`agents.Hooks.{PublicationComplete, PublicationCheckpoint}`；`RegisterPeriodic(ranking)` | agents 部分有环：monitor→agents。PublicationCheckpoint 也被 **deploy** 读（publish.go:343），deploy→monitor 无环 | `mon := monitor.New(dataDir, rt, Options{HARules: ha})`。Manager 收到 `agents.Options{Publication: mon}`；deploy 直接持有 `mon`（或者直接调用 monitor 的包函数）。historyFactory 改成默认值，测试用字段注入 | S7b（Manager 之前），HARules 依赖 dnsha，所以要么 monitor 在 dnsha 之后构造，要么 HA 规则通过参数传入（裁定 J1） | 4 个测试文件用 ResetRankingsCache，1 个用 ResetRankingStores；缓存成为实例字段后，这些 Reset 就不需要了 |
| W15 | `upgrades.Wire` agents/upgrades/wire.go | W/A | `agents.Hooks.{AddressFollowTyped, AddressConfirmedTyped}` | **agents 不读这两个字段**，只有 api/agents/agents.go:478–484 读；api/agents 已经 import upgrades | 删掉钩子，api/agents 的 handler 直接调用 `upgrades.FollowModel/ConfirmedModel`（或者 handler 持有 `*upgrades.Service`） | S11 | 无 |
| W16 | `substore.Wire` substore/wire.go | W/B | 用 `Default()` 惰性单例注册 `RegisterPeriodic(idle)`；再调一次 `RegisterStartup()`（cmd 已经注册过） | 不是为了避环，是进程单例 + 注册 | `sub := substore.NewService(substore.NewEnv())`，构造时把 rt 传进去注册（`sub.Register(rt)`） | S5 | 15 个测试文件用 `substore.SetDefault`，改成把实例注入给消费方 |
| W17 | `refresh.Wire` refresh/rows.go:268 | W/A | `tasks.SetRowBuilder(SlotSubscriptions, SlotRuleSources)`；`SetRunner("subscription","rule_source")`；`RegisterPeriodic` | 有环：refresh→notifications→tasks、refresh→tasks | `refresh.New(repo, sub, rt, tasks)`，构造时注册行、运行器和周期任务；`refreshLoop` 包级 pacer 改成字段 | S5 | 13 个测试文件用 `SetRefreshEnv`（生产从来不设），改成构造参数 |
| W18 | `wireLoopWakes` server.go:118 | W/A | `refresh.OnRulesRefreshed(rulesync.WakeLoop)` | **无环**：rulesync 可以 import refresh（反方向 refresh→rulesync 才有环：rulesync→deploy→refresh） | `refresh.Service.OnRulesRefreshed(fn)` 改成实例方法（事件订阅），由 `rulesync.New(..., Refresh: rs)` 在构造时订阅；或者 server 在 S10 订阅（只有一行） | S10 | `refresh_wire_test.go` 改测实例 |
| W19 | `health.Wire` health/wire.go | W/A | `agents.OnAgentDeleted("domain_probe","region_check")` | **无环**：health 只在 wire.go 里 import agents，所以 agents 可以 import health。但 health→substore→convert 偏底层，让 agents 直接依赖它没有必要 | `agents.Options{DeletedStores: […]}`，由 server 传入 `health.ProbeStores(dataDir)` 的删除器（小接口 `Delete(ctx, agentID) error`） | S7b | 1 个测试文件用 OnAgentDeleted |
| W20 | `discovery.Wire` discovery/auto.go:260 | W/A | `tasks.SetRowBuilder/SetRunner(SlotDomains)`；`RegisterPeriodic(auto)`；`RegisterShutdown(matcherShutdown)` | 有环：discovery→agents→tasks、discovery→tasks | `discovery.New(repo, m, rt, tasks)`；`matcherCache`、`jobState` 改成实例字段 | S10 | 测试自己 `New` |
| W21 | `mihomoenv.Wire` convert/mihomoenv/wire.go | W/A | `convert.SetMihomoEnvironment(NativeEnv())` | 有环：mihomoenv/native.go→convert；而 NativeEnv 需要 provider、refresh、substore、repo，这些包都 import convert | `env := mihomoenv.NativeEnv(repo, sub, refresh, provider)` 返回值；convert 的生成入口改成接收 `convert.Env`（结构体方法或显式参数）。这是改动面最大的一项，见裁定 J2 | S6 | 23 个测试文件用 SetMihomoEnvironment |
| W22 | `ippresence.Wire` ippresence/wire.go | W/A | `convert.SetMihomoIntegrations{FullTun}`；`oxidns.SetNativeClassicalIPLines` | FullTun：有环（ippresence/ippresence.go→convert），**而且和 gateway.Hooks().FullTun 重复，被后者覆盖**。classical：**无环**（ippresence 只在 wire.go 里 import oxidns） | FullTun：删掉 ippresence 这份。classical：oxidns 直接 import ippresence，或者 oxidns 的生成器持有 `ClassicalIPLines` 字段 | S6 | oxidns 有 3 个测试文件用 SetNativeClassicalIPLines，改成字段注入 |
| W23 | `routers.Wire` routers/wire.go | W/A | `wireRuleSync`：`CaptureManual = …`（**自己装自己**）、`tasks.SetRowBuilder/SetRunner(SlotRouters)`、`RegisterPeriodic("router-rule-sync")`；`repo.RecordHooks.{RedactRoutersJSON, JobLockRecord}`；`notifications.SetInputs{Summarize}` | repo、notifications、tasks 都有环（routers→repo/notifications/tasks） | routers 内部归协调者。对外：`repo.Options{RouterRedactor, RouterJobLock}`（纯函数，S1 传入）；`notifications.New(..., Summarizer: routers.SummarizeJSON 包装)`；`routers.NewSyncService(repo, rt, tasks)` 注册 | S1、S3、S10 | — |
| W24 | `wireSetupContent` server/setup.go:23 | W/A | `setup.ExternalOwners.ReadRouteContent = apirules.ReadRulesetContent` | **无环**：setup 和 api/rules 互不 import。钩子只是为了避免核心包 import api 包（分层原因） | 把 `ReadRulesetContent` 和 `sourceContent` 抽到一个非 api 的 `rulesource` 服务，`setup.New(..., Rules: rs)` 小接口。routers.RulesetContent、gateway.ruleSourceReader 也由它提供（W28） | S6b | setup 有 4 个测试文件用 SetExternalOwners，改成构造参数 |
| W25 | `wireSetupOnboarding` server/setup.go:35 | W/A | `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)`；`setup.ExternalOwners.DNS`（5 个 onboarding 纯函数或常量） | **无环**：两个包互不 import。两个方向都可以直接 import，但不能同时 import | setup 直接 import onboarding，DNSOwner 删除改为直接调用。onboarding 的 AdoptOwnedWrite 保留为构造参数 `onboarding.New(..., Adopter: setupSvc)`；或者反过来（裁定 J4） | S10b | 3 个测试文件用 SetAdoptOwnedWrite |
| W26 | `installation.Wire` gateway/installation/wire.go | W/A | `agents.Hooks.{InstallationProfileTyped, InstallationRegisterTyped}` | **agents 不读**，只有 api/agents/agents.go:203–269 读；api/agents 已经 import installation | 删掉钩子，api/agents handler 持有 `*installation.Service{Repository, Manager}` 直接调用 | S11 | 无 |
| W27 | `wireSetupInstallation` server/setup.go:50 | W/A | `setup.ExternalOwners.Installation{Latest, HasInstallation, GatewayStatus}`（内部调用 `agents.Get`） | **无环**：setup 和 installation、host 互不 import（setup→routers→agents 也不碰它们） | `setup.New(repo, Options{Installation: inst, Gateway: hostBridge})` 小接口；`installationState/gatewayStatus` 两个解析函数挪进 setup 或 installation | S10b | 同 W24 |
| W28 | `apirules.Wire` api/rules/read_adapter.go:343 | W/A | `routers.RulesetContent = …`；`gateway.SetRuleSourceReader(...)` | 去掉 read_adapter.go 以后 routers、gateway 都可以 import api/rules（无环），但核心包 import api 包不合适 | 同 W24：`rulesource.New(repo, rulefetch.Client)` 服务，routers、gateway、setup、api/rules 共用 | S6b | routers 有 1+2 个测试文件、gateway 有 4 个测试文件 |
| W29 | `gateway.Wire` gateway/wire.go | W/A | `convert.SetMihomoIntegrations(gateway.Hooks())`（ApplyGateway、GatewayEnabled、FakeIPNetworkError、DNSLoop、FullTun） | 有环：gateway→convert（apply、extract），gateway→routersplit→convert | 作为 `convert.Env.Integrations` 的值由 server 组装（见 W21），`gateway.Hooks()` 已经是纯函数 | S6 | 9 个测试文件用 SetMihomoIntegrations |
| W30 | `setupflow.Wire` setupflow/wire.go | W/A | `routersplit.SetSessionReader` | **无环**：setupflow 只在 wire.go 里 import routersplit，routersplit 可以直接调用 `setupflow.ProfileSessions` | 直接调用（要把 Session 的 JSON 转换挪进 routersplit） | 无 | 3 个测试文件用 SetSessionReader，改成用真实的 setupflow 目录夹具 |
| W31 | `wireGatewayGuards` server.go:156 | W/A | `deploy.SetGatewayGuards(...)`，用 host.Bridge 包装 | **无环**：deploy 可以 import gateway/host（host 不 reach deploy）。注释说的「host cannot import deploy」是反方向，和这里无关 | deploy 直接构造 `host.Bridge{Repository, Manager}`，或者 `deploy.New(..., Guards: hostBridge)` 小接口（便于测试） | S10 | gateway_guards_test.go 改测 deploy 构造 |
| W32 | `wireIPv6` server.go:142 | W/A | `agentipv6.Wire(apiagents.ContentChecker)`：`deploy.SetIPv6Enroll`、`agents.OnManagerInit`（Init 时**才** `lifecycle.RegisterPeriodic("ipv6-verification-scheduler")`）；`routers.IPv6HostRequest = …`（内部调用 `agents.Get`） | 有环：agents/ipv6→deploy、agents、routers（service.go 24 处） | `ip := agentipv6.New(m, checker, rt)` 在 S10 构造时注册周期任务；`deploy.New(..., IPv6Enroll: ip)`；`routers.New…(Options{IPv6Host: ip})` 小接口 | S10 | ipv6_wire_test.go；routers 有 4 个测试文件替换 IPv6HostRequest |
| W33 | `registerStartup` cmd/configflow/service.go:78 | W/B | 启动步骤：load-config（`repo.PrepareConfig`）、substore prune、agent-manager-init（`agents.Init`）、metrics-load-history | 顺序依赖：启动步骤按注册顺序执行，Wire 里重复注册时按名字原地替换 | 由 server 在 S12 显式按顺序 `rt.RegisterStartup(...)`；agent-manager-init 改成 S8 直接构造 Manager（New 不做 I/O 的话仍可作为启动步骤做加载） | S12 | — |
| W34 | `apisettings.SetWebListener` api/settings/settings.go:38，由 service.go:186 调用 | A | 路由注册**之后**才把 `*weblistener.Manager` 塞进 api/settings | 真正的晚绑定：weblistener 需要 app（Edge.API）才能建，而 app 的路由需要 weblistener | 保留一个窄接口字段，`settings.Handler{Web WebListener}`，由 server 在建好 listener 后设置；或者 listener 先建一个空壳再 Listen（裁定 J5） | S13 | 0 个测试 |
| W35 | `routePackages`/`lateRoutePackages` server.go:80/108 | W | 24+2 个 `Register(r)`。late 只是为了确保 tasks、notifications 的槽位都已经装好 | 包级函数 + 定位器 | 每个 api 包 `New(deps) *Handler` + `h.Register(r)`；槽位完整性改成在构造期校验，late 顺序不再需要 | S11 | 29 个测试文件用 `server.NewApp()`，改成 `server.New(Options{Repo: …})` |

---

## 2. 非测试代码里的 `func init()`（routers 包里的 6 个归协调者，只列出来不展开）

| 编号 | 位置 | 类别 | 做什么 | 改成什么 | 测试影响 |
|---|---|---|---|---|---|
| I01 | importer/parse.go:18 | D | 把一串规则名填进 `supportedRules` map | 改成 map 字面量或 `var supportedRules = setOf(strings.Fields(...))`，不动语义 | 无 |
| I02 | oxidns/validation.go:112 | D | 预热 `schemas()`，把内嵌的 schema 读进 `catalog`（本来就是 sync.Once 惰性） | 删掉 init，保持惰性；或者 `sync.OnceValues`。不需要钩子 | 无 |
| I03 | auth/auth.go:50 | B | `current.Store(&Config{SecretKey: randomSecret()})`，进程默认鉴权配置；cmd 再用 `auth.Configure(FromEnv())` 覆盖 | 改成 `auth.New(cfg) *Gate`，由 server 构造，`auth.Require` 中间件变成 Gate 的方法。`gateway.isAuthEnabled` 这类读取改成参数。见裁定 J6（auth 有 29 个导入方） | 测试里 `auth.Configure` 改成各自 New |
| I04 | health/owners.go:14 | B/A | `currentProbeEnv.Collaborators = defaultCollaborators(nil)`，默认转换器取 `substore.NativeConverter()`（定位器） | `health.NewProbeEnv(Options{Converter: sub.Client})` 构造参数；`currentProbeEnv` 删除 | 9 个测试文件用 SetProbeEnv，改成构造参数 |
| I05 | discovery/native_bridge.go:312 | D | 把 26 个本包函数装进 `collaboration` 结构体（Python 晚绑定照搬，生产里不会再改） | 删掉间接层，直接调用；如果测试需要替换，就让 `discovery.Service` 带 `collab collaborators` 字段 | 只有 1 个测试文件引用 collaboration |
| I06 | convert/subscription_dialers.go:12 | A | 由 convert 调用 `subs.SetSharedDialers(...)`，把 convert 的函数装进 subs | 有环：convert→subs（8 个文件），subs 不能 import convert | `subs` 中需要拨号器的入口改为接收 `subs.SharedDialers` 参数，或者 subs 的 Service 持有它；由 server 用 `convert.SharedDialers()` 构造 | 0 个测试文件直接 Set（依赖 init 自动装入） |
| I07 | webdav/adapter.go:21 | B | `service.Store(NewService(nil))`，默认备份服务 | 合并到 W06 的 `webdav.New` | 同 W06 |
| I08 | routers/routerfake/ikuai.go:352 | D | 测试假设备的表单字段表 | 不动（改成字面量也行），不在生产路径上 | 无 |
| I09–I14 | routers/{ipv6_pd_write.go:418, dns_guard_plan.go:267, natlist_write.go:25, dns_guard_write.go:410, rospolicy_write.go:42, ipv6_write.go:634} | A（包内） | `RegisterRouterOSHandler(...)` ×9 种 kind；`ReportParts = …`（dns_guard_plan.go:269） | 归协调者：改成 `routerOSHandlers = map[string]RouterOSHandler{...}` 字面量，ReportParts 直接调用 | — |

---

## 3. Hooks 结构体的字段（谁填、谁读）

### 3a. `repo.Hooks`（repo/hooks.go:12、record_hooks.go:43）

| 编号 | 字段 | 类别 | 谁填 | 谁读 | 为何钩子 | 改成什么 |
|---|---|---|---|---|---|---|
| R1 | RecordHooks.JobLockRecord | A | routers（jobLockHook → `RouterJobLock`） | repo/router_guard.go | 有环：routers→repo | `repo.Options{RouterJobLock: func}`，S1 传入。RouterJobLock 依赖包级锁表（routers 内部，归协调者） |
| R2 | ConfigurationCommittedRecords | A | webdav | repo/transaction.go:228 | 有环 | `repo.Subscribe(observer)` 事件订阅（可以多个） |
| R3 | PlanNodeDeletionRecords | A | api/nodes | repo/shared.go:112 | repo 不能 import api 包；但实现只依赖 repo 和 convert | **下沉到 repo，删掉钩子** |
| R4 | SanitizeExternalDocument | A | configexport | repo/importexport.go | 有环：redact→repo | `repo.Options{Exporter}` 小接口 |
| R5 | RedactOxidnsJSON | A | configexport | repo/importexport.go | 同 R4 | 并入 R4 的接口 |
| R6 | RedactRoutersJSON | A | routers（RedactRouters） | repo/importexport.go | 有环 | 并入 R4 的接口，由 server 组合 |
| R7 | RedactEasytierJSON | A→直接调用 | server（convert.RedactEasyTierJSON） | repo/importexport.go:310 | **无环**：repo 已经 import convert | 直接调用 |
| R8 | DefaultServerDomain | A | server | repo/scope.go:prepareConfig | repo 不能 import server | 作为 PrepareConfig 的参数 |
| R9 | InitAgentManager | A（死） | agents.Wire | repo.LoadConfig（生产里没有调用方） | — | 删除，连同 LoadConfig 一起删（7 个测试文件改用 PrepareConfig） |

### 3b. `agents.Hooks`（agents/hooks.go:27）以及列表型钩子

| 编号 | 字段 | 类别 | 谁填 | 谁读 | 为何钩子 | 改成什么 |
|---|---|---|---|---|---|---|
| G1 | PinnedWithheld | A→直接调用 | rulesync | **只有 api/rules**（adapter.go:271 等 5 处） | api/rules→rulesync 无环 | api/rules handler 持有 rulesync 服务 |
| G2 | AddMetricsJSON | A | metrics | agents/registry.go:479 | 有环 | `agents.Options{Metrics}` |
| G3 | DeleteMetricsHistoryContext | A | metrics | agents/mutations.go:313 | 有环 | 同 G2 |
| G4 | DeleteTrafficHistoryContext | A | metrics | agents/mutations.go:317 | 有环 | 同 G2（metrics 同时管理 traffic） |
| G5 | PublicationComplete | A | monitor | agents/deployments.go:84 | 有环 | `agents.Options{Publication}` |
| G6 | PublicationCheckpoint | A | monitor | agents/deployment_hook_bridge.go:20，**deploy/publish.go:343** | agents 有环；deploy→monitor 无环 | agents 走 Options；deploy 直接调用 |
| G7 | AddressFollowTyped | A→直接调用 | upgrades | **只有 api/agents** | api/agents 已经 import upgrades | 直接调用 |
| G8 | AddressConfirmedTyped | A→直接调用 | upgrades | **只有 api/agents** | 同 G7 | 直接调用 |
| G9 | ReceiptMatches | A | deploy | agents/deployment_hook_bridge.go:12 | 有环 | `agents.Options{Receipts}` |
| G10 | HAServiceOperationTyped | A | dnsha | agents/remote_model.go:422 | 有环 | `agents.Options{HA}` 或两阶段 `m.SetHA`（J1） |
| G11 | HADeployment | A | dnsha | agents/ha.go:27 | 有环 | 同 G10 |
| G12 | HAActivate | A | dnsha | agents/ha.go:39 | 有环 | 同 G10 |
| G13 | InstallationProfileTyped | A→直接调用 | installation | **只有 api/agents** | api/agents 已经 import installation | 直接调用 |
| G14 | InstallationRegisterTyped | A→直接调用 | installation | **只有 api/agents** | 同 G13 | 直接调用 |
| G15 | OnAgentDeleted（deleteHooks） | A | metrics（traffic_store）、health（domain_probe、region_check） | agents/hooks.go:218/229（DELETE 路由） | metrics 有环；health 无环 | `agents.Options{DeletedStores []NamedDeleter}`，顺序固定写在 server 里，`AgentDeletedStores` 常量的排序逻辑也就不需要了 |
| G16 | OnManagerInit（initHooks） | A | dnsfollow、dnsha、agents/ipv6 | agents.Init | 有环 | Manager 唯一，server 在 Manager 构造后直接按顺序构造这三个服务（S9/S10） |
| G17 | SetRuleSyncRow / SetDNSFollowRow | A | rulesync、dnsfollow | agents.agentRows → `tasks.SetRowBuilder(SlotAgents)` | agents 不能 import 子包 | 把 `agentRows` 挪出 agents 包（放进 server 或新建 `agents/agenttasks`），它 import agents、rulesync、dnsfollow 三者后用 `tasks.Register(SlotAgents, ...)` 注册 |

---

## 4. 导出的 Set* / Reset* / Register* / On*（剔除误报后）

`hooks.txt`（114 行）加上补漏，共 143 个声明。下列误报（X）已经剔除：`yamlx.Set`、`devicenotes.Set`、`oxidns.SetNote`、`model.SetStoredMember`、`convert.SetGeneralOption`、`deploy.SettingsDigest`、`gateway.SettingsOf`、`discovery.SettingsPayload`、`repo.SetRequest`、`repo.ResetConfigContext`（这两个是对 ctx 里 Scope 的请求级操作）、`monitor.ResetStatistics`、`metrics.SetFlushInterval`、`metrics.SetTrafficRetention`（这三个是业务操作）、`localhttp.Register`（本地 HTTP 入口）、26 个 api 和 mcp 的路由 `Register`、`RegisterWithOptions`、`RegisterAlways`（见第 7 节），以及各包的 `Wire`（见第 1 节）。

| 编号 | 位置 | 类别 | 谁填（非测试） | 谁用 | 为何 | 改成什么 | 构造步骤 | 测试影响（测试文件数） |
|---|---|---|---|---|---|---|---|---|
| H01 | repo/hooks.go:33 RegisterHooks | A | configexport、webdav、api/nodes、routers、agents、server | repo | 见 3a | `repo.Options` 加 `Subscribe` | S1 | 4 |
| H02 | repo/hooks.go:68 SetHooks | C | — | 测试 | 测试替换 | 删除；测试 `repo.Open(dir, Options{…fake})` | — | 25 |
| H03 | repo/scope.go:68 SetRepository | B | cmd/service.go:131（只调用一次）、tools/proddiff ×2 | GetRepository | Python `set_repository` 照搬 | 删除；仓库通过构造参数传下去 | S1 | 14 |
| H04 | agents/hooks.go:93 RegisterHooks | A | 7 个包 | agents、api/agents、api/rules、deploy | 见 3b | `agents.New(ctx, repo, Options{…})` | S8 | 11 |
| H05 | agents/hooks.go:141 ResetHooks | C | — | 测试 | 清全局 | 删除 | — | 24 |
| H06 | agents/hooks.go:188 OnAgentDeleted | A | metrics、health | agents | 见 G15 | Options.DeletedStores | S8 | 1 |
| H07 | agents/hooks.go:240 OnManagerInit | A | dnsfollow、dnsha、ipv6 | agents.Init | 见 G16 | 显式构造 | S9 | 0 |
| H08 | agents/singleton.go:83 SetRuleSyncRow | A | rulesync | agents.agentRows | 见 G17 | 挪出 agents | S10 | 0 |
| H09 | agents/singleton.go:86 SetDNSFollowRow | A | dnsfollow | 同上 | 同上 | 同上 | S10 | 0 |
| H10 | agents/client.go:345 SetDefaultClient | C | — | 测试 | 换 Agent HTTP 客户端 | `agents.Options{Client}` 字段 | S8 | 4 |
| H11 | agents/runtime_state.go:293 ResetRuntime | C（清 E） | — | 测试 | 清包级 `states` map | `states` 改成 Manager 字段后删除 | — | 21 |
| H12 | agents/testhooks.go:7 SetLatestVersionForTest | C | — | 测试 | 测试接缝写在生产文件里 | `Options.LatestVersion` | — | 1 |
| H13 | agents/metrics/flush.go:209 SetNativeTrafficFlush | A（自装） | metrics 自己 | metrics | 不需要 | 直接调用 flushTraffic | — | 2 |
| H14 | agents/metrics/flush.go:79 ResetFlushState | C | — | metrics.Reset | 清 E | 删除 | — | 0 |
| H15 | agents/metrics/flush.go:87 SetStartedAt | C | — | 测试 | 时钟 | 字段 | — | 2 |
| H16 | agents/metrics/traffic.go:359 SetTrafficNow | C | — | 测试 | 时钟 | `clock` 字段 | — | 1 |
| H17 | agents/metrics/traffic.go:590 ResetTraffic | C | — | 测试 | 清 trafficHistories | 删除 | — | 2 |
| H18 | agents/metrics/wire.go:39 Reset | C | — | 测试 | 清 histories | 删除 | — | 3 |
| H19 | agents/metrics/wire.go:48 RegisterStartup | B | cmd **和** metrics.Wire（重复） | lifecycle | 注册顺序 | 由 server 显式注册一次 | S12 | — |
| H20 | agents/monitor/history.go:91 SetRankingHistory | A（自装） | monitor.Wire | monitor | 不需要 | 默认值；测试用字段注入 | — | 1 |
| H21 | agents/monitor/monitor.go:378 ResetRankingsCache | C | — | 测试 | 清 nativeRankingsCache | 删除 | — | 4 |
| H22 | agents/monitor/panel.go:396 ResetCoreLatest | C | — | 测试 | 清 coreLatest | 删除 | — | 1 |
| H23 | agents/monitor/rankings.go:785 ResetRankingStores | C | — | 测试 | 清 stores | 删除 | — | 1 |
| H24 | agents/monitor/ruleupdate.go:256 SetHARulesJSON | A | dnsha | monitor | 有环（dnsha→rulesync→monitor） | `monitor.Options{HARules}` | S9 | 1 |
| H25 | agents/rulesync/hooks.go:17 SetPairAdvance | A | dnsha | rulesync | 有环 | `rulesync.Options{Pair: ha}` | S10 | 1 |
| H26 | agents/rulesync/hooks.go:46 SetPairFailureMessage | A | dnsha | rulesync | 有环 | 同上 | S10 | 1 |
| H27 | agents/trafficstore/store.go:128 ResetCache | C | — | 测试 | 清 statusIndex | 删除 | — | 2 |
| H28 | agents/trafficstore/store.go:700 SetNow | C | — | 测试 | 时钟 | 字段 | — | 1 |
| H29 | agents/trafficstore/store.go:139 SetCacheLimit | C | — | 测试 | 调容量 | 字段 | — | 1 |
| H30 | agents/upgrades/address.go:60 ResetAddressState | C | — | 测试 | 清 attempts、corruptWarned | 删除 | — | 4 |
| H31 | agents/upgrades/artifacts.go:323 ResetArtifactCache | C | — | 测试 | 清 cacheIndex | 删除 | — | 4 |
| H32 | agents/upgrades/testhooks.go:13–57 Set{Request,Artifacts,Reaches,Ask,RetrySeconds}ForTest | C | — | 测试 | 测试接缝写在生产文件里 | `upgrades.Service` 的字段 | — | 4/1/1/1/1 |
| H33 | api/agents/content_wire.go:30 SetContentPrepare | A（自装） | api/agents 自己 | api/agents | 不需要 | 直接调用，测试用 Checker Options | — | 2 |
| H34 | api/settings/settings.go:38 SetWebListener | A | cmd | api/settings | 晚绑定（W34） | handler 字段 | S13 | 0 |
| H35 | convert/mihomo_environment.go:72 SetMihomoEnvironment | A | mihomoenv | convert | 有环 | `convert.Env` 参数（J2） | S6 | 23 |
| H36 | convert/mihomo_integrations.go:28 SetMihomoIntegrations | A | gateway、ippresence（重复 FullTun） | convert | 有环 | 并入 `convert.Env.Integrations` | S6 | 9 |
| H37 | deploy/publish_hooks.go:31 SetGatewayGuards | A→直接调用 | server | deploy | **无环** | deploy 直接用 host.Bridge 或 Options | S10 | 0（gateway_guards_test 走 server） |
| H38 | deploy/publish_hooks.go:40 SetIPv6Enroll | A | agents/ipv6 | deploy | 有环 | `deploy.Options{IPv6: ip}` | S10 | 0 |
| H39 | deploy/publish_hooks.go:50 SetPairPublication | A | dnsha | deploy | 有环 | `deploy.Options{Pair: ha}` | S10 | 2 |
| H40 | gateway/onboarding/adapter.go:197 SetAdoptOwnedWrite | A | server（setup.AdoptOwnedWrite） | onboarding | **无环** | 见 W25 | S10b | 3 |
| H41 | gateway/rule_source.go:14 SetRuleSourceReader | A | api/rules | gateway | 分层原因 | rulesource 服务 | S6b | 4 |
| H42 | health/runner.go:107 SetProbeEnv | C | —（生产从来不调用） | 测试 | 测试替换（**没有加锁**） | 构造参数 | — | 9 |
| H43 | lifecycle/lifecycle.go:49 RegisterPeriodic | B | 约 15 处 | lifecycle | 进程 Runtime | `rt.RegisterPeriodic` 实例方法 | S2 | 由 28 个测试文件里的 Configure 承担 |
| H44 | lifecycle/lifecycle.go:56 RegisterStartup | B | cmd、metrics、substore、webdav | lifecycle | 同上 | 同上 | S2 | 同上 |
| H45 | lifecycle/lifecycle.go:59 RegisterShutdown | B | cmd、metrics、api/agents、webdav、discovery | lifecycle | 同上 | 同上 | S2 | 同上 |
| H46 | notifications/notifications.go:45 SetInputs | A | server、agents、routers | notifications | Rows/Missing **无环**；Agents/Summarize 有环 | `notifications.New(repo, tasks, Options{Agents, Summarize})` | S3 + 晚绑定 Agents | 12 |
| H47 | notifications/notifications.go:65 ResetInputs | C | — | 测试 | 清全局 | 删除 | — | 3 |
| H48 | oxidns/adapter.go:248 SetNativeClassicalIPLines | A→直接调用 | ippresence | oxidns | **无环** | 直接 import 或字段 | S6 | 3 |
| H49 | provider/hooks.go:111 SetAggregationRenderer | A | aggregations | provider | 有环 | provider Options | S6 | 3 |
| H50 | provider/hooks.go:192 SetDeliveryEnv | C | —（生产用 DefaultDeliveryEnv，里面调用 substore.Default 定位器） | 测试 | 测试替换 | `provider.New(Options{Fetch: sub.Client…})` | S6 | 10 |
| H51 | provider/hooks.go:98 SetLocations | A | deploy | provider | 有环 | provider Options | S6 | 8 |
| H52 | provider/response.go:49 SetResponseSeconds | C | cmd/manifest_proddiff.go（build tag `configflow_proddiff`） | provider | 测试和差分 | provider Options 字段 | S6 | 4 |
| H53 | refresh/env.go:9 SetRefreshEnv | C | —（生产走 DefaultRefreshEnv） | 测试 | 测试替换 | `refresh.New(Options)` | S5 | 13 |
| H54 | refresh/rows.go:284 OnRulesRefreshed | A | server | refresh | 见 W18 | 实例事件订阅 | S10 | 0 |
| H55 | routers/routeros.go:905 RegisterRouterOSHandler | A（包内） | routers 自己的 init ×6 | routers | 照搬 | 归协调者（改成字面量表） | — | — |
| H56 | routersplit/records.go:29 SetSessionReader | A→直接调用 | setupflow | routersplit | **无环** | 直接调用 | — | 3 |
| H57 | setup/owners.go:119 SetExternalOwners | C | — | 测试 | 整体替换 | `setup.New(Options)` | — | 4 |
| H58 | setup/owners.go:154 RegisterExternalOwners | A | server ×3 | setup | **都无环**（W24、W25、W27） | `setup.New(repo, Options{Rules, Installation, Gateway, VerifyRouter})` | S10b | 0 |
| H59 | subs/core_status.go:24 ResetCoreVersionWarnState | C | — | 测试 | 清 coreWarnLast（E） | 实例字段 | — | 2 |
| H60 | subs/shared_dialers.go:44 SetSharedDialers | A | convert 的 init | subs | 有环 | 见 I06 | S4 | 0 |
| H61 | substore/cache.go:38 SetContentSyncInvalidator | A | api/agents | substore | 有环 | `sub.OnContentChange` 订阅 | S11 前 | 3 |
| H62 | substore/wire.go:46 SetDefault | B/C | — | 测试 | 换单例 | 删除，实例注入 | S5 | 15 |
| H63 | substore/wire.go:76 RegisterStartup | B | cmd **和** substore.Wire（重复） | lifecycle | 顺序 | server 显式注册一次 | S12 | — |
| H64 | webdav/adapter.go:27 SetDefault | B/C | — | 测试 | 换单例 | 删除 | S7 | 9 |
| H65 | tasks/tasks.go:107 SetIntervalEditor | A | metrics | tasks | 有环 | `tasks.Registry.AddInterval` 实例方法（只在构造期调用） | S3 | 1 |
| H66 | tasks/tasks.go:88 SetRowBuilder | A | agents、metrics、refresh、discovery、routers | tasks | 有环 | `tasks.Registry` 实例方法，或 `tasks.New([]Slot{...})` 由 server 传入完整列表 | S3/S10 | 14 |
| H67 | tasks/tasks.go:98 SetRunner | A | webdav、dnsfollow、metrics、refresh、discovery、routers | tasks | 有环 | 同上 | 同上 | 5 |
| H68 | logx/logx.go Setup（→ `logx.current`） | B | cmd | 所有包里的 `var logger = logx.Get(name)` | 进程日志根 | **建议不动**（J7）：Logger 只是一个名字，实际取 current 根；保留单例，测试用 `logx.Setup` 隔离 | S0 | — |

---

## 5. globals.txt 逐项（127 项；和第 4 节重复的写「见 Hxx」）

| 编号 | 位置 | 类别 | 谁填（W） | 谁用 | 改成什么 | 测试影响（本包 _test 赋值文件数 / 包外） |
|---|---|---|---|---|---|---|
| V001 | agents/metrics/history.go:737 appendLines | C | — | metrics | `History.fs` 字段或保留未导出变量 | 2 |
| V002 | agents/metrics/history.go:53 isValidUTF8 | D | — | metrics | 改成直接调用 `utf8.Valid` | 0 |
| V003 | agents/metrics/history.go:188 now | C | — | metrics | `History.clock` 字段 | 2 |
| V004 | agents/metrics/flush.go:228 recordStatus | C | — | metrics | `Service.recordStatus` 字段（实际是 notifications.TaskStatus 定位器的包装） | 1 |
| V005 | agents/metrics/history.go:779 replaceFile | C | — | metrics | fs 字段 | 2 |
| V006 | agents/metrics/flush.go:205 trafficFlush | A（自装） | traffic_wire.go | metrics | 见 H13 | 0 |
| V007 | agents/metrics/traffic.go:568 trafficHistories | E | traffic.go | metrics | `metrics.Service.traffic`（Manager 唯一，按 dataDir 建 map 不再需要） | 由 ResetTraffic 负责清，2 |
| V008 | agents/metrics/traffic.go:355 trafficNow | C | traffic.go（SetTrafficNow） | metrics | clock 字段 | 1（经由 Set） |
| V009 | agents/metrics/history.go:702 unlink | D | — | metrics | 直接调用 os.Remove | 0 |
| V010 | agents/metrics/flush.go:75 utcNow | C | — | metrics | clock 字段 | 1 |
| V011 | agents/monitor/panel.go:410 downloadSettings | C | — | monitor | 内部调用 GetRepository，改成 `Service.repo` 后删除 | 1 |
| V012 | agents/monitor/ruleupdate.go:253 haJSON | A | ruleupdate.go（SetHARulesJSON） | monitor | 见 H24 | 0 |
| V013 | agents/monitor/history.go:84 historyFactory | A（自装） | history.go | monitor | 见 H20 | 0 |
| V014 | agents/monitor/panel.go:444 latestCoreVersion | C | — | monitor | 字段 | 2 |
| V015 | agents/monitor/monitor.go:375 monotonic | C | — | monitor | clock 字段 | 3 |
| V016 | agents/monitor/rankings.go:422 nowSeconds | D | — | monitor | 普通函数（没有测试替换） | 0 |
| V017 | agents/monitor/monitor.go:369 nowUTC | D | — | monitor | 普通函数 | 0 |
| V018 | agents/monitor/rankings.go:830 rankingSleep | C | — | monitor | 字段 | 2 |
| V019 | agents/monitor/state.go:424 replaceFile | C | — | monitor | fs 字段 | 1 |
| V020 | agents/monitor/query_protocol.go:15 requestNative | C | — | monitor | Service 字段（Agent 传输） | 3 |
| V021 | agents/monitor/panel.go:440 requestRule | C | — | monitor | 注入 `*rulefetch.Client` | 2 |
| V022 | agents/monitor/rankings.go:769 stores | E | rankings.go | monitor | `monitor.Service.rankings`（单一 dataDir） | 由 ResetRankingStores 负责清 |
| V023 | agents/monitor/publication.go:16 tokenHex | C | — | monitor | rand 字段 | 2 |
| V024 | agents/rulesync/wire.go:12 activeService | B/E | wire.go（Get） | rulesync | 删除；`rulesync.New` 返回的实例由 server 持有并传给 api/agents、api/rules | 0 |
| V025 | agents/rulesync/hooks.go:11 pairAdvance | A | dnsha | rulesync | 见 H25 | 0 |
| V026 | agents/rulesync/hooks.go:38 pairFailure | A | dnsha | rulesync | 见 H26 | 0 |
| V027 | agents/trafficstore/store.go:697 now | C | store.go（SetNow） | trafficstore | clock 字段 | 1 |
| V028 | agents/trafficstore/store.go:197 statusSignature | D | — | trafficstore | 普通函数 | 0 |
| V029 | agents/upgrades/upgrades.go:37 artifactsFunc | C | testhooks.go | upgrades | 字段 | 1（经由 Set） |
| V030 | agents/upgrades/address.go:51 askOverride | C | testhooks.go | upgrades | 字段 | 1 |
| V031 | agents/upgrades/upgrades.go:36 now | C | — | upgrades | clock 字段 | 2 |
| V032 | agents/upgrades/artifacts.go:128 openHook | C | — | upgrades | 字段 | 1 |
| V033 | agents/upgrades/address.go:50 reachesOverride | C | testhooks.go | upgrades | 字段 | 1 |
| V034 | agents/upgrades/artifacts.go:129 readHook | C | — | upgrades | 字段 | 2 |
| V035 | agents/upgrades/protocol.go:104 requestOverride | C | testhooks.go | upgrades | 字段 | 4（经由 Set） |
| V036 | agents/upgrades/address.go:46 startThread | C | testhooks.go | upgrades | 注入 lifecycle spawner | 1 |
| V037 | agents/upgrades/upgrades.go:38 updateIDSource | D | — | upgrades | 普通函数 | 0 |
| V038 | agents/time.go:15 clock | C | — | agents | `Manager.clock` | 1 |
| V039 | agents/hooks.go:161 deleteHooks | A | hooks.go | agents | 见 G15 | 0 |
| V040 | agents/singleton.go:77 dnsFollowRow | A | — | agents | 见 G17 | 0 |
| V041 | agents/hooks.go:89 hooks | A | hooks.go | agents | 见 3b | 由 ResetHooks 负责 |
| V042 | agents/hooks.go:162 initHooks | A | hooks.go | agents | 见 G16 | 0 |
| V043 | agents/runtime_state.go:454 removeLegacyFile | C | — | agents | 字段 | 1 |
| V044 | agents/client.go:207 resolveHook | C | — | agents | `Client.Resolve` 字段 | 3 |
| V045 | agents/singleton.go:76 ruleSyncRow | A | — | agents | 见 G17 | 0 |
| V046 | agents/singleton.go:20 singleton | B | singleton.go | agents（Get/Init） | 删除；server 持有 `*agents.Manager` | 0（间接：所有用 agents.Get 的测试） |
| V047 | agents/manager.go:200 testFailUpdate | C | — | agents | Manager 测试字段 | 2 |
| V048 | agents/manager.go:202 testRecordHeartbeat | C | — | agents | 同上 | 1 |
| V049 | api/agents/content_wire.go:22 contentPrepare | A（自装） | content_wire.go | api/agents | 见 H33 | 0 |
| V050 | api/logs/logs.go:35 LogFilePath | B/C | — | api/logs | `logs.Handler{Path}`，由 server 传入 `logx.DefaultFile(dataDir)` | 4 |
| V051 | api/logs/logs.go:38 openFile | C | — | api/logs | handler 字段 | 1 |
| V052 | api/stats/stats.go:40 Now | C | — | api/stats | handler.clock | 3 |
| V053 | convert/mihomo_environment.go:66 environmentState | A | mihomoenv | convert | 见 H35 | 23（经由 Set） |
| V054 | convert/mihomo_integrations.go:22 integrationState | A | gateway、ippresence | convert | 见 H36 | 9 |
| V055 | convert/mihomo_nodes.go:183 parseProxyYAML | C | — | convert | 字段或保留未导出变量 | 1 |
| V056 | deploy/publish_hooks.go:23 publicationHooks | A | dnsha、ipv6、server | deploy | 见 H37–H39 | 2 |
| V057 | discovery/collaborators.go:44 collaboration | D | native_bridge.go 的 init | discovery | 见 I05 | 0 |
| V058 | discovery/matcher.go:13 matcherCache | E | matcher.go | discovery | `discovery.Service.matchers` | 1 |
| V059 | discovery/summary.go:20 now | D | — | discovery | 普通函数 | 0 |
| V060 | gateway/onboarding/adapter.go:191 adoptHook | A | server | onboarding | 见 H40 | 3（经由 Set） |
| V061 | gateway/apply.go:18 isAuthEnabled | C（包 B） | — | gateway | 改成 `gateway.Config` 的参数（来自 auth Gate） | 1 |
| V062 | gateway/adapter.go:285 ruleSourceReader | A | api/rules | gateway | 见 H41 | 4 |
| V063 | health/check/region.go:467 Services | D | — | health/check | 不动（静态目录数据） | 0 |
| V064 | health/runner.go:99 currentProbeEnv | B/A | owners.go 的 init、runner.go | health | 见 I04、H42 | 9 |
| V065 | httpx/edge.go:382 gzipPool | D | — | httpx | 不动 | 0 |
| V066 | installer/installertest/nopython.go:31 legacy | D | — | 测试支持包 | 不动 | 0 |
| V067 | installer/installertest/deltas.go:35 load | D | — | 测试支持包 | 不动 | 0 |
| V068 | installer/docker_ha_guide.go:317 bootstrapCommand | C | — | installer | 字段 | 1 |
| V069 | installer/docker_ha_enrollment.go:38 enrollmentLocalNow | C | — | installer | clock | 1 |
| V070 | installer/docker_ha_enrollment.go:35 enrollmentNow | C | — | installer | clock | 1 |
| V071 | installer/docker_ha_enrollment.go:47 randomBytes | C | — | installer | rand 字段 | 2 |
| V072 | jsondecode/jsondecode.go:102 plainObjects | D | — | jsondecode | 不动（按类型的反射缓存） | 0 |
| V073 | lifecycle/lifecycle.go:21 defaultRuntime | B | lifecycle.go（Configure） | lifecycle（7 处） | `rt := lifecycle.NewRuntime(opts)`，由 server 往下传；`lifecycle.Detach/Stopping/ClockFrom(ctx)` 等 ctx 函数保留 | 28 个测试文件用 Configure，改成 `NewRuntime` |
| V074 | logx/logx.go:121 current | B | logx.go（Setup） | logx | 建议不动（J7） | — |
| V075 | model/encode.go:354 cycleTypes | D | — | model | 不动 | 0 |
| V076 | model/stored.go:582 hasPlainCache | D | — | model | 不动 | 0 |
| V077 | model/plain_switch.go:374 inlineIndexCache | D | — | model | 不动 | 0 |
| V078 | model/stored.go:797 memberFieldCache | D | — | model | 不动 | 0 |
| V079 | model/ordered.go:231 orderedElemCache | D | — | model | 不动 | 0 |
| V080 | model/stored.go:103 plainFieldCache | D | — | model | 不动 | 0 |
| V081 | mosdns/ruleproxy.go:39 downloadRule | C/B | — | mosdns | **注意**：值是 init 时取的 `rulefetch.DefaultClient.Download`，测试换掉 DefaultClient 对它不生效。改成注入 `*rulefetch.Client` | 0 |
| V082 | mosdns/ruleproxy.go:49 fetchNativeFn | C | — | mosdns | 字段 | 1 |
| V083 | notifications/store.go:37 Clock | C | — | notifications | 实例 clock 字段 | 包外 7 |
| V084 | notifications/notifications.go:41 inputs | A | server、agents、routers | notifications | 见 H46 | 12 |
| V085 | oxidns/adapter.go:241 classicalHook | A | ippresence | oxidns | 见 H48 | 3 |
| V086 | oxidns/common.go:601 fallbackOff | D | — | oxidns | 不动（节点构造常量） | 0 |
| V087 | oxidns/common.go:600 fallbackOn | D | — | oxidns | 不动 | 0 |
| V088 | oxidns/common.go:602 fallbackSecondary | D | — | oxidns | 不动 | 0 |
| V089 | oxidns/zone.go:277 idna2003 | D | — | oxidns | 不动 | 0 |
| V090 | provider/hooks.go:93 aggregationProvider | A | aggregations | provider | 见 H49 | 3 |
| V091 | provider/hooks.go:187 env | C | — | provider | 见 H50 | 10 |
| V092 | refresh/env.go:6 nativeEnv | C | — | refresh | 见 H53 | 13 |
| V093 | refresh/rows.go:231 refreshLoop | E | — | refresh | `refresh.Service.loop` | 0 |
| V094 | refresh/rows.go:276 rulesRefreshed | A | server | refresh | 见 H54 | 0 |
| V095 | repo/scope.go:36 defaultRepository | B | scope.go | repo（GetRepository） | 删除（第 6 节） | 由 SetRepository 负责，14 |
| V096 | repo/hooks.go:27 hooks | A | 多处 | repo | 见 3a | 25 |
| V097 | repo/repository.go:229 nowISO | C | — | repo | `Repository.clock` | 1 |
| V098 | repo/system.go:20 randomHex | C | — | repo | `Repository.rand` | 1 |
| V099 | repo/system.go:30 tokenURLSafe | C | — | repo | 同上 | 1 |
| V100 | repo/storage_upgrade.go:18 upgradeRowHook | C | — | repo | 存储升级的测试字段 | 1 |
| V101 | routers/publish.go:65 CaptureManual | A（自装） | rule_sync_tasks.go | routers | 归协调者：直接调用同步服务 | 1 |
| V102 | routers/inspect.go:36 IPv6HostRequest | A | server（wireIPv6） | routers | 见 W32 | 4 |
| V103 | routers/router.go:322 NewRouterID | C | — | routers | 归协调者：字段 | 1 |
| V104 | routers/report.go:174 ReportParts | A（自装） | dns_guard_plan.go 的 init | routers | 归协调者：直接调用 | 1 |
| V105 | routers/desired.go:163 RulesetContent | A | api/rules | routers | 见 W28 | 1 + 包外 2 |
| V106 | routersplit/records.go:23 sessionReaderState | A | setupflow | routersplit | 见 H56 | 3 |
| V107 | routers/routeros.go:899 rosHandlerState | A（包内） | routers 的 init | routers | 归协调者 | 0 |
| V108 | routers/rule_sync.go:503 ruleSyncLoop | E | — | routers | 归协调者：同步服务字段 | 0 |
| V109 | rulefetch/fetch.go:46 DefaultClient | B | — | rulefetch 和 9 个以上的包 | 由 server 构造 `*rulefetch.Client` 注入（monitor、mosdns、api/rules 等）；保留一个只读的包级默认值也可以接受（J8） | 包外 3 |
| V110 | rules/text.go:17 idnaProfile | D | — | rules | 不动 | 0 |
| V111 | rules/matchercache.go:201 testHookAfterRead | C | — | rules | 字段 | 1 |
| V112 | rules/regex.go:589 wordSpans | D | — | rules | 不动（OnceValue） | 0 |
| V113 | server/server.go:108 lateRoutePackages | W | — | server | 见 W35 | 0 |
| V114 | server/server.go:80 routePackages | W | — | server | 见 W35 | 0 |
| V115 | server/server.go:124 wirePackages | W | — | server | 见第 1 节 | 0 |
| V116 | setup/owners.go:112 ownersState | A | server | setup | 见 H58 | 4 |
| V117 | subs/shared_dialers.go:37 dialerCollaborators | A | convert 的 init | subs | 见 I06 | 0 |
| V118 | substore/cache.go:117 getShared | C | — | substore | 字段 | 2 |
| V119 | tasks/tasks.go:84 intervals | E | tasks.go | tasks | `tasks.Registry` | 1 |
| V120 | tasks/tasks.go:82 rows | E | tasks.go | tasks | 同上 | 14 |
| V121 | tasks/tasks.go:83 runners | E | tasks.go | tasks | 同上 | 5 |
| V122 | testgolden/golden.go:86 dependencyCache | D | — | 测试支持包 | 不动 | 0 |
| V123 | testgolden/fingerprint.go:60 processFingerprint | D | — | 测试支持包 | 不动 | 0 |
| V124 | testgolden/sources.go:22 sourceAudits | D | — | 测试支持包 | 不动 | 0 |
| V125 | webdav/adapter.go:125 automaticWorker | E/B | — | webdav | `webdav.Service.worker` | 0 |
| V126 | yamlx/yamlxtest/history.go:54 frozenSummaries | D | — | 测试支持包 | 不动 | 0 |
| V127 | yamlx/yamlxtest/history.go:124 readerHistories | D | — | 测试支持包 | 不动 | 0 |

### 5b. globals.txt 没覆盖的同类状态（类型里不含函数，用 grep 补出来，供归属参考）

| 位置 | 类别 | 归属 |
|---|---|---|
| webdav/adapter.go:19 `service atomic.Pointer[Service]` | B | webdav.New |
| substore/cache.go:35 `contentSync atomic.Pointer` | A | 见 H61 |
| substore/wire.go `defaultService` | B | substore.NewService，server 持有 |
| api/settings/settings.go:34 `webListener` | A | 见 W34 |
| auth/auth.go:48 `current` | B | 见 I03 |
| localhttp/localhttp.go:60 `application` | B | 本地 HTTP 入口（cmd 的 `app.RegisterLocal`），保留或改成 App 字段 |
| agents/runtime_state.go:55 `states`、`legacyChecked` | E | Manager 字段（ResetRuntime 被 21 个测试文件使用，可以随之删除） |
| agents/metrics/wire.go:19 `histories` | E | metrics.Service |
| agents/monitor/monitor.go:380 `nativeRankingsCache`；panel.go:390 `coreLatest/coreLocks` | E | monitor.Service |
| agents/upgrades/address.go:44 `attempts/corruptWarned`；artifacts.go:121 `cacheIndex` | E | upgrades.Service |
| agents/trafficstore/store.go:119 `statusIndex/damagedIndex`；warnings.go:11 | E | trafficstore.Store（由 server 构造、注入 metrics、discovery、deploy、api/agents） |
| agents/dnsfollow/wire.go:13 `services.byManager`；dnsha/hooks.go:18 `services` | E | 删除（Manager 唯一） |
| api/agents/content_wire.go:21 `contentChecks.checkers` | E | 删除，只保留一个 Checker |
| agents/ipv6/wire.go:213 `scheduler`、`schedulerLoop` | E | ipv6.Service |
| agents/rulesync/wire.go:38 `loop`；dnsfollow、dnsha 的 `loop`（agents.NewWaker） | E | 各自服务字段 |
| discovery/jobs.go:55 `jobState` | E | discovery.Service |
| deploy/owned_stores.go:8 `previewOwners`；deploy/convert.go:31 `conversions` | E | deploy.Service |
| refresh/owners.go:138 `nodeCacheViews` | E | refresh.Service |
| substore/cache_memo.go:41 `cacheMemo`；runtime.go:143 `leaseOwners` | E | substore.Service |
| subs/core_status.go:20 `coreWarnLast` | E | 实例字段（随 H59 一起） |
| ippresence/ippresence.go:45 `memo` | E | ippresence 实例，或者保留为纯缓存 D |
| aggregations/source_nodes.go:84 `sourceNodeMemo` | E | aggregations 实例 |
| convert/subscription_cache_memo.go:66、mihomo_nodes.go:182 | E/D | 纯 memo，可保留（J9） |
| notifications/notifications.go:547 `routerSigCache` | E | notifications 实例 |
| lockx/order.go:43 `orderChecks` | D | 调试用的锁序检查，不动 |

---

## 6. 服务定位器

**调用量（非测试）**

- `repo.GetRepository(` 158 处 / 46 包：discovery 12、api/agents 11、substore 10、refresh 10、rulefetch 9、api/rulelibrary 8、health 7、api/subscriptions 6、api/profiles 6、api/config 6、provider 4、configgen 4、api/settings 4、agents/metrics 4、routers 3、redact 3、mcp 3、deploy 3、api/stats 3、api/rules 3、api/proxygroups 3、api/nodes 3，其余 24 个包各 1–2 处。repo 包内部另有 9 处（scope.go 里的 ResolveProfile、GetDefaultProfileID、Config、prepareConfig 等）。
- `agents.Get(` / `core.Get(` 56 处 / 13 包：api/agents 36、discovery 4、server 2、api/generate 2、agents/rulesync 2、agents/metrics 2、agents/dnsfollow 2，api/routers、api/profiles、api/linuxgateway、api/ipv6、agents/monitor、agents/dnsha 各 1；agents 包内部另有 2 处（agentRows、NotificationAgents）。
- `substore.Default(` 10 处 / 7 包：api/settings 3、refresh 2，provider、deploy、api/subscriptions、api/nodes、aggregations 各 1；另有 `substore.NativeConverter()` → Default 的间接调用（health 的 init、mihomoenv）。`webdav.Default(` 1 处（api/settings）。
- 隐式取默认仓库的 repo 包级函数（都在 repo/scope.go）：`ResolveProfileID` 27 处 / 16 包、`ResolveProfile` 3/2、`GetDefaultProfileID` 3/3、`Config` 1/1、`LoadConfig` 生产 0 处、`PrepareConfig` 只在 cmd 里用。它们的真正依赖是 **ctx 里的 Scope（请求级缓存和请求信息）+ 仓库**。建议改成 `(*Repository).ResolveProfile(ctx, …)` 和 `(*Repository).ScopedConfig(ctx, id)` 方法，Scope 仍然放在 ctx 里（这是请求作用域，不是定位器）；保留 `WithRepository` 只是为了「后台任务拿到发起方的仓库」，生产里仓库唯一以后可以一起删掉。
- 其他「按 ctx 或 dataDir 取实例」的定位器：`trafficstore.GetNative(ctx)` 6/5、`health.GetProbeStore/GetRegionStore(ctx)` 6/2、`notifications.TaskStatus(ctx, dataDir)` 8/8、`rulefetch.GetRulesDir(ctx)` 1、`rulesync.Get(ctx)` 1、`metrics.For/ForManager` 4、`dnsfollow/dnsha.ForManager` 4、`agents.CurrentHooks()` 4/3、`auth.Current/IsAuthEnabled` 12/5。

**仓库和 Manager 是否在整个进程里唯一：是。**

- `repo.SetRepository`：生产里只在 service.go:131 调用一次（紧跟在 `repo.OpenDefault(dataDir)` 之后）；`tools/proddiff/{baseline,generator}` 各一次（离线工具）。导入走 `repo.ImportAllJSON → applyImport(tx …)`（importexport.go:113/342），恢复和 WebDAV 都只是对同一个 `*Repository` 提交事务，**没有任何运行中替换仓库的路径**。`GetRepository` 的「首次使用时 OpenDefault」分支在生产里也不会走到（SetRepository 先执行）。
- 生产里所有 `repo.WithRepository(ctx, X)`（routers/wire.go:39、rule_sync.go:40、clients/fixed.go:392、deploy/artifacts.go:29、discovery、dnsfollow、upgrades、ipv6、installation、webdav、server/setup.go:53、content_wire.go:61、importexport.go:175）传入的 X 都来自 `m.Repository()`、`s.Repo` 或者调用方已经拿到的默认仓库，所以全是同一个实例。
- `agents.Get` 在 `singleton.repository != repository` 时重建 Manager。生产里仓库唯一，所以只会在第一次调用时创建。启动步骤 `agent-manager-init`（`agents.Init`）会**无条件**新建一个 Manager 并关闭旧的；在它之前执行的只有 load-config 和 subscription-cache-prune，监听端口要等 RunStartup 结束才打开。因此生产里 Manager 在 Init 之后唯一，Init 之前最多存在一个短命的 Manager（前提是有启动步骤调用了 Get，目前没发现）。重建逻辑只服务于测试（httpxtest 每个测试给 ctx 绑一个新仓库）。
- 结论：`repo.Repository`、`agents.Manager`、`substore.Service`、`webdav.Service`、`lifecycle.Runtime`、`rulesync.Service`、dnsfollow、dnsha、ipv6 服务在生产里都是一次性构造、进程内唯一，可以由 server 构造一次，再通过构造参数传下去。按 Manager 或 dataDir 建的 map（dnsfollow、dnsha、contentChecks、metrics.histories、monitor.stores、trafficHistories）都可以退化成单个字段。

---

## 7. api 包的形状

- `internal/api/*` 共 **24 个包**（25 个目录里，`api/nodes` 另有 latency.go 作为第二个 Register 入口），加上 `mcp`，共 26 个路由入口。路由注册（`r.GET/POST/PUT/DELETE/Handle(`）共 **213 处**：GET 77、POST 78、PUT 6、DELETE 1、Handle（多方法）51。分布：agents 66、routers 10、subscriptions 9、rules 8、profiles 7、settings 6、aggregations 6、rulelibrary 6、nodes 5、config 5，其余 1–4。处理函数大约 200 个。
- 现在的形状（看了 stats、settings、agents、routers）：`func Register(r *httpx.Router)` 把**包级函数** `func(c *httpx.Ctx) (*httpx.Response, error)` 挂到路由上；处理函数内部用 `repo.GetRepository(c.Context())`、`agents.Get(ctx)`（api/agents 有 36 处）、`substore.Default()`、`webdav.Default()`、包级 `webListener.Load()`、`agents.CurrentHooks()` 来取依赖，用包级 `var Now/LogFilePath` 做测试接缝。已经有雏形的：`api/routers`、`api/clientdevices` 用了 `type handler struct{…}` + 方法；`api/rules`、`api/rulelibrary` 用了 `RegisterWithOptions(r, Options{…})`。
- 建议改法（一句话）：**每个 api 包定义 `type Handler struct{ Repo *repo.Repository; Agents *agents.Manager; …本包要用的服务或小接口 }`，加上 `func New(deps Deps) *Handler` 和 `func (h *Handler) Register(r *httpx.Router)`，处理函数改成方法，`repo.GetRepository(c.Context())` 换成 `h.Repo`**。可以按包分批迁移：先在 Handler 上实现，旧的包级 `Register` 暂时用 `New(defaultDeps())` 包装，过渡期两种并存。api/agents 体量最大（66 条路由，依赖约 15 个服务），建议拆成几个子 Handler，按现有的 `registerXxx(r)` 分组。

---

## 8. 现有依赖（相关包的 configflow 内部 import，仅列和钩子有关的包；非测试文件）

| 包 | import 的相关内部包 |
|---|---|
| logx | — |
| subs | logx |
| convert | logx, subs |
| repo | convert, logx |
| lifecycle | logx, repo（只为 `repo.WithScope`/`ResetConfigContext`） |
| tasks | lifecycle, repo |
| notifications | lifecycle, logx, repo, tasks |
| redact | repo |
| configexport | redact, repo |
| auth | lifecycle, logx, repo |
| rulefetch | convert, logx, repo |
| mosdns | auth, convert, lifecycle, logx, refresh, repo, rulefetch |
| oxidns | lifecycle, logx, mosdns, repo |
| substore | convert, lifecycle, logx, repo, rulefetch, subs |
| refresh | convert, lifecycle, logx, notifications, repo, rulefetch, subs, substore, tasks |
| provider | convert, lifecycle, logx, refresh, repo, subs, substore |
| convert/mihomoenv | convert, lifecycle, provider, refresh, repo, substore |
| routersplit | convert, repo |
| setupflow | repo, routersplit |
| ippresence | convert, mosdns, oxidns, routersplit, rulefetch |
| gateway | auth, convert, routersplit, rulefetch |
| aggregations | convert, lifecycle, logx, provider, refresh, repo, subs, substore |
| agents | lifecycle, logx, notifications, redact, repo, tasks |
| agents/trafficstore | lifecycle, logx, redact, repo |
| health | agents（**只在 wire.go**）, convert, lifecycle, logx, repo, subs, substore |
| webdav | lifecycle, logx, notifications, repo, tasks |
| routers | agents, lifecycle, logx, mosdns, notifications, oxidns, redact, repo, rulefetch, tasks |
| agents/monitor | agents, lifecycle, logx, redact, repo, rulefetch |
| agents/upgrades | agents, lifecycle, logx, notifications, redact, repo |
| discovery | agents, agents/trafficstore, convert, health, lifecycle, logx, notifications, repo, rulefetch, subs, tasks |
| agents/metrics | agents, agents/trafficstore, discovery, lifecycle, logx, notifications, redact, repo, tasks |
| gateway/host | agents, agents/monitor, gateway, lifecycle, redact, repo, routersplit |
| gateway/onboarding | convert, gateway, lifecycle, oxidns, redact, repo, routersplit, setupflow |
| deploy | agents, agents/trafficstore, convert, gateway, lifecycle, logx, mosdns, oxidns, provider, redact, refresh, repo, routersplit, rulefetch, setupflow, subs, substore |
| deploy/contentsync | deploy, lifecycle, logx, redact, repo |
| agents/rulesync | agents, agents/monitor, convert, deploy, lifecycle, logx, notifications, oxidns, redact, repo, tasks |
| agents/dnsfollow | agents, deploy, lifecycle, logx, notifications, oxidns, redact, repo, tasks |
| agents/dnsha | agents, agents/monitor, agents/rulesync, deploy, lifecycle, logx, oxidns, redact, repo |
| agents/ipv6 | agents, agents/monitor, convert, deploy, deploy/contentsync, lifecycle, logx, redact, repo, routers |
| gateway/installation | agents, agents/monitor, convert, deploy, gateway, gateway/host, gateway/onboarding, lifecycle, logx, provider, redact, repo, routersplit |
| setup | convert, gateway, lifecycle, oxidns, redact, refresh, repo, routers, routersplit, rulefetch, setupflow, subs, substore（**不** import onboarding、installation、host、api/rules） |
| api/rules | agents, auth, gateway, lifecycle, logx, mosdns, oxidns, redact, refresh, repo, routers, rulefetch |
| api/nodes | auth, convert, health, lifecycle, repo, subs, substore |
| api/agents | agents, agents/{dnsfollow,dnsha,metrics,monitor,rulesync,trafficstore,upgrades}, auth, deploy, deploy/contentsync, discovery, gateway/host, gateway/installation, health, lifecycle, logx, notifications, oxidns, redact, repo, rulefetch, setupflow, substore |
| server | 上面几乎所有包 |

由此得到的层次（自底向上）：`logx → subs → convert → repo → lifecycle → tasks → notifications → {rulefetch, auth, redact, substore} → {mosdns, oxidns, refresh} → provider → {mihomoenv, aggregations, routersplit, setupflow, gateway, ippresence} → agents → {trafficstore, monitor, upgrades, health, webdav} → {discovery, metrics, routers} → deploy → {contentsync, host, onboarding} → {rulesync, dnsfollow, dnsha, ipv6, installation, setup} → api/* → server`。下面的构造顺序严格沿这个方向，**每一步只接收已经构造好的实例或纯函数**。几处双向依赖用「小接口 + 后一步构造」或「两阶段 Set」处理（见 J1、J3）。

---

## 9. 建议的 server 构造顺序（`server.New(ctx, Options) (*App, error)`）

- **S0 日志**：`root := logx.Setup(...)`（保留单例，J7）。`auth.New(auth.FromEnv())` 生成鉴权 Gate（或者保留 auth 单例，J6）。
- **S1 仓库**：`r := repo.Open(dataDir, repo.Options{Export: configexport.Exporter{RouterRedactor: routers.RedactRouters}, RouterJobLock: routers.JobLockRecord})`，传入的都是纯函数，不依赖实例。PlanNodeDeletion 下沉到 repo，EasyTier 改成直接调用，InitAgentManager 删除。去掉 `SetRepository`。
- **S2 运行时**：`rt := lifecycle.NewRuntime(lifecycle.Options{Grace, Drain: drainFn})`，drainFn 引用 S13 才创建的 HTTP server，用闭包前向引用（现状就是这样）。
- **S3 任务和通知**：`tk := tasks.NewRegistry(r, rt)`；`nt := notifications.New(r, tk, notifications.Options{Summarize: routers.SummarizeForNotice})`，Agents 来源在 S8 之后接上（J3）。
- **S4 拨号器**：`subs` 需要的 `SharedDialers` 由 `convert.SharedDialers()` 提供，作为值传给 subs 的入口（替代 convert 的 init）。
- **S5 Sub-Store 和刷新**：`sub := substore.NewService(substore.NewEnv())`，`sub.Register(rt)`（idle 周期任务、prune 启动步骤）；`rf := refresh.New(r, sub, rt, tk)`，构造时注册任务行、运行器、周期任务。
- **S6 生成环境**：`pv := provider.New(r, sub, rf, provider.Options{Locations: deploy.Locations{}, Aggregations: aggregations.Renderer})`；`cenv := mihomoenv.NativeEnv(r, sub, rf, pv)` 加上 `Integrations: gateway.Hooks()`（只装一份 FullTun）；oxidns 的 ClassicalIPLines 直接用 ippresence。
- **S6b 规则源**：`rs := rulesource.New(r, rulefetch.Client)`，从 api/rules 抽出来，给 routers、gateway、setup、api/rules 共用。
- **S7 Agent 周边存储（不需要 Manager）**：`ts := trafficstore.New(dataDir)`；`mt := metrics.New(dataDir, rt, tk, ts)`；`hl := health.NewStores(dataDir)`；`wd := webdav.New(r, rt, tk, nt)`（构造时 `r.Subscribe(wd)`）。
- **S8 Manager**：`m := agents.New(ctx, r, agents.Options{Runtime: rt, Notifications: nt, Metrics: mt, Publication: monitorPublication, Receipts: deploy.ReceiptMatches, DeletedStores: [traffic(ts), probe(hl), region(hl)], HA: haProxy})`。然后接上 `nt.SetAgents(m)`（J3）。monitor 的 Publication 方法本来就以 `*Manager` 为参数，所以 `mon := monitor.New(dataDir, rt, rulefetch.Client)` 可以放在 S7。
- **S9 HA 与 DNS 跟随**：`ha := dnsha.New(m, rt)`（它只用 deploy、rulesync 的无状态函数）；`m.SetHA(ha)` 或者 haProxy 闭包指向 ha（J1）；`mon.SetHARules(ha)` 同理；`follow := dnsfollow.New(m, rt, tk)`。
- **S10 发布与同步**：`dp := deploy.New(m, deploy.Options{Pair: ha, Guards: host.Guards(r, m), IPv6: ipv6Proxy, Monitor: mon})`；`cs := contentsync.New(m, dp)`；`ip := agentipv6.New(m, cs, rt)`；`rsync := rulesync.New(m, rt, tk, rulesync.Options{Pair: ha, Refresh: rf})`（构造时订阅 `rf.OnRulesRefreshed`）；用 `agenttasks.Register(tk, m, rsync, follow)` 生成 SlotAgents 行；`rt2 := routers.NewSyncService(r, rt, tk, routers.Options{IPv6Host: ip, Rules: rs})`（归协调者）；`dc := discovery.New(r, m, ts, rt, tk)`；`sub.OnContentChange(cs.Invalidate)`。
- **S10b setup 流程**：`inst := installation.NewService(r, m)`；`st := setup.New(r, setup.Options{Rules: rs, Installation: inst, Gateway: host.Bridge{r, m}, VerifyRouter: ?})`；`onboarding.New(..., Adopter: st)`（J4）。
- **S11 API**：`app := httpx.New()`；26 个 `apiX.New(apiX.Deps{...}).Register(app.Router)`，顺序保持 Flask blueprint 的顺序；tasks 和 notifications 不再需要「late」，改成 `tk.Validate()` 在构造期检查槽位是否齐全；最后 `auth.Install(app)`。
- **S12 启动步骤（按顺序显式注册一次）**：load-config（`r.PrepareConfig(ctx, DefaultServerDomain)`）→ subscription-cache-prune → agent-load（Manager 的加载部分）→ metrics-load-history → webdav-worker-init。
- **S13 监听**：`weblistener.Manager` 建好后执行 `settingsHandler.SetWebListener(wl)`（J5）；`rt.RunStartup`、`Listen`、`rt.Start`。

测试对应：每个测试调用 `server.New(ctx, server.Options{DataDir: t.TempDir(), …fakes})` 或者只构造被测的那个服务；`repo.SetHooks`、`agents.ResetHooks`、`agents.ResetRuntime`、`*.SetDefault`、`notifications.ResetInputs`、`lifecycle.Configure` 等约 150 处「改全局再恢复」的写法随之消失，测试之间也就不再共享可变全局状态（符合 AGENTS.md 的要求）。

---

## 10. 需要裁定的点

- **J1 HA 的双向依赖**：Manager（G10–G12）、deploy（H39）、rulesync（H25/26）、monitor（H24）需要 dnsha，而 dnsha 需要 Manager。可选 (a)：两阶段，`m.SetHA(ha)` 等实例 setter 只允许在 `Start` 之前由 server 调用一次；(b)：保留「钩子函数带 *Manager 参数」的签名，作为 Options 里的闭包，闭包体内引用稍后赋值的 `ha` 变量（Go 的前向引用）；(c)：把 HA 判定下沉成 Manager 内部的一个子组件。推荐 (a)，更显式，也方便 vet。
- **J2 convert 的生成环境**：`convert.SetMihomoEnvironment` 有 23 个测试文件在用，而且 convert 的入口是大量包级函数。是把 `convert.Env` 显式往下传（改动面大，要改所有生成入口），还是保留一个「只在 server 构造期设置一次、之后只读」的包级环境（就是现状去掉 restore）？前者符合目标，后者成本低。建议单独排期。
- **J3 notifications 与 Manager、provider 与 aggregations 的先后顺序**：notifications 比 Manager 低一层，但需要 Agent 列表；provider 需要 aggregations 的渲染器，而 aggregations 依赖 provider。是统一用「构造后 Set 一次」（并加上 `started` 断言），还是拆接口？
- **J4 setup 和 onboarding 的方向**：两者互不 import，只能选一个方向直接 import。setup→onboarding（DNS 纯函数直接调用，AdoptOwnedWrite 注入 onboarding），还是 onboarding→setup？
- **J5 web listener 的晚绑定**：保留 `SetWebListener` 这一个实例 setter，还是先构造 weblistener 再把 Edge.API 延迟设置？
- **J6 auth 单例**：auth 有 29 个 import 方，`auth.Require` 作为中间件到处都在用。是改成 `*auth.Gate` 由 server 传给每个 Handler，还是保留为只读单例（`Configure` 只调用一次）？
- **J7 logx**：建议保留单例，Logger 只是名字句柄；如果连它也要改，影响面是全仓库。
- **J8 rulefetch.DefaultClient 和 HTTP 客户端**：要不要全部改成注入（monitor、mosdns、api/rules 等）？`mosdns.downloadRule` 在 init 时捕获了 DefaultClient，测试替换对它无效，这是一个小 bug，迁移时顺手修掉。
- **J9 纯 memo 缓存**（convert、ippresence、aggregations 的 memo、model/jsondecode 的反射缓存）：是否允许保留为包级 D 类（键里不含实例身份，内容是不可变的派生值）？
- **J10 `setup.ExternalOwners.VerifyRouter`**：生产里从来没有注册过，setup 走到 completion.go:286 时一律返回 unavailable。这是有意的（功能还没移植），还是漏接了？迁移时需要明确它的提供方。
- **J11 repo 的请求作用域 API**：`repo.ResolveProfileID/Config` 等（约 34 处调用）改成 `*Repository` 的方法后，ctx 里的 Scope 保留；`repo.WithRepository` 在生产里只用于「后台任务带上发起方的仓库」，仓库唯一以后要不要一起删掉（httpxtest 依赖它）？
- **J12 已有的重复注册**：FullTun 双装、metrics/substore 的 RegisterStartup 双注册、LoadConfig 和 InitAgentManager 死代码。迁移前先单独清理，作为一个小提交，还是并进大改动？
