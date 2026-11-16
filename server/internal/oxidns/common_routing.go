package oxidns

import (
	"configflow/internal/jsonx"
)

// Parts of backend/common/oxidns_common_routing.py (module C25) that
// oxidns_common (C24) needs and that have no further dependencies; C25 adds
// the rest of the module to this file.

// Selectors is SELECTORS.
var Selectors = []string{"direct_rules", "proxy_rules", "direct_rulesets", "proxy_rulesets"}

// RuleDNSFields is RULE_DNS_FIELDS.
var RuleDNSFields = []string{"rule_actions", "rule_dns_follow"}

// RoutingFields is ROUTING_FIELDS: the routing controls of the common
// settings form.
var RoutingFields = func() map[string]bool {
	out := map[string]bool{}
	for _, field := range append(append(append([]string{}, Selectors...), RuleDNSFields...),
		"aggregate_rules", "default_forward", "custom_matches", "custom_match_position") {
		out[field] = true
	}
	return out
}()

// references is _references(value, tag): whether a text anywhere in value
// names tag as a whole word ([\w.\-] boundaries, optional "$"), i.e.
// re.search(r'(?<![\w.\-])\$?' + re.escape(tag) + r'(?![\w.\-])', text).
func references(value any, tag string) bool {
	switch x := value.(type) {
	case string:
		return referencesText(x, tag)
	case *jsonx.Object:
		for _, element := range x.Values() {
			if references(element, tag) {
				return true
			}
		}
	case []any:
		for _, element := range x {
			if references(element, tag) {
				return true
			}
		}
	}
	return false
}

// isTagRune is [\w.\-] with Python's Unicode \w.
func isTagRune(r rune) bool { return r == '.' || r == '-' || isWordRune(r) }

// tagAt reports whether g occurs in t at q followed by a non-tag rune or
// the end.
func tagAt(t []rune, q int, g []rune) bool {
	if q+len(g) > len(t) {
		return false
	}
	for i := range g {
		if t[q+i] != g[i] {
			return false
		}
	}
	end := q + len(g)
	return end == len(t) || !isTagRune(t[end])
}

func referencesText(text, tag string) bool {
	t, g := []rune(text), []rune(tag)
	for p := 0; p <= len(t); p++ {
		if p > 0 && isTagRune(t[p-1]) {
			continue
		}
		if p < len(t) && t[p] == '$' && tagAt(t, p+1, g) {
			return true
		}
		if tagAt(t, p, g) {
			return true
		}
	}
	return false
}

// words is _words(value): every maximal run of tag runes in the strings
// references reads.
func words(value any, found map[string]bool) {
	switch x := value.(type) {
	case string:
		start := -1
		runes := []rune(x)
		for i, r := range runes {
			if isTagRune(r) {
				if start < 0 {
					start = i
				}
			} else if start >= 0 {
				found[string(runes[start:i])] = true
				start = -1
			}
		}
		if start >= 0 {
			found[string(runes[start:])] = true
		}
	case *jsonx.Object:
		for _, element := range x.Values() {
			words(element, found)
		}
	case []any:
		for _, element := range x {
			words(element, found)
		}
	}
}

// isWordTag is _WORD.fullmatch(tag).
func isWordTag(tag string) bool {
	if tag == "" {
		return false
	}
	for _, r := range tag {
		if !isTagRune(r) {
			return false
		}
	}
	return true
}

// mentions is _Mentions: which of several values reference a tag, exactly
// as references decides (each value is scanned once for all word tags).
type mentions struct {
	values []any
	index  map[string][]int
}

func newMentions(values []any) *mentions {
	m := &mentions{values: values, index: map[string][]int{}}
	for position, value := range values {
		found := map[string]bool{}
		words(value, found)
		for word := range found {
			m.index[word] = append(m.index[word], position)
		}
	}
	return m
}

// positions is _Mentions.positions(tag) (ascending).
func (m *mentions) positions(tag string) []int {
	if isWordTag(tag) {
		return m.index[tag]
	}
	var out []int
	for position, value := range m.values {
		if references(value, tag) {
			out = append(out, position)
		}
	}
	return out
}

// replaceDownloadEntries is replace_download_entries(download, entries):
// download['args'] = deepcopy({**download['args'], 'downloads': entries}).
func replaceDownloadEntries(download *jsonx.Object, entries []any) {
	args := item(download, "args")
	merged := mergedCopy(args)
	merged.Set("downloads", entries)
	download.Set("args", copyValue(merged))
}

// mergedCopy is {**value}: a shallow copy of a mapping (TypeError for
// anything else).
func mergedCopy(value any) *jsonx.Object {
	obj, ok := value.(*jsonx.Object)
	if !ok || obj == nil {
		raise(pyErr("TypeError", "'"+typeName(value)+"' object is not a mapping"))
	}
	return obj.Clone()
}

// referenceReason is reference_reason(references, prefix) (raising).
func referenceReason(found []any, prefix string) string {
	reason, err := ReferenceReason(found, prefix)
	must(err)
	return reason
}
