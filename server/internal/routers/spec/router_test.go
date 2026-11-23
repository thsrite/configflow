package spec

import "testing"

// Accepted difference (documented in PROGRESS): Go rejects management
// addresses urllib's urlparse lets through but no HTTP client can connect to
// (non-numeric port, spaces, bad escapes); everything usable stays accepted.
func TestR01ReviewEndpoints(t *testing.T) {
	for _, e := range []string{"http://192.168.88.1", "https://router.lan:8443/", "http://[fd00::1]:80", "HTTP://H", "http://h/;x",
		"http://user:pw@h", "http://h?q", "http://h#f", "https://路由器.lan"} {
		if !validEndpoint(e) {
			t.Errorf("%q rejected", e)
		}
	}
	for _, e := range []string{"http://a:abc", "http://a b", "http://h:-1", "http://[::1", "ftp://h", "http://h/a", "http:///x"} {
		if validEndpoint(e) {
			t.Errorf("%q accepted", e)
		}
	}
}
