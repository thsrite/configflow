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
- [ ] 1.7 OxiDNS Settings 等类型化

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

### 1.7 OxiDNS

- 裁定见上。本轮已把 `api/*` 调用方都改成从 `*model.Config` 进入（`NewTypedProfile`），并留了 5 个过渡接口
  （`RuleConvertible`、`RuleFollowRoute`、`RuleHasIPPart`、`WithoutRuleReferences`、`SettingsText`/`SettingsStrings`），
  1.7 时随 `Settings` 结构体一起改成类型化签名。
- 起点：`settings.go`（`NormalizeSettings`/`checkTemplate` 逐字段类型检查 → 解码成 `Settings`）、`ruledns*.go`、
  `devicepolicy.go`、`converter.go` 读设置部分、`adapter.go`（`Profile` 不再把规则/组/Agent 包成 Node 文档）。
  原生文档相关文件（`common.go`、`routing.go`、`updates.go`、`native_*.go`、`constraints_*.go`、`editor.go`）
  改为边界类型 `nativeDoc`。

## 功能差异（对外）

- `api/rules` 创建/修改规则、批量添加的响应 `data`/`rules`：不再回显未声明的成员（它们本来就不会被保存）；
  成员顺序由字典序变为声明顺序。批量添加未传 `policy` 时回显 `""`（存储里一直就是空引用），原来回显 `null`。
- `GET /api/nodes` 与节点创建/修改的 `data`：成员顺序由字典序变为声明顺序（存储顺序的协议参数仍按存储顺序）。
- 同一请求里「成员类型错误」与其他错误同时出现时，报哪一条可能不同（单一错误的文案和状态码不变）。
