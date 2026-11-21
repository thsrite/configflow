# O10-R4 + R7 HR 复核：Agent 公开投影、脱敏、派生视图缓存（2026-11-22）

复核对象：`batch-26`（= `go-perf-r4`，rebase 到 `go@51081064b`），`58cbaadaf..a910ba28b` 共 12 个提交。
方法：通读 `api/agents/agent_view_cache.go`、`public_agent_adapter.go`、`content_routes_adapter.go`、`agents/registry.go`
（`ReadAgentsAt/ReadAgentAt`）、`runtime_model.go`（overlay）、`repo/agents_borrow.go`、`agents_view.go`、`system.go`
（`AgentRecordCredentials`）、`redact/redact.go`、`fields.go`、`agents/diag/{diag,credentials}.go`、`agents/messages.go`、
`ha_model.go`、`events.go`、`deploy/adapter.go`（`SourceFromConfig`、`BorrowedTarget`）、`content_adapter.go`、
`repo/profile_records.go`。作者的 difftest/oracle 测试只读不重跑全套（WORKFLOW「Leaner verification」），自己的修复配负向对照。

## 结论

缓存键、按成员投影、正则前置条件、记忆化、`SourceFromConfig`、R7 都成立。发现 1 个中危（已修）、4 条低危/说明。

## 中

### M1 投影用的存储凭据可能来自比记录更新的一代（已修，`4615e5ada`）

第二轮把凭据集合改成「同一代所有存储 Agent 的凭据 + 实时 overlay」，但实现是两次独立的存储读取：
`ReadAgentsAt` → `BorrowAgentsAt` 读一次 head 拿记录；`liveAgentSecrets` → `redact.AgentRecordCredentials` 再读一次 head
（`store.Reading` 里的 `connection.Head()`，缓存不中时 `readEntry` 甚至可能读到第三代）。两次之间有提交时，
H1 的记录用 H2 的凭据投影。

能泄露的只有**私有成员**里的凭据：记录自己的凭据字段（`password`、`api_key` 等）会被 `SanitizeCredentials` 在投影内自己发现
（`s.discovered` + 第二遍），不依赖这份集合；但 `token`、`applied_content`、`dns_follow_job` 在投影前就被丢掉了，
它们的值只能靠集合遮蔽。场景：Agent token 被轮换（重新注册/重置）的提交恰好落在两次读取之间，而该记录的某个公开文本字段
含有旧 token —— 旧 token 原样出现在响应里；HTTP 出口的 token 脱敏只认当前 head 的 token，也拦不住。
缓存开时这份结果还会以 H1 为键缓存，直到下一代读者到来（只有 H1 读者能命中，窗口很短）。
改前（第一轮）summary 的凭据在 `inspect` 回调里从同一批借来的记录收集，全量/详情从记录自身收集，都没有这个问题；是第二轮引入的。

修复：`agents.Generation` 带上 `BorrowAgentsAt` 返回的借用记录；新增 `Manager.GenerationCredentials(ctx, gen)` →
`redact.AgentRegistryCredentials` → `repo.AgentRecordCredentialsOf(head, shared, records, collect)`：凭据**只从这一代的记录**收集，
共享代复用/刷新原来按 head 的缓存（键、值语义与 `AgentRecordCredentials` 相同），写事务内（shared=false）照旧收集工作副本、不缓存。
不是 `ReadAgentsAt/ReadAgentAt` 得到的 `Generation`（零值）回退到原来的当前代读取。
`Generation` 只用带键字面量构造，没有 `==` 比较，加字段不影响其他调用方。

测试 `TestAgentViewMasksItsOwnGenerationCredentials`：记录 token=旧值、未知成员 `remark` 含旧 token；`ReadAgentsAt` 后提交轮换 token；
用旧代投影必须把 `remark` 遮成 `[REDACTED]`。负向对照：把 `liveAgentSecrets` 换回 `redact.AgentRecordCredentials(ctx, nil)`，
测试失败（`own generation token leaked: "registered with old-agent-token-7f3a"`）；恢复后通过。
（第一版用 `ssh_password` 做对照时没有失败——它被 sanitizer 自己发现，所以对照换成私有成员 token。）

