# N8-10 agents core-1 HR 复核（fresh Opus）

复核对象：`go-n8-10-core@7a3f1da4e`（产品提交 702b5b289、5b0afe1f7），作者 Codex 8019dda1。
复核分支 `go-opus-review-n8-10-core`。只读代码对照 Python（backend/agents）和迁移前 Go（bcbb6e573），
定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4），未重跑作者整套测试，未跑完整门禁。

## 结论

high 0，medium 1（已修），low 4（1 条已修，3 条记录）。core-1 可以 verified，修复随本分支叠批。
协调者转来的两点：(a) 成立（M1，已修）；(b) 不成立，有回归测试为证。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | `RememberPreparedContent` 只把 *proof* 的身份按原始 string 校验，*已存储* 的 deployment `profile_id`、Agent `service_type`/`profile_id` 仍用 model 文本投影比较。存储为数字 `7` 的 deployment profile 会与 proof `"7"` 匹配并写入 prepared_content。Python（`state['profile_id'] != prepared['profile_id']`）和迁移前 Go（`GetString`/`profileOf` 返回原值）都拒绝。与报告“数字/null 投影不能成为部署所有权证明”的说法不符。修复：存储侧三个身份只接受原始 JSON string；Agent profile 缺失/null/"" 仍取默认 profile，其他类型 fail closed（原 Go 对 0/false 取默认；该类型会被仓库的 profile 绑定校验拒绝，实际不可达）。deployment profile 只编码这一条 preparing 记录，不走历史。测试 `TestReviewN810PreparedOwnershipRejectsCoercedStoredIdentity`（修复前 `coerced stored identity accepted: <nil>`）。 |
| L1 | low | 已修 | 心跳版本持久化改为比较投影（`source.Version != payload.Version`），存储为数字/null/缺失、上报为同文本时不再写入。Python 是 `heartbeat[key] != agent.get(key)`，按类型和存在性判断；例如旧记录缺少 `config_version`、Agent 上报 `""` 时 Python 写入，core-1 不写。修复：在同一个缓存 View 内读取原始成员，string 按值比较，其他按 JSON token 比较。测试 `TestReviewN810HeartbeatPersistsReportedTextOverCoercedStoredValue`（修复前 `numeric stored version kept`）。分配上界测试仍通过。 |
| L2 | low | 记录 | `ReadHAPair` 的 profile 比较只接受 string/null。两边都是数字且相等的 profile，Python 和旧 Go（`jsonx.Equal`）视为成对，现在拒绝发布（“配对不完整，请先修复主备关系”）。数字 profile 写不进仓库（绑定校验“绑定的配置不存在”），实际不可达，fail closed 可接受。 |
| L3 | low | 记录 | `HAEnablingModel` 对 `enable` 为 `false`/`""`/`0` 等错误类型也算 enabling（旧 Go 按 Truthy 判为否）。只在存在非终态 HA journal 时阻止规则同步设置写入，符合作者“坏类型的保留 intent 不能授权编辑”的 fail closed 设计，不会锁死 UI：journal 结束后自动解除。 |
| L4 | low | 记录 | HA guard 分配测试按 allocs 计数而不是字节；`large > small+1` 对“编码整个 journal”不敏感，真正生效的是绝对上界 `large > 4`。负控已验证有效（见下），建议以后改为同时比较 `AllocedBytesPerOp`。 |

## 逐项核对

