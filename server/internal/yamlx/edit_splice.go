package yamlx

import (
	"bytes"
	"sort"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type sourceLine struct{ start, end, next int }
type textPatch struct {
	start, end int
	text       []byte
	pair       bool
}
type editSplicer struct {
	source   []byte
	lines    []sourceLine
	columns  [][]int // indexed rune columns; nil keeps the editor's sparse lookup
	before   editSnapshot
	patches  []textPatch
	anchored map[*Node]bool
	seen     map[*Node]bool
}

func spliceEdit(src []byte, doc *Node, before editSnapshot) ([]byte, bool) {
	if bytes.Contains(src, []byte("\u0085")) || bytes.Contains(src, []byte("\u2028")) || bytes.Contains(src, []byte("\u2029")) {
		return nil, false
	}
	p := editSplicer{source: src, lines: editSourceLines(src), before: before, anchored: make(map[*Node]bool), seen: make(map[*Node]bool)}
	for _, n := range before.nodes {
		if n.Kind == yaml.AliasNode {
			walkEditNodes(n.Alias, func(n *Node) { p.anchored[n] = true })
		}
	}
	if !p.visit(doc, false, false) {
		return nil, false
	}
	sort.Slice(p.patches, func(i, j int) bool { return p.patches[i].start < p.patches[j].start })
	var out bytes.Buffer
	from := 0
	for _, patch := range p.patches {
		if patch.start < from || patch.end < patch.start || patch.end > len(src) {
			return nil, false
		}
		if patch.pair {
			for _, comment := range before.comments {
				if bytes.Count(src[patch.start:patch.end], []byte(comment)) > bytes.Count(patch.text, []byte(comment)) {
					return nil, false
				}
			}
		}
		out.Write(src[from:patch.start])
		out.Write(patch.text)
		from = patch.end
	}
	out.Write(src[from:])
	return out.Bytes(), true
}

func editSourceLines(src []byte) []sourceLine {
	var out []sourceLine
	for start := 0; start < len(src); {
		end := start
		for end < len(src) && yamlBreakWidth(src[end:]) == 0 {
			end++
		}
		next := end
		if next < len(src) {
			next += yamlBreakWidth(src[next:])
		}
		out = append(out, sourceLine{start, end, next})
		start = next
	}
	return out
}

func yamlBreakWidth(src []byte) int {
	if len(src) == 0 {
		return 0
	}
	switch src[0] {
	case '\r':
		if len(src) > 1 && src[1] == '\n' {
			return 2
		}
		return 1
	case '\n':
		return 1
	case 0xc2:
		if len(src) > 1 && src[1] == 0x85 {
			return 2
		}
	case 0xe2:
		if len(src) > 2 && src[1] == 0x80 && (src[2] == 0xa8 || src[2] == 0xa9) {
			return 3
		}
	}
	return 0
}

func (p *editSplicer) offset(n *Node) (int, bool) {
	if n.Line < 1 || n.Line > len(p.lines) || n.Column < 1 {
		return 0, false
	}
	line := p.lines[n.Line-1]
	if p.columns != nil {
		columns := p.columns[n.Line-1]
		if columns != nil {
			if n.Column > len(columns) {
				return 0, false
			}
			return columns[n.Column-1], true
		}
		if n.Column-1 > line.end-line.start {
			return 0, false
		}
		offset := line.start + n.Column - 1
		return offset, true
	}
	offset := line.start
	if n.Line == 1 && bytes.HasPrefix(p.source, []byte("\ufeff")) {
		offset += 3
	}
	for column := 1; column < n.Column; column++ {
		if offset >= line.end {
			return 0, false
		}
		_, size := utf8.DecodeRune(p.source[offset:line.end])
		offset += size
	}
	return offset, offset <= line.end
}

func (p *editSplicer) visit(n *Node, flow, isKey bool) bool {
	if n == nil {
		return false
	}
	if p.seen[n] {
		return true
	}
	p.seen[n] = true
	old, ok := p.before.nodes[n]
	if !ok || !sameEditMetadata(&old, n) {
		return false
	}
	flow = flow || n.Style&yaml.FlowStyle != 0
	if old.Value != n.Value || old.Tag != n.Tag {
		if n.Kind != yaml.ScalarNode || p.anchored[n] {
			return false
		}
		start, ok := p.offset(&old)
		if !ok {
			return false
		}
		end, ok := p.scalarEnd(&old, start, flow, isKey)
		if !ok {
			return false
		}
		value := &Node{Kind: yaml.ScalarNode, Tag: n.Tag, Value: n.Value}
		if flow && value.ShortTag() == "!!str" {
			value.Style = yaml.DoubleQuotedStyle
		}
		text, err := Marshal(value)
		if err != nil {
			return false
		}
		text = bytes.TrimSuffix(text, []byte{'\n'})
		if bytes.ContainsAny(text, "\r\n") {
			return false
		}
		p.patches = append(p.patches, textPatch{start, end, text, false})
	}
	if n.Kind == yaml.MappingNode {
		if len(n.Content)%2 != 0 {
			return false
		}
		current := 0
		for i := 0; i < len(old.Content); i += 2 {
			if current < len(n.Content) && n.Content[current] == old.Content[i] && n.Content[current+1] == old.Content[i+1] {
				if !p.visit(n.Content[current], flow, true) || !p.visit(n.Content[current+1], flow, false) {
					return false
				}
				current += 2
			} else {
				if flow || p.anchored[n] || len(n.Content) == 0 || !p.removePair(&old, old.Content[i], old.Content[i+1]) {
					return false
				}
			}
		}
		return current == len(n.Content)
	}
	if len(old.Content) != len(n.Content) {
		return false
	}
	for i, child := range n.Content {
		if old.Content[i] != child || !p.visit(child, flow, false) {
			return false
		}
	}
	return true
}

func (p *editSplicer) scalarEnd(n *Node, start int, flow, isKey bool) (int, bool) {
	if n.Anchor != "" || n.Style&(yaml.TaggedStyle|yaml.LiteralStyle|yaml.FoldedStyle) != 0 || n.Line < 1 || n.Line > len(p.lines) {
		return 0, false
	}
	lineEnd := p.lines[n.Line-1].end
	if start >= lineEnd {
		return 0, false
	}
	text := p.source[start:lineEnd]
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		quote := byte('"')
		if n.Style&yaml.SingleQuotedStyle != 0 {
			quote = '\''
		}
		if text[0] != quote {
			return 0, false
		}
		end, ok := quotedScalarEnd(text)
		return start + end, ok
	}
	// yaml.v3 accepts a tab-only continuation after a plain scalar, but the
	// same continuation after a quoted replacement is invalid. Such whitespace
	// belongs to the scalar's lexical context and needs whole-document encoding.
	for i := n.Line; i < len(p.lines); i++ {
		line := p.lines[i]
		text := p.source[line.start:line.end]
		if len(bytes.Trim(text, " \t")) != 0 {
			break
		}
		if bytes.IndexByte(text, '\t') >= 0 {
			return 0, false
		}
	}
	end := len(text)
	for i, b := range text {
		if b == '#' && (i == 0 || text[i-1] == ' ' || text[i-1] == '\t') || flow && bytes.IndexByte([]byte(",]}"), b) >= 0 || isKey && b == ':' {
			end = i
			break
		}
	}
	token := bytes.TrimRight(text[:end], " \t")
	if len(token) == 0 || string(token) != n.Value {
		return 0, false
	}
	return start + len(token), true
}

