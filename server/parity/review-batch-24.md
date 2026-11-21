# batch-24 独立复核（2026-11-22）

范围：`go@bc8961f9f` 之上的 O18 小清扫 7 个提交（T5、T6、T7、C15a、C15b、C15c、C17）和实验网分支 `go-audit-lab` 的
14 个提交（D1 `678192f9e` 恢复受理返回 202 + L2–L5 实验报告）。复核修复提交：`3e65b75fa`。

## 冲突解决

- `internal/api/agents/deploy_test.go`：两边都保留（M16 的 EasyTier 内核版本测试与 D1 的
  `TestDeployHTTPAcceptedRecoveryIsAccepted`），无冲突标记、无重复函数；`go vet ./...` 通过，`internal/api/agents`
  `-race` 通过。
- `parity/behaviour-changes.md`：O18 各节与「L2 实验网 — 重新恢复原配置受理后的状态码」都在，顺序正常。

## 发现

### 中

- **C15c：POST 路由返回的文件也进了 `http.ServeContent`。** `ServeContent` 不区分方法：生成配置的 POST 下载
  （`generate.go`，按 `Path` 发送时带 ETag）收到 `If-None-Match: *` 或不匹配的 `If-Match` 时返回 412（无正文），带
  `Range` 时返回 206 片段。原来（以及 Python）只对 GET/HEAD 做区间和条件处理。浏览器正常使用不会带这些头，但这是
  不应有的放宽面，且与 RFC 9110 不符。已修（`3e65b75fa`）：只有 GET/HEAD 的 200 文件响应走 `ServeContent`，其余整份
  发送且不带 `Accept-Ranges`；`behaviour-changes.md` C15c 节中「POST 下载也带 `Accept-Ranges`」一条改为此说明。
  负控：去掉判断后 `TestFilePOSTIgnoresPreconditions`（412）与 `TestFileRangeHeaders` 的 POST+Range 行（206）失败。

### 低（记录，不修）

- C17：`exp` 带小数时现在按精确值比较，原来 `int()` 截断，所以带小数的 `exp` 最多晚不到 1 秒过期；`iat`/`nbf`
  反而更严。只有用服务器密钥签发的令牌才能带这类声明，面板自己签发的都是整数，可忽略。
- C15c：`writeFile` 对非 200 / 非 GET 的路径仍按 `File()` 时 stat 得到的大小写 `Content-Length`，文件在两次之间被
  改写时长度可能不符（与改动前相同，生成文件是原子替换，不影响实际使用）。

## 逐项结论

- **C17 JWT**：alg 检查（缺失 → 拒绝、非 `"HS256"` 字符串 → 拒绝）、HMAC 常量时间比较都在声明检查之前，未改动。
  `numericDate` 只接受 JSON 数字：字符串、`true`、`null`、数组、对象都是 401 `token_invalid`（原来部分是 500 或按
  `int()` 接受）；`1e400`/`-1e400` 溢出 → `Float64` 报错 → 拒绝；负数 `exp` → 过期；超大整数按 float 比较（远期，
  正确）；Go JSON 不接受 NaN/Infinity，载荷解析即失败；声明名区分大小写（`EXP` 进 Extra，不当作 `exp`），缺失则不检查
  （与 PyJWT 默认一致）。结论：除上面的小数 `exp` 外只会更严或相同。新增 `TestNumericDateClaimsReview` 覆盖以上各项。
- **C15a 公开路径**：网关与路由用同一个已解码的 `req.URL.Path`；路由不解析 `..`，`//` 先重定向，字面段区分大小写。
  `%2F` 解码成 `/` 后被 `[^/]+` 拦下（更严）；`..` 只能落在公开路由自己的参数里（如 `/api/config/x/loon/..` 仍到 Loon
  公开处理器）。`\z` 只收紧末尾换行。新增 `TestGateHasNoPublicPathBypass`（公开前缀 + `..`、`%2E%2E`、`%2F`、`//`、
  大小写、尾斜杠、`%0A`，带 `<path:>` 的私有路由）和白名单单元用例，均需登录。
- **C15b Host**：`Host()` 仍用 Werkzeug 主机正则整串匹配（字符集只有字母数字、`.-:[]`，`$` 在 RE2 中即文本末尾），
  非法字符返回空；多个 Host 头由 net/http 400；scheme 只看 TLS，不读 `X-Forwarded-*`。去掉小写化只影响大小写，
  安装命令里拼接的地址字符集不变（方括号原来就会出现）。无注入面变化。
- **C15c 其余**：Content-Type、Content-Disposition、Cache-Control/Expires、ETag/Last-Modified 都由 `File()` 先设好，
  `ServeContent` 看到已有 Content-Type 不再嗅探；认证都在处理器里（`auth.Require` / 网关）先于写响应；HEAD 无正文；
  文件按句柄流式发送（`Path` 方式），内存数据本来就在内存；304 去掉 Content-Type/Length（Content-Disposition 保留，无害）。
- **T5 time.Parse**：RFC3339（含 `Z`、可选小数秒 `.`/`,`）、秒级偏移、无时区日期时间、纯日期；无时区值按 UTC 墙钟
  返回并报告 naive，与原 `FromISOFormat` + 探测一致；偏移 ≥ 1 天拒绝；`parseStamp`/`parseTimeKeepingOffset` 现在
  直接保留解析出的偏移，等价于原来的重建。Go 小时字段接受 1 位数（只放宽了畸形输入），快照中不存在。
- **T6/T7**：按 JSON 值比较的差分保留，未发现问题（未重跑作者全套，按 Leaner verification 抽查）。
- **D1**：202 只在 `recover` 走到最后一步——Agent 返回非 4xx/5xx、记录为 `rolling_back`——时出现；前置拒绝
  （404/409）、Agent 404/405/400/409（恢复为 `rollback_failed` 并带 http_status）、传输错误和 5xx（`unknown`）都不变；
  有 http_status 的结果不会被改写（只改 `Status == 500` 且 `Err == nil`）。

## 验证（测试服务器 192.168.0.64）

- `gofmt -l` 无输出，`go vet ./...` 通过。
- `go test -race -count=1 ./internal/httpx/... ./internal/auth/... ./internal/api/agents/ ./internal/taskstatus/...`：全部 ok。
- `go test -count=1 -tags configflow_difftest ./internal/api/agents/difftest/ ./internal/api/generate/... ./internal/httpx/difftest/`：ok
  （agents/difftest 0.1 s，Flask 对照在此环境下多半跳过，由批次门禁完整执行）。
- `/tmp/preflight.sh .paseo__worktrees__302rx1dg__batch-24 -p 8`：vet 两组标签通过，`PREFLIGHT PASS`。
