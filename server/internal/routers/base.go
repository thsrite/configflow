// Package routers is the port of backend/routers (upstream router
// integration: drivers, desired state, drift report, release) and the
// inspection part of backend/routes/routers.py. Modules land one at a time
// (server/parity/PROGRESS-routers.md); calls into modules that are not ported
// yet go through the collaborator variables in pending.go.
//
// Dataclasses are Go structs used by pointer (Python passes objects by
// reference and code mutates them in place). Optional[...] fields are
// pointers or nil-able values: nil is None. For Optional[List] fields a nil
// slice is None and a non-nil empty slice is [].
package routers

import (
	"strconv"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// ManagedPrefix is MANAGED_PREFIX: generated deployments carry it.
const ManagedPrefix = "cf"

// RouterInfo is the RouterInfo dataclass.
type RouterInfo struct {
	Vendor       string
	Version      string
	Model        string
	Capabilities []string
}

// RouteEntry is the RouteEntry dataclass (Table defaults to "main"; Active
// and Distance nil mean the driver supplied no runtime evidence).
type RouteEntry struct {
	ID       string
	Dst      string // CIDR, e.g. 198.18.0.0/16
	Gateway  string
	Enabled  bool
	Comment  string
	Table    string
	Active   *bool
	Distance *int64
}

// NewRouteEntry is RouteEntry(id, dst, gateway, enabled, comment) with the
// defaults table='main', active=None, distance=None.
func NewRouteEntry(id, dst, gateway string, enabled bool, comment string) *RouteEntry {
	return &RouteEntry{ID: id, Dst: dst, Gateway: gateway, Enabled: enabled, Comment: comment, Table: "main"}
}

// StaticRouteSelection is the StaticRouteSelection dataclass.
type StaticRouteSelection struct {
	Candidates []*RouteEntry
	Selected   []*RouteEntry
	Covered    bool
	Conflict   bool
}

// PolicyRouteEntry is the PolicyRouteEntry dataclass (iKuai 端口分流 with a
// next hop).
type PolicyRouteEntry struct {
	ID        string
	Dst       []string
	Gateway   string
	Enabled   bool
	Comment   string
	Src       []string
	SrcInvert bool
	DstInvert bool
	Protocol  string
	Priority  *int64
}

// NatBypassEntry is the NatBypassEntry dataclass.
type NatBypassEntry struct {
	ID         string
	Dst        []string
	Src        []string
	Enabled    bool
	Comment    string
	Attributes *jsonx.Object // never nil after New…; Python default {}
}

// DhcpDnsEntry is the DhcpDnsEntry dataclass.
type DhcpDnsEntry struct {
	ID          string
	Network     string
	DNS         []string
	Enabled     bool // default True
	Attributes  *jsonx.Object
	Blockers    []string
	DNSExplicit bool // default True
}

// NewDhcpDnsEntry is DhcpDnsEntry(id, network, dns) with the defaults.
func NewDhcpDnsEntry(id, network string, dns []string) *DhcpDnsEntry {
	return &DhcpDnsEntry{ID: id, Network: network, DNS: dns, Enabled: true, Attributes: jsonx.NewObject(),
		Blockers: []string{}, DNSExplicit: true}
}

// DhcpDnsState is the DhcpDnsState dataclass.
type DhcpDnsState struct {
	Entries  []*DhcpDnsEntry
	Evidence *jsonx.Object
}

// RouterState is the RouterState dataclass (see the Python field comments).
type RouterState struct {
	Routes              []*RouteEntry
	NatBypass           []*NatBypassEntry
	AddressSets         *jsonx.Object // name -> list of CIDRs; Python default {}
	NatContext          *jsonx.Object
	NatLists            *jsonx.Object
	NatListsReader      func() (*jsonx.Object, error) // repr=False, compare=False
	DhcpDNS             *DhcpDnsState
	PolicyRoutes        []*PolicyRouteEntry // nil: None
	RetiredPolicyRoutes []*PolicyRouteEntry // nil: None
	ActiveRoutes        []*RouteEntry       // nil: None
	RosPolicy           *jsonx.Object
	RosPolicyPlan       *jsonx.Object
	IPv6FakeipPlan      *jsonx.Object
	IPv6RoutesPlan      *jsonx.Object
	IPv6DNSPlan         *jsonx.Object
	IPv6PDGuardPlan     *jsonx.Object
	DNSGuard            *jsonx.Object
	DNSGuardPlan        *jsonx.Object
	IkuaiGroups         *jsonx.Object
}

// NewRouterState is RouterState() with the dataclass defaults.
func NewRouterState() *RouterState {
	return &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{}, AddressSets: jsonx.NewObject()}
}

