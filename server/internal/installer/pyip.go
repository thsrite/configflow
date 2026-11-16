package installer

import (
	"errors"
	"strings"
)

var errAddress = errors.New("invalid IPv4 address")

// parseIPv4 is ipaddress.IPv4Address(text) for a str: four dot-separated
// ASCII decimal octets of at most three digits, no leading zero, each <= 255.
func parseIPv4(text string) (uint32, error) {
	if text == "" || strings.Contains(text, "/") {
		return 0, errAddress
	}
	octets := strings.Split(text, ".")
	if len(octets) != 4 {
		return 0, errAddress
	}
	var value uint32
	for _, octet := range octets {
		if octet == "" || len(octet) > 3 || !asciiDigits(octet) {
			return 0, errAddress
		}
		n := 0
		for i := 0; i < len(octet); i++ {
			n = n*10 + int(octet[i]-'0')
		}
		if n > 255 || (octet[0] == '0' && len(octet) > 1) {
			return 0, errAddress
		}
		value = value<<8 | uint32(n)
	}
	return value, nil
}

func ipv4Text(v uint32) string {
	parts := make([]string, 4)
	for i := 0; i < 4; i++ {
		parts[3-i] = itoa(int(v >> (8 * i) & 0xff))
	}
	return strings.Join(parts, ".")
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

// prefixFromInt is _prefix_from_ip_int: a netmask of leading ones.
func prefixFromInt(v uint32) (int, bool) {
	trailing := 0
	for trailing < 32 && v&(1<<trailing) == 0 {
		trailing++
	}
	prefix := 32 - trailing
	var leading uint32
	if trailing < 32 {
		leading = v >> trailing
	}
	var allOnes uint32
	if prefix == 32 {
		allOnes = ^uint32(0)
	} else {
		allOnes = 1<<prefix - 1
	}
	return prefix, leading == allOnes
}

// parseIPv4Network is ipaddress.IPv4Network(text, strict=True) for a str:
// address[/prefix], the prefix as a decimal length, a netmask or a hostmask.
func parseIPv4Network(text string) (ipv4Network, error) {
	parts := strings.Split(text, "/")
	if len(parts) > 2 {
		return ipv4Network{}, errAddress
	}
	address, err := parseIPv4(parts[0])
	if err != nil {
		return ipv4Network{}, err
	}
	prefix := 32
	if len(parts) == 2 {
		p := parts[1]
		if p != "" && asciiDigits(p) {
			n, err := pyInt(p)
			if err != nil || !inRange(n, 0, 32) {
				return ipv4Network{}, errAddress
			}
			prefix = int(n.Int64())
		} else {
			mask, err := parseIPv4(p)
			if err != nil {
				return ipv4Network{}, errAddress
			}
			var ok bool
			if prefix, ok = prefixFromInt(mask); !ok {
				if prefix, ok = prefixFromInt(mask ^ ^uint32(0)); !ok {
					return ipv4Network{}, errAddress
				}
			}
		}
	}
	network := ipv4Network{address: address, prefix: prefix}
	if address&^network.mask() != 0 {
		return ipv4Network{}, errAddress // strict: host bits set
	}
	return network, nil
}
