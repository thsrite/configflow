# N8-8a provider 修复快速独立复核

范围 `batch-6@8ec68fd4d..0a0aa27f8`（四提交），作者 694df9d0；本复核者不是作者。
分支 `go-review-n8-8a-fix`，基于 `0a0aa27f8`，只改自己的 worktree。
结论：high 0、medium 1（已修）、low 0；未跑完整门禁/完整对照，未 push。

## Medium：身份分派字段被通用标量兼容规则放宽

- `f5416741b` 把 `ConfigShapes` 中 `proxies_order.type` 的 `*string` 改为
  `*model.String`：数字 123/123.0 不再拒绝，后续 GroupRefs 枚举分派丢掉此引用；
  无活动代理链时 ConfigDialers 可直接成功，不能靠后续检查补救。
- 同类排查：普通组 `type` 没有原值类型检查，数字及 R5 回退后的 bool/容器也会
  作为非 chain 组放行。后者原来已存在，本次按维护者严格身份字段合同一并补齐，
  不把它说成四提交中新引入的问题。
- 业务入口现在检查保留的 JSON，返回 `<字段> 必须是字符串，当前值：<JSON>`。
  缺少 entry.type 保持原形状错误；引用 ID 仍用通用无损文本 codec，不回退 model 规则。
- 测试 `TestReviewProviderFixOrderTypeRemainsStrict` 和
  `TestReviewProviderFixGroupTypeRemainsStrict` 修复前失败：数字 order.type 和所有
  普通组 type 例均 err=nil；其他 order.type 例诊断不符。修复后通过。
  正对照 `TestReviewProviderFixNumericReferenceIDsRemainCompatible` 验证引用 ID 123.0
  仍读成 `"123.0"`，写回数字 token 不变。修复见本报告随附产品提交。

## 其他核对

- String 用 json.Number 保留原始数字 token；Int 的 quoted 分支先检查 ASCII 十进制，
  再检查 int64 边界，拒绝加号/空白/小数/指数/Unicode 数字/溢出。普通数值保留 R8。
- 独立测试 `TestReviewProviderFixScalarContainersAndUnrelatedEdit` 检查指针、列表、
  Map 的 123.0/1E+03/大整数无损，quoted bool 仍为类型问题；无操作和只改名称
  都不改其他字段原始存储。作者严格整数文本边界测试只抽查这一条。
- Dynamic/Params 不进入新标量 codec，Mihomo 开放参数仍保留原值和 tag。
  身份的业务校验不能用 model.Issues 为空代替原值类型检查。
- 已读 provider 拓扑形状检查/错误优先级、ippresence unknown/惰性首匹配、repo
  换基适配和精确期望登记，未见其他新增问题；没有重复作者的完整对照。

## 定向验证

固定用户指定的主测试服务器（以下 `$REVIEW_HOST`），每次单包、`-p 2`：

```sh
# 修复前 FAIL 0.005s
RT_HOST="$REVIEW_HOST" rt -- bash -lc 'cd server && go test -p 2 -count=1 ./internal/convert -run "^TestReviewProviderFix"'
# 修复后 PASS 1.073s
RT_HOST="$REVIEW_HOST" rt -- bash -lc 'cd server && go test -race -p 2 -count=1 ./internal/convert -run "^TestReviewProviderFix|^TestTypedDialer|^TestTypedNumericResource|^TestTypedTopology|^TestTypedMalformedStoredReference"'
# PASS 1.021s
RT_HOST="$REVIEW_HOST" rt -- bash -lc 'cd server && go test -race -p 2 -count=1 ./internal/model -run "^TestReviewProviderFix|^TestScalarTextBoundariesAndPreservation$"'
# 无输出、退出0
RT_HOST="$REVIEW_HOST" rt -- bash -lc 'cd server && test -z "$(gofmt -l internal/convert/dialers.go internal/convert/messages.go internal/convert/review_providerfix_test.go internal/model/review_providerfix_test.go)" && go vet -p 2 ./internal/convert && go vet -p 2 ./internal/model'
```

集成门禁仍由协调者运行，本复核不宣称完整门禁通过。
