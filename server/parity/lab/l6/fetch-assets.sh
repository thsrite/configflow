#!/usr/bin/env bash
# L6: download the release files the lab cannot fetch itself (GitHub is slow or
# blocked from cfgt) into a GitHub-shaped mirror tree, with SHA256SUMS.
# Runs on a test server. The installers still verify their own pinned SHA256.
#
#   server/parity/lab/l6/fetch-assets.sh [extra GitHub release URL...]
#
# Collected automatically:
#   - every https://github.com/<o>/<r>/releases/download/<tag>/<file> pinned for
#     linux amd64 in this tree's installer scripts (Go panel),
#   - the same from the last Python panel image (L6_PY_PANEL, default
#     thsrite/config-flow:latest; skipped when the image is not present locally),
#   - mosdns (lab resolver) and the Sub-Store bundle version the images use.
# Output: ${L6_ASSETS:-~/.cache/cf-l6/assets}/gh/<o>/<r>/releases/download/<tag>/<file>
set -euo pipefail
root=$(cd "$(dirname "$0")/../../../.." && pwd)
out=${L6_ASSETS:-$HOME/.cache/cf-l6/assets}
py_panel=${L6_PY_PANEL:-thsrite/config-flow:latest}
mkdir -p "$out/gh"
urls=$(mktemp)
trap 'rm -f "$urls"' EXIT

pattern='https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/releases/download/v?[0-9][A-Za-z0-9_.-]*/[A-Za-z0-9_.-]+'
keep='(amd64|x86_64)'
grep -rhoE "$pattern" "$root/server/internal/installer" | grep -E "$keep" >> "$urls" || true
if sudo docker image inspect "$py_panel" >/dev/null 2>&1; then
  sudo docker run --rm --entrypoint sh "$py_panel" -c \
    "grep -rhoE '$pattern' /app 2>/dev/null || true" | grep -E "$keep" >> "$urls" || true
else
  echo "note: $py_panel not present locally; Python-pinned URLs not collected" >&2
fi
echo https://github.com/IrineSistiana/mosdns/releases/download/v5.3.3/mosdns-linux-amd64.zip >> "$urls"
printf '%s\n' "$@" >> "$urls"

sort -u "$urls" | while read -r url; do
  [ -n "$url" ] || continue
  rel=${url#https://github.com/}
  dest="$out/gh/$rel"
  if [ -s "$dest" ]; then echo "have $rel"; continue; fi
  mkdir -p "$(dirname "$dest")"
  echo "get  $rel"
  curl -fsSL --retry 3 -m 600 -o "$dest.tmp" "$url" && mv "$dest.tmp" "$dest" || { rm -f "$dest.tmp"; echo "FAILED $url" >&2; }
done
(cd "$out" && find gh -type f ! -name '*.tmp' | sort | xargs sha256sum > SHA256SUMS)
cat "$out/SHA256SUMS"
