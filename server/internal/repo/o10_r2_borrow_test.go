package repo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
)

// O10-R2: borrowed Config/Profile/Shared/SystemConfig/Backup reads.

// r2BorrowRepo has every section a borrowed read lends: shared resources
// selected by a group, a hydrated ruleset, a router, settings and backup.
func r2BorrowRepo(t *testing.T) *Repository {
	t.Helper()
	r := newRepo(t)
	noError(t, r.UpdateShared(bg, func(_ context.Context, shared *model.Shared) error {
		for member, value := range map[string]string{
			"subscriptions": `[{"id": "s", "name": "Sub", "url": "https://sub.example.com/a"}]`,
			"nodes": `[{"id": "n", "name": "Node", "subscription_id": "s"},
				{"id": "n2", "name": "Spare node A"}, {"id": "n3", "name": "Spare node B"}]`,
			"subscription_aggregations": `[{"id": "a", "name": "Agg", "nodes": ["n"]}]`,
			"rule_library":              `[{"id": "r", "name": "Rule", "url": "https://rules.example.com/r.list"}]`,
		} {
			if err := shared.SetJSON(member, json.RawMessage(value)); err != nil {
				return err
			}
		}
		return nil
	}))
	_, _, err := r.SaveProfileFieldsJSON(bg, "default", json.RawMessage(`{
		"proxy_groups": [{"id": "g", "name": "Group", "type": "select", "manual_nodes": ["n"], "aggregations": ["a"]},
			{"id": "g2", "name": "Spare group A", "type": "select", "manual_nodes": ["n2"]},
			{"id": "g3", "name": "Spare group B", "type": "select", "manual_nodes": ["n3"]}],
		"rule_configs": [{"id": "rule", "itemType": "ruleset", "library_rule_id": "r", "policy": "g"},
			{"id": "final", "itemType": "rule", "rule_type": "MATCH", "policy": "DIRECT"}],
		"routers": {"items": [{"id": "rt", "name": "Router", "type": "routeros", "host": "192.0.2.1"}]}}`), nil)
	noError(t, err)
	noError(t, r.UpdateSystem(bg, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = "https://panel.example.com"
		defaultID := "default"
		system.SystemConfig.DefaultProfileID = &defaultID
		system.SystemConfig.NodeTestDNS = []string{"192.0.2.53", "198.51.100.53", "192.0.2.54"}
		path := "/backup"
		system.Backup.WebDAVPath = &path
		return nil
	}))
	return r
}

// r2BorrowReads are the migrated reads; each returns the borrowed value.
var r2BorrowReads = []struct {
	name string
	read func(context.Context, *Repository) (any, error)
}{
	{"Config", func(ctx context.Context, r *Repository) (any, error) { return r.Config(ctx, "default", nil) }},
	{"ConfigRoots", func(ctx context.Context, r *Repository) (any, error) {
		return r.Config(ctx, "default", ResourceRoots{})
	}},
	{"ConfigWithOxiDNSAgents", func(ctx context.Context, r *Repository) (any, error) {
		return r.ConfigWithOxiDNSAgents(ctx, "default", nil)
	}},
	{"ConfigFrom", func(ctx context.Context, r *Repository) (any, error) {
		// ConfigFrom borrows the View document: encode it in the callback.
		var encoded json.RawMessage
		err := r.View(ctx, func(document *model.Document) error {
			config, err := ConfigFrom(document, "default", nil)
			if err == nil {
				encoded, err = model.Marshal(config)
			}
			return err
		})
		return encoded, err
	}},
	{"Profile", func(ctx context.Context, r *Repository) (any, error) { return r.Profile(ctx, "default") }},
	{"Shared", func(ctx context.Context, r *Repository) (any, error) { return r.Shared(ctx) }},
	{"SystemConfig", func(ctx context.Context, r *Repository) (any, error) { return r.SystemConfig(ctx) }},
	{"Backup", func(ctx context.Context, r *Repository) (any, error) { return r.Backup(ctx) }},
}

