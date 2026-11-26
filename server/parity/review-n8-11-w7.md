# N8-11 W7 生成/导出簇 + proddiff/工具 + 存储布尔开关统一独立复核

复核对象：`go-n8-11-w7-generation`，覆盖 `go..f3bfec82c` 的六个提交（`0398b0836` 存储布尔开关 JSON 真值统一与 `oxidns` L4 修复、`4fd7073de` `configgen`/`api/generate`/`ippresence`/`oxidns`/`mosdns` 迁移、`b14bbdfc9` `convert`/`mihomoenv` 清理、`41c2dd5ba` `proddiff`/工具/`yamlxtest`/`aggregations`/棘轮与 `pyref` 刷新、`10ba489ca` `api/generate` 预览脱敏修复、`f3bfec82c` 进度与 `behaviour-changes.md`）。复核分支 `go-review-n8-11-w7`。

对照的是 Python 源码（`backend/common/config_repository.py`、`backend/routes/generate.py`、`backend/common/rule_ip_presence.py`、`backend/converters/common.py`、`backend/routes/tasks.py`、`backend/routes/rules.py`、`backend/utils/node_latency.py`、`backend/routers/desired.py`、`backend/agents/ipv6_*.py`、`backend/agents/rule_sync.py`、`backend/agents/oxidns_ha*.py`、`backend/utils/mosdns_archive.py`、`backend/notifications.py`）和基底 `go` 上的旧 Go 代码。协调者既定开关规则（键缺失 → 原默认值；显式 `false`/`null`/数值零/`""`/`[]`/`{}` → 假；其他 → 真）与既有批准项只核对实现，不重新讨论。按 Leaner verification 仅在 `.32`/`.64` 上运行修复相关定向测试、负控、少量高风险抽查及交回前完整预检，全部回归用例只用合成数据。

## 结论

