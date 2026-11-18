package convert_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/rulesetfmt"
)

func TestRulesetFormatNarrowCodecPreservesR5(t *testing.T) {
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
			value, err := jsonx.ParseString(test.rule)
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := jsonx.ParseString(`{"rule_library":` + test.library + `}`)
			if err != nil {
				t.Fatal(err)
			}
			config := parsed.(*jsonx.Object)
			// These values cannot be encoded as JSON and do not participate in
			// format inference. They must never be traversed by this codec.
			config.Set("agents", func() {})
			config.Set("oxidns", make(chan int))
			got, err := convert.RulesetFormat(value.(*jsonx.Object), config, "")
			if got != test.want || (test.err == nil && err != nil) || (test.err != nil && !errors.Is(err, test.err)) {
				t.Fatalf("got %q / %v; want %q / %v", got, err, test.want, test.err)
			}
		})
	}
}

// BenchmarkRulesetFormatUnrelatedConfig measures whether unrelated Agent
// histories are traversed. Input allocation is outside the measured operation;
// the 0/1/16 MiB variants have identical format declarations.
func BenchmarkRulesetFormatUnrelatedConfig(b *testing.B) {
	for _, size := range []int{0, 1 << 20, 16 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rule := jsonx.Obj("library_rule_id", "r")
			config := jsonx.Obj("rule_library", []any{jsonx.Obj("id", "r", "format", "yaml")},
				"agents", []any{jsonx.Obj("unrelated_history", strings.Repeat("x", size))})
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				format, err := convert.RulesetFormat(rule, config, "")
				if err != nil || format != "yaml" {
					b.Fatalf("format=%q: %v", format, err)
				}
			}
		})
	}
}
