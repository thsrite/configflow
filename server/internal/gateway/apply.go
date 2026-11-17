package gateway

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/gateway/gwbase"

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
	value = resolve(value)
	if value == nil {
		return ""
	}
	switch value.Kind {
	case yaml.MappingNode:
		if mark := get(value, "routing-mark"); mark != nil && !unset(mark) {
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
	if key = resolve(key); key == nil {
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
	tun := mapNode()
	if current := get(doc, "tun"); truthy(current) {
		if !isMap(current) {
			return gatewayError(msgTunNotObject)
		}
		tun = clone(current)
	}
	for _, key := range tunFilterKeys {
		if truthy(get(tun, key)) {
			return gatewayError(fmt.Sprintf(fmtTunFilter, key))
		}
	}
	for _, key := range routeKeys {
		remove(tun, key)
	}
	set(tun, "enable", boolNode(true))
	set(tun, "auto-route", boolNode(true))
	set(tun, "auto-redirect", boolNode(true))
	set(tun, "auto-detect-interface", boolNode(true))
	set(tun, "strict-route", boolNode(false))
	set(tun, "dns-hijack", strSeq(nil))
	setDefault(tun, "stack", strNode("mixed"))
	setDefault(tun, "device", strNode("Mihomo"))

	providers := setDefault(doc, "rule-providers", mapNode())
	if !isMap(providers) {
		return gatewayError(msgProvidersNotObject)
	}
	reserved := false
	pairs(providers, func(name string, _ *yaml.Node) bool {
		reserved = strings.HasPrefix(name, ProviderPrefix)
		return !reserved
	})
	if reserved {
		return gatewayError(msgReservedPrefix)
	}
	set(providers, FakeProvider, mapNode("type", strNode("inline"), "behavior", strNode("ipcidr"),
		"format", strNode("yaml"), "payload", strSeq([]string{fakeRange.String()})))
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
		set(providers, name, mapNode("type", strNode("http"), "behavior", strNode("ipcidr"), "format", strNode("yaml"),
			"url", strNode(url), "path", strNode("./ruleset/"+name+".yaml"), "interval", intNode(RefreshInterval)))
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
		original := get(providers, originalName.text)
		if original == nil || isScalar(original, tagNull) {
			continue
		}
		if !isMap(original) {
			return errProviderNotMapping
		}
		sourceURL, err := c.tokenURL(base + c.ProviderPath(ruleID, true))
		if err != nil {
			return err
		}
		set(original, "url", strNode(sourceURL))
		set(original, "format", strNode("yaml"))
		set(original, "interval", intNode(RefreshInterval))
	}
	set(tun, "route-address-set", strSeq(names))
	set(doc, "tun", tun)
	set(doc, "ipv6", boolNode(false))
	set(doc, "mode", strNode("rule"))

	dns := mapNode()
	if current := get(doc, "dns"); truthy(current) {
		dns = clone(current)
	}
	set(dns, "enable", boolNode(true))
	set(dns, "listen", strNode("127.0.0.1:1053"))
	set(dns, "enhanced-mode", strNode("fake-ip"))
	set(dns, "ipv6", boolNode(false))
	setDefault(dns, "fake-ip-range", strNode(DefaultFakeIPRange))
	set(doc, "dns", dns)
	set(doc, MarkerKey, mapNode("enabled", boolNode(true), "version", intNode(1),
		"provider_prefix", strNode(ProviderPrefix), "profile_id", strNode(c.profileID.or(""))))
	return nil
}

// tokenURL adds the subscription token (config_token) to a panel URL.
func (c *Config) tokenURL(url string) (string, error) {
	if c.configToken == "" {
		return url, nil
	}
	return convert.AppendURLQuery(url, [][2]any{{"token", c.configToken}})
}
