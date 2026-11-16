package oxidns

import (
	"configflow/internal/convert"
	"configflow/internal/jsonx"
	"configflow/internal/rules"
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
// names tag as a whole word ([\w.\-] boundaries, optional "$").
func references(value any, tag string) bool {
	switch x := value.(type) {
	case string:
		pattern, err := rules.CompilePyRegex(`(?<![\w.\-])\$?`+convert.PyReEscape(tag)+`(?![\w.\-])`, false)
		must(err)
		return pattern.Search(x)
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
