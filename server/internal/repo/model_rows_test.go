package repo

import (
	"bytes"
	"slices"
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
	// The store fixture also has rows no model record declares; the model
	// drops those (TestModelRowsUnknownMembersAndNullSwitch), so they are left
	// out here.
	declared := func(fields []store.Field, names ...string) []store.Field {
		var kept []store.Field
		for _, field := range fields {
			if !slices.Contains(names, field.Name) {
				kept = append(kept, field)
			}
		}
		return kept
	}
	doc.Root = declared(doc.Root, "future")
	doc.Order = slices.DeleteFunc(doc.Order, func(name string) bool { return name == "future" })
	doc.System.Fields = declared(doc.System.Fields, "future")
	doc.Shared.Fields = declared(doc.Shared.Fields, "unknown")
	for i := range doc.Profiles {
		doc.Profiles[i].Fields = declared(doc.Profiles[i].Fields, "future")
	}
	// These bytes are outside JSON value tokens, but inside stored row values.
	doc.Root[0].Value = append(append([]byte(" \n"), doc.Root[0].Value...), []byte("\t ")...)
	doc.Profiles[0].Fields[1].Value = json.RawMessage(" \n\"Example\"\t")
	return &doc
}

// modelRowKey names one stored row.
type modelRowKey struct{ scope, owner, name string }

// assertModelRows requires that got and want hold the same values for every
// declared member (O18 T1a: rows are compared by value; Go writes its own
// encoding, member order and whitespace, so bytes are not compared).
func assertModelRows(t *testing.T, got, want *store.Document) {
	t.Helper()
	a, _, err := decodeModelRows(got)
	noError(t, err)
	b, _, err := decodeModelRows(want)
	noError(t, err)
	wantJSONEqual(t, must(model.Marshal(a))(t), must(model.Marshal(b))(t))
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
	// A structural copy keeps the row bytes exactly (encoding/json would
	// re-escape them).
	fields := func(in []store.Field) []store.Field {
		out := make([]store.Field, len(in))
		for i, field := range in {
			out[i] = store.Field{Name: field.Name, Value: bytes.Clone(field.Value)}
		}
		return out
	}
	out := &store.Document{Order: slices.Clone(doc.Order), Root: fields(doc.Root)}
	if doc.System != nil {
		out.System = &store.Section{Fields: fields(doc.System.Fields)}
	}
	if doc.Shared != nil {
		out.Shared = &store.Section{Fields: fields(doc.Shared.Fields)}
	}
	for _, profile := range doc.Profiles {
		out.Profiles = append(out.Profiles, store.ProfileRows{ID: profile.ID, Fields: fields(profile.Fields)})
	}
	return out
}

func TestModelRowsRoundTripAndTypedEdits(t *testing.T) {
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
	// The first Go write rewrites rows in Go's encoding, with the same values.
	base, _, err = target.Commit(base, encoded, nil)
	noError(t, err)
	assertModelRows(t, base.Document, original)
	document, _, err = decodeModelRows(base.Document)
	noError(t, err)
	encoded = must(encodeModelRows(document, base.Document))(t)
	before := must(target.Fingerprint())(t)
	if _, changed, err := target.Commit(base, encoded, nil); err != nil || changed {
		t.Fatalf("no-op commit: %v %v", changed, err)
	}
	if must(target.Fingerprint())(t) != before {
		t.Fatal("no-op wrote rows")
	}
	document.Profiles[0].Name = "Renamed"
	encoded = must(encodeModelRows(document, base.Document))(t)
	changed := changedModelRowKeys(base.Document, encoded)
	if len(changed) != 1 || !changed[modelRowKey{"profile", base.Document.Profiles[0].ID, "name"}] {
		t.Fatalf("typed rename changed %v", changed)
	}
	next, written, err := target.Commit(base, encoded, nil)
	if err != nil || !written || next.Head.Generation != base.Head.Generation+1 {
		t.Fatalf("typed rename: %v %v", written, err)
	}
	if document.Profiles[0].Name != "Renamed" {
		t.Fatal("encode mutated caller")
	}
}

