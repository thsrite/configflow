#!/usr/bin/env bash
# Build with normal network access, then run only in a private loopback namespace.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../.." && pwd)
work=$(mktemp -d "$HOME/.cache/configflow-faults.XXXXXX")
mkdir -m 700 "$work/build"
trap 'rm -rf "$work/build"' EXIT
(cd "$root/server" && go build -tags configflow_difftest -o "$work/build/configflow-difftest" ./cmd/configflow && go build -o "$work/build/configflow" ./cmd/configflow && go build -o "$work/build/fixture" ./tools/faults/fixture)
python=${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}
unshare --user --map-root-user --net --mount --pid --fork --kill-child --mount-proc bash -c '
 set -euo pipefail
 ip link set lo up
 mount -t tmpfs -o mode=755 fault-run /run
 printf "hosts: files dns\n" >/run/fault-nsswitch.conf
 printf "nameserver 127.0.0.1\noptions attempts:1 timeout:1\n" >/run/fault-resolv.conf
 mount --bind /run/fault-nsswitch.conf /etc/nsswitch.conf
 resolv=$(readlink -m /etc/resolv.conf)
 if [[ $resolv == /run/* ]]; then
  mkdir -p "$(dirname "$resolv")"; cp /run/fault-resolv.conf "$resolv"
 else mount --bind /run/fault-resolv.conf "$resolv"; fi
 export PYTHONDONTWRITEBYTECODE=1
 exec setpriv --no-new-privs --bounding-set=-all "$1" "$2/server/tools/faults/run.py" --binary "$3/build/configflow-difftest" --production-binary "$3/build/configflow" --fixture "$3/build/fixture" --output "$3/results" "${@:4}"
' faults "$python" "$root" "$work" "$@"
