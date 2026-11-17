# N8-3 — model skeleton and byte-preserving record codec

Status: implementation complete; final acceptance after slice-cycle fix pending.
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

Buildable steps before final rebase: 79481211 codec; f3355a10 skeleton/type guards;
3b87a2af repo integration/round trips; 7aada095 independent fuzz assertions.
Next commit fixes the adversarial slice-alias cycle false positive (no design
change). .63 gofmt/vet, model race PASS 1.034s, fixture + private Python round trips
PASS 9.620s after this fix.

First .32 acceptance (before the slice fix) passed: gofmt/vet, model race 1.041s,
repo race 367.576s (uptime load 31.56/29.34/25.05 at start), explicit private round
trip 12.216s, full suite (repo 57.280s, store 6.862s, oxidns 103.646s, installer
187.223s; final yamlx 1.481s), proddiff 2831 cases/415 comparisons/65 GET routes,
coverage_regressions=0, unmapped_routes=0, new=0 stale=0 (11 existing differences).
Final post-fix .32 commands/results, 120s fuzz targets, cleanup/rebase/clean
worktree check still pending; do not treat this as verified.

Additional explicit skeleton coverage retains unknown members at root, System,
Shared and Profile levels through a typed edit. .32 gofmt/vet + model race PASS
1.039s; native/legacy-constraint/log-dedup/private round-trip selection PASS
15.885s. Final post-fix repo race already PASS 319.732s (start uptime load
0.72/4.60/7.86). A runner-only global umask 077 caused unrelated installer/store
file-mode assertions to fail; remove that override, retain private logs inside
a 0700 mktemp directory. Default umask 0022 targeted reruns PASS (store 0.003s,
installer 2.190s), no implementation or expected-mode change. One prior SSH
session lost its remote process/result (not counted as passing); .63 subsequently
became unreachable, so remaining checks run on .32. Final corrected-mask full
suite, proddiff and post-fix 120s fuzz results are still pending.
