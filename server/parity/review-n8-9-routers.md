# N8-9 routers / routersplit S pass — fresh Opus HR 复核（2026-11-19）

复核对象：分支 go-n8-9-routers，提交 eceb4e2e6 加当时未提交的工作区改动（repo/router_settings.go 把 replacement 检查移到 CAS 之后，
routersplit identityError 改为 RouterSplitError 409，sidecar 脚本仍在改）。只读产品代码。负控在 .64 的独立目录
`~/n89-review` 里跑，没有经过 rt，也没有碰 .32 上作者的目录，跑完已删除；本地工作区没有新增测试文件。

## 结论

有 2 项必须修复：F1（高）、F2（中）。另有 3 项中等问题（F3、F4、F9），其余为低严重度或登记类问题。
Python 从来不会写出 F1/F2 需要的存储形态，只有导入、恢复或手工编辑的配置会触发；但一旦触发就违反 R5「坏成员不得造成 UI 锁死」，
F2 还会阻断整个 profile 的 Mihomo 发布。

## 发现

### F1 高：存储身份字段类型错误时，路由器的 PUT/DELETE 返回 500，UI 无法修复或删除

- 位置：`internal/routers/joblock.go:47-52`（`RouterJobLock` 调用 `validateIdentity`）、`internal/routers/stored_model.go:115-140`、
  `internal/api/routers/handlers.go:155-165`（`job()` 只把 KindConflict 映射成 409，其他错误直接返回，变成 500）。
- 触发方式：某个存储的路由器出现 `ip_rule_sets:["r1",2]` 或 `username:12345`（其他身份字段是数字或容器时同样触发）。
- 负控（.64）：创建路由器后，用 `UpdateProfile` 把对应成员 `SetJSON` 成坏值。之后
  `PUT /api/routers/<id>`（payload 已带修正后的 `ip_rule_sets`/`username`）返回 **500**，`DELETE` 也返回 **500**，日志为
  `Exception on … 路由器身份和凭据字段必须是文本`。两个 case 结果相同。
- Python 的行为：DELETE 只检查 *_state，总能删除；PUT 中 payload 会覆盖存储值，修正后能保存成功；
  数字形式的用户名或密码会被 `str()` 后照常使用。
- 同一把锁的其他调用方也会受影响：`clients/fixed.go:439`、`agents/ipv6/readiness.go:218`、`routers/rule_sync.go:556`。
  它们会原样返回 routers.ValidationError，也可能变成 500，而且一个坏路由器会挡住整组操作。
- 修复建议：RouterJobLock 只负责锁，身份校验移到真正打开驱动或写设备之前（`ManagementService.open`、Inspect、发布、读取路由等）。
  设置的 PUT/DELETE 不校验存储身份。DELETE 只做现有的 *_state 检查；PUT 交给 Normalize 校验合并后的结果，
  payload 覆盖的坏值不再报错，没覆盖的坏选择列表按 Python 返回 400（见 F3）。所有身份错误统一映射为 400/409，不能出 500。
  同时补一条 HTTP 回归测试：坏存储身份下 DELETE 返回 200，PUT 修复后返回 200，probe/apply 返回 4xx。

### F2 中：ValidateTopology 的范围太大，一个无关路由器的坏 proxy 链接会让整个 profile 返回 409，marker 模式和发布也受影响

- 位置：`internal/routersplit/routersplit.go:359-392`；调用方为 `legacy.go` 中的 LegacySource、LegacyGatewayAgentIDs、
  ModeFromContext、RouterSplitState、PublishLegacySource。
- 负控（.64）：
  1. 有效 marker 的 profile 中，只要有一个路由器 `proxy_gateway_agent_id:5`，`ModeFromContext` 就返回
     `主路由分流的拓扑身份必须是非空文本`。Python 返回 `"full_tun"`。
  2. 非 marker 的 profile 中，r1 绑定 `agent-a`，另一个 r2 的 proxy 为 5，`LegacyGatewayAgentIDs` 报错。Python 返回 `["agent-a"]`。
