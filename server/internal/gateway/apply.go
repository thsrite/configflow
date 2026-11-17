package gateway

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/gateway/gwbase"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// isAuthEnabled reports whether panel login is on (a variable for tests).
var isAuthEnabled = auth.IsAuthEnabled

// TUN keys that would let connections bypass the TUN; the gateway refuses
// them (the order is the order of the messages).
var tunFilterKeys = gwbase.TUNFilterKeys

// routeKeys are TUN route settings the gateway drops: CIDR routing and the
// provider-based route-address-set intersect, so an old fake-IP-only
// route-address would silently stop real-IP rules from working.
var routeKeys = []string{
	"route-address", "inet4-route-address", "inet6-route-address",
	"route-exclude-address-set", "route-exclude-address",
	"inet4-route-exclude-address", "inet6-route-exclude-address",
}

// MarkerKey is the generated document's marker of a gateway configuration.
const MarkerKey = "x-configflow-linux-gateway"

// errProviderNotMapping: a custom rule provider the gateway re-points is not
// a mapping (Python failed with an AttributeError, a 500).
var errProviderNotMapping = errors.New("gateway: rule provider is not a mapping")

// RoutingMarkPath is the path ("Mihomo.proxies.0.routing-mark") of the first
// routing-mark set anywhere in value (depth first, in document order), ""
// when there is none. root names value in the path. A routing-mark of null,
// false, 0 or "" is not set. The gateway's routing marks would conflict.
func RoutingMarkPath(value *yaml.Node, root string) string {
	value = yamlx.Resolve(value)
	if value == nil {
		return ""
	}
	switch value.Kind {
	case yaml.MappingNode:
		if mark := yamlx.Get(value, "routing-mark"); mark != nil && !unset(mark) {
			return root + ".routing-mark"
		}
		for i := 0; i+1 < len(value.Content); i += 2 {
			if found := RoutingMarkPath(value.Content[i+1], root+"."+keyText(value.Content[i])); found != "" {
				return found
			}
		}
	case yaml.SequenceNode:
		for i, child := range value.Content {
			if found := RoutingMarkPath(child, root+"."+strconv.Itoa(i)); found != "" {
				return found
			}
		}
	}
	return ""
}

func keyText(key *yaml.Node) string {
	if key = yamlx.Resolve(key); key == nil {
		return ""
	}
	return key.Value
}

