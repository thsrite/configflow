package yamlx

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Parse reads exactly one YAML document and returns its content node. Empty
// input is null. Duplicate keys, recursive aliases and excessive alias
// expansion are rejected before any caller can navigate the tree.
func Parse(src []byte) (*Node, error) {
	d := yaml.NewDecoder(bytes.NewReader(src))
	var doc Node
	if err := d.Decode(&doc); err != nil {
		if err == io.EOF {
			return Null(), nil
		}
		return nil, syntaxError(err)
	}
	var extra Node
	if err := d.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, syntaxError(err)
		}
		return nil, ErrMultipleDocuments
	}
	n := &doc
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return Null(), nil
		}
		n = contentNode(n)
	}
	if err := validateNode(n); err != nil {
		return nil, err
	}
	return n, nil
}

// Unmarshal parses one document using the facade's policies, then decodes it.
func Unmarshal(src []byte, v any) error {
	n, err := Parse(src)
	if err != nil {
		return err
	}
	return Decode(n, v)
}

// Decode decodes a subtree using yaml.v3's typed rules. Timestamps remain text
// under interface values, but retain yaml.v3's time.Time decoding in typed fields.
func Decode(n *Node, v any) (err error) {
	defer nativeRecovery(&err)
	t := reflect.TypeOf(v)
	if t == nil || t.Kind() != reflect.Pointer || reflect.ValueOf(v).IsNil() {
		return fmt.Errorf("yamlx: Decode destination must be a non-nil pointer")
	}
	if n == nil {
		n = Null()
	}
	if err := validateNode(n); err != nil {
		return err
	}
	c := decodeTree(n, t.Elem())
	if err := c.Decode(v); err != nil {
		var typeError *yaml.TypeError
		if errors.As(err, &typeError) {
			return &decodeTypeError{cause: err}
		}
		return syntaxError(err)
	}
	return nil
}

// Marshal encodes a node or typed value as YAML with two-space indentation,
// LF line endings and no line wrapping. Node annotations and aliases survive.
func Marshal(v any) (data []byte, err error) {
	defer nativeRecovery(&err)
	n, err := Encode(v)
	if err != nil {
		return nil, err
	}
	n = copyGraph(n, map[*Node]*Node{})
	prepareStrings(n, map[*Node]bool{})
	var b bytes.Buffer
	e := yaml.NewEncoder(&b)
	e.SetIndent(2)
	if err := e.Encode(n); err != nil {
		return nil, fmt.Errorf("encode YAML: %w", err)
	}
	if err := e.Close(); err != nil {
		return nil, fmt.Errorf("close YAML encoder: %w", err)
	}
	return b.Bytes(), nil
}

// Encode converts a typed value to a fresh node using its yaml tags. Node input
// is copied without losing anchors or aliases; document wrappers are removed.
func Encode(v any) (n *Node, err error) {
	defer nativeRecovery(&err)
	switch x := v.(type) {
	case *Node:
		n = x
	case Node:
		n = &x
	default:
		n = new(Node)
		if err := n.Encode(v); err != nil {
			return nil, fmt.Errorf("encode YAML node: %w", err)
		}
	}
	if n == nil {
		n = Null()
	}
	if err := validateNode(n); err != nil {
		return nil, err
	}
	if n.Kind == yaml.DocumentNode {
		n = contentNode(n)
	}
	return copyGraph(n, map[*Node]*Node{}), nil
}

func contentNode(doc *Node) *Node {
	c := *doc.Content[0]
	if doc.HeadComment != "" {
		c.HeadComment = joinComment(doc.HeadComment, c.HeadComment)
	}
	if doc.FootComment != "" {
		c.FootComment = joinComment(c.FootComment, doc.FootComment)
	}
	return &c
}

func joinComment(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "\n" + b
}

// yaml.v3 panics on unsupported Go kinds and invalid struct tags, even though
// its public encoders return errors. Contain those library panics at the facade.
func nativeRecovery(err *error) {
	if problem := recover(); problem != nil {
		if cause, ok := problem.(error); ok {
			*err = fmt.Errorf("yamlx: codec failed: %w", cause)
		} else {
			*err = fmt.Errorf("yamlx: codec failed: %v", problem)
		}
	}
}

