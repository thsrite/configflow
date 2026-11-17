# N7-4 — native YAML writers

Branch: `go-n7-4`, initially based on `go` at `d7c481c7`.
Implementation and final acceptance are in progress; this is not a hand-back report.

## Agent push diff

A12 has already migrated this consumer: `deploy/pushdiff/config.go:canonicalYAML`
copies the physical Node graph, drops presentation annotations and calls
`yamlx.Marshal`. Credential spans are found by parsing that canonical output;
there is no `Marked`, `DumpOptions.OnMarked`, or legacy dumper dependency in
`agents` or `deploy/pushdiff`. Keep this implementation rather than reintroducing
a marking writer.

`TestNativeWriterTransition` covers indentation/Unicode, scalar tags (timestamp,
large integer, binary, custom tag and number-looking string), aliases/merge and
multiline/tab credentials. A writer-only change has no semantic rows and no
canonical patch; a subsequent password change still has exactly one semantic row
and remains hidden in the redacted response.

Iteration on `.64`:

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/deploy/pushdiff/writer_test.go -- bash -lc 'cd server && gofmt -w internal/deploy/pushdiff/writer_test.go && go test -count=1 ./internal/deploy/pushdiff && go vet ./internal/deploy/pushdiff'
```

Output: `ok configflow/internal/deploy/pushdiff 1.893s`; vet silent.
A subsequent `.64` `gofmt -l internal/ tools/ && go vet ./...` was silent and exited 0.

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzConfig$" -fuzztime=60s -parallel=4 ./internal/deploy/pushdiff'
```

Output: `PASS; ok configflow/internal/deploy/pushdiff 61.059s`
(59,053 executions, 936 cached seeds plus 29 new interesting inputs).

## Writer inventory before the switch

The inventory includes test/tool callers because deleting the writer must leave
the entire repository buildable. Counts are static call sites, not run counts.

| Package | Runtime writer sites | Test/tool sites or old style types |
|---|---:|---|
| convert | 1 (`GenerateMihomoConfig`) | `IndentDumperOptions` removed (no callers); two strict-byte generation corpora |
| agents/deploy/pushdiff | 0 legacy; A12 already uses Marshal | transition regression added above |
| installer | 3 (Docker compose, compact HA, HA guide) | SharedString, FlowMap, FlowList; wait for N7-3c |
| provider | 1 (`prepareSubscriptionProvider`) | corpus helper |
| mosdns | 1 (`GenerateMosDNSConfig`) | rows helper |
| rulefetch | 1 (YAML normalization fallback) | two test helpers |
| refresh | 1 (cached subscription conversion) | three test helpers |
| routersplit | 1 (target generation) | recorded corpus helper |
| gateway | 1 (rule payload) | — |
| subs | 0 | listener test helper |
| tools/proddiff/generator | 1 (Mihomo bundle adapter) | production snapshot validation tool |
| yamlx | implementation | dump/spec corpora, marked ranges, old type restrictions, non-string mappings |

## Remaining work

- Mihomo implementation/structural corpus comparison completed together; final acceptance remains pending.
- Coordinator approved every remaining writer listed below for N7-4; wait for N7-3c before overlapping mosdns/provider/routersplit/installer changes.
- Installer compose/guide: explicitly deferred until the coordinator announces
  that N7-3c is merged, then rebase first.
- Record the one-time generation byte change (D2/O14); no baseline updates.
- Final gofmt/vet, full race, all tests/difftests, relevant fuzz and proddiff.

## Unused convert wrapper removed