- 产品影响：
  - `deploy/adapter.go:589` 中，每次非 docker、未启用网关的 Mihomo 发布都会走 PublishLegacySource，因此整个 profile 的发布都被挡住。
  - `gateway/draft/adapter.go:220` 中，ModeFromContext 在 marker 判断之前执行，marker 模式下保存网关草稿也会被挡住。
  - host 和 onboarding 中的 RouterSplitState 同样受影响。
- 字典序 sidecar 把这一点固化成了「期待」：约 1060 行从 out 变成 err（拓扑身份），其中包括 r02_corpus 第 12 行这样的有效 marker 输入。
  这一部分不属于 R5 类型政策，属于新增的拒绝范围。
- 修复建议：非字符串或容器形式的 proxy 链接一律视为「无链接」（不转成文本，也不参与匹配），不再整体报错。
  只有在真正消费错型身份时才 fail-closed：链接匹配上的路由器其 id 错型，或者 context 的 mihomo_agent_ids/session_id 错型。
  marker 有效或无效时，不对拓扑做校验（这条路径本来就不读拓扑）。修复后用真实 Python 重新生成 sidecar。

### F3 中（目前被 F1 遮住，修 F1 后会暴露）：无关编辑会把错型的选择列表静默写成 `[]`

- 位置：`router.go` 的 Normalize 中 `m["ip_rule_sets"]=existing.StaticRuleSets()`，以及 `idList(m["ros_policy_rule_sets"])`；
  `stored_model.go:58-71` 把坏列表投影成 nil。
- 负控（.64）：existing 为 `ros_policy_rule_sets:["x",2], ip_rule_sets:["r1",3]`，payload 为 `{"name":"renamed"}`。
  Normalize 成功，写出 `"ros_policy_rule_sets":[],"ip_rule_sets":[]`。
- 违反的规则：R5「原始字节保留，除非代码赋非零值」。用户的路由选择被清空，下一次发布会删除设备上的对应路由。
  Python 遇到这种情况返回 400 `IP 路由规则集必须是规则集 ID 列表`。
- 修复建议：existing 的选择列表有 issue 且 payload 没有覆盖时，按 Python 返回同一条 400 消息，不要用回退值写回。

### F4 中：确认不了 explicit 意图与原始表示之外的写回路径——存储路径改未知字段会报错

- 位置：`router.go:131-137`。`Router.MarshalJSON` 在存储路径上对 Extra 中的改动调用 `stored.SetJSON(key, …)`，
  而 `model.SetMemberJSON` 对未声明成员返回 `set of an undeclared member`。
- 负控（.64）：存储记录带 `"future":1`，把 `r.Extra["future"]` 改成 `2` 后 MarshalJSON 报错；新增一个未知键也报同样的错误。
- 现在的写入方只改已声明的 *_state、last_*，`captureRouter` 也把 source 置空，所以暂时不会触发；但「未知字段保留并可改」的契约是坏的。
- 修复建议：未声明成员改用 `stored.Fields.Set`（删除已经用的是 Fields.Delete）；补表驱动用例：修改或新增未知字段、删除已声明 state。
- explicit map 本身核对过，没有问题：显式写入等于默认值时也会写；不显式且等于 baseline 时不物化；`*r=*existing` 之后 explicit 会重建；
  `stored := *r.stored` 浅拷贝依赖 Fields/Raw 的写时复制，不会污染原始元数据。

### F5 低到中：请求侧行为变化超出 R5 类型政策，尚未登记，sidecar 与 Go 的口径也有差异

- 已放宽：`rule_sync_interval` 的请求值现在接受 `"60"` 和 `60.0`（`router.go` 用 model.Int 解析）。设计文档 R8 明确写着
  「`type(x) is int` 是请求校验，不是存储规则」，Python 对这两种输入都返回 400。建议请求侧改为只接受 JSON 整数（可以保留 6e1 这类整值），
  或者登记为批准的行为变化。
