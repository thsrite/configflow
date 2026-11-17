# N7-1 — yamlx native core

Status: implementation in progress; branch `go-n7-1` (from `go`). No sub-agents, pushes or caller migration.

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

## Next

1. Add FromJSON/ToJSON, JSON tests, synthetic PyYAML comparison and fuzz targets.
2. Iterate on .63; commit every compiling step with this handoff.
3. Final .32 gofmt/vet, yamlx race, full suite, fuzz targets 60s each, proddiff; rebase onto go, clean tree.

## Decisions/findings

- No old API or consumer changed; Python compatibility tests remain unchanged. This is additive, so no
  existing Python feature/test mapping is removed. New native policy differences are specified in design §5.7.
- Node encoding bypasses yaml.v3's string encoder's YAML 1.1 ambiguity quoting; Marshal asks its typed string
  encoder for the style on potentially ambiguous text. It also preserves leading newline/Unicode separators.
- No new dependencies. Full non-test before/after counts and final acceptance results pending.

## Commands

`RT_HOST=sunbroke@192.168.0.63 rt --back server/internal/yamlx -- bash -lc 'cd server && gofmt -w internal/yamlx/native*.go internal/yamlx/messages.go && go test -count=1 ./internal/yamlx/... && go vet ./...'`
