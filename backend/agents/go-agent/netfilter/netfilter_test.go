package netfilter

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// host is a fake Linux host: only listed tools exist and only the read-only
// commands of the contract may run.
type host struct {
	t        *testing.T
	tools    map[string]string // name -> resolved binary path
	outputs  map[string]string // "name args" -> stdout
	errs     map[string]error
	files    map[string]string
	modules  map[string]bool
	calls    []string
	block    bool
	ufwState string
}

var allowedCommands = map[string]bool{
	"nft -j list ruleset":            true,
	"iptables-save -t filter":        true,
	"iptables-legacy-save -t filter": true,
	"firewall-cmd --state":           true,
	"ufw status":                     true,
}

func newHost(t *testing.T) *host {
	return &host{t: t, tools: map[string]string{}, outputs: map[string]string{}, errs: map[string]error{}, files: map[string]string{
		"/proc/net/route": "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\neth0\t00000000\tFE00610A\t0003\t0\t0\t0\t00000000\t0\t0\t0\neth0\t0000610A\t00000000\t0001\t0\t0\t0\t00FFFFFF\t0\t0\t0\n",
	}, modules: map[string]bool{}}
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// nftHost has nf_tables loaded and an iptables-nft userspace, like Debian 13.
func nftHost(t *testing.T, ruleset, iptables string) *host {
	h := newHost(t)
	h.modules["/sys/module/nf_tables"] = true
	h.tools["nft"] = "/usr/sbin/nft"
	h.outputs["nft -j list ruleset"] = fixture(t, ruleset)
	if iptables != "" {
		h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
		h.tools["iptables-legacy-save"] = "/usr/sbin/xtables-legacy-multi"
		h.outputs["iptables-save -t filter"] = fixture(t, iptables)
	}
	return h
}

type fileInfo struct{}

func (fileInfo) Name() string       { return "module" }
func (fileInfo) Size() int64        { return 0 }
func (fileInfo) Mode() os.FileMode  { return os.ModeDir }
func (fileInfo) ModTime() time.Time { return time.Time{} }
func (fileInfo) IsDir() bool        { return true }
func (fileInfo) Sys() any           { return nil }

func (h *host) probe() Probe {
	return Probe{
		LookPath: func(name string) (string, error) {
			if _, ok := h.tools[name]; ok {
				return "/usr/sbin/" + name, nil
			}
			return "", errors.New("not found")
		},
		EvalSymlinks: func(path string) (string, error) {
			if resolved, ok := h.tools[filepath.Base(path)]; ok {
				return resolved, nil
			}
			return path, nil
		},
		Run: func(ctx context.Context, name string, args ...string) (string, error) {
			command := name + " " + strings.Join(args, " ")
			h.calls = append(h.calls, command)
			if !allowedCommands[command] {
				h.t.Fatalf("command outside the read-only contract: %s", command)
			}
			if h.block {
				<-ctx.Done()
				return "", ctx.Err()
			}
			if command == "ufw status" && h.ufwState != "" {
				return "Status: " + h.ufwState + "\n", nil
			}
			output, ok := h.outputs[command]
			if err := h.errs[command]; err != nil {
				return output, err
			}
			if !ok {
				h.t.Fatalf("unexpected command: %s", command)
			}
			return output, nil
		},
		ReadFile: func(path string) ([]byte, error) {
			if data, ok := h.files[path]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		},
		Stat: func(path string) (os.FileInfo, error) {
			if h.modules[path] {
				return fileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
		Now: func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	}
}

func (h *host) inspect(device string) Result {
	return Inspect(context.Background(), h.probe(), Target{Device: device, AutoRedirect: true})
}

// standalone is ConfigFlow's full TUN on a single-service Mihomo host: mixed
// stack, auto-redirect and LAN DNS to this host hijacked into the TUN.
var standalone = Target{Device: "Meta", AutoRedirect: true, Stack: "mixed", DNSHijack: true}

func (h *host) inspectTarget(target Target) Result {
	return Inspect(context.Background(), h.probe(), target)
}

// inspectWithoutRedirect leaves nf_tables presence to the kernel facts.
func (h *host) inspectWithoutRedirect(device string) Result {
	return Inspect(context.Background(), h.probe(), Target{Device: device})
}

// builtin lists the kernel's built-in modules, as /lib/modules/<release> does.
func (h *host) builtin(modules ...string) {
	h.files["/proc/sys/kernel/osrelease"] = "6.12.0-test\n"
	h.files["/lib/modules/6.12.0-test/modules.builtin"] = strings.Join(modules, "\n") + "\n"
}

// exitStatus is a command that ran and exited with this status, like
// *exec.ExitError; firewall-cmd writes its messages to stderr only.
type exitStatus int

func (e exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

func (h *host) called(command string) bool {
	for _, call := range h.calls {
		if call == command {
			return true
		}
	}
	return false
}

func onlyRemedy(t *testing.T, result Result) Remedy {
	t.Helper()
	if result.State != StateMayDrop || len(result.Remedies) != 1 {
		t.Fatalf("want one remedy: %+v", result)
	}
	return result.Remedies[0]
}

func TestPlainForwardDropIsReportedOnceWithExactCommands(t *testing.T) {
	// Lab 71: iptables-nft shows the same policy in both views.
	h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
	result := h.inspect("Meta")
	if result.Policy != "drop" || len(result.Sources) != 1 || result.Sources[0].Kind != "iptables" || result.Sources[0].Backend != "nft" || result.Sources[0].Policy != "drop" {
		t.Fatalf("wrong sources: %+v", result)
	}
	remedy := onlyRemedy(t, result)
	want := []string{"iptables -I FORWARD -o Meta -j ACCEPT", "iptables -I FORWARD -i Meta -j ACCEPT"}
	if !reflect.DeepEqual(remedy.Commands, want) || remedy.Source != "iptables" || !strings.Contains(remedy.PersistNote, "netfilter-persistent save") {
		t.Fatalf("wrong remedy: %+v", remedy)
	}
	if !strings.Contains(result.Hint, "UDP（如 QUIC、语音通话、游戏）和 ping 会失败") || !strings.Contains(result.Hint, "其他 TCP 连接不受影响") || !strings.Contains(result.Hint, "Meta") || !strings.Contains(result.Verify, "ping") || result.CheckedAt != "2026-10-24T08:00:00Z" {
		t.Fatalf("wrong text: %+v", result)
	}
	if !reflect.DeepEqual(result.Flows, []string{FlowForward}) || result.InputPolicy != "accept" || result.Sources[0].Hook != "forward" {
		t.Fatalf("wrong flows or policies: %+v", result)
	}
	if h.called("iptables-legacy-save -t filter") {
		t.Fatal("legacy table was not loaded; listing it would load it")
	}
}

func TestDeviceNameComesFromTheTarget(t *testing.T) {
	h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
	remedy := onlyRemedy(t, h.inspect("cf-tun0"))
	if remedy.Commands[0] != "iptables -I FORWARD -o cf-tun0 -j ACCEPT" || remedy.Device != "cf-tun0" {
		t.Fatalf("device not used: %+v", remedy)
	}
}

func TestWithoutIptablesTheNFTViewOfIPFilterIsEvaluated(t *testing.T) {
	h := nftHost(t, "plain-drop.nft.json", "")
	result := h.inspect("Meta")
	remedy := onlyRemedy(t, result)
	if result.Sources[0].Kind != "nftables" || result.Sources[0].Table != "filter" || result.Sources[0].Chain != "FORWARD" {
		t.Fatalf("wrong source: %+v", result.Sources)
	}
	want := []string{`nft 'insert rule ip filter FORWARD oifname "Meta" accept'`, `nft 'insert rule ip filter FORWARD iifname "Meta" accept'`}
	if !reflect.DeepEqual(remedy.Commands, want) {
		t.Fatalf("wrong commands: %q", remedy.Commands)
	}
}

func TestAcceptPolicyWithMihomoTablesIsOK(t *testing.T) {
	h := nftHost(t, "mihomo-only.nft.json", "plain-drop.iptables")
	result := h.inspect("Meta")
	if result.State != StateOK || result.Policy != "accept" || len(result.Sources) != 0 || len(result.Remedies) != 0 || result.Hint != "" {
		t.Fatalf("baseline should be ok: %+v", result)
	}
	// No ip filter table: iptables-nft has nothing to show and is not run.
	if h.called("iptables-save -t filter") {
		t.Fatal("iptables-nft listing without an ip filter table")
	}
}

func TestDockerDropPolicyUsesDockerUser(t *testing.T) {
	for _, lanKnown := range []bool{true, false} {
		h := nftHost(t, "ip-filter-present.nft.json", "docker-drop.iptables")
		if !lanKnown {
			delete(h.files, "/proc/net/route")
		}
		result := h.inspect("Meta")
		remedy := onlyRemedy(t, result)
		want := []string{"iptables -I DOCKER-USER -o Meta -j ACCEPT", "iptables -I DOCKER-USER -i Meta -j ACCEPT"}
		if !reflect.DeepEqual(remedy.Commands, want) || remedy.Source != "docker" || !strings.Contains(remedy.PersistNote, "DOCKER-USER") {
			t.Fatalf("lan=%v wrong remedy: %+v", lanKnown, remedy)
		}
		// Docker's own container rules never count; only the policy does.
		if len(result.Sources) != 1 || result.Sources[0].Kind != "docker" || result.Sources[0].Policy != "drop" {
			t.Fatalf("lan=%v wrong sources: %+v", lanKnown, result.Sources)
		}
	}
}

func TestDockerAcceptPolicyWithContainersIsOK(t *testing.T) {
	for _, name := range []string{"docker-accept.iptables", "docker-drop-fixed.iptables"} {
		for _, lanKnown := range []bool{true, false} {
			h := nftHost(t, "ip-filter-present.nft.json", name)
			if !lanKnown {
				delete(h.files, "/proc/net/route")
			}
			if result := h.inspect("Meta"); result.State != StateOK {
				t.Fatalf("%s lan=%v false positive: %+v", name, lanKnown, result)
			}
		}
	}
}

func TestLegacyBackendCommandsAndGuard(t *testing.T) {
	// iptables-save is legacy itself: plain iptables commands.
	h := newHost(t)
	h.tools["iptables-save"] = "/usr/sbin/xtables-legacy-multi"
	h.tools["iptables-legacy-save"] = "/usr/sbin/xtables-legacy-multi"
	h.files["/proc/net/ip_tables_names"] = "nat\nfilter\n"
	h.outputs["iptables-save -t filter"] = fixture(t, "plain-drop.iptables")
	result := h.inspect("Meta")
	remedy := onlyRemedy(t, result)
	if result.Sources[0].Backend != "legacy" || remedy.Commands[0] != "iptables -I FORWARD -o Meta -j ACCEPT" || h.called("iptables-legacy-save -t filter") {
		t.Fatalf("legacy default backend: %+v %v", result, h.calls)
	}

	// nft default plus loaded legacy tables (lab 70 warning): iptables-legacy.
	h = nftHost(t, "mihomo-only.nft.json", "plain-drop.iptables")
	h.files["/proc/net/ip_tables_names"] = "filter\n"
	h.outputs["iptables-legacy-save -t filter"] = fixture(t, "plain-drop.iptables")
	result = h.inspect("Meta")
	remedy = onlyRemedy(t, result)
	if remedy.Commands[1] != "iptables-legacy -I FORWARD -i Meta -j ACCEPT" || remedy.Label != "iptables（legacy）" {
		t.Fatalf("legacy view: %+v", remedy)
	}

	// The legacy filter table is not loaded: never list it.
	h = newHost(t)
	h.tools["iptables-save"] = "/usr/sbin/xtables-legacy-multi"
	h.tools["iptables-legacy-save"] = "/usr/sbin/xtables-legacy-multi"
	h.files["/proc/net/ip_tables_names"] = "nat\n"
	h.builtin("kernel/net/ipv4/netfilter/ip_tables.ko")
	if result := h.inspectWithoutRedirect("Meta"); result.State != StateOK || len(h.calls) != 0 {
		t.Fatalf("legacy guard: %+v %v", result, h.calls)
	}
}

func TestWithoutNFTablesNFTIsNotRun(t *testing.T) {
	h := newHost(t)
	h.tools["nft"] = "/usr/sbin/nft"
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.builtin("kernel/net/ipv4/netfilter/ip_tables.ko")
	if result := h.inspectWithoutRedirect("Meta"); result.State != StateOK || len(h.calls) != 0 {
		t.Fatalf("no nf_tables, nothing to inspect: %+v %v", result, h.calls)
	}
}

func TestBuiltInNFTablesIsInspected(t *testing.T) {
	// CONFIG_NF_TABLES=y (WSL2, some VPS and SBC kernels): no /sys/module entry.
	for _, tc := range []struct {
		name     string
		redirect bool
		builtin  []string
	}{
		{"auto-redirect uses nftables", true, nil},
		{"modules.builtin lists nf_tables", false, []string{"kernel/net/netfilter/nf_tables.ko"}},
	} {
		h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
		delete(h.modules, "/sys/module/nf_tables")
		if tc.builtin != nil {
			h.builtin(tc.builtin...)
		}
		result := Inspect(context.Background(), h.probe(), Target{Device: "Meta", AutoRedirect: tc.redirect})
		if result.State != StateMayDrop || result.Policy != "drop" || !h.called("nft -j list ruleset") {
			t.Fatalf("%s: %+v %v", tc.name, result, h.calls)
		}
	}
	// Presence cannot be told and nothing was inspected: never a false ok.
	h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
	delete(h.modules, "/sys/module/nf_tables")
	if result := h.inspectWithoutRedirect("Meta"); result.State != StateUnknown || result.Reason != "nft_state_unknown" || len(h.calls) != 0 {
		t.Fatalf("undetermined nf_tables: %+v %v", result, h.calls)
	}
}

func TestUFWRoutedDenyAndRouteAllow(t *testing.T) {
	// UFW denies routed and incoming traffic: forwarded UDP and, through the
	// INPUT default deny, the TCP auto-redirect hands to a local port.
	h := nftHost(t, "ip-filter-present.nft.json", "ufw-active.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result := h.inspect("Meta")
	remedy := onlyRemedy(t, result)
	want := []string{"ufw route allow in on Meta", "ufw route allow out on Meta", UFWBeforeRulesCommand, "ufw reload"}
	if !reflect.DeepEqual(remedy.Commands, want) || remedy.Source != "ufw" || remedy.PersistNote != ufwBeforeRulesNote {
		t.Fatalf("ufw remedy: %+v", remedy)
	}
	if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowRedirectTCP}) || strings.Contains(result.Hint, "不受影响") || !strings.Contains(result.Hint, "TCP 连接会失败") {
		t.Fatalf("ufw flows: %+v", result)
	}

	// Lab v5-2: `ufw route allow` alone left TCP broken; the warning stays.
	h = nftHost(t, "ip-filter-present.nft.json", "ufw-route-allowed.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result = h.inspect("Meta")
	remedy = onlyRemedy(t, result)
	if !reflect.DeepEqual(remedy.Commands, []string{UFWBeforeRulesCommand, "ufw reload"}) || !reflect.DeepEqual(result.Flows, []string{FlowRedirectTCP}) {
		t.Fatalf("route allow leaves redirected TCP blocked: %+v", result)
	}
	// The real FORWARD policy is still DROP although forwarding is accepted.
	if result.Policy != "drop" || result.InputPolicy != "drop" || result.Sources[0].Hook != "input" || !strings.Contains(result.Sources[0].Detail, "默认拒绝入站") {
		t.Fatalf("policies: %+v", result)
	}
}

func TestUFWInputAndRemedyFromTheLab(t *testing.T) {
	// Lab 2026-10-24: ufw 0.36.2 enabled with 22 and 9090 allowed broke
	// forwarded UDP, DNS to the host (UDP and TCP) and forwarded TCP.
	h := nftHost(t, "ufw-input.nft.json", "ufw-input.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result := h.inspectTarget(standalone)
	remedy := onlyRemedy(t, result)
	want := []string{"ufw route allow in on Meta", "ufw route allow out on Meta", "ufw allow in on Meta", UFWBeforeRulesCommand, "ufw reload"}
	if !reflect.DeepEqual(remedy.Commands, want) {
		t.Fatalf("ufw commands: %q", remedy.Commands)
	}
	if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowDNS, FlowRedirectTCP, FlowTUNTCP}) || !strings.Contains(result.Hint, "查不到域名") {
		t.Fatalf("ufw flows: %+v", result)
	}
	// After those commands (and a reboot) everything worked: ok, while both
	// default policies really are still DROP.
	h = nftHost(t, "ufw-input-fixed.nft.json", "ufw-input-fixed.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result = h.inspectTarget(standalone)
	if result.State != StateOK || result.Policy != "drop" || result.InputPolicy != "drop" || len(result.Flows) != 0 {
		t.Fatalf("fixed ufw: %+v", result)
	}
}

func TestUFWBeforeRulesCommandIsExact(t *testing.T) {
	// Verified in the lab: idempotent, the rule lands right after the marker.
	want := `grep -q -- '-A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT' /etc/ufw/before.rules || sed -i '/^# End required lines/a -A ufw-before-input -m conntrack --ctstate DNAT -j ACCEPT' /etc/ufw/before.rules`
	if UFWBeforeRulesCommand != want {
		t.Fatalf("command changed: %s", UFWBeforeRulesCommand)
	}
}

func TestIptablesInputDropBlocksRedirectedAndTUNTCP(t *testing.T) {
	// Lab: INPUT DROP with ssh allowed. Forwarded TCP (redirected to a local
	// port) and TCP DNS (system stack, from Meta) failed; UDP and ping worked.
	h := nftHost(t, "input-drop.nft.json", "input-drop.iptables")
	result := h.inspectTarget(standalone)
	remedy := onlyRemedy(t, result)
	want := []string{"iptables -I INPUT -m conntrack --ctstate DNAT -j ACCEPT", "iptables -I INPUT -i Meta -j ACCEPT"}
	if !reflect.DeepEqual(remedy.Commands, want) || remedy.Source != "iptables" || !strings.Contains(remedy.PersistNote, "这些规则") {
		t.Fatalf("input remedy: %+v", remedy)
	}
	if !reflect.DeepEqual(result.Flows, []string{FlowRedirectTCP, FlowTUNTCP}) || result.ForwardProtocols == nil || len(result.ForwardProtocols) != 0 || result.Policy != "accept" || result.InputPolicy != "drop" {
		t.Fatalf("input flows: %+v", result)
	}
	for _, text := range []string{"TCP 连接会失败", "TCP 方式的 DNS 查询会失败"} {
		if !strings.Contains(result.Hint, text) {
			t.Fatalf("hint lacks %q: %s", text, result.Hint)
		}
	}
	if strings.Contains(result.Hint, "UDP") || strings.Contains(result.Hint, "不受影响") || !strings.Contains(result.Verify, "网页") || !strings.Contains(result.Verify, "DNS") {
		t.Fatalf("impact text: %s / %s", result.Hint, result.Verify)
	}
	// Both commands applied: ok, and the real INPUT policy is still reported.
	h = nftHost(t, "input-drop-fixed.nft.json", "input-drop-fixed.iptables")
	if result := h.inspectTarget(standalone); result.State != StateOK || result.InputPolicy != "drop" {
		t.Fatalf("fixed input: %+v", result)
	}
	// The gvisor stack (Mihomo's default) keeps TCP in user space.
	h = nftHost(t, "input-drop.nft.json", "input-drop.iptables")
	target := standalone
	target.Stack = ""
	if remedy := onlyRemedy(t, h.inspectTarget(target)); !reflect.DeepEqual(remedy.Commands, want[:1]) {
		t.Fatalf("gvisor: %q", remedy.Commands)
	}
}

func TestInputRulesOnlyCountCertainDrops(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules string
		drop  bool
	}{
		{"port-specific drop", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp -m tcp --dport 3306 -j DROP\n", false},
		{"source ban list", ":INPUT ACCEPT [0:0]\n:f2b - [0:0]\n-A INPUT -p tcp -m multiport --dports 22 -j f2b\n-A f2b -s 192.0.2.1/32 -j REJECT\n-A f2b -j RETURN\n", false},
		{"udp drop", ":INPUT ACCEPT [0:0]\n-A INPUT -p udp -j DROP\n", false},
		{"tcp drop", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp -j DROP\n", true},
		{"new connections dropped", ":INPUT ACCEPT [0:0]\n-A INPUT -m conntrack --ctstate NEW -j DROP\n", true},
		{"established only accepted", ":INPUT DROP [0:0]\n-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n", true},
		{"dnat accepted", ":INPUT DROP [0:0]\n-A INPUT -m conntrack --ctstate DNAT -j ACCEPT\n", false},
		{"not-local drop skipped for local", ":INPUT DROP [0:0]\n:nl - [0:0]\n-A INPUT -m conntrack --ctstate DNAT -j nl\n-A INPUT -m conntrack --ctstate DNAT -j ACCEPT\n-A nl -m addrtype --dst-type LOCAL -j RETURN\n-A nl -j DROP\n", false},
		// Redirected TCP goes to a port from the ephemeral range (32768-60999).
		{"ephemeral ports accepted", ":INPUT DROP [0:0]\n-A INPUT -p tcp -m tcp --dport 1024:65535 -j ACCEPT\n", false},
		{"service ports accepted", ":INPUT DROP [0:0]\n-A INPUT -p tcp -m multiport --dports 22,80,443,8000:9000 -j ACCEPT\n", true},
		{"part of the ephemeral ports accepted", ":INPUT DROP [0:0]\n-A INPUT -p tcp -m tcp --dport 40000:41000 -j ACCEPT\n", false},
		// A source or interface condition may hold for LAN clients (ufw allow from <LAN>).
		{"LAN source accepted", ":INPUT DROP [0:0]\n-A INPUT -i lo -j ACCEPT\n-A INPUT -s 192.168.1.0/24 -j ACCEPT\n", false},
		{"source accepted in a user chain", ":INPUT DROP [0:0]\n:allow - [0:0]\n-A INPUT -j allow\n-A allow -s 192.168.1.0/24 -j ACCEPT\n", false},
		{"source accepted after a jump", ":INPUT DROP [0:0]\n:allow - [0:0]\n-A INPUT -s 192.168.1.0/24 -j allow\n-A allow -j ACCEPT\n", false},
		{"accept for another interface", ":INPUT DROP [0:0]\n-A INPUT -i wg0 -j ACCEPT\n", true},
		// New TCP connections start with SYN; other ports than ssh include the redirect port.
		{"syn dropped", ":INPUT ACCEPT [0:0]\n-A INPUT -i eth0 -p tcp -m tcp --tcp-flags FIN,SYN,RST,ACK SYN -j DROP\n", true},
		{"syn dropped (old syntax)", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp --syn -j DROP\n", true},
		{"non-syn dropped", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp -m tcp ! --tcp-flags FIN,SYN,RST,ACK SYN -m conntrack --ctstate NEW -j DROP\n", false},
		{"all but ssh rejected", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp -m tcp ! --dport 22 -j REJECT --reject-with icmp-port-unreachable\n", true},
		{"high ports dropped", ":INPUT ACCEPT [0:0]\n-A INPUT -p tcp -m tcp --dport 1024:65535 -j DROP\n", true},
		{"hardened input", ":INPUT ACCEPT [0:0]\n-A INPUT -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT\n-A INPUT -p tcp -m tcp --tcp-flags FIN,SYN,RST,ACK SYN -j DROP\n", true},
		{"syn dropped after a LAN accept", ":INPUT ACCEPT [0:0]\n-A INPUT -s 192.168.1.0/24 -j ACCEPT\n-A INPUT -p tcp --syn -j DROP\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			table, ok := parseIptablesSave("*filter\n" + tc.rules + "COMMIT\n")
			if !ok {
				t.Fatal("parse")
			}
			in := &inspection{ctx: context.Background(), target: Target{Device: "Meta", AutoRedirect: true}, lan: []string{"eth0"}}
			in.evaluateIptables(table, "iptables", "nft")
			if got := len(in.findings) > 0; got != tc.drop {
				t.Fatalf("drop=%v, want %v: %+v", got, tc.drop, in.findings)
			}
		})
	}
}

func TestNFTInputPolicyDrop(t *testing.T) {
	// Lab: Debian-style inet filter input policy drop (ssh and 9090 open).
	h := nftHost(t, "nft-input-drop.nft.json", "")
	result := h.inspectTarget(standalone)
	remedy := onlyRemedy(t, result)
	want := []string{`nft 'insert rule inet filter input ct status dnat accept'`, `nft 'insert rule inet filter input iifname "Meta" accept'`}
	if !reflect.DeepEqual(remedy.Commands, want) || remedy.Label != "nftables inet filter input" || result.InputPolicy != "drop" || result.Policy != "accept" {
		t.Fatalf("nft input: %+v", result)
	}
	if result.Sources[0].Hook != "input" || result.Sources[0].Chain != "input" {
		t.Fatalf("source: %+v", result.Sources)
	}
	h = nftHost(t, "nft-input-fixed.nft.json", "")
	if result := h.inspectTarget(standalone); result.State != StateOK || result.InputPolicy != "drop" {
		t.Fatalf("fixed nft input: %+v", result)
	}
}

func TestNativeNFTDropPolicy(t *testing.T) {
	h := nftHost(t, "nft-native-drop.nft.json", "")
	result := h.inspect("Meta")
	remedy := onlyRemedy(t, result)
	want := []string{`nft 'insert rule inet filter forward oifname "Meta" accept'`, `nft 'insert rule inet filter forward iifname "Meta" accept'`}
	if !reflect.DeepEqual(remedy.Commands, want) || remedy.PersistNote != nftPersistNote || result.Sources[0].Family != "inet" || result.Sources[0].Policy != "drop" {
		t.Fatalf("nft remedy: %+v", result)
	}
	h = nftHost(t, "nft-native-fixed.nft.json", "")
	if result := h.inspect("Meta"); result.State != StateOK {
		t.Fatalf("inserted accepts should clear the warning: %+v", result)
	}
}

func TestInvalidOnlyDropIsNotAProblem(t *testing.T) {
	h := nftHost(t, "nft-invalid-only.nft.json", "")
	if result := h.inspect("Meta"); result.State != StateOK {
		t.Fatalf("ct state invalid drop is harmless: %+v", result)
	}
	table, _ := parseIptablesSave("*filter\n:FORWARD ACCEPT [0:0]\n-A FORWARD -m conntrack --ctstate INVALID -j DROP\nCOMMIT\n")
	in := &inspection{ctx: context.Background(), target: Target{Device: "Meta"}, lan: []string{"eth0"}}
	in.evaluateIptables(table, "iptables", "nft")
	if len(in.findings) != 0 {
		t.Fatalf("iptables INVALID drop reported: %+v", in.findings)
	}
}

func evaluate(t *testing.T, rules string, lan []string) []finding {
	t.Helper()
	table, ok := parseIptablesSave("*filter\n" + rules + "COMMIT\n")
	if !ok {
		t.Fatalf("parse failed: %s", rules)
	}
	in := &inspection{ctx: context.Background(), target: Target{Device: "Meta"}, lan: lan}
	in.evaluateIptables(table, "iptables", "nft")
	return in.findings
}

func TestIptablesEvaluationIsConservative(t *testing.T) {
	for _, tc := range []struct {
		name  string
		rules string
		lan   []string
		drop  bool
	}{
		{"other interface drop, lan known", ":FORWARD ACCEPT [0:0]\n-A FORWARD -i eth1 -j DROP\n", []string{"eth0"}, false},
		{"other interface drop, lan unknown", ":FORWARD ACCEPT [0:0]\n-A FORWARD -i eth1 -j DROP\n", nil, true},
		{"tun drop", ":FORWARD ACCEPT [0:0]\n-A FORWARD -o Meta -p udp -j DROP\n", []string{"eth0"}, true},
		{"address-conditioned drop", ":FORWARD ACCEPT [0:0]\n-A FORWARD -d 10.0.0.0/8 -j REJECT --reject-with icmp-port-unreachable\n", []string{"eth0"}, true},
		{"unknown target", ":FORWARD ACCEPT [0:0]\n-A FORWARD -j NFQUEUE --queue-num 1\n", []string{"eth0"}, true},
		{"log only", ":FORWARD ACCEPT [0:0]\n-A FORWARD -j LOG --log-prefix \"fw drop \"\n", []string{"eth0"}, false},
		{"return applies drop policy", ":FORWARD DROP [0:0]\n-A FORWARD -j RETURN\n-A FORWARD -j ACCEPT\n", []string{"eth0"}, true},
		{"explicit accepts before drop policy", ":FORWARD DROP [0:0]\n-A FORWARD -o Meta -j ACCEPT\n-A FORWARD -i Meta -j ACCEPT\n", []string{"eth0"}, false},
		{"replies accepted by state", ":FORWARD DROP [0:0]\n-A FORWARD -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT\n-A FORWARD -i eth0 -o Meta -j ACCEPT\n", []string{"eth0"}, false},
		{"wildcard", ":FORWARD ACCEPT [0:0]\n-A FORWARD -o Me+ -j DROP\n", []string{"eth0"}, true},
		{"negated interface", ":FORWARD ACCEPT [0:0]\n-A FORWARD ! -o Meta -i Meta -j DROP\n", []string{"eth0"}, true},
		{"chain loop is bounded", ":FORWARD ACCEPT [0:0]\n:LOOP - [0:0]\n-A FORWARD -j LOOP\n-A LOOP -j LOOP\n", []string{"eth0"}, true},
		{"goto falls back to caller of caller", ":FORWARD DROP [0:0]\n:A - [0:0]\n-A FORWARD -g A\n-A FORWARD -j ACCEPT\n", []string{"eth0"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(evaluate(t, tc.rules, tc.lan)) > 0; got != tc.drop {
				t.Fatalf("drop=%v, want %v", got, tc.drop)
			}
		})
	}
}

func firewalldHost(t *testing.T, ruleset string) *host {
	h := nftHost(t, ruleset, "")
	h.tools["firewall-cmd"] = "/usr/bin/firewall-cmd"
	h.outputs["firewall-cmd --state"] = "running\n"
	return h
}

var firewalldCommands = []string{
	"firewall-cmd --permanent --info-zone=configflow-tun >/dev/null 2>&1 || firewall-cmd --permanent --new-zone=configflow-tun",
	"firewall-cmd --permanent --zone=configflow-tun --set-target=ACCEPT",
	"firewall-cmd --permanent --zone=configflow-tun --change-interface=Meta",
	"firewall-cmd --permanent --info-policy=configflow-fwd >/dev/null 2>&1 || firewall-cmd --permanent --new-policy=configflow-fwd",
	"firewall-cmd --permanent --policy=configflow-fwd --add-ingress-zone=ANY",
	"firewall-cmd --permanent --policy=configflow-fwd --add-egress-zone=configflow-tun",
	"firewall-cmd --permanent --policy=configflow-fwd --set-target=ACCEPT",
	"firewall-cmd --reload",
}

func TestFirewalldRulesetFromTheLab(t *testing.T) {
	// firewalld 2.3.1 (nftables backend), lab 2026-10-24. Its own chains are
	// evaluated: ct status dnat accept lets redirected TCP and hijacked DNS in.
	for _, tc := range []struct {
		ruleset string
		flows   []string
	}{
		// Meta in no zone: forwarded UDP/ping and TCP DNS (from Meta) failed.
		{"firewalld-public.nft.json", []string{FlowForward, FlowTUNTCP}},
		// Meta in trusted only: what enters from Meta works, forwarding into it does not.
		{"firewalld-trusted-only.nft.json", []string{FlowForward}},
		// Meta in public: intra-zone forwarding works, TCP DNS from Meta fails;
		// moving Meta to trusted alone would break forwarding, so the policy is included.
		{"firewalld-meta-public.nft.json", []string{FlowTUNTCP}},
	} {
		result := firewalldHost(t, tc.ruleset).inspectTarget(standalone)
		remedy := onlyRemedy(t, result)
		if !reflect.DeepEqual(result.Flows, tc.flows) || !reflect.DeepEqual(remedy.Commands, firewalldCommands) || remedy.Source != "firewalld" || remedy.PersistNote != firewalldNote+firewalldSavedNote {
			t.Fatalf("%s: %+v", tc.ruleset, result)
		}
		if result.Sources[0].Kind != "firewalld" || !strings.HasPrefix(result.Sources[0].Detail, "firewalld 正在运行") {
			t.Fatalf("%s sources: %+v", tc.ruleset, result.Sources)
		}
	}
	// Lab v7: after the commands (configflow-tun zone with only Meta, and the
	// configflow-fwd policy) everything worked, also after a reboot.
	if result := firewalldHost(t, "firewalld-zone-fixed.nft.json").inspectTarget(standalone); result.State != StateOK || result.Policy != "accept" {
		t.Fatalf("fixed firewalld: %+v", result)
	}
	// Meta in trusted plus an ANY -> trusted policy (lab v6) is also clean.
	if result := firewalldHost(t, "firewalld-fixed.nft.json").inspectTarget(standalone); result.State != StateOK {
		t.Fatalf("trusted firewalld: %+v", result)
	}
}

func TestFirewalldCommandsCanBeRerun(t *testing.T) {
	// Lab v7 A-6: re-running the commands failed only on --new-zone and
	// --new-policy (NAME_CONFLICT); the others only warn. Both are guarded by
	// an existence check, with or without the zone and policy in use.
	in := &inspection{target: Target{Device: "Meta"}}
	for _, flows := range []map[string]bool{{FlowForward: true}, {FlowTUNTCP: true}, {FlowForward: true, FlowTUNTCP: true}} {
		commands := in.remedy(&remedyPlan{source: "firewalld", flows: flows}).Commands
		if !reflect.DeepEqual(commands, firewalldCommands) {
			t.Fatalf("%v: %q", flows, commands)
		}
	}
	for _, command := range firewalldCommands {
		if strings.Contains(command, "--new-") && !strings.Contains(command, " >/dev/null 2>&1 || ") {
			t.Fatalf("unguarded: %s", command)
		}
		// The policy only ever accepts forwarding into the TUN-only zone.
		if strings.Contains(command, "trusted") || (strings.Contains(command, "--add-egress-zone=") && !strings.HasSuffix(command, "--add-egress-zone="+FirewalldZone)) {
			t.Fatalf("policy wider than the TUN zone: %s", command)
		}
	}
	if FirewalldNewZoneCommand != firewalldCommands[0] || FirewalldNewPolicyCommand != firewalldCommands[3] {
		t.Fatal("exact templates changed")
	}
	// Redirected TCP only (an old release without ct status dnat accept): no command.
	if remedy := in.remedy(&remedyPlan{source: "firewalld", flows: map[string]bool{FlowRedirectTCP: true}}); len(remedy.Commands) != 0 {
		t.Fatalf("redirect only: %+v", remedy)
	}
}

func TestFirewalldStateDecidesHowItsTableIsJudged(t *testing.T) {
	// firewalld stopped (exit 252, nothing on stdout): its leftover table is
	// evaluated like any other, and a clean host stays ok.
	h := nftHost(t, "firewalld-public.nft.json", "")
	h.tools["firewall-cmd"] = "/usr/bin/firewall-cmd"
	h.errs["firewall-cmd --state"] = exitStatus(252)
	if result := h.inspectTarget(standalone); result.State != StateMayDrop || result.Sources[0].Kind != "nftables" {
		t.Fatalf("stopped firewalld: %+v", result)
	}
	h = nftHost(t, "mihomo-only.nft.json", "")
	h.tools["firewall-cmd"] = "/usr/bin/firewall-cmd"
	h.errs["firewall-cmd --state"] = exitStatus(252)
	if result := h.inspect("Meta"); result.State != StateOK {
		t.Fatalf("disabled firewalld must not make the host unknown: %+v", result)
	}
	// Its state cannot be read: its table is not judged as a plain nft chain.
	for _, err := range []error{errors.New("signal: killed"), exitStatus(1), context.DeadlineExceeded} {
		h = nftHost(t, "firewalld-public.nft.json", "")
		h.tools["firewall-cmd"] = "/usr/bin/firewall-cmd"
		h.errs["firewall-cmd --state"] = err
		if result := h.inspectTarget(standalone); result.State != StateUnknown || len(result.Remedies) != 0 {
			t.Fatalf("unreadable firewalld state %v: %+v", err, result)
		}
	}
}

func TestFailuresAndTimeoutsAreUnknown(t *testing.T) {
	h := nftHost(t, "mihomo-only.nft.json", "")
	h.errs["nft -j list ruleset"] = errors.New("exit status 1: Operation not permitted")
	if result := h.inspect("Meta"); result.State != StateUnknown || result.Reason != "inspect_failed" || result.Hint != "" {
		t.Fatalf("failure: %+v", result)
	}

	h = newHost(t)
	h.modules["/sys/module/nf_tables"] = true
	if result := h.inspect("Meta"); result.State != StateUnknown || result.Reason != "nft_unavailable" {
		t.Fatalf("nft missing: %+v", result)
	}

	old := TotalBudget
	TotalBudget = 150 * time.Millisecond
	t.Cleanup(func() { TotalBudget = old })
	h = nftHost(t, "mihomo-only.nft.json", "plain-drop.iptables")
	h.block = true
	start := time.Now()
	result := h.inspect("Meta")
	if result.State != StateUnknown || result.Reason != "timeout" || time.Since(start) > time.Second {
		t.Fatalf("timeout: %+v after %s", result, time.Since(start))
	}
}

func TestFindingsWinOverPartialFailures(t *testing.T) {
	h := nftHost(t, "nft-native-drop.nft.json", "")
	h.tools["firewall-cmd"] = "/usr/bin/firewall-cmd"
	h.errs["firewall-cmd --state"] = errors.New("exit status 1")
	if result := h.inspect("Meta"); result.State != StateMayDrop {
		t.Fatalf("known drop must still be reported: %+v", result)
	}
}

func TestInvalidDeviceOrNamesGiveNoCommands(t *testing.T) {
	h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
	result := h.inspect("bad name;rm")
	if result.State != StateMayDrop || len(result.Remedies) != 0 || result.Device != "" || !strings.Contains(result.Hint, "手动放行") {
		t.Fatalf("invalid device: %+v", result)
	}
	in := &inspection{target: Target{Device: "Meta"}}
	if remedy := in.remedy(&remedyPlan{source: "nftables", family: "inet", table: "bad'table", chain: "forward", flows: map[string]bool{FlowForward: true}}); len(remedy.Commands) != 0 {
		t.Fatalf("unsafe table name produced commands: %+v", remedy)
	}
}

func TestHintWithoutAutoRedirectCoversTCP(t *testing.T) {
	h := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables")
	result := Inspect(context.Background(), h.probe(), Target{Device: "Meta"})
	if !strings.Contains(result.Hint, "包括 TCP、UDP 和 ping") || result.AutoRedirect {
		t.Fatalf("hint: %+v", result)
	}
}

func TestIptablesQuotedArguments(t *testing.T) {
	args, ok := splitArgs(`-A FORWARD -m comment --comment "a \"quoted\" -j DROP" -j ACCEPT`)
	if !ok || len(args) != 8 || args[5] != `a "quoted" -j DROP` {
		t.Fatalf("split: %q", args)
	}
	if _, ok := parseIptablesSave("# Table `filter' is incompatible, use 'nft' tool.\n"); ok {
		t.Fatal("incompatible table must not be read as empty")
	}
}

// nftRules builds a `nft -j list ruleset` document with one inet filter
// forward base chain and optional regular chains, sets and maps.
func nftRules(policy string, objects ...string) string {
	head := `{"metainfo": {"version": "1.0.6", "json_schema_version": 1}}, {"table": {"family": "inet", "name": "filter", "handle": 1}}, ` +
		`{"chain": {"family": "inet", "table": "filter", "name": "forward", "handle": 2, "type": "filter", "hook": "forward", "prio": 0, "policy": "` + policy + `"}}`
	return `{"nftables": [` + head + ", " + strings.Join(objects, ", ") + `]}`
}

func nftRule(chain, expr string) string {
	return `{"rule": {"family": "inet", "table": "filter", "chain": "` + chain + `", "expr": ` + expr + `}}`
}

func TestNFTVerdictMapsAndNamedSets(t *testing.T) {
	regular := `{"chain": {"family": "inet", "table": "filter", "name": "udp_fwd", "handle": 3}}`
	for _, tc := range []struct {
		name  string
		doc   string
		state string
	}{
		{"ct state vmap with invalid drop", nftRules("accept", nftRule("forward",
			`[{"vmap": {"key": {"ct": {"key": "state"}}, "data": {"set": [["established", {"accept": null}], ["related", {"accept": null}], ["invalid", {"drop": null}]]}}}]`)), StateOK},
		{"vmap jump into a dropping chain", nftRules("accept", regular, nftRule("forward",
			`[{"vmap": {"key": {"meta": {"key": "l4proto"}}, "data": {"set": [["udp", {"jump": {"target": "udp_fwd"}}]]}}}]`), nftRule("udp_fwd", `[{"drop": null}]`)), StateMayDrop},
		{"vmap jump into an accepting chain", nftRules("accept", regular, nftRule("forward",
			`[{"vmap": {"key": {"meta": {"key": "l4proto"}}, "data": {"set": [["udp", {"jump": {"target": "udp_fwd"}}]]}}}]`), nftRule("udp_fwd", `[{"accept": null}]`)), StateOK},
		{"named set allows the device", nftRules("drop",
			`{"set": {"family": "inet", "table": "filter", "name": "tun_ifs", "type": "ifname", "handle": 9, "elem": ["Meta"]}}`,
			nftRule("forward", `[{"match": {"op": "==", "left": {"meta": {"key": "iifname"}}, "right": "@tun_ifs"}}, {"accept": null}]`),
			nftRule("forward", `[{"match": {"op": "==", "left": {"meta": {"key": "oifname"}}, "right": "@tun_ifs"}}, {"accept": null}]`)), StateOK},
		{"named set drops the device", nftRules("accept",
			`{"set": {"family": "inet", "table": "filter", "name": "blocked", "type": "ifname", "handle": 9, "elem": [{"elem": {"val": "Meta", "comment": "x"}}]}}`,
			nftRule("forward", `[{"match": {"op": "==", "left": {"meta": {"key": "oifname"}}, "right": "@blocked"}}, {"drop": null}]`)), StateMayDrop},
		{"named set without the device", nftRules("accept",
			`{"set": {"family": "inet", "table": "filter", "name": "blocked", "type": "ifname", "handle": 9, "elem": ["wg9"]}}`,
			nftRule("forward", `[{"match": {"op": "==", "left": {"meta": {"key": "oifname"}}, "right": "@blocked"}}, {"drop": null}]`)), StateOK},
		{"unknown named set is a maybe", nftRules("accept",
			nftRule("forward", `[{"match": {"op": "==", "left": {"meta": {"key": "oifname"}}, "right": "@missing"}}, {"drop": null}]`)), StateMayDrop},
		{"named verdict map", nftRules("accept",
			`{"map": {"family": "inet", "table": "filter", "name": "fwd", "type": "ifname", "handle": 9, "map": "verdict", "elem": [["Meta", {"drop": null}]]}}`,
			nftRule("forward", `[{"vmap": {"key": {"meta": {"key": "oifname"}}, "data": "@fwd"}}]`)), StateMayDrop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := nftHost(t, "mihomo-only.nft.json", "")
			h.outputs["nft -j list ruleset"] = tc.doc
			if result := h.inspect("Meta"); result.State != tc.state {
				t.Fatalf("state %s, want %s: %+v", result.State, tc.state, result)
			}
		})
	}
}

func TestUFWIsRecognizedWithoutEnglishOutput(t *testing.T) {
	h := nftHost(t, "ip-filter-present.nft.json", "ufw-active.iptables")
	h.files["/etc/ufw/ufw.conf"] = "# /etc/ufw/ufw.conf\nENABLED=yes\nLOGLEVEL=low\n"
	remedy := onlyRemedy(t, h.inspect("Meta"))
	if remedy.Source != "ufw" || h.called("ufw status") {
		t.Fatalf("ufw.conf: %+v %v", remedy, h.calls)
	}
	h = nftHost(t, "ip-filter-present.nft.json", "ufw-active.iptables")
	h.files["/etc/ufw/ufw.conf"] = "ENABLED=no\n"
	if remedy := onlyRemedy(t, h.inspect("Meta")); remedy.Source != "iptables" {
		t.Fatalf("disabled ufw: %+v", remedy)
	}
}

func TestDefaultProbeIsUntranslatedAndBounded(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LANGUAGE", "zh_CN")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	probe := DefaultProbe()
	out, err := probe.Run(context.Background(), "sh", "-c", `printf %s "$LC_ALL/$LANG/$LANGUAGE"`)
	if err != nil || out != "C/C/C" {
		t.Fatalf("locale: %q %v", out, err)
	}
	// A wrapper killed by its budget while its child still holds the pipe.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := probe.Run(ctx, "sh", "-c", "sleep 5; :"); err == nil {
		t.Fatal("killed command reported success")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("waited %s for an orphaned child", elapsed)
	}
}

func TestTUNTargetReadsTheDeployedTUN(t *testing.T) {
	local := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("10.97.0.20").To4()}
	list := func(values ...string) []any {
		result := []any{}
		for _, value := range values {
			result = append(result, value)
		}
		return result
	}
	for _, tc := range []struct {
		name string
		tun  map[string]any
		want Target
	}{
		{"ConfigFlow standalone full TUN", map[string]any{"enable": true, "stack": "Mixed", "auto-redirect": true, "dns-hijack": list("any:53", "tcp://any:53")},
			Target{Device: "Meta", AutoRedirect: true, Stack: "mixed", DNSHijack: true}},
		{"AIO excludes this host", map[string]any{"device": "cf0", "stack": "mixed", "auto-redirect": true, "dns-hijack": list("any:53"), "route-exclude-address": list("127.0.0.0/8", "10.97.0.20/32")},
			Target{Device: "cf0", AutoRedirect: true, Stack: "mixed"}},
		{"Mihomo's default hijack", map[string]any{"auto-redirect": true}, Target{Device: "Meta", AutoRedirect: true, DNSHijack: true}},
		{"no port 53", map[string]any{"auto-redirect": true, "dns-hijack": list("any:1053")}, Target{Device: "Meta", AutoRedirect: true}},
		{"empty hijack", map[string]any{"auto-redirect": true, "dns-hijack": list()}, Target{Device: "Meta", AutoRedirect: true}},
		{"bare excluded address", map[string]any{"auto-redirect": true, "route-exclude-address": list("10.97.0.20")}, Target{Device: "Meta", AutoRedirect: true}},
		{"other excluded network", map[string]any{"auto-redirect": true, "route-exclude-address": list("192.168.0.0/16")}, Target{Device: "Meta", AutoRedirect: true, DNSHijack: true}},
		{"without auto-redirect", map[string]any{"stack": "system", "dns-hijack": list("0.0.0.0:53")}, Target{Device: "Meta", Stack: "system"}},
		// Review: excluding the private ranges a Docker bridge uses does not keep
		// DNS to the LAN address out of the TUN.
		{"Docker network excluded", map[string]any{"auto-redirect": true, "route-exclude-address": list("172.16.0.0/12")}, Target{Device: "Meta", AutoRedirect: true, DNSHijack: true}},
	} {
		got := TUNTarget(tc.tun, local)
		if !reflect.DeepEqual(got.LAN, local) {
			t.Fatalf("%s: LAN addresses %v", tc.name, got.LAN)
		}
		got.LAN = nil
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// Every LAN address must be excluded; none known means hijacked.
	tun := map[string]any{"auto-redirect": true, "route-exclude-address": list("10.97.0.20/32")}
	if got := TUNTarget(tun, []net.IP{net.ParseIP("10.97.0.20"), net.ParseIP("192.168.50.2")}); !got.DNSHijack {
		t.Fatalf("second LAN address: %+v", got)
	}
	if got := TUNTarget(tun, nil); !got.DNSHijack {
		t.Fatalf("no LAN address: %+v", got)
	}
}

func TestImpactTextFollowsTheBlockedFlows(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   Target
		blocked  []string
		contains []string
		absent   []string
	}{
		{"forward with auto-redirect", Target{Device: "Meta", AutoRedirect: true}, []string{FlowForward},
			[]string{"UDP（如 QUIC、语音通话、游戏）和 ping 会失败", "其他 TCP 连接不受影响"}, []string{"DNS"}},
		// Names do not resolve, so TCP by name fails as well.
		{"forward with hijacked DNS", standalone, []string{FlowForward, FlowDNS},
			[]string{"UDP（如 QUIC", "查不到域名（UDP 和 TCP 方式都会失败）"}, []string{"不受影响"}},
		{"forward with local DNS", aio, []string{FlowForward, FlowLocalDNS},
			[]string{"UDP（如 QUIC", "客户端查不到域名（UDP 和 TCP 方式都失败）"}, []string{"不受影响"}},
		// Only TCP DNS fails: names still resolve over UDP.
		{"forward with TCP-only local DNS", aio, []string{FlowForward, FlowLocalDNS, FlowLocalDNS + "/tcp"},
			[]string{"客户端 TCP 方式的 DNS 查询会失败", "其他 TCP 连接不受影响"}, nil},
		{"TCP DNS from the TUN", standalone, []string{FlowTUNTCP},
			[]string{"TCP 方式的 DNS 查询会失败", "其他 TCP 连接不受影响"}, []string{"UDP（"}},
		{"redirected TCP", standalone, []string{FlowRedirectTCP},
			[]string{"TCP 连接会失败（auto-redirect"}, []string{"不受影响", "UDP"}},
		{"forward without auto-redirect", Target{Device: "Meta", Stack: "mixed"}, []string{FlowForward, FlowTUNTCP},
			[]string{"包括 TCP、UDP 和 ping"}, []string{"不受影响", "TUN 把它们交给"}},
		{"tun input without auto-redirect", Target{Device: "Meta", Stack: "mixed"}, []string{FlowTUNTCP},
			[]string{"TCP 连接会失败（TUN 把它们交给本机的 Mihomo"}, []string{"UDP"}},
	} {
		blocked := map[string]bool{}
		for _, name := range tc.blocked {
			blocked[name] = true
		}
		text := hint(tc.target, blocked, true, true)
		for _, want := range tc.contains {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: %q lacks %q", tc.name, text, want)
			}
		}
		for _, unwanted := range tc.absent {
			if strings.Contains(text, unwanted) {
				t.Fatalf("%s: %q contains %q", tc.name, text, unwanted)
			}
		}
		if !strings.HasSuffix(text, "ConfigFlow 不会改动防火墙，需要时在这台 Linux 上执行下面的命令放行。") {
			t.Fatalf("%s: tail %q", tc.name, text)
		}
	}
	// An incomplete view never claims TCP is unaffected.
	if text := hint(Target{Device: "Meta", AutoRedirect: true}, map[string]bool{FlowForward: true}, false, true); strings.Contains(text, "不受影响") {
		t.Fatalf("incomplete: %s", text)
	}
	// A warning is cached for 5 seconds (routes.forwardFilterWarningTTL); the
	// text says so and does not claim a longer delay.
	if !strings.Contains(verifyPrefix, "缓存 5 秒") || strings.Contains(verifyPrefix, "30 秒") {
		t.Fatalf("verify prefix: %s", verifyPrefix)
	}
	if text := verify(Target{AutoRedirect: true}, map[string]bool{FlowForward: true}); text != verifyPrefix+" ping 一个走代理的地址，或打开一个使用 UDP 的应用（如 QUIC 网页、语音通话）确认可用。" {
		t.Fatalf("forward verify: %s", text)
	}
	if text := verify(standalone, map[string]bool{FlowRedirectTCP: true, FlowTUNTCP: true}); text != verifyPrefix+"打开一个走代理的网页；再用这台 Linux 作 DNS 解析一个域名，确认都可用。" {
		t.Fatalf("input verify: %s", text)
	}
}

func TestDockerHostWithInputDropGetsTwoGroups(t *testing.T) {
	h := nftHost(t, "ip-filter-present.nft.json", "")
	docker := strings.Replace(fixture(t, "docker-drop.iptables"), ":INPUT ACCEPT [0:0]", ":INPUT DROP [0:0]", 1)
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = docker
	result := h.inspectTarget(standalone)
	if len(result.Remedies) != 2 || result.Remedies[0].Source != "docker" || result.Remedies[1].Source != "iptables" {
		t.Fatalf("groups: %+v", result.Remedies)
	}
	if want := []string{"iptables -I INPUT -m conntrack --ctstate DNAT -j ACCEPT", "iptables -I INPUT -i Meta -j ACCEPT"}; !reflect.DeepEqual(result.Remedies[1].Commands, want) {
		t.Fatalf("input group: %q", result.Remedies[1].Commands)
	}
}

// nftInputRules is a minimal ruleset: inet filter with an input and a forward base
// chain and the given input rules (nft -j rule expressions).
func nftInputRules(inputPolicy, forwardPolicy string, input ...string) string {
	objects := []string{
		`{"table": {"family": "inet", "name": "filter", "handle": 1}}`,
		`{"chain": {"family": "inet", "table": "filter", "name": "input", "handle": 1, "type": "filter", "hook": "input", "prio": 0, "policy": "` + inputPolicy + `"}}`,
		`{"chain": {"family": "inet", "table": "filter", "name": "forward", "handle": 2, "type": "filter", "hook": "forward", "prio": 0, "policy": "` + forwardPolicy + `"}}`,
	}
	for _, expr := range input {
		objects = append(objects, `{"rule": {"family": "inet", "table": "filter", "chain": "input", "expr": `+expr+`}}`)
	}
	return `{"nftables": [` + strings.Join(objects, ", ") + `]}`
}

const (
	nftLoopbackAccept  = `[{"match": {"op": "==", "left": {"meta": {"key": "iif"}}, "right": "lo"}}, {"accept": null}]`
	nftEstablished     = `[{"match": {"op": "in", "left": {"ct": {"key": "state"}}, "right": ["established", "related"]}}, {"accept": null}]`
	nftServicePorts    = `[{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": {"set": [22, 9090, {"range": [8000, 9000]}]}}}, {"accept": null}]`
	nftLANSourceAccept = `[{"match": {"op": "==", "left": {"payload": {"protocol": "ip", "field": "saddr"}}, "right": {"prefix": {"addr": "10.97.0.0", "len": 24}}}}, {"accept": null}]`
	nftAllButSSHDrop   = `[{"match": {"op": "!=", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": 22}}, {"drop": null}]`
	nftSYNDrop         = `[{"match": {"op": "==", "left": {"&": [{"payload": {"protocol": "tcp", "field": "flags"}}, {"|": ["fin", "syn", "rst", "ack"]}]}, "right": "syn"}}, {"drop": null}]`
	nftSYNBitDrop      = `[{"match": {"op": "in", "left": {"payload": {"protocol": "tcp", "field": "flags"}}, "right": "syn"}}, {"drop": null}]`
	nftSourceBan       = `[{"match": {"op": "==", "left": {"payload": {"protocol": "ip", "field": "saddr"}}, "right": "192.0.2.1"}}, {"drop": null}]`
	nftTUNSourceAccept = `[{"match": {"op": "==", "left": {"payload": {"protocol": "ip", "field": "saddr"}}, "right": {"set": [{"range": ["198.18.0.0", "198.18.0.3"]}, "10.0.0.1"]}}}, {"accept": null}]`
	nftEphemeralAccept = `[{"match": {"op": "==", "left": {"payload": {"protocol": "th", "field": "dport"}}, "right": {"range": [1024, 65535]}}}, {"accept": null}]`
)

func TestConditionalAcceptsKeepInputDropsUncertain(t *testing.T) {
	// Review: `ufw allow from <LAN>` (or -s LAN -j ACCEPT) lets redirected TCP
	// in, since auto-redirect keeps the client's source address. A DROP
	// policy after such a rule is no longer reported as certain. Lab v7 C:
	// forwarded TCP worked; TCP DNS, which the TUN stack sends from
	// 198.18.0.2, was still blocked.
	h := nftHost(t, "ufw-input.nft.json", "ufw-allow-from-lan.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result := h.inspectTarget(standalone)
	if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowDNS, FlowTUNTCP}) || strings.Contains(result.Hint, "TCP 连接会失败") || strings.Contains(result.Hint, "不受影响") {
		t.Fatalf("ufw allow from LAN: %+v", result)
	}
	if remedy := onlyRemedy(t, result); !reflect.DeepEqual(remedy.Commands, []string{"ufw route allow in on Meta", "ufw route allow out on Meta", "ufw allow in on Meta"}) {
		t.Fatalf("ufw allow from LAN commands: %q", remedy.Commands)
	}

	// Plain iptables: INPUT DROP with the LAN accepted.
	h = nftHost(t, "input-drop.nft.json", "")
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = strings.Replace(fixture(t, "input-drop.iptables"),
		"-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT\n", "-A INPUT -s 10.97.0.0/24 -j ACCEPT\n-A INPUT -p tcp -m tcp --dport 22 -j ACCEPT\n", 1)
	if result := h.inspectTarget(standalone); !reflect.DeepEqual(result.Flows, []string{FlowTUNTCP}) || result.InputPolicy != "drop" {
		t.Fatalf("iptables -s LAN -j ACCEPT: %+v", result)
	}

	// nftables: ip saddr <LAN> accept before policy drop.
	for _, tc := range []struct {
		name  string
		rules []string
		flows []string
	}{
		{"LAN source accepted", []string{nftLoopbackAccept, nftEstablished, nftServicePorts, nftLANSourceAccept}, []string{FlowTUNTCP}},
		{"LAN and TUN sources accepted", []string{nftEstablished, nftLANSourceAccept, nftTUNSourceAccept}, []string{}},
		{"ephemeral ports accepted", []string{nftEstablished, nftEphemeralAccept}, []string{}},
		{"service ports only", []string{nftLoopbackAccept, nftEstablished, nftServicePorts}, []string{FlowRedirectTCP, FlowTUNTCP}},
	} {
		h = nftHost(t, "mihomo-only.nft.json", "")
		h.outputs["nft -j list ruleset"] = nftInputRules("drop", "accept", tc.rules...)
		if result := h.inspectTarget(standalone); !reflect.DeepEqual(result.Flows, tc.flows) {
			t.Fatalf("nft %s: %+v", tc.name, result)
		}
	}
}

func TestInputDropsOfNewTCPAreCertain(t *testing.T) {
	// Review: these rules drop every new TCP connection to the redirect port,
	// so forwarded TCP fails although INPUT accepts by default.
	for _, tc := range []struct {
		name  string
		rules []string
	}{
		{"all but ssh dropped", []string{nftEstablished, nftAllButSSHDrop}},
		{"syn dropped", []string{nftEstablished, nftServicePorts, nftSYNDrop}},
		{"syn bit dropped", []string{nftEstablished, nftSYNBitDrop}},
	} {
		h := nftHost(t, "mihomo-only.nft.json", "")
		h.outputs["nft -j list ruleset"] = nftInputRules("accept", "drop", tc.rules...)
		result := h.inspectTarget(standalone)
		if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowDNS, FlowRedirectTCP, FlowTUNTCP}) || strings.Contains(result.Hint, "不受影响") || !strings.Contains(result.Hint, "TCP 连接会失败") {
			t.Fatalf("nft %s: %+v", tc.name, result)
		}
	}
	h := nftHost(t, "plain-drop.nft.json", "")
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = strings.Replace(fixture(t, "plain-drop.iptables"), "COMMIT", "-A INPUT -p tcp -m tcp ! --dport 22 -j DROP\nCOMMIT", 1)
	result := h.inspectTarget(standalone)
	if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowDNS, FlowRedirectTCP, FlowTUNTCP}) || strings.Contains(result.Hint, "不受影响") {
		t.Fatalf("iptables ! --dport 22: %+v", result)
	}
}

