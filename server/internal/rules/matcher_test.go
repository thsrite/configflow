package rules

import (
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/model"
	"configflow/internal/testjson"
)

// Ported from backend/test_rule_matcher_behavior.py and
// backend/test_rule_matcher_index.py.

func boolp(v bool) *bool { return &v }

func applyRuleOverrides(item *model.RuleConfig, overrides ...any) {
	for i := 0; i+1 < len(overrides); i += 2 {
		k := overrides[i].(string)
		v := overrides[i+1]
		switch k {
		case "format":
			s := v.(string)
			item.Format = &s
		case "behavior":
			s := v.(string)
			item.Behavior = &s
		case "source_type":
			s := v.(string)
			item.SourceType = &s
		case "url":
			s := v.(string)
			item.URL = &s
		case "content":
			s := v.(string)
			item.Content = &s
		case "enabled":
			b := v.(bool)
			item.Enabled = &b
		case "library_enabled":
			b := v.(bool)
			item.LibraryEnabled = &b
		case "library_rule_id":
			item.LibraryRuleID = v.(string)
		case "policy":
			s := v.(string)
			item.Policy = &s
		case "name":
			item.Name = v.(string)
		default:
			panic("unknown override key: " + k)
		}
	}
}

// matcherFor is the Python helper matcher(): one ruleset item, a fixed body.
func matcherFor(source, behavior string, overrides ...any) *Matcher {
	beh := behavior
	item := model.RuleConfig{ID: "set", ItemType: "ruleset", Behavior: &beh}
	applyRuleOverrides(&item, overrides...)
	return NewMatcher([]model.RuleConfig{item}, nil, func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) { return source, nil }, nil, nil)
}

func matchedLine(t *testing.T, m *Matcher, query string) string {
	t.Helper()
	result := m.Match(query)
	if result == nil {
		return "<nil>"
	}
	return result.MatchedLine
}

func TestInvalidOrCrossFormatLinesDoNotClaimAMatch(t *testing.T) {
	cases := []struct{ behavior, content, query string }{
		{"ipcidr", "+.tmdb.org", "api.tmdb.org"},
		{"ipcidr", "DOMAIN-SUFFIX,tmdb.org", "tmdb.org"},
		{"ipcidr", "127.0.0.1", "127.0.0.1"},
		{"ipcidr", "::1", "::1"},
		{"ipcidr", "IP-CIDR,127.0.0.0/8,no-resolve", "127.0.0.1"},
		{"ipcidr", "127.0.0.1/255.0.0.0", "127.0.0.1"},
		{"ipcidr", "127.0.0.1/33", "127.0.0.1"},
		{"ipcidr", "127.0.0.1/008", "127.0.0.1"},
		{"domain", "DOMAIN-SUFFIX,tmdb.org", "tmdb.org"},
		{"domain", "domain-shuffix,tmdb.org", "tmdb.org"},
		{"domain", "full:tmdb.org", "tmdb.org"},
		{"domain", "domain:tmdb.org", "tmdb.org"},
		{"domain", "127.0.0.0/8", "127.0.0.1"},
		{"domain", "127.0.0.1", "127.0.0.1"},
		{"domain", "IP-CIDR,127.0.0.0/8", "127.0.0.1"},
		{"classical", "*.tmdb.org", "api.tmdb.org"},
		{"classical", ".tmdb.org", "api.tmdb.org"},
		{"classical", "full:tmdb.org", "tmdb.org"},
		{"classical", "domain:tmdb.org", "tmdb.org"},
		{"classical", "IP-CIDR,127.0.0.1", "127.0.0.1"},
		{"classical", "IP-CIDR,127.0.0.1/008", "127.0.0.1"},
		{"classical", "127.0.0.1/008", "127.0.0.1"},
		{"classical", "IP,127.0.0.1", "127.0.0.1"},
		{"classical", "REGEX,.*", "tmdb.org"},
		{"classical", "DOMAIN-SHUFFIX,tmdb.org", "tmdb.org"},
		{"classical", "DOMAIN-SUFFIX,", "tmdb.org"},
		{"classical", "DOMAIN-REGEX,[", "tmdb.org"},
		{"classical", "MATCH", "match"},
		{"classical", "DOMAIN", "domain"},
	}
	for _, c := range cases {
		if got := matcherFor(c.content, c.behavior).Match(c.query); got != nil {
			t.Errorf("%s %q query %q matched %+v", c.behavior, c.content, c.query, got)
		}
	}
}

func TestIPCIDRAcceptsOnlyCIDRAndKeepsNetworkSemantics(t *testing.T) {
	for _, c := range [][2]string{{"127.0.0.0/8", "127.0.0.1"}, {"127.0.0.1/8", "127.0.0.2"}, {"2001:db8::/32", "2001:db8::1"}, {"::1/128", "::1"}} {
		if got := matchedLine(t, matcherFor(c[0], "ipcidr"), c[1]); got != c[0] {
			t.Errorf("%q query %q -> %q", c[0], c[1], got)
		}
	}
}

