package routers

import (
	"container/heap"
	"sort"

	"net/netip"

	"configflow/internal/netipx"
)

// preferredRoutes is _preferred_routes(routes).
func preferredRoutes(routes []*RouteEntry) []*RouteEntry {
	var live, active []*RouteEntry
	for _, route := range routes {
		if route.Active == nil || *route.Active {
			live = append(live, route)
		}
	}
	if len(live) == 0 {
		return []*RouteEntry{}
	}
	for _, route := range live {
		if route.Active != nil && *route.Active {
			active = append(active, route)
		}
	}
	pool := active
	if len(pool) == 0 {
		pool = live
	}
	var distance *int64
	for _, route := range pool {
		if route.Distance != nil && (distance == nil || *route.Distance < *distance) {
			d := *route.Distance
			distance = &d
		}
	}
	out := []*RouteEntry{}
	for _, route := range routes {
		isActive := route.Active != nil && *route.Active
		notInactive := route.Active == nil || *route.Active
		if isActive ||
			(route.Distance != nil && distance != nil && *route.Distance <= *distance) ||
			(notInactive && (route.Distance == nil || distance == nil)) {
			out = append(out, route)
		}
	}
	return out
}

// StaticRouteIndex is one readback's parsed routes; callers discard it after
// their batch.
type StaticRouteIndex struct {
	// Exact is index.exact: every main-table route by its dst text.
	Exact      map[string][]*RouteEntry
	groups     map[netip.Prefix][]*RouteEntry
	groupOrder []netip.Prefix
	preferred  map[netip.Prefix][]*RouteEntry
	networks   map[int][]netip.Prefix
	starts     map[int][]netip.Addr
}

// NewStaticRouteIndex is StaticRouteIndex(routes).
func NewStaticRouteIndex(routes []*RouteEntry) *StaticRouteIndex {
	ix := &StaticRouteIndex{Exact: map[string][]*RouteEntry{}, groups: map[netip.Prefix][]*RouteEntry{},
		preferred: map[netip.Prefix][]*RouteEntry{}, networks: map[int][]netip.Prefix{}, starts: map[int][]netip.Addr{}}
	for _, route := range routes {
		if route.Table != "main" {
			continue
		}
		ix.Exact[route.Dst] = append(ix.Exact[route.Dst], route)
		if !route.Enabled {
			continue
		}
		network, ok := netipx.ParseNetwork(route.Dst, false)
		if !ok {
			continue
		}
		if _, seen := ix.groups[network]; !seen {
			ix.groupOrder = append(ix.groupOrder, network)
		}
		ix.groups[network] = append(ix.groups[network], route)
	}
	for _, network := range ix.groupOrder {
		ix.preferred[network] = preferredRoutes(ix.groups[network])
	}
	for _, version := range []int{4, 6} {
		var list []netip.Prefix
		for _, network := range ix.groupOrder {
			if versionOf(network) == version {
				list = append(list, network)
			}
		}
		// sorted(key=(int(network_address), prefixlen)): a consistent order, stable.
		netipx.SortPrefixes(list)
		ix.networks[version] = list
		starts := make([]netip.Addr, len(list))
		for i, network := range list {
			starts[i] = network.Addr()
		}
		ix.starts[version] = starts
	}
	return ix
}

func versionOf(p netip.Prefix) int {
	if p.Addr().Is4() {
		return 4
	}
	return 6
}

// wide is an address or the value one past the last address of the family
// (broadcast_address + 1).
type wide struct {
	over bool
	v    netip.Addr
}

func wideOf(v netip.Addr) wide { return wide{v: v} }

func widePlus1(v netip.Addr) wide {
	next := v.Next()
	if !next.IsValid() {
		return wide{over: true}
	}
	return wide{v: next}
}

func (a wide) less(b wide) bool {
	if a.over || b.over {
		return !a.over && b.over
	}
	return a.v.Less(b.v)
}

type interval struct {
	start, end netip.Addr
	network    netip.Prefix
}

type heapItem struct {
	negPrefix int
	end       netip.Addr
	cursor    int
	network   netip.Prefix
}

type routeHeap []heapItem

func (h routeHeap) Len() int { return len(h) }
func (h routeHeap) Less(i, j int) bool {
	a, b := h[i], h[j]
	if a.negPrefix != b.negPrefix {
		return a.negPrefix < b.negPrefix
	}
	if a.end != b.end {
		return a.end.Less(b.end)
	}
	return a.cursor < b.cursor
}
func (h routeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *routeHeap) Push(x any)   { *h = append(*h, x.(heapItem)) }
func (h *routeHeap) Pop() any {
	old := *h
	item := old[len(old)-1]
	*h = old[:len(old)-1]
	return item
}

