package routers

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/model"
)

// PolicyEnvironment keeps IPv4/IPv6 packet-path tables and shared routing rules.
// Nil means missing/read failure; an empty non-nil slice is a successful read.
type PolicyEnvironment struct {
	IPv6         PolicySnapshot
	IPv4         PolicySnapshot
	RoutingRules []Row
}

// PolicyTablePaths returns fresh table metadata for the R11 readiness reader.
func PolicyTablePaths() map[string]string {
	return map[string]string{"raw": "/ipv6/firewall/raw", "mangle": "/ipv6/firewall/mangle", "address_lists": "/ipv6/firewall/address-list", "ipv4_routes": "/ip/route", "ipv4_filter": "/ip/firewall/filter", "ipv4_nat": "/ip/firewall/nat", "ipv4_raw": "/ip/firewall/raw", "ipv4_mangle": "/ip/firewall/mangle", "ipv4_address_lists": "/ip/firewall/address-list", "interface_lists": "/interface/list", "interface_members": "/interface/list/member", "interfaces": "/interface", "routing_rules": "/routing/rule"}
}

// PolicyEnvironmentFromSnapshot converts R11's read results without treating a
// failed or missing table as proof of absence.
func PolicyEnvironmentFromSnapshot(snapshot map[string][]Row, errors []string) PolicyEnvironment {
	read := func(key string) []Row {
		if slices.Contains(errors, key) {
			return nil
		}
		return snapshot[key]
	}
	family := func(prefix string, n int) PolicySnapshot {
		routes := "routes"
		if n == 4 {
			routes = "ipv4_routes"
		}
		return PolicySnapshot{Family: n, Raw: read(prefix + "raw"), Mangle: read(prefix + "mangle"), NAT: read(prefix + "nat"), Filter: read(prefix + "filter"), Routes: read(routes), AddressLists: read(prefix + "address_lists"), InterfaceLists: read("interface_lists"), InterfaceMembers: read("interface_members"), Interfaces: read("interfaces")}
	}
	return PolicyEnvironment{family("", 6), family("ipv4_", 4), read("routing_rules")}
}

// PolicyNodeTarget is a supplied literal node endpoint, never resolved by DNS.
type PolicyNodeTarget struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// PolicyClient is a supplied client address, optionally an unobserved candidate.
type PolicyClient struct {
	Address         string `json:"address"`
	SourceCandidate bool   `json:"source_candidate"`
	SourceNote      string `json:"source_note"`
}

// PolicyContext supplies actual endpoints and the configured IPv4 Fake-IP range.
type PolicyContext struct {
	NodeTargets   []PolicyNodeTarget `json:"node_targets,omitempty"`
	Clients       []PolicyClient     `json:"clients,omitempty"`
	FakeIPv4Range string             `json:"fake_ipv4_range"`
}

// PolicyConfiguration is the saved configuration required by static diagnostics.
type PolicyConfiguration struct {
	FakeIPv6Enabled bool   `json:"fake_ipv6_enabled"`
	FakeIPv6Range   string `json:"fake_ipv6_range"`
}

// PolicyCheck is one conservative report; Details are deduplicated and capped at 40.
type PolicyCheck struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Status   string   `json:"status"`
	Decision string   `json:"decision"`
	Message  string   `json:"message"`
	Details  []string `json:"details"`
}

