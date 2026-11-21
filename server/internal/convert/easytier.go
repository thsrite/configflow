package convert

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/subs"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
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
	easytierKeepURL     = regexp.MustCompile(`^https?://[^\s]+$`)
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

// DefaultEasyTier returns an independent disabled profile overlay.
func DefaultEasyTier() *model.EasyTierSettings {
	var settings model.EasyTierSettings
	_ = json.Unmarshal([]byte(`{"enabled":false,"name":"EasyTier","apply_to":"subscription","routes":[],"magic_dns":false,"keepalive":{"enabled":false,"url":"","interval":300},"options":{"udp":true}}`), &settings)
	return &settings
}

func easyTierText(node *yamlx.Node, label string, required bool, limit int) (string, error) {
	text := ""
	if node != nil && node.ShortTag() != "!!null" {
		if node.Kind != yaml.ScalarNode || node.ShortTag() != "!!str" {
			return "", fmt.Errorf(msgETText, label)
		}
		text = node.Value
	}
	text = strings.TrimSpace(text)
	if required && text == "" {
		return "", fmt.Errorf(msgETRequired, label)
	}
	if utf8.RuneCountInString(text) > limit || strings.ContainsFunc(text, func(r rune) bool { return r < 32 || r == 127 }) {
		return "", fmt.Errorf(msgETTextLimit, label)
	}
	return text, nil
}
func easyTierList(node *yamlx.Node, label string) ([]string, error) {
	if node == nil || node.ShortTag() == "!!null" {
		return []string{}, nil
	}
	if node.Kind != yaml.SequenceNode || len(node.Content) > easytierMaxList {
		return nil, fmt.Errorf(msgETList, label, easytierMaxList)
	}
	result := []string{}
	seen := map[string]bool{}
	for _, value := range node.Content {
		text, err := easyTierText(value, label, false, easytierMaxText)
		if err != nil {
			return nil, err
		}
		if text != "" && !seen[text] {
			seen[text] = true
			result = append(result, text)
		}
	}
	return result, nil
}
func easyTierBool(node *yamlx.Node, label string, fallback bool) (bool, error) {
	if node == nil {
		return fallback, nil
	}
	if node.ShortTag() != "!!bool" {
		return false, fmt.Errorf(msgETBoolean, label)
	}
	return node.Value == "true", nil
}
func easyTierNetwork(value, label string) (string, error) {
	if strings.Contains(value, "->") {
		return "", fmt.Errorf(msgETNetworkMap, label, value)
	}
	network, err := easyTierPrefix(value)
	if err != nil {
		return "", fmt.Errorf(msgETNetwork, label, value)
	}
	if !network.Addr().Is4() {
		return "", fmt.Errorf(msgETIPv4Network, label, value)
	}
	return network.Masked().String(), nil
}

