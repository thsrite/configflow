# A19 HR review (Linux gateway core)

Reviewer: fresh Opus (task O4), 2026-11-17. Branch `go-opus-review-a19` (cut from `go-port-a19`, rebased
onto `go`). Scope: `backend/common/linux_gateway.py` and its callers against `internal/gateway`
(settings, typed config view, extraction, providers, `Apply`, overlap, editor, validation, convert
hooks). Method: every Python branch walked against the Go code; corpus comparator read for
comparisons that cannot fail; corpus coverage measured (`-coverprofile` of `TestPythonCorpus`);
adversarial inputs run against real Python on the test server; each fix purpose-broken (old code
restored) to prove its test fails.

## Findings

All fixed; **0 open**.

- **F1 — Medium (fixed): shared YAML containers were expanded; cyclic graphs never ended.**
  The jsonx↔node boundary (`codec`), `cloneShared` and `RoutingMarkPath` treated every occurrence of
  a shared container (a YAML alias the loader keeps as one value) as a separate tree. Python keeps
  aliases shared (`deepcopy` memo, `_routing_mark_path`'s `seen` set).
  Failure: a base configuration with an alias chain (`l1: &l1 [*l0, *l0]` …) made generation with
  the gateway on, the editor state and selection validation exponential — depth 16 gave a 9.9 MB
  config, depth 18 44 MB in 1.8 s, depth 40 never finishes (memory exhaustion of the panel); Python
  answers depth 40 in 2 ms with a 2 KB config. The same applies to A11's routing-mark check of
  third-party provider files (`RoutingMarkPathJSONX`): an airport could stall publishing. A cyclic
  `yaml.Node` graph (yaml.v3 keeps self-referencing aliases) made `RoutingMarkPath` recurse until
  the Go stack overflowed — a fatal crash of the whole process.
  Fix: `codec.node` maps each shared input container to one node, `codec.value` each node to one
  rebuilt value (lists are identified by first element *and* length, so a list and its prefix
  stay distinct), `cloneShared` copies a shared container once, `RoutingMarkPath` visits each
  container once (Python's `seen`). Shared provider mappings are now edited once and seen through
  every alias, which is what Python's in-place `dict.update` did.
  Tests: `TestRoutingMarkPathGraphs` (cycles, shared chains of depth 64, jsonx chains),
  `TestSharedContainersStayShared`, `TestGenerateWithAliasChain`. On the old code: the chain cases
  time out after 10 s, the cycle case crashes, sharing is lost, the generated config is 9,962,743
  bytes.
- **F2 — Low (fixed): `Apply`/`RoutingMarkPath` silently did nothing on a yaml.v3 document
  wrapper or a non-mapping.** `RoutingMarkPath(DocumentNode)` returned "" (no conflict) without
  looking inside, and `Apply` on a `DocumentNode` or a sequence returned nil without patching —
  a consumer (A11/A20) parsing with `yaml.Unmarshal` would publish an un-patched configuration as
  a gateway configuration and skip the routing-mark guard. Now a document wrapper is transparent
  and any other non-mapping is an error (not `*Error`; a caller bug, Python's dict was never
  anything else). Test: `TestApplyDocumentShapes`, `TestRoutingMarkPathGraphs/document wrapper`.
- **F3 — Low (fixed): non-ASCII white space around `payload:` emptied a classical capture.**
  Python's `^\s*payload\s*:` uses Unicode white space; RE2's `\s` is ASCII only. Content such as
  ` payload:\n  - IP-CIDR,1.2.3.0/24` (Python: YAML, captures 1.2.3.0/24) or
  ` payload:` / `\vpayload:` / `\x1cpayload:` (Python: YAML, refused — Mihomo keeps the
  previous provider) was read by Go as text lines that hold no rule, so a classical set served an
  **empty** payload: a silently cleared capture. The detection now accepts any Unicode white space
  or ASCII separator control around the key. 62 corpus rows added (`gen_a19.py` FIXED_CONTENT,
  random rows unchanged); with the old regex 40 rows differ.

Coverage additions (no defect): content size limit (16 MiB, +1), rule count (250000, +1, YAML
form) → `TestContentLimits`; all eleven TUN filter keys with set/unset values →
`TestTUNFilterKeysRefused`; scalar `tun`/`dns` (false, true, 0, 1, 0.0, "", "x", null; expected
values from Python) → `TestScalarSections`. These branches had zero corpus rows.

## Checked without findings

- **Apply semantics** (selective; full_tun through `routersplit.CheckGeneration`; legacy global view
  `/api/custom-config`): order of checks (router split, setting, routing-mark in document then
  nodes, fake-ip range, TUN type, filters, providers type, reserved prefix, token), removed TUN
  route keys, forced TUN/DNS values, `setdefault` of `stack`/`device`/`fake-ip-range` (null kept),
  fake provider always first in `route-address-set`, original providers re-pointed only for usable
  rule sets, `ipv6: false`, `mode: rule`, marker. Disabled = no conversion at all. Corpus `apply`
  rows compare exact `jsonx.Compact` (order + types); `generate`/`downloads` rows run the real
  generator.
- **Extraction**: `ParseNetwork` forms (bare host, netmask/hostmask, leading-zero prefix, no
  leading-zero octets), IPv4-mapped IPv6 stays IPv6 (skipped, never unmapped into IPv4), zones only
  on IPv6, `/8` limit before collapse, `/0` after collapse, duplicates/overlaps collapsed and sorted,
  IP-CIDR/IP-CIDR6 version check, malformed lines refuse the whole set. Fuzz target ran 60 s.
- **Fail-closed reading**: without the C32 hook every default read is `ErrRulesetContentUnavailable`
  (not `*Error`, so A20 answers 5xx and Mihomo keeps its provider); with it, an empty result is
  accepted only with an existing regular cache file; loader errors propagate unchanged (A11's
  `DeploymentPreparationError`). Generation never reads content. Empty payloads come only from the
  Python cases (gateway off, not selected, deleted, MRS, disabled, explicitly empty source).
- **Accepted difference classes**: IPv6 text forms in source providers (same network); Unicode
  `strip`/`upper` edges (Go strips a subset of Python's white space → Go only refuses more; no
  non-ASCII letter upper-cases to `IP-CIDR` differently); crash classes as non-`*Error` (500 both);
  invalid UTF-8 refused. The zone fix (`fe80::1%eth0/128` → `fe80::1/128`) is justified: Mihomo
  cannot parse a zone and IPv6 is off in gateway mode. The `\s` class was not safe → F3.
- **Settings / validation / stored data**: `NormalizeSettings` matches Python for every JSON shape
  (null, wrong types, 1024/1025 ids, 256/257 runes, control characters, duplicates);
  `ValidateSelection` always accepts switching off once well formed; stored ids may stay
  (`previous`); malformed stored data only narrows (non-string ids/library ids never match →
  cleared capture), never widens. `member`/`flag` keep Python's truthiness (`enabled: null` = off)
  instead of R5's "absent" — deliberate, safer, documented by the porter.
- **Errors for A20**: `*Error` = 400 text, `*routersplit.RouterSplitError` matches `*Error` too
  (check it first), everything else 500. Chinese messages: all in `messages.go`, byte-compared by
  the corpus.
- **API for A11/A20–A22/C36**: every Python symbol the consumers import has a Go counterpart
  (`Settings`/`SettingsOf`, `ProviderYAML`/`SourceYAML` with a `ContentLoader`, `ProviderPath`,
  `RoutingMarkPathJSONX`, `DirectOverlapWarnings`, `EditorState`, `ValidateRuleSetIDs`,
  `ValidateSelection`, `Error`).
- **Concurrency**: no locks besides the hook's `RWMutex`, no goroutines.

## Debt (not findings)

- **N7-3**: `BaseDocument` and `yamlPayloadStrings` still use `yamlx.LoadWith` (PyYAML port, with
  non-string keys and unrepresentable placeholders) while convert's generator uses `yamlx.Load`
  (refuses them). They disagree only on acceptance: the editor/validation can accept a base
  configuration that generation then refuses loudly; they never read a different fake-ip range.
  N7-3 must move both to the one facade.
- **N3/N8**: `convert.PySplitlines` (shared helper, Python line breaks) and `jsonx.Str` (Python
  `str()` for `rule_type`/`value` names and non-string keys) are shared, not private copies.
- **N9**: `gwbase.LinuxGatewayError.PythonClass()` (routers' decision 2, not A19 code).
- C32 must install `SetRulesetContent` before A20's provider routes ship.

## L4 (lab only)

Unchanged from PROGRESS A19: TUN auto-route/auto-redirect nft capture of exactly fake-IP + selected
IPv4 networks; `route-address-set` refresh every 300 s with the token, previous provider kept on
5xx; routing-mark relevance; fake-ip DNS on 127.0.0.1:1053 with IP-DoH upstreams not hijacked;
IPv6 off end to end; LAN clients through selective / full_tun / legacy; persistence across reboot.

## Commands and results

- Python reference: `RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/gateway/testdata/a19_corpus.jsonl.gz -- bash -lc 'PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/gateway/testdata/gen_a19.py'` → 7494 rows (was 7432).
- Purpose-broken checks (.63, old code restored per fix): F1/F2 tests fail (timeouts, crash,
  9,962,743-byte config); old regex → `40 of 7494 rows differ`.
- Fuzz (.63): `go test -run x -fuzz FuzzExtractCIDRs -fuzztime 60s ./internal/gateway/` → `ok 61.157s`.
- Final (.32, rebased on `go`):
  - `gofmt -l .` → empty; `go vet ./...` → ok.
  - `go test -count=1 -race ./internal/gateway/... ./internal/convert/...` → `ok configflow/internal/gateway 35.783s`, `ok configflow/internal/convert 109.511s`.
  - `go test -count=1 ./...` → all ok except `internal/importer` `TestAdversarialImporterResourceBounds/wide` (import subprocess killed by its timeout; load average 20 on .32); rerun alone → `ok configflow/internal/importer 1.046s`. Unrelated to A19.
  - `CONFIGFLOW_DIFF=1 go test -count=1 $(go list ./... | grep /difftest)` → 9 packages ok (agents 127.6 s, subscriptions 95.9 s, discovery, health, httpx, mosdns, refresh, substore, webdav).
  - `rt -- server/scripts/proddiff.sh` → `self_check: PASS`, `coverage_regressions: 0`, `new: 0`, `stale: 0`, `baseline: new=0 stale=0`.

Lines (non-test): adapter.go 361→418, apply.go 223→249, node.go 111→120, extract.go 172→176.
