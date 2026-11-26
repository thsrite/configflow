package yamlx

import (
	"bytes"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Source indexes immutable YAML source bytes using the editor's native source
// positions. Nodes passed to it must come from Parse of the same unchanged text.
type Source struct{ positions editSplicer }

// NewSource indexes source lines once for repeated physical-span checks.
// The caller must keep src unchanged for the lifetime of the Source.
func NewSource(src []byte) *Source {
	lines := editSourceLines(src)
	columns := make([][]int, len(lines))
	for index, line := range lines {
		ascii := true
		for _, value := range src[line.start:line.end] {
			if value >= utf8.RuneSelf {
				ascii = false
				break
			}
		}
		if ascii {
			continue
		}
		start := line.start
		if index == 0 && bytes.HasPrefix(src, []byte("\ufeff")) {
			start += 3
		}
		for offset := start; offset < line.end; {
			columns[index] = append(columns[index], offset)
			_, size := utf8.DecodeRune(src[offset:line.end])
			offset += size
		}
		columns[index] = append(columns[index], line.end)
	}
	return &Source{positions: editSplicer{source: src, lines: lines, columns: columns}}
}

// IsSingleLineScalar reports whether a scalar's actual source token ends on
// its starting line. Folded values alone cannot establish physical single-line
// layout. Unknown positions, block scalars and anchors are conservatively false.
func (s *Source) IsSingleLineScalar(n *Node) bool {
	if s == nil || n == nil || n.Kind != yaml.ScalarNode || n.Anchor != "" || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		return false
	}
	p := &s.positions
	start, ok := p.offset(n)
	if !ok {
		return false
	}
	line := p.lines[n.Line-1]
	text := p.source[start:line.end]
	// A tagged node's column points at the tag property, before its value.
	if n.Style&yaml.TaggedStyle != 0 {
		if len(text) == 0 || text[0] != '!' {
			return false
		}
		end := 0
		if bytes.HasPrefix(text, []byte("!<")) {
			end = bytes.IndexByte(text, '>')
			if end < 0 {
				return false
			}
			end++
		} else {
			for end < len(text) && text[end] != ' ' && text[end] != '\t' {
				end++
			}
		}
		text = bytes.TrimLeft(text[end:], " \t")
		if len(text) == 0 {
			return false
		}
	}
	if n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0 {
		_, ok := quotedScalarEnd(text)
		return ok
	}
	// Plain scalar values are retained verbatim by yaml.v3 on one line. A
	// physically continued scalar has already folded additional source into Value.
	return bytes.HasPrefix(text, []byte(n.Value))
}

// quotedScalarEnd is shared with the editor's splice lexer. It scans only one
// physical source line, accounting for doubled single quotes and double-quoted
// escapes; no private YAML parser or decoded-value newline heuristic is used.
func quotedScalarEnd(text []byte) (int, bool) {
	if len(text) == 0 || text[0] != '\'' && text[0] != '"' {
		return 0, false
	}
	quote := text[0]
	for i := 1; i < len(text); i++ {
		if quote == '"' && text[i] == '\\' {
			i++
			continue
		}
		if text[i] == quote {
			if quote == '\'' && i+1 < len(text) && text[i+1] == '\'' {
				i++
				continue
			}
			return i + 1, true
		}
	}
	return 0, false
}
