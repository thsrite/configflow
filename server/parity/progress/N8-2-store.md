# N8-2 — store on raw rows

Status: implemented; final .32 acceptance pending; branch `go-n8-2`.

Scope: `design/n7-n8.md` §2.4 exactly. SQLite schema, version, locking,
transaction boundaries and error kinds unchanged. No model or typed repo API
(N8-3/N8-5). D2: no canonjson; store metadata uses encoding/json.

Done:
- `c22843c4`: designed Field/Section/Document/ProfileRows/Meta types.
- Store Load/Commit/Create/BuildTemporary/Meta/SalvageMeta carry raw JSON.
  Only the metadata needed by the store is decoded. JSON validity and row
  envelopes are checked; field values are never decoded. Diff uses raw bytes.
- repo/store_rows.go is the temporary jsonx boundary. It retains original row
  bytes when the ordered legacy encoding is unchanged; committed rows and
  decoded cache remain immutable. N8-3 deletes this bridge for the model codec.
- Fingerprint uses length-prefixed Go rows, not Python JSON. Store crosscheck
  already compares tables; repo interchange now compares every actual row too.
- Raw fixture preserves whitespace, escapes, integral float/exponent syntax,
  unknown/malformed-shape members, empty sections/profiles; one-field edit
  verifies all unrelated bytes/revisions; typed raw-envelope rejection tests.
- Existing Python crosschecks remain live public ConfigStore/ProfileRepository
  boundary tests (not mocks). Legacy input fixture adapters are test-only.
  Their independent ordered-JSON shell codec does not copy repo's byte-preservation
  logic or decide whether a row changed (.63 live Python suite: store 18.800s).
  Two old encoder-specific tests were replaced: non-JSON struct input becomes
  invalid raw JSON rejection; Python tuple repr becomes native check diagnostics.
  Synthetic Infinity/NaN in the old generator were replaced with finite values;
  the designed json.Valid policy separately rejects non-standard JSON explicitly.
- `TestStoredRoundTrip` uses real Python, committed Go/Python fixture variants,
  and optional CONFIGFLOW_SNAPSHOT read-only SQLite backup. Only copies mutate;
  Python diagnostics suppressed; test output contains counts only. Private copies
  and expectations live in a mode-0700 directory under ~/cf-prod-snapshot/runs,
  removed by t.Cleanup on success/failure. Other tests retain default /tmp.
- Encoder inventory: parity/n8-2-json-encoders.tsv, 86 non-test call sites,
  classified by use; owning consumer steps apply D2 to persisted identities.
  No persisted application identity is changed by this storage-only step.

Iterative .63 validation:
`RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO="$(dirname "$PWD")" go test -count=1 ./internal/store ./internal/repo && go test -run "^$" ./...'`
passed: store 11.374s, repo 45.228s; every package compiles.
Targeted raw + fixture tests: store 1.080s, repo 0.152s; both fixture directions
12 rows each, no-op unchanged, Go→Python→Go→Python passed.

Additional checks: raw metadata bytes and metadata-only commits; UTF-8 validity
checked before writing fields, identifiers and metadata (json.Valid alone accepts
invalid UTF-8 inside strings); bridge encoding errors keep ErrDocument; failed
post-commit confirmation keeps the conservative uncertain/cache-preservation path.
The Python audit now recursively compares exact JSON types and member order.
Targeted .63 raw suite: store 1.448s, repo 0.154s; rollback/cache + bridge checks
passed after these safeguards.

Next: .32 gofmt/vet, store+repo race, full tests, 60s raw-envelope fuzz,
proddiff new=0, private-copy interchange; rebase go; clean; no push.
No design deviations. Only internal malformed-JSON/version repr and quick_check
formatting use Go diagnostics (Chinese category/prefix retained); native digest
value changes as explicitly allowed in design §5.7.

## Design mapping and coverage

- §2.4: designed public raw row types in store/document.go; row envelopes in
  rows.go; store.go/conn.go hold only raw field JSON and typed store metadata.
  Commit compares raw bytes and positions; table definitions and storage version
  unchanged. Fingerprint is length-prefixed rows, as explicitly permitted.
- §4 N8-2: repo/store_rows.go is the only new production jsonx boundary; no
  consumer API change. Store non-test imports of jsonx/pyyaml/pystr: zero.
