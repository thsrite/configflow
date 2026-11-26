# 复核 O17 W11（作者 Gemini Neon）：删除全部服务定位器与过渡层 + W9/W10 留项 L1–L5

分支 `review-w11`（= `go-o17-w11@c3b5733dc`，基于 `go-o17-w10@d7089f06d`，24 个提交）。复核者 Claude Opus 5.5，2026-11-24。
只追加提交，不变基。验证全部在 .64 上跑。

## 1. 结论

W11 把定位器换成显式依赖的方向是对的：鉴权 token 校验、httpx 响应清洗、profile 中间件、导出脱敏（`redact.*`）、WebDAV 订阅、
任务/通知、Agent Manager、规则同步唤醒，在生产组装里拿到的都是同一个实例。但有两类问题：

1. 原来「定位器回退到进程默认实例」的地方，有几处改成了「就地新建一个空实例」，或者在 nil 时直接报错，生产里功能坏掉。
   复核发现 3 处（H1、H2、H3）。
2. 原来 `server.NewApp()` 会注册进程级 repo hooks（导出脱敏器、路由器作业锁、默认 server_domain）。夹具改成
   `server.New` 后没有用 `server.RepoOptions()` 打开仓库，于是失去了这些协作者。另有 3 个 difftest 夹具是自己拼的服务，
   少接了依赖。生产不受影响，但差分测试是红的。

全部修复后，所有 difftest 包（真 Python）、两种 tag 的 vet、受影响包的 `-race` 都通过（第 5 节）。

## 2. 发现

### 高（生产回归，已修）

**H1 Agent 内容同步比对用了空的 deploy 实例**（`5f413b0f3`）。
`api/agents/content_wire.go` 的 `prepareContent` 在 W11 从 `deploy.PrepareContentBundle`（走 `deploy.Default()`，
里面是 server 接好的 Sub-Store 和 provider）改成了 `deploy.New(deploy.Options{})`。新实例的 substore 是 nil，所以
`PreferCached` 拉订阅时报错「deploy: substore service is nil」；provider 也是现建的，没有聚合渲染器。
影响：Agent 卡片上的 `content_sync` 状态、推送差异、条目查询，以及 IPv6 服务用到的 `ContentChecker`。只要配置里有订阅或
聚合 provider，比对就会失败。
修复：`NewHandler` 按 Manager 把注入的 `Deps.Deploy` 绑定到内容池（Manager 关闭时解绑）。没有绑定的 Manager
fail closed，返回 `errContentDeployUnbound`。
测试：`TestHandlerUsesInjectedDeployForContentAndInstallation`（绑定的是同一个实例，关闭后解绑），
`TestPrepareContentRefusesUnboundManager`（反向对照）。

**H2 网关安装向导的发布用了空的 deploy 实例**（`5f413b0f3`）。
`installation.Service.Deploy` 是 W11 新加的字段，但生产里 4 处构造都没有填，所以 `deployService()` 每次都新建
`deploy.New(Options{})`。结果：HA 成对目标报 `ErrPairPublisherMissing`；发布后不做 IPv6 登记；provider 没有聚合渲染器；
provider 缓存写入走的是包级 `substore.SaveCache`，不再通知 Sub-Store 的 content-change 订阅者。W11 之前走的是
`deploy.Default()`，这些都正常。
修复：`api/agents` 的安装处理改用 `Deps.installationService`，带上注入的 deploy。provider 缓存提交改走新增的
`(*deploy.Service).CommitArtifacts`，和普通部署共用 provider 和 Sub-Store。
`server/setup.go` 和注册路径（`agents.go`）里构造的 `installation.Service` 不会发布，所以不填 Deploy。

**H3 MosDNS rule-proxy 对未声明的 URL 一律返回 400；规则库手动刷新 URL 源失败**（`91f7be195`）。
`rulefetch.Download` 原来在调用方没给 `Config` 时，从 ctx 里的仓库读全局抓取设置。W11 改成显式的 `DownloadRequest.Repo`，
但有两个调用方两者都没传：
- `mosdns` rule-proxy 的未声明 URL 分支：`errNilRepository` 被归为 400「Invalid remote URL」，根本没有下载。
  这是协调者门禁之外、全量 difftest 抓到的：`TestDifferentialMosDNSRoutes` 和 Flask 有 5 处差异；在 W10 基线 `d7089f06d` 上是通过的。
