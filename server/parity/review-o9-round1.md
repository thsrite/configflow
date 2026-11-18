# O9 安全复核 · 第一轮

审查人：fresh Opus（未移植过这些代码）。基线 `go@95440b69e`，分支 `go-opus-o9-security`。
范围：`server/` 产品代码；MCP（未注册）与 artifact 成功路径（固定 503）不审，列入第二轮。
batch-7（`6c6b46ff5`）只读参考，见 §5。

## 1. 威胁模型

| 攻击者 | 能力 | 关心的后果 |
|---|---|---|
| A1 局域网无凭据者 | 能连面板 Web 端口（默认 80→5010）；host 网络模式下还能连后端端口 5001 | 绕过登录、读配置/凭据、写路由器、打崩进程 |
| A2 普通凭据持有者 | 有 `config_token`（客户端订阅链接）、规则代理 token、某个 Agent 的 token | 越权读其他 profile/全局数据、写操作 |
| A3 恶意外部源 | 订阅、规则源、Sub-Store、版本检查的响应可控 | 内存耗尽、解析炸弹、重定向进内网、注入生成配置 |
| A4 恶意设备回包 | Agent / RouterOS / iKuai 的响应可控 | 注入后续脚本、内存耗尽、凭据泄露到日志 |
| A5 恶意网页（新增） | 局域网用户浏览器里的任意网页 | 跨站读写面板 API（CORS） |

面板登录可选；**默认安装不开登录**，此时 A1 天然拥有全部 API。本轮按“开了登录”与“未开登录”两种部署分别判断。

## 2. 发现表

严重度按“开了登录的部署”评，未开登录时 H1 的影响更大（见正文）。“继承”= Python 版同样存在。

