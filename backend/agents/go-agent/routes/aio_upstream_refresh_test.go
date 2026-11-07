package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const refreshGatewayTUN = "tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: [], route-address-set: [cf_gateway_fake_ip], route-exclude-address: [127.0.0.0/8, 192.168.8.2/32%s]}\n"

func refreshDoc(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestNewlyExcludedOxiDNSUpstreams(t *testing.T) {
	oxidns := refreshDoc(t, aioUpstreamOxiDNS)
	excluded := fmt.Sprintf(refreshGatewayTUN, ", 119.29.29.29/32, 223.5.5.5/32")
	for _, test := range []struct {
		name, previous, current string
		want                    []netip.Addr
	}{
		{"gateway upgrade", fmt.Sprintf(refreshGatewayTUN, ""), excluded,
			[]netip.Addr{netip.MustParseAddr("119.29.29.29"), netip.MustParseAddr("223.5.5.5")}},
		{"one new upstream", fmt.Sprintf(refreshGatewayTUN, ", 223.5.5.5/32"), excluded, []netip.Addr{netip.MustParseAddr("119.29.29.29")}},
		{"already excluded", excluded, excluded, nil},
		// Without a previous hijack there is no stale DNAT to clear.
		{"previous TUN off", "tun: {enable: false}\n", excluded, nil},
		{"no previous config", "", excluded, nil},
		{"previous full TUN without hijack", "tun: {enable: true, auto-route: true, auto-redirect: true, dns-hijack: []}\n", excluded, nil},
		{"still hijacked", fmt.Sprintf(refreshGatewayTUN, ""), fmt.Sprintf(refreshGatewayTUN, ""), nil},
		{"TUN turned off", fmt.Sprintf(refreshGatewayTUN, ""), "tun: {enable: false, route-exclude-address: [0.0.0.0/0]}\n", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := newlyExcludedOxiDNSUpstreams(refreshDoc(t, test.current), refreshDoc(t, test.previous), oxidns)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("newly excluded = %v, want %v", got, test.want)
			}
		})
	}
}

// testAIOPeerReload runs a fake OxiDNS control API for a same-host OxiDNS whose
// configuration lives on disk, and points the Mihomo Agent's peer lookup at it.
func testAIOPeerReload(t *testing.T) (*Config, *fakeReloadCore) {
	t.Helper()
	cfg := testDeploymentConfig(t)
	cfg.NativeAIO = true
	oxidnsCfg := &Config{ServiceType: "oxidns", ConfigPath: filepath.Join(t.TempDir(), "oxidns.yaml")}
	core := &fakeReloadCore{cfg: oxidnsCfg}
	server := httptest.NewServer(http.HandlerFunc(core.serve))
	t.Cleanup(server.Close)
	live := fmt.Sprintf("api:\n  http:\n    listen: %s\n    auth: {type: basic, username: tester, password: secret}\n%s",
		strings.TrimPrefix(server.URL, "http://"), aioUpstreamOxiDNS)
	writeActivationFixture(t, oxidnsCfg.ConfigPath, []byte(live), 0600)
	core.running = sha256Hex([]byte(live))
	metadata, _ := json.Marshal(map[string]interface{}{"service_type": "oxidns", "native_aio": true, "config_path": oxidnsCfg.ConfigPath})
	original := readAIOPeerDNS
	readAIOPeerDNS = func(name string) ([]byte, error) {
		if name == "/opt/configflow-agent/oxidns/config.json" {
			return metadata, nil
		}
		return os.ReadFile(name)
	}
	t.Cleanup(func() { readAIOPeerDNS = original })
	return cfg, core
}

func TestRefreshAIOPeerUpstreamsReloadsOxiDNSOnlyForNewExclusions(t *testing.T) {
	cfg, core := testAIOPeerReload(t)
	previous := []byte(fmt.Sprintf(refreshGatewayTUN, ""))
	current := []byte(fmt.Sprintf(refreshGatewayTUN, ", 119.29.29.29/32, 223.5.5.5/32"))
	writeActivationFixture(t, cfg.ConfigPath, current, 0600)

	refreshAIOPeerUpstreams(cfg, previous)
	if core.reloads != 1 {
		t.Fatalf("reloads after new exclusions = %d, want 1", core.reloads)
	}
	// Republishing the same exclusions must not clear the OxiDNS cache again.
	refreshAIOPeerUpstreams(cfg, current)
	if core.reloads != 1 {
		t.Fatalf("reloads after unchanged exclusions = %d, want 1", core.reloads)
	}
	// OxiDNS running something other than its on-disk file is its own Agent's
	// business; a reload here would activate that file.
	core.running = "other"
	refreshAIOPeerUpstreams(cfg, previous)
	if core.reloads != 1 {
		t.Fatalf("reloaded an OxiDNS that runs another version: %d", core.reloads)
	}
	core.running = sha256Hex(mustRead(t, core.cfg.ConfigPath))
	// Single-service Mihomo has no same-host OxiDNS to reload.
	t.Setenv("ENABLE_OXIDNS", "false")
	refreshAIOPeerUpstreams(&Config{ServiceType: "mihomo", ConfigPath: cfg.ConfigPath}, previous)
	if core.reloads != 1 {
		t.Fatalf("single-service Mihomo reloaded OxiDNS: %d", core.reloads)
	}
}

// The real release path: a gateway Mihomo publication that newly excludes the
// OxiDNS upstreams reloads the same-host OxiDNS once it has succeeded.
func TestGatewayMihomoPublishReloadsSameHostOxiDNS(t *testing.T) {
	cfg, core := testAIOPeerReload(t)
	stubAgentIPv4(t, func() (string, error) { return "192.168.8.2", nil })
	originalVersion := gatewayCoreVersion
	t.Cleanup(func() { gatewayCoreVersion = originalVersion })
	gatewayCoreVersion = func(string) (string, error) { return "Mihomo Meta v1.19.32 linux amd64", nil }
	gateway := "mode: rule\ndns: {enable: true, enhanced-mode: fake-ip, listen: 127.0.0.1:1053}\n" +
		fmt.Sprintf(refreshGatewayTUN, "") + "x-configflow-linux-gateway: {enabled: true, version: 1}\n"
	writeActivationFixture(t, cfg.ConfigPath, []byte(gateway), 0600)
	writeActivationFixture(t, filepath.Join(filepath.Dir(cfg.ConfigPath), "running"), nil, 0600)

	state := submitBundle(t, cfg, "gateway-refresh", map[string][]byte{"config.yaml": []byte(gateway + "# republish\n")}, true)
	if state.Status != "succeeded" {
		t.Fatalf("gateway publish failed: %+v", state)
	}
	if excluded := aioExclusions(t, string(mustRead(t, cfg.ConfigPath))); !reflect.DeepEqual(excluded, []string{"127.0.0.0/8", "192.168.8.2/32", "119.29.29.29/32", "223.5.5.5/32"}) {
		t.Fatalf("live exclusions = %v", excluded)
	}
	if core.reloads != 1 {
		t.Fatalf("OxiDNS reloads after the gateway publish = %d, want 1", core.reloads)
	}
	state = submitBundle(t, cfg, "gateway-again", map[string][]byte{"config.yaml": []byte(gateway + "# again\n")}, true)
	if state.Status != "succeeded" || core.reloads != 1 {
		t.Fatalf("unchanged exclusions reloaded OxiDNS again: %+v reloads=%d", state, core.reloads)
	}
}

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
