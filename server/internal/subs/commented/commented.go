// Package commented is the port of backend/utils/commented_mihomo.py: it
// edits the fixed `proxy` targets of Mihomo listeners in a YAML text while
// keeping comments, producing the bytes ruamel.yaml's round-trip dumper
// writes. The supported YAML subset and the documented gaps are listed in
// the package tests; anything outside it returns ErrUnsupported (Python
// would have produced output), and the caller keeps the original text.
package commented

import (
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

func splitSourceLines(source string) []string {
	lines := strings.Split(source, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// pySplitLines lengths: the longest line of source.splitlines().
func longestLine(source string) int {
	longest, cur := 0, 0
	rs := []rune(source)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\n', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		case '\r':
			if i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
		default:
			cur++
			continue
		}
		if cur > longest {
			longest = cur
		}
		cur = 0
	}
	if cur > longest {
		longest = cur
	}
	return longest
}

func copyNode(n *node) *node {
	if n.kind == kAlias {
		return copyNode(n.target)
	}
	c := *n
	c.anchor = ""
	c.entries = nil
	c.items = nil
	for _, e := range n.entries {
		c.entries = append(c.entries, &entry{key: copyNode(e.key), value: copyNode(e.value), rec: copyRec(e.rec)})
	}
	for _, it := range n.items {
		c.items = append(c.items, &item{value: copyNode(it.value), rec: copyRec(it.rec), compactFlag: it.compactFlag})
	}
	return &c
}

func copyRec(r rec) rec {
	c := r
	if r.eol != nil {
		e := *r.eol
		c.eol = &e
	}
	c.gap = append([]string(nil), r.gap...)
	c.trail = append([]string(nil), r.trail...)
	return c
}

func plainKey(n *node, name string) bool {
	return n.kind == kScalar && (n.style == sPlain || n.style == sSingle || n.style == sDouble) && n.raw == name
}

func (n *node) find(name string) (*entry, int) {
	for i, e := range n.entries {
		if plainKey(e.key, name) {
			return e, i
		}
	}
	return nil, -1
}

func countRefs(n *node, refs map[*node]int, visited map[*node]bool) {
	if n == nil {
		return
	}
	if n.kind == kAlias {
		refs[n.target]++
		return
	}
	if n.anchor != "" {
		refs[n]++
	}
	if visited[n] {
		return
	}
	visited[n] = true
	for _, e := range n.entries {
		countRefs(e.value, refs, visited)
	}
	for _, it := range n.items {
		countRefs(it.value, refs, visited)
	}
}

// keyIdentity is the constructed value of a mapping key, as far as duplicates are concerned.
func keyIdentity(k *node) string {
	if k.style == sPlain && !isStringPlain(k.raw) {
		t, null, _ := plainOut(k.raw)
		if null {
			return "n:"
		}
		return "p:" + t
	}
	return "s:" + k.raw
}

func rejectMerge(n *node, seen map[*node]bool) error {
	if n == nil || seen[n] {
		return nil
	}
	seen[n] = true
	keys := map[string]bool{}
	for _, e := range n.entries {
		id := keyIdentity(e.key)
		if keys[id] {
			return unsupported("duplicate mapping key")
		}
		keys[id] = true
	}
	for _, e := range n.entries {
		if isMergeKey(e.key) && !(e.value.kind == kAlias && e.value.target.kind == kMap) {
			return unsupported("merge key other than one alias of a mapping")
		}
		if err := rejectMerge(e.value, seen); err != nil {
			return err
		}
	}
	for _, it := range n.items {
		if err := rejectMerge(it.value, seen); err != nil {
			return err
		}
	}
	return nil
}

func isMergeKey(k *node) bool { return k.kind == kScalar && k.style == sPlain && k.raw == "<<" }

// materialize is the merge part of _detach: CommentedMap(result.items())
// lists the own keys in order, then the merged keys the mapping does not
// override. The merge entry's comments become comment lines before the
// next entry (returned as lead when the merge entry was the first one).
func materialize(m *node) (lead []string, err error) {
	at := -1
	for i, e := range m.entries {
		if isMergeKey(e.key) {
			at = i
		}
	}
	if at < 0 {
		return nil, nil
	}
	merge := m.entries[at]
	source := merge.value
	if source.kind != kMap {
		return nil, unsupported("merge of something other than a mapping")
	}
	own := map[string]bool{}
	var entries []*entry
	for i, e := range m.entries {
		if i != at {
			own[keyIdentity(e.key)] = true
			entries = append(entries, e)
		}
	}
	var moved []string
	if merge.rec.eol != nil {
		moved = append(moved, spaces(merge.rec.eol.col)+merge.rec.eol.text)
	}
	moved = append(moved, merge.rec.gap...)
	moved = append(moved, merge.rec.trail...)
	for _, e := range source.entries {
		if !own[keyIdentity(e.key)] && !isMergeKey(e.key) {
			// CommentedMap(result.items()) copies only the listener's own comments
			entries = append(entries, &entry{key: e.key, value: e.value})
		}
	}
	if len(moved) > 0 {
		// before the next own key; without one, after the whole mapping
		switch {
		case at < len(m.entries)-1 && at > 0:
			prev := m.entries[at-1]
			prev.rec.gap = append(prev.rec.gap, moved...)
		case at < len(m.entries)-1:
			lead = moved
		case len(entries) > 0:
			m.endGap = append(m.endGap, moved...)
		default:
			return nil, unsupported("comment on a merge-only listener")
		}
	}
	m.entries = entries
	return lead, nil
}

// attachComments moves comments to where ruamel's composer keeps them:
// the comment after an alias belongs to no node and is written on the next
// line, at its column; the comment after a flow collection's closing
// bracket also belongs to that collection (post).
func attachComments(n *node, seen map[*node]bool) {
	if n == nil || n.kind == kAlias || seen[n] {
		return
	}
	seen[n] = true
	fix := func(v *node, r *rec, inSeq bool) {
		if r.eol == nil {
			if v.kind == kAlias {
				r.trail, r.gap = r.gap, nil
			}
			return
		}
		switch {
		case !inSeq && v.anchor != "" && !v.flow && (v.kind == kMap || v.kind == kSeq):
			c := *r.eol
			v.head = &c
			r.eol = nil
		case v.kind == kAlias:
			r.trail = append([]string{spaces(r.eol.col) + r.eol.text}, r.gap...)
			r.eol, r.gap = nil, nil
		case !inSeq && (v.flow && (v.kind == kMap || v.kind == kSeq) || v.kind == kScalar && v.anchor != ""):
			// a sequence item's comment stays with the sequence (ca.items)
			c := *r.eol
			v.post = &c
		}
	}
	for _, e := range n.entries {
		fix(e.value, &e.rec, false)
		attachComments(e.value, seen)
	}
	for _, it := range n.items {
		fix(it.value, &it.rec, true)
		attachComments(it.value, seen)
	}
}

func hasComment(r *rec) bool { return r != nil && (r.eol != nil || len(r.gap) > 0) }

// subtreeCommented reports a comment anywhere inside n (its own entries' and
// items' lines).
func subtreeCommented(n *node, seen map[*node]bool) bool {
	if n == nil || seen[n] || n.kind == kAlias {
		return false
	}
	seen[n] = true
	if len(n.flowComments) > 0 {
		return true
	}
	for _, e := range n.entries {
		if hasComment(&e.rec) || subtreeCommented(e.value, seen) {
			return true
		}
	}
	for _, it := range n.items {
		if hasComment(&it.rec) || subtreeCommented(it.value, seen) {
			return true
		}
	}
	return false
}

var removedComment = func(line string) string {
	t := strings.TrimLeft(line, " \t\n\r\f\v")
	if strings.HasPrefix(t, "#") {
		t = t[1:]
		t = strings.TrimPrefix(t, " ")
		return t
	}
	return line
}

// EditListenerTargets is edit_listener_targets: indexes are the listeners
// whose proxy is replaced by newName (nil removes it); expected is the
// document the result must load to.
func EditListenerTargets(source string, indexes []int, newName *string, expected *jsonx.Object) (string, error) {
	if strings.HasPrefix(source, "\ufeff") {
		return "", unsupported("byte order mark")
	}
	lineBreak := "\n"
	if strings.Contains(source, "\r\n") {
		lineBreak = "\r\n"
	}
	width := longestLine(source)
	if width < 80 {
		width = 80
	}
	doc, err := parseDocument(splitSourceLines(source))
	if err != nil {
		return "", err
	}
	if err := rejectMerge(doc.root, map[*node]bool{}); err != nil {
		return "", err
	}
	attachComments(doc.root, map[*node]bool{})
	if doc.root.kind != kMap {
		return "", unsupported("root is not a mapping")
	}
	le, _ := doc.root.find("listeners")
	if le == nil {
		return "", unsupported("no listeners")
	}
	listeners := le.value
	if listeners.kind == kAlias {
		listeners = listeners.target
	}
	if listeners.kind != kSeq {
		return "", unsupported("listeners is not a sequence")
	}
	// document['listeners'] = CommentedSeq(original): a copy, so a shared
	// (anchored) sequence keeps its other users unchanged.
	shallow := &node{kind: kSeq, flow: listeners.flow}
	for _, it := range listeners.items {
		cp := *it
		shallow.items = append(shallow.items, &cp)
	}
	le.value = shallow
	listeners = shallow
	preLines := map[*item][]string{}
	for _, index := range indexes {
		if index < 0 || index >= len(listeners.items) {
			return "", unsupported("listener index")
		}
		it := listeners.items[index]
		replacement := copyNode(it.value)
		if replacement.kind != kMap {
			return "", unsupported("listener is not a mapping")
		}
		lead, err := materialize(replacement)
		if err != nil {
			return "", err
		}
		it.dashBreak = lead
		if newName == nil {
			if en, at := replacement.find("proxy"); en != nil {
				// The removed key's comment token: its end-of-line comment (or the
				// line break when there is none) and the lines after it.
				var comments []string
				if en.rec.eol != nil {
					comments = append(comments, en.rec.eol.text)
				} else if len(en.rec.gap) > 0 {
					comments = append(comments, "")
				}
				comments = append(comments, en.rec.gap...)
				replacement.entries = append(replacement.entries[:at], replacement.entries[at+1:]...)
				if len(comments) > 0 && replacement.head != nil {
					// ruamel's emitter fails (AssertionError) on a listener that
					// keeps its own head comment and gets the removed key's
					// comment set before it
					return "", unsupported("removed key comment on a listener with its own comment")
				}
				if len(comments) > 0 {
					var texts []string
					for _, c := range comments {
						texts = append(texts, removedComment(c))
					}
					if n := len(texts); n > 1 && texts[n-1] == "" {
						texts = texts[:n-1] // the token's own final line break
					}
					joined := strings.Join(texts, "\n")
					parts := strings.Split(joined, "\n")
					if joined == "\n" || joined == "" {
						parts = []string{""} // yaml_set_comment_before_after_key: before == '\n' is one blank line
					}
					for _, com := range parts {
						if com == "" {
							preLines[it] = append(preLines[it], "")
						} else {
							preLines[it] = append(preLines[it], "# "+com)
						}
					}
				}
			}
		} else {
			value := &node{kind: kScalar, style: sDouble, raw: *newName}
			if en, _ := replacement.find("proxy"); en != nil {
				en.value = value
			} else {
				replacement.entries = append(replacement.entries, &entry{
					key:   &node{kind: kScalar, style: sPlain, raw: "proxy"},
					value: value,
				})
			}
		}
		it.value = replacement
	}
	if lineBreak == "\r\n" && (doc.hasInnerGaps || doc.crlfRisk) {
		return "", unsupported("CRLF document with comment or blank lines between entries")
	}
	e := &emitter{width: width, seen: map[*node]bool{}, refs: map[*node]int{}, genNames: map[*node]string{}, preLines: preLines, yaml12: doc.explicitStart}
	countRefs(doc.root, e.refs, map[*node]bool{})
	if doc.explicitStart {
		// comments before `---` are dropped by the dump; the missing-comment
		// step below puts them back on top (stripped, without blank lines)
		e.out = append(e.out, "---")
	}
	head := doc.head
	if !doc.explicitStart {
		for len(head) > 0 && head[0] == "" {
			head = head[1:] // leading blank lines are not kept
		}
	}
	e.gap(head)
	e.emitMap(doc.root, 0, "")
	if e.err != nil {
		return "", e.err
	}
	if doc.explicitEnd || e.openEnded {
		e.out = append(e.out, "...")
	}
	result := strings.Join(e.out, lineBreak) + lineBreak
	// Comments the round trip cannot keep (inside flow collections) are put on top.
	counts := map[string]int{}
	for _, c := range e.emitted {
		counts[c]++
	}
	var missing []string
	seen := map[string]bool{}
	for _, c := range doc.comments {
		if seen[c] {
			continue
		}
		seen[c] = true
		total := 0
		for _, d := range doc.comments {
			if d == c {
				total++
			}
		}
		for i := 0; i < total-counts[c]; i++ {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		result = strings.Join(missing, lineBreak) + lineBreak + result
	}
	got, err := yamlx.SafeLoad(result)
	if err != nil || !same(got, expected) {
		return "", unsupported("the edited text does not load to the expected document")
	}
	_ = utf8.RuneError
	return result, nil
}

// same is _same: equality that also compares types (1 != 1.0, True != 1).
func same(a, b any) bool {
	switch x := a.(type) {
	case *jsonx.Object:
		y, ok := b.(*jsonx.Object)
		if !ok || x.Len() != y.Len() {
			return false
		}
		for k, v := range x.All() {
			w, has := y.Get(k)
			if !has || !same(v, w) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !same(x[i], y[i]) {
				return false
			}
		}
		return true
	case jsonx.Number:
		y, ok := b.(jsonx.Number)
		return ok && x.IsInt() == y.IsInt() && jsonx.Equal(x, y)
	case yamlx.Timestamp:
		// date or datetime (type(left) is type(right)), then ==
		y, ok := b.(yamlx.Timestamp)
		if !ok || x.HasTime != y.HasTime {
			return false
		}
		if !x.HasTime {
			return x == y
		}
		if x.HasZone != y.HasZone {
			return false // naive == aware is False
		}
		return x.Time().Equal(y.Time())
	default:
		if _, isNum := b.(jsonx.Number); isNum {
			return false
		}
		return jsonx.Equal(a, b)
	}
}
