package mosdns

import (
	"errors"
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func rejectYAML(err error) bool {
	return errors.Is(err, yamlx.ErrPolicy) || errors.Is(err, yamlx.ErrAliasRecursion) || errors.Is(err, yamlx.ErrAliasBudget)
}

func customYAMLError(err error) error {
	return &UnsupportedYAMLError{Msg: fmt.Sprintf(msgCustomYAML, err), cause: err}
}

// documentNode is the boundary between existing generated JSON configuration
// sections and user YAML Nodes. User containers are never expanded into JSON.
// The final facade copy validates the whole graph, preserving shared targets.
func documentNode(value any) (*yamlx.Node, error) {
	type listID struct {
		first  *any
		length int
	}
	memo := map[any]*yamlx.Node{}
	var build func(any) (*yamlx.Node, error)
	build = func(value any) (*yamlx.Node, error) {
		switch value := value.(type) {
		case *yamlx.Node:
			return value, nil
		case *jsonx.Object:
			if value == nil {
				return yamlx.Null(), nil
			}
			if node := memo[value]; node != nil {
				return node, nil
			}
			node := yamlx.Map()
			memo[value] = node
			for key, item := range value.All() {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, yamlx.Str(key), child)
			}
			return node, nil
		case []any:
			var id listID
			if len(value) > 0 {
				id = listID{&value[0], len(value)}
				if node := memo[id]; node != nil {
					return node, nil
				}
			}
			node := yamlx.Seq()
			if id.first != nil {
				memo[id] = node
			}
			for _, item := range value {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				node.Content = append(node.Content, child)
			}
			return node, nil
		default:
			data, err := jsonx.Compact(value)
			if err != nil {
				return nil, err
			}
			return yamlx.FromJSON(data)
		}
	}
	root, err := build(value)
	if err != nil {
		return nil, err
	}
	root, err = yamlx.Encode(root)
	if err != nil {
		return nil, err
	}
	// Separate input documents may use the same anchor names. Assign unique
	// names by target identity before combining them in the generated document.
	seen := map[*yamlx.Node]bool{}
	var aliases []*yamlx.Node
	index := 0
	var visit func(*yamlx.Node)
	visit = func(node *yamlx.Node) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		if node.Anchor != "" {
			index++
			node.Anchor = fmt.Sprintf("dns%d", index)
		}
		if node.Kind == yaml.AliasNode {
			aliases = append(aliases, node)
		}
		for _, child := range node.Content {
			visit(child)
		}
		visit(node.Alias)
	}
	visit(root)
	for _, node := range aliases {
		node.Value = node.Alias.Anchor
	}
	return root, nil
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
