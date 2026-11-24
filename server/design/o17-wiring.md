# O17 3.7 拆 `routers` 大包 + 1.5 显式依赖组装 — 设计

分支 `go-o17-wiring-plan`（= batch-48 分支 `15ae7cb3c`），2026-11-23。对应 `parity/o17-idiomatic-go.md` 1.5、3.7。
**本文只是方案，未改任何 `.go` 文件。** 实施在「运行状态收尾」任务（routers `last_release` → `agent_state`、agents 循环/启动连接）
合入 `go` 之后开始：先 rebase，再用附录 A 的脚本重算一遍依赖（数字会小幅变化，规则不变）。

数据来源（全部在 .64 上用 `rt` 只读跑出，脚本见附录 A）：
- routers 文件级依赖：用 `go/types` 对 104 个非测试文件做类型检查，记下「文件 A 用了文件 B 定义的哪个顶层符号/方法」，734 条边；
- 测试归属：同样方法跑上 104 个测试文件；
- 全局变量：对 `./internal/... ./cmd/...` 全部非测试包，列出含函数的包级变量（127 个）及写入/读取位置；
- 服务定位器与 `Set*/Reset*/Register*`：grep + 逐个读实现（清单见第 2 节，原始表格在附录 B）。

---

## 0. 结论先行

1. **routers 拆成 13 个包、8 层，严格无环**（第 1 节）。现在 104 个文件里有 71 个互相成环（一个强连通分量），原因只有四个：
   ① `RouterState`/`Desired`/`Router`/`Report` 四个聚合类型被所有功能当「黑板」读写，又反过来以字段形式引用各功能的类型；
   ② 各功能把方法直接挂在 `*RouterOS`/`*Ikuai` 驱动类型上，驱动结构体里还存着各功能的缓存；
   ③ 驱动的 `ReadState`/`Apply` 本身就是跨功能的组合；
   ④ 一批通用小工具（`uniqueStrings`、`mustPolicyJSON`、`Networks`…）随手放在某个功能文件里。
   消解办法都是机械的：聚合类型留在顶层编排包，功能函数只收自己用到的字段（已逐函数算出，见 1.4）；驱动拆成「RouterOS/iKuai 客户端」
   （底层）和「组合驱动」（顶层，`ReadState` 和按操作种类分发 `Apply`）；小工具下沉到 `routers/device`。
   拆完后 `RegisterRouterOSHandler` + 6 个 `init()`、`ReportParts` 的 `init` 链、`CaptureManual`、`IPv6HostRequest` 这 4 个钩子自然消失。
2. **显式组装（第 2 节）**：生产里仓库（`*repo.Repository`）和 Agent 管理器（`*agents.Manager`）在整个进程生命周期都是唯一的
   （`SetRepository` 只在启动调一次，导入/恢复/WebDAV 都是在同一个仓库上写），所以服务定位器可以整个换成构造参数，不需要
   「运行中替换仓库」的语义。`internal/server` 改成 `server.New(Deps)` 一次性构造全部服务对象，依赖经结构体字段/构造参数传下去；
   跨包回调改成**在使用方定义的小接口**，由 server 把实现方的对象塞进去。`wirePackages`、各包 `Wire()`/`Register()`、
   `repo.RegisterHooks`/`agents.RegisterHooks`、`Set*`/`Reset*` 测试钩子全部删除；每个测试自己 `New(...)` 一份实例。
3. **分步**（第 3 节）：R1–R10（拆 routers，10 步）+ W0–W11（组装，12 步），共 22 步，另有可选的 W12（convert 生成环境显式传参）。
   每步单独可编译、可测、可复核、能过 gate。R 线和 W0–W8 改的是不同的包，**可以两位 agent 并行**；W4 之后 W5/W6、W7、W8 再分给
   第三位 agent；W10（api 处理器）可三路并行。合计约 27–29 个 agent 工作日，2–3 位 agent 约 2–2.5 周。
4. **需裁定**（第 4 节，D1–D14）：最要紧的是 D1（Manager↔dnsha 双向依赖用两阶段 setter）、D2（convert 生成环境是否显式传参，
   23 个测试文件）、D9（`setup.VerifyRouter` 生产里从未注册，是漏接还是有意）、D14（api 包全部改 `Handler` 结构体放切换前），
   其余是包名、`ros` 导出面、临时别名、`auth`/`lifecycle` 的保留范围。

---

## 1. 拆 `routers`（3.7）

### 1.1 现状

- `internal/routers`：104 个非测试文件、33 980 行；104 个测试文件；另有子包 `routerfake`（不依赖 routers，保持不动）。
- 文件级依赖 734 条边；**71 个文件构成一个强连通分量**，另有两个两文件小环（`fixed_ip_read`↔`fixed_ip_ikuai_read`、`management`↔`workspace`）。
- 外部导入方（非测试）：`api/routers`（48 个符号）、`clients`（22）、`agents/ipv6`（19）、`setup`（8）、`api/rules`（3）、`server`（6）、
  `api/clientdevices`（3）。测试另有 `notifications`、`routersplit`、`api/config`、两个 difftest 包。
- routers 自己的全局钩子：`RegisterRouterOSHandler`（13 种操作由 6 个 `init()` 注册）、`ReportParts`（`dns_guard_plan.go` 的 `init`
  串接）、`CaptureManual`（`rule_sync_tasks.go` 在 `Wire` 里填）、`IPv6HostRequest`（server 填，因为 `agents/ipv6` import routers）、
  `RulesetContent`（`api/rules` 的 `Wire` 填）、`NewRouterID`（只给测试换）、`ruleSyncLoop`（包级循环节拍器）。

### 1.2 目标包（13 个，按层）

依赖只能指向更低的层；同层之间不互相 import。层号就是允许的 import 方向（附录 A 的 `ALLOWED` 表逐包列出）。行数是现有文件行数之和（不含测试），搬完会因导出改名略有变化。

| 包 | 层 | 行 | 负责 | 不负责 | 文件 |
|---|---|---|---|---|---|
| `routers/device` | L0 | ≈1 200 | 厂商无关的词汇：错误（`RouterError`、`ErrorKind`、`ValidationError`）、设备读到的条目（`RouteEntry`、`PolicyRouteEntry`、`NatBypassEntry`、`DhcpDns*`、`RouterInfo`）、托管注释规则、发布操作头（`Op`/`OpHead`/`CIDRGateway`/`NATChecks`）、静态路由索引、DHCP DNS 检查、`DeviceHTTP` 接口、通用小工具、跨包共用的中文消息（11 条）；**新增 `device.State`**（设备事实，见 1.4） | 任何 I/O、任何厂商细节 | `model`（去掉 `RouterState`、`Driver`）、`plan_model`、`staticindex`、`dhcpdns`、`messages`（只留共用的 11 条） |
| `routers/ros` | L1 | ≈5 200 | RouterOS REST 客户端 `ros.Client`：请求/列表/事务/批量/恢复/读回校验、`Row` 与行工具、路径常量、脚本值、NAT 上下文与评估（`NatContext`、`AssessNAT`…）、地址列表读写原语、设备回显脱敏、**归属清单 `ros.Ownership`**（原来散在驱动里的 5 份 manifest） | 任何功能的计划/写入逻辑；`ReadState`；按功能分发的 `Apply` | `routeros`（去掉 `ReadState`）、`routeros_batch`、`rosnat`、`rosvalues`、`ros_script`、`natlist`（规则识别）、`addresslists`、`addresswrite`、`device_redact` |
| `routers/ikuai` | L1 | ≈3 900 | iKuai 客户端 `ikuai.Client`：登录、表单、批量写、DHCP、IP 分组（`GroupState`） | `ReadState` 组合；路由编辑器 | `ikuai`（去掉 `ReadState`）、`ikuai_apply`、`ikuai_dhcp`、`ikuai_forms`、`ikuai_row`、`ikuaigroups` |
| `routers/spec` | L1 | ≈1 000 | 路由器记录与期望状态：`Router`/`RouterSettings`/`DNSGuard`/`PDGuard` 设置、存储编解码（`stored_model`）、`Desired`/`ComputeDesired`/`ContentLoader`、`RuleSet`/`LibraryRule`；搬进来 `DesiredDNSGuard`、`ROSPolicyDesired` 两个期望类型和 `routersOf`/`releaseRouterRecord`/`pendingProfile` 三个记录读取函数 | 读设备、计划 | `router`、`stored_model`、`desired` |
| `routers/oplock` | L2 | ≈150 | 发布 L1 锁 `RouterJobLock`、每连接 L2 锁 `RouterOperationLock(s)`（从 `route_editor.go` 搬来 `routeLocks` 与 `RouterOperationLock`） | — | `joblock`、`operation_group` |
| `routers/routeedit` | L3 | ≈1 250 | 手动静态路由编辑（两种厂商）：`ReadRoutes`、`BuildRoutePlan`、`PublicRoute`… | 锁（在 oplock） | `route_editor`（去掉锁）、`routeros_routes`、`ikuai_routes` |
| `routers/dnsguard` | L2 | ≈1 200 | RouterOS DNS 守卫：期望规格、计划、写入/恢复、报告段 | — | `dns_guard_*`（5 个） |
| `routers/ipv6` | L5 | ≈5 800 | RouterOS IPv6：fake-ip/路由清单、DNS 下发、PD 守卫、策略评估、就绪检查、转发提示、报告段 | — | `ipv6_*`（16 个）、`forward_hint`、`forward_hint_messages` |
| `routers/fixedip` | L2 | ≈1 350 | 固定 IP（DHCP 静态绑定）读写，两种厂商 | — | `fixed_ip_*`（6 个） |
| `routers/clientlist` | L2 | ≈680 | 读路由器上的在线客户端/连接（两种厂商） | — | `clients_*`、`client_messages`、`connection_reader` |
| `routers/rospolicy` | L4 | ≈3 800 | RouterOS 策略路由 + NAT 地址列表（两者互相调用：NAT 列表写入要先快照/恢复策略路由，策略计划要用 NAT 列表的资源与重叠判断，**必须同包**） | — | `rospolicy_*`（5 个）、`natlist_plan`、`natlist_read`、`natlist_write`；可 import `routeedit`（`editROSEntry`） |
| `routers`（原包） | L6 | ≈6 100 | 编排：`RouterState`、`Driver` 接口与**组合驱动**（`NewDriver`、两种厂商的 `ReadState`、按种类分发 `Apply`）、`Report`/`BuildReport`、计划、发布/回滚、检查（inspect）、待推送缓存与摘要、管理视图、工作区、回程路由提示、Agent 回程、repo 钩子实现 | 规则同步 | `plan*`、`release*`、`publish`、`report`、`inspect`、`pending*`、`management*`、`workspace`、`repository_hooks`、`wire`（变成构造函数）、`agent_return`、`return_advisory`；新增 `driver_routeros.go`、`driver_ikuai.go` |
| `routers/rulesync` | L7 | ≈2 550 | 路由器规则自动同步（循环、存储、任务行）；**在编排包之上**，编排包需要它的地方（发布成功后「捕获手工基线」）改成构造参数 | — | `rule_sync_*`（10 个） |

