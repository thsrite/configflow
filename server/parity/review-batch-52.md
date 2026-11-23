# batch-52 复核：O17 1.7 OxiDNS 类型化第二轮

- 分支 `batch-52`（原 `go-o17-typed-oxidns2@58cf21561`，作者 Gemini Neon），已用
  `git rebase --onto` 从 `go-o17-typed-oxidns` 移到 `batch-48`；rebase 后 batch-48 又多了 be0d2dbb4
  （repo 测试改用 `pathx.Accessible`），再叠一次，底座为 `be0d2dbb4`。
- 复核人：Opus 5.5（HR），2026-11-23。

## rebase

- `stored_projection.go`：batch-48 把 `requestTypeError` 改名 `checkRequestTypes`（80cba13e8），本分支把函数体改成
  `Member` 解码。取本分支函数体、batch-48 的名字。
- pyref 黄金文件：两个提交（`560e1b665`、`9a4464de1`）的 7 个黄金文件冲突，全部取 batch-48 版本，最后统一重录（见中-4）。
- 注意：`560e1b665` 的提交信息写「sources and metadata refreshed」，实际还带了 6 个 Go 文件的代码改动
  （`.node()`→`.toNode()`、`applyRoutingNode` 等）。不影响正确性，只是提交信息不准。

## 重点核对

### 1. 生成配置逐字节不变

- pyref check（oxidns、mosdns，含 boundary 参考 148 份最终配置、mosdns golden）、proddiff 结果见文末。
- 重录的 13 个黄金文件逐个解压，与 batch-48（be0d2dbb4）比较：除 `metadata` 外完全相同。
- 模板生成：`templateDocument` 改用 `ruleDNSFromSettings(s.node)` + `s.AggregateRules`。输入是 `NormalizeSettings`
  之后的设置（`rule_actions`/`rule_dns_parked` 已规范化、没有 null 成员），`selected`/`helperPlugins` 的取值与旧
  `get(...)` 路径一致；`aggregate_rules` 来自 `templateSettings`，与旧 `isTrue(get(...))` 一致。
- `rulesetDownloads` 模板分支：旧代码把所有非列表/对象的成员换成 `[]` 后再 `selected`，新代码直接用
  `ruleDNSFromSettings`；对已规范化的设置等价。

### 2. 存储对象保持开放

- 设置对象仍是 `*Node`；新代码只在读取时解码 `settingsView`/`projectionWire`，没有把类型化值整体写回。
- 新增的写回点逐个核对：
  - `parkInactive` 现在用 `actionMap.toNode()` 重写 `rule_actions`/`rule_dns_parked`。两个调用方
    （`PrepareEditor`、`PreviewTemplate`）的输入都已经过 `normalizeRuleDNS`，条目本来就是规范形式，字节不变。
  - `settle` 读 `rule_dns_parked` 时先 `normalizeAction`，失败的条目以 `raw` 原样保留；存储里的暂存条目
    本来就由 `normalizeRuleDNS` 规范化，往返结果相同。
  - `ApplyRuleDNS` 原生分支的补丁改为 `RoutingChanges`，成员与顺序（4 个选择列表、`rule_actions`、`rule_dns_follow`）
    与旧节点相同。
- `ReadCommonSettings`/`PrepareEditor` 草稿保持开放的理由成立：草稿就是存储对象本身（未知成员、`_common_disabled`
  里停用插件的原生子树、`template_settings`），只能整体克隆后局部编辑。`Revision`/ETag 的计算没有改。

### 3. 宽松设置视图与原 `str`/`isTrue` 等价

- `Member` 语义：缺失 → `Present()=false`；null → `Present()=true, OK=false`，与旧 `get()` 对 null 返回非 nil
  节点、`has()` 为真一致；类型不对 → `OK=false`，与 `isStr`/`isBool` 为假一致。
- 逐项核对：`config_mode`（`isNative`、`Or("template")`）、`native_config`（`loadNative`/`loadNativeOrEmpty`，
  2 MiB 上限仍在 `LoadNativeYAML`→`loadNative`）、`custom_hosts`、`adguard.enabled/urls/content`、`renamed_plugins`、
  `local/remote/fallback_dns`、`default_forward`、`prefer_ipv4`（只有严格 `false` 才关）、`template_settings`
  （对象才 OK）、`legacy_native_config`（`Present`）。`memberTextNodeOr` 对类型不对的值还原原值，与 `orDefault` 一致。
