package rules

import (
	"strings"
	"testing"
)

// Ported from backend/test_rule_syntax.py. Cases that go through
// ruleset_content.inspect_domain_content / validate_domain_content belong to
// package #12 and are listed in the hand-back; the rule_syntax and
// classical_line_error ones are here.

type kv struct{ kind, value string }

func TestKnownValidRuleValuesAndGoRegexFeatures(t *testing.T) {
	cases := []kv{
		{"DOMAIN", "api.example.test"}, {"DOMAIN-SUFFIX", "example.test"},
		{"DOMAIN-KEYWORD", "example"}, {"DOMAIN-WILDCARD", "*.example.?om"},
		{"DOMAIN-REGEX", `(?<host>\p{Han}+)\.test$`},
		{"DOMAIN-REGEX", `^node[]a,b]$`}, {"DOMAIN-REGEX", `^node[^]a,b]$`},
		{"DOMAIN-REGEX", `^node[[:alpha:](]$`},
		{"DOMAIN-REGEX", `^node{foo$`}, {"DOMAIN-REGEX", `^node{1$`},
		{"DOMAIN-REGEX", `\Q[(literal)\E`},
		{"IP-CIDR", "2001:db8::/32"}, {"IP-CIDR6", "192.0.2.0/24"},
		{"SRC-IP-CIDR", "192.0.2.1/32"}, {"IP-SUFFIX", "192.0.2.1/16"},
		{"SRC-IP-SUFFIX", "2001:db8::1/64"},
		{"SRC-PORT", "0/80/443/8000-9000/65535"}, {"DST-PORT", "443"}, {"IN-PORT", "1-65535"},
		{"DSCP", "0/12-18/63"}, {"DSCP", "*"}, {"UID", "0-4294967295"},
		{"IP-ASN", "65535"}, {"SRC-IP-ASN", "0"}, {"GEOIP", "CN"}, {"SRC-GEOIP", "CN"},
		{"GEOSITE", "cn@ads"}, {"PROCESS-NAME", "app"}, {"PROCESS-PATH", "/usr/bin/app"},
		{"PROCESS-NAME-REGEX", `^app[0-9]{1,3}$`}, {"PROCESS-PATH-REGEX", `/apps/(a,b)`},
		{"PROCESS-NAME-WILDCARD", "app*"}, {"PROCESS-PATH-WILDCARD", "/apps/*"},
		{"NETWORK", "tcp"}, {"IN-TYPE", "HTTP"}, {"IN-USER", "user"}, {"IN-NAME", "listener"},
		{"REMATCH-NAME", "example"}, {"SUB-RULE", "(NETWORK,tcp)"}, {"RULE-SET", "Example"},
		{"AND", "((DOMAIN,example.test),(OR,((NETWORK,tcp),(DST-PORT,80/443))))"},
		{"OR", "((DOMAIN-REGEX,^node{1,3}$),(DOMAIN,example.test))"},
		{"NOT", "((DOMAIN,example.test))"}, {"MATCH", ""},
	}
	for _, c := range cases {
		if !IsRuleType(c.kind) {
			t.Errorf("%v not in RULE_TYPES", c.kind)
		}
		if got := RuleValueError(c.kind, c.value); got != "" {
			t.Errorf("rule_value_error(%v, %q) = %q, want none", c.kind, c.value, got)
		}
	}
}

