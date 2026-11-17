package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/model"
)

func n85Files(t *testing.T, directory string) map[string]string {
	t.Helper()
	files := map[string]string{}
	noError(t, filepath.WalkDir(directory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() == DatabaseFilename {
			return err
		}
		files[must(filepath.Rel(directory, path))(t)] = string(must(os.ReadFile(path))(t))
		return nil
	}))
	return files
}

func TestReviewN85InvalidStoredValuesDoNotTriggerStartupWrites(t *testing.T) {
	tests := []struct {
		name, settings string
		invalidName    bool
	}{
		{"null settings", `null`, false},
		{"numeric settings", `7`, false},
		{"string settings", `"malformed"`, false},
		{"invalid name and missing token", `{}`, true},
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
			_, _, err := r.store.Commit(base, base.Document, nil)
			noError(t, err)
			noError(t, os.WriteFile(filepath.Join(r.DataDir(), "future.json"), []byte(` {"future":null,"wrong":"type"} `), 0o600))
			rows, files := xcFingerprint(t, r.store), n85Files(t, r.DataDir())
			opened, err := Open(r.DataDir(), Options{})
			noError(t, err)
			// Compare all DB tables and the complete non-database file tree.
			if !reflect.DeepEqual(xcFingerprint(t, r.store), rows) || !reflect.DeepEqual(n85Files(t, r.DataDir()), files) {
				t.Fatal("loading changed the database or files")
			}
			_, err = opened.System(bg)
			noError(t, err)
			noError(t, opened.Update(bg, func(_ context.Context, d *model.Document) error {
				if tt.invalidName {
					profileByKey(d, DefaultProfileID).Name = "repaired"
				} else {
					d.System.SystemConfig.RuleProxyToken = "synthetic-rule-token"
				}
				return nil
			}))
			if reflect.DeepEqual(xcFingerprint(t, r.store), rows) {
				t.Fatal("repair did not commit")
			}
			if !reflect.DeepEqual(n85Files(t, r.DataDir()), files) {
				t.Fatal("repair changed unrelated files")
			}
		})
	}
}

// Automatic token maintenance must not re-encode the other members of the
// settings record through the lossy legacy adapter.
func TestReviewN85TokenMaintenancePreservesUntouchedMembers(t *testing.T) {
	for _, entry := range []string{"typed", "legacy"} {
		t.Run(entry, func(t *testing.T) {
			r := newRepo(t)
			base := must(r.store.Load())(t)
			const settings = `{"rule_proxy_token":"synthetic-rule-token","retired_rule_proxy_tokens":["old","old"],"default_profile_id":null,"node_test_dns":{"future": 1.2300},"future": {"number":9007199254740993.0,"escaped":"\u003c"}}`
			for i := range base.Document.System.Fields {
				if base.Document.System.Fields[i].Name == "system_config" {
					base.Document.System.Fields[i].Value = json.RawMessage(settings)
				}
			}
			_, _, err := r.store.Commit(base, base.Document, nil)
			noError(t, err)
			if entry == "typed" {
				noError(t, r.Update(bg, func(_ context.Context, d *model.Document) error {
					profileByKey(d, DefaultProfileID).Name = "unrelated edit"
					return nil
				}))
			} else {
				_, err = r.UpdateProfile(bg, DefaultProfileID, parse(t, `{"name":"unrelated edit"}`))
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
			for name, value := range original.All() {
				if name == "retired_rule_proxy_tokens" {
					continue // the explicit business normalization
				}
				got, present := saved.Get(name)
				if !present || !bytes.Equal(got, value) {
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
