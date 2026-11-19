# Deliberate behaviour changes

## N8-8d — typed provider delivery and response identity

- Provider快照持有只读Node图；stored group的错型enabled按共享R5读为缺省值，错型group数组
  读为空列表。缺失或无效的必需名称仍明确拒绝。85条snapshot逐行独立预期执行真实Python拓扑
  业务，仅注入批准的stored read view与native输入校验；另4条退役内部异常保留拒绝，改用
  typed InputError／net/url.Error。原archive、业务中文和正常产物／缓存副作用保持原期待。
- provider flight摘要使用共享jsonresp.Canonical与SHA-256，保留数字token、排序对象键；
  这是瞬时single-flight key，无持久化身份迁移或离线兜底失效。独立精确JSON字节与负控验证。
- 修复Go迁移遗留的关机绕过：浏览器断开仍完成已接管工作，但Runtime关机在每个写入点停止
  后续缓存／聚合artifact／last-good发布，不再用Uninterruptible跨过shutdown。真实Runtime、
  repo叶锁和两次缓存写之间关机的回归证明已完成前缀保留、后续不写、不补偿。
- DNS辅助出口改为typed query pairs、规则文件行切分、标准regexp quoting、netip/netipx；
  仅测试在原HTTP corpus入口投影退休的动态query参数，原输入和期望不动。没有新依赖。

## N8-8a provider fix review — strict identity discriminators

- `proxies_order.type` and a strategy group's `type` must be JSON strings at
  the converter shape boundary. Wrong types report the field and original JSON
  value instead of becoming unknown enum strings or R5 zero values that silently
  remove references/bypass chain classification. Resource IDs retain the shared
  numeric-text compatibility and unchanged stored tokens. This applies the
  maintainer's strict identity-field rule without narrowing the shared codec.

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

## A14 review — 手动回滚预留不再因丢失提交而卡死（Python bug 修正）

手动回滚先把任务写成 `rolling_back` + `manual_rollback`，再向 Agent 提交。若面板在两者之间退出
（kill -9，或关机取消在 Agent 读到请求前断开连接），Python 只在状态为 `unknown` 时重发回滚，
之后每次查询都停在「回滚请求正在等待 Agent 确认」，新的发布、回滚、恢复全部被拒绝，只能改数据库。
Go 记录本进程是否仍持有这次提交：持有期间行为不变；没有持有者时，查询与 `unknown` 一样重发同一个
幂等、受 Agent 保护的回滚请求，然后按 Agent 结果记录。见 [A14 复核](review-a14.md) M1，回归
`agents.TestReviewA14ManualRollbackWithoutLiveOwnerIsReissued`（真实 Runtime 关机 + 重启）。

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

### O3-6b — RouterOS 批次间的请求断开

按 O3 §2.5 的 owner Detach 契约与 PORTING 4b，ApplyMany 批次间检查工作 ctx，保留
Runtime 关机取消，但不再因浏览器断开停止后续批次。旧 R04 review 回归有意从断开后
100/150 改为完成 150/150，所有写入仍必须有证明和记录；新增真实 Runtime 控制在
第100条已证明记录回调后关机，保持100/100且后续批次零写入。生产batch上限和设备timeout不变。

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
- A malformed Agent `forward_filter` report affects only that gateway's list
  item: wrong-typed `state`, source `kind` or remedy `source` yields `error`,
  a fixed diagnostic and `can_persist:false`. Healthy gateways remain visible.
  Python's set membership could raise TypeError and fail the whole list; the
  previous Go sanitizer instead silently discarded the malformed evidence.
  `TestStepBA20MalformedReportOnlyAffectsItsGateway` checks all three fields
  with arrays/objects through the authenticated HTTP boundary and two Agents.
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
  Python device write and Go's zero writes and unchanged storage. Step B
  strengthens `TestReviewA20HeadGatewayForwardingIsReadOnly`: empty/valid/
  malformed/non-object bodies retain all GET headers and status, have no body,
  never enter a save transaction, and leave SQLite fingerprint, revision,
  ordinary file bytes/modes/mtimes and Agent state unchanged (POST control writes).

See PROGRESS-agents A20 for per-class checks, commands and the L4 boundary.

## C26 — URI 协议名不再误报为插件引用（Python bug 修正）

当主监听插件 tag 为 `udp` 时，Python 把独立旁路上游 `udp://…` 中的协议名当成插件引用，
错误推导该旁路会调用 sequence_main，导致 ReadPolicies 不可编辑、ApplyPolicies 拒绝合法配置。
Go 的共享引用扫描通过 `net/url` 识别紧接 `://` 的 scheme，只排除协议 token，继续扫描后面的
真实引用。`$udp`、`jump udp`、URI 后的 `$udp` 和真实的主链依赖仍会阻止不安全的编辑；
不是全局忽略名为 udp 的插件，也不改 YAML 内容。

证据：`TestStepBC26ProtocolSchemeIsNotReference` 的协议/真实引用对照，及
`TestStepBC26IndependentSidePolicyLifecycle` 的公开编辑边界全流程。主监听 tag=udp、独立旁路
使用 udp 上游时，添加/修改/撤销设备策略均成功，保留 side_entries、旁路配置和注释。
加入真实 `$udp` 引用的负控仍不允许编辑和保存。Python 源码和原比较规则不变。

## C36 — 完整向导响应保护私有会话凭据

Go 的私有 router draft 和未知凭据字段继续只保存在 session 文件里。公开响应在完整组装后，
还使用共享 `redact` 和原始会话凭据清洗字符串引用，包括默认配置摘要、集合的来源/规则名称和
放弃计划。Python 只投影 router draft；原 Go 也漏掉了这些在会话投影之外组装的元数据。
公开失败消息同样清洗私有会话凭据引用，保留原状态码和错误链；固定的 owner 就绪 503 文案不变。
普通用户名和 `has_password` 布尔值保留，存储中的可恢复密码不变。
证据：`setup.TestReviewC36ResponsesRedactPrivateSessionCredentials` 的 GET、PATCH、集合和
abandon dry-run 四条回归，以及 `TestReviewC36PublicOwnerFailureRedactsSessionCredential` 的
公开 409 失败回归，均先失败、修复后通过。不会修改 Python 或差分比较规则。

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
- **null / 缺失 body**：独立复核时维护者撤销统一空更新裁定，要求逐引擎严格匹配真实 Flask。Surge/Loon 的 JSON null 返回200、业务字段不变、revision+1；Mihomo/MosDNS 的 JSON null 返回500、无写入；四者缺失 body（带 JSON Content-Type 或不带）均500、无写入。OxiDNS 的 null/缺失均400、无写入。显式 `{}` 是另一输入：Mihomo/MosDNS 清空 custom_config 并 revision+1；Surge/Loon 空更新并 revision+1；OxiDNS 400。null/缺失不再登记为 Python 功能差异，错误内部诊断文字不模拟。
- 普通对象的每个字段值仍交给现有业务 validator；不把 Python 容器成员关系移植为新 helper。profile create/generate 不采用本项新 schema。

`api/config/difftest.TestC34ExpectedDifferenceCustomJSONShapes` 对五个引擎逐项断言十二种 shape 的 Python/Go 状态码，以及两边各自的 DB、完整字段、revision、文件效果。`TestC34ObjectFieldValues` 单独对照对象里的 null/false/0/空数组/空对象/true/数字，避免 DTO 意外收窄业务输入。严格畸形 JSON 400 / 非 JSON 媒体类型 415 沿用 N8-6 的迁移决策。YAML `yes` 为字符串、重复键和 alias 预算沿用 N7 §5.7，不作为未知差异。

## C34 base_url 字段 schema

协调者裁定（2026-11-18）：缺失/null 不覆盖，字符串（含空串）照常使用，其他 JSON 类型统一 400，且不提交 DB、业务文件、生成缓存。仅限 `base_url` 字段，不改变其他字段，也不扩大 custom-config 的 body schema 到 profile create/generate 的空 body 默认行为。

Python 的 Surge 等生成器将 null/false/数字/容器隐式转成文本，可生成 `None/api/…`、`False/api/…`、`3/api/…` 之类的无效 URL；OxiDNS 的同类预览请求因字符串属性访问返回 500。Go 用共享的 `configgen.RequestBaseURL` 读取精确字段名并校验类型，不移植这些 Python 文本转换。`TestC34BaseURLInputAlternatives` 明确断言五个引擎十种输入的 Python/Go 状态、Surge 的 None URL 证据、Go null 等同缺失、合法字符串输出一致、预览及两个下载前缀均无非法输入提交。


## C34 独立复核 — profile 绑定元数据凭据

profile 列表、Agent绑定列表和改绑结果的 host 可能是旧的 Basic-auth userinfo 形式，
Python及原Go投影均原样显示；改绑冲突名称中的 password=... 也会进入错误JSON。
这四个公共出口现在复用 redact.SanitizeCredentials；原存储/完整管理员备份仍保留连接信息。
这是凭据安全修正，单独回归 TestReviewC34BindingCredentials，不放宽普通Flask差分。
custom null/缺失裁定见上文最新逐引擎合同，dry_run及profile默认/校验修复均恢复Python行为。

C34 rebase 到 N8-5 后补充实证：custom-config 先保存 `config:false`，再显式保存
`config:0` 时，Python 的三方合并把 false/0 视为相等而保留 false；Go 按已裁定的
JSON 类型比较保存 0。`TestC34ObjectFieldValues` 对四个宽松引擎分别断言这个差异，
仅替换这一字段建立预期快照后继续比较完整 DB/files；OxiDNS 仍按其文本合同拒绝。
这沿用 N8-5 的类型裁定，不放宽其他请求或状态差分。

### R05 Step B — iKuai response presentation and failed probes (Python bugs fixed)

- `IKUAI_NETMASK_SUFFIX_ROLLBACK` (medium): static_rt network reads, add
  confirmation and compensation compare the mask token without the UI display
  suffix, matching the existing edit-form convention. Plain-mask replies remain
  supported. Compensation still requires the unique tag and every recorded
  configuration field, and deletes the ID read from that matching device row;
  external changes are not adopted. The default suffix fake is retained.
  Python's confirmation and undo both failed on the suffixed reply and left the
  newly created route. Real-firmware presentation and cleanup remain L5 items.
- `IKUAI_PROBE_NESTED_SCHEMA_CRASH` (low): a non-object, non-null verinfo is
  rejected as `*RouterError{Kind: KindInvalidResponse}` with
  `iKuai 版本信息格式无法识别，请检查固件版本和管理接口。`, rather than Python's
  AttributeError/HTML 500. Missing/null verinfo keeps the unsupported-version
  diagnostic. No untrusted value is echoed.
- `IKUAI_PROBE_REDIRECT_PARSE_CRASH` (low): malformed Location is a
  `KindInvalidResponse` RouterError with
  `iKuai 重定向地址格式无法识别，请在浏览器核对管理地址、协议和端口。`. Go already
  returned malformed redirects without following them; the explicit response
  error also covers a malformed port. Python requests could throw ValueError
  before its RouterError handler and skip recording last_probe. R15 must catch
  all RouterError variants, persist the failed probe, then map its HTTP status.
  A repository-consumer contract test proves this error can be saved as the
  sole last_probe replacement with exactly one revision increment; R15 HTTP
  remains unported and is not claimed verified here.

## R16 review — isolated client evidence and private credential references

- RouterOS discovery skips non-object members of a valid table array and keeps
  its healthy rows. It marks the reading incomplete, so absent saved devices
  remain unknown. Python rejects the whole mixed table: a malformed ARP or DHCP
  server row can hide all clients, and optional tables lose their healthy rows.
  A non-array table still fails; this does not turn invalid replies into empty
  successful scans or change the shared write/ownership driver paths.
- Discovery applies shared credential redaction after joining saved identities,
  live readings and refresh diagnostics. A device's text cannot echo a peer's
  stored password or a known Agent credential from the profile. Encoded
  references and unsupported/cached responses are covered; usernames stay
  visible. Private configuration and cached evidence are not rewritten.
  Production HTTP authorization and its repository-wide token hook remain R17.

## N8-5c — typed validation, router guard, imports

- **写入校验的类型错误消息。** 名称不是字符串时，提示里显示其 JSON 文本（`null`、`true`、`{"a":1}`），Python 显示 repr（`None`、`True`）。
  Python 因内部异常返回 500 的输入（例如 `system_config` 不是对象、路由器 ID 是对象、旧备份里用对象作名称）改为 400 校验错误，或（旧格式导入）正常转换。
- **路由器保护按 JSON 类型比较。** `verify_tls` 从 `true` 改为 `1` 等类型变化视为连接变化，会先取发布锁；格式错误的发布记录仍视为存在（不允许删除或改地址）。
- **名称去除首尾控制字符。** 配置空间名称去掉首尾空白与控制字符（含 U+001C–U+001F，与 Python 的 strip 一致）。
- **停用标志按存储值判断。** `enabled` 存为 `0` 等非布尔值时按其真假判断（与 Python 相同），不按缺省“启用”处理。

## C34 proddiff — N7-4 / D2 与 deferred owner 的精确映射

- N7-4（D3）：`GET /api/custom-config/mihomo` 的 `default_config` 是默认模板的
  原生 YAML 文本；文档结构不变，writer 布局可变。只登记这个字段。
- D2/N7-4：proddiff 既有 baseline-format 证明在私有 Go 副本迁移主配置 YAML 的
  字节表示。`GET /api/config/export` 仍导出副本中的可恢复真实状态，因此 Agents 的
  applied_content.fingerprint、主文件 config.yaml 的文件摘要和存在时的
  deployed_config_sha256 随之不同；只登记实际证明涉及的精确结构字段，不忽略整段 Agent。
- C34 Clarified：两个 artifact POST 模板和 artifact GET 暂缺 frozen release owner
  时明确 503，未知 service 仍400、未知 action 仍404。差分登记 deferred → A14/A21，
  这些 owner 正式接线后条目必须 stale 并删除，不能作为永久豁免。
- logs 路由 owner 是 C06；既有 A18 F1 产生的异常日志后果随 A18 延期，未批准忽略
  普通日志内容。

## A16 — 自动规则同步的提交与日志安全修正

- Python `_publish` 在异常时无条件记录 failed；Go 使用已有 A01 OnlyUnsubmitted，已提交的部署交由原轮询确认，避免将可能已生效的部署误标终态。
- A16 的状态、任务记录及调用共享 presence owner 的日志字段先经过共享凭据快照/diagnostics/weburl；不保留 Python 将 URL userinfo、token 或密码写进设备名称日志的行为。正常文字保留，URI userinfo 按共享日志策略移除，原 Agent 数据不变。
- 按既定 D2，rule_sync.state.key 使用 Go 原生 CanonicalDigest；切换时一次重建防抖键。旧 accepted native task 仍先查询确认，键不同只触发重新检查/刷新，不提前接受来源，也不回滚已提交工作。
- 按 O12 不再创建 worker.lock 或仅供加锁的目录；实际 receipt/归档路径仍兼容。batch-5 集成后任务记录已使用 RecordContext(owner ctx)，共享 SQLite/taskstatus 最终取消检查与真实 Runtime 锁等待取消回归均通过。

A16 收尾的两条并发可靠性修正：HA enable 在初次检查之后才提交时，settings 事务仍会拒绝并整批回滚；OxiDNS GET 状态期间绑定/设置变化时，首次及回退 POST 前重新校验，不继续发起旧来源下载。Python 的这两个检查只在较早位置，分别有修复前失败的 SQLite/loopback 回归，见 A16-handoff-zh.md。

## A17 — DNS-follow 摘要与写归属

按设计最终裁定 **D-A17-1 → B（2026-11-17）**，DNS binding/私有 job 使用 A14 的 Go-native
CanonicalDigest/SettingsDigest。切换后旧 binding 需重新选择一次（O12/O14 发布说明项）；不仿真
CPython JSON 编码。配置与 Agent 归档按解析后语义相等验证，排版不追平。

