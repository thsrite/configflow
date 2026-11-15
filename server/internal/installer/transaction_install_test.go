package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// Port of backend/test_agent_transaction_install.py (native installer
// cases). The four Docker entrypoint cases test
// docker/docker-agent-entrypoint-service.sh, which no generator renders;
// they stay with the image (finish domain, #40).

func goGenerated(mutate func(*GoAgentOptions)) (string, error) {
	o := GoAgentOptions{ServerURL: "https://config.test", AgentName: "native-test", ServiceType: "mihomo",
		BinaryDownloadURL: "https://config.test/api/agents/download"}
	if mutate != nil {
		mutate(&o)
	}
	return GenerateGoAgentInstallScript(o)
}

func generatedConfig(t *testing.T, script string) map[string]any {
	_, rest, found := strings.Cut(script, "<<'CONFIGFLOW_JSON_EOF'\n")
	if !found {
		t.Fatal("no config heredoc")
	}
	body, _, _ := strings.Cut(rest, "\nCONFIGFLOW_JSON_EOF")
	var cfg map[string]any
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNativeInstallHasExplicitLifecycleWithoutBootGate(t *testing.T) {
	for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
		script := ok(goGenerated(func(o *GoAgentOptions) { o.ServiceType = service }))
		cfg := generatedConfig(t, script)
		if cfg["service_manager"] != "__CONFIGFLOW_INIT__" || cfg["service_unit"] != service || cfg["service_binary"] != service || cfg["deployment_method"] != "shell" {
			t.Errorf("%s: %v", service, cfg)
		}
		for _, absent := range []string{"RemainAfterExit=yes", "Requires=configflow-recover-" + service + ".service", `rc_need="${rc_need:-} configflow-recover-`, "ExecStartPre=", "/tmp/configflow-agent"} {
			notContains(t, script, absent)
		}
		for _, present := range []string{`-config "$CONFIG_FILE.new" -check-deployments`, `-config "$CONFIG_FILE" -check-deployments`, `"$AGENT_DIR/config.json"`,
			"mktemp /usr/local/bin/.configflow-agent.install.XXXXXX", `mv "$BINARY_TMP" /usr/local/bin/configflow-agent`, "trap"} {
			contains(t, script, present)
		}
		path := writeFile(t, filepath.Join(t.TempDir(), "install.sh"), script, 0o644)
		if r := runShell(t, "sh", "-n", runOptions{args: []string{path}}); r.code != 0 {
			t.Fatal(r.stderr)
		}
	}
}

func TestCustomRestartRequiresExplicitLifecycle(t *testing.T) {
	if _, err := goGenerated(func(o *GoAgentOptions) { o.RestartCommand = "/custom/restart" }); err == nil || !strings.Contains(err.Error(), "stop_command") {
		t.Fatalf("err %v", err)
	}
	cfg := generatedConfig(t, ok(goGenerated(func(o *GoAgentOptions) {
		o.RestartCommand, o.StopCommand, o.StartCommand, o.StatusCommand = "/custom/restart", "/custom/stop", "/custom/start", "/custom/status"
	})))
	if cfg["service_manager"] != "command" || cfg["stop_command"] != "/custom/stop" || cfg["start_command"] != "/custom/start" {
		t.Fatal(cfg)
	}
}

func TestInstallerPreservesLiteralJSONAndShellCharacters(t *testing.T) {
	value := "/tmp/\"config${HOME}{service_type}\\file\n.yaml"
	if cfg := generatedConfig(t, ok(goGenerated(func(o *GoAgentOptions) { o.ConfigPath = value }))); cfg["config_path"] != value {
		t.Fatal(cfg["config_path"])
	}
}

type nativeHarness struct {
	tmp, script, gate, core, dropin, agentDropin, config string
	env                                                  []string
}

func (h nativeHarness) run(t *testing.T, extra ...string) runResult {
	return runShell(t, "sh", h.script, runOptions{env: append(append([]string{}, h.env...), extra...), replace: true, input: "1\n", timeout: 30e9})
}

