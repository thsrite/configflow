package convert

// Port of backend/common/mihomo_easytier.py: profile-scoped EasyTier
// networking for generated Mihomo configs (Mihomo v1.19.31 `type: easytier`
// outbound). The setting lives at config['mihomo']['easytier']; every option
// of the outbound is kept under "options" with Mihomo's kebab-case keys.

import (
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
	"configflow/internal/subs"
)

// EasyTier constants (mihomo_easytier.py module level).
const (
	EasyTierDefaultName              = "EasyTier"
	EasyTierDefaultTLD               = "et.net"
	EasyTierDefaultKeepaliveInterval = 300
	easytierMaxList                  = 64
	easytierMaxText                  = 512
)

// EasyTierMinCoreVersion is MIN_CORE_VERSION.
var EasyTierMinCoreVersion = [3]int64{1, 19, 31}

// EasyTierApplyTo is APPLY_TO; EasyTierIPVersions is IP_VERSIONS.
var (
	EasyTierApplyTo    = []string{"subscription", "agent", "all"}
	EasyTierIPVersions = []string{"dual", "ipv4", "ipv6", "ipv4-prefer", "ipv6-prefer"}
)

var (
	easytierPeerSchemes = []string{"tcp", "udp", "ws", "wss", "wg", "quic", "faketcp", "txt", "srv", "http", "https"}
	easytierHostname    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,62})$`)
	easytierKeepURL     = mustPyRegex(`^https?://\S+$`, false)
)

// EasyTierField is one (key, kind) of FIELDS. Kinds: text, secret, bool,
// int, list, choice.
type EasyTierField struct{ Key, Kind string }

// EasyTierFields is FIELDS, in order.
var EasyTierFields = []EasyTierField{
	{"network-name", "text"}, {"network-secret", "secret"}, {"hostname", "text"}, {"ipv4", "text"},
	{"dhcp", "bool"}, {"peers", "list"}, {"secure-mode", "bool"}, {"local-private-key", "secret"},
	{"local-public-key", "text"}, {"listeners", "list"}, {"no-listener", "bool"}, {"mapped-listeners", "list"},
	{"exit-nodes", "list"}, {"proxy-networks", "list"}, {"instance-name", "text"}, {"state-dir", "text"},
	{"udp", "bool"}, {"accept-dns", "bool"}, {"enable-exit-node", "bool"}, {"enable-encryption", "bool"},
	{"encryption-algorithm", "text"}, {"private-mode", "bool"}, {"latency-first", "bool"},
	{"disable-p2p", "bool"}, {"enable-kcp-proxy", "bool"}, {"disable-kcp-input", "bool"},
	{"enable-quic-proxy", "bool"}, {"disable-quic-input", "bool"}, {"mtu", "int"}, {"tld-dns-zone", "text"},
	{"dialer-proxy", "text"}, {"interface-name", "text"}, {"routing-mark", "int"}, {"ip-version", "choice"},
}

// EasyTierSecretFields is SECRET_FIELDS.
var EasyTierSecretFields = []string{"network-secret", "local-private-key"}

var easytierIntRanges = map[string][2]int64{"mtu": {576, 9000}, "routing-mark": {0, 1<<32 - 1}}

func easytierFieldKind(key string) (string, bool) {
	for _, f := range EasyTierFields {
		if f.Key == key {
			return f.Kind, true
		}
	}
	return "", false
}

// EasyTierDefaultSettings is default_settings().
func EasyTierDefaultSettings() *jsonx.Object {
	return jsonx.Obj(
		"enabled", false,
		"name", EasyTierDefaultName,
		"apply_to", "subscription",
		"routes", []any{},
		"magic_dns", false,
		"keepalive", jsonx.Obj("enabled", false, "url", "", "interval", jsonx.Int(EasyTierDefaultKeepaliveInterval)),
		"options", jsonx.Obj("udp", true),
	)
}

