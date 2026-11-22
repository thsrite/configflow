package convert_test

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
)

// TestRuleFormatPreservesR5 covers library shapes. Rows with wrong-typed IDs,
// names and URLs were removed in O18 T1c: plain records read them as missing,
// so the format follows the remaining values.
func TestRuleFormatPreservesR5(t *testing.T) {
	for _, test := range []struct {
		name, rule, library, want string
		err                       error
	}{
		{"null library", `{"url":"https://rules.example.com/r.yaml"}`, `null`, "yaml", nil},
		{"scalar library", `{"url":"https://rules.example.com/r.yaml"}`, `false`, "yaml", nil},
		{"bad entry resets library", `{"url":"https://rules.example.com/r.yaml"}`, `[null,{"name":"r","format":"mrs"}]`, "yaml", nil},
		{"format precedes URL", `{"name":"r"}`, `[{"name":"r","format":"mrs","url":[]}]`, "mrs", nil},
		{"first name match", `{"name":"r"}`, `[{"name":"r","format":"yaml"},{"name":"r","format":"mrs"},{"name":[]}]`, "yaml", nil},
		{"first ID match", `{"library_rule_id":"r"}`, `[{"id":"r","format":"text"},{"id":"r","format":"mrs"},{"id":[]}]`, "text", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rule model.RuleConfig
			if err := json.Unmarshal([]byte(test.rule), &rule); err != nil {
				t.Fatal(err)
			}
			var shared model.Shared
			if err := json.Unmarshal([]byte(`{"rule_library":`+test.library+`}`), &shared); err != nil {
				t.Fatal(err)
			}
			config := &model.Config{RuleLibrary: shared.RuleLibrary}
			got, err := convert.RuleFormat(rule, config, "")
			if got != test.want || (test.err == nil && err != nil) || (test.err != nil && !errors.Is(err, test.err)) {
				t.Fatalf("got %q / %v; want %q / %v", got, err, test.want, test.err)
			}
		})
	}
}
