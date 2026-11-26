# N8-3 model 骨架 / 字节保留编解码器 / repo 行转换 复核

状态：**verified**；N8-3 未关闭问题 **0**。分支 `go-opus-review-n8-3`（rebase 到 `go` 1e0f7ac3）。
复核人不是作者（作者为 Codex，`go-n8-3`）。只用合成数据；私有快照只读、只在测试服务器私有副本上跑，
跑完副本数为 0。

## 发现与修复

| 严重程度 | 发现 | 修复 / 证据 |
|---|---|---|
| medium（接口） | 存储的列表/Map 被清空时写成 `null`：`var kept []T` 过滤后为 nil、`p.X = model.Map[V]{}` 都会把原来的 `[]`/`{}` 改成 `null`（Python 一直写数组/对象，前端会对 `null` 调 `.map`）。N8-4 以后每个列表字段都会踩到 | nil slice → `[]`，零值 Map → `{}`（与 encoding/json/v2 一致）；原来存的 `null` 在值未变时仍按 R2 原样写回。删除成员用 `*[]T = nil` 或 `Fields.Delete`。`TestReviewN83EmptyContainersStayContainers`（11 例） |
| medium（接口） | Profile 的隐藏 owner 键随结构体复制：`clone := *p; clone.ID = new` 这种最直接的 clone_profile 写法会报 duplicate profile owner；若同一事务删除原 profile，副本会写进旧键，id 和键不一致 | 解码时同时记录 ownerID；ID 未改时沿用存储键（包括格式错误的 id），ID 改了就用新 ID 作键。两个 profile 争同一键仍报错。model `TestReviewN83CopiedProfileUsesItsNewID`；repo `TestN83ReviewCopiedProfileWritesOnlyNewRows`（源 profile 行逐字节不变，store 接受提交） |
| low（R8） | `[]json.Number`、`[][]json.Number` 中的 `"1"` 被 encoding/json 当成数字接受（只有顶层做了检查） | decodeInto 逐元素检查含 json.Number 的列表；字符串整成员按 R5 处理（保留原字节，产生 issue）。`TestReviewN83NumberListsRejectStrings` |
| low | 定长数组可声明为存储类型：encoding/json 会静默丢掉多余的存储元素，之后一次编辑就会丢数据 | storedType 不再接受 Array。`TestReviewN83ArraysAreNotStoredTypes` |
| low（R2） | `Dynamic.Node()` 后不修改直接 `SetNode` 会把原字节改写成紧凑形式（行 revision 变化） | ToJSON 结果和存储值的 ToJSON 相同时保留原字节。`TestReviewN83DynamicUnchangedTreeKeepsBytes` |
| low（N8-4 防误用） | 记录类型声明错误会静默丢失：多个 Raw 字段（最后一个生效）、借用别的值的 Raw（R5 issue 到不了 DecodeDocument）、没有 Raw 字段、`,string` 之类 tag 选项被忽略 | 声明时报 ErrType：只能有一个 Raw，必须传入自己的 Raw，tag 选项只允许 omitempty/omitzero（R3 已覆盖）。`TestReviewN83RecordDeclarationGuards` |
| low（诊断） | 存储行含重复成员名（D1）时，启动报错只有 `model: invalid JSON: duplicate member`，无法定位是哪一行 | repo 的 modelFields 报 `字段 scope/owner/field 无法按配置模型读取（成员名重复或不是有效 JSON）`（常量在 messages.go），沿用 N8-2 的恢复提示；不回显值，数据库逐字节不变。`TestN83ReviewDuplicateMemberRowIsLocatedWithoutValues` |
| low（性能，记录不改） | 嵌套记录解码复杂度 O(depth³)：每一层都对子树重新编码以计算 baseline，而每次 Marshal 都会对子树做 acyclic + validJSON。实测（.64）：`next` 链深度 50/100/200 → 15 ms/105 ms/0.9 s；Map 嵌套深度 200 → 4.5 s | schema 只有几层（Profile→列表→记录），生产快照往返 13.5 s 内完成，不构成问题。设计 2.3 已记录：递归记录类型（如规则树）加入前要先测出上界。新 fuzz 目标把输入限制在 1 KiB |

## 逐项结论（任务重点 1–7）

