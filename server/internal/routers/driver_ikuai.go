package routers

import (
	"context"
	"encoding/json"

	"configflow/internal/routers/device"
	"configflow/internal/routers/ikuai"
	"configflow/internal/routers/routeedit"
)

// Ikuai is the composed iKuai 4.x driver: it embeds the low-level client and adds ReadState.
type Ikuai struct {
	*ikuai.Client
}

// NewIkuai constructs an iKuai 4.x driver with requests-compatible device HTTP.
func NewIkuai(endpoint, username, password string, verifyTLS bool) *Ikuai {
	return &Ikuai{Client: ikuai.NewClient(endpoint, username, password, verifyTLS)}
}

// NewIkuaiWithHTTP creates an iKuai driver with an injected device client.
func NewIkuaiWithHTTP(endpoint, username, password string, client DeviceHTTP) *Ikuai {
	return &Ikuai{Client: ikuai.NewClientWithHTTP(endpoint, username, password, client)}
}

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
		if dst, ok := ikuai.Network(row); ok {
			state.Routes = append(state.Routes, device.NewRoute(row.Text("id"), dst, row.Text("gateway"),
				row.Is("enabled", "yes"), ikuai.PercentDecode(row.Text("comment"))))
		}
	}
	// IDs are optional here: only an edit or delete of one of our groups needs one.
	groups, err := d.ReadGroups(ctx)
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
		if !row.Is("action", "filter") {
			continue
		}
		state.NatBypass = append(state.NatBypass, &NatBypassEntry{ID: row.Text("id"),
			Dst: ikuai.Addresses(row["dst_addr"], state.AddressSets, byGID), Src: ikuai.Addresses(row["src_addr"], state.AddressSets, byGID),
			Enabled: row.Is("enabled", "yes"), Comment: ikuai.PercentDecode(row.Text("comment")), Attributes: json.RawMessage("{}")})
	}
	policyRaw, err := d.ListAll(ctx, "stream_ipport", "", true, "")
	if err != nil {
		return nil, err
	}
	policyRows := ikuai.DecodeRows(policyRaw)
	for _, row := range policyRows {
		entry := &PolicyRouteEntry{ID: row.Text("id"), Dst: ikuai.Addresses(row["dst_addr"], state.AddressSets, byGID),
			Enabled: row.Is("enabled", "yes"), Comment: row.Text("comment"),
			Src:       ikuai.Addresses(row["src_addr"], state.AddressSets, byGID),
			SrcInvert: ikuai.NumberEquals(row["src_addr_inv"], 1), DstInvert: ikuai.NumberEquals(row["dst_addr_inv"], 1),
			Protocol: row.Text("protocol")}
		if ikuai.NumberEquals(row["type"], ikuai.PolicyNexthop) {
			entry.Gateway = row.Text("nexthop")
		}
		if prio, ok := ikuai.JSONInt(row["prio"]); ok {
			entry.Priority = &prio
		}
		state.PolicyRoutes = append(state.PolicyRoutes, entry)
	}
	state.IkuaiGroups = ikuai.ReadGroupRules(groups, ikuai.DecodeRows(natRows), policyRows)
	state.ActiveRoutes = d.ActiveRoutes(ctx)
	return state, nil
}

// ReadRoutes implements routeedit.RouteEditor for iKuai.
func (d *Ikuai) ReadRoutes(ctx context.Context) ([]*EditableRoute, error) {
	return routeedit.ReadIkuaiRoutes(ctx, d.Client)
}

// MutateRoute implements routeedit.RouteEditor for iKuai.
func (d *Ikuai) MutateRoute(ctx context.Context, action string, expected *EditableRoute, values *RouteValues) (*RouteMutation, error) {
	return routeedit.MutateIkuaiRoute(ctx, d.Client, action, expected, values)
}