high 0，medium 2（均已修复，附回归测试和负控），low 4（记录）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **`repo/import_conversion.go:949` 漏迁存储布尔开关，与同一函数其余三处矛盾。** 位置：`server/internal/repo/import_conversion.go:949`（`scopeImportedDialers`）。问题：同一函数中第 969、974、1014 行均已迁至 `model.SwitchEnabled(..., "enabled", ..., true)`，但第 949 行仍保留 `if node.Enabled != nil && !*node.Enabled { continue }`。对照 Python `backend/common/config_repository.py:2127`，原判断是 `if not node.get('enabled', True): continue`。影响：导入旧多配置时，若节点以非布尔假值停用（`enabled` 为 `0`、`-0`、`0.0`、`0e0`、`null`、`""`、`[]`、`{}`），`node.Enabled` 为 `nil`，第 949 行未能跳过该停用节点，反而继续校验其原始 `dialer-proxy` 并建依赖边；若目标缺失/非法会错误 400 拒绝导入（Python 直接跳过），或与下方第 969/1014 行的停用判定自相矛盾。作者的 `repo/switch_regression_test.go` 只覆盖了 `storedEnabled`，未覆盖 `scopeImportedDialers`。修复：第 949 行改为 `if !model.SwitchEnabled(node, "enabled", node.Enabled, true) { continue }`。测试：`internal/repo/switch_regression_test.go::TestScopeImportedDialersSwitchTruthiness` 覆盖 13 种 `enabled` 形态（缺失/`false`/`null`/`0`/`0.0`/`-0`/`0e0`/`""`/`[]`/`{}`/`1`/`"yes"`/`true`），断言假值跳过不报错、真值正常触发目标校验；负控（在远端副本改回 `node.Enabled != nil && !*node.Enabled`）确认失败，还原后通过。 |
| M2 | medium | 已修 | **`configgen.outdatedAgents` 在 `repo.View` 读锁内对每条 Agent 执行整条 `model.Marshal`（含完整部署历史）。** 位置：`server/internal/configgen/generate.go:233-242`（`outdatedAgents`）。问题：每次 Mihomo 预览/生成且 `subscription_hosts.dns.applied == true` 时，`outdatedAgents` 在 `r.View` 读锁内循环调用 `model.Marshal(agent)` 再 `json.Unmarshal` 到 `subs.CoreAgent`。`subs.OldCoreAgents` / `subs.AgentCoreVersion` 实际只读 5 个轻量字段（`ID`、`Name`、`ProfileID`、`ServiceType`、`DiscoveryEnabled`），不使用 `Deployments`、`AppliedContent`、`Raw` 或 `Fields`。对带 2 MB 部署历史的 Agent，每次调用在读锁内分配数十 MB 并拉长读锁持有时间（与 W1 复核 M1 同类）。修复：新增 `coreAgentOf(agent *model.Agent) subs.CoreAgent`，直接读取 `ID`/`Name`/`ProfileID`/`DomainDiscoveryEnabled`，仅用 `agent.MemberJSON("service_type")` 区分缺失/`null` 与显式 `""`，零全记录编码。测试：`internal/configgen/review_n811_test.go::TestReviewN811CoreAgentOfMatchesJSONDecode` 核对 8 种 `service_type`/`domain_discovery_enabled` 组合与整条 JSON 解码结果逐项等价；`TestReviewN811OutdatedAgentsSkipAgentHistory` 对比 1 条 vs 2000 条（约 2 MB）部署历史下的每次调用分配字节，要求差值不超过 64 KB；负控（远端副本改回 `model.Marshal`）确认超标失败，还原后通过。 |
| L1 | low | 记录 | **`mihomoenv/native.go` 默认缺省 `provider_update_interval` 时整条编码 `model.MihomoSettings`。** 位置：`server/internal/convert/mihomoenv/native.go:28-41`。当配置未显式设置 `provider_update_interval`（最常见默认路径）时，`settings.ProviderUpdateInterval == nil`，代码会调用 `model.Marshal(settings)`（编码整段可能几十 KB 的 `CustomConfig` YAML 文本和 `EasyTier`）再反序列化为 `map[string]json.RawMessage`，只为判断 `"provider_update_interval"` 键是否存在。建议给 `model.MihomoSettings` 增加 `MemberJSON` 单成员快速路径，留交后续清理。 |
| L2 | low | 记录 | **`convert.DefaultMihomoEnvironment().ValidateRules` 未对 `config`/`config.Profile` 做 nil 防御。** 位置：`server/internal/convert/subscription_environment.go:68-70`。直接访问 `config.Profile.RuleConfigs`；现有调用方均在入口检查了非 nil，但作为导出环境工厂的回调，防御性判空可避免未来调用方误传导致 panic。 |
| L3 | low | 记录 | **`ippresence.jsonStringMember` 未先 `bytes.TrimSpace` 原始 JSON 字节。** 位置：`server/internal/ippresence/typed.go:30-39`。判断 `string(raw) == "null"` 与 `raw[0] == '"'` 前未去首尾空白；虽然 `model.Raw` 解码出的成员通常已紧凑，但经 `SetJSON` 写入带空白的合法 JSON 字符串/null 时会误判。建议先 `bytes.TrimSpace(raw)`。 |
| L4 | low | 记录 | **`tools/proddiff/generator/main.go:161,166` 的 `RegionProbe` 与整条 `model.Marshal(agent)`。** 位置：`server/tools/proddiff/generator/main.go:161,166-178`。`opts.RegionProbe` 用 `agent.DomainDiscoveryEnabled != nil && *agent.DomainDiscoveryEnabled`（与旧 Go `GetBool` 一致，但 Python `python_generators.py:31` 用 `bool(agent.get('domain_discovery_enabled'))`）；第 166 行也用了 `model.Marshal(agent)` 解码 `subs.CoreAgent`。属离线比对工具，非生产热路径，留作记录。 |

## 逐项核对

