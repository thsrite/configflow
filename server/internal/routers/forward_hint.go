package routers

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

// ForwardHintResult is advisory only. Never include it in a release operation,
// check, evidence snapshot, or fingerprint.
type ForwardHintResult struct {
	Status     string `json:"status"`
	FixCommand string `json:"fix_command"`
	Warning    string `json:"warning"`
}

// ForwardHintCheck reports a definite filter drop, or an unconfirmed read.
// An unknown matcher is status ok: it must not become a daily drop warning.
type ForwardHintCheck struct {
	Status  string `json:"status"`
	Command string `json:"command"`
	Rule    Row    `json:"rule"`
}

// ForwardDropWarning explains same-port drops for the supplied IPv4/IPv6 family.
// Both families share wording; IPv6 also names its two protected route classes.
func ForwardDropWarning(family int) string {
	if family == 6 {
		return messageForwardIPv6Drop
	}
	return messageForwardIPv4Drop
}

var forwardBareValue = regexp.MustCompile(`^[A-Za-z0-9._\-]+$`)

func forwardQuote(value string) string {
	if forwardBareValue.MatchString(value) {
		return value
	}
	return ScriptString(value)
}

func samePortPacket(lan, target string) PolicyPacket {
	return PolicyPacket{InInterface: new(lan), OutInterface: new(lan), DstAddress: new(target), Protocol: new("tcp"), ConnectionState: new("invalid"), IPSecPolicy: map[string]string{"in": "none", "out": "none"}}
}
func forwardSnapshot(ctx context.Context, d *RouterOS, family int, rows []Row) (*PolicySnapshot, error) {
	s := &PolicySnapshot{Filter: rows, AddressLists: []Row{}}
	for _, item := range []struct {
		path string
		to   *[]Row
	}{{"/interface/list", &s.InterfaceLists}, {"/interface/list/member", &s.InterfaceMembers}, {"/interface", &s.Interfaces}} {
		value, err := d.list(ctx, item.path)
		if err != nil {
			return nil, err
		}
		*item.to = value
	}
	names := []string{}
	for _, row := range rows {
		if row.is("chain", "input") || row.is("chain", "output") {
			continue
		}
		for _, field := range []string{"src-address-list", "dst-address-list"} {
			if row.set(field) {
				names = append(names, strings.TrimLeft(row.text(field), "!"))
			}
		}
	}
	names = uniqueStrings(names)
	slices.Sort(names)
	if family == 4 {
		lists, err := d.ReadAddresses(ctx, names)
		if err != nil {
			return nil, err
		}
		for _, name := range names {
			s.AddressLists = append(s.AddressLists, lists[name]...)
		}
	} else {
		for _, name := range names {
			rows, err := d.list(ctx, "/ipv6/firewall/address-list?list="+strings.ReplaceAll(url.QueryEscape(name), "+", "%20"))
			if err != nil {
				return nil, err
			}
			s.AddressLists = append(s.AddressLists, rows...)
		}
	}
	return s, nil
}

// CheckForwardHint evaluates same-port forwarding using shared three-valued
// filter semantics. It only reads; family is 4 or 6, rows nil means fetch it.
func CheckForwardHint(ctx context.Context, d *RouterOS, family int, lan string, targets []string, rows []Row) ForwardHintCheck {
	unknown := ForwardHintCheck{Status: "unknown"}
	targets = slices.DeleteFunc(slices.Clone(targets), func(s string) bool { return s == "" })
	if d == nil || (family != 4 && family != 6) || lan == "" || len(targets) == 0 {
		return unknown
	}
	path := "/ip/firewall/filter"
	command := "/ip firewall filter"
	if family == 6 {
		path = "/ipv6/firewall/filter"
		command = "/ipv6 firewall filter"
	}
	if rows == nil {
		var err error
		rows, err = d.list(ctx, path)
		if err != nil {
			return unknown
		}
	}
	snapshot, err := forwardSnapshot(ctx, d, family, rows)
	if err != nil {
		return unknown
	}
	destinationSpecific := slices.ContainsFunc(rows, func(row Row) bool {
		return policyEnabled(row) && (row.set("dst-address") || row.set("dst-address-list"))
	})
	if !destinationSpecific {
		targets = targets[:1]
	}
	for _, target := range targets {
		packet := samePortPacket(lan, target)
		if EvaluatePolicyChain(snapshot, "filter", "forward", packet).Decision != "drop" {
			continue
		}
		var taker Row
		for _, row := range rows {
			if !row.is("chain", "forward") || !policyEnabled(row) || row.is("action", "log") || row.is("action", "passthrough") || MatchPolicyRule(snapshot, row, packet) == PolicyNoMatch || rosTruthy(row["dynamic"]) || row.id() == "" {
				continue
			}
			taker = row
			break
		}
		fix := fmt.Sprintf(`%s add chain=forward action=accept in-interface=%s out-interface=%s comment="LAN to Mihomo via router"`, command, forwardQuote(lan), forwardQuote(lan))
		if taker != nil {
			fix += " place-before=" + taker.id()
		}
		return ForwardHintCheck{Status: "drop", Command: fix, Rule: taker}
	}
	return ForwardHintCheck{Status: "ok"}
}

// ForwardHint provides the optional IPv4 report hint for this router. It never
// changes the router, plan, checks, or fingerprint; unreadable evidence is advice.
func ForwardHint(ctx context.Context, r *Router, desired *Desired, state *RouterState, report *Report, d *RouterOS) (*ForwardHintResult, error) {
	if r == nil || r.Type != "routeros" || r.ManageReturnRouting || d == nil || state == nil || desired == nil || report == nil {
		return nil, nil
	}
	targets := []string{}
	for _, item := range report.Routes.Items {
		if slices.Contains([]string{"synced", "adoptable", "missing"}, item.Status) {
			targets = append(targets, item.CIDR)
		}
	}
	if desired.ROSPolicy != nil && len(desired.ROSPolicy.Selected) > 0 && state.ROSPolicyPlan != nil && len(state.ROSPolicyPlan.Blockers) == 0 {
		for _, item := range state.ROSPolicyPlan.Items {
			targets = append(targets, item.CIDR)
		}
	}
	targets = uniqueStrings(targets)
	slices.SortStableFunc(targets, func(a, b string) int {
		if a == b {
			return 0
		}
		if a == desired.FakeIPRange {
			return -1
		}
		if b == desired.FakeIPRange {
			return 1
		}
		return 0
	})
	if len(targets) == 0 {
		return nil, nil
	}
	gateway, ok := netipx.ParseAddr4(desired.ProxyGateway)
	if !ok {
		return nil, nil
	}
	lan, _ := NewRouteEgressIndex(state.NatContext).GatewayEgress(gateway)
	if lan == "" {
		return &ForwardHintResult{Status: "unknown", Warning: messageForwardUnknownLAN}, nil
	}
	var rows []Row
	if state.ROSPolicy != nil {
		rows = (*state.ROSPolicy)[rosFilter]
	} else if state.DNSGuard != nil {
		rows = (*state.DNSGuard)[rosFilter]
	}
	result := CheckForwardHint(ctx, d, 4, lan, targets, rows)
	switch result.Status {
	case "drop":
		return &ForwardHintResult{Status: "drop", FixCommand: result.Command, Warning: messageForwardIPv4Drop}, nil
	case "unknown":
		return &ForwardHintResult{Status: "unknown", Warning: messageForwardUnknownRead}, nil
	default:
		return nil, nil
	}
}
