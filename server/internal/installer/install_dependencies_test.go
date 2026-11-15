package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_agent_install_dependencies.py: dependency setup runs
// inside native installers before touching deployments. No host package
// manager is reachable: PATH holds only the fixture commands.

var dependencyTools = strings.Fields("awk basename cat chmod cmp cp dirname grep head ln mkdir mktemp mv readlink rm sed wc")

var forwardingTools = []string{"sysctl", "cmp", "ln"}

type forwardingManager struct {
	name          string
	prefix        []string
	sysctlPackage string
}

var forwardingManagers = []forwardingManager{
	{"apt-get", []string{"install", "-y", "--no-install-recommends"}, "procps"},
	{"apk", []string{"add", "--no-cache"}, "procps"},
	{"pacman", []string{"-S", "--needed", "--noconfirm"}, "procps-ng"},
}

// isolateDependencies is isolate_dependencies(tmp_path, env, missing,
// package_manager, tzdata); it returns the updated env and the package log.
func isolateDependencies(t *testing.T, tmp string, e env, missing []string, packageManager string, tzdata bool) (env, string) {
	commands := filepath.Join(tmp, "commands")
	os.MkdirAll(commands, 0o755)
	unavailable := filepath.Join(tmp, "unavailable")
	if err := os.Mkdir(unavailable, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range append(append([]string{}, dependencyTools...), "sh") {
		if !exists(filepath.Join(commands, tool)) {
			real, err := exec.LookPath(tool)
			if err != nil {
				t.Fatal(err)
			}
			os.Symlink(real, filepath.Join(commands, tool))
		}
	}
	for _, tool := range []string{"wget", "sleep"} {
		if !exists(filepath.Join(commands, tool)) {
			writeFile(t, filepath.Join(commands, tool), "#!/bin/sh\nexit 0\n", 0o755)
		}
	}
	if !exists(filepath.Join(commands, "sysctl")) {
		writeFile(t, filepath.Join(commands, "sysctl"), "#!/bin/sh\nprintf 'unexpected sysctl execution\\n' >&2\nexit 97\n", 0o755)
	}
	for _, tool := range missing {
		if err := os.Rename(filepath.Join(commands, tool), filepath.Join(unavailable, tool)); err != nil {
			t.Fatal(err)
		}
	}
	if packageManager != "" {
		writeFile(t, filepath.Join(commands, packageManager), "#!"+pythonExe(t)+` -S
import json, os, sys
from pathlib import Path
with open(os.environ['PACKAGE_EVENTS'], 'a') as log:
    log.write(json.dumps([Path(sys.argv[0]).name, sys.argv[1:], os.environ.get('DEBIAN_FRONTEND')]) + '\n')
updating = sys.argv[1:] == ['update']
if os.environ.get('EVENTS'):
    with open(os.environ['EVENTS'], 'a') as log:
        log.write(('package-update' if updating else 'package-install') + '\n')
if os.environ.get('FAIL_PACKAGE') == ('update' if updating else 'install'): raise SystemExit(41)
if updating or os.environ.get('PACKAGE_NO_COMMANDS'): raise SystemExit(0)
if 'tzdata' in sys.argv or 'timezone' in sys.argv:
    zone = Path(os.environ['TZDIR']) / 'Asia/Shanghai'
    zone.parent.mkdir(parents=True, exist_ok=True)
    zone.write_text('TZif')
for executable in Path(os.environ['TEST_UNAVAILABLE']).iterdir():
    packages = {'awk': ['gawk'], 'grep': ['grep'], 'sed': ['sed'], 'wget': ['curl'],
                'sysctl': ['procps-ng' if Path(sys.argv[0]).name in ('pacman', 'dnf', 'yum') else 'procps'],
                'cmp': ['diffutils'], 'nft': ['nftables'], 'ss': ['iproute2', 'iproute'],
                'tar': ['tar'], 'gzip': ['gzip']}.get(executable.name, ['coreutils'])
    if any(package in sys.argv[1:] for package in packages):
        executable.rename(Path(os.environ['TEST_COMMANDS']) / executable.name)
`, 0o755)
	}
	events := writeFile(t, filepath.Join(tmp, "packages"), "", 0o644)
	zoneinfo := filepath.Join(tmp, "zoneinfo")
	if tzdata {
		writeFile(t, filepath.Join(zoneinfo, "Asia/Shanghai"), "TZif", 0o644)
	}
	coreBins := filepath.Join(tmp, "root/usr/local/bin")
	return e.without("DEBIAN_FRONTEND").with("PATH="+commands+":"+coreBins, "TEST_COMMANDS="+commands,
		"TEST_UNAVAILABLE="+unavailable, "PACKAGE_EVENTS="+events, "TZDIR="+zoneinfo), events
}

type dependencyHarness struct {
	tmp, script, events string
	env                 env
}

func (h *dependencyHarness) run(t *testing.T) runResult {
	return runInstaller(t, h.script, h.env, "1")
}

func (h *dependencyHarness) transactions(t *testing.T) []string { return lines(readFile(t, h.events)) }

type dependencySetup struct {
	missing  []string
	manager  string // "" = apk; "-" = none
	required []string
	noTZ     bool
	options  []string
}

// newDependencyHarness is dependency_harness(tmp_path, missing, manager, required, tzdata, **options).
func newDependencyHarness(t *testing.T, s dependencySetup, prepare func(tmp string)) *dependencyHarness {
	tmp := t.TempDir()
	if prepare != nil {
		prepare(tmp)
	}
	manager := s.manager
	if manager == "" {
		manager = "apk"
	} else if manager == "-" {
		manager = ""
	}
	quoted := make([]string, len(s.required))
	for i, tool := range s.required {
		quoted[i] = shlexQuote(tool)
	}
	script := writeFile(t, filepath.Join(tmp, "dependencies.sh"), "set -eu\n"+AgentInstallDependencies+
		"\nagent_ensure_dependencies "+strings.Join(quoted, " ")+"\nprintf 'ready\\n'\n", 0o644)
	e, events := isolateDependencies(t, tmp, baseEnv().with(s.options...), s.missing, manager, !s.noTZ)
	return &dependencyHarness{tmp: tmp, script: script, events: events, env: e}
}

func decodeEvent(t *testing.T, line string) (string, []string, any) {
	var raw []json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil || len(raw) != 3 {
		t.Fatalf("event %q", line)
	}
	var name string
	var args []string
	var frontend any
	json.Unmarshal(raw[0], &name)
	json.Unmarshal(raw[1], &args)
	json.Unmarshal(raw[2], &frontend)
	return name, args, frontend
}

func frontendFor(manager string) any {
	if manager == "apt-get" {
		return "noninteractive"
	}
	return nil
}

func eventsEqual(t *testing.T, got []string, want [][3]any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("transactions %v want %v", got, want)
	}
	for i, line := range got {
		name, args, frontend := decodeEvent(t, line)
		if name != want[i][0] || fmt.Sprint(args) != fmt.Sprint(want[i][1]) || frontend != want[i][2] {
			t.Errorf("transaction %d: %s want %v", i, line, want[i])
		}
	}
}

