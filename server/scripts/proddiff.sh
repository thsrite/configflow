#!/usr/bin/env bash
# Builds may use the network; snapshot readers and both servers never do.
#
# Modes (no-python-tests.md WP9):
#   PRODDIFF_MODE=python (default)  Flask + Python generators vs the Go candidate (this tree).
#   PRODDIFF_MODE=go                Go baseline vs the Go candidate. PRODDIFF_BASE_TREE names the
#                                   baseline source tree (a checkout containing server/); its
#                                   configflow binary and proddiff generator replace Flask and the
#                                   Python generators. The comparator then runs from a backend-free
#                                   tools venv (server/tools/requirements-tools.txt).
#   PRODDIFF_COMPARATOR=python (default) | go
#                                   go (requires PRODDIFF_MODE=go) runs the Go comparator
#                                   (server/tools/proddiff/comparator) instead of run.py: no Python
#                                   and no tools venv at all.
set -euo pipefail
umask 077
root=$(cd "$(dirname "$0")/../.." && pwd)
mode=${PRODDIFF_MODE:-python}
comparator=${PRODDIFF_COMPARATOR:-python}
if [[ ${1:-} == --help ]]; then
 printf '%s\n' 'Usage: [PRODDIFF_MODE=go PRODDIFF_BASE_TREE=<dir> [PRODDIFF_COMPARATOR=go]] rt -- server/scripts/proddiff.sh [snapshot-data-directory] [--record-baseline]'
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
case $comparator in
 python) ;;
 go) [[ $mode == go ]] || { printf '%s\n' 'PRODDIFF_COMPARATOR=go needs PRODDIFF_MODE=go'; exit 2; } ;;
 *) printf '%s\n' 'PRODDIFF_COMPARATOR must be python or go'; exit 2 ;;
esac
case $mode in
 python) python=${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python} ;;
 go)
  [[ -n ${PRODDIFF_BASE_TREE:-} && -d ${PRODDIFF_BASE_TREE}/server ]] \
   || { printf '%s\n' 'PRODDIFF_MODE=go needs PRODDIFF_BASE_TREE=<baseline source tree with server/>'; exit 2; }
  base_tree=$(realpath "$PRODDIFF_BASE_TREE")
  if [[ $comparator == go ]]; then python=; else
  # Comparator-only venv: stdlib + PyYAML, no Flask and no backend requirements.
  tools=${PRODDIFF_TOOLS_VENV:-$HOME/cf-tools/.venv}
  requirements=$root/server/tools/requirements-tools.txt
  stamp=$(sha256sum "$requirements" | cut -d' ' -f1)
  mkdir -p "$(dirname "$tools")"
  (
   flock 9
   if [[ ! -x $tools/bin/python || $(cat "$tools/.requirements-sha256" 2>/dev/null) != "$stamp" ]]; then
    rm -rf "$tools"
    uv venv -q --python 3.11 "$tools"
    uv pip install -q --python "$tools/bin/python" -r "$requirements"
    printf '%s\n' "$stamp" >"$tools/.requirements-sha256"
   fi
  ) 9>"$tools.lock"
  python=$tools/bin/python
  fi
  ;;
 *) printf '%s\n' 'PRODDIFF_MODE must be python or go'; exit 2 ;;
esac
runner=server/tools/proddiff/run.py
snapshot=$(realpath "$snapshot")
private=$HOME/cf-prod-snapshot
mkdir -p "$private/runs"
chmod 700 "$private" "$private/runs"
build=$(mktemp -d)
trap 'rm -rf "$build"' EXIT
(cd "$root/server" && go build -tags configflow_proddiff -o "$build/configflow" ./cmd/configflow && go build -o "$build/generator" ./tools/proddiff/generator)
if [[ $mode == python ]]; then
 (cd "$root/server" && go build -o "$build/baseline" ./tools/proddiff/baseline)
fi
if [[ $comparator == go ]]; then
 (cd "$root/server" && go build -o "$build/comparator" ./tools/proddiff/comparator)
 python=$build/comparator runner=run
fi
export PRODDIFF_MODE=$mode
if [[ $mode == go ]]; then
 mkdir "$build/base"
 (cd "$base_tree/server" && go build -tags configflow_proddiff -o "$build/base/configflow" ./cmd/configflow && go build -o "$build/base/generator" ./tools/proddiff/generator)
 export PRODDIFF_BASE_BINARY=$build/base/configflow PRODDIFF_BASE_GENERATOR=$build/base/generator
fi
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
 export PYTHONDONTWRITEBYTECODE=1
 # Go-vs-Go comparator must not be able to import backend/: only python mode gets the repo root.
 if [[ $PRODDIFF_MODE == python ]]; then export PYTHONPATH="$2"; else unset PYTHONPATH; fi
 export PRODDIFF_GENERATOR="$(dirname "$4")/generator"
 export PRODDIFF_BASELINE_TOOL="$(dirname "$4")/baseline"
 exec "$3" "$5" "$1" "$4" "${@:6}"
' proddiff "$snapshot" "$root" "$python" "$build/configflow" "$runner" "$@"