func TestDomainProviderWildcardsFollowLabelBoundaries(t *testing.T) {
	cases := []struct {
		pattern       string
		matches, miss []string
	}{
		{"tmdb.org", []string{"tmdb.org"}, []string{"api.tmdb.org"}},
		{"+.tmdb.org", []string{"tmdb.org", "api.tmdb.org", "api.deep.tmdb.org"}, []string{"other.org"}},
		{".tmdb.org", []string{"api.tmdb.org", "api.deep.tmdb.org"}, []string{"tmdb.org"}},
		{"*.tmdb.org", []string{"api.tmdb.org"}, []string{"tmdb.org", "api.deep.tmdb.org"}},
		{"api.*.ORG", []string{"api.tmdb.org", "api.other.org"}, []string{"api.org", "api.deep.tmdb.org"}},
		{"+.*.tmdb.org", []string{"api.tmdb.org", "api.deep.tmdb.org"}, []string{"tmdb.org"}},
		{".api.*.org", []string{"sub.api.tmdb.org", "deep.sub.api.tmdb.org"}, []string{"api.tmdb.org"}},
		{"*", []string{"localhost"}, []string{"tmdb.org"}},
	}
	for _, c := range cases {
		subject := matcherFor(c.pattern, "domain")
		for _, query := range c.matches {
			if got := matchedLine(t, subject, query); got != c.pattern {
				t.Errorf("%q query %q -> %q", c.pattern, query, got)
			}
		}
		for _, query := range c.miss {
			if got := subject.Match(query); got != nil {
				t.Errorf("%q query %q matched", c.pattern, query)
			}
		}
	}
}

func TestClassicalSafeBareRepairsAndTypedRulesMatch(t *testing.T) {
	cases := [][2]string{
		{"+.tmdb.org", "api.tmdb.org"}, {"tmdb.org", "tmdb.org"},
		{"127.0.0.1", "127.0.0.1"}, {"127.0.0.1/8", "127.0.0.2"}, {"::1", "::1"},
		{"DOMAIN-SUFFIX,tmdb.org", "api.tmdb.org"}, {"domain-suffix,tmdb.org", "api.tmdb.org"},
		{"IP-CIDR6,127.0.0.0/8,no-resolve", "127.0.0.1"}, {"IP-CIDR,::1/128,no-resolve", "::1"},
		{`DOMAIN-REGEX,^node[0-9]{1,3}\.org$`, "node123.org"},
		{`DOMAIN-REGEX,^\D+\.org$`, "text.org"},
		{`DOMAIN-REGEX,^EXAMPLE\.org$`, "example.org"},
	}
	for _, c := range cases {
		if got := matchedLine(t, matcherFor(c[0], "classical"), c[1]); got != c[0] {
			t.Errorf("%q query %q -> %q", c[0], c[1], got)
		}
	}
}

func TestPayloadYAMLQuotedKeysAndBOMAreDecoded(t *testing.T) {
	for _, source := range []string{
		"\ufeff+.tmdb.org\n",
		`payload: ["+.tmdb.org", "127.0.0.1"]`,
		`"payload": ["+.tmdb.org", "127.0.0.1"]`,
		"\ufeffpayload:\n  - \"+.tmdb.org\" # suffix\n  - \"127.0.0.1\"\n",
	} {
		format := "text"
		if strings.Contains(source, "payload") {
			format = "yaml"
		}
		subject := matcherFor(source, "classical", "format", format)
		if subject.Match("api.tmdb.org") == nil {
			t.Errorf("%q: no match for api.tmdb.org", source)
		}
		if strings.Contains(source, "127.0.0.1") && subject.Match("127.0.0.1") == nil {
			t.Errorf("%q: no match for 127.0.0.1", source)
		}
	}
}

func TestInvalidPayloadDoesNotFallBackToMatchingDocumentLines(t *testing.T) {
	for _, source := range []string{
		`payload: ["+.tmdb.org", true]`, `payload: {x: "+.tmdb.org"}`,
		`payload: ["+.tmdb.org"`, "payload: [\"+.tmdb.org\"]\npayload: [\"other.org\"]",
	} {
		if got := matcherFor(source, "classical", "format", "yaml").Match("api.tmdb.org"); got != nil {
			t.Errorf("%q matched %+v", source, got)
		}
	}
}

func TestDeclaredFormatMismatchDoesNotClaimAMatch(t *testing.T) {
	for _, c := range [][2]string{
		{"yaml", "DOMAIN,tmdb.org\n"},
		{"text", "payload:\n  - DOMAIN,tmdb.org\n"},
		{"text", `payload: ["DOMAIN,tmdb.org"]` + "\n"},
		{"mrs", "DOMAIN,tmdb.org\n"},
	} {
		if got := matcherFor(c[1], "classical", "format", c[0]).Match("tmdb.org"); got != nil {
			t.Errorf("format %s %q matched", c[0], c[1])
		}
	}
}

// countingLoader returns content and counts calls (Python's Mock).
type countingLoader struct {
	calls   atomic.Int32
	content ContentLoader
}

func (l *countingLoader) load(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error) {
	l.calls.Add(1)
	return l.content(item, library)
}

func fixed(content string) *countingLoader {
	return &countingLoader{content: func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) { return content, nil }}
}

func hitRuleID(h *Hit) string {
	if h == nil || h.RuleID == nil {
		return ""
	}
	return *h.RuleID
}

func TestSourceIndexesDoNotMixFormats(t *testing.T) {
	loader := fixed(`payload: ["DOMAIN,tmdb.org"]`)
	configs := []model.RuleConfig{
		{ID: "text", ItemType: "ruleset", Behavior: strp("classical"), Format: strp("text")},
		{ID: "yaml", ItemType: "ruleset", Behavior: strp("classical"), Format: strp("yaml")},
	}
	subject := NewMatcher(configs, nil, loader.load, func(*model.RuleConfig, *model.RuleLibraryItem) (string, bool) { return "shared", true }, nil)
	if got := hitRuleID(subject.Match("tmdb.org")); got != "yaml" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 2 {
		t.Fatalf("calls = %d", loader.calls.Load())
	}
	if n := subject.InvalidateRulesets(func(item *model.RuleConfig, _ *model.RuleLibraryItem) bool { return valueOr(item.Format, "") == "yaml" }); n != 1 {
		t.Fatalf("invalidated %d", n)
	}
	if got := hitRuleID(subject.Match("tmdb.org")); got != "yaml" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 3 {
		t.Fatalf("calls = %d", loader.calls.Load())
	}
}

