package convert

// TODO(N8-8c/d, N8-9, N8-10): delete these transcoding entry points as callers
// adopt the typed APIs. Business decisions belong to the typed implementation.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sync"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

func decodeLegacy[T any](value any) (T, error) {
	var result T
	data, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	err = json.Unmarshal(data, &result)
	return result, err
}

// ProxyStringConverter is the unported collaborator boundary. New callers use
// NodeConverter and return owned native nodes directly.
type ProxyStringConverter interface {
	ConvertProxyString(context.Context, string) *jsonx.Object
}

type legacyNodeConverter struct{ converter ProxyStringConverter }

func (c legacyNodeConverter) ConvertProxy(ctx context.Context, text string) (*yamlx.Node, error) {
	if c.converter == nil {
		return nil, &HookMissingError{Hook: "convert_proxy_string", Provider: "convert/mihomoenv"}
	}
	proxy := c.converter.ConvertProxyString(ctx, text)
	if proxy == nil {
		return nil, nil
	}
	return yamlbridge.Encode(proxy)
}

func legacyProxy(node *yamlx.Node) (*jsonx.Object, error) {
	if node == nil {
		return nil, nil
	}
	value, err := yamlbridge.Value(node)
	if err != nil {
		return nil, err
	}
	proxy, _ := value.(*jsonx.Object)
	return proxy, nil
}

func replaceLegacyProxy(target *jsonx.Object, node *yamlx.Node) error {
	proxy, err := legacyProxy(node)
	if err != nil {
		return err
	}
	for _, key := range target.Keys() {
		target.Delete(key)
	}
	for key, value := range proxy.All() {
		target.Set(key, value)
	}
	return nil
}

// FixProxyFields adapts an in-place legacy object to FixProxyNode.
func FixProxyFields(proxy *jsonx.Object) {
	node, err := yamlbridge.Encode(proxy)
	if err != nil {
		return
	}
	FixProxyNode(node)
	if proxy != nil {
		_ = replaceLegacyProxy(proxy, node)
	}
}

// ApplyGithubProxyDomain adapts the resolved view to GitHubURL.
func ApplyGithubProxyDomain(address string, value *jsonx.Object) (string, error) {
	if value == nil {
		return GitHubURL(address, nil)
	}
	config, err := githubConfigFromLegacy(value)
	if err != nil {
		return "", err
	}
	return GitHubURL(address, config)
}

// MihomoUsedPorts adapts the integer port set to the old number-keyed boundary.
func MihomoUsedPorts(value *jsonx.Object) (map[jsonx.Number]bool, error) {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return nil, err
	}
	result := map[jsonx.Number]bool{}
	for port := range UsedPorts(node) {
		result[jsonx.Int(port)] = true
	}
	return result, nil
}

// PortOwner is the legacy representation of a ListeningPort.
type PortOwner struct {
	Port  jsonx.Number
	Value any
	Owner string
}

// MihomoListenPorts adapts native port owners to old generated-settings callers.
func MihomoListenPorts(value *jsonx.Object) ([]PortOwner, error) {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return nil, err
	}
	var result []PortOwner
	for _, port := range ListenPorts(node) {
		number := jsonx.Int(port.Port)
		result = append(result, PortOwner{number, number, port.Owner})
	}
	return result, nil
}

// ParseStructuredProxyString adapts the native structured proxy parser.
func ParseStructuredProxyString(text string) (*jsonx.Object, error) {
	node, err := StructuredProxy(text)
	if err != nil {
		return nil, err
	}
	return legacyProxy(node)
}

// InspectStructuredProxyString applies the same parser as generation.
func InspectStructuredProxyString(text string) (*jsonx.Object, error) {
	node, err := StructuredProxy(text)
	if err != nil {
		return nil, err
	}
	return legacyProxy(node)
}

// ConversionResults adapts the delivery-scoped native conversion record.
type ConversionResults struct{ record Conversions }

// NewConversionResults creates a native record for a legacy caller.
func NewConversionResults() *ConversionResults { return &ConversionResults{} }

// Get decodes a private recorded graph at the legacy boundary.
func (c *ConversionResults) Get(text string) (*jsonx.Object, bool) {
	node, found, err := c.record.Get(text)
	if err != nil {
		return nil, false
	}
	proxy, err := legacyProxy(node)
	return proxy, found && err == nil
}

// Keys lists attempts in insertion order.
func (c *ConversionResults) Keys() []string { return c.record.Keys() }

// Len returns the number of recorded attempts.
func (c *ConversionResults) Len() int { return c.record.Len() }

// WithNodeConversions adapts the old delivery scope to WithConversions.
func WithNodeConversions(ctx context.Context, results *ConversionResults, reuse bool) context.Context {
	var record *Conversions
	if results != nil {
		record = &results.record
	}
	return WithConversions(ctx, record, reuse)
}

// EnsureNodeConversions adapts the old delivery scope to EnsureConversions.
func EnsureNodeConversions(ctx context.Context) context.Context { return EnsureConversions(ctx) }

// ConvertNodeToMihomo adapts one stored node and its collaborator to NodeProxy.
func ConvertNodeToMihomo(ctx context.Context, value *jsonx.Object, converter ProxyStringConverter) (*jsonx.Object, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return nil, err
	}
	proxy, err := NodeProxy(ctx, node, legacyNodeConverter{converter})
	if err != nil {
		return nil, err
	}
	return legacyProxy(proxy)
}

func encodeLegacy(value any) (any, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

// configFromLegacy decodes the complete view at public generation boundaries.
// Metadata helpers below use their own narrow projections; hot per-node and
// per-ruleset paths pass the already decoded native config.
func configFromLegacy(value *jsonx.Object) (*model.Config, error) {
	if value == nil {
		value = jsonx.NewObject()
	}
	// A resolved view is composed of three stored sections. Decode each
	// member only in its owner, rather than retaining the whole view as
	// unknown fields in all three records.
	profileMembers := value.Clone()
	for _, key := range []string{"profile_id", "system_config", "backup", "agents", "nodes", "subscriptions", "subscription_aggregations", "rule_library"} {
		profileMembers.Delete(key)
	}
	profile, err := decodeLegacy[model.Profile](profileMembers)
	if err != nil {
		return nil, err
	}
	shared, err := decodeLegacy[model.Shared](legacyMembers(value, "nodes", "subscriptions", "subscription_aggregations", "rule_library"))
	if err != nil {
		return nil, err
	}
	wire, err := decodeLegacy[model.System](legacyMembers(value, "system_config", "backup", "agents"))
	if err != nil {
		return nil, err
	}
	return &model.Config{Profile: &profile, ProfileID: value.GetString("profile_id"), SystemConfig: wire.SystemConfig, Backup: wire.Backup, Agents: wire.Agents, HasAgents: value.Has("agents"), Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}

// legacyMembers projects only a codec's inputs, preserving absent members.
func legacyMembers(value *jsonx.Object, names ...string) *jsonx.Object {
	selected := jsonx.NewObject()
	for _, name := range names {
		if member, present := value.Get(name); present {
			selected.Set(name, member)
		}
	}
	return selected
}

func rulesConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	profile, err := decodeLegacy[model.Profile](legacyMembers(value, "rule_configs"))
	return &model.Config{Profile: &profile}, err
}

func ruleLibraryConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	shared, err := decodeLegacy[model.Shared](legacyMembers(value, "rule_library"))
	return &model.Config{RuleLibrary: shared.RuleLibrary}, err
}

func githubConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	wire, err := decodeLegacy[model.System](legacyMembers(value, "system_config"))
	return &model.Config{SystemConfig: wire.SystemConfig}, err
}

func dialerConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	profile, err := decodeLegacy[model.Profile](legacyMembers(value, "proxy_groups"))
	if err != nil {
		return nil, err
	}
	shared, err := decodeLegacy[model.Shared](legacyMembers(value, "nodes", "subscriptions", "subscription_aggregations"))
	return &model.Config{Profile: &profile, Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations}, err
}

func easyTierProfileFromLegacy(value any) (*model.Profile, error) {
	if object, ok := value.(*jsonx.Object); ok {
		selected := legacyMembers(object, "mihomo")
		if mihomo, ok := selected.Value("mihomo").(*jsonx.Object); ok {
			selected.Set("mihomo", legacyMembers(mihomo, "easytier"))
		}
		value = selected
	}
	return decodeLegacy[*model.Profile](value)
}

// NTPDirectRule adapts the built-in typed rule for remaining legacy callers.
func NTPDirectRule() *jsonx.Object {
	value, _ := encodeLegacy(BuiltinNTPRule())
	result, _ := value.(*jsonx.Object)
	return result
}

// UDPFallback adapts the resolved view to FallbackPolicy.
func UDPFallback(value *jsonx.Object) (string, error) {
	config, err := rulesConfigFromLegacy(value)
	if err != nil {
		return "", err
	}
	return FallbackPolicy(config), nil
}

// NTPDirect adapts the resolved view to DirectNTP.
func NTPDirect(value *jsonx.Object) (bool, error) {
	config, err := rulesConfigFromLegacy(value)
	if err != nil {
		return false, err
	}
	return DirectNTP(config), nil
}

// EmittedRules adapts typed emitted rules to the legacy generator boundary.
func EmittedRules(value *jsonx.Object) ([]any, error) {
	config, err := rulesConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	return emittedRules(config)
}

func emittedRules(config *model.Config) ([]any, error) {
	encoded, err := encodeLegacy(RulesForOutput(config))
	if err != nil {
		return nil, err
	}
	result, _ := encoded.([]any)
	return result, nil
}

// AfterMatchComment adapts a stored rule to RuleComment.
func AfterMatchComment(value *jsonx.Object) string {
	rule, _ := decodeLegacy[model.RuleConfig](value)
	return RuleComment(rule)
}

// RulesetFormat adapts stored rule records to the shared typed implementation.
func RulesetFormat(rule, value *jsonx.Object, sourceURL string) (string, error) {
	config, err := ruleLibraryConfigFromLegacy(value)
	if err != nil {
		return "", err
	}
	return rulesetFormat(rule, config, sourceURL)
}

func rulesetFormat(rule *jsonx.Object, config *model.Config, sourceURL string) (string, error) {
	item, err := decodeLegacy[model.RuleConfig](rule)
	if err != nil {
		return "", err
	}
	return RuleFormat(item, config, sourceURL)
}

// easyTierFromLegacy only decodes the obsolete polymorphic API boundary.
func easyTierFromLegacy(value any) (*model.EasyTierSettings, error) {
	result, err := decodeLegacy[*model.EasyTierSettings](value)
	if err != nil {
		return nil, easytierError(msgETSettingsObject)
	}
	return result, nil
}
func easyTierLegacyError(err error) error {
	if err == nil {
		return nil
	}
	return easytierError(err.Error())
}

// EasyTierDefaultSettings adapts the typed default to legacy callers.
func EasyTierDefaultSettings() *jsonx.Object {
	value, _ := encodeLegacy(DefaultEasyTier())
	result, _ := value.(*jsonx.Object)
	return result
}

// NormalizeEasyTierSettings decodes, validates and encodes the legacy settings boundary.
func NormalizeEasyTierSettings(value any) (*jsonx.Object, error) {
	settings, err := easyTierFromLegacy(value)
	if err != nil {
		return nil, err
	}
	normalized, err := NormalizeEasyTier(settings)
	if err != nil {
		return nil, easyTierLegacyError(err)
	}
	encoded, err := encodeLegacy(normalized)
	if err != nil {
		return nil, err
	}
	result, _ := encoded.(*jsonx.Object)
	return result, nil
}

// EasyTierSettings adapts the old resolved view to ConfigEasyTier.
func EasyTierSettings(value any) (*jsonx.Object, error) {
	profile, err := easyTierProfileFromLegacy(value)
	if err != nil {
		return nil, err
	}
	settings, err := ConfigEasyTier(&model.Config{Profile: profile})
	if err != nil {
		return nil, easyTierLegacyError(err)
	}
	encoded, err := encodeLegacy(settings)
	if err != nil {
		return nil, err
	}
	result, _ := encoded.(*jsonx.Object)
	return result, nil
}

// EasyTierKeepaliveGroupName adapts the old settings object.
func EasyTierKeepaliveGroupName(value *jsonx.Object) string {
	settings, _ := decodeLegacy[model.EasyTierSettings](value)
	return EasyTierGroupName(&settings)
}

// EasyTierApplies adapts stored settings to the typed delivery predicate.
func EasyTierApplies(value *jsonx.Object, forAgent bool) bool {
	settings, _ := decodeLegacy[model.EasyTierSettings](value)
	return EasyTierFor(&settings, forAgent)
}

// ApplyEasyTier adapts the old document and polymorphic settings/version arguments.
func ApplyEasyTier(document *jsonx.Object, value any, forAgent bool, version any) (*EasyTierResult, error) {
	node, err := yamlbridge.Encode(document)
	if err != nil {
		return nil, err
	}
	profile, err := easyTierProfileFromLegacy(value)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(version)
	if err != nil {
		return nil, err
	}
	result, err := ApplyEasyTierNode(node, &model.Config{Profile: profile}, forAgent, raw)
	if encodeErr := replaceLegacyProxy(document, node); encodeErr != nil {
		return nil, encodeErr
	}
	return result, easyTierLegacyError(err)
}

// RedactEasyTierSettings transcodes old export hooks to the typed redactor.
func RedactEasyTierSettings(value any) any {
	settings, err := decodeLegacy[*model.EasyTierSettings](value)
	if err != nil {
		return value
	}
	redacted, err := RedactEasyTier(settings)
	if err != nil {
		return value
	}
	encoded, err := encodeLegacy(redacted)
	if err != nil {
		return value
	}
	return encoded
}