func cleanText(value any, label string, required bool, limit int) (string, error) {
	if value == nil {
		value = ""
	}
	text, ok := value.(string)
	if !ok {
		return "", easytierError(label + " 必须是文本")
	}
	text = pystr.Strip(text)
	if required && text == "" {
		return "", easytierError("请填写" + label)
	}
	bad := utf8.RuneCountInString(text) > limit
	for _, r := range text {
		if r < 32 || r == 127 {
			bad = true
		}
	}
	if bad {
		return "", easytierError(label + " 过长或包含控制字符")
	}
	return text, nil
}

func cleanList(value any, label string) ([]string, error) {
	if value == nil {
		return []string{}, nil
	}
	list, ok := value.([]any)
	if !ok || len(list) > easytierMaxList {
		return nil, easytierError(label + " 必须是列表，最多 " + strconv.Itoa(easytierMaxList) + " 项")
	}
	items := make([]string, 0, len(list))
	for _, item := range list {
		text, err := cleanText(item, label, false, easytierMaxText)
		if err != nil {
			return nil, err
		}
		items = append(items, text)
	}
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out, nil
}

func ipv4Network(value, label string) (string, error) {
	if strings.Contains(value, "->") {
		return "", easytierError(label + "「" + value + "」：Mihomo 的 EasyTier 不支持「->」网段映射，只能原样填写网段")
	}
	canonical, version, ok := ipNetwork(value)
	if !ok {
		return "", easytierError(label + "「" + value + "」不是有效的网段")
	}
	if version != 4 {
		return "", easytierError(label + "「" + value + "」：EasyTier 虚拟网只支持 IPv4")
	}
	return canonical, nil
}

func peerURI(value, label string) (string, error) {
	scheme, rest, found := strings.Cut(value, "://")
	if !found || !contains(easytierPeerSchemes, pystr.Lower(scheme)) || rest == "" {
		return "", easytierError(label + "「" + value + "」格式不对，应类似 tcp://203.0.113.10:11010（支持 " +
			strings.Join(easytierPeerSchemes[:7], ", ") + " 等）")
	}
	return value, nil
}

func easytierOption(key, kind string, value any) (any, error) {
	label := key
	switch kind {
	case "bool":
		if _, ok := value.(bool); !ok {
			return nil, easytierError(label + " 必须是开或关")
		}
		return value, nil
	case "int":
		number, ok := value.(jsonx.Number)
		if !ok || !number.IsInt() {
			return nil, easytierError(label + " 必须是整数")
		}
		bounds := easytierIntRanges[key]
		n, _ := number.BigInt()
		if n.Cmp(big.NewInt(bounds[0])) < 0 || n.Cmp(big.NewInt(bounds[1])) > 0 {
			return nil, easytierError(label + " 需在 " + strconv.FormatInt(bounds[0], 10) + " ~ " +
				strconv.FormatInt(bounds[1], 10) + " 之间")
		}
		return value, nil
	case "list":
		items, err := cleanList(value, label)
		if err != nil {
			return nil, err
		}
		switch key {
		case "peers", "listeners", "mapped-listeners":
			for i, item := range items {
				if items[i], err = peerURI(item, label); err != nil {
					return nil, err
				}
			}
		case "proxy-networks":
			for i, item := range items {
				if items[i], err = ipv4Network(item, label); err != nil {
					return nil, err
				}
			}
		case "exit-nodes":
			for _, item := range items {
				if ipAddressVersion(item) != 4 {
					return nil, easytierError("exit-nodes「" + item + "」应填写出口节点的虚拟 IPv4 地址")
				}
			}
		}
		return jsonx.ListOf(items), nil
	case "choice":
		if !contains(EasyTierIPVersions, value) {
			return nil, easytierError("ip-version 只能是 " + strings.Join(EasyTierIPVersions, " / "))
		}
		return value, nil
	}
	limit := easytierMaxText
	if kind == "secret" {
		limit = 4096
	}
	text, err := cleanText(value, label, false, limit)
	if err != nil {
		return nil, err
	}
	if key == "ipv4" && text != "" {
		switch ipInterfaceVersion(text) {
		case 0:
			return nil, easytierError("ipv4「" + text + "」应类似 10.144.144.10 或 10.144.144.10/24")
		case 6:
			return nil, easytierError("ipv4 只能填写 IPv4 地址")
		}
	}
	if key == "hostname" && text != "" && !easytierHostname.MatchString(text) {
		return nil, easytierError("hostname 只能包含字母、数字和连字符，不超过 63 个字符")
	}
	return text, nil
}

