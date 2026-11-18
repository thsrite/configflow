package ipv6

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

func field(n *yamlx.Node, keys ...string) *yamlx.Node {
	for _, k := range keys {
		n = yamlx.Get(n, k)
	}
	return yamlx.Resolve(n)
}
func scalar(n *yamlx.Node) string {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return n.Value
	}
	return ""
}
func boolean(n *yamlx.Node) bool { return n != nil && n.ShortTag() == "!!bool" && n.Value == "true" }
func jsonNode(n *yamlx.Node) json.RawMessage {
	if n == nil {
		return raw(nil)
	}
	b, e := yamlx.ToJSON(n)
	if e != nil {
		return raw(nil)
	}
	return b
}

// ReadConfiguration parses saved YAML through the shared facade. IPv6 host bits
// are masked before passing the range to the R11 router inspector.
func ReadConfiguration(config Document) (Configuration, *yamlx.Node) {
	fallback := func() (Configuration, *yamlx.Node) {
		return Configuration{IPv6: raw(nil), DNSIPv6: raw(nil), Errors: []string{msgParse}, Source: "saved_draft"}, yamlx.Map()
	}
	c := Configuration{IPv6: raw(true), DNSIPv6: raw(false), Errors: []string{}, Source: "saved_draft"}
	var source string
	value := object(config["mihomo"])["custom_config"]
	if len(value) > 0 && string(value) != "null" {
		if json.Unmarshal(value, &source) != nil {
			return fallback()
		}
	}
	var doc *yamlx.Node
	var err error
	if strings.TrimSpace(source) == "" {
		doc, err = defaultMihomo()
	} else {
		doc, err = yamlx.Parse([]byte(source))
	}
	if err != nil || doc.Kind != yaml.MappingNode {
		return fallback()
	}
	dns := field(doc, "dns")
	if dns != nil && dns.Kind != yaml.MappingNode {
		return fallback()
	}
	top := field(doc, "ipv6")
	v6 := field(dns, "ipv6")
	c.IPv6Explicit = ptr(top != nil)
	c.DNSIPv6Explicit = ptr(v6 != nil)
	if top != nil {
		c.IPv6 = jsonNode(top)
		if top.ShortTag() != "!!bool" {
			c.Errors = append(c.Errors, fmt.Sprintf(msgBoolean, "ipv6"))
		}
	}
	if v6 != nil {
		c.DNSIPv6 = jsonNode(v6)
		if v6.ShortTag() != "!!bool" {
			c.Errors = append(c.Errors, fmt.Sprintf(msgBoolean, "dns.ipv6"))
		}
	}
	fake := field(dns, "fake-ip-range6")
	c.FakeIPv6Range = jsonNode(fake)
	hasFake := fake != nil && fake.ShortTag() != "!!null" && !(fake.ShortTag() == "!!bool" && fake.Value == "false") && !(fake.ShortTag() == "!!int" && fake.Value == "0") && !(fake.ShortTag() == "!!str" && fake.Value == "")
	if hasFake {
		prefix, e := netip.ParsePrefix(scalar(fake))
		if e != nil || !prefix.Addr().Is6() || prefix.Addr().Is4In6() {
			return fallback()
		}
		prefix = prefix.Masked()
		if prefix.Bits() < 7 || !prefix.Addr().IsPrivate() {
			c.Errors = append(c.Errors, msgULA)
		}
		c.FakeIPv6Range = raw(prefix.String())
	}
	c.TUNEnabled = boolean(field(doc, "tun", "enable"))
	c.FakeIPv6Route = yes(c.IPv6) && boolean(field(dns, "enable")) && scalar(field(dns, "enhanced-mode")) == "fake-ip" && hasFake
	c.FakeIPv6Enabled = c.FakeIPv6Route && yes(c.DNSIPv6)
	return c, doc
}

// ProbeTargets takes at most three saved supported TCP nodes. Literal IPv6
// precedes hostnames; IPv4, indirect and disabled nodes are not probed.
func ProbeTargets(config Document, doc *yamlx.Node) []ProbeTarget {
	candidates := []Document{}
	if proxies := field(doc, "proxies"); proxies != nil && proxies.Kind == yaml.SequenceNode {
		for _, node := range proxies.Content {
			if node.Kind == yaml.MappingNode {
				candidates = append(candidates, object(jsonNode(node)))
			}
		}
	}
	var nodes []json.RawMessage
	_ = json.Unmarshal(config["nodes"], &nodes)
	for _, value := range nodes {
		node := object(value)
		if node == nil || string(node["enabled"]) == "false" {
			continue
		}
		if source := text(node["proxy_string"]); source != "" {
			n, e := yamlx.Parse([]byte(source))
			if e == nil && n.Kind == yaml.MappingNode {
				candidates = append(candidates, object(jsonNode(n)))
			}
		} else {
			combined := object(raw(node))
			for k, v := range object(node["params"]) {
				combined[k] = v
			}
			candidates = append(candidates, combined)
		}
	}
	out := []ProbeTarget{}
	for _, node := range candidates {
		kind := strings.ToLower(text(node["type"]))
		if !slices.Contains([]string{"socks5", "http", "ss", "ssr", "vmess", "vless", "trojan", "anytls"}, kind) || indirect(node["dialer-proxy"]) {
			continue
		}
		host := text(node["server"])
		var port int
		if host == "" || json.Unmarshal(node["port"], &port) != nil || port < 1 || port > 65535 {
			continue
		}
		host = strings.Trim(host, "[]")
		if host == "" {
			continue
		}
		ip, e := netip.ParseAddr(host)
		if e == nil && !ip.Is6() {
			continue
		}
		out = append(out, ProbeTarget{host, port, e == nil})
	}
	slices.SortStableFunc(out, func(a, b ProbeTarget) int {
		if a.Literal == b.Literal {
			return 0
		}
		if a.Literal {
			return -1
		}
		return 1
	})
	if len(out) > 3 {
		out = out[:3]
	}
	return out
}
func prepareConfig(config Document) Document {
	next := object(raw(config))
	for _, key := range []string{"agents", "backup", "system_config", "created_at", "updated_at", "_revision"} {
		delete(next, key)
	}
	return next
}

// A malformed nonempty dialer selection is not a directly reachable node.
func indirect(b json.RawMessage) bool {
	return len(b) > 0 && string(b) != "null" && string(b) != `""` && string(b) != "false" && string(b) != "0"
}
