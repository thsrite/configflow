package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Port of backend/test_aio_network_prerequisites.py: network setup
// regressions, with all installer effects confined to temp files.

// shellFunction is shell_function(filename, name): a top-level function of
// an embedded script, extracted without running the installer around it.
func shellFunction(t *testing.T, filename, name string) string {
	t.Helper()
	source, found := Script(filename)
	require(t, found, "no script %s", filename)
	match := regexp.MustCompile(`(?ms)^` + regexp.QuoteMeta(name) + `\(\) \{\n.*?^\}$`).FindString(source)
	require(t, match != "", "Missing shell function %s", name)
	return match
}

const checkEnvironmentMarker = "if '-check-environment' in args:\n"

func TestMissingForwardingPrerequisiteFailsBeforeStoppingAgents(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		for _, tool := range []string{"sysctl", "cmp", "ln"} {
			manager, tool := manager, tool
			t.Run(manager+"/"+tool, func(t *testing.T) {
				t.Parallel()
				h := newAIOHarness(t, manager, true, nil)
				e, _ := isolateDependencies(t, h.tmp, env(h.env), nil, "", true)
				e = e.with("MISSING_TOOL_READY="+h.path("missing-tool-ready"), "MISSING_TOOL="+tool)
				agent := e.get("FAKE_AGENT")
				source := readFile(t, agent)
				require(t, strings.Contains(source, checkEnvironmentMarker), "marker")
				writeFile(t, agent, strings.Replace(source, checkEnvironmentMarker,
					checkEnvironmentMarker+"    open(os.environ['MISSING_TOOL_READY'], 'w').close()\n", 1), 0o755)
				before := snapshotFiles(t, h.path("agent"), h.units)
				override := `command() {
    if [ -f "$MISSING_TOOL_READY" ] && [ "$#" -eq 2 ] && [ "$1" = -v ] && [ "$2" = "$MISSING_TOOL" ]; then
        return 127
    fi
    /bin/sh -c 'command "$@"' sh "$@"
}
`
				writeFile(t, h.installer, strings.Replace(readFile(t, h.installer), "#!/bin/sh\n", "#!/bin/sh\n"+override, 1), 0o644)
				r := runShell(t, "sh", h.installer, runOptions{env: e, replace: true, input: "1\n", timeout: 30e9})
				if r.code == 0 {
					t.Fatal("succeeded")
				}
				contains(t, detailOutput(t, r), "缺少 "+tool+" 命令")
				notContains(t, r.stdout, "安装完成")
				events := h.events(t)
				if !anyLine(events, func(l string) bool { return strings.Contains(l, " -check-environment ") }) {
					t.Error("no environment check")
				}
				for _, event := range events {
					padded := " " + event + " "
					if strings.Contains(padded, " stop ") || strings.Contains(padded, " start ") || strings.Contains(event, "daemon-reload") || strings.HasPrefix(event, "sysctl ") {
						t.Errorf("lifecycle event %s", event)
					}
				}
				sameFiles(t, before)
				if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) || globAny(h.path("agent/.aio-install.*")) {
					t.Error("forwarding or stage")
				}
			})
		}
	}
}

