package rules

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"fmt"
)

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
