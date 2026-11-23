package routes

import (
	"errors"
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGatewayDNSPreservesOxiDNSAndOrdinaryMode(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "", errors.New("no LAN interface") })
	input := "mode: rule\ndns: {enable: true, listen: '0.0.0.0:1053'}\ntun: {enable: true, auto-redirect: true, dns-hijack: [any:1053]}\n"
	cfg := &Config{AgentIP: "192.0.2.2"}
	ordinary, err := adaptGatewayDNS(cfg, input)
	if err != nil || ordinary != input {
		t.Fatalf("ordinary publication changed: %v", err)
	}
	stubAgentIPv4(t, func() (string, error) { return "192.0.2.2", nil })
	input += "x-configflow-linux-gateway: {enabled: true, version: 1}\n"
	got, err := adaptGatewayDNS(cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatal(err)
	}
	tun := doc["tun"].(map[string]any)
	excluded := tun["route-exclude-address"].([]any)
	if len(excluded) != 2 || excluded[0] != "127.0.0.0/8" || excluded[1] != "192.0.2.2/32" || len(tun["dns-hijack"].([]any)) != 0 || doc["dns"].(map[string]any)["listen"] != "127.0.0.1:1053" {
		t.Fatalf("gateway DNS not protected: %s", got)
	}
	again, err := adaptGatewayDNS(cfg, got)
	if err != nil || again != got {
		t.Fatalf("gateway adaptation is not idempotent: %v", err)
	}
	stubAgentIPv4(t, func() (string, error) { return "", errors.New("no LAN interface") })
	for _, address := range []string{"", "0.0.0.0", "127.0.0.1", "::1", "example.com"} {
		if _, err := adaptGatewayDNS(&Config{AgentIP: address}, input); err == nil {
			t.Fatalf("accepted invalid gateway address %q", address)
		}
	}
}

func TestLinuxGatewayRejectsOldDNSHijackingPublication(t *testing.T) {
	f := newGatewayFixture(t)
	data, err := os.ReadFile(f.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.ReplaceAll(string(data), "  route-exclude-address: [127.0.0.0/8, 192.168.1.2/32]\n", ""))
	if err := os.WriteFile(f.cfg.ConfigPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil || !strings.Contains(err.Error(), "OxiDNS") {
		t.Fatalf("unsafe DNS publication accepted: %v", err)
	}
	if f.mutations != 0 {
		t.Fatal("blocked DNS configuration changed firewall")
	}
}
