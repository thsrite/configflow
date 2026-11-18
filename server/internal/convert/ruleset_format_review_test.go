package convert

import (
	"testing"

	"configflow/internal/jsonx"
)

func TestRulesetFormatR5MalformedLibraryAfterMatch(t *testing.T) {
	for _, rule := range []*jsonx.Object{
		jsonx.Obj("name", "Edge 15"),
		jsonx.Obj("library_rule_id", "edge-15"),
	} {
		config := jsonx.Obj("rule_library", []any{
			jsonx.Obj("id", "edge-15", "name", "Edge 15", "format", "text"),
			nil,
		})
		got, err := RulesetFormat(rule, config, "")
		if err != nil || got != "text" {
			t.Fatalf("RulesetFormat(%v): got %q, %v; want R5 default format", rule, got, err)
		}
	}
}

func TestRulesetFormatR5MalformedLibraryBeforeMatch(t *testing.T) {
	rule := jsonx.Obj("name", "Edge 15")
	config := jsonx.Obj("rule_library", []any{
		nil,
		jsonx.Obj("id", "edge-15", "name", "Edge 15", "format", "text"),
	})
	got, err := RulesetFormat(rule, config, "")
	if err != nil || got != "text" {
		t.Fatalf("got %q/%v, want R5 default text", got, err)
	}
}
