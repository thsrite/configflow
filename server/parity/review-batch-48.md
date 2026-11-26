# HR 复核：batch-48（O17 1.6 收尾：聚合/订阅/规则库/策略组请求体强类型化）

分支 batch-48（= go-o17-typed-rest，作者 Gemini Neon，7 个提交），复核 2026-11-23，测试都在 .64（`GOTMPDIR=~/.cache/rt-tmp/b48`）。

rebase：`git rebase --onto go go-o17-agent-small batch-48`（go@c92d92266），无冲突。rebase 后的作者提交：
b3a20d586（aggregations）、d2246e279（subscriptions）、ad9acee49（rulelibrary）、7cb849cf7（proxygroups）、d149bda97（报告）、ddf3721bf（proxygroups 复用 BindObject）、ce4de4abe（报告）。
复核提交：de62121d0（修正 + 测试）、本报告提交（文档）。

## 方法：改前/改后边界对照

临时探针（未提交，同 batch-44 做法）：同一份种子（两个订阅、手动节点、两个聚合、url/content 两条规则库、三个策略组 url-test/select/load-balance、一条规则），
每个请求开一个新仓库，记录状态码、响应体（随机 ID、时间、修订号归一）和请求后的 `ExportAllJSON`；在 go@c92d92266（改前）与 batch-48（改后）上各跑一遍逐项比较。
共 463 个请求：四类资源的创建/替换各配约 30 个变体（未声明成员、大小写不同的成员名、重复成员、数字/文本 id、每个已声明成员的类型错误和 `null`、
派生/统计成员、清空列表、`delete_fields`、按类型清理的成员、改名冲突、改类型补默认健康检查）；规则库的草稿、check-content、cache、proxy-domains、test-single；
所有路由的 8 种非对象请求体（`[]`、`null`、标量、`{}`、截断、多值、空体）和 `text/plain`；删除与列表。httpxtest 的借用快照守卫每个用例都检查。

第一轮（作者原样）：35 个请求状态码/文案/存储有差异（见 M1、M2、L1）。修正后第二轮：**状态码、中文提示、存储全部相同**，只剩 `_chain_dialer` 两个用例（L1，有意）
和 61 个只差回显的用例（已登记）。

## 发现

### 高

无。

### 中

**M1 订阅创建/修改、规则库创建：类型不对的已声明成员由「按存储规则读入、200」变成 400（已修，de62121d0）**
改前这三条路径把请求按存储规则读入（`model.Subscription`/`RuleLibraryItem` 的宽松 `UnmarshalJSON`）：数字读成文本（`"type":5` 存 `"5"`、`"name":5` 存 `"5"`、
`"id":5` 存 `"5"`），`"enabled":"x"` 存 `true`，非对象 `health_check` 当缺失，`{"interval":"x"}` 存 `{}`，规则库 `"update_interval":"x"` 当缺失。
作者改成 `DecodeInputMember` 严格解码，这些请求都变成 400；数字 `id` 的订阅创建还报了不相干的「subscriptions 中每一项必须有 ID」。作者的 behaviour-changes 把它记成
「以前静默丢弃，现在报错」，与实际（以前是转换后保存）不符，也违反「状态码与文案不变」。
修法：`decodeSubscription` 和规则库创建的 `decodeItem(..., strict=false)` 对每个出现的已声明成员调 `model.SetStoredMember`（与整条宽松解码同一套叶子规则）；
订阅的 `subscriptionTypeError`/`subscriptionErrorItem` 随之删除。规则库修改、聚合、策略组改前就是严格的，保持不变。
表驱动测试 `subscriptions.TestReviewBatch48LenientMembers`、`rulelibrary.TestReviewBatch48CreateLenientReplaceStrict`。

**M2 聚合/订阅创建：数字 `health_check_url` 的文案变了（已修，de62121d0）**
改前 `check.InitProviderHealth` 把旧字段 `health_check_url` 按文本读（数字也读成文本）作为默认健康检查的 URL，于是 `"health_check_url":5` 报
「探测 URL 必须是无凭据的 HTTP(S) 地址」。作者的 `defaultProviderHealth` 只认 JSON 文本，加上严格解码，聚合报「subscription_aggregations 类型不正确」、订阅报「subscriptions 类型不正确」。
修法：新增 `check.ProviderHealthDefaults(legacyURL json.RawMessage)`（按 `InitProviderHealth` 的规则读旧 URL），替换两个包里逐行相同的 `defaultProviderHealth` 副本；
聚合的类型错误条目带上创建时补的默认 `health_check`，让仓库先报 URL 文案（与改前同序）。表驱动测试 `aggregations.TestReviewBatch48TypeErrors`（负向对照：
去掉补默认健康检查后「create number legacy URL」失败），`check.TestProviderHealthDefaults`。

### 低

**L1 策略组 `_chain_dialer`/`_chain_original_candidates` 不再从请求存入（不改，已登记）**
改前策略组创建/修改把请求里这两个已声明成员原样存入，修改时也保留已存的值；`groupRequest` 不声明它们，现在请求里的被忽略、修改后已存的被去掉。
它们只由生成时的代理链展开写在运行时副本上（`model.ProxyGroup` 注释：editors never write them），编辑器从不发送，正常数据里没有。
保留现状，登记在 behaviour-changes「O17 1.6」。

