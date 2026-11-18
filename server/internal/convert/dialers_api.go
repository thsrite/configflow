package convert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v3"
)

// IncomingRefs returns stable metadata users followed by active graph dependents.
func IncomingRefs(config *model.Config, kind, id string) ([]Ref, error) {
	target := Ref{kind, id}
	var direct []Ref
	if config != nil {
		for _, node := range config.Nodes {
			if node.DialerRef != nil && stableRef(node.DialerRef) && resourceRef(*node.DialerRef) == target {
				direct = append(direct, Ref{"node", node.ID})
			}
		}
	}
	for _, group := range configGroups(config) {
		matches := false
		if group.Type == "chain" {
			matches = group.Chain != nil && (resourceRef(group.Chain.Entry) == target || resourceRef(group.Chain.Exit) == target)
		} else {
			matches = slices.Contains(GroupRefs(group), target)
			if !matches && kind == "group" {
				matches = slices.Contains(stringList(group.IncludeGroups), id) || stringValue(group.FollowGroup) == id
			}
		}
		if matches {
			direct = append(direct, Ref{"group", group.ID})
		}
	}
	graph, err := BuildDialerGraph(config)
	if err != nil {
		return nil, err
	}
	for _, key := range graph.keys {
		if slices.Contains(graph.children[key], target) {
			direct = append(direct, key)
		}
	}
	result := []Ref{}
	seen := map[Ref]bool{}
	for _, ref := range direct {
		if !seen[ref] {
			seen[ref] = true
			result = append(result, ref)
		}
	}
	return result, nil
}

// ResolveNodeDialer returns the stored target name for a managed reference.
func ResolveNodeDialer(config *model.Config, node model.Node) (*string, error) {
	if err := validateStoredNodeReference(node); err != nil {
		return nil, err
	}
	if node.DialerRef == nil {
		return nil, nil
	}
	if !stableRef(node.DialerRef) {
		return nil, errors.New(msgDialerRef)
	}
	if config != nil {
		if node.DialerRef.Type == "node" {
			for _, target := range config.Nodes {
				if target.ID == node.DialerRef.ID {
					name := target.Name
					return &name, nil
				}
			}
		} else {
			for _, target := range configGroups(config) {
				if target.ID == node.DialerRef.ID {
					name := target.Name
					return &name, nil
				}
			}
		}
	}
	return nil, errors.New(msgDialerRefMissing)
}

// ApplyNodeDialer attaches stable metadata to an owned converted proxy.
func ApplyNodeDialer(config *model.Config, node model.Node, proxy *yamlx.Node) (*yamlx.Node, error) {
	if proxy == nil || proxy.Kind == yaml.MappingNode && len(proxy.Content) == 0 {
		graph, err := BuildDialerGraph(config)
		if err != nil {
			return nil, err
		}
		if graph.has(Ref{"node", node.ID}) {
			return nil, errors.New(msgDialerConversion)
		}
		return proxy, nil
	}
	if node.DialerRef != nil {
		raw, _ := node.Fields.Get("_proxy_chain_id")
		var chainID string
		if json.Unmarshal(raw, &chainID) == nil && chainID != "" {
			yamlx.Set(proxy, "name", yamlx.Str(node.Name))
		}
		name, err := ResolveNodeDialer(config, node)
		if err != nil {
			return nil, err
		}
		yamlx.Set(proxy, "dialer-proxy", yamlx.Str(*name))
	}
	return proxy, nil
}

// FilterSpec describes original-name filtering, in backtick pattern order.
type FilterSpec struct{ Include, Exclude, ExcludeType string }

// NameRule is one native Provider name substitution.
type NameRule struct {
	Pattern string `json:"pattern"`
	Target  string `json:"target"`
}

// NameOverride defines Provider names independently of the payload node shape.
type NameOverride struct {
	Rules  []NameRule `json:"proxy-name"`
	Prefix string     `json:"additional-prefix"`
	Suffix string     `json:"additional-suffix"`
}

type providerCandidate struct {
	name, kind string
	node       *yamlx.Node
	ref        Ref
}

