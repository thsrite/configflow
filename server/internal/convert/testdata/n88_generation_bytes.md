# N8-8 normal-generation byte baseline

`n88_generation_bytes.json` records SHA-256 of 4548 successful generated outputs
from unmodified Go converter code at `2277ce047d4abb78ffed436442c19b31152ba85d`.
The source archives are synthetic Python test corpora already stored here;
their full hashes and row indices are included. No production data is involved.
Existing Python goldens and semantic oracles are unchanged.

Producer: `generation_bytes_test.go`, `TestN88GenerationBytes`, copied into an
isolated checkout of the named baseline. The test-only `CommonReplayIssue`
classifier is copied there too. The producer uses model.DecodeDocument to
exclude malformed stored types and the explicit common null/type classifier.
Only original successful generate/surge/loon rows are recorded.

Reproduce on that baseline only, on the test server:

```
CONFIGFLOW_RECORD_N88=1 go test -count=1 -run '^TestN88GenerationBytes$' -v ./internal/convert
```

Normal verification does not set the recording variable. It verifies source
hashes, regenerates the exact fixed rows and compares every output hash.