依赖图（每个包只 import 列出的包；附录 A 脚本按此检查，实施后用 `go list -deps` 复核）：

```
L7 rulesync   → routers, ipv6, rospolicy, routeedit, oplock, ros, spec, device
L6 routers    → ipv6, rospolicy, routeedit, dnsguard, fixedip, clientlist, oplock, ros, ikuai, spec, device
L5 ipv6       → rospolicy, dnsguard, ros, spec, device          （ForwardHint 要读策略与 DNS 守卫的快照/计划）
L4 rospolicy  → routeedit, ros, spec, device                    （只用 routeedit 的 editROSEntry）
L3 routeedit  → oplock, ros, ikuai, spec, device
L2 oplock     → spec, device
L2 dnsguard   → ros, spec, device
L2 fixedip    → ros, ikuai, spec, device
L2 clientlist → ros, ikuai, spec, device
L1 ros / ikuai / spec → device（三者互不 import）
L0 device     → 只有标准库与 configflow 的叶子包（jsondecode、netipx、redact、devhttp…）
```

外部包的依赖方向（拆完后实测要满足）：`agents/ipv6` 只 import `routers/ipv6`、`ros`、`spec`、`oplock`、`clientlist`、`device`，
**不再 import `routers`（编排包）**，所以编排包可以反过来依赖 IPv6 主机查询能力——`IPv6HostRequest` 钩子改成编排服务的构造参数（2.3）。

### 1.3 每个反向依赖怎么消（R 步骤的执行清单）

下表是按上面的分配算出的**全部**跨包反向引用（附录 A 脚本可复算），以及处理方式。「下沉」= 把定义搬到更低的包并导出；
「上移」= 搬到编排包；「收窄」= 改函数签名，只收用到的字段。

| 引用方 → 被引用 | 符号 | 处理 |
|---|---|---|
| 所有功能 → 散落的小工具 | `uniqueStrings` `firstNonEmpty` `nonEmpty` `stringSet` `jsonEqual` `jsonValuesEqual` `mustPolicyJSON` `orText` `requestString` `hasStoredState` `isTrue` `clientIPv4` `invalid` `routerError` `rosError` `rollbackError` `isRouterError` `routerErrorKind` `Networks` `SupernetIndex`/`NewSupernetIndex` `ReportPart` `DeviceHTTP` | 下沉到 `device`。名字按 Go 习惯：`device.Unique`、`device.FirstNonEmpty`、`device.JSON[T]`（原 `mustPolicyJSON`）、`device.Errorf(kind, …)` 等由实施者定，`gofmt -r` 批量改调用点。`ikuai.go` 的 `nonEmptyText` 与 `device.FirstNonEmpty` 不合并（语义不同） |
| `ros` → ipv6 / natlist / rospolicy / dnsguard | `rosIPv6ND` `pdLists` `rosTables` `ipv6DNSFlag` `StableIPv6Row` `policyFlag` `messageIPv6RoutesOwnership` `ROSResource`/`resource` `guardRaw` | 下沉到 `ros`：路径常量、行判定都属于 RouterOS 客户端 |
| `ros` → 编排 | `(*RouterOS).ReadState`、`(*RouterOS).stateNATLists`、`RouterState` | `ReadState` 上移到 `routers/driver_routeros.go`（约 90 行，调用 `ros.Client` 的公开原语 + `rospolicy.ReadNATLists` + `dnsguard.OwnedHairpinIDs`） |
| `ros` 驱动结构体里的功能缓存 | `policyManifest` `dnsGuardManifest` `ipv6FakeIPManifest` `ipv6RoutesManifest` `ipv6PDGuardManifest`（+ `…Set` 标志） | 这 5 份是**跨功能共用的「本设备上哪些对象归 ConfigFlow 所有」**（`rospolicy_model` 读全部 5 份，`addresslists` 读策略那份），不是某个功能私有的：收成 `ros.Ownership{Policy, DNSGuard, IPv6FakeIP, IPv6Routes, IPv6PDGuard json.RawMessage}`，`c.Ownership()` 读写。`natListTouched` 改成 `c.MarkNATListTouched()`/`NATListTouched()`；`forwardFilterRows`、地址缓存三件留在 `ros.Client`（地址读原语自己用） |
| `ros` → `routeedit` | `ikuaiRow`（`route_editor.go` 用） | `route_editor` 去 `routeedit`，那里可以 import `ikuai`（`ikuai.Row` 导出） |
| `ikuai` → `ros` | `DeviceHTTP` `isRouterError` `routerErrorKind` `uniqueStrings` | 已在第一行下沉到 `device` |
| `ikuai` → 编排 | `RouterState`、`PolicyRoute`、`(*Ikuai).ReadState` | `ReadState` 上移到 `routers/driver_ikuai.go`；`GroupReportSection(state)` 收窄为 `(groups *GroupState, facts *device.State)`；`VerifyGroups(check, state)` 收窄为 `(check, groups *GroupState)`；`PolicyRoute(state, comment)` 改为 `device.PolicyRoute(routes []*PolicyRouteEntry, comment)` |
| 各功能 → `RouterState` | 见 1.4 字段表 | 收窄 |
| 各功能 → `Desired`/`Router` | `Desired`、`ProxyCIDR`、`ComputeDesired`、`ContentLoader`、`Router`、`RouterSettings`、`PDGuard`、`DNSGuard`、`(*Router).State`、`(*Router).ValidateForDevice` | 这些在 `spec`，L2/L3 本来就能 import，不用改 |
| `natlist_*`/`forward_hint` → `Report` | `Report`、`NatItem` | 只用到 `report.Routes`（`StillRoutedNAT`、`ForwardHint`）和 `report.NatBypass`（`PlanNATLists`）：收窄为传 `RoutesReport`/`NatReport`，两个类型和 `NatItem` 一起放 `device`（它们是纯数据） |
| `dnsguard`、`ipv6` → 编排 | `ReportParts`（`init` 里串接的函数变量） | 删除变量和 `init`。`BuildReport` 直接按固定顺序调用 `dnsguard.ReportPart(desired, guard)`、`ipv6.ReportParts(plans)`、`rospolicy.ReportPart(...)`（顺序照现在 `init` 串接的结果，测试比对 JSON） |
| `rospolicy` → 编排 | `(*RouterOS).ReadState`（写后复读） | 改成参数 `readNAT func(context.Context) (*ros.NatContext, error)`，组合驱动传入 |
| `fixedip`、`routeedit` → 编排 | `Driver` | 在使用方定义小接口（`fixedip.Device`：`ReadFixedIP`/`ApplyFixedIP`…；`routeedit.Device`：读/写静态路由），组合驱动实现 |
| `clientlist` → 编排 | `routersOf`、`releaseRouterRecord` | 两个函数只读路由器记录，下沉到 `spec` |
| `oplock` → `ros`/`routeedit` | `rosError`、`routeLocks` | `rosError` 已下沉；`routeLocks` 随 `RouterOperationLock` 搬到 `oplock` |
| `spec` → ipv6 | `messageIPv6YAMLType` | 这条消息搬到 `spec/messages.go` |
| 编排 → `rulesync` | `wireRuleSync`（填 `CaptureManual`） | `CaptureManual` 变成编排服务的字段 `ManualBaseline func(ctx, …) error`（或小接口 `BaselineCapturer`），server 用 `rulesync.Service` 填（2.3） |
| 编排 `management`/`workspace` → `rulesync` | `findSyncRouter` | 这个函数只按 id 找路由器记录，下沉到 `spec` |

**方法改函数**：功能文件里挂在 `*RouterOS` 上的 72 个方法、挂在 `*Ikuai` 上的 22 个方法、挂在 `*RouterState` 上的 1 个
（`natPolicyEvidence`）不能留在别的包。为了让方法体几乎不动，每个功能包定义一个私有包装：

```go
// package rospolicy
type device struct{ *ros.Client }          // 方法体里的 d.List(...)、d.Transaction(...) 不变

// Apply 是对外入口：组合驱动在 Apply 分发时调用。
func Apply(ctx context.Context, c *ros.Client, op *Replace) (*Undo, error) { return (&device{c}).apply(ctx, op) }
```

`ros.Client` 的原语因此要导出（`list`→`List`、`transaction`→`Transaction`、`ownershipSnapshot`→`OwnershipSnapshot`…，约 100 个原来未导出的名字，
见 4.1 裁定点 D2）。`Row` 的小写方法（`text`、`id`、`is`…）导出为 `Text`、`ID`、`Is`。

**消息常量**：`messages.go` 445 行、373 条在用的常量里 362 条只被一个目标包使用（rospolicy 110、routeedit 87、ikuai 84、ros 75、spec 6），
随用户包走，各包一个 `messages.go`（AGENTS.md 规则）；跨包共用的 11 条放 `device/messages.go`。测试引用同一常量（改包名前缀即可）。

### 1.4 `RouterState` 拆分与函数收窄

`RouterState` 现在有 20 个字段：一半是驱动读到的**设备事实**，一半是计划阶段临时挂上去的**功能计划**（`json:"-"`）。新形状：

