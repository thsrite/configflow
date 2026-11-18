package repo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

// storedRows is every field row of a repository keyed by scope/owner/field.
func storedRows(t *testing.T, r *Repository) (store.Entry, map[modelRowKey]string) {
	t.Helper()
	entry := must(r.store.Load())(t)
	rows := map[modelRowKey]string{}
	add := func(scope, owner string, fields []store.Field) {
		for _, field := range fields {
			rows[modelRowKey{scope, owner, field.Name}] = string(field.Value)
		}
	}
	add("root", "", entry.Document.Root)
	add("system", "", entry.Document.System.Fields)
	add("shared", "", entry.Document.Shared.Fields)
	for _, profile := range entry.Document.Profiles {
		add("profile", profile.ID, profile.Fields)
	}
	return entry, rows
}

func rowKeyNames(keys map[modelRowKey]bool) []string {
	var names []string
	for key := range keys {
		names = append(names, key.scope+"/"+key.owner+"/"+key.name)
	}
	sort.Strings(names)
	return names
}

// TestSectionWritesStoredRoundTrip is the N8-5b storage differential: the
// typed section writes and the legacy writers built on them change exactly
// the rows they own, keep unknown members, stored nulls, wrong-typed bytes
// and number text of untouched data, refuse without writing, and the real
// Python ProfileRepository opens every Go-written database with the same
// document (CONFIGFLOW_PY/CONFIGFLOW_REPO).
func TestSectionWritesStoredRoundTrip(t *testing.T) {
	interpreter, root := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	pythonReady := interpreter != "" && root != ""
	r := must(OpenDefault(t.TempDir()))(t)
	audit := func(t *testing.T, mode string) {
		t.Helper()
		if !pythonReady {
			return
		}
		entry := must(r.store.Load())(t)
		expectation := filepath.Join(t.TempDir(), "expected.json")
		noError(t, os.WriteFile(expectation, must(json.Marshal(entry.Document))(t), 0o600))
		command := exec.Command(interpreter, "../store/testdata/raw_crosscheck.py", mode+"-repository", r.DatabasePath(), expectation)
		command.Env = append(os.Environ(), "CONFIGFLOW_REPO="+root, "PYTHONDONTWRITEBYTECODE=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("Python %s: %v\n%s", mode, err, output)
		}
	}
	profile := func(owner, name string) modelRowKey { return modelRowKey{"profile", owner, name} }
	shared := func(name string) modelRowKey { return modelRowKey{"shared", "", name} }
	system := func(name string) modelRowKey { return modelRowKey{"system", "", name} }
	step := func(t *testing.T, label string, want []modelRowKey, write func() error) {
		t.Helper()
		before, beforeRows := storedRows(t, r)
		if err := write(); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		after, afterRows := storedRows(t, r)
		changed := changedModelRowKeys(before.Document, after.Document)
		expected := map[modelRowKey]bool{}
		for _, key := range want {
			expected[key] = true
		}
		if got, wanted := rowKeyNames(changed), rowKeyNames(expected); strings.Join(got, ",") != strings.Join(wanted, ",") {
			t.Fatalf("%s changed rows %v, want %v", label, got, wanted)
		}
		for key, value := range beforeRows {
			if !changed[key] && afterRows[key] != value {
				t.Fatalf("%s rewrote %v", label, key)
			}
		}
		audit(t, "audit")
	}
	refused := func(t *testing.T, label string, kind error, write func() error) {
		t.Helper()
		before := must(r.store.Fingerprint())(t)
		if err := write(); err == nil || !errors.Is(err, kind) {
			t.Fatalf("%s: %v", label, err)
		}
		if must(r.store.Fingerprint())(t) != before {
			t.Fatalf("%s wrote", label)
		}
	}
	const p = DefaultProfileID

	// Unknown members, a stored null, a wrong-typed member and number text in
	// rows the following writes either own or must leave alone.
	entry := must(r.store.Load())(t)
	for i := range entry.Document.Shared.Fields {
		field := &entry.Document.Shared.Fields[i]
		switch field.Name {
		case "nodes":
			field.Value = json.RawMessage(`[{"id":"n1","name":"Node 1","type":"ss","server":"192.0.2.1","port":8388,"remark":7,"n85":{"z":1.0}},` +
				`{"id":"n2","name":"Node 2","type":"ss","server":"192.0.2.2","port":8389}]`)
		case "subscriptions":
			field.Value = json.RawMessage(`[{"id":"s1","name":"Sub","url":"https://sub.example.com/x","description":"\u00e9","n85":null}]`)
		}
	}
	entry.Document.Shared.Fields = append(entry.Document.Shared.Fields, store.Field{Name: "n85_shared", Value: json.RawMessage(` {"keep":"é"} `)})
	for i := range entry.Document.Profiles {
		if entry.Document.Profiles[i].ID != p {
			continue
		}
		for j := range entry.Document.Profiles[i].Fields {
			field := &entry.Document.Profiles[i].Fields[j]
			switch field.Name {
			case "proxy_groups":
				field.Value = json.RawMessage(`[{"id":"g1","name":"Group","type":"select","manual_nodes":["n1"],"n85":[1.50]},` +
					`{"id":"g2","name":"Other","type":"select","manual_nodes":["n2"]}]`)
			case "mihomo":
				text := strings.TrimSpace(string(field.Value))
				if strings.TrimSpace(text[1:]) == "}" {
					field.Value = json.RawMessage(`{"n85":1e2}`)
				} else {
					field.Value = json.RawMessage(`{"n85":1e2,` + text[1:])
				}
			}
		}
		entry.Document.Profiles[i].Fields = append(entry.Document.Profiles[i].Fields,
			store.Field{Name: "n85_profile", Value: json.RawMessage(`{"nested":[null,true]}`)})
	}
	_, written, err := r.store.Commit(entry, entry.Document, nil)
	if err != nil || !written {
		t.Fatalf("fixture: %v %v", written, err)
	}
	audit(t, "audit")

	step(t, "typed UpdateProfile", []modelRowKey{profile(p, "proxy_groups"), profile(p, "_revision"), profile(p, "updated_at")}, func() error {
		_, err := r.UpdateProfile(bg, p, func(_ context.Context, profile *model.Profile) error {
			profile.ProxyGroups[1].Name = "Renamed"
			profile.Name = "ignored: metadata is not a setting"
			return nil
		})
		return err
	})
	step(t, "typed UpdateShared", []modelRowKey{shared("nodes"), shared("_revision")}, func() error {
		return r.UpdateShared(bg, func(_ context.Context, s *model.Shared) error {
			var node model.Node
			noError(t, json.Unmarshal([]byte(`{"id":"n3","name":"Node 3","type":"ss","server":"192.0.2.3","port":8390}`), &node))
			s.Nodes = append(s.Nodes, node)
			return nil
		})
	})
	refused(t, "removing a referenced node", ErrInUse, func() error {
		return r.UpdateShared(bg, func(_ context.Context, s *model.Shared) error {
			s.Nodes = s.Nodes[1:]
			return nil
		})
	})
	step(t, "typed UpdateSharedAndProfiles removes a node with its last reference",
		[]modelRowKey{shared("nodes"), shared("_revision"), profile(p, "proxy_groups"), profile(p, "_revision"), profile(p, "updated_at")},
		func() error {
			return r.UpdateSharedAndProfiles(bg, func(_ context.Context, s *model.Shared, profiles []*model.Profile) error {
				// n2's only reference (group g2) moves to n1 in the same write.
				kept := s.Nodes[:0]
				for _, node := range s.Nodes {
					if node.ID != "n2" {
						kept = append(kept, node)
					}
				}
				s.Nodes = kept
				for _, profile := range profiles {
					if profile.ID == p {
						nodes := []string{"n1"}
						profile.ProxyGroups[1].ManualNodes = &nodes
					}
				}
				return nil
			})
		})
	step(t, "legacy no-op profile update keeps every setting row", []modelRowKey{profile(p, "_revision"), profile(p, "updated_at")}, func() error {
		_, _, err := r.UpdateProfileTransaction(bg, p, func(context.Context, *jsonx.Object) (*jsonx.Object, error) { return nil, nil })
		return err
	})
	step(t, "legacy shared update", []modelRowKey{shared("subscriptions"), shared("_revision")}, func() error {
		_, err := r.UpdateSharedTransaction(bg, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			current.Set("subscriptions", append(current.GetList("subscriptions"),
				jsonx.Obj("id", "s2", "name", "Sub 2", "url", "https://sub.example.com/y")))
			return nil, nil
		})
		return err
	})
	refused(t, "legacy updater with a wrong-shaped setting", ErrValidation, func() error {
		_, _, err := r.UpdateProfileTransaction(bg, p, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
			current.Set("mosdns", "not an object")
			return nil, nil
		})
		return err
	})
	step(t, "metadata rename", []modelRowKey{profile(p, "name"), profile(p, "_revision"), profile(p, "updated_at")}, func() error {
		_, err := r.UpdateProfileMetadata(bg, p, jsonx.Obj("name", "N8-5b"))
		return err
	})
	var cloneRows []modelRowKey
	_, sourceRows := storedRows(t, r)
	for key := range sourceRows {
		if key.scope == "profile" && key.owner == p {
			cloneRows = append(cloneRows, profile("copy", key.name))
		}
	}
	step(t, "clone", cloneRows, func() error {
		_, err := r.CloneProfile(bg, p, jsonx.Obj("id", "copy", "name", "Copy"))
		return err
	})
	step(t, "default switch", []modelRowKey{system("system_config"), system("_revision")}, func() error {
		_, err := r.SetDefaultProfile(bg, "copy")
		return err
	})

	_, rows := storedRows(t, r)
	for key, want := range map[modelRowKey]string{
		shared("n85_shared"):           ` {"keep":"é"} `,
		profile(p, "n85_profile"):      `{"nested":[null,true]}`,
		profile("copy", "n85_profile"): `{"nested":[null,true]}`,
	} {
		if rows[key] != want {
			t.Fatalf("%v = %s", key, rows[key])
		}
	}
	for key, part := range map[modelRowKey]string{
		shared("nodes"): `"remark":7,"n85":{"z":1.0}`,
		// A legacy writer re-encodes the rows it changes as a whole (as
		// Python did); values survive, escapes are not kept.
		shared("subscriptions"):    `"description":"é","n85":null`,
		profile(p, "proxy_groups"): `"n85":[1.50]`,
		profile(p, "mihomo"):       `"n85":1e2`,
	} {
		if !strings.Contains(rows[key], part) {
			t.Fatalf("%v lost %s: %s", key, part, rows[key])
		}
	}
	if pythonReady {
		audit(t, "audit-edit")
		step(t, "typed edit after Python", []modelRowKey{profile(p, "proxy_groups"), profile(p, "_revision"), profile(p, "updated_at")}, func() error {
			_, err := r.UpdateProfile(bg, p, func(_ context.Context, profile *model.Profile) error {
				profile.ProxyGroups[0].Name = "After Python"
				return nil
			})
			return err
		})
		audit(t, "audit-final")
	}
}
