package setup

import (
	"encoding/json"
	"fmt"
	"testing"

	"configflow/internal/model"
)

func TestResourceEnabledFollowsJSONTruthiness(t *testing.T) {
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
		t.Run("subscription/"+tc.name, func(t *testing.T) {
			var sub model.Subscription
			raw := fmt.Sprintf(`{"id":"s1","name":"S1","url":"https://example.com/sub"%s}`, tc.field)
			if err := json.Unmarshal([]byte(raw), &sub); err != nil {
				t.Fatalf("unmarshal subscription: %v", err)
			}
			if got := resourceEnabled(&sub, sub.Enabled); got != tc.want {
				t.Fatalf("resourceEnabled = %v, want %v", got, tc.want)
			}
			selectable := selectableSubscriptions([]model.Subscription{sub})
			if (len(selectable) == 1) != tc.want {
				t.Fatalf("selectableSubscriptions len = %d, want selected=%v", len(selectable), tc.want)
			}
		})

		t.Run("node/"+tc.name, func(t *testing.T) {
			var node model.Node
			raw := fmt.Sprintf(`{"id":"n1","name":"N1"%s}`, tc.field)
			if err := json.Unmarshal([]byte(raw), &node); err != nil {
				t.Fatalf("unmarshal node: %v", err)
			}
			if got := resourceEnabled(&node, node.Enabled); got != tc.want {
				t.Fatalf("resourceEnabled = %v, want %v", got, tc.want)
			}
		})

		t.Run("rule_library/"+tc.name, func(t *testing.T) {
			var item model.RuleLibraryItem
			raw := fmt.Sprintf(`{"id":"r1","name":"R1","behavior":"domain","format":"text","url":"https://example.com/r.list"%s}`, tc.field)
			if err := json.Unmarshal([]byte(raw), &item); err != nil {
				t.Fatalf("unmarshal rule library item: %v", err)
			}
			if got := resourceEnabled(&item, item.Enabled); got != tc.want {
				t.Fatalf("resourceEnabled = %v, want %v", got, tc.want)
			}
			if got := supported(item); got != tc.want {
				t.Fatalf("supported = %v, want %v", got, tc.want)
			}
		})
	}
}
