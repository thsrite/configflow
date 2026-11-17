# N8-3 — model skeleton and byte-preserving record codec

Status: in progress, branch `go-n8-3`; follows design/n7-n8.md §2.3 and R1–R9.
No push. Tests/build/vet/fuzz only via rt; iterate .63, final .32.

Maintainer clarified in this session (recorded in design “Clarified in N8-3”):
model collects FieldIssue path/category only, no logx/I/O; repo logs once per loaded
revision. Native JSON fragments preserve records/RawMessage/containers; native
Encoder is used for new scalars. Output must be valid, unique-key JSON with order
and unchanged member bytes preserved.

First buildable codec step: model/json.go validated JSON shell parsing/splicing,
native scalar/container Marshal, cycle detection; record.go reflection descriptor,
Raw immutable provenance, R2 typed baselines/R3 presence/R5 issues; Int exact
integral JSON number parsing; Fields and Map[V] ordered COW containers; Dynamic
on yamlx. Codec tests cover byte/no-op/unknown/child edit, types, presence, numeric
bounds, duplicate rejection, map and Dynamic ownership.

Validation: `.63 gofmt -w internal/model; go vet ./internal/model; go test -race -count=1 ./internal/model` PASS (`ok configflow/internal/model 1.013s`).

Next: skeleton
Document/Profile/Shared/System (complex fields RawMessage), repo integration,
structured issue logging; corpus + private snapshot boundary round-trip harness;
fuzz + final .32 gates; record invariants/results; git rebase go; clean/no push.

Important implementation notes: no any in record fields; generic V any and the
specified codec/Marshal entry points are generic plumbing only. Raw is immutable.
Fields/Map mutators copy their state. Type errors zero the typed field but retain
its original JSON; syntax/duplicate/record-shape failures remain errors. stdlib
json.Unmarshal removes outer JSON whitespace before calling UnmarshalJSON, so the
record API tests outer whitespace via DecodeRecord directly; repo's row adapter
must preserve each original row's outer whitespace explicitly.
