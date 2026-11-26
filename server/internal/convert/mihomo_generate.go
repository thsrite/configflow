package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// CustomConfigYAMLError wraps a native base YAML diagnostic with public context.
type CustomConfigYAMLError struct {
	Message string
	cause   error
}

func (e *CustomConfigYAMLError) Error() string { return e.Message }

// Unwrap exposes syntax, policy and alias-budget errors.
func (e *CustomConfigYAMLError) Unwrap() error { return e.cause }

// GenerationReport carries the generator-owned diagnostic sections.
type GenerationReport struct{ SubscriptionHosts, SubscriptionDialers *yamlx.Node }

// MihomoOptions controls typed generation without an untyped argument boundary.
type MihomoOptions struct {
	BaseURL                                                    string
	SyncLANHosts, SkipProviderPreflight, RegionProbe, ForAgent bool
	CoreVersion                                                json.RawMessage
	Report                                                     *GenerationReport
}

// LogicRuleTypes is LOGIC_RULE_TYPES.
var LogicRuleTypes = []string{"AND", "OR", "NOT"}

var resolveTriggerRuleTypes = []string{"IP-CIDR", "IP-CIDR6", "IP-SUFFIX", "GEOIP", "IP-ASN"}

// ApplyNoResolveToLogicRule is apply_no_resolve_to_logic_rule: no-resolve
// is added inside each destination-IP sub-condition `( ... )` of a logic
// rule (re.sub(r'\(([^()]*)\)', ...)).
func ApplyNoResolveToLogicRule(value string) string {
	var out strings.Builder
	i := 0
	for i < len(value) {
		if value[i] == '(' {
			j := i + 1
			for j < len(value) && value[j] != '(' && value[j] != ')' {
				j++
			}
			if j < len(value) && value[j] == ')' {
				inner := value[i+1 : j]
				out.WriteString(injectNoResolve(inner, value[i:j+1]))
				i = j + 1
				continue
			}
		}
		out.WriteByte(value[i])
		i++
	}
	return out.String()
}

func injectNoResolve(inner, whole string) string {
	parts := strings.Split(inner, ",")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	if !slices.Contains(resolveTriggerRuleTypes, strings.ToUpper(parts[0])) {
		return whole
	}
	for _, p := range parts {
		if strings.ToLower(p) == "no-resolve" {
			return whole
		}
	}
	return "(" + inner + ",no-resolve)"
}

// DefaultHealthCheckURL is the compatibility probe used for absent legacy settings.
const DefaultHealthCheckURL = "https://www.gstatic.com/generate_204"

