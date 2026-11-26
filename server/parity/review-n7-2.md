# N7-2 review (yaml.v3 comment-preserving editor)

Reviewer: fresh Opus, 2026-11-17. Branch `go-opus-review-n7-2` (cut from `go-n7-2`, rebased onto `go`).
Scope: `internal/yamlx` `Edit` (splice planner, rewrite path, comment retention, verification), the
shared `Marshal` corrections of N7-2, and the migrated caller `subs.RelinkGroupListeners` with its
transactional HTTP boundary test. Method: every edit kind walked against the code; adversarial inputs
run on the test server; the corpus comment check repeated with an **independent oracle** (ruamel's
scanner in Python, not yamlx's own comment extraction); each fix test written first and seen failing,
and later fixes purpose-broken (fix removed → test fails).

## Findings

All fixed; **0 open** (F1 high, F2 medium, F3–F6 low).

- **F1 — High (fixed): alias expansion in the editor silently replaced user values with `null`.**
  Same class as A19 F1. `yamlx.Clone` replaces an alias expansion beyond the yaml.v3 budget with
  `Null` (N7-1 contract for hand-built graphs). The budget is checked per call, so expanding a
  *subtree* of a document that `Parse` accepted can exceed it: the subtree's alias ratio is higher
  than the whole document's. Two call sites used `Clone` on user documents:
  1. `materializeEditAliases` (rewrite path), when an edit removed an anchor definition that an alias
     still used. Example: `x: &x [a ×51]`, `big: &b [*x ×31]`, `use: *b` plus 3000 ordinary
     values; deleting `big` wrote `use:` with 31 copies of `x` but some entries replaced by `null`.
  2. `RelinkGroupListeners`, which cloned the whole `listeners` sequence when it contained any alias
     or anchor. Example: `users: *u` with `u: &u [*x ×31]`: renaming or unpinning the listener's
     policy wrote the listener's `users` partly as `null`.
  Both returned success. The verification did not help: `rewriteEdit` compared the output with the
  tree *after* materialisation (already corrupted), and relink's corruption happened inside the
  callback, so the "intended" document already contained the nulls. Python's `deepcopy` keeps
  sharing and kept every value. Expansion also inflated the stored config.
  Fix: (a) materialisation never expands. The first consumer that the emitter reaches becomes the
  anchor's definition (anchor kept), later consumers stay aliases; an alias line comment on a block
  collection moves to its head comment, because yaml.v3 does not write that position. (b) The
  rewrite path verifies against a copy of the caller's document taken before any preparation step.
  (c) Relink copies only containers that an alias or anchor makes reachable from elsewhere, one
  level deep (children stay shared, unexpanded). Every matched item of a copied sequence is copied.
  A renamed target replaces the value node (comments kept) instead of mutating a shared scalar.
  Ordinary unshared listeners are still edited in place, so they keep the splice path.
  Tests: `TestReviewN72DisplacedAnchorKeepsAliasChain`, `TestReviewN72RelinkKeepsAliasChains`
  (rename + unpin, output size and comment checked), two Python-vs-Go HTTP boundary cases (alias
  chain renamed / unpinned) in `subs/difftest`, fuzz seed for a displaced anchor. Before the fix
  both unit tests showed the `null` entries.
- **F2 — Medium (fixed): valid edits refused when the splice planner's text failed verification.**
  A splice whose output did not reparse to the edited document returned `ErrUnsafeEdit` (route 400)
  instead of using the verified whole-document path that D3 provides. Reproduced: deleting a key
  whose value is an indentless block sequence (`a:\n- x\nb: 1`, the common Mihomo layout
  `proxies:\n- …`, also nested inside a sequence item). The line range stopped at the first `-` line,
  leaving the entries behind as invalid YAML. A key renamed beyond the 1024-character implicit-key
  limit was also refused. Fix: a splice that fails verification falls back to the rewrite path.
  Indentless sequence entries (`-` at the key's indentation, when the value is a block sequence
  starting there) now belong to the removed pair, so that case splices and keeps every other byte.
  Tests: `TestReviewN72SpliceFallsBackOrCoversIndentlessSequences` (exact splice bytes for both
  indentless cases, long key via rewrite); fuzz seed for the indentless shape.
- **F3 — Low (fixed): rewrites wrote every merge key as `!!merge <<`.** yaml.v3's resolver does not
  know `<<`, so its Node encoder adds an explicit tag. Every rewrite of a base config that uses
  `<<: *anchor` (common for proxy groups) changed all such lines in the user's text. Meaning was the
  same for yaml.v3 readers. Fix in the shared `Marshal`: an untagged, plain merge key is written as
  `<<`, which reads back as the same merge key. A source that spelled `!!merge` explicitly keeps it.
  Test: `TestReviewN72MergeKeysStayImplicit` (block, flow, explicit tag).
  **Follow-up (coordinator's .32 fuzz, `FuzzEditScalar` seed `4d82c85ec5bc765e`):** the same
  resolver gap made a *string* `<<` come out plain, which reads back as a merge key, so editing a
  value to the text `<<` was refused (safe 400, but a valid edit). This predates the review: the
  property test below fails identically on the pre-review commit aaaf0fcf, and F3 only touches nodes
  tagged `!!merge`. Now `Marshal` quotes a plain string `<<`; merge keys stay plain `<<`. Property test
  `TestReviewN72StringsSurviveMarshal`: about 1,700 strings (YAML 1.1/1.2 special plain scalars,
  indicators, numbers, timestamps, white space and line separators, every one- and two-character
  string over a 41-character alphabet) × 5 positions (root, value, key, flow item, flow key) × 4
  styles must read back as the same string. `<<` was the only failure.
- **F4 — Low (fixed): comments of removed pairs and alias items moved to the top of the document.**
  On the rewrite path every orphaned note went to the document head. Python's editor put a removed
  `proxy`'s notes before its listener. In the frozen corpora 335 of 11,309 accepted edits moved a
  comment to the head, e.g. `- proxy: Pinned ##double` as first key of an item, `- <<: *base # merge`,
  or `- *base # list` (`Clone` drops the alias node's own comment). Fix: notes of a removed pair go
  before the sequence item that held the mapping, or before the next key of another mapping (else
  onto the mapping itself). Alias-item comments travel with the copied listener. Now 5 rows remain.
  Three of them had the note at the head already (after `---`); two are random-corpus notes at
  invalid indentation that yaml.v3 itself relocates. Tests: `TestReviewN72RemovedPairCommentsStayNearTheirMapping`,
  `TestReviewN72RelinkKeepsAliasItemComments` (purpose-broken: without the block line-comment move,
  `# list` goes back to the head), Python-vs-Go case "merged listener comments".
- **F5 — Low (fixed): multi-document base config answered with English text inside the Chinese
  message.** `Parse` returned the bare sentinel, so relink answered
  `Mihomo 基础配置无法解析，未修改策略组或入口引用：yamlx: more than one document`. The design asks
  for Chinese facade diagnostics in `yamlx/messages.go`. Now
  `…：YAML 只能包含一个文档（第 2 行开始第二个文档）`, still `errors.Is(err, ErrMultipleDocuments)`.
  Python answered 400 with the context only (no reason). Ledger updated.
  Tests: `TestReviewN72MultipleDocumentsDiagnostic`, `TestReviewN72RelinkMultipleDocumentsMessage`.

- **F6 — Low (fixed; found by the final `FuzzNativeRoundTrip` run on .32): a tag without a type
  name could be read but not written.** `!%21` (also `!<tag:yaml.org,2002:>`) parses as the tag `!!`;
  yaml.v3's emitter writes it back as a bare `!!`, which is not YAML (its tag analysis accepts an
  empty suffix after the `!!` prefix). A rewrite of such a document was refused; `Marshal` broke the
  Parse→Marshal→Parse property. PyYAML refused the input (`could not determine a constructor for
  the tag '!!'`). Now the shared node validation (Parse, Decode, Marshal) rejects that one tag with
  `YAML 标签 !! 缺少类型名（第 N 行）` (`ErrSyntax`). Every other tag round-trips, because the emitter
  %-escapes the rest (`!%21%21` → `!!%21`). Tests: `TestReviewN72EmptyYAMLTagRejected`, fuzz seed
  `fe71577a44d84ad1`.

## Checked without findings

- **Splice bounds.** Scalar patches cover exactly the old token: plain tokens must equal the node
  value on one physical line (multi-line plain refused); quoted tokens must close on the same line.
  Tab-only continuations, BOM offset on line 1, UTF-8 columns and CRLF/CR line ends were checked.
  Patches are sorted and overlapping patches refuse. Pair deletions are refused for flow parents,
  first key after `- `, explicit keys, anchors/tags/block scalars, empty plain scalars, multi-line
  flow values and anchored parents, and when they would empty a mapping. Removed-range notes are
  counted against kept text. Every splice is reparsed and compared with the edited tree (not with a
  reparse of itself). Equal is a semantic comparison (tags, numbers by value, merges and aliases
  resolved), so nothing outside the edit can change meaning undetected.
- **Comments, independent oracle.** All 11,309 accepted corpus edits (five frozen corpora) were
  dumped and their comment tokens compared with ruamel's scanner: **0 rows lost a comment** before
  and after the fixes. Comment text inside scalars is never counted.
- **Anchors and aliases.** Editing a value inside an anchored block on the generic path changes the
  alias consumers too (the anchor is kept by the rewrite), as YAML requires. Relink never changes
  other consumers, like Python's `deepcopy`: shared listener, shared sequence, merged listener,
  alias target, scalar alias (Python refused that one; Go's success is the recorded improvement).
  Recursion and over-budget documents are rejected by `Parse` before the callback runs; a cycle
  created by the callback returns `ErrUnsafeEdit`. Pre-edit walks never follow aliases, and each
  alias target is walked once per alias, which the Parse budget bounds.
- **D1.** Duplicate keys: `errors.Is` `ErrDuplicateKey`/`ErrSyntax`, `errors.As`
  `DuplicateKeyError{Key, Line}`, Chinese message with key and line appended to the relink context,
  source and transaction unchanged (unit + Python-vs-Go HTTP boundary cases).
- **D3 limits.** The rewrite drops directives, `---`/`...` markers and the BOM, normalises indentation
  to two spaces and blank lines. Quoting, flow/block style, tags (expanded `%TAG` handles become
  verbatim tags) and anchors are kept. yaml.v3 may shift a foot comment by one indentation level.
  Edits only change comments where needed: deleting a comment through the callback is not supported
  (the old text is restored). Relink never does this.
- **Shared Marshal corrections.** Folded→literal writes the actual value (yaml.v3 falls back to
  double quotes when block style cannot hold it, e.g. trailing spaces); Tab strings double-quoted;
  collection-key line comments moved above the pair. Mihomo reads the generated config, not this
  text directly; the generator reads it through `yamlx.Parse` (yaml.v3). `FuzzNativeRoundTrip`
  covers Parse→Marshal→Parse equality.
- **Relink boundary.** `subs/difftest/edit_test.go` (now 21 cases) compares the real Python public
  function with Go through a repository transaction and test HTTP shell: status, body, every group
  field, YAML meaning and the commit fingerprint, including the rollback after a later failure.
- **Large input.** 2.5 MB config, 22k comments, 2001 matched listeners: rename (splice) 0.54 s,
  unpin with one first-key listener (rewrite) 1.35 s on .63.

## Notes for the coordinator (not N7-2 findings)

- `yamlx.Clone` keeps its N7-1 contract (over-budget → `Null`) and now has no runtime caller. A
  future consumer (N7-3) must not use it to copy values of a user document: a subtree of an accepted
  document can exceed the budget. The corpus oracle `listenerCorpusExpected` still uses `Clone`. It
  agrees with the new code on all 12,479 rows, and the review tests use explicit expectations.
- Design §2.2 text was updated for the behaviour above (splice fallback, verification target,
  non-expanding materialisation, removed-pair comment placement, `<<`, multi-document diagnostic,
  empty `!!` tag rejected).
  These are clarifications made in this review; please confirm.

## Commands (final, .32)

Recorded in `progress/n7-2.md` under "Opus review".