func candidateFromNode(node *yamlx.Node) (providerCandidate, error) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return providerCandidate{}, errors.New(msgTopologyShape)
	}
	name, err := requiredEmittedString(node, "name")
	if err != nil {
		return providerCandidate{}, err
	}
	kind, err := emittedString(node, "type")
	if err != nil {
		return providerCandidate{}, err
	}
	return providerCandidate{name: name, kind: kind, node: node}, nil
}
func filterCandidates(proxies []providerCandidate, spec FilterSpec) ([]providerCandidate, error) {
	var exclusions, filters []*rules.PyRegex
	compile := func(pattern string) (*rules.PyRegex, error) {
		re, err := rules.CompilePyRegex(pattern, false)
		if err != nil {
			return nil, errors.New(msgProviderFilter)
		}
		return re, nil
	}
	for _, pattern := range strings.Split(spec.Exclude, "`") {
		if pattern != "" {
			re, err := compile(pattern)
			if err != nil {
				return nil, err
			}
			exclusions = append(exclusions, re)
		}
	}
	for _, pattern := range strings.Split(spec.Include, "`") {
		re, err := compile(pattern)
		if err != nil {
			return nil, err
		}
		filters = append(filters, re)
	}
	excludedTypes := map[string]bool{}
	for _, kind := range strings.Split(spec.ExcludeType, "|") {
		if kind != "" {
			excludedTypes[cases.Lower(language.Und).String(kind)] = true
		}
	}
	selected := []providerCandidate{}
	seen := map[string]bool{}
	for _, pattern := range filters {
		for _, proxy := range proxies {
			if seen[proxy.name] || excludedTypes[cases.Lower(language.Und).String(proxy.kind)] || !pattern.Search(proxy.name) {
				continue
			}
			excluded := false
			for _, exclude := range exclusions {
				if exclude.Search(proxy.name) {
					excluded = true
					break
				}
			}
			if !excluded {
				selected = append(selected, proxy)
				seen[proxy.name] = true
			}
		}
	}
	return selected, nil
}

// FilterProxyNodes selects original provider nodes without copying or expanding them.
func FilterProxyNodes(proxies []*yamlx.Node, spec FilterSpec) ([]*yamlx.Node, error) {
	candidates := make([]providerCandidate, 0, len(proxies))
	for _, proxy := range proxies {
		candidate, err := candidateFromNode(proxy)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	selected, err := filterCandidates(candidates, spec)
	if err != nil {
		return nil, err
	}
	result := make([]*yamlx.Node, 0, len(selected))
	for _, candidate := range selected {
		result = append(result, candidate.node)
	}
	return result, nil
}

var providerGroupPlaceholder = regexp.MustCompile(`\$([\p{Nd}]+)`)

func expandProviderTarget(target string, match *rules.Match) (string, error) {
	var failure error
	result := providerGroupPlaceholder.ReplaceAllStringFunc(target, func(part string) string {
		// Native group ordinals are ASCII. Preserve the entire spelling of a
		// non-ASCII ordinal, including a mixed ordinal, as literal text.
		if strings.ContainsFunc(part[1:], func(r rune) bool { return r < '0' || r > '9' }) {
			return part
		}
		index, err := strconv.Atoi(part[1:])
		if err != nil || index > match.Groups() {
			failure = errors.New(msgProviderNameRule)
			return ""
		}
		value, _ := match.Group(index)
		return value
	})
	return result, failure
}

// ProviderName applies the typed chain-name override to one original name.
func ProviderName(name string, override NameOverride) (string, error) {
	if len(override.Rules) > 0 {
		marker := override.Rules[0].Target
		for range 2 {
			if index := strings.LastIndexByte(marker, '_'); index >= 0 {
				marker = marker[:index]
			} else {
				break
			}
		}
		if strings.HasPrefix(name, marker+"_") {
			return "", errors.New(msgProviderMarker)
		}
	}
	for _, rule := range override.Rules {
		pattern, err := rules.CompilePyRegex(rule.Pattern, false)
		if err != nil {
			return "", errors.New(msgProviderNameRule)
		}
		name, err = pattern.Sub(name, func(match *rules.Match) (string, error) { return expandProviderTarget(rule.Target, match) }, 0)
		if err != nil {
			return "", errors.New(msgProviderNameRule)
		}
	}
	return override.Prefix + name + override.Suffix, nil
}

// TopologyOptions supplies optional verified Provider payloads.
type TopologyOptions struct {
	ForceChains, RequireProviders bool
	ProviderProxies               map[string][]*yamlx.Node
}

func emittedString(node *yamlx.Node, key string) (string, error) {
	value := yamlx.Resolve(yamlx.Get(node, key))
	if value == nil {
		return "", nil
	}
	if value.Kind != yaml.ScalarNode || value.ShortTag() != "!!str" {
		return "", errors.New(msgTopologyShape)
	}
	return value.Value, nil
}

func requiredEmittedString(node *yamlx.Node, key string) (string, error) {
	if yamlx.Get(node, key) == nil {
		return "", errors.New(msgTopologyShape)
	}
	return emittedString(node, key)
}
func emittedList(node *yamlx.Node, key string) ([]*yamlx.Node, error) {
	value := yamlx.Resolve(yamlx.Get(node, key))
	if value == nil {
		return nil, nil
	}
	if value.Kind != yaml.SequenceNode {
		return nil, errors.New(msgTopologyShape)
	}
	return value.Content, nil
}
func emittedNames(node *yamlx.Node, key string) ([]string, error) {
	items, err := emittedList(node, key)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.ScalarNode || item.ShortTag() != "!!str" {
			return nil, errors.New(msgTopologyShape)
		}
		result = append(result, item.Value)
	}
	return result, nil
}
func emittedFilter(node *yamlx.Node) (FilterSpec, error) {
	include, err := emittedString(node, "filter")
	if err != nil {
		return FilterSpec{}, err
	}
	exclude, err := emittedString(node, "exclude-filter")
	if err != nil {
		return FilterSpec{}, err
	}
	kind, err := emittedString(node, "exclude-type")
	return FilterSpec{include, exclude, kind}, err
}
func presentTarget(node *yamlx.Node) (*yamlx.Node, bool) {
	value := yamlx.Resolve(yamlx.Get(node, "dialer-proxy"))
	if value == nil {
		return nil, false
	}
	if value.ShortTag() == "!!null" {
		return nil, true
	}
	return value, true
}
func emittedProviderOverride(node *yamlx.Node) (*yamlx.Node, error) {
	node = yamlx.Resolve(node)
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, errors.New(msgTopologyShape)
	}
	value := yamlx.Resolve(yamlx.Get(node, "override"))
	if value == nil {
		value = yamlx.Map()
	}
	if value.Kind != yaml.MappingNode {
		return nil, errors.New(msgTopologyShape)
	}
	return value, nil
}
func emittedOverride(node *yamlx.Node) (*yamlx.Node, NameOverride, error) {
	value, err := emittedProviderOverride(node)
	if err != nil {
		return nil, NameOverride{}, err
	}
	result, err := nameOverrideFromNode(value)
	return value, result, err
}