func TestUndeclaredFormatUsesSharedConverterInference(t *testing.T) {
	for _, metadata := range [][]any{
		{"source_type", "url", "url", "https://rules.test/list.yaml"},
		{"source_type", "content", "content", `payload: ["DOMAIN,tmdb.org"]`},
	} {
		subject := matcherFor(`payload: ["DOMAIN,tmdb.org"]`, "classical", metadata...)
		if subject.Match("tmdb.org") == nil {
			t.Errorf("%v: no match", metadata)
		}
	}
}

func TestLibraryFormatIsUsedOverReferenceFormat(t *testing.T) {
	subject := NewMatcher(
		[]model.RuleConfig{{ID: "set", ItemType: "ruleset", LibraryRuleID: "lib", Behavior: strp("classical"), Format: strp("text")}},
		[]model.RuleLibraryItem{{ID: "lib", Format: strp("yaml")}},
		func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) {
			return `payload: ["DOMAIN,tmdb.org"]`, nil
		}, nil, nil)
	if subject.Match("tmdb.org") == nil {
		t.Fatal("library format ignored")
	}
}

func TestSourceReuseIncludesBehaviorAndInvalidatesOnlySelectedIndex(t *testing.T) {
	var configs []model.RuleConfig
	for _, behavior := range []string{"ipcidr", "domain", "classical"} {
		configs = append(configs, model.RuleConfig{ID: behavior, ItemType: "ruleset", Behavior: strp(behavior), LibraryRuleID: "shared"})
	}
	loader := fixed("+.tmdb.org\nIP-CIDR,127.0.0.0/8,no-resolve")
	subject := NewMatcher(configs, []model.RuleLibraryItem{{ID: "shared", Behavior: strp("classical")}}, loader.load,
		func(*model.RuleConfig, *model.RuleLibraryItem) (string, bool) { return "shared", true }, nil)
	if got := hitRuleID(subject.Match("api.tmdb.org")); got != "domain" {
		t.Fatalf("rule_id = %v", got)
	}
	if got := hitRuleID(subject.Match("127.0.0.1")); got != "classical" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 3 {
		t.Fatalf("calls = %d", loader.calls.Load())
	}
	if n := subject.InvalidateRulesets(func(item *model.RuleConfig, _ *model.RuleLibraryItem) bool { return item.ID == "domain" }); n != 1 {
		t.Fatalf("invalidated %d", n)
	}
	if got := hitRuleID(subject.Match("127.0.0.1")); got != "classical" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 4 {
		t.Fatalf("calls = %d", loader.calls.Load())
	}
}

func TestLibraryBehaviorIsUsedWhenReferenceHasNotBeenHydrated(t *testing.T) {
	subject := NewMatcher([]model.RuleConfig{{ID: "set", ItemType: "ruleset", LibraryRuleID: "lib"}},
		[]model.RuleLibraryItem{{ID: "lib", Behavior: strp("ipcidr")}},
		func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) { return "+.tmdb.org", nil }, nil, nil)
	if got := subject.Match("api.tmdb.org"); got != nil {
		t.Fatalf("matched %+v", got)
	}
}

func TestMissingBehaviorDefaultsToDomainLikeGeneratedProviders(t *testing.T) {
	subject := NewMatcher([]model.RuleConfig{{ID: "set", ItemType: "ruleset"}}, nil,
		func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) {
			return "127.0.0.1\nDOMAIN,typed.org\n+.tmdb.org", nil
		}, nil, nil)
	if subject.Match("127.0.0.1") != nil || subject.Match("typed.org") != nil {
		t.Fatal("non-domain lines matched a domain provider")
	}
	if got := subject.Match("api.tmdb.org").Behavior; got != "domain" {
		t.Fatalf("behavior = %v", got)
	}
}

func TestGenericParserStaysAvailableToCrossFormatImportConsumers(t *testing.T) {
	for line, want := range map[string][2]string{
		"full:tmdb.org": {"DOMAIN", "tmdb.org"}, "domain:tmdb.org": {"DOMAIN-SUFFIX", "tmdb.org"}, "*.tmdb.org": {"DIRECT-SUBDOMAIN", "tmdb.org"},
	} {
		if kind, value, ok := ParseRuleLine(line); !ok || kind != want[0] || value != want[1] {
			t.Errorf("ParseRuleLine(%q) = %q %q %v", line, kind, value, ok)
		}
	}
}

func TestInvalidDottedIPsAreNotDomains(t *testing.T) {
	for query, valid := range map[string]bool{"999.999.999.999": false, "300.1.1.1": false, "123.456": false, "3com.org": true, "123.example": true} {
		if IsValidDomain(query, false) != valid {
			t.Errorf("is_valid_domain(%q) != %v", query, valid)
		}
	}
}

// ---- test_rule_matcher_index.py ----

func ruleset(id string, overrides ...any) model.RuleConfig {
	item := model.RuleConfig{ID: id, ItemType: "ruleset", Name: id, Policy: strp("PROXY"), Behavior: strp("classical")}
	applyRuleOverrides(&item, overrides...)
	return item
}

