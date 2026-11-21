# O9 安全复核 · 第二轮（完整树）

审查人：fresh Opus（没有写过被审代码）。基线 `go@9a5bebf0a`（N8-11 W9 已合入），分支 `go-audit-o9r2`。
范围：`server/` 完整树，含 MCP、artifact 成功路径、Edge 静态文件，以及发行相关的 `docker/` Go 镜像。
测试只在 `.64` 用 `rt` 跑；没有连接生产，也没有写入真实主机名、IP 或 token。

## 1. 结论

| 级别 | 数量 | 本轮处理 |
|---|---|---|
| high | 0 | — |
| medium | 1 | 已修 + 回归测试（含负控） |
| low | 6 | 记录，交协调者排期 |

第一轮修复全部仍然有效（§3）。第一轮留到本轮的 MCP、artifact 两项没有发现新问题（§4）。

## 2. 发现表

| ID | 级别 | 位置 | 问题 | 可利用条件 | 影响 | 状态 / 建议 |
|---|---|---|---|---|---|---|
| O9R2-M1 | **medium**（继承） | `internal/agents/client.go` `HTTPClient.Do`；`internal/devhttp/devhttp.go` `Client.Do` | 没有显式 `MaxBody` 的 Agent 请求（`CallAgent`：部署、日志、区域检测、回程路由、HA、升级校验等）和全部路由器请求（RouterOS REST、iKuai），都会把响应体整个读进内存，没有上限。读超时只按单次读取算，不限总时长 | A4：被攻破或伪造的 Agent，或 Agent/路由器地址上实际应答的任意主机（Agent 地址跟随 renumbering 而改变）。管理员或后台任务对它发一次请求即可 | 返回无限长的响应体就能耗尽面板内存，导致进程被 OOM 杀掉，所有 profile 和设备的管理都会中断 | **已修**：Agent 响应默认上限 64 MiB（是现有最大显式上限 16 MiB 的 4 倍），超出时返回 `TransportError(ErrResponseTooLarge)`，调用方按连接失败处理，提示「Agent 响应超过 64 MiB 上限，已中止读取」；路由器响应上限 128 MiB，超出时返回 `ConnectionError`。正常响应远小于上限（路由器读取都经过过滤、分页或脚本分块），用户可见行为不变 |
| O9R2-L1 | low（继承） | `agents/client.go` `deadlineConn`、`devhttp` watchdog | 慢速持续发数据（每次读取都在超时内）的应答方可以无限期占住请求；`RemoteCall` 对非 GET 请求还会 `lifecycle.Detach`，客户端断开也不会取消 | 同 M1 | 占住 goroutine 和设备锁，不会耗尽内存（M1 已封顶） | 建议：`CallAgent` 和 devhttp 各加一个总时长上限（例如读超时 × 10，或固定 10 分钟），属于行为变化，需要协调者决定 |
| O9R2-L2 | low（继承） | `agents/deployment_remote.go:44` | `CallAgent` 不设 `NoRedirects`，会跟随 Agent 的 3xx。Go 跨主机重定向时会去掉 `Authorization`，所以 token 不会泄露；但面板会替 Agent 请求它指定的任意 URL，并把结果（经脱敏）返回给管理员 | 恶意 Agent | 有限的 SSRF，目标和结果都只对管理员可见 | 建议：与 monitor/gateway 调用保持一致，加 `NoRedirects: true`（需确认 Python 行为差异，记入 O12） |
| O9R2-L3 | low（继承） | `convert/client_nodes.go` `SurgeNode`/`LoonNode` | 手工节点的名称、密码等字段原样拼进 Surge/Loon 的 INI 文本；名称里含换行可以注入额外的段或规则 | 只有管理员录入或粘贴的节点（订阅节点经 Sub-Store，不走这里）；导入不可信分享链接时，名称里的 `%0A` 解码后会变成换行 | 改写该 profile 的客户端规则 | 建议：生成时把 `\r\n` 等行分隔符替换为空格（Python 同样不做；需要和 Python 对照，记入 O12） |
| O9R2-L4 | low（文档） | `backend/agents/go-agent/go.mod` `toolchain go1.26.10` | Agent 模块仍声明 1.26.10（GO-2026-6859，`govulncheck` 在 1.26.10 下可达）。发行镜像用 `golang:1.27.4`（主镜像）和 `1.26.11`（Dockerfile.agent）构建，不受影响；只有本地或 CI `setup-go go-version-file` 构建会选到 1.26.10 | 用旧工具链自行构建 Agent | 那份 Agent 二进制可被远程打崩 | 建议：Agent 模块 `toolchain` 升到 ≥ go1.26.11（Agent 不在 server/ 范围内，本轮没有改） |
| O9R2-L5 | low（已知、有意） | `internal/mcp/transport.go` `authenticate` | `config_token`（订阅链接令牌）就等于 MCP 管理员权限 | 持有订阅链接 | 可导出配置、改路由器 | Python `mcp_server/auth.py` 的文档写明这是维护者知情后的选择。保持不变；若以后订阅链接要外发，应另设 MCP 令牌 |
| O9R2-L6 | low（继承） | `internal/gateway/installation/service.go:431` `shellQuote` | 网关安装包里有一份私有的 shell 引号函数，而 `installer.ShlexQuote` 已经存在（违反"不复制共享 helper"规则）；两者对所有输入都是安全的单引号写法 | — | 无安全影响，属于维护性问题 | 建议在 S 轮次改用 `installer.ShlexQuote` |

