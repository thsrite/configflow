package oxidns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Dynamic values (native documents, settings, profile configuration) are
// *yaml.Node trees. Loaders resolve every alias to a shared pointer (pointer
// identity is object identity: an in-place edit reaches every holder) and give
// every scalar its resolved short tag.

// Node is a node of a dynamic document.
type Node = yaml.Node

const (
	tagStr   = "!!str"
	tagInt   = "!!int"
	tagFloat = "!!float"
	tagBool  = "!!bool"
	tagNull  = "!!null"
)

func isMap(n *Node) bool { n = yamlx.Resolve(n); return n != nil && n.Kind == yaml.MappingNode }
func isSeq(n *Node) bool { n = yamlx.Resolve(n); return n != nil && n.Kind == yaml.SequenceNode }
func isTag(n *Node, tag string) bool {
	n = yamlx.Resolve(n)
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == tag
}
func isStr(n *Node) bool  { return isTag(n, tagStr) }
func isInt(n *Node) bool  { return isTag(n, tagInt) }
func isBool(n *Node) bool { return isTag(n, tagBool) }

// isNone reports a missing or null field.
func isNone(n *Node) bool { return n == nil || isTag(n, tagNull) }

// isNum: int or finite float (not bool).
func isNum(n *Node) bool {
	if isTag(n, tagFloat) {
		f, err := strconv.ParseFloat(n.Value, 64)
		return err == nil && !math.IsInf(f, 0) && !math.IsNaN(f)
	}
	return isInt(n)
}

func str(n *Node) string {
	n = yamlx.Resolve(n)
	if isStr(n) {
		return n.Value
	}
	return ""
}

func isTrue(n *Node) bool { n = yamlx.Resolve(n); return isBool(n) && n.Value == "true" }

func intOf(n *Node) (*big.Int, bool) {
	if !isInt(n) {
		return nil, false
	}
	data, err := yamlx.ToJSON(n)
	if err != nil {
		return nil, false
	}
	v, ok := new(big.Int).SetString(string(data), 10)
	return v, ok
}

// int64Of reads an int that fits int64.
func int64Of(n *Node) (int64, bool) {
	if !isInt(n) {
		return 0, false
	}
	var v int64
	err := yamlx.Decode(n, &v)
	return v, err == nil
}

// ratOf reads a validated YAML number for exact schema range checks.
func ratOf(n *Node) (*big.Rat, bool) {
	if !isNum(n) {
		return nil, false
	}
	data, err := yamlx.ToJSON(n)
	if err != nil {
		return nil, false
	}
	value, ok := new(big.Rat).SetString(string(data))
	return value, ok
}

func get(n *Node, key string) *Node { return yamlx.Resolve(yamlx.Get(n, key)) }

func has(n *Node, key string) bool { return get(n, key) != nil }

// at follows a path of mapping keys.
func at(n *Node, keys ...string) *Node {
	for _, key := range keys {
		n = get(n, key)
	}
	return n
}

// presentation retains user annotations when an editor replaces a value.
func presentation(value, previous *Node) *Node {
	if value == nil || previous == nil || previous.Style == 0 && previous.HeadComment == "" && previous.LineComment == "" && previous.FootComment == "" {
		return value
	}
	v := *value
	v.HeadComment, v.LineComment, v.FootComment = previous.HeadComment, previous.LineComment, previous.FootComment
	if v.Kind == previous.Kind && v.Tag == previous.Tag {
		v.Style = previous.Style
	}
	if isStr(&v) {
		v.Style = stringStyle(v.Value, v.Style)
	}
	return &v
}

// set replaces the value of key or appends the pair.
func set(m *Node, key string, value *Node) { yamlx.Set(m, key, presentation(value, get(m, key))) }

// setDefault installs a missing field.
func setDefault(m *Node, key string, value *Node) *Node {
	if v := get(m, key); v != nil || has(m, key) {
		return v
	}
	set(m, key, value)
	return value
}

// del removes an effective key through the YAML facade.
func del(m *Node, key string) *Node { value := yamlx.Get(m, key); yamlx.Delete(m, key); return value }
func mapSize(m *Node) int {
	count := 0
	for range yamlx.Pairs(m) {
		count++
	}
	return count
}
func keys(m *Node) []string {
	out := []string{}
	for key := range yamlx.Pairs(m) {
		out = append(out, key)
	}
	return out
}
func pairs(m *Node, f func(string, *Node) bool) {
	for key, value := range yamlx.Pairs(m) {
		if !f(key, yamlx.Resolve(value)) {
			break
		}
	}
}

// items returns the elements of a sequence (nil otherwise).
func items(n *Node) []*Node {
	n = yamlx.Resolve(n)
	if !isSeq(n) {
		return nil
	}
	out := make([]*Node, len(n.Content))
	for i, value := range n.Content {
		out[i] = yamlx.Resolve(value)
	}
	return out
}

// mapOr returns the mapping or an empty optional section.
func mapOr(n *Node) *Node {
	if isMap(n) {
		return n
	}
	return M()
}

// S, I, B, Null and Num build scalars; M and L build containers from Go
// literals (see val).
func S(s string) *Node { return yamlx.Str(s) }

// stringStyle avoids yaml.v3's block emitter consuming the first value newline
// as the block-header newline. YAML also normalizes CR/NEL in physical scalar
// lines. Quoted output preserves these values and Unicode line separators;
// ordinary strings retain their literal/folded presentation.
func stringStyle(value string, style yaml.Style) yaml.Style {
	if strings.HasPrefix(value, "\n") || strings.ContainsAny(value, "\r\u0085\u2028\u2029") {
		return (style & yaml.TaggedStyle) | yaml.DoubleQuotedStyle
	}
	return style
}
func I(i int64) *Node           { return yamlx.Int(i) }
func B(value bool) *Node        { return yamlx.Bool(value) }
func Null() *Node               { return yamlx.Null() }
func Float(value float64) *Node { return yamlx.Float(value) }

