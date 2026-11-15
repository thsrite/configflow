package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Harnesses of backend/test_single_agent_core_install.py and
// backend/test_aio_agent_core_install.py (imported by several installer
// tests), and their cases that do not go through the install-script route.
// The route cases (Flask client fixture) are A10's.

// env is a subprocess environment; later entries win, as with exec.Cmd.
type env []string

func (e env) with(kv ...string) env { return append(append(env{}, e...), kv...) }

func (e env) get(key string) string {
	for i := len(e) - 1; i >= 0; i-- {
		if k, v, _ := strings.Cut(e[i], "="); k == key {
			return v
		}
	}
	return ""
}

func (e env) without(keys ...string) env {
	var out env
	for _, kv := range e {
		k, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, key := range keys {
			drop = drop || k == key
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

func baseEnv() env { return env(os.Environ()) }

// withFakeCore replaces the embedded core phase body of a rendered installer
// with fake: the effect of monkeypatching generate_core_install_script.
func withFakeCore(t *testing.T, script, fake string) string {
	t.Helper()
	for _, forwarding := range []bool{false, true} {
		core := pyRstrip(ok(GenerateCoreInstallScript(forwarding)))
		if strings.Count(script, core) == 1 {
			return strings.Replace(script, core, pyRstrip(fake), 1)
		}
	}
	t.Fatal("no embedded core phase")
	return ""
}

func replaceAll(source string, pairs [][2]string) string {
	for _, p := range pairs {
		source = strings.ReplaceAll(source, p[0], p[1])
	}
	return source
}

func darwinSed(source string) string {
	if runtime.GOOS == "darwin" {
		return strings.ReplaceAll(source, "sed -i ", "sed -i '' ")
	}
	return source
}

func writeCommands(t *testing.T, dir string, mocks map[string]string, prefix string) {
	for name, body := range mocks {
		if !strings.HasPrefix(body, "#!") {
			body = prefix + body
		}
		writeFile(t, filepath.Join(dir, name), body, 0o755)
	}
}

func runInstaller(t *testing.T, script string, e env, choice string, args ...string) runResult {
	return runShell(t, "/bin/sh", script, runOptions{args: args, env: e, replace: true, input: choice + "\n", timeout: 60e9})
}

func eventsText(t *testing.T, path string) string {
	data, _ := os.ReadFile(path)
	return string(data)
}

// -- test_single_agent_core_install.py ------------------------------------

const singleFakeCore = `#!/bin/sh
set -eu
printf 'core %s\n' "$*" >> "$EVENTS"
[ "$1" = --core ]
[ "$2" = "$TEST_CORE" ]
if [ "${FAIL_CORE:-}" = yes ]; then false; fi
printf 'complete\n' > "$CORE_COMPLETED"
# The Agent must not inherit core variables, traps or an explicit exit.
AGENT_DIR=/must-not-leak
INIT_SYSTEM=must-not-leak
trap 'printf "core cleanup\n" >> "$EVENTS"' 0
exit 0
`

func singleGenerated(service string, mutate func(*GoAgentOptions)) (string, error) {
	o := GoAgentOptions{ServerURL: "https://config.test", AgentName: "single", ServiceType: service,
		BinaryDownloadURL: "https://config.test/api/agents/download"}
	if mutate != nil {
		mutate(&o)
	}
	return GenerateGoAgentInstallScript(o)
}

type singleHarness struct {
	tmp, script, root, events string
	env                       env
}

func (h *singleHarness) run(t *testing.T, choice string, args ...string) runResult {
	return runInstaller(t, h.script, h.env, choice, args...)
}

func (h *singleHarness) eventsText(t *testing.T) string { return eventsText(t, h.events) }

type singleSetup struct {
	service, manager string
	realCore         bool
	installCore      bool
	mutate           func(*GoAgentOptions) // the monkeypatched single.generated
	settings         []string
}

// newSingleHarness is harness(tmp_path, monkeypatch, service, manager, real_core, install_core, **settings).
func newSingleHarness(t *testing.T, s singleSetup) *singleHarness {
	if s.service == "" {
		s.service = "mihomo"
	}
	if s.manager == "" {
		s.manager = "systemd"
	}
	tmp := t.TempDir()
	python := pythonExe(t)
	source := ok(singleGenerated(s.service, func(o *GoAgentOptions) {
		o.InstallCore = s.installCore
		if s.mutate != nil {
			s.mutate(o)
		}
	}))
	if !s.realCore && strings.Contains(source, "set -- --core ") {
		source = withFakeCore(t, source, singleFakeCore)
	}
	root := filepath.Join(tmp, "root")
	for _, rel := range []string{"etc/systemd/system", "etc/init.d", "usr/local/bin", "sbin", "run", "opt", "var/log"} {
		os.MkdirAll(filepath.Join(root, rel), 0o755)
	}
	if s.manager == "systemd" {
		os.MkdirAll(filepath.Join(root, "run/systemd/system"), 0o755)
	} else {
		writeFile(t, filepath.Join(root, "sbin/openrc-run"), "#!/bin/sh\n", 0o755)
	}
	source = replaceAll(source, [][2]string{
		{"/usr/local/", root + "/usr/local/"},
		{"/usr/lib/", root + "/usr/lib/"},
		{"/lib/systemd/", root + "/lib/systemd/"},
		{"/etc/", root + "/etc/"},
		{"/run/", root + "/run/"},
		{"/sbin/openrc-run", root + "/sbin/openrc-run"},
		{"/opt/configflow-agent", root + "/opt/configflow-agent"},
		{"/var/log", root + "/var/log"},
		{"/dev/tty", "/dev/stdin"},
		{"/proc/[0-9]*/status", filepath.Join(tmp, "agent-process")},
	})
	writeFile(t, filepath.Join(tmp, "agent-process"), "PPid:\tmissing\n", 0o644)
	source = strings.ReplaceAll(source, "in /run /etc /usr/local ", "in "+root+"/run "+root+"/etc "+root+"/usr/local ")
	source = darwinSed(source)
	script := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
	writeFile(t, filepath.Join(root, "etc", s.service, "config.yaml"), "# existing core config\n", 0o644)
	writeFile(t, filepath.Join(root, "usr/local/bin", s.service), "#!/bin/sh\nexit 0\n", 0o755)
	unit := filepath.Join(root, "etc/init.d", s.service)
	if s.manager == "systemd" {
		unit = filepath.Join(root, "etc/systemd/system", s.service+".service")
	}
	writeFile(t, unit, "# existing core service\n", 0o755)
	commands := filepath.Join(tmp, "commands")
	agent := writeFile(t, filepath.Join(tmp, "fake-agent"), "#!"+python+`
import json, os, sys
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('agent ' + ' '.join(args) + '\n')
if args in (['-h'], ['-version']):
    raise SystemExit(0)
cfg = json.load(open(args[args.index('-config') + 1]))
assert cfg['service_manager'] == os.environ['TEST_MANAGER']
if '-check-environment' in args and os.environ.get('VERBOSE_ENVIRONMENT') == 'yes':
    print('verbose-environment-detail-for-log-only')
if '-check-deployments' in args and os.environ.get('PENDING_DEPLOYMENT') == 'yes':
    print('2026/10/18 12:00:00 Deployment check requires attention: pending publication; inspect its journal', file=sys.stderr)
`, 0o755)
	managerBody := `printf '%s %s\n' "$(basename "$0")" "$*" >> "$EVENTS"
case "$*" in *--property=LoadState*) printf '%s\n' "${CORE_LOAD_STATE:-loaded}" ;; esac
`
	mocks := map[string]string{
		"id":    "printf '0\\n'\n",
		"uname": `case "$1" in -s) echo Linux ;; *) echo "${TEST_ARCH:-x86_64}" ;; esac` + "\n",
		"wget":  "echo agent-download >> \"$EVENTS\"\n[ \"${FAIL_AGENT_DOWNLOAD:-}\" != yes ] || exit 47\ncp \"$FAKE_AGENT\" \"$3\"\n",
		"systemctl": managerBody, "rc-service": managerBody, "rc-update": managerBody,
	}
	if !s.installCore && s.manager == "systemd" {
		delete(mocks, "rc-service")
		delete(mocks, "rc-update")
	}
	writeCommands(t, commands, mocks, "#!/bin/sh\nset -eu\n")
	events := writeFile(t, filepath.Join(tmp, "events"), "", 0o644)
	e := baseEnv().with("PATH="+commands+":"+root+"/usr/local/bin:"+os.Getenv("PATH"),
		"EVENTS="+events, "FAKE_AGENT="+agent, "TEST_CORE="+s.service, "TEST_MANAGER="+s.manager,
		"CORE_COMPLETED="+filepath.Join(tmp, "core-completed"), "CONFIGFLOW_INSTALL_LOG_DIR="+tmp).with(s.settings...)
	return &singleHarness{tmp: tmp, script: script, root: root, events: events, env: e}
}

func TestDefaultLayoutAliasesAreAccepted(t *testing.T) {
	// install_core="false" (a str) is a Python type error ruled out by Go's bool.
	for _, service := range []string{"mihomo", "oxidns"} {
		source := ok(singleGenerated(service, func(o *GoAgentOptions) {
			o.InstallCore = true
			o.ConfigPath = "/etc/" + service + "/config.yaml"
			o.ServiceUnit = service + ".service"
			o.ServiceBinary = "/usr/local/bin/" + service
			o.RestartCommand = "rc-service " + service + " restart"
		}))
		if strings.Split(source, "\n")[1] != AgentCoreInstallInterface {
			t.Errorf("%s: no core interface marker", service)
		}
	}
}

func TestCoreCompletesBeforeAgentWithIsolatedState(t *testing.T) {
	for _, service := range []string{"mihomo", "oxidns"} {
		managers(t, func(t *testing.T, manager string) {
			h := newSingleHarness(t, singleSetup{service: service, manager: manager, installCore: true})
			r := h.run(t, "1")
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			log := h.eventsText(t)
			a, b, c := strings.Index(log, "core --core "+service), strings.Index(log, "core cleanup"), strings.Index(log, "agent-download")
			if a < 0 || !(a < b && b < c) {
				t.Errorf("order: %s", log)
			}
			if !exists(filepath.Join(h.root, "opt/configflow-agent/config-"+service+".json")) || globAny(filepath.Join(h.root, "usr/local/bin/.configflow-agent.install.*")) {
				t.Error("files")
			}
			unexpected := "systemctl"
			if manager == "systemd" {
				unexpected = "rc-service"
			}
			notContains(t, log, unexpected)
		})
	}
}

func TestSingleAgentOnlyPrintsProgressAndServiceSummary(t *testing.T) {
	for service, label := range map[string]string{"mihomo": "Mihomo", "oxidns": "OxiDNS", "mosdns": "MosDNS"} {
		h := newSingleHarness(t, singleSetup{service: service, settings: []string{"VERBOSE_ENVIRONMENT=yes"}})
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		for _, needle := range []string{"请输入选项 [1-3]: ", "检查安装环境与依赖", "下载并校验 Agent", "检查核心接管与运行环境",
			"安装并启动 Agent 服务", label + " Agent：端口 8080，服务已启动", "完整日志："} {
			contains(t, r.stdout, needle)
		}
		contains(t, r.stdout+r.stderr, "安装完成")
		notContains(t, r.stdout+r.stderr, "verbose-environment-detail-for-log-only")
		contains(t, detailOutput(t, r), "verbose-environment-detail-for-log-only")
		notContains(t, h.eventsText(t), "core --core")
	}
}

func TestSingleAgentPendingPublicationRemainsVisibleInSuccessSummary(t *testing.T) {
	for _, existing := range []bool{false, true} {
		h := newSingleHarness(t, singleSetup{})
		if existing {
			if r := h.run(t, "1"); r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			writeFile(t, h.events, "", 0o644)
		}
		h.env = h.env.with("PENDING_DEPLOYMENT=yes")
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		contains(t, r.stdout+r.stderr, "安装完成")
		if strings.Count(r.stderr, "发布记录需要处理：pending publication; inspect its journal") != 1 {
			t.Errorf("stderr: %s", r.stderr)
		}
		notContains(t, r.stdout+r.stderr, "Deployment check requires attention:")
		details := detailOutput(t, r)
		want := 1
		if existing {
			want = 2
		}
		if strings.Count(details, "Deployment check requires attention:") != want || strings.Count(h.eventsText(t), "-check-deployments") != want {
			t.Errorf("existing=%v: checks", existing)
		}
		contains(t, details, "inspect its journal")
	}
}

func TestSingleUninstallOrCancelNeverRunsCore(t *testing.T) {
	for _, choice := range []string{"2", "3"} {
		h := newSingleHarness(t, singleSetup{installCore: true})
		config := filepath.Join(h.root, "etc/mihomo/config.yaml")
		original := readFile(t, config)
		r := h.run(t, choice)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		notContains(t, h.eventsText(t), "core --core")
		notContains(t, h.eventsText(t), "agent-download")
		if readFile(t, config) != original {
			t.Error("config changed")
		}
	}
}

func TestSingleCoreFailureStopsBeforeAgentPhase(t *testing.T) {
	h := newSingleHarness(t, singleSetup{installCore: true, settings: []string{"FAIL_CORE=yes"}})
	if r := h.run(t, "1"); r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, h.eventsText(t), "core --core mihomo")
	notContains(t, h.eventsText(t), "agent-download")
	if exists(h.env.get("CORE_COMPLETED")) || exists(filepath.Join(h.root, "opt/configflow-agent")) {
		t.Error("side effects")
	}
}

func TestLaterAgentFailurePreservesCompletedCoreAndReportsIt(t *testing.T) {
	h := newSingleHarness(t, singleSetup{installCore: true, settings: []string{"FAIL_AGENT_DOWNLOAD=yes"}})
	r := h.run(t, "1")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	if readFile(t, h.env.get("CORE_COMPLETED")) != "complete\n" {
		t.Error("core not completed")
	}
	contains(t, r.stderr, "核心予以保留")
	contains(t, r.stderr, "Agent 二进制下载失败")
	if globAny(filepath.Join(h.root, "usr/local/bin/.configflow-agent.install.*")) {
		t.Error("staged binary left")
	}
	notContains(t, h.eventsText(t), "stop mihomo")
}

func TestSingleUnsupportedArchitectureNeverRunsCore(t *testing.T) {
	h := newSingleHarness(t, singleSetup{installCore: true, settings: []string{"TEST_ARCH=unknown"}})
	if r := h.run(t, "1"); r.code == 0 {
		t.Fatal("succeeded")
	}
	if h.eventsText(t) != "" || exists(filepath.Join(h.root, "opt/configflow-agent")) {
		t.Error("side effects")
	}
}

func TestIncompleteExistingCoreStopsBeforeAgentFiles(t *testing.T) {
	for _, missing := range []string{"config", "binary", "service"} {
		h := newSingleHarness(t, singleSetup{installCore: true})
		switch missing {
		case "config":
			os.Remove(filepath.Join(h.root, "etc/mihomo/config.yaml"))
		case "binary":
			os.Remove(filepath.Join(h.root, "usr/local/bin/mihomo"))
		default:
			h.env = h.env.with("CORE_LOAD_STATE=not-found")
		}
		r := h.run(t, "1")
		if r.code == 0 {
			t.Fatalf("%s: succeeded", missing)
		}
		contains(t, r.stderr, "取消同时安装核心")
		notContains(t, h.eventsText(t), "agent-download")
		if exists(filepath.Join(h.root, "opt/configflow-agent")) {
			t.Error("agent dir created")
		}
	}
}

func TestMissingAgentToolHasRepairHintAndCannotDeleteInheritedTemp(t *testing.T) {
	h := newSingleHarness(t, singleSetup{installCore: true})
	inherited := writeFile(t, filepath.Join(h.tmp, "unrelated-existing-file"), "keep me", 0o644)
	h.env = h.env.with("BINARY_TMP="+inherited, "PATH="+h.tmp+"/commands:"+h.root+"/usr/local/bin")
	r := h.run(t, "1")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stderr, "Agent 安装缺少工具：awk")
	contains(t, r.stderr, "未找到支持的包管理器")
	contains(t, r.stderr, "请手动安装 gawk coreutils grep sed 后重新运行")
	notContains(t, h.eventsText(t), "agent-download")
	if readFile(t, inherited) != "keep me" {
		t.Error("inherited temp changed")
	}
}

func TestExistingCoreReusesRealSkipLogicWithoutModification(t *testing.T) {
	h := newSingleHarness(t, singleSetup{installCore: true, realCore: true})
	paths := []string{filepath.Join(h.root, "etc/mihomo/config.yaml"), filepath.Join(h.root, "usr/local/bin/mihomo"),
		filepath.Join(h.root, "etc/systemd/system/mihomo.service")}
	originals := map[string]string{}
	for _, p := range paths {
		originals[p] = readFile(t, p)
	}
	r := h.run(t, "1")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	contains(t, r.stdout, "Mihomo：已有部署，已保留")
	contains(t, detailOutput(t, r), "跳过已有 mihomo")
	sameFiles(t, originals)
	log := h.eventsText(t)
	contains(t, log, "agent-download")
	for _, line := range strings.Split(log, "\n") {
		if strings.HasSuffix(line, " mihomo") {
			t.Errorf("core touched: %s", line)
		}
	}
}

// -- test_aio_agent_core_install.py ---------------------------------------

const aioFakeCore = `#!/bin/sh
set -eu
printf 'core %s\n' "$*" >> "$EVENTS"
[ "$1" = --core ]
[ "$2" = both ]
if [ "${FAIL_CORE:-}" = yes ]; then false; fi
printf 'complete\n' > "$CORE_COMPLETED"
# The outer Agent phase must not inherit core variables or traps.
AGENT_ROOT=/must-not-leak
INIT_SYSTEM=must-not-leak
STAGE_DIR=/must-not-delete
trap 'printf "core cleanup\n" >> "$EVENTS"' 0
exit 0
`

// newAIOCoreHarness is test_aio_agent_core_install.harness(tmp_path, monkeypatch, manager, existing, real_core).
func newAIOCoreHarness(t *testing.T, manager string, existing, realCore bool) aioHarness {
	source := ok(aioGenerated(t, func(o *AIOOptions) { o.InstallCore = true }))
	if !realCore {
		source = withFakeCore(t, source, aioFakeCore)
	}
	h := newAIOHarnessSource(t, manager, existing, source)
	tmp := h.tmp
	text := replaceAll(readFile(t, h.installer), [][2]string{
		{"/run/", tmp + "/run/"},
		{"/sbin/openrc-run", tmp + "/sbin/openrc-run"},
		{"/usr/lib/", tmp + "/usr/lib/"},
		{"/lib/systemd/", tmp + "/lib/systemd/"},
	})
	text = strings.ReplaceAll(text, "in /run /etc /usr/local ", "in "+tmp+"/run "+tmp+"/etc "+tmp+"/usr-local ")
	writeFile(t, h.installer, text, 0o644)
	for _, dir := range []string{"run", "usr-local", "local-bin", "sbin"} {
		os.MkdirAll(filepath.Join(tmp, dir), 0o755)
	}
	if manager == "systemd" {
		os.MkdirAll(filepath.Join(tmp, "run/systemd/system"), 0o755)
	} else {
		writeFile(t, filepath.Join(tmp, "sbin/openrc-run"), "#!/bin/sh\n", 0o755)
	}
	commands := filepath.Join(tmp, "commands")
	managerMock := `#!/bin/sh
set -eu
printf '%s %s\n' "$(basename "$0")" "$*" >> "$EVENTS"
case "$*" in *--property=LoadState*) printf '%s\n' "${CORE_LOAD_STATE:-loaded}" ;; esac
`
	for _, name := range []string{"systemctl", "rc-service", "rc-update"} {
		writeFile(t, filepath.Join(commands, name), managerMock, 0o755)
	}
	writeFile(t, filepath.Join(commands, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo Linux ;; *) echo \"${TEST_ARCH:-x86_64}\" ;; esac\n", 0o755)
	writeFile(t, filepath.Join(commands, "wget"), "#!/bin/sh\nset -eu\necho agent-download >> \"$EVENTS\"\n[ \"${FAIL_AGENT_DOWNLOAD:-}\" != yes ] || exit 49\ncp \"$FAKE_AGENT\" \"$3\"\n", 0o755)
	writeFile(t, filepath.Join(tmp, "events"), "", 0o644)
	h.env = append(h.env, "CORE_COMPLETED="+filepath.Join(tmp, "core-completed"))
	return h
}

func TestDefaultUnitAndBinaryAliasesAreAccepted(t *testing.T) {
	// install_core="false" (a str) is a Python type error ruled out by Go's bool.
	o := NewAIOOptions("https://config.test", "gateway")
	o.InstallCore = true
	o.MihomoServiceUnit, o.OxidnsServiceUnit = "mihomo.service", "oxidns.service"
	o.MihomoServiceBinary, o.OxidnsServiceBinary = "/usr/local/bin/mihomo", "/usr/local/bin/oxidns"
	if strings.Split(ok(GenerateGoAIOInstallScript(o)), "\n")[1] != AgentCoreInstallInterface {
		t.Fatal("no core interface marker")
	}
}

func fileSnapshot(t *testing.T, dir string) map[string]string { return snapshotFiles(t, dir) }

func TestAIOCorePhasePrecedesAgentWithIsolatedStateAndIdentity(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, existing := range []bool{true, false} {
			h := newAIOCoreHarness(t, manager, existing, false)
			r := h.run(t, "1")
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			log := h.eventsText()
			a, b, c := strings.Index(log, "core --core both"), strings.Index(log, "core cleanup"), strings.Index(log, "agent-download")
			if a < 0 || !(a < b && b < c) {
				t.Errorf("order: %s", log)
			}
			unexpected := "systemctl"
			if manager == "systemd" {
				unexpected = "rc-service"
			}
			notContains(t, log, unexpected)
			for _, service := range []string{"mihomo", "oxidns"} {
				var cfg map[string]any
				json.Unmarshal([]byte(readFile(t, h.path("agent/"+service+"/config.json"))), &cfg)
				wantID, wantToken := "", ""
				if existing {
					wantID, wantToken = "id-"+service, "token-"+service
				}
				if cfg["service_manager"] != manager || cfg["agent_id"] != wantID || cfg["token"] != wantToken {
					t.Errorf("%s: %v", service, cfg)
				}
				if readFile(t, h.path("etc/"+service+"/config.yaml")) != "# unchanged core config\n" {
					t.Error("core config changed")
				}
			}
			if globAny(h.path("agent/.aio-install.*")) {
				t.Error("stage left")
			}
		}
	})
}

