package repo

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"configflow/internal/model"
	"configflow/internal/store"
)

// roundTripRecord is a test-only generic record over any stored list
// element: id, name and enabled are typed, every other member stays opaque
// in Fields (a counter of the wrong type reads as 0 and keeps its JSON).
type roundTripRecord struct {
	ID             string
	Name           string
	Enabled        *bool
	Counter        model.Int
	Fields         model.Fields
	counterInvalid bool
}

func (r *roundTripRecord) UnmarshalJSON(data []byte) error {
	var fields model.Fields
	if err := fields.UnmarshalJSON(data); err != nil {
		return err
	}
	out := roundTripRecord{}
	for _, member := range []struct {
		name   string
		target any
	}{{"id", &out.ID}, {"name", &out.Name}, {"enabled", &out.Enabled}, {"n8_counter", &out.Counter}} {
		raw, ok := fields.Get(member.name)
		if !ok {
			continue
		}
		if json.Unmarshal(raw, member.target) != nil {
			if member.name != "n8_counter" {
				return fmt.Errorf("record member %s", member.name)
			}
			out.counterInvalid = true
			continue
		}
		fields.Delete(member.name)
	}
	out.Fields = fields
	*r = out
	return nil
}

func (r roundTripRecord) MarshalJSON() ([]byte, error) {
	var fields model.Fields
	set := func(name string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		return fields.Set(name, data)
	}
	if err := set("id", r.ID); err != nil {
		return nil, err
	}
	if err := set("name", r.Name); err != nil {
		return nil, err
	}
	if r.Enabled != nil {
		if err := set("enabled", *r.Enabled); err != nil {
			return nil, err
		}
	}
	if r.Counter != 0 {
		if err := set("n8_counter", int64(r.Counter)); err != nil {
			return nil, err
		}
	}
	for name, value := range r.Fields.All() {
		if !fields.Has(name) {
			if err := fields.Set(name, value); err != nil {
				return nil, err
			}
		}
	}
	return fields.MarshalJSON()
}

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
	if r.Counter != 0 || !r.counterInvalid {
		t.Fatal("record fallback missing")
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

// TestStoredRoundTrip always exercises the committed row fixture and a full
// synthetic repository.
func TestStoredRoundTrip(t *testing.T) {
	run := func(t *testing.T, path string) {
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
		// The first Go write stores the rows in Go's encoding with the same
		// values (O18 T1a); after that a decode/encode round trip is a no-op.
		if next, changed, err := target.Commit(t.Context(), entry, rows, nil); err != nil {
			t.Fatal("normalizing commit failed")
		} else if changed {
			entry = next
		}
		assertModelRows(t, entry.Document, before)
		typed, _, err = decodeModelRows(entry.Document)
		if err != nil {
			t.Fatal("model decode after normalizing failed")
		}
		rows, err = encodeModelRows(typed, entry.Document)
		if err != nil {
			t.Fatal("model encode after normalizing failed")
		}
		fingerprint, err := target.Fingerprint()
		if err != nil {
			t.Fatal("initial fingerprint failed")
		}
		if _, changed, err := target.Commit(t.Context(), entry, rows, nil); err != nil || changed {
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
					t.Fatalf("%s changed an unrelated row %v", label, key)
				}
			}
			next, written, err := target.Commit(t.Context(), entry, rows, nil)
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

		}
		profileKey := func(name string) modelRowKey { return modelRowKey{"profile", owner, name} }
		marker := json.RawMessage(` { "z":1.0,"a":"\u4f8b <>&","nested": { "future": true } } `)
		// Plain records drop undeclared members (O18 T1c): a declared
		// backup member changes the row instead.
		_ = marker
		commit("backup member", []modelRowKey{{"system", "", "backup"}}, func(d *model.Document) {
			path := "/n8-round-trip/"
			d.System.Backup.WebDAVPath = &path
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
		// Wrong-typed stored members of each plain record read as zero values
		// with an issue each.
		commit("profile R5", []modelRowKey{profileKey("description")}, func(d *model.Document) { d.Profiles[0].Description = "will replace via raw fixture" })
		// Inject raw wrong types through the store only, then exercise unrelated typed edits.
		injected := cloneStoreDoc(t, entry.Document)
		for i := range injected.Profiles {
			if injected.Profiles[i].ID == owner {
				setStoreField(&injected.Profiles[i].Fields, "description", json.RawMessage(`{"password":"synthetic-never-log"}`))
			}
		}
		setStoreField(&injected.System.Fields, "_revision", json.RawMessage(`true`))
		setStoreField(&injected.Shared.Fields, "_revision", json.RawMessage(`true`))
		var written bool
		entry, written, err = target.Commit(t.Context(), entry, injected, nil)
		if err != nil || !written {
			t.Fatal("R5 injection failed")
		}
		fallback, issues, err := decodeModelRows(entry.Document)
		if err != nil || len(issues) != 3 || fallback.System.Revision != 0 || fallback.Shared.Revision != 0 || fallback.Profiles[0].Description != "" {
			t.Fatal("R5 skeleton fallback failed")
		}
		assertModelRows(t, must(encodeModelRows(fallback, entry.Document))(t), entry.Document)
		// encodeModelRows alone writes every row in Go's encoding, so the rows
		// read leniently are rewritten with their (zero) typed values here;
		// repository commits keep them (keepUnchangedRows).
		commit("R5 unrelated edit", []modelRowKey{profileKey("name"), profileKey("description"), {"system", "", "_revision"}, {"shared", "", "_revision"}},
			func(d *model.Document) { d.Profiles[0].Name = "N8 R5 repaired" })
		loaded, err := target.Load()
		if err != nil {
			t.Fatal(err)
		}
		reloaded, _, err := decodeModelRows(loaded.Document)
		if err != nil {
			t.Fatal(err)
		}
		assertModelRows(t, must(encodeModelRows(reloaded, loaded.Document))(t), loaded.Document)
		t.Logf("rows=%d profiles=%d: value round trip/no-op, typed edits, and lenient types passed", len(before.Root)+len(before.System.Fields)+len(before.Shared.Fields)+func() int {
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
		run(t, path)
	})
	t.Run("full-go-repository", func(t *testing.T) { r := newRepo(t); run(t, r.DatabasePath()) })
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
