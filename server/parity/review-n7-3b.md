# N7-3b — Node reader migration and adversarial boundary review

Branch: `go-n7-3b`, originally based on `go@6090e113`. No sub-agents, production
access, push or private-input commits. Final acceptance host is explicitly
`RT_HOST=sunbroke@192.168.0.32`; .63 was unavailable. Rebase/integration results
and final command tails are recorded below when complete.

## Migrated call sites

| Package / entry | Old boundary | Native boundary |
|---|---|---|
| rulefetch.normalizeMihomoYAML | Compose/Scan/LoadWith, private source spans | Parse/Node, shared Edit splice then D3 |
| rulefetch.domainEntries / InspectDomainContent | Compose and scalar marks | Parse, direct payload scalar Nodes; Edit |
| rulefetch.remoteTextEntries / ValidateRemoteRuleContent | Compose + token anchor scan | Parse, physical Node anchor/alias visits |
| rulefetch.ReadRulePayload | LoadWith + Compose | Parse, payload-only scalar access |
| rulefetch.AppendRulePayload/AppendRuleLines/RemoveRulePayload | private marks/insertion/removal | shared Edit, Node sequence edits |
| rulefetch.pyerrors | recursion exception probe | removed unused emulated probe |
| refresh health/cache tests | SafeLoad | Parse/typed Unmarshal |
| refresh.SourceCacheMetadata / RememberSourceDownload | delegated reads / write before policy check | shared Parse policy admission before metadata/cache writes |
| refresh.DownloadSubscriptionNodes / FetchSubscriptionSource | remember downloaded text before parsing | same admission gate through RememberSourceDownload |
| convert.loadProxyYAML / ParseStructuredProxyString / InspectStructuredProxyString | legacy LoadWith; private yaml.v3 decoder/resolver/constructor, recursive placeholder | shared Parse + AllPairs; memoized containers; scalar-only ToJSON |
| api/subscriptions.yamlContent | yaml.Unmarshal on own generated JSON | FromJSON + Marshal, no YAML parser |

The old convert/proxyyaml_loose.go was deleted, including custom scalar typing,
merge flattening, CPython character checking and recursive null-sentinel path.
The cache admission estimate now accounts for Go values; it no longer imitates
CPython allocations. Cache admission never refuses otherwise valid input.
Legacy yamlx loaders and internal/pyyaml are intentionally retained for the other
N7-3 migrations and N7-3d cleanup.

Shared changes cherry-picked from N7-3a: 0b98b42e (tag shape), dc3f339c (AllPairs),
23196ddd (merge/scalar validation), a7acc788 (error identities), a5f553d0 (Chinese syntax diagnostics),
4ad822f5 (unbounded decimal ints),942e752f (JSON key projection),09740db3
(writer tag preservation; logical equivalent of ae109f82). No duplicate
policy/merge implementation. Remaining bare yaml.v3 readers outside this scope
(importer, rules, OxiDNS) were reported and assigned to N7-3d by the coordinator.

## Findings and resolutions

- High: the private proxy decoder/constructor path bypassed the facade's D1,
  tag/merge and alias protections, and manufactured recursive placeholder values.
  Every YAML path now enters Parse; aliases are preserved by memoized container
  translation or refused by the shared recursion/amplification checks. Containers
  are never JSON-serialized once per occurrence. Shared results bypass cache
  DeepCopy; ordinary cached results remain isolated between requests.
- High: proxy policy failures were indistinguishable from URI/base64 and could
  fall through to Sub-Store. Both internal public readers now return an error;
  only true ErrSyntax/no structured result retains fallback. ProxyYAMLError keeps
  the yamlx cause and the existing strategy validation channel. No bad input is
  silently retried, selected, or written as null.
- Medium: refresh remembered downloaded text before the graph was validated.
  Metadata extraction and transport cache admission now refuse ErrPolicy and
  alias errors before writes. Four entry tests preserve both old cache values. Fresh historical transport
  cache reuse also validates before returning text and retains refused bytes.
- Medium (Python bugs fixed): clearing an unindented list emitted invalid YAML;
  alias-containing flow append could insert before an existing item. Native
  sequence edits emit a valid empty list and append after all old rules.
