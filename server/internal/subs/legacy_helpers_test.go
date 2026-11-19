package subs

import (
	"context"
	"math/big"
	"strconv"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// Archived assertions use JSON objects; all parsing remains in shared yamlx.
func readSubscriptionYAML(source string, stringKeysOnly bool) (any, error) {
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	mode := yamlx.RejectNonStringKeys
	if stringKeysOnly {
		mode = yamlx.DiscardNonStringKeys
	}
	if err := yamlx.ProjectJSONKeys(node, mode); err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func getOr(value *jsonx.Object, key string, fallback any) any {
	if item, present := value.Get(key); present {
		return item
	}
	return fallback
}
func isString(value any) bool { _, ok := value.(string); return ok }

// Archived JSON fixtures adapt only at this test boundary to the native owners.
// ValidateListenerPolicies adapts the open engine graph once.
func ValidateListenerPolicies(document *jsonx.Object) error {
	native, err := legacyNode(document)
	if err != nil {
		return err
	}
	return ValidateListenerNode(native)
}

// ValidateRegexFilter retains the strict request token boundary before native validation.
func ValidateRegexFilter(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return msgFilterType
	}
	return ValidateFilter(text)
}

// CompileSubscriptionFilter adapts one stored subscription record.
func CompileSubscriptionFilter(value any) Matcher {
	sub, err := decodeLegacy[model.Subscription](value)
	if err != nil {
		return nil
	}
	return CompileFilter(sub)
}

// ParseYAMLObject adapts one open structured node at the old boundary.
func ParseYAMLObject(source string) *jsonx.Object {
	graph := ParseObject(source)
	if graph == nil {
		return nil
	}
	data, err := yamlx.ToJSON(graph)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	object, _ := value.(*jsonx.Object)
	if object != nil {
		object.Set("_raw_object", true)
	}
	return object
}

// ListenerAddress adapts a legacy listener scalar.
func ListenerAddress(value any) (string, string) { text, _ := value.(string); return DNSListener(text) }

// ParseCoreVersion adapts a legacy version scalar.
func ParseCoreVersion(value any) ([3]*big.Int, bool) {
	text, _ := value.(string)
	return CoreVersion(text)
}

// CoreTooOld adapts a legacy version scalar.
func CoreTooOld(value any) bool { text, _ := value.(string); return IsCoreTooOld(text) }

// FinalTarget adapts an open hosts graph for callers not yet migrated.
func FinalTarget(hosts *jsonx.Object, domain string) string {
	graph, err := legacyNode(hosts)
	if err != nil {
		return ""
	}
	return NewHostResolver(graph).Target(domain)
}

// ApplyNodeDNS adapts each complete open document once and copies back only DNS.
func ApplyNodeDNS(main, merged *jsonx.Object, version string) *jsonx.Object {
	graph, err := legacyNode(main)
	if err != nil {
		return nil
	}
	hosts, err := legacyNode(merged)
	if err != nil {
		return nil
	}
	report, err := ApplyDNS(graph, hosts, version)
	if err != nil || report == nil {
		return nil
	}
	dns := yamlx.Get(graph, "dns")
	if dns != nil {
		data, err := yamlx.ToJSON(dns)
		if err != nil {
			return nil
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			return nil
		}
		main.Set("dns", value)
	}
	out, _ := encodeLegacy(report)
	return out
}

// ExtractSubscriptionHosts adapts typed metadata for the unported refresh owner.
func ExtractSubscriptionHosts(text string) *jsonx.Object {
	record := ExtractHosts(text)
	if record == nil {
		return nil
	}
	out, _ := encodeLegacy(record)
	return out
}

// HostsDigest adapts an old metadata value to the native D2 identity.
func HostsDigest(value *jsonx.Object) string {
	record, err := decodeLegacy[*HostsData](value)
	if err != nil {
		return ""
	}
	digest, _ := HostDigest(record)
	return digest
}

// DescribeChange adapts two old host metadata values.
func DescribeChange(previous, current *jsonx.Object) *jsonx.Object {
	old, err := decodeLegacy[*HostsData](previous)
	if err != nil {
		return nil
	}
	next, err := decodeLegacy[*HostsData](current)
	if err != nil {
		return nil
	}
	change, err := CompareHosts(old, next)
	if err != nil {
		return nil
	}
	out, _ := encodeLegacy(change)
	return out
}

// legacyConfig decodes each owning section once at old public boundaries.
func legacyConfig(value *jsonx.Object) (*model.Config, error) {
	if value == nil {
		value = jsonx.NewObject()
	}
	profileMembers := value.Clone()
	for _, name := range []string{"profile_id", "system_config", "backup", "agents", "nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		profileMembers.Delete(name)
	}
	profile, err := decodeLegacy[model.Profile](profileMembers)
	if err != nil {
		return nil, err
	}
	selected := jsonx.NewObject()
	for _, name := range []string{"nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		if member, present := value.Get(name); present {
			selected.Set(name, member)
		}
	}
	shared, err := decodeLegacy[model.Shared](selected)
	if err != nil {
		return nil, err
	}
	return &model.Config{Profile: &profile, ProfileID: value.GetString("profile_id"), Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}

// NewHostsIndex adapts a single cache, without ever decoding configuration.
func NewHostsIndex(value *jsonx.Object) *HostsIndex {
	cache, err := decodeLegacy[*SubscriptionCache](value)
	if err != nil {
		return nil
	}
	index, err := NewHostIndex(cache)
	if err != nil || index == nil {
		return nil
	}
	return &HostsIndex{native: index}
}

// ForURL adapts the current source URL.
func (h *HostsIndex) ForURL(value *jsonx.Object) *HostsIndex {
	if h == nil {
		return nil
	}
	sub, err := decodeLegacy[model.Subscription](value)
	if err != nil {
		return nil
	}
	index := h.native.ForSubscription(sub)
	if index == nil {
		return nil
	}
	return &HostsIndex{native: index}
}

// MergeSubscriptionHosts adapts the full config once, then only transcodes collaborators.
func MergeSubscriptionHosts(main, config *jsonx.Object, userKeys, mosdnsKeys []string, inline []InlineProxy, mergedOut *jsonx.Object, load HostsIndexOf) *jsonx.Object {
	graph, err := legacyNode(main)
	if err != nil {
		return nil
	}
	native, err := legacyConfig(config)
	if err != nil {
		return nil
	}
	selected := make([]HostInline, 0, len(inline))
	for _, item := range inline {
		node, err := decodeLegacy[model.Node](item.Node)
		if err != nil {
			return nil
		}
		proxy, err := legacyNode(item.Proxy)
		if err != nil {
			return nil
		}
		selected = append(selected, HostInline{node, proxy})
	}
	var loader HostIndexLoader
	if load != nil {
		loader = func(sub model.Subscription) (*HostIndex, error) {
			wire, err := encodeLegacy(sub)
			if err != nil {
				return nil, err
			}
			index := load(wire)
			if index == nil {
				return nil, nil
			}
			return index.native, nil
		}
	}
	report, merged, err := MergeHosts(graph, native, userKeys, mosdnsKeys, selected, loader)
	if err != nil {
		return nil
	}
	if report.Merged > 0 {
		data, err := yamlx.ToJSON(yamlx.Get(graph, "hosts"))
		if err != nil {
			return nil
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			return nil
		}
		main.Set("hosts", value)
	}
	if mergedOut != nil {
		data, err := yamlx.ToJSON(merged)
		if err != nil {
			return nil
		}
		value, err := jsonx.Parse(data)
		if err != nil {
			return nil
		}
		mergedOut.Update(value.(*jsonx.Object))
	}
	out, _ := encodeLegacy(report)
	return out
}

// Matches adapts a legacy domain matcher scalar.
func Matches(value any, domain string) bool {
	text, ok := value.(string)
	return ok && hostPatternMatches(hostPatternText(text), domain)
}

// LogReport adapts the old preview DTO.
func LogReport(id string, value *jsonx.Object) {
	report, err := decodeLegacy[*HostReport](value)
	if err == nil {
		_ = LogHostReport(id, report)
	}
}

// ExtractSubscriptionDialers transcodes typed source metadata for unported refresh.
func ExtractSubscriptionDialers(text string) (*jsonx.Object, error) {
	record, err := ExtractDialers(text)
	if err != nil || record == nil {
		return nil, err
	}
	return encodeLegacy(record)
}

// AdaptSubscriptionProxies transcodes a complete batch once, without config decoding.
func AdaptSubscriptionProxies(subscription string, proxies []any, main, record *jsonx.Object, chains bool) []any {
	graph, err := legacyNode(proxies)
	if err != nil {
		return nil
	}
	document, err := legacyNode(main)
	if err != nil {
		return nil
	}
	metadata, err := decodeLegacy[*DialersData](record)
	if err != nil {
		return nil
	}
	selected, err := AdaptProxies(subscription, graph.Content, document, metadata, chains)
	if err != nil {
		return nil
	}
	data, err := yamlx.ToJSON(yamlx.Seq(selected...))
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	out, _ := value.([]any)
	return out
}

// DialerRecord adapts the old cache/read collaborator at its public boundary.
func DialerRecord(sub, cache *jsonx.Object, downloaded func(string) (string, bool)) (*jsonx.Object, error) {
	source, err := decodeLegacy[model.Subscription](sub)
	if err != nil {
		return nil, err
	}
	loaded, err := decodeLegacy[*SubscriptionCache](cache)
	if err != nil {
		return nil, err
	}
	var read func(context.Context, string) (string, bool)
	if downloaded != nil {
		read = func(_ context.Context, url string) (string, bool) { return downloaded(url) }
	}
	record, err := SourceDialers(context.Background(), source, loaded, read)
	if err != nil || record == nil {
		return nil, err
	}
	return encodeLegacy(record)
}

// AddDialerGroups adapts the config once and each collaborator's own record only.
func AddDialerGroups(main, config *jsonx.Object, deps DialerDeps) (*jsonx.Object, error) {
	graph, err := legacyNode(main)
	if err != nil {
		return nil, err
	}
	native, err := legacyConfig(config)
	if err != nil {
		return nil, err
	}
	inputs := DialerInputs{}
	if deps.LoadCache != nil {
		inputs.LoadCache = func(_ context.Context, id string) (*SubscriptionCache, error) {
			return decodeLegacy[*SubscriptionCache](deps.LoadCache(id))
		}
	}
	if deps.ConvertNode != nil {
		inputs.Convert = func(_ context.Context, node model.Node) (*yamlx.Node, error) {
			wire, err := encodeLegacy(node)
			if err != nil {
				return nil, err
			}
			return legacyNode(deps.ConvertNode(wire))
		}
	}
	if deps.DownloadedText != nil {
		inputs.DownloadedText = func(_ context.Context, url string) (string, bool) { return deps.DownloadedText(url) }
	}
	report, failure := AddGroups(context.Background(), graph, native, inputs)
	// Business failures expose the same generated prefix through this old API.
	for _, field := range []string{"proxies", "proxy-groups"} {
		if value := yamlx.Get(graph, field); value != nil {
			data, err := yamlx.ToJSON(value)
			if err != nil {
				return nil, err
			}
			decoded, err := jsonx.Parse(data)
			if err != nil {
				return nil, err
			}
			main.Set(field, decoded)
		}
	}
	if failure != nil {
		return nil, failure
	}
	if report == nil {
		return nil, nil
	}
	return encodeLegacy(report)
}

// ValidateProxyChains adapts one old resolved view to the sole chain validator.
func ValidateProxyChains(value *jsonx.Object) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	if err := ValidateChains(config); err != nil {
		return dre(err.Error())
	}
	return nil
}

