# O10-R2 HR 复核：Config/Shared/Profile/SystemConfig/Backup 读借用不可变快照（2026-11-21）

复核对象：`go-perf-r2`（`f4903c9ec..b8ed3b2cb`，5 个提交），cherry-pick 到 `batch-23`（= R1 + R1 复核修复之上）。
方法：通读 `repo/config_borrow.go`、`read.go`、`resolve.go`、`profiles.go`、`config_from.go`、`validation.go`、
`system_view.go`、`snapshot.go`、`transaction.go`、`view_guard.go` 和 model 层类型；**自己 grep 全部调用点**
（`internal`、`cmd`、`tools` 下非测试调用 70 处 + 函数值/包装 2 处，和作者表一致），外加全仓扫描：
经嵌套指针/切片元素的赋值、嵌套记录 `Set/SetJSON/Clear`、对模型列表的 `append`、`slices.Sort/Delete/Insert`、`sort.*`、
`*p = …`/`(*p)[i] = …`，以及生成器 convert/configgen/mosdns/oxidns/subs/provider/deploy/gateway/routers。

## 结论

R2 的借用语义成立，没有发现高危问题，也没有找到改写借用快照的生产代码。发现 1 个中危（测试安全网），已修；
3 条低危或说明，其中 1 条已修。行为差异只有 `Profile.Key()` 一处，对外无影响，已记入 behaviour-changes。

## 中

### M1 源码门禁 `TestO10R2DeepCopiesAreExplicit` 能被绕过（已修，整合提交）

和 R1 复核 M2 是同一个问题：`r2CopySites` 只匹配 `FuncDecl` 里的 `CallExpr`。`clone := r.PublishConfig; clone(…)`（方法值）、
`var dup = copyRecord[model.Shared]`（包级别名，不在任何 FuncDecl 里）都看不到；另外只扫 `internal/`，`cmd/`、`tools/`
里的深拷贝（proddiff 工具调 `Repository.System`）不在门禁内，而门禁注释说 `System` 也是被管住的显式拷贝。

修复（`9159df4db`）：改为匹配**任何**标识符引用（含 selector 的 `Sel`），包级声明记为 `file:<package-level>`；遍历
`internal`、`cmd`、`tools`；repo 包外带参数的 `.System(…)` 调用也计入（`System` 同时是字段名和类型名，所以只看带参数的调用）。
proddiff 的 3 处 `System` 加了带理由的豁免。自测 `TestO10R2DeepCopyGateDetectsIndirectUse` 覆盖：直接调用、方法值、闭包、
包级别名、`System` 调用与字段/类型的区分、借用读不计、repo 内函数值和包级别名。现有树上的站点集合不变，没有过期豁免。

## 低 / 说明

### L1 “给借来的顶层切片 append 是安全的”依赖一个没被固定的解码器性质（已修，只加测试）

`config_borrow.go` 的约定允许调用方给借来记录的顶层切片 append。浅拷贝共享底层数组，如果快照切片 `cap > len`，
两个借用者并发 append 会写同一块内存（数据竞争，且互相看到对方的元素）。R2 只对 `resolveConfig` 里的
`ProxyGroups/RuleConfigs/Routers.Items/RuleLibrary` 做了 `slices.Clip`，`Profile()`、`Shared()`、`SystemConfig()` 和
`Config.SystemConfig` 里的切片都没有。

实测后确认**目前不会出问题**：model 解码器 `record.go` 对每个切片 `reflect.MakeSlice(typ, n, n)`，快照里所有切片
（含值类型成员和切片元素里的）都是 `cap == len`，append 一定重新分配。所以那几处 `Clip` 是空操作，作者的约定也成立，
只是没有测试固定。我一度在 `lend` 里加了反射 clip，确认这个性质后撤回（不要无用的运行时代价），改成加测试（`df87380b0`）：

- `TestO10R2SnapshotSlicesHaveNoSpareCapacity`：Shared、Profile、SystemConfig、Backup、聚合、路由器里不经指针能到的
  所有切片（含 `Routers.Items`、`MosDNS.*`、`DomainDiscovery.*`、`SmartGroups`）和 `ProxyGroup.ManualNodes` 都 `cap == len`，
  至少检查 6 个非空切片，防止空转。
