package pyip

import (
	"errors"
	"sort"
)

func mustNet(text string) Network {
	n, ok := ParseNetwork(text, true)
	if !ok {
		panic("pyip: bad constant " + text)
	}
	return n
}

func nets(texts ...string) []Network {
	out := make([]Network, len(texts))
	for i, t := range texts {
		out[i] = mustNet(t)
	}
	return out
}

// CPython 3.11.17 _IPv4Constants / _IPv6Constants.
var (
	v4Private = nets("0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32")
	v4PrivateExceptions = nets("192.0.0.9/32", "192.0.0.10/32")
	v4Public            = mustNet("100.64.0.0/10")
	v4Loopback          = mustNet("127.0.0.0/8")
	v4Multicast         = mustNet("224.0.0.0/4")

	v6Private = nets("::1/128", "::/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "2001::/23",
		"2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10")
	v6PrivateExceptions = nets("2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48",
		"2001:20::/28", "2001:30::/28")
	v6Loopback  = mustNet("::1/128")
	v6Multicast = mustNet("ff00::/8")
)

func anyContains(list []Network, a Addr) bool {
	for _, n := range list {
		if n.Contains(a) {
			return true
		}
	}
	return false
}

// ipv4Mapped is IPv6Address.ipv4_mapped.
func (a Addr) ipv4Mapped() (Addr, bool) {
	if a.ver != 6 || a.v.Hi != 0 || a.v.Lo>>32 != 0xffff {
		return Addr{}, false
	}
	return Addr{ver: 4, v: U128{Lo: a.v.Lo & 0xffffffff}}, true
}

// IsPrivate is address.is_private.
func (a Addr) IsPrivate() bool {
	if a.ver == 4 {
		return anyContains(v4Private, a) && !anyContains(v4PrivateExceptions, a)
	}
	if m, ok := a.ipv4Mapped(); ok {
		return m.IsPrivate()
	}
	return anyContains(v6Private, a) && !anyContains(v6PrivateExceptions, a)
}

// IsGlobal is address.is_global.
func (a Addr) IsGlobal() bool {
	if a.ver == 4 {
		return !v4Public.Contains(a) && !a.IsPrivate()
	}
	if m, ok := a.ipv4Mapped(); ok {
		return m.IsGlobal()
	}
	return !a.IsPrivate()
}

// IsLoopback is address.is_loopback.
func (a Addr) IsLoopback() bool {
	if a.ver == 4 {
		return v4Loopback.Contains(a)
	}
	if m, ok := a.ipv4Mapped(); ok {
		return m.IsLoopback()
	}
	return v6Loopback.Contains(a)
}

// IsMulticast is address.is_multicast.
func (a Addr) IsMulticast() bool {
	if a.ver == 4 {
		return v4Multicast.Contains(a)
	}
	if m, ok := a.ipv4Mapped(); ok {
		return m.IsMulticast()
	}
	return v6Multicast.Contains(a)
}

// IsUnspecified is address.is_unspecified (IPv4: == 0.0.0.0).
func (a Addr) IsUnspecified() bool {
	if a.ver == 4 {
		return a.v == U128{}
	}
	if m, ok := a.ipv4Mapped(); ok {
		return m.IsUnspecified()
	}
	return a.v == U128{}
}

// IsPrivate is network.is_private (3.11.17: both ends inside one private
// block and neither end inside an exception block).
func (n Network) IsPrivate() bool {
	private, exceptions := v4Private, v4PrivateExceptions
	if n.ver == 6 {
		private, exceptions = v6Private, v6PrivateExceptions
	}
	first, last := n.NetworkAddress(), n.BroadcastAddress()
	inside := false
	for _, p := range private {
		if p.Contains(first) && p.Contains(last) {
			inside = true
			break
		}
	}
	if !inside {
		return false
	}
	for _, e := range exceptions {
		if e.Contains(first) || e.Contains(last) {
			return false
		}
	}
	return true
}

// IsGlobal is network.is_global.
func (n Network) IsGlobal() bool {
	if n.ver == 4 {
		return !(v4Public.Contains(n.NetworkAddress()) && v4Public.Contains(n.BroadcastAddress())) && !n.IsPrivate()
	}
	return !n.IsPrivate()
}

// ErrMixedVersions is collapse_addresses' TypeError.
type ErrMixedVersions struct{ A, B string }

func (e *ErrMixedVersions) Error() string { return e.A + " and " + e.B + " are not of the same version" }

