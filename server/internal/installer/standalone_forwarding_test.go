package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Port of backend/test_standalone_forwarding_install.py (generator and shell
// cases): single-service Mihomo installs enable and persist IPv4 forwarding
// like the Shell AIO installer, run against isolated files with mocked
// services, sysctl and ip_forward. The install-command / install-script API
// cases are in internal/api/agents/installer_forwarding_test.go.

const forwardContent = "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1\nnet.ipv4.ip_forward=1\n"

// aioHookScript is the AIO hook heredoc of aio-ipv4-forwarding.sh.
func aioHookScript() string {
	_, rest, _ := strings.Cut(installGoAIOForwarding, "<<'CONFIGFLOW_AIO_FORWARDING_EOF'\n")
	body, _, _ := strings.Cut(rest, "\nCONFIGFLOW_AIO_FORWARDING_EOF\n")
	return body + "\n"
}

// standaloneHookScript is HOOK_SCRIPT: the AIO hook naming its one Agent.
func standaloneHookScript() string {
	return strings.ReplaceAll(aioHookScript(), AIOForwardingHookComment, StandaloneForwardingHookComment)
}

func standaloneGenerated(service string, mutate func(*AgentInstallOptions)) (string, error) {
	o := AgentInstallOptions{AgentPort: 8080, ServerURL: "https://config.test", AgentName: "single", ServiceType: service, AgentIP: "192.0.2.20",
		BinaryDownloadURL: "https://config.test/api/agents/download"}
	if mutate != nil {
		mutate(&o)
	}
	return RenderAgentInstall(o)
}

func forwardingOption(f Forwarding) func(*AgentInstallOptions) {
	return func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = f }
}

func TestOptOutIsByteIdenticalToThePreviousRelease(t *testing.T) {
	expected := deltas(t).SingleServiceScriptSHA256
	for _, core := range []bool{false, true} {
		script := singleServiceScript(t, "mihomo", core, ForwardingOff)
		if got := sha256Hex(withoutReviewedInstallerDeltas(t, script, core)); got != expected[digestKey("mihomo", core)] {
			t.Errorf("core=%v: %s", core, got)
		}
	}
	got := singleServiceScriptDigests(t)
	for key, digest := range expected {
		if got[key] != digest {
			t.Errorf("%s differs", key)
		}
	}
}

func TestDNSServicesNeverTakeForwarding(t *testing.T) {
	expected := deltas(t).SingleServiceScriptSHA256
	for service, cores := range map[string][]bool{"oxidns": {false, true}, "mosdns": {false}} {
		for _, core := range cores {
			for _, value := range []Forwarding{ForwardingAuto, ForwardingOff} {
				script := singleServiceScript(t, service, core, value)
				if sha256Hex(withoutReviewedInstallerDeltas(t, script, core)) != expected[digestKey(service, core)] {
					t.Errorf("%s core=%v forwarding=%d differs", service, core, value)
				}
			}
		}
	}
}

func TestAutomaticChoiceEnablesForwardingForMihomo(t *testing.T) {
	expected := deltas(t).SingleServiceScriptSHA256
	for _, core := range []bool{false, true} {
		for _, value := range []Forwarding{ForwardingAuto, ForwardingOn} {
			script := ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) { o.InstallCore = core; o.ManageIPv4Forwarding = value }))
			contains(t, script, "persist_ipv4_forwarding\n")
			if sha256Hex(withoutReviewedInstallerDeltas(t, script, core)) == expected[digestKey("mihomo", core)] {
				t.Error("forwarding script equals the opt-out baseline")
			}
		}
	}
}

// test_forwarding_option_must_be_a_boolean (1, 0, "true", "false", "yes") is
// a Python type check that Go's Forwarding type rules out.

var customLifecycle = func(o *AgentInstallOptions) {
	o.RestartCommand, o.StopCommand, o.StartCommand, o.StatusCommand = "/custom/restart", "/custom/stop", "/custom/start", "/custom/status"
}

