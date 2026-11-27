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

// Port of backend/test_shell_ha_prepare.py: one-command native OxiDNS HA
// preparation; every subprocess is isolated. The route case
// (test_unsupported_ha_requests_are_rejected) is in
// internal/api/agents/installer_ha_test.go.

func haGenerated(core bool) string {
	return ok(RenderAgentInstall(AgentInstallOptions{AgentPort: 8080, ServerURL: "https://config.test", AgentName: "ha-node", ServiceType: "oxidns",
		PrepareDNSHA: true, InstallCore: core}))
}

func TestMarkersAndAutoInstallKeepNormalInstallerUnchanged(t *testing.T) {
	for _, core := range []bool{false, true} {
		source := haGenerated(core)
		lines := strings.Split(source, "\n")
		if lines[2] != DNSHAPreparationInterface || (core && lines[1] != AgentCoreInstallInterface) {
			t.Errorf("core=%v: markers %q", core, lines[:3])
		}
		contains(t, source, `exec sudo sh "$0" "$@"`)
		notContains(t, source, "read -r choice < /dev/tty")
		contains(t, source, "choice=1")
		contains(t, source, `DNS_HA_EFFECTIVE_PORT="8080"`)
		if strings.Index(source, "agent_prepare_dns_ha\n") > strings.Index(source, "BINARY_BASE=") {
			t.Error("preparation after the Agent download")
		}
		checkSh(t, source)
	}
	plain := func(prepare bool) string {
		return ok(RenderAgentInstall(AgentInstallOptions{AgentPort: 8080, ServerURL: "https://config.test", AgentName: "plain", ServiceType: "oxidns", PrepareDNSHA: prepare}))
	}
	ordinary := plain(false)
	notContains(t, ordinary, DNSHAPreparationInterface)
	contains(t, ordinary, "read -r choice < /dev/tty")
	notContains(t, ordinary, "agent_prepare_dns_ha")
}

func TestPrepareExistingStopsBeforeInstallation(t *testing.T) {
	source := ok(RenderAgentInstall(AgentInstallOptions{AgentPort: 8080, ServerURL: "https://config.test", AgentName: "ha-node", ServiceType: "oxidns", PrepareDNSHA: true, PrepareExisting: true}))
	contains(t, source, `CONFIG_FILE="$AGENT_DIR/config.json"`)
	stop := strings.Index(source, "agent_prepare_dns_ha\ncf_complete")
	if stop < 0 || stop > strings.Index(source, "for isolated_dir") || !strings.HasPrefix(source[stop:], "agent_prepare_dns_ha\ncf_complete \"主备工具已准备，请返回面板重新检查。\"\nexit 0") {
		t.Fatal("existing host must finish before the single-service installer")
	}
	checkSh(t, source)
}

type haHarness struct {
	tmp, script, root, events string
	env                       env
}

func (h *haHarness) run(t *testing.T, extra ...string) runResult {
	return runShell(t, "/bin/sh", h.script, runOptions{env: h.env.with(extra...), replace: true, timeout: 30e9})
}

func (h *haHarness) transactions(t *testing.T, manager string) [][]string {
	var out [][]string
	for _, line := range lines(readFile(t, h.events)) {
		var item []json.RawMessage
		json.Unmarshal([]byte(line), &item)
		var name string
		var args []string
		json.Unmarshal(item[0], &name)
		json.Unmarshal(item[1], &args)
		if manager == "" || name == manager {
			out = append(out, append([]string{name}, args...))
		}
	}
	return out
}

func (h *haHarness) path(rel string) string { return filepath.Join(h.root, rel) }

