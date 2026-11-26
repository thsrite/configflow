#!/usr/bin/env bash
# Builds may use the network; snapshot readers and both servers never do.
#
# Go-vs-Go production-snapshot differential (no Python):
#   PRODDIFF_MODE=go (default)        Go baseline vs the Go candidate. PRODDIFF_BASE_TREE names the
#                                     baseline source tree (a checkout containing server/); its
#                                     configflow binary and proddiff generator form the left side.
#   PRODDIFF_COMPARATOR=go (default)  Runs the Go comparator (server/tools/proddiff/comparator).
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../.." && pwd)
mode=${PRODDIFF_MODE:-go}
comparator=${PRODDIFF_COMPARATOR:-go}
if [[ ${1:-} == --help ]]; then
 printf '%s\n' 'Usage: rt -e PRODDIFF_BASE_TREE=<dir> -- server/scripts/proddiff.sh [snapshot-data-directory] [--record-baseline]'
 exit 0
fi
snapshot=$HOME/cf-prod-snapshot/20261116/data
if (($#)) && [[ $1 != --* ]]; then snapshot=$1; shift; fi
for option in "$@"; do
 case $option in
  --record-baseline) ;;
  *) printf '%s\n' 'Unknown option'; exit 2 ;;
 esac
done
[[ $mode == go ]] || { printf '%s\n' 'PRODDIFF_MODE must be go'; exit 2; }
[[ $comparator == go ]] || { printf '%s\n' 'PRODDIFF_COMPARATOR must be go'; exit 2; }
[[ -n ${PRODDIFF_BASE_TREE:-} && -d ${PRODDIFF_BASE_TREE}/server ]] \
 || { printf '%s\n' 'PRODDIFF_MODE=go needs PRODDIFF_BASE_TREE=<baseline source tree with server/>'; exit 2; }
base_tree=$(realpath "$PRODDIFF_BASE_TREE")
snapshot=$(realpath "$snapshot")
private=$HOME/cf-prod-snapshot
mkdir -p "$private/runs"
chmod 700 "$private" "$private/runs"
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
(cd "$root/server" && go build -tags configflow_proddiff -o "$build/configflow" ./cmd/configflow && go build -o "$build/generator" ./tools/proddiff/generator && go build -o "$build/comparator" ./tools/proddiff/comparator)
mkdir "$build/base"
(cd "$base_tree/server" && go build -tags configflow_proddiff -o "$build/base/configflow" ./cmd/configflow && go build -o "$build/base/generator" ./tools/proddiff/generator)
export PRODDIFF_MODE=go
export PRODDIFF_BASE_BINARY=$build/base/configflow PRODDIFF_BASE_GENERATOR=$build/base/generator
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
 export PRODDIFF_GENERATOR="$(dirname "$4")/generator"
 exec "$3" run "$1" "$4" "${@:5}"
' proddiff "$snapshot" "$root" "$build/comparator" "$build/configflow" "$@"