// MihomoTemplate returns an independently owned built-in base document.
func MihomoTemplate() *yamlx.Node {
	filter := yamlx.Seq()
	for _, domain := range subs.ConnectivityFilter {
		filter.Content = append(filter.Content, yamlx.Str(domain))
	}
	filter.Content = append(filter.Content, yamlx.Str("+.pool.ntp.org"))
	return yamlx.Map(yamlx.Str("mixed-port"), yamlx.Int(7890), yamlx.Str("allow-lan"), yamlx.Bool(true), yamlx.Str("bind-address"), yamlx.Str("*"), yamlx.Str("mode"), yamlx.Str("rule"), yamlx.Str("log-level"), yamlx.Str("info"), yamlx.Str("external-controller"), yamlx.Str("127.0.0.1:9090"), yamlx.Str("dns"), yamlx.Map(
		yamlx.Str("enable"), yamlx.Bool(true), yamlx.Str("listen"), yamlx.Str("0.0.0.0:53"), yamlx.Str("enhanced-mode"), yamlx.Str("fake-ip"), yamlx.Str("fake-ip-range"), yamlx.Str("198.18.0.1/16"), yamlx.Str("fake-ip-filter"), filter,
		yamlx.Str("nameserver"), stringSequence([]string{"223.5.5.5", "119.29.29.29"}), yamlx.Str("fallback"), stringSequence([]string{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"})))
}
func stringSequence(values []string) *yamlx.Node {
	node := yamlx.Seq()
	for _, value := range values {
		node.Content = append(node.Content, yamlx.Str(value))
	}
	return node
}
func readMihomoBase(text string) (*yamlx.Node, error) {
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	node, err = projectReadGraph(node, yamlx.RejectNonStringKeys)
	if err != nil {
		return nil, err
	}

	return node, nil
}
func customMihomoBase(text string) (*yamlx.Node, error) {
	if text == model.DamagedText {
		return nil, &CustomConfigYAMLError{Message: fmt.Sprintf(msgCustomConfigNotText, "mihomo")}
	}
	if strings.TrimSpace(text) == "" {
		return MihomoTemplate(), nil
	}
	node, err := readMihomoBase(text)
	if err != nil {
		return nil, &CustomConfigYAMLError{Message: msgCustomYAMLInvalid + err.Error(), cause: err}
	}
	if node.Kind != yaml.MappingNode {
		// Keep the existing public shape diagnostic; this is a validation label,
		// not dynamic language dispatch.
		labels := map[string]string{"!!seq": "list", "!!null": "NoneType", "!!str": "str", "!!bool": "bool", "!!int": "int", "!!float": "float"}
		label := labels[node.ShortTag()]
		if label == "" {
			label = node.ShortTag()
		}
		return nil, &CustomConfigYAMLError{Message: msgCustomYAMLMapping + label}
	}

	return node, nil
}

// ValidateIPv6Node rejects non-boolean flags in an open generated document.
func ValidateIPv6Node(document *yamlx.Node) error {
	for _, setting := range []struct {
		node    *yamlx.Node
		message string
	}{{yamlx.Get(document, "ipv6"), msgIPv6Boolean}, {basePath(document, []string{"dns", "ipv6"}), msgDNSIPv6Boolean}} {
		if setting.node != nil {
			if _, ok := boolNode(setting.node); !ok {
				return &CustomConfigYAMLError{Message: setting.message}
			}
		}
	}
	return nil
}

// NormalizeProcessMode normalizes only the supported bool/text representations.
func NormalizeProcessMode(document *yamlx.Node) error {
	node := yamlx.Get(document, "find-process-mode")
	if value, ok := boolNode(node); ok {
		mode := "off"
		if value {
			mode = "always"
		}
		yamlx.Set(document, "find-process-mode", yamlx.Str(mode))
	} else if text, ok := textNode(node); ok {
		mode := strings.ToLower(strings.TrimSpace(text))
		if slices.Contains(FindProcessModes, mode) {
			yamlx.Set(document, "find-process-mode", yamlx.Str(mode))
		}
	}
	return nil
}

// MihomoRuleLists returns enabled rules and rule sets, including typed legacy lists.
func MihomoRuleLists(config *model.Config) ([]model.RuleConfig, []model.RuleConfig, error) {
	rules, sets := []model.RuleConfig{}, []model.RuleConfig{}
	if config == nil || config.Profile == nil {
		return rules, sets, fmt.Errorf("%s", msgMihomoConfig)
	}
	if len(config.Profile.RuleConfigs) == 0 {
		if raw, ok := config.Profile.LegacyRuleList("rules"); ok {
			if err := decodeRuleList(raw, &rules); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", msgMihomoRules, err)
			}
		}
		if raw, ok := config.Profile.LegacyRuleList("rule_sets"); ok {
			if err := decodeRuleList(raw, &sets); err != nil {
				return nil, nil, fmt.Errorf("%s: %w", msgMihomoRules, err)
			}
		}
		return rules, sets, nil
	}
	for _, rule := range config.Profile.RuleConfigs {
		if !enabled(rule.Enabled) || !libraryEnabled(rule.LibraryEnabled) {
			continue
		}
		switch rule.ItemType {
		case "rule":
			rules = append(rules, rule)
		case "ruleset":
			sets = append(sets, rule)
		}
	}
	return rules, sets, nil
}

// decodeRuleList applies R5 to each record in a legacy open list. Non-record
// members have no rule fields and are omitted; surrounding records survive.
func decodeRuleList(data json.RawMessage, output *[]model.RuleConfig) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}
	for _, item := range items {
		var rule model.RuleConfig
		if err := json.Unmarshal(item, &rule); err != nil {
			continue
		}
		*output = append(*output, rule)
	}
	return nil
}

func providerHealthNode(check *model.ProviderHealthCheck, address *string, env MihomoEnvironment) (*yamlx.Node, error) {
	if check != nil {
		if env.ProviderHealth == nil {
			return nil, &HookMissingError{Hook: "provider_health_check", Provider: "health/check"}
		}
		return env.ProviderHealth(check)
	}
	url := strings.TrimSpace(stringValue(address))
	if url == "" {
		url = DefaultHealthCheckURL
	}
	return yamlx.Map(yamlx.Str("enable"), yamlx.Bool(true), yamlx.Str("url"), yamlx.Str(url), yamlx.Str("interval"), yamlx.Int(300), yamlx.Str("lazy"), yamlx.Bool(true)), nil
}
func mosdnsHosts(text string) *yamlx.Node {
	result := yamlx.Map()
	for _, pair := range ParseHostsPairs(text) {
		yamlx.Set(result, pair.Domain, yamlx.Str(pair.Address))
	}
	return result
}
func mappingKeys(node *yamlx.Node) []string {
	var keys []string
	for key := range yamlx.Pairs(node) {
		keys = append(keys, key)
	}
	return keys
}
func syncMihomoHosts(document *yamlx.Node, config *model.Config) error {
	synced := mosdnsHosts(config.Profile.MosDNS.CustomHosts)
	if len(synced.Content) == 0 {
		return nil
	}
	hosts := yamlx.Resolve(yamlx.Get(document, "hosts"))
	if hosts == nil || hosts.Kind != yaml.MappingNode {
		hosts = yamlx.Map()
	}
	for key, value := range yamlx.Pairs(synced) {
		if yamlx.Get(hosts, key) == nil {
			yamlx.Set(hosts, key, value)
		}
	}
	yamlx.Set(document, "hosts", hosts)
	dns := yamlx.Resolve(yamlx.Get(document, "dns"))
	if dns != nil && dns.Kind == yaml.MappingNode && yamlx.Get(dns, "use-hosts") == nil {
		yamlx.Set(dns, "use-hosts", yamlx.Bool(true))
	}
	return nil
}
func profilePath(config *model.Config, suffix string) string {
	if config.ProfileID != "" {
		return "/api/profiles/" + config.ProfileID + suffix
	}
	return suffix
}
func followed(groups []model.ProxyGroup, id string) *model.ProxyGroup {
	for i := range groups {
		if groups[i].ID == id {
			return &groups[i]
		}
	}
	return nil
}

type mihomoSelection struct{ nodes, subscriptions, aggregations, direct, aggregationRoots map[string]bool }

func mihomoUsed(config *model.Config) *mihomoSelection {
	selected := &mihomoSelection{map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}}
	for _, node := range config.Nodes {
		if node.ProxyChainID != "" {
			selected.nodes[node.ID] = true
		}
	}
	for _, group := range config.Profile.ProxyGroups {
		if ref := group.ChainDialer; ref != nil && ref.Type == "node" {
			selected.nodes[ref.ID] = true
		}
	}
	for _, group := range config.Profile.ProxyGroups {
		if !enabled(group.Enabled) {
			continue
		}
		source := &group
		if stringValue(group.FollowGroup) != "" {
			source = followed(config.Profile.ProxyGroups, *group.FollowGroup)
			if source == nil {
				continue
			}
		}
		for _, id := range stringList(source.ManualNodes) {
			if id != "DIRECT" && id != "REJECT" {
				selected.nodes[id] = true
				selected.direct[id] = true
			}
		}
		covered := map[string]bool{}
		for _, id := range stringList(source.Aggregations) {
			selected.aggregations[id] = true
			for _, agg := range config.Aggregations {
				if agg.ID == id && enabled(agg.Enabled) {
					for _, sub := range agg.Subscriptions {
						covered[sub] = true
					}
					break
				}
			}
		}
		for _, id := range stringList(source.Subscriptions) {
			if !covered[id] {
				selected.subscriptions[id] = true
			}
		}
		for _, ref := range refList(group.ProxiesOrder) {
			if ref.Type == "node" && ref.ID != "DIRECT" && ref.ID != "REJECT" {
				selected.nodes[ref.ID] = true
			}
		}
		for _, ref := range refList(source.ProxiesOrder) {
			if ref.Type == "node" && ref.ID != "DIRECT" && ref.ID != "REJECT" {
				selected.direct[ref.ID] = true
			}
		}
	}
	for _, group := range config.Profile.ProxyGroups {
		if enabled(group.Enabled) {
			for _, ref := range GroupRefs(group) {
				if ref.Kind == "node" && ref.ID != "DIRECT" && ref.ID != "REJECT" {
					selected.nodes[ref.ID] = true
					selected.direct[ref.ID] = true
				}
			}
		}
	}
	for _, agg := range config.Aggregations {
		if enabled(agg.Enabled) && selected.aggregations[agg.ID] {
			for _, id := range agg.Nodes {
				selected.aggregationRoots[id] = true
			}
		}
	}
	return selected
}
func orderedRoots(config *model.Config, selection *mihomoSelection) []string {
	var result []string
	seen := map[string]bool{}
	for _, node := range config.Nodes {
		if (selection.nodes[node.ID] || selection.aggregationRoots[node.ID]) && !seen[node.ID] {
			result = append(result, node.ID)
			seen[node.ID] = true
		}
	}
	return result
}
func groupEngineOptions(target *yamlx.Node, group model.ProxyGroup) error {
	// Each option is read as its own member, not from a whole-group encode (O10 R9).
	for _, key := range []string{"url", "interval", "tolerance", "lazy", "strategy", "timeout", "max-failed-times", "expected-status", "disable-udp", "hidden", "icon", "exclude-filter", "exclude-type"} {
		value, err := groupMember(&group, key)
		if err != nil {
			return err
		}
		if value != nil {
			yamlx.Set(target, key, value)
		}
	}
	return nil
}