func policyReport(key, label, decision, message string, details []string) PolicyCheck {
	statuses := map[string]string{"allow": "ok", "drop": "error", "translate": "warning", "unknown": "unknown"}
	details = uniqueStrings(details)
	return PolicyCheck{key, label, statuses[decision], decision, message, details[:min(40, len(details))]}
}
func policyRouteInterface(s *PolicySnapshot, destination string) (string, string) {
	target, ok := policyNetwork(destination)
	if !ok || s.Routes == nil {
		return "", messagePolicyRouteIncomplete
	}
	best := -1
	matches := []Row{}
	partial := []int{}
	for _, row := range s.Routes {
		network, ok := policyNetwork(row.text("dst-address"))
		if !policyEnabled(row) || !policyFlag(row["active"]) || row.textOr("routing-table", "main") != "main" || !ok || network.Addr().BitLen() != target.Addr().BitLen() {
			continue
		}
		if target.Bits() >= network.Bits() && network.Contains(target.Addr()) {
			if network.Bits() > best {
				best = network.Bits()
				matches = nil
			}
			if network.Bits() == best {
				matches = append(matches, row)
			}
		} else if target.Overlaps(network) {
			partial = append(partial, network.Bits())
		}
	}
	if len(matches) == 0 {
		return "", messagePolicyRouteMissing
	}
	for _, n := range partial {
		if n > best {
			return "", messagePolicyRoutePartial
		}
	}
	interfaces := map[string]bool{}
	for _, row := range matches {
		for _, key := range []string{"blackhole", "unreachable", "prohibit"} {
			if policyFlag(row[key]) {
				return "", messagePolicyRouteDiscard
			}
		}
		gateway := row.text("immediate-gw")
		if gateway == "" {
			gateway = row.text("gateway")
		}
		if strings.Contains(gateway, ",") || gateway == "" {
			return "", messagePolicyRouteAmbiguous
		}
		iface := ""
		if index := strings.LastIndex(gateway, "%"); index >= 0 {
			iface = gateway[index+1:]
		} else if _, ok := policyNetwork(gateway); !ok {
			iface = gateway
		} else {
			return "", messagePolicyRouteUnresolved
		}
		interfaces[iface] = true
	}
	if len(interfaces) == 1 {
		for iface := range interfaces {
			return iface, messagePolicyRouteChosen
		}
	}
	return "", messagePolicyRouteMultiple
}

type policyPathResult struct {
	PolicyDecision
	NATDecision string
}

func policyPath(s *PolicySnapshot, chain string, p PolicyPacket) policyPathResult {
	before := "prerouting"
	if chain == "output" {
		before = "output"
	}
	stages := [][2]string{{"raw", before}, {"mangle", before}}
	state := ""
	if p.ConnectionState != nil {
		state = *p.ConnectionState
	}
	if chain != "output" && !slices.Contains([]string{"established", "related", "untracked"}, state) {
		stages = append(stages, [2]string{"nat", "dstnat"})
	}
	stages = append(stages, [2]string{"mangle", chain}, [2]string{"filter", chain})
	if chain != "input" {
		stages = append(stages, [2]string{"mangle", "postrouting"})
	}
	if chain != "input" && state == "new" {
		stages = append(stages, [2]string{"nat", "srcnat"})
	}
	evidence := []string{}
	nat := ""
	seen := map[[2]string]bool{}
	for _, stage := range stages {
		if seen[stage] {
			continue
		}
		seen[stage] = true
		result := EvaluatePolicyChain(s, stage[0], stage[1], p)
		evidence = append(evidence, result.Evidence...)
		if stage == [2]string{"nat", "srcnat"} {
			nat = result.Decision
		}
		if result.Decision != "allow" {
			if result.Decision == "translate" && stage[1] != "srcnat" {
				return policyPathResult{PolicyDecision: PolicyDecision{Decision: "unknown", Evidence: append(evidence, messagePolicyDestinationTranslated)}, NATDecision: "unknown"}
			}
			result.Evidence = evidence
			if nat == "" {
				nat = "unknown"
			}
			return policyPathResult{result, nat}
		}
	}
	return policyPathResult{PolicyDecision{Decision: "allow", Evidence: evidence}, nat}
}
func policyRouteUncertain(env PolicyEnvironment, s *PolicySnapshot, p PolicyPacket) bool {
	if env.RoutingRules == nil {
		return true
	}
	for _, row := range env.RoutingRules {
		if !policyEnabled(row) {
			continue
		}
		action := row.textOr("action", "lookup")
		if row.is("table", "main") && (action == "lookup" || action == "lookup-only-in-table") {
			continue
		}
		conditions := Row{}
		for _, key := range []string{"src-address", "dst-address", "interface"} {
			if v, ok := row[key]; ok {
				to := key
				if key == "interface" {
					to = "in-interface"
				}
				conditions[to] = v
			}
		}
		if MatchPolicyRule(s, conditions, p) != PolicyNoMatch {
			return true
		}
	}
	return false
}
func policyFakeGateway(s *PolicySnapshot, target netip.Prefix, agent *model.Agent, host IPv6Host) bool {
	known := map[string]bool{}
	if s.Family == 4 {
		if agent != nil {
			if n, ok := policyNetwork(agent.Host); ok {
				known[n.Addr().String()] = true
			}
		}
	} else {
		for _, item := range host.Addresses {
			if n, ok := policyNetwork(item.Address); ok && n.Addr().Is6() && n.Bits() == 128 && !item.Deprecated && !item.Tentative && !item.DADFailed && !item.Temporary {
				known[n.Addr().String()] = true
			}
		}
	}
	best := -1
	matches := []Row{}
	for _, row := range s.Routes {
		n, ok := policyNetwork(row.text("dst-address"))
		if !ok || n.Addr().BitLen() != target.Addr().BitLen() || target.Bits() < n.Bits() || !n.Contains(target.Addr()) || !policyEnabled(row) || !policyFlag(row["active"]) || row.textOr("routing-table", "main") != "main" {
			continue
		}
		if n.Bits() > best {
			best = n.Bits()
			matches = nil
		}
		if n.Bits() == best {
			matches = append(matches, row)
		}
	}
	if len(matches) == 0 {
		return false
	}
	for _, row := range matches {
		gw := row.text("immediate-gw")
		if gw == "" {
			gw = row.text("gateway")
		}
		if !known[strings.SplitN(gw, "%", 2)[0]] {
			return false
		}
	}
	return true
}