// dialerLegacyError preserves only the old caller's validation category.
func dialerLegacyError(err error) error {
	if err == nil {
		return nil
	}
	var yamlErr *ProxyYAMLError
	if errors.As(err, &yamlErr) {
		return err
	}
	return &DialerReferenceError{Message: err.Error(), cause: err}
}

// IDValue adapts a native string resource ID for old serialization callers.
func (r Ref) IDValue() any { return r.ID }

// shapeConfigFromLegacy decodes non-null resource arrays strictly at the old
// untyped boundary; model.Config itself can only contain typed resource slices.
func shapeConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	if err := validateLegacyResourceArrays(value); err != nil {
		return nil, err
	}
	return dialerConfigFromLegacy(value)
}

func validateLegacyResourceArrays(value *jsonx.Object) error {
	for _, key := range []string{"nodes", "proxy_groups"} {
		if raw, present := value.Get(key); present {
			items, err := decodeLegacy[[]json.RawMessage](raw)
			if err != nil || items == nil {
				return &DialerReferenceError{Message: fmt.Sprintf(msgResourceArray, key), cause: err}
			}
			for _, item := range items {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil || object == nil {
					return &DialerReferenceError{Message: msgResourceObject, cause: err}
				}
			}
		}
	}
	return nil
}

// clientConfigFromLegacy is a complete public generation boundary. Keeping it
// separate from the metadata-only dialer codec preserves client rules/settings.
func clientConfigFromLegacy(value *jsonx.Object) (*model.Config, error) {
	if err := validateLegacyResourceArrays(value); err != nil {
		return nil, err
	}
	return configFromLegacy(value)
}

// validateGenerationRules only transcodes the legacy generation boundary.
func validateGenerationRules(value *jsonx.Object) error {
	profile, err := decodeLegacy[model.Profile](jsonx.Obj("rule_configs", value.Value("rule_configs")))
	if err != nil {
		return err
	}
	return validateRuleIdentities(profile.RuleConfigs)
}

// ValidateShapes transcodes the old resolved view to ConfigShapes.
func ValidateShapes(value *jsonx.Object, requireIDs bool) error {
	config, err := shapeConfigFromLegacy(value)
	if err != nil {
		return err
	}
	return dialerLegacyError(ConfigShapes(config, requireIDs))
}

// ValidateDialers transcodes the old resolved view to ConfigDialers.
func ValidateDialers(value *jsonx.Object) error {
	config, err := shapeConfigFromLegacy(value)
	if err != nil {
		return err
	}
	return dialerLegacyError(ConfigDialers(config))
}

// DependencyGraph transcodes the old resolved view to BuildDialerGraph.
func DependencyGraph(value *jsonx.Object) (*Graph, error) {
	config, err := shapeConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	graph, err := BuildDialerGraph(config)
	return graph, dialerLegacyError(err)
}

// RawDialer transcodes a typed node's declared dialer.
func RawDialer(value *jsonx.Object) (any, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return nil, err
	}
	target, err := NodeDialer(node)
	if err != nil {
		return nil, dialerLegacyError(err)
	}
	if target == nil {
		return nil, nil
	}
	return yamlbridge.Value(target)
}

// DialerTarget transcodes the view and node to NodeDialerTarget.
func DialerTarget(value, nodeValue *jsonx.Object) (*Ref, error) {
	config, err := dialerConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return nil, err
	}
	ref, err := NodeDialerTarget(config, node)
	return ref, dialerLegacyError(err)
}

// GroupMembers transcodes an old group to GroupRefs.
func GroupMembers(value *jsonx.Object) ([]Ref, error) {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return nil, err
	}
	return GroupRefs(group), nil
}
func groupMembers(value *jsonx.Object) ([]Ref, error) {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return nil, err
	}
	return GroupRefs(group), nil
}

// GroupDependencies transcodes a logical group to GroupSources.
func GroupDependencies(value, groupValue *jsonx.Object) ([]Ref, error) {
	config, err := dialerConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	group, err := decodeLegacy[model.ProxyGroup](groupValue)
	if err != nil {
		return nil, err
	}
	refs, err := GroupSources(config, group)
	return refs, dialerLegacyError(err)
}

// CheckCycles adapts the graph validation category.
func CheckCycles(graph *Graph) error { return dialerLegacyError(CycleCheck(graph)) }

// IncomingDialers transcodes the old view to IncomingRefs.
func IncomingDialers(value *jsonx.Object, kind, id string) ([]Ref, error) {
	config, err := shapeConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	refs, err := IncomingRefs(config, kind, id)
	return refs, dialerLegacyError(err)
}

// ResolveDialer transcodes the old view/node to ResolveNodeDialer.
func ResolveDialer(value, nodeValue *jsonx.Object) (any, error) {
	config, err := dialerConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return nil, err
	}
	name, err := ResolveNodeDialer(config, node)
	if name == nil {
		return nil, dialerLegacyError(err)
	}
	return *name, dialerLegacyError(err)
}

// OverlayDialer preserves the old in-place result at the codec boundary.
func OverlayDialer(value, nodeValue, proxyValue *jsonx.Object) (*jsonx.Object, error) {
	config, err := dialerConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	return overlayDialer(config, nodeValue, proxyValue)
}

// overlayDialer is the codec boundary for one node. A generator can reuse the
// typed view of its stable lowered profile without decoding the whole profile
// for every emitted node. Validation and overlay behaviour remain native.
func overlayDialer(config *model.Config, nodeValue, proxyValue *jsonx.Object) (*jsonx.Object, error) {
	node, err := decodeLegacy[model.Node](nodeValue)
	if err != nil {
		return nil, err
	}
	var proxy *yamlx.Node
	if proxyValue != nil {
		proxy, err = yamlbridge.Encode(proxyValue)
		if err != nil {
			return nil, err
		}
	}
	result, err := ApplyNodeDialer(config, node, proxy)
	if result != nil && proxyValue != nil {
		if writeErr := replaceLegacyProxy(proxyValue, result); writeErr != nil {
			return nil, writeErr
		}
	}
	return proxyValue, dialerLegacyError(err)
}

