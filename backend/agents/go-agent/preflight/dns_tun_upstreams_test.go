package preflight

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func tunUpstreamDoc(t *testing.T, content string) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

const tunUpstreamOxiDNS = `plugins:
- tag: forward_local
  type: forward
  args:
    upstreams:
    - addr: udp://223.5.5.5
    - addr: 119.29.29.29
    - addr: tcp://[2400:3200::1]:53
    - addr: https://1.12.12.12/dns-query
    - addr: udp://dns.example.net:53
      dial_addr: 198.51.100.7
    - addr: udp://198.51.100.8:5353
    - addr: udp://127.0.0.1:1053
- tag: forward_remote
  type: forward
  args:
    upstreams:
    - addr: udp://8.8.8.8
    - {addr: "https://dns.example.org/dns-query", bootstrap: "198.51.100.9:53"}
- tag: forward_fallback
  type: forward
  args:
    upstreams: [{addr: tcp://1.1.1.1}]
- tag: rule_upstream
  type: forward
  args:
    upstreams: [{addr: udp://203.0.113.5, port: 53}, {addr: udp://203.0.113.6:5353, port: 53}]
- tag: not_forward
  type: sequence
  args: [{exec: accept}]
`

func TestTUNHijackedOxiDNSUpstreamsFollowsMihomoMatching(t *testing.T) {
	oxidns := tunUpstreamDoc(t, tunUpstreamOxiDNS)
	addresses := func(values ...string) []netip.Addr {
		var result []netip.Addr
		for _, value := range values {
			result = append(result, netip.MustParseAddr(value))
		}
		return result
	}
	all := addresses("119.29.29.29", "198.51.100.7", "198.51.100.9", "203.0.113.5", "203.0.113.6", "223.5.5.5", "2400:3200::1")
	for _, test := range []struct {
		hijack string
		want   []netip.Addr
	}{
		// The port in any:<port> is ignored: every destination port 53 is hijacked.
		{"[any:1053, tcp://any:1053]", all},
		{"[any:53]", all},
		{"['udp://[::]:53']", all},
		{"[223.5.5.5:53]", addresses("223.5.5.5")},
		{"[198.51.100.8:5353]", addresses("198.51.100.8")},
		{"[]", nil},
		{"null", nil},
		{"[not-an-address]", nil},
	} {
		t.Run(test.hijack, func(t *testing.T) {
			tun := tunUpstreamDoc(t, "enable: true\ndns-hijack: "+test.hijack+"\n")
			if got := TUNHijackedOxiDNSUpstreams(oxidns, tun); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("hijacked upstreams = %v, want %v", got, test.want)
			}
		})
	}
	// Mark mode (auto-redirect plus an address set) DNATs every local port 53
	// even with an empty dns-hijack; the Linux gateway always publishes it.
	for _, test := range []struct {
		name, tun string
		want      []netip.Addr
	}{
		{"gateway route-address-set", "{enable: true, auto-route: true, auto-redirect: true, dns-hijack: [], route-address-set: [cf_gateway_fake_ip]}", all},
		{"route-exclude-address-set", "{enable: true, auto-route: true, auto-redirect: true, route-exclude-address-set: [cn]}", all},
		{"explicit hijack dns-mode", "{enable: true, auto-route: true, auto-redirect: true, dns-mode: hijack, route-address-set: [a]}", all},
		{"full TUN without sets", "{enable: true, auto-route: true, auto-redirect: true, dns-hijack: []}", nil},
		{"sets without auto-redirect", "{enable: true, auto-route: true, dns-hijack: [], route-address-set: [a]}", nil},
		{"auto-redirect needs auto-route", "{enable: true, auto-redirect: true, route-address-set: [a]}", nil},
		{"native dns-mode", "{enable: true, auto-route: true, auto-redirect: true, dns-mode: native, route-address-set: [a]}", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := TUNHijackedOxiDNSUpstreams(oxidns, tunUpstreamDoc(t, test.tun)); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("hijacked upstreams = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDNSWarnsWhenRunningMihomoStillHijacksOxiDNSUpstream(t *testing.T) {
	oxidns := dnsTestCandidate("oxidns", "plugins:\n- {tag: server, type: udp_server, args: {listen: '0.0.0.0:53', entry: main}}\n- {tag: main, type: sequence, args: [{exec: '$forward_local'}, {exec: accept}]}\n- {tag: forward_local, type: forward, args: {concurrent: 2, upstreams: [{addr: 'udp://223.5.5.5'}, {addr: 'udp://119.29.29.29'}]}}\n", true)
	mihomo := func(tun string) *DNSCandidate {
		peer := dnsTestCandidate("mihomo", "dns: {enable: true, listen: '0.0.0.0:1053', nameserver: [223.5.5.5]}\ntun: "+tun+"\n", true)
		return &peer
	}
	for _, test := range []struct {
		name, tun string
		warn      bool
	}{
		{"user report", "{enable: true, dns-hijack: [any:1053], route-exclude-address: [127.0.0.0/8, 223.5.5.5/32]}", true},
		{"covered", "{enable: true, dns-hijack: [any:1053], route-exclude-address: [223.5.5.5/32, 119.29.0.0/16]}", false},
		{"tun disabled", "{enable: false, dns-hijack: [any:1053]}", false},
		{"no hijack", "{enable: true, dns-hijack: []}", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			report := dnsTestCheck(oxidns, mihomo(test.tun))
			if got := dnsHas(report, "warn", "119.29.29.29"); got != test.warn {
				t.Fatalf("warning = %v, want %v: %+v", got, test.warn, report)
			}
			for _, check := range report {
				if check.Name == "同机 TUN 劫持上游" && strings.Contains(check.Message, "223.5.5.5") {
					t.Fatalf("excluded upstream reported: %+v", check)
				}
			}
		})
	}
	gatewayTUN := "{enable: true, auto-route: true, auto-redirect: true, dns-hijack: [], route-address-set: [cf_gateway_fake_ip], route-exclude-address: [127.0.0.0/8, 192.168.1.2/32%s]}"
	gateway := func(extra string) *DNSCandidate {
		peer := dnsTestCandidate("mihomo", "dns: {enable: true, listen: '127.0.0.1:1053'}\ntun: "+fmt.Sprintf(gatewayTUN, extra)+"\nx-configflow-linux-gateway: {enabled: true}\n", true)
		return &peer
	}
	if report := dnsTestCheck(oxidns, gateway("")); !dnsHas(report, "warn", "119.29.29.29") || !dnsHas(report, "warn", "223.5.5.5") {
		t.Fatalf("running gateway still hijacks upstreams but no warning: %+v", report)
	}
	if report := dnsTestCheck(oxidns, gateway(", 223.5.5.5/32, 119.29.29.29/32")); dnsHas(report, "", "同机 TUN 劫持上游") {
		t.Fatalf("excluded gateway upstreams reported: %+v", report)
	}
	if report := dnsTestCheck(dnsTestCandidate("mihomo", "tun: {enable: true, dns-hijack: [any:53]}\n", true), &oxidns); dnsHas(report, "", "同机 TUN 劫持上游") {
		t.Fatalf("mihomo publications adapt instead of warning: %+v", report)
	}
}
