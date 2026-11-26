# N8-11 W2 agents typed 边界 HR 复核（fresh Opus）

复核对象：`go-n8-11-w2-agents-typed`，P0 基底 `212aeac09` 之后的两个提交：`5d10d974e`（主体）和 `ce4bb4f76`（OxiDNS enabled 修复）。
复核分支 `go-opus-review-n8-11-w2`。注意：`go-n8-11-p0` 标签已经移到 `e7aba2586`（P0 rebase 并刷新 pyref metadata），
和 `212aeac09` 只差 pyref metadata 与文档，所以代码 diff 以 `212aeac09..HEAD` 为准。对照 Python（`backend/agents/oxidns_ha_publish.py`、
`backend/converters/oxidns.py` 等）和迁移前 Go。协调者已给的裁定（W2 文件范围、deploy/oxidns 越界授权、Receipts Stamp/AdvanceContent
留 W6、registration facade 留 W8/W9、monitor 只认 ASCII）只核对实现，没有重新讨论。按 Leaner verification，没有重跑作者整套，
只在 .64 跑了修复相关的定向测试和少量抽查，没有跑完整门禁。

## 结论

high 0，medium 0，low 8（2 已修、6 记录；其中 L3 需要协调者裁定，并且是跨包的一类问题）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| L1 | low | 已修（含测试） | **HA 规则更新的失败任务在 journal message 为空时丢掉 `message` 成员。** 位置 `dnsha/adapter.go` `rulesResult`。W2 把 jsonx 信封换成共用的 `monitor.RuleTask`，它的 `message` 带 `omitempty`。Python（`oxidns_ha_publish.py:537`）在 failed 时总是写 `code` 和 `message=job['message']`；旧 Go 也总写 `"message": ""`。journal message 为空时，新代码返回的 JSON 少了 `message` 字段，属于 Agent/HTTP 协议字段的存在性变化。修复：dnsha 改用自己的 `RuleTask`（`Message *string`），failed 时总是设置；`j == nil` 时不再解引用。测试 `TestReviewN811PairRuleTaskEnvelope`（表驱动：failed 空/非空 message、running、idle）比较 task 的完整 JSON 字节和 `ha_publication` 是否存在。负控：恢复修复前的 adapter.go 后，只有 `failed_empty_message` 失败（输出见下）。 |
| L2 | low | 已修 | **rulesync worker 在“重载成功”后重读 Agent 时没有处理 nil。** 位置 `rulesync/worker.go` `run`（`record, e = s.m.ReadAgent(...)` 之后）。旧代码对 nil jsonx 解码得到空 Agent，不会崩溃；新代码在 `comparison` 里对 nil 解引用 `a.AppliedContent` 会 panic。Agent 在保存 succeeded 状态和重读之间被删除时会触发。`Service.Run` 的 recover 会兜住，结果是一条 “worker panicked” 告警和一次 failure 路径（Agent 已不存在，failure 直接返回），没有写入。但这违反“请求路径/后台任务不 panic”。修复：nil 时直接返回（Agent 已删除，没有可同步的内容）。这个时间窗口没有测试 seam，没有单独加回归；rulesync worker 相关测试已通过。 |
| L3 | low | 记录，**需协调者裁定**（跨包同类） | **OxiDNS 规则开关只有布尔 true 才算启用，非布尔真值（`1`、`"yes"`、非空列表/对象）和 Python 不一致。** `ce4bb4f76` 让 `oxidns.ruleNode` 和已有的 `RuleEnabled`/`enabledOrDefault`（`isTrue` 只认 bool true）一致：缺失 → 启用；null/0/空串/false → 禁用，这三例 Python 差分现在通过，符合授权。但 Python 是 `item.get('enabled', True)` 真值判断，所以 `enabled: 1` 在 Python 中启用，在 Go 的生成器和 rulesync `active` 中都是禁用。修复前 P0 的 typed 生成器恰好把它当启用（nil → true），所以这次修复对“非布尔真值”方向造成了生成结果变化。`rulesync/signatures.go:22` 的注释写的是 Python 真值语义，与实现不符；ledger N8-5c 也写“`enabled` 存为 `0` 等非布尔值时按其真假判断（与 Python 相同）”。只有手工或导入的非布尔数据会触发，前端只写 bool。**同一类问题（显式假值被当作缺失 → 启用）还在 W2 以外存在**：`rules/matcher.go:552`（规则匹配）、`discovery/repository.go:228`、`convert/common.go:47`（节点/组/聚合的 `enabled`）、`provider/delivery.go:71`、`routers/ipv6_plan.go:190`、`notifications/notifications.go:174`，`enabled: 0/null/""` 都按启用处理，而 Python 是排除。建议：协调者决定 OxiDNS/规则开关是按 Python 真值，还是按“只认 bool，其他非布尔值登记为 native 差异”；然后由 W4/W7 的所有者统一做一次扫描（`model` 解码后保留原始 enabled 证据，或者提供一个共享的 `model.StoredEnabled` 辅助函数），不要在每个调用方各写一份。本轮没有改。 |
| L4 | low | 记录 | **`ruleNode` 在开关为 nil 时，每条规则都完整 `model.Marshal` 一次。** 位置 `oxidns/adapter.go:154`。生成 DNS 配置时，每条缺少 enabled 的规则都要编码整个 RuleConfig（含 raw），只为了读两个成员。规则多时，这会在生成/规则同步的计算路径上增加分配。`model` 已有 `recordMemberJSON`（Agent/HA 记录都暴露了 `MemberJSON`），给 RuleConfig 加一个 `MemberJSON` 即可只读取单个成员，但 model 不属于 W2。可以和 L3 一起处理。 |
| L5 | low | 记录（既有） | **ipv6 `systemSecrets` 把整个 `*model.System` 传给 `redact.ExternalPayloadSecrets`。** 位置 `ipv6/adapter.go:288`。`fieldValue` 只识别对象/map/RawMessage，并且 `rule_fetch_proxy` 位于 `system_config` 下面，所以这个字段不会进入秘密集合（rule proxy token 由仓库另行收集，不受影响）。迁移前用 `repo.GetSystemConfig` 返回的整个 system 节，同样找不到这个字段，所以**不是 W2 回退**。作者在 rulesync 已经改成传 `SystemConfig` 的 JSON 投影（`systemSecrets`），ipv6 可以用同样的写法。没有改：会改变 IPv6 诊断文本的脱敏结果，需要单独回归。 |
| L6 | low | 记录 | **`n811NumericPanel` 绑定的三条请求被从语料删除时不会失败。** 位置 `api/agents/difftest/panel_diff_test.go`。绑定本身是严格的：名称 + 完整 path + 空 body；Python 必须是 200 + `success:true` + 1 次 Agent 调用；Go 必须是 400 + `INVALID_REQUEST` 原文 + 0 次调用 + 三个响应头相同。但如果有人删掉这三条请求，只会少覆盖，不会报错（N8-10 的 metrics 绑定有“请求被删除时失败”的检查）。不会造成差异被放过。 |
| L7 | low | 记录（风格） | `agents/hooks.go:56/68/80` 的字段注释仍然以已删除的 `AddressFollow`、`HAServiceOperation`、`InstallationProfile` 开头，说明的却是 Typed 字段；`deploy.Receipts.LoadAgent` 复制了 `receiptAgent` 的默认 profile 查找，并且对编码错误返回错误（旧 Load 会忽略），可以合并成一个 view 构造函数。都不影响行为。 |
| L8 | low | 记录 | **`Manager.ReturnRoutingApply`（W5 facade）的 payload 编码错误类别变了。** 位置 `agents/remote.go`。旧代码由 `jsonBody`→`EncodeDevice` 拒绝非有限数，返回 `TransportError(msgInvalidDevicePayload)`；新代码先 `json.Marshal(spec)`，失败时返回原始 json 错误。spec 由 Go 内部的路由器配置生成，不来自用户浮点输入，正常情况下不会出现。W5 迁移 routers 调用方时会直接调用 `ReturnRoutingApplyModel(raw)`，这个 facade 届时删除。 |