// FilterProviderCandidates decodes the obsolete dynamically typed filter arguments.
func FilterProviderCandidates(values []*jsonx.Object, include, exclude, excludeType any) ([]*jsonx.Object, error) {
	spec, err := decodeLegacy[FilterSpec](jsonx.Obj("Include", include, "Exclude", exclude, "ExcludeType", excludeType))
	if err != nil {
		return nil, dialerLegacyError(errors.New(msgTopologyShape))
	}
	nodes := make([]*yamlx.Node, 0, len(values))
	for _, value := range values {
		node, err := yamlbridge.Encode(value)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	selected, err := FilterProxyNodes(nodes, spec)
	if err != nil {
		return nil, dialerLegacyError(err)
	}
	result := make([]*jsonx.Object, 0, len(selected))
	for _, node := range selected {
		value, err := legacyProxy(node)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// ProviderProxyName transcodes native typed name overrides.
func ProviderProxyName(proxy, override *jsonx.Object) (string, error) {
	proxyNode, err := yamlbridge.Encode(proxy)
	if err != nil {
		return "", err
	}
	name, err := requiredEmittedString(proxyNode, "name")
	if err != nil {
		return "", dialerLegacyError(errors.New(msgProviderNameRule))
	}
	overrideNode, err := yamlbridge.Encode(override)
	if err != nil {
		return "", err
	}
	naming, err := nameOverrideFromNode(overrideNode)
	if err != nil {
		return "", dialerLegacyError(errors.New(msgProviderNameRule))
	}
	result, err := ProviderName(name, naming)
	return result, dialerLegacyError(err)
}

// EmitOptions is the remaining legacy topology caller's payload representation.
type EmitOptions struct {
	ForceChains      bool
	ProviderProxies  *jsonx.Object
	RequireProviders bool
}

func providerPayloadsFromLegacy(value *jsonx.Object) (map[string][]*yamlx.Node, error) {
	result := map[string][]*yamlx.Node{}
	if value == nil {
		return result, nil
	}
	root, err := yamlbridge.Encode(value)
	if err != nil {
		return nil, err
	}
	for key, source := range yamlx.Pairs(root) {
		source = yamlx.Resolve(source)
		if source == nil || source.Kind != yaml.SequenceNode {
			return nil, errors.New(msgTopologyShape)
		}
		result[key] = source.Content
	}
	return result, nil
}

// ValidateEmitted transcodes generated legacy documents and payloads.
func ValidateEmitted(value *jsonx.Object, opts EmitOptions) error {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return err
	}
	payloads, err := providerPayloadsFromLegacy(opts.ProviderProxies)
	if err != nil {
		return dialerLegacyError(err)
	}
	return dialerLegacyError(ValidateTopology(node, TopologyOptions{ForceChains: opts.ForceChains, RequireProviders: opts.RequireProviders, ProviderProxies: payloads}))
}

// RejectSurgeDialers adapts old collaborators to RejectUnsupportedDialers.
func RejectSurgeDialers(ctx context.Context, value *jsonx.Object, client string, converter ProxyStringConverter) error {
	config, err := dialerConfigFromLegacy(value)
	if err != nil {
		return err
	}
	return dialerLegacyError(RejectUnsupportedDialers(ctx, config, client, legacyNodeConverter{converter}))
}

// DependencyNodeIDs transcodes a resolved view to RequiredNodes.
func DependencyNodeIDs(value *jsonx.Object, selected []string, includeRoots bool) (map[string]bool, error) {
	config, err := shapeConfigFromLegacy(value)
	if err != nil {
		return nil, err
	}
	result, err := RequiredNodes(config, selected, includeRoots)
	return result, dialerLegacyError(err)
}

// ValidateBaseSettings transcodes the old form boundary to native YAML nodes.
func ValidateBaseSettings(document, previous any, reserved map[int]string, localAddresses []any) error {
	root, err := yamlbridge.Encode(document)
	if err != nil {
		return err
	}
	var before *yamlx.Node
	if previous != nil {
		before, err = yamlbridge.Encode(previous)
		if err != nil {
			return err
		}
	}
	hosts := []netip.Addr{}
	for _, raw := range localAddresses {
		value, ok := raw.(string)
		if !ok {
			continue
		}
		address, err := netip.ParseAddr(value)
		if err == nil {
			hosts = append(hosts, address)
		}
	}
	return ValidateBaseNode(root, before, reserved, hosts)
}

// SplitHostPort adapts optional legacy listener text.
func SplitHostPort(value any) (string, int, bool, error) {
	text, err := decodeLegacy[string](value)
	if err != nil {
		return "", 0, false, nil
	}
	host, port, ok := ParseListener(text)
	return host, port, ok, nil
}

// SniffPortError transcodes a legacy scalar to the native port validator.
func SniffPortError(value any) (bool, error) {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return true, err
	}
	return SniffPortInvalid(node), nil
}

// TakeoverMode adapts a resolved legacy configuration.
func TakeoverMode(value *jsonx.Object) (string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
	return GatewayMode(config)
}

// AgentSource is the legacy inventory interface, removed when callers migrate.
type AgentSource interface {
	ProfileAgentIDs(string) ([]any, error)
	AgentByID(any) *jsonx.Object
}

// AgentContext transcodes the selected old inventory to typed Agents.
func AgentContext(source AgentSource, profileID string) (map[int]string, []int, []any) {
	ids, err := source.ProfileAgentIDs(profileID)
	if err != nil {
		return map[int]string{}, nil, []any{}
	}
	records := []model.Agent{}
	for _, id := range ids {
		record := source.AgentByID(id)
		if record == nil {
			continue
		}
		agent, err := decodeLegacy[model.Agent](record)
		if err == nil {
			records = append(records, agent)
		}
	}
	reserved, order, hosts := AgentPorts(records)
	values := make([]any, len(hosts))
	for i, host := range hosts {
		values[i] = host
	}
	return reserved, order, values
}
func registerLegacyMihomoHooks(hooks MihomoHooks) func() {
	native := MihomoIntegrations{FakeIPNetworkError: hooks.FakeIPNetworkError}
	if hooks.ApplyLinuxGateway != nil {
		native.ApplyGateway = func(document *yamlx.Node, config *model.Config, url string) error {
			value, err := encodeLegacy(config)
			if err != nil {
				return err
			}
			logical, _ := value.(*jsonx.Object)
			raw, err := yamlbridge.Value(document)
			if err != nil {
				return err
			}
			main, _ := raw.(*jsonx.Object)
			if err = hooks.ApplyLinuxGateway(main, logical, url); err != nil {
				return err
			}
			result, err := yamlbridge.Encode(main)
			if err != nil {
				return err
			}
			*document = *result
			return nil
		}
	}
	if hooks.GatewayEnabled != nil {
		native.GatewayEnabled = func(config *model.Config) (bool, error) {
			value, err := encodeLegacy(config)
			if err != nil {
				return false, err
			}
			wire, _ := value.(*jsonx.Object)
			return hooks.GatewayEnabled(wire)
		}
	}
	if hooks.IsFullTun != nil {
		native.FullTun = func(config *model.Config) (bool, error) {
			value, err := encodeLegacy(config)
			if err != nil {
				return false, err
			}
			wire, _ := value.(*jsonx.Object)
			return hooks.IsFullTun(wire)
		}
	}
	if hooks.MihomoDNSLoop != nil {
		native.DNSLoop = func(dns *yamlx.Node, addresses []netip.Addr) (string, string, bool, error) {
			value, err := yamlbridge.Value(dns)
			if err != nil {
				return "", "", false, err
			}
			wire, _ := value.(*jsonx.Object)
			key, upstream, found, err := hooks.MihomoDNSLoop(wire, addresses)
			if err != nil {
				return "", "", found, err
			}
			text, err := decodeLegacy[string](upstream)
			return key, text, found, err
		}
	}
	return SetMihomoIntegrations(native)
}

func legacyConfigObject(config *model.Config) (*jsonx.Object, error) {
	value, err := encodeLegacy(config)
	if err != nil {
		return nil, err
	}
	result, _ := value.(*jsonx.Object)
	return result, nil
}

// legacyDialerView contains the complete owned records used by chain, group,
// host and provider collaborators. Unrelated rule/Agent histories are excluded.
func legacyDialerView(config *model.Config) (*jsonx.Object, error) {
	source := struct {
		Groups        []model.ProxyGroup   `json:"proxy_groups"`
		Nodes         []model.Node         `json:"nodes"`
		Subscriptions []model.Subscription `json:"subscriptions"`
		Aggregations  []model.Aggregation  `json:"subscription_aggregations"`
	}{config.Profile.ProxyGroups, config.Nodes, config.Subscriptions, config.Aggregations}
	// The model boundary represents absent native lists as empty arrays.
	if source.Groups == nil {
		source.Groups = []model.ProxyGroup{}
	}
	if source.Nodes == nil {
		source.Nodes = []model.Node{}
	}
	if source.Subscriptions == nil {
		source.Subscriptions = []model.Subscription{}
	}
	if source.Aggregations == nil {
		source.Aggregations = []model.Aggregation{}
	}
	return decodeLegacy[*jsonx.Object](source)
}

func replaceLegacyDocument(document *yamlx.Node, value *jsonx.Object) error {
	result, err := yamlbridge.Encode(value)
	if err != nil {
		return err
	}
	*document = *result
	return nil
}
func adaptDocument(document *yamlx.Node, callback func(*jsonx.Object) error) error {
	value, err := legacyProxy(document)
	if err != nil {
		return err
	}
	if err = callback(value); err != nil {
		return err
	}
	return replaceLegacyDocument(document, value)
}
func legacyCache(data json.RawMessage) (*jsonx.Object, error) {
	if len(data) == 0 {
		return nil, nil
	}
	value, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	result, _ := value.(*jsonx.Object)
	return result, nil
}

// MihomoEnvironmentFromLegacy registers the one temporary codec for each N8-9
// collaborator. No external business implementation is copied into convert.
func MihomoEnvironmentFromLegacy(old MihomoEnv) MihomoEnvironment {
	env := MihomoEnvironment{DownloadedText: old.DownloadedText, Clients: legacyClientCallbacks()}
	if old.Converter != nil {
		env.Converter = legacyNodeConverter{old.Converter}
	}
	if old.ProviderInterval != nil {
		env.ProviderInterval = func(settings model.MihomoSettings) (int64, error) {
			wire, err := decodeLegacy[*jsonx.Object](settings)
			if err != nil {
				return 0, err
			}
			if settings.ProviderUpdateInterval != nil {
				wire.Set("provider_update_interval", int64(*settings.ProviderUpdateInterval))
			}
			return old.ProviderInterval(wire)
		}
	}
	if old.LoadSubscriptionCache != nil {
		env.LoadSubscriptionCache = func(ctx context.Context, id string) (json.RawMessage, error) {
			cache, err := old.LoadSubscriptionCache(ctx, id)
			if err != nil {
				return nil, err
			}
			if cache == nil {
				return nil, nil
			}
			return json.Marshal(cache)
		}
	}
	if old.PrepareProviderBundle != nil {
		env.Preflight = func(ctx context.Context, config *model.Config, document *yamlx.Node) error {
			logical, err := legacyConfigObject(config)
			if err != nil {
				return err
			}
			main, err := legacyProxy(document)
			if err != nil {
				return err
			}
			return old.PrepareProviderBundle(ctx, logical, main)
		}
	}
	env.LowerChains = func(config *model.Config) (*model.Config, error) {
		wire, err := legacyDialerView(config)
		if err != nil {
			return nil, err
		}
		lowered, err := subs.LowerProxyChains(wire)
		if err != nil {
			return nil, subsError(err)
		}
		projection, err := dialerConfigFromLegacy(lowered)
		if err != nil {
			return nil, err
		}
		result := *config
		profile := *config.Profile
		profile.ProxyGroups = projection.Profile.ProxyGroups
		result.Profile = &profile
		result.Nodes, result.Subscriptions, result.Aggregations = projection.Nodes, projection.Subscriptions, projection.Aggregations
		return &result, nil
	}
	env.RenderMembers = func(group model.ProxyGroup, config *model.Config) (*yamlx.Node, error) {
		return rules.RenderGroupDocument(group, rules.GroupResources(config)), nil
	}
	env.ProviderHealth = func(health *model.ProviderHealthCheck) (*yamlx.Node, error) {
		wire, err := decodeLegacy[*jsonx.Object](health)
		if err != nil {
			return nil, err
		}
		result, err := check.ProviderHealthCheck(jsonx.Obj("health_check", wire))
		if err != nil {
			return nil, err
		}
		return yamlbridge.Encode(result)
	}
	env.FilterGroups = func(config *model.Config, document *yamlx.Node) error {
		return rules.LowerProviderFilters(rules.GroupResources(config), document)
	}
	env.ChainProviders = func(config *model.Config, document *yamlx.Node) error {
		logical, err := legacyDialerView(config)
		if err != nil {
			return err
		}
		return adaptDocument(document, func(main *jsonx.Object) error { return subsError(subs.LowerChainProviders(logical, main)) })
	}
	env.MergeHosts = func(ctx context.Context, document *yamlx.Node, config *model.Config, userKeys, mosdnsKeys []string, inline []InlineNode, load CacheReader) (*HostsResult, error) {
		main, err := legacyProxy(document)
		if err != nil {
			return nil, err
		}
		logical, err := legacyDialerView(config)
		if err != nil {
			return nil, err
		}
		values := make([]subs.InlineProxy, 0, len(inline))
		for _, item := range inline {
			node, err := decodeLegacy[*jsonx.Object](item.Node)
			if err != nil {
				return nil, err
			}
			proxy, err := legacyProxy(item.Proxy)
			if err != nil {
				return nil, err
			}
			values = append(values, subs.InlineProxy{Node: node, Proxy: proxy})
		}
		merged := jsonx.NewObject()
		var failure error
		report := subs.MergeSubscriptionHosts(main, logical, userKeys, mosdnsKeys, values, merged, func(sub *jsonx.Object) *subs.HostsIndex {
			if load == nil {
				failure = &HookMissingError{Hook: "load_subscription_cache", Provider: "convert/mihomoenv"}
				return nil
			}
			data, err := load(ctx, sub.GetString("id"))
			if err != nil {
				failure = err
				return nil
			}
			cache, err := legacyCache(data)
			if err != nil {
				failure = err
				return nil
			}
			if cache == nil {
				return nil
			}
			return subs.NewHostsIndex(cache).ForURL(sub)
		})
		if failure != nil {
			return nil, failure
		}
		if err = replaceLegacyDocument(document, main); err != nil {
			return nil, err
		}
		reportNode, err := yamlbridge.Encode(report)
		if err != nil {
			return nil, err
		}
		mergedNode, err := yamlbridge.Encode(merged)
		if err != nil {
			return nil, err
		}
		return &HostsResult{Report: reportNode, Merged: mergedNode}, nil
	}
	env.NodeDNS = func(document, merged *yamlx.Node, version string) (*yamlx.Node, error) {
		main, err := legacyProxy(document)
		if err != nil {
			return nil, err
		}
		hosts, err := legacyProxy(merged)
		if err != nil {
			return nil, err
		}
		report := subs.ApplyNodeDNS(main, hosts, version)
		if err = replaceLegacyDocument(document, main); err != nil {
			return nil, err
		}
		if report == nil {
			return nil, nil
		}
		return yamlbridge.Encode(report)
	}
	env.LogHosts = func(profileID string, report *yamlx.Node) error {
		wire, err := legacyProxy(report)
		if err != nil {
			return err
		}
		subs.LogReport(profileID, wire)
		return nil
	}
	env.DialerGroups = func(ctx context.Context, document *yamlx.Node, config *model.Config, deps DialerCollaborators) (*yamlx.Node, error) {
		main, err := legacyProxy(document)
		if err != nil {
			return nil, err
		}
		logical, err := legacyDialerView(config)
		if err != nil {
			return nil, err
		}
		var failure error
		report, err := subs.AddDialerGroups(main, logical, subs.DialerDeps{
			LoadCache: func(id string) *jsonx.Object {
				if deps.LoadCache == nil {
					failure = &HookMissingError{Hook: "load_subscription_cache", Provider: "convert/mihomoenv"}
					return nil
				}
				raw, err := deps.LoadCache(ctx, id)
				if err != nil {
					failure = err
					return nil
				}
				wire, err := legacyCache(raw)
				if err != nil {
					failure = err
				}
				return wire
			},
			ConvertNode: func(value *jsonx.Object) *jsonx.Object {
				if deps.Convert == nil {
					failure = &HookMissingError{Hook: "convert_node", Provider: "convert/mihomoenv"}
					return nil
				}
				node, err := decodeLegacy[model.Node](value)
				if err != nil {
					failure = err
					return nil
				}
				proxy, err := deps.Convert(ctx, node)
				if err != nil {
					failure = err
					return nil
				}
				wire, err := legacyProxy(proxy)
				if err != nil {
					failure = err
				}
				return wire
			},
			DownloadedText: func(url string) (string, bool) {
				if deps.DownloadedText == nil {
					failure = &HookMissingError{Hook: "downloaded_text", Provider: "convert/mihomoenv"}
					return "", false
				}
				return deps.DownloadedText(ctx, url)
			},
		})
		if failure != nil {
			return nil, failure
		}
		if err != nil {
			return nil, subsError(err)
		}
		if err = replaceLegacyDocument(document, main); err != nil {
			return nil, err
		}
		if report == nil {
			return nil, nil
		}
		return yamlbridge.Encode(report)
	}
	env.ValidateRules = func(config *model.Config, names []string, client string) error {
		return rules.ValidatePolicies(config.Profile.RuleConfigs, names, client)
	}
	env.ValidateListeners = func(document *yamlx.Node) error {
		wire, err := legacyProxy(document)
		if err != nil {
			return err
		}
		return subs.ValidateListenerPolicies(wire)
	}
	return env
}

// GenerateOptions are the keyword arguments of generate_mihomo_config.
type GenerateOptions struct {
	// BaseURL is base_url (used when system_config.server_domain is empty).
	BaseURL string
	// SyncLANHosts is sync_lan_hosts: add the MosDNS custom hosts (Agent pushes only).
	SyncLANHosts bool
	// SkipProviderPreflight is preflight_providers=False.
	SkipProviderPreflight bool
	// RegionProbe is region_probe=True.
	RegionProbe bool
	// Report, when non-nil, receives subscription_hosts / subscription_dialers.
	Report *jsonx.Object
	// MihomoCoreVersion is mihomo_core_version (nil = None).
	MihomoCoreVersion any
	// ForAgent is for_agent=True.
	ForAgent bool
}

// GenerateMihomoConfig adapts a resolved legacy profile to the typed generator.
func GenerateMihomoConfig(ctx context.Context, value *jsonx.Object, options GenerateOptions) (string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
	native, report, err := legacyMihomoOptions(options)
	if err != nil {
		return "", err
	}
	text, err := MihomoConfig(ctx, config, native)
	if copyErr := copyMihomoReport(options.Report, report); err == nil {
		err = copyErr
	}
	return text, err
}

// GenerateMihomoDocument adapts the documented generated-text readback boundary.
func GenerateMihomoDocument(ctx context.Context, value *jsonx.Object, options GenerateOptions) (*jsonx.Object, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, err
	}
	native, report, err := legacyMihomoOptions(options)
	if err != nil {
		return nil, err
	}
	text, err := MihomoConfig(ctx, config, native)
	if copyErr := copyMihomoReport(options.Report, report); err == nil {
		err = copyErr
	}
	if err != nil {
		return nil, err
	}
	node, err := readMihomoBase(text)
	if err != nil {
		return nil, err
	}
	return legacyProxy(node)
}
func legacyMihomoOptions(options GenerateOptions) (MihomoOptions, *GenerationReport, error) {
	var version json.RawMessage
	if options.MihomoCoreVersion != nil {
		raw, err := json.Marshal(options.MihomoCoreVersion)
		if err != nil {
			return MihomoOptions{}, nil, err
		}
		version = raw
	}
	var report *GenerationReport
	if options.Report != nil {
		report = &GenerationReport{}
	}
	return MihomoOptions{BaseURL: options.BaseURL, SyncLANHosts: options.SyncLANHosts, SkipProviderPreflight: options.SkipProviderPreflight, RegionProbe: options.RegionProbe, ForAgent: options.ForAgent, CoreVersion: version, Report: report}, report, nil
}
func copyMihomoReport(target *jsonx.Object, report *GenerationReport) error {
	if target == nil || report == nil {
		return nil
	}
	if report.SubscriptionHosts != nil {
		wire, err := legacyProxy(report.SubscriptionHosts)
		if err != nil {
			return err
		}
		target.Set("subscription_hosts", wire)
	}
	if report.SubscriptionDialers != nil {
		wire, err := legacyProxy(report.SubscriptionDialers)
		if err != nil {
			return err
		}
		target.Set("subscription_dialers", wire)
	}
	return nil
}

