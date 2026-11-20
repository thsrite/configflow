package convert

// The parts of Python's ipaddress module (3.11) that EasyTier settings and
// hosts parsing use, with its acceptance rules.

import (
	"fmt"
	"math/bits"
	"net/netip"
	"strconv"
	"strings"
)

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// parseIPv6 parses an IPv6 address the way ipaddress.IPv6Address does; the
// zone ("%eth0") is accepted and dropped from the returned address.
func parseIPv6(text string) (netip.Addr, bool) {
	addr := text
	if i := strings.IndexByte(text, '%'); i >= 0 {
		zone := text[i+1:]
		if zone == "" || strings.Contains(zone, "%") {
			return netip.Addr{}, false
		}
		addr = text[:i]
	}
	if !strings.Contains(addr, ":") {
		return netip.Addr{}, false
	}
	// Embedded IPv4 tail: Python parses it with the strict IPv4 rules.
	if i := strings.LastIndexByte(addr, ':'); strings.Contains(addr[i+1:], ".") {
		v4, ok := parseIPv4(addr[i+1:])
		if !ok {
			return netip.Addr{}, false
		}
		b := v4.As4()
		addr = addr[:i+1] + fmt.Sprintf("%x:%x", int(b[0])<<8|int(b[1]), int(b[2])<<8|int(b[3]))
	}
	for _, part := range strings.Split(addr, ":") {
		if len(part) > 4 {
			return netip.Addr{}, false
		}
		for i := 0; i < len(part); i++ {
			c := part[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return netip.Addr{}, false
			}
		}
	}
	parsed, err := netip.ParseAddr(addr)
	if err != nil || !parsed.Is6() || parsed.Zone() != "" {
		return netip.Addr{}, false
	}
	return parsed, true
}

// parseIPv4 parses dotted-quad text the way ipaddress.IPv4Address does:
// exactly four ASCII decimal octets, no leading zeros, each at most 255.
func parseIPv4(text string) (netip.Addr, bool) {
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}
	var b [4]byte
	for i, part := range parts {
		if part == "" || len(part) > 3 {
			return netip.Addr{}, false
		}
		value := 0
		for j := 0; j < len(part); j++ {
			c := part[j]
			if c < '0' || c > '9' {
				return netip.Addr{}, false
			}
			value = value*10 + int(c-'0')
		}
		if value > 255 || (len(part) > 1 && part[0] == '0') {
			return netip.Addr{}, false
		}
		b[i] = byte(value)
	}
	return netip.AddrFrom4(b), true
}

// prefixFromString is _prefix_from_prefix_string: ASCII digits only, in
// 0..max.
func prefixFromString(text string, max int) (int, bool) {
	if text == "" || !isASCII(text) {
		return 0, false
	}
	for i := 0; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return 0, false
		}
	}
	trimmed := strings.TrimLeft(text, "0")
	if len(trimmed) > 3 {
		return 0, false
	}
	value, _ := strconv.Atoi("0" + trimmed)
	if value > max {
		return 0, false
	}
	return value, true
}

// prefixFromIPInt is _prefix_from_ip_int for 32 bits.
func prefixFromIPInt(value uint32) (int, bool) {
	trailing := bits.TrailingZeros32(value)
	prefix := 32 - trailing
	if trailing == 32 {
		return 0, true
	}
	leading := value >> trailing
	if uint64(leading) != (uint64(1)<<prefix)-1 {
		return 0, false
	}
	return prefix, true
}

// v4Netmask is IPv4 _make_netmask(text): a prefix length, a netmask or a
// hostmask in dotted-quad form.
func v4Netmask(text string) (int, bool) {
	if prefix, ok := prefixFromString(text, 32); ok {
		return prefix, true
	}
	addr, ok := parseIPv4(text)
	if !ok {
		return 0, false
	}
	b := addr.As4()
	value := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	if prefix, ok := prefixFromIPInt(value); ok {
		return prefix, true
	}
	return prefixFromIPInt(^value)
}

// ipNetwork is ipaddress.ip_network(text, strict=False): the canonical text
// and IP version, or ok=false for the ValueError.
func ipNetwork(text string) (canonical string, version int, ok bool) {
	parts := strings.Split(text, "/")
	if len(parts) > 2 {
		return "", 0, false
	}
	if addr, valid := parseIPv4(parts[0]); valid {
		prefix := 32
		if len(parts) == 2 {
			if prefix, valid = v4Netmask(parts[1]); !valid {
				return "", 0, false
			}
		}
		network, _ := addr.Prefix(prefix)
		return network.String(), 4, true
	}
	if addr, valid := parseIPv6(parts[0]); valid {
		prefix := 128
		if len(parts) == 2 {
			if prefix, valid = prefixFromString(parts[1], 128); !valid {
				return "", 0, false
			}
		}
		network, _ := addr.Prefix(prefix)
		return network.String(), 6, true
	}
	return "", 0, false
}

// ipInterfaceVersion is ipaddress.ip_interface(text).version (0 for the
// ValueError).
func ipInterfaceVersion(text string) int {
	if _, version, ok := ipNetwork(text); ok {
		return version
	}
	return 0
}

// ipAddressVersion is ipaddress.ip_address(text).version (0 for the
// ValueError).
func ipAddressVersion(text string) int {
	if _, ok := parseIPv4(text); ok {
		return 4
	}
	if _, ok := parseIPv6(text); ok {
		return 6
	}
	return 0
}
