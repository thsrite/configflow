package health

import (
	"net/netip"
	"strings"
)

func parseIPv4(text string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(text)
	return addr, err == nil && addr.Is4() && addr.Zone() == ""
}

func ipAddress(text string) (addr netip.Addr, scope string, ok bool) {
	addr, err := netip.ParseAddr(text)
	if err != nil || strings.Contains(addr.Zone(), "%") {
		return netip.Addr{}, "", false
	}
	return addr.WithZone(""), addr.Zone(), true
}

func ipString(addr netip.Addr, scope string) string { return addr.WithZone(scope).String() }
