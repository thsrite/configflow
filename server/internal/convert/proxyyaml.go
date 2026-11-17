package convert

import (
	"errors"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// proxyYAMLValue is the temporary boundary to convert's legacy document API.
// Each parsed node is translated once. It preserves alias sharing and
// never invents replacement values for cyclic or unsupported input.
type proxyYAMLValue struct {
	values map[*yamlx.Node]any
}

func (c *proxyYAMLValue) value(n *yamlx.Node) (any, error) {
	n = yamlx.Resolve(n)
	if n == nil {
		return nil, errors.New(msgProxyYAMLNode)
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
			return nil, fmt.Errorf("%s: %w", msgProxyYAMLScalar, err)
		}
		c.values[n] = value
		return value, nil
	default:
		return nil, errors.New(msgProxyYAMLNode)
	}
}

// readProxyYAML keeps shared policy and key projection ahead of the temporary
// jsonx boundary. ProjectJSONKeys mutates only this request-owned parsed graph.
func readProxyYAML(text string) (any, error) {
	root, err := parseProxyYAML(text)
	if err != nil {
		return nil, err
	}
	if err := yamlx.ProjectJSONKeys(root, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	bridge := proxyYAMLValue{values: map[*yamlx.Node]any{}}
	return bridge.value(root)
}
