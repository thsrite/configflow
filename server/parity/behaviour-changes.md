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
