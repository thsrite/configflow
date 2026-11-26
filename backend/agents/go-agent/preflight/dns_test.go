package preflight

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeDNSQuery struct {
	calls []portBinding
	names []string
	err   error
}

func (p *fakeDNSQuery) Query(_ context.Context, b portBinding, name string) error {
	p.calls = append(p.calls, b)
	p.names = append(p.names, name)
	return p.err
}
func dnsTestCandidate(service, content string, aio bool) DNSCandidate {
	return DNSCandidate{Config: Config{ServiceType: service, ServiceUnit: service, ServiceBinary: service, ConfigPath: "/etc/" + service + "/live.yaml", NativeAIO: aio}, Content: []byte(content)}
}
func dnsHas(report Report, level, text string) bool {
	for _, check := range report {
		if (level == "" || check.Level == level) && strings.Contains(check.Name+check.Message, text) {
			return true
		}
	}
	return false
}
func dnsTestCheck(candidate DNSCandidate, peer *DNSCandidate) Report {
	return checkDNSWithProbe(candidate, DNSOptions{Peer: peer}, fakePortProbe{}, &fakeDNSQuery{}, "linux")
}
func dnsOxiConfig(listen, upstream string, conditional bool) string {
	condition := ""
	if conditional {
		condition = "matches: ['qname example.com'], "
	}
	return "plugins:\n- {tag: server, type: udp_server, args: {listen: '" + listen + "', entry: main}}\n- {tag: main, type: sequence, args: [{" + condition + "exec: '$upstream'}, {exec: accept}]}\n- {tag: upstream, type: forward, args: {upstreams: [{addr: '" + upstream + "'}]}}\n"
}

func TestDNSCheckDoesNotRequireTUNNodesOrAgentPorts(t *testing.T) {
	candidate := dnsTestCandidate("mihomo", "mixed-port: 7890\ndns: {enable: false}\ntun: {enable: false}\n", false)
	candidate.Config.AgentPort = 53
	report := checkDNSWithProbe(candidate, DNSOptions{Probe: true}, fakePortProbe{listeners: []portListener{portTestListener(":53", "tcp", portProcess{PID: 12, Exe: "/usr/bin/foreign"})}}, &fakeDNSQuery{}, "linux")
	if report.HasErrors() || !dnsHas(report, "pass", "不适用") {
		t.Fatalf("disabled DNS incorrectly checked unrelated requirements: %+v", report)
	}
	for _, check := range report {
		if strings.Contains(check.Name, "TUN") || strings.Contains(check.Name, "IPv4") || strings.Contains(check.Name, "Agent") {
			t.Fatalf("environment leaked into DNS preflight: %+v", report)
		}
	}
	candidate = dnsTestCandidate("mihomo", "dns: {enable: true, listen: '0.0.0.0:5353', nameserver: [1.1.1.1]}\n", true)
	report = dnsTestCheck(candidate, nil)
	if report.HasErrors() || !dnsHas(report, "pass", "udp/5353") || dnsHas(report, "", "1053") {
		t.Fatalf("final YAML custom port was substituted or nodes required: %+v", report)
	}
}

func TestDNSCheckManagedOwnerUsesLivePathAndProbeBoundaries(t *testing.T) {
	candidate := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053', nameserver: [1.1.1.1]}", false)
	live := portTestCore(candidate.Config)
	for _, test := range []struct {
		name   string
		owners []portProcess
		fail   bool
	}{{"managed", []portProcess{live}, false}, {"foreign", []portProcess{{PID: 123, Exe: "/usr/bin/dnsmasq"}}, true}, {"unknown", nil, true}, {"mixed", []portProcess{live, {PID: 321, Exe: "/usr/bin/other"}}, true}} {
		t.Run(test.name, func(t *testing.T) {
			query := &fakeDNSQuery{}
			report := checkDNSWithProbe(candidate, DNSOptions{Probe: true}, fakePortProbe{listeners: []portListener{portTestListener(":1053", "udp", test.owners...)}}, query, "linux")
			if report.HasErrors() != test.fail {
				t.Fatalf("ownership result: %+v", report)
			}
			if test.fail && len(query.calls) > 0 {
				t.Fatal("queried unknown/foreign listener")
			}
			if !test.fail && (len(query.calls) != 1 || query.names[0] != "example.com" || !query.calls[0].Address.IsLoopback()) {
				t.Fatalf("did not query owned actual address: %+v", query)
			}
		})
	}
	query := &fakeDNSQuery{err: errors.New("private credentials should not leak")}
	report := checkDNSWithProbe(candidate, DNSOptions{Probe: true}, fakePortProbe{listeners: []portListener{portTestListener(":1053", "udp", live)}}, query, "linux")
	if report.HasErrors() || !dnsHas(report, "warn", "查询失败") || dnsHas(report, "", "private credentials") {
		t.Fatalf("probe failure should be sanitized nonblocking warning: %+v", report)
	}
	query = &fakeDNSQuery{}
	report = checkDNSWithProbe(candidate, DNSOptions{Probe: true}, fakePortProbe{}, query, "linux")
	if report.HasErrors() || len(query.calls) != 0 || !dnsHas(report, "warn", "待生效") {
		t.Fatalf("new listeners should wait without failed probe: %+v", report)
	}
}

