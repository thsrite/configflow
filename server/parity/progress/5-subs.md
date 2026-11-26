# Package 5: subs (subscription parsing) — progress

## Brief
Port to `server/internal/subs`: `subscription_parser.parse_uri_list` (+ per-scheme parsers, `parse_yaml_object`;
`parse_subscription`/`parse_clash_yaml` not ported, no caller), `subscription_hosts`, `subscription_filter`,
`subscription_dialers`, `proxy_chains`, `mihomo_listeners`, `proxy_server_dns`, and `commented_mihomo`
(ruamel round-trip listener edit, sub-package `subs/commented`, byte-identical output). Subscriptions themselves
go through Sub-Store (#13). Follow-up from the coordinator: the commented editor must not lose features —
reduce refusals (`无法安全保留…`) to zero on realistic configs, then per category (numbers, comment before `---`,
folded/tags, comment on dash line, CRLF incl. ruamel's mixed `\r\r\n`, alias-to-commented-block last);
invariant 0 mismatches (refusing beats different bytes). `\N{name}` filters and the 24 Python-crash anchor
rows stay documented as is.

## Done (all tests pass on the test server)
- Modules ported + corpora (hand-back sent earlier): parser, filter, hosts, dialers, chains, dns, listeners,
  commented; tldextract 5.1.2 snapshot + matcher (`psl.go`, 89c701bc); lazy `ReportedCoreVersion` (d981fb7c);
  compat golden cross-check (0c980414).
- Follow-up:
  - realistic corpus `commented_realistic` (128 repo base configs × listener layouts, 4911 rows): 0 refused,
    0 mismatches (f233dcb6).
  - Category numbers: ruamel resolver (first-char lookup), RT int/float constructors + representers,
    generated anchors (id001) for OctalInt/date, dropped anchors for sexagesimal/inf/null; `commented_scalars`
    corpus 2080 rows (307cca7a). Negative hex/bin `!!int '0x-1F'` reloaded via a decimal substitution because
    yamlx cannot construct it.
  - Category comment before `---`: ruamel drops them, the missing-comment step re-adds them on top (b419883a,
    refined in the folded commit).
  - Category folded `>`: scanner with `\a` fold marks, FoldedScalarString fold_pos, write_folded port
    (fd980e75); a folded scalar ending in spaces leaves the line open (the next blank line only ends it).
  - Category tags: block-context `!!str` (TaggedScalar kept), `!!int/!!float/!!bool/!!null` and verbatim
    `!<tag:yaml.org,2002:..>` (constructed + represented); `commented_blocks` corpus 1112 rows (9c916e0e).
  - Category comment on a bare dash line holding a nested block: comment and following lines move above
    the dash at their column, before removed-key comments (365fe740).
  - Category alias to commented block: head comment (`key: &a # c` on a block collection) kept on the node,
    written on its own line (and above the dash of an expansion); inner comments travel with copies;
    merged entries carry no comments; ruamel-crash combination refused; anchors corpus extended to 1965 rows.

## Current counts (refused / rows; mismatches 0 everywhere)
- commented (random): 118 / 2411 — CRLF with comments 118.
- commented_anchors: 33 / 1965 — commented block collection expanded inside a flow collection 33 (+24 Python crashes, Go refuses them too).
- commented_realistic: 0 / 4911.
- commented_scalars: 16 / 2080 — yamlx cannot parse source (`1,000` in flow) 8, lone indicator `-` 4,
  comment on null item of nested seq 4.
- commented_blocks: 14 / 1112 — `!!binary`/`!!map`/`!!seq`/`!!set` tags 12, yamlx parse 2.

## In progress
Nothing. Follow-up handed back (2026-11-15); all 12 corpora reproduce from gen.py (checked on the server).

## Next (only if the coordinator asks)
1. CRLF documents with comments/blank lines (118 random rows): needs a port of RoundTripScanner comment-token
   formation (comment text keeps its `\r`, blank lines become `\n`, post tokens lose the final `\n` and get
   `\r\n`, plain scalars followed by blank lines form `\n`-only tokens, CRLF blank lines skip the
   empty-line branch) — estimate 1–2 days; refused for now.
2. Commented block collection expanded inside a flow collection (33 anchor rows): ruamel writes the block's
   comments inside the flow text with line breaks (`[\n  # base\n {k: 1 # k\n, ...}]`) — estimate half a day.
3. Smaller leftovers: `!!binary`/`!!map`/`!!seq`/`!!set` tags, lone `-` scalar, comment on a null item of a
   nested compact sequence, sources yamlx cannot parse (`1,000` inside flow, package yamlx).

## Decisions and findings
- Refusals return Python's failure message; route answers 400; original text kept.
- `compat.go` = private copy of dialer_references (#6 to replace); non-string-key proxy strings and 3 malformed
  profiles differ (documented in compat_golden_test.go).
- `\N{name}` filter patterns: filter not applied (rules engine), documented.
- Debug helper lives outside the repo (/tmp/zz_debug_test.go.keep + temporary `DebugLast` var); never commit.

## Commands
- Tests: `rt -- bash -lc 'cd server && gofmt -l internal/subs; go vet ./internal/subs/... && go test -count=1 ./internal/subs/...'`
  (+ `-race` once at the end).
- Corpus stats: `go test -count=1 -v -run TestCommented ./internal/subs/`.
- Regenerate one corpus: `rt --back server/internal/subs/testdata/<name>.jsonl.gz -- bash -lc 'GEN_ONLY=<name> PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/subs/testdata/gen.py'`
- Check all corpora reproduce: copy gen.py to /tmp/genchk on the server, run it there, `cmp` with testdata.
