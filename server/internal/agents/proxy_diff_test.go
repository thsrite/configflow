package agents

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// TestProxyDifferentialAgainstRequests (CONFIGFLOW_DIFF=1, test server): a
// loopback recording proxy and a fake Agent on 127.0.0.1; for every
// environment, real requests 2.31 (Session with trust_env, as the manager's
// calls) and HTTPClient send the same request, and what reached the proxy
// and the Agent (request target, Host, Proxy-Authorization) must be equal.
// No ported route sends a trust_env request yet (A14/A15 routes will), so
// the comparison is made at the client.
func TestProxyDifferentialAgainstRequests(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 runs the differential against real requests")
	}
	python := filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	var mu sync.Mutex
	var seen []string
	record := func(who string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			seen = append(seen, who+" "+r.Method+" "+r.RequestURI+" host="+r.Host+" pa="+r.Header.Get("Proxy-Authorization")+
				" auth="+r.Header.Get("Authorization"))
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"success": true}`))
		}
	}
	proxy := listenHTTP(t, record("proxy"))
	agent := listenHTTP(t, record("agent"))
	_, agentPort, _ := strings.Cut(agent, ":")
	targets := []string{"http://" + agent + "/api/status?x=1", "http://localhost:" + agentPort + "/api/logs"}
	envs := [][][2]string{
		{{"HTTP_PROXY", "http://" + proxy}},
		{{"http_proxy", proxy}},
		{{"HTTP_PROXY", "http://user:pa%40ss@" + proxy}},
		{{"ALL_PROXY", "http://" + proxy}},
		{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "127.0.0.1"}},
		{{"HTTP_PROXY", "http://" + proxy}, {"no_proxy", "127.0.0.0/8"}},
		{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "localhost"}},
		{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "LOCALHOST"}},
		{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", "*"}},
		{{"HTTP_PROXY", "http://" + proxy}, {"NO_PROXY", ":" + agentPort}},
		{{"HTTP_PROXY", "http://" + proxy}, {"http_proxy", ""}},
		{{"REQUEST_METHOD", "GET"}, {"HTTP_PROXY", "http://" + proxy}},
		{{"HTTPS_PROXY", "http://" + proxy}},
		{},
	}
	const script = `import sys, requests
s = requests.Session()
r = s.request('GET', sys.argv[1], headers={'Authorization': 'Bearer tok', 'Accept': 'application/json'}, timeout=(3, 5))
print(r.status_code)`
	run := func(env [][2]string, target string) (string, string) {
		mu.Lock()
		seen = nil
		mu.Unlock()
		cmd := exec.Command(python, "-I", "-c", script, target)
		cmd.Env = []string{}
		for _, pair := range env {
			cmd.Env = append(cmd.Env, pair[0]+"="+pair[1])
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("python %q %s: %v\n%s", env, target, err, out)
		}
		mu.Lock()
		python := strings.Join(seen, "|")
		seen = nil
		mu.Unlock()
		withEnviron(t, env)
		header := http.Header{"Authorization": {"Bearer tok"}, "Accept": {"application/json"}}
		if _, err := (HTTPClient{}).Do(context.Background(), Request{Method: "GET", URL: target, Header: header, Timeout: Pair(3, 5)}); err != nil {
			t.Fatalf("go %q %s: %v", env, target, err)
		}
		mu.Lock()
		defer mu.Unlock()
		return python, strings.Join(seen, "|")
	}
	cases, viaProxy := 0, 0
	for _, env := range envs {
		for _, target := range targets {
			python, golang := run(env, target)
			cases++
			if strings.HasPrefix(python, "proxy") {
				viaProxy++
			}
			if python != golang {
				t.Errorf("env %q %s:\n python %s\n go     %s", env, target, python, golang)
			}
		}
	}
	t.Logf("%d requests compared (%d through the proxy in both)", cases, viaProxy)
}
