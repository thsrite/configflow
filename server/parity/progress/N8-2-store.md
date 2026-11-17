# N8-2 — store on raw rows

Status: **verified**; independent adversarial review complete, zero open N8-2 findings.
Reviewer branch: `go-review-n8-2`; report: [review-n8-2.md](../review-n8-2.md).
Branch: `go-n8-2`; integrated with N7-1 and N8-1 (`go` at `5535931d`).
No design deviation, schema/version change, new dependency or push.

## Design correspondence

- `design/n7-n8.md` §2.4: `store/document.go` defines exactly the designed
  Field/Section/Document/ProfileRows/Meta types. Entry.Document and all write
  APIs carry raw rows; metadata inputs are `map[string]json.RawMessage`.
- Store never decodes field values. It validates JSON syntax, UTF-8 and envelope
  consistency, preserves field bytes/order, and diffs bytes/positions. Metadata
  needed by the engine is decoded with encoding/json; unknown metadata stays raw.
  SQLite tables, version, transactions, locking, backups and error kinds remain.
  Malformed JSON/version/quick_check diagnostic details use Go formatting; the
  Chinese error categories/prefixes remain (PORTING permits internal text changes).
- §4 N8-2: `repo/store_rows.go` is the temporary jsonx boundary. It preserves
  original bytes when the ordered legacy value is unchanged, keeps committed
  entries immutable, and preserves fail-closed handling of uncertain commits.
  The N8-3 model codec replaces this bridge; N8-5 changes the public repo API.
- §2.4/§5.7: Fingerprint is a Go-defined SHA-256 over length-prefixed rows.
  Cross-language tests compare actual rows, not Python/Go digest equality.
- §2.7/§6 D2: native store metadata/digest; no canonjson. Current encoder
  inventory: `parity/n8-2-json-encoders.tsv`, **80** non-test calls (86 before
  N8-1 removed six response/request/helper calls). Inventory base `db41f7a1`.
  N8-1's design Appendix A retains its original frozen inventory.
- Persisted application identities are unchanged in N8-2. Inventory explicitly
  identifies health latency source keys and HA installation owner/compact Agent
  hashes/enrollment .index files. N8-10's HR installer owner must account for
  adoption/reinstall and session reuse when migrating these encoders under D2.

## Storage boundary and regression coverage

`TestStoredRoundTrip` invokes real Python ConfigStore; the private case also
opens/exports through real Python ProfileRepository before/after Go writes.
No Python mocks or value emulation. Recursive comparisons check exact JSON
scalar/container types and member order (no Python bool/int equality loophole).

| Corpus | Rows before probe | Result |
|---|---:|---|
| Committed raw fixture written by Go | 12 | Go→Python→Go→Python PASS |
| Same committed fixture written by Python | 12 | Python→Go→Python→Go→Python PASS |
| Private snapshot read-only SQLite backup | 43 | Both directions + ProfileRepository PASS |

Every case asserts byte-identical no-op rows, unchanged generation, and one Go
probe row added while all original field bytes/revisions stay unchanged. Python
then edits the same database; Go reads it and Python checks again. Unknown and
wrong-shape opaque members, escaped Unicode, whitespace, exponent/integral float
notation, empty sections/profiles and key order survive. The private source is
opened with `mode=ro`; copies and expectations stay under a mode-0700 directory
in `~/cf-prod-snapshot/runs`, removed by t.Cleanup. Final private-copy count: 0.
No private values are printed or committed; child Python diagnostics are discarded.

`backend/common/config_store.py` row layout, diff, conflicts, ordering, metadata,
creation/recovery/backup, transactions and locking retain live crosschecks.
All 11 behaviours from `backend/test_config_store.py` remain covered by
store/store_test.go and repo/store_level_test.go. Native raw tests replace
unsupported-Go-value input with invalid raw JSON rejection and add UTF-8,
opaque metadata, immutable entry, row revision and failed-encoding rollback
checks. Existing public repository crosschecks still cover operations, locks,
cache invalidation, rollback and interchange. Legacy corpus adapters are test-only
and use an independent JSON shell codec, not a copy of repo's preservation logic.
Python's unchanged-value pointer identity is internal; raw byte/revision invariants
replace it. Synthetic Infinity/NaN legacy corpus values are finite now; separate
native tests enforce the designed json.Valid rejection of non-standard JSON.
No feature skipped; model-specific R1–R9 tests remain N8-3 work.

