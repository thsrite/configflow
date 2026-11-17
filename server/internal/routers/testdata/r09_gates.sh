#!/usr/bin/env bash
# Run only on an isolated test server, via rt from the R09 worktree.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../../.." && pwd)
cd "$root/server"
export LANG=en_US.UTF-8
logs=$(mktemp -d /tmp/configflow-r09-gates.XXXXXX)
printf 'R09_GATE_LOGS %s\n' "$logs"
run_gate() {
 local name=$1
 shift
 printf 'RUN %s\n' "$name"
 if "$@" >"$logs/$name.log" 2>&1; then
  tail -5 "$logs/$name.log"
  printf 'PASS %s\n' "$name"
 else
  tail -80 "$logs/$name.log"
  printf 'FAIL %s\n' "$name"
  return 1
 fi
}
run_gate gofmt bash -c 'result=$(gofmt -l internal/ tools/); test -z "$result" || { printf "%s\n" "$result"; exit 1; }'
run_gate vet go vet ./...
run_gate race go test -race -count=1 ./internal/routers/... ./internal/routersplit/... ./internal/devhttp/...
run_gate full go test -p=4 -count=1 ./...
mapfile -t diffs < <(go list ./... | rg /difftest)
run_gate difftest env CONFIGFLOW_DIFF=1 go test -tags=configflow_difftest -p=4 -count=1 "${diffs[@]}"
run_gate fuzz_manifest go test -run '^$' -fuzz '^FuzzR09DNSGuardManifest$' -fuzztime 60s -parallel 4 ./internal/routers
run_gate fuzz_policy go test -run '^$' -fuzz '^FuzzR09PolicyEvaluator$' -fuzztime 60s -parallel 4 ./internal/routers
cd "$root"
run_gate mutations env PYTHONPATH=. "$HOME/cf-test/.venv/bin/python" server/internal/routers/testdata/r09_mutations.py
run_gate proddiff server/scripts/proddiff.sh
printf 'R09_ALL_GATES_PASS\n'