func TestNewCombinedInstallDoesNotRepeatDNSSetupInstructions(t *testing.T) {
	h := newAIOCoreHarness(t, "systemd", false, false)
	marker := "# The outer Agent phase must not inherit core variables or traps."
	writeFile(t, h.installer, strings.ReplaceAll(readFile(t, h.installer), marker,
		"cf_fact core_mihomo_new yes\ncf_fact core_oxidns_new yes\n"+marker), 0o644)
	r := h.run(t, "1")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	contains(t, r.stdout, "Mihomo Agent：端口 8080，服务已启动")
	contains(t, r.stdout, "OxiDNS Agent：端口 8082，服务已启动")
	for _, needle := range []string{"请核对 DNS 链路", "将 DNS 调整到 1053", "随后发布 OxiDNS 配置"} {
		notContains(t, r.stdout, needle)
	}
}

func TestAIOUninstallAndCancelSkipCorePhase(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, choice := range []string{"2", "3"} {
			h := newAIOCoreHarness(t, manager, true, false)
			originals := map[string]string{}
			entries, _ := os.ReadDir(h.units)
			for _, entry := range entries {
				if !strings.HasPrefix(entry.Name(), "configflow-agent") && entry.Type().IsRegular() {
					originals[filepath.Join(h.units, entry.Name())] = readFile(t, filepath.Join(h.units, entry.Name()))
				}
			}
			r := h.run(t, choice)
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			log := h.eventsText()
			notContains(t, log, "core --core")
			notContains(t, log, "agent-download")
			sameFiles(t, originals)
		}
	})
}