// groupMember is a group's stored member as a YAML node, or nil when absent.
func groupMember(group *model.ProxyGroup, key string) (*yamlx.Node, error) {
	raw, present, err := model.EncodeMember(group, key)
	if err != nil || !present {
		return nil, err
	}
	return yamlx.FromJSON(raw)
}
func groupDefaults(target *yamlx.Node, group model.ProxyGroup) error {
	if stringValue(group.URL) != "" {
		yamlx.Set(target, "url", yamlx.Str(*group.URL))
	}
	if group.Interval != nil && *group.Interval != 0 {
		// A followed group may omit interval. In that case the local stored
		// override survives with its YAML scalar type, including quoted text.
		interval, err := groupMember(&group, "interval")
		if err != nil {
			return err
		}
		yamlx.Set(target, "interval", interval)
	}
	if group.Type == "url-test" {
		yamlx.Set(target, "tolerance", yamlx.Int(100))
		yamlx.Set(target, "lazy", yamlx.Bool(true))
	}
	if group.Type == "load-balance" {
		if stringValue(group.Strategy) != "" {
			yamlx.Set(target, "strategy", yamlx.Str(*group.Strategy))
		}
		if group.Lazy != nil {
			yamlx.Set(target, "lazy", yamlx.Bool(*group.Lazy))
		}
	}
	return nil
}
func mihomoGroups(config *model.Config, proxies *yamlx.Node, env MihomoEnvironment) (*yamlx.Node, error) {
	result := yamlx.Seq()
	for _, group := range config.Profile.ProxyGroups {
		if !enabled(group.Enabled) {
			continue
		}
		target := yamlx.Map(yamlx.Str("name"), yamlx.Str(group.Name), yamlx.Str("type"), yamlx.Str(group.Type))
		settings := &group
		if id := stringValue(group.FollowGroup); id != "" {
			settings = followed(config.Profile.ProxyGroups, id)
			if settings == nil {
				continue
			}
			yamlx.Set(target, "type", yamlx.Str(settings.Type))
		}
		members, err := env.RenderMembers(*settings, config)
		if err != nil {
			return nil, err
		}
		for key, value := range yamlx.Pairs(members) {
			yamlx.Set(target, key, value)
		}
		if settings != &group {
			if err := groupDefaults(target, *settings); err != nil {
				return nil, err
			}
		}
		if err := groupDefaults(target, group); err != nil {
			return nil, err
		}
		if err := groupEngineOptions(target, *settings); err != nil {
			return nil, err
		}
		kind, _ := textNode(yamlx.Get(target, "type"))
		if kind != "url-test" {
			yamlx.Delete(target, "tolerance")
		}
		result.Content = append(result.Content, target)
	}
	if len(result.Content) == 0 {
		chained := false
		for _, node := range config.Nodes {
			if node.ProxyChainID != "" {
				chained = true
				break
			}
		}
		if !chained {
			names := []string{}
			for _, proxy := range proxies.Content {
				if name, ok := textNode(yamlx.Get(proxy, "name")); ok {
					names = append(names, name)
				}
			}
			if len(names) > 0 {
				result = yamlx.Seq(yamlx.Map(yamlx.Str("name"), yamlx.Str("PROXY"), yamlx.Str("type"), yamlx.Str("select"), yamlx.Str("proxies"), stringSequence(append([]string{"Auto"}, names...))), yamlx.Map(yamlx.Str("name"), yamlx.Str("Auto"), yamlx.Str("type"), yamlx.Str("url-test"), yamlx.Str("proxies"), stringSequence(names), yamlx.Str("url"), yamlx.Str(DefaultHealthCheckURL), yamlx.Str("interval"), yamlx.Int(300), yamlx.Str("tolerance"), yamlx.Int(100), yamlx.Str("lazy"), yamlx.Bool(true)))
			}
		}
	}
	return result, nil
}
func mihomoRuleLines(config *model.Config) (*yamlx.Node, error) {
	lines := []string{}
	for _, rule := range RulesForOutput(config) {
		if !enabled(rule.Enabled) || !libraryEnabled(rule.LibraryEnabled) {
			continue
		}
		policy := stringValue(rule.Policy)
		switch rule.ItemType {
		case "rule":
			switch {
			case rule.RuleType == "MATCH":
				lines = append(lines, "MATCH,"+policy)
			case rule.RuleType == "RULE-SET":
				lines = append(lines, "RULE-SET,"+stringValue(rule.Value)+","+policy)
			default:
				value := stringValue(rule.Value)
				logic := slices.Contains(LogicTypes, rule.RuleType)
				if logic && rule.NoResolve != nil && *rule.NoResolve {
					value = ApplyNoResolveToLogicRule(value)
				}
				line := rule.RuleType + "," + value + "," + policy
				if !logic && rule.NoResolve != nil && *rule.NoResolve {
					line += ",no-resolve"
				}
				lines = append(lines, line)
			}
		case "ruleset":
			if rule.Policy == nil {
				policy = "PROXY"
			}
			line := "RULE-SET," + rule.Name + "," + policy
			if rule.NoResolve != nil && *rule.NoResolve {
				line += ",no-resolve"
			}
			lines = append(lines, line)
		}
	}
	if FallbackPolicy(config) == "REJECT" {
		lines = append(lines, "NETWORK,udp,REJECT")
	}
	return stringSequence(lines), nil
}