func textOf(contents map[string]string) ContentLoader {
	return func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, error) { return contents[item.ID], nil }
}

// linearMatch is the Python _linear_match: one pass over every rule with the
// same behavior-aware source interpretation, without any index.
func linearMatch(configs []model.RuleConfig, contents map[string]string, query string) *Hit {
	for priority, item := range configs {
		if item.Enabled != nil && !*item.Enabled {
			continue
		}
		if item.LibraryEnabled != nil && !*item.LibraryEnabled {
			continue
		}
		kind := item.ItemType
		if kind == "" {
			kind = "rule"
		}
		policy := valueOr(item.Policy, "DIRECT")
		id := item.ID
		var idPtr *string
		if id != "" {
			idPtr = &id
		}
		switch kind {
		case "rule":
			if MatchQuery(query, item.RuleType, item.Value) {
				return &Hit{
					RuleName: item.RuleType + " 规则", RuleType: "rule", MatchedLine: item.RuleType + "," + item.Value,
					Policy: policy, Source: "直接配置的规则", Priority: priority + 1,
					RuleID: idPtr, ItemType: "rule", Behavior: "classical",
				}
			}
		case "ruleset":
			format := valueOr(item.Format, "text")
			behavior := valueOr(item.Behavior, "classical")
			for line := range rulesetLines(contents[item.ID], format) {
				rk, rv, ok := parseRulesetLine(line.value, behavior)
				if ok && MatchQuery(query, rk, rv) {
					name := item.Name
					if name == "" {
						name = "规则集"
					}
					return &Hit{
						RuleName: name, RuleType: "ruleset", MatchedLine: strings.TrimSpace(line.source),
						Policy: policy, Source: "规则集: " + name,
						Priority: priority + 1, RuleID: idPtr, ItemType: "ruleset",
						Behavior: behavior,
					}
				}
			}
		}
	}
	return nil
}

func sameResult(t *testing.T, got, want *Hit, context string) {
	t.Helper()
	if !testjson.Equal(want, got) {
		t.Errorf("%s:\n%s", context, testjson.Diff(want, got))
	}
}

func TestMixedYAMLRulesMatchLinearReference(t *testing.T) {
	body := `
# common supported formats, and ignored YAML metadata
payload:
  - 'DOMAIN-KEYWORD,ads'
  - 'REGEX,[invalid'
  - 'regexp:^cdn[0-9]{1,3}\.other\.test$'
  - 'DOMAIN-SUFFIX,EXAMPLE.com'
  - 'full:example.com'
  - 'plain.example'
  - '*.direct.example'
  - '.only.example'
  - 'domain:only.example'
  - 'IP-CIDR,192.0.2.9/24,no-resolve'
  - 'IP,192.0.2.1'
  - 'ip:198.51.100.19'
  - '2001:db8::/32'
  - 'fe80::1%eth0'
  - 'IP-CIDR6,::ffff:192.0.2.0/120'
  - 'DOMAIN-REGEX,.*'
  - 'localhost' # preserve original displayed line
`
	contents := map[string]string{"mixed": body}
	configs := []model.RuleConfig{ruleset("disabled", "enabled", false), ruleset("mixed", "format", "yaml")}
	subject := NewMatcher(configs, nil, textOf(contents), nil, nil)
	for _, query := range []string{
		"EXAMPLE.COM", "a.example.com", "a.b.example.com", "plain.example", "a.plain.example",
		"a.direct.example", "b.a.direct.example", "direct.example", "only.example", "a.only.example",
		"ads.other.test", "cdn23.other.test", "unmatched.test", "localhost", "bad host", "",
		"192.0.2.1", "192.0.2.250", "198.51.100.19", "2001:db8::1", "2001:db9::1",
		"fe80::1%eth0", "fe80::1%eth1", "::ffff:192.0.2.1",
	} {
		sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
	}
}

func TestEarliestLineWinsOverSpecificityOrRuleType(t *testing.T) {
	cases := []struct {
		lines    []string
		query    string
		expected string
	}{
		{[]string{"DOMAIN-SUFFIX,com", "DOMAIN,a.example.com"}, "a.example.com", "DOMAIN-SUFFIX,com"},
		{[]string{"DOMAIN,a.example.com", "DOMAIN-SUFFIX,com"}, "a.example.com", "DOMAIN,a.example.com"},
		{[]string{"DOMAIN-KEYWORD,example", "+.example.com"}, "a.example.com", "DOMAIN-KEYWORD,example"},
		{[]string{"+.example.com", "DOMAIN-KEYWORD,example"}, "a.example.com", "+.example.com"},
		{[]string{"DOMAIN-REGEX,example", "DOMAIN-SUFFIX,example.com"}, "a.example.com", "DOMAIN-REGEX,example"},
		{[]string{"DOMAIN-SUFFIX,example.com", "DOMAIN-REGEX,example"}, "a.example.com", "DOMAIN-SUFFIX,example.com"},
		{[]string{"full:Example.com", "DOMAIN,example.com"}, "example.com", "DOMAIN,example.com"},
		{[]string{"IP-CIDR,192.0.2.1/24", "IP-CIDR,192.0.2.0/25", "IP,192.0.2.1"}, "192.0.2.1", "IP-CIDR,192.0.2.1/24"},
		{[]string{"192.0.2.1", "IP-CIDR6,192.0.2.0/24"}, "192.0.2.1", "192.0.2.1"},
		{[]string{"IP-CIDR,::/0", "IP-CIDR6,2001:db8::/32"}, "2001:db8::1", "IP-CIDR,::/0"},
		{[]string{"IP,invalid", "IP-CIDR,bad", "IP-CIDR6,fe80::/16"}, "fe80::1%eth0", "IP-CIDR6,fe80::/16"},
	}
	for _, c := range cases {
		subject := NewMatcher([]model.RuleConfig{ruleset("set")}, nil, textOf(map[string]string{"set": strings.Join(c.lines, "\n")}), nil, nil)
		if got := matchedLine(t, subject, c.query); got != c.expected {
			t.Errorf("%v query %q -> %q, want %q", c.lines, c.query, got, c.expected)
		}
	}
}

