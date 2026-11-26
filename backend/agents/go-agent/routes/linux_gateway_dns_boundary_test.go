package routes

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAIOPublicationWithoutLANIPv4KeepsLiveConfiguration(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "", errors.New("no active LAN interface") })
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	live := []byte("mode: direct\n")
	if err := os.WriteFile(cfg.ConfigPath, live, 0600); err != nil {
		t.Fatal(err)
	}
	input := []byte("dns: {enable: true}\ntun: {enable: true, auto-redirect: true}\n")
	state := submitBundle(t, cfg, "no-lan-ip", map[string][]byte{"config.yaml": input}, true)
	if state.Status != "failed" || state.FailedStage != "verifying" || !strings.Contains(state.Error, "no active LAN interface") {
		t.Fatalf("unexpected failure: %+v", state)
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if err != nil || string(data) != string(live) {
		t.Fatalf("failed verification changed live configuration: %q, %v", data, err)
	}
}

func TestGatewayDNSStagingDoesNotEnableOrdinaryDNSOrTUN(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) {
		t.Error("disabled TUN must not need a LAN address")
		return "", nil
	})
	t.Setenv("ENABLE_MOSDNS", "false")
	t.Setenv("ENABLE_OXIDNS", "false")
	for _, nativeAIO := range []bool{false, true} {
		name := "standalone"
		if nativeAIO {
			name = "native-aio"
		}
		t.Run(name, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO = nativeAIO
			// Ordinary agents need no gateway LAN address. Existing routing
			// exclusions and disabled DNS/TUN must survive the actual staging path.
			cfg.AgentIP = ""
			input := "mode: direct\ndns: {enable: false, listen: '127.0.0.1:5454'}\ntun: {enable: false, route-exclude-address: [10.0.0.0/8]}\nx-configflow-linux-gateway: {enabled: false, version: 1}\n"
			state := submitBundle(t, cfg, "ordinary-dns-boundary", map[string][]byte{"config.yaml": []byte(input)}, false)
			if state.Status != "ready" {
				t.Fatalf("ordinary staging failed: %+v", state)
			}
			data, err := os.ReadFile(filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := yaml.Unmarshal(data, &doc); err != nil {
				t.Fatal(err)
			}
			dns := doc["dns"].(map[string]any)
			tun := doc["tun"].(map[string]any)
			if dns["enable"] != false || tun["enable"] != false || doc["mode"] != "direct" ||
				!reflect.DeepEqual(tun["route-exclude-address"], []any{"10.0.0.0/8"}) {
				t.Fatalf("ordinary DNS/TUN behavior changed: %s", data)
			}
			wantListen := "127.0.0.1:5454"
			if dns["listen"] != wantListen {
				t.Fatalf("ordinary listener changed: %s", data)
			}
			if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
				t.Fatalf("staging wrote live configuration: %v", err)
			}
		})
	}
}

