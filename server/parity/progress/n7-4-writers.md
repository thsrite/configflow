# N7-4 — native YAML writers

Branch: `go-n7-4`, initially based on `go` at `d7c481c7`.
Implementation and acceptance complete; coordinator verification pending.
Final integration base: `go@64815165`. No push. N7-3d remains a separate follow-up.

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
| installer | 3 (Docker compose, compact HA, HA guide) | now native Nodes after N7-3c rebase |
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

N7-4 has no implementation or acceptance work remaining. Coordinator verification
and merge follow. N7-3d retains convert reader migration, importer/rules bare
decoders, legacy loader/pyyaml deletion, bridge legacy-branch cleanup, helper
consolidation and grep gate. OxiDNS's two readers are assigned to C29. Historical
package notes below are superseded by the final integration results at the end.

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

## Unique transition graph bridge after N7-3c

Rebased cleanly onto go@65c0e6d4 (N7-3c merged). The private convert and mosdns
builders were removed; `internal/yamlbridge.Encode` is the single temporary
implementation. yamlx's public API was not expanded. All corresponding tests
moved to the shared package. Its package comment explicitly marks **N7-3d 删除**
for Timestamp/[]byte/Mapping support; the remaining jsonx boundary is removed
with N8's consumer migrations.

It traverses each physical input container once, builds explicit shared aliases
before validation, copies native user Nodes, promotes orphan targets to a first
definition and disambiguates colliding source anchor names. It preserves source
annotations and never mutates input graphs. This order also rejects a 40-level
shared physical graph that has no explicit AliasNodes yet, instead of expanding
it during an early budget check. Tests cover legacy values, 40-level sharing,
object/list/native-container/pure-alias recursion, orphan definitions, multiple
source anchors and source ownership. The former writer fuzz is now
`yamlbridge.FuzzEncode` and will be rerun in the final gates.

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/yamlbridge --back server/internal/convert/mihomo_generate.go --back server/internal/mosdns/generate.go --back server/internal/mosdns/rows_test.go --back server/internal/mosdns/review_c21_test.go --back server/internal/mosdns/yaml_adapter.go -- bash -lc 'cd server && gofmt -w internal/yamlbridge internal/convert/mihomo_generate.go internal/mosdns/generate.go internal/mosdns/rows_test.go internal/mosdns/review_c21_test.go internal/mosdns/yaml_adapter.go && gofmt -l internal/ tools/ && go vet ./... && go test -count=1 ./internal/yamlbridge ./internal/convert ./internal/mosdns'
```

Output: fmt/vet silent; yamlbridge **0.014s**, convert **27.593s**, mosdns **96.457s**.

The preceding pre-c-integration race iteration on `.64` passed all six packages:
convert91.520s/gateway18.656s/yamlx13.508s/rulefetch22.627s/subs42.750s/refresh272.300s.
It is iteration evidence only; full final race remains pending.

## Remaining provider/refresh/router/tool writers

`provider.prepareSubscriptionProvider`, `refresh.FetchSubscriptionSource` cached
conversion, `routersplit.dump` and the proddiff bundle adapter now build their
Node through the one yamlbridge and call Marshal. Source order is retained;
old sort/ASCII/width presets are gone. MosDNS already called Marshal in N7-3c;
its private bridge was removed in the preceding commit.

Provider corpus results and generated `profiles/*/providers/*.yaml` files now
compare Parse/Equal, while cache updates, errors, other metadata and stored JSON
file bytes stay strict. Refresh's recorded `fetch_subscription_source` compares
only result text `[0]` as YAML; source/traffic/time and every other output field
stay strict. Negative tests reject type/value/extra-field changes, cache metadata
changes, missing files, and even a whitespace-only change in stored JSON.

The N7-3c test classifier also stopped using the deleted writer: old loader
values go through the same validated bridge. This exposed one pre-existing
checker false-equality: old string `0X1F` was emitted bare and misread as int.
The existing implicit typing class now uses actual node tag differences for
unchanged scalar text, with all other fields equal; no new lexical scanner or
normalization. A dedicated regression covers core and router caller shapes.
This changes a migration test's diagnosis, not reader behavior.

`.64` full fmt/vet and changed-package test output:

```text
configflow/tools/proddiff/generator PASS 0.083s
configflow/internal/yamlx/yamltest PASS 0.553s
configflow/internal/routersplit PASS 7.304s
configflow/internal/provider PASS 28.395s
configflow/internal/refresh PASS 35.459s
```

Command uses `go test -json -count=1` for the five packages above, recorded in
`/tmp/n74-writer-gate.jsonl`; only test/package pass/fail metadata is printed to
avoid dumping entire fixture documents. Initial expected byte mismatches were
replaced by the narrow structural comparisons described above; unknown reader
classifications remain zero. Subsequent comparator-negative tests pass (0.008s).

## Writer oracle transport

The coordinator approved an installer writer-layer oracle: patch only real
Python yaml.dump/safe_dump, persist its actual typed input graph before rendering,
render through native Marshal, persist those bytes before returning them to the
unchanged Python postprocessor. No business-source line or non-writer hook changes.
The graph transport now includes explicit flow styles and shared image anchors.
The Go tool's streaming mode invokes only graph reconstruction, yamlbridge.Encode,
Marshal and Parse; no Go business generator participates in expected values.

Classification records changed paths and restored character counts separately
for U+0085 (NEL), U+2028 and U+2029, all explicitly approved. Negative tool tests
reject unrelated values, fields and sequence changes. Existing safe_load graph
fixtures keep their original codec behavior when writer_styles is false.

`.32` changed-tool vet/tests PASS:
`go vet ./tools/yamlwriteroracle ./internal/yamlx/yamlxtest`;
`go test -p=2 -count=1 ./tools/yamlwriteroracle ./internal/yamlx/yamlxtest`
(tool0.007s, yamlxtest0.005s). Installer fixture generation and full acceptance
remain in progress.


## Installer migration and legacy emitter removal

The three installer emission sites now use the shared bridge and Marshal.
Compact Compose uses Node flow styles and the existing agent_image anchor,
without changing its annotation or business validation. No old writer API calls
remain; dump.go/emitter.go/types.go are deleted. The old loader is intentionally
retained for N7-3d. The native core semantic reference corpus replaces the removed
writer-option byte matrix in the same change.

The complete installer writer-only oracle is documented beside its generator:
[fixture provenance](../../internal/installer/testdata/n74_writer_oracle.md).
It verifies 8,623 original rows before injection; 8,584 changed rows are fully
classified (8,127 layout,21 string type,436 Unicode),39 unchanged,unknown=0.
The 8,368 unique changed calls retain source rows/digests, 12,870 physical typed
graphs and native bytes; 1,635 semantic/hash-only cases include complete results.
Layout-only multi-field cases reuse the unchanged complete old semantic
expectation, with strict non-YAML fields. Source/graph/case/result digests and
every graph's re-rendered bytes have a dedicated provenance test.

NEL/LS/PS are counted separately by row, changed field and restored occurrence:
U+0085 345/1856/2020; U+2028 53/255/285; U+2029 47/213/231 (rows overlap).
The writer preserves these values through unchanged Python postprocessing and
native Compose parsing. Regression inputs exercise all three generation modes.
Numeric-looking image strings retain strings and sharing. Raw Compose and ZIP
comparisons now check Parse/Equal including scalar tags; metadata, member names,
permissions and non-YAML bytes stay strict. Negative tests cover timestamp,
binary, nonfinite float, custom tag and large integer versus string.

Generation command (.64, exit0):
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=2 -- bash -lc 'set -e; cd server; go build -o /tmp/n74-yamlwriter ./tools/yamlwriteroracle; cd ..; ~/cf-test/.venv/bin/python server/internal/installer/testdata/n74_writer_oracle.py /tmp/n74-yamlwriter'
```
Output: checked8623,unique_expected8368,full_expected1635,graphs12870,unknown0.

The first affected-corpus/provenance/Unicode iteration used:
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=2 -- bash -lc 'cd server; go vet ./internal/installer; go test -json -count=1 ./internal/installer -run "^(TestComposeCorpusMatchesPython|TestDockerHAReviewCorpusMatchesPython|TestFuzzCorpusMatchesPython|TestRenderMatrixMatchesPython|TestN73ComposeHashOnlyReviewCases|TestComposeReviewCorpusMatchesPython|TestComposeWriterPreserves.*|TestWriterOracleProvenance)$"'
```
Output: installer PASS33.825s, vet silent. Strict tag-comparator iteration and
final merge gates follow below. No golden/baseline edits.


Final pre-rebase scoped validation (.64, exit0):
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=2 -- bash -lc 'set -e; cd server; gofmt -l internal/ tools/; go vet ./internal/yamlx/... ./internal/yamlbridge ./internal/installer ./tools/yamlwriteroracle; go test -p=2 -count=1 ./internal/yamlx/... ./internal/yamlbridge ./tools/yamlwriteroracle'
```
Output: fmt/vet silent; yamlx1.332s/yamltest0.119s/yamlxtest0.002s/
yamlbridge0.005s/yamlwriteroracle0.004s PASS. The second strict installer
comparison run (same affected-corpus regex plus DockerHACorpus/recorded and tag
negative tests, JSON log `/tmp/n74-installer-strict.jsonl`) passed16.702s.


## HTTP Compose comparator completion

Queue-time review found the plain Compose download still compared bytes.
It now uses Parse/Equal only for successful text/yaml responses; status and all
existing headers remain strict. Guide and ZIP comparison now also retain scalar
tags. Negative cases include timestamp/binary tags, value/list order, bad YAML
and changed headers.

The first scoped HTTP run passed DockerHA9.320s and Installer3.550s but exposed
four deterministic installer HTTP-fuzz NEL paths. These are the approved writer
bug, not new behavior. The explicit HTTP writer-only extension records actual
Flask route responses and one typed writer call per URI, validates the old SHA,
and injects native bytes into unchanged Python. Four rows/seven fields/seven NEL
occurrences are classified; unknown=0. Every other HTTP-fuzz row stays strict.
The exact URI/live source SHA/status/type is mandatory before selecting the
fixture, and exactly four named expectations must pass. Provenance and generation:
[HTTP writer oracle](../../internal/api/agents/difftest/testdata/n74_writer_http_oracle.md).
The original full gate was still waiting for a slot when stopped for this test
completion; no full race or proddiff had started.

Scoped HTTP iteration (.32, exit0; fmt/vet silent):
```sh
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=3 -- bash -lc 'cd server; go vet ./internal/api/agents/difftest; CONFIGFLOW_DIFF=1 go test -json -count=1 ./internal/api/agents/difftest -run "Test(NativeComposeHTTPComparison|NativeWriterHTTPProvenance|N73GuideComposeComparison|Installer|DockerHA)"'
```
Output: configflow/internal/api/agents/difftest PASS44.337s.

## Final integrated gate

Host: **192.168.0.64**, 20 GB + swap, `-p 6`, GOMAXPROCS=6.
Integration base at launch: `go@5680fbb3`. Uses the coordinator's shared
2-slot gate; the initial `-p4` and first `-p6` invocations were stopped while
still queued, before any full race/proddiff started. The following is the one
actual final full-race invocation:

```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=6 -e GATE_FUZZ_PARALLEL=1 -e GATE_LOGS=/tmp/n74-final-gate -- bash server/scripts/gate.sh -p 6 FuzzEncode:./internal/yamlbridge FuzzNativeRoundTrip:./internal/yamlx FuzzNativeNoPanic:./internal/yamlx FuzzComposeYAML:./internal/installer FuzzProviderYAML:./internal/provider FuzzRouterSplitYAML:./internal/routersplit FuzzMosDNSYAML:./internal/mosdns FuzzSourceYAMLPolicy:./internal/refresh FuzzConfig:./internal/deploy/pushdiff
```

The gate runs `gofmt -l internal/ tools/ cmd/`, `go vet ./...`, build/vet/lifecycle
checks with both configflow_proddiff and configflow_difftest tags, then the full
`go test -race -p=6 -count=1 -timeout=60m ./...` and proddiff. The full race run
executes the complete suite; the gate intentionally has no redundant non-race
full run. Enabled difftests run with `CONFIGFLOW_DIFF=1`, followed by all nine
listed fuzz targets for 60 seconds each, one worker per target.

Confirmed so far: gofmt/vet/tag checks passed. Proddiff exit0:
`cases=2831 compared=462 new=0 stale=0 coverage_regressions=0`, all23 selfchecks
PASS; aggregation-provider2/2 succeeded. Full race/difftest/fuzz all passed; final output `GATE PASS`.


First gate final output (.64, exit0):
```text
baseline: new=0 stale=0
== difftest
12 packages PASS (agents110.391s, subscriptions46.266s, others0.449–16.650s)
== fuzz (9 targets, 60s each, concurrent)
FuzzEncode ok configflow/internal/yamlbridge 61.013s
FuzzNativeRoundTrip ok configflow/internal/yamlx 61.046s
FuzzNativeNoPanic ok configflow/internal/yamlx 61.051s
FuzzComposeYAML ok configflow/internal/installer 61.002s
FuzzProviderYAML ok configflow/internal/provider 61.031s
FuzzRouterSplitYAML ok configflow/internal/routersplit 61.042s
FuzzMosDNSYAML ok configflow/internal/mosdns 60.101s
FuzzSourceYAMLPolicy ok configflow/internal/refresh 61.022s
FuzzConfig ok configflow/internal/deploy/pushdiff 61.056s
GATE PASS
```
Full race: all88 test packages passed, including convert115.924s,
installer507.446s/provider168.265s/refresh232.089s/mosdns157.392s/
routersplit11.880s/gateway24.053s/pushdiff59.684s/yamlbridge1.016s/yamlx7.904s.
Logs retrieved to ignored `.ci-results/n74-gate-5680/` with:
```sh
RT_HOST=sunbroke@192.168.0.64 rt --no-sync --back .ci-results/n74-gate-5680 -- bash -lc 'mkdir -p .ci-results/n74-gate-5680; cp /tmp/n74-final-gate/* .ci-results/n74-gate-5680/'
```

During this run go advanced to0f943623: provider's negative test timeout now
waits for completed output in proddiff, production's positive deadline is
unchanged; a new provider/difftest package was added. Rebase and a second
integrated gate are warranted by this upstream change. Writer code and the nine
passing fuzz targets are unchanged, so the next gate omits repeated fuzz.


## Final rebase onto N8-6

Coordinator merged N8-6 at64815165 and explicitly requested only vet plus changed
packages after this non-overlapping integration. The provider-integration second
full gate was still queued and was canceled before any race/proddiff started.
Final base is `go@64815165`; rebase was conflict-free. The one completed full
race/proddiff/fuzz gate above remains the full acceptance evidence.

Post-rebase commands (all exit0):
```sh
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=6 -- bash -lc 'set -e; cd server; gofmt -l internal/ tools/ cmd/; go vet ./...; go vet -tags configflow_proddiff ./cmd/...; go test -tags configflow_proddiff -count=1 ./cmd/configflow -run "Test.*Manifest"; go test -p=6 -count=1 ./internal/yamlx/... ./internal/yamlbridge ./internal/convert ./internal/deploy/pushdiff ./internal/gateway ./internal/installer ./internal/provider ./internal/mosdns ./internal/refresh ./internal/routersplit ./internal/rulefetch ./internal/subs ./tools/proddiff/generator ./tools/yamlwriteroracle > /tmp/n74-rebase-packages.log; tail -n 20 /tmp/n74-rebase-packages.log'
RT_HOST=sunbroke@192.168.0.64 rt --no-sync -e GOMAXPROCS=6 -- bash -lc 'cd server && go test -tags configflow_proddiff -count=1 ./cmd/configflow -run "^TestProddiffWaitsForFreshProvider$"'
RT_HOST=sunbroke@192.168.0.32 rt -e GOMAXPROCS=4 -- bash -lc 'set -e; cd server; CONFIGFLOW_DIFF=1 go test -p=4 -count=1 -timeout=40m $(go list ./... | grep difftest) > /tmp/n74-rebase-diff.log; cat /tmp/n74-rebase-diff.log'
```
The first tagged command used a non-matching test filter and reported “no tests
to run”; it is not counted as verification. The exact new provider manifest
regression is run by the second command. All13 enabled differential packages
are rerun, including the newly merged provider/difftest.


Post-rebase final output:
```text
# .64: gofmt/vet silent; changed packages
ok configflow/internal/yamlx 1.963s
ok configflow/internal/yamlx/yamltest 0.264s
ok configflow/internal/yamlx/yamlxtest 0.003s
ok configflow/internal/yamlbridge 0.006s
ok configflow/internal/convert 15.173s
ok configflow/internal/deploy/pushdiff 4.138s
ok configflow/internal/gateway 2.088s
ok configflow/internal/installer 169.571s
ok configflow/internal/provider 19.545s
ok configflow/internal/mosdns 54.236s
ok configflow/internal/refresh 22.363s
ok configflow/internal/routersplit 3.407s
ok configflow/internal/rulefetch 4.363s
ok configflow/internal/subs 5.401s
ok configflow/tools/proddiff/generator 0.012s
ok configflow/tools/yamlwriteroracle 0.003s
# .64: exact tagged TestProddiffWaitsForFreshProvider
ok configflow/cmd/configflow 0.068s
# .32: CONFIGFLOW_DIFF=1, all13 packages
ok configflow/internal/api/agents/difftest 165.174s
ok configflow/internal/api/aggregations/difftest 46.821s
ok configflow/internal/api/subscriptions/difftest 137.329s
ok configflow/internal/devicenotes/difftest 1.177s
ok configflow/internal/discovery/difftest 5.217s
ok configflow/internal/health/difftest 15.828s
ok configflow/internal/httpx/difftest 4.110s
ok configflow/internal/mosdns/difftest 5.368s
ok configflow/internal/provider/difftest 51.280s
ok configflow/internal/refresh/difftest 5.794s
ok configflow/internal/subs/difftest 3.062s
ok configflow/internal/substore/difftest 11.880s
ok configflow/internal/webdav/difftest 29.581s
```
Logs retrieved to ignored `.ci-results/n74-rebase-packages/` and
`.ci-results/n74-rebase-diff/` using the matching pinned host's
`rt --no-sync --back` after completion. No test process remains running.

Fuzz execution counts (all60s, PASS): Encode668276; NativeRoundTrip11997;
NativeNoPanic19379; Compose9569; Provider2148; RouterSplit15832;
MosDNS37157; SourceYAMLPolicy85561; Config115699. Depth40 sharing and
object/list/native/pure-alias recursion are explicit unit regressions; invalid
graphs return typed alias errors before output, never null/truncated output.

Design correspondence: §2.2 owns all emission through native Marshal; N7-4's
entire caller inventory migrated, core old emitter/style types removed; §5.3
uses narrow structural outputs and independent Python typed oracles; D2/O14
records the one-time generated-byte/full-push change. Approved Python type and
NEL/LS/PS writer bug fixes plus the unified alias budget are in behaviour-changes.
All old golden/baseline files remain unchanged. The production snapshot has no
new/stale differences. No API field/status or storage-identity migration occurs.

Implementation commits after final rebase (record-only final handoff follows):
```text
c80e55f1 deploy transition regression
933fc656 remove unused convert options
cb1d4f86 Mihomo writer and independent Python semantic references
d3c61886 fuzz actual legacy input values
3bfb4975 typed gateway payload and alias budget
1221f363 rule/subscription test writer migration
8473075f core1395 semantic references
ac996f76 sole physical graph bridge
56750be6 provider/refresh/router/tool writers
5a5ee997 writer-only oracle transport
591317d4 installer native writers and old emitter removal
70072b5a strict HTTP YAML comparison and explicit4 NEL references
e10153f6 first complete gate record
```
Final `git rebase go` and clean-worktree confirmation are performed after this
record-only commit; the final response supplies that commit's hash. No push.
