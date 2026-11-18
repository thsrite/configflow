package rulefetch

import (
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/rulesetfmt"
	"strings"
)

func sameSourceURL(left, right string, config *SourceConfig) bool {
	base := ""
	if config != nil {
		base = strings.TrimRight(config.Settings.ServerDomain, "/")
	}
	absolute := func(value string) string {
		if strings.HasPrefix(value, "/") && base != "" {
			return base + value
		}
		return value
	}
	return absolute(left) == absolute(right)
}

// ResolveSource resolves a remote declaration by stable ID, or by an
// unambiguous URL/behavior/format. A missing ID must never dereference nil.
func ResolveSource(address string, config *SourceConfig, sourceID string) (*model.RuleConfig, error) {
	if config == nil {
		config = &SourceConfig{}
	}
	library := make([]model.RuleConfig, len(config.Library))
	byID := make(map[string]*model.RuleConfig, len(library))
	for i, item := range config.Library {
		library[i] = rules.LibraryRule(item)
		if item.ID != "" {
			byID[item.ID] = &library[i]
		}
	}
	items := make([]model.RuleConfig, 0, len(config.Rules)+len(config.RuleSets))
	for _, item := range config.Rules {
		if item.ItemType == "ruleset" {
			items = append(items, item)
		}
	}
	items = append(items, config.RuleSets...)
	if sourceID != "" {
		source := byID[sourceID]
		if source == nil {
			for i := range items {
				item := &items[i]
				if item.ID == sourceID {
					source = byID[item.LibraryRuleID]
					if source == nil {
						source = item
					}
					break
				}
			}
		}
		if source == nil {
			return nil, NewRemoteRuleMessage(msgSourceGone)
		}
		if textOr(source.SourceType, "url") != "url" {
			return nil, NewRemoteRuleMessage(msgSourceInline)
		}
		if !sameSourceURL(textOr(source.URL, ""), address, config) {
			return nil, NewRemoteRuleMessage(msgSourceChanged)
		}
		return source, nil
	}
	candidates := make([]*model.RuleConfig, 0)
	for i := range library {
		item := &library[i]
		if sameSourceURL(textOr(item.URL, ""), address, config) {
			candidates = append(candidates, item)
		}
	}
	for i := range items {
		item := &items[i]
		if sameSourceURL(textOr(item.URL, ""), address, config) {
			if declaration := byID[item.LibraryRuleID]; declaration != nil {
				candidates = append(candidates, declaration)
			} else {
				candidates = append(candidates, item)
			}
		}
	}
	var remote []*model.RuleConfig
	declarations := map[[2]string]bool{}
	for _, item := range candidates {
		if textOr(item.SourceType, "url") != "url" {
			continue
		}
		remote = append(remote, item)
		format, err := rulesetfmt.RuleFormat(*item, config.Library, "")
		if err != nil {
			return nil, err
		}
		declarations[[2]string{textOr(item.Behavior, "domain"), format}] = true
	}
	if len(remote) == 0 {
		return nil, nil
	}
	if len(declarations) != 1 {
		return nil, NewRemoteRuleMessage(msgSourceAmbiguous)
	}
	return remote[0], nil
}
