package convert

// The parts of Python's ipaddress module (3.11) that EasyTier settings and
// hosts parsing use, with its acceptance rules.

import (
	"math/bits"
	"strconv"
	"strings"
)

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