- `TestO10R2ConcurrentAppendsToLentSlices`：4 个借用者同时 append `Shared.Nodes`、`Profile.ProxyGroups`、
  `Config.SystemConfig.NodeTestDNS`、`Config.RuleLibrary`、`Config.Aggregations[0].Nodes`。
- 负向对照：把解码器临时改成多分配 1 个容量，两个测试都失败（spare capacity 报错 + 并发测试失败）。跑完已恢复。
- 注释写明：如果以后解码器改了，`lend` 必须 clip。

### L2 SystemConfig 的 guard 负向对照是空转的（已修）

`TestO10R2ViewGuardCatchesModifiedBorrowedValues/SystemConfig pointer` 在 `DefaultProfileID == nil` 时 `t.Skip`。
新建仓库不写 `default_profile_id`（探针实测为 nil），所以这个子测试一直被跳过，SystemConfig 的嵌套指针篡改从没验证过。
修复：fixture 写入 `default_profile_id` 和 `node_test_dns`，去掉 Skip，并加两个切片元素用例
（`SystemConfig().NodeTestDNS[0]`、`Config().SystemConfig.NodeTestDNS[0]`），都会触发 guard。

### L3 请求 Scope 缓存的是借用的 Config

`repo.Config(ctx, id)`（包函数）把 `repository.Config` 的结果缓存在请求 Scope 里。如果同一个 Scope 之后在写事务里再调用它，
拿到的是缓存的借用值而不是工作副本的深拷贝。这和 R2 之前一样（以前缓存的是已提交代的副本，同样不是工作副本），
唯一的包外调用方（`api/rules/read_adapter.go:scopedLocalConfig`）只读。不改，只记录。

### 说明：Profile.Key() 行为差异

借用的 Profile 浅拷贝保留存储键，`Key()` 返回映射键；旧实现的编解码副本回落到 `id`。只有 id 与映射键不一致时才不同，
而 `validateStored` 要求 `profile.ID == Key()`，已提交的快照里不可能出现；repo 包外没有对借用 Profile/Config 调 `Key()`
（`routers/stored_model.go`、`discovery`、`agents/rebind.go`、`notifications` 等都遍历 `document.Profiles`）。
已写入 `behaviour-changes.md`「O10-R2」。

## 逐项核对

### 1. 浅拷贝后哪些成员会写穿

| 记录 | 值成员（浅拷贝后归调用方） | 共享（禁止原地改） |
|---|---|---|
| `Profile` | ID/Name/… 字符串、`Revision`；`Mihomo/Surge/Loon/MosDNS/Routers/DomainDiscovery/DeviceNotes/ClientFixedIPs` 结构体本身 | `ProxyGroups`、`RuleConfigs` 的元素；`MosDNS.*Rulesets/*Rules/CustomMatches`、`Routers.Items`、`DomainDiscovery.Ignored/History`、`Surge.SmartGroups` 的元素；`Mihomo.ProviderUpdateInterval/EasyTier`、`MosDNS.*` 指针 |
| `ProxyGroup`（元素） | — | `Enabled`、`*[]string`（Subscriptions/ManualNodes/Aggregations/IncludeGroups/Proxies/Use）、`ProxiesOrder`、`Chain`、各 `*string/*Int/*bool` |
| `Shared` | `Revision` | `Subscriptions/Nodes/Aggregations/RuleLibrary` 的元素；元素里的 `HealthCheck`、`DialerRef`、`Aggregation.Subscriptions/Nodes` |
| `SystemConfig` | 字符串成员 | `RetiredRuleProxyTokens`、`NodeTestDNS` 元素；`NodeTestDNSConfig` 和全部 `*string/*bool/*Int` |
| `Backup` | 字符串成员 | 全部指针成员 |
| `Config` | `Profile` 指针（指向调用方自己的浅拷贝）、`SystemConfig/Backup`（值拷贝）、`Subscriptions/Nodes/Aggregations`（新切片，元素是浅拷贝） | `RuleLibrary` 直接是快照切片；上面各记录的共享成员 |