| ID | 严重度 | 位置 | 问题 | 复现 | 状态 / 建议 | 负责模块 |
|---|---|---|---|---|---|---|
| O9-H1 | **high**（继承） | `internal/httpx/cors.go` | Flask-CORS 默认：任意 `Origin` 原样反射，预检放行所有方法和头。未开登录时，局域网用户打开的任意网页可跨站**读写**整个 API（导出配置、改路由器）；开登录时只能碰公开入口 | `go test -tags o9repro -run TestO9ReproCORSReflectsForeignOrigin ./internal/httpx`（失败）；现有 `httpx_test.go:323` 也证明 | **未修，需维护者决定**（会改变 Flask↔Go 差分里的 CORS 头）。建议：只在 `Origin` 与请求自身 scheme+host 一致时回写 ACAO，其余不加 CORS 头；同源前端不受影响；记入 O12 行为变更 | httpx（C05） |
| O9-H2 | **high**（继承） | `api/agents` `register_agent` + `get_agent_config` | 开了登录但未设 `AGENT_REGISTRATION_KEY`（默认）时，无凭据者可 `POST /api/agents/register` 得到新 Agent 的 token，再 `GET /api/agents/<id>/config?token=` 拿到默认 profile 的完整生成配置（节点、`secret` 等）。登录形同虚设 | `go test -tags o9repro -run TestO9Repro ./internal/api/agents`（失败：返回了 `secret: o9-synthetic-controller-secret`） | **未修，需维护者决定**（会影响现有安装脚本/compose 流程）。建议：开了登录时，首次注册必须带注册密钥、网关安装能力或面板 JWT；安装命令由登录用户生成，可内嵌短期一次性注册能力（与 `gateway_installation` 同模式）；未开登录时保持现状 | agents（A01/A03）+ installer |
| O9-H3 | **high** → **已修** | 工具链（Go 1.26.10） | GO-2026-6859：使用 `http.MaxBytesReader` 的 HTTP/1 服务端可被远程打崩。Edge 对所有 `/api` 用 MaxBytesReader，A1 无凭据可触发 | `GOTOOLCHAIN=go1.26.10 govulncheck ./...` 报 1 个可达漏洞；守卫测试在 1.26.10 下失败 | **已修**：`server/go.mod` 加 `toolchain go1.26.11`；`docker/Dockerfile`、`docker/Dockerfile.agent` 的 golang 镜像 1.26.10→1.26.11；`TestO9ToolchainHasMaxBytesReaderFix` 防止用旧工具链构建。O14 发行镜像构建 Go 服务端时必须 ≥1.26.11 | 发行（O14） |
| O9-M1 | medium（Go 回退） | `cmd/configflow/service.go` 后端监听；`httpx/ctx.go:289` | 后端端口（`CONFIGFLOW_BACKEND_HOST` 默认 `0.0.0.0:5001`）直接服务 App，没有 Edge 的 1 MiB/96 MiB 上限；`Ctx` 把请求体整体读入内存。host 网络部署下 A1 可对公开入口（如 `/api/auth/login`）发 GB 级请求体耗尽内存。Python 的 waitress 上限 1 GiB 且溢出到磁盘 | `TestO9ReproBackendPortHasNoBodyLimit`（8 MiB 登录体被解析，401 而不是 413） | 建议：后端 listener 外包一层与 Edge 相同的按路径上限（1 MiB / oxidns 96 MiB），或默认只监听 127.0.0.1（docker-compose 注释已建议） | server / httpx |
| O9-M2 | medium（继承） | `rulefetch/transport.go` `Content()`、`cache.go:229/280`；`substore/httpclient.go:442/549` | 规则下载（刷新、`/api/rules/local/<name>` 触发的 2 s 刷新）不设解压后大小上限，`Content-Encoding: gzip` 炸弹直接展开到内存；`ReadTimeout` 是单次读超时，不限总时长。A3 可耗尽面板内存；`/api/rules/local/*` 是公开入口，A1 可反复触发已配置源的下载 | `TestO9ReproRuleDownloadHasNoDecodedSizeLimit`（195 KB→192 MiB 全进内存） | 建议：`RefreshRemoteRule` 和规则缓存下载统一传 `MaxBytes`（如 64 MiB，与部署文件上限同级），解码层计数解压后字节；Sub-Store 客户端同样加上限 | rulefetch（C14）、substore |
| O9-M3 | medium（继承） | `auth.FromEnv` | `JWT_SECRET_KEY` 未校验：docker-compose 示例里的占位值 `your-unique-secret-key-at-least-32-chars` 或短密钥会被直接使用，照抄示例的部署任何人都能签发有效登录 token | 代码阅读（`auth/auth.go` FromEnv） | 建议：拒绝已知占位值并对 <32 字节的密钥启动时报错或改用随机密钥加警告（行为变更，记 O12） | auth |
| O9-L1 | low（继承） | `httpx/edge.go` `logAccess` | 4xx/5xx 访问日志写完整 `RequestURI`，含 `?token=`（config_token、Agent token）。nginx 原样如此 | 代码阅读 | 建议：日志里用 `weburl.SafeForLog` 脱敏查询串 | httpx |
| O9-L2 | low（继承） | `auth.Gate`、登录 | 登录无失败次数限制；JWT 不可吊销（改密码后旧 token 在 `JWT_SECRET_KEY` 不变时仍有效 24 h） | — | 建议：按来源 IP 的简单退避；改凭据时轮换签名密钥 | auth |
| O9-L3 | low（继承） | `mosdns/ruleproxy.go:124`、`httpx/ctx.go:482` | 规则代理 token / config token 比较、内部调用头比较不是常量时间（`auth.ValidateTokenOrJWT` 已是常量时间） | — | 建议统一用 `subtle.ConstantTimeCompare` | mosdns、httpx |
| O9-L4 | low（继承） | `api/agents/agents.go:207` | 注册时 `X-Forwarded-For` 第一段取作客户端 IP，可伪造；但请求体本可直接给 `host`，不增加能力 | — | 记录即可 | agents |
| O9-L5 | low（继承） | Edge 静态文件、`/api/rules/local/*` | 无 `X-Content-Type-Options: nosniff`。公开规则内容以 `text/plain` 返回，主流浏览器不会把它当 HTML，风险很低 | — | 可在 Edge 和文本下载统一加 `nosniff` | httpx |

## 3. 逐项结论

### 3.1 认证、会话、CSRF

