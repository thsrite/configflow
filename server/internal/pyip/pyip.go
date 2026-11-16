// Package pyip is the subset of CPython 3.11.17's ipaddress module the
// router code uses: IPv4Address/IPv6Address/ip_address parsing, networks
// (ip_network, IPv4Network, IPv6Network with strict=False), containment,
// is_private/is_global (3.11.17 tables, incl. the 2024 exception lists),
// ordering, supernet and collapse_addresses.
//
// net/netip differs from Python in several observable ways (scope ids with
// arbitrary text, IPv4 netmask/hostmask prefixes, IPv4-mapped text, zone-aware
// equality), so this package ports the Python algorithms instead. The
// parser started as a copy of the reviewed private port in internal/rules
// (ipaddr.go); internal/rules keeps its own copy.
//
// Equality follows Python: Addr and Network are comparable Go values whose
// == (and use as a map key) matches Python's __eq__/__hash__ pair: version,
// integer value, prefix and the IPv6 scope id.
//
// Leaf package: imports only the standard library and jsonx (for repr()).
package pyip

import (
	"math/bits"
	"strconv"
	"strings"

	"configflow/internal/jsonx"
)

// U128 is an unsigned 128-bit integer (IPv4 values live in Lo).
type U128 struct{ Hi, Lo uint64 }

func (a U128) and(b U128) U128 { return U128{a.Hi & b.Hi, a.Lo & b.Lo} }
func (a U128) or(b U128) U128  { return U128{a.Hi | b.Hi, a.Lo | b.Lo} }
func (a U128) not() U128       { return U128{^a.Hi, ^a.Lo} }

// Less reports a < b.
func (a U128) Less(b U128) bool { return a.Hi < b.Hi || (a.Hi == b.Hi && a.Lo < b.Lo) }

// Add1 returns a+1 and whether it overflowed 128 bits.
func (a U128) Add1() (U128, bool) {
	lo := a.Lo + 1
	hi := a.Hi
	if lo == 0 {
		hi++
		if hi == 0 {
			return U128{}, true
		}
	}
	return U128{hi, lo}, false
}

// Sub returns a-b (b <= a).
func (a U128) Sub(b U128) U128 {
	lo, borrow := bits.Sub64(a.Lo, b.Lo, 0)
	hi, _ := bits.Sub64(a.Hi, b.Hi, borrow)
	return U128{hi, lo}
}

// Shl1 returns 1 << n (n < 128).
func Shl1(n int) U128 {
	if n >= 64 {
		return U128{Hi: 1 << uint(n-64)}
	}
	return U128{Lo: 1 << uint(n)}
}

// AddU returns a+b, overflow reported.
func (a U128) AddU(b U128) (U128, bool) {
	lo, carry := bits.Add64(a.Lo, b.Lo, 0)
	hi, carry2 := bits.Add64(a.Hi, b.Hi, carry)
	return U128{hi, lo}, carry2 != 0
}

// BitLen is int.bit_length().
func (a U128) BitLen() int {
	if a.Hi != 0 {
		return 64 + bits.Len64(a.Hi)
	}
	return bits.Len64(a.Lo)
}

// TrailingZeros counts the zero bits at the right (bits for zero).
func (a U128) trailingZeros(width int) int {
	if a.Lo != 0 {
		return bits.TrailingZeros64(a.Lo)
	}
	if a.Hi != 0 {
		return 64 + bits.TrailingZeros64(a.Hi)
	}
	return width
}

// Addr is an IPv4Address or IPv6Address.
type Addr struct {
	ver  int // 4 or 6
	v    U128
	zone string
}

// Version is 4 or 6.
func (a Addr) Version() int { return a.ver }

// Int is int(address).
func (a Addr) Int() U128 { return a.v }

// Zone is the IPv6 scope id ("" when none).
func (a Addr) Zone() string { return a.zone }

// IsValid reports whether a is a parsed address (the zero Addr is not).
func (a Addr) IsValid() bool { return a.ver != 0 }

