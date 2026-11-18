#!/usr/bin/env bash
# Run the exact same uncached race command, sequentially, on two source trees.
# Both trees and the logs must be on a test server. No git/network operations.
set -euo pipefail
before="$(realpath "${1:?baseline repository root}")"
after="$(cd "$(dirname "$0")/../../.." && pwd)"
logs="$(realpath -m "${2:?output directory}")"
mkdir -p "$logs"
packages=(./internal/mosdns ./internal/oxidns ./internal/provider ./internal/deploy/... ./internal/discovery ./internal/routersplit)
for phase in before after; do
  tree="$before"; [ "$phase" = after ] && tree="$after"
  (
    cd "$tree/server"
    printf 'host='; hostname
    printf 'tree=%s\n' "$tree"
    cat /proc/loadavg
    date -Iseconds
    go test -race -count=1 -p 4 -timeout=30m -json "${packages[@]}" > "$logs/$phase.jsonl"
    date -Iseconds
    cat /proc/loadavg
  ) > "$logs/$phase.meta" 2>&1
  echo "race measurement $phase passed; $logs/$phase.jsonl"
done
