# N7-1 — yamlx native core

Status: core implemented; final acceptance pending. Branch `go-n7-1` (from `go`). No sub-agents, pushes or caller migration.

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
- First compiling step: `beb5c56e`; .63 yamlx tests and full `go vet ./...` passed.
- FromJSON/ToJSON keep object order, duplicate-last semantics and exact number text; JSON strings use
  encoding/json without HTML escaping. Non-string keys and non-finite floats return errors.
- Live PyYAML reference: 257 synthetic YAML documents agree on semantic values or rejection; 15 string
  round trips stay strings in PyYAML. Explicit differences: YAML 1.1 bool/sexagesimal typing and D1.
- Two fuzz targets cover YAML round trips, arbitrary-input safety and exact JSON round trips. .63 short
  runs (15s each) passed after fixing the discovered empty-null-key encoder issue; its seed is committed.
- Typed timestamp tests cover aliases to different destination types, merge sequences and inline maps.

## Next

1. Final .32 gofmt/vet, yamlx race, full suite, fuzz targets 60s each, proddiff.
2. Record exact results/counts, rebase onto go, confirm clean tree.

## Decisions/findings

- No old API or consumer changed; Python compatibility tests remain unchanged. This is additive, so no
  existing Python feature/test mapping is removed. New native policy differences are specified in design §5.7.
- Node encoding bypasses yaml.v3's string encoder's YAML 1.1 ambiguity quoting; Marshal asks its typed string
  encoder for the style on potentially ambiguous text. It also preserves leading newline/Unicode separators.
- No new dependencies. Full non-test before/after counts and final acceptance results pending.
- yaml.v3 emits an empty null mapping key as a quoted empty string, changing its type. Marshal now spells
  that null as `null`; also moves document-wrapper comments onto the returned content node.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/yamlx -- bash -lc 'cd server && gofmt -w internal/yamlx/native*.go internal/yamlx/messages.go && go test -count=1 ./internal/yamlx/... && go vet ./...'`
