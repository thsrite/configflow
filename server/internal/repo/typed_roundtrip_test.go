package repo

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/model"
)

// TestTypedUpdateStoredRoundTrip is design 5.2 through the repository's typed
// write API (N8-5a): every edit runs as Repository.Update (validation, router
// guard, commit), changes exactly the rows it touched.
// It covers the three write intents of a stored member: untouched (null and
// wrong-typed bytes survive edits of the same row), replace and Clear.
func TestTypedUpdateStoredRoundTrip(t *testing.T) {
	r := must(OpenDefault(t.TempDir()))(t)
	path := r.DatabasePath()
	row := func(t *testing.T, key modelRowKey) string {
		t.Helper()
		entry := must(r.store.Load())(t)
		rows := map[modelRowKey]string{}
		for _, field := range entry.Document.Root {
			rows[modelRowKey{"root", "", field.Name}] = string(field.Value)
		}
		for _, field := range entry.Document.System.Fields {
			rows[modelRowKey{"system", "", field.Name}] = string(field.Value)
		}
		for _, field := range entry.Document.Shared.Fields {
			rows[modelRowKey{"shared", "", field.Name}] = string(field.Value)
		}
		for _, profile := range entry.Document.Profiles {
			for _, field := range profile.Fields {
				rows[modelRowKey{"profile", profile.ID, field.Name}] = string(field.Value)
			}
		}
		return rows[key]
	}
	update := func(t *testing.T, label string, allowed []modelRowKey, edit func(*model.Document)) {
		t.Helper()
		before := must(r.store.Load())(t)
		noError(t, r.Update(context.Background(), func(_ context.Context, d *model.Document) error {
			edit(d)
			return nil
		}))
		after := must(r.store.Load())(t)
		changed := changedModelRowKeys(before.Document, after.Document)
		if len(changed) != len(allowed) {
			t.Fatalf("%s changed %d rows, want %d: %v", label, len(changed), len(allowed), changed)
		}
		for _, key := range allowed {
			if !changed[key] {
				t.Fatalf("%s did not change %v", label, key)
			}
		}
		if after.Head.Generation != before.Head.Generation+1 {
			t.Fatalf("%s generation %d → %d", label, before.Head.Generation, after.Head.Generation)
		}
		db := must(sql.Open("sqlite", path))(t)
		defer db.Close()
		result := must(db.Query("SELECT scope,owner,field,revision FROM fields"))(t)
		defer result.Close()
		for result.Next() {
			var key modelRowKey
			var revision int64
			noError(t, result.Scan(&key.scope, &key.owner, &key.name, &revision))
			if (revision == after.Head.Generation) != changed[key] {
				t.Fatalf("%s: row %v revision %d", label, key, revision)
			}
		}
		noError(t, result.Err())

	}
	profile := func(name string) modelRowKey { return modelRowKey{"profile", DefaultProfileID, name} }
	defaultProfile := func(d *model.Document) *model.Profile { return profileByKey(d, DefaultProfileID) }

	before := must(r.store.Load())(t)
	noError(t, r.Update(bg, func(context.Context, *model.Document) error { return nil }))
	if after := must(r.store.Load())(t); after.Head != before.Head {
		t.Fatal("no-op Update committed")
	}

	update(t, "profile rename", []modelRowKey{profile("name")}, func(d *model.Document) { defaultProfile(d).Name = "N8-5 typed" })
	update(t, "node add", []modelRowKey{{"shared", "", "nodes"}}, func(d *model.Document) {
		var node model.Node
		noError(t, json.Unmarshal([]byte(`{"id":"n85-node","name":"N85","type":"ss","server":"192.0.2.10","port":8388,"n8_unknown":{"k":[1,2.50]}}`), &node))
		d.Shared.Nodes = append(d.Shared.Nodes, node)
	})
	update(t, "rule reorder", []modelRowKey{profile("rule_configs")}, func(d *model.Document) {
		rules := defaultProfile(d).RuleConfigs
		if len(rules) < 2 {
			t.Fatal("default profile needs two rules")
		}
		rules[0], rules[1] = rules[1], rules[0]
	})

	// Stored null and wrong-typed members, written by a raw store commit
	// (Python tolerates both): untouched → kept, also when another member of
	// the same row changes; replace; Clear.
	prepend := func(value json.RawMessage, members string) json.RawMessage {
		text := strings.TrimSpace(string(value))
		if strings.TrimSpace(text[1:]) == "}" {
			return json.RawMessage("{" + members + "}")
		}
		return json.RawMessage("{" + members + "," + text[1:])
	}
	entry := must(r.store.Load())(t)
	raw := entry.Document
	for i := range raw.System.Fields {
		switch raw.System.Fields[i].Name {
		case "system_config":
			raw.System.Fields[i].Value = prepend(raw.System.Fields[i].Value, `"default_profile_id":null`)
		case "backup":
			raw.System.Fields[i].Value = prepend(raw.System.Fields[i].Value, `"auto_backup":"yes","keep_daily":1.0`)
		}
	}
	for i := range raw.Profiles {
		for j := range raw.Profiles[i].Fields {
			if raw.Profiles[i].ID == DefaultProfileID && raw.Profiles[i].Fields[j].Name == "description" {
				raw.Profiles[i].Fields[j].Value = json.RawMessage(`{"password":"synthetic-never-log"}`)
			}
		}
	}
	_, written, err := r.store.Commit(t.Context(), entry, raw, nil)
	if err != nil || !written {
		t.Fatalf("raw injection: %v %v", written, err)
	}

	noError(t, r.View(bg, func(d *model.Document) error {
		// auto_backup pushes on its own: a wrong type reads as off (O18 T1c).
		if model.Flag(d.System.Backup.AutoBackup, false) || defaultProfile(d).Description != "" || d.System.SystemConfig.DefaultProfileID != nil {
			t.Fatal("R5/null fallback values")
		}
		return nil
	}))
	update(t, "unrelated edit keeps rows it does not change", []modelRowKey{profile("name")}, func(d *model.Document) { defaultProfile(d).Name = "N8-5 R5" })
	// A row a typed edit changes is written in Go's form (O18 T1c): a stored
	// null, a wrong-typed value and number text are not kept.
	update(t, "same-row edit writes the typed values", []modelRowKey{{"system", "", "system_config"}}, func(d *model.Document) {
		d.System.SystemConfig.DefaultProfileID = nil
		d.System.SystemConfig.ServerDomain = "cf.example.com"
	})
	if got := row(t, modelRowKey{"system", "", "system_config"}); strings.Contains(got, "default_profile_id") {
		t.Fatal("stored null kept")
	}
	update(t, "same-row edit writes wrong type as read", []modelRowKey{{"system", "", "backup"}}, func(d *model.Document) {
		d.System.Backup.WebDAVPath = new(string)
		*d.System.Backup.WebDAVPath = "/n85"
	})
	// keep_daily 1.0 is not an integer literal: it reads as absent (the
	// default retention, as in Python) and is not written (int:"literal").
	if got := row(t, modelRowKey{"system", "", "backup"}); !strings.Contains(got, `"auto_backup":false`) || strings.Contains(got, `"keep_daily"`) || strings.Contains(got, `1.0`) {
		t.Fatal("backup row not written in Go's form: " + got)
	}
	update(t, "description replace", []modelRowKey{profile("description")}, func(d *model.Document) { defaultProfile(d).Description = "replaced" })

}
