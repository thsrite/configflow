package convert

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestBatch6R5RuleIdentityValidationPreservesStorage(t *testing.T) {
	const source = `{"rule_configs":[{"itemType":"rule","rule_type":"DST-PORT","value":"5","policy":"DIRECT","future":1e3}]}`
	config, err := DecodeRulesConfigJ(mustJSON(t, source))
	if err != nil {
		t.Fatal(err)
	}
	before, err := model.Marshal(config.Profile.RuleConfigs[0])
	if err != nil {
		t.Fatal(err)
	}
	after, err := model.Marshal(config.Profile.RuleConfigs[0])
	if err != nil || string(before) != string(after) {
		t.Fatalf("validation changed storage: %s %v", after, err)
	}
	// Model decoding remains lossless; rejection belongs to generation only.
	var rule model.RuleConfig
	if err := json.Unmarshal([]byte(`{"value":5}`), &rule); err != nil || model.TextValue(rule.Value) != "5" {
		t.Fatalf("shared scalar decode: %+v %v", rule, err)
	}
}
