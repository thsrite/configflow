# Batch-5 pyref metadata refresh

## Scope and base

Branch `go-speed-a-rerecord`, based on the coordinator's frozen final batch-5
`0d43e3f15a9404152435265de4c76cc172e12470`. The original requested base was
`d6fc03226`; after detecting further same-package test additions, the coordinator
explicitly changed the base to `0d43e3f1` and froze it until hand-back.
`batch-5` itself is untouched. All execution uses
`RT_HOST=sunbroke@192.168.0.32`; no full gate or performance measurement.

Only golden source metadata is updated. No Go/Python implementation, comparator,
input, expected output, venv fingerprint or fingerprint policy changes.

## Inventory and changes

All ten A/B golden consumers are included, not just the initially reported
MosDNS corpus failure. Counts below are exact transcript input keys; the two B
payloads each contain many individual cases inside one key.

| Group/package | Golden test | Exact inputs | Change |
|---|---|---:|---|
| A/mosdns | TestCorpus | 2863 | Add shutdown_archive_test.go source hash |
| A/mosdns | TestRecordedPythonTests | 0 | Same source metadata update |
| A/mosdns | TestReviewUnholdableYAMLOutsideTheOutputMatchesPython | 5 | Same source metadata update |
| A/mosdns | TestN73SharedUpstreamMetadataMatchesPython | 1 | Same source metadata update |
| A/oxidns | TestC29ArchivePythonBoundary | 18 | Add review_stepb_test.go source hash |
| A/routersplit | TestRecordedPythonTests | 0 | Unchanged |
| A/routersplit | TestCorpus | 5 | Unchanged |
| A/routersplit | TestEdgeCorpus | 22 | Unchanged |
| B/api/agents | TestReviewConditionalMatchesWerkzeug | 1 | Update agents_test.go; add review_a20_test.go and five A14 test files |
| B/installer | TestReviewDeflateAndZipMatchPython | 1 | Unchanged |
| **Total** | **10 transcripts** | **2916** | **6 metadata updates, 4 unchanged** |

The additional A14 sources are `deploy_content_test.go`,
`deploy_disconnect_test.go`, `deploy_legacy_test.go`, `deploy_test.go` and
`review_a14_test.go`, all directly in `server/internal/api/agents/`.
Backend and oracle-script hashes are unchanged. The zero-entry sessions still
execute their original static-corpus assertions; they are not skipped tests.

Before accepting any update, read-only real-Python checks showed no `PYREF DIFF`
and unchanged old/new entry counts. The first six metadata updates were carried
from the original base; on the frozen final base the conditional consumer needed
the additional five source hashes. That consumer was checked read-only again
before acceptance. All ten consumers are then checked live and replayed on the
final tree.

A pre-record snapshot and post-record comparison verify equality of every
non-metadata JSON field, including the complete entry-key set and every input and
output byte string. The declared-input and venv hashes must also remain equal;
the script restores the snapshot and fails if any of these checks fail. A final
comparison against Git's base goldens is required before committing.

## Commands and evidence

All commands run from the remote `server/` directory via
`RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc '...'` (subsequent phases use
`--no-sync` to preserve the reviewed remote recordings):

```sh
pattern='^(TestCorpus|TestRecordedPythonTests|TestEdgeCorpus|TestN73SharedUpstreamMetadataMatchesPython|TestReviewUnholdableYAMLOutsideTheOutputMatchesPython|TestC29ArchivePythonBoundary|TestReviewConditionalMatchesWerkzeug|TestReviewDeflateAndZipMatchPython)$'
packages=(./internal/mosdns ./internal/oxidns ./internal/routersplit ./internal/api/agents ./internal/installer)

# Read-only check before accepting the additional final-tree metadata.
PYREF_MODE=check go test -race -count=1 -p 2 -timeout=30m -v \
  -run '^TestReviewConditionalMatchesWerkzeug$' ./internal/api/agents
# Expected failure: metadata only; no changed output and old=1/new=1.

# Accept the reviewed metadata, then compare against the saved byte-level snapshot.
PYREF_MODE=record PYREF_ACCEPT=1 go test -race -count=1 -p 2 -timeout=30m -v \
  -run '^TestReviewConditionalMatchesWerkzeug$' ./internal/api/agents

# All A/B consumers, real Python first, ordinary replay second.
PYREF_MODE=check go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
env -u PYREF_MODE -u PYREF_ACCEPT go test -race -count=1 -p 2 -timeout=30m -v -run "$pattern" "${packages[@]}"
```

Final logs: `.32:~/.cache/pyref-batch5-rerecord-0d43e3f1/`, including
`check-conditional-before.log`, `record.log`, `check-after.log` and
`replay-after.log`. Earlier `d6fc0322` logs are historical evidence only.

Final-tree results (2026-11-18, `.32`, `archlinux`):

```text
PASS: final merged tree, 10/10 live equal, 2916 exact inputs, 10/10 ordinary replay
Git base 0d43e3f1: 10/10 transcript bodies byte-identical; 6 source-metadata-only changes; all recorded source hashes match final tree
```

| Package | Live check, race | Ordinary replay, race |
|---|---:|---:|
| mosdns | 79.079s | 28.121s |
| oxidns | 1.605s | 1.315s |
| routersplit | 6.108s | 5.914s |
| api/agents | 1.681s | 1.322s |
| installer | 23.567s | 21.625s |

These are validation durations, not A's postponed formal before/after timings.
The final phase ran 05:07:11–05:09:07 UTC; one-minute load was 2.71 at start and
2.97 at end. The final replay output ends with:

```text
ok   configflow/internal/routersplit  5.914s
ok   configflow/internal/api/agents   1.322s
ok   configflow/internal/installer    21.625s
```

Final artifact review decodes the ten Git-base/current JSON documents, requires
every field except `metadata.sources` to match, restricts source deltas to the
nine named test files above, and checks every recorded source hash against the
final tree. All checks passed. `git diff --check` is clean. After the tests,
`ps -eo pid,ppid,args` filtered for the four reference script names found no
remaining Python oracle processes. No Go files changed, and no full gate ran.

## Recommendation only: narrow the source fingerprint

The immediate cause is `sourceMetadata` automatically globbing every
`*_test.go` in the consuming package. Adding an unrelated test therefore changes
every golden in that package. This refresh deliberately retains that policy.

A follow-up should remove the automatic package-wide test glob and explicitly
declare each oracle's input-generating test/helper files and fixture sources
through the shared Spec/NewLines API. Keep the oracle script, relevant backend
dependencies, venv fingerprint and exact serialized input keys in the hash.
For one-shot oracles, continue hashing the complete actual input payload, not
merely an RNG seed or temporary path. Shared input helpers must be declared too.
Keep the external-backend/venv cache fingerprint and its runtime validation.

Using only the oracle script and exact inputs is another possible policy, but
explicit input-source declarations also document normalization and generation
dependencies. Prefer that narrower declared set over the whole test package.
Acceptance for the follow-up should prove that adding an unrelated test leaves
the golden valid, while changing a declared generator, helper, fixture, oracle,
backend dependency, input or venv still fails. No such implementation is made in
this commit.
