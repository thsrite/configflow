package onboarding

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestStoredSwitchesFollowJSONTruthiness(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  bool
	}{
		{"absent", "", true},
		{"false", `,"enabled":false`, false},
		{"null", `,"enabled":null`, false},
		{"zero", `,"enabled":0`, false},
		{"zero float", `,"enabled":0.0`, false},
		{"empty string", `,"enabled":""`, false},
		{"empty array", `,"enabled":[]`, false},
		{"empty object", `,"enabled":{}`, false},
		{"one", `,"enabled":1`, true},
		{"non-empty string", `,"enabled":"yes"`, true},
		{"true", `,"enabled":true`, true},
	}

	for _, tc := range cases {
		t.Run("rule_config_enabled/"+tc.name, func(t *testing.T) {
			doc := Document{
				"rule_configs": json.RawMessage(fmt.Sprintf(`[{"id":"r1","itemType":"ruleset","name":"R1","behavior":"domain","policy":"PROXY"%s}]`, tc.field)),
			}
			got, err := domainCandidates(doc, nil)
			if err != nil {
				t.Fatalf("domainCandidates: %v", err)
			}
			if len(got) != 1 || got[0].Enabled != tc.want || got[0].Available != tc.want {
				t.Fatalf("got %+v, want enabled/available=%v", got, tc.want)
			}
		})

		t.Run("rule_config_library_enabled/"+tc.name, func(t *testing.T) {
			libField := ""
			if tc.field != "" {
				libField = `,"library_enabled":` + tc.field[len(`,"enabled":`):]
			}
			doc := Document{
				"rule_configs": json.RawMessage(fmt.Sprintf(`[{"id":"r1","itemType":"ruleset","name":"R1","behavior":"domain","policy":"PROXY"%s}]`, libField)),
			}
			got, err := domainCandidates(doc, nil)
			if err != nil {
				t.Fatalf("domainCandidates: %v", err)
			}
			if len(got) != 1 || got[0].Enabled != tc.want || got[0].Available != tc.want {
				t.Fatalf("got %+v, want enabled/available=%v", got, tc.want)
			}
		})

		t.Run("rule_library_enabled/"+tc.name, func(t *testing.T) {
			doc := Document{
				"rule_configs": json.RawMessage(`[{"id":"r1","itemType":"ruleset","library_rule_id":"lib1","policy":"PROXY"}]`),
				"rule_library": json.RawMessage(fmt.Sprintf(`[{"id":"lib1","name":"Lib1","behavior":"domain"%s}]`, tc.field)),
			}
			got, err := domainCandidates(doc, nil)
			if err != nil {
				t.Fatalf("domainCandidates: %v", err)
			}
			if len(got) != 1 || got[0].Enabled != tc.want || got[0].Available != tc.want {
				t.Fatalf("got %+v, want enabled/available=%v", got, tc.want)
			}
		})

		t.Run("proxy_prerequisites_group/"+tc.name, func(t *testing.T) {
			doc := Document{
				"proxy_groups": json.RawMessage(fmt.Sprintf(`[{"id":"g1","name":"Proxy"%s}]`, tc.field)),
				"nodes":        json.RawMessage(`[{"id":"n1","enabled":true}]`),
			}
			got, err := proxyPrerequisites(doc, nil)
			if err != nil {
				t.Fatalf("proxyPrerequisites: %v", err)
			}
			if got.Ready != tc.want || (len(got.Policies) > 0) != tc.want {
				t.Fatalf("got %+v, want ready=%v", got, tc.want)
			}
		})

		for _, key := range []string{"nodes", "subscriptions", "subscription_aggregations"} {
			t.Run("proxy_prerequisites_"+key+"/"+tc.name, func(t *testing.T) {
				doc := Document{
					"proxy_groups": json.RawMessage(`[{"id":"g1","name":"Proxy","enabled":true}]`),
					key:            json.RawMessage(fmt.Sprintf(`[{"id":"s1"%s}]`, tc.field)),
				}
				got, err := proxyPrerequisites(doc, nil)
				if err != nil {
					t.Fatalf("proxyPrerequisites: %v", err)
				}
				if got.Ready != tc.want {
					t.Fatalf("got %+v, want ready=%v", got, tc.want)
				}
			})
		}
	}
}
