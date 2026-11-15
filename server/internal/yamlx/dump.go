package yamlx

import (
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unsafe"

	"configflow/internal/jsonx"
)

// FlowStyle is PyYAML's default_flow_style.
type FlowStyle int

const (
	// FlowBlock is default_flow_style=False (PyYAML 6's default): block style
	// everywhere except empty collections, which are always "[]" and "{}".
	FlowBlock FlowStyle = iota
	// FlowFlow is default_flow_style=True: every collection in flow style.
	FlowFlow
	// FlowAuto is default_flow_style=None: a collection of plain scalars is
	// written in flow style, the rest in block style.
	FlowAuto
)

// DumpOptions are the yaml.dump keyword arguments the application uses.
// The zero value is `yaml.safe_dump(v, sort_keys=False)`; see SafeDump for the
// defaults of the Python function.
type DumpOptions struct {
	AllowUnicode bool
	SortKeys     bool
	FlowStyle    FlowStyle
	// Indent is `indent=`; values outside 2..9 (including 0) mean 2.
	Indent int
	// Width is `width=`; a value not above twice the indent means 80.
	Width int
	// IndentSequences makes list items inside a mapping indented under their
	// key, like the application's IndentDumper subclasses (increase_indent
	// overridden to never be "indentless").
	IndentSequences bool
	// ExplicitStart and ExplicitEnd are `explicit_start=` and `explicit_end=`.
	ExplicitStart bool
	ExplicitEnd   bool
	// NoAliases writes a value that is referenced twice (the same *Object or
	// the same non-empty slice) in full each time instead of as an anchor and
	// alias. PyYAML always uses anchors; keep this false unless the Python
	// code copied the value.
	NoAliases bool
	// OnMarked is called for every Marked scalar after it was written, with
	// the style actually used (0 plain, '\'' or '"') and the half-open range
	// of output characters (code points, not bytes) it occupies.
	OnMarked func(m Marked, style byte, start, end int)
}

// SafeDump is `yaml.safe_dump(v)`: sort_keys=True, allow_unicode=False.
func SafeDump(v any) (string, error) {
	return Dump(v, DumpOptions{SortKeys: true})
}

// SafeDumpUnicode is `yaml.safe_dump(v, allow_unicode=True, sort_keys=False)`,
// the call most of the application makes.
func SafeDumpUnicode(v any) (string, error) {
	return Dump(v, DumpOptions{AllowUnicode: true})
}

// IndentDump is `yaml.dump(v, Dumper=IndentDumper, allow_unicode=True,
// default_flow_style=False, sort_keys=False, indent=2)`.
func IndentDump(v any) (string, error) {
	return Dump(v, DumpOptions{AllowUnicode: true, Indent: 2, IndentSequences: true})
}

// Dump renders one document like yaml.dump with PyYAML's pure-Python
// SafeDumper (which represents exactly the jsonx value model plus the types
// declared in this package).
func Dump(v any, o DumpOptions) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch x := r.(type) {
			case emitterError:
				err = errors.New(x.msg)
			case representError:
				err = errors.New(x.msg)
			default:
				panic(r)
			}
		}
	}()
	r := &representer{opts: o, seen: map[idKey]*node{}}
	root := r.represent(v)
	e := newEmitter(o)
	e.emit(&event{kind: evStreamStart})
	s := &serializer{emitter: e, explicitStart: o.ExplicitStart, explicitEnd: o.ExplicitEnd,
		anchors: map[*node]string{}, anchored: map[*node]bool{}, serialized: map[*node]bool{}}
	s.serialize(root)
	e.emit(&event{kind: evStreamEnd})
	return e.out.String(), nil
}

type representError struct{ msg string }

type nodeKind int

const (
	scalarNode nodeKind = iota
	sequenceNode
	mappingNode
)

