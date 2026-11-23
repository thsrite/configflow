package routes

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type haFixture struct {
	cfg       *Config
	spec      OxiDNSHASpec
	addresses string
	running   bool
	enabled   bool
	fail      map[string]error
	commands  []string
	missing   map[string]bool
}

func newHAFixture(t *testing.T) *haFixture {
	t.Helper()
	oldOS, oldUID, oldLook, oldExe := haOS, haGeteuid, haLookPath, haExecutable
	oldRun, oldSleep, oldDir, oldSecure, oldProbe := haRun, haSleep, haKeepalivedDir, haScriptSecure, haHandoffProbe
	t.Cleanup(func() {
		haOS, haGeteuid, haLookPath, haExecutable = oldOS, oldUID, oldLook, oldExe
		haRun, haSleep, haKeepalivedDir, haScriptSecure, haHandoffProbe = oldRun, oldSleep, oldDir, oldSecure, oldProbe
	})
	root := t.TempDir()
	f := &haFixture{
		cfg:       &Config{AgentConfigFile: filepath.Join(root, "agent", "config-oxidns.json"), ConfigPath: filepath.Join(root, "oxidns.yaml"), ServiceType: "oxidns", ServiceManager: "systemd"},
		spec:      OxiDNSHASpec{SafetyVersion: 1, VIP: "192.168.0.2", VRID: 53, Priority: 150, SelfIP: "192.168.0.20", PeerIP: "192.168.0.21"},
		addresses: `[{"ifname":"lo","addr_info":[{"local":"127.0.0.1","prefixlen":8}]},{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24}]}]`,
		fail:      map[string]error{},
		missing:   map[string]bool{},
	}
	if err := os.MkdirAll(filepath.Dir(f.cfg.AgentConfigFile), 0700); err != nil {
		t.Fatal(err)
	}
	f.writeOxiDNS(t, "0.0.0.0:53")
	haOS, haGeteuid = "linux", func() int { return 0 }
	haKeepalivedDir = filepath.Join(root, "keepalived")
	if err := os.MkdirAll(haKeepalivedDir, 0755); err != nil {
		t.Fatal(err)
	}
	haExecutable = func() (string, error) { return "/opt/configflow-agent/configflow-agent", nil }
	haScriptSecure = func(string) error { return nil }
	haSleep = func(time.Duration) {}
	haHandoffProbe = func(string, string, time.Duration) error { return nil }
	haLookPath = func(name string) (string, error) {
		if f.missing[name] {
			return "", fmt.Errorf("not found")
		}
		return "/usr/sbin/" + name, nil
	}
	haRun = func(name string, args ...string) (string, error) {
		command := name + " " + strings.Join(args, " ")
		if err := f.fail[command]; err != nil {
			f.commands = append(f.commands, command)
			return "", err
		}
		switch command {
		case "ip -j -4 addr show":
			return f.addresses, nil
		case "systemctl is-active --quiet keepalived":
			if f.running {
				return "", nil
			}
			return "", fmt.Errorf("exit status 3")
		case "systemctl is-enabled --quiet keepalived":
			if f.enabled {
				return "", nil
			}
			return "", fmt.Errorf("exit status 1")
		}
		f.commands = append(f.commands, command)
		switch command {
		case "systemctl enable keepalived":
			f.enabled = true
		case "systemctl disable keepalived":
			f.enabled = false
		case "systemctl start keepalived", "systemctl restart keepalived":
			f.running = true
		case "systemctl stop keepalived":
			f.running = false
		default:
			t.Fatalf("unexpected command: %s", command)
		}
		return "", nil
	}
	return f
}

