// Package routers is the upstream-router integration: router records,
// desired state, drift report, release. JSON names are the stored/API ones;
// for optional lists nil is "not reported" and empty is "reported empty".
package routers

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"
)

// RouterState is one readback of the device (later modules add their fields).
type RouterState struct {
	IPv6FakeIPPlan  *IPv6FakeIPPlan     `json:"-"`
	IPv6DNSPlan     *IPv6DNSPlan        `json:"-"`
	IPv6PDGuardPlan *IPv6PDGuardPlan    `json:"-"`
	IPv6RoutesPlan  *IPv6RoutesPlan     `json:"-"`
	DNSGuard        *DNSGuardSnapshot   `json:"dns_guard,omitempty"`
	DNSGuardPlan    *DNSGuardPlan       `json:"-"`
	ROSPolicy       *ROSPolicySnapshot  `json:"ros_policy,omitempty"`
	ROSPolicyPlan   *ROSPolicyPlan      `json:"-"`
	Routes          []*RouteEntry       `json:"routes"`
	NatBypass       []*NatBypassEntry   `json:"nat_bypass"`
	AddressSets     map[string][]string `json:"address_sets"`
	DhcpDNS         *DhcpDnsState       `json:"dhcp_dns"`
	// nil: proxy CIDRs go by static routes; a list: by policy rules.
	PolicyRoutes []*PolicyRouteEntry `json:"policy_routes"`
	// Our policy rules being withdrawn (RetirePolicyRoutes).
	RetiredPolicyRoutes []*PolicyRouteEntry `json:"retired_policy_routes"`
	// Routes the router actually forwards with (iKuai 当前路由表); nil when unknown.
	ActiveRoutes []*RouteEntry `json:"active_routes"`
	// RouterOS NAT/forwarding evidence; nil keeps the legacy coverage check
	// (empty Rules means there are no source-NAT rules).
	NatContext *NatContext `json:"nat_context"`
	// NAT rule ids the RouterOS policy-routing plan (R08) removes.
	PolicyNATRetiring []string `json:"-"`
	// iKuai only: IP groups with ConfigFlow ownership and the rules using them.
	IkuaiGroups *GroupState `json:"-"`
	// NATLists is eager when the lists are referenced; the reader is lazy otherwise.
	NATLists       *NATLists                                `json:"-"`
	NATListsReader func(context.Context) (*NATLists, error) `json:"-"`
}

// Driver talks to one router.
type Driver interface {
	Vendor() string
	SupportsWrite() bool // false: stages are exported as a script
	Probe(ctx context.Context) (*RouterInfo, error)
	ReadState(ctx context.Context) (*RouterState, error)
	ReadDHCPDNS(ctx context.Context) (*DhcpDnsState, error) // only when a DNS Agent is selected
	// Apply applies one release operation and returns the one that undoes it.
	Apply(ctx context.Context, op json.RawMessage) (json.RawMessage, error)
	RenderScript(ops []json.RawMessage) (string, error)
	Close()
}

// PolicyRoute is the first policy route carrying comment.
func PolicyRoute(state *RouterState, comment string) *PolicyRouteEntry {
	for _, e := range state.PolicyRoutes {
		if e.Comment == comment {
			return e
		}
	}
	return nil
}

// RetirePolicyRoutes routes proxy CIDRs by static routes and withdraws our
// policy rules (they need managed return routing).
func RetirePolicyRoutes(state *RouterState) {
	if state.PolicyRoutes == nil {
		return
	}
	state.RetiredPolicyRoutes = slices.DeleteFunc(slices.Clone(state.PolicyRoutes), func(e *PolicyRouteEntry) bool {
		return e.Comment != PolicyAutoComment && e.Comment != PolicyManualComment
	})
	state.PolicyRoutes = nil
}

// StaleRoutes are active routes to the gateway that no configured static
// route explains (iKuai keeps kernel routes after edits).
func StaleRoutes(state *RouterState, gateway string) []*RouteEntry {
	if gateway == "" || state.ActiveRoutes == nil {
		return []*RouteEntry{}
	}
	return slices.DeleteFunc(slices.Clone(state.ActiveRoutes), func(a *RouteEntry) bool {
		return a.Gateway != gateway || slices.ContainsFunc(state.Routes, func(r *RouteEntry) bool {
			return r.Enabled && r.Gateway == gateway && r.Dst == a.Dst
		})
	})
}

// StaleRouteWarning explains StaleRoutes to the user.
func StaleRouteWarning(routes []*RouteEntry, gateway string) string {
	dsts := make([]string, len(routes))
	for i, r := range routes {
		dsts[i] = r.Dst
	}
	return "路由器当前路由表中有 " + strconv.Itoa(len(routes)) + " 条指向 " + gateway + " 的路由不在静态路由配置里" +
		"（" + strings.Join(dsts, ", ") + "）：这是修改或删除静态路由后残留的内核路由，" +
		"优先于分流规则，会让 " + gateway + " 自身的流量回环。界面和接口都无法删除，需要重启路由器清除。"
}
