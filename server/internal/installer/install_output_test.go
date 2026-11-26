package installer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_install_output.py: installer output must not change
// shell failures, traps or nested installs.

func TestDownloadedInstallerHandlesPrivilegesWithoutLosingStdinArgsOrExitStatus(t *testing.T) {
	for _, shell := range shells() {
		for _, root := range []bool{false, true} {
			for _, exitCode := range []int{0, 19} {
				t.Run(fmt.Sprintf("%s/root=%v/%d", filepath.Base(shell), root, exitCode), func(t *testing.T) {
					tmp := t.TempDir()
					commands := filepath.Join(tmp, "commands")
					os.MkdirAll(commands, 0o755)
					os.Symlink(shell, filepath.Join(commands, "sh"))
					writeFile(t, filepath.Join(commands, "id"), "#!/bin/sh\n"+`if [ "$TEST_IS_ROOT" = yes ]; then printf "0\n"; else printf "1000\n"; fi`+"\n", 0o755)
					writeFile(t, filepath.Join(commands, "sudo"), "#!/bin/sh\n"+`printf "sudo\n" >> "$TEST_EVENTS"; TEST_IS_ROOT=yes; export TEST_IS_ROOT; exec "$@"`+"\n", 0o755)
					source := WithRootPrivileges("#!/bin/sh\n# ConfigFlow test installer\nset -eu\n" +
						`trap 'printf "cleanup\n" >> "$TEST_EVENTS"' 0` + "\nIFS= read -r answer\n" +
						`printf '%s\n' "$answer" "$1" "$2" >> "$TEST_EVENTS"` + "\nexit " + fmt.Sprint(exitCode) + "\n")
					if strings.Split(source, "\n")[1] != "# ConfigFlow test installer" {
						t.Fatal("marker moved")
					}
					script := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
					isRoot := "no"
					if root {
						isRoot = "yes"
					}
					result := runShell(t, shell, script, runOptions{args: []string{"argument with spaces", "--verbose"}, replace: true,
						env:   []string{"PATH=" + commands, "TEST_EVENTS=" + filepath.Join(tmp, "events"), "TEST_IS_ROOT=" + isRoot},
						input: "terminal-input\n", timeout: 5e9})
					if result.code != exitCode {
						t.Fatalf("exit %d: %s", result.code, result.stderr)
					}
					want := "terminal-input\nargument with spaces\n--verbose\ncleanup\n"
					if !root {
						want = "sudo\n" + want
					}
					if got := readFile(t, filepath.Join(tmp, "events")); got != want {
						t.Fatalf("events %q", got)
					}
				})
			}
		}
	}
}

func TestDownloadedInstallerMissingSudoStopsBeforeInstallation(t *testing.T) {
	tmp := t.TempDir()
	commands := filepath.Join(tmp, "commands")
	writeFile(t, filepath.Join(commands, "id"), "#!/bin/sh\nprintf \"1000\\n\"\n", 0o755)
	script := writeFile(t, filepath.Join(tmp, "install.sh"), WithRootPrivileges("#!/bin/sh\nprintf unexpected > \"$TEST_EVENTS\"\n"), 0o644)
	result := runShell(t, "/bin/sh", script, runOptions{replace: true,
		env: []string{"PATH=" + commands, "TEST_EVENTS=" + filepath.Join(tmp, "events")}, timeout: 5e9})
	if result.code != 1 || !strings.Contains(result.stderr, "root") || !strings.Contains(result.stderr, "sudo") {
		t.Fatalf("%+v", result)
	}
	if _, err := os.Stat(filepath.Join(tmp, "events")); err == nil {
		t.Fatal("installation ran")
	}
}

func runOutputScript(t *testing.T, tmp, body, shell string, args []string, overrides ...string) runResult {
	script := writeFile(t, filepath.Join(tmp, "install.sh"), WithInstallOutput("#!/bin/sh\n"+body), 0o644)
	env := append([]string{"CONFIGFLOW_INSTALL_LOG_DIR=" + tmp, "TEST_EVENTS=" + filepath.Join(tmp, "events")}, overrides...)
	return runShell(t, shell, script, runOptions{args: args, env: env, input: "1\n"})
}

func logFile(t *testing.T, tmp string) string {
	t.Helper()
	logs, _ := filepath.Glob(filepath.Join(tmp, "configflow-install.*", "install.log"))
	if len(logs) != 1 {
		t.Fatalf("logs %v", logs)
	}
	return logs[0]
}

