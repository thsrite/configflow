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

func setStoreField(fields *[]store.Field, name string, value json.RawMessage) {
	for i := range *fields {
		if (*fields)[i].Name == name {
			(*fields)[i].Value = value
			return
		}
	}
	*fields = append(*fields, store.Field{Name: name, Value: value})
}

func cloneStoreDoc(t *testing.T, doc *store.Document) *store.Document {
	t.Helper()
	data := must(json.Marshal(doc))(t)
	var out store.Document
	noError(t, json.Unmarshal(data, &out))
	return &out
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

// N8-5 split stored-data load validation from write validation (design n7-n8,
// N8-5 prerequisite note). A stored value that fails the business rules no
// longer prevents loading: the typed model reads it under R5, the stored bytes
// survive reads and unrelated rows, the write that repairs it passes, and other
// writes are refused with the validation message until then.
func TestModelR5StoredValuesLoadWriteValidationRepairs(t *testing.T) {
	noopProfile := func(context.Context, *model.Profile) error { return nil }
	noopShared := func(context.Context, *model.Shared) error { return nil }
	fixed := "fixed"
	tests := []struct {
		name, field, text string
		wantIssues        bool
		typedRevision     model.Int
		typedName         string
		// unrelated is the error of a write elsewhere ("" = it commits).
		unrelated string
		repair    func(r *Repository) error
		// repaired is the stored value after the repairing write.
		repaired string
	}{
		{"string revision", "_revision", `"60"`, false, 60, "默认配置", "配置修订号无效",
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `61`},
		{"integral float revision", "_revision", `60.0`, false, 60, "默认配置", "",
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `61`},
		// 60.5 is not an integer (R8): it reads as 0, so the repair stores 1.
		{"fractional revision", "_revision", `60.5`, true, 0, "默认配置", "配置修订号无效",
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `1`},
		{"numeric name", "name", `7`, false, 0, "7", "配置名称不能为空",
			func(r *Repository) error {
				_, err := r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &fixed})
				return err
			}, `"fixed"`},
		{"empty name", "name", `""`, false, 0, "", "配置名称不能为空",
			func(r *Repository) error {
				_, err := r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &fixed})
				return err
			}, `"fixed"`},
	}
	storedValue := func(t *testing.T, r *Repository, field string) string {
		t.Helper()
		entry := must(r.store.Load())(t)
		for _, profile := range entry.Document.Profiles {
			for _, row := range profile.Fields {
				if profile.ID == "default" && row.Name == field {
					return string(row.Value)
				}
			}
		}
		t.Fatal("stored field missing")
		return ""
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			base := must(r.store.Load())(t)
			rows := cloneStoreDoc(t, base.Document)
			for i := range rows.Profiles {
				if rows.Profiles[i].ID == "default" {
					setStoreField(&rows.Profiles[i].Fields, tt.field, json.RawMessage(tt.text))
				}
			}
			entry, changed, err := r.store.Commit(base, rows, nil)
			if err != nil || !changed {
				t.Fatalf("fixture commit: %v %v", changed, err)
			}
			typed, issues, err := decodeModelRows(entry.Document)
			noError(t, err)
			if (len(issues) > 0) != tt.wantIssues {
				t.Fatalf("issues=%v", issues)
			}
			encoded := must(encodeModelRows(typed, entry.Document))(t)
			assertModelRows(t, encoded, entry.Document)

			reopened := must(Open(r.DataDir(), Options{}))(t)
			reopened.viewGuard = func() { t.Error("View callback modified the snapshot") }
			profile := must(reopened.Profile(bg, "default"))(t)
			if profile.Revision != tt.typedRevision && tt.field == "_revision" {
				t.Fatalf("typed revision %d", profile.Revision)
			}
			if tt.field == "name" && profile.Name != tt.typedName {
				t.Fatal("wrong typed name fallback")
			}
			err = reopened.UpdateShared(bg, noopShared)
			if tt.unrelated == "" {
				noError(t, err)
			} else if err == nil || !strings.Contains(err.Error(), tt.unrelated) {
				t.Fatalf("unrelated write: %v", err)
			}
			if got := storedValue(t, reopened, tt.field); got != tt.text {
				t.Fatalf("stored value changed by an unrelated write: %s", got)
			}
			noError(t, tt.repair(reopened))
			if got := storedValue(t, reopened, tt.field); got != tt.repaired {
				t.Fatalf("repaired value %s", got)
			}
			noError(t, reopened.UpdateShared(bg, noopShared))
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
		noError(t, r.Update(bg, func(_ context.Context, document *model.Document) error {
			return profileByKey(document, "default").SetJSON("description", json.RawMessage(`{"password":"`+secret+`"}`))
		}))
		for range 5 {
			_, err := r.Profile(bg, "default")
			noError(t, err)
		}
		noError(t, r.Update(bg, func(_ context.Context, document *model.Document) error {
			profileByKey(document, "default").Name = "next"
			return nil
		}))
		for range 5 {
			_, err := r.Profile(bg, "default")
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
