package model

import (
	"bytes"
	"encoding/json"

	"configflow/internal/yamlx"
)

// Dynamic holds schema-free JSON. Node returns a fresh YAML tree; callers edit
// that tree and explicitly save it with SetNode. Original bytes stay immutable.
type Dynamic struct{ raw json.RawMessage }

// UnmarshalJSON stores an independent copy of valid, duplicate-free JSON.
func (d *Dynamic) UnmarshalJSON(data []byte) error {
	if err := validJSON(data); err != nil {
		return err
	}
	d.raw = bytes.Clone(data)
	return nil
}

// MarshalJSON returns the original JSON, or null for a zero Dynamic.
func (d Dynamic) MarshalJSON() ([]byte, error) {
	if d.raw == nil {
		return []byte("null"), nil
	}
	return bytes.Clone(d.raw), nil
}

// Node returns a fresh, caller-owned tree without changing the stored JSON.
func (d Dynamic) Node() (*yamlx.Node, error) {
	if d.raw == nil {
		return yamlx.Null(), nil
	}
	return yamlx.FromJSON(d.raw)
}

// SetNode replaces the JSON with a validated encoding of the caller's tree.
// Under R2 a tree that encodes like the stored value keeps the stored bytes,
// so reading a Node and saving it back unchanged is a no-op.
func (d *Dynamic) SetNode(node *yamlx.Node) error {
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return err
	}
	if d.raw != nil {
		if original, err := yamlx.FromJSON(d.raw); err == nil {
			if baseline, err := yamlx.ToJSON(original); err == nil && bytes.Equal(baseline, data) {
				return nil
			}
		}
	}
	return d.UnmarshalJSON(data)
}
