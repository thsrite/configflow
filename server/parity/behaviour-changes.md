# Deliberate behaviour changes

## R09 — 强制 DNS 的归属保护（Python bug 修正）

- 路由器上存在未登记归属、注释为 `cf-dnsguard:hijack` 的 NAT（不论 chain、端口、是否 disabled，
  也不论是否开启「OxiDNS 故障时自动暂停」）时，启用或更新强制 DNS 的预览变为「未知」并说明原因，
  不生成写入；撤销不受影响。Python 不阻断，Netwatch 会按注释一并启停这些规则。**用户可见：**
  由 Python 发布过、且路由器上已有这类规则的设备，升级后第一次检查会从「一致」变为该提示，
  需要先修改那条规则的注释。
- 预览后出现的保留名称对象、同注释 NAT、更靠前的 53 规则，或受管对象被改动，发布时写前拒绝并要求
  重新预览；删除前按发布记录的证明比较，被改过的对象保留现场（Python 会删除或吸收中途修改）。
- 回滚按「表 + ID」重映射恢复后的新 ID（Python 只按 ID，可能跨表混淆）；内部回滚只完成一部分时，
  已成功恢复的对象仍登记为 ConfigFlow 所有（Python 会把它们变成无归属对象，阻断之后的每次预览）。
- 见 [R09 复核](review-r09.md) 与 [R09 移植报告](progress/R09-dns-guard.md)。

## A24 review — public Agent/deployment credential safety

复核按任务要求修复 Python 原版同样存在的泄露：旧/导入 Agent 与部署记录中的
password、private key、API key、registration key 等字段，以及 host 的 Basic-auth
userinfo、HA/部署诊断中的凭据原本会透传。公共 Agent helper 和共享
`deploy.PublicDeployment` 现在复用 `redact.SanitizeCredentials`，掩码凭据字段、
含凭据的文本、已知 Agent token / 环境注册密钥及旧凭据在其他字段中的 URL 编码引用。
磁盘记录和实际请求凭据保持完整，非敏感未来字段继续保留。全局 response token
redactor 的既有策略不变。无新接口或 UI 操作变化。

覆盖：`TestReviewA24PublicCredentials`、`TestReviewA24PublicHTTPDoesNotExposeLegacyCredentials`、
`redact.TestPublicMetadataCredentials`。24 组非泄露类真实 Python 参照继续逐字段相等；
泄露类单独断言安全结果，不放宽差分比较。见 [A24 复核](review-a24.md)。

## L1 D6 — HA 验证命令选择可用的 DNS 工具

不再无条件依赖 nslookup。生成命令按 dig、nslookup、BusyBox nslookup 的顺序
选择当前可用工具，始终查询指定的已校验 IPv4 VIP；不使用无法指定 DNS 服务器的
getent。缺少工具时以 127 退出，明确提示 Debian/Ubuntu 的 dnsutils 与 Alpine 的
bind-tools；查询本身失败保留非零状态，不偷偷改用系统默认 DNS。固定批准的命令
模板用于历史语料/Flask 差分，其余字段与非法 VIP 校验继续严格比较。

## L1 D5 — HA 安装检查端口和核心状态

修复 Python 遗留缺陷：新 HA Host 安装在启动容器前通过临时 Python socket 探针
检查 Docker daemon 网络空间的 UDP/TCP 53；已证明归属且正在运行的本次主容器
续装时跳过该探针，避免误判自己的监听。新安装和迁移脚本均在输出成功前等待
OxiDNS 的 Supervisor RUNNING；FATAL/EXITED/STOPPED 或 30 次等待仍失败时非零退出。
不因外层容器 Running 就报成功。既有数据、CID 和迁移备份保留，便于明确续装/回滚。
固定 `installertest/l1-ha-deltas.json` 只反向应用批准的函数和调用点；其余语料、
Compose、注册凭据和撤销命令严格比较。真实 socket/Supervisor 测试仅使用自有容器网络空间。

## L1 D7 — 无法满足的二进制下载 Range 返回 416

协调者批准修复：Agent 二进制下载中 Werkzeug 原本抛出的范围错误被 Python 的
catch-all 转成 500；Go 返回正确的 416 和 `Content-Range: bytes */<实际长度>`。
保持 JSON 错误消息、GET/HEAD 语义，不带附件头，不记为服务器 ERROR。
正常 200/206/304、非法文件名和不存在文件的 404 不改变。差分只认可准确的同一
范围错误的 500→416 及长度头变化，其余响应严格比较。

## L1 D4 — 注册锁定提示

Python 原来就要求普通注册提交 `registration_key`，而现有 Agent/普通安装器没有
传入该字段的功能；HA enrollment 使用独立 capability。这是既有注册策略，保留
403、错误密钥的旧提示、密钥校验及已有身份 token 校验。仅在缺少/空密钥时把
`Invalid registration key` 扩充为 `api/agents.RegistrationLocked` 的明确中文操作提示。
不把全局密钥写入链接、脚本、响应或持久数据。文档说明锁定与 HA enrollment 的区别。

## L1 D3 — 覆盖安装失败恢复原 Agent

协调者批准修复 Python 遗留缺陷（2026-11-17）：单服务和 AIO 安装在停止旧服务后、
覆盖文件前保存旧程序、配置（包括身份和 token）、服务定义及启用/运行状态。失败时先
停止并禁用本次服务，再原子恢复文件，最后恢复安装前的服务状态；后续信号不会中断
补偿。未能停止新服务或完整恢复时保留私有备份并明确报错，不宣称恢复成功。
不修改核心配置或已发布数据。新生成器指纹使旧版不安全安装链接失效，须重新生成。
A08 的历史逐字节基线只反向应用 `installertest/l1-rollback-deltas.json` 中的固定
批准差异；模板、模块完整内容和指纹均有漂移检查，其余字节继续严格比较。

同一修复带来的其他可见变化（独立复核补记，[review-l1-installer.md](review-l1-installer.md)）：

- 单服务安装停止旧 Agent 失败且服务仍在运行时，报「无法停止原 Agent；未替换程序和配置。」
  并退出（Python 是 `|| true` 后继续覆盖）；AIO 原来停止失败即退出，现在服务确已停止时继续。
- 全新安装失败时删除本次写入的程序、配置、服务定义、logrotate 与 IPv4 转发启动脚本；
  AIO 还删除本次新建的 `/opt/configflow-agent/mihomo|oxidns` 目录（含新 Agent 在其中写入的
  文件）。原已存在的目录和其他文件不删除。Python 会把这些都留下。
- 单服务和 AIO 安装脚本的外层输出壳增加 `trap ':' HUP INT TERM`：Ctrl-C、SSH 断开或
  对整组发送 TERM 时，等内层补偿结束后再输出失败摘要和日志位置并以原状态码退出；
  Python 的外层壳会立即退出，补偿在后台继续且结果只写入日志。核心安装、网关安装不变。
  对应 Python 基线比较的固定 rendered delta 记录在同一 JSON 的 `rendered` 字段。
- OpenRC 补偿时用 `setsid` 启动恢复的旧服务，使其 supervise-daemon 不再位于安装器的
  进程组，避免整组 TERM 再次停止它。
- 发现以前中断（如断电）或恢复失败留下的 `.agent-install-backup.*` 时输出提醒并列出目录；
  不自动套用、不删除。

## L1 D1 — compact Compose 拒绝冒号数据目录

协调者批准修复 Python 遗留缺陷（2026-11-17）：`compose_compact` 与其他安装方式
统一拒绝数据目录中的 `:`，返回 400「持久数据根目录须为规范的绝对路径，不能包含
逗号、冒号或换行」，在生成内容和签发安装凭据前停止。已有非法路径的校验提示也
统一使用这条文案。Compose 的 `create_host_path: true` 会使用 Docker 的旧 Binds
字符串格式，长格式 YAML 仍不能使冒号源路径可用；compact 又没有 prepare 阶段。

五份原始录制语料保持不变，在比较器中仅显式认可这项路径拒绝及准确文案变化：
`docker_ha_compose_recorded.jsonl.gz`、`docker_ha_compose.jsonl.gz`、
`docker_ha_compose_review.jsonl.gz`、`docker_ha_recorded.jsonl.gz`、旧 A09 的
`docker_ha.jsonl.gz`（额外 1 条冒号拒绝、12 条旧路径提示）。其余输出继续严格
比较。在线 Flask 差分明确断言 Python 200/生成失效挂载并写入 capability，与 Go
400/不写入 capability 的差异。真实 Docker Compose 挂载测试见
[L1 修复记录](progress/l1-installer-fixes.md)。

## N8-1 — non-finite JSON response numbers (Python bug fix)

Coordinator decision, 2026-11-17; design/n7-n8.md §2.6 clarification.
All ordinary JSON responses encode NaN and positive/negative infinity as `null`.
Python emitted bare `NaN`/`Infinity`/`-Infinity`, which are not JSON and made
browser JSON.parse fail. Go returns valid JSON; charts can treat null as a
missing point. Status codes, finite numbers, strings and other fields stay the
same. File/stream passthrough exemptions are unchanged.

Coverage: httpx TestN8NonFiniteResponsesAreValidJSON and
TestN8RawNonFiniteResponses; api/agents/difftest explicitly checks only the
NaN history CPU field and the NaN/Infinity summary average fields seeded by
oddMetrics. The panel fake reply also asserts only data.a (NaN) and data.b (Infinity) → null.
Remaining fields are compared strictly. Request parsing is a
separate N8-6 contract; this decision does not change requests or persisted data.

Follow-up: A04/A05 should reject or discard a non-finite metric point at
report ingestion, so it cannot enter newly stored history. Not part of N8-1.

本表记录维护者明确接受、需要在切换说明中告知用户的行为。设计决定见
[N7/N8 第 6 节](../design/n7-n8.md)。不以兼容层模拟 Python 的编码。

