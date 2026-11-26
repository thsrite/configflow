# O17 结构项 ③：字典式写法改成类型化结构（分支 go-o17-typed）

任务：1.6 CRUD 请求体 → 1.8 RouterOS/iKuai 行 → 1.7 OxiDNS（顺序按协调者裁定）。

## 协调者裁定（2026-11-23）

1. OxiDNS：用户原生文档保留 `yaml.Node`（注释、未知插件、插件参数），边界类型 `nativeDoc` 注释写明；
   ConfigFlow 自己的 schema 全部类型化，逐字段类型检查沿用原中文文案；清掉 `!(!isNone(...))` 一类机械翻译。
2. 不动 `model`：`model.Profile.OxiDNS`（`model.Dynamic`）在 oxidns 入口解码一次、写回时编码；
   未知成员是否保存按 `storedProjection` 与往返测试核对后定，结论写在这里。
3. RouterOS 行 `map[string]string`；只读分析处用结构体；iKuai 按接口定义结构体。

## 状态（2026-11-23 交回）

**1.6 完成；1.8、1.7 未开始**（理由与计划见「剩余」）。已 rebase 到 `go@4c99e4495`（batch-42 合入后），每个提交单独 build + vet 通过；以下验证在 rebase 后重跑，都在 .64 上（oxidns fuzz 在 .32 上，rebase 前）：
- `-race`（`-timeout 60m`）：api/rules、nodes、proxygroups、aggregations、subscriptions、rulelibrary、mcp、rules、rulefetch、
  jsondecode、oxidns、repo、aggregations、discovery、server，全部 ok
- difftest（`CONFIGFLOW_DIFF=1`）：api/rules、aggregations、subscriptions、rulelibrary、config、oxidns、mcp、subs、health、httpx、
  agents/dnsfollow，全部 ok
- `/tmp/preflight.sh … -p 6`：PREFLIGHT PASS（vet 两组 tag + 全量非 race）
- `GATE_ONLY=proddiff gate.sh -p 6`：baseline new=0 stale=0，GATE STAGE PASS
- R11：`internal/server` 通过；删掉 12 条已过期的整记录编码豁免，新增 0 条
- fuzz：FuzzZoneValidation、FuzzNativeYAMLRoundTrip 各 60 s PASS（routers 未改动，未跑）

## 设计

- 请求成员：`jsondecode.Member[T]`（原始字节 + 类型化值 + 是否合法）。类型不对不让整体解码失败，
  由处理函数按原来的检查顺序给原来的中文文案/状态码；成员名大小写精确匹配（`jsondecode`）。
- 1.6 `api/rules`：不再把整份 profile 编成 JSON 字典再编辑，直接改 `*model.Config`；
  repo 的 `changedMembers` 已经按成员比较，删掉本包的 `applyFields`/`sameJSONValue`。

## 状态

- [x] 1.6 api/rules（请求 DTO + 直接编辑 `*model.Config`；`rulefetch.Declaration` 校验类型化；`oxidns.RuleConvertible`/`WithoutRuleReferences`/`SettingsText`、`repo.HydratedRuleset` 为过渡接口）
- [x] 1.6 api/nodes（请求 DTO；节点 CRUD 直接改 `[]model.Node`；删除规划器在 `*model.Document` 副本上做，确认摘要协议不变；`rules.DecodeOrder` 取代 `OrderIDs`）
- [x] 1.6 api/proxygroups（配置直接改 `*model.Config`；批量探测 URL、排序、预览为请求 DTO；`rules.GroupMembers` 取代
  `CheckCandidateMembers`/`CheckFilterMembers` 的 `map[string]json.RawMessage`。**例外**：策略组新增/修改的补丁保留为
  `groupFields`（包了 `model.Fields`）——按类型清理时不论类型都要丢掉的成员太多，且 repo 的输入文案（与整份导入共用）
  定义在结果列表的 JSON 上；只有这一个组以 JSON 进入 `SetJSON("proxy_groups")`，其余组都是类型化值）
- [x] 1.6 api/aggregations（共享目录类型化读写：`generation.LoadCatalog/SaveCatalog` 改为 `*model.Shared`；计数、预览、排序类型化；
  预览改用 `GenerateConfig`（与下载同一路径）。聚合新增/修改请求同 proxygroups 保留为 `aggregationFields` 边界；
  删除 `enabled(record)` 回归测试——节点的 `enabled` 已是 `*bool`）
