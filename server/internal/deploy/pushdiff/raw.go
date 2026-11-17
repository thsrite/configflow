package pushdiff

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"configflow/internal/agents/diag"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

type markedLine struct {
	text    string
	display Text
}

func rawLines(data []byte) []markedLine {
	text := string(data)
	if text == "" {
		return nil
	}
	starts := []int{0}
	for i, c := range data {
		if c == '\n' {
			starts = append(starts, i+1)
		}
	}
	var spans [][2]int
	if doc, err := yamlx.Parse(data); err == nil {
		var visit func(*yaml.Node, []string, bool, int)
		visit = func(n *yaml.Node, p []string, secret bool, depth int) {
			if n == nil {
				return
			}
			if depth > MaxDepth {
				secret = true
			}
			start := 0
			if n.Line > 0 && n.Line <= len(starts) {
				line := starts[n.Line-1]
				start = line
				for column := 1; column < n.Column && start < len(text); column++ {
					_, size := utf8.DecodeRuneInString(text[start:])
					start += size
				}
			}
			// yaml.v3 positions include anchor/tag properties before a scalar.
			if n.Kind == yaml.ScalarNode {
				for start < len(text) && (text[start] == '&' || text[start] == '!') {
					for start < len(text) && text[start] != ' ' && text[start] != '\n' {
						start++
					}
					for start < len(text) && text[start] == ' ' {
						start++
					}
				}
			}
			if n.Kind == yaml.AliasNode {
				if secret {
					end := start + 1
					for end < len(text) && !strings.ContainsRune(" ,]}\n", rune(text[end])) {
						end++
					}
					spans = append(spans, [2]int{start, end})
				}
				visit(n.Alias, p, secret, depth+1)
				return
			}
			if n.Kind == yaml.MappingNode {
				for i := 0; i < len(n.Content); i += 2 {
					key, value := n.Content[i], n.Content[i+1]
					visit(key, p, secret, depth+1)
					visit(value, appendPath(p, scalar(key)), secret || secretField(p, scalar(key)), depth+1)
				}
				return
			}
			if n.Kind == yaml.SequenceNode {
				for _, v := range n.Content {
					visit(v, p, secret, depth+1)
				}
				return
			}
			end := scalarEnd(text, start, n, starts)
			if secret {
				spans = append(spans, [2]int{start, end})
				return
			}
			ranges := diag.CredentialSpans(n.Value)
			if len(ranges) == 0 {
				return
			}
			// Plain/single-quoted strings are emitted without wrapping. For escaped
			// double quotes, hide the whole token rather than risk a partial leak.
			token := text[start:end]
			if n.Style&yaml.DoubleQuotedStyle != 0 || n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
				spans = append(spans, [2]int{start, end})
				return
			}
			offset := start
			if strings.HasPrefix(token, "'") {
				offset++
				for _, r := range ranges {
					a := len(strings.ReplaceAll(n.Value[:r[0]], "'", "''"))
					b := len(strings.ReplaceAll(n.Value[:r[1]], "'", "''"))
					spans = append(spans, [2]int{offset + a, offset + b})
				}
				return
			}
			if strings.HasPrefix(token, n.Value) {
				for _, r := range ranges {
					spans = append(spans, [2]int{offset + r[0], offset + r[1]})
				}
			} else {
				spans = append(spans, [2]int{start, end})
			}
		}
		visit(doc, nil, false, 0)
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i][0] < spans[j][0] })
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	out := make([]markedLine, len(lines))
	pending := 0
	for i, line := range lines {
		start := starts[i]
		end := start + len(line)
		var t Text
		pos := start
		for pending < len(spans) && spans[pending][1] <= start {
			pending++
		}
		for j := pending; j < len(spans) && spans[j][0] < end; j++ {
			a, b := max(pos, spans[j][0]), min(end, spans[j][1])
			if b <= a {
				continue
			}
			t.add(text[pos:a], false)
			t.add(text[a:b], true)
			pos = b
		}
		t.add(text[pos:end], false)
		out[i] = markedLine{line, t}
	}
	return out
}
func scalarEnd(text string, start int, n *yaml.Node, starts []int) int {
	if start >= len(text) {
		return start
	}
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 {
		end := len(text)
		indent := -1
		for i := n.Line; i < len(starts)-1; i++ {
			line := text[starts[i]:starts[i+1]]
			if strings.TrimSpace(line) == "" {
				continue
			}
			spaces := len(line) - len(strings.TrimLeft(line, " "))
			if indent < 0 {
				indent = spaces
			}
			if spaces < indent {
				end = starts[i]
				break
			}
		}
		for end > start && (text[end-1] == '\n' || text[end-1] == '\r') {
			end--
		}
		return end
	}
	if text[start] == '\'' || text[start] == '"' {
		quote := text[start]
		for i := start + 1; i < len(text); i++ {
			if quote == '"' && text[i] == '\\' {
				i++
				continue
			}
			if text[i] == quote {
				if quote == '\'' && i+1 < len(text) && text[i+1] == '\'' {
					i++
					continue
				}
				return i + 1
			}
		}
		return len(text)
	}
	// Canonical plain scalars are emitted verbatim on one line.
	return min(len(text), start+len(n.Value))
}

