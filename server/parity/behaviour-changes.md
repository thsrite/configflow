# Deliberate behaviour changes

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

## D2 — 上游持久化标识的 Go 原生编码

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
| Step B #3，所有 /api 错误出口 | Werkzeug HTML 错误页（含400/404/405/500及文件失败） | 同状态 JSON `{success:false,message}`；不回显内部诊断，保留 Allow 等头，HEAD 不返回正文。原JSON错误的字段与语义继续保留。非API HTML/重定向不改变。`httpx/error_boundary_test.go`。 |
| R058-B03，domain-discovery 未知遗留字段 | 更新设置时删除未知字段 | 按N8保留未知字段并原样往返，包含false与空数组。`api/domaindiscovery/legacy_settings_test.go`。 |
| Step B #12a，Docker HA server_url 末尾换行 | Python200（正则$接受末尾换行） | 保留既定A09决定：400「请填写完整的 ConfigFlow HTTP(S) 地址」。用例明确断言该差异。 |
| Step B #12a，Docker HA非法IPv6 URL | urllib错误「Invalid IPv6 URL」 | 同为400、success=false，使用net/url原生英文诊断。只对该命名向量声明文本差异，其余字段/头严格比较。 |
| Step B #12b，安装器非法UTF-8 query | Werkzeug保留非法字节为%XX | Go使用U+FFFD，引用/转义经过bash语法和隔离赋值测试。仅把非法字节规范化后的有效UTF-8请求交给真实Flask生成完整预期，与Go严格比较；不mask脚本片段。 |

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

## O3 — 单实例与有界关机

设计 D1/D5 与维护者 M1：每个 DATA_DIR 只允许一个 Go 服务进程；启动时同时持有 Python
四把调度器生命周期锁，忙时最多等 30 秒后退出 1。Go 不再依靠多个服务进程间的这些
调度锁分配工作。路径别名使用同一份仓库路径解析，不能绕过实例锁。

SIGTERM/SIGINT 先保存历史，再在默认 8 秒宽限期内收尾，在截止前 2 秒取消剩余工作，
最后保存新增历史并关闭资源；空闲提前结束。Python 原来在保存后直接退出，Go 在镜像
既有 10 秒停止预算内等待已有工作。第二次信号立即退出；镜像停止时限没有提高。
