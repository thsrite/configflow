# O10-R1 HR 复核：Agent 读 API 借用不可变快照（2026-11-21）

复核对象：`go-perf-r1` = `go@0c009f67e` 之上 `8c15c512e..9c02665c0`（5 个提交）。复核分支 `go-review-r1`。
方法：通读 `model/record.go`、`fields.go`、`map.go`、`dynamic.go`、`member.go`，`repo/agents_borrow.go`、`view_guard.go`、
`transaction.go`、`agents/registry.go`、`ha_model.go`、dnsha/dnsfollow/rulesync 适配器；**自己 grep 全部调用点**（没有照抄 81 条表），
外加三类全仓扫描：经嵌套指针的赋值、对嵌套 Map 的 `Set/SetJSON/Clear`、对嵌套切片的 `append`/下标赋值。

## 结论

S1 的借用语义成立，没有发现高危问题。作者修好的 dnsha journal 是唯一一处真实写穿，同类写法已排查，没有第二处。
发现 2 个中危问题（都在测试安全网里，不影响生产行为），已修；另有 3 条低危或说明。

## 中

### M1 guard 字段有数据竞争，`-race` 下会偶发失败（已修）

`Repository.viewGuard` 是普通 `func()` 字段。`guardBorrow`、`View`、`visitAgents` 在锁外读它，`SetViewGuard` 在 `guardMu` 下写它。
9 个分配测试在 `Open`/`Init` 之后调用 `SetViewGuard(nil)`，这时后台循环（规则同步 worker 等）可能已经在借用。这是真实的读写竞争，
在 `-race` 门禁里会表现成和被测代码无关的偶发失败。生产不受影响：生产从不设置这个字段。

修复：改成 `atomic.Pointer[func()]`，统一用 `currentViewGuard()` 读取。repo 包内测试原来直接给字段赋值的 5 处改为 `SetViewGuard`。
生产路径多一次原子 Load（nil）就返回，没有锁，也没有编码。新增 `TestViewGuardReplacedWhileBorrowing`（2 个借用 goroutine，同时切换 guard 50 次）。

### M2 源码门禁 `TestAgentDeepCopyBoundary` 能被函数值绕过（已修）

`deepCopySites` 只匹配 `CallExpr` 里的 `copyAgent`/`CloneAgent`。下面三种写法都能绕过：
`clone := agents.CloneAgent; clone(r)`（函数值）、`var dup = copyAgent`（包级别名，不在任何 FuncDecl 里，根本没被遍历）、
借用方法值。闭包倒是被覆盖了（按外层函数记）。

修复：改为匹配**任何**对这两个名字的标识符引用（包括 selector 的 `Sel`），包级声明记为 `file:<package-level>`。
自测加了 3 个用例：函数值、闭包、包级别名。现有树上的站点集合不变，4 个豁免都还在用，没有过期豁免。

## 低 / 说明

- **L1 门禁只管两个具名函数。** 有人自己写 `model.Marshal` + `json.Unmarshal` 拷贝整条 Agent，门禁看不到（dnsha `ownJournal`
  就是这种写法，只拷 journal，合理）。这是门禁的设计范围：它防止"拷贝 API 重新变成默认"，不防所有深拷贝。不改，只记录。
- **L2 生产代码 import `testing`。** `repo/view_guard.go` 用 `testing.Testing()` 判断是否装默认 guard。`repo` 是第一个 import
  `testing` 的非测试包。Go 1.13 起 `testing` 不在 init 时注册 flag，生产二进制只是体积略大，运行时行为不变。
  要让**其他包**的测试二进制也默认带 guard，这是唯一不需要构建标签的办法。接受。
- **L3 `ReadDeploymentModel` 等返回的 `*model.Deployment` 也是借用的。** 它是 `Map.Get` 得到的结构体浅拷贝，`SetJSON/Clear`
  是 COW，但 `Events`、`PreparedContent`、`HAPublication` 跟快照共享。这次检查的调用方（`HAAggregateModel` 先 `copyDeployment`、
  dnsha `aggregate`、API 只编码）都不改它们。建议在 "Borrowed records" 注释里写明：从借来的记录取出的 Map 值同样适用借用规则。
  只改文档，留给 R3/R4 一起做。

## 逐项核对

### 1. 浅拷贝后哪些成员会写穿

`model.Agent` 的顶层成员分三类：