func TestRuleValueErrorsAreExplicit(t *testing.T) {
	cases := []kv{
		{"DOMAIN-SHUFFIX", "example.test"}, {"FUTURE-TYPE", "x"}, {"", "x"},
		{"DOMAIN", ""}, {"DOMAIN", "999.999.999.999"}, {"DOMAIN-SUFFIX", "192.0.2.1"},
		{"DOMAIN", "-bad.test"}, {"DOMAIN", "bad..test"}, {"DOMAIN", "bad.test\nOTHER"},
		{"IP-CIDR", "192.0.2.1"}, {"IP-CIDR", "192.0.2.0/33"},
		{"IP-CIDR6", "2001:db8::/129"}, {"IP-CIDR", "192.0.2.0/255.255.255.0"},
		{"IP-CIDR", "fe80::1%eth0/64"}, {"IP-CIDR", "999.2.3.4/8"},
		{"IP-CIDR", "192.0.2.0/024"}, {"IP-CIDR6", "2001:db8::/032"},
		{"DOMAIN-REGEX", "[abc"}, {"DOMAIN-REGEX", "(abc"}, {"DOMAIN-REGEX", "abc)"},
		{"DOMAIN-REGEX", "abc\\"}, {"DOMAIN-REGEX", "a{3,2}"}, {"DOMAIN-REGEX", `\p{Han}[`},
		{"PROCESS-NAME-REGEX", "*app"},
		{"DST-PORT", "65536"}, {"SRC-PORT", "-1"}, {"IN-PORT", "443-80"},
		{"DST-PORT", "80/abc"}, {"DSCP", "64"}, {"UID", "4294967296"},
		{"IP-ASN", "not-asn"}, {"NETWORK", "quic"}, {"MATCH", "example.test"},
		{"AND", "(DOMAIN,example.test)"}, {"AND", "((DOMAIN,example.test)"},
		{"AND", "((DOMAIN-SHUFFIX,example.test))"},
		{"NOT", "((DOMAIN,one.test),(DOMAIN,two.test))"}, {"AND", "((MATCH,))"},
	}
	for _, c := range cases {
		if RuleValueError(c.kind, c.value) == "" {
			t.Errorf("rule_value_error(%v, %q) accepted", c.kind, c.value)
		}
	}
}

func TestUnknownRuleTypeSuggestsTheClosestName(t *testing.T) {
	if got := RuleValueError("DOMAIN-SHUFFIX", "x"); got != "未知规则类型 DOMAIN-SHUFFIX。是否要填写 DOMAIN-SUFFIX？" {
		t.Errorf("got %q", got)
	}
}

func TestRuleParserMatchesClassicalPayloadCommaRules(t *testing.T) {
	check := func(line, kind, payload string, params ...string) {
		t.Helper()
		k, p, ps := SplitRulePayload(line)
		if k != kind || p != payload || strings.Join(ps, "|") != strings.Join(params, "|") {
			t.Errorf("SplitRulePayload(%q) = %q %q %q", line, k, p, ps)
		}
	}
	check("DOMAIN,example.test,DIRECT,no-resolve", "DOMAIN", "example.test", "DIRECT", "no-resolve")
	check("DOMAIN-REGEX,^a{1,3}$,DIRECT", "DOMAIN-REGEX", "^a{1,3}$,DIRECT")
	check("PROCESS-PATH-REGEX,/apps/(a,b)", "PROCESS-PATH-REGEX", "/apps/(a,b)")
	if ClassicalLineError("DOMAIN,example.test,DIRECT,no-resolve") != "" {
		t.Error("classical parameters rejected")
	}
}

func TestIPCIDRBareAddressErrorExplainsTheRequiredPrefix(t *testing.T) {
	for value, mask := range map[string]string{"192.0.2.1": "32", "2001:db8::1": "128"} {
		if got := CIDRError(value); !strings.Contains(got, value+"/"+mask) {
			t.Errorf("CIDRError(%q) = %q", value, got)
		}
	}
}

func TestExtremelyLongIntegerValuesReturnValidationError(t *testing.T) {
	for _, kind := range []string{"DST-PORT", "DSCP", "UID"} {
		if RuleValueError(kind, strings.Repeat("9", 5000)) == "" {
			t.Errorf("%s accepted a 5000 digit value", kind)
		}
	}
}

func TestExtremelyNestedRegexReturnsValidationError(t *testing.T) {
	value := strings.Repeat("(", 2000) + "x" + strings.Repeat(")", 2000)
	if got := RuleValueError("DOMAIN-REGEX", value); got != "正则表达式嵌套过深，请简化表达式。" {
		t.Errorf("got %q", got)
	}
}