- [x] 1.6 api/subscriptions（订阅列表/健康/节点/排序类型化，列表视图为 `subscriptionView`；新增/替换请求为
  `subscriptionFields` 边界；代理定义改名 `proxy`（协议成员开放，保留 JSON 对象）；删除对象 `enabled` 真假回归测试）
- [x] 1.6 api/rulelibrary（共享目录、系统设置、受影响配置的重建全部类型化：`editShared`/`editSystem`/`editWithProfiles`/`rebuild`
  直接改 `*model.Shared`/`*model.System`/`*model.Config`；排序直接排 `[]model.RuleLibraryItem`。库条目本身保留为
  JSON 成员（`record`），因为草稿修订号、按原样校验和「未发送则继承 update_interval」都定义在条目成员上；
  写回用 `setLibrary`（`SetJSON("rule_library")`，文案与原来一致）。已删除 `rules.OrderIDs`）
- [x] 1.6 mcp/invoker（参数本来就是 `arguments` DTO；固定形状的出站请求改为 DTO。`object` 保留并写明：Agent 发来的资源/设置变更
  原样转发给面板 API 校验，以及把 API 结果转给 Agent，这两处本来就是开放 JSON）
- [ ] 1.8 RouterOS Row → map[string]string；只读分析结构体
- [ ] 1.8 iKuai 结构体
- [x] 1.7 OxiDNS 类型化：第一轮 + 第二轮全部完成（见下「1.7 进度」）

## 做法要点（供续作）

- 验证命令（.64/.32 均可）：`rt -- bash -lc 'cd server && go build ./... && go vet ./internal/<pkg>/ && CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON=$HOME/cf-test/.venv/bin/python go test -count=1 ./internal/<pkg>/... '`
- 「原样成员」边界（`groupFields`/`aggregationFields`，以后 `subscriptionFields`）：仅用于新增/替换的那一条记录，
  其他记录一律类型化；写入时用 `SetJSON(列表)` 让 repo 给出与导入相同的文案。
- 部分更新：解码到「存储记录副本」（先 `detach` 指针成员，避免写穿）；要按原样校验或可能被丢弃的成员在外层用
  `jsondecode.Member` 遮蔽（见 `api/rules/model.go` 的 `ruleInput`）。
- oxidns 过渡接口（1.7 时改为类型化）：`RuleConvertible`、`RuleFollowRoute`、`WithoutRuleReferences`、`SettingsText`、`SettingsStrings`。
- `rules.OrderIDs` 是过渡包装，subscriptions/rulelibrary 改完后删除。

## 剩余：1.8、1.7（未开始，建议各开一位 agent）

### 1.8 RouterOS / iKuai（实测范围比审计估计大）

- `routers` 包 5.5 万行，`Row` 出现约 1600 次；**`Row` 同时用于 RouterOS 和 iKuai**。iKuai 写入里有整数
  （`ikuai_apply.go` 的 `"iface_band": 0`、`ikuai_routes.go` 的 `"prio": 1` 等），所以不能把 `Row` 整体改成
  `map[string]string`——那会把发给 iKuai 的数字变成字符串，改变写入内容。
- 建议顺序：① 新类型 `rosRow map[string]string`（`UnmarshalJSON` 在读设备回复时把数字/布尔转文本、数组/对象转 JSON
  文本，规则同现在的 `comparable`），只替换 RouterOS 路径（`routeros*.go`、`rosnat.go`、`addresslists.go`、
  `addresswrite.go`、`natlist_*`、`dns_guard_*`、`ipv6_*`、`rospolicy_*`、`fixed_ip_ros_*`、`clients_ros.go`），
  删 `rosText`/`rosTruthy`/`firstText`/`clientTrue`；`ROSChange.Values` 的 JSON 形状不变（RouterOS 写入本来全是字符串，
  先用 grep 确认没有非字符串字面量）。② iKuai 按接口定义结构体（`json.Number` 保留数字），去掉 `truthyValue`。
  ③ `NatContext` 等只读分析处改结构体。
- 守住：routerdiff、`internal/routers` 的 difftest（`CONFIGFLOW_DIFF`）、`FuzzRouterOS*`/iKuai 现有 fuzz、proddiff；
  审计建议跟 PVE 实验室回归一起做。

