package oxidns

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestRuleNodePreservesExplicitDisabledFlags(t *testing.T) {
	for _, key := range []string{"enabled", "library_enabled"} {
		for _, tc := range []struct {
			name, raw string
			want      bool
		}{
			{"absent", `{}`, true},
			{"true", `{"` + key + `":true}`, true},
			{"false", `{"` + key + `":false}`, false},
			{"null", `{"` + key + `":null}`, false},
			{"zero", `{"` + key + `":0}`, false},
			{"empty", `{"` + key + `":""}`, false},
		} {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				var item model.RuleConfig
				if err := json.Unmarshal([]byte(tc.raw), &item); err != nil {
					t.Fatal(err)
				}
				before, err := model.Marshal(item)
				if err != nil {
					t.Fatal(err)
				}
				source, err := FromJSON([]byte(tc.raw))
				if err != nil {
					t.Fatal(err)
				}
				if got := RuleEnabled(ruleNode(item)); got != tc.want || got != RuleEnabled(source) {
					t.Fatalf("enabled=%v, want %v; source YAML enabled=%v", got, tc.want, RuleEnabled(source))
				}
				after, err := model.Marshal(item)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("projection changed the stored rule", err)
				}
				// An explicit typed edit must supersede the original disabled value.
				value := true
				item.Enabled, item.LibraryEnabled = &value, &value
				if !RuleEnabled(ruleNode(item)) {
					t.Fatal("typed edit did not supersede stored flags")
				}
			})
		}
	}
}