R05-3 的非空证明要求应用到 DNS follow：空 binding/child/job proof、错类型或 null 索引不授权对
upstream 0 的写入。finalization 在同一 profile 事务内重新核对来源、目标及 binding，封住 Python
核验后再写的归属变化窗口；用户在发布期间编辑的新草稿仍保留，报告成功但 draft_synced=false。
公共 options、状态、事件、任务错误和日志 label 扩展使用 A24 的共享凭据脱敏合同，磁盘私有 job
保留。见 [A17 交回](progress/A17-handoff-zh.md)。

A17 复核（2026-11-18，[review-a17.md](review-a17.md)）补充：

- 草稿已写入后的签名重绑只认 binding id（与 Python 相同）；草稿事务内的归属重查保留。重复 finalize 正确识别
  已写入的候选草稿，不再误报「草稿有新修改」。
- 失败 child 的 `error_details` 先用全部 Agent 凭据脱敏再交给 A01（Python 只用目标自己的 token，属 Python 漏洞）。
- DNS 接口回复逐成员解码：类型不对的成员按缺失处理，给出与 Python 相同的具体拒绝文本；非字符串的 `message`
  按缺失处理（落到「请升级 Agent…」默认文本），不复刻 Python 的 `str()` 表示。

## N8-8a — 畸形存储值改为类型校验

规则集格式推断的 typed 入口不再模拟 Python 的 truthiness 或动态 ID 相等。
URL 为 0、false、空数组/对象等非字符串值时明确拒绝（5 等原有错误仍保留 URL 类型错误类别）；
引用 ID/name 为非字符串、规则库元素为非 object 时按 R5 或明确类型校验处理。
legacy 边界先解码整份规则库，因此匹配项之后的非 object 也会拒绝。正常输入的格式输出不变。

- common 的错误类型字段按 R5 读零值：如畸形 rule_configs/规则库列表读为空列表，
  非字符串规则值/策略在注释中读为空串，错误类型的 enabled 读默认启用；原存储字节仍由 model 保留。
- `SetGeneralOption` 只用 Unicode 空白集合清理段落末尾；U+001C/U+001D 不再被当作空白删除。
  5 条旧语料显式断言原生输出，属于 PORTING 已批准的 CPython 专有范围；正常 INI 字节不变。
  大写转换改用原生 `x/text/cases`，10 条完整 Unicode 一对多映射结果保持原样。

## N8-8a — 生成不再改动输入副本

节点转换只读 typed Node/Params，在独立输出图中补齐 VLESS 的 encryption 和 reality short-id。
不再重现 Python 浅拷贝导致的输入工作副本被偷偷加上 `short-id: ""` 的副作用。
31 条原始语料用独立行号/路径夹具明确断言输入保持原文，其他 mutated 字段仍严格比较；
正常生成配置字节不变，也没有存储或 API 响应变化。

- 节点名、server 等错误类型字段按 R5 读取；非对象 params、非字符串的结构化 type 明确校验失败。
  原生整数端口集合不接收布尔、浮点字面量、溢出的端口，也不保留 CPython 集合迭代顺序。
- 非法 JSON 中的 NaN/Infinity 裸词经原生 YAML 解析为字符串；合法 JSON 仍优先使用 JSON 解析，
  包括 surrogate pair。不恢复 Python 的非标准 JSON 扩展。

## N7 shared writer — 拒绝无法读回的显式标量

JSON 的超大指数（如 1e400）仍按原文保留并能 JSON 读回，但 yaml.v3 无法按 float 标签解析它。
Marshal 会返回中文 ErrPolicy，不再成功输出一份自己 Parse 不了的 YAML。
正常有限 float、下溢数和大整数的读写闭合不变。

## N8-8a — EasyTier typed settings

正常设置的默认值、选项顺序、生成字节和中文验证消息保持不变。
畸形根值、非数组生成节、非字符串版本改为明确类型校验；typed Config 里无法解码的
EasyTier 字段按 R5 缺省，避免重建 Python truthiness 和容器遍历异常。
整数选项和保活 interval 按 R8 接受整数字面量的浮点写法（如 1500.0、6e1）。
保留标准 IPv4 net/host mask 和带前导零的前缀长度支持；不会替换成私有 IP 解析器。
导出时 secret 成员即使存成错误类型也会脱敏（非 null、非空字符串），不再按动态 truthiness 决定。

## N8-8a — typed dialer validation

拨号资源只使用字符串 ID 和 typed 引用。畸形存储字段按 R5 或明确中文类型校验处理，
不重建数字/布尔 ID 相等、Python truthiness、StopIteration/KeyError 等内部异常。
缺失目标或目标名称返回明确验证错误；错误类型的 enabled（包括 null）按 typed 默认值读取。
整份资源数组和策略组列表的既有中文校验仍保留，避免把坏类型传进尚未迁移的生成器。
非法或超出原生正则能力的过滤/改名规则统一走原有中文业务验证通道。

Provider 改名规则里的非 ASCII 分组序号（如 `<$١>`、`<$１>`）保留为字面量，
属于 PORTING 已批准的 CPython 专有 Unicode 范围。旧 golden 不改；两条 review 行和
含 `$1$0$١` 的八条旧语料各有完整 native 期望。ASCII `$1`、`$01`、`$0` 及越界仍按原业务验证。
override 和改名规则按精确大小写键读取，未知大小写拼法继续忽略。

### N8-8a review — displaced YAML anchor repair

MaterializeAliases now also handles a definition moved after its first consumer,
rather than deleted. The first consumer remains the definition and the old
physical occurrence loses its duplicate Anchor; later aliases keep their exact
binding. This restores valid Edit behavior without weakening Marshal policy or
adding a production Parse/Equal pass. Normal converter byte expectations and the
31 explicit input-immutability paths are unchanged.

## N8-7 second batch — native early consumers

- Web listener environment values use native Unicode whitespace: U+001C/U+001D
  surrounding a port no longer count as spaces (PORTING CPython-only scope).
  Normal integer ports and saved files are unchanged.
- Log I/O failures retain the Chinese context, status and selected path; the
  OS detail uses Go's native error text rather than Python's errno/repr format.

- Task status counters use the same native decimal reader as task history.
  CPython-only Unicode decimal strings now return a validation error instead of
  emulating int(); the original file remains untouched on failure. Persisted
  NaN/Infinity in task-status.json becomes null on read; a later counter update
  counts on from zero, while unrelated fields and receipts remain present.
  Counter errors use ordinary Go error chains rather than Python class names.

### N8-7 second batch — D2 and file readers

- **D2 / 一次性身份变化**：notification fingerprint 和 RouterSignature 改用
  encoding/json 的原生编码（对象键由标准库排序，数字保留 json.Number）。
  不保留 Python 编码器。升级后仍存在的问题会使已忽略通知重新出现一次；
  在 Go 中再次确认后，相同问题不会反复出现。路由器旧检查通知可能标为 stale，
  下次检查/预览会刷新它。该签名仅服务于通知，不进入规则同步或发布的任务、
  baseline、状态机；不会因此触发同步、发布或新增 pending/waiting 任务。
  无需为了编码变化手动同步或重新发布。独立进程重启测试锁住两个新签名。
- **持久化 nonfinite**：task-status.json 的计数/时长及其他成员、notifications.json
  的 occurrences/failure_count/evidence 及未知成员、各配置空间 stats_history.json
  的 samples[].subscriptions/nodes/proxyGroups/rules 及未知成员，读取时将裸
  NaN/Infinity/-Infinity 转为 null，有限数据和数字原文保留，首次重写变为合法 JSON。
  字符串里的这些单词不变。请求绑定不调用此修复入口。
- 统计历史的超大数（例如 1e400 或 4301 位整数）仍原样存储；超出有限绘图范围
  的投影点为 null，不再让 /api/stats/overview 因浮点溢出返回 500。正常计数仍与
  Python 严格对照；坏的十进制计数字符串仍给出明确验证错误。
- R5：错型任务/报告/统计历史成员用类型化读取或明确错误处理，不再模拟
  Python 的 len/str/truthiness 或异常类。任务计数器的原 168 行语料不改，
  8 行 CPython Unicode 数字改为显式拒绝；12 行 nonfinite 使用显式原生预期。
  其余字段继续严格比较。通知的非 object 单项仍是 500，非法 store 容器仍是 503。
- task-status、通知、统计历史文件的空白和键顺序可以改变；读回结构与普通字段
  值保持一致，未知成员保留。统计历史重写现在也保留根的未知成员（R1），
  Python 原实现会丢掉它们。通知 store 或无效计数的校验错误不覆盖原文件；
  task-status/可选 stats 的损坏文件沿用原先的静默默认策略。

紧急回滚补充：task-status 和 stats 的 null 计数可由 Python 的 `int(value or 0)`
消费；真实 Python 互通测试覆盖读取和保存。notifications 的正常生产 occurrences
始终是整数；若手工/损坏文件原来含 nonfinite，Go 重写后该值为 null，Python
`observe` 的 `occurrences += 1` 在下一次不同 evidence 时会抛 TypeError，可能使
通知列表返回 500 或后台通知刷新失败。此类未解决通知没有统一的自动保留期，
需要删除 notifications.json 中对应坏项后重新观察；无需删除任务状态或配置。
真实 Python 回归固定“能加载保存、递增时 TypeError”。O14 清单见
[progress/o14-switch.md](progress/o14-switch.md)。第一批 metrics/traffic 的 24 小时
回滚注意事项仍适用。

### N8-7 second-batch review corrections

Invalid root/items notification containers retain ErrInvalidStore and HTTP 503
with the native decode cause; invalid individual items retain their existing
classification. Nullable legacy Agent inputs now reach the native owners'
existing no-op/default-registration paths instead of failing during transcoding.
Notification occurrences use arbitrary-precision JSON integer counts, fixing an
inherited Go int64 wrap/reset at large finite values; Python integer increments
already preserve these counts. Unchanged numeric notation, invalid-field R5
provenance and finite/unknown siblings remain intact. These corrections add no
new D2 signature transition and do not change the documented null rollback risk.

## N8-9 discovery — typed boundaries

- Python bug 修正：apply/probe 的非 object 根请求不再触发 AttributeError/500，而是返回明确的中文 400；原有两个 `[1]` HTTP 用例显式断言 Python 500、Go 400。忽略域名的存储列表使用共享 model.String 解码后按域名规则过滤，避免错型或无效主机名变成忽略规则；正常域名原文不变。
- 畸形存储值改为类型校验：discovery 的布尔开关只接受 bool，错型使用 R5 缺省值。合成差分夹具的 `auto_probe: 1` 从 Python 的 true 变为 false，下次保存设置时写回 false；普通 true/false 不变。HTTP 差分只对这个字段逐项断言，其余响应、Agent 请求和落盘数据严格比较。
- 规则库 id/name/source_type/behavior/content、规则 id/itemType/rule_type/value/library_rule_id/policy、策略组 id/name 和参与操作的 Agent 标识字段，只有 model 记录错型时才拒绝，错误为「<资源> <id> 的 <字段> 必须是字符串，当前值：<存储 JSON>」。数字文本和整数字符串使用 model 的共享解码规则；合法空值、缺失和 null 不额外收紧。
- discovery 的 11 个写请求使用 native BindSilent：畸形、空 body 和任何位置的 NaN/±Infinity 均在写入前返回中文 400「请求必须是 JSON 对象」。33 个逐词 HTTP 断言保留真实 Flask 的 200/400 证据，Go 全部返回 400。其他 legacy 请求调用方不受影响。

## A21 — gateway onboarding

- 同属「HEAD 只读」类别。Python `gateway_onboarding` 只区分 GET 与其他方法，带合法 JSON 的 HEAD 会保存 Mihomo/OxiDNS 草稿并增加 revision。Go HEAD 走 GET，只返回相同状态与 headers，不返回 body，DB、业务文件、revision 不变。`api/gatewayonboarding.TestHEADIsReadOnlyAndMatchesGET` 与真实 Flask 的 `TestFlaskHEADExpectedReadOnlyDifference` 显式验证。

- Python bug（复核 A21 F2）：原生 OxiDNS 的备用链已停用、`_prepare_native_gateway_fallback` 已恢复成功，但随后 `apply_common_settings` 仍拒绝（例如监听入口缺失）且用户明确确认转换模板时，Python 在 `settings` 被重新赋值为「已恢复」投影之后才写 `gateway_onboarding_native_backup`，备份里是被改过的 native_config（补回 fallback 插件、改写 proxy_dns_seq），并非用户原始文档，与提示「原生配置……保存在 backup 中，可从配置导出恢复」不符。Go 的恢复结果只在 apply 成功时替换 settings，备份始终是原始原生文档。`gateway/onboarding.TestConversionBackupKeepsOriginalNative` 固定此修复；真实 Python 复现见 review-a21.md。

A21 的临时 C36 依赖缺失503豁免已撤销：batch-5 合入后 server 注册真实 DNS/adopt owner，真实 Flask 保存与 revision 条件一致。独立未注册的 package 防线不列为生产差分。
### N8-8a provider 调用方 — 畸形 definition / override 明确拒绝

Provider snapshot 调用 typed `ValidateTopology` 时，非对象的 Provider definition 或
`override` 不再泄漏 Python `AttributeError`，改为现有 `DialerReferenceError` 通道，
中文消息为「拨号拓扑字段类型无效」。这属于已批准的 N8-8 明确类型校验边界，不能返回成功。
修复 typed 迁移漏检：`yamlx.Get` 对非对象返回 nil，曾把这种坏数据静默当成没有 override。
保留先发生的名称冲突、缺失 dialer 目标、重复名称等业务校验及其消息，正常输出不变。

原始 Python corpus 保持不变。`internal/provider/testdata/n88a-snapshot-native.json`
固定下列四行的完整输入及全部 152 个 outcome；索引均从 0 起，唯一变更为
`{"error":"AttributeError"}` →
`{"error":"DialerReferenceError","message":"拨号拓扑字段类型无效"}`：

| row | 显式变更的 outcome 索引（闭区间） | 项数 |
|---|---|---|
| 366 | 2–21、32–41、82–101、132–151 | 70 |
| 369 | 2–41、82–101、132–151 | 80 |
| 375 | 2–21、32–41、82–101、132–151 | 70 |
| 378 | 2–41、82–101、132–151 | 80 |

其余 300 个业务拒绝逐项严格比较，包括 366/375 的 outcome 22–31
「Provider 节点名称与代理链出口冲突」及四行的 outcome 42–71、122–131
「生成的 dialer-proxy 引用了不存在的代理或策略组」；8 个头部结果也不变。
测试验证每一项变更的原始/原生完整结果、每行输入和变更项数，禁止把其他错误改成成功。

### N8-8a provider dialer-proxy — 诊断保留畸形原值

非字符串的 Provider `dialer-proxy` 仍拒绝；错误消息不再把列表、对象、数字或布尔显示成
空字符串。使用 yamlx 的紧凑 JSON 表示原值（大整数保留精度），字符串保持原样。
例如 Python 的 `dialer-proxy「['relay']」` 在 Go 中显示为 `dialer-proxy「["relay"]」`，
其余中文、节点名、HTTP 400、响应字段和持久化效果不变。这是已批准的原生诊断边界，
不是允许畸形引用。

`api/subscriptions/difftest.TestProviderDeliveryGraphs` 只在
`{none,managed,raw,uri}/single/malformed` 四个用例指定完整的 Python/native body，
两边分别严格匹配；没有改 normalize，也没有对其他消息做替换或放宽比较。

### N8-8a 通用 R5 标量兼容（协调者最终裁定）

共享入口为 `model.String.UnmarshalJSON`、`model.Int.UnmarshalJSON`；
`model.DecodeRecord` / `model.SetMemberJSON` 使用同一解码规则，普通 string、指针、
字符串/整数列表及 Map 值一致。移除了先前 Node 专用的 number-text 标记。

