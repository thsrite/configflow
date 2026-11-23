package routes

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestHAHandoffIsDurableIdempotentAndOnlyTouchesItsPreference(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	include, main := readText(t, haConfigPath()), readText(t, haMainConfigPath())
	f.commands = nil
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatal(err)
	}
	path, _ := haHandoffPath(f.cfg)
	original := readText(t, path)
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private preference mode: %v %v", info, err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil || readText(t, path) != original {
		t.Fatalf("replay rewrote its record: %v", err)
	}
	if err := changeHAHandoff(f.cfg, "publish-2", false); err == nil || readText(t, path) != original {
		t.Fatal("another publication replaced the current owner")
	}
	if err := changeHAHandoff(f.cfg, "publish-2", true); err == nil || readText(t, path) != original {
		t.Fatal("another publication released the current owner")
	}
	// A blocking upload does not prevent reconciliation, and no clock expiry
	// can let the VIP return immediately before that upload activates.
	release, err := AcquireServiceOperation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatalf("core operation blocked HA reconciliation: %v", err)
	}
	state := mustHAState(t, f.cfg)
	handoff, err := loadHAHandoff(f.cfg, state)
	if err != nil {
		t.Fatal(err)
	}
	handoff.CreatedAt = time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339Nano)
	data, _ := json.Marshal(handoff)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	// Simulate a fresh process by reading both durable records again.
	if !haYieldToPeer(f.cfg, mustHAState(t, f.cfg)) {
		t.Fatal("long publication or process restart lost the handoff")
	}
	if err := changeHAHandoff(f.cfg, "publish-1", true); err != nil {
		t.Fatal(err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", true); err != nil || haYieldToPeer(f.cfg, state) {
		t.Fatalf("release is not idempotent: %v", err)
	}
	if len(f.commands) != 0 || readText(t, haConfigPath()) != include || readText(t, haMainConfigPath()) != main {
		t.Fatalf("handoff changed keepalived or other instances: %v", f.commands)
	}
}

func TestHAHandoffNeverFencesLocalDNSWhenPeerFails(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	peerHealthy := false
	haHandoffProbe = func(address, name string, timeout time.Duration) error {
		if address != f.spec.PeerIP+":53" || timeout > 300*time.Millisecond {
			t.Fatalf("unexpected or unbounded peer probe: %s %s %v", address, name, timeout)
		}
		if !peerHealthy {
			return fmt.Errorf("peer unavailable")
		}
		return nil
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err == nil {
		t.Fatal("started a handoff toward a failed peer")
	}
	peerHealthy = true
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatal(err)
	}
	state := mustHAState(t, f.cfg)
	if !haYieldToPeer(f.cfg, state) {
		t.Fatal("healthy peer did not enable yielding")
	}
	peerHealthy = false
	if haYieldToPeer(f.cfg, state) {
		t.Fatal("handoff suppressed automatic takeover after peer failure")
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatalf("idempotent replay failed while preference is suspended: %v", err)
	}
	peerHealthy = true
	if !haYieldToPeer(f.cfg, state) {
		t.Fatal("healthy peer recovery did not restore the planned preference")
	}
}

func TestHAHandoffOwnershipAndMalformedRecordFailOpen(t *testing.T) {
	f := newHAFixture(t)
	if err := changeHAHandoff(f.cfg, "publish-1", false); err == nil {
		t.Fatal("handoff enabled without HA")
	}
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatal(err)
	}
	state := mustHAState(t, f.cfg)
	other := *state
	other.Token = strings.Repeat("f", 32)
	if haYieldToPeer(f.cfg, &other) {
		t.Fatal("a new owner inherited the old handoff")
	}
	other = *state
	other.Spec.PeerIP = "192.168.0.22"
	if haYieldToPeer(f.cfg, &other) || haYieldToPeer(f.cfg, nil) {
		t.Fatal("reconfigured or disabled HA inherited the old handoff")
	}
	path, _ := haHandoffPath(f.cfg)
	if err := os.WriteFile(path, []byte(`{"version":`), 0600); err != nil {
		t.Fatal(err)
	}
	if haYieldToPeer(f.cfg, state) {
		t.Fatal("corrupt preference fenced the local server")
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err == nil {
		t.Fatal("overwrote a record with unknown ownership")
	}
}

func TestHAHandoffRejectsConfigurationDriftWithoutServiceChanges(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	if err := os.WriteFile(haConfigPath(), []byte(readText(t, haConfigPath())+"# external edit\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err == nil || len(f.commands) != 0 {
		t.Fatalf("drift accepted or service changed: %v %v", err, f.commands)
	}
	path, _ := haHandoffPath(f.cfg)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("rejected handoff wrote its record")
	}
}

func TestHAHandoffStatusProbesVIPFromBackupAndHidesPrivateOwnership(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatal(err)
	}
	vipProbes := 0
	haHandoffProbe = func(address, _ string, _ time.Duration) error {
		if address == f.spec.VIP+":53" {
			vipProbes++
		}
		return nil
	}
	result := haStatus(f.cfg)
	runtime := result.Runtime
	if runtime.VIPPresent || !runtime.VIPHealthy || vipProbes != 1 {
		t.Fatalf("backup did not test the remote VIP network path: %v %d", runtime, vipProbes)
	}
	data, _ := json.Marshal(result)
	if strings.Contains(string(data), mustHAState(t, f.cfg).Token) || strings.Contains(string(data), `"token"`) {
		t.Fatal("handoff status exposed private ownership")
	}
	if result.Handoff.(map[string]any)["operation_id"] != "publish-1" {
		t.Fatal("public handoff status lost its operation identity")
	}
}

