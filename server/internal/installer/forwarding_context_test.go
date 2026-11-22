package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_installer_forwarding_context.py: installers that
// persist IPv4 forwarding tell the environment check so
// (CONFIGFLOW_INSTALLER_FORWARDING=persist); the others keep their scripts.

const (
	installerForwardingVariable = "CONFIGFLOW_INSTALLER_FORWARDING"
	filterNote                  = "转发过滤：这台 Linux 的防火墙可能拦截经过 Mihomo TUN 的流量，安装不受影响；影响范围和放行命令见上方「转发过滤」或完整日志。"
	conflictNote                = "IPv4 转发冲突：其他 sysctl 设置会在重启后把 net.ipv4.ip_forward 改回 0；处理方法见上方「IPv4 转发冲突」或完整日志。"
)

var forwardingExport = InstallerForwardingEnvironment + "if cf_environment_check "

func checkSh(t *testing.T, script string) {
	t.Helper()
	if r := runShell(t, "sh", "-n", runOptions{args: []string{writeTemp(t, "script.sh", script)}}); r.code != 0 {
		t.Fatal(r.stderr)
	}
}

func TestSingleServiceMihomoThatPersistsForwardingTellsTheEnvironmentCheck(t *testing.T) {
	for _, mutate := range []func(*AgentInstallOptions){
		forwardingOption(ForwardingAuto), forwardingOption(ForwardingOn),
		func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = ForwardingAuto; o.InstallCore = true },
	} {
		script := ok(standaloneGenerated("mihomo", mutate))
		if strings.Count(script, forwardingExport) != 1 ||
			strings.Count(script, InstallerForwardingEnvironmentEnd+`cf_step_done "核心接管与运行环境检查通过"`) != 1 ||
			strings.Count(script, installerForwardingVariable) != 3 {
			t.Error("forwarding context")
		}
		checkSh(t, script)
	}
}

func TestSingleServiceGatewayRecordIsRefusedBeforeTheEnvironmentCheck(t *testing.T) {
	for _, core := range []bool{false, true} {
		script := ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = ForwardingAuto; o.InstallCore = core }))
		refusal := strings.Index(script, "if [ -e \"$AGENT_DIR/linux-gateway.json\" ] || [ -L \"$AGENT_DIR/linux-gateway.json\" ]; then\n    fail \"检测到 Linux 网关记录")
		export, step := strings.Index(script, forwardingExport), strings.Index(script, `cf_step "检查 IPv4 转发持久化"`)
		if refusal < 0 || !(refusal < export && export < step) || strings.Count(script, "检测到 Linux 网关记录") != 1 {
			t.Errorf("core=%v: order %d %d %d", core, refusal, export, step)
		}
	}
}

func TestInstallsThatDoNotManageForwardingNeverSetTheContext(t *testing.T) {
	for _, c := range []struct {
		service string
		mutate  func(*AgentInstallOptions)
	}{
		{"mihomo", forwardingOption(ForwardingOff)},
		{"mihomo", func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = ForwardingOff; o.InstallCore = true }},
		{"mihomo", func(o *AgentInstallOptions) {
			o.ManageIPv4Forwarding = ForwardingAuto
			o.ServiceManager, o.StopCommand, o.StartCommand, o.StatusCommand = "command", "stop-it", "start-it", "status-it"
		}},
		{"oxidns", forwardingOption(ForwardingAuto)},
		{"oxidns", func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = ForwardingAuto; o.InstallCore = true }},
		{"mosdns", forwardingOption(ForwardingAuto)},
	} {
		script := ok(standaloneGenerated(c.service, c.mutate))
		notContains(t, script, installerForwardingVariable)
		notContains(t, script, "{ipv4_forwarding_environment")
	}
}

func TestAIOThatPersistsForwardingTellsTheEnvironmentCheck(t *testing.T) {
	for _, script := range []string{ok(aioGenerated(t, nil)), ok(aioGenerated(t, func(o *AIOInstallOptions) { o.InstallCore = true }))} {
		if strings.Count(script, forwardingExport+`"$BINARY_TMP" -config "$STAGE_DIR/mihomo-config.json"`) != 1 ||
			strings.Count(script, InstallerForwardingEnvironmentEnd+`cf_step_done "运行环境检查通过"`) != 1 ||
			strings.Count(script, installerForwardingVariable) != 3 {
			t.Error("forwarding context")
		}
		checkSh(t, script)
	}
	o := DefaultAIOOptions("https://config.test", "gw")
	o.ManageIPv4Forwarding, o.ManageTun = false, false
	selective := ok(RenderAIOInstall(o))
	notContains(t, selective, installerForwardingVariable)
	notContains(t, selective, "{ipv4_forwarding_environment")
}

func TestCoreOnlyInstallerIsInstallCoresShUnchanged(t *testing.T) {
	expected := WithInstallOutput(script("install-cores.sh"))
	if ok(GenerateCoreInstallScript(false)) != expected {
		t.Fatal("core installer differs from install-cores.sh")
	}
	contains(t, expected, "旁路网关修复：")
}

