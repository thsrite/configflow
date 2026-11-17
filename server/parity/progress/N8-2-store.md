# N8-2 — store on raw rows

Status: **ported**; independent adversarial review in progress on `go-review-n8-2`.
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

## Final commands and output (.32)

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

## Independent review (in progress)

Buildable store/recovery step: `ba7af7d2`. .63 vet and store/repo/agents/substore/weburl tests passed; .32 live private ConfigStore and ProfileRepository writes both passed.

- Reproduced unchanged-raw-metadata Commit writing rows/normalizing generation;
  fixed equal-metadata filtering and unrelated generation-byte preservation.
- Removed value-leaking CPython UTF-8 diagnostic emulation. Stored invalid UTF-8
  is ErrCorrupt; native JSON/UTF-8 diagnostics name table/key/column/byte offset
  and category only. Startup reports the original path and actionable recovery;
  final error boundary sends it to the log and stderr.
- Added synthetic corrupt/truncated/oversized/deep rows, large valid opaque rows,
  eight concurrent raw writers, unchanged metadata, and cancellation-after-start
  tests. New private-copy Python ProfileRepository writes ↔ Go repository writes
  audit passed on .32 (1.560s); existing live store audits also passed (12+12+43 rows, 2.015s). Final combined run remains.
- Scope decision: syntactically damaged JSON/non-UTF-8 refuses startup and keeps
  the original database. Legal JSON wrong-type R5 belongs to N8-3.
- **repo.update 入口 detach（PORTING 4b）→ N8-5/O3-6**. This review proves raw
  Commit is not aborted after starting; it does not move outer lock cancellation.
- Ancillary A07 failure: shared selector moved to x/net/httpproxy plus ALL_PROXY
  fallback by coordinator decision; separate Agent/Sub-Store boundaries and
  requests differential, with native outcomes explicitly asserted. See review.
- Next: final .32 private interchange, formatting/vet/race/all/proddiff, then
  rebase go and mark verified only when all review findings are closed.

Proxy step validation after ALL_PROXY preservation: .63 vet + agents 41.321s,
substore 6.481s, weburl 0.003s. .32 real requests differential: 42 cases,
18 explicit native differences / 24 identical outcomes, 4.511s. Runtime proxy
instructions updated in doc/manual/system-settings.md (no UI/screenshot change).

Buildable proxy step: `3ac0714d`; store step `ba7af7d2`. Added reproducible
`server/scripts/review-n8-2-startup.py` synthetic CLI smoke: .32 exit 0,
3 cases exit=1 / safe log+stderr / original bytes unchanged. It builds/runs only
on test servers; no snapshot content or network access. Final combined acceptance pending.
