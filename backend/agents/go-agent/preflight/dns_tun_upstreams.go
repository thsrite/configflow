package preflight

import (
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Mihomo answers a TUN DNS query itself when a dns-hijack entry matches it,
// and an entry with an unspecified address (any:1053 included) matches every
// destination on port 53, whatever port the entry names. A same-host OxiDNS
// whose plain-DNS upstream is not routed around the TUN therefore receives
// fake-ip answers instead of the upstream it was configured to ask.
//
// Remote and backup forwards are left alone: answering them through Mihomo is
// what they approximate today, while a direct query would be poisoned.
var tunUpstreamRemoteForwards = map[string]bool{"forward_remote": true, "forward_fallback": true}

func tunHijackEntries(tun map[string]interface{}) []netip.AddrPort {
	var entries []netip.AddrPort
	for _, value := range dnsStrings(tun["dns-hijack"]) {
		if _, after, ok := strings.Cut(value, "://"); ok {
			value = after
		}
		value = strings.Replace(value, "any", "0.0.0.0", 1)
		if entry, err := netip.ParseAddrPort(value); err == nil {
			entries = append(entries, entry)
		}
	}
	if tunMarkModeHijacksLocalDNS(tun) {
		entries = append(entries, netip.AddrPortFrom(netip.IPv4Unspecified(), 53))
	}
	return entries
}

// A route-address-set or route-exclude-address-set puts auto-redirect into
// mark mode. sing-tun then also DNATs this host's own port-53 traffic (except
// to route-exclude-address and loopback) to Mihomo's DNS, whatever dns-hijack
// says; the Linux gateway always works this way. Without those sets the host's
// queries only meet Mihomo inside the TUN, where dns-hijack alone decides.
func tunMarkModeHijacksLocalDNS(tun map[string]interface{}) bool {
	if tun["auto-route"] != true || tun["auto-redirect"] != true {
		return false
	}
	if mode, _ := tun["dns-mode"].(string); mode != "" && mode != "hijack" {
		return false
	}
	return len(dnsStrings(tun["route-address-set"])) > 0 || len(dnsStrings(tun["route-exclude-address-set"])) > 0
}

func tunHijacks(entries []netip.AddrPort, target netip.AddrPort) bool {
	for _, entry := range entries {
		if entry == target || (entry.Addr().IsUnspecified() && target.Port() == 53) {
			return true
		}
	}
	return false
}

// plainDNSEndpoint splits an OxiDNS plain UDP/TCP upstream into host and port.
func plainDNSEndpoint(raw string) (string, uint16, bool) {
	address := raw
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil || (parsed.Scheme != "udp" && parsed.Scheme != "tcp") {
			return "", 0, false
		}
		address = parsed.Host
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		host, portText = strings.Trim(address, "[]"), "53"
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 || host == "" {
		return "", 0, false
	}
	return host, uint16(port), true
}

func plainDNSAddress(raw string) (netip.AddrPort, bool) {
	host, port, ok := plainDNSEndpoint(raw)
	ip, err := netip.ParseAddr(host)
	if !ok || err != nil {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(ip.Unmap(), port), true
}

// TUNHijackedOxiDNSUpstreams lists the OxiDNS upstream and bootstrap addresses
// that the given enabled Mihomo TUN would hijack. Loopback addresses never
// enter the TUN and are omitted.
func TUNHijackedOxiDNSUpstreams(oxidns, tun map[string]interface{}) []netip.Addr {
	entries := tunHijackEntries(tun)
	if len(entries) == 0 {
		return nil
	}
	found := map[netip.Addr]bool{}
	add := func(target netip.AddrPort) {
		if !target.Addr().IsLoopback() && tunHijacks(entries, target) {
			found[target.Addr()] = true
		}
	}
	plugins, _ := oxidns["plugins"].([]interface{})
	for _, item := range plugins {
		plugin, _ := item.(map[string]interface{})
		if kind, _ := plugin["type"].(string); kind != "forward" {
			continue
		}
		tag, _ := plugin["tag"].(string)
		args, _ := plugin["args"].(map[string]interface{})
		upstreams, _ := args["upstreams"].([]interface{})
		for _, value := range upstreams {
			upstream, _ := value.(map[string]interface{})
			// Bootstrap answers locate the upstream itself; a fake-ip there is
			// never what any forward wants.
			if bootstrap, _ := upstream["bootstrap"].(string); bootstrap != "" {
				if target, ok := plainDNSAddress(bootstrap); ok {
					add(target)
				}
			}
			if tunUpstreamRemoteForwards[tag] {
				continue
			}
			addr, _ := upstream["addr"].(string)
			host, port, ok := plainDNSEndpoint(addr)
			if !ok {
				continue
			}
			// dial_addr and port override where a plain upstream connects.
			if dial, _ := upstream["dial_addr"].(string); dial != "" {
				host = dial
			}
			if value, ok := upstream["port"].(int); ok && value > 0 && value <= 65535 {
				port = uint16(value)
			}
			if ip, err := netip.ParseAddr(host); err == nil {
				add(netip.AddrPortFrom(ip.Unmap(), port))
			}
		}
	}
	result := make([]netip.Addr, 0, len(found))
	for address := range found {
		result = append(result, address)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Less(result[j]) })
	return result
}

// PrefixesCover reports whether one of the CIDR strings contains the address.
func PrefixesCover(values []interface{}, address netip.Addr) bool {
	for _, value := range values {
		text, _ := value.(string)
		if prefix, err := netip.ParsePrefix(text); err == nil && prefix.Masked().Contains(address) {
			return true
		}
	}
	return false
}
