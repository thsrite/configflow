package subs

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Archived assertions use JSON objects; all parsing remains in shared yamlx.
func readSubscriptionYAML(source string, stringKeysOnly bool) (any, error) {
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	mode := yamlx.RejectNonStringKeys
	if stringKeysOnly {
		mode = yamlx.DiscardNonStringKeys
	}
	if err := yamlx.ProjectJSONKeys(node, mode); err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func getOr(value *jsonx.Object, key string, fallback any) any {
	if item, present := value.Get(key); present {
		return item
	}
	return fallback
}
func isString(value any) bool { _, ok := value.(string); return ok }