// DefaultMihomoTemplate adapts the native built-in document.
func DefaultMihomoTemplate() *jsonx.Object { value, _ := legacyProxy(MihomoTemplate()); return value }

// ValidateIPv6Flags adapts free-form legacy configuration input.
func ValidateIPv6Flags(value any) error {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return err
	}
	return ValidateIPv6Node(node)
}

// NormalizeFindProcessMode adapts an old mutable document.
func NormalizeFindProcessMode(value *jsonx.Object) {
	_ = adaptDocumentValue(value, NormalizeProcessMode)
}
func adaptDocumentValue(value *jsonx.Object, fn func(*yamlx.Node) error) error {
	node, err := yamlbridge.Encode(value)
	if err != nil {
		return err
	}
	if err = fn(node); err != nil {
		return err
	}
	wire, err := legacyProxy(node)
	if err != nil {
		return err
	}
	for _, key := range value.Keys() {
		value.Delete(key)
	}
	value.Update(wire)
	return nil
}

// ParseMosdnsCustomHosts adapts the native hosts mapping.
func ParseMosdnsCustomHosts(text string) *jsonx.Object {
	value, _ := legacyProxy(mosdnsHosts(text))
	return value
}

// SyncMosdnsHosts adapts the old mutable generation helper.
func SyncMosdnsHosts(document, logical *jsonx.Object) error {
	config, err := configFromLegacy(logical)
	if err != nil {
		return err
	}
	return adaptDocumentValue(document, func(node *yamlx.Node) error { return syncMihomoHosts(node, config) })
}