`IndentDumperOptions` had no callers in the repository; removing it is independent
of the output switch. The native writer and structural comparator changes remain
pending together.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/convert/mihomo_nodes.go -- bash -lc 'cd server && gofmt -w internal/convert/mihomo_nodes.go && gofmt -l internal/ tools/ && go vet ./... && go test -count=1 ./internal/convert ./internal/deploy/pushdiff'
```

Output: gofmt/vet silent, `ok configflow/internal/convert 7.034s`,
`ok configflow/internal/deploy/pushdiff 1.892s`.

## Mihomo native writer

`GenerateMihomoConfig` constructs Nodes with `mihomoDocumentNode`, then calls
`yamlx.Marshal`. Repeated physical objects/slices become aliases; cycles and a
40-level exponential shared graph return ErrAliasRecursion/ErrAliasBudget before
output. Scalar strings, bools and numbers keep their intended types, including
big integers and non-finite YAML floats. Timestamp/binary/Mapping are a temporary
boundary for the remaining old convert readers; coordinator explicitly assigned
those reader migrations and removal of these three branches to N7-3d.

Two generation corpus comparisons and their shared review entry now parse both
outputs and compare Equal, retaining strict errors/reports/mutations/text elsewhere.
Native parsing of the old PyYAML text misread **111 rows**, independently confirmed
by real Python safe_load: string-read-as-number56, large-int-read-as-float22,
timestamp-read-as-string33; unknown=0. The old writer emitted text under YAML1.1
rules that Mihomo (yaml.v3) interpreted with different scalar tags. This is the
approved Python bug fix in behaviour-changes. The fixture only overrides these
explicit rows; every other output still compares its native Parse result directly.

Oracle: Python records complete typed physical graphs from 5408 successful old
outputs, then a Go-only parser filter selects the 111 differing old interpretations.
No Go business code participates in recording expected values. Every reference
has corpus/row/output SHA256 and category/field metadata; tests reject stale
references and digest/count drift. Old golden files are unchanged. All 111 rows,
old→new consumer types, source hashes and exact reproduction commands are in
`internal/convert/testdata/mihomo_writer_oracle.md`.

Iteration `.64`:

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/convert/testdata/mihomo_writer_oracle.json.gz --back server/tools/yamlwriteroracle/main.go -- bash -lc 'cd server && gofmt -w tools/yamlwriteroracle/main.go && go run ./tools/yamlwriteroracle -in /tmp/n74-writer-graphs.jsonl -out internal/convert/testdata/mihomo_writer_oracle.json.gz'
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/convert/yaml_writer_test.go --back server/internal/convert/mihomo_corpus_test.go --back server/internal/convert/mihomo_recorded_test.go --back server/internal/convert/review_mihomo_test.go -- bash -lc 'cd server && gofmt -w internal/convert/yaml_writer_test.go internal/convert/mihomo_corpus_test.go internal/convert/mihomo_recorded_test.go internal/convert/review_mihomo_test.go && go test -count=1 ./internal/convert ./internal/yamlx/yamlxtest ./tools/yamlwriteroracle && go vet ./...'
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'cd server && gofmt -l internal/ tools/'
```

Output: oracle `{changed:111,total:5408,unknown:0}`; convert **11.345s**,
yamlxtest **0.005s**, oracle tool builds, whole-repository vet/gofmt silent.
An earlier pre-switch iteration passed convert/pushdiff race (46.774s/25.695s);
it is not final writer acceptance.

## Gateway writer and alias budget

`gateway.dumpPayload` now marshals a typed `payload []string` record. It preserves
empty lists and ambiguous scalar strings. The existing gateway corpus already
compares YAML payload semantics (including its pre-existing approved IP spelling
exception), so no byte comparator was relaxed. A new Parse/Equal regression checks
the entire output and tags.

A19's old 16-level binary alias graph now returns ErrAliasBudget and no output;
a 6-level graph still succeeds with compact shared aliases. Editor/selection
checks remain. Coordinator explicitly approved this security regression update;
the native emitter must not generate a file Mihomo itself cannot materialize.
PROGRESS-agents A19 and behaviour-changes both record the change.

After rebase onto go@d3124353 (including N7-3b), `.64` gofmt/vet pass and
`go test -count=1 ./internal/gateway ./internal/convert` passes:
gateway **1.184s**, convert **10.238s**.

