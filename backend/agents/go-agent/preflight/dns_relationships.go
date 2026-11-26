package preflight

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type dnsUpstream struct {
	binding portBinding
	known   bool
	direct  bool
}
type dnsGroup struct {
	upstreams []dnsUpstream
	definite  bool
}

// Only plain DNS endpoints can be compared with UDP/TCP listeners. Hostnames,
// encrypted transports, proxy fragments and complex routing remain unproven.
func dnsParseUpstream(raw string) dnsUpstream {
	result := dnsUpstream{direct: true}
	if strings.Contains(raw, "#") {
		result.direct = false
		raw = strings.SplitN(raw, "#", 2)[0]
	}
	protocol := "udp"
	address := raw
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return result
		}
		if parsed.Scheme != "udp" && parsed.Scheme != "tcp" {
			return result
		}
		protocol = parsed.Scheme
		address = parsed.Host
		if parsed.User != nil || parsed.RawQuery != "" {
			result.direct = false
		}
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		host = address
		portText = "53"
		if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
			host = strings.Trim(host, "[]")
		}
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return result
	}
	if host == "localhost" {
		host = "127.0.0.1"
		result.direct = false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return result
	}
	result.binding = portBinding{Protocol: protocol, Address: ip.Unmap(), Port: port}
	result.known = true
	return result
}

func dnsStrings(value any) []string {
	if text, ok := value.(string); ok {
		return []string{text}
	}
	var result []string
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if text, ok := item.(string); ok {
				result = append(result, text)
			} else {
				result = append(result, "")
			}
		}
	}
	return result
}

func dnsGroups(cfg Config, doc map[string]any) ([]dnsGroup, bool) {
	if cfg.ServiceType == "mihomo" {
		dns, _ := doc["dns"].(map[string]any)
		enabled, _ := dns["enable"].(bool)
		if !enabled {
			return nil, false
		}
		policy, _ := dns["nameserver-policy"].(map[string]any)
		values := append(dnsStrings(dns["nameserver"]), dnsStrings(dns["fallback"])...)
		respectRules, _ := dns["respect-rules"].(bool)
		group := dnsGroup{definite: len(policy) == 0 && !respectRules}
		for _, value := range values {
			group.upstreams = append(group.upstreams, dnsParseUpstream(value))
		}
		var groups []dnsGroup
		if len(group.upstreams) > 0 {
			groups = append(groups, group)
		}
		for _, value := range policy {
			g := dnsGroup{}
			for _, text := range dnsStrings(value) {
				g.upstreams = append(g.upstreams, dnsParseUpstream(text))
			}
			if len(g.upstreams) > 0 {
				groups = append(groups, g)
			}
		}
		return groups, false
	}
	plugins, _ := doc["plugins"].([]any)
	tags := map[string]map[string]any{}
	for _, item := range plugins {
		plugin, _ := item.(map[string]any)
		tag, _ := plugin["tag"].(string)
		if tag != "" {
			tags[tag] = plugin
		}
	}
	var groups []dnsGroup
	unknown := false
	var walk func(string, bool, map[string]bool, int)
	walk = func(tag string, definite bool, seen map[string]bool, depth int) {
		if depth > 16 || seen[tag] {
			unknown = true
			return
		}
		plugin, ok := tags[tag]
		if !ok {
			unknown = true
			return
		}
		seen[tag] = true
		defer delete(seen, tag)
		kind, _ := plugin["type"].(string)
		if kind == "forward" {
			args, _ := plugin["args"].(map[string]any)
			upstreams, _ := args["upstreams"].([]any)
			group := dnsGroup{definite: definite}
			for _, item := range upstreams {
				upstream, _ := item.(map[string]any)
				address, _ := upstream["addr"].(string)
				parsed := dnsParseUpstream(address)
				for _, field := range []string{"dialer", "dial_addr", "outbound", "socks5", "proxy"} {
					if value, _ := upstream[field].(string); value != "" {
						parsed.direct = false
					}
				}
				if dial, _ := upstream["dial_addr"].(string); dial != "" {
					parsed.known = false
					unknown = true
				}
				if port, present := upstream["port"]; present && port != nil && port != 0 {
					parsed.known = false
					unknown = true
				}
				group.upstreams = append(group.upstreams, parsed)
			}
			if len(group.upstreams) == 0 {
				unknown = true
			} else {
				groups = append(groups, group)
			}
			return
		}
		if kind != "sequence" {
			unknown = true
			return
		}
		steps, ok := plugin["args"].([]any)
		if !ok {
			unknown = true
			return
		}
		// Proof is intentionally narrow: a direct forward (possibly wrapped in
		// sequences), followed by accept. Conditional/other steps are only warnings.
		certain := definite
		for _, item := range steps {
			step, ok := item.(map[string]any)
			if !ok {
				unknown = true
				certain = false
				continue
			}
			conditional := false
			if matches, present := step["matches"]; present && matches != nil {
				conditional = true
				certain = false
			}
			exec, _ := step["exec"].(string)
			if exec == "accept" || exec == "return" {
				if !conditional {
					return
				}
				continue
			}
			next := ""
			if strings.HasPrefix(exec, "$") {
				next = strings.TrimPrefix(exec, "$")
			} else if fields := strings.Fields(exec); len(fields) == 2 && (fields[0] == "jump" || fields[0] == "goto") {
				next = fields[1]
			}
			if next != "" {
				walk(next, certain, seen, depth+1)
				certain = false
			} else {
				unknown = true
				certain = false
			}
		}
	}
	entries, listeners := map[string]bool{}, map[string]bool{}
	for _, item := range plugins {
		plugin, _ := item.(map[string]any)
		kind, _ := plugin["type"].(string)
		if kind != "udp_server" && kind != "tcp_server" {
			continue
		}
		args, _ := plugin["args"].(map[string]any)
		entry, _ := args["entry"].(string)
		entries[entry] = true
		for _, listen := range dnsStrings(args["listen"]) {
			listeners[listen] = true
		}
	}
	// Multiple entrypoints can intentionally chain through another listener in
	// the same process. Without an endpoint-specific graph, references alone
	// cannot prove that the same query returns to its original entrypoint.
	unambiguous := len(entries) == 1 && len(listeners) == 1
	for entry := range entries {
		if entry == "" {
			unknown = true
			continue
		}
		walk(entry, unambiguous, map[string]bool{}, 0)
	}
	return groups, unknown
}

