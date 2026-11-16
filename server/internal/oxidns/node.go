package oxidns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"

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

func isMap(n *Node) bool { return n != nil && n.Kind == yaml.MappingNode }
func isSeq(n *Node) bool { return n != nil && n.Kind == yaml.SequenceNode }
func isTag(n *Node, tag string) bool {
	return n != nil && n.Kind == yaml.ScalarNode && n.Tag == tag
}
func isStr(n *Node) bool  { return isTag(n, tagStr) }
func isInt(n *Node) bool  { return isTag(n, tagInt) }
func isBool(n *Node) bool { return isTag(n, tagBool) }

// isNone: missing or null (Python None).
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
	if isStr(n) {
		return n.Value
	}
	return ""
}

func isTrue(n *Node) bool { return isBool(n) && n.Value == "true" }

// truthy is Python bool(value).
func truthy(n *Node) bool {
	switch {
	case isNone(n):
		return false
	case n.Kind == yaml.MappingNode:
		return mapSize(n) > 0
	case n.Kind == yaml.SequenceNode:
		return len(n.Content) > 0
	case n.Tag == tagBool:
		return n.Value == "true"
	case n.Tag == tagInt || n.Tag == tagFloat:
		r, _ := ratOf(n)
		return r != nil && r.Sign() != 0
	}
	return n.Value != ""
}

func intOf(n *Node) (*big.Int, bool) {
	if !isInt(n) {
		return nil, false
	}
	v, ok := new(big.Int).SetString(n.Value, 10)
	return v, ok
}

// int64Of reads an int that fits int64.
func int64Of(n *Node) (int64, bool) {
	if !isInt(n) {
		return 0, false
	}
	v, err := strconv.ParseInt(n.Value, 10, 64)
	return v, err == nil
}

// ratOf reads a number (int, float or bool as 0/1, like Python arithmetic).
func ratOf(n *Node) (*big.Rat, bool) {
	switch {
	case isBool(n):
		if n.Value == "true" {
			return big.NewRat(1, 1), true
		}
		return new(big.Rat), true
	case isInt(n), isTag(n, tagFloat):
		r, ok := new(big.Rat).SetString(n.Value)
		return r, ok
	}
	return nil, false
}

func get(n *Node, key string) *Node {
	if !isMap(n) {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Tag != "!!merge" && n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	for _, source := range mergeSources(n) {
		if value := get(source, key); value != nil {
			return value
		}
	}
	return nil
}

// mergeSources keeps YAML merge precedence: the first mapping in a sequence wins.
func mergeSources(n *Node) []*Node {
	if !isMap(n) {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Tag == "!!merge" {
			v := n.Content[i+1]
			if isMap(v) {
				return []*Node{v}
			}
			return items(v)
		}
	}
	return nil
}

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
func set(m *Node, key string, value *Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = presentation(value, m.Content[i+1])
			return
		}
	}
	m.Content = append(m.Content, S(key), value)
}

// setDefault is dict.setdefault.
func setDefault(m *Node, key string, value *Node) *Node {
	if v := get(m, key); v != nil || has(m, key) {
		return v
	}
	set(m, key, value)
	return value
}

// del removes key and returns its value (dict.pop).
func del(m *Node, key string) *Node {
	// Materialize merges before deletion so an inherited value cannot reappear.
	if len(mergeSources(m)) > 0 {
		flat := M()
		pairs(m, func(k string, v *Node) bool { set(flat, k, v); return true })
		m.Content = flat.Content
	}
	if !isMap(m) {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			m.Content = append(m.Content[:i:i], m.Content[i+2:]...)
			return v
		}
	}
	return nil
}

func mapSize(m *Node) int { return len(keys(m)) }

func keys(m *Node) []string {
	if !isMap(m) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if !seen[k] {
			out = append(out, k)
			seen[k] = true
		}
	}
	for _, source := range mergeSources(m) {
		for _, k := range keys(source) {
			add(k)
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Tag != "!!merge" {
			add(m.Content[i].Value)
		}
	}
	return out
}

