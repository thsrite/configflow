# HR 复核：batch-44（O17 1.6 CRUD 请求体与整份编辑类型化）

分支 batch-44（= go-o17-typed，go@4c99e4495 之上 5a9ac169f…1a2ed4324，11 个提交），复核 2026-11-23，测试都在 .64。
按 WORKFLOW「Leaner verification」：不重跑作者的逐包对照，自己做一次改前/改后的边界对照，再跑受影响包和 proddiff。

## 方法：改前/改后边界对照

临时探针（未提交）：同一份种子数据（节点、订阅、聚合、规则库、两个策略组、行内规则/规则集/MATCH 规则），
对 196 个请求各开一个新仓库，记录状态码、响应体（随机 ID/时间归一）和请求后的整份导出；分别在 `go`（改前）和 batch-44（改后）上运行再逐项比较。
请求覆盖规则/规则集创建修改删除、批量添加、改策略、改组名、排序、DNS 选择；节点增改；策略组增改删、批量健康检查 URL、排序；
聚合、订阅、规则库的增改删、草稿内容、排序。每类都有：成员类型错误（文本给数字、开关给文本、列表给标量、列表元素类型错）、显式 `null`、
大小写不同的成员名（`Rule_Type`/`VALUE`/`IDS`/`Name`）、重复成员（取后者）、未声明成员、嵌套重复成员、非法 UTF-8、非对象请求体。
探针在 httpxtest 下运行，结束时仓库的借用快照守卫（`CheckBorrowed`）对每个用例复查。

结果：**所有用例的存储结果相同**（唯一差异是每次随机生成的 `rule_proxy_token`），**所有失败用例的状态码和中文提示相同**。
差异只在成功响应的回显上（见 M1）。

## 发现

### 高

无。

### 中

**M1 1.6 的对外变化没有写进 behaviour-changes.md，且有三处回显差异作者未记录（已补，见提交）**
进度文件里列了「规则回显不含未声明成员、声明顺序、批量添加 policy 回显 `""`」和节点的成员顺序，但 `behaviour-changes.md` 没有 1.6 节。
对照另外发现：
- 节点创建/修改的 `data` 由「原样回显请求」变成「保存后的节点」：`"name":5` 回显 `"5"`，`"enabled":"x"` 回显 `true`，`"port":"443"` 回显 `443`，
  `"port":"bad"` 不回显。存储与以前相同（以前这些值也这样保存）。
- 规则修改发 `policy: null` 回显 `""`（以前 `null`）；发 `remark: null` 等可选成员为 `null` 时回显里没有该成员（以前 `null`）。
- 聚合、订阅创建/修改的 `data` 成员顺序按请求顺序（以前字典序，`map` 编码）。
前端只读这些响应的 `warnings` 等顶层成员（`RuleEditorDialog.vue`、`Nodes.vue` 不读 `data`；批量添加前端未调用），
MCP 的 `manage_node`/`manage_rule` 把 `data` 作为 `item` 转给 Agent，只用其中的 `id`（`manageRule` 的置底排序）。不依赖 `null`。
都写进 behaviour-changes「O17 1.6」节。

### 低

**L1 `detach` 只复制指针成员，没有守卫（已补测试）**
部分更新只有一处把请求解码到存储记录的副本上：`api/rules` 的 `decodeRule`（规则/规则集修改）。`detach` 给每个非空指针成员换新地址，
`model.RuleConfig` 目前只有文本和 `*string`/`*bool`，所以够用；但以后给规则加切片、map 或嵌套记录成员时，encoding/json 会复用原切片底层数组 /
合并进原 map，写穿到快照，而现有测试只检查解码结果，不检查原记录。新增 `rules.TestReviewBatch44DecodeRuleLeavesBaseUntouched`：
① `RuleConfig` 每个成员只能是文本或指向文本/布尔的指针；② 每个指针成员都发一个新值，解码后原记录编码不变。
负向对照：注释掉 `detach(&in.ruleWire)` 后测试失败（`enabled`、`remark`、`group_name`、`no_resolve` 等被改写到原记录）。

**L2 `changedMembers` 不写「编码后缺失」的成员（与改前相同，未改）**
`repo.changedMembers` 和被删掉的 `api/rules.applyFields` 一样跳过编码后不存在的成员。profile 里只有 `oxidns`（`Dynamic`）和 `client_fixed_ips`
是 `omitzero`；本批的规则编辑只会把 `oxidns` 改成仍有成员的设置（删规则时去掉引用，列表留空），不会变成零值，所以没有漏写。
如果以后某个编辑把这两个成员清成零值，清空不会落库——建议届时让 `changedMembers` 对「前有后无」的成员调用 `Clear`。

## 逐项结论

1. **严格输入**：见方法。`jsondecode.Member[T]` 语义逐项核对：缺失（`Present` 假、`Or` 给默认值）、`null`（`Present` 真、`Null` 真、`OK` 假）、
   类型不对（`Raw` 保留原文、`OK` 假、不让外层解码失败）、成员名精确匹配（大小写不同视为缺失/未声明，与改前 `map`/`Fields` 精确取键相同）、
   重复成员由外层 `lastMemberHook` 先归并为最后一次再交给 `Member`（`Member` 是自定义解码器，自己不归并；外层结构体是普通对象，归并在它之前发生）。
   订阅/聚合的请求体由 `map[string]json.RawMessage` 改成 `model.Fields`（后者拒绝重复成员和非法 UTF-8），但请求体先经 `c.Bind` 归一，
   对照里重复成员、嵌套重复成员、非法 UTF-8 的结果改前改后一致。
