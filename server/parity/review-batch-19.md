# Batch-19（N8-11 W8 / W8b / W8c 测试迁移）独立复核报告

- **复核分支**：`go-review-batch-19`（基线 `go` @ `57bbbcff0`，复核前 HEAD `b399e4a6f`）
- **复核范围**：`go..HEAD` 全部 20 个提交（w8、w8b、w8c 共改动 600 个文件：595 个 `*_test.go`、`server/internal/lifecycle/legacy_gate_allow.txt`、`server/internal/installer/testdata/pyref/TestReviewDeflateAndZipMatchPython.json` 与 3 份进度文档；未改动任何生产 `.go` 代码、冻结语料数据或 `pyref` 期望条目）。
- **复核目标**：确认测试覆盖没有被削弱。

---

## 1. 结论摘要

| 严重级别 | 发现数 | 已修复 | 说明 |
|---|---:|---:|---|
| High | 0 | 0 | 未发现大面积删测试、放宽 difftest 比较器或绕过门禁的问题 |
| Medium | 3 | 3 | F1：`monitor/cache_test.go` 上限边界被重复元素掩盖；F2：`monitor/a07_reference_test.go` 语料适配器短路生产 `CallCache` 校验；F3：`provider/recorded_test.go` 丢失对 `cfg` 入参不可变性的断言 |
| Low | 1 | 1 | F4：`monitor/a06_reference_test.go` 与 `a07_reference_test.go` 中 4301 位整数豁免未绑定原期望与命中次数 |
| **合计** | **4** | **4** | 全部已在本分支直接修复并验证通过 |

---

## 2. 详细发现与修复

### [Medium] F1：`monitor/cache_test.go::TestValidRequestTagsEnforcesBoundsAndUniqueness` 长度上限边界失效

- **位置**：`server/internal/agents/monitor/cache_test.go`
- **问题**：原测试 `TestValidRequestTagsFollowsPythonSetSemantics` 覆盖了 65 个元素超过 `CacheMaxTags`（64）上限的拒绝分支。改写后使用：
  ```go
  many := make([]string, 65)
  for i := range many {
      many[i] = "tag"
  }
  ```
  由于 `validTags` 同时校验 `len(tags) > CacheMaxTags` 与唯一性（`seen[tag]`），65 个完全相同的 `"tag"` 在第 2 个元素就会被去重逻辑拒绝——即使删除 `len(tags) > CacheMaxTags` 检查，该用例仍会通过；且缺少 64 个唯一合法 tag 应被接受的正向边界，以及 `nil` 切片用例。
- **修复**：构造 64 个唯一合法 tag（`tag-0`..`tag-63`，期望 `true`）与 65 个唯一合法 tag（`tag-0`..`tag-64`，期望 `false`），补齐 `nil` 用例，并保留原有两合法 tag、重复、空切片、非法前缀用例。

### [Medium] F2：`monitor/a07_reference_test.go::callCacheCorpus` 提前拦截 `validTags`，短路生产 `CallCache` 校验

- **位置**：`server/internal/agents/monitor/a07_reference_test.go`
- **问题**：`TestReferenceCacheCalls` 通过 `callCacheCorpus` 适配语料输入到 typed 生产函数 `CallCache(ctx, m, id, method, tags)`。但 `callCacheCorpus` 在调用 `CallCache` 之前提前执行了：
  ```go
  if !ok || !validTags(tags, true) {
      return nil, CacheFail("INVALID_REQUEST", 400)
  }
  ```
  这导致所有形状已是合法 `[]string`、但应由生产函数 `CallCache` 拒绝的语料行（空数组、重复 tag、超 64 个 tag、非法 tag 字符、以及 `GET` 携带非 nil tags）在测试辅助函数中被直接短路返回，**根本没有调用生产函数 `CallCache`**。
- **修复**：从 `callCacheCorpus` 中移除 `|| !validTags(tags, true)`，仅在输入无法转为 `[]string`（`!ok`）或含不可哈希 list/map 元素时由边界适配层报错，其余所有 `[]string` 用例全部交由生产函数 `CallCache` 实际执行校验。

### [Medium] F3：`provider/recorded_test.go` 丢失 `PrepareBundle` 对 `cfg` 入参不被修改的断言