func (h nativeHarness) events() []string {
	data, _ := os.ReadFile(filepath.Join(h.tmp, "events"))
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

const nativeFakeAgent = `#!/bin/sh
printf 'agent %s\n' "$*" >> "$EVENTS"
case "$*" in
    *-recover-*) exit 42 ;;
    *-validate-service*) if [ "${FAIL_VALIDATION:-}" = "yes" ]; then exit 46; fi ;;
    *-check-environment*)
        if [ "${FAIL_ENVIRONMENT:-}" = "yes" ]; then
            echo "端口已被其他进程占用；修复建议：选择空闲端口或调整冲突服务。" >&2
            exit 48
        fi
        ;;
esac
`

const nativeManagerMock = `printf '%s %s\n' "$(basename "$0")" "$*" >> "$EVENTS"
case "$*" in *stop*configflow-recover*|*configflow-recover*stop*|*stop*old-core*|*old-core*stop*) exit 44 ;; esac
if [ "$1" = daemon-reload ] || [ "$1" = -u ]; then
    if [ -f "$GATE_FILE" ]; then
        echo reload-gate-present >> "$EVENTS"
        for unit in "$OLD_CORE_FILE" "$OLD_DEPENDENCY_FILE" "$OLD_AGENT_FILE"; do
            if [ -f "$unit" ] && grep -q "configflow-recover-$SERVICE" "$unit"; then
                echo "dependency still present: $unit" >&2
                exit 45
            fi
        done
    else
        echo reload-gate-absent >> "$EVENTS"
    fi
fi
`

// newNativeHarness is native_install_harness(tmp_path, manager, service).
func newNativeHarness(t *testing.T, manager, service string) nativeHarness {
	tmp := t.TempDir()
	source := ok(goGenerated(func(o *GoAgentOptions) { o.ServiceType = service; o.ServiceUnit = "new-core" }))
	if runtime.GOOS == "darwin" {
		source = strings.ReplaceAll(source, "sed -i ", "sed -i '' ")
	}
	for _, pair := range [][2]string{
		{"/etc/", filepath.Join(tmp, "etc") + "/"},
		{"/usr/local/bin", filepath.Join(tmp, "local-bin")},
		{"/opt/configflow-agent", filepath.Join(tmp, "agent")},
		{"/var/log", filepath.Join(tmp, "logs")},
		{"/dev/tty", "/dev/stdin"},
		{"/run/systemd/system", filepath.Join(tmp, "run-systemd")},
		{"/proc/[0-9]*/status", filepath.Join(tmp, "agent-process")},
	} {
		source = strings.ReplaceAll(source, pair[0], pair[1])
	}
	writeFile(t, filepath.Join(tmp, "agent-process"), "PPid:\tmissing\n", 0o644)
	if manager == "systemd" {
		os.MkdirAll(filepath.Join(tmp, "run-systemd"), 0o755)
	}
	h := nativeHarness{tmp: tmp, script: writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)}
	commands := filepath.Join(tmp, "commands")
	agent := writeFile(t, filepath.Join(tmp, "fake-agent"), nativeFakeAgent, 0o755)
	mocks := map[string]string{"id": "printf '0\\n'\n", "uname": "printf 'x86_64\\n'\n", "wget": "cp \"$FAKE_AGENT\" \"$3\"\n", "sleep": "exit 0\n"}
	var agentUnit string
	if manager == "systemd" {
		mocks["systemctl"] = nativeManagerMock
		units := filepath.Join(tmp, "etc/systemd/system")
		h.gate = filepath.Join(units, "configflow-recover-"+service+".service")
		h.core = writeFile(t, filepath.Join(units, "old-core.service"), "[Unit]\nRequires=network.target configflow-recover-"+service+".service\n[Service]\nExecStart=/usr/bin/core\n", 0o644)
		h.dropin = writeFile(t, filepath.Join(units, "old-core.service.d/configflow-recovery.conf"), "[Unit]\nAfter=configflow-recover-"+service+".service \\\n local-fs.target\n[Service]\nEnvironment=KEEP=1\n", 0o644)
		agentUnit = writeFile(t, filepath.Join(units, "configflow-agent.service"), "[Unit]\nRequires=configflow-recover-"+service+".service\nAfter=network.target configflow-recover-"+service+".service\n", 0o644)
		h.agentDropin = writeFile(t, filepath.Join(units, "configflow-agent.service.d/custom.conf"), "[Unit]\nRequires=keep.service configflow-recover-other.service configflow-recover-"+service+".service\n[Service]\nEnvironment=EXTRA=1\n", 0o644)
		writeFile(t, filepath.Join(units, "new-core.service.d/configflow-recovery.conf"), "[Unit]\nRequires=configflow-recover-"+service+".service\nAfter=configflow-recover-"+service+".service\n", 0o644)
	} else {
		mocks["rc-service"] = nativeManagerMock
		mocks["rc-update"] = nativeManagerMock
		units := filepath.Join(tmp, "etc/init.d")
		conf := filepath.Join(tmp, "etc/conf.d")
		h.gate = filepath.Join(units, "configflow-recover-"+service)
		h.core = writeFile(t, filepath.Join(units, "old-core"), "#!/sbin/openrc-run\ndepend() {\n need localmount configflow-recover-"+service+"\n}\ncommand=\"/usr/bin/core\"\n", 0o644)
		h.dropin = writeFile(t, filepath.Join(conf, "old-core"), "CUSTOM=yes\n# BEGIN CONFIGFLOW RECOVERY "+service+"\nrc_need=\"${rc_need:-} configflow-recover-"+service+"\"\n# END CONFIGFLOW RECOVERY "+service+"\n", 0o644)
		agentUnit = writeFile(t, filepath.Join(units, "configflow-agent"), "#!/sbin/openrc-run\ndepend() {\n need net configflow-recover-"+service+"\n}\n", 0o644)
		h.agentDropin = writeFile(t, filepath.Join(conf, "configflow-agent"), "rc_need=\"keep configflow-recover-other configflow-recover-"+service+"\"\nCUSTOM=yes\n", 0o644)
	}
	writeFile(t, h.gate, "old recovery service\n", 0o644)
	for name, body := range mocks {
		writeFile(t, filepath.Join(commands, name), "#!/bin/sh\nset -eu\n"+body, 0o755)
	}
	h.config = writeFile(t, filepath.Join(tmp, "agent", "config-"+service+".json"),
		`{"service_unit": "old-core.service", "service_type": "`+service+`", "agent_name": "native-test", "agent_id": "existing-id", "token": "existing-token"}`, 0o644)
	h.env = append(os.Environ(), "PATH="+commands+":"+os.Getenv("PATH"), "EVENTS="+filepath.Join(tmp, "events"), "FAKE_AGENT="+agent,
		"GATE_FILE="+h.gate, "OLD_CORE_FILE="+h.core, "OLD_DEPENDENCY_FILE="+h.dropin, "OLD_AGENT_FILE="+agentUnit, "SERVICE="+service)
	return h
}

