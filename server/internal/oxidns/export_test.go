package oxidns

import (
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// duplicateAnchor: an anchor name defined twice (PyYAML refuses that; yaml.v3
// lets the later one win).
func duplicateAnchor(n *Node, seen map[string]bool) bool {
	if n == nil || n.Kind == yaml.AliasNode {
		return false
	}
	if n.Anchor != "" {
		if seen[n.Anchor] {
			return true
		}
		seen[n.Anchor] = true
	}
	for _, child := range n.Content {
		if duplicateAnchor(child, seen) {
			return true
		}
	}
	return false
}

// int64Of reads an int that fits int64.
func int64Of(n *Node) (int64, bool) {
	if !isInt(n) {
		return 0, false
	}
	var v int64
	err := yamlx.Decode(n, &v)
	return v, err == nil
}

func (c ownedChain) toNode() *Node { return c.action.toNode() }

// HelperPlugins are the owned plugins the given group routes need, in
// first-use order; actions (rule_actions) supply pinned upstreams, features
// the china_dns hooks copied into local chains.
func HelperPlugins(routes []string, actions *Node, features chainFeatures) ([]*Node, error) {
	return helperPlugins(routes, actionMapFromNode(actions), features)
}