// easyTierPrefix accepts addresses, CIDR prefixes and IPv4 net/host masks.
// IPMask.Size validates contiguous masks without a private IP parser.
func easyTierPrefix(value string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix, nil
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	host, mask, _ := strings.Cut(value, "/")
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf(msgETNetwork, "", value)
	}
	if size, err := strconv.ParseUint(mask, 10, 8); err == nil && size <= uint64(addr.BitLen()) {
		return netip.PrefixFrom(addr, int(size)), nil
	}
	if !addr.Is4() {
		return netip.Prefix{}, fmt.Errorf(msgETNetwork, "", value)
	}
	parsed, err := netip.ParseAddr(mask)
	if err != nil || !parsed.Is4() {
		return netip.Prefix{}, fmt.Errorf(msgETNetwork, "", value)
	}
	bits := parsed.As4()
	ones, width := net.IPMask(bits[:]).Size()
	if width == 0 {
		for i := range bits {
			bits[i] = ^bits[i]
		}
		ones, width = net.IPMask(bits[:]).Size()
	}
	if width == 0 {
		return netip.Prefix{}, fmt.Errorf(msgETNetwork, "", value)
	}
	return netip.PrefixFrom(addr, ones), nil
}
func easyTierPeer(value, label string) (string, error) {
	scheme, rest, ok := strings.Cut(value, "://")
	if !ok || !slices.Contains(easytierPeerSchemes, strings.ToLower(scheme)) || rest == "" {
		return "", fmt.Errorf(msgETPeer, label, value, strings.Join(easytierPeerSchemes[:7], ", "))
	}
	return value, nil
}
func easyTierOption(field EasyTierField, node *yamlx.Node) (*yamlx.Node, error) {
	key := field.Key
	switch field.Kind {
	case "bool":
		if node.ShortTag() != "!!bool" {
			return nil, fmt.Errorf(msgETSwitch, key)
		}
		return yamlx.Bool(node.Value == "true"), nil
	case "int":
		// R8 accepts integral JSON float notation; no Python int lexical emulation.
		data, err := yamlx.ToJSON(node)
		if err != nil {
			return nil, err
		}
		var number model.Int
		if err = json.Unmarshal(data, &number); err != nil {
			if node.ShortTag() == "!!int" {
				bounds := easytierIntRanges[key]
				return nil, fmt.Errorf(msgETRange, key, bounds[0], bounds[1])
			}
			return nil, fmt.Errorf(msgETInteger, key)
		}
		bounds := easytierIntRanges[key]
		if int64(number) < bounds[0] || int64(number) > bounds[1] {
			return nil, fmt.Errorf(msgETRange, key, bounds[0], bounds[1])
		}
		return yamlx.Int(int64(number)), nil
	case "list":
		items, err := easyTierList(node, key)
		if err != nil {
			return nil, err
		}
		result := yamlx.Seq()
		for _, value := range items {
			switch key {
			case "peers", "listeners", "mapped-listeners":
				value, err = easyTierPeer(value, key)
			case "proxy-networks":
				value, err = easyTierNetwork(value, key)
			case "exit-nodes":
				addr, e := netip.ParseAddr(value)
				if e != nil || !addr.Is4() {
					err = fmt.Errorf(msgETExitNode, value)
				}
			}
			if err != nil {
				return nil, err
			}
			result.Content = append(result.Content, yamlx.Str(value))
		}
		return result, nil
	case "choice":
		if node.ShortTag() != "!!str" || !slices.Contains(EasyTierIPVersions, node.Value) {
			return nil, fmt.Errorf(msgETIPVersion, strings.Join(EasyTierIPVersions, " / "))
		}
		return yamlx.Str(node.Value), nil
	}
	limit := easytierMaxText
	if field.Kind == "secret" {
		limit = 4096
	}
	text, err := easyTierText(node, key, false, limit)
	if err != nil {
		return nil, err
	}
	if key == "ipv4" && text != "" {
		prefix, e := easyTierPrefix(text)
		addr := prefix.Addr()
		if e != nil {
			return nil, fmt.Errorf(msgETIPv4, text)
		}
		if !addr.Is4() {
			return nil, errors.New(msgETOnlyIPv4)
		}
	}
	if key == "hostname" && text != "" && !easytierHostname.MatchString(text) {
		return nil, errors.New(msgETHostname)
	}
	return yamlx.Str(text), nil
}
func easyTierSettingsNode(settings *model.EasyTierSettings) (*yamlx.Node, error) {
	data, err := model.Marshal(settings)
	if err != nil {
		return nil, err
	}
	return yamlx.FromJSON(data)
}
func easyTierDefault(node *yamlx.Node, key string, fallback *yamlx.Node) *yamlx.Node {
	value := yamlx.Get(node, key)
	if value == nil {
		return fallback
	}
	return value
}

