package rules

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// Ported from backend/test_connection_ip_rules.py at the level of the pure
// logic. The HTTP route, repository transactions, Agent binding races and the
// YAML payload editing (ruleset_content.append_rule_payload, package #12) are
// outside this package; the fakes below implement the plain-text payload.

func connectionDeps(ruleConfigs []model.RuleConfig, library []model.RuleLibraryItem) IPDeps {
	return IPDeps{
		Describe: func(id string) (RulesetInfo, error) {
			var refs []RuleReference
			for _, item := range ruleConfigs {
				if item.LibraryRuleID == id {
					en := true
					if item.Enabled != nil {
						en = *item.Enabled
					}
					policy := valueOr(item.Policy, "")
					refs = append(refs, RuleReference{Enabled: en, Policy: policy})
				}
			}
			for _, lib := range library {
				if lib.ID == id {
					beh := "classical"
					if lib.Behavior != nil {
						beh = *lib.Behavior
					}
					return RulesetInfo{ID: id, Name: lib.Name, Behavior: beh, References: refs}, nil
				}
			}
			return RulesetInfo{}, nil
		},
		ReadPayload: func(content string) ([]string, *yamlx.Node, error) {
			return slices.Collect(lines(content)), nil, nil
		},
		AppendPayload: func(content, line string, _ *yamlx.Node) (string, error) {
			separator := "\n"
			if content == "" || strings.HasSuffix(content, "\n") {
				separator = ""
			}
			return content + separator + line + "\n", nil
		},
	}
}

func connectionSetup(behavior, content, policy string) ([]model.RuleConfig, []model.RuleLibraryItem) {
	en := true
	st := "content"
	beh := behavior
	c := content
	library := []model.RuleLibraryItem{{
		ID: "lib", Name: "Library", SourceType: &st, Behavior: &beh, Content: &c, Enabled: &en,
	}}
	configs := []model.RuleConfig{{
		ID: "ref", ItemType: "ruleset", LibraryRuleID: "lib", Policy: strp(policy),
	}}
	return configs, library
}

func applyIP(t *testing.T, configs []model.RuleConfig, library []model.RuleLibraryItem, value, ruleType, target string) (*IPResult, error) {
	t.Helper()
	destination, err := ParseDestination(value, ruleType)
	if err != nil {
		return nil, err
	}
	res, err := AppendIP(connectionDeps(configs, library), library, "lib", target, destination, ruleType)
	if err == nil && res != nil {
		for _, upd := range res.Updated {
			for i := range library {
				if library[i].ID == upd.ID {
					library[i].Content = upd.Content
				}
			}
		}
	}
	return res, err
}

func contentOf(library []model.RuleLibraryItem) string {
	return valueOr(library[0].Content, "")
}

func TestSingleAddressAppendPreservesContent(t *testing.T) {
	for _, c := range []struct{ behavior, content, value, ruleType, line string }{
		{"classical", "# Keep this comment\nDOMAIN,existing.test", "192.0.2.8", "IP-CIDR", "IP-CIDR,192.0.2.8/32,no-resolve"},
		{"classical", "", "2001:0db8:0::8", "IP-CIDR6", "IP-CIDR6,2001:db8::8/128,no-resolve"},
		{"ipcidr", "# Keep this comment\n192.0.2.4/32\n", "192.0.2.8", "IP-CIDR", "192.0.2.8/32"},
		{"ipcidr", "", "2001:db8::8", "IP-CIDR6", "2001:db8::8/128"},
		{"classical", "", "::ffff:192.0.2.8", "IP-CIDR", "IP-CIDR,192.0.2.8/32,no-resolve"},
	} {
		configs, library := connectionSetup(c.behavior, c.content, "DIRECT")
		result, err := applyIP(t, configs, library, c.value, c.ruleType, "direct")
		if err != nil || len(result.Added) != 1 {
			t.Fatalf("%v: %v %v", c, result, err)
		}
		saved := contentOf(library)
		split := strings.Split(strings.TrimRight(saved, "\n"), "\n")
		if !strings.HasPrefix(saved, c.content) || split[len(split)-1] != c.line || strings.Count(saved, c.line) != 1 {
			t.Errorf("%v: saved %q", c, saved)
		}
		if len(result.Updated) != 1 {
			t.Errorf("%v: updated %d items", c, len(result.Updated))
		}
		again, err := applyIP(t, configs, library, c.value, c.ruleType, "direct")
		if err != nil || len(again.Added) != 0 || again.Skipped[0].Reason != "exists" {
			t.Errorf("%v: second apply %v %v", c, again, err)
		}
	}
}

