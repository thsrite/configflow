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
exec sudo unshare --net --fork bash -ceu '
  ip link set lo up
  exec runuser -u "$1" -- env CONFIGDIFF_ISOLATED_NETWORK=1 "${@:2}"
' configdiff-native-network "$(id -un)" "$@"
