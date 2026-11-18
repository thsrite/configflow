#!/usr/bin/env bash
# Trace only the WebDAV binary: its legacy Python branch emits no Go subtest.
set -euo pipefail
if [[ $(basename "$1") == webdav.test ]]; then
  (umask 077; : > "${GATE_LIVE_TRACE:?}")
  exec strace -f -qq -e trace=execve -s 16384 -o "${GATE_LIVE_TRACE:?}" "$@"
fi
exec "$@"
