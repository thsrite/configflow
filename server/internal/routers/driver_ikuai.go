package routers

import (
	"context"
	"encoding/json"

	"configflow/internal/routers/device"
)

// ReadState reads static routes, IP groups, NAT filters, 端口分流 rules, and
// the routes the router forwards with. 端口分流 rules route proxy CIDRs: a
// static route would also catch the gateway's own (DIRECT) traffic to a proxy
// CIDR and bounce it between router and gateway forever.
func (d *Ikuai) ReadState(ctx context.Context) (*RouterState, error) {
	state := &RouterState{Routes: []*RouteEntry{}, NatBypass: []*NatBypassEntry{},
		AddressSets: map[string][]string{}, PolicyRoutes: []*PolicyRouteEntry{}}
	routes, err := d.ListAll(ctx, "static_rt", "", true, "")
	if err != nil {
		return nil, err
	}
	for _, row := range routes {
		if dst, ok := ikuaiNetwork(row); ok {
			state.Routes = append(state.Routes, device.NewRoute(row.text("id"), dst, row.text("gateway"),
				row.is("enabled", "yes"), percentDecode(row.text("comment"))))
		}
	}
	// IDs are optional here: only an edit or delete of one of our groups needs one.
	groups, err := d.readGroups(ctx)
	if err != nil {
		return nil, err
	}
	byGID := map[string][]string{}
	for _, g := range groups {
		state.AddressSets[g.Name] = g.Values
		if g.GID != "" {
			byGID[g.GID] = g.Values
		}
	}
	natRows, err := d.ListAll(ctx, "nat_rule", "", true, "")
	if err != nil {
		return nil, err
	}
	for _, row := range natRows {
		if !row.is("action", "filter") {
			continue
		}
		state.NatBypass = append(state.NatBypass, &NatBypassEntry{ID: row.text("id"),
			Dst: ikuaiAddresses(row["dst_addr"], state.AddressSets, byGID), Src: ikuaiAddresses(row["src_addr"], state.AddressSets, byGID),
			Enabled: row.is("enabled", "yes"), Comment: percentDecode(row.text("comment")), Attributes: json.RawMessage("{}")})
	}
	policyRaw, err := d.ListAll(ctx, "stream_ipport", "", true, "")
	if err != nil {
		return nil, err
	}
	policyRows := decodeRows(policyRaw)
	for _, row := range policyRows {
		entry := &PolicyRouteEntry{ID: row.text("id"), Dst: ikuaiAddresses(row["dst_addr"], state.AddressSets, byGID),
			Enabled: row.is("enabled", "yes"), Comment: row.text("comment"),
			Src:       ikuaiAddresses(row["src_addr"], state.AddressSets, byGID),
			SrcInvert: numberEquals(row["src_addr_inv"], 1), DstInvert: numberEquals(row["dst_addr_inv"], 1),
			Protocol: row.text("protocol")}
		if numberEquals(row["type"], ikuaiPolicyNexthop) {
			entry.Gateway = row.text("nexthop")
		}
		if prio, ok := jsonInt(row["prio"]); ok {
			entry.Priority = &prio
		}
		state.PolicyRoutes = append(state.PolicyRoutes, entry)
	}
	state.IkuaiGroups = ReadGroupRules(groups, decodeRows(natRows), policyRows)
	state.ActiveRoutes = d.activeRoutes(ctx)
	return state, nil
}