// r2Commit changes every section a borrowed read lends.
func r2Commit(t testing.TB, r *Repository, round int) {
	t.Helper()
	noError(t, r.Update(bg, func(_ context.Context, document *model.Document) error {
		name := fmt.Sprintf("Node %d", round)
		document.Shared.Nodes[0].Name = name
		document.Shared.RuleLibrary[0].Name = name
		document.Shared.Revision++
		profile := profileByKey(document, "default")
		profile.ProxyGroups[0].Name = fmt.Sprintf("Group %d", round)
		profile.Routers.Items[0].Name = fmt.Sprintf("Router %d", round)
		profile.Revision++
		document.System.SystemConfig.ServerDomain = fmt.Sprintf("https://panel%d.example.com", round)
		path := fmt.Sprintf("/backup/%d", round)
		document.System.Backup.WebDAVPath = &path
		document.System.Revision++
		return nil
	}))
}

// TestO10R2BorrowedReadsRaceWithCommits runs concurrent readers that encode
// every member of what they borrow while a writer commits in a loop; after
// each commit the writer checks that the value it borrowed before is
// unchanged and that a new read sees the commit. Run with -race -count=20.
func TestO10R2BorrowedReadsRaceWithCommits(t *testing.T) {
	for _, tt := range r2BorrowReads {
		t.Run(tt.name, func(t *testing.T) {
			r := r2BorrowRepo(t)
			var tripped atomic.Int64
			r.SetViewGuard(func() { tripped.Add(1) })
			defer func() {
				if tripped.Load() > 0 || r.CheckBorrowed() != nil {
					t.Error("a borrowed snapshot was modified")
				}
			}()
			const readers, rounds = 4, 15
			var stop atomic.Bool
			var reads atomic.Int64
			var wg sync.WaitGroup
			for range readers {
				wg.Go(func() {
					for !stop.Load() {
						value, err := tt.read(bg, r)
						if err != nil {
							t.Errorf("read: %v", err)
							return
						}
						if _, err := model.Marshal(value); err != nil {
							t.Errorf("encode borrowed value: %v", err)
							return
						}
						reads.Add(1)
					}
				})
			}
			for round := range rounds {
				old := must(tt.read(bg, r))(t)
				before := mustMarshalJSON(t, old)
				r2Commit(t, r, round)
				if after := mustMarshalJSON(t, old); !bytes.Equal(after, before) {
					t.Fatalf("round %d: a commit changed a borrowed value:\nbefore %s\nafter  %s", round, before, after)
				}
				if current := mustMarshalJSON(t, must(tt.read(bg, r))(t)); bytes.Equal(current, before) {
					t.Fatalf("round %d: the read after a commit returned the old generation", round)
				}
			}
			stop.Store(true)
			wg.Wait()
			if reads.Load() == 0 {
				t.Fatal("no concurrent reads ran")
			}
		})
	}
}

// TestO10R2BorrowedReadsShareTheSnapshot pins that borrowed reads no longer
// deep-copy: the records they return reference the committed snapshot.
func TestO10R2BorrowedReadsShareTheSnapshot(t *testing.T) {
	r := r2BorrowRepo(t)
	entry := must(r.readEntry(bg, false))(t)
	stored := profileByKey(entry.Model, "default")
	config := must(r.Config(bg, "default", nil))(t)
	if &config.Profile.ProxyGroups[0] != &stored.ProxyGroups[0] {
		t.Fatal("Config copied the profile's proxy groups")
	}
	if &config.RuleLibrary[0] != &entry.Model.Shared.RuleLibrary[0] {
		t.Fatal("Config copied the rule library")
	}
	if &config.Profile.RuleConfigs[0] == &stored.RuleConfigs[0] {
		t.Fatal("Config hydrated rulesets into the snapshot's rule_configs")
	}
	if got := config.Profile.RuleConfigs[0].URL; got == nil || *got != "https://rules.example.com/r.list" {
		t.Fatalf("ruleset not hydrated: %v", got)
	}
	if stored.RuleConfigs[0].URL != nil {
		t.Fatal("hydration wrote into the stored ruleset")
	}
	shared := must(r.Shared(bg))(t)
	if &shared.Nodes[0] != &entry.Model.Shared.Nodes[0] || shared == &entry.Model.Shared {
		t.Fatal("Shared must lend a shallow copy that shares the stored nodes")
	}
	profile := must(r.Profile(bg, "default"))(t)
	if profile == stored || &profile.ProxyGroups[0] != &stored.ProxyGroups[0] {
		t.Fatal("Profile must lend a shallow copy that shares the stored groups")
	}
	// Explicit clones and writers' reads stay deep copies.
	if clone := must(r.CloneShared(bg))(t); &clone.Nodes[0] == &entry.Model.Shared.Nodes[0] {
		t.Fatal("CloneShared shares the snapshot")
	}
	noError(t, r.Update(bg, func(ctx context.Context, document *model.Document) error {
		inside := must(r.Config(ctx, "default", nil))(t)
		working := profileByKey(document, "default")
		if &inside.Profile.ProxyGroups[0] == &working.ProxyGroups[0] {
			t.Error("Config inside a write transaction shares the working copy")
		}
		return nil
	}))
	published, _, err := r.PublishConfig(bg, "default", false)
	noError(t, err)
	current := profileByKey(must(r.readEntry(bg, false))(t).Model, "default")
	if &published.Profile.ProxyGroups[0] == &current.ProxyGroups[0] {
		t.Fatal("PublishConfig shares the snapshot")
	}
}