func eachShell(t *testing.T, f func(t *testing.T, shell string)) {
	for _, shell := range shells() {
		t.Run(filepath.Base(shell), func(t *testing.T) { f(t, shell) })
	}
}

func TestSummaryKeepsDetailsPrivateAndInteractionVisible(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		tmp := t.TempDir()
		result := runOutputScript(t, tmp, `set -eu
cf_ui '1) 安装'
cf_prompt '请选择: '
read -r choice
[ "$choice" = 1 ]
cf_step '下载核心'
printf 'raw-download-progress\n'
printf 'raw-stderr-detail\n' >&2
cf_step_done '下载完成'
cf_fact core_mihomo_new yes
cf_fact service_mihomo 'Mihomo：运行中'
cf_note direct '没有节点，当前直连'
cf_complete '安装完成'
`, shell, nil)
		if result.code != 0 {
			t.Fatal(result.stderr)
		}
		contains(t, result.stdout, "1) 安装\n请选择: \n[进行] 下载核心")
		contains(t, result.stdout, "  Mihomo：运行中")
		contains(t, result.stderr, "完成 · 有提醒：安装完成")
		contains(t, result.stderr, "没有节点，当前直连")
		notContains(t, result.stdout+result.stderr, "raw-")
		notContains(t, result.stdout, "\n  yes\n")
		log := logFile(t, tmp)
		text := readFile(t, log)
		contains(t, text, "raw-download-progress")
		contains(t, text, "raw-stderr-detail")
		if strings.Index(text, "Mihomo：运行中") > strings.Index(text, "完成 · 有提醒：安装完成") {
			t.Error("fact after result")
		}
		info, _ := os.Stat(log)
		dirInfo, _ := os.Stat(filepath.Dir(log))
		if info.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
			t.Errorf("modes %v %v", info.Mode().Perm(), dirInfo.Mode().Perm())
		}
	})
}

func TestOriginalErrexitExitStatusAndCleanupArePreserved(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		for _, c := range []struct {
			failure string
			code    int
		}{{"false", 1}, {"exit 42", 42}} {
			tmp := t.TempDir()
			result := runOutputScript(t, tmp, `set -eu
trap 'printf "original-cleanup\n" >> "$TEST_EVENTS"' 0
cf_step '启动服务'
printf '修复建议：检查服务配置\n' >&2
`+c.failure+`
printf 'must-not-run\n' >> "$TEST_EVENTS"
cf_complete '安装完成'
`, shell, nil)
			if result.code != c.code {
				t.Fatalf("%s: exit %d", c.failure, result.code)
			}
			if got := readFile(t, filepath.Join(tmp, "events")); got != "original-cleanup\n" {
				t.Errorf("events %q", got)
			}
			contains(t, result.stderr, fmt.Sprintf("[失败] 启动服务（退出码 %d）", c.code))
			contains(t, result.stderr, "修复建议：检查服务配置")
			notContains(t, result.stdout, "安装完成")
			contains(t, result.stdout+result.stderr, logFile(t, tmp))
		}
	})
}

func TestNestedCoreSharesLogWithoutEarlySuccess(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		for _, coreFails := range []bool{false, true} {
			tmp := t.TempDir()
			tail := "cf_fact service_mihomo '核心已启动'\ncf_complete '核心安装完成'\n"
			if coreFails {
				tail = "exit 7\n"
			}
			nested := WithInstallOutput("#!/bin/sh\nset -eu\ncf_step '安装核心'\ntrap 'printf \"core-cleanup\\n\" >> \"$TEST_EVENTS\"' 0\n" + tail)
			result := runOutputScript(t, tmp, "set -eu\ntrap 'printf \"parent-cleanup\\n\" >> \"$TEST_EVENTS\"' 0\n(\n"+nested+")\ncf_step '安装 Agent'\nexit 19\n", shell, nil)
			want := 19
			if coreFails {
				want = 7
			}
			if result.code != want {
				t.Fatalf("exit %d", result.code)
			}
			if got := readFile(t, filepath.Join(tmp, "events")); got != "core-cleanup\nparent-cleanup\n" {
				t.Errorf("events %q", got)
			}
			notContains(t, result.stdout, "核心安装完成")
			if n := strings.Count(result.stderr, "[失败]"); n != 1 {
				t.Errorf("[失败] %d times", n)
			}
			if strings.Contains(result.stdout, "[进行] 安装 Agent") != !coreFails {
				t.Error("agent step visibility")
			}
			logFile(t, tmp)
		}
	})
}

