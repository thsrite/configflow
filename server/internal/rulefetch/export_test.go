package rulefetch

import (
	"configflow/internal/jsondecode"
	"container/list"
	"encoding/json"
)

func strp(s string) *string { return &s }

// Clear empties the cache.
func (c *ResultCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]*list.Element{}
	c.order.Init()
	c.size = 0
}

// ValidateContentJSON is ValidateDeclaration on declarations as JSON objects.
func ValidateContentJSON(raw, previous json.RawMessage) error {
	var sent Declaration
	if err := jsondecode.Unmarshal(raw, &sent); err != nil {
		return err
	}
	var before *Declaration
	if previous != nil {
		before = new(Declaration)
		_ = jsondecode.Unmarshal(previous, before)
	}
	return ValidateDeclaration(sent, before)
}