func withPrefix(prefix []string, packages ...string) []string {
	return append(append([]string{}, prefix...), packages...)
}

var dependencyManagers = []struct {
	name   string
	prefix []string
}{
	{"apk", []string{"add", "--no-cache"}},
	{"apt-get", []string{"install", "-y", "--no-install-recommends"}},
	{"dnf", []string{"install", "-y"}},
	{"yum", []string{"install", "-y"}},
	{"pacman", []string{"-S", "--needed", "--noconfirm"}},
	{"zypper", []string{"--non-interactive", "install", "--no-recommends"}},
}

func expectedTransactions(manager string, install []string) [][3]any {
	want := [3]any{manager, install, frontendFor(manager)}
	if manager == "apt-get" {
		return [][3]any{{"apt-get", []string{"update"}, nil}, want}
	}
	return [][3]any{want}
}

func TestMissingRuntimeToolsInstalledAndRechecked(t *testing.T) {
	for _, m := range dependencyManagers {
		m := m
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{missing: []string{"awk", "basename", "grep", "sed", "wget"}, manager: m.name}, nil)
			r := h.run(t)
			if r.code != 0 || !strings.HasSuffix(r.stdout, "ready\n") {
				t.Fatal(r.stdout + r.stderr)
			}
			transactions := h.transactions(t)
			eventsEqual(t, transactions, expectedTransactions(m.name, withPrefix(m.prefix, "gawk", "coreutils", "grep", "sed", "curl", "ca-certificates")))
			if r := h.run(t); r.code != 0 || len(h.transactions(t)) != len(transactions) {
				t.Error("second run made a transaction")
			}
		})
	}
}