func TestAIOPublicationUsesAutomaticLANIPv4WithEmptySavedAddress(t *testing.T) {
	t.Setenv("ENABLE_MOSDNS", "false")
	t.Setenv("ENABLE_OXIDNS", "false")
	originalVersion := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = originalVersion })
	gatewayCoreVersion = func(string) (string, error) { return "Mihomo Meta v1.19.32 linux arm64", nil }
	for _, gateway := range []bool{false, true} {
		name := "native-aio"
		if gateway {
			name = "gateway"
		}
		t.Run(name, func(t *testing.T) {
			cfg := testDeploymentConfig(t)
			cfg.NativeAIO = true
			cfg.AgentIP = ""
			currentIPs := []string{"192.168.8.2", "192.168.9.2"}
			stubAgentIPv4s(t, func() ([]string, error) { return currentIPs, nil })
			input := `mode: rule
dns: {enable: true, nameserver: ['https://223.5.5.5/dns-query']}
tun: {enable: true, auto-route: true, auto-redirect: true, route-exclude-address: [10.0.0.0/8]}
`
			if gateway {
				input += `rule-providers:
  cf_gateway_fake_ip: {type: inline, behavior: ipcidr, payload: [198.18.0.0/16]}
x-configflow-linux-gateway: {enabled: true, version: 1, profile_id: gateway-test}
`
			}
			for _, id := range []string{"first", "address-changed"} {
				if id == "address-changed" {
					currentIPs = []string{"192.168.8.3", "192.168.9.3"}
				}
				// Exercise the actual upload/verification path; native AIO also
				// activates through the test core without touching system networking.
				activate := !gateway
				state := submitBundle(t, cfg, id, map[string][]byte{"config.yaml": []byte(input)}, activate)
				wantStatus := "ready"
				if activate {
					wantStatus = "succeeded"
				}
				if state.Status != wantStatus {
					t.Fatalf("automatic IP publication failed: %+v", state)
				}
				if !activate {
					if err := loadEffectiveManifest(cfg, state); err != nil {
						t.Fatalf("adapted stage no longer matches manifest: %v", err)
					}
				}
				path := filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml")
				if activate {
					path = cfg.ConfigPath
				}
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var doc map[string]any
				if err := yaml.Unmarshal(data, &doc); err != nil {
					t.Fatal(err)
				}
				wantExcluded := []any{"127.0.0.0/8"}
				for _, ip := range currentIPs {
					wantExcluded = append(wantExcluded, ip+"/32")
				}
				if !gateway {
					wantExcluded = append([]any{"10.0.0.0/8"}, wantExcluded...)
				}
				if !reflect.DeepEqual(doc["tun"].(map[string]any)["route-exclude-address"], wantExcluded) || cfg.AgentIP != "" {
					t.Fatalf("automatic address was lost, stale or persisted: %s; agent_ip=%q", data, cfg.AgentIP)
				}
			}
		})
	}
}

func TestGatewayDNSOverridesNativeAIOOnlyInMarkedStagedBundle(t *testing.T) {
	original := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = original })
	gatewayCoreVersion = func(string) (string, error) { return "Mihomo Meta v1.19.32 linux arm64", nil }
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	cfg.AgentIP = "192.0.2.2"
	stubAgentIPv4(t, func() (string, error) { return cfg.AgentIP, nil })
	input := `mode: rule
dns: {enable: true, listen: '0.0.0.0:1053', nameserver: ['https://223.5.5.5/dns-query']}
tun: {enable: true, auto-route: true, auto-redirect: true, route-address-set: [cf_gateway_fake_ip], dns-hijack: [any:1053]}
rule-providers:
  cf_gateway_fake_ip: {type: inline, behavior: ipcidr, payload: [198.18.0.0/16]}
x-configflow-linux-gateway: {enabled: true, version: 1, profile_id: gateway-test}
`
	state := submitBundle(t, cfg, "gateway-aio-dns-boundary", map[string][]byte{"config.yaml": []byte(input)}, false)
	if state.Status != "ready" {
		t.Fatalf("gateway staging failed: %+v", state)
	}
	if err := loadEffectiveManifest(cfg, state); err != nil {
		t.Fatalf("adapted stage no longer matches its immutable manifest: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(deploymentPath(cfg, state.DeploymentID), "stage", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	tun := doc["tun"].(map[string]any)
	dns := doc["dns"].(map[string]any)
	if dns["listen"] != "127.0.0.1:1053" ||
		!reflect.DeepEqual(tun["dns-hijack"], []any{}) ||
		!reflect.DeepEqual(tun["route-exclude-address"], []any{"127.0.0.0/8", "192.0.2.2/32"}) ||
		!reflect.DeepEqual(tun["route-address-set"], []any{"cf_gateway_fake_ip"}) ||
		!reflect.DeepEqual(dns["nameserver"], []any{"https://223.5.5.5/dns-query"}) {
		t.Fatalf("native AIO overrode gateway DNS protection or routing: %s", data)
	}
	if _, err := os.Stat(cfg.ConfigPath); !os.IsNotExist(err) {
		t.Fatalf("staging wrote live configuration: %v", err)
	}
}
