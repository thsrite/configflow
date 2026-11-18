#!/usr/bin/env bash
# Merge gate; run on a test server, never on the Mac. See gate.md.
set -euo pipefail
cd "$(dirname "$0")/.."
pflag=()
if [[ ${1:-} == -p ]]; then pflag=(-p="${2:?missing -p value}"); shift 2; fi
stage=${GATE_ONLY:-all}
case "$stage" in all|core|compare|proddiff|difftest) ;; *) echo "Unknown GATE_ONLY stage: $stage" >&2; exit 2 ;; esac
logs=${GATE_LOGS:-$(mktemp -d "${TMPDIR:-/tmp}/gate.XXXXXX")}
mkdir -p "$logs"
chmod 700 "$logs"
logs=$(realpath "$logs")
echo "gate: stage=$stage logs=$logs"; uptime
slots=${GATE_SLOTS:-1}
[[ $slots =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid GATE_SLOTS' >&2; exit 2; }
mkdir -p "$HOME/rt/.gate-slots"
waited=0
while :; do
  for ((i=1; i<=slots; i++)); do
    exec 8>"$HOME/rt/.gate-slots/$i"
    if flock -n 8; then got=$i; break 2; fi
    exec 8>&-
  done
  if ((waited == 0)); then echo "gate: $slots gate(s) running; waiting for a slot..."; fi
  waited=1; sleep 20
done
echo "gate: slot $got of $slots"
fail() { echo "GATE FAIL: $*" >&2; exit 1; }

# Decide once for the entire invocation; compare-only does not refresh the stamp.
full=0
stamp=$HOME/rt/.gate-last-full
now=$(date +%s)
last=$(stat -c %Y "$stamp" 2>/dev/null || echo 0)
if [[ ${GATE_FULL:-0} == 1 ]]; then
  full=1; reason=GATE_FULL
elif ((last == 0 || now-last >= 86400 || last > now)); then
  full=1; reason='no successful full core within 24h'
else
  reason='successful full core within 24h'
fi
if ((full)); then echo "gate: FULL mode ($reason)"; else echo "gate: CACHE mode ($reason)"; fi

run_core() {
  local out tag
  echo '== gofmt'
  out=$(gofmt -l internal/ tools/ cmd/) || fail gofmt
  [[ -z $out ]] || { echo "$out"; fail gofmt; }
  echo '== vet'
  go vet ./... || fail vet
  echo '== build tags'
  for tag in configflow_proddiff configflow_difftest; do
    go build -tags "$tag" ./... || fail "build -tags $tag"
    go vet -tags "$tag" ./internal/lifecycle/... ./cmd/... || fail "vet -tags $tag"
    go test -tags "$tag" -count=1 ./internal/lifecycle/... >"$logs/tag-$tag.log" 2>&1 \
      || { cat "$logs/tag-$tag.log"; fail "lifecycle tests -tags $tag"; }
  done
  if [[ -d tools/pyref ]]; then
    PYREF_INPUT_FINGERPRINT=$(go run ./tools/pyref -fingerprint) || fail 'pyref fingerprint'
    [[ -n $PYREF_INPUT_FINGERPRINT ]] || fail 'empty pyref fingerprint'
    export PYREF_INPUT_FINGERPRINT
    echo "gate: PYREF_INPUT_FINGERPRINT=$PYREF_INPUT_FINGERPRINT"
  else
    echo 'gate: pyref fingerprint entry not integrated; not setting PYREF_INPUT_FINGERPRINT'
  fi
  echo '== race'
  if ((full)); then
    echo 'gate: go clean -testcache (shared host cache; results will be repopulated)'
    go clean -testcache || fail 'clean test cache'
  fi
  go test -race "${pflag[@]}" -timeout=60m ./... >"$logs/race.log" 2>&1 \
    || { cat "$logs/race.log"; fail "race (see $logs/race.log)"; }
  cat "$logs/race.log"
  awk '/^ok[[:space:]]/ && /\(cached\)/ { print $2 }' "$logs/race.log" >"$logs/race-cached.txt"
  echo "gate: race cache hits=$(wc -l <"$logs/race-cached.txt")"
  cat "$logs/race-cached.txt"
  if (($#)); then
    echo "== fuzz ($# targets, ${GATE_FUZZTIME:-60s} each)"
    local target name pkg i bad=0
    local pids=() names=()
    for target in "$@"; do
      [[ $target == Fuzz*:* ]] || fail "invalid fuzz target: $target"
      name=${target%%:*}; pkg=${target#*:}
      go test -run '^$' -fuzz="^${name}\$" -fuzztime="${GATE_FUZZTIME:-60s}" \
        -parallel="${GATE_FUZZ_PARALLEL:-2}" "$pkg" >"$logs/fuzz-$name.log" 2>&1 &
      pids+=("$!"); names+=("$name")
    done
    for i in "${!pids[@]}"; do
      if wait "${pids[$i]}"; then tail -1 "$logs/fuzz-${names[$i]}.log"
      else cat "$logs/fuzz-${names[$i]}.log"; bad=1; fi
    done
    ((bad == 0)) || fail fuzz
  fi
  # A failed vet/build/race/fuzz never makes the next run cache-eligible.
  if ((full)); then touch "$stamp"; echo "gate: full core stamp updated: $stamp"; fi
}
run_proddiff() {
  echo '== proddiff observer self-checks'
  go test -tags configflow_proddiff -count=1 -run '^TestProddiff(Content|WaitsForFreshProvider)' \
    ./internal/api/agents ./cmd/configflow >"$logs/observer.log" 2>&1 \
    || { cat "$logs/observer.log"; fail 'proddiff observer self-checks'; }
  echo '== proddiff (live; no response cache)'
  (cd .. && server/scripts/proddiff.sh) >"$logs/proddiff.log" 2>&1 \
    || { tail -30 "$logs/proddiff.log"; fail proddiff; }
  tail -30 "$logs/proddiff.log"
}
run_difftest() {
  echo '== difftest (uncached)'
  local packages
  packages=$(go list ./... | grep '/difftest$') || fail 'no difftest packages'
  # Intentional splitting: go list produces whitespace-free import paths.
  CONFIGFLOW_DIFF=1 go test "${pflag[@]}" -count=1 -timeout=40m $packages >"$logs/diff.log" 2>&1 \
    || { cat "$logs/diff.log"; fail difftest; }
  cat "$logs/diff.log"
}
run_live() {
  echo '== synthetic live (uncached, required JSON execution manifest)'
  "${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}" scripts/gate-live.py "$logs" "${pflag[@]}" || fail 'synthetic live'
}
case "$stage" in
  all) run_core "$@"; run_proddiff; run_difftest; run_live ;;
  core) run_core "$@" ;;
  compare) run_proddiff; run_difftest; run_live ;;
  proddiff) run_proddiff ;;
  difftest) run_difftest ;;
esac
uptime
echo "GATE STAGE PASS: $stage"
