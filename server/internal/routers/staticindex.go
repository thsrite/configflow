package routers

import (
	"net/netip"
	"sort"

	"configflow/internal/netipx"
)

// StaticRouteSelection is the forwarding evidence for one CIDR. Every
// Candidate must be owned to verify a deployment (a wrong aggregate shadowed
// by correct more-specific routes is never one); Selected lists every route
// that may forward, for NAT evidence.
type StaticRouteSelection struct {
	Candidates []*RouteEntry
	Selected   []*RouteEntry
	Covered    bool
	Conflict   bool
}

// preferredRoutes keeps the routes that may forward: active ones, those at
// the best distance, and those without evidence.
func preferredRoutes(routes []*RouteEntry) []*RouteEntry {
	var live, active []*RouteEntry
	for _, r := range routes {
		if r.Active == nil || *r.Active {
			live = append(live, r)
			if r.Active != nil {
				active = append(active, r)
			}
		}
	}
	if len(live) == 0 {
		return nil
	}
	pool := active
	if len(pool) == 0 {
		pool = live
	}
	var best *int64
	for _, r := range pool {
		if r.Distance != nil && (best == nil || *r.Distance < *best) {
			best = r.Distance
		}
	}
	var out []*RouteEntry
	for _, r := range routes {
		isActive := r.Active != nil && *r.Active
		if isActive || (r.Distance != nil && best != nil && *r.Distance <= *best) ||
			((r.Active == nil || *r.Active) && (r.Distance == nil || best == nil)) {
			out = append(out, r)
		}
	}
	return out
}

// StaticRouteIndex holds one readback's main-table routes, grouped by
// network and sorted, so each lookup only visits possible subnets. Build one
// per readback; never reuse it across reads or writes.
type StaticRouteIndex struct {
	groups    map[netip.Prefix][]*RouteEntry
	preferred map[netip.Prefix][]*RouteEntry
	sorted    map[bool][]netip.Prefix // by Is4, sorted by (address, prefix length)
}

// NewStaticRouteIndex indexes the enabled main-table routes.
func NewStaticRouteIndex(routes []*RouteEntry) *StaticRouteIndex {
	ix := &StaticRouteIndex{groups: map[netip.Prefix][]*RouteEntry{}, preferred: map[netip.Prefix][]*RouteEntry{},
		sorted: map[bool][]netip.Prefix{}}
	for _, r := range routes {
		if r.Table != "main" || !r.Enabled {
			continue
		}
		network, ok := netipx.ParseNetwork(r.Dst, false)
		if !ok {
			continue
		}
		if ix.groups[network] == nil {
			ix.sorted[network.Addr().Is4()] = append(ix.sorted[network.Addr().Is4()], network)
		}
		ix.groups[network] = append(ix.groups[network], r)
	}
	for network, group := range ix.groups {
		ix.preferred[network] = preferredRoutes(group)
	}
	for _, list := range ix.sorted {
		netipx.SortPrefixes(list)
	}
	return ix
}

// edge is an interval boundary: an address, or one past the family's last
// address (end).
type edge struct {
	end  bool
	addr netip.Addr
}

func after(a netip.Addr) edge {
	if next := a.Next(); next.IsValid() {
		return edge{addr: next}
	}
	return edge{end: true}
}

func (a edge) less(b edge) bool { return !a.end && (b.end || a.addr.Less(b.addr)) }

func firstManaged(routes []*RouteEntry) *RouteEntry {
	for _, r := range routes {
		if IsManaged(r.Comment) {
			return r
		}
	}
	return routes[0]
}

// Select is the forwarding evidence for cidr through gateway. An invalid
// cidr is a ValidationError-free programming error: callers pass desired
// CIDRs, so false is returned in ok.
func (ix *StaticRouteIndex) Select(cidr, gateway string) (sel StaticRouteSelection, ok bool) {
	target, ok := netipx.ParseNetwork(cidr, false)
	if !ok {
		return sel, false
	}
	low, high := target.Addr(), netipx.Broadcast(target)
	list := ix.sorted[low.Is4()]
	from := sort.Search(len(list), func(i int) bool { return !list[i].Addr().Less(low) })
	to := sort.Search(len(list), func(i int) bool { return high.Less(list[i].Addr()) })
	var networks []netip.Prefix
	evidence := false
	for _, n := range list[from:to] {
		if n.Bits() < target.Bits() {
			continue
		}
		networks = append(networks, n)
		for _, r := range ix.groups[n] {
			evidence = evidence || r.Active != nil || r.Distance != nil
		}
	}
	if !evidence {
		// Legacy drivers: only the exact route with the right gateway counts.
		var good []*RouteEntry
		for _, r := range ix.groups[target] {
			if gateway != "" && r.Gateway == gateway {
				good = append(good, r)
			}
		}
		if len(good) > 0 {
			sel = StaticRouteSelection{Candidates: []*RouteEntry{firstManaged(good)}, Selected: good, Covered: true}
		}
		return sel, true
	}

	// Sweep the target range; at each boundary the most specific preferred
	// network covering it forwards.
	var intervals []netip.Prefix
	edges := map[edge]bool{{addr: low}: true, after(high): true}
	for _, n := range networks {
		if len(ix.preferred[n]) > 0 {
			intervals = append(intervals, n)
			edges[edge{addr: n.Addr()}] = true
			edges[after(netipx.Broadcast(n))] = true
		}
	}
	points := make([]edge, 0, len(edges))
	for e := range edges {
		points = append(points, e)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].less(points[j]) })
	var chosen []netip.Prefix
	picked := map[netip.Prefix]bool{}
	sel.Covered = true
	for _, point := range points[:len(points)-1] {
		// The most specific preferred network covering this point forwards it.
		var best netip.Prefix
		for _, n := range intervals {
			if !point.end && n.Contains(point.addr) && (!best.IsValid() || n.Bits() > best.Bits()) {
				best = n
			}
		}
		if !best.IsValid() {
			sel.Covered = false
		} else if !picked[best] {
			picked[best] = true
			chosen = append(chosen, best)
		}
	}
	for _, n := range chosen {
		sel.Selected = append(sel.Selected, ix.preferred[n]...)
	}
	for _, r := range sel.Selected {
		if gateway == "" || r.Gateway != gateway {
			sel.Conflict = true
		}
	}
	if !sel.Covered || sel.Conflict {
		return sel, true
	}
	// Own the effective exact anchor, or the full more-specific cover.
	ownership := [][]*RouteEntry{}
	anchors := ix.preferred[target]
	anchorOK := len(anchors) > 0
	for _, r := range anchors {
		anchorOK = anchorOK && r.Gateway == gateway
	}
	if anchorOK {
		ownership = append(ownership, anchors)
	} else {
		for _, n := range chosen {
			ownership = append(ownership, ix.preferred[n])
		}
	}
	for _, group := range ownership {
		var live []*RouteEntry
		for _, r := range group {
			if r.Active != nil && *r.Active {
				live = append(live, r)
			}
		}
		if len(live) == 0 {
			for _, r := range group {
				if r.Active == nil {
					live = append(live, r)
				}
			}
		}
		sel.Candidates = append(sel.Candidates, firstManaged(live))
	}
	return sel, true
}

// StaticRouteCandidates is the complete ownership proof for one lookup;
// empty when coverage is incomplete or unsafe.
func StaticRouteCandidates(routes []*RouteEntry, cidr, gateway string) []*RouteEntry {
	sel, _ := NewStaticRouteIndex(routes).Select(cidr, gateway)
	return sel.Candidates
}
