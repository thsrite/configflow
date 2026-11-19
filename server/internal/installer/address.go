package installer

import (
	"configflow/internal/netipx"
	"encoding/binary"
	"errors"
	"net/netip"
)

var errAddress = errors.New("invalid IPv4 address")

// parseIPv4 uses the shared native IPv4 parser.
func parseIPv4(text string) (uint32, error) {
	address, ok := netipx.ParseAddr4(text)
	if !ok {
		return 0, errAddress
	}
	b := address.As4()
	return binary.BigEndian.Uint32(b[:]), nil
}
func ipv4Text(value uint32) string {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], value)
	return netip.AddrFrom4(b).String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func inNet(v, network uint32, prefix int) bool {
	if prefix == 0 {
		return true
	}
	mask := ^uint32(0) << (32 - prefix)
	return v&mask == network
}

func isLoopback(v uint32) bool    { return inNet(v, 127<<24, 8) }
func isMulticast(v uint32) bool   { return inNet(v, 224<<24, 4) }
func isLinkLocal(v uint32) bool   { return inNet(v, 169<<24|254<<16, 16) }
func isUnspecified(v uint32) bool { return v == 0 }
func isReserved(v uint32) bool    { return inNet(v, 240<<24, 4) }

// ipv4Network is a parsed ipaddress.IPv4Network.
type ipv4Network struct {
	address uint32
	prefix  int
}

func (n ipv4Network) mask() uint32 {
	if n.prefix == 0 {
		return 0
	}
	return ^uint32(0) << (32 - n.prefix)
}
func (n ipv4Network) broadcast() uint32      { return n.address | ^n.mask() }
func (n ipv4Network) contains(v uint32) bool { return v&n.mask() == n.address }
func (n ipv4Network) String() string         { return ipv4Text(n.address) + "/" + itoa(n.prefix) }

// parseIPv4Network shares strict CIDR/netmask/hostmask parsing with rules.
func parseIPv4Network(text string) (ipv4Network, error) {
	prefix, ok := netipx.ParseNetwork4(text, true)
	if !ok {
		return ipv4Network{}, errAddress
	}
	b := prefix.Addr().As4()
	return ipv4Network{address: binary.BigEndian.Uint32(b[:]), prefix: prefix.Bits()}, nil
}
