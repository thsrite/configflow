package convert

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestConvertSwitchTruthiness(t *testing.T) {
	cases := []struct {
		name, value string
		want        bool
	}{
		{"absent", "", true},
		{"false", "false", false},
		{"null", "null", false},
		{"zero", "0", false},
		{"zero_float", "0.0", false},
		{"neg_zero", "-0", false},
		{"zero_exp", "0e0", false},
		{"empty_string", `""`, false},
		{"empty_array", `[]`, false},
		{"empty_object", `{}`, false},
		{"one", "1", true},
		{"yes", `"yes"`, true},
		{"true", "true", true},
	}
	for _, tc := range cases {
		t.Run("fallback/"+tc.name, func(t *testing.T) {
			raw := `{"rule_configs":[{"itemType":"rule","rule_type":"MATCH","udp_fallback":"DIRECT"}]}`
			if tc.value != "" {
				raw = `{"rule_configs":[{"itemType":"rule","rule_type":"MATCH","enabled":` + tc.value + `,"udp_fallback":"DIRECT"}]}`
			}
			var profile model.Profile
			if err := json.Unmarshal([]byte(raw), &profile); err != nil {
				t.Fatal(err)
			}
			got := FallbackPolicy(&model.Config{Profile: &profile})
			want := ""
			if tc.want {
				want = "DIRECT"
			}
			if got != want {
				t.Fatalf("FallbackPolicy(%s) = %q, want %q", raw, got, want)
			}
		})
		t.Run("library_enabled/"+tc.name, func(t *testing.T) {
			raw := `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT"}`
			if tc.value != "" {
				raw = `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","library_enabled":` + tc.value + `}`
			}
			var rule model.RuleConfig
			if err := json.Unmarshal([]byte(raw), &rule); err != nil {
				t.Fatal(err)
			}
			if got := libraryEnabled(&rule, rule.LibraryEnabled); got != tc.want {
				t.Fatalf("libraryEnabled(%s) = %v, want %v", raw, got, tc.want)
			}
		})
	}
}