1. **R1/R2 字节不变量**：逐项构造了对抗输入，均已加测试（`TestReviewN83AdversarialLayoutsKeepBytes`，11 种布局 × 编辑后检查）：
   - 嵌套 RawMessage 带空白、记录嵌套记录、Map 嵌套记录；
   - `\u` 形式的键（`"name"` 识别为已声明成员，键字节保留）、`\/`、代理对、孤立代理项；
   - 原始 U+2028/2029 原样保留，新写入时转义为 ` `；
   - `1e999999`、30 位整数、`-0.0e-0`、`1E+400`、int64 最小值；
   - 空对象 `{}`、`{ \t\n }` 中新增成员；R5 无效成员；
   - 新标量用 `SetEscapeHTML(false)`：`<>&` 不转义，`\x00` 写成 `\u0000`。

   校验方式：输出 json.Valid、各层键唯一（独立的 token 栈，不用 codec 自己的解析器）、未改成员字节和相对顺序不变、再次解码编码结果稳定。

   重复键（含转义后相同的键，以及嵌套在 raw/Dynamic/Map 中的重复）在解码、`Fields.Set`、写出 raw 三处都会被拒（`TestReviewN83DuplicateMembersAreRejected`）。删除首个、中间、末尾、唯一成员以及 Map 首尾项时逗号都正确（`TestReviewN83DeletingMembersKeepsValidCommas`，10 例，逐字节对照期望输出）。新增 `FuzzReviewRecordEdits`：随机组合删除、新增、指针置 nil、清空列表、删除 Map 项，并做上述独立检查。
2. **R5**：FieldIssue 只有 Path/Kind。路径来自已声明的成员名和数字下标（Map 的用户键不进路径），repo 用 logx 按 head 只记一次（包括不缓存的大文档）。作者的子进程测试 `TestModelIssuesLoggedOncePerRevision` 确认 5 次读取只记 1 次，日志中不出现密码或值。新加的行级诊断同样不带值。
3. **反射与循环检测**：visitKey =（类型，指针，slice 长度），active 集合按当前路径维护，作者对 slice 共享前缀的修复是正确的。论证如下：真正的循环一定会在同一条路径上重复同一个 (ptr,len)，所以能发现；同一指针在 DAG 中被多处引用时，出栈会删除标记，不会误判；空 slice 没有子节点；Map/Fields 采用写时复制，自身不会成环。补充测试覆盖了 DAG 共享、经 `Map[*T]` 形成的环、经 encoding/json 入口（`json.Marshal`）触发的环、间接指针环。没有发现漏判或误判。
4. **D2 / N8-2 衔接**：model.Marshal 不参与任何持久化身份（指纹、缓存名等仍按 N8-1 清单由消费方迁移），store.Fingerprint 在 N8-3 中未改。与 N8-2 原始行的衔接：行的外层空白单独保留；no-op 不写入、generation 不变；只有改动的行 revision 变化；Python 和 Go 双向互读。以上由作者的 `TestStoredRoundTrip` 覆盖（committed、full Go、Python 写入、私有快照 43 行/2 profile），本次在 .32 上复跑通过。
5. **legacy 加载校验**：`TestModelR5LegacyLoadValidationRemainsUntilN85` 用 5 种存储值断言 `Open` 失败且错误信息保持不变。N8-5 一旦拆分加载校验，`Open` 就会成功，这个测试会失败，所以确实锁定了当前行为。设计中「N8-5 prerequisite note」也指向它。
6. **N8-4..N8-11 适用性**：修完上面两条 medium 后，骨架可以直接用：
   - 记录类型写法（`raw Raw` + 指针 UnmarshalJSON + 值 MarshalJSON）现在由声明检查强制；
   - `Map`/`Fields`/`Dynamic` 写时复制；FieldIssue 路径稳定；
   - 新增成员按结构体顺序追加；`*T` 置 nil 表示删除。

   已写进设计 2.3「Settled by the N8-3 review」。仍需后续步骤自己处理的：N8-5 的三方合并会经过 Node 再解码，未改的字段必须保留当前值，否则会丢失 Raw 来源；N8-5 的 View 共享 `*Profile` 指针，只能按约定只读。这两点是 N8-5 设计内的事，不需要改 N8-3 的接口。另有一个已接受的行为：列表里只要有一个元素类型错误，整个列表成员就按 R5 读成空，代码写入非零值时会整体替换。这符合 R5 的字面规定，Python 写入的数据不会出现这种情况，生产快照中也没有。