func TestUncertainInputDropsDoNotClaimTCPIsUnaffected(t *testing.T) {
	// A drop that may concern redirected TCP is not reported, but the hint no
	// longer says other TCP connections are unaffected.
	h := nftHost(t, "plain-drop.nft.json", "")
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = strings.Replace(fixture(t, "plain-drop.iptables"), "COMMIT", "-A INPUT -s 192.0.2.0/24 -j DROP\nCOMMIT", 1)
	result := h.inspectTarget(Target{Device: "Meta", AutoRedirect: true})
	if !reflect.DeepEqual(result.Flows, []string{FlowForward}) || strings.Contains(result.Hint, "不受影响") || !strings.Contains(result.Hint, "UDP") {
		t.Fatalf("uncertain input drop: %+v", result)
	}
	h = nftHost(t, "mihomo-only.nft.json", "")
	h.outputs["nft -j list ruleset"] = nftInputRules("accept", "drop", nftSourceBan)
	if result := h.inspectTarget(standalone); strings.Contains(result.Hint, "不受影响") || !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowDNS}) {
		t.Fatalf("nft uncertain input drop: %+v", result)
	}
	// Port rules that cannot concern the redirect port leave the claim intact.
	h = nftHost(t, "plain-drop.nft.json", "")
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = strings.Replace(fixture(t, "plain-drop.iptables"), "COMMIT", "-A INPUT -p tcp -m tcp --dport 3306 -j DROP\nCOMMIT", 1)
	if result := h.inspectTarget(Target{Device: "Meta", AutoRedirect: true}); !strings.Contains(result.Hint, "其他 TCP 连接不受影响") {
		t.Fatalf("port drop: %+v", result)
	}
}

