package rules

import (
	"math/big"
	"math/bits"
	"net/netip"
	"strconv"
	"strings"
)

// Python's ipaddress module semantics for the subset the rule engine uses:
// ip_address(text) and ip_network(text, strict=False).

type u128 struct{ hi, lo uint64 }

func (a u128) and(b u128) u128 { return u128{a.hi & b.hi, a.lo & b.lo} }

// ipAddr is an ipaddress.IPv4Address or IPv6Address. Equality (including as a
// map key) compares version, value and IPv6 scope id, like Python's.
type ipAddr struct {
	ver  int // 4 or 6
	v    u128
	zone string
}

func (a ipAddr) maxPrefix() int {
	if a.ver == 4 {
		return 32
	}
	return 128
}

// parseIPAddress is ipaddress.ip_address(text).
func parseIPAddress(text string) (ipAddr, bool) {
	if text == "" || strings.ContainsAny(text, " \t\r\n\v\f/") {
		return ipAddr{}, false
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return ipAddr{}, false
	}
	zone := addr.Zone()
	if strings.Contains(zone, "%") {
		return ipAddr{}, false
	}
	if addr.Is4() {
		b := addr.As4()
		return ipAddr{ver: 4, v: u128{lo: uint64(b[0])<<24 | uint64(b[1])<<16 | uint64(b[2])<<8 | uint64(b[3])}}, true
	}
	b := addr.As16()
	var hi, lo uint64
	for i := 0; i < 8; i++ {
		hi = hi<<8 | uint64(b[i])
		lo = lo<<8 | uint64(b[8+i])
	}
	return ipAddr{ver: 6, v: u128{hi, lo}, zone: zone}, true
}

// ipNet is an ip_network(..., strict=False) result: host bits are cleared.
type ipNet struct {
	ver    int
	base   u128
	prefix int
	zone   string // IPv6 scope id, kept only while no host bit was cleared
}

func maskFor(ver, prefix int) u128 {
	total := 32
	if ver == 6 {
		total = 128
	}
	host := uint(total - prefix)
	if ver == 4 {
		return u128{lo: (uint64(1)<<32 - 1) &^ (uint64(1)<<host - 1)}
	}
	switch {
	case host == 0:
		return u128{^uint64(0), ^uint64(0)}
	case host >= 128:
		return u128{}
	case host >= 64:
		return u128{hi: ^uint64(0) << (host - 64)}
	default:
		return u128{hi: ^uint64(0), lo: ^uint64(0) << host}
	}
}

func (n ipNet) contains(a ipAddr) bool {
	return a.ver == n.ver && a.v.and(maskFor(n.ver, n.prefix)) == n.base
}

