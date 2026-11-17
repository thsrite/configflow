package oxidns

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestReviewSReferences(t *testing.T) {
	runBoundary(t, "review_s.jsonl.gz", "zone_records_error", "format_error", "generate", "custom_files")
}

func TestReviewNativeRevision(t *testing.T) {
	t.Parallel()
	a, _ := FromJSON([]byte(`{"a":1,"b":[true,"中文",1.0],"large":18446744073709551616}`))
	b, _ := FromJSON([]byte(`{"large":18446744073709551616,"b":[true,"中文",1.0],"a":1}`))
	if Revision(a) != Revision(b) {
		t.Fatal("mapping order changed concurrency token")
	}
	original := Revision(a)
	for _, change := range []*Node{I(2), Float(1), B(true), S("1"), Null()} {
		set(b, "a", change)
		if original == Revision(b) {
			t.Fatalf("changed setting %s did not invalidate editor token", ToJSON(change))
		}
	}
	if Revision(clone(a)) != original {
		t.Fatal("read-only copy invalidated editor token")
	}
	// String upstream keys are stored inside native configs: preserve their existing names.
	upstream := M("addr", "https://dns.example/q?a=1&b=<x>")
	raw := []byte(`{"source":"remote","upstream":{"addr":"https://dns.example/q?a=1&b=<x>"}}`)
	sum := sha256.Sum256(raw)
	if got := EntryKey(upstream, "remote"); got != hex.EncodeToString(sum[:])[:12] {
		t.Fatalf("stored upstream chain renamed: %s", got)
	}
	for _, identity := range []*Node{S("中文-id"), Null(), B(true)} {
		sum := sha256.Sum256([]byte(identity.Value))
		if TagFor("rule", identity) != "rule_"+hex.EncodeToString(sum[:])[:16] {
			t.Fatal("native resource identity changed")
		}
	}
	if !bytes.Equal(canonicalJSON(a), canonicalJSON(clone(a))) {
		t.Fatal("canonical encoding unstable")
	}
}

func TestReviewYAMLEditRoundTrip(t *testing.T) {
	t.Parallel()
	raw := `# document comment
log: # log comment
  level: info # level comment
extra: # extra comment
  first: | # literal comment
    第一行
    第二行
  defaults: &defaults
    retained: yes # inherited comment
    count: 3
  inherited:
    <<: *defaults # merge comment
    count: 4
plugins: [] # plugins comment
# final comment
`
	doc, err := LoadNativeYAML(raw, NativeLabel)
	if err != nil {
		t.Fatal(err)
	}
	inherited := at(doc, "extra", "inherited")
	if str(get(inherited, "retained")) != "yes" || !equal(get(inherited, "count"), I(4)) {
		t.Fatal("YAML merge precedence lost")
	}
	// An edit must override a merged value without changing the inherited source.
	set(inherited, "count", I(5))
	if !equal(at(doc, "extra", "defaults", "count"), I(3)) {
		t.Fatal("override mutated merge source")
	}
	settings := M("config_mode", "native", "native_config", reviewDump(t, doc))
	result, err := ApplyCommonSettings(settings, M("log.level", "debug"), NewProfile(nil), "")
	if err != nil {
		t.Fatal(err)
	}
	text := str(get(result.Settings, "native_config"))
	for _, comment := range []string{"document comment", "log comment", "level comment", "extra comment", "literal comment", "inherited comment", "merge comment", "plugins comment", "final comment"} {
		if !strings.Contains(text, "# "+comment) {
			t.Errorf("lost comment %q:\n%s", comment, text)
		}
	}
	if !strings.Contains(text, "<<:") || !strings.Contains(text, "first: |") {
		t.Errorf("lost merge or literal style:\n%s", text)
	}
	roundTrip, err := LoadNativeYAML(text, NativeLabel)
	if err != nil {
		t.Fatal(err)
	}
	if str(at(roundTrip, "log", "level")) != "debug" || str(at(roundTrip, "extra", "inherited", "retained")) != "yes" || !equal(at(roundTrip, "extra", "inherited", "count"), I(5)) {
		t.Fatalf("edit lost values:\n%s", text)
	}
	if strings.Index(text, "first:") > strings.Index(text, "defaults:") {
		t.Fatal("user key order changed")
	}
	// Exercise the three-way merge used by template preview as well.
	current := clone(doc)
	base := clone(doc)
	next := clone(doc)
	set(get(next, "log"), "level", S("warn"))
	merged := (&merger{}).merge(base, current, next, nil)
	mergedText := reviewDump(t, merged)
	for _, comment := range []string{"document comment", "log comment", "level comment", "merge comment", "final comment"} {
		if !strings.Contains(mergedText, "# "+comment) {
			t.Errorf("template merge lost %s", comment)
		}
	}
	mergedDoc, err := LoadNativeYAML(mergedText, NativeLabel)
	if err != nil || str(at(mergedDoc, "log", "level")) != "warn" {
		t.Fatalf("template merge failed: %v\n%s", err, mergedText)
	}
	del(inherited, "retained")
	if has(inherited, "retained") {
		t.Fatal("deleted merge key reappeared")
	}
}