func TestEphemeralPortRangeIsRead(t *testing.T) {
	h := nftHost(t, "mihomo-only.nft.json", "")
	h.files["/proc/sys/net/ipv4/ip_local_port_range"] = "10000\t20000\n"
	accept := `[{"match": {"op": "==", "left": {"payload": {"protocol": "tcp", "field": "dport"}}, "right": {"range": [10000, 20000]}}}, {"accept": null}]`
	h.outputs["nft -j list ruleset"] = nftInputRules("drop", "accept", accept)
	if result := h.inspect("Meta"); result.State != StateOK {
		t.Fatalf("custom range accepted: %+v", result)
	}
	for _, tc := range []struct {
		value string
		want  portSpan
	}{
		{"10000\t20000\n", portSpan{10000, 20000}},
		{"garbage", defaultLocalPorts},
		{"20000 10000", defaultLocalPorts},
	} {
		probe := Probe{ReadFile: func(string) ([]byte, error) { return []byte(tc.value), nil }}
		if got := localPortRange(probe); got != tc.want {
			t.Fatalf("%q: %+v", tc.value, got)
		}
	}
}

func TestPortAndFlagMatching(t *testing.T) {
	f := flow{proto: "tcp", ct: ctFacts{known: true, isNew: true}, dport: portSpan{32768, 60999}}
	for _, tc := range []struct {
		spans string
		want  tri
	}{
		{"22", no}, {"1024:65535", yes}, {"1024:", yes}, {"40000", maybe}, {"30000:40000", maybe}, {"61000:65535", no},
		{"1000:32767,32768:50000,50001:61000", yes}, {"32768:40000,40002:60999", maybe},
	} {
		spans, ok := parsePortList(tc.spans, true)
		if !ok || portMatch(spans, f) != tc.want {
			t.Fatalf("%s: %v %v, want %v", tc.spans, ok, portMatch(spans, f), tc.want)
		}
	}
	if portMatch([]portSpan{{22, 22}}, flow{}) != maybe {
		t.Fatal("unknown ports")
	}
	for _, tc := range []struct {
		mask, comp string
		want       tri
	}{
		{"FIN,SYN,RST,ACK", "SYN", yes}, {"SYN,ACK", "SYN", yes}, {"SYN,ACK", "ACK", no}, {"ALL", "NONE", no}, {"ALL", "SYN", yes}, {"SYN,ECE", "SYN", maybe},
	} {
		mask, _ := parseTCPFlags(tc.mask)
		comp, _ := parseTCPFlags(tc.comp)
		if got := (tcpFlags{mask: mask, comp: comp}).match(f); got != tc.want {
			t.Fatalf("%s %s: %v, want %v", tc.mask, tc.comp, got, tc.want)
		}
	}
	if (tcpFlags{mask: synMask, comp: tcpFlagSYN}).match(flow{proto: "tcp"}) != maybe {
		t.Fatal("forward flows are not only SYN packets")
	}
}

