# N7-1 — yamlx native core

Status: ported; final .32 acceptance passed, ready for independent review. Branch `go-n7-1`, rebased onto
`go` at `54496551` (documentation-only changes after final acceptance on `68c86a01`). No sub-agents,
pushes or caller migration.

## Brief

Implement design `server/design/n7-n8.md` §2.2 alongside the existing yamlx/pyyaml APIs. Preserve Node
annotations and anchors, reject duplicate keys everywhere in the new API (D1), expose ordered JSON↔Node
and native builders/navigation/clone/equality. Editor and reader/writer migration are subsequent steps.

## Done

- Parse/Unmarshal/Decode/Encode/Marshal; syntax sentinels and positioned diagnostics, Chinese policy
  templates in messages.go. Document wrapper comments retained when returning the content node.
- Builders, merge-aware accessors/iteration/deletion/equality and independent alias-expanding Clone.
- Coordinator clarified recursive references, alias budget, merge semantics and timestamp decoding in §2.2.
- Regression tests: realistic commented Mihomo CRLF/Chinese/anchors, ambiguity quoting, native typing,
  duplicate key messages/positions, clone isolation/manual cycles, alias bomb, 60k ordinary rules.
- Compiling steps (final rebased ids): `5e0d2f97` native core, `885bddde` ordered JSON and
  differential/fuzz coverage, `bc7948a6` scalar-text/type-error clarifications. Each passed .63 yamlx and
  full `go vet ./...` before committing.
- FromJSON/ToJSON keep object order, duplicate-last semantics and exact number text; JSON strings use
  encoding/json without HTML escaping. Non-string keys return errors; YAML non-finite floats become their
  original scalar text strings (coordinator's final clarification, documented in design §2.2).
- Live PyYAML reference: 257 synthetic YAML documents agree on semantic values or rejection; 15 string
  round trips stay strings in PyYAML. Explicit differences: YAML 1.1 bool/sexagesimal typing and D1.
- Two fuzz targets cover YAML round trips, arbitrary-input safety and exact JSON round trips. .63 short
  runs (15s each) passed after fixing the discovered empty-null-key encoder issue; its seed is committed.
- Typed timestamp tests cover aliases to different destination types, merge sequences and inline maps.
- Typed Decode mismatches use ErrType, retaining errors.As to *yaml.TypeError and a Chinese template;
  syntax/policy failures remain ErrSyntax.

## Next

No N7-1 implementation work remains. Coordinator reviews/integrates the branch; N7-2 adds Edit,
N7-3 migrates readers and classifies old versus new corpora, N7-4 migrates writers. Legacy APIs and
oxidns helpers are intentionally retained until their migration steps.

## Decisions/findings

- No old API or consumer changed; Python compatibility tests remain unchanged. This is additive, so no
  existing Python feature/test mapping is removed. New native policy differences are specified in design §5.7.
- Node encoding bypasses yaml.v3's string encoder's YAML 1.1 ambiguity quoting; Marshal asks its typed string
  encoder for the style on potentially ambiguous text. It also preserves leading newline/Unicode separators.
- No new dependencies. yamlx non-test lines: **3109 → 4225**; additive by design, legacy removal deferred.
- yaml.v3 emits an empty null mapping key as a quoted empty string, changing its type. Marshal now spells
  that null as `null`; also moves document-wrapper comments onto the returned content node.
- Coordinator explicitly defers design §5.4's old-versus-new corpus classification and invalid explicit
  tag/shape policy (e.g. `!!null [a]`) to N7-3; the design records "Open for N7-3". Both old corpora still
  run unchanged in the full yamlx suite. New native errors/messages intentionally differ from PyYAML;
  YAML 1.1 bool/sexagesimal resolution follows yaml.v3, duplicate keys now reject only on the new API.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/yamlx -- bash -lc 'cd server && gofmt -w internal/yamlx/native*.go internal/yamlx/messages.go && go test -count=1 ./internal/yamlx/... && go vet ./...'`

Final acceptance on .32, after the code commits were rebased:

```sh
RT_HOST=sunbroke@192.168.0.32 rt --back server/internal/yamlx/testdata/fuzz -- bash -lc 'cd server && test -z "$(gofmt -l .)" && go vet ./... && go test -race -count=1 ./internal/yamlx/... && go test -count=1 ./... && go test -run="^$" -fuzz="^FuzzNativeRoundTrip$" -fuzztime=60s -parallel=4 ./internal/yamlx && go test -run="^$" -fuzz="^FuzzNativeNoPanic$" -fuzztime=60s -parallel=4 ./internal/yamlx && cd .. && server/scripts/proddiff.sh'
```

Exit **0**, 2026-11-17. gofmt/vet silent; race `ok configflow/internal/yamlx 9.054s`. Full-suite tail:
`ok configflow/internal/yamlx 1.309s`, `? configflow/tools/proddiff/generator [no test files]`;
installer 354.216s, oxidns 173.415s, no failures. Both old YAML corpora pass unchanged with all native tests.
Round-trip fuzz: 60s budget, 260513 executions, `PASS`, `ok configflow/internal/yamlx 61.083s`.
Arbitrary-input/JSON fuzz: 60s budget, 99450 executions, `PASS`, `ok configflow/internal/yamlx 61.070s`.
Updated proddiff: all 23 self-checks PASS, 2831 cases, 415 comparisons, 10 existing unique baseline
differences, coverage regressions 0, unmapped routes 0; final line **`baseline: new=0 stale=0`**.

`git diff --check` and new-code emulation grep (jsonx/pyyaml/PythonClass/dictGet/raise/catch) are clean.
Final `git rebase go` incorporated documentation-only O3 changes; no code or test changed after the
successful .32 run. Final worktree is clean; no push performed.
The only findings fixed in this step were loss of document-wrapper comments and empty null keys changing
into strings on native encoding; the former has a realistic regression, the latter a saved fuzz seed.
No open N7-1 functional finding and no runtime caller behaviour changed. No new Python feature is
unported: old implementation/tests remain until the later swaps; native differences are specified above.