func (f *haFixture) writeOxiDNS(t *testing.T, listen string) {
	t.Helper()
	content := "plugins:\n  - tag: main_sequence\n    type: sequence\n    args:\n      - exec: accept\n  - tag: udp_server\n    type: udp_server\n    args:\n      listen: " + listen + "\n      entry: main_sequence\n"
	if err := os.WriteFile(f.cfg.ConfigPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *haFixture) preview(t *testing.T, spec OxiDNSHASpec) map[string]any {
	t.Helper()
	body, _ := json.Marshal(spec)
	recorder := httptest.NewRecorder()
	OxiDNSHAPreviewHandler(f.cfg)(recorder, httptest.NewRequest(http.MethodPost, "/api/oxidns-ha/preview", strings.NewReader(string(body))))
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status %d: %s", recorder.Code, recorder.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func readText(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestOxiDNSHASpecValidation(t *testing.T) {
	valid := OxiDNSHASpec{SafetyVersion: 1, VIP: "192.168.0.2", VRID: 53, Priority: 100, SelfIP: "192.168.0.20", PeerIP: "192.168.0.21"}
	if err := validateOxiDNSHASpec(valid); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*OxiDNSHASpec){
		"version":       func(s *OxiDNSHASpec) { s.SafetyVersion = 2 },
		"vrid-zero":     func(s *OxiDNSHASpec) { s.VRID = 0 },
		"vrid-high":     func(s *OxiDNSHASpec) { s.VRID = 256 },
		"priority":      func(s *OxiDNSHASpec) { s.Priority = 120 },
		"vip-ipv6":      func(s *OxiDNSHASpec) { s.VIP = "fd00::2" },
		"vip-multicast": func(s *OxiDNSHASpec) { s.VIP = "224.0.0.18" },
		"vip-loopback":  func(s *OxiDNSHASpec) { s.VIP = "127.0.0.2" },
		"vip-padded":    func(s *OxiDNSHASpec) { s.VIP = "192.168.000.2" },
		"self-empty":    func(s *OxiDNSHASpec) { s.SelfIP = "" },
		"peer-bcast":    func(s *OxiDNSHASpec) { s.PeerIP = "255.255.255.255" },
		"vip-is-self":   func(s *OxiDNSHASpec) { s.VIP = s.SelfIP },
		"vip-is-peer":   func(s *OxiDNSHASpec) { s.VIP = s.PeerIP },
		"self-is-peer":  func(s *OxiDNSHASpec) { s.PeerIP = s.SelfIP },
	} {
		spec := valid
		change(&spec)
		if err := validateOxiDNSHASpec(spec); err == nil {
			t.Errorf("%s: invalid spec accepted", name)
		}
	}
}

func TestOxiDNSHAPreviewReportsNetworkWithoutChanges(t *testing.T) {
	f := newHAFixture(t)
	result := f.preview(t, f.spec)
	if result["ok"] != true || result["interface"] != "eth0" || result["cidr"] != "192.168.0.0/24" {
		t.Fatalf("unexpected preview: %#v", result)
	}
	if len(f.commands) != 0 {
		t.Fatalf("preview changed the host: %v", f.commands)
	}
	if entries, _ := os.ReadDir(haKeepalivedDir); len(entries) != 0 {
		t.Fatal("preview wrote keepalived files")
	}
	if state, err := loadHAState(f.cfg); err != nil || state != nil {
		t.Fatalf("preview wrote a journal: %v", err)
	}
}

// haRPMSample is the start of the keepalived.conf that RHEL/Fedora packages install.
const haRPMSample = `! Configuration File for keepalived

global_defs {
   notification_email {
     acassen@firewall.loc
   }
   router_id LVS_DEVEL
   vrrp_skip_check_adv_addr
   vrrp_strict
   vrrp_garp_interval 0
   vrrp_gna_interval 0
}

vrrp_instance VI_1 {
    state MASTER
    interface eth0
    virtual_router_id 51
    priority 100
    advert_int 1
    authentication {
        auth_type PASS
        auth_pass 1111
    }
    virtual_ipaddress {
        192.168.200.16
        192.168.200.17
        192.168.200.18
    }
}

virtual_server 192.168.200.100 443 {
    delay_loop 6
    lb_algo rr
    lb_kind NAT
    protocol TCP
}
`

