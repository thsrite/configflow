package model

import (
	"encoding/json"
	"testing"
)

func TestJSONSwitchEnabled(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		defaultValue bool
		want         bool
	}{
		{"absent_default_true", "", true, true},
		{"absent_default_false", "", false, false},
		{"whitespace_default_true", "  \t\n ", true, true},
		{"false", "false", true, false},
		{"null", "null", true, false},
		{"zero_int", "0", true, false},
		{"neg_zero_int", "-0", true, false},
		{"zero_float", "0.0", true, false},
		{"neg_zero_float", "-0.0", true, false},
		{"zero_exp", "0e0", true, false},
		{"zero_exp_upper", "0.00E+10", true, false},
		{"empty_string", `""`, true, false},
		{"empty_array", "[]", true, false},
		{"empty_array_spaces", "[ \n\t ]", true, false},
		{"empty_object", "{}", true, false},
		{"empty_object_spaces", "{ \n }", true, false},
		{"one", "1", false, true},
		{"neg_one", "-1", false, true},
		{"small_float", "0.001", false, true},
		{"huge_exp", "1e400", false, true},
		{"yes_string", `"yes"`, false, true},
		{"false_string", `"false"`, false, true},
		{"zero_string", `"0"`, false, true},
		{"space_string", `" "`, false, true},
		{"non_empty_array", `[false]`, false, true},
		{"non_empty_object", `{"a":false}`, false, true},
		{"true", "true", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := JSONSwitchEnabled(json.RawMessage(tt.raw), tt.defaultValue); got != tt.want {
				t.Fatalf("JSONSwitchEnabled(%q, %v) = %v, want %v", tt.raw, tt.defaultValue, got, tt.want)
			}
		})
	}
}

func TestSwitchEnabledRecordAndTypedOverride(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"absent", `{"id":"r1"}`, true},
		{"false", `{"id":"r1","enabled":false}`, false},
		{"null", `{"id":"r1","enabled":null}`, false},
		{"zero", `{"id":"r1","enabled":0}`, false},
		{"neg_zero", `{"id":"r1","enabled":-0}`, false},
		{"zero_float", `{"id":"r1","enabled":0.0}`, false},
		{"zero_exp", `{"id":"r1","enabled":0e0}`, false},
		{"empty_string", `{"id":"r1","enabled":""}`, false},
		{"empty_array", `{"id":"r1","enabled":[]}`, false},
		{"empty_object", `{"id":"r1","enabled":{}}`, false},
		{"one", `{"id":"r1","enabled":1}`, true},
		{"yes", `{"id":"r1","enabled":"yes"}`, true},
		{"true", `{"id":"r1","enabled":true}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var rule RuleConfig
			if err := json.Unmarshal([]byte(tc.raw), &rule); err != nil {
				t.Fatal(err)
			}
			if got := Flag(rule.Enabled, true); got != tc.want {
				t.Fatalf("SwitchEnabled(%s) = %v, want %v", tc.raw, got, tc.want)
			}
			// Typed edit overrides stored raw value.
			override := !tc.want
			rule.Enabled = &override
			if got := Flag(rule.Enabled, true); got != override {
				t.Fatalf("after typed override got %v, want %v", got, override)
			}
		})
	}

	if SwitchEnabled(nil, "enabled", nil, false) {
		t.Fatal("nil interface must use defaultValue")
	}
}
