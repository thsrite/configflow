# C06 HTTP shell, authentication and settings

The HTTP shell owns routing, authentication gates, binding and response policy.
Settings handlers translate requests into package calls. Persistent operations
belong to owners, which detach from client cancellation while retaining Runtime
shutdown cancellation. C07 listener internals and C13/C35 operations retain their
existing owners.

## Clarified in C06 completion (2026-11-18)

Coordinator decisions:

- HEAD on `/api/settings/rule-fetch-proxy`, `/api/settings/web-port`,
  `/api/server-domain`, `/api/config-token`, `/api/backup/config` and
  `/api/settings/subscription-aggregation` is exactly GET without a response
  body. Ignore any body without binding it. Never enter a save branch; DB rows,
  files and revisions remain unchanged. This fixes Python's unsafe non-GET
  dispatch and its missing HEAD return values. Only web-port HTTP dispatch is
  changed; C07 owns the listener.
- Migrate settings request readers to `Ctx.Bind` for Python `request.json`,
  `Ctx.BindSilent` for `get_json(silent=True)`. Strict syntax, media type and
  incompatible object shapes use N8-6's native 400/415 policy, even where Python
  caught those failures and returned 500. Silent readers retain their business
  validation responses. NaN/Infinity/-Infinity are rejected in every body reader under this policy; explicit real-Python vectors record both changed statuses/messages and aggregation writes that now get rejected without any commit. Preserve raw member presence and wire types so existing
  field validators and permissive stored settings retain their semantics.
- The native request DTO has a temporary adapter to existing jsonx repository
  and validation APIs. N8-5 removes the adapter when typed repository consumers
  are migrated; it must retain unknown stored fields, explicit null, numeric
  validation and the selected binding policy.
- Wait for O3-6a before changing overlapping owner structures or removing
  backup's handler `WithoutCancel`. Afterwards, each C06 write owner checks
  `ctx.Err()` after Detach and immediately before every commit/shared write.
  Test real Runtime.Start/BindRequest/Shutdown: stopped runtime cannot write;
  disconnected requests finish all writes while Runtime is live.
- Shared final commit windows are **known and assigned**: O3-6c, owner
  `6e5c7760`, handles repo final commit, taskstatus context entries, rulefetch
  touch/rename and the archive chain. C06 must not invent parallel helpers.
- MCP authentication (finish), C34 config/export/generate HTTP entry points and
  C33 proxy-group CRUD remain their owners' work. Contract tests close C06/C11
  only; they do not claim those external routes have been implemented or tested.

## Acceptance

`api/settings.TestSettingsHEADIsReadOnly` compares GET/HEAD headers, all files and
the complete store fingerprint including revision, with valid, malformed and
non-object bodies on each path. `httpx/difftest.TestC06SettingsFlaskStorageBoundary`
compares real Flask responses and complete meta/sections/fields tables plus
artifact files after each write/rejection. Random commit IDs and volatile
timestamps alone are normalized across independent commits; generation and
field revisions are retained. Named HEAD and strict-binding differences are
asserted explicitly, not excluded from the differential.

`TestPythonAuthenticationMatrixForEveryRegisteredRoute` traverses every current
Go registration and compares the global gate with the actual Flask registrations
and gate, including HEAD/OPTIONS and anonymous/invalid/valid JWT. Stub leaf
handlers prevent network/device actions; self-authenticating leaf protocols stay
with their modules. `TestPythonTextRedactionAndStoredSecretFinder` checks the real
Python helpers, encoded variants, overlapping Unicode ranges and frozen secrets.

Exact source-case mappings, commands and outstanding integration steps live in
`server/parity/progress/c06.md` and `c11.md`. Separate independent Codex reviews
are required; implementation evidence does not set either module verified.
