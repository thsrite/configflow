package routers

// This is the shared ipv6_policy rule evaluator. R09 uses its read-only
// forward-chain subset; R10 must extend this file rather than add a private copy.

import (
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/netipx"
)

// PolicyMatch is a three-valued matcher result; unknown never means a match.
type PolicyMatch uint8

const (
	// PolicyUnknown means the supplied packet cannot decide this matcher.
	PolicyUnknown PolicyMatch = iota
	// PolicyNoMatch means the rule cannot match this packet.
	PolicyNoMatch
	// PolicyMatches means the rule definitely matches this packet.
	PolicyMatches
)

// PolicySnapshot contains the tables needed by the shared rule evaluator.
// Nil means unreadable; an empty non-nil table proves absence.
type PolicySnapshot struct {
	Filter           []Row `json:"filter"`
	AddressLists     []Row `json:"address_lists"`
	InterfaceLists   []Row `json:"interface_lists"`
	InterfaceMembers []Row `json:"interface_members"`
	Interfaces       []Row `json:"interfaces"`
}

// PolicyPacket describes only known packet fields. Nil fields stay unknown.
type PolicyPacket struct {
	InInterface        *string           `json:"in-interface,omitempty"`
	OutInterface       *string           `json:"out-interface,omitempty"`
	SrcAddress         *string           `json:"src-address,omitempty"`
	DstAddress         *string           `json:"dst-address,omitempty"`
	Protocol           *string           `json:"protocol,omitempty"`
	ConnectionState    *string           `json:"connection-state,omitempty"`
	ConnectionNATState *string           `json:"connection-nat-state,omitempty"`
	SrcPort            *int              `json:"src-port,omitempty"`
	DstPort            *int              `json:"dst-port,omitempty"`
	ICMPType           *int              `json:"icmp-type,omitempty"`
	ICMPCode           *int              `json:"icmp-code,omitempty"`
	HopLimit           *int              `json:"hop-limit,omitempty"`
	TCPFlags           *[]string         `json:"tcp-flags,omitempty"`
	IPSecPolicy        map[string]string `json:"ipsec-policy,omitempty"`
}

