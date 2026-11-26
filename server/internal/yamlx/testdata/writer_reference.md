# N7-4 native writer semantic corpus

`writer_reference.json.gz` records real Python `yaml.safe_load` of the unchanged
old outputs in `corpus.jsonl`: 1374 `dump` rows (the `safe_u` output) and all 21
`spec` rows. Native Marshal has one canonical format, so the old width/indent/
sort/ASCII/style option matrix no longer defines output bytes. Every value,
resolved scalar tag and sequence order remains significant.

Generator: [gen_writer_reference.py](gen_writer_reference.py). The typed physical
graph codec [writer_graph.py](writer_graph.py) is also used by the Mihomo writer
oracle; it only encodes real Python values, without changing them. It calls no
Go code or business functions. Row and old-output SHA256 are saved per reference.

```sh
# server/ on a test server
~/cf-test/.venv/bin/python internal/yamlx/testdata/gen_writer_reference.py
```

The native writer test independently reconstructs these reference Nodes, marshals
them, parses the output and compares Equal. It verifies every source digest,
rejects duplicate references and checks exact corpus counts. Old writer option/byte tests are replaced in N7-4 by these semantic references;
old loader tests remain until N7-3d.

Core corpus SHA256: `587c768ddfb2750367979ff31c15cb5ad535d87ac0a72c497860c1234dfdf01f`.
