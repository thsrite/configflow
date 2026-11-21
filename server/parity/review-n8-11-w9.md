# N8-11 W9 删除与门禁清零 HR 复核（fresh Opus，O7 终审）

复核对象：`go..c834eb228` 的 11 个提交（5ac4af1c6 … c834eb228），复核分支 `go-opus-review-n8-11-w9`。
按 Leaner verification 不重跑作者全套：读差异和作者报告，只跑修复相关测试，并抽查关键 difftest，在 go 基线和本分支各跑一次做对比。
另外单独跑了 synthetic live 阶段和完整预检。测试都在 .64 上跑。

## 结论

high 0，medium 0，low 6：1 条已修，1 条已补登，4 条只记录。没有发现外部行为回退：HTTP 状态、JSON 字段、中文消息、存储字节、Agent 协议都没有变化。
作者的两个行为修复提交正确，并有测试；同类问题已全仓排查，没有遗漏。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| L1 | low | 已修 | **Sub-Store 更新完成时间在真实时间戳下改成了指数形式。** 位置 `substore/dependency_records.go` `UpdateState.MarshalJSON`（W9 新增，用来代替被删的 `legacyDependencyStatus` 里的 `jsonx.Float`）。原实现用 `strconv.FormatFloat(v, 'g', -1, 64)`，最短格式在指数 ≥ 6 时会切到 `%e`。因此 `GET /api/settings/dependencies` 在一次更新后返回 `"finished_at":1.763700000123456e+09`；W9 之前和 Python 都返回 `1763700000.123456`，整数秒时返回 `1763700000.0`。数值相同，前端解析不受影响，但响应字节和 W9 之前不一致。作者只测了空闲时的 `0.0`。修复：改用 `'f'` 格式，没有小数点时补 `.0`。新增表驱动测试 `TestUpdateStateFinishedAtToken`，覆盖 0、整数秒、6 位小数和 .25。负控：旧的 `'g'` 格式会把整数秒输出成 `1.7637e+09`，用例会失败。 |
| L2 | low | 已补登 | **`httpx.Ctx.ArgInt` 只接受 ASCII 数字。** `PyParseInt` 被删后改为原生解析：可选 `+`/`-` 加 `0-9`，前后用 `strings.TrimSpace` 去空白。`1_000`、全角数字、U+001C–U+001F 前后空白现在都回落到默认值。只影响 `/api/logs?lines=`（默认 100）和域名发现的 `days`（默认 7），前端只发 ASCII 数字。这符合 3.3 裁定 2，但之前没有登记，已补到 `behaviour-changes.md` 的「N8-11 W9」段。 |
| L3 | low | 记录 | **Loon 实时配置文件名改读 typed `Profile.Name`。** 位置 `api/config/live.go`。旧代码用 compat 视图的 `GetString("name")`，非字符串时取空，回退到 profile_id；新代码用 R5 投影后的 `config.Profile.Name`。Python 的写法是 `str(name or '')`。存储中的名称是数字 `0` 时，Python 和旧 Go 都回退到 profile_id，新 Go 推断会得到 `"0"`。这一点按 R5 规则推断，没有实测。反过来，名称是数字 `5` 时，新 Go 与 Python 一致，旧 Go 不一致。只有手工写坏的存储才会出现非字符串名称（API 建档要求字符串），而且只影响 `Content-Disposition` 文件名，所以没有改。 |
| L4 | low | 记录 | **`httpxtest.Result.JSON()` 不再拒绝响应体后面的多余数据。** 旧版用 `jsonx.Parse` 读完整个 body；新版的 `json.Decoder.Decode` 只读第一个值。测试工具因此略微放宽，但生产响应由 `jsonresp` 生成，不会有尾随数据。`WantJSON` 改用 `testjson.Diff(json.RawMessage(body), …)`，仍然是完整比较。 |
| L5 | low | 记录 | **作者报告里关于 repo 测试编码器的说法不准确。** `repo/testutil_test.go` 的 `testObj.MarshalJSON` 对键和值都直接调用 `json.Marshal`，所以会转义 HTML 字符和 U+2028/2029。外层 `testCompact` 的 `SetEscapeHTML(false)` 改变不了已经转义好的字节。`TestReferenceThreeWay` 等用例拿 Go 输出和 `testCompact(Python 期望)` 做逐字节比较。语料里如果出现 `<`/`&`/U+2028，结果会是误报失败，而不是误报通过，所以断言没有放宽。当前语料没有这些字符，全部通过。报告第 3 节说“保留了不转义 HTML / U+2028/2029”，与实现不符，以后改语料时要注意。 |
| L6 | low | 记录 | **`TestLegacyFacadeBoundary` 的日志写死了各类 `=0`。** 违例确实会用 `t.Error` 失败，日志只是不反映实际分类计数，不影响门禁。 |

