package convert

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"

	"configflow/internal/health/check"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/pystr"
	"configflow/internal/rules"
	"configflow/internal/subs"
	"configflow/internal/weburl"
	"configflow/internal/yamlbridge"
	"configflow/internal/yamlx"
)

// Port of the generation group of backend/converters/mihomo.py (C18):
// CustomConfigYAMLError, validate_ipv6_flags, _default_mihomo_template,
// split_rules_and_rulesets, build_provider_health_check,
// apply_no_resolve_to_logic_rule, normalize_find_process_mode,
// parse_mosdns_custom_hosts, sync_mosdns_hosts, inject_region_probe and
// generate_mihomo_config. The download helpers are in mihomo_downloads.go.

// CustomConfigYAMLError is mihomo.CustomConfigYAMLError: the custom base
// YAML cannot be parsed or validated (routes answer 500; nothing is written).
type CustomConfigYAMLError struct{ Message string }

func (e *CustomConfigYAMLError) Error() string { return e.Message }

// ValidateIPv6Flags is validate_ipv6_flags: ipv6 and dns.ipv6 must be YAML
// booleans when present.
func ValidateIPv6Flags(document any) error {
	obj, ok := document.(*jsonx.Object)
	if !ok || obj == nil {
		return nil
	}
	if value, present := obj.Get("ipv6"); present {
		if _, isBool := value.(bool); !isBool {
			return &CustomConfigYAMLError{"ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"}
		}
	}
	if dns, ok := obj.Value("dns").(*jsonx.Object); ok {
		if value, present := dns.Get("ipv6"); present {
			if _, isBool := value.(bool); !isBool {
				return &CustomConfigYAMLError{"dns 下的 ipv6 必须为 true 或 false，不能是字符串或拼写错误的值"}
			}
		}
	}
	return nil
}

// DefaultMihomoTemplate is _default_mihomo_template: the built-in base used
// while mihomo.custom_config was never set. Each call returns a new value.
func DefaultMihomoTemplate() *jsonx.Object {
	filter := make([]any, 0, len(subs.ConnectivityFilter)+1)
	for _, item := range subs.ConnectivityFilter {
		filter = append(filter, item)
	}
	filter = append(filter, "+.pool.ntp.org")
	return jsonx.Obj(
		"mixed-port", jsonx.Int(7890),
		"allow-lan", true,
		"bind-address", "*",
		"mode", "rule",
		"log-level", "info",
		"external-controller", "127.0.0.1:9090",
		"dns", jsonx.Obj(
			"enable", true,
			"listen", "0.0.0.0:53",
			"enhanced-mode", "fake-ip",
			"fake-ip-range", "198.18.0.1/16",
			"fake-ip-filter", filter,
			"nameserver", []any{"223.5.5.5", "119.29.29.29"},
			"fallback", []any{"https://1.1.1.1/dns-query", "https://dns.google/dns-query"},
		),
	)
}

// SplitRulesAndRulesets is split_rules_and_rulesets: the enabled rules and
// rule sets of rule_configs, or the legacy rules / rule_sets lists when
// rule_configs is empty.
func SplitRulesAndRulesets(config *jsonx.Object) (rulesList, ruleSets any, err error) {
	defer catch(&err)
	rulesList, ruleSets = splitRulesAndRulesets(config)
	return
}

func splitRulesAndRulesets(config *jsonx.Object) (any, any) {
	allRules := dictGet(config, "rule_configs", []any{})
	rulesOut, sets := []any{}, []any{}
	for _, item := range iterate(allRules) {
		if !jsonx.Truthy(dictGet(item, "enabled", true)) || !jsonx.Truthy(dictGet(item, "library_enabled", true)) {
			continue
		}
		itemType := dictGet(item, "itemType", "")
		if jsonx.Equal(itemType, "rule") {
			rulesOut = append(rulesOut, item)
		} else if jsonx.Equal(itemType, "ruleset") {
			sets = append(sets, item)
		}
	}
	if !jsonx.Truthy(allRules) {
		return dictGet(config, "rules", []any{}), dictGet(config, "rule_sets", []any{})
	}
	return rulesOut, sets
}

// DefaultHealthCheckURL is DEFAULT_HEALTH_CHECK_URL.
const DefaultHealthCheckURL = "https://www.gstatic.com/generate_204"

// BuildProviderHealthCheck is build_provider_health_check: the provider's
// health-check block (the structured health_check when present).
func BuildProviderHealthCheck(source *jsonx.Object) (out *jsonx.Object, err error) {
	defer catch(&err)
	return buildProviderHealthCheck(source), nil
}

func buildProviderHealthCheck(source any) *jsonx.Object {
	obj, ok := source.(*jsonx.Object)
	if !ok {
		raise(pyError("TypeError", "argument of type '"+typeName(source)+"' is not iterable"))
	}
	if obj.Has("health_check") {
		return mustValue(check.ProviderHealthCheck(obj))
	}
	value := dictGet(obj, "health_check_url", nil)
	if !jsonx.Truthy(value) {
		value = ""
	}
	url := pystr.Strip(fstr(value))
	if url == "" {
		url = DefaultHealthCheckURL
	}
	return jsonx.Obj("enable", true, "url", url, "interval", jsonx.Int(300), "lazy", true)
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
		parts[i] = pystr.Strip(p)
	}
	if !inStrings(pystr.Upper(parts[0]), resolveTriggerRuleTypes...) {
		return whole
	}
	for _, p := range parts {
		if pystr.Lower(p) == "no-resolve" {
			return whole
		}
	}
	return "(" + inner + ",no-resolve)"
}

