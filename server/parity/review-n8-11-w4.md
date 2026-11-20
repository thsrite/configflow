# N8-11 W4 规则簇独立复核

复核对象：`go-n8-11-w4-rules`（`git log go..7d1500c76` 全部 5 个提交：`37fa1ca07`、`83c577f80`、`a7d4e124d`、`19291d719`、`7d1500c76`）。复核分支 `go-review-n8-11-w4`。对照的是 Python（`backend/routes/{rules,rule_library,proxy_groups}.py`、`backend/utils/{website_identity,ruleset_content,rule_utils,rule_fetch,reorder}.py`）和基底 `go`（`5c03be121`）上的旧 Go 实现。越界授权的 `repo/scope_edits.go`、`gateway/adapter.go` 与 `gateway/rule_source.go` 按 HR 标准逐行核查；协调者已裁定的事项（`x/net/html` + 精确 manifest、固定中文单引号列表 + 非字符串 JSON 文本、`scopedLocalConfig` 留 W9、包级 `UpdateConfig`/`UpdateSharedConfigs` Scope 失效语义、typed hook 优先单次触发、`testjson.Decode[T]`、非有限数保持现有 malformed 边界）只核对实现，不重新讨论。回归测试全部使用合成数据，按 Leaner verification 分别在 `.32`（定向 `-race`、差分与门禁）和 `.64`（完整非 race 预检）通过 `rt` 运行。

## 结论

