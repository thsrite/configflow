package yamlx

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// checkMarshalBindings follows emission order, not alias targets. A repeated
// content subtree is emitted again, so its anchor definitions must be replayed
// too. Encode has already rejected recursive graphs and unsafe alias expansion.
func checkMarshalBindings(root *Node) error {
	defined := map[string]*Node{}
	var visit func(*Node) error
	visit = func(node *Node) error {
		if node == nil || node.Kind == 0 {
			return nodeError(node, msgMarshalNodeKind)
		}
		if node.Kind == yaml.AliasNode {
			if node.Alias == nil || node.Value == "" || node.Value != node.Alias.Anchor || defined[node.Value] != node.Alias {
				return nodeError(node, fmt.Sprintf(msgMarshalAlias, node.Line))
			}
			return nil
		}
		if node.Anchor != "" {
			defined[node.Anchor] = node
		}
		for _, child := range node.Content {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root)
}
