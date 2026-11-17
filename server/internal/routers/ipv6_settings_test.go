package routers

import (
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"configflow/internal/model"
)

func TestR10SettingsAndYAMLBytesPython(t *testing.T) {
	inputs := []string{"", r10YAML, strings.ReplaceAll(r10YAML, "\n", "\r\n"), "# 原始文本\nipv6: true\ntun: {enable: true}\ndns: {enable: true, ipv6: true, enhanced-mode: fake-ip, fake-ip-range6: 'fd00:face::1/64'}\n", "ipv6: false\n", "ipv6: 'true'\n", "dns: []\n", "[]\n", "dns: {ipv6: 'false'}\n", strings.ReplaceAll(r10YAML, "  ipv6: true", "  ipv6: false"), strings.ReplaceAll(r10YAML, "  enhanced-mode: fake-ip", "  enhanced-mode: redir-host"), strings.ReplaceAll(r10YAML, "  fake-ip-range6: fd00:face::1/64\n", ""), strings.ReplaceAll(r10YAML, "tun:\n  enable: true", "tun:\n  enable: false"), "tun: &t {enable: true}\ndns: {enable: true, ipv6: true, enhanced-mode: fake-ip, fake-ip-range6: fd00:face::1/64}\n"}
	configs := []*model.Profile{}
	for _, raw := range inputs {
		configs = append(configs, r10Config(raw))
	}
	raw := r09Python(t, "r10_settings_reference.py", configs, "")
	var expected []json.RawMessage
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err, string(raw))
	}
	for i, c := range configs {
		t.Run(hex.EncodeToString([]byte(inputs[i]))[:min(32, len(inputs[i])*2)], func(t *testing.T) {
			before, _ := json.Marshal(c)
			got := struct {
				Fake    IPv6Settings      `json:"fake"`
				Routes  IPv6RouteSettings `json:"routes"`
				YAMLHex string            `json:"yaml_hex"`
			}{IPv6FakeIPSettings(c), IPv6RoutesSettings(c), hex.EncodeToString([]byte(c.Mihomo.CustomConfig))}
			// Python's internal IPv6Network(None) text is deliberately replaced
			// by a clear Chinese validation error (PORTING native-parser rule).
			if strings.Contains(inputs[i], "enhanced-mode: fake-ip") && !strings.Contains(inputs[i], "fake-ip-range6:") {
				if got.Fake.Error != messageIPv6FakeSettings+messageIPv6Range {
					t.Fatal("missing range not rejected", got.Fake)
				}
				var ref struct {
					Fake    IPv6Settings      `json:"fake"`
					Routes  IPv6RouteSettings `json:"routes"`
					YAMLHex string            `json:"yaml_hex"`
				}
				_ = json.Unmarshal(expected[i], &ref)
				if !strings.HasPrefix(ref.Fake.Error, messageIPv6FakeSettings) {
					t.Fatal("Python missing range was not rejected")
				}
				ref.Fake.Error = ""
				got.Fake.Error = ""
				expected[i], _ = json.Marshal(ref)
			}
			a, b := r08Comparable(t, expected[i]), r08Comparable(t, got)
			if !reflect.DeepEqual(a, b) {
				t.Error(r08Difference(a, b, "$"))
			}
			after, _ := json.Marshal(c)
			if string(before) != string(after) || c.Mihomo.CustomConfig != inputs[i] {
				t.Fatal("read-only settings rewrote YAML bytes")
			}
		})
	}
}

func FuzzR10IPv6Input(f *testing.F) {
	for _, seed := range []string{`null`, `{}`, `{"version":1,"spec":{"cidr":"fd00::/64","gateway":"fe80::4%bridge"},"resources":[]}`, `{"version":1,"gateway":"fe80::4%bridge","resources":[{"id":"*1","path":"/ipv6/route","config":{}}]}`, `{"mihomo":{"custom_config":"ipv6: true"}}`, r10YAML} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<18 {
			return
		}
		_, _ = ipv6Owned(data, IPv6Snapshot{"routes": {}}, true)
		_, _ = ipv6Owned(data, IPv6Snapshot{"routes": {}}, false)
		config := r10Config(string(data))
		_ = IPv6FakeIPSettings(config)
		_ = IPv6RoutesSettings(config)
		var p model.Profile
		if json.Unmarshal(data, &p) == nil {
			_ = IPv6FakeIPSettings(&p)
			_ = IPv6RoutesSettings(&p)
		}
		var row Row
		if json.Unmarshal(data, &row) == nil {
			_ = StableIPv6Row(row)
		}
	})
}

func TestR10IPv6AddressMathPython(t *testing.T) {
	rows := []Row{}
	for _, address := range []string{"fd00:1::4", "fd00:1::4/64", "fd00:1::4/064", "fe80::4", "fe80::4%eth0", "fe80::4%eth0/64", "fe80::4%eth0/128", "::ffff:192.0.2.4/128", "2001:db8:1::4/64", "invalid"} {
		rows = append(rows, Row{"address": address, "actual-interface": "bridge", "valid": "2h", "preferred": "1h", "dynamic": "true"})
	}
	raw := r09Python(t, "r10_address_reference.py", rows, "")
	var expected []Row
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err, string(raw))
	}
	for i, row := range rows {
		if got := StableIPv6Row(row); !reflect.DeepEqual(got, expected[i]) {
			t.Errorf("%s: got=%v Python=%v", row.text("address"), got, expected[i])
		}
	}
	for _, text := range []string{"fe80::4%eth0", "fe80::4%eth0/64", "fe80::4%eth0/128"} {
		if _, ok := ipv6Network(text); ok {
			t.Error("scoped route destination silently changed", text)
		}
	}
	for _, text := range []string{"fd00:1::4/64", "fd00:1::4/064"} {
		if p, ok := ipv6Network(text); !ok || p.String() != "fd00:1::/64" {
			t.Error("host bits were not masked", text, p)
		}
	}
	live := IPv6Snapshot{"addresses": {{"address": "fd00:1::1/64", "interface": "bridge"}}}
	if gateway := IPv6Gateway(IPv6Host{Addresses: []IPv6HostAddress{{Address: "fe80::4%eth0"}}}, live, "bridge", nil); gateway != "" {
		t.Error("double-scoped next hop produced", gateway)
	}
}