- **门禁与白名单**：`auth.Gate` 与 `backend/routes/auth.py setup_before_request` 逐条比对：public exact 路径、6 个前缀、4 条 profile 正则（Go 用 `\n?\z` 复刻 `re.match` 的 `$`）、自认证 endpoint 集合完全一致。Go 端 endpoint 名称逐个核对注册处：`agents.download_agent_binary`、`download_install_command`、`docker_oxidns_ha_enroll`、`agent_heartbeat`、`agent_traffic_report`、`agent_traffic_history_report`、`get_agent_config`、`get_agent_upgrade`、`report_agent_upgrade`、两组 linux gateway provider 名称都与 Python 相同；`gateway_installation_script/enroll` 在 batch-7 中注册且名称一致。名称写错只会“失败关闭”（需要 JWT），不会放开。
- **点段绕过**：Edge 按规范化路径选 location，但把原始路径交给 App（与 nginx `proxy_pass` 无 URI 时一致）；门禁按原始路径判断公开前缀，路由器不解析 `..`，所以 `/assets/../api/x`、`/api/rules/local/../../x` 只会 404。新增 `TestO9GateDotSegmentsAfterPublicPrefixStayProtected`（经 Edge 和直连后端两条路径，含 `<path:>` 路由）通过。
- **JWT**：只接受 HS256，HMAC 常量时间比较，exp/nbf/iat 检查同 PyJWT；无 exp 的 token 会被接受（与 Python 同，只有服务端能签）。未设 `JWT_SECRET_KEY` 时每进程随机。占位密钥问题见 O9-M3。
- **Agent 凭据**：心跳/流量 `authenticatedAgent`、升级 `agentTokenMatches`、配置拉取 `GetAgentByToken` 都校验 token 与 agent_id 对应；比较为常量时间。注册见 O9-H2。
- **客户端配置入口**：`/api/config/*`、profile 订阅/聚合 provider 都调用 `auth.ValidateTokenOrJWT`，拒绝规则代理 token 冒充，比较为常量时间（比 Python 更严）。`/api/rules/local/*`、`/api/rule-library/content/*` 按 Python 设计公开。
- **内部调用**：`X-ConfigFlow-Internal` 为每进程 32 字节随机值，只由 `App.Invoke`（MCP）设置；`localhttp.Do` 不带该头，回调仍走门禁。外部不可伪造。
- **CSRF**：认证用 `Authorization: Bearer`，不用 cookie，开登录时没有传统 CSRF；未开登录时的跨站读写由 CORS 放大，见 O9-H1。

### 3.2 静态文件与下载

- Edge `resolve` 用 `path.Clean("/"+uri)` 后再 `filepath.Join(Root, …)`，`TestO9EdgeStaticNeverLeavesRoot`（`../`、`%2e%2e`、`%2f`、`%5c` 共 7 种）通过。符号链接只可能来自镜像内的前端构建目录，不受请求控制。
- Content-Type 用 nginx 映射表，默认 `application/octet-stream`；缓存头与 nginx.conf 一致（哈希资源 immutable、其余 no-cache、版本文件 no-store）。
- Agent 二进制下载是 3 个文件名白名单；部署包/预览 zip、tar 都由面板自己生成，读取时按成员大小和总量限长（`deploy/bundle.go`、`agents/rulesync/snapshot.go`）。WebDAV 备份文件名受 `config_backup_YYYYMMDD_HHMMSS.json` 正则约束，WebDAV 列表 XML 限 16 MiB。
- 备份导入走 JSON 存储层，不按条目写文件；规则缓存文件名经 `SanitizeRuleName`/哈希，不会穿越。

### 3.3 注入（脚本、单元、配置）

- **RouterOS**：控制台脚本里的用户值统一经 `routers.ScriptString`（转义 `\ " $ \n \r \t`）；键名来自固定表；DNS 防护的 netwatch 脚本只拼入经 `netip` 解析过的地址。未找到可执行注入点。
- **安装脚本**：`agent_name`/`service_unit` 白名单字符（`matchesClass`）；URL、token、校验和、版本等经 `shlexQuote`；Agent 配置 JSON 以 `EnsureASCII` 编码后放进单引号 heredoc，无法提前结束 heredoc。自定义 start/stop/status 命令是登录管理员有意写入的命令，属功能本身。
- keepalived/nft/systemd 片段：本轮抽查 installer 模板里出现的变量均为固定值、端口数字或上面两类已转义值；HA 部分（batch-7 A18）在 §5 补审。

### 3.4 外部请求

- 订阅、规则、Sub-Store、版本检查**按设计**可访问任意主机（含内网），没有私网过滤；内部回调边界与重定向由 CFG-01 修复后的 `prepareRuleTarget` 控制，现状与 `review-cfg-c14-security.md` 一致：**DNS rebinding / 主机别名仍是切换后项（LOW-1/LOW-2），未变化**。
- 超时：规则下载有连接和单次读超时；Agent 客户端、版本检查（`coreVersionMaxBodyBytes`）、部署素材（`MaxFileBytes`）有响应体上限；规则刷新和 Sub-Store 没有，见 O9-M2。

### 3.5 解析炸弹

- YAML：`yamlx.Parse`/`Unmarshal` 拒绝别名放大（yaml.v3 的预算 + `aliasBudget`），新增 `TestO9ParseRejectsAliasAmplification`（9^9 展开）通过且在秒级内拒绝。
- JSON：Edge 限 1 MiB（OxiDNS 96 MiB）；后端端口无上限见 O9-M1。
- 正则：用户正则只有 `rules/regex.go` 用 `regexp2`（回溯），已设 `MatchTimeout`；其余均为 Go RE2，线性时间。
- 压缩：规则下载 gzip/deflate 无上限见 O9-M2；面板自产的 tar/zip 有成员与总量上限。