`Fields`、`Raw`、`Map`、`Dynamic` 的 COW 在 R1 复核里已逐个读代码确认，所以在浅拷贝上 `Set/SetJSON/Clear`、`Fields.Set`、
`Dynamic.SetNode` 是安全的；`ProxyGroup` 的 `*[]string` 通过 `SetJSON` 替换指针，不改旧切片。

### 2. 调用点

自己 grep 出 70 处直接调用 + `substore.getShared`（函数值）+ `repo.Config` 包函数，和作者表一致。重点看的：

- **生成器**：`convert.MihomoConfig/SurgeConfig/LoonConfig`、`mosdns.Generate`、`oxidns.NewTypedProfile` 都是把模型投影成
  yaml 节点或新结构；`dialerScope`、`effectiveRule`、`ConfigShapes` 取元素指针但只读。全仓 `slices.Sort/Delete/Insert`、
  `sort.*`（convert/oxidns/subs 里 20 处）排的都是自己新建的切片或 yaml 节点。`subs.LowerChains` 先 `slices.Clone`
  组和节点，`appendGroupID` 新建切片再 `SetJSON`，`*group = *original` 写的是克隆切片里的元素。`provider.PrepareBundle`
  是 `owned := *config` 加 `append(slices.Clone(...))`。
- **resolveConfig 的 ruleset 水合**：第一次要写时 `slices.Clone(profile.RuleConfigs)`，之后只写克隆切片的元素；
  `hydratedRuleset` 返回新值。`ProxyGroups/RuleConfigs/Routers.Items/RuleLibrary` 的 `Clip` 见 L1（空操作，但无害）。
  `validateProfileIn` 借用正在校验的工作副本，只做只读的 dialer 检查，水合同样走克隆，`TestO10R2ValidationDoesNotModifyTheDocument` 固定。
- **嵌套赋值扫描**：`x.<列表>[i].Y =` 只有 `setup/source.go:109`（在 `updateShared` 事务里）、`resolve.go:472`（克隆后的
  切片）和 routers/discovery 自己的类型；嵌套指针成员赋值（`NodeTestDNSConfig.`、`EasyTier.`、`HealthCheck.` 等）没有；
  `deploy/adapter.go:87` 改的是 `SourceFromJSON` 解码出的自有副本。
- **append 扫描**：对 `Subscriptions/Nodes/Aggregations/RuleLibrary/ProxyGroups/RuleConfigs/…` 的 append 都在
  `UpdateShared` 等事务回调或自有类型上。
- **写入路径**：`SaveSystemConfig/SaveBackup` 的调用方用 `CloneSystemConfig/CloneBackup`，`SaveSharedRecord` 的调用方用
  `SharedConfig`（`CloneShared`）或自己解码的副本；`SaveProfileFieldsJSON` 只收 JSON。没有把借来的记录交给写入的地方。
- **被保留的借用值**：`discovery/ruleowners.go` 的匹配器缓存保留 `RuleConfigs/RuleLibrary`（`rules.Cache.signature`、
  `NewMatcher` 只读）；请求 Scope 缓存（L3）；notifications 只存摘要字符串。快照不可变，保留是安全的。

### 3. 写事务内自动退回深拷贝

`borrowsSnapshot(ctx)` 与 `viewDocument(ctx)` 用同一个 ctx 和同一个 `transactionOf`，判断一致：

- `Update`/`transact` 回调的 ctx 带 `txKey{r}` → `shared=false`，返回工作副本的深拷贝。
- 嵌套事务被 `transact` 以 `nestedWriteMessage` 拒绝，回调里读到的仍是外层工作副本 → 深拷贝。
- section updater：`editSystemView` 只在 `UpdateSystem` 的 `Update` 回调里调用，ctx 同时带 tx 和 systemView；
  `systemInView` 投影出的文档同样是工作副本 → 深拷贝。即使 systemView 单独出现（目前没有），也只会更保守。
- `ctxx.Detach`/`lifecycle.Detach` 用 `WithoutCancel` 保留值；`ctxx.WithParent(tx, ctx)` 之后再 Detach，`detachedContext.Value`
  先查 values（含 tx），所以回调里分离后的 ctx 仍被识别为事务内（`proxygroups/rulelibrary` 的 edit 回调就是这种写法）。
