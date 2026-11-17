# N8-2 — store on raw rows

Status: implemented; final .32 acceptance pending; branch `go-n8-2`.

Scope: `design/n7-n8.md` §2.4 exactly. SQLite schema, version, locking,
transaction boundaries and error kinds unchanged. No model or typed repo API
(N8-3/N8-5). D2: no canonjson; store metadata uses encoding/json.

Done:
- `37748f39`: designed Field/Section/Document/ProfileRows/Meta types.
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
  and expectations are t.TempDir files, removed by testing on success/failure.
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
store/raw_test.go 251, store/fuzz_test.go 35, repo/store_rows_test.go 74;
Python raw interchange driver 77. No production schema or dependency changes.