## N7-4 — 原生 YAML writer 与 Python 类型输出 bug 修正

生成配置改由 yaml.v3 输出，统一两空格缩进、LF、不折行；引号、显式标签、
锚点和 Unicode 的字节形式会变化一次。切换时按 D2/O14 既定安排全量推送一次，
并先预热规则缓存。后续相同输入的输出保持确定。

修复 Python bug：旧 PyYAML writer 按自己的隐式类型规则输出文本，Mihomo 使用
yaml.v3 读取时会误解其中部分类型。例如字符串 `0o17` 被读成整数 15，字符串
`1e3` 被读成浮点数 1000；超大整数被隐式读成浮点数；带空格和时区的 timestamp
被读成字符串。新 writer 加上必要的引号或显式类型，保留原本的字符串、整数精度
和 timestamp 类型。这类节点或字段以前可能失效，现在能够按正确类型工作。

生成时若 alias 展开超过 yamlx/Mihomo 的统一预算，会明确返回带中文诊断的错误，
不再输出 Mihomo 本身无法加载的配置。小型共享图继续保留 alias，不按出现次数复制。

Mihomo 旧语料中逐行确认 111 项：字符串误读 56、超大整数误读 22、timestamp
误读 33。期望图只由真实 Python `safe_load` 旧输出生成，旧 golden 不改；其余
输出仍直接解析后比较。来源、生成命令和全部行号见
[writer oracle](../internal/convert/testdata/mihomo_writer_oracle.md)。

修复 Python bug：Docker HA 的 PyYAML 输出经过注释、`splitlines` 或中间重读时，
会在 U+0085（NEL）、U+2028、U+2029 处断开，破坏路径等字段。原生 writer 转义
这三个字符，Compose（同样使用 yaml.v3）重新解析后保留输入值。这类路径以前可能
无法使用，现在按原值工作；镜像字段的原有业务校验不变。8,623 行 writer oracle
中，字符串类型修复 21 行，Unicode 修复 436 行；分别涉及 NEL 345 行、U+2028
53 行、U+2029 47 行（有交叠）。其余 8,127 行仅布局变化，39 行输出未变，
unknown=0。每行来源、完整类型图和分类见
[installer writer oracle](../internal/installer/testdata/n74_writer_oracle.md)。
旧 golden 未改；配置字节变化合并在上述一次切换推送中。另有 HTTP fuzz 的 4 个
Compose 请求逐行确认相同的 NEL 修复（7 个字段、7 次字符恢复），以真实 Flask
writer-only 夹具显式断言；其余 HTTP 行严格比较。

## D2 — 上游持久化标识的 Go 原生编码

**C33 编辑 revision 的一次性影响**：`/api/rule-library/<id>/draft-content` 的乐观锁 token 改为 Go 原生 `encoding/json` 递归排序后 SHA-256，不模拟 ASCII 转义、Python 数字拼写。token 不持久化。切换前打开的页面可能第一次保存得到原有 409「规则集已在其他页面更新，请重新读取后再编辑。」；刷新重新读取即可。`TestDraftNativeRevisionContract` 分别验证 Python/Go token 稳定、内容变化后过期；`TestBothSidesRejectConcurrentDraftSubmissions` 在真实 Flask/Go 两侧分别并发提交并断言一条 200、一条 409，其他响应字段、DB 和文件仍严格比较。

生产差分只在精确路由 `/api/rule-library/<rule_id>/draft-content` 的顶层 `revision` 使用 `^[0-9a-f]{64}$` 归一化（协调者同意）。错误格式/类型、字段缺失仍报差异，其他响应字段、存储 revision 和其他路由仍严格比较；harness 有对应正负控。仅改 normalize 配置，baseline 不改。

C27：上游 `tag` 等字符串元数据包含真实 U+2028/U+2029 时，Go `encoding/json` 的
转义会使 `EntryKey` 与 Python 的旧标识不同。字面反斜杠文本 `\u2028` / `\u2029`
不属于此项。Go 保持原生 JSON 哈希，不增加 `canonjson` 或局部旧编码 adapter。

- **template 模式**：原来的 choice key 按过期选择处理，返回现有 400 设置错误；刷新后
  按新的上游列表重新选择。
- **native 模式**：如果旧 helper 的 hash 无法匹配任何 Go 原生 source hash，则停止
  依形状猜测 local/remote/fallback。尤其不能把 fallback 悄悄变成 remote。
  原图保持锁定；严格写入返回现有 409 routing conflict，宽松写入给 warning，
  保留原设置、source 和 YAML。旧 choice key 仍可返回 400 过期选择错误，不是 500。
  恢复需用户在编辑器里按当前上游**重新生成分流**，不支持在锁定图上直接单次重选。
- 提示：`上游标识已更新，请刷新后重新选择；已有完整配置请在编辑器按当前上游重新生成分流`。
- 新生成、能匹配 Go 原生 hash 的 helper 继续正常工作。已有 native 配置的交付结构不被
  自动改源；这里限制的是含过期标识的自动编辑/重建。

验证见 [C27 复核报告](review-c27.md)：36 条显式 D2 结果断言（精确键变化、400、
锁定读模型、409、宽松 warning/不改 YAML），其余 469 条严格比较。
## C35：WebDAV 凭据安全与表单密码保留

协调者在 C35 复核中明确要求（2026-11-17）：

- 安全修正：受 JWT 保护的 `GET /api/backup/config` 继续返回用户名和地址供编辑，密码继续掩码；地址剥除 URL userinfo。Python 原样返回 URL，可能泄露嵌入密码。
- 安全修正：WebDAV 目录诊断、日志、任务回执和通知中的 URL userinfo 凭据、百分号编码凭据一并脱敏；Python 未覆盖这些路径。实际远端请求仍使用独立的用户名/密码字段，完整备份仍包含恢复所需的完整配置。
- 防止凭据丢失：`POST /api/backup/config` 的密码为 `******` 或空字符串时均保留已保存密码。Python 仅对 `******` 复用，空字符串会清空。提交真实的新密码仍会替换旧密码；未增加清空密码入口。`backup/test`、`backup/now` 的空密码验证合同保留。

证据与最终提交见 [review-c35.md](review-c35.md)。

## C35：拒绝 WebDAV 点路径段

安全修正，经协调者裁定（2026-11-17）：WebDAV 地址和备份路径中的 `.`、`..` 路径段（含 `%2e`、`%2E%2E`、多层编码及编码分隔符变体）在保存设置、测试连接、执行备份入口统一返回 400，且不发送网络请求。中文提示统一为「WebDAV 地址和备份路径不能包含 . 或 .. 路径段，请改用不含 . 或 .. 的普通目录」。Python webdav3 会展开这些路径，可能创建 `/a/`、`/a/b/` 等意外目录；Go 不模拟该行为。

用户应将地址和备份路径改为不含 `.`、`..` 的普通目录，例如 `/backups/configflow/`。旧设置不会被静默改写；自动备份遇到这种路径时，任务状态记录同一条提示，配置提交仍然保留。重复斜杠仍归一为单斜杠，例如 `one//two` 对应 `one/two`，与 Python 一致。
## Config Step B — coordinator decisions

本表记录维护者明确决定的行为；Python 的崩溃不作为 Go 的接口契约。

| 条目 | Python | Go 决定及验证 |
|---|---|---|
| Step B #1，nodes/reorder、subscriptions/reorder、subscriptions/<id>/fetch 非对象/非法排序字段 | truthy非法类型500，falsy非对象被当成空对象，或错误信息混入缺失ID | 400 JSON、准确中文；包括null/空数组/false/0/空字符串。排序形状在提交前校验；fetch在资源查找前拒绝，不访问上游、不写状态。`api/subscriptions/input_shape_test.go`。合法对象中的真实未知ID保持404。 |
| Step B #2，server-domain/config-token 的孤立 UTF-16 JSON 转义 | Python 500；旧 Go 静默替换 U+FFFD 并提交 | 400 JSON「请求内容包含无效的 Unicode 字符」，提交前拒绝。合法代理对、显式 U+FFFD 与字面反斜杠不拒绝。`api/settings/unicode_test.go`。 |
| Step B #3，/api 错误出口（下列显式例外除外） | Werkzeug HTML 错误页（含400/404/405/500及文件失败） | 同状态 JSON `{success:false,message}`；不回显内部诊断，保留 Allow 等头，HEAD 不返回正文。原JSON错误的字段与语义继续保留。非API HTML/重定向不改变。`httpx/error_boundary_test.go`。 |
| R058-B03，domain-discovery 未知遗留字段 | 更新设置时删除未知字段 | 按N8保留未知字段并原样往返，包含false与空数组。`api/domaindiscovery/legacy_settings_test.go`。 |
| Step B #12a，Docker HA server_url 末尾换行 | Python200（正则$接受末尾换行） | 保留既定A09决定：400「请填写完整的 ConfigFlow HTTP(S) 地址」。用例明确断言该差异。 |
| Step B #12a，Docker HA非法IPv6 URL | urllib错误「Invalid IPv6 URL」 | 同为400、success=false，使用net/url原生英文诊断。只对该命名向量声明文本差异，其余字段/头严格比较。 |
| Step B #12b，安装器非法UTF-8 query | Werkzeug保留非法字节为%XX | Go使用U+FFFD，引用/转义经过bash语法和隔离赋值测试。仅把非法字节规范化后的有效UTF-8请求交给真实Flask生成完整预期，与Go严格比较；不mask脚本片段。 |

Step B #3 的面向 shell 下载例外（L1 D8，路由级 `PreservePlainErrors`）：

- `GET/HEAD /api/agents/i/<token>`，`agents.download_install_command`：不存在、
  过期或非法 token 的 404、读取存储失败的 503，保留原中文正文及末尾换行、
  `text/plain; charset=utf-8`、状态码、no-store/nosniff。**与 Python 一致，不属于变化**。
  按协调者先前的明确要求，错误额外移除 `Content-Disposition: attachment`；
  此头在 Python 中存在，是单独声明的头部修正，差分显式断言两端取值。