- 已收紧（Python 原本接受）：`verify_tls` 为 null、1、`[]` 时返回 400（Python 的判断是 `is not False`）；
  `ip_rule_sets`/`ros_policy_rule_sets` 为 `false`、`0`、`""` 时返回 400（Python 用 `or []` 当作空列表）；
  数字 name 被接受，bool 或 list 形式的 name 被拒绝。前端只发规范类型，影响很小，但需要登记到 behaviour-changes。
- R01 sidecar 的 `run()` 对字符串 interval 使用 `re.fullmatch('[+-]?[0-9]+', v.strip())`，接受 `"+60"` 和 `" 60 "`；
  model.Int 拒绝加号和空白。当前语料里没有这类行，所以测试能过，但这是一处潜在的口径不一致，应与 model.Int 规则逐字对齐。

### F6 低：规则环境的分配负控是空转的；环境缓存按 ctx 是否存在而不是按 config 区分

（作者已获授权 rebase 到 go-review-n8-9-rules@9989da440，删除 legacy_rulefetch，所以这里不作为最终缺口。
但下面第一点的教训同样适用于替换后的负控：测量必须经过真实的逐规则路径，并有一个去掉缓存就失败的对照。）

- `n89_model_test.go:186-212` 先 `withSyncEnvironment` 再测量，之后的循环只命中缓存，完全不执行 `loadSyncSources`、Download、sync 这些真实路径。
  即使把产品中三处 `withSyncEnvironment` 全删，这个测试也会通过。建议改成经过 `loadSyncSources` 跑多条规则来测量，
  或者加一个去掉预装环境就失败的对照。
- `legacy_rulefetch.go:24` 只要 ctx 里已有环境就直接复用，不比较 config。如果将来同一个 ctx 被用于另一个 config，会静默拿到旧的代理或系统设置。
  建议把 config 指针或摘要存进环境并比较。
- `syncTransportKey` 在缺少环境时会因类型断言 panic（违反「不用 panic」）。建议用 ok 形式断言并返回错误。

### F7 低：数字文本仍然会在少数地方变成可用身份

- `routersplit.Session.ID` 是 `*model.String`，会把 sessions 中的数字 id 7 变成 `"7"`，经 LegacyContext 输出后再解码时已经是合法字符串，
  绕过了 Context 对 session_id 的严格校验。sidecar 的 `session()` 也用 `text()` 做同样的转换，等于和 Go 共用同一套口径。
- `GatewayWasUsed` 的 `gateway_installation_id` 用 model.String 读取，数字 0 会变成 `"0"`，被当成安装证明；Python 中 0 为假。
- `inspect.go:351`（returnCleanup 的 SharedWith）和 `pending.go:104` 会让数字 proxy id `"5"` 匹配到 agent `"5"`，而 Python 不会匹配。
- `api/routers/handlers.go:347`（GET routes）和 clients discovery 打开驱动前不调用 `validateIdentity`，与其他入口不一致。
  这些都是读操作，Python 同样会把凭据 str() 后使用，所以严重度低。
- 修复建议：这些身份位置改用严格的字符串读取，错型时按「无」处理（不报错，也不转成文本）。

### F8 低：sidecar 和 pyref 元数据尚未收口（是在作者自己改动期间看到的状态）

- routersplit 的 `n89_expectations.json`（06:19 生成）中，拓扑错误的 class 是 `ValueError`，没有 code/status；
  当前 `n89_reference.py`（06:22 修改）抛出的是 `RouterSplitError(…,409)`。期待文件需要用最终脚本重新生成。
  compareError 会比较 class、code 和 status，如果没重生，测试应该会失败。
- 脚本会写出 `n89_session_expectation.json`，但 testdata 里没有这个文件，`sources.json` 也没有登记。
- routers 包的 `n89_reference.py` 和 `n89_expectations.json` 没有出现在 `tools/pyref/sources.json` 中。
  需要确认 routers 的 R01 是否属于必须登记的 pyref oracle（batch-6 曾因未登记的 oracle 被门禁挡住）。
