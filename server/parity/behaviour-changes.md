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