// Apply patches a generated Mihomo document (a mapping) for the profile's
// gateway; with the gateway off it changes nothing. It first runs the
// router split generation check (a *routersplit.RouterSplitError for an
// invalid marker or a full-TUN router split with the gateway on). The
// document then gets: an auto-route/auto-redirect TUN (without IPv6, strict
// route or DNS hijack) whose route-address-set is the inline fake-IP
// provider plus one HTTP provider per selected rule set (refreshed from the
// panel by stable ID, with the subscription token), the selected rule sets'
// own providers re-pointed to their source URLs, fake-ip DNS on
// 127.0.0.1:1053, rule mode, ipv6 off and the MarkerKey marker.
// baseURL is used when system_config.server_domain is empty. Errors that
// refuse the configuration are *Error; the document may then be partly
// patched and must be discarded.
func (c *Config) Apply(doc *yaml.Node, baseURL string) error {
	if err := c.checkGeneration(); err != nil {
		return err
	}
	settings, err := c.Settings()
	if err != nil || !settings.Enabled {
		return err
	}
	conflict := RoutingMarkPath(doc, pathMihomo)
	if conflict == "" {
		conflict = RoutingMarkPath(c.nodes(), pathNodes)
	}
	if conflict != "" {
		return gatewayError(fmt.Sprintf(fmtRoutingMark, conflict))
	}
	fakeRange, err := FakeIPNetwork(doc)
	if err != nil {
		return err
	}
	tun := yamlx.Map()
	if current := yamlx.Get(doc, "tun"); truthy(current) {
		if !isMap(current) {
			return gatewayError(msgTunNotObject)
		}
		tun = cloneShared(current)
	}
	for _, key := range tunFilterKeys {
		if truthy(yamlx.Get(tun, key)) {
			return gatewayError(fmt.Sprintf(fmtTunFilter, key))
		}
	}
	for _, key := range routeKeys {
		yamlx.Delete(tun, key)
	}
	yamlx.Set(tun, "enable", yamlx.Bool(true))
	yamlx.Set(tun, "auto-route", yamlx.Bool(true))
	yamlx.Set(tun, "auto-redirect", yamlx.Bool(true))
	yamlx.Set(tun, "auto-detect-interface", yamlx.Bool(true))
	yamlx.Set(tun, "strict-route", yamlx.Bool(false))
	yamlx.Set(tun, "dns-hijack", yamlx.Seq())
	setDefault(tun, "stack", yamlx.Str("mixed"))
	setDefault(tun, "device", yamlx.Str("Mihomo"))

	providers := setDefault(doc, "rule-providers", yamlx.Map())
	if !isMap(providers) {
		return gatewayError(msgProvidersNotObject)
	}
	for name := range yamlx.Pairs(providers) {
		if strings.HasPrefix(name, ProviderPrefix) {
			return gatewayError(msgReservedPrefix)
		}
	}
	yamlx.Set(providers, FakeProvider, yamlx.Map(
		yamlx.Str("type"), yamlx.Str("inline"),
		yamlx.Str("behavior"), yamlx.Str("ipcidr"),
		yamlx.Str("format"), yamlx.Str("yaml"),
		yamlx.Str("payload"), strSeq([]string{fakeRange.String()})))
	names := []string{FakeProvider}
	base := strings.TrimRight(c.serverDomain, "/")
	if c.serverDomain == "" {
		base = strings.TrimRight(baseURL, "/")
	}
	if len(settings.RuleSetIDs) > 0 && isAuthEnabled() && c.configToken == "" {
		return gatewayError(msgTokenRequired)
	}
	for _, ruleID := range settings.RuleSetIDs {
		name := ProviderName(ruleID)
		url, err := c.tokenURL(base + c.ProviderPath(ruleID, false))
		if err != nil {
			return err
		}
		yamlx.Set(providers, name, yamlx.Map(
			yamlx.Str("type"), yamlx.Str("http"),
			yamlx.Str("behavior"), yamlx.Str("ipcidr"),
			yamlx.Str("format"), yamlx.Str("yaml"),
			yamlx.Str("url"), yamlx.Str(url),
			yamlx.Str("path"), yamlx.Str("./ruleset/"+name+".yaml"),
			yamlx.Str("interval"), yamlx.Int(RefreshInterval)))
		names = append(names, name)
		item := c.RuleSet(ruleID)
		if item == nil || c.Reason(item) != "" {
			continue
		}
		originalName := source(item, c.Library(item)).name
		if !originalName.present {
			originalName = item.name
		}
		if !originalName.isString {
			continue
		}
		original := yamlx.Get(providers, originalName.text)
		if original == nil || isScalar(original, "!!null") {
			continue
		}
		if !isMap(original) {
			return errProviderNotMapping
		}
		sourceURL, err := c.tokenURL(base + c.ProviderPath(ruleID, true))
		if err != nil {
			return err
		}
		yamlx.Set(original, "url", yamlx.Str(sourceURL))
		yamlx.Set(original, "format", yamlx.Str("yaml"))
		yamlx.Set(original, "interval", yamlx.Int(RefreshInterval))
	}
	yamlx.Set(tun, "route-address-set", strSeq(names))
	yamlx.Set(doc, "tun", tun)
	yamlx.Set(doc, "ipv6", yamlx.Bool(false))
	yamlx.Set(doc, "mode", yamlx.Str("rule"))

	dns := yamlx.Map()
	if current := yamlx.Get(doc, "dns"); truthy(current) {
		dns = cloneShared(current)
	}
	yamlx.Set(dns, "enable", yamlx.Bool(true))
	yamlx.Set(dns, "listen", yamlx.Str("127.0.0.1:1053"))
	yamlx.Set(dns, "enhanced-mode", yamlx.Str("fake-ip"))
	yamlx.Set(dns, "ipv6", yamlx.Bool(false))
	setDefault(dns, "fake-ip-range", yamlx.Str(DefaultFakeIPRange))
	yamlx.Set(doc, "dns", dns)
	yamlx.Set(doc, MarkerKey, yamlx.Map(
		yamlx.Str("enabled"), yamlx.Bool(true),
		yamlx.Str("version"), yamlx.Int(1),
		yamlx.Str("provider_prefix"), yamlx.Str(ProviderPrefix),
		yamlx.Str("profile_id"), yamlx.Str(c.profileID.or(""))))
	return nil
}

// tokenURL adds the subscription token (config_token) to a panel URL.
func (c *Config) tokenURL(url string) (string, error) {
	if c.configToken == "" {
		return url, nil
	}
	return convert.AppendURLQuery(url, [][2]any{{"token", c.configToken}})
}
