#!/usr/bin/env bash
# Run through rt on a test server. The repository gate owns shared validation.
set -euo pipefail
root=$(cd "$(dirname "$0")/../../../.." && pwd)
cd "$root"
export LANG=en_US.UTF-8
server/scripts/gate.sh -p 4 \
 FuzzR09DNSGuardManifest:./internal/routers \
 FuzzR09DNSGuardOperation:./internal/routers \
 FuzzR09PolicyEvaluator:./internal/routers
PYTHONPATH=. "$HOME/cf-test/.venv/bin/python" server/internal/routers/testdata/r09_mutations.py
printf 'R09_ALL_GATES_PASS\n'