func TestCorePhaseOfAForwardingInstallerDefersToTheAgentStep(t *testing.T) {
	for name, source := range map[string]string{
		"single": ok(standaloneGenerated("mihomo", func(o *AgentInstallOptions) { o.ManageIPv4Forwarding = ForwardingAuto; o.InstallCore = true })),
		"aio":    ok(aioGenerated(t, func(o *AIOInstallOptions) { o.InstallCore = true })),
	} {
		contains(t, source, pyRstrip(ok(GenerateCoreInstallScript(true))))
		notContains(t, source, "旁路网关修复：")
		notContains(t, source, "99-configflow-forward.conf")
		notContains(t, source, "cf_note forwarding 'IPv4 转发：当前已开启")
		if !strings.Contains(source, "安装器将在 Agent 步骤开启并持久化（/etc/sysctl.d/99-configflow-aio-forward.conf）") {
			t.Errorf("%s: no deferral text", name)
		}
	}
}

func TestCorePhaseTextRequiresTheExactInstallCoresBlock(t *testing.T) {
	// Python points core_install_script.__file__ at a changed install-cores.sh;
	// the Go renderer reads the embedded template, swapped here.
	original := installCoresTemplate
	installCoresTemplate = "#!/bin/sh\necho changed\n"
	defer func() { installCoresTemplate = original }()
	contains(t, ok(GenerateCoreInstallScript(false)), "changed")
	if _, err := GenerateCoreInstallScript(true); err == nil || !strings.Contains(err.Error(), "install-cores.sh") {
		t.Fatalf("err %v", err)
	}
}

func TestCorePhaseForwardingBlockReportsTheCurrentValue(t *testing.T) {
	for _, c := range []struct {
		value    *string
		expected string
	}{
		{strp("0\n"), "IPv4 转发：当前为 0，安装器将在 Agent 步骤开启并持久化（/etc/sysctl.d/99-configflow-aio-forward.conf）。"},
		{strp("1\n"), "IPv4 转发：当前为 1，安装器将在 Agent 步骤持久化（/etc/sysctl.d/99-configflow-aio-forward.conf）。"},
		{nil, "IPv4 转发：状态无法读取，安装器将在 Agent 步骤再次检查并开启。"},
	} {
		proc := filepath.Join(t.TempDir(), "ip_forward")
		if c.value != nil {
			writeFile(t, proc, *c.value, 0o644)
		}
		block := strings.ReplaceAll(installerForwardingBlock, "/proc/sys/net/ipv4/ip_forward", proc)
		r := runShell(t, "sh", "-eu", runOptions{args: []string{"-c", "cf_note() { echo \"note $*\"; }\n" + block}})
		if r.code != 0 || r.stdout != c.expected+"\n" {
			t.Errorf("%q: %d %q %s", proc, r.code, r.stdout, r.stderr)
		}
	}
}

func TestContextReachesOnlyTheEnvironmentCheck(t *testing.T) {
	stub := writeFile(t, filepath.Join(t.TempDir(), "agent"), "#!/bin/sh\nprintf \"check:%s\\n\" \"${CONFIGFLOW_INSTALLER_FORWARDING-unset}\"\n", 0o755)
	body := InstallerForwardingEnvironment +
		"if cf_environment_check \"$BINARY_TMP\" -check-environment; then\n    :\nelse\n    exit 1\nfi\n" +
		InstallerForwardingEnvironmentEnd +
		"printf \"after:%s\\n\" \"${CONFIGFLOW_INSTALLER_FORWARDING-unset}\"\n" +
		"sh -c 'printf \"service:%s\\n\" \"${CONFIGFLOW_INSTALLER_FORWARDING-unset}\"'\n"
	r := runShell(t, "sh", "-eu", runOptions{args: []string{"-c", "cf_environment_check() { \"$@\"; }\n" + body},
		env: []string{"PATH=/usr/bin:/bin", "BINARY_TMP=" + stub}, replace: true})
	if r.code != 0 || r.stdout != "check:persist\nafter:unset\nservice:unset\n" {
		t.Fatalf("%q %s", r.stdout, r.stderr)
	}
}