func (a Addr) maxPrefix() int {
	if a.ver == 4 {
		return 32
	}
	return 128
}

// IPv4FromInt is IPv4Address(int); ok false outside 0..2**32-1.
func IPv4FromInt(value uint64) (Addr, bool) {
	if value > 0xffffffff {
		return Addr{}, false
	}
	return Addr{ver: 4, v: U128{Lo: value}}, true
}

// IPv6FromInt is IPv6Address(int).
func IPv6FromInt(value U128) Addr { return Addr{ver: 6, v: value} }

// ParseAddress is ipaddress.ip_address(text).
func ParseAddress(text string) (Addr, bool) {
	if a, ok := ParseIPv4Address(text); ok {
		return a, true
	}
	return ParseIPv6Address(text)
}

// ParseIPv4Address is IPv4Address(text) for a str.
func ParseIPv4Address(text string) (Addr, bool) {
	if strings.Contains(text, "/") {
		return Addr{}, false
	}
	v, ok := parseIPv4Int(text)
	if !ok {
		return Addr{}, false
	}
	return Addr{ver: 4, v: U128{Lo: uint64(v)}}, true
}

// ParseIPv6Address is IPv6Address(text) for a str (scope id kept verbatim).
func ParseIPv6Address(text string) (Addr, bool) {
	if strings.Contains(text, "/") {
		return Addr{}, false
	}
	addr, zone, found := strings.Cut(text, "%")
	if found && (zone == "" || strings.Contains(zone, "%")) {
		return Addr{}, false
	}
	v, ok := parseIPv6Int(addr)
	if !ok {
		return Addr{}, false
	}
	return Addr{ver: 6, v: v, zone: zone}, true
}

// AddressError is ip_address's ValueError text.
func AddressError(text string) string {
	return jsonx.Repr(text) + " does not appear to be an IPv4 or IPv6 address"
}

// NetworkError is ip_network's ValueError text.
func NetworkError(text string) string {
	return jsonx.Repr(text) + " does not appear to be an IPv4 or IPv6 network"
}