func TestExistingWgetOnlyInstallsMissingRuntimePackage(t *testing.T) {
	t.Parallel()
	h := newDependencyHarness(t, dependencySetup{missing: []string{"sed"}}, nil)
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stderr)
	}
	eventsEqual(t, h.transactions(t), [][3]any{{"apk", []string{"add", "--no-cache", "sed"}, nil}})
}

func TestNoPackageManagerGivesManualRepairWithoutContinuing(t *testing.T) {
	t.Parallel()
	h := newDependencyHarness(t, dependencySetup{missing: []string{"awk"}, manager: "-"}, nil)
	r := h.run(t)
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stderr, "未找到支持的包管理器")
	contains(t, r.stderr, "请手动安装 gawk 后重新运行")
	notContains(t, r.stdout, "ready")
	if readFile(t, h.events) != "" {
		t.Error("transactions")
	}
}

func TestDependencyFailedPackageTransactionDoesNotContinue(t *testing.T) {
	for _, c := range [][2]string{{"apk", "install"}, {"apt-get", "update"}, {"apt-get", "install"}} {
		c := c
		t.Run(c[0]+"/"+c[1], func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{missing: []string{"grep"}, manager: c[0], options: []string{"FAIL_PACKAGE=" + c[1]}}, nil)
			r := h.run(t)
			if r.code == 0 {
				t.Fatal("succeeded")
			}
			contains(t, r.stderr, "Agent 系统依赖安装失败")
			notContains(t, r.stdout, "ready")
			if c[1] == "update" && len(h.transactions(t)) != 1 {
				t.Error(h.transactions(t))
			}
		})
	}
}

func TestNominalPackageSuccessMustProvideActualTools(t *testing.T) {
	t.Parallel()
	h := newDependencyHarness(t, dependencySetup{missing: []string{"grep"}, options: []string{"PACKAGE_NO_COMMANDS=yes"}}, nil)
	r := h.run(t)
	if r.code == 0 {
		t.Fatal("succeeded")
	}
	contains(t, r.stderr, "包管理器执行后仍缺少 Agent 工具：grep")
	notContains(t, r.stdout, "ready")
}

var gatewayTools = []string{"nft", "ss", "tar", "gzip"}

func stubTools(tools ...string) func(tmp string) {
	return func(tmp string) {
		for _, tool := range tools {
			path := filepath.Join(tmp, "commands", tool)
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755)
		}
	}
}

func TestGatewayToolsInstallDistributionPackagesOnce(t *testing.T) {
	for _, m := range dependencyManagers {
		if m.name == "yum" {
			continue // not in the Python parameter list
		}
		m := m
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{missing: gatewayTools, manager: m.name, required: gatewayTools}, stubTools(gatewayTools...))
			r := h.run(t)
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			contains(t, r.stdout, "缺少工具：nft ss tar gzip")
			iproute := "iproute2"
			if m.name == "dnf" {
				iproute = "iproute"
			}
			var installs []string
			for _, line := range h.transactions(t) {
				if _, args, _ := decodeEvent(t, line); fmt.Sprint(args) != "[update]" {
					installs = append(installs, line)
				}
			}
			eventsEqual(t, installs, [][3]any{{m.name, withPrefix(m.prefix, "nftables", iproute, "tar", "gzip"), frontendFor(m.name)}})
			count := len(h.transactions(t))
			if r := h.run(t); r.code != 0 || len(h.transactions(t)) != count {
				t.Error("second run")
			}
		})
	}
}