只标记上述确实返回纯文本错误的路由，不按 URL 猜测。审计的 Agent 二进制下载
`/api/agents/download/<filename>`（缺失文件/非法文件名）、core-install-script、
gateway-network-probe.sh、install-script、docker-agent-run/compose，以及已生成配置
的文件/归档下载，原来明确返回的 JSON 错误继续为 JSON；不因成功时下载文件就豁免。
新增 Flask 差分分别检查安装链接和二进制下载的 404/非法名称；正常 API 错误仍有
全局 JSON 边界断言，并有真实 curl -fsSL、curl | sh 回归。

## N2 — Go 原生环境代理规则（2026-11-17）

协调者决定：共享选择器使用 `golang.org/x/net/http/httpproxy`，覆盖 A07 决定 5
和早期 N2 的 loopback 经代理例外。Agent HTTPClient、Sub-Store 使用同一选择器。

- `localhost` / loopback 始终直连；内置 Sub-Store 不被环境代理绕路。需要走代理的
  Agent 请配置可路由地址。显式指定的下载代理仍生效，不受环境 bypass 影响。
- 小写 HTTP(S)_PROXY / NO_PROXY 优先；空的小写值回退大写。Python 空小写变量会
  屏蔽大写，依赖该方式禁用代理的部署应移除大写变量或使用合适的 NO_PROXY。
- HTTP(S)_PROXY 为空时继续回退 `all_proxy` / `ALL_PROXY`（小写非空优先），避免已有
  全局代理配置失效。scheme 代理优先。SOCKS5 使用 Go 原生 transport，不模拟 PySocks。
- NO_PROXY `:port` 不匹配任何 host；请写 `host:port`。`.example.com` 只匹配子域，
  要同时 bypass apex 请写 `example.com`。
- CGI（设置 REQUEST_METHOD）拒绝 HTTP 环境代理并返回错误，包括 ALL_PROXY 回退；
  不发送请求、不泄露认证。Python 忽略大写 HTTP_PROXY 后直连，两端显式断言这项
  安全差异。面板不作为 CGI 运行；不能把 httpoxy 保护回归当作预期差异。
- 环境代理 URL 按原生 net/url 语法解析；userinfo 中的保留字符应百分号编码。
  显式配置代理继续沿用 weburl.ParseProxy 的用户输入清理。

验证：`agents.TestProxyDifferentialAgainstRequests` 对真实 requests 与 Go 的逐次
录制请求分别精确断言（14 个环境 × 3 个目标）；另有 Agent/Sub-Store 的 loopback、
ALL_PROXY、优先级、CGI 零请求、显式代理和 redirect 边界。A15 的6条真实路由
额外精确断言4条 loopback 的 Python代理/Go直连 trace，升级直连和可路由 follow
仍保持完整严格比较。详情见
[N8-2 复核报告](review-n8-2.md)。

## N7-2 — native Mihomo base diagnostics and safe edits

Coordinator decision, 2026-11-17; design/n7-n8.md §2.2.
Relink parse errors retain their existing Chinese context and append yaml.v3's
native reason. Duplicate keys match ErrDuplicateKey/ErrSyntax, expose Key/Line,
and are rejected with the key and line shown; the source and transaction stay
unchanged. Other listener validation messages stay unchanged.

D3: a safe scalar/pair splice preserves all other bytes; ambiguous layouts
rewrite the document, retaining comments and all values while normalising layout.
CRLF documents with comments no longer have the old C11 refusal. Folded scalars
are emitted in literal style by the shared Marshal API to preserve their actual
value, avoiding yaml.v3's extra-newline emitter bug. Sequence/mapping-key line
comments move above their pair so yaml.v3 cannot emit an invalid colon layout;
scalar-key comments keep their position. Tab-containing strings use double quotes
(with explicit tags retained), since literal/folded Tab lines can be unparsable. Native YAML schema policies
from N7-1 remain in force. No new application route is added in N7-2; actual
proxy-groups route differential is deferred to C33.

Opus review of N7-2 (2026-11-17, parity/review-n7-2.md): a base config with more than one YAML
document is still refused with the same context (Python: context only); the appended reason is now
Chinese, e.g. `YAML 只能包含一个文档（第 2 行开始第二个文档）`. Whole-document rewrites keep merge keys
as `<<`, place notes of a removed pair before its listener (as Python did), and never expand aliases. A tag with no type name (`!%21`)
is refused with `YAML 标签 !! 缺少类型名（第 N 行）` (Python refused it as well, without that text).

## O12 — retired startup migrations

Go 不接受 legacy 存储，不再执行 `node_test_dns_cleanup.migrate` 等一次性旧数据迁移。
用户必须先升级到最后一个 Python 版本并启动一次，再切换到 Go；这些迁移已在 Python
启动时完成。O3-3 保留此既定边界，在有序 Startup 中省略 `node-test-dns-migrate`。
来源：PORTING 的不移植清单、Config Step B 的 O12 决定，以及 O3-3 协调者确认。

### O12 锁文件（O3-6a）

服务端资源互斥改为进程内锁：**Python 遗留的锁文件不再创建，已有的不删除**。
这包括下列文件（`<sha256>` 表示 64 位小写十六进制摘要）：

- `task-status.json.lock`。
- `notifications.lock`。
- `web-listener.lock`。
- `.agent-deployment-locks/<sha256(agent_id/deployment_id)>`。
- `docker-ha-installations/.issue.lock`。
- `dns-monitor/<sha256(agent_id)>.lock`（本轮差分涉及三个 Agent 的三把锁）。
- `shared/rules/by-source/<sha256>.list.lock`。
- 其他共享缓存、生成文件和配置空间文件旁的 `<file>.lock`。
- `profiles/.<profile_id>.operation.lock`。
- Sub-Store runtime 目录里的 `work.lock`、`control.lock`。

已有锁文件的内容、inode、mtime 保持不变；订阅缓存删除与孤立缓存清理也不删除旁边的旧锁。
差分只排除明确退休的锁文件或精确的摘要文件名 glob，继续逐项比较响应、配置和实际数据文件，
并额外断言 Go 没有新建对应资源锁。实例锁（含兼容 Python 的生命周期锁）和 `.config.lock`
仍保留；目标机 `bootstrap.py` 使用的 `.compose-enrollment.lock` 不属于服务端资源锁，保持不变。

## O3 — 单实例与有界关机

设计 D1/D5 与维护者 M1：每个 DATA_DIR 只允许一个 Go 服务进程；启动时同时持有 Python
四把调度器生命周期锁，忙时最多等 30 秒后退出 1。Go 不再依靠多个服务进程间的这些
调度锁分配工作。路径别名使用同一份仓库路径解析，不能绕过实例锁。

SIGTERM/SIGINT 先保存历史，再在默认 8 秒宽限期内收尾，在截止前 2 秒取消剩余工作，
最后保存新增历史并关闭资源；空闲提前结束。Python 原来在保存后直接退出，Go 在镜像
既有 10 秒停止预算内等待已有工作。第二次信号立即退出；镜像停止时限没有提高。

## A11：归档编码与 OxiDNS 校验证明

按 A14 设计 2.3/7.1 及 A11 澄清：Go 使用原生 tar/gzip，归档字节与 SHA256 会变；已由真实 Agent 解包验证。
内容漂移的 v1 Fingerprint 保持 Python 紧凑 UTF-8 JSON 编码：文件内容未变时不因编码迁移产生变更。
生成主 YAML 的字节变化仍属于 N7 已接受的一次性重新发布。
OxiDNS RulesFingerprint 则专用于校验证明，绑定 Go 嵌入 schema 和构建版本；Python 下 ready 的旧候选需要重新推送。
旧候选激活返回「候选配置缺少当前规则的校验证明，已停止激活。请重新推送配置并完成校验。」且不发出激活请求。
此外修正 Python 对缺少 profile_id 的 Agent 固定使用 default 字面量的问题：回执按当前仓库解析后的默认配置空间匹配。

## C31：聚合预览使用本次校验后的快照

可靠性修正：Python 的聚合预览在生成、发布后再次读取 provider 文件。另一个请求在这两个步骤之间替换文件时，预览节点可能来自另一次发布，与本次的订阅统计不一致。Go 直接使用生成器返回的已校验节点快照；发布文件后发生的替换不会改变这次预览。接口字段和普通场景的结果不变。`TestPublishedSnapshotSurvivesArtifactReplacement` 验证发布结果不会随随后替换的文件而改变。

C31 生成器的输入校验修正：聚合的 `subscriptions` 必须为字符串数组，缺省为空数组；Go 不把字符串拆成字符 ID。Python 的干运行生成器会把 `"s"` 当成选择订阅 `s`，而某些 null/布尔值又会在更晚的步骤崩溃。Go 在生成前拒绝这些错误类型，保持已有公共保存接口的数组合同。用户应把 `"subscriptions":"s"` 改为 `"subscriptions":["s"]`。公共路由的正常状态码、字段和中文提示不变。

C31 同时拒绝损坏的非数组节点缓存。Python 在拉取失败、缓存 `nodes` 为布尔值等错误类型时，会吞掉工作线程异常并返回成功的空 provider；Go 返回错误并保留旧发布内容，避免静默丢节点。真实 Python 差分 `TestCorruptNodeCacheCannotProducePartialProvider` 明确验证此修正，干运行两端均不写状态。

## N7-3d — 统一 reader 的诊断位置

Mihomo 导入原先拒绝多文档时只显示“YAML 解析失败”和无位置的内部原因，
现在由共享 Parse 指出第二份文档的起始行；拒绝状态与原中文上下文不变。
真实导入语料中单列“诊断位置”1 条。provider→A11 的接口改为原生 error 和
一基行列号，235 个冻结旧输出逐字核对，193 个错误和147个带位置的提示均不变，
位置差异0。生成、发布指纹和网关行为保持原有约束。

