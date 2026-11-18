# Explicit golden source declarations

Completed on `go-pyref-narrow`, based on `go@c5f5ff349`. All tests, recording,
vet and formatting checks ran on **192.168.0.32**. No full gate, product-code
change, new dependency, push or formal timing run.

## Implementation

`tools/pyref/sources.json` explicitly declares all ten current references.
`sourceMetadata` no longer globs the consuming package's test files. `NewLines`
loads and audits the declaration; the two one-shot consumers use
`testgolden.Sources(t, backend, oracle)`. Declared sources retain strong byte
hashes. Backend Python files, oracle scripts, exact input bytes, native venv
metadata and the external-input cache fingerprint remain checked.

The native source audit follows test entry points, local helper declarations,
function values, methods, init/TestMain and non-pure initialization. It includes
ordinary imported harness-package source, transitive helper imports, embedded
assets and identifiable input files. Local binding/import shadowing is handled;
known lazy sync.Once constructors are traversed when their result is used.
Declaration granularity is a source file or imported harness package, not a
runtime coverage sample. The audit itself executes neither Go nor Python.

Products under test have reviewed local symbol and imported-package/member
boundaries. They are not mistaken for reference-generation helpers: normal
product fixes must still run against the same Python answers. A new ordinary Go
helper or new member of an existing product import is unclassified until a
helper source or reviewed product boundary is declared. Unused product entries
are rejected. Source suggestions never approve unknown symbols as product and
never write; they require manual classification and review.

`TestAuditRepository` checks source declarations for both groups, including
unregistered/indirect constructors and one-shot references that omit the audited
Sources call. Every consumer also audits its own dependencies before replay or
recording. Parsed ASTs are shared within one test process; actual source bytes
are still freshly hashed for each reference.

Shared corpus readers now use `testgolden.Open/ReadFile`. Actual repository
reads must appear in that consumer's declaration, including dynamic paths and
symlinks in either direction. Native source audit rejects raw os/ioutil/fs
reads in reachable local harness code, including aliases/function values.
Non-golden callers of shared loaders retain ordinary I/O. Temporary materialized
payloads remain covered by exact input hashes.

Ordinary replay additionally rejects unconsumed recorded inputs, preventing a
branch change from quietly shrinking the previously recorded comparison set.
No assertions, cases or timeouts were removed or weakened.

## Declaration results

The historical unrelated `shutdown_archive_test.go`, `review_stepb_test.go`,
`agents_test.go`, `review_a20_test.go` and A14 deploy/review additions are absent
from these consumers' source maps. Examples of the actual test-source closure:

| Consumers | Declared test files |
|---|---|
| MosDNS corpus | rows_test.go, n73_reference_test.go |
| MosDNS recorded/review/shared-upstream | The above plus the respective recorded_test.go, review_c21_test.go or n73_yaml_test.go |
| OxiDNS archive | archive_test.go plus actual TestMain and external init files |
| RouterSplit three transcripts | recorded_test.go, n73_reference_test.go |
| Agent conditional response | a10_review_internal_test.go |
| Installer deflate/ZIP | review_a10_test.go |

Corpus files and helper-package embedded indexes are explicitly listed too.
The manifest itself is not added to every golden's file hash: its resolved
per-reference source set is bound by metadata, and its fields are read/audited
on execution. Adding a separate reference therefore does not invalidate all
existing answers.

## Validation and negative controls

Persistent native tests prove all of these:

- Adding a standalone unrelated test leaves the required source set unchanged.
- Omitting a local helper, imported helper Go file, ordinary Go helper or
  embedded/input fixture is rejected; a declared ordinary helper is accepted.
- New imported members cannot hide behind an existing product-package boundary;
  the suggestion tool refuses to silently classify a missing helper as product.
- Raw read aliases/function values, indirect constructors and misleading nested
  Sources fields are rejected.
