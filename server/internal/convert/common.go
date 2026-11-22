package convert

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/rulesetfmt"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// UDPFallbackPolicies are the accepted fallback policies.
var UDPFallbackPolicies = []string{"DIRECT", "REJECT"}

// LogicTypes are the composite rule types.
var LogicTypes = []string{"AND", "OR", "NOT"}

// PortRuleTypes are the rules whose value describes a port.
var PortRuleTypes = []string{"DST-PORT", "SRC-PORT", "IN-PORT"}

const (
	networkConditionPattern = `\(\s*NETWORK\s*,\s*(tcp|udp)\s*\)`
	portConditionPattern    = `\(\s*(?:DST|DEST|SRC|IN)-PORT\s*,\s*([^()]*)\)`
)

func effectiveRule(config *model.Config) *model.RuleConfig {
	if config == nil || config.Profile == nil {
		return nil
	}
	for i := range config.Profile.RuleConfigs {
		rule := &config.Profile.RuleConfigs[i]
		if rule.ItemType == "rule" && rule.RuleType == "MATCH" && enabled(rule, rule.Enabled) {
			return rule
		}
	}
	return nil
}

func enabled(record model.SwitchMember, value *bool) bool {
	return model.SwitchEnabled(record, "enabled", value, true)
}

func libraryEnabled(record model.SwitchMember, value *bool) bool {
	return model.SwitchEnabled(record, "library_enabled", value, true)
}
func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// FallbackPolicy returns the first enabled MATCH rule's UDP fallback.
func FallbackPolicy(config *model.Config) string {
	if rule := effectiveRule(config); rule != nil && slices.Contains(UDPFallbackPolicies, stringValue(rule.UDPFallback)) {
		return *rule.UDPFallback
	}
	return ""
}

// DirectNTP reports whether the effective MATCH enables direct NTP traffic.
func DirectNTP(config *model.Config) bool {
	rule := effectiveRule(config)
	return rule != nil && rule.NTPDirect != nil && *rule.NTPDirect
}

// BuiltinNTPRule constructs the direct NTP rule emitted before stored rules.
func BuiltinNTPRule() model.RuleConfig {
	rule := builtinNTPRule
	policy, on := *rule.Policy, *rule.Enabled
	rule.Policy, rule.Enabled = &policy, &on
	return rule
}

// The stored-member codec retains this public section's established member
// order as well as its values. This is a constant document, not user input.
var builtinNTPRule = func() model.RuleConfig {
	var rule model.RuleConfig
	if err := json.Unmarshal([]byte(`{"id":"builtin-ntp-direct","itemType":"rule","rule_type":"AND","value":"((NETWORK,udp),(DST-PORT,123))","policy":"DIRECT","enabled":true}`), &rule); err != nil {
		panic(err)
	}
	return rule
}()

// RulesForOutput returns stored rules in order, preceded by direct NTP if enabled.
func RulesForOutput(config *model.Config) []model.RuleConfig {
	var result []model.RuleConfig
	if DirectNTP(config) {
		result = append(result, BuiltinNTPRule())
	}
	if config != nil && config.Profile != nil {
		result = append(result, config.Profile.RuleConfigs...)
	}
	return result
}

// validateRuleIdentities checks retained types before configuration generation.
// Number-to-text compatibility is for scalar data, not rule identity fields.
func validateRuleIdentities(rules []model.RuleConfig) error {
	for i := range rules {
		rule := &rules[i]
		// Two members are read; the rule is not encoded whole (O10 R9).
		for _, field := range []struct{ key, label string }{{"rule_type", msgRuleTypeField}, {"value", msgRuleValueField}} {
			raw, present, err := rule.MemberJSON(field.key)
			if err != nil {
				return err
			}
			if present {
				if err := identityString(raw, field.label); err != nil {
					if rule.ID != "" {
						return fmt.Errorf(msgMihomoStoredText, kindRule, rule.ID, field.key, raw)
					}
					return err
				}
			}
		}
	}
	return nil
}

// RuleComment renders the explanatory comment for a rule after MATCH.
func RuleComment(rule model.RuleConfig) string {
	var line string
	if rule.ItemType == "ruleset" {
		line = "RULE-SET," + rule.Name + "," + stringValue(rule.Policy)
	} else {
		line = rule.RuleType + "," + rule.Value + "," + stringValue(rule.Policy)
	}
	return "# " + AfterMatchNote + ": " + line
}

// RuleFormat determines a rule set's format from the resolved typed view.
func RuleFormat(rule model.RuleConfig, config *model.Config, sourceURL string) (string, error) {
	if config == nil {
		return rulesetfmt.RuleFormat(rule, nil, sourceURL)
	}
	return rulesetfmt.RuleFormat(rule, config.RuleLibrary, sourceURL)
}

// NetworkAsProtocol maps transport conditions to Surge/Loon protocol conditions.
func NetworkAsProtocol(ruleType, value string) (string, string) {
	if ruleType == "NETWORK" {
		return "PROTOCOL", cases.Upper(language.Und).String(strings.TrimSpace(value))
	}
	if !slices.Contains(LogicTypes, ruleType) {
		return ruleType, value
	}
	re, err := rules.CompilePyRegex(networkConditionPattern, true)
	if err != nil {
		return ruleType, value
	}
	out, err := re.Sub(value, func(m *rules.Match) (string, error) {
		group, _ := m.Group(1)
		return "(PROTOCOL," + cases.Upper(language.Und).String(group) + ")", nil
	}, 0)
	if err != nil {
		return ruleType, value
	}
	return ruleType, out
}

// HasPortList identifies lists unsupported by Surge/Loon's single-port rules.
func HasPortList(ruleType, value string) bool {
	if slices.Contains(PortRuleTypes, ruleType) {
		return strings.ContainsAny(value, "/,")
	}
	if !slices.Contains(LogicTypes, ruleType) {
		return false
	}
	re, err := rules.CompilePyRegex(portConditionPattern, true)
	if err != nil {
		return false
	}
	found := false
	_, _ = re.Sub(value, func(m *rules.Match) (string, error) {
		part, _ := m.Group(1)
		found = found || strings.ContainsAny(part, "/,")
		return "", nil
	}, 0)
	return found
}

// SetGeneralOption appends a missing option to a client's General section.
func SetGeneralOption(section, key, value string) string {
	re, err := rules.CompilePyRegex(`(?m)^\s*`+regexp.QuoteMeta(key)+`\s*=`, false)
	if err == nil && re.Search(section) {
		return section
	}
	return strings.TrimRightFunc(section, unicode.IsSpace) + "\n" + key + " = " + value
}

// HostsPair is one domain/address row of a hosts editor.
type HostsPair struct{ Domain, Address string }

// ParseHostsPairs accepts both domain/address and legacy address/domain rows.
func ParseHostsPairs(text string) []HostsPair {
	var result []HostsPair
	isAddress := func(text string) bool {
		address, err := netip.ParseAddr(text)
		return err == nil && !strings.Contains(address.Zone(), "%")
	}
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return strings.ContainsRune("\r\n\v\f\x1c\x1d\x1e\u0085\u2028\u2029", r) }) {
		line, _, _ = strings.Cut(line, "#")
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if isAddress(fields[1]) {
			result = append(result, HostsPair{fields[0], fields[1]})
		} else if isAddress(fields[0]) {
			result = append(result, HostsPair{fields[1], fields[0]})
		}
	}
	return result
}
