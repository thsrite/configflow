# Routers public HTTP differential — Step B

Test-only code derived from parity/difftest-routers.md (including O6), Python,
routes.tsv and frontend APIs. Do not read Go business implementation or call
private Go helpers. Python/Go run as real public processes with their normal
startup/background loops. Device fakes will be independent per-side network
namespace, not shared. Production/PVE/home devices are out of scope.

The matrix inventory is a design ledger, not executable/pass coverage. Concrete
recipes must name entries and validate their Python reference prerequisite before
a result can count. Actual Go route registration determines unimplemented routes;
a business 404 and routes.tsv's stale todo fields never do.

All commands run on .32 through rt. Initial inventory gate:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server/tools/routerdiff && ~/cf-test/.venv/bin/python -m unittest -v test_inventory'
```

The public-process/fake/strict comparison runner is being built. No complete HTTP
coverage is claimed by this initial scaffold.

Current executable foundation uses namespace.sh/worker.py, two public processes,
independent stateful HTTP peers and logical SQLite/filesystem snapshots. Python
starts with `-m backend.app` and real non-DEBUG startup loops. The Go build overlay
only observes the public no-op manifest hook; no lifecycle/business patches.

Iteration (pin one native host; .32 and .64 are both supported):

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; routerdiff_build=$(mktemp -d /tmp/routerdiff-build.XXXXXX); ~/cf-test/.venv/bin/python build.py "$routerdiff_build"; ROUTERDIFF_BINARY="$routerdiff_build/configflow" ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$routerdiff_build/configflow" --output /tmp/routerdiff-results.json --findings /tmp/routerdiff-findings.md'
```

The runner's default gate exits 2 while ANY reviewed design entry still needs a
variant/reachability audit. `--allow-incomplete` is explicitly for iteration;
it does not mark entries covered, hide missing Go routes or downgrade differences.
Exit 1 denotes actual compared differences; fixture/precondition errors abort and
persist their error in the JSON ledger. Each group atomically checkpoints the
ledger, so an interrupted run cannot silently leave an older report looking new.
Do not call the current foundation a completed Step B acceptance suite.
