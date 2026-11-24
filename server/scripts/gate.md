# Split merge gate (Go-only)

Run from a private worktree on a test server. `gate.sh` holds one machine-local
slot for its entire invocation (`GATE_SLOTS`, default 1); core and compare can
run on different hosts with independent slots. `GATE_LOGS` selects a persistent
log directory. Exit 0 means all selected work passed; it does not mean unselected
stages ran.

```sh
GATE_ONLY=core server/scripts/gate.sh -p 6 FuzzNativeRoundTrip:./internal/yamlx
GATE_ONLY=compare PRODDIFF_BASE_TREE=<baseline tree> server/scripts/gate.sh -p 6
GATE_ONLY=agent server/scripts/gate.sh -p 6     # Agent module only
GATE_ONLY=frontend server/scripts/gate.sh        # frontend only
# No GATE_ONLY: core followed by compare, under one slot (requires PRODDIFF_BASE_TREE).
```

## Stages

- **Core**: `gofmt`, whole-repository `go vet ./...`, both lifecycle build tags
  (`configflow_proddiff`, `configflow_difftest`), whole-repository `go test -race ./...`
  (including `internal/skipaudit` with an empty allowlist and committed pyref golden
  replays in `installer`/`mosdns`/`oxidns`/`provider`/`routersplit`), then the Agent
  module (`backend/agents/go-agent`: gofmt, vet, `halab nftlab` vet, cross-build, race),
  and any supplied fuzz targets (60s by default). `GATE_ONLY=agent` runs only the
  Agent module and, like compare, never refreshes the full-core stamp.
- **Compare**: observer self-checks + Go-vs-Go `proddiff` (`PRODDIFF_MODE=go`,
  `PRODDIFF_COMPARATOR=go`, requiring `PRODDIFF_BASE_TREE=<tree with server/>`),
  then the frontend (`npm run -s typecheck` + `npx vitest run`, with `node_modules`
  cached per `package-lock.json` hash under `~/cf-test/node_modules/<hash>`). Both
  groups run even if an earlier one fails; the final status remains failed and logs
  retain results for the other group.

## Fuzz temporary space and concurrency

Fuzz runs use a fresh `~/.cache/configflow-gate/fuzz.XXXXXX` directory on the
home disk, with separate `go/` and `tmp/` children exported as `GOTMPDIR` and
`TMPDIR`. The private directory is removed when the fuzz stage finishes, including
on failure. Persistent `GATE_LOGS/fuzz-<name>.log` files remain available for
diagnosis.

At most **8 fuzz targets** run simultaneously by default (`GATE_FUZZ_JOBS`),
separate from `GATE_FUZZ_PARALLEL` (default 2) and `-p`. Each target's budget is
`GATE_FUZZTIME` (default 60s).

## Race result cache

`GATE_FULL=1`, a missing `~/rt/.gate-last-full`, an age of at least 24h, or a
future-dated stamp selects FULL, which runs `go clean -testcache` before the
cache-enabled `-race` run. Only a fully successful core refreshes the stamp.

## Routing regression controls

```sh
cd server
bash -n scripts/gate.sh scripts/proddiff.sh
go test -v ./tools/gaterouting
```
