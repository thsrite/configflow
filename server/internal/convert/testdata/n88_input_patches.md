# Typed node input immutability

`n88_input_patches.json` lists exactly 31 frozen Mihomo source rows and JSON
pointer paths. The source archive and N7 loader-only expected archive are pinned
by SHA-256. The old Python golden remains unchanged.

Python's shallow parameter copy added `short-id: ""` to the caller's own
`params.reality-opts`. The typed converter owns a fresh graph and adds that
field only to its output. Each fixture path explicitly expects the original
input's absent field. Every other member of the mutated profile is still
compared strictly, including every sibling of `short-id`.

The independent producer `record_n88_input_patches.go.txt` reads only original
inputs and the frozen Python expected snapshots (including approved N7 loader
expectations). It calls no Go conversion or business function. It identifies
an added empty short-id and verifies all siblings are identical before writing
the row/path index. Malformed rows use the same test-only schema classifiers
as the normal-input corpus comparison.

Regenerate on the test server from `server/`:

```
cp internal/convert/testdata/record_n88_input_patches.go.txt internal/convert/n88_input_patches_record_test.go
trap 'rm -f internal/convert/n88_input_patches_record_test.go' EXIT
go test -count=1 -v ./internal/convert -run '^TestRecordN88InputPatches$'
```

The producer must find exactly 31 rows. A missing/extra path, changed source
archive, changed old side effect, or another changed sibling fails validation.
