package monitor

import (
	"net/netip"
	"strings"
)

// pyIPAddress is str(ipaddress.ip_address(text)) for a str: ok false for
// the ValueError. IPv6 scope ids are kept ("fe80::1%eth0"); Python refuses
// an empty scope or a second "%", which netip would accept.
func pyIPAddress(text string) (canonical string, v6 bool, ok bool) {
	if strings.Count(text, "%") > 1 {
		return "", false, false
	}
	addr, err := netip.ParseAddr(text)
	if err != nil {
		return "", false, false
	}
	if addr.Is4() {
		return addr.String(), false, true
	}
	return addr.String(), true, true
}

// isPlainIP is `'%' not in text and ipaddress.ip_address(text)` succeeding.
func isPlainIP(text string) bool {
	if strings.Contains(text, "%") {
		return false
	}
	_, _, ok := pyIPAddress(text)
	return ok
}