// MihomoDocument builds an owned output graph from one typed resolved snapshot.
func MihomoDocument(ctx context.Context, logical *model.Config, options MihomoOptions) (*yamlx.Node, error) {
	ctx = EnsureConversions(ctx)
	if logical == nil || logical.Profile == nil {
		return nil, fmt.Errorf("%s", msgMihomoConfig)
	}
	env := CurrentMihomoEnvironment()
	if err := env.CheckGeneration(); err != nil {
		return nil, err
	}
	if err := ConfigDialers(logical); err != nil {
		return nil, &DialerReferenceError{Message: err.Error(), cause: err}
	}
	config, err := env.LowerChains(logical)
	if err != nil {
		return nil, err
	}
	if config == nil || config.Profile == nil {
		return nil, fmt.Errorf("%s", msgMihomoConfig)
	}
	_, ruleSets, err := MihomoRuleLists(config)
	if err != nil {
		return nil, err
	}
	document, err := customMihomoBase(config.Profile.Mihomo.CustomConfig)
	if err != nil {
		return nil, err
	}
	if err := ValidateIPv6Node(document); err != nil {
		return nil, err
	}
	if err := NormalizeProcessMode(document); err != nil {
		return nil, err
	}
	userHostKeys := mappingKeys(yamlx.Get(document, "hosts"))
	if options.SyncLANHosts {
		if err := syncMihomoHosts(document, config); err != nil {
			return nil, err
		}
	}
	var mosdnsHostKeys []string
	for _, key := range mappingKeys(yamlx.Get(document, "hosts")) {
		if !slices.Contains(userHostKeys, key) {
			mosdnsHostKeys = append(mosdnsHostKeys, key)
		}
	}
	selection := mihomoUsed(config)
	roots := orderedRoots(config, selection)
	required, err := RequiredNodes(config, roots, false)
	if err != nil {
		return nil, &DialerReferenceError{Message: err.Error(), cause: err}
	}
	used, err := RequiredNodes(config, roots, true)
	if err != nil {
		return nil, &DialerReferenceError{Message: err.Error(), cause: err}
	}
	onlyAggregated := map[string]bool{}
	for id := range selection.aggregationRoots {
		if !selection.direct[id] && !required[id] {
			onlyAggregated[id] = true
		}
	}
	proxies := yamlx.Seq()
	inline := []InlineNode{}
	for _, node := range config.Nodes {
		if !enabled(node.Enabled) || !used[node.ID] || onlyAggregated[node.ID] {
			continue
		}
		converted, err := NodeProxy(ctx, node, env.Converter)
		if err != nil {
			return nil, err
		}
		proxy, err := ApplyNodeDialer(config, node, converted)
		if err != nil {
			return nil, &DialerReferenceError{Message: err.Error(), cause: err}
		}
		if proxy != nil && len(proxy.Content) > 0 {
			proxies.Content = append(proxies.Content, proxy)
			inline = append(inline, InlineNode{node, proxy})
		}
	}
	yamlx.Set(document, "proxies", proxies)
	baseURL := strings.TrimSpace(config.SystemConfig.ServerDomain)
	if baseURL == "" {
		baseURL = options.BaseURL
	}
	interval, err := env.ProviderInterval(config.Profile.Mihomo)
	if err != nil {
		return nil, err
	}
	providers := yamlx.Map()
	provider := func(name, endpoint string, check *model.ProviderHealthCheck, address *string) error {
		url := baseURL + profilePath(config, endpoint)
		if config.SystemConfig.ConfigToken != "" {
			var err error
			url, err = appendProfileQuery(url, [][2]string{{"token", config.SystemConfig.ConfigToken}})
			if err != nil {
				return err
			}
		}
		health, err := providerHealthNode(check, address, env)
		if err != nil {
			return err
		}
		yamlx.Set(providers, name, yamlx.Map(yamlx.Str("type"), yamlx.Str("http"), yamlx.Str("url"), yamlx.Str(url), yamlx.Str("path"), yamlx.Str("./providers/"+name+".yaml"), yamlx.Str("interval"), yamlx.Int(interval), yamlx.Str("health-check"), health))
		return nil
	}
	for _, sub := range config.Subscriptions {
		if enabled(sub.Enabled) && selection.subscriptions[sub.ID] {
			if err := provider(sub.Name, "/subscriptions/"+sub.ID+"/proxies", sub.HealthCheck, sub.HealthCheckURL); err != nil {
				return nil, err
			}
		}
	}
	for _, agg := range config.Aggregations {
		if enabled(agg.Enabled) && selection.aggregations[agg.ID] {
			if err := provider(agg.Name, "/aggregations/"+agg.ID+"/provider", agg.HealthCheck, agg.HealthCheckURL); err != nil {
				return nil, err
			}
		}
	}
	if len(providers.Content) > 0 {
		yamlx.Set(document, "proxy-providers", providers)
	}
	groups, err := mihomoGroups(config, proxies, env)
	if err != nil {
		return nil, err
	}
	yamlx.Set(document, "proxy-groups", groups)
	ruleProviders := yamlx.Map()
	for _, rule := range ruleSets {
		if !enabled(rule.Enabled) {
			continue
		}
		url, original, err := ruleDownloadURL(config, rule, baseURL)
		if err != nil {
			return nil, err
		}
		format, err := RuleFormat(rule, config, original)
		if err != nil {
			return nil, err
		}
		extension := format
		if extension == "text" {
			extension = "list"
		}
		behavior := "domain"
		if rule.Behavior != nil {
			behavior = *rule.Behavior
		}
		yamlx.Set(ruleProviders, rule.Name, yamlx.Map(yamlx.Str("type"), yamlx.Str("http"), yamlx.Str("behavior"), yamlx.Str(behavior), yamlx.Str("url"), yamlx.Str(url), yamlx.Str("path"), yamlx.Str("./ruleset/"+rule.Name+"."+extension), yamlx.Str("interval"), yamlx.Int(86400), yamlx.Str("format"), yamlx.Str(format)))
	}
	if len(ruleProviders.Content) > 0 {
		yamlx.Set(document, "rule-providers", ruleProviders)
	}
	if err := applyGateway(document, config, baseURL); err != nil {
		return nil, err
	}
	ruleLines, err := mihomoRuleLines(config)
	if err != nil {
		return nil, err
	}
	yamlx.Set(document, "rules", ruleLines)
	if _, err := ApplyEasyTierNode(document, logical, options.ForAgent, options.CoreVersion); err != nil {
		return nil, &EasyTierSettingsError{Message: err.Error()}
	}
	if err := env.FilterGroups(config, document); err != nil {
		return nil, err
	}
	if err := env.ChainProviders(config, document); err != nil {
		return nil, err
	}
	var hosts *HostsResult
	typedCache := env.activeTypedSubscriptionCache()
	if typedCache != nil {
		hosts, err = mergeHostsWithReader(ctx, document, config, userHostKeys, mosdnsHostKeys, inline, typedCache, env.LoadSubscriptionCache)
	} else {
		hosts, err = env.MergeHosts(ctx, document, config, userHostKeys, mosdnsHostKeys, inline, env.LoadSubscriptionCache)
	}
	if err != nil {
		return nil, err
	}
	if hosts == nil || hosts.Report == nil || hosts.Merged == nil {
		return nil, fmt.Errorf("%s", msgMihomoHosts)
	}
	var version string
	if len(options.CoreVersion) > 0 && string(options.CoreVersion) != "null" {
		if err := json.Unmarshal(options.CoreVersion, &version); err != nil {
			return nil, fmt.Errorf("%s", msgETVersionType)
		}
	}
	dns, err := env.NodeDNS(document, hosts.Merged, version)
	if err != nil {
		return nil, err
	}
	if dns != nil {
		yamlx.Set(hosts.Report, "dns", dns)
	}
	if err := env.LogHosts(logical.ProfileID, hosts.Report); err != nil {
		return nil, err
	}
	if options.Report != nil {
		options.Report.SubscriptionHosts = hosts.Report
	}
	dialers, err := env.DialerGroups(ctx, document, config, DialerCollaborators{LoadCache: env.LoadSubscriptionCache, LoadCacheTyped: typedCache, Convert: func(ctx context.Context, node model.Node) (*yamlx.Node, error) {
		return NodeProxy(ctx, node, env.Converter)
	}, DownloadedText: env.DownloadedText})
	if err != nil {
		return nil, err
	}
	if options.Report != nil && dialers != nil && len(dialers.Content) > 0 {
		options.Report.SubscriptionDialers = dialers
	}
	names := []string{}
	for _, key := range []string{"proxies", "proxy-groups"} {
		items, ok := sequenceNode(yamlx.Get(document, key))
		if !ok {
			return nil, fmt.Errorf("%s", msgMihomoSections)
		}
		for _, item := range items {
			if name, ok := textNode(yamlx.Get(item, "name")); ok {
				names = append(names, name)
			} else {
				return nil, fmt.Errorf("%s", msgMihomoName)
			}
		}
	}
	if err := env.ValidateRules(config, names, "mihomo"); err != nil {
		return nil, err
	}
	if err := ValidateTopology(document, TopologyOptions{}); err != nil {
		return nil, &DialerReferenceError{Message: err.Error(), cause: err}
	}
	if !options.SkipProviderPreflight {
		if env.Preflight == nil {
			return nil, &HookMissingError{Hook: "prepare_provider_bundle", Provider: "convert/mihomoenv (C17 provider)"}
		}
		if err := env.Preflight(ctx, logical, document); err != nil {
			return nil, err
		}
	}
	if options.RegionProbe {
		port := int64(DefaultRegionProbePort)
		if logical.Profile.DomainDiscovery.RegionProbePort != nil && *logical.Profile.DomainDiscovery.RegionProbePort != 0 {
			port = int64(*logical.Profile.DomainDiscovery.RegionProbePort)
		}
		if _, err := addRegionProbe(document, port); err != nil {
			return nil, err
		}
	}
	if err := env.ValidateListeners(document); err != nil {
		return nil, err
	}
	return outputGraph(document)
}