func TestLANAddressesComeFromTheDefaultRoute(t *testing.T) {
	ip := func(values ...string) []net.IP {
		var result []net.IP
		for _, value := range values {
			result = append(result, net.ParseIP(value))
		}
		return result
	}
	up := net.FlagUp | net.FlagBroadcast
	interfaces := []interfaceAddresses{
		{name: "lo", flags: net.FlagUp | net.FlagLoopback, ips: ip("127.0.0.1")},
		{name: "eth0", flags: up, ips: ip("10.97.0.20", "fe80::1")},
		{name: "docker0", flags: up, ips: ip("172.17.0.1")},
		{name: "Meta", flags: net.FlagUp | net.FlagPointToPoint, ips: ip("198.18.0.1")},
		{name: "eth1", flags: up, ips: ip("192.168.50.2")},
	}
	if got := lanAddresses([]string{"eth0"}, interfaces); len(got) != 1 || !got[0].Equal(net.ParseIP("10.97.0.20")) {
		t.Fatalf("default route: %v", got)
	}
	// Without a default route: no loopback, TUN or container bridge.
	if got := lanAddresses(nil, interfaces); len(got) != 2 || !got[0].Equal(net.ParseIP("10.97.0.20")) || !got[1].Equal(net.ParseIP("192.168.50.2")) {
		t.Fatalf("fallback: %v", got)
	}
}

