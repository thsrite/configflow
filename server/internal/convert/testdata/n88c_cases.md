# N8-8(c) acceptance data

All row numbers are zero-based; original corpora, Python goldens and
`n88_generation_bytes.json` are unchanged.

- `n88c_byte_exceptions.json`: 107 approved byte changes. Original Python output
  equals the pre-migration Go SHA for every one of these rows. Each explicit
  patch records its original line offset, exact old/new lines, class and native
  SHA. Tests apply only those frozen line patches, check every old line, and
  compare the complete result byte for byte. There is no content-wide replace
  or normalization. The raw source also independently confirms the114 modified
  health occurrences are null or absent in the followed group.
- `n88c_helper_classifications.json.gz`: one classification per malformed or
  expressly changed input in the34697-row helper corpus. The fixture stores
  source/input hashes, source row, target, production decoder field diagnosis
  and class family. It contains no captured Go output. Normal rows retain the
  strict real-Python oracle. Non-object nodes are rejected at record decoding;
  all representable rows execute the public codec/native boundary.
- `n88c_review_classifications.json.gz`: the same input-only audit for the306
  additional real-Python boundary cases. The128-call recorded suite is entirely
  strict and has no classification fixture.

The coordinator approved the Leaner helper rule: explicit production-decoder
classification plus representative native assertions for each family. The
separate4548-row gate still requires exact counts, unique sources, source SHAs,
old SHA or exact approved native expectation, and typed/legacy byte equality.

The input indexes were recorded on .32 with:

```sh
CONFIGFLOW_RECORD_CLIENT_INDEX=1 go test -count=1 -v ./internal/convert \
  -run '^(TestSurgeLoonCorpus|TestClientReviewBoundaryReferences)$'
```

That explicit audit mode records input classifications only; it never records
expected results. Ordinary test runs are read-only and require exact fixture
membership/field/family/input SHA. An unclassified difference is a failure.

`pending shared integer-string` is an unresolved8a dependency, **not an accepted
R5 output exception**. After the shared model decoder lands, remove those pending
rows from the index (or reclassify a different malformed field), enable the
positive shared-entry checks, and require normal quoted port/interval values to
match the old output. Numeric Name/Server source rows pending the same shared
repair are listed in `parity/progress/n8-8c.md`.
