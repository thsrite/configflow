package yamlx

import (
	"iter"
	"math"
	"math/big"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Node is a yaml.v3 node. Dynamic documents use content nodes, not document wrappers.
type Node = yaml.Node

// Map builds a mapping from alternating key and value nodes.
// An odd number of nodes is retained and rejected by Marshal or Decode.
func Map(pairs ...*Node) *Node {
	return &Node{Kind: yaml.MappingNode, Tag: "!!map", Content: nodeItems(pairs)}
}

// Seq builds a sequence.
func Seq(items ...*Node) *Node {
	return &Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: nodeItems(items)}
}

func nodeItems(items []*Node) []*Node {
	out := append([]*Node(nil), items...)
	for i, n := range out {
		if n == nil {
			out[i] = Null()
		}
	}
	return out
}

// Str builds a string scalar, protecting line separators from YAML normalisation.
func Str(s string) *Node {
	n := &Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	protectString(n)
	return n
}

func protectString(n *Node) {
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && (strings.HasPrefix(n.Value, "\n") || strings.ContainsAny(n.Value, "\r\u0085\u2028\u2029")) {
		n.Style = n.Style&yaml.TaggedStyle | yaml.DoubleQuotedStyle
	}
}

// Int builds an integer scalar.
func Int(i int64) *Node {
	return &Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(i, 10)}
}

// Float builds a float scalar, including YAML's non-finite values.
func Float(f float64) *Node {
	s := strconv.FormatFloat(f, 'g', -1, 64)
	switch {
	case math.IsNaN(f):
		s = ".nan"
	case math.IsInf(f, 1):
		s = ".inf"
	case math.IsInf(f, -1):
		s = "-.inf"
	case !strings.ContainsAny(s, ".eE"):
		s += ".0"
	}
	return &Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}
}

// Bool builds a boolean scalar.
func Bool(b bool) *Node {
	return &Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}
}

// Null builds a null scalar.
func Null() *Node { return &Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"} }

// Resolve follows aliases; a missing target or a hand-built alias loop returns nil.
func Resolve(n *Node) *Node {
	seen := map[*Node]bool{}
	for n != nil && n.Kind == yaml.AliasNode {
		if seen[n] {
			return nil
		}
		seen[n] = true
		n = n.Alias
	}
	return n
}

func stringKey(n *Node) (string, bool) {
	n = Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return n.Value, true
	}
	return "", false
}

// Get returns the effective value of a string key, following aliases and merges.
// Explicit keys override inherited keys; nil means absent or not a mapping.
func Get(m *Node, key string) *Node { return getNode(m, key, map[*Node]bool{}) }

func getNode(m *Node, key string, path map[*Node]bool) *Node {
	m = Resolve(m)
	if m == nil || m.Kind != yaml.MappingNode || path[m] {
		return nil
	}
	path[m] = true
	defer delete(path, m)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, ok := stringKey(m.Content[i]); ok && k == key {
			return m.Content[i+1]
		}
	}
	for _, source := range nativeMergeSources(m) {
		if v := getNode(source, key, path); v != nil {
			return v
		}
	}
	return nil
}

func nativeMergeSources(m *Node) []*Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := Resolve(m.Content[i])
		if k == nil || k.ShortTag() != "!!merge" {
			continue
		}
		v := Resolve(m.Content[i+1])
		if v == nil {
			return nil
		}
		if v.Kind == yaml.SequenceNode {
			return v.Content
		}
		return []*Node{v}
	}
	return nil
}

// Set replaces an explicit pair in place or appends it. A non-mapping is unchanged.
func Set(m *Node, key string, v *Node) {
	m = Resolve(m)
	if m == nil || m.Kind != yaml.MappingNode {
		return
	}
	if v == nil {
		v = Null()
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, ok := stringKey(m.Content[i]); ok && k == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, Str(key), v)
}

