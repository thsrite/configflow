package onboarding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"configflow/internal/yamlx"
)

func TestFormShapesAndLimits(t *testing.T) {
	valid := testOperation().Body
	for _, raw := range []string{"null", "[]", "true", "0", `"x"`, `{}`, `{"rule_set_ids":null}`, `{"rule_set_ids":[null]}`} {
		if _, err := ParseRequest(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	for _, tc := range []struct {
		key   string
		value json.RawMessage
	}{{"rule_set_ids", encode(strings.Repeat("x", 5))}, {"rule_set_ids", encode([]string{""})}, {"rule_set_ids", encode([]string{strings.Repeat("界", 257)})}, {"rule_set_ids", encode([]string{"id\x7f"})}, {"direct_rule_set_ids", encode(make([]string, 1025))}, {"query_recorder", encode("true")}, {"query_recorder", json.RawMessage("null")}, {"default_forward", json.RawMessage("null")}, {"confirm_template_conversion", encode(1)}, {"expected_config_revision", json.RawMessage("1.0")}, {"expected_config_revision", encode(-1)}, {"unknown", encode(true)}, {"local_dns", encode("https://192.0.2.53:00053/dns-query")}, {"local_dns", encode("tls://192.0.2.53:01053")}} {
		t.Run(tc.key+string(tc.value)[:min(12, len(tc.value))], func(t *testing.T) {
			body := object(valid)
			body[tc.key] = tc.value
			if _, err := ParseRequest(encode(body)); err == nil {
				t.Fatal("accepted invalid form")
			}
		})
	}
	body := object(valid)
	body["rule_set_ids"] = encode([]string{"a", "a", "b"})
	form, err := ParseRequest(encode(body))
	if err != nil || len(form.RuleSetIDs) != 2 {
		t.Fatal("deduplication", form, err)
	}
}
func TestNativeGraphSafety(t *testing.T) {
	chain := "leaf: &a [x]\n"
	for i := 0; i < 40; i++ {
		previous := "a"
		if i > 0 {
			previous = fmt.Sprintf("n%d", i-1)
		}
		chain += fmt.Sprintf("n%d: &n%d [*%s, *%s]\n", i, i, previous, previous)
	}
	for _, source := range []string{"plugins: &x [*x]\n", "plugins: []\nplugins: []\n", chain} {
		n := yamlx.Map(yamlx.Str("config_mode"), yamlx.Str("native"), yamlx.Str("native_config"), yamlx.Str(source))
		if err := CheckNativeDNSPath(n, true); err == nil {
			t.Fatal("unsafe graph accepted")
		}
		if _, err := PrepareNativeFallback(n); err == nil {
			t.Fatal("unsafe graph restored")
		}
	}
}
func TestUnavailableSwitchCannotProveSourceReadiness(t *testing.T) {
	config := Document{"mihomo": encode(map[string]string{"custom_config": "mixed-port: 7890\n"}), "proxy_groups": json.RawMessage(`[{"name":"Proxy","enabled":null}]`), "nodes": json.RawMessage(`[{"name":"Example","enabled":null}]`)}
	prerequisites, err := proxyPrerequisites(config, nil)
	if err != nil || prerequisites.Ready {
		t.Fatalf("invalid enablement ready=%v err=%v", prerequisites.Ready, err)
	}
	for _, raw := range []string{`[null]`, `[{"enabled":{}}]`, `[[]]`, `[1]`, `["x"]`} {
		config["rule_configs"] = json.RawMessage(raw)
		_, _, _ = readConfig(config)
	}
}

func FuzzGatewayOnboarding(f *testing.F) {
	f.Add(string(testOperation().Body), "plugins: []\n")
	f.Add("null", "plugins: &x [*x]\n")
	f.Add(`{"rule_set_ids":[null]}`, "plugins: []\nplugins: []\n")
	f.Fuzz(func(t *testing.T, body, native string) {
		if len(body) > 32768 || len(native) > 8192 {
			return
		}
		form, err := ParseRequest(json.RawMessage(body))
		if err == nil {
			if _, err := ParseRequest(encode(form)); err != nil {
				t.Fatalf("validated form does not round trip %v", err)
			}
		}
		if document, err := yamlx.Parse([]byte(native)); err == nil {
			_, _ = PrepareMihomoDNS(document, nil, noLoop)
		}
		config := Document{"mihomo": encode(map[string]string{"custom_config": "mixed-port: 7890\n"}), "oxidns": encode(map[string]string{"config_mode": "native", "native_config": native})}
		before := string(encode(config))
		_, _ = ReadState(config, 1, Options{Takeover: "selective"})
		if string(encode(config)) != before {
			t.Fatal("state mutated input")
		}
	})
}

func TestMalformedStoredMihomoIsNotRepaired(t *testing.T) {
	form, err := ParseRequest(testOperation().Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"null", "[]", "false", "1"} {
		config := Document{"mihomo": json.RawMessage(raw)}
		before := string(encode(config))
		if _, _, err := Prepare(context.Background(), config, form, Options{DNSLoop: noLoop}); err == nil {
			t.Fatalf("malformed section accepted: %s", raw)
		}
		if before != string(encode(config)) {
			t.Fatal("source changed")
		}
	}
}

func TestRouterPlainDNSRejectsLocalListeners(t *testing.T) {
	local := []netip.Addr{netip.MustParseAddr("192.0.2.10")}
	for _, input := range []string{"udp://127.0.0.1:53", "tcp://192.0.2.10:53", "192.0.2.53:1053", "tcp://192.0.2.53:70000", "addr: udp://192.0.2.53\ndial_addr: 192.0.2.10"} {
		if _, err := localUpstreams(input, true, local); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}