func (p *editSplicer) removePair(parent, key, value *Node) bool {
	if parent.Style&yaml.FlowStyle != 0 || key.Kind != yaml.ScalarNode || key.Line < 1 || key.Line > len(p.lines) {
		return false
	}
	start, ok := p.offset(key)
	if !ok {
		return false
	}
	first := p.lines[key.Line-1]
	prefix := p.source[first.start:start]
	if len(bytes.Trim(prefix, " \t")) != 0 {
		return false
	} // includes first key after '- '.
	if !p.singleLineCollections(value) {
		return false
	}
	safe := true
	for _, root := range []*Node{key, value} {
		walkEditNodes(root, func(n *Node) {
			if p.anchored[n] || n.Anchor != "" || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 {
				safe = false
			}
			if n.Kind == yaml.ScalarNode && n.Style == 0 {
				offset, ok := p.offset(n)
				if !ok || n.Value == "" || !bytes.HasPrefix(p.source[offset:p.lines[n.Line-1].end], []byte(n.Value)) {
					safe = false
				}
			}
			if n.Kind == yaml.ScalarNode && n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
				offset, ok := p.offset(n)
				if !ok {
					safe = false
					return
				}
				if _, ok := p.scalarEnd(n, offset, false, false); !ok {
					safe = false
				}
			}
		})
	}
	if !safe {
		return false
	}
	last := key.Line
	indent := len(prefix)
	// A block sequence value may start its entries at the key's indentation.
	indentless := value.Kind == yaml.SequenceNode && value.Style&yaml.FlowStyle == 0 && value.Line > key.Line && value.Column-1 == indent
	for last < len(p.lines) {
		line := p.lines[last]
		text := p.source[line.start:line.end]
		trimmed := bytes.TrimSpace(text)
		if len(trimmed) != 0 && trimmed[0] != '#' {
			width := len(text) - len(bytes.TrimLeft(text, " \t"))
			if width < indent || width == indent && !(indentless && editSequenceEntry(trimmed)) {
				break
			}
		}
		last++
	}
	flowDepth := 0
	for i := key.Line - 1; i < last; i++ {
		line := p.lines[i]
		text := p.source[line.start:line.end]
		trimmed := bytes.TrimSpace(text)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			continue
		}
		var keep []byte
		if comment := editLineComment(text, &flowDepth); comment >= 0 {
			indentation := len(text) - len(bytes.TrimLeft(text, " \t"))
			keep = append(keep, text[:indentation]...)
			keep = append(keep, p.source[line.start+comment:line.next]...)
		}
		p.patches = append(p.patches, textPatch{line.start, line.next, keep, true})
	}
	return true
}