2. **部分更新/写穿**：只有 `decodeRule` 解码到存储副本（L1）。其余：节点、规则库来源、各种 DTO 都解码到新值；策略组/聚合/订阅/规则库条目用
   `model.Fields`，`Set`/`Delete` 都先克隆条目（写时复制），`merged := original` 不会改到 `original`；`UpdateConfig` 的配置是 `resolveConfig(…, shared=false)`
   的私有副本（`before` 另行编解码），`LoadCatalog` 是 `CloneShared` 深拷贝，`UpdateShared` 是事务工作副本。删除路径的新切片
   （`slices.Clone` 后 `slices.Delete`、`kept := []string{}`）不碰快照。受影响包 `-race` 通过；探针每个用例都过了借用快照守卫。
3. **整份编辑只存变化成员**：profile 侧 `changedMembers` 与旧 `applyFields` 规则相同（跳过缺失、按编码比较；旧版的 `sameJSONValue` 语义比较在两边
   同一编码器输出时与字节比较等价）。共享目录：旧 `SaveCatalog` 也是先把 JSON 严格解码成 `model.Shared` 再 `SaveSharedRecord`，新版直接传类型化值，
   同一条三方合并路径；规则库的 `editShared`/`editSystem` 由「JSON 往返 + 按成员 SetJSON」改为在事务工作副本上直接改，提交由 T1a 的逐行比较决定写哪些行。
   对照里删除（规则、策略组、聚合、规则库条目）、清空（`value:null`、`remark:null`、`delete_fields`）、改名联动（策略组改名改规则 policy）的存储逐项相同。
   L2 是未覆盖的边角（与改前相同）。
4. **保留 `model.Fields` 的三处 + MCP `object`**：
   - MCP `object`：成立。Agent 发来的资源/设置变更原样转发给面板 API 校验，API 结果原样转回 Agent，本来就是开放 JSON；固定形状的出站请求已改为 DTO。
   - 订阅的 `proxy`（Mihomo 代理定义）：成立，协议成员开放。
   - 策略组/聚合/订阅新增替换、规则库条目：理由（按类型清理要丢的成员不论类型、repo 的类型错误文案定义在结果列表 JSON 上）可以成立，但不是必须。
     规则已经示范了更干净的做法：`type xxxWire model.Xxx` + 对「要校验、可能被丢弃、要引用原值」的成员用 `jsondecode.Member` 遮蔽，
     类型错误用 `model.MemberTypeError{Record, Member, Value: 结果列表}` 交给 `repo.InputError` 得到与导入相同的文案（`ruleTypeError`、`libraryTypeError`）。
     建议后续（不阻塞）：① 把 `ruleTypeError`/`libraryTypeError` 提成一个泛型 `repo.ListTypeError(record, member string, list any, cause error)`；
     ② 策略组按规则的模式改：`delete_fields` 和按类型清理的约 25 个成员用 `Member` 遮蔽（不论类型都能丢），`MergeGroup` 直接吃 DTO；
     ③ 聚合、订阅的新增/替换是整条替换，没有「丢弃不论类型」的问题，改成 `xxxWire` + 严格解码最直接；
     ④ 规则库条目的草稿修订号、「未发送则继承 update_interval」可用 `Member[...]` 的 `Present` 表达，原样校验用 `Raw`。
     ③ 最便宜，② 最大（策略组的清理表要与 `rules.GroupMembers` 一起整理）。
5. **对外变化**：见 M1；proddiff new=0 stale=0。

## 提交（batch-44，1a2ed4324 之后）

- 6e4225a3f go(api/rules): review test — decoding a request onto a stored rule never writes through it
- （本提交）go(parity): batch-44 review — O17 1.6 behaviour notes and report（本文件 + behaviour-changes「O17 1.6」节）

## 验证（.64）

- `gofmt -l internal/ tools/ cmd/` 无输出；`go vet ./...` 通过。
- `go test -race -count=1 -p 6`：api/rules（含 difftest）、api/nodes、api/proxygroups、api/aggregations（含 difftest）、api/subscriptions（含 difftest）、
  api/rulelibrary（含 difftest）、mcp（含 difftest）、rules、rulefetch、jsondecode、aggregations 全部 ok。
- `CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON=~/cf-test/.venv/bin/python`：api/rules/difftest 45.0 s、api/aggregations/difftest 32.4 s、
  api/subscriptions/difftest 64.6 s、api/rulelibrary/difftest 24.7 s、mcp/difftest ok。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`：self_check PASS，2831 cases，`baseline: new=0 stale=0`，`GATE STAGE PASS: proddiff`。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-44 -p 6`（负载 2.8）：vet 两套 tag 通过，`PREFLIGHT PASS`（树含 6e4225a3f；之后只有文档提交）。
- 改前/改后对照探针：196 个请求，存储全同，失败用例文案/状态码全同（探针与比较脚本未提交，服务器上的临时副本已删）。
