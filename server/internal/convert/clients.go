package convert

// Shared helpers for the Surge and Loon generators. The legacy dynamic
// document/error adapters remain until S-convert (after the N8 design);
// string transformations use the Go standard library.

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/rules"
	"configflow/internal/subs"
)

// pyDict is a Python dict with hashable keys (hashID): insertion-ordered, a
// later assignment keeps the first position.
type pyDict struct {
	keys   []string
	values map[string]any
	orig   map[string]any
}

func newDict() *pyDict { return &pyDict{values: map[string]any{}, orig: map[string]any{}} }

// set is d[k] = v (the key is hashed when stored).
func (d *pyDict) set(k, v any) {
	h := key(k)
	if _, ok := d.values[h]; !ok {
		d.keys = append(d.keys, h)
		d.orig[h] = k
	}
	d.values[h] = v
}

// get is d.get(k, def).
func (d *pyDict) get(k, def any) any {
	if v, ok := d.values[key(k)]; ok {
		return v
	}
	return def
}

// has is `k in d`.
func (d *pyDict) has(k any) bool {
	_, ok := d.values[key(k)]
	return ok
}

// item is d[k].
func (d *pyDict) item(k any) any {
	v, ok := d.values[key(k)]
	if !ok {
		raise(pyError("KeyError", jsonx.Repr(k)))
	}
	return v
}

func (d *pyDict) len() int { return len(d.keys) }

// ordered values in insertion order.
func (d *pyDict) list() []any {
	out := make([]any, len(d.keys))
	for i, k := range d.keys {
		out[i] = d.values[k]
	}
	return out
}

// truthy is bool(v), including YAML-only values.
func truthy(v any) bool { return pyTruthy(v) }

// or2 is `a or b`.
func or2(a, b any) any {
	if truthy(a) {
		return a
	}
	return b
}

// eq is a == b.
func eq(a, b any) bool { return jsonx.Equal(a, b) }

// joinAny is sep.join(items): every item must be a str.
func joinAny(sep string, items []any) string {
	parts := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			raise(pyError("TypeError", fmt.Sprintf("sequence item %d: expected str instance, %s found", i, typeName(item))))
		}
		parts[i] = s
	}
	return strings.Join(parts, sep)
}

// pyStrip is v.strip() (AttributeError when v is not a str).
func pyStrip(v any) string { return strings.TrimSpace(strMethod(v, "strip")) }

// pyReplace is v.replace(old, new).
func pyReplace(v any, old, new string) string {
	return strings.ReplaceAll(strMethod(v, "replace"), old, new)
}

// pyLower is v.lower().
func pyLower(v any) string { return strings.ToLower(strMethod(v, "lower")) }

// startsWith is v.startswith(prefix).
func startsWith(v any, prefix string) bool {
	return strings.HasPrefix(strMethod(v, "startswith"), prefix)
}

// isList is isinstance(v, list).
func isList(v any) bool {
	_, ok := v.([]any)
	return ok
}

// isDict is isinstance(v, dict).
func isDict(v any) bool {
	o, ok := v.(*jsonx.Object)
	return ok && o != nil
}

// strIn is `needle in v` for a str needle.
func strIn(needle string, v any) bool {
	switch x := v.(type) {
	case string:
		return strings.Contains(x, needle)
	case []any:
		for _, item := range x {
			if eq(item, needle) {
				return true
			}
		}
		return false
	case *jsonx.Object:
		if x != nil {
			return x.Has(needle)
		}
	case []byte:
		raise(pyError("TypeError", "a bytes-like object is required, not 'str'"))
	}
	raise(pyError("TypeError", "argument of type '"+typeName(v)+"' is not iterable"))
	return false
}

// nextMatch is next((x for x in items if x.get(field) == want), None).
func nextMatch(items any, field string, want any) any {
	for _, item := range iterate(items) {
		if eq(dictGet(item, field, nil), want) {
			return item
		}
	}
	return nil
}

