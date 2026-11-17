#!/usr/bin/env bash
# gate.sh — the full merge gate for the Go server, run on a test server (via rt), never on the Mac.
#
#   server/scripts/gate.sh [-p N] [FuzzName:./pkg ...]
#
# Steps: gofmt + vet, both build tags (build ./..., vet + lifecycle tests), then in parallel (a) full `go test -race` and (b) proddiff, then all
# difftest packages in one `go test` (packages run in parallel), then the given fuzz targets
# concurrently for 60 s each. The race run already executes every test, so there is no separate
# non-race run. Exit code 0 only when every step passed; logs stay in $GATE_LOGS (printed).
#
#   -p N     package parallelism for race/difftest (default: go's default = GOMAXPROCS).
#            Use -p 4 on .64 (15 GB RAM; full -race at default -p was OOM-killed once).
# Env: GATE_FUZZTIME (default 60s), GATE_FUZZ_PARALLEL (default 2 workers per target).
set -u
cd "$(dirname "$0")/.."            # server/

pflag=()
if [ "${1:-}" = "-p" ]; then pflag=(-p="$2"); shift 2; fi
fuzztime="${GATE_FUZZTIME:-60s}"
fuzzpar="${GATE_FUZZ_PARALLEL:-2}"
logs="${GATE_LOGS:-$(mktemp -d /tmp/gate.XXXXXX)}"
mkdir -p "$logs"
echo "gate: logs in $logs"; uptime

# At most GATE_SLOTS full gates per test server at once (default 2): a dozen agents running full
# -race + proddiff together pushed the load past 100 on 12 cores and produced timing-only diffs.
slots="${GATE_SLOTS:-2}"
mkdir -p "$HOME/rt/.gate-slots"
waited=0
while :; do
  for i in $(seq 1 "$slots"); do
    exec 8>"$HOME/rt/.gate-slots/$i"
    if flock -n 8; then got=$i; break 2; fi
    exec 8>&-
  done
  [ "$waited" -eq 0 ] && echo "gate: $slots gate(s) already running on this server; waiting for a slot..."
  waited=1; sleep 20
done
echo "gate: slot $got of $slots"

fail() { echo "GATE FAIL: $*"; exit 1; }
quiet() { grep -v -E '^ok |no test files' "$1"; }

echo "== gofmt"
out="$(gofmt -l internal/ tools/ cmd/)"; [ -z "$out" ] || { echo "$out"; fail gofmt; }
echo "== vet"
go vet ./... || fail vet

echo "== build tags (configflow_proddiff, configflow_difftest)"
for tag in configflow_proddiff configflow_difftest; do
  go build -tags "$tag" ./... || fail "build -tags $tag"
  go vet -tags "$tag" ./internal/lifecycle/... ./cmd/... || fail "vet -tags $tag"
  go test -tags "$tag" -count=1 ./internal/lifecycle/... >"$logs/tag-$tag.log" 2>&1 \
    || { cat "$logs/tag-$tag.log"; fail "lifecycle tests -tags $tag"; }
done

echo "== race + proddiff (parallel)"
( cd .. && server/scripts/proddiff.sh >"$logs/proddiff.log" 2>&1; echo $? >"$logs/proddiff.rc" ) &
pd=$!
go test -race "${pflag[@]}" -count=1 -timeout=60m ./... >"$logs/race.log" 2>&1
race=$?
quiet "$logs/race.log"
wait "$pd"
tail -1 "$logs/proddiff.log"
[ "$race" -eq 0 ] || fail "race (see $logs/race.log; 'signal: killed' = OOM, rerun that package)"
[ "$(cat "$logs/proddiff.rc")" = 0 ] || { tail -30 "$logs/proddiff.log"; fail proddiff; }

echo "== difftest"
mapfile_pkgs() { go list ./... | grep difftest; }
CONFIGFLOW_DIFF=1 go test "${pflag[@]}" -count=1 -timeout=40m $(mapfile_pkgs) >"$logs/diff.log" 2>&1
diff=$?
cat "$logs/diff.log" | grep -E '^(ok|FAIL|---)'
[ "$diff" -eq 0 ] || fail difftest

if [ $# -gt 0 ]; then
  echo "== fuzz ($# targets, $fuzztime each, concurrent)"
  pids=(); names=()
  for t in "$@"; do
    name="${t%%:*}"; pkg="${t#*:}"
    go test -run '^$' -fuzz="^${name}\$" -fuzztime="$fuzztime" -parallel="$fuzzpar" "$pkg" \
      >"$logs/fuzz-$name.log" 2>&1 &
    pids+=($!); names+=("$name")
  done
  bad=0
  for i in "${!pids[@]}"; do
    if wait "${pids[$i]}"; then echo "${names[$i]} $(tail -1 "$logs/fuzz-${names[$i]}.log")"
    else echo "FUZZ FAIL ${names[$i]}"; tail -20 "$logs/fuzz-${names[$i]}.log"; bad=1; fi
  done
  [ "$bad" -eq 0 ] || fail fuzz
fi

uptime
echo "GATE PASS"
