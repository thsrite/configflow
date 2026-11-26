package routes

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func ipv6VerificationTestConfig(t *testing.T, controller string) *Config {
	t.Helper()
	root := t.TempDir()
	cfg := &Config{AgentConfigFile: filepath.Join(root, "agent.json"), ConfigPath: filepath.Join(root, "mihomo.yaml"), ServiceType: "mihomo", ServiceManager: "systemd"}
	content := fmt.Sprintf("ipv6: true\nexternal-controller: %s\nsecret: must-not-leak\ntun: {enable: true}\ndns: {enable: true, ipv6: false, listen: '0.0.0.0:1053'}\nproxies: [{name: node, type: socks5, server: '2001:db8::1', port: 1080}]\n", controller)
	if err := os.WriteFile(cfg.ConfigPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	oldOS, oldQuery := gatewayOS, ipv6VerifyDNSQuery
	gatewayOS = "linux"
	ipv6VerifyDNSQuery = func(_ context.Context, _, _ string, kind uint16) (ipv6DNSAnswer, error) {
		if kind == 1 {
			return ipv6DNSAnswer{Addresses: []net.IP{net.ParseIP("198.18.0.2")}}, nil
		}
		return ipv6DNSAnswer{}, nil
	}
	t.Cleanup(func() { gatewayOS = oldOS; ipv6VerifyDNSQuery = oldQuery })
	return cfg
}

func ipv6RunVerification(t *testing.T, cfg *Config, action string, body any) (int, map[string]any) {
	t.Helper()
	data, _ := json.Marshal(body)
	recorder := httptest.NewRecorder()
	IPv6ReadinessHandler(cfg, action)(recorder, httptest.NewRequest(http.MethodPost, "/api/ipv6-readiness/"+action, strings.NewReader(string(data))))
	var result map[string]any
	if json.Unmarshal(recorder.Body.Bytes(), &result) != nil {
		t.Fatalf("invalid response %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "must-not-leak") {
		t.Fatal("secret leaked")
	}
	return recorder.Code, result
}
func ipv6CheckStatus(t *testing.T, result map[string]any, id string) string {
	t.Helper()
	for _, raw := range result["checks"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == id {
			return row["status"].(string)
		}
	}
	t.Fatalf("missing check %s", id)
	return ""
}

func TestIPv6VerificationRequiresFreshSuccessfulHTTPNotDelayAlone(t *testing.T) {
	for _, test := range []struct {
		name           string
		alive, history bool
		want           string
	}{{"successful", true, true, "ok"}, {"http-failed-despite-delay", false, true, "unknown"}, {"no-http-evidence", true, false, "unknown"}} {
		t.Run(test.name, func(t *testing.T) {
			target := "https://example.test/check"
			var completed time.Time
			var paths []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.Method+" "+r.URL.Path)
				if r.Header.Get("Authorization") != "Bearer must-not-leak" {
					w.WriteHeader(401)
					return
				}
				switch r.URL.Path {
				case "/version":
					fmt.Fprint(w, `{"version":"test"}`)
				case "/configs":
					fmt.Fprint(w, `{"ipv6":true,"tun":{"enable":true}}`)
				case "/proxies":
					fmt.Fprint(w, `{"proxies":{"node":{"name":"node","type":"Socks5","id":"stable"}}}`)
				case "/proxies/node/delay":
					if r.URL.Query().Get("expected") != "200-299" {
						t.Error("missing expected HTTP status")
					}
					completed = time.Now()
					fmt.Fprint(w, `{"delay":2}`)
				case "/proxies/node":
					node := map[string]any{"name": "node", "type": "Socks5", "id": "stable"}
					if test.history {
						node["extra"] = map[string]any{target: map[string]any{"alive": test.alive, "history": []any{map[string]any{"time": completed, "delay": 2}}}}
					}
					_ = json.NewEncoder(w).Encode(node)
				default:
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			cfg := ipv6VerificationTestConfig(t, strings.TrimPrefix(server.URL, "http://"))
			data, _ := os.ReadFile(cfg.ConfigPath)
			code, result := ipv6RunVerification(t, cfg, "verify", map[string]any{"test_url": target, "expected_config_sha256": sha256Hex(data)})
			if code != 200 || ipv6CheckStatus(t, result, "proxy_http") != test.want {
				t.Fatalf("unexpected verification %v", result)
			}
			if ipv6CheckStatus(t, result, "dns_aaaa") != "ok" || ipv6CheckStatus(t, result, "node_ipv6_transport") != "unknown" || ipv6CheckStatus(t, result, "client_path") != "unknown" {
				t.Fatal("unsupported evidence was marked passed")
			}
			for _, path := range paths {
				if !strings.HasPrefix(path, "GET ") {
					t.Fatalf("verification mutated core: %s", path)
				}
			}
		})
	}
}

func TestIPv6VerificationRejectsWrongPublishedFileBeforeNetwork(t *testing.T) {
	cfg := ipv6VerificationTestConfig(t, "127.0.0.1:1")
	code, result := ipv6RunVerification(t, cfg, "verify", map[string]any{"expected_config_sha256": strings.Repeat("0", 64)})
	if code != 200 || ipv6CheckStatus(t, result, "deployed_config") != "error" || len(result["checks"].([]any)) != 1 {
		t.Fatalf("wrong file was verified: %v", result)
	}
}

func TestIPv6VerificationRejectsUnsafeURLForms(t *testing.T) {
	for _, raw := range []string{"file:///etc/passwd", "http://user:secret@example.com/", "https://example.com/#fragment", "https://example.com/#", "https://example.com:99999/", "https://example.com/\n"} {
		if _, err := ipv6VerificationURL(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if _, err := ipv6VerificationURL("http://fixture.lab.invalid:18080/check?value=1"); err != nil {
		t.Fatal(err)
	}
}

func TestIPv6DNSValidatesQuestionAndRecordsEmptyAAAA(t *testing.T) {
	query := buildDNSQuery(7, "example.test")
	binary.BigEndian.PutUint16(query[len(query)-4:len(query)-2], 28)
	answer := append([]byte(nil), query...)
	answer[2] = 0x81
	answer[3] = 0x80
	got, err := ipv6ParseDNS(answer, query, "example.test", 28)
	if err != nil || got.RCode != 0 || len(got.Addresses) != 0 {
		t.Fatalf("valid NODATA rejected %v", err)
	}
	answer[1]++
	if _, err = ipv6ParseDNS(answer, query, "example.test", 28); err == nil {
		t.Fatal("wrong transaction accepted")
	}
	answer[1]--
	answer[len(answer)-3] = 1
	if _, err = ipv6ParseDNS(answer, query, "example.test", 28); err == nil {
		t.Fatal("wrong question accepted")
	}
	if _, _, err := ipv6DNSName([]byte{0xc0, 0}, 0, 0); err == nil {
		t.Fatal("cyclic DNS pointer accepted")
	}
}

func TestIPv6ClientUsesDNSAnswersAndDoesNotFollowRedirects(t *testing.T) {
	cfg := ipv6VerificationTestConfig(t, "127.0.0.1:1")
	cfg.ServiceType = "oxidns"
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Location", "http://forbidden.test/")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	oldLookup, oldDial, oldRoutes, oldHold := ipv6ClientLookup, ipv6ClientDial, ipv6ClientRouteRead, ipv6ClientHold
	t.Cleanup(func() {
		ipv6ClientLookup, ipv6ClientDial, ipv6ClientRouteRead, ipv6ClientHold = oldLookup, oldDial, oldRoutes, oldHold
	})
	ipv6ClientHold = time.Millisecond
	ipv6ClientLookup = func(_ context.Context, family, host string) ([]net.IP, error) {
		if host != "fixture.test" {
			t.Error("wrong DNS name")
		}
		if family == "ip4" {
			return []net.IP{net.ParseIP("198.18.0.2")}, nil
		}
		return nil, &net.DNSError{IsNotFound: true}
	}
	ipv6ClientDial = func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp4" || address != "198.18.0.2:80" {
			t.Errorf("bypassed DNS answer: %s %s", network, address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, strings.TrimPrefix(server.URL, "http://"))
	}
	ipv6ClientRouteRead = func(_ context.Context, _ string) ([]byte, error) {
		return []byte(`[{"gateway":"192.0.2.1","dev":"eth0"}]`), nil
	}
	code, result := ipv6RunVerification(t, cfg, "verify-client", map[string]any{"test_url": "http://fixture.test/test", "fake_ipv4_range": "198.18.0.0/16", "request_id": "test-request-123456"})
	if code != 200 || ipv6CheckStatus(t, result, "client_dns_aaaa") != "ok" || ipv6CheckStatus(t, result, "client_http_ipv4") != "error" {
		t.Fatalf("redirect passed %v", result)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 1 || !strings.Contains(requests[0], "__cf_ipv6_verify=test-request-123456") {
		t.Fatalf("unexpected requests %v", requests)
	}
}

func TestIPv6SocketEvidenceRequiresOwnedObservedSocket(t *testing.T) {
	oldPID, oldSocket := ipv6VerifyCorePID, ipv6VerifySocketRead
	t.Cleanup(func() { ipv6VerifyCorePID = oldPID; ipv6VerifySocketRead = oldSocket })
	ipv6VerifyCorePID = func(context.Context, *Config) (int, error) { return 42, nil }
	ipv6VerifySocketRead = func(pid int, ip net.IP, port int) bool {
		return pid == 42 && ip.Equal(net.ParseIP("2001:db8::1")) && port == 1080
	}
	stop := ipv6ObserveNodeSockets(context.Background(), &Config{}, map[string]any{"server": "2001:db8::1", "port": 1080}, "")
	time.Sleep(time.Millisecond)
	if stop().Status != "ok" {
		t.Fatal("actual socket not recorded")
	}
	if ipv6ObserveNodeSockets(context.Background(), &Config{}, map[string]any{"server": "node.test", "port": 1080}, "")().Status != "unknown" {
		t.Fatal("DNS node inferred to use IPv6")
	}
	match := ipv6ConnectionMatch{SourcePort: 32000, RemoteDestination: "2001:db8::1", Network: "tcp"}
	node := map[string]any{"server": "2001:db8::1", "port": 1080}
	proxy := ipv6RuntimeProxy{Type: "Socks5"}
	if !ipv6ConnectionTransport(context.Background(), &Config{}, match, node, proxy) {
		t.Fatal("matching runtime remote and owned IPv6 socket not correlated")
	}
	match.RemoteDestination = "2001:db8::2"
	if ipv6ConnectionTransport(context.Background(), &Config{}, match, node, proxy) {
		t.Fatal("unrelated IPv6 socket passed client transport")
	}
	match.RemoteDestination = "2001:db8::1"
	proxy.DialerProxy = "other"
	if ipv6ConnectionTransport(context.Background(), &Config{}, match, node, proxy) {
		t.Fatal("chained proxy wrongly inferred as physical IPv6")
	}
	ip, port, err := ipv6ProcAddress("B80D0120000000000000000001000000:0438")
	if err != nil || !ip.Equal(net.ParseIP("2001:db8::1")) || port != 1080 {
		t.Fatalf("invalid proc decode %s %d %v", ip, port, err)
	}
}

func TestIPv6ConnectionsOnlyAssociatesNewClientConnections(t *testing.T) {
	count := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/connections":
			count++
			connections := []any{map[string]any{"id": "old", "metadata": map[string]any{"sourceIP": "192.0.2.5", "host": "fixture.test"}, "chains": []string{"node"}}}
			if count > 1 {
				connections = append(connections, map[string]any{"id": "new", "metadata": map[string]any{"sourceIP": "192.0.2.5", "sourcePort": "32000", "host": "fixture.test", "destinationIP": "198.18.0.2", "destinationPort": "80"}, "chains": []string{"node", "select"}, "rule": "Match"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": connections})
		case "/proxies":
			fmt.Fprint(w, `{"proxies":{"node":{"type":"Socks5"},"select":{"type":"Selector","all":["node"]}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := ipv6VerificationTestConfig(t, strings.TrimPrefix(server.URL, "http://"))
	code, result := ipv6RunVerification(t, cfg, "verify-connections", map[string]any{"source_ip": "192.0.2.5", "host": "fixture.test", "request_id": "test-request-123456"})
	if code != 200 || ipv6CheckStatus(t, result, "client_mihomo_path") != "ok" {
		t.Fatalf("new connection not found %v", result)
	}
	matches := result["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["id"] != "new" {
		t.Fatal("old connection used as evidence")
	}
}

func TestIPv6FreshHTTPResultRejectsStaleOrConcurrentHistory(t *testing.T) {
	started := time.Now()
	for _, times := range [][]time.Time{{started.Add(-time.Second)}, {started, started.Add(time.Millisecond)}} {
		raw, _ := json.Marshal(map[string]any{"extra": map[string]any{"https://test/": map[string]any{"alive": true, "history": func() []any {
			out := []any{}
			for _, when := range times {
				out = append(out, map[string]any{"time": when, "delay": 3})
			}
			return out
		}()}}})
		var proxy ipv6RuntimeProxy
		_ = json.Unmarshal(raw, &proxy)
		if ipv6FreshHTTPResult(proxy, "https://test/", started) {
			t.Fatal("ambiguous HTTP history accepted")
		}
	}
}

func TestIPv6VerificationCapabilitiesExcludeUnknownContainerNetwork(t *testing.T) {
	oldOS := gatewayOS
	gatewayOS = "linux"
	defer func() { gatewayOS = oldOS }()
	for _, manager := range []string{"systemd", "docker"} {
		cfg := &Config{ServiceType: "oxidns", ServiceManager: manager}
		rec := httptest.NewRecorder()
		CapabilitiesHandler(cfg)(rec, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
		var result map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		_, present := result["ipv6_client_probe_protocols"]
		if present != (manager == "systemd") {
			t.Fatalf("incorrect capability for %s", manager)
		}
	}
}