// editSequenceEntry reports whether a trimmed line starts a block sequence entry.
func editSequenceEntry(trimmed []byte) bool {
	return len(trimmed) > 0 && trimmed[0] == '-' && (len(trimmed) == 1 || trimmed[1] == ' ' || trimmed[1] == '\t')
}

// A flow collection can close at the enclosing map's indentation. Only splice
// it when the native parser proves its entire value fits on its starting line;
// indentation alone cannot locate a multiline flow collection's end.
func (p *editSplicer) singleLineCollections(n *Node) bool {
	if (n.Kind == yaml.MappingNode || n.Kind == yaml.SequenceNode) && n.Style&yaml.FlowStyle != 0 {
		start, ok := p.offset(n)
		if !ok {
			return false
		}
		parsed, err := Parse(p.source[start:p.lines[n.Line-1].end])
		return err == nil && Equal(parsed, n)
	}
	for _, child := range n.Content {
		if !p.singleLineCollections(child) {
			return false
		}
	}
	return true
}

// editLineComment identifies comments only on lines already known not to be
// inside a block/multiline scalar. It is not a YAML reader; Parse owns syntax.
func editLineComment(line []byte, flowDepth *int) int {
	var quote byte
	tokenStart := true
	afterFlow := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if quote != 0 {
			if quote == '"' && b == '\\' {
				i++
				continue
			}
			if b == quote {
				if quote == '\'' && i+1 < len(line) && line[i+1] == '\'' {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		if b == '#' && (afterFlow || i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return i
		}
		if b == ' ' || b == '\t' {
			continue
		}
		if tokenStart && (b == '"' || b == '\'') {
			quote = b
			tokenStart = false
			continue
		}
		afterFlow = false
		switch b {
		case '[', '{':
			if tokenStart || *flowDepth > 0 {
				*flowDepth++
				tokenStart = true
			} else {
				tokenStart = false
			}
		case ']', '}':
			afterFlow = *flowDepth > 0
			if *flowDepth > 0 {
				*flowDepth--
			}
			tokenStart = false
		case ',':
			tokenStart = *flowDepth > 0
		case ':':
			tokenStart = (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t')
		case '?', '-':
			tokenStart = tokenStart && (i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t')
		default:
			tokenStart = false
		}
	}
	return -1
}