// packetDetails follows the packet field order used to describe each named flow.
// It formats only the explicitly supplied fields and does not invent unknown data.
func packetDetails(p PolicyPacket, control bool) string {
	values := []string{}
	addString := func(key string, v *string) {
		if v != nil {
			values = append(values, key+"="+*v)
		}
	}
	addInt := func(key string, v *int) {
		if v != nil {
			values = append(values, key+"="+strconv.Itoa(*v))
		}
	}
	addControl := func() {
		addString("protocol", p.Protocol)
		addInt("icmp-type", p.ICMPType)
		addInt("icmp-code", p.ICMPCode)
		addInt("hop-limit", p.HopLimit)
		addString("src-address", p.SrcAddress)
		addString("dst-address", p.DstAddress)
		addInt("src-port", p.SrcPort)
		addInt("dst-port", p.DstPort)
		addString("connection-state", p.ConnectionState)
	}
	if control {
		addControl()
	} else {
		addString("src-address", p.SrcAddress)
		addString("dst-address", p.DstAddress)
		addString("protocol", p.Protocol)
		addInt("src-port", p.SrcPort)
		addInt("dst-port", p.DstPort)
		addInt("icmp-type", p.ICMPType)
		addInt("icmp-code", p.ICMPCode)
		addString("connection-state", p.ConnectionState)
	}
	if p.TCPFlags != nil {
		// This is a fixed descriptive packet field, never a Python value encoder.
		flags := []string{}
		for _, flag := range *p.TCPFlags {
			flags = append(flags, "'"+flag+"'")
		}
		values = append(values, "tcp-flags=["+strings.Join(flags, ", ")+"]")
	}
	addString("in-interface", p.InInterface)
	addString("out-interface", p.OutInterface)
	return strings.Join(values, "；")
}