// TestO10R2ViewGuardCatchesModifiedBorrowedValues is the negative control:
// changing what a borrowed read shares trips the guard on the next read, and
// top-level changes to the lent value do not.
func TestO10R2ViewGuardCatchesModifiedBorrowedValues(t *testing.T) {
	tests := []struct {
		name   string
		modify func(t *testing.T, r *Repository)
		trips  bool
	}{
		{"Config group", func(t *testing.T, r *Repository) {
			must(r.Config(bg, "default", nil))(t).Profile.ProxyGroups[0].Name = "Changed"
		}, true},
		{"Config rule library", func(t *testing.T, r *Repository) {
			must(r.Config(bg, "default", nil))(t).RuleLibrary[0].Name = "Changed"
		}, true},
		{"Config router", func(t *testing.T, r *Repository) {
			noError(t, must(r.ConfigWithOxiDNSAgents(bg, "default", nil))(t).Profile.Routers.Items[0].SetJSON("name", json.RawMessage(`"Changed"`)))
		}, true},
		{"Profile group pointer", func(t *testing.T, r *Repository) {
			*must(r.Profile(bg, "default"))(t).ProxyGroups[0].ManualNodes = []string{"x"}
		}, true},
		{"Shared node", func(t *testing.T, r *Repository) {
			must(r.Shared(bg))(t).Nodes[0].Name = "Changed"
		}, true},
		{"SystemConfig pointer", func(t *testing.T, r *Repository) {
			*must(r.SystemConfig(bg))(t).DefaultProfileID = "changed"
		}, true},
		{"SystemConfig slice element", func(t *testing.T, r *Repository) {
			must(r.SystemConfig(bg))(t).NodeTestDNS[0] = "192.0.2.99"
		}, true},
		{"Config settings slice element", func(t *testing.T, r *Repository) {
			must(r.Config(bg, "default", nil))(t).SystemConfig.NodeTestDNS[0] = "192.0.2.99"
		}, true},
		{"Backup pointer", func(t *testing.T, r *Repository) {
			*must(r.Backup(bg))(t).WebDAVPath = "/changed"
		}, true},
		{"top-level changes stay local", func(t *testing.T, r *Repository) {
			config := must(r.Config(bg, "default", nil))(t)
			config.Profile.Name = "Changed"
			config.Nodes[0].Name = "Changed" // the Config's own resource slice
			config.Profile.ProxyGroups = append(config.Profile.ProxyGroups, model.ProxyGroup{ID: "new"})
			config.SystemConfig.ServerDomain = "https://changed.example.com"
			config.Profile.RuleConfigs = append([]model.RuleConfig(nil), config.Profile.RuleConfigs...)
			config.Profile.RuleConfigs[0].Remark = "custom"
			shared := must(r.Shared(bg))(t)
			shared.Nodes = append(shared.Nodes, model.Node{ID: "new"})
			noError(t, shared.SetJSON("nodes", json.RawMessage(`[]`)))
			backup := must(r.Backup(bg))(t)
			backup.WebDAVPath = nil
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := r2BorrowRepo(t)
			var tripped atomic.Int64
			r.SetViewGuard(func() { tripped.Add(1) })
			before := must(r.ExportAllJSON(bg, false))(t)
			tt.modify(t, r)
			_ = must(r.Shared(bg))(t) // the next borrowing read re-checks
			if got := tripped.Load() > 0; got != tt.trips {
				t.Fatalf("guard tripped = %v, want %v", got, tt.trips)
			}
			if !tt.trips {
				wantJSONEqual(t, must(r.ExportAllJSON(bg, false))(t), before)
			}
			// Deliberate mutations: forget the remembered generations.
			r.SetViewGuard(nil)
		})
	}
}

