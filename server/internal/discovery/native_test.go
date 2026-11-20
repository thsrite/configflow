package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
	"configflow/internal/repo"
)

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
			_, err := configFromModel(&model.Config{Profile: &profile, RuleLibrary: shared.RuleLibrary})
			if !errors.Is(err, repo.ErrValidation) || !strings.Contains(err.Error(), "的 "+tc.field+" 必须是字符串，当前值：") || !strings.HasPrefix(err.Error(), tc.resource+" ") {
				t.Fatalf("missing typed diagnostic: %v", err)
			}
		})
	}
	// A genuinely empty, absent or null field has no type issue and is not
	// rejected merely because its typed value is the zero value.
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
	if settings.AutoProbe || settings.AutoApply || settings.RegionEnabled || settings.Confidence != 80 {
		t.Fatalf("R5 settings: %+v", settings)
	}
	var record model.Profile
	if err := json.Unmarshal([]byte(`{"proxy_groups":[{"id":"g","name":123.50}],"domain_discovery":{"region_probe_port":"18003"}}`), &record); err != nil {
		t.Fatal(err)
	}
	if issues := model.Issues(record); len(issues) != 0 {
		t.Fatalf("shared scalar decode reported issues: %+v", issues)
	}
	view, err := configFromModel(&model.Config{Profile: &record})
	if err != nil || view.Groups[0].Name != "123.50" || view.Settings.RegionPort != 18003 {
		t.Fatalf("shared coercion: %+v %v", view, err)
	}
}

func TestNativeRepositoryUpdatesPreserveUnownedStoredMembers(t *testing.T) {
	ctx := jobFixture(t)
	r, err := repo.GetRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("domain_discovery", json.RawMessage(`{"history":[{"value":"example.com","future":{"n":123.50}}],"future":{"flag":false}}`))
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = editSettings(ctx, "default", func(s *Settings) error { s.AutoProbe = true; return nil }); err != nil {
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
	if string(fields["future"]) != `{"flag":false}` || !strings.Contains(string(fields["history"]), `"future":{"n":123.50}`) {
		t.Fatalf("unowned fields changed: %s", data)
	}
	sentinel := errors.New("discard")
	before, _ := json.Marshal(p)
	err = editProfile(ctx, "default", func(c *config) error { c.Settings.AutoApply = true; return sentinel })
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
	value := reflect.ValueOf(collaboration)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).IsNil() {
			t.Errorf("missing collaborator %s", value.Type().Field(i).Name)
		}
	}
	if client == nil {
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
	ctx := jobFixture(t)
	repository, err := repo.GetRepository(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = repository.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.Agents = []model.Agent{{ID: "runtime-agent", Name: "Synthetic", ProfileID: "default", ServiceType: "mihomo", DomainDiscoveryEnabled: ptr(true), LastHeartbeat: "2000-01-01T00:00:00Z", Version: "1.0.0"}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := agents.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Runtime().RestoreHeartbeat(ctx, "runtime-agent", 4070908800, "2099-01-01T00:00:00Z", &model.Agent{Version: minRegionVersion}); err != nil {
		t.Fatal(err)
	}
	records, err := profileAgents(ctx, "default")
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