func TestAIOCoreFailureStopsBeforeAgentChanges(t *testing.T) {
	h := newAIOCoreHarness(t, "systemd", true, false)
	originals := fileSnapshot(t, h.path("agent"))
	r := h.run(t, "1", "FAIL_CORE=yes")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	if h.eventsText() != "core --core both\n" {
		t.Errorf("events: %q", h.eventsText())
	}
	if exists(h.path("core-completed")) {
		t.Error("core completed")
	}
	notContains(t, r.stderr, "核心予以保留")
	sameFiles(t, originals)
}

func TestAgentFailureKeepsCompletedCorePhase(t *testing.T) {
	for _, failure := range []string{"FAIL_AGENT_DOWNLOAD", "FAIL_ENVIRONMENT"} {
		h := newAIOCoreHarness(t, "systemd", true, false)
		originals := fileSnapshot(t, h.path("agent"))
		r := h.run(t, "1", failure+"=yes")
		if r.code == 0 {
			t.Fatalf("%s: succeeded", failure)
		}
		if readFile(t, h.path("core-completed")) != "complete\n" {
			t.Error("core not completed")
		}
		contains(t, r.stderr, "核心予以保留")
		notContains(t, h.eventsText(), "stop")
		if globAny(h.path("agent/.aio-install.*")) {
			t.Error("stage left")
		}
		sameFiles(t, originals)
	}
}

