package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Ported from backend/test_connection_ip_rules.py at the level of the pure
// logic. The HTTP route, repository transactions, Agent binding races and the
// YAML payload editing (ruleset_content.append_rule_payload, package #12) are
// outside this package; the fakes below implement the plain-text payload.

func connectionDeps() ConnectionDeps {
	return ConnectionDeps{
		RulesetInfo: func(config *jsonx.Object, id string) *jsonx.Object {
			var references []any
			for _, entry := range config.GetList("rule_configs") {
				item := entry.(*jsonx.Object)
				if item.Value("library_rule_id") == id {
					references = append(references, obj("enabled", item.GetOr("enabled", true), "after_match", item.GetOr("after_match", false), "policy", item.Value("policy")))
				}
			}
			for _, entry := range config.GetList("rule_library") {
				library := entry.(*jsonx.Object)
				if library.Value("id") == id {
					return obj("id", id, "name", library.Value("name"), "behavior", library.GetOr("behavior", "classical"), "references", references)
				}
			}
			return nil
		},
		ReadRulePayload: func(content string) ([]string, any, error) {
			return slices.Collect(lines(content)), nil, nil
		},
		AppendRulePayload: func(content, line string, _ any) (string, error) {
			separator := "\n"
			if content == "" || strings.HasSuffix(content, "\n") {
				separator = ""
			}
			return content + separator + line + "\n", nil
		},
	}
}

func connectionSetup(behavior, content string, policy any) (config, shared *jsonx.Object) {
	library := obj("id", "lib", "name", "Library", "source_type", "content", "behavior", behavior, "content", content, "enabled", true)
	shared = obj("rule_library", seq(library))
	config = obj("rule_configs", seq(obj("id", "ref", "itemType", "ruleset", "library_rule_id", "lib", "policy", policy)))
	return
}

func apply(t *testing.T, config, shared *jsonx.Object, value string, ruleType, target string) (*IPRuleResult, error) {
	t.Helper()
	destination, err := ParseDestination(value, ruleType)
	if err != nil {
		return nil, err
	}
	return ApplyIPRule(connectionDeps(), config, shared, "lib", target, destination, ruleType)
}

func contentOf(shared *jsonx.Object) string {
	return shared.GetList("rule_library")[0].(*jsonx.Object).GetString("content")
}

func TestSingleAddressAppendPreservesContent(t *testing.T) {
	for _, c := range []struct{ behavior, content, value, ruleType, line string }{
		{"classical", "# Keep this comment\nDOMAIN,existing.test", "192.0.2.8", "IP-CIDR", "IP-CIDR,192.0.2.8/32,no-resolve"},
		{"classical", "", "2001:0db8:0::8", "IP-CIDR6", "IP-CIDR6,2001:db8::8/128,no-resolve"},
		{"ipcidr", "# Keep this comment\n192.0.2.4/32\n", "192.0.2.8", "IP-CIDR", "192.0.2.8/32"},
		{"ipcidr", "", "2001:db8::8", "IP-CIDR6", "2001:db8::8/128"},
		{"classical", "", "::ffff:192.0.2.8", "IP-CIDR", "IP-CIDR,192.0.2.8/32,no-resolve"},
	} {
		config, shared := connectionSetup(c.behavior, c.content, "DIRECT")
		result, err := apply(t, config, shared, c.value, c.ruleType, "direct")
		if err != nil || len(result.Added) != 1 {
			t.Fatalf("%v: %v %v", c, result, err)
		}
		saved := contentOf(shared)
		lines := strings.Split(strings.TrimRight(saved, "\n"), "\n")
		if !strings.HasPrefix(saved, c.content) || lines[len(lines)-1] != c.line || strings.Count(saved, c.line) != 1 {
			t.Errorf("%v: saved %q", c, saved)
		}
		if len(result.Updated) != 1 {
			t.Errorf("%v: updated %d items", c, len(result.Updated))
		}
		again, err := apply(t, config, shared, c.value, c.ruleType, "direct")
		if err != nil || len(again.Added) != 0 || again.Skipped[0].(*jsonx.Object).GetString("reason") != "exists" {
			t.Errorf("%v: second apply %v %v", c, again, err)
		}
	}
}

func TestExistingCoveringNetworkIsNotDuplicated(t *testing.T) {
	for _, c := range [][4]string{
		{"IP-CIDR,192.0.2.0/24,no-resolve", "192.0.2.8", "IP-CIDR", "classical"},
		{"192.0.2.0/24", "192.0.2.8", "IP-CIDR", "ipcidr"},
	} {
		config, shared := connectionSetup(c[3], c[0], "DIRECT")
		result, err := apply(t, config, shared, c[1], c[2], "direct")
		if err != nil || len(result.Added) != 0 || result.Skipped[0].(*jsonx.Object).GetString("reason") != "exists" || contentOf(shared) != c[0] {
			t.Errorf("%v: %v %v content %q", c, result, err, contentOf(shared))
		}
	}
}

