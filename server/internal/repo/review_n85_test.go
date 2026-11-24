package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

func TestReviewN85PythonFullStorageDifferential(t *testing.T) {
	interpreter, root := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	if interpreter == "" || root == "" {
		t.Skip("CONFIGFLOW_PY and CONFIGFLOW_REPO are required")
	}
	python := func(directory, action string) *testObj {
		t.Helper()
		output := filepath.Join(t.TempDir(), "snapshot.json")
		command := exec.Command(interpreter, "testdata/review_n85_storage.py", directory, output, action)
		command.Env = append(os.Environ(), "CONFIGFLOW_REPO="+root, "PYTHONDONTWRITEBYTECODE=1")
		if data, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Python action %s: %v\n%s", action, err, data)
		}
		return parseJSONObj(t, must(os.ReadFile(output))(t))
	}
	pyDir, goDir := t.TempDir(), t.TempDir()
	python(pyDir, "seed")
	xcCopyTree(t, pyDir, goDir)
	r := openRepo(t, goDir)
	for _, action := range []string{"rename", "same-row", "replace", "clear"} {
		t.Run(action, func(t *testing.T) {
			want := python(pyDir, action)
			noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
				switch action {
				case "rename":
					profileByKey(d, DefaultProfileID).Name = "review renamed"
				case "same-row":
					d.System.SystemConfig.ServerDomain = "cf.example.com"
					path := "/review"
					d.System.Backup.WebDAVPath = &path
				case "replace":
					profileByKey(d, DefaultProfileID).Description = "repaired"
				case "clear":
					if err := d.System.SystemConfig.Clear("default_profile_id"); err != nil {
						return err
					}
					return d.System.Backup.Clear("auto_backup")
				}
				return nil
			}))
			gotTables, wantTables := xcDump(t, goDir), want.GetObject("tables")
			// O18 T1a: an undeclared root member is dropped by Go's first
			// write; Python keeps it. Everything else compares as before.
			var kept []any
			for _, value := range wantTables.GetList("fields") {
				if row := value.([]any); row[0] == "root" && row[2] == "future_root" {
					continue
				}
				kept = append(kept, value)
			}
			wantTables.Set("fields", kept)
			for _, value := range wantTables.GetList("meta") {
				if row := value.([]any); row[0] == "root_order" {
					switch order := row[1].(type) {
					case string:
						row[1] = strings.Replace(order, `,"future_root"`, "", 1)
					case []any:
						var names []any
						for _, name := range order {
							if name != "future_root" {
								names = append(names, name)
							}
						}
						row[1] = names
					}
				}
			}
			// The export shows typed values: a stored description of the
			// wrong type is exported as "" (read leniently), not as stored.
			if export, ok := want.Value("export").(*testObj); ok {
				export.Delete("future_root")
				// Members no plain record declares are dropped (O18 T1c).
				if config := export.GetObject("system").GetObject("system_config"); config != nil {
					config.Delete("future_setting")
				}
				// auto_backup starts automatic pushes: stored with another
				// type it reads as off (O18 T1c gate ruling).
				if backup := export.GetObject("system").GetObject("backup"); backup != nil {
					if _, flag := backup.Value("auto_backup").(bool); !flag && backup.Has("auto_backup") {
						backup.Set("auto_backup", false)
					}
				}
				for _, profile := range export.GetObject("profiles").All() {
					if object, ok := profile.(*testObj); ok {
						if _, text := object.Value("description").(string); !text {
							object.Set("description", "")
						}
					}
				}
			}
			// JSON value spelling is not a cross-language formatting contract.
			// Compare every table column, parsing only fields.value.
			for _, tables := range []*testObj{gotTables, wantTables} {
				for _, value := range tables.GetList("fields") {
					row := value.([]any)
					row[4] = parseJSONVal(t, []byte(row[4].(string)))
				}
			}
			// A backup row Go rewrote holds auto_backup as read: another
			// type is off (O18 T1c gate ruling).
			// Members no plain record declares are dropped when Go rewrites
			// the row (O18 T1c); an unchanged row keeps its stored bytes.
			for _, tables := range []*testObj{gotTables, wantTables} {
				for _, value := range tables.GetList("fields") {
					row := value.([]any)
					if config, ok := row[4].(*testObj); ok && row[0] == "system" && row[2] == "system_config" {
						config.Delete("future_setting")
					}
				}
			}
			for _, value := range wantTables.GetList("fields") {
				row := value.([]any)
				if row[0] != "system" || row[2] != "backup" {
					continue
				}
				for _, got := range gotTables.GetList("fields") {
					goRow := got.([]any)
					if goRow[0] != "system" || goRow[2] != "backup" {
						continue
					}
					backup, ok := row[4].(*testObj)
					goBackup, goOK := goRow[4].(*testObj)
					if ok && goOK && goBackup.Value("auto_backup") == false {
						if _, flag := backup.Value("auto_backup").(bool); !flag && backup.Has("auto_backup") {
							backup.Set("auto_backup", false)
						}
					}
				}
			}
			xcSame(t, "all database tables", gotTables, wantTables)
			data := must(json.Marshal(n85Files(t, goDir)))(t)
			xcSame(t, "complete file contents", parseJSONVal(t, data), want.Value("files"))
			xcSame(t, "complete exported document", parseJSONVal(t, must(r.ExportAllJSON(bg, false))(t)), want.Value("export"))
		})
		if t.Failed() {
			return
		}
	}
}

