# A11 adversarial HR review (Agent deployment bundle, A14 design steps A11-1…A11-4)

Reviewer: fresh Opus session (did not write the port; the author is a Codex agent), 2026-11-17.
Branch `go-opus-review-a11` (cut from `go-port-a11@49b51bc1`, rebased onto `go`). Scope: Python
`agents/deployment_bundle.py`, `deployment_locations.py`, `mihomo_resources.py`, `generated_preview.py`,
`rule_sync_store.py`, `oxidns_validation_receipt.py` (all read completely) and the parts of `manager.py`,
`push_diff.py`, `rule_sync.py`, `utils/ruleset_content.py`, `utils/scheduled_refresh.py`,
`diagnostics.py` they rely on, against `internal/deploy` (all files), `internal/agents/agentfake`,
`agents/record.go`, `oxidns/schema_fingerprint.go`, the `convert.Download` change, the C30 difftest
change, the Agent-side archive conformance test and the proddiff bundle generator.

Method: every Python branch walked against Go; each suspected difference turned into a failing test
first, with the expected value taken from the real Python functions: `testdata/gen_a11_review.py`
(run on the test server) writes `testdata/review.json` — 24 conversions, 19 excerpts, 10 Geo
selections, 4 fingerprints, 9 `Build` cases through a loopback rule server. `review_test.go` compares
Go with it. All review tests failed on the ported code (output below) and pass after the fixes; each
fix was also purpose-broken on the server copy to show its test fails. Fuzz targets were added for the
three untrusted-input parsers (none existed in the changed packages).

## Findings

All fixed; **0 open**.

### Medium

- **F1 — JSON/flow-mapping and quoted-key rule sources became empty MosDNS/OxiDNS rule files.**
  `ConvertMosDNSRules` used a private copy of the payload detector (`hasYAMLPayload`) that only knew a
  bare `payload:` key. Python's `_has_yaml_payload` (and the shared Go `rulesetfmt.HasYAMLPayload`)
  also treat `"payload":`, `'payload':` and any content starting with `{` as a YAML document. Go read
  such sources line by line: `{"payload": ["DOMAIN,example.com", …]}` is one line with commas whose
  "kind" is `{"payload": ["DOMAIN`, so every rule was skipped and the device received an **empty**
  domain_set/ip_set (or the rule-proxy empty-part text) — publication succeeded, the DNS split was
  silently lost. A sing-box JSON document (no `payload`) was shipped empty where Python refuses it
  (`规则文件的 YAML 内容格式不正确，请检查规则内容。`); `"payload":` block YAML failed with a wrong
  `Unrecognized MosDNS rule source content` on line 1. Reachable for declared `format: yaml` sources,
  undeclared downloads, library content rules and attachments converted for a plugin (declared
  `text` sources are refused earlier by the shared remote validation in both). Class: private
  incomplete copy of a shared helper (R04 F10) causing a wrong shape to be silently emptied (A19 F2/F3).
  Fix: use `rulesetfmt.HasYAMLPayload`; the private copy is deleted.
  Tests: `TestReviewYAMLPayloadForms` (24 Python rows; 22 failed before), `TestReviewBuildDownloadsAndJSONSources`
  `json-json`, `json-undeclared-json`, `json-undeclared-singbox` (Build boundary; fail with the old detector).
- **F2 — credentials continuing on the excerpted line were shown in error messages.**
  Python's `source_excerpt` hides a line when **any of three** patterns spans its start: the
  redaction pattern `_SECRET` (whose value may follow `Bearer`/`Basic` and whitespace, including a
  newline, and whose key may be separated from `:` by a newline), `_UUID`, and `_QUOTED_SECRET`. Go
  checked only the quoted pattern. `authorization: Bearer⏎<token>`, `headers: {Authorization: Basic⏎<value>}`,
  `token⏎: <value>`, `uuid⏎= <value>` and a flow proxy `password: Bearer⏎<value>` printed the value as
  `出错内容：…`. These texts reach the UI, the deployment record/events and the log (provider files from
  airports and rule sources go through this function via the C30 locations hook). Fix: `diag.SecretSpans`
  exports the spans of the shared FailureText pattern (one implementation: `replaceAtWordStart` now uses
  the same `matchesAtWordStart` finder); `SourceExcerpt` checks it, the uuid pattern and quoted values.
  Test: `TestReviewExcerptHidesContinuedCredentials` (19 Python rows; 5 leaked before).

### Low

