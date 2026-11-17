#!/usr/bin/env bash
# Run the official Go gate, then config's full black-box suite in the SAME gate slot.
# Usage: RT_HOST=... rt -- ... server/tools/configdiff/gate.sh <Go-baseline> [-p N]
# Source gate.sh so its descriptor 8 stays locked until configdiff has finished.
# Even a known proddiff failure must not prevent obtaining config's final evidence.
set -u
configdiff_root=$(cd "$(dirname "$0")/../../.." && pwd)
configdiff_baseline=${1:?Pass the exact Go baseline}
shift
configdiff_logs=${CONFIGDIFF_GATE_LOGS:-$(mktemp -d /tmp/configdiff-gate.XXXXXX)}
mkdir -p "$configdiff_logs"
configdiff_after_gate() {
    local go_rc=$? config_rc=0 fixture_uid
    local -a report_args=()
    trap - EXIT
    printf '%s\n' "$go_rc" > "$configdiff_logs/go-exit.txt"
    # The official script sets got only after acquiring its shared server slot.
    if [ -z "${got:-}" ]; then
        echo 'configdiff: official gate did not acquire a slot; full suite not started'
        exit "$go_rc"
    fi
    cd "$configdiff_root" || exit 2
    # A second host may still be returning an earlier snapshot. Keep this
    # run's evidence in distinct files until both foreground commands finish.
    if [ -n "${CONFIGDIFF_REPORT:-}" ]; then report_args+=(--output "$CONFIGDIFF_REPORT"); fi
    if [ -n "${CONFIGDIFF_FINDINGS:-}" ]; then report_args+=(--findings "$CONFIGDIFF_FINDINGS"); fi
    fixture_uid=$(id -u)
    server/tools/configdiff/native_network.sh env \
        XDG_RUNTIME_DIR="/run/user/$fixture_uid" \
        DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$fixture_uid/bus" \
        systemd-run --user --scope --quiet --collect -p MemoryMax=14G -p MemorySwapMax=0 \
        "${CONFIGDIFF_PYTHON:-$HOME/cf-test/.venv/bin/python}" server/tools/configdiff/run.py \
        --groups all --baseline "$configdiff_baseline" "${report_args[@]}" > "$configdiff_logs/configdiff.log" 2>&1 || config_rc=$?
    printf '%s\n' "$config_rc" > "$configdiff_logs/configdiff-exit.txt"
    rg -v '^SUMMARY ' "$configdiff_logs/configdiff.log" | tail -8
    echo "configdiff gate: Go=$go_rc configdiff=$config_rc logs=$configdiff_logs"
    [ "$go_rc" -eq 0 ] || exit "$go_rc"
    exit "$config_rc"
}
echo "configdiff gate: logs in $configdiff_logs; baseline $configdiff_baseline"
trap configdiff_after_gate EXIT
# Official gate owns concurrency, builds, race and proddiff; never duplicate them here.
# The official script resolves server/ from $0, so Bash's argv[0] must identify it
# while sourced as well. All wrapper paths were resolved before this assignment.
BASH_ARGV0="$configdiff_root/server/scripts/gate.sh"
source "$BASH_ARGV0" "$@"