// NormalizeFindProcessMode is normalize_find_process_mode: YAML booleans
// become always/off, known mode names are trimmed and lower-cased.
func NormalizeFindProcessMode(mihomoConfig *jsonx.Object) {
	value := mihomoConfig.Value("find-process-mode")
	switch x := value.(type) {
	case nil:
		return
	case bool:
		if x {
			mihomoConfig.Set("find-process-mode", "always")
		} else {
			mihomoConfig.Set("find-process-mode", "off")
		}
	case string:
		normalized := pystr.Lower(pystr.Strip(x))
		if inStrings(normalized, "always", "strict", "off") {
			mihomoConfig.Set("find-process-mode", normalized)
		}
	}
}

// ParseMosdnsCustomHosts is parse_mosdns_custom_hosts: dict(parse_hosts_pairs(text)).
func ParseMosdnsCustomHosts(customHosts string) *jsonx.Object {
	out := jsonx.NewObject()
	for _, pair := range ParseHostsPairs(customHosts) {
		out.Set(pair.Domain, pair.Address)
	}
	return out
}

// SyncMosdnsHosts is sync_mosdns_hosts: MosDNS custom hosts are added to the
// top-level hosts (written entries win) and dns.use-hosts is turned on when
// the user did not set it.
func SyncMosdnsHosts(mihomoConfig, config *jsonx.Object) (err error) {
	defer catch(&err)
	syncMosdnsHosts(mihomoConfig, config)
	return nil
}

func syncMosdnsHosts(mihomoConfig *jsonx.Object, config any) {
	customHosts := dictGet(dictGet(config, "mosdns", jsonx.NewObject()), "custom_hosts", "")
	text, ok := customHosts.(string)
	if !ok {
		// parse_hosts_pairs calls .splitlines() on the value.
		raise(attributeError(customHosts, "splitlines"))
	}
	synced := ParseMosdnsCustomHosts(text)
	if synced.Len() == 0 {
		return
	}
	hosts, ok := mihomoConfig.Value("hosts").(*jsonx.Object)
	if !ok {
		hosts = jsonx.NewObject()
	}
	for domain, ip := range synced.All() {
		hosts.SetDefault(domain, ip)
	}
	mihomoConfig.Set("hosts", hosts)
	if dns, ok := mihomoConfig.Value("dns").(*jsonx.Object); ok && !dns.Has("use-hosts") {
		dns.Set("use-hosts", true)
	}
}

// InjectRegionProbe is inject_region_probe: a loopback mixed listener whose
// traffic goes to a hidden select group. It returns false (and logs) when
// the port is already used by an inbound.
func InjectRegionProbe(mihomoConfig *jsonx.Object, port jsonx.Number) (injected bool, err error) {
	defer catch(&err)
	return injectRegionProbe(mihomoConfig, port), nil
}