// nameOverrideFromNode uses the engine's exact, case-sensitive keys. A plain
// encoding/json struct decoder would also accept "Additional-Prefix", which
// the engine and the old exact-key reader intentionally ignore.
func nameOverrideFromNode(node *yamlx.Node) (NameOverride, error) {
	var result NameOverride
	node = yamlx.Resolve(node)
	if node == nil || node.ShortTag() == "!!null" {
		return result, nil
	}
	if node.Kind != yaml.MappingNode {
		return result, errors.New(msgProviderNameRule)
	}
	var err error
	result.Prefix, err = emittedString(node, "additional-prefix")
	if err != nil {
		return result, errors.New(msgProviderNameRule)
	}
	result.Suffix, err = emittedString(node, "additional-suffix")
	if err != nil {
		return result, errors.New(msgProviderNameRule)
	}
	items, err := emittedList(node, "proxy-name")
	if err != nil {
		return result, errors.New(msgProviderNameRule)
	}
	for _, item := range items {
		item = yamlx.Resolve(item)
		if item == nil || item.Kind != yaml.MappingNode || yamlx.Get(item, "pattern") == nil || yamlx.Get(item, "target") == nil {
			return result, errors.New(msgProviderNameRule)
		}
		pattern, err := emittedString(item, "pattern")
		if err != nil {
			return result, errors.New(msgProviderNameRule)
		}
		target, err := emittedString(item, "target")
		if err != nil {
			return result, errors.New(msgProviderNameRule)
		}
		result.Rules = append(result.Rules, NameRule{Pattern: pattern, Target: target})
	}
	return result, nil
}