func TestLateAgentFailureRestartsPreviousAgentsWithoutChangingCores(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOCoreHarness(t, manager, true, false)
		originals := fileSnapshot(t, h.path("agent"))
		r := h.run(t, "1", "FAIL_JOURNAL_AFTER_STOP=oxidns")
		if r.code != 47 {
			t.Fatalf("exit %d: %s", r.code, r.stderr)
		}
		contains(t, r.stderr, "核心予以保留")
		log := h.events(t)
		for _, service := range []string{"mihomo", "oxidns"} {
			stop := indexOf(log, func(l string) bool { return l == agentEvent(manager, service, "stop") })
			start := indexOf(log, func(l string) bool { return l == agentEvent(manager, service, "start") })
			if stop < 0 || start < 0 || stop > start {
				t.Errorf("%s: stop %d start %d", service, stop, start)
			}
			if readFile(t, h.path("etc/"+service+"/config.yaml")) != "# unchanged core config\n" {
				t.Error("core config changed")
			}
		}
		sameFiles(t, originals)
	})
}

func TestAIOUnsupportedArchitectureStopsBeforeCore(t *testing.T) {
	h := newAIOCoreHarness(t, "systemd", false, false)
	r := h.run(t, "1", "TEST_ARCH=unknown")
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	if h.eventsText() != "" || exists(h.path("agent")) {
		t.Errorf("side effects: %q", h.eventsText())
	}
}