## 逐项核对

1. **删除是否改变外部行为。**
   - repo：W9 之前，包级旧 API（`GetConfig`/`SaveConfig`/`GetSharedConfig`/`SaveSharedConfig`/`GetSystemConfig`/`SaveSystemConfig`/`Update*Transaction`/`SafeImportConfig`）只剩 `api/config/live.go` 和 `api/rules/read_adapter.go` 两处生产调用方读取 `GetConfig`，没有生产代码调用旧的 save 系列。删除不影响三路合并基线：shared 的 `recordBaselines[@shared]`、`SaveSharedRecord` 和 `UpdateConfig` 失效逻辑都没改。新的 `repo.Config` 按 Scope 缓存 `*model.Config`；唯一的调用方 `scopedLocalConfig` 只读不改，缓存不会被污染。失效路径和旧版一致：`UpdateConfig` 删除该 profile 的条目，`UpdateSharedConfigs`/`ResetConfigContext` 清空全部。`live.go` 改成直接调用 `repository.Config`，原来也只读一次，不需要 Scope 快照。`prepareConfig` 改用 typed `UpdateSystem`，只写 `server_domain`。
   - httpx：W9 之前已经没有生产处理器调用 `c.JSON()/JSONSilent()/JSONForce()/JSONObject()/JSONForceError()`（`git grep` 只命中文档），所以请求绑定（非法 JSON、非有限数→400，静默入口）没有改变。`Message`/`RepositoryErrorHandler` 从 jsonx 对象改成匿名 struct，但 `JSONStatus` 仍会 `sortValue` 并经过同一条 sanitize/jsonresp 管线，键顺序和转义不变。`jsonvalue.go` 删掉 jsonx 分支后，深度上限仍为 256。ETag 浮点格式用 `'f'` 加 `.0`，对 mtime 量级的值与旧版一致。
   - agents：W9 之前唯一还在调用 Agent 旧 facade 的生产代码是 `api/clientdevices`（`GetAllAgents`），见第 2 项。其余文件（deployments/ha/remote/manager 等）都是纯删除，构建通过说明没有调用方了。`observeHeartbeat` 的测试钩子改成 typed 签名，生产路径 `runtime.recordFields` 不变。`addReportedMetrics` 只调用 `AddMetricsJSON`，旧的 `AddMetrics`/`DeleteMetricsHistory`/`DeleteTrafficHistory` 钩子已删，服务端只注册 Context 版本。
   - 其他：`taskstatus.RecordContext`→`SaveResult`、`ParseTime`→`ParseTimestamp` 是被删适配器的直接内联。`substore.StartSubStoreUpdate`→`StartUpdate` 的消息与旧版 `err.Error()` 一致。`deploy` 的 `convert.Download`→`ResourceDownload` 只改了类型：URL/LocalPath 原来做类型断言，现在本来就是字符串。`yamlbridge.Encode` 新增的 map/整数分支在生产上不可达，唯一的生产调用方 `importer` 只传入 `*yamlx.Node`。
2. **两个行为修复提交。**
   - acf63b9d1：旧的 `Repository.ProfileMetadata` 内部带 `ctxx.Detach`，`ResolveProfile` 迁移到 `ProfileMetadataRecord` 后丢掉了它，修复把 Detach 补了回来。测试覆盖三点：请求断连后仍能解析、直接调用 `ProfileMetadataRecord` 时尊重取消、经 `ctxx.WithParent` 传入的 shutdown 会被拒绝。同类排查：W9 之前生产代码另有 4 处 `.ProfileMetadata(` 迁到 `ProfileMetadataRecord`，即 `agents/registry.go`、旧版 `agents/manager.go`（已删）、`setup/action.go`、`setup/source.go`。它们的入口都已经先调用 `lifecycle.Detach`，所以没有回退。
   - 4ec305c3d：`clientdevices` 的 id/name/host/status/profile_id/service_type 改读原始 JSON 字符串，非字符串时为空，与旧 `GetString` 一致。status 先判断 `FreshHeartbeat`，过期时为 `offline`，与旧 `OnlineStatus` 一致。`oxidns_ha` 仍以原始 JSON 透传，缺失时为 null。同类排查：其他迁到 typed 字段的生产代码只有 `live.go` 的 `Profile.Name`（见 L3）。
   - 基线对比：在 .64 上用 go 基线 `git archive` 出一份树，和本分支同时跑 `api/config`、`api/aggregations`、`httpx`、`substore`、`api/agents`、`api/rules` 六个 difftest（`CONFIGFLOW_DIFF=1`，真实 Flask）。两边都是 6/6 ok，各 680 个 `--- PASS`，排序后的测试名和结果逐行相同。