func TestUnavailableLogFallsBackBeforeDependencyInstall(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		for _, brokenMktemp := range []bool{false, true} {
			tmp := t.TempDir()
			commands := filepath.Join(tmp, "commands")
			os.MkdirAll(commands, 0o755)
			if brokenMktemp {
				writeFile(t, filepath.Join(commands, "mktemp"), "#!"+shell+"\nprintf \"%s\\n\" \""+tmp+"/missing/log-dir\"\n", 0o755)
			}
			result := runOutputScript(t, tmp, `set -eu
cf_step '安装依赖'
printf 'original-output\n'
cf_fact service_mihomo '服务已启动'
cf_complete '安装完成'
`, shell, nil, "PATH="+commands)
			if result.code != 0 {
				t.Fatal(result.stderr)
			}
			contains(t, result.stderr, "无法创建详细日志")
			contains(t, result.stdout, "original-output")
			contains(t, result.stdout, "服务已启动")
			contains(t, result.stdout, "安装完成")
		}
	})
}

func TestMetadataFailureDoesNotAbortInstaller(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		tmp := t.TempDir()
		result := runOutputScript(t, tmp, `set -eu
CF_OUTPUT_DIR="$CF_OUTPUT_LOG"
cf_fact service_mihomo '服务已启动'
printf 'original-output-after-log-failure\n'
cf_complete '安装完成'
`, shell, nil)
		if result.code != 0 {
			t.Fatal(result.stderr)
		}
		contains(t, result.stderr, "日志记录不可用")
		contains(t, result.stdout, "original-output-after-log-failure")
		contains(t, result.stdout, "安装完成")
		logFile(t, tmp)
	})
}

func TestEnvironmentSummaryPreservesPreciseCoreState(t *testing.T) {
	for _, verified := range []bool{false, true} {
		tmp := t.TempDir()
		preflight := writeFile(t, filepath.Join(tmp, "preflight"), `#!/bin/sh
printf '%s\n' 'raw-environment-details' '[提醒] TUN 实际启动：未验证实际接口' '[提醒] IPv4 转发持久化：未知'
`, 0o755)
		fact := ""
		if verified {
			fact = "cf_fact tun_verified yes\n"
		}
		result := runOutputScript(t, tmp, "set -eu\ncf_note forwarding 'IPv4 转发：当前已开启；重启后是否保持尚未确认。'\n"+fact+
			"cf_environment_check \"$TEST_PREFLIGHT\"\ncf_complete '安装完成'\n", shells()[0], nil, "TEST_PREFLIGHT="+preflight)
		if result.code != 0 {
			t.Fatal(result.stderr)
		}
		contains(t, result.stderr, "IPv4 转发：当前已开启")
		notContains(t, result.stdout, "承担网关转发时请检查")
		contains(t, result.stderr, "TUN 实际启动：未验证实际接口")
		notContains(t, result.stdout+result.stderr, "预检未验证核心实际创建接口")
		notContains(t, result.stdout, "raw-environment-details")
		contains(t, readFile(t, logFile(t, tmp)), "raw-environment-details")
	}
}

func TestExternalCheckStatusAndActionableWarningArePreserved(t *testing.T) {
	for _, helper := range []string{"cf_environment_check", "cf_deployment_check"} {
		for _, status := range []int{0, 37} {
			tmp := t.TempDir()
			check := writeFile(t, filepath.Join(tmp, "check"), fmt.Sprintf(`#!/bin/sh
printf '%%s\n' '2026/10/18 Deployment check requires attention: recover-needed' '检查详情及修复建议'
exit %d
`, status), 0o755)
			result := runOutputScript(t, tmp, "set -eu\ncf_step '运行检查'\n"+helper+" \"$TEST_CHECK\"\ncf_complete '安装完成'\n", shells()[0], nil, "TEST_CHECK="+check)
			if result.code != status {
				t.Fatalf("%s %d: exit %d", helper, status, result.code)
			}
			if status != 0 {
				contains(t, result.stderr, "检查详情及修复建议")
				contains(t, result.stderr, "[失败] 运行检查")
				notContains(t, result.stdout, "安装完成")
			} else if helper == "cf_deployment_check" {
				contains(t, result.stderr, "发布记录需要处理：recover-needed")
			}
			contains(t, readFile(t, logFile(t, tmp)), "recover-needed")
		}
	}
}