- `InTransaction` 分支（`rulefetch/publication.go:245`）：同一个 repository、同一个 ctx，拿到深拷贝；回调只读。
- `WithReadSnapshot`：`lockedSnapshot` 的 entry 是已提交代，不可变 → 借用正确。
- `ConfigFrom` 总是借用（不看 ctx）。唯一调用方 `api/nodes` 的 `PlanNodeDeletionRecords` 拿到的可能是写事务的工作副本，
  但只读 `view.Nodes[i].ID`，结果不出回调。文档注释已写明调用方须保持文档稳定、不得修改。

### 4. 并发

`TestO10R2BorrowedReadsRaceWithCommits`：8 个借用读各 4 个读者对结果 `model.Marshal`（遍历全部嵌套成员），写者循环 15 轮提交，
每轮同时改 Shared 节点、规则库、Profile 组、路由器、SystemConfig、Backup，并断言提交前借来的值编码不变、提交后的新读能看到新代。
加上 L1 的并发 append 测试。两者 `-race -count=10` 见“验证”。

### 5. configdiff 那 4 个 revision 用例

在 batch-23 上（.64）把 `profiles-real-concurrent-rules-settings-and-provider-URLs`、`profile-concurrent-metadata`、
`profile-concurrent-rebind` 三组连跑 3 次：差异数 4 / 2 / 4，同一棵树结果不同。全部差异都是两个同时提交的写者
generation 号对调（Python 6/Go 7 ↔ Python 7/Go 6；8↔9；5↔6），字段值、状态码、响应都一致。确认是写者先后顺序的时序抖动，
和 R2 无关。

## 修复提交

- `9159df4db` 整合：R2 门禁按 R1 方式统计所有引用（M1），含自测
- `df87380b0` 复核：固定快照切片 `cap == len` + 并发 append 测试（L1）；SystemConfig 负向对照不再空转（L2）
- 本报告与 behaviour-changes 单独提交

## 验证（最终树 `df87380b0`；报告提交只改文档）

```
.32  gofmt -l . → 无输出；/tmp/preflight.sh（go vet ./... + go vet -tags configflow_proddiff,configflow_difftest ./...
     + 完整非 race go test ./... -p 8）
     == vet / == full non-race tests (-p 8) / PREFLIGHT PASS
.32  go test -race -count=10 -run 'TestO10R2BorrowedReadsRaceWithCommits|TestO10R2ConcurrentAppendsToLentSlices' ./internal/repo/
     ok  configflow/internal/repo  155.271s
.32  go test -race -count=1 -p 4 -timeout 60m <50 个受影响包：repo、agents{,/dnsfollow,/dnsha,/ipv6,/metrics,/rulesync}、
     api/{agents,aggregations,clientdevices,config,generate,ipv6,linuxgateway,mosdns,nodes,oxidns,profiles,proxygroups,routers,
     rulelibrary,rules,settings,stats,subscriptions}、auth、clients、configgen、deploy、discovery、gateway/{draft,host,installation,
     onboarding}、health、mcp、mosdns、notifications、oxidns、refresh、routers、rulefetch、setup、substore、tasks、convert、subs、
     provider、httpx/...>
     race exit 0；50 ok，无 FAIL / DATA RACE
.64  scripts/gate-live.py --difftest（全部 CONFIGFLOW_DIFF difftest，一个 go test）
     DIFF PASS: 214 required entries ran and passed; 0 test skips
.64  configdiff 三组 ×3（见第 5 节）：差异只有并发写者 generation 号对调
```

pyref：preflight 通过即 golden 源哈希未变，无需重录。proddiff 未跑（门禁跑）。

## 性能（o10perf，仅供参考）

.64 同机先后两次：先 batch-23（`df87380b0`），后 `go@0c009f67e`（R1、R2 之前）；`--servers go --idle-minutes 4
--iterations 15 --route-profile-seconds 8`，取每路由 profile 阶段中位延迟和每请求平均分配/CPU。私有输出
`~/cf-prod-snapshot/runs/r2review-{new,base}-64`。**负载提示**：同期另一个 agent（R4）在 .64 上跑 o10perf，基线那次后半段
loadavg 升到 10，所以基线延迟/CPU 偏高（provider、oxidns、mosdns 尤其明显）；分配不受负载影响，是硬数据。
Python 列取 `o10-rootcause.md` run3（.64，同快照，不同日期）。batch-23 含 R1 + R2，Agent 路由的改善主要来自 R1。

