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

func TestRuleNodePreservesSwitchTruthiness(t *testing.T) {
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
				if got := RuleEnabled(ruleNode(item)); got != tc.want || got != RuleEnabled(source) {
					t.Fatalf("enabled=%v, want %v; source YAML enabled=%v", got, tc.want, RuleEnabled(source))
				}
				after, err := model.Marshal(item)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatal("projection changed the stored rule", err)
				}
				// An explicit typed edit must supersede the original stored value.
				value := true
				item.Enabled, item.LibraryEnabled = &value, &value
				if !RuleEnabled(ruleNode(item)) {
					t.Fatal("typed edit did not supersede stored flags")
				}
			})
		}
	}
}

func TestRuleNodeDoesNotFullMarshalWhenSwitchAbsent(t *testing.T) {
	// L4 regression: a rule without explicit enabled/library_enabled must not
	// re-marshal the entire RuleConfig (including large extra Fields) just to
	// determine switch truthiness.
	smallRaw := `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT"}`
	largeRaw := `{"id":"r1","itemType":"rule","rule_type":"DOMAIN","value":"example.com","policy":"DIRECT","extra_blob":"` + string(bytes.Repeat([]byte("x"), 8192)) + `"}`
	var smallRule, largeRule model.RuleConfig
	if err := json.Unmarshal([]byte(smallRaw), &smallRule); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(largeRaw), &largeRule); err != nil {
		t.Fatal(err)
	}
	// Warm up once.
	if !RuleEnabled(ruleNode(smallRule)) || !RuleEnabled(ruleNode(largeRule)) {
		t.Fatal("expected absent switches to default enabled")
	}
	smallAllocs := testing.AllocsPerRun(20, func() {
		_ = ruleNode(smallRule)
	})
	largeAllocs := testing.AllocsPerRun(20, func() {
		_ = ruleNode(largeRule)
	})
	if largeAllocs > smallAllocs+2 {
		t.Fatalf("ruleNode scaled with extra fields (small=%.1f allocs, large=%.1f allocs); expected no full RuleConfig marshal", smallAllocs, largeAllocs)
	}
}
