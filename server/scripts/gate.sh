#!/usr/bin/env bash
# Merge gate; run on a test server, never on the Mac. See gate.md.
set -euo pipefail
cd "$(dirname "$0")/.."
pflag=()
if [[ ${1:-} == -p ]]; then pflag=(-p="${2:?missing -p value}"); shift 2; fi
stage=${GATE_ONLY:-all}
case "$stage" in all|core|agent|compare|proddiff|difftest|frontend) ;; *) echo "Unknown GATE_ONLY stage: $stage" >&2; exit 2 ;; esac
# proddiff reference: python (default until WP11) or go (Go baseline tree vs this tree).
proddiff_mode=${PRODDIFF_MODE:-python}
case "$proddiff_mode" in
  python) ;;
  go) [[ -d ${PRODDIFF_BASE_TREE:-}/server ]] || { echo 'PRODDIFF_MODE=go needs PRODDIFF_BASE_TREE=<baseline tree>' >&2; exit 2; } ;;
  *) echo "Unknown PRODDIFF_MODE: $proddiff_mode" >&2; exit 2 ;;
esac
# proddiff comparator: python run.py (default until the switch) or the Go comparator (Go mode only).
proddiff_comparator=${PRODDIFF_COMPARATOR:-python}
case "$proddiff_comparator" in
  python) ;;
  go) [[ $proddiff_mode == go ]] || { echo 'PRODDIFF_COMPARATOR=go needs PRODDIFF_MODE=go' >&2; exit 2; } ;;
  *) echo "Unknown PRODDIFF_COMPARATOR: $proddiff_comparator" >&2; exit 2 ;;
esac
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

