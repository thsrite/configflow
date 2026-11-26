package yamlx

import (
	"bytes"
	"strings"

	"gopkg.in/yaml.v3"
)

// sourceEditComments also retains standalone notes the scanner did not attach
// to a Node. Scalar content that happens to start with '#' is not a comment.
func sourceEditComments(src []byte, doc *Node, known []string) []string {
	p := editSplicer{source: src, lines: editSourceLines(src)}
	protected := make(map[int]bool)
	var walk func(*Node, *Node)
	walk = func(n, key *Node) {
		if n == nil {
			return
		}
		if n.Kind == yaml.ScalarNode && n.Line > 0 && n.Line <= len(p.lines) {
			switch {
			case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
				header := p.lines[n.Line-1]
				text := src[header.start:header.end]
				base := len(text) - len(bytes.TrimLeft(text, " \t"))
				if key != nil {
					base = key.Column - 1
				}
				indentation := 0
				// A numeric block indentation indicator overrides first-content detection.
				clean := text
				depth := 0
				if i := editLineComment(clean, &depth); i >= 0 {
					clean = clean[:i]
				}
				marker := bytes.LastIndexAny(clean, "|>")
				if marker >= 0 {
					for _, b := range clean[marker+1:] {
						if b >= '1' && b <= '9' {
							indentation = base + int(b-'0')
							break
						}
					}
				}
				for i := n.Line; i < len(p.lines); i++ {
					line := p.lines[i]
					raw := src[line.start:line.end]
					blank := len(bytes.TrimSpace(raw)) == 0
					width := len(raw) - len(bytes.TrimLeft(raw, " \t"))
					if !blank && indentation == 0 {
						if width <= base {
							break
						}
						indentation = width
					}
					if !blank && width < indentation {
						break
					}
					protected[i] = true
				}
			case n.Style&(yaml.SingleQuotedStyle|yaml.DoubleQuotedStyle) != 0:
				offset, ok := p.offset(n)
				if !ok {
					break
				}
				quote := byte('"')
				if n.Style&yaml.SingleQuotedStyle != 0 {
					quote = '\''
				}
				start := bytes.IndexByte(src[offset:p.lines[n.Line-1].end], quote)
				if start < 0 {
					break
				}
				start += offset
				end := start + 1
				for end < len(src) {
					if quote == '"' && src[end] == '\\' {
						end += 2
						continue
					}
					if src[end] == quote {
						if quote == '\'' && end+1 < len(src) && src[end+1] == quote {
							end += 2
							continue
						}
						break
					}
					end++
				}
				for i := n.Line; i < len(p.lines) && p.lines[i].start <= end; i++ {
					protected[i] = true
				}
			}
		}
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(n.Content[i], nil)
				walk(n.Content[i+1], n.Content[i])
			}
		} else {
			for _, child := range n.Content {
				walk(child, nil)
			}
		}
	}
	walk(doc, nil)
	counts := map[string]int{}
	for i, line := range p.lines {
		if protected[i] {
			continue
		}
		text := strings.TrimSpace(string(src[line.start:line.end]))
		text = strings.TrimPrefix(text, "\ufeff")
		if strings.HasPrefix(text, "#") {
			counts[text]++
		}
	}
	for _, line := range known {
		if counts[line] > 0 {
			counts[line]--
		}
	}
	out := append([]string(nil), known...)
	// Source order is kept for otherwise unattached notes.
	for i, line := range p.lines {
		if protected[i] {
			continue
		}
		text := strings.TrimPrefix(strings.TrimSpace(string(src[line.start:line.end])), "\ufeff")
		if counts[text] > 0 {
			out = append(out, text)
			counts[text]--
		}
	}
	return out
}