A19 网关编辑器与启用校验也受统一 alias 预算约束：超预算基础 YAML 在编辑器显示
原有中文 warning，启用校验拒绝；小型共享链继续可用。A19 四条语料的原生 loader
变化（递归1、隐式类型1、Tab2）由真实 Python 仅替换 loader 得到完整新期望，
旧 golden 不改，changed=classified4、unknown0。

C31 聚合输出的 JSON→Node 桥改用标准 JSON 解码，合法代理对转义不再被当作
YAML 转义拒绝；数字样/布尔样字符串、大整数精度和特殊字符保留原值与类型。
这是标准 JSON 语义与共享 writer 的修正，输出字段仍由原 typed struct 决定。

## N7-3 — native YAML readers (subs/substore)

Approved by the maintainer/coordinator; design §2.2/5.7. YAML 1.1 implicit spellings follow yaml.v3
(strings for yes/on/sexagesimal forms). Explicit scalar tags must parse natively; integer tags additionally
accept arbitrary-length signed decimal text, preserving precision. Duplicate keys, invalid tag shapes,
invalid merge sources and recursive/excessive aliases are rejected. All native syntax errors have a
Chinese prefix and an available line; policy/alias errors retain their Chinese diagnostics. D1 now
matches ErrPolicy plus ErrDuplicateKey, not ErrSyntax.

Timestamp/binary/nonfinite scalar JSON values preserve original text. Unknown/non-JSON tags retain
native nodes/text rather than Python constructors. Tabs in native-valid separators/scalars/URIs may
now be accepted. Native syntax range A (`\/`, `%YAML 1.2`, some double BOMs) is refused; B (repeated
anchors, deeper ordinary sequences) is accepted; C (both leading BOMs skipped) fixes BOM key text.
A cases occur only in synthetic core corpora, not actual consumer sources. No parser fork or text rewrite.

Generated YAML style follows D3. Shared Marshal retains scalar tags even when the flow emitter quotes
an implicitly typed value; Parse→Marshal→Parse must be Equal. JSON-boundary key policies remain
caller-specific (parser rejects, dialers discard, substore converts number/bool/null); one shared
projector preserves alias sharing and refuses over-budget output without truncation or null fallback.
API statuses/fields and non-YAML behavior remain unchanged. Exact input counts, expected-change
classification and final gates: [N7-3a report](progress/n7-3a.md).

N7-3a follow-up (coordinator): standard JSON with paired UTF-16 escapes is decoded first through
encoding/json (FromJSON), so pasted/stored JSON emoji nodes remain valid. Non-JSON YAML flow with
surrogate escapes is refused by yaml.v3 with Chinese ErrSyntax; no text repair is added. Read-only
snapshot scan: 43 stored fields, 55 JSON, 14 YAML and 522 other stored/subscription text files, zero
high-surrogate escape occurrences in every category (therefore JSON=0, non-JSON YAML flow=0).
Surge/Loon's 29 changed synthetic corpus outputs use complete real-Python expected configurations,
not omissions or normalization. The original 34697-row old oracle is checked before native injection.


## N7-3b — rule YAML and structured proxy readers

D1 / §5.4 / HANDOFF 3d: rulefetch, refresh cache admission and convert's
structured-proxy reader use the shared Node reader. Duplicate keys, malformed
explicit standard tags/merge sources, recursive aliases and alias amplification
are refused explicitly. A proxy policy/alias failure is a validation error; it
is not retried through Sub-Store. A failed subscription policy check replaces
neither its transport cache nor its validated-node cache; an unsafe historical
transport cache is refused on reuse while its original bytes are retained. Alias diagnostics name
physical occurrences, without repeatedly validating the alias target as another
rule; some redundant errors/positions therefore differ from Python.

§5.7/D3: rule edits use the shared splice-first editor. Unchanged documents and
no-op edits keep every byte, including BOM/CRLF/comments/quotes. A changed scalar
keeps all other bytes; sequence insertion/removal and flow-to-block normalization
may require a whole-document rewrite. Comments and rule order/values survive;
blank lines/quotes/indentation of stored text may normalize once when the user
actually edits that rule. Delivery-only flow-to-block formatting remains a
generated-output concern (§5.7); it does not rewrite source/cache bytes. Read-only/device reads of an already supported block layout do not
rewrite stored content or change its fingerprint.

Clarified in N7-3 (coordinator, 2026-11-17): Python refused physically multiline
quoted rule scalars; Go accepts and folds them according to native YAML semantics.
A folded value containing a space is still rejected by domain-rule validation.
A valid escaped-line continuation has the same rule matching behavior as its
single-line value. Native scalar typing, timestamp/binary text, tagged collection
shape and preservation of non-string Node keys follow §5.7; a complex mapping key
cannot cross the structured-proxy JSON boundary and is an explicit validation
error. Scalar-key projection uses shared `ProjectJSONKeys(StringifyScalarKeys)`;
converted-name collisions keep the last value at the first position, as the
existing JSON boundary did. Native map member order/JSON number spellings may differ.

Python bugs fixed: clearing an unindented payload sequence emitted invalid YAML
(`payload:` followed by unindented `[]`); it now emits a valid empty list. Appending
to a flow list containing an alias previously inserted ahead of an existing rule;
new rules now follow every existing rule in order. Neither fix changes other
rules' values.

N7-3b format-hint bug (same origin in Python and old Go): leading blank lines,
`#` comments or a standalone `---` hid a flow-root `payload` mapping from format
inference. Such sources could be treated as plain text, return empty DNS rule
sets, or acquire invalid YAML on append. The shared hint now recognizes the flow
mapping after those leading lines; nil-payload append re-reads and validates it.
The visible change is correct rule recognition and valid appended YAML. Plaintext
remains opaque; no YAML mapping policy is applied to ordinary Classical text.
This also fixes A11's MosDNS/OxiDNS rule-source conversion; no production
baseline is changed to conceal a difference.

## N7-3c — native YAML reader policies

Coordinator decisions, 2026-11-17; `design/n7-n8.md` §§5.4/5.7.

- MosDNS keeps the existing default-template/line-format fallback for ordinary parser syntax
  errors. D1 duplicates, invalid tags/merges/explicit scalar contents and recursive or excessive
  aliases now use its existing rejection path with the facade's Chinese line-number diagnostic;
  these errors are never hidden by a fallback or replaced with null.
- Unknown and `!!python/*` tags are kept as Nodes (no Python constructors execute): scalar text and
  collection structure pass through. Typed fields still validate their native destination type;
  invalid tagged discovery ports return a Chinese validation error, never an empty/zero port.
- Composite keys remain Nodes; string-key schemas ignore irrelevant keys, while JSON proxy
  objects reject composite keys through the existing “Provider YAML 内容无效” path. Scalar
  int/bool/null/float keys use the single shared JSON key projection. HR desired plans are tested
  against the same input with opaque keys removed.
- Ordinary deep nesting follows yaml.v3's limit rather than Python's recursion limit. Native
  parser grammar (TAB, anchors, directives, BOM/escapes) and unlimited decimal integers follow
  the approved shared policy; native syntax diagnostics carry Chinese context and positions
  when available. Counts are in progress/n7-3c-gate.md.
- Router desired reading and router-split base documents use the shared policies. Ordinary generated
  output remains semantically equivalent; native YAML typing, timestamp text and the approved
  rejection classes are counted individually in the N7-3c migration gate. No router-write logic changes.

### N7-3c HR desired-plan refusal (coordinator decision)

Router desired reading previously substituted the default fake-IP network when
YAML was refused. It now stops on D1/other ErrPolicy, recursive aliases and alias
budget errors, returning a typed Chinese validation error identifying the profile
and Mihomo base configuration, plus the native key/line/reason. No desired plan is
returned and no caller-supplied work/device request starts. Only true ErrSyntax
keeps the old default fallback. No router-writing code changes. Three D1/budget/
self-reference loopback RouterOS regressions assert zero requests, with a live
ordinary-syntax fallback control. R01 corpus: 3 D1 + 1 other policy refusal.

## N8-6 — native request binding and correct bad-request statuses

Coordinator-approved local migration: only `POST /api/auth/login` and R226
`PUT /api/tasks/interval` use the new native binders in this step. Login malformed/empty/trailing or
non-object JSON previously became Python's caught 500; Go returns the native bad-request 400 envelope.
Wrong/missing JSON Content-Type becomes 415 (Python login caught it as 500). Valid JSON with/without
charset, +json, BOM/UTF-16, login success, wrong credentials and wrong credential field types retain
behavior; wrong credential types still produce the original Chinese 401.

These two migrated callers reject NaN/Infinity/-Infinity as malformed JSON. Python login used to
return 401 on those credential forms; Python R226 reached its interval validator and returned its
numeric-validation 400, while Go's silent binding returns the existing malformed-body Chinese 400.
17 named vectors independently record both the real Python handler response and Go's expected full
error body from real Flask strict/malformed controls; no broad normalization or field exclusion.
Native responses still convert non-finite Go numbers to null through N8-1's sole response exit.

Legacy JSON helpers stay unchanged until their callers migrate. Every N8-7+ consumer records its own
non-finite request →400 transition explicitly; Agent metric/traffic ingestion and stored NaN seeds
remain A04/A05 follow-up. No query/int-string lexical rules change (N3 owns them).

## C32 — HEAD 规则接口只读（Python bug 修正）

Python 的 `GET/PUT /api/rules/dns` handler 用「不是 GET」选择写分支，因此带合法 JSON body 的授权 HEAD 请求会提交 DNS 设置。Go 明确按 PUT 才写，HEAD 走 GET 并保持数据不变。规则列表和规则集列表的 HEAD 也走读取分支，修复 Python 未返回响应而报500的问题。用户需要修改 DNS 时继续使用 PUT；无需迁移存量数据。

