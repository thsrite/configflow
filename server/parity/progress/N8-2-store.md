# N8-2 — store on raw rows

Status: in progress; author branch `go-n8-2`.

Scope: implement `design/n7-n8.md` §2.4 exactly; unchanged SQLite schema,
locking, error kinds and Chinese messages. Store owns opaque JSON rows;
repo temporarily bridges raw rows to its existing jsonx document. No model
or repo API S pass (N8-3/N8-5). D2 uses Go encoding, no canonjson.

Done: added the designed Field/Section/Document/ProfileRows/Meta types.

Next: switch storage and repo boundary together; preserve unchanged raw bytes;
replace digest comparisons with actual row comparisons; verify Python↔Go
interchange on committed fixtures and a disposable private snapshot copy.
Final .32: gofmt, vet, changed-package race, full tests, proddiff new=0,
fixture and private-copy interchange; rebase go, clean, no push.

Commands: `RT_HOST=sunbroke@192.168.0.63 rt -- bash -lc 'cd server && go vet ./internal/store ./internal/repo && go test -count=1 ./internal/store ./internal/repo'`.
