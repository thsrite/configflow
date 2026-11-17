package gateway

import (
	"math/big"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Helpers for the *yaml.Node documents this package reads and edits.
// TODO(N7): use the shared yamlx Node helpers (n7-n8.md 2.2) once they land.

// Node tags.
const (
	tagStr   = "!!str"
	tagInt   = "!!int"
	tagFloat = "!!float"
	tagBool  = "!!bool"
	tagNull  = "!!null"
	tagMap   = "!!map"
	tagSeq   = "!!seq"
	// tagOpaque marks a scalar this package only carries (a YAML timestamp
	// or binary value of the source document); it counts as set and truthy.
	tagOpaque = "!configflow/opaque"
)

// resolve follows aliases and unwraps a document node.
func resolve(n *yaml.Node) *yaml.Node {
	for n != nil {
		switch {
		case n.Kind == yaml.AliasNode:
			n = n.Alias
		case n.Kind == yaml.DocumentNode && len(n.Content) == 1:
			n = n.Content[0]
		default:
			return n
		}
	}
	return nil
}

func isMap(n *yaml.Node) bool {
	n = resolve(n)
	return n != nil && n.Kind == yaml.MappingNode
}

func isScalar(n *yaml.Node, tag string) bool {
	n = resolve(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == tag
}

// stringValue is the text of a string scalar.
func stringValue(n *yaml.Node) (string, bool) {
	if !isScalar(n, tagStr) {
		return "", false
	}
	return resolve(n).Value, true
}

// numberIsZero reports an integer or float scalar equal to zero.
func numberIsZero(n *yaml.Node) bool {
	switch {
	case isScalar(n, tagInt):
		value, ok := new(big.Int).SetString(resolve(n).Value, 0)
		return ok && value.Sign() == 0
	case isScalar(n, tagFloat):
		value, err := strconv.ParseFloat(resolve(n).Value, 64)
		return err == nil && value == 0
	}
	return false
}

// truthy reports a value that is not null, false, zero, "" or an empty
// mapping/sequence.
func truthy(n *yaml.Node) bool {
	n = resolve(n)
	switch {
	case n == nil, isScalar(n, tagNull):
		return false
	case isScalar(n, tagBool):
		return n.Value == "true"
	case isScalar(n, tagInt), isScalar(n, tagFloat):
		return !numberIsZero(n)
	case isScalar(n, tagStr):
		return n.Value != ""
	case n.Kind == yaml.MappingNode, n.Kind == yaml.SequenceNode:
		return len(n.Content) > 0
	}
	return true
}

// unset reports a routing-mark value that leaves the default routing: null,
// false, zero or "" (an empty list or mapping still counts as set).
func unset(n *yaml.Node) bool {
	n = resolve(n)
	return n == nil || isScalar(n, tagNull) || (isScalar(n, tagBool) && n.Value == "false") ||
		numberIsZero(n) || (isScalar(n, tagStr) && n.Value == "")
}

// pairs calls fn for each string key of a mapping, in order.
func pairs(m *yaml.Node, fn func(key string, value *yaml.Node) bool) {
	m = resolve(m)
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if key, ok := stringValue(m.Content[i]); ok {
			if !fn(key, resolve(m.Content[i+1])) {
				return
			}
		}
	}
}

// get is the value of key in a mapping (nil when absent).
func get(m *yaml.Node, key string) *yaml.Node {
	var found *yaml.Node
	pairs(m, func(k string, v *yaml.Node) bool {
		if k == key {
			found = v
			return false
		}
		return true
	})
	return found
}

// set replaces the value of key in place, or appends the pair.
func set(m *yaml.Node, key string, value *yaml.Node) {
	m = resolve(m)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, ok := stringValue(m.Content[i]); ok && k == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, strNode(key), value)
}

// setDefault is set when key is absent; it returns the value of key.
func setDefault(m *yaml.Node, key string, value *yaml.Node) *yaml.Node {
	for i := 0; i+1 < len(resolve(m).Content); i += 2 {
		if k, ok := stringValue(resolve(m).Content[i]); ok && k == key {
			return resolve(resolve(m).Content[i+1])
		}
	}
	set(m, key, value)
	return value
}

// remove deletes key from a mapping.
func remove(m *yaml.Node, key string) {
	m = resolve(m)
	kept := m.Content[:0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, ok := stringValue(m.Content[i]); ok && k == key {
			continue
		}
		kept = append(kept, m.Content[i], m.Content[i+1])
	}
	m.Content = kept
}

// clone copies mappings and sequences (aliases become copies); scalars are
// shared, since nothing in this package edits a scalar in place.
func clone(n *yaml.Node) *yaml.Node {
	n = resolve(n)
	if n == nil || n.Kind == yaml.ScalarNode {
		return n
	}
	out := *n
	out.Content = make([]*yaml.Node, len(n.Content))
	for i, child := range n.Content {
		out.Content[i] = clone(child)
	}
	return &out
}

func strNode(s string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: s}
}

func boolNode(b bool) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagBool, Value: strconv.FormatBool(b)}
}

func intNode(i int64) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: strconv.FormatInt(i, 10)}
}

// mapNode builds a mapping from key, value, key, value, ...
func mapNode(kv ...any) *yaml.Node {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: tagMap}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Content = append(m.Content, strNode(kv[i].(string)), kv[i+1].(*yaml.Node))
	}
	return m
}

func strSeq(items []string) *yaml.Node {
	s := &yaml.Node{Kind: yaml.SequenceNode, Tag: tagSeq, Content: []*yaml.Node{}}
	for _, item := range items {
		s.Content = append(s.Content, strNode(item))
	}
	return s
}