func TestDNSCheckUnavailableVisibilityWarns(t *testing.T) {
	candidate := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053'}", false)
	for _, platform := range []string{"darwin", "linux"} {
		report := checkDNSWithProbe(candidate, DNSOptions{Probe: true}, fakePortProbe{err: errors.New("no proc")}, &fakeDNSQuery{}, platform)
		if report.HasErrors() || !dnsHas(report, "warn", "尚未验证") {
			t.Fatalf("unavailable snapshot blocked publish: %+v", report)
		}
	}
}

func TestDNSRelationshipsCustomPortsAndLoops(t *testing.T) {
	mihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':2533', nameserver: [1.1.1.1]}", true)
	oxidns := dnsTestCandidate("oxidns", dnsOxiConfig(":5353", "udp://127.0.0.1:2533", false), true)
	report := dnsTestCheck(oxidns, &mihomo)
	if report.HasErrors() || !dnsHas(report, "pass", "端口对应") {
		t.Fatalf("custom ports not matched: %+v", report)
	}
	oxidns.Content = []byte(dnsOxiConfig(":5353", "udp://127.0.0.1:1053", false))
	report = dnsTestCheck(oxidns, &mihomo)
	if report.HasErrors() || !dnsHas(report, "warn", "端口配置不一致") {
		t.Fatalf("mismatched local endpoint should warn, not assume another service: %+v", report)
	}
	oxidns.Content = []byte(dnsOxiConfig(":5353", "udp://127.0.0.1:2533", false))
	mihomo.Content = []byte("dns: {enable: true, listen: ':2533', nameserver: ['127.0.0.1:5353']}")
	report = dnsTestCheck(mihomo, &oxidns)
	if !report.HasErrors() || !dnsHas(report, "error", "全部指向对端") {
		t.Fatalf("unconditional loop missed: %+v", report)
	}
	oxidns.Content = []byte(dnsOxiConfig(":5353", "udp://127.0.0.1:2533", true))
	report = dnsTestCheck(mihomo, &oxidns)
	if report.HasErrors() || !dnsHas(report, "warn", "相互转发") {
		t.Fatalf("conditional references falsely proven loop: %+v", report)
	}
}

func TestDNSAIOBlocksActualPeerListenerConflictsAtAnyPort(t *testing.T) {
	for _, test := range []struct {
		name, mihomoListen, oxidnsListen string
		conflict                         bool
	}{
		{"shared-53", ":53", ":53", true},
		{"shared-custom-port", ":5353", "127.0.0.1:5353", true},
		{"custom-ports", "127.0.0.1:5353", ":53", false},
		{"separate-addresses", "127.0.0.1:53", "192.0.2.1:53", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			mihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: '"+test.mihomoListen+"', nameserver: [1.1.1.1]}", true)
			oxidns := dnsTestCandidate("oxidns", dnsOxiConfig(test.oxidnsListen, "udp://1.1.1.1:53", false), true)
			for _, report := range []Report{dnsTestCheck(mihomo, &oxidns), dnsTestCheck(oxidns, &mihomo)} {
				if report.HasErrors() != test.conflict || dnsHas(report, "error", "监听冲突") != test.conflict {
					t.Fatalf("actual peer binding conflict was misclassified: %+v", report)
				}
			}
		})
	}
}