- Dynamic undeclared reads and both symlink directions are rejected.
- Real child-Go normal replay rejects changes to each declared driver/helper/
  fixture, while accepting an unrelated file. Existing source/input/venv,
  failed-assertion, no-acceptance and no-reference-execution controls still pass.
- Skipping a recorded exact input is detected by normal replay cleanup.

Actual merged-tree controls on `.32` also passed, with restoration in `finally`:

```text
control-unrelated-files: PASS
control-missing-helper: expected rejection detected
control-missing-fixture: expected rejection detected
control-fixture-drift: expected rejection detected
control-restored-audit: PASS
```

Three temporary unrelated files were added to MosDNS, OxiDNS and api/agents:
all six relevant consumers passed without re-recording. A temporary helper
called by the real MosDNS corpus test failed specifically with `undeclared
sources` naming that new helper. Dropping its corpus fixture from the manifest
also failed the declaration audit. Appending synthetic bytes to the declared
fixture failed the source-hash guard before replay. All original files were
restored, A/B audit passed again and golden bytes remained unchanged.

All ten goldens were migrated with real Python: **2916 exact keys, byte-identical
inputs and Python outputs**. Two fallback sessions have zero live keys and keep
their original static-corpus assertions. Before accepting metadata, read-only
live checks showed no output, input or venv drift. After acceptance, every JSON
field except `metadata.sources` was compared against the saved originals and
Git `c5f5ff34`; all matched. Every new source digest matches the final local tree.

Post-migration real live checks and ordinary replay both pass all ten consumers:

| Package | Live check (-race) | Ordinary replay (-race, strace) |
|---|---:|---:|
| mosdns | 85.242s | 32.020s |
| oxidns | 2.047s | 1.810s |
| routersplit | 6.907s | 7.216s |
| api/agents | 1.795s | 1.671s |
| installer | 24.501s | 23.143s |

These are validation durations, not formal performance measurements. Ordinary
replay's execve trace contains **zero Python/PyPy executions**. Helper and actual
repository audit suites pass under race (A: 12 sites/6 scopes; B: 33 sites/5
scopes). Targeted vet and changed-file gofmt checks pass.

## Commands and logs

Commands run through `RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '...'`;
subsequent phases use `--no-sync` until recorded artifacts are copied back.
Inside `server/`:

```sh
go test -race -count=1 -v ./internal/testgolden ./tools/pyref
go vet ./internal/testgolden ./tools/pyref ./internal/mosdns ./internal/oxidns ./internal/routersplit ./internal/api/agents ./internal/installer

pattern='^(TestCorpus|TestRecordedPythonTests|TestEdgeCorpus|TestN73SharedUpstreamMetadataMatchesPython|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestC29ArchivePythonBoundary|TestReviewConditionalMatchesWerkzeug|TestReviewDeflateAndZipMatchPython)$'
packages=(./internal/mosdns ./internal/oxidns ./internal/routersplit ./internal/api/agents ./internal/installer)
PYREF_MODE=check go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
# Only after reviewing the metadata-only differences:
PYREF_MODE=record PYREF_ACCEPT=1 go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
PYREF_MODE=check go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
env -u PYREF_MODE -u PYREF_ACCEPT strace -f -e execve -o "$HOME/.cache/pyref-narrow-c5f5ff34/replay.execve" go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
```

Logs: `.32:~/.cache/pyref-narrow-c5f5ff34/` contains helpers-final.log,
check-final-plan-before.log, record.log, check-after.log, replay-after.log,
replay.execve and the five control-*.log files. The interrupted recording was
inspected and its saved before/after artifacts verified before resuming checks;
no result was assumed from the interrupted command.

Final output:

```text
PASS: 10/10 live equal; 10/10 replay; 2916 exact keys; zero Python exec in ordinary tests
PASS: unrelated files accepted (6 consumers); missing helper/missing fixture/fixture drift rejected; A+B audit restored; all golden bytes unchanged
```

The scheduled after-23:00 A/B timing order is unchanged; no timing claim is made
here. The coordinator owns the full merge gate.