- 所有声明字符串的 typed 字段接受 int/float：姓名、标签、server、密码、资源/引用 ID、
  URL 和状态等均读为无损 JSON 数值文本。`123` → `"123"`、`1.25` → `"1.25"`、
  `1e3` → `"1e3"`；大整数/高精度小数不经 float64。null 按缺失文本处理，bool 不变成文本。
- 所有 `model.Int` 字段接受严格 ASCII 十进制整数字符串：节点 port `"443"` → 443、
  策略组 interval `"300"` → 300；可有一个前导负号及前导零。带空格、加号、小数点、指数、
  Unicode 数字或超出 int64 范围仍按 R5/验证错误处理。`json.Number` 仍不接受引号数字。
- 原始字节、未知字段、未修改成员继续按 R2 保留；没有把这些兼容读取写回存储。
  JSON 请求绑定仍遵守各路由自己的契约，后续业务检查不会因标量兼容而被跳过。
- 开放的 Mihomo Params / proxy_string YAML 继续保留原值和 tag，不强制把密码数字转字符串。
  数字 password 两条来源仍输出 `password: 123`；存储的引号 port 保留原始存储字节，
  生成时按整数输出（下述 batch-6 修复更正了最初保留引号的输出）。
  原冻结字节门的 quoted-port 类型边界仍单独用 native 回归。

回归：`model.TestGenericNumericTextAndQuotedIntegers`、
`model.TestScalarTextBoundariesAndPreservation`、`model.TestNodeNumericText`、
`convert.TestMihomoNumericServer`、`convert.TestMihomoNumericPasswordPreservesTag`、
`convert.TestMihomoQuotedPort`、`convert.TestTypedNumericResourceText`。

浮点字符串化按协调者确认保留 token 写法，不把数值等价的拼法合并：
`123.0` → `"123.0"`、`1e3` → `"1e3"`、`123.50` → `"123.50"`。
这与 Python 的 `str(float)` 有意不同（如 `1e3` 在 Python 成为 `"1000.0"`），
避免密码等文本字段丢掉用户给出的数值拼法。

通用标量规则带来两条系统设置差异：convert 原冻结 corpus row 2021、2103 中
GitHub 代理域名的数值 `5` 现在读成 `"5"`，不再忽略，因此启用相应代理前缀。
`convert/testdata/n88_scalar_native.json` 逐行固定 fn、完整输入、Python 输出和完整 native
输出的 SHA-256；不复制归档 URL，不做字符串替换或通用比较器放宽。

### N8-8a 调用方补查 — 明确类型拒绝及 unknown 保留

- 类型错误的 `dialer_ref` 不再因 R5 读作 nil 而从拓扑消失；ConfigDialers、NodeDialer、
  NodeDialerTarget 和 ResolveNodeDialer 在使用前拒绝，消息带原存储 JSON：
  `dialer_ref 必须是 {type: node|group, id: 非空字符串}，当前值："b"`。
  原业务错误类型仍为 DialerReferenceError；repo compat row 57 固定此完整结果。
- ippresence 的显式 null/无法解析的 rule_library 不能建立缓存声明的归属，返回
  unknown(null)，不借 R5 零值继续读缓存。空字符串/空对象保持 legacy 空库语义；
  已匹配声明之后的损坏尾项不影响有效前缀，保留已选格式和正常缓存结果。URL/引用 ID/名称的原生校验错误通过 errors.Is
  接入原来的“无法判断”分类。正常有据的缓存仍照常返回 true/false。
- 原 Python AttributeError 的兼容读取在此 advisory 层改为 unknown。精确白名单为
  `ippresence/testdata/n88a-native.json`，每项固定完整原行 SHA-256、Python 错误和 native null。
  corpus 行号：287、537、697、965、1010、1013、1239、1288、1302、1527、1596、1611、1773、1975；
  edge 行号：2385、2387、2393、2505、2507、2509、2511、2512。其余结果不豁免。
- repo 的已批准 native 结果固定在 `compat_native_yaml.json`：11/18/23/30/139/155 为
  标量文本读取；13 为 CPython U+001C 空白差异；64 为 include-all:1 按严格 bool 的边界；
  91/92/93 为无关畸形来源 metadata；145/146 为 params null/[]。
  structured type 的数值拒绝逐项为 175、198、199、200、201、202、203、204、207、208、209、210、
  211、212、213、218、219、220、222、223，完整中文消息/ValueError 均固定。
- `repo/testdata/reference_native_scalars.json` 逐项固定完整 Python record 和 native 结果：
  node name int、node proxy_string int、node subscription zero、group name int、group manual int、
  order id int。兼容解码不会绕过后续的来源归属、legacy 结构约束和资源存在性验证。


### N8-8a 修复换到 N8-5(c) 之后

旧 compat_dialers/native golden 测试已删除，以上旧行号留作原基线历史证据；不恢复旧文件，
也不继续改动未被执行的 compat_native_yaml 归档。typed repo 的 nodeRawDialer/legacy import
改用共享 NodeDialer；checkDialers 直接调用 ConfigDialers。通用标量和业务校验本身不变。
reference_native_scalars 的 schema str 明确接受严格整数字符串版本号；subscription zero
由新 resolver 报 ProfileInUse「引用的共享资源不存在：subscriptions/0」，保留完整 profile 上下文。


### batch-6 R5 全包断言收口（协调者裁定）

- 节点 Name 不属于严格身份字段：`name:5` 读为 `"5"`，节点输出和转换日志都保留这个名称；
  profile name `7` 也读为 `"7"`，不再出现 model 类型诊断。更新旧的空串断言，未改 codec。
- Node.Type 也是严格身份字段：实际走存储节点转换时拒绝数字等非字符串，消息如
  `节点 type 必须是字符串，当前值：5`。proxy_string 已提供节点时不读取存储 Type，
  保持原覆盖路径；Node.Name 的数字文本兼容不变。
- RuleConfig 的 `value` 和 `rule_type` 是严格身份字段。Mihomo、Surge、Loon 的生成入口
  检查保留的原始 JSON，数字、bool、null、容器不能被 R5 兼容或回退悄悄放行；
  诊断如 `规则 value 必须是字符串，当前值：5`。底层 RuleComment 仍是纯格式化：
  独立调用时会把已经解码的 value `"5"` 格式化成 `MATCH,5,`，不负责业务拒绝。
- 存储节点 port `"80"`、`"00443"` 分别生成整数 `80`、`443`，修复 typed 读取后又复用原始
  字符串 tag 的遗漏；生成不修改存储，引号原值依旧按 R2 保留。已有数值 `80.0` 保留其合法
  数值写法，Mihomo 开放 Params 和 proxy_string 的原始值与 tag 不受此修改影响。
- EasyTier MTU `"1500"` 按共享 Int 规则有效，`"1500.0"` 仍无效；通知 occurrences `"7"`
  有效读取为 7，改无关字段后仍原样存储 `"7"`。bool 和非整数仍保持原拒绝/回退规则。
- URL `0` 按共享 String 规则读为 `"0"`，规则格式为 text，文件为 `Rules.list`，不再报告
  URL 类型错误；bool、数组、对象仍按原 URL 类型错误拒绝。
- `_revision:"60"` 的 typed 读取为 60、无类型诊断，修复事务将 revision 增加为 61；
  这不取消 N8-5 的独立原始写入校验，尚未修复的字符串 revision 和数字 profile name
  仍会阻止无关写入。保留其错误断言，继续覆盖“读兼容、写验证、原样往返、修复”四个边界。

回归：`convert.TestBatch6R5GenerationRejectsRuleIdentityTypes`、
`convert.TestBatch6R5RuleIdentityValidationPreservesStorage`、`convert.TestBatch6R5StoredNodeType`、
`convert.TestBatch6R5UnusedStoredNodeType`、`convert.TestMihomoQuotedPort`、
`convert.TestTypedEasyTierQuotedMTU`，以及本轮更新的 Common、Node、日志、URL、通知、repo 旧断言。

## batch-6 — 客户端取消的日志边界

HTTP handler 返回的错误若与该 HTTP 请求已结束的 context cause 匹配，不再误记为应用 `Exception`。subscriptions 的专用失败日志同样使用工作 ctx；活请求中的内部 cancellation、断连后的无关故障和 detached owner 的真实失败仍记录。状态码/JSON/中文消息、后台 owner 的生命周期均不变。真实 TCP 断连回归证明修复前误报、修复后不误报；原先把 proddiff 的相关日志差异归为可放过的“负载噪声”的判断已撤回，另修生成器重复解码造成的超时回退。详见 progress/batch6-logs-zh.md。


batch-6 codec 窄化：格式查询只解规则库，继续使用 model.Shared 的原 R5 解码和原 RuleFormat；其余 metadata codec 同样只解 native helper 消费的字段。不再保留“无关字段里非 JSON 瞬态 Go 值碰巧让 Marshal 失败”的旧行为（协调者裁定）；有效持久化 JSON 的 R5 校验、ID/name/URL 和首匹配语义不变。窄 codec 及同快照单请求/分配基准见 progress/batch6-logs-zh.md。


## A22 — gateway installation

- Capability credential recovery proves the installation marker and full
  Agent identity inside the registry transaction. Python read the proof before
  entering that transaction; a concurrent rebind could invalidate it. Ordinary
  owned replays and explicit existing-Agent bearer proofs remain compatible.
- Malformed or wrong-typed private installation sessions fail closed with 409
  and bounded 2 MiB reads instead of some Python JSON/KeyError 500 paths.
  Latest ignores unsafe records; abandon still treats a parseable profile_id
  as occupied. Unknown valid session members survive updates.
- A21 F4's two-level redaction applies here too: system credentials are masked
  across the response, while discovered user credentials are masked only in
  diagnostics. This also protects references to another Agent's private
  credentials without corrupting structural fields for short passwords.
- The existing O3 in-process lock and shutdown rules apply to installation
  workers. Cancellation after the drain deadline preserves the recorded
  publishing IDs, makes no further commit, and resumes only on an explicit
  later publication request; no new recovery action is added.
- Python bug（复核 A22 F3）：setup check 读取 Agent 状态时，`managed` 是真值但不是对象（不可读的转发记录），
  Python `managed.get` 抛 AttributeError → 500。Go 按 A20 `HasRecord` 的 fail-closed 规则视为仍有转发记录，
  返回对应的 409。其余成员类型不符时与 Python 的真值 / `is True` / `== '1'` 判定一致。见 review-a22.md。

## R14 — 自动规则同步

- `controls` 按 D2 使用 Go canonical JSON + SHA-256，不仿真 Python 编码；旧回执仍可读取，若旧 control 不匹配则暂停并要求手动「预览发布」重新建立基准。合法 profile/router ID 的回执文件 key 保持兼容。
- 修复 F-R12-3：Python `rule_sync_write._apply_delta` 未捕获最终 `_verify` 的 `ReleaseError`，设备目的更新可能残留；Go 对所有回读错误补偿自己的增删，并保留外部编辑。`TestR14DeltaPython/verify_failure` 同时证明 Python 残留与 Go 补偿。
- 30 秒下载总预算从请求前开始，包含连接、共享客户端重试和响应读取；Python 在收到响应后才开始该总预算。连接/单次读取仍分别为 5 秒/10 秒，超时或超限不成为空路由意图。收到响应前预算用完（含重试）与 Python 的连接/读超时一样记为下载失败（failed，按间隔重试）；只有读响应体时超时或超过 4 MiB 才是「超过大小或时间限制」并暂停（R14 复核 M2）。
- 使用 O3 的进程内单实例/L1/L2，不创建每设备 flock。关机取消停止设备和本地提交；已保存的 running 回执在重启后按既有规则暂停人工确认，不新增不可中断提交。


## R17 — 客户端固定 IP 与 HTTP

- HEAD `/api/client-devices?agent=...` 与 GET 使用同一 Agent 绑定空间，保持只读；Python HEAD 原先按全局空间读取。
- 完整公开 HTTP 投影复用共享 redact，设备名称/发现/错误中引用其他路由器密码或已知 Agent 凭据时脱敏。私有存储与 CAS 证据保留原值。沿用 R16 坏行隔离，不把无效整张表当作空表。
- signed preview token 的签名、压缩、时钟仍与 itsdangerous 双向互验，但 payload 中的完整设备操作改成 Go 原生 JSON + SHA-256 摘要。签名不是加密：Python 把 DHCP before-image 写入可解码 token，可能泄露 client-id/备注等私有字段；Go 在服务端重新读取并按完整摘要比较。旧 Python preview token 在切换后需要重做（本来仅存活 600 秒；连接指纹也按 D2 改用原生编码）。解压上限 8 MiB，token 上限 1 MB。
- DHCP 归属证明要求 RouterOS server、iKuai interface/gateway 非空，且补偿 receipt 的完整 expected 存在。Python 的空 scope 相等不能作为 Go 删除外部对象的证明。异常设备响应仍可能使固定 IP 控件 fail closed，但健康发现设备保留。
- R17 iKuai 新路径保留 Runtime 关机取消，通过共用 writeForm 编码/错误分类发请求；浏览器断连由 owner Detach 后完成。旧公共 Write 单操作完成语义不变。关机后不补偿/不提交本地 metadata；中止前设备可能已落下的独特 marker 保留，需重新发现确认。未增加不可中断提交或套用仅 R12 获批的 D-R12-3 例外。
- JSON/YAML 原生表示与 D2 摘要不追逐 Python 字节。短随机 tag/nonce、validated updated_at/checked_at 是差分的精确归一化项，其余响应、revision、完整 profile 和设备表逐值比较。

## R15 — 路由器 HTTP 与策略工作区

- HEAD collection/forwarding 明确进入 GET 语义，修复 Python HEAD body 创建 router/持久化转发的缺陷。报告/提醒沿用 GET 的只读设备检查和观察记录语义。
- R05 已明确分类的坏 verinfo / 损坏 Location 在 probe HTTP 中变为结构化502，失败 last_probe 保存且只增一次配置修订；Python 内部异常会 HTML500并丢掉本次探测。没有把坏回应当成成功或空表。
- 延用 A20 的坏报告隔离：异常 forward_filter 只使本条网关 error，其他条目继续显示。公开最终投影（包括跨空间 upstream）、新 Management 日志/诊断使用共享 credential redactor，用户名保留。同名 has_password 布尔 presence flag 保持布尔，同名字符串仍按凭据字段脱敏，修复 Go 过度脱敏导致的类型变化。
- 策略工作区 revision 按 D2 使用 Go native JSON；router 的 last_probe 不影响 draft revision，名字由独立 C31 revision 保护。旧 preview/draft 在切换后需重新载入。C22 生成/保存的 native_config、template_base_config 和 dns_content 按 YAML 结构兼容，格式及其派生 opaque revision 可不同。
- R15 的规则同步 HTTP 差分也验证了已有 R14 私有 receipt 的表示差异：Go _rule_order 是对规则集合的顺序索引，公开 rules 数组仍按序严格比较；成功 delta 后 Python 省略的零值 Desired/added/removed/log，按明确缺省值核对，不丢弃非缺省数据。202 返回可以处于 queued/running/synced 的合法阶段；每个阶段中文消息须匹配，任务结束后完整 DB/文件/公开状态仍严格比较。
- 设备锁沿用 R06 的有界 write wait，mutation preflight 与 owner 都保持 Runtime cancellation；仅 R12 已获批 publication 例外在设备尝试写入后记录中止。未增加不可中断区或新的后台调度器。

## N8-8b — typed Mihomo base settings

- 原生监听地址/端口解析不模拟 CPython 数字字符：sniffer 的全角数字范围和
  上标数字返回明确的 BaseSettingsError；旧 reference 保留，两条差异单独断言。
  这是 PORTING 已批准的 CPython-only Unicode 边界。正常 ASCII 端口不变。