## 低 / 说明

- **L1 详情和列表共用 `viewFull` 键但 status 不同时会互相挤掉。** 列表在读时把心跳过期的记录改成 `offline`，详情（与 Python 一致）不改；
  同一 Agent 在心跳过期后交替请求列表/详情，指纹不同，每次都重算。只影响命中率，不影响正确性（指纹里有 status）。不改。
- **L2 内容源缓存的 Config 也是单独读的。** `views.source` 以注册表读到的 head 为键，但 `Repository.Config` 是另一次读取，可能拿到更新一代。
  它只影响 `content_sync`（每请求在缓存外计算），结果与不缓存时同一请求读到的一样新；一代内的旧读者最多看到新一代的内容源。不是泄露，不改。
- **L3 `SourceFromConfig` 的类型化视图是借来的 Config 本身，而不是其编码的再解码。** oracle 测试比较了 raw、input key、类型化视图编码，
  只覆盖 fixture 的一份配置；若某个 Config 成员解码后与原对象「编码相同、内存状态不同」，生成器可能看到差别。目前没有找到这样的成员
  （生成器只经 `ViewConfig`/`config()` 读）。Source 现在跨请求共享（视图缓存 + checker 后台比对），生成代码若对 `*model.Config`
  顶层赋值会是数据竞争——view guard 只抓文档里嵌套值的修改，抓不到 `lend` 出来的浅拷贝的顶层字段；`-race` 覆盖了 api/agents、deploy 的现有路径。
  记为后续审计点。
- **L4 缓存只保留一代、键有界。** 条目键 (位置, id, 视图)，id 只来自读到的记录，详情 404 不入缓存；指纹变化覆盖同键条目。
  旧代读者到来会把缓存切回旧代（抖动，不是错误）。并发：计算在锁外，填充/命中在 `v.mu` 下，命中返回 `maps.Clone`，
  调用方只增删键（`content_sync`、`has_update`、`upgrade_available`、history 字段删除），不改 `RawMessage` 字节。

## 逐项核对

### 1. 派生视图缓存的键

公开响应逐字段来源（全量/详情；summary 同，少 content_sync/has_update/upgrade_available）：

| 字段 | 来源 | 键/每请求 |
|---|---|---|
| 存储的声明成员、未知成员（`Fields`） | 借来的记录 | head + 位置 + id |
| `token`/`applied_content`/`dns_follow_job` | 删除 | — |
| `deployments`/`latest_deployment` | 类型化历史投影（`HAAggregateView`、消息本地化、`CleanOperationEvents`），无时间依赖 | head |
| `latest_upgrade` | `PublicUpgradeStateModel`（`DiagnosticText`、`ExplainUpgradeFailure`），无时间依赖 | head |
| `oxidns_ha_publication` | `HASummaryModel`，只读本记录 | head |
| `version`、`service_status`、`system_metrics`、`cpu`、`memory`、`disk`、`network` | 心跳 overlay（`RuntimeFields`） | 指纹 |
| `last_heartbeat`、`status`（含读时离线判定） | overlay + `FreshHeartbeat` | 指纹（离线判定在算指纹前写入） |
| 凭据遮蔽 | 本代存储凭据（M1 修后）+ overlay 成员凭据 + `AGENT_REGISTRATION_KEY` | head + 指纹 + env |
| history=0 | 从克隆删 `AgentHistoryFields` | 每请求 |
| `content_sync` | checker、内容源（按 head+profile 缓存，见 L2） | 每请求 |
| `has_update`、`upgrade_available` | 版本 + 磁盘升级文件 | 每请求 |
| 出口 token 脱敏 | `jsonresp` | 每请求 |

overlay 只写 `RuntimeFields` + `LastHeartbeat` + `Status`（`runtime_model.go`），与 `liveMembers` 的 9 个成员一致。
没有登录用户/权限相关字段（`auth.Require` 只做门禁）。配置空间/资源变化都会改 head。写事务内（`Shared=false`）不缓存。

