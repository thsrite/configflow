package rulefetch

// Ported from backend/test_remote_ruleset_validation.py,
// test_mihomo_yaml_delivery.py, test_ruleset_classical_format.py,
// test_ruleset_domain_format.py (direct cases), test_rule_syntax.py (the
// cases through ruleset_content) and test_website_identity.py (pure cases).

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"

	"github.com/klauspost/compress/zstd"
	"gopkg.in/yaml.v3"
)

func remoteRule(behavior, format string) model.RuleConfig {
	return model.RuleConfig{
		Name:       "测试订阅",
		Behavior:   strp(behavior),
		Format:     strp(format),
		SourceType: strp("url"),
	}
}

func asRemote(t *testing.T, err error) *RemoteRuleValidationError {
	t.Helper()
	var remote *RemoteRuleValidationError
	if !errors.As(err, &remote) {
		t.Fatalf("want RemoteRuleValidationError, got %v", err)
	}
	if !errors.Is(err, repo.ErrValidation) {
		t.Fatalf("RemoteRuleValidationError must be a ProfileValidationError")
	}
	return remote
}

func firstProblem(e *RemoteRuleValidationError) Problem { return e.Errors[0] }

func rulePayloadYAML(t *testing.T, lines ...string) string {
	t.Helper()
	out, err := yamlx.Marshal(struct {
		Payload []string `yaml:"payload"`
	}{Payload: lines})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func safeLoadJSON(t *testing.T, text string) json.RawMessage {
	t.Helper()
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(data)
}

func safeLoadMap(t *testing.T, text string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(safeLoadJSON(t, text), &m); err != nil {
		t.Fatalf("safe_load(%q): %v", text, err)
	}
	return m
}

// test_valid_sources_preserve_content_and_metadata
func TestValidSourcesPreserveContentAndMetadata(t *testing.T) {
	cases := map[string][]string{
		"domain": {"example.com", "+.example.com", ".example.com", "*.example.com", "*.*.microsoft.com", "xbox.*.example.com", "*", "+.*", ".*", "_srv.example.com"},
		"ipcidr": {"192.0.2.0/24", "192.0.2.3/32", "2001:db8::/32", "2001:db8::1/128"},
		"classical": {"DOMAIN,example.com", "DOMAIN-SUFFIX,example.com", "NETWORK,tcp", `DOMAIN-REGEX,^(foo|bar)\.example\.com$`,
			"PROCESS-NAME,app.exe", "AND,((DOMAIN,example.com),(NETWORK,tcp))", "IP-CIDR,192.0.2.0/24,no-resolve",
			"IP-CIDR6,2001:db8::/32,no-resolve", "+.example.com", "example.com", "192.0.2.3", "2001:db8::1", "192.0.2.0/24"},
	}
	for behavior, lines := range cases {
		for _, format := range []string{"text", "yaml"} {
			var source string
			if format == "text" {
				source = "# source\r\n\r\n" + strings.Join(lines, "\r\n") + "\r\n"
			} else {
				source = rulePayloadYAML(t, lines...)
			}
			source = "\uFEFF" + source
			for _, asBytes := range []bool{false, true} {
				content := Content{Data: []byte(source), Text: !asBytes}
				item := remoteRule(behavior, format)
				before := encode(item)
				if err := ValidateRemote(content, item, nil, ""); err != nil {
					t.Errorf("%s/%s bytes=%v: %v", behavior, format, asBytes, err)
				}
				if encode(item) != before {
					t.Errorf("rule mutated")
				}
			}
		}
	}
}

// test_type_errors_expose_original_line_and_reason
func TestTypeErrorsExposeOriginalLineAndReason(t *testing.T) {
	cases := [][3]string{
		{"domain", "DOMAIN-SUFFIX,example.com", "Classical"}, {"domain", "192.0.2.0/24", "域名"},
		{"domain", "full:example.com", "域名"}, {"domain", "foo..example.com", "域名"},
		{"domain", "*suffix.example.com", "域名"}, {"ipcidr", "192.0.2.1", "/32"}, {"ipcidr", "2001:db8::1", "/128"},
		{"ipcidr", "IP-CIDR,192.0.2.0/24", "网段"}, {"ipcidr", "+.example.com", "CIDR"}, {"ipcidr", "192.0.2.0/33", "前缀"},
		{"classical", ".example.com", "语义"}, {"classical", "*.example.com", "语义"}, {"classical", "full:example.com", "语义"},
		{"classical", "DOMAIN-SHUFFIX,example.com", "DOMAIN-SUFFIX"}, {"classical", "DOMAIN,", "为空"},
		{"classical", "IP-CIDR,192.0.2.0/33", "前缀"}, {"classical", "RULE-SET,nested", "不支持"}, {"classical", "MATCH", "不支持"},
	}
	for _, c := range cases {
		for _, format := range []string{"text", "yaml"} {
			source, line := "# source\n"+c[1], 2
			if format == "yaml" {
				source, line = "# source\npayload:\n  - "+jsonDumps(c[1], true), 3
			}
			e := asRemote(t, ValidateRemote(Content{Data: []byte(source)}, remoteRule(c[0], format), nil, ""))
			first := firstProblem(e)
			if first.Line != line || textOr(first.Before, "") != c[1] || !strings.Contains(first.Message, c[2]) {
				t.Errorf("%v %s: %v", c, format, encode(first))
			}
			for _, want := range []string{"测试订阅", c[0] + "/" + format, fmt.Sprintf("第 %d 行", line), c[1]} {
				if !strings.Contains(e.Error(), want) {
					t.Errorf("%v %s: %q lacks %q", c, format, e.Error(), want)
				}
			}
		}
	}
}

// test_reports_every_invalid_entry_without_accepting_partial_set
func TestReportsEveryInvalidEntry(t *testing.T) {
	e := asRemote(t, ValidateRemote(Content{Data: []byte("valid.test\nDOMAIN-SUFFIX,mixed.test\n192.0.2.0/24\nlast.test\n"), Text: true}, remoteRule("domain", "text"), nil, ""))
	if len(e.Errors) != 2 || e.Errors[0].Line != 2 || e.Errors[1].Line != 3 {
		t.Fatal(encode(e.Errors))
	}
	if !strings.Contains(e.Error(), "共 2 处错误") {
		t.Fatal(e.Error())
	}
}

// test_format_errors_have_physical_source_locations
func TestFormatErrorsHavePhysicalSourceLocations(t *testing.T) {
	cases := []struct {
		source, format string
		line           int
		reason         string
	}{
		{"payload:\n  - example.com\n", "text", 1, "YAML"}, {"example.com\n", "yaml", 1, "payload"},
		{"payload:\n  - true\n", "yaml", 2, "单行文本"}, {"payload:\n  - 123\n", "yaml", 2, "单行文本"},
		{"payload:\n  - [example.com]\n", "yaml", 2, "单行文本"}, {"payload:\n  - |\n    example.com\n", "yaml", 2, "单行文本"},
		{"payload: [\"example.com\\nother.com\"]\n", "yaml", 1, "单行文本"}, {"payload: []\npayload: []\n", "yaml", 2, "重复键"},
		{"payload:\n  - &x example.com\n  - *x\n", "yaml", 2, "锚点"}, {"payload: example.com\n", "yaml", 1, "列表"},
		{"payload: [example.com\n", "yaml", 1, "YAML 格式无效"},
	}
	for _, c := range cases {
		first := firstProblem(asRemote(t, ValidateRemote(Content{Data: []byte(c.source), Text: true}, remoteRule("domain", c.format), nil, "")))
		if first.Line != c.line || !strings.Contains(first.Message, c.reason) {
			t.Errorf("%q: %s", c.source, encode(first))
		}
	}
}

// test_binary_content_is_not_lossily_decoded
func TestBinaryContentIsNotLossilyDecoded(t *testing.T) {
	for _, content := range [][]byte{[]byte("example.com\n\xff"), []byte("\xff"), []byte("example.com\r\xff"), append([]byte("example.com "), 0xff)} {
		e := asRemote(t, ValidateRemote(Content{Data: content}, remoteRule("domain", "text"), nil, ""))
		want := 2
		if string(content) == "\xff" {
			want = 1
		}
		if !strings.Contains(e.Error(), "UTF-8") || firstProblem(e).Line != want {
			t.Errorf("%q: %s", content, e.Error())
		}
	}
}

// test_legacy_format_inference_and_library_metadata_match_delivery
func TestLegacyFormatInference(t *testing.T) {
	item := model.RuleConfig{Name: "订阅", Behavior: strp("domain"), URL: strp("https://rules.test/list.yaml")}
	if err := ValidateRemote(Content{Data: []byte("payload: [example.com]"), Text: true}, item, nil, ""); err != nil {
		t.Fatal(err)
	}
	item.URL = strp("https://rules.test/list.txt")
	library := []model.RuleLibraryItem{{Name: "订阅", Format: strp("yaml")}}
	if err := ValidateRemote(Content{Data: []byte("payload: [example.com]"), Text: true}, item, library, ""); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRemote(Content{Data: []byte("payload: [example.com]"), Text: true}, item, nil, ""); err == nil || !strings.Contains(err.Error(), "YAML") {
		t.Fatal(err)
	}
}

// test_invalid_declared_metadata_is_not_silently_inferred
func TestInvalidDeclaredMetadata(t *testing.T) {
	for _, kv := range [][2]string{{"behavior", "domain-shuffix"}, {"format", "json"}} {
		rule := remoteRule("domain", "text")
		if kv[0] == "behavior" {
			rule.Behavior = strp(kv[1])
		} else {
			rule.Format = strp(kv[1])
		}
		asRemote(t, ValidateRemote(Content{Data: []byte("example.com"), Text: true}, rule, nil, ""))
	}
}

var nativeMRS = map[string]string{
	"domain": "KLUv/QQATQEAlAFNUlMBACgGqqqqAA1tb2MuZWxwbWF4ZS4rBQBgoJ3naGbBMgGu0wItMyBa",
	"ipcidr": "KLUv/QQALQEAckEFDOBBAT/YP2OX/K7e981Rn6NeRAQQBQBDEQYkWAqsEMDginLvahA=",
}

// test_native_mrs_type_matches_and_binary_bytes_are_unchanged
func TestNativeMRS(t *testing.T) {
	for behavior, encoded := range nativeMRS {
		content, _ := base64.StdEncoding.DecodeString(encoded)
		if err := ValidateRemote(Content{Data: content}, remoteRule(behavior, "mrs"), nil, ""); err != nil {
			t.Fatal(err)
		}
		other := "domain"
		if behavior == "domain" {
			other = "ipcidr"
		}
		if err := ValidateRemote(Content{Data: content}, remoteRule(other, "mrs"), nil, ""); err == nil || !strings.Contains(err.Error(), "实际类型为 "+behavior) {
			t.Fatal(err)
		}
		if err := ValidateRemote(Content{Data: content}, remoteRule("classical", "mrs"), nil, ""); err == nil || !strings.Contains(err.Error(), "不支持 classical") {
			t.Fatal(err)
		}
		if base64.StdEncoding.EncodeToString(content) != encoded {
			t.Fatal("bytes changed")
		}
	}
}

func mrsEnvelope(magic string, behavior byte, count, extra int64, body string) []byte {
	raw := []byte(magic)
	raw = append(raw, behavior)
	for _, v := range []int64{count, extra} {
		for i := 7; i >= 0; i-- {
			raw = append(raw, byte(uint64(v)>>(8*i)))
		}
	}
	raw = append(raw, body...)
	encoder, _ := zstd.NewWriter(nil, zstd.WithEncoderCRC(true))
	return encoder.EncodeAll(raw, nil)
}

// test_mrs_envelope_or_compression_failure_rejects_refresh
func TestMRSEnvelopeFailures(t *testing.T) {
	good := mrsEnvelope("MRS\x01", 0, 1, 0, "body")
	cases := []struct {
		content Content
		reason  string
	}{
		{Content{Data: []byte("text"), Text: true}, "原始二进制"}, {Content{Data: []byte("example.com")}, "压缩流"},
		{Content{Data: []byte{}}, "不完整"}, {Content{Data: good[:len(good)-1]}, "不完整"},
		{Content{Data: append(append([]byte{}, good...), "bad"...)}, "压缩流"}, {Content{Data: mrsEnvelope("BAD!", 0, 1, 0, "body")}, "文件头"},
		{Content{Data: mrsEnvelope("MRS\x01", 2, 1, 0, "body")}, "未知规则类型"}, {Content{Data: mrsEnvelope("MRS\x01", 0, -1, 0, "body")}, "数量"},
		{Content{Data: mrsEnvelope("MRS\x01", 0, 1, -1, "body")}, "扩展字段"}, {Content{Data: mrsEnvelope("MRS\x01", 0, 1, 1000, "body")}, "扩展字段"},
		{Content{Data: mrsEnvelope("MRS\x01", 0, 1, 0, "")}, "正文缺失"},
	}
	for i, c := range cases {
		err := ValidateRemote(c.content, remoteRule("domain", "mrs"), nil, "")
		if err == nil || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

// test_mrs_checksum_corruption_is_rejected
func TestMRSChecksumCorruption(t *testing.T) {
	content, _ := base64.StdEncoding.DecodeString(nativeMRS["domain"])
	content[len(content)-1] ^= 1
	if err := ValidateRemote(Content{Data: content}, remoteRule("domain", "mrs"), nil, ""); err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Fatal(err)
	}
}

// test_remote_error_supports_source_resolution_message
func TestRemoteErrorMessageForm(t *testing.T) {
	e := NewRemoteRuleMessage("来源配置有歧义")
	if e.Error() != "来源配置有歧义" || encode(e.Errors) != `[{"line":0,"before":"","message":"来源配置有歧义"}]` {
		t.Fatal(e.Error(), encode(e.Errors))
	}
}

// test_text_error_keeps_original_indentation_for_diagnostics
func TestTextErrorKeepsIndentation(t *testing.T) {
	e := asRemote(t, ValidateRemote(Content{Data: []byte("valid.example\n  DOMAIN,wrong.example  \n"), Text: true}, remoteRule("domain", "text"), nil, ""))
	if textOr(firstProblem(e).Before, "") != "  DOMAIN,wrong.example  " {
		t.Fatal(encode(e.Errors))
	}
}

// test_mihomo_yaml_delivery.py

func TestFlowLayoutBecomesBlockWithoutChangingValues(t *testing.T) {
	for _, source := range []string{
		"# header\npayload: [\"+.tmdb.org\", \"192.0.2.0/24\"] # tail\nother: keep\n",
		"\"payload\": [\n  \"+.tmdb.org\", # suffix\n  \"192.0.2.0/24\",\n]\nother: keep\n",
		`{"other":"keep","payload":["+.tmdb.org","192.0.2.0/24"]}`,
		"\uFEFF{\"other\":\"keep\",\"payload\":[\"+.tmdb.org\",\"192.0.2.0/24\"]}\n",
		"# header\n{other: keep, # metadata\npayload: [\"+.tmdb.org\", \"192.0.2.0/24\"]} # tail\n",
		"--- {other: keep, payload: [\"+.tmdb.org\", \"192.0.2.0/24\"]}\n",
	} {
		result, warnings := NormalizeYAML(source)
		if len(warnings) != 0 || !testjson.Equal(safeLoadJSON(t, source), safeLoadJSON(t, result)) {
			t.Fatalf("%q -> %q %v", source, result, warnings)
		}
		if root, err := yamlx.Parse([]byte(result)); err != nil || root.Style&yaml.FlowStyle != 0 || payloadPairs(root)[0][1].Style&yaml.FlowStyle != 0 {
			t.Fatalf("still flow: %q", result)
		}
		for _, comment := range []string{"# header", "# tail", "# suffix", "# metadata"} {
			if strings.Contains(source, comment) && !strings.Contains(result, comment) {
				t.Fatalf("lost %s: %q", comment, result)
			}
		}
		if strings.HasPrefix(result, "\uFEFF") != strings.HasPrefix(source, "\uFEFF") {
			t.Fatal("BOM")
		}
		if again, w := NormalizeYAML(result); again != result || len(w) != 0 {
			t.Fatal("not idempotent")
		}
	}
}

func TestExistingBlockAndEmptyPayloadAreBytePreserved(t *testing.T) {
	for _, source := range []string{"payload:\n  - \"DOMAIN,tmdb.org\" # preserve\nother: keep\n", "\uFEFF\"payload\":\r\n  - \"+.tmdb.org\"\r\n", "payload: []\n"} {
		if result, warnings := NormalizeYAML(source); result != source || len(warnings) != 0 {
			t.Errorf("%q -> %q %v", source, result, warnings)
		}
	}
}

func TestAmbiguousYAMLIsUnchangedWithWarning(t *testing.T) {
	for _, source := range []string{
		`payload: ["+.tmdb.org"`, "payload: true", "payload: [true]", "payload: [\"tmdb.org\"]\npayload: [\"other.org\"]",
		"base: &list [\"tmdb.org\"]\npayload: *list", `payload: [&host "tmdb.org", *host]`, "payload:\n  - |\n    tmdb.org\n",
		`{payload: ["tmdb.org"], other: 1, other: 2}`, `{payload: ["tmdb.org"], metadata: {key: 1, key: 2}}`,
	} {
		result, warnings := NormalizeYAML(source)
		if result != source || len(warnings) == 0 || warnings[0].Message == "" {
			t.Errorf("%q -> %q %v", source, result, warnings)
		}
	}
}

func TestHashInRuleValueIsNotMovedIntoAComment(t *testing.T) {
	source := "payload: [\"DOMAIN-REGEX,(?x)^tmdb # (\", \"DOMAIN,literal#value\"] # note\n"
	result, warnings := NormalizeYAML(source)
	if len(warnings) != 0 || !testjson.Equal(safeLoadJSON(t, source), safeLoadJSON(t, result)) || strings.Count(result, "# note") != 1 {
		t.Fatalf("%q %v", result, warnings)
	}
}

func TestLongRulesAreNotWrapped(t *testing.T) {
	value := "DOMAIN-REGEX," + strings.Repeat("abc ", 100)
	result, warnings := NormalizeYAML(jsonDumps(value, true)[0:0] + `{"payload": [` + jsonDumps(value, true) + `]}`)
	if len(warnings) != 0 {
		t.Fatal(warnings)
	}
	_, payload, _ := ReadRulePayload(result)
	if payload == nil || strings.Count(strings.TrimSpace(result), "\n") != 1 {
		t.Fatal("wrapped")
	}
	if !testjson.Equal(map[string]any{"payload": []string{value}}, safeLoadJSON(t, result)) {
		t.Fatal(result)
	}
}

func TestDeliveryWrapperPreservesEachBehavior(t *testing.T) {
	for _, c := range [][2]string{{"domain", "+.tmdb.org"}, {"ipcidr", "192.0.2.0/24"}, {"classical", "DOMAIN-SUFFIX,tmdb.org"}} {
		source := `{"payload": [` + jsonDumps(c[1], true) + `]}`
		result, warnings, err := NormalizeRuleset(source, c[0], "yaml")
		if err != nil || len(warnings) != 0 || !testjson.Equal(map[string]any{"payload": []string{c[1]}}, safeLoadJSON(t, result)) {
			t.Fatal(result, warnings, err)
		}
		if again, w, _ := NormalizeRuleset(result, c[0], "yaml"); again != result || len(w) != 0 {
			t.Fatal("not idempotent")
		}
	}
}

func TestDeliveryWrapperCombinesClassicalRepairsAndLayout(t *testing.T) {
	source := "payload: [\"+.tmdb.org\", \"*.other.org\"]\n"
	result, warnings, _ := NormalizeRuleset(source, "classical", "yaml")
	if !testjson.Equal([]string{"DOMAIN-SUFFIX,tmdb.org", "*.other.org"}, safeLoadMap(t, result)["payload"]) ||
		len(warnings) != 1 || textOr(warnings[0].Before, "") != "*.other.org" {
		t.Fatal(result, warnings)
	}
	for _, c := range [][2]string{{"domain", "text"}, {"classical", "mrs"}} {
		if r, w, _ := NormalizeRuleset(source, c[0], c[1]); r != source || len(w) != 0 {
			t.Fatal(c, r, w)
		}
	}
}

// test_ruleset_classical_format.py

func normalizeOK(t *testing.T, source string) (string, []Problem) {
	t.Helper()
	out, warnings, err := NormalizeClassical(source)
	if err != nil {
		t.Fatal(err)
	}
	return out, warnings
}

func TestNormalizeSafeBareEntries(t *testing.T) {
	cases := [][2]string{
		{"+.tmdb.org", "DOMAIN-SUFFIX,tmdb.org"}, {"+.x", "DOMAIN-SUFFIX,x"}, {"api.tmdb.org", "DOMAIN,api.tmdb.org"}, {"x", "DOMAIN,x"},
		{"API.Example.COM", "DOMAIN,API.Example.COM"}, {"xn--fsqu00a.xn--0zwm56d", "DOMAIN,xn--fsqu00a.xn--0zwm56d"},
		{"192.0.2.1", "IP-CIDR,192.0.2.1/32,no-resolve"}, {"192.0.2.9/24", "IP-CIDR,192.0.2.9/24,no-resolve"},
		{"0.0.0.0/0", "IP-CIDR,0.0.0.0/0,no-resolve"}, {"2001:db8::1", "IP-CIDR6,2001:db8::1/128,no-resolve"},
		{"2001:DB8::9/32", "IP-CIDR6,2001:DB8::9/32,no-resolve"}, {"::1", "IP-CIDR6,::1/128,no-resolve"},
		{"::/0", "IP-CIDR6,::/0,no-resolve"}, {"::ffff:192.0.2.1", "IP-CIDR6,::ffff:192.0.2.1/128,no-resolve"},
		// test_utf8_bom_is_preserved_without_hiding_the_first_entry
		{"\uFEFF+.tmdb.org\r\n", "\uFEFFDOMAIN-SUFFIX,tmdb.org\r\n"}, {"\uFEFF  example.com\n", "\uFEFF  DOMAIN,example.com\n"},
		{"\uFEFF# comment\n+.tmdb.org", "\uFEFF# comment\nDOMAIN-SUFFIX,tmdb.org"},
		{"\uFEFFpayload: [\"+.tmdb.org\"]\n", "\uFEFFpayload: [\"DOMAIN-SUFFIX,tmdb.org\"]\n"},
		{"\uFEFF{\"payload\":[\"example.com\"]}", "\uFEFF{\"payload\":[\"DOMAIN,example.com\"]}"}, {"\uFEFF", "\uFEFF"},
	}
	for _, c := range cases {
		for _, source := range []string{c[0], c[1]} {
			if out, w := normalizeOK(t, source); out != c[1] || len(w) != 0 {
				t.Errorf("%q -> %q %v", source, out, w)
			}
		}
	}
}

func TestUnchangedLinesAreBytePreserved(t *testing.T) {
	for _, source := range []string{"", "\n \n", "# comment\n// comment\n", "DOMAIN-SUFFIX,tmdb.org\nDOMAIN,example.com\nIP-CIDR,192.0.2.0/24",
		"DOMAIN-KEYWORD,google\nIP-CIDR6,2001:db8::/32,no-resolve", "  DOMAIN,example.com \r\n# +.tmdb.org\r\n\t// 192.0.2.1\r\n",
		"payload: []\n", "payload: [\"DOMAIN,x\", \"\", \"// note\"]\n", "payload:\n  - \"DOMAIN,x\" # existing\nother: api.test\n"} {
		if out, w := normalizeOK(t, source); out != source || len(w) != 0 {
			t.Errorf("%q -> %q %v", source, out, w)
		}
	}
}

func TestAmbiguousOrInvalidEntriesArePreservedWithLineWarning(t *testing.T) {
	for _, source := range []string{".example.com", "*.example.com", "full:example.com", "domain:example.com", "regexp:^example",
		"ip:192.0.2.1", "+.", "+.bad..test", "+.192.0.2.1", "foo..test", "bad-.test", "-bad.test", "https://example.com",
		"example.com/path", "192.0.2.999", "192.0.2.1/33", "2001:db8::/129", "192.0.2.0/255.255.255.0", "fe80::1%eth0",
		"example.com # inline text", strings.Repeat("a", 64) + ".example"} {
		out, w := normalizeOK(t, source)
		if out != source || len(w) != 1 || w[0].Line != 1 || textOr(w[0].Before, "") != source || w[0].Message == "" {
			t.Errorf("%q -> %q %v", source, out, encode(w))
		}
	}
}

func TestMixedTextKeepsOrderIndentationNewlinesAndWarnings(t *testing.T) {
	source := "# Header\r\n  +.tmdb.org \r\n\r\napi.test\r\n192.0.2.1\r\nDOMAIN,x\r\n  *.test\r\n// End"
	expected := "# Header\r\n  DOMAIN-SUFFIX,tmdb.org \r\n\r\nDOMAIN,api.test\r\nIP-CIDR,192.0.2.1/32,no-resolve\r\nDOMAIN,x\r\n  *.test\r\n// End"
	out, w := normalizeOK(t, source)
	if out != expected || len(w) != 1 || w[0].Line != 7 {
		t.Fatal(out, encode(w))
	}
	if again, w2 := normalizeOK(t, out); again != out || encode(w2) != encode(w) {
		t.Fatal("not stable")
	}
}

func TestYAMLPayloadRewriteIsLosslessStableAndValid(t *testing.T) {
	for _, source := range []string{
		"# Header\npayload:\n  - \"+.tmdb.org\" # suffix\n  - api.test\n  - 192.0.2.1\n  - \"2001:db8::/32\"\n  - \"DOMAIN,old.test\"\n  - \"*.other.test\"\n  - \"# embedded comment\"\nother: preserved\n",
		"payload: [\"+.tmdb.org\", api.test, 192.0.2.1, \"2001:db8::/32\", \"DOMAIN,old.test\", \"*.other.test\", \"# embedded comment\",]\nother: preserved\n",
		`{"payload":["+.tmdb.org", "api.test", "192.0.2.1", "2001:db8::/32", "DOMAIN,old.test", "*.other.test", "# embedded comment"],"other":"preserved"}`,
	} {
		out, w := normalizeOK(t, source)
		want := map[string]any{
			"payload": []string{"DOMAIN-SUFFIX,tmdb.org", "DOMAIN,api.test", "IP-CIDR,192.0.2.1/32,no-resolve",
				"IP-CIDR6,2001:db8::/32,no-resolve", "DOMAIN,old.test", "*.other.test", "# embedded comment"},
			"other": "preserved",
		}
		if !testjson.Equal(want, safeLoadJSON(t, out)) || len(w) != 1 || textOr(w[0].Before, "") != "*.other.test" {
			t.Fatal(out, encode(w))
		}
		if strings.Contains(source, "# suffix") && !strings.Contains(out, "# suffix") || !strings.Contains(out, `"DOMAIN,old.test"`) ||
			strings.HasSuffix(source, "\n") != strings.HasSuffix(out, "\n") {
			t.Fatal(out)
		}
		if again, w2 := normalizeOK(t, out); again != out || encode(w2) != encode(w) {
			t.Fatal("not stable")
		}
	}
}

func TestInvalidOrAmbiguousYAMLIsNeverPartiallyRewritten(t *testing.T) {
	for _, source := range []string{`payload: ["+.example.com",`, "payload: true", "payload: [true]", "payload: []\npayload: []",
		"a: &rules [\"+.example.com\"]\npayload: *rules", `payload: [&rule "+.example.com", *rule]`, "payload:\n  - |\n    example.com\n"} {
		out, w := normalizeOK(t, source)
		checked, _ := Inspect(source, "classical", "")
		if out != source || len(w) == 0 || checked.Content != source || len(checked.Errors) == 0 || len(checked.Changes) != 0 {
			t.Errorf("%q", source)
		}
	}
}

func inspectionWarnings(r *Inspection) []Problem {
	if r == nil || r.Warnings == nil {
		return nil
	}
	return *r.Warnings
}

func TestInspectionProposesExplicitChanges(t *testing.T) {
	result, _ := Inspect("+.tmdb.org\n192.0.2.1\n*.example.com", "classical", "")
	if encode(result.Changes) != `[{"line":1,"before":"+.tmdb.org","after":"DOMAIN-SUFFIX,tmdb.org"},{"line":2,"before":"192.0.2.1","after":"IP-CIDR,192.0.2.1/32,no-resolve"}]` ||
		len(result.Errors) != 0 || inspectionWarnings(result)[0].Line != 3 ||
		result.Content != "DOMAIN-SUFFIX,tmdb.org\nIP-CIDR,192.0.2.1/32,no-resolve\n*.example.com" {
		t.Fatal(encode(result))
	}
}

func classicalRule(content string) model.RuleConfig {
	return model.RuleConfig{
		SourceType: strp("content"),
		Behavior:   strp("classical"),
		Content:    strp(content),
		Enabled:    boolp(true),
	}
}

func TestClassicalValidationOnlyChecksEditedHandwrittenContent(t *testing.T) {
	existing := classicalRule("+.tmdb.org")
	before := encode(existing)
	ok := func(rule model.RuleConfig, previous *model.RuleConfig) {
		t.Helper()
		if err := ValidateContent(rule, previous); err != nil {
			t.Fatal(err)
		}
	}
	bad := func(rule model.RuleConfig) {
		t.Helper()
		if err := ValidateContent(rule, nil); !errors.Is(err, repo.ErrValidation) {
			t.Fatal(err)
		}
	}
	ok(existing, nil)
	r := existing
	r.Enabled = boolp(false)
	ok(r, &existing)
	r = existing
	r.Name = "Renamed"
	ok(r, &existing)
	r = existing
	r.SourceType = strp("url")
	ok(r, nil)
	r = existing
	r.Behavior = strp("ipcidr")
	bad(r)
	r = existing
	r.Content = strp("*.example.com\nfull:example.com")
	ok(r, nil)
	r = existing
	r.Content = strp("payload: [true]")
	bad(r)
	r = existing
	r.Content = strp("+.changed.test")
	ok(r, &existing)
	if encode(existing) != before {
		t.Fatal("mutated")
	}
}

func classicalMatches(query, line string) bool {
	parts := strings.Split(line, ",")
	kind, value := parts[0], parts[1]
	switch kind {
	case "DOMAIN":
		return strings.EqualFold(query, value)
	case "DOMAIN-SUFFIX":
		q, v := strings.ToLower(query), strings.ToLower(value)
		return q == v || strings.HasSuffix(q, "."+v)
	}
	addr, err := netip.ParseAddr(query)
	prefix, err2 := netip.ParsePrefix(value)
	return err == nil && err2 == nil && prefix.Masked().Contains(addr)
}

func TestNormalizedClassicalSemanticsAgreeWithRuleMatcher(t *testing.T) {
	for _, line := range []string{"+.tmdb.org", "api.tmdb.org", "192.0.2.1", "192.0.2.9/24", "2001:db8::1", "2001:db8::9/32"} {
		normalized, w := normalizeOK(t, line)
		if len(w) != 0 {
			t.Fatal(w)
		}
		kind, value, _ := rules.ParseRuleLine(line)
		for _, query := range []string{"tmdb.org", "api.tmdb.org", "sub.api.tmdb.org", "eviltmdb.org", "other.test",
			"192.0.2.1", "192.0.2.200", "198.51.100.1", "2001:db8::1", "2001:db8:1::2", "2001:db9::1"} {
			if rules.MatchQuery(query, kind, value) != classicalMatches(query, normalized) {
				t.Errorf("%s %s", line, query)
			}
		}
	}
}

func TestQuotedYAMLPayloadKeysUseTheSameLosslessRewrite(t *testing.T) {
	for _, key := range []string{"payload", `"payload"`, "'payload'"} {
		for _, prefix := range []string{"", "\uFEFF"} {
			source := prefix + "# keep\n" + key + ":\n  - '+.tmdb.org' # suffix\n"
			expected := prefix + "# keep\n" + key + ":\n  - \"DOMAIN-SUFFIX,tmdb.org\" # suffix\n"
			native := strings.Replace(source, "'+.tmdb.org'", "DOMAIN-SUFFIX,tmdb.org", 1)
			if out, w := normalizeOK(t, source); out != native || len(w) != 0 {
				t.Fatal(out, w)
			}
			for _, unchanged := range []string{expected, native} {
				if out, w := normalizeOK(t, unchanged); out != unchanged || len(w) != 0 {
					t.Fatal("no-op changed bytes", out, w)
				}
			}
			lines, payload, err := ReadRulePayload(source)
			if err != nil || len(lines) != 1 || lines[0] != "+.tmdb.org" || payload == nil {
				t.Fatal(lines, err)
			}
		}
	}
}

func TestTaggedPayloadAnchorNeverLeavesADanglingAlias(t *testing.T) {
	for _, c := range [][2]string{{"domain", "DOMAIN-SUFFIX,tmdb.org"}, {"classical", "+.tmdb.org"}} {
		for _, properties := range []string{"!!str &host", "&host !!str"} {
			source := "payload:\n  - " + properties + " " + c[1] + "\nother: *host\n"
			if safeLoadMap(t, source)["other"] != c[1] {
				t.Fatal("yaml")
			}
			checked, _ := Inspect(source, c[0], "")
			if checked.Content != source || len(checked.Errors) == 0 || len(checked.Changes) != 0 {
				t.Fatal(encode(checked))
			}
			if c[0] == "classical" {
				if out, w := normalizeOK(t, source); out != source || len(w) == 0 {
					t.Fatal(out)
				}
			}
		}
	}
}

func TestAnchorsOutsidePayloadAndLiteralAmpersandsAreUntouched(t *testing.T) {
	source := "note: &note \"a&b\"\ncopy: *note\npayload: [\"+.tmdb.org\", \"DOMAIN-REGEX,a&b\"]\n"
	out, w := normalizeOK(t, source)
	if len(w) != 0 || out != strings.Replace(source, "+.tmdb.org", "DOMAIN-SUFFIX,tmdb.org", 1) ||
		safeLoadMap(t, out)["copy"] != "a&b" {
		t.Fatal(out, w)
	}
}

// test_ruleset_domain_format.py (direct cases; the MosDNS conversions are
// deferred to C21, the route cases to C32/C33/C37).

func TestDomainPrefixErrorsPreserveText(t *testing.T) {
	for _, source := range []string{
		"# domains\r\nDOMAIN-SUFFIX,x.ai\r\nDOMAIN,api.telegram.org\r\n+.grok.com\r\nexact.test\r\n",
		"# domains\npayload:\n  - 'DOMAIN-SUFFIX,x.ai' # suffix\n  - DOMAIN,api.telegram.org\n  - '+.grok.com'\n  - exact.test\n",
		"payload: [\"DOMAIN-SUFFIX,x.ai\", \"DOMAIN,api.telegram.org\", \"+.grok.com\", \"exact.test\",]\n",
	} {
		checked, _ := Inspect(source, "domain", "")
		problems := checked.Errors
		want := []int{2, 3}
		if strings.HasPrefix(source, "payload: [") {
			want = []int{1, 1}
		} else if strings.Contains(source, "payload:") {
			want = []int{3, 4}
		}
		if len(problems) != 2 || checked.Content != source || len(checked.Changes) != 0 ||
			problems[0].Line != want[0] || problems[1].Line != want[1] {
			t.Fatal(encode(checked))
		}
		for _, p := range problems {
			if !strings.Contains(p.Message, "example.com") || !strings.Contains(p.Message, "+.example.com") {
				t.Fatal(p.Message)
			}
		}
		expected := strings.ReplaceAll(source, "'DOMAIN-SUFFIX,x.ai'", `"+.x.ai"`)
		expected = strings.ReplaceAll(expected, `"DOMAIN-SUFFIX,x.ai"`, `"+.x.ai"`)
		expected = strings.ReplaceAll(expected, "DOMAIN-SUFFIX,x.ai", "+.x.ai")
		expected = strings.ReplaceAll(expected, `"DOMAIN,api.telegram.org"`, `"api.telegram.org"`)
		replacement := "api.telegram.org"
		if strings.Contains(source, "payload:") {
			replacement = `"api.telegram.org"`
		}
		expected = strings.ReplaceAll(expected, "DOMAIN,api.telegram.org", replacement)
		again, _ := Inspect(expected, "domain", "")
		if again.Content != expected || len(again.Changes) != 0 || len(again.Errors) != 0 || len(inspectionWarnings(again)) != 0 {
			t.Fatal(encode(again))
		}
	}
}

func TestUnsupportedOrAmbiguousInputIsNotSilentlyDropped(t *testing.T) {
	for _, source := range []string{"DOMAIN-KEYWORD,google", "IP-CIDR,192.0.2.0/24", "DOMAIN-SUFFIX,x.ai,DIRECT", "+.x.ai\ninvalid/host",
		"+.x.ai\nfoo..test", "payload: [true]", `payload: ["DOMAIN,x.ai",`, "a: &rules [\"DOMAIN,x.ai\"]\npayload: *rules",
		`payload: [&rule "DOMAIN,x.ai", *rule]`, "payload: []\npayload: []"} {
		if checked, _ := Inspect(source, "domain", ""); len(checked.Errors) == 0 {
			t.Errorf("%q", source)
		}
	}
}

func TestPlainAndWildcardMixtureNeedsNoRepair(t *testing.T) {
	source := "# unchanged\n+.x.ai\napi.telegram.org\n*.example.test\n.example.org\n\n"
	checked, _ := Inspect(source, "domain", "")
	if checked.Content != source || len(checked.Changes) != 0 || len(checked.Errors) != 0 || len(inspectionWarnings(checked)) != 0 {
		t.Fatal(encode(checked))
	}
	if c, _ := Inspect("DOMAIN-KEYWORD,google", "classical", ""); len(c.Errors) != 0 {
		t.Fatal("classical")
	}
	// test_all_supported_domain_patterns_remain_valid_and_save_unchanged (inspection part)
	for _, c := range [][2]string{
		{"# all supported Domain patterns\n.example.com\n*.example.com\n*.*.example.com\n+.example.com\n*\n", "text"},
		{"payload:\n  - '.example.com'\n  - '*.example.com'\n  - '*.*.example.com'\n  - '+.example.com'\n  - '*'\n", "yaml"},
		{"payload: [\".example.com\", \"*.example.com\", \"*.*.example.com\", \"+.example.com\", \"*\"]\n", "yaml"},
	} {
		got, _ := Inspect(c[0], "domain", c[1])
		if got.Content != c[0] || len(got.Changes) != 0 || len(got.Errors) != 0 || len(inspectionWarnings(got)) != 0 {
			t.Fatal(encode(got))
		}
	}
}

func TestYAMLRemovalCanEmptyAndRefillTheSamePayload(t *testing.T) {
	for _, content := range []string{"payload: [\"first.test\", \"last.test\",]\n", "payload:\n  - first.test\n  - last.test\n",
		`{"payload": ["first.test", "last.test"], "other": "preserved"}`} {
		for _, value := range []string{"first.test", "last.test"} {
			var removed []string
			var err error
			content, removed, err = RemoveRulePayload(content, func(line string) bool { return line == value })
			if err != nil || len(removed) != 1 || removed[0] != value {
				t.Fatal(content, removed, err)
			}
			for _, item := range safeLoadMap(t, content)["payload"].([]any) {
				if item == value {
					t.Fatal(content)
				}
			}
		}
		_, payload, err := ReadRulePayload(content)
		if err != nil {
			t.Fatal(err)
		}
		content, err = AppendRulePayload(content, "+.new.test", payload)
		if err != nil {
			t.Fatal(err)
		}
		doc := safeLoadMap(t, content)
		if !testjson.Equal([]string{"+.new.test"}, doc["payload"]) {
			t.Fatal(content)
		}
		if strings.Contains(content, "other") && doc["other"] != "preserved" {
			t.Fatal(content)
		}
	}
}

// test_rule_syntax.py cases through ruleset_content (deferred from C08).

func validateContent(behavior, content string) error {
	return ValidateContent(model.RuleConfig{SourceType: strp("content"), Behavior: strp(behavior), Content: strp(content)}, nil)
}

func TestRuleSyntaxThroughRulesetContent(t *testing.T) {
	if rules.ClassicalLineError("DOMAIN,example.test,DIRECT,no-resolve") != "" {
		t.Fatal("classical_line_error")
	}
	// test_ipcidr_rejects_domains_typed_rules_and_invalid_networks
	for _, source := range []string{"+.tmdb.org", "api.tmdb.org", "DOMAIN-SUFFIX,tmdb.org", "DOMAIN,tmdb.org", "IP-CIDR,192.0.2.0/24",
		"192.0.2.1", "2001:db8::1", "192.0.2.0/33", "2001:db8::/129", "999.2.3.4/8", "192.0.2.0/255.255.255.0"} {
		for _, asYAML := range []bool{false, true} {
			s := source
			if asYAML {
				s = rulePayloadYAML(t, source)
			}
			checked, _ := Inspect(s, "ipcidr", "")
			if len(checked.Errors) == 0 || checked.Content != s || len(checked.Changes) != 0 {
				t.Errorf("ipcidr %q", s)
			}
			if !errors.Is(validateContent("ipcidr", s), repo.ErrValidation) {
				t.Errorf("ipcidr save %q", s)
			}
		}
	}
	// test_ipcidr_preserves_valid_networks_without_conversion
	for _, source := range []string{"# networks\n192.0.2.8/24\n2001:db8::8/32\n// keep\n", "payload: [\"192.0.2.1/32\", \"2001:db8::1/128\"]\n"} {
		checked, _ := Inspect(source, "ipcidr", "")
		if checked.Content != source || len(checked.Changes) != 0 || len(checked.Errors) != 0 || validateContent("ipcidr", source) != nil {
			t.Errorf("%q", source)
		}
	}
	// test_domain_rejects_ip_like_and_non_domain_rules
	for _, source := range []string{"192.0.2.1", "192.0.2.0/24", "2001:db8::1", "999.999.999.999", "+.999.999.999.999", "*.192.0.2.1",
		"IP-CIDR,192.0.2.0/24", "DOMAIN,999.2.3.4"} {
		if checked, _ := Inspect(source, "domain", ""); len(checked.Errors) == 0 {
			t.Errorf("domain %q", source)
		}
	}
	// test_classical_explicit_errors_reject_saves_but_delivery_keeps_original
	for _, source := range []string{"DOMAIN-SHUFFIX,tmdb.org", "DOMAIN,", "DOMAIN,tmdb.org,", "IP-CIDR,192.0.2.1/33", "IP-CIDR,192.0.2.1",
		"IP-CIDR6,2001:db8::/129", "DOMAIN-REGEX,[broken", "DST-PORT,99999", "MATCH", "RULE-SET,Other", "SUB-RULE,(NETWORK,tcp)",
		"DOMAIN", "UNKNOWN,a,b", "full:foreign.example,extra"} {
		checked, _ := Inspect(source, "classical", "")
		if len(checked.Errors) == 0 || checked.Content != source || !errors.Is(validateContent("classical", source), repo.ErrValidation) {
			t.Errorf("classical %q", source)
		}
		out, w := normalizeOK(t, source+"\n+.valid.test")
		if out != source+"\nDOMAIN-SUFFIX,valid.test" || w[0].Line != 1 || textOr(w[0].Before, "") != source {
			t.Errorf("normalize %q: %q", source, out)
		}
	}
	// test_classical_safe_bare_content_and_ambiguous_warnings_still_save_unchanged
	source := "+.tmdb.org\napi.tmdb.org\n192.0.2.1\n2001:db8::1\n*.manual.test\nfull:foreign.test"
	if validateContent("classical", source) != nil {
		t.Fatal("save")
	}
	if checked, _ := Inspect(source, "classical", ""); len(checked.Errors) != 0 || len(inspectionWarnings(checked)) != 2 {
		t.Fatal(encode(checked))
	}
	// test_bad_behavior_format_and_handwritten_mrs_are_rejected
	for _, c := range []struct {
		behavior *string
		format   *string
	}{
		{strp("unknown"), strp("text")}, {strp("DOMAIN"), strp("text")}, {nil, strp("text")},
		{strp("domain"), strp("json")}, {strp("classical"), strp("mrs")}, {strp("ipcidr"), strp("mrs")}, {strp("domain"), strp("mrs")},
	} {
		beh := "\x00"
		if c.behavior != nil {
			beh = *c.behavior
		}
		fmtStr := ""
		if c.format != nil {
			fmtStr = *c.format
		}
		if checked, _ := Inspect("", beh, fmtStr); len(checked.Errors) == 0 {
			t.Errorf("%v", c)
		}
		rule := model.RuleConfig{SourceType: strp("content"), Behavior: c.behavior, Format: c.format, Content: strp("")}
		if c.behavior == nil {
			rule = model.RuleConfig{}
			if err := json.Unmarshal([]byte(`{"source_type":"content","behavior":null,"format":"text","content":""}`), &rule); err != nil {
				t.Fatal(err)
			}
		}
		if !errors.Is(ValidateContent(rule, nil), repo.ErrValidation) {
			t.Errorf("save %v", c)
		}
	}
	// test_url_metadata_is_validated_without_reading_remote_content
	for _, b := range []string{"domain", "ipcidr"} {
		if ValidateContent(model.RuleConfig{SourceType: strp("url"), Behavior: strp(b), Format: strp("mrs")}, nil) != nil {
			t.Fatal(b)
		}
	}
	for _, rule := range []model.RuleConfig{
		{SourceType: strp("url"), Behavior: strp("typo"), Format: strp("text")},
		{SourceType: strp("url"), Behavior: strp("domain"), Format: strp("binary")},
		{SourceType: strp("url"), Behavior: strp("classical"), Format: strp("mrs")},
	} {
		if !errors.Is(ValidateContent(rule, nil), repo.ErrValidation) {
			t.Errorf("%+v", rule)
		}
	}
	// test_unchanged_legacy_invalid_sources_can_still_be_renamed_or_disabled
	old := model.RuleConfig{Name: "Old", SourceType: strp("content"), Behavior: strp("typo"), Format: strp("broken"), Content: strp("DOMAIN-SHUFFIX,x")}
	renamed := old
	renamed.Name = "New"
	disabled := old
	disabled.Enabled = boolp(false)
	edited := old
	edited.Content = strp("changed")
	if ValidateContent(renamed, &old) != nil || ValidateContent(disabled, &old) != nil ||
		!errors.Is(ValidateContent(edited, &old), repo.ErrValidation) {
		t.Fatal("legacy")
	}
	// test_unspecified_legacy_format_stays_editable
	for _, format := range []*string{nil, strp("")} {
		fmtStr := ""
		if format != nil {
			fmtStr = *format
		}
		checked, _ := Inspect("+.tmdb.org", "classical", fmtStr)
		if len(checked.Errors) != 0 ||
			ValidateContent(model.RuleConfig{SourceType: strp("content"), Behavior: strp("classical"), Format: format, Content: strp("+.tmdb.org")}, nil) != nil {
			t.Fatal(format)
		}
	}
	// test_invalid_source_types_are_rejected
	for _, st := range []*string{strp("inline"), strp("unknown"), nil} {
		rule := model.RuleConfig{SourceType: st, Behavior: strp("domain"), Content: strp("example.test")}
		if st == nil {
			rule = model.RuleConfig{}
			if err := json.Unmarshal([]byte(`{"source_type":null,"behavior":"domain","content":"example.test"}`), &rule); err != nil {
				t.Fatal(err)
			}
		}
		if !errors.Is(ValidateContent(rule, nil), repo.ErrValidation) {
			t.Errorf("%v", st)
		}
	}
	// test_bare_prefixes_with_leading_zeroes_are_not_converted_into_invalid_rules
	for _, s := range []string{"192.0.2.0/024", "2001:db8::/032", "0.0.0.0/00"} {
		if out, w := normalizeOK(t, s); out != s || len(w) == 0 {
			t.Errorf("%q", s)
		}
	}
	// test_classical_malformed_bare_ip_is_an_error_but_never_rewritten
	for _, s := range []string{"192.0.2.1/33", "999.999.999.999", ":::/64", "2001:db8::/129", "192.0.2.0/024"} {
		checked, _ := Inspect(s, "classical", "")
		if len(checked.Errors) == 0 || checked.Content != s || len(inspectionWarnings(checked)) != 0 ||
			!errors.Is(validateContent("classical", s), repo.ErrValidation) {
			t.Errorf("%q", s)
		}
		if out, w := normalizeOK(t, s); out != s || len(w) == 0 {
			t.Errorf("%q", s)
		}
	}
	// test_classical_foreign_or_ambiguous_syntax_remains_a_nonblocking_warning
	for _, s := range []string{".example.test", "*.example.test", "full:example.test", "domain:example.test", "ip:192.0.2.0/24"} {
		checked, _ := Inspect(s, "classical", "")
		if len(inspectionWarnings(checked)) == 0 || len(checked.Errors) != 0 || checked.Content != s || validateContent("classical", s) != nil {
			t.Errorf("%q", s)
		}
	}
	// test_native_valid_verbose_and_comment_regexes_are_left_to_the_core
	for _, expression := range []string{"(?x)^example # (", "(?x)^example # [", "(?x)^example # \\", "(?#literal comment [)^example",
		"(?ix:^ example )", "(?-i:x)(?x)^example # ["} {
		if rules.RuleValueError("DOMAIN-REGEX", expression) != "" {
			t.Errorf("%q", expression)
		}
		for _, asYAML := range []bool{false, true} {
			s := "DOMAIN-REGEX," + expression
			if asYAML {
				s = rulePayloadYAML(t, s)
			}
			checked, _ := Inspect(s, "classical", "")
			if len(checked.Errors) != 0 || len(inspectionWarnings(checked)) != 0 || checked.Content != s || validateContent("classical", s) != nil {
				t.Errorf("%q", s)
			}
		}
	}
	// test_explicit_text_format_rejects_yaml_payload_without_rewriting
	// test_yaml_and_unspecified_formats_accept_payload_without_changing_saved_source
	for _, c := range [][2]string{{"domain", "example.test"}, {"ipcidr", "192.0.2.0/24"}, {"classical", "+.example.test"}} {
		for _, key := range []string{"payload", `"payload"`, "'payload'"} {
			s := key + ":\n  - \"" + c[1] + "\"\n"
			checked, _ := Inspect(s, c[0], "text")
			if checked.Content != s || len(checked.Changes) != 0 ||
				encode(checked.Errors) != `[{"line":0,"message":"内容是 YAML payload，请选择 YAML 格式后保存。"}]` {
				t.Errorf("%q", s)
			}
			err := ValidateContent(model.RuleConfig{SourceType: strp("content"), Behavior: strp(c[0]), Format: strp("text"), Content: strp(s)}, nil)
			if err == nil || !strings.Contains(err.Error(), "选择 YAML") {
				t.Errorf("%v", err)
			}
		}
		for _, format := range []*string{nil, strp(""), strp("yaml")} {
			s := "payload: [\"" + c[1] + "\"]\n"
			fmtStr := ""
			if format != nil {
				fmtStr = *format
			}
			checked, _ := Inspect(s, c[0], fmtStr)
			if len(checked.Errors) != 0 ||
				ValidateContent(model.RuleConfig{SourceType: strp("content"), Behavior: strp(c[0]), Format: format, Content: strp(s)}, nil) != nil {
				t.Errorf("%q %v", s, format)
			}
		}
	}
	// test_classical_existing_parameter_fields_remain_accepted
	if rules.ClassicalLineError("DOMAIN-KEYWORD,foo,DIRECT") != "" || rules.ClassicalLineError("DST-PORT,80,no-resolve") != "" {
		t.Fatal("params")
	}
}

// test_website_identity.py (pure cases; the route/job cases → C37).

func TestKnownServiceRequiresDomainBoundary(t *testing.T) {
	if LookupService("api.openai.com").Name != "ChatGPT" || LookupService("youtube.com").Name != "YouTube" ||
		LookupService("notopenai.com") != nil || LookupService("openai.com.evil.test") != nil {
		t.Fatal("known_service")
	}
}

func TestPageMetadataAttributeOrderEntitiesAndPathRedaction(t *testing.T) {
	page := "<TITLE>Example &amp; docs</TITLE>\n      <meta content='Example &amp; Co' property='og:site_name'>\n      <meta name=\"description\" content=\"Documentation &amp; help\">"
	markers := map[string]string{}
	for _, m := range PageMarkers {
		re := mustPyRegex(m.Pattern, false)
		if match, _ := re.Match(page); match != nil {
			if match.Groups() > 0 {
				text, _ := match.Group(1)
				markers[m.Key] = text
			} else {
				text, _ := match.Group(0)
				markers[m.Key] = text
			}
		}
	}
	entry := IdentifyPage(PageProbe{
		Status:   json.RawMessage("200"),
		Markers:  rawMarkers(markers),
		FinalURL: "https://docs.example.com/private?token=secret",
	}, "example.com", "now")
	if entry.Name != "Example & Co" || entry.Description != "Documentation & help" ||
		entry.FinalHost == nil || *entry.FinalHost != "docs.example.com" || strings.Contains(encode(entry), "secret") {
		t.Fatal(encode(entry))
	}
}

func rawMarkers(m map[string]string) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(m))
	for k, v := range m {
		b, _ := json.Marshal(v)
		out[k] = b
	}
	return out
}