// Delete removes an effective string key. Merges are materialised first so an
// inherited value cannot reappear; non-string keys are retained.
func Delete(m *Node, key string) bool {
	m = Resolve(m)
	if m == nil || m.Kind != yaml.MappingNode || Get(m, key) == nil {
		return false
	}
	if len(nativeMergeSources(m)) != 0 {
		var content []*Node
		for _, p := range effectivePairs(m, map[*Node]bool{}) {
			content = append(content, p.key, p.value)
		}
		m.Content = content
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if k, ok := stringKey(m.Content[i]); ok && k == key {
			m.Content = append(m.Content[:i:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// Pairs iterates effective string keys in stored order, inherited keys first.
// Non-string keys are skipped. Explicit values override merge defaults.
func Pairs(m *Node) iter.Seq2[string, *Node] {
	return func(yield func(string, *Node) bool) {
		for _, p := range effectivePairs(m, map[*Node]bool{}) {
			if k, ok := stringKey(p.key); ok && !yield(k, p.value) {
				return
			}
		}
	}
}

type nodePair struct{ key, value *Node }

func effectivePairs(m *Node, path map[*Node]bool) []nodePair {
	m = Resolve(m)
	if m == nil || m.Kind != yaml.MappingNode || path[m] {
		return nil
	}
	path[m] = true
	defer delete(path, m)
	var out []nodePair
	index := map[string]int{}
	appendPair := func(p nodePair, override bool) {
		if k, ok := stringKey(p.key); ok {
			if i, exists := index[k]; exists {
				if override {
					out[i] = p
				}
				return
			}
			index[k] = len(out)
		} else {
			for i, old := range out {
				if Equal(old.key, p.key) {
					if override {
						out[i] = p
					}
					return
				}
			}
		}
		out = append(out, p)
	}
	for _, source := range nativeMergeSources(m) {
		for _, p := range effectivePairs(source, path) {
			appendPair(p, false)
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := Resolve(m.Content[i])
		if k != nil && k.ShortTag() == "!!merge" {
			continue
		}
		appendPair(nodePair{m.Content[i], m.Content[i+1]}, true)
	}
	return out
}

// Clone deeply copies a tree and expands aliases into independent copies.
// Hand-built cycles and alias expansions beyond yaml.v3's budget become Null
// at the offending edge. Parse rejects both before a document reaches Clone.
func Clone(n *Node) *Node {
	b := &aliasBudget{}
	return cloneNode(n, map[*Node]bool{}, b, false)
}

func cloneNode(n *Node, path map[*Node]bool, b *aliasBudget, alias bool) *Node {
	if n == nil {
		return nil
	}
	if path[n] || !b.visit(alias) {
		return Null()
	}
	path[n] = true
	defer delete(path, n)
	if n.Kind == yaml.AliasNode {
		return cloneNode(n.Alias, path, b, true)
	}
	c := *n
	c.Anchor = ""
	c.Alias = nil
	c.Content = nil
	if n.Content != nil {
		c.Content = make([]*Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = cloneNode(child, path, b, alias)
		}
	}
	return &c
}

// Equal compares resolved tags and values, ignoring mapping order and
// presentation. Merges are compared by effective values; numbers by value
// within their tag (a bool, int and float remain distinct).
func Equal(a, b *Node) bool { return equalNode(a, b, map[[2]*Node]bool{}) }

func equalNode(a, b *Node, path map[[2]*Node]bool) bool {
	a, b = Resolve(a), Resolve(b)
	if a == b {
		return true
	}
	if a == nil || b == nil || a.Kind != b.Kind || a.ShortTag() != b.ShortTag() {
		return false
	}
	p := [2]*Node{a, b}
	if path[p] {
		return true
	}
	path[p] = true
	defer delete(path, p)
	switch a.Kind {
	case yaml.MappingNode:
		ap, bp := effectivePairs(a, map[*Node]bool{}), effectivePairs(b, map[*Node]bool{})
		if len(ap) != len(bp) {
			return false
		}
		byString := map[string]*Node{}
		for _, q := range bp {
			if k, ok := stringKey(q.key); ok {
				byString[k] = q.value
			}
		}
		for _, q := range ap {
			if k, ok := stringKey(q.key); ok {
				v, exists := byString[k]
				if !exists || !equalNode(q.value, v, path) {
					return false
				}
				continue
			}
			found := false
			for _, r := range bp {
				if equalNode(q.key, r.key, path) && equalNode(q.value, r.value, path) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case yaml.SequenceNode, yaml.DocumentNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equalNode(a.Content[i], b.Content[i], path) {
				return false
			}
		}
		return true
	case yaml.ScalarNode:
		switch a.ShortTag() {
		case "!!null":
			return true
		case "!!bool":
			return strings.EqualFold(a.Value, b.Value)
		case "!!int":
			x, xok := yamlInteger(a.Value)
			y, yok := yamlInteger(b.Value)
			return xok && yok && x.Cmp(y) == 0
		case "!!float":
			return floatKey(a.Value) == floatKey(b.Value)
		}
	}
	return a.Value == b.Value
}

func yamlInteger(s string) (*big.Int, bool) {
	s = strings.ReplaceAll(s, "_", "")
	return new(big.Int).SetString(s, 0)
}

// floatKey normalises decimal floats without allocating exponent-sized powers.
func floatKey(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, "_", ""))
	negative := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "+"), "-")
	if s == ".nan" {
		return "nan"
	}
	if s == ".inf" {
		if negative {
			return "-inf"
		}
		return "inf"
	}
	mantissa, exp, hasExp := strings.Cut(s, "e")
	e := new(big.Int)
	if hasExp {
		if _, ok := e.SetString(exp, 10); !ok {
			return "invalid:" + s
		}
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	digits := strings.TrimLeft(whole+fraction, "0")
	if digits == "" {
		return "0"
	}
	trimmed := strings.TrimRight(digits, "0")
	e.Add(e, big.NewInt(int64(len(digits)-len(trimmed)-len(fraction))))
	if negative {
		trimmed = "-" + trimmed
	}
	return trimmed + "e" + e.String()
}