// InjectRegionProbe adapts an old integer and mutable document.
func InjectRegionProbe(document *jsonx.Object, port jsonx.Number) (bool, error) {
	value, ok := port.Int64()
	if !ok {
		return false, fmt.Errorf("%s", msgRegionPort)
	}
	var injected bool
	err := adaptDocumentValue(document, func(node *yamlx.Node) error { var err error; injected, err = addRegionProbe(node, value); return err })
	return injected, err
}

// BuildProviderHealthCheck adapts one legacy provider record.
func BuildProviderHealthCheck(source *jsonx.Object) (*jsonx.Object, error) {
	record, err := decodeLegacy[model.Subscription](source)
	if err != nil {
		return nil, err
	}
	node, err := providerHealthNode(record.HealthCheck, record.HealthCheckURL, MihomoEnvironmentFromLegacy(CurrentMihomoEnv()))
	if err != nil {
		return nil, err
	}
	return legacyProxy(node)
}

// SplitRulesAndRulesets adapts the typed split for remaining clients.
func SplitRulesAndRulesets(value *jsonx.Object) (any, any, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, nil, err
	}
	a, b, err := MihomoRuleLists(config)
	if err != nil {
		return nil, nil, err
	}
	left, err := encodeLegacy(a)
	if err != nil {
		return nil, nil, err
	}
	right, err := encodeLegacy(b)
	return left, right, err
}
func splitRulesAndRulesets(value *jsonx.Object) (any, any) {
	a, b, err := SplitRulesAndRulesets(value)
	if err != nil {
		raise(err)
	}
	return a, b
}
func profileAPIPath(value *jsonx.Object, suffix string) string {
	config, err := configFromLegacy(value)
	if err != nil {
		return ""
	}
	return profilePath(config, suffix)
}

