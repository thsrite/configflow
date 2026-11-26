package mosdns

import (
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func hasNonStringKey(root *yamlx.Node) bool {
	seen := map[*yamlx.Node]bool{}
	var visit func(*yamlx.Node) bool
	visit = func(node *yamlx.Node) bool {
		node = yamlx.Resolve(node)
		if node == nil || seen[node] {
			return false
		}
		seen[node] = true
		if node.Kind == yaml.MappingNode {
			for key, value := range yamlx.AllPairs(node) {
				key = yamlx.Resolve(key)
				if key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" || visit(value) {
					return true
				}
			}
		}
		for _, child := range node.Content {
			if visit(child) {
				return true
			}
		}
		return false
	}
	return visit(root)
}
