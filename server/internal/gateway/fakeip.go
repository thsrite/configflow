package gateway

import (
	"net/netip"

	"configflow/internal/netipx"

	"gopkg.in/yaml.v3"
)

// DefaultFakeIPRange is Mihomo's fake-ip range when dns.fake-ip-range is not
// set.
const DefaultFakeIPRange = "198.18.0.1/16"

// FakeIPNetwork is the fake-ip network of a Mihomo document (a mapping):
// dns.fake-ip-range, DefaultFakeIPRange when absent. Errors are *Error: dns
// is not a mapping, or the range is not usable (see ParseFakeIPRange).
func FakeIPNetwork(doc *yaml.Node) (netip.Prefix, error) {
	dns := get(doc, "dns")
	if !truthy(dns) {
		return ParseFakeIPRange(DefaultFakeIPRange)
	}
	if !isMap(dns) {
		return netip.Prefix{}, gatewayError(msgDNSNotObject)
	}
	value := get(dns, "fake-ip-range")
	if value == nil {
		return ParseFakeIPRange(DefaultFakeIPRange)
	}
	text, ok := stringValue(value)
	if !ok {
		return netip.Prefix{}, gatewayError(msgFakeRange)
	}
	return ParseFakeIPRange(text)
}

// ParseFakeIPRange checks a fake-ip range: an IPv4 network (host bits are
// cleared) of /8 to /30 that is not loopback, multicast, link-local or
// 0.0.0.0. The error is *Error.
func ParseFakeIPRange(text string) (netip.Prefix, error) {
	network, ok := netipx.ParseNetwork4(text, false)
	if !ok || network.Bits() < 8 || network.Bits() > 30 {
		return netip.Prefix{}, gatewayError(msgFakeRange)
	}
	first, last := network.Addr(), netipx.Broadcast(network)
	if (first.IsLoopback() && last.IsLoopback()) || (first.IsMulticast() && last.IsMulticast()) ||
		(first.IsLinkLocalUnicast() && last.IsLinkLocalUnicast()) || first.IsUnspecified() {
		return netip.Prefix{}, gatewayError(msgFakeRange)
	}
	return network, nil
}
