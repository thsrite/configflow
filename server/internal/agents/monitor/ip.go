package monitor

import (
	"net/netip"
	"strings"
)

// parseIP canonicalizes a registered IP endpoint with the native parser.
func parseIP(text string) (canonical string, v6 bool, ok bool) {
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

// isPlainIP validates a client IP without an interface zone.
func isPlainIP(text string) bool {
	if strings.Contains(text, "%") {
		return false
	}
	_, _, ok := parseIP(text)
	return ok
}
