# C36 原生 setup flow 交接

分支 `go-port-c36`，从 C34 复核分支 `4ad74cf47` 叠起。五个方法均在
`internal/api/setup` 注册，核心编排在 `internal/setup`。不是 HR 模块，需另一位
Codex 独立复核。未 push、未开子 agent、未跑完整 gate；只使用合成数据。

当前依赖状态：等待协调者通知 batch-4（含 A20 reader）合入，然后 rebase 到
本地 go。没有复制或 cherry-pick A20/A21 文件。A21 最终注册由后合入者完成。

## 已实现的边界

- 五方法及 GET 隐含 HEAD：认证、no-store、严格对象/32 KiB 请求、状态码和中文消息。
- 可恢复创建、来源去重、进度保护、默认配置建议与清理提示、只读集合/单项。
- 模板目录、内置规则去重/别名/同名冲突、语义相等内容复用、区域策略筛选；
  模板从 repo 已嵌入资源读取，不另复制一份。
- prepare：独立 DNS/连接漏网处理、查询记录开关、来源缓存 URL 校验、可用节点计数、
  双核候选验证、保留用户基础 YAML、控制面板密码校验、端口冲突、完整 TUN 或旧
  Linux 网关兼容、选择性 IP 应用接管、CAS 与 owned/without-base 摘要保护、幂等重试。
- check/finish：真实发布/设备/转发证据、核心指纹、完整 TUN 与选择性网关分支、
  路由说明与缺失规则、手动显式确认、托管路由器检查、检查期间修改保护、可选默认配置。
- 私有 router draft：只在 session 0600 文件保留凭据；公共视图复用 routers.View
  和统一脱敏；检查后先落 checkpoint 再关联真实设备对，可幂等恢复。
- abandon：dry-run 零写；占用/安装/default 判定；保留依赖空间；删除私有来源、
  health/cache；并发占用后降级保留；显式放弃抑制首次使用提示。
- `setup.AdoptOwnedWrite`：只更新 profile 匹配、未完成且旧 owned_digest 匹配的
  session；保留未知字段，更新 full/without-base 两个摘要；无 session 是 no-op。

## 外部 owner 接线清单

`setup.SetExternalOwners` 替换全部 callbacks（测试隔离）；生产接线用
`setup.RegisterExternalOwners`，只合并非 nil owner，保留早先注册的 C32。
`setup.New` 每请求捕获一份 callbacks 快照。未注册的必需 owner 明确返回
503「等待首次使用向导模块」，不会构造安装状态或静默放行。

### A21（后合入者负责 server 注册）

1. `onboarding.SetAdoptOwnedWrite(setup.AdoptOwnedWrite)`。两包只由 server wiring
   同时 import；setupflow 是现有只读叶层，不能 import onboarding 形成环。
2. `setup.RegisterExternalOwners(setup.ExternalOwners{DNS: &setup.DNSOwner{...}})`：
   - `LocalUpstreams: onboarding.LocalUpstreams`。
   - `PrepareMihomoDNS` 薄闭包调用
     `onboarding.PrepareMihomoDNS(document, localAddresses, check)`，直接复用同一纯 leaf。
   - `check` 薄适配 C34 的 `gateway.MihomoDNSLoop(dns, addresses) != nil`，不得另写
     DNS parser；localAddresses 沿用 A21 的已裁定本机地址来源。
   - `DefaultLocalDNS: onboarding.DefaultLocalDNS`、`DNSTarget: onboarding.DNSTarget`、
     `Nameservers: onboarding.DefaultMihomoNameservers`，不重复定义常量。
3. A21 必须在 repo L3 释放后、`saved._revision == before._revision+1` 且 ctx 检查
   通过时调用 hook；before/after 为完整存储 profile JSON，不能拿 hydrated/shared
   snapshot。revision 不能把 null/缺失/非法值的 model fallback 当成有效证明。
4. C36 hook 自己 Detach，保留同一个 Runtime generation；取自己的 session 操作锁
   后及 rename 前检查 ctx。A21 不另取 session 锁，不从 Background 起新 generation。
5. 统一锁是 `lockx.LevelSetupSession = LevelDevice-1`（提交 f681824cb），顺序
   setup-session → profile/device operation → repo。A20 reader 无操作锁；若以后
   需要操作锁必须复用这个 owner，不能同资源另起不同级别的锁。
6. 最后接线时补 **真实 A21/C36 的 server 级测试**：注册后 prepare 的 503 消失并
   成功生成；嵌入保存的 revision+1、owned_digest 和 without_base 对照真实 Python；
   去掉 A21 暂缺 owner 的 503 分支。C36 当前 server 测试已证明 callback 注册从 503
   切换到真实 owner 的校验结果；它不冒充尚未合入的 A21 正式集成测试。

### A22 / A19 / A06 / A01

注册 `ExternalOwners.Installation`：
- `Latest(ctx, repository, profileID)` 返回 A22 最新 publication session 的 ID、phase、
  config/target changed 和按 freshness 规则得到的 Mihomo/OxiDNS 身份与在线状态。
- `HasInstallation` 用于 abandon 的保守占用判定，不能暂时写死 false。
- `GatewayStatus(ctx, repository, mihomoAgentID, profileID)` 返回现有 owner 的 IPv4
  forwarding/TUN/managed 状态，不由 setup 做 Agent transport。
