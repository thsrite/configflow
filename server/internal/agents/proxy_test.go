package agents

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
)

type proxyReferenceCase struct {
	Env    [][2]string `json:"env"`
	URL    string      `json:"url"`
	Result struct {
		Proxy *string `json:"proxy"`
		Auth  *string `json:"auth"`
		Error string  `json:"error"`
	} `json:"result"`
}

func withEnviron(t *testing.T, env [][2]string) {
	t.Helper()
	previous := environ
	entries := make([]string, len(env))
	for i, pair := range env {
		entries[i] = pair[0] + "=" + pair[1]
	}
	environ = func() []string { return entries }
	t.Cleanup(func() { environ = previous })
}

func proxyErrorClass(err error) string {
	switch {
	case errors.Is(err, errInvalidURL):
		return "InvalidURL"
	case errors.Is(err, errInvalidProxyURL):
		return "InvalidProxyURL"
	case errors.Is(err, errInvalidSchema):
		return "InvalidSchema"
	}
	class, _, _ := strings.Cut(err.Error(), ":")
	return class
}

// TestReferenceEnvironmentProxies compares the proxy choice with real
// requests 2.31 / urllib3 2.8 (testdata/proxy_reference.json): lowercase
// variables winning, REQUEST_METHOD, ALL_PROXY and odd variable names,
// NO_PROXY suffix/CIDR/host:port/'*' forms through both of requests' and
// urllib's checks, no automatic loopback bypass, proxy URL forms, ports and
// Proxy-Authorization.
func TestReferenceEnvironmentProxies(t *testing.T) {
	data, err := os.ReadFile("testdata/proxy_reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []proxyReferenceCase
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 900 {
		t.Fatalf("only %d cases", len(cases))
	}
	for _, c := range cases {
		withEnviron(t, c.Env)
		proxy, err := environProxy(c.URL)
		var got, want string
		switch {
		case err != nil:
			got = "error " + proxyErrorClass(err)
		case proxy == nil:
			got = "direct"
		default:
			got = proxy.String() + " auth=" + proxy.auth
		}
		switch {
		case c.Result.Error != "":
			want = "error " + c.Result.Error
		case c.Result.Proxy == nil:
			want = "direct"
		default:
			auth := ""
			if c.Result.Auth != nil {
				auth = *c.Result.Auth
			}
			want = *c.Result.Proxy + " auth=" + auth
		}
		if got != want {
			t.Errorf("env %q url %q: got %s, want %s", c.Env, c.URL, got, want)
		}
	}
}

// TestLoopbackAgentGoesThroughTheEnvironmentProxy is the regression test of
// coordinator decision 5: with HTTP_PROXY set, a manager call to an Agent on
// 127.0.0.1 is sent to the proxy (Go's ProxyFromEnvironment went direct),
// unless NO_PROXY names it; NoProxy requests always go direct.
func TestLoopbackAgentGoesThroughTheEnvironmentProxy(t *testing.T) {
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
		{"proxied", [][2]string{{"HTTP_PROXY", "http://u:p@" + proxy}}, false, "proxy GET " + target + " Basic dTpw"},
		{"lowercase", [][2]string{{"http_proxy", proxy}}, false, "proxy GET " + target + " "},
		{"no_proxy ip", [][2]string{{"HTTP_PROXY", proxy}, {"NO_PROXY", "127.0.0.1"}}, false, "agent GET /api/status"},
		{"no_proxy cidr", [][2]string{{"HTTP_PROXY", proxy}, {"no_proxy", "127.0.0.0/8"}}, false, "agent GET /api/status"},
		{"no_proxy other", [][2]string{{"HTTP_PROXY", proxy}, {"NO_PROXY", "localhost,10.0.0.0/8"}}, false, "proxy GET " + target + " "},
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
	if _, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: target, Timeout: Seconds(5)}); !IsTransportError(err) {
		t.Errorf("socks proxy: %v, want a transport error (InvalidSchema)", err)
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