- `api/rulelibrary` 手动刷新 URL 规则时下载不带 Repo，同样会失败。
修复：rule-proxy 改为传入已解析配置的全局抓取设置（和 rulefetch 原来从仓库读的三个字段相同，不带规则目录）；
规则库刷新传 `Repo`。新增测试 `TestRuleProxyUndeclaredURLDownloads`。其余 `Download` 调用方都已核对，都有非 nil 的 Config。

### 中（已修）

**M1 生产 web listener 的 Serve 任务不再受 Runtime 管理**（`5f413b0f3`）。
`cmd` 构造 `weblistener.Manager` 时没有传 Spawner，`Listen(work)` 用的又是没有绑定 Runtime 的 ctx。改之前，
`DefaultSpawner` 会把它交给进程 Runtime：关停时取消，Stop 之后拒绝准入。W11 之后变成 `ContextSpawner` 自起的 goroutine。
修复：`cmd` 传入 `Spawner: rt`。

**M2 `server.New` 接受缺少导出脱敏器/路由器作业锁的仓库；夹具因此丢失协作者**（`e4fbddcdd`）。
这正是协调者门禁里 `TestN811GlobalFlaskBoundaries` 失败的原因：`desensitize=true` 时 Go 返回 400，Flask 返回 200。
生产没问题：`cmd` 用 `repo.OpenDefaultWithOptions(dataDir, server.RepoOptions())` 打开仓库，其中有 `Export`、`RouterJobLock`
和 `DefaultServerDomain`。问题在于 W11 之前，`NewApp()` 会把这三项注册成进程级 repo hooks；夹具和
`tools/proddiff/{generator,baseline}` 改成 `server.New` 之后，打开仓库时没有带这些 Options。
修复：`repo.(*Repository).RequireServerCollaborators` 检查这两项，`server.New` 遇到缺少的仓库直接拒绝（fail closed，不放宽）。
全部 `server.New` 调用方（30 个测试/difftest 文件和两个 proddiff 工具）都改用 `server.RepoOptions()`。
测试 `TestNewRequiresServerRepositoryOptions`：裸仓库被拒绝，生产 Options 的三项都不是 nil。

**M3 `go vet -tags configflow_difftest` 编译失败**（`e4fbddcdd`）。
`cmd/configflow/faults_difftest_test.go` 里打开的仓库变量没有用到：W11 去掉 `WithRepository` 后，没有把仓库接到
`app.Repo`。现在补上了；没有仓库时 JSON 出口会 fail closed。作者跑 vet 时只用了组合 tag
（`proddiff,difftest`），而这个文件是 `difftest && !proddiff`，所以没被编译到。现在四种 tag 组合都已通过。

**M4 三个 difftest 夹具自己拼服务，少接了依赖**（`a263d52cd`；生产不受影响，W10 基线上都通过）：
- `discovery`：路由没有 Agent manager 和流量库，GET agents/domains 返回 500。现在夹具直接用 `server.New`。
- `subscriptions`：Handler 没有 refresh 和通知服务，fetch 失败时不写 `notifications.json`。现在接上夹具自己的这两个服务
  （生产的 `apisubscriptions.NewHandler` 传的是 `gen.refresh`、`nt`）。
- `aggregations`：GENERATE 用的是只带 Repo 的新 `generation.Service`，没有 provider，诊断信息里缺少位置。现在用路由所用的服务。

### 低

- **L1（已修）** `webdav`：`webdav-worker-init` 之前的提交（比如加载配置时的迁移）会用 `ContextSpawner` 起一个裸
  goroutine，激活后才停止。现在 `Register` 把 Runtime 交给预启动的 worker。测试 `TestRegisterBindsPreStartupWorkerToRuntime`。