- 公开状态错误适配成保留 Cause 的 `*setup.Error`（沿用原 HTTP status/message）；
  cancellation 保留错误链。缺省 callbacks 是 503，nil Latest 是实际“无安装”。

### 路由器 owner（routes.routers）

注册 `ExternalOwners.VerifyRouter(ctx, repository, configRaw, router)`，复用真实
Driver → Inspect → BuildPlan，保证 Close，返回 blockers/empty；设备错误按 Python
502 保留错误链。C36 只读检查，不自行发布、不伪造 empty plan。未注册时 managed
finish 是 503；manual 的真实确认流程不依赖此 owner。

### C32（本分支已接线）

单独提交 `99afa95df`，薄 API `rules.ReadRulesetContent` + `ContentOptions` 只转换
参数并调用原 sourceContent；cache/inline/remote/mirror 决策只有原 owner 一份。
server.wireSetupContent 已注册 ReadRouteContent；保留旧调用方签名和默认 30 秒。
向导总预算 30 秒；connect ≤ 5 秒、read ≤ 20 秒且都收紧到剩余时间和 ctx deadline。
预算耗尽后只读离线内容。tracked worker 用原 Runtime generation 和 fresh repo Scope，
没有裸后台 goroutine；请求断开不杀写 owner，关机取消不会穿越 generation。

## 验证映射（Leaner verification）

Python `backend/test_setup_flow.py` 的 76 个测试族按行为合并验证，没有逐参数机械复制：

| Python 行为族 | Go 代表检查 |
|---|---|
| suggestion/create/retry、sources/invalid URL、progress/auth | TestSetupFlaskHTTPStateAndAuthentication（真实 Flask，完整 DB/files），TestSetupIndexCreationAndSourceResume |
| recorder、fallback、domain selections、double-core fields、CAS/idempotence | TestPrepareFieldsMatchRealPythonAndRetainOwnership（两种目标，选中的 direct/proxy 内置规则），prepare.go 的原生 DTO/开关分支 |
| catalog/recommendation/disabled/aliases/namesakes/hand-written equality/regions | TestSetupCatalogOptionsMatchPython、TestHandwrittenRuleEqualityMatchesPython、TestSetupBuiltinResolutionIsIdempotentAndKeepsNamesakes、TestTemplateStrategyMatchesPython |
| user base/full TUN/port/password/gateway TUN | TestMihomoBaseChangesMatchPython（真实 Python 的文档与 changes），prepare 的双目标结构对照；缺失 DNS 补全归 A21 leaf 正式集成 |
| embedded owned write、revision/without-base、unknown session fields | TestOwnedDigestMatchesPython、TestAdoptOwnedWriteMatchesSessionOwnership、TestRecordPreservesUnknownAndExplicitClear；A21 的 revision+1 入口条件由 A21 最终接线测试补齐 |
| draft privacy/binding/attach/retry | TestSetupRouterDraftIsPrivateAndLeavesProfileRevisionAlone、TestRouterDraftAttachmentAndManualManagedFinish；checkpoint-before-profile 提交顺序在 action.go |
| installation/pair/freshness/full-TUN/selective/receipts/default | TestCompletionChecksAndPublicationContentGuards、TestFullTUNStatusFailures、TestCoreFingerprintMatchesPython |
| manual ack/managed plan/change guards/newly downloaded route table | TestRouterDraftAttachmentAndManualManagedFinish、TestCheckPreservesAbandonedSessionAndRequiresFreshRouteAcknowledgement |
| warm-up total budget/offline/connect/read | TestRouteWarmupBudgetAndOriginShutdown；C32 TestPublicRulesetContentTimeoutsAndOffline（真实 loopback stall） |
| abandon dry-run/delete/keep/install/reopen/suggestion | TestAbandonDryRunDeletionAndRetainedInstallation；跨 profile 订阅占用和设备并发由 repo 原 owner 的验证/删除事务处理 |
| shutdown/disconnect/session wait/native round trip | TestSessionOwnerRuntimeCancellation、TestWizardWriteOwnersPreserveRuntimeGeneration、TestWriteOwnerRechecksGenerationAfterSessionWait、FuzzSetupRecord |
| final wiring/readiness | TestSetupServerOwnerRegistrationRemovesUnavailable；真实 A20/A21/A22、设备 I/O、installer 端到端随各外部 owner 合入后补 |

## 已知功能差异与未完成项

- 原生并发/安全改动：不新建 Python flock 文件；HEAD/只读查询不创建 session 目录；
  损坏的 ownership 证据 fail closed；标准 YAML facade 的重复键/递归别名限制继续生效。
- 普通 JSON 对 session 未知凭据字段也执行统一脱敏，超出 Python 只投影 router draft
  的保护；可恢复凭据始终保留在私有 session 文件。
- 其余自有编排的正常行为、中文消息、已对照生成字段和状态码无已知差异。
- 外部 owner 的 503 是已授权衔接边界，不能作为永久功能缺失结案。A21 最后注册清单
  如上；A22 和路由器正式检查 callback 必须由其 owner 提供。
- 当前尚未 rebase 到包含 batch-4 的 go；协调者通知后再做，不自行复制依赖文件。