- **F3 — YAML values were judged by tag text, not by value.** `truth` treated `False`, `0x0`, `0.0`
  (YAML 1.2 core values yaml.v3 and PyYAML both type) as true, and `geodata-mode: True`/`TRUE` was not
  "is True"; so a disabled `fallback`/`fallback-filter.geoip` still required GeoIP and `geodata-mode:
  True` downloaded `geoip.metadb` instead of `GeoIP.dat`. Also: a UTF-8 BOM hid the first line of a
  text-format classical provider (Python decodes both formats as utf-8-sig), the sniffer `geoip:` code
  list ran to the end instead of the next colon (`geoip:lan:x` required GeoIP), and mapping **keys**
  were scanned as rules (Python walks dict values only). `truth`/`isTrue` now decode the scalar
  (`yamlx.Decode`); same for the `include`/`args` checks that use `truth`. Test:
  `TestReviewGeoSelectionUsesYAMLValues` (10 Python rows; 6 failed before).
- **F4 — v1 content fingerprint differed for paths with U+2028/U+2029.** `encoding/json` escapes
  them even with HTML escaping off; Python writes them raw. Paths cannot contain `\`, so the two
  escapes are replaced after encoding; the fingerprint is now byte-identical for every valid manifest.
  (The design had accepted this edge; the fix is two lines.) Test: `TestReviewFingerprintEscapes`
  (4 Python rows incl. `<>&"'`, CJK/emoji; 2 failed before).
- **F5 — receipts with a profile snapshot over 32 MiB were silently treated as missing.**
  `receipt.json` embeds the whole profile (inline content rules included); Python reads it without a
  limit. A larger receipt made `Load` return none, i.e. rule sync/push-diff lost their baseline. Bound
  raised to 512 MiB (`maxReceiptBytes`, like preview metadata). Test: `TestReviewLargeReceiptSourceLoads`.
- **F6 — the stale-cache warning named the source id instead of the rule** (`规则集「%s」更新失败，
  使用上一次有效缓存`). `readRule` now returns the declaration's name (typed `ruleRead` instead of four
  positional results). Log only.
- **F7 — `Bundle.Files` re-read and re-validated the whole archive on every call** (decompress and
  SHA-256 every member), where Python's `_archive_files` stops once the wanted members are found. The
  archive is validated when packed/opened and never changes, so `readMembers` streams only up to the
  wanted members (`Contents`, `ValidateBundle` and `Open` keep the full validation). Test:
  `TestReviewFilesStopsAtWantedMembers` (a stream damaged after the wanted member: read succeeds;
  reading past it fails; the old code failed the first read).
- **F8 — duplicated Chinese constant and dead code.** `deploy.UncheckedCandidateMessage` was a copy
  of the literal in A01's activation gate; now one constant `agents.MessageUncheckedCandidate`, used by
  the gate and the test. An empty `if` left in the C30 difftest after removing `deferA11Locations` is
  removed.
- **F9 — `RuleDownloadError` was not a `*PreparationError` for `errors.As`.** It embeds
  `*PreparationError`, whose promoted `Unwrap` returns the transport cause, so `errors.As(err,
  &prep)` skipped the preparation error (Python: `RuleDownloadError(DeploymentPreparationError)`).
  Latent in A11 (callers test `*RuleDownloadError` first), but A14's `IsPreparationFailure` and route
  mapping would have answered 500 without the remedy instead of 400. `Unwrap` now returns the embedded
  error. Test: `TestReviewRuleDownloadErrorIsPreparationError`.

## Codex-port quality

- **High-severity findings: 0.** Medium: 2 (F1, F2). Low: 7.
- Classes: **private incomplete copy of a shared helper** (F1, also F8) — the shared, complete helper
  existed (`rulesetfmt.HasYAMLPayload`) and the hand-back claimed "Shared diag/mosdns/rulefetch/refresh"
  for R04 F10; **partial port of a multi-part safety check** (F2: one of three secret patterns); **YAML
  value semantics by tag/text instead of decoded values** (F3); persisted-identity encoding edge (F4);
  an over-tight bound that fails open into "no baseline" (F5); Go error-chain semantics for embedded
  error types (F9).
- What the port got right (checked, below): cancellation and lock ordering, archive safety and bounds,
  validation-receipt identity and the fail-closed refusal of Python-era candidates, the content
  fingerprint encoding, the honest `partial` labelling in proddiff, and broad Python corpora.
- Fallback rule (≥ 2 high → remaining agents-area HR ports back to Opus): **not triggered**. Recommended
  addition to Codex HR briefs: "before writing a helper, grep for an existing exported one
  (`rulesetfmt`, `diag`, `rulefetch`, `mosdns`); when porting a function that applies several patterns,
  list every pattern and test each".

## Author's per-class claims checked (A11-HANDOFF checklist)

