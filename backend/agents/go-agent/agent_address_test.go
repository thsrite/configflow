package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Other tests configure addresses this machine does not have. Unless a test
// states the interfaces, they are unreadable and a configured agent_ip is kept.
func init() {
	localInterfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("interfaces are not inspected in tests") }
}

func withInterfaces(t *testing.T, addresses ...string) {
	t.Helper()
	previousAddrs, previousDetect, previousFlag := localInterfaceAddrs, detectLocalIPv4Except, serverReadsHeartbeatHost.Load()
	t.Cleanup(func() {
		localInterfaceAddrs, detectLocalIPv4Except = previousAddrs, previousDetect
		serverReadsHeartbeatHost.Store(previousFlag)
	})
	localInterfaceAddrs = func() ([]net.Addr, error) {
		result := []net.Addr{}
		for _, value := range addresses {
			result = append(result, &net.IPNet{IP: net.ParseIP(value), Mask: net.CIDRMask(24, 32)})
		}
		return result, nil
	}
	detectLocalIPv4Except = func([]string) (string, error) {
		if len(addresses) == 0 {
			return "", errors.New("no address")
		}
		return addresses[0], nil
	}
}

func TestConfiguredAgentIPIsUsedWhileTheHostHasIt(t *testing.T) {
	withInterfaces(t, "192.0.2.11", "192.0.2.2")
	cfg := &Config{AgentIP: "192.0.2.2", ServiceManager: "systemd"}
	if got := cfg.effectiveAgentIP(); got != "192.0.2.2" {
		t.Fatalf("effective = %q", got)
	}
	if got := cfg.heartbeatHost(); got != "192.0.2.2" {
		t.Fatalf("heartbeat host = %q", got)
	}
}

func TestStaleAgentIPFallsBackToDetection(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	cfg := &Config{AgentIP: "192.0.2.2", ServiceManager: "systemd"}
	if got := cfg.effectiveAgentIP(); got != "" {
		t.Fatalf("stale agent_ip kept: %q", got)
	}
	if got := cfg.heartbeatHost(); got != "192.0.2.11" {
		t.Fatalf("heartbeat host = %q", got)
	}
	if got := toRoutesConfig(cfg).AgentIP; got != "" {
		t.Fatalf("routes still see the stale address: %q", got)
	}
}

func TestUnreadableInterfacesKeepTheConfiguredAddress(t *testing.T) {
	withInterfaces(t)
	localInterfaceAddrs = func() ([]net.Addr, error) { return nil, errors.New("denied") }
	cfg := &Config{AgentIP: "192.0.2.2", ServiceManager: "openrc"}
	if got := cfg.effectiveAgentIP(); got != "192.0.2.2" {
		t.Fatalf("effective = %q", got)
	}
}

func TestContainerReportsOnlyItsExplicitAddress(t *testing.T) {
	withInterfaces(t, "172.17.0.2")
	if got := (&Config{ServiceManager: "supervisor"}).heartbeatHost(); got != "" {
		t.Fatalf("bridge container reported %q", got)
	}
	cfg := &Config{AgentIP: "192.0.2.21", ServiceManager: "supervisor"}
	if cfg.heartbeatHost() != "192.0.2.21" || cfg.effectiveAgentIP() != "192.0.2.21" {
		t.Fatal("explicit AGENT_IP of a container must be kept")
	}
}

