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
if [[ -n ${O10_GO_BIN:-} ]]; then
  # Prebuilt overlay binary (pve.sh builds it on the test server; the CT has no Go toolchain).
  cp "$O10_GO_BIN" "$build/configflow"
else
  # O10_TAGS=memory.counters fills libc/mmap_bytes in /debug/o10/metrics.
  # O10_LEAN=1: metrics endpoints only, no net/http/pprof (product-identical heap profiling = off).
  overlay=pprof_overlay.go.txt
  [[ ${O10_LEAN:-} == 1 ]] && overlay=lean_overlay.go.txt
  printf '{"Replace":{"%s":"%s"}}' "$root/server/cmd/configflow/o10_pprof_overlay.go" \
    "$root/server/tools/o10perf/$overlay" >"$build/overlay.json"
  # Same flags as docker/Dockerfile (static, no cgo, stripped) so RSS matches the product binary.
  (cd "$root/server" && CGO_ENABLED=0 go build -trimpath -tags="${O10_TAGS:-}" -ldflags='-s -w' -overlay "$build/overlay.json" -o "$build/configflow" ./cmd/configflow)
fi
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