// ValidateTopology checks a generated document and its verified provider candidates.
func ValidateTopology(document *yamlx.Node, opts TopologyOptions) error {
	if _, err := yamlx.Encode(document); err != nil {
		return err
	}
	if root := yamlx.Resolve(document); root == nil || root.Kind != yaml.MappingNode {
		return errors.New(msgTopologyShape)
	}
	proxies, err := emittedList(document, "proxies")
	if err != nil {
		return err
	}
	groupNodes, err := emittedList(document, "proxy-groups")
	if err != nil {
		return err
	}
	objects := append(slices.Clone(proxies), groupNodes...)
	definitions := yamlx.Resolve(yamlx.Get(document, "proxy-providers"))
	if definitions == nil {
		definitions = yamlx.Map()
	}
	if definitions.Kind != yaml.MappingNode {
		return errors.New(msgTopologyShape)
	}
	active := opts.ForceChains
	for _, node := range objects {
		if target, _ := presentTarget(node); target != nil {
			active = true
		}
	}
	for _, definition := range yamlx.Pairs(definitions) {
		if active {
			break
		}
		override, err := emittedProviderOverride(definition)
		if err != nil {
			return err
		}
		if target, _ := presentTarget(override); target != nil && scalarText(target) != "" {
			active = true
		}
	}
	for _, payload := range opts.ProviderProxies {
		for _, proxy := range payload {
			if target, _ := presentTarget(proxy); target != nil {
				active = true
			}
		}
	}
	if !active {
		return nil
	}
	available := map[string]bool{}
	byName := map[string]providerCandidate{}
	for _, node := range objects {
		candidate, err := candidateFromNode(node)
		if err != nil {
			return err
		}
		if candidate.name == "" || available[candidate.name] || dialerBuiltinNames[candidate.name] {
			return errors.New(msgEmittedNames)
		}
		available[candidate.name] = true
		byName[candidate.name] = candidate
	}
	for name := range dialerBuiltinNames {
		available[name] = true
	}
	graph := newRefGraph()
	candidates := map[string][]providerCandidate{}
	for name, definition := range yamlx.Pairs(definitions) {
		payload, ok := opts.ProviderProxies[name]
		if !ok {
			if opts.RequireProviders {
				return errors.New(msgProviderPayloadMissing)
			}
			continue
		}
		seen := map[string]bool{}
		for _, node := range payload {
			candidate, err := candidateFromNode(node)
			if err != nil {
				return err
			}
			if seen[candidate.name] {
				return errors.New(msgProviderDuplicate)
			}
			seen[candidate.name] = true
		}
		definition = yamlx.Resolve(definition)
		if definition == nil || definition.Kind != yaml.MappingNode {
			return errors.New(msgTopologyShape)
		}
		spec, err := emittedFilter(definition)
		if err != nil {
			return err
		}
		selected, err := FilterProxyNodes(payload, spec)
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			return errors.New(msgProviderFilteredEmpty)
		}
		override, naming, err := emittedOverride(definition)
		if err != nil {
			return err
		}
		for _, proxy := range selected {
			candidate, err := candidateFromNode(proxy)
			if err != nil {
				return err
			}
			candidate.name, err = ProviderName(candidate.name, naming)
			if err != nil {
				return err
			}
			candidate.ref = Ref{"provider", name + "\x00" + candidate.name}
			if dialerBuiltinNames[candidate.name] || available[candidate.name] && naming.Prefix != "" {
				return errors.New(msgProviderConflict)
			}
			target, present := presentTarget(override)
			if !present {
				target, present = presentTarget(definition)
			}
			if !present {
				target, _ = presentTarget(proxy)
			}
			var children []Ref
			if target != nil {
				value := scalarText(target)
				if target.ShortTag() != "!!str" || !available[value] {
					if target.ShortTag() != "!!str" {
						raw, err := yamlx.ToJSON(target)
						if err != nil {
							return err
						}
						value = string(raw)
					}
					return fmt.Errorf(msgProviderTarget, candidate.name, value)
				}
				children = []Ref{{"name", value}}
			}
			graph.set(candidate.ref, children)
			candidates[name] = append(candidates[name], candidate)
		}
	}
	groups := map[string]*yamlx.Node{}
	var groupOrder []Ref
	for _, node := range groupNodes {
		name, err := emittedString(node, "name")
		if err != nil {
			return err
		}
		if _, ok := groups[name]; !ok {
			groupOrder = append(groupOrder, Ref{"name", name})
		}
		groups[name] = node
	}
	for _, node := range objects {
		name, err := emittedString(node, "name")
		if err != nil {
			return err
		}
		names, err := emittedNames(node, "proxies")
		if err != nil {
			return err
		}
		children := []Ref{}
		seen := map[string]bool{}
		for _, child := range names {
			if seen[child] {
				return errors.New(msgGroupCandidatesDuplicate)
			}
			seen[child] = true
			children = append(children, Ref{"name", child})
		}
		for _, child := range names {
			if !available[child] {
				return errors.New(msgEmittedCandidateMissing)
			}
		}
		if _, isGroup := groups[name]; isGroup {
			static := make([]providerCandidate, 0, len(names))
			for _, child := range names {
				candidate, ok := byName[child]
				if !ok {
					candidate = providerCandidate{name: child, kind: child}
				}
				static = append(static, candidate)
			}
			spec, err := emittedFilter(node)
			if err != nil {
				return err
			}
			spec.Include = ""
			kept, err := filterCandidates(static, spec)
			if err != nil {
				return err
			}
			children = nil
			for _, candidate := range kept {
				children = append(children, Ref{"name", candidate.name})
			}
		}
		candidateNames := map[string]bool{}
		for _, child := range children {
			candidateNames[child.ID] = true
		}
		if target, _ := presentTarget(node); target != nil {
			value := scalarText(target)
			if target.ShortTag() != "!!str" || !available[value] {
				return errors.New(msgEmittedTargetMissing)
			}
			children = append(children, Ref{"name", value})
		}
		use, err := emittedNames(node, "use")
		if err != nil {
			return err
		}
		for _, provider := range use {
			if yamlx.Get(definitions, provider) == nil {
				return errors.New(msgEmittedProviderMissing)
			}
			spec, err := emittedFilter(node)
			if err != nil {
				return err
			}
			selected, err := filterCandidates(candidates[provider], spec)
			if err != nil {
				return err
			}
			for _, candidate := range selected {
				if candidateNames[candidate.name] {
					return errors.New(msgGroupCandidatesDuplicate)
				}
				candidateNames[candidate.name] = true
				children = append(children, candidate.ref)
			}
		}
		graph.set(Ref{"name", name}, children)
	}
	if err := checkCycles(graph); err != nil {
		return err
	}
	var pending []Ref
	for _, node := range objects {
		if target, _ := presentTarget(node); target != nil && scalarText(target) != "" {
			pending = append(pending, Ref{"name", target.Value})
		}
	}
	for _, definition := range yamlx.Pairs(definitions) {
		// Unused providers still participate in the reachability scan. Check
		// their shape here, after graph business checks, rather than treating
		// a scalar definition/override as an absent dialer-proxy.
		override, err := emittedProviderOverride(definition)
		if err != nil {
			return err
		}
		if target, _ := presentTarget(override); target != nil && scalarText(target) != "" {
			pending = append(pending, Ref{"name", target.Value})
		}
	}
	if opts.RequireProviders {
		pending = append(pending, groupOrder...)
	}
	seen := map[Ref]bool{}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if group, ok := groups[key.ID]; key.Kind == "name" && ok && len(graph.children[key]) == 0 {
			use, err := emittedNames(group, "use")
			if err != nil {
				return err
			}
			if opts.RequireProviders || len(use) == 0 {
				return errors.New(msgEmittedGroupEmpty)
			}
		}
		pending = append(pending, graph.children[key]...)
	}
	return nil
}

