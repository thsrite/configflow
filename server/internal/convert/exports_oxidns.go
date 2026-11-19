package convert

import (
	"net/netip"
	"strings"
)

// ParseIPAddress parses a host address and discards an optional IPv6 zone.
func ParseIPAddress(text string) (netip.Addr, bool) {
	address, err := netip.ParseAddr(text)
	if err != nil || strings.Contains(address.Zone(), "%") {
		return netip.Addr{}, false
	}
	return address.WithZone(""), true
}