```go
// package device —— 驱动读到的、厂商无关的事实
type State struct {
    Routes              []*RouteEntry
    NatBypass           []*NatBypassEntry
    AddressSets         map[string][]string
    DhcpDNS             *DhcpDnsState
    PolicyRoutes        []*PolicyRouteEntry
    RetiredPolicyRoutes []*PolicyRouteEntry
    ActiveRoutes        []*RouteEntry
    PolicyNATRetiring   []string            // json:"-"
}

// package routers —— 编排用的完整视图；嵌入保证 state.Routes 等写法和 JSON 输出都不变
type RouterState struct {
    DNSGuard       *dnsguard.Snapshot   `json:"dns_guard,omitempty"`   // 放在嵌入字段前：JSON 成员顺序与现在一致
    ROSPolicy      *rospolicy.Snapshot  `json:"ros_policy,omitempty"`
    device.State                                                       // routes … active_routes
    NatContext     *ros.NatContext      `json:"nat_context"`
    IkuaiGroups    *ikuai.GroupState    `json:"-"`
    NATLists       *rospolicy.NATLists  `json:"-"`
    NATListsReader func(context.Context) (*rospolicy.NATLists, error) `json:"-"`
    // 计划阶段的产物（原 json:"-" 字段），只有编排包读写
    DNSGuardPlan   *dnsguard.Plan
    ROSPolicyPlan  *rospolicy.Plan
    IPv6FakeIPPlan *ipv6.FakeIPPlan; IPv6RoutesPlan *ipv6.RoutesPlan; IPv6DNSPlan *ipv6.DNSPlan; IPv6PDGuardPlan *ipv6.PDGuardPlan
}
```

JSON：`encoding/json` 把嵌入结构体的字段摊平在嵌入的位置，所以上面的声明顺序输出的成员顺序与现在相同
（`dns_guard`、`ros_policy`、`routes` … `active_routes`、`nat_context`）。`PolicyNATRetiring` 本来就是 `json:"-"`，挪进 `device.State` 不影响输出。
验收：带 state 的接口（检查、待推送、发布回执）JSON 逐字节比对 + routerdiff 抽样（3.3）。

功能函数收窄（类型检查得出的实际字段使用；左边是现签名里的 `*RouterState`）：

| 函数 | 实际用到 | 新参数 |
|---|---|---|
| `ros.AssessNAT` | `NatContext` | `nat *NatContext` |
| `ros.AssessBypass` | `AddressSets` `NatContext` `PolicyNATRetiring` | `facts *device.State, nat *NatContext` |
| `ros.PlannedBypassConflicts` | `AddressSets` `NatContext` | 同上 |
| `ikuai.GroupReportSection` | `IkuaiGroups` `NatBypass` `PolicyRoutes` | `groups *GroupState, facts *device.State` |
| `ikuai.VerifyGroups` | `IkuaiGroups` | `groups *GroupState` |
| `dnsguard.BuildDNSGuard` | `DNSGuard` | `snap *Snapshot` |
| `dnsguard.guardDesiredSpec` | `NatContext` | `nat *ros.NatContext` |
| `rospolicy.BuildROSPolicy` | `NatContext` `ROSPolicy` | `nat, snap` |
| `rospolicy.natPolicyEvidence`（方法） | `ROSPolicyPlan` | 函数 `NATPolicyEvidence(plan *Plan)` |
| `rospolicy.policyReportPart` | `PolicyNATRetiring` `ROSPolicyPlan` | `retiring []string, plan *Plan` |
| `rospolicy.VerifyNAT` / `VerifyROSPolicy` / `verifyReplacementPolicyNAT` / `applyPolicy` | `NatBypass`/`AddressSets`/`NatContext`/`Routes` | `facts *device.State, nat *ros.NatContext` |
| `rospolicy.StillRoutedNAT` | `NatContext` `PolicyRoutes` `RetiredPolicyRoutes` | `routes device.RoutesReport, facts, nat` |
| `rospolicy.ListsOfNAT` | `NATLists` `NATListsReader` | `lists *NATLists, read func(ctx)(*NATLists, error)` |
| `rospolicy.PlanNATLists` | `NATLists` `NatBypass` `NatContext` | `nat device.NatReport, facts, natCtx, lists` |
| `rospolicy.VerifyNATLists` | `NatContext` | `nat` |
| `ipv6.ForwardHint` | `DNSGuard` `NatContext` `ROSPolicy` `ROSPolicyPlan` | 收成小结构 `ipv6.ForwardInputs{Guard *dnsguard.Snapshot; NAT *ros.NatContext; Policy *rospolicy.Snapshot; PolicyPlan *rospolicy.Plan}`（ipv6 在 L5，可以 import 两者） |
| `ipv6.ipv6ReportParts` | 4 个 IPv6 计划 | `ipv6.ReportParts(fake, routes, dns, pd)` |

### 1.5 组合驱动（编排包里新增的两个文件）

- `Driver` 接口保留在 `routers`（`Vendor/SupportsWrite/Probe/ReadState/ReadDHCPDNS/Apply/RenderScript/Close`），`BatchDriver` 同。
- `type rosDriver struct{ c *ros.Client }`：`Probe`/`ReadDHCPDNS`/`RenderScript`/`Close`/`ApplyMany`/`RestoreMany`/`BatchKinds` 直接转给 `c`；
  `ReadState` 是从 `routeros.go` 搬来的 90 行；`Apply` 先解出 `kind`，13 种功能操作用 `switch` 直接调 `rospolicy.Apply…`/`dnsguard.Apply…`/
  `ipv6.Apply…`，其余交给 `c.Apply`（基础的路由/NAT/DHCP 操作和 `ros_restore`）。**删除** `RegisterRouterOSHandler`、`rosHandlerState`、
  `routerOSHandler` 和 6 个 `init()`；`ErrNotPorted` 分支随之消失（编译期保证每种操作都有实现）。
- `type ikuaiDriver struct{ c *ikuai.Client }` 同理（iKuai 没有功能分发，`Apply` 直接转发）。
- `NewDriver(r *spec.Router) Driver` 留在编排包；`clients` 的固定 IP、`agents/ipv6` 只需要 `fixedip.Device`/`ros.Client`，不再经 `NewDriver`。

### 1.6 外部导入方要改的地方

| 包 | 现在用 | 改成 |
|---|---|---|
| `api/routers` | `core.*` 48 个 | `device`（`KindConflict` `RouterError` `ValidationError`）、`oplock`（`RouterJobLock`）、`routeedit`（`PublicRoute` `ReadRoutes` `RoutePlan` `RouteResult`）、`spec`（`Router` `RouterView`）、`rulesync`（`NeedsBaseline` `PausedMessage` `Service` `Store`）、其余 33 个留 `routers`。处理函数拿服务对象而不是包级函数（2.4） |
| `clients` | 22 个 | `clientlist`、`device`、`fixedip`、`oplock`、`rospolicy`（`ROSPolicyDigest`）、`spec`；`NewDriver` 只为固定 IP → 改用 `fixedip.NewDevice(router)` |
| `agents/ipv6` | 19 个 | `ipv6`、`clientlist`、`device`、`oplock`、`ros`、`spec`（**不再 import 编排包**） |
| `setup` | 8 个 | 全部在 `spec` |
| `api/rules` | `RulesetContent` `RuleSet` `LibraryRule` | 类型在 `spec`；`RulesetContent` 赋值删除（2.3 W-rules） |
| `server` | `IPv6HostRequest` `GatewayAgent` `IPv6Host` `Wire` `RuleSet` `LibraryRule` | 见第 2 节 |
| `api/clientdevices` | `RouterError` `KindConflict` | `device` |

### 1.7 测试怎么跟着走

- 104 个测试文件按「用到的最高层包」归属（实测）：rulesync 9、编排 38、rospolicy 12、ipv6 7、routeedit 5、ros 5、spec 5、fixedip 3、
  dnsguard 2、ikuai 1、clientlist 1；13 个 `routers_test` 外部测试包文件留在编排包。
- 58 个测试文件引用了**别的目标包里现在未导出**的 120 个符号（最多的是 RouterOS 路径常量、`Row` 方法、`mustPolicyJSON`、`guardRaw`、
  PD 相关路径）。其中约 90 个在 R 步骤里本来就要导出（路径常量、`Row` 方法、小工具）；剩下约 30 个（如 `pdClients`、`ipv6Owned`）
  只给测试用的，不为测试导出：**把那条测试搬到符号所在的包**，或者经 `export_test.go` 暴露。不新增非测试的测试钩子。
- `routerfake` 不动（它不 import routers）。`testdata/gen_r01.py` 跟 R01 语料测试走。
- `server/o10_r11_gate_test.go` 的源码门禁按包路径扫描「整记录编码」调用：搬文件后核对它的扫描范围仍覆盖新包（R 每步都跑 `./internal/server`）。
- pyref：`tools/pyref/sources.json` 的作用域目前不含 `internal/routers`（含 `routersplit`），R 线预计不需要重录；W 线改到作用域内的包
  （mosdns、oxidns、provider、deploy、discovery、routersplit）时，**必须同步 `sources.json` 并只重录 metadata**（batch-45/46 的教训，
  见 `70f60f2ed`）。

---

## 2. 显式组装（1.5）

逐项清单（35 个 wire 条目、68 个 `Set*/Reset*/Register*/On*`、127 个含函数的包级变量、12 个 `init()`、服务定位器调用量）在
**附录 B `o17-wiring/inventory.md`**，每行写了：谁填、谁用、为什么当初用钩子（逐个用 import 闭包核实有没有环）、改成什么、在哪个构造步骤提供、测试影响。
本节只写规则和结论。

### 2.1 事实（决定方案的前提）

- **生产里每个服务对象都只有一份。** `repo.SetRepository` 只在 `cmd/configflow/service.go` 启动时调一次；导入、恢复、WebDAV 都在同一个
  `*Repository` 上提交事务，没有运行中换仓库的路径。`agents.Init` 只在启动步骤 `agent-manager-init` 跑一次，「仓库变了就重建 Manager」
  只有测试会走到（每个测试给 ctx 绑不同仓库）。所以 `repo.Repository`、`agents.Manager`、`substore.Service`、`webdav.Service`、
  `lifecycle.Runtime`、规则同步/DNS 联动/HA/IPv6 服务都可以由 server 构造一次，按 Manager 或数据目录建的 map（dnsfollow、dnsha、
  contentChecks、metrics、monitor、traffic）退化成单个字段。