| 路由 | go@0c009f67e | batch-23 | 分配变化 | CPU/次 | Python | Go/Py |
|---|---:|---:|---:|---:|---:|---:|
| GET /api/config/<p>/surge | 10.6 ms / 12.4 MB | **6.1 ms / 6.5 MB** | −47% | 18→8 ms | 11.2 ms | 0.54 |
| GET /api/config/<p>/loon | 18.9 ms / 12.1 MB | **6.1 ms / 6.5 MB** | −46% | 22→8 ms | ≈11 ms | 0.55 |
| GET /api/config/<p>/mosdns | 33.8 ms / 15.2 MB | **6.0 ms / 9.3 MB** | −39% | 31→10 ms | 15.6 ms | 0.38 |
| GET /api/config/<p>/oxidns | 68.1 ms / 33.5 MB | **16.4 ms / 16.2 MB** | −52% | 78→23 ms | 33 ms | 0.50 |
| GET /api/config/<p>/mihomo | 97.6 ms / 170 MB | 95.8 ms / 164 MB | −4% | 179→165 ms | 544 ms | 0.18 |
| GET …/aggregations/<agg>/provider | 688 ms / 247 MB | 188 ms / 196 MB | −21% | 516→211 ms | 868 ms | 0.22 |
| GET /api/agents | 305 ms / 223 MB | 163 ms / 174 MB | −22% | 410→238 ms | 35 ms | 4.7 |
| GET /api/agents?history=0 | 346 ms / 218 MB | 152 ms / 169 MB | −22% | 442→215 ms | 67 ms | 2.3 |
| GET /api/agents?summary=1 | 74.5 ms / 33.8 MB | 29.9 ms / 29.8 MB | −12% | 78→39 ms | 3.5 ms | 8.5 |
| GET /api/agents/<id> | 111 ms / 86.7 MB | 58.6 ms / 66.3 MB | −23% | 154→87 ms | 23 ms | 2.5 |
| GET /api/agents/<id>/rule-sync | 8.1 ms / 5.8 MB | **3.6 ms / 2.7 MB** | −53% | 11→5 ms | 10 ms | 0.36 |
| POST /api/agents/<id>/heartbeat | 2.9 ms / 2.4 MB | **1.4 ms / 0.4 MB** | −82% | 5→2 ms | 2.1 ms | 0.67 |
| GET /api/notifications | 30.1 ms / 19.7 MB | **16.6 ms / 15.8 MB** | −20% | 41→23 ms | 38 ms | 0.44 |
| GET /api/profiles | 12.7 ms / 11.4 MB | 7.8 ms / 11.4 MB | 0% | 19→12 ms | 1.3 ms | 6.0 |
| GET /api/routers | 17.2 ms / 15.4 MB | 9.6 ms / 10.6 MB | −32% | 26→16 ms | 6.2 ms | 1.5 |
| GET /api/routers/<id>/policy | 69.1 ms / 70.8 MB | **15.4 ms / 17.2 MB** | −76% | 106→23 ms | 12.8 ms | 1.2 |
| GET /api/routers/<id>/policy-settings | 75.7 ms / 77.3 MB | 27.7 ms / 29.4 MB | −62% | 117→42 ms | 16.1 ms | 1.7 |
| GET /api/routers/<id>/rule-sync | 16.1 ms / 15.0 MB | 9.5 ms / 9.1 MB | −39% | 25→14 ms | 6.5 ms | 1.5 |
| GET /api/agents/<id>/upstream-routers | 58.1 ms / 68.5 MB | 33.6 ms / 44.0 MB | −36% | 98→59 ms | 13–34 ms | 1.0–2.6 |

空闲（3.8 min）：CPU 平均 3.40% → 2.63%，10 s 峰值 23.4% → 26.0%，分配速率 1.55 → 1.23 GB/min，GC 后存活堆 30.5 → 30.5 MB，
RSS 结束 131 → 150 MB（同期负载不同，RSS 只作参考）。没有分配变多的路由。