func TestReviewUnicodeWaiver(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"打印机.例子.", "bücher.example.", "שלום.example."} {
		r := row{Fn: "zone_records_error", Args: []json.RawMessage{json.RawMessage(strconv.Quote(name + " A 192.0.2.1")), json.RawMessage("false")}}
		if unicodeEdge(r) {
			t.Fatalf("ordinary name %s waived", name)
		}
	}
}

func reviewDump(t *testing.T, n *Node) string {
	t.Helper()
	text, err := dump(n)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestReviewYAMLScalarAndMergeForms(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		want       *Node
	}{
		{"annotated alias", "source: &d {value: 3}\ncopy: *d # alias comment\n", M("source", M("value", 3), "copy", M("value", 3))},
		{"sequence precedence", "first: &a {value: 1, retained: yes}\nsecond: &b {value: 2, other: no}\ncopy: {<<: [*a, *b], other: override}\n", M("first", M("value", 1, "retained", "yes"), "second", M("value", 2, "other", "no"), "copy", M("value", 1, "retained", "yes", "other", "override"))},
		{"explicit quoted scalar", "integer: !!int \"0x1F\"\nboolean: !!bool \"true\"\nstring: !!str true\n", M("integer", 31, "boolean", true, "string", "true")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := LoadNativeYAML(test.text, NativeLabel)
			if err != nil {
				t.Fatal(err)
			}
			text := reviewDump(t, clone(doc))
			actual, err := LoadNativeYAML(text, NativeLabel)
			if err != nil || !same(actual, test.want) {
				t.Fatalf("round trip changed values: %v\n%s", err, text)
			}
			if strings.Contains(test.text, "# alias comment") && !strings.Contains(text, "# alias comment") {
				t.Fatal("alias comment lost")
			}
		})
	}
	for _, raw := range []string{"copy: {<<: 3}", "copy: {<<: [1]}", "copy: &a {<<: *a}"} {
		if _, err := LoadNativeYAML(raw, NativeLabel); err == nil {
			t.Fatalf("invalid/cyclic merge accepted: %s", raw)
		}
	}
	p := profileOf(M("oxidns", M("custom_config", "extra: first\n---\nextra: second\n")))
	if _, err := Generate(p, ""); err == nil {
		t.Fatal("second YAML document silently dropped")
	}
}

func TestReviewEmbeddedSchemas(t *testing.T) {
	// Sequential: changes CONFIGFLOW_ROOT to prove schemas are embedded.
	for _, name := range []string{"global.json", "plugins.json", "dns-codes.json"} {
		embedded, err := schemaFS.ReadFile("schemas/" + name)
		if err != nil {
			t.Fatal(err)
		}
		original, err := os.ReadFile(filepath.Join("../../../backend/schemas/oxidns", name))
		if err != nil || !bytes.Equal(embedded, original) {
			t.Fatalf("schema %s differs from Python catalog: %v", name, err)
		}
	}
	t.Setenv("CONFIGFLOW_ROOT", t.TempDir())
	if FormatError("dns-qtype", S("A")) != "" {
		t.Fatal("schema validation depends on backend files")
	}
}

func TestReviewExpandedWithoutSources(t *testing.T) {
	t.Parallel()
	doc := M("plugins", L(M("tag", "test", "type", "cache", "args", M("size", -1))))
	issues := ValidateExpandedDocument(doc, nil)
	if !hasError(issues) {
		t.Fatal("invalid cache accepted without source metadata")
	}
	for _, issue := range issues {
		if issue.Source != "config.yaml" {
			t.Fatal("missing default source")
		}
	}
}

