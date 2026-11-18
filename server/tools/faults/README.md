# Black-box fault scenarios

Run only on a test server, with synthetic data:

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- server/tools/faults/run.sh
# Repeat one scenario:
RT_HOST=sunbroke@192.168.0.32 rt -- server/tools/faults/run.sh --scenario sigkill-metrics
```

`run.sh` builds the real server twice (production and `configflow_difftest`) plus
the small Go fixture seeder. It then creates a user/network/mount/PID namespace
with **only loopback**, hides host resolver sockets, drops capabilities, and runs
`run.py`. Every scenario gets a fresh marked DATA_DIR under a private
`~/.cache/configflow-faults.XXXXXX/results/`. No existing data/snapshot is accepted.
All servers/fakes listen on 127.0.0.1; their children and sockets are reaped on exit.
The build directory is removed, while private logs, data and `report.json` remain.
An error is a FAIL, not a skip. This is a targeted test tool, not a full gate.

The scenarios cover instance/Python-style lock exclusion, startup wait signals,
SIGTERM with heartbeat/refresh/WebDAV work, both metrics saves, SIGKILL in refresh,
metrics and before SQLite COMMIT, repository lock timeout/cancellation, read-only
DB/cache writes, restart/retry, five client-disconnect controls, and 1000 mixed
requests returning to the goroutine baseline ±5. The 30s orphan-barrier scenario
checks the actual bound without replacing it. HR device scenarios are extensions;
the report does not claim those are covered yet.

Instrumentation exists only under `configflow_difftest`. Production Point is an
empty inlineable function, tested/benchmarked with zero allocations, and production
routes contain no `/__faults/`. `/__faults/state` checks the actual peer's loopback
address plus a random token from a mode-0600 file in the mode-0700 control directory;
it returns only goroutine counts and registered phase names. Tokens never appear
in reports. Barriers honour ctx and release after at most 30s, recording timeout.
They cannot disable loops; the normal production lifecycle timings remain active.

## Add a scenario (HR owners)

1. Add a function in `scenarios.py` (or import a domain scenario module from
   `run.py`) decorated with `@SCENARIOS.register("r12-mid-publish", covers=["5.7",
   "R12"])`. It receives an Environment. Obtain a fresh World with `env.world()`;
   for a custom loopback device fake, use `env.shared_source(MyFake)` and pass that
   source into the World. Extend the Go fixture seeder or configure through real APIs;
   retain its marker/empty-directory/loopback checks. Never load production data.
2. Establish a real boundary: a fake transcript must prove a device write or I/O
   arrived, or arm a registered barrier with `world.arm("store")` and wait with
   `world.entered("store")`. If a new boundary is necessary, obtain design approval,
   define its Phase constant and Catalog entry in `internal/faultprobe/points.go`,
   and call Point(ctx, Constant). Do not add private sleep hooks or magic timeouts.
   The harness reads the Catalog from the compiled fixture; unknown names fail.
3. Send the actual signal through `world.signal`/`kill`, remove the fault, restart,
   and assert domain evidence (manifest/receipt/revision/ownership, compensation
   transcript, and a successful next operation). Use `world.check` for reportable
   assertions and `world.audit` for all SQLite integrity checks/committed JSON.
   A signal or a readable DB alone does not prove recovery. No empty scenario passes.
4. For disconnects, compare with an uninterrupted clone of the same synthetic
   seed. `convergence_snapshot` compares exact artifact paths/modes, SQLite tables,
   JSON, task counters/statuses and file bytes. It only normalizes the explicit
   CLOCK_FIELDS, SQLite commit IDs, the instance diagnostic, the known panel origin, the one provider generated-time comment,
   and consistent generated subscription-node identities (content-addressed, with
   references remapped). Only `app.log` is excluded as diagnostics. WAL/SHM/journals
   are validated through SQLite instead of compared as physical bytes. Do not add
   broad directory/key exclusions; normalization controls must still catch lost
   data, wrong counts/revisions and broken references. A torn final metrics line
   is allowed only by the existing loader's documented contract; valid prefix and
   successful subsequent flush must be demonstrated.
5. Run the new scenario on .32, its control tests, Go race for changed packages,
   production isolation and the O3-7 AST gate. Save exact commands and the private
   report summary. Add coverage to the report; never register a placeholder pass.

Control tests: `cd server/tools/faults && PYTHONDONTWRITEBYTECODE=1 python test_core.py`.
