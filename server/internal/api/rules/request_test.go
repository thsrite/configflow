package rules

import (
	"errors"
	"testing"

	"configflow/internal/model"
)

func TestRuleInputReplacesOnlySentMembers(t *testing.T) {
	base := model.RuleConfig{ID: "r", ItemType: "rule", RuleType: "DOMAIN", Value: new("a.example"), Policy: new("DIRECT"), Remark: new("keep")}
	tests := []struct {
		name, body string
		check      func(model.RuleConfig) bool
		typeErr    bool
	}{
		{"absent keeps base", `{}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" && *r.Policy == "DIRECT" }, false},
		{"text replaced", `{"remark":"x","value":"b.example"}`, func(r model.RuleConfig) bool { return *r.Remark == "x" && *r.Value == "b.example" }, false},
		{"null policy is empty", `{"policy":null}`, func(r model.RuleConfig) bool { return r.Policy != nil && *r.Policy == "" }, false},
		{"null value clears", `{"value":null}`, func(r model.RuleConfig) bool { return r.Value == nil }, false},
		{"wrong shadowed member", `{"value":5}`, func(model.RuleConfig) bool { return true }, true},
		{"wrong other member", `{"enabled":"yes"}`, func(model.RuleConfig) bool { return true }, true},
		{"unknown member ignored", `{"future":{"keep":true}}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" }, false},
		{"member names are exact", `{"Remark":"x"}`, func(r model.RuleConfig) bool { return *r.Remark == "keep" }, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeRule([]byte(tc.body), base).rule()
			if (err != nil) != tc.typeErr || !tc.check(got) {
				t.Fatalf("rule %+v err %v", got, err)
			}
		})
	}
}

func TestNormalizedDropsMembersTheKindDoesNotKeep(t *testing.T) {
	config := &model.Config{Profile: &model.Profile{}, RuleLibrary: []model.RuleLibraryItem{{ID: "lib"}}}
	in := decodeRule([]byte(`{"itemType":"ruleset","library_rule_id":"lib","url":5,"udp_fallback":"DIRECT","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err != nil {
		t.Fatal(err)
	}
	got, err := in.rule()
	if err != nil || got.URL != nil || got.UDPFallback != nil || got.NTPDirect != nil || got.LibraryRuleID != "lib" {
		t.Fatalf("rule %+v err %v", got, err)
	}
	in = decodeRule([]byte(`{"rule_type":"MATCH","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err == nil || err.Error() != msgNTP {
		t.Fatalf("MATCH ntp: %v", err)
	}
	in = decodeRule([]byte(`{"rule_type":"DOMAIN","value":"a.example","ntp_direct":"x"}`), model.RuleConfig{})
	if err := normalized(in, nil, config); err != nil {
		t.Fatal(err)
	}
	if got, err := in.rule(); err != nil || got.ItemType != "rule" || got.NTPDirect != nil {
		t.Fatalf("rule %+v err %v", got, err)
	}
	if err := ruleTypeError([]model.RuleConfig{{ID: "a", ItemType: "rule"}}, errMemberType); err == nil || err.Error() != "rule_configs 类型不正确" {
		t.Fatalf("type error message: %v", err)
	} else if !errors.Is(err, err) {
		t.Fatal("unreachable")
	}
}