## Author acceptance commands and output (.32)

Actual phase logs: `/tmp/n8-2-integrated.mvzoHa/`. Default /tmp; `-p=2` only
limits package concurrency; no test sampling/timeout changes.

```sh
rt -- bash -lc 'set -euo pipefail; unset TMPDIR; export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO="$PWD" CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data; cd server; test -z "$(gofmt -l .)"; go vet ./...; go test -race -p=2 -count=1 ./internal/store ./internal/repo; go test -count=1 -run "^TestStoredRoundTrip$" -v ./internal/store; go test -p=2 -count=1 ./...; cd ..; server/scripts/proddiff.sh'
rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzRawDocument$" -fuzztime=60s -parallel=4 ./internal/store'
```

- gofmt: no output; vet: PASS; production store emulation-import gate: PASS.
- Race: `ok configflow/internal/store 11.219s`, `ok configflow/internal/repo 197.156s`.
- Explicit interchange: PASS, 12+12+43 rows, `ok configflow/internal/store 1.420s`.
- Full suite: every package PASS; store 8.615s, repo 16.028s, oxidns 88.804s,
  installer 188.378s; final `ok configflow/internal/yamlx 1.850s` and
  `? configflow/tools/proddiff/generator [no test files]`.
- Proddiff: all self-checks PASS; 2831 cases / 415 comparisons / 65 GET routes;
  coverage_regressions=0, unmapped_routes=0, **new=0 stale=0**. Ten existing
  baseline differences retained. Python/Go transport failures both 30. Every
  available configuration/file/download generator succeeds on all its cases.
- Raw fuzz: integrated rerun PASS, **61.078s / 464224 executions**, no failure.
  Earlier raw-code run also passed (61.055s / 230146 executions).

Earlier runner issues were resolved, not hidden: exporting TMPDIR to the snapshot
Btrfs disk made every SQLite test fsync to disk (default /tmp is tmpfs); unrelated
packages timed out at 600s. Only private copies now use that disk. A later proddiff
run had two Python transport timeouts at domain-discovery/domains (0 vs Go 200,
then 200/200); rerun passed with no baseline edits. Default timeouts were retained.

## Commits and size

Buildable steps: `c9914ee0` types; `381c65f9` raw engine/repo bridge;
`f3381010` UTF-8/rollback guards; `dd6307c6` independent fixture codec;
`0935159d` private-copy isolation; `db41f7a1` Python repository audits.

Non-test Go lines (comments/blank lines included): store **1629→1684**;
temporary repo bridge 212 + messages 8. New files: store/document.go 40,
store/messages.go 31, store/raw_test.go 274, store/fuzz_test.go 35,
store/legacy_test.go 211, testdata/raw_crosscheck.py 82, raw_rows.json 23;
repo/store_rows_test.go 74. No open N8-2 finding or implementation work remains. Final `git rebase go`
and clean-worktree check are performed after this acceptance record commit.

## Independent review acceptance

Commits after final rebase: `dcab48dc` (no-op/security/recovery), `1a331eb0`
(native proxies + ALL fallback), `9744bef9` (CLI smoke), `8f065c7a` (large
non-integer version classification), `17f7d48e` (A15 route outcomes), plus the
final acceptance record. Final integration base: `045f2485`; no rebase conflict.

Closed findings: unchanged raw metadata committed and rewrote generation;
UTF-8 diagnostics exposed entire values; startup recovery lacked original path
and log/stderr coverage; unbounded version parse/diagnostics; CGI proxy protection
was lost by N2's custom selector. Native httpproxy replaces that matcher, with
coordinator-required ALL_PROXY fallback. Loopback is always direct, replacing
A07 decision 5; accepted native outcomes are explicit in the ledger and tests.