证据：真实 Flask `TestPythonHEADWriteIsFixed` 断言旧行为实际写出 blackhole action，同时断言 Go 完整 SQLite/文件状态不变；`TestHEADCollectionsAndDNSStayReadOnly` 覆盖四个列表/DNS入口。其他方法继续严格差分。
## A24 shared publicAgent — credential references before projection (A13 review)

Security correction, independently reproduced against real Python: a public Agent's journal or
HA message could expose a credential defined in an old deployment. Projecting or sanitizing that
deployment first removed the defining field before the outer sanitizer could learn its value.
The shared helper now collects named credentials from the raw record before projection and applies
the existing shared sanitizer to the public result. Full, detail, summary=1 and history=0 reads all
retain that protection. Summary collects in the same repository read view before omitting history;
it constructs no history copy and returns no raw history reference. The collector reuses the sole
bounded/cycle-safe visitor and never logs values. No global JSON-hook policy or revealable user
configuration credential policy changes.

## A13 — registration credentials in content diffs

Security correction, reproduced through real Python preparation and Flask routes: the environment
AGENT_REGISTRATION_KEY, including URL-encoded forms, could leave through push-diff, paged entries
and search matches. StoredSecretFinder now includes an independent snapshot of that key. The sole
A12 WithoutStored walker retains partial redaction for display texts and masks any remaining known
credential in metadata strings/keys (including row paths and file paths). Search uses the same finder.
Plain user configuration passwords remain revealable only in their existing marked credential pieces;
their hidden view stays secret-free. Global ExternalPayloadSecrets / JSON-hook policy is unchanged.



## HEAD 只读（C07 / C29 / C31 / C32 / C34）

协调者确认的共同类别：HEAD 按 GET 的只读语义处理，沿用 GET 的状态和 headers，不返回 body。
C29 的 Python settings 路由自动接受 HEAD，但把非 GET 请求都当作保存；携带 JSON 的 HEAD
因而会保存设置并递增 revision。Go 的两个 OxiDNS settings 前缀统一进入只读 owner，忽略 HEAD
请求体，不改变 DB、文件或 revision。`api/oxidns/difftest.TestC29ExpectedDifferenceHEADReadOnly`
在隔离的 Python 副本上明确复现旧写入，再验证 Go HEAD 与 GET 的一致性及完整存储不变；
`api/oxidns.TestC29HEADReturnsGETHeadersWithoutSaving` 逐个比较 GET/HEAD 的全部 headers。

## N7-3d share — OxiDNS readers

`parseYAML` 与 `safeLoad` 迁到共享 `yamlx.Parse`；复用 §5.7 已有 D1、reader-policy、unsafe-alias
等类别。只有 ErrSyntax 保留原来的 native syntax / template fallback 契约；重复键、错误标签与
形状、无效 merge 源、自引用和超预算 alias 不回退。native 的原 builder 与所有业务校验不变，
include 的 source/path 诊断保留 reader cause。真实输入逐行旧/新对照 unknown=0，计数与安全回归
见 [reader 迁移报告](progress/n7-3d-oxidns.md)。普通接受输入与 C29 行为严格比较，无额外差异。


C29 / N8-6 非有限 JSON 请求：两个 OxiDNS 前缀的 16 个 POST method/path pair 使用 BindSilent，
在绑定阶段拒绝 NaN、Infinity、-Infinity，返回对应路由现有的 malformed-body 400 完整响应。
Python 对 worker_threads 的非有限数原本进入整数校验（仍400，但错误消息不同）；草稿请求中
未使用的顶层成员含非有限数时原本可返回200。`TestC29ExpectedDifferenceNonFinite` 为每个 POST
各列三条显式预期差异（48条），真实 Flask malformed-body 作为完整 Go 响应控制，并严格比较
headers、完整 DB 和所有文件不变。合法 JSON、普通数字和原有业务错误继续严格差分。


- N7-3d convert readers apply the approved native scalar/text policy at their
  temporary JSON boundary (N8-8 replaces it). Real caller corpus audit:278
  classified expected changes, unknown0; non-JSON scalar metadata has no Mihomo
  numeric/time field occurrence. Custom YAML failures keep their Chinese context
  and report native reasons;34 diagnostic rows change displayed positions
  (32 primary lines,34 unavailable columns omitted), recorded in
  `internal/convert/testdata/n73d_diagnostic_positions.json` under5.7「诊断位置」.

## Go 取代 Nginx：Web 监听端口的验收边界

维护者在 Config Step B 中明确决定（2026-11-17）：Go 通过公开入口的 `CONFIGFLOW_FRONTEND_DIR` 启用原生 HTTP listener，取代 Python 部署中的 Nginx。API-only 模式没有 Web listener，不能拿它与带 Nginx 的完整 Python 部署比较并据此报缺失功能。

- R206-F17/F18/F19 的 Nginx 配置检查、重载、回滚专属故障列为部分覆盖；Go 验证对应的原生监听失败。用户可见结果应等价：换端口失败时配置不被改动，响应有清楚的中文提示，原端口仍可继续访问。不要求 Go 模拟 Nginx 的进程、配置文件或错误文本。
- Step B 继续比较完整部署的公开状态、成功换端口、占用拒绝、无变化重试、数据库/保存的端口文件与失败后的旧监听可达性。不同实现的临时端口必须按各自实际分配值验证，不能任意忽略端口字段。
- O6 中“真实 Nginx + Flask 整体”的叠加验证归入 .32 容器实验和 O9/O14，不在 Step B 内执行。Step B 的 Python前端HTTP peer可以为fake，须明确其边界；Go 端仍从公开入口真正启动原生前端。

之前以 API-only Go 对比完整 Python 部署得到的13个web变体证据已撤回，不能计作业务差异或覆盖通过；原始无效报告仅保留在测试者的临时目录。
## C33 — 规则库缓存和连通性测试可靠性修正

- **DB 已提交而内容缓存写入失败时，禁止继续交付旧缓存**。Python draft/内容 PUT 在提交后才改稳定缓存，磁盘错误会留下「新 DB + 旧文件」。Go 先暂存并同步内容，在 DB 提交前留下持久失效标记，提交后原子替换并 fsync 目录，最后清除标记。替换失败返回 500，旧稳定和 legacy 缓存均不可回退；交付从当前 DB/inline 重建或直接交付。未知提交故障保留失效标记，避免误判回滚后恢复旧文件；手动缓存可修复标记。事务内明确拒绝时清理暂存文件，配置不变。未新增 repo 更新原语；N8-5 正式事务 API 落地后重写这条 DB+cache 协议。
- **连通性检查不覆盖并发编辑**。Python 批量 HEAD 后保存旧共享快照，可能覆盖测试期间的库修改；同时没有按 PUT 重建 native DNS。Go 检查结束后重新开启事务，只有 ID 和完整声明仍与测试快照一致才关闭失败源，并复用 PUT 的 native profile rebuild/parking。检查期间的其他编辑保留，已关闭源的 DNS 状态同步提交。
- **HEAD 只读**。Python draft handler 会把带合法 JSON 的授权 HEAD 当 PUT 保存；Go 只有 PUT 才写。规则库、策略组和代理域名集合的 HEAD 按 GET 处理，修复 Python 无返回值而 500 的分支。正常 GET/POST/PUT/DELETE 的功能不变。
- **缓存选择必须是数组**。Python 把非空字符串/对象当容器筛选 ID；例如 `rule_ids: "manual"` 会同时缓存 ID `a` 和 `manual`。Go 拒绝这类非数组选择并返回现有 400，请调用方传 `rule_ids: ["manual"]`。前端已使用数组。真实 Flask 回归复现误选两条，Go 拒绝并保持 DB/文件不变；数组中的未知值仍按 Python 忽略，完全未匹配时返回原有 404。

证据：`internal/api/rulelibrary/fault_test.go`、`draft_test.go`、`mutate_test.go`；真实 Flask/Go 的 `TestDraftHEADCannotWrite`、native DNS/parking、全部 25 方法 HTTP/SQLite/文件差分。D1 的 YAML 引号/缩进和 English 内部异常诊断按既有标准比较；监听入口用户注释另有保存断言。

## C33 独立复核 — inline 缓存发布的读侧约束

失效标记只有明确不存在时才允许稳定/legacy 回退；权限或 I/O 错误一律按失效处理。inline 交付在编辑标记存在期间不补写文件，防止旧请求快照在 draft rename 后覆盖新缓存。普通冷缓存仍会补写当前 DB 声明；共用 rulefetch 的 inline writer 在现有 repo 事务锁内核对当前声明及字节，过期/已删除/已变 remote 的迟到调用不发布。只做缓存副作用后中止未修改的 DB 候选，不改变 revision 或配置 hooks。交付保持 cache→inline 顺序，返回状态/内容和普通冷缓存文件与 Python 相同。没有新增后台恢复；失败/中断编辑的标记由下次编辑或手动缓存修复。

## C33 / N8 原生 JSON 绑定

C33 请求 adapter 使用现有 Bind/BindSilent，拒绝 Python json.loads 独有的 NaN、Infinity、-Infinity。
严格绑定入口按 malformed JSON 返回400，batch-health-url 的 silent 入口返回原有 malformed400；
明确捕获 JSON 解码异常的 cache/reorder/proxy-domains/preview-regex 保留各自 malformed500合同。
这是既有 N8 原生 JSON 类别，不影响合法 JSON、Content-Type、精确大小写成员或未知字段保留。
真实 Flask 正控证明 check-content 原来可接受 unused:NaN；三种非有限字面量在14入口的响应/无写入
均按真实 Flask malformed 控制比较，不放宽 normalizer。

## C33 预览来源选择的输入验证