func TestFailedInstallPrintsACopyableManualCommand(t *testing.T) {
	for _, c := range [][2]string{
		{"apk", "apk add --no-cache nftables iproute2"},
		{"apt-get", "apt-get update && apt-get install -y --no-install-recommends nftables iproute2"},
		{"dnf", "dnf install -y nftables iproute"},
		{"pacman", "pacman -S --needed nftables iproute2"},
		{"zypper", "zypper install nftables iproute2"},
	} {
		c := c
		t.Run(c[0], func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{missing: []string{"nft", "ss"}, manager: c[0], required: []string{"nft", "ss"},
				options: []string{"FAIL_PACKAGE=install"}}, stubTools("nft", "ss"))
			r := h.run(t)
			if r.code == 0 {
				t.Fatal("succeeded")
			}
			if !strings.HasSuffix(pyStrip(r.stderr), "可在 root 下手动执行："+c[1]) {
				t.Errorf("stderr: %s", r.stderr)
			}
			notContains(t, r.stdout, "ready")
		})
	}
}

// forwardingCombinations is combinations(FORWARDING_TOOLS, 1..3).
func forwardingCombinations() [][]string {
	s, c, l := forwardingTools[0], forwardingTools[1], forwardingTools[2]
	return [][]string{{s}, {c}, {l}, {s, c}, {s, l}, {c, l}, {s, c, l}}
}

func TestForwardingToolsInstallOnlyNeededPackagesAndSkipSecondRun(t *testing.T) {
	for _, m := range forwardingManagers {
		for _, missing := range forwardingCombinations() {
			m, missing := m, missing
			t.Run(m.name+"/"+strings.Join(missing, "+"), func(t *testing.T) {
				t.Parallel()
				h := newDependencyHarness(t, dependencySetup{missing: missing, manager: m.name, required: forwardingTools}, nil)
				r := h.run(t)
				if r.code != 0 || !strings.HasSuffix(r.stdout, "ready\n") {
					t.Fatal(r.stdout + r.stderr)
				}
				packageFor := map[string]string{"sysctl": m.sysctlPackage, "cmp": "diffutils", "ln": "coreutils"}
				var packages []string
				for _, tool := range missing {
					packages = append(packages, packageFor[tool])
				}
				transactions := h.transactions(t)
				eventsEqual(t, transactions, expectedTransactions(m.name, withPrefix(m.prefix, packages...)))
				contains(t, r.stdout, "缺少工具："+strings.Join(missing, " "))
				for _, tool := range forwardingTools {
					if !exists(filepath.Join(h.tmp, "commands", tool)) {
						t.Errorf("%s not installed", tool)
					}
				}
				if r := h.run(t); r.code != 0 || fmt.Sprint(h.transactions(t)) != fmt.Sprint(transactions) {
					t.Error("second run")
				}
			})
		}
	}
}

func TestExistingForwardingCommandsDoNotInstallAnyPackages(t *testing.T) {
	for _, manager := range []string{"apt-get", "apk", "pacman", "-"} {
		manager := manager
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{manager: manager, required: forwardingTools}, nil)
			if r := h.run(t); r.code != 0 || readFile(t, h.events) != "" {
				t.Error(r.stderr, readFile(t, h.events))
			}
		})
	}
}

func TestSharedCoreutilsIsInstalledOnceForRuntimeAndForwardingTools(t *testing.T) {
	for _, m := range forwardingManagers {
		m := m
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{missing: []string{"head", "ln"}, manager: m.name, required: forwardingTools}, nil)
			if r := h.run(t); r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			transactions := h.transactions(t)
			_, args, _ := decodeEvent(t, transactions[len(transactions)-1])
			if fmt.Sprint(args) != fmt.Sprint(withPrefix(m.prefix, "coreutils")) {
				t.Error(transactions)
			}
		})
	}
}

