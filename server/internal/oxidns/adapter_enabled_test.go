package oxidns

import (
	"bytes"
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

var switchTruthinessCases = []struct {
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

func TestRuleActivePreservesSwitchTruthiness(t *testing.T) {
	for _, key := range []string{"enabled", "library_enabled"} {
		for _, tc := range switchTruthinessCases {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				raw := `{}`
				if tc.value != "" {
					raw = `{"` + key + `":` + tc.value + `}`
				}
				var item model.RuleConfig
				if err := json.Unmarshal([]byte(raw), &item); err != nil {
					t.Fatal(err)
				}
				before, err := model.Marshal(item)
				if err != nil {
					t.Fatal(err)
				}
				source, err := FromJSON([]byte(raw))
				if err != nil {
					t.Fatal(err)
				}
				if got := ruleActive(item); got != tc.want || got != RuleEnabled(source) {
					t.Fatalf("enabled=%v, want %v; source YAML enabled=%v", got, tc.want, RuleEnabled(source))
				}
				after, err := model.Marshal(item)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("projection changed the stored rule", err)
				}
				// An explicit typed edit must supersede the original stored value.
				value := true
				item.Enabled, item.LibraryEnabled = &value, &value
				if !ruleActive(item) {
					t.Fatal("typed edit did not supersede stored flags")
				}
			})
		}
	}
}