func TestConfigurationPriorityAndLazyLoadingAreUnchanged(t *testing.T) {
	library := model.RuleLibraryItem{ID: "lib", Behavior: strp("domain")}
	configs := []model.RuleConfig{
		{ID: "disabled", RuleType: "DOMAIN", Value: "example.com", Enabled: boolp(false)},
		ruleset("disabled-lib", "library_enabled", false),
		{ID: "direct", RuleType: "DOMAIN", Value: "direct.test"},
		ruleset("first", "library_rule_id", "lib", "policy", "first-policy", "behavior", "domain"),
		ruleset("last", "policy", "last-policy"),
	}
	contents := map[string]string{"first": "+.example.com", "last": "example.com"}
	var loaded []string
	loader := func(item *model.RuleConfig, lib *model.RuleLibraryItem) (string, error) {
		loaded = append(loaded, item.ID)
		if item.ID == "first" && (lib == nil || lib.ID != "lib") {
			t.Error("loader did not receive the library entry")
		}
		return contents[item.ID], nil
	}
	subject := NewMatcher(configs, []model.RuleLibraryItem{library}, loader, nil, nil)
	sameResult(t, subject.Match("direct.test"), linearMatch(configs, contents, "direct.test"), "direct")
	if len(loaded) != 0 {
		t.Fatalf("loaded %v before any ruleset was needed", loaded)
	}
	sameResult(t, subject.Match("example.com"), linearMatch(configs, contents, "example.com"), "example.com")
	if len(loaded) != 1 || loaded[0] != "first" {
		t.Fatalf("loaded %v", loaded)
	}
	if got := subject.Match("A.example.com").Priority; got != 4 {
		t.Fatalf("priority = %v", got)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded %v", loaded)
	}
}

func TestDirectRulesRetainQueryClassificationAndRegexSemantics(t *testing.T) {
	cases := [][3]string{
		{"IP", "192.0.2.1", "192.0.2.1"}, {"IP", "fe80::1%eth0", "fe80::1%eth0"}, {"IP", "fe80::1%eth0", "fe80::1%eth1"},
		{"IP", "bad", "192.0.2.1"}, {"IP-CIDR", "192.0.2.123/24", "192.0.2.1"}, {"IP-CIDR", "2001:db8::/32", "2001:db8::1"},
		{"IP-CIDR6", "192.0.2.0/24", "192.0.2.1"}, {"IP-CIDR6", "bad", "2001:db8::1"}, {"DOMAIN", "192.0.2.1", "192.0.2.1"},
		{"IP", "192.0.2.1", "www.example.com"}, {"REGEX", `^\\D+$`, "123"}, {"REGEX", `^\\D+$`, "abc"}, {"REGEX", "[", "abc"},
	}
	for _, c := range cases {
		configs := []model.RuleConfig{{ID: "direct", RuleType: c[0], Value: c[1]}}
		subject := NewMatcher(configs, nil, func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) {
			t.Error("unexpected ruleset load")
			return "", nil
		}, nil, nil)
		sameResult(t, subject.Match(c[2]), linearMatch(configs, nil, c[2]), fmt.Sprint(c))
	}
}

func TestClassicalRegexPreservesRegexEscapesAndOriginalLine(t *testing.T) {
	content := `DOMAIN-REGEX,^\D+$`
	subject := NewMatcher([]model.RuleConfig{ruleset("set")}, nil, textOf(map[string]string{"set": content}), nil, nil)
	if got := matchedLine(t, subject, "abc"); got != content {
		t.Errorf("got %q", got)
	}
	if subject.Match("123") != nil {
		t.Error("123 matched")
	}
}

func TestShuffledRulesAndQueriesMatchLinearReference(t *testing.T) {
	base := []string{
		"DOMAIN,example.com", "DOMAIN-SUFFIX,example.com", "keyword:ample", "REGEXP:example",
		".example.com", "*.example.com", "example.net", "+.example.net", `regexp:^a\..*`,
		"REGEX,[", "DOMAIN-REGEX,.*", "DOMAIN-KEYWORD,", "REGEX,", "DOMAIN-SUFFIX,",
		"IP-CIDR,192.0.2.19/24", "IP-CIDR6,192.0.2.0/25", "IP,192.0.2.10", "IP-CIDR,bad",
		"IP-CIDR6,2001:db8::/32", "IP-CIDR,::/0", "ip:2001:db8::1", "IP,fe80::1%eth0",
	}
	for seed := int64(0); seed < 12; seed++ {
		rng := rand.New(rand.NewSource(seed))
		rules := append([]string(nil), base...)
		rng.Shuffle(len(rules), func(i, j int) { rules[i], rules[j] = rules[j], rules[i] })
		configs := []model.RuleConfig{ruleset("a"), {ID: "direct", RuleType: "DOMAIN-SUFFIX", Value: "test"}, ruleset("b")}
		contents := map[string]string{"a": strings.Join(rules[:10], "\n"), "b": strings.Join(rules[10:], "\n")}
		subject := NewMatcher(configs, nil, textOf(contents), nil, nil)
		var queries []string
		for _, prefix := range []string{"", "a.", "a.b."} {
			for _, domain := range []string{"EXAMPLE.COM", "example.net", "sample.test", "unknown.org"} {
				queries = append(queries, prefix+domain)
			}
		}
		queries = append(queries, "192.0.2.10", "192.0.2.199", "192.0.3.1", "2001:db8::1", "2001:db9::1", "fe80::1%eth0")
		for _, query := range queries {
			sameResult(t, subject.Match(query), linearMatch(configs, contents, query), fmt.Sprint(seed, query))
		}
	}
}