func policyBool(v bool) PolicyMatch {
	if v {
		return PolicyMatches
	}
	return PolicyNoMatch
}
func policyNot(v PolicyMatch) PolicyMatch {
	if v == PolicyMatches {
		return PolicyNoMatch
	}
	if v == PolicyNoMatch {
		return PolicyMatches
	}
	return PolicyUnknown
}
func policyAnd(values ...PolicyMatch) PolicyMatch {
	out := PolicyMatches
	for _, v := range values {
		if v == PolicyNoMatch {
			return v
		}
		if v == PolicyUnknown {
			out = v
		}
	}
	return out
}
func policyOr(values ...PolicyMatch) PolicyMatch {
	out := PolicyNoMatch
	for _, v := range values {
		if v == PolicyMatches {
			return v
		}
		if v == PolicyUnknown {
			out = v
		}
	}
	return out
}
func policyFlag(value any) bool  { return rosTruthy(value) || rosText(value) == "1" }
func policyEnabled(row Row) bool { return !policyFlag(row["disabled"]) && !policyFlag(row["invalid"]) }
func policyNetwork(text string) (netip.Prefix, bool) {
	n, ok := netipx.ParseNetwork(strings.SplitN(text, "%", 2)[0], false)
	return n, ok
}
func policyAddress(actual *string, expected string) PolicyMatch {
	if actual == nil {
		return PolicyUnknown
	}
	source, ok := policyNetwork(*actual)
	if !ok {
		return PolicyUnknown
	}
	negate := strings.HasPrefix(expected, "!")
	expected = strings.TrimLeft(expected, "!")
	out := PolicyUnknown
	if target, ok := policyNetwork(expected); ok {
		out = PolicyNoMatch
		if source.Addr().BitLen() == target.Addr().BitLen() && source.Overlaps(target) {
			if source.Bits() >= target.Bits() {
				out = PolicyMatches
			} else {
				out = PolicyUnknown
			}
		}
	} else if lowText, highText, rangeOK := strings.Cut(expected, "-"); rangeOK {
		low, e1 := netip.ParseAddr(lowText)
		high, e2 := netip.ParseAddr(highText)
		if e1 == nil && e2 == nil {
			last := prefixLast(source)
			switch {
			case low.BitLen() != source.Addr().BitLen() || high.BitLen() != source.Addr().BitLen() || high.Less(low):
				out = PolicyNoMatch
			case source.Addr().Compare(low) >= 0 && last.Compare(high) <= 0:
				out = PolicyMatches
			case last.Less(low) || source.Addr().Compare(high) > 0:
				out = PolicyNoMatch
			}
		}
	}
	if negate {
		return policyNot(out)
	}
	return out
}
func prefixLast(n netip.Prefix) netip.Addr {
	if n.Addr().Is4() {
		a := n.Masked().Addr().As4()
		for bit := n.Bits(); bit < 32; bit++ {
			a[bit/8] |= 1 << uint(7-bit%8)
		}
		return netip.AddrFrom4(a)
	}
	a := n.Masked().Addr().As16()
	for bit := n.Bits(); bit < 128; bit++ {
		a[bit/8] |= 1 << uint(7-bit%8)
	}
	return netip.AddrFrom16(a)
}
func policyNumber(actual *int, expected string) PolicyMatch {
	if actual == nil {
		return PolicyUnknown
	}
	negate := strings.HasPrefix(expected, "!")
	expected = strings.TrimLeft(expected, "!")
	found := false
	for _, part := range strings.Split(expected, ",") {
		lowText, highText, has := strings.Cut(part, "-")
		if !has {
			highText = lowText
		}
		low, e1 := strconv.Atoi(strings.TrimSpace(lowText))
		high, e2 := strconv.Atoi(strings.TrimSpace(highText))
		if e1 != nil || e2 != nil {
			return PolicyUnknown
		}
		found = found || low <= *actual && *actual <= high
	}
	if negate {
		found = !found
	}
	return policyBool(found)
}
func policyProtocol(s string) string {
	switch strings.ToLower(s) {
	case "tcp":
		return "6"
	case "udp":
		return "17"
	case "icmp":
		return "1"
	case "icmpv6", "ipv6-icmp", "icmp6":
		return "58"
	default:
		return strings.ToLower(s)
	}
}
func policyString(actual *string, expected string, protocol bool) PolicyMatch {
	if actual == nil {
		return PolicyUnknown
	}
	negate := strings.HasPrefix(expected, "!")
	expected = strings.TrimLeft(expected, "!")
	found := false
	for _, part := range strings.Split(expected, ",") {
		part = strings.TrimSpace(part)
		if protocol {
			found = found || policyProtocol(*actual) == policyProtocol(part)
		} else {
			found = found || *actual == part
		}
	}
	if negate {
		found = !found
	}
	return policyBool(found)
}
func policyInterface(s *PolicySnapshot, iface *string, name string, visited map[string]bool) PolicyMatch {
	budget := 256
	return policyInterfaceBounded(s, iface, name, visited, &budget)
}
func policyInterfaceBounded(s *PolicySnapshot, iface *string, name string, visited map[string]bool, budget *int) PolicyMatch {
	*budget--
	if *budget < 0 || len(visited) > 12 {
		return PolicyUnknown
	}
	if iface == nil {
		return PolicyUnknown
	}
	if *iface == "" {
		return PolicyNoMatch
	}
	if name == "all" {
		return PolicyMatches
	}
	if name == "none" {
		return PolicyNoMatch
	}
	if name == "dynamic" || name == "static" {
		if s.Interfaces == nil {
			return PolicyUnknown
		}
		for _, row := range s.Interfaces {
			if row.is("name", *iface) {
				return policyBool(policyFlag(row["dynamic"]) == (name == "dynamic"))
			}
		}
		return PolicyUnknown
	}
	if visited[name] || s.InterfaceLists == nil || s.InterfaceMembers == nil {
		return PolicyUnknown
	}
	var definition Row
	count := 0
	for _, row := range s.InterfaceLists {
		if row.is("name", name) {
			definition = row
			count++
		}
	}
	if count != 1 {
		return PolicyUnknown
	}
	for _, row := range s.InterfaceMembers {
		if policyEnabled(row) && row.is("list", name) && row.is("interface", *iface) {
			return PolicyMatches
		}
	}
	visited[name] = true
	defer delete(visited, name)
	listMatch := func(key string) PolicyMatch {
		out := PolicyNoMatch
		for _, nested := range strings.Split(definition.text(key), ",") {
			if nested != "" {
				out = policyOr(out, policyInterfaceBounded(s, iface, nested, visited, budget))
			}
		}
		return out
	}
	return policyAnd(listMatch("include"), policyNot(listMatch("exclude")))
}
func policyList(s *PolicySnapshot, actual *string, name string) PolicyMatch {
	if s.AddressLists == nil || actual == nil {
		return PolicyUnknown
	}
	negate := strings.HasPrefix(name, "!")
	name = strings.TrimLeft(name, "!")
	out := PolicyNoMatch
	for _, row := range s.AddressLists {
		if policyEnabled(row) && row.is("list", name) {
			out = policyOr(out, policyAddress(actual, row.text("address")))
		}
	}
	if negate {
		return policyNot(out)
	}
	return out
}

var policyMeta = stringSet(".id", "chain", "action", "disabled", "invalid", "dynamic", "comment", "log", "log-prefix", "bytes", "packets", "jump-target", "reject-with", "to-addresses", "to-ports", "random", "fully-random", "persistent", "hw-offload")

