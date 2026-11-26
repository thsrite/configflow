# 复核 O17 W9 + W10（作者 Gemini Neon），含 W10a/b/c 三路合并

分支 `go-o17-w10`，基于 `go` `98a05aebd`。复核者 Claude Opus 5.5，2026-11-24。验证全部在 .64 上跑。

## 1. 合并方式

- 起点：W9 + W10a（`e40b553fb`、`2a6037d59`、`630307648`、`aa6e08e34`、`97ba762c3`）。
- 依次 cherry-pick `go-o17-w10b`（W9 之上 6 个提交）和 `go-o17-w10c`（W9 之上 6 个提交）。新提交号：
  W10b `154b641a6` `3b2174c95` `a49dfff36` `df3408f8f` `d5fb66538` `4d3e144c2`；
  W10c `bba25f9f8` `974a1c4ac` `cbc0d8902` `434519232` `7462e8618` `5da7ae68a`。
  `o17-wiring.md` W10 行里各段实施记录写的仍是原分支上的提交号。
- 冲突只在 `server/server.go`（`registerRoutes` 的签名和 `routePackages` 列表）、`server/app.go`（注册那一行）和 `o17-wiring.md`
  （W10 行）。三路改的是同一处，手工合成一份：每个 api 包都用显式构造的 Handler，各注册一次。`1a4830100` 又把三组 Handler
  收进同一个 `apiHandlers`，`tasks`/`notifications` 也放进同一个有序列表的末尾（和 go 上的位置一样）。
- `b24b6fa15` 加了 `TestProductionRouteTableMatchesGo`：枚举生产 mux（`server.New` 带仓库）的全部 method + path + endpoint，
  和 `testdata/routes_go.tsv` 逐行按顺序比对（共 301 行）。这份清单是在 `go` 上用未提交的临时探针（`git archive go`）导出的。

## 2. 发现

### 高

**H1（W9，已修 `f49f11acc`）：setup 完成检查在网关模式（非 full-TUN）选了规则集时返回 ErrNotPorted。**
在 go 上，`setup/completion.go` 的非 full-TUN 分支给 `ComputeDesired` 传 nil 加载器，nil 时会走服务端接好的全局
`spec.RulesetContent`（`rulesource.ReadOfflineJSON`），离线读取所选规则集。W9 删掉了这个全局，并把 nil 加载器改成
fail closed（`device.ErrNotPorted`），但没给这个调用方补加载器。结果是：只要 Linux 网关设置里选了规则集，向导第 4 步
「检查」就报错。修复：`setup.ExternalOwners` 新增 `ReadOfflineRouteContent`，由 server 的 `wireSetupContentWith`
接到同一个 `rs.ReadOfflineJSON`，完成检查用它构造加载器；没接时仍 fail closed。
新增测试 `setup.TestGatewayCheckReadsSelectedRuleSetsOffline`：先断言没接 owner 时返回 ErrNotPorted，再断言接上后
检查成功，并且读到了所选规则集和已发布的配置快照。另外两处生产 owner 断言也加上了这个字段。
其余 `ComputeDesired` 调用方都已核对：`routers.Inspect`（经 `Service` 或请求里的 `Rules` 填）、`rospolicy`（用 Inspect 的
同一个加载器）、`rulesync`（`syncIPLoader`）、`pending.go`（透传 `Rules`），都会传非 nil 的加载器。

### 中

**M1（W10c，已修 `1a4830100`）：生产里 `settings.Handler.WebDAV` 是 nil，每次请求都走 `webdav.Default()`。**
`newW10cHandlers` 传的是 `apisettings.NewHandler(opts.Repo, gen.substore, nil)`。现在改成在组装时注入
`webdav.Default()`：webdav 还没实例化，它的实例化留给 W11。`webdav.Capture`/`webdav.Run` 这两个包级函数也留给 W11。

**M2（W9，已补断言 `367206254`）：生产组装测试没覆盖 `routers.Options.Notifications`、`routers.Service` 的仓库、
`RuleSyncService.Repo/Agents`。** 这几项为 nil 时会退回进程级通知定位器，或者每次调用都走 `agents.Get`。当前生产路径都有值。
新增 `HasNotifications`/`HasRepository` 两个方法，并在 `TestProductionAssemblyLeavesNoDependencyUnset` 里加了 4 条断言。

### 低（记录，不改）

- L1：`httpx` 的响应清洗（`redact.ExternalPayloadSecrets`）、`profileContext` 中间件、`auth.ValidateSystemTokenOrJWT`、
  `repo.ResolveProfileID`/`repo.Config` 这类包级 scope 函数仍然经 `repo.GetRepository` 取进程仓库。生产里这就是注入的
  同一个仓库，属于 W11 范围。所以「把定位器改成 panic」做不到：任何 JSON 响应都会触发。复核里改用两项测试：反射断言
  Handler 依赖非 nil，再加全局 GET 的「双仓库差分」（见第 3 节）。
- L2：`api/rules.ReadRulesetContent`、`api/tasks.updateInterval`、`api/agents.WireContentSyncOn` 在生产里没有调用方，
  仍直接用 `*.Default()`。W11 删除。
- L3：`api/logs.Handler.{LogFilePath,OpenFile}`、`api/stats.Handler.Now` 是新加的导出测试接缝，nil 时退回包级变量，等于
  一个接缝分成了两层。`api/rules`、`api/rulelibrary` 的 `pinnedCheck`/`replaceCache` 是原有接缝。W11 删测试接缝时一并收掉。
- L4：订阅源抓取从 `provider.DefaultDeliveryEnv().Fetch` 改成了 `provider.Service.DeliveryEnv().Fetch`。生产里两者相同，
  只有测试替换 `SetDeliveryEnv` 时行为不同（新写法更一致）。