// PythonClass is "TypeError".
func (e *ErrMixedVersions) PythonClass() string { return "TypeError" }

// IsMixedVersions reports an ErrMixedVersions.
func IsMixedVersions(err error) bool {
	var m *ErrMixedVersions
	return errors.As(err, &m)
}

// SortNetworks is sorted(networks) / list.sort() (CPython's algorithm with
// Network.Less, see Sort).
func SortNetworks(list []Network) {
	Sort(list, Network.Less)
}

// CollapseAddresses is list(ipaddress.collapse_addresses(networks)) for
// networks (no bare addresses).
func CollapseAddresses(networks []Network) ([]Network, error) {
	var ips []Addr
	var nets []Network
	for _, n := range networks {
		if n.prefix == n.MaxPrefix() {
			if len(ips) > 0 && ips[len(ips)-1].ver != n.ver {
				return nil, &ErrMixedVersions{n.String(), ips[len(ips)-1].String()}
			}
			ips = append(ips, n.NetworkAddress())
		} else {
			if len(nets) > 0 && nets[len(nets)-1].ver != n.ver {
				return nil, &ErrMixedVersions{n.String(), nets[len(nets)-1].String()}
			}
			nets = append(nets, n)
		}
	}
	// sorted(set(ips)): equal values are one entry; addresses that differ
	// only by scope id sort as equals (their relative order does not change
	// the result: summarizing drops scope ids).
	seen := map[Addr]bool{}
	var unique []Addr
	for _, a := range ips {
		if !seen[a] {
			seen[a] = true
			unique = append(unique, a)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return unique[i].v.Less(unique[j].v) })
	var addrs []Network
	if len(unique) > 0 {
		// _find_address_range
		first, last := unique[0], unique[0]
		flush := func() {
			addrs = append(addrs, summarizeRange(first, last)...)
		}
		for _, ip := range unique[1:] {
			next, overflow := last.v.Add1()
			if overflow || ip.v != next {
				flush()
				first = ip
			}
			last = ip
		}
		flush()
	}
	all := append(addrs, nets...)
	for _, n := range all {
		if n.ver != all[0].ver {
			// Python raises this from sorted() inside _collapse_addresses_internal;
			// which pair it names depends on the sort's comparisons.
			return nil, &ErrMixedVersions{n.String(), all[0].String()}
		}
	}
	return collapseInternal(all), nil
}

// summarizeRange is summarize_address_range(first, last).
func summarizeRange(first, last Addr) []Network {
	width := first.maxPrefix()
	var out []Network
	f, l := first.v, last.v
	for !l.Less(f) {
		span, overflow := l.Sub(f).Add1()
		var spanBits int
		if overflow {
			spanBits = 128
		} else {
			spanBits = span.BitLen() - 1
		}
		nbits := f.trailingZeros(width)
		if nbits > width {
			nbits = width
		}
		if spanBits < nbits {
			nbits = spanBits
		}
		out = append(out, Network{ver: first.ver, base: f, prefix: width - nbits})
		if nbits == 128 {
			break
		}
		next, overflow := f.AddU(Shl1(nbits))
		if overflow {
			break
		}
		if first.ver == 4 && next.Lo > 0xffffffff {
			break
		}
		f = next
	}
	return out
}

// collapseInternal is _collapse_addresses_internal.
func collapseInternal(addresses []Network) []Network {
	toMerge := append([]Network(nil), addresses...)
	var order []Network // insertion order of the subnets dict
	subnets := map[Network]Network{}
	for len(toMerge) > 0 {
		net := toMerge[len(toMerge)-1]
		toMerge = toMerge[:len(toMerge)-1]
		super := net.Supernet()
		existing, ok := subnets[super]
		if !ok {
			subnets[super] = net
			order = append(order, super)
		} else if existing != net {
			delete(subnets, super)
			for i, k := range order {
				if k == super {
					order = append(order[:i], order[i+1:]...)
					break
				}
			}
			toMerge = append(toMerge, super)
		}
	}
	values := make([]Network, 0, len(order))
	for _, k := range order {
		values = append(values, subnets[k])
	}
	SortNetworks(values)
	var out []Network
	var last *Network
	for i := range values {
		net := values[i]
		if last != nil && !last.BroadcastAddress().v.Less(net.BroadcastAddress().v) {
			continue
		}
		out = append(out, net)
		last = &values[i]
	}
	return out
}