- R02 sidecar 中的 `split._router_ids` monkeypatch（空 id 不能证明转发关系）修改的是业务逻辑，不是类型投影，
  应作为 Python bug 修复登记进 behaviour-changes。

### F9 中（既有问题，不是本轮引入）：坏的 dns_guard 回退成「关闭」，可能撤掉设备上的守卫

- `stored_model.go:76-81` 遇到 `IssueUnder("dns_guard"/"ipv6_pd_guard")` 时置 nil，`inspect.go:358` 再把它当作 DefaultDNSGuard（关闭）。
  例如 `{"enabled":true,"bypass":"x"}`：Python 保持启用（之后在 bypass 处出错），Go 按关闭规划，存在 dns_guard_state 时会生成撤除操作。
  旧 Go 对整个成员类型错误时也是丢弃，所以不是回归；但新注释声称「partially invalid guard cannot authorize device management」，
  实际效果是 fail-open（撤除守卫）。
- 修复建议：守卫配置有 issue 且存在 state，或原始 enabled 为 true 时，发布和规划直接拒绝（返回明确错误），不能回退成关闭。

## 已核对、未发现问题的部分

- **RouterJobLock 唯一 L1**：`lockx.NewKeyed` 只有一张表，repo 的 `jobLockHook`（legacy_wire.go）也走同一个函数。
  顺序是先 TryLock，busy 时直接返回 KindConflict（busy 优先，`TestR15JobLockPrecedesValidation` 覆盖），拿到锁后检查 `held.Err()`，
  失败时 release。只是身份校验放在锁里这一点有问题，见 F1。
- **关机与断开**：`SaveRouterSettingsJSON` 在 Detach 之后、事务内提交前都检查 `ctx.Err()` 和 `work.Err()`；
  `recordPendingNotice` 在 `SaveRouter` 前检查 `work.Err()`；management 的事务路径没有变化。
  没有发现新的写入入口绕过第四条常设项。真实 Runtime 关机回归按作者计划在最终 race 中执行，本复核没有重复跑。
- **设备请求原始 body/header 与凭据脱敏**：相对 d4fab6441，ROS/iKuai 客户端、请求编码和 `internal/redact` 都没有改动
  （rosnat.go 只改了注释）；`RedactRouters` 只是把 truthy 改名为 hasStoredState，语义不变；safeLog/safeText 仍走 redact。
  R04–R17 的原始请求测试不受影响。
- **原语料**：r01、r02、r03 以及 r02_* 的 `.jsonl.gz` 相对基线没有改动。两个 sidecar 都绑定逐行输入的 SHA256，
  有「stale/duplicate」保护，脚本不读 Go 输出。普通行仍走旧比较器；routers 的 `reprField`/`readerDefaults` 豁免是旧的，没有扩大。
  sidecar 的问题在于政策范围（F2、F5、F7、F8），不是期待来自 Go。
- **R01 默认物化**：sidecar 的 pop 规则（existing 和 payload 都没有、结果等于默认值时不写）与 Go 的 baseline 加 explicit 判定等价。
  显式写入的默认值会落盘。
- **routerProfiles 分配负控**：padding 放在 CustomConfig 字符串中，旧实现每个 profile 都要做 GetProfile 加整体编解码，
  会超出 64 KiB 的差额，所以这个负控对旧路径有效。分配不按路由器数量放大。
- **saveWorkspacePolicies**：按协调者要求，这一轮没有改动。

## 负控记录（.64，独立目录，已删除）

```
TestZZReviewMalformedStoredRouterLockout/ip_rule_sets: PUT 修复 500，DELETE 500
TestZZReviewMalformedStoredRouterLockout/username:     PUT 修复 500，DELETE 500
TestZZReviewUnknownExtraEdit: modified/added unknown → model: incompatible JSON type: set of an undeclared member "future"
TestZZReviewSelectionListEdit: rename → "ros_policy_rule_sets":[],"ip_rule_sets":[]
TestZZReviewUnrelatedMalformedLink: marker/plain ModeFromContext 和 LegacyGatewayAgentIDs → 主路由分流的拓扑身份必须是非空文本
```

