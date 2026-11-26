package rules

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestSwitchTruthinessRegression(t *testing.T) {
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
	for _, key := range []string{"enabled", "library_enabled"} {
		for _, tc := range cases {
			t.Run("ruleEnabled/"+key+"/"+tc.name, func(t *testing.T) {
				raw := `{}`
				if tc.value != "" {
					raw = `{"` + key + `":` + tc.value + `}`
				}
				var item model.RuleConfig
				if err := json.Unmarshal([]byte(raw), &item); err != nil {
					t.Fatal(err)
				}
				if got := ruleEnabled(&item); got != tc.want {
					t.Fatalf("ruleEnabled(%s) = %v, want %v", raw, got, tc.want)
				}
			})
		}
	}
	for _, tc := range cases {
		t.Run("enabledResource/"+tc.name, func(t *testing.T) {
			raw := `{"id":"n1","name":"Node"}`
			if tc.value != "" {
				raw = `{"id":"n1","name":"Node","enabled":` + tc.value + `}`
			}
			var node model.Node
			if err := json.Unmarshal([]byte(raw), &node); err != nil {
				t.Fatal(err)
			}
			if got := enabledResource(node.Enabled); got != tc.want {
				t.Fatalf("enabledResource(%s) = %v, want %v", raw, got, tc.want)
			}
			var rawField json.RawMessage
			if tc.value != "" {
				rawField = json.RawMessage(tc.value)
			}
			if got := EnabledField(rawField, true); got != tc.want {
				t.Fatalf("EnabledField(%s, true) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