func TestForwardingNeedsMihomoUnderSystemdOrOpenRC(t *testing.T) {
	for _, c := range []struct {
		service string
		mutate  func(*AgentInstallOptions)
	}{
		{"oxidns", nil}, {"mosdns", nil}, {"mihomo", customLifecycle},
		{"mihomo", func(o *AgentInstallOptions) {
			o.ServiceManager, o.StopCommand, o.StartCommand, o.StatusCommand = "command", "/s", "/t", "/u"
		}},
	} {
		with := func(f Forwarding) func(*AgentInstallOptions) {
			return func(o *AgentInstallOptions) {
				if c.mutate != nil {
					c.mutate(o)
				}
				o.ManageIPv4Forwarding = f
			}
		}
		_, err := standaloneGenerated(c.service, with(ForwardingOn))
		if err == nil || !strings.Contains(err.Error(), "仅使用 systemd 或 OpenRC 的 Mihomo 单独安装") {
			t.Errorf("%s: %v", c.service, err)
		}
		notContains(t, ok(standaloneGenerated(c.service, with(ForwardingAuto))), "persist_ipv4_forwarding")
	}
}

func TestExplicitStandardManagersKeepForwarding(t *testing.T) {
	for _, mutate := range []func(*AgentInstallOptions){
		func(o *AgentInstallOptions) { o.ServiceManager = "systemd" }, func(o *AgentInstallOptions) { o.ServiceManager = "openrc" },
		func(o *AgentInstallOptions) { o.RestartCommand = "rc-service mihomo restart" },
	} {
		contains(t, ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) { mutate(o); o.ManageIPv4Forwarding = ForwardingAuto })), "persist_ipv4_forwarding\n")
	}
}

// serviceHeredoc is service_heredoc(script, manager).
func serviceHeredoc(t *testing.T, script, manager string) string {
	start := map[string]string{"systemd": "cat > /etc/systemd/system/configflow-agent.service <<'SERVICEEOF'\n",
		"openrc": "cat > /etc/init.d/configflow-agent <<'SERVICEEOF'\n"}[manager]
	require(t, strings.Count(script, start) == 1, "service heredoc %s", manager)
	_, rest, _ := strings.Cut(script, start)
	body, _, _ := strings.Cut(rest, "\nSERVICEEOF\n")
	return body + "\n"
}

func TestHookIsRegisteredWhereTheAgentLooksForIt(t *testing.T) {
	for _, core := range []bool{false, true} {
		script := ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) { o.InstallCore = core; o.ManageIPv4Forwarding = ForwardingAuto }))
		systemd := strings.Split(strings.TrimSuffix(serviceHeredoc(t, script, "systemd"), "\n"), "\n")
		var pre []int
		for i, line := range systemd {
			if strings.HasPrefix(line, "ExecStartPre=") {
				pre = append(pre, i)
			}
		}
		if len(pre) != 1 || systemd[pre[0]] != "ExecStartPre=/bin/sh "+StandaloneForwardingHook ||
			!strings.HasPrefix(systemd[pre[0]+1], "ExecStart=") || systemd[pre[0]-1] != "WorkingDirectory=/opt/configflow-agent" {
			t.Errorf("systemd unit: %v", systemd)
		}
		openrc := strings.Split(strings.TrimSuffix(serviceHeredoc(t, script, "openrc"), "\n"), "\n")
		first := indexOf(openrc, func(l string) bool { return l == "start_pre() {" }) + 1
		hooks := 0
		for _, line := range openrc {
			if strings.Contains(line, "enable-ipv4-forwarding.sh") {
				hooks++
			}
		}
		if openrc[first] != "    /bin/sh "+StandaloneForwardingHook+" || return 1" || hooks != 1 {
			t.Errorf("openrc unit: %v", openrc)
		}
		notContains(t, script, "$AGENT_ROOT")
		step := strings.Index(script, `cf_step "安装并启动 Agent 服务"`)
		if strings.Index(script, "prepare_ipv4_forwarding\n") > step+strings.Index(script[step:], "systemctl stop configflow-agent 2>/dev/null || true\n") ||
			strings.Index(script, "install_ipv4_forwarding_hook\n") > strings.Index(script, "cat > /etc/systemd/system/configflow-agent.service") ||
			strings.Index(script, "if ! rc-service configflow-agent status ||\n") > strings.Index(script, "persist_ipv4_forwarding\n") ||
			strings.Count(script, `cleanup_ipv4_forwarding "$agent_status"`) != 1 {
			t.Errorf("core=%v: order", core)
		}
		if r := runShell(t, "sh", "-n", runOptions{args: []string{writeTemp(t, "install.sh", script)}}); r.code != 0 {
			t.Fatal(r.stderr)
		}
	}
}