func TestTUNTCPSourceIsTheTUNPeer(t *testing.T) {
	// Lab v7: TCP the mixed stack hands to Mihomo enters as 198.18.0.2 -> 198.18.0.1.
	for _, tc := range []struct {
		name   string
		rules  string
		target Target
		flows  []string
	}{
		{"TUN source rejected", "-A INPUT -s 198.18.0.0/30 -p tcp -j REJECT\n", standalone, []string{FlowTUNTCP}},
		{"other sources rejected", "-A INPUT ! -s 198.18.0.0/16 -p tcp -j REJECT\n", standalone, []string{}},
		{"configured inet4-address", "-A INPUT -s 172.19.0.2/32 -p tcp -j REJECT\n", func() Target {
			target := standalone
			target.TUNPeer = netip.MustParseAddr("172.19.0.2")
			return target
		}(), []string{FlowTUNTCP}},
	} {
		h := nftHost(t, "ip-filter-present.nft.json", "")
		h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
		h.outputs["iptables-save -t filter"] = "*filter\n:INPUT ACCEPT [0:0]\n:FORWARD ACCEPT [0:0]\n" + tc.rules + "COMMIT\n"
		if result := h.inspectTarget(tc.target); !reflect.DeepEqual(result.Flows, tc.flows) {
			t.Fatalf("%s: %+v", tc.name, result)
		}
	}
	for _, tc := range []struct {
		value any
		want  string
	}{
		{[]any{"172.19.0.1/30"}, "172.19.0.2"}, {"10.0.0.1/24", "10.0.0.2"}, {nil, "invalid IP"}, {[]any{"fdfe::1/126"}, "invalid IP"}, {"bad", "invalid IP"},
	} {
		if got := tunPeer(map[string]any{"inet4-address": tc.value}).String(); got != tc.want {
			t.Fatalf("%v: %s", tc.value, got)
		}
	}
	prefixes, ok := nftPrefixes(map[string]any{"range": []any{"10.0.0.1", "10.0.0.6"}}, nil)
	if !ok || fmt.Sprint(prefixes) != "[10.0.0.1/32 10.0.0.2/31 10.0.0.4/31 10.0.0.6/32]" {
		t.Fatalf("range: %v %v", prefixes, ok)
	}
}