func TestOxiDNSHAPreviewBlockers(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*testing.T, *haFixture, *OxiDNSHASpec)
		want   []string
	}{
		{"tools", func(_ *testing.T, f *haFixture, _ *OxiDNSHASpec) {
			f.missing["keepalived"], f.missing["ip"] = true, true
		},
			[]string{"keepalived、ip", "apt install keepalived", "apk add keepalived", "dnf install keepalived"}},
		{"specific-listener", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) { f.writeOxiDNS(t, "192.168.0.20:53") }, []string{"OxiDNS 只监听了具体地址"}},
		{"no-53-listener", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) { f.writeOxiDNS(t, "0.0.0.0:5353") }, []string{"没有在 53 端口监听"}},
		{"ipv6-wildcard-ok", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) { f.writeOxiDNS(t, ":53") }, nil},
		{"self-missing", func(_ *testing.T, _ *haFixture, s *OxiDNSHASpec) { s.SelfIP = "192.168.0.30" }, []string{"本机网卡上没有地址 192.168.0.30"}},
		{"vip-outside", func(_ *testing.T, _ *haFixture, s *OxiDNSHASpec) { s.VIP = "192.168.1.2" }, []string{"必须是本机网段 192.168.0.0/24"}},
		{"peer-outside", func(_ *testing.T, _ *haFixture, s *OxiDNSHASpec) { s.PeerIP = "10.0.0.21" }, []string{"必须是本机网段"}},
		{"vip-held", func(_ *testing.T, f *haFixture, _ *OxiDNSHASpec) {
			f.addresses = `[{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24},{"local":"192.168.0.2","prefixlen":24}]}]`
		}, []string{"已在本机网卡上"}},
		{"vrid-conflict", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte("vrrp_instance VI_1 {\n    virtual_router_id 53 # mine\n}\n"), 0644)
		}, []string{"virtual_router_id 为 53"}},
		{"vrid-comment-ignored", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) {
			f.running = true
			os.WriteFile(haMainConfigPath(), []byte("# virtual_router_id 53\nvrrp_instance VI_1 {\n    virtual_router_id 54\n}\n"), 0644)
		}, nil},
		{"vrrp-strict", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) {
			f.running = true
			os.WriteFile(haMainConfigPath(), []byte("global_defs {\n    vrrp_strict\n}\n"), 0644)
		}, []string{"vrrp_strict"}},
		{"vrrp-strict-comment-ignored", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte("global_defs {\n    # vrrp_strict\n    router_id lab\n}\n"), 0644)
		}, nil},
		{"rpm-sample-config", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte(haRPMSample), 0644)
		}, []string{"vrrp_strict"}},
		{"dormant-sections", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte(strings.Replace(haRPMSample, "vrrp_strict", "", 1)), 0644)
		}, []string{"keepalived 当前未运行", "vrrp_instance、virtual_server"}},
		{"dormant-include", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte("include /etc/keepalived/conf.d/*.conf\n"), 0644)
		}, []string{"keepalived 当前未运行", "include"}},
		{"running-sections-ok", func(t *testing.T, f *haFixture, _ *OxiDNSHASpec) {
			f.running = true
			os.WriteFile(haMainConfigPath(), []byte(strings.Replace(haRPMSample, "vrrp_strict", "", 1)), 0644)
		}, nil},
		{"foreign-include", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haConfigPath(), []byte("vrrp_instance x {}\n"), 0644)
		}, []string{"不属于当前 DNS 高可用记录"}},
		{"orphan-block", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte(haBlock()), 0644)
		},
			[]string{"已有 ConfigFlow 标记块"}},
		{"malformed-block", func(t *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			os.WriteFile(haMainConfigPath(), []byte(haBlockBegin+"\ninclude x\n"), 0644)
		}, []string{"标记不完整"}},
		{"not-root", func(_ *testing.T, _ *haFixture, _ *OxiDNSHASpec) { haGeteuid = func() int { return 1000 } }, []string{"root 权限"}},
		{"container", func(_ *testing.T, f *haFixture, _ *OxiDNSHASpec) { f.cfg.ServiceManager = "s6" }, []string{"未显式启用 DNS 高可用"}},
		{"not-linux", func(_ *testing.T, _ *haFixture, _ *OxiDNSHASpec) { haOS = "darwin" }, []string{"仅支持 Linux"}},
		{"unsafe-script-path", func(_ *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			haExecutable = func() (string, error) { return "/opt/it's/agent", nil }
		}, []string{"keepalived 无法安全调用"}},
		{"insecure-script-path", func(_ *testing.T, _ *haFixture, _ *OxiDNSHASpec) {
			haScriptSecure = func(string) error { return fmt.Errorf("可被非 root 用户修改") }
		}, []string{"可被非 root 用户修改"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newHAFixture(t)
			spec := f.spec
			test.change(t, f, &spec)
			result := f.preview(t, spec)
			blockers, _ := json.Marshal(result["blockers"])
			if len(test.want) == 0 {
				if result["ok"] != true {
					t.Fatalf("unexpected blockers: %s", blockers)
				}
				return
			}
			if result["ok"] != false {
				t.Fatalf("preview passed: %#v", result)
			}
			for _, want := range test.want {
				if !strings.Contains(string(blockers), want) {
					t.Errorf("blockers %s do not mention %q", blockers, want)
				}
			}
			if len(f.commands) != 0 {
				t.Fatalf("preview changed the host: %v", f.commands)
			}
		})
	}
}

func TestOxiDNSHAMarkerBlock(t *testing.T) {
	haKeepalivedDir = "/etc/keepalived"
	original := "global_defs {\n    router_id main\n}\n"
	added, err := haUpsertSection(original, true)
	if err != nil || added != original+haBlock() {
		t.Fatalf("append: %q %v", added, err)
	}
	if again, err := haUpsertSection(added, true); err != nil || again != added {
		t.Fatalf("upsert is not idempotent: %q %v", again, err)
	}
	if clean, found, err := haStripSection(added); err != nil || !found || clean != original {
		t.Fatalf("strip: %q %v %v", clean, found, err)
	}
	noNewline, _ := haUpsertSection("router_id x", true)
	if noNewline != "router_id x\n"+haBlock() {
		t.Fatalf("missing separator: %q", noNewline)
	}
	created, _ := haUpsertSection("", false)
	if created != haBlock() || !haHasBlock(created) || !haHasBlock(added) || haHasBlock(original) {
		t.Fatal("block detection is wrong")
	}
	edited := strings.Replace(added, "include ", "include  ", 1)
	if haHasBlock(edited) {
		t.Fatal("edited block treated as intact")
	}
	if repaired, _ := haUpsertSection(edited, true); repaired != added {
		t.Fatalf("upsert did not restore block in place: %q", repaired)
	}
	for _, malformed := range []string{
		original + haBlockBegin + "\ninclude x\n",
		original + haBlockEnd + "\n",
		original + haBlock() + haBlock(),
	} {
		if clean, _, err := haStripSection(malformed); err == nil || clean != malformed {
			t.Fatalf("malformed marker changed content: %q", clean)
		}
		if _, err := haUpsertSection(malformed, true); err == nil {
			t.Fatal("upsert accepted malformed marker")
		}
	}
}