- **True:** R04 F1 (manifest/member/count/type checks; corrupt metadata rejected before use), F2 (shared
  messages; Python context corpus), F3 (eight workers always joined; `Build` returns `ctx.Err()` and
  records nothing; `Receipts`/`Previews` detach with `context.WithoutCancel`; per-attempt 10 s/60 s rule
  timeouts, no extra overall deadline), F4 (no device writes in A11), F5 (fingerprint check order and
  messages), F6 (first error retained), F7 (typed manifests, `reflect.DeepEqual`, no interface
  comparison panic), F9 (missing bundled file/provider refused); R05 C1–C4, C6; A19 F1 (hostile
  providers: yamlx alias budget + `gateway.RoutingMarkPath` visits each container once).
- **False or partial:** R04 F10 "shared helpers" — false (F1 private `hasYAMLPayload`, F8 duplicated
  message). A19 F2 "explicit payload shape" / A19 F3 "invalid source fails" — false for JSON/quoted
  payloads (F1). R05 C5 "indexed member lookup" — partial (F7). R04 F8 bounds — one bound too tight
  (F5). "23 sensitive/indented excerpts" — the count is right, but the corpus had no value continuing on
  the excerpted line (F2).

## Checked without findings

- **Content fingerprint / digests (persisted identity).** Members sorted alphabetically like
  `sort_keys`, compact separators, `SetEscapeHTML(false)`, digests lowercased, main file canonical
  `config.yaml`, `resource` role refused, validation order and English texts as Python (31 + 17 Python
  rows of the author, + 4 review rows). Unchanged bytes → unchanged fingerprint across the switch.
- **RulesFingerprint.** Embedded schema summary (`oxidns.SchemaFingerprint`, read-only, sorted names
  and bytes) + build identity (`debug.ReadBuildInfo` main version and `vcs.revision`, else
  `version.Version`); changing either changes the digest; a Python-era `rules_sha256` is refused by the
  real A01 activation gate with `候选配置缺少当前规则的校验证明，已停止激活。请重新推送配置并完成校验。`,
  status `failed`, 409, and **no** activation POST (`TestRulesProofAndOldCandidateRefusal`).
  `ReceiptMatches` compares the whole receipt (extra or foreign fields refused) and uses the resolved
  default profile (Python bug fixed, listed in behaviour-changes).
- **Archive safety.** Reading: gzip and tar bounded (`LimitReader`, ≤ 2×MaxFiles headers, 64 MiB per
  member, 4 MiB manifest, aggregate limit), only `TypeReg/TypeRegA` (no links, devices, sparse files),
  directories only under `files/` and path-checked, every member path canonical and listed, duplicates
  refused, sizes and SHA-256 equal to the manifest, manifest structurally equal, gzip trailer consumed
  (truncation refused). Python-written PAX archives load. Writing: deterministic PAX, mode 0600, sorted.
  Private stores: 0700 directories, symlinked files/dirs refused (`Lstat`), atomic temp+fsync+rename,
  preview ids `[a-f0-9]{32}`, receipt folders are SHA-256 names, `Preview.File` serves only listed
  canonical paths.
- **Locks and concurrency (O3).** `Receipts` reads the retention set from the repository before the
  per-Agent L4 lock and runs `AdvanceContent`'s transaction after releasing it (never L3 inside L4);
  `Previews` save/evict under one L4 mutex; conversion and raw caches are L5 (memory only, cloned
  bytes); the raw cache single-flights per URL within one build, followers honour their context, failed
  fetches are not cached; worker panics are recovered and logged via `logx`. `-race` clean.
