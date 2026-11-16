// Package clientrange normalizes client IPv4/IPv6 addresses and CIDR ranges.
package clientrange

import (
	"net/netip"
	"strconv"
	"strings"
)

// Normalize canonicalizes a client address or range. IPv4-mapped peers become
// IPv4, host bits are cleared, and a full-length prefix is returned as a bare
// address. Multicast networks and the unspecified address are rejected.
func Normalize(raw string) (string, bool) {
	addressText, prefixText, hasPrefix := strings.Cut(strings.TrimSpace(raw), "/")
	if addressText == "" || strings.Trim(addressText, "0123456789abcdefABCDEF:.") != "" || strings.Contains(prefixText, "/") {
		return "", false
	}
	address, err := netip.ParseAddr(addressText)
	if err != nil {
		return "", false
	}
	prefix := address.BitLen()
	if hasPrefix {
		if len(prefixText) > 3 || !asciiDigits(prefixText) {
			return "", false
		}
		prefix, _ = strconv.Atoi(prefixText)
	}
	if prefix > address.BitLen() {
		return "", false
	}
	if address.Is4In6() {
		if prefix < 96 {
			return "", false
		}
		address, prefix = address.Unmap(), prefix-96
	}
	network := netip.PrefixFrom(address, prefix).Masked()
	if network.Addr().IsMulticast() && lastAddress(network).IsMulticast() {
		return "", false
	}
	if prefix == address.BitLen() {
		if address.IsUnspecified() {
			return "", false
		}
		return address.String(), true
	}
	return network.String(), true
}

func asciiDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func lastAddress(network netip.Prefix) netip.Addr {
	bytes := network.Addr().AsSlice()
	for bit := network.Bits(); bit < len(bytes)*8; bit++ {
		bytes[bit/8] |= 0x80 >> (bit % 8)
	}
	last, _ := netip.AddrFromSlice(bytes)
	return last
}