// pairs visits effective mapping values, including inherited YAML merge keys.
func pairs(m *Node, f func(key string, value *Node) bool) {
	if !isMap(m) {
		return
	}
	if len(mergeSources(m)) == 0 {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if !f(m.Content[i].Value, m.Content[i+1]) {
				return
			}
		}
		return
	}
	for _, k := range keys(m) {
		if !f(k, get(m, k)) {
			return
		}
	}
}

// items returns the elements of a sequence (nil otherwise).
func items(n *Node) []*Node {
	if !isSeq(n) {
		return nil
	}
	return n.Content
}

// mapOr returns n when it is a mapping, else an empty mapping (Python
// `x if isinstance(x, dict) else {}`).
func mapOr(n *Node) *Node {
	if isMap(n) {
		return n
	}
	return M()
}

// S, I, B, Null and Num build scalars; M and L build containers from Go
// literals (see val).
func S(s string) *Node {
	return &Node{Kind: yaml.ScalarNode, Tag: tagStr, Value: s, Style: stringStyle(s, 0)}
}

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
func I(i int64) *Node {
	return &Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: strconv.FormatInt(i, 10)}
}
func B(b bool) *Node { return &Node{Kind: yaml.ScalarNode, Tag: tagBool, Value: strconv.FormatBool(b)} }
func Null() *Node    { return &Node{Kind: yaml.ScalarNode, Tag: tagNull, Value: "null"} }
func Float(f float64) *Node {
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eEn") {
		text += ".0"
	}
	return &Node{Kind: yaml.ScalarNode, Tag: tagFloat, Value: text}
}

// M builds a mapping from alternating keys and values.
func M(kv ...any) *Node {
	m := &Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for i := 0; i+1 < len(kv); i += 2 {
		m.Content = append(m.Content, S(kv[i].(string)), val(kv[i+1]))
	}
	return m
}

// L builds a sequence.
func L(values ...any) *Node {
	l := &Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*Node{}}
	for _, v := range values {
		l.Content = append(l.Content, val(v))
	}
	return l
}

func seqOf(nodes []*Node) *Node {
	return &Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: append([]*Node{}, nodes...)}
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

// clone is copy.deepcopy: shared nodes stay shared inside the copy.
func clone(n *Node) *Node { return cloneMemo(n, map[*Node]*Node{}) }

func cloneMemo(n *Node, memo map[*Node]*Node) *Node {
	if n == nil {
		return nil
	}
	if c, ok := memo[n]; ok {
		return c
	}
	c := new(Node)
	*c = *n
	c.Anchor = ""
	memo[n] = c
	if n.Content != nil {
		c.Content = make([]*Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = cloneMemo(child, memo)
		}
	}
	return c
}

// shallow is dict(x) / list(x): a new container with the same children.
func shallow(n *Node) *Node {
	c := *n
	c.Content = append([]*Node{}, n.Content...)
	return &c
}

// equal is Python ==: numbers by value (True == 1), mappings regardless of order.
func equal(a, b *Node) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return isNone(a) && isNone(b)
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.MappingNode:
		if len(keys(a)) != len(keys(b)) {
			return false
		}
		for _, key := range keys(a) {
			other := get(b, key)
			if other == nil || !equal(get(a, key), other) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equal(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	}
	ra, oka := ratOf(a)
	rb, okb := ratOf(b)
	if oka || okb {
		return oka && okb && ra.Cmp(rb) == 0
	}
	return a.Tag == b.Tag && (a.Tag == tagNull || a.Value == b.Value)
}

// same is the editors' _same: equal, but types must match exactly
// (False is not 0, 1 is not 1.0).
func same(a, b *Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.MappingNode:
		if len(keys(a)) != len(keys(b)) {
			return false
		}
		for _, key := range keys(a) {
			if !has(b, key) || !same(get(a, key), get(b, key)) {
				return false
			}
		}
		return true
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !same(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	}
	return a.Tag == b.Tag && equal(a, b)
}

// indexOf is list.index by ==; -1 when absent.
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

// FromJSON decodes JSON keeping key order and the int/float distinction.
func FromJSON(data []byte) (*Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	n, err := decodeJSON(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("oxidns: trailing JSON data")
	}
	return n, nil
}

