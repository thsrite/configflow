package convert

import "net/netip"

// Exported access to Python-semantics helpers of this package for the OxiDNS
// cluster (internal/oxidns, C05/C22), so it does not keep private copies.

// ParseIPAddress is ipaddress.ip_address(text) for a string (Python 3.11
// rules: strict dotted IPv4 without leading zeros, IPv6 with an optional
// embedded IPv4 tail and an optional %scope); ok=false is the ValueError.
// The scope is not part of the returned address.
func ParseIPAddress(text string) (netip.Addr, bool) {
	if addr, ok := parseIPv4(text); ok {
		return addr, true
	}
	return parseIPv6(text)
}
