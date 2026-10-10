package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 来自 mihomo v1.19.32 的真实日志行
const (
	sampleDirectRefused = `[TCP] dial DIRECT (match Match/) 127.0.0.1:47434 --> 127.0.0.1:1 error: dial tcp 127.0.0.1:1: connect: connection refused`
	sampleProxyTimeout  = `[TCP] dial PROXY (match DomainSuffix/github.com) 127.0.0.1:47430 --> github.com:443 error: 10.255.255.1:1080 connect error: dial tcp 10.255.255.1:1080: i/o timeout`
)

func TestParseDialFailure(t *testing.T) {
	fail, ok := parseDialFailure(sampleProxyTimeout)
	if !ok {
		t.Fatal("expected proxy failure to parse")
	}
	want := dialFailure{Network: "tcp", Policy: "PROXY", Rule: "DomainSuffix", RulePayload: "github.com",
		Source: "127.0.0.1:47430", Host: "github.com", Port: 443, Kind: "timeout"}
	if fail != want {
		t.Fatalf("got %+v want %+v", fail, want)
	}

	line := `[TCP] dial 🐟 漏网 之鱼 (match Match/) 192.168.1.5:5000(Google Chrome) --> WWW.Blocked.COM:443 error: connection reset by peer`
	fail, ok = parseDialFailure(line)
	if !ok || fail.Policy != "🐟 漏网 之鱼" || fail.Host != "www.blocked.com" || fail.Kind != "reset" || fail.Rule != "Match" {
		t.Fatalf("unexpected parse: %+v %v", fail, ok)
	}

	for _, ignored := range []string{
		sampleDirectRefused, // 目标是 IP，没有域名可上报
		`[UDP] dial DIRECT (match Match/) 1.1.1.1:1 --> [2001:db8::1]:53 error: timeout`,
		`[TCP] dial DIRECT (match Match/) 1.1.1.1:1 --> printer.lan:80 error: timeout`,
		`Start initial configuration in progress`,
	} {
		if _, ok := parseDialFailure(ignored); ok {
			t.Fatalf("expected %q to be ignored", ignored)
		}
	}
}

func TestClassifyDialError(t *testing.T) {
	cases := map[string]string{
		"dial tcp 1.2.3.4:443: i/o timeout":    "timeout",
		"context deadline exceeded":            "timeout",
		"read: connection reset by peer":       "reset",
		"connect: connection refused":          "refused",
		"dns resolve failed: couldn't find ip": "dns",
		"lookup x: no such host":               "dns",
		"unexpected EOF":                       "eof",
		"tls: handshake failure":               "other",
	}
	for message, want := range cases {
		if got := classifyDialError(message); got != want {
			t.Errorf("%q: got %s want %s", message, got, want)
		}
	}
}