- L5：`apisetup.NewHandler` 在组装时对 `setup.RegisteredOwners()` 取快照。所有 owner 都在 `build` 里、Handler 构造之前
  注册，`cmd` 里没有之后的注册，所以没有问题。以后如果在 `New` 之后注册 owner，Handler 看不到。

## 3. 按任务逐项结论

1. **生产路径不走定位器**（`1a4830100`）
   - `TestProductionHandlersHaveNoNilDependencies`：在 `server.New` 之后，用反射遍历挂载的全部 Handler，包括子 Handler
     和 Deps 结构体，断言没有 nil 的指针、接口或函数字段。只有一份带理由的白名单：4 个测试接缝（nil 就是生产行为），
     加上 D5 的 web listener（`cmd` 在 `New` 之后才设置）。由于这些字段在生产里都非 nil，「字段为 nil 时回退到定位器」的
     辅助逻辑不会走到。逐个读过各包的回退辅助函数：都只在字段为 nil 时才回退。
   - `TestProductionGlobalRoutesDoNotUseRepositoryLocator`：对每个没有路径参数的全局 GET 路由请求两遍。第一遍进程仓库就是
     App 的仓库（和 `cmd` 一样）。第二遍进程仓库换成另一个空仓库，Agent manager 单例置空。App 的仓库预先放了别处没有的
     profile、节点和规则库条目，比对时把时间戳归一化，要求两遍响应完全一致，并断言这三样数据确实出现在响应里。
     变异验证：把 `apiprofiles` 的 Repo 去掉后，上面两项测试都会失败。
   - 已有的 `TestW10bHandlersUseInjectedDependencies`、W10a/W10c 的注入测试都保留。
2. **鉴权**：`internal/auth`、`internal/httpx` 相对 go 没有改动；`auth.Install` 仍插在 before 链的第 0 位。各 api 包里
   `auth.Require` 和 `PreservePlainErrors` 包装的总数，go 和 HEAD 一样，都是 163。endpoint 名由路由表测试逐条锁定。
   `81bd7628e` 加了 `TestProductionAuthGateMatchesGo`：开启登录后，不带凭据请求全部路由，不返回 401 的路由集合与 go 上用
   探针导出的 `testdata/auth_open_go.txt` 一致（公共 endpoint/路径，以及自带 token 校验的路由）。其余受保护的路由全部是 401。
3. **W9**：
   - `routers.New` 的依赖为 nil 时：`IPv6Hosts` → `messageIPv6HostUnavailable`；`Rules` → 选了规则集时 `ErrNotPorted`；
     `ManualBaseline` → `ErrNotPorted`，记为基线捕获警告；`Notifications` → 退回进程级通知（过渡，生产已断言非 nil）。
     作者的 `TestServiceNilCollaboratorsFailClosed`、`TestComputeDesiredNilLoaderFailsClosed` 覆盖了前三项。
   - `router-rule-sync` 循环只由 `server/routers.go` 注册一次；`tasks.SlotRouters` 的行构建器和运行器只在同一处设置一次
     （全仓 grep 没有第二处）。`TestNewRegistersEachLifecycleTaskOnce` 断言名字和数量都和 go 一致，没有原地替换。
   - `RuleSyncService` 把 `ruleSyncLoop` 收进实例的 `pacer` 后，删掉了 `RunRound` 里的 `*s` 拷贝（拷贝会复制 pacer），
     这是对的。`CaptureManual` 在 `target != s` 时补发 `s.pacer.wake`；`target == s` 时 `Capture` 自己会唤醒 s。
4. **diff 审查**：把 W10 的 10 个 api 提交按「加接收者 / 定位器换成 `h.xxx` / 函数改为方法」归一化后，剩下的删除行逐行看过
   （脚本只在本地临时使用）。只有上面 L4 一处有实际差异；`(h *HTTPError)` 改名为 `httpErr` 是为了避开接收者名冲突。
   W9 的逻辑提交逐行审过（`service.go`、`rule_sync*.go`、`inspect/publish/pending/management.go`、`server/routers.go`、
   `server/setup.go`），H1 就是在这里发现的。D5（`settings.SetWebListener` 两阶段）和 go 一致：设置之前读到的是同一个包级
   指针，go 上这时也是 nil；没有前端时显式设为 nil；设置后才 `MarkStarted`。

## 4. 功能差异

修复 H1 之后没有对外的功能差异。H1 本身是 W9 引入的回归（go 上正常），不是 Python 的 bug。

## 5. 验证（.64，HEAD 见交回消息）

```
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -l . ; go vet ./... && go vet -tags configflow_proddiff,configflow_difftest ./... && echo VET-OK;
  go test -race -count=1 -p 6 ./internal/api/... ./internal/server/... ./internal/routers/... ./internal/setup/... ./cmd/...'
  → gofmt 无输出，VET-OK，-race 全部 ok（RACE-EXIT=0）
CONFIGFLOW_DIFF=1 （真实 Python）go test $(go list ./internal/api/... | grep /difftest)
  → agents 260.8s、aggregations、config、gatewayonboarding、linuxgateway、oxidns、rulelibrary、rules、subscriptions 全部 ok
PYREF_MODE=check go test ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/routersplit → 4 个 ok
GATE_ONLY=proddiff server/scripts/gate.sh -p 6 → cases 2831, baseline: new=0 stale=0, self_check PASS, GATE STAGE PASS: proddiff
/tmp/preflight.sh .paseo__worktrees__302rx1dg__go-o17-w10 -p 6 → PREFLIGHT PASS
```
