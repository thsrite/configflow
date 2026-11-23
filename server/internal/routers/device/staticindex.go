package device

import (
	"net/netip"
	"slices"
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

// IsTrue reports whether a boolean pointer is non-nil and true.
func IsTrue(b *bool) bool { return b != nil && *b }

// preferredRoutes keeps the routes that may forward: active ones, those at the
// best distance (of active, else live routes), live ones without evidence.
func preferredRoutes(routes []*RouteEntry) []*RouteEntry {
	live := slices.DeleteFunc(slices.Clone(routes), func(r *RouteEntry) bool { return r.Active != nil && !*r.Active })
	pool := slices.DeleteFunc(slices.Clone(live), func(r *RouteEntry) bool { return r.Active == nil })
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
		if len(live) > 0 && (IsTrue(r.Active) || r.Distance != nil && best != nil && *r.Distance <= *best ||
			(r.Active == nil || *r.Active) && (r.Distance == nil || best == nil)) {
			out = append(out, r)
		}
	}
	return out
}

// StaticRouteIndex holds one readback's enabled main-table routes by network;
// build one per readback, never reuse it across reads or writes.
type StaticRouteIndex struct {
	groups    map[netip.Prefix][]*RouteEntry
	preferred map[netip.Prefix][]*RouteEntry
	sorted    map[bool][]netip.Prefix // by Is4, sorted by (address, prefix length)
}

func NewStaticRouteIndex(routes []*RouteEntry) *StaticRouteIndex {
	ix := &StaticRouteIndex{groups: map[netip.Prefix][]*RouteEntry{}, preferred: map[netip.Prefix][]*RouteEntry{},
		sorted: map[bool][]netip.Prefix{}}
	for _, r := range routes {
		if network, ok := netipx.ParseNetwork(r.Dst, false); ok && r.Table == "main" && r.Enabled {
			if ix.groups[network] == nil {
				ix.sorted[network.Addr().Is4()] = append(ix.sorted[network.Addr().Is4()], network)
			}
			ix.groups[network] = append(ix.groups[network], r)
		}
	}
	for network, group := range ix.groups {
		ix.preferred[network] = preferredRoutes(group)
	}
	for _, list := range ix.sorted {
		netipx.SortPrefixes(list)
	}
	return ix
}

func firstManaged(routes []*RouteEntry) *RouteEntry {
	if i := slices.IndexFunc(routes, func(r *RouteEntry) bool { return IsManaged(r.Comment) }); i >= 0 {
		return routes[i]
	}
	return routes[0]
}

// Select is the forwarding evidence for cidr through gateway; ok is false
// for an invalid cidr (callers pass desired CIDRs).
func (ix *StaticRouteIndex) Select(cidr, gateway string) (sel StaticRouteSelection, ok bool) {
	target, ok := netipx.ParseNetwork(cidr, false)
	if !ok {
		return sel, false
	}
	low, high := target.Addr(), netipx.Broadcast(target)
	list := ix.sorted[low.Is4()]
	from := sort.Search(len(list), func(i int) bool { return !list[i].Addr().Less(low) })
	to := sort.Search(len(list), func(i int) bool { return high.Less(list[i].Addr()) })
	var networks []netip.Prefix // subnets of target with routes
	evidence := false
	for _, n := range list[from:to] {
		if n.Bits() >= target.Bits() {
			networks = append(networks, n)
			for _, r := range ix.groups[n] {
				evidence = evidence || r.Active != nil || r.Distance != nil
			}
		}
	}
	if !evidence {
		// Legacy drivers: only the exact route with the right gateway counts.
		good := slices.DeleteFunc(slices.Clone(ix.groups[target]), func(r *RouteEntry) bool {
			return gateway == "" || r.Gateway != gateway
		})
		if len(good) > 0 {
			sel = StaticRouteSelection{Candidates: []*RouteEntry{firstManaged(good)}, Selected: good, Covered: true}
		}
		return sel, true
	}

	// Sweep the target range: between consecutive boundaries (network starts
	// and the address after each network) the most specific preferred network
	// covering the boundary forwards.
	networks = slices.DeleteFunc(networks, func(n netip.Prefix) bool { return len(ix.preferred[n]) == 0 })
	points := []netip.Addr{low}
	for _, n := range networks {
		points = append(points, n.Addr())
		if next := netipx.Broadcast(n).Next(); next.IsValid() && !high.Less(next) {
			points = append(points, next)
		}
	}
	slices.SortFunc(points, netip.Addr.Compare)
	var chosen []netip.Prefix
	sel.Covered = true
	for _, point := range slices.Compact(points) {
		var best netip.Prefix
		for _, n := range networks {
			if n.Contains(point) && (!best.IsValid() || n.Bits() > best.Bits()) {
				best = n
			}
		}
		if !best.IsValid() {
			sel.Covered = false
		} else if !slices.Contains(chosen, best) {
			chosen = append(chosen, best)
		}
	}
	for _, n := range chosen {
		sel.Selected = append(sel.Selected, ix.preferred[n]...)
	}
	sel.Conflict = slices.ContainsFunc(sel.Selected, func(r *RouteEntry) bool { return gateway == "" || r.Gateway != gateway })
	if !sel.Covered || sel.Conflict {
		return sel, true
	}
	// Own the effective exact anchor, or the full more-specific cover.
	ownership := [][]*RouteEntry{ix.preferred[target]}
	if len(ownership[0]) == 0 || slices.ContainsFunc(ownership[0], func(r *RouteEntry) bool { return r.Gateway != gateway }) {
		ownership = ownership[:0]
		for _, n := range chosen {
			ownership = append(ownership, ix.preferred[n])
		}
	}
	for _, group := range ownership {
		live := slices.DeleteFunc(slices.Clone(group), func(r *RouteEntry) bool { return !IsTrue(r.Active) })
		if len(live) == 0 {
			live = slices.DeleteFunc(slices.Clone(group), func(r *RouteEntry) bool { return r.Active != nil })
		}
		sel.Candidates = append(sel.Candidates, firstManaged(live))
	}
	return sel, true
}

// StaticRouteCandidates is the complete ownership proof for one lookup; empty when coverage is incomplete or unsafe.
func StaticRouteCandidates(routes []*RouteEntry, cidr, gateway string) []*RouteEntry {
	sel, _ := NewStaticRouteIndex(routes).Select(cidr, gateway)
	return sel.Candidates
}