func TestNativeReinstallSafelyDetachesOldGateAndIsRepeatable(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
			h := newNativeHarness(t, manager, service)
			for i := 0; i < 2; i++ {
				r := h.run(t)
				if r.code != 0 {
					t.Fatalf("%s: %s", service, r.stderr)
				}
				if exists(h.gate) {
					t.Error("gate kept")
				}
				for _, file := range []string{h.core, h.dropin, h.agentDropin} {
					notContains(t, readFile(t, file), "configflow-recover-"+service)
				}
				core := readFile(t, h.core)
				contains(t, core, "/usr/bin/core")
				if manager == "openrc" {
					contains(t, core, "localmount")
					contains(t, readFile(t, h.dropin), "CUSTOM=yes")
				} else {
					contains(t, core, "network.target")
					contains(t, readFile(t, h.dropin), "Environment=KEEP=1")
					if exists(filepath.Join(filepath.Dir(h.gate), "new-core.service.d/configflow-recovery.conf")) {
						t.Error("new-core drop-in kept")
					}
				}
				contains(t, readFile(t, h.agentDropin), "keep")
				contains(t, readFile(t, h.agentDropin), "configflow-recover-other")
				var cfg map[string]any
				json.Unmarshal([]byte(readFile(t, h.config)), &cfg)
				if cfg["agent_id"] != "existing-id" || cfg["token"] != "existing-token" {
					t.Error("identity lost")
				}
			}
			events := h.events()
			present := indexOf(events, func(l string) bool { return l == "reload-gate-present" })
			absent := indexOf(events, func(l string) bool { return l == "reload-gate-absent" })
			if present < 0 || absent < 0 || present > absent {
				t.Errorf("gate reload order %d %d", present, absent)
			}
			if anyLine(events, func(l string) bool {
				return strings.Contains(l, "stop") && (strings.Contains(l, "configflow-recover-"+service) || strings.Contains(l, "old-core"))
			}) || !anyLine(events, func(l string) bool { return strings.Contains(l, "-check-deployments") }) {
				t.Error("events")
			}
			validation := indexOf(events, func(l string) bool { return strings.Contains(l, "-validate-service") })
			environment := indexOf(events, func(l string) bool { return strings.Contains(l, "-check-environment") })
			stop := indexOf(events, func(l string) bool { return strings.Contains(l, "stop") && strings.Contains(l, "configflow-agent") })
			if !(validation < environment && environment < stop) {
				t.Errorf("order %d %d %d", validation, environment, stop)
			}
			checks := 0
			for _, l := range events {
				if strings.Contains(l, "-check-environment") {
					checks++
				}
				if strings.Contains(l, " -recover-") {
					t.Error(l)
				}
			}
			if checks != 2 {
				t.Errorf("checks %d", checks)
			}
		}
	})
}

