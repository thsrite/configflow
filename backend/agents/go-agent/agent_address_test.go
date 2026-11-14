package main

import (
	"encoding/json"
	"errors"
	"net"
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