## 3. 第一轮修复复核（仍然有效）

| 第一轮 | 现状 | 证据 |
|---|---|---|
| H1 CORS | `httpx/cors.go`：只有与请求自身 scheme/host/port 相同的 Origin 才回写 ACAO；Edge 用进程内 context 传原始来源，不信任 `X-Forwarded-*` | `o9_fixes_test.go`（gate 中） |
| H2 匿名注册 | 首次注册需要 JWT、注册密钥或一次性注册凭据；公开的 `/api/agents/register` 不暴露身份是否存在 | `TestO9AnonymousRefusalDoesNotRevealExistingIdentity` 等 |
| H3 工具链 | `server/go.mod` `toolchain go1.27.4`，`docker/Dockerfile` 用 `golang:1.27.4`，`Dockerfile.agent` 用 `1.26.11`；`govulncheck` 见 §6 | — |
| M1 后端端口 | `httpx.LimitRequestBody` 与 Edge 共用 1 MiB / 96 MiB 上限 | `bodylimit.go`、`cmd/configflow/o9_body_limit_test.go` |
| M2 解压炸弹 | rulefetch 每层解压和最终正文都限 64 MiB；substore `body_limit.go` 用 `LimitReader` | behaviour-changes「batch-8 O9-M2」 |
| M3 JWT 密钥 | 占位值或不足 32 字节时改用随机密钥，仅在开启登录时警告 | `auth/o9_secret_test.go` |
| L1/L3/L5/L6 | 访问日志脱敏；内部调用头常量时间比较；`nosniff`；未登录先 401 | `o9_fixes_test.go` |
| 全路由门禁 | `internal/server/o9_gate_test.go` 对全部已注册路由做匿名请求，要求一律 401（白名单除外）；这次新增的路由也自动覆盖 | gate |

## 4. 逐项结论（本轮新增范围）

- **MCP**：入口认证逐行对照 `backend/mcp_server/auth.py`：拒绝 rule-proxy 内部 token；config_token 比较用 sha256 + 常量时间（比 Python 严格）；开启登录时只接受有效 JWT；`system_config` 损坏时拒绝，不放行。工具调用经 `App.Invoke`，方法和路径来自固定注册表；内部调用头只在认证通过后由进程内设置，外部无法伪造（L3 已改为常量时间）。路径参数即使带 `../` 也只能到达其他 API 路由，MCP 本来就有这些权限，不构成越权。
- **artifact**：`preview_id` 先过正则；存储根目录和预览目录都拒绝符号链接；校验归属（`AgentID == "manual"`）、过期时间和 manifest 一致性；`file` 只返回 manifest 里列出的成员（`RelativePath` 规范化后必须与原值相同）；下载 Content-Type 是 `application/octet-stream` 或 `zip`，文件名来自引擎白名单；响应 `no-store`。
- **静态文件 / SPA 回退**：`resolve` 用 `path.Clean("/"+uri)` 后再 Join 到 Root，`try_files` 的三个分支都限制在 Root 内；`/api`、`/mcp` 判断用规范化路径，转给 App 的是原始路径（与 nginx 一致），门禁和路由器都不解析 `..`。`/_configflow/listener-health` 返回的是随机 nonce，只用来做监听自检，不是凭据。
- **RouterOS 脚本注入**：所有 `/execute` 脚本里的用户值都经过 `ScriptString`（转义 `\ " $ \n \r \t`）；DNS 防护 netwatch 脚本只拼入经 `netipx` 解析并回写规范形式的地址（`guardValidateSpec`）；地址列表批量写入的每一项都经过 `ScriptString`。
- **安装脚本 / shell**：`agent_name`、`service_unit` 只允许白名单字符；URL、路径、二进制名经 `shlexQuote`；Agent 配置 JSON 放在单引号 heredoc 里；Docker HA 引导的 Python 字面量先 JSON 编码，再转义 `\` 和 `'`。服务端 `exec` 只有 health runner 和 Sub-Store，都用 argv 形式，没有 `sh -c`。
- **YAML/JSON 生成**：Mihomo、OxiDNS、MosDNS 等配置都经编码器生成，没有字符串拼接 YAML；规则文本导出先按全部行分隔符拆分，并转义标点。Surge/Loon 的手工节点见 L3。
- **iKuai**：走 JSON API，值由 JSON 编码；逗号分隔的列表字段只来自管理员输入。
- **SSRF**：规则抓取的内部边界见 CFG-01 复核（DNS rebinding 仍是切换后项）；订阅经本机 Sub-Store；WebDAV 有 30 s 超时，重定向受控，列表限 16 MiB；面板没有出站通知（webhook/telegram）。Agent 重定向见 L2。
- **上传 / 导入 / 备份**：请求体经 Edge 和后端统一限长；部署包 tar 按成员大小和总量读取、只读白名单成员；rule-sync 快照限制压缩前后大小、成员数、重复名和类型，不解压到磁盘；WebDAV 备份文件名受正则约束；安装命令快照 ID 用 `crypto/rand` 生成。
- **日志**：抽查 `logger.*` 中带 err 的调用：WebDAV 经 `DescribeError` 只输出分类；Agent/路由器错误经 `redact.DiagnosticText`/`safeDeviceText`；没有发现直接输出 URL、token 的情况。
- **授权竞态**：H2 注册的发号和消费在同一个 `repo.Update` 写事务里（第一轮复核已证实）；本轮没有发现其他"先检查、后使用"的授权路径。

