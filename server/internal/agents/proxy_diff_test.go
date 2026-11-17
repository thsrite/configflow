package agents

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestProxyDifferentialAgainstRequests compares actual recorded requests.
// Each deliberate N2 difference has independent Python and Go expectations;
// request targets and credentials are never normalized or skipped.
func TestProxyDifferentialAgainstRequests(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs real requests")
	}
	python := filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	var mu sync.Mutex
	var seen []string
	record := func(who string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, who+" "+r.Method+" "+r.RequestURI+" host="+r.Host+" pa="+r.Header.Get("Proxy-Authorization")+" auth="+r.Header.Get("Authorization"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success":true}`))
		}
	}
	proxy := listenHTTP(t, record("proxy"))
	agent := listenHTTP(t, record("agent"))
	_, port, _ := strings.Cut(agent, ":")
	targets := []string{"http://" + agent + "/api/status?x=1", "http://localhost:" + port + "/api/logs", "http://agent.example:" + port + "/api/status"}
	// Only DNS is faked: both real clients connect to loopback when going
	// direct to the synthetic routable hostname. Proxy decisions are real.
	oldResolve := resolveHook
	resolveHook = func(host string) (string, error) { return "127.0.0.1", nil }
	t.Cleanup(func() { resolveHook = oldResolve })
	tests := []struct {
		name     string
		env      [][2]string
		py       [3]bool
		goRemote bool
		goCGI    bool
	}{
		{"uppercase", [][2]string{{"HTTP_PROXY", "http://" + proxy}}, [3]bool{true, true, true}, true, false},
		{"lowercase", [][2]string{{"http_proxy", proxy}}, [3]bool{true, true, true}, true, false},
		{"credentials", [][2]string{{"HTTP_PROXY", "http://user:pa%40ss@" + proxy}}, [3]bool{true, true, true}, true, false},
		{"all-proxy", [][2]string{{"ALL_PROXY", "http://" + proxy}}, [3]bool{true, true, true}, true, false},
		{"no-proxy-ip", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "127.0.0.1"}}, [3]bool{false, true, true}, true, false},
		{"no-proxy-cidr", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"no_proxy", "127.0.0.0/8"}}, [3]bool{false, true, true}, true, false},
		{"no-proxy-localhost", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "localhost"}}, [3]bool{true, false, true}, true, false},
		{"no-proxy-case", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "LOCALHOST"}}, [3]bool{true, false, true}, true, false},
		{"no-proxy-star", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "*"}}, [3]bool{}, false, false},
		{"no-proxy-port-only", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", ":" + port}}, [3]bool{true, false, false}, true, false},
		{"lowercase-empty", [][2]string{{"HTTP_PROXY", "http://" + proxy}, {"http_proxy", ""}}, [3]bool{}, true, false},
		{"cgi", [][2]string{{"REQUEST_METHOD", "GET"}, {"HTTP_PROXY", "http://" + proxy}}, [3]bool{}, false, true},
		{"https-only", [][2]string{{"HTTPS_PROXY", "http://" + proxy}}, [3]bool{}, false, false},
		{"unset", nil, [3]bool{}, false, false},
	}
	const script = `import sys, socket, requests
original = socket.getaddrinfo
def resolve(host, *args, **kwargs):
    return original('127.0.0.1' if host == 'agent.example' else host, *args, **kwargs)
socket.getaddrinfo = resolve
r = requests.Session().get(sys.argv[1], headers={'Authorization':'Bearer synthetic-token','Accept':'application/json'}, timeout=(3,5))
assert r.status_code == 200
`
	snapshot := func() string { mu.Lock(); defer mu.Unlock(); s := strings.Join(seen, "|"); seen = nil; return s }
	count, differences := 0, 0
	for _, tt := range tests {
		for i, target := range targets {
			t.Run(fmt.Sprintf("%s/%d", tt.name, i), func(t *testing.T) {
				want := func(proxied bool) string {
					path := []string{"/api/status?x=1", "/api/logs", "/api/status"}[i]
					host := strings.TrimPrefix(strings.TrimSuffix(target, path), "http://")
					uri, who, auth := path, "agent", ""
					if proxied {
						uri, who = target, "proxy"
						if tt.name == "credentials" {
							auth = "Basic dXNlcjpwYUBzcw=="
						}
					}
					return who + " GET " + uri + " host=" + host + " pa=" + auth + " auth=Bearer synthetic-token"
				}
				snapshot()
				cmd := exec.Command(python, "-I", "-c", script, target)
				cmd.Env = []string{}
				for _, pair := range tt.env {
					cmd.Env = append(cmd.Env, pair[0]+"="+pair[1])
				}
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("Python failed: %v %s", err, out)
				}
				py := snapshot()
				if py != want(tt.py[i]) {
					t.Fatalf("Python boundary got %q want %q", py, want(tt.py[i]))
				}
				withEnviron(t, tt.env)
				response, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: target, Header: http.Header{"Authorization": {"Bearer synthetic-token"}, "Accept": {"application/json"}}, Timeout: Pair(3, 5)})
				got := snapshot()
				if tt.goCGI {
					if err == nil || !IsTransportError(err) || !strings.Contains(err.Error(), "CGI") || got != "" {
						t.Fatalf("unsafe CGI outcome: %v requests=%q", err, got)
					}
					differences++
					count++
					return
				}
				expected := want(i == 2 && tt.goRemote)
				if err != nil || response.StatusCode != 200 || got != expected {
					t.Fatalf("Go boundary: %v got %q want %q", err, got, expected)
				}
				if py != got {
					differences++
				}
				count++
			})
		}
	}
	if count != 42 || differences != 18 {
		t.Fatalf("cases=%d differences=%d", count, differences)
	}
	t.Logf("%d recorded requests compared; %d explicit native N2 outcomes", count, differences)
}