1. **存储布尔开关规则实现（`model/switch.go`）与 L4。** `JSONSwitchEnabled` 正确处理键缺失/全空白 → `defaultValue`，`false`/`null`/任意 JSON 零值（逐字符扫描尾数与指数，覆盖 `0`/`-0`/`0.0`/`-0.0`/`0e0`/`0.00E+10`，且 `1e-500` 不会浮点下溢为 0）/`""`/`[]`/`{}`（含内部空白）→ `false`，其余值 → `true`。`SwitchEnabled` 在 `current != nil` 时走快速路径（代码显式修改优先于旧原始字节），否则调用 `record.MemberJSON(name)` 单成员读取，不编码兄弟字段；所有新增 `MemberJSON` 均处理 nil 接收者。`oxidns/adapter.go` `ruleNode` 已改为调用 `model.SwitchEnabled`，并由 `TestRuleNodeDoesNotFullMarshalWhenSwitchAbsent` 证明大 `extra_blob` 不增加分配。
2. **作者声称保留的"已批准不同规则"逐项核实（对照 Python 源码与 `behaviour-changes.md`）。**
   - `agents/registry.go:349`（`AgentEnabledModel`）：成立，N8-10 core-1 已批准 fail-closed（`n8-10-core.md:60`）；同时核实 `agents/rulesync/worker.go:206` 由 `rulesync/adapter.go:385` 先经 `AgentEnabledModel` 规范化，非漏改。
   - `agents/ha_model.go:12`（`oxidns_ha.enabled`）：成立，`behaviour-changes.md:2254-2258` 已批准不跟随+告警，且 Python `backend/agents/oxidns_ha.py:44` 原为 `is True`。
   - `oxidns/stored_projection.go:25-37`（`custom_matches[].enabled`）：成立，`behaviour-changes.md:1583-1584` 已批准仅接受布尔与数字 `0`/`1` 并记录 `FieldIssue`。
   - `mosdns/archive.go:136,173`（`literalFlag`）：成立，N8-9 DNS（`677929f8e`）已批准安全边界严格布尔 fail-closed。
   - `setup/record.go:95`（`completed=1`）：成立，`behaviour-changes.md:1976` 已批准仅 bool `true` 表示完成。
   - `convert/easytier.go:297,335`（`easyTierBool`）：成立，`behaviour-changes.md:1131` 已批准严格布尔校验。
   - `convert/common.go:70`（`DirectNTP`）：成立，Python `backend/converters/common.py:34` 原为 `is True`。
   - Python 原用 `is False`/`is not False` 的字段：`refresh/rows.go`（`backend/routes/tasks.py:63,70,84`）、`agents/ipv6/*`（`ipv6_readiness.py:105,167,441` 与 `ipv6_verification.py:42,118,165,568`）、`routers/desired.go`（`backend/routers/desired.py:24,28`）、`api/rules/compose.go`（`backend/routes/rules.py:363,406,471`）、`health/latency.go:188,218,273`（`backend/utils/node_latency.py:73,81`）——全部核对 Python 源码成立。`api/rules/read_adapter.go:321`（对应 `backend/routes/rules.py:1024`）经 `rawFlag` → `rules.EnabledField` → `model.JSONSwitchEnabled` 已统一。
   - 并行 W6 留存 3 处（`gateway/onboarding/config.go:96`、`setup/repository.go:66`、`server/setup.go:137`）属 W6 所有权，未越界修改。
   - 全仓 `*bool` 开关扫描发现 1 处漏改：`repo/import_conversion.go:949`（见 M1，已修）。
