package ipv6

// The shared evaluator is read-only and deliberately conservative: unknown
// selectors never prove a packet path or a real connection.

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/ros"
	"fmt"
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
	Family           int       `json:"-"`
	Raw              []ros.Row `json:"raw"`
	Mangle           []ros.Row `json:"mangle"`
	NAT              []ros.Row `json:"nat"`
	Routes           []ros.Row `json:"routes"`
	Filter           []ros.Row `json:"filter"`
	AddressLists     []ros.Row `json:"address_lists"`
	InterfaceLists   []ros.Row `json:"interface_lists"`
	InterfaceMembers []ros.Row `json:"interface_members"`
	Interfaces       []ros.Row `json:"interfaces"`
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

func PolicyBool(v bool) PolicyMatch {
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
func policyEnabled(row ros.Row) bool {
	return !ros.PolicyFlag(row["disabled"]) && !ros.PolicyFlag(row["invalid"])
}
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
	return PolicyBool(found)
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
	return PolicyBool(found)
}
func PolicyInterface(s *PolicySnapshot, iface *string, name string, visited map[string]bool) PolicyMatch {
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
			if row.Is("name", *iface) {
				return PolicyBool(ros.PolicyFlag(row["dynamic"]) == (name == "dynamic"))
			}
		}
		return PolicyUnknown
	}
	if visited[name] || s.InterfaceLists == nil || s.InterfaceMembers == nil {
		return PolicyUnknown
	}
	var definition ros.Row
	count := 0
	for _, row := range s.InterfaceLists {
		if row.Is("name", name) {
			definition = row
			count++
		}
	}
	if count != 1 {
		return PolicyUnknown
	}
	for _, row := range s.InterfaceMembers {
		if policyEnabled(row) && row.Is("list", name) && row.Is("interface", *iface) {
			return PolicyMatches
		}
	}
	visited[name] = true
	defer delete(visited, name)
	listMatch := func(key string) PolicyMatch {
		// A null (nullMarker) or non-text list is unknown, never empty.
		if ros.StructuredText(definition[key]) {
			return PolicyUnknown
		}
		out := PolicyNoMatch
		for _, nested := range strings.Split(definition.Text(key), ",") {
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
		if policyEnabled(row) && row.Is("list", name) {
			out = policyOr(out, policyAddress(actual, row.Text("address")))
		}
	}
	if negate {
		return policyNot(out)
	}
	return out
}

var policyMeta = device.StringSet(".id", "chain", "action", "disabled", "invalid", "dynamic", "comment", "log", "log-prefix", "bytes", "packets", "jump-target", "reject-with", "to-addresses", "to-ports", "random", "fully-random", "persistent", "hw-offload")