- Agent 上下文只读取 typed Agent 字段；错型/无效的 Agent 端口与地址按 R5
  处理，不再模拟 Python 的“整数作为 IP 地址”等动态调用语义。

## N8-8b — typed Mihomo generation and downloads

正常存储输入的 Mihomo 生成、订阅与规则集下载顺序、URL 和字节不变。畸形存储字段
按 R5 使用 typed 缺省并保留原存储字节：非数值的组 interval、非对象 health_check、
非文本 health_check_url/custom_config/custom_hosts、错误类型的区域检测端口不再进入
Python 动态类型逻辑。旧 rules/rule_sets 开放列表中的非对象项按 R5 忽略，其余项保留。
R8 接受整数字值的浮点表示（Agent port、provider_update_interval），不复制 int() 的
字符串转换规则。下载项 URL/path 明确要求文本，错误类型返回验证错误。

端口解析使用 net.SplitHostPort 和 ASCII 整数：全角/阿拉伯数字及上标数字不再作为
端口，非法的 `a]:1` 不再作为监听地址。16 条基础设置及5 条 helper 归档显式断言
原生结果（旧 golden 不改）；基础设置仍返回既有中文提示，未改字段的校验规则不变。

共享的 typed URL/rules 边界也覆盖两条 Surge helper 畸形值：非数组 rule_sets
返回明确中文类型错误；非字符串 profile_id 按 R5 缺省，生成 URL 不再拼进 Python 的
列表 repr。各有一条原始 source row 的显式 native 期望；其他输出字节保持严格比较。

## N8-8b 复核 — 生成键/规则文本的错型存储字段明确拒绝

以下存储字段在 Mihomo 输出中成为 provider 键、策略组类型或规则行文本。错型时 R5 会读成
空串/缺省，typed 生成器曾静默输出 `""` provider（`./providers/.yaml`，与策略组按原值渲染的
`use` 不一致，多个错型名称互相覆盖）、`type: ""`、`RULE-SET,,G`、`DOMAIN,,G`，或把规则集
`behavior` 换成 `domain`、把跟随组当普通组输出。现在统一返回
「<资源> <id> 的 <字段> 必须是字符串，当前值：<存储 JSON>」：订阅/订阅聚合 `name`、规则集
`name`/`behavior`、规则 `rule_type`/`value`（MATCH 不检查 value）、策略组 `type`/`follow_group`；
规则集下载入口同样拒绝。Python 对这些值或写出非字符串键、或跳过跟随组、或输出 `7` 这样的文本，
迁移前 Go 对 provider 名称已明确报错；正常字符串输入字节不变（字节门 4548/4548）。

同一节补登 8b 已有、但先前未逐项列出的 R5/显式差异：`system_config.config_token` 错型时
provider URL 不带 token；策略组引擎选项（`timeout`/`lazy`/`strategy` 等）错型时不写入输出；
`region_probe_port` 为超出 1–65535 的整数时返回「区域检测端口必须为 1–65535 的整数」
（Python 原样注入，Mihomo 加载时失败）。

## N8-8(c) — Surge/Loon typed option and health boundaries

- 节点文本协议字段的数字字面量无损读成文本，数字密码保留；null 按字段缺失处理。
  只有 bool 才作为 udp/tls/skip-cert-verify 等布尔参数，错型值按 R5 使用缺省值并警告；
  不再把字符串 `"false"` 当 true。Mihomo 开放 Params 仍原样透传。
- **修复 Python bug**：Surge WireGuard 数字列表 reserved 输出官方的 `1/2/3`
  client-id，不再输出 Python 列表 repr；字符串 reserved/client-id 保留原文。
- **修复 Python bug**：Surge TUIC 的字符串 ALPN 保留；单字符串列表展开；多元素或含
  非字符串元素的列表省略 alpn 并记录带节点名、原列表的生成警告，让 Surge 使用默认值。
  不再输出 Python 列表 repr。Loon 的列表 ALPN 格式不变。
- **修复 Python bug**：Surge 策略组 url/interval 为 null，或 follow_group 继承时缺失，
  使用默认测速 URL 和300秒，不再生成 Surge 无法使用的 `None`。
- 已批准的 CPython-only Unicode 范围：down 的非 ASCII 数字前缀不识别、不输出带宽。
- 和8b复核 F1一致，会变成输出标识或规则文本的存储字段错型时明确拒绝；诊断包含资源、
  id、字段和存储 JSON，只有模型记录了类型问题才触发。正常字段保留旧字节。

冻结字节门仍执行2353份Mihomo、1064份Loon、1131份Surge。107条已批准差异在独立的
`internal/convert/testdata/n88c_byte_exceptions.json` 中逐行记录；旧 golden/manifest 未改。

8a 共享标量修复接入后，8c 的名字、server、协议文本及 custom_config 都使用 model.String，
port/interval 使用 model.Int；不再保留私有数字文本解码或待接入例外。正常数字文本继续严格
比较旧输出。helper 的六条原 Python 类型异常输入（6630、13526、16441、16555、18393、20243）
按已批准的无损文本规则读取，逐行 source SHA 和字段保存在输入分类索引中；6630 随后仍由
正常业务校验拒绝其不存在的策略，未把错误配置改为成功。数值协议 type 是判别枚举，明确
返回包含节点、id、字段和原 JSON 的类型错误，不再静默省略该节点；原 Python 也因类型错误
失败（15488/15489）。遗留 rule_sets=false（5230）按已有类型化规则列表校验拒绝，保留中文
「规则列表必须为规则对象数组」上下文。上述 malformed/native 边界均不修改旧 golden。

## N8-9 health/check — typed 边界与原生诊断

- health/check 的存储消费按共享 model R5/R8 解码：numeric text 无损，Int 使用共享的
  数值/严格十进制字符串规则，wrong bool/容器按 schema fallback；身份字段和写入时间戳
  另作业务校验。错误不再伪装成 Python TypeError/KeyError。请求健康检查仍执行严格的
  JSON token 类型校验，worker version/latency 仍作协议验证，不能借存储宽容规则通过。
- 评分的 delay 是 float64，超范围值按 R5；不再保留 Python 任意精度运算。节点 worker
  的正整数 latency 仍以 json.Number 保留大数精度。具体 typed 差异均在独立合成参考中
  逐条断言，原三份 corpus 不改；见 progress/n8-9-health.md。
- 非法 worker UTF-8 仍失败（HTTP 400），消息由 Python codec 字节异常改成固定中文
  “测速组件输出不是有效 UTF-8”，不显示 worker 内容。Flask 差分分别断言双方完整错误体。
- 环境变量 CONFIGFLOW_NODE_TEST_TIMEOUT_MS 使用 strconv 的十进制语法，不再接受
  Python 的下划线或 Unicode 数字写法。非法值仍用原中文设置错误和503。
- worker 的失败 error 字段按协议要求为非空字符串；非字符串 truthy 容器不再被当作
  有效失败记录接受。版本缺失/不匹配的原中文区分保留；非有限 JSON 明确拒绝。
- node-probe JSON stdin 使用 D2 的 Go 原生编码；空白和 Unicode 转义可不同，JSON 字段、
  数值、argv、预算、环境、结果和持久化仍严格比较。目录删除保留 errno 与“不删除目录”的
  约束，使用 N8-7 jsonstore 的原生错误，不拼装 Python OSError 文本。

## A23 — IPv6 readiness and verification

- `check_token`、检查缓存 key、验收 target/settings/client/applied fingerprints 按 D2 复用 Go canonical digest，不仿真 Python JSON 编码。旧缓存/历史报告仍可读取；旧 token 或 fingerprints 不匹配时需要重新检查/验收，不把旧证据用于当前配置。
- 连接表混合坏行由 R16 受限 reader 隔离；不完整或超过 256 行的基线不证明某连接是新增的。无效、负数、超出 uint64 的包计数保留为未知，不把解析失败或溢出算作正包数。Python 的混合坏行会使整表失败；网络包计数的格式错误不影响其他健康记录。
- 已保存节点中的空服务器、非空但类型错误的 dialer-proxy 不占直接 TCP probe 的三个候选名额。Python 可能把这些坏节点作为直接候选发给 Agent；Go 保留健康候选。
- 系统密钥整份脱敏；用户凭据只影响诊断及明确凭据字段，保留状态、ID、作用范围和 check_token 等协议值。种子覆盖完整 peer 配置与捕获快照，包含在请求期间被替换的旧凭据；主机和 ROS 的诊断不能引用另一台设备的密码/token。
- 真实 Runtime 关机取消停止设备请求、缓存/报告提交，正在运行的旧报告按既有五分钟期限显示中断；浏览器断连不取消已开始的写操作。HEAD 保持 Python 的 404，零写入、零设备请求。
- 主机持久化证据和转发/RA 详情中的非字符串值按 JSON 字面量显示（`null`、`true`），不仿真 Python 的 `None`/`True`/字典 repr；字符串和整数值与 Python 相同（A23 复核登记）。
- A23 复核 F2：主机 prepare 失败时 Agent 返回的文字也使用捕获的凭据种子脱敏（Python 不脱敏这条消息里的用户凭据）。

## batch-7 D2 — IPv6 验收与已忽略通知的一次性迁移

协调者确认：切换后需重新做一次 IPv6 验收。旧报告的 Python 摘要与 Go-native digest 不同，
旧报告保留并显示陈旧；新 Go 报告在输入未变时继续有效，读取不会改写旧记录。
切换后已忽略的通知会重新出现一次；以 Go 原生指纹再次忽略后，同一事件不再出现。
这两项必须进入 **O12 发布说明**，提醒用户重新验收并确认一次重新出现的通知。
精确 baseline 仅登记旧验收报告的 message、checks[0].details 长度/首项，以及通知 items 长度；
没有扩大到其他字段。回归见 `TestD2VerificationReportMigration`、
`TestNativeD2DismissalReappearsOnceAndRouterStateIsPreserved` 和 subscription 专项迁移用例。
### N8-9 rules — R5 / D2（协调者，2026-11-18）

规则匹配、policy验证和缓存声明使用model typed读取。错误类型字段按R5回退，
不再仿真Python的容器truthiness、None/True等显示文本；正常输入的输出继续逐字比较。
规则远端缓存摘要改用D2原生JSON；600条原始缓存名档案及114条policy档案保持不变，
另有真实Python typed-input投影和逐行独立D2预期，未空跳过差异类别。

O12发布说明：远端规则缓存参与生成时的离线兜底。切换不能仅丢弃旧摘要文件；
旧摘要只作一次性兼容读取地址，在当前声明认领且内容通过校验后迁入native名称。
拟议编辑、旧声明或invalid receipt均不得借用该旧文件。实现及回归闭环见
`progress/n8-9-rules.md`（本项实施中，最终交回需补齐验证）。

N8-9规则缓存O12闭环：`rulefetch.ReadCache`对当前声明下的旧摘要文件校验内容，
repo事务里再次核对声明及旧/新invalid receipt，保存native文件且不提交DB/revision；
旧文件保留供回滚。离线刷新可继续使用迁移后的有效旧内容。
`TestNativeCacheMigrationPreservesOfflineFallback`、拒绝旧声明/坏内容/receipt的负控，
以及真实Runtime下载后关机/请求断开测试覆盖发布切换风险。

N8-9下载诊断R8：重定向Location为非UTF-8时仍拒绝，不再创建Python
UnicodeDecodeError，改为原生ErrRedirectEncoding（`redirect Location is not valid UTF-8`）。
只改变内部英文异常类别/文本，响应拒绝及loopback请求transcript不变；原Python完整诊断另有断言。

N8-9候选读取R5/D2：候选源列表及排序的读取按model schema回退，排序消费DTO的缺失type/id
为字符串零值；数字文本使用已批准的model.String codec。新写请求仍严格验证数组、ID和排序项，
不能借回退绕过校验。provider私有副本的path由Python repr(tuple)摘要切换为原生JSON摘要，
不改变命名冲突处理、URL、过滤表达式、引用顺序或其他provider字段。真实Python的1500条
候选读取及688条lower完整文档对照有独立R5/D2预期，原输入/原档案保留。

N8-9 refresh收口（R5/R8/D2）：interval与health count统一使用共享model.Int；非对象任务回执、
非列表或混入非对象的health history按schema回退。调度继续复用taskstatus.Count，拒绝非ASCII
计数文本及非空容器；非有限JSON不能成为有效回执。31条R5差异由真实Python对typed输入
重录，另有2条原生诊断（fs.PathError、repo.Error）逐条固定预期。原1210条refresh档案和
3150条记录不改，未变化的行仍用原期望。超大interval的无上次运行/失败退避短路、空URL
注入回调、受控Dialer错误类别等实际回归已修代码，没有登记为有意差异。

D2写入的subscription health、transport与nodes缓存仅JSON成员顺序/空白可以变化，
完整JSON值继续相等；比较器负控包括URL、node ID、数字类型、字段删除及非owner文件。

O12规则缓存迁移补充：原生文件必须保留旧cache mtime，迁移不能延长 freshness；在目标
file锁内确认不存在才安装，已有新文件不覆盖。readonly snapshot中仍可使用通过内容和
声明校验的旧缓存，暂缓迁移至下一次可写读取；迁移写失败不会破坏离线读取，真实关机
取消仍传播且不得继续写入。不会修改DB/revision，也不会删除旧文件，便于回滚。

N8-9 D2 provider私有路径的跨包对照：convert的独立真实Python参考只替换49条archive行
中的精确provider path；39条当前正常生成/下载行直接验证，原有N7/R5错误路径继续完整比较。
生成文件内容、URL、过滤表达式、顺序、host与节点语义均不得借D2豁免。首次Go生成/推送会
引用新的私有provider文件名，与既定D2一次性重新发布计划一致。


## N8-9 DNS — 原生字段消费与 OxiDNS 动态设置

- MosDNS 读取 typed model 的 R5 字段，不再使用 Python truthiness、任意类型 ID 相等或私有整数/字符串转换。正常配置的生成语义、文件名、规则顺序和 provider 鉴权不变。空/非字符串能力 token 不能经数字转字符串获得权限；退休 token 只作为重建已知生成链接的证据，不新增公开入口权限。
- MosDNS profile URL 路径段用 `url.PathEscape` 编码；畸形非 slug 的历史 ID 不再原样插进 URL。URL query 仍保留协议字段及顺序。数值/错误形状的旧元数据按共享 R5 消费；原录制不改，以独立 typed 输入投影调用真实 Python、比较完整输出。
- OxiDNS 原有私有 YAML 标量解析器被共享 yamlx 替代。`0123` 是八进制 83，timestamp/binary/未知标量的 JSON 边界是原文本；所有读取仍执行共享别名预算、安全字段名及 native 文档大小/深度/有限数校验。源 YAML 原文不因读取而回写。新原生结果单独登记，N7 的冻结历史语料不修改。
- 协调者裁定：OxiDNS API 写入时，`custom_matches[].enabled` 必须是 bool；`ros_route.args.routing_table/gateway4/gateway6` 必须是文本。错误返回 400 和集中中文消息，不提交 DB 或文件。原 Python 接受的非原生类型按精确输入指纹单独登记在 `internal/oxidns/testdata/native-request-expectations.json`；原 C05/随机录制不改。
- 已存储的上述值按兼容投影读取，不使整份 DNS 生成失败、不改原始字节：enabled 的数字 0/1 分别取 false/true；数字路由表/网关取原数字 token 文本；无效容器关闭这条 RouterOS 路由功能。其他非布尔 enabled 无效并停用该匹配。每个问题产生不含值的 `model.FieldIssue` 和路径告警。数字转文本会使生成 YAML 从数字变为字符串；无效 RouterOS 容器从 Python 的错误类型参数变为不交付该功能，其余输出由真实 Python 投影逐字段对照。用户编辑保存时应提交 bool 和文本以修复旧值。
- 补充原生诊断：布尔 false 不再作为数值零触发第二条 range 错误，错误形状的日期列表只由 schema 报类型错误；完整其他诊断和顺序不变，52 条精确输入结果另存 `native-validation-expectations.json`。UDP 的非文本 entry 不再被用于自动创建 TCP，2 条完整结果另存 capability 索引，并有真实 Python 的错误可编辑状态及正确文本正控。