7. **无 Python 模拟**：model 只依赖标准库和 yamlx，没有 jsonx、pystr、pyyaml，也没有模拟的异常。repo 的 legacy 适配器沿用 N8-2 已批准的 jsonx 桥（N8-11 删除）。

## 测试覆盖（新增）

- `internal/model/review_n83_test.go`：8 个表驱动或独立测试，加 `FuzzReviewRecordEdits`。
- `internal/repo/review_n83_test.go`：重复成员行的定位和不泄露；复制 profile 只新增行。
- 新测试在修复前能复现问题：clone 报 duplicate owner；数字列表不产生 issue；Dynamic 被改写；数组不报错；重复行的错误中没有定位信息。修复后全部通过。

## 最终验收（.32，`RT_HOST=sunbroke@192.168.0.32`，基点 go 1e0f7ac3 + 本分支）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server; out=$(gofmt -l internal/ tools/ cmd/); [[ -z "$out" ]] || { echo "$out"; exit 1; }; echo gofmt-clean; go vet ./... && echo vet-ok; CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO=$(dirname "$PWD") CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data go test -race -p=2 -count=1 -timeout=30m ./internal/model ./internal/repo; find "$HOME/cf-prod-snapshot/runs" -maxdepth 1 -type d -name "n8-3-roundtrip-*" | wc -l'
```
```text
gofmt-clean
vet-ok
ok  	configflow/internal/model	1.061s
ok  	configflow/internal/repo	352.406s
0
```
(开始时 load 21.57；repo race 包含真实 Python 和私有快照往返；私有副本残留 0。)

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server; CONFIGFLOW_PY=… CONFIGFLOW_REPO=… CONFIGFLOW_SNAPSHOT=… go test -count=1 -run "^TestStoredRoundTrip$" -v ./internal/repo; go test -p=2 -count=1 ./...'
```
```text
model_roundtrip_test.go:389: rows=43 profiles=2: byte/no-op, typed edits, unknowns/R5 and Python↔Go passed
--- PASS: TestStoredRoundTrip/committed-rows (0.86s) / full-go-repository (4.41s) / python-written-rows (0.80s) / private-snapshot (13.51s)
全量：74 个包 ok，0 个 FAIL；installer 191.904s、oxidns 83.012s、repo 7.664s、model 0.013s、store 1.052s
ok  	configflow/internal/yamlx	1.052s
?   	configflow/tools/proddiff/generator	[no test files]
```

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server; CONFIGFLOW_DIFF=1 go test -p=2 -count=1 -timeout=40m $(go list ./... | grep difftest)'
```
```text
ok  	configflow/internal/api/agents/difftest	119.115s
ok  	configflow/internal/api/subscriptions/difftest	74.431s
ok  	configflow/internal/discovery/difftest	1.847s
ok  	configflow/internal/health/difftest	13.551s
ok  	configflow/internal/httpx/difftest	1.353s
ok  	configflow/internal/mosdns/difftest	1.664s
ok  	configflow/internal/refresh/difftest	3.427s
ok  	configflow/internal/subs/difftest	1.757s
ok  	configflow/internal/substore/difftest	5.950s
ok  	configflow/internal/webdav/difftest	17.539s
```

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- bash -lc 'cd server; for f in FuzzRecordRoundTrip FuzzDocumentRoundTrip FuzzReviewRecordEdits; do go test -run "^$" -fuzz "^$f\$" -fuzztime=60s -parallel=4 ./internal/model; done'
```
```text
FuzzRecordRoundTrip    execs: 3095077  PASS  ok configflow/internal/model 61.030s
FuzzDocumentRoundTrip  execs: 4848780  PASS  ok configflow/internal/model 60.092s
FuzzReviewRecordEdits  execs: 5421429  PASS  ok configflow/internal/model 60.216s
```

```sh
RT_HOST=sunbroke@192.168.0.32 rt --no-sync -- server/scripts/proddiff.sh
```
```text
self-check: 23 项全部 PASS；cases 2831，compared 415，coverage_regressions=0，unmapped_routes=0，unique_differences=11（既有基线）
baseline: new=0 stale=0
```
迭代过程中曾在 .64 上跑（rt 自动选机）。
最终 rebase 到 go 0b367ae5（相对 1e0f7ac3 只多了 AGENT-BRIEF/HANDOFF 文档提交，无代码改动），验收结果不变。
