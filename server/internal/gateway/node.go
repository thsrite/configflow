package gateway

import (
	"math/big"
	"strconv"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Value predicates and builders for the *yaml.Node documents this package
// reads and edits, on top of the shared yamlx node helpers.

func isMap(n *yaml.Node) bool {
	n = yamlx.Resolve(n)
	return n != nil && n.Kind == yaml.MappingNode
}

func isScalar(n *yaml.Node, tag string) bool {
	n = yamlx.Resolve(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == tag
}

// stringValue is the text of a string scalar.
func stringValue(n *yaml.Node) (string, bool) {
	if !isScalar(n, "!!str") {
		return "", false
	}
	return yamlx.Resolve(n).Value, true
}

// numberIsZero reports an integer or float scalar equal to zero.
func numberIsZero(n *yaml.Node) bool {
	switch {
	case isScalar(n, "!!int"):
		value, ok := new(big.Int).SetString(yamlx.Resolve(n).Value, 0)
		return ok && value.Sign() == 0
	case isScalar(n, "!!float"):
		value, err := strconv.ParseFloat(yamlx.Resolve(n).Value, 64)
		return err == nil && value == 0
	}
	return false
}

// truthy reports a value that is not absent, null, false, zero, "" or an
// empty mapping/sequence.
func truthy(n *yaml.Node) bool {
	n = yamlx.Resolve(n)
	switch {
	case n == nil, isScalar(n, "!!null"):
		return false
	case isScalar(n, "!!bool"):
		return n.Value == "true"
	case isScalar(n, "!!int"), isScalar(n, "!!float"):
		return !numberIsZero(n)
	case isScalar(n, "!!str"):
		return n.Value != ""
	case n.Kind == yaml.MappingNode, n.Kind == yaml.SequenceNode:
		return len(n.Content) > 0
	}
	return true
}

// unset reports a routing-mark value that keeps the default routing: null,
// false, zero or "" (an empty list or mapping still counts as set).
func unset(n *yaml.Node) bool {
	n = yamlx.Resolve(n)
	return n == nil || isScalar(n, "!!null") || (isScalar(n, "!!bool") && n.Value == "false") ||
		numberIsZero(n) || (isScalar(n, "!!str") && n.Value == "")
}

// setDefault sets key when it is absent and returns the value of key.
func setDefault(m *yaml.Node, key string, value *yaml.Node) *yaml.Node {
	if current := yamlx.Get(m, key); current != nil {
		return yamlx.Resolve(current)
	}
	yamlx.Set(m, key, value)
	return value
}

// cloneShared copies mappings and sequences but shares scalar nodes, which
// this package never edits in place. A container reached twice (an alias)
// is copied once and stays shared in the copy, like Python's deepcopy.
// Unlike yamlx.Clone it keeps scalar identity, so the jsonx boundary (codec)
// can return untouched values of a copied TUN or DNS section exactly.
func cloneShared(n *yaml.Node) *yaml.Node {
	return cloneSharedMemo(n, map[*yaml.Node]*yaml.Node{})
}

func cloneSharedMemo(n *yaml.Node, copies map[*yaml.Node]*yaml.Node) *yaml.Node {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind == yaml.ScalarNode {
		return n
	}
	if done, ok := copies[n]; ok {
		return done
	}
	out := *n
	out.Anchor = ""
	out.Content = make([]*yaml.Node, len(n.Content))
	copies[n] = &out
	for i, child := range n.Content {
		out.Content[i] = cloneSharedMemo(child, copies)
	}
	return &out
}

// strSeq is a sequence of strings.
func strSeq(items []string) *yaml.Node {
	nodes := make([]*yaml.Node, len(items))
	for i, item := range items {
		nodes[i] = yamlx.Str(item)
	}
	return yamlx.Seq(nodes...)
}
