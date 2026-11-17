#!/usr/bin/env bash
# Run through rt on a test server. The repository gate owns shared validation.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../../.." && pwd)
cd "$root"
export LANG=en_US.UTF-8
export GATE_LOGS="${GATE_LOGS:-$(mktemp -d /tmp/configflow-r09-gate.XXXXXX)}"
server/scripts/gate.sh -p 6 \
 FuzzR09DNSGuardManifest:./internal/routers \
 FuzzR09DNSGuardOperation:./internal/routers \
 FuzzR09PolicyEvaluator:./internal/routers
PYTHONPATH=. "$HOME/cf-test/.venv/bin/python" server/internal/routers/testdata/r09_mutations.py | tee "$GATE_LOGS/mutations.log"
printf 'R09_ALL_GATES_PASS\n'
