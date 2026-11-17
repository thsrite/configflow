package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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
	if _, changed, err := s.Commit(loaded, loaded.Document, nil); err != nil || changed {
		t.Fatalf("unchanged commit: %v %v", changed, err)
	}
	if must(s.Fingerprint())(t) != stamp {
		t.Fatal("unchanged commit wrote rows")
	}
	rows := must(documentRows(loaded.Document))(t)
	proposed := must(buildDocument(rows.rootOrder, rows.sections, rows.fields))(t)
	original := must(json.Marshal(loaded.Document))(t)
	proposed.Profiles[0].Fields[1].Value = json.RawMessage(`"renamed"`)
	updated, changed, err := s.Commit(loaded, proposed, nil)
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
			if _, _, err := s.Commit(base, doc, nil); !errors.Is(err, ErrDocument) {
				t.Fatalf("expected ErrDocument, got %v", err)
			}
			if must(s.Fingerprint())(t) != stamp {
				t.Fatal("invalid input wrote rows")
			}
		})
	}
	s := New(filepath.Join(t.TempDir(), Filename), seconds(2))
	base := must(s.Create(rawFixture(t), nil))(t)
	if _, _, err := s.Commit(base, base.Document, map[string]json.RawMessage{"bad": json.RawMessage(`NaN`)}); !errors.Is(err, ErrDocument) {
		t.Fatal(err)
	}
}

// TestStoredRoundTrip never emits documents or SQLite values, even on failure.
// The private source is opened read-only by Python's SQLite backup API. Only
// disposable copies are modified, and t.TempDir removes them on success/failure.
func TestStoredRoundTrip(t *testing.T) {
	interpreter, root := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || root == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are required for live Python interchange")
	}
	python := func(t *testing.T, args ...string) {
		t.Helper()
		cmd := exec.Command(interpreter, append([]string{"testdata/raw_crosscheck.py"}, args...)...)
		cmd.Env = append(os.Environ(), "CONFIGFLOW_REPO="+root, "PYTHONDONTWRITEBYTECODE=1")
		// Suppress interpreter diagnostics: they can include private row contents.
		if err := cmd.Run(); err != nil {
			t.Fatalf("Python storage boundary failed (%T)", err)
		}
	}
	run := func(t *testing.T, path string) {
		s := New(path, func() time.Duration { return 5 * time.Second })
		if err := s.Check(); err != nil {
			t.Fatal("store check failed")
		}
		base, err := s.Load()
		if err != nil {
			t.Fatal("store load failed")
		}
		before := fieldRows(t, path)
		stamp, err := s.Fingerprint()
		if err != nil {
			t.Fatal("fingerprint failed")
		}
		if _, changed, err := s.Commit(base, base.Document, nil); err != nil || changed {
			t.Fatal("no-op commit changed database")
		}
		now, err := s.Fingerprint()
		if err != nil || now != stamp {
			t.Fatal("no-op fingerprint changed")
		}
		// Marshal only an opaque row envelope to a private temporary expectation.
		expected := filepath.Join(t.TempDir(), "expected.json")
		data, err := json.Marshal(base.Document)
		if err != nil {
			t.Fatal("expectation encode failed")
		}
		if err := os.WriteFile(expected, data, 0o600); err != nil {
			t.Fatal("expectation write failed")
		}
		python(t, "audit", path, expected)
		rows := must(documentRows(base.Document))(t)
		doc, err := buildDocument(rows.rootOrder, rows.sections, rows.fields)
		if err != nil {
			t.Fatal("copy rows failed")
		}
		doc.Order = append(doc.Order, "n8_2_go_probe")
		doc.Root = append(doc.Root, Field{Name: "n8_2_go_probe", Value: json.RawMessage(`{"unknown":{"z":1.0,"a":"例 <>&"},"bad_type":"kept"}`)})
		updated, changed, err := s.Commit(base, doc, nil)
		if err != nil || !changed {
			t.Fatal("Go commit failed")
		}
		after := fieldRows(t, path)
		for key, value := range before {
			if after[key] != value {
				t.Fatal("Go commit changed an unrelated row")
			}
		}
		if len(after) != len(before)+1 {
			t.Fatal("Go commit added unexpected rows")
		}
		data, err = json.Marshal(updated.Document)
		if err != nil {
			t.Fatal("expectation encode failed")
		}
		if err := os.WriteFile(expected, data, 0o600); err != nil {
			t.Fatal("expectation write failed")
		}
		python(t, "audit-edit", path, expected)
		final, err := s.Load()
		if err != nil || final.Head.Generation != updated.Head.Generation+1 {
			t.Fatal("Go cannot read Python commit")
		}
		parsed := false
		for _, field := range final.Document.Root {
			if field.Name == "n8_2_python_probe" {
				parsed = bytes.Equal(field.Value, []byte(`{"written":"Python","number":1.0}`))
			}
		}
		if !parsed {
			t.Fatal("Python edit missing or changed")
		}
		python(t, "audit-final", path, expected)
		t.Logf("rows=%d: no-op unchanged; Go→Python→Go→Python passed; unrelated rows preserved", len(before))
	}
	t.Run("committed-go-fixture", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), Filename)
		s := New(path, seconds(2))
		if _, err := s.Create(rawFixture(t), nil); err != nil {
			t.Fatal(err)
		}
		run(t, path)
	})
	t.Run("committed-python-fixture", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), Filename)
		python(t, "fixture", path, "testdata/raw_rows.json")
		run(t, path)
	})
	if snapshot := os.Getenv("CONFIGFLOW_SNAPSHOT"); snapshot != "" {
		t.Run("private-snapshot", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), Filename)
			python(t, "copy", filepath.Join(snapshot, Filename), path)
			run(t, path)
		})
	}
}