// NormalizeEasyTier validates a typed stored overlay and returns canonical settings.
// Raw member evidence is checked at this validation boundary rather than silently
// passing a malformed stored option through to the generated configuration.
func NormalizeEasyTier(settings *model.EasyTierSettings) (*model.EasyTierSettings, error) {
	if settings == nil {
		return DefaultEasyTier(), nil
	}
	input, err := easyTierSettingsNode(settings)
	if err != nil {
		return nil, err
	}
	enabled, err := easyTierBool(yamlx.Get(input, "enabled"), "enabled", false)
	if err != nil {
		return nil, err
	}
	name, err := easyTierText(easyTierDefault(input, "name", yamlx.Str(EasyTierDefaultName)), msgETName, true, 64)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ",") {
		return nil, errors.New(msgETNameComma)
	}
	apply := easyTierDefault(input, "apply_to", yamlx.Str("subscription"))
	if apply.ShortTag() != "!!str" || !slices.Contains(EasyTierApplyTo, apply.Value) {
		return nil, errors.New(msgETApply)
	}
	routes, err := easyTierList(yamlx.Get(input, "routes"), msgETRoutes)
	if err != nil {
		return nil, err
	}
	routeNodes := yamlx.Seq()
	for _, value := range routes {
		network, err := easyTierNetwork(value, msgETRoutes)
		if err != nil {
			return nil, err
		}
		routeNodes.Content = append(routeNodes.Content, yamlx.Str(network))
	}
	magic, err := easyTierBool(yamlx.Get(input, "magic_dns"), "magic_dns", false)
	if err != nil {
		return nil, err
	}
	keep := yamlx.Get(input, "keepalive")
	if keep == nil || keep.ShortTag() == "!!null" {
		keep = yamlx.Map()
	}
	if keep.Kind != yaml.MappingNode {
		return nil, errors.New(msgETKeepObject)
	}
	keepEnabled, err := easyTierBool(yamlx.Get(keep, "enabled"), "keepalive.enabled", false)
	if err != nil {
		return nil, err
	}
	address, err := easyTierText(yamlx.Get(keep, "url"), msgETKeepURLLabel, false, easytierMaxText)
	if err != nil {
		return nil, err
	}
	intervalNode := easyTierDefault(keep, "interval", yamlx.Int(EasyTierDefaultKeepaliveInterval))
	var interval model.Int
	rawInterval, err := yamlx.ToJSON(intervalNode)
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(rawInterval, &interval) != nil || interval < 10 || interval > 86400 {
		return nil, errors.New(msgETInterval)
	}
	if address != "" && (!easytierKeepURL.MatchString(address) || strings.ContainsFunc(address, unicode.IsSpace)) {
		return nil, errors.New(msgETKeepURL)
	}
	if keepEnabled && address == "" {
		return nil, errors.New(msgETKeepRequired)
	}
	options := easyTierDefault(input, "options", yamlx.Map())
	if options.Kind != yaml.MappingNode {
		return nil, errors.New(msgETOptionsObject)
	}
	unknown := []string{}
	for key := range yamlx.Pairs(options) {
		if _, ok := easytierFieldKind(key); !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf(msgETUnknown, strings.Join(unknown, ", "))
	}
	cleaned := yamlx.Map()
	for _, field := range EasyTierFields {
		value := yamlx.Get(options, field.Key)
		if value == nil || value.ShortTag() == "!!null" {
			continue
		}
		item, err := easyTierOption(field, value)
		if err != nil {
			return nil, err
		}
		if item.ShortTag() == "!!str" && item.Value == "" || item.Kind == yaml.SequenceNode && len(item.Content) == 0 {
			continue
		}
		yamlx.Set(cleaned, field.Key, item)
	}
	if enabled {
		if scalarText(yamlx.Get(cleaned, "network-name")) == "" {
			return nil, errors.New(msgETNetworkName)
		}
		listeners := yamlx.Get(cleaned, "listeners")
		hasListeners := listeners != nil && len(listeners.Content) > 0
		noListener := yamlx.Get(cleaned, "no-listener")
		if hasListeners && noListener != nil && noListener.Value == "true" {
			return nil, errors.New(msgETListenerConflict)
		}
		peers := yamlx.Get(cleaned, "peers")
		if (peers == nil || len(peers.Content) == 0) && !hasListeners && (noListener == nil || noListener.Value != "false") {
			return nil, errors.New(msgETPeersRequired)
		}
		if scalarText(yamlx.Get(cleaned, "local-public-key")) != "" && scalarText(yamlx.Get(cleaned, "local-private-key")) == "" {
			return nil, errors.New(msgETPrivateKey)
		}
	}
	result := yamlx.Map(yamlx.Str("enabled"), yamlx.Bool(enabled), yamlx.Str("name"), yamlx.Str(name), yamlx.Str("apply_to"), yamlx.Str(apply.Value), yamlx.Str("routes"), routeNodes, yamlx.Str("magic_dns"), yamlx.Bool(magic), yamlx.Str("keepalive"), yamlx.Map(yamlx.Str("enabled"), yamlx.Bool(keepEnabled), yamlx.Str("url"), yamlx.Str(address), yamlx.Str("interval"), yamlx.Int(int64(interval))), yamlx.Str("options"), cleaned)
	data, err := yamlx.ToJSON(result)
	if err != nil {
		return nil, err
	}
	var normalized model.EasyTierSettings
	if err = json.Unmarshal(data, &normalized); err != nil {
		return nil, err
	}
	return &normalized, nil
}

