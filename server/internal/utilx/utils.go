// Package utilx is the port of backend/common/utils.py.
package utilx

import (
	"crypto/rand"
	"math/big"
	"net"
	"strings"
)

// Dial is net.Dial, replaceable in tests.
type Dial func(network, address string) (net.Conn, error)

// GetLocalIP is get_local_ip(): the address the machine would use to reach the
// outside, found by "connecting" a UDP socket to 8.8.8.8:80 (nothing is sent),
// or 127.0.0.1 when that fails.
func GetLocalIP() string { return LocalIP(net.Dial) }

// LocalIP is GetLocalIP with an injectable dialer.
func LocalIP(dial Dial) string {
	conn, err := dial("udp4", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return "127.0.0.1"
	}
	return addr.IP.String()
}

const tokenAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// GenerateRandomToken is generate_random_token(length): letters and digits,
// each drawn uniformly from the system CSPRNG (secrets.choice). Python's
// default length is 32.
func GenerateRandomToken(length int) string {
	var b strings.Builder
	max := big.NewInt(int64(len(tokenAlphabet)))
	for i := 0; i < length; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic("utilx: no randomness: " + err.Error())
		}
		b.WriteByte(tokenAlphabet[n.Int64()])
	}
	return b.String()
}

// StrToBool is str_to_bool(s): str(s).lower() == "true". A bool true is true
// too (str(True) is "True"); None, numbers and everything else are false.
func StrToBool(v any) bool {
	switch v := v.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}
