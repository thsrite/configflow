#!/usr/bin/env bash
# Same sequential, uncached before/after method as measure-a.sh.
# Run only on a test server after A releases the window and both loads are <10.
# Both source snapshots must already be synchronized; this script never syncs.
set -euo pipefail
before="$(realpath "${1:?baseline repository root}")"
after="$(cd "$(dirname "$0")/../../.." && pwd)"
logs="$(realpath -m "${2:?output directory}")"
mkdir -p "$logs"
packages=(./internal/agents/... ./internal/api/agents ./internal/installer ./internal/ippresence ./internal/repo)
for phase in before after; do
  tree="$before"; [ "$phase" = after ] && tree="$after"
  (
    cd "$tree/server"
    printf 'host='; hostname
    printf 'tree=%s\n' "$tree"
    printf 'GOMAXPROCS=%s\n' "${GOMAXPROCS:-unset}"
    go version
    cat /proc/loadavg
    date -Iseconds
    go test -race -count=1 -p 4 -timeout=60m -json "${packages[@]}" > "$logs/$phase.jsonl"
    date -Iseconds
    cat /proc/loadavg
  ) > "$logs/$phase.meta" 2>&1
  echo "race measurement $phase passed; $logs/$phase.jsonl"
done
