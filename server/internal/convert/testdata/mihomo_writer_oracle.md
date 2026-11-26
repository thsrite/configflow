# N7-4 Mihomo writer oracle

Source: real Python `yaml.safe_load` applied to the **unchanged old Python YAML output**.
Generator: [gen_writer_oracle.py](gen_writer_oracle.py); filter: [tools/yamlwriteroracle](../../../tools/yamlwriteroracle/main.go).
No Go business code is called to generate an expected graph. The filter only compares native parsing of old text against the recorded Python graph.

Run from `server/` on a test server:

```sh
~/cf-test/.venv/bin/python internal/convert/testdata/gen_writer_oracle.py > /tmp/n74-writer-graphs.jsonl
go run ./tools/yamlwriteroracle -in /tmp/n74-writer-graphs.jsonl -out internal/convert/testdata/mihomo_writer_oracle.json.gz
```

5,408 successful recorded generation outputs checked; 111 corrected rows, all classified; unknown=0.
The other outputs continue to compare `Parse(Python output)` and `Parse(Go output)` directly with Equal.
Each reference records its corpus/zero-based row, old-output SHA256 and complete typed physical graph.
Tests verify source digests, require every reference to remain necessary, and reject any unclassified/count drift.

| Category | Rows | Mihomo read before | Mihomo reads now |
|---|---:|---|---|
| string-read-as-number | 56 | `0o17` → integer 15; `1e3` → float 1000 | original string |
| large-int-read-as-float | 22 | oversized plain integer → float, possible precision loss | explicit integer, original precision |
| timestamp-read-as-string | 33 | space-separated zoned timestamp → string | explicit timestamp |

Old corpus file SHA256 (goldens are unchanged):

- `mihomo_corpus.jsonl.gz`: `5e207c0aa54ac716a6b39acb2f363d39bb0be8e77337623c9bb0016e478b3ef7`
- `mihomo_recorded.jsonl.gz`: `b0c05e054db8fff7791505deb0c085195577b82e33c9a9482c7cf2f74545cb73`
- `review_mihomo.jsonl.gz`: `6738d7cbb331d41dd601fb371e9709540c7cc4dc35a314dd90b3db574dc55448`

Per-row classification (zero-based row numbers; repeated fields within a row remain listed):

| Corpus | Row | Category | Field path | Old → intended/native writer tag |
|---|---:|---|---|---|
| mihomo_corpus.jsonl.gz | 444 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 627 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 839 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 872 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1123 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 1335 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 1351 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1354 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1417 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 1545 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1663 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1900 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 1966 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 2014 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2019 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 2192 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 2198 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 2259 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2276 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2291 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2312 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2318 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2362 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 2684 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 2847 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 2861 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 2883 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 2934 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3005 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3040 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 3046 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3146 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 3181 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 3192 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3318 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 3319 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3333 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3336 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3357 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 3408 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3414 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3502 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3506 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3507 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 3679 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3684 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3689 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 3764 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3791 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 3880 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 3952 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4005 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 4010 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 4011 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4017 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4098 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4185 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4191 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 4209 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4225 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 4274 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4300 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4302 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 4399 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4438 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 4503 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4699 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4772 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4778 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4867 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 4894 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 4956 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 5045 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5066 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 5078 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 5194 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 5209 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5332 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5402 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 5479 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5522 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5627 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 5666 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 5840 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5865 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5931 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 5952 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6073 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6096 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6112 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 6217 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 6237 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 6242 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6465 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 6560 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 6678 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 6808 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 6812 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6817 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6834 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 6942 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 7004 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 7196 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 7201 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 7241 | string-read-as-number | `.text` | `!!float` → `!!str` |
| mihomo_corpus.jsonl.gz | 7440 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 7468 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 7562 | string-read-as-number | `.mixed-port` | `!!int` → `!!str` |
| mihomo_corpus.jsonl.gz | 7682 | timestamp-read-as-string | `.stamp` | `!!str` → `!!timestamp` |
| mihomo_corpus.jsonl.gz | 7693 | large-int-read-as-float | `.number` | `!!float` → `!!int` |
| mihomo_corpus.jsonl.gz | 7735 | string-read-as-number | `.text` | `!!float` → `!!str` |
