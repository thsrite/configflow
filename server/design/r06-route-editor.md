# R06 conservative manual route editor

Scope: port route_editor.py, routeros_routes.py and ikuai_routes.py in the existing
routers package. HTTP registration, auth, repository lookup and L1 job locks remain
R15. No manifest, Agent, NAT or DNS side effects.

Contract: EditableRoute carries typed RouteFields and an open vendor Row snapshot;
PublicRoute omits the snapshot and adds an opaque revision. RouteValues,
RouteIntent, RoutePlan and RouteResult are typed. RouteEditor extends an existing
Driver through ReadRoutes/MutateRoute. NormalizeRouteRequest accepts RawMessage
at the HTTP boundary; no Python dynamic calling conventions. BuildRoutePlan is
pure; ExecuteRoutePlan and each vendor MutateRoute detach cancellation themselves.
RouterOperationLock exports the L2 per-(type, endpoint without trailing slash)
lock. Until lockx lands it uses the O3 transitional in-process lock map; waiters
count as references and unused keys disappear. Write waits are limited to 10 min;
reads retain cancellation. Callers acquire once, never recursively, and take L1
before validation where Python does. No goroutine, flock or new dependency.

State: normalize -> read -> preview -> acquire L1/L2 (R15) -> re-read connection
and routes -> rebuild fingerprint -> preflight complete stable snapshot -> write
once -> complete readback -> success. On any write/readback error, fresh evidence
first -> safe compensation -> verified recovery or KindRollback reporting the
original and compensation error. A lost compensation reply is resolved by readback,
never by blindly resending. Missing identity, reused ID, unknown form fields,
dynamic/special/VRF routes and concurrent changes fail closed.

HR issue-class handling (review-r04 F1-F10; review-r05 checklist 1-6):

- Shared client: reuse devhttp and the verified drivers without changing transport,
  auth, cookies, redirects, retries or proxy handling. No client behaviour change.
- Error types/messages: all device edit refusals and compensation failures return
  RouterError with messages.go constants; validation alone uses ValidationError.
  Compensation runs for every write error, independent of error subtype.
- Identity: a nonempty unique valid ID and complete stable snapshot are required;
  iKuai adds require a fresh unique random RT name and all form fields equal.
  Never interpret absent names as matching. RouterOS create requires a fresh trusted
  *hex ID; a matching destination alone never proves ownership.
- ID reuse/restore: restore only if the live record is still the exact desired
  snapshot; deleted rows are recreated only if ID, destination and (iKuai) name
  are free. Never write an old image over an unknown ID occupant.
- Readback: each edit proves one object with a bounded number of linear scans;
  no nested per-item scans or quadratic batch proof (R05 batch indexes stay reused). R06 edits only one route per operation.
- Refused versus uncertain: preserve driver classification. Both paths reconcile
  against fresh evidence; unchanged tables need no compensation, unproven writes
  preserve the scene. No assumed missing-object proof. No release Batch/UndoOf
  reuse: those rollbacks deliberately have weaker external-writer semantics.
- Cancellation: vendor edit and ExecuteRoutePlan detach before preflight; every
  Call/ListAll/readback and compensation gets that detached context. Device I/O
  still has its 10 s timeout. Cancellation cannot strand an accepted edit.
- Malformed/composite device data: no unchecked slices/assertions or comparable
  maps. Deep structural equality preserves composite distinctions. Unknown or
  wrong-typed stable config is visible but read-only; runtime fields are excluded.
- Advanced configuration: patch only four RouterOS fields, preserve full iKuai
  form. Check editability both before and after writing. Full snapshot comparison
  detects drift; no preview-proof omission (R04 F9).
- Batch limits and status accounting: R06 writes one row and does not use console
  scripts/batch limits or skipped-record bookkeeping. R04 implementations reused
  unchanged. Every compensation has a fresh complete verification.
- Credentials: no raw transport error or connection values enter diagnostics/logs;
  previews/fingerprints exclude password. Connection is hashed, not returned.

Buildable commits: (1) typed editor, validation/preview, L2 lock; (2) vendor
adapters with safe compensation; (3) Python-vs-Go loopback corpus, adversarial and
mutation checks, coverage ledger. Each commit updates PROGRESS and passes remote
format/vet/compilation. Final authoritative checks run on .32.

Acceptance: same real Python editors against the same loopback fake per case;
compare parsed outcomes, complete requests, empty undo records (editor owns
compensation internally) and final tables. Random RT identifiers are injected as
one deterministic synthetic UUID in the reference and a per-call private test
name seam in Go. Revisions/fingerprints are opaque native-JSON hashes (N8 D2):
assert shape and binding separately and substitute each side's own revision when
building plans, never mask any other returned field. Purposely corrupt a result,
a request body and a final table to prove comparator sensitivity. Cover the
three source test files explicitly in PROGRESS. R15 HTTP differential remains
blocked on R15: do not register fake partial production handlers to claim coverage.

L5-A remaining: firmware form acceptance/encoding/readonly defaults and runtime
convergence; real ID allocation/reuse, full-table pagination under device churn;
lost management link and identity after recreation; real routing/VRF/ECMP/dynamic
entry protection, active forwarding and persistence over reboot. Fakes prove
protocol and conservative compensation, not firmware or actual packet paths.

Accepted deviations: native opaque JSON hashes; malformed RouterOS comment objects
remain in snapshots and are read-only but display empty instead of Python repr;
add identity fields accept absent/null/empty strings, rejecting falsy wrong types
that Python accepted. Device errors echoing credentials are replaced with a Chinese
redacted diagnostic instead of exposing the text (matrix X01/O9 security fix).
