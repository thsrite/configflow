package health

import (
	"net/netip"
	"strconv"
	"strings"
)

// parseIPv4 is ipaddress.IPv4Address(text) for a str (Python 3.11: four
// decimal ASCII octets, no leading zeros, each <= 255).
func parseIPv4(text string) (netip.Addr, bool) {
	if strings.ContainsAny(text, ":%") {
		return netip.Addr{}, false
	}
	parts := strings.Split(text, ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}
	var octets [4]byte
	for i, part := range parts {
		if part == "" || len(part) > 3 {
			return netip.Addr{}, false
		}
		for j := 0; j < len(part); j++ {
			if part[j] < '0' || part[j] > '9' {
				return netip.Addr{}, false
			}
		}
		if len(part) > 1 && part[0] == '0' {
			return netip.Addr{}, false
		}
		value, _ := strconv.Atoi(part)
		if value > 255 {
			return netip.Addr{}, false
		}
		octets[i] = byte(value)
	}
	return netip.AddrFrom4(octets), true
}

// ipAddress is ipaddress.ip_address(text) for a str: the address, its
// scope id (IPv6 only) and whether it parsed.
func ipAddress(text string) (addr netip.Addr, scope string, ok bool) {
	if v4, ok := parseIPv4(text); ok {
		return v4, "", true
	}
	address, zone, scoped := strings.Cut(text, "%")
	if scoped && (zone == "" || strings.Contains(zone, "%")) {
		return netip.Addr{}, "", false
	}
	if !isASCII(address) || strings.Count(address, ":") < 2 {
		return netip.Addr{}, "", false
	}
	parsed, err := netip.ParseAddr(address)
	if err != nil || !parsed.Is6() || parsed.Zone() != "" {
		return netip.Addr{}, "", false
	}
	return parsed, zone, true
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ipString is str(ip_address(...)) of Python 3.11: dotted IPv4, IPv6 as
// lower-case hextets with the longest run (first of equals, at least two) of
// zero hextets compressed, IPv4-mapped addresses as ::ffff:a.b.c.d, plus
// "%scope".
func ipString(addr netip.Addr, scope string) string {
	if addr.Is4() {
		return addr.String()
	}
	raw := addr.As16()
	if addr.Is4In6() {
		// CPython 3.11.17 prints IPv4-mapped addresses in dotted form.
		out := "::ffff:" + netip.AddrFrom4([4]byte{raw[12], raw[13], raw[14], raw[15]}).String()
		if scope != "" {
			out += "%" + scope
		}
		return out
	}
	hextets := make([]string, 8)
	for i := range hextets {
		hextets[i] = strconv.FormatUint(uint64(raw[2*i])<<8|uint64(raw[2*i+1]), 16)
	}
	bestStart, bestLen, start := -1, 0, -1
	for i, h := range hextets {
		if h == "0" {
			if start < 0 {
				start = i
			}
			if length := i + 1 - start; length > bestLen {
				bestStart, bestLen = start, length
			}
		} else {
			start = -1
		}
	}
	out := strings.Join(hextets, ":")
	if bestLen > 1 {
		left := strings.Join(hextets[:bestStart], ":")
		right := strings.Join(hextets[bestStart+bestLen:], ":")
		out = left + "::" + right
	}
	if scope != "" {
		out += "%" + scope
	}
	return out
}
