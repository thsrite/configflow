# batch-55 复核：O17 1.13 `weburl` 换标准库

作者提交（rebase 到 `go` 后）：`fdc8947b2` 语料 + fuzz、`cf1f33523` 解码换 `net/url`、`5247e0343` pyref 源哈希、
`a28c78d4c` fuzz 幂等检查加保护。复核提交：`b4c79dc8e`（补边界用例）、本文件。

## 1. 对照语料是否真在旧代码上跑过

- `parity_test.go` 在 `fdc8947b2`（旧实现）之后没有再改过；我另外把 `weburl.go`、`logsafe.go` 换回 `go` 版本，
  在 .32 上跑了当前全部 `TestO17*`：作者的 75 条语料全部通过，确认期望值来自旧实现。
- 自己补了约 80 个输入（`%u00e9`、`%2B` 与 `+` 在 path/query、`%2f`/`%2F`、空串、只有 `%`、超长（30 万字节）、
  裸/转义的非 UTF-8 字节、userinfo 里多个 `@` 和 `:`、IPv6 zone、相对 URL 等），新旧实现分别导出
  `Quote/Unquote/UnquotePlus/EncodeQuery/Prepare(+RequestURI)/Parse/SafeForLog` 后逐行 diff：
  **`Prepare`、`Parse`、`Quote`、`Unquote`、`UnquotePlus`、`EncodeQuery` 零差异**；`SafeForLog` 有 1 类差异（见下）。
  代表性用例固化在 `review_batch55_test.go`，Prepare/Unquote 部分在旧实现上同样通过。

## 2. 发现

| 级别 | 发现 | 处理 |
|---|---|---|
| 低 | 作者称「0 差异」只覆盖了 `Unquote`（FuzzUnquoteParity）；`decodeLayers` 换 `url.PathUnescape` 后，**不含 `%` 的裸非 UTF-8 字节**在 `SafeForLog` 里从 `%FF` 变成 `%EF%BF%BD`（旧 `Unquote` 无 `%` 时提前返回，跳过 `ToValidUTF8`）。只影响日志渲染；JSON/HTTP 输入本身是合法 UTF-8，实际碰不到。新行为与 `%ff` 输入一致，更合理。 | 保留新行为，`TestO17ReviewSafeForLogInvalidUTF8` 固化 |
| 低 | `a28c78d4c` 把 Prepare 幂等 fuzz 改成「`url.Parse` 能解析时才查」。反例是 IPv6 zone 带非 ASCII（`//[::%\xd7]` → `//[::%25%D7]`，`net/url` 拒绝 zone 里的 `%D7`）。这种 URL 无法被请求，放宽可接受。 | 不改，记录 |
| — | 实际发出的请求 URL：`Prepare` 只改了常量名（`uriChars`→`pathChars`），逻辑未动；对照零差异。 | — |

无高/中问题。

## 3. 1.13 目标是否达成、还剩哪些手写转义

- 已是标准库：`Quote`（`url.QueryEscape` + `+`→`%20`）、`EncodeQuery`（`url.QueryEscape`，保序故不用 `url.Values`）、
  `Unquote` 主路径与 `decodeLayers`（`url.PathUnescape`/`QueryUnescape`）、`Prepare` 的点段解析（`ResolveReference`）。
- 剩下的手写部分及不能换的理由：
  - `escapeExcept`（Prepare 的 RawPath/RawQuery、Parse 的 userinfo）：`net/url` 没有「只转义非法字节、保留已有转义」的函数。
    清空 `RawPath` 让 `EscapedPath()` 重算会把 `%2F` 变成 `/`、`%7e` 变成 `~`；`url.Values` 重编码会重排键、改 `+`/`%20`/`%2F`。
    这正是 1.13 允许保留的「已转义的不重复转义」，约 15 行。
  - `escapeStrayPercent`：WHATWG「不成转义的 `%` 代表自己」的产品规则（Parse 需要）；`Unquote` 的容错回退复用它，
    标准库没有不失败的解码。
  - `queryChars` 不含 `[]` 符合 RFC 3986（pchar 不含 gen-delims）；`pathChars` 含 `[]` 与 `net/url` 的 RawPath 校验一致。
  - 调用方 `subs.ParseQueryOptions` 用 `UnquotePlus` 而不是 `url.ParseQuery`：后者遇坏转义整对丢弃，订阅源有坏转义，需容错。
- 结论：1.13 目标达成，照搬 Python 的规则已去掉（注释也不再引用 requests），没有更多可安全替换的手写转义。

## 4. 验证（.32，rebase 到 `go` 之后）

- `gofmt -l .` 无输出，`go vet ./...` 通过。
- `go test -race` weburl 及调用方（agents/monitor、convert、gateway、httpx、localhttp、mosdns、redact、rulefetch、subs、substore）全部 ok
  （convert 首轮在 loadavg 20 下撞默认 10 分钟超时，`-timeout 40m` 重跑 ok 686s）。
- fuzz 各 60 s：`FuzzUnquoteParity`、`FuzzParseAndPrepare`、`FuzzSafeForLog` 均 PASS。
- `PYREF_MODE=check go test ./internal/installer ./internal/mosdns ./internal/oxidns ./internal/routersplit` 全 ok（rebase 无冲突，无需重录）。
- `GATE_ONLY=difftest scripts/gate.sh -p 6`：`DIFF PASS: 214 required entries ran and passed; 0 test skips`。
- `GATE_ONLY=proddiff scripts/gate.sh -p 6`：`baseline: new=0 stale=0`，self_check PASS。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-55 -p 6`：`PREFLIGHT PASS`。