// newHAHarness is preparation_harness(tmp_path, manager, init).
func newHAHarness(t *testing.T, manager, init string) *haHarness {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "root")
	commands := filepath.Join(tmp, "commands")
	os.MkdirAll(commands, 0o755)
	for _, dir := range []string{"etc/init.d", "etc/keepalived", "etc/systemd/system", "run/systemd/system", "state", "opt/configflow-agent"} {
		os.MkdirAll(filepath.Join(root, dir), 0o755)
	}
	for _, tool := range strings.Fields("awk basename cat chmod cp dirname grep head mkdir mktemp mv readlink rm sed wc sh uname jq") {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("%s is required on the test server: %v", tool, err)
		}
		os.Symlink(real, filepath.Join(commands, tool))
	}
	writeFile(t, filepath.Join(commands, "sleep"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(commands, "wget"), "#!/bin/sh\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(commands, "ip"), "#!/bin/sh\nexit 2\n", 0o755)
	writeFile(t, filepath.Join(commands, "systemctl"), stubScript(t, "ha-systemctl", ""), 0o755)
	writeFile(t, filepath.Join(commands, manager), stubScript(t, "ha-package", ""), 0o755)
	source := "set -eu\ncf_step() { :; }; cf_step_done() { :; }; cf_fact() { :; }; cf_ui() { printf \"%s\\n\" \"$*\"; }\n"
	source += AgentInstallDependencies + "\n" + strings.ReplaceAll(dnsHAPreparation, "{agent_port}", "8082")
	source += "\nINIT_SYSTEM=" + init + "\nAGENT_DIR=" + shlexQuote(filepath.Join(root, "opt/configflow-agent")) +
		"\nCONFIG_FILE=\"$AGENT_DIR/config-oxidns.json\"\nagent_prepare_dns_ha\nprintf \"ready:%s\\n\" \"$DNS_HA_EFFECTIVE_PORT\"\n"
	source = replaceAll(source, [][2]string{{"/etc/", root + "/etc/"}, {"/run/", root + "/run/"}, {"/usr/sbin/", root + "/usr/sbin/"}})
	script := writeFile(t, filepath.Join(tmp, "prepare.sh"), source, 0o644)
	events := writeFile(t, filepath.Join(tmp, "events"), "", 0o644)
	e := baseEnv().with("PATH="+commands, "HA_ROOT="+root, "HA_COMMANDS="+commands, "HA_EVENTS="+events)
	return &haHarness{tmp: tmp, script: script, root: root, events: events, env: e}
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestMissingHAPackagesAreInstalledOnceWithoutActivatingDefaultVIP(t *testing.T) {
	for _, c := range [][3]string{
		{"apt-get", "systemd", "iproute2"}, {"apk", "openrc", "iproute2"}, {"dnf", "systemd", "iproute"},
		{"yum", "systemd", "iproute"}, {"pacman", "systemd", "iproute2"}, {"zypper", "systemd", "iproute2"},
	} {
		manager, init, ipPackage := c[0], c[1], c[2]
		t.Run(manager, func(t *testing.T) {
			t.Parallel()
			h := newHAHarness(t, manager, init)
			if r := h.run(t); r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			transactions := h.transactions(t, manager)
			last := transactions[len(transactions)-1]
			if !hasArg(last, ipPackage) || !hasArg(last, "keepalived") || hasArg(last, "keepalived-openrc") != (init == "openrc") {
				t.Error(last)
			}
			if exists(h.path("state/unexpected-start")) || isSymlink(h.path("run/systemd/system/keepalived.service")) ||
				(init == "systemd" && exists(h.path("state/enabled"))) {
				t.Error("keepalived activated")
			}
			if !strings.HasPrefix(readFile(t, h.path("etc/keepalived/keepalived.conf")), "# ConfigFlow:") {
				t.Error("config not replaced")
			}
			samples, _ := filepath.Glob(h.path("etc/keepalived/keepalived.conf.package-default.*"))
			if len(samples) == 0 || !strings.HasPrefix(readFile(t, samples[0]), "vrrp_instance SAMPLE") {
				t.Error("package sample not kept")
			}
			if r := h.run(t); r.code != 0 {
				t.Fatal(r.stderr)
			}
			if fmt.Sprint(h.transactions(t, manager)) != fmt.Sprint(transactions) {
				t.Error("second run made a transaction")
			}
		})
	}
}

func TestFailedHAPackageTransactionCleansMaskAndSamplesAndAllowsRetry(t *testing.T) {
	for _, failure := range []string{"before", "after-keepalived"} {
		failure := failure
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			h := newHAHarness(t, "apt-get", "systemd")
			if r := h.run(t, "FAIL_PACKAGE="+failure); r.code == 0 {
				t.Fatal("succeeded")
			}
			if isSymlink(h.path("run/systemd/system/keepalived.service")) || exists(h.path("state/enabled")) || exists(h.path("state/unexpected-start")) {
				t.Error("not cleaned")
			}
			if r := h.run(t); r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			if !strings.HasPrefix(readFile(t, h.path("etc/keepalived/keepalived.conf")), "# ConfigFlow:") {
				t.Error("retry did not prepare")
			}
		})
	}
}

func TestPartialMaskFailureIsCleanedBeforeExiting(t *testing.T) {
	t.Parallel()
	h := newHAHarness(t, "apt-get", "systemd")
	if r := h.run(t, "MASK_PARTIAL_FAILURE=yes"); r.code == 0 {
		t.Fatal("succeeded")
	}
	if isSymlink(h.path("run/systemd/system/keepalived.service")) {
		t.Error("mask left")
	}
	for _, item := range h.transactions(t, "") {
		if item[0] != "systemctl" {
			t.Error(item)
		}
	}
}