1. **live heartbeat overlay / 凭据 / 部署证明 / HA 身份**：overlay 只覆盖 7 个 RuntimeFields，与旧 `Update(entry.Fields)` 的键集合一致；部分心跳继承 version/service_status，system_metrics 拆出四个组件，与 Python 一致；delete generation mark 保留。注册凭据（O9-H2）不在 core-1 改动范围，manager/registration 未动。部署证明见 M1。HA 身份 `haPairIdentity` 与旧 `GetString` 语义等价。
2. **错误类型 Agent 记录仍可修改/删除（F1 类），以及协调者 (b)**：仓库写入校验拒绝数字 `id`（“Agent 中每一项必须有 ID”）和不存在的 profile 绑定，所以数字 id/profile 的记录不会存在；`GetAgentByID` 现在走 `registryAgent`（原始 string id），旧实现与 Python 也是严格相等，行为不变。HA 停用路径 `Disable` 使用 `s.get/s.all` 和 model 投影，不经过 `ReadHAPair`，因此严格配对不影响解除。回归 `TestReviewN810MalformedPairIdentityStaysRepairable`：`group_id` 或 `peer_id` 存为数字时，`pair` 拒绝发布，两台都能通过“停用”清除 `oxidns_ha`/`oxidns_ha_state`（peer 数字时每台各停用一次，与 Python 查找 peer 失败的行为一致）。
3. **第四条常设项**：`ObserveHeartbeat`、`EditAgents` 提交点、旧 runtime.sqlite3 导入（入口、逐文件删除、seed 前）、`New`、`metrics.AddSample` 都在 Detach 后检查 ctx；作者测试用真实 `lifecycle.NewRuntime/Start/BindRequest/Shutdown`，同时覆盖 shutdown 后不写、disconnect 后继续写。复核未发现缺口。
4. **凭据脱敏**：唯一新增日志 `logMetricsFailure` 用 `redact.CollectCredentials` + `SanitizeCredentials`，失败时整体替换为 `redact.Redacted`；用户名保留有测试。无私有脱敏。
5. **分配负控**：复核者临时让 `HAEnablingModel` 先 `model.Marshal(job)`，`TestTypedHAEnablePresenceAndAllocationBound` 失败（0 → 108 allocs/op，2 MiB → 204，`HA guard walks captured payload`），已还原未提交。心跳负控沿用作者记录（未重复）。
6. **测试期望 / pyref**：作者只新增 5 个测试文件，没有修改既有测试。12 份 pyref 的 `version` 与 `entries` 与基线逐项相等，只有 `metadata.sources` 中 4 个 model 文件 SHA 变化。复核修复刻意不改 `internal/model`，不需要刷新 pyref 元数据。

## 提交与测试

提交：本分支 `go(agents): require original stored text in prepared ownership and heartbeat diffs`（M1、L1 修复和 3 个回归测试），以及本报告/PROGRESS 提交。

rt 命令（均为 `RT_HOST=sunbroke@192.168.0.64`，在 `server/` 下）：

1. 修复前（产品文件还原到 7a3f1da4e，保留新测试）：
   `go test -p 4 -count=1 -run "ReviewN810" ./internal/agents` →
   `review_n810_test.go:45: coerced stored identity accepted: <nil>`；`review_n810_test.go:74: numeric stored version kept: "2"`；`FAIL configflow/internal/agents`。
2. HA guard 负控（临时变更）：`go test -count=1 -v -run "^TestTypedHAEnablePresenceAndAllocationBound$" ./internal/agents` →
   `HA guards unused=0: 108 allocs/op; unused=2MiB: 204 allocs/op`，`FAIL`。
3. 修复后：`test -z "$(gofmt -l internal/agents internal/model)"; go vet -p 4 ./...; go test -race -p 4 -count=1 -run "ReviewN810|Typed|Prepared|Preparation|Heartbeat|Deployment|RuntimeState|Version" ./internal/agents; go test -race -p 4 -count=1 -run "ReviewN810" ./internal/agents/dnsha; CONFIGFLOW_DIFF=1 go test -p 3 -count=1 ./internal/api/agents/difftest` →
   `ok configflow/internal/agents 151.334s`；`ok configflow/internal/agents/dnsha 2.016s`；`ok configflow/internal/api/agents/difftest 233.210s`。
4. 直接调用方：`go test -race -p 4 -count=1 -run "Publish|Prepar|Attest|PairGate|Capability|FinalResource" ./internal/deploy` → `ok configflow/internal/deploy 289.025s`。
5. 先前一轮（修复含 model 改动、后已撤回）的 dnsha 相关 race：`-run "ReviewN810|Disable|MemberIdentity|Pair|IdentityChange" ./internal/agents/dnsha` → `ok 187.613s`。
