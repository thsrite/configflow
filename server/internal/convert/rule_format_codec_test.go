package convert_test

import (
	"encoding/json"
	"errors"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/model"
	"configflow/internal/rulesetfmt"
)

func TestRuleFormatPreservesR5(t *testing.T) {
	for _, test := range []struct {
		name, rule, library, want string
		err                       error
	}{
		{"null library", `{"url":"https://rules.example.com/r.yaml"}`, `null`, "yaml", nil},
		{"scalar library", `{"url":"https://rules.example.com/r.yaml"}`, `false`, "yaml", nil},
		{"bad entry resets library", `{"url":"https://rules.example.com/r.yaml"}`, `[null,{"name":"r","format":"mrs"}]`, "yaml", nil},
		{"bad reference ID", `{"library_rule_id":[]}`, `[{"id":"r"}]`, "", rulesetfmt.ErrReferenceIDType},
		{"bad declaration ID", `{"library_rule_id":"r"}`, `[{"id":[]}]`, "", rulesetfmt.ErrReferenceIDType},
		{"bad reference name", `{"name":[]}`, `[{"name":"r"}]`, "", rulesetfmt.ErrReferenceNameType},
		{"bad declaration name", `{"name":"r"}`, `[{"name":[]}]`, "", rulesetfmt.ErrReferenceNameType},
		{"bad rule URL", `{"url":[]}`, `[]`, "text", rulesetfmt.ErrURLType},
		{"bad declaration URL", `{"name":"r"}`, `[{"name":"r","url":[]}]`, "text", rulesetfmt.ErrURLType},
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
