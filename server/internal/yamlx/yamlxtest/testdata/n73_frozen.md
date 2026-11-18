# Retired reader comparison evidence

`n73_frozen.json.gz` was captured on test servers while the two real Go readers
still existed. Old implementation/classifier revision:7d58ac61 (unchanged through
852a7fc6); capture changes only added recording, not reader/classification logic.
There is no legacy loader implementation hidden in these fixtures.

Capture algorithm, for every actual source sent to the original Classify:

1. Call the original old/new Classify and require no unclassified result.
2. Record source/SHA, class and baseline revision.
3. Record current Parse's full physical tag/position/alias audit or native error.
4. Call the actual former LoadWith with KeepNonStringKeys/KeepUnrepresentable;
   record its error or its graph from the then-production yamlbridge.Encode.
5. Write one record keyed by exact source SHA. Repeated sources must describe
   the same result. Deduplicate only after all caller occurrence gates complete.

Exact capture inputs were produced by these commands on .32, with a temporary
recording-only Classify wrapper and CONFIGFLOW_YAML_FREEZE=/tmp/n73d-freeze:

```sh
cd server
go test -json -p=4 -count=1 ./internal/yamlx/yamltest ./internal/gateway ./internal/mosdns ./internal/routersplit
go test -json -p=4 -count=1 -run 'ReaderMigrationGate$' ./internal/routers ./internal/provider ./internal/installer ./internal/discovery
```

The same wrapper in tools/yamlreaderoracle was then fed every3,837 source/mode
from convert/testdata/n73d_reader_values.json.gz. This invokes only production
Parse/projector/physical transition, never Go business functions. The original
per-caller tests retain their input selection and occurrence counts. The first
capture run's gateway depth16 generation assertion exposed the new earlier
reader rejection; preserving its typed cause fixed it, and the full gateway
suite then passed. All classification captures themselves were unknown0.

21,343 unique sources, old/new graphs/errors saved; the gzip SHA is pinned in
history.go. Unrecorded sources, changed native graphs/diagnostics, revisions,
archive bytes or counts fail. Native-only regressions remain live tests; the
retired API-only tests are superseded by this source-by-source gate.

`reader_history.json.gz` separately records mode-specific old messages/classes
for7,174 N7-3a/A19 source/mode pairs from the unchanged native-value fixtures.
It also records those whole fixture files' SHA256. CheckReaderFixtures verifies
the exact original bytes, historical mode/error, raw live Parse graph, actual
production projector, and explicit expected-change accounting. Native recorder
reruns use this frozen old half and recalculate the native half independently.
To regenerate the retired half for a new migration audit, use a checkout at the
baseline revision; do not resurrect private parser/constructor code here.

## Complete replay and compact caller lookup (N7-3d performance follow-up)

The original archive above is unchanged. `history_test.go` alone embeds and
streams every21,343 record for `TestAllFrozenReaderSources`; it checks source
SHA/revision/unknown status, live Parse's entire physical graph or exact error,
and every entry of the compact caller index. Duplicate IDs and missing entries
fail. The ordinary `go test ./...` gate always executes this complete test.

`n73_index.json.gz` is derived data, not a new oracle. Generate on a test server:

```sh
cd server/internal/yamlx/yamlxtest
go generate
sha256sum testdata/n73_index.json.gz
```

The generator reads only the original checksum-pinned archive; no YAML loader,
business function or Python implementation runs. Each index entry preserves its
source SHA, class, exact native diagnostic and SHA256 of the canonical typed
physical audit JSON (tags, values, source positions, ordered child IDs, alias IDs
and nil/empty distinctions). Both archive and index checksums are pinned. The
full replay cross-checks the generator's JSON wire schema against ParseAudit.
Index SHA:`47e7732d7e632eacae626e6a99ecc2e136a5873c8d2297a68c0c8e5f9c45134b`.

Classify loads only this index, then runs live Parse/audit on the caller's actual
source. All caller selection logic and occurrence counts remain unchanged;
unrecorded sources still fail. This avoids decoding/retaining the full archive
in every test binary without dropping or sampling any historical coverage.
Detailed measurements: `server/parity/progress/n73-gate-perf.md`.
