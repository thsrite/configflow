#!/usr/bin/env bash
# Builds may use the network; snapshot readers and both servers never do.
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../.." && pwd)
python=${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}
if [[ ${1:-} == --help ]]; then
 printf '%s\n' 'Usage: rt -- server/scripts/proddiff.sh [snapshot-data-directory] [--record-baseline]'
 exit 0
fi
snapshot=$HOME/cf-prod-snapshot/20261116/data
if (($#)) && [[ $1 != --* ]]; then snapshot=$1; shift; fi
for option in "$@"; do
 [[ $option == --record-baseline ]] || { printf '%s\n' 'Unknown option'; exit 2; }
done
snapshot=$(realpath "$snapshot")
private=$HOME/cf-prod-snapshot
mkdir -p "$private/runs"
chmod 700 "$private" "$private/runs"
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
"$python" "$root/server/tools/proddiff/build.py" "$root" "$build"
gofmt -w "$build/main.go" "$build/lifecycle.go"
(cd "$root/server" && go build -tags configflow_proddiff -overlay "$build/overlay.json" -o "$build/configflow" ./cmd/configflow)
# Mount namespace makes the source itself read-only, including to descendants.
unshare --user --map-root-user --net --mount --pid --fork --kill-child --mount-proc bash -c '
 set -euo pipefail
 ip link set lo up
 test "$(ip -o link show | wc -l)" -eq 1
 mount --bind "$1" "$1"
 mount -o remount,bind,ro "$1"
 cd "$2"
 export PYTHONPATH="$2" PYTHONDONTWRITEBYTECODE=1
 exec "$3" server/tools/proddiff/run.py "$1" "$4" "${@:5}"
' proddiff "$snapshot" "$root" "$python" "$build/configflow" "$@"
