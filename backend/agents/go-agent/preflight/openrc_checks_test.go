package preflight

import (
	"strings"
	"testing"
)

func groupCommands(check Check) string {
	var commands []string
	for _, group := range check.Groups {
		commands = append(commands, group.Commands...)
	}
	return strings.Join(commands, "\n")
}

// PVE's Alpine LXC template has the OpenRC sysctl service in no runlevel: a
// sysctl.d file then never applies at boot and must not count as persisted.
func TestSystemForwardingPersistenceRequiresTheOpenRCSysctlService(t *testing.T) {
	for _, tc := range []struct {
		name, runlevel, alpine string
		value                  string
		level                  string
		message, commands      []string
	}{
		{"alpine-boot", "boot", "yes", "1", "pass", []string{"Alpine sysctl 服务"}, nil},
		{"alpine-default", "default", "yes", "1", "pass", []string{"Alpine sysctl 服务"}, nil},
		{"alpine-sysinit", "sysinit", "yes", "1", "pass", []string{"Alpine sysctl 服务"}, nil},
		{"alpine-no-runlevel", "", "yes", "1", "warn",
			[]string{"ip_forward=1（/etc/sysctl.d/99-configflow-aio-forward.conf:2）", "没有加入任何运行级别", "Mihomo Agent"},
			[]string{"rc-update add sysctl boot"}},
		{"alpine-no-runlevel-off", "", "yes", "0", "warn",
			[]string{"ip_forward=0", "另外，OpenRC 的 sysctl 服务没有加入任何运行级别"},
			[]string{"rc-update add sysctl boot"}},
		{"alpine-no-service", "missing", "yes", "1", "warn",
			[]string{"没有 /etc/init.d/sysctl 服务"},
			[]string{"/etc/local.d/configflow-ip-forward.start", "chmod 755 /etc/local.d/configflow-ip-forward.start", "rc-update add local default"}},
		// OpenRC outside Alpine (procps sysctl --system) uses the same service.
		{"openrc-procps-no-runlevel", "", "", "1", "warn", []string{"（sysctl --system）", "没有加入任何运行级别"}, []string{"rc-update add sysctl boot"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := healthySystemFixture()
			fixture.files = map[string][]byte{"/etc/sysctl.d/99-configflow-aio-forward.conf": []byte("# Managed by ConfigFlow Shell AIO IPv4 forwarding v1\nnet.ipv4.ip_forward=" + tc.value + "\n")}
			if tc.alpine != "" {
				fixture.useLoader("alpine")
			}
			fixture.useOpenRC(tc.runlevel)
			check := inspectForwardingPersistence(fixture.probe())
			if check.Level != tc.level {
				t.Fatalf("level: %+v", check)
			}
			for _, want := range tc.message {
				if !strings.Contains(check.Message, want) {
					t.Fatalf("message lacks %q: %+v", want, check)
				}
			}
			commands := groupCommands(check)
			for _, want := range tc.commands {
				if !strings.Contains(commands, want) {
					t.Fatalf("manual step %q missing: %+v", want, check)
				}
			}
			if tc.level == "warn" && (check.Fix == "" || len(check.Groups) == 0) {
				t.Fatalf("warning without copyable steps: %+v", check)
			}
			if source, ok := systemForwardingPersistence(fixture.probe()); ok != (tc.level == "pass") {
				t.Fatalf("status persistence disagrees with the check: %q %v", source, ok)
			}
		})
	}
}

func TestSystemdAndProcpsIgnoreOpenRCSysctlService(t *testing.T) {
	for _, loader := range []string{"systemd", ""} {
		fixture := healthySystemFixture()
		fixture.useLoader(loader)
		// No /run/openrc (procps) or systemd running: the OpenRC service is irrelevant.
		if check := inspectForwardingPersistence(fixture.probe()); check.Level != "pass" || len(check.Groups) != 0 {
			t.Fatalf("%q: %+v", loader, check)
		}
		if state := detectSysctlBootState(fixture.probe()); state != SysctlBootNotApplicable {
			t.Fatalf("%q: %q", loader, state)
		}
	}
	// systemd wins even if an OpenRC state directory is present.
	fixture := healthySystemFixture()
	fixture.useLoader("systemd")
	fixture.useOpenRC("")
	if state := detectSysctlBootState(fixture.probe()); state != SysctlBootNotApplicable {
		t.Fatalf("systemd host judged by OpenRC: %q", state)
	}
}