## 逐项核对

1. **typed 迁移与字段/协议/存储。** rulesync：`decodeAgent`/`agentView` 删除后，直接使用 `ReadAgent`/`AgentSnapshots`/`AgentPair`（快照为副本）。`SettingsFor`→`AgentSettings` 使用同一个 `model.RuleSyncSettings` 的容错解码，错型时仍然关闭；`snapshotVersion` 保留 `applied_content.config_version` 的存在性语义；`workerAgent` 的 enabled/profile 覆盖不变；`publish` 用 `NewTypedTarget(record)` 代替 `NewTarget(record)`，记录内容相同。dnsfollow：任务行的 enabled 用 `JSONHasContent`（Python 真值），history 用 map 是否存在判断；行的脱敏仍然收集所有 Agent 的凭据（`token` 是 credential key），名称取原始字符串。dnsha：`GetDeploymentDirect`→`ReadDeploymentModel`、`ActivateDeploymentDirect`→`ActivateCandidateModel` 是同一个 owner（不经过 HA hook，没有递归）；拒绝包用 `SetJSON("success", false)` 保留字段存在。冲突包的键序变化只影响字节，HTTP 经排序输出，dnsha difftest 通过。presence：`ObserveRegistryPresence` 读取 `MemberJSON("status")`，`Raw.MemberJSON` 对修改过的字段返回当前值，所以 `ListAgents` 派生的 offline 会生效；名称/心跳的富结构先脱敏再渲染。ipv6：`MihomoTemplate()` 每次新建节点，没有共享可变状态。
2. **心跳/注册/部署回执/规则同步快照。** 生产心跳与注册路由没有改（`api/agents` 生产代码只改了 `client_tools_adapter.go` 的 `GetNative`）。`receipts.LoadAgent` 和 `Load` 使用同一个 `receiptAgentView` 解码和 `loadView`；`FetchSnapshot` 的请求参数（3/90 s、NoProxy、NoRedirects、MaxBody、Bearer）不变，`monitor.Origin` 是 `AgentOrigin` 的 native 版本。
3. **旧 hook 删除的负控。** `Hooks` 删除 `AddressFollow/AddressConfirmed/InstallationProfile/InstallationRegister/HAServiceOperation`；`HADeployment/HAActivate` 改为 `*model.Deployment` 签名，旧签名直接编译失败。`TestRetiredHooksFailCompilation` 对每个旧字段用 go/types 检查“unknown field”，并且有正控（`AddressFollowTyped`）。生产代码中旧名字只出现在注释里（L7）。`monitor.SetHARules`→`SetHARulesJSON`；`deploy.SetPairPublisher` 保留，作为兼容入口。
4. **单次触发。** `deploy.Publish`：nativePair 存在时只调用它，不再调用旧 pair；两者都为 nil 才返回 `ErrPairPublisherMissing`。`TestNativePairOwnerTakesPrecedenceAndRuntimeGuards` 同时注册两者，断言 native 1 次、legacy 0 次。`RegisterHooks` 改为替换，`TestRegisterHooksReplacesNativeOwner` 覆盖。
5. **锁/Spawn/ctx（第四条）。** 没有新增 flock，没有新增裸 `go`。`serviceOperation` 在 Detach 后补了 `ctx.Err()`；dnsfollow `saveDraft` 改用 typed `UpdateProfile`，在回调开头和赋值前检查 `tx.Err()`；新的 feature 请求经 `AgentRequestModel`（非 GET 先 Detach）。真实 Runtime 测试：`TestNativeFeatureWritesFollowRuntimeLifetime`、deploy 的 pair owner 测试（断线后完成，关机后 `context.Canceled` 且 owner 不被调用）。
6. **deploy 越界改动只加不改。** `receipts.go` 只抽出 `loadView` 并新增 `LoadAgent`，`Load` 行为不变；`publish_hooks.go` 新增 `nativePair` 槽和 `SetPairPublication`；`publish.go` 只在原 pair 分支前加 native 分支，并把 fail-closed 条件扩展为“两者都缺”。旧入口保留。`oxidns/adapter.go` 只改 `ruleNode` 的两个开关（L3/L4 是对这个改动的说明）。
7. **ASCII 数字边界与登记。** `asciiDigits`（logs after，≤18）、`decimalInteger`（`^[+-]?[0-9]+(?:_[0-9]+)*$`，TrimSpace 和旧的 Unicode 空白转换效果一致，\x1c–\x1f 两边都不剥离，去掉符号/下划线后最多 4300 位）、metrics 的 `sampleRE` 加值内空白检查（与旧 Unicode `\S` 等价：任何 Unicode 空白都使样本不匹配）、时间戳只认 ASCII。删除了 pyIP/pydigit/pydecimal 表和 `pyDecimalValue`。behaviour-changes 已加 N8-11 W2 段；冻结语料不改，`n811_ascii_native.json` 按原行 SHA 绑定。
8. **心跳分配阈值。** diff 中没有任何 B/op、allocs 阈值的修改；抽查 `TestBatch10HeartbeatAllocationCeilings` 通过。
9. **差分绑定。** rulesync 的 `TestProjectionAgainstPython`（含 3 个 falsy-enabled 用例，CONFIGFLOW_DIFF=1）抽查通过；panel 的数字拒绝绑定是严格的（L6 是覆盖保持问题）；dnsha difftest 只把 `UpdateHeartbeat` 机械换成 `ObserveHeartbeat`。作者报告里 gate.sh 默认不跑 rulesync 根包 Python 差分的分析是对的，修门禁留给协调者。
10. **测试替身与原始请求。** `TestNativeFeatureRequestsKeepRawWireContract` 比较六条路径的 method/path/headers/完整 body 字节和超时原值，替身不覆盖生产 timeout。
11. **棘轮门禁。** `legacy_gate_allow.txt` 只有删除（27 行），没有新增。
12. **无 Python 仿真。** 新代码没有 jsonx/pystr 新导入；monitor 的 CPython 表已删除；测试专用 codec 放在 `_test.go`。