func copyGraph(n *Node, memo map[*Node]*Node) *Node {
	if n == nil {
		return nil
	}
	if c := memo[n]; c != nil {
		return c
	}
	c := *n
	memo[n] = &c
	if n.Content != nil {
		c.Content = make([]*Node, len(n.Content))
		for i, child := range n.Content {
			c.Content[i] = copyGraph(child, memo)
		}
	}
	c.Alias = copyGraph(n.Alias, memo)
	return &c
}

func prepareStrings(n *Node, seen map[*Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	// yaml.v3 quotes an empty scalar in key position, which would change an
	// implicit empty null key into a string. Spell its null value explicitly.
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!null" && n.Value == "" {
		n.Value = "null"
	}
	// yaml.v3's folded emitter can insert an extra newline around more-indented
	// continuation lines. Literal style writes the actual value without refolding.
	if n.Kind == yaml.ScalarNode && n.Style&yaml.FoldedStyle != 0 {
		n.Style = n.Style&^yaml.FoldedStyle | yaml.LiteralStyle
	}
	protectString(n)
	// The Node encoder bypasses yaml.v3's string encoder's YAML 1.1 quoting.
	// Ask that encoder for the style instead of introducing another resolver.
	if n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" && n.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0 {
		s := n.Value
		if len(s) > 0 && (strings.ContainsRune("yYnNoO", rune(s[0])) || strings.Contains(s, ":")) {
			if data, err := yaml.Marshal(s); err == nil && len(data) > 0 && data[0] == '"' {
				n.Style |= yaml.DoubleQuotedStyle
			}
		}
	}
	for _, c := range n.Content {
		prepareStrings(c, seen)
	}
	prepareStrings(n.Alias, seen)
}

var timeType = reflect.TypeFor[time.Time]()
var nodeType = reflect.TypeFor[Node]()
var yamlUnmarshaler = reflect.TypeFor[yaml.Unmarshaler]()

func decodeTree(n *Node, t reflect.Type) *Node {
	if n == nil {
		return Null()
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nodeType || t == timeType || t != nil && reflect.PointerTo(t).Implements(yamlUnmarshaler) {
		return copyGraph(n, map[*Node]*Node{})
	}
	n = Resolve(n)
	if n == nil {
		return Null()
	}
	if n.Kind == yaml.DocumentNode {
		return decodeTree(n.Content[0], t)
	}
	c := *n
	c.Anchor = ""
	c.Alias = nil
	c.Content = nil
	if c.Kind == yaml.ScalarNode && c.ShortTag() == "!!timestamp" && (t == nil || t.Kind() == reflect.Interface) {
		c.Tag = "!!str"
	}
	for i, child := range n.Content {
		var target reflect.Type
		if t != nil {
			switch t.Kind() {
			case reflect.Slice, reflect.Array:
				target = t.Elem()
			case reflect.Map:
				if n.Kind == yaml.MappingNode && i%2 == 0 {
					target = t.Key()
				} else {
					target = t.Elem()
				}
			case reflect.Struct:
				if n.Kind == yaml.MappingNode && i%2 == 1 {
					if key, ok := stringKey(n.Content[i-1]); ok {
						target = yamlFieldType(t, key)
					}
				}
			}
		}
		// Merge sources are mappings decoded into the enclosing destination.
		if n.Kind == yaml.MappingNode && i%2 == 1 && n.Content[i-1].ShortTag() == "!!merge" {
			c.Content = append(c.Content, decodeMergeTree(child, t))
			continue
		}
		c.Content = append(c.Content, decodeTree(child, target))
	}
	return &c
}

func decodeMergeTree(n *Node, t reflect.Type) *Node {
	n = Resolve(n)
	if n.Kind != yaml.SequenceNode {
		return decodeTree(n, t)
	}
	c := *n
	c.Anchor = ""
	c.Content = make([]*Node, len(n.Content))
	for i, source := range n.Content {
		c.Content[i] = decodeTree(source, t)
	}
	return &c
}

func yamlFieldType(t reflect.Type, key string) reflect.Type {
	var inlineMap reflect.Type
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		if name == "-" {
			continue
		}
		if strings.Contains(","+opts+",", ",inline,") {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if found := yamlFieldType(ft, key); found != nil {
					return found
				}
			}
			if ft.Kind() == reflect.Map {
				inlineMap = ft.Elem()
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		if name == key {
			return f.Type
		}
	}
	return inlineMap
}