# Keep fuzz work off /tmp even when the caller did not source ~/.rt-env.
# A subshell scopes both temporary environment variables and cleanup traps.
run_fuzz() (
  local jobs=${GATE_FUZZ_JOBS:-8} target name pkg fuzz_tmp bad=0
  local pids=() names=()
  [[ $jobs =~ ^[1-9][0-9]*$ ]] || { echo 'Invalid GATE_FUZZ_JOBS' >&2; exit 2; }
  for target in "$@"; do
    [[ $target == Fuzz*:* ]] || fail "invalid fuzz target: $target"
  done
  mkdir -p "$HOME/.cache/configflow-gate" || exit 1
  fuzz_tmp=$(mktemp -d "$HOME/.cache/configflow-gate/fuzz.XXXXXX") || exit 1
  cleanup_fuzz() {
    # Reap tracked fuzz commands before removing their temporary directories.
    # jobs reports only live children, avoiding stale numeric PID reuse.
    local active
    active=$(jobs -pr)
    if [[ -n $active ]]; then
      # Intentional splitting: jobs -pr produces whitespace-separated PIDs.
      kill $active 2>/dev/null || true
    fi
    wait || true
    rm -rf -- "$fuzz_tmp"
  }
  trap cleanup_fuzz EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  export GOTMPDIR="$fuzz_tmp/go" TMPDIR="$fuzz_tmp/tmp"
  mkdir -p "$GOTMPDIR" "$TMPDIR" || exit 1
  echo "== fuzz ($# targets, ${GATE_FUZZTIME:-60s} each, at most $jobs jobs; tmp=$fuzz_tmp)"
  wait_first_fuzz() {
    if wait "${pids[0]}"; then tail -1 "$logs/fuzz-${names[0]}.log"
    else cat "$logs/fuzz-${names[0]}.log"; bad=1; fi
    pids=("${pids[@]:1}")
    names=("${names[@]:1}")
  }
  for target in "$@"; do
    name=${target%%:*}; pkg=${target#*:}
    go test -run '^$' -fuzz="^${name}\$" -fuzztime="${GATE_FUZZTIME:-60s}" \
      -parallel="${GATE_FUZZ_PARALLEL:-2}" "$pkg" >"$logs/fuzz-$name.log" 2>&1 &
    pids+=("$!"); names+=("$name")
    if ((${#pids[@]} >= jobs)); then wait_first_fuzz; fi
  done
  while ((${#pids[@]})); do wait_first_fuzz; done
  ((bad == 0))
)

# The Agent is its own Go module (backend/agents/go-agent); "go test ./..." in
# server/ never reaches it. halab/nftlab tests need a disposable container
# (backend/agents/go-agent/integration/*_lab.sh), so they are vetted only.
agent_dir=../backend/agents/go-agent
run_agent() (
  local out
  echo '== agent module (backend/agents/go-agent)'
  cd "$agent_dir" || fail agent-module
  out=$(gofmt -l .) || fail agent-module
  [[ -z $out ]] || { echo "$out"; fail agent-module; }
  go vet ./... || fail agent-module
  go vet -tags 'halab nftlab' ./... || fail agent-module
  go test -race "${pflag[@]}" -timeout=60m ./... >"$logs/agent-race.log" 2>&1 \
    || { cat "$logs/agent-race.log"; fail "agent-module (see $logs/agent-race.log)"; }
  cat "$logs/agent-race.log"
  echo "gate: agent race cache hits=$(awk '/^ok[[:space:]]/ && /\(cached\)/' "$logs/agent-race.log" | wc -l)"
  echo 'gate: agent module PASS (gofmt, vet, vet -tags halab nftlab, race)'
)

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
  # After the server race run, never alongside it: same -p, same test cache
  # (FULL mode cleaned the shared cache above).
  run_agent || exit 1
  if (($#)); then run_fuzz "$@" || fail fuzz; fi
  # A failed vet/build/race/fuzz never makes the next run cache-eligible.
  if ((full)); then touch "$stamp"; echo "gate: full core stamp updated: $stamp"; fi
}
run_proddiff() {
  echo '== proddiff observer self-checks'
  go test -tags configflow_proddiff -count=1 -run '^TestProddiff(Content|WaitsForFreshProvider)' \
    ./internal/api/agents ./cmd/configflow >"$logs/observer.log" 2>&1 \
    || { cat "$logs/observer.log"; fail 'proddiff observer self-checks'; }
  echo "== proddiff (live; no response cache) mode=$proddiff_mode comparator=$proddiff_comparator"
  (cd .. && PRODDIFF_MODE=$proddiff_mode PRODDIFF_COMPARATOR=$proddiff_comparator server/scripts/proddiff.sh) >"$logs/proddiff.log" 2>&1 \
    || { tail -30 "$logs/proddiff.log"; fail proddiff; }
  tail -30 "$logs/proddiff.log"
}
run_difftest() {
  echo '== difftest (uncached)'
  "${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}" scripts/gate-live.py --difftest "$logs" "${pflag[@]}" || fail difftest
}
run_live() {
  echo '== synthetic live (uncached, required JSON execution manifest)'
  "${CONFIGFLOW_DIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}" scripts/gate-live.py "$logs" "${pflag[@]}" || fail 'synthetic live'
}
# Frontend (../frontend): the same commands as cf-test typecheck / cf-test
# frontend. node_modules is installed once per package-lock.json hash into the
# cf-test cache (~/cf-test/node_modules/<hash>) and linked, never per run.
frontend_deps() {
  local base=$HOME/cf-test hash cache
  hash=$(sha256sum ../frontend/package-lock.json | cut -c1-16) || return 1
  cache=$base/node_modules/$hash
  if [[ ! -d $cache/node_modules ]]; then
    mkdir -p "$base/node_modules" || return 1
    (
      exec 9>"$base/node_modules/.lock-$hash"
      flock 9
      if [[ ! -d $cache/node_modules ]]; then
        echo "gate: installing frontend dependencies ($hash)"
        rm -rf "$cache.tmp" && mkdir -p "$cache.tmp" || exit 1
        cp ../frontend/package.json ../frontend/package-lock.json "$cache.tmp/" || exit 1
        (cd "$cache.tmp" && npm ci --no-audit --no-fund --loglevel=error) || { rm -rf "$cache.tmp"; exit 1; }
        mv "$cache.tmp" "$cache"
      fi
    ) || return 1
  fi
  rm -rf ../frontend/node_modules && ln -sfn "$cache/node_modules" ../frontend/node_modules
}
run_frontend() {
  echo '== frontend (npm ci cached by lockfile, vue-tsc typecheck, vitest)'
  frontend_deps >"$logs/frontend-deps.log" 2>&1 \
    || { cat "$logs/frontend-deps.log"; fail frontend; }
  (cd ../frontend && npm run -s typecheck) >"$logs/frontend-typecheck.log" 2>&1 \
    || { tail -60 "$logs/frontend-typecheck.log"; fail "frontend (typecheck, see $logs/frontend-typecheck.log)"; }
  (cd ../frontend && npx vitest run) >"$logs/frontend-vitest.log" 2>&1 \
    || { grep -E '(FAIL|×) ' "$logs/frontend-vitest.log" | head -40; tail -20 "$logs/frontend-vitest.log"; fail "frontend (vitest, see $logs/frontend-vitest.log)"; }
  echo "gate: frontend PASS (typecheck; vitest $(grep -E '^ +Tests ' "$logs/frontend-vitest.log" | tr -s ' ' | sed 's/^ //'))"
}
# Run every comparison even when an earlier group fails, retaining all evidence.
# The frontend runs here, not in core: it uses no Go caches or race workers.
run_compare() {
  local bad=0
  (run_proddiff) || bad=1
  (run_difftest) || bad=1
  (run_live) || bad=1
  (run_frontend) || bad=1
  ((bad == 0)) || fail compare
}
case "$stage" in
  all) run_core "$@"; run_compare ;;
  core) run_core "$@" ;;
  agent)
    # Agent module only; like compare-only it never refreshes the full stamp.
    if ((full)); then go clean -testcache || fail 'clean test cache'; fi
    run_agent || exit 1 ;;
  compare) run_compare ;;
  proddiff) run_proddiff ;;
  difftest) run_difftest ;;
  frontend) run_frontend ;;
esac
uptime
echo "GATE STAGE PASS: $stage"
