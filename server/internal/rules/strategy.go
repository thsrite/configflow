package rules

import (
	"fmt"

	"configflow/internal/jsonx"
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

// ValidateRulePolicies: every enabled rule or rule
// set (rule_configs of config) must target an available policy name or a
// built-in of the format ("mihomo", "surge" or "loon"). It returns
// *StrategyReferenceError for the first offender. An unknown format is a
// programming error and panics.
func ValidateRulePolicies(config *jsonx.Object, availableNames []string, format string) error {
	builtins, ok := builtinPolicies[format]
	if !ok {
		panic(fmt.Sprintf("unknown policy format %q", format))
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
	for _, entry := range config.GetList("rule_configs") {
		rule, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		if !enabled(rule) {
			continue
		}
		kind, _ := rule.Value("itemType").(string)
		if kind != "rule" && kind != "ruleset" {
			continue
		}
		var policy any
		if value, has := rule.Get("policy"); has {
			policy = value
		} else if kind == "ruleset" {
			policy = defaultPolicy
		} else {
			policy = ""
		}
		name, isStr := policy.(string)
		if _, found := available[name]; !isStr || !found {
			id := rule.GetOr("id", rule.GetOr("name", ""))
			return &StrategyReferenceError{Message: fmt.Sprintf(
				"规则 %s 引用了不存在或未启用的策略 '%s'，请修改策略引用", jsonx.Str(id), jsonx.Str(policy))}
		}
	}
	return nil
}