// NormalizeEasyTierSettings is normalize_settings: validate a stored or
// submitted setting and return its canonical form.
func NormalizeEasyTierSettings(value any) (*jsonx.Object, error) {
	if value == nil {
		return EasyTierDefaultSettings(), nil
	}
	input, ok := value.(*jsonx.Object)
	if !ok {
		return nil, easytierError("EasyTier 设置必须是对象")
	}
	result := EasyTierDefaultSettings()
	enabledValue := input.GetOr("enabled", false)
	enabled, ok := enabledValue.(bool)
	if !ok {
		return nil, easytierError("enabled 必须是布尔值")
	}
	result.Set("enabled", enabled)
	name, err := cleanText(input.GetOr("name", EasyTierDefaultName), "节点名称", true, 64)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ",") {
		return nil, easytierError("节点名称不能包含英文逗号，否则规则无法引用")
	}
	result.Set("name", name)
	applyTo := input.GetOr("apply_to", "subscription")
	if !contains(EasyTierApplyTo, applyTo) {
		return nil, easytierError("下发范围只能是 subscription、agent 或 all")
	}
	result.Set("apply_to", applyTo)
	routeItems, err := cleanList(input.Value("routes"), "走 EasyTier 的网段")
	if err != nil {
		return nil, err
	}
	routes := make([]any, 0, len(routeItems))
	for _, item := range routeItems {
		network, err := ipv4Network(item, "走 EasyTier 的网段")
		if err != nil {
			return nil, err
		}
		routes = append(routes, network)
	}
	result.Set("routes", routes)
	magicDNS, ok := input.GetOr("magic_dns", false).(bool)
	if !ok {
		return nil, easytierError("magic_dns 必须是布尔值")
	}
	result.Set("magic_dns", magicDNS)

	keepaliveValue := input.Value("keepalive")
	if !jsonx.Truthy(keepaliveValue) {
		keepaliveValue = jsonx.NewObject()
	}
	keepalive, ok := keepaliveValue.(*jsonx.Object)
	if !ok {
		return nil, easytierError("keepalive 必须是对象")
	}
	keepEnabled, ok := keepalive.GetOr("enabled", false).(bool)
	if !ok {
		return nil, easytierError("keepalive.enabled 必须是布尔值")
	}
	url, err := cleanText(keepalive.GetOr("url", ""), "保活检测地址", false, easytierMaxText)
	if err != nil {
		return nil, err
	}
	interval := keepalive.GetOr("interval", jsonx.Int(EasyTierDefaultKeepaliveInterval))
	if number, ok := interval.(jsonx.Number); !ok || !number.IsInt() {
		return nil, easytierError("保活检测间隔需为 10 ~ 86400 秒的整数")
	} else if n, _ := number.BigInt(); n.Cmp(big.NewInt(10)) < 0 || n.Cmp(big.NewInt(86400)) > 0 {
		return nil, easytierError("保活检测间隔需为 10 ~ 86400 秒的整数")
	}
	if url != "" && !easytierKeepURL.Search(url) {
		return nil, easytierError("保活检测地址应为 http:// 或 https:// 开头的网址")
	}
	if keepEnabled && url == "" {
		return nil, easytierError("开启保活时请填写检测地址，例如虚拟网内某台设备上的网页")
	}
	result.Set("keepalive", jsonx.Obj("enabled", keepEnabled, "url", url, "interval", interval))

	options, ok := input.GetOr("options", jsonx.NewObject()).(*jsonx.Object)
	if !ok {
		return nil, easytierError("options 必须是对象")
	}
	var unknown []string
	for _, key := range options.Keys() {
		if _, known := easytierFieldKind(key); !known {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, easytierError("不认识的 EasyTier 参数：" + strings.Join(unknown, ", "))
	}
	cleaned := jsonx.NewObject()
	for _, field := range EasyTierFields {
		raw, present := options.Get(field.Key)
		if !present || raw == nil {
			continue
		}
		item, err := easytierOption(field.Key, field.Kind, raw)
		if err != nil {
			return nil, err
		}
		if item == "" {
			continue
		}
		if list, isList := item.([]any); isList && len(list) == 0 {
			continue
		}
		cleaned.Set(field.Key, item)
	}
	result.Set("options", cleaned)

	if enabled {
		if !jsonx.Truthy(cleaned.Value("network-name")) {
			return nil, easytierError("请填写 network-name（EasyTier 网络名）")
		}
		if jsonx.Truthy(cleaned.Value("listeners")) && cleaned.Value("no-listener") == true {
			return nil, easytierError("填写了 listeners 时不能同时开启 no-listener")
		}
		listening := jsonx.Truthy(cleaned.Value("listeners")) || cleaned.Value("no-listener") == false
		if !jsonx.Truthy(cleaned.Value("peers")) && !listening {
			return nil, easytierError("不监听端口时至少要填写一个 peers（入网节点）")
		}
		if jsonx.Truthy(cleaned.Value("local-public-key")) && !jsonx.Truthy(cleaned.Value("local-private-key")) {
			return nil, easytierError("local-public-key 需要和 local-private-key 一起填写")
		}
	}
	return result, nil
}

