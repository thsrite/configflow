package convert

import (
	"testing"

	"configflow/internal/model"
)

func TestRulesetFormatR5MalformedLibraryAfterMatch(t *testing.T) {
	for _, raw := range []string{
		`{"name":"Edge 15"}`,
		`{"library_rule_id":"edge-15"}`,
	} {
		rule, err := DecodeJ[model.RuleConfig](mustJSON(t, raw))
		if err != nil {
			t.Fatal(err)
		}
		config, err := DecodeRuleLibraryConfigJ(mustJSON(t, `{"rule_library":[{"id":"edge-15","name":"Edge 15","format":"text"},null]}`))
		if err != nil {
			t.Fatal(err)
		}
		got, err := RuleFormat(rule, config, "")
		if err != nil || got != "text" {
			t.Fatalf("RuleFormat(%s): got %q, %v; want R5 default format", raw, got, err)
		}
	}
}

func TestRulesetFormatR5MalformedLibraryBeforeMatch(t *testing.T) {
	rule, err := DecodeJ[model.RuleConfig](mustJSON(t, `{"name":"Edge 15"}`))
	if err != nil {
		t.Fatal(err)
	}
	config, err := DecodeRuleLibraryConfigJ(mustJSON(t, `{"rule_library":[null,{"id":"edge-15","name":"Edge 15","format":"text"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := RuleFormat(rule, config, "")
	if err != nil || got != "text" {
		t.Fatalf("got %q/%v, want R5 default text", got, err)
	}
}
