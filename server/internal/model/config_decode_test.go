package model

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCapturedConfigOwnerRoundTrip(t *testing.T) {
	source := []byte(`{"profile_id":"default","future_profile":{"n":2.00},"system_config":{"future_system":1e0},"agents":[{"id":"device","future_agent":4.00}],"rule_library":[{"id":"rules","future_rule":3.0}],"nodes":[],"subscriptions":[],"subscription_aggregations":[]}`)
	config, err := DecodeConfig(source)
	if err != nil {
		t.Fatal(err)
	}
	if config.ProfileID != "default" || !config.HasAgents || len(config.Agents) != 1 || len(config.RuleLibrary) != 1 {
		t.Fatal("lost resolved sections")
	}
	profile, err := Marshal(config.Profile)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(profile, []byte("system_config")) || bytes.Contains(profile, []byte("agents")) || bytes.Contains(profile, []byte("rule_library")) {
		t.Fatal("composition copied into profile Raw")
	}
	result, err := Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"n":2.00`, `"future_system":1e0`, `"future_agent":4.00`, `"future_rule":3.0`} {
		if !bytes.Contains(result, []byte(want)) {
			t.Fatalf("lost evidence %s: %s", want, result)
		}
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(result, &decoded) != nil {
		t.Fatal("invalid captured JSON")
	}
	if err := config.Profile.Fields.Set("future_profile", []byte(`null`)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(source, []byte(`"n":2.00`)) {
		t.Fatal("codec changed input")
	}
}
