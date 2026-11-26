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
	// Plain records drop undeclared members at every level (O18 T1a–T1c;
	// system_config and rule_library items kept theirs before T1c).
	for _, unknown := range []string{"future_profile", "future_system", "future_agent", "future_rule"} {
		if bytes.Contains(result, []byte(unknown)) {
			t.Fatalf("unknown member %s kept: %s", unknown, result)
		}
	}
	for _, want := range []string{`"profile_id":"default"`, `"agents":[{"id":"device"}]`, `"rule_library":[{"id":"rules"}]`} {
		if !bytes.Contains(result, []byte(want)) {
			t.Fatalf("lost %s: %s", want, result)
		}
	}
	var decoded map[string]json.RawMessage
	if json.Unmarshal(result, &decoded) != nil {
		t.Fatal("invalid captured JSON")
	}
	if !bytes.Contains(source, []byte(`"n":2.00`)) {
		t.Fatal("codec changed input")
	}
}
