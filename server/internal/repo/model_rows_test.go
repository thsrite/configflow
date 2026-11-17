package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

func modelRowFixture(t *testing.T) *store.Document {
	t.Helper()
	data := must(os.ReadFile("../store/testdata/raw_rows.json"))(t)
	var doc store.Document
	noError(t, json.Unmarshal(data, &doc))
	// These bytes are outside JSON value tokens, but inside stored row values.
	doc.Root[1].Value = append(append([]byte(" \n"), doc.Root[1].Value...), []byte("\t ")...)
	doc.Profiles[0].Fields[1].Value = json.RawMessage(" \n\"Example\"\t")
	return &doc
}

func assertModelRows(t *testing.T, got, want *store.Document) {
	t.Helper()
	if strings.Join(got.Order, "\x00") != strings.Join(want.Order, "\x00") {
		t.Fatal("root order differs")
	}
	fields := func(a, b []store.Field) {
		t.Helper()
		if len(a) != len(b) {
			t.Fatal("field count differs")
		}
		for i := range a {
			if a[i].Name != b[i].Name || !bytes.Equal(a[i].Value, b[i].Value) {
				t.Fatal("row name/order/bytes differ")
			}
		}
	}
	fields(got.Root, want.Root)
	for _, pair := range [][2]*store.Section{{got.System, want.System}, {got.Shared, want.Shared}} {
		if (pair[0] == nil) != (pair[1] == nil) {
			t.Fatal("section presence differs")
		}
		if pair[0] != nil {
			fields(pair[0].Fields, pair[1].Fields)
		}
	}
	if len(got.Profiles) != len(want.Profiles) {
		t.Fatal("profile count differs")
	}
	for i := range got.Profiles {
		if got.Profiles[i].ID != want.Profiles[i].ID {
			t.Fatal("profile order differs")
		}
		fields(got.Profiles[i].Fields, want.Profiles[i].Fields)
	}
}

func TestModelRowsByteRoundTripAndTypedEdits(t *testing.T) {
	original := modelRowFixture(t)
	document, issues, err := decodeModelRows(original)
	noError(t, err)
	if len(issues) != 0 {
		t.Fatalf("unexpected issues=%v", issues)
	}
	encoded := must(encodeModelRows(document, original))(t)
	assertModelRows(t, encoded, original)
	target := store.New(filepath.Join(t.TempDir(), store.Filename), LockTimeout)
	base := must(target.Create(original, nil))(t)
	before := must(target.Fingerprint())(t)
	next, changed, err := target.Commit(base, encoded, nil)
	if err != nil || changed {
		t.Fatalf("no-op commit: %v %v", changed, err)
	}
	if must(target.Fingerprint())(t) != before {
		t.Fatal("no-op wrote rows")
	}
	document.Profiles[0].Name = "Renamed"
	encoded = must(encodeModelRows(document, original))(t)
	oldName := original.Profiles[0].Fields[1].Value
	original.Profiles[0].Fields[1].Value = json.RawMessage(`"Renamed"`)
	assertModelRows(t, encoded, original)
	original.Profiles[0].Fields[1].Value = oldName
	next, changed, err = target.Commit(base, encoded, nil)
	if err != nil || !changed || next.Head.Generation != base.Head.Generation+1 {
		t.Fatalf("typed rename: %v %v", changed, err)
	}
	if document.Profiles[0].Name != "Renamed" {
		t.Fatal("encode mutated caller")
	}
}

// This is an explicit transition limitation. N8-5 must split stored-data load
// validation from write/business validation and update these locked expectations.
func TestModelR5LegacyLoadValidationRemainsUntilN85(t *testing.T) {
	tests := []struct {
		name, field, text string
		wantIssues        bool
		typedRevision     model.Int
		message           string
	}{
		{"string revision", "_revision", `"60"`, true, 0, "配置修订号无效"},
		{"integral float revision", "_revision", `60.0`, false, 60, "配置修订号无效"},
		{"fractional revision", "_revision", `60.5`, true, 0, "配置修订号无效"},
		{"numeric name", "name", `7`, true, 0, "配置名称不能为空"},
		{"empty name", "name", `""`, false, 0, "配置名称不能为空"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			base := must(r.store.Load())(t)
			legacy := must(decodeRows(base.Document))(t)
			legacy.GetObject("profiles").GetObject("default").Set(tt.field, must(jsonx.ParseString(tt.text))(t))
			rows := must(encodeRows(legacy, base.Document))(t)
			entry, changed, err := r.store.Commit(base, rows, nil)
			if err != nil || !changed {
				t.Fatalf("fixture commit: %v %v", changed, err)
			}
			typed, issues, err := decodeModelRows(entry.Document)
			noError(t, err)
			if (len(issues) > 0) != tt.wantIssues {
				t.Fatalf("issues=%v", issues)
			}
			if tt.field == "_revision" && typed.Profiles[0].Revision != tt.typedRevision {
				t.Fatal("wrong typed revision fallback")
			}
			encoded := must(encodeModelRows(typed, entry.Document))(t)
			assertModelRows(t, encoded, entry.Document)
			_, err = Open(r.DataDir(), Options{})
			if err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("legacy load restriction changed: %v", err)
			}
		})
	}
}

// The child process gives this logging boundary test its own logx root and
// avoids global logger mutation shared with other repository tests.
func TestModelIssuesLoggedOncePerRevision(t *testing.T) {
	const child = "CONFIGFLOW_MODEL_LOG_CHILD"
	const secret = "synthetic-value-that-must-not-be-logged"
	if os.Getenv(child) == "1" {
		r := newRepo(t)
		r.cacheMaxBytes = 0
		r.dropCache()
		noError(t, r.update(bg, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
			document.GetObject("profiles").GetObject("default").Set("description", jsonx.Obj("password", secret))
			return nil
		}))
		for range 5 {
			_, err := r.GetProfile(bg, "default")
			noError(t, err)
		}
		noError(t, r.update(bg, writeOptions{}, func(_ context.Context, document *jsonx.Object) error {
			document.GetObject("profiles").GetObject("default").Set("name", "next")
			return nil
		}))
		for range 5 {
			_, err := r.GetProfile(bg, "default")
			noError(t, err)
		}
		return
	}
	command := exec.Command(os.Args[0], "-test.run=^TestModelIssuesLoggedOncePerRevision$", "-test.v")
	command.Env = append(os.Environ(), child+"=1")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("logging child: %v\n%s", err, output)
	}
	marker := fmt.Sprintf(msgModelFieldIssue, "profiles[0].description", model.IssueType)
	if bytes.Count(output, []byte(marker)) != 2 {
		t.Fatalf("expected one issue per head:\n%s", output)
	}
	if bytes.Contains(output, []byte(secret)) || bytes.Contains(output, []byte("password")) {
		t.Fatal("stored value leaked into diagnostic")
	}
}