func n85Files(t *testing.T, directory string) map[string]string {
	t.Helper()
	files := map[string]string{}
	noError(t, filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path == filepath.Join(directory, BackupsDirName) {
			return filepath.SkipDir // Go's pre-upgrade backup of the Python database (O17)
		}
		if err != nil || entry.IsDir() || entry.Name() == DatabaseFilename {
			return err
		}
		files[must(filepath.Rel(directory, path))(t)] = string(must(os.ReadFile(path))(t))
		return nil
	}))
	return files
}

// O18 T1a: wrong-typed stored values read as their zero value with a load
// warning. Settings that are not an object stay as stored (no startup write);
// a numeric profile name reads as text, so the document is valid and startup
// token maintenance writes once. The next start writes nothing and no other
// file changes.
func TestReviewN85StartupWritesOnlyRepairableStoredValues(t *testing.T) {
	tests := []struct {
		name, settings string
		invalidName    bool
		// repaired: startup token maintenance writes once. Settings that are
		// not an object are damaged credentials and are never rewritten
		// implicitly (SystemSettingsDamaged).
		repaired bool
	}{
		{"null settings", `null`, false, false},
		{"numeric settings", `7`, false, false},
		{"string settings", `"malformed"`, false, false},
		{"numeric name and missing token", `{}`, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRepo(t)
			base := must(r.store.Load())(t)
			for i := range base.Document.System.Fields {
				if base.Document.System.Fields[i].Name == "system_config" {
					base.Document.System.Fields[i].Value = json.RawMessage(tt.settings)
				}
			}
			if tt.invalidName {
				for i := range base.Document.Profiles[0].Fields {
					if base.Document.Profiles[0].Fields[i].Name == "name" {
						base.Document.Profiles[0].Fields[i].Value = json.RawMessage(`7`)
					}
				}
			}
			_, _, err := r.store.Commit(t.Context(), base, base.Document, nil)
			noError(t, err)
			noError(t, os.WriteFile(filepath.Join(r.DataDir(), "future.json"), []byte(` {"future":null,"wrong":"type"} `), 0o600))
			rows, files := xcFingerprint(t, r.store), n85Files(t, r.DataDir())
			opened, err := Open(r.DataDir(), Options{})
			noError(t, err)
			if reflect.DeepEqual(xcFingerprint(t, r.store), rows) == tt.repaired {
				t.Fatalf("startup write=%v, want %v", !tt.repaired, tt.repaired)
			}
			if !reflect.DeepEqual(n85Files(t, r.DataDir()), files) {
				t.Fatal("startup changed unrelated files")
			}
			settings := must(opened.SystemConfig(bg))(t)
			if (settings.RuleProxyToken != "") != tt.repaired {
				t.Fatal("token maintenance")
			}
			if damaged := must(opened.SystemSettingsDamaged(bg))(t); damaged == tt.repaired {
				t.Fatalf("damaged=%v", damaged)
			}
			if tt.invalidName && must(opened.Profile(bg, DefaultProfileID))(t).Name != "7" {
				t.Fatal("numeric name not read as text")
			}
			rows = xcFingerprint(t, r.store)
			_, err = Open(r.DataDir(), Options{})
			noError(t, err)
			if !reflect.DeepEqual(xcFingerprint(t, r.store), rows) {
				t.Fatal("second start wrote")
			}
		})
	}
}