// Driver is RouterDriver: probe/read the device and apply release
// operations one at a time (Apply returns the operation that undoes it).
// Later modules add optional capability interfaces for what Python probes
// with hasattr.
type Driver interface {
	Vendor() string
	SupportsWrite() bool
	Probe() (*RouterInfo, error)
	ReadState() (*RouterState, error)
	// ReadDHCPDNS reads DHCP DNS only when the user selected a DNS Agent.
	ReadDHCPDNS() (*DhcpDnsState, error)
	Apply(op *jsonx.Object) (*jsonx.Object, error)
	RenderScript(ops []*jsonx.Object) (string, error)
	Close()
}

// BaseDriver provides RouterDriver's default methods; embed it and set
// VendorName.
type BaseDriver struct {
	VendorName string
	Writable   bool
}

func (d *BaseDriver) Vendor() string      { return d.VendorName }
func (d *BaseDriver) SupportsWrite() bool { return d.Writable }

// Probe is RouterDriver.probe (NotImplementedError).
func (d *BaseDriver) Probe() (*RouterInfo, error) { return nil, pyErr("NotImplementedError", "") }

// ReadState is RouterDriver.read_state (NotImplementedError).
func (d *BaseDriver) ReadState() (*RouterState, error) { return nil, pyErr("NotImplementedError", "") }

// ReadDHCPDNS is RouterDriver.read_dhcp_dns.
func (d *BaseDriver) ReadDHCPDNS() (*DhcpDnsState, error) {
	return nil, NewRouterError(d.VendorName + " 不支持 DHCP DNS 下发")
}

// Apply is RouterDriver.apply.
func (d *BaseDriver) Apply(op *jsonx.Object) (*jsonx.Object, error) {
	return nil, NewRouterError(d.VendorName + " 不支持直接写入")
}

// RenderScript is RouterDriver.render_script.
func (d *BaseDriver) RenderScript(ops []*jsonx.Object) (string, error) { return "", nil }

// Close is RouterDriver.close.
func (d *BaseDriver) Close() {}

// IsManaged is is_managed(comment).
func IsManaged(comment string) bool {
	text := pystr.Lower(pystr.Strip(comment))
	return text == ManagedPrefix || strings.HasPrefix(text, ManagedPrefix+":") || strings.HasPrefix(text, ManagedPrefix+"-")
}

// ManagedComment is managed_comment(comment): "tg" -> "cf-tg".
func ManagedComment(comment string) string {
	text := pystr.Strip(comment)
	if IsManaged(text) {
		return text
	}
	if text != "" {
		return ManagedPrefix + "-" + text
	}
	return ManagedPrefix
}

// Policy route comments and priority (POLICY_AUTO_COMMENT etc.).
const (
	PolicyAutoComment   = ManagedPrefix + "-mihomo"
	PolicyManualComment = ManagedPrefix + "-mihomo-manual"
	RestoredComment     = "mihomo-manual"
	PolicyPriority      = 0
)

// PolicyRouteOK is policy_route_ok(entry, gateway).
func PolicyRouteOK(entry *PolicyRouteEntry, gateway string) bool {
	return entry.Enabled && gateway != "" && entry.Gateway == gateway && len(entry.Src) == 1 && entry.Src[0] == gateway &&
		entry.SrcInvert && !entry.DstInvert && entry.Protocol == "any" &&
		entry.Priority != nil && *entry.Priority == PolicyPriority
}

// PolicyRoute is policy_route(state, comment): the first policy route
// carrying comment.
func PolicyRoute(state *RouterState, comment string) *PolicyRouteEntry {
	for _, entry := range state.PolicyRoutes {
		if entry.Comment == comment {
			return entry
		}
	}
	return nil
}

// RetirePolicyRoutes is retire_policy_routes(state).
func RetirePolicyRoutes(state *RouterState) {
	if state.PolicyRoutes == nil {
		return
	}
	retired := []*PolicyRouteEntry{}
	for _, entry := range state.PolicyRoutes {
		if entry.Comment == PolicyAutoComment || entry.Comment == PolicyManualComment {
			retired = append(retired, entry)
		}
	}
	state.RetiredPolicyRoutes = retired
	state.PolicyRoutes = nil
}

// StaleRoutes is stale_routes(state, gateway).
func StaleRoutes(state *RouterState, gateway string) []*RouteEntry {
	out := []*RouteEntry{}
	if gateway == "" || state.ActiveRoutes == nil {
		return out
	}
	configured := map[string]bool{}
	for _, route := range state.Routes {
		if route.Enabled && route.Gateway == gateway {
			configured[route.Dst] = true
		}
	}
	for _, route := range state.ActiveRoutes {
		if route.Gateway == gateway && !configured[route.Dst] {
			out = append(out, route)
		}
	}
	return out
}

// StaleRouteWarning is stale_route_warning(routes, gateway).
func StaleRouteWarning(routes []*RouteEntry, gateway string) string {
	dsts := make([]string, len(routes))
	for i, route := range routes {
		dsts[i] = route.Dst
	}
	return "路由器当前路由表中有 " + strconv.Itoa(len(routes)) + " 条指向 " + gateway + " 的路由不在静态路由配置里" +
		"（" + strings.Join(dsts, ", ") + "）：这是修改或删除静态路由后残留的内核路由，" +
		"优先于分流规则，会让 " + gateway + " 自身的流量回环。界面和接口都无法删除，需要重启路由器清除。"
}