func TestHeartbeatCarriesHostOnlyForServersThatReadIt(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	disabled := false
	cfg := &Config{ServiceManager: "systemd", EnableMetrics: &disabled}
	decode := func() map[string]any {
		data, err := cfg.createHeartbeatRequest()
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(data, &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	serverReadsHeartbeatHost.Store(false)
	if _, present := decode()["host"]; present {
		t.Fatal("an older server would reject the unknown field")
	}
	serverReadsHeartbeatHost.Store(true)
	if got := decode()["host"]; got != "192.0.2.11" {
		t.Fatalf("host = %v", got)
	}
}

func addressConfig(t *testing.T, agentIP, manager string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config-oxidns.json")
	data, _ := json.Marshal(map[string]interface{}{"server_url": "http://192.0.2.1", "agent_name": "dns", "agent_port": 8082,
		"agent_ip": agentIP, "service_type": "oxidns", "service_manager": manager, "installer_extension": "kept"})
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func storedAgentIP(t *testing.T, cfg *Config) (string, map[string]interface{}) {
	t.Helper()
	data, err := os.ReadFile(cfg.filePath)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]interface{}{}
	if err = json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	value, _ := values["agent_ip"].(string)
	return value, values
}

func TestConfirmedAddressReplacesAStalePinOnceAndOnlyOnce(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	cfg := addressConfig(t, "192.0.2.2", "systemd")
	// Not confirmed yet, or confirmed for an address this Agent does not report: the file is left alone.
	cfg.adoptConfirmedHost("")
	cfg.adoptConfirmedHost("192.0.2.2")
	cfg.adoptConfirmedHost("192.0.2.99")
	if got, _ := storedAgentIP(t, cfg); got != "192.0.2.2" {
		t.Fatalf("file changed before confirmation: %q", got)
	}
	cfg.adoptConfirmedHost("192.0.2.11")
	got, values := storedAgentIP(t, cfg)
	if got != "192.0.2.11" || cfg.effectiveAgentIP() != "192.0.2.11" || values["installer_extension"] != "kept" {
		t.Fatalf("stored %q, effective %q, values %v", got, cfg.effectiveAgentIP(), values)
	}
	before, err := os.Stat(cfg.filePath)
	if err != nil {
		t.Fatal(err)
	}
	// Every later heartbeat carries the same confirmation; the file must not be rewritten.
	if err = os.Chmod(filepath.Dir(cfg.filePath), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(cfg.filePath), 0700) })
	for i := 0; i < 5; i++ {
		cfg.adoptConfirmedHost("192.0.2.11")
	}
	after, err := os.Stat(cfg.filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("config file was rewritten for an unchanged address")
	}
}

func TestConfirmedAddressIsNotWrittenWhenNothingWasPinnedOrInAContainer(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	automatic := addressConfig(t, "", "systemd")
	automatic.adoptConfirmedHost("192.0.2.11")
	if got, _ := storedAgentIP(t, automatic); got != "" {
		t.Fatalf("automatic install was pinned to %q", got)
	}
	container := addressConfig(t, "192.0.2.2", "supervisor")
	container.adoptConfirmedHost("192.0.2.11")
	if got, _ := storedAgentIP(t, container); got != "192.0.2.2" {
		t.Fatalf("container AGENT_IP replaced by %q", got)
	}
}

func TestPinStillOnThisHostIsNeverReplaced(t *testing.T) {
	// Two addresses: the pinned one is still here, so it is what the Agent
	// reports and no confirmation for the other one can replace it.
	withInterfaces(t, "192.0.2.11", "192.0.2.2")
	cfg := addressConfig(t, "192.0.2.2", "systemd")
	cfg.adoptConfirmedHost("192.0.2.11")
	if got, _ := storedAgentIP(t, cfg); got != "192.0.2.2" {
		t.Fatalf("pinned address replaced by %q", got)
	}
}

func TestFailedWriteKeepsTheOldValueInMemory(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	cfg := addressConfig(t, "192.0.2.2", "systemd")
	if err := os.Chmod(filepath.Dir(cfg.filePath), 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Dir(cfg.filePath), 0700) })
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	cfg.adoptConfirmedHost("192.0.2.11")
	if got := cfg.configuredAgentIP(); got != "192.0.2.2" {
		t.Fatalf("memory says %q although the file was not written", got)
	}
}