// MihomoConfig renders the typed Mihomo output with the shared native writer.
func MihomoConfig(ctx context.Context, config *model.Config, options MihomoOptions) (string, error) {
	document, err := MihomoDocument(ctx, config, options)
	if err != nil {
		return "", err
	}
	data, err := yamlx.Marshal(document)
	return yamlx.ReadableUnicode(string(data)), err
}
func addRegionProbe(document *yamlx.Node, port int64) (bool, error) {
	if port < 1 || port > 65535 {
		return false, fmt.Errorf("%s", msgRegionPort)
	}
	if UsedPorts(document)[port] {
		mihomoLog.Warningf(msgRegionConflict, port)
		return false, nil
	}
	groups := yamlx.Seq()
	names := []string{"DIRECT"}
	if list, ok := sequenceNode(yamlx.Get(document, "proxy-groups")); ok {
		for _, group := range list {
			name, valid := textNode(yamlx.Get(group, "name"))
			if name == RegionProbeGroup {
				continue
			}
			groups.Content = append(groups.Content, group)
			if valid && name != "" {
				names = append(names, name)
			}
		}
	} else if value := yamlx.Get(document, "proxy-groups"); value != nil && !nullNode(value) {
		return false, fmt.Errorf("%s", msgMihomoSections)
	}
	groups.Content = append(groups.Content, yamlx.Map(yamlx.Str("name"), yamlx.Str(RegionProbeGroup), yamlx.Str("type"), yamlx.Str("select"), yamlx.Str("proxies"), stringSequence(names), yamlx.Str("include-all"), yamlx.Bool(true), yamlx.Str("hidden"), yamlx.Bool(true)))
	yamlx.Set(document, "proxy-groups", groups)
	listeners := yamlx.Seq()
	if list, ok := sequenceNode(yamlx.Get(document, "listeners")); ok {
		for _, listener := range list {
			name, _ := textNode(yamlx.Get(listener, "name"))
			if name != RegionProbeListener {
				listeners.Content = append(listeners.Content, listener)
			}
		}
	}
	listeners.Content = append(listeners.Content, yamlx.Map(yamlx.Str("name"), yamlx.Str(RegionProbeListener), yamlx.Str("type"), yamlx.Str("mixed"), yamlx.Str("listen"), yamlx.Str("127.0.0.1"), yamlx.Str("port"), yamlx.Int(port), yamlx.Str("proxy"), yamlx.Str(RegionProbeGroup)))
	yamlx.Set(document, "listeners", listeners)
	return true, nil
}