// ---- as_dict (dataclasses.asdict: field order, deep copies) ----

func optBool(v *bool) any {
	if v == nil {
		return nil
	}
	return *v
}

func optInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}

func copyObject(o *jsonx.Object) any {
	if o == nil {
		return nil
	}
	return o.DeepCopy()
}

// AsDict is as_dict(info).
func (i *RouterInfo) AsDict() *jsonx.Object {
	return jsonx.Obj("vendor", i.Vendor, "version", i.Version, "model", i.Model, "capabilities", anyStrings(i.Capabilities))
}

// AsDict is as_dict(route).
func (r *RouteEntry) AsDict() *jsonx.Object {
	return jsonx.Obj("id", r.ID, "dst", r.Dst, "gateway", r.Gateway, "enabled", r.Enabled, "comment", r.Comment,
		"table", r.Table, "active", optBool(r.Active), "distance", optInt(r.Distance))
}

func routeList(routes []*RouteEntry) []any {
	out := []any{}
	for _, r := range routes {
		out = append(out, r.AsDict())
	}
	return out
}

// AsDict is as_dict(selection).
func (s *StaticRouteSelection) AsDict() *jsonx.Object {
	return jsonx.Obj("candidates", routeList(s.Candidates), "selected", routeList(s.Selected), "covered", s.Covered,
		"conflict", s.Conflict)
}

// AsDict is as_dict(entry).
func (p *PolicyRouteEntry) AsDict() *jsonx.Object {
	return jsonx.Obj("id", p.ID, "dst", anyStrings(p.Dst), "gateway", p.Gateway, "enabled", p.Enabled,
		"comment", p.Comment, "src", anyStrings(p.Src), "src_invert", p.SrcInvert, "dst_invert", p.DstInvert,
		"protocol", p.Protocol, "priority", optInt(p.Priority))
}

// AsDict is as_dict(entry).
func (n *NatBypassEntry) AsDict() *jsonx.Object {
	return jsonx.Obj("id", n.ID, "dst", anyStrings(n.Dst), "src", anyStrings(n.Src), "enabled", n.Enabled,
		"comment", n.Comment, "attributes", copyObject(n.Attributes))
}

// AsDict is as_dict(entry).
func (d *DhcpDnsEntry) AsDict() *jsonx.Object {
	return jsonx.Obj("id", d.ID, "network", d.Network, "dns", anyStrings(d.DNS), "enabled", d.Enabled,
		"attributes", copyObject(d.Attributes), "blockers", anyStrings(d.Blockers), "dns_explicit", d.DNSExplicit)
}

// AsDict is as_dict(state).
func (d *DhcpDnsState) AsDict() *jsonx.Object {
	entries := []any{}
	for _, e := range d.Entries {
		entries = append(entries, e.AsDict())
	}
	return jsonx.Obj("entries", entries, "evidence", copyObject(d.Evidence))
}

func policyList(list []*PolicyRouteEntry) any {
	if list == nil {
		return nil
	}
	out := []any{}
	for _, p := range list {
		out = append(out, p.AsDict())
	}
	return out
}

// AsDict is as_dict(state). Python copies nat_lists_reader (a function,
// which no JSON encoder accepts); it is written as None here.
func (s *RouterState) AsDict() *jsonx.Object {
	nat := []any{}
	for _, n := range s.NatBypass {
		nat = append(nat, n.AsDict())
	}
	var dhcp any
	if s.DhcpDNS != nil {
		dhcp = s.DhcpDNS.AsDict()
	}
	var active any
	if s.ActiveRoutes != nil {
		active = routeList(s.ActiveRoutes)
	}
	return jsonx.Obj("routes", routeList(s.Routes), "nat_bypass", nat, "address_sets", copyObject(s.AddressSets),
		"nat_context", copyObject(s.NatContext), "nat_lists", copyObject(s.NatLists), "nat_lists_reader", nil,
		"dhcp_dns", dhcp, "policy_routes", policyList(s.PolicyRoutes),
		"retired_policy_routes", policyList(s.RetiredPolicyRoutes), "active_routes", active,
		"ros_policy", copyObject(s.RosPolicy), "ros_policy_plan", copyObject(s.RosPolicyPlan),
		"ipv6_fakeip_plan", copyObject(s.IPv6FakeipPlan), "ipv6_routes_plan", copyObject(s.IPv6RoutesPlan),
		"ipv6_dns_plan", copyObject(s.IPv6DNSPlan), "ipv6_pd_guard_plan", copyObject(s.IPv6PDGuardPlan),
		"dns_guard", copyObject(s.DNSGuard), "dns_guard_plan", copyObject(s.DNSGuardPlan),
		"ikuai_groups", copyObject(s.IkuaiGroups))
}
