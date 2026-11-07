package routes

import (
	"agent/preflight"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type gatewayFixture struct {
	cfg       *Config
	spec      LinuxGatewaySpec
	nft       []map[string]interface{}
	mutations int
	handle    int
	failWrite bool
	routes    string
	addresses string
}

func newGatewayFixture(t *testing.T) *gatewayFixture {
	t.Helper()
	oldOS, oldUID, oldRoot := gatewayOS, gatewayGeteuid, gatewaySysctlRoot
	oldRun, oldInput, oldLook := gatewayRun, gatewayRunInput, gatewayLookPath
	oldWrite := gatewayWriteSysctl
	oldCoreVersion := gatewayCoreVersion
	oldAIOSysctl := aioSysctlDir
	oldSystemForwarding, oldSysctlBoot := aioSystemForwarding, aioSysctlBoot
	// Never read the test machine's own sysctl configuration.
	aioSystemForwarding = func() (string, bool) { return "", false }
	aioSysctlBoot = func() preflight.SysctlBootState { return preflight.SysctlBootNotApplicable }
	t.Cleanup(func() {
		aioSysctlBoot = oldSysctlBoot
		gatewayOS, gatewayGeteuid, gatewaySysctlRoot = oldOS, oldUID, oldRoot
		gatewayRun, gatewayRunInput, gatewayLookPath = oldRun, oldInput, oldLook
		gatewayWriteSysctl = oldWrite
		gatewayCoreVersion = oldCoreVersion
		aioSysctlDir = oldAIOSysctl
		aioSystemForwarding = oldSystemForwarding
	})
	root := t.TempDir()
	// Never let a persisted AIO forwarding file on the test host change removal.
	aioSysctlDir = filepath.Join(root, "sysctl.d")
	gatewayOS, gatewayGeteuid, gatewaySysctlRoot = "linux", func() int { return 0 }, filepath.Join(root, "proc")
	gatewayCoreVersion = func(string) (string, error) { return "Mihomo Meta v1.19.0", nil }
	fixture := &gatewayFixture{cfg: &Config{AgentConfigFile: filepath.Join(root, "agent.json"), ConfigPath: filepath.Join(root, "mihomo.yaml"), ServiceType: "mihomo", ServiceManager: "systemd"}, spec: LinuxGatewaySpec{Interface: "eth0", Address: "192.168.1.2", LANCIDR: "192.168.1.0/24", UpstreamGateway: "192.168.1.1"}, nft: []map[string]interface{}{}, addresses: `[{"ifname":"eth0","addr_info":[{"local":"192.168.1.2","prefixlen":24}]}]`, routes: `[{"dev":"eth0","gateway":"192.168.1.1"}]`}
	initial := gatewayStateSysctls(&gatewayState{Spec: fixture.spec, TunDevice: "Meta"})
	initial["net/ipv4/conf/all/accept_redirects"] = "1"
	for key, value := range initial {
		if value == "1" {
			value = "0"
		} else {
			value = "1"
		}
		path := filepath.Join(gatewaySysctlRoot, key)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	fixture.writeConfig(t, true)
	gatewayLookPath = func(name string) (string, error) { return "", fmt.Errorf("not installed") }
	gatewayRun = func(name string, args ...string) (string, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ip -j -4 addr show":
			return fixture.addresses, nil
		case "ip -j -4 route show default":
			return fixture.routes, nil
		case "nft -j list ruleset":
			b, err := json.Marshal(map[string]interface{}{"nftables": fixture.nft})
			return string(b), err
		default:
			return "", fmt.Errorf("unexpected command: %s %v", name, args)
		}
	}
	gatewayRunInput = func(input, name string, args ...string) (string, error) {
		if name != "nft" {
			t.Fatalf("unexpected mutation: %s", name)
		}
		if len(args) > 0 && args[0] == "-c" {
			return "", nil
		}
		fixture.mutations++
		if fixture.failWrite {
			return "", fmt.Errorf("simulated transaction failure")
		}
		var doc struct {
			NFT []map[string]map[string]map[string]interface{} `json:"nftables"`
		}
		if err := json.Unmarshal([]byte(input), &doc); err != nil {
			t.Fatal(err)
		}
		for _, command := range doc.NFT {
			if added := command["add"]; added != nil {
				for kind, body := range added {
					fixture.handle++
					body["handle"] = fixture.handle
					fixture.nft = append(fixture.nft, nftObject(kind, body))
				}
			}
			if deleted := command["delete"]; deleted != nil {
				body := deleted["table"]
				if body == nil || body["family"] != "ip" || body["handle"] == nil {
					t.Fatalf("unsafe delete: %s", input)
				}
				fixture.nft = []map[string]interface{}{}
			}
		}
		return "", nil
	}
	return fixture
}

func (f *gatewayFixture) writeConfig(t *testing.T, enabled bool) {
	t.Helper()
	config := `mode: rule
tun:
  enable: true
  auto-route: true
  auto-redirect: true
  route-exclude-address: [127.0.0.0/8, 192.168.1.2/32]
  route-address-set: [cf_gateway_fake_ip, cf_gateway_rules_1]
dns:
  enable: true
  listen: 0.0.0.0:1053
  enhanced-mode: fake-ip
  fake-ip-range: 198.18.0.1/16
rule-providers:
  cf_gateway_fake_ip:
    type: inline
    behavior: ipcidr
    payload: [198.18.0.0/16]
  cf_gateway_rules_1:
    type: inline
    behavior: ipcidr
    payload: [8.8.8.0/24]
`
	if enabled {
		config += "x-configflow-linux-gateway: {enabled: true, version: 1, profile_id: test-profile, provider_prefix: cf_gateway_}\n"
	}
	if err := os.WriteFile(f.cfg.ConfigPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxGatewaySpecRejectsUnsafeInputs(t *testing.T) {
	base := LinuxGatewaySpec{Interface: "eth0", Address: "192.168.1.2", LANCIDR: "192.168.1.0/24", UpstreamGateway: "192.168.1.1"}
	for _, mutation := range []func(*LinuxGatewaySpec){func(s *LinuxGatewaySpec) { s.Interface = "eth0;id" }, func(s *LinuxGatewaySpec) { s.Interface = "../all" }, func(s *LinuxGatewaySpec) { s.Address = "192.168.1.255" }, func(s *LinuxGatewaySpec) { s.Address = "192.168.1.0" }, func(s *LinuxGatewaySpec) { s.Address = "::1" }, func(s *LinuxGatewaySpec) { s.LANCIDR = "192.168.1.2/24" }, func(s *LinuxGatewaySpec) { s.LANCIDR = "0.0.0.0/0" }, func(s *LinuxGatewaySpec) { s.UpstreamGateway = s.Address }, func(s *LinuxGatewaySpec) { s.UpstreamGateway = "10.0.0.1" }} {
		spec := base
		mutation(&spec)
		if err := validateLinuxGatewaySpec(spec); err == nil {
			t.Fatalf("accepted unsafe spec: %+v", spec)
		}
	}
	if err := validateLinuxGatewaySpec(base); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxGatewayPreviewIsReadOnlyAndShowsDeploymentBlocker(t *testing.T) {
	f := newGatewayFixture(t)
	f.writeConfig(t, false)
	preview := gatewayPreview(f.cfg, f.spec, "test-profile")
	if preview["ready"] != false || len(preview["commands"].([]string)) == 0 || len(preview["blockers"].([]string)) == 0 {
		t.Fatalf("invalid preview: %+v", preview)
	}
	if f.mutations != 0 {
		t.Fatal("preview changed nftables")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.cfg.AgentConfigFile), "linux-gateway.json")); !os.IsNotExist(err) {
		t.Fatal("preview wrote journal")
	}
	data, _ := os.ReadFile(filepath.Join(gatewaySysctlRoot, "net/ipv4/ip_forward"))
	if string(data) != "0" {
		t.Fatal("preview enabled forwarding")
	}
	detection := preview["detection"].(gatewayDetection)
	if detection.Suggested == nil || *detection.Suggested != f.spec {
		t.Fatalf("bad detection: %+v", detection)
	}
}

func TestLinuxGatewayApplyRequiresOptInProfileAndMatchingNetwork(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*gatewayFixture)
	}{
		{"disabled", func(f *gatewayFixture) { f.writeConfig(t, false) }},
		{"platform", func(f *gatewayFixture) { gatewayOS = "darwin" }},
		{"oxidns", func(f *gatewayFixture) { f.cfg.ServiceType = "oxidns" }},
		{"docker", func(f *gatewayFixture) { f.cfg.ServiceManager = "supervisor" }},
		{"address", func(f *gatewayFixture) { f.spec.Address = "192.168.1.3" }},
		{"default route", func(f *gatewayFixture) { f.routes = `[{"dev":"eth0","gateway":"192.168.1.254"}]` }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newGatewayFixture(t)
			test.change(f)
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
				t.Fatal("unsafe apply accepted")
			}
			if f.mutations != 0 {
				t.Fatal("mutation before failed preflight")
			}
		})
	}
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "other-profile"); err == nil {
		t.Fatal("accepted stale profile config")
	}
	if f.mutations != 0 {
		t.Fatal("mutated on profile mismatch")
	}
}

