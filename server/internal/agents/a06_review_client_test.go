package agents

// A06 review: scoped IPv6 Agent hosts. oxidns_monitor._agent_origin accepts
// "fe80::1%eth0" (ipaddress keeps the scope), and requests reaches such an
// Agent: requote_uri turns the invalid escape "%et" into "%25et" (and every
// other "%" of the URL with it), urllib3 hands "fe80::1%eth0" to glibc's
// getaddrinfo, which accepts an interface name for link-local addresses and
// a decimal zone for any address. Go's URL parser refused "%eth0" outright.

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestRequoteURIFallbackAndZonesLikeRequests(t *testing.T) {
	cases := map[string]string{
		// Outputs of requests 2.31 Request.prepare() / requote_uri on CPython 3.11.
		"http://[fe80::1%eth0]:8082/api/oxidns/overview": "http://[fe80::1%25eth0]:8082/api/oxidns/overview",
		"http://[fe80::1%25eth0]:8082/x":                 "http://[fe80::1%25eth0]:8082/x",
		"http://[fe80::1%lo]:1/q?qname=%E6%B8%AC+a%3Ab":  "http://[fe80::1%25lo]:1/q?qname=%25E6%25B8%25AC+a%253Ab",
		"http://[::1%1]:1/q?qname=%E6%B8%AC":             "http://[::1%251]:1/q?qname=%E6%B8%AC",
		"http://[::1%01]:1/x":                            "http://[::1%2501]:1/x",
		"http://[::1%4294967295]:1/x":                    "http://[::1B94967295]:1/x",
		"http://127.0.0.1:1/a%zz?b=%41":                  "http://127.0.0.1:1/a%25zz?b=%2541",
		"http://[fe80::1%dc0]:1/x":                       "http://[fe80::1%25dc0]:1/x",
	}
	for in, want := range cases {
		// The second form is what Go's parser needs for the same zone
		// ("%25" + zone); requests' text has the zone after a bare "%".
		if got := encodeZone(unquoteUnreserved(in)); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestGlibcScopeRules(t *testing.T) {
	cases := map[string]bool{
		"::1%1": true, "::1%0": true, "::1%01": true, "::1%4294967295": true, "::1%4294967296": false,
		"::1%lo": false, "fe80::1%lo": true, "fe80::1%nonexistent0": false, "fe80::1%1x": false, "fe80::1%": false,
		"ff02::1%lo": true, "ff01::1%lo": true, "2001:db8::1%lo": false, "1.2.3.4%1": false,
	}
	for host, want := range cases {
		if got := glibcScopeOK(host); got != want {
			t.Errorf("%q: %v, want %v", host, got, want)
		}
	}
}

func TestScopedIPv6AgentIsReachedLikeRequests(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("no IPv6 loopback: %v", err)
	}
	var seen []string
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.RequestURI())
		w.Write([]byte(`{"ok": true}`))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()
	port := strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
	client := HTTPClient{}
	// A decimal zone: no requote fallback, the query keeps its escapes.
	response, err := client.Do(context.Background(), Request{Method: "GET", Timeout: Pair(3, 12), NoProxy: true,
		URL: "http://[::1%1]:" + port + "/api/oxidns/queries?qname=%E6%B8%AC+a%3Ab"})
	if err != nil || response.StatusCode != 200 || len(seen) != 1 || seen[0] != "/api/oxidns/queries?qname=%E6%B8%AC+a%3Ab" {
		t.Fatalf("%v %v %v", response, err, seen)
	}
	// An interface name on a global address fails getaddrinfo in glibc.
	for _, host := range []string{"::1%lo", "fe80::1%nonexistent0"} {
		if _, err := client.Do(context.Background(), Request{Method: "GET", Timeout: Pair(3, 12), NoProxy: true,
			URL: "http://[" + host + "]:" + port + "/x"}); err == nil || !IsTransportError(err) {
			t.Fatalf("%s: %v", host, err)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("%v", seen)
	}
}