func TestLargeRulesetReusesIndexAndLoadsOnce(t *testing.T) {
	var body strings.Builder
	for i := 0; i < 100_000; i++ {
		fmt.Fprintf(&body, "DOMAIN-SUFFIX,rule%d.example\n", i)
	}
	loader := fixed(body.String())
	subject := NewMatcher([]model.RuleConfig{ruleset("set")}, nil, loader.load, nil, nil)
	if subject.Match("warmup.test") != nil {
		t.Fatal("warmup matched")
	}
	for i := 0; i < 400; i++ {
		want := fmt.Sprintf("DOMAIN-SUFFIX,rule%d.example", 99_999-i)
		if got := matchedLine(t, subject, fmt.Sprintf("sub.rule%d.example", 99_999-i)); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
		if subject.Match(fmt.Sprintf("unmatched%d.test", i)) != nil {
			t.Fatal("unmatched host matched")
		}
	}
	if loader.calls.Load() != 1 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
}

func TestConcurrentFirstQueriesPublishOneCompleteIndex(t *testing.T) {
	loading, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	loader := &countingLoader{content: func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) {
		once.Do(func() { close(loading) })
		<-release
		return "+.example.com\nDOMAIN,other.test", nil
	}}
	subject := NewMatcher([]model.RuleConfig{ruleset("set")}, nil, loader.load, nil, nil)
	var wg sync.WaitGroup
	results := make([]string, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = matchedLine(t, subject, "a.example.com")
		}()
	}
	<-loading
	close(release)
	wg.Wait()
	for _, line := range results {
		if line != "+.example.com" {
			t.Errorf("result %q", line)
		}
	}
	if loader.calls.Load() != 1 {
		t.Errorf("loader called %d times", loader.calls.Load())
	}
}

func TestFailedRulesetLoadIsCachedAndLaterRulesCanMatch(t *testing.T) {
	loader := &countingLoader{content: func(*model.RuleConfig, *model.RuleLibraryItem) (string, error) {
		return "", errors.New("missing cache")
	}}
	subject := NewMatcher([]model.RuleConfig{ruleset("set"), {ID: "fallback", RuleType: "DOMAIN", Value: "example.com"}}, nil, loader.load, nil, nil)
	for i := 0; i < 2; i++ {
		if got := hitRuleID(subject.Match("example.com")); got != "fallback" {
			t.Fatalf("rule_id = %v", got)
		}
	}
	if loader.calls.Load() != 1 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
}

func byLibraryID(_ *model.RuleConfig, library *model.RuleLibraryItem) (string, bool) {
	if library == nil {
		return "", false
	}
	return library.ID, true
}

func TestCompactIndexKeepsFirstSourceLineAcrossDuplicateAndIgnoredRules(t *testing.T) {
	lines := []string{
		"DOMAIN-REGEX,ignored", "IP,bad", "REGEX,[invalid", "  full:Example.com  ", "DOMAIN,example.com",
		"IP-CIDR,192.0.2.9/24,no-resolve", "IP-CIDR,192.0.2.0/24", "IP,192.0.2.1", "IP,192.0.2.1",
		"DOMAIN-KEYWORD,example", "REGEX,example",
	}
	contents := map[string]string{"set": strings.Join(lines, "\n")}
	configs := []model.RuleConfig{ruleset("set")}
	subject := NewMatcher(configs, nil, textOf(contents), nil, nil)
	for _, query := range []string{"example.com", "www.example.com", "192.0.2.1", "192.0.2.250", "absent.test"} {
		sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
	}
}

func TestSharedLibraryDoesNotReuseItemDependentLoaderByDefault(t *testing.T) {
	library := model.RuleLibraryItem{ID: "shared", Name: "same-name"}
	configs := []model.RuleConfig{ruleset("first", "library_rule_id", "shared"), ruleset("second", "library_rule_id", "shared")}
	contents := map[string]string{"first": "first.example", "second": "second.example"}
	loader := &countingLoader{content: func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, error) { return contents[item.ID], nil }}
	subject := NewMatcher(configs, []model.RuleLibraryItem{library}, loader.load, nil, nil)
	for _, query := range []string{"second.example", "first.example"} {
		sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
	}
	if loader.calls.Load() != 2 {
		t.Errorf("loader called %d times", loader.calls.Load())
	}
}

