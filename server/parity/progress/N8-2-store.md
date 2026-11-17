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