high 0，medium 6（均已修，附回归测试），low 3（记录，其中 L3 顺带补强用例）。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **`api/rulelibrary` 单节写事务丢失 `Scope` 失效。** 位置 `server/internal/api/rulelibrary/adapter.go` `editShared` / `editSystem`。迁移前 `editShared` 调 `repo.UpdateSharedConfigTransaction`（成功提交后调 `repo.ResetConfigContext(ctx)`，失败不动 Scope），Python 的 `edit_rule_library_content`、`reorder_rule_library`、`handle_proxy_domains` 同样经 `update_shared_config_transaction`/`save_shared_config`/`save_system_config` 清空请求上下文。W4 改为直接调 `*repo.Repository` 的 `r.UpdateShared`/`r.UpdateSystem`，方法层不重置 Scope。影响：同一 `repo.WithScope` 内先读 `GetConfig`/`SharedConfig` 再调 `SaveDraft`/`Reorder`/`SetProxyDomains`，后续 `GetConfig` 仍读到旧 `rule_library`/`system_config`，后续 `SaveSharedRecord` 继续沿用过期基线。修复：在 `editShared` 与 `editSystem` 成功返回前调用 `repo.ResetConfigContext(ctx)`，失败路径不调用。测试：`TestReviewN811EditSharedAndSystemInvalidateScopeOnlyOnSuccess`（`api/rulelibrary/n811_records_test.go`）覆盖失败保留快照、成功刷新 `GetConfig` 且使旧 `SharedConfig` 保存返回 409。 |
| M2 | medium | 已修 | **`check-content` 将非字符串 `format` 静默转成空串而放行。** 位置 `server/internal/api/rulelibrary/adapter.go` `inspect`。迁移前 `rulefetch.InspectDomainContent` 把非 nil 非字符串的 `format` 映射为 `"\x00"`，由 `metadataError` 返回 `规则集格式必须是 text、yaml 或 mrs。`（对齐 Python `_metadata_error`：`rule_format not in (None, '', 'text', 'yaml', 'mrs')`）。W4 改为 `text(body, "format")`，`json.Unmarshal` 失败被吞掉并返回 `""`（代表未指定格式），导致 `format: 123`、`true`、`[]`、`{}` 全部零错误通过。修复：新增 `optionalFormat`，区分缺失/`null`（`""`）与非字符串（`"\x00"`）。测试：`TestReviewN811CheckContentRejectsNonStringFormat`（单元测试）与 `TestN811LibraryRecordRequestValidation`（`api/rulelibrary/difftest`，Flask 差分对比）。 |
| M3 | medium | 已修 | **查重扫描把显式 `"policy": null` 改写成 `"DIRECT"`。** 位置 `server/internal/api/rules/read_adapter.go` `scanRows`。迁移前用 `item.GetOr("policy", "DIRECT")`（对齐 Python `rule_item.get('policy', 'DIRECT')`），仅在缺少 `policy` 键时默认 `"DIRECT"`，显式 `null`（如 `POST /api/rules/batch` 省略 `policy` 时写入的 `"policy": null`）保留为 `null`。W4 改为 `sourceField(item.Policy, "DIRECT")`，`*string` 无法区分缺键与显式 `null`。影响：`/api/rules/find-duplicates` 对 `null` 策略规则错误上报 `"policy": "DIRECT"`，并在其与 `"DIRECT"` 规则重复时把 `policy_conflict` 误判为 `false`。修复：直接从已解码的 `raw["policy"]` 读取，仅在键缺失时默认 `"DIRECT"`。测试：`TestReviewN811DuplicateScanPreservesNullPolicy`（`api/rules/read_test.go`）。 |
| M4 | medium | 已修 | **HR：`gateway/adapter.go` 过度裁剪 `config.Library` 元数据破坏按名推断格式，且全量序列化 `system_config`。** 位置 `server/internal/gateway/adapter.go` `storedContent`。W4 为 typed `ruleSourceReader` 构造 `rulefetch.SourceConfig` 时只保留每条库记录的 `id`/`name`，去掉了 `format`/`url`。但 `ReadCache` → `ValidateRemote` → `rulesetfmt.RuleFormat` 在规则集未带 `library_rule_id` 时按 `name` 匹配 `config.Library` 条目并读取其 `format`/`url`；裁剪后带 `format: "yaml"` 或 `.yaml` URL 的同名库条目回退成 `"text"`，导致合法 YAML 缓存被拒。同时 `system_config` 被整块 marshal，带入了 `config_token`/`rule_proxy_token` 等无关字段。修复：`system_config` 仅投影 `rulefetch` 消费的 `server_domain`/`github_proxy_domain`/`rule_fetch_proxy`；`rule_library` 投影 `id`/`name`/`format`/`url` 四个标量元数据，继续排除 `content` 正文与 `nodes`。测试：`TestReviewN811ReaderProjectionPreservesFormatInferenceWithoutBody`（`gateway/gateway_test.go`）覆盖 1 MB 正文/节点参数不进入投影、敏感设置不进入投影、无 `library_rule_id` 规则集通过 `rulefetch.ReadCache` 正确按 YAML 校验并产出 `ProviderPayload`。 |
| M5 | medium | 已修 | **HR：`repo/scope_edits.go` 缺少 `Detach` 后即刻 `ctx.Err()` 检查与 `scope.bind(r)`，且缺少真实 `Runtime` 与 `recordBaselines` 回归。** 位置 `server/internal/repo/scope_edits.go`、`server/internal/repo/scope_edits_runtime_test.go`。按 WORKFLOW 第四条，写入口在 `ctxx.Detach(ctx)` 后应立即检查 `ctx.Err()`，避免已停机 Runtime 先进入 `ResolveProfileID` 读库或返回校验错而非 `context.Canceled`；`UpdateConfig` 删除缓存前补 `scope.bind(r)` 与 `scope.go` 一致。另外原 `scope_edits_test.go` 未用真实 `lifecycle.Runtime` 验证关机/断线，也未验证 `UpdateConfig`/`UpdateSharedConfigs` 与 typed `SharedConfig`/`SaveSharedRecord` 三路合并基线（`recordBaselines`）的交互。修复：两个包级入口在 `Detach` 后即查 `ctx.Err()`，`UpdateConfig` 删键前调 `scope.bind(r)`；新增 `TestScopedRecordTransactionsRuntimeAndSharedBaseline`（`repo_test`）。 |
| M6 | medium | 已修 | **`ReadRulesetContent` 未修剪 `library` 参数空白导致 `" null "` 掩盖 `item`。** 位置 `server/internal/api/rules/content.go` L41。原判断 `len(library) != 0 && string(library) != "null"` 遇到带空白的 `null`（如 `" null \n"`）时会分配非 nil 空 `&model.RuleLibraryItem{}`，而 `encoding/json.Unmarshal` 对 `null` 到结构体是空操作且返回 `nil`，结果非 nil 空库条目覆盖 `item` 使 `sourceContent` 静默返回空串。修复：用 `bytes.TrimSpace(library)` 修剪后再判空与 `"null"`。测试：在 `TestPublicRulesetContentCapturedRecords`（`api/rules/content_test.go`）新增 `null-library-padded` 用例。 |
| L1 | low | 记录 | **`n811_native_html_differences.json` 含 36 条完全相同的重复记录。** 总计 2161 行、2125 个唯一 ID（重复行源于语料内相同输入，其 `id`/`native`/`category` 均一致）。`nativeHTMLCases` 已做冲突检测，不影响精确匹配，无需重录。 |
| L2 | low | 记录（W9） | **保留的 `*_legacy.go` 中有两个导出符号已无外部生产调用方。** `rules.ReorderByIDs`（`rules/reorder_legacy.go`，仅同文件 `ResolveNewOrder` 内部调用）与 `rulefetch.ReadValidatedRuleCacheWithFormat`（`rulefetch/cache_legacy.go`，仅同文件 `ReadValidatedRuleCache` 内部调用）。所属文件仍有 W3/W7 生产调用方，留 W9 一并删除。 |
| L3 | low | 已补强用例 | **`TestN811HTMLAndCharset` 的 `script`/`style` 用例把有效 `<meta>` 放在后面。** 位置 `server/internal/rulefetch/n811_native_test.go`。原输入 `<script>...<meta content='hidden'>...</script><meta content="visible">` 即使未跳过 script 内容也会被末尾 `<meta>` 覆盖；核对 `x/net/html` 文档确认 tokenizer 默认进入 raw-text 状态，并补加 `script-after`/`style-after`（`<meta content="visible">` 在前）反向验证。 |

