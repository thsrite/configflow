package ippresence

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// cacheMetadata is the narrow pre-N8 rulefetch bridge. Only identity and source
// members enter it; no settings or unconsumed library fields are encoded.
func cacheMetadata(rule model.RuleConfig, library []model.RuleLibraryItem) (*jsonx.Object, *jsonx.Object) {
	item := jsonx.Obj("name", rule.Name)
	if rule.ID != "" {
		item.Set("id", rule.ID)
	}
	if rule.LibraryRuleID != "" {
		item.Set("library_rule_id", rule.LibraryRuleID)
	}
	for _, member := range []struct {
		key   string
		value *string
	}{{"url", rule.URL}, {"content", rule.Content}, {"source_type", rule.SourceType}, {"behavior", rule.Behavior}, {"format", rule.Format}} {
		if member.value != nil {
			item.Set(member.key, *member.value)
		}
	}
	entries := make([]any, 0, len(library))
	for _, entry := range library {
		entries = append(entries, jsonx.Obj("id", entry.ID, "name", entry.Name))
	}
	return item, jsonx.Obj("rule_library", entries)
}