func TestRoutesSeeTheAddressAsItIsNow(t *testing.T) {
	withInterfaces(t, "192.0.2.2")
	cfg := addressConfig(t, "192.0.2.2", "systemd")
	routesCfg := toRoutesConfig(cfg)
	if got := routesCfg.AgentIPSource(); got != "192.0.2.2" {
		t.Fatalf("source = %q", got)
	}
	// The host is renumbered while the Agent keeps running.
	withInterfaces(t, "192.0.2.11")
	if got := routesCfg.AgentIPSource(); got != "" {
		t.Fatalf("routes still see the address the host no longer has: %q", got)
	}
}

func addressRequest(t *testing.T, cfg *Config, method, body string) (int, map[string]interface{}) {
	t.Helper()
	recorder := httptest.NewRecorder()
	agentAddressHandler(cfg)(recorder, httptest.NewRequest(method, "/api/agent-address", strings.NewReader(body)))
	result := map[string]interface{}{}
	if recorder.Body.Len() > 0 {
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatalf("body %q: %v", recorder.Body.String(), err)
		}
	}
	return recorder.Code, result
}

func TestAddressEndpointListsAndPinsOnlyAddressesOfThisHost(t *testing.T) {
	withInterfaces(t, "192.0.2.11", "192.0.2.2")
	previous := detectAllLocalIPv4
	detectAllLocalIPv4 = func() ([]string, error) { return []string{"192.0.2.11", "192.0.2.2", "192.0.2.11"}, nil }
	t.Cleanup(func() { detectAllLocalIPv4 = previous })
	cfg := addressConfig(t, "", "systemd")

	code, info := addressRequest(t, cfg, http.MethodGet, "")
	if code != 200 || info["writable"] != true || info["configured"] != "" || info["reported"] != "192.0.2.11" ||
		len(info["addresses"].([]interface{})) != 2 {
		t.Fatalf("GET %d %v", code, info)
	}
	if code, reply := addressRequest(t, cfg, http.MethodPost, `{"host":"192.0.2.99"}`); code != 400 || !strings.Contains(reply["message"].(string), "192.0.2.99") {
		t.Fatalf("foreign address: %d %v", code, reply)
	}
	if code, _ := addressRequest(t, cfg, http.MethodPost, `{`); code != 400 {
		t.Fatalf("invalid body: %d", code)
	}
	if got, _ := storedAgentIP(t, cfg); got != "" {
		t.Fatalf("rejected request wrote %q", got)
	}
	if code, reply := addressRequest(t, cfg, http.MethodPost, `{"host":" 192.0.2.2 "}`); code != 200 || reply["host"] != "192.0.2.2" {
		t.Fatalf("pin: %d %v", code, reply)
	}
	if got, _ := storedAgentIP(t, cfg); got != "192.0.2.2" || cfg.heartbeatHost() != "192.0.2.2" {
		t.Fatalf("stored %q, reported %q", got, cfg.heartbeatHost())
	}
	if code, _ := addressRequest(t, cfg, http.MethodDelete, ""); code != 405 {
		t.Fatalf("DELETE: %d", code)
	}
}

func TestAddressEndpointDoesNotWriteAContainersAddress(t *testing.T) {
	withInterfaces(t, "192.0.2.11")
	previous := detectAllLocalIPv4
	detectAllLocalIPv4 = func() ([]string, error) { return []string{"192.0.2.11"}, nil }
	t.Cleanup(func() { detectAllLocalIPv4 = previous })
	cfg := addressConfig(t, "192.0.2.2", "supervisor")
	if code, info := addressRequest(t, cfg, http.MethodGet, ""); code != 200 || info["writable"] != false || info["configured"] != "192.0.2.2" {
		t.Fatalf("GET %d %v", code, info)
	}
	if code, _ := addressRequest(t, cfg, http.MethodPost, `{"host":"192.0.2.11"}`); code != 409 {
		t.Fatalf("POST: %d", code)
	}
	if got, _ := storedAgentIP(t, cfg); got != "192.0.2.2" {
		t.Fatalf("container address written: %q", got)
	}
}