func TestInstallerForwardingSaysItWillEnableTheOpenRCSysctlService(t *testing.T) {
	fixture := healthySystemFixture()
	fixture.useLoader("alpine")
	fixture.useOpenRC("")
	checks := systemChecksWithProbe(nil, installerProbe(fixture, ""))
	check := namedSystemCheck(t, checks, installerSysctlBootName)
	if check.Level != "pass" || !strings.Contains(check.Message, "rc-update add sysctl boot") {
		t.Fatalf("installer boot step not described: %+v", check)
	}
	if compactPass(check) == "" {
		t.Fatal("compact summary hides the installer's boot step")
	}

	fixture.useOpenRC("missing")
	checks = systemChecksWithProbe(nil, installerProbe(fixture, ""))
	check = namedSystemCheck(t, checks, installerSysctlBootName)
	if check.Level != "warn" || !strings.Contains(groupCommands(check), "rc-update add local default") {
		t.Fatalf("missing sysctl service without manual steps: %+v", check)
	}

	fixture.useOpenRC("boot")
	for _, check := range systemChecksWithProbe(nil, installerProbe(fixture, "")) {
		if check.Name == installerSysctlBootName {
			t.Fatalf("enabled service still reported: %+v", check)
		}
	}
}

const openRCMihomoScript = `#!/sbin/openrc-run
name="mihomo"
command="/usr/local/bin/mihomo"
command_args="-d /etc/mihomo -f /etc/mihomo/config.yaml"
command_background=true
pidfile="/run/mihomo.pid"
depend() {
    need net
}
`

func openRCCoreFixture(t *testing.T) (*systemFixture, Config) {
	t.Helper()
	fixture := healthySystemFixture()
	fixture.files["/etc/init.d/mihomo"] = []byte(openRCMihomoScript)
	fixture.files["/run/mihomo.pid"] = []byte("42\n")
	fixture.links = map[string]string{"/proc/42/exe": "/usr/local/bin/mihomo"}
	cfg := fixtureMihomo(t, true)
	cfg.ServiceManager, cfg.ServiceBinary = "openrc", "mihomo"
	return fixture, cfg
}

// ConfigFlow's OpenRC scripts declare pidfile="/run/<service>.pid"; the core's
// real CapEff, device view and TUN fd are then checked like under systemd.
func TestSystemOpenRCCoreIsInspectedThroughItsPidfile(t *testing.T) {
	for _, mode := range []string{"pidfile", "rc-svcname", "conventional", "symlinked-binary", "replaced-binary"} {
		t.Run(mode, func(t *testing.T) {
			fixture, cfg := openRCCoreFixture(t)
			switch mode {
			case "rc-svcname":
				fixture.files["/etc/init.d/mihomo"] = []byte(strings.Replace(openRCMihomoScript, `pidfile="/run/mihomo.pid"`, `pidfile="/run/${RC_SVCNAME}.pid"`, 1))
			case "conventional":
				fixture.files["/etc/init.d/mihomo"] = []byte(strings.Replace(openRCMihomoScript, "pidfile=\"/run/mihomo.pid\"\n", "", 1))
			case "symlinked-binary":
				fixture.links["/proc/42/exe"] = "/opt/mihomo/mihomo-linux-amd64"
				fixture.links["/usr/local/bin/mihomo"] = "/opt/mihomo/mihomo-linux-amd64"
			case "replaced-binary":
				fixture.links["/proc/42/exe"] = "/usr/local/bin/mihomo (deleted)"
			}
			probe := fixture.probe()
			probe.showUnit = func(string) ([]byte, error) {
				t.Fatal("OpenRC diagnostic invoked systemctl")
				return nil, nil
			}
			checks, hasCapability := inspectCoreService(probe, cfg, tunEnabled)
			if !hasCapability {
				t.Fatalf("verified core capability not reported: %+v", checks)
			}
			if got := namedSystemCheck(t, checks, "Mihomo 服务权限"); got.Level != "pass" || !strings.Contains(got.Message, "PID 42") {
				t.Fatalf("pidfile source not reported: %+v", got)
			}
			if got := namedSystemCheck(t, checks, "Mihomo 运行权限"); got.Level != "pass" || !strings.Contains(got.Message, "CapEff=0000000000001000") {
				t.Fatalf("actual CapEff not shown: %+v", got)
			}
			if got := namedSystemCheck(t, checks, "TUN 实际启动"); got.Level != "pass" || !strings.Contains(got.Message, "Meta") {
				t.Fatalf("core TUN interface not inspected: %+v", got)
			}
			for _, check := range checks {
				if check.Level != "pass" || strings.Contains(check.Message+check.Fix, "systemd") {
					t.Fatalf("unexpected result: %+v", check)
				}
			}
		})
	}
}