func dnsEndpointMatches(upstream dnsUpstream, requirements []portRequirement) bool {
	if !upstream.known {
		return false
	}
	for _, requirement := range requirements {
		if (!requirement.Address.IsValid() || requirement.Address.IsUnspecified()) && !dnsLocalAddress(upstream.binding.Address) {
			continue
		}
		if portBindingsOverlap(upstream.binding, requirement.portBinding) {
			return true
		}
	}
	return false
}

// A wildcard listener only accepts this host's addresses; it does not make a
// remote public resolver an upstream pointing back to ourselves.
func dnsLocalAddress(ip netip.Addr) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	addresses, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, address := range addresses {
		prefix, err := netip.ParsePrefix(address.String())
		if err == nil && prefix.Addr().Unmap() == ip.Unmap() {
			return true
		}
	}
	return false
}

func dnsGroupReferences(group dnsGroup, requirements []portRequirement) (some, all bool) {
	all = group.definite && len(group.upstreams) > 0
	for _, upstream := range group.upstreams {
		matches := dnsEndpointMatches(upstream, requirements)
		some = some || matches
		all = all && matches && upstream.direct
	}
	return
}

func dnsSelfChecks(cfg Config, doc map[string]any, requirements []portRequirement) []Check {
	groups, _ := dnsGroups(cfg, doc)
	var checks []Check
	for _, group := range groups {
		some, all := dnsGroupReferences(group, requirements)
		if !some {
			continue
		}
		level := "warn"
		message := cfg.ServiceType + " 的部分 DNS 上游指向自身监听，可能形成循环；需结合条件分流确认"
		if all {
			level = "error"
			message = cfg.ServiceType + " 的无条件 DNS 转发全部指向自身监听，形成直接循环"
		}
		checks = append(checks, Check{Name: "DNS 循环转发", Level: level, Message: message, Fix: "将对应查询转发到独立的 DNS 上游，避免请求重新进入自身解析入口。"})
	}
	return checks
}

