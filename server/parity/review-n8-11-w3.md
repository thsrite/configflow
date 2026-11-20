# N8-11 W3 资源 API 簇复核（fresh Opus，provider 投递与 token 路径按 HR 标准）

复核对象：`go..54ab967d4` 的四个提交（5f07fe778、78b252ba3、ae2276e9d、54ab967d4）。复核分支 `go-opus-review-n8-11-w3`。
对照的是 Python（`backend/common/auth.py`、`backend/routes/{subscriptions,aggregations,nodes}.py`、`backend/common/node_deletion.py`、
`backend/utils/reorder.py`）和 `go` 上的旧 Go。协调者已给的裁定（config_token 类型损坏 fail closed、aggregations 非有限 JSON → 400、
refresh/env.go fallback → DefaultRefreshEnv、三处越界授权、ValidateSystemTokenOrJWT 新入口）只核对了实现，没有重新讨论。
按 Leaner verification，没有重跑作者整套差分，只跑修复相关的定向测试和少量抽查；测试只在 .32 上经 `rt` 运行，回归测试只用合成数据。

## 结论

high 0，medium 1（已修，附 Flask 差分回归测试和负控），low 3（记录）。另有 1 项怀疑经 Flask 差分证伪，不计入。

## 发现

| # | 级别 | 状态 | 内容 |
|---|---|---|---|
| M1 | medium | 已修 | **`/api/nodes/delete` 两个 400 的先后顺序与 Flask 相反。** 位置 `api/nodes/crud.go` `deleteNodes`。W3 把 ids 形状检查从 legacy facade 移到 HTTP 绑定，但仍放在确认摘要检查之前。Python `delete_nodes_with_references` 先检查 `confirmation`（`请先查看并确认删除影响`），再由 `plan_node_deletion` 检查 ids（`请选择要删除的节点`）。所以 `{"ids":"n"}`、`{"ids":[1]}`、`{}` 这类请求不带确认时，Go 返回的中文消息和 Python 不同。旧 Go 的 facade 也是这个顺序，W3 接手绑定后没有改正；作者的 `TestTypedDeletionRequestShapes` 只测了 `/delete-preview`，碰不到这个问题。修复：形状不对时把 ids 置空，由 repo 按原顺序处理：`DeleteNodesWithReferencesIDs` 先检查确认，然后由 planner 对空选择返回 `msgSelectNodes`。preview 路径的结果不变。测试 `TestReviewN811W3DeletionMessageOrder`（subscriptions/difftest，真实 Flask）：对 4 种请求体（非列表、数字成员、缺 ids、带合法形状确认但 ids 非列表）分别调用 `/delete` 和 `/delete-preview`，比较状态码、正文和存储。负控：在服务器副本上恢复原来的提前返回，`/delete` 三处失败，报错 `.message Python=请先查看并确认删除影响 Go=请选择要删除的节点`；还原后通过。 |
| L1 | low | 记录（请协调者裁定是否收紧） | **数字型 `config_token` 会被当作文本凭据。** 位置 `auth/token_validation.go`。按 model 的 R5 标量规则（`model.String`），存储里的数字 `12345` 会解码成文本 `"12345"`，而且不记 issue。所以 `IssueUnder(..., "config_token")` 不会触发 fail closed，`?token=12345` 会通过。Python 里 `url_token == 12345` 永远为假：开启登录时，数字令牌不接受任何 URL token；关闭登录且值为假（`0`）时全部放行。这和“类型损坏 fail closed”的裁定意图不完全一致。不过存储值本身就是运维配置的秘密，只有手工编辑或导入才会出现数字，所以记为 low。作者的 `TestNativeMalformedConfigTokenFailsClosed` 的 `"0"` 用例在请求不带 token 时得到 401，并没有验证 fail closed（带 `?token=0` 会通过）。如要收紧：读取 `MemberJSON("config_token")`，原始值不是 JSON 字符串或 null 时按损坏处理，并把测试改成带 `?token=<同值>`。 |
| L2 | low | 记录 | **aggregations 的 `enabled` 真值判断漏了浮点零。** 位置 `api/aggregations/adapter.go` `enabled`。它按原始字节匹配 `null/false/0/""/[]/{}`，但存储中的 `0.0`、`-0`、`0e0`（经 R5 保留原字节）在 Python 里为假，在 Go 里为真。旧 Go 用 `jsonx.Truthy`，可以处理。只有手工写入的浮点零会触发。修复建议：先把原始值解码为 `json.Number`，再判断是否为零。 |
| L3 | low | 记录 | **删除 planner 在检查 ids 之前就会先报 dialer 或 profile 解析错误。** 位置 `api/nodes/adapter.go` `Wire`。hook 先对所有 profile 调 `ConfigFrom`、对所有节点调 `NodeDialer`，然后才进入 `planDeletion` 检查 ids。Python 的 `plan_node_deletion` 第一步就校验 ids。只有存储里本来就有坏 dialer，并且请求的 ids 也不合法时，两边报的错误才不同。旧 Go 的顺序也是这样。 |

