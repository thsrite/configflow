# N8-3 — model skeleton and byte-preserving record codec

Status: **ported**; ready for independent Opus stored-data-core review.
Branch `go-n8-3`, no push; Opus review required before verified.
Scope follows design/n7-n8.md §2.3, N8-3 and R1–R9. Concrete resource/settings
records and public typed repo transactions remain N8-4/N8-5 respectively.

## Implementation and contract

- `internal/model` is a leaf: standard library + yamlx, no I/O/logx/store/repo/jsonx.
  Document/Profile/Shared/System are skeletons; complex fields are RawMessage.
  Revisions use model.Int per R8 (the schematic declaration now agrees with R8).
- Public codec: Raw, DecodeRecord, EncodeRecord, Marshal; ordered Fields and Map[V];
  Dynamic.Node/SetNode (fresh yamlx nodes); Int exact integral-number decoding.
- `DecodeDocument([]byte) (*Document, []FieldIssue, error)` is the load boundary.
  FieldIssue contains only Path/Kind. Raw.Issues returns a record's direct issues.
  Diagnostics contain schema names/numeric indices, never user keys/IDs or values.
  Repo logs once per loaded head, including uncached oversized documents.
- Repo builds model.Document from ordered raw store rows and splits model output
  back into rows. Outer whitespace belonging to an unchanged standalone row is
  retained separately from a JSON member's lexical value. Schema unchanged.
- Legacy callers retain N8-2's ordered intent adapter before model decoding.
  This is temporary: e.g. true→false must remain an observable legacy edit even
  though both read as model.Int(0). N8-5 removes it with legacy mutations.
- Maintainer decisions are recorded under “Clarified in N8-3” in the design:
  diagnostics collected by model/logged by repo; raw JSON fragment composition
  for records/RawMessage/containers, native Encoder(SetEscapeHTML(false)) for
  new scalars; no Python encoder/emulated exceptions/new dependency.

## Reflection/encoding invariants and evidence

1. Schema descriptors are cached immutable values; tags are exact, duplicate tags
   rejected. Stored declarations reject interface/native map/native integers;
   nested records require codecs. R4 nonzero defaults require pointers/accessors;
   the test optional-enabled accessor distinguishes absent from explicit false.
2. Decode owns a cloned source, constructs a fresh temporary record, and publishes
   it atomically. Raw contains original ordered fragments + immutable typed
   baselines, is bound to its record type, and is never mutated by encoding.
   Fields/Map mutations copy state; Fields byte getters and Dynamic nodes are
   independent copies. References within generic V retain normal Go semantics.
3. R2 compares current encoded typed values with the decode-time baseline; equal
   values reuse original bytes (null, 6e1/60.0, escapes, nested order, whitespace).
   R5 errors zero only that typed member, retain its original raw fragment and
   collect an issue; an unrelated edit preserves it. Invalid JSON/root shape or
   duplicate names remain errors. No issue contains an error text or stored value.
4. Original keys/member order/layout survive; changed values retain their key
   layout. Absent zero fields omit; new nonzero fields append in struct order,
   then new Fields. Present zero fields stay. A changed pointer set to nil deletes;
   unchanged original null remains under R2. Fields.Delete + re-add appends.
5. Every output is UTF-8 json.Valid JSON with unique keys at every object depth.
   Only validated JSON fragments are spliced. stdlib encodes new scalar values.
   No generic struct fallback silently drops private Raw/unknown fields.
6. Marshal rejects actual pointer/map/slice cycles. Slice identity includes length:
   overlapping shorter prefixes and empty references are acyclic and accepted.
   Regression TestSliceOverlapIsNotACycle covers both valid sharing and a cycle.
7. codec_test.go covers untouched/unknown/child edits, presence and defaults,
   strict/exact number bounds, duplicate/invalid UTF-8/cycle rejection, schema/Raw
   type guards, Map order/COW and Dynamic ownership. Both fuzz targets independently
   parse output using encoding/json tokens and RawMessage boundaries (not codec
   helpers), checking recursive uniqueness, exact no-op bytes, unchanged member
   bytes/order, stable decode/encode and successful record/profile edits.

## Boundary/storage proofs and deferred legacy restriction

TestStoredRoundTrip always exercises committed raw_rows.json, a full synthetic
Go repository, and a Python-written fixture when CONFIGFLOW_PY is set. The final
CONFIGFLOW_SNAPSHOT run additionally uses a read-only SQLite backup of the private
snapshot. Every run checks exact row bytes/order, no-op generation/fingerprint,
scripted group rename/node add+delete/agent edit/router setting/rule reorder,
unknown fields at each record level, R5 preservation, and physical revisions of
only touched rows. Nested records in this harness are test-only; no N8-4 schema
is prematurely introduced. Re-run this harness for every future model type.

The unchanged real-Python store/testdata/raw_crosscheck.py checks ConfigStore and
ProfileRepository.export_all after Go writes, then edits the same database for Go
to load and round-trip. No Python behaviour is simulated. Existing live repo/store
crosschecks and tests for backend/test_config_store.py, test_config_repository.py,
test_config_repository_cache.py, test_config_storage_migration.py and
test_config_import.py remain in the full suite; this step adds storage boundaries,
not replacements/skips of their tests.

