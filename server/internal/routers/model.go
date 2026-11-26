// Package routers is the upstream-router integration: router records,
// desired state, drift report, release. JSON names are the stored/API ones;
// for optional lists nil is "not reported" and empty is "reported empty".
package routers

import (
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ipv6"
	"configflow/internal/routers/ros"
	"configflow/internal/routers/rospolicy"
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"configflow/internal/routers/device"
)

// RouterState is one readback of the device plus planning-stage outputs.
// Field order matches the legacy JSON contract: dns_guard, ros_policy,
// device.State (routes..active_routes), nat_context.
type RouterState struct {
	DNSGuard  *dnsguard.Snapshot           `json:"dns_guard,omitempty"`
	ROSPolicy *rospolicy.ROSPolicySnapshot `json:"ros_policy,omitempty"`
	device.State
	// RouterOS NAT/forwarding evidence; nil keeps the legacy coverage check
	// (empty Rules means there are no source-NAT rules).
	NatContext *ros.NatContext `json:"nat_context"`
	// iKuai only: IP groups with ConfigFlow ownership and the rules using them.
	IkuaiGroups *ikuai.GroupState `json:"-"`
	// NATLists is eager when the lists are referenced; the reader is lazy otherwise.
	NATLists       *rospolicy.NATLists                                `json:"-"`
	NATListsReader func(context.Context) (*rospolicy.NATLists, error) `json:"-"`
	// Planning-stage outputs owned by the orchestration package.
	DNSGuardPlan    *dnsguard.Plan           `json:"-"`
	ROSPolicyPlan   *rospolicy.ROSPolicyPlan `json:"-"`
	IPv6FakeIPPlan  *ipv6.IPv6FakeIPPlan     `json:"-"`
	IPv6DNSPlan     *ipv6.IPv6DNSPlan        `json:"-"`
	IPv6PDGuardPlan *ipv6.IPv6PDGuardPlan    `json:"-"`
	IPv6RoutesPlan  *ipv6.IPv6RoutesPlan     `json:"-"`
}

// Driver talks to one router.
type Driver interface {
	Vendor() string
	SupportsWrite() bool // false: stages are exported as a script
	Probe(ctx context.Context) (*device.RouterInfo, error)
	ReadState(ctx context.Context) (*RouterState, error)
	ReadDHCPDNS(ctx context.Context) (*device.DhcpDnsState, error) // only when a DNS Agent is selected
	// Apply applies one release operation and returns the one that undoes it.
	Apply(ctx context.Context, op json.RawMessage) (json.RawMessage, error)
	RenderScript(ops []json.RawMessage) (string, error)
	Close()
}

// BatchDriver is implemented by devices that can apply selected operation
// runs together and restore those undo records in bounded batches.
type BatchDriver interface {
	BatchKinds() []string
	ApplyMany(context.Context, []json.RawMessage, func(json.RawMessage, json.RawMessage)) error
	BatchRestorable(json.RawMessage) bool
	RestoreMany(context.Context, []json.RawMessage, bool) []string
}

// RetirePolicyRoutes routes proxy CIDRs by static routes and withdraws our
// policy rules (they need managed return routing).
func RetirePolicyRoutes(state *RouterState) {
	if state.PolicyRoutes == nil {
		return
	}
	state.RetiredPolicyRoutes = slices.DeleteFunc(slices.Clone(state.PolicyRoutes), func(e *device.PolicyRouteEntry) bool {
		return e.Comment != device.PolicyAutoComment && e.Comment != device.PolicyManualComment
	})
	state.PolicyRoutes = nil
}

// StaleRoutes are active routes to the gateway that no configured static
// route explains (iKuai keeps kernel routes after edits).
func StaleRoutes(state *RouterState, gateway string) []*device.RouteEntry {
	if gateway == "" || state.ActiveRoutes == nil {
		return []*device.RouteEntry{}
	}
	return slices.DeleteFunc(slices.Clone(state.ActiveRoutes), func(a *device.RouteEntry) bool {
		return a.Gateway != gateway || slices.ContainsFunc(state.Routes, func(r *device.RouteEntry) bool {
			return r.Enabled && r.Gateway == gateway && r.Dst == a.Dst
		})
	})
}

// StaleRouteWarning explains StaleRoutes to the user.
func StaleRouteWarning(routes []*device.RouteEntry, gateway string) string {
	dsts := make([]string, len(routes))
	for i, r := range routes {
		dsts[i] = r.Dst
	}
	return "路由器当前路由表中有 " + strconv.Itoa(len(routes)) + " 条指向 " + gateway + " 的路由不在静态路由配置里" +
		"（" + strings.Join(dsts, ", ") + "）：这是修改或删除静态路由后残留的内核路由，" +
		"优先于分流规则，会让 " + gateway + " 自身的流量回环。界面和接口都无法删除，需要重启路由器清除。"
}
