package gateway

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

// DNSLoopMatch identifies the first upstream that resolves through the local
// host. The original text and field are retained for the form's diagnostic.
type DNSLoopMatch struct{ Key, Upstream string }

// MihomoDNSLoop checks only dns and the supplied host addresses. It performs no
// I/O and needs no onboarding/session/device state. A21 reuses this function.
func MihomoDNSLoop(dns *yamlx.Node, hosts []netip.Addr) *DNSLoopMatch {
	for _, key := range []string{"nameserver", "fallback", "default-nameserver", "proxy-server-nameserver", "direct-nameserver", "nameserver-policy", "proxy-server-nameserver-policy"} {
		value := yamlx.Resolve(yamlx.Get(dns, key))
		pending := []*yamlx.Node{value}
		if value != nil && value.Kind == yaml.MappingNode {
			pending = nil
			for _, item := range yamlx.Pairs(value) {
				pending = append(pending, item)
			}
		}
		seen := map[*yamlx.Node]bool{}
		for len(pending) > 0 {
			last := len(pending) - 1
			n := yamlx.Resolve(pending[last])
			pending = pending[:last]
			if n == nil || seen[n] {
				continue
			}
			seen[n] = true
			if n.Kind == yaml.SequenceNode {
				pending = append(pending, n.Content...)
				continue
			}
			if n.Kind != yaml.ScalarNode || n.ShortTag() != "!!str" {
				continue
			}
			if localDNSUpstream(n.Value, hosts) {
				return &DNSLoopMatch{key, n.Value}
			}
		}
	}
	return nil
}
func localDNSUpstream(upstream string, hosts []netip.Addr) bool {
	raw, _, _ := strings.Cut(upstream, "#")
	target := raw
	if !strings.Contains(target, "://") {
		target = "udp://" + target
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return false
	}
	hostname := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if port != "" {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return false
		}
	} else if parsed.Scheme == "udp" || parsed.Scheme == "tcp" {
		port = "53"
	}
	local, matches := hostname == "localhost", false
	if addr, err := netip.ParseAddr(hostname); err == nil {
		local = addr.IsLoopback() || addr.IsUnspecified() || addr.Is4In6() && addr.Unmap().IsLoopback()
		for _, host := range hosts {
			if addr == host || addr.Is4In6() && addr.Unmap() == host {
				matches = true
				break
			}
		}
	}
	return local || matches && (port == "53" || port == "1053") || strings.TrimSpace(raw) == "system" || strings.TrimSpace(raw) == "system://"
}
