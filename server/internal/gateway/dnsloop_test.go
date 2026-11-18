package gateway

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/yamlx"
)

func TestDNSLoopMatchesPython(t *testing.T) {
	type probe struct {
		DNS   json.RawMessage `json:"dns"`
		Hosts []string        `json:"hosts"`
	}
	tests := []probe{}
	hosts := []string{"192.0.2.1", "2001:db8::1"}
	upstreams := []string{"example.com", "192.0.2.53", "2001:db8::53", "127.0.0.1", "127.0.0.2", "0.0.0.0", "localhost", "LOCALHOST", "[::1]", "[::]", "[::ffff:127.0.0.1]", "192.0.2.1", "192.0.2.1:53", "192.0.2.1:1053", "192.0.2.1:5353", "[2001:db8::1]:53", "[2001:db8::1]:1053", "[2001:db8::1]:5353", "system", "system://", "system#DIRECT", "example.com:bad", "[broken", "192.0.2.1:65536", "127.0.0.1:65536", "https://user:credential@localhost/dns-query", "", "udp://", "https://[::1]/dns-query"}
	for _, scheme := range []string{"udp", "tcp", "tls", "https", "quic"} {
		for _, host := range []string{"127.0.0.1", "localhost", "192.0.2.1", "192.0.2.1:53", "192.0.2.1:853", "[::1]", "[2001:db8::1]:1053", "example.com:443"} {
			upstreams = append(upstreams, scheme+"://"+host)
		}
	}
	for _, field := range []string{"nameserver", "fallback", "default-nameserver", "proxy-server-nameserver", "direct-nameserver", "nameserver-policy", "proxy-server-nameserver-policy"} {
		for _, upstream := range upstreams {
			var value any = []any{upstream}
			if field == "nameserver-policy" || field == "proxy-server-nameserver-policy" {
				value = map[string]any{"example.com": []any{"192.0.2.53", upstream}}
			}
			data, _ := json.Marshal(map[string]any{field: value})
			tests = append(tests, probe{data, hosts})
		}
	}
	for _, raw := range []string{`null`, `[]`, `{"nameserver":[false,2,null,["192.0.2.53","localhost"]]}`, `{"nameserver":["localhost","127.0.0.2"],"fallback":["127.0.0.1"]}`, `{"nameserver-policy":{"a":["localhost"],"b":["127.0.0.2"]}}`} {
		tests = append(tests, probe{[]byte(raw), hosts})
	}
	input, _ := json.Marshal(tests)
	home, _ := os.UserHomeDir()
	cmd := exec.Command(filepath.Join(home, "cf-test/.venv/bin/python"), "-c", `import json,sys,ipaddress;from backend.common.gateway_onboarding import mihomo_dns_loop;json.dump([mihomo_dns_loop(v['dns'],[ipaddress.ip_address(h) for h in v['hosts']]) for v in json.load(sys.stdin)],sys.stdout)`)
	cmd.Dir = "../../.."
	cmd.Stdin = bytes.NewReader(input)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Python: %v %s", err, data)
	}
	var expected [][]string
	if err = json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	addresses := []netip.Addr{netip.MustParseAddr(hosts[0]), netip.MustParseAddr(hosts[1])}
	for i, test := range tests {
		node, err := yamlx.FromJSON(test.DNS)
		if err != nil {
			t.Fatal(err)
		}
		result := MihomoDNSLoop(node, addresses)
		var got []string
		if result != nil {
			got = []string{result.Key, result.Upstream}
		}
		if !reflect.DeepEqual(got, expected[i]) {
			t.Errorf("case %d %s: Go=%v Python=%v", i, test.DNS, got, expected[i])
		}
	}
	t.Logf("pure DNS loop Python comparisons=%d", len(tests))
}