// PolicyChecks assesses only the supplied node/client endpoints. Static success
// never proves proxy handshakes, conntrack state, return routing or Internet access.
func PolicyChecks(env PolicyEnvironment, agent *model.Agent, host IPv6Host, configuration PolicyConfiguration, lan string, context PolicyContext) []PolicyCheck {
	env.IPv4.Family = 4
	env.IPv6.Family = 6
	checks := []PolicyCheck{}
	traffic, nat := []string{}, []string{}
	scope := messagePolicyScope
	str := func(v string) *string {
		if v == "" {
			return nil
		}
		return new(v)
	}
	flow := func(key, label string, s *PolicySnapshot, p PolicyPacket, chain string, conditional, candidate bool, note string) policyPathResult {
		details := []string{scope, packetDetails(p, chain == "input")}
		if note != "" {
			details = append(details, note)
		}
		var result policyPathResult
		if chain != "input" && policyRouteUncertain(env, s, p) {
			result = policyPathResult{PolicyDecision: PolicyDecision{Decision: "unknown", Evidence: []string{messagePolicyRoutingUncertain}}}
		} else {
			result = policyPath(s, chain, p)
		}
		messages := map[string]string{"allow": messagePolicyAllow, "drop": messagePolicyDrop, "translate": messagePolicyTranslate, "unknown": messagePolicyUnknown}
		report := policyReport(key, label, result.Decision, messages[result.Decision], append(details, result.Evidence...))
		if conditional && report.Status == "error" {
			report.Status = "warning"
			report.Message = messagePolicyConditionalDrop
		}
		if candidate && report.Status == "error" {
			report.Status = "warning"
			report.Message = messagePolicyCandidateDrop
		}
		checks = append(checks, report)
		if !conditional {
			decision := result.Decision
			if candidate && decision == "drop" {
				decision = "unknown"
			}
			traffic = append(traffic, decision)
			if p.ConnectionState != nil && *p.ConnectionState == "new" {
				decision = result.NATDecision
				if decision == "" {
					decision = "unknown"
				}
				nat = append(nat, decision)
			}
		}
		return result
	}
	wan := ""
	for _, row := range env.IPv6.Routes {
		if row.is("dst-address", "::/0") && policyEnabled(row) && policyFlag(row["active"]) {
			wan, _ = policyRouteInterface(&env.IPv6, "::/0")
			break
		}
	}
	controls := []struct {
		key, label string
		p          PolicyPacket
	}{
		{"ra_input", messagePolicyRAInput, PolicyPacket{Protocol: new("icmpv6"), ICMPType: new(134), ICMPCode: new(0), HopLimit: new(255), SrcAddress: new("fe80::/10"), DstAddress: new("ff02::1"), InInterface: str(wan)}},
		{"dhcpv6_input", messagePolicyDHCPInput, PolicyPacket{Protocol: new("udp"), SrcPort: new(547), DstPort: new(546), InInterface: str(wan)}},
		{"rs_input", messagePolicyRSInput, PolicyPacket{Protocol: new("icmpv6"), ICMPType: new(133), ICMPCode: new(0), HopLimit: new(255), InInterface: str(lan)}},
		{"nd_input", messagePolicyNDInput, PolicyPacket{Protocol: new("icmpv6"), ICMPType: new(135), ICMPCode: new(0), HopLimit: new(255), InInterface: str(lan)}},
		{"na_input", messagePolicyNAInput, PolicyPacket{Protocol: new("icmpv6"), ICMPType: new(136), ICMPCode: new(0), HopLimit: new(255), InInterface: str(lan)}},
	}
	for i, title := range []string{messagePolicyUnreachable, messagePolicyMTU, messagePolicyTimeout, messagePolicyParameter} {
		p := PolicyPacket{Protocol: new("icmpv6"), ICMPType: new(i + 1), ConnectionState: new("related"), InInterface: str(wan)}
		if i == 1 {
			p.ICMPCode = new(0)
		}
		controls = append(controls, struct {
			key, label string
			p          PolicyPacket
		}{fmt.Sprintf("icmpv6_%d_input", i+1), fmt.Sprintf(messagePolicyICMPInput, title), p})
	}
	for _, control := range controls {
		control.p.OutInterface = new("")
		flow(control.key, control.label, &env.IPv6, control.p, "input", true, false, "")
	}
	addresses := []string{}
	for _, row := range host.Addresses {
		if n, ok := policyNetwork(row.Address); ok && n.Addr().Is6() && n.Bits() == 128 && row.Usable && !row.Deprecated && !row.Tentative && !row.DADFailed {
			addresses = append(addresses, n.Addr().String())
		}
	}
	targets := []PolicyNodeTarget{}
	for _, item := range context.NodeTargets {
		if n, ok := policyNetwork(item.Address); ok && n.Addr().Is6() && n.Bits() == 128 && item.Port >= 1 && item.Port <= 65535 {
			targets = append(targets, PolicyNodeTarget{n.Addr().String(), item.Port})
		}
	}
	if len(addresses) > 3 || len(targets) > 3 {
		checks = append(checks, policyReport("node_policy_scope", messagePolicyNodeScopeLabel, "unknown", messagePolicyNodeScopeLimit, []string{scope}))
		traffic = append(traffic, "unknown")
		nat = append(nat, "unknown")
	}
	if len(addresses) == 0 || len(targets) == 0 || lan == "" {
		checks = append(checks, policyReport("node_policy", messagePolicyNodeLabel, "unknown", messagePolicyNodeMissing, []string{scope}))
		traffic = append(traffic, "unknown")
		nat = append(nat, "unknown")
	}
	for index, target := range targets[:min(3, len(targets))] {
		iface, reason := policyRouteInterface(&env.IPv6, target.Address)
		for si, source := range addresses[:min(3, len(addresses))] {
			key := fmt.Sprintf("node_%d_%d", index, si)
			if iface == "" {
				checks = append(checks, policyReport(key, messagePolicyNodeEgress, "unknown", reason, []string{scope}))
				traffic = append(traffic, "unknown")
				nat = append(nat, "unknown")
				continue
			}
			p := PolicyPacket{SrcAddress: new(source), DstAddress: new(target.Address), Protocol: new("tcp"), DstPort: new(target.Port), ConnectionState: new("new"), TCPFlags: &[]string{"syn"}, InInterface: str(lan), OutInterface: new(iface)}
			outgoing := flow(key, messagePolicyNodeCandidate, &env.IPv6, p, "forward", false, true, "")
			if outgoing.Decision == "translate" {
				checks = append(checks, policyReport(key+"_return", messagePolicyNodeReturn, "unknown", messagePolicyNodeNATReturn, outgoing.Evidence))
			} else {
				flow(key+"_return", messagePolicyNodeReturn, &env.IPv6, PolicyPacket{SrcAddress: new(target.Address), DstAddress: new(source), Protocol: new("tcp"), SrcPort: new(target.Port), ConnectionState: new("established"), InInterface: new(iface), OutInterface: str(lan)}, "forward", false, true, "")
				flow(key+"_mtu", messagePolicyNodeMTU, &env.IPv6, PolicyPacket{DstAddress: new(source), Protocol: new("icmpv6"), ICMPType: new(2), ICMPCode: new(0), ConnectionState: new("related"), InInterface: new(iface), OutInterface: str(lan)}, "forward", false, true, "")
			}
		}
	}
	clients := []PolicyClient{}
	for _, item := range context.Clients {
		if p, ok := policyNetwork(item.Address); ok && p.Bits() == p.Addr().BitLen() {
			item.Address = p.Addr().String()
			clients = append(clients, item)
		}
	}
	for _, family := range []int{6, 4} {
		fake := configuration.FakeIPv6Range
		s := &env.IPv6
		if family == 6 && !configuration.FakeIPv6Enabled {
			continue
		}
		if family == 4 {
			fake = context.FakeIPv4Range
			s = &env.IPv4
		}
		network, ok := policyNetwork(fake)
		if !ok || (network.Addr().Is4() != (family == 4)) {
			continue
		}
		selected := []PolicyClient{}
		for _, c := range clients {
			p, _ := policyNetwork(c.Address)
			if p.Addr().Is4() == (family == 4) {
				selected = append(selected, c)
			}
		}
		if len(selected) == 0 {
			checks = append(checks, policyReport(fmt.Sprintf("fake%d_policy", family), fmt.Sprintf(messagePolicyFakeLabel, family), "unknown", messagePolicyFakeClientsMissing, []string{scope, fmt.Sprintf(messagePolicyFakeKnownRange, network)}))
			traffic = append(traffic, "unknown")
			nat = append(nat, "unknown")
			continue
		}
		if len(selected) > 3 {
			checks = append(checks, policyReport(fmt.Sprintf("fake%d_policy_scope", family), fmt.Sprintf(messagePolicyClientScopeLabel, family), "unknown", messagePolicyClientScopeLimit, []string{scope}))
			traffic = append(traffic, "unknown")
			nat = append(nat, "unknown")
		}
		iface, reason := policyRouteInterface(s, network.String())
		for i, client := range selected[:min(3, len(selected))] {
			clientIface, clientReason := policyRouteInterface(s, client.Address)
			key := fmt.Sprintf("fake%d_%d", family, i)
			suffix := ""
			if client.SourceCandidate {
				suffix = messagePolicyCandidateSuffix
			}
			if iface == "" || clientIface == "" || iface != lan || !policyFakeGateway(s, network, agent, host) {
				message := reason
				if iface != "" {
					message = clientReason
					if clientIface != "" {
						message = messagePolicyFakeWrongGateway
					}
				}
				details := []string{scope}
				if client.SourceNote != "" {
					details = append(details, client.SourceNote)
				}
				checks = append(checks, policyReport(key, fmt.Sprintf(messagePolicyFakeLabel, family)+suffix, "unknown", message, details))
				traffic = append(traffic, "unknown")
				nat = append(nat, "unknown")
				continue
			}
			outgoing := flow(key, fmt.Sprintf(messagePolicyFakeOutgoing, family)+suffix, s, PolicyPacket{SrcAddress: new(client.Address), DstAddress: new(network.String()), Protocol: new("tcp"), ConnectionState: new("new"), TCPFlags: &[]string{"syn"}, InInterface: new(clientIface), OutInterface: new(iface)}, "forward", false, client.SourceCandidate, client.SourceNote)
			flow(key+"_return", fmt.Sprintf(messagePolicyFakeReturn, family)+suffix, s, PolicyPacket{SrcAddress: new(network.String()), DstAddress: new(client.Address), Protocol: new("tcp"), ConnectionState: new("established"), InInterface: new(iface), OutInterface: new(clientIface)}, "forward", true, client.SourceCandidate, client.SourceNote)
			details := append([]string{scope, messagePolicyFakeEstablished}, outgoing.Evidence[max(0, len(outgoing.Evidence)-2):]...)
			checks = append(checks, policyReport(key+"_return_routing", fmt.Sprintf(messagePolicyFakeReturnLabel, family), "unknown", messagePolicyFakeReturnUncertain, details))
		}
	}
	summary := func(key, label string, values []string) PolicyCheck {
		decision := "allow"
		switch {
		case slices.Contains(values, "drop"):
			decision = "drop"
		case slices.Contains(values, "unknown") || len(values) == 0:
			decision = "unknown"
		case slices.Contains(values, "translate"):
			decision = "translate"
		}
		messages := map[string]string{"drop": messagePolicySummaryDrop, "unknown": messagePolicySummaryUnknown, "translate": messagePolicySummaryTranslate, "allow": messagePolicySummaryAllow}
		return policyReport(key, label, decision, messages[decision], []string{scope})
	}
	return append([]PolicyCheck{summary("firewall", messagePolicyFirewallLabel, traffic), summary("nat", messagePolicyNATLabel, nat)}, checks...)
}

// MarshalJSON describes policy configuration in the same external JSON vocabulary.
func (e PolicyEnvironment) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		IPv6         PolicySnapshot `json:"ipv6"`
		IPv4         PolicySnapshot `json:"ipv4"`
		RoutingRules []Row          `json:"routing_rules"`
	}{e.IPv6, e.IPv4, e.RoutingRules})
}
