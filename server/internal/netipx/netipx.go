// Package netipx holds the few helpers the router code needs on top of
// net/netip, where Python's ipaddress accepts or answers something netip
// does not provide:
//
//   - ParseNetwork: ip_network(text, strict) forms (a bare host is /32 or
//     /128, a prefix with leading zeros, an IPv4 netmask or hostmask);
//   - IsPrivate/IsGlobal of a network with CPython 3.11.17's tables (which
//     proxy CIDRs count as public);
//   - Broadcast, ComparePrefix, Collapse (collapse_addresses).
//
// Not reproduced (functional parity, nothing in the router features depends
// on them): IPv6 scope ids in networks (rejected here, Python keeps them),
// Python's exception texts.
package netipx

import (
	"errors"
	"math/bits"
	"net/netip"
	"sort"
	"strconv"
	"strings"
)

// ParseAddr4 is IPv4Address(text) for a str.
func ParseAddr4(text string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(text)
	if err != nil || !addr.Is4() {
		return netip.Addr{}, false
	}
	return addr, true
}

func asciiDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// maskBits is the prefix length of an IPv4 netmask (255.255.0.0) or hostmask
// (0.0.255.255) given as dotted text.
func maskBits(text string) (int, bool) {
	addr, ok := ParseAddr4(text)
	if !ok {
		return 0, false
	}
	b := addr.As4()
	value := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	for _, v := range []uint32{value, ^value} {
		ones := bits.LeadingZeros32(^v)
		if ones == 32 || v<<uint(ones) == 0 {
			return ones, true
		}
	}
	return 0, false
}

// ParseNetwork is ipaddress.ip_network(text, strict): with strict false the
// host bits are cleared.
func ParseNetwork(text string, strict bool) (netip.Prefix, bool) {
	addrText, maskText, hasMask := strings.Cut(text, "/")
	if strings.Contains(maskText, "/") || strings.Contains(addrText, "%") {
		return netip.Prefix{}, false
	}
	addr, err := netip.ParseAddr(addrText)
	if err != nil {
		return netip.Prefix{}, false
	}
	n := addr.BitLen()
	if hasMask {
		if asciiDigits(maskText) {
			v, err := strconv.Atoi(maskText)
			if err != nil || v > addr.BitLen() {
				return netip.Prefix{}, false
			}
			n = v
		} else if addr.Is4() {
			v, ok := maskBits(maskText)
			if !ok {
				return netip.Prefix{}, false
			}
			n = v
		} else {
			return netip.Prefix{}, false
		}
	}
	prefix := netip.PrefixFrom(addr, n)
	masked := prefix.Masked()
	if strict && masked.Addr() != addr {
		return netip.Prefix{}, false
	}
	return masked, true
}

// ParseNetwork4 is IPv4Network(text, strict).
func ParseNetwork4(text string, strict bool) (netip.Prefix, bool) {
	p, ok := ParseNetwork(text, strict)
	if !ok || !p.Addr().Is4() {
		return netip.Prefix{}, false
	}
	return p, true
}

// Broadcast is network.broadcast_address (the last address).
func Broadcast(p netip.Prefix) netip.Addr {
	b := p.Masked().Addr().AsSlice()
	for i := p.Bits(); i < len(b)*8; i++ {
		b[i/8] |= 1 << (7 - i%8)
	}
	addr, _ := netip.AddrFromSlice(b)
	return addr
}

// ComparePrefix orders networks like Python: network address, then prefix
// length.
func ComparePrefix(a, b netip.Prefix) int {
	if c := a.Addr().Compare(b.Addr()); c != 0 {
		return c
	}
	return a.Bits() - b.Bits()
}

// SortPrefixes is sorted(networks) (one address family).
func SortPrefixes(list []netip.Prefix) {
	sort.SliceStable(list, func(i, j int) bool { return ComparePrefix(list[i], list[j]) < 0 })
}

func mustPrefixes(texts ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(texts))
	for i, t := range texts {
		out[i] = netip.MustParsePrefix(t)
	}
	return out
}

// CPython 3.11.17 _IPv4Constants / _IPv6Constants.
var (
	v4Private = mustPrefixes("0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32")
	v4PrivateExceptions = mustPrefixes("192.0.0.9/32", "192.0.0.10/32")
	v4Shared            = netip.MustParsePrefix("100.64.0.0/10")
	v6Private           = mustPrefixes("::1/128", "::/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "2001::/23",
		"2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10")
	v6PrivateExceptions = mustPrefixes("2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48",
		"2001:20::/28", "2001:30::/28")
)

// IsPrivate is network.is_private: both ends inside one private block and
// neither end inside an exception block.
func IsPrivate(p netip.Prefix) bool {
	private, exceptions := v4Private, v4PrivateExceptions
	if p.Addr().Is6() {
		private, exceptions = v6Private, v6PrivateExceptions
	}
	first, last := p.Masked().Addr(), Broadcast(p)
	inside := false
	for _, block := range private {
		if block.Contains(first) && block.Contains(last) {
			inside = true
			break
		}
	}
	if !inside {
		return false
	}
	for _, block := range exceptions {
		if block.Contains(first) || block.Contains(last) {
			return false
		}
	}
	return true
}

// IsGlobal is network.is_global.
func IsGlobal(p netip.Prefix) bool {
	if p.Addr().Is4() && v4Shared.Contains(p.Masked().Addr()) && v4Shared.Contains(Broadcast(p)) {
		return false
	}
	return !IsPrivate(p)
}

// ErrMixedVersions is collapse_addresses' TypeError for mixed families.
var ErrMixedVersions = errors.New("networks are not of the same version")

// Collapse is list(ipaddress.collapse_addresses(networks)): the smallest
// sorted list of networks covering exactly the union of the input.
func Collapse(networks []netip.Prefix) ([]netip.Prefix, error) {
	if len(networks) == 0 {
		return nil, nil
	}
	list := make([]netip.Prefix, len(networks))
	for i, n := range networks {
		if n.Addr().Is4() != networks[0].Addr().Is4() {
			return nil, ErrMixedVersions
		}
		list[i] = n.Masked()
	}
	SortPrefixes(list)
	var out []netip.Prefix
	first, last := list[0].Addr(), Broadcast(list[0])
	for _, n := range list[1:] {
		start := n.Addr()
		next := last.Next()
		if next.IsValid() && start.Compare(next) > 0 {
			out = append(out, summarize(first, last)...)
			first, last = start, Broadcast(n)
			continue
		}
		if end := Broadcast(n); end.Compare(last) > 0 {
			last = end
		}
	}
	return append(out, summarize(first, last)...), nil
}

// summarize is summarize_address_range(first, last).
func summarize(first, last netip.Addr) []netip.Prefix {
	var out []netip.Prefix
	width := first.BitLen()
	for {
		// The largest aligned block starting at first that ends at or before last.
		n := width
		for n > 0 {
			p := netip.PrefixFrom(first, n-1)
			if p.Masked().Addr() != first || Broadcast(p).Compare(last) > 0 {
				break
			}
			n--
		}
		block := netip.PrefixFrom(first, n)
		out = append(out, block)
		end := Broadcast(block)
		if end.Compare(last) >= 0 {
			return out
		}
		first = end.Next()
	}
}
