package convert

import (
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// readProxyNode projects the reader's physical graph once. Scalar values use
// the same native JSON scalar policy as the former transition, without ever
// expanding each occurrence of a shared container into a JSON document.
func readProxyNode(text string) (*yamlx.Node, error) {
	root, err := parseProxyYAML(text)
	if err != nil {
		return nil, err
	}
	return projectReadGraph(root, yamlx.StringifyScalarKeys)
}

// projectReadGraph applies the shared scalar/key policy to an owned physical graph.
func projectReadGraph(root *yamlx.Node, policy yamlx.JSONKeyMode) (*yamlx.Node, error) {
	if err := yamlx.ProjectJSONKeys(root, policy); err != nil {
		return nil, err
	}
	pending := []*yamlx.Node{root}
	seen := map[*yamlx.Node]bool{}
	for len(pending) > 0 {
		n := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if n == nil || seen[n] {
			continue
		}
		seen[n] = true
		n.HeadComment, n.LineComment, n.FootComment = "", "", ""
		if n.Kind == yaml.AliasNode {
			pending = append(pending, n.Alias)
			continue
		}
		if n.Kind == yaml.MappingNode {
			n.Tag = "!!map"
			n.Style = 0
		}
		if n.Kind == yaml.SequenceNode {
			n.Tag = "!!seq"
			n.Style = 0
		}
		// ProjectJSONKeys has already materialized merge pairs and replaced
		// mapping keys. Any remaining merge-tag scalar is a value (including
		// a root or alias target), so apply ToJSON's string policy to it too.
		if n.Kind == yaml.ScalarNode {
			data, err := yamlx.ToJSON(n)
			if err != nil {
				return nil, err
			}
			value, err := yamlx.FromJSON(data)
			if err != nil {
				return nil, err
			}
			n.Kind, n.Tag, n.Value, n.Style = value.Kind, value.Tag, value.Value, value.Style
		}
		pending = append(pending, n.Content...)
	}
	return root, nil
}