const (
	tagStr       = "tag:yaml.org,2002:str"
	tagNull      = "tag:yaml.org,2002:null"
	tagBool      = "tag:yaml.org,2002:bool"
	tagInt       = "tag:yaml.org,2002:int"
	tagFloat     = "tag:yaml.org,2002:float"
	tagSeq       = "tag:yaml.org,2002:seq"
	tagMap       = "tag:yaml.org,2002:map"
	tagBinary    = "tag:yaml.org,2002:binary"
	tagTimestamp = "tag:yaml.org,2002:timestamp"
	tagMerge     = "tag:yaml.org,2002:merge"
	tagValue     = "tag:yaml.org,2002:value"
	tagSet       = "tag:yaml.org,2002:set"
	tagOmap      = "tag:yaml.org,2002:omap"
	tagPairs     = "tag:yaml.org,2002:pairs"
)

type node struct {
	kind  nodeKind
	tag   string
	value string // scalars
	style byte   // scalars
	flow  bool   // collections
	items []*node
	pairs [][2]*node
	// anchorName is the fixed anchor of a SharedString scalar, if any.
	anchorName string
	mark       *Marked
}

type idKey struct {
	ptr  unsafe.Pointer
	n    int
	kind byte
}

type representer struct {
	opts DumpOptions
	seen map[idKey]*node
}

func (r *representer) flowFor(forced bool, nodes ...*node) bool {
	if forced {
		return true
	}
	switch r.opts.FlowStyle {
	case FlowFlow:
		return true
	case FlowAuto:
		for _, n := range nodes {
			if n.kind != scalarNode || n.style != 0 {
				return false
			}
		}
		return true
	}
	return false
}

func scalar(tag, value string) *node { return &node{kind: scalarNode, tag: tag, value: value} }

func (r *representer) represent(v any) *node {
	switch x := v.(type) {
	case nil:
		return scalar(tagNull, "null")
	case bool:
		if x {
			return scalar(tagBool, "true")
		}
		return scalar(tagBool, "false")
	case string:
		return scalar(tagStr, x)
	case Marked:
		n := scalar(tagStr, x.Value)
		m := x
		n.mark = &m
		return n
	case *SharedString:
		if x == nil {
			return scalar(tagNull, "null")
		}
		key := idKey{ptr: unsafe.Pointer(x), kind: 's'}
		if n, ok := r.seen[key]; ok && !r.opts.NoAliases {
			return n
		}
		n := scalar(tagStr, x.Value)
		n.anchorName = x.Anchor
		r.seen[key] = n
		return n
	case Timestamp:
		return scalar(tagTimestamp, x.String())
	case []byte:
		return &node{kind: scalarNode, tag: tagBinary, value: encodeBase64Lines(x), style: '|'}
	case *jsonx.Object:
		if x == nil {
			return scalar(tagNull, "null")
		}
		return r.mapping(x, false, unsafe.Pointer(x), 0, 'o')
	case FlowMap:
		if x.Object == nil {
			return scalar(tagNull, "null")
		}
		return r.mapping(x.Object, true, unsafe.Pointer(x.Object), 0, 'm')
	case []any:
		return r.sequence(x, false, 'l')
	case FlowList:
		return r.sequence([]any(x), true, 'f')
	}
	if n, ok := jsonx.AsNumber(v); ok {
		return representNumber(n)
	}
	panic(representError{fmt.Sprintf("cannot represent an object: %T", v)})
}

func representNumber(n jsonx.Number) *node {
	text := n.String()
	if n.IsInt() {
		return scalar(tagInt, text)
	}
	switch text {
	case "NaN":
		return scalar(tagFloat, ".nan")
	case "Infinity":
		return scalar(tagFloat, ".inf")
	case "-Infinity":
		return scalar(tagFloat, "-.inf")
	}
	text = strings.ToLower(text)
	if !strings.Contains(text, ".") && strings.Contains(text, "e") {
		text = strings.Replace(text, "e", ".0e", 1)
	}
	return scalar(tagFloat, text)
}

func encodeBase64Lines(data []byte) string {
	// base64.encodebytes: 76 characters per line, every line ends in "\n".
	encoded := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(encoded) > 76 {
		b.WriteString(encoded[:76])
		b.WriteByte('\n')
		encoded = encoded[76:]
	}
	if len(encoded) > 0 {
		b.WriteString(encoded)
		b.WriteByte('\n')
	}
	return b.String()
}

