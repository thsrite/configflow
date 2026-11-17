# N7-4 installer writer oracle

Generator: [n74_writer_oracle.py](n74_writer_oracle.py). Sources are the unchanged
`docker_ha*.jsonl.gz`, `matrix.jsonl`, and `fuzz.jsonl.gz` beside this file; exact
source SHA256 and counts are in [n74_writer_manifest.json](n74_writer_manifest.json).
Only successful writer calls are selected. Rows already excluded by the original
replay because they patch Python internals remain excluded; no new business hook
is introduced.

```sh
# repository root, test server; Python is the shared Python 3.11 environment
(cd server && go build -o /tmp/n74-yamlwriter ./tools/yamlwriteroracle)
~/cf-test/.venv/bin/python server/internal/installer/testdata/n74_writer_oracle.py /tmp/n74-yamlwriter
```

The first pass invokes real Python business code with the original writers and
verifies every legacy output or SHA/length (including hash-only Compose cases).
The second pass changes only `yaml.dump`/`yaml.safe_dump`. Their actual typed
inputs are recorded using the shared `yamlx/testdata/writer_graph.py` codec,
rendered by the Go tool through yamlbridge.Encode/Marshal, and journaled before
being passed to unchanged Python postprocessing. Flow styles and image sharing
are explicit graph properties. Later input changes caused by an intermediate
reread are separately recorded and classified; no Go business function generates
an expectation.

`n74_writer_graphs.json.gz` maps a typed graph SHA256 to its complete physical
node graph and native YAML bytes (12,870 graphs). `n74_writer_expected.json.gz`
maps the canonical `{fn,args}` SHA256 to source file/zero-based row/digest,
legacy/native writer call graph IDs, input changes, and every output change path
and category. It is the per-row classification listing: each source row is kept,
including duplicate calls. There are 8,368 unique changed calls. All 1,635
semantic-change or hash-only Compose cases carry complete native Python results.
Layout-only multi-field cases reference the unchanged complete legacy semantic
expectation and retain the native result digest; scripts and metadata remain
strict, YAML compares by Parse/Equal. Old golden files are never rewritten.

| Source | Verified rows |
|---|---:|
| docker_ha.jsonl.gz | 608 |
| docker_ha_compose.jsonl.gz | 3,402 |
| docker_ha_compose_recorded.jsonl.gz | 115 |
| docker_ha_compose_review.jsonl.gz | 1,334 |
| docker_ha_recorded.jsonl.gz | 137 |
| docker_ha_review.jsonl.gz | 1,840 |
| fuzz.jsonl.gz | 1,104 |
| matrix.jsonl | 83 |
| Total | 8,623 |

| Classification | Source rows |
|---|---:|
| Layout only | 8,127 |
| Numeric-looking image strings retain string type | 21 |
| Unicode line separator preservation | 436 |
| Unchanged output | 39 |
| Unknown | 0 |

Unicode restoration is counted separately, without normalizing either output.
Rows can contain more than one character. Field occurrences include unchanged
Python's derived Docker command arguments after rereading a corrected path.

| Character | Source rows | Changed fields | Restored occurrences |
|---|---:|---:|---:|
| U+0085 (NEL) | 345 | 1,856 | 2,020 |
| U+2028 (LS) | 53 | 255 | 285 |
| U+2029 (PS) | 47 | 213 | 231 |

Python's raw emission followed by splitlines/annotation can turn these characters
into line breaks or spaces. Native quoted escapes preserve their actual values
when Docker Compose's yaml.v3 reader loads them. Dedicated regressions cover all
three characters in data_dir through compose, direct and compact generation;
all parse back to exactly the original bind source. Numeric-looking image strings
also retain their string tag and shared anchor. Unrelated shape/value/sequence
changes fail classification, and complete new expected responses still compare
strictly outside YAML.

`TestWriterOracleProvenance` verifies original source digests, case and graph
identities, every typed graph's reproducible native bytes, source/call coverage,
and complete expected result digests. A semantic change cannot fall back to a
layout-only legacy expectation. The original core and Mihomo oracle fixtures
remain separate and unchanged.
