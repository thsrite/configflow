package pushdiff

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"configflow/internal/rulesetfmt"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// RuleEntries reads text rules or a YAML payload, rejecting invalid UTF-8 and
// unsafe YAML. Comments, blank lines and YAML quoting are excluded from rules.
func RuleEntries(data []byte) ([]string, error) {
	if !utf8.Valid(data) {
		return nil, fmt.Errorf("rule file is not UTF-8")
	}
	text := strings.TrimPrefix(string(data), "\ufeff")
	out := []string{}
	if rulesetfmt.HasYAMLPayload(text) {
		doc, err := yamlx.Parse([]byte(text))
		if err != nil {
			return nil, fmt.Errorf("parse rule payload: %w", err)
		}
		payload := resolve(yamlx.Get(doc, "payload"))
		if !isList(payload) {
			return nil, fmt.Errorf("rule payload must be a sequence")
		}
		for _, n := range payload.Content {
			n = resolve(n)
			if n == nil || n.Kind != yaml.ScalarNode {
				return nil, fmt.Errorf("rule payload entry must be a scalar")
			}
			out = append(out, scalar(n))
		}
		return out, nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r", "\n"), "\n") {
		s := strings.TrimSpace(line)
		if s != "" && !strings.HasPrefix(s, "#") && !strings.HasPrefix(s, "//") {
			out = append(out, s)
		}
	}
	return out, nil
}
func counts(items []string) map[string]int {
	out := map[string]int{}
	for _, s := range items {
		out[s]++
	}
	return out
}
func subtract(a, b map[string]int) (map[string]int, int) {
	out := map[string]int{}
	total := 0
	for k, n := range a {
		if d := n - b[k]; d > 0 {
			out[k] = d
			total += d
		}
	}
	return out, total
}
func ruleChange(old, new []byte, budget *detailBudget, keep *keeper) (*Summary, *Details) {
	a, err := RuleEntries(old)
	if err != nil {
		return sizeSummary(old, new), nil
	}
	b, err := RuleEntries(new)
	if err != nil {
		return sizeSummary(old, new), nil
	}
	added, nadd := subtract(counts(b), counts(a))
	removed, nremove := subtract(counts(a), counts(b))
	summary := &Summary{Kind: "rules", Added: &nadd, Removed: &nremove}
	if old != nil {
		summary.Before = ptr(len(a))
	}
	if new != nil {
		summary.After = ptr(len(b))
	}
	if budget == nil || nadd+nremove == 0 {
		return summary, nil
	}
	produce := func(change string, yield func(Entry) bool) {
		items, left := b, added
		if change == "removed" {
			items, left = a, removed
		}
		for _, item := range items {
			if left[item] > 0 {
				left[item]--
				if !yield(Entry{Change: change, Text: markedString(item, false, nil).clipped(MaxValueChars)}) {
					return
				}
			}
		}
	}
	return summary, listGroups("rules", []string{"added", "removed"}, []int{nadd, nremove}, produce, budget, keep, 0)
}

type nodeItem struct {
	name       string
	node       *yaml.Node
	comparable string
}

func nodeItems(data []byte) ([]nodeItem, error) {
	if data == nil {
		return []nodeItem{}, nil
	}
	doc, err := yamlx.Parse(data)
	if err != nil {
		return nil, err
	}
	proxies := resolve(yamlx.Get(doc, "proxies"))
	if !isList(proxies) {
		return nil, fmt.Errorf("subscription must contain proxies sequence")
	}
	out := make([]nodeItem, 0, len(proxies.Content))
	for _, node := range proxies.Content {
		name := ""
		if n := resolve(yamlx.Get(node, "name")); n != nil && n.ShortTag() == "!!str" {
			name = n.Value
		}
		out = append(out, nodeItem{name, node, valueID(node)})
	}
	return out, nil
}

type nodePairs struct {
	added, removed []int
	modified       [][2]int
	repeated       int
}

