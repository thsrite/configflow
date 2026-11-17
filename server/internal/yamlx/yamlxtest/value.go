package yamlxtest

import (
	"configflow/internal/jsonx"
	"encoding/json"
)

// ValueNode records a physical JSON value without expanding shared containers.
type ValueNode struct {
	Kind  string      `json:"kind"`
	Value any         `json:"value,omitempty"`
	Pairs []ValuePair `json:"pairs,omitempty"`
	Items []int       `json:"items,omitempty"`
}

// ValuePair records an ordered string key and child identity.
type ValuePair struct {
	Key   string `json:"key"`
	Value int    `json:"value"`
}

// ValueGraph records transition values before they are injected into Python.
func ValueGraph(value any) []ValueNode {
	type listID struct {
		First  *any
		Length int
	}
	seen := map[any]int{}
	nodes := []ValueNode{}
	var visit func(any) int
	visit = func(value any) int {
		var identity any
		switch v := value.(type) {
		case *jsonx.Object:
			identity = v
		case []any:
			if len(v) > 0 {
				identity = listID{&v[0], len(v)}
			}
		}
		if identity != nil && seen[identity] != 0 {
			return seen[identity]
		}
		id := len(nodes) + 1
		nodes = append(nodes, ValueNode{})
		if identity != nil {
			seen[identity] = id
		}
		n := ValueNode{Kind: "scalar", Value: value}
		if number, ok := value.(jsonx.Number); ok {
			n.Value = json.Number(number)
		}
		switch v := value.(type) {
		case *jsonx.Object:
			n = ValueNode{Kind: "mapping"}
			for key, child := range v.All() {
				n.Pairs = append(n.Pairs, ValuePair{key, visit(child)})
			}
		case []any:
			n = ValueNode{Kind: "sequence"}
			for _, child := range v {
				n.Items = append(n.Items, visit(child))
			}
		}
		nodes[id-1] = n
		return id
	}
	visit(value)
	return nodes
}
