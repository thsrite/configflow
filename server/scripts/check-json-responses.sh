#!/usr/bin/env bash
# N8-1 audit: handlers return httpx responses; JSON policy has one exit.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
cd "$root"
failed=0
reject() {
 local message=$1 pattern=$2
 shift 2
 local matches
 matches=$(find "$@" -type f -name '*.go' ! -name '*_test.go' -exec grep -nHE "$pattern" {} + || true)
 if [[ -n $matches ]]; then
  printf '%s\n%s\n' "$message" "$matches"
  failed=1
 fi
}
reject 'Handlers must return httpx responses, not write HTTP bodies directly:' \
 'http\.ResponseWriter|\.WriteHeader\(' internal/api internal/server
reject 'Legacy HTTP encoders must not bypass writeJSON:' \
 'jsonx\.(Encode|Compact|Default|Sorted|Response)\(' internal/httpx internal/jsonresp
# Encoding outside policy is allowed only for the three request/test helpers.
while IFS= read -r match; do
 case "$match" in
  internal/httpx/sanitize.go:*|internal/httpx/internal.go:*|internal/httpx/httpxtest/httpxtest.go:*|internal/localhttp/localhttp.go:*) ;;
  *) printf 'Unexpected native HTTP encoder: %s\n' "$match"; failed=1 ;;
 esac
done < <(find internal -type f -name '*.go' ! -name '*_test.go' -exec grep -nHE 'jsonresp\.Encode\(' {} + || true)
# Within the policy file, all encodings must belong to writeJSON itself.
awk '
 /^func / { in_exit = ($0 ~ /^func writeJSON\(/) }
 /jsonresp\.Encode\(/ && !in_exit { print "JSON encoding outside writeJSON: " FNR ":" $0; bad=1 }
 END { exit bad }
' internal/httpx/sanitize.go || failed=1
if ((failed)); then exit 1; fi
printf '%s\n' 'JSON response audit: PASS'
