package rules

// This boundary is temporary while convert, discovery and API records still
// use jsonx. Only the two consumed collections are decoded; never Config.
import (
	"encoding/json"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rulesetfmt"
)

// RulesetFormat adapts an unported caller to the typed format leaf.
func RulesetFormat(rule *jsonx.Object, library []any, sourceURL string) string {
	var r model.RuleConfig
	if err := decodeRecord(rule, &r); err != nil {
		return "text"
	}
	entries := make([]model.RuleLibraryItem, 0, len(library))
	for _, entry := range library {
		var item model.RuleLibraryItem
		if err := decodeRecord(entry, &item); err != nil {
			return "text"
		}
		entries = append(entries, item)
	}
	result, _ := rulesetfmt.RuleFormat(r, entries, sourceURL)
	if result == "" {
		return "text"
	}
	return result
}

func decodeRecord(source, target any) error {
	data, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(data, target)
}

// RuleCacheFilename adapts existing API/cache callers to CacheFilename.
func RuleCacheFilename(source *jsonx.Object) string {
	var rule model.RuleConfig
	_ = decodeRecord(source, &rule)
	return CacheFilename(rule)
}

// RuleCacheReadPath adapts existing API/cache callers to CacheReadPath.
func RuleCacheReadPath(source *jsonx.Object, library, stored []any, dir string) string {
	var rule model.RuleConfig
	_ = decodeRecord(source, &rule)
	return CacheReadPath(rule, legacyLibrary(library), legacyLibrary(stored), dir)
}
func legacyLibrary(source []any) []model.RuleLibraryItem {
	if source == nil {
		return nil
	}
	result := make([]model.RuleLibraryItem, 0, len(source))
	for _, value := range source {
		var item model.RuleLibraryItem
		if decodeRecord(value, &item) == nil {
			result = append(result, item)
		}
	}
	return result
}
