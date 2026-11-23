package routers

// Drift report: the desired side-gateway state compared with the router.
// Item statuses: synced (managed and correct), adoptable (correct but made by
// hand: the release tags it instead of recreating), conflict (other next hop
// or effective forwarding elsewhere), missing, not_required (RouterOS: no
// source-NAT rule can match), unknown (RouterOS: cannot be determined).
// Managed objects no longer desired are "extra".

import (
	"configflow/internal/routers/device"
	"configflow/internal/routers/dnsguard"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/ros"
	"encoding/json"
	"net/netip"
	"slices"
	"strings"

	"configflow/internal/netipx"
)

// Report is the drift report.
type Report struct {
	ReturnRouting *ReturnStatus  `json:"return_routing,omitempty"`
	Desired       *Desired       `json:"desired"`
	Routes        RoutesReport   `json:"routes"`
	NatBypass     NatReport      `json:"nat_bypass"`
	DhcpDNS       DNSReport      `json:"dhcp_dns"`
	Summary       map[string]int `json:"summary"`
	InSync        bool           `json:"in_sync"`
	IkuaiGroups   *GroupReport   `json:"ikuai_groups,omitempty"`
	Parts         []ReportPart   `json:"-"`
}

// MarshalJSON adds the later modules' sections under their keys.
func (r Report) MarshalJSON() ([]byte, error) {
	type plain Report
	if r.Desired != nil {
		desired := *r.Desired
		desired.ROSPolicy = nil
		desired.DNSGuard = nil
		r.Desired = &desired
	}
	data, err := json.Marshal(plain(r))
	if err != nil || len(r.Parts) == 0 {
		return data, err
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	for _, part := range r.Parts {
		if out[part.Key], err = json.Marshal(part.Section); err != nil {
			return nil, err
		}
	}
	return json.Marshal(out)
}

func gatewayOrUnknown(gateway string) string {
	if gateway == "" {
		return "未知"
	}
	return gateway
}

func desiredCIDRs(desired *Desired) map[string]bool {
	set := map[string]bool{}
	for _, e := range desired.ProxyCIDRs {
		set[e.CIDR] = true
	}
	return set
}

func routeIDs(routes []*RouteEntry) []string {
	ids := make([]string, len(routes))
	for i, r := range routes {
		ids[i] = r.ID
	}
	return ids
}

// policyRouteItems: proxy CIDRs routed by a policy rule that excludes the
// gateway as a source. Every enabled static route to the gateway loops and is
// migrated into a policy rule (hand-made ones into cf-mihomo-manual).
func policyRouteItems(desired *Desired, state *RouterState) RoutesReport {
	gateway := desired.ProxyGateway
	wanted := desiredCIDRs(desired)
	auto := device.PolicyRoute(state.PolicyRoutes, PolicyAutoComment)
	var covered []netip.Prefix
	if auto != nil && device.PolicyRouteOK(auto, gateway) {
		covered = device.Networks(auto.Dst)
	}
	coverage := device.NewSupernetIndex(covered)
	// A disabled static route forwards nothing: the policy rule decides.
	enabledByDst := map[string][]*RouteEntry{}
	for _, r := range state.Routes {
		if r.Enabled {
			enabledByDst[r.Dst] = append(enabledByDst[r.Dst], r)
		}
	}
	out := RoutesReport{Items: []RouteItem{}, Extra: []ExtraRoute{}, UnmanagedOther: []UnmanagedRoute{}, Mode: "policy",
		Policy: &PolicyRules{Auto: auto, Manual: device.PolicyRoute(state.PolicyRoutes, PolicyManualComment)}}
	for _, entry := range desired.ProxyCIDRs {
		matches := enabledByDst[entry.CIDR]
		item := RouteItem{CIDR: entry.CIDR, Sources: entry.Sources, Status: "missing", RouteIDs: routeIDs(matches)}
		target, _ := netipx.ParseNetwork4(entry.CIDR, true)
		if bad := slices.IndexFunc(matches, func(r *RouteEntry) bool { return gateway == "" || r.Gateway != gateway }); bad >= 0 {
			item.Status, item.Detail = "conflict", "同网段路由下一跳为 "+matches[bad].Gateway+"，期望下一跳 "+gatewayOrUnknown(gateway)
		} else if len(matches) > 0 {
			item.Status, item.Detail = "adoptable", "静态路由会让 mihomo 自身的流量回环，将迁移为排除 mihomo 的策略路由"
		} else if target.IsValid() && coverage.Covers(target) {
			item.Status = "synced"
		}
		out.Items = append(out.Items, item)
	}
	for _, r := range state.Routes {
		if device.IsManaged(r.Comment) && !wanted[r.Dst] {
			out.Extra = append(out.Extra, ExtraRoute{CIDR: r.Dst, Gateway: r.Gateway, RouteID: r.ID, Comment: r.Comment})
		}
	}
	if auto != nil {
		// iKuai may read an IP group's 1.1.1.1/32 back as 1.1.1.1: compare addresses.
		canon := map[string]bool{}
		for cidr := range wanted {
			canon[ikuai.CanonAddress(cidr)] = true
		}
		for _, cidr := range auto.Dst {
			if !canon[ikuai.CanonAddress(cidr)] {
				out.Extra = append(out.Extra, ExtraRoute{CIDR: cidr, Gateway: auto.Gateway, RouteID: "policy:" + auto.ID, Comment: auto.Comment})
			}
		}
	}
	for _, r := range state.Routes {
		if gateway != "" && r.Gateway == gateway && !device.IsManaged(r.Comment) && !wanted[r.Dst] {
			out.UnmanagedOther = append(out.UnmanagedOther, UnmanagedRoute{CIDR: r.Dst, Gateway: r.Gateway, Comment: r.Comment, Enabled: r.Enabled, Migrate: new(r.Enabled)})
		}
	}
	return out
}

func routeItems(desired *Desired, state *RouterState) RoutesReport {
	if state.PolicyRoutes != nil {
		return policyRouteItems(desired, state)
	}
	gateway := desired.ProxyGateway
	wanted := desiredCIDRs(desired)
	out := RoutesReport{Items: []RouteItem{}, Extra: []ExtraRoute{}, UnmanagedOther: []UnmanagedRoute{}}
	required := map[string]bool{}
	index := device.NewStaticRouteIndex(state.Routes)
	exact := map[string][]*RouteEntry{}
	for _, r := range state.Routes {
		if r.Table == "main" {
			exact[r.Dst] = append(exact[r.Dst], r)
		}
	}
	for _, entry := range desired.ProxyCIDRs {
		matches := exact[entry.CIDR]
		sel, _ := index.Select(entry.CIDR, gateway)
		good := sel.Candidates
		for _, r := range good {
			required[r.ID] = true
		}
		item := RouteItem{CIDR: entry.CIDR, Sources: entry.Sources, Status: "missing", RouteIDs: routeIDs(matches)}
		switch {
		case len(good) > 0 && !slices.ContainsFunc(good, func(r *RouteEntry) bool { return !device.IsManaged(r.Comment) }):
			item.Status = "synced"
		case len(good) > 0:
			item.Status, item.Detail = "adoptable", "已有手工路由提供完整有效覆盖，将直接接管"
		case len(matches) > 0:
			reason := "下一跳为 " + matches[0].Gateway
			if !matches[0].Enabled {
				reason = "已停用"
			}
			if slices.ContainsFunc(matches, func(r *RouteEntry) bool { return r.Enabled && r.Gateway == gateway }) {
				reason = "未成为有效主路由，或存在可优先/等价转发到其他下一跳的路由"
			}
			item.Status, item.Detail = "conflict", "同网段路由"+reason+"，期望下一跳 "+gatewayOrUnknown(gateway)
		case sel.Conflict:
			item.Status, item.Detail = "conflict", "更细网段存在可转发到其他下一跳的路由，期望下一跳 "+gatewayOrUnknown(gateway)
		}
		out.Items = append(out.Items, item)
	}
	for _, r := range state.Routes {
		// Policy ownership is established by its manifest, not its name.
		if r.Table == "main" && device.IsManaged(r.Comment) && !wanted[r.Dst] && !required[r.ID] &&
			!strings.HasPrefix(r.Comment, "cf-ros-policy-") {
			out.Extra = append(out.Extra, ExtraRoute{CIDR: r.Dst, Gateway: r.Gateway, RouteID: r.ID, Comment: r.Comment})
		}
	}
	// Hand-made routes to the gateway ConfigFlow does not derive (e.g. public
	// DNS routed for TUN DNS hijack) are informational, never touched.
	for _, r := range state.Routes {
		if r.Table == "main" && gateway != "" && r.Gateway == gateway && !device.IsManaged(r.Comment) && !wanted[r.Dst] {
			out.UnmanagedOther = append(out.UnmanagedOther, UnmanagedRoute{CIDR: r.Dst, Gateway: r.Gateway, Comment: r.Comment, Enabled: r.Enabled})
		}
	}
	return out
}

func natItems(desired *Desired, state *RouterState) NatReport {
	var rules []*NatBypassEntry
	out := NatReport{Items: []NatItem{}, DisabledRules: []DisabledNat{}}
	for _, r := range state.NatBypass {
		if r.Enabled {
			rules = append(rules, r)
		} else {
			out.DisabledRules = append(out.DisabledRules, DisabledNat{RuleID: r.ID, Comment: r.Comment, Dst: r.Dst})
		}
	}
	routeIndex := ros.NewRouteEgressIndex(state.NatContext)
	var cover *SupernetIndex
	for _, entry := range desired.ProxyCIDRs {
		item := NatItem{CIDR: entry.CIDR, RuleIDs: []string{}}
		if state.NatContext != nil {
			a := ros.AssessBypass(entry.CIDR, desired.ProxyGateway, &state.State, state.NatContext, routeIndex)
			item.Status, item.Detail, item.RuleIDs = a.Status, a.Detail, a.RuleIDs
			out.Items = append(out.Items, item)
			continue
		}
		if cover == nil {
			// Each rule's destinations are parsed once, not once per CIDR.
			cover = device.NewSupernetIndex(nil)
			for position, rule := range rules {
				cover.Add(device.Networks(rule.Dst), position)
			}
		}
		var covering []*NatBypassEntry
		if target, ok := netipx.ParseNetwork4(entry.CIDR, true); ok {
			for _, position := range cover.Owners(target) {
				covering = append(covering, rules[position])
			}
		}
		for _, r := range covering {
			item.RuleIDs = append(item.RuleIDs, r.ID)
		}
		switch {
		case slices.ContainsFunc(covering, func(r *NatBypassEntry) bool { return device.IsManaged(r.Comment) }):
			item.Status = "synced"
		case len(covering) > 0:
			name := covering[0].Comment
			if name == "" {
				name = covering[0].ID
			}
			item.Status, item.Detail = "adoptable", "已被手工规则「"+name+"」覆盖，将直接接管"
		default:
			item.Status, item.Detail = "missing", "未发现覆盖该网段的 NAT 免转换规则，需结合源 NAT 配置确认是否影响客户端 IP"
		}
		out.Items = append(out.Items, item)
	}
	return out
}

// BuildReport compares desired with the router state, calling each feature's
// report builder in fixed order (dns_guard, policy_routing, ipv6).
func BuildReport(desired *Desired, state *RouterState) (*Report, error) {
	r := &Report{Desired: desired, Routes: routeItems(desired, state), Summary: map[string]int{}}
	parts := []ReportPart{}
	if desired.DNSGuard != nil {
		p := dnsguard.Build(desired, state.DNSGuard, state.NatContext)
		state.DNSGuardPlan = p
		parts = append(parts, dnsguard.ReportPart(p))
	}
	if desired.ROSPolicy != nil {
		plan := BuildROSPolicy(desired, state.NatContext, state.ROSPolicy)
		state.ROSPolicyPlan = plan
		part, natRetiring := policyReportPart(desired, plan)
		state.PolicyNATRetiring = natRetiring
		parts = append(parts, part)
	}
	parts = append(parts, ipv6ReportParts(state.IPv6FakeIPPlan, state.IPv6RoutesPlan, state.IPv6DNSPlan, state.IPv6PDGuardPlan)...)
	r.Parts = parts
	for _, part := range parts {
		r.Routes.Extra = slices.DeleteFunc(r.Routes.Extra, func(e ExtraRoute) bool { return slices.Contains(part.RetireExtra, e.RouteID) })
	}
	// Policy rules being withdrawn because the router routes by static routes again.
	r.Routes.RetiredPolicy = []RetiredPolicyItem{}
	for _, entry := range state.RetiredPolicyRoutes {
		for _, cidr := range entry.Dst {
			r.Routes.RetiredPolicy = append(r.Routes.RetiredPolicy,
				RetiredPolicyItem{CIDR: cidr, Gateway: entry.Gateway, Comment: entry.Comment, Restore: entry.Comment == PolicyManualComment})
		}
	}
	r.NatBypass = natItems(desired, state)
	var dns DNSSettings
	if desired.DNSSettings != nil {
		dns = *desired.DNSSettings
	}
	r.DhcpDNS = device.BuildDNSReport(dns, desired.ProxyGateway, state.DhcpDNS)
	r.InSync = len(r.Routes.Extra) == 0 && len(r.Routes.RetiredPolicy) == 0 &&
		(r.DhcpDNS.Status == "disabled" || r.DhcpDNS.Status == "synced")
	for _, item := range r.Routes.Items {
		r.Summary[item.Status]++
		r.InSync = r.InSync && (item.Status == "synced" || item.Status == "not_required")
	}
	for _, item := range r.NatBypass.Items {
		r.Summary[item.Status]++
		r.InSync = r.InSync && (item.Status == "synced" || item.Status == "not_required")
	}
	r.Summary["extra"] = len(r.Routes.Extra)
	if r.DhcpDNS.Status != "disabled" {
		r.Summary[r.DhcpDNS.Status]++
	}
	for _, u := range r.Routes.UnmanagedOther {
		r.InSync = r.InSync && (u.Migrate == nil || !*u.Migrate)
	}
	for _, part := range parts {
		for _, status := range part.Statuses {
			r.Summary[status]++
		}
		r.InSync = r.InSync && part.InSync
	}
	if state.IkuaiGroups != nil {
		groups := ikuai.GroupReportSection(state.IkuaiGroups, &state.State)
		r.IkuaiGroups = &groups
		// Rules still listing CIDRs themselves, groups nothing uses, or a
		// marker to write back: a release is due.
		if due := len(groups.Migrations) + len(groups.Orphans) + len(groups.Marks); due > 0 {
			r.Summary["pending"] += due
			r.InSync = false
		}
	}
	return r, nil
}