func TestSiteTitleDropsPageNamesAndPoweredBy(t *testing.T) {
	for _, c := range [][2]string{{"HHCLUB :: 登录 - Powered by NexusPHP", "HHCLUB"}, {"Login - GitHub", "GitHub"},
		{"首页 - 某站 - Powered by Discuz!", "某站"}, {"Cloud Monitoring as a Service | Datadog", "Cloud Monitoring as a Service | Datadog"},
		{"Wi-Fi Home", "Wi-Fi Home"}, {"登录", "登录"}} {
		if got := SiteTitle(c[0]); got != c[1] {
			t.Errorf("%q -> %q", c[0], got)
		}
		if got := IdentifyPage(PageProbe{Status: json.RawMessage("200"), Markers: rawMarkers(map[string]string{"title": c[0]})}, "example.com", "now").Name; got != c[1] {
			t.Errorf("page %q -> %q", c[0], got)
		}
	}
}

func TestFailureIsNotAWebsiteName(t *testing.T) {
	cases := []struct {
		probe  PageProbe
		reason string
	}{
		{PageProbe{Error: "timeout"}, "访问失败"},
		{PageProbe{Status: json.RawMessage("403"), Markers: rawMarkers(map[string]string{"title": "Example"})}, "HTTP 403"},
		{PageProbe{Status: json.RawMessage("200"), Markers: rawMarkers(map[string]string{"title": "Example", "challenge": "cf-chl-"})}, "人机验证"},
		{PageProbe{Status: json.RawMessage("200"), Markers: rawMarkers(map[string]string{"title": "404 Not Found"})}, "错误页"},
		{PageProbe{Status: json.RawMessage("200"), Markers: map[string]json.RawMessage{}}, "未找到"},
		{PageProbe{}, "未成功"},
	}
	for _, c := range cases {
		entry := IdentifyPage(c.probe, "example.com", "now")
		if entry.Name != "" || !strings.Contains(entry.Reason, c.reason) {
			t.Errorf("%s", encode(entry))
		}
	}
}
