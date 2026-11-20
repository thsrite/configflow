package discovery

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestRuleFromModelSwitchTruthiness(t *testing.T) {
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
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT"}`
			if tc.value != "" {
				raw = `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","enabled":` + tc.value + `}`
			}
			var record model.RuleConfig
			if err := json.Unmarshal([]byte(raw), &record); err != nil {
				t.Fatal(err)
			}
			item, err := ruleFromModel(record)
			if err != nil {
				t.Fatal(err)
			}
			if item.Enabled != tc.want {
				t.Fatalf("ruleFromModel(%s).Enabled = %v, want %v", raw, item.Enabled, tc.want)
			}
			var rawOpt json.RawMessage
			wantOpt := tc.want
			if tc.value != "" {
				rawOpt = json.RawMessage(tc.value)
			} else {
				wantOpt = false
			}
			if got := JSONOptionEnabled(rawOpt); got != wantOpt {
				t.Fatalf("JSONOptionEnabled(%s) = %v, want %v", tc.value, got, wantOpt)
			}
		})
	}
}
