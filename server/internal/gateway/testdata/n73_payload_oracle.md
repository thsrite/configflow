# N7-3d A19 loader-only oracle

Coordinator-approved changes are limited to four rows of unchanged
`a19_corpus.jsonl.gz`:111/130/209/210 (zero-based). Row111 refuses a recursive
alias in Parse; row130 reads a YAML1.1 sexagesimal-looking scalar as a string;
rows209/210 accept the same native-valid Tab-separated YAML with different
business behaviors. Three unique actual loader sources produce four changed
business expectations. Changed=4/classified=4/unknown=0.

[n73_payload_oracle.py](n73_payload_oracle.py) first calls the real Python
`extract_gateway_cidrs` and strictly reproduces every old recorded output/error.
Only yaml.load/safe_load are wrapped to record actual source arguments. The Go
reader recorder calls Parse, saves the complete physical tag/position/alias audit
before conversion, and records native values or an explicit refusal. It never
calls a Go gateway/business function. The Python replay replaces only those YAML
loaders using the already saved native fixture, then calls the unchanged Python
business entry again to obtain the complete new expected result.

```sh
# repository root on the test server
PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/gateway/testdata/n73_payload_oracle.py collect /tmp/n73d-gateway-collected.json
(cd server && go run ./internal/yamlx/testdata/readers /tmp/n73d-gateway-collected.json /tmp/n73d-gateway-native.json)
PYTHONPATH=. ~/cf-test/.venv/bin/python server/internal/gateway/testdata/n73_payload_oracle.py replay /tmp/n73d-gateway-collected.json /tmp/n73d-gateway-native.json server/internal/gateway/testdata/n73_payload_expected.json.gz server/internal/gateway/testdata/n73_payload_native.json.gz
```

Output: original rows4 verified,actual loader sources3;
`alias-recursion:1 implicit-schema:1 native-parser-tab:1` (unique sources);
changed4/classified4/unknown0 (source row occurrences).

The gzip fixtures retain each source/caller mode, original Parse tags and
positions, old API diagnostic, native outcome, source IDs and classification,
both Python business results and the old corpus SHA256. At replay, only an exact
row/fn/argument/original-result match can use the new expectation. All7,490 other
rows keep the original golden. There is no output normalization or omission.
The shared reader-fixture gate checks source hashes, native Parse audits and
refusals, and changed=classified. All7,494 A19 corpus rows pass after injection.
