#!/usr/bin/env bash
# Builds may use the network; snapshot readers and both servers never do.
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../.." && pwd)
python=${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}
snapshot=${1:-$HOME/cf-prod-snapshot/20261116/data}
if (($#)); then shift; fi
snapshot=$(realpath "$snapshot")
private=$HOME/cf-prod-snapshot
mkdir -p "$private/runs"
chmod 700 "$private" "$private/runs"
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
"$python" "$root/server/tools/proddiff/build.py" "$root" "$build"
(cd "$root/server" && go build -overlay "$build/overlay.json" -o "$build/configflow" ./cmd/configflow)
# Mount namespace makes the source itself read-only, including to descendants.
unshare --user --map-root-user --net --mount bash -c '
 set -euo pipefail
 ip link set lo up
 test "$(ip -o link show | wc -l)" -eq 1
 mount --bind "$1" "$1"
 mount -o remount,bind,ro "$1"
 cd "$2"
 export PYTHONPATH="$2" PYTHONDONTWRITEBYTECODE=1
 exec "$3" server/tools/proddiff/run.py "$1" "$4" "${@:5}"
' proddiff "$snapshot" "$root" "$python" "$build/configflow" "$@"