func TestGoAgentUnitFixturesMatchTheSingleServiceInstaller(t *testing.T) {
	for _, c := range [][2]string{{"systemd", "service"}, {"openrc", "initd"}} {
		for _, variant := range []string{"plain", "hook"} {
			f := ForwardingOff
			if variant == "hook" {
				f = ForwardingAuto
			}
			script := ok(standaloneGenerated("mihomo", forwardingOption(f)))
			fixture := readFile(t, filepath.Join("..", "..", "..", "backend", "agents", "go-agent", "routes", "testdata",
				"standalone-unit-"+c[0]+"-"+variant+"."+c[1]))
			if fixture != serviceHeredoc(t, script, c[0]) {
				t.Errorf("%s/%s fixture differs", c[0], variant)
			}
		}
	}
}

func TestUsesTheAIOHookWithASingleAgentComment(t *testing.T) {
	script := ok(standaloneGenerated("mihomo", forwardingOption(ForwardingAuto)))
	if standaloneHookScript() == aioHookScript() || strings.Count(aioHookScript(), AIOForwardingHookComment) != 1 {
		t.Fatal("hook comments")
	}
	if strings.Count(script, standaloneHookScript()) != 1 {
		t.Error("hook not embedded once")
	}
	notContains(t, script, "both Agents")
	notContains(t, script, AIOForwardingHookComment)
	contains(t, script, `cp "$STAGE_DIR/enable-ipv4-forwarding.sh" "$AGENT_DIR/enable-ipv4-forwarding.sh"`)
	contains(t, script, `/bin/sh "$AGENT_DIR/enable-ipv4-forwarding.sh" || ipv4_failure`)
}

func TestGoAgentWritesAndRecognisesTheSameHookComments(t *testing.T) {
	source := readFile(t, filepath.Join("..", "..", "..", "backend", "agents", "go-agent", "routes", "aio_forwarding.go"))
	for name, comment := range map[string]string{"aioHookComment": AIOForwardingHookComment, "aioStandaloneHookComment": StandaloneForwardingHookComment} {
		quoted, _ := json.Marshal(comment) // json.dumps(comment, ensure_ascii=False) for this ASCII text
		if !regexp.MustCompile(`(?m)^\s*` + name + `\s*= ` + regexp.QuoteMeta(string(quoted)) + `$`).MatchString(source) {
			t.Errorf("%s does not match the installer", name)
		}
	}
}

// -- Running the installer --------------------------------------------------

type standaloneHarness struct {
	tmp, script                                    string
	root, conf, hook, unit, agentDir, proc, events string
	env                                            env
}

type standaloneSetup struct {
	manager     string
	installCore bool
	forwarding  *Forwarding // nil = the harness default (None)
	mutate      func(*AgentInstallOptions)
}

