# N8-11 W6 deploy / gateway / setup HR 复核（fresh Opus）

复核对象：`go..caf040dbd` 的 4 个提交（3633e0689 deploy、f435991d4 gateway、2b68de499 setup/setupflow/routersplit/router hooks、
caf040dbd pyref metadata）。复核分支 `go-opus-review-n8-11-w6`。协调者已批准的裁定（deploy typed 签名、agents 子包机械迁移、
typed `ReceiptMatches`、Receipts 旧口删除、越界修改清单、W4 RuleSourceReader、显式 null policy、setup digest 原生化 + 版本、
薄委托、CommitConfig、proddiff baseline、options_legacy 保留、routers RecordHooks）只核对实现与测试，没有重新讨论。
按 Leaner verification 只跑修复相关的定向测试和少量抽查，测试只在 .64 经 `rt` 运行，没有跑完整门禁。

## 结论

high 1（已修），medium 0，low 6（1 已修、1 需协调者裁定（已收紧测试）、4 记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| H1 | high | 已修 | **部署激活路由绕过了 HA owner。** 位置 `api/agents/deploy.go` `deploymentAction("activate")`。迁移前调用 `m.ActivateDeployment`（→ `ActivateDeploymentModel`：先走 `HAActivate` hook，无 hook 时做 `ownsHAChild`/`haOperationConflict` 检查），迁移后改成了 `m.ActivateCandidateModel`（直接激活候选）。这正是作者在 `gateway/installation` 修过的同一类误用，路由这一处漏了。影响：`POST /api/agents/<id>/deployments/<did>/activate` 对 OxiDNS HA 成员会绕过配对发布 owner，单独激活 pair 子任务，HA 冲突检查也被跳过（rollback/recover/poll 的映射正确）。修复：改回 `ActivateDeploymentModel`。测试 `TestReviewN811ActivateRouteDispatchesHAOwner`：注册 `HAActivate`，断言路由恰好调用一次、参数和响应来自 owner。负控：把路由改回 `ActivateCandidateModel` 后测试失败。全仓 `ActivateCandidateModel` 其余调用方（`agents.defaultHAActivateModel`、`ActivateDeploymentDirect`、`dnsha` 两处 owner 内部）都是 owner 自身的直通路径，正确。 |
| L1 | low | 已修 | **`deploy.targetByID` 吞掉 `NewTypedTarget` 的错误。** 位置 `deploy/adapter.go`。`t, e := NewTypedTarget(a)` 失败时 `t` 为空，随后 `t.ProfileID == ""` 分支用 `DefaultProfileID` 的结果覆盖 `e`，返回 `(空 Target, found=true, nil)`，发布链会对一个没有 ID/Record 的目标继续执行。实际只有 Agent 记录无法重新编码时才触发，可能性低。修复：错误时直接返回。由 deploy 整包回归覆盖。 |
| L2 | low | 需协调者裁定（测试已收紧） | **a19 语料 2 行差异未在授权清单中。** 位置 `gateway/corpus_test.go` apply 分支，`behaviour-changes.md` N8-11 W6 段末条。差异行是第 6945、6948 行：config 的 `nodes` 是对象 `{"x":{"routing-mark":…}}` 或嵌套数组 `[[{"routing-mark":…}]]`，Python 报 `Linux 网关与 节点.x.routing-mark 冲突`，Go 成功。核实：`model.Shared` 解码时类型回退，错型 `nodes` 只保留在 raw，`model.Config.Nodes` 为空，Mihomo 生成只读 typed `Nodes`，所以 routing-mark 不会进入输出，守卫失去对象，差异实质安全。但协调者的授权清单里没有这一条，作者只做了登记。原豁免条件也偏宽：只要求“畸形 nodes + Python 报 routing-mark”，没有限定行数。收紧：(a) 抽出 `malformedNodesApplyRow`，并断言豁免恰好命中 2 行，多一行就失败；(b) 新增通用检查：任何语料行的 config 被 `model.DecodeConfig` 拒绝时直接判失败（`NewFixtureModel` 会吞掉解码错误并换成空配置，可能让行空过）。运行结果是 0 行受影响，7494 行全部通过。请协调者裁定是否接受这 2 行；如果不接受，需要在 gateway 读取 raw `nodes` 做守卫（相当于 Python 仿真，不建议）。 |
| L3 | low | 记录 | **`publicDeployment` 在内部编码失败时返回 nil。** 位置 `deploy/adapter.go`。迁移后有 6 个错误分支返回 nil，HTTP 响应体或 client-devices 的 `deployment` 会静默变成 `null`；旧版不会失败。只有 `model.Deployment` 无法编码时才触发，所以没有改。 |
| L4 | low | 记录 | **`CommitConfig` 不清理请求 Scope 缓存。** 位置 `repo/config_commit.go`。它与被替换的 `UpdateProfileTransaction` 行为相同（都直接委托 `updateConfig`），没有回退。`gateway/draft` 提交后自己调用 `repo.ResetConfigContext`；`gateway/onboarding` 提交后不再从 Scope 读配置，所以目前没有可观测的影响。协调者设想的“包级版本 + 只删该 profile 的 Scope 缓存”没有实现。作者测试只覆盖 Scope 内回滚，没有覆盖成功提交后在同一 Scope 内读取。如果后续有调用方在同一请求内提交后再 `GetConfig`，需要补上。返回 revision = profile.Revision + shared.Revision，测试与 `ConfigRevision` 一致。 |
| L5 | low | 记录 | **`setup.abandonPlan` 不再把非对象绑定项判为 `ErrCorruptRecord`。** 位置 `setup/abandon.go`。绑定已改为 typed `ProfileAgentBindingsRecord`，名称/ID 非字符串时显示为空并回退到 ID。只影响放弃确认框里的设备名，不影响保留规则。属于类型边界的自然结果。 |
| L6 | low | 记录 | **`gateway/installation.decodeAgent` 内联了在线判断。** `agents.OnlineStatus/IsUsable` 被换成 `FreshHeartbeat(r.LastHeartbeat)` + `status`/`enabled` 判断。已逐条对照旧实现：`last_heartbeat` 非字符串时两边都视为不新鲜，`enabled` 只有 JSON `false` 才禁用，结论等价。记录下来，避免以后两份实现分叉；建议 W9 收回 agents 的 typed helper。 |

