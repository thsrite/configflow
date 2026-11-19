# N8-10 agents core-2 HR 复核（fresh Opus）

复核对象：`go-n8-10-core-2@a75dd4a1d`（基于 go@d9515f7fe，20 个提交），作者 Codex 8019dda1。
复核分支 `go-opus-review-n8-10-core-2`。对照 Python（backend/agents）和迁移前 Go（d9515f7fe）阅读代码。
定向测试只在 .64 经 `rt` 运行（GOMAXPROCS=4，-p 4）。没有重跑作者的整套测试，也没有跑完整门禁。

## 结论

high 0，medium 1（已修），low 2（记录）。core-2 可以 verified，修复随本分支叠批。
需要协调者裁定 1 项（见文末）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **重注册保留了 Python 会丢弃的成员（未登记的行为变化，并延长了凭据能力）。** `setRegistrationMembers` 刷新已有 Agent 时，除了 Python `register_agent` 固定列表里的成员，还保留了 `docker_ha_installation_id`、`node_test_dns_endpoints`、`service_status`/`system_metrics`/`cpu`/`memory`/`disk`/`network`、值为假的 `gateway_installation_id`，并用 `a.Fields = original.Fields` 保留了全部未知成员。Python 和迁移前的 Go 都按固定列表重建记录，这些成员都会丢弃，而 behaviour-changes 里没有登记这项变化。安全影响：Docker HA 安装（`installer/docker_ha_enrollment.go:460`）按 `docker_ha_installation_id` 找到“自己的” Agent 后会返回它的 token。在 Python 里，设备第一次 /register 之后这个标记就没了，同一个安装凭据再重放会得到 409；core-2 下，在会话过期前它可以一直取回这台已注册 Agent 的 token。修复：刷新时只写 Python 列表里的成员；`gateway_installation_id` 只在有内容时保留（Python 的 truthy 规则，用共享的 `model.JSONHasContent`）；不再复制未知成员。回归测试 `TestReviewN810ReregistrationKeepsOnlyPythonMembers` 修复前失败（5 个成员被保留，`re-registration kept docker_ha_installation_id="enrollment-session"` 等），修复后通过。作者新增测试 `TestTypedEnrollmentKeepsUnchangedStoredTokens` 原先断言未知成员 `future` 会保留，这条期望本身就是未登记的变化，已改为断言丢弃；其余三项保留期望（config_version 数字原文、traffic_history_enabled 错误类型原文、deployments 嵌套原文）没有改。 |
| L1 | low | 记录 | 安装重放（`ReplayInstallation`）的身份逐项比较，现在用的是 `RegistrationInput` 里**补过默认值**的成员（name→"Unnamed Agent"、port→8080、service_type→"mihomo"、host→"" 、profile_id→null）。迁移前的 Go 比较的是请求原始值，缺失就是 nil。因此，如果请求省略了 port，而存储的 port 是 8080，原来返回 409，现在会通过并回显 token。这一层是 Go 独有的纵深检查：网关 `register.go` 在调用前已经用 `match(r, existing, …)` 按会话核对过身份，受支持的入口也总会注入完整字段，所以实际不可达。建议后续在 `RegistrationInput` 中保留请求成员是否出现，比较时用原始成员。 |
| L2 | low | 记录 | 带 token 的重注册如果显式提供了 profile，而 Agent 现存的 profile 绑定已损坏（R5 错误类型）：Python 和迁移前的 Go 在计算当前 profile 时会报错；现在只要没有进行中的发布或升级就会继续，并写入新的 profile。这和作者为管理编辑登记的“显式编辑可修复 R5 profile”一致，需要 token 证明，有进行中任务时仍然拒绝。可以接受，但 behaviour-changes 里只登记了编辑入口，建议补一句注册入口。 |

## 逐项核对