### 1.8 进度（分支 go-o17-typed-ros，2026-11-23）

**状态：① ② 完成；③ 未做（理由见下，交协调者定）。** 提交（rebase 到 `go@d8286168b`）：
`11fcae95a` step 0（iKuai 拆出 `ikuaiRow`）、`c286b9430` step 1（RouterOS `Row map[string]string`）、
`d335b32c4` step 2（iKuai 新建表单结构体）。

做法：
- `Row map[string]string`（`rosnat.go`）：`UnmarshalJSON` 在读设备回复/存储时解码一次——数字保留字面文本、布尔
  `"true"/"false"`、数组/对象紧凑 JSON 文本（仍按值比较）、**null 成员丢弃（=缺失）**（协调者裁定方案 A）。
  拒绝 null 的读取点（IPv6 ND/IPv4 地址的 disabled/invalid/dynamic 与 DNS 列表、DHCP 的 dhcp-option/
  dhcp-option-set/options/dns-server/dns-none、`/routing/settings policy-rules`）用 `strictList`/`markNulls`
  把 null 保留为 `nullMarker`（`"{}"`，任何标志/列表/名称读取都不接受），沿原解析路径给原文案、保持 fail closed。
- 删除 `rosText`/`rosTextDefault`/`firstText`/`rosTruthy`/`clientTrue`/`valueOr`/`cloneAddressRow` 及全部 `any`
  分支；`request` 返回原始回复，`asRow`/`asRows` 解码；`/print` 查询体为 `rosPrint` 结构体（字节不变）。
  列表字段统一 `textList`（逗号文本或 JSON 数组文本），`structuredText` 判断原回复里的数组/对象。
- 对外 JSON 不变的两处专门类型：IPv6 预览证据 `IPv6EvidenceSet`/`IPv6EvidenceRow`（路由 `active` 仍是 JSON 布尔）；
  路由编辑快照 `RouteSnapshot`（RouterOS 行或 iKuai 行，编码为原记录）。iKuai 固定 IP 的 expected 字段为文本 `Row`
  （本来全是字符串）。`agents/ipv6` 的连接证据改读文本。
- iKuai：新建 NAT 免转换、端口分流、静态路由为 `ikuaiNATForm`/`ikuaiPolicyForm`/`ikuaiRouteForm`（整数为 `int`，
  地址/端口/时间为 `ikuaiAddressSet`/`ikuaiSchedule`）；`add`/`staticWrite` 接 `ikuaiForm` 接口。**修改/恢复仍发开放的
  设备记录 `ikuaiRow`**：必须把设备返回的全部字段（含未知字段、设备给的原值类型）原样写回，结构体会丢字段或改值类型。
  `truthyValue` → `ikuaiRow.filled`。iKuai 读取行仍是 `ikuaiRow`（`map[string]any`），DHCP 读取按字段逐个做类型校验
  并给中文阻断文案，这正是开放记录的语义。

未做 ③（NatContext 等只读分析改结构体）的理由：NAT 免转换评估、路由/策略证据的语义就是「行里出现了哪些字段」
（`exemptionFields` 判断未知匹配条件、`rosRouteEvidence` 选字段、R03/R09 语料喂任意字段），改成结构体会丢掉未知字段，
把 fail closed 变成放行。固定字段的小读取（`/system/resource`、ping 回复）用 `Row` 已足够。建议 ③ 撤销或改为只针对
确有固定形状的读取点。

功能差异（设备不会发的输入，均为 Python 边界/语料用例）：
- RouterOS 回复里的非文本值按文本读：数字 `30` 与 `"30"`、布尔 `true` 与 `"true"` 不再区分（路由快照、预览证据
  里显示为文本）；数组/对象为其 JSON 文本，形如 `[..]`/`{..}` 且是合法 JSON 的文本值按结构化值处理（只会走更保守的分支）。
- null 成员视为缺失（拒绝 null 的读取点除外，见上）。测试里对应用例改为按文本解码后比较（`sameTextRow`、
  `r08TextEvidence`、R06 快照归一），登记为「O17 1.8 接受」。

验证（rebase 到 `go@d8286168b` 后，代码提交 `d335b32c4`）：
- `-race -timeout 60m` + `CONFIGFLOW_DIFF=1`（.32）：routers/...、agents/...（含 dnsfollow/dnsha difftest）、api/clientdevices、
  api/config、api/routers/...、api/rules/...（含 difftest）、clients/...、notifications、routersplit/...、setup，全部 ok
  （routers 1027 s）。