- Shared JSON boundary: private key conversion is gone. ProjectJSONKeys handles
  StringifyScalarKeys, including last-value/first-position converted-name
  collisions as in the original boundary; unsupported/complex keys fail clearly.
  Scalars are memoized as well as containers, so a1MiB scalar with100 aliases
  shares its backing storage instead of being serialized/copied per occurrence.
- Python/old-Go bug: a leading comment or document marker hid a flow-root
  payload from the shared format hint. That could append invalid YAML or yield
  empty DNS rule sets. The minimal shared hint fix and nil-payload re-read
  recognize it correctly; plaintext remains opaque. No matcher reader changed.

Append-channel closeout (`423dea5d`): the three Append internal APIs and
Rules.ConnectionDeps callback now return(string,error), rather than silently
returning source on Edit failure. Rules/connection and discovery/write propagate
errors before claiming success; direct nil/non-nil payload tests preserve source
and assert native policy/alias identities plus the typed400 channel. Remove edit
errors use the same validation wrapper. Plaintext syntax fallback remains;
policy/alias failures never fall back. Rulefetch fuzz also covers append.

## Error-return caller choices

`74c9e28d` isolates the signature/caller adaptation as requested. Generation
(ConvertNodeToMihomo), convert/repo rawDialer, repo import dialer rewrite and
provider delivery use their existing error-return paths. Repo maps native proxy
validation to its ValueError channel (400), rather than an unintended TypeError.
health.needsSubStore does not count a policy-refused string as remote conversion;
health.proxyOf uses its existing failed-probe result after conversion fails.
The existing api/subscriptions.cachedProxies lenient batch is handled explicitly:
ProxyYAMLError skips one node, logs only its index and error category, and increments
a local skipped count reported by one summary warning. Other nodes still return;
strict mode returns the typed validation error without logging it. HTTP fields
are unchanged. The regression checks both paths and confirms no node text, names,
passwords or tokens appear in warnings. Dedicated tests assert
zero fake Sub-Store calls and zero cache entries for each policy/alias class.

## Migration gate and accepted differences

The unchanged Python corpora/whole-suite records remain the old boundary oracle.
Every row runs the migrated consumer, preserving response fields and exact rule
values. Only generated YAML text is compared as Nodes; independent exact CRLF/
BOM/quote/comment/no-op assertions constrain splice behavior. Refused policy
inputs must retain the original text or return the existing validation class and
the exact shared diagnostic. Special outcomes use exact native result/error
fingerprints keyed to the original suite/function/input, rather than blanket
function skips. No candidate-recording bypass remains in tests.

Rulefetch changed rows: **9,079**; classes and per-corpus counts are in
PROGRESS-config's N7-3b table. Total by class: D1 100; §5.4 39; diagnostics 6,460;
D3 layout 2,431; D3 quoted folding 11; alias diagnostic collation 6; Node mapping
keys 5; binary text 1; the two Python fixes 25+1. The final49 exceptional calls
were reviewed individually (44 distinct input fingerprints).

Convert: **48 distinct changed inputs**: native schema28, D1 4, §5.4 7, alias
recursion5, native JSON numeric spelling2, mapping order2. Both cached repeats
assert the same native expectation. Repo caller golden: **30 explicit cases**:
schema18, D1 3, §5.4 6, complex-key JSON refusal2, mapping order1. Earlier
non-string-key recorder markers/URL allowances remain separate; other rows keep
strict comparisons.

Accepted §5.7/D3 details are in behaviour-changes.md. In particular, physical
multiline quoted scalars follow native YAML folding; an illegal folded domain is
still rejected. Splice keeps every untouched byte. Sequence/layout fallback can
normalize formatting once during an actual rule edit, while preserving comments,
order and values. No-op/read-only paths keep original text and fingerprints.

C09 consumer: all1536 apply rows pass;198 native edit layouts compare as equal
Nodes, and36 malformed-YAML responses keep the original Chinese context plus
the exact native diagnostic. Other result fields remain exact. The first full
run exposed only these stale consumer assertions; they were adapted without
changing the rules runtime reader, then the full acceptance was repeated.