// Automatic token maintenance must not re-encode the other members of the
// settings record through the lossy legacy adapter.
func TestReviewN85TokenMaintenancePreservesUntouchedMembers(t *testing.T) {
	for _, entry := range []string{"update", "metadata"} {
		t.Run(entry, func(t *testing.T) {
			r := newRepo(t)
			base := must(r.store.Load())(t)
			const settings = `{"rule_proxy_token":"synthetic-rule-token","retired_rule_proxy_tokens":["old","old"],"default_profile_id":null,"node_test_dns":{"future": 1.2300},"future": {"number":9007199254740993.0,"escaped":"\u003c"}}`
			for i := range base.Document.System.Fields {
				if base.Document.System.Fields[i].Name == "system_config" {
					base.Document.System.Fields[i].Value = json.RawMessage(settings)
				}
			}
			_, _, err := r.store.Commit(t.Context(), base, base.Document, nil)
			noError(t, err)
			if entry == "update" {
				noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
					profileByKey(d, DefaultProfileID).Name = "unrelated edit"
					return nil
				}))
			} else {
				name := "unrelated edit"
				_, err = r.UpdateProfileMetadataRecord(bg, DefaultProfileID, ProfileMetadataUpdate{Name: &name})
				noError(t, err)
			}
			after := must(r.store.Load())(t)
			var original, saved model.Fields
			noError(t, original.UnmarshalJSON([]byte(settings)))
			for _, field := range after.Document.System.Fields {
				if field.Name == "system_config" {
					noError(t, saved.UnmarshalJSON(field.Value))
				}
			}
			// Token maintenance rewrites the settings row in Go's form (O18
			// T1c): a null, wrong-typed or undeclared member is not kept.
			for _, name := range []string{"default_profile_id", "node_test_dns", "future"} {
				if got, present := saved.Get(name); present {
					t.Errorf("dropped member %s kept: %s", name, got)
				}
			}
			for name, value := range original.All() {
				if name == "retired_rule_proxy_tokens" || name == "default_profile_id" || name == "node_test_dns" || name == "future" {
					continue // the explicit business normalization, or dropped above
				}
				// Values, number text included; whitespace and escapes follow
				// Go's encoding (O18 T1a).
				got, present := saved.Get(name)
				if !present || !testjson.Equal(value, got, testjson.CompareOptions{DistinguishNumberLiterals: true}) || bytes.Contains(value, []byte("1.2300")) != bytes.Contains(got, []byte("1.2300")) {
					t.Errorf("untouched member %s changed: %s -> %s", name, value, got)
				}
			}
			retired, _ := saved.Get("retired_rule_proxy_tokens")
			if string(retired) != `["old"]` {
				t.Fatalf("token maintenance did not run: %s", retired)
			}
		})
	}
}

