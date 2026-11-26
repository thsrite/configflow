package yamlxtest

import (
	"math"
	"strconv"
	"strings"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Migration categories are the approved reader changes in design/n7-n8.md §5.7.
const (
	SyntaxRange          = "原生解析器语法范围"
	ExplicitScalarNative = "explicit-scalar-native"
	Equal                = "equal"
	Diagnostics          = "native-diagnostics"
	Duplicate            = "D1-duplicate"
	Policy               = "reader-policy"
	Alias                = "unsafe-alias"
	Typing               = "YAML1.1/timestamp"
	Tags                 = "tagged-collections"
	Unknown              = "未知标签：Python 拒绝 → Go 接受"
	Composite            = "复合映射键：Node 保留"
	Deep                 = "深层普通节点：Go 接受"
)

// SemanticEqual compares output meaning under the native scalar-text and tag
// policies. Scalar text is retained verbatim; presentation is ignored.
func SemanticEqual(left, right string) bool {
	a, err := yamlx.Parse([]byte(left))
	if err != nil {
		return false
	}
	b, err := yamlx.Parse([]byte(right))
	if err != nil {
		return false
	}
	normalize(a, map[*yamlx.Node]bool{})
	normalize(b, map[*yamlx.Node]bool{})
	return yamlx.Equal(a, b)
}

// ReferenceText quotes native string values for an independent PyYAML oracle.
// Unknown tags and non-JSON scalars become the text/shape the caller observes.
func ReferenceText(source string) (string, error) {
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		return "", err
	}
	normalize(node, map[*yamlx.Node]bool{})
	data, err := yamlx.Marshal(node)
	return string(data), err
}

func normalize(node *yamlx.Node, seen map[*yamlx.Node]bool) {
	if node == nil || seen[node] {
		return
	}
	seen[node] = true
	switch node.Kind {
	case yaml.MappingNode:
		node.Tag = "!!map"
	case yaml.SequenceNode:
		node.Tag = "!!seq"
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str", "!!bool", "!!null":
		case "!!int":
			if data, err := yamlx.ToJSON(node); err == nil {
				node.Value = string(data)
			}
		case "!!float":
			var value float64
			if yamlx.Decode(node, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
				node.Value = strconv.FormatFloat(value, 'g', -1, 64)
				if !strings.ContainsAny(node.Value, ".eE") {
					node.Value += ".0"
				}
			}
		case "!!timestamp", "!!binary":
			node.Tag = "!!str"
		default:
			node.Tag = "!!str"
		}
	}
	node.Style &^= yaml.TaggedStyle
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		node.Style = yaml.DoubleQuotedStyle
	}
	for _, child := range node.Content {
		normalize(child, seen)
	}
	normalize(node.Alias, seen)
}