3. **Mihomo / Loon / Surge 精确字节门与 Mihomo HR 抽查。** `convert/generation_bytes_test.go` 仅把 `SetMihomoEnv` 换成 `SetMihomoEnvironment`，未改断言或放宽任何例外；抽查实测精确保持 `map[generate:2353 loon:1064 surge:1131]; explicit client exceptions: 107; explicit invalid Agent versions: 9; unknown: 0`。`convert/client_nodes.go:462` 已是原生 ASCII `^[0-9]+`，无非 ASCII 带宽数字 Python 仿真残留。HR 抽查 `convert/subscription_environment.go`、`convert/mihomoenv/native.go` 与 `configgen/generate.go` 发现 M2（已修）与 L1、L2。
4. **`api/generate` 预览脱敏。** 核对 `10ba489ca` 与 `redact/redact.go`、`redact/fields.go`：`redact.SanitizeExternalPayload(c.Context(), json.RawMessage(raw), json.RawMessage(rawSys))` 对 payload 走 `decodePublic` → `*orderedFields`（保留键序，不折叠 `api.http.auth` 等对象），对 `rawSys` 通过 `fieldValue` 支持 `json.RawMessage` 提取 `SensitiveSystemConfigFields` 并合并仓库 `RuleProxyTokens`/`AgentTokens`。核对 `backend/routes/generate.py` 与 `api/generate/{generate,artifacts}.go` 全部路由，无其他脱敏路径被折叠或漏遮。
5. **`configgen.RenderConfig` 与旧 `Render` 等价性。** 逐行对照旧 `Render` 与新 `RenderConfig`：五引擎分支、`baseURL`、错误类型、Mihomo `subscription_hosts`/`subscription_dialers`/`outdated_agents` 注入、`Build`/`Capture`/`materialize` 中的 `lifecycle.Detach` 与提交前 `ctx.Err()` 检查均等价；`configgen/adapter.go` 保留薄桥接 `Render` 供并行 W5 `api/config/live.go`。
6. **`ippresence` 非字符串 ID/正文 fail closed。** 对照 `backend/common/rule_ip_presence.py`：`content` 非字符串 Python 本即返回 `None`；`library_rule_id` 和 `rule_library[].id` 非字符串 fail closed 返回未知 `nil`，避免仿真 Python `True == 1` / `1.0 == 1` 跨类型匹配，且在 `r02_recorded.jsonl.gz` 上 524/524 全一致（`0 untyped-excluded`），行为变化已登记在 `server/parity/behaviour-changes.md`（N8-11 W7 节）。
7. **被删文件与调用方审计。** `oxidns/legacy.go`、`convert/pyhelpers.go`、`convert/mihomo_generate_py.go`、`convert/mihomoenv/legacy.go`、`yamlxtest/oracle.go` 均已删除，全仓 grep 确认无任何代码残留引用；`mosdns/archive.go` 与 `ruleproxy.go` 已去掉 `PythonClass` 判别改用 `errors.As` / typed 协作者；`legacy_gate_allow.txt` 棘轮净减 49 行（774 → 725，新增 0 行）。
8. **`proddiff`/工具与 `pyref` 审计。** `tools/proddiff/generator/{main,bundle}.go` 已切至 typed 生成入口，仅留 20 行 `deployTarget`/`deploySource` 桥接 W6 的 `deploy.NewTarget`/`NewSource`；作者的 baseline 自检（23/23 PASS、0 new / 0 stale）与隔离合成负控（修改 `MATCH` 策略 `DIRECT -> REJECT` 检出 mihomo/surge/loon 差异）有效。逐一解压对比 16 份改动的 `pyref` `.json.gz`：14 份（`oxidns` 8、`routersplit` 3、`mosdns` 3）`entries` 逐条完全一致，仅刷新 `metadata.sources`；`mosdns` 的 `TestCorpus.json.gz`（18040 → 17307）与 `TestN73SharedUpstreamMetadataMatchesPython.json.gz`（1 → 1，output 逐字节不变、input 保留 `enabled: 0`）系任务 B 撤回 `mosdns/r5_reference_test.go` 非布尔开关 R5 投影豁免所致，冻结语料本身未改动。
9. **测试替身。** 核查新增/修改测试，无测试替身覆盖生产参数的情况。

## 复核运行