| 类型 | 成员 | 浅拷贝后 |
|---|---|---|
| 值（string、`Int`） | ID、Name、Host、Port、Status、LastHeartbeat、… | 调用方自己的，可改 |
| 指针 | LoggingConfig、Enabled、UpdatedAt、ConfigRevision、PushedAt、*InstallationID、DomainDiscoveryEnabled、Traffic*、LatestDeployment、AppliedContent、LatestUpgrade、RuleSync、DNSFollow{,State,Job}、OxiDNSHA{,State,Publication}、IPv6VerificationSettings、ServiceStatus、SystemMetrics、CPU/Memory/Disk/Network | **共享**。重新赋值指针可以；`*p = …`、`p.X = …` 会写穿 |
| `Map[V]` | Deployments、IPv6Verifications、NodeTestDNSEndpoints | 在自己的浅拷贝上 `Set/SetAll/Delete` 是 COW；`Get` 返回的 V 是值拷贝，但 V 内部的指针和切片共享 |
| `Fields`、`raw Raw` | — | COW（见下） |

嵌套类型里需要特别注意的：`AgentRuleSync.Settings` 是值字段，但它挂在指针 `RuleSync` 下面，所以 `a.RuleSync.Settings.X = …` 同样写穿；
`HAJournal` 的 `Order/Warnings/Routers/Members` 是切片，`Children/Identities` 是 Map；`Deployment.Events/DNSChecks` 是切片，
`Health`、`AgentLoggingConfig.Enabled` 是 `json.RawMessage`；`LegacyDNSEndpoint.Plugins []Dynamic`。

model 层的 COW 声明逐个读代码确认成立：

- `Raw`：`ClearMember`、`SetMemberJSON` 都先 `slices.Clone(raw.members)`，再给 `issues` 新建切片，最后整体替换，从不原地改；
  `EncodeRecord`、`Raw.MemberJSON` 只读，返回 `bytes.Clone` 或新编码。
- `Fields`：`Set`/`set`/`Delete` 都走 `copyState()`（克隆 entries）再替换指针；`Get`/`All` 返回 `bytes.Clone`。
- `Map`：`Set`/`SetAll`/`Delete` 走 `copyState()`；`MemberJSON` 返回新编码或 `bytes.Clone`。`Get`/`All` 按值返回 V（见上表）。
- `Dynamic`：`UnmarshalJSON` 克隆输入，`MarshalJSON` 返回克隆，`SetNode` 整体替换 `raw`。
- `SetMemberJSON` 用 `reflect.New` 解码到新值再赋给字段，所以在浅拷贝上 `SetJSON("oxidns_ha", …)` 换的是指针，不会改到旧的 HASettings。
  心跳 overlay（`overlayModels`）就是走这条路。

### 2. 调用点

自己 grep 出 81 个非测试调用，和表一致。另外三类全仓扫描的结果：

- 经嵌套指针的赋值（`x.<指针成员>.Y = …`）：只有 dnsfollow `service.go:168/239`、`publish.go:456`，dnsha `configure.go:276`，
  `deploy/adapter.go:327`。全部在 `EditAgents` 回调里，作用于事务工作副本。
- 写事务工作副本是从行数据**重新解码**的（`transaction.go:128`），提交后缓存也是从提交结果**重新解码**的（`commitStore` → `decodeEntry`），
  所以工作副本永远不会变成已发布的代。唯一的风险是把借来的嵌套指针放进工作副本后再原地改。dnsha 的 `save/fail/step/recovery/*Handoff`
  会原地改 journal，我把 journal 的所有来源都查了一遍：`advance`、`ActivatePair` 已有 `ownJournal`；`reserve` 返回新建的 journal；
  `configure.go:76`、`hooks.go:68/186/214/350/413`、`publication.go:437/521`、`deployment()`、`Reconcile` 都只读
  （`summary`、`keys`、`journalChildID`、`rulesResult`）。没有第二处。
- 嵌套切片 `append`/下标赋值（Events、Members、Order、Warnings、Routers、DNSChecks、Plugins）：只出现在工作副本或新建对象上。
- 被长期保存的借用值：`tasks.CacheAgents`（只在一次请求的 ctx 内）、notifications、monitor cache，都只读。
- S 类（rulesync `workerAgent`、dnsfollow `modelAgentWithProfile`、`ListAgents`、ipv6/dnsha 改 `Status`）只改顶层字段，
  `Enabled` 换成新指针。核对无误。