func decodeJSON(dec *json.Decoder) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			m := M()
			for dec.More() {
				k, err := dec.Token()
				if err != nil {
					return nil, err
				}
				v, err := decodeJSON(dec)
				if err != nil {
					return nil, err
				}
				set(m, k.(string), v)
			}
			_, err = dec.Token()
			return m, err
		}
		l := L()
		for dec.More() {
			v, err := decodeJSON(dec)
			if err != nil {
				return nil, err
			}
			l.Content = append(l.Content, v)
		}
		_, err = dec.Token()
		return l, err
	case string:
		return S(t), nil
	case bool:
		return B(t), nil
	case nil:
		return Null(), nil
	case json.Number:
		if strings.ContainsAny(string(t), ".eE") {
			f, _ := t.Float64()
			return Float(f), nil
		}
		return &Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: string(t)}, nil
	}
	return nil, fmt.Errorf("oxidns: unexpected JSON token %v", tok)
}

// MarshalJSON encodes a node as JSON (mappings keep their order).
func ToJSON(n *Node) []byte {
	var b bytes.Buffer
	writeJSON(&b, n)
	return b.Bytes()
}

// canonicalJSON uses encoding/json's deterministic mapping-key order. Number
// spellings retain their YAML scalar kind, so 1 and 1.0 produce different tokens.
func canonicalJSON(n *Node) []byte {
	var native func(*Node) any
	native = func(n *Node) any {
		switch {
		case n == nil || isTag(n, tagNull):
			return nil
		case isMap(n):
			m := map[string]any{}
			pairs(n, func(k string, v *Node) bool { m[k] = native(v); return true })
			return m
		case isSeq(n):
			values := make([]any, len(n.Content))
			for i, v := range n.Content {
				values[i] = native(v)
			}
			return values
		case isBool(n):
			return n.Value == "true"
		case isInt(n), isTag(n, tagFloat):
			if json.Valid([]byte(n.Value)) {
				return json.Number(n.Value)
			}
			return n.Value
		default:
			return n.Value
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(native(n)) // only JSON numbers, strings, booleans and containers above
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}

func writeJSON(b *bytes.Buffer, n *Node) {
	switch {
	case n == nil || isTag(n, tagNull):
		b.WriteString("null")
	case n.Kind == yaml.MappingNode:
		order := keys(n)
		b.WriteByte('{')
		for j, key := range order {
			if j > 0 {
				b.WriteByte(',')
			}
			writeString(b, key)
			b.WriteByte(':')
			writeJSON(b, get(n, key))
		}
		b.WriteByte('}')
	case n.Kind == yaml.SequenceNode:
		b.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, c)
		}
		b.WriteByte(']')
	case n.Tag == tagInt || n.Tag == tagBool:
		b.WriteString(n.Value)
	case n.Tag == tagFloat:
		b.WriteString(n.Value)
	default:
		writeString(b, n.Value)
	}
}

func writeString(b *bytes.Buffer, s string) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.Encode(s)
	data := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	b.Write(data)
}

// deliveryNode expands merges for consumers that do not interpret YAML merge
// keys. Editor drafts retain their presentation; generated configurations carry
// only the effective keys and values, just as the Python generator did.
func deliveryNode(n *Node) *Node {
	if n == nil {
		return nil
	}
	out := *n
	out.Anchor = ""
	if isMap(n) {
		out.Content = nil
		pairs(n, func(k string, v *Node) bool { out.Content = append(out.Content, S(k), deliveryNode(v)); return true })
	} else if isSeq(n) {
		out.Content = make([]*Node, len(n.Content))
		for i, v := range n.Content {
			out.Content[i] = deliveryNode(v)
		}
	}
	return &out
}

// ---- YAML ----

// dump is yaml.safe_dump(allow_unicode=True, sort_keys=False): block style,
// two-space indentation, no anchors (shared nodes are written out).
func dump(n *Node) (string, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return "", fmt.Errorf("encode OxiDNS YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("close OxiDNS YAML: %w", err)
	}
	return b.String(), nil
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
