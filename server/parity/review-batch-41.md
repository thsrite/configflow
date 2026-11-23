# batch-41 复核（O18 T2 / T3 / T4）

日期 2026-11-23。复核者 Opus 5.5。分支 `batch-41`（rebase 到 go@481281d3e 之上）。
被复核提交：437126493（T2）、84805bdd3（T3）、55d1b207a（T4）、ecb4a159d、21b0b126c。
复核新增提交：b56242876（T2 构造测试）、ab2041456（ValueError/TypeError 改名，协调裁定）、本报告。

## 结论

没有高、中级问题，也没有行为变化。T3 与改前实现逐值差分一致；T2 的合并键比旧 digest 更细（不会把结果不同的请求合并）；
T4 状态码和文案不变，被删的 ValueError 类 HelperError 在改前就没有构造点。

## T3 jsondecode → encoding/json/v2

背景：Go 1.27 的 `encoding/json`（v1）本身就是 `jsonv2.Unmarshal(data, v, DefaultOptionsV1())`
（服务器 `go list encoding/json` 只编译 `v2_*.go`）。所以新实现和 `json.Unmarshal` 的区别只剩两点：
`MatchCaseInsensitiveNames(false)`，以及有重复成员时用 hook 把普通 struct/map 的对象缩成最后一组成员。
错误类型、非法 UTF-8（替换为 U+FFFD）、深度上限 10000、尾随数据、BOM、空输入、大数溢出都走同一套 v2 代码，与改前一致。

验证方法：把改前的 `jsondecode.go` 原样搬进临时测试文件（改名为 legacy*，**未提交**，副本在 `/tmp/b41-review/`），
写差分 `FuzzB41LegacyDifferential`，对 8 种目标逐一比较新旧 `Unmarshal`/`Decode`/`Select`：
错误有无、错误类型、成功时 `reflect.DeepEqual` 完整结果（RawMessage/自定义 codec 字节逐字节相同）。目标包括：
嵌入字段的支配与歧义、大小写别名（`Password`/`password`/`ID`/`id`）、`,string`、`json.Number`、
已预填的目标（指针、map、`any` 里放非 nil 指针，即合并语义）、`map[int]string` 的 `"01"`/`"1"`、`[2]T`、
嵌套 `map[string][]T`、`*any` 里放指针、`[]DTO`。种子覆盖：重复（转义名、嵌套、map 里、非法 UTF-8 名 `\xff`/`\xfe`）、
空键重复、BOM、尾随数据、深度 9990/10001、`1e400`、超出 int64/uint8、`\ud800`。

- 种子全部一致；fuzz 120 s：231 098 次执行，无差异。
- 作者的三个 fuzz 各跑 120 s：`FuzzBoundary` 1 422 156 次、`FuzzReviewNativeSemantics` 3 099 256 次、
  `FuzzSelectExactMembers` 4 077 931 次，全部 PASS。
- 安全边界：没有任何输入从「拒绝」变为「接受」或从「最后一个」变为「第一个」（差分比较的就是这两点）。
  自定义 codec 和 RawMessage 收到的字节与改前相同，所以 model.Fields / Agent 协议照旧自己拒绝重复成员。
- `rawNumber` 代替 `UseNumber`：v1 选项带 `AllowDuplicateNames`，v2 不走 `unmarshalValueAny` 快路径，
  嵌套在 `any` 里的数字也会经过 hook，差分里 `Decode` 的 `any` 嵌套数字一致。
- 低（不处理）：有重复成员时，hook 在改写后的对象上解码，错误里的 offset 指向改写后的字节。
  改前实现同样先改写再解码，而且调用方都不把这段文本返回给用户。

## T2 provider 合并键

- `ConfigAt` / `borrowAt` 与原来的 `viewDocument` 读的是同一份文档（事务内是工作副本，否则是 `readEntry`）。
  系统视图投影只出现在工作副本路径上，这条路径不合并（solo）。
- `store.Head{StoreID, Generation, CommitID}` 是仓库缓存自己使用的身份（`loadEntry` 按 head 命中缓存）；
  head 相同就是同一份已提交文档。`resolveConfig(document, profileID, roots)` 只读文档，所以新键比旧的
  「配置规范化 JSON 摘要」更细：旧键合并的请求，新键要么合并到同样内容，要么各自渲染（只会少合并，不会错合并）。
