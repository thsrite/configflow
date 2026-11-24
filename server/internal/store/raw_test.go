package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func rawFixture(t testing.TB) *Document {
	t.Helper()
	data, err := os.ReadFile("testdata/raw_rows.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc Document
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return &doc
}

func TestRawRowsPreserveBytesAndRevisions(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(2))
	entry := must(s.Create(rawFixture(t), nil))(t)
	before := fieldRows(t, s.Path())
	stamp := must(s.Fingerprint())(t)
	loaded := must(s.Load())(t)
	if !reflect.DeepEqual(entry, loaded) {
		t.Fatal("create and load disagree")
	}
	if _, changed, err := s.Commit(t.Context(), loaded, loaded.Document, nil); err != nil || changed {
		t.Fatalf("unchanged commit: %v %v", changed, err)
	}
	if must(s.Fingerprint())(t) != stamp {
		t.Fatal("unchanged commit wrote rows")
	}
	rows := must(documentRows(loaded.Document))(t)
	proposed := must(buildDocument(rows.rootOrder, rows.sections, rows.fields))(t)
	original := must(json.Marshal(loaded.Document))(t)
	proposed.Profiles[0].Fields[1].Value = json.RawMessage(`"renamed"`)
	updated, changed, err := s.Commit(t.Context(), loaded, proposed, nil)
	if err != nil || !changed {
		t.Fatalf("edit: %v %v", changed, err)
	}
	after := fieldRows(t, s.Path())
	touched := 0
	for key, row := range after {
		if row != before[key] {
			touched++
			if key != (fieldKey{"profile", "default", "name"}) || row.revision != updated.Head.Generation {
				t.Fatal("unrelated row changed")
			}
		}
	}
	if touched != 1 {
		t.Fatalf("changed %d rows", touched)
	}
	proposed.Profiles[0].Fields[1].Value[1] = 'X'
	if !bytes.Equal(updated.Document.Profiles[0].Fields[1].Value, []byte(`"renamed"`)) {
		t.Fatal("entry aliases caller bytes")
	}
	if !bytes.Equal(original, must(json.Marshal(loaded.Document))(t)) {
		t.Fatal("base entry mutated")
	}
}

func TestRawRowsRefuseInvalidEnvelopesBeforeWriting(t *testing.T) {
	tests := []struct {
		name string
		edit func(*Document)
	}{
		{"invalid UTF-8 value", func(d *Document) { d.Root[0].Value = json.RawMessage{'"', 255, '"'} }},
		{"invalid UTF-8 field", func(d *Document) { d.Root[0].Name = string([]byte{255}) }},
		{"invalid UTF-8 owner", func(d *Document) { d.Profiles[0].ID = string([]byte{255}) }},
		{"invalid UTF-8 root", func(d *Document) { d.Order[0] = string([]byte{255}) }},
		{"invalid field", func(d *Document) { d.Root[0].Value = json.RawMessage(`NaN`) }},
		{"invalid nested JSON", func(d *Document) { d.Shared.Fields[0].Value = json.RawMessage(`{"x":Infinity}`) }},
		{"duplicate field", func(d *Document) { d.Root = append(d.Root, d.Root[0]) }},
		{"duplicate root", func(d *Document) { d.Order = append(d.Order, d.Order[0]) }},
		{"missing root", func(d *Document) { d.Root = nil }},
		{"missing section", func(d *Document) { d.System = nil }},
		{"extra section", func(d *Document) { d.Order = d.Order[1:] }},
		{"empty profile id", func(d *Document) { d.Profiles[0].ID = "" }},
		{"duplicate profile", func(d *Document) { d.Profiles = append(d.Profiles, d.Profiles[0]) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(filepath.Join(t.TempDir(), Filename), seconds(2))
			base := must(s.Create(rawFixture(t), nil))(t)
			stamp := must(s.Fingerprint())(t)
			doc := rawFixture(t)
			tt.edit(doc)
			if _, _, err := s.Commit(t.Context(), base, doc, nil); !errors.Is(err, ErrDocument) {
				t.Fatalf("expected ErrDocument, got %v", err)
			}
			if must(s.Fingerprint())(t) != stamp {
				t.Fatal("invalid input wrote rows")
			}
		})
	}
	s := New(filepath.Join(t.TempDir(), Filename), seconds(2))
	base := must(s.Create(rawFixture(t), nil))(t)
	for _, meta := range []map[string]json.RawMessage{{"bad": json.RawMessage(`NaN`)}, {"bad": json.RawMessage{'"', 255, '"'}}, {string([]byte{255}): json.RawMessage(`null`)}} {
		if _, _, err := s.Commit(t.Context(), base, base.Document, meta); !errors.Is(err, ErrDocument) {
			t.Fatal(err)
		}
	}
}

func TestOpaqueMetadata(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), Filename), seconds(2))
	opaque := json.RawMessage(`{ "z": 1.0, "a": "\u4f8b <>&" }`)
	base := must(s.Create(rawFixture(t), map[string]json.RawMessage{"future": opaque}))(t)
	if got := must(s.Meta("future"))(t); !bytes.Equal(got, opaque) {
		t.Fatal("metadata was re-encoded")
	}
	if missing := must(s.Meta("missing"))(t); missing != nil {
		t.Fatal("missing metadata is present")
	}
	if got := s.SalvageMeta()["future"]; !bytes.Equal(got, opaque) {
		t.Fatal("salvage decoded metadata")
	}
	changed := json.RawMessage(`{"next":true}`)
	next, committed, err := s.Commit(t.Context(), base, base.Document, map[string]json.RawMessage{"future": changed})
	if err != nil || !committed || next.Head != base.Head {
		t.Fatal("metadata-only commit changed document head")
	}
	if got := must(s.Meta("future"))(t); !bytes.Equal(got, changed) {
		t.Fatal("metadata-only write missing")
	}
}