- §2.7/§6 D2: inventory is parity/n8-2-json-encoders.tsv (86 call sites). Native
  store metadata/digest; application identity migrations remain in their owning
  consumer steps. No canonjson package or new dependency.
- §5.2 early storage gate: native fixture plus real-Python-created fixture,
  each 12 rows; private read-only source copy, 43 rows. Byte-identical no-op,
  generation unchanged, only new probe row written by Go, original row revisions
  retained, Python opens/edits Go database and Go reads it, Python checks again.
  N8-3 adds the model-specific typed record edits/warnings; this step does not
  claim to implement those future codec rules.
- backend/common/config_store.py: row schema, ordering, diff, conflicts, temporary
  builds, metadata, recovery-copy/backup and locking remain covered by the live
  crosscheck + store boundary tests. backend/test_config_store.py: all 11 tests'
  behaviour covered by store_test.go/repo/store_level_test.go; raw_test.go
  replaces unsupported Go-value rejection and adds opaque-byte invariants.
  Python's unchanged-value pointer identity is internal and replaced by immutable
  raw entries; exact row bytes/revisions are asserted. No feature skipped.
- Existing repo Python crosschecks still run after the bridge: public repository
  operations, rollback, concurrency, locking, cache invalidation and interchange.

Sizes (Go non-test source lines, including comments/blank lines): store
1629 → 1684; temporary repo bridge 212 + messages 8. New focused tests:
store/raw_test.go 264, store/fuzz_test.go 35, repo/store_rows_test.go 74;
Python raw interchange driver 82. No production schema or dependency changes.

Runner correction: the first .32 commands exported TMPDIR under the snapshot's
Btrfs disk for **all** packages; /tmp on .32 is tmpfs. Thousands of SQLite fsyncs
there caused unrelated full-suite packages to exceed the default 600s; the next
reduced-parallelism full run was intentionally terminated once the filesystem
cause was identified. No timeout was increased. The harness now allocates only
its private snapshot copy/expectation under ~/cf-prod-snapshot/runs and cleans
that directory explicitly; ordinary test temp files remain on default /tmp.
After rebasing on N7-1 (`e5903ffb`), live Python interchange on .32 passed again:
fixture 12 + 12 rows, private 43 rows; total 0.677s. Final gates are rerun with
this corrected environment, not counted from the invalid full-suite runs.

Final acceptance in progress on .32 after rebase to e5903ffb, using default /tmp:
`go vet ./...` passed; `go test -race -p=2 -count=1 ./internal/store ./internal/repo`
passed (store 11.991s, repo 233.133s); explicit live-Python round trip passed again
(12 + 12 + 43 rows, 1.137s). Full normal suite, 60s fuzz and proddiff pending.

Inventory follow-up for owning consumer steps (no identity is changed in N8-2):
health's source keys are persisted in latency results; HA installer hashes also
select enrollment .index files, compact Agent identities and .ha-install/container
ownership labels. These are explicitly marked persisted identities in the TSV,
not ordinary file writers. N8-10's HR installer owner must handle adoption/reinstall
and enrollment reuse when applying D2. Provider single-flight and log dedup digests
are transient. The inventory does not silently preserve Python encodings or add
canonjson, and it does not convert these other packages in this storage-only step.

Completed final-code .32 results before N8-1 integration:
- Full normal suite passed (`-p=2`): monitor 20.740s, installer 381.983s,
  oxidns 189.476s, repo 29.596s, store 12.703s; final yamlx 3.393s.
- Raw fuzz: PASS, 61.055s, 230146 executions, no failure.
- Proddiff: 2831 cases / 415 compared, coverage_regressions=0, all self-checks
  PASS, but new=2 at domain-discovery/domains. Private response inspection
  showed exactly two Python transport timeouts (status 0) vs Go 200, then
  a subsequent request 200/200; load reached 65.55. No baseline edits.
  Retry on the rebased final branch is pending; this is not marked accepted.
- Added private ProfileRepository audits in addition to ConfigStore: the real
  Python repository opens/exports the Go-written private DB before and after
  edits, with exact type/member-order equality. .32 store race passed 14.807s;
  explicit 12+12+43-row interchange passed 2.711s, all copies deleted.

Next: commit the additional audit; rebase N8-1; rerun integrated acceptance and
proddiff to new=0. No runtime store code changed since the successful full suite.
