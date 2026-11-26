package routes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAIOForwardingStandaloneLayoutRecognition(t *testing.T) {
	for _, unit := range []string{"", "configflow-agent", "configflow-agent.service"} {
		f := newAIOLayoutFixture(t, "systemd", "standalone")
		f.cfg.AgentServiceUnit = unit
		target, err := aioForwardingTargetFor(f.cfg)
		if err != nil {
			t.Fatalf("unit %q refused: %v", unit, err)
		}
		want := aioForwardingTarget{
			Layout: "standalone", Directory: aioAgentRoot, Unit: "configflow-agent", Manager: "systemd",
			ExecStart: "ExecStart=/usr/local/bin/configflow-agent -config " + filepath.Join(aioAgentRoot, "config-mihomo.json"),
			OpenRCOwnership: []string{
				`command="/usr/local/bin/configflow-agent"`,
				`command_args="-config ` + filepath.Join(aioAgentRoot, "config-mihomo.json") + `"`,
			},
		}
		if fmt.Sprintf("%#v", target) != fmt.Sprintf("%#v", want) {
			t.Fatalf("wrong target:\n%#v\nwant\n%#v", target, want)
		}
		if target.Hook() != filepath.Join(aioAgentRoot, "enable-ipv4-forwarding.sh") || target.UnitPath() != filepath.Join(aioSystemdUnitDir, "configflow-agent.service") {
			t.Fatalf("wrong hook or unit path: %s %s", target.Hook(), target.UnitPath())
		}
	}
	for _, test := range []struct {
		name   string
		change func(*aioFixture)
	}{
		{"OxiDNS", func(f *aioFixture) {
			f.cfg.ServiceType = "oxidns"
			f.cfg.AgentConfigFile = filepath.Join(aioAgentRoot, "config-oxidns.json")
		}},
		{"OxiDNS with Mihomo path", func(f *aioFixture) { f.cfg.ServiceType = "oxidns" }},
		{"config.json", func(f *aioFixture) { f.cfg.AgentConfigFile = filepath.Join(aioAgentRoot, "config.json") }},
		{"config-oxidns.json", func(f *aioFixture) { f.cfg.AgentConfigFile = filepath.Join(aioAgentRoot, "config-oxidns.json") }},
		{"other directory", func(f *aioFixture) { f.cfg.AgentConfigFile = filepath.Join(f.root, "config-mihomo.json") }},
		{"other unit", func(f *aioFixture) { f.cfg.AgentServiceUnit = "other" }},
		{"AIO unit", func(f *aioFixture) { f.cfg.AgentServiceUnit = "configflow-agent-mihomo" }},
		{"native AIO flag", func(f *aioFixture) { f.cfg.NativeAIO = true }},
		{"command manager", func(f *aioFixture) { f.cfg.ServiceManager = "command" }},
		{"supervisor manager", func(f *aioFixture) { f.cfg.ServiceManager = "s6" }},
		{"mixed layout directory", func(f *aioFixture) { os.Mkdir(filepath.Join(aioAgentRoot, "mihomo"), 0755) }},
		{"mixed layout file", func(f *aioFixture) { os.WriteFile(filepath.Join(aioAgentRoot, "mihomo"), nil, 0644) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := newAIOLayoutFixture(t, "systemd", "standalone")
			test.change(f)
			if target, err := aioForwardingTargetFor(f.cfg); err == nil {
				t.Fatalf("accepted: %+v", target)
			}
			before := f.snapshot(t)
			if _, err := persistAIOForwarding(f.cfg, false); err == nil {
				t.Fatal("unsupported installation persisted forwarding")
			}
			if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) || len(f.writes) != 0 || f.hookRuns != 0 {
				t.Fatal("unsupported installation changed host")
			}
			if status := aioForwardingStatus(f.cfg); status["supported"] != false || status["layout"] != nil || status["startup_hook"] != "unknown" {
				t.Fatalf("wrong status: %+v", status)
			}
		})
	}
}

func TestAIOForwardingStandaloneOpenRCOwnership(t *testing.T) {
	config := "/opt/configflow-agent/config-mihomo.json"
	for _, test := range []struct{ name, old, new string }{
		{"command_args", `command_args="-config ` + config + `"`, `command_args="-config ` + config + ` -verbose"`},
		{"command", `command="/usr/local/bin/configflow-agent"`, `command="/usr/bin/other-agent"`},
		{"indented command", `command="/usr/local/bin/configflow-agent"`, `  command="/usr/local/bin/configflow-agent"`},
	} {
		for _, variant := range []string{"plain", "hook"} {
			t.Run(test.name+"/"+variant, func(t *testing.T) {
				f := newAIOLayoutFixture(t, "openrc", "standalone")
				old := strings.ReplaceAll(test.old, "/opt/configflow-agent", aioAgentRoot)
				changed := strings.Replace(f.fixtureUnit(t, variant), old, strings.ReplaceAll(test.new, "/opt/configflow-agent", aioAgentRoot), 1)
				if changed == f.fixtureUnit(t, variant) {
					t.Fatal("fixture does not contain the ownership line")
				}
				f.writeUnit(t, changed)
				if status := aioForwardingStatus(f.cfg); status["startup_hook"] != "unknown" || status["persisted"] != false {
					t.Fatalf("foreign init script reported as ours: %+v", status)
				}
				before := f.snapshot(t)
				_, err := persistAIOForwarding(f.cfg, false)
				if err == nil || !strings.Contains(err.Error(), "不是 ConfigFlow 单独安装写入的格式") {
					t.Fatalf("foreign init script accepted: %v", err)
				}
				if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) || f.hookRuns != 0 || f.forwarding(t) != "0" {
					t.Fatal("refusal changed host")
				}
			})
		}
	}
}

