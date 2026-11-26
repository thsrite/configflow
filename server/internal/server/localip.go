package server

import "net"

// localIP is the address this machine would use to reach the outside, found by
// "connecting" a UDP socket to a public address (nothing is sent), or
// 127.0.0.1 when that fails.
func localIP(dial func(network, address string) (net.Conn, error)) string {
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