- **L2（已修）** `httpx.RegisterLocal` 在 Runtime 为 nil 时，会把 typed-nil 的 `*Runtime` 存进接口，`localhttp` 的
  `spawner == nil` 检查拦不住，之后会对 nil 解引用。现在显式存 nil，回调返回 `ErrMissingSpawner`。生产里 Runtime 始终不是 nil。
- **L3（记录）** 仍有不少「字段为 nil 就现建实例」的回退：`Deps.deployService`、`dnsHAService`、`dnsFollowService`、
  `api/subscriptions` 的 substore/refresh/provider、`aggregations.provider`、`mihomoenv` 在 pv 为 nil 时、`installation.deployService`
  等。生产路径上这些字段都由 `TestProductionHandlersHaveNoNilDependencies`（反射）和 H1/H2 的新测试保证不是 nil。
  建议下一轮把它们改成 nil 时 fail closed，这样夹具漏接会直接报错，而不是像 M4 那样悄悄改变行为。
- **L4（记录）** 仍有进程级状态：`api/agents` 的 `contentChecks`/`SetContentPrepare`（按 Manager 分键，只给测试替换）、
  `localhttp.application`、`health.currentProbeEnv`（`init` 里装默认协作者，`SetProbeEnv` 只给测试用）、`agents.ResetRuntime`
  的 states 表，以及约 30 个包级 `var x = func` 测试接缝（now/random/download 一类，都显式接收 repo，不是定位器）。
  设计第 2.2-8 条要求这些改成服务字段，W11 没有做完。没有发现 `sync.Once` 订阅全局实例的残留。
  `init()` 共有 6 个：词表/schema/默认密钥/路由器夹具/共享拨号器/health 默认协作者，都不是注册服务实例。
- **L5（记录）** `lifecycle.ContextSpawner` 在 ctx 没有绑定 Runtime 时自起 goroutine。逐个核对了生产里的入口：HTTP 请求
  （`App.ServeHTTP` 绑定 Runtime）、启动步骤/循环/任务（generation ctx）、`localhttp`（spawner 是 `a.Runtime`）都已绑定。
  只有 M1 和 L1 两处会走到无 Runtime 的分支，都已改成注入 rt。保留这个分支是为了单元测试，不建议改成报错：
  约 80 处测试直接用 `context.Background()` 调用会派生任务的代码。

## 3. 按任务逐项

1. **生产行为不变 / 同一实例**：auth（4 个调用点都传 `h.Repo`）、httpx（`App.Repo`、`App.Runtime`）、`redact.*`（都显式接收 repo）、
   `configexport.Sanitize`、WebDAV（`wd.Repo`/`Tasks`/`Notifications`，`Subscribe(opts.Repo)`）、tasks/notifications
   （`tk`/`nt` 各一份，各服务的 `TaskStatus` 都经同一个 `nt`）、Agent Manager（`ag.manager`）、规则同步唤醒
   （`gen.refresh.OnRulesRefreshed(rsync.WakeLoop)`），这些都已逐个核对。不一致的就是 H1、H2、H3。
   对比方法：把 `d7089f06d` 上全部非测试代码里的 `Default()` 调用点和 HEAD 的对应位置逐一比对；
   另外 grep 生产代码里的 `New(...Options{})` / 只带 Repo 的构造。
2. **fail closed**：新补的反向测试：`redact` 的 7 个入口遇到 nil 仓库返回 `errNilRepository`（表驱动）；
   `auth.ValidateSystemTokenOrJWT` 遇到 nil 仓库拒绝；App 没有 Repository 时返回 5xx 且不泄露载荷；
   `server.New` 拒绝裸仓库；内容比对拒绝没有绑定 deploy 的 Manager。deploy 的 Pair 为 nil 时返回 `ErrPairPublisherMissing`
   （已有测试）。
3. **ContextSpawner**：见 L5、M1、L1。
4. **测试辅助**：删掉 `NewApp()` 后，测试用的是 `server.New`，但 M2 那批没有用 `RepoOptions`，M4 那三个夹具绕开了组装。
   M4 的修复按同一原则处理：能用生产组装就用生产组装，不能用的话，接上和生产相同的协作者。