## 复核运行（.64）

修复后：

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'test -z "$(gofmt -l internal/agents/dnsha internal/agents/rulesync)" && echo gofmt-ok && go vet ./internal/agents/dnsha/... ./internal/agents/rulesync/... && echo vet-ok && go test -race -count=1 -timeout=30m -run "TestReviewN811PairRuleTaskEnvelope|Rules|Monitor" ./internal/agents/dnsha 2>&1 | tail -3 && go test -race -count=1 -timeout=30m -run "TestWorker|TestNative|TestEditDuringRefresh|TestBindingChange|TestRuleSyncRuntime|TestStructurePublication" ./internal/agents/rulesync 2>&1 | tail -3 && CONFIGFLOW_DIFF=1 go test -count=1 -timeout=30m ./internal/agents/dnsha/difftest 2>&1 | tail -3'
```

```text
gofmt-ok
vet-ok
ok  	configflow/internal/agents/dnsha	54.491s
ok  	configflow/internal/agents/rulesync	69.395s
ok  	configflow/internal/agents/dnsha/difftest	16.734s
```

L1 负控（在本机临时换回修复前的 `dnsha/adapter.go`，同步运行后恢复）：

```text
== control: pre-fix adapter
--- FAIL: TestReviewN811PairRuleTaskEnvelope (0.00s)
    --- FAIL: TestReviewN811PairRuleTaskEnvelope/failed_empty_message (0.00s)
        review_n811_test.go:48: task = {...,"code":"RULE_UPDATE_FAILED"}, want {...,"code":"RULE_UPDATE_FAILED","message":""}
FAIL	configflow/internal/agents/dnsha	0.017s
```

抽查作者用例（同一副本，`--no-sync`）：

```sh
go test -run '^TestRuleNodePreservesExplicitDisabledFlags$' ./internal/oxidns
CONFIGFLOW_DIFF=1 go test -run '^TestProjectionAgainstPython$' ./internal/agents/rulesync
go test -run 'TestRetiredHooksFailCompilation|TestRegisterHooksReplacesNativeOwner|TestBatch10HeartbeatAllocationCeilings|TestNativeFeatureRequestsKeepRawWireContract' ./internal/agents
go test -run 'TestPanelASCIIIntegerRules|TestMonitorMachineNumbersRejectNonASCII' ./internal/agents/monitor
go test -run 'TestNativePairOwnerTakesPrecedenceAndRuntimeGuards' ./internal/deploy
```

```text
ok  	configflow/internal/oxidns	0.012s
ok  	configflow/internal/agents/rulesync	1.133s
ok  	configflow/internal/agents	7.655s
ok  	configflow/internal/agents/monitor	0.010s
ok  	configflow/internal/deploy	0.233s
```

没有跑 agents 整包 race、完整门禁或 oxidns 整包（pyref metadata 失配由 P0 处理）。
