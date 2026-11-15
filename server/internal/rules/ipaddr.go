package rules

import (
	"math/big"
	"math/bits"
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

// parseIPAddress is ipaddress.ip_address(text): CPython 3.11's IPv4Address
// and IPv6Address string parsers (net/netip differs: it refuses white space
// and other text in a scope id, which Python keeps verbatim).
func parseIPAddress(text string) (ipAddr, bool) {
	if strings.Contains(text, "/") {
		return ipAddr{}, false
	}
	if v, ok := parseIPv4Int(text); ok {
		return ipAddr{ver: 4, v: u128{lo: uint64(v)}}, true
	}
	addr, zone, found := strings.Cut(text, "%")
	if found && (zone == "" || strings.Contains(zone, "%")) {
		return ipAddr{}, false
	}
	v, ok := parseIPv6Int(addr)
	if !ok {
		return ipAddr{}, false
	}
	return ipAddr{ver: 6, v: v, zone: zone}, true
}

// parseIPv4Int is IPv4Address._ip_int_from_string.
func parseIPv4Int(text string) (uint32, bool) {
	if text == "" {
		return 0, false
	}
	octets := strings.Split(text, ".")
	if len(octets) != 4 {
		return 0, false
	}
	var value uint32
	for _, octet := range octets {
		if !pyIsASCIIDigits(octet) || len(octet) > 3 || (octet != "0" && octet[0] == '0') {
			return 0, false
		}
		n, _ := strconv.Atoi(octet)
		if n > 255 {
			return 0, false
		}
		value = value<<8 | uint32(n)
	}
	return value, true
}

// parseHextet is IPv6Address._parse_hextet.
func parseHextet(text string) (uint64, bool) {
	if text == "" || len(text) > 4 {
		return 0, false
	}
	n, err := strconv.ParseUint(text, 16, 64)
	if err != nil || strings.ContainsAny(text, "+-_xX") {
		return 0, false
	}
	return n, true
}

// parseIPv6Int is IPv6Address._ip_int_from_string (without the scope id).
func parseIPv6Int(text string) (u128, bool) {
	if text == "" {
		return u128{}, false
	}
	parts := strings.Split(text, ":")
	if len(parts) < 3 {
		return u128{}, false
	}
	if last := parts[len(parts)-1]; strings.Contains(last, ".") {
		v4, ok := parseIPv4Int(last)
		if !ok {
			return u128{}, false
		}
		parts = append(parts[:len(parts)-1], strconv.FormatUint(uint64(v4>>16), 16), strconv.FormatUint(uint64(v4&0xffff), 16))
	}
	if len(parts) > 9 {
		return u128{}, false
	}
	skip := -1
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			if skip >= 0 {
				return u128{}, false
			}
			skip = i
		}
	}
	var hi, lo, skipped int
	if skip >= 0 {
		hi, lo = skip, len(parts)-skip-1
		if parts[0] == "" {
			if hi--; hi != 0 {
				return u128{}, false
			}
		}
		if parts[len(parts)-1] == "" {
			if lo--; lo != 0 {
				return u128{}, false
			}
		}
		skipped = 8 - (hi + lo)
		if skipped < 1 {
			return u128{}, false
		}
	} else {
		if len(parts) != 8 || parts[0] == "" || parts[len(parts)-1] == "" {
			return u128{}, false
		}
		hi = len(parts)
	}
	var value u128
	shift := func(bits int, add uint64) {
		for ; bits > 0; bits -= 16 {
			value = u128{value.hi<<16 | value.lo>>48, value.lo << 16}
		}
		value.lo |= add
	}
	for i := 0; i < hi; i++ {
		h, ok := parseHextet(parts[i])
		if !ok {
			return u128{}, false
		}
		shift(16, h)
	}
	shift(16*skipped, 0)
	for i := len(parts) - lo; i < len(parts); i++ {
		h, ok := parseHextet(parts[i])
		if !ok {
			return u128{}, false
		}
		shift(16, h)
	}
	return value, true
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