- fuzz 各 60 s（.64）：routers 13 个现有 Fuzz（R07/R08/R09×3/R10/R11×2/R12/R14/N89/FakeIPYAML/ClientDuration）
  + routersplit FuzzRouterSplitYAML，全部 PASS。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`（.64）：baseline new=0 stale=0，self_check PASS，GATE STAGE PASS。
- server 包 R11（`-run TestO10`，含分配上限与整记录编码边界）ok；`/tmp/preflight.sh … -p 6`（.32）：PREFLIGHT PASS（两组 tag 的 vet + 全量非 race）。
- routerdiff 抽跑（.64，auth-off-release/auth-off-fixed/return-protocol2/return-rollback/policy-view/dhcp-ikuai-write-*/
  native-owned-*，159 个配方）：与基线 `go@d8286168b` 同样抽样逐条对比，差异路径/类别集合相同（都是已知的 CORS 头、
  nonce、令牌等），仅时间戳类字段（`last_release/at`、`generated_at`）随机出入，无新增差异。
- 行数（相对 `go@d8286168b`）：非测试 +1038/−947，测试 +173/−62（含 iKuai `Row→ikuaiRow` 改名行、新类型）。

### 1.7 OxiDNS

- 裁定见上。本轮已把 `api/*` 调用方都改成从 `*model.Config` 进入（`NewTypedProfile`），并留了 5 个过渡接口
  （`RuleConvertible`、`RuleFollowRoute`、`RuleHasIPPart`、`WithoutRuleReferences`、`SettingsText`/`SettingsStrings`），
  1.7 时随 `Settings` 结构体一起改成类型化签名。
- 起点：`settings.go`（`NormalizeSettings`/`checkTemplate` 逐字段类型检查 → 解码成 `Settings`）、`ruledns*.go`、
  `devicepolicy.go`、`converter.go` 读设置部分、`adapter.go`（`Profile` 不再把规则/组/Agent 包成 Node 文档）。
  原生文档相关文件（`common.go`、`routing.go`、`updates.go`、`native_*.go`、`constraints_*.go`、`editor.go`）
  改为边界类型 `nativeDoc`。

### 1.7 进度（2026-11-23，分支 go-o17-typed-oxidns，基于 go-o17-typed@1a2ed4324）

提交（每个单独可编译、oxidns 测试通过）：

| 提交 | 内容 |
|---|---|
| db4dbe3ec | `Profile` 不再把规则/组/Agent/系统设置重包成 Node 文档：规则项一律 `model.RuleConfig`（目录、路由、规则 DNS、下载），策略路由读 `model.ProxyGroup`，Agent 绑定/展示读 `model.Agent`，设备备注存 JSON；`Convertible`/`HasIPPart`/`FollowRoute` 改为 `model.RuleConfig` 签名，删 1.6 过渡的 `RuleConvertible`/`RuleHasIPPart`/`RuleFollowRoute`；`RulesetDownload.Behavior` 改为文本 |
| 64ea042d9 | 模板模式设置解码成 `templateSettings`（`settings_template.go`）：`jsondecode.Member` 逐字段检查，原中文文案与检查顺序不变，取代 `checkTemplate`；模板生成读类型化值。固定上游链读类型化 `chainFeatures`（模板或原生文档） |
| eb15dd56f | `RuleAction`（种类、来源、原样保留的原生上游条目）：`NormalizeAction`/`RouteKey`/辅助插件/固定上游链 |
| 61c6bd727 | 设备策略行在包内为 `devicenotes.Policy`（`policyPlugin`/`patchPolicies`/`PatchDocument`）；`PolicyView.Policies` 的 JSON 形状不变（routers 在用，未改 routers） |
| 5ac185785 | 边界类型 `nativeDoc`（注释写明：保留注释、键序、未知插件与参数）用于原生文档参数；清掉 `!(!isNone(x))`、`!(str(x) != "")`、`!(len(..) > 0)` 等机械翻译；`node.go` 的「指针身份即对象身份」注释改写 |
| c1625058b | `ReadSettingsHead(model.Dynamic)` 取代 1.6 的 `SettingsText`/`SettingsStrings`：api/rules、api/proxygroups、api/rulelibrary 不再在设置树上取值 |

| cb6b87fac | pyref `sources.json` 跟随新文件（`settings_template.go`、`jsondecode/member.go`、`rules/group_members.go`），删已移除的 `Profile.get`/`text`/`has`；oxidns 8 个、mosdns 5 个黄金文件只刷新元数据（逐个核对 entries 完全相同） |

验证（全部在 .32，HEAD=cb6b87fac 之前的代码提交 + 该 pyref 提交）：
- `gofmt -l` 无输出，`go vet ./...` 通过
- `-race -p 6 -timeout 60m`：oxidns 及全部调用方（agents/dnsfollow、dnsha、rulesync，api/agents、clientdevices、config、generate、oxidns、
  proxygroups、routers、rulelibrary、rules，clients、configgen、deploy、devicenotes、gateway/onboarding、ippresence、routers、setup、tools/proddiff）全部 ok
- difftest（`CONFIGFLOW_DIFF=1`）：oxidns、mosdns(+difftest)、api/oxidns、api/rules、api/rulelibrary、api/proxygroups、api/config、
  api/clientdevices、api/agents、api/generate、devicenotes、agents/dnsfollow、dnsha、rulesync、configgen、gateway/onboarding 全部 ok
- `PYREF_MODE=check go test ./internal/oxidns ./internal/mosdns`：ok（boundary 参考：generate 501、downloads 113、custom_files 94 全部 identical）
- fuzz 60 s：FuzzZoneValidation PASS（75.9 万次）、FuzzNativeYAMLRoundTrip PASS（50.5 万次）
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`：baseline new=0 stale=0，self_check PASS，GATE STAGE PASS
- `/tmp/preflight.sh … -p 6`：PREFLIGHT PASS（两组 tag vet + 全量非 race，含 internal/server 的 R11）

`get(` 调用（非测试文件）：1286 → 1004（审计时 1282；测试文件另计）。改动 43 个文件，+1486/−1155。

**未知成员结论（storedProjection 与往返测试）**：存储的 OxiDNS 设置对象必须原样保留未知成员和存储顺序——
`TestRuleDNSAssignmentOrder` 要求 `future` 成员保留，Python `_merge` 把用户未知键追加在默认键之后并保存；
原生模式下模板字段不做类型检查，类型不对的值也原样保存并在 GET 时返回；`Revision`/ETag 是存储对象的规范化哈希。
所以存储层仍是开放对象（`model.Dynamic` ↔ YAML 树，`storedProjection` 只在读取时做已批准的兼容投影、不改存储），
类型化放在消费边界：模板生成入口 `decodeTemplate`、`RuleAction`、设备策略行、`ReadSettingsHead`。

**保持不变**：生成配置语义（oxidns pyref check、boundary 参考测试 148 份最终配置比较）、原生编辑结果、API 字段/状态码/文案（difftest）。

**第二轮（2026-11-23 完成，分支 `go-o17-typed-oxidns2`，基于 `go-o17-typed-oxidns@1a58914e8`）**：

提交（每个单独可编译、测试通过）：

| 提交 | 内容 |
|---|---|
| `c38d77dd2` | `storedProjection` / `requestTypeError` 改为 `jsondecode.Member` 解码（`stored_projection.go`），保留历史兼容投影、诊断路径与请求类型拒绝语义不变 |
| `7cbb08ba2` | 新增 `ruledns_values.go`（`actionMap`、`ruleDNSValues`、`routingValues`）；`routing.go`（`routingState.values`、`recognize`、`observedLegacyGroups`、`assignStored`/`validateSelections`/`takeOver`/`settle`/`ApplyRouting`/`RemoveRule`）、`converter.go`（`selected`/`routingGroups`/`templateDocument`）、`ruledns.go`（`helperPlugins`/`pinnedRuleAction`）、`ruledns_state.go`（`storedRuleDNS`/`readRuleDNS`/`ReadRuleDNS`/`ExplicitRuleDNS`）、`ruledns_apply.go`（`pruneDNSFields`/`placeDNSChoice`/`resolveDNSChoice`/`ApplyRuleDNS`）、`editor.go`（`restoreParked`/`parkInactive`）改用类型化路由 values；`RuleAction` 对畸形暂存条目保留原样 `raw` 节点确保往返与校验一致 |
| `1cb781c17` | 新增 `settings_view.go`（宽松解码视图 `settingsView`，`jsondecode.Member` 语义），替换 `common.go`、`updates.go`、`native.go`、`native_apply.go`、`matchtargets.go`、`converter.go`、`routing.go`、`ruledns_state.go`、`ruledns_apply.go`、`editor.go`、`devicepolicy.go`、`settings.go` 中读取设置对象字段（`config_mode`、`native_config`、`adguard`、`custom_hosts`、`_common_disabled.renamed_plugins`、`template_settings` 等）的 `get(`；原生 YAML 文档编辑与停用插件子树保留 `nativeDoc` 边界 |
| `4d90cb151` | 外部入口类型化：`ApplyPolicies`、`SavePolicyRows`、`PreparePolicyNotes` 行参数改为 `[]devicenotes.Policy`（并导出 `ParsePolicies`）；`ApplyRouting` 补丁改为类型化 `RoutingChanges` + `RuleActionEntry`（包内 `applyRoutingNode` 保留给开放 JSON 常用设置补丁）；新增 `PrepareProfileEditor` 直接返回准备后的草稿与解析文档。同步更新调用方：`clients/devices.go`、`api/agents/client_tools_adapter.go`、`routers/management_repo.go`（仅 +4/−4 行）、`agents/dnsfollow/document.go`、`agents/dnsha/validation.go`、`agents/rulesync/native.go` 及对应测试 |
| `560e1b665` | `server/tools/pyref/sources.json` 登记 `ruledns_values.go`、`settings_view.go` 及符号更新，内部 `.node()` 改名 `.toNode()` 避免与 `n73_legacy_test.go` 碰撞；8 个 oxidns 黄金文件只刷新元数据（逐个核对 `entries` 100% 相同） |
| `9a4464de1` | 在 `commonContext` 缓存一次 `settingsView`，消除 `aliasCapability` 热路径重复序列化/解码整份设置（`oxidns` 单测耗时 338s → 89s）；同步刷新 7 个 oxidns pyref 元数据（`entries` 100% 相同） |

验证（在 `9a4464de1` 上）：
- `gofmt -l` 无输出，`go vet ./...` 通过
- `PYREF_MODE=check go test -count=1 ./internal/oxidns ./internal/mosdns`（.32）：ok（92.9s / 59.4s）
- fuzz 60s（.32）：`FuzzZoneValidation` PASS（127.1 万次）、`FuzzNativeYAMLRoundTrip` PASS（64.7 万次）
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`（.32）：baseline `new=0 stale=0`，`self_check: PASS`，`GATE STAGE PASS: proddiff`
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__go-o17-typed-oxidns2 -p 6`（.32）：`PREFLIGHT PASS`（两组 tag vet + 全量非 race 测试）
- `-race -p 6 -timeout 60m`（.64）：`oxidns` 及全部调用方（`agents/{dnsfollow,dnsha,rulesync}`、`api/{agents,clientdevices,config,generate,oxidns,proxygroups,routers,rulelibrary,rules}`、`clients`、`configgen`、`deploy`、`devicenotes`、`gateway/onboarding`、`ippresence`、`routers`、`setup`、`tools/proddiff/...`）全部 ok
- difftest（`CONFIGFLOW_DIFF=1`，.64）：`oxidns`、`mosdns/...`、`api/{oxidns,rules,rulelibrary,proxygroups,config,clientdevices,agents,generate}/...`、`devicenotes/...`、`agents/{dnsfollow,dnsha,rulesync}/...`、`configgen/...`、`gateway/onboarding/...` 全部 ok

`get(` 调用（`server/internal/oxidns/` 非测试文件）：1286 → 1004（第一轮）→ **864**（第二轮，净再降 140 处；剩余均位于 `nativeDoc` 原生文档/开放补丁边界）。第二轮改动 38 个文件，+1428/−605。

## 功能差异（对外）

- `api/rules` 创建/修改规则、批量添加的响应 `data`/`rules`：不再回显未声明的成员（它们本来就不会被保存）；
  成员顺序由字典序变为声明顺序。批量添加未传 `policy` 时回显 `""`（存储里一直就是空引用），原来回显 `null`。
- `GET /api/nodes` 与节点创建/修改的 `data`：成员顺序由字典序变为声明顺序（存储顺序的协议参数仍按存储顺序）。
- 同一请求里「成员类型错误」与其他错误同时出现时，报哪一条可能不同（单一错误的文案和状态码不变）。
