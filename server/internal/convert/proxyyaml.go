package convert

import (
	"errors"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// proxyYAMLValue is the temporary boundary to convert's legacy document API.
// Each parsed container is translated once. It preserves alias sharing and
// never invents replacement values for cyclic or unsupported input.
type proxyYAMLValue struct {
	containers    map[*yamlx.Node]any
	nonStringKeys bool
}

func (c *proxyYAMLValue) value(n *yamlx.Node) (any, error) {
	n = yamlx.Resolve(n)
	if n == nil {
		return nil, errors.New(msgProxyYAMLNode)
	}
	if previous, ok := c.containers[n]; ok {
		return previous, nil
	}
	switch n.Kind {
	case yaml.MappingNode:
		out := jsonx.NewObject()
		c.containers[n] = out
		for key, value := range yamlx.AllPairs(n) {
			key = yamlx.Resolve(key)
			if key == nil || key.Kind != yaml.ScalarNode {
				return nil, errors.New(msgProxyYAMLKey)
			}
			name := key.Value
			if key.ShortTag() != "!!str" {
				c.nonStringKeys = true
				encoded, err := yamlx.ToJSON(key)
				if err != nil {
					return nil, err
				}
				scalar, err := jsonx.Parse(encoded)
				if err != nil {
					return nil, err
				}
				if text, ok := scalar.(string); ok {
					name = text
				} else {
					name = string(encoded)
				}
			}
			child, err := c.value(value)
			if err != nil {
				return nil, err
			}
			out.Set(name, child)
		}
		return out, nil
	case yaml.SequenceNode:
		out := make([]any, len(n.Content))
		c.containers[n] = out
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
			return nil, fmt.Errorf("%s: %w", msgProxyYAMLScalar, err)
		}
		return value, nil
	default:
		return nil, errors.New(msgProxyYAMLNode)
	}
}