func TestVerboseOptionReplaysDetailsOnceAndFinalResultIsLast(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		tmp := t.TempDir()
		result := runOutputScript(t, tmp, `set -eu
cf_step '检查环境'
printf 'unique-stdout-detail\n'
printf 'unique-stderr-detail\n' >&2
cf_step_done '检查完成'
cf_fact service_mihomo 'Mihomo：已保留'
cf_note existing '已有配置继续使用'
cf_complete '安装完成'
`, shell, []string{"--verbose"})
		output := result.stdout + result.stderr
		if result.code != 0 {
			t.Fatal(output)
		}
		if strings.Count(output, "unique-stdout-detail") != 1 || strings.Count(output, "unique-stderr-detail") != 1 {
			t.Error("details not replayed exactly once")
		}
		contains(t, result.stderr, "完成 · 有提醒：安装完成")
		log := readFile(t, logFile(t, tmp))
		if strings.Index(log, "Mihomo：已保留") > strings.Index(log, "完成 · 有提醒：安装完成") {
			t.Error("order")
		}
		if !strings.HasSuffix(strings.TrimRight(log, " \t\n"), "完整日志："+logFile(t, tmp)) {
			t.Errorf("log tail %q", log[max(0, len(log)-200):])
		}
		notContains(t, output, "\x1b")
	})
}

func TestAgentSharedLogAndReportFdKeepRealTunResult(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		tmp := t.TempDir()
		preflight := writeFile(t, filepath.Join(tmp, "preflight"), `#!/bin/sh
printf '%s\n' '[通过] TUN 实际启动：Mihomo 核心 PID 123 正持有内核 TUN/TAP 接口 tun0，TUN probe detail: fdinfo and target net/dev verified' >> "$CONFIGFLOW_LOG_FILE"
printf '%s\n' '[通过] TUN tun0 · Mihomo PID 123 已持有' >&3
`, 0o755)
		var args []string
		if verbose {
			args = []string{"--verbose"}
		}
		result := runOutputScript(t, tmp, "set -eu\ncf_step '检查环境'\ncf_environment_check \"$TEST_PREFLIGHT\"\ncf_complete '安装完成'\n", shells()[0], args, "TEST_PREFLIGHT="+preflight)
		output := result.stdout + result.stderr
		if result.code != 0 {
			t.Fatal(output)
		}
		if strings.Count(output, "TUN tun0 · Mihomo PID 123 已持有") != 1 {
			t.Error("TUN line count")
		}
		contains(t, result.stdout, "TUN：tun0 · Mihomo PID 123 已持有")
		if strings.Index(result.stdout, "TUN：tun0 · Mihomo PID 123 已持有") > strings.Index(result.stdout, "[通过] 安装完成") {
			t.Error("order")
		}
		if strings.Contains(output, "TUN probe detail:") != verbose {
			t.Error("verbose detail")
		}
		contains(t, readFile(t, logFile(t, tmp)), "TUN probe detail:")
		notContains(t, output, "预检未验证核心实际创建接口")
	}
}

func TestExternalNestedInstallerReusesLogAndDefersCompletion(t *testing.T) {
	eachShell(t, func(t *testing.T, shell string) {
		tmp := t.TempDir()
		child := writeFile(t, filepath.Join(tmp, "child.sh"), WithInstallOutput(`#!/bin/sh
set -eu
cf_step '子安装阶段'
echo child-raw-detail
cf_fact service_mihomo 'Mihomo：运行中'
cf_complete '子安装完成'
`), 0o644)
		result := runOutputScript(t, tmp, "set -eu\nsh \"$TEST_CHILD\"\ncf_step '父安装后续阶段'\ncf_complete '全部安装完成'\n", shell, []string{"--verbose"}, "TEST_CHILD="+child)
		output := result.stdout + result.stderr
		if result.code != 0 {
			t.Fatal(output)
		}
		for needle, n := range map[string]int{"ConfigFlow ·": 1, "完整日志：": 1, "child-raw-detail": 1} {
			if strings.Count(output, needle) != n {
				t.Errorf("%q count %d", needle, strings.Count(output, needle))
			}
		}
		notContains(t, output, "子安装完成")
		contains(t, output, "Mihomo：运行中")
		contains(t, output, "全部安装完成")
		logFile(t, tmp)
	})
}