// LowerProxyChains adapts a view once and copies back only owned runtime fields.
func LowerProxyChains(value *jsonx.Object) (*jsonx.Object, error) {
	config, err := legacyConfig(value)
	if err != nil {
		return nil, err
	}
	runtime, err := LowerChains(config)
	if err != nil {
		return nil, dre(err.Error())
	}
	if runtime == config {
		return value, nil
	}
	out := value.DeepCopy()
	groupJSON, err := model.Marshal(runtime.Profile.ProxyGroups)
	if err != nil {
		return nil, err
	}
	groups, err := jsonx.Parse(groupJSON)
	if err != nil {
		return nil, err
	}
	nodeJSON, err := model.Marshal(runtime.Nodes)
	if err != nil {
		return nil, err
	}
	nodes, err := jsonx.Parse(nodeJSON)
	if err != nil {
		return nil, err
	}
	out.Set("proxy_groups", groups)
	out.Set("nodes", nodes)
	return out, nil
}

// LowerChainProviders adapts one runtime view and copies the owned prefix back.
func LowerChainProviders(value, main *jsonx.Object) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	document, err := legacyNode(main)
	if err != nil {
		return err
	}
	failure := ChainProviders(config, document)
	for _, field := range []string{"proxy-providers", "proxy-groups"} {
		node := yamlx.Get(document, field)
		if node == nil {
			main.Delete(field)
			continue
		}
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return err
		}
		decoded, err := jsonx.Parse(data)
		if err != nil {
			return err
		}
		main.Set(field, decoded)
	}
	return referenceFailure(failure)
}

