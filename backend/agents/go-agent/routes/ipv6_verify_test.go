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

func TestIPv6VerificationReadsRuntimeWithoutNodeTest(t *testing.T) {
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
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := ipv6VerificationTestConfig(t, strings.TrimPrefix(server.URL, "http://"))
	data, _ := os.ReadFile(cfg.ConfigPath)
	code, result := ipv6RunVerification(t, cfg, "verify", map[string]any{"test_url": "https://example.test/check", "expected_config_sha256": sha256Hex(data)})
	if code != 200 {
		t.Fatalf("unexpected verification %v", result)
	}
	for id, want := range map[string]string{"deployed_config": "ok", "runtime_core": "ok", "runtime_ipv6": "ok", "runtime_tun": "ok", "dns_aaaa": "ok", "client_path": "unknown"} {
		if got := ipv6CheckStatus(t, result, id); got != want {
			t.Errorf("%s = %s, want %s", id, got, want)
		}
	}
	for _, raw := range result["checks"].([]any) {
		if id := raw.(map[string]any)["id"]; id == "proxy_http" || id == "node_ipv6_transport" {
			t.Errorf("node test %s still reported", id)
		}
	}
	for _, path := range paths {
		if !strings.HasPrefix(path, "GET ") || strings.HasPrefix(path, "GET /proxies") {
			t.Fatalf("verification touched proxies or mutated core: %s", path)
		}
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

func TestIPv6ConnectionTransport(t *testing.T) {
	tests := []struct {
		name   string
		remote string
		proxy  ipv6RuntimeProxy
		want   string
	}{
		{"ipv6 node", "2001:db8::1", ipv6RuntimeProxy{Type: "AnyTLS"}, "ipv6"},
		{"ipv6 node with port", "[2001:db8::1]:443", ipv6RuntimeProxy{Type: "Trojan"}, "ipv6"},
		{"ipv4 node", "203.0.113.7", ipv6RuntimeProxy{Type: "Shadowsocks"}, "ipv4"},
		{"domain remote", "node.example", ipv6RuntimeProxy{Type: "Hysteria2"}, "unknown"},
		{"empty remote", "", ipv6RuntimeProxy{Type: "Vmess"}, "unknown"},
		{"smux reports the target", "2001:db8::1", ipv6RuntimeProxy{Type: "Shadowsocks", Smux: true}, "unknown"},
		{"dialer proxy hides the node socket", "2001:db8::1", ipv6RuntimeProxy{Type: "Vless", DialerProxy: "relay"}, "unknown"},
		{"remote equals the target", "2001:db8::99", ipv6RuntimeProxy{Type: "Vless"}, "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match := ipv6ConnectionMatch{RemoteDestination: test.remote, DestinationIP: "2001:db8::99", Network: "tcp"}
			if got := ipv6ConnectionTransport(match, test.proxy); got != test.want {
				t.Fatalf("transport = %s, want %s", got, test.want)
			}
		})
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
				connections = append(connections, map[string]any{"id": "new", "metadata": map[string]any{"sourceIP": "192.0.2.5", "sourcePort": "32000", "host": "fixture.test", "destinationIP": "198.18.0.2", "destinationPort": "80", "remoteDestination": "2001:db8::7"}, "chains": []string{"sub-node", "select"}, "rule": "Match"})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": connections})
		case "/proxies":
			// Provider nodes are absent here, as in a real core.
			fmt.Fprint(w, `{"proxies":{"PASS-RULE":{"name":"PASS-RULE","type":"PassRule"},"select":{"type":"Selector","all":["sub-node"]}}}`)
		case "/providers/proxies":
			fmt.Fprint(w, `{"providers":{"sub":{"proxies":[{"name":"sub-node","type":"AnyTLS","smux":false}]}}}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cfg := ipv6VerificationTestConfig(t, strings.TrimPrefix(server.URL, "http://"))
	code, result := ipv6RunVerification(t, cfg, "verify-connections", map[string]any{"source_ip": "192.0.2.5", "host": "fixture.test", "request_id": "test-request-123456"})
	if code != 200 || ipv6CheckStatus(t, result, "client_mihomo_path") != "ok" || ipv6CheckStatus(t, result, "client_ipv6_transport") != "ok" {
		t.Fatalf("provider node over IPv6 not recognised %v", result)
	}
	matches := result["matches"].([]any)
	if len(matches) != 1 || matches[0].(map[string]any)["id"] != "new" {
		t.Fatal("old connection used as evidence")
	}
	match := matches[0].(map[string]any)
	if match["proxy_name"] != "sub-node" || match["transport"] != "ipv6" || match["ipv6_transport_verified"] != true {
		t.Fatalf("unexpected match %v", match)
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
