#!/usr/bin/env bash
# One independent public server and fake-device world. No host network access.
set -euo pipefail
if [[ $(uname -s) != Linux || $# -lt 3 ]]; then
  echo 'usage: namespace.sh owned-root python worker.py [args...] (native Linux)' >&2
  exit 2
fi
owned_root=$(realpath "$1")
shift
test -d "$owned_root/data"
# This empty mount point contains no data outside the private mount namespaces.
mkdir -p /tmp/routerdiff-active
exec sudo unshare --net --mount --pid --fork --kill-child --mount-proc bash -ceu '
  mount --make-rprivate /
  mount --bind "$2" /tmp/routerdiff-active
  ip link set lo up
  ip address add 10.44.0.1/24 dev lo
  ip address add 10.44.0.2/32 dev lo
  ip address add 10.44.0.3/32 dev lo
  ip address add 10.44.0.53/32 dev lo
  ip address add 10.44.0.54/32 dev lo
  ip address add 10.44.0.55/32 dev lo
  ip -6 address add fd42:cf::1/48 dev lo
  # A private resolver cannot contact host DNS even if a negative case uses a name.
  printf "nameserver 127.0.0.1\noptions attempts:1 timeout:1\n" > /tmp/routerdiff-active/resolv.conf
  mount --bind /tmp/routerdiff-active/resolv.conf /etc/resolv.conf
  exec runuser -u "$1" -- env ROUTERDIFF_ISOLATED=1 "${@:3}"
' routerdiff-world "$(id -un)" "$owned_root" "$@"
