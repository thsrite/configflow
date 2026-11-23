package server

import (
	"errors"
	"net"
	"testing"
)

type fakeConn struct {
	net.Conn
	addr net.Addr
}

func (c fakeConn) LocalAddr() net.Addr { return c.addr }
func (c fakeConn) Close() error        { return nil }

func TestLocalIP(t *testing.T) {
	var seen string
	ok := func(network, address string) (net.Conn, error) {
		seen = network + " " + address
		return fakeConn{addr: &net.UDPAddr{IP: net.ParseIP("192.0.2.10"), Port: 5555}}, nil
	}
	if got := localIP(ok); got != "192.0.2.10" || seen != "udp4 8.8.8.8:80" {
		t.Errorf("got %q after %q", got, seen)
	}
	failing := func(string, string) (net.Conn, error) { return nil, errors.New("unreachable") }
	if got := localIP(failing); got != "127.0.0.1" {
		t.Errorf("got %q", got)
	}
}
