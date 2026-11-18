package convert

import (
	"testing"

	"configflow/internal/jsonx"
)

func TestRulesetFormatRejectsMalformedLibraryAfterMatch(t *testing.T) {
	for _, rule := range []*jsonx.Object{
		jsonx.Obj("name", "Edge 15"),
		jsonx.Obj("library_rule_id", "edge-15"),
	} {
		config := jsonx.Obj("rule_library", []any{
			jsonx.Obj("id", "edge-15", "name", "Edge 15", "format", "text"),
			nil,
		})
		got, err := RulesetFormat(rule, config, "")
		if err == nil || got != "" {
			t.Fatalf("RulesetFormat(%v): got %q, %v; want typed validation error", rule, got, err)
		}
	}
}

func TestRulesetFormatRejectsMalformedEntryBeforeMatch(t *testing.T) {
	rule := jsonx.Obj("name", "Edge 15")
	config := jsonx.Obj("rule_library", []any{
		nil,
		jsonx.Obj("id", "edge-15", "name", "Edge 15", "format", "text"),
	})
	_, err := RulesetFormat(rule, config, "")
	if PyErrorClass(err) != "AttributeError" {
		t.Fatalf("RulesetFormat error = %v (%s), want AttributeError", err, PyErrorClass(err))
	}
}