- **定位器调用量**：`repo.GetRepository` 158 处/46 包；`agents.Get` 56 处/13 包（api/agents 36）；`substore.Default` 10 处/7 包；
  隐式取默认仓库的 `repo.ResolveProfileID` 等约 34 处；另有 `trafficstore.GetNative`、`health.GetProbeStore`、`notifications.TaskStatus`、
  `rulesync.Get`、`metrics.For`、`dnsfollow/dnsha.ForManager` 等按 ctx/目录取实例的小定位器。
- **钩子分四种**（附录 B 0 节）：
  1. **其实没有环，可以直接 import/直接调用**（当初是照 Python 晚绑定或注释过时）：`RedactEasytierJSON`（repo 早已 import convert）、
     `notifications` 的 `Rows/MissingTasks`（notifications 早已 import tasks）、`agents.Hooks` 里 agents 自己从不读的 5 个字段
     （`PinnedWithheld`、`Address*`、`Installation*`，读的是 api 包）、`PublicationCheckpoint` 在 deploy 侧的读取、`deploy.SetGatewayGuards`、
     `routersplit.SetSessionReader`、`oxidns.SetNativeClassicalIPLines`、`wireLoopWakes`、setup.go 的三个 `RegisterExternalOwners`；
  2. **自己装自己**（同包，无需钩子）：`metrics.trafficFlush`、`monitor.historyFactory`、`api/agents.contentPrepare`、`discovery.collaboration`、
     routers 的 `CaptureManual`/`ReportParts`/`RegisterRouterOSHandler`；
  3. **死代码或重复**：`repo.Hooks.InitAgentManager`（`LoadConfig` 没有生产调用方）、`FullTun` 被 ippresence 和 gateway 各装一份（后者覆盖）、
     metrics/substore 的 `RegisterStartup` 各注册两次（靠按名字去重才没事）；`setup.VerifyRouter` 生产里从未注册（裁定 D9）；
     `mosdns.downloadRule` 在 init 时捕获 `rulefetch.DefaultClient`，测试替换对它无效（小 bug）；
  4. **真环，必须注入**：`agents` ← metrics/monitor/dnsha/deploy/installation；`repo` ← configexport/routers/webdav/server；
     `convert` ← mihomoenv/gateway/subs；`provider` ← deploy/aggregations；`tasks`/`notifications` 的行与运行器；
     dnsha ↔ Manager/deploy/rulesync/monitor 双向。
- 测试接缝（C 类）74 个，绝大多数是 `now`/`clock`/`randomHex`/`replaceFile` 一类，测试里约 150 处「改全局 → defer 恢复」。

### 2.2 规则（W 线每一步都按这几条做）

1. **构造参数优先**：每个有状态的包导出 `type Service struct{…}` + `func New(…) (*Service, error)`（或已有的类型名，如 `agents.Manager`），
   依赖是必需参数；可选依赖放 `Options` 结构体，**nil 的含义必须和现在钩子未装时一样**（例如 HA 未接时成对发布 fail closed 返回
   `ErrHAOwnerMissing`，安装注册未接时 501，`ReceiptMatches` 未接时 false）。不能因为改成构造参数而把 fail closed 变成 panic 或 fail open。
2. **小接口定义在使用方**：`agents` 需要 HA 的三个动作，就在 `agents` 里定义 `type PairPublisher interface{ PairDeployment(…); ActivatePair(…); ServiceOperation(…) }`，
   由 `*dnsha.Service` 实现；不在提供方定义「给别人用的接口」。
3. **真双向依赖用两阶段，且只在 server 里**：`m.SetPairPublisher(ha)` 这类 setter 只允许在 `Start` 之前调用一次（第二次或启动后调用返回错误并
   记日志，测试断言）；不是全局，是实例字段。用于：Manager↔dnsha、monitor↔dnsha、notifications↔Manager（Agent 列表）、provider↔aggregations、
   settings↔weblistener（裁定 D5、D6）。
4. **能直接 import 就直接 import**：上面第 1、2 种钩子直接删掉，不换成接口。
5. **后台循环/启动/关停由 server 注册**：服务提供 `Loop() lifecycle.Periodic`、`Startup() lifecycle.Startup`、`Close(ctx)`，server 依次
   `rt.RegisterPeriodic(svc.Loop())`；包里不再自己调全局 `lifecycle.RegisterPeriodic`。启动步骤顺序在 server 里写死一处（现在分散在 cmd 与各 Wire，
   靠「按名字原地替换」维持顺序）。
6. **任务页与通知用实例注册表**：`tasks.Registry`、`notifications.Service`；行构建器/运行器/间隔编辑器由服务构造时注册到实例上。
   「late route」不再需要，改成构造完成后 `registry.Validate()` 检查槽位齐全。
7. **请求作用域留在 ctx**：`repo.WithScope`（请求内快照缓存）、`lifecycle.Detach`/`Stopping(ctx)`/`SpawnerFrom(ctx)`（请求/任务生命周期）是
   正当的 context 用法，保留；被删除的是「从 ctx 或全局里**找服务对象**」。`repo.ResolveProfileID(ctx, …)` 等改成 `(*Repository)` 的方法。
8. **测试接缝**：`now`/`clock` 改成服务字段（`internal/clock` 已有接口），随机源、文件替换同理；只给测试用的 `Set*ForTest`/`Reset*` 删除，
   测试自己 `New` 一份带 fake 的实例。确实只能在包级的（纯 memo 缓存、反射缓存、编译期常量函数，附录 B 的 D 类 34 项）保留（裁定 D10）。
9. **`logx` 保留单例**（Logger 只是名字句柄），`auth` 的配置保留为只读单例或改成 `*auth.Gate` 由 server 传入（裁定 D7）。

### 2.3 每个 wire 条目的去向（摘要；明细见附录 B 第 1 节 W01–W35）

| 现在 | 改成 | W 步 |
|---|---|---|
| `wireCore`：notifications 输入、`DefaultServerDomain`、`RedactEasytierJSON` | 直接调用 `tasks`；`repo.Options{DefaultServerDomain}`；repo 直接调 convert | W0、W2 |
| `deploy.Wire`、`aggregations.Wire` | `provider.New(Options{Locations, Aggregations})`（aggregations 两阶段，D6） | W7 |
| `configexport.Wire`、`apinodes.Wire`、`routers.Wire` 的 repo 部分、`webdav.Wire` 的提交回调 | `repo.Options{Export: configexport.Exporter{…}, RouterJobLock: …}`；`planDeletion` 下沉到 repo；WebDAV 用 `r.OnCommit`（运行状态任务已加） | W2 |
| `agents.Wire`、`rulesync.Wire`、`dnsfollow.Register`、`dnsha.Register`、`metrics.Wire/WireTraffic`、`monitor.Wire`、`upgrades.Wire`、`health.Wire`、`installation.Wire`、`wireIPv6` 的 agents 部分 | `agents.New(ctx, r, agents.Options{Runtime, Notifications, Metrics, Publication, Receipts, DeletedStores, …})`；HA 两阶段 `SetPairPublisher`；任务行由 `agenttasks` 组装；`upgrades`/`installation`/`PinnedWithheld` 改为 api 处理器直接调用；各服务 `New` + server 注册循环 | W5、W6 |
| `substore.Wire`、`refresh.Wire`、`wireLoopWakes`、`mihomoenv.Wire`、`gateway.Wire`、`ippresence.Wire` | `substore.NewService`、`refresh.New`（`OnRulesRefreshed` 实例事件，`rulesync.New` 订阅）；convert 环境见 D2；`FullTun` 只留 gateway 一份；oxidns 直接 import ippresence | W0、W7 |
| `wireSetupContent`、`apirules.Wire`（`routers.RulesetContent`、`gateway.SetRuleSourceReader`） | 规则内容读取从 `api/rules` 抽到新的领域包 `internal/rulesource`，`setup`/`gateway`/`routers`/`api/rules` 共用（spec 的 `ContentLoader` 参数由它提供） | W8 |
| `wireSetupOnboarding`、`wireSetupInstallation`、`setupflow.Wire`、`wireGatewayGuards` | setup 直接 import onboarding（D4）；`setup.New(r, Options{Installation, Gateway})`；routersplit 直接调 setupflow；deploy 直接用 `host.Bridge` | W0、W8 |
| routers：`IPv6HostRequest`、`CaptureManual`、`notifications.SetInputs{Summarize}`、tasks 行/运行器、`router-rule-sync` 循环、`NewRouterID` | `routers.New(r, Options{IPv6Hosts: ip /* *agentipv6.Service */, Rules: rs, ManualBaseline: rsync})`；`rulesync.New(...)`；`NewRouterID` → 服务字段 | W9 |
| `apisettings.SetWebListener` | `settings.Handler` 的两阶段字段（D5） | W10 |
| `routePackages`/`lateRoutePackages` | 每个 api 包 `New(Deps) *Handler` + `h.Register(r)`；顺序保持 | W10 |
| cmd 的 `registerStartup` | server 里一处按序注册 | W1 |

### 2.4 server 的新构造顺序

