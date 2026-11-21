#!/usr/bin/env bash
# O10 performance probe. Builds the normal Go server plus a pprof listener
# (added with -overlay, never in the source tree), then runs perf.py in a
# loopback-only user/net/mount/pid namespace on private snapshot copies.
# Usage: rt -- server/tools/o10perf/run.sh [--idle-minutes N] [--iterations N] [--servers go,python]
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../../.." && pwd)
python=${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}
snapshot=$(realpath "${O10_SNAPSHOT:-$HOME/cf-prod-snapshot/20261116/data}")
private=$HOME/cf-prod-snapshot/runs
mkdir -p "$private"
chmod 700 "$HOME/cf-prod-snapshot" "$private"
out=${O10_OUT:-$private/o10-$(date +%Y%m%d-%H%M%S)}
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
printf '{"Replace":{"%s":"%s"}}' "$root/server/cmd/configflow/o10_pprof_overlay.go" \
  "$root/server/tools/o10perf/pprof_overlay.go.txt" >"$build/overlay.json"
(cd "$root/server" && go build -overlay "$build/overlay.json" -o "$build/configflow" ./cmd/configflow)
echo "o10: private output $out"
unshare --user --map-root-user --net --mount --pid --fork --kill-child --mount-proc bash -c '
 set -euo pipefail
 ip link set lo up
 test "$(ip -o link show | wc -l)" -eq 1
 mount -t tmpfs -o mode=755 o10-run /run
 printf "hosts: files dns\n" >/run/o10-nsswitch.conf
 printf "nameserver 127.0.0.1\noptions attempts:1 timeout:1\n" >/run/o10-resolv.conf
 mount --bind /run/o10-nsswitch.conf /etc/nsswitch.conf
 resolv=$(readlink -m /etc/resolv.conf)
 if [[ $resolv == /run/* ]]; then mkdir -p "$(dirname "$resolv")"; cp /run/o10-resolv.conf "$resolv"
 else mount --bind /run/o10-resolv.conf "$resolv"; fi
 mount --bind "$1" "$1"
 mount -o remount,bind,ro "$1"
 cd "$2"
 export PYTHONPATH="$2" PYTHONDONTWRITEBYTECODE=1
 exec "$3" "$2/server/tools/o10perf/perf.py" "$1" --go "$4" --python "$3" --root "$2" --out "$5" "${@:6}"
' o10 "$snapshot" "$root" "$python" "$build/configflow" "$out" "$@"
