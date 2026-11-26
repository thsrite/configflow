# N7-3d native diagnostic interface reference

`n73_diagnostic_baseline.json.gz` freezes the actual provider→A11 public error
strings at **b6fe812a**, before the native error interface replaced legacy marks.
It contains 224 real `kind=parse` sources from unchanged `corpus.json.gz`, plus
11 explicit position boundary inputs in `n73_diagnostic_inputs.json`.
235 cases,193 refusals,147 actual source-position displays.

The control inputs cover block/flow, CJK+BOM+CRLF, merges and shared aliases,
syntax errors, unknown-position escape/directive/depth errors, root shapes,
duplicates and secret continuations. The SHA256 of both source files is saved.
The complete visible error strings include owner/file/source labels, native
parser details, line/column displays and redacted excerpts. No substring or
position normalization is used; the new hook must reproduce each string exactly.
The comparison records **0 position changes,unknown=0**. Ordinary provider and
A11/A12 recorded corpora also remain unchanged; A13 diagnostic tests are rerun.

Recorder: `server/tools/yamldiagnosticoracle/main.go`. It calls the real
`provider.ParseProviderProxies` with the real `deploy.Locations` hook in a detached
worktree at b6fe812a. Only the recording tool and synthetic controls are added to
that worktree; no product source is copied or changed. This records the old Go
public diagnostic API, not replacement loader values or Python expectations.
No private error type or old implementation is needed in the recording tool.

Executed remotely from that detached worktree:

```sh
# assert the frozen revision locally before rt (rt excludes .git)
git rev-parse --short=8 HEAD  # b6fe812a
RT_HOST=sunbroke@192.168.0.64 rt -e GOMAXPROCS=4 --back server/internal/provider/testdata/n73_diagnostic_baseline.json.gz -- bash -lc 'set -e; cd server; gofmt -w tools/yamldiagnosticoracle/main.go; go run ./tools/yamldiagnosticoracle -base b6fe812a > /tmp/n73-diagnostic-baseline.json; ~/cf-test/.venv/bin/python -c '\''import gzip,json; p="internal/provider/testdata/n73_diagnostic_baseline.json.gz"; d=open("/tmp/n73-diagnostic-baseline.json","rb").read(); gzip.open(p,"wb").write(d); print("sources",len(json.loads(d)["cases"]))'\'''
```

Output: `sources235`. The baseline reader/source position implementation was
also inspected on that pinned remote copy before accepting the artifact.
Only the resulting fixture is copied to the active task. The current-tree
`TestN73NativeDiagnosticInterface` validates the base label and source hashes,
replays all cases with the real native hook and compares the entire result.
Existing provider fake/recorded collaborators retain their historical wire
encoding solely in tests; runtime hooks receive ordinary native errors and
one-based coordinates, with no synthetic legacy exception object.