func TestSummaryRepeatsWarningsFromTheDetailedLog(t *testing.T) {
	for _, filtered := range []bool{true, false} {
		for _, conflict := range []bool{true, false} {
			text := "[通过] IPv4 转发（安装器管理）：当前 net.ipv4.ip_forward=0\n"
			if conflict {
				text += "[提醒] IPv4 转发冲突：/etc/sysctl.d/99-zz.conf:1 在 ConfigFlow 的持久化文件之后加载\n"
			}
			if filtered {
				text += "[提醒] 转发过滤：这台 Linux 的防火墙会拦截转发到 Mihomo TUN（Meta）的流量\n  命令：iptables -I FORWARD -o Meta -j ACCEPT\n"
			} else {
				text += "[信息] 转发过滤：未能确认转发过滤（timeout），不影响安装。\n"
			}
			log := writeTemp(t, "install.log", text)
			harness := "cf_output_is_active() { return 1; }\n" +
				"cf_note() { printf \"note %s %s\\n\" \"$1\" \"$2\"; }\n" +
				"cf_fact() { printf \"fact %s\\n\" \"$1\"; }\n" +
				installGoAIOForwarding + "\nsummarize_ipv4_forwarding\n"
			r := runShell(t, "sh", "-eu", runOptions{args: []string{"-c", harness}, env: []string{"PATH=/usr/bin:/bin", "CONFIGFLOW_LOG_FILE=" + log}, replace: true})
			if r.code != 0 {
				t.Fatal(r.stderr)
			}
			if strings.Contains(r.stdout, "note forward_filter "+filterNote+"\n") != filtered ||
				strings.Contains(r.stdout, "note forwarding_conflict "+conflictNote+"\n") != conflict ||
				!strings.Contains(r.stdout, "note forwarding IPv4 转发已设置持久化") || !strings.HasSuffix(r.stdout, "fact ipv4_forwarding\n") {
				t.Errorf("filtered=%v conflict=%v: %s", filtered, conflict, r.stdout)
			}
		}
	}
}

func TestSummaryWithoutALogFileAddsNoForwardFilterNote(t *testing.T) {
	harness := "cf_output_is_active() { return 1; }\ncf_note() { printf \"note %s\\n\" \"$1\"; }\n" +
		"cf_fact() { :; }\n" + installGoAIOForwarding + "\nsummarize_ipv4_forwarding\n"
	r := runShell(t, "sh", "-eu", runOptions{args: []string{"-c", harness}, env: []string{"PATH=/usr/bin:/bin"}, replace: true})
	if r.code != 0 || r.stdout != "note forwarding\n" {
		t.Fatalf("%q %s", r.stdout, r.stderr)
	}
}

// wrapAgent is wrap_agent(tmp_path, agent, warn): records the context of
// every Agent call; optionally logs a forward-filter warning.
func wrapAgent(t *testing.T, tmp, agent string, warn bool) string {
	real := filepath.Join(tmp, "fake-agent.real")
	if err := os.Rename(agent, real); err != nil {
		t.Fatal(err)
	}
	contexts := filepath.Join(tmp, "contexts")
	yes := "no"
	if warn {
		yes = "yes"
	}
	writeFile(t, agent, `#!/bin/sh
for arg in "$@"; do last=$arg; done
printf '%s %s\n' "${last:-none}" "${CONFIGFLOW_INSTALLER_FORWARDING-unset}" >> `+contexts+`
if [ "${last:-}" = -check-environment ] && [ `+yes+` = yes ] && [ -n "${CONFIGFLOW_LOG_FILE:-}" ]; then
    printf '%s\n' '[提醒] 转发过滤：这台 Linux 的防火墙会拦截转发到 Mihomo TUN（Meta）的流量。' \
        '  命令：iptables -I FORWARD -o Meta -j ACCEPT' >> "$CONFIGFLOW_LOG_FILE"
fi
exec `+real+` "$@"
`, 0o755)
	return contexts
}

func recordedContexts(t *testing.T, contexts string) [][2]string {
	var out [][2]string
	for _, line := range lines(readFile(t, contexts)) {
		flag, value, _ := strings.Cut(line, " ")
		out = append(out, [2]string{flag, value})
	}
	return out
}

func checkContexts(t *testing.T, calls [][2]string, expected string) {
	t.Helper()
	found := false
	for _, call := range calls {
		if call[0] == "-check-environment" {
			found = found || call[1] == expected
		} else if call[1] != "unset" {
			t.Errorf("context leaked: %v", call)
		}
	}
	if !found {
		t.Errorf("no -check-environment %s in %v", expected, calls)
	}
}

func TestAIOInstallPassesTheContextToItsEnvironmentCheckOnly(t *testing.T) {
	for _, warn := range []bool{false, true} {
		h := newAIOHarness(t, "systemd", true, nil)
		contexts := wrapAgent(t, h.tmp, h.path("fake-agent"), warn)
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		checkContexts(t, recordedContexts(t, contexts), "persist")
		if strings.Contains(r.stderr, filterNote) != warn {
			t.Errorf("warn=%v: %s", warn, r.stderr)
		}
		contains(t, r.stdout, "IPv4 转发已开启并持久化")
	}
}

func TestSingleServiceInstallPassesTheContextOnlyWhenItPersistsForwarding(t *testing.T) {
	auto, off := ForwardingAuto, ForwardingOff
	for _, c := range []struct {
		forwarding *Forwarding
		expected   string
	}{{&auto, "persist"}, {&off, "unset"}} {
		h := newStandaloneHarness(t, standaloneSetup{forwarding: c.forwarding})
		contexts := wrapAgent(t, h.tmp, filepath.Join(h.tmp, "fake-agent"), true)
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		checkContexts(t, recordedContexts(t, contexts), c.expected)
		if strings.Contains(r.stderr, filterNote) != (*c.forwarding == ForwardingAuto) {
			t.Errorf("%s: %s", c.expected, r.stderr)
		}
	}
}
