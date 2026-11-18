package routers

import "slices"

func ipv6ReportParts(state *RouterState) []ReportPart {
	parts := []ReportPart{}
	if p := state.IPv6FakeIPPlan; p != nil {
		statuses := []string{}
		if p.Status != "disabled" && p.Status != "borrowed" {
			statuses = append(statuses, p.Status)
		}
		parts = append(parts, ReportPart{Key: "ipv6_fakeip", Section: p.IPv6FakeIPReport, Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced", "borrowed"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	if p := state.IPv6RoutesPlan; p != nil {
		statuses := []string{}
		for _, item := range p.Items {
			if item.Status != "borrowed" {
				statuses = append(statuses, item.Status)
			}
		}
		parts = append(parts, ReportPart{Key: "ipv6_routes", Section: p.IPv6RoutesReport, Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced"}, p.Status) && len(p.Skip) == 0 && len(p.Ops) == 0})
	}
	if p := state.IPv6DNSPlan; p != nil {
		statuses := []string{}
		if p.Status != "disabled" {
			statuses = append(statuses, p.Status)
		}
		parts = append(parts, ReportPart{Key: "ipv6_dns", Section: ipv6DNSPublic(p), Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	if p := state.IPv6PDGuardPlan; p != nil {
		statuses := []string{}
		if p.Status != "disabled" && p.Status != "borrowed" {
			s := p.Status
			if s == "blocked" {
				s = "unknown"
			}
			statuses = append(statuses, s)
		}
		parts = append(parts, ReportPart{Key: "ipv6_pd_guard", Section: ipv6PDPublic(p), Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced", "borrowed"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	return parts
}

func ipv6DNSPublic(p *IPv6DNSPlan) IPv6DNSReport {
	out := p.IPv6DNSReport
	out.Blockers = slices.Clone(out.Blockers)
	out.Warnings = slices.Clone(out.Warnings)
	out.Items = slices.Clone(out.Items)
	for i := range out.Items {
		out.Items[i].DNSServers = slices.Clone(out.Items[i].DNSServers)
	}
	return out
}
func ipv6PDPublic(p *IPv6PDGuardPlan) IPv6PDGuardReport {
	out := p.IPv6PDGuardReport
	out.Blockers = slices.Clone(out.Blockers)
	out.Warnings = slices.Clone(out.Warnings)
	out.Items = slices.Clone(out.Items)
	return out
}