func TestLinuxGatewayApplyIsIdempotentAndRemovalRestores(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if err := checkGatewaySysctls(&gatewayState{Spec: f.spec, TunDevice: "Meta"}); err != nil {
		t.Fatal(err)
	}
	state, err := loadGatewayState(f.cfg)
	if err != nil || state == nil || !state.Active || state.Pending {
		t.Fatalf("invalid state: %+v %v", state, err)
	}
	if result := gatewayManaged(f.cfg); result["healthy"] != true {
		t.Fatalf("not healthy: %+v", result)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if f.mutations != 1 {
		t.Fatalf("idempotent apply rewrote firewall %d times", f.mutations)
	}
	f.writeConfig(t, false)
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	if len(f.nft) != 0 {
		t.Fatal("table retained")
	}
	for key, old := range state.Original {
		data, _ := os.ReadFile(filepath.Join(gatewaySysctlRoot, key))
		if string(data) != old {
			t.Errorf("%s restored %q want %q", key, data, old)
		}
	}
	if state, err := loadGatewayState(f.cfg); err != nil || state != nil {
		t.Fatalf("journal retained: %+v %v", state, err)
	}
}

func TestLinuxGatewayRebootRecreatesOnlyOwnedTable(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	f.nft = []map[string]interface{}{} // A host reboot clears the runtime nftables state.
	if result := gatewayManaged(f.cfg); result["healthy"] != false {
		t.Fatal("missing nft falsely healthy")
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	if f.mutations != 2 || len(f.nft) != 6 {
		t.Fatalf("bad replay: %d, %+v", f.mutations, f.nft)
	}
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
}

func TestLinuxGatewayPreservesExternalChanges(t *testing.T) {
	t.Run("foreign table", func(t *testing.T) {
		f := newGatewayFixture(t)
		f.nft = []map[string]interface{}{nftObject("table", map[string]interface{}{"family": "ip", "name": gatewayTable, "handle": 2})}
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
			t.Fatal("adopted foreign table")
		}
		if f.mutations != 0 {
			t.Fatal("modified foreign table")
		}
	})
	t.Run("extra rule", func(t *testing.T) {
		f := newGatewayFixture(t)
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
			t.Fatal(err)
		}
		f.nft = append(f.nft, nftObject("rule", map[string]interface{}{"family": "ip", "table": gatewayTable, "chain": "forward", "expr": []interface{}{map[string]interface{}{"drop": nil}}}))
		if err := removeLinuxGateway(f.cfg); err == nil {
			t.Fatal("deleted externally edited table")
		}
		if f.mutations != 1 {
			t.Fatal("changed externally edited table")
		}
	})
	t.Run("sysctl drift", func(t *testing.T) {
		f := newGatewayFixture(t)
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(gatewaySysctlRoot, "net/ipv4/conf/eth0/send_redirects")
		if err := os.WriteFile(path, []byte("1"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := removeLinuxGateway(f.cfg); err != nil {
			t.Fatal(err)
		}
		data, _ := os.ReadFile(path)
		if string(data) != "1" {
			t.Fatal("overwrote external sysctl")
		}
	})
	t.Run("replaced handle", func(t *testing.T) {
		f := newGatewayFixture(t)
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
			t.Fatal(err)
		}
		f.nft[0]["table"].(map[string]interface{})["handle"] = 9000
		if err := removeLinuxGateway(f.cfg); err == nil {
			t.Fatal("deleted replacement resource with different identity")
		}
		if f.mutations != 1 {
			t.Fatal("changed replacement table")
		}
	})
}

func TestLinuxGatewayRejectsConflictingFirewallAndReturnRouting(t *testing.T) {
	t.Run("forward drop", func(t *testing.T) {
		f := newGatewayFixture(t)
		f.nft = []map[string]interface{}{nftObject("chain", map[string]interface{}{"family": "inet", "table": "filter", "name": "forward", "hook": "forward", "policy": "drop"})}
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
			t.Fatal("ignored forward drop")
		}
		if f.mutations != 0 {
			t.Fatal("wrote while firewall conflicts")
		}
	})
	t.Run("return journal", func(t *testing.T) {
		f := newGatewayFixture(t)
		if err := saveReturnState(f.cfg, &returnRoutingState{Active: true}); err != nil {
			t.Fatal(err)
		}
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
			t.Fatal("ignored active return routing")
		}
	})
	t.Run("return cannot start after gateway", func(t *testing.T) {
		f := newGatewayFixture(t)
		if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyReturnRouting(f.cfg, ReturnRoutingSpec{}); err == nil || !strings.Contains(err.Error(), "Linux 网关") {
			t.Fatalf("wrong return routing guard: %v", err)
		}
	})
}