func TestSubnetIsCanonicalizedAndOnlyFullCoverageIsSkipped(t *testing.T) {
	for _, c := range [][3]string{
		{"192.0.2.123/24", "IP-CIDR", "192.0.2.0/24"},
		{"192.0.3.123/23", "IP-CIDR", "192.0.2.0/23"},
		{"2001:db8:1234:5678::abcd/64", "IP-CIDR6", "2001:db8:1234:5678::/64"},
		{"2001:db8:1234:5678::abcd/56", "IP-CIDR6", "2001:db8:1234:5600::/56"},
	} {
		host := strings.Split(c[0], "/")[0]
		suffix := 32
		if c[1] == "IP-CIDR6" {
			suffix = 128
		}
		content := fmt.Sprintf("%s,%s/%d,no-resolve\n", c[1], host, suffix)
		config, shared := connectionSetup("classical", content, "DIRECT")
		result, err := apply(t, config, shared, c[0], c[1], "direct")
		if err != nil || len(result.Added) != 1 { // an existing host must not hide the broader subnet
			t.Fatalf("%v: %v %v", c, result, err)
		}
		if want := content + fmt.Sprintf("%s,%s,no-resolve\n", c[1], c[2]); contentOf(shared) != want {
			t.Errorf("%v: content %q, want %q", c, contentOf(shared), want)
		}
		again, _ := apply(t, config, shared, c[0], c[1], "direct")
		if len(again.Added) != 0 || again.Skipped[0].(*jsonx.Object).GetString("reason") != "exists" {
			t.Errorf("%v: duplicate not skipped", c)
		}
	}
}

func TestInvalidIPRequestsDoNotChangeRules(t *testing.T) {
	for _, c := range []struct {
		value    string
		ruleType string
	}{
		{"new.test", "IP-CIDR"}, {"192.0.2.8/33", "IP-CIDR"}, {"192.0.2.8\nMATCH,DIRECT", "IP-CIDR"},
		{"192.0.2.8", "IP-CIDR6"}, {"2001:db8::8", "IP-CIDR"}, {"fe80::1%eth0", "IP-CIDR6"},
		{"", "IP-CIDR"}, {"224.0.0.1", "IP-CIDR"}, {"0.0.0.0", "IP-CIDR"},
	} {
		_, err := ParseDestination(c.value, c.ruleType)
		var invalid *ProfileValidationError
		if err == nil || fmt.Sprintf("%T", err) != fmt.Sprintf("%T", invalid) {
			t.Errorf("ParseDestination(%v, %s) = %v", c.value, c.ruleType, err)
		}
	}
}

func TestIncompatibleSourcesAreNotOverwritten(t *testing.T) {
	for _, change := range []struct{ key, value string }{{"behavior", "domain"}, {"source_type", "url"}} {
		config, shared := connectionSetup("classical", "# Keep\n", "DIRECT")
		shared.GetList("rule_library")[0].(*jsonx.Object).Set(change.key, change.value)
		if _, err := apply(t, config, shared, "192.0.2.8", "IP-CIDR", "direct"); err == nil || err.Error() != "所选规则集不支持 IP、不可编辑或策略已改变，请重新打开后选择。" {
			t.Errorf("%v: err = %v", change, err)
		}
		if contentOf(shared) != "# Keep\n" {
			t.Errorf("%v: content changed", change)
		}
	}
}

func TestCurrentProfilePolicyFiltersTargetsAndIsEnforcedOnWrite(t *testing.T) {
	for policy, want := range map[string]string{"DIRECT": `["direct"]`, "PROXY": `["proxy"]`, "REJECT": `[]`, "PASS": `[]`} {
		config, _ := connectionSetup("ipcidr", "", policy)
		config.Set("rule_library", seq(obj("id", "lib", "name", "Library", "source_type", "content", "behavior", "ipcidr", "enabled", true)))
		targets := IPRuleTargets(config, connectionDeps())
		if len(targets) != 1 || compact(t, targets[0].Value("targets")) != want {
			t.Errorf("policy %s: targets %s, want %s", policy, jsonx.Repr(targets), want)
		}
	}
	config, shared := connectionSetup("ipcidr", "", "DIRECT")
	if _, err := apply(t, config, shared, "192.0.2.8", "IP-CIDR", "proxy"); err == nil {
		t.Error("a target the policy does not allow was accepted")
	}
	if contentOf(shared) != "" {
		t.Error("content changed")
	}
}

func TestUnreferencedRulesetOffersBothTargetsAndWarns(t *testing.T) {
	config, shared := connectionSetup("ipcidr", "", "DIRECT")
	config.Set("rule_configs", seq())
	config.Set("rule_library", shared.Value("rule_library"))
	targets := IPRuleTargets(config, connectionDeps())
	if compact(t, targets[0].Value("targets")) != `["direct","proxy"]` || targets[0].Value("active") != false {
		t.Fatalf("targets %s", jsonx.Repr(targets))
	}
	result, err := apply(t, config, shared, "192.0.2.8", "IP-CIDR", "direct")
	if err != nil || len(result.Warnings) != 1 {
		t.Fatalf("%v %v", result, err)
	}
}