## 逐项核对

1. **HR：`repo/scope_edits.go`（Scope 失效与并发编辑）。** 包级 `UpdateConfig` 仅删除目标 profile 的 `scope.cache[id]` 与 `scope.baselines[id]`，保留其他 profile 快照及 `scope.recordBaselines["@shared"]`；`UpdateSharedConfigs` 成功调用 `ResetConfigContext(ctx)` 清空全部缓存/基线/`recordBaselines`；两者失败均不动 Scope。补齐 `Detach` 后即刻 `ctx.Err()`、`scope.bind(r)` 与真实 `Runtime` + `SharedConfig`/`SaveSharedRecord` 三路合并回归（M5）。请求内 `scopedLocalConfig` 保持协调者裁定的 `repo.GetConfig` 快照薄口留 W9。
2. **HR：`gateway/adapter.go` 与 `gateway/rule_source.go`。** `SetRuleSourceReader` 与 `SetRulesetContent` 共用 `contentMu`，`storedContent` 一次持锁读取两个 hook，typed 优先、单次调用、restore 正确回退；修复 M4 后仅投影 fetch 三设置与库条目 `id`/`name`/`format`/`url`，不复制完整 `Config`、节点参数、敏感令牌或规则源正文。
3. **写事务 delta、失败回滚与 WORKFLOW 第四条。** `api/rules`（`editProfile`/`editSharedProfile` 走 `sameJSONValue`）、`api/rulelibrary`（`editShared`/`editSystem`/`editWithProfiles` 走 `applyFields`）、`api/proxygroups`（`edit` 仅写 `bytes.Equal` 变化的 `ProfileFields`）均只写真实变化成员，保留未改字段原始字节。所有写服务入口均先 `lifecycle.Detach(ctx)` 再查 `ctx.Err()`，既有真实 `Runtime` 关机/断线回归（`api/rules/review_test.go`、`api/rulelibrary/runtime_test.go`、`review_context_test.go`、`api/proxygroups/review_context_test.go`）保留完整；修复 M1 补齐 `editShared`/`editSystem` 的 Scope 重置。
4. **规则抓取 HTML/charset、manifest 与错误类。** `pyhtml.go`/`html5_entities.go`/`pyerrors.go` 已删除；`html.go` 改用 `html.UnescapeString` 与 `golang.org/x/net/html`；`transport.go` 字符集改用 `mime.ParseMediaType` + `strings`；错误改为普通 typed error + `ErrorClass()`，保留原日志标签（`RuleFetchError`/`ReadTimeout`/`OSError` 等），`refresh/legacy_errors_test.go` 已补 `ErrorClass()` 分支。`nativeHTMLDifference` 仅接受四个明确 `(suite, fn)` 组合且同时校验输入键 SHA 与输出 SHA（exact match），`TestN811HTMLReplayNegativeControl` 证实篡改输出不会被吞掉；`parity/behaviour-changes.md` 四类 HTML 差异各登记一条。
5. **规则缓存读写路径。** `rules.CacheFilename`/`CacheReadPath`/`StoredCacheReadPath` 与 `rulefetch.ReadCache`/`WriteCache`/`RefreshRule`/`ReadSource`/`SaveSource` 路径与旧实现一致；16 个跨包测试文件的 `RuleCacheFilename` 迁移为 `CacheFilename(testjson.Decode[model.RuleConfig](...))` 且不改原断言。
6. **中文消息与输入提示。** `api/rules`/`api/rulelibrary`/`api/proxygroups` 的 `messages.go` 中文模板未变动；`rules.IdentifierList([]string)` 仅收 `[]string` 并保留单引号与转义呈现；`InvalidValueText` 对非字符串非法值输出 JSON 文本，混合缺失 ID 列表输出 JSON 数组；修复 M2、M3 恢复非字符串 `format` 校验和显式 `null` 策略。
7. **被删 facade、门禁、pyref 与测试替身。** 已删 `rules/{cache,connection,matcher,format}_legacy.go` 与 `rulefetch/website_legacy.go` 无遗漏生产调用方；`legacy_gate_allow.txt` 净减 20 行（809 → 789），零新增；11 份刷新后的 pyref `.json.gz`（mosdns 4、oxidns 7）经脚本逐文件校验，18,425 条 `entries` 与其他元数据均与 `go` 基底完全一致，仅 `metadata.sources` 移除 4 个已删旧文件并加入 `order_message.go`；本轮修复不触及任何 pyref 源。测试替身未覆盖生产超时/参数，无新增 Python 仿真，`testjson.Decode[T]` 仅测试可见。