### 3. guard

- 生产：`testViewGuard()` 在非测试二进制里返回 nil。修复后 `guardBorrow`、`View`、`visitAgents` 第一步就是原子 Load，拿到 nil 立即返回，
  不编码。
- 覆盖面：`BorrowAgents` 是唯一的借出口。`ViewAgents` 调用它，`ReadAgent*`/`ListAgents`/`AgentSnapshots`/`ReadHAPair`/`AgentPair`
  也都经过它。`View`（整份文档）保留原来回调前后比较的做法。`repo` 里其他读 `System.Agents` 的地方（`resolve.go` 的 `copyValue`/
  `copyOxiDNSAgents`、`system.go` 的 `gather`）仍然是拷贝，不属于借用。
- key 用 `&records[0]`。guard 自己持有这个切片，被记住的代不会被 GC 回收、地址也不会被复用；代被淘汰时会再校验一次。
- 负向对照 `TestViewGuardCatchesBorrowedMutation` 的 4 种篡改（嵌套指针、指针标量、Map 值里的指针、嵌套切片元素）都会触发 guard，
  默认 guard 也确实 panic。我抽查后确认它不是空转：每个子测试在篡改前先断言 `CheckBorrowed()==nil`。

### 4. 源码门禁

见 M2。4 个豁免的理由都成立：`readerAgent` 只在 `shared=false`（事务工作副本还可能变）时深拷贝；`CloneAgent` 是显式 API；
`setAgentSwitch`、`DeleteAgent` 把结果从写事务的工作副本里拷出来，因为工作副本在回调结束后不再保证可用。

### 5. 并发

`TestBorrowedReadsSurviveConcurrentCommits` 的读者对整个结果做 `model.Marshal`，遍历了所有嵌套成员；同时一个写者循环 25 次提交
（顶层、嵌套指针、Map 条目）并发送心跳。`ReadAgentNoHistory`、`ListAgents(false)` 让多个 goroutine 同时在共享 Raw 上走
`Clear` 的 COW；心跳 overlay 让多个 goroutine 同时 `SetJSON`。这正是 COW 出错时 race 检测器能抓到的位置。

head 切换：`setCache` 只替换 `documentCache` 指针，旧代的切片仍被借用者引用，不会被改动。`lockedSnapshot` 和 `agentsCache`
也一样。持有旧代是安全的。

### 6. 行为

只改了读路径的拷贝方式和测试安全网，路由、JSON、存储、Agent 协议都没变。抽查了 Flask difftest：`api/agents/difftest`
和 dnsha difftest，结果见下文“验证”。

## 修复提交

- `f0791cb77` M1：guard 改为原子读写，加回归测试 `TestViewGuardReplacedWhileBorrowing`
- `0147a1755` M2：门禁统计所有引用，加自测用例（函数值、闭包、包级别名）
- 本报告单独提交

## 验证（全部在 .32）

```
gofmt -l internal/repo internal/agents → 无输出（GOFMT-OK）
go vet ./internal/{repo,agents,routers,notifications,configgen,substore}/... → VET-OK
go test -race -count=1 ./internal/repo/
  ok  configflow/internal/repo  344.180s
go test -race -count=3 -run 'TestBorrowed|TestViewGuard|TestAgentDeepCopy' ./internal/agents/
  ok  configflow/internal/agents  261.668s
```

M1 负向对照：把 `repo/{repository,view_guard,read,agents_view}.go` 临时换回 `9c02665c0` 的版本，只保留新测试：

```
go test -race -run TestViewGuardReplacedWhileBorrowing ./internal/agents/
  WARNING: DATA RACE
  --- FAIL: TestViewGuardReplacedWhileBorrowing (0.43s)
```

跑完已恢复。M2 的新自测用例在旧的只看 CallExpr 的扫描下必然失败：`agents.CloneAgent` 不在调用位置，`var dup = copyAgent` 不在任何 FuncDecl 里。

Flask difftest 抽查（`CONFIGFLOW_DIFF=1`，`-tags configflow_difftest`，没有 SKIP）：

```
--- PASS: TestContentReadsAgainstFlask (3.07s)
--- PASS: TestDifferentialAgainstFlask (9.05s)
--- PASS: TestRuleSyncHTTPAgainstFlask (2.75s)
ok  configflow/internal/api/agents/difftest  14.884s
ok  configflow/internal/agents/dnsha/difftest  21.939s
```