3. **门禁硬化。** `TestLegacyFacadeBoundary` 先断言 `legacy_gate_allow.txt` 不存在，再扫描整个 `server/`，任何违例都直接 `t.Error`。检查覆盖 2.11 的全部 5 项：(1) 任何 `.go` 文件（含测试）不得导入 jsonx/pystr/pyyaml，这三个目录也不得存在；(2) testjson 只能被 `_test.go`、`*test` 包、testgolden 和指定 tools 导入；(3) 13 个退役符号按 `ast.Ident` 匹配，非测试代码不得出现，也不得有 legacy 文件；(4) 函数引用和方法值也算（`TestLegacyGateTestOnlyConsumers` 里的 `other.PythonClass` 用例）；(5) 负控从 testdata 读入违例源，在临时树里逐个检出 5 类违例、13 个符号和 3 个目录。三个退役目录在本地确实不存在。
4. **测试删除与迁移。** 作者第 3 节的映射表已核对。被删的 `mihomo_boundary_test`/`store_rows_test` 只测已删的桥接，对应的 typed 和存储往返测试都在。`yamlbridge` 的 value 测试是整体迁到 `yamlxtest`。和 Python 做字节比较的地方没有用会放宽断言的编码（见 L5，方向是更严）。`httpxtest` 见 L4。
5. **并发/取消。** W9 新增的写入点只有 `prepareConfig` 里的 typed `UpdateSystem`（在 `PrepareConfig` 的 Detach 之下）和 `addReportedMetrics`（先检查 `ctx.Err()`）。`UpdateConfig`/`UpdateSharedConfigs` 仍是 Detach 后立即检查。没有新增裸 `go` 或 flock，预检里的 `TestConcurrencyFacadeBoundary` 通过。
6. **pyref。** 17 个归档逐个解压比较（Python 读 JSON），每个文件只有 `metadata.sources` 变化，`entries`、`input_sha256`、`venv_dependencies_sha256` 和其余 metadata 都不变。体积变化来自重新压缩，以及 sources 列表里 jsonx/pystr 条目被删。

## 运行的命令（.64）

```sh
# 修复相关
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'test -z "$(gofmt -l internal/substore)" && go vet ./internal/substore ./internal/api/settings && go test -race -count=1 ./internal/substore ./internal/api/settings ./internal/lifecycle && CONFIGFLOW_DIFF=1 … go test -count=1 ./internal/substore/difftest'
#   ok configflow/internal/substore 70.474s
#   ok configflow/internal/api/settings 23.543s
#   ok configflow/internal/lifecycle 6.558s
#   ok configflow/internal/substore/difftest 6.697s

# go 基线 vs 本分支 difftest（同机并行，CONFIGFLOW_DIFF=1，真实 Flask）
#   基线与分支：api/config 103s、api/aggregations 51s、httpx 36s、substore 10s、api/agents 342s、api/rules 77s 全部 ok
#   680 / 680 --- PASS，测试名集合 diff 为空

# synthetic live（gate.sh 同款）
ssh sunbroke@192.168.0.64 'cd ~/rt/.paseo__worktrees__302rx1dg__go-opus-review-n8-11-w9/server && ~/cf-test/.venv/bin/python scripts/gate-live.py ~/.cache/w9-live -p 6'
#   ok agents / agents/ipv6 / api/ipv6 / agents/rulesync / model / api/agents / gateway/onboarding / agents/dnsha /
#      webdav / gateway/installation / testgolden / yamlx / server / store / repo (244s)
#   LIVE PASS: 45 required entries ran and passed; 0 test skips

# 完整预检
RT_HOST=sunbroke@192.168.0.64 rt sync && ssh sunbroke@192.168.0.64 'bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-opus-review-n8-11-w9 -p 6'
#   == vet
#   == full non-race tests (-p 6)
#   PREFLIGHT PASS
```