- **位置**：`server/internal/provider/recorded_test.go`（`w.run` 中 `case "bundle":`）
- **问题**：迁移前测试记录了 `before := jsonx.DeepCopy([]any{config, main})`，并在调用 `PrepareProviderBundle(ctx, config, main, bundleOpts)` 后断言 `jsonx.Equal(before, []any{config, main})`，同时保证配置对象与主配置 YAML 图均不被调用方修改。迁移到 `PrepareBundle(ctx, cfg, mainNode, bundleOpts)` 后，只保留了 `beforeMain := yamlx.CopyGraph(mainNode)` 与 `!yamlx.Equal(beforeMain, mainNode)`，丢失了对 `cfg`（`*model.Config`）不可变性的检查。
- **修复**：在调用 `PrepareBundle` 前后分别序列化 `cfg`（`beforeCfg` / `afterCfg`），使用 `testjson.Equal(json.RawMessage(beforeCfg), json.RawMessage(afterCfg))` 与 `yamlx.Equal(beforeMain, mainNode)` 同时断言两个入参均未被修改。

### [Low] F4：`monitor` 4301 位整数豁免未校验原语料期望与精确命中次数

- **位置**：`server/internal/agents/monitor/a06_reference_test.go`（`TestReferenceAgentAnswers`）、`server/internal/agents/monitor/a07_reference_test.go`（`compareCall` / `TestReferencePanelCalls`）
- **问题**：两处对 CPython 4300 位限制退场后的 4301 位整数响应加了按响应体匹配的豁免分支（经核实原测试调用的旧 `request()` 包装内含 `jsonx.Parse` 触发 4300 位报错，生产 `monitorRequest`/`CallPanel` 本身保留该合法 RFC 8259 整数，符合 N8-11 §3.3 规则）。但两处分支未校验原语料期望确为 `INVALID_RESPONSE`（502），也未校验整个语料精确命中 1 次；若未来条件写错或语料变动可能静默失效或误吞其他错误。
- **修复**：在两处豁免分支中校验原语料期望确为 `error == "INVALID_RESPONSE"` 且 `status == 502`，并新增 `matched4301` 计数，在循环结束后断言 `matched4301 == 1`。

---

## 3. 四项核查明细

### 3.1 被删除的测试文件（共 8 个）

经 `git show go:<file>` 逐个核对，以下 8 个被删文件**均不含任何 `Test*` 测试函数**，全部是此前为旧 `*jsonx.Object` 测试代码保留的纯测试适配器/包装层；其调用方已全部迁移到对应包的 typed API，核心边界与错误类均已保留：

| 删除文件 | 原内容性质 | 核对结论 |
|---|---|---|
| `server/internal/agents/trafficstore/legacy_write_test.go` | 5 个 legacy 包装函数（`CleanReport`/`AddReport`/`SplitRouteKey`/`CachedStatus`/`decodeCleanedReport`），0 个 `Test*` | 调用方 `store_test.go`、`native_service_test.go`、`native_files_test.go` 已改调 typed `DecodeReport`/`Add`/`ParseRouteKey`/`StatusJSON`，原断言全部保留 |
| `server/internal/agents/metrics/flush_legacy_test.go` | 10 个 legacy 包装函数（`ValidateInterval`/`Run`/`FlushOnShutdown`/`LoadHistory` 等），0 个 `Test*` | `flush_test.go` 已改调 typed `FlushNow`/`SaveOnShutdown`/`LoadSavedHistory` 与 HTTP 边界（保留非法间隔 `120/900/"600"/true/nil/0` 检查） |
| `server/internal/agents/metrics/history_legacy_test.go` | 7 个 legacy 包装函数（`AddMetrics`/`GetMetrics`/`GetLatestMetrics` 等），0 个 `Test*` | `history_test.go`、`a04_reference_test.go` 已改调 typed `AddSample`/`ReadPoints` 等（`a04_reference_test.go` 使用 `DistinguishNumberLiterals: true`） |
| `server/internal/agents/metrics/stats_legacy_test.go` | 3 个 legacy 包装函数（`GetMetricsSummary`/`GetTrafficStats`/`GetTrafficTrend`），0 个 `Test*` | `history_test.go`、`a04_reference_test.go`、`stats_test_helpers_test.go` 已改调 typed `Summarize`/`TrafficStatistics`/`TrafficTrend` |
| `server/internal/agents/metrics/traffic_legacy_test.go` | 8 个 legacy 包装函数（`CleanTrafficReport`/`Query`/`RetentionDays` 等），0 个 `Test*` | `traffic_test.go`、`a05_reference_test.go` 已改调 typed `DecodeTrafficReport`/`QueryTraffic` 等（`a05_reference_test.go` 保留 `DistinguishNumberLiterals: true`） |
| `server/internal/agents/upgrades/legacy_test.go` | 16 个 legacy 包装函数与钩子（`Start`/`Poll`/`Follow`/`request`/`save` 等），0 个 `Test*` | `upgrades_test.go`、`address_test.go`、`native_test.go`、`a15_review_test.go` 已改调 typed `SetRequestForTest`/`SetReachesForTest`/`SetAskForTest` 等，断言逐条保留 |
| `server/internal/routers/task_legacy_test.go` | 1 个测试辅助函数 `ruleSyncRows`，0 个 `Test*` | 唯一调用方 `r14_service_test.go` 已改调 typed `ruleSyncTasks` |
| `server/internal/subs/legacy_helpers_test.go` | ~40 个纯 legacy 适配器函数，0 个 `Test*` | `subs/*_test.go` 已改调 typed `ValidateListenerNode`/`ValidateFilter`/`FilterProxies`/`ExtractHosts`/`ParseObject` 等 |

