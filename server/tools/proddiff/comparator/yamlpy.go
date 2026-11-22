package main

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// loadYAML parses one YAML document with the server's own reader (yamlx:
// yaml.v3 core schema, duplicate keys and alias bombs rejected) into the value
// model. Merge keys are flattened; explicit keys win over merged ones. Tags
// outside the core schema fail: no generated config uses them, and silently
// dropping a tag would hide a type change.
func loadYAML(src []byte) (any, error) {
	n, err := yamlx.Parse(src)
	if err != nil {
		return nil, err
	}
	c := yamlConverter{done: map[*yamlx.Node]any{}}
	return c.convert(n, 0)
}

type yamlConverter struct {
	done map[*yamlx.Node]any
}

var errYAMLTag = errors.New("yaml: unsupported tag")

func (c *yamlConverter) convert(n *yamlx.Node, depth int) (any, error) {
	if depth > 10000 {
		return nil, errors.New("yaml: nesting too deep")
	}
	if n.Kind == yaml.AliasNode {
		if n.Alias == nil {
			return nil, errors.New("yaml: unknown alias")
		}
		if v, ok := c.done[n.Alias]; ok {
			return deepCopy(v), nil
		}
		v, err := c.convert(n.Alias, depth+1)
		if err != nil {
			return nil, err
		}
		c.done[n.Alias] = v
		return deepCopy(v), nil
	}
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) == 0 {
			return nil, nil
		}
		return c.convert(n.Content[0], depth+1)
	case yaml.SequenceNode:
		if tag := n.ShortTag(); tag != "!!seq" {
			return nil, fmt.Errorf("%w %s", errYAMLTag, tag)
		}
		out := make([]any, 0, len(n.Content))
		for _, child := range n.Content {
			v, err := c.convert(child, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case yaml.MappingNode:
		if tag := n.ShortTag(); tag != "!!map" {
			return nil, fmt.Errorf("%w %s", errYAMLTag, tag)
		}
		return c.mapping(n, depth)
	case yaml.ScalarNode:
		return scalar(n)
	}
	return nil, errors.New("yaml: unknown node")
}

func (c *yamlConverter) mapping(n *yamlx.Node, depth int) (any, error) {
	type pair struct{ key, value any }
	var merged, explicit []pair
	seen := newMap()
	for i := 0; i+1 < len(n.Content); i += 2 {
		keyNode, valueNode := n.Content[i], n.Content[i+1]
		if keyNode.Kind == yaml.ScalarNode && keyNode.ShortTag() == "!!merge" {
			// Later sources lose to earlier ones; explicit keys win over all.
			sources := []*yamlx.Node{valueNode}
			target := yamlx.Resolve(valueNode)
			if target != nil && target.Kind == yaml.SequenceNode {
				sources = nil
				for j := len(target.Content) - 1; j >= 0; j-- {
					sources = append(sources, target.Content[j])
				}
			}
			for _, source := range sources {
				v, err := c.convert(source, depth+1)
				if err != nil {
					return nil, err
				}
				m, ok := v.(*Map)
				if !ok {
					return nil, errors.New("yaml: merge value is not a mapping")
				}
				for j, k := range m.keys {
					merged = append(merged, pair{k, m.vals[j]})
				}
			}
			continue
		}
		key, err := c.convert(keyNode, depth+1)
		if err != nil {
			return nil, err
		}
		if _, ok := hashKey(key); !ok {
			return nil, errors.New("yaml: unhashable key")
		}
		if seen.Has(key) {
			return nil, errDuplicateKey
		}
		seen.Set(key, true)
		value, err := c.convert(valueNode, depth+1)
		if err != nil {
			return nil, err
		}
		explicit = append(explicit, pair{key, value})
	}
	m := newMap()
	for _, p := range append(merged, explicit...) {
		m.Set(p.key, p.value)
	}
	return m, nil
}

func scalar(n *yamlx.Node) (any, error) {
	switch tag := n.ShortTag(); tag {
	case "!!str":
		return n.Value, nil
	case "!!null", "!!bool", "!!int", "!!float", "!!timestamp", "!!binary":
		var v any
		if err := yamlx.Decode(n, &v); err != nil {
			return nil, err
		}
		switch x := v.(type) {
		case nil, bool, string, float64:
			return x, nil
		case int:
			return int64(x), nil
		case int64:
			return x, nil
		case uint64:
			return new(big.Int).SetUint64(x), nil
		case []byte:
			return string(x), nil
		}
		// yaml.v3 decodes integers past uint64 as text; keep them integral.
		if tag == "!!int" {
			if i, ok := new(big.Int).SetString(n.Value, 0); ok {
				return i, nil
			}
		}
		return fmt.Sprint(v), nil
	default:
		return nil, fmt.Errorf("%w %s", errYAMLTag, tag)
	}
}

// dumpYAML renders the value model as YAML (yaml.safe_dump in the old
// self-check). sortKeys orders mapping keys by their text.
func dumpYAML(v any, sortKeys bool) ([]byte, error) {
	n, err := yamlNode(v, sortKeys)
	if err != nil {
		return nil, err
	}
	return yamlx.Marshal(n)
}

func yamlNode(v any, sortKeys bool) (*yamlx.Node, error) {
	switch x := v.(type) {
	case nil:
		return yamlx.Null(), nil
	case bool:
		return yamlx.Bool(x), nil
	case int64:
		return yamlx.Int(x), nil
	case *big.Int:
		return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: x.String()}, nil
	case float64:
		return yamlx.Float(x), nil
	case string:
		return yamlx.Str(x), nil
	case []byte:
		return yamlx.Str(string(x)), nil
	case []any:
		items := make([]*yamlx.Node, 0, len(x))
		for _, item := range x {
			n, err := yamlNode(item, sortKeys)
			if err != nil {
				return nil, err
			}
			items = append(items, n)
		}
		return yamlx.Seq(items...), nil
	case *Map:
		order := make([]int, x.Len())
		for i := range order {
			order[i] = i
		}
		if sortKeys {
			sort.SliceStable(order, func(i, j int) bool { return pyStr(x.keys[order[i]]) < pyStr(x.keys[order[j]]) })
		}
		var pairs []*yamlx.Node
		for _, i := range order {
			k, err := yamlNode(x.keys[i], sortKeys)
			if err != nil {
				return nil, err
			}
			value, err := yamlNode(x.vals[i], sortKeys)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, k, value)
		}
		return yamlx.Map(pairs...), nil
	}
	return nil, fmt.Errorf("yaml: cannot encode %T", v)
}
