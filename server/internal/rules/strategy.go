package rules

import (
	"fmt"

	"configflow/internal/model"
)

// Rule policies are checked against the policies a converter actually emits
// (backend/utils/strategy_references.py).

// StrategyReferenceError means an enabled rule targets a policy absent from
// the generated configuration. Error() is the Chinese message.
type StrategyReferenceError struct{ Message string }

func (e *StrategyReferenceError) Error() string { return e.Message }

var builtinPolicies = map[string][]string{
	// https://wiki.metacubex.one/config/proxies/built-in/
	"mihomo": {"DIRECT", "REJECT", "REJECT-DROP", "PASS", "PASS-RULE", "COMPATIBLE"},
	// https://manual.nssurge.com/policies/built-in.html
	"surge": {"DIRECT", "REJECT", "REJECT-DROP", "REJECT-TINYGIF", "REJECT-NO-DROP",
		"CELLULAR", "CELLULAR-ONLY", "HYBRID", "NO-HYBRID"},
	// https://github.com/Loon0x00/LoonManual/blob/main/docs/cn/policy.md
	"loon": {"DIRECT", "REJECT", "REJECT-DROP", "REJECT-IMG", "REJECT-DICT", "REJECT-ARRAY"},
}

// ValidatePolicies: every enabled rule or rule
// set (rule_configs of config) must target an available policy name or a
// built-in of the format ("mihomo", "surge" or "loon"). It returns
// *StrategyReferenceError for the first offender. An unknown format is a
// validation error.
func ValidatePolicies(configs []model.RuleConfig, availableNames []string, format string) error {
	builtins, ok := builtinPolicies[format]
	if !ok {
		return fmt.Errorf("unknown policy format %q", format)
	}
	available := map[string]struct{}{}
	for _, name := range availableNames {
		available[name] = struct{}{}
	}
	for _, name := range builtins {
		available[name] = struct{}{}
	}
	defaultPolicy := "Proxy"
	if format == "mihomo" {
		defaultPolicy = "PROXY"
	}
	for _, rule := range configs {
		if !ruleEnabled(&rule) {
			continue
		}
		kind := rule.ItemType
		if kind != "rule" && kind != "ruleset" {
			continue
		}
		fallback := ""
		if kind == "ruleset" {
			fallback = defaultPolicy
		}
		policy := valueOr(rule.Policy, fallback)
		if _, found := available[policy]; !found {
			id := rule.ID
			if id == "" {
				id = rule.Name
			}
			return &StrategyReferenceError{Message: fmt.Sprintf(msgPolicyReference, id, policy)}
		}
	}
	return nil
}
