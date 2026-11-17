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
	return parts
}
