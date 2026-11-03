package routes

import (
	"agent/preflight"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func stubAIOPeerOxiDNS(t *testing.T, oxidns string) {
	t.Helper()
	metadata, _ := json.Marshal(preflight.Config{ServiceType: "oxidns", NativeAIO: true, ConfigPath: "/opt/configflow-agent/oxidns/config.yaml"})
	files := map[string][]byte{"/opt/configflow-agent/oxidns/config.json": metadata}
	if oxidns != "" {
		files["/opt/configflow-agent/oxidns/config.yaml"] = []byte(oxidns)
	}
	original := readAIOPeerDNS
	readAIOPeerDNS = func(name string) ([]byte, error) {
		if value, ok := files[name]; ok {
			return value, nil
		}
		return nil, fmt.Errorf("not found")
	}
	t.Cleanup(func() { readAIOPeerDNS = original })
}

const aioUpstreamOxiDNS = "plugins:\n- {tag: forward_local, type: forward, args: {concurrent: 2, upstreams: [{addr: 'udp://223.5.5.5'}, {addr: 'udp://119.29.29.29'}]}}\n- {tag: forward_remote, type: forward, args: {upstreams: [{addr: 'udp://127.0.0.1:1053'}]}}\n"

func aioExclusions(t *testing.T, content string) []string {
	t.Helper()
	var doc struct {
		TUN struct {
			Excluded []string `yaml:"route-exclude-address"`
		} `yaml:"tun"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.TUN.Excluded
}

func TestAIOUpstreamDNSExcludesHijackedOxiDNSUpstreams(t *testing.T) {
	stubAIOPeerOxiDNS(t, aioUpstreamOxiDNS)
	cfg := &Config{ServiceType: "mihomo", NativeAIO: true}
	// The reported configuration: dns-hijack any:1053 still hijacks port 53.
	input := "mode: rule\ntun:\n  enable: true\n  auto-redirect: true\n  dns-hijack: [any:1053, tcp://any:1053]\n  route-exclude-address: [127.0.0.0/8, 192.168.8.2/32, 223.5.5.5/32]\n"
	got, err := adaptAIOUpstreamDNS(cfg, input)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"127.0.0.0/8", "192.168.8.2/32", "223.5.5.5/32", "119.29.29.29/32"}
	if excluded := aioExclusions(t, got); !reflect.DeepEqual(excluded, want) {
		t.Fatalf("exclusions = %v, want %v", excluded, want)
	}
	if again, err := adaptAIOUpstreamDNS(cfg, got); err != nil || again != got {
		t.Fatalf("upstream exclusion is not idempotent: %v\n%s", err, again)
	}
	// Without auto-redirect the TUN still hijacks local port-53 traffic.
	got, err = adaptAIOUpstreamDNS(cfg, "tun: {enable: true, dns-hijack: [any:53]}\n")
	if err != nil {
		t.Fatal(err)
	}
	if excluded := aioExclusions(t, got); !reflect.DeepEqual(excluded, []string{"119.29.29.29/32", "223.5.5.5/32"}) {
		t.Fatalf("exclusions without auto-redirect = %v", excluded)
	}
}

func TestAIOUpstreamDNSLeavesOtherConfigurationsUnchanged(t *testing.T) {
	stubAIOPeerOxiDNS(t, aioUpstreamOxiDNS)
	for _, input := range []string{
		"mode: rule\n",
		"tun: {enable: false, dns-hijack: [any:53]}\n",
		"tun: {enable: true, dns-hijack: []}\n",
		"tun: {enable: true, dns-hijack: [any:53], route-exclude-address: [0.0.0.0/0]}\n",
		"tun: {enable: true, dns-hijack: [any:53]}\nx-configflow-linux-gateway: {enabled: true}\n",
	} {
		got, err := adaptAIOUpstreamDNS(&Config{ServiceType: "mihomo", NativeAIO: true}, input)
		if err != nil || got != input {
			t.Fatalf("configuration changed: %v\n%s", err, got)
		}
	}
	if _, err := adaptAIOUpstreamDNS(&Config{ServiceType: "mihomo", NativeAIO: true}, "tun: {enable: true, dns-hijack: [any:53], route-exclude-address: {}}\n"); err == nil {
		t.Fatal("invalid exclusion list accepted")
	}
	t.Setenv("ENABLE_OXIDNS", "false")
	input := "tun: {enable: true, dns-hijack: [any:53]}\n"
	if got, err := adaptAIOUpstreamDNS(&Config{ServiceType: "mihomo"}, input); err != nil || got != input {
		t.Fatalf("single-service mihomo was adapted: %v\n%s", err, got)
	}
	stubAIOPeerOxiDNS(t, "")
	if got, err := adaptAIOUpstreamDNS(&Config{ServiceType: "mihomo", NativeAIO: true}, input); err != nil || got != input {
		t.Fatalf("unreadable peer must leave the configuration unchanged: %v\n%s", err, got)
	}
}

func TestRenderFinalConfigIncludesAIOUpstreamExclusions(t *testing.T) {
	stubAgentIPv4(t, func() (string, error) { return "192.168.8.2", nil })
	stubAIOPeerOxiDNS(t, aioUpstreamOxiDNS)
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	got, err := renderFinalConfig(cfg, "mode: rule\ntun: {enable: true, auto-redirect: true, dns-hijack: [any:1053], route-exclude-address: [223.5.5.5/32]}\n")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"223.5.5.5/32", "127.0.0.0/8", "192.168.8.2/32", "119.29.29.29/32"}
	if excluded := aioExclusions(t, got); !reflect.DeepEqual(excluded, want) {
		t.Fatalf("final exclusions = %v, want %v", excluded, want)
	}
}
