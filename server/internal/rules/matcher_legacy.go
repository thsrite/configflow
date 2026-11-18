package rules

import (
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"encoding/json"
)

// LoadContent adapts an unported loader. New callers use ContentLoader.
type LoadContent func(item, library *jsonx.Object) (string, error)

// IndexKey adapts an unported source key. New callers use SourceKey.
type IndexKey func(item, library *jsonx.Object) (string, bool)

// RuleConfigMatcher is the JSON boundary for existing discovery and cache callers.
type RuleConfigMatcher struct {
	*Matcher
	rules     map[*model.RuleConfig]*jsonx.Object
	libraries map[*model.RuleLibraryItem]*jsonx.Object
}

// NewRuleConfigMatcher decodes only rules and declarations once per matcher.
func NewRuleConfigMatcher(configs, library []any, load LoadContent, key IndexKey, logger Logger) *RuleConfigMatcher {
	records := make([]model.RuleConfig, 0, len(configs))
	originals := make([]*jsonx.Object, 0, len(configs))
	// Keep positions, including malformed rows, for the externally visible priority.
	for _, entry := range configs {
		var record model.RuleConfig
		object, _ := entry.(*jsonx.Object)
		if object == nil {
			disabled := false
			record.Enabled = &disabled
		} else if decodeRecord(object, &record) != nil {
			disabled := false
			record.Enabled = &disabled
		}
		records = append(records, record)
		originals = append(originals, object)
	}
	entries := make([]model.RuleLibraryItem, 0, len(library))
	oldLibrary := make([]*jsonx.Object, 0, len(library))
	for _, entry := range library {
		object, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		var record model.RuleLibraryItem
		if decodeRecord(object, &record) != nil {
			continue
		}
		entries = append(entries, record)
		oldLibrary = append(oldLibrary, object)
	}
	result := &RuleConfigMatcher{rules: make(map[*model.RuleConfig]*jsonx.Object), libraries: make(map[*model.RuleLibraryItem]*jsonx.Object)}
	for i := range records {
		result.rules[&records[i]] = originals[i]
	}
	for i := range entries {
		result.libraries[&entries[i]] = oldLibrary[i]
	}
	var loader ContentLoader
	if load != nil {
		loader = func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, error) {
			return load(result.rules[item], result.libraries[library])
		}
	}
	var source SourceKey
	if key != nil {
		source = func(item *model.RuleConfig, library *model.RuleLibraryItem) (string, bool) {
			return key(result.rules[item], result.libraries[library])
		}
	}
	result.Matcher = NewMatcher(records, entries, loader, source, logger)
	return result
}

// Match encodes the native DTO for a legacy caller.
func (m *RuleConfigMatcher) Match(query string) *jsonx.Object {
	hit := m.Matcher.Match(query)
	if hit == nil {
		return nil
	}
	data, _ := json.Marshal(hit)
	value, _ := jsonx.Parse(data)
	return value.(*jsonx.Object)
}

// InvalidateRulesets adapts the existing cache's selector.
func (m *RuleConfigMatcher) InvalidateRulesets(selectSource func(item, library *jsonx.Object) bool) int {
	return m.Matcher.InvalidateRulesets(func(item *model.RuleConfig, library *model.RuleLibraryItem) bool {
		return selectSource(m.rules[item], m.libraries[library])
	})
}
func enabled(item *jsonx.Object) bool {
	return jsonx.Truthy(item.GetOr("enabled", true)) && jsonx.Truthy(item.GetOr("library_enabled", true))
}
func itemType(item *jsonx.Object) string {
	value, _ := item.GetOr("itemType", "rule").(string)
	return value
}
func rulesetBehavior(item, library *jsonx.Object) string {
	if value, ok := item.Get("behavior"); ok {
		text, _ := value.(string)
		return text
	}
	if library != nil {
		if value, ok := library.Get("behavior"); ok {
			text, _ := value.(string)
			return text
		}
	}
	return "domain"
}