证伪的怀疑：`editSubscriptions` 用 `MergeJSON(before, before, after)` 合并列表时，按 id 去重（“first position, last value”）。我怀疑 POST 一个已存在的 id 会覆盖原有订阅，而 Python 是追加。真实 Flask 差分显示两边都返回 400：存储校验拒绝重复 id，所以库里不会有重复 id，PUT/DELETE/reorder 也都和 Python 的 `save_shared_config` 三方合并一致。回归用例没有保留。

## 逐项核对

1. **token 校验（HR 标准）。** `ValidateSystemTokenOrJWT` 与 Python `validate_token_or_jwt` 的顺序一致：internal call → 当前和退役 rule-proxy token 拒绝（bearer 和 `?token=` 都查）→ 未开启登录且没有 config_token 时放行 → JWT → `?token=`。所有比较都经 `equalCredential`（sha256 后 `subtle.ConstantTimeCompare`），退役列表逐项比较，不短路，和旧 Go 一致（Python 用集合成员判断，Go 更严格）。退役列表从 `MemberJSON` 原始值逐项解码，非字符串成员跳过，坏成员不会抹掉有效的内部 token（Python 是 `isinstance(token, str)`）；`?token=` 超长时置空。nil system 路径先 Detach，再检查 `ctx.Err()`，然后才读库。旧 `ValidateTokenOrJWT` 只把参数转码后委托新入口，W5 的两个快照调用仍传非 nil。损坏 config_token（false/[]/{}）fail closed 已核对，数字型见 L1。新增改动没有新日志；错误信息走 `weburl.SafeErrorDetails`，不含 URL token。
2. **provider 投递。** subscriptions 和 aggregations 的公开路由仍然先鉴权，再查资源是否存在，最后解析 filters，和 Python 一致（路由文件 `providers.go` 本轮未改）。`provider.Serve` 只用 typed config 的 digest 做 single-flight 键，渲染输入仍是完整 config；last-good 回退、头部（`text/yaml; charset=utf-8`、`X-ConfigFlow-Provider: last-good`）都没改。默认 fetch：`provider.DefaultDeliveryEnv().Fetch`、refresh 的 `DefaultRefreshEnv().Sources.Fetch`、aggregations 删除的 `defaultFetch`，三者都调用 `substore.Default().Client.Fetch(ctx,id,url,"",FetchOptions{})`，和 `go` 上的旧路径等价；外部旧 callback 只经 `sourceFetch` 转换。作者的 provider 字节比较（`ReviewN88dProviderByteSamples`、`SlowProviderParity`）读过，没有重跑。
3. **catalog 三方合并。** `LoadCatalog` 用 `repo.SharedConfig` 记录 Scope baseline，`SaveCatalog` 用 `SaveSharedRecord(ctx,&doc,nil)` 按 baseline 逐成员合并（W1 已复核），`AppendCatalog` 在单个 `UpdateShared` 事务里追加。语义与 Python 的 `save_shared_config(baseline)` 和 `update_shared_config_transaction` 对应。
4. **节点删除与 ids 形状。** typed hook 只把 ids 交给原 planner；确认摘要检查在 repo（`confirmationPattern` 全匹配 64 位小写十六进制）。`[null]` 解码成 `[""]` 后被 planner 的空白检查拒绝，和 Python 的 `not isinstance(value, str)` 结果相同。别名 dialer 经 `yamlx.Resolve` 处理，作者已加 Flask 差分。消息顺序见 M1。
5. **ResourceUsage DTO。** `repo.Usage{kind,id,name,profile_id?,profile_name?}`，空结果是 `[]` 不是 `null`（`resourceUsages` 从 `[]Usage{}` 开始），和旧 jsonx 列表一致。
6. **被删 facade。** 作者的完整 preflight（含两套 tag 的 vet 和全量编译）在 ae2276e9d 通过；我的修改之后再跑一次 preflight，结果见下。生产代码里没有残留调用。棘轮 `legacy_gate_allow.txt` 只删不增。
7. **MosDNS pyref。** 五份 golden 在 .32 上解压，和 `go` 版本比较：除 `metadata.sources` 外，`entries`、`version` 和其余 metadata 完全相同。
8. **ctx/Detach（第四条）。** `editSubscriptions`、`appendImported`、`mutateNodes`、`AppendCatalog`、`SaveCatalog` 都先 Detach、检查 `ctx.Err()`，再进入 `UpdateShared`/`SaveSharedRecord` 事务（事务内会再查一次并 `CommitContext`）。refresh/health 的共享文件写入（`writeSharedData`、`WriteLatencyFile`）在 `WriteSharedText` 之前检查 ctx，leaf store `writeSharedText` 自己也会 Detach 并检查。作者用真实 `Runtime.Start/BindRequest/Shutdown` 测了订阅的四个写入口。
9. **测试替身与负控。** refresh Flask harness 只关掉自动 scheduler 的后台启动，没有覆盖被测的超时或 fetch 参数。作者的两个负控（内部 token 守卫、断开 fetch callback）都会让测试失败，不会空过。M1 的负控见上。没有 Python 仿真。

