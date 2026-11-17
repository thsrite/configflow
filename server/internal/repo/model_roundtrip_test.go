package repo

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/store"
)

// Test-only records exercise nested R1–R9 without prematurely defining N8-4's
// production group/node/agent/router/rule types. Unknown members remain opaque.
type roundTripRecord struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Enabled *bool     `json:"enabled"`
	Counter model.Int `json:"n8_counter"`
	Fields  model.Fields
	raw     model.Raw
}

func (r *roundTripRecord) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, r, &r.raw)
}
func (r roundTripRecord) MarshalJSON() ([]byte, error) { return model.EncodeRecord(&r, &r.raw) }

func roundTripList(value any) ([]roundTripRecord, error) {
	raw, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var records []roundTripRecord
	err = json.Unmarshal(raw, &records)
	return records, err
}

func roundTripNewRecord(t *testing.T, id, name string) roundTripRecord {
	t.Helper()
	var r roundTripRecord
	source := []byte(`{"id":"` + id + `","name":"` + name + `","n8_counter":true,"n8_unknown": { "z":1.0, "a":"\u4f8b <>&", "nested": {"preserved":true} }}`)
	if err := json.Unmarshal(source, &r); err != nil {
		t.Fatal("synthetic record decode failed")
	}
	if r.Counter != 0 || len(r.raw.Issues()) != 1 {
		t.Fatal("record R5 fallback missing")
	}
	return r
}

func roundTripFields(t *testing.T, raw json.RawMessage) model.Fields {
	t.Helper()
	var fields model.Fields
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = json.RawMessage(`{}`)
	}
	if err := fields.UnmarshalJSON(raw); err != nil {
		t.Fatal("opaque object decode failed")
	}
	return fields
}

func changedModelRowKeys(before, after *store.Document) map[modelRowKey]bool {
	flatten := func(doc *store.Document) map[modelRowKey][]byte {
		values := map[modelRowKey][]byte{}
		add := func(scope, owner string, fields []store.Field) {
			for _, field := range fields {
				values[modelRowKey{scope, owner, field.Name}] = field.Value
			}
		}
		add("root", "", doc.Root)
		if doc.System != nil {
			add("system", "", doc.System.Fields)
		}
		if doc.Shared != nil {
			add("shared", "", doc.Shared.Fields)
		}
		for _, p := range doc.Profiles {
			add("profile", p.ID, p.Fields)
		}
		return values
	}
	a, b := flatten(before), flatten(after)
	changed := map[modelRowKey]bool{}
	for key, value := range a {
		other, ok := b[key]
		if !ok || !bytes.Equal(value, other) {
			changed[key] = true
		}
	}
	for key, value := range b {
		other, ok := a[key]
		if !ok || !bytes.Equal(value, other) {
			changed[key] = true
		}
	}
	return changed
}