func TestRealExistingCoreSkipLogicPreservesBothCores(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOCoreHarness(t, manager, true, true)
		suffix := ""
		if manager == "systemd" {
			suffix = ".service"
		}
		originals := map[string]string{}
		for _, p := range []string{h.path("etc/mihomo/config.yaml"), h.path("etc/oxidns/config.yaml"), h.path("commands/mihomo"),
			h.path("commands/oxidns"), filepath.Join(h.units, "mihomo"+suffix), filepath.Join(h.units, "oxidns"+suffix)} {
			originals[p] = readFile(t, p)
		}
		r := h.run(t, "1")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		contains(t, r.stdout, "Mihomo：已有部署，已保留")
		contains(t, r.stdout, "OxiDNS：已有部署，已保留")
		details := detailOutput(t, r)
		contains(t, details, "跳过已有 mihomo")
		contains(t, details, "跳过已有 oxidns")
		sameFiles(t, originals)
		log := h.events(t)
		if !anyLine(log, func(l string) bool { return l == "agent-download" }) {
			t.Error("no agent download")
		}
		for _, service := range []string{"mihomo", "oxidns"} {
			if anyLine(log, func(l string) bool { return l == "systemctl stop "+service || l == "rc-service "+service+" stop" }) {
				t.Errorf("%s stopped", service)
			}
		}
	})
}