```
cmd/configflow.run
  instance.Acquire → logx.Setup → auth.Configure（D7）
  r  := repo.Open(dataDir, repo.Options{Export, RouterJobLock, DefaultServerDomain})        S1
  rt := lifecycle.NewRuntime(...)                                                             S2
  app, err := server.New(ctx, server.Options{Repo: r, Runtime: rt, Clock: clock.Real{}})
server.New
  S3  tk := tasks.NewRegistry(r, rt); nt := notifications.New(r, tk, …)
  S4  subs 的共享拨号器作为值传入（替代 convert/subscription_dialers.go 的 init）
  S5  sub := substore.NewService(env); rf := refresh.New(r, sub, rt, tk)
  S6  pv := provider.New(r, sub, rf, …); agg := aggregations.New(pv, …); pv.SetAggregations(agg)   （D6）
      cenv := mihomoenv.New(r, sub, rf, pv) + gateway 集成                                    （D2）
  S6b rs := rulesource.New(r, rulefetch client)
  S7  ts := trafficstore.New(dataDir); mon := monitor.New(dataDir, rt, …); mt := metrics.New(dataDir, rt, tk, ts)
      hl := health.NewStores(dataDir); wd := webdav.New(r, rt, tk, nt)
  S8  m := agents.New(ctx, r, agents.Options{Runtime: rt, Notifications: nt, Metrics: mt, Publication: mon,
                       Receipts: deploy.ReceiptMatches, DeletedStores: […]}); nt.SetAgents(m)
  S9  ha := dnsha.New(m, rt); m.SetPairPublisher(ha); mon.SetHARules(ha); follow := dnsfollow.New(m, rt, tk)
  S10 dp := deploy.New(m, deploy.Options{Pair: ha, Guards: host.Bridge{…}, Monitor: mon})
      cs := contentsync.New(m, dp); sub.OnContentChange(cs.Invalidate)
      ip := agentipv6.New(m, cs, rt); rsync := rulesync.New(m, rt, tk, rulesync.Options{Pair: ha, Refresh: rf})
      rr := routerrulesync.New(...); rtr := routers.New(r, routers.Options{IPv6Hosts: ip, Rules: rs, ManualBaseline: rr})
      dc := discovery.New(r, m, ts, rt, tk)
  S10b inst := installation.New(r, m); st := setup.New(r, setup.Options{Rules: rs, Installation: inst, Gateway: host.Bridge{…}})
  S11 app := httpx.New(); 26 个 apiX.New(Deps{…}).Register(app.Router)（Flask blueprint 顺序）; tk.Validate(); auth.Install(app)
  S12 rt.RegisterStartup(load-config, subscription-cache-prune, agent-load, metrics-load-history, webdav-worker-init)
      rt.RegisterPeriodic(各服务 Loop())，顺序 = 现在 lifecycle.Names() 的顺序（测试断言）
cmd：weblistener 建好后 settings 两阶段注入（D5）→ rt.RunStartup → Listen → rt.Start
```

每一步只接收已经构造好的实例；按附录 B 第 8 节的现有 import 分层逐步核过无环（`o17-wiring/reach2.py`）。

### 2.5 测试怎么变

- 现在 29 个测试文件用 `server.NewApp()`：改成 `server.New(ctx, server.Options{Repo: repotest.Open(t), Runtime: lifecycletest.New(t), …})`，
  保留一个测试辅助 `servertest.New(t, opts…)`。
- `repo.SetHooks`、`agents.ResetHooks/ResetRuntime`、`*.SetDefault`、`notifications.ResetInputs`、`lifecycle.Configure`（50 处）、
  `SetMihomoEnvironment`（23 个文件，取决于 D2）等约 150 处「改全局再恢复」消失；测试可以 `t.Parallel()`。
- `httpxtest` 现在靠「每个测试给 ctx 绑一个仓库」隔离；W11 后改成每个测试构造自己的 `server.App`。
- 新增两个回归测试：`lifecycle` 循环/启动步骤名单与顺序和改造前一致；任务页行顺序与通知来源顺序一致（JSON 黄金）。

---

## 3. 分步计划

### 3.0 每步通用验证（agent 自己跑，全部用 `rt` 在 .32/.64）

- `gofmt -l` 无输出、`go vet ./...`（及 gate 里的两种 tag 组合）、`go build ./...`；
- 本步改到的包及其直接导入方 `go test`（写设备、锁、并发、生命周期相关的包加 `-race`）；
- 本步搬动了 pyref 作用域内包（mosdns、oxidns、provider、deploy、discovery、routersplit）的代码：更新 `tools/pyref/sources.json`，
  `go run ./tools/pyref -check` 通过，只允许 metadata 变化；
- 不跑全量 gate（协调者批量跑）。R 线开工前在 rebase 后的树上记一次 routerdiff `--allow-incomplete` 与 proddiff 基线（R0，不改代码），
  之后的抽样都和它比「有没有新增差异」。
- 复核（Opus）对「纯搬家」提交用 `git diff -M --stat` + `git diff -M` 确认只有包名/导出改名；对改签名/改分发/两阶段 setter 的提交逐行审。

### 3.1 R 线：拆 `routers`（10 步，按顺序）

通用做法（每步都一样）：`git mv` 文件到新目录、改 `package` 行、`goimports`；编译错误逐个按 1.3 表处理；未导出名被别的包用到时按 Go 习惯导出
（`gofmt -r 'uniqueStrings(a) -> device.Unique(a)' -w` 批量改调用点）。**允许临时类型别名**：为了让 `api/routers`、`clients`、`agents/ipv6`
等外部包在中间步骤不用跟着改，搬走的导出类型在 `routers` 里留 `type RouteEntry = device.RouteEntry` 这类别名（集中在一个
`routers/aliases.go`，注释写「R10 删除」）；函数不留转发包装。R10 统一删除别名并改外部导入。

| 步 | 内容 | 依赖 | 风险 | 验证（除通用项外） | 量 |
|---|---|---|---|---|---|
| R0 | 重算 `routers` 依赖与分层（`go@0a5d08ca1`）：104 个非测试文件共 34 010 行（较 `15ae7cb3c` 仅增 `management_repo.go` 导入 `jsonresp`、`router.go` 新增 `(*Router).StateRestoredAt`、`rule_sync_messages.go` 新增 `messageSyncRestored`），`layer.py a4.py` 仍是 13 包 0 未分配、14 组既有违规完全不变，1.2/1.3 无需调整；基线：`proddiff` 通过（2831 cases, `new=0 stale=0`），`routerdiff --allow-incomplete` 在 `.64` 后台记录至 `~/r0-routerdiff.log` 与 `~/.cache/rt-tmp/go-o17-r/routerdiff-r0*.{json,md}` ✅ 完成 `659ed48b4` | — | 无：不改代码 | `proddiff` + `routerdiff --allow-incomplete` 基线 | — |
| R1 | 建 `routers/device`：搬 `model.go`（`RouterState`、`Driver`、`PolicyRoute`、`RetirePolicyRoutes`、`StaleRoutes`、`StaleRouteWarning` 暂留）、`plan_model`、`staticindex`、`dhcpdns`；1.3 第一行的小工具下沉；`messages.go` 里 11 条共用消息搬过去，其余暂留。实施记录：`jsonValuesEqual` 依赖 `Row`/`sameRow`（自定义 JSON 解码），随 `Row` 在 R2 下沉到 `routers/ros`；`(*DhcpDnsEntry).AttributesRow` 仅有 1 处调用（`routeros.go`），改为包内函数 `dhcpAttributesRow`。 ✅ 完成 `2b47e0908` | — | 低：纯搬家 | 无额外 | M（≈0.5 天） |
| R2 | 建 `routers/ros`：`ros.Client` = 原 `RouterOS` 的连接/原语部分；`Row`、路径常量、批量、恢复、NAT 上下文、地址列表原语、脚本值、脱敏、`ros.Ownership`；1.3 第二行下沉。`routers` 里暂留 `type RouterOS struct{ *ros.Client }`，功能方法（72 个）先留在 `routers` 并把 `d.list(` 改成 `d.List(` 等；`ReadState` 与 `Apply` 分发移到 `routers/driver_routeros.go`，**删除 `RegisterRouterOSHandler` 和 6 个 `init()`**（分发改 `switch`）。实施记录：`ros.Client.ApplyManyWith`/`RestoreManyWith` 接收外层 `apply` 回调以保证批量内嵌套功能操作（如 `nat_list_sync`）走 `RouterOS.Apply` 完整分发；`Manifest`/`Manifests`/`ManifestHolder` 下沉到 `ros` 承担 `ros.Ownership`。 ✅ 完成 `bc8e75530` / `1dcb25470` | R1 | 中：驱动是写设备的路径；`Apply` 分发改写、manifest 搬家 | routers 全部测试 `-race`；routerdiff 抽样（RouterOS 发布/回滚/恢复配方各 2 个）；`switch` 覆盖 13 种操作的表驱动测试（旧注册表的种类全集） | L（≈1–1.5 天） |
| R3 | 建 `routers/ikuai`：`ikuai.Client` + 表单/批量/DHCP/IP 分组；`routers` 里暂留 `type Ikuai struct{ *ikuai.Client }`；`ReadState` 移到 `driver_ikuai.go`；`GroupReportSection`/`VerifyGroups` 收窄；`device.PolicyRoute(routes, comment)` ✅ 完成 `eadb991a7` / `f3aa96458` | R1（与 R2 无冲突文件，**可并行**） | 中：iKuai 写路径 | routers 测试 `-race`；routerdiff iKuai 配方抽样 | M（≈1 天） |
| R4 | 建 `routers/spec`：`router`、`stored_model`、`desired` + `DesiredDNSGuard`/`ROSPolicyDesired` 类型 + `routersOf`/`releaseRouterRecord`/`pendingProfile`/`findSyncRouter`；`setup`、`api/rules` 改 import（它们只用 spec 的东西） ✅ 完成 `2c24375ca` | R1 | 低 | `./internal/setup/... ./internal/api/rules/...` 测试 | M（≈0.5 天） |
| R5 | 建 `routers/oplock`（`joblock`、`operation_group`、`routeLocks`、`RouterOperationLock`）和 `routers/routeedit`（`route_editor`、`routeros_routes`、`ikuai_routes`）；`routeedit.Device` 小接口代替 `Driver` ✅ 完成 `f69e7b72f` | R2、R3、R4 | 中：锁层级（L1/L2）不能变 | `lockx.Violations()` 断言为空的现有测试；`-race` | M（≈0.5 天） |
| R6 | `RouterState` 拆成 `device.State` + 编排包里的 `RouterState`（1.4）；1.4 表里的函数全部收窄；`ReportParts` 变量与 `init` 删除，`BuildReport` 显式调用各功能的报告段；`RoutesReport`/`NatReport`/`NatItem` 下沉 ✅ 完成 `0d28586b8` | R2–R5 | 中：JSON 成员顺序、报告段顺序 | 带 state 的接口 JSON 逐字节比对（现有黄金 + 新增 1 条覆盖 `dns_guard`/`ros_policy`/`nat_context` 都在的样本）；proddiff `/api/routers` GET；routerdiff 检查/待推送配方抽样 | M（≈1 天） |
| R7 | 建 `routers/dnsguard`、`routers/fixedip`、`routers/clientlist`：功能方法改成包内 `type device struct{ *ros.Client }` 包装 + 导出入口函数（1.3「方法改函数」）；`fixedip.Device` 小接口；`clients` 改 import ✅ 完成 `66da38bdf` / `7736f5f82` / `ee64c0c3b` | R6 | 中 | 三个包与 `clients` 测试 `-race`；routerdiff DNS 守卫/固定 IP 配方抽样 | M（≈1 天；三包可在同一 agent 内分三个提交） |
| R8 | 建 `routers/rospolicy`（策略路由 + NAT 地址列表 8 个文件） ✅ 完成 `b13b0e0f4` | R6 | 中–高：策略/NAT 写入、读回、恢复最复杂 | rospolicy 全部测试 `-race`；routerdiff 策略/NAT 列表配方抽样（含失败回滚）；R12 安全测试（`r12_safety`、`rospolicy_safety`）必须原样通过 | M–L（≈1 天） |
| R9 | 建 `routers/ipv6`（18 个文件）；`ForwardInputs`；`agents/ipv6` 改 import，确认它不再 import 编排包（`go list -deps` 断言） ✅ 完成 `f4f9a8007` | R8 | 中 | ipv6 测试 `-race`；`./internal/agents/ipv6/...`；routerdiff IPv6 配方抽样 | M（≈1 天） |
| R10 | 建 `routers/rulesync`（10 个文件，在编排包之上）；`CaptureManual` 变量 → 编排服务字段（暂由 `routers` 的现有构造路径填，W 线再挪到 server）；删除 `aliases.go`，改 `api/routers`、`clients`、`agents/ipv6`、`api/clientdevices`、`server` 的导入；`messages.go` 拆完；新增 `routers/layers_test.go`（用 `go/parser` 读各包 import，按 1.2 的允许表断言，不 exec `go`）；更新 `ARCHITECTURE.md` 2.3/3.3/3.5。实施记录：`CaptureManual` 暂保持为 `routers.CaptureManual` 回调，由 `rulesync.Wire()`（在 `server.go` 中紧随 `routers.Wire` 调用）注册，W9 再收进编排服务结构体；`proddiff` 通过（2831 cases, `new=0 stale=0`）；`routerdiff --allow-incomplete` 重叠 2685 条配方对比 `status_changed=0`（无新增差异） ✅ 完成 `2560d5519` / `492cec74e` | R9 | 低–中：机械但面广 | 全部 routers 子包 + 外部导入方测试 `-race`；proddiff；routerdiff 一轮 `--allow-incomplete` 全量（只看与 R0 基线相比有无新增差异） | M（≈1 天） |