preview-regex 的 subscriptions/aggregations 空值继续按空选择处理；数组内无法命中来源的非字符串成员
继续忽略，不阻止其余字符串ID。真值非数组现在400拒绝，避免 Python 把字符串逐字符、对象逐键当作
来源ID遍历；调用方应传ID数组。这与规则库 cache 的非数组 rule_ids 属于同一验证纠错类别；无数据迁移，
合法数组和预览结果不变。`TestReviewPreviewSelectionShapes` 对照真实Python并明确断言双方无写入。

## C33 缓存协议的跨 owner 串行化

发布暂存缓存和撤销新建失效标记时，都在现有repo锁内核对来源声明：旧暂存文件不能覆盖另一owner已提交
的新来源，旧失败请求也不能清除新来源仍需要的失效状态。来源已变时保留marker，交付使用当前DB inline，
手动缓存或新编辑可修复；名称、interval等非来源编辑不受影响，预备raw字节不重写。Create的实际提交加入
同ID编辑/清理lease，下载仍在lease前。文件副作用后不提交DB候选，不新增revision/hook/接口或后台恢复。
这些是已登记DB/cache可靠性修正的并发闭合，不代表其他模块自身DB-first修改来源的整条协议已完成。


## C06 — HEAD 设置接口只读（与 C31/C32 同类，Python bug 修正）

`rule-fetch-proxy`、`web-port`、`server-domain`、`config-token`、`backup/config`、`subscription-aggregation` 的 HEAD 一律走 GET，状态码和 headers 相同、无 body。修复 Python 将 HEAD 当写请求以及未返回响应的问题。每条路径用合法 JSON、畸形 JSON、非对象 body 断言完整 DB 指纹（含 revision）和所有文件不变；web-port 使用已启动的真实测试 listener，只改 HTTP 分派。真实 Flask 差分显式复现 rule-fetch-proxy HEAD 写入，作为已批准的预期差异。

设置路由同时沿用 N8-6 `Bind`/`BindSilent`：严格读取的畸形 JSON、非对象/null body 返回 400，错误 MIME 返回 415，替代 Python 捕获异常后的 500；silent 读取仍返回原业务校验消息。其他字段值验证及持久化语义保持 Python 一致。见 `design/c06-http-settings.md`、`TestC06SettingsFlaskStorageBoundary`。

C06 的 9 个 body reader 另有 27 条真实 Flask 对照，显式记录 NaN/Infinity/-Infinity → 原有严格或 silent 畸形请求响应的 N8-6 迁移差异；其中 Python 原来会保存非有限 aggregation enabled 值，Go 现在拒绝且 DB、文件及 revision 不变。其他有限数值及字段校验不变。

C06 独立复核安全修正：服务域名和 subscription-aggregation 更新日志只记事件，
不再拼接完整用户输入。Python 与原 Go 都会把包含 URL 密码、query token 或换行的
new_domain / 宽松 enabled 值直接写入 INFO 日志。请求校验、响应和持久化值保持原语义；
`TestReviewSettingsLogsDoNotEchoValues` 经真实 HTTP 保存验证日志无凭据/伪造行、DB 值不变。

## CFG-01：内部规则 API 下载边界