### N8-9 MosDNS：原生规则文本空白与大小写

MosDNS 的经典规则关键字、域名模式、规则内容不再导入 pystr；首尾空白用 strings.TrimSpace，大小写用 x/text/cases 的语言无关完整映射。真实 Python 对照确认 ß 关键字大写、土耳其 i/ı/İ、域名中的 İ 和最终 sigma、NBSP、普通域名规则输出不变。

明确差异逐项登记：ParseClashRuleValue 的 U+001C、U+001D、U+001E、U+001F 首尾字符在 Go 中保留，Python strip 会删除。例如每个控制字符 c 的输入 c+"DOMAIN,example.com"+c，Go 返回 [c+"DOMAIN", "example.com"+c]，Python 返回 ["DOMAIN", "example.com"]。完整转换里 U+001C/U+001D/U+001E 原本就是共享换行语法，输出仍相同；U+001F 不是换行，输入 "DOMAIN,example.com\x1f" 在 Go 输出 "full:example.com\x1f"，Python 输出 "full:example.com"。15 个真实 Python 精确正负控覆盖上述每项；冻结语料不改。用户应移除规则首尾控制字符，普通空格、制表符和 Unicode 空白按原合同处理。

### N8-9 MosDNS 请求边界（协调者裁定）

前端 Generate.vue 的 cache-size/cache-ttl/cache-dump-interval 使用 v-model.number，加载时 Number(...)，Switch 绑定 bool；保存 /mosdns/settings 时直接传 draft 的数字/布尔。严格 ASCII 十进制整数字符串仍由共享 model.Int 接受（HTTP 正控使用 "128" / "60" / "20"）。整数的 60.9、true、超 int64 值、" 1_000 "、"+60"、"60.9"，以及 bool 字段的 0，Go 400、不写 DB 行/revision/业务文件；Python 的缓存接口会自动转换或回退默认并返回200。每个向量使用独立合成夹具，真实 Flask/Go 逐项断言，避免拒绝写入造成后续 revision 级联差异。

媒体类型非 JSON、畸形 JSON、JSON 数组以及非 cache 接口的 null，Python 旧路由内部异常500；Go 返回明确的400，无写入。cache-settings 的 JSON null 特例继续与 Python 一致：恢复默认、200、正常递增 revision。rulesets 的不存在引用由 Python500改为Go409，保留同一中文说明，增加 usages；两边均不写入。比较器和 proddiff baseline 不改。

原接口允许 custom_matches 中的字符串元素原样保存、显示，生成时忽略；现由 model.MosDNSMatch 的字符串/对象 union 保留该合同。DeviceNotes 的 R5 消费值不能充当写入合法性的证据：写入前使用原字段类型诊断和单独备注字段的原始成员验证，无效 null/[]/bool 继续与 Python 同样拒绝；不使无关 OxiDNS 生成失败。

上述 MosDNS 缓存 bool 严格校验包含显式字段 null：cache_enabled/cache_dump_enabled 的 null 和 whole-editor 的 cache_enabled:null 均为 Python200、Go400、无写入。它们与整个 cache-settings 请求体为 JSON null 的有效默认重置（两侧200）分别测试，不能混用。

## MCP #39 — typed 请求和凭据权限边界

- 管理凭据必须是原始JSON字符串，不能由model的R5零值/数字文本推导授权；损坏的
  config_token拒绝配置令牌认证，仍可在启用登录时使用有效JWT。0/false/空容器不被视为匿名配置。
- MCP arguments/params及工具字段采用原生DTO，非法false/0/空数组不模拟Python的 `or {}`；
  非有限JSON拒绝为400，内部异常返回固定中文，不复刻Python异常文本。
- 路由段ID拒绝分隔符和点目录；Oxi对象递归merge最多128层。正常工具、profile、权限、
  partial/reorder/事务和内部凭据脱敏合同不变。详细边界与真实Flask证据见
  [MCP交回](progress/mcp.md)，待Opus HR复核。


## O9 第一轮安全修复（O12 发布说明）

- **O9-H1**：跨域请求不再被接受（不提供 CORS 许可头），同源前端不变。只对 Origin 与请求自身 scheme、host、端口一致的请求添加 CORS 头；没有 Origin 时也不再发送 `*`。这只改变响应头，不拒绝请求、不改变状态码或响应体。反代终止 TLS/改写 Host 时，面板继续正常处理请求；不信任 `X-Forwarded-*` 来放宽 CORS。
- **O9-M3**：`JWT_SECRET_KEY` 未设置、等于 compose/安装文档示例占位值或少于 32 字节时，启动生成随机密钥；仅在用户名和密码都已设置、开启登录时记录不含密钥的 WARNING（复核 R5）。开启登录的部署使用占位值或过短密钥时，每次重启都需要重新登录；至少 32 字节的正式密钥行为不变。安装说明要求生成并保留正式随机密钥。
- **O9-M3 复核 R6（固定 IP）**：`JWT_SECRET_KEY` 还签名固定 IP 操作的 10 分钟预览确认令牌。原短密钥或示例占位值升级后被替换，升级及此后每次重启都会使此前待确认的令牌失效（未设置密钥也有此重启影响）。提交尚未确认的固定/取消固定操作返回 409「固定 IP 预览无效或已过期，请重新预览确认。」，验证失败发生在路由器写入之前；已生效 DHCP 静态绑定、设备记录和客户端联网不受影响。即使未开启登录，也应生成并保留至少 32 字节的正式随机密钥；首次换密钥后，如开启登录则重新登录，重新载入来源设备，再重新预览、确认所需操作。
- **O9-L6**：开启登录的受保护请求先认证，再解析 profile；未登录时一律 401，无 `X-ConfigFlow-Profile`。Python 对不存在/非法 profile 原先先返回 404/400；已登录、公开入口和关闭登录时仍按原规则校验 profile。
- **O9-L5**：Edge 响应与 App 的文本响应统一增加精确的 `X-Content-Type-Options: nosniff`，防止浏览器把文本下载猜成可执行内容。差分仅允许该头从缺失变为 `nosniff`；其他头和内容继续比较。
- **O9-M1/L1/L3**：后端监听保留默认地址，共用 Edge 的 1 MiB/96 MiB 请求体上限；已知长度和 chunked 超限均 413。访问日志 URL/Referer 脱敏；内部调用头改用常量时间比较。



## O9-H2 / O12 — 开启登录后的 Agent 首次注册授权

开启登录后，手工拼接的安装命令不能再注册，需要用面板生成的命令或设置注册密钥。
首次注册必须带注册密钥、面板 JWT 或面板生成的一次性注册凭据。普通 Shell、AIO、Docker
安装 API 自动生成每个服务独立的 24h 凭据，绑定名称、服务、端口、部署方式、profile；
指定 Agent IP 时还绑定 host。成功注册与消费在同一 repo system 事务提交，重启、并发、
删除 Agent 后都不能再次兑现；最多 100 个未消费身份，生成/成功注册时清过期项；
名额仍不足时淘汰最早签发的未用凭据，新命令仍能生成。失效命令返回中文重新生成／注册密钥提示。
缺少、错误、过期、重放或身份不匹配均明确提示“请在面板重新生成安装命令或设置注册密钥”。
已有 Agent 仍须出示自己的 token；JWT/注册密钥不能接管已有身份。未开登录且未设注册密钥时
保留匿名首次注册。网关与 HA 的现有独立安装会话保持各自身份校验。

D2：`system.agent_registration_sessions` 是 Go 原生新字段，Python 没有；使用 typed
`model.AgentRegistrationSession`，仅存 SHA-256 hash、绑定身份和 expires，不存明文凭据。
按协调者裁定复用仓库事务，替代网关会话的独立 JSON 文件，以消除跨文件消费/注册窗口。
不改变既有数据 schema version；旧数据无该字段可读，备份导入不恢复一次性注册会话。
安装预览/下载脚本含明文凭据，响应 no-store；日志和通知只记录字段白名单与固定拒绝原因。
未指定 IP 时仍属于一次性 bearer capability，不能防止首次使用前凭据被盗且身份被完整冒用；
文档要求保密并尽快使用。指定 IP 的不同地址和首次成功后第二台机器均拒绝。

Flask 差分只为安装脚本 JSON 的 `registration_token` 与 Docker 的
`AGENT_<SERVICE>_REGISTRATION_TOKEN` 字段增加严格 43 字符 base64url 精确规则；
注册差分的旧匿名成功样例改为两侧都带 JWT，匿名拒绝单独回归，不宽泛忽略状态或响应。

发布要求：面板必须同时提供由本次源码重建的 Agent 二进制和 Docker 镜像。旧二进制不识别
`registration_token` / `registration_key`，不能执行登录开启后的新首次注册流程；已有 Agent
持有自身 token 的运行协议不变，无须仅为此强制升级。

### O9-H2 复核补充 — 匿名注册不再暴露已有 name/host

开启登录（或未开登录但设置了 `AGENT_REGISTRATION_KEY`）时，没有注册密钥、面板 JWT、也没有与该身份匹配的一次性凭据的请求，
遇到同名同地址的已有 Agent 时返回与未知身份相同的 403（`RegistrationLocked`），不再返回 Python 的 401 归属提示；
否则公开的注册路由会告诉匿名调用者哪些 name/host 已注册。带面板 JWT、正确注册密钥或该身份有效凭据的请求仍得到原 401 提示，
且该凭据不被消费。未开登录且未设注册密钥时行为不变。差分只对 4 个具名重复身份样例断言精确的 Python 401 / Go 403 完整 JSON，
回归见 `TestO9AnonymousRefusalDoesNotRevealExistingIdentity`。

### O9-H2 复核补充 — Docker 成对输出、容量和凭据可见范围（R2–R4）

前端只请求一次 `GET /api/agents/docker-agent-compose?format=both`，返回 typed
`dockerInstallCommands` JSON 的 `compose`、`run` 字段。这是 Go 原生可选扩展（Python 没有）；
不带该参数的既有文本 API 不变。两种格式共享每个服务的一次性凭据，AIO 每次只占两个名额。
仅登录管理员可签发；100 个名额满后，在同一写事务内先清过期，再淘汰最早签发的未用项，
同秒按签发顺序淘汰；旧命令失效返回 403，最新命令可注册。匿名拒绝不暴露身份存在性。

未开登录但设置注册密钥时：已有 Agent 凭自身 token 重注册无需再提供密钥；错误密钥改为中文
重新生成／设置密钥提示；新 Agent 可通过 JSON 的 `registration_key` 完成注册。
“只存 hash”限定为主库会话字段：installer store 的 24h 脚本快照含明文凭据，下载链接属于
持有者链接；Docker run 参数、`docker inspect` 环境中也可看到明文。首次成功注册后凭据失效，
这些载体必须按临时密码保管，日志和通知仍不记录明文。见安装指南及截图说明。

### batch-8 O9-M2 — 规则响应解压大小限制

规则下载及缓存刷新的共享 rulefetch 解码边界现在统一限制每个解压层及最终正文最多 64 MiB。
Content-Length、gzip/deflate 压缩大小及调用者的 streaming 选择均不能绕过；更小的
部署文件限制仍生效。超限关闭响应，返回 typed size refusal，不把部分内容写进缓存，
保留上一份已验证内容。O12 发布说明需列出这个上限；Sub-Store 专属路径另由 subs 组处理。

### batch-8 D2 — 历史 profile-local URL 缓存归属

当前 SQLite 中已保存的 profile-local URL 来源也可认领与其声明完全一致的旧规则缓存，
包括旧 rule_sets 存储形式。归属只来自当前 stored document，不能来自 captured args、
拟议编辑、已删除来源、未解析的 library 引用或不同 ID 的同名替代来源。纯生成走 Peek，
不迁移；正常 reader 在带来源 ctx 的写事务内再次核对归属，保存新文件并保留旧文件及
mtime，不改 DB/revision。O12 发布说明应与既有规则缓存迁移说明一起列出。

C28 的两个原冻结 world 不含 captured 来源的当前声明，按协调者裁定精确断言离线
拒绝（Timeout）、无 ZIP 和无缓存改动；另七条 D2 写入/迁移仅改变原生缓存地址并按原
world 保留旧文件。原 archive 不改；独立 Python 参考绑定完整 archive 和每条原行摘要，
正常 ZIP 的完整内容和全部 cache bytes 仍比较，没有通配 baseline。

### N8-9 subs/substore — 缓存边界（协调者裁定）

- 数字订阅 ID `5` 经共享 model.String/record codec 读作 `"5"`，清理时归属文件为
  `5.json`；移除仅为了复刻 CPython `AttributeError` 的旧断言。原始存储值不改写。
  `substore/testdata/n89_cache_reference.py` 对精确 model 消费投影调用真实 Python
  `prune_orphan_caches`，生成完整删除列表与剩余文件内容；实际入口负控改变一个期望文件名
  必须失败。不是从 Go 输出反推期望，也没有修改原 corpus/比较器。
- 损坏缓存 JSON 使用原生解析器；日志固定业务前缀 `读取订阅缓存失败 (<id>): ` 不变，
  后面的 CPython parser 内部原文改为共享原生 DecodeStored 的错误。原来“返回无缓存、文件
  保留”的行为不变。订阅列表/ID 损坏仍以 `检查订阅缓存失败，跳过清理: ` 开头，
  不从 R5 的 nil 列表推断“全部缓存都是孤立文件”。
- 缓存保存、删除、清理在真实 Runtime 关机后拒绝副作用；请求断开仍完成。新共享删除入口
  使用 repo 现有文件叶锁，取得锁后/最后 unlink 前检查取消；不删除 Python 遗留 lock 文件。
  主文件、备份、未知字段和有效原始缓存字节的既有测试保持。作废内容核对的 panic 日志
  只输出固定业务文本，避免回显任意 panic 值中的节点凭据。


### N8-9 subs/substore — 私有 runtime 状态文件（协调者裁定）

- 私有 `<tempdir>/configflow-sub-store-<DATA_DIR sha256前12位>/state.json` 改用共享 model
  记录和原生 UTF-8 JSON framing，不再复刻 Python json.dumps 的单行空格/ASCII 转义。
  业务字段、值、成员顺序、有效 supervisorctl 命令和副作用不变；未知成员和原数字文本
  继续按 R1/R2 保存。显式写入的字段通过 SetMemberJSON 修复，不能因值恰好为零而保留损坏值。
- 生产读取方审计：旧 `backend/utils/sub_store_runtime.py::_read` 用 json.loads；新
  `substore.loadState` 用共享 DecodeStored + model。`docker/sub-store-start.sh` 只选 bundle/VERSION，
  Node 数据目录由 `docker/Dockerfile` 的 SUB_STORE_DATA_BASE_PATH=/data/sub-store 指定；
  启动/安装脚本没有定位上述 hashed scratch state 文件，没有发现其他按字节匹配该文件的进程。
- `n89_runtime_reference.py` 由真实 Python 原样写出旧格式（包括空格、Unicode ASCII 转义及
  超过 2^53 的未知整数）；Go 读取测试检查字段/未知整数并确认读取不改写文件。
- 非对象 state 根以 RuntimeStateError 明确拒绝，不再构造 CPython AttributeError/TypeError。
  该无效根在任何服务控制前拒绝，文件保持原样。last_used 的 null/非有限数/错误类型
  不能作为可停止服务的空闲证据；被真正消费时拒绝，未消费的读取/被覆盖路径仍可工作。
  其他 R5 字符串/指针字段遵守共享 codec，而不是 Python truthiness。