// TestStoredRoundTrip always exercises the committed row fixture and a full
// synthetic repository. Live Python and the private snapshot are optional env
// additions, mandatory in the final .32 command. No private value is reported.
func TestStoredRoundTrip(t *testing.T) {
	interpreter, root := os.Getenv("CONFIGFLOW_PY"), os.Getenv("CONFIGFLOW_REPO")
	pythonReady := interpreter != "" && root != ""
	python := func(t *testing.T, args ...string) {
		t.Helper()
		if !pythonReady {
			return
		}
		// Reuse the existing real-Python storage boundary driver rather than copy it.
		command := exec.Command(interpreter, append([]string{"../store/testdata/raw_crosscheck.py"}, args...)...)
		command.Env = append(os.Environ(), "CONFIGFLOW_REPO="+root, "PYTHONDONTWRITEBYTECODE=1")
		if err := command.Run(); err != nil {
			t.Fatalf("Python interchange failed (%T)", err)
		}
	}
	run := func(t *testing.T, path string, repositoryAudit bool) {
		target := store.New(path, LockTimeout)
		entry, err := target.Load()
		if err != nil {
			t.Fatal("initial store load failed")
		}
		before := entry.Document
		typed, _, err := decodeModelRows(before)
		if err != nil {
			t.Fatal("initial model decode failed")
		}
		rows, err := encodeModelRows(typed, before)
		if err != nil {
			t.Fatal("initial model encode failed")
		}
		assertModelRows(t, rows, before)
		fingerprint, err := target.Fingerprint()
		if err != nil {
			t.Fatal("initial fingerprint failed")
		}
		if _, changed, err := target.Commit(entry, rows, nil); err != nil || changed {
			t.Fatal("no-op model commit wrote rows")
		}
		current, err := target.Fingerprint()
		if err != nil || current != fingerprint {
			t.Fatal("no-op fingerprint changed")
		}
		if len(typed.Profiles) == 0 {
			t.Fatal("fixture needs a profile")
		}
		owner := before.Profiles[0].ID
		auditMode := func(mode string) string {
			if repositoryAudit {
				return mode + "-repository"
			}
			return mode
		}
		expectation := filepath.Join(filepath.Dir(path), "model-expected.json")
		audit := func(mode string) {
			if !pythonReady {
				return
			}
			data, err := json.Marshal(entry.Document)
			if err != nil {
				t.Fatal("expectation encode failed")
			}
			if os.WriteFile(expectation, data, 0o600) != nil {
				t.Fatal("expectation write failed")
			}
			python(t, auditMode(mode), path, expectation)
		}
		audit("audit")
		commit := func(label string, allowed []modelRowKey, edit func(*model.Document)) {
			t.Helper()
			document, _, err := decodeModelRows(entry.Document)
			if err != nil {
				t.Fatal("model reload failed")
			}
			edit(document)
			rows, err := encodeModelRows(document, entry.Document)
			if err != nil {
				t.Fatalf("%s model encode failed", label)
			}
			changed := changedModelRowKeys(entry.Document, rows)
			permit := map[modelRowKey]bool{}
			for _, key := range allowed {
				permit[key] = true
			}
			if len(changed) == 0 {
				t.Fatalf("%s did not touch a row", label)
			}
			for key := range changed {
				if !permit[key] {
					t.Fatalf("%s changed an unrelated row", label)
				}
			}
			next, written, err := target.Commit(entry, rows, nil)
			if err != nil || !written {
				t.Fatalf("%s store commit failed", label)
			}
			if next.Head.Generation != entry.Head.Generation+1 {
				t.Fatalf("%s generation differs", label)
			}
			// Physical revisions, not just semantic values: only touched rows advance.
			// Querying through a plain connection is independent of the model codec.
			db := must(sql.Open("sqlite", path))(t)
			result := must(db.Query("SELECT scope,owner,field,revision FROM fields"))(t)
			for result.Next() {
				var key modelRowKey
				var revision int64
				if result.Scan(&key.scope, &key.owner, &key.name, &revision) != nil {
					t.Fatal("revision scan failed")
				}
				if (revision == next.Head.Generation) != changed[key] {
					t.Fatalf("%s changed unrelated row revision", label)
				}
			}
			if result.Err() != nil {
				t.Fatal("revision read failed")
			}
			result.Close()
			db.Close()
			entry = next
			audit("audit")
		}
		profileKey := func(name string) modelRowKey { return modelRowKey{"profile", owner, name} }
		marker := json.RawMessage(` { "z":1.0,"a":"\u4f8b <>&","nested": { "future": true } } `)
		commit("unknown root", []modelRowKey{{"root", "", "n8_unknown"}}, func(d *model.Document) {
			if d.Root.Set("n8_unknown", marker) != nil {
				t.Fatal("root unknown set failed")
			}
		})
		commit("unknown system", []modelRowKey{{"system", "", "backup"}}, func(d *model.Document) {
			fields := roundTripFields(t, must(model.Marshal(d.System.Backup))(t))
			if fields.Set("n8_unknown", marker) != nil {
				t.Fatal("backup unknown set failed")
			}
			d.System.Backup = roundTripDecode[model.Backup](t, fields)
		})
		commit("unknown shared", []modelRowKey{{"shared", "", "n8_unknown"}}, func(d *model.Document) {
			if d.Shared.Fields.Set("n8_unknown", marker) != nil {
				t.Fatal("shared unknown set failed")
			}
		})
		commit("unknown profile", []modelRowKey{profileKey("n8_unknown")}, func(d *model.Document) {
			if d.Profiles[0].Fields.Set("n8_unknown", marker) != nil {
				t.Fatal("profile unknown set failed")
			}
		})
		commit("profile rename", []modelRowKey{profileKey("name")}, func(d *model.Document) { d.Profiles[0].Name = "N8 round-trip profile" })
		commit("group add", []modelRowKey{profileKey("proxy_groups")}, func(d *model.Document) {
			records := must(roundTripList(d.Profiles[0].ProxyGroups))(t)
			records = append(records, roundTripNewRecord(t, "n8-group", "N8 group"))
			d.Profiles[0].ProxyGroups = roundTripDecode[[]model.ProxyGroup](t, records)
		})
		commit("group rename", []modelRowKey{profileKey("proxy_groups")}, func(d *model.Document) {
			records := must(roundTripList(d.Profiles[0].ProxyGroups))(t)
			for i := range records {
				if records[i].ID == "n8-group" {
					records[i].Name = "N8 group renamed"
				}
			}
			d.Profiles[0].ProxyGroups = roundTripDecode[[]model.ProxyGroup](t, records)
		})
		commit("node add", []modelRowKey{{"shared", "", "nodes"}}, func(d *model.Document) {
			records := must(roundTripList(d.Shared.Nodes))(t)
			records = append(records, roundTripNewRecord(t, "n8-node", "N8 node"))
			d.Shared.Nodes = roundTripDecode[[]model.Node](t, records)
		})
		commit("node delete", []modelRowKey{{"shared", "", "nodes"}}, func(d *model.Document) {
			records := must(roundTripList(d.Shared.Nodes))(t)
			kept := records[:0]
			for _, record := range records {
				if record.ID != "n8-node" {
					kept = append(kept, record)
				}
			}
			d.Shared.Nodes = roundTripDecode[[]model.Node](t, kept)
		})
		commit("agent add", []modelRowKey{{"system", "", "agents"}}, func(d *model.Document) {
			records := must(roundTripList(d.System.Agents))(t)
			added := roundTripNewRecord(t, "n8-agent", "N8 agent")
			if added.Fields.Set("profile_id", must(model.Marshal(d.Profiles[0].ID))(t)) != nil {
				t.Fatal("agent binding failed")
			}
			records = append(records, added)
			d.System.Agents = roundTripDecode[[]model.Agent](t, records)
		})
		commit("agent edit", []modelRowKey{{"system", "", "agents"}}, func(d *model.Document) {
			records := must(roundTripList(d.System.Agents))(t)
			for i := range records {
				if records[i].ID == "n8-agent" {
					records[i].Name = "N8 agent renamed"
				}
			}
			d.System.Agents = roundTripDecode[[]model.Agent](t, records)
		})
		commit("router add", []modelRowKey{profileKey("routers")}, func(d *model.Document) {
			fields := roundTripFields(t, must(model.Marshal(d.Profiles[0].Routers))(t))
			items, _ := fields.Get("items")
			records := must(roundTripList(items))(t)
			added := roundTripNewRecord(t, "n8-router", "N8 router")
			for key, value := range map[string]string{"type": "routeros", "endpoint": "192.0.2.1", "username": "synthetic", "password": "synthetic"} {
				if added.Fields.Set(key, must(model.Marshal(value))(t)) != nil {
					t.Fatal("router field set failed")
				}
			}
			records = append(records, added)
			if fields.Set("items", must(model.Marshal(records))(t)) != nil {
				t.Fatal("router list set failed")
			}
			d.Profiles[0].Routers = roundTripDecode[model.Routers](t, fields)
		})
		commit("router setting", []modelRowKey{profileKey("routers")}, func(d *model.Document) {
			fields := roundTripFields(t, must(model.Marshal(d.Profiles[0].Routers))(t))
			items, _ := fields.Get("items")
			records := must(roundTripList(items))(t)
			for i := range records {
				if records[i].ID == "n8-router" {
					if records[i].Fields.Set("verify_tls", json.RawMessage(`true`)) != nil {
						t.Fatal("router setting failed")
					}
				}
			}
			if fields.Set("items", must(model.Marshal(records))(t)) != nil {
				t.Fatal("router list set failed")
			}
			d.Profiles[0].Routers = roundTripDecode[model.Routers](t, fields)
		})
		commit("rules add", []modelRowKey{profileKey("rule_configs")}, func(d *model.Document) {
			records := must(roundTripList(d.Profiles[0].RuleConfigs))(t)
			for _, suffix := range []string{"a", "b"} {
				added := roundTripNewRecord(t, "n8-rule-"+suffix, "N8 rule "+suffix)
				off := false
				added.Enabled = &off
				for key, value := range map[string]string{"itemType": "rule", "rule_type": "DOMAIN", "value": suffix + ".example.com", "policy": "DIRECT"} {
					if added.Fields.Set(key, must(model.Marshal(value))(t)) != nil {
						t.Fatal("rule field set failed")
					}
				}
				records = append(records, added)
			}
			d.Profiles[0].RuleConfigs = roundTripDecode[[]model.RuleConfig](t, records)
		})
		commit("rules reorder", []modelRowKey{profileKey("rule_configs")}, func(d *model.Document) {
			records := must(roundTripList(d.Profiles[0].RuleConfigs))(t)
			records[len(records)-1], records[len(records)-2] = records[len(records)-2], records[len(records)-1]
			d.Profiles[0].RuleConfigs = roundTripDecode[[]model.RuleConfig](t, records)
		})
		// R5 for each skeleton record type: wrong description type and bool revisions
		// are retained; current legacy validation intentionally permits these cases.
		commit("profile R5", []modelRowKey{profileKey("description")}, func(d *model.Document) { d.Profiles[0].Description = "will replace via raw fixture" })
		// Inject raw wrong types through the store only, then exercise unrelated typed edits.
		raw := entry.Document
		legacy := must(decodeRows(raw))(t)
		legacy.GetObject("profiles").GetObject(owner).Set("description", jsonx.Obj("password", "synthetic-never-log"))
		legacy.GetObject("system").Set("_revision", true)
		legacy.GetObject("shared").Set("_revision", true)
		injected := must(encodeRows(legacy, raw))(t)
		var written bool
		entry, written, err = target.Commit(entry, injected, nil)
		if err != nil || !written {
			t.Fatal("R5 injection failed")
		}
		fallback, issues, err := decodeModelRows(entry.Document)
		if err != nil || len(issues) != 3 || fallback.System.Revision != 0 || fallback.Shared.Revision != 0 || fallback.Profiles[0].Description != "" {
			t.Fatal("R5 skeleton fallback failed")
		}
		assertModelRows(t, must(encodeModelRows(fallback, entry.Document))(t), entry.Document)
		commit("R5 unrelated edit", []modelRowKey{profileKey("name")}, func(d *model.Document) { d.Profiles[0].Name = "N8 R5 retained" })
		audit("audit-edit")
		afterPython, err := target.Load()
		if err != nil {
			t.Fatal("Go read after Python edit failed")
		}
		if pythonReady {
			if afterPython.Head.Generation != entry.Head.Generation+1 {
				t.Fatal("Python edit generation differs")
			}
			entry = afterPython
			audit("audit-final")
		}
		reloaded, _, err := decodeModelRows(afterPython.Document)
		if err != nil {
			t.Fatal("model read after Python edit failed")
		}
		assertModelRows(t, must(encodeModelRows(reloaded, afterPython.Document))(t), afterPython.Document)
		t.Logf("rows=%d profiles=%d: byte/no-op, typed edits, unknowns/R5 and Python↔Go passed", len(before.Root)+len(before.System.Fields)+len(before.Shared.Fields)+func() int {
			n := 0
			for _, p := range before.Profiles {
				n += len(p.Fields)
			}
			return n
		}(), len(before.Profiles))
	}
	t.Run("committed-rows", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), store.Filename)
		target := store.New(path, LockTimeout)
		if _, err := target.Create(modelRowFixture(t), nil); err != nil {
			t.Fatal("fixture create failed")
		}
		run(t, path, false)
	})
	t.Run("full-go-repository", func(t *testing.T) { r := newRepo(t); run(t, r.DatabasePath(), true) })
	if pythonReady {
		t.Run("python-written-rows", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), store.Filename)
			python(t, "fixture", path, "../store/testdata/raw_rows.json")
			run(t, path, false)
		})
	}
	if snapshot := os.Getenv("CONFIGFLOW_SNAPSHOT"); snapshot != "" {
		if !pythonReady {
			t.Fatal("snapshot audit requires CONFIGFLOW_PY and CONFIGFLOW_REPO")
		}
		t.Run("private-snapshot", func(t *testing.T) {
			home, err := os.UserHomeDir()
			if err != nil {
				t.Fatal("private home unavailable")
			}
			runs := filepath.Join(home, "cf-prod-snapshot", "runs")
			if os.MkdirAll(runs, 0o700) != nil {
				t.Fatal("private runs unavailable")
			}
			directory, err := os.MkdirTemp(runs, "n8-3-roundtrip-")
			if err != nil {
				t.Fatal("private copy create failed")
			}
			t.Cleanup(func() {
				if os.RemoveAll(directory) != nil {
					t.Error("private copy cleanup failed")
				}
			})
			path := filepath.Join(directory, store.Filename)
			python(t, "copy", filepath.Join(snapshot, store.Filename), path)
			run(t, path, true)
		})
	}
}

func roundTripDecode[T any](t *testing.T, value any) T {
	t.Helper()
	var result T
	data, err := model.Marshal(value)
	if err != nil {
		t.Fatal("typed fixture encode failed")
	}
	if json.Unmarshal(data, &result) != nil {
		t.Fatal("typed fixture decode failed")
	}
	return result
}
