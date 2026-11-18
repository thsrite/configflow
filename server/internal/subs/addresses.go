package subs

import (
	"net/netip"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// parseHostIP validates a host address, preserving its optional IPv6 zone.
// A second percent sign cannot name a zone in a generated nameserver URL.
func parseHostIP(text string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(text)
	return addr, err == nil && !strings.Contains(addr.Zone(), "%")
}

func usableAddress(text string) string {
	addr, ok := parseHostIP(strings.TrimSpace(text))
	if !ok {
		return ""
	}
	flags := addr.WithZone("").Unmap()
	if flags.IsUnspecified() || flags.IsMulticast() {
		return ""
	}
	return addr.String()
}

func hostPatternText(text string) string {
	return strings.TrimRight(cases.Lower(language.Und).String(strings.TrimSpace(text)), ".")
}
