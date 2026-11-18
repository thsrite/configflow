package convert

// TODO(N8-8b/c/d, N8-9): delete these transcoding entry points as callers
// adopt the typed APIs. Business decisions belong to the typed implementation.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"

	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
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
		return nil, nil
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
	config, err := configFromLegacy(value)
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

// readDocumentYAML is only the remaining generator's legacy document boundary.
// N8-8b removes it when generation consumes typed sections and Nodes directly.
func readDocumentYAML(text string) (any, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	if err := yamlx.ProjectJSONKeys(root, yamlx.RejectNonStringKeys); err != nil {
		return nil, err
	}
	return yamlbridge.Value(root)
}

func encodeLegacy(value any) (any, error) {
	data, err := model.Marshal(value)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

func configFromLegacy(value *jsonx.Object) (*model.Config, error) {
	profile, err := decodeLegacy[model.Profile](value)
	if err != nil {
		return nil, err
	}
	shared, err := decodeLegacy[model.Shared](value)
	if err != nil {
		return nil, err
	}
	wire, err := decodeLegacy[model.System](value)
	if err != nil {
		return nil, err
	}
	return &model.Config{Profile: &profile, ProfileID: value.GetString("profile_id"), SystemConfig: wire.SystemConfig, Backup: wire.Backup, Agents: wire.Agents, HasAgents: value.Has("agents"), Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}

// NTPDirectRule adapts the built-in typed rule for remaining legacy callers.
func NTPDirectRule() *jsonx.Object {
	value, _ := encodeLegacy(BuiltinNTPRule())
	result, _ := value.(*jsonx.Object)
	return result
}

// UDPFallback adapts the resolved view to FallbackPolicy.
func UDPFallback(value *jsonx.Object) (string, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
	return FallbackPolicy(config), nil
}

// NTPDirect adapts the resolved view to DirectNTP.
func NTPDirect(value *jsonx.Object) (bool, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return false, err
	}
	return DirectNTP(config), nil
}

// EmittedRules adapts typed emitted rules to the legacy generator boundary.
func EmittedRules(value *jsonx.Object) ([]any, error) {
	config, err := configFromLegacy(value)
	if err != nil {
		return nil, err
	}
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
	config, err := configFromLegacy(value)
	if err != nil {
		return "", err
	}
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
	profile, err := decodeLegacy[*model.Profile](value)
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
	profile, err := decodeLegacy[*model.Profile](value)
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
	for _, key := range []string{"nodes", "proxy_groups"} {
		if raw, present := value.Get(key); present {
			items, err := decodeLegacy[[]json.RawMessage](raw)
			if err != nil || items == nil {
				return nil, &DialerReferenceError{Message: fmt.Sprintf(msgResourceArray, key), cause: err}
			}
			for _, item := range items {
				var object map[string]json.RawMessage
				if err := json.Unmarshal(item, &object); err != nil || object == nil {
					return nil, &DialerReferenceError{Message: msgResourceObject, cause: err}
				}
			}
		}
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
	config, err := configFromLegacy(value)
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
	config, err := configFromLegacy(value)
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
	config, err := configFromLegacy(value)
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
	config, err := configFromLegacy(value)
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
	config, err := configFromLegacy(value)
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