func dnsRelationshipChecks(candidate DNSCandidate, doc map[string]any, requirements []portRequirement, peer DNSCandidate, peerDoc map[string]any, peerRequirements []portRequirement) []Check {
	checks := dnsSelfChecks(peer.Config, peerDoc, peerRequirements)
	// The peer is not part of this transaction. Blocking on its pre-existing
	// self-loop would prevent repairing either core when both are broken.
	// Candidate errors and loops caused by the resulting pair still block below.
	for i := range checks {
		if checks[i].Level == "error" {
			checks[i].Level = "warn"
			checks[i].Message = "对端现有配置存在问题：" + checks[i].Message + "；请随后修复对端配置，本次发布不会修改对端。"
		}
	}
	groups, _ := dnsGroups(candidate.Config, doc)
	peerGroups, _ := dnsGroups(peer.Config, peerDoc)
	somePeer, allPeer, someBack, allBack := false, false, false, false
	for _, group := range groups {
		some, all := dnsGroupReferences(group, peerRequirements)
		somePeer = somePeer || some
		allPeer = allPeer || all
	}
	for _, group := range peerGroups {
		some, all := dnsGroupReferences(group, requirements)
		someBack = someBack || some
		allBack = allBack || all
	}
	if somePeer && someBack {
		level := "warn"
		message := "两套 DNS 存在相互转发引用；条件分流可能不同，需验证是否产生循环"
		if allPeer && allBack {
			level = "error"
			message = "两套 DNS 的无条件转发全部指向对端，形成循环"
		}
		checks = append(checks, Check{Name: "DNS 上下游循环", Level: level, Message: message, Fix: "为相应查询配置独立出口，避免 OxiDNS 与 Mihomo 将同一请求交回对方。"})
	} else if somePeer || someBack {
		checks = append(checks, Check{Name: "DNS 上下游地址", Level: "pass", Message: "已识别的本机 DNS 上游与对端监听地址、端口对应；条件分流效果仍需运行验证"})
	}
	checks = append(checks, dnsUnmatchedLocalUpstreams(candidate.Config, groups, requirements, peer.Config, peerRequirements)...)
	checks = append(checks, dnsUnmatchedLocalUpstreams(peer.Config, peerGroups, peerRequirements, candidate.Config, requirements)...)
	// This intentionally checks recognizable local DNS relationships rather than
	// proving every plugin and branch. Common cache/fallback/extension plugins
	// alone are not actionable risks and must not create generic warnings.
	// Concrete local references still produce the targeted warnings above.
	return checks
}

func dnsUnmatchedLocalUpstreams(cfg Config, groups []dnsGroup, own []portRequirement, peer Config, other []portRequirement) []Check {
	var checks []Check
	seen := map[portBinding]bool{}
	for _, group := range groups {
		for _, upstream := range group.upstreams {
			if !upstream.known || !dnsLocalAddress(upstream.binding.Address) || dnsEndpointMatches(upstream, own) || dnsEndpointMatches(upstream, other) || seen[upstream.binding] {
				continue
			}
			seen[upstream.binding] = true
			// Loopback alone does not identify the intended service. An arbitrary local
			// resolver can be intentional, so a mismatched port is a warning, not proof.
			message := fmt.Sprintf("%s 的本机 DNS 上游 %s/%d 未匹配这两套服务的监听；可能是其他本机 DNS 或端口配置不一致", cfg.ServiceType, upstream.binding.Protocol, upstream.binding.Port)
			if len(other) == 0 {
				message += fmt.Sprintf("；%s 当前未配置外部 DNS 监听", peer.ServiceType)
			}
			checks = append(checks, Check{Name: "DNS 上下游待确认", Level: "warn", Message: message, Fix: "如果此上游应指向对端，请同步监听地址、TCP/UDP 协议和端口，并确认对端 DNS 已启用。"})
		}
	}
	return checks
}