func TestReviewN85MergeTypedEqualityAndConflicts(t *testing.T) {
	tests := []struct {
		name, current, baseline, proposed, want string
		changed                                 bool
	}{
		{"same float despite spelling", `{"v":2.0,"keep":true}`, `{"v":1.00}`, `{"v":1e0}`, `{"v":2.0,"keep":true}`, false},
		{"mapping order and escapes", `{"keep":true,"v":{"x":"é","y":2}}`, `{"v":{"x":"é","y":1}}`, `{"v":{"y":1,"x":"\u00e9"}}`, `{"keep":true,"v":{"x":"é","y":2}}`, false},
		{"exact large float change", `{"v":9007199254740992.0}`, `{"v":9007199254740992.0}`, `{"v":9007199254740993.0}`, `{"v":9007199254740993.0}`, true},
		{"same member caller wins", `{"v":2}`, `{"v":1}`, `{"v":3}`, `{"v":3}`, true},
		{"int float change is intentional", `{"v":2}`, `{"v":1}`, `{"v":1.0}`, `{"v":1.0}`, true},
		{"deleted item is not resurrected", `[]`, `[{"id":"a","v":1}]`, `[{"id":"a","v":2}]`, `[]`, false},
		{"caller deletion wins over item edit", `[{"id":"a","v":2},{"id":"b"}]`, `[{"id":"a","v":1},{"id":"b"}]`, `[{"id":"b"}]`, `[{"id":"b"}]`, true},
		{"distinct typed ids", `[{"id":1,"v":"new"},{"id":true,"v":"old"}]`, `[{"id":1,"v":"old"},{"id":true,"v":"old"}]`, `[{"id":1,"v":"old"},{"id":true,"v":"edit"}]`, `[{"id":1,"v":"new"},{"id":true,"v":"edit"}]`, true},
		{"same unkeyed object despite order", `[{"a":1,"b":2},"concurrent"]`, `[{"a":1,"b":2}]`, `[{"b":2,"a":1},"append"]`, `[{"a":1,"b":2},"concurrent","append"]`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, changed, err := MergeJSON([]byte(tt.current), []byte(tt.baseline), []byte(tt.proposed))
			noError(t, err)
			if string(got) != tt.want || changed != tt.changed {
				t.Fatalf("got %s changed=%v; want %s changed=%v", got, changed, tt.want, tt.changed)
			}
		})
	}
}

func TestReviewN85ViewRespectsCacheLimit(t *testing.T) {
	r := newRepo(t)
	r.cacheMaxBytes = 0
	r.dropCache()
	start := ccCounts(r)
	for range 2 {
		noError(t, r.View(bg, func(d *model.Document) error {
			if profileByKey(d, DefaultProfileID) == nil {
				t.Fatal("missing profile in uncached snapshot")
			}
			return nil
		}))
	}
	if got := ccSince(r, start); got != [2]int64{2, 2} || r.cached() != nil {
		t.Fatalf("uncached View loads/validations=%v cached=%v", got, r.cached() != nil)
	}
}

func FuzzMergeJSON(f *testing.F) {
	f.Add([]byte(`{"v":1.0}`), []byte(`{"v":1e0}`), []byte(`{"v":1.00}`))
	f.Add([]byte(`[{"id":"a","v":2}]`), []byte(`[{"id":"a","v":1}]`), []byte(`[]`))
	f.Add([]byte(`{"unknown":{"v":9007199254740993.0}}`), []byte(`{}`), []byte(`{"new":null}`))
	f.Fuzz(func(t *testing.T, current, baseline, proposed []byte) {
		if !json.Valid(current) || !json.Valid(baseline) || !json.Valid(proposed) {
			return
		}
		copies := [3][]byte{bytes.Clone(current), bytes.Clone(baseline), bytes.Clone(proposed)}
		got, changed, err := MergeJSON(current, baseline, proposed)
		// Native node depth/shape validation may reject arbitrary JSON trees.
		if err == nil {
			if !json.Valid(got) || (!changed && !bytes.Equal(got, current)) {
				t.Fatal("merge result violates JSON/current-byte contract")
			}
		}
		for i, value := range [3][]byte{current, baseline, proposed} {
			if !bytes.Equal(value, copies[i]) {
				t.Fatal("merge mutated an input")
			}
		}
		// A caller that did not edit its baseline must preserve a valid
		// current value byte for byte, even if current changed concurrently.
		got, changed, err = MergeJSON(current, baseline, baseline)
		noError(t, err)
		if changed || !bytes.Equal(got, current) {
			t.Fatal("unchanged caller rewrote current")
		}
	})
}