- 原 review_reference.json 及 checkOutcome 比较器未变。128条逐行 native 参照由原文件哈希锁定，
  真正的 Python生命周期函数读取 model 消费视图；真实 Python writer 继续操作原文件，
  避免无关字段被投影规范化。精确期望改变一个 mode 的负控能使实际测试失败。


### N8-9 subs/substore：原生客户端边界（2026-11-19）

- 下载结果、转换缓存直接保存 typed Traffic / yamlx.Node，legacy API 仅转码。订阅临时对象
  POST 改用共享 jsonresp DTO 编码：字段、值、顺序、URL、noFlow、下载/删除命令不变，JSON
  framing 为原生紧凑 UTF-8；新测试逐字节检查 POST 和无 body 的 DELETE。
- 删除 CPython OverflowError：非有限流量计数器现在是 `*substore.TrafficError`，明确指出
  upload/total 字段，原有限数字的 float→整数协议和精度保持。精确 native fixture 覆盖
  corpus 的 `upload=1e400`、review 的两条 total 溢出；只变错误类型/引擎文字。
- CPython 特有 U+001C 不再当 Unicode 空白：`total\x1c=1` 无合法流量字段，返回 null。
  标准 Unicode 空白、有限计数器和其他 sibling 字段不变。精确 fixture 列明该唯一 corpus 项。
- typed proxies 是对象数组。旧引擎把空字符串/空映射当空结果、把 null 节点跳过的私有
  动态行为不再保留；显式拒绝并在中文消息带原值。native fixture 逐项固定 corpus
  147/148/151/152/155；其余 corpus 和 N7 原始 YAML 参考及比较器不修改。proxy type 仍为
  严格身份字段；name/server 使用共享 String，port 使用共享 Int，开放 Params 不改原 tag。
- 关机取消不再形成共享下载的上游失败 cooldown，不再写入转换结果；等待同一 flight 的
  请求可立即响应 Runtime 关机。请求断开仍完成原工作。真实 Runtime 回归覆盖 fetch/convert。


### N8-9 subs/substore：原生环境、HTTP JSON 和在线更新（2026-11-19）

- 删除 product 中的 pystr/pyInt、模拟异常类、CPython UTF-8 错误位置、os.makedirs / shutil
  异常原文及动态 release 字段操作。原生进程错误保留 `%w`，关机取消可 errors.Is；非法
  UTF-8 输出是 ErrCommandEncoding，非法 VERSION 是 ErrVersionEncoding。正常 argv、
  超时、返回码、换行转换、文件和重启/回滚断言不变。只改旧引擎错误文字/类别断言。
- 标准 Unicode 空白不包含 U+001C：原 `NBSP+3001+U+001C` 与 corpus 的 `3001+U+001C`
  明确拒绝为 PortError。其他合法 ASCII 数字、前导零、标准前后空白和端口范围不变。
- version 是原生十进制 dotted 协议，使用 big.Int.SetString，不再复制 Python int 的 Unicode
  数字/underscore/分段空白和4300位限制。精确 corpus 边界逐行登记：`２.４２`、`2. 3`、
  `2.NBSP3`、`2.U+00853`、`2.1_0`、`𝟘.1`、`٣.٤`、`U+001C2.1` 无有效版本；对应
  `is_newer([２.43,2.42])` 为 false。4301位十进制分量保留原精度；fixture 用完整十进制文本
  比较每个 native 分量，避免旧 jsonx parser 自身的 CPython 位数限制。其余版本字节/比较不变。
- Release/asset 使用共享 model codec。错误 root/assets/body 形状是 ReleaseError，检查更新
  的固定中文业务前缀、最后一次成功结果、正常字段/值/顺序/TTL 不变。数字 digest:5 按已定
  String 规则读为文本 "5"，没有 sha256 前缀，SHA256 为空，精确测试固定完整 Release 零值。
  更新内部 panic 不回显内容，保留“更新失败：”并给固定中文内部错误；原重启失败诊断类别
  从 RuntimeError 改成 native errors.errorString，回滚效果及后缀不变。
- 原 malformed node 参考三项精确列入 native fixture：review parse row21 的0节点明确拒绝，
  不再把空对象当成功结果缓存；review nodes row18 的 name:1/server:2→文本，port:"x" 的 typed 读值为0但保留原始类型证据；
  row19 的 type:null 按字段不存在处理，输出空 type。有效输出五个字段及 Params 不改。
- 取消闸门覆盖写下载文件、复制备份、替换 bundle、写 VERSION、文件 mode/time、重启、
  回滚。关机后停止后续修改，不扩大不可中断范围；仅清理本次临时下载文件并更新进程内
  terminal observation。已经替换 bundle 但还没写 VERSION 时保留旧 VERSION 和完整备份，
  不再重启或破坏恢复证据；请求断开仍完成全部更新。真实 Runtime 的三提交点×两种取消
  及入口关机回归验证这些状态和最终锁释放。


### N8-9 subs — 监听器、筛选与订阅 Base64（2026-11-19）

- 监听器核心使用 model.Profile / yamlx.Node；保留原有 yamlx.Edit、alias/anchor 分离、
  原文与注释、错误定位、路径顺序及不安全编辑拒绝规则，旧入口只转码并回写唯一拥有的
  custom_config 字段，不重写其他字段。
- 订阅筛选按 model.Subscription 读取，一批只编译一次正则。节点名的数字文本兼容保留；
  bool/容器/null 名称按 R5 的空文本消费，不再用 Python repr 构造节点名。返回的是选中的
  原始节点（未知字段、原值、顺序不改）；numeric regex_filter 通过共享文本规则消费。
  `n89_filter_reference.py` 在精确消费投影上调用真实 Python，然后把选择位置映射回原始
  记录；9条变化有 source/row SHA 和完整期待，原 corpus/比较器不改。严格请求 token 校验
  `节点筛选正则必须是字符串` 和固定中文日志前缀不变。
- **有意保留兼容规则，非行为变化**（协调者明确裁定）：外部订阅源的 Base64 必须继续
  宽松读取。decodeSubscriptionBase64 忽略非字母表 ASCII（空格/换行/垃圾）、忽略尚不能
  完成末尾量子的多余 padding，完整 padding 后停止；非 ASCII 和不完整末尾仍失败。
  前置清洗后由 encoding/base64 完成解码，删除旧手写 bit decoder。2500条原 helper 语料
  及3714条实际 URI 语料保持原期待，不改为 strict、不扩展无 padding 编码的接受范围。


### N8-9 subs — typed 订阅解析及类型证据（2026-11-19）

- 新 ParseList/ParsedProxy：URI 导出 model.Node；结构化对象保留 yamlx 图及 typed 读视图。
  Base64 保持明确批准的宽松兼容，原2500条 helper 语料不变。订阅不读任何全局配置；
  VMess 只用标准 JSON 选取十个协议字段，再进共享 model codec，重复字段继续取最后一个，
  原始 null 根不是对象、不生成节点。未知的开放参数不因 typed 迁移变成空值。
- VMess port/aid 是外部协议整数：标准数字、共享严格带引号整数和整数字值的浮点数可用；
  bool、fraction、overflow、错误容器/文本均拒绝该条 URI，不把坏端口悄悄当0成功。字段缺失
  使用原来的0。原始 URI 的端口使用 strconv 十进制，保留原有正负号、前后标准空白和前导0；
  CPython 下划线/Unicode 数字以及超过int64的端口不再接受。整数范围外的端口也无法用于网络。
- Node 的 name/server 使用共享文本读法：有效数字无损转文本，null 按字段不存在处理。
  **错型 bool/容器的原 token 和 FieldIssue 保留**，让 Surge/Loon 继续按所属 Node ID 明确拒绝，
  不把已有业务错误改成生成空 server 的“成功”。开放 Params 中 uuid/cipher/network/path/host
  等值不在解析器里强制转字符串或替换 null；下游 typed option owner 使用自己的 schema。
- 同样修复 substore.ProxyNodes 的重构问题：原来新建记录会丢掉 server:[] 的 FieldIssue，
  让已有 `skip-failing-node` 分类和拒绝失效。现在 name/server/port 的错误 token 保留；
  有效 name/server 兼容文本及 port:"443"→443仍保持，Params 原 tag 不变。既有8c的306条
  boundary reference及其分类索引原样通过，未修改任何8c期待或比较器。
- `n89_parser_reference.py` 对冻结输入调用真实 Python 业务，仅注入已审核的共享整数/文本
  消费和原生 URI 数字规则；YAML复用原N7独立 loader值，不取Go业务结果。795条已有输入的
  变化有逐行SHA、具体field/class、完整输出，21条旧私有 int helper 转为实际 URI 整数边界。
  这不是扩充输入矩阵。冻结 corpus、N7资料和比较器未改；一次完整输出负控确实失败。
- 开放 JSON 的 `1e400` 和大整数 token 仍保留，typed Port 溢出为0并保留问题证据；
  JSON不接受的 NaN 文本按既有开放 YAML 原生规则成为字符串“NaN”。独立回归固定完整原文。

### N8-9 subs — native DNS 图与协议文本（2026-11-19）

- ApplyDNS 消费固定 typed DNS 字段，开放 hosts/policy/filter 保留 YAML 图。DNS listener 端口、
  semantic version 使用 ASCII 协议数字，不再接受 CPython 的 Unicode Nd：冻结 DNS row919
  `1.2.3.4:٥٣` 明确返回 listen_unparsed，row955 `v١.١٩.٢٠` 是未知版本。两条期待由真实
  Python 确认旧值并逐行绑定 SHA；其余4767条原期待及比较器不变。
- 固定 String/Int/bool 的已定 R5 边界同样用于 DNS 与 discovery 状态；无配置/缓存的每节点
  转码。rule 模式的 fake-ip-filter 必须为列表，false/0/空字符串/映射不能证明是空名单，
  因此保留整个配置并给现有 filter_not_list 中文提示，不再覆盖这些用户错误值。
- DNS 写入先完成计划与验证，再克隆拥有的 DNS 子树；不通过共享 alias 改动用户其他字段。
  hosts 索引每次生成构建一次，null 的末尾 host 值维持原“目标域名尚未解析”语义。

### N8-9 subs — typed hosts 元数据、合并与 D2（2026-11-19）

- ExtractHosts 直接读 YAML 节点并生成 HostsData，保留 entries/total/skipped_count/skipped
  四个输出字段，包括显式0、空映射/空列表、空 domain。只提取顶层 hosts，不被订阅其他坏段影响。
  合法 YAML 的 CR/LF/NEL/U+2028/U+2029 行边界保持；CPython splitlines 特有的 VT/FF/
  U+001C/U+001D/U+001E 不再把控制字符悄悄删成新行，交给原生 YAML 拒绝。冻结语料中
  19条这种已有输入由真实 Python业务加原生区块选择独立生成完整 parse_error 期待；正常字段/
  值/顺序/文件和副作用不放宽，原 archive/N7资料/比较函数不改。
- 按 design/n7-n8.md §2.7 的 D2，现在启用原生 HostDigest：共享 jsonresp.Canonical 编码，
  标准 UTF-8/紧凑 JSON，而非 Python json.dumps 的 spacing/ASCII。69条既有 digest 输入逐项
  固定原生 SHA；HTTP共享入口的单条 hosts identity期待同步更新，并保留历史hash注释。该值
  是订阅 hosts 内容身份，切换后允许报告一次变化；不是设备生成文档或命令的字节签名。
- SourceMetadata/SubscriptionCache 是 subs 的只读消费 DTO，持久化 owner 仍为 substore.Cache。
  一批缓存节点逐个进入共享 model.Node codec，坏的非对象 sibling 保持跳过，不把整批有效节点
  丢掉；没有重写/清理缓存文件。HostIndex 保留 absent/null/错型 entries 的“无可用索引”区别，
  不制造可用的空集合。URL一致性、共享 String/Int/bool规则、未知字段原始存储均保持。
- MergeHosts 直接消费 model.Config、typed source index 和开放 YAML 图；每个订阅只取一次索引，
  不在节点/订阅循环重解整个配置。只克隆并替换拥有的 hosts 容器，不修改其他 alias。缺失
  必需 owner 明确报 CollaborationError，而非把没检查的数据当成不需要合并。

### N8-9 subs — 外部 dialer 元数据与交付（2026-11-19）

- **有意保留兼容规则，非行为变化**（协调者明确裁定）：外部机场 proxy-groups.type 只接受
  select/url-test/fallback/load-balance，其他值（含数字/未知类型）通过 airportGroupType 回退
  select，确保可用中转仍能选择；这不放宽用户存储的 model.ProxyGroup.Type 或 Node.Type。
  新回归同时证明机场数字 type 可用、用户存储数字 type 仍被唯一共享校验器明确拒绝。
- ExtractDialers 直接读取 YAML 图，节点名称使用共享 model.String。proxies/proxy-groups 及
  实际访问的组成员必须为列表/null，不再把错误字符串逐字符、错误映射按键迭代。11条原有
  畸形输入由真实 Python 业务+已审核 N7 loader值的 typed 消费投影生成精确原生期待，绑定
  source/row/loader SHA；明确 SubscriptionShapeError 类型和中文字段路径，不回显整份含
  凭据的订阅。原语料及比较函数不变；独立回归逐条核对原生类型和完整错误消息。
- 实际组遍历每个 target 只访问各子组一次；重复 DAG 边不再造成指数展开，保留首次叶节点
  发现顺序、去重、50 targets/100 members 上限及 unresolved。30层重复子组回归通过。
- AdaptProxies 返回原始开放图节点，只在需要拥有 dialer-proxy 替换时克隆该节点。使用共享
  yamlx.Encode 检查图/alias预算而非把整个开放参数强制转 JSON，保留合法非字符串 YAML键。
  同样调整已迁移 hosts/DNS 克隆前的图验证；新增非字符串开放参数和原记录不变回归。

### N8-9 subs — typed 中转组生成（2026-11-19）

- AddGroups / DialerInputs / DialerReport / SourceDialers 直接消费 typed 配置及缓存，引用解析走
  convert 唯一实现。metadata.dialers 的缺失与显式 null/错误值保持区别：后两者不回头读取
  downloaded 缓存。记录存在但没有可确认成员（空、错型 members 或错型 target）时，不再
  生成空中转组；保留该节点 missing 提示并暂不交付。这修复 Python 在损坏 metadata 上构造
  空 group 的问题。有效源/成员/顺序不变；未知数据不被当作有效空集合。
- 缓存健康字段按声明的 String/Int/bool消费后写入生成图，metadata原始字节由 model保留；
  开放节点 Params 不做标量强制改写。Provider 名称字面量过滤使用 regexp.QuoteMeta 加
  既有 portable punctuation 转义，保持生成 filter 的字节和反引号排序语法。
- 业务名称冲突仍保留已经生成的代理前缀；本次未生成的 group 不伪造成功。Source cache 与
  model.Config 不修改。Runtime关机在读缓存返回后立即阻止继续转换和图提交，取消保持原样。

### N8-9 subs — 唯一 typed 链 owner 与旧私有 helper 删除（2026-11-19）

- LowerChains/ChainProviders 只消费 model.Config/yamlx，dialer 校验、图、成员解析、过滤和结构化
  proxy_string 使用 convert 唯一共享实现。删除 subs 的私有 compat.go、pyconv.go、旧 chains.go、
  yaml_reader.go；legacy*.go 仅在旧入口转码。固定业务错误及正常生成字节不变，不再复刻
  CPython repr/AttributeError/动态 ID 相等。缺少 shared collaborator 明确失败。