1. **O9-H2 typed claim**：对比 `admitRegistrationClaim` 与 d9515f7fe 的 `admitRegistration`，下面几项都保持不变：
   - 发号：32 字节随机值，只持久化 SHA-256，24h，最多 100 个会话，按到期时间淘汰。
   - 恒时比较：`subtle.ConstantTimeCompare` 比较 digest。
   - 同事务消费：只修改外层 `editRegistry` 事务里的 system 工作副本，与 Agent 插入一起提交。
   - 失败不消费：`matchesRegistrationClaim` 在 probe 副本上检查，带 AllowNew 的路径不匹配时返回 `""`。
   - 匿名请求遇到已有身份时，无 proof 返回 `ErrRegistrationAuthorization`（403，不暴露 name/host 是否已注册），有匹配 proof 返回 PermissionError。
   - 带 existing token 重注册时，可选 proof 照样消费。
   - 新增的收紧都是 fail closed：会话的 name/host/service_type/deployment_method/profile_id 必须是原始 string，port/expires 必须是原始 number；请求 port 为 `true` 不再等于 1；非字符串 name 等字段视为 invalid claim。
   - shutdown/断开/备份不复活：原 O9 回归在本轮定向运行中全部通过（`^TestO9` 包含在下面第 2 条命令里）。
   - `tokenMatches` 要求存储 token 非空、两边都是 ASCII，并且恒时比较，不比旧 `ConstantTimeASCIIEqual` 宽松。
2. **存储侧身份只认原始 JSON string**：抽查了以下几处，均先用 `storedText(...MemberJSON)` 读取原始 token，失败即拒绝，没有使用 R5 投影：
   - `prepared.go`（proof 与存储侧的 profile/service）
   - `deployment_actions.go`（`deploymentTaskIDMatches`、`deploymentProfileMatches`、`originalService`）
   - `deployment_record.go`（完成确认时的 deployment_id/profile_id/service_type）
   - `registry.go`（`agentIDProven`、`ReadAgentByToken`）
   - `mutations.go`（`agentProfileBinding`：缺失或 null 取默认 profile，其他类型返回 validation 错误）

   端口相等只比较两边都是 JSON number 时的整数值。core-1 M1 的回归测试保留并通过。
3. **来自 Agent 的 JSON 精确键名**：在本轮 diff 新增的非测试 `json.Unmarshal` 中，凡是 Agent 输入，都解到 `model.Fields`（`parseObject`：精确键、拒绝重复键）、标量，或实现了 `UnmarshalJSON → DecodeRecord` 的 model record（Agent/Deployment/HAPublicationSummary 等）。`deploy.File`/`Manifest` 去掉了 `type plain` 的默认 struct 解码，改为 Fields 精确读取，字段集合与结构体一致。`rulesync.nativeTask` 有自定义的精确键解码器。其余是服务端自己 `model.Marshal` 再转码的内部 codec，不是外部输入。没有发现 encoding/json 对外部输入做默认 struct 解码。
4. **Unicode17 / 版本号**：
   - 在 .64 从 unicode.org 重新下载 UCD 17.0.0 的三个文件，SHA 与 fixture 的 `sources` 一致。用作者的生成脚本独立重算，得到的 fixture SHA 完全相同（`9407e75f…f268`，55 lower + 307 sigma）。生成脚本只读 UCD 和原 A05 存档，不依赖 Go 输出。
   - 原 A05 存档 SHA 没变，测试对全部码点（0..0x10FFFF）比较；未绑定的码点仍比较原 A05 值。测试硬断言 `cases.UnicodeVersion == "17.0.0"`（.64 的工具链为 go1.27.4）。
   - 负控：临时把 `lowerTrafficDomain` 改成 `strings.ToLower` 后，`TestA05ReferenceLowerEveryCodePoint` 和 `TestA05ReferenceFinalSigmaContext` 都失败；已还原，未提交。作者的 `TestNativeUnicodePolicyRowsAreActuallyChecked` 对每条绑定行都做了旧值和伪造原值的反例。
   - 版本号：非 ASCII 分量（含 Unicode Nd）无法通过 `taskstatus.Count` 的 ASCII 十进制正则，按裁定退回文本比较；空分量不会被当成 0。