### 3.6 敏感信息

- 日志：抽查 `logger.*f` 里出现 URL/token 的调用均经 `weburl.SafeForLog`/`SafeErrorDetails`；注册日志在记录前移除 `registration_key`。例外是 Edge 访问日志（O9-L1）。`App.internalError` 记录 `err.Error()`，错误文本里的 URL 由各模块的 `Safe*` 包装负责，前几轮 R06/R07/A13 已覆盖，本轮未发现新的直出。
- 响应：`httpx/sanitize.go` 的 token 端点豁免与 Python 一致；Agent 重注册不回 token（已有测试）。

### 3.7 依赖

`govulncheck ./...`（`.32`）：
- 服务器工具链 go1.27.3：No vulnerabilities found。
- `GOTOOLCHAIN=go1.26.10`（原 Docker 镜像版本）：**GO-2026-6859**（net/http，fixed in 1.26.11），调用链经 Edge → 已修，见 O9-H3。
- `GOTOOLCHAIN=go1.26.11`：No vulnerabilities found。第三方模块（regexp2、uuid、klauspost/compress、x/net、x/sys、yaml.v3、modernc sqlite）无已知可达漏洞。

## 4. 测试

| 文件 | 类型 | 说明 |
|---|---|---|
| `internal/httpx/o9_security_test.go` | 常规（进 gate） | Edge 不出 Root；门禁点段；工具链守卫 |
| `internal/yamlx/o9_alias_bomb_test.go` | 常规 | 别名放大被拒 |
| `internal/httpx/o9repro_test.go` | `//go:build o9repro` | O9-H1 CORS、O9-M1 后端体积，修复前失败 |
| `internal/api/agents/o9repro_registration_test.go` | `o9repro` | O9-H2 匿名注册读配置，修复前失败 |
| `internal/rulefetch/o9repro_bomb_test.go` | `o9repro` | O9-M2 gzip 炸弹，修复前失败 |

复现测试用 build tag 隔离，避免未决问题让 gate 变红；修复时去掉 tag 并把断言保留为回归测试。

## 5. batch-7 合入后补审清单

batch-7 新入口已只读看过注册处：

- 已看：`/api/agents/gateway-installations*` 的 `enroll`/`script` 不经门禁，由会话 token（常量时间比较、过期检查）认证，请求体限 16 KiB，脚本响应 `no-store` + `no-referrer`；其余入口均 `auth.Require`。`/api/agents/<id>/oxidns-ha*`、`/api/client-devices*`、`/api/domain-discovery/*`、`/api/routers*`、`/api/agents/<id>/upstream-routers` 都有 `auth.Require`/`h.auth`；`ipv6-readiness` 四条未包 `auth.Require`，但不在任何白名单，门禁要求 JWT（与 Python 相同）。
- 合入后补审：
  1. R15 路由器 CRUD：路由器密码/API token 在列表、报告、错误信息、日志中的脱敏（`device_redact.go`）。
  2. R15/R17 写设备：用户填写的设备名、备注、固定 IP 主机名进入 RouterOS 脚本/iKuai 请求的转义（`ScriptString` 覆盖面）。
  3. A18 OxiDNS HA：keepalived 配置与 `docker_ha_*` 生成物里的 VIP、接口名、密码是否只接受解析后的值；enroll 会话 token 生命周期。
  4. A22 网关安装：`gateway/installation` 生成脚本中的网络参数（接口、网段、DNS 上游）是否全部校验或 `shlexQuote`。
  5. A23 IPv6：`verification-settings` PUT 的输入（探测目标）是否成为 SSRF 入口。
  6. 若 O9-H2 修复改动注册流程，复查 batch-7 网关安装注册路径（`registration_profile`）。

## 6. 第二轮清单

- MCP（`/mcp`、`/mcp/` 6 条）：入口认证、`App.Invoke` 的内部头只在认证后设置、工具参数到 REST 的映射是否能越权调用。
- artifact 成功路径（3 条）：预览归属、过期、文件读取路径校验、下载 Content-Type/Disposition。
- O9-H1/H2/M1–M3 的修复复核（维护者决定后）。
- 全树（切换前）重跑 govulncheck，确认发行镜像 Go ≥1.26.11。
- DNS rebinding / 主机别名（CFG-01 LOW-1/2）若决定做成真正的 SSRF 控制，复核 `net.Dialer.Control` 实现。