- 既有 shared R5 边界现在同样适用于旧 subs 入口：数字 proxy_string/ResourceRef.ID/组列表文本
  用共享 String；错型整体 nodes/subscriptions/aggregations、列表、follow_group 消费零值但不改
  原存储；params:null 按缺失；include-all 只收 bool。坏 dialer_ref 不得因 R5 变 nil 后漏掉业务
  校验，仍由共享 owner fail-closed；错误保留实际原 JSON。结构化 type 为非字符串不可用，
  YAML 标量键及非有限 JSON 后转 YAML 的规则跟唯一共享 owner 一致，不保留私有旧解析分支。
- n89_chain_reference.py 读取冻结输入、调用真实 Python 业务并应用已批准的 shared R5 读视图，
  对656条已有输入逐项记录类别、原输入 SHA 和完整 native 输出；原 archives、N7 oracle、比较
  函数均不变。具体类别逐项列在 fixture.classes（19类，含正常 ID 数字文本及明确错误）。
  泛化的历史 Python 内部异常仍沿用原先“必须报错”的约束，不把真实 DialerReferenceError 放宽。
- URI query 解析使用明确的 ParseQueryOptions：保持重复参数顺序、空值忽略和分号作为值；PSL
  输入使用 strings.TrimSpace，不复刻 CPython C0 空白。既有 URI/base64/registrable 语料继续验证。
- 新增 ProxyGroup.SetJSON 复用共享 SetMemberJSON；只替换拥有成员，保留显式 null/空列表和新增
  成员顺序，不改未知大整数及输入副本。链私有 ID/名称/排序/provider 复制及中途错误的成功前缀
  均保持。与未迁移 rules/provider 的边界不复制业务；convert 仅新增订阅 typed 接线文件。

### N8-9 substore — 原生临时根目录选择（2026-11-19）

- 删除残留的 tempfile.gettempdir 仿真及写“blat”探测文件。runtimeTempRoot 保持显式
  TMPDIR/TEMP/TMP 优先级、绝对路径和既有有效 state.json 的位置；无显式配置使用 os.TempDir。
  不可用的显式目录由实际写入返回错误，不再悄悄退到其他目录并启动第二份 runtime。
  目录选择本身不写文件。旧 Python 原样 state.json 仍由读取回归验证；正常命令/副作用不改。

### N8-9 subs — 跨包调用边界收口（2026-11-19）

- Python bug 修复（协调者裁定）：EasyTier 的 Agent 门禁对非空但无法解析的 coreVersion
  fail-closed，固定中文消息“Agent 上报的内核版本无法识别：<原值>，请升级或重启 Agent”。
  不恢复非ASCII机器版本数字；空/未上报/null仍走既有unknown路径，非Agent路径不扩大限制。
  冻结 review_c12 row2125（v１.19.30）绑定完整原输入、真实Python旧错误和明确native错误；
  正常ASCII版本和生成字节不变。新增回归检查拒绝发生在修改文档之前。
- 已批准的私有引擎错误替换：Surge/Loon的22条既有 proxies_* 数字type输入，Python原为
  AttributeError，Go明确 ProxyShapeError：“节点 type 必须是字符串，当前值：5”。
  由真实Python重跑确认旧错误，逐行输入SHA及完整native错误固定在独立caller fixture；
  原archive/比较器/8c分类索引均不改，其他错误类别不放宽。4条版本项与其共26条精确参照。
- 修复本次迁移引入的两处调用遗漏：ParseProxyNodes只投影实际消费的proxies子树，保持
  无proxies但有无关复杂YAML键时的原空列表行为；DescribeFailure对无cause的
  SubscriptionFetchError保持“更新失败（SubscriptionFetchError）”，不泄露Go包名到固定业务文案。
  两项均有修复前失败的原跨包语料和专门回归，未改其业务期待。

- 上述Agent版本新裁定还覆盖冻结corpus/34045、35971与review_c12/2121的alpha-*版本；
  和row2125共4条，明确在任何图修改前拒绝，完整未修改图进入独立native期待。
  MosDNS三份Python参考只刷新已删除/新增代码的依赖元数据；2863/0/5个既有exact entries、
  声明输入hash及venv依赖hash均与git原文件相同，未修改Python业务答案。

### N8-9 subs — Agent未知版本生成边界与原生生产接线（2026-11-19）

- 协调者批准的版本门禁同时影响Mihomo生成：mihomo_corpus的12条既有输入、review_mihomo/471。
  使用真实Python完整生成器，仅注入已批准的“非空未知Agent版本拒绝”守卫，捕获完整错误、
  部分report与逻辑profile状态；逐行/source/原字节清单SHA绑定。正常版本不变。
- 原4548项字节manifest完全不改。其中9项含alpha-abc机器版本，现在单独逐项断言完整
  EasyTierSettingsError、原baseline SHA和空输出；其余4539项仍逐字节比较。Mihomo仍保留
  原2353项覆盖计数（2344字节比较+9明确拒绝），Loon1064、Surge1131及既有8c例外不变。
  没有全局比较器豁免，没有从失败的Go输出重录字节期待。
- 临时目录原生环境边界已获协调者明确批准：首个非空TMPDIR/TEMP/TMP选定后不回退、不写
  探测文件。首次实际使用检查目录存在/类型/写入与遍历权限（含内核ACL/只读挂载检查），
  RuntimeDirectoryError明确显示变量名、绝对路径和中文原因，并保留原IO错误链。内置运行时
  启动接线即发告警；未启动/关机后的写入仍按原取消闸门拒绝，不因诊断创建新目录或state。
- 生产mihomoenv.NativeEnv直接接substore.LoadCache与NativeConverter，移除这两条已迁移owner
  的JSONx往返。活跃Default仍按调用时解析；Runtime关机错误不会在旧无error转换签名中丢失。
  provider/refresh未迁移的回调仍只走原owner薄codec，不复制其业务。

- N8-9 最终HTTP对照修复迁移回归：旧筛选facade曾把选中节点重新编码成副本，打断聚合待
  提交缓存与所选节点的引用，导致后加的enabled字段未落入缓存。现在只映射native选择结果
  回原始对象，保留重复引用、原顺序与未知字段；原聚合缓存字段/副作用期待不改。
  专用身份/未知字段回归与原聚合HTTP用例修复前失败、修复后通过；六组最终HTTP全绿。

## N8-9 subs/substore independent review: response and subtree budgets (2026-11-19)

- Close the substore portion of O9-M2: the production subscription/Sub-Store HTTP client now
  refuses wire bodies and every decoded gzip/deflate layer above **64 MiB**. This also bounds
  intermediary decoded layers before another decoder consumes them. The failure is a
  `ContentDecodingError` wrapping `substore.ErrResponseTooLarge`, with the Chinese diagnostic
  `订阅或 Sub-Store 响应内容超过 64 MiB 上限`; it never returns a partial successful response.
  Python's formerly unbounded fetch is intentionally restricted. Exact-limit bodies remain valid.
- A parent graph passing yamlx's alias ratio budget does not establish that a copied child passes
  the same policy in isolation. Provider proxy records, DNS policy and rebuilt fake-ip-filter now
  validate the exact subtree before Clone. Excessive aliases return the shared `ErrAliasBudget`
  rather than silently substituting null and reporting success. DNS rejection leaves its source
  document unchanged. The shared alias budget and all ordinary generation expectations stay intact.
- Evidence, failures before each fix and targeted checks: `review-n8-9-subs.md`.

### N8-9 routers / routersplit typed boundary

- 用户写入的 type/endpoint/username/password/Agent引用必须为文本，verify_tls 必须为 bool；name 使用共享 model.String（数值保真文本，bool/容器按 R5 缺省），存储检查间隔使用 model.Int，HTTP 请求仍严格要求 JSON 整数（字符串或60.0均拒绝）。规则选择是身份列表，不能用数值自动造出规则 ID。原 R01 冻结输入/答案不改，另列725条完整逐行SHA绑定的独立 Python 期望；原生变化不复用 Go 输出。
- 请求中的 `ip_rule_sets` / `ros_policy_rule_sets` 为 `false`、`0` 或 `""` 时返回400；Python原先按空列表处理。存储同类错型也必须显式修复，避免无关编辑清空设备路由选择。坏守卫配置存在原启用开关或所有权 state 时，probe/routes/fixed-IP/IPv6 等设备操作也会提示修复后再执行（保守拒绝）。
- 存储读取使用共享 model；坏选择列表在无关改名时明确400而不是写空，校正后的PUT和DELETE仍可用；损坏守卫有所有权记录或原启用开关时阻止设备操作，不能退化为撤除。运行时默认不将无关的缺失字段物化，显式赋默认仍写入。旧未知字段、错型成员和大整数在无关编辑后保留，Extra 删除显式使用 Clear/Fields.Delete。已发布 manifest 的坏值仍作为不透明所有权证据，不能用 R5 默认去掉保护。
- routersplit 不再生成 Python AttributeError/TypeError 或做 str/list/hashable 仿真；错型 record/list/bool 遵循 shared model，原始标记仍严格验证 version 的整数 token1 和所有字符串身份。实际使用的拓扑/绑定数值、容器、空必需身份 fail-closed，用 RouterSplitError/ROUTER_SPLIT_INVALID/409；可选非文本/null链接视为没有链接，不阻塞无关路由器；marker路径不读取旧拓扑证明。Python 的 None/空 ID、数值/bool相等不再制造关系。
- 会话 completed=1 不再被 Python truthiness 当成 true；仅 bool true 表示完成。旧 hook 单测仍使用同一数字输入，独立 fixture 同时固定真实 Python 旧答案和 R5 投影后的完整答案。没有把输入改成 true 来躲过差异。
- RouterOS/iKuai 回包属于外部输入：既有 Row.text/flag/number 的厂商字符串/数值兼容保留，设备请求编码、顺序、超时及 raw body/header 断言不改；日志/错误凭据继续用 internal/redact。导出脱敏沿既有出口合同。
- 本组没有新增 D2 设备身份/缓存签名变更；通知继续使用 N8-7 已切换的 native RouterDigest，规则同步控制摘要沿 R14 已批准 canonicalReleaseJSON。YAML 的原 N7 oracle 与列出的差异类别不变。
### N8-10 core-2 — registration profile optional DTO

按协调者 2026-11-19 的裁定，机器注册请求的 `profile_id` 仅接受字符串、null 或不传；
空串仍表示默认配置。非字符串的 boolean、number、array、object 返回 400
`{"success":false,"message":"Invalid profile id"}`，不插入 Agent、不消费一次性 proof。
旧实现把 false、0、[]、{} 静默当作默认配置，其余非字符串通常落入 500；这些无效形状
现在统一拒绝。存储的原始 R5 字段仍保留，不能把其字符串投影当所有权证明。

受支持调用方核实（没有发现要兼容的 false/0 发包方）：

- `backend/agents/go-agent/client.go:28` 的 `RegisterRequest` 及 `RegisterAgent` 的构造
  没有 `profile_id` 成员；`Config` 也没有该字段。`git log --all -G 'ProfileID|profile_id'
  -- backend/agents/go-agent/client.go` 无记录，仓库保留的历史版本没有该请求字段。
- shell 单服务/AIO：`backend/agents/go_install_script.py`、`backend/agents/scripts/install-go*.sh`、
  Go 端 `internal/installer/goagent.go` / `aio.go` 及对应 scripts 不写 profile 配置；
  O9-H2 只向已有配置添加 string `registration_token` / `registration_key`。
- Docker 单服务/AIO/HA compose 和 `docker/docker-agent-entrypoint-service.sh` 生成 Go Agent
  Config，再由上述固定结构体发包；没有 profile env/JSON 注入口。`internal/installer/docker.go`
  的 O9-H2 环境项也是 string credential。compose/入口脚本历史中没有 profile 注入记录。
- 网关独立安装会话由 `gateway_installation.py` 验证 profile 字符串后注入；Go 端同样由
  安装会话提供已验证 profile。Docker HA enrollment 的 Python / Go issue、注册入口均先
  ValidateProfileID，Go `IssueEnrollment` 的 profile 参数本身是 string；不会发 false/0。

逐行绑定原始 JSON 的独立 HTTP 回归：`TestN810RegistrationProfileOptionalMachineValues`
（8 个拒绝形状，null/空串/default 三个成功）；另验证 null profile 的真实一次性 proof
仍与 Agent 注册原子消费。原差分、corpus 和已提交的旧期望不改。

### N8-10 core-2 — explicit profile repairs a malformed stored binding

Agent 已存的 `profile_id` 为错误类型（R5，非 string/null）时，Python 和迁移前的 Go 在
计算当前 profile 时报错。现在只要没有进行中的发布或升级，带显式 profile 的请求就可以
直接写入新的合法绑定：

- 管理编辑入口（EditAgents）；
- 注册入口：已有 Agent 携带正确 token 重注册且请求给出 `profile_id` 时同样适用
  （HR 复核 L2 补登记）。

有进行中的发布或升级时仍以“Agent has an unfinished deployment or upgrade; recover it before
changing profile”拒绝；未给出 profile 的重注册仍报告原 validation 错误，不会把 R5 投影当作
当前绑定。

### N8-10 core-2 — malformed deployment status replies

按协调者裁定，Agent 部署回包的 `status` 为 array/object 时，Go 原生状态机拒绝该
上游格式错误，HTTP 返回结构化 502（`code: INVALID_RESPONSE`，中文原因
“Agent 返回的发布状态格式不正确，请检查 Agent 版本和日志。”）；不再暴露 CPython 内部
TypeError 文本或裸 500，也不把该形状静默投影为 pending。拒绝不写数据库、不改变
revision、prepared content 或配置确认记录。已有 terminal/stale receipt 的忽略顺序保留。

原 `a01_reference.json` 原文及 SHA 完全保留。只有八条已有 TypeError 行用完整 archive
SHA 和各自 compact row SHA 精确绑定，分别核验 Python 错误类别、Go 拒绝、事务不写入；
其余正常行和标量行继续原期望比较。真实 HTTP 两条回归分别发送 list/object status，
比较设备实际收到的 method、request URI、Bearer header 和空 body 原字节，断言结构化
502、脱敏与数据库 fingerprint 不变。

部署操作在原始 journal 的 `deployment_id`、`profile_id`、`service_type` 不匹配时拒绝
跨绑定的查询/激活/回滚；数字文本投影、大写别名不能作为该证明。损坏的非空 HA marker
不能放开成员单独回滚。原先并发删除 Agent 之后触发的 CPython NoneType 内部异常，
现在使用原生 endpoint refusal；原 nil 调用及 Python TypeError 原文另以完整单行 SHA
绑定，继续验证拒绝、非 transport error 和事务不写入。

成功的部署 HTTP 回包在非 object、非 JSON 或带重复 member 时同样返回上述结构化 502，
不再静默变为 pending；未知大小写别名不认作已知协议字段。注册／编辑／deployment patch
的原生 RawMessage 入口拒绝真实重复键（含 escaped 同名与嵌套重复）；旧 API facade 自身的
JSON materialization 属其后续 S 步骤，本块没有改写其 parser 或扩张准入。

### N8-10 core-2 — machine version numbers

按协调者裁定，Agent 版本来自 go-agent/client.go 的固定 AgentVersion（upgrade.Version），
当前 migration.go 常量及 Python version.py 的 LATEST/changelog 均为 ASCII dotted 机器值；
注册/心跳结构体没有版本表达式或 Unicode 数字入口。去掉 `-suffix`、不足位补 0、任一
分量无法解析时按原文本比较的业务规则保留。整数分量统一使用 taskstatus.Count，不再
模拟 Unicode Nd 或 CPython 4300 位限制。冻结 a02 的九条人为 Unicode/超长分量行因此
改变比较方向；原档案、输入输出及 SHA 不变。gen_n810_version_native.py 的独立字面期望
通过档案 SHA 和每条完整原行 SHA 绑定，其他行继续使用原 Python want。空分量仍走
文本比较，不被共享 counter 的空值语义当作 0。

### N8-10 core-2 — native legacy-state diagnostics