// HostsIndex is the old cache-index API; its data is owned by the native index.
type HostsIndex struct{ native *HostIndex }

// HostsIndexOf is the old refresh cache callback.
type HostsIndexOf func(*jsonx.Object) *HostsIndex

// InlineProxy is the old inline delivery pair.
type InlineProxy struct {
	Node  *jsonx.Object
	Proxy *jsonx.Object
}

// DialerDeps is the old API's typed-collaborator codec boundary.
type DialerDeps struct {
	LoadCache      func(string) *jsonx.Object
	ConvertNode    func(*jsonx.Object) *jsonx.Object
	DownloadedText func(string) (string, bool)
}

func parseStructuredProxyString(text string) *jsonx.Object {
	ops := sharedDialers()
	if ops.Structured == nil {
		return nil
	}
	node, err := ops.Structured(text)
	if err != nil || node == nil {
		return nil
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	out, _ := value.(*jsonx.Object)
	return out
}
func rawDialer(value *jsonx.Object) (any, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.NodeDialer == nil {
		return nil, &CollaborationError{Operation: "node dialer"}
	}
	target, err := ops.NodeDialer(node)
	if err != nil {
		return nil, referenceFailure(err)
	}
	if target == nil {
		return nil, nil
	}
	data, err := yamlx.ToJSON(target)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
func validateShapes(value *jsonx.Object, ids bool) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	ops := sharedDialers()
	if ops.Shapes == nil {
		return &CollaborationError{Operation: "resource shapes"}
	}
	return referenceFailure(ops.Shapes(config, ids))
}

// ValidateDialers adapts one view to the sole native dependency validator.
func ValidateDialers(value *jsonx.Object) error {
	config, err := legacyConfig(value)
	if err != nil {
		return err
	}
	ops := sharedDialers()
	if ops.Validate == nil {
		return &CollaborationError{Operation: "dialer validation"}
	}
	return referenceFailure(ops.Validate(config))
}

type ref struct{ kind, id string }

func legacyRefs(values []DependencyRef) []ref {
	out := make([]ref, 0, len(values))
	for _, value := range values {
		out = append(out, ref{value.Kind, value.ID})
	}
	return out
}
func groupMembers(value *jsonx.Object) []ref {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return nil
	}
	ops := sharedDialers()
	if ops.Members == nil {
		return nil
	}
	return legacyRefs(ops.Members(group))
}
func groupDependencies(configValue, groupValue *jsonx.Object) ([]ref, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return nil, err
	}
	group, err := decodeLegacy[model.ProxyGroup](groupValue)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.Sources == nil {
		return nil, &CollaborationError{Operation: "group sources"}
	}
	refs, err := ops.Sources(config, group)
	return legacyRefs(refs), referenceFailure(err)
}
func dialerTarget(configValue, nodeValue *jsonx.Object) (*ref, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return nil, err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.NodeTarget == nil {
		return nil, &CollaborationError{Operation: "dialer target"}
	}
	target, err := ops.NodeTarget(config, node)
	if target == nil {
		return nil, referenceFailure(err)
	}
	return &ref{target.Kind, target.ID}, referenceFailure(err)
}

