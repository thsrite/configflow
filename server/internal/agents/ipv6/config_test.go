package ipv6

import (
	"bufio"
	"bytes"
	"configflow/internal/jsonresp"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestConfigurationAndTargetsPython(t *testing.T) {
	sources := []string{"", "ipv6: false\ndns: {ipv6: true}\n", "ipv6: true\ndns: {enable: true, enhanced-mode: fake-ip, ipv6: true, fake-ip-range6: 'fd00:abcd::1234/64'}\ntun: {enable: true}\n", "ipv6: 'false'\ndns: {ipv6: 'true'}\n", "dns: {enable: true, enhanced-mode: fake-ip, fake-ip-range6: '2001:db8::1/64'}\n", "dns: null\n", "dns: [", "[true]", "dns: {fake-ip-range6: ''}", "dns: {fake-ip-range6: false}", "dns: {fake-ip-range6: 'bad'}", "proxies: [{name: inline, type: ss, server: '2001:db8::2', port: 443}]\n"}
	input := bytes.Buffer{}
	actual := []json.RawMessage{}
	for _, source := range sources {
		config := Document{"mihomo": jsonresp.Value(map[string]string{"custom_config": source}), "nodes": jsonresp.Value([]any{nil, true, map[string]any{"type": "ss", "server": "2001:db8::3", "port": 443}, map[string]any{"type": "ss", "server": "192.0.2.3", "port": 443}, map[string]any{"type": "trojan", "server": "node.example.com", "port": 443}, map[string]any{"type": "vless", "server": "2001:db8::4", "port": 443, "dialer-proxy": "previous"}, map[string]any{"type": "ss", "server": "2001:db8::5", "port": 443, "enabled": false}})}
		// Python's old helper crashes on malformed node rows in some upstream callers;
		// this one already isolates them, and the Go fixture preserves healthy peers.
		settings, doc := ReadConfiguration(config)
		actual = append(actual, jsonresp.Value(map[string]any{"configuration": settings, "targets": ProbeTargets(config, doc)}))
		input.Write(jsonresp.Value(config))
		input.WriteByte('\n')
	}
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		for _, result := range actual {
			if !json.Valid(result) {
				t.Fatal("invalid configuration JSON")
			}
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "testdata/config_reference.py")
	root, _ := filepath.Abs("../../../..")
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1")
	cmd.Stdin = &input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err, stderr.String())
	}
	scanner := bufio.NewScanner(bytes.NewReader(out))
	i := 0
	for scanner.Scan() {
		if i >= len(actual) {
			t.Fatal("extra Python row")
		}
		var a, b any
		if err = json.Unmarshal(actual[i], &a); err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(scanner.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("case %d differs\nGo %s\nPython %s", i, actual[i], scanner.Bytes())
		}
		i++
	}
	if i != len(actual) {
		t.Fatal("missing Python row", i, stderr.String())
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
