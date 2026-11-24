package ipv6

import (
	"configflow/internal/jsonresp"
	"encoding/json"
	"testing"
)

func TestConfigurationAndTargets(t *testing.T) {
	sources := []string{"", "ipv6: false\ndns: {ipv6: true}\n", "ipv6: true\ndns: {enable: true, enhanced-mode: fake-ip, ipv6: true, fake-ip-range6: 'fd00:abcd::1234/64'}\ntun: {enable: true}\n", "ipv6: 'false'\ndns: {ipv6: 'true'}\n", "dns: {enable: true, enhanced-mode: fake-ip, fake-ip-range6: '2001:db8::1/64'}\n", "dns: null\n", "dns: [", "[true]", "dns: {fake-ip-range6: ''}", "dns: {fake-ip-range6: false}", "dns: {fake-ip-range6: 'bad'}", "proxies: [{name: inline, type: ss, server: '2001:db8::2', port: 443}]\n"}
	for _, source := range sources {
		config := Document{"mihomo": jsonresp.Value(map[string]string{"custom_config": source}), "nodes": jsonresp.Value([]any{nil, true, map[string]any{"type": "ss", "server": "2001:db8::3", "port": 443}, map[string]any{"type": "ss", "server": "192.0.2.3", "port": 443}, map[string]any{"type": "trojan", "server": "node.example.com", "port": 443}, map[string]any{"type": "vless", "server": "2001:db8::4", "port": 443, "dialer-proxy": "previous"}, map[string]any{"type": "ss", "server": "2001:db8::5", "port": 443, "enabled": false}})}
		settings, doc := ReadConfiguration(config)
		result := jsonresp.Value(map[string]any{"configuration": settings, "targets": ProbeTargets(config, doc)})
		if !json.Valid(result) {
			t.Fatal("invalid configuration JSON")
		}
	}
}
func TestSavedIPv6PrefixNormalization(t *testing.T) {
	settings, _ := ReadConfiguration(Document{"mihomo": jsonresp.Value(map[string]string{"custom_config": "dns: {enable: true, enhanced-mode: fake-ip, ipv6: true, fake-ip-range6: 'fd00:abcd::1234/64'}"})})
	if text(settings.FakeIPv6Range) != "fd00:abcd::/64" || !settings.FakeIPv6Enabled || len(settings.Errors) != 0 {
		t.Fatal("R11 received noncanonical IPv6 prefix", settings)
	}
}
func FuzzSavedIPv6Configuration(f *testing.F) {
	f.Add("ipv6: true\ndns: {ipv6: false}")
	f.Add("dns: [")
	f.Fuzz(func(t *testing.T, source string) {
		if len(source) > 65536 {
			return
		}
		settings, doc := ReadConfiguration(Document{"mihomo": jsonresp.Value(map[string]string{"custom_config": source})})
		if !json.Valid(jsonresp.Value(settings)) || doc == nil {
			t.Fatal("configuration parse failed to produce diagnostic")
		}
		for _, target := range ProbeTargets(Document{}, doc) {
			if target.Port < 1 || target.Port > 65535 || target.Host == "" {
				t.Fatal("invalid probe target")
			}
		}
	})
}

func TestMalformedNodeDoesNotConsumeDirectProbeSlots(t *testing.T) {
	_, doc := ReadConfiguration(Document{})
	config := Document{"nodes": json.RawMessage(`[{"type":"ss","server":"[]","port":443},{"type":"ss","server":"","port":443},{"type":"ss","server":"indirect.example","port":443,"dialer-proxy":true},{"type":"ss","server":"2001:db8::2","port":443}]`)}
	targets := ProbeTargets(config, doc)
	if len(targets) != 1 || targets[0].Host != "2001:db8::2" {
		t.Fatal("bad node hid healthy direct endpoint", targets)
	}
}
