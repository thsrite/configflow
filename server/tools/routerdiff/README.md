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