// M builds a mapping from alternating keys and values.
func M(kv ...any) *Node {
	m := yamlx.Map()
	for i := 0; i+1 < len(kv); i += 2 {
		m.Content = append(m.Content, S(kv[i].(string)), val(kv[i+1]))
	}
	return m
}

// L builds a sequence.
func L(values ...any) *Node {
	l := yamlx.Seq()
	for _, v := range values {
		l.Content = append(l.Content, val(v))
	}
	return l
}

func seqOf(nodes []*Node) *Node {
	return yamlx.Seq(nodes...)
}

func strs(values []string) *Node {
	l := L()
	for _, v := range values {
		l.Content = append(l.Content, S(v))
	}
	return l
}

func val(v any) *Node {
	switch x := v.(type) {
	case *Node:
		if x == nil {
			return Null()
		}
		return x
	case nil:
		return Null()
	case string:
		return S(x)
	case int:
		return I(int64(x))
	case int64:
		return I(x)
	case bool:
		return B(x)
	case float64:
		return Float(x)
	case []*Node:
		return seqOf(x)
	case []string:
		return strs(x)
	case []any:
		return L(x...)
	}
	return &Node{Kind: yaml.ScalarNode, Tag: tagUnsupported, Value: fmt.Sprintf("unsupported Go value %T", v)}
}

// clone returns a caller-owned native YAML tree.
func clone(n *Node) *Node { return yamlx.CopyGraph(n) }

// shallow returns a detached container with shared immutable children.
func shallow(n *Node) *Node {
	c := *n
	c.Content = append([]*Node{}, n.Content...)
	return &c
}

// equal compares native YAML types and effective values.
func equal(a, b *Node) bool { return yamlx.Equal(a, b) }
func same(a, b *Node) bool  { return yamlx.Equal(a, b) }

// indexOf finds a structurally equal native YAML value.
func indexOf(list *Node, item *Node) int {
	for i, v := range items(list) {
		if equal(v, item) {
			return i
		}
	}
	return -1
}

func contains(list *Node, item *Node) bool { return indexOf(list, item) >= 0 }

func containsStr(list *Node, s string) bool { return contains(list, S(s)) }

// filter keeps the elements f accepts, in a new slice.
func filter(nodes []*Node, f func(*Node) bool) []*Node {
	out := []*Node{}
	for _, n := range nodes {
		if f(n) {
			out = append(out, n)
		}
	}
	return out
}

// tagOf is plugin.get('tag') when it is a string.
func tagOf(plugin *Node) (string, bool) {
	t := get(plugin, "tag")
	return str(t), isStr(t)
}

// ---- JSON ----

// FromJSON decodes a native JSON tree with the shared facade.
func FromJSON(data []byte) (*Node, error) { return yamlx.FromJSON(data) }

// ToJSON writes validated JSON-shaped nodes. Persistence uses Dynamic.SetNode,
// whose error-returning contract rejects unsupported document graphs.
func ToJSON(node *Node) []byte { data, _ := yamlx.ToJSON(node); return data }

// canonicalJSON applies Go encoding/json's deterministic mapping order to
// already validated JSON nodes; revision tokens are not persisted data.
func canonicalJSON(node *Node) []byte {
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return nil
	}
	var result bytes.Buffer
	encoder := json.NewEncoder(&result)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(value); err != nil {
		return nil
	}
	return bytes.TrimSuffix(result.Bytes(), []byte("\n"))
}

// deliveryNode expands merges for consumers that do not interpret YAML merge
// keys. Editor drafts retain their presentation; generated configurations carry
// only the effective keys and values, just as the Python generator did.
func deliveryNode(node *Node) *Node {
	owned := yamlx.CopyGraph(node)
	seen := map[*Node]bool{}
	var flatten func(*Node)
	flatten = func(n *Node) {
		n = yamlx.Resolve(n)
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		if isMap(n) {
			var content []*Node
			for key, value := range yamlx.AllPairs(n) {
				content = append(content, key, value)
			}
			n.Content = content
		}
		for _, child := range n.Content {
			flatten(child)
		}
	}
	flatten(owned)
	yamlx.MaterializeAliases(owned)
	return owned
}

// ---- YAML ----

// dump is yaml.safe_dump(allow_unicode=True, sort_keys=False): block style,
// two-space indentation, no anchors (shared nodes are written out).
func dump(node *Node) (string, error) {
	owned := yamlx.CopyGraph(node)
	yamlx.MaterializeAliases(owned)
	data, err := yamlx.Marshal(owned)
	if err != nil {
		return "", fmt.Errorf("encode OxiDNS YAML: %w", err)
	}
	return string(data), nil
}

func containsRune(s string, r rune) bool { return strings.ContainsRune(s, r) }

func trimSpace(s string) string { return strings.TrimSpace(s) }

// setNativeConfig serializes a draft before changing its stored text.
func setNativeConfig(settings, document *Node) error {
	text, err := dump(document)
	if err != nil {
		return err
	}
	set(settings, "native_config", S(text))
	return nil
}

// externalIncludes refuses any declared external graph; an empty include list
// is inert. Malformed non-list declarations also fail closed.
func externalIncludes(document *Node) bool {
	value := get(document, "include")
	return !isNone(value) && (!isSeq(value) || len(value.Content) > 0)
}
func positive(value *Node) bool { number, ok := ratOf(value); return ok && number.Sign() > 0 }

// numericZero checks a quantity without making bool and int interchangeable.
func numericZero(node *Node) bool {
	value, ok := ratOf(node)
	return ok && value.Sign() == 0
}