func TestNormalizeDiscoveryHost(t *testing.T) {
	cases := map[string]string{
		"Example.COM.":  "example.com",
		"1.2.3.4":       "",
		"::1":           "",
		"localhost":     "",
		"nas.local":     "",
		"bad_host.com":  "",
		"-x.com":        "",
		"a.b.c.example": "a.b.c.example",
	}
	for input, want := range cases {
		if got := normalizeDiscoveryHost(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
}

func TestResolveMihomoController(t *testing.T) {
	dir := t.TempDir()
	cases := []struct{ yaml, base, secret string }{
		{"external-controller: 0.0.0.0:9090\nsecret: \"123456\"\n", "http://127.0.0.1:9090", "123456"},
		{"external-controller: ':9091'\n", "http://127.0.0.1:9091", ""},
		{"external-controller: '[::]:9092'\n", "http://127.0.0.1:9092", ""},
		{"external-controller: 192.168.1.2:9093\n", "http://192.168.1.2:9093", ""},
	}
	for i, tc := range cases {
		path := filepath.Join(dir, fmt.Sprintf("c%d.yaml", i))
		os.WriteFile(path, []byte(tc.yaml), 0600)
		controller, err := resolveMihomoController(path)
		if err != nil || controller.BaseURL != tc.base || controller.Secret != tc.secret {
			t.Fatalf("case %d: got %+v %v", i, controller, err)
		}
	}
	path := filepath.Join(dir, "none.yaml")
	os.WriteFile(path, []byte("mixed-port: 7890\n"), 0600)
	if _, err := resolveMihomoController(path); err == nil {
		t.Fatal("expected error without external-controller")
	}
}

func conn(id, host string, download int64, start time.Time, chains ...string) mihomoConn {
	var c mihomoConn
	c.ID = id
	c.Metadata.Host = host
	c.Metadata.Network = "tcp"
	c.Metadata.DestinationPort = "443"
	c.Download = download
	c.Start = start
	c.Chains = chains
	c.Rule = "Match"
	return c
}

func TestConnTrackerFinishesVanishedConnections(t *testing.T) {
	tracker := newConnTracker(2)
	now := time.Now()
	finished, overflow := tracker.observe([]mihomoConn{conn("a", "a.com", 1, now), conn("b", "b.com", 1, now), conn("c", "c.com", 1, now)})
	if len(finished) != 0 || overflow != 1 {
		t.Fatalf("got finished=%d overflow=%d", len(finished), overflow)
	}
	updated := conn("a", "a.com", 99, now)
	finished, _ = tracker.observe([]mihomoConn{updated})
	if len(finished) != 1 || finished[0].ID != "b" {
		t.Fatalf("expected b to finish, got %+v", finished)
	}
	finished, _ = tracker.observe(nil)
	if len(finished) != 1 || finished[0].Download != 99 {
		t.Fatalf("expected last snapshot of a, got %+v", finished)
	}
}

func TestAggregatorCountsDrainsAndRestores(t *testing.T) {
	agg := newDiscoveryAggregator()
	now := time.Now()
	agg.addConn(conn("1", "x.com", 0, now.Add(-10*time.Second), "DIRECT"), now)
	agg.addConn(conn("2", "x.com", 500, now, "DIRECT"), now)
	agg.addConn(conn("3", "", 1, now, "DIRECT"), now)
	agg.addConn(conn("4", "y.com", 1, now, "HK-01", "PROXY"), now)
	fail, _ := parseDialFailure(`[TCP] dial DIRECT (match Match/) 1.1.1.1:5 --> x.com:443 error: i/o timeout`)
	agg.addFail(fail)

	items, ipOnly, dropped := agg.drain(1)
	if ipOnly != 1 || dropped != 1 || len(items) != 1 {
		t.Fatalf("got items=%d ipOnly=%d dropped=%d", len(items), ipOnly, dropped)
	}
	x := items[0]
	if x.Host != "x.com" || x.Conns != 2 || x.Fails != 1 || x.ZeroDL != 1 || x.FailKinds["timeout"] != 1 || x.Outlet != "direct" || x.Policy != "DIRECT" {
		t.Fatalf("unexpected item %+v", x)
	}
	if again, _, _ := agg.drain(10); len(again) != 0 {
		t.Fatal("drain must reset the window")
	}
	agg.restore(items, ipOnly, dropped)
	restored, ipOnly, _ := agg.drain(10)
	if len(restored) != 1 || restored[0].Conns != 2 || ipOnly != 1 {
		t.Fatalf("restore lost data: %+v", restored)
	}
}

func TestAggregatorRoutesProxyChain(t *testing.T) {
	agg := newDiscoveryAggregator()
	agg.addConn(conn("1", "y.com", 1, time.Now(), "HK-01", "PROXY"), time.Now())
	items, _, _ := agg.drain(10)
	if items[0].Outlet != "proxy" || items[0].Policy != "PROXY" {
		t.Fatalf("unexpected routing %+v", items[0])
	}
}

func TestFailureDeduperMergesRetries(t *testing.T) {
	fail, _ := parseDialFailure(`[TCP] dial DIRECT (match Match/) 1.1.1.1:5 --> x.com:443 error: refused`)
	d := &failureDeduper{}
	now := time.Now()
	if !d.first(fail, now) || d.first(fail, now.Add(time.Second)) {
		t.Fatal("retries within the window must be merged")
	}
	if !d.first(fail, now.Add(discoveryRetryDedupe+time.Second)) {
		t.Fatal("a later failure must count again")
	}
}

// 端到端：假 mihomo 控制接口 + 假 ConfigFlow 服务端
func TestCollectorEndToEnd(t *testing.T) {
	discoveryPollInterval = 50 * time.Millisecond
	defer func() { discoveryPollInterval = 2 * time.Second }()

	var mu sync.Mutex
	polls := 0
	refreshed := []string{}
	mihomo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.URL.Path == "/version":
			w.Write([]byte(`{"meta":true,"version":"v1.19.32"}`))
		case r.URL.Path == "/connections":
			mu.Lock()
			polls++
			n := polls
			mu.Unlock()
			if n == 1 {
				w.Write([]byte(`{"connections":[{"id":"c1","metadata":{"network":"tcp","host":"","sniffHost":"Sniffed.example.com","destinationPort":"443"},"upload":1,"download":2,"start":"2026-10-10T10:00:00Z","chains":["DIRECT"],"rule":"Match","rulePayload":""}]}`))
				return
			}
			w.Write([]byte(`{"connections":[]}`))
		case r.URL.Path == "/logs":
			w.(http.Flusher).Flush()
			line, _ := json.Marshal(map[string]string{"type": "warning", "payload": `[TCP] dial DIRECT (match Match/) 1.1.1.1:5 --> blocked.example.org:443 error: i/o timeout`})
			w.Write(append(line, '\n'))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/providers/rules/"):
			mu.Lock()
			refreshed = append(refreshed, strings.TrimPrefix(r.URL.Path, "/providers/rules/"))
			mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mihomo.Close()

	var report discoveryReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents/agent-1/traffic-report" || r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		json.NewDecoder(r.Body).Decode(&report)
		w.Write([]byte(`{"success":true,"rule_providers":{"域名发现-代理":"abc"}}`))
	}))
	defer server.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte(fmt.Sprintf("external-controller: %s\nsecret: s3cret\n", strings.TrimPrefix(mihomo.URL, "http://"))), 0600)
	cfg := &Config{ServerURL: server.URL, AgentID: "agent-1", Token: "tok", ConfigPath: configPath, ServiceType: "mihomo"}
	collector := newDiscoveryCollector(cfg, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go collector.pollLoop(ctx)
	go collector.logLoop(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		collector.aggregator.mu.Lock()
		size := len(collector.aggregator.items)
		collector.aggregator.mu.Unlock()
		if size == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	collector.reportOnce(ctx, time.Now().Add(-time.Minute))

	if report.Status != "running" || report.MihomoVersion != "v1.19.32" || len(report.Items) != 2 {
		t.Fatalf("unexpected report %+v", report)
	}
	hosts := map[string]trafficItem{}
	for _, item := range report.Items {
		hosts[item.Host] = item
	}
	if hosts["sniffed.example.com"].Conns != 1 || hosts["blocked.example.org"].Fails != 1 {
		t.Fatalf("unexpected items %+v", report.Items)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(refreshed) != 1 || refreshed[0] != "域名发现-代理" {
		t.Fatalf("expected provider refresh, got %v", refreshed)
	}
}

func TestCollectorReportsAuthFailure(t *testing.T) {
	mihomo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer mihomo.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(configPath, []byte("external-controller: "+strings.TrimPrefix(mihomo.URL, "http://")+"\n"), 0600)
	collector := newDiscoveryCollector(&Config{ConfigPath: configPath}, nil)
	controller, _ := collector.currentController()
	if _, err := collector.fetchConnections(context.Background(), controller); err != errControllerAuth {
		t.Fatalf("expected auth error, got %v", err)
	}
}
