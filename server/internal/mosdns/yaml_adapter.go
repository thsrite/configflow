package mosdns

import (
	"errors"
	"fmt"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func rejectYAML(err error) bool {
	return errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrAliasBudget)
}

func customYAMLError(err error) error {
	return &UnsupportedYAMLError{Msg: fmt.Sprintf(msgCustomYAML, err), cause: err}
}

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

// textScalar is the schema for string-valued free-form YAML fields. Native
// non-JSON tags carry scalar text; numeric/bool/null tags keep their own type.
func textScalar(node *yamlx.Node) bool {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	switch node.ShortTag() {
	case "!!int", "!!float", "!!bool", "!!null":
		return false
	}
	return true
}
