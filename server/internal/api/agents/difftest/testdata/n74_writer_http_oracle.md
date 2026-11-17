# N7-4 explicit HTTP writer differences

The deterministic installer HTTP fuzz (PCG seed 20261116,8) contains four
Compose requests whose bind sources contain U+0085. All other existing HTTP
cases keep the original strict non-YAML comparison. Raw Compose and guide/ZIP
YAML compare Parse/Equal, including scalar tags, with status and headers strict.

`n74_writer_http_inputs.json` records the four original request URIs, the existing
Go `nativeQueryVariant` boundary URI, and real authenticated Flask response
status/content-type/SHA256. These inputs were captured by the existing Go
Flask differential helpers (seed/startPython/login/sendInstaller), not from a Go
business generator. The provenance test independently checks the URI boundary.

Generator: [n74_writer_http_oracle.py](n74_writer_http_oracle.py). On the test
server, from repository root:

```sh
(cd server && go build -o /tmp/n74-yamlwriter ./tools/yamlwriteroracle)
~/cf-test/.venv/bin/python server/internal/api/agents/difftest/testdata/n74_writer_http_oracle.py /tmp/n74-yamlwriter
```

The generator registers the real agents blueprint in a Flask test app with a
temporary DATA_DIR. Its standard auth configuration is disabled for recording;
no function other than yaml.dump/safe_dump is patched, and the original route
output must reproduce the previously authenticated response digest. It shares
the installer writer-only recording/injection implementation and graph codec.
Each actual typed input graph is recorded before native Marshal and the injected
bytes before returning to unchanged Python route code. The response status and
all headers except the layout-dependent Content-Length must stay identical.

`n74_writer_http_expected.json` stores the complete native response text, graph,
source digest, call and per-field classification for every URI. Source rows 0–3
have respectively 1/2/2/2 changed fields and restored occurrences, all NEL;
4 rows,7 fields,7 occurrences,unknown=0. This is the separately counted HTTP
extension of the approved Python Unicode writer bug fix. No other Unicode or
value normalization occurs. The source fixture's SHA is also recorded.

At replay, the expected value is used only for the exact recorded URI and only
when the live real Flask response reproduces the recorded SHA/status/type.
The original invalid-UTF8 query alternative is still evaluated separately. The
Go response then compares against the complete recorded native expectation by
Parse/Equal. The HTTP fuzz explicitly requires exactly four successful named
expectations. Provenance tests re-render every graph and verify all changes are
NEL preservation. Old corpora and old responses remain unchanged.