func TestMissingTimezoneDataInstalledForOxidns(t *testing.T) {
	for _, c := range [][2]string{{"apk", "tzdata"}, {"apt-get", "tzdata"}, {"zypper", "timezone"}} {
		c := c
		t.Run(c[0], func(t *testing.T) {
			t.Parallel()
			h := newDependencyHarness(t, dependencySetup{manager: c[0], required: []string{"zoneinfo"}, noTZ: true}, nil)
			r := h.run(t)
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			contains(t, r.stdout, "时区数据")
			var installs [][]string
			for _, line := range h.transactions(t) {
				if _, args, _ := decodeEvent(t, line); fmt.Sprint(args) != "[update]" {
					installs = append(installs, args)
				}
			}
			if len(installs) != 1 || installs[0][len(installs[0])-1] != c[1] || !exists(filepath.Join(h.tmp, "zoneinfo/Asia/Shanghai")) {
				t.Error(installs)
			}
			count := len(h.transactions(t))
			if r := h.run(t); r.code != 0 || len(h.transactions(t)) != count {
				t.Error("second run")
			}
		})
	}
}

func TestTimezoneDataNotRequiredWithoutOxidns(t *testing.T) {
	t.Parallel()
	h := newDependencyHarness(t, dependencySetup{noTZ: true}, nil)
	if r := h.run(t); r.code != 0 || readFile(t, h.events) != "" {
		t.Error(r.stderr)
	}
}

func TestUnrequestedForwardingCommandsAreNotInstalled(t *testing.T) {
	t.Parallel()
	h := newDependencyHarness(t, dependencySetup{missing: forwardingTools}, nil)
	if r := h.run(t); r.code != 0 || readFile(t, h.events) != "" {
		t.Error(r.stderr)
	}
	for _, tool := range forwardingTools {
		if exists(filepath.Join(h.tmp, "commands", tool)) {
			t.Errorf("%s installed", tool)
		}
	}
}

func TestInstallEntrypointsRequestForwardingToolsOnlyWhenEnabled(t *testing.T) {
	calls := func(source string) []string {
		var out []string
		for _, line := range strings.Split(source, "\n") {
			s := pyStrip(line)
			if strings.HasPrefix(s, "agent_ensure_dependencies") && !strings.HasSuffix(line, "{") {
				out = append(out, s)
			}
		}
		return out
	}
	for i, c := range []struct {
		source string
		want   []string
	}{
		{ok(aioGenerated(t, nil)), []string{"agent_ensure_dependencies zoneinfo sysctl cmp ln"}},
		{ok(aioGenerated(t, func(o *AIOOptions) { o.ManageIPv4Forwarding, o.ManageTun = false, false })), []string{"agent_ensure_dependencies zoneinfo"}},
		{ok(singleGenerated("mihomo", nil)), []string{"agent_ensure_dependencies"}},
		{ok(singleGenerated("oxidns", nil)), []string{"agent_ensure_dependencies zoneinfo"}},
		{ok(gatewayGenerate(nil)), []string{"agent_ensure_dependencies ha_ip nft ss tar gzip sha256sum cmp zoneinfo", "agent_ensure_dependencies zoneinfo"}},
	} {
		if got := calls(c.source); fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("case %d: %q want %q", i, got, c.want)
		}
	}
}

// installerHarness is installer_harness(tmp_path, monkeypatch, mode, combined):
// the script, its env, its temp root and its events file.
func installerHarness(t *testing.T, mode string, combined bool) (string, env, string) {
	if mode == "aio" {
		var h aioHarness
		if combined {
			h = newAIOCoreHarness(t, "openrc", true, false)
		} else {
			h = newAIOHarness(t, "openrc", true, nil)
		}
		return h.installer, env(h.env), h.tmp
	}
	h := newSingleHarness(t, singleSetup{manager: "openrc", installCore: combined})
	writeFile(t, filepath.Join(h.root, "opt/configflow-agent/config-mihomo.json"),
		`{"service_type": "mihomo", "service_unit": "mihomo", "service_manager": "openrc", "agent_name": "single", "agent_id": "old-id", "token": "old-token"}`, 0o644)
	return h.script, h.env, h.tmp
}

