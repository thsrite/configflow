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

## Remaining work

- Convert Mihomo writer and structural corpus comparison in the same commit.
- Confirm remaining writer call sites with the coordinator before removing the
  shared old writer; do not broaden product scope implicitly.
- Installer compose/guide: explicitly deferred until the coordinator announces
  that N7-3c is merged, then rebase first.
- Record the one-time generation byte change (D2/O14); no baseline updates.
- Final gofmt/vet, full race, all tests/difftests, relevant fuzz and proddiff.