type lineOp struct {
	kind     byte
	old, new int
}
type lineMatch struct{ old, new int }

// matchLines uses the patience algorithm: equal ends, unique common anchors,
// and their longest increasing subsequence. Unanchored regions are replacements.
// Work and memory stay bounded even for repeated or entirely different lines.
func matchLines(a, b []markedLine) []lineMatch {
	matches := []lineMatch{}
	var region func(int, int, int, int, int)
	region = func(lo, hi, from, to, depth int) {
		for lo < hi && from < to && a[lo].text == b[from].text {
			matches = append(matches, lineMatch{lo, from})
			lo++
			from++
		}
		tail := 0
		for lo < hi-tail && from < to-tail && a[hi-tail-1].text == b[to-tail-1].text {
			tail++
		}
		h, t := hi-tail, to-tail
		if lo < h && from < t && depth < 64 {
			type occurrence struct{ index, count int }
			old, new := map[string]occurrence{}, map[string]occurrence{}
			for i := lo; i < h; i++ {
				v := old[a[i].text]
				v.index = i
				v.count++
				old[a[i].text] = v
			}
			for i := from; i < t; i++ {
				v := new[b[i].text]
				v.index = i
				v.count++
				new[b[i].text] = v
			}
			anchors := []lineMatch{}
			for i := lo; i < h; i++ {
				v, w := old[a[i].text], new[a[i].text]
				if v.count == 1 && w.count == 1 {
					anchors = append(anchors, lineMatch{i, w.index})
				}
			}
			tails, indices, previous := []int{}, []int{}, make([]int, len(anchors))
			for i, v := range anchors {
				at := sort.SearchInts(tails, v.new)
				previous[i] = -1
				if at > 0 {
					previous[i] = indices[at-1]
				}
				if at == len(tails) {
					tails = append(tails, v.new)
					indices = append(indices, i)
				} else {
					tails[at] = v.new
					indices[at] = i
				}
			}
			if len(indices) > 0 {
				chain := []lineMatch{}
				for i := indices[len(indices)-1]; i >= 0; i = previous[i] {
					chain = append(chain, anchors[i])
				}
				slices.Reverse(chain)
				for _, v := range chain {
					region(lo, v.old, from, v.new, depth+1)
					matches = append(matches, v)
					lo = v.old + 1
					from = v.new + 1
				}
				region(lo, h, from, t, depth+1)
			}
		}
		for i := 0; i < tail; i++ {
			matches = append(matches, lineMatch{h + i, t + i})
		}
	}
	region(0, len(a), 0, len(b), 0)
	return matches
}
func operations(a, b []markedLine) []lineOp {
	out := make([]lineOp, 0, len(a)+len(b))
	old, new := 0, 0
	for _, m := range append(matchLines(a, b), lineMatch{len(a), len(b)}) {
		for old < m.old {
			out = append(out, lineOp{'-', old, new})
			old++
		}
		for new < m.new {
			out = append(out, lineOp{'+', old, new})
			new++
		}
		if old < len(a) && new < len(b) {
			out = append(out, lineOp{' ', old, new})
			old++
			new++
		}
	}
	return out
}
func unifiedRange(start, count int) string {
	if count == 1 {
		return fmt.Sprint(start + 1)
	}
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}
func rawDiff(old, new []byte, oldName, newName string) RawDiff {
	a, b := rawLines(old), rawLines(new)
	if len(a)+len(b) > MaxRawInputLines {
		return RawDiff{Reason: "too_large", TotalLines: len(a) + len(b)}
	}
	result := RawDiff{Lines: []Text{}}
	ops := operations(a, b)
	type interval struct{ start, end int }
	groups := []interval{}
	for i, op := range ops {
		if op.kind == ' ' {
			continue
		}
		start, end := max(0, i-3), min(len(ops), i+4)
		if len(groups) > 0 && start <= groups[len(groups)-1].end {
			groups[len(groups)-1].end = end
		} else {
			groups = append(groups, interval{start, end})
		}
	}
	total, size := 0, 0
	full := false
	add := func(t Text) {
		total++
		if full {
			return
		}
		s, _ := t.Unpack()
		if len(result.Lines) >= MaxRawLines || size+len(s)+1 > MaxRawBytes {
			full = true
			return
		}
		size += len(s) + 1
		result.Lines = append(result.Lines, t)
	}
	if len(groups) > 0 {
		add(markedString("--- a/"+oldName, false, nil))
		add(markedString("+++ b/"+newName, false, nil))
	}
	for _, g := range groups {
		oldCount, newCount := 0, 0
		for _, op := range ops[g.start:g.end] {
			if op.kind != '+' {
				oldCount++
			}
			if op.kind != '-' {
				newCount++
			}
		}
		first := ops[g.start]
		add(plain(fmt.Sprintf("@@ -%s +%s @@", unifiedRange(first.old, oldCount), unifiedRange(first.new, newCount))))
		for _, op := range ops[g.start:g.end] {
			var line markedLine
			if op.kind == '+' {
				line = b[op.new]
			} else {
				line = a[op.old]
			}
			t := plain(string(op.kind))
			t.append(line.display)
			add(t)
		}
	}
	result.Truncated = total > len(result.Lines)
	result.OmittedLines = total - len(result.Lines)
	return result
}
