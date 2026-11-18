package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
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
		{"valid", -1, "", ""},
		{"subscription name", 0, `7`, fmt.Sprintf(msgMihomoStoredText, kindSubscription, "s1", "name", "7")},
		{"aggregation name", 1, `["A"]`, fmt.Sprintf(msgMihomoStoredText, kindAggregation, "a", "name", `["A"]`)},
		{"group type", 2, `7`, fmt.Sprintf(msgMihomoStoredText, kindGroup, "g", "type", "7")},
		{"follow group", 3, `7`, fmt.Sprintf(msgMihomoStoredText, kindGroup, "g2", "follow_group", "7")},
		{"rule set name", 4, `7`, fmt.Sprintf(msgMihomoStoredText, kindRuleSet, "r1", "name", "7")},
		{"rule set behavior", 5, `7`, fmt.Sprintf(msgMihomoStoredText, kindRuleSet, "r1", "behavior", "7")},
		{"rule type", 6, `7`, fmt.Sprintf(msgMihomoStoredText, kindRule, "r2", "rule_type", "7")},
		{"rule value", 7, `443`, fmt.Sprintf(msgMihomoStoredText, kindRule, "r2", "value", "443")},
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
			config, err := configFromLegacy(mustJSON(t, fmt.Sprintf(base, values...)))
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
