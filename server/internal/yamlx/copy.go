package yamlx

// CopyGraph returns an independently owned physical graph. It preserves aliases,
// anchors and shared references without expanding or truncating them. Copy work
// and memory scale with physical node count, not the expanded document size.
// It does not validate the graph; Parse and the writing/decoding boundaries
// retain their existing policy and alias-budget checks.
func CopyGraph(node *Node) *Node { return copyGraph(node, map[*Node]*Node{}) }