// MatchPolicyRule evaluates supported RouterOS selectors without inventing any
// missing packet data. A false selector dominates an unknown one.
func MatchPolicyRule(s *PolicySnapshot, row Row, p PolicyPacket) PolicyMatch {
	if s == nil {
		return PolicyUnknown
	}
	out := PolicyMatches
	for key, value := range row {
		if policyMeta[key] || value == nil {
			continue
		}
		if text, ok := value.(string); ok && text == "" {
			continue
		}
		v := rosText(value)
		match := PolicyUnknown
		if v == "" {
			out = policyAnd(out, PolicyUnknown)
			continue
		}
		switch key {
		case "src-address":
			match = policyAddress(p.SrcAddress, v)
		case "dst-address":
			match = policyAddress(p.DstAddress, v)
		case "src-address-list":
			match = policyList(s, p.SrcAddress, v)
		case "dst-address-list":
			match = policyList(s, p.DstAddress, v)
		case "in-interface-list", "out-interface-list":
			iface := p.InInterface
			if key == "out-interface-list" {
				iface = p.OutInterface
			}
			match = policyInterface(s, iface, strings.TrimLeft(v, "!"), map[string]bool{})
			if strings.HasPrefix(v, "!") {
				match = policyNot(match)
			}
		case "in-interface":
			match = policyString(p.InInterface, v, false)
		case "out-interface":
			match = policyString(p.OutInterface, v, false)
		case "connection-state":
			match = policyString(p.ConnectionState, v, false)
		case "connection-nat-state":
			match = policyString(p.ConnectionNATState, v, false)
		case "protocol":
			match = policyString(p.Protocol, v, true)
		case "src-port":
			match = policyNumber(p.SrcPort, v)
		case "dst-port":
			match = policyNumber(p.DstPort, v)
		case "port":
			match = policyOr(policyNumber(p.SrcPort, v), policyNumber(p.DstPort, v))
		case "icmp-options":
			a, b, has := strings.Cut(v, ":")
			match = policyNumber(p.ICMPType, a)
			if has {
				match = policyAnd(match, policyNumber(p.ICMPCode, b))
			}
		case "tcp-flags":
			if p.TCPFlags != nil {
				match = PolicyMatches
				for _, flag := range strings.Split(v, ",") {
					found := slices.Contains(*p.TCPFlags, strings.TrimPrefix(flag, "!"))
					if strings.HasPrefix(flag, "!") {
						found = !found
					}
					match = policyAnd(match, policyBool(found))
				}
			}
		case "ipsec-policy":
			direction, policy, has := strings.Cut(v, ",")
			if actual, ok := p.IPSecPolicy[direction]; has && ok && (policy == "ipsec" || policy == "none") {
				match = policyBool(actual == policy)
			}
		case "hop-limit":
			if p.HopLimit != nil {
				op, text, has := strings.Cut(v, ":")
				n, err := strconv.Atoi(text)
				if has && err == nil {
					switch op {
					case "equal":
						match = policyBool(*p.HopLimit == n)
					case "less-than":
						match = policyBool(*p.HopLimit < n)
					case "greater-than":
						match = policyBool(*p.HopLimit > n)
					}
				}
			}
		}
		out = policyAnd(out, match)
		if out == PolicyNoMatch {
			return out
		}
	}
	return out
}

// EvaluatePolicyChain evaluates ordered filter rules with unknown branches and
// bounded jumps. It returns allow/drop/unknown; it proves no actual connectivity.
func EvaluatePolicyChain(s *PolicySnapshot, chain string, p PolicyPacket) string {
	if s == nil || s.Filter == nil {
		return "unknown"
	}
	chains := map[string][]int{}
	for i, row := range s.Filter {
		if policyEnabled(row) {
			chains[row.text("chain")] = append(chains[row.text("chain")], i)
		}
	}
	type frame struct {
		chain  string
		offset int
	}
	budget := 0
	var walk func(string, int, []frame) string
	walk = func(current string, offset int, stack []frame) string {
		budget++
		if budget > 256 || len(stack) > 12 {
			return "unknown"
		}
		index := -1
		for _, i := range chains[current] {
			if i >= offset {
				index = i
				break
			}
		}
		if index < 0 {
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				return walk(parent.chain, parent.offset, stack[:len(stack)-1])
			}
			return "allow"
		}
		row := s.Filter[index]
		match := MatchPolicyRule(s, row, p)
		action := firstNonEmpty(row.text("action"), "accept")
		if match == PolicyNoMatch || action == "log" || action == "passthrough" {
			return walk(current, index+1, stack)
		}
		apply := func() string {
			switch action {
			case "accept":
				return "allow"
			case "drop", "reject":
				return "drop"
			case "return":
				if len(stack) > 0 {
					parent := stack[len(stack)-1]
					return walk(parent.chain, parent.offset, stack[:len(stack)-1])
				}
				return "allow"
			case "jump":
				target := row.text("jump-target")
				if target == "" || target == current || len(chains[target]) == 0 {
					return "unknown"
				}
				for _, f := range stack {
					if f.chain == target {
						return "unknown"
					}
				}
				return walk(target, 0, append(slices.Clone(stack), frame{current, index + 1}))
			default:
				return "unknown"
			}
		}
		matched := apply()
		if match == PolicyMatches {
			return matched
		}
		skipped := walk(current, index+1, stack)
		if matched == skipped && (matched == "allow" || matched == "drop") {
			return matched
		}
		return "unknown"
	}
	return walk(chain, 0, nil)
}
