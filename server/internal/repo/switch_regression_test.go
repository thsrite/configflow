package repo

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestStoredEnabledSwitchTruthiness(t *testing.T) {
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
			raw := `{"id":"n1","name":"Node"}`
			if tc.value != "" {
				raw = `{"id":"n1","name":"Node","enabled":` + tc.value + `}`
			}
			var node model.Node
			if err := json.Unmarshal([]byte(raw), &node); err != nil {
				t.Fatal(err)
			}
			if got := storedEnabled(&node, node.Enabled); got != tc.want {
				t.Fatalf("storedEnabled(%s) = %v, want %v", raw, got, tc.want)
			}
		})
	}
}