func TestForwardingSummaryOnlyReplacesResolvedWarningAndPreservesLog(t *testing.T) {
	tmp := t.TempDir()
	resolved := "IPv4 转发：当前 net.ipv4.ip_forward=0；作为网关时需开启。\n"
	names := []string{"note.environment_1", "note.environment_2", "note.environment_3", "note.environment_4", "note.environment_5", "note.administrator", "note.forwarding"}
	warnings := map[string]string{
		"note.environment_1": resolved,
		"note.environment_2": "DNS 监听：端口已被其他服务占用。\n",
		"note.environment_3": "IPv4 转发：持久化文件被其他服务覆盖的风险仍存在。\n",
		"note.environment_4": "IPv4 转发：当前 net.ipv4.ip_forward=1；请核对启动顺序。\n",
		"note.environment_5": "转发检查历史：" + resolved,
		"note.administrator": resolved,
		"note.forwarding":    "IPv4 转发：当前为 0，持久化未验证。\n",
	}
	originalLog := "检查时的原始状态\n"
	for _, name := range names {
		writeFile(t, filepath.Join(tmp, name), warnings[name], 0o644)
		originalLog += warnings[name]
	}
	writeFile(t, filepath.Join(tmp, "install.log"), originalLog, 0o644)
	source := WithInstallOutput("#!/bin/sh\n" + strings.Join([]string{
		"set -eu",
		shellFunction(t, "aio-ipv4-forwarding.sh", "summarize_ipv4_forwarding"),
		`for note in "$1"/note.*; do cp "$note" "$CF_OUTPUT_DIR/"; done`,
		`cat "$1/install.log"`,
		`cp "$CONFIGFLOW_LOG_FILE" "$1/log-before-summary"`,
		"summarize_ipv4_forwarding",
		`cp "$CONFIGFLOW_LOG_FILE" "$1/log-after-summary"`,
	}, "\n"))
	e := baseEnv().without("CONFIGFLOW_CLI_ACTIVE", "CONFIGFLOW_LOG_FILE", "CONFIGFLOW_LOG_STATE_DIR", "CONFIGFLOW_LOG_CURSOR",
		"CONFIGFLOW_REPORT_FD", "CONFIGFLOW_REPORT_NESTED").with("CONFIGFLOW_LOG_DIR="+filepath.Join(tmp, "logs"), "CONFIGFLOW_VERBOSE=0")
	r := runShell(t, "/bin/sh", "-c", runOptions{args: []string{source, "summary-test", tmp}, env: e, replace: true})
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	logs, _ := filepath.Glob(filepath.Join(tmp, "logs", "configflow-install.*", "install.log"))
	if len(logs) != 1 {
		t.Fatal(logs)
	}
	report := filepath.Dir(logs[0])
	if exists(filepath.Join(report, "note.environment_1")) {
		t.Error("resolved warning kept")
	}
	for name, content := range warnings {
		if name != "note.environment_1" && name != "note.forwarding" && readFile(t, filepath.Join(report, name)) != content {
			t.Errorf("%s changed", name)
		}
	}
	summary := readFile(t, filepath.Join(report, "note.forwarding"))
	for _, needle := range []string{"已设置持久化", "尚未实际重启验证", "其他配置或服务仍可能在重启后把它改回 0", "sysctl net.ipv4.ip_forward 确认仍为 1"} {
		contains(t, summary, needle)
	}
	notContains(t, summary, "原检查结果")
	notContains(t, summary, "当前为 0")
	contains(t, readFile(t, filepath.Join(report, "fact.ipv4_forwarding")), "已开启并持久化")
	if readFile(t, filepath.Join(tmp, "log-before-summary")) != readFile(t, filepath.Join(tmp, "log-after-summary")) {
		t.Error("summary changed the log")
	}
	contains(t, readFile(t, logs[0]), originalLog)
}

func TestSuccessfulAIOInstallDoesNotRepeatResolvedForwardingWarning(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		agent := h.path("fake-agent")
		resolved := "[提醒] IPv4 转发：当前 net.ipv4.ip_forward=0；作为网关时需开启。"
		unrelated := "[提醒] DNS 检查：仍需核对客户端入口。"
		risk := "[提醒] IPv4 转发持久化：其他配置或服务可能覆盖。"
		source := readFile(t, agent)
		require(t, strings.Contains(source, checkEnvironmentMarker), "marker")
		prints := ""
		for _, message := range []string{resolved, unrelated, risk} {
			prints += "    print('" + message + "')\n" // repr() of these quote-free strings
		}
		writeFile(t, agent, strings.Replace(source, checkEnvironmentMarker, checkEnvironmentMarker+prints, 1), 0o755)
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		out := r.stdout + r.stderr
		if readFile(t, h.path("ip_forward")) != "1\n" {
			t.Error("forwarding")
		}
		for _, needle := range []string{"IPv4 转发已开启并持久化", "尚未实际重启验证", "其他配置或服务仍可能在重启后把它改回 0", "DNS 检查：仍需核对客户端入口。"} {
			contains(t, out, needle)
		}
		notContains(t, out, "当前 net.ipv4.ip_forward=0")
		log := detailOutput(t, r)
		for _, message := range []string{resolved, unrelated, risk} {
			contains(t, log, message)
		}
	})
}

