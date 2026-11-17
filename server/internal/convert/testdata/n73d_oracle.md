# N7-3d Mihomo reader migration reference

Run only on the test server, from repository root:

```sh
(cd server && go build -o /tmp/n73d-yamlreader ./tools/yamlreaderoracle && go build -o /tmp/n73d-yamlwriter ./tools/yamlwriteroracle)
PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/convert/testdata/n73d_oracle.py /tmp/n73d-yamlreader /tmp/n73d-yamlwriter
```

The unchanged Python business functions run in gen_mihomo's original synthetic
collaborator environment. No new business/collaborator patches are installed.
Only yaml.load/safe_load and yaml.dump/safe_dump are replaced. Every old answer
is first reproduced exactly. The writer-only stage is the approved N7-4 oracle;
the combined stage injects native loader values. Actual physical values/raw Parse
tags are journaled before injection, with the same production ProjectJSONKeys
and yamlbridge.Value transitions. Shared containers are reconstructed by identity.
Writer input typed graphs are saved separately before native Marshal renders them.
Old golden files remain unchanged; rows not listed still use their original
strict comparison (including every mihomo_recorded row).

| Caller corpus | Checked reader-relevant rows | Expected changes | Classified |
|---|---:|---:|---:|
| mihomo_corpus |7487|276|276|
| review_mihomo |474|2|2|

3,837 actual loader sources, all convert-document mode; unknown=0.

| Class | Changed rows | Mihomo consumer consequence |
|---|---:|---|
| YAML1.1/timestamp |182|Native scalar resolution: octal/exponent spellings use native numbers; YAML1.1 words remain strings, so boolean field validators reject them. Timestamp text only occurs in extra metadata `.date`/`.stamp`; no numeric/time schema field is affected.|
| native-diagnostics |34|Same custom YAML rejection/context, native Chinese reason and position.|
| nonfinite-scalar-text |31|`.number` metadata retains original `.inf` text as a string; no numeric schema field is affected.|
| tagged-collections |31|`.bin` metadata becomes original base64 text rather than a binary-tag value; no schema field is affected.|

The pre-switch input audit saw7,128 custom_config occurrences/3,616 unique texts:
timestamp53 at `.date`,binary44 at `.bin`,nonfinite57 at `.number`; none occur in
Mihomo numeric/time schema fields. Some inputs reject before generation or lose
unused custom metadata through unchanged business processing, so occurrence and
changed-output counts intentionally differ.

All34 native diagnostic rows are listed in n73d_diagnostic_positions.json.
32 change the primary line (PyYAML's final problem mark versus native parser's
reported line); all34 drop the old column, which yaml.v3 does not report here.
This is the separately audited design5.7「诊断位置」subclassification. Complete
messages still compare exactly against the real loader-only Python outcome.

Tests verify source hashes/modes, raw physical Parse audit, the complete injected
value graph, every old fn/args/result, and fixed changed=classified counts. No
broad value normalization or arbitrary output override is used. Convert's JSON
value boundary is temporary; N8-8 replaces it with a typed model.
