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
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'set -euo pipefail; cd server/tools/routerdiff; export PYTHONDONTWRITEBYTECODE=1; routerdiff_build=$(mktemp -d "${TMPDIR:-$HOME/.cache/rt-tmp}/routerdiff-build.XXXXXX"); ~/cf-test/.venv/bin/python build.py "$routerdiff_build"; ROUTERDIFF_BINARY="$routerdiff_build/configflow" ~/cf-test/.venv/bin/python -m unittest -v test_inventory test_seed test_compare test_recipes test_console test_boundary; ~/cf-test/.venv/bin/python run.py --allow-incomplete --binary "$routerdiff_build/configflow" --output "$HOME/.cache/routerdiff-results.json" --findings "$HOME/.cache/routerdiff-findings.md"'
```

The runner's default gate exits 2 while ANY reviewed design entry still needs a
variant/reachability audit. `--allow-incomplete` is explicitly for iteration;
it does not mark entries covered, hide missing Go routes or downgrade differences.
Exit 1 denotes actual compared differences; fixture/precondition errors abort and
persist their error in the JSON ledger. Each group atomically checkpoints the
ledger, so an interrupted run cannot silently leave an older report looking new.
Do not call the current foundation a completed Step B acceptance suite.

Source-unreachable guards are separate ledger outcomes, never executed passes.
`reachability.py` names the full Python call-chain proof and actual public
controls. All those controls must complete before `source-unreachable` is emitted;
missing/bad controls keep it pending. These H IDs are not execution tags on the
control recipes. Reports separate `audited_entries` (executed variants) from
`source_unreachable_entries`; every proof is marked `execution_claim=false`.
No branch is made reachable by patching a Python driver, class or helper.

The two old R001 global P1/P2 entries explicitly struck out by the approved O6
matrix are `review-superseded`, not unreachable-source branches or executed
passes. Their replacement refusals and ignored-selector controls must all run;
otherwise they remain `review-proof-controls-pending`. The report keeps a
separate `review_superseded_entries` count and the full2301-entry denominator.

A native main, preparatory or settling-request connection/read/protocol failure is retained as actual
transport/process metadata with no invented HTTP status/body. A completed main
response remains visible if only its later settling request fails. Later dependencies
are blocked. Immediate snapshots after client failure explicitly do not prove
quiescence or compensation. Worker exception classes are structured observations;
RPC/fixture failures and Python precondition failures are kept distinct. Oracle
negative controls kill only an owned disposable process and do not enter the
product findings/corpus counts.

Store commit IDs are paired only after full candidate DB equality and own raw
store/generation/config provenance validation. Only meta.commit_id is projected;
no generic UUID replacement. Physical revisions, generations, stable metadata,
config values and file contents stay strict. Metadata-only commits and literal
legacy missing/null/empty IDs are handled separately. Real native write/read and
owned-store corruption controls test this rule without entering routers counts.

R002 GET/R003 PUT oxidns_revision is an approved Go-native canonical content
hash. Its own stored-settings relation must be deterministic and collision-free
across observations; only equal settings and equal other response members permit
pairing the hash text. Names and counters are excluded dependencies. All real
Pairs validate the reference even without Go, and raw own-side references stay
untouched. HEAD/OPTIONS have no body token. Workspace hashes and signed previews
are separate pending binding contracts.