// aio is ConfigFlow's all-in-one host (lab v6-d-aio): full TUN with
// auto-redirect and the mixed stack, the host's own address excluded from the
// TUN, and OxiDNS answering 0.0.0.0:53 itself.
var aio = Target{Device: "Meta", AutoRedirect: true, Stack: "mixed", LAN: []net.IP{net.ParseIP("10.97.0.20").To4()}}

// procHex writes an address the way /proc/net does: 32-bit words in hex,
// each in the host's byte order.
func procHex(ip net.IP) string {
	raw := ip.To4()
	if raw == nil {
		raw = ip.To16()
	}
	var text strings.Builder
	for i := 0; i < len(raw); i += 4 {
		fmt.Fprintf(&text, "%08X", binary.NativeEndian.Uint32(raw[i:i+4]))
	}
	return text.String()
}

// procSocket is one /proc/net/{tcp,udp}[6] line.
func procSocket(local string, port int, remote string, state string) string {
	return fmt.Sprintf("   0: %s:%04X %s:0000 %s 00000000:00000000 00:00000000 00000000   997        0 21571 2 0000000000000000 0\n", procHex(net.ParseIP(local)), port, procHex(net.ParseIP(remote)), state)
}

const procHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode ref pointer drops\n"

// oxidnsListening adds what `ss -lntup` showed on the AIO host (12-host-dns):
// OxiDNS on 0.0.0.0:53 (UDP and TCP), Mihomo's DNS on *:1053, and the
// Agent's API.
func (h *host) oxidnsListening() *host {
	h.files["/proc/net/udp"] = procHeader + procSocket("0.0.0.0", 53, "0.0.0.0", "07")
	h.files["/proc/net/tcp"] = procHeader + procSocket("0.0.0.0", 53, "0.0.0.0", "0A") + procSocket("0.0.0.0", 8080, "0.0.0.0", "0A") +
		procSocket("10.97.0.20", 53, "10.97.0.200", "01")
	h.files["/proc/net/udp6"] = procHeader + procSocket("::", 1053, "::", "07")
	return h
}