// Rows a record does not declare are dropped on load with an IssueUnknown and
// deleted by the next write; null *bool members read as false.
func TestModelRowsUnknownMembersAndNullSwitch(t *testing.T) {
	original := modelRowFixture(t)
	original.Root = append(original.Root, store.Field{Name: "n18_root", Value: json.RawMessage(`{"x":1}`)})
	original.Order = append(original.Order, "n18_root")
	original.Shared.Fields = append(original.Shared.Fields, store.Field{Name: "n18_shared", Value: json.RawMessage(`1`)})
	original.Profiles[0].Fields = append(original.Profiles[0].Fields, store.Field{Name: "n18_profile", Value: json.RawMessage(`"secret-value"`)})
	document, issues, err := decodeModelRows(original)
	noError(t, err)
	want := []model.FieldIssue{{Path: "n18_root", Kind: model.IssueUnknown}, {Path: "shared.n18_shared", Kind: model.IssueUnknown}, {Path: "profiles[0].n18_profile", Kind: model.IssueUnknown}}
	if fmt.Sprint(issues) != fmt.Sprint(want) {
		t.Fatalf("issues=%v want %v", issues, want)
	}
	encoded := must(encodeModelRows(document, original))(t)
	changed := changedModelRowKeys(original, encoded)
	for _, key := range []modelRowKey{{"root", "", "n18_root"}, {"shared", "", "n18_shared"}, {"profile", original.Profiles[0].ID, "n18_profile"}} {
		if !changed[key] {
			t.Fatalf("unknown row %v kept", key)
		}
	}
	if slices.Contains(encoded.Order, "n18_root") {
		t.Fatal("unknown root member kept in order")
	}

	type switches struct {
		Enabled *bool  `json:"enabled"`
		Other   *bool  `json:"other"`
		Count   int64  `json:"count"`
		Name    string `json:"name"`
	}
	var value switches
	found, err := model.DecodeStored([]byte(`{"enabled":null,"count":"60","name":7,"unknown":1}`), &value, "x")
	noError(t, err)
	if value.Enabled == nil || *value.Enabled || value.Other != nil || value.Count != 60 || value.Name != "7" {
		t.Fatalf("decoded %+v", value)
	}
	if fmt.Sprint(found) != fmt.Sprint([]model.FieldIssue{{Path: "x.unknown", Kind: model.IssueUnknown}}) {
		t.Fatalf("issues %v", found)
	}
}

// N8-5 split stored-data load validation from write validation (design n7-n8,
// N8-5 prerequisite note); O18 T1a reads wrong-typed stored values leniently.
// A stored value never prevents loading: Python's number notations read as
// their value, anything else as the zero value with a load warning. A write
// that does not change the member keeps its stored text; the write that
// changes it stores the typed value. A value that still fails the business
// rules refuses unrelated writes with the validation message until repaired.
func TestModelStoredValuesLoadLenientlyAndWriteTyped(t *testing.T) {
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
		// stored is the stored value after the unrelated write (unchanged).
		stored string
		repair func(r *Repository) error
		// repaired is the stored value after the repairing write.
		repaired string
	}{
		{"string revision", "_revision", `"60"`, false, 60, "默认配置", "", `"60"`,
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `61`},
		{"integral float revision", "_revision", `60.0`, false, 60, "默认配置", "", `60.0`,
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `61`},
		// 60.5 is not an integer: it reads as 0, so the repair stores 1.
		{"fractional revision", "_revision", `60.5`, true, 0, "默认配置", "", `60.5`,
			func(r *Repository) error { _, err := r.UpdateProfile(bg, "default", noopProfile); return err }, `1`},
		{"numeric name", "name", `7`, false, 0, "7", "", `7`,
			func(r *Repository) error {
				_, err := r.UpdateProfileMetadataRecord(bg, "default", ProfileMetadataUpdate{Name: &fixed})
				return err
			}, `"fixed"`},
		{"empty name", "name", `""`, false, 0, "", "配置名称不能为空", `""`,
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
			_, issues, err := decodeModelRows(entry.Document)
			noError(t, err)
			if (len(issues) > 0) != tt.wantIssues {
				t.Fatalf("issues=%v", issues)
			}

			reopened := must(Open(r.DataDir(), Options{}))(t)
			reopened.SetViewGuard(func() { t.Error("View callback modified the snapshot") })
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
			if got := storedValue(t, reopened, tt.field); got != tt.stored {
				t.Fatalf("stored value after an unrelated write: %s", got)
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
		base := must(r.store.Load())(t)
		rows := cloneStoreDoc(t, base.Document)
		for i := range rows.Profiles {
			setStoreField(&rows.Profiles[i].Fields, "description", json.RawMessage(`{"password":"`+secret+`"}`))
			setStoreField(&rows.Profiles[i].Fields, "future_member", json.RawMessage(`"`+secret+`"`))
		}
		_, _, err := r.store.Commit(base, rows, nil)
		noError(t, err)
		r.dropCache()
		for range 5 {
			_, err := r.Profile(bg, "default")
			noError(t, err)
		}
		// Repair the description so the next head has no type issue.
		noError(t, r.Update(bg, func(_ context.Context, document *model.Document) error {
			profileByKey(document, "default").Description = "repaired"
			return nil
		}))
		base = must(r.store.Load())(t)
		rows = cloneStoreDoc(t, base.Document)
		for i := range rows.Profiles {
			setStoreField(&rows.Profiles[i].Fields, "description", json.RawMessage(`{"password":"`+secret+`"}`))
		}
		_, _, err = r.store.Commit(base, rows, nil)
		noError(t, err)
		r.dropCache()
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
	// Unknown members are summarized once per head by path pattern and count.
	unknown := fmt.Sprintf(msgModelUnknownMember, "profiles[].future_member", 1)
	if bytes.Count(output, []byte(unknown)) != 1 {
		t.Fatalf("expected one unknown-member summary:\n%s", output)
	}
	if bytes.Contains(output, []byte(secret)) || bytes.Contains(output, []byte("password")) {
		t.Fatal("stored value leaked into diagnostic")
	}
}