## Alias and fuzz coverage

Rule inspection, normalization, payload read/remove, remote validation and both
proxy readers cover depth40 shared diamonds and self-reference. Linear alias40
coverage proves either a refusal with original bytes retained or shared container
identity (no per-occurrence copying/nulls). Refresh metadata/remember/download/
preview tests reject unsafe input without modifying either cache. Fuzz targets
cover rule read/edit/validation, source policy admission, and structured proxies.
Runtime yamlx.Clone callers: zero; no private scanner or Python span emulation.

## Acceptance commands and final tails

All commands below run with `RT_HOST=sunbroke@192.168.0.32` (the updated rt
otherwise balances .32/.64). Before final rebase, format/vet, race, full, all10
differential packages, fuzz and proddiff passed. The historical-cache reuse
follow-up then passed its focused race test and is included in the post-rebase
integration check below. No completed fuzz target's implementation changed.

```sh
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && gofmt -l internal/ tools/'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go vet ./...'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -p=4 -count=1 -timeout=30m ./internal/rulefetch ./internal/refresh ./internal/convert ./internal/repo ./internal/provider ./internal/health ./internal/api/subscriptions'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -race -count=1 -timeout=30m ./internal/rulefetch ./internal/rules'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -p=4 -count=1 ./...'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && for d in $(go list ./... | rg difftest); do CONFIGFLOW_DIFF=1 go test -count=1 "$d" || exit; done'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzRuleYAMLBoundary$" -fuzztime=60s -parallel=2 ./internal/rulefetch'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzSourceYAMLPolicy$" -fuzztime=60s -parallel=2 ./internal/refresh'
RT_HOST=sunbroke@192.168.0.32 rt -- bash -lc 'cd server && go test -run "^$" -fuzz "^FuzzStructuredProxyYAML$" -fuzztime=60s -parallel=2 ./internal/convert'
RT_HOST=sunbroke@192.168.0.32 rt -- server/scripts/proddiff.sh
```

Final lines (full logs retained privately on .32 as `/tmp/n73b-*.log`):

```text
format: 0 bytes; vet: 0 bytes, exit 0
ok configflow/internal/rulefetch 34.100s   # seven-package race
ok configflow/internal/refresh 159.536s
ok configflow/internal/convert 98.750s
ok configflow/internal/repo 39.227s
ok configflow/internal/provider 109.079s
ok configflow/internal/health 138.710s
ok configflow/internal/api/subscriptions 2.648s
ok configflow/internal/rulefetch 17.619s   # consumer follow-up race
ok configflow/internal/rules 8.246s
ok configflow/internal/yamlx 2.144s        # full p4 tail, exit 0
?  configflow/tools/proddiff/generator [no test files]
ok configflow/internal/api/agents/difftest 115.703s
ok configflow/internal/api/subscriptions/difftest 81.517s
ok configflow/internal/discovery/difftest 1.974s
ok configflow/internal/health/difftest 13.660s
ok configflow/internal/httpx/difftest 1.106s
ok configflow/internal/mosdns/difftest 1.472s
ok configflow/internal/refresh/difftest 3.395s
ok configflow/internal/subs/difftest 1.582s
ok configflow/internal/substore/difftest 5.474s
ok configflow/internal/webdav/difftest 17.892s
fuzz rulefetch: 563715 executions, PASS, 61.011s
fuzz refresh: 98895 executions, PASS, 61.011s
fuzz convert: 83684 executions, PASS, 61.014s
proddiff: cases=2831 compared=415 self_check=PASS new=0 stale=0
baseline: new=0 stale=0
ok configflow/internal/refresh 1.261s       # historical-cache focused race
```

Post-rebase results pending; no test-server dependency on a is being hidden.

## Integration follow-up explicitly retained

N7-3a is not yet merged at this review's base. Per coordinator instruction, do not
wait for it: hand back this completed b implementation/acceptance, then rebase
once a lands and rerun refresh-related tests, **all** difftest packages and
proddiff. Delegated subs/substore host/dialer/proxy-list integration is not marked
verified until that run. No known open local functional finding.