内部API原始分类不再因dot-normalization丢失；规范化后及每次redirect前共用origin/API
前缀检查，边界在整条链中保留。歧义的初始穿越地址会在任何I/O前更早400（Python可能
先请求一次源再拒绝redirect），禁止目标请求数必须始终0。正常内部重定向和外部源不变。
20组真实Python合成loopback对照另证明：编码API前缀的dot初始路径可被Python当外部并
访问跨origin目标；encoded-dot、double-encoded-dot、backslash redirect目标也可被
Python接受。Go按本次安全修复要求统一拒绝这些歧义，保留Invalid remote URL的400出口，
不复制Python的边界缺陷。具体向量和原始路径见rulefetch/testdata/cfg01.json；待独立
安全复核，不用此条泛化豁免其他URL/响应/存储差异。
安全复核补充（review-cfg-c14-security.md）：带主机的重定向Location按原始写法判断是否进入内部API，
外部源跳到`/api/../`后再逃逸与Python同样拒绝（Go在请求该跳前即400）；非ASCII主机名按IDNA lookup形式比较，
因此punycode/全角写法的面板地址也按内部处理（Python当外部）。反斜杠只在作分隔符读会离开API时拒绝，
含`\`的规则名称内部地址与Python一样可用；`Location: http:ok`这类无主机的绝对引用按原生net/url返回400。

## CFG-06 / C37 — 初始化 targets 的单次调用内稳定去重

协调者裁定（2026-11-18）：`POST /api/domain-discovery/rulesets/init` 的 `targets` 按首次出现顺序去重，重复项返回200，每个目标只创建一套规则库、引用及空缓存。Python 会创建孤立库项和重复引用；旧 Go 还会重复写同一缓存，产生空 `.list.bak`。Go 修复这两个共同冗余问题，不复制旧结果；无效目标仍400且无写入，空 targets 仍默认 direct、proxy。

去重限定在一次请求内。不同调用之间保持 Python 合同：继续创建新资源及名称后缀，保留之前的资源。`TestCFG06DuplicateTargetDifference` 用真实 Flask 对重复/非相邻混合目标的两次调用分别断言 Python 冗余与 Go 去重的库项、引用、首见顺序和缓存数量；普通输入仍严格比较。后续是否复用已有绑定另行裁定。

## A20 — gateway bridge and recoverable drafts

- Agent gateway replies and diagnostics now use shared credential redaction,
  including multiline/encoded passwords, legacy registration keys and Agent
  tokens. Usernames alone remain visible. Python forwarded these fields.
- Draft previews collect named credentials from complete current/candidate and
  generated documents before projecting fields. Plain/encoded references in
  other change texts, change paths and nested added/removed object keys are hidden; Python leaks
  them. The original Chinese field mask stays `[已隐藏]`, and stored documents
  remain untouched. Exact Python leaks are asserted in the HTTP differential.
- Malformed host `managed` evidence is an unreadable record, never proof of no
  gateway record. Wrong-typed wizard routing evidence fails closed with a
  Chinese diagnostic and zero writes. The shared A14/C36 reader refuses a
  session over 16 MiB rather than treating it as absent.
- Wrong-typed Mihomo/OxiDNS draft sections refuse with Chinese 400 rather than
  becoming empty recovery snapshots (Python could crash or create an unusable
  snapshot). The original stored value is preserved.
- Native request binding rejects non-finite JSON with 400; the A20 HTTP
  differential explicitly asserts the different diagnostic for `enabled:NaN`.
  Non-standard JSON from an Agent is refused as invalid response (502).
- Preview tokens use native JSON/SHA-256; they are ephemeral and regenerated
  after switching implementations. Persisted restore documents retain their
  unknown members; restoration explicitly replaces both service sections.
  Stored snapshot version uses model.Int (1, 1.0, 1e0 accepted; bool refused).
- HEAD `/api/agents/gateway-forwarding` is read-only (A20 review, Python bug
  fix, same class as C32). Flask's handler took every method but GET as the
  POST branch, so an authorised HEAD with `{"agent_id":…}` persisted IPv4
  forwarding on the device. Go serves HEAD from the GET branch; POST is
  unchanged. Real Flask `TestA20PythonHEADForwardingWriteIsFixed` asserts the
  Python device write and Go's zero writes and unchanged storage.

See PROGRESS-agents A20 for per-class checks, commands and the L4 boundary.

## C07 — 状态列表就绪与只读 HEAD

协调者批准：tasks 和 notifications 的四条入口立即注册。真实 task slot 尚未齐备时，
两个列表 GET 返回 503、中文说明与 missing_slots；不会把缺失行当作空任务列表。后台刷新仍更新
已经观察到的失败，保留尚未观察到的 router/agent 旧通知。已就绪 owner 的运行/间隔修改仍可用，
已知未移植 owner 返回 503，未知 key 返回 400；已提交的间隔不会因展示查询失败再报 500。
R14、A16/A17 提供真实行后列表自然恢复成功，不安装空占位。

同属上面的「HEAD 只读」类别：stats HEAD 不写 stats_history.json；notifications HEAD 不刷新、
迁移或替换 notifications.json，沿用 GET 的状态码及 headers，不返回正文。真实 Flask 差分
TestC07ExpectedHEADReadOnlyDifference 明确证明 Python 原写入和 Go 完整 DB/revision 不变。

run/read 使用 N8-6 BindSilent，只有合法且有限的 JSON 能进入 owner。Python 允许忽略成员中的
NaN/Infinity 并继续执行；Go 返回 400，差分 TestC07ExpectedNonFiniteBindingDifference 明确列出。
其余畸形及非对象 body 的状态码和中文信息逐个与真实 Flask 比较。关机后的写入拒绝保留原
Runtime；请求断开继续完成。共享提交点仍由已分派的 O3-6c 补齐。

proddiff 只识别这两个精确列表入口、原 Python 成功形状及精确 Go 503 body/缺失的
router_rule_sync、agent 两项，不放宽其他 503；content type 和全部原 headers 仍严格比较。
对应的合成变异回归会拒绝错消息、错 slot、多字段、其他路由和丢失 no-store。

### N8-7 — native early-consumer codecs

- JWT: legal Python/Go HS256 sessions remain interchangeable, including tokens
  without exp and fractional numeric dates. Illegal base64url alphabet is now
  rejected with 401; Python's permissive decoder sometimes ignored junk. Unknown
  claims remain opaque. Parser diagnostic details use native JSON errors.
- Internal log formatting uses Go fmt for generic values (`nil` displays `<nil>`
  instead of `None`); visible text and anti-log-forging filtering remain intact.
  localhttp URL errors use Go quoted strings instead of Python repr.
- Existing metrics JSON/JSONL and JSON stores (traffic, probe and region) read
  NaN/Infinity/-Infinity as null only at the individual value. Finite fields and
  complete points survive; strings containing those words are unchanged. Rewriting
  produces valid JSON; repaired metrics JSONL is queued for rewrite on the next flush. This fixes invalid Python JSON that browsers could not read.
  New migrated request callers reject non-finite JSON with 400.
- Metrics summaries omit missing/null samples; an all-null sample set returns null.
  Traffic derived projections skip missing-counter points; raw points remain.
  Explicit HTTP differential changes are enumerated in progress/n8-7.md.

O14 emergency rollback caution (N8-7): Python can load and save the null-bearing
metrics files but its calc_stats/get_traffic_stats arithmetic can throw TypeError.
Summary and traffic endpoints can return 500 until affected points expire from the
24-hour metrics history. Remove null-bearing points from the corresponding Agent
metrics JSON/JSONL file or wait for retention; no migration tool is needed for this
emergency path. Go handles the missing values directly.

N8-7 native number range: persisted JSON retains valid number lexemes, including
4301-digit integers and exponents outside float64 (1e400/1e-400), rather than applying
Python's 4300-digit limit or rounding while loading. Derived metrics skip values
that cannot be represented by float64; raw history and files keep the exact value.
The synthetic metrics summary CPU average becomes 2.5625 from its four valid
samples after the seeded NaN becomes missing. No broad response normalization is
used; HTTP differential assertions name each changed field.

Python's default 4300-digit integer input limit still applies on an emergency
rollback: such a new synthetic oversized-integer JSONL point is skipped by the
Python reader. No real caller input or production snapshot contained these values;
this is the explicitly accepted native JSON range, not a common stored-file rewrite.

N8-7 Unicode log filtering (PORTING's accepted “CPython 专有 Unicode 范围” rule):
Go retains newly assigned printable U+31350 (CJK Ext-H) and U+11F00 (Kawi), where
Python3.11's Unicode14 table removed them. The two original golden rows remain as
Python evidence; the new full logfields are asserted explicitly and other rows
remain strict. Unicode15 Cf controls U+13439–U+1343F are still excluded, as are
existing controls/newlines/bidi overrides. See the authoritative
[Unicode15 category data](https://www.unicode.org/Public/15.0.0/ucd/UnicodeData.txt).

## R10 — IPv6 路由归属与回滚安全修正

Python 的 IPv6 规则集批量删除只核对 ID/目标地址，Go 在每个批次提交前核对完整 stable config，
拒绝预览后外部修改/ID 复用；创建也在最后 scoped/console 读表拒绝新出现的同目标外部路由。
Python `_rebuild` 可按相同配置猜测新的 owner ID，Go 只接受本次恢复响应证明的新 ID，
部分成功也重映射；若初次 apply 已创建对象但补偿 DELETE 拒绝或回读失败，
保留创建响应/console 证明时的完整 config 与可信新 ID，登记恢复 receipt，
下一次预览逐项核对，避免已知归属对象成为孤儿；不会在失败后按当前相似配置补认领。
保持原配置证明，避免重映射时把外部编辑刷新为“自己的配置”。
规则集整体回滚不完整时保存两代原始证明；Fake-IP 旧对象未恢复时保留当前已发布证明；
失败恢复后不继续删除仍能保护流量的替代路由。见 R10 部分回滚/外部同配置/重映射回归。

错误类型/空 ID/重复 ID/错误版本/缺失 resources、Fake-IP spec 与 resource 不一致的 manifest，
即使该 ID 已不见也拒绝作为空 ownership。已带 zone 的 Agent 候选不再被拼成双 `%` 下一跳；
路由前缀解析拒绝 scope-id，避免复用 matcher 的去 zone 处理误把 /64 截成 /128；
接口地址 stable evidence 仍保留 zone/host bits，规范化 /064。
无有效唯一候选时保持既有 blocker/skip。无效/缺失 range 的 Python 内部 ipaddress 异常文本
改为明确中文（业务错误文案不改）。model 的 custom_config R5 fallback 也拒绝作“已关闭”处理。

host 错误沿用共享 agents/diag 脱敏，隐藏已知 Agent token/router password 及标注凭据，
普通 username/api/权限诊断保留。设备所有写/回滚 owner 在 Detach 后与提交前遵循 shutdown 检查，
关机取消后禁止继续写；请求断开仍完成。不可证明的丢失写回复保留现场，不猜测、不删除。
R10 不生成/重写 YAML；原保存 YAML（含 CRLF/注释/引号/alias）逐字节保留，读取政策沿用 N7。

R10 rule-set 的 borrowed 回读也要求路由仍然 active：Python 仅比较排除了 active 的 stable config，
会把发布期间失活但配置未变的借用路由报为验证成功；Go 返回既有 borrowed verification 错误。
`TestR10BorrowedRouteMustRemainActive` 同时固定 fake/rules 的只读回读边界。

R10 复核 F1：IPv6 路由补偿 DELETE 报错（回复丢失、回读失败）后，若重新 `GET` 证明该 ID 已不存在，
按补偿成功处理并继续后续恢复。Python 把它记为回滚失败（`RouterRollbackError`）但仍继续恢复；
Go 移植版原本把它当失败并跳过 Fake-IP 旧路由恢复，使网段失去路由。现在设备结果与 Python 相同，
旧路由的新 ID 按恢复响应重映射进 manifest，错误类别为原始失败而非回滚失败。
`TestR10ReviewLostDeleteDeviceMatchesPython`、`TestR10ReviewFakeRollbackRestoresAfterLostDelete`。

## R11 — PD 归属、部分补偿与 readiness 原生证据

Python 的 PD guard 只保存 role/path/ID；Go 新写记录追加完整 stable config，后续外改和 ID 复用不再
按同名或「像 cf-pd」覆盖。旧 ID-only 记录继续读取，但存量脚本/定时任务必须能确认为原模板，
而非只含保留名称；重复保留名称、错型/重复角色、错误表或 ID 均拒绝。完全匹配的手动安装仍沿用，
不登记；确认的部分更新仍按 Python 规则管理整套安装，共用 hook 不接管。记录的未知顶层、spec、
resource 成员在正常更新、重映射和部分恢复中保留。
完整配置证明只用于 ConfigFlow 自己的脚本和定时任务；hook 所在的 dhcp-client 属于用户，仍按 Python
只认 ID，钩子内容由 hook 状态把关（R11 复核 F1）：改 use-peer-dns、改池名、把前缀换到另一个客户端
都照常更新或撤回，不会把整个保护卡死。

Python 只在整个 PD 回滚成功后更新旧 ID，且按 ID、不含表名映射。Go 对每个已证恢复即时收集新 ID，
即使其他步骤失败也按 (path, ID) 重映射，保留原配置证明；补偿删除失败时保留已证的新对象创建配置，
整个 stage 的恢复失败也不丢掉当前记录。不会按当前相似配置猜测/刷新证明，不收养恢复期间出现的
同名外部对象。补偿 DELETE 回复丢失，用一次 GET 的 404 证明已不存在才按成功继续（R10 复核 F1）；
拒绝、外改或读失败继续报告回滚未完成。已有未知 PUT 仍保留现场，不凭相同配置猜新 ID。

IPv6 DNS 的 ND payload 在最后事务读取也严格验证 disabled/dynamic/invalid；null 或未知状态不再
作为可写对象。新增 readiness route undo 在保留随机尝试标识的同时核对完整 stable config，
因此外部更改非目的/下一跳字段也会阻止删除；丢失 DELETE 回复同样只在 GET 证明确已删除后成功。
关机后的补偿受同一取消约束，无法补偿的残留仍需 O14 drain 和 L5 验证；请求断开继续完成。

原生格式差异：ND 的 DNS 列表在内部 stable 快照中用 JSON 文本，替代 Python repr；服务器列表、
操作值及设备表本身不变。readiness fingerprint 使用 Go 原生、稳定排序后的 JSON 摘要，首次切换
需要重新检查后再 prepare；只依赖稳定路由字段，路由顺序和 lease timer 变化不影响摘要。
摘要输入里的路由器记录不含设备密码（Python 含），前端拿到的摘要不能用来离线猜密码；改密码不再改变摘要（R11 复核 F2）。
复制用 CLI 复用 ScriptString，对控制字符也转义。业务中文、所有有效操作、设备最终状态与真实
Python 对照；PD 脚本 source 逐字节对照三组参数。readiness 不自动写入路由，仍引导用户走上游路由发布。

R11 撤回回读也检查第二次 sweep 后新出现的 `cf-pd-last`/静态弃用前缀：Python 只比原先删除的 ID，
进行中的 hook/script 若晚写出新 ID 会被误报为已撤回。Go 在本次确实删除受管 script 时报告既有
撤回校验错误，不额外删除新对象；`TestR11PDWithdrawalDetectsLateRuntimeState` 固定此边界，L5继续验证实际时序。

PD runtime 地址列表事务沿用已有 IPv4 single-row 模式：按 ID 核验/删除，只按 `list=cf-pd-last`
取创建范围。Python 对每个 state DELETE 全读 IPv6 地址列表，会连同大型 GeoIP 表反复读取；Go
扩展共享 single-row/scoped 分类到 IPv6 地址列表，完整配置证明和未知 PUT 的保留政策不变。

PD 恢复 PUT 回复已经证明新 ID、但紧接着的 GET 失败时，Go 仍保留这个新 ID 与原配置证明，
返回回滚未完成；下一次独立读取可以重新确认实际成功的恢复。对整 stage 部分 undo，只保留
尚未成功补偿的当前证明，已成功清掉的 hook 不继续登记为新安装，允许下次安全修复；保留的
新 ID 可替代该 role 原先已缺失的旧 ID。均不从现有相似配置推测归属。

## N8-5a — repository typed core

- 独立复核补齐 R5 加载边界：存储业务值无效时，启动不再尝试可选 token 写入；
  保留原 DB 和文件供 UI/typed API 修复。修复事务恢复正常 token 维护。
  Python 在这些畸形场景仍可能拒绝启动；这是 N8-5 加载/写入校验拆分的明确变化。

- **加载不再按业务规则拒绝。** 打开数据库时只检查结构：存储版本、三个配置节、配置空间 ID 合法且与节键一致、
  默认配置存在。名称、修订号、引用、Agent 绑定等规则改在每次写入时检查。Python 遇到这类存储值会整体拒绝加载
  （界面无法打开）；Go 照常读取（类型错误的成员按 R5 读作零值并保留原字节），修复它的那次写入通过，
  其他写入在修复前返回原来的校验消息。只有手工改库或旧版本写坏的数据会遇到。
- **三方合并按 JSON 类型比较。** `SaveConfig`/共享/系统设置带基线保存时，`1`、`1.0` 与 `true` 不再视为同一个值
  （Python 用 `==`）。参照集 30 例中 3 例结果改变（`reference_test.go` 列出）；正常界面数据不会混用这些类型。
- **修订号写入规则按 R8。** 整数值的浮点写法（如 `60.0`）是有效修订号；Python 写入校验拒绝它。负数、字符串、
  null、缺失仍拒绝，布尔仍按 Python 接受。

## N8-5b — typed profile/shared writes

- **共享资源写入保留 shared 节的未知成员。** Python 每次 `update_shared_transaction` 只写回四类资源和 `_revision`，
  会丢掉 shared 节里其他成员；Go 按 R1 保留。
- **按类型读取的名称。** 资源被引用时的提示与 usages 中，名称存为 `null` 或错误类型时显示资源 ID（Python 原样返回该值）。

### R12 — release ownership and negotiated return routing

- Canonical native JSON (sorted keys at every depth, no HTML escaping) gives the
  opaque release fingerprint. It is stable across processes and retains exact
  integer evidence, but intentionally differs from Python JSON whitespace hashes.
- F-R12-1: a transport failure after Agent apply/remove retains compensation for
  the potentially committed write. Reverse compensation runs through the same
  ownership-aware safety handshake. A lost compensation reply is accepted only
  after a GET proves removal or the complete expected raw spec (including unknown
  fields); unavailable readback does not prove success. A definite A01 RemoteError
  remains a rejection, without speculative compensation.
- F-R12-2: return cleanup excludes the explicit (profileID, routerID) pair while
  searching all profiles. A background inspection no longer accidentally excludes
  a same-ID router in the default profile or marks its own router as shared.
- F-R12-3: all Verify failures use native errors/RouterError so callers, including
  R14, can compensate uniformly; no Python ReleaseError exception category is
  needed. R14's delta owner must handle every error (its own acceptance).
- D-R12-3 / Clarified in R12: after Runtime shutdown interrupts a release with a
  started (R12 review F2: attempted, not only proven) device or Agent write, stop device I/O and compensation, retain Result, and record
  one local failure+manifest+revision transaction using the explicitly approved
  ctxx.Uninterruptible boundary. Before the first write, cancellation commits
  nothing. R04–R11 interruption before a lower owner records its manifest remains
  an O14 follow-up; R12 does not guess ownership of an unproven create.
- New iKuai NAT group checks now include the Python contract's explicit `id:null`;
  policy group checks continue to omit `id`. Public desired reports omit internal
  DNS guard inputs, as Python does. Skipped IPv6 DNS retains `checks.ipv6_dns:{}`.
- Agent errors use shared diag credential redaction without hiding the username;
  raw successful Agent status preserves unknown fields and numeric literals.
- R12 review F4 (Python bug fixed): a forward return-routing apply/remove first
  re-proves that the Agent still holds the managed spec the plan was built from
  (unknown fields by value, `proxy_dst` as a set). If another router sharing the
  Agent changed or removed it after the preview, the stage fails with Python's
  `回流资源分配在预览后发生变化，请重新预览后发布` and rolls back; Python removed or
  overwrote the other router's rule. Compensation during rollback is unchanged.

### R13 — pending reminders

- Pending reads reuse R12's explicit-profile inspection (F-R12-2 / T27), including
  cloned router IDs across profiles; invalid or nonexistent explicit profiles never fall
  back to a different profile. The inspection/advisory implementation remains R12-owned.
- Cache identity uses native canonical JSON and an internal SHA-256 digest. It
  intentionally differs from Python's ephemeral cache hash; router credentials
  never appear in reminder payloads or cached result DTOs. There is no on-disk
  cache migration and no TTL. Unknown reads are retried.
- Unexpected reader errors/panics produce the clear native message
  `读取失败：内部错误` instead of exposing Python exception class names. RouterError
  diagnostics use shared credential redaction; panic payloads are never logged.
- Shared reads survive request disconnection through tracked lifecycle workers.
  Runtime shutdown cancels device reads and rejects cache/notification admission;
  C07 batch-1 owns the final cancellation checks under the notification leaf lock
  and before Rename. R13 does not duplicate that transaction implementation.
- (R13 review F1 / X1) Stored router records follow Python's `routers_of` and the
  repository's R5 rule everywhere in `internal/routers` (`routersOf`, used by R12
  `returnCleanup`/`Publish` and R13 pending): a missing or non-object `routers`/non-list
  `items` means no routers, `null` and non-object items are skipped, and a member of the
  wrong type reads as absent (its default) instead of failing the record. One such
  record no longer fails the reminder of every Agent, another router's release, or the
  return-cleanup check; the stored bytes are kept for publication CAS and write-back.
- (R13 review F2) An empty Agent ID links nothing. Python's `== agent_id` would have
  matched routers stored with an empty gateway (unreachable through Flask's route).
- (R13 review F3) Timing at the read boundary: the first caller also waits at most 35 s
  for the shared read (Python's owner had no bound; the read itself still ends at
  25 s), and reads still running at the 25 s budget are cancelled instead of
  continuing unobserved in background threads. An unsupported router type is logged.
- (R13 review F4) The C07 Summarize hook decodes the public report/plan JSON with
  typed fields; data it cannot decode yields an `unknown` change receipt
  (`无法解析上游路由差异报告，请重新检查。`) where Python's `summarize` either
  tolerated the shape or raised and `safely` skipped the receipt. Only Go's own
  marshalled Report/Plan reach the hook, so this path is not reachable in practice.

C34 同类修复：`/api/profiles` 的 HEAD 不再创建 profile，`/api/profiles/<profile_id>` 的 HEAD 不再删除；全部 profile GET 路径走读取分支。回归 `api/profiles.TestProfileHEADIsReadOnly` 比较 GET headers，并断言 DB、revision、业务文件和生成缓存不变。真实 Flask 预期差异另见 C34 差分测试。

C34 自定义配置五条路径同样将 HEAD 分派给 GET：Mihomo、Surge、MosDNS 不再因无返回值而 500；Loon/OxiDNS 不再落入保存分支。`api/config.TestCustomHEADAndBodyBinding` 逐条验证全部 GET headers、无 body，以及 DB、revision、业务文件和生成缓存不变。严格请求绑定的畸形 JSON/媒体类型沿用 N8-6 的 400/415 修正；不把捕获后的 Python 内部 500 作为合法输入契约。

## C34 custom-config JSON 对象契约

协调者裁定（2026-11-18），只适用于五条 `/api/custom-config/{mihomo,surge,loon,mosdns,oxidns}`：

- **Python 200 的无字段非对象请求**：Surge/Loon 将部分字符串、数组、数字、布尔（包括 `""`、`0`、`false`、`[]`）误当无字段补丁，未改业务字段却递增 revision。Go 一律 400，DB、文件、revision 不变。
- **Python 500 的非对象请求**：Mihomo/MosDNS 的属性访问，以及 Surge/Loon 的不可迭代值或含字段名的字符串/数组，会抛内部异常。Go 一律 400，DB、文件、revision 不变。OxiDNS 原已 400，继续拒绝；消息明确要求 JSON 对象。
- **null / 缺失 body**：五条路由统一执行空更新，返回 200、revision 恰好 +1，业务字段和文件不变。Surge/Loon 的 JSON null 本来如此；Mihomo/MosDNS 的 null 和严格解析缺失 body 原为 500，OxiDNS 原为 400，也按本次显式裁定统一修正。
- 普通对象的每个字段值仍交给现有业务 validator；不把 Python 容器成员关系移植为新 helper。profile create/generate 不采用本项新 schema。

`api/config/difftest.TestC34ExpectedDifferenceCustomJSONShapes` 对五个引擎逐项断言十二种 shape 的 Python/Go 状态码，以及两边各自的 DB、完整字段、revision、文件效果。`TestC34ObjectFieldValues` 单独对照对象里的 null/false/0/空数组/空对象/true/数字，避免 DTO 意外收窄业务输入。严格畸形 JSON 400 / 非 JSON 媒体类型 415 沿用 N8-6 的迁移决策。YAML `yes` 为字符串、重复键和 alias 预算沿用 N7 §5.7，不作为未知差异。

## C34 base_url 字段 schema

协调者裁定（2026-11-18）：缺失/null 不覆盖，字符串（含空串）照常使用，其他 JSON 类型统一 400，且不提交 DB、业务文件、生成缓存。仅限 `base_url` 字段，不改变其他字段，也不扩大 custom-config 的 body schema 到 profile create/generate 的空 body 默认行为。

Python 的 Surge 等生成器将 null/false/数字/容器隐式转成文本，可生成 `None/api/…`、`False/api/…`、`3/api/…` 之类的无效 URL；OxiDNS 的同类预览请求因字符串属性访问返回 500。Go 用共享的 `configgen.RequestBaseURL` 读取精确字段名并校验类型，不移植这些 Python 文本转换。`TestC34BaseURLInputAlternatives` 明确断言五个引擎十种输入的 Python/Go 状态、Surge 的 None URL 证据、Go null 等同缺失、合法字符串输出一致、预览及两个下载前缀均无非法输入提交。