// RejectUnsupportedDialers refuses topology the target client cannot represent.
func RejectUnsupportedDialers(ctx context.Context, config *model.Config, client string, converter NodeConverter) error {
	for _, group := range configGroups(config) {
		if group.Type == "chain" && enabled(group.Enabled) {
			return fmt.Errorf(msgClientChain, client)
		}
	}
	if config == nil {
		return nil
	}
	for _, node := range config.Nodes {
		if !enabled(node.Enabled) {
			continue
		}
		var raw *yamlx.Node
		var err error
		if node.DialerRef == nil {
			raw, err = NodeDialer(node)
			if err != nil {
				return err
			}
		}
		if node.DialerRef != nil || raw != nil {
			return fmt.Errorf(msgClientDialer, client)
		}
		if node.ProxyString != nil && *node.ProxyString != "" {
			proxy, err := NodeProxy(ctx, node, converter)
			if err != nil {
				return err
			}
			if target, _ := presentTarget(proxy); target != nil {
				return fmt.Errorf(msgClientConvertedDialer, client)
			}
		}
	}
	return nil
}

// RequiredNodes returns manual node IDs in the selected dependency closures.
func RequiredNodes(config *model.Config, selected []string, includeRoots bool) (map[string]bool, error) {
	graph, err := BuildDialerGraph(config)
	if err != nil {
		return nil, err
	}
	var pending []Ref
	result := map[string]bool{}
	for _, id := range selected {
		root := Ref{"node", id}
		if includeRoots {
			pending = append(pending, root)
			result[id] = true
		} else {
			pending = append(pending, graph.children[root]...)
		}
	}
	seen := map[Ref]bool{}
	for len(pending) > 0 {
		key := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[key] {
			continue
		}
		seen[key] = true
		if key.Kind == "node" {
			result[key.ID] = true
		}
		pending = append(pending, graph.children[key]...)
	}
	return result, nil
}
