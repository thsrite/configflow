package rulefetch

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
)

func TestRuleYAMLAliasBoundaries(t *testing.T) {
	var b strings.Builder
	b.WriteString("payload: [example.com]\na0: &a0 [example.com]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, source := range []string{b.String(), "payload: [example.com]\na: &a [*a]"} {
		if got, warnings := NormalizeYAML(source); got != source || len(warnings) == 0 {
			t.Fatal("normalizer did not retain unsafe source")
		}
		got, err := Inspect(source, "domain", "yaml")
		if err != nil || got.Content != source || len(got.Errors) == 0 {
			t.Fatal("inspection did not reject unsafe graph")
		}
		if _, _, err := ReadRulePayload(source); err == nil {
			t.Fatal("payload reader accepted unsafe graph")
		}
		if got, _, err := RemoveRulePayload(source, func(string) bool { return true }); err == nil || got != "" {
			t.Fatal("remove did not reject unsafe graph")
		}
		if err := ValidateRemote(Content{Data: []byte(source), Text: true}, remoteRule("domain", "yaml"), nil, ""); err == nil {
			t.Fatal("remote validator accepted unsafe graph")
		}
	}
}

func TestQuotedMultilineUsesNativeFoldAndRuleValidation(t *testing.T) {
	source := "payload: [\"example.\ncom\"]\n"
	root, err := yamlx.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if yamlx.Get(root, "payload").Content[0].Value != "example. com" {
		t.Fatal("native quoted multiline fold changed")
	}

	normalized, warnings := NormalizeYAML(source)
	if len(warnings) != 1 || normalized != source {
		t.Fatal("physical multiline delivery must preserve its original source and warning", warnings)
	}
	after, err := yamlx.Parse([]byte(normalized))
	if err != nil || !yamlx.Equal(root, after) {
		t.Fatalf("fold changed: %v", err)
	}
	if err := ValidateRemote(Content{Data: []byte(normalized), Text: true}, remoteRule("domain", "yaml"), nil, ""); err == nil {
		t.Fatal("folded illegal domain accepted")
	}
	result, err := Inspect(source, "domain", "yaml")
	if err != nil || len(result.Errors) == 0 {
		t.Fatal("editor accepted illegal domain")
	}
}

func TestNativeEditSpliceKeepsUntouchedBytes(t *testing.T) {
	source := "# header\r\nmeta: 'unchanged' # note\r\n\r\npayload:\r\n  - \"example.com\" # rule\r\ntail: [a, b]\r\n"
	result, err := Inspect(source, "classical", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(source, "\"example.com\"", "DOMAIN,example.com", 1)
	if result.Content != want {
		t.Fatalf("untouched bytes changed: %q", result.Content)
	}
	if again, err := Inspect(want, "classical", "yaml"); err != nil || again.Content != want {
		t.Fatal("no-op edit changed bytes")
	}
}

func TestNativeSequenceEditsKeepSemanticsAndComments(t *testing.T) {
	source := "# header\npayload: [example.com, other.example] # payload\ntail: 'unchanged' # tail\n"
	_, node, err := ReadRulePayload(source)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := AppendRuleLines(source, []string{"+.added.example"}, node)
	if err != nil {
		t.Fatal(err)
	}
	lines, _, err := ReadRulePayload(appended)
	if err != nil || !slices.Equal(lines, []string{"example.com", "other.example", "+.added.example"}) {
		t.Fatalf("append: %v %v", lines, err)
	}
	removed, _, err := RemoveRulePayload(appended, func(s string) bool { return s == "other.example" })
	lines, _, readErr := ReadRulePayload(removed)
	if err != nil || readErr != nil || !slices.Equal(lines, []string{"example.com", "+.added.example"}) {
		t.Fatalf("remove: %v %v %v", lines, err, readErr)
	}
	for _, comment := range []string{"# header", "# payload", "# tail"} {
		if strings.Count(removed, comment) != 1 {
			t.Fatalf("lost comment %s", comment)
		}
	}
}

func FuzzRuleYAMLBoundary(f *testing.F) {
	for _, s := range []string{"payload: [example.com]", "payload: &p [*p]", "payload: [\"example.\ncom\"]", "payload: !!str [x]"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, source string) {
		_, _ = NormalizeYAML(source)
		_, _ = AppendRuleLines(source, []string{"example.com"}, yamlx.Seq())
		_, _, _ = ReadRulePayload(source)
		_, _, _ = RemoveRulePayload(source, func(s string) bool { return s == "example.com" })
		_ = ValidateRemote(Content{Data: []byte(source), Text: true}, remoteRule("domain", "yaml"), nil, "")
	})
}

func TestNativeEditsFixEmptyAndAliasInsertion(t *testing.T) {
	empty, removed, err := RemoveRulePayload("# header\npayload:\n- example.com\n", func(string) bool { return true })
	if err != nil || len(removed) != 1 {
		t.Fatalf("empty removal: %v %v", removed, err)
	}
	if lines, _, err := ReadRulePayload(empty); err != nil || len(lines) != 0 {
		t.Fatalf("empty YAML is invalid: %q %v", empty, err)
	}
	source := "payload: [&rule example.com, *rule]\n"
	_, payload, err := ReadRulePayload(source)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := AppendRuleLines(source, []string{"added.example"}, payload)
	if err != nil {
		t.Fatal(err)
	}
	lines, _, err := ReadRulePayload(appended)
	if err != nil || !slices.Equal(lines, []string{"example.com", "example.com", "added.example"}) {
		t.Fatalf("alias order changed: %v %v", lines, err)
	}
}