New boundary files: store/review_n82_test.go (194 lines), repo/review_n82_test.go
(147), repo/testdata/review_n82_interchange.py (80), agents/review_n82_proxy_test.go
(48), substore/review_n82_proxy_test.go (42), scripts/review-n8-2-startup.py (53).
Existing real Python audits retained; new private copy exercises actual Python
ProfileRepository writes and Go UpdateProfileTransaction both ways. Source mode=ro,
private 0700 copies deleted, zero residual test directories; no private values
printed/committed. Synthetic corrupt/deep/10MiB rows fail safely without changing
DB bytes; ~9MiB valid opaque rows, eight-writer conflict, no-op byte/revision,
metadata-only and cancellation-after-start checks pass.

Final .32 combined acceptance at 0efddadf: gofmt clean, vet pass; five-package
race store 18.087s / repo 217.966s / agents 43.141s / substore 6.174s / weburl
1.014s; 42 actual requests, 18 explicit native outcomes, 24 identical (5.510s);
full ordinary suite all pass (installer 160.758s, oxidns 88.397s, final yamlx
0.922s). CLI smoke 3/3 safe failures. Proddiff: 23 self-checks PASS, 2831 cases /
415 comparisons / 11 existing baselines; new=0 stale=0 coverage_regressions=0,
unmapped_routes=0, symmetric 30/30 transport failures (not successes).
Version-suffix follow-up: final store source passes .32 full live-Python/private
race 12.510s and vet. After clean rebase including O3/R05: formatting/vet all,
A15 route race 5.360s (six cases, four exact native outcomes, zero unexpected
differences), and CLI smoke pass again. Coordinator explicitly owns a new
integration-wide run after rebase; we did not chase each concurrent merge.

Scope decisions: syntactically damaged JSON / non-UTF-8 refuses startup and
preserves the original database; legal JSON wrong-type R5 belongs to N8-3.
**repo.update 入口 detach（PORTING 4b）→ N8-5/O3-6**; corresponding N8-5 design
step updated. Raw Commit is proved to finish after cancellation; outer lock
cancellation is deliberately not migrated here. No schema/version change,
new dependency, feature omission, push or PR.

Exact commands and final output: [review report](../review-n8-2.md).

## Supplementary opt-in differential audit — complete

Coordinator found an omitted C13 route differential after hand-back. The original
review had not enumerated every opt-in package; runtime proxy behaviour was
correct but TestDifferentialReviewLoopbackProxy still asserted old decision 5.
It now checks each route's full Python proxy / Go origin request (list=1, mode
readiness=1, failed GitHub update=0), including authority/auth headers. The list
asserts current_version Python=2.99.0 / Go=2.40.0, then strictly compares the
entire remaining JSON; mode/update and persisted mode compare strictly.
New non-loopback explicit-proxy differential compares real requests and Go:
full authenticated absolute target, status, content type and complete JSON.
No skips or broad normalization. .32 targeted substore/difftest passed 5.917s.
Source sweep of all *_test.go loopback / environment proxy references and all
CONFIGFLOW_DIFF entry points found no other stale loopback assertion (A15
already updated). A stale C13 comment was corrected. Next: enumerate every
Go package named difftest on .32, then CONFIGFLOW_DIFF agents/substore/upgrades
and the package-external WebDAV TLS probe. All checks below passed.

Supplementary fix: `0eba907c` on go base `9e4bbc3d`. .32 gofmt/vet all pass.
All **9 difftest packages** enumerated by `go list ./... | grep difftest` pass:
api/agents 148.101s; api/subscriptions 123.828s; discovery 2.767s; health 14.293s;
httpx 1.143s; mosdns 1.669s; refresh 3.231s; substore 6.171s; webdav 20.244s.
Additional CONFIGFLOW_DIFF=1 agents 7.750s / substore 2.173s / substore/difftest
6.639s / agents/upgrades 12.531s / webdav 0.589s all pass. Aggregate failures=0.
Changed substore/difftest race also passes 11.396s, gofmt/vet remain clean.
Logs: /tmp/n82-all-difftest.66z5C5 on .32 (synthetic fixtures only).
No new runtime change, skip, dependency or acceptance exception; the existing
native proxy decision is now exercised at every identified consumer boundary.
Full commands/results are appended to the review report. Final rebase and clean
worktree check follow this acceptance commit; no push.
