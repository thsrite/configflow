# R06 acceptance and Python test mapping

Status: implementation ported, pending fresh Opus HR review. No lab claim.

`internal/routers/r06_boundary_test.go::TestR06PythonBoundary` runs real Python
editors (`testdata/r06_reference.py`) and Go against the same loopback RouterOS
and iKuai fakes, resetting identical tables/faults before each side. 169 cases:
results (including complete entry snapshots), Chinese errors/classes, complete
parsed request sequences and final device tables. Manual editors do not emit
release undo records: the corpus asserts `records: []` on both sides and compares
the actual internal compensation requests and final table, not a fabricated undo.

Only native opaque hashes in known public views/plans are substituted. Device
snapshots are never traversed by hash normalization. The source and returned
fingerprint/revision bindings are additionally tested in `r06_safety_test.go`.
A single explicit expected difference in malformed RouterOS comment display is
asserted as read-only on both sides; all remaining fields compare unchanged.

## Python files → acceptance

Counts below are test functions, matching PROGRESS; parameterized cases expand.

| Python file | Functions | Go boundary coverage | Pending / reason |
|---|---:|---|---|
| `backend/test_route_editor.py` | 14 | `plan/{add,update,delete}`, `plan/comment/*`, `normalize/*`; `TestR06PreviewBindsFullSnapshotAndConnection` covers normalization, full snapshot revision, intent/connection binding, secret/unrelated-row independence, runtime stability, no-op; vendor corpus covers missing/read-only/duplicate drift semantics | 7 `test_api_*` / `test_manual_route_api_*` / `test_invalid_api_*` functions remain R15 HTTP acceptance; 7 pure-editor functions covered |
| `backend/test_routeros_routes.py` | 24 | `routeros/readonly/*`, `readonly_or_native/*`, duplicate IDs, runtime snapshot/minimal firmware shape, CRUD/noop, duplicate-disabled/other-table, preflight drift/failed read/changed expected identity, rejected/lost/timeout-after-commit, missing returned ID, transient/persistent/undo read failure, lost undo patch/recreate, advanced external change/ID reuse/replacement destination, new foreign duplicate cleans only proven ID, explicit undo failure; malformed raw values validated by normalize corpus before I/O | 0 skipped functions; direct malformed Python-call shapes become typed Go values + strict raw HTTP intent validation |
| `backend/test_ikuai_routes.py` | 24 | `ikuai/pagination_1001`, runtime/netmask suffix, missing/unknown/wrong-typed form fields, full-form CRUD/noop, duplicate-disabled, preflight drift/failed read, rejected/lost/timeout-after-commit, transient/persistent/undo read failure, concurrent priority changes/ID reuse/new duplicate/name occupied, unprovable add/ambiguous tag/foreign duplicate only proven new ID removed, explicit undo failure; malformed raw values via normalize corpus | 0 skipped functions; malformed pagination/changed totals/IDs also covered by inherited R05 `TestIkuaiBoundaryCorpus` list cases |

`r06_safety_test.go` adds cancellation after an accepted write (successful readback
and compensation, both vendors), already-cancelled entry, L2 cross-profile same
endpoint serialization/trailing slash identity/read cancellation/write detachment
and key cleanup, no-op without driver, and credential echoes. All tests use
loopback or injected `DeviceHTTP`; no production host or lab guest is contacted.

## Matrix coverage and outstanding HTTP work

R06 owns no routes (PROGRESS split). R019 S01–S03, F11–F13 and B01–B05 device
behaviour; R020/R021 device CRUD, normalization, previews, drift, conservative
compensation; T16 and X10 cancellation are covered at the editor/device boundary.
This does **not** claim the HTTP codes, auth/profile resolution, fingerprint
confirmation gate or database/manifest isolation of R15.

R15 must run the actual Flask↔Go HTTP cases R019–R021, especially R020 F24/X04:
`_manual_sync_lock` acquires L1 **before router lookup and malformed body checks**;
busy connection -> 409, even if otherwise 400/404. Then L2, reset repository scope,
compare connection, fresh read -> plan -> fingerprint comparison -> execute.
R021 has L2 only and waits; R019 has no operation lock in Python. Do not implement
mock Go handlers and call that production route acceptance. Current branch has no
`internal/api/routers` or R14 L1 job-lock owner, so these cases are pending R15.

## Mutation gate

Run `~/cf-test/.venv/bin/python internal/routers/testdata/r06_mutations.py` from
`server` on .32. It compiles Go overlays changing (1) returned Changed, (2) sent
gateway, (3) removing add-compensation ownership check. Each runs the real Python
boundary and **must exit 1 with a mismatch**; a compile failure does not count.

## L5-A: only the lab can prove

1. Real RouterOS/iKuai firmware accepts these payloads, encodings, complete edit
   forms and read-only/runtime/default field classifications; versions and account
   permissions (R04/R05 L5 items remain prerequisites).
2. Actual device ID allocation/reuse, recreation identity, large paginated tables
   while firmware changes rows and asynchronous active-route convergence.
3. A real management link dropped after accepting an edit; recovery evidence and
   ID identity after firmware re-creation/restart.
4. Dynamic/ECMP/VRF/recursive routes stay protected on real exports; the edited
   route really forwards packets as intended and persists over reboot.

## Deliberate differences / O12

- Native opaque revision/fingerprint JSON hashes (not persisted) change once.
- Malformed RouterOS composite comment is preserved in snapshot, visible read-only,
  public comment empty instead of Python dict/list repr. No Python repr emulation.
- Add id/revision fields accept absent, null or empty text; wrong-typed falsy
  values (false/0/empty array/object) are refused with the Chinese identity error.
- Credential echoes in vendor error text (plain or encoded) are hidden, keeping
  RouterError kind and a Chinese message; Python exposes them (matrix X01).
- RouterOS recreation additionally demands a valid fresh *hex ID; malformed
  recreation reply IDs never count as a verified restore.
- Write L2 wait limited to ten minutes per O3 (Python waits forever); unused
  connection lock keys are removed. No per-resource flock or background work.