// TestO10R2ValidationDoesNotModifyTheDocument pins that write/load validation,
// which resolves every profile borrowing the document, leaves it unchanged.
func TestO10R2ValidationDoesNotModifyTheDocument(t *testing.T) {
	r := r2BorrowRepo(t)
	document := must(r.readEntry(bg, false))(t).Model
	before := mustMarshalJSON(t, document)
	noError(t, validateStored(document))
	if after := mustMarshalJSON(t, document); !bytes.Equal(after, before) {
		t.Fatalf("validation changed the document:\n%s\n%s", before, after)
	}
}

// r2SliceFields returns the slices of *value reachable without following a
// pointer, slice element or map: its exported slice fields and those of its
// exported struct-valued fields, recursively.
func r2SliceFields(value any) []reflect.Value {
	var out []reflect.Value
	var walk func(reflect.Value)
	walk = func(v reflect.Value) {
		for i := range v.NumField() {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			switch field := v.Field(i); field.Kind() {
			case reflect.Slice:
				out = append(out, field)
			case reflect.Struct:
				walk(field)
			}
		}
	}
	walk(reflect.ValueOf(value).Elem())
	return out
}

// TestO10R2SnapshotSlicesHaveNoSpareCapacity pins the invariant that makes
// appending to a lent record's slices safe: the model decoder allocates every
// slice of the snapshot (also inside struct-valued members, e.g.
// Profile.Routers.Items, and inside slice elements) with capacity equal to its
// length, so an append by one borrower always reallocates and never writes a
// backing array another borrower reads. If a decoder change breaks this, lend
// must clip the slices it hands out.
func TestO10R2SnapshotSlicesHaveNoSpareCapacity(t *testing.T) {
	r := r2BorrowRepo(t)
	document := must(r.readEntry(bg, false))(t).Model
	stored := profileByKey(document, "default")
	values := map[string]any{
		"Shared":       &document.Shared,
		"Profile":      stored,
		"SystemConfig": &document.System.SystemConfig,
		"Backup":       &document.System.Backup,
		"Aggregation":  &document.Shared.Aggregations[0],
		"Router":       &stored.Routers.Items[0],
	}
	checked := 0
	for name, value := range values {
		for _, field := range r2SliceFields(value) {
			if field.Len() > 0 {
				checked++
			}
			if field.Cap() != field.Len() {
				t.Errorf("%s: a snapshot slice has spare capacity (len %d, cap %d)", name, field.Len(), field.Cap())
			}
		}
	}
	for _, group := range stored.ProxyGroups {
		if group.ManualNodes != nil && cap(*group.ManualNodes) != len(*group.ManualNodes) {
			t.Errorf("group %s: manual_nodes has spare capacity", group.ID)
		}
	}
	if checked < 6 {
		t.Fatalf("only %d non-empty snapshot slices checked; the fixture does not exercise the invariant", checked)
	}
}

// TestO10R2ConcurrentAppendsToLentSlices runs borrowers that append to the
// slices of their shallow copies concurrently (the -race run detects a
// shared backing array) and checks that each sees only its own element.
func TestO10R2ConcurrentAppendsToLentSlices(t *testing.T) {
	r := r2BorrowRepo(t)
	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Go(func() {
			id := fmt.Sprintf("worker-%d", worker)
			for range 50 {
				shared, err := r.Shared(bg)
				if err != nil {
					t.Error(err)
					return
				}
				nodes := append(shared.Nodes, model.Node{ID: id})
				profile, err := r.Profile(bg, "default")
				if err != nil {
					t.Error(err)
					return
				}
				groups := append(profile.ProxyGroups, model.ProxyGroup{ID: id})
				config, err := r.Config(bg, "default", nil)
				if err != nil {
					t.Error(err)
					return
				}
				dns := append(config.SystemConfig.NodeTestDNS, id)
				library := append(config.RuleLibrary, model.RuleLibraryItem{ID: id})
				members := append(config.Aggregations[0].Nodes, id)
				if nodes[len(nodes)-1].ID != id || groups[len(groups)-1].ID != id || dns[len(dns)-1] != id ||
					library[len(library)-1].ID != id || members[len(members)-1] != id {
					t.Errorf("%s: another borrower's append is visible", id)
					return
				}
			}
		})
	}
	wg.Wait()
}