func TestAIOWithUFWReportsLocalDNSFromTheLab(t *testing.T) {
	// Lab v6-d-aio 41-46: UFW deny (incoming) and deny (routed). After the
	// commands of the previous release, TCP and forwarded UDP worked while DNS
	// to 10.97.0.20:53 (OxiDNS, outside the TUN) was still dropped on INPUT
	// ([UFW BLOCK] IN=eth0 DPT=53), UDP and TCP alike. Forwarded ping worked
	// throughout: UFW accepts forwarded echo requests.
	h := nftHost(t, "ufw-input.nft.json", "ufw-input.iptables").oxidnsListening()
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result := h.inspectTarget(aio)
	remedy := onlyRemedy(t, result)
	want := []string{"ufw route allow in on Meta", "ufw route allow out on Meta", "ufw allow in on eth0 to any port 53", UFWBeforeRulesCommand, "ufw reload"}
	if !reflect.DeepEqual(remedy.Commands, want) {
		t.Fatalf("ufw commands: %q", remedy.Commands)
	}
	if !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowLocalDNS, FlowRedirectTCP}) {
		t.Fatalf("flows: %+v", result.Flows)
	}
	for _, text := range []string{"客户端查不到域名（UDP 和 TCP 方式都失败）", "发到本机 53 端口的 DNS 查询", "主路由转来的 UDP（如 QUIC、语音通话、游戏）会失败"} {
		if !strings.Contains(result.Hint, text) {
			t.Fatalf("hint lacks %q: %s", text, result.Hint)
		}
	}
	if strings.Contains(result.Hint, "ping") || strings.Contains(result.Verify, "ping") || !strings.Contains(result.Verify, "DNS") {
		t.Fatalf("UFW accepts forwarded ping: %s / %s", result.Hint, result.Verify)
	}
	// After `ufw allow in on eth0 to any port 53` as well: ok.
	h = nftHost(t, "ufw-input-fixed.nft.json", "ufw-dns-fixed.iptables").oxidnsListening()
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	if result := h.inspectTarget(aio); result.State != StateOK {
		t.Fatalf("dns allowed: %+v", result)
	}
	// Without the DNS rule the previous fix leaves exactly the DNS warning.
	h = nftHost(t, "ufw-input-fixed.nft.json", "ufw-input-fixed.iptables").oxidnsListening()
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result = h.inspectTarget(aio)
	if remedy := onlyRemedy(t, result); !reflect.DeepEqual(remedy.Commands, []string{"ufw allow in on eth0 to any port 53"}) || !reflect.DeepEqual(result.Flows, []string{FlowLocalDNS}) {
		t.Fatalf("dns only: %+v", result)
	}
	if !strings.HasPrefix(result.Hint, "这台 Linux 的防火墙可能拦截发到本机 53 端口的 DNS 查询（由本机的 DNS 服务应答，不经过 TUN）：客户端查不到域名（UDP 和 TCP 方式都失败）。") || strings.Contains(result.Hint, "不受影响") {
		t.Fatalf("dns only hint: %s", result.Hint)
	}
}

func TestAIOWithIptablesInputDropReportsLocalDNS(t *testing.T) {
	h := nftHost(t, "input-drop.nft.json", "input-drop.iptables").oxidnsListening()
	result := h.inspectTarget(aio)
	remedy := onlyRemedy(t, result)
	want := []string{
		"iptables -I INPUT -m conntrack --ctstate DNAT -j ACCEPT",
		"iptables -I INPUT -i eth0 -p udp --dport 53 -j ACCEPT",
		"iptables -I INPUT -i eth0 -p tcp --dport 53 -j ACCEPT",
	}
	if !reflect.DeepEqual(remedy.Commands, want) || !reflect.DeepEqual(result.Flows, []string{FlowLocalDNS, FlowRedirectTCP}) {
		t.Fatalf("iptables: %+v", result)
	}
	h = nftHost(t, "input-drop-fixed.nft.json", "input-dns-fixed.iptables").oxidnsListening()
	if result := h.inspectTarget(aio); result.State != StateOK {
		t.Fatalf("fixed: %+v", result)
	}
	// The legacy backend names its own binary.
	in := &inspection{target: aio, lan: []string{"eth0"}}
	if commands := in.remedy(&remedyPlan{source: "iptables", binary: "iptables-legacy", flows: map[string]bool{FlowLocalDNS: true}}).Commands; commands[0] != "iptables-legacy -I INPUT -i eth0 -p udp --dport 53 -j ACCEPT" {
		t.Fatalf("legacy: %q", commands)
	}
}

func TestAIOWithNFTInputDropReportsLocalDNS(t *testing.T) {
	h := nftHost(t, "nft-input-drop.nft.json", "").oxidnsListening()
	result := h.inspectTarget(aio)
	remedy := onlyRemedy(t, result)
	want := []string{
		`nft 'insert rule inet filter input ct status dnat accept'`,
		`nft 'insert rule inet filter input iifname "eth0" udp dport 53 accept'`,
		`nft 'insert rule inet filter input iifname "eth0" tcp dport 53 accept'`,
	}
	if !reflect.DeepEqual(remedy.Commands, want) {
		t.Fatalf("nft: %q", remedy.Commands)
	}
	if result := nftHost(t, "nft-input-dns-fixed.nft.json", "").oxidnsListening().inspectTarget(aio); result.State != StateOK {
		t.Fatalf("fixed: %+v", result)
	}
}