// newStandaloneHarness is harness(tmp_path, monkeypatch, manager, install_core, **options).
func newStandaloneHarness(t *testing.T, s standaloneSetup) *standaloneHarness {
	if s.manager == "" {
		s.manager = "systemd"
	}
	tmp := t.TempDir()
	forwarding := ForwardingAuto
	if s.forwarding != nil {
		forwarding = *s.forwarding
	}
	source := ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) {
		o.InstallCore = s.installCore
		o.ManageIPv4Forwarding = forwarding
		if s.mutate != nil {
			s.mutate(o)
		}
	}))
	if s.installCore {
		source = withFakeCore(t, source, singleFakeCore)
	}
	root := filepath.Join(tmp, "root")
	for _, rel := range []string{"etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "run", "var/log"} {
		os.MkdirAll(filepath.Join(root, rel), 0o755)
	}
	writeFile(t, filepath.Join(tmp, "agent-process"), "PPid:\tmissing\n", 0o644)
	if s.manager == "systemd" {
		os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755)
	} else {
		writeFile(t, filepath.Join(root, "sbin/openrc-run"), "#!/bin/sh\n", 0o755)
	}
	source = replaceAll(source, [][2]string{
		{"/proc/sys/net/ipv4/ip_forward", filepath.Join(tmp, "ip_forward")},
		{"/proc/[0-9]*/status", filepath.Join(tmp, "agent-process")},
		{"/usr/local/", root + "/usr/local/"},
		{"/etc/", root + "/etc/"},
		{"/run/", root + "/run/"},
		{"/sbin/openrc-run", root + "/sbin/openrc-run"},
		{"/opt/configflow-agent", root + "/opt/configflow-agent"},
		{"/var/log", root + "/var/log"},
		{"/dev/tty", "/dev/stdin"},
	})
	source = strings.ReplaceAll(source, "in /run /etc /usr/local ", "in "+root+"/run "+root+"/etc "+root+"/usr/local ")
	source = darwinSed(source)
	script := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
	writeFile(t, filepath.Join(tmp, "ip_forward"), "0\n", 0o644)
	writeFile(t, filepath.Join(root, "etc/mihomo/config.yaml"), "# existing core config\n", 0o644)
	writeFile(t, filepath.Join(root, "usr/local/bin/mihomo"), "#!/bin/sh\nexit 0\n", 0o755)
	coreUnit, unit := filepath.Join(root, "etc/init.d/mihomo"), filepath.Join(root, "etc/init.d/configflow-agent")
	if s.manager == "systemd" {
		coreUnit, unit = filepath.Join(root, "etc/systemd/system/mihomo.service"), filepath.Join(root, "etc/systemd/system/configflow-agent.service")
	}
	writeFile(t, coreUnit, "# existing core service\n", 0o755)
	commands := filepath.Join(tmp, "commands")
	agent := writeFile(t, filepath.Join(tmp, "fake-agent"), "#!/bin/sh\nprintf 'agent %s\\n' \"$*\" >> \"$EVENTS\"\nexit 0\n", 0o755)
	managerMock := `printf '%s %s\n' "${0##*/}" "$*" >> "$EVENTS"
case "$*" in *--property=LoadState*) printf 'loaded\n' ;; esac
case "${0##*/}:$*" in
    "systemctl:start configflow-agent"|"rc-service:configflow-agent start")
        [ "${FAIL_AGENT_START:-}" != yes ] || exit 49
        hook=$(sed -n 's#^.*/bin/sh \([^ ]*enable-ipv4-forwarding\.sh\).*$#\1#p' "$AGENT_UNIT")
        if [ -n "$hook" ]; then /bin/sh "$hook"; fi
        ;;
esac
`
	sysctlMock := "#!" + pythonExe(t) + ` -S
import os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('sysctl ' + ' '.join(args) + '\n')
if args not in (['-q', '-w', 'net.ipv4.ip_forward=0'], ['-q', '-w', 'net.ipv4.ip_forward=1']):
    raise SystemExit('Unexpected sysctl command in isolated installer test: ' + repr(args))
if os.environ.get('FAIL_SYSCTL_WRITES') == 'yes':
    print('sysctl: permission denied on key net.ipv4.ip_forward', file=sys.stderr)
    raise SystemExit(50)
Path(os.environ['FORWARD_PROC']).write_text(args[-1].rsplit('=', 1)[1] + '\n')
`
	mocks := map[string]string{"id": "printf '0\\n'\n", "uname": "case \"${1:-}\" in -s) echo Linux ;; *) echo x86_64 ;; esac\n",
		"wget": "cp \"$FAKE_AGENT\" \"$3\"\n", "sysctl": sysctlMock, "sleep": "exit 0\n"}
	if s.manager == "systemd" {
		mocks["systemctl"] = managerMock
	} else {
		mocks["rc-service"] = managerMock
		mocks["rc-update"] = managerMock
	}
	writeCommands(t, commands, mocks, "#!/bin/sh\nset -eu\n")
	events := writeFile(t, filepath.Join(tmp, "events"), "", 0o644)
	e := baseEnv().with("PATH="+commands+":"+root+"/usr/local/bin:"+os.Getenv("PATH"), "EVENTS="+events,
		"FAKE_AGENT="+agent, "FORWARD_PROC="+filepath.Join(tmp, "ip_forward"), "AGENT_UNIT="+unit,
		"TEST_CORE=mihomo", "CORE_COMPLETED="+filepath.Join(tmp, "core-completed"), "CONFIGFLOW_INSTALL_LOG_DIR="+tmp)
	return &standaloneHarness{tmp: tmp, script: script, root: root, conf: filepath.Join(root, "etc/sysctl.d/99-configflow-aio-forward.conf"),
		hook: filepath.Join(root, "opt/configflow-agent/enable-ipv4-forwarding.sh"), unit: unit,
		agentDir: filepath.Join(root, "opt/configflow-agent"), proc: filepath.Join(tmp, "ip_forward"), events: events, env: e}
}

func (h *standaloneHarness) run(t *testing.T, choice string, extra ...string) runResult {
	return runInstaller(t, h.script, h.env.with(extra...), choice)
}

func (h *standaloneHarness) sysctlWrites(t *testing.T) []string {
	var out []string
	for _, line := range lines(readFile(t, h.events)) {
		if strings.HasPrefix(line, "sysctl ") {
			out = append(out, line)
		}
	}
	return out
}

