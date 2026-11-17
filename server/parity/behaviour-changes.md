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