// ConfigEasyTier validates the profile's optional overlay settings.
func ConfigEasyTier(config *model.Config) (*model.EasyTierSettings, error) {
	if config == nil || config.Profile == nil {
		return NormalizeEasyTier(nil)
	}
	return NormalizeEasyTier(config.Profile.Mihomo.EasyTier)
}

// EasyTierGroupName is the generated fallback group's name.
func EasyTierGroupName(settings *model.EasyTierSettings) string {
	name := ""
	if settings != nil && settings.Name != nil {
		name = *settings.Name
	}
	return name + msgETKeepSuffix
}

// EasyTierFor reports whether normalized settings apply to this delivery target.
func EasyTierFor(settings *model.EasyTierSettings, forAgent bool) bool {
	if settings == nil || settings.Enabled == nil || !*settings.Enabled {
		return false
	}
	target := "subscription"
	if forAgent {
		target = "agent"
	}
	return settings.ApplyTo != nil && (*settings.ApplyTo == "all" || *settings.ApplyTo == target)
}

// EasyTierResult describes the overlay appended to a generated document.
type EasyTierResult struct {
	Proxy, Target string
	Rules         int
}

// ApplyEasyTierNode adds the validated overlay to an owned generated graph.
func ApplyEasyTierNode(document *yamlx.Node, config *model.Config, forAgent bool, coreVersion json.RawMessage) (*EasyTierResult, error) {
	settings, err := ConfigEasyTier(config)
	if err != nil {
		return nil, err
	}
	if !EasyTierFor(settings, forAgent) {
		return nil, nil
	}
	var version string
	if len(coreVersion) > 0 && string(coreVersion) != "null" {
		if json.Unmarshal(coreVersion, &version) != nil {
			return nil, errors.New(msgETVersionType)
		}
	}
	if parsed, recognized := subs.CoreVersion(version); recognized {
		for i, part := range parsed {
			if cmp := part.Cmp(big.NewInt(EasyTierMinCoreVersion[i])); cmp < 0 {
				return nil, fmt.Errorf(msgETVersion, version)
			} else if cmp > 0 {
				break
			}
		}
	}
	if document == nil || document.Kind != yaml.MappingNode {
		return nil, errors.New(msgETDocument)
	}
	proxies, groups := yamlx.Resolve(yamlx.Get(document, "proxies")), yamlx.Resolve(yamlx.Get(document, "proxy-groups"))
	if proxies == nil {
		proxies = yamlx.Seq()
		yamlx.Set(document, "proxies", proxies)
	}
	if groups == nil {
		groups = yamlx.Seq()
		yamlx.Set(document, "proxy-groups", groups)
	}
	if proxies.Kind != yaml.SequenceNode || groups.Kind != yaml.SequenceNode {
		return nil, errors.New(msgETDocument)
	}
	taken := map[string]bool{}
	for _, source := range []*yamlx.Node{proxies, groups} {
		for _, item := range source.Content {
			item = yamlx.Resolve(item)
			if item == nil {
				return nil, errors.New(msgETDocument)
			}
			if item.Kind == yaml.MappingNode {
				taken[scalarText(yamlx.Get(item, "name"))] = true
			}
		}
	}
	name := *settings.Name
	target := name
	keep := settings.Keepalive
	if keep.Enabled != nil && *keep.Enabled {
		target = EasyTierGroupName(settings)
	}
	for _, value := range []string{name, target} {
		if taken[value] {
			return nil, fmt.Errorf(msgETNameConflict, value)
		}
	}
	if dialer := settings.Options.DialerProxy; dialer != "" && !taken[dialer] && dialer != "DIRECT" {
		return nil, fmt.Errorf(msgETDialerMissing, dialer)
	}
	normalized, err := easyTierSettingsNode(settings)
	if err != nil {
		return nil, err
	}
	proxy := yamlx.Map(yamlx.Str("name"), yamlx.Str(name), yamlx.Str("type"), yamlx.Str("easytier"))
	for key, value := range yamlx.Pairs(yamlx.Get(normalized, "options")) {
		yamlx.Set(proxy, key, value)
	}
	proxies.Content = append(proxies.Content, proxy)
	if target != name {
		groups.Content = append(groups.Content, yamlx.Map(yamlx.Str("name"), yamlx.Str(target), yamlx.Str("type"), yamlx.Str("fallback"), yamlx.Str("proxies"), yamlx.Seq(yamlx.Str(name)), yamlx.Str("url"), yamlx.Str(keep.URL), yamlx.Str("interval"), yamlx.Int(int64(*keep.Interval))))
	}
	added := yamlx.Seq()
	for _, network := range settings.Routes {
		added.Content = append(added.Content, yamlx.Str("IP-CIDR,"+network+","+target+",no-resolve"))
	}
	if settings.MagicDNS != nil && *settings.MagicDNS {
		tld := strings.Trim(settings.Options.TLDDNSZone, ".")
		if tld == "" {
			tld = EasyTierDefaultTLD
		}
		added.Content = append(added.Content, yamlx.Str("DOMAIN-SUFFIX,"+tld+","+target))
		if dns := yamlx.Resolve(yamlx.Get(document, "dns")); dns != nil && dns.Kind == yaml.MappingNode {
			policy := yamlx.Resolve(yamlx.Get(dns, "nameserver-policy"))
			if policy == nil || policy.ShortTag() == "!!null" {
				policy = yamlx.Map()
				yamlx.Set(dns, "nameserver-policy", policy)
			}
			if policy.Kind == yaml.MappingNode && yamlx.Get(policy, "+."+tld) == nil {
				yamlx.Set(policy, "+."+tld, yamlx.Str("et://"+name))
			}
		}
	}
	count := len(added.Content)
	existing := yamlx.Resolve(yamlx.Get(document, "rules"))
	if existing != nil && existing.ShortTag() != "!!null" {
		if existing.Kind != yaml.SequenceNode {
			return nil, errors.New(msgETDocument)
		}
		added.Content = append(added.Content, existing.Content...)
	}
	yamlx.Set(document, "rules", added)
	return &EasyTierResult{Proxy: name, Target: target, Rules: count}, nil
}