func (r *representer) sequence(items []any, forced bool, kind byte) *node {
	var key idKey
	if len(items) > 0 {
		key = idKey{ptr: unsafe.Pointer(unsafe.SliceData(items)), n: len(items), kind: kind}
		if n, ok := r.seen[key]; ok && !r.opts.NoAliases {
			return n
		}
	}
	n := &node{kind: sequenceNode, tag: tagSeq}
	if key.ptr != nil {
		r.seen[key] = n
	}
	for _, item := range items {
		n.items = append(n.items, r.represent(item))
	}
	n.flow = r.flowFor(forced, n.items...)
	return n
}

func (r *representer) mapping(o *jsonx.Object, forced bool, ptr unsafe.Pointer, size int, kind byte) *node {
	key := idKey{ptr: ptr, n: size, kind: kind}
	if n, ok := r.seen[key]; ok && !r.opts.NoAliases {
		return n
	}
	n := &node{kind: mappingNode, tag: tagMap}
	r.seen[key] = n
	keys := o.Keys()
	if r.opts.SortKeys {
		sort.Strings(keys)
	}
	var all []*node
	for _, k := range keys {
		value, _ := o.Get(k)
		nk := scalar(tagStr, k)
		nv := r.represent(value)
		n.pairs = append(n.pairs, [2]*node{nk, nv})
		all = append(all, nk, nv)
	}
	n.flow = r.flowFor(forced, all...)
	return n
}

// serializer is yaml/serializer.py.
type serializer struct {
	emitter       *emitter
	explicitStart bool
	explicitEnd   bool
	anchors       map[*node]string
	anchored      map[*node]bool // node seen (value "" until a second reference)
	serialized    map[*node]bool
	lastAnchorID  int
}

func (s *serializer) serialize(root *node) {
	s.emitter.emit(&event{kind: evDocumentStart, explicit: s.explicitStart})
	s.anchorNode(root)
	s.serializeNode(root)
	s.emitter.emit(&event{kind: evDocumentEnd, explicit: s.explicitEnd})
}

func (s *serializer) anchorNode(n *node) {
	if s.anchored[n] {
		if s.anchors[n] == "" {
			s.anchors[n] = s.generateAnchor(n)
		}
		return
	}
	s.anchored[n] = true
	switch n.kind {
	case sequenceNode:
		for _, item := range n.items {
			s.anchorNode(item)
		}
	case mappingNode:
		for _, p := range n.pairs {
			s.anchorNode(p[0])
			s.anchorNode(p[1])
		}
	}
}

func (s *serializer) generateAnchor(n *node) string {
	if n.kind == scalarNode && n.anchorName != "" {
		return n.anchorName
	}
	s.lastAnchorID++
	return fmt.Sprintf("id%03d", s.lastAnchorID)
}

func (s *serializer) serializeNode(n *node) {
	alias := s.anchors[n]
	if s.serialized[n] {
		s.emitter.emit(&event{kind: evAlias, anchor: alias, hasAnchor: true})
		return
	}
	s.serialized[n] = true
	switch n.kind {
	case scalarNode:
		detected := detectTag(n.value)
		s.emitter.emit(&event{kind: evScalar, anchor: alias, hasAnchor: alias != "", tag: n.tag, hasTag: true,
			implicit: [2]bool{n.tag == detected, n.tag == tagStr}, value: n.value, style: n.style, mark: n.mark})
	case sequenceNode:
		s.emitter.emit(&event{kind: evSequenceStart, anchor: alias, hasAnchor: alias != "", tag: n.tag, hasTag: true,
			implicit: [2]bool{true}, flow: n.flow})
		for _, item := range n.items {
			s.serializeNode(item)
		}
		s.emitter.emit(&event{kind: evSequenceEnd})
	case mappingNode:
		s.emitter.emit(&event{kind: evMappingStart, anchor: alias, hasAnchor: alias != "", tag: n.tag, hasTag: true,
			implicit: [2]bool{true}, flow: n.flow})
		for _, p := range n.pairs {
			s.serializeNode(p[0])
			s.serializeNode(p[1])
		}
		s.emitter.emit(&event{kind: evMappingEnd})
	}
}