5. **心跳阈值 / ctx / 脱敏 / 期望**：
   - `TestBatch10HeartbeatAllocationCeilings` 没有修改。修改过的测试只有 `registry_native_test.go` 的 HA guard 分配测试，它是按 core-1 L4 建议加强的（同时断言 B/op 和 allocs）。
   - `RegisterIdentity`、edits、deployments 都先 `lifecycle.Detach`，提交前检查 `tx.Err()`。
   - 新增的脱敏都通过 `internal/redact`（`CollectCredentials`/`SanitizeCredentials`/`Redacted`），diff 里没有私有的脱敏实现。
   - 对比 12 份 pyref 的 base 与 head：只有 `metadata.sources` 变化，entries 完全一致（共 18,444 条）。没有修改或删除任何既有 testdata/corpus/golden。
   - 10 个修改过的既有测试，都是改走“完整存档 SHA + 单行 SHA 封闭绑定”的原生比较，或者加强断言。抽查 `verifyNativeRemoteReference`：只对 9 条按名字列出的行生效，同时核验原始 body/status、Python 类别和文本、事务无写入，其他行仍按原期望比较，不会空过。
6. **部分完成清单**：报告开头的“仍动态或延后”清单与代码一致。`deployments.go`、`manager.go`、`remote.go` 等仍引用 jsonx，但只是 Record facade 和 codec（例如 `RecordDeployment` 转给 `RecordDeploymentModel`）。monitor、upgrades、installer、api/agents binder、A05 SQL engine 都明确列为延后。没有发现把未迁移部分写成已完成。

## 需要协调者裁定

- **`node_test_dns_endpoints` 在重注册时是否保留。** M1 修复后它会被丢弃，与 Python 和迁移前的 Go 一致。但它是节点测速 DNS 端点的清理簿记（`node_test_dns_cleanup.py`），丢掉后，已下发到设备的临时端点可能失去清理依据，这像是 Python 的 bug。可选：(a) 维持与 Python 一致（当前状态）；(b) 作为 Python bug 修复，在刷新时保留它，并登记 behaviour-changes。Docker HA 标记和未知成员不建议保留（见 M1）。

## 提交与测试

提交：本分支 `go(agents): drop non-Python members on Agent re-registration`（M1 修复和回归测试，以及本报告）。

rt 命令（均为 `RT_HOST=sunbroke@192.168.0.64`，在 `server/` 下执行）：

1. 修复前：`go test -p 4 -count=1 -run "ReviewN810Reregistration" ./internal/agents` →
   `re-registration kept docker_ha_installation_id="enrollment-session" … node_test_dns_endpoints … service_status … gateway_installation_id="" … future={"number":2.00}`，`FAIL`。
2. 修复后：`test -z "$(gofmt -l internal/agents)" && go vet -p 4 ./internal/agents ./internal/api/agents ./internal/gateway/... ./internal/installer && go test -race -p 4 -count=1 -run "ReviewN810|Registration|Register|Enrollment|AgentMutations|ProfileGuard|AutoHostPin|^TestO9|N810Registration|N810Stored|DockerHA|Installation" ./internal/agents ./internal/api/agents ./internal/gateway/installation ./internal/installer` →
   `ok agents 68.365s`；`ok api/agents 121.052s`；`ok gateway/installation 3.876s`；`ok installer 42.400s`。
3. `CONFIGFLOW_DIFF=1 go test -p 2 -count=1 -run "^TestDifferentialAgainstFlask$|DockerHA" ./internal/api/agents/difftest` → `ok 11.786s`。
4. Unicode 独立复算：从 unicode.org 下载 17.0.0 UCD 后运行 `~/cf-test/.venv/bin/python -I internal/agents/metrics/testdata/gen_n810_unicode_native.py <ucd>` → `lower=55 sigma=307`，输出 SHA `9407e75f…f268`，与提交的 fixture 相同。
5. Unicode 负控（临时改动，已还原）：`go test -run "A05ReferenceLowerEveryCodePoint|A05ReferenceFinalSigmaContext|NativeUnicodePolicy" ./internal/agents/metrics` → 两个 A05 测试 `FAIL`。