- **Rule materialization.** Recursion bound 3, rule-proxy/gateway/library callbacks only from the
  snapshot, last-good cache through `refresh.ReadRuleSource`, remote validation for Mihomo consumers,
  Chinese download texts and remedies (review rows: declared, undeclared, library, connection refused,
  Mihomo provider — equal up to Python's raw exception line). Secrets in transport errors pass
  `diag.FailureText` (URLs sanitised).
- **Mihomo resources.** Default URLs, alias reuse, Agent capability list, empty/HTML refusal, size
  limits, progress texts, resources excluded from the content fingerprint.
- **Previews.** TTL/count/byte eviction (oldest first; metadata counts toward the quota), identity
  and input-digest freshness checks, frozen ZIP/file bytes, final-preview adaptation and comparison
  baseline, Python-written previews load.
- **proddiff `partial` labelling is honest.** The frozen aggregation-provider case is detected by the
  private frozen file Python writes (mode 0600, only when an `/aggregations/` provider exists), counted
  as `partial` with the coverage text `partial: A11 packaging only, aggregation provider frozen (C31
  pending)`, excluded from `success`, protected by its own regression counter, and the
  aggregation-provider generator stays `Go 尚未实现`. Only the frozen provider bytes are shared; every
  other file of that bundle is generated independently. It is not counted as end-to-end parity.

## Accepted differences and debt (not findings)

- The `原始错误：` line of a download failure shows Go's error type and text (`*rulefetch.FetchError: …`)
  where Python shows `HTTPError: …` (internal exception text, not chased; a cleaner Go text can come in
  an S pass). Python's `原始诊断：'payload'` (a KeyError text) has no Go counterpart.
- `Receipt.Bundle` and `PrepareMihomoResources` keep `comparison_config` where Python's loaded receipt
  bundle and resource bundle had none; Python never read it there (push diff and capture use the
  receipt record's / content bundle's value). A12/A16 must keep reading `Receipt.ComparisonConfig`.
- After the first failed download Go still reports progress for the remaining joined workers; Python
  stopped reporting. Only `preparing` messages precede the failure record.
- A Mihomo `rule-providers` entry **without** `behavior` whose URL is a local `target=mihomo` callback is
  normalised by Go (Python compared `None` and never normalised); Mihomo refuses such providers anyway.
- A `null` `geox-url.<key>` prints `下载地址：null` (Python `None`); CPython `splitlines` separators
  (`\v`, `\x85`, U+2028) in rule text and the `\x1c`–`\x1f` whitespace around `payload` are not chased.
- A malformed stored gateway setting fails every internal callback in Go; Python failed only callbacks
  matching a gateway path. Settings are validated when saved.
- `RulesFingerprint` relies on the image build setting `version.Version` (or VCS stamping): two
  local `dev` builds with different validator code share a fingerprint (design 2.3).

## Requirements on later modules

- **A14:** classify preparation failures with `errors.As(err, **PreparationError)` (now true for
  `*RuleDownloadError`, F9) and read the remedy from it; keep passing `Behavior`/`Format` of Mihomo
  rule providers from the final main file (Build does), not from `convert.Download` defaults.
- **A12/A16:** read the receipt comparison entry from `Receipt.ComparisonConfig`; use `Bundle.Files(paths…)`
  for a few files (fast path, F7) and `Contents()` when all are needed.

## L2 lab items (unchanged; do not block `verified`)

As in A11-HANDOFF "Only L2 can prove" and design 6.7: real core load/reload of Go-built archives on
current and oldest protocol-1 Agents (< 1.8.30-go, < 1.8.57-go), stage-only → ready → activate, forced
activation failure and restore, rollback failure → recover, link loss after upload, AIO final-preview
DNS-port adaptation, Geo database reuse/permissions, process death mid-preparation/upload, Docker
paths/ownership. Add: a MosDNS/OxiDNS device fed a JSON-form (`{"payload": […]}`) rule source loads a
non-empty rule set (F1).

## Commands and results

Before the fixes (`.63`, review tests against the ported code; trimmed):

```
--- FAIL: TestReviewYAMLPayloadForms
    row 0 "{\"payload\": [\"DOMAIN,example.com\", …]}" domain_set: got "" <nil> want "full:example.com\ndomain:example.org"
    row 4 "\"payload\":\n  - DOMAIN,example.com\n…" domain_set: Unrecognized MosDNS rule source content … want "full:example.com"
    row 16 "{\"version\": 1, \"rules\": […]}" domain_set: accepted "", Python refused: Invalid rule YAML payload
    … 22 of 24 rows
--- FAIL: TestReviewExcerptHidesContinuedCredentials
    "authorization: Bearer\nsynthetic-bearer-value\n…" line 2 got "出错内容：synthetic-bearer-value" want "出错内容：[敏感内容已隐藏]"
    … 5 rows
--- FAIL: TestReviewGeoSelectionUsesYAMLValues
    "dns: {fallback: False}\n": got [mmdb] want []   … 6 rows
--- FAIL: TestReviewFingerprintEscapes   "rules/a\u2028b.txt": got "v1:c802…" want "v1:03f1…"   (2 rows)
--- FAIL: TestReviewLargeReceiptSourceLoads   invalid private deployment file
```

Purpose-broken after the fixes (`.32`, change applied to the server copy only): old payload detector →
`TestReviewBuildDownloadsAndJSONSources/{json-json,json-undeclared-json,json-undeclared-singbox}` fail;
full-archive read in `Files` → `TestReviewFilesStopsAtWantedMembers` fails; `RuleDownloadError.Unwrap`
removed → `TestReviewRuleDownloadErrorIsPreparationError` fails.

Final authoritative run (`.32`, branch rebased onto `go` `62427c41`; commands and output tails in the A11
section of PROGRESS-agents.md): gofmt empty, `go vet ./...` ok, `-race` deploy/agents/agents-diag and
subscriptions difftest ok, `go test -p=4 -count=1 ./...` exit 0, all ten CONFIGFLOW_DIFF packages ok, three fuzz
targets 60 s each PASS, go-agent ServerArchive ok, proddiff.sh self_check PASS new=0 stale=0 (bundle compared=3
success=2 partial=1).
