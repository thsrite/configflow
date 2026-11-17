package yamlxtest

import (
	"fmt"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// WriterNode is a physical node recorded from Python safe_load of old output.
// Children are indexes into the containing WriterReference's Nodes slice.
type WriterNode struct {
	Kind     uint32 `json:"kind"`
	Tag      string `json:"tag"`
	Value    string `json:"value,omitempty"`
	Style    uint32 `json:"style,omitempty"`
	Anchor   string `json:"anchor,omitempty"`
	Children []int  `json:"children,omitempty"`
}

// WriterChange records one old consumer interpretation corrected by a writer.
type WriterChange struct {
	Path     string         `json:"path"`
	Before   string         `json:"before"`
	After    string         `json:"after"`
	Category string         `json:"category"`
	Restored map[string]int `json:"restored,omitempty"`
}

// WriterReference preserves the source row, output digest and Python value graph.
// It is migration test data, never a runtime adapter or business-code oracle.
type WriterReference struct {
	Corpus  string         `json:"corpus"`
	Row     int            `json:"row"`
	SHA256  string         `json:"sha256"`
	Text    string         `json:"text,omitempty"`
	Nodes   []WriterNode   `json:"nodes"`
	Changes []WriterChange `json:"changes,omitempty"`
}

// Document reconstructs a recorded physical graph without decoding scalar values.
func (r WriterReference) Document() (*yamlx.Node, error) {
	if len(r.Nodes) == 0 {
		return nil, fmt.Errorf("empty Python writer graph")
	}
	nodes := make([]*yamlx.Node, len(r.Nodes))
	for i, v := range r.Nodes {
		nodes[i] = &yamlx.Node{Kind: yaml.Kind(v.Kind), Tag: v.Tag, Value: v.Value, Style: yaml.Style(v.Style), Anchor: v.Anchor}
	}
	for i, v := range r.Nodes {
		for _, child := range v.Children {
			if child < 0 || child >= len(nodes) {
				return nil, fmt.Errorf("Python writer node %d has invalid child %d", i, child)
			}
			nodes[i].Content = append(nodes[i].Content, nodes[child])
		}
	}
	return nodes[0], nil
}