func TestDNSWildcardDoesNotMakeRemoteUpstreamSelf(t *testing.T) {
	for _, service := range []string{"mihomo", "oxidns"} {
		content := "dns: {enable: true, listen: ':53', nameserver: [1.1.1.1]}"
		if service == "oxidns" {
			content = dnsOxiConfig(":53", "udp://1.1.1.1:53", false)
		}
		report := dnsTestCheck(dnsTestCandidate(service, content, true), nil)
		if report.HasErrors() || dnsHas(report, "", "循环转发") {
			t.Fatalf("public upstream became self due wildcard: %+v", report)
		}
	}
	candidate := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':53', nameserver: ['127.0.0.1:53']}", true)
	if report := dnsTestCheck(candidate, nil); !dnsHas(report, "error", "直接循环") {
		t.Fatalf("AIO missing peer must still check self: %+v", report)
	}
	candidate.Content = []byte("dns: {enable: true, listen: ':53', nameserver: ['127.0.0.1:53', 1.1.1.1]}")
	if report := dnsTestCheck(candidate, nil); report.HasErrors() || !dnsHas(report, "warn", "自身监听") {
		t.Fatalf("independent fallback should not be proven endless loop: %+v", report)
	}
}

func TestDNSHijackOnlyReportedOnOwningAIOCore(t *testing.T) {
	for _, test := range []struct {
		name, content string
		aio, warn     bool
	}{
		{"non-aio", "tun: {enable: true, dns-hijack: ['any:53']}", false, false},
		{"disabled", "tun: {enable: false, dns-hijack: ['any:53']}", true, false},
		{"top-level", "tun: {enable: true, dns-hijack: ['any:53']}", true, true},
		{"listener", "listeners: [{type: tun, dns-hijack: ['tcp://any:53']}]", true, true},
		{"1053", "tun: {enable: true, dns-hijack: ['any:1053']}", true, false},
		{"custom", "tun: {enable: true, dns-hijack: ['any:5353', 'tcp://any:7874']}", true, false},
		{"mixed-custom-and-53", "tun: {enable: true, dns-hijack: ['any:5353', 'tcp://any:53']}", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := dnsTestCandidate("mihomo", test.content+"\ndns: {enable: false}", test.aio)
			report := dnsTestCheck(candidate, nil)
			if report.HasErrors() || dnsHas(report, "warn", "劫持范围") != test.warn {
				t.Fatalf("incorrect hijack treatment: %+v", report)
			}
			if test.aio && test.warn {
				oxi := dnsTestCandidate("oxidns", dnsOxiConfig(":5353", "1.1.1.1", false), true)
				if report = dnsTestCheck(oxi, &candidate); dnsHas(report, "warn", "劫持范围") {
					t.Fatalf("duplicate peer hijack warning: %+v", report)
				}
			}
		})
	}
}

func TestDNSUnusedForwardDoesNotCreateLoopAndCredentialsStayPrivate(t *testing.T) {
	content := dnsOxiConfig(":5353", "udp://1.1.1.1", false) + "- {tag: unused, type: forward, args: {upstreams: [{addr: 'udp://127.0.0.1:5353'}]}}\n"
	if report := dnsTestCheck(dnsTestCandidate("oxidns", content, false), nil); report.HasErrors() || dnsHas(report, "", "循环转发") {
		t.Fatalf("unused forward treated as live path: %+v", report)
	}
	content = dnsOxiConfig(":5353", "https://private:credential@example.net/dns-query?token=secret", false)
	mihomo := dnsTestCandidate("mihomo", "dns: {enable: false}", true)
	if report := dnsTestCheck(dnsTestCandidate("oxidns", content, true), &mihomo); dnsHas(report, "", "credential") || dnsHas(report, "", "secret") {
		t.Fatalf("upstream credential leaked: %+v", report)
	}
}

func TestDNSMultipleOxiEntrypointsCanChainLegitimately(t *testing.T) {
	content := `plugins:
- {tag: first, type: udp_server, args: {listen: ':53', entry: local}}
- {tag: second, type: udp_server, args: {listen: ':5353', entry: remote}}
- {tag: local, type: forward, args: {upstreams: [{addr: 'udp://127.0.0.1:5353'}]}}
- {tag: remote, type: forward, args: {upstreams: [{addr: 'udp://1.1.1.1'}]}}
`
	report := dnsTestCheck(dnsTestCandidate("oxidns", content, true), nil)
	if report.HasErrors() || !dnsHas(report, "warn", "自身监听") {
		t.Fatalf("legitimate listener chain falsely proven loop: %+v", report)
	}
}