## 5. 修复与测试（O9R2-M1）

| 文件 | 内容 |
|---|---|
| `internal/agents/client.go` | `DefaultMaxBody = 64 MiB`；`ErrResponseTooLarge`（TransportError） |
| `internal/agents/messages.go` | `msgAgentResponseTooLarge` |
| `internal/devhttp/devhttp.go` | `MaxResponseBytes = 128 MiB`；`ErrResponseTooLarge`（ConnectionError） |
| `internal/agents/o9r2_body_limit_test.go` | 表驱动：正好等于上限时正常读完（负控）、超 1 字节（Content-Length / chunked）、无限长响应体 |
| `internal/devhttp/o9r2_body_limit_test.go` | 同上 |

修复前失败（临时还原两处产品代码，`.64`）：
```
--- FAIL: TestO9R2DefaultReplyLimit … err = <nil>, want TransportError(ErrResponseTooLarge)   ×3
--- FAIL: TestO9R2ReplyLimit … err = <nil>, want ConnectionError(ErrResponseTooLarge)        ×2
```
"正好等于上限"一例在修复前后都通过，证明上限没有误伤合法响应。命令与结果见 §6。

## 6. 运行记录

修复提交 `a47f5fa34`。

1. 依赖（`.64`，工具链 go1.27.4）：`govulncheck ./...` 和 `govulncheck -tags difftest ./...` → `No vulnerabilities found.`（两次）。
   Agent 模块：`GOTOOLCHAIN=go1.26.10 govulncheck ./...` → GO-2026-6859（net/http，在 1.26.11 修复）；`GOTOOLCHAIN=go1.26.11` → `No vulnerabilities found.`（见 L4）。
2. 修复前（`.64`，临时还原产品代码）：`go test -count=1 -run TestO9R2 ./internal/agents ./internal/devhttp` → 两个包都 FAIL（见 §5）。
3. 修复后（`.64`）：`go test -race -count=1 -run TestO9R2 ./internal/agents ./internal/devhttp` → `ok agents 3.763s`、`ok devhttp 5.386s`。
4. 定向 race（`.64`）：`gofmt -l .` 无输出，`go vet ./...` 通过；`go test -race -count=1 -p 6 ./internal/devhttp ./internal/routers ./internal/api/agents` → `ok 8.375s / 461.545s / 567.210s`。
   `./internal/agents` 在默认 10 分钟超时下超时（race 下这个包本来就慢）；按 gate 的 `-timeout=60m` 单独重跑：`ok configflow/internal/agents 778.717s`。
5. 受影响的 difftest（`.32`）：`CONFIGFLOW_DIFF=1 go test -count=1 ./internal/api/agents/difftest ./internal/agents/dnsfollow/difftest ./internal/agents/dnsha/difftest` → `ok 150.974s / 18.912s / 11.820s`。
6. 完整预检（`.32`，空闲）：`RT_HOST=sunbroke@192.168.0.32 rt sync` 后 `ssh … 'bash /tmp/preflight.sh .paseo__worktrees__302rx1dg__go-audit-o9r2 -p 8'` → 两组 vet 通过，`PREFLIGHT PASS`。

没有运行完整门禁 `gate.sh`（按简报，由协调者在批次树上跑）。