## 修复复核（提交 117cf39c0，2026-11-19）

本轮只读代码，没有跑任何测试；作者说明负控已固化为永久 HTTP/单元回归，并在 .32 通过。
按作者要求，前面 .64 上的负控不计入验收。

| 项 | 结论 | 核对依据 |
|---|---|---|
| F1 | 已修复 | `RouterJobLock` 只做 try-lock 和 shutdown 检查（joblock.go）。身份和守卫校验集中在 `ValidateForDevice`（stored_model.go:157），调用点覆盖 management.open、Inspect（发布、pending、rule sync 都经由它）、rule_sync.go:346、GET routes、inspect handler、fixed-IP、IPv6 readiness/inspect/evidence、PD guard。错误统一为 ValidationError（HTTP 400）或各模块的 409，不再出现 500。`n89_boundary_test.go:86` 覆盖：坏 username 和坏选择列表下 probe/routes 返回 400，无关改名返回 400，修复后 PUT 返回 200，DELETE 返回 200。 |
| F2 | 已修复 | `ValidateTopology` 在 marker 非 absent 时直接返回。`topologyRouter` 中非文本或 null 的链接视为无链接；只有链接确实命中 context 的 Agent、而路由器 id 不是非空文本时才返回 409。Context 和最新会话的错型身份清空后返回 409。sidecar 用同一口径重新生成（2553 行），R02 原输入总数不变。 |
| F3 | 已修复 | Normalize 在 payload 没覆盖时读取原始 `existing.source` 的选择列表，错型就按 Python 的消息返回 400；payload 覆盖时允许保存。 |
| F4 | 已修复 | 未声明的 Extra 改用 `Fields.Set`，已声明的 state/last_* 用 `SetJSON`，删除用 `Fields.Delete` 或 `Clear`。 |
| F5 | 已修复并登记 | 请求中的 interval 恢复为严格 JSON 整数（`*int`），`"60"` 和 `60.0` 都返回 400，与 Python 一致；sidecar 删除了 interval 投影，直接用真实 Python。TLS/身份严格化已登记到 behaviour-changes。 |
| F6 | 搁置 | legacy_rulefetch 将随 rules rebase 删除，届时复核新负控能否覆盖真实逐规则路径。 |
| F7 | 已修复 | Session 只对最新非 abandoned 会话的错型 id 拒绝；installation proof 只接受非空字符串；pending、returnCleanup、IPv6 linked 用 `LinkID`（原 token 必须是文本，不再把数字转成文本）。 |
| F8 | 已修复 | 拓扑错误的 class/code/status 统一为 RouterSplitError/ROUTER_SPLIT_INVALID/409；`n89_session_expectation.json` 已提交，并由 `n89_session_test.go` 绑定输入、原 Python 答案和 R5 答案；`_router_ids` 的修改已作为「空 ID 和数值相等不再制造关系」登记。 |
| F9 | 已修复 | 守卫配置有 issue，且原始 enabled 为 true 或存在 *_state 时，`ValidateForDevice` 返回 `messageStoredGuardInvalid`，不会再回退成关闭并撤除守卫。 |

### 剩余意见（都是低严重度，不阻塞 verified）

1. 守卫校验写在 `ValidateForDevice` 里，所以错型的守卫配置也会挡住与守卫无关的设备操作：probe、GET routes、fixed-IP 写入、IPv6 readiness。Python 中这些操作不受影响。
   如果希望范围更小，可以拆成 `ValidateIdentity`（所有设备操作都调）和 `ValidateGuards`（只在 Inspect/plan/publish 中调）。按现状保留也可以接受，因为方向是 fail-closed，并且错误消息会提示用户先修正设置。
2. Normalize 的 F3 检查放在其他字段校验之前。存储列表错型、同时还有其他错误的请求，返回的第一条消息与 Python 的顺序不同；另外存储值为 `false/0/""` 时 Python 按空列表处理，Go 返回 400。两者都只出现在手改数据中。
3. behaviour-changes 里还没写到这一点：请求中 `ip_rule_sets`/`ros_policy_rule_sets` 为 `false/0/""` 时现在返回 400（Python 把它们当作空列表）。建议在 N8-9 小节补一句。
4. rules 接线：见下一节，已复核。