// MihomoEnv holds the config-domain collaborators of generation.
type MihomoEnv struct {
	// Converter is Sub-Store's convert_proxy_string (substore.Converter()).
	Converter ProxyStringConverter
	// ProviderInterval is validate_refresh_interval(
	// mihomo.get('provider_update_interval', DEFAULT_PROVIDER_INTERVAL),
	// 'Mihomo 订阅同步间隔') (C15 refresh) for the profile's `mihomo` object.
	ProviderInterval func(mihomo *jsonx.Object) (int64, error)
	// LoadSubscriptionCache is subscription_cache.load_subscription_cache
	// (C13 substore): nil when there is no cache.
	LoadSubscriptionCache func(ctx context.Context, subID string) (*jsonx.Object, error)
	// DownloadedText is the `text` of the shared download cache of a URL
	// (subscription_dialers.dialer_record's fallback read; C15 refresh's
	// _download_cache_path through the repository); ok false when it cannot
	// be read or holds no text.
	DownloadedText func(ctx context.Context, url string) (string, bool)
	// PrepareProviderBundle is provider_delivery.prepare_provider_bundle(
	// logical_config, mihomo_config) with default options (C17 provider).
	PrepareProviderBundle func(ctx context.Context, config, main *jsonx.Object) error
}

// MihomoHooks are the functions of other domains the generator and the base
// settings checks call. Register them with RegisterMihomoHooks from the
// providing module; a nil field keeps the default described on it.
type MihomoHooks struct {
	// ApplyLinuxGateway is common/linux_gateway.apply_linux_gateway(mihomo,
	// config, base_url), which first calls routers' router_split
	// .check_generation(config). Provided by agents A19/A21 (Linux gateway)
	// together with routers (router_split).
	// Default: a no-op when config.mihomo has no `linux_gateway` (or it is
	// null) and no `router_split` marker — exactly where Python does nothing;
	// otherwise *HookMissingError.
	ApplyLinuxGateway func(mihomo, config *jsonx.Object, baseURL string) error
	// GatewayEnabled is linux_gateway.gateway_settings(config)['enabled']
	// (agents A19/A21); the error is the LinuxGatewayError of a malformed
	// setting. Default: false when config.mihomo has no `linux_gateway` or
	// it is null; otherwise *HookMissingError.
	GatewayEnabled func(config *jsonx.Object) (bool, error)
	// FakeIPNetworkError is linux_gateway.fake_ip_network({'dns':
	// {'fake-ip-range': value}}) for a string value: nil when Python returns,
	// non-nil when it raises LinuxGatewayError (agents A19/A21).
	// Default: *HookMissingError.
	FakeIPNetworkError func(value string) error
	// MihomoDNSLoop is gateway_onboarding.mihomo_dns_loop(dns, host_addresses)
	// (agents, gateway onboarding): the first (key, upstream) that resolves
	// through this host. Default: *HookMissingError.
	MihomoDNSLoop func(dns *jsonx.Object, hostAddresses []netip.Addr) (key string, upstream any, found bool, err error)
	// IsFullTun is router_split.is_full_tun(config) (routers, clients/split).
	// Default: false when config.mihomo has no `router_split` marker;
	// otherwise *HookMissingError.
	IsFullTun func(config *jsonx.Object) (bool, error)
}

var (
	mihomoMu  sync.RWMutex
	mihomoEnv MihomoEnv
)

// SetMihomoEnv installs the config-domain collaborators and returns a
// function that restores the previous ones (for tests).
func SetMihomoEnv(env MihomoEnv) (restore func()) {
	restoreNative := SetMihomoEnvironment(MihomoEnvironmentFromLegacy(env))
	mihomoMu.Lock()
	previous := mihomoEnv
	mihomoEnv = env
	mihomoMu.Unlock()
	return func() {
		restoreNative()
		mihomoMu.Lock()
		mihomoEnv = previous
		mihomoMu.Unlock()
	}
}

// CurrentMihomoEnv returns the installed collaborators.
func CurrentMihomoEnv() MihomoEnv {
	mihomoMu.RLock()
	defer mihomoMu.RUnlock()
	return mihomoEnv
}

// RegisterMihomoHooks sets the non-nil fields of hooks and returns a function
// that restores the previous hooks (for tests).
func RegisterMihomoHooks(hooks MihomoHooks) func() { return registerLegacyMihomoHooks(hooks) }