- `storedProjection`/`checkRequestTypes`：数字判断从 YAML 标签改为原始 JSON 首字符，数值文本取自同一个 JSON 编码器；
  null 的 `enabled` 新旧都报问题并投影成 `false`。
- 「无法确认就阻断」：设置视图本身都是从 JSON 来源的树（请求体或 `model.Dynamic`），`ToJSON` 不会失败；
  见低-1。真正变成放行的是三处入口改造（中-1、中-2、中-3），已修。
- **NormalizeFQDN**：本分支（含作者全部 7 个提交）和整个仓库都没有 `NormalizeFQDN` 这个符号，diff 里也没有任何
  FQDN 相关改动；测试文件的改动只有签名跟随（`ApplyPolicies` 行参数、`applyRoutingNode`、`toNode`）。
  所以不存在「为让测试通过改 NormalizeFQDN」这一生产行为变化。

### 4. `commonContext.view` 缓存

- 快照在 `newContext` 时取，只用于 `aliasCapability`（`renamed_plugins`）、`resources()`（`adguard.urls`）、
  `readCommon`（`adguard.content`、`custom_hosts`）。
- 同一 context 内对这些成员的写入：`renamed_plugins` 只在 `nativeEdit.apply` 里写 `e.current`，此时还没建 context
  （之后的 `newContext` 才读）；`patchResources` 在调用 `c.resources()` 之后才写 `adguard`；`custom_hosts` 的写入
  不经过视图读取；`readCommon` 自己新建 context。没有「改完再读到旧视图」的路径。
- 其他 `readSettingsView` 调用都是现读，不缓存。

### 5. `routers/management_repo.go`

- 与 batch-46 的 routers 类型化（1a57fff58，行参数保持 `json.RawMessage`）一致：现在先 `oxidns.ParsePolicies`
  再交给 `PreparePolicyNotes`/`SavePolicyRows`，错误顺序与旧代码相同（旧代码在 `PreparePolicyNotes` 里解析）。
- 唯一问题是备注编码，见低-2（已修）。

## 发现

- 高：无。
- 中-1（已修，547085c70）：`ApplyPolicies` 改成收 `[]devicenotes.Policy` 后不再校验行。旧代码在入口
  `devicenotes.ParsePolicies` 校验地址、重复、名称、模式和 512 行上限。`clients.UpdateDevice` 的行只经过
  `NormalizeChange`，已有 512 行时再加一行会被写进 OxiDNS 草稿（旧代码拒绝）；备注里不合规的名称同理。
  修法：`ApplyPolicies` 先用 `checkedPolicies`（编码后走 `ParsePolicies`）校验，与旧入口逐字节同一检查，nil 视为空。
- 中-2（已修，547085c70）：`ApplyRuleDNS` 模板分支把 `normalizeRuleDNS(values)` 换成只查跟随互斥的
  `validateRuleDNS`，丢了对每条 `rule_actions`/`rule_dns_parked` 的 `NormalizeAction` 检查（例如候选上游 `addr`
  为空时旧代码报「指定上游需要非空的 addr 地址」，新代码直接写入设置，到生成时才失败）。
  修法：`validateRuleDNS` 对两组条目逐个 `normalizeAction` 并替换为规范形式，然后再查跟随。
- 中-3（已修，547085c70）：`restoreParked`（模板输入恢复暂存选择）改成双路径，类型化路径用 `actionMap` 判断
  「规则已有 DNS 处理」，而 `actionMapFromNode` 跳过 null 成员；旧代码和 Python 的 `identity in actions` 对
  `{"r1": null}` 都为真。`PreviewTemplate` 的 `proposedInputs` 是未规范化的请求体，`rule_actions` 里带 null 时，
  该规则会留在直连/代理列表里、改变预览与随后写入的路由。修法：`restoreParked` 恢复为 batch-48 的节点版本
  （这里处理的就是请求原样输入，节点版本更合适）。
- 中-4（已修，1a5671134）：rebase 后 `PYREF_MODE=check/record` 拒绝 oxidns 18 条、mosdns 5 条参考：batch-48 引入的
  `internal/digest`（6f827780f）和 `internal/pathx` 进入了依赖图但 `sources.json` 没登记（batch-48 本身同样会失败，
  普通 `go test` 不查）。修法：按 `-suggest-sources` 登记这两个文件，重录 13 个黄金文件，逐个比较只有 `metadata` 变化。
  `-suggest-sources` 还提示 routersplit 3 条（`digest`、`logx/*`）和 installer 1 条（`digest`）缺声明，不在本批范围，
  留给协调者。