func TestMissingDaemonWithExistingUserConfigIsRejectedBeforePackageChanges(t *testing.T) {
	t.Parallel()
	h := newHAHarness(t, "apt-get", "systemd")
	config := writeFile(t, h.path("etc/keepalived/keepalived.conf"), "vrrp_instance USER {}\n", 0o644)
	if r := h.run(t); r.code == 0 {
		t.Fatal("succeeded")
	}
	if readFile(t, config) != "vrrp_instance USER {}\n" || len(h.transactions(t, "apt-get")) != 0 {
		t.Error("user config or packages touched")
	}
}

func TestExistingBinaryWithoutSystemdUnitStillInstallsServicePackage(t *testing.T) {
	t.Parallel()
	h := newHAHarness(t, "apt-get", "systemd")
	writeFile(t, filepath.Join(h.env.get("HA_COMMANDS"), "keepalived"), "#!/bin/sh\nexit 0\n", 0o755)
	if r := h.run(t); r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	transactions := h.transactions(t, "apt-get")
	if len(transactions) == 0 || !hasArg(transactions[len(transactions)-1], "keepalived") || !exists(h.path("state/unit")) ||
		exists(h.path("state/unexpected-start")) || isSymlink(h.path("run/systemd/system/keepalived.service")) || exists(h.path("state/enabled")) {
		t.Error(transactions)
	}
}

// fullHAInstaller is full_installer(tmp_path, monkeypatch).
func fullHAInstaller(t *testing.T) *singleHarness {
	h := newSingleHarness(t, singleSetup{service: "oxidns", installCore: true, mutate: func(o *AgentInstallOptions) { o.PrepareDNSHA = true }})
	for name, body := range map[string]string{"keepalived": "exit 0", "ip": "printf \"[]\\n\""} {
		writeFile(t, filepath.Join(h.tmp, "commands", name), "#!/bin/sh\n"+body+"\n", 0o755)
	}
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("jq must be installed on the test server")
	}
	return h
}

func (h *singleHarness) runNoInput(t *testing.T, args ...string) runResult {
	return runShell(t, "/bin/sh", h.script, runOptions{args: args, env: h.env, replace: true, timeout: 60e9})
}

func TestCompleteGeneratedInstallerAutomaticallyRunsAndPreservesExistingCustomConfig(t *testing.T) {
	h := fullHAInstaller(t)
	if r := h.runNoInput(t); r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	config := filepath.Join(h.root, "opt/configflow-agent/config-oxidns.json")
	var original map[string]any
	json.Unmarshal([]byte(readFile(t, config)), &original)
	for k, v := range map[string]any{"agent_name": "existing-user-name", "agent_port": 18082, "agent_id": "retained-id", "token": "retained-token",
		"config_path": "/srv/custom/oxidns.yaml", "service_unit": "custom-oxidns", "health_url": "http://localhost:9199/health"} {
		original[k] = v
	}
	data, _ := json.MarshalIndent(original, "", "  ")
	writeFile(t, config, string(data)+"\n", 0o644)
	before := readFile(t, config)
	r := h.runNoInput(t)
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if readFile(t, config) != before || strings.Count(h.eventsText(t), "core --core oxidns") != 1 {
		t.Error("config changed or core reinstalled")
	}
	contains(t, r.stdout, "端口 18082")
}

func TestMaintenanceDoesNotPrepareHAOrInstallCore(t *testing.T) {
	for _, flag := range []string{"--cancel", "--uninstall"} {
		h := fullHAInstaller(t)
		writeFile(t, filepath.Join(h.tmp, "commands/keepalived"), "#!/bin/sh\nexit 88\n", 0o755)
		r := h.runNoInput(t, flag)
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		notContains(t, h.eventsText(t), "core --core")
		notContains(t, h.eventsText(t), "agent-download")
		notContains(t, r.stdout, "DNS 高可用依赖已准备")
	}
}

func TestUnusableKeepalivedStopsBeforeCoreOrAgentMutation(t *testing.T) {
	h := fullHAInstaller(t)
	writeFile(t, filepath.Join(h.tmp, "commands/keepalived"), "#!/bin/sh\nexit 88\n", 0o755)
	if r := h.runNoInput(t); r.code == 0 {
		t.Fatal("succeeded")
	}
	notContains(t, h.eventsText(t), "core --core")
	notContains(t, h.eventsText(t), "agent-download")
}

func TestFullInstallerHandlesRootPathWithoutUsrLocalBin(t *testing.T) {
	h := fullHAInstaller(t)
	h.env = h.env.with("PATH=" + h.tmp + "/commands:/usr/sbin:/usr/bin:/sbin:/bin")
	if r := h.runNoInput(t); r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	if strings.Count(h.eventsText(t), "core --core oxidns") != 1 || !exists(filepath.Join(h.root, "opt/configflow-agent/config-oxidns.json")) {
		t.Error("core or Agent missing")
	}
}
