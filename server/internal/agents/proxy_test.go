package agents

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
)

func withEnviron(t *testing.T, env [][2]string) {
	t.Helper()
	for _, key := range []string{"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "NO_PROXY", "no_proxy", "REQUEST_METHOD"} {
		t.Setenv(key, "")
	}
	for _, pair := range env {
		t.Setenv(pair[0], pair[1])
	}
}

// TestLoopbackAgentBypassesEnvironmentProxy asserts the native N2 decision
// of 2026-11-17: loopback calls always go direct.
func TestLoopbackAgentBypassesEnvironmentProxy(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	proxy := listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, "proxy "+r.Method+" "+r.RequestURI+" "+r.Header.Get("Proxy-Authorization"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"via":"proxy"}`))
	}))
	agent := listenHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, "agent "+r.Method+" "+r.RequestURI)
		mu.Unlock()
		w.Write([]byte(`{"via":"agent"}`))
	}))
	target := "http://" + agent + "/api/status"
	for _, c := range []struct {
		name    string
		env     [][2]string
		noProxy bool
		want    string
	}{
		{"proxied", [][2]string{{"HTTP_PROXY", "http://u:p@" + proxy}}, false, "agent GET /api/status"},
		{"lowercase", [][2]string{{"http_proxy", proxy}}, false, "agent GET /api/status"},
		{"no_proxy ip", [][2]string{{"HTTP_PROXY", proxy}, {"NO_PROXY", "127.0.0.1"}}, false, "agent GET /api/status"},
		{"no_proxy cidr", [][2]string{{"HTTP_PROXY", proxy}, {"no_proxy", "127.0.0.0/8"}}, false, "agent GET /api/status"},
		{"no_proxy other", [][2]string{{"HTTP_PROXY", proxy}, {"NO_PROXY", "localhost,10.0.0.0/8"}}, false, "agent GET /api/status"},
		{"trust_env off", [][2]string{{"HTTP_PROXY", proxy}}, true, "agent GET /api/status"},
	} {
		withEnviron(t, c.env)
		mu.Lock()
		seen = nil
		mu.Unlock()
		response, err := HTTPClient{}.Do(context.Background(), Request{Method: "GET", URL: target, Timeout: Seconds(5), NoProxy: c.noProxy})
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		mu.Lock()
		got := strings.Join(seen, "|")
		mu.Unlock()
		if got != c.want || response.StatusCode != 200 {
			t.Errorf("%s: %q (status %d), want %q", c.name, got, response.StatusCode, c.want)
		}
	}
	withEnviron(t, [][2]string{{"HTTP_PROXY", "socks5://" + proxy}})
	if _, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: target, Timeout: Seconds(5)}); err != nil {
		t.Errorf("loopback must bypass SOCKS environment proxy: %v", err)
	}
}

func listenHTTP(t *testing.T, handler http.Handler) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(listener)
	t.Cleanup(func() { server.Close() })
	return listener.Addr().String()
}
