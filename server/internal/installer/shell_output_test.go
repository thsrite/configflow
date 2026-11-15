package installer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Port of backend/test_agent_shell_output.py (cli-output.sh only; coordinator
// decision 2: run the embedded script with sh) and
// backend/test_docker_agent_cli_output.py.

var reportEnvDrop = []string{"CONFIGFLOW_CLI_ACTIVE", "CONFIGFLOW_LOG_CURSOR", "CONFIGFLOW_LOG_FILE", "CONFIGFLOW_LOG_STATE_DIR", "CONFIGFLOW_VERBOSE"}

// cleanEnv is os.environ without the reporting context, plus extra
// (later entries win).
func cleanEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, key := range reportEnvDrop {
			if strings.HasPrefix(kv, key+"=") {
				keep = false
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	return append(env, extra...)
}

func cliModule(t *testing.T, dir string) string {
	return writeFile(t, filepath.Join(dir, "cli-output.sh"), CLIOutputHelpers, 0o644)
}

var fullLogPath = regexp.MustCompile(`完整日志：([^\n]+)`)

func runReport(t *testing.T, tmp, body string, verbose bool, extra ...string) (runResult, string) {
	module := cliModule(t, filepath.Join(tmp, "module"))
	script := writeFile(t, filepath.Join(tmp, "report.sh"), "set -eu\n. \"$CF_TEST_MODULE\"\ncf_verbose_options \"$@\"\ncf_init \"测试任务\"\n"+body, 0o644)
	var args []string
	if verbose {
		args = []string{"--verbose"}
	}
	env := cleanEnv(append([]string{"CF_TEST_MODULE=" + module, "CONFIGFLOW_LOG_DIR=" + filepath.Join(tmp, "logs"), "NO_COLOR=1"}, extra...)...)
	result := runShell(t, "sh", script, runOptions{args: args, env: env, replace: true})
	matches := fullLogPath.FindAllStringSubmatch(result.stdout+result.stderr, -1)
	if len(matches) == 0 {
		return result, ""
	}
	return result, matches[len(matches)-1][1]
}

func TestShellDefaultReportIsCompactAndRetainsPrivateFullLog(t *testing.T) {
	result, logfile := runReport(t, t.TempDir(), `
cf_step "检查环境"
printf 'diagnostic-only-line\n'
cf_success "环境已检查"
cf_warn "TUN 接口尚未创建"
cf_result "Agent 更新完成"
`, false)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	notContains(t, result.stdout+result.stderr, "diagnostic-only-line")
	contains(t, result.stdout, "\n\n[进行] 检查环境")
	contains(t, result.stderr, "完成 · 有提醒：Agent 更新完成")
	notContains(t, result.stdout+result.stderr, "\x1b")
	if logfile == "" {
		t.Fatal("no log")
	}
	contains(t, readFile(t, logfile), "diagnostic-only-line")
	if info, _ := os.Stat(logfile); info.Mode().Perm() != 0o600 {
		t.Error("log mode")
	}
	if !strings.HasSuffix(strings.TrimRight(result.stdout, " \n\t"), "完整日志："+logfile) {
		t.Error("footer not last")
	}
}

func TestShellVerboseReplaysDetailOnceAndPreservesFailureExitCode(t *testing.T) {
	result, logfile := runReport(t, t.TempDir(), `
cf_run "执行检查" sh -c 'echo command-detail; echo specific-error >&2; exit 37'
echo must-not-run
`, true)
	combined := result.stdout + result.stderr
	if result.code != 37 || strings.Count(combined, "command-detail") != 1 || strings.Count(combined, "specific-error") != 1 {
		t.Fatalf("%d\n%s", result.code, combined)
	}
	notContains(t, combined, "must-not-run")
	contains(t, combined, "退出码 37")
	contains(t, readFile(t, logfile), "specific-error")
}

func TestShellFailureExpandsRawCauseWithoutVerbose(t *testing.T) {
	result, logfile := runReport(t, t.TempDir(), `
cf_step "启动 Agent"
printf 'service-specific-failure\n' >&2
exit 42
`, false)
	if result.code != 42 {
		t.Fatal(result.code)
	}
	contains(t, result.stderr, "service-specific-failure")
	contains(t, result.stderr, "退出码 42")
	contains(t, readFile(t, logfile), "service-specific-failure")
}

func TestNestedShellReportSharesLogAndPrintsOneHeaderAndFooter(t *testing.T) {
	tmp := t.TempDir()
	child := writeFile(t, filepath.Join(tmp, "child.sh"), `set -eu
. "$CF_TEST_MODULE"
cf_init "子任务"
cf_step "子任务检查"
echo nested-detail
cf_success "子任务检查完成"
`, 0o644)
	result, logfile := runReport(t, tmp, "sh \"$CF_TEST_CHILD\"\ncf_result \"全部完成\"\n", true, "CF_TEST_CHILD="+child)
	combined := result.stdout + result.stderr
	if result.code != 0 {
		t.Fatal(combined)
	}
	for needle, n := range map[string]int{"ConfigFlow ·": 1, "完整日志：": 1, "nested-detail": 1} {
		if strings.Count(combined, needle) != n {
			t.Errorf("%q count %d", needle, strings.Count(combined, needle))
		}
	}
	contains(t, readFile(t, logfile), "子任务检查")
	logs, _ := filepath.Glob(filepath.Join(tmp, "logs", "configflow-install.*", "install.log"))
	if len(logs) != 1 {
		t.Errorf("logs %v", logs)
	}
}

func TestShellLogCreationFailureFallsBackAndReportsActualPath(t *testing.T) {
	tmp := t.TempDir()
	commands := filepath.Join(tmp, "commands")
	primary, fallback := filepath.Join(tmp, "primary"), filepath.Join(tmp, "fallback")
	os.MkdirAll(fallback, 0o755)
	mktemp, err := exec.LookPath("mktemp")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(commands, "mktemp"), "#!/bin/sh\ncase \"$2\" in \"$CF_TEST_PRIMARY\"/*) exit 1 ;; esac\nexec \""+mktemp+"\" \"$@\"\n", 0o755)
	result, logfile := runReport(t, tmp, "cf_result \"检查完成\"\n", false,
		"PATH="+commands+":"+os.Getenv("PATH"), "CONFIGFLOW_LOG_DIR="+primary, "CF_TEST_PRIMARY="+primary, "TMPDIR="+fallback)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	contains(t, result.stderr, "日志目录不可写，已改用")
	if logfile == "" || filepath.Dir(filepath.Dir(logfile)) != fallback {
		t.Fatalf("log %q", logfile)
	}
	if info, err := os.Stat(logfile); err != nil || !info.Mode().IsRegular() {
		t.Fatal("log missing")
	}
}

func TestShellBinaryDiagnosticDoesNotHideExitStatusOrLogFooter(t *testing.T) {
	tmp := t.TempDir()
	module := cliModule(t, filepath.Join(tmp, "module"))
	script := writeFile(t, filepath.Join(tmp, "binary-output.sh"), `set -eu
. "$CF_TEST_MODULE"
cf_init "诊断输出"
printf '\377raw-byte-detail\n' >&2
exit 19
`, 0o644)
	result := runShell(t, "sh", script, runOptions{replace: true,
		env: cleanEnv("CF_TEST_MODULE="+module, "CONFIGFLOW_LOG_DIR="+filepath.Join(tmp, "logs"))})
	if result.code != 19 {
		t.Fatal(result.code)
	}
	contains(t, result.stderr, "raw-byte-detail")
	notContains(t, result.stderr, "illegal byte sequence")
	contains(t, result.stderr, "退出码 19")
	contains(t, result.stdout, "完整日志：")
}

func TestChildLogDegradationRestoresParentRawCommandOutput(t *testing.T) {
	result, logfile := runReport(t, t.TempDir(), `
( cf_output_degrade )
cf_step "继续父任务"
printf 'parent-raw-after-degradation\n'
printf 'parent-error-after-degradation\n' >&2
cf_result "父任务结束"
`, false)
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	contains(t, result.stdout, "parent-raw-after-degradation")
	contains(t, result.stderr, "parent-error-after-degradation")
	contains(t, result.stderr, "后续输出未完整保存")
	if logfile != "" || strings.Contains(result.stdout+result.stderr, "完整日志：") {
		t.Error("log footer after degradation")
	}
}

func TestDockerRunUsesSameReportingAndPreservesDockerExit(t *testing.T) {
	for _, c := range []struct {
		verbose  bool
		exitCode int
	}{{false, 0}, {true, 0}, {false, 37}} {
		t.Run(fmt.Sprintf("verbose=%v/%d", c.verbose, c.exitCode), func(t *testing.T) {
			tmp := t.TempDir()
			o := NewDockerOptions("https://config.example")
			o.AgentName = "docker-test"
			docker, err := GenerateDockerAgentRun(o)
			if err != nil {
				t.Fatal(err)
			}
			script := writeFile(t, filepath.Join(tmp, "docker-agent.sh"), docker, 0o644)
			commands := filepath.Join(tmp, "bin")
			writeFile(t, filepath.Join(commands, "docker"), fmt.Sprintf("#!/bin/sh\nprintf 'DOCKER_PROGRESS container-id\\n'\nexit %d\n", c.exitCode), 0o755)
			env := cleanEnv("PATH="+commands+":"+os.Getenv("PATH"), "CONFIGFLOW_LOG_DIR="+filepath.Join(tmp, "logs"))
			if syntax := runShell(t, "sh", "-n", runOptions{args: []string{script}}); syntax.code != 0 {
				t.Fatal(syntax.stderr)
			}
			var args []string
			if c.verbose {
				args = []string{"--verbose"}
			}
			result := runShell(t, "sh", script, runOptions{args: args, env: env, replace: true, input: "1\n", timeout: 15e9})
			if result.code != c.exitCode {
				t.Fatalf("exit %d", result.code)
			}
			screen := result.stdout + result.stderr
			if strings.Contains(screen, "DOCKER_PROGRESS") != (c.verbose || c.exitCode != 0) {
				t.Error("progress visibility")
			}
			logs, _ := filepath.Glob(filepath.Join(tmp, "logs", "configflow-install.*", "install.log"))
			if len(logs) != 1 {
				t.Fatalf("logs %v", logs)
			}
			info, _ := os.Stat(logs[0])
			dirInfo, _ := os.Stat(filepath.Dir(logs[0]))
			if info.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
				t.Error("modes")
			}
			contains(t, readFile(t, logs[0]), "DOCKER_PROGRESS")
			contains(t, screen, logs[0])
		})
	}
}