## 逐项核对

1. **Agent 协议与回执字节。** `TestDeploymentModelKeepsRawRequestAndReceiptBytes` 对 GET 原始请求（方法、空 body、无 Content-Type、Authorization）和回执原始字节（含 2^63 未知字段）做逐字节比较，严格，满足第二条常设项（原始请求对比）。发布 POST（tar.gz、`X-Deployment-ID`/`X-Content-SHA256`/`X-Activate`）的头部参数没有改，`DeploymentRequest`→`DeploymentRequestModel` 共用同一请求实现；作者在 `api/agents/difftest` 的 Flask 差分覆盖 body SHA256。`DeploymentResponse`/`PollResponse` 只改了类型。
2. **激活/HA 路径。** 见 H1。`installation` 使用 `ActivateDeploymentModel`，正确。`GenerateFinal` 的 pair 成员改为 `ReadHAPair` + `NewTypedTarget`，失败时 fail。`Publish` 删除旧 `pair` hook 后只剩 `nativePair`，`ErrPairPublisherMissing` 条件相应变为 `nativePair == nil`，fail-closed 语义不变。`RestartAgentService` → `RunServiceOperation(RestartService)`：仍先走 `HAServiceOperationTyped`，无 hook 时做 `haOperationConflict`，等价。
3. **回执 typed 化。** `ReceiptMatches` 直接传入 captured model，单次触发（`TestReceiptHookUsesCapturedModelsOnce`）。`receiptMatches` 从原始 JSON 字符串身份读取，非字符串 profile/sha 都失败，不会因显示投影误判。`LoadAgent` 的 DefaultProfileID 回退移到 `receiptAgent`，与旧 `Load` 一致。`advanceApplied` 改为 `EditAgents`：仍要求 applied_content 原始 JSON 相等、latest 已结束，回调末尾检查 `tx.Err()`。
4. **ctx/Detach 提交点（第四条）。** 新写入点：`advanceApplied`（AdvanceContent 先 Detach）、`installation.setDomain`/`bind`（`UpdateSystem`/`EditAgents` 回调内检查 `tx.Err()`）、`draft`/`onboarding` 的 `CommitConfig` 回调首尾都检查 `ctx.Err()`，repo `transact` 在 COMMIT 前再检查一次；`setup.abandon` 在 `UpdateSystem` 回调内检查。作者列出的真实 Runtime 回归（deploy `runtime_boundary_test.go` 等）没有被削弱，测试只改了类型。
5. **setup 旧会话。** 没有 `digest_version` 的会话：`Get` 可读；Prepare（已 prepared）→ `msgForeignEdit` 409；`AdoptOwnedWrite` 不再收养；completion 两处 → `msgNeedPublish`/`msgCoreChanged` 409，重新检查后会写入带版本的 checked。`TestOldWizardDigestConflictCanRecover` 覆盖读取 → 冲突 → abandon → 重新 Create → Get。所有 `OwnedDigest`/`CoreFingerprint` 比较点都有版本判断，没有遗漏。
6. **digest 确定性。** `UseNumber` 递归解码后用 `json.Marshal`（map 键排序，数字字面量原样保留，HTML 转义固定），输入是本函数自己产生的 JSON，结果确定；`TestNativeDigestsIgnoreObjectOrderAndKeepValues` 覆盖键序无关和大整数变化。
7. **Wire 负控。** `TestN811MissingGenerationCollaboratorFailsClosed`：空 Mihomo 环境下 `Wire` 后首次生成返回 `*convert.HookMissingError`，不会静默跳过。
8. **改成断言 native 差异的测试。** `onboarding/python_test`（null-policy `'None'`→`'null'`）、`rules/review_c09_test`（R5 行）对应显式 null policy 裁定；`server/setup_wire_test` 和 `gatewayonboarding/difftest`（Python 会话用 Python 哈希，Go 会话用带版本的原生哈希）对应 digest 裁定；`gateway/corpus_test` 的 2 行见 L2，未授权，已收紧并提请裁定。其余测试改动都是机械的类型迁移。
9. **pyref。** 15 个归档逐个解压比较：只有 `metadata.sources` 中 `rules/strategy.go`（mosdns/oxidns）或 `routersplit/recorded_test.go`（routersplit）的 SHA 变化，entries 与其余 metadata 全部一致。体积变化来自 gzip 重新压缩。
10. **被删 facade。** `SetPairPublisher`、`Receipts.Load`、`ApplyJSONX`/`RoutingMarkPathJSONX`/`SetRulesetContent`、`routers/legacy_wire.go` 都已没有生产调用方（构建与 vet 通过即可证明）；`legacy_gate_allow.txt` 只减不增。
11. **测试替身。** 新增替身（hooks、SessionReader、fake Agent）没有覆盖超时、锁或预算参数。

## 运行的命令（.64）

```
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'gofmt -l internal/api/agents internal/deploy internal/gateway; go vet ./internal/api/agents ./internal/deploy ./internal/gateway; go test -count=1 ./internal/gateway -run TestPythonCorpus -v; go test -count=1 -race -p 6 ./internal/api/agents -run "TestReviewN811|TestDeployHTTP"; <负控：路由改回 ActivateCandidateModel 后跑 TestReviewN811>; go test -count=1 -p 6 ./internal/deploy'
```

结果：gofmt 无输出，vet 通过；`TestPythonCorpus` PASS（7494 行，豁免 2 行）；api/agents race ok；负控 FAIL（符合预期）；deploy ok（194 s）。