// hasPortListAny is common.has_port_list(rule_type, value) for any values.
func hasPortListAny(ruleType, value any) bool {
	switch {
	case contains(PortRuleTypes, ruleType):
		if s, ok := value.(string); ok {
			return HasPortList(ruleType.(string), s)
		}
		return strIn("/", value) || strIn(",", value)
	case contains(LogicTypes, ruleType):
		s, ok := value.(string)
		if !ok {
			raise(pyError("TypeError", "expected string or bytes-like object, got '"+typeName(value)+"'"))
		}
		return HasPortList(ruleType.(string), s)
	}
	return false
}

// networkAsProtocolAny is common.network_as_protocol(rule_type, value).
func networkAsProtocolAny(ruleType, value any) (any, any) {
	if eq(ruleType, "NETWORK") {
		return "PROTOCOL", strings.ToUpper(pyStrip(value))
	}
	if contains(LogicTypes, ruleType) {
		s, ok := value.(string)
		if !ok {
			raise(pyError("TypeError", "expected string or bytes-like object, got '"+typeName(value)+"'"))
		}
		t, v := NetworkAsProtocol(ruleType.(string), s)
		return t, v
	}
	return ruleType, value
}

// rulesetFormatAny is common.ruleset_format(rule, config_data, source_url)
// for a source_url of any type. A non-string truthy source_url only matters
// when ruleset_format falls back to it, where urlsplit raises
// AttributeError ('decode'): the two sentinel URLs tell whether it does.
func rulesetFormatAny(rule, config *jsonx.Object, sourceURL any) string {
	if s, ok := sourceURL.(string); ok {
		return mustValue(RulesetFormat(rule, config, s))
	}
	if !truthy(sourceURL) {
		return mustValue(RulesetFormat(rule, config, ""))
	}
	yamlFormat := mustValue(RulesetFormat(rule, config, "/x.yaml"))
	mrsFormat := mustValue(RulesetFormat(rule, config, "/x.mrs"))
	if yamlFormat != mrsFormat {
		raise(attributeError(sourceURL, "decode"))
	}
	return yamlFormat
}

// validateRulePolicies is strategy_references.validate_rule_policies; the
// set membership of an unhashable policy raises TypeError in Python.
func validateRulePolicies(config *jsonx.Object, available []string, format string, builtins []string) {
	known := map[string]bool{}
	for _, name := range available {
		known[name] = true
	}
	for _, name := range builtins {
		known[name] = true
	}
	defaultPolicy := "Proxy"
	for _, item := range iterate(dictGet(config, "rule_configs", []any{})) {
		if !truthy(dictGet(item, "enabled", true)) || !truthy(dictGet(item, "library_enabled", true)) {
			continue
		}
		itemType := dictGet(item, "itemType", nil)
		if !inStrings(itemType, "rule", "ruleset") {
			continue
		}
		def := any("")
		if eq(itemType, "ruleset") {
			def = defaultPolicy
		}
		policy := dictGet(item, "policy", def)
		key(policy) // unhashable → TypeError
		if s, ok := policy.(string); ok && known[s] {
			continue
		}
		break
	}
	must(rules.ValidateRulePolicies(config, available, format))
}

// missingConverter fails closed when Sub-Store is not wired.
type missingConverter struct{}

func (missingConverter) ConvertProxyString(context.Context, string) *jsonx.Object {
	raise(&HookMissingError{Hook: "convert_proxy_string", Provider: "convert/mihomoenv (C13 substore)"})
	return nil
}

// resolveProxyString is the proxy_string step shared by convert_node_to_surge
// and loon._resolve_node: parse_uri_list(node['proxy_string'])[0] with the
// outer name; ok false when Python returns None (no node, or any exception
// inside the try).
func resolveProxyString(node *jsonx.Object) (parsed *jsonx.Object, raw, ok bool) {
	outerName := node.GetOr("name", "")
	text, isString := node.Value("proxy_string").(string)
	if !isString {
		return nil, false, false // content.strip() raises inside the try
	}
	nodes := subs.ParseURIList(text)
	if len(nodes) == 0 {
		return nil, false, false
	}
	first := nodes[0]
	if truthy(first.Value("_raw_object")) {
		copied := jsonx.NewObject()
		for k, v := range first.All() {
			if k != "_raw_object" {
				copied.Set(k, v)
			}
		}
		copied.Set("name", outerName)
		return copied, true, true
	}
	first.Set("name", outerName)
	return first, false, true
}