func TestSystemOpenRCCoreReportsRealPermissionProblems(t *testing.T) {
	fixture, cfg := openRCCoreFixture(t)
	fixture.files["/proc/42/status"] = []byte("CapEff:\t0000000000000400\n")
	delete(fixture.devices, "/proc/42/root/dev/net/tun")
	checks, hasCapability := inspectCoreService(fixture.probe(), cfg, tunEnabled)
	if hasCapability {
		t.Fatal("dropped capability accepted")
	}
	if got := namedSystemCheck(t, checks, "Mihomo 运行权限"); got.Level != "warn" || !strings.Contains(got.Message, "CapEff=0000000000000400") {
		t.Fatalf("dropped capability: %+v", got)
	}
	if got := namedSystemCheck(t, checks, "Mihomo 设备视图"); got.Level != "error" || strings.Contains(got.Fix, "systemd") || strings.Contains(got.Fix, "DeviceAllow") {
		t.Fatalf("device view: %+v", got)
	}
}

// Without a verified PID (stopped core, stale or foreign pidfile) the original
// reminders remain; a reused PID is never read as the core.
func TestSystemOpenRCCoreWithoutVerifiedPIDKeepsReminders(t *testing.T) {
	for _, tc := range []struct{ mode, reason string }{
		{"stopped", "无法读取 /run/mihomo.pid"},
		{"stale", "已退出"},
		{"reused", "运行的是 /usr/bin/sleep"},
		{"garbage", "不是有效的 PID"},
		{"no-script", "无法读取 /etc/init.d/mihomo"},
		{"expanded", "不是可识别的绝对路径"},
		{"bad-name", "未指定有效的 OpenRC 服务名"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			fixture, cfg := openRCCoreFixture(t)
			switch tc.mode {
			case "stopped":
				delete(fixture.files, "/run/mihomo.pid")
			case "stale":
				fixture.links = nil
			case "reused":
				fixture.links["/proc/42/exe"] = "/usr/bin/sleep"
			case "garbage":
				fixture.files["/run/mihomo.pid"] = []byte("abc\n")
			case "no-script":
				delete(fixture.files, "/etc/init.d/mihomo")
			case "expanded":
				fixture.files["/etc/init.d/mihomo"] = []byte(strings.Replace(openRCMihomoScript, `"/run/mihomo.pid"`, `"$piddir/mihomo.pid"`, 1))
			case "bad-name":
				cfg.ServiceUnit = "../mihomo"
			}
			checks, hasCapability := inspectCoreService(fixture.probe(), cfg, tunEnabled)
			if hasCapability || len(checks) != 2 {
				t.Fatalf("unverified PID used: %+v", checks)
			}
			service := namedSystemCheck(t, checks, "Mihomo 服务权限")
			runtime := namedSystemCheck(t, checks, "TUN 实际启动")
			if service.Level != "warn" || runtime.Level != "warn" || !strings.Contains(service.Message, "尚未取得并验证") || !strings.Contains(service.Message, tc.reason) {
				t.Fatalf("reminder lost or reason missing: %+v", checks)
			}
		})
	}
}

// A DNS core never forwards traffic: an OxiDNS-only Agent is not asked to
// enable or persist IPv4 forwarding, while an all-in-one install still is.
func TestSystemDNSOnlyAgentSkipsForwardingChecks(t *testing.T) {
	fixture := healthySystemFixture()
	fixture.files = map[string][]byte{"/proc/sys/net/ipv4/ip_forward": []byte("0\n"), "/proc/self/status": fixture.files["/proc/self/status"]}
	forwardingNames := []string{"IPv4 转发", "IPv4 转发持久化", installerForwardingName, installerSysctlBootName, "IPv4 转发权限"}
	for _, owner := range []string{"", "persist"} {
		probe := fixture.probe()
		if owner != "" {
			probe = installerProbe(fixture, "")
		}
		for _, service := range []string{"oxidns", "mosdns"} {
			for _, check := range systemChecksWithProbe([]Config{{ServiceType: service}}, probe) {
				for _, name := range forwardingNames {
					if check.Name == name {
						t.Fatalf("%s-only Agent checked forwarding: %+v", service, check)
					}
				}
			}
		}
	}
	// All-in-one: Mihomo beside OxiDNS keeps the forwarding checks.
	checks := systemChecksWithProbe([]Config{fixtureMihomo(t, false), {ServiceType: "oxidns"}}, fixture.probe())
	namedSystemCheck(t, checks, "IPv4 转发")
	namedSystemCheck(t, checks, "IPv4 转发持久化")
	checks = systemChecksWithProbe([]Config{fixtureMihomo(t, false), {ServiceType: "oxidns"}}, installerProbe(fixture, ""))
	namedSystemCheck(t, checks, installerForwardingName)
}