func TestHAHandoffHTTPAuthenticationValidationAndCapabilities(t *testing.T) {
	f := newHAFixture(t)
	f.cfg.Token = "secret"
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterRoutes(mux, f.cfg)
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			r.Header.Set("Authorization", "Bearer secret")
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
		if w := request(method, "/api/oxidns-ha/handoff", `{"operation_id":"publish-1"}`, false); w.Code != 401 {
			t.Fatalf("unauthenticated %s: %d", method, w.Code)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"operation_id":"../bad"}`, `{"operation_id":"ok","peer_ip":"192.0.2.1"}`, `{"operation_id":"ok"} {}`} {
		if w := request(http.MethodPost, "/api/oxidns-ha/handoff", body, true); w.Code != 400 {
			t.Fatalf("invalid request %s: %d", body, w.Code)
		}
	}
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		w := request(method, "/api/oxidns-ha/handoff", `{"operation_id":"publish-1"}`, true)
		if w.Code != 200 || strings.Contains(w.Body.String(), `"token"`) {
			t.Fatalf("valid %s: %d %s", method, w.Code, w.Body.String())
		}
	}
	if w := request(http.MethodGet, "/api/capabilities", "", true); !strings.Contains(w.Body.String(), `"oxidns_ha_handoff_protocols":[1]`) {
		t.Fatalf("missing handoff capability: %s", w.Body.String())
	}
	for _, service := range []string{"mihomo", "mosdns"} {
		cfg := *f.cfg
		cfg.ServiceType = service
		otherMux := http.NewServeMux()
		RegisterRoutes(otherMux, &cfg)
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPost, "/api/oxidns-ha/handoff", strings.NewReader(`{"operation_id":"publish-1"}`))
		r.Header.Set("Authorization", "Bearer secret")
		otherMux.ServeHTTP(w, r)
		if w.Code != 404 {
			t.Fatalf("%s exposed the handoff endpoint: %d", service, w.Code)
		}
	}
}

func TestHAHealthRejectsUnhealthyDNSResponses(t *testing.T) {
	for _, rcode := range []byte{0, 2, 3, 5} {
		t.Run(fmt.Sprint(rcode), func(t *testing.T) {
			address := fakeDNSServer(t, func(query []byte) []byte {
				reply := append([]byte(nil), query...)
				reply[2] |= 0x80
				reply[3] = (reply[3] &^ 0x0f) | rcode
				return reply
			})
			err := haDNSResolves(address, haCheckName, 100*time.Millisecond)
			if (err == nil) != (rcode == 0 || rcode == 3) {
				t.Fatalf("RCODE %d: %v", rcode, err)
			}
		})
	}
	for _, invalid := range []string{"truncated", "wrong-question", "header-only"} {
		t.Run(invalid, func(t *testing.T) {
			address := fakeDNSServer(t, func(query []byte) []byte {
				reply := append([]byte(nil), query...)
				reply[2] |= 0x80
				switch invalid {
				case "truncated":
					reply[2] |= 2
				case "wrong-question":
					reply[13] ^= 1
				case "header-only":
					reply = reply[:12]
				}
				return reply
			})
			if haDNSResolves(address, haCheckName, 40*time.Millisecond) == nil {
				t.Fatalf("accepted %s packet", invalid)
			}
		})
	}
}

func TestRunHACheckYieldsOnlyWhilePeerIsHealthy(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", false); err != nil {
		t.Fatal(err)
	}
	local := fakeDNSServer(t, func(query []byte) []byte {
		reply := append([]byte(nil), query...)
		reply[2] |= 0x80
		return reply
	})
	_, port, _ := net.SplitHostPort(local)
	f.writeOxiDNS(t, "0.0.0.0:"+port)
	data, _ := json.Marshal(map[string]string{"config_path": f.cfg.ConfigPath})
	if err := os.WriteFile(f.cfg.AgentConfigFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	if code := RunOxiDNSHACheck(f.cfg.AgentConfigFile); code != 1 {
		t.Fatalf("healthy peer did not lower the holder's priority: %d", code)
	}
	haHandoffProbe = func(string, string, time.Duration) error { return fmt.Errorf("peer failed") }
	if code := RunOxiDNSHACheck(f.cfg.AgentConfigFile); code != 0 {
		t.Fatalf("healthy local DNS could not resume automatic takeover: %d", code)
	}
	if err := changeHAHandoff(f.cfg, "publish-1", true); err != nil {
		t.Fatal(err)
	}
	if code := RunOxiDNSHACheck(f.cfg.AgentConfigFile); code != 0 {
		t.Fatalf("released handoff still suppressed local priority: %d", code)
	}
}

func TestHALocalLivenessStillAcceptsSERVFAIL(t *testing.T) {
	address := fakeDNSServer(t, func(query []byte) []byte {
		reply := append([]byte(nil), query...)
		reply[2] |= 0x80
		reply[3] = (reply[3] &^ 0x0f) | 2
		return reply
	})
	if err := haDNSAnswers(address, haCheckName, 100*time.Millisecond); err != nil {
		t.Fatalf("upstream failure changed local liveness: %v", err)
	}
	if haDNSResolves(address, haCheckName, 100*time.Millisecond) == nil {
		t.Fatal("SERVFAIL authorized a planned handoff")
	}
	f := newHAFixture(t)
	_, port, _ := net.SplitHostPort(address)
	f.writeOxiDNS(t, "0.0.0.0:"+port)
	data, _ := json.Marshal(map[string]string{"config_path": f.cfg.ConfigPath})
	if err := os.WriteFile(f.cfg.AgentConfigFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	if code := RunOxiDNSHACheck(f.cfg.AgentConfigFile); code != 0 {
		t.Fatalf("ordinary keepalived health check rejected responding core: %d", code)
	}
}