func isASCIIDigits(s string) bool {
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
		if !isASCIIDigits(octet) || len(octet) > 3 || (octet != "0" && octet[0] == '0') {
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
func parseIPv6Int(text string) (U128, bool) {
	if text == "" {
		return U128{}, false
	}
	parts := strings.Split(text, ":")
	if len(parts) < 3 {
		return U128{}, false
	}
	if last := parts[len(parts)-1]; strings.Contains(last, ".") {
		if strings.Contains(last, "/") {
			return U128{}, false
		}
		v4, ok := parseIPv4Int(last)
		if !ok {
			return U128{}, false
		}
		parts = append(parts[:len(parts)-1], strconv.FormatUint(uint64(v4>>16), 16), strconv.FormatUint(uint64(v4&0xffff), 16))
	}
	if len(parts) > 9 {
		return U128{}, false
	}
	skip := -1
	for i := 1; i < len(parts)-1; i++ {
		if parts[i] == "" {
			if skip >= 0 {
				return U128{}, false
			}
			skip = i
		}
	}
	var hi, lo, skipped int
	if skip >= 0 {
		hi, lo = skip, len(parts)-skip-1
		if parts[0] == "" {
			if hi--; hi != 0 {
				return U128{}, false
			}
		}
		if parts[len(parts)-1] == "" {
			if lo--; lo != 0 {
				return U128{}, false
			}
		}
		skipped = 8 - (hi + lo)
		if skipped < 1 {
			return U128{}, false
		}
	} else {
		if len(parts) != 8 || parts[0] == "" || parts[len(parts)-1] == "" {
			return U128{}, false
		}
		hi = len(parts)
	}
	var value U128
	shift := func(n int, add uint64) {
		for ; n > 0; n -= 16 {
			value = U128{value.Hi<<16 | value.Lo>>48, value.Lo << 16}
		}
		value.Lo |= add
	}
	for i := 0; i < hi; i++ {
		h, ok := parseHextet(parts[i])
		if !ok {
			return U128{}, false
		}
		shift(16, h)
	}
	shift(16*skipped, 0)
	for i := len(parts) - lo; i < len(parts); i++ {
		h, ok := parseHextet(parts[i])
		if !ok {
			return U128{}, false
		}
		shift(16, h)
	}
	return value, true
}

// Network is an IPv4Network or IPv6Network.
type Network struct {
	ver    int
	base   U128
	prefix int
	zone   string // scope id of network_address (kept only when no host bit was cleared)
}

func maskFor(ver, prefix int) U128 {
	total := 32
	if ver == 6 {
		total = 128
	}
	host := uint(total - prefix)
	if ver == 4 {
		return U128{Lo: (uint64(1)<<32 - 1) &^ (uint64(1)<<host - 1)}
	}
	switch {
	case host == 0:
		return U128{^uint64(0), ^uint64(0)}
	case host >= 128:
		return U128{}
	case host >= 64:
		return U128{Hi: ^uint64(0) << (host - 64)}
	default:
		return U128{Hi: ^uint64(0), Lo: ^uint64(0) << host}
	}
}

func hostmaskFor(ver, prefix int) U128 {
	m := maskFor(ver, prefix).not()
	if ver == 4 {
		m.Hi = 0
		m.Lo &= 0xffffffff
	}
	return m
}

// Version is 4 or 6.
func (n Network) Version() int { return n.ver }

// Prefix is prefixlen.
func (n Network) Prefix() int { return n.prefix }

// MaxPrefix is max_prefixlen (32 or 128).
func (n Network) MaxPrefix() int {
	if n.ver == 4 {
		return 32
	}
	return 128
}

// IsValid reports whether n is a parsed network.
func (n Network) IsValid() bool { return n.ver != 0 }

// NetworkAddress is network_address (with its scope id).
func (n Network) NetworkAddress() Addr { return Addr{ver: n.ver, v: n.base, zone: n.zone} }

// BroadcastAddress is broadcast_address (never has a scope id).
func (n Network) BroadcastAddress() Addr {
	return Addr{ver: n.ver, v: n.base.or(hostmaskFor(n.ver, n.prefix))}
}

// Netmask is int(netmask).
func (n Network) Netmask() U128 { return maskFor(n.ver, n.prefix) }

// Contains is `address in network`.
func (n Network) Contains(a Addr) bool {
	return a.ver == n.ver && a.v.and(maskFor(n.ver, n.prefix)) == n.base
}

// NetworkFromInt is IPv4Network((value, prefix)) / IPv6Network((value,
// prefix)) with strict=False (host bits cleared). The prefix must be valid.
func NetworkFromInt(ver int, value U128, prefix int) Network {
	return Network{ver: ver, base: value.and(maskFor(ver, prefix)), prefix: prefix}
}

// prefixFromPrefixString is _prefix_from_prefix_string.
func prefixFromPrefixString(text string, max int) (int, bool) {
	if !isASCIIDigits(text) {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	if err != nil {
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

// ParseNetwork is ipaddress.ip_network(text, strict) for a str.
func ParseNetwork(text string, strict bool) (Network, bool) {
	if n, ok := ParseIPv4Network(text, strict); ok {
		return n, true
	}
	return ParseIPv6Network(text, strict)
}

func splitAddrPrefix(text string) (string, string, bool, bool) {
	parts := strings.Split(text, "/")
	if len(parts) > 2 {
		return "", "", false, false
	}
	if len(parts) == 2 {
		return parts[0], parts[1], true, true
	}
	return parts[0], "", false, true
}

// ParseIPv4Network is IPv4Network(text, strict) for a str: /prefix,
// /netmask or /hostmask.
func ParseIPv4Network(text string, strict bool) (Network, bool) {
	addrText, mask, hasMask, ok := splitAddrPrefix(text)
	if !ok {
		return Network{}, false
	}
	v, ok := parseIPv4Int(addrText)
	if !ok {
		return Network{}, false
	}
	prefix := 32
	if hasMask {
		if p, ok := prefixFromPrefixString(mask, 32); ok {
			prefix = p
		} else {
			value, ok := parseIPv4Int(mask)
			if !ok {
				return Network{}, false
			}
			p, ok := prefixFromIPInt(value)
			if !ok {
				p, ok = prefixFromIPInt(value ^ 0xffffffff)
			}
			if !ok {
				return Network{}, false
			}
			prefix = p
		}
	}
	addr := U128{Lo: uint64(v)}
	base := addr.and(maskFor(4, prefix))
	if strict && base != addr {
		return Network{}, false
	}
	return Network{ver: 4, base: base, prefix: prefix}, true
}

// ParseIPv6Network is IPv6Network(text, strict) for a str.
func ParseIPv6Network(text string, strict bool) (Network, bool) {
	addrText, mask, hasMask, ok := splitAddrPrefix(text)
	if !ok {
		return Network{}, false
	}
	addr, ok := ParseIPv6Address(addrText)
	if !ok {
		return Network{}, false
	}
	prefix := 128
	if hasMask {
		p, ok := prefixFromPrefixString(mask, 128)
		if !ok {
			return Network{}, false
		}
		prefix = p
	}
	network := Network{ver: 6, base: addr.v.and(maskFor(6, prefix)), prefix: prefix}
	if network.base == addr.v {
		network.zone = addr.zone
	} else if strict {
		return Network{}, false
	}
	return network, true
}

// SubnetOf is n.subnet_of(outer) for networks of one version (false for
// mixed versions, where Python raises TypeError).
func (n Network) SubnetOf(outer Network) bool {
	if n.ver != outer.ver {
		return false
	}
	// outer.network_address <= n.network_address and outer.broadcast_address >= n.broadcast_address,
	// with Address <= being (< or ==) where == compares scope ids.
	nb, ob := n.base, outer.base
	le := ob.Less(nb) || (ob == nb && outer.zone == n.zone)
	return le && !outer.BroadcastAddress().v.Less(n.BroadcastAddress().v)
}

// Supernet is supernet() (prefixlen_diff=1); a /0 returns itself.
func (n Network) Supernet() Network {
	if n.prefix == 0 {
		return n
	}
	return NetworkFromInt(n.ver, n.base, n.prefix-1)
}

// Less is _BaseNetwork.__lt__ for networks of one version: network_address
// first (a scope id difference makes them unequal without either being
// smaller), then the netmask.
func (n Network) Less(o Network) bool {
	if n.base != o.base || n.zone != o.zone {
		return n.base.Less(o.base)
	}
	if n.prefix != o.prefix {
		return n.prefix < o.prefix
	}
	return false
}

// String is str(network).
func (n Network) String() string {
	return n.NetworkAddress().String() + "/" + strconv.Itoa(n.prefix)
}

// String is str(address): IPv6 compressed like CPython 3.11.17 (IPv4-mapped
// addresses with a dotted tail), scope id appended.
func (a Addr) String() string {
	text := formatIP(a.ver, a.v)
	if a.zone != "" {
		text += "%" + a.zone
	}
	return text
}

func formatIP(ver int, v U128) string {
	if ver == 4 {
		x := uint32(v.Lo)
		return strconv.Itoa(int(x>>24)) + "." + strconv.Itoa(int(x>>16&255)) + "." +
			strconv.Itoa(int(x>>8&255)) + "." + strconv.Itoa(int(x&255))
	}
	if v.Hi == 0 && v.Lo>>32 == 0xffff {
		return "::ffff:" + formatIP(4, U128{Lo: v.Lo & 0xffffffff})
	}
	var groups [8]uint64
	for i := 0; i < 4; i++ {
		groups[i] = v.Hi >> (48 - 16*uint(i)) & 0xffff
		groups[4+i] = v.Lo >> (48 - 16*uint(i)) & 0xffff
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