type depGraph struct {
	order []ref
	m     map[ref][]ref
}

func dependencyGraph(value *jsonx.Object) (*depGraph, error) {
	config, err := legacyConfig(value)
	if err != nil {
		return nil, err
	}
	ops := sharedDialers()
	if ops.Graph == nil {
		return nil, &CollaborationError{Operation: "dialer graph"}
	}
	graph, err := ops.Graph(config)
	if err != nil {
		return nil, referenceFailure(err)
	}
	if graph == nil {
		return nil, &CollaborationError{Operation: "dialer graph result"}
	}
	out := &depGraph{order: legacyRefs(graph.Order), m: map[ref][]ref{}}
	for key, children := range graph.Edges {
		out.m[ref{key.Kind, key.ID}] = legacyRefs(children)
	}
	return out, nil
}
func resolveDialer(configValue, nodeValue *jsonx.Object) (string, error) {
	config, err := legacyConfig(configValue)
	if err != nil {
		return "", err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return "", err
	}
	ops := sharedDialers()
	if ops.Resolve == nil {
		return "", &CollaborationError{Operation: "dialer resolution"}
	}
	name, err := ops.Resolve(config, node)
	if name == nil {
		return "", referenceFailure(err)
	}
	return *name, referenceFailure(err)
}

// Candidate is the old filter boundary. Raw is never inspected by the native owner.
type Candidate struct {
	Name, Type string
	Raw        any
}

func filterProviderCandidates(values []Candidate, include, exclude, excludeType string) ([]Candidate, error) {
	ops := sharedDialers()
	if ops.Filter == nil {
		return nil, &CollaborationError{Operation: "provider filtering"}
	}
	native := make([]FilterCandidate, 0, len(values))
	for index, value := range values {
		native = append(native, FilterCandidate{Name: value.Name, Type: value.Type, Key: strconv.Itoa(index)})
	}
	selected, err := ops.Filter(native, include, exclude, excludeType)
	if err != nil {
		return nil, referenceFailure(err)
	}
	out := make([]Candidate, 0, len(selected))
	for _, value := range selected {
		index, err := strconv.Atoi(value.Key)
		if err != nil {
			return nil, err
		}
		if index < 0 || index >= len(values) {
			return nil, &CollaborationError{Operation: "provider filtering result"}
		}
		out = append(out, values[index])
	}
	return out, nil
}