### 3.2 被改写/改名的测试、`jsonx.Repr` 替换与断言行数审计

- **全仓改名/改写的 `Test*` 函数仅 4 个**（静态遍历全部 138 个含测试包确认）：
  1. `agents/events_native_test.go`: `TestLegacyEventWindowCountsInvalidEntries` → `TestEventWindowCountsInvalidEntries` — 等价（`model.OperationEvent` 含 `Fields` 保留未知字段 `"future":1e400`，空结构占位测满 80 条滑出窗口）。
  2. `agents/monitor/cache_test.go`: `TestValidRequestTagsFollowsPythonSetSemantics` → `TestValidRequestTagsEnforcesBoundsAndUniqueness` — **发现 F1 并已修复**。
  3. `agents/rulesync/service_test.go`: `TestSettingsAndAgentViewNeverReadHistory` → `TestAgentSettingsStrictBooleans` — 等价（生产 `AgentSettings` 仅收 typed `*model.Agent`；仍在役的 legacy `repo.AgentHistoryNotRead` 守卫由 `agents/review_a13_test.go` 6 个完整用例覆盖）。
  4. `api/tasks/tasks_test.go`: `TestISO` → `TestTimestamp` — 1-to-1 等价覆盖（有效时间与 `nil` 边界）。
- **`jsonx.Repr` 逻辑断言全量核查**：全仓 diff 共筛出 24 个在比较/断言逻辑中移除 `jsonx.Repr` 的 hunk（19 个文件），逐个核查均已替换为等价的结构比较（`testjson.Equal`/`testjson.Diff`/`jsonEqual`）、JSON 序列化脱敏检查（`json.Marshal` + `strings.Contains`）或测试侧 `pyStringRepr`/`pyASCIIStringRepr`/`pyQuote`，未发现将精确相等降级为 `Contains` 或非空判断的情况。
- **断言净减少与不可变性检查全量核查**：对全仓 56 个 `rem_assert > add_assert` 文件（及 75 个非平凡 hunk）和所有移除 `.Has()` / `DeepCopy` 的 hunk 逐一复核：除上述 F1–F4 外，其余断言减少均来自删去多余的 `jsonx.Parse`/`jsonx.Compact` 辅助解析报错、`generation_bytes_test.go` 删去 typed 自身与自身二次对比、或调用已移除的同包测试包装函数（如 `HistorySupported`/`SupportsPaths`/`AppendRulePayloadAny`）。
- **门禁清单与 difftest 比较器**：`legacy_gate_allow.txt` 严格只删不增（`+0 / -545` 行，0 非测试行被删）；64 个 difftest 文件未新增忽略字段或放宽规则；`TestReviewDeflateAndZipMatchPython.json` 仅更新 4 个迁移 `_test.go` 的 `metadata.sources` SHA-256，未改 `entries` 或 `input_sha256`。

### 3.3 测试用例数与子测试数对比（`go` vs 本分支）

- **全仓静态函数级盘点（全部 138 个含测试包）**：`go = 4326` 个 `Test*` 函数，`HEAD = 4326` 个 `Test*` 函数（净变化 `0`，仅上述 4 个改名）。
- **抽查 12 个包 `go test -list .` 对比（`192.168.0.64`）**：`internal/agents`（224→224，1 改名）、`internal/agents/metrics`（76→76）、`internal/agents/monitor`（226→226，1 改名）、`internal/agents/rulesync`（28→28，1 改名）、`internal/agents/trafficstore`（35→35）、`internal/agents/upgrades`（49→49）、`internal/api/agents`（198→198）、`internal/api/tasks`（13→13，1 改名）、`internal/provider`（58→58）、`internal/rules`（140→140）、`internal/subs`（98→98）、`internal/substore`（131→131），全部数量一致。
- **抽查 11 个代表性包（覆盖 w8 / w8b / w8c）`go test -json -count=1` 顶层与子测试实测对比（`192.168.0.64`，含 `Fuzz*` 种子回归）**：