// EasyTierSettings is easytier_settings(config).
func EasyTierSettings(config any) (*jsonx.Object, error) {
	var value any
	if c, ok := config.(*jsonx.Object); ok && c != nil {
		if mihomo, ok := c.Value("mihomo").(*jsonx.Object); ok && mihomo != nil {
			value = mihomo.Value("easytier")
		}
	}
	return NormalizeEasyTierSettings(value)
}

// EasyTierKeepaliveGroupName is keepalive_group_name.
func EasyTierKeepaliveGroupName(settings *jsonx.Object) string {
	return jsonx.Str(settings.Value("name")) + " 保活"
}

func easytierTLD(settings *jsonx.Object) string {
	zone := settings.GetObject("options").Value("tld-dns-zone")
	text, _ := zone.(string)
	if !jsonx.Truthy(zone) {
		text = EasyTierDefaultTLD
	}
	if text = pyStripChars(text, "."); text == "" {
		return EasyTierDefaultTLD
	}
	return text
}

// EasyTierApplies is applies(settings, for_agent=...).
func EasyTierApplies(settings *jsonx.Object, forAgent bool) bool {
	if settings.Value("enabled") != true {
		return false
	}
	applyTo := settings.Value("apply_to")
	target := "subscription"
	if forAgent {
		target = "agent"
	}
	return applyTo == "all" || applyTo == target
}

// EasyTierResult is the summary apply_easytier returns.
type EasyTierResult struct {
	Proxy  string
	Target string
	Rules  int
}