### 1. M1 / M2 定向 `-race`、pyref 校验与负控（`.32`）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -e
cd server
test -z "$(gofmt -l internal/repo/import_conversion.go internal/repo/switch_regression_test.go internal/configgen/generate.go internal/configgen/review_n811_test.go)"
go vet ./internal/repo ./internal/configgen
go test -race -count=1 -run "TestScopeImportedDialersSwitchTruthiness|TestStoredEnabledSwitchTruthiness" ./internal/repo
go test -race -count=1 ./internal/configgen
PYREF_MODE=check go test -count=1 ./tools/pyref
# M1 负控：远端副本将 import_conversion.go:949 改回 node.Enabled != nil && !*node.Enabled → 8 项非布尔假值全部 FAIL，还原后 PASS
# M2 负控：远端副本将 generate.go 改回 model.Marshal(doc.System.Agents[i]) → 分配 56698108 bytes vs 87426 bytes FAIL，还原后 PASS'
```

```text
ok  	configflow/internal/repo	1.135s
ok  	configflow/internal/configgen	17.682s
ok  	configflow/tools/pyref	0.812s
== negative control M1: restore old node.Enabled check ==
    --- FAIL: TestScopeImportedDialersSwitchTruthiness/null (0.00s)
        switch_regression_test.go:85: scopeImportedDialers(null) unexpected error for disabled node: 旧配置 default 的原始拨号目标不存在或名称不唯一：MissingTarget
    --- FAIL: TestScopeImportedDialersSwitchTruthiness/zero (0.00s)
    ... (zero_float, neg_zero, zero_exp, empty_string, empty_array, empty_object 均 FAIL)
FAIL	configflow/internal/repo	0.014s
== M1 restored ==
ok  	configflow/internal/repo	0.014s
== negative control M2: marshal whole Agent ==
--- FAIL: TestReviewN811OutdatedAgentsSkipAgentHistory (1.87s)
    review_n811_test.go:129: outdatedAgents allocates 56698108 bytes with a large history (87426 with a small one)
FAIL	configflow/internal/configgen	1.882s
== M2 restored ==
ok  	configflow/internal/configgen	1.010s
```

### 2. 高风险抽查（`.64`）

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'set -e
cd server
go test -v -count=1 -run "^TestN88GenerationBytes$" ./internal/convert | tail -n 10
go test -race -count=1 -run "^TestPreviewOxiDNSPreservesAuthObjectAndRedactsSystemSecrets$" ./internal/api/generate
go test -count=1 -run "^(TestLegacyFacadeBoundary|TestLegacyGateNegativeControls)$" ./internal/lifecycle
go test -count=1 -run "^(TestRuleNodePreservesSwitchTruthiness|TestRuleNodeDoesNotFullMarshalWhenSwitchAbsent)$" ./internal/oxidns
go test -race -count=1 -run "^(TestJSONSwitchEnabled|TestSwitchEnabledRecordAndTypedOverride)$" ./internal/model
go test -count=1 -run "^(TestRecordedPythonTests|TestEdgeCorpus|TestCorpus)$" ./internal/ippresence'
```

```text
    generation_bytes_test.go:230: original Go byte comparisons: map[generate:2353 loon:1064 surge:1131]; explicit client exceptions: 107; explicit invalid Agent versions: 9; unknown: 0
--- PASS: TestN88GenerationBytes (11.24s)
ok  	configflow/internal/convert	11.337s
ok  	configflow/internal/api/generate	1.407s
ok  	configflow/internal/lifecycle	0.418s
ok  	configflow/internal/oxidns	0.011s
ok  	configflow/internal/model	1.012s
ok  	configflow/internal/ippresence	1.012s
```

### 3. 交回前完整预检（`.32`，`-p 8`）

```sh
RT_HOST=sunbroke@192.168.0.32 rt sync && ssh sunbroke@192.168.0.32 'bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-review-n8-11-w7 -p 8'
```

```text
~/rt/.paseo__worktrees__302rx1dg__go-review-n8-11-w7
== vet
== full non-race tests (-p 8)
PREFLIGHT PASS
```
