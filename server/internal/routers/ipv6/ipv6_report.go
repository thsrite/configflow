package ipv6

import "configflow/internal/routers/device"

import "slices"

func ReportParts(fake *IPv6FakeIPPlan, routes *IPv6RoutesPlan, dns *IPv6DNSPlan, pd *IPv6PDGuardPlan) []device.ReportPart {
	parts := []device.ReportPart{}
	if p := fake; p != nil {
		statuses := []string{}
		if p.Status != "disabled" && p.Status != "borrowed" {
			statuses = append(statuses, p.Status)
		}
		parts = append(parts, device.ReportPart{Key: "ipv6_fakeip", Section: p.IPv6FakeIPReport, Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced", "borrowed"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	if p := routes; p != nil {
		statuses := []string{}
		for _, item := range p.Items {
			if item.Status != "borrowed" {
				statuses = append(statuses, item.Status)
			}
		}
		parts = append(parts, device.ReportPart{Key: "ipv6_routes", Section: p.IPv6RoutesReport, Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced"}, p.Status) && len(p.Skip) == 0 && len(p.Ops) == 0})
	}
	if p := dns; p != nil {
		statuses := []string{}
		if p.Status != "disabled" {
			statuses = append(statuses, p.Status)
		}
		parts = append(parts, device.ReportPart{Key: "ipv6_dns", Section: DNSPublic(p), Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	if p := pd; p != nil {
		statuses := []string{}
		if p.Status != "disabled" && p.Status != "borrowed" {
			s := p.Status
			if s == "blocked" {
				s = "unknown"
			}
			statuses = append(statuses, s)
		}
		parts = append(parts, device.ReportPart{Key: "ipv6_pd_guard", Section: PDPublic(p), Statuses: statuses, InSync: slices.Contains([]string{"disabled", "synced", "borrowed"}, p.Status) && len(p.Blockers) == 0 && len(p.Ops) == 0})
	}
	return parts
}

func DNSPublic(p *IPv6DNSPlan) IPv6DNSReport {
	out := p.IPv6DNSReport
	out.Blockers = slices.Clone(out.Blockers)
	out.Warnings = slices.Clone(out.Warnings)
	out.Items = slices.Clone(out.Items)
	for i := range out.Items {
		out.Items[i].DNSServers = slices.Clone(out.Items[i].DNSServers)
	}
	return out
}
func PDPublic(p *IPv6PDGuardPlan) IPv6PDGuardReport {
	out := p.IPv6PDGuardReport
	out.Blockers = slices.Clone(out.Blockers)
	out.Warnings = slices.Clone(out.Warnings)
	out.Items = slices.Clone(out.Items)
	return out
}
