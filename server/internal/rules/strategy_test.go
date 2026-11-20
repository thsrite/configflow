package rules

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"configflow/internal/model"
)

// strategy_references.validate_rule_policies is exercised in Python through the
// Surge/Loon/Mihomo converters (test_loon_generation.py and others, left for
// packages #6/#7); these tests pin the function itself.

func TestValidateRulePolicies(t *testing.T) {
	disabled := false
	ok := []struct {
		format string
		rules  []model.RuleConfig
	}{
		{"mihomo", []model.RuleConfig{
			{ItemType: "rule", Policy: strp("Proxy A")},
			{ItemType: "ruleset", Policy: strp("DIRECT")},
			{ItemType: "ruleset"},
		}},
		{"loon", []model.RuleConfig{
			{ItemType: "ruleset"},
			{ItemType: "rule", Policy: strp("REJECT-IMG")},
		}},
		{"surge", []model.RuleConfig{
			{ItemType: "rule", Policy: strp("CELLULAR")},
		}},
		{"mihomo", []model.RuleConfig{
			{ItemType: "rule", Policy: strp("Gone"), Enabled: &disabled},
			{ItemType: "rule", Policy: strp("Gone"), LibraryEnabled: &disabled},
			{ItemType: "other", Policy: strp("Gone")},
		}},
	}
	for _, c := range ok {
		if err := ValidatePolicies(c.rules, []string{"Proxy A", "PROXY", "Proxy"}, c.format); err != nil {
			t.Errorf("%s %+v rejected: %v", c.format, c.rules, err)
		}
	}
	err := ValidatePolicies([]model.RuleConfig{{ID: "r1", ItemType: "rule", Policy: strp("Missing")}}, []string{"Proxy A"}, "loon")
	var reference *StrategyReferenceError
	if !errors.As(err, &reference) || err.Error() != "规则 r1 引用了不存在或未启用的策略 'Missing'，请修改策略引用" {
		t.Errorf("err = %v", err)
	}
	// A rule without a policy targets "" and fails; a rule set defaults to the main strategy.
	if err := ValidatePolicies([]model.RuleConfig{{ID: "r2", ItemType: "rule"}}, nil, "mihomo"); err == nil || !strings.Contains(err.Error(), "''") {
		t.Errorf("empty policy: %v", err)
	}
	if err := ValidatePolicies([]model.RuleConfig{{Name: "set", ItemType: "ruleset"}}, nil, "surge"); err == nil || !strings.Contains(err.Error(), "'Proxy'") {
		t.Errorf("default surge policy: %v", err)
	}
	if err := ValidatePolicies([]model.RuleConfig{{ItemType: "ruleset"}}, nil, "mihomo"); err == nil || !strings.Contains(err.Error(), "'PROXY'") {
		t.Errorf("default mihomo policy: %v", err)
	}
}

func TestValidatePoliciesExplicitNullAndNonString(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		available []string
		format    string
		wantErr   string
	}{
		{
			name:      "absent ruleset policy uses default PROXY",
			raw:       `{"id":"r1","itemType":"ruleset"}`,
			available: []string{"PROXY"},
			format:    "mihomo",
		},
		{
			name:      "absent ruleset policy missing PROXY",
			raw:       `{"id":"r1","itemType":"ruleset"}`,
			available: nil,
			format:    "mihomo",
			wantErr:   "规则 r1 引用了不存在或未启用的策略 'PROXY'，请修改策略引用",
		},
		{
			name:      "explicit null ruleset policy does not default to PROXY",
			raw:       `{"id":"proxy","itemType":"ruleset","policy":null}`,
			available: []string{"PROXY", "null"},
			format:    "mihomo",
			wantErr:   "规则 proxy 引用了不存在或未启用的策略 'null'，请修改策略引用",
		},
		{
			name:      "explicit null rule policy renders null",
			raw:       `{"id":"r2","itemType":"rule","policy":null}`,
			available: []string{"null"},
			format:    "mihomo",
			wantErr:   "规则 r2 引用了不存在或未启用的策略 'null'，请修改策略引用",
		},
		{
			name:      "explicit numeric policy rejected even if string name exists",
			raw:       `{"id":"num","itemType":"rule","policy":5}`,
			available: []string{"5"},
			format:    "mihomo",
			wantErr:   "规则 num 引用了不存在或未启用的策略 '5'，请修改策略引用",
		},
		{
			name:      "explicit boolean policy renders JSON text",
			raw:       `{"id":"flag","itemType":"ruleset","policy":true}`,
			available: []string{"PROXY", "true"},
			format:    "mihomo",
			wantErr:   "规则 flag 引用了不存在或未启用的策略 'true'，请修改策略引用",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var rule model.RuleConfig
			if err := json.Unmarshal([]byte(tt.raw), &rule); err != nil {
				t.Fatal(err)
			}
			err := ValidatePolicies([]model.RuleConfig{rule}, tt.available, tt.format)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			var ref *StrategyReferenceError
			if !errors.As(err, &ref) || err.Error() != tt.wantErr {
				t.Fatalf("got %v, want %q", err, tt.wantErr)
			}
		})
	}
}