// RedactEasyTier returns a stored-member-preserving copy for human exports.
func RedactEasyTier(settings *model.EasyTierSettings) (*model.EasyTierSettings, error) {
	if settings == nil {
		return nil, nil
	}
	node, err := easyTierSettingsNode(settings)
	if err != nil {
		return nil, err
	}
	options := yamlx.Get(node, "options")
	if options != nil && options.Kind == yaml.MappingNode {
		for _, key := range EasyTierSecretFields {
			value := yamlx.Get(options, key)
			// Invalid stored types are still secret material in an export.
			if value != nil && value.ShortTag() != "!!null" && !(value.ShortTag() == "!!str" && value.Value == "") {
				yamlx.Set(options, key, yamlx.Str("[REDACTED]"))
			}
		}
	}
	data, err := yamlx.ToJSON(node)
	if err != nil {
		return nil, err
	}
	var result model.EasyTierSettings
	err = json.Unmarshal(data, &result)
	return &result, err
}

// RedactEasyTierJSON redacts secret fields in raw EasyTier settings JSON for
// human exports, leaving non-object or malformed payloads unchanged.
func RedactEasyTierJSON(raw json.RawMessage) (json.RawMessage, error) {
	var settings *model.EasyTierSettings
	if err := jsondecode.Unmarshal(raw, &settings); err != nil || settings == nil {
		return raw, nil
	}
	redacted, err := RedactEasyTier(settings)
	if err != nil {
		return raw, nil
	}
	encoded, err := model.Marshal(redacted)
	if err != nil {
		return raw, nil
	}
	return encoded, nil
}