func (h *standaloneHarness) expectedHook() string {
	return strings.ReplaceAll(strings.ReplaceAll(standaloneHookScript(), "/proc/sys/net/ipv4/ip_forward", h.proc), "/etc/", h.root+"/etc/")
}

func (h *standaloneHarness) leftovers() bool {
	return globAny(filepath.Join(h.agentDir, ".ipv4-forward.*")) || globAny(filepath.Join(h.root, "etc/sysctl.d/.configflow-aio-forward.*"))
}

func (h *standaloneHarness) stopped(t *testing.T) bool {
	for _, line := range lines(readFile(t, h.events)) {
		if strings.Contains(line, " stop") && strings.Contains(line, "configflow-agent") {
			return true
		}
	}
	return false
}

func (h *standaloneHarness) assertPersisted(t *testing.T, manager string) {
	t.Helper()
	info, err := os.Stat(h.hook)
	if readFile(t, h.conf) != forwardContent || err != nil || info.Mode().Perm() != 0o755 || readFile(t, h.hook) != h.expectedHook() {
		t.Error("forwarding files")
	}
	unitLines := strings.Split(readFile(t, h.unit), "\n")
	hookLine := "/bin/sh " + h.hook
	if manager == "systemd" {
		index := indexOf(unitLines, func(l string) bool { return l == "ExecStartPre="+hookLine })
		pre := 0
		for _, line := range unitLines {
			if strings.HasPrefix(line, "ExecStartPre=") {
				pre++
			}
		}
		if index < 0 || !strings.HasPrefix(unitLines[index+1], "ExecStart=") || pre != 1 {
			t.Error("systemd hook line")
		}
	} else if first := indexOf(unitLines, func(l string) bool { return l == "start_pre() {" }); first < 0 || unitLines[first+1] != "    "+hookLine+" || return 1" {
		t.Error("openrc hook line")
	}
	if readFile(t, h.proc) != "1\n" || h.leftovers() {
		t.Error("forwarding not enabled or leftovers")
	}
}

// snapshotRoot is snapshot(paths): regular files under root except staged configs.
func (h *standaloneHarness) snapshotRoot(t *testing.T) map[string]string {
	out := map[string]string{}
	filepath.Walk(h.root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() && !strings.HasSuffix(info.Name(), ".json.new") {
			out[path] = readFile(t, path)
		}
		return nil
	})
	return out
}

func (h *standaloneHarness) symlinks(t *testing.T) map[string]string {
	out := map[string]string{}
	filepath.Walk(h.root, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			out[path] = target
		}
		return nil
	})
	return out
}

func sameMap(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, found := b[k]; !found || w != v {
			return false
		}
	}
	return true
}

func TestStandaloneInstallPersistsForwardingAndIsRepeatable(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, initial := range []string{"0", "1"} {
			for _, core := range []bool{false, true} {
				manager, initial, core := manager, initial, core
				t.Run(manager+initial+map[bool]string{false: "", true: "+core"}[core], func(t *testing.T) {
					t.Parallel()
					h := newStandaloneHarness(t, standaloneSetup{manager: manager, installCore: core})
					writeFile(t, h.proc, initial+"\n", 0o644)
					for attempt := 0; attempt < 2; attempt++ {
						writeFile(t, h.events, "", 0o644)
						r := h.run(t, "1")
						if r.code != 0 {
							t.Fatal(r.stdout + r.stderr)
						}
						h.assertPersisted(t, manager)
						writes := h.sysctlWrites(t)
						for _, line := range writes {
							if line != "sysctl -q -w net.ipv4.ip_forward=0" && line != "sysctl -q -w net.ipv4.ip_forward=1" {
								t.Error(line)
							}
						}
						if initial == "1" || attempt > 0 {
							for _, line := range writes {
								if line == "sysctl -q -w net.ipv4.ip_forward=0" {
									t.Error("disabled forwarding")
								}
							}
						}
						contains(t, r.stdout, "IPv4 转发已开启并持久化")
					}
				})
			}
		}
	}
}

func TestStandaloneOptOutLeavesForwardingAlone(t *testing.T) {
	off := ForwardingOff
	managers(t, func(t *testing.T, manager string) {
		h := newStandaloneHarness(t, standaloneSetup{manager: manager, forwarding: &off})
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if exists(h.conf) || exists(h.hook) || strings.Contains(readFile(t, h.unit), "enable-ipv4-forwarding") ||
			readFile(t, h.proc) != "0\n" || len(h.sysctlWrites(t)) != 0 {
			t.Error("forwarding touched")
		}
	})
}