func TestOptInReuseSeparatesBehaviorAndPreservesReferenceMetadata(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		library := model.RuleLibraryItem{ID: "shared", Name: "shared-source"}
		configs := []model.RuleConfig{
			ruleset("first", "name", "first display", "library_rule_id", "shared", "policy", "DIRECT", "behavior", "domain"),
			ruleset("second", "name", "second display", "library_rule_id", "shared", "policy", "PROXY", "behavior", "classical"),
		}
		if reverse {
			configs[0], configs[1] = configs[1], configs[0]
		}
		body := "+.example.com\nIP-CIDR,192.0.2.0/24"
		contents := map[string]string{"first": body, "second": body}
		loader := fixed(body)
		subject := NewMatcher(configs, []model.RuleLibraryItem{library}, loader.load, byLibraryID, nil)
		// Different behavior parses the same source into distinct indexes.
		if subject.Match("absent.test") != nil {
			t.Fatal("absent.test matched")
		}
		for _, query := range []string{"example.com", "sub.example.com", "192.0.2.99"} {
			sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
		}
		if loader.calls.Load() != 2 {
			t.Errorf("reverse=%v loader called %d times", reverse, loader.calls.Load())
		}
	}
}

func TestReuseIsScopedToMatcherAndSourceIdentityNotDisplayName(t *testing.T) {
	configs := []model.RuleConfig{ruleset("one", "library_rule_id", "one"), ruleset("two", "library_rule_id", "two")}
	libraries := []model.RuleLibraryItem{{ID: "one", Name: "same"}, {ID: "two", Name: "same"}}
	contents := map[string]string{"one": "one.example", "two": "two.example"}
	loader := &countingLoader{content: func(_ *model.RuleConfig, lib *model.RuleLibraryItem) (string, error) { return contents[lib.ID], nil }}
	first := NewMatcher(configs, libraries, loader.load, byLibraryID, nil)
	if got := hitRuleID(first.Match("two.example")); got != "two" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 2 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
	// A different repository/snapshot with the same IDs must build its own index.
	second := NewMatcher(configs, libraries, fixed("new.example").load, byLibraryID, nil)
	if got := hitRuleID(second.Match("new.example")); got != "one" {
		t.Fatalf("rule_id = %v", got)
	}
	if first.Match("new.example") != nil {
		t.Fatal("first matcher saw the second one's index")
	}
}

func TestNoneReuseKeyFallsBackToIndividualItemWithoutKeyCollision(t *testing.T) {
	configs := []model.RuleConfig{ruleset("one"), ruleset("two"), ruleset("three")}
	contents := map[string]string{"one": "one.example", "two": "two.example", "three": "three.example"}
	loader := &countingLoader{content: func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, error) { return contents[item.ID], nil }}
	subject := NewMatcher(configs, nil, loader.load, func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, bool) {
		return "1", item.ID == "three"
	}, nil)
	for _, query := range []string{"one.example", "two.example", "three.example"} {
		sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
	}
	if loader.calls.Load() != 3 {
		t.Errorf("loader called %d times", loader.calls.Load())
	}
}

func TestInvalidatingSharedFailedSourceRetriesAllReferencesButKeepsHealthyIndex(t *testing.T) {
	libraries := []model.RuleLibraryItem{{ID: "failed", Name: "failed"}, {ID: "healthy", Name: "healthy"}}
	configs := []model.RuleConfig{
		ruleset("failed-first", "library_rule_id", "failed"),
		ruleset("healthy", "library_rule_id", "healthy"),
		ruleset("failed-second", "library_rule_id", "failed"),
	}
	var unavailable atomic.Bool
	unavailable.Store(true)
	loader := &countingLoader{content: func(_ *model.RuleConfig, lib *model.RuleLibraryItem) (string, error) {
		if lib.ID == "healthy" {
			return "healthy.example", nil
		}
		if unavailable.Load() {
			return "", errors.New("temporary source read failure")
		}
		return "recovered.example", nil
	}}
	subject := NewMatcher(configs, libraries, loader.load, byLibraryID, nil)
	if subject.Match("absent.test") != nil {
		t.Fatal("absent.test matched")
	}
	if loader.calls.Load() != 2 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
	unavailable.Store(false)
	// Selecting just the later reference still invalidates the one shared source.
	if n := subject.InvalidateRulesets(func(item *model.RuleConfig, _ *model.RuleLibraryItem) bool { return item.ID == "failed-second" }); n != 1 {
		t.Fatalf("invalidated %d", n)
	}
	if got := hitRuleID(subject.Match("recovered.example")); got != "failed-first" {
		t.Fatalf("rule_id = %v", got)
	}
	if got := hitRuleID(subject.Match("healthy.example")); got != "healthy" {
		t.Fatalf("rule_id = %v", got)
	}
	if loader.calls.Load() != 3 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
	if n := subject.InvalidateRulesets(func(*model.RuleConfig, *model.RuleLibraryItem) bool { return false }); n != 0 {
		t.Fatalf("invalidated %d", n)
	}
}

func TestInvalidationWithoutReuseOnlyReloadsSelectedItem(t *testing.T) {
	configs := []model.RuleConfig{ruleset("one"), ruleset("two")}
	contents := map[string]string{"one": "one.example", "two": "two.example"}
	loader := &countingLoader{content: func(item *model.RuleConfig, _ *model.RuleLibraryItem) (string, error) { return contents[item.ID], nil }}
	subject := NewMatcher(configs, nil, loader.load, nil, nil)
	if subject.Match("absent.test") != nil {
		t.Fatal("absent.test matched")
	}
	contents["one"] = "changed.example"
	if n := subject.InvalidateRulesets(func(item *model.RuleConfig, _ *model.RuleLibraryItem) bool { return item.ID == "one" }); n != 1 {
		t.Fatalf("invalidated %d", n)
	}
	if got := hitRuleID(subject.Match("two.example")); got != "two" {
		t.Fatalf("rule_id = %v", got)
	}
	if got := hitRuleID(subject.Match("changed.example")); got != "one" {
		t.Fatalf("rule_id = %v", got)
	}
	if subject.Match("one.example") != nil {
		t.Fatal("old content still matches")
	}
	if loader.calls.Load() != 3 {
		t.Fatalf("loader called %d times", loader.calls.Load())
	}
}