func TestLinuxGatewayFailedTransactionLeavesRecoverableJournal(t *testing.T) {
	f := newGatewayFixture(t)
	f.failWrite = true
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err == nil {
		t.Fatal("ignored nft transaction failure")
	}
	state, err := loadGatewayState(f.cfg)
	if err != nil || state == nil || !state.Pending || state.Active {
		t.Fatalf("missing pending state: %+v %v", state, err)
	}
	f.failWrite = false
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(gatewaySysctlRoot, "net/ipv4/ip_forward"))
	if string(data) != "0" {
		t.Fatal("forwarding incorrectly enabled")
	}
}

func TestLinuxGatewayHTTPStrictInputAndProfile(t *testing.T) {
	f := newGatewayFixture(t)
	for _, body := range []string{`{"interface":"eth0","command":"rm -rf /"}`, `{}` + `{}`} {
		recorder := httptest.NewRecorder()
		LinuxGatewayHandler(f.cfg, "preview")(recorder, httptest.NewRequest(http.MethodPost, "/api/linux-gateway/preview", strings.NewReader(body)))
		if recorder.Code != 400 {
			t.Fatalf("accepted unknown input %s: %d", body, recorder.Code)
		}
	}
	body, _ := json.Marshal(map[string]string{"interface": f.spec.Interface, "address": f.spec.Address, "lan_cidr": f.spec.LANCIDR, "upstream_gateway": f.spec.UpstreamGateway, "profile_id": "test-profile"})
	recorder := httptest.NewRecorder()
	LinuxGatewayHandler(f.cfg, "preview")(recorder, httptest.NewRequest(http.MethodPost, "/api/linux-gateway/preview", bytes.NewReader(body)))
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"ready":true`) {
		t.Fatalf("preview failed: %s", recorder.Body.String())
	}
	if f.mutations != 0 {
		t.Fatal("HTTP preview mutated host")
	}
}

func TestLinuxGatewayPreservesKernelSysctlSideEffects(t *testing.T) {
	f := newGatewayFixture(t)
	other := map[string]string{"net/ipv4/conf/eth1/forwarding": "0", "net/ipv4/conf/eth1/send_redirects": "1", "net/ipv4/conf/lo/forwarding": "1", "net/ipv4/conf/all/accept_redirects": "0"}
	for key, value := range other {
		path := filepath.Join(gatewaySysctlRoot, key)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Emulate Linux inet_forward_change and devinet_copy_dflt_conf instead of
	// relying on regular temporary files to behave like procfs.
	gatewayWriteSysctl = func(key, value string) error {
		if err := os.WriteFile(filepath.Join(gatewaySysctlRoot, key), []byte(value), 0644); err != nil {
			return err
		}
		field := ""
		if key == "net/ipv4/ip_forward" {
			field = "forwarding"
			redirect := "0"
			if value == "0" {
				redirect = "1"
			}
			if err := os.WriteFile(filepath.Join(gatewaySysctlRoot, "net/ipv4/conf/all/accept_redirects"), []byte(redirect), 0644); err != nil {
				return err
			}
		} else if strings.HasPrefix(key, "net/ipv4/conf/default/") {
			field = filepath.Base(key)
		}
		if field != "" {
			entries, _ := os.ReadDir(filepath.Join(gatewaySysctlRoot, "net/ipv4/conf"))
			for _, entry := range entries {
				path := filepath.Join(gatewaySysctlRoot, "net/ipv4/conf", entry.Name(), field)
				if _, err := os.Stat(path); err == nil {
					if err := os.WriteFile(path, []byte(value), 0644); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
	assertPreserved := func() {
		for key, expected := range other {
			value, err := gatewayReadSysctl(key)
			if err != nil || value != expected {
				t.Errorf("unrelated %s=%q expected %q (%v)", key, value, expected, err)
			}
		}
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	assertPreserved()
	if err := removeLinuxGateway(f.cfg); err != nil {
		t.Fatal(err)
	}
	assertPreserved()
}

func TestLinuxGatewayAcceptOnlyForwardChainsRemainCompatible(t *testing.T) {
	f := newGatewayFixture(t)
	// The shape produced by nft for an established-flow accept chain, such as
	// the forwarding allowances installed alongside a transparent proxy.
	raw := `{"nftables":[{"chain":{"family":"inet","table":"mihomo","name":"forward","type":"filter","hook":"forward","prio":0,"policy":"accept"}},{"rule":{"family":"inet","table":"mihomo","chain":"forward","expr":[{"match":{"op":"in","left":{"ct":{"key":"state"}},"right":{"set":["established","related"]}}},{"counter":{"packets":42,"bytes":1000}},{"accept":null}]}}]}`
	var parsed struct {
		NFT []map[string]interface{} `json:"nftables"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatal(err)
	}
	f.nft = parsed.NFT
	if _, err := gatewayPreflightResources(f.cfg, nil); err != nil {
		t.Fatal(err)
	}
	rule := f.nft[1]["rule"].(map[string]interface{})
	rule["expr"] = []interface{}{map[string]interface{}{"jump": map[string]interface{}{"target": "other"}}}
	if _, err := gatewayPreflightResources(f.cfg, nil); err == nil {
		t.Fatal("trusted unknown jump target")
	}
}