func TestInstallRepairsToolsBeforeInspectingExistingIdentity(t *testing.T) {
	for _, mode := range []string{"single", "aio"} {
		for _, combined := range []bool{false, true} {
			mode, combined := mode, combined
			t.Run(fmt.Sprint(mode, combined), func(t *testing.T) {
				t.Parallel()
				script, e, tmp := installerHarness(t, mode, combined)
				e, events := isolateDependencies(t, tmp, e, []string{"grep", "head", "sed"}, "apk", true)
				r := runInstaller(t, script, e, "1")
				if r.code != 0 {
					t.Fatal(r.stdout + r.stderr)
				}
				if len(lines(readFile(t, events))) != 1 {
					t.Error(readFile(t, events))
				}
				config, want := filepath.Join(tmp, "root/opt/configflow-agent/config-mihomo.json"), "old-id"
				if mode == "aio" {
					config, want = filepath.Join(tmp, "agent/mihomo/config.json"), "id-mihomo"
				}
				var cfg map[string]any
				json.Unmarshal([]byte(readFile(t, config)), &cfg)
				if cfg["agent_id"] != want {
					t.Error(cfg)
				}
			})
		}
	}
}

func TestCancelAndUninstallNeverInstallSystemDependencies(t *testing.T) {
	for _, mode := range []string{"single", "aio"} {
		for _, choice := range []string{"2", "3"} {
			mode, choice := mode, choice
			t.Run(mode+choice, func(t *testing.T) {
				t.Parallel()
				script, e, tmp := installerHarness(t, mode, true)
				e, events := isolateDependencies(t, tmp, e, []string{"wc"}, "apk", true)
				r := runInstaller(t, script, e, choice)
				if r.code != 0 {
					t.Fatal(r.stdout + r.stderr)
				}
				if readFile(t, events) != "" || exists(filepath.Join(tmp, "core-completed")) {
					t.Error("dependencies installed or core ran")
				}
			})
		}
	}
}

func TestUnsupportedArchitectureDoesNotInstallMissingDependencies(t *testing.T) {
	for _, mode := range []string{"single", "aio"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			script, e, tmp := installerHarness(t, mode, true)
			e, events := isolateDependencies(t, tmp, e, []string{"awk"}, "apk", true)
			r := runInstaller(t, script, e.with("TEST_ARCH=unsupported"), "1")
			if r.code == 0 {
				t.Fatal("succeeded")
			}
			contains(t, r.stdout+r.stderr, "不支持的 CPU 架构")
			if readFile(t, events) != "" || exists(filepath.Join(tmp, "core-completed")) {
				t.Error("dependencies installed or core ran")
			}
		})
	}
}

func TestDependencyFailurePreservesExistingAgentsBeforeCorePhase(t *testing.T) {
	for _, mode := range []string{"single", "aio"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			script, e, tmp := installerHarness(t, mode, true)
			agentDir := filepath.Join(tmp, "root/opt/configflow-agent")
			if mode == "aio" {
				agentDir = filepath.Join(tmp, "agent")
			}
			before := snapshotFiles(t, agentDir)
			e, _ = isolateDependencies(t, tmp, e, []string{"grep"}, "apk", true)
			r := runInstaller(t, script, e.with("FAIL_PACKAGE=install"), "1")
			if r.code == 0 {
				t.Fatal("succeeded")
			}
			contains(t, r.stderr, "Agent 系统依赖安装失败")
			if exists(filepath.Join(tmp, "core-completed")) {
				t.Error("core ran")
			}
			notContains(t, eventsText(t, filepath.Join(tmp, "events")), "agent-download")
			if after := snapshotFiles(t, agentDir); fmt.Sprint(after) != fmt.Sprint(before) {
				t.Error("agent files changed")
			}
		})
	}
}

