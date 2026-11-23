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
3. **分步**（第 3 节）：R0–R9（拆 routers，10 步）+ W0–W11（组装，12 步），共 22 步，每步单独可编译、可测、可复核、能过 gate。
   R 线和 W 线前半段改的是不同的包，**可以两位 agent 并行**；W 线里按子系统分的步骤（W3–W8）在 W2 之后也可以并行。
4. **需裁定**（第 4 节）：包名、`ros` 包导出面偏大（约 150 个导出名）、`api/*` 全部改成 `Handler` 结构体（380 个路由的处理函数
   变成方法）是否在切换前做、`lifecycle` 全局运行时是否一并改成实例、测试钩子迁移的取舍。

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