R 线合计约 8–9 个工作日（单 agent 顺序）。R2/R3 可并行；R7 的三个包互相独立但都改 `driver_routeros.go` 的分发 `switch`，建议同一 agent 顺序做。

### 3.2 W 线：显式组装（12 步 + 1 个可选）

| 步 | 内容 | 依赖 | 风险 | 验证（除 3.0 外） | 量 |
|---|---|---|---|---|---|
| W0 | **先清理，不改结构**：删 `InitAgentManager`、ippresence 那份 `FullTun`、重复 `RegisterStartup`；2.1 第 1、2 种钩子改直接调用（`RedactEasytierJSON`、notifications 的 tasks 输入、agents.Hooks 不读的 5 个字段、`SetGatewayGuards`、`SetSessionReader`、`SetNativeClassicalIPLines`、`wireLoopWakes`、四个自己装自己）；修 `mosdns.downloadRule` 捕获问题 ✅ 完成 `11a1435f5`（实施记录：`repo.RecordHooks.RedactEasytierJSON` 保留为已弃用空字段至 W9 以免触碰 `internal/routers/repository_hooks_test.go`；`routersplit.SetSessionReader` 与 `api/agents.SetContentPrepare` 生产路径改为直接调用，暂留测试覆盖接缝至 W8/W10） | — | 低 | 受影响包测试；`lifecycle.Names()` 与启动步骤名单前后一致 | M（≈1 天） |
| W1 | `server.New(ctx, Options)`：把 cmd 的启动步骤注册搬进 server、`NewApp()` 变成测试辅助对它的包装；行为不变 ✅ 完成 `9b56978ea` | W0 | 低 | `./internal/server/... ./cmd/...`；启动顺序测试 | S（≈0.5 天） |
| W2 | `repo`：`Hooks`/`RecordHooks` 全局 → `repo.Options` 实例字段（`RegisterHooks`/`SetHooks` 删除）；`planDeletion` 下沉；导出脱敏用 `configexport.Exporter` 小接口；仓库 `Open` 由 server 传 Options ✅ 完成 `36ed68477`、`412dd4e1d`（实施记录：`planDeletion` 已下沉至 `repo/node_delete.go` 并删除 `apinodes.Wire`；生产路径经 `server.RepoOptions()` 与 `webdav.Subscribe(r)` 全部走实例字段；`repo.Hooks`/`RegisterHooks`/`SetHooks` 暂留为回退路径，供未触碰的 `internal/routers` 与 `NewApp()` 测试使用，待 W9/W11 删除） | W1 | 中：导出脱敏/路由器作业锁走错实例会泄露凭据或绕过锁 | repo、configexport、api/nodes、webdav 测试；导出脱敏黄金；proddiff 导出类用例 | M（≈1 天） |
| W3 | `lifecycle`：注册类函数（`RegisterPeriodic/Startup/Shutdown`、`Configure`）改成 `*Runtime` 方法，全局 `defaultRuntime` 删除；`Detach`/`Stopping(ctx)`/`SpawnerFrom(ctx)`/`BindRequest` 保留 ctx 形式（D8）；各包的注册暂时由各自 `Wire` 拿 server 传来的 rt 调用 ✅ 完成 `63a8f86db`（实施记录：`*Runtime` 新增 `RegisterPeriodic/RegisterStartup/RegisterShutdown/Stop/ReportReady` 与 `NewProcessRuntime`，`cmd` 与 `server.New` 及 11 个非 routers 子系统已全部显式传 `rt`；`defaultRuntime` 经 `UseRuntime` 暂留回退，供未触碰的 `internal/routers` 与测试使用，待 W9/W11 删除） | W1 | 中：关停顺序、首请求启动 | lifecycle、o3 关停测试 `-race`；`Names()` 顺序测试 | M（≈1 天） |
| W4 | `tasks.Registry`、`notifications.Service` 实例化（行构建器/运行器/间隔编辑器/输入改为实例方法；late route 改 `Validate()`） W4 ✅ 完成 `e2cf4ed83`（实施记录：`tasks.Registry` 与 `notifications.Service` 已实例化，`lateRoutePackages` 删除改为 `server.New` 末尾 `tk.Validate()`/`nt.Validate()`；`tasks.Default()`/`notifications.Default()` 暂留为回退路径供未触碰的 `internal/routers` 与测试使用，待 W9/W11 删除） | W3 | 低–中：任务页行顺序 | 任务页 JSON 黄金、通知列表黄金；api/tasks、api/notifications 测试 | M（≈1 天） |
| W5 | `agents.Manager`：`Hooks` → `agents.Options` + 两阶段 `SetPairPublisher`（D1）；`singleton`/`Get`/`Init`/`ResetRuntime`/`ResetHooks`/`OnManagerInit`/`OnAgentDeleted`/`SetRuleSyncRow`/`SetDNSFollowRow` 删除，Agent 任务行拼装搬到 `agents/agenttasks`；**`agents.Get` 暂时保留为读 server 注入的那个实例的薄函数**（W11 删） ✅ 完成 `3affb852e`（实施记录：`agents.Options` + `MetricsSink`/`PublicationObserver`/`PairPublisher` 小接口 + D1 两阶段 `SetPairPublisher`/`MarkStarted` 已落地；`agentRows` 迁入 `agents/agenttasks` 并删除 `SetRuleSyncRow`/`SetDNSFollowRow`；`server/agents.go` 接管组装；`agents.Hooks`/`ResetHooks`/`ResetRuntime`/`OnManagerInit`/`OnAgentDeleted` 暂留回退供 W6/W10/W11 迁移期测试使用） | W4 | **高**：部署状态机、HA fail closed、心跳路径 | agents 全部测试 `-race`；api/agents difftest；HA 未接时 fail closed 的负对照（1 条） | L（≈2 天） |
| W6 | Agent 子领域：metrics、trafficstore、monitor、upgrades、dnsfollow、dnsha、rulesync、ipv6 各 `New`，按 Manager 的 map 变字段，循环由 server 注册 ✅ 完成 `3688d1be1`、`94fefcb87`（实施记录：8 个 Agent 子领域服务均已实例化并在 `server/agents.go` 统一组装、挂载 `App` 字段及 `rt` 循环；`monitor.Service` 实现 D1 两阶段 `SetHARules`/`MarkStarted` 与 nil fail-closed；`dnsha`/`dnsfollow`/`rulesync`/`ipv6` 均改为实例级 `Waker` 并补齐配置提交/状态提交/Agent 上线/规则刷新唤醒测试；`ForManager`/`UseService`/`deploy.SetPairPublication`/`deploy.SetIPv6Enroll`/`monitor.SetHARulesJSON`/`routers.IPv6HostRequest` 暂留过渡桥接供未迁移的 W7/W9/W10 调用方与 `NewApp()` 测试使用） | W5 | **高**：HA 成对发布、规则同步、DNS 联动的唤醒 | 各包 `-race`；o17 运行状态任务新增的唤醒测试；api/agents difftest | L（≈2–3 天；可拆 W6a 指标/流量/监控/升级 与 W6b 联动/HA/规则同步/IPv6 两个 agent 并行） |
| W7 | 生成链路：substore、refresh（实例事件 `OnRulesRefreshed`）、provider、aggregations、deploy、contentsync、mihomoenv（convert 环境按 D2 的决定） ✅ 完成 `1f7fa4e66`、`0d3e6ac19`（实施记录：按 D2 裁定不做 W12，`convert.SetMihomoEnvironment`/`SetMihomoIntegrations` 改为只能设一次并返回哨兵错误 `ErrMihomoEnvironmentAlreadySet`/`ErrMihomoIntegrationsAlreadySet`，同时提供仅测试可用的 `OverrideMihomo*ForTest`/`ResetMihomo*ForTest`；`substore`/`refresh`/`provider`/`aggregations`/`deploy` 已实例化并在 `server/generate.go` 显式组装，保留 `Default()`/`UseService` 回退供未触碰的 `internal/agents` 与测试至 W5/W6/W11 删除；重录 12 份 mosdns/oxidns pyref 黄金文件，仅 metadata 变化，entries 零差异）| W4 | 中：生成结果、provider 回退 | proddiff 生成类全量；config/subscriptions/aggregations difftest；pyref check（provider、deploy、mosdns、oxidns 在作用域内） | M–L（≈2 天） |
| W8 | 新包 `internal/rulesource`（从 api/rules 抽出规则内容读取）；setup/setupflow/routersplit/gateway/onboarding/installation/host 的钩子改构造参数或直接调用（D4） ✅ 完成 `29e40074c`、`a653b15aa`（实施记录：新建 `internal/rulesource` 承载规则内容读取，`api/rules` 委派其实现；`gateway` 新增 `NewConfigWithReader`、`setup` 直接 import `gateway/onboarding` 提供 `DefaultDNSOwner()`（D4）与 `NewWithOptions`、`onboarding` 新增 `AdoptOwnedWrite` 字段与 `New(r, ServiceOptions)`，nil 均保持 fail closed 并补负对照测试；`server.New` 实例化 `rulesource.Service` 并保持 D9 `VerifyRouter` 注册与 `TestSetupOwnersAllRegisteredByApp` 通过；`routersplit.SetSessionReader` 生产已直调 `setupflow.ProfileSessions`，测试注入接缝暂留至 W11 删除，避免破坏 `api/gatewayonboarding/difftest` 并发测试） | W4 | 中：向导、网关安装 | setup、gateway 系列、api/gatewayonboarding、api/linuxgateway difftest | M（≈1.5 天） |
| W9 | routers 组装：`routers.New(Options{IPv6Hosts, Rules, ManualBaseline, …})`、`rulesync`（路由器）`New`，循环/任务行/通知摘要由 server 注册；`IPv6HostRequest`/`RulesetContent`/`NewRouterID` 变量删除 ✅ 完成 `e40b553fb`（实施记录：新增 `routers.Service`（`routers.New`）与 `rulesync.New`，将 `ruleSyncLoop` 收进 `RuleSyncService` 实例字段并消除 `RunRound` 的 `*s` 结构体拷贝；删除 `spec.RulesetContent`/`spec.NewRouterID`/`routers.IPv6HostRequest`/`routers.CaptureManual`/`routers.Wire`/`rulesync.Wire`；`server/routers.go` 组装两服务并注册 `router-rule-sync` 循环与 `tasks.SlotRouters` 行构建器/运行器，`server/repo.go` 填入 `RouterJobLock: routers.JobLockRecord`，`server/app.go` 在 `notifications.New` 填入 `Summarize: routers.NotificationSummary`；`api/routers` 新增 `NewHandler(rtr, rr)`，暂留 `Register` 与 `routers.Default`/`rulesync.Default` 过渡桥供未迁移测试至 W10/W11 删除；所有 nil 协作项保持 fail closed 并补反向测试，`TestProductionAssemblyLeavesNoDependencyUnset` 与 `routers/layers_test.go` 均通过） | R10、W5、W8 | 中 | routers 子包 `-race`；routerdiff 抽样 | M（≈1 天） |
| W10 | api 包全部改 `Handler` 结构体（26 个入口，213 个路由注册，约 200 个处理函数）；可拆 W10a `api/agents`（66 条，按现有 `registerXxx` 分成几个子 Handler）、W10b routers/rules/rulelibrary/config/profiles/generate/mosdns/oxidns、W10c 其余，**三个 agent 并行**；W10a ✅ 完成 `630307648` / `aa6e08e34`（实施记录：`api/agents` 新增 `Deps`、`Handler`、`NewHandler` 与 17 个按 `registerXxx` 划分的子 Handler，处理函数不再直接调用 `repo.GetRepository`/`agents.Get`/`rulesync.Get`/`ForManager`/`TrafficForManager`/`GetNative`/`GetProbeStore`/`GetRegionStore`/`repo.DataDir`，`content_wire.go` 的 `prepareContent` 直接接收 `m`；`server/api_agents.go` 构造并注入生产服务实例；过渡期 nil 字段在 `Deps` 辅助方法保留回退供 `Register(r)` 与 `NewApp()` 测试至 W11 删除；`content_observer_proddiff.go` 新增 `(h *Handler) InstallProddiffContentObserver` 并保留包级入口）；W10b ✅ 完成 `147bc26a7` / `12c03f8a5` / `f10f4c728` / `f1d982e03` / `3b6a0c070`（实施记录：8 个 W10b api 包均新增 `Deps` / `Handler` / `New(Deps)` / `(*Handler).Register`，处理函数体不再直接调用 `repo.GetRepository`/`agents.Get`/`*.Default()`；组装逻辑集中在 `server/api_w10b.go` 并保持 `routePackages` 注册顺序、路由路径、方法、endpoint 名与中间件顺序不变；保留旧 `Register(r)` 及 nil 字段回退辅助方法以兼容未迁移的 `NewApp()` 与单包测试至 W11 删除；`repo.ResolveProfileID`/`repo.UpdateConfig`/`repo.UpdateSharedConfigs`/`repo.Config` 等包级 scope 函数留待 W11 改为 `*repo.Repository` 方法；新增 `TestW10bHandlersUseInjectedDependencies` 验证生产组装下 8 个包均使用注入实例而非全局定位器）；W10c ✅ 完成 `ebf6d4ed9` / `f09981edb` / `28f2794c9` / `79aa2594d` / `ab33bcbf7`（实施记录：覆盖 16 个 api 包——`auth`、`logs`、`stats`、`domaindiscovery`、`tasks`、`notifications`、`clientdevices`、`gatewayonboarding`、`ipv6`、`linuxgateway`、`setup`、`nodes`、`proxygroups`、`aggregations`、`subscriptions`、`settings`；所有处理函数改为 `Handler` 结构体方法并经 `server/api_w10c.go` 显式注入依赖；`settings.Handler` 落地 D5 两阶段 `SetWebListener`/`MarkStarted` 并在 `cmd/configflow/service.go` 注入，补重复设置/启动后设置/显式 nil 不回退全局测试；`gatewayonboarding.NewHandler` 补 nil `AdoptOwnedWrite` fail-closed 503 负对照测试；各包暂留 `Register` 过渡桥接供未迁移测试至 W11 删除；`proddiff` 2831 cases `new=0 stale=0`） | W5–W9 | 中：面广但机械；鉴权门禁按 endpoint 名，不能改 endpoint 名 | 全部 difftest；proddiff；`routes.tsv` 380 条全部注册的现有测试；MCP 经 `App.Invoke` 的测试 | L（≈3 天，并行约 1–1.5 天） |
| W11 | 删除定位器：`GetRepository`/`SetRepository`/`WithRepository`（D11）、`agents.Get`、`substore.Default/SetDefault`、`webdav.Default/SetDefault`、`trafficstore.GetNative`、`health.Get*Store`、`notifications.TaskStatus(ctx,dir)` 等；`repo` 包级 scope 函数改方法；`httpxtest`/`servertest` 改为每测试构造 App；所有 `Set*ForTest`/`Reset*` 删除；`ARCHITECTURE.md` 2.x/2.4、`AGENT-BRIEF.md` 更新 | W10 | 中：测试改动最多（约 150 处） | `go test ./...`（非 race）+ 受影响包 `-race`；随后协调者全量 gate | L（≈2 天） |
| W12（可选，D2） | convert 生成环境显式传参：`convert` 入口改成 `convert.Env` 的方法，删除 `SetMihomoEnvironment`/`SetMihomoIntegrations` | W7 | 中 | 生成类全部黄金 + proddiff | L（≈2–3 天） |