旧心跳库读取、坏 UTF-8 TEXT 和 unlink 失败的诊断不再重放 CPython sqlite3/OSError
English 文本。SQLite driver 错误保留原生链；无效 TEXT 仅报告列名，不回显存储原文。
原始 a02 档案与日志原文不变，七条读取失败行及两条删除警告逐行 SHA 绑定，核验原
Python 类别/errno、Go 拒绝、无部分行、事务不写入和目录不被删除。一次性扫描、主文件
必须成功删除才导入、仅更新较新 heartbeat、失败版本写入等原规则保留。

原生 MIME/IANA 文本 codec 取代私有 Python alias 与 Unicode decoder；受支持 Agent 的
UTF-8 JSON/text 及旧 UTF-16/32 BLOB 原始语料保持不变。额外 IANA charset 由原生注册表
处理，MIME 大小写按标准解析，不再局限手写的旧 codec 列表。

### N8-10 core-2 — direct Agent protocol and preparation errors

原生 RemoteRequest/CallAgent、RemoteCall、SetLoggingModel、AgentRequestModel、direct service
operation 和 legacy config owner 取代动态业务。成功的 Agent 回复如果不是所需 object、
不是 JSON、含真实重复键或 success 为非 boolean，拒绝为 INVALID_RESPONSE 中文 502；
状态/日志原本允许的其他合法 JSON 值仍透明保留。HA state 本来不要求 success，仍允许
省略；原先把 list 回复悄悄变成空成功 state 的异常路径现在同样拒绝。

原 remote_ref.py 未修改；真实 Python 原输出单独冻结为 n810_remote_python.json。
九条变动用完整 archive SHA、原完整 result/error row SHA、实际 body/status 逐项绑定。
AttributeError/TypeError/JSONDecodeError 的原文和类别保留在 oracle；仅比较 native
拒绝、错误类与事务无写入。损坏的已存 logging_config=null/list 继续拒绝且不写入，
改为明确中文原生 validation reason；未把它当作上游错误或静默修复。

CallError/PythonClass、KeyError 和 UnicodeEncodeError 的文本仿真删除。缺失/非字符串
凭据按 ErrAgentEndpoint 拒绝；Latin-1 编码用 x/text ISO-8859-1，不能编码的 header 用
ErrHeaderEncoding 拒绝。CR/LF 诊断不回显 header 凭据。原 Latin-1 字节契约保留，新增
37 条原 A15 输入/输出完整行 SHA；upgrades 仅接原生 sentinel 到它已有的延后分类层，
其四条原 encode error 行和原 archive SHA 精确绑定，其余 wire/class 期望不动。

日志设置等写操作保留请求值但脱离浏览器断线，仍跟随 Runtime shutdown；真实日志配置
HTTP 检查实际 method/path/Authorization/Content-Type/body 字节，并验证 502 脱敏和
全库 fingerprint 不变。断线可完成、回包时关机不得提交、旧 generation 不得发新请求。

## N8-10 core-2 — traffic machine domains and native IDNA validation

- 流量统计域名以 x/text cases.Lower（Unicode 17）小写，保留 final-sigma；删除私有
  Unicode14 表。相比原 A05 的 Python14：55 条 lowercase、307 条 final-sigma 行变化，
  实际域名大多为 ASCII/punycode，只有涉及这些码点的统计键变化。原 A05 输入、输出、
  archive SHA 完全保留；官方 UCD17 独立 fixture 逐行绑定，包含每条旧值反例和伪造绑定
  反例，测试硬断言 cases.UnicodeVersion == 17.0.0。工具链/发行镜像由 batch-11 独立
  升级，本分支不改 go.mod/Dockerfile。没有生产兼容表或新模块。
- Agent HTTP 非 ASCII hostname 用 x/net/idna.Registration 验证，删除私有 urllib3 码点
  范围拒绝表及英文解释器错误文本；无效 IDNA 返回不带 hostname/凭据的中文原因。
  ASCII 本地标签的既有地址契约保留。IP 统计键复用 rules/netip，不维护私有 IP parser。

- Legacy push/restart 的 HTTP200 非对象 JSON、坏 JSON/plain-text 回包也归入统一的
  中文 INVALID_RESPONSE/502，不再以旧英文失败对象走内部500。十条真实 Python reply
  行（method/body/status/result 全文 SHA）独立冻结，原测试的失败对象与脱敏 details
  断言原文保留，比较层逐条检查旧 Python 结果、Go 拒绝和事务无写入；真实 HTTP restart
  另核对 method/path/header/body 原字节、502 和全库 fingerprint。

### N8-10 monitor — malformed HA publication guards

协调者 2026-11-19 批准：已存 `oxidns_ha_publication` 若 present、非 null、非 object，
规则更新 POST 一律拒绝，不发设备请求、不写入。原 Python 对空串、0 当作没有任务的路径
收紧；沿当前无同类中文 409 的约定保持 native 500，明确提示“OxiDNS HA 发布记录损坏，
请先停用或修复主备配置。”非对象非空值、object.status 为容器仍拒绝，删除 CPython
异常文本仿真。a07_review 原档案和期望不变，六条完整原行/source SHA 绑定 native 拒绝；
现有 HA 停用清除损坏字段后规则更新恢复的回归覆盖空串、0、string、array。

Mihomo Agent 回包继续遵循 N8-1 的逐值 NaN/Infinity → null 响应约定，复用共享
jsonresp.NormalizeNonFinite；字符串和有限字段不改。原 a07/135 完整 archive/row/设备请求
SHA 绑定提前归一化后的 native 结果；HTTP 差分仍沿原 N8-1 比较，无新增 NaN 拒绝规则。

### N8-10 monitor — pending publication checkpoints and native JSON

协调者批准：pending dashboard checkpoint 对应的 deployments/latest_deployment/entry 为
非对象，或原始 status 为非字符串时，保持检查点、拒绝统计边界写入，并通过既有
DASHBOARD_STATE_ERROR/500 显示；不以 R5 零值或 Python falsy 值作为丢弃 checkpoint 的
证明。a06 原 archive/88 条原始行不变，完整 row SHA 精确绑定 native refusal。用户可以
使用已有的“重置统计”操作明确开始新周期；回归验证四类坏记录不会写入，手动重置后
面板与后续统计恢复正常。早于 checkpoint 的采样仍遵循原先 PublicationPending 顺序。
复核补充（review-n8-10-monitor M1）：判断检查点是否被新部署取代时，pending 部署的
status、以及被参考的 latest_deployment.status 若为非字符串，同样按损坏返回
DASHBOARD_STATE_ERROR，不再以 Python 的“不在列表中”视为已取代而丢弃检查点；null 与缺失
等同；时间早于 checkpoint 的采样仍先返回 PublicationPending。

状态文件中的真实重复 member（包括 escaped 同名）现在明确按损坏文件拒绝，避免不同
消费者选中不同统计起点；a06_review 的三条重复字段成功行完整保留并单独 SHA 绑定。
Agent 回包采用精确键名和严格 boolean 协议标志；a06 的三条 responses 重复 member/
数字 boolean 行绑定 INVALID_RESPONSE/502。另一个 responses NaN 行只绑定 N8-1 允许的
逐值 null 修复，有限字段及原始档案不变；queries 中未使用的 NaN 字段照原合同丢弃。
普通未知 JSON 字段仍按各 DTO 的原合同过滤或保留。

统计接续同样拒绝原始 profile/service/deployment 身份为非字符串、或 manual_rollback 为
非 boolean 的损坏证明；unknown 大小写别名不能覆盖它。五条独立回归核验原始 number
及 alias、checkpoint 文件字节不变，并验证显式重置可以恢复。现有 UI 仅在已加载统计时
显示重置按钮；首次加载错误时的管理员恢复 API 已补入 doc/manual/oxidns-panel.md。

### N8-10 monitor — native machine release versions

Mihomo release 分量使用共享 taskstatus.Count，不再复刻 CPython 的 4300 位限制或 ValueError
文本。机器版本只认 ASCII；公开 core/update 对当前版本沿用 version.txt 的同一字母表和
64 字节上限，错误返回 INVALID_RESPONSE/502，且不会读取/覆盖远端版本缓存。受支持
Mihomo 的短 release/alpha 版本不变。原 a07 的 1 条 Unicode helper、3 条超长分量和8条
公开 current-version 行完整保留；独立 Python ASCII/任意精度 policy generator 逐项绑定
archive/完整 row SHA 和 native 结果，不读取 Go 输出。用户测速/日志参数原兼容路径留待 N10。

指标整数也不再使用解释器的 4300 位位数上限；原 a06_reference 的 2 条及 a06_review
的 13 条超长前导零样本现在得到精确计数。真实 Python 业务仅关闭 sys int digit cap 后重跑，完整 parsed/
summary 与 archive/row SHA 独立绑定；同一计数规则传播到 1 条 overview_snapshot，
也用真实 Python 结果和完整行 SHA 单独绑定。原语料不改。metrics 仍受原响应、行长、样本数及
有限数值上限约束，不接受溢出/负值计数。

存储身份摘要只接受标准 JSON；a06_review 的 17 条含裸 NaN/Infinity 的旧私有 target
helper 行不再生成摘要，完整行和 archive SHA 单独绑定拒绝。该输入不是有效的存储 JSON；
设备 HTTP 回包仍采用 N8-1 的逐值 null 归一化，未扩大设备端拒绝范围。

### N8-10 installer — installation session ownership

协调者批准的 Python bug 修正（路径注入 + null 会话指针）：安装会话的 index 与
session.id 必须都是 32 位小写 hex 且相同。损坏记录（含 null、父目录、绝对路径、
不一致 ID）在 issue 返回 409 `安装记录无法读取，请保留现场并检查服务端数据。`，
enroll 返回 401 `安装凭据无效，请重新生成安装命令。`，均拒绝且无写入。
Python `backend/agents/docker_ha_enrollment.py:58` 唯一合法生成入口为 `uuid.uuid4().hex`。
测试服务器离线快照统计：会话数 0，非规范 ID 数 0；没有复制任何快照内容。

### N8-10 installer — native machine text and scalar admission

按协调者裁定，网卡序号、ss 计数、版本分量只认 ASCII 数字，空白使用 Go
strings.Fields/TrimSpace；非规范探测值为 unknown，不能放行 preflight。安装端口和
timeout 复用 taskstatus.Count，保留 ASCII 有符号/下划线十进制，拒绝非 ASCII 数字，
不模拟 CPython 4300 位限制。timeout 超出 int64 或 time.Duration 秒数承载范围
（±9223372036）明确拒绝，绝不回绕。Go Agent 端口为 1..65535，HA 保留 1024..65535。
脚本尾空白使用 strings.TrimRightFunc(unicode.IsSpace)；1c–1f 不再模拟 Python 空白。
HR 复核 L1（协调者裁定，裁定 3 优先）：ss 端口证据中只有 ASCII 空格/制表符组成的行
算空行；含 NBSP、U+3000、U+0085、\v、\f 等其他空白且无法解析为监听行的行判 unknown，
不能再被当作空行而判定 53 端口空闲/pass（Python `line.strip()` 曾丢弃这些行）。
现有 preflight 语料无受影响行，无新增绑定。
机器采集记录只按固定模板的 LF/CRLF 分行，base64 使用 encoding/base64 的标准语法；
非规范 padding 与其他行分隔不再复刻 binascii/str.splitlines。固定采集模板只输出
标准 base64 和 LF；人工旧行保留并以完整 archive/row SHA 绑定新 report/拒绝。
这些差异仅以人工旧行的完整 archive/row SHA 绑定，保留原 Python 输出；独立参考保存
完整新字节/错误，shell 其余字节严格比较。真实脚本模板固定 LF 结尾。

### N8-10 installer — corrupt registry and early capability admission

与 core-2 repo 的坏成员 fail-closed 规则一致：Agent registry 含非对象成员时拒绝
500、无写入，不复刻 Python 找到前方 owned Agent 后忽略坏尾部的短路行为。
IssueCapability 在创建目录前拒绝 name/host/port 错型；四条旧 helper 人工行的原输出
和目录副作用完整 SHA 绑定，新行为为原生错误且无目录写入。HTTP guide 原本先验证，
合法请求行为不变。

### N8-10 installer — normalized dangerous directory checks

Python bug 修正：拒绝危险目录前用 path.Clean 归一化，仅用于比较；`//etc`、`//opt`、
`/etc/`、`/./etc`、`/opt/../etc`、`//` 等别名不能绕过系统/共享根目录保护。
身份文件与数据目录、迁移目录之间的重叠检查同样归一化，防止双斜线别名绕过。
拒绝使用已有中文提示，发生在生成与持久化之前。合法请求保留原始路径和既有 D2
owner/hash，`//srv/native` 的合法安装身份没有重发。回归只用合成路径。

HR 复核补充（R1）：所有 guide 路径（数据目录、身份文件、HA 目录、备份目录、bind
挂载源）原先只用原文 `== "/"` 拒绝根目录，`//` 能绕过，脚本/安装包模式会把数据
写到根目录下，安装包的身份文件重叠检查也随之失效。现在按 path.Clean 后是否为 `/`
判断，`//` 与 `/` 一样返回该路径的既有中文 400；精简/直接 Compose 中 `//` 因此先
命中路径规则，而不是系统目录提示。`//srv/native` 等合法双斜线路径不变。

### N8-10 installer — string IPv4 identities and finite session JSON

协调者批准：HTTP guide 的 agent_ip 只接收 JSON string；Python 曾接受整数 IPv4 并
保存 numeric host，Go 在创建凭据前用现有 IPv4 中文提示拒绝 400，无目录或 Agent
写入。前端只发送字符串，整数形式不是受支持输入。四种安装方法保留旧请求与原 200
断言，并与新 400 精确对应；旧纯 renderer 的人工 numeric 兼容限于 legacy codec，
N8-11 删除。

损坏已存会话的 expires=NaN 不再由 Python json.loads 放行或使凭据永不过期；原生
标准 JSON fail closed：enroll 401 / issue 409、无写入。原 NaN 行和 Python 输出
完整保留在 SHA 绑定参考中，不保留非标准 JSON parser。

### N8-10 upgrades — native operation evidence and Python upgrade fixes

- 协调者批准：已存 `latest_upgrade` / `latest_deployment` / HA 守卫 present、非 null、
  类型不正确时以 `CorruptStateError`/500 拒绝，不自动清除、不写数据库。原来部分 Python
  falsy 错型被当成空记录、pair-list 被 dict() 接受；Go 不再把这些值变成任务所有权证明。
  原始 string 身份才可授权，numeric/null token 不再经 str() 发到设备。机器版本复用
  ASCII 64 字节白名单；时间数值拒绝布尔值、非有限值和溢出。原旧 A15 archive 保留，
  `n810_native.json` 完整行 SHA 绑定新拒绝（含旧异常文本），有修改输入不命中负控。
- Python bug 修正（协调者明确批准）：protocol-2 成功必须由**同一个** worker 回包同时证明
  update_id、agent_id、status=succeeded，再配合正确的 executable identity/version/
  migration_ready。错 agent_id 即使另一条 upgrade-info 完全正确也保留 pending，不写成功。
- Python bug 修正（协调者明确批准）：非空 upgrade-info 缺失/空/错型 architecture 在 reservation
  前以现有“更新文件未准备就绪”409 拒绝，不遗留 queued。原 Python 对空/缺失/空数组先写
  queued 再 KeyError500；truthy numeric 原本已409。五条真实 Python 原输入、回包、错误、
  calls、存储副作用冻结在 `n810_boundary_python.json`，逐行 SHA 校验，Go 只提供拒绝回归。
- 原生地址文本使用 strings.TrimSpace/netip，不保留私有 CPython whitespace/IP parser。
  protocol-1、durable ID、fresh heartbeat 回滚确认、未知结果继续查询保持。坏记录恢复步骤
  见 `doc/manual/agents.md`，本轮不新增 HTTP/UI 恢复入口。