## F6 接线复核（rebase 到 rules@9989da440，基于 117cf39c0 加未提交的 native 接线，只读）

只读了代码，没有跑测试。数值引用作者在 .32 的结果：`TestN89SyncPipelineAllocations|TestR14DownloadsProductionParameters|TestR14SourceSelection`，11.170s 通过。

- **legacy codec 已删除**：`legacy_rulefetch.go` 和 `syncTransportKey` 都不在了。`rule_sync_sources.go` 直接调用 typed 的
  `rulefetch.SourceConfig`、`ValidateRemote`、`CacheReadPath`、`WriteCache`；Download 使用正式的 `DownloadRequest`，
  5 s/10 s/30 s 的生产参数保持不变。routers 包里已经没有 jsonx 规则环境。
- **每次操作只解码一次**：`withSyncEnvironment` 只读 `system_config` 和 `rule_library`，传输摘要在构建环境时计算一次；
  Download 的缓存键只是 `transport + "\x00" + URL`。所有逐规则调用方都从同一个 ctx 取环境，包括 loadSyncSources 中的
  syncCacheRead/syncValidate、sync() 第 409 行之后的 syncCacheWrite 循环，以及 Capture 中的 loadSyncSources。
  `sync()` 里的 config 从读取开始到写缓存都没有重新赋值，所以环境不会和 config 对不上。
- **缓存读取**：在 `CacheReadPath` 返回的 typed 路径上做有界只读读取（`syncMaxBytes` 加 UTF-8 校验），不迁移旧路径，也不改 mtime；
  没有使用会迁移的 ReadCache，这个取舍正确。
- **分配负控（解决了上一轮「负控空转」的问题）**：
  - 新测试穿过真实的 `loadSyncSources` 逐规则路径。cache 模式把 1 MiB 无关数据放在 shared 和 config 里，用来抓逐规则重复读取
    repo/shared。download 模式用 loopback HTTP，走真实的 `rulefetch.HTTPTransport`。
  - 对照组通过 `noSyncEnvironment` 只在 Download 的 ctx 中隐藏环境。
  - 数字吻合：native 下 16 条规则额外 256 KiB settings 增加约 3.7 MB，约等于构建一次环境；隐藏环境后增加 63 MB，约等于 16 次。
    6 MiB 预算放在两者之间，多一次重建就会超限，判别力足够。
  - loopback 服务端断言了 GET 和空 body，符合常设第二项的原始请求要求。

### 剩余意见（低，不阻塞）

1. cache 模式下 system_config 里没有放 padding。可能被逐规则重解的大成员只有 settings，mihomo 字符串在 encoding/json 中被跳过时几乎不分配，
   所以这一模式只能证明「没有逐规则读 repo/shared」，证明不了「没有逐规则重建环境」；sync() 第 409 行之后的 `syncCacheWrite` 循环也不在测量范围里。
   建议 cache 模式同样在 settings 中放 padding，或者对写缓存循环加一条同预算断言。
2. `withSyncEnvironment` 仍然只要 ctx 里有环境就直接复用，不比较 config。目前所有调用方都是「一次操作一个 config」，所以不会出错；
   建议在注释里写明这个前提，或者把 config 摘要存进环境，防止以后复用 ctx。
3. 每次构建环境的分配约为 settings 大小的 14 倍（3.7 MB 对 256 KiB）。每次操作只有一次，所以不是问题，记下来供以后优化参考。

## 最终结论

F1–F9 都已关闭（F6 由本节关闭）。修复复核和接线复核中的剩余意见都是低严重度，不阻塞。
本复核同意把 N8-9 routers/routersplit 标为 **verified**，前提是作者自己的收尾检查通过：cross-corpus、定向 HTTP、新 fuzz 60s、gofmt/vet，以及把 native 接线提交。