func TestOxiDNSHARenderedConfig(t *testing.T) {
	haKeepalivedDir = "/etc/keepalived"
	state := &haState{Token: strings.Repeat("a", 32), Spec: OxiDNSHASpec{SafetyVersion: 1, VIP: "192.168.0.2", VRID: 53, Priority: 150, SelfIP: "192.168.0.20", PeerIP: "192.168.0.21"}, Interface: "eth0", CIDR: "192.168.0.0/24"}
	text := renderHAConfig(state, "/opt/configflow-agent/configflow-agent -config /opt/configflow-agent/config-oxidns.json -ha-check")
	for _, want := range []string{
		"# configflow-oxidns-ha:" + strings.Repeat("a", 32) + "\n",
		"script \"/opt/configflow-agent/configflow-agent -config /opt/configflow-agent/config-oxidns.json -ha-check\"",
		"weight -60", "state BACKUP", "interface eth0", "virtual_router_id 53", "priority 150",
		"unicast_src_ip 192.168.0.20", "        192.168.0.21\n", "192.168.0.2/24 dev eth0", "cf_oxidns_check\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered config lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "# configflow-oxidns-ha:") || strings.Contains(text, "nopreempt") || strings.Contains(text, "preempt_delay") {
		t.Fatalf("unexpected config:\n%s", text)
	}
	if quoted, err := haScriptArgument("/opt/config flow/agent"); err != nil || quoted != "'/opt/config flow/agent'" {
		t.Fatalf("path with space: %q %v", quoted, err)
	}
	for _, bad := range []string{"relative/agent", "/opt/\"agent", "/opt/a\\b", "/opt/a#b", "/opt/a\nb"} {
		if _, err := haScriptArgument(bad); err == nil {
			t.Errorf("unsafe path accepted: %q", bad)
		}
	}
}

func TestOxiDNSHAApplyIsIdempotentAndRemoveRestores(t *testing.T) {
	f := newHAFixture(t)
	original := "global_defs {\n    router_id lab\n}\n"
	if err := os.WriteFile(haMainConfigPath(), []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if want := []string{"systemctl enable keepalived", "systemctl start keepalived"}; strings.Join(f.commands, ",") != strings.Join(want, ",") {
		t.Fatalf("service commands: %v", f.commands)
	}
	state, err := loadHAState(f.cfg)
	if err != nil || state == nil || !state.Active || state.Pending || state.Interface != "eth0" || state.CIDR != "192.168.0.0/24" {
		t.Fatalf("journal: %#v %v", state, err)
	}
	if state.Service.WasEnabled || state.Service.WasRunning || state.Files[1].Created {
		t.Fatalf("service/file ownership recorded wrongly: %#v", state)
	}
	include := readText(t, haConfigPath())
	if !haHeaderOwned(include, state.Token) || !strings.Contains(include, "-config "+f.cfg.AgentConfigFile+" -ha-check") {
		t.Fatalf("include file:\n%s", include)
	}
	if main := readText(t, haMainConfigPath()); main != original+haBlock() {
		t.Fatalf("main config: %q", main)
	}
	if info, _ := os.Stat(haMainConfigPath()); info.Mode().Perm() != 0640 {
		t.Fatalf("main config mode changed: %v", info.Mode())
	}
	if haFilesDrift(f.cfg, state) != nil {
		t.Fatal("fresh apply reports drift")
	}

	f.commands = nil
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil || len(f.commands) != 0 {
		t.Fatalf("repeated apply restarted keepalived: %v %v", f.commands, err)
	}
	// A priority change rewrites only our file and restarts keepalived once.
	changed := f.spec
	changed.Priority = 100
	f.commands = nil
	f.addresses = `[{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24},{"local":"192.168.0.2","prefixlen":24}]}]`
	if err := applyOxiDNSHA(f.cfg, changed); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.commands, ",") != "systemctl restart keepalived" {
		t.Fatalf("reapply commands: %v", f.commands)
	}
	if updated, _ := loadHAState(f.cfg); updated.Token != state.Token || updated.Service != state.Service || !strings.Contains(readText(t, haConfigPath()), "priority 100") {
		t.Fatal("reapply lost ownership or original service state")
	}

	// Removal verifies that the virtual IP left the host; stopping keepalived
	// releases it.
	f.commands = nil
	f.addresses = `[{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24}]}]`
	removed, err := removeOxiDNSHA(f.cfg)
	if err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if main := readText(t, haMainConfigPath()); main != original {
		t.Fatalf("main config not restored: %q", main)
	}
	if _, err := os.Stat(haConfigPath()); !os.IsNotExist(err) {
		t.Fatal("include file kept")
	}
	if f.running || f.enabled {
		t.Fatalf("service not restored: running=%v enabled=%v (%v)", f.running, f.enabled, f.commands)
	}
	if state, err := loadHAState(f.cfg); err != nil || state != nil {
		t.Fatal("journal kept after removal")
	}
	if removed, err := removeOxiDNSHA(f.cfg); err != nil || removed {
		t.Fatalf("second remove: %v %v", removed, err)
	}
}

