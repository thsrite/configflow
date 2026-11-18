package subs

// Legacy entry points transcode the old API; native owners never call them.
import (
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"context"
	"encoding/json"
	"math/big"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err == nil {
		err = json.Unmarshal(data, &result)
	}
	return result, err
}
func encodeLegacy(value any) (*jsonx.Object, error) {
	data, err := jsonresp.Encode(value)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	result, _ := decoded.(*jsonx.Object)
	return result, nil
}
func legacyNode(value any) (*yamlx.Node, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}

// RelinkGroupListeners adapts a profile once and copies back only the owned member.
func RelinkGroupListeners(profile *jsonx.Object, oldName string, newName *string) ([]string, error) {
	native, err := decodeLegacy[model.Profile](profile)
	if err != nil {
		return nil, err
	}
	changes, err := RelinkListeners(&native, oldName, newName)
	if err != nil || len(changes) == 0 {
		return changes, err
	}
	settings := profile.GetObject("mihomo")
	if settings == nil {
		settings = jsonx.NewObject()
		profile.Set("mihomo", settings)
	}
	settings.Set("custom_config", native.Mihomo.CustomConfig)
	return changes, nil
}

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

// FilterSubscriptionItems transcodes the subscription and complete proxy batch once.
func FilterSubscriptionItems(value any, items []any) []any {
	sub, err := decodeLegacy[model.Subscription](value)
	if err != nil {
		sub = model.Subscription{}
	}
	graph, err := legacyNode(items)
	if err != nil {
		return nil
	}
	selected := FilterProxies(sub, graph.Content)
	if selected == nil {
		return nil
	}
	data, err := yamlx.ToJSON(yamlx.Seq(selected...))
	if err != nil {
		return nil
	}
	decoded, err := jsonx.Parse(data)
	if err != nil {
		return nil
	}
	out, _ := decoded.([]any)
	return out
}

// ParseURIList transcodes a parsed batch for unported node consumers.
func ParseURIList(source string) []*jsonx.Object {
	parsed := ParseList(source)
	result := make([]*jsonx.Object, 0, len(parsed))
	for _, item := range parsed {
		if item.Raw != nil {
			data, err := yamlx.ToJSON(item.Raw)
			if err != nil {
				continue
			}
			decoded, err := jsonx.Parse(data)
			if err != nil {
				continue
			}
			if object, ok := decoded.(*jsonx.Object); ok {
				object.Set("_raw_object", true)
				result = append(result, object)
			}
		} else {
			value, err := encodeLegacy(item.Node)
			if err == nil {
				result = append(result, value)
			}
		}
	}
	return result
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

// StatusLoader is the unported traffic store boundary.
type StatusLoader func(agentID string) *jsonx.Object

func legacyStatusLoader(load StatusLoader) CoreStatusLoader {
	if load == nil {
		return nil
	}
	return func(id string) CoreStatus { value, _ := decodeLegacy[CoreStatus](load(id)); return value }
}

// ReportedCoreVersion adapts one legacy agent and its owning status loader.
func ReportedCoreVersion(agent *jsonx.Object, load StatusLoader) string {
	native, err := decodeLegacy[CoreAgent](agent)
	if err != nil {
		return ""
	}
	return AgentCoreVersion(native, legacyStatusLoader(load))
}

// OutdatedAgents adapts a legacy batch and returns preview DTOs.
func OutdatedAgents(agents []*jsonx.Object, profileID, defaultID string, load StatusLoader) []any {
	native, err := decodeLegacy[[]CoreAgent](agents)
	if err != nil {
		return []any{}
	}
	reports := OldCoreAgents(native, profileID, defaultID, legacyStatusLoader(load))
	out := make([]any, 0, len(reports))
	for _, report := range reports {
		item, err := encodeLegacy(report)
		if err == nil {
			out = append(out, item)
		}
	}
	return out
}

func domainOf(value any) string  { text, _ := value.(string); return hostDomain(text) }
func addressOf(value any) string { text, _ := value.(string); return usableAddress(text) }

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

// HostsIndex is the old cache-index API; its data is owned by the native index.
type HostsIndex struct{ native *HostIndex }

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

// HostsIndexOf is the old refresh cache callback.
type HostsIndexOf func(*jsonx.Object) *HostsIndex

// InlineProxy is the old inline delivery pair.
type InlineProxy struct {
	Node  *jsonx.Object
	Proxy *jsonx.Object
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

// DialerDeps is the old API's typed-collaborator codec boundary.
type DialerDeps struct {
	LoadCache      func(string) *jsonx.Object
	ConvertNode    func(*jsonx.Object) *jsonx.Object
	DownloadedText func(string) (string, bool)
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