func TestBareClassicalIPAndPrefixErrors(t *testing.T) {
	// From test_bare_prefixes_with_leading_zeroes... and test_classical_malformed_bare_ip:
	// the delivery-side rewrite must refuse these lines.
	for _, source := range []string{"192.0.2.0/024", "2001:db8::/032", "0.0.0.0/00", "192.0.2.1/33", "999.999.999.999", ":::/64", "2001:db8::/129"} {
		if _, err := ClassicalLine(source); err == nil {
			t.Errorf("_classical_line(%q) rewrote an invalid line", source)
		}
	}
}

func TestNativeValidVerboseAndCommentRegexesAreLeftToTheCore(t *testing.T) {
	// Mihomo v1.19.32 loads and matches the comment cases; bracket counting
	// without understanding regexp2's comments incorrectly rejected them.
	for _, expression := range []string{
		"(?x)^example # (", "(?x)^example # [", `(?x)^example # \`,
		"(?#literal comment [)^example", "(?ix:^ example )", "(?-i:x)(?x)^example # [",
	} {
		if got := RuleValueError("DOMAIN-REGEX", expression); got != "" {
			t.Errorf("rule_value_error(DOMAIN-REGEX, %q) = %q", expression, got)
		}
		line := "DOMAIN-REGEX," + expression
		if got := ClassicalLineError(line); got != "" {
			t.Errorf("classical_line_error(%q) = %q", line, got)
		}
	}
}

func TestNonRegexRuleValuesCannotInjectExtraConfigFields(t *testing.T) {
	for _, c := range []kv{
		{"DST-PORT", "80,443"}, {"DOMAIN-KEYWORD", "foo,bar"}, {"PROCESS-NAME", "app,DIRECT"},
		{"RULE-SET", "Example,DIRECT"}, {"DOMAIN-WILDCARD", "*.example.test,DIRECT"},
	} {
		if got := RuleValueError(c.kind, c.value); !strings.Contains(got, "逗号") {
			t.Errorf("rule_value_error(%v, %q) = %q", c.kind, c.value, got)
		}
	}
}

func TestClassicalExistingParameterFieldsRemainAccepted(t *testing.T) {
	for _, line := range []string{"DOMAIN-KEYWORD,foo,DIRECT", "DST-PORT,80,no-resolve"} {
		if got := ClassicalLineError(line); got != "" {
			t.Errorf("classical_line_error(%q) = %q", line, got)
		}
	}
	for _, c := range []kv{
		{"DOMAIN-REGEX", "^foo,bar$"}, {"AND", "((DOMAIN,example.test),(DST-PORT,80/443))"}, {"SUB-RULE", "(NETWORK,tcp)"},
	} {
		if got := RuleValueError(c.kind, c.value); got != "" {
			t.Errorf("rule_value_error(%v, %q) = %q", c.kind, c.value, got)
		}
	}
}

func TestClassicalSafeBareRewrites(t *testing.T) {
	// normalize_classical_lines' per-line rewrite (ruleset_content._classical_line)
	// as far as the matcher depends on it.
	for source, want := range map[string]string{
		"+.valid.test": "DOMAIN-SUFFIX,valid.test", "api.tmdb.org": "DOMAIN,api.tmdb.org",
		"192.0.2.1": "IP-CIDR,192.0.2.1/32,no-resolve", "2001:db8::1": "IP-CIDR6,2001:db8::1/128,no-resolve",
		"192.0.2.0/24": "IP-CIDR,192.0.2.0/24,no-resolve",
	} {
		if got, err := ClassicalLine(source); err != nil || got != want {
			t.Errorf("_classical_line(%q) = %q, %v; want %q", source, got, err, want)
		}
	}
	for _, source := range []string{".example.test", "*.example.test", "full:example.test", "domain:example.test", "ip:192.0.2.0/24"} {
		if _, err := ClassicalLine(source); err == nil {
			t.Errorf("_classical_line(%q) should keep ambiguous syntax", source)
		}
	}
}
