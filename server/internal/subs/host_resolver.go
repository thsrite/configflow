package subs

import (
	"strings"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// HostResolver indexes one immutable open hosts graph. Exact keys are O(1);
// only wildcard keys participate in fallback scans, in source discovery order.
// Build it once per generation, not once for each mapped node domain.
type HostResolver struct {
	values    map[string]*yamlx.Node
	wildcards []string
}

// NewHostResolver builds a lookup without serializing or decoding configuration.
// The supplied graph must remain unchanged while the resolver is used.
func NewHostResolver(hosts *yamlx.Node) *HostResolver {
	result := &HostResolver{values: map[string]*yamlx.Node{}}
	for key, value := range yamlx.AllPairs(hosts) {
		key = yamlx.Resolve(key)
		if key == nil || key.Kind != yaml.ScalarNode || key.ShortTag() != "!!str" {
			continue
		}
		name := hostPatternText(key.Value)
		if _, exists := result.values[name]; !exists && (strings.Contains(name, "*") || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "+.")) {
			result.wildcards = append(result.wildcards, name)
		}
		result.values[name] = value
	}
	return result
}
func (h *HostResolver) lookup(domain string) *yamlx.Node {
	if value, present := h.values[domain]; present {
		return nonNullHost(value)
	}
	var value *yamlx.Node
	best := -1
	for _, pattern := range h.wildcards {
		if len(pattern) > best && hostPatternMatches(pattern, domain) {
			value = h.values[pattern]
			best = len(pattern)
		}
	}
	return nonNullHost(value)
}

func nonNullHost(value *yamlx.Node) *yamlx.Node {
	resolved := yamlx.Resolve(value)
	if resolved == nil || (resolved.Kind == yaml.ScalarNode && resolved.ShortTag() == "!!null") {
		return nil
	}
	return value
}

// Target follows CNAME-style host values and returns the final unresolved domain.
// Address lists, invalid values, cycles and absent mappings have no target.
func (h *HostResolver) Target(domain string) string {
	value := h.lookup(domain)
	seen := map[string]bool{}
	for value != nil {
		value = yamlx.Resolve(value)
		if value == nil {
			return ""
		}
		if value.Kind == yaml.SequenceNode {
			if len(value.Content) != 1 {
				return ""
			}
			value = yamlx.Resolve(value.Content[0])
		}
		if value == nil || value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" || usableAddress(value.Value) != "" {
			return ""
		}
		target := hostPatternText(value.Value)
		if seen[target] {
			return ""
		}
		seen[target] = true
		following := h.lookup(target)
		if following == nil {
			return target
		}
		value = following
	}
	return ""
}

func hostPatternMatches(pattern, domain string) bool {
	if strings.HasPrefix(pattern, "+.") {
		base := pattern[2:]
		return domain == base || strings.HasSuffix(domain, "."+base)
	}
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(domain, pattern)
	}
	if !strings.Contains(pattern, "*") {
		return pattern == domain
	}
	left, right := strings.Split(pattern, "."), strings.Split(domain, ".")
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != "*" && left[i] != right[i] {
			return false
		}
	}
	return true
}
