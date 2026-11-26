package provider

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestCaptureDeliverySwitchTruthiness(t *testing.T) {
	cases := []struct {
		name, value string
		wantChain   bool
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
			raw := `{"proxy_groups":[{"id":"g1","name":"Chain","type":"chain"}]}`
			if tc.value != "" {
				raw = `{"proxy_groups":[{"id":"g1","name":"Chain","type":"chain","enabled":` + tc.value + `}]}`
			}
			var profile model.Profile
			if err := json.Unmarshal([]byte(raw), &profile); err != nil {
				t.Fatal(err)
			}
			snap, err := CaptureDelivery("default", nil, &profile)
			if err != nil {
				t.Fatal(err)
			}
			gotChain := snap.chainNames["Chain"]
			if gotChain != tc.wantChain {
				t.Fatalf("CaptureDelivery(%s).chainNames = %v, wantChain=%v", raw, snap.chainNames, tc.wantChain)
			}
		})
	}
}
