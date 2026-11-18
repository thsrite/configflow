package convert

import (
	"fmt"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// outputGraph owns presentation of a generated graph: mapping order is retained,
// effective merges are materialized and shared containers use stable automatic
// anchor names. Scalars use the shared original-text policy. Nothing expands an
// alias per occurrence, and inputs are never changed.
func outputGraph(root *yamlx.Node) (*yamlx.Node, error) {
	if _, err := yamlx.Encode(root); err != nil {
		return nil, err
	}
	memo := map[*yamlx.Node]*yamlx.Node{}
	counter := 0
	var build func(*yamlx.Node) (*yamlx.Node, error)
	build = func(node *yamlx.Node) (*yamlx.Node, error) {
		node = yamlx.Resolve(node)
		if node == nil {
			return nil, fmt.Errorf("%s", msgProxyYAMLNode)
		}
		if node.Kind == yaml.ScalarNode {
			data, err := yamlx.ToJSON(node)
			if err != nil {
				return nil, err
			}
			return yamlx.FromJSON(data)
		}
		if prior := memo[node]; prior != nil {
			if prior.Anchor == "" {
				counter++
				prior.Anchor = fmt.Sprintf("id%03d", counter)
			}
			return &yamlx.Node{Kind: yaml.AliasNode, Value: prior.Anchor, Alias: prior}, nil
		}
		switch node.Kind {
		case yaml.MappingNode:
			result := yamlx.Map()
			memo[node] = result
			for key, value := range yamlx.Pairs(node) {
				child, err := build(value)
				if err != nil {
					return nil, err
				}
				yamlx.Set(result, key, child)
			}
			return result, nil
		case yaml.SequenceNode:
			result := yamlx.Seq()
			memo[node] = result
			for _, item := range node.Content {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				result.Content = append(result.Content, child)
			}
			return result, nil
		default:
			return nil, fmt.Errorf("%s", msgProxyYAMLNode)
		}
	}
	return build(root)
}
