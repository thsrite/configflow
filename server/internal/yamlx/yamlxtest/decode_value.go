package yamlxtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"iter"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

const (
	msgValueNode   = "YAML 节点无效"
	msgValueScalar = "YAML 标量无法转换"
)

// OrderedObject preserves YAML mapping key order and physical container identity
// for reader oracle tests.
type OrderedObject struct {
	keys   []string
	values map[string]any
}

// NewOrderedObject returns an empty OrderedObject.
func NewOrderedObject() *OrderedObject {
	return &OrderedObject{values: map[string]any{}}
}

// Set inserts or replaces a key while preserving insertion order.
func (o *OrderedObject) Set(key string, value any) {
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

// Len returns the number of keys.
func (o *OrderedObject) Len() int {
	if o == nil {
		return 0
	}
	return len(o.keys)
}

// Get returns the value for key and whether it is present.
func (o *OrderedObject) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

// Value returns the value for key or nil if absent.
func (o *OrderedObject) Value(key string) any {
	if o == nil {
		return nil
	}
	return o.values[key]
}

// GetObject returns the nested OrderedObject value for key or nil.
func (o *OrderedObject) GetObject(key string) *OrderedObject {
	v, _ := o.Value(key).(*OrderedObject)
	return v
}

// GetList returns the slice value for key or nil.
func (o *OrderedObject) GetList(key string) []any {
	v, _ := o.Value(key).([]any)
	return v
}

// All iterates over entries in insertion order.
func (o *OrderedObject) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		if o == nil {
			return
		}
		for _, k := range o.keys {
			if !yield(k, o.values[k]) {
				return
			}
		}
	}
}

// Value projects a validated string-keyed physical graph into test JSON values.
// It retains shared containers; non-JSON scalars use yamlx's original-text policy.
func Value(root *yamlx.Node) (any, error) {
	if _, err := yamlx.Encode(root); err != nil {
		return nil, err
	}
	decoder := valueDecoder{values: map[*yamlx.Node]any{}}
	return decoder.value(root)
}

type valueDecoder struct {
	values map[*yamlx.Node]any
}

func (c *valueDecoder) value(n *yamlx.Node) (any, error) {
	n = yamlx.Resolve(n)
	if n == nil {
		return nil, errors.New(msgValueNode)
	}
	if previous, ok := c.values[n]; ok {
		return previous, nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		out := NewOrderedObject()
		c.values[n] = out
		for name, value := range yamlx.Pairs(n) {
			child, err := c.value(value)
			if err != nil {
				return nil, err
			}
			out.Set(name, child)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		c.values[n] = out
		for i, node := range n.Content {
			child, err := c.value(node)
			if err != nil {
				return nil, err
			}
			out[i] = child
		}
		return out, nil
	case yaml.ScalarNode:
		encoded, err := yamlx.ToJSON(n)
		if err != nil {
			return nil, err
		}
		dec := json.NewDecoder(bytes.NewReader(encoded))
		dec.UseNumber()
		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("%s: %w", msgValueScalar, err)
		}
		c.values[n] = value
		return value, nil
	default:
		return nil, errors.New(msgValueNode)
	}
}
