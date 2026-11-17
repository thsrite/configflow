#!/usr/bin/env bash
# Complete A12 acceptance. Run only through rt on a test server.
set -euo pipefail
cd "$(dirname "$0")/../../../.."
format=$(gofmt -l internal/ tools/)
if [[ -n "$format" ]]; then
  printf '%s\n' "$format"
  exit 1
fi
go vet ./...
go test -race -p=4 -count=1 ./internal/deploy/pushdiff ./internal/agents/diag ./internal/yamlx ./internal/jsonresp
go test -p=4 -count=1 ./...
for d in $(go list ./... | grep difftest); do
  CONFIGFLOW_DIFF=1 go test -count=1 "$d"
done
for target in FuzzConfig FuzzRuleEntries FuzzNodeItems FuzzCredentialSpans; do
  go test -run '^$' -fuzz "^${target}$" -fuzztime=60s ./internal/deploy/pushdiff
done
cd ..
server/scripts/proddiff.sh