Private copies/expectations use unique 0700 directories under
~/cf-prod-snapshot/runs/n8-3-roundtrip-*; SQLite source opened mode=ro. Cleanup
removes every copy. Child diagnostics are discarded; output contains counts only.
Fixture rows=12, full synthetic rows=27, private rows=43/profiles=2.

**N8-5 prerequisite:** model satisfies R5, but legacy ValidateDocument is unchanged.
Non-integer-typed revisions (including integral 60.0 and string "60"), non-string
or empty names can still block loading. TestModelR5LegacyLoadValidationRemainsUntilN85
locks this restriction while also proving native fallback/raw preservation. N8-5
must split load validation from write/business validation and update the test.
TestModelIssuesLoggedOncePerRevision uses a child process and uncached reads:
one warning per head, no secret/password in logs, no shared logger mutation.

## Commits and acceptance

Buildable commits (after rebase onto go 62427c41):
800a0bb2 codec; 78c9ddcd skeleton/type guards; 59ab729e repo integration/round trips;
37a17b20 independent fuzz assertions; af6b2bca slice-sharing cycle correction;
5ad799b4 explicit unknown members at every skeleton record level.

Final .32 logs: `/tmp/n8-3-final.zljxZS/`, result **PASS**, default umask 0022,
TMPDIR unset (/tmp tmpfs); only snapshot copies use private on-disk directories.
The full repo race log is `/tmp/n8-3-final.irF7yR/race.log`; later model/repo
runtime sources are byte-identical (only tests/docs and unrelated go merges).
No timeout increase, sampling, corpus/feature skip, baseline change or dependency.

Reproduction phases (all via rt from this worktree; phase logging omitted):

```sh
rt -- bash -lc 'set -euo pipefail; unset TMPDIR; export CONFIGFLOW_PY=$HOME/cf-test/.venv/bin/python CONFIGFLOW_REPO="$PWD" CONFIGFLOW_SNAPSHOT=$HOME/cf-prod-snapshot/20261116/data; cd server; test -z "$(gofmt -l .)"; go vet ./...; go test -race -p=2 -count=1 ./internal/model ./internal/repo; go test -count=1 -run "^TestStoredRoundTrip$" -v ./internal/repo; go test -p=2 -count=1 ./...; scripts/proddiff.sh; go test -run "^$" -fuzz "^FuzzRecordRoundTrip$" -fuzztime=120s -parallel=2 ./internal/model; go test -run "^$" -fuzz "^FuzzDocumentRoundTrip$" -fuzztime=120s -parallel=2 ./internal/model'
```

- gofmt: no output; vet: PASS.
- Full changed-package race: model 1.031s / repo **319.732s**, default 600s
  timeout unchanged. Start uptime load **0.72/4.60/7.86**. Earlier shared-load
  acceptance also passed repo 367.576s (start load 31.56/29.34/25.05).
- Explicit final fixture + Python-written + full Go + private snapshot round trip:
  PASS **13.744s**, counts 12/12/27/43 rows and private profiles=2. No-op bytes,
  generation/fingerprint and touched row revisions all checked. Private copies=0.
- Full suite: every package PASS. installer **155.585s**, model 0.004s,
  oxidns 66.978s, repo **43.745s**, store 6.041s; final lines:
  `ok configflow/internal/yamlx 2.183s`,
  `? configflow/tools/proddiff/generator [no test files]`.
- Proddiff: all self-checks PASS; 2831 cases / 415 comparisons / 65 GET routes;
  coverage_regressions=0, unmapped_routes=0, **new=0 stale=0**; 11 existing
  baseline differences unchanged. Available snapshot generators all succeed.
- FuzzRecordRoundTrip: PASS **120.110s / 6042777 executions**.
- FuzzDocumentRoundTrip: PASS **120.127s / 3581117 executions**.
- Post-rebase onto R06 integration: gofmt/vet PASS; model race **1.069s**;
  `go test -race -count=1 -run "^TestStoredRoundTrip$|^TestModel" ./internal/repo`
  PASS **77.334s**, including private copy and legacy limitation/logging tests.
  Post-rebase proddiff also PASS: **new=0 stale=0**, coverage_regressions=0,
  unmapped_routes=0 (2831 cases, 415 comparisons).

Runner incidents resolved: one SSH session lost its remote process/exit status
(not counted as a pass), so reran with keepalive and per-phase server logs. .63
then became unreachable; final work stayed on .32. A global runner umask 077
changed expected 0644/0640 test files to 0600; restored default 0022, targeted
store/installer cases and the complete suite passed with no code/expectation
change. Logs are protected by the 0700 mktemp directory, not a global test mask.

Size (new Go files, comments/blanks included): model runtime **1403** lines;
model tests **516** (codec 316, fuzz 200); repo model_rows.go 196;
repo model_rows_test.go 184 / model_roundtrip_test.go 440. N8-3 has no open
implementation finding. The legacy loading restriction above is an explicitly
accepted N8-5 prerequisite, not evidence that legacy APIs already implement R5.
Final rebase/commit/clean-tree check follows the acceptance record; no push.