## 复核运行

### 定向 `-race`、Flask 差分与架构门禁（`.32`）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/repo/scope_edits.go internal/repo/scope_edits_test.go internal/repo/scope_edits_runtime_test.go internal/gateway/adapter.go internal/gateway/gateway_test.go internal/api/rules/read_adapter.go internal/api/rules/read_test.go internal/api/rules/content.go internal/api/rules/content_test.go internal/api/rulelibrary/adapter.go internal/api/rulelibrary/n811_records_test.go internal/api/rulelibrary/difftest/n811_records_test.go internal/rulefetch/n811_native_test.go)" && go vet ./... && go test -race -count=1 -v -run "TestScopedRecordTransactions" ./internal/repo && go test -race -count=1 -v -run "TestN811|TestReviewN811|TestDefaultContentReader" ./internal/gateway && go test -race -count=1 -v -run "TestReviewN811|TestPublicRulesetContent|TestN811" ./internal/api/rules && go test -race -count=1 -v -run "TestReviewN811|TestN811" ./internal/api/rulelibrary && go test -race -count=1 -v -run "TestN811" ./internal/rulefetch && CONFIGFLOW_DIFF=1 go test -count=1 -v -run "TestN811LibraryRecordRequestValidation" ./internal/api/rulelibrary/difftest && go test -count=1 -run "TestLegacyFacadeBoundary|TestLegacyGateNegativeControls|TestConcurrencyFacadeBoundary" -v ./internal/lifecycle'
```

```text
ok  	configflow/internal/repo	19.044s
ok  	configflow/internal/gateway	2.011s
ok  	configflow/internal/api/rules	9.185s
ok  	configflow/internal/api/rulelibrary	3.433s
ok  	configflow/internal/rulefetch	1.081s
ok  	configflow/internal/api/rulelibrary/difftest	2.470s
    gate_test.go:172: scanned 1033 non-test Go files; exemptions: go=8 wall-time=7
    legacy_gate_test.go:207: legacy allowances: retired-import=725 retired-directory=2 retired-symbol=29 legacy-file=33 testjson-import=0 total=789
    legacy_gate_test.go:255: synthetic source rejected (19 exact sites); stale allowance rejected
ok  	configflow/internal/lifecycle	2.124s
```

### 完整非 race 预检（`.64`，`-p 6`）

```sh
RT_HOST=sunbroke@192.168.0.64 rt sync && ssh sunbroke@192.168.0.64 'bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-review-n8-11-w4 -p 6'
```

```text
~/rt/.paseo__worktrees__302rx1dg__go-review-n8-11-w4
== vet
== full non-race tests (-p 6)
PREFLIGHT PASS
```