### 2. 脱敏

- 按成员投影 vs 整条投影：`agentMemberNames` 取自 `model.Agent` 的 json tag，加 `Fields.All()`；私有成员在两处都丢。
  oracle 测试 13 种形态（私有成员、HA、空/null/错型历史、重复/无效条目、凭据字段、未知成员）逐字节相同。抽查无遗漏。
- 凭据超集：同一代（M1 修后确实是同一代）所有存储凭据 ⊇ 本记录的存储凭据；overlay 成员单独收集 ⊇ 原来从 live 记录收集到的 overlay 部分。只会多遮。
- 正则前置条件逐个核对：`secretPattern` 必须有字面 `[:=]`（`(?i)` 下 `:`/`=` 无大小写折叠）；`authPattern` 是 `foldI((Bearer|Basic))`，
  首字母 `b` 在 Go 的简单折叠里只有 `b/B`（`i` 被替换成 `[iİı]`，不影响首字母）；`pemPattern` 区分大小写、必须有 `-----BEGIN`（含 `B`）。
  `CredentialSpans`：URL 分支要求 `://`，`bareUserinfo` 要求 `:`。构造不出缺这些字符却能匹配的串，前置条件不会放过凭据。
- 记忆化：`texts` 挂在单个 `sanitizer` 上（每次 `SanitizeCredentials` 新建），`str` 的结果只依赖输入串和不变的 `s.secrets`
  （`s.discovered` 只在第二遍的新 sanitizer 里用），不会跨请求或跨 Agent 共享。

### 3. SourceFromConfig

只读借来的 Config：`model.Marshal` 编码、`members` 是自己的解码；`modelConfig` 只经 `config()`/`ViewConfig`/`InputKey` 读。
view guard 在测试二进制默认开（`testViewGuard`），api/agents 的测试全部走缓存开的路径；只有分配测试 `SetViewGuard(nil)`。见 L3。

### 4. R7 `/api/profiles`

`agentBindingRecords` 类型化记录逐个 `MemberJSON` 读 id/name/host/port/service_type/profile_id，`agentProfileBinding`/`agentBinding`
两条路径共用同一套默认值与非字符串 profile_id 跳过规则；`System.Agents` 为空时走原编码路径。与原实现等价；
`TestAgentBindingsTypedMatchesEncoded` 覆盖默认值、假值、奇怪类型、未知成员、带历史、空。

### 5. 行为

响应字节由作者的 Flask difftest 覆盖（未重跑全套，按 Leaner verification 由批次门禁覆盖；本复核跑了 api/agents 相关 difftest，见下）。
唯一行为差异是更严格的遮蔽（作者已说明，Python `_public_agent` 不做凭据脱敏）。

## 提交

- `4615e5ada` M1：投影用记录所在代的存储凭据，加回归测试（含负向对照）
- 本报告单独提交

## 验证（.64，`4615e5ada` 上）

```
gofmt -l .                       → 无输出
go vet ./...                     → VET-OK
go test -race -count=1 -p 6 redact repo agents api/agents deploy api/profiles routers api/routers api/clientdevices
  ok redact 1.4s / repo 260.9s / agents 422.1s / api/agents 307.5s / deploy 292.2s / api/profiles 4.9s /
     routers 450.2s / api/routers 39.3s / api/clientdevices 7.3s（无 DATA RACE）
CONFIGFLOW_DIFF=1 go test -tags configflow_difftest api/agents/... api/config/difftest httpx/difftest
  ok api/agents 99.3s / api/agents/difftest 260.7s / api/config/difftest 65.5s / httpx/difftest 26.2s
/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-26 x 6 → PREFLIGHT PASS
```

负向对照（M1）：`liveAgentSecrets` 换回 `redact.AgentRecordCredentials(ctx, nil)` → `TestAgentViewMasksItsOwnGenerationCredentials` FAIL；恢复后 ok。
