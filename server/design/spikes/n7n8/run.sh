#!/usr/bin/env bash
# Runs the N7/N8 spike on the test server against a private copy of the
# production snapshot. Prints counts only; the copy is removed afterwards.
# Usage (from the repo root, via rt): rt -- server/design/spikes/n7n8/run.sh
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../../../.." && pwd)
snapshot=${1:-$HOME/cf-prod-snapshot/20261116/data}
work=$(mktemp -d "$HOME/cf-prod-snapshot/runs/n7n8.XXXXXX")
trap 'rm -rf "$work"' EXIT
cp -r "$snapshot" "$work/data"
"$HOME/cf-test/.venv/bin/python" -I "$root/server/design/spikes/n7n8/collect.py" "$work/data" "$work/out"
cd "$root/server"
go run -tags spike ./design/spikes/n7n8 "$work/out" "$work/data/configflow.sqlite3"