func TestCoreTunPermissionsGuidanceMatchesActualServiceManager(t *testing.T) {
	for _, manager := range []string{"systemd", "openrc"} {
		source := strings.Join([]string{
			"set -eu",
			`cf_fact() { printf "fact:%s:%s\n" "$1" "$2"; }`,
			`cf_note() { printf "note:%s:%s\n" "$1" "$2"; }`,
			shellFunction(t, "install-cores.sh", "show_tun_disabled_guidance"),
			`INIT_SYSTEM="$1"; TUN_REASON="/dev/net/tun 未提供。"`,
			"show_tun_disabled_guidance",
		}, "\n")
		r := runShell(t, "/bin/sh", "-c", runOptions{args: []string{source, "guidance-test", manager}})
		if r.code != 0 {
			t.Fatal(r.stderr)
		}
		for _, needle := range []string{"modprobe tun", "PVE 宿主机", "CAP_NET_ADMIN", "CapEff", "fact:tun:TUN：已关闭", "note:tun:TUN 未启用：/dev/net/tun 未提供。"} {
			contains(t, r.stdout, needle)
		}
		if manager == "openrc" {
			for _, needle := range []string{"权限（OpenRC）", "/etc/init.d/mihomo", "/etc/conf.d/mihomo", "command_user", "capabilities", "root 身份不能证明具备该能力"} {
				contains(t, r.stdout, needle)
			}
			for _, option := range []string{"systemd", "CapabilityBoundingSet", "DeviceAllow", "AmbientCapabilities"} {
				notContains(t, r.stdout, option)
			}
		} else {
			for _, needle := range []string{"权限（systemd）", "CapabilityBoundingSet", "DeviceAllow=/dev/net/tun rw", "AmbientCapabilities"} {
				contains(t, r.stdout, needle)
			}
			notContains(t, r.stdout, "OpenRC")
			notContains(t, r.stdout, "command_user")
		}
	}
}

func TestForwardingPersistenceMakesOpenRCApplySysctlAtBoot(t *testing.T) {
	for _, c := range []string{"added", "failed", "unavailable", "enabled", "systemd"} {
		c := c
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			manager := "openrc"
			if c == "systemd" {
				manager = "systemd"
			}
			h := newAIOHarness(t, manager, true, nil)
			runlevels := h.path("etc/runlevels")
			script := h.path("etc/init.d/sysctl")
			if c != "unavailable" {
				writeFile(t, script, "#!/sbin/openrc-run\n", 0o644)
			}
			if c == "enabled" {
				os.MkdirAll(filepath.Join(runlevels, "boot"), 0o755)
				os.Symlink(script, filepath.Join(runlevels, "boot/sysctl"))
			}
			if rcUpdate := h.path("commands/rc-update"); exists(rcUpdate) {
				writeFile(t, rcUpdate, "#!/bin/sh\nprintf '%s %s\\n' \"${0##*/}\" \"$*\" >> \"$EVENTS\"\n"+
					"if [ \"$*\" = \"add sysctl boot\" ]; then\n"+
					"    [ \"${FAIL_RC_UPDATE:-}\" != yes ] || exit 1\n"+
					fmt.Sprintf("    mkdir -p \"%s/boot\" && ln -s \"%s\" \"%s/boot/sysctl\"\n", runlevels, script, runlevels)+
					"fi\n", 0o755)
			}
			var extra []string
			if c == "failed" {
				extra = []string{"FAIL_RC_UPDATE=yes"}
			}
			r := h.run(t, "1", extra...)
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			if readFile(t, h.path("ip_forward")) != "1\n" {
				t.Error("forwarding")
			}
			added := 0
			for _, line := range h.events(t) {
				if line == "rc-update add sysctl boot" {
					added++
				}
			}
			want := 0
			if c == "added" || c == "failed" {
				want = 1
			}
			if added != want {
				t.Errorf("added %d want %d", added, want)
			}
			out := r.stdout + detailOutput(t, r)
			switch c {
			case "added":
				if info, err := os.Lstat(filepath.Join(runlevels, "boot/sysctl")); err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Error("not added")
				}
				contains(t, out, "已执行 rc-update add sysctl boot")
			case "failed":
				contains(t, out, "无法自动把 OpenRC 的 sysctl 服务加入开机运行级别")
				contains(t, out, "请执行：rc-update add sysctl boot")
			case "unavailable":
				contains(t, out, "系统没有 OpenRC 的 sysctl 服务")
				contains(t, out, "rc-update add local default")
				contains(t, out, "/etc/local.d/configflow-ip-forward.start")
			default:
				notContains(t, out, "forwarding_boot")
				notContains(t, out, "OpenRC 的 sysctl 服务")
			}
		})
	}
}