// Select is StaticRouteIndex.select(cidr, gateway). The error is
// ip_network's ValueError for an invalid cidr.
func (ix *StaticRouteIndex) Select(cidr, gateway string) (*StaticRouteSelection, error) {
	target, ok := netipx.ParseNetwork(cidr, false)
	if !ok {
		return nil, pyErr("ValueError", "invalid network: "+cidr)
	}
	starts := ix.starts[versionOf(target)]
	low := target.Addr()
	high := netipx.Broadcast(target)
	left := sort.Search(len(starts), func(i int) bool { return !starts[i].Less(low) })
	right := sort.Search(len(starts), func(i int) bool { return high.Less(starts[i]) })
	var networks []netip.Prefix
	for _, network := range ix.networks[versionOf(target)][left:right] {
		if network.Bits() >= target.Bits() {
			networks = append(networks, network)
		}
	}
	exact := ix.groups[target]
	inNetworks := false
	for _, network := range networks {
		if network == target {
			inNetworks = true
		}
	}
	if !inNetworks {
		exact = nil
	}
	result := &StaticRouteSelection{Candidates: []*RouteEntry{}, Selected: []*RouteEntry{}}
	evidence := false
	for _, network := range networks {
		for _, route := range ix.groups[network] {
			if route.Active != nil || route.Distance != nil {
				evidence = true
			}
		}
	}
	if !evidence {
		var good []*RouteEntry
		for _, route := range exact {
			if gateway != "" && route.Gateway == gateway {
				good = append(good, route)
			}
		}
		if len(good) > 0 {
			chosen := good[0]
			for _, route := range good {
				if IsManaged(route.Comment) {
					chosen = route
					break
				}
			}
			result.Candidates = []*RouteEntry{chosen}
			result.Selected = good
			result.Covered = true
		}
		return result, nil
	}

	var preferredOrder []netip.Prefix
	preferred := map[netip.Prefix][]*RouteEntry{}
	for _, network := range networks {
		if group := ix.preferred[network]; len(group) > 0 {
			if _, seen := preferred[network]; !seen {
				preferredOrder = append(preferredOrder, network)
			}
			preferred[network] = group
		}
	}
	boundaries := map[wide]bool{wideOf(low): true, widePlus1(high): true}
	var intervals []interval
	for _, network := range preferredOrder {
		start, end := network.Addr(), netipx.Broadcast(network)
		boundaries[wideOf(start)] = true
		boundaries[widePlus1(end)] = true
		intervals = append(intervals, interval{start, end, network})
	}
	sorted := make([]wide, 0, len(boundaries))
	for b := range boundaries {
		sorted = append(sorted, b)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].less(sorted[j]) })
	var selectedOrder []netip.Prefix
	selectedGroups := map[netip.Prefix][]*RouteEntry{}
	result.Covered = true
	active := &routeHeap{}
	cursor := 0
	for _, start := range sorted[:len(sorted)-1] {
		for cursor < len(intervals) && !start.less(wideOf(intervals[cursor].start)) {
			item := intervals[cursor]
			heap.Push(active, heapItem{-item.network.Bits(), item.end, cursor, item.network})
			cursor++
		}
		for active.Len() > 0 && wideOf((*active)[0].end).less(start) {
			heap.Pop(active)
		}
		if active.Len() == 0 {
			result.Covered = false
			continue
		}
		network := (*active)[0].network
		if _, seen := selectedGroups[network]; !seen {
			selectedOrder = append(selectedOrder, network)
		}
		selectedGroups[network] = preferred[network]
	}
	for _, network := range selectedOrder {
		result.Selected = append(result.Selected, selectedGroups[network]...)
	}
	for _, route := range result.Selected {
		if gateway == "" || route.Gateway != gateway {
			result.Conflict = true
			break
		}
	}
	if result.Covered && !result.Conflict {
		anchors := preferred[target]
		ownership := [][]*RouteEntry{}
		allGood := len(anchors) > 0
		for _, route := range anchors {
			if route.Gateway != gateway {
				allGood = false
			}
		}
		if allGood {
			ownership = append(ownership, anchors)
		} else {
			for _, network := range selectedOrder {
				ownership = append(ownership, selectedGroups[network])
			}
		}
		for _, group := range ownership {
			var live []*RouteEntry
			for _, route := range group {
				if route.Active != nil && *route.Active {
					live = append(live, route)
				}
			}
			if len(live) == 0 {
				for _, route := range group {
					if route.Active == nil || *route.Active {
						live = append(live, route)
					}
				}
			}
			if len(live) == 0 {
				return nil, pyErr("IndexError", "list index out of range")
			}
			chosen := live[0]
			for _, route := range live {
				if IsManaged(route.Comment) {
					chosen = route
					break
				}
			}
			result.Candidates = append(result.Candidates, chosen)
		}
	}
	return result, nil
}

// StaticRouteSelectionOf is static_route_selection(routes, cidr, gateway).
func StaticRouteSelectionOf(routes []*RouteEntry, cidr, gateway string) (*StaticRouteSelection, error) {
	return NewStaticRouteIndex(routes).Select(cidr, gateway)
}

// StaticRouteCandidates is static_route_candidates(routes, cidr, gateway).
func StaticRouteCandidates(routes []*RouteEntry, cidr, gateway string) ([]*RouteEntry, error) {
	selection, err := StaticRouteSelectionOf(routes, cidr, gateway)
	if err != nil {
		return nil, err
	}
	return selection.Candidates, nil
}