func TestDNSRoutingOverridesPreventFalseLoopProof(t *testing.T) {
	candidate := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':53', respect-rules: true, nameserver: ['127.0.0.1:53']}", false)
	if report := dnsTestCheck(candidate, nil); report.HasErrors() || !dnsHas(report, "warn", "自身监听") {
		t.Fatalf("routed DNS cannot prove local loop: %+v", report)
	}
	for _, override := range []string{"socks5: 'socks5://127.0.0.1:7890'", "outbound: via_proxy", "dial_addr: 1.1.1.1", "port: 1053"} {
		content := "plugins:\n- {tag: server, type: udp_server, args: {listen: ':53', entry: upstream}}\n- {tag: upstream, type: forward, args: {upstreams: [{addr: 'udp://127.0.0.1:53', " + override + "}]}}\n"
		if report := dnsTestCheck(dnsTestCandidate("oxidns", content, true), nil); report.HasErrors() {
			t.Fatalf("override incorrectly proved loop: %s %+v", override, report)
		}
	}
}

func TestDNSCommonPluginsDoNotCreateGenericWarnings(t *testing.T) {
	mihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053', nameserver: [1.1.1.1]}", true)
	for _, plugin := range []string{"cache", "fallback", "query_recorder", "custom_extension"} {
		t.Run(plugin, func(t *testing.T) {
			content := `plugins:
- {tag: server, type: udp_server, args: {listen: ':53', entry: main}}
- {tag: main, type: sequence, args: [{exec: '$helper'}, {exec: '$remote'}, {exec: accept}]}
- {tag: helper, type: ` + plugin + `, args: {}}
- {tag: remote, type: forward, args: {upstreams: [{addr: 'udp://1.1.1.1'}]}}
`
			oxidns := dnsTestCandidate("oxidns", content, true)
			report := dnsTestCheck(oxidns, &mihomo)
			if report.HasErrors() || dnsHas(report, "warn", "") {
				t.Fatalf("normal external DNS path produced generic noise: %+v", report)
			}
			oxidns.Content = []byte(strings.Replace(content, "udp://1.1.1.1", "udp://127.0.0.1:5353", 1))
			if report = dnsTestCheck(oxidns, &mihomo); !dnsHas(report, "warn", "端口配置不一致") {
				t.Fatalf("concrete local upstream warning disappeared: %+v", report)
			}
			oxidns.Content = []byte(strings.Replace(content, "udp://1.1.1.1", "udp://127.0.0.1:53", 1))
			if report = dnsTestCheck(oxidns, &mihomo); !dnsHas(report, "warn", "自身监听") {
				t.Fatalf("concrete local loop warning disappeared: %+v", report)
			}
		})
	}
}

func TestDNSPeerSelfLoopDoesNotPreventIndependentRepairs(t *testing.T) {
	goodMihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053', nameserver: [1.1.1.1]}", true)
	badMihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053', nameserver: ['127.0.0.1:1053']}", true)
	goodOxiDNS := dnsTestCandidate("oxidns", dnsOxiConfig(":53", "udp://1.1.1.1:53", false), true)
	badOxiDNS := dnsTestCandidate("oxidns", dnsOxiConfig(":53", "udp://127.0.0.1:53", false), true)
	for _, tc := range []struct {
		name      string
		candidate DNSCandidate
		peer      DNSCandidate
	}{
		{"repair-mihomo-first", goodMihomo, badOxiDNS},
		{"repair-oxidns-first", goodOxiDNS, badMihomo},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report := dnsTestCheck(tc.candidate, &tc.peer)
			if report.HasErrors() || !dnsHas(report, "warn", "对端现有配置存在问题") {
				t.Fatalf("peer self-loop prevented repairing an independent core: %+v", report)
			}
			// Reversing the same pair makes the loop part of this candidate and
			// therefore a publication error, rather than a peer-only warning.
			report = dnsTestCheck(tc.peer, &tc.candidate)
			if !report.HasErrors() || !dnsHas(report, "error", "直接循环") {
				t.Fatalf("candidate self-loop no longer blocks publication: %+v", report)
			}
		})
	}
	// This is a loop created by the final pair, so it must remain blocking.
	mihomo := dnsTestCandidate("mihomo", "dns: {enable: true, listen: ':1053', nameserver: ['127.0.0.1:53']}", true)
	oxidns := dnsTestCandidate("oxidns", dnsOxiConfig(":53", "udp://127.0.0.1:1053", false), true)
	if report := dnsTestCheck(mihomo, &oxidns); !report.HasErrors() || !dnsHas(report, "error", "全部指向对端") {
		t.Fatalf("cross-core loop was incorrectly downgraded: %+v", report)
	}
}
