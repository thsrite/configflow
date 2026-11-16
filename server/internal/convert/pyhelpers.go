package convert

// Small Python built-ins the converters rely on. String rules (lower, strip,
// isspace, decimal digits) come from internal/pystr.

import (
	"fmt"
	"net/netip"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

// pyStripChars is str.strip(chars).
func pyStripChars(s, chars string) string {
	return strings.Trim(s, chars)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// pyIsDigit is str.isdigit(): decimal digits plus other digits (superscripts,
// circled digits) whose numeric type is Digit.
func pyIsDigit(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if pystr.Decimal(r) < 0 && !isDigitProperty(r) {
			return false
		}
	}
	return true
}

// isDigitProperty reports the characters with Numeric_Type=Digit (all of
// category No in Unicode 14.0.0).
func isDigitProperty(r rune) bool {
	switch {
	case r == 0xB2 || r == 0xB3 || r == 0xB9:
		return true
	case r >= 0x1369 && r <= 0x1371:
		return true
	case r == 0x19DA:
		return true
	case r == 0x2070 || (r >= 0x2074 && r <= 0x2079):
		return true
	case r >= 0x2080 && r <= 0x2089:
		return true
	case r >= 0x2460 && r <= 0x2468:
		return true
	case r >= 0x2474 && r <= 0x247C:
		return true
	case r >= 0x2488 && r <= 0x2490:
		return true
	case r == 0x24EA || (r >= 0x24F5 && r <= 0x24FD) || r == 0x24FF:
		return true
	case r >= 0x2776 && r <= 0x277E, r >= 0x2780 && r <= 0x2788, r >= 0x278A && r <= 0x2792:
		return true
	case r >= 0x10A40 && r <= 0x10A43:
		return true
	case r >= 0x10E60 && r <= 0x10E68:
		return true
	case r >= 0x11052 && r <= 0x1105A:
		return true
	case r >= 0x1F100 && r <= 0x1F10A:
		return true
	}
	return false
}

// pyTypeName is type(v).__name__ for jsonx values.
func pyTypeName(v any) string {
	switch value := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if value.IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// validIPv6 reports whether ipaddress.IPv6Address(text) accepts text
// (scope ids included, as Python 3.9+ does).
func validIPv6(text string) bool {
	_, ok := parseIPv6(text)
	return ok
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