- 调用 `Serve` 的只有订阅单个 provider（roots = `[id]`，id 就是 itemID）和聚合下载（roots = `[agg_id]`）。
  渲染闭包只读 `format`、`regex`/`exclude_regex`（`RequestedFilters`）和解析好的 config/agg，不读 UA、Host
  或其他请求头。这三个参数都在键里。订阅缓存的更新经 `commitStaged` 写进仓库，会改变 head。
  Sub-Store / 远端订阅内容这类仓库外的输入，旧键同样不包含（与改前相同，也与 Python 相同）。
- 作者测试只覆盖了 regex 一个维度。新增 `TestReviewB41FlightNeverJoinsDifferentRenders`：第一次渲染被挡住时，
  再发一个只差一个输入的请求（format、无 format、regex、exclude_regex、profile、资源 id、generation、commit、
  store、工作副本），要求它单独渲染，并拿到自己的内容。另有 `TestReviewB41FlightKeyFields` 固定键的字段。
  变异检查：把 `exclude_regex` 从键里去掉，测试失败（超时，现在报 “second request joined”）。

## T4 哨兵错误

- `Error.Is(ErrRejected)` 的条件和旧 `IsValueError` 对 `*Error` 的条件逐字相同（Validation/Exists/InUse/NodeDeletionChanged）。
  16 处调用逐一对照，都是 `IsValueError(err)` → `errors.Is(err, ErrRejected)`，分支顺序没变。
- 死代码确认：改前全树（不含测试）只有 `resolve.go` 两处构造 HelperError，都是 `ClassKeyError`。
  ValueError / DialerReference / OxidnsSettings 三个类从未构造，也没有调用方按 KeyError 分支。
  所以这两处换成 `*MissingError` 后仍不属于 ErrRejected（仍是 500），`Error()` 文本一样
  （`strconv.Quote` 等价于 `%q`，`'%s'` 不变）。
- 低（理论差异，不处理）：`errors.Is` 会沿错误链往下找，旧 `IsValueError` 只看链上第一个 `*Error`。只有当
  非拒绝类的 `*Error`（例如 `ErrRuleLibraryCreationUncertain`）的 Cause 里还包着拒绝类的 `*Error` 时，两者结果才会不同。
  已查所有 `Cause:` 构造点：Uncertain 只在提交报错、而库里已经有这条规则时才会构造，这时 cause 是存储错误，
  不是校验错误；而且 rulelibrary 路由先判断 Uncertain（返回 503）。没有可达路径。
- 文档：PORTING.md 仍写着已删除的 `IsValueError`，已在 ab2041456 中改成 `ErrRejected` / `*MissingError`。

## 协调裁定的改名（ab2041456）

- `installer.ValueError` / `installer.TypeError` 别名删除，调用方改用 `installer.ValidationError` / `installer.RenderError`。
- `agents.ValueError` 改名为 `agents.ValidationError`（包括 `registry` 别名导入的 `client_tools.go`）。文案、状态码不变。
- 测试里和 Python 记录比对的类名字面量（`"ValueError"`、`"ValueError: "`）保持不变：那是 Python 记录里的数据。
- 后续（低，不在本次范围）：`health.ValueError`、`health/check.ValueError`、`agents/metrics.ValueError`
  （`RetentionValueError`）、`agents.PermissionError`，以及 installer 未导出的 `valueError()`/`typeError()` 辅助函数，
  仍然是 Python 风格的名字，可以在 T7 小清扫时一并改。

## 验证（服务器 .32 / .64）

- `rt -- bash -lc 'cd server && gofmt -l ./internal ./cmd; go vet ./... && go test -race -count=1 ./internal/installer/... ./internal/agents/... ./internal/api/agents/... ./internal/deploy/... ./internal/gateway/... ./internal/provider/... ./internal/jsondecode/... ./internal/repo/... ./internal/api/aggregations/... ./internal/api/subscriptions/...'`（.64）：gofmt 无输出，vet 通过，全部 ok。
- fuzz（.32，各 120 s，并行）：FuzzB41LegacyDifferential / FuzzBoundary / FuzzReviewNativeSemantics / FuzzSelectExactMembers 全部 PASS。
- `CONFIGFLOW_DIFF=1 go test -count=1 -p 6` provider、aggregations、subscriptions、api/agents、health、refresh、mosdns、rulelibrary、httpx 的 difftest（.32）：全部 ok。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-41 -p 8`（.32）：`PREFLIGHT PASS`（pyref 未失效，不需要重录）。
- `GATE_ONLY=proddiff server/scripts/gate.sh -p 6`（.32）：`baseline: new=0 stale=0`，self_check PASS，`GATE STAGE PASS: proddiff`。