Mihomo writer fuzz `.32`:

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/convert/testdata/fuzz -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzMihomoWriter$" -fuzztime=60s -parallel=4 ./internal/convert'
```

PASS **61.026s**, **348927 executions**. An initial harness failure was invalid
UTF-8: encoding/json replaces it but the legacy reader rejects it before a
builder value exists. The input generator now skips failed legacy parses;
builder errors and semantic mismatches remain fatal. Non-finite YAML values are
covered explicitly in unit tests.

First post-Mihomo-switch proddiff `.64` (before the N7-3b rebase):

```sh
RT_HOST=sunbroke@192.168.0.64 rt -- bash -lc 'bash server/scripts/proddiff.sh'
```

2831 cases / 435 comparisons, all 23 self-checks PASS;
**new=0 stale=0 coverage_regressions=0**. No baseline changes.

## Remaining non-overlapping test writers

After N7-3b rebase, rulefetch's runtime normalization already uses the verified
Edit/Marshal path. Its string-payload test helper now marshals a typed record;
subs listener tests use the existing JSON→Node API then Marshal; refresh's
synthetic subscription feeds build Nodes and Marshal. No third runtime value
bridge was introduced. The refresh helper now reports failures through t.Fatal
instead of panicking.

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/rulefetch/content_test.go --back server/internal/subs/listeners_test.go --back server/internal/refresh/refresh_test.go --back server/internal/refresh/review_c15_test.go --back server/internal/refresh/native_reader_test.go --back server/internal/refresh/routes_test.go -- bash -lc 'cd server && gofmt -w internal/rulefetch/content_test.go internal/subs/listeners_test.go internal/refresh/refresh_test.go internal/refresh/review_c15_test.go internal/refresh/native_reader_test.go internal/refresh/routes_test.go && gofmt -l internal/ tools/ && go vet ./... && go test -count=1 ./internal/rulefetch ./internal/subs ./internal/refresh'
```

Output: gofmt/vet silent; rulefetch **2.984s**, subs **3.703s**, refresh **11.941s**.
Remaining refresh runtime writer awaits the shared transition boundary after
N7-3c integration, alongside mosdns/provider/routersplit/installer and deletion
of the old yamlx emitter.

## Core native writer references prepared

Independent real Python safe_load references cover 1374 old dump values and 21
shared/flow spec values (1395 total), preserving complete typed physical graphs.
Native Marshal→Parse→Equal passes every one; sources/digests are checked. This
prepares the semantic replacement for the old byte option matrix when dump.go/
emitter.go are deleted. The old byte tests remain until that deletion commit.

The Python graph codec is shared by the core and Mihomo fixture generators;
refactoring it preserved the entire 5408-line Mihomo raw recording byte-for-byte
(`cmp` passed). It is fixture tooling, not another runtime value bridge.

`.64` commands:

```sh
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/yamlx/testdata/writer_reference.json.gz -- bash -lc 'cd server && ~/cf-test/.venv/bin/python internal/yamlx/testdata/gen_writer_reference.py && ~/cf-test/.venv/bin/python internal/convert/testdata/gen_writer_oracle.py > /tmp/n74-writer-graphs-refactored.jsonl && cmp /tmp/n74-writer-graphs.jsonl /tmp/n74-writer-graphs-refactored.jsonl'
RT_HOST=sunbroke@192.168.0.64 rt --back server/internal/yamlx/writer_reference_test.go -- bash -lc 'cd server && gofmt -w internal/yamlx/writer_reference_test.go && go test -count=1 ./internal/yamlx -run "^TestNativeWriterPythonReferences$" && go vet ./...'
```

Output: `{dump:1374,spec:21,rows:1395}`, cmp silent; yamlx **0.159s**, vet silent.

Preparation also moved the legacy tag constants from dump.go to resolve.go,
where the old loader still needs them until N7-3d. No loader was removed.
Full convert/yamlx tests after codec sharing passed (10.589s/1.198s);
subsequent `.64` gofmt/vet and yamlx tests after the constant move were silent/PASS
(yamlx 1.300s).
