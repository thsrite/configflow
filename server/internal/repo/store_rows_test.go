package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/store"
)

func TestRawStoreBridgePreservesUntouchedFields(t *testing.T) {
	r := newRepo(t)
	base := must(r.store.Load())(t)
	raw := base.Document
	opaque := json.RawMessage(`{ "z": 6e1, "a": "\u4f8b <>&", "future": {"b":1.0,"a":null} }`)
	raw.Order = append(raw.Order, "n8_unknown")
	raw.Root = append(raw.Root, store.Field{Name: "n8_unknown", Value: opaque})
	for i := range raw.System.Fields {
		if raw.System.Fields[i].Name == "backup" {
			raw.System.Fields[i].Value = opaque
		}
	}
	raw.Shared.Fields = append(raw.Shared.Fields, store.Field{Name: "n8_unknown", Value: opaque})
	raw.Profiles[0].Fields = append(raw.Profiles[0].Fields, store.Field{Name: "n8_unknown", Value: opaque})
	written, changed, err := r.store.Commit(base, raw, nil)
	if err != nil || !changed {
		t.Fatalf("prepare raw rows: %v %v", changed, err)
	}
	r.dropCache()
	noError(t, r.update(bg, writeOptions{}, func(context.Context, *jsonx.Object) error { return nil }))
	if got := must(r.store.Load())(t); got.Head != written.Head {
		t.Fatal("unchanged bridge commit rewrote rows")
	}
	noError(t, r.update(bg, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
		document.GetObject("profiles").GetObject("default").Set("name", "Renamed")
		return nil
	}))
	final := must(r.store.Load())(t)
	var fields []store.Field
	fields = append(fields, final.Document.Root...)
	fields = append(fields, final.Document.System.Fields...)
	fields = append(fields, final.Document.Shared.Fields...)
	fields = append(fields, final.Document.Profiles[0].Fields...)
	count := 0
	for _, field := range fields {
		if field.Name == "n8_unknown" || field.Name == "backup" {
			count++
			if !bytes.Equal(field.Value, opaque) {
				t.Fatal("bridge rewrote an unknown field")
			}
		}
	}
	if count != 4 {
		t.Fatalf("retained %d unknown fields", count)
	}
}