5. **grep 审计**（HEAD）：`repo.(Get|Set|With)Repository|RegisterHooks|SetHooks` 只出现在注释里；
   `agents.Get(`、`*.Default()`、`UseService/SetDefault/UseRegistry/UseManager/ForManager/GetNative/Get*Store` 在生产代码里 0 处；
   `lifecycle` 的全局注册函数 0 处（只在两个工具的注释里）；api 包级的无参 `Register(r)` 0 处；`TaskStatus(ctx, dir)` 都是
   `nt` 的方法。另外三类残留见 L4。
6. **pyref 提交**（`ce4a17758`）：13 个黄金文件逐个解压，比对 `entries`/`version`：完全相同，只有 `metadata` 变了。

## 4. 功能差异

修复后没有对外功能差异。H1、H2、H3、M1 都是 W11 引入的回归，在 W10 基线上正常，修复恢复到 W11 之前的语义。
M2 的 `server.New` 前置校验是新增的 fail closed，生产 `cmd` 本来就满足。

## 5. 验证（.64）

提交：`5f413b0f3`、`e4fbddcdd`、`91f7be195`、`a263d52cd`（本文件随后一个提交）。

```
go vet（无 tag / configflow_difftest / configflow_proddiff / proddiff,difftest）→ 四种都 OK；gofmt -l 无输出
CONFIGFLOW_DIFF=1 CONFIGFLOW_PY=~/cf-test/.venv/bin/python go test $(go list ./... | grep /difftest)
  → 修复前：aggregations、subscriptions、discovery、mosdns 4 个包 FAIL（在 d7089f06d 上都 ok）
  → 修复后：这 4 个包 ok；其余 22 个包第一轮就是 ok（agents/dnsfollow、dnsha、api/agents 269s、config、gatewayonboarding、
    linuxgateway、oxidns、rulelibrary、rules、devicenotes、gateway/installation、health、httpx、mcp、provider、refresh、subs、
    substore、tasks、webdav）
CONFIGFLOW_DIFF=1 … go test -run TestN811GlobalFlaskBoundaries ./internal/repo/ → ownership-conflict=false/true 都 PASS
```
（`-race`、pyref check、proddiff gate、preflight 的结果见交回消息末尾。）

## 6. W11 第 5 步复核 OxiDNS classical 缓存

本节由执行者 Codex（GPT-6.2 Sol）记录自查与验证。前五节为 Claude Opus 5.5 的独立复核。

检查从 `go@f381bc2fd` 切到 W11 后，`Profile.Repo` 缺失是否改变 OxiDNS 写入、生成或签名。协调者在 `final-w@80711a9a6` 独立修复了规则库列表 `has_ip_part`，它是这次调用链中唯一直接消费探测值的生产输出。

- 旧版 `classicalWithoutIP` 用后台 context 调用只读 peek，生产进程通过 `repo.SetRepository` 取到同一仓库。W11 的 nil 仓库会把 URL 内容当作未知，`hasIPPart` 因此返回可能含 IP。内联内容仍直接解析。
- `legacyContentAware` 在旧版和 W11 都没有调用方。routing catalog 会调 `hasIPPart`，但只把结果用于 `ruleRecord.shapes` 中的一个候选形状。一个可读的 classical 记录总有 `legacyAll` 产生的「域名＋IP」和当前规则产生的「域名」。缓存命中无 IP 时添加后者，其余情况添加前者。两个键已经存在，IP-only 依旧不合法。资源归属、下载与撤回使用 `records.parts` 和 `atoms`，两个字段来自 `legacyAll`。
- rules 的规则编辑、删除及 DNS 写入会进入 native routing 识别。proxygroups 只在已有跟随标记且去向发生变化时进入 `ApplyRuleDNS`，rulelibrary 在来源变化影响 native 分流时以 lenient 模式进入。strict 的冲突及 lenient 的警告都依赖同一识别结果。模板分支及依赖未变化时的提前返回不读 catalog。
- deploy 的 Agent 配置及 bundle、rulesync 的候选构造、native 投影及签名、dnsfollow 的已应用配置比对、gateway onboarding 和 setup 校验、OxiDNS draft/render、routers preview、configgen、proddiff generator 的 YAML 和资源描述走同一套 native 路由识别或只使用资源路径。模板生成使用 `legacyNone`。规则集归属、设置 revision、设备备注和 Agent 投影也不消费缓存探测结果。由代码检查确认这些调用无需补同一 repo。
- 写事务中的 profile 构造保留现有路径。缓存缺失时路径解析可能进入 `repo.View`；若底层读取失败，`readEntry` 会重试并尝试拿 L3。当前探测传的是后台 context，直接在写事务里接入 repo 会丢失持锁信息。此次没有增加读缓存、迁移或写盘路径。