func TestAIORepairsForwardingToolsBeforeCoreAndAgentChanges(t *testing.T) {
	for _, m := range forwardingManagers {
		for _, combined := range []bool{false, true} {
			m, combined := m, combined
			t.Run(fmt.Sprint(m.name, combined), func(t *testing.T) {
				t.Parallel()
				script, e, tmp := installerHarness(t, "aio", combined)
				e, events := isolateDependencies(t, tmp, e, forwardingTools, m.name, true)
				r := runInstaller(t, script, e, "1")
				if r.code != 0 {
					t.Fatal(r.stdout + r.stderr)
				}
				transactions := lines(readFile(t, events))
				_, args, _ := decodeEvent(t, transactions[len(transactions)-1])
				want := 1
				if m.name == "apt-get" {
					want = 2
				}
				if fmt.Sprint(args) != fmt.Sprint(withPrefix(m.prefix, m.sysctlPackage, "diffutils", "coreutils")) || len(transactions) != want {
					t.Error(transactions)
				}
				log := eventsText(t, filepath.Join(tmp, "events"))
				a, b, c := strings.Index(log, "package-install"), strings.Index(log, "agent mihomo"), strings.Index(log, "sysctl ")
				if a < 0 || !(a < b && b < c) {
					t.Errorf("order: %s", log)
				}
				if combined {
					x, y, z := strings.Index(log, "package-install"), strings.Index(log, "core --core both"), strings.Index(log, "agent-download")
					if !(x < y && y < z) || !exists(filepath.Join(tmp, "core-completed")) {
						t.Error("core order")
					}
				} else {
					notContains(t, log, "core --core")
				}
				if readFile(t, filepath.Join(tmp, "ip_forward")) != "1\n" {
					t.Error("forwarding")
				}
				contains(t, readFile(t, filepath.Join(tmp, forwardConfig)), "net.ipv4.ip_forward=1")
				for _, service := range []string{"mihomo", "oxidns"} {
					var cfg map[string]any
					json.Unmarshal([]byte(readFile(t, filepath.Join(tmp, "agent", service, "config.json"))), &cfg)
					if cfg["agent_id"] != "id-"+service {
						t.Error(cfg)
					}
				}
			})
		}
	}
}

func TestForwardingDependencyFailureKeepsExistingServicesAndConfigs(t *testing.T) {
	for _, manager := range []string{"apt-get", "apk", "pacman"} {
		for _, tool := range forwardingTools {
			for _, failure := range []string{"install", "no-commands"} {
				manager, tool, failure := manager, tool, failure
				t.Run(manager+"/"+tool+"/"+failure, func(t *testing.T) {
					t.Parallel()
					script, e, tmp := installerHarness(t, "aio", true)
					before := snapshotFiles(t, filepath.Join(tmp, "agent"), filepath.Join(tmp, "etc"))
					e, _ = isolateDependencies(t, tmp, e, []string{tool}, manager, true)
					if failure == "install" {
						e = e.with("FAIL_PACKAGE=install")
					} else {
						e = e.with("PACKAGE_NO_COMMANDS=yes")
					}
					r := runInstaller(t, script, e, "1")
					if r.code == 0 {
						t.Fatal("succeeded")
					}
					out := r.stdout + r.stderr + detailOutput(t, r)
					contains(t, out, tool)
					if failure == "install" {
						contains(t, out, "Agent 系统依赖安装失败")
					} else {
						contains(t, out, "包管理器执行后仍缺少 Agent 工具")
					}
					notContains(t, r.stdout, "安装完成")
					if exists(filepath.Join(tmp, "core-completed")) {
						t.Error("core ran")
					}
					for _, line := range lines(eventsText(t, filepath.Join(tmp, "events"))) {
						if line != "package-update" && line != "package-install" {
							t.Error(line)
						}
					}
					if readFile(t, filepath.Join(tmp, "ip_forward")) != "0\n" || exists(filepath.Join(tmp, forwardConfig)) {
						t.Error("forwarding changed")
					}
					sameFiles(t, before)
				})
			}
		}
	}
}

func TestOtherInstallersDoNotAddForwardingPackages(t *testing.T) {
	for _, mode := range []string{"single", "aio-optout"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			var script, tmp string
			var e env
			if mode == "single" {
				script, e, tmp = installerHarness(t, "single", false)
			} else {
				h := newAIOHarness(t, "openrc", true, func(o *AIOOptions) { o.ManageIPv4Forwarding, o.ManageTun = false, false })
				script, e, tmp = h.installer, env(h.env), h.tmp
			}
			e, events := isolateDependencies(t, tmp, e, forwardingTools, "apk", true)
			r := runInstaller(t, script, e, "1")
			if r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			if readFile(t, events) != "" || exists(filepath.Join(tmp, forwardConfig)) {
				t.Error("forwarding packages")
			}
			for _, tool := range forwardingTools {
				if exists(filepath.Join(tmp, "commands", tool)) {
					t.Errorf("%s installed", tool)
				}
			}
		})
	}
}
