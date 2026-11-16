package installer

// Port of the shell scenarios of backend/test_gateway_network_probe.py
// (module A10, decision 2): the embedded detect-gateway-network.sh run with
// sh against a stub `ip` command. `_spec` (A19's linux_gateway validation)
// is replaced by checking the exact JSON answer.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const probeStubIP = `#!/bin/sh
printf "%s\n" "$*" >> "$PROBE_LOG"
case "$*" in
    '-4 -o route show default')
        [ "$PROBE_ROUTE_ERROR" = 0 ] || exit 1
        cat "$PROBE_ROUTES_FILE" ;;
    "-4 -o addr show dev $PROBE_INTERFACE")
        [ "$PROBE_ADDRESS_ERROR" = 0 ] || exit 1
        cat "$PROBE_ADDRESSES_FILE" ;;
    *) printf 'unexpected ip command: %s\n' "$*" >&2; exit 91 ;;
esac
`

type probeOptions struct {
	interface_               string
	routeError, addressError bool
}

func runProbe(t *testing.T, route, addresses string, o probeOptions) runResult {
	t.Helper()
	if o.interface_ == "" {
		o.interface_ = "ens18"
	}
	dir := t.TempDir()
	commands := filepath.Join(dir, "commands")
	os.Mkdir(commands, 0o755)
	os.WriteFile(filepath.Join(commands, "ip"), []byte(probeStubIP), 0o755)
	os.WriteFile(filepath.Join(dir, "routes"), []byte(route+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "addresses"), []byte(addresses+"\n"), 0o644)
	script := filepath.Join(dir, "probe.sh")
	os.WriteFile(script, []byte(GatewayNetworkProbe), 0o644)
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	env := []string{"PATH=" + commands + ":/usr/bin:/bin", "PROBE_LOG=" + filepath.Join(dir, "commands.log"),
		"PROBE_INTERFACE=" + o.interface_, "PROBE_ROUTES_FILE=" + filepath.Join(dir, "routes"),
		"PROBE_ADDRESSES_FILE=" + filepath.Join(dir, "addresses"),
		"PROBE_ROUTE_ERROR=" + flag(o.routeError), "PROBE_ADDRESS_ERROR=" + flag(o.addressError)}
	result := runShell(t, "sh", script, runOptions{env: env, timeout: 5 * time.Second})
	log, _ := os.ReadFile(filepath.Join(dir, "commands.log"))
	calls := strings.Split(strings.TrimRight(string(log), "\n"), "\n")
	if !reflect.DeepEqual(calls, []string{"-4 -o route show default"}) &&
		!reflect.DeepEqual(calls, []string{"-4 -o route show default", "-4 -o addr show dev " + o.interface_}) {
		t.Fatalf("ip calls %q", calls)
	}
	return result
}

const probeRoute = "default via 192.168.8.1 dev ens18 proto dhcp metric 100"
const probeAddress = "2: ens18 inet 192.168.8.23/24 brd 192.168.8.255 scope global dynamic ens18"

func TestProbeUsesActualDefaultRouteInterface(t *testing.T) {
	for _, iface := range []string{"eth0", "enp2s0", "ens18", "br0", "bond0.10", "vlan-20", "_lan0"} {
		r := runProbe(t, "default via 192.168.8.1 dev "+iface+" proto static",
			"5: "+iface+"@if4 inet 192.168.8.23/24 brd 192.168.8.255 scope global "+iface, probeOptions{interface_: iface})
		if r.code != 0 || r.stderr != "" {
			t.Fatalf("%s: %d %s", iface, r.code, r.stderr)
		}
		var body map[string]any
		if err := json.Unmarshal([]byte(r.stdout), &body); err != nil {
			t.Fatal(err)
		}
		want := map[string]any{"version": 1.0, "interface": iface, "address": "192.168.8.23", "lan_cidr": "192.168.8.0/24", "upstream_gateway": "192.168.8.1"}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("%s: %v", iface, body)
		}
	}
}

func TestProbeCalculatesCanonicalNetwork(t *testing.T) {
	for _, c := range [][4]string{{"10.42.20.7", "8", "10.0.0.1", "10.0.0.0"}, {"172.20.31.23", "20", "172.20.16.1", "172.20.16.0"},
		{"192.168.8.6", "30", "192.168.8.5", "192.168.8.4"}} {
		r := runProbe(t, "default via "+c[2]+" dev ens18", "2: ens18 inet "+c[0]+"/"+c[1]+" scope global ens18", probeOptions{})
		var body map[string]any
		if r.code != 0 || json.Unmarshal([]byte(r.stdout), &body) != nil || body["lan_cidr"] != c[3]+"/"+c[1] {
			t.Errorf("%v: %d %q %q", c, r.code, r.stdout, r.stderr)
		}
	}
}

func probeRefuses(t *testing.T, r runResult, message string) {
	t.Helper()
	if r.code == 0 || r.stdout != "" || !strings.Contains(r.stderr, message) {
		t.Errorf("want refusal containing %q: %d %q %q", message, r.code, r.stdout, r.stderr)
	}
}

func TestProbeRefusesMissingOrAmbiguousRoutes(t *testing.T) {
	for _, c := range [][2]string{
		{"", "未找到"},
		{"default via 192.168.8.1 dev ens18\ndefault via 10.0.0.1 dev br0 metric 200", "多个"},
		{"default nexthop via 192.168.8.1 dev ens18 weight 1 nexthop via 10.0.0.1 dev br0 weight 1", "多路径"},
		{"default nhid 11 via 192.168.8.1 dev ens18", "多路径"},
		{"default dev ens18 scope link", "缺少"},
		{"default via 192.168.8.1", "缺少"},
		{"default via 192.168.8.1 dev lo", "网卡名"},
		{"default via 192.168.8.1 dev ens18;id", "网卡名"},
		{"default via 192.168.8.1 dev abcdefghijklmnop", "网卡名"},
		{"default via 192.168.8.1 dev ens18 linkdown", "未连接"},
	} {
		probeRefuses(t, runProbe(t, c[0], probeAddress, probeOptions{}), c[1])
	}
}

func TestProbeRefusesAmbiguousOrUnusableAddresses(t *testing.T) {
	for _, c := range [][2]string{
		{"", "没有可用"},
		{"2: ens18 inet 192.168.8.23/24 scope global ens18\n2: ens18 inet 192.168.8.24/24 scope global secondary ens18", "多个"},
		{"2: ens18 inet 192.168.8.23/24 scope global deprecated ens18", "尚未可用"},
		{"2: ens18 inet 192.168.8.23/24 scope global tentative ens18", "尚未可用"},
		{"2: ens18 inet 192.168.8.23/24 scope global dadfailed ens18", "尚未可用"},
		{"2: ens18 inet 192.168.8.23/31 scope global ens18", "掩码"},
		{"2: ens18 inet 192.168.8.23/7 scope global ens18", "掩码"},
		{"2: ens18 inet 192.168.8.0/24 scope global ens18", "网络或广播"},
		{"2: ens18 inet 192.168.8.255/24 scope global ens18", "网络或广播"},
		{"2: ens18 inet 192.168.8.1/24 scope global ens18", "不能相同"},
		{"2: ens18 inet 192.168.08.23/24 scope global ens18", "有效"},
		{"2: ens18 inet 192.168.8.256/24 scope global ens18", "有效"},
	} {
		probeRefuses(t, runProbe(t, probeRoute, c[0], probeOptions{}), c[1])
	}
}

func TestProbeRejectsUnusableHostAddresses(t *testing.T) {
	for _, c := range [][2]string{{"192.168.8.23", "10.0.0.1"}, {"192.168.8.23", "192.168.8.0"}, {"192.168.8.23", "192.168.8.255"},
		{"192.168.8.23", "bad"}, {"127.0.0.2", "127.0.0.1"}, {"169.254.8.2", "169.254.8.1"}, {"224.0.0.2", "224.0.0.1"},
		{"0.0.0.0", "0.0.0.1"}, {"0.1.2.3", "0.1.2.1"}, {"240.0.0.2", "240.0.0.1"}} {
		probeRefuses(t, runProbe(t, "default via "+c[1]+" dev ens18", "2: ens18 inet "+c[0]+"/24 scope global ens18", probeOptions{}), "")
	}
}

func TestProbeReportsIPCommandFailures(t *testing.T) {
	probeRefuses(t, runProbe(t, probeRoute, probeAddress, probeOptions{routeError: true}), "无法读取")
	probeRefuses(t, runProbe(t, probeRoute, probeAddress, probeOptions{addressError: true}), "无法读取")
}

func TestProbeHasPOSIXSyntaxAndNoPrivilegedCommands(t *testing.T) {
	script := filepath.Join(t.TempDir(), "probe.sh")
	os.WriteFile(script, []byte(GatewayNetworkProbe), 0o644)
	if out, err := exec.Command("sh", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	for _, command := range []string{"python", "jq ", "id -u", "sudo ", "sysctl ", "nft ", "ip route add"} {
		if strings.Contains(GatewayNetworkProbe, command) {
			t.Errorf("probe contains %q", command)
		}
	}
}
