package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/model"
)

// TestN88bReviewWrongTypedOutputIdentities covers stored members that become an
// output key or rule text. Under R5 they read as "", which used to emit a
// provider named "" (unreachable from the group's original `use` entry, and two
// such providers collide) or rule lines like "DOMAIN,,G". They must be rejected
// with the stored value, as the pre-typed generator did for provider names.
func TestN88bReviewWrongTypedOutputIdentities(t *testing.T) {
	env := nativeGenerationEnv(t)
	env.LoadSubscriptionCache = func(context.Context, string) (json.RawMessage, error) { return nil, nil }
	t.Cleanup(SetMihomoEnvironment(env))
	const base = `{"profile_id":"p",
"nodes":[{"id":"n","name":"N","type":"http","server":"n.example.test","port":80}],
"subscriptions":[{"id":"s1","name":%s,"url":"https://sub.example.com/a"}],
"subscription_aggregations":[{"id":"a","name":%s,"subscriptions":[]}],
"proxy_groups":[{"id":"g","name":"G","type":%s,"manual_nodes":["n"],"subscriptions":["s1"],"aggregations":["a"]},
 {"id":"g2","name":"G2","type":"select","follow_group":%s}],
"rule_configs":[
 {"id":"r1","name":%s,"itemType":"ruleset","behavior":%s,"url":"https://rules.example.com/a.yaml","policy":"G"},
 {"id":"r2","itemType":"rule","rule_type":%s,"value":%s,"policy":"G"}]}`
	valid := []string{`"S1"`, `"A"`, `"select"`, `"g"`, `"RS"`, `"domain"`, `"DOMAIN"`, `"a.example.com"`}
	tests := []struct {
		name  string
		index int
		value string
		want  string
	}{
		// The wrong-typed rows (name false, type 7, ...) were removed in O18
		// T1c: plain records read them as zero values and no stored
		// original remains for a refusal.
		{"valid", -1, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := make([]any, len(valid))
			for i, v := range valid {
				values[i] = v
			}
			if tt.index >= 0 {
				values[tt.index] = tt.value
			}
			config, err := DecodeConfigJ(mustJSON(t, fmt.Sprintf(base, values...)))
			if err != nil {
				t.Fatal(err)
			}
			out, err := MihomoConfig(context.Background(), config, MihomoOptions{SkipProviderPreflight: true})
			if tt.want == "" {
				if err != nil || !strings.Contains(out, "RULE-SET,RS,G") || !strings.Contains(out, "DOMAIN,a.example.com,G") || !strings.Contains(out, "- name: G2\n    type: select\n") {
					t.Fatalf("valid profile: %v\n%s", err, out)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("generation: want %q, got %v\n%s", tt.want, err, out)
			}
			if strings.HasPrefix(tt.name, "rule set") {
				downloads, err := MihomoRulesetDownloads(context.Background(), config, "")
				if downloads != nil || err == nil || err.Error() != tt.want {
					t.Fatalf("ruleset downloads: want %q, got %v %v", tt.want, downloads, err)
				}
			}
		})
	}
}

// A numeric stored rule value reads as its text (plain records, O18 T1c) and
// generates the rule (R5 kept the number and refused generation).
func TestN88bReviewNumericRuleValueGenerates(t *testing.T) {
	t.Cleanup(SetMihomoEnvironment(nativeGenerationEnv(t)))
	var profile model.Profile
	if err := json.Unmarshal([]byte(`{"rule_configs":[{"id":"port","itemType":"rule","rule_type":"DST-PORT","value":443,"policy":"DIRECT"}]}`), &profile); err != nil {
		t.Fatal(err)
	}
	if model.TextValue(profile.RuleConfigs[0].Value) != "443" {
		t.Fatal("shared scalar text not used")
	}
	output, err := MihomoConfig(context.Background(), &model.Config{Profile: &profile}, MihomoOptions{SkipProviderPreflight: true})
	if err != nil || !strings.Contains(output, "DST-PORT,443,DIRECT") {
		t.Fatalf("numeric rule value: %v %s", err, output)
	}
}