func TestNewCoreIsReadyBeforeAgentCoreChecks(t *testing.T) {
	h := newAIOCoreHarness(t, "systemd", false, false)
	config, binary, unit := h.path("etc/oxidns/config.yaml"), h.path("commands/oxidns"), filepath.Join(h.units, "oxidns.service")
	for _, p := range []string{config, binary, unit} {
		os.Remove(p)
	}
	originalMihomo := readFile(t, h.path("etc/mihomo/config.yaml"))
	h.env = append(h.env, "NEW_CONFIG="+config, "NEW_BINARY="+binary, "NEW_UNIT="+unit)
	writeFile(t, h.installer, strings.Replace(readFile(t, h.installer), `printf 'complete\n' > "$CORE_COMPLETED"`,
		`printf '# newly installed peer\n' > "$NEW_CONFIG"`+"\n"+`printf '#!/bin/sh\nexit 0\n' > "$NEW_BINARY"`+"\nchmod 755 \"$NEW_BINARY\"\n"+
			`printf '# newly installed service\n' > "$NEW_UNIT"`+"\n"+`printf 'complete\n' > "$CORE_COMPLETED"`, 1), 0o644)
	r := h.run(t, "1")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if readFile(t, config) != "# newly installed peer\n" || readFile(t, h.path("etc/mihomo/config.yaml")) != originalMihomo ||
		!exists(h.path("agent/oxidns/config.json")) {
		t.Error("new core not used")
	}
}

func TestMissingAgentParentIsCreatedAfterCoreCompletion(t *testing.T) {
	h := newAIOCoreHarness(t, "systemd", false, false)
	parent := h.path("new-opt")
	writeFile(t, h.installer, strings.ReplaceAll(readFile(t, h.installer), h.path("agent"), filepath.Join(parent, "configflow-agent")), 0o644)
	r := h.run(t, "1")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if !exists(h.path("core-completed")) || !exists(filepath.Join(parent, "configflow-agent/mihomo/config.json")) ||
		!exists(filepath.Join(parent, "configflow-agent/oxidns/config.json")) {
		t.Error("files missing")
	}
}