// prefixFromPrefixString is _BaseV4._prefix_from_prefix_string.
func prefixFromPrefixString(text string, max int) (int, bool) {
	if !pyIsASCIIDigits(text) {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	if err != nil {
		// Digit strings too large for int are above any valid prefix.
		return 0, false
	}
	if value < 0 || value > max {
		return 0, false
	}
	return value, true
}

// prefixFromIPInt is _prefix_from_ip_int for a 32-bit netmask.
func prefixFromIPInt(value uint32) (int, bool) {
	trailing := 32
	if value != 0 {
		trailing = bits.TrailingZeros32(value)
	}
	prefix := 32 - trailing
	var leading uint32
	if trailing < 32 {
		leading = value >> uint(trailing)
	}
	allOnes := uint32(0)
	if prefix > 0 {
		allOnes = uint32((uint64(1) << uint(prefix)) - 1)
	}
	return prefix, leading == allOnes
}

// parseIPNetwork is ipaddress.ip_network(text, strict=False).
func parseIPNetwork(text string) (ipNet, bool) {
	parts := strings.Split(text, "/")
	if len(parts) > 2 {
		return ipNet{}, false
	}
	addr, ok := parseIPAddress(parts[0])
	if !ok {
		return ipNet{}, false
	}
	prefix := addr.maxPrefix()
	if len(parts) == 2 {
		mask := parts[1]
		if p, ok := prefixFromPrefixString(mask, addr.maxPrefix()); ok {
			prefix = p
		} else if addr.ver == 4 {
			netmask, ok := parseIPAddress(mask)
			if !ok || netmask.ver != 4 || netmask.zone != "" {
				return ipNet{}, false
			}
			value := uint32(netmask.v.lo)
			p, ok := prefixFromIPInt(value)
			if !ok {
				p, ok = prefixFromIPInt(value ^ 0xffffffff)
			}
			if !ok {
				return ipNet{}, false
			}
			prefix = p
		} else {
			return ipNet{}, false
		}
	}
	network := ipNet{ver: addr.ver, base: addr.v.and(maskFor(addr.ver, prefix)), prefix: prefix}
	if network.base == addr.v {
		network.zone = addr.zone
	}
	return network, true
}

// isValidIP is is_valid_ip.
func isValidIP(text string) bool {
	_, ok := parseIPAddress(text)
	return ok
}

// subnetOf reports whether n lies inside outer (ipaddress subnet_of:
// outer.network_address <= n.network_address and outer.broadcast_address >=
// n.broadcast_address). IPv6Address.__eq__ also compares scope ids, so equal
// network addresses with different scopes are not "<=" (broadcast addresses
// never carry a scope): fe80::/64 is not a subnet of fe80::%eth0/64.
func (n ipNet) subnetOf(outer ipNet) bool {
	if n.ver != outer.ver || n.prefix < outer.prefix || !outer.contains(ipAddr{ver: n.ver, v: n.base}) {
		return false
	}
	return outer.base != n.base || outer.zone == n.zone
}

// String renders the network like str(ip_network) of CPython 3.11: IPv6 is
// compressed (longest zero run of two or more groups), IPv4-mapped addresses
// keep a dotted tail and a scope id stays attached.
func (n ipNet) String() string {
	text := formatIP(n.ver, n.base)
	if n.zone != "" {
		text += "%" + n.zone
	}
	return text + "/" + strconv.Itoa(n.prefix)
}

func formatIP(ver int, v u128) string {
	if ver == 4 {
		x := uint32(v.lo)
		return strconv.Itoa(int(x>>24)) + "." + strconv.Itoa(int(x>>16&255)) + "." +
			strconv.Itoa(int(x>>8&255)) + "." + strconv.Itoa(int(x&255))
	}
	if v.hi == 0 && v.lo>>32 == 0xffff {
		// CPython 3.11.17 prints IPv4-mapped addresses with a dotted tail.
		return "::ffff:" + formatIP(4, u128{lo: v.lo & 0xffffffff})
	}
	var groups [8]uint64
	for i := 0; i < 4; i++ {
		groups[i] = v.hi >> (48 - 16*uint(i)) & 0xffff
		groups[4+i] = v.lo >> (48 - 16*uint(i)) & 0xffff
	}
	bestStart, bestLen := -1, 0
	for i := 0; i < 8; {
		if groups[i] != 0 {
			i++
			continue
		}
		j := i
		for j < 8 && groups[j] == 0 {
			j++
		}
		if j-i > bestLen {
			bestStart, bestLen = i, j-i
		}
		i = j
	}
	if bestLen < 2 {
		bestStart = -1
	}
	var b strings.Builder
	for i := 0; i < 8; i++ {
		if i == bestStart {
			b.WriteString("::")
			i += bestLen - 1
			continue
		}
		if b.Len() > 0 && !strings.HasSuffix(b.String(), ":") {
			b.WriteByte(':')
		}
		b.WriteString(strconv.FormatUint(groups[i], 16))
	}
	return b.String()
}

// bigString is str(int(address)) for a 128-bit value.
func bigString(v u128) string {
	if v.hi == 0 {
		return strconv.FormatUint(v.lo, 10)
	}
	number := new(big.Int).SetUint64(v.hi)
	number.Lsh(number, 64)
	number.Or(number, new(big.Int).SetUint64(v.lo))
	return number.String()
}