**L2 作者的 `BindObject[json.RawMessage]` + `jsondecode.Unmarshal` 绕法（已去掉，de62121d0）**
go 上的 batch-45 修正已让 `httpx.DecodeObject` 精确匹配成员名，聚合、订阅、规则库（`decodeRequestObject`）和策略组的 `decodeObject` 改为直接 `httpx.BindObject[DTO]`。
策略组的创建/修改例外：`Service.Create/Mutate` 是导出方法，被 `rulelibrary` 的测试以原始 JSON 调用，`groupRequest` 不导出，所以仍传 `json.RawMessage`、
在 `decodeGroupRequest` 里解码成 DTO（这不是大小写绕法）。

**L3 回显变化（作者已登记，复核改写）**
作者写的「按结构体声明顺序输出」不对：HTTP 响应成员仍是字典序（改前改后一致）。实际差异是：不回显未声明/大小写不同/派生成员；值为 `null` 的可选成员不回显；
按存储规则读入的成员回显读入后的值。前端：策略组只读 `message`/`warnings`（`notifyGroupSaved`），订阅、聚合、规则库页面的创建/修改不读响应；唯一读 `data` 的是
`RuleSourceEditor.vue`（规则库 PUT 后把 `data.data` 当作来源覆盖显示），它发送的成员（`id`、`name`、`source_type`、`behavior`、`enabled`、`url`/`format` 或 `content`/`format`）
类型都正确、不含 `null`，对这种请求改前改后回显相同（都带继承的 `update_interval`）；
MCP `crud` 把 `data` 作为 `item` 原样转给 Agent，不依赖其中成员。behaviour-changes「O17 1.6」改写为实际差异。

## 逐项结论

1. **严格输入**：修正后 463 个请求状态码和中文提示全同。类型错误（各成员）、`null`、大小写不同（`Name`/`Subscriptions`/`URL`/`Content`/`Rule_IDs`/`Revision`/`Manual_Nodes`）、
   重复成员（取后者，`Bind` 先归并）、未声明成员、非对象/截断/多值/空体、`text/plain` 都覆盖。
2. **存储**：四种记录都没有「未声明成员」容器（`model.Aggregation`/`Subscription`/`RuleLibraryItem`/`ProxyGroup` 都是固定成员），改前请求里的未知成员也不保存
   （对照里 `future` 用例存储相同），**不存在丢用户数据**。唯一存储差异是 L1。只写变化成员：聚合替换走 `SaveCatalog` 三方合并、规则库/策略组在事务工作副本上改，
   对照里删除成员（替换时省略 `description`/`health_check`）、清空列表（`subscriptions:[]`、`manual_nodes:[]`）、`delete_fields`、改名联动规则 `policy` 的存储逐项相同。
3. **部分更新/失败不改快照**：聚合 `item` 用 `LoadCatalog` = `CloneShared` 深拷贝，`shared.Aggregations[index] = agg` 不碰快照；规则库 `slices.Clone(catalog.RuleLibrary)`；
   策略组 `requestFromGroup` 经编解码得到新值、`overlayGroup` 结果再 `buildGroup` 解码成新记录、`append(slices.Clone(groups), …)`。没有把请求解码到存储记录上的路径。
   三个新测试对每个被拒请求比较请求前后整份导出；探针 463 个用例都过了借用快照守卫；受影响包 `-race` 通过。
4. **回显**：见 L3，前端/MCP 不依赖；proddiff 见下。

## 验证（.64）

- `gofmt -l internal/ cmd/ tools/`（只列出了当时未提交的临时探针，已删除）；`go vet ./...` 通过。
- `go test -race -count=1 -p 6`：api/aggregations（含 difftest）、api/subscriptions（含 difftest）、api/rulelibrary（含 difftest）、api/proxygroups、health/check、
  httpx/...、mcp（含 difftest）全部 ok（修测试期望后 aggregations 单独重跑 `ok configflow/internal/api/aggregations 5.021s`）。
- `CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON=~/cf-test/.venv/bin/python`：aggregations/difftest 42.3 s、subscriptions/difftest 89.2 s、rulelibrary/difftest 32.3 s、mcp/difftest 2.0 s，全部 ok。
- fuzz 60 s：`FuzzBindNativeJSON`（httpx）PASS、`FuzzEditableContent`（rulelibrary）PASS。
- 改前/改后对照探针：463 个请求，状态码/文案/存储全同（除 L1 两例），探针与比较脚本未提交，服务器临时副本已删。
- `GATE_ONLY=proddiff scripts/gate.sh -p 6`（de62121d0）：self_check PASS，2831 cases，`baseline: new=0 stale=0`，`GATE STAGE PASS: proddiff`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-48 -p 6`（de62121d0，负载 1.5）：vet 两套 tag 通过，`PREFLIGHT PASS`。之后只有文档提交。