func TestRuleYAMLLinearAlias40DoesNotMaterializeMetadata(t *testing.T) {
	var b strings.Builder
	b.WriteString("payload: [example.com]\na0: &a0 {v: example.com}\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d {child: *a%d}\n", i, i, i-1)
	}
	source := b.String()
	if lines, _, err := ReadRulePayload(source); err != nil || len(lines) != 1 || lines[0] != "example.com" {
		t.Fatalf("payload: %v %v", lines, err)
	}
	if got, warnings := NormalizeYAML(source); got != source || len(warnings) == 0 {
		t.Fatal("alias source was materialized")
	}
	if got, _, err := RemoveRulePayload(source, func(string) bool { return false }); err != nil || got != source {
		t.Fatal("read-only edit changed aliases")
	}
}

func TestQuotedContinuationKeepsRuleMatching(t *testing.T) {
	source := "payload: [\"example.\\\n  com\"]\n"
	lines, _, err := ReadRulePayload(source)
	if err != nil || len(lines) != 1 || lines[0] != "example.com" {
		t.Fatalf("native continuation: %v %v", lines, err)
	}
	normalized, warnings := NormalizeYAML(source)
	if len(warnings) != 1 || normalized != source {
		t.Fatal("physical continuation must be delivered unchanged with one warning", warnings)
	}
	if err := ValidateRemote(Content{Data: []byte(normalized), Text: true}, remoteRule("domain", "yaml"), nil, ""); err != nil {
		t.Fatal(err)
	}
	result, err := Inspect(source, "classical", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	classical, _, err := ReadRulePayload(result.Content)
	if err != nil || len(classical) != 1 {
		t.Fatalf("%v %v", classical, err)
	}
	kind, value, ok := rules.ParseRuleLine(classical[0])
	if !ok {
		t.Fatal(classical)
	}
	for _, query := range []string{"example.com", "other.example", "sub.example.com"} {
		if rules.MatchQuery(query, kind, value) != rules.MatchQuery(query, "DOMAIN", "example.com") {
			t.Fatalf("matching changed for %s", query)
		}
	}
}

func TestAppendNeverSwallowsPolicyOrAliasErrors(t *testing.T) {
	var b strings.Builder
	b.WriteString("payload: []\na0: &a0 [example.com]\n")
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "a%d: &a%d [*a%d, *a%d]\n", i, i, i-1, i-1)
	}
	for _, tc := range []struct {
		source string
		kind   error
	}{
		{"payload: []\npayload: []", yamlx.ErrPolicy},
		{"payload: []\nx: !!str [x]", yamlx.ErrPolicy},
		{"payload: []\nx: &x [*x]", yamlx.ErrAliasRecursion},
		{b.String(), yamlx.ErrAliasBudget},
	} {
		for _, node := range []*yamlx.Node{nil, yamlx.Seq()} {
			got, err := AppendRuleLines(tc.source, []string{"example.com"}, node)
			if got != tc.source || !errors.Is(err, tc.kind) || !errors.Is(err, repo.ErrValidation) {
				t.Fatalf("append lines: %v", err)
			}
			got, err = AppendRulePayload(tc.source, "example.com", node)
			if got != tc.source || !errors.Is(err, tc.kind) || !errors.Is(err, repo.ErrValidation) {
				t.Fatalf("append payload: %v", err)
			}
		}
	}
}

func TestNativeRuleReencodeKeepsScalarTags(t *testing.T) {
	source := "# header\n{payload: [example.com], when: 2024-01-02, large: !!int 123456789012345678901234567890, binary: !!binary YQ==, custom: !label value, infinity: .inf, nan: .nan, digits: \"0123\"}\n"
	before, err := yamlx.Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	normalized, warnings := NormalizeYAML(source)
	after, err := yamlx.Parse([]byte(normalized))
	if len(warnings) != 0 || err != nil || !yamlx.Equal(before, after) {
		t.Fatalf("normalization changed tags: %v %v", warnings, err)
	}
	_, payload, err := ReadRulePayload(source)
	if err != nil {
		t.Fatal(err)
	}
	appended, err := AppendRuleLines(source, []string{"added.example"}, payload)
	if err != nil {
		t.Fatal(err)
	}
	after, err = yamlx.Parse([]byte(appended))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"when", "large", "binary", "custom", "infinity", "nan", "digits"} {
		if !yamlx.Equal(yamlx.Get(before, key), yamlx.Get(after, key)) {
			t.Fatalf("metadata tag/value changed: %s", key)
		}
	}
	lines, _, err := ReadRulePayload(appended)
	if err != nil || len(lines) != 2 || lines[0] != "example.com" || lines[1] != "added.example" {
		t.Fatalf("rule order: %v %v", lines, err)
	}
	if strings.Count(appended, "# header") != 1 {
		t.Fatal("header comment lost")
	}
}

func TestNilAppendRecognizesCommentedFlowButKeepsPlaintextOpaque(t *testing.T) {
	source := "# header\n{payload: [example.com]}\n"
	appended, err := AppendRuleLines(source, []string{"added.example"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	lines, payload, err := ReadRulePayload(appended)
	if err != nil || payload == nil || len(lines) != 2 || lines[0] != "example.com" || lines[1] != "added.example" {
		t.Fatalf("flow append: %v %v", lines, err)
	}
	plain := "DOMAIN-REGEX,foo: bar\nDOMAIN-REGEX,foo: baz\n"
	appended, err = AppendRuleLines(plain, []string{"DOMAIN,example.com"}, nil)
	if err != nil || appended != plain+"DOMAIN,example.com\n" {
		t.Fatalf("plaintext was interpreted as YAML: %v", err)
	}
}
