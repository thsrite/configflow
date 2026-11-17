package yamlxtest

import (
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// ParseNode records one original physical node, including its source position.
type ParseNode struct {
	ID           int       `json:"id"`
	Kind         yaml.Kind `json:"kind"`
	Tag          string    `json:"tag"`
	Value        string    `json:"value"`
	Line, Column int
	Content      []int `json:"content"`
	Alias        int   `json:"alias"`
}

// ParseAudit records tags and physical alias sharing before key projection.
func ParseAudit(root *yamlx.Node) []ParseNode {
	ids := map[*yamlx.Node]int{}
	var result []ParseNode
	var visit func(*yamlx.Node) int
	visit = func(n *yamlx.Node) int {
		if n == nil {
			return 0
		}
		if id := ids[n]; id != 0 {
			return id
		}
		id := len(result) + 1
		ids[n] = id
		result = append(result, ParseNode{ID: id, Kind: n.Kind, Tag: n.Tag, Value: n.Value, Line: n.Line, Column: n.Column})
		var children []int
		for _, child := range n.Content {
			children = append(children, visit(child))
		}
		alias := visit(n.Alias)
		result[id-1].Content = children
		result[id-1].Alias = alias
		return id
	}
	visit(root)
	return result
}