W 线合计约 18–20 个工作日（不含 W12）。

### 3.3 并行与顺序

```
R1 ─┬─ R2 ─┐
    ├─ R3 ─┼─ R5 ─ R6 ─ R7 ─ R8 ─ R9 ─ R10 ───────────────┐
    └─ R4 ─┘                                                ├─ W9 ─┐
W0 ─ W1 ─ W2 ─ W3 ─ W4 ─┬─ W5 ─ W6(a‖b) ────────────────────┘      ├─ W10(a‖b‖c) ─ W11
                        ├─ W7 ───────────────────────────────────────┤
                        └─ W8 ───────────────────────────────────────┘
```

- **R 线与 W0–W8 可以两位 agent 并行**：W0–W8 不碰 `internal/routers`（W0 若顺手碰到 routers 的钩子，留给 W9）。两线都改 `server/server.go`，
  冲突小：W 线把组装拆到 `server/agents.go`、`server/generate.go`、`server/setup.go` 等按子系统的文件里，R 线只改 `routers` 那几行。
- W7、W8 与 W5/W6 改不同的包，W4 之后可以并行（第三位 agent）。
- 建议批次（协调者 gate）：批 1 = R1–R4 + W0–W1；批 2 = R5–R6 + W2–W4；批 3 = R7–R9 + W5；批 4 = R10 + W6–W8；批 5 = W9–W10；批 6 = W11（+W12）。
  每批前 rebase 到最新 `go`。

### 3.4 合计

22 步（R1–R10、W0–W11）+ 1 个可选（W12）。工作量约 27–29 个 agent 工作日；按上面的并行（2–3 位 agent）约 2–2.5 周墙钟时间，
不含 gate 排队与复核往返。HR（需要 Opus 对抗复核）：R2、R8（写设备）、W5、W6（Agent 状态机/HA）；其余普通复核。