func pairNodes(a, b []nodeItem) nodePairs {
	result := nodePairs{}
	old, new := map[string][]int{}, map[string][]int{}
	names := []string{}
	seen := map[string]bool{}
	for i, item := range a {
		old[item.name] = append(old[item.name], i)
		if !seen[item.name] {
			names = append(names, item.name)
			seen[item.name] = true
		}
	}
	for i, item := range b {
		new[item.name] = append(new[item.name], i)
		if !seen[item.name] {
			names = append(names, item.name)
			seen[item.name] = true
		}
	}
	for _, name := range names {
		olds, news := old[name], new[name]
		if len(olds) == 1 && len(news) == 1 {
			if a[olds[0]].comparable != b[news[0]].comparable {
				result.modified = append(result.modified, [2]int{olds[0], news[0]})
			}
			continue
		}
		if len(olds) > 1 || len(news) > 1 {
			result.repeated++
			same := map[string][]int{}
			for _, i := range olds {
				same[a[i].comparable] = append(same[a[i].comparable], i)
			}
			leftNew := []int{}
			for _, i := range news {
				twins := same[b[i].comparable]
				if len(twins) > 0 {
					same[b[i].comparable] = twins[1:]
				} else {
					leftNew = append(leftNew, i)
				}
			}
			olds = []int{}
			for _, indices := range same {
				olds = append(olds, indices...)
			}
			sort.Ints(olds)
			news = leftNew
		}
		common := min(len(olds), len(news))
		for i := 0; i < common; i++ {
			result.modified = append(result.modified, [2]int{olds[i], news[i]})
		}
		result.removed = append(result.removed, olds[common:]...)
		result.added = append(result.added, news[common:]...)
	}
	sort.Ints(result.added)
	sort.Ints(result.removed)
	sort.Slice(result.modified, func(i, j int) bool { return result.modified[i][1] < result.modified[j][1] })
	return result
}
func occurrences(items []nodeItem) map[int]int {
	total, seen := map[string]int{}, map[string]int{}
	for _, item := range items {
		total[item.name]++
	}
	out := map[int]int{}
	for i, item := range items {
		if total[item.name] > 1 {
			seen[item.name]++
			out[i] = seen[item.name]
		}
	}
	return out
}
func nodeInfo(n *yaml.Node) *Text {
	if !isMap(n) {
		return nil
	}
	parts := []string{}
	kind := resolve(yamlx.Get(n, "type"))
	if kind != nil && kind.ShortTag() == "!!str" && kind.Value != "" {
		parts = append(parts, kind.Value)
	}
	server, port := resolve(yamlx.Get(n, "server")), resolve(yamlx.Get(n, "port"))
	if server != nil && (server.ShortTag() == "!!str" || server.ShortTag() == "!!int") && scalar(server) != "" {
		host := scalar(server)
		if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
			host = "[" + host + "]"
		}
		if port != nil && port.ShortTag() != "!!null" && scalar(port) != "" {
			host += ":" + scalar(port)
		}
		parts = append(parts, host)
	}
	if len(parts) == 0 {
		return nil
	}
	return ptr(markedString(strings.Join(parts, " · "), false, nil).clipped(MaxValueChars))
}
func nodeEntry(change string, items []nodeItem, i int, places map[int]int) Entry {
	name := items[i].name
	if name == "" {
		name = unnamed
	}
	e := Entry{Change: change, Text: markedString(name, false, nil).clipped(MaxValueChars), Nth: places[i]}
	if change != "modified" {
		e.Info = nodeInfo(items[i].node)
	}
	return e
}
func nodeChange(old, new []byte, budget *detailBudget, keep *keeper) (*Summary, *Details) {
	a, err := nodeItems(old)
	if err != nil {
		return sizeSummary(old, new), nil
	}
	b, err := nodeItems(new)
	if err != nil {
		return sizeSummary(old, new), nil
	}
	pairs := pairNodes(a, b)
	summary := &Summary{Kind: "nodes", Added: ptr(len(pairs.added)), Removed: ptr(len(pairs.removed)), Modified: ptr(len(pairs.modified))}
	if old != nil {
		summary.Before = ptr(len(a))
	}
	if new != nil {
		summary.After = ptr(len(b))
	}
	if budget == nil || len(pairs.added)+len(pairs.removed)+len(pairs.modified) == 0 {
		return summary, nil
	}
	oldPlaces, newPlaces := occurrences(a), occurrences(b)
	produce := func(change string, yield func(Entry) bool) {
		if change == "modified" {
			for _, p := range pairs.modified {
				e := nodeEntry(change, b, p[1], newPlaces)
				fields, omitted := entryFields(a[p[0]].node, b[p[1]].node, []string{"proxies"})
				rank := func(key string) int {
					switch key {
					case "type":
						return 0
					case "server":
						return 1
					case "port":
						return 2
					}
					return 3
				}
				slices.SortStableFunc(fields, func(a, b Row) int { return rank(a.Key) - rank(b.Key) })
				e.Fields = &fields
				e.FieldsOmitted = &omitted
				if !yield(e) {
					return
				}
			}
			return
		}
		items, indices, places := b, pairs.added, newPlaces
		if change == "removed" {
			items, indices, places = a, pairs.removed, oldPlaces
		}
		for _, i := range indices {
			if !yield(nodeEntry(change, items, i, places)) {
				return
			}
		}
	}
	return summary, listGroups("nodes", []string{"added", "removed", "modified"}, []int{len(pairs.added), len(pairs.removed), len(pairs.modified)}, produce, budget, keep, pairs.repeated)
}