func splitLinesRef(parts []string, sep string, trailing bool) []string {
	var out []string
	out = append(out, parts...)
	_ = sep
	if trailing {
		out = append(out, "")
	}
	return out
}

func TestStreamedLinesPreserveOriginalMatch(t *testing.T) {
	for _, sep := range []string{"\n", "\r", "\r\n"} {
		for _, trailing := range []bool{false, true} {
			parts := []string{"", "  full:Example.com  ", "# comment", "+.second.example", ""}
			content := strings.Join(parts, sep)
			if trailing {
				content += sep
			}
			got := slices.Collect(lines(content))
			want := splitLinesRef(parts[:4], sep, trailing)
			if strings.Join(got, "\x00") != strings.Join(want, "\x00") || len(got) != len(want) {
				t.Errorf("sep %q trailing %v: got %q want %q", sep, trailing, got, want)
			}
			configs := []model.RuleConfig{ruleset("set")}
			contents := map[string]string{"set": content}
			subject := NewMatcher(configs, nil, textOf(contents), nil, nil)
			for _, query := range []string{"example.com", "sub.second.example", "absent.test"} {
				sameResult(t, subject.Match(query), linearMatch(configs, contents, query), fmt.Sprintf("%q %q", sep, query))
			}
		}
	}
}

func TestStreamedEmptyAndMixedNewlines(t *testing.T) {
	for content, want := range map[string][]string{
		"": nil, "one": {"one"}, "\r\r\n": {"", ""}, "one\r\ntwo\n\rthree\vfour\u2028": {"one", "two", "", "three\vfour\u2028"},
	} {
		got := slices.Collect(lines(content))
		if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", want) {
			t.Errorf("%q: got %q want %q", content, got, want)
		}
	}
	for _, separator := range []string{"\v", "\f", "\x1c", "\x1d", "\x1e", "\u0085", "\u2028", "\u2029"} {
		got := slices.Collect(lines("first" + separator + "second"))
		if fmt.Sprintf("%q", got) != fmt.Sprintf("%q", []string{"first" + separator + "second"}) {
			t.Errorf("separator %q: got %q", separator, got)
		}
	}
}

func TestCompactSourcesPreserveUnicodeAndMalformedCommentText(t *testing.T) {
	for _, comment := range []string{"中文", "😀", "\ufffd", "\x00", "\x01\x7f"} {
		var lines []string
		for i := 0; i < 300; i++ {
			lines = append(lines, fmt.Sprintf("DOMAIN-SUFFIX,item%d.example # %s", i, comment))
		}
		index := newRuleIndex(func(yield func(ruleEntry) bool) {
			for i, line := range lines {
				if !yield(ruleEntry{"DOMAIN-SUFFIX", fmt.Sprintf("item%d.example", i), line}) {
					return
				}
			}
		})
		for _, i := range []int{0, 149, 299} {
			if got, _ := index.matchDomain(fmt.Sprintf("sub.item%d.example", i)); got != lines[i] {
				t.Errorf("comment %q: got %q", comment, got)
			}
		}
	}
}

func TestHugeAndWideSourceLinesComeBackUnchanged(t *testing.T) {
	for _, line := range []string{"DOMAIN,huge.example # " + strings.Repeat("😀", 64*1024+1), "DOMAIN,huge.example # " + strings.Repeat("中", 10_000)} {
		index := newRuleIndex(func(yield func(ruleEntry) bool) { yield(ruleEntry{"DOMAIN", "huge.example", line}) })
		if got, ok := index.matchDomain("huge.example"); !ok || got != line {
			t.Errorf("line of %d bytes changed", len(line))
		}
	}
}

func TestMixedSourcesKeepOrderSharedKeysPatternsAndFirstDuplicate(t *testing.T) {
	lines := []string{"early.example", "DOMAIN-SUFFIX,early.example", `DOMAIN-REGEX,^regex[0-9]+\.example$`, "DOMAIN-KEYWORD,keyword", "DOMAIN,keyword.example", "middle.example"}
	for i := 0; i < 300; i++ {
		lines = append(lines, fmt.Sprintf("DOMAIN-SUFFIX,item%d.example", i))
	}
	lines = append(lines, "DOMAIN-SUFFIX,item0.example", "last.example")
	content := strings.Join(lines, "\n")
	configs := []model.RuleConfig{ruleset("set")}
	contents := map[string]string{"set": content}
	subject := NewMatcher(configs, nil, textOf(contents), nil, nil)
	for _, query := range []string{"early.example", "sub.early.example", "regex42.example", "keyword.example", "middle.example", "item0.example", "last.example"} {
		sameResult(t, subject.Match(query), linearMatch(configs, contents, query), query)
	}
}

func TestEmptyOrInvalidOnlySourcesIndexNothing(t *testing.T) {
	for _, entries := range [][]ruleEntry{nil, {{"REGEX", "[invalid", "REGEX,[invalid"}}, {{"IP-CIDR", "bad network", "IP-CIDR,bad network"}}} {
		index := newRuleIndex(func(yield func(ruleEntry) bool) {
			for _, entry := range entries {
				if !yield(entry) {
					return
				}
			}
		})
		if len(index.lines) != 0 {
			t.Errorf("indexed %v", index.lines)
		}
		if _, ok := index.matchDomain("example.com"); ok {
			t.Error("matched")
		}
	}
}