func TestReviewPublicEditorsPreserveYAML(t *testing.T) {
	t.Parallel()
	p := NewProfile(nil)
	draft, err := PrepareEditor(M("local_dns", "udp://192.0.2.53", "remote_dns", "https://dns.example/q"), p, "")
	if err != nil {
		t.Fatal(err)
	}
	text := "# retained draft comment\n" + str(get(draft, "native_config")) + `manual:
  description: | # multiline comment
    保留第一行
    保留第二行
  defaults: &d {inherited: retained}
  override:
    <<: *d # merge comment
`
	set(draft, "native_config", S(text))
	proposed := clone(get(draft, "template_settings"))
	set(get(proposed, "log"), "level", S("debug"))
	preview, err := PreviewTemplate(draft, proposed, p, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	policies, err := ApplyPolicies(draft, p, L(M("ip", "192.0.2.10", "name", "设备", "mode", "direct")), "")
	if err != nil {
		t.Fatal(err)
	}
	for name, settings := range map[string]*Node{"template preview": preview.Settings, "device policies": policies} {
		t.Run(name, func(t *testing.T) {
			text := str(get(settings, "native_config"))
			for _, fragment := range []string{"# retained draft comment", "# multiline comment", "# merge comment", "description: |", "<<:"} {
				if !strings.Contains(text, fragment) {
					t.Fatalf("lost %q:\n%s", fragment, text)
				}
			}
			doc, err := LoadNativeYAML(text, NativeLabel)
			if err != nil || str(at(doc, "manual", "override", "inherited")) != "retained" || str(at(doc, "manual", "description")) != "保留第一行\n保留第二行\n" {
				t.Fatalf("manual content changed: %v\n%s", err, text)
			}
		})
	}
}

func TestReviewMergeEffectiveFields(t *testing.T) {
	t.Parallel()
	doc, err := LoadNativeYAML("action: {<<: {action: upstream, source: remote}, upstream: {addr: 'https://dns.example/q'}}\nrefs: {<<: {exec: '$old'}, exec: '$new'}\n", NativeLabel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeAction(get(doc, "action"), false); err != nil {
		t.Fatal(err)
	}
	if refersTo(get(doc, "refs"), "old") || !refersTo(get(doc, "refs"), "new") {
		t.Fatal("inactive inherited reference changed dependency graph")
	}
}

func TestReviewGeneratedMergesAreExplicit(t *testing.T) {
	t.Parallel()
	p := profileOf(M("oxidns", M("config_mode", "native", "native_config", "defaults: &d {value: 1}\ncopy: {<<: *d, other: retained}\nplugins: []\n")))
	text, err := Generate(p, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "<<:") || strings.Contains(text, "*d") {
		t.Fatalf("delivered config relies on merge support:\n%s", text)
	}
	doc, err := LoadNativeYAML(text, NativeLabel)
	if err != nil || !equal(at(doc, "copy", "value"), I(1)) || str(at(doc, "copy", "other")) != "retained" {
		t.Fatalf("generated merge content lost: %v\n%s", err, text)
	}
}

func TestReviewLeadingBlankBlockScalar(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, text, want string }{
		{"fuzz regression", "A: |\n \n 行", "\n行"},
		{"ASCII", "A: |-\n\n  text", "\ntext"},
		{"multiple blanks", "A: |+\n\n\n  行\n\n", "\n\n行\n\n"},
		{"explicit indentation", "A: |2-\n  \n  行", "\n行"},
		{"space content", "A: |2-\n   \n  行", " \n行"},
		{"folded", "A: >-\n\n  行\n  次", "\n行 次"},
		{"non-leading blank", "A: |-\n  行\n\n  次", "行\n\n次"},
		{"four-byte rune", "A: |-\n\n  😀", "\n😀"},
		{"escaped carriage return", "A: \"行\\r次\"", "行\r次"},
		{"escaped NEL", "A: \"行\\N次\"", "行\u0085次"},
		{"leading line separator", "A: |-\n  \u2028  行", "\u2028行"},
		{"leading paragraph separator", "A: |-\n  \u2029  行", "\u2029行"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			doc, err := LoadNativeYAML(test.text, NativeLabel)
			if err != nil {
				t.Fatal(err)
			}
			if get(doc, "A").Value != test.want {
				t.Fatalf("unexpected input value: %q", get(doc, "A").Value)
			}
			encoded, err := dump(doc)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := LoadNativeYAML(encoded, NativeLabel)
			if err != nil || !same(doc, actual) {
				t.Fatalf("block scalar changed: %v before=%q encoded=%q", err, test.want, encoded)
			}
			// Also cover template YAML and programmatically constructed/replaced values.
			template, ok, err := safeLoad(test.text)
			if !ok || err != nil {
				t.Fatalf("template parse: %v", err)
			}
			if round, err := LoadNativeYAML(reviewDump(t, template), NativeLabel); err != nil || !same(round, doc) {
				t.Fatalf("template round trip: %v", err)
			}
			value := M("A", test.want)
			if round, err := LoadNativeYAML(reviewDump(t, value), NativeLabel); err != nil || !same(round, doc) {
				t.Fatalf("constructed round trip: %v", err)
			}
			set(doc, "A", S("\n替换"))
			if round, err := LoadNativeYAML(reviewDump(t, doc), NativeLabel); err != nil || str(get(round, "A")) != "\n替换" {
				t.Fatalf("replacement round trip: %v", err)
			}
		})
	}
}
