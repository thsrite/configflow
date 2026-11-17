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
(cd "$root/server" && go build -tags configflow_proddiff -o "$build/configflow" ./cmd/configflow && go build -o "$build/generator" ./tools/proddiff/generator)
# Mount namespace makes the source itself read-only, including to descendants.
unshare --user --map-root-user --net --mount --pid --fork --kill-child --mount-proc bash -c '
 set -euo pipefail
 ip link set lo up
 test "$(ip -o link show | wc -l)" -eq 1
 # Filesystem sockets ignore network namespaces: nss-resolve would reach systemd-resolved on
 # the host and resolve snapshot hostnames there. Hide /run and pin name lookup to loopback.
 mount -t tmpfs -o mode=755 proddiff-run /run
 printf "hosts: files dns\n" >/run/proddiff-nsswitch.conf
 printf "nameserver 127.0.0.1\noptions attempts:1 timeout:1\n" >/run/proddiff-resolv.conf
 mount --bind /run/proddiff-nsswitch.conf /etc/nsswitch.conf
 resolv=$(readlink -m /etc/resolv.conf)
 if [[ $resolv == /run/* ]]; then
  mkdir -p "$(dirname "$resolv")"
  cp /run/proddiff-resolv.conf "$resolv"
 else
  mount --bind /run/proddiff-resolv.conf "$resolv"
 fi
 mount --bind "$1" "$1"
 mount -o remount,bind,ro "$1"
 cd "$2"
 export PYTHONPATH="$2" PYTHONDONTWRITEBYTECODE=1
 export PRODDIFF_GENERATOR="$(dirname "$4")/generator"
 exec "$3" server/tools/proddiff/run.py "$1" "$4" "${@:5}"
' proddiff "$snapshot" "$root" "$python" "$build/configflow" "$@"