func TestOxiDNSHARemoveDeletesCreatedMainConfigAndKeepsRunningService(t *testing.T) {
	f := newHAFixture(t)
	f.running, f.enabled = true, true
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.commands, ",") != "systemctl restart keepalived" {
		t.Fatalf("apply commands: %v", f.commands)
	}
	if main := readText(t, haMainConfigPath()); main != haBlock() {
		t.Fatalf("created main config: %q", main)
	}
	f.commands = nil
	if removed, err := removeOxiDNSHA(f.cfg); err != nil || !removed {
		t.Fatal(err)
	}
	if _, err := os.Stat(haMainConfigPath()); !os.IsNotExist(err) {
		t.Fatal("created keepalived.conf kept")
	}
	if strings.Join(f.commands, ",") != "systemctl restart keepalived" || !f.running || !f.enabled {
		t.Fatalf("originally running keepalived not restored: %v", f.commands)
	}
}

func TestOxiDNSHARemoveRefusesUnprovenOwnership(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	modified := readText(t, haConfigPath()) + "# local edit\n"
	if err := os.WriteFile(haConfigPath(), []byte(modified), 0644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	f.cfg.Token = "secret"
	RegisterRoutes(mux, f.cfg)
	request := httptest.NewRequest(http.MethodDelete, "/api/oxidns-ha", nil)
	request.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	f.commands = nil
	mux.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict || !strings.Contains(recorder.Body.String(), "已被手动修改") {
		t.Fatalf("modified file removal: %d %s", recorder.Code, recorder.Body.String())
	}
	if readText(t, haConfigPath()) != modified || !strings.Contains(readText(t, haMainConfigPath()), haBlockBegin) || len(f.commands) != 0 {
		t.Fatal("refused removal changed files or service")
	}
	if state, _ := loadHAState(f.cfg); state == nil || state.Pending {
		t.Fatal("refused removal changed the journal")
	}
	// A foreign header is never deleted either.
	if err := os.WriteFile(haConfigPath(), []byte("# configflow-oxidns-ha:"+strings.Repeat("b", 32)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := removeOxiDNSHA(f.cfg); err == nil {
		t.Fatal("foreign include removed")
	}
	// A corrupt journal keeps everything as it is.
	path, _ := haJournalPath(f.cfg)
	os.WriteFile(path, []byte("{broken"), 0600)
	if _, err := removeOxiDNSHA(f.cfg); err == nil {
		t.Fatal("corrupt journal accepted")
	}
}

func TestOxiDNSHAApplyRollsBackFailedServiceStart(t *testing.T) {
	f := newHAFixture(t)
	original := "global_defs {}\n"
	os.WriteFile(haMainConfigPath(), []byte(original), 0644)
	f.fail["systemctl start keepalived"] = fmt.Errorf("unit failed")
	err := applyOxiDNSHA(f.cfg, f.spec)
	if err == nil || !strings.Contains(err.Error(), "已恢复 keepalived 原配置") {
		t.Fatalf("failed apply: %v", err)
	}
	if readText(t, haMainConfigPath()) != original {
		t.Fatal("main config not restored")
	}
	if _, err := os.Stat(haConfigPath()); !os.IsNotExist(err) {
		t.Fatal("include file kept after rollback")
	}
	if f.enabled || f.running {
		t.Fatalf("service state not restored: %v", f.commands)
	}
	if state, err := loadHAState(f.cfg); err != nil || state != nil {
		t.Fatalf("journal kept after rollback: %#v %v", state, err)
	}
}

func TestOxiDNSHARollbackKeepsMainConfigMode(t *testing.T) {
	f := newHAFixture(t)
	original := "global_defs {\n    router_id lab\n}\n"
	if err := os.WriteFile(haMainConfigPath(), []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	f.fail["systemctl start keepalived"] = fmt.Errorf("unit failed")
	if err := applyOxiDNSHA(f.cfg, f.spec); err == nil {
		t.Fatal("failed start accepted")
	}
	if readText(t, haMainConfigPath()) != original {
		t.Fatal("main config not restored")
	}
	if info, _ := os.Stat(haMainConfigPath()); info.Mode().Perm() != 0600 {
		t.Fatalf("rollback widened keepalived.conf to %v", info.Mode().Perm())
	}
}

func TestOxiDNSHAApplyRefusesDormantSampleConfig(t *testing.T) {
	f := newHAFixture(t)
	sample := strings.Replace(haRPMSample, "vrrp_strict", "", 1)
	os.WriteFile(haMainConfigPath(), []byte(sample), 0644)
	err := applyOxiDNSHA(f.cfg, f.spec)
	if err == nil || !strings.Contains(err.Error(), "keepalived 当前未运行") {
		t.Fatalf("dormant sample accepted: %v", err)
	}
	if len(f.commands) != 0 || readText(t, haMainConfigPath()) != sample {
		t.Fatalf("refused apply changed the host: %v", f.commands)
	}
	if _, err := os.Stat(haConfigPath()); !os.IsNotExist(err) {
		t.Fatal("refused apply wrote the include file")
	}
	// keepalived already running these sections: ours joins them, and a
	// re-apply under our own journal is not refused for them either.
	f.running, f.enabled = true, true
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	f.running = false
	changed := f.spec
	changed.Priority = 100
	if err := applyOxiDNSHA(f.cfg, changed); err != nil {
		t.Fatalf("re-apply refused: %v", err)
	}
}

func TestOxiDNSHAApplyRefusesBlockers(t *testing.T) {
	f := newHAFixture(t)
	f.writeOxiDNS(t, "192.168.0.20:53")
	body, _ := json.Marshal(f.spec)
	recorder := httptest.NewRecorder()
	OxiDNSHAHandler(f.cfg)(recorder, httptest.NewRequest(http.MethodPut, "/api/oxidns-ha", strings.NewReader(string(body))))
	if recorder.Code != http.StatusUnprocessableEntity || !strings.Contains(recorder.Body.String(), "具体地址") {
		t.Fatalf("blocked apply: %d %s", recorder.Code, recorder.Body.String())
	}
	if len(f.commands) != 0 {
		t.Fatalf("blocked apply changed the host: %v", f.commands)
	}
	for _, body := range []string{`{"safety_version":1,"extra":1}`, `{"safety_version":1}{}`, `not json`} {
		recorder := httptest.NewRecorder()
		OxiDNSHAHandler(f.cfg)(recorder, httptest.NewRequest(http.MethodPut, "/api/oxidns-ha", strings.NewReader(body)))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: %d", body, recorder.Code)
		}
	}
}

func TestOxiDNSHAStatus(t *testing.T) {
	f := newHAFixture(t)
	f.writeOxiDNS(t, "127.0.0.1:1") // the health probe gets a fast refusal
	status := haStatus(f.cfg)
	runtime := status.Runtime
	if status.Managed != nil || status.Protocol != 1 || runtime.Role != "unknown" || runtime.KeepalivedRunning || runtime.Healthy {
		t.Fatalf("unmanaged status: %#v", status)
	}
	f.writeOxiDNS(t, "0.0.0.0:53")
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	f.writeOxiDNS(t, "0.0.0.0:1")
	status = haStatus(f.cfg)
	runtime = status.Runtime
	managed := status.Managed
	if managed == nil || !managed.Active || runtime.Role != "backup" || runtime.VIPPresent || !runtime.KeepalivedRunning || status.Error != "" {
		t.Fatalf("backup status: %#v", status)
	}
	f.addresses = `[{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24},{"local":"192.168.0.2","prefixlen":24}]}]`
	if runtime := haStatus(f.cfg).Runtime; runtime.Role != "master" || !runtime.VIPPresent {
		t.Fatalf("master status: %#v", runtime)
	}
	os.Remove(haConfigPath())
	if status := haStatus(f.cfg); !strings.Contains(status.Error, "缺失或与记录不一致") {
		t.Fatalf("drift not reported: %#v", status)
	}
}

func TestReapplyOxiDNSHA(t *testing.T) {
	f := newHAFixture(t)
	if err := reapplyOxiDNSHA(f.cfg, 3, func() {}); err != nil || len(f.commands) != 0 {
		t.Fatalf("no journal touched keepalived: %v %v", f.commands, err)
	}
	if _, err := os.Stat(haMainConfigPath()); !os.IsNotExist(err) {
		t.Fatal("no journal wrote keepalived files")
	}
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	f.commands = nil
	if err := reapplyOxiDNSHA(f.cfg, 3, func() {}); err != nil || len(f.commands) != 0 {
		t.Fatalf("consistent host restarted keepalived: %v %v", f.commands, err)
	}
	// Missing include and stopped service are repaired once.
	os.Remove(haConfigPath())
	f.running = false
	if err := reapplyOxiDNSHA(f.cfg, 3, func() {}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.commands, ",") != "systemctl start keepalived" || haFilesDrift(f.cfg, mustHAState(t, f.cfg)) != nil {
		t.Fatalf("repair: %v", f.commands)
	}
	// A manual edit is never overwritten.
	edited := readText(t, haConfigPath()) + "# edit\n"
	os.WriteFile(haConfigPath(), []byte(edited), 0644)
	f.commands = nil
	if err := reapplyOxiDNSHA(f.cfg, 3, func() {}); err == nil || readText(t, haConfigPath()) != edited || len(f.commands) != 0 {
		t.Fatalf("manual edit overwritten: %v %v", f.commands, err)
	}
	// Transient service failures are retried a bounded number of times.
	os.WriteFile(haConfigPath(), []byte(renderHAConfig(mustHAState(t, f.cfg), mustScript(t, f.cfg))), 0644)
	f.running = false
	f.fail["systemctl start keepalived"] = fmt.Errorf("busy")
	pauses := 0
	if err := reapplyOxiDNSHA(f.cfg, 3, func() { pauses++ }); err == nil || pauses != 2 {
		t.Fatalf("retry: %v pauses=%d", err, pauses)
	}
}

func mustHAState(t *testing.T, cfg *Config) *haState {
	t.Helper()
	state, err := loadHAState(cfg)
	if err != nil || state == nil {
		t.Fatalf("journal: %v", err)
	}
	return state
}

func mustScript(t *testing.T, cfg *Config) string {
	t.Helper()
	script, err := haScriptCommand(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestOxiDNSHAUninstallGuard(t *testing.T) {
	f := newHAFixture(t)
	if err := checkOxiDNSHAUninstall(f.cfg); err != nil {
		t.Fatalf("ordinary uninstall blocked: %v", err)
	}
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	if err := checkOxiDNSHAUninstall(f.cfg); err == nil || !strings.Contains(err.Error(), "停用 DNS 高可用") {
		t.Fatalf("active HA did not block uninstall: %v", err)
	}
	mihomo := *f.cfg
	mihomo.ServiceType = "mihomo"
	if err := checkOxiDNSHAUninstall(&mihomo); err != nil {
		t.Fatalf("guard affected another service: %v", err)
	}
	path, _ := haJournalPath(f.cfg)
	os.WriteFile(path, []byte("broken"), 0600)
	if err := checkOxiDNSHAUninstall(f.cfg); err == nil {
		t.Fatal("corrupt journal allowed uninstall")
	}
}

func TestOxiDNSHARoutesAndCapabilities(t *testing.T) {
	newHAFixture(t) // never inspect the test host's keepalived
	for _, serviceType := range []string{"oxidns", "mihomo", "mosdns"} {
		root := t.TempDir()
		cfg := &Config{ServiceType: serviceType, Token: "secret", AgentConfigFile: filepath.Join(root, "agent.json"), ConfigPath: filepath.Join(root, "config.yaml")}
		mux := http.NewServeMux()
		RegisterRoutes(mux, cfg)
		get := func(path string) *httptest.ResponseRecorder {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Authorization", "Bearer secret")
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			return recorder
		}
		var capabilities map[string]any
		json.Unmarshal(get("/api/capabilities").Body.Bytes(), &capabilities)
		_, advertised := capabilities["oxidns_ha_protocols"]
		status := get("/api/oxidns-ha").Code
		if serviceType == "oxidns" {
			if fmt.Sprint(capabilities["oxidns_ha_protocols"]) != "[1]" || status != http.StatusOK {
				t.Fatalf("oxidns: %v %d", capabilities, status)
			}
			request := httptest.NewRequest(http.MethodGet, "/api/oxidns-ha", nil)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated status: %d", recorder.Code)
			}
		} else if advertised || status != http.StatusNotFound {
			t.Fatalf("%s advertised HA: %v %d", serviceType, capabilities, status)
		}
	}
}

func TestOxiDNSHAVirtualIPsExcludedFromAgentAddresses(t *testing.T) {
	root := t.TempDir()
	cfg := &Config{AgentConfigFile: filepath.Join(root, "config.json"), ServiceType: "oxidns"}
	if OxiDNSHAVirtualIPs(cfg) != nil {
		t.Fatal("no journal reported a virtual IP")
	}
	original := detectAgentIPv4s
	t.Cleanup(func() { detectAgentIPv4s = original })
	detectAgentIPv4s = func() ([]string, error) { return []string{"192.168.0.20", "192.168.0.2"}, nil }
	addresses, err := agentLANIPv4s(cfg)
	if err != nil || len(addresses) != 2 {
		t.Fatalf("without journal: %v %v", addresses, err)
	}
	os.WriteFile(filepath.Join(root, "oxidns-ha.json"), []byte(`{"spec":{"vip":"192.168.0.2"}}`), 0600)
	if got := OxiDNSHAVirtualIPs(cfg); len(got) != 1 || got[0] != "192.168.0.2" {
		t.Fatalf("virtual IPs: %v", got)
	}
	addresses, err = agentLANIPv4s(cfg)
	if err != nil || len(addresses) != 1 || addresses[0].String() != "192.168.0.20" {
		t.Fatalf("with journal: %v %v", addresses, err)
	}
	// The native AIO Mihomo Agent sees the sibling OxiDNS journal.
	oldRoot := aioAgentRoot
	t.Cleanup(func() { aioAgentRoot = oldRoot })
	aioAgentRoot = filepath.Join(root, "aio")
	os.MkdirAll(filepath.Join(aioAgentRoot, "oxidns"), 0700)
	os.WriteFile(filepath.Join(aioAgentRoot, "oxidns", "oxidns-ha.json"), []byte(`{"spec":{"vip":"192.168.0.3"}}`), 0600)
	mihomo := &Config{AgentConfigFile: filepath.Join(aioAgentRoot, "mihomo", "config.json"), ServiceType: "mihomo", NativeAIO: true}
	if got := OxiDNSHAVirtualIPs(mihomo); len(got) != 1 || got[0] != "192.168.0.3" {
		t.Fatalf("native AIO virtual IPs: %v", got)
	}
}

// fakeDNSServer answers every query with the given header transformation.
func fakeDNSServer(t *testing.T, answer func(query []byte) []byte) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			if reply := answer(append([]byte(nil), buffer[:n]...)); reply != nil {
				conn.WriteTo(reply, addr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestRunOxiDNSHACheck(t *testing.T) {
	root := t.TempDir()
	names := make(chan string, 4)
	address := fakeDNSServer(t, func(query []byte) []byte {
		name, offset := []string{}, 12
		for offset < len(query) && query[offset] != 0 {
			length := int(query[offset])
			name = append(name, string(query[offset+1:offset+1+length]))
			offset += 1 + length
		}
		names <- strings.Join(name, ".")
		reply := append([]byte(nil), query...)
		reply[2] |= 0x80
		reply[3] = (reply[3] &^ 0x0f) | 3 // NXDOMAIN still proves the process answers
		return reply
	})
	_, port, _ := net.SplitHostPort(address)
	write := func(listen string, watchdog bool) string {
		first := "      - exec: accept\n"
		if watchdog {
			first = "      - matches:\n          - qname full:configflow-watchdog.invalid\n        exec: reject 3\n" + first
		}
		yaml := "plugins:\n  - tag: main\n    type: sequence\n    args:\n" + first + "  - tag: udp\n    type: udp_server\n    args:\n      listen: " + listen + "\n      entry: main\n"
		os.WriteFile(filepath.Join(root, "oxidns.yaml"), []byte(yaml), 0600)
		agent := filepath.Join(root, "agent.json")
		data, _ := json.Marshal(map[string]string{"config_path": filepath.Join(root, "oxidns.yaml")})
		os.WriteFile(agent, data, 0600)
		return agent
	}
	if code := RunOxiDNSHACheck(write("0.0.0.0:"+port, false)); code != 0 {
		t.Fatalf("answering OxiDNS: exit %d", code)
	}
	if name := <-names; name != "configflow-ha-check.invalid" {
		t.Fatalf("probe name: %s", name)
	}
	if code := RunOxiDNSHACheck(write(":"+port, true)); code != 0 {
		t.Fatalf("answering OxiDNS with watchdog rule: exit %d", code)
	}
	if name := <-names; name != "configflow-watchdog.invalid" {
		t.Fatalf("locally answered name not used: %s", name)
	}
	silent := fakeDNSServer(t, func([]byte) []byte { return nil })
	_, silentPort, _ := net.SplitHostPort(silent)
	start := time.Now()
	if code := RunOxiDNSHACheck(write("0.0.0.0:"+silentPort, false)); code != 1 || time.Since(start) > 3*time.Second {
		t.Fatalf("silent OxiDNS: exit %d after %v", code, time.Since(start))
	}
	wrongID := fakeDNSServer(t, func(query []byte) []byte {
		reply := append([]byte(nil), query...)
		binary.BigEndian.PutUint16(reply, binary.BigEndian.Uint16(query)+1)
		reply[2] |= 0x80
		return reply
	})
	_, wrongPort, _ := net.SplitHostPort(wrongID)
	if code := RunOxiDNSHACheck(write("0.0.0.0:"+wrongPort, false)); code != 1 {
		t.Fatal("reply with another ID accepted")
	}
	notReply := fakeDNSServer(t, func(query []byte) []byte { return query })
	_, notReplyPort, _ := net.SplitHostPort(notReply)
	if code := RunOxiDNSHACheck(write("0.0.0.0:"+notReplyPort, false)); code != 1 {
		t.Fatal("echoed query without QR accepted")
	}
	if address, name := haCheckTarget(""); address != "127.0.0.1:53" || name != haCheckName {
		t.Fatalf("default target: %s %s", address, name)
	}
	if address, _ := haCheckTarget(filepath.Join(root, "missing.yaml")); address != "127.0.0.1:53" {
		t.Fatalf("unreadable config target: %s", address)
	}
}
