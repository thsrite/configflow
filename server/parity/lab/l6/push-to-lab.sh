#!/usr/bin/env bash
# L6: copy the built images and fetched assets from a test server to the PVE lab
# host (ssh alias `pve`), verifying SHA256 on arrival.
#
#   server/parity/lab/l6/push-to-lab.sh <commit>     (images of that build)
#
# Lands in /root/cf-gwip-lab/l6/xfer/{images/<commit>,assets}. Loading into the
# lab registry happens on PVE: pve/setup-infra.sh images <commit>.
set -euo pipefail
commit=${1:?usage: push-to-lab.sh <commit (12 hex)>}
images=${L6_OUT:-$HOME/.cache/cf-l6/images/$commit}
assets=${L6_ASSETS:-$HOME/.cache/cf-l6/assets}
dest=/root/cf-gwip-lab/l6/xfer
[ -f "$images/SHA256SUMS" ] || { echo "no build at $images" >&2; exit 1; }
ssh pve "mkdir -p $dest/images/$commit $dest/assets"
rsync -a --info=stats1 "$images/" "pve:$dest/images/$commit/"
[ -d "$assets/gh" ] && rsync -a --info=stats1 "$assets/" "pve:$dest/assets/"
ssh pve "cd $dest/images/$commit && sha256sum -c --quiet SHA256SUMS && echo images verified;
  [ -f $dest/assets/SHA256SUMS ] && cd $dest/assets && sha256sum -c --quiet SHA256SUMS && echo assets verified || true"
