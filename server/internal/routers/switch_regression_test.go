package routers

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestIPv6AgentEnabledSwitchTruthiness(t *testing.T) {
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
		// The Agent's enabled switch is a device-write gate (O18 T1b
		// review): another type than a boolean is off.
		{"one", "1", false},
		{"yes", `"yes"`, false},
		{"true", "true", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := `{"id":"a1","service_type":"mihomo"}`
			if tc.value != "" {
				raw = `{"id":"a1","service_type":"mihomo","enabled":` + tc.value + `}`
			}
			var agent model.Agent
			if err := json.Unmarshal([]byte(raw), &agent); err != nil {
				t.Fatal(err)
			}
			if got := ipv6AgentEnabled(&agent); got != tc.want {
				t.Fatalf("ipv6AgentEnabled(%s) = %v, want %v", raw, got, tc.want)
			}
		})
	}
}