func injectRegionProbe(mihomoConfig *jsonx.Object, port jsonx.Number) bool {
	used := mustValue(MihomoUsedPorts(mihomoConfig))
	if used[port] {
		mihomoLog.Warning("区域检测入口端口 " + port.String() + " 与已有入站冲突，本次未注入")
		return false
	}
	var groups []any
	source := mihomoConfig.Value("proxy-groups")
	if !jsonx.Truthy(source) {
		source = []any{}
	}
	for _, group := range iterate(source) {
		if !jsonx.Equal(dictGet(group, "name", nil), RegionProbeGroup) {
			groups = append(groups, group)
		}
	}
	proxies := []any{"DIRECT"}
	for _, group := range groups {
		if jsonx.Truthy(dictGet(group, "name", nil)) {
			proxies = append(proxies, dictItem(group, "name"))
		}
	}
	groups = append(groups, jsonx.Obj(
		"name", RegionProbeGroup,
		"type", "select",
		"proxies", proxies,
		"include-all", true,
		"hidden", true,
	))
	mihomoConfig.Set("proxy-groups", groups)
	listeners := []any{}
	if existing, ok := mihomoConfig.Value("listeners").([]any); ok {
		for _, item := range existing {
			if obj, ok := item.(*jsonx.Object); ok && jsonx.Equal(obj.Value("name"), RegionProbeListener) {
				continue
			}
			listeners = append(listeners, item)
		}
	}
	listeners = append(listeners, jsonx.Obj("name", RegionProbeListener, "type", "mixed", "listen", "127.0.0.1",
		"port", port, "proxy", RegionProbeGroup))
	mihomoConfig.Set("listeners", listeners)
	return true
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

// GenerateMihomoConfig is generate_mihomo_config(config_data, ...): the Mihomo
// YAML of a profile, rendered with yamlx.Marshal (two-space indentation and LF).
// Generated document semantics match Python. Errors are the exceptions Python
// raises: *CustomConfigYAMLError, *DialerReferenceError, StrategyReferenceError,
// ListenerReferenceError, refresh/health ValueErrors, *HookMissingError for
// an unregistered hook, *PyError for crashes on malformed values.
// Sub-Store node conversions reuse the scope installed by WithNodeConversions.
func GenerateMihomoConfig(ctx context.Context, config *jsonx.Object, opts GenerateOptions) (out string, err error) {
	ctx = EnsureNodeConversions(ctx)
	main, err := generateMihomoDocument(ctx, config, opts)
	if err != nil {
		return "", err
	}
	n, err := yamlbridge.Encode(main)
	if err != nil {
		return "", err
	}
	data, err := yamlx.Marshal(n)
	return string(data), err
}

// GenerateMihomoDocument is yaml.safe_load(generate_mihomo_config(...)) for
// callers that parse the generated YAML again: the dumped text is loaded
// back, so the value is exactly what Python's callers see.
func GenerateMihomoDocument(ctx context.Context, config *jsonx.Object, opts GenerateOptions) (*jsonx.Object, error) {
	text, err := GenerateMihomoConfig(ctx, config, opts)
	if err != nil {
		return nil, err
	}
	value, err := yamlx.Load(text)
	if err != nil {
		return nil, err
	}
	obj, ok := value.(*jsonx.Object)
	if !ok {
		return nil, pyError("AttributeError", "'"+typeName(value)+"' object has no attribute 'get'")
	}
	return obj, nil
}

func loadCustomConfig(custom any) *jsonx.Object {
	text, isString := custom.(string)
	if !jsonx.Truthy(custom) || (isString && pystr.Strip(text) == "") {
		return DefaultMihomoTemplate()
	}
	if !isString {
		raise(attributeError(custom, "strip"))
	}
	value, err := yamlx.Load(text)
	if err != nil {
		var yerr *yamlx.Error
		if errors.As(err, &yerr) && yerr.IsYAMLError() {
			// Parser errors contain excerpts of user YAML, including credentials.
			// Return the diagnostic to the caller without duplicating it in logs.
			raise(&CustomConfigYAMLError{"mihomo.custom_config YAML 解析失败: " + yerr.Error()})
		}
		raise(err)
	}
	obj, ok := value.(*jsonx.Object)
	if !ok {
		name := typeName(value)
		raise(&CustomConfigYAMLError{"mihomo.custom_config 不是 mapping，得到 " + name})
	}
	return obj
}

// followed is the `next((g for g in groups if g.get('id') == follow), None)` lookup.
func followedGroup(groups []any, follow any) any {
	for _, g := range groups {
		if jsonx.Equal(dictGet(g, "id", nil), follow) {
			return g
		}
	}
	return nil
}

func subsError(err error) error {
	var d *subs.DialerReferenceError
	if errors.As(err, &d) {
		return dialerError(d.Message)
	}
	return err
}

func generateMihomoDocument(ctx context.Context, logical *jsonx.Object, opts GenerateOptions) (mihomoConfig *jsonx.Object, err error) {
	defer catch(&err)
	env := CurrentMihomoEnv()

	// lower_proxy_chains: validate_dialers (this package) then the lowering.
	must(ValidateDialers(logical))
	config, lowerErr := subs.LowerProxyChains(logical)
	must(subsError(lowerErr))
	rulesList, ruleSetsList := splitRulesAndRulesets(config)
	_ = rulesList
	ruleLibrary := dictGet(config, "rule_library", []any{})

	mihomoSettings := dictGet(config, "mihomo", jsonx.NewObject())
	mihomoConfig = loadCustomConfig(dictGet(mihomoSettings, "custom_config", ""))

	must(ValidateIPv6Flags(mihomoConfig))
	NormalizeFindProcessMode(mihomoConfig)

	var userHostKeys []string
	if written, ok := mihomoConfig.Value("hosts").(*jsonx.Object); ok {
		userHostKeys = written.Keys()
	}
	if opts.SyncLANHosts {
		syncMosdnsHosts(mihomoConfig, config)
	}
	var mosdnsHostKeys []string
	if synced, ok := mihomoConfig.Value("hosts").(*jsonx.Object); ok {
		for _, k := range synced.Keys() {
			if !containsString(userHostKeys, k) {
				mosdnsHostKeys = append(mosdnsHostKeys, k)
			}
		}
	}

	nodesValue := dictGet(config, "nodes", []any{})
	groupsValue := dictGet(config, "proxy_groups", []any{})
	aggregationsValue := dictGet(config, "subscription_aggregations", []any{})
	groupList := iterate(groupsValue)

	usedNodeIDs := newSet()
	for _, n := range iterate(nodesValue) {
		if jsonx.Truthy(dictGet(n, "_proxy_chain_id", nil)) {
			usedNodeIDs.add(dictItem(n, "id"))
		}
	}
	for _, g := range groupList {
		dialer := dictGet(g, "_chain_dialer", jsonx.NewObject())
		if jsonx.Equal(dictGet(dialer, "type", nil), "node") {
			usedNodeIDs.add(dictItem(dictItem(g, "_chain_dialer"), "id"))
		}
	}
	usedSubscriptionIDs := newSet()
	usedAggregationIDs := newSet()

	debug := mihomoLog.Enabled(logx.Debug)
	mihomoLog.Debug("开始收集被策略组使用的节点、订阅和聚合...")

	for _, group := range groupList {
		if !jsonx.Truthy(dictGet(group, "enabled", true)) {
			continue
		}
		var manualNodes, aggregationIDs, subscriptions any
		if follow := dictGet(group, "follow_group", nil); jsonx.Truthy(follow) {
			followed := followedGroup(iterate(dictGet(config, "proxy_groups", []any{})), follow)
			if followed == nil {
				continue
			}
			manualNodes = dictGet(followed, "manual_nodes", []any{})
			aggregationIDs = dictGet(followed, "aggregations", []any{})
			subscriptions = dictGet(followed, "subscriptions", []any{})
		} else {
			manualNodes = dictGet(group, "manual_nodes", []any{})
			aggregationIDs = dictGet(group, "aggregations", []any{})
			subscriptions = dictGet(group, "subscriptions", []any{})
		}
		for _, nodeID := range iterate(manualNodes) {
			if !inStrings(nodeID, "DIRECT", "REJECT") {
				usedNodeIDs.add(nodeID)
				if debug {
					mihomoLog.Debug("策略组 '" + fstr(dictGet(group, "name", nil)) + "' 添加手动节点到 used_node_ids: " + fstr(nodeID))
				}
			}
		}
		if jsonx.Truthy(aggregationIDs) {
			for _, aggID := range iterate(aggregationIDs) {
				usedAggregationIDs.add(aggID)
			}
		}
		inGroupAggregations := newSet()
		if jsonx.Truthy(aggregationIDs) {
			aggregations := dictGet(config, "subscription_aggregations", []any{})
			for _, aggID := range iterate(aggregationIDs) {
				var agg any
				for _, a := range iterate(aggregations) {
					if jsonx.Equal(dictItem(a, "id"), aggID) && jsonx.Truthy(dictGet(a, "enabled", true)) {
						agg = a
						break
					}
				}
				if agg != nil {
					for _, s := range iterate(dictGet(agg, "subscriptions", []any{})) {
						inGroupAggregations.add(s)
					}
				}
			}
		}
		for _, subID := range iterate(subscriptions) {
			if !inGroupAggregations.has(subID) {
				usedSubscriptionIDs.add(subID)
			}
		}
		proxiesOrder := dictGet(group, "proxies_order", []any{})
		if jsonx.Truthy(proxiesOrder) {
			for _, item := range iterate(proxiesOrder) {
				if jsonx.Equal(dictGet(item, "type", nil), "node") {
					nodeID := dictGet(item, "id", nil)
					if !inStrings(nodeID, "DIRECT", "REJECT") {
						usedNodeIDs.add(nodeID)
						if debug {
							mihomoLog.Debug("策略组 '" + fstr(dictGet(group, "name", nil)) + "' 从 proxies_order 添加节点到 used_node_ids: " + fstr(nodeID))
						}
					}
				}
			}
		}
	}

	directlySelected := newSet()
	for _, group := range groupList {
		if !jsonx.Truthy(dictGet(group, "enabled", true)) {
			continue
		}
		var manualNodes, proxiesOrder any
		if follow := dictGet(group, "follow_group", nil); jsonx.Truthy(follow) {
			followed := followedGroup(iterate(dictGet(config, "proxy_groups", []any{})), follow)
			if followed == nil {
				continue
			}
			manualNodes = dictGet(followed, "manual_nodes", []any{})
			proxiesOrder = dictGet(followed, "proxies_order", []any{})
		} else {
			manualNodes = dictGet(group, "manual_nodes", []any{})
			proxiesOrder = dictGet(group, "proxies_order", []any{})
		}
		for _, nodeID := range iterate(manualNodes) {
			if !inStrings(nodeID, "DIRECT", "REJECT") {
				directlySelected.add(nodeID)
			}
		}
		if jsonx.Truthy(proxiesOrder) {
			for _, item := range iterate(proxiesOrder) {
				if jsonx.Equal(dictGet(item, "type", nil), "node") {
					nodeID := dictGet(item, "id", nil)
					if !inStrings(nodeID, "DIRECT", "REJECT") {
						directlySelected.add(nodeID)
					}
				}
			}
		}
	}
	if debug {
		mihomoLog.Debug("策略组直接选择的节点ID: " + directlySelected.repr())
	}

	onlyInAggregations := newSet()
	for _, agg := range iterate(aggregationsValue) {
		if jsonx.Truthy(dictGet(agg, "enabled", true)) && usedAggregationIDs.has(dictGet(agg, "id", nil)) {
			aggNodes := dictGet(agg, "nodes", []any{})
			for _, nodeID := range iterate(aggNodes) {
				if !directlySelected.has(nodeID) {
					onlyInAggregations.add(nodeID)
				}
			}
			if debug {
				mihomoLog.Debug("聚合 '" + fstr(dictGet(agg, "name", nil)) + "' 包含节点: " + jsonx.Repr(aggNodes))
			}
		}
	}
	if debug {
		mihomoLog.Debug("仅通过聚合使用的节点ID（将被排除）: " + onlyInAggregations.repr())
	}

	for _, group := range groupList {
		if jsonx.Truthy(dictGet(group, "enabled", true)) {
			obj, _ := group.(*jsonx.Object)
			members := mustValue(groupMembers(obj))
			for _, ref := range members {
				if ref.Kind == "node" && !inStrings(ref.IDValue(), "DIRECT", "REJECT") {
					usedNodeIDs.add(ref.IDValue())
					directlySelected.add(ref.IDValue())
				}
			}
		}
	}
	aggregationRoots := newSet()
	for _, agg := range iterate(aggregationsValue) {
		if jsonx.Truthy(dictGet(agg, "enabled", true)) && usedAggregationIDs.has(dictGet(agg, "id", nil)) {
			for _, id := range iterate(dictGet(agg, "nodes", []any{})) {
				aggregationRoots.add(id)
			}
		}
	}
	roots := append(append([]string(nil), usedNodeIDs.keys...), aggregationRoots.keys...)
	required := mustValue(DependencyNodeIDs(config, roots, false))
	for k := range required {
		onlyInAggregations.removeKey(k)
	}
	for _, k := range directlySelected.keys {
		onlyInAggregations.removeKey(k)
	}
	usedAll := mustValue(DependencyNodeIDs(config, roots, true))

	proxies := []any{}
	var inline []subs.InlineProxy
	if debug {
		mihomoLog.Debug("收集到的被使用节点ID: " + pySetRepr(sortedKeys(usedAll), keyValues(usedAll, usedNodeIDs, aggregationRoots)))
		mihomoLog.Debug("开始生成 proxies，总节点数: " + strconv.Itoa(len(iterate(dictGet(config, "nodes", []any{})))))
	}
	for _, n := range iterate(dictGet(config, "nodes", []any{})) {
		if !jsonx.Truthy(dictGet(n, "enabled", true)) {
			if debug {
				mihomoLog.Debug("跳过禁用节点: " + fstr(dictGet(n, "name", nil)))
			}
			continue
		}
		id := dictGet(n, "id", nil)
		if !usedAll[key(id)] {
			if debug {
				mihomoLog.Debug("跳过未被策略组使用的节点: " + fstr(dictGet(n, "name", nil)) + " (id: " + fstr(id) + ")")
			}
			continue
		}
		if onlyInAggregations.has(id) {
			if debug {
				mihomoLog.Debug("跳过仅通过聚合使用的节点: " + fstr(dictGet(n, "name", nil)) + " (id: " + fstr(id) + ")")
			}
			continue
		}
		node := n.(*jsonx.Object)
		converted := mustValue(ConvertNodeToMihomo(ctx, node, env.Converter))
		proxy := mustValue(OverlayDialer(config, node, converted))
		if proxy != nil && proxy.Len() > 0 {
			proxies = append(proxies, proxy)
			inline = append(inline, subs.InlineProxy{Node: node, Proxy: proxy})
			if debug {
				mihomoLog.Debug("添加节点到 proxies: " + fstr(dictGet(n, "name", nil)))
			}
		}
	}
	if debug {
		mihomoLog.Debug("最终生成的 proxies 数量: " + strconv.Itoa(len(proxies)))
	}
	mihomoConfig.Set("proxies", proxies)

	systemConfig := dictGet(config, "system_config", jsonx.NewObject())
	serverDomain := pystr.Strip(strMethod(dictGet(systemConfig, "server_domain", ""), "strip"))
	effectiveBaseURL := serverDomain
	if effectiveBaseURL == "" {
		effectiveBaseURL = opts.BaseURL
	}
	configToken := dictGet(dictGet(config, "system_config", jsonx.NewObject()), "config_token", "")

	proxyProviders := jsonx.NewObject()
	if env.ProviderInterval == nil {
		raise(&HookMissingError{Hook: "validate_refresh_interval", Provider: "convert/mihomoenv (C15 refresh)"})
	}
	mihomoObject, ok := dictGet(config, "mihomo", jsonx.NewObject()).(*jsonx.Object)
	if !ok {
		raise(attributeError(dictGet(config, "mihomo", nil), "get"))
	}
	interval := jsonx.Int(mustValue(env.ProviderInterval(mihomoObject)))
	mihomoLog.Debug("开始生成订阅 proxy-providers，使用本地接口")
	if debug {
		mihomoLog.Debug("Server domain: " + weburl.SafeForLog(effectiveBaseURL))
		if jsonx.Truthy(configToken) {
			mihomoLog.Debug("Config token: 已配置")
		} else {
			mihomoLog.Debug("Config token: 未配置")
		}
	}

	for _, sub := range iterate(dictGet(config, "subscriptions", []any{})) {
		if jsonx.Truthy(dictGet(sub, "enabled", true)) && usedSubscriptionIDs.has(dictGet(sub, "id", nil)) {
			subID := dictItem(sub, "id")
			subURL := effectiveBaseURL + profileAPIPath(config, "/subscriptions/"+fstr(subID)+"/proxies")
			if jsonx.Truthy(configToken) {
				subURL = appendURLQuery(subURL, [][2]any{{"token", configToken}})
			}
			if debug {
				mihomoLog.Debug("订阅 '" + fstr(dictItem(sub, "name")) + "' 使用本地接口: " + weburl.SafeForLog(subURL))
			}
			name := providerKey(dictItem(sub, "name"))
			proxyProviders.Set(name, jsonx.Obj(
				"type", "http",
				"url", subURL,
				"path", "./providers/"+fstr(dictItem(sub, "name"))+".yaml",
				"interval", interval,
				"health-check", buildProviderHealthCheck(sub),
			))
		}
	}
	for _, agg := range iterate(dictGet(config, "subscription_aggregations", []any{})) {
		if jsonx.Truthy(dictGet(agg, "enabled", true)) && usedAggregationIDs.has(dictGet(agg, "id", nil)) {
			aggID := dictItem(agg, "id")
			aggURL := effectiveBaseURL + profileAPIPath(config, "/aggregations/"+fstr(aggID)+"/provider")
			if jsonx.Truthy(configToken) {
				aggURL = appendURLQuery(aggURL, [][2]any{{"token", configToken}})
			}
			name := providerKey(dictItem(agg, "name"))
			proxyProviders.Set(name, jsonx.Obj(
				"type", "http",
				"url", aggURL,
				"path", "./providers/"+fstr(dictItem(agg, "name"))+".yaml",
				"interval", interval,
				"health-check", buildProviderHealthCheck(agg),
			))
		}
	}
	if proxyProviders.Len() > 0 {
		mihomoConfig.Set("proxy-providers", proxyProviders)
	}

	proxyGroups := []any{}
	if debug {
		mihomoLog.Debug("开始处理策略组，总数: " + strconv.Itoa(len(iterate(dictGet(config, "proxy_groups", []any{})))))
	}
	for _, group := range iterate(dictGet(config, "proxy_groups", []any{})) {
		if debug {
			mihomoLog.Debug("处理策略组: " + fstr(dictGet(group, "name", nil)))
		}
		if !jsonx.Truthy(dictGet(group, "enabled", true)) {
			if debug {
				mihomoLog.Debug("跳过禁用的策略组: " + fstr(dictGet(group, "name", nil)))
			}
			continue
		}
		proxyGroup := jsonx.Obj("name", dictItem(group, "name"), "type", dictItem(group, "type"))
		follow := dictGet(group, "follow_group", nil)
		var followed any
		if jsonx.Truthy(follow) {
			followed = followedGroup(iterate(dictGet(config, "proxy_groups", []any{})), follow)
			if followed == nil {
				continue
			}
			proxyGroup.Set("type", dictItem(followed, "type"))
			proxyGroup.Update(rules.RenderMembers(followed.(*jsonx.Object), config))
			if v := dictGet(followed, "url", nil); jsonx.Truthy(v) {
				proxyGroup.Set("url", dictItem(followed, "url"))
			}
			if v := dictGet(followed, "interval", nil); jsonx.Truthy(v) {
				proxyGroup.Set("interval", dictItem(followed, "interval"))
			}
			if jsonx.Equal(dictItem(followed, "type"), "url-test") {
				proxyGroup.Set("tolerance", jsonx.Int(100))
				proxyGroup.Set("lazy", true)
			}
			if jsonx.Equal(dictItem(followed, "type"), "load-balance") {
				if jsonx.Truthy(dictGet(followed, "strategy", nil)) {
					proxyGroup.Set("strategy", dictItem(followed, "strategy"))
				}
				if dictGet(followed, "lazy", nil) != nil {
					proxyGroup.Set("lazy", dictItem(followed, "lazy"))
				}
			}
		} else {
			proxyGroup.Update(rules.RenderMembers(group.(*jsonx.Object), config))
		}
		if jsonx.Truthy(dictGet(group, "url", nil)) {
			proxyGroup.Set("url", dictItem(group, "url"))
		}
		if jsonx.Truthy(dictGet(group, "interval", nil)) {
			proxyGroup.Set("interval", dictItem(group, "interval"))
		}
		if jsonx.Equal(dictItem(group, "type"), "url-test") {
			proxyGroup.Set("tolerance", jsonx.Int(100))
			proxyGroup.Set("lazy", true)
		}
		if jsonx.Equal(dictItem(group, "type"), "load-balance") {
			if jsonx.Truthy(dictGet(group, "strategy", nil)) {
				proxyGroup.Set("strategy", dictItem(group, "strategy"))
			}
			if dictGet(group, "lazy", nil) != nil {
				proxyGroup.Set("lazy", dictItem(group, "lazy"))
			}
		}
		settings := group
		if jsonx.Truthy(follow) {
			settings = followed
		}
		settingsObj := settings.(*jsonx.Object)
		for _, k := range []string{"url", "interval", "tolerance", "lazy", "strategy", "timeout", "max-failed-times",
			"expected-status", "disable-udp", "hidden", "icon", "exclude-filter", "exclude-type"} {
			if v, present := settingsObj.Get(k); present {
				proxyGroup.Set(k, v)
			}
		}
		if !jsonx.Equal(proxyGroup.Value("type"), "url-test") {
			proxyGroup.Delete("tolerance")
		}
		proxyGroups = append(proxyGroups, proxyGroup)
	}

	if len(proxyGroups) == 0 {
		chained := false
		for _, n := range iterate(dictGet(config, "nodes", []any{})) {
			if jsonx.Truthy(dictGet(n, "_proxy_chain_id", nil)) {
				chained = true
				break
			}
		}
		if !chained {
			var names []any
			for _, p := range proxies {
				names = append(names, dictItem(p, "name"))
			}
			if len(names) > 0 {
				proxyGroups = []any{
					jsonx.Obj("name", "PROXY", "type", "select", "proxies", append([]any{"Auto"}, names...)),
					jsonx.Obj("name", "Auto", "type", "url-test", "proxies", append([]any(nil), names...),
						"url", "https://www.gstatic.com/generate_204", "interval", jsonx.Int(300),
						"tolerance", jsonx.Int(100), "lazy", true),
				}
			}
		}
	}
	mihomoConfig.Set("proxy-groups", proxyGroups)

	ruleProviders := jsonx.NewObject()
	serverDomain = pystr.Strip(strMethod(dictGet(dictGet(config, "system_config", jsonx.NewObject()), "server_domain", ""), "strip"))
	effectiveBaseURL = serverDomain
	if effectiveBaseURL == "" {
		effectiveBaseURL = opts.BaseURL
	}
	for _, ruleSet := range iterate(ruleSetsList) {
		if !jsonx.Truthy(dictGet(ruleSet, "enabled", true)) {
			continue
		}
		url, originalURL := mihomoRulesetURL(config, ruleSet, ruleLibrary, effectiveBaseURL)
		behavior := dictGet(ruleSet, "behavior", "domain")
		ruleFormat := mustValue(RulesetFormat(ruleSet.(*jsonx.Object), config, originalURL))
		extension := ruleFormat
		if ruleFormat == "text" {
			extension = "list"
		}
		name := providerKey(dictItem(ruleSet, "name"))
		ruleProviders.Set(name, jsonx.Obj(
			"type", "http",
			"behavior", behavior,
			"url", url,
			"path", "./ruleset/"+fstr(dictItem(ruleSet, "name"))+"."+extension,
			"interval", jsonx.Int(86400),
			"format", ruleFormat,
		))
	}
	if ruleProviders.Len() > 0 {
		mihomoConfig.Set("rule-providers", ruleProviders)
	}

	must(applyLinuxGateway(mihomoConfig, config, effectiveBaseURL))

	ruleLines := []any{}
	emitted := mustValue(EmittedRules(config))
	for _, item := range emitted {
		if !jsonx.Truthy(dictGet(item, "enabled", true)) || !jsonx.Truthy(dictGet(item, "library_enabled", true)) {
			continue
		}
		itemType := dictGet(item, "itemType", "")
		if jsonx.Equal(itemType, "rule") {
			ruleType := dictItem(item, "rule_type")
			value := dictGet(item, "value", "")
			policy := dictItem(item, "policy")
			switch {
			case jsonx.Equal(ruleType, "MATCH"):
				ruleLines = append(ruleLines, "MATCH,"+fstr(policy))
			case jsonx.Equal(ruleType, "RULE-SET"):
				ruleLines = append(ruleLines, "RULE-SET,"+fstr(value)+","+fstr(policy))
			case inStrings(ruleType, LogicRuleTypes...):
				if jsonx.Truthy(dictGet(item, "no_resolve", false)) {
					text, ok := value.(string)
					if !ok {
						raise(pyError("TypeError", "expected string or bytes-like object, got '"+typeName(value)+"'"))
					}
					value = ApplyNoResolveToLogicRule(text)
				}
				ruleLines = append(ruleLines, fstr(ruleType)+","+fstr(value)+","+fstr(policy))
			default:
				if jsonx.Truthy(dictGet(item, "no_resolve", false)) {
					ruleLines = append(ruleLines, fstr(ruleType)+","+fstr(value)+","+fstr(policy)+",no-resolve")
				} else {
					ruleLines = append(ruleLines, fstr(ruleType)+","+fstr(value)+","+fstr(policy))
				}
			}
		} else if jsonx.Equal(itemType, "ruleset") {
			policy := dictGet(item, "policy", "PROXY")
			name := dictItem(item, "name")
			if jsonx.Truthy(dictGet(item, "no_resolve", false)) {
				ruleLines = append(ruleLines, "RULE-SET,"+fstr(name)+","+fstr(policy)+",no-resolve")
			} else {
				ruleLines = append(ruleLines, "RULE-SET,"+fstr(name)+","+fstr(policy))
			}
		}
	}
	if mustValue(UDPFallback(config)) == "REJECT" {
		ruleLines = append(ruleLines, "NETWORK,udp,REJECT")
	}
	mihomoConfig.Set("rules", ruleLines)

	mustValue(ApplyEasyTier(mihomoConfig, logical, opts.ForAgent, opts.MihomoCoreVersion))
	rules.LowerGroupProviderFilters(config, mihomoConfig)
	must(subsError(subs.LowerChainProviders(config, mihomoConfig)))

	mergedHosts := jsonx.NewObject()
	hostsReport := subs.MergeSubscriptionHosts(mihomoConfig, config, userHostKeys, mosdnsHostKeys, inline, mergedHosts,
		func(sub *jsonx.Object) *subs.HostsIndex {
			return subscriptionHostsIndex(ctx, env, sub)
		})
	coreVersion, _ := opts.MihomoCoreVersion.(string)
	if proxyDNS := subs.ApplyNodeDNS(mihomoConfig, mergedHosts, coreVersion); proxyDNS != nil {
		hostsReport.Set("dns", proxyDNS)
	}
	profileID := dictGet(logical, "profile_id", nil)
	if !jsonx.Truthy(profileID) {
		profileID = ""
	}
	subs.LogReport(fstr(profileID), hostsReport)
	if opts.Report != nil {
		opts.Report.Set("subscription_hosts", hostsReport)
	}

	dialerReport, dialerErr := subs.AddDialerGroups(mihomoConfig, config, subs.DialerDeps{
		LoadCache: func(subID string) *jsonx.Object {
			return loadSubscriptionCache(ctx, env, subID)
		},
		ConvertNode: func(node *jsonx.Object) *jsonx.Object {
			return mustValue(ConvertNodeToMihomo(ctx, node, env.Converter))
		},
		DownloadedText: func(url string) (string, bool) {
			if env.DownloadedText == nil {
				return "", false
			}
			return env.DownloadedText(ctx, url)
		},
	})
	must(subsError(dialerErr))
	if opts.Report != nil && dialerReport != nil && dialerReport.Len() > 0 {
		opts.Report.Set("subscription_dialers", dialerReport)
	}

	var available []string
	for _, p := range iterate(mihomoConfig.Value("proxies")) {
		available = append(available, fstr(dictItem(p, "name")))
	}
	for _, g := range iterate(mihomoConfig.Value("proxy-groups")) {
		available = append(available, fstr(dictItem(g, "name")))
	}
	must(rules.ValidateRulePolicies(config, available, "mihomo"))

	must(ValidateEmitted(mihomoConfig, EmitOptions{}))
	if !opts.SkipProviderPreflight {
		if env.PrepareProviderBundle == nil {
			raise(&HookMissingError{Hook: "prepare_provider_bundle", Provider: "convert/mihomoenv (C17 provider)"})
		}
		must(env.PrepareProviderBundle(ctx, logical, mihomoConfig))
	}
	discovery := dictGet(logical, "domain_discovery", nil)
	if !jsonx.Truthy(discovery) {
		discovery = jsonx.NewObject()
	}
	if opts.RegionProbe {
		port := dictGet(discovery, "region_probe_port", nil)
		if !jsonx.Truthy(port) {
			port = jsonx.Int(DefaultRegionProbePort)
		}
		injectRegionProbe(mihomoConfig, pyIntOf(port))
	}
	must(subs.ValidateListenerPolicies(mihomoConfig))
	return mihomoConfig, nil
}

// mihomoRulesetURL is the URL logic shared by generate_mihomo_config and
// get_mihomo_ruleset_downloads: (url, original_url).
func mihomoRulesetURL(config *jsonx.Object, ruleSet, ruleLibrary any, effectiveBaseURL string) (any, string) {
	libraryRuleID := dictGet(ruleSet, "library_rule_id", nil)
	url := dictGet(ruleSet, "url", "")
	originalURL := url
	if jsonx.Truthy(libraryRuleID) {
		var libraryRule any
		for _, r := range iterate(ruleLibrary) {
			if jsonx.Equal(dictItem(r, "id"), libraryRuleID) {
				libraryRule = r
				break
			}
		}
		if libraryRule != nil {
			originalURL = dictGet(libraryRule, "url", url)
			ruleName := dictGet(libraryRule, "name", "")
			if jsonx.Truthy(ruleName) {
				url = profileAPIPath(config, "/rules/local/"+fstr(ruleName))
				if mustValue(RulesetFormat(ruleSet.(*jsonx.Object), config, sourceURLText(originalURL))) != "mrs" {
					url = appendURLQuery(url.(string), [][2]any{{"target", "mihomo"}})
				}
			}
		}
	}
	if jsonx.Truthy(url) {
		text := strMethod(url, "startswith")
		if strings.HasPrefix(text, "/") && effectiveBaseURL != "" {
			url = effectiveBaseURL + text
		} else if strings.HasPrefix(text, "http") {
			url = mustValue(ApplyGithubProxyDomain(text, config))
		}
	}
	return url, sourceURLText(originalURL)
}

// sourceURLText passes original_url to ruleset_format, which only reads it
// as text (a non-string is treated like Python's checks on it would).
func sourceURLText(v any) string {
	// Empty legacy URL values defer to the rule's own URL. Stringifying a
	// false/zero/empty container would turn it into a nonempty override.
	if !jsonx.Truthy(v) {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fstr(v)
}

// providerKey is a provider/rule-set name used as a YAML mapping key.
func providerKey(name any) string {
	s, ok := name.(string)
	if !ok {
		// Python would write a non-string key; jsonx objects cannot hold one.
		raise(pyError("TypeError", "provider name "+jsonx.Repr(name)+" is not a string (not supported)"))
	}
	return s
}

func profileAPIPath(config *jsonx.Object, suffix string) string {
	if profileID := config.Value("profile_id"); jsonx.Truthy(profileID) {
		return "/api/profiles/" + fstr(profileID) + suffix
	}
	return suffix
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func (s *orderedSet) removeKey(k string) {
	if _, ok := s.values[k]; !ok {
		return
	}
	delete(s.values, k)
	for i, existing := range s.keys {
		if existing == k {
			s.keys = append(s.keys[:i:i], s.keys[i+1:]...)
			break
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func keyValues(m map[string]bool, sets ...*orderedSet) map[string]any {
	out := map[string]any{}
	for k := range m {
		out[k] = k
		for _, s := range sets {
			if v, ok := s.values[k]; ok {
				out[k] = v
			}
		}
	}
	return out
}

func loadSubscriptionCache(ctx context.Context, env MihomoEnv, subID string) *jsonx.Object {
	if env.LoadSubscriptionCache == nil {
		raise(&HookMissingError{Hook: "load_subscription_cache", Provider: "convert/mihomoenv (C13 substore)"})
	}
	return mustValue(env.LoadSubscriptionCache(ctx, subID))
}

// subscriptionHostsIndex is subscription_hosts.subscription_hosts_index(sub)
// without the file-stamp memo: the hosts of the subscription's cache, or nil.
func subscriptionHostsIndex(ctx context.Context, env MihomoEnv, sub *jsonx.Object) *subs.HostsIndex {
	id, ok := sub.Value("id").(string)
	if !ok {
		return nil // _get_cache_path(sub['id']) fails: KeyError/TypeError → None
	}
	cache := loadSubscriptionCache(ctx, env, id)
	if cache == nil {
		return nil
	}
	return subs.NewHostsIndex(cache).ForURL(sub)
}