| 包 | `go` 基线 (`top` / `sub` / `total` / `skip`) | 本分支含修复 (`top` / `sub` / `total` / `skip`) | 对比结论 |
|---|---:|---:|---|
| `internal/agents/monitor` | 229 / 81 / 310 / 0 | 229 / 81 / 310 / 0 | 完全一致 |
| `internal/agents/rulesync` | 26 / 35 / 61 / 3 | 26 / 35 / 61 / 3 | 完全一致（3 skip 均为既有 `CONFIGFLOW_DIFF=1`） |
| `internal/agents/trafficstore` | 36 / 15 / 51 / 0 | 36 / 15 / 51 / 0 | 完全一致 |
| `internal/agents/upgrades` | 50 / 82 / 132 / 0 | 50 / 82 / 132 / 0 | 完全一致 |
| `internal/api/tasks` | 13 / 6 / 19 / 0 | 13 / 6 / 19 / 0 | 完全一致 |
| `internal/notifications` | 32 / 28 / 60 / 0 | 32 / 28 / 60 / 0 | 完全一致 |
| `internal/provider` | 60 / 124 / 184 / 0 | 60 / 124 / 184 / 0 | 完全一致 |
| `internal/rules` | 143 / 79 / 222 / 0 | 143 / 79 / 222 / 0 | 完全一致 |
| `internal/subs` | 99 / 87 / 186 / 0 | 99 / 87 / 186 / 0 | 完全一致 |
| `internal/substore` | 133 / 73 / 206 / 0 | 133 / 73 / 206 / 0 | 完全一致 |
| `internal/taskstatus` | 15 / 6 / 21 / 0 | 15 / 6 / 21 / 0 | 完全一致 |
| **合计** | **836 / 616 / 1452 / 3** | **836 / 616 / 1452 / 3** | **100% 一致（0 失败）** |

另额外实测本分支 `internal/routers`：`top=300 sub=2628 total=2928 skip=0 fail=0`。

### 3.4 留给 W9 文件的用例运行核查

- 静态检查全部 35 个留给 W9 的测试文件（w8b 8 个 + w8c 27 个）：均无新增 `//go:build` / `// +build` 约束，无新增 `t.Skip`。
- `agents/registry_test.go` 迁入 `agents/review_a13_test.go` 的 6 个用例与 `agents/runtime_state_test.go` 迁入 `agents/review_b10_heartbeat_test.go` 的 2 个用例，均已在 `.64` 上定向执行全部 `PASS`（`ok configflow/internal/agents 2.567s`，0 skip）。

---

## 4. 验证记录

### 4.1 格式化与定向测试（`192.168.0.64`）

1. `gofmt -l`（修改的 4 个测试文件）：`GOFMT_CLEAN`
2. 迁至 W9 留存文件的 8 个用例（`review_a13_test.go` 6 个 + `review_b10_heartbeat_test.go` 2 个）：
   ```text
   PASS
   ok  	configflow/internal/agents	2.567s
   ```
3. Legacy 门禁 `TestLegacyFacadeBoundary`：
   ```text
   ok  	configflow/internal/lifecycle	0.398s
   ```
4. 修复对应包测试（`./internal/agents/monitor`、`./internal/provider`）：
   ```text
   HEAD internal/agents/monitor: top=229 sub=81 total=310 skip=0 fail=0
   HEAD internal/provider: top=60 sub=124 total=184 skip=0 fail=0
   ```
   定向确认 4 个修复用例：
   ```text
   --- PASS: TestReferenceAgentAnswers (0.02s)
   --- PASS: TestReferencePanelCalls (0.05s)
   --- PASS: TestReferenceCacheCalls (0.01s)
   --- PASS: TestValidRequestTagsEnforcesBoundsAndUniqueness (0.00s)
   PASS
   ok  	configflow/internal/agents/monitor	0.093s
   ```

### 4.2 完整预检（空闲机 `192.168.0.64`，`-p 6`）

命令：
```bash
RT_HOST=sunbroke@192.168.0.64 rt sync && ssh sunbroke@192.168.0.64 'bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-review-batch-19 -p 6'
```

输出：
```text
~/rt/.paseo__worktrees__302rx1dg__go-review-batch-19
== vet
== full non-race tests (-p 6)
PREFLIGHT PASS
```
汇总：`138` 个含测试包全部 `ok`（0 `FAIL`，12 个无测试包）；其中修复涉及包 `internal/agents/monitor`（15.446s）、`internal/provider`（15.620s）、`internal/lifecycle`（0.717s）均全绿。