// The fixtures are rendered from install-go.sh; patching the plain unit must
// produce exactly what the installer writes with forwarding enabled.
func TestAIOForwardingUnitPatchFixturesRoundTrip(t *testing.T) {
	for _, layout := range aioLayouts {
		for _, manager := range []string{"systemd", "openrc"} {
			t.Run(layout+"/"+manager, func(t *testing.T) {
				f := newAIOLayoutFixture(t, manager, layout)
				target, err := aioForwardingTargetFor(f.cfg)
				if err != nil {
					t.Fatal(err)
				}
				patch := func(data string) ([]byte, error) {
					if manager == "systemd" {
						return aioPatchSystemd([]byte(data), target)
					}
					return aioPatchOpenRC([]byte(data), target)
				}
				patched, err := patch(f.fixtureUnit(t, "plain"))
				if err != nil || string(patched) != f.fixtureUnit(t, "hook") {
					t.Fatalf("patched plain unit differs from installer output (%v):\n%s", err, patched)
				}
				if again, err := patch(f.fixtureUnit(t, "hook")); err != nil || again != nil {
					t.Fatalf("registered unit patched again: %v %q", err, again)
				}
			})
		}
	}
}

func TestAIOForwardingStandaloneHookLines(t *testing.T) {
	f := newAIOLayoutFixture(t, "systemd", "standalone")
	target, err := aioForwardingTargetFor(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	hook := filepath.Join(aioAgentRoot, "enable-ipv4-forwarding.sh")
	if target.SystemdHookLine() != "ExecStartPre=/bin/sh "+hook || target.OpenRCHookLine() != "    /bin/sh "+hook+" || return 1" {
		t.Fatalf("wrong hook lines: %q %q", target.SystemdHookLine(), target.OpenRCHookLine())
	}
	// Check the native AIO Mihomo Agent default too.
	if aioDefaultTarget().Hook() != filepath.Join(aioAgentRoot, "mihomo", "enable-ipv4-forwarding.sh") {
		t.Fatalf("default hook path changed: %s", aioDefaultTarget().Hook())
	}
}

// The single-service hook differs from the AIO hook only in the comment naming
// its one Agent, and each layout recognises only its own installer's hook.
func TestAIOForwardingStandaloneHookNamesOneAgent(t *testing.T) {
	if strings.Count(aioForwardingHook, aioHookComment) != 1 {
		t.Fatal("AIO hook comment not found")
	}
	if aioStandaloneForwardingHook == aioForwardingHook || strings.Contains(aioStandaloneForwardingHook, "both Agents") ||
		strings.Replace(aioStandaloneForwardingHook, aioStandaloneHookComment, aioHookComment, 1) != aioForwardingHook {
		t.Fatalf("standalone hook is not the AIO hook with its own comment:\n%s", aioStandaloneForwardingHook)
	}
	for _, layout := range aioLayouts {
		f := newAIOLayoutFixture(t, "systemd", layout)
		target, err := aioForwardingTargetFor(f.cfg)
		if err != nil {
			t.Fatal(err)
		}
		other := map[string]string{"aio": aioStandaloneForwardingHook, "standalone": aioForwardingHook}[layout]
		for content, want := range map[string]string{f.hookContent(): "managed", other: "modified"} {
			os.WriteFile(f.hookPath(), []byte(content), 0755)
			if state, err := aioHookScriptState(target); err != nil || state != want {
				t.Fatalf("%s: hook state %q %v, want %q", layout, state, err, want)
			}
		}
	}
}

// A single-service host that tried the Linux gateway (ip_forward originally 0)
// and then persisted forwarding keeps forwarding on when the gateway is removed.
func TestAIOForwardingStandalonePersistKeepsForwardingOnGatewayRemoval(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) {
			f := newAIOLayoutFixture(t, manager, "standalone")
			if err := applyLinuxGateway(f.cfg, f.spec, "test-profile"); err != nil {
				t.Fatal(err)
			}
			state, err := loadGatewayState(f.cfg)
			if err != nil || state == nil || state.Original["net/ipv4/ip_forward"] != "0" {
				t.Fatalf("fixture must start with forwarding off: %+v %v", state, err)
			}
			if _, err := os.Stat(filepath.Join(aioAgentRoot, "linux-gateway.json")); err != nil {
				t.Fatalf("gateway journal not in the single-service Agent directory: %v", err)
			}
			if gatewayManaged(f.cfg).PreservesForwarding {
				t.Fatal("forwarding preserved before it was persisted")
			}
			if _, err := persistAIOForwarding(f.cfg, false); err != nil {
				t.Fatal(err)
			}
			f.assertPersisted(t)
			if !gatewayManaged(f.cfg).PreservesForwarding {
				t.Fatal("persisted single-service forwarding not recognised by the gateway")
			}
			f.writeConfig(t, false)
			if err := removeLinuxGateway(f.cfg); err != nil {
				t.Fatal(err)
			}
			for key, original := range state.Original {
				want := original
				if key == "net/ipv4/ip_forward" || strings.HasSuffix(key, "/forwarding") {
					want = "1"
				}
				if value, err := gatewayReadSysctl(key); err != nil || value != want {
					t.Errorf("%s=%q want %q (%v)", key, value, want, err)
				}
			}
			if state.Original["net/ipv4/conf/all/send_redirects"] != "1" {
				t.Fatal("fixture must prove send_redirects is restored, not merely kept")
			}
			for _, write := range f.writes {
				if write == "net/ipv4/ip_forward=0" {
					t.Fatalf("forwarding written to 0: %v", f.writes)
				}
			}
			if state, err := loadGatewayState(f.cfg); err != nil || state != nil {
				t.Fatalf("journal retained: %+v %v", state, err)
			}
			f.assertPersisted(t)
		})
	}
}