## 复核运行（.32）

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/api/subscriptions internal/api/nodes)" && go vet ./internal/api/subscriptions/... ./internal/api/nodes && export CONFIGFLOW_DIFF=1 CONFIGFLOW_DIFF_PYTHON=$HOME/cf-test/.venv/bin/python && go test -race -count=1 ./internal/api/nodes ./internal/api/subscriptions/difftest -run "TestReviewN811W3|TestTypedDeletionRequestShapes|TestReviewedDeletion|Delet" 2>&1 | tail -4; echo "== negative control: ids guard before confirmation"; B=$HOME/.cache/rt-tmp; cp internal/api/nodes/crud.go $B/zzc.go && sed -i "s/^\t\tids = nil$/\t\treturn httpx.Message(400, false, msgSelectNodes), nil/" internal/api/nodes/crud.go && go test -count=1 ./internal/api/subscriptions/difftest -run "TestReviewN811W3" 2>&1 | grep -E "^(--- |ok|FAIL)|body:" | head -6; cp $B/zzc.go internal/api/nodes/crud.go; echo restored; go test -count=1 ./internal/api/subscriptions/difftest -run "TestReviewN811W3" 2>&1 | tail -1'
```

```text
ok  	configflow/internal/api/nodes	6.588s
ok  	configflow/internal/api/subscriptions/difftest	30.395s
== negative control: ids guard before confirmation
--- FAIL: TestReviewN811W3DeletionMessageOrder (0.84s)
    diff_test.go:342: POST /api/nodes/delete body: .message Python=请先查看并确认删除影响 Go=请选择要删除的节点
FAIL	configflow/internal/api/subscriptions/difftest	0.851s
restored
ok  	configflow/internal/api/subscriptions/difftest	0.858s
```