func TestNormalizeClientRange(t *testing.T) {
	for in, want := range map[string]string{
		"192.0.2.8": "192.0.2.8", "192.0.2.8/32": "192.0.2.8", "192.0.2.123/24": "192.0.2.0/24",
		"::ffff:192.0.2.8": "192.0.2.8", "::ffff:192.0.2.0/120": "192.0.2.0/24", " 2001:0db8::8 ": "2001:db8::8",
		"2001:db8::1/64": "2001:db8::/64",
	} {
		if got, err := NormalizeClientRange(in); err != nil || got != want {
			t.Errorf("NormalizeClientRange(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "x", "192.0.2.8/33", "224.0.0.1", "0.0.0.0", "::", "::ffff:0:0/64", "fe80::1%eth0", "1.2.3.4/1/2", "192.0.2.8/1000"} {
		if _, err := NormalizeClientRange(in); err == nil {
			t.Errorf("NormalizeClientRange(%v) accepted", in)
		}
	}
}

func TestApplyAppendFailureKeepsContentAndReturnsNoSuccess(t *testing.T) {
	config, shared := connectionSetup("ipcidr", "192.0.2.1/32\n", "DIRECT")
	deps := connectionDeps()
	failure := errors.New("append failed")
	deps.AppendRulePayload = func(content, line string, _ any) (string, error) { return content, failure }
	destination, err := ParseDestination("192.0.2.8", "IP-CIDR")
	if err != nil {
		t.Fatal(err)
	}
	result, err := ApplyIPRule(deps, config, shared, "lib", "direct", destination, "IP-CIDR")
	if result != nil || !errors.Is(err, failure) || contentOf(shared) != "192.0.2.1/32\n" {
		t.Fatalf("%v %v", result, err)
	}
}

// Historical fixture boundary: delegates to the typed implementation.

// ConnectionDeps adapts the unported discovery description and opaque edit handle.
type ConnectionDeps struct {
	RulesetInfo       func(*jsonx.Object, string) *jsonx.Object
	ReadRulePayload   func(string) ([]string, any, error)
	AppendRulePayload func(string, string, any) (string, error)
}

func ipDeps(config *jsonx.Object, deps ConnectionDeps) IPDeps {
	native := IPDeps{Describe: func(id string) (RulesetInfo, error) {
		var info RulesetInfo
		if deps.RulesetInfo == nil {
			return info, fmt.Errorf("%s", msgRuleInfo)
		}
		err := decodeRecord(deps.RulesetInfo(config, id), &info)
		return info, err
	}}
	if deps.ReadRulePayload != nil {
		native.ReadPayload = func(content string) ([]string, *yamlx.Node, error) {
			lines, payload, err := deps.ReadRulePayload(content)
			if err != nil {
				return nil, nil, err
			}
			node, ok := payload.(*yamlx.Node)
			if payload != nil && !ok {
				return nil, nil, fmt.Errorf("%s", msgRuleInfo)
			}
			return lines, node, nil
		}
	}
	if deps.AppendRulePayload != nil {
		native.AppendPayload = func(content, line string, node *yamlx.Node) (string, error) {
			var payload any
			if node != nil {
				payload = node
			}
			return deps.AppendRulePayload(content, line, payload)
		}
	}
	return native
}

// IPRuleTargets adapts editable targets to the connection route DTO.
func IPRuleTargets(config *jsonx.Object, deps ConnectionDeps) []*jsonx.Object {
	result, err := IPTargets(legacyLibrary(config.GetList("rule_library")), ipDeps(config, deps))
	if err != nil {
		return nil
	}
	var rows []*jsonx.Object
	for _, row := range result {
		rows = append(rows, oldObject(row))
	}
	return rows
}

// IPRuleResult is the temporary connection API response adapter.
type IPRuleResult struct {
	Added, Skipped, Warnings []any
	Updated                  []*jsonx.Object
}

// ApplyIPRule projects only the shared rules and keeps all unowned fields in place.
func ApplyIPRule(deps ConnectionDeps, config, shared *jsonx.Object, id, target string, destination *Destination, ruleType string) (*IPRuleResult, error) {
	config.Set("rule_library", shared.GetOr("rule_library", []any{}))
	result, err := AppendIP(ipDeps(config, deps), legacyLibrary(shared.GetList("rule_library")), id, target, destination, ruleType)
	if err != nil {
		return nil, err
	}
	adapted := &IPRuleResult{Added: []any{}, Skipped: []any{}, Warnings: oldStrings(result.Warnings)}
	for _, row := range result.Added {
		adapted.Added = append(adapted.Added, oldObject(row))
	}
	for _, row := range result.Skipped {
		adapted.Skipped = append(adapted.Skipped, oldObject(row))
	}
	for _, updated := range result.Updated {
		for _, entry := range shared.GetList("rule_library") {
			if object, ok := entry.(*jsonx.Object); ok && object.GetString("id") == updated.ID {
				object.Set("content", valueOr(updated.Content, ""))
				adapted.Updated = append(adapted.Updated, object)
				break
			}
		}
	}
	return adapted, nil
}
