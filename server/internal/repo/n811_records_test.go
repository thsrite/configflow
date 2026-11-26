package repo_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func recordRepository(t *testing.T) *repo.Repository {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func exportRecord(t *testing.T, r *repo.Repository) json.RawMessage {
	t.Helper()
	raw, err := r.ExportAllJSON(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestN811ImportExportOpaqueBytes(t *testing.T) {
	r := recordRepository(t)
	raw := exportRecord(t, r)
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	opaque := json.RawMessage(`{ "large": 900719925474099312345678901234567890, "decimal": 1.230000e+04, "escape": "\u4e2d", "future": [null, { "x": true }] }`)
	// Undeclared root and settings members are dropped (O18 T1a, T1c).
	if err := fields.Set("future_extension", opaque); err != nil {
		t.Fatal(err)
	}
	systemRaw, _ := fields.Get("system")
	var system, settings model.Fields
	if err := system.UnmarshalJSON(systemRaw); err != nil {
		t.Fatal(err)
	}
	settingsRaw, _ := system.Get("system_config")
	if err := settings.UnmarshalJSON(settingsRaw); err != nil {
		t.Fatal(err)
	}
	if err := settings.Set("future_extension", opaque); err != nil {
		t.Fatal(err)
	}
	settingsRaw, _ = settings.MarshalJSON()
	if err := system.Set("system_config", settingsRaw); err != nil {
		t.Fatal(err)
	}
	systemRaw, _ = system.MarshalJSON()
	if err := fields.Set("system", systemRaw); err != nil {
		t.Fatal(err)
	}
	raw, err := fields.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.ImportAllJSON(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	var exported model.Fields
	if err := exported.UnmarshalJSON(exportRecord(t, r)); err != nil {
		t.Fatal(err)
	}
	if exported.Has("future_extension") {
		t.Fatal("undeclared root member kept")
	}
	exportedSystem, _ := exported.Get("system")
	if err := system.UnmarshalJSON(exportedSystem); err != nil {
		t.Fatal(err)
	}
	settingsRaw, _ = system.Get("system_config")
	if err := settings.UnmarshalJSON(settingsRaw); err != nil {
		t.Fatal(err)
	}
	// The settings record is a plain record since O18 T1c: its undeclared
	// members are dropped like the root's.
	if actual, present := settings.Get("future_extension"); present {
		t.Fatalf("undeclared settings member kept: %s", actual)
	}
}

func hydrateFixture(t *testing.T, r *repo.Repository) {
	t.Helper()
	ctx := context.Background()
	var item model.RuleLibraryItem
	if err := json.Unmarshal([]byte(`{"id":"source","name":"Source","source_type":"content","content":"example.com\n","behavior":"domain","format":"text","enabled":true}`), &item); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		shared.RuleLibrary = append(shared.RuleLibrary, item)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "fixture-agent", Name: "Fixture Agent", ProfileID: "default", Token: "synthetic-agent-token"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, profile *model.Profile) error {
		profile.RuleConfigs = []model.RuleConfig{{ID: "rule", ItemType: "ruleset", LibraryRuleID: "source"}}
		return markerSet(&profile.Loon.CustomConfig, []byte(`{ "large": 90071992547409931234567890 }`))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestN811ResolvedConfigWrites(t *testing.T) {
	for _, sharedPath := range []bool{false, true} {
		name := "profile"
		if sharedPath {
			name = "shared"
		}
		t.Run(name, func(t *testing.T) {
			r := recordRepository(t)
			hydrateFixture(t, r)
			ctx := context.Background()
			before, err := r.Profile(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			opaque, _ := markerGet(json.RawMessage(before.Loon.CustomConfig))
			check := func(config *model.Config) {
				t.Helper()
				if len(config.RuleLibrary) != 1 || config.Profile.RuleConfigs[0].Name != "Source" || config.Profile.RuleConfigs[0].Content == nil || config.SystemConfig.RuleProxyToken == "" || !config.HasAgents || len(config.Agents) != 1 || config.Agents[0].ID != "fixture-agent" || config.Agents[0].ProfileID != "default" {
					t.Fatal("resolved view was not hydrated")
				}
			}
			edit := func(_ context.Context, config *model.Config) error {
				check(config)
				config.Profile.Mihomo.CustomConfig = "mixed-port: 17890"
				return nil
			}
			if sharedPath {
				err = r.UpdateSharedConfigs(ctx, func(_ context.Context, shared *model.Shared) error {
					shared.RuleLibrary[0].Name = "Renamed"
					return nil
				}, func(_ context.Context, id string, before, after *model.Config) error {
					check(before)
					if after.Profile.RuleConfigs[0].Name != "Renamed" {
						t.Fatal("after did not resolve edited shared catalog")
					}
					after.Profile.Mihomo.CustomConfig = "mixed-port: 17890"
					return nil
				}, func(*model.Shared, []*model.Profile) ([]string, error) { return []string{"default", "default"}, nil })
			} else {
				err = r.UpdateConfig(ctx, "default", edit)
			}
			if err != nil {
				t.Fatal(err)
			}
			after, err := r.Profile(ctx, "default")
			if err != nil {
				t.Fatal(err)
			}
			retained, _ := markerGet(json.RawMessage(after.Loon.CustomConfig))
			if !bytes.Equal(opaque, retained) {
				t.Fatal("untouched opaque member changed")
			}
			if after.RuleConfigs[0].Name != "" || after.RuleConfigs[0].Content != nil {
				t.Fatal("hydrated source leaked into stored rule")
			}
			if after.Revision != before.Revision+1 {
				t.Fatal("selected profile updated more than once")
			}
		})
	}
}

func TestN811ResolvedWritesLifetimeAndRollback(t *testing.T) {
	for _, sharedPath := range []bool{false, true} {
		for _, mode := range []string{"failure", "shutdown", "disconnect"} {
			name := mode
			if sharedPath {
				name = "shared/" + mode
			}
			t.Run(name, func(t *testing.T) {
				r := recordRepository(t)
				hydrateFixture(t, r)
				ctx := context.Background()
				before := exportRecord(t, r)
				runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
				runtime.Start(ctx)
				defer runtime.Shutdown(ctx)
				request, disconnect := context.WithCancel(runtime.BindRequest(ctx))
				defer disconnect()
				failure := errors.New("synthetic editor failure")
				edit := func(_ context.Context, config *model.Config) error {
					config.Profile.Mihomo.CustomConfig = "port: 17890"
					switch mode {
					case "failure":
						return failure
					case "shutdown":
						runtime.Shutdown(ctx)
					case "disconnect":
						disconnect()
					}
					return nil
				}
				var err error
				if sharedPath {
					err = r.UpdateSharedConfigs(request, func(_ context.Context, shared *model.Shared) error {
						shared.RuleLibrary[0].Name = "Changed"
						return nil
					}, func(ctx context.Context, _ string, _, after *model.Config) error { return edit(ctx, after) }, func(*model.Shared, []*model.Profile) ([]string, error) { return []string{"default"}, nil })
				} else {
					err = r.UpdateConfig(request, "default", edit)
				}
				after := exportRecord(t, r)
				switch mode {
				case "failure":
					if !errors.Is(err, failure) || !bytes.Equal(before, after) {
						t.Fatal("editor failure committed", err)
					}
				case "shutdown":
					if !errors.Is(err, context.Canceled) || !bytes.Equal(before, after) {
						t.Fatal("shutdown committed", err)
					}
				case "disconnect":
					if err != nil || bytes.Equal(before, after) {
						t.Fatal("disconnected request did not commit", err)
					}
				}
			})
		}
	}
}

func TestN811SharedConfigPreservesConcurrentEdits(t *testing.T) {
	r := recordRepository(t)
	base := context.Background()
	if err := r.UpdateShared(base, func(_ context.Context, shared *model.Shared) error {
		shared.Nodes = []model.Node{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	first, second := repo.WithScope(base), repo.WithScope(base)
	left, err := r.ScopedSharedConfig(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := r.ScopedSharedConfig(second)
	if err != nil {
		t.Fatal(err)
	}
	left.Nodes[0].Name = "Edited A"
	right.Nodes[1].Name = "Edited B"
	if err := r.SaveScopedSharedRecord(first, left, nil); err != nil {
		t.Fatal(err)
	}
	if err := r.SaveScopedSharedRecord(second, right, nil); err != nil {
		t.Fatal(err)
	}
	result, err := r.Shared(base)
	if err != nil {
		t.Fatal(err)
	}
	if result.Nodes[0].Name != "Edited A" || result.Nodes[1].Name != "Edited B" {
		t.Fatalf("concurrent edits lost: %+v", result.Nodes)
	}
	if err := r.SaveScopedSharedRecord(second, right, nil); !errors.Is(err, repo.ErrInUse) {
		t.Fatalf("successful save did not reset the Scope baseline: %v", err)
	}
}

func TestN811ResourceUsageRecords(t *testing.T) {
	r := recordRepository(t)
	ctx := context.Background()
	if err := r.UpdateShared(ctx, func(_ context.Context, s *model.Shared) error {
		s.Nodes = []model.Node{{ID: "node", Name: "Node"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		ids := []string{"node"}
		p.ProxyGroups = []model.ProxyGroup{{ID: "group", Name: "Group", Type: "select", ManualNodes: &ids}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	records, err := r.ResourceUsage(ctx, "nodes", "node")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Kind != "profile" || (records[0].ProfileID == nil || *records[0].ProfileID != "default") {
		t.Fatalf("unexpected usages: %+v", records)
	}
	if _, err := r.ResourceUsage(ctx, "unknown", "node"); !errors.Is(err, repo.ErrValidation) {
		t.Fatalf("unknown kind accepted: %v", err)
	}
}

func TestN811ExplicitMetadataRepairsNull(t *testing.T) {
	r := recordRepository(t)
	ctx := context.Background()
	if err := r.Update(ctx, func(_ context.Context, d *model.Document) error {
		if err := d.Profiles[0].SetJSON("description", []byte(`null`)); err != nil {
			return err
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Input of the wrong type is refused where it is set (O18 T1a).
	err := r.Update(ctx, func(_ context.Context, d *model.Document) error {
		return d.Profiles[0].SetJSON("_revision", []byte(`true`))
	})
	if !errors.Is(err, repo.ErrValidation) || err.Error() != "配置修订号无效" {
		t.Fatalf("wrong-typed revision: %v", err)
	}
	if _, err := r.CloneProfileRecord(ctx, "default", repo.ProfileCreation{ID: "clone", Name: "Clone"}); err != nil {
		t.Fatal(err)
	}
	profile, err := r.Profile(ctx, "clone")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := model.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	var fields model.Fields
	if err := fields.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	description, _ := fields.Get("description")
	revision, _ := fields.Get("_revision")
	if string(description) != `""` || string(revision) != "0" {
		t.Fatalf("clone metadata did not repair original values: %s", raw)
	}
	empty := ""
	if _, err := r.UpdateProfileMetadataRecord(ctx, "default", repo.ProfileMetadataUpdate{Description: &empty}); err != nil {
		t.Fatal(err)
	}
	profile, err = r.Profile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	raw, err = model.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := fields.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	description, _ = fields.Get("description")
	if string(description) != `""` {
		t.Fatalf("explicit empty description did not repair null: %s", description)
	}
}

func TestN811Schema2CollidingLibraryIDsKeepLocalReferences(t *testing.T) {
	r := recordRepository(t)
	raw := json.RawMessage(`{"schema_version":2,"system":{"schema_version":2,"profiles":[{"id":"default","name":"First"},{"id":"second","name":"Second"}]},"profiles":{"default":{"rule_library":[{"id":"library","name":"Source","url":"https://one.example/rules"}],"rule_configs":[{"id":"rule","itemType":"ruleset","library_rule_id":"library","policy":"DIRECT"}]},"second":{"rule_library":[{"id":"library","name":"Source","url":"https://two.example/rules"}],"rule_configs":[{"id":"rule","itemType":"ruleset","library_rule_id":"library","policy":"DIRECT"}]}}}`)
	if _, err := r.ImportAllJSON(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ id, url string }{{"default", "https://one.example/rules"}, {"second", "https://two.example/rules"}} {
		config, err := r.Config(context.Background(), tc.id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(config.Profile.RuleConfigs) != 1 || config.Profile.RuleConfigs[0].URL == nil || *config.Profile.RuleConfigs[0].URL != tc.url {
			t.Fatalf("profile %s lost local source: %+v", tc.id, config.Profile.RuleConfigs)
		}
	}
}
