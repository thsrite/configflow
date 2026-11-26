# MCP #39 Opus HR 复核

范围：`205c116b0`（transport、auth、invoker、53 个工具适配）和 `02a39c5a8`（凭据容器补强）。
复核者没有参与移植。分支 `go-opus-review-mcp`，基于 `go-port-mcp@00f5a36a4`。
对照 Python `backend/mcp_server/{server,auth,invoker,tools}.py`、`backend/common/auth.py`、
`backend/common/config_export.py`。按 Leaner verification：读了作者的测试，没有重跑作者的完整比对套件，
只给可疑点写定向测试。

**结论：high 0，medium 0，low 2（已修），另记 4 条 low 级说明（不修）。状态改为 verified。**

## 发现

| # | 级别 | 问题 | 修复 | 回归（修复前失败） |
|---|---|---|---|---|
| 1 | low | `publicText` 用 `json.Marshal` 生成工具文本，`&`、`<`、`>` 被转义成 `&`/`<`/`>`。Python `json.dumps(ensure_ascii=False)` 原样输出。订阅 URL 里的 `&` 会以转义形式交给模型。作者的 difftest 会先把文本里的 JSON 解析出来再比较，所以没有发现这个差异 | 改用共享的 `jsonresp.EncodeUTF8`（不转义 HTML，U+2028/2029 保持原样），缩进方式不变 | `TestTextContentKeepsLiteralHTMLCharacters`（对象、列表） |
| 2 | low | 工具业务失败时，Go 不记日志。Python 会记一条 warning：`MCP 工具 {name} 调用失败: {message}` | 补上 `logger.Warningf(logToolFailed, …)`，常量放在 `messages.go`。消息先经过 `redact.RedactSecretsInText`，rule-proxy 和 Agent 内部令牌不会写进日志（Python 原本不做脱敏，这里更严格） | `TestToolFailureIsLoggedWithoutInternalToken` |

不修的 low 级说明（都是 fail-closed 或边缘输入，没有放宽权限）：

- L3：`system_config` 整个容器不是对象时，Go 拒绝所有 MCP 请求，包括开启登录时带有效 JWT 的请求。交接说明只写了“损坏的 config_token 仍接受 JWT”。这样做是保守的，可以接受，在这里补记。
- L4：`runTool` 在入口对所有工具的 `id` 都做 `pathPart` 检查，包括 `import_client_config` 这种 `id` 只出现在请求体里的工具。含 `/` 的 id 会返回“id 不能包含路径分隔符”，不会交给 owner 校验。合法的 profile id 本来就不能含 `/`，不影响正常功能。
- L5：`manage_rule` 先 create 再移到 bottom。如果两步之间发生 Shutdown，规则已经创建，但 MCP 返回 -32603。Python 没有关机语义，这属于 Runtime 新增的边界。没有写入不一致：create 已经提交，reorder 没有执行。
- L6：`method` 不是字符串（例如 `5`）时，Go 返回 -32600；Python 返回 -32601 “不支持的方法: 5”。`params` 为 `[]` 时，Go 返回 -32603；Python 会当作空对象处理，返回 -32602。这些都是非法 RPC 输入，属于作者登记的 DTO 严格化差异。

## 核对过没问题的项

**认证**
- token 的来源和 Python 相同：严格的 Bearer 解析（只接受 `Bearer `、可打印 ASCII、≤8192 字符）；query 里的 `token` 超长时视为空；开启登录时才接受 JWT；两种凭据都没配置时放行。
- 判断顺序和 Python 相同：先拒绝当前和退休的 rule-proxy token，再比较配置令牌，最后检查 JWT。配置令牌和 rule-proxy token 相同的旧库会被拒绝（作者的测试表里有 `"synthetic-internal"` 这一行）。
- 配置令牌用 SHA-256 后再 `subtle.ConstantTimeCompare` 比较，是常量时间。Python 用的是 `==`，Go 更严格。rule-proxy 的查找用 map，但这是拒绝分支，不会泄露可用于授权的信息。
- 损坏的凭据：`config_token` 为 true、数字、对象或数组，或 `system_config` 不是对象时，Go 都拒绝，不会变成匿名访问，数字也不会被当成新的 token。`model.Marshal` 对改过的字段输出新值，对没改过的字段输出存储原文，所以令牌轮换后旧令牌立即失效。
- GET/DELETE 返回 405，不做认证，和 Python 相同（Python 的 `unsupported_transport` 也不认证）。`/mcp` 和 `/mcp/` 在全局 Gate 中是公开路径，和 Python 的 `public_endpoints`/`public_prefixes` 一致。

