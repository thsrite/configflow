package mosdns

import (
	"encoding/json"
	"testing"

	"configflow/internal/model"
)

func TestFlagSwitchTruthiness(t *testing.T) {
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
			t.Run("rule/"+key+"/"+tc.name, func(t *testing.T) {
				raw := `{"id":"r1"}`
				if tc.value != "" {
					raw = `{"id":"r1","` + key + `":` + tc.value + `}`
				}
				var rule model.RuleConfig
				if err := json.Unmarshal([]byte(raw), &rule); err != nil {
					t.Fatal(err)
				}
				current := rule.Enabled
				if key == "library_enabled" {
					current = rule.LibraryEnabled
				}
				if got := flag(current); got != tc.want {
					t.Fatalf("flag(%s, %q) = %v, want %v", raw, key, got, tc.want)
				}
			})
		}
	}
	for _, tc := range cases {
		t.Run("settings/cache_enabled/"+tc.name, func(t *testing.T) {
			raw := `{}`
			if tc.value != "" {
				raw = `{"cache_enabled":` + tc.value + `}`
			}
			var settings model.MosDNSSettings
			if err := json.Unmarshal([]byte(raw), &settings); err != nil {
				t.Fatal(err)
			}
			if got := flag(settings.CacheEnabled); got != tc.want {
				t.Fatalf("flag(%s, cache_enabled) = %v, want %v", raw, got, tc.want)
			}
		})
	}
}