// MatchPolicyRule evaluates supported RouterOS selectors without inventing any
// missing packet data. A false selector dominates an unknown one.
func MatchPolicyRule(s *PolicySnapshot, row ros.Row, p PolicyPacket) PolicyMatch {
	if s == nil {
		return PolicyUnknown
	}
	out := PolicyMatches
	for key, value := range row {
		if policyMeta[key] || value == "" {
			continue
		}
		if ros.StructuredText(value) {
			// A non-text selector (JSON array or object) is unknown.
			out = policyAnd(out, PolicyUnknown)
			continue
		}
		v := value
		match := PolicyUnknown
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
			match = PolicyInterface(s, iface, strings.TrimLeft(v, "!"), map[string]bool{})
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
					match = policyAnd(match, PolicyBool(found))
				}
			}
		case "ipsec-policy":
			direction, policy, has := strings.Cut(v, ",")
			if actual, ok := p.IPSecPolicy[direction]; has && ok && (policy == "ipsec" || policy == "none") {
				match = PolicyBool(actual == policy)
			}
		case "hop-limit":
			if p.HopLimit != nil {
				op, text, has := strings.Cut(v, ":")
				n, err := strconv.Atoi(text)
				if has && err == nil {
					switch op {
					case "equal":
						match = PolicyBool(*p.HopLimit == n)
					case "less-than":
						match = PolicyBool(*p.HopLimit < n)
					case "greater-than":
						match = PolicyBool(*p.HopLimit > n)
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

// PolicyDecision contains a static conclusion and its ordered rule evidence.
// Translation is set only for a proven NAT translation action.
type PolicyDecision struct {
	Decision    string   `json:"decision"`
	Evidence    []string `json:"evidence"`
	Translation string   `json:"translation,omitempty"`
}

func (s *PolicySnapshot) table(name string) []ros.Row {
	if s == nil {
		return nil
	}
	switch name {
	case "raw":
		return s.Raw
	case "mangle":
		return s.Mangle
	case "nat":
		return s.NAT
	case "filter":
		return s.Filter
	}
	return nil
}

func policyEvidence(row ros.Row, index int, table, chain string) string {
	identity := row.ID()
	if identity == "" {
		identity = fmt.Sprintf(messagePolicyPosition, index+1)
	}
	comment := []rune(strings.ReplaceAll(row.Text("comment"), "\n", " "))
	suffix := ""
	if len(comment) > 0 {
		suffix = fmt.Sprintf(messagePolicyComment, string(comment[:min(180, len(comment))]))
	}
	action := "accept"
	if row.Has("action") {
		action = row.Text("action")
	}
	return fmt.Sprintf(messagePolicyEvidence, table, chain, index+1, identity, suffix, action)
}

// EvaluatePolicyChain evaluates one ordered table with bounded unknown branches
// and jumps. Nil tables mean unreadable; empty non-nil tables prove absence.
func EvaluatePolicyChain(s *PolicySnapshot, table, chain string, p PolicyPacket) PolicyDecision {
	rows := s.table(table)
	if rows == nil {
		key := table
		if s != nil && s.Family == 4 {
			key = "ipv4_" + key
		}
		return PolicyDecision{Decision: "unknown", Evidence: []string{fmt.Sprintf(messagePolicyUnread, key)}}
	}
	chains := map[string][]int{}
	for i, row := range rows {
		if policyEnabled(row) {
			chains[row.Text("chain")] = append(chains[row.Text("chain")], i)
		}
	}
	type frame struct {
		chain  string
		offset int
	}
	budget := 0
	var walk func(string, int, []frame, []string) PolicyDecision
	walk = func(current string, offset int, stack []frame, trail []string) PolicyDecision {
		result := func(decision string, extra ...string) PolicyDecision {
			return PolicyDecision{Decision: decision, Evidence: append(slices.Clone(trail), extra...)}
		}
		budget++
		if budget > 256 || len(stack) > 12 {
			return result("unknown", messagePolicyBudget)
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
				return walk(parent.chain, parent.offset, stack[:len(stack)-1], trail)
			}
			message := fmt.Sprintf(messagePolicyDefault, table, current)
			if table == "nat" {
				message = fmt.Sprintf(messagePolicyNATDefault, current)
			}
			return result("allow", message)
		}
		row := rows[index]
		match := MatchPolicyRule(s, row, p)
		action := "accept"
		if row.Has("action") {
			action, _ = row.Str("action")
		}
		if match == PolicyNoMatch || action == "log" || action == "passthrough" {
			return walk(current, index+1, stack, trail)
		}
		evidence := policyEvidence(row, index, table, current)
		nextTrail := append(slices.Clone(trail), evidence)
		apply := func() PolicyDecision {
			switch action {
			case "accept":
				return result("allow", evidence)
			case "drop", "reject":
				return result("drop", evidence)
			case "return":
				if len(stack) > 0 {
					parent := stack[len(stack)-1]
					return walk(parent.chain, parent.offset, stack[:len(stack)-1], nextTrail)
				}
				return result("allow", evidence, messagePolicyReturn)
			case "jump":
				target := row.Text("jump-target")
				cycle := target == "" || target == current || len(chains[target]) == 0
				for _, f := range stack {
					cycle = cycle || f.chain == target
				}
				if cycle {
					return result("unknown", evidence, messagePolicyJump)
				}
				return walk(target, 0, append(slices.Clone(stack), frame{current, index + 1}), nextTrail)
			}
			if table == "nat" && slices.Contains([]string{"masquerade", "src-nat", "dst-nat", "netmap", "redirect"}, action) {
				out := result("translate", evidence)
				out.Translation = action
				return out
			}
			return result("unknown", evidence, messagePolicyAction)
		}
		matched := apply()
		if match == PolicyMatches {
			return matched
		}
		skipped := walk(current, index+1, stack, trail)
		if matched.Decision == skipped.Decision && (matched.Decision == "allow" || matched.Decision == "drop") {
			return PolicyDecision{Decision: matched.Decision, Evidence: device.Unique(append(append(matched.Evidence, skipped.Evidence...), messagePolicyConverge))}
		}
		return result("unknown", evidence, messagePolicyDiverge)
	}
	return walk(chain, 0, nil, nil)
}
