package routes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxGatewayRequiresCoreThatUnderstandsSelectiveRouting(t *testing.T) {
	original := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = original })
	config := &Config{ServiceType: "mihomo", ServiceBinary: "/custom/mihomo"}
	doc := map[string]any{"x-configflow-linux-gateway": map[string]any{"enabled": true, "version": 1}}
	for _, test := range []struct {
		output string
		accept bool
	}{
		{"Mihomo Meta v1.18.5 linux arm64 with go1.23.0", false},
		{"Mihomo Meta v1.19.0 linux amd64 with go1.24.0", true},
		{"Mihomo Meta v1.20.1 darwin arm64 with go1.26.0", true},
		{"Mihomo Meta alpha-unknown linux arm64", false},
		{"Mihomo Meta v1.19.0-alpha linux arm64", false},
		{"unrelated tool v1.99.0", false},
	} {
		gatewayCoreVersion = func(binary string) (string, error) {
			if binary != config.ServiceBinary {
				t.Fatalf("checked a different executable: %s", binary)
			}
			return test.output, nil
		}
		if err := checkLinuxGatewayCore(config, doc); (err == nil) != test.accept {
			t.Errorf("core %q: accepted=%v error=%v", test.output, test.accept, err)
		}
	}
	gatewayCoreVersion = func(string) (string, error) { return "", fmt.Errorf("missing core") }
	if err := checkLinuxGatewayCore(config, doc); err == nil {
		t.Fatal("missing core was accepted")
	}
}

func TestLinuxGatewayOldCoreBlockedBeforeDeploymentChangesLiveService(t *testing.T) {
	original := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = original })
	gatewayCoreVersion = func(string) (string, error) { return "Mihomo Meta v1.18.5 linux arm64", nil }
	cfg := testDeploymentConfig(t)
	cfg.AgentIP = "192.0.2.2"
	stubAgentIPv4(t, func() (string, error) { return cfg.AgentIP, nil })
	before := []byte("mode: direct\n")
	if err := os.WriteFile(cfg.ConfigPath, before, 0600); err != nil {
		t.Fatal(err)
	}
	running := filepath.Join(filepath.Dir(cfg.ConfigPath), "running")
	if err := os.WriteFile(running, nil, 0600); err != nil {
		t.Fatal(err)
	}
	state := submitBundle(t, cfg, "gateway-old-core", map[string][]byte{
		"config.yaml": []byte("mode: rule\ndns: {enable: true}\ntun: {enable: true}\nx-configflow-linux-gateway: {enabled: true, version: 1}\n"),
	}, true)
	if state.Status != "failed" || !strings.Contains(state.Error, "v1.19.0") {
		t.Fatalf("old core did not block actual deployment: %+v", state)
	}
	after, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("live config changed: %q, %v", after, err)
	}
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("live service stopped before core capability check: %v", err)
	}
}

func TestLinuxGatewayCoreCheckIsOptIn(t *testing.T) {
	original := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = original })
	gatewayCoreVersion = func(string) (string, error) {
		t.Fatal("disabled gateway caused a core command")
		return "", nil
	}
	for _, doc := range []map[string]any{
		{}, {"x-configflow-linux-gateway": map[string]any{"enabled": false}},
	} {
		if err := checkLinuxGatewayCore(&Config{}, doc); err != nil {
			t.Fatal(err)
		}
	}
}
