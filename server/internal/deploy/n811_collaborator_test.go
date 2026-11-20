package deploy

import (
	"configflow/internal/convert"
	"configflow/internal/model"
	"encoding/json"
	"errors"
	"testing"
)

func TestN811MissingGenerationCollaboratorFailsClosed(t *testing.T) {
	restore := convert.SetMihomoEnvironment(convert.MihomoEnvironment{})
	defer restore()
	Wire()
	source, err := SourceFromJSON([]byte(`{"mihomo":{"custom_config":"mode: rule"}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	target, err := NewTypedTarget(&model.Agent{ServiceType: "mihomo"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = GenerateAgentConfig(testContext(t), source, target, "https://panel.example.com")
	var missing *convert.HookMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("missing collaborator was skipped: %v", err)
	}
}

func TestOptionalSnapshotsAndTargetsRemainSafe(t *testing.T) {
	revision := int64(7)
	empty, err := SourceFromJSON([]byte(`{}`), &revision)
	if err != nil {
		t.Fatal(err)
	}
	if empty.revision == nil || *empty.revision != revision || empty.serverDomain("https://panel.example.com") != "https://panel.example.com" {
		t.Fatal("empty source lost optional revision/default")
	}
	source, err := SourceFromJSON([]byte(`{}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := ContentInputKey(Target{}, source, "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ContentInputKey(Target{Record: &model.Agent{}}, source, "")
	if err != nil || a != b {
		t.Fatal("absent target no longer projects null identity", err)
	}
}

func TestSourceKeepsLegacyRuleSetProjection(t *testing.T) {
	source, err := SourceFromJSON([]byte(`{"system_config":{"server_domain":123},"rule_sets":[{"id":"legacy-rule","url":"https://rules.example.com/source"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	input := source.ruleConfig()
	if len(input.RuleSets) != 1 || input.RuleSets[0].ID != "legacy-rule" {
		t.Fatal("legacy stored rule_sets were dropped")
	}
	if source.serverDomain("https://panel.example.com") != "https://panel.example.com" {
		t.Fatal("scalar projection became publishing origin")
	}
}

func TestTargetKeepsOriginalIdentityAndCapabilityTypes(t *testing.T) {
	target, err := NewTypedTarget(fixtureAgent(map[string]any{"id": 123, "name": 20, "profile_id": 456, "host": 789, "token": 100, "version": 1.9, "service_type": 30, "deployment_method": 40, "domain_discovery_enabled": "true"}))
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "" || target.Name != "" || target.ProfileID != "" || target.Host != "" || target.Token != "" || target.Version != "" || target.ServiceType != "" || target.DeploymentMethod != "" || target.DomainDiscovery {
		t.Fatal("stored display projections became captured identity/capability")
	}
}

func TestResultFieldPreservesExtraSummaryCounts(t *testing.T) {
	base := &model.Deployment{Success: true, DeploymentID: "dep-1", Status: "ready"}
	out := resultField(base, "profile_id", "default")
	out = resultField(out, "file_count", 3)
	out = resultField(out, "provider_count", 1)
	out = resultField(out, "rule_count", 2)
	raw, err := model.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["profile_id"] != "default" || got["file_count"] != float64(3) || got["provider_count"] != float64(1) || got["rule_count"] != float64(2) {
		t.Fatalf("extra summary fields lost: %s", raw)
	}
}
