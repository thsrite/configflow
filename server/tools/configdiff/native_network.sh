#!/usr/bin/env bash
# Run the complete public test boundary with loopback only, as the normal user.
set -euo pipefail
if [[ $(uname -s) != Linux || $(uname -r) == *[Mm]icrosoft* ]]; then
  echo 'configdiff native network fixtures require the native Linux .32 server' >&2
  exit 2
fi
if (($# == 0)); then
  echo 'usage: native_network.sh command [args...]' >&2
  exit 2
fi
exec sudo unshare --net --mount --fork bash -ceu '
  mount --make-rprivate /
  test -x /usr/sbin/nginx && test -d /etc/nginx || {
    echo "native listener fixtures require installed nginx and /etc/nginx" >&2
    exit 2
  }
  listener_root=$(mktemp -d /tmp/configdiff-listener.XXXXXX)
  chown "$1" "$listener_root"
  mount --bind "$listener_root" /etc/nginx
  cleanup() { umount /etc/nginx; rm -rf "$listener_root"; }
  trap cleanup EXIT
  ip link set lo up
  runuser -u "$1" -- env CONFIGDIFF_ISOLATED_NETWORK=1 CONFIGDIFF_ISOLATED_LISTENER=1 CONFIGDIFF_LISTENER_SOURCE="$listener_root" "${@:2}"
' configdiff-native-network "$(id -un)" "$@"
