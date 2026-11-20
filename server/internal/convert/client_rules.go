package convert

import (
	"slices"
	"strings"

	"configflow/internal/model"
)

func validateClientRule(rule model.RuleConfig) error {
	kind := kindRule
	members := []string{"rule_type", "value", "policy"}
	if rule.RuleType == "MATCH" {
		members = []string{"rule_type", "policy"}
	}
	if rule.ItemType == "ruleset" {
		kind = kindRuleSet
		members = []string{"name", "behavior", "policy"}
	}
	for _, member := range members {
		if err := storedText(&rule, kind, rule.ID, member); err != nil {
			return err
		}
	}
	return nil
}
func clientRuleURL(rule model.RuleConfig, config *model.Config, base string) (address, original string) {
	address = stringValue(rule.URL)
	original = address
	for _, lib := range config.RuleLibrary {
		if enabled(&lib, lib.Enabled) && lib.Name == rule.Name {
			original = stringValue(lib.URL)
		}
	}
	if address == "" {
		address = original
	}
	if strings.HasPrefix(address, "/") && base != "" {
		address = base + address
	}
	return
}

// SurgeRules renders local and remotely hosted rules in stored order.
func SurgeRules(config *model.Config, base string) ([]string, error) {
	lines := []string{"[Rule]"}
	afterMatch := false
	for _, rule := range RulesForOutput(config) {
		if !enabled(&rule, rule.Enabled) || !libraryEnabled(&rule, rule.LibraryEnabled) {
			continue
		}
		if err := validateClientRule(rule); err != nil {
			return nil, err
		}
		if afterMatch {
			lines = append(lines, RuleComment(rule))
			continue
		}
		policy := stringValue(rule.Policy)
		switch rule.ItemType {
		case "rule":
			typ, value := rule.RuleType, rule.Value
			switch {
			case typ == "MATCH":
				lines = append(lines, "FINAL,"+policy)
				afterMatch = true
			case typ == "RULE-SET":
				lines = append(lines, "RULE-SET,"+value+","+policy)
			case HasPortList(typ, value):
				lines = append(lines, msgSurgePortList+typ+","+value+","+policy)
			case slices.Contains(LogicTypes, typ):
				value = strings.ReplaceAll(value, "),(", "), (")
				for _, pair := range surgeRuleTypeMap {
					value = strings.ReplaceAll(value, pair[0], pair[1])
				}
				_, value = NetworkAsProtocol(typ, value)
				lines = append(lines, typ+","+value+","+policy)
			default:
				typ, value = NetworkAsProtocol(typ, value)
				for _, pair := range surgeRuleTypeMap {
					if typ == pair[0] {
						typ = pair[1]
						break
					}
				}
				line := typ + "," + value + "," + policy
				if clientFlag(rule.NoResolve) {
					line += ",no-resolve"
				}
				lines = append(lines, line)
			}
		case "ruleset":
			if rule.Policy == nil {
				policy = "Proxy"
			}
			address, original := clientRuleURL(rule, config, base)
			var options []string
			if clientFlag(rule.NoResolve) {
				options = append(options, "no-resolve")
			}
			format, err := RuleFormat(rule, config, original)
			if err != nil {
				return nil, err
			}
			if format == "yaml" {
				options = append(options, "rule-set-format=yaml")
			}
			line := "RULE-SET," + address + "," + policy
			if len(options) > 0 {
				line += "," + strings.Join(options, ",")
			}
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// LoonRules renders local rules and remote rule subscriptions separately.
func LoonRules(config *model.Config, base string) ([]string, []string, error) {
	lines, remote := []string{"[Rule]"}, []string{"[Remote Rule]"}
	final := ""
	for _, rule := range RulesForOutput(config) {
		if !enabled(&rule, rule.Enabled) || !libraryEnabled(&rule, rule.LibraryEnabled) {
			continue
		}
		if err := validateClientRule(rule); err != nil {
			return nil, nil, err
		}
		if final != "" {
			if rule.ItemType == "ruleset" {
				remote = append(remote, RuleComment(rule))
			} else {
				lines = append(lines, RuleComment(rule))
			}
			continue
		}
		policy := stringValue(rule.Policy)
		switch rule.ItemType {
		case "rule":
			typ, value := rule.RuleType, rule.Value
			if typ == "MATCH" {
				final = "FINAL," + policy
				continue
			}
			if HasPortList(typ, value) {
				lines = append(lines, msgLoonPortList+typ+","+value+","+policy)
				continue
			}
			typ, value = NetworkAsProtocol(typ, value)
			if slices.Contains(LogicTypes, typ) {
				for _, pair := range loonRuleTypeMap {
					value = strings.ReplaceAll(value, pair[0], pair[1])
				}
				line := typ + "," + value + "," + policy
				unsupported := false
				for _, sub := range loonSubTypeRe.FindAllStringSubmatch(value, -1) {
					if !slices.Contains(LoonRuleTypes, sub[1]) {
						unsupported = true
					}
				}
				if unsupported {
					line = msgLoonRuleType + line
				}
				lines = append(lines, line)
				continue
			}
			for _, pair := range loonRuleTypeMap {
				if typ == pair[0] {
					typ = pair[1]
					break
				}
			}
			line := typ + "," + value + "," + policy
			if clientFlag(rule.NoResolve) {
				line += ",no-resolve"
			}
			if !slices.Contains(LoonRuleTypes, typ) {
				line = msgLoonRuleType + line
			}
			lines = append(lines, line)
		case "ruleset":
			if rule.Policy == nil {
				policy = "Proxy"
			}
			address, original := clientRuleURL(rule, config, base)
			if address == "" {
				continue
			}
			if original == "" {
				original = address
			}
			format, err := RuleFormat(rule, config, original)
			if err != nil {
				return nil, nil, err
			}
			if format == "yaml" || format == "mrs" {
				remote = append(remote, msgLoonRuleFormat+rule.Name+" "+address)
				continue
			}
			tag := clientSanitizeName(rule.Name)
			if tag == "" {
				tag = "rule"
			}
			remote = append(remote, loonRulesetURL(address)+",policy="+policy+",tag="+tag+",enabled=true")
		}
	}
	if final != "" {
		lines = append(lines, final)
	}
	return lines, remote, nil
}