func TestExistingCoveringNetworkIsNotDuplicated(t *testing.T) {
	for _, c := range [][4]string{
		{"IP-CIDR,192.0.2.0/24,no-resolve", "192.0.2.8", "IP-CIDR", "classical"},
		{"192.0.2.0/24", "192.0.2.8", "IP-CIDR", "ipcidr"},
	} {
		configs, library := connectionSetup(c[3], c[0], "DIRECT")
		result, err := applyIP(t, configs, library, c[1], c[2], "direct")
		if err != nil || len(result.Added) != 0 || result.Skipped[0].Reason != "exists" || contentOf(library) != c[0] {
			t.Errorf("%v: %v %v content %q", c, result, err, contentOf(library))
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
		configs, library := connectionSetup("classical", content, "DIRECT")
		result, err := applyIP(t, configs, library, c[0], c[1], "direct")
		if err != nil || len(result.Added) != 1 { // an existing host must not hide the broader subnet
			t.Fatalf("%v: %v %v", c, result, err)
		}
		if want := content + fmt.Sprintf("%s,%s,no-resolve\n", c[1], c[2]); contentOf(library) != want {
			t.Errorf("%v: content %q, want %q", c, contentOf(library), want)
		}
		again, _ := applyIP(t, configs, library, c[0], c[1], "direct")
		if len(again.Added) != 0 || again.Skipped[0].Reason != "exists" {
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
		configs, library := connectionSetup("classical", "# Keep\n", "DIRECT")
		v := change.value
		if change.key == "behavior" {
			library[0].Behavior = &v
		} else {
			library[0].SourceType = &v
		}
		if _, err := applyIP(t, configs, library, "192.0.2.8", "IP-CIDR", "direct"); err == nil || err.Error() != "所选规则集不支持 IP、不可编辑或策略已改变，请重新打开后选择。" {
			t.Errorf("%v: err = %v", change, err)
		}
		if contentOf(library) != "# Keep\n" {
			t.Errorf("%v: content changed", change)
		}
	}
}

func TestCurrentProfilePolicyFiltersTargetsAndIsEnforcedOnWrite(t *testing.T) {
	for policy, want := range map[string]string{"DIRECT": `["direct"]`, "PROXY": `["proxy"]`, "REJECT": `[]`, "PASS": `[]`} {
		configs, library := connectionSetup("ipcidr", "", policy)
		targets, err := IPTargets(library, connectionDeps(configs, library))
		if err != nil {
			t.Fatal(err)
		}
		if len(targets) != 1 || compact(t, targets[0].Targets) != want {
			t.Errorf("policy %s: targets %+v, want %s", policy, targets, want)
		}
	}
	configs, library := connectionSetup("ipcidr", "", "DIRECT")
	if _, err := applyIP(t, configs, library, "192.0.2.8", "IP-CIDR", "proxy"); err == nil {
		t.Error("a target the policy does not allow was accepted")
	}
	if contentOf(library) != "" {
		t.Error("content changed")
	}
}

func TestUnreferencedRulesetOffersBothTargetsAndWarns(t *testing.T) {
	_, library := connectionSetup("ipcidr", "", "DIRECT")
	var configs []model.RuleConfig
	targets, err := IPTargets(library, connectionDeps(configs, library))
	if err != nil {
		t.Fatal(err)
	}
	if compact(t, targets[0].Targets) != `["direct","proxy"]` || targets[0].Active {
		t.Fatalf("targets %+v", targets)
	}
	result, err := applyIP(t, configs, library, "192.0.2.8", "IP-CIDR", "direct")
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
	configs, library := connectionSetup("ipcidr", "192.0.2.1/32\n", "DIRECT")
	deps := connectionDeps(configs, library)
	failure := errors.New("append failed")
	deps.AppendPayload = func(content, line string, _ *yamlx.Node) (string, error) { return content, failure }
	destination, err := ParseDestination("192.0.2.8", "IP-CIDR")
	if err != nil {
		t.Fatal(err)
	}
	result, err := AppendIP(deps, library, "lib", "direct", destination, "IP-CIDR")
	if result != nil || !errors.Is(err, failure) || contentOf(library) != "192.0.2.1/32\n" {
		t.Fatalf("%v %v", result, err)
	}
}