func applyLinuxGateway(main, logical *jsonx.Object, url string) error {
	config, err := configFromLegacy(logical)
	if err != nil {
		return err
	}
	return adaptDocumentValue(main, func(node *yamlx.Node) error { return applyGateway(node, config, url) })
}
func gatewayEnabled(value *jsonx.Object) (bool, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return false, err
	}
	return gatewayActive(config)
}
func isFullTun(value *jsonx.Object) (bool, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return false, err
	}
	return fullTunActive(config)
}

// Download is one entry of get_mihomo_provider_downloads /
// get_mihomo_ruleset_downloads: {'name', 'url', 'local_path'}.
type Download struct {
	Name      string
	URL       any
	LocalPath any
	// Behavior and Format carry the final consumer metadata for deployment builds.
	// Object deliberately retains the existing public download representation.
	Behavior string `json:"behavior,omitempty"`
	Format   string `json:"format,omitempty"`
}

// Object is the Python dict of the entry.
func (d Download) Object() *jsonx.Object {
	return jsonx.Obj("name", d.Name, "url", d.URL, "local_path", d.LocalPath)
}

func legacyDownloads(values []ResourceDownload, err error) ([]Download, error) {
	if err != nil {
		return nil, err
	}
	result := make([]Download, 0, len(values))
	for _, value := range values {
		result = append(result, Download{Name: value.Name, URL: value.URL, LocalPath: value.LocalPath, Behavior: value.Behavior, Format: value.Format})
	}
	return result, nil
}

// GetMihomoProviderDownloads transcodes a resolved profile and optional document.
func GetMihomoProviderDownloads(ctx context.Context, value *jsonx.Object, baseURL string, main *jsonx.Object) ([]Download, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, err
	}
	var document *yamlx.Node
	if main != nil {
		document, err = yamlbridge.Encode(main)
		if err != nil {
			return nil, err
		}
	}
	return legacyDownloads(MihomoProviderDownloads(ctx, config, baseURL, document))
}

// GetMihomoRulesetDownloads transcodes the old ruleset download boundary.
func GetMihomoRulesetDownloads(ctx context.Context, value *jsonx.Object, baseURL string) ([]Download, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, err
	}
	return legacyDownloads(MihomoRulesetDownloads(ctx, config, baseURL))
}

// nativeClientConverter is removed with the Surge/Loon adapters in N8-8c.
func subsError(err error) error {
	var reference *subs.DialerReferenceError
	if errors.As(err, &reference) {
		return &DialerReferenceError{Message: reference.Message, cause: err}
	}
	return err
}

// Temporary client codecs disappear as N8-8c consumers use the typed entries.
// ConvertNodeToSurge is the single codec for the old node entry.
func ConvertNodeToSurge(value *jsonx.Object) (string, string, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return "", "", err
	}
	return SurgeNode(node)
}

// ConvertNodeToLoon is the single codec for the old node entry.
func ConvertNodeToLoon(value *jsonx.Object) (string, error) {
	node, err := decodeLegacy[model.Node](value)
	if err != nil {
		return "", err
	}
	return LoonNode(node)
}
func legacyClientCallbacks() ClientCollaborators {
	return ClientCollaborators{
		ParseNodes: subscriptionSources,
		FilterNodes: func(nodes []model.Node, include, exclude *string) ([]model.Node, error) {
			return rules.Filter(nodes, include, exclude, func(node model.Node) string { return node.Name })
		},
	}
}

// ConvertProxyGroupToSurge transcodes the old group and resolved snapshot.
func ConvertProxyGroupToSurge(value any, source *jsonx.Object, base string, smart *jsonx.Object) (string, error) {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return "", err
	}
	config, err := configFromLegacy(source)
	if err != nil {
		return "", err
	}
	priorities, err := decodeLegacy[map[string]string](smart)
	if err != nil {
		return "", err
	}
	return SurgeGroup(group, config, base, priorities)
}

// ConvertProxyGroupToLoon transcodes the old group to LoonGroup.
func ConvertProxyGroupToLoon(value any, members []string) (string, error) {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return "", err
	}
	return LoonGroup(group, members)
}

// BuildLoonRules transcodes the old resolved view to LoonRules.
func BuildLoonRules(value *jsonx.Object, base string) ([]string, []string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, nil, err
	}
	return LoonRules(config, base)
}

// GenerateSurgeConfig transcodes to the typed Surge generator.
func GenerateSurgeConfig(ctx context.Context, value *jsonx.Object, base string) (string, error) {
	config, err := clientConfigFromLegacy(value)
	if err != nil {
		return "", err
	}
	return SurgeConfig(ctx, config, base)
}

// GenerateLoonConfig transcodes to the typed Loon generator.
func GenerateLoonConfig(ctx context.Context, value *jsonx.Object, base string) (string, error) {
	config, err := clientConfigFromLegacy(value)
	if err != nil {
		return "", err
	}
	return LoonConfig(ctx, config, base)
}

// SanitizeLoonName transcodes an old string entry to the native name formatter.
func SanitizeLoonName(value any) (string, error) {
	text, err := decodeLegacy[string](value)
	if err != nil {
		return "", err
	}
	return clientSanitizeName(text), nil
}

// GetProxyGroupNodes transcodes stored group membership to the typed filter.
func GetProxyGroupNodes(value any, source *jsonx.Object) ([]any, error) {
	group, err := decodeLegacy[model.ProxyGroup](value)
	if err != nil {
		return nil, err
	}
	config, err := configFromLegacy(source)
	if err != nil {
		return nil, err
	}
	names, err := ClientGroupNodes(group, config)
	if err != nil {
		return nil, err
	}
	result, err := encodeLegacy(names)
	if err != nil {
		return nil, err
	}
	return result.([]any), nil
}
func legacyClientDecoder(decode func([]any) ([]any, error)) ClientNodeDecoder {
	if decode == nil {
		return nil
	}
	return func(proxies []*yamlx.Node) ([]model.Node, error) {
		values := make([]any, len(proxies))
		for i, proxy := range proxies {
			value, err := legacyProxy(proxy)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		nodes, err := decode(values)
		if err != nil {
			return nil, err
		}
		return decodeLegacy[[]model.Node](nodes)
	}
}
func legacyClientProxies(proxies []any) ([]*yamlx.Node, error) {
	result := make([]*yamlx.Node, len(proxies))
	for i, value := range proxies {
		node, err := yamlbridge.Encode(value)
		if err != nil {
			return nil, err
		}
		result[i] = node
	}
	return result, nil
}

// ConvertProxiesToSurgeText transcodes the N8-9 substore callback in one place.
func ConvertProxiesToSurgeText(proxies []any, decode func([]any) ([]any, error)) (string, error) {
	nodes, err := legacyClientProxies(proxies)
	if err != nil {
		return "", err
	}
	return ClientProxyText(nodes, legacyClientDecoder(decode), "surge")
}

// ConvertProxiesToLoonText transcodes the N8-9 substore callback in one place.
func ConvertProxiesToLoonText(proxies []any, decode func([]any) ([]any, error)) (string, error) {
	nodes, err := legacyClientProxies(proxies)
	if err != nil {
		return "", err
	}
	return ClientProxyText(nodes, legacyClientDecoder(decode), "loon")
}