---

## 4. 风险与需要裁定的点

### 4.1 需裁定（协调者审，必要时问维护者）

| 编号 | 问题 | 建议 |
|---|---|---|
| **D1** | Manager ↔ dnsha（以及 monitor↔dnsha）真双向依赖的处理（附录 B J1） | 两阶段 setter，只在 server、`Start` 前调一次；比「闭包前向引用」显式，比「把 HA 塞进 Manager」改动小 |
| **D2** | convert 的生成环境：显式传参（W12，改所有生成入口和 23 个测试文件）还是保留「server 构造期设一次、之后只读」的包级环境 | 切换前做 W0–W11，W12 排在最后视时间决定；若不做，`SetMihomoEnvironment` 改成只能调用一次（第二次返回错误），去掉 restore |
| **D3** | routers 新包名：`device`、`ros`、`ikuai`、`spec`、`oplock`、`routeedit`、`dnsguard`、`ipv6`、`fixedip`、`clientlist`、`rospolicy`、`rulesync` | 同意或改名；注意 `routers/ipv6` 与 `agents/ipv6`、`routers/rulesync` 与 `agents/rulesync` 同名（导入时要起别名，但各自目录语义清楚）。备选：`rosipv6`、`routersync` |
| **D4** | setup 与 onboarding 谁 import 谁（两者现在互不 import） | setup → onboarding（DNS 纯函数直接调用），onboarding 的 `AdoptOwnedWrite` 用构造参数 |
| **D5** | api/settings 与 weblistener 的晚绑定 | 保留一个两阶段字段（`settings.Handler.SetWebListener`），只在 cmd 建好 listener 后调一次 |
| **D6** | provider ↔ aggregations、notifications ↔ Manager 的先后 | 两阶段 setter + 「已启动后不得再设」断言 |
| **D7** | `auth`（29 个导入方，`auth.Require` 中间件到处用）改实例还是保留只读单例 | 保留只读单例：`Configure` 只能调用一次；不在本批改 |
| **D8** | `lifecycle` 的 ctx 形式 API（`Detach` 289 处、`Stopping`、`SpawnerFrom`、`BindRequest`）保留 | 保留（context 承载生命周期是地道 Go）；只删全局注册与 `Configure` |
| **D9** | `setup.ExternalOwners.VerifyRouter` 生产里从未注册，向导走到那一步一律「不可用」 | 需要确认：是漏接还是有意未移植。若漏接，W8 里由 routers 服务提供（单独登记行为变化） |
| **D10** | 纯 memo/反射缓存（附录 B D 类 34 项）是否允许保留为包级变量 | 允许：键不含实例身份、内容不可变、无测试替换 |
| **D11** | `repo.WithRepository`（httpxtest 依赖它隔离测试）是否随定位器一起删 | 一起删（W11），测试改为每测试构造 App |
| **D12** | `ros` 包导出面约 150 个名字（其中约 100 个是为功能包导出的原语和路径常量） | 接受：它是 RouterOS REST 工具箱，6 个功能包都要用；可在 R10 后把行工具改成 `Row` 方法再收一轮 |
| **D13** | R 线中间步骤允许临时类型别名（`aliases.go`，R10 删） | 允许：让每步小、外部包在中间步骤不跟着改 |
| **D14** | api 包全部改 `Handler` 结构体（W10）放在切换前 | 维护者已要求 O17 结构项全部切换前完成；W10 是 1.5 的必要部分（处理函数是定位器的主要用户），建议做 |

### 4.2 风险

| 风险 | 触发 | 防护 |
|---|---|---|
| 钩子变构造参数后 fail closed 变 fail open（nil 被当成「允许」） | W2、W5、W6、W8 | 规则 2.2-1；每个「未接时拒绝」的钩子保留一条负对照测试（附录 B 标了哪些是 fail closed） |
| 启动步骤/循环/任务行/通知来源的顺序变了 | W0、W1、W3、W4 | 名单与顺序黄金测试；现在靠「按名字原地替换」的隐式顺序改成 server 一处显式列表 |
| `RouterState` JSON 成员顺序或报告段顺序变化 | R6 | 1.4 的字段顺序；带 state 接口逐字节比对；`BuildReport` 段顺序测试 |
| `Apply` 分发漏掉一种操作 | R2 | 13 种操作的表驱动测试；`switch` 的 `default` 仍走 `c.Apply` 且对未知功能种类返回原来的错误文案 |
| 锁层级（L1/L2/L3）在搬家后被打乱 | R5、W5 | `lockx.Violations()` 为空的现有测试；`-race` |
| 两阶段 setter 在启动后被调用 / 漏调 | W5、W6、W7、W10 | setter 内断言 + 测试；server 构造完成后 `Validate()` 检查必需的两阶段依赖都已设置 |
| 与「运行状态收尾」任务冲突（它改 routers `last_release` 和 agents 循环/启动） | 全部 | 它合入后再开工；R0 重新跑附录 A 脚本，数字有变化就更新本文 1.2/1.3 再动手 |
| pyref 元数据失配（批 45/46 的教训） | W7、W8 及任何搬动作用域包的步骤 | 3.0 的 pyref 规则 |
| `o10_r11_gate_test.go` 源码门禁扫描范围漏了新包 | R1–R10 | 每步跑 `./internal/server`；R10 核对门禁的包列表 |
| 大 diff 难复核，复核者漏看语义变化 | R2、R6、W5、W10 | 纯搬家与改语义分提交；复核用 `git diff -M`；纯搬家提交里出现非改名改动即退回 |
| 测试大改（约 150 处）时把测试改弱 | W11 | 复核规则：测试只允许把「改全局」换成「构造实例」，断言不得删减；`go test -count=1` 前后测试数对比 |

---

## 5. 工作量汇总

| 线 | 步 | 量 | 合计 |
|---|---|---|---|
| R | R1 M、R2 L、R3 M、R4 M、R5 M、R6 M、R7 M、R8 M–L、R9 M、R10 M | 0.5/1.5/1/0.5/0.5/1/1/1/1/1 天 | ≈ 9 天 |
| W | W0 M、W1 S、W2 M、W3 M、W4 M、W5 L、W6 L、W7 M–L、W8 M、W9 M、W10 L、W11 L | 1/0.5/1/1/1/2/2.5/2/1.5/1/3/2 天 | ≈ 18.5 天 |
| W12（可选） | L | 2–3 天 | |

---

## 附录 A：分析脚本与原始数据（`design/o17-wiring/`）

| 文件 | 用途 |
|---|---|
| `filedeps.go.txt` | 类型检查一个包目录，输出每个文件定义的顶层符号和文件间引用边（`go run` 前改名为 `.go` 并放到模块外的目录；第二个参数 `tests` 时包含测试文件） |
| `globals.go.txt` | 全部非测试包里含函数的包级变量及写入/读取位置（输出即 `globals.txt`） |
| `fields.go.txt` | 每个函数用到的 `RouterState`/`Desired`/`Report`/`Router` 字段（1.4 的来源） |
| `routers-filedeps.txt`、`routers_files.txt` | 104 个非测试文件的依赖边与行数（batch-48 分支 `15ae7cb3c`） |
| `layer.py` + `a4.py` | 按 1.2 的分配与允许表检查跨包引用；`python3 server/design/o17-wiring/layer.py server/design/o17-wiring/a4.py`（在仓库根目录跑）。当前输出剩 14 组「违规」，**全部**是 1.3/1.4 里要靠改签名、上移 `ReadState`、小接口、删 `ReportParts`/`CaptureManual` 消掉的（脚本不模拟签名修改），没有别的 |
| `imports.json`、`hooks.txt`、`files.py`、`imports.py`、`reach.py`、`reach2.py` | 包级 import 表与「T 能否直接 import F」的闭包检查（附录 B 用） |

重跑方法（.64，`RT_HOST` 固定）：把 `*.go.txt` 复制到服务器 `~/cfwire/*.go`，在 `server/` 下
`go run ~/cfwire/filedeps.go internal/routers > routers-filedeps.txt`。

## 附录 B：钩子/全局变量/定位器逐项清单

见 `design/o17-wiring/inventory.md`（W01–W35、init 12 个、`repo.Hooks`/`agents.Hooks` 逐字段、68 个 `Set*/Reset*/Register*/On*`、
127 个包级变量逐项分类、定位器调用量、api 包形状、现有依赖表、构造顺序 S0–S13、裁定点 J1–J12；本文 D1–D11 与 J1–J12 的对应：
D1=J1、D2=J2、D6=J3、D4=J4、D5=J5、D7=J6、logx=J7（保留单例）、J8 并入 W0（`mosdns.downloadRule`）与 W7（HTTP 客户端注入）、D10=J9、D9=J10、D11=J11、J12=W0）。

## 协调者裁定（2026-11-23）

- 维护者：22 步（R1–R10、W0–W11，含 D14 api Handler 结构体）全部在切换前完成。W12（convert 生成环境显式传参）视时间，不做则改成「只能设一次」（D2）。
- 其余 D1–D14 按本文建议执行；D9（setup.VerifyRouter 漏接）由单独任务 go-setup-verify-router 先排查修复，W8 以其结果为准。
- 开工条件：go-o17-state-followup 合并后，先用附录 A 脚本在新 go 上重算依赖并更新 1.2/1.3，再按第 3 节分三条线并行；R2、R8、W5、W6 由 Opus 对抗复核，其余步骤 Opus 复核；按第 3 节建议分 6 批走门禁。
- 维护者 2026-11-23：不按步跑门禁。每步只做编译 + vet + 受影响包测试（几分钟）；R 线（纯搬家）全部做完后跑一次完整门禁；W 线在 W6 之后一次、W11 之后一次。共 3 次完整门禁（原建议 6 批）。复核仍按步骤（R2、R8、W5、W6 对抗复核），但可以按线合并成一次交付。
- D9 已解决（2026-11-24，分支 go-setup-verify-router）：确为漏接，server 启动时注册 VerifyRouter（routers.Inspect + BuildPlan，只读），与 Python setup_completion 对照一致；全仓钩子审计无第二个漏接。W8 不再需要处理 D9。