func TestAIOWithFirewalldReportsLocalDNS(t *testing.T) {
	// firewalld public zone (lab 2026-10-24) offers ssh and dhcpv6-client
	// only; DNS to the host needs the dns service in eth0's zone.
	result := firewalldHost(t, "firewalld-public.nft.json").oxidnsListening().inspectTarget(aio)
	remedy := onlyRemedy(t, result)
	want := append(append([]string{}, firewalldCommands[:7]...), "firewall-cmd --permanent --zone=public --add-service=dns", "firewall-cmd --reload")
	if !reflect.DeepEqual(remedy.Commands, want) || !reflect.DeepEqual(result.Flows, []string{FlowForward, FlowLocalDNS}) {
		t.Fatalf("firewalld: %+v", result)
	}
	// firewalld accepts redirected TCP itself, but clients that cannot resolve
	// names cannot open TCP connections by name either.
	if !strings.Contains(result.Hint, "客户端查不到域名（UDP 和 TCP 方式都失败）") || strings.Contains(result.Hint, "不受影响") {
		t.Fatalf("firewalld hint: %s", result.Hint)
	}
	if !strings.Contains(remedy.PersistNote, "开放 DNS 服务") || !strings.Contains(remedy.PersistNote, "configflow-tun") {
		t.Fatalf("note: %s", remedy.PersistNote)
	}
	found := false
	for _, source := range result.Sources {
		found = found || strings.Contains(source.Detail, "53 端口的 DNS 查询")
	}
	if !found {
		t.Fatalf("sources: %+v", result.Sources)
	}
	// After the forwarding commands only DNS is left; after both, ok.
	result = firewalldHost(t, "firewalld-zone-fixed.nft.json").oxidnsListening().inspectTarget(aio)
	if remedy := onlyRemedy(t, result); !reflect.DeepEqual(remedy.Commands, []string{"firewall-cmd --permanent --zone=public --add-service=dns", "firewall-cmd --reload"}) || remedy.PersistNote != firewalldDNSNote+firewalldSavedNote {
		t.Fatalf("dns only: %+v", result)
	}
	if result := firewalldHost(t, "firewalld-dns-fixed.nft.json").oxidnsListening().inspectTarget(aio); result.State != StateOK {
		t.Fatalf("fixed: %+v", result)
	}
	// The zone is read from firewalld's dispatch: eth0 by name before the default zone.
	ruleset, err := parseNFT(fixture(t, "firewalld-zone-fixed.nft.json"))
	if err != nil {
		t.Fatal(err)
	}
	if zones := firewalldZones(ruleset, []string{"eth0"}); !reflect.DeepEqual(zones, []string{"public"}) {
		t.Fatalf("zones: %v", zones)
	}
	if zones := firewalldZones(ruleset, []string{"Meta"}); !reflect.DeepEqual(zones, []string{"configflow-tun"}) {
		t.Fatalf("tun zone: %v", zones)
	}
	if zones := firewalldZones(ruleset, nil); len(zones) != 0 {
		t.Fatalf("unknown LAN: %v", zones)
	}
}

func TestLocalDNSNeedsAReachableListener(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"nothing on 53", map[string]string{}, nil},
		{"systemd-resolved stub", map[string]string{"/proc/net/udp": procHeader + procSocket("127.0.0.53", 53, "0.0.0.0", "07"), "/proc/net/tcp": procHeader + procSocket("127.0.0.53", 53, "0.0.0.0", "0A")}, nil},
		{"TUN address only", map[string]string{"/proc/net/udp": procHeader + procSocket("198.18.0.1", 53, "0.0.0.0", "07")}, nil},
		{"LAN address", map[string]string{"/proc/net/udp": procHeader + procSocket("10.97.0.20", 53, "0.0.0.0", "07")}, []string{"udp"}},
		{"connected UDP and a TCP client", map[string]string{"/proc/net/udp": procHeader + procSocket("0.0.0.0", 53, "10.97.0.200", "01"), "/proc/net/tcp": procHeader + procSocket("0.0.0.0", 53, "0.0.0.0", "06")}, nil},
		{"IPv6 wildcard", map[string]string{"/proc/net/tcp6": procHeader + procSocket("::", 53, "::", "0A"), "/proc/net/udp6": procHeader + procSocket("::", 53, "::", "07")}, []string{"udp", "tcp"}},
		{"IPv4-mapped LAN address", map[string]string{"/proc/net/udp6": procHeader + procSocket("::ffff:10.97.0.20", 53, "::", "07")}, []string{"udp"}},
	} {
		probe := Probe{ReadFile: func(path string) ([]byte, error) {
			if data, ok := tc.files[path]; ok {
				return []byte(data), nil
			}
			return nil, os.ErrNotExist
		}}
		if got := localDNSProtocols(probe, aio); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s: %v", tc.name, got)
		}
	}
	// An installed DNS service counts before it listens.
	target := aio
	target.ServesDNS = true
	if got := localDNSProtocols(Probe{}, target); !reflect.DeepEqual(got, []string{"udp", "tcp"}) {
		t.Fatalf("installed service: %v", got)
	}
	// Hijacked DNS goes through the TUN instead (flow dns), never local_dns.
	h := nftHost(t, "input-drop.nft.json", "input-drop.iptables").oxidnsListening()
	if result := h.inspectTarget(standalone); slicesContain(result.Flows, FlowLocalDNS) {
		t.Fatalf("hijacked: %+v", result.Flows)
	}
	// A DNS drop that may not apply (accept from the LAN network first) is not reported.
	h = nftHost(t, "ip-filter-present.nft.json", "").oxidnsListening()
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = "*filter\n:INPUT DROP [0:0]\n:FORWARD ACCEPT [0:0]\n-A INPUT -s 10.97.0.0/24 -j ACCEPT\n-A INPUT -m conntrack --ctstate DNAT -j ACCEPT\nCOMMIT\n"
	if result := h.inspectTarget(aio); slicesContain(result.Flows, FlowLocalDNS) {
		t.Fatalf("uncertain: %+v", result)
	}
	// Only the blocked protocol is described; UDP allowed, TCP dropped.
	h = nftHost(t, "ip-filter-present.nft.json", "").oxidnsListening()
	h.tools["iptables-save"] = "/usr/sbin/xtables-nft-multi"
	h.outputs["iptables-save -t filter"] = "*filter\n:INPUT DROP [0:0]\n:FORWARD ACCEPT [0:0]\n-A INPUT -m conntrack --ctstate DNAT -j ACCEPT\n-A INPUT -p udp -m udp --dport 53 -j ACCEPT\nCOMMIT\n"
	result := h.inspectTarget(aio)
	if !reflect.DeepEqual(result.Flows, []string{FlowLocalDNS}) || !strings.Contains(result.Hint, "客户端 TCP 方式的 DNS 查询会失败") {
		t.Fatalf("tcp only: %+v", result)
	}
	// Unknown LAN interfaces: no command opens port 53 on every interface.
	h = nftHost(t, "input-drop.nft.json", "input-drop.iptables").oxidnsListening()
	delete(h.files, "/proc/net/route")
	result = h.inspectTarget(aio)
	for _, remedy := range result.Remedies {
		for _, command := range remedy.Commands {
			if strings.Contains(command, "53") {
				t.Fatalf("unscoped DNS command: %s", command)
			}
		}
	}
	if !slicesContain(result.Flows, FlowLocalDNS) {
		t.Fatalf("unknown LAN still warns: %+v", result)
	}
}

func slicesContain(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestForwardedPingIsJudgedSeparately(t *testing.T) {
	// Plain FORWARD DROP (lab 72): UDP and ping failed.
	result := nftHost(t, "plain-drop.nft.json", "plain-drop.iptables").inspect("Meta")
	if !strings.Contains(result.Hint, "UDP（如 QUIC、语音通话、游戏）和 ping 会失败") || !strings.Contains(result.Verify, "ping") {
		t.Fatalf("plain drop: %s / %s", result.Hint, result.Verify)
	}
	if !reflect.DeepEqual(result.ForwardProtocols, []string{"udp", "icmp"}) {
		t.Fatalf("plain drop protocols: %v", result.ForwardProtocols)
	}
	// UFW accepts forwarded echo requests; ping stays out of the text.
	h := nftHost(t, "ip-filter-present.nft.json", "ufw-active.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result = h.inspect("Meta")
	if !strings.Contains(result.Hint, "主路由转来的 UDP（如 QUIC、语音通话、游戏）会失败") || strings.Contains(result.Hint, "ping") || strings.Contains(result.Verify, "ping") {
		t.Fatalf("ufw: %s / %s", result.Hint, result.Verify)
	}
	if !reflect.DeepEqual(result.ForwardProtocols, []string{"udp"}) {
		t.Fatalf("ufw protocols: %v", result.ForwardProtocols)
	}
	// Without auto-redirect TCP is forwarded too.
	h = nftHost(t, "ip-filter-present.nft.json", "ufw-active.iptables")
	h.tools["ufw"] = "/usr/sbin/ufw"
	h.ufwState = "active"
	result = h.inspectWithoutRedirect("Meta")
	if !strings.Contains(result.Hint, "主路由转来的 TCP 和 UDP（如 QUIC、语音通话、游戏）会失败") {
		t.Fatalf("ufw without auto-redirect: %s", result.Hint)
	}
	if !reflect.DeepEqual(result.ForwardProtocols, []string{"tcp", "udp"}) {
		t.Fatalf("ufw without auto-redirect protocols: %v", result.ForwardProtocols)
	}
	// Only ping dropped: the report names icmp alone, so the panel does not
	// claim UDP is blocked.
	pingOnly := strings.Replace(fixture(t, "plain-drop.iptables"), ":FORWARD DROP [17:874]\n", ":FORWARD ACCEPT [0:0]\n-A FORWARD -p icmp -j DROP\n", 1)
	h = nftHost(t, "ip-filter-present.nft.json", "plain-drop.iptables")
	h.outputs["iptables-save -t filter"] = pingOnly
	result = h.inspect("Meta")
	if !reflect.DeepEqual(result.Flows, []string{FlowForward}) || !reflect.DeepEqual(result.ForwardProtocols, []string{"icmp"}) || !strings.Contains(result.Hint, "主路由转来的 ping 会失败") || strings.Contains(result.Hint, "UDP") || !strings.Contains(result.Verify, "ping") {
		t.Fatalf("ping only: %+v", result)
	}
	echo := flow{proto: "icmp"}
	for _, tc := range []struct {
		value string
		want  tri
	}{{"8", yes}, {"echo-request", yes}, {"8/0", yes}, {"any", yes}, {"3", no}, {"0", no}} {
		name, _ := icmpTypeName(tc.value)
		if got := icmpTypeMatch([]string{name}, echo); got != tc.want {
			t.Fatalf("%s: %v", tc.value, got)
		}
	}
	if icmpTypeMatch([]string{"echo-request"}, flow{proto: "udp"}) != no || icmpTypeMatch([]string{"echo-request"}, flow{}) != maybe {
		t.Fatal("icmp type on other flows")
	}
}

func TestFirewalldPersistNoteSaysSavedOnce(t *testing.T) {
	result := firewalldHost(t, "firewalld-public.nft.json").oxidnsListening().inspectTarget(aio)
	remedy := onlyRemedy(t, result)
	if strings.Count(remedy.PersistNote, firewalldSavedNote) != 1 || !strings.Contains(remedy.PersistNote, firewalldNote) || !strings.Contains(remedy.PersistNote, firewalldDNSNote) {
		t.Fatalf("persist note: %q", remedy.PersistNote)
	}
}

func TestRunOnceNoteOnlyForNonIdempotentCommands(t *testing.T) {
	for _, note := range []string{iptablesPersistNote, dockerPersistNote, nftPersistNote} {
		if !strings.HasSuffix(note, runOnceNote) {
			t.Fatalf("missing run-once note: %q", note)
		}
	}
	for _, note := range []string{ufwNote, firewalldNote, firewalldDNSNote, firewalldSavedNote} {
		if strings.Contains(note, runOnceNote) {
			t.Fatalf("idempotent tool must not carry the run-once note: %q", note)
		}
	}
}
