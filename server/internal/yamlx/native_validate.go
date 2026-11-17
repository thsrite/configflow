package yamlx

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// aliasBudget uses yaml.v3 v3.0.1's decoder policy: at least 100 alias-driven
// operations and 1000 total operations before the ratio gate, a 99% allowance
// through 400k operations, scaling to 10% at 4m. Plain nodes have no size cap.
type aliasBudget struct{ total, aliases int }

func (b *aliasBudget) visit(alias bool) bool {
	b.total++
	if alias {
		b.aliases++
	}
	ratio := 0.99
	if b.total >= 4000000 {
		ratio = 0.10
	} else if b.total > 400000 {
		ratio -= 0.89 * float64(b.total-400000) / 3600000
	}
	return b.aliases <= 100 || b.total <= 1000 || float64(b.aliases)/float64(b.total) <= ratio
}

func validateNode(n *Node) error {
	if err := checkGraph(n, map[*Node]bool{}, map[*Node]bool{}); err != nil {
		return err
	}
	return checkAliasBudget(n, &aliasBudget{}, false)
}

func checkGraph(n *Node, path, done map[*Node]bool) error {
	if n == nil {
		return invalidNode(n, "nil child node")
	}
	if path[n] {
		return nodeError(n, fmt.Sprintf(msgRecursiveAlias, n.Line))
	}
	if done[n] {
		return nil
	}
	path[n] = true
	defer delete(path, n)
	switch n.Kind {
	case yaml.DocumentNode:
		if len(n.Content) != 1 {
			return invalidNode(n, "document must have one content node")
		}
	case yaml.MappingNode:
		if len(n.Content)%2 != 0 {
			return invalidNode(n, "mapping must have key/value pairs")
		}
	case yaml.SequenceNode, yaml.ScalarNode:
	case yaml.AliasNode:
		if n.Alias == nil {
			return invalidNode(n, "alias has no target")
		}
		if path[n.Alias] {
			return nodeError(n, fmt.Sprintf(msgRecursiveAlias, n.Line))
		}
		if err := checkGraph(n.Alias, path, done); err != nil {
			return err
		}
	default:
		if !n.IsZero() {
			return invalidNode(n, "unknown node kind")
		}
	}
	for _, child := range n.Content {
		if err := checkGraph(child, path, done); err != nil {
			return err
		}
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string][]*Node{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			resolved := Resolve(key)
			id := "complex"
			if resolved.Kind == yaml.ScalarNode {
				id = resolved.ShortTag() + ":" + resolved.Value
				switch resolved.ShortTag() {
				case "!!null":
					id = "!!null"
				case "!!bool":
					id = "!!bool:" + strings.ToLower(resolved.Value)
				case "!!int":
					if v, ok := yamlInteger(resolved.Value); ok {
						id = "!!int:" + v.String()
					}
				case "!!float":
					id = "!!float:" + floatKey(resolved.Value)
				}
			}
			for _, previous := range seen[id] {
				if Equal(previous, key) {
					label := resolved.Value
					if resolved.Kind != yaml.ScalarNode {
						label = "<collection>"
					}
					return nodeError(key, fmt.Sprintf(msgDuplicateKey, label, key.Line))
				}
			}
			seen[id] = append(seen[id], key)
		}
	}
	done[n] = true
	return nil
}

func checkAliasBudget(n *Node, b *aliasBudget, alias bool) error {
	if !b.visit(alias) {
		return nodeError(n, fmt.Sprintf(msgExcessiveAliases, n.Line))
	}
	if n.Kind == yaml.AliasNode {
		return checkAliasBudget(n.Alias, b, true)
	}
	for _, child := range n.Content {
		if err := checkAliasBudget(child, b, alias); err != nil {
			return err
		}
	}
	return nil
}
