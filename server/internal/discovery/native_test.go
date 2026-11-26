package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"

	"configflow/internal/model"
)

// TestNativeStoredIdentifierTypes (O18 T1c): rule library items, rules and
// proxy groups are plain records, so a stored identifier of another JSON type
// reads leniently (a number as its text, anything else as empty, with a load
// warning) and no longer fails the read with the Go-only "必须是字符串" message
// (the R5 original-value evidence is gone; the stored-string guard was removed).
func TestNativeStoredIdentifierTypes(t *testing.T) {
	t.Parallel()
	tests := []struct{ resource, field, source string }{
		{"规则库", "id", `{"rule_library":[{"id":false}]}`},
		{"规则库", "name", `{"rule_library":[{"id":"lib","name":[]}]}`},
		{"规则库", "source_type", `{"rule_library":[{"id":"lib","source_type":{}}]}`},
		{"规则库", "behavior", `{"rule_library":[{"id":"lib","behavior":true}]}`},
		{"规则库", "content", `{"rule_library":[{"id":"lib","content":false}]}`},
		{"规则", "id", `{"rule_configs":[{"id":false}]}`},
		{"规则", "itemType", `{"rule_configs":[{"id":"rule","itemType":[]}]}`},
		{"规则", "rule_type", `{"rule_configs":[{"id":"rule","rule_type":false}]}`},
		{"规则", "value", `{"rule_configs":[{"id":"rule","value":{}}]}`},
		{"规则", "library_rule_id", `{"rule_configs":[{"id":"rule","library_rule_id":false}]}`},
		{"规则", "policy", `{"rule_configs":[{"id":"rule","policy":true}]}`},
		{"策略组", "id", `{"proxy_groups":[{"id":true}]}`},
		{"策略组", "name", `{"proxy_groups":[{"id":"group","name":{}}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.resource+"/"+tc.field, func(t *testing.T) {
			var profile model.Profile
			var shared model.Shared
			if err := json.Unmarshal([]byte(tc.source), &profile); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.source), &shared); err != nil {
				t.Fatal(err)
			}
			if _, err := configFromModel(&model.Config{Profile: &profile, RuleLibrary: shared.RuleLibrary}); err != nil {
				t.Fatalf("lenient stored identifier rejected: %v", err)
			}
		})
	}
	// A number stored as a name reads as its text.
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"proxy_groups":[{"id":"g","name":7}]}`), &profile); err != nil {
		t.Fatal(err)
	}
	view, err := configFromModel(&model.Config{Profile: &profile})
	if err != nil || len(view.Groups) != 1 || view.Groups[0].Name != "7" {
		t.Fatalf("numeric identifier: %+v %v", view, err)
	}
	// A genuinely empty, absent or null field is not rejected either.
	for _, source := range []string{`{}`, `{"proxy_groups":[{"name":""}]}`, `{"rule_library":[{"name":null}]}`} {
		var profile model.Profile
		var shared model.Shared
		_ = json.Unmarshal([]byte(source), &profile)
		_ = json.Unmarshal([]byte(source), &shared)
		if _, err := configFromModel(&model.Config{Profile: &profile, RuleLibrary: shared.RuleLibrary}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNativeStoredR5AndSharedNumbers(t *testing.T) {
	t.Parallel()
	var settings Settings
	if err := json.Unmarshal([]byte(`{"auto_probe":1,"auto_apply":"false","region_check_enabled":[],"auto_apply_min_confidence":80.0}`), &settings); err != nil {
		t.Fatal(err)
	}
	// O18 T1c: a stored switch of another type keeps its truth value like
	// Python (auto_probe 1 is on), except auto_apply, which is switch:"off"
	// (a wrong type reads as off, fail closed); [] is off.
	if !settings.AutoProbe || settings.AutoApply || settings.RegionEnabled || settings.Confidence != 80 {
		t.Fatalf("lenient settings: %+v", settings)
	}
	var record model.Profile
	if err := json.Unmarshal([]byte(`{"proxy_groups":[{"id":"g","name":123.50}],"domain_discovery":{"region_probe_port":"18003"}}`), &record); err != nil {
		t.Fatal(err)
	}
	view, err := configFromModel(&model.Config{Profile: &record})
	if err != nil || view.Groups[0].Name != "123.50" || view.Settings.RegionPort != 18003 {
		t.Fatalf("shared coercion: %+v %v", view, err)
	}
}

// TestNativeRepositoryUpdatesDropUndeclaredMembers (O18 T1c; was
// TestNativeRepositoryUpdatesPreserveUnownedStoredMembers, which kept
// undeclared members byte for byte): DomainDiscovery is a plain record, so its
// undeclared members are dropped; a settings edit changes only that setting
// and keeps the history; a failed edit changes nothing.
func TestNativeRepositoryUpdatesDropUndeclaredMembers(t *testing.T) {
	svc, ctx := jobFixture(t)
	r := svc.Repo
	_, err := r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("domain_discovery", json.RawMessage(`{"history":[{"value":"example.com","future":{"n":123.50}}],"future":{"flag":false}}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
		t.Fatal(err)
	}
	p, err := r.Profile(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p.DomainDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if strings.Contains(string(data), "future") || !strings.Contains(string(fields["history"]), `"value":"example.com"`) || string(fields["auto_probe"]) != "true" {
		t.Fatalf("settings edit: %s", data)
	}
	sentinel := errors.New("discard")
	before, _ := json.Marshal(p)
	err = svc.editProfile(ctx, "default", func(c *config) error { c.Settings.AutoApply = true; return sentinel })
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	p, _ = r.Profile(ctx, "default")
	after, _ := json.Marshal(p)
	if string(before) != string(after) {
		t.Fatal("failed edit changed profile")
	}
}

func TestNativeCollaboratorsInstalled(t *testing.T) {
	t.Parallel()
	if New(Options{}).agentClient() == nil {
		t.Fatal("missing Agent client")
	}
}

func TestNativeCoreBoundary(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"dictGet": true, "raise": true, "catch": true, "must": true, "pyError": true}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.HasSuffix(imp.Path.Value, `/jsonx"`) || strings.HasSuffix(imp.Path.Value, `/pystr"`) {
				t.Errorf("%s imports legacy values", name)
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && forbidden[id.Name] {
					t.Errorf("%s calls legacy %s", name, id.Name)
				}
			case *ast.FuncType:
				if n.Params != nil {
					for _, p := range n.Params.List {
						if id, ok := p.Type.(*ast.Ident); ok && id.Name == "any" {
							t.Errorf("%s has any parameter", name)
						}
					}
				}
			}
			return true
		})
	}
}

func FuzzNativeConfigProjection(f *testing.F) {
	for _, seed := range []string{`{}`, `{"rule_library":[{"id":"l","name":"Test","behavior":"domain"}]}`, `{"domain_discovery":{"auto_probe":1},"proxy_groups":[{"name":true}]}`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, source string) {
		var p model.Profile
		var shared model.Shared
		if json.Unmarshal([]byte(source), &p) != nil || json.Unmarshal([]byte(source), &shared) != nil {
			return
		}
		before, err := json.Marshal(p)
		if err != nil {
			return
		}
		_, _ = configFromModel(&model.Config{Profile: &p, RuleLibrary: shared.RuleLibrary})
		after, err := json.Marshal(p)
		if err != nil || string(before) != string(after) {
			t.Fatal("projection mutated model input")
		}
	})
}

func TestNativeRegistryRetainsHeartbeatOverlay(t *testing.T) {
	svc, ctx := jobFixture(t)
	repository := svc.Repo
	err := repository.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "runtime-agent", Name: "Synthetic", ProfileID: "default", ServiceType: "mihomo", DomainDiscoveryEnabled: new(true), LastHeartbeat: "2000-01-01T00:00:00Z", Version: "1.0.0"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manager := svc.Agents
	if err := manager.Runtime().RestoreHeartbeat(ctx, "runtime-agent", 4070908800, "2099-01-01T00:00:00Z", &model.Agent{Version: minRegionVersion}); err != nil {
		t.Fatal(err)
	}
	records, err := svc.profileAgents(ctx, "default")
	if err != nil || len(records) != 1 {
		t.Fatalf("registry: %+v %v", records, err)
	}
	if records[0].Heartbeat != "2099-01-01T00:00:00Z" || records[0].Version != minRegionVersion {
		t.Fatalf("lost runtime overlay: %+v", records[0])
	}
	stored, err := repository.System(ctx)
	if err != nil || stored.Agents[0].LastHeartbeat != "2000-01-01T00:00:00Z" {
		t.Fatal("test did not distinguish runtime and stored state")
	}
}