func TestLinuxGatewayReconcileOnlyExistingJournalAndDisabledCleanup(t *testing.T) {
	f := newGatewayFixture(t)
	reconcileLinuxGateway(f.cfg)
	if f.mutations != 0 {
		t.Fatal("reconcile opted in a host without journal")
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	f.writeConfig(t, false)
	// Publishing disabled can remove the TUN interface before reconciliation.
	if err := os.Remove(filepath.Join(gatewaySysctlRoot, "net/ipv4/conf/Meta/forwarding")); err != nil {
		t.Fatal(err)
	}
	reconcileLinuxGateway(f.cfg)
	if len(f.nft) != 0 {
		t.Fatal("disabled deployment left owned firewall")
	}
	state, err := loadGatewayState(f.cfg)
	if err != nil || state != nil {
		t.Fatalf("disabled cleanup retained journal: %+v %v", state, err)
	}
}

func TestLinuxGatewayRejectsReboundJournalAndTamperedManifest(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(f.cfg.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.cfg.ConfigPath, bytes.ReplaceAll(data, []byte("test-profile"), []byte("next-profile")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := applyLinuxGateway(f.cfg, f.spec, "next-profile"); err == nil {
		t.Fatal("adopted old profile journal")
	}
	state, err := loadGatewayState(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	state.NFT = state.NFT[:len(state.NFT)-1]
	if err := saveGatewayState(f.cfg, state); err != nil {
		t.Fatal(err)
	}
	if err := removeLinuxGateway(f.cfg); err == nil {
		t.Fatal("trusted incomplete journal")
	}
	if f.mutations != 1 {
		t.Fatal("tampered journal changed firewall")
	}
}

func TestLinuxGatewayRejectsOldTunFilters(t *testing.T) {
	for _, item := range []struct {
		key, value string
		allowed    bool
	}{{"include-interface", "[eth1]", false}, {"exclude-interface", "[eth0]", false}, {"exclude-uid", "[0]", false}, {"include-mac-address", "[aa:bb:cc:dd:ee:ff]", false}, {"exclude-package", "[]", true}, {"include-interface", "null", true}} {
		t.Run(item.key+item.value, func(t *testing.T) {
			f := newGatewayFixture(t)
			data, err := os.ReadFile(f.cfg.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			data = bytes.Replace(data, []byte("tun:\n"), []byte("tun:\n  "+item.key+": "+item.value+"\n"), 1)
			if err := os.WriteFile(f.cfg.ConfigPath, data, 0600); err != nil {
				t.Fatal(err)
			}
			err = checkGatewayConfig(f.cfg, f.spec, "test-profile")
			if (err == nil) != item.allowed {
				t.Fatalf("filter %s=%s: %v", item.key, item.value, err)
			}
		})
	}
}

func TestLinuxGatewayBootWaitsForLateTunWithoutPartialMutation(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	f.nft = []map[string]interface{}{}
	tunPath := filepath.Join(gatewaySysctlRoot, "net/ipv4/conf/Meta/forwarding")
	if err := os.Remove(tunPath); err != nil {
		t.Fatal(err)
	}
	waits := 0
	err := reapplyLinuxGateway(f.cfg, 3, func() {
		waits++
		state, err := loadGatewayState(f.cfg)
		if err != nil || state == nil || state.Pending || !state.Active {
			t.Fatalf("readiness changed journal: %+v %v", state, err)
		}
		if f.mutations != 1 || len(f.nft) != 0 {
			t.Fatal("waiting for TUN mutated firewall")
		}
		if err := os.WriteFile(tunPath, []byte("0"), 0644); err != nil {
			t.Fatal(err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if waits != 1 || f.mutations != 2 {
		t.Fatalf("bad bounded replay waits=%d mutations=%d", waits, f.mutations)
	}
	if result := gatewayManaged(f.cfg); result["healthy"] != true {
		t.Fatalf("late TUN replay unhealthy: %+v", result)
	}
}

func TestLinuxGatewayBootDoesNotRetryOwnershipConflict(t *testing.T) {
	f := newGatewayFixture(t)
	if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
		t.Fatal(err)
	}
	f.nft[0]["table"].(map[string]interface{})["handle"] = 8000
	waits := 0
	err := reapplyLinuxGateway(f.cfg, 3, func() { waits++ })
	if err == nil || waits != 0 || f.mutations != 1 {
		t.Fatalf("retried or changed conflicting owner: %v waits=%d mutations=%d", err, waits, f.mutations)
	}
}

func TestLinuxGatewayRemovalPreservesPersistedAIOForwarding(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
		remove  func(*Config) error
		keep    bool
	}{
		{"remove", aioForwardingContent, removeLinuxGateway, true},
		{"reconcile", aioForwardingContent, reconcileLinuxGateway, true},
		// A modified persistence file does not prove ownership: restore as before.
		{"tampered", aioForwardingContent + "net.ipv4.conf.all.forwarding=1\n", removeLinuxGateway, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newGatewayFixture(t)
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
				t.Fatal(err)
			}
			state, err := loadGatewayState(f.cfg)
			if err != nil || state == nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(aioSysctlDir, 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(aioForwardingConfigPath(), []byte(test.content), 0644); err != nil {
				t.Fatal(err)
			}
			if gatewayManaged(f.cfg)["preserves_forwarding"] != test.keep {
				t.Fatal("managed status does not report forwarding preservation")
			}
			// Deploying a full TUN configuration drops the gateway marker.
			f.writeConfig(t, false)
			if err := test.remove(f.cfg); err != nil {
				t.Fatal(err)
			}
			for key, original := range state.Original {
				want := original
				if test.keep && (key == "net/ipv4/ip_forward" || strings.HasSuffix(key, "/forwarding")) {
					want = "1"
				}
				if value, err := gatewayReadSysctl(key); err != nil || value != want {
					t.Errorf("%s=%q want %q (%v)", key, value, want, err)
				}
			}
			if state.Original["net/ipv4/conf/all/send_redirects"] != "1" {
				t.Fatal("fixture must prove send_redirects is restored, not merely kept")
			}
			if len(f.nft) != 0 {
				t.Fatal("owned firewall retained")
			}
			if state, err := loadGatewayState(f.cfg); err != nil || state != nil {
				t.Fatalf("journal retained: %+v %v", state, err)
			}
		})
	}
}
