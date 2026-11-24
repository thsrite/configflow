package routers

import (
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"encoding/json"
	"strings"
	"testing"

	"configflow/internal/routers/device"
	"configflow/internal/routers/spec"
)

// TestRouterStateJSONFieldOrder verifies that embedding device.State inside
// RouterState preserves the exact legacy JSON member order when dns_guard,
// ros_policy, device facts, and nat_context are all populated.
func TestRouterStateJSONFieldOrder(t *testing.T) {
	guard := dnsguard.Snapshot{"/ip/firewall/nat": {ros.Row{".id": "*1"}}}
	policy := rospolicy.ROSPolicySnapshot{"/routing/table": {ros.Row{".id": "*2"}}}
	state := &RouterState{
		DNSGuard:  &guard,
		ROSPolicy: &policy,
		State: device.State{
			Routes:              []*device.RouteEntry{},
			NatBypass:           []*device.NatBypassEntry{},
			AddressSets:         map[string][]string{},
			PolicyRoutes:        []*device.PolicyRouteEntry{},
			RetiredPolicyRoutes: []*device.PolicyRouteEntry{},
			ActiveRoutes:        []*device.RouteEntry{},
		},
		NatContext: &ros.NatContext{Routes: []ros.Row{}, Rules: []ros.Row{}},
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	order := []string{
		`"dns_guard":`,
		`"ros_policy":`,
		`"routes":`,
		`"nat_bypass":`,
		`"address_sets":`,
		`"dhcp_dns":`,
		`"policy_routes":`,
		`"retired_policy_routes":`,
		`"active_routes":`,
		`"nat_context":`,
	}
	prev := -1
	for _, key := range order {
		idx := strings.Index(text, key)
		if idx < 0 {
			t.Fatalf("missing key %s in %s", key, text)
		}
		if idx <= prev {
			t.Fatalf("key %s out of order (pos %d <= %d) in %s", key, idx, prev, text)
		}
		prev = idx
	}
}

// TestBuildReportSectionOrder verifies that removing ReportParts/init keeps
// the exact fixed section order: dns_guard, policy_routing, ipv6_*.
func TestBuildReportSectionOrder(t *testing.T) {
	desired := &spec.Desired{
		ProxyGateway: "192.0.2.2",
		ProxyCIDRs:   []spec.ProxyCIDR{},
		IPv6CIDRs:    []spec.ProxyCIDR{},
		DNSGuard:     &spec.DesiredDNSGuard{Settings: spec.DefaultDNSGuard()},
		ROSPolicy:    &spec.ROSPolicyDesired{Selected: []string{}, Sources: []string{}, CIDRs: []spec.ProxyCIDR{}},
	}
	guard := dnsguard.Snapshot{}
	policy := rospolicy.ROSPolicySnapshot{}
	state := &RouterState{
		DNSGuard:  &guard,
		ROSPolicy: &policy,
		State: device.State{
			Routes:      []*device.RouteEntry{},
			NatBypass:   []*device.NatBypassEntry{},
			AddressSets: map[string][]string{},
		},
		IPv6FakeIPPlan:  &ipv6.IPv6FakeIPPlan{IPv6FakeIPReport: ipv6.IPv6FakeIPReport{Status: "disabled"}},
		IPv6RoutesPlan:  &ipv6.IPv6RoutesPlan{IPv6RoutesReport: ipv6.IPv6RoutesReport{Status: "disabled"}},
		IPv6DNSPlan:     &ipv6.IPv6DNSPlan{IPv6DNSReport: ipv6.IPv6DNSReport{Status: "disabled"}},
		IPv6PDGuardPlan: &ipv6.IPv6PDGuardPlan{IPv6PDGuardReport: ipv6.IPv6PDGuardReport{Status: "disabled"}},
	}
	report, err := BuildReport(desired, state)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dns_guard", "policy_routing", "ipv6_fakeip", "ipv6_routes", "ipv6_dns", "ipv6_pd_guard"}
	if len(report.Parts) != len(want) {
		t.Fatalf("got %d parts, want %d", len(report.Parts), len(want))
	}
	for i, k := range want {
		if report.Parts[i].Key != k {
			t.Fatalf("part[%d].Key = %q, want %q", i, report.Parts[i].Key, k)
		}
	}
}