func TestGatewayRecordRefusesBeforeTheAgentIsStopped(t *testing.T) {
	off := ForwardingOff
	managers(t, func(t *testing.T, manager string) {
		h := newStandaloneHarness(t, standaloneSetup{manager: manager})
		writeFile(t, filepath.Join(h.agentDir, "config-mihomo.json"), `{"agent_name": "single", "service_type": "mihomo", "agent_id": "id", "token": "token"}`, 0o644)
		writeFile(t, filepath.Join(h.agentDir, "linux-gateway.json"), "{\"owner\":\"gateway\"}\n", 0o644)
		writeFile(t, h.unit, "# previous Agent unit\n", 0o644)
		before := h.snapshotRoot(t)
		r := h.run(t, "1")
		if r.code == 0 {
			t.Fatal("succeeded")
		}
		contains(t, r.stderr, "不开启 IPv4 转发")
		contains(t, r.stderr, "Agent 页这台设备的卡片上")
		if h.stopped(t) || !sameMap(h.snapshotRoot(t), before) || exists(h.conf) || readFile(t, h.proc) != "0\n" || len(h.sysctlWrites(t)) != 0 || h.leftovers() {
			t.Error("host changed")
		}
		h = newStandaloneHarness(t, standaloneSetup{manager: manager, forwarding: &off})
		writeFile(t, filepath.Join(h.agentDir, "linux-gateway.json"), "{\"owner\":\"gateway\"}\n", 0o644)
		r = h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if exists(h.conf) || len(h.sysctlWrites(t)) != 0 {
			t.Error("opt-out touched forwarding")
		}
	})
}

func TestUnsafeHostStateFailsBeforeTheAgentIsStopped(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, problem := range []string{"foreign_config", "symlink_config", "symlink_hook", "sysctl_denied"} {
			manager, problem := manager, problem
			t.Run(manager+"/"+problem, func(t *testing.T) {
				t.Parallel()
				h := newStandaloneHarness(t, standaloneSetup{manager: manager})
				os.MkdirAll(filepath.Join(h.root, "etc/sysctl.d"), 0o755)
				os.MkdirAll(h.agentDir, 0o755)
				var extra []string
				switch problem {
				case "foreign_config":
					writeFile(t, h.conf, "net.ipv4.ip_forward=1\n", 0o644)
				case "symlink_config":
					os.Symlink(writeFile(t, filepath.Join(h.tmp, "elsewhere.conf"), forwardContent, 0o644), h.conf)
				case "symlink_hook":
					os.Symlink(writeFile(t, filepath.Join(h.tmp, "elsewhere.sh"), "exit 0\n", 0o644), h.hook)
				default:
					extra = []string{"FAIL_SYSCTL_WRITES=yes"}
				}
				before, links := h.snapshotRoot(t), h.symlinks(t)
				r := h.run(t, "1", extra...)
				if r.code == 0 {
					t.Fatal(r.stdout)
				}
				if h.stopped(t) || !sameMap(h.snapshotRoot(t), before) || !sameMap(h.symlinks(t), links) || readFile(t, h.proc) != "0\n" || h.leftovers() {
					t.Error("host changed")
				}
				for _, line := range h.sysctlWrites(t) {
					if line == "sysctl -q -w net.ipv4.ip_forward=1" {
						t.Error("enabled forwarding")
					}
				}
				if problem == "sysctl_denied" {
					contains(t, r.stderr, "CAP_NET_ADMIN")
				}
			})
		}
	}
}

func TestFailedAgentStartCommitsNothingAndARerunConverges(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newStandaloneHarness(t, standaloneSetup{manager: manager})
		if r := h.run(t, "1", "FAIL_AGENT_START=yes"); r.code == 0 {
			t.Fatal("succeeded")
		}
		if exists(h.conf) || readFile(t, h.proc) != "0\n" || h.leftovers() {
			t.Error("committed")
		}
		if r := h.run(t, "1"); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		h.assertPersisted(t, manager)
	})
}

func TestUninstallKeepsThePersistedForwarding(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newStandaloneHarness(t, standaloneSetup{manager: manager})
		if r := h.run(t, "1"); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		r := h.run(t, "2")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		contains(t, r.stdout, "IPv4 转发及其 sysctl.d 持久化设置保留")
		if readFile(t, h.conf) != forwardContent || exists(h.hook) || exists(h.unit) || readFile(t, h.proc) != "1\n" {
			t.Error("forwarding removed")
		}
	})
}