`TestClassicalCacheDoesNotChangeNativeRouting` 用同一合成草稿对比旧 `by-id` 缓存经验证无 IP 和 profile 无 repo 时探测为未知的结果。域名和域名＋IP 草稿生成的 YAML、严格 DNS 更新产出的 replacement settings JSON 均逐字节一致。IP-only 的冲突类型、报错文本和保留 IP match 的生成结果也一致。探测前后比较文档、revision、提交次数、文件内容与 mtime，验证旧缓存未迁移。字节实测覆盖共用 OxiDNS 入口，HTTP、Agent 和 proddiff 的上层调用通过代码追踪确认。

在测试服务器 .64 上，合成用例三组均 PASS，`gofmt -l` 无输出，`go vet ./...` 和 `go vet -tags configflow_proddiff,configflow_difftest ./tools/proddiff/...` 通过。

包级 race 运行在 ippresence 通过后主动结束，改为以下定向 race。六包全部 `ok`，用时 1.052–10.216s。覆盖合成缓存、锁定草稿回滚、发布前校验、DNS 联动、native graph 校验及 router 拒绝处理。

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- timeout 10m bash -lc 'cd server && test -z "$(gofmt -l internal/ippresence/generation_peek_test.go)" && go test -race -count=1 -timeout 5m -run "^(TestClassicalCacheDoesNotChangeNativeRouting|TestGeneratePeeksLegacyCacheAfterRuntimeShutdown|TestC27ReviewLockedTransactionRollsBack|TestC27ReviewNativeIdentitiesRemainEditable|TestOxiDNSPublicationAttestsBeforeActivation|TestConfirmedFollowChangesOnlySelectedUpstream|TestNativeGraphSafety|TestReviewR15OxidnsAndRepositoryRejectionsAre400)$" -v ./internal/ippresence ./internal/oxidns ./internal/deploy ./internal/agents/dnsfollow ./internal/gateway/onboarding ./internal/api/routers'
```

在 .32 上使用合成夹具运行以下 HTTP 差分用例。rules 写入和自定义分流拒绝、proxygroups 跟随写入、rulelibrary native parking 四项全部 PASS。两包末行分别为 `ok configflow/internal/api/rules/difftest 7.737s` 和 `ok configflow/internal/api/rulelibrary/difftest 7.471s`。

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- timeout 10m bash -lc 'cd server && CONFIGFLOW_DIFF=1 CONFIGFLOW_PY="$HOME/cf-test/.venv/bin/python" go test -count=1 -timeout 5m -run "^(TestTemplateAndNativeDNSWrites|TestCustomNativeRoutingPreservesCommittedMetadata|TestProxyGroupFollowDNSHTTPParity|TestLibraryNativeParkingHTTPParity)$" -v ./internal/api/rules/difftest ./internal/api/rulelibrary/difftest'
```

复核收尾在 .32 验证。traffic 回归用例把内核上报写入应用注入的临时 store，确认生成的 DNS 策略随版本变化。用 Go overlay 临时恢复旧的独立 store 读取后，旧版本子用例因错误生成策略而失败。OxiDNS 合成用例在采集状态快照后清空 memo，覆盖冷缓存读取。两个受影响包的格式检查、vet 和 race 测试均通过。