- 低-1：`readSettingsView`/`decodeProjection` 在整份设置 `ToJSON` 失败时返回空视图（`checkRequestTypes` 因此放行）。
  设置树都来自 JSON（请求体或 `model.Dynamic`），停用插件子树来自 `loadNative` 已校验的 YAML（键必须是字符串、
  浮点有限），目前不可达。不修。
- 低-2（已修，9af19f9dd）：`saveWorkspacePolicies` 用 `json.Marshal(notes)`，设备名里的 `& < >` 会写成
  `&` 等；旧代码经 `yamlx.ToJSON` 不转义。语义相同但存储字节变化。改用 `jsonresp.Encode`。
- 低-3：`dnsfollow` 读草稿文档的标签由 `""` 变成 `NativeLabel`。`PrepareEditor` 已用同一标签加载过同一文本，
  第二次加载不会失败，错误文案无可见变化。不修。
- 低-4：`agents/rulesync` 的补丁改为 `RoutingChanges`：非字符串 ID 变成 `""`、`rule_actions` 去掉未知成员和 null 条目。
  输入来自 `oxidns.Analyze` 识别出的值（全是字符串、规范动作），不可达。不修。

## 测试

新增 `oxidns/review_batch52_test.go`（`TestApplyPoliciesValidatesTypedRows`、`TestValidateRuleDNSNormalizesActions`、
`TestRestoreParkedNullActionLeavesLists`）和 `routers/review_batch52_test.go`
（`TestWorkspacePolicyNotesKeepLiteralHTMLCharacters`）。

## 提交

- 作者 7 个提交（rebase 后）：1abfd5b85 … fb5fe8f86。
- 复核修复：547085c70（oxidns 三处校验）、9af19f9dd（routers 备注编码）、1a5671134（pyref 声明 + 重录）。
- 本报告单独一个提交。

## 验证

全部在代码 HEAD=1a5671134 上。

- `gofmt -l` 无输出；`go vet ./...` 通过（.32）。
- `PYREF_MODE=check go test ./internal/oxidns ./internal/mosdns`（.32）：`ok oxidns 141.0s`、`ok mosdns 73.7s`
  （含 boundary 参考 148 份最终配置、mosdns golden）。
- `-race -p 6`（.32）：oxidns 及调用方 agents/{dnsfollow,dnsha,rulesync}、api/{agents,clientdevices,config,generate,oxidns,
  proxygroups,routers,rulelibrary,rules}、clients、configgen、deploy(+contentsync,pushdiff)、devicenotes、gateway/onboarding、
  ippresence、routers、setup、tools/proddiff/{baseline,comparator,generator} 全部 ok（oxidns 1294s、routers 891s）。
- difftest（`CONFIGFLOW_DIFF=1 -tags configflow_difftest`，.32）：oxidns、mosdns(+difftest)、api/{oxidns,rules,rulelibrary,
  proxygroups,config,clientdevices,agents,generate,routers}(+difftest)、devicenotes(+difftest)、agents/{dnsfollow,dnsha,rulesync}、
  configgen、gateway/onboarding、routers、clients 全部 ok。
- fuzz 60 s（.32）：FuzzZoneValidation PASS（59.8 万次）、FuzzNativeYAMLRoundTrip PASS（34.0 万次）。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`（.64）：baseline `new=0 stale=0`，`self_check: PASS`，
  oxidns 生成器 4 组全部 success，`GATE STAGE PASS: proddiff`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-52 -p 6`（.64）：两组 tag vet 通过，全量非 race `PREFLIGHT PASS`。

## 再次 rebase（batch-48@15ae7cb3c）

- 原 pyref 提交与 batch-48 的 70f60f2ed 重复，变基时成为空提交并丢弃；sources.json 取 batch-48 版本，再补本分支的登记
  （`ruledns_values.go`、`settings_view.go` 及符号变化），以及 70f60f2ed 漏掉的 7 条 oxidns boundary 参考的 digest/pathx。
  `-suggest-sources` 与清单一致。重录后 8 个 oxidns 黄金文件与 batch-48 比只有 metadata 变化；installer、mosdns、routersplit 不变（1a5671134）。
- `PYREF_MODE=check go test ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/routersplit`（.32）全部 ok；
  gofmt 无输出、两组 tag 的 `go vet` 通过；上文列出的 `-race` 包全部 ok。
