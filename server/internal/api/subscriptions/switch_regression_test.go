package subscriptions

import (
	"encoding/json"
	"testing"
)

func TestSubscriptionObjectEnabledSwitchTruthiness(t *testing.T) {
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
			item := object{}
			if tc.value != "" {
				item["enabled"] = json.RawMessage(tc.value)
			}
			if got := item.enabled(); got != tc.want {
				t.Fatalf("object.enabled(%s) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
