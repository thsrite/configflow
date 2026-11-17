package pushdiff

import (
	"fmt"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/agents/diag"
	"configflow/internal/redact"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func dataKeyed(p []string) bool {
	switch strings.Join(p, "\x00") {
	case "hosts", "dns\x00hosts", "dns\x00nameserver-policy", "dns\x00proxy-server-nameserver-policy", "rule-providers", "proxy-providers", "sub-rules":
		return true
	}
	return false
}
func secretField(p []string, k string) bool { return !dataKeyed(p) && diag.IsCredentialKey(k) }
func resolve(n *yaml.Node) *yaml.Node       { return yamlx.Resolve(n) }
func isMap(n *yaml.Node) bool               { n = resolve(n); return n != nil && n.Kind == yaml.MappingNode }
func isList(n *yaml.Node) bool              { n = resolve(n); return n != nil && n.Kind == yaml.SequenceNode }
func elements(n *yaml.Node) []*yaml.Node {
	if isList(n) {
		return resolve(n).Content
	}
	return nil
}

type pair struct {
	key   string
	node  *yaml.Node
	value *yaml.Node
}

func pairs(n *yaml.Node) []pair {
	out := []pair{}
	if !isMap(n) {
		return out
	}
	n = resolve(n)
	for k, v := range yamlx.AllPairs(n) {
		out = append(out, pair{scalar(k), k, v})
	}
	return out
}
func keyID(p pair) string { return resolve(p.node).ShortTag() + "\x00" + p.key }
func scalar(n *yaml.Node) string {
	n = resolve(n)
	if n == nil {
		return "null"
	}
	switch n.ShortTag() {
	case "!!null":
		return "null"
	case "!!bool":
		var v bool
		if yamlx.Decode(n, &v) == nil {
			return strconv.FormatBool(v)
		}
	case "!!int", "!!float":
		if b, err := yamlx.ToJSON(n); err == nil {
			return strings.Trim(string(b), "\"")
		}
	}
	return n.Value
}
func show(n *yaml.Node, p []string, secret, sorted, clip bool) Text {
	var out Text
	renderValue(n, p, &out, secret, sorted, false, 0)
	if clip {
		return out.clipped(MaxValueChars)
	}
	return out
}
func renderValue(n *yaml.Node, p []string, out *Text, secret, sorted, jsonMode bool, depth int) {
	n = resolve(n)
	if depth > MaxDepth {
		out.add(`"…"`, secret)
		return
	}
	if isMap(n) {
		ps := pairs(n)
		if sorted && len(ps) > 0 {
			tag := resolve(ps[0].node).ShortTag()
			sameType := true
			for _, pair := range ps {
				if resolve(pair.node).ShortTag() != tag {
					sameType = false
					break
				}
			}
			if sameType {
				sort.SliceStable(ps, func(i, j int) bool {
					if tag == "!!int" || tag == "!!float" {
						var a, b float64
						if yamlx.Decode(ps[i].node, &a) == nil && yamlx.Decode(ps[j].node, &b) == nil {
							return a < b
						}
					}
					return ps[i].key < ps[j].key
				})
			}
		}
		out.add("{", secret)
		for i, pair := range ps {
			if i > 0 {
				out.add(", ", secret)
			}
			out.add(`"`, secret)
			out.append(markedString(pair.key, secret, escapeJSON))
			out.add(`": `, secret)
			renderValue(pair.value, appendPath(p, pair.key), out, secret || secretField(p, pair.key), sorted, true, depth+1)
		}
		out.add("}", secret)
	} else if isList(n) {
		out.add("[", secret)
		for i, v := range n.Content {
			if i > 0 {
				out.add(", ", secret)
			}
			renderValue(v, p, out, secret, sorted, true, depth+1)
		}
		out.add("]", secret)

	} else {
		stringValue := n != nil
		if n != nil {
			switch n.ShortTag() {
			case "!!null", "!!bool", "!!int", "!!float":
				stringValue = false
			}
		}
		if !stringValue {
			out.add(scalar(n), secret)
			return
		}
		if jsonMode {
			out.add(`"`, secret)
			out.append(markedString(n.Value, secret, escapeJSON))
			out.add(`"`, secret)
		} else {
			out.append(markedString(n.Value, secret, nil))
		}
	}
}
func appendPath(p []string, k string) []string {
	out := make([]string, len(p)+1)
	copy(out, p)
	out[len(p)] = k
	return out
}

type rowBudget struct {
	rows           []Row
	omitted, limit int
}

func newRows(limit int) *rowBudget { return &rowBudget{rows: []Row{}, limit: limit} }
func (b *rowBudget) row(p, full []string, change string, old, new *yaml.Node, sensitive, nested bool, list *ListDiff) {
	if len(b.rows) >= b.limit {
		b.omitted++
		return
	}
	safePath := make([]string, len(p))
	for i, key := range p {
		safePath[i] = metadataKey(key)
	}
	r := Row{Key: strings.Join(safePath, "."), Change: change, Sensitive: sensitive, List: list}
	if !nested {
		r.Path = ptr(safePath)
		if len(p) == 0 {
			r.Path = ptr([]string{})
		}
	}
	if list == nil {
		if old != nil {
			r.Old = ptr(show(old, full, sensitive, false, true))
		}
		if new != nil {
			r.New = ptr(show(new, full, sensitive, false, true))
		}
	} else if nested {
		list.brief = true
		list.AddedItems = []Text{}
		list.RemovedItems = []Text{}
		for _, e := range list.Entries {
			if e.Change == "added" && len(list.AddedItems) < MaxFieldItems {
				list.AddedItems = append(list.AddedItems, e.Text)
			}
			if e.Change == "removed" && len(list.RemovedItems) < MaxFieldItems {
				list.RemovedItems = append(list.RemovedItems, e.Text)
			}
		}
	}
	b.rows = append(b.rows, r)
}
func walk(old, new *yaml.Node, p, base []string, b *rowBudget, nested bool) {
	full := p
	if len(base) > 0 {
		full = append(slices.Clone(base), p...)
	}
	change := "modified"
	if old == nil {
		change = "added"
	} else if new == nil {
		change = "removed"
	}
	if old == nil && new == nil {
		return
	}
	sensitive := len(full) > 0 && secretField(full[:len(full)-1], full[len(full)-1])
	if sensitive {
		if change != "modified" || !yamlx.Equal(old, new) {
			b.row(p, full, change, old, new, sensitive, nested, nil)
		}
		return
	}
	if change != "modified" && len(full) > 0 && dataKeyed(full[:len(full)-1]) {
		b.row(p, full, change, old, new, false, nested, nil)
		return
	}
	nonnil := new
	if new == nil {
		nonnil = old
	}
	if isMap(old) && isMap(new) || change != "modified" && isMap(nonnil) && len(pairs(nonnil)) > 0 {
		olds, news := pairs(old), pairs(new)
		byOld := map[string]pair{}
		byNew := map[string]pair{}
		for _, q := range olds {
			byOld[keyID(q)] = q
		}
		for _, q := range news {
			byNew[keyID(q)] = q
			walk(byOld[keyID(q)].value, q.value, append(p, q.key), base, b, nested)
		}
		for _, q := range olds {
			if _, ok := byNew[keyID(q)]; !ok {
				walk(q.value, nil, append(p, q.key), base, b, nested)
			}
		}
		return
	}
	if isList(old) && isList(new) || change != "modified" && isList(nonnil) {
		if change == "modified" && yamlx.Equal(old, new) {
			return
		}
		b.row(p, full, change, nil, nil, false, nested, listDiff(elements(old), elements(new), full))
		return
	}
	if change != "modified" || !yamlx.Equal(old, new) {
		b.row(p, full, change, old, new, false, nested, nil)
	}
}

func identity(items []*yaml.Node) string {
	if len(items) == 0 {
		return ""
	}
	for _, key := range []string{"name", "tag"} {
		seen := map[string]bool{}
		valid := true
		for _, item := range items {
			v := resolve(yamlx.Get(item, key))
			if v == nil || v.ShortTag() != "!!str" || v.Value == "" || seen[v.Value] {
				valid = false
				break
			}
			seen[v.Value] = true
		}
		if valid {
			return key
		}
	}
	return ""
}
func valueID(n *yaml.Node) string {
	// Canonical typed identities are JSON display values with sorted keys.
	t := show(n, nil, false, true, false)
	s, _ := t.Unpack()
	n = resolve(n)
	if n != nil && n.ShortTag() == "!!str" {
		return "s" + s
	}
	return "j" + s
}
func reordered(a, b []string) bool {
	aset, bset := map[string]bool{}, map[string]bool{}
	for _, s := range a {
		aset[s] = true
	}
	for _, s := range b {
		bset[s] = true
	}
	left, right := []string{}, []string{}
	for _, s := range a {
		if bset[s] {
			left = append(left, s)
		}
	}
	for _, s := range b {
		if aset[s] {
			right = append(right, s)
		}
	}
	return !slices.Equal(left, right)
}
func listDiff(old, new []*yaml.Node, p []string) *ListDiff {
	l := &ListDiff{Before: len(old), After: len(new), Entries: []Entry{}}
	key := identity(old)
	if len(old) == 0 {
		key = identity(new)
	}
	if key != "" && (len(new) == 0 || identity(new) == key) {
		l.Identity = key
		oldBy, newBy := map[string]*yaml.Node{}, map[string]*yaml.Node{}
		a, b := []string{}, []string{}
		for _, item := range old {
			s := resolve(yamlx.Get(item, key)).Value
			oldBy[s] = item
			a = append(a, s)
		}
		for _, item := range new {
			s := resolve(yamlx.Get(item, key)).Value
			newBy[s] = item
			b = append(b, s)
		}
		for _, change := range []string{"added", "removed", "modified"} {
			names := b
			if change == "removed" {
				names = a
			}
			for _, name := range names {
				o, n := oldBy[name], newBy[name]
				match := change == "added" && o == nil || change == "removed" && n == nil || change == "modified" && o != nil && n != nil && !yamlx.Equal(o, n)
				if !match {
					continue
				}
				switch change {
				case "added":
					l.Added++
				case "removed":
					l.Removed++
				default:
					l.Modified++
				}
				if len(l.Entries) >= MaxListEntries {
					l.Omitted++
					continue
				}
				e := Entry{Change: change, Text: markedString(name, false, nil).clipped(MaxValueChars)}
				if change == "modified" {
					fields, omitted := entryFields(o, n, p)
					e.Fields = &fields
					e.FieldsOmitted = &omitted
				}
				l.Entries = append(l.Entries, e)
			}
		}
		l.Reordered = reordered(a, b)
		return l
	}
	a, b := make([]string, len(old)), make([]string, len(new))
	countA, countB := map[string]int{}, map[string]int{}
	for i, item := range old {
		a[i] = valueID(item)
		countA[a[i]]++
	}
	for i, item := range new {
		b[i] = valueID(item)
		countB[b[i]]++
	}
	for _, change := range []string{"added", "removed"} {
		ids, items, from, to := b, new, countB, countA
		if change == "removed" {
			ids, items, from, to = a, old, countA, countB
		}
		remaining := map[string]int{}
		for id, n := range from {
			remaining[id] = max(0, n-to[id])
			if change == "added" {
				l.Added += remaining[id]
			} else {
				l.Removed += remaining[id]
			}
		}
		for i, id := range ids {
			if remaining[id] == 0 {
				continue
			}
			remaining[id]--
			if len(l.Entries) >= MaxListEntries {
				l.Omitted++
				continue
			}
			l.Entries = append(l.Entries, Entry{Change: change, Text: show(items[i], p, false, true, true)})
		}
	}
	l.Reordered = l.Added == 0 && l.Removed == 0 && !slices.Equal(a, b)
	if l.Added+l.Removed > 0 {
		l.Reordered = reordered(unique(a), unique(b))
	}
	return l
}
func unique(a []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, s := range a {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}
func entryFields(old, new *yaml.Node, p []string) ([]Row, int) {
	b := newRows(MaxEntryFields)
	walk(old, new, nil, p, b, true)
	return b.rows, b.omitted
}

// Config compares two main YAML files by effective values, with bounded rows.
// Both inputs use yamlx.Parse's duplicate, recursion, and alias-budget policies.
func Config(old, new []byte, oldName, newName string) ConfigDiff {
	result := ConfigDiff{Format: "yaml", Status: "unparsable", Items: []Row{}}
	a, err := yamlx.Parse(old)
	if err != nil {
		return result
	}
	b, err := yamlx.Parse(new)
	if err != nil {
		return result
	}
	a = agentPaths(a)
	b = agentPaths(b)
	budget := newRows(MaxConfigRows)
	walk(a, b, nil, nil, budget, false)
	result.Status = "compared"
	if len(budget.rows) == 0 {
		result.Status = "formatting"
	}
	result.Items = budget.rows
	result.Omitted = budget.omitted
	result.Total = ptr(len(budget.rows) + budget.omitted)
	oldText, oldErr := canonicalYAML(a)
	newText, newErr := canonicalYAML(b)
	delta := len(new) - len(old)
	if oldErr == nil && newErr == nil {
		delta -= len(newText) - len(oldText)
	} else {
		delta = 0
	}
	result.Reformatted = ptr(abs(delta) >= FormatNoteBytes && (presentationChanged(a, b, 0) || abs(blankBytes(old)-blankBytes(new)) >= FormatNoteBytes))
	if oldName == "" {
		oldName = "config.yaml"
	}
	if newName == "" {
		newName = "config.yaml"
	}
	result.Raw = ptr(rawDiff(oldText, newText, oldName, newName))
	return result
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// agentPaths copies only containers on the edited path. Accepted alias graphs
// remain intact; yamlx.Clone must not be used on user-owned subtrees.
func agentPaths(n *yaml.Node) *yaml.Node {
	if !isMap(n) {
		return n
	}
	root := *resolve(n)
	root.Content = slices.Clone(root.Content)
	for _, section := range []string{"proxy-providers", "rule-providers"} {
		providers := resolve(yamlx.Get(n, section))
		if !isMap(providers) {
			continue
		}
		m := yamlx.Map()
		for key, provider := range yamlx.AllPairs(providers) {
			value := provider
			pn := resolve(yamlx.Get(provider, "path"))
			if pn != nil && pn.ShortTag() == "!!str" && pn.Value != "" && !strings.HasPrefix(pn.Value, "/") {
				cp := *resolve(provider)
				cp.Content = slices.Clone(cp.Content)
				yamlx.Set(&cp, "path", yamlx.Str(path.Clean(pn.Value)))
				value = &cp
			}
			m.Content = append(m.Content, key, value)
		}
		yamlx.Set(&root, section, m)
	}
	return &root
}

// canonicalYAML removes presentation annotations without expanding aliases.
func canonicalYAML(n *yaml.Node) ([]byte, error) {
	memo := map[*yaml.Node]*yaml.Node{}
	var copyNode func(*yaml.Node) *yaml.Node
	copyNode = func(n *yaml.Node) *yaml.Node {
		if n == nil {
			return nil
		}
		if c := memo[n]; c != nil {
			return c
		}
		c := *n
		memo[n] = &c
		c.Style = 0
		c.HeadComment = ""
		c.LineComment = ""
		c.FootComment = ""
		c.Line = 0
		c.Column = 0
		c.Content = nil
		for _, v := range n.Content {
			c.Content = append(c.Content, copyNode(v))
		}
		if n.Alias != nil {
			c.Alias = copyNode(n.Alias)
		}
		if c.Kind == yaml.ScalarNode {
			switch c.ShortTag() {
			case "!!bool", "!!int", "!!float", "!!null":
				c.Value = scalar(n)
			}
		}
		return &c
	}
	out, err := yamlx.Marshal(copyNode(n))
	if err != nil {
		return nil, fmt.Errorf("render config diff: %w", err)
	}
	return out, nil
}

// presentationChanged checks shared, nonempty values. A native encoder may
// indent a sequence differently from the input, so its byte delta alone must
// not label a large ordinary list edit as reformatting.
func presentationChanged(a, b *yaml.Node, depth int) bool {
	a, b = resolve(a), resolve(b)
	if a == nil || b == nil || depth > MaxDepth || a.Kind != b.Kind {
		return false
	}
	if (a.Kind == yaml.MappingNode || a.Kind == yaml.SequenceNode) && (len(a.Content) == 0 || len(b.Content) == 0) {
		return false
	}
	if a.Style != b.Style || a.Column != b.Column || a.HeadComment != b.HeadComment || a.LineComment != b.LineComment || a.FootComment != b.FootComment {
		return true
	}
	if a.Kind == yaml.MappingNode {
		byKey := map[string]pair{}
		for _, q := range pairs(b) {
			byKey[keyID(q)] = q
		}
		for _, q := range pairs(a) {
			if other, ok := byKey[keyID(q)]; ok && (presentationChanged(q.node, other.node, depth+1) || presentationChanged(q.value, other.value, depth+1)) {
				return true
			}
		}
	}
	if a.Kind == yaml.SequenceNode {
		for i := 0; i < min(len(a.Content), len(b.Content)); i++ {
			if presentationChanged(a.Content[i], b.Content[i], depth+1) {
				return true
			}
		}
	}
	return false
}

// Metadata paths have a fixed string wire type, unlike display texts. Keep
// credentials out of these grouping/search keys, while the marked raw diff
// and display values still let the dialog reveal the original key.
func metadataKey(s string) string {
	spans := diag.CredentialSpans(s)
	if len(spans) == 0 {
		return s
	}
	var b strings.Builder
	position := 0
	for _, r := range spans {
		b.WriteString(s[position:r[0]])
		b.WriteString(redact.Redacted)
		position = r[1]
	}
	b.WriteString(s[position:])
	return b.String()
}

func blankBytes(src []byte) int {
	total := 0
	for _, line := range strings.SplitAfter(string(src), "\n") {
		if strings.TrimSpace(line) == "" {
			total += len(line)
		}
	}
	return total
}
