package yamlbridge

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
)

// Value projects a validated string-keyed physical graph into legacy JSON values.
// It retains shared containers; non-JSON scalars use yamlx's original-text policy.
// N8-8 replaces convert's use with a typed model; other consumers migrate in N8.
func Value(root *yamlx.Node) (any, error) {
	if _, err := yamlx.Encode(root); err != nil {
		return nil, err
	}
	decoder := valueDecoder{values: map[*yamlx.Node]any{}}
	return decoder.value(root)
}

// valueDecoder is the temporary boundary to convert's legacy document API.
// Each parsed node is translated once. It preserves alias sharing and
// never invents replacement values for cyclic or unsupported input.
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
		out := jsonx.NewObject()
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
		// Scalar conversion is owned by yamlx. Containers above never go through
		// JSON serialization, which would expand every occurrence of an alias.
		encoded, err := yamlx.ToJSON(n)
		if err != nil {
			return nil, err
		}
		value, err := jsonx.Parse(encoded)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", msgValueScalar, err)
		}
		c.values[n] = value
		return value, nil
	default:
		return nil, errors.New(msgValueNode)
	}
}