**invoker 和 App.Invoke 的边界**
- 每次内部调用新建一个 `http.Request`：固定 `localhost`、`127.0.0.1`，只带 `X-ConfigFlow-Profile`、`Content-Type` 和进程内随机的内部能力头，不转发外部请求的 header、method 或 URL。内部头只放行 `Gate`、`Require`、`legacy` 和 MosDNS 的 rule-proxy 认证，和 Python `is_internal_call()` 的使用点一一对应。
- `ServeHTTP` 每次都新建 `repo.WithScope`。外层请求没有在 ctx 里留下持有的锁、读快照或事务，所以不会混用请求上下文。业务校验、事务和锁都在 REST owner 里。
- 路由段：`pathPart` 拒绝 `/`、`\`、`.`、`..`，`EscapedPath` 会转义 `?`、`#`、`%`，id 不能改写目标路由。profile 作用域的备份路径同样经过 `pathPart` 检查。
- 不自动跟随重定向。二进制响应、空响应、文本响应和 ≥400 时的 message/error/msg 提取都和 Python 一致。
- 53 个工具逐个对照了 Python 的路径、必填项、默认值、读后写合并（health_check 的 false 会保留、proxy-group 直接提交 partial、backup 原样回传 `******`、OxiDNS 深合并）、`profile_id` 补齐和 `generate_config` 不回显配置内容。另有 94 条 Python 适配参考在作者的测试里。

**transport**
- 显式 405、空批量请求返回 400、非对象或非数组返回 400、坏 JSON 返回 -32700/400、通知不执行（批量里带写操作的通知也不执行）、未知方法返回 -32601、未知工具或参数非法返回 -32602、内部异常统一返回固定中文、处理过程中的 panic 会被 recover。
- `"id": null` 当作请求处理并应答，不当作通知（新增 `TestNullIDRequestIsAnswered`，修复前后都通过，属于正控）。
- 请求体上限沿用 edge 的 1 MiB（与 nginx 相同）。超长的 chunked 请求体会让 Bind 失败，返回 400，和其他 `/api` 路由的行为相同。批量请求条数没有上限，和 Python 相同，而且必须先通过认证。

**敏感信息**
- 工具结果和错误文本都经过共享的 `redact.SanitizeExternalPayload`。它会删除 `rule_proxy_token`、`retired_rule_proxy_tokens`、`rule_fetch_proxy`，并把含有 rule-proxy 或 Agent token 的字符串替换成 `[REDACTED]`。
- “配置令牌由管理员可读”确实是 Python 原有的约定：`SENSITIVE_SYSTEM_CONFIG_FIELDS` 里没有 `config_token`，`get_settings` 工具的说明里明确列出了 `config_token` 分区，`auth.py` 的文档字符串也说明持有订阅链接就等同于管理员。
- 内部异常不返回 `str(exc)`（Python 会返回），日志只写固定消息。Go 在这一点上更严格。

**关机**
- 每个工具只 Detach 一次：Detach 会去掉请求取消，但保留所属 generation 的关机取消。每次内部调用前都检查 `ctx.Err()`，owner 在最终提交点还会再检查一次。作者用真实 Runtime 写了“读完、写之前 Shutdown 不落盘”和“请求断开后写入仍完成”两条回归（`TestToolWriteUsesOriginalRuntime`）。我读过代码确认语义正确，没有重跑。

**作者登记的 fail-closed 差异**，逐条判断：
1. 非字符串的 config_token 一律拒绝：合理。Python 会把 0、false、空容器当作“未配置”并匿名放行，这正是需要关上的口子。
2. arguments、params 和工具基本字段按 DTO 严格校验，错误类型返回工具 400 或 -32602：合理。只影响非法输入，错误消息是中文常量。
3. 拒绝 NaN/Infinity、内部异常用固定文本、id 里的路径分隔符被拒绝、深度上限 128 层：合理。
4. 文本内容按原生 JSON 编码：语义上合理，但 HTML 转义差异已按发现 1 修正。

## 留到切换后或 O9 第二轮再看

- 用真实 MCP 客户端（例如 Claude Desktop 或 Inspector）走一遍 initialize、tools/list、tools/call，确认文本内容的可读性。
- 真实设备上的 Agent 动作（push_config、upgrade、uninstall）只在 owner 层验收过。MCP 层只验证了请求序列，没有验证设备端结果。
- 带 Shutdown 的多步工具（create 后 reorder、读后写）会出现部分完成后报 -32603 的情况（L5）。如果维护者希望改成返回“已创建但未移动”的业务结果，需要另开任务。

## 验证命令

```sh
# 修复前：两条新回归失败（transport.go 临时恢复为 HEAD 版本）
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -count=1 -run "TestToolFailureIsLoggedWithoutInternalToken|TestTextContentKeepsLiteralHTMLCharacters" ./internal/mcp'
# --- FAIL: TestTextContentKeepsLiteralHTMLCharacters / --- FAIL: TestToolFailureIsLoggedWithoutInternalToken

# 修复后
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -e CONFIGFLOW_DIFF=1 -- bash -lc 'set -e; cd server; test -z "$(gofmt -l internal/mcp)"; go vet -p 4 ./internal/mcp/... ./internal/server; go test -p 4 -race -count=1 -timeout=10m ./internal/mcp/... ./internal/server'
# ok configflow/internal/mcp 7.768s
# ok configflow/internal/mcp/difftest 5.868s
# ok configflow/internal/server 8.929s
```