// ApplyEasyTier is apply_easytier: add the EasyTier proxy, keep-alive group,
// rules and MagicDNS to mihomoConfig. It returns nil (and leaves the document
// untouched) when the setting is off or not meant for this target. coreVersion
// is the Agent's reported Mihomo version (nil when unknown).
func ApplyEasyTier(mihomoConfig *jsonx.Object, config any, forAgent bool, coreVersion any) (*EasyTierResult, error) {
	settings, err := EasyTierSettings(config)
	if err != nil {
		return nil, err
	}
	if !EasyTierApplies(settings, forAgent) {
		return nil, nil
	}
	if parsed, ok := subs.ParseCoreVersion(coreVersion); ok {
		older := false
		for i, part := range parsed {
			if c := part.Cmp(big.NewInt(EasyTierMinCoreVersion[i])); c != 0 {
				older = c < 0
				break
			}
		}
		if older {
			return nil, easytierError("设备上的 Mihomo " + jsonx.Str(coreVersion) +
				" 不支持 EasyTier 节点（需要 v1.19.31 或更新），请先更新内核，或在 EasyTier 设置里不再下发到 Agent")
		}
	}
	name := settings.GetString("name")
	proxiesValue := mihomoConfig.SetDefault("proxies", []any{})
	groupsValue := mihomoConfig.SetDefault("proxy-groups", []any{})
	taken := map[string]bool{}
	for _, source := range []any{proxiesValue, groupsValue} {
		items, err := iterList(source)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			obj, ok := item.(*jsonx.Object)
			if !ok || obj == nil {
				continue
			}
			value := obj.Value("name")
			if err := hashable(value); err != nil {
				return nil, err
			}
			if text, isText := value.(string); isText {
				taken[text] = true
			}
		}
	}
	keepalive := settings.GetObject("keepalive")
	keepEnabled := keepalive.Value("enabled") == true
	target := name
	names := []string{name}
	if keepEnabled {
		target = EasyTierKeepaliveGroupName(settings)
		names = append(names, target)
	}
	for _, item := range names {
		if taken[item] {
			return nil, easytierError("EasyTier 节点名「" + item + "」与已有节点或策略组重名，请在 EasyTier 设置里改名")
		}
	}
	options := settings.GetObject("options")
	if dialer, _ := options.Value("dialer-proxy").(string); dialer != "" && !taken[dialer] && dialer != "DIRECT" {
		return nil, easytierError("EasyTier 的 dialer-proxy「" + dialer + "」不在生成的节点或策略组里")
	}

	proxies, ok := proxiesValue.([]any)
	if !ok {
		return nil, attributeError(proxiesValue, "append")
	}
	proxy := jsonx.Obj("name", name, "type", "easytier")
	for key, value := range options.DeepCopy().All() {
		proxy.Set(key, value)
	}
	mihomoConfig.Set("proxies", append(proxies, proxy))
	if keepEnabled {
		groups, ok := groupsValue.([]any)
		if !ok {
			return nil, attributeError(groupsValue, "append")
		}
		mihomoConfig.Set("proxy-groups", append(groups, jsonx.Obj("name", target, "type", "fallback",
			"proxies", []any{name}, "url", keepalive.Value("url"), "interval", keepalive.Value("interval"))))
	}

	var added []any
	for _, network := range settings.GetList("routes") {
		added = append(added, "IP-CIDR,"+jsonx.Str(network)+","+target+",no-resolve")
	}
	if settings.Value("magic_dns") == true {
		tld := easytierTLD(settings)
		added = append(added, "DOMAIN-SUFFIX,"+tld+","+target)
		if dns, ok := mihomoConfig.Value("dns").(*jsonx.Object); ok && dns != nil {
			policy := dns.Value("nameserver-policy")
			if policy == nil {
				policy = jsonx.NewObject()
				dns.Set("nameserver-policy", policy)
			}
			if p, ok := policy.(*jsonx.Object); ok && p != nil {
				p.SetDefault("+."+tld, "et://"+name)
			}
		}
	}
	existing := []any{}
	if value := mihomoConfig.Value("rules"); jsonx.Truthy(value) {
		if existing, err = iterList(value); err != nil {
			return nil, err
		}
	}
	rules := append(append([]any{}, added...), existing...)
	mihomoConfig.Set("rules", rules)
	return &EasyTierResult{Proxy: name, Target: target, Rules: len(added)}, nil
}

// RedactEasyTierSettings is redact_settings: a copy for human-shareable
// exports with secrets replaced and the shape kept.
func RedactEasyTierSettings(value any) any {
	input, ok := value.(*jsonx.Object)
	if !ok || input == nil {
		return value
	}
	result := input.DeepCopy()
	if options, ok := result.Value("options").(*jsonx.Object); ok && options != nil {
		for _, key := range EasyTierSecretFields {
			if jsonx.Truthy(options.Value(key)) {
				options.Set(key, "[REDACTED]")
			}
		}
	}
	return result
}