func TestDockerGeneratedVolumesPersistIdentityAndTransactionRoots(t *testing.T) {
	o := NewDockerOptions("https://config.test")
	o.EnableMosdns = true
	compose, err := yamlx.SafeLoad(ok(GenerateDockerAgentCompose(o)))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, v := range jsonx.Path(compose, "services", "agent", "volumes").([]any) {
		lines = append(lines, v.(string))
	}
	text := strings.Join(lines, "\n")
	for _, volume := range []string{"./agent_data/agent:/opt/configflow-agent", "./agent_data/mihomo:/etc/mihomo", "./agent_data/mosdns:/etc/mosdns"} {
		contains(t, text+"\n", volume+"\n")
	}
	command := ok(GenerateDockerAgentRun(o))
	contains(t, command, "./agent_data/agent:/opt/configflow-agent")
	contains(t, command, "./agent_data/mihomo:/etc/mihomo")
	notContains(t, command, "/root/.config/mihomo")
}

func TestOxidnsInvalidWorkingDirectoryLeavesExistingAgentRunning(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newNativeHarness(t, manager, "oxidns")
		config, core := readFile(t, h.config), readFile(t, h.core)
		r := h.run(t, "FAIL_VALIDATION=yes")
		if r.code != 46 || !exists(h.gate) || readFile(t, h.config) != config || readFile(t, h.core) != core {
			t.Fatalf("exit %d %s", r.code, r.stderr)
		}
		events := h.events()
		if !anyLine(events, func(l string) bool { return strings.Contains(l, "-validate-service") }) ||
			anyLine(events, func(l string) bool { return strings.Contains(l, "stop") || strings.Contains(l, "reload") }) {
			t.Errorf("events %v", events)
		}
	})
}

func TestNativeEnvironmentFailureKeepsExistingAgentAndCore(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, service := range []string{"mihomo", "mosdns", "oxidns"} {
			h := newNativeHarness(t, manager, service)
			before := map[string]string{}
			for _, path := range []string{h.config, h.core, h.dropin, h.agentDropin, h.gate} {
				before[path] = readFile(t, path)
			}
			r := h.run(t, "FAIL_ENVIRONMENT=yes")
			if r.code != 48 {
				t.Fatalf("exit %d %s", r.code, r.stderr)
			}
			for _, needle := range []string{"端口", "修复建议", "未知 -check-environment 参数", "1.8.19-go 或更高"} {
				contains(t, r.stderr, needle)
			}
			sameFiles(t, before)
			events := h.events()
			validation := indexOf(events, func(l string) bool { return strings.Contains(l, "-validate-service") })
			var checks []int
			for i, l := range events {
				if strings.Contains(l, "-check-environment") {
					checks = append(checks, i)
				}
			}
			if len(checks) != 1 || validation > checks[0] ||
				anyLine(events, func(l string) bool {
					return strings.Contains(l, "stop") || strings.Contains(l, "start") || strings.Contains(l, "reload")
				}) ||
				globAny(filepath.Join(h.tmp, "local-bin/.configflow-agent.install.*")) {
				t.Errorf("events %v", events)
			}
		}
	})
}

func TestNativeReinstallPreservesUnaffectedVendorUnitSymlink(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newNativeHarness(t, manager, "oxidns")
		original := "#!/sbin/openrc-run\ncommand=\"/usr/bin/core\""
		if manager == "systemd" {
			original = "[Unit]\nAfter=network.target\n[Service]\nExecStart=/usr/bin/core"
		}
		vendor := writeFile(t, filepath.Join(h.tmp, "vendor-unit"), original, 0o644)
		os.Remove(h.core)
		os.Symlink(vendor, h.core)
		info, _ := os.Lstat(h.core)
		inode := inodeOf(info)
		for i := 0; i < 2; i++ {
			if r := h.run(t); r.code != 0 {
				t.Fatal(r.stderr)
			}
			now, err := os.Lstat(h.core)
			target, _ := os.Readlink(h.core)
			if err != nil || now.Mode()&os.ModeSymlink == 0 || inodeOf(now) != inode || target != vendor || readFile(t, vendor) != original || exists(h.gate) {
				t.Fatal("vendor unit rewritten")
			}
		}
	})
}

func TestNativeReinstallRefusesToRewriteVendorDependencySymlink(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newNativeHarness(t, manager, "oxidns")
		original := readFile(t, h.core)
		vendor := writeFile(t, filepath.Join(h.tmp, "vendor-unit"), original, 0o644)
		os.Remove(h.core)
		os.Symlink(vendor, h.core)
		r := h.run(t)
		if r.code == 0 {
			t.Fatal("accepted")
		}
		contains(t, r.stderr, "恢复依赖位于符号链接服务文件")
		target, _ := os.Readlink(h.core)
		if target != vendor || readFile(t, vendor) != original || !exists(h.gate) {
			t.Error("changed")
		}
	})
}

func inodeOf(info os.FileInfo) uint64 { return info.Sys().(*syscall.Stat_t).Ino }
