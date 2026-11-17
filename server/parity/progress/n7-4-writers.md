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

- Convert Mihomo writer and structural corpus comparison in the same commit.
- Confirm remaining writer call sites with the coordinator before removing the
  shared old writer; do not broaden product scope implicitly.
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
