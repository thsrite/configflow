package installer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"configflow/internal/jsonx"
)

// Port of backend/test_agent_native_aio_install.py: the native cohost
// installer with isolated files and mocked services.

func aioGenerated(t *testing.T, mutate func(*AIOOptions)) (string, error) {
	o := NewAIOOptions("https://config.test", "cohost")
	o.BinaryDownloadURL = "https://config.test/download"
	if mutate != nil {
		mutate(&o)
	}
	return GenerateGoAIOInstallScript(o)
}

// aioConfigs is configs(script): the two heredoc JSON configs.
func aioConfigs(t *testing.T, script string) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, service := range []string{"mihomo", "oxidns"} {
		marker := "CONFIGFLOW_" + strings.ToUpper(service) + "_JSON_EOF"
		_, rest, ok := strings.Cut(script, "<<'"+marker+"'\n")
		if !ok {
			t.Fatalf("no %s heredoc", service)
		}
		body, _, _ := strings.Cut(rest, "\n"+marker)
		var cfg map[string]any
		if err := json.Unmarshal([]byte(body), &cfg); err != nil {
			t.Fatal(err)
		}
		out[service] = cfg
	}
	return out
}

func TestNativeAIORejectsConflictingOrInvalidOptions(t *testing.T) {
	// manage_ipv4_forwarding/manage_tun None or 1 and float ports are Python
	// type errors that Go's types rule out.
	for i, mutate := range []func(*AIOOptions){
		func(o *AIOOptions) { o.MihomoAgentPort = 0 }, func(o *AIOOptions) { o.OxidnsAgentPort = 65536 },
		func(o *AIOOptions) { o.MihomoAgentPort = true }, func(o *AIOOptions) { o.MihomoAgentPort = "8080.5" },
		func(o *AIOOptions) { o.OxidnsAgentPort = 8080 },
		func(o *AIOOptions) { o.MihomoConfigPath = "relative/config.yaml" }, func(o *AIOOptions) { o.MihomoConfigPath = "/" },
		func(o *AIOOptions) { o.MihomoConfigPath = "/etc/../oxidns/config.yaml" },
		func(o *AIOOptions) { o.MihomoConfigPath = "/etc/oxidns/config.yaml" },
		func(o *AIOOptions) { o.MihomoConfigPath = "/etc/oxidns/other.yaml" },
		func(o *AIOOptions) { o.MihomoServiceUnit = "oxidns.service" }, func(o *AIOOptions) { o.MihomoServiceUnit = "configflow-agent" },
		func(o *AIOOptions) { o.MihomoConfigPath = "/opt/configflow-agent/mihomo/core.yaml" },
		func(o *AIOOptions) { o.OxidnsConfigPath = "//opt/configflow-agent/oxidns/core.yaml" },
		func(o *AIOOptions) { o.MihomoServiceBinary = "/opt/configflow-agent/mihomo/core" },
		func(o *AIOOptions) { o.OxidnsServiceBinary = "/usr/../opt/configflow-agent/oxidns/core" },
	} {
		if _, err := aioGenerated(t, mutate); err == nil {
			t.Errorf("case %d accepted", i)
		} else if _, ok := err.(*ValueError); !ok {
			t.Errorf("case %d: %T", i, err)
		}
	}
}

func TestNativeAIOConfigAndQuotedValues(t *testing.T) {
	literal := `/etc/mihomo/custom ${HOME} "quoted".yaml`
	script := ok(aioGenerated(t, func(o *AIOOptions) { o.MihomoConfigPath = literal; o.MihomoServiceUnit = "mihomo-custom.service" }))
	cfgs := aioConfigs(t, script)
	if cfgs["mihomo"]["config_path"] != literal || cfgs["mihomo"]["service_unit"] != "mihomo-custom" {
		t.Fatal(cfgs["mihomo"])
	}
	for service, port := range map[string]float64{"mihomo": 8080, "oxidns": 8082} {
		cfg := cfgs[service]
		if cfg["agent_port"] != port || cfg["agent_name"] != "cohost-"+service || cfg["native_aio"] != true ||
			cfg["agent_service_unit"] != "configflow-agent-"+service || cfg["service_manager"] != "__CONFIGFLOW_INIT__" {
			t.Errorf("%s: %v", service, cfg)
		}
	}
	path := writeFile(t, filepath.Join(t.TempDir(), "install.sh"), script, 0o644)
	if r := runShell(t, "sh", "-n", runOptions{args: []string{path}}); r.code != 0 {
		t.Fatal(r.stderr)
	}
}

func TestNativeAIOAllowsCorePathsWithSimilarDirectoryPrefix(t *testing.T) {
	cfg := aioConfigs(t, ok(aioGenerated(t, func(o *AIOOptions) {
		o.MihomoConfigPath = "/opt/configflow-agent-core/mihomo/config.yaml"
		o.MihomoServiceBinary = "/opt/configflow-agent-core/mihomo/core"
	})))["mihomo"]
	if cfg["config_path"] != "/opt/configflow-agent-core/mihomo/config.yaml" {
		t.Fatal(cfg)
	}
}

const (
	forwardMarker = "# Managed by ConfigFlow Shell AIO IPv4 forwarding v1"
	forwardConfig = "etc/sysctl.d/99-configflow-aio-forward.conf"
	forwardHelper = "agent/mihomo/enable-ipv4-forwarding.sh"
)

type aioHarness struct {
	tmp, installer, units string
	env                   []string
}

func (h aioHarness) path(rel string) string { return filepath.Join(h.tmp, rel) }

func (h aioHarness) run(t *testing.T, choice string, extra ...string) runResult {
	return runShell(t, "sh", h.installer, runOptions{env: append(append([]string{}, h.env...), extra...), replace: true,
		input: choice + "\n", timeout: 30e9})
}

func (h aioHarness) events(t *testing.T) []string {
	data, err := os.ReadFile(h.path("events"))
	if err != nil {
		return nil
	}
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func (h aioHarness) eventsText() string {
	data, _ := os.ReadFile(h.path("events"))
	return string(data)
}

func fakeAgentSource(python string) string {
	return "#!" + python + ` -S
import json, os, sys
args = sys.argv[1:]
if args in (['-h'], ['-version']):
    raise SystemExit(0)
cfg = json.load(open(args[args.index('-config') + 1]))
with open(os.environ['EVENTS'], 'a') as log:
    log.write('agent ' + cfg['service_type'] + ' ' + args[-1] + ' ' + args[1] + '\n')
if '-prepare-tun' in args:
    assert cfg['service_type'] == 'mihomo', 'Only the Mihomo candidate prepares TUN'
    candidate = args[args.index('-config') + 1]
    assert '.aio-install.' in candidate and candidate.endswith('/mihomo-config.json')
    with open(os.environ['EVENTS'], 'a') as log:
        log.write('prepare-tun:' + cfg['service_type'] + ':' + candidate + '\n')
    if os.environ.get('TUN_PREPARE_FAIL') == 'yes':
        print('TUN 自动准备失败：请检查容器设备和 CAP_NET_ADMIN 权限。', file=sys.stderr)
        raise SystemExit(51)
    raise SystemExit(0)
if '-check-environment' in args:
    if os.environ.get('VERBOSE_ENVIRONMENT') == 'yes':
        print('verbose-environment-detail-for-log-only')
    assert '-environment-peer-config' in args, 'AIO must inspect both services together'
    peer_path = args[args.index('-environment-peer-config') + 1]
    peer = json.load(open(peer_path))
    assert peer['service_type'] == 'oxidns'
    assert peer['service_manager'] in ('systemd', 'openrc')
    assert cfg['service_manager'] == peer['service_manager']
    assert cfg['agent_port'] != peer['agent_port']
    previous = open(os.environ['EVENTS']).read()
    assert 'agent mihomo -validate-service' in previous
    assert 'agent oxidns -validate-service' in previous
    with open(os.environ['EVENTS'], 'a') as log:
        log.write('environment peer oxidns ' + peer_path + '\n')
    if os.environ.get('FAIL_ENVIRONMENT') == 'yes':
        print('IPv4 转发未开启；修复建议：确认网关用途后配置持久化转发。', file=sys.stderr)
        raise SystemExit(48)
if cfg['service_type'] == os.environ.get('FAIL_VALIDATION') and '-validate-service' in args:
    raise SystemExit(46)
if cfg['service_type'] == os.environ.get('FAIL_JOURNAL') and '-require-clean-deployments' in args:
    raise SystemExit(47)
if cfg['service_type'] == os.environ.get('FAIL_JOURNAL_AFTER_STOP') and '-require-clean-deployments' in args:
    if 'stop' in open(os.environ['EVENTS']).read():
        raise SystemExit(47)
`
}

const aioManagerMock = `printf '%s %s\n' "${0##*/}" "$*" >> "$EVENTS"
case "$*" in
    *--property=LoadState*) printf 'loaded\n' ;;
esac
case "${0##*/}:$*" in
    systemctl:"start configflow-agent-"*|rc-service:configflow-agent-*" start")
        if [ "${0##*/}" = systemctl ]; then service="$2"; else service="$1"; fi
        [ "${FAIL_AGENT_START:-}" != "$service" ] || exit 49
        if [ "$service" = configflow-agent-mihomo ] && [ -f "$FORWARD_HELPER" ]; then
            /bin/sh "$FORWARD_HELPER"
        fi
        if [ "$service" = configflow-agent-oxidns ] && [ "${BLOCK_FORWARD_PERSIST:-}" = yes ]; then
            mkdir -p "$FORWARD_CONFIG"
        fi
        if [ "$service" = configflow-agent-oxidns ] && [ "${RACE_GATEWAY_RECORD:-}" = start ]; then
            printf '{"owner":"gateway"}\n' > "$GATEWAY_RECORD"
        fi
        ;;
esac
case "${0##*/}:$*" in
    systemctl:"stop configflow-agent-mihomo"|rc-service:"configflow-agent-mihomo stop")
        if [ "${RACE_GATEWAY_RECORD:-}" = stop ]; then
            printf '{"owner":"gateway"}\n' > "$GATEWAY_RECORD"
        fi
        ;;
esac
`

func sysctlMock(python string) string {
	return "#!" + python + ` -S
import os, sys
from pathlib import Path
args = sys.argv[1:]
with open(os.environ['EVENTS'], 'a') as log:
    log.write('sysctl ' + ' '.join(args) + '\n')
current = Path(os.environ['FORWARD_PROC'])
if args in (['-n', 'net.ipv4.ip_forward'], ['net.ipv4.ip_forward']):
    value = current.read_text().strip()
    print(value if '-n' in args else 'net.ipv4.ip_forward = ' + value)
    raise SystemExit(0)
if args not in (['-q', '-w', 'net.ipv4.ip_forward=0'], ['-q', '-w', 'net.ipv4.ip_forward=1']):
    raise SystemExit('Unexpected sysctl command in isolated installer test: ' + repr(args))
value = args[-1].rsplit('=', 1)[1]
if os.environ.get('FAIL_SYSCTL_WRITES') == 'yes' or (value == '1' and os.environ.get('FAIL_SYSCTL_ENABLE') == 'yes'):
    print('sysctl: permission denied on key net.ipv4.ip_forward', file=sys.stderr)
    raise SystemExit(50)
if os.environ.get('SYSCTL_WRITE_NO_EFFECT') != 'yes':
    current.write_text(value + '\n')
if value == '1' and os.environ.get('DELETE_FORWARD_CONFIG_ON_ENABLE') == 'yes':
    Path(os.environ['FORWARD_CONFIG']).unlink(missing_ok=True)
`
}

// newAIOHarness is harness(tmp_path, manager, existing, **options).
func newAIOHarness(t *testing.T, manager string, existing bool, mutate func(*AIOOptions)) aioHarness {
	return newAIOHarnessSource(t, manager, existing, ok(aioGenerated(t, mutate)))
}

// newAIOHarnessSource is harness() with aio.generated patched to return source.
func newAIOHarnessSource(t *testing.T, manager string, existing bool, source string) aioHarness {
	tmp := t.TempDir()
	python := pythonExe(t)
	if runtime.GOOS == "darwin" {
		source = strings.ReplaceAll(source, "sed -i ", "sed -i '' ")
	}
	for _, pair := range [][2]string{
		{"/etc/", filepath.Join(tmp, "etc") + "/"},
		{"/usr/local/bin", filepath.Join(tmp, "local-bin")},
		{"/opt/configflow-agent", filepath.Join(tmp, "agent")},
		{"/var/log", filepath.Join(tmp, "logs")},
		{"/dev/tty", "/dev/stdin"},
		{"/proc/sys/net/ipv4/ip_forward", filepath.Join(tmp, "ip_forward")},
		{"/run/systemd/system", filepath.Join(tmp, "run-systemd")},
		{"/proc/[0-9]*/status", filepath.Join(tmp, "agent-process")},
	} {
		source = strings.ReplaceAll(source, pair[0], pair[1])
	}
	writeFile(t, filepath.Join(tmp, "agent-process"), "PPid:\tmissing\n", 0o644)
	if manager == "systemd" {
		os.MkdirAll(filepath.Join(tmp, "run-systemd"), 0o755)
	}
	installer := writeFile(t, filepath.Join(tmp, "install.sh"), source, 0o644)
	commands := filepath.Join(tmp, "commands")
	writeFile(t, filepath.Join(tmp, "ip_forward"), "0\n", 0o644)
	agentSource := fakeAgentSource(python)
	agent := writeFile(t, filepath.Join(tmp, "fake-agent"), agentSource, 0o755)
	mocks := map[string]string{
		"id": "printf '0\\n'\n", "uname": "printf 'x86_64\\n'\n",
		"wget": "cp \"$FAKE_AGENT\" \"$3\"\n", "mihomo": "exit 0\n", "oxidns": "exit 0\n",
		"sysctl":   sysctlMock(python),
		"modprobe": "printf \"forbidden-modprobe %s\\n\" \"$*\" >> \"$EVENTS\"\nexit 97\n",
		"mknod":    "printf \"forbidden-mknod %s\\n\" \"$*\" >> \"$EVENTS\"\nexit 97\n",
		"sleep":    "exit 0\n",
	}
	var units string
	if manager == "systemd" {
		mocks["systemctl"] = aioManagerMock
		units = filepath.Join(tmp, "etc/systemd/system")
	} else {
		mocks["rc-service"] = aioManagerMock
		mocks["rc-update"] = aioManagerMock
		units = filepath.Join(tmp, "etc/init.d")
	}
	os.MkdirAll(units, 0o755)
	for name, body := range mocks {
		if !strings.HasPrefix(body, "#!") {
			body = "#!/bin/sh\nset -eu\n" + body
		}
		writeFile(t, filepath.Join(commands, name), body, 0o755)
	}
	suffix := ""
	if manager == "systemd" {
		suffix = ".service"
	}
	cfgs := aioConfigs(t, source)
	for _, service := range []string{"mihomo", "oxidns"} {
		writeFile(t, cfgs[service]["config_path"].(string), "# unchanged core config\n", 0o644)
		writeFile(t, filepath.Join(units, service+suffix), "# original core unit\n", 0o644)
		if existing {
			directory := filepath.Join(tmp, "agent", service)
			cfg := map[string]any{}
			for k, v := range cfgs[service] {
				cfg[k] = v
			}
			cfg["agent_id"], cfg["token"] = "id-"+service, "token-"+service
			data, _ := json.Marshal(cfg)
			writeFile(t, filepath.Join(directory, "config.json"), string(data), 0o644)
			writeFile(t, filepath.Join(directory, "configflow-agent"), agentSource, 0o755)
			writeFile(t, filepath.Join(directory, "keep-state"), service, 0o644)
			writeFile(t, filepath.Join(units, "configflow-agent-"+service+suffix), "# old Agent unit\n", 0o644)
		}
	}
	env := append(os.Environ(), "PATH="+commands+":"+os.Getenv("PATH"),
		"EVENTS="+filepath.Join(tmp, "events"), "FAKE_AGENT="+agent, "CONFIGFLOW_INSTALL_LOG_DIR="+tmp,
		"FORWARD_PROC="+filepath.Join(tmp, "ip_forward"), "FORWARD_CONFIG="+filepath.Join(tmp, forwardConfig),
		"FORWARD_HELPER="+filepath.Join(tmp, forwardHelper),
		"GATEWAY_RECORD="+filepath.Join(tmp, "agent/mihomo/linux-gateway.json"))
	return aioHarness{tmp: tmp, installer: installer, units: units, env: env}
}

// detailOutput is detail_output(result): the full log named on stdout.
func detailOutput(t *testing.T, r runResult) string {
	for _, line := range strings.Split(r.stdout, "\n") {
		if strings.HasPrefix(line, "完整日志：") {
			return readFile(t, strings.TrimPrefix(line, "完整日志："))
		}
	}
	t.Fatal("no log path")
	return ""
}

// snapshotFiles maps every regular file under the directories to its bytes.
func snapshotFiles(t *testing.T, dirs ...string) map[string]string {
	out := map[string]string{}
	for _, dir := range dirs {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err == nil && info.Mode().IsRegular() {
				out[path] = readFile(t, path)
			}
			return nil
		})
	}
	return out
}

func sameFiles(t *testing.T, before map[string]string) {
	t.Helper()
	for path, data := range before {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Errorf("%s changed", path)
		}
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func globAny(pattern string) bool {
	matches, _ := filepath.Glob(pattern)
	return len(matches) > 0
}

func indexOf(lines []string, match func(string) bool) int {
	for i, line := range lines {
		if match(line) {
			return i
		}
	}
	return -1
}

func anyLine(lines []string, match func(string) bool) bool { return indexOf(lines, match) >= 0 }

func lifecycleLine(line string) bool {
	return strings.Contains(line, "stop") || strings.Contains(line, "start") || strings.Contains(line, "daemon-reload")
}

func managers(t *testing.T, f func(t *testing.T, manager string)) {
	for _, manager := range []string{"systemd", "openrc"} {
		t.Run(manager, func(t *testing.T) { f(t, manager) })
	}
}

func agentEvent(manager, service, action string) string {
	if manager == "systemd" {
		return "systemctl " + action + " configflow-agent-" + service
	}
	return "rc-service configflow-agent-" + service + " " + action
}

func TestSecondCorePreflightFailureLeavesBothAgentsUntouched(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, failure := range []string{"FAIL_VALIDATION", "FAIL_JOURNAL"} {
			h := newAIOHarness(t, manager, true, nil)
			before := snapshotFiles(t, h.path("agent"), h.units)
			r := h.run(t, "1", failure+"=oxidns")
			if r.code != 46 && r.code != 47 {
				t.Fatalf("%s: exit %d %s", failure, r.code, r.stderr)
			}
			sameFiles(t, before)
			events := h.events(t)
			if !anyLine(events, func(l string) bool { return strings.Contains(l, "agent mihomo -validate-service") }) ||
				anyLine(events, lifecycleLine) || globAny(h.path("agent/.aio-install.*")) ||
				readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) ||
				anyLine(events, func(l string) bool { return strings.HasPrefix(l, "sysctl ") || strings.HasPrefix(l, "prepare-tun:") }) {
				t.Errorf("%s: events %v", failure, events)
			}
		}
	})
}

func TestNativeAIOEnvironmentFailurePreservesBothAgentsAndPrintsGuidance(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		before := snapshotFiles(t, h.path("agent"), h.units)
		r := h.run(t, "1", "FAIL_ENVIRONMENT=yes")
		if r.code != 48 {
			t.Fatalf("exit %d %s", r.code, r.stderr)
		}
		for _, needle := range []string{"IPv4 转发", "修复建议", "未知 -check-environment 参数", "1.8.19-go 或更高"} {
			contains(t, r.stderr, needle)
		}
		sameFiles(t, before)
		for _, service := range []string{"mihomo", "oxidns"} {
			if readFile(t, h.path("etc/"+service+"/config.yaml")) != "# unchanged core config\n" {
				t.Error("core config changed")
			}
		}
		events := h.events(t)
		var checks []int
		peers := 0
		for i, line := range events {
			if strings.Contains(line, " -check-environment ") {
				checks = append(checks, i)
			}
			if strings.HasPrefix(line, "environment peer oxidns ") {
				peers++
			}
		}
		if len(checks) != 1 || peers != 1 {
			t.Fatalf("checks %v peers %d", checks, peers)
		}
		for _, service := range []string{"mihomo", "oxidns"} {
			if v := indexOf(events, func(l string) bool { return strings.Contains(l, "agent "+service+" -validate-service") }); v < 0 || v > checks[0] {
				t.Errorf("%s validation order", service)
			}
		}
		if anyLine(events, lifecycleLine) || globAny(h.path("agent/.aio-install.*")) || readFile(t, h.path("ip_forward")) != "0\n" ||
			exists(h.path(forwardConfig)) || anyLine(events, func(l string) bool { return strings.HasPrefix(l, "sysctl ") }) {
			t.Errorf("side effects: %v", events)
		}
	})
}

func TestNativeAIOInstallIsolatedIdentityAndRepeatable(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, existing := range []bool{false, true} {
			h := newAIOHarness(t, manager, existing, nil)
			for attempt := 0; attempt < 2; attempt++ {
				writeFile(t, h.path("events"), "", 0o644)
				r := h.run(t, "1")
				if r.code != 0 {
					t.Fatalf("existing=%v attempt %d: %s", existing, attempt, r.stderr)
				}
				for _, service := range []string{"mihomo", "oxidns"} {
					directory := h.path("agent/" + service)
					var cfg map[string]any
					json.Unmarshal([]byte(readFile(t, filepath.Join(directory, "config.json"))), &cfg)
					wantID, wantToken := "", ""
					if existing {
						wantID, wantToken = "id-"+service, "token-"+service
					}
					if cfg["service_manager"] != manager || cfg["agent_id"] != wantID || cfg["token"] != wantToken ||
						cfg["agent_service_unit"] != "configflow-agent-"+service || !exists(filepath.Join(directory, "configflow-agent")) ||
						readFile(t, cfg["config_path"].(string)) != "# unchanged core config\n" {
						t.Errorf("%s cfg %v", service, cfg)
					}
					if existing && readFile(t, filepath.Join(directory, "keep-state")) != service {
						t.Error("state lost")
					}
				}
				events := h.events(t)
				var endChecks, stops, envChecks, tunChecks []int
				peers := 0
				for i, line := range events {
					if strings.Contains(line, "agent oxidns -require-clean-deployments") && strings.Contains(line, ".aio-install.") {
						endChecks = append(endChecks, i)
					}
					if strings.Contains(line, "stop") && strings.Contains(line, "configflow-agent") {
						stops = append(stops, i)
					}
					if strings.Contains(line, " -check-environment ") {
						envChecks = append(envChecks, i)
					}
					if strings.HasPrefix(line, "prepare-tun:") {
						tunChecks = append(tunChecks, i)
					}
					if strings.HasPrefix(line, "environment peer oxidns ") {
						peers++
					}
				}
				if len(envChecks) != 1 || len(tunChecks) != 1 || tunChecks[0] > envChecks[0] || peers != 1 {
					t.Fatalf("checks env %v tun %v peers %d", envChecks, tunChecks, peers)
				}
				for _, service := range []string{"mihomo", "oxidns"} {
					if v := indexOf(events, func(l string) bool { return strings.Contains(l, "agent "+service+" -validate-service") }); v < 0 || v > tunChecks[0] {
						t.Errorf("%s validation order", service)
					}
					journal := 0
					for _, line := range events[:tunChecks[0]] {
						if strings.Contains(line, "agent "+service+" -require-clean-deployments") {
							journal++
						}
					}
					want := 1
					if existing || attempt > 0 {
						want = 2
					}
					if journal != want {
						t.Errorf("%s journal checks %d want %d", service, journal, want)
					}
				}
				if existing || attempt > 0 {
					if len(endChecks) == 0 || len(stops) == 0 || endChecks[0] > stops[0] || endChecks[len(endChecks)-1] < stops[len(stops)-1] || envChecks[0] > stops[0] {
						t.Errorf("order end %v stops %v", endChecks, stops)
					}
				}
				for _, line := range events {
					for _, service := range []string{"mihomo", "oxidns"} {
						if line == "systemctl stop "+service || line == "rc-service "+service+" stop" {
							t.Error("core stopped")
						}
					}
					if strings.HasPrefix(line, "forbidden-") {
						t.Error(line)
					}
				}
				if exists(h.path("agent/config-mihomo.json")) || exists(h.path("local-bin/configflow-agent")) {
					t.Error("legacy layout written")
				}
			}
		}
	})
}

func TestNativeAIOSummaryPreservesExistingCoreDNSGuidanceWithoutVerboseOutput(t *testing.T) {
	h := newAIOHarness(t, "systemd", true, nil)
	r := h.run(t, "1", "VERBOSE_ENVIRONMENT=yes")
	if r.code != 0 {
		t.Fatal(r.stdout + r.stderr)
	}
	for _, needle := range []string{"请输入选项 [1-3]: ", "检查安装环境与依赖", "检查核心接管与下载 Agent", "检查运行环境", "安装并启动 Agent 服务",
		"Mihomo Agent：端口 8080，服务已启动", "OxiDNS Agent：端口 8082，服务已启动", "完整日志："} {
		contains(t, r.stdout, needle)
	}
	contains(t, r.stdout+r.stderr, "已有核心配置已保留；发布也会保留自定义 DNS 端口和 dns-hijack")
	notContains(t, r.stdout, "先在 ConfigFlow 发布")
	contains(t, r.stdout+r.stderr, "安装完成")
	notContains(t, r.stdout+r.stderr, "verbose-environment-detail-for-log-only")
	contains(t, detailOutput(t, r), "verbose-environment-detail-for-log-only")
}

func TestNativeAIOTunPreparationFailurePreservesAgentsAndForwarding(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, initial := range []string{"0", "1"} {
			h := newAIOHarness(t, manager, true, nil)
			writeFile(t, h.path("ip_forward"), initial+"\n", 0o644)
			before := snapshotFiles(t, h.path("agent"), h.units)
			r := h.run(t, "1", "TUN_PREPARE_FAIL=yes")
			if r.code != 51 {
				t.Fatalf("exit %d %s", r.code, r.stdout+r.stderr)
			}
			contains(t, r.stderr, "TUN")
			notContains(t, r.stdout, "安装完成")
			notContains(t, r.stdout+r.stderr+detailOutput(t, r), "IPv4 转发已开启并持久化")
			sameFiles(t, before)
			events := h.events(t)
			tun := 0
			for _, l := range events {
				if strings.HasPrefix(l, "prepare-tun:mihomo:") {
					tun++
				}
			}
			if tun != 1 || anyLine(events, func(l string) bool { return strings.Contains(l, " -check-environment ") }) || anyLine(events, lifecycleLine) ||
				anyLine(events, func(l string) bool { return strings.HasPrefix(l, "sysctl ") || strings.HasPrefix(l, "forbidden-") }) {
				t.Errorf("events %v", events)
			}
			if readFile(t, h.path("ip_forward")) != initial+"\n" || exists(h.path(forwardConfig)) || exists(h.path(forwardHelper)) || globAny(h.path("agent/.aio-install.*")) {
				t.Error("side effects")
			}
		}
	})
}

func TestNativeAIOCanSkipTunPreparationWhileManagingIPv4Forwarding(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, func(o *AIOOptions) { o.ManageTun = false })
		notContains(t, readFile(t, h.installer), "-prepare-tun")
		r := h.run(t, "1", "TUN_PREPARE_FAIL=yes")
		if r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		notContains(t, h.eventsText(), "prepare-tun")
		notContains(t, h.eventsText(), "forbidden-")
		if readFile(t, h.path("ip_forward")) != "1\n" {
			t.Error("forwarding")
		}
		contains(t, readFile(t, h.path(forwardConfig)), forwardMarker)
	})
}

func TestNativeAIOUninstallPreservesOtherDirectoriesAndCores(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		sibling := writeFile(t, h.path("agent/unrelated/keep"), "keep", 0o644)
		r := h.run(t, "2")
		if r.code != 0 {
			t.Fatal(r.stderr)
		}
		if readFile(t, sibling) != "keep" {
			t.Error("sibling")
		}
		suffix := ""
		if manager == "systemd" {
			suffix = ".service"
		}
		for _, service := range []string{"mihomo", "oxidns"} {
			if exists(h.path("agent/"+service)) || readFile(t, h.path("etc/"+service+"/config.yaml")) != "# unchanged core config\n" ||
				readFile(t, filepath.Join(h.units, service+suffix)) != "# original core unit\n" {
				t.Errorf("%s not cleanly removed", service)
			}
		}
		events := h.eventsText()
		notContains(t, events, "recover-and-exit")
		notContains(t, events, "-check-environment")
		notContains(t, events, "prepare-tun")
		if strings.Index(events, "agent oxidns -require-clean-deployments") > strings.Index(events, "stop") {
			t.Error("journal check after stop")
		}
	})
}

func TestNativeAIOUninstallBlocksPendingSecondCore(t *testing.T) {
	h := newAIOHarness(t, "systemd", true, nil)
	r := h.run(t, "2", "FAIL_JOURNAL=oxidns")
	if r.code != 47 || !exists(h.path("agent/mihomo/config.json")) || !exists(h.path("agent/oxidns/config.json")) {
		t.Fatalf("exit %d", r.code)
	}
	notContains(t, h.eventsText(), "stop")
}

func TestRacingPublicationRestartsOriginalAgentsWithoutReplacingFiles(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, choice := range []string{"1", "2"} {
			h := newAIOHarness(t, manager, true, nil)
			before := snapshotFiles(t, h.path("agent"), h.units)
			r := h.run(t, choice, "FAIL_JOURNAL_AFTER_STOP=oxidns")
			if r.code != 47 {
				t.Fatalf("exit %d %s", r.code, r.stderr)
			}
			sameFiles(t, before)
			events := h.events(t)
			for _, service := range []string{"mihomo", "oxidns"} {
				stop := indexOf(events, func(l string) bool { return l == agentEvent(manager, service, "stop") })
				start := indexOf(events, func(l string) bool { return l == agentEvent(manager, service, "start") })
				if stop < 0 || start < 0 || stop > start {
					t.Errorf("%s stop %d start %d", service, stop, start)
				}
			}
			if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) ||
				anyLine(events, func(l string) bool { return l == "sysctl -q -w net.ipv4.ip_forward=1" }) {
				t.Error("forwarding touched")
			}
		}
	})
}

func TestNativeAIOEnablesAndPersistsForwardingAfterBothAgentsStart(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, initial := range []string{"0", "1"} {
			h := newAIOHarness(t, manager, true, nil)
			writeFile(t, h.path("ip_forward"), initial+"\n", 0o644)
			unrelated := writeFile(t, h.path("etc/sysctl.d/20-existing.conf"), "# administrator-owned\nnet.ipv6.conf.all.forwarding=0\n", 0o644)
			for attempt := 0; attempt < 2; attempt++ {
				writeFile(t, h.path("events"), "", 0o644)
				r := h.run(t, "1")
				if r.code != 0 {
					t.Fatal(r.stdout + r.stderr)
				}
				contains(t, r.stdout, "IPv4 转发已开启并持久化")
				contains(t, detailOutput(t, r), "IPv4 转发已开启并持久化")
				if readFile(t, h.path("ip_forward")) != "1\n" {
					t.Error("not enabled")
				}
				lines := strings.Split(readFile(t, h.path(forwardConfig)), "\n")
				markers, settings := 0, 0
				for _, l := range lines {
					if l == forwardMarker {
						markers++
					}
					if l == "net.ipv4.ip_forward=1" {
						settings++
					}
				}
				if markers != 1 || settings != 1 || readFile(t, unrelated) != "# administrator-owned\nnet.ipv6.conf.all.forwarding=0\n" {
					t.Error("persistent config")
				}
				events := h.events(t)
				environment := indexOf(events, func(l string) bool { return strings.Contains(l, " -check-environment ") })
				firstWrite := indexOf(events, func(l string) bool { return strings.HasPrefix(l, "sysctl -q -w ") })
				if firstWrite < 0 || environment > firstWrite {
					t.Error("write before environment check")
				}
				if initial == "0" && attempt == 0 {
					enable := indexOf(events, func(l string) bool { return l == "sysctl -q -w net.ipv4.ip_forward=1" })
					for _, service := range []string{"mihomo", "oxidns"} {
						if start := indexOf(events, func(l string) bool { return l == agentEvent(manager, service, "start") }); start < 0 || start > enable {
							t.Errorf("%s start after enable", service)
						}
					}
				}
				if globAny(h.path("agent/.aio-install.*")) {
					t.Error("stage left")
				}
			}
			helper := h.path(forwardHelper)
			suffix := ""
			if manager == "systemd" {
				suffix = ".service"
			}
			mihomoUnit := readFile(t, filepath.Join(h.units, "configflow-agent-mihomo"+suffix))
			oxidnsUnit := readFile(t, filepath.Join(h.units, "configflow-agent-oxidns"+suffix))
			contains(t, mihomoUnit, helper)
			if manager == "systemd" {
				contains(t, mihomoUnit, "ExecStartPre=/bin/sh ")
			} else {
				contains(t, mihomoUnit, "start_pre()")
			}
			notContains(t, oxidnsUnit, "enable-ipv4-forwarding.sh")
			writeFile(t, h.path("ip_forward"), "0\n", 0o644)
			writeFile(t, h.path("events"), "", 0o644)
			if r := runShell(t, "/bin/sh", helper, runOptions{env: h.env, replace: true}); r.code != 0 {
				t.Fatal(r.stdout + r.stderr)
			}
			if readFile(t, h.path("ip_forward")) != "1\n" {
				t.Error("hook did not enable")
			}
			contains(t, h.eventsText(), "sysctl -q -w net.ipv4.ip_forward=1")
			writeFile(t, h.path("events"), "", 0o644)
			if r := runShell(t, "/bin/sh", helper, runOptions{env: h.env, replace: true}); r.code != 0 {
				t.Fatal(r.stderr)
			}
			notContains(t, h.eventsText(), " -w ")
		}
	})
}

// failedForwarding asserts the common "no success, nothing enabled" outcome.
func failedForwarding(t *testing.T, h aioHarness, r runResult) {
	t.Helper()
	if r.code == 0 {
		t.Fatal("installer succeeded")
	}
	notContains(t, r.stdout, "安装完成")
	notContains(t, r.stdout+r.stderr+detailOutput(t, r), "IPv4 转发已开启并持久化")
}

func TestForwardingPermissionProbeFailureLeavesServicesAndForwardingUntouched(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		before := snapshotFiles(t, h.path("agent"), h.units)
		r := h.run(t, "1", "FAIL_SYSCTL_WRITES=yes")
		failedForwarding(t, h, r)
		if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) {
			t.Error("forwarding")
		}
		sameFiles(t, before)
		events := h.events(t)
		if !anyLine(events, func(l string) bool { return l == "sysctl -q -w net.ipv4.ip_forward=0" }) || anyLine(events, lifecycleLine) {
			t.Errorf("events %v", events)
		}
	})
}

func TestForwardingPrepareRejectsBlockedPersistentDirectoryWithoutEnabling(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		blocked := writeFile(t, h.path("etc/sysctl.d"), "not a directory\n", 0o644)
		r := h.run(t, "1")
		failedForwarding(t, h, r)
		if readFile(t, blocked) != "not a directory\n" || readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("changed")
		}
		events := h.eventsText()
		notContains(t, events, "sysctl -q -w net.ipv4.ip_forward=1")
		notContains(t, events, " stop ")
		notContains(t, events, " start ")
	})
}

func TestAgentStartFailureDoesNotCommitForwardingAndCanRetry(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		r := h.run(t, "1", "FAIL_AGENT_START=configflow-agent-oxidns")
		failedForwarding(t, h, r)
		if exists(h.path(forwardConfig)) || readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("committed")
		}
		notContains(t, h.eventsText(), "sysctl -q -w net.ipv4.ip_forward=1")
		helper := h.path(forwardHelper)
		if !exists(helper) {
			t.Fatal("hook missing")
		}
		if s := runShell(t, "/bin/sh", helper, runOptions{env: h.env, replace: true}); s.code != 0 {
			t.Fatal(s.stderr)
		}
		if readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("hook enabled before commit")
		}
		if retried := h.run(t, "1"); retried.code != 0 {
			t.Fatal(retried.stdout + retried.stderr)
		}
		if readFile(t, h.path("ip_forward")) != "1\n" {
			t.Error("retry")
		}
		contains(t, readFile(t, h.path(forwardConfig)), forwardMarker)
	})
}

func TestPersistentWriteRaceDoesNotReportSuccessOrEnableForwarding(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		r := h.run(t, "1", "BLOCK_FORWARD_PERSIST=yes")
		failedForwarding(t, h, r)
		if readFile(t, h.path("ip_forward")) != "0\n" {
			t.Error("enabled")
		}
		if info, err := os.Stat(h.path(forwardConfig)); err == nil && info.Mode().IsRegular() {
			t.Error("config written")
		}
	})
}

func TestForwardingFinalEnableFailureOrFailedReadbackCannotReportSuccess(t *testing.T) {
	for _, failure := range []string{"FAIL_SYSCTL_ENABLE", "SYSCTL_WRITE_NO_EFFECT"} {
		h := newAIOHarness(t, "systemd", true, nil)
		r := h.run(t, "1", failure+"=yes")
		failedForwarding(t, h, r)
		if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) ||
			globAny(h.path("etc/sysctl.d/.configflow-aio-forward.*")) || globAny(h.path("agent/.aio-install.*")) {
			t.Errorf("%s: side effects", failure)
		}
		contains(t, h.eventsText(), "sysctl -q -w net.ipv4.ip_forward=1")
	}
}

func TestForwardingConfigRemovedDuringEnableDoesNotReportSuccessOrDisableRuntime(t *testing.T) {
	h := newAIOHarness(t, "systemd", true, nil)
	r := h.run(t, "1", "DELETE_FORWARD_CONFIG_ON_ENABLE=yes")
	failedForwarding(t, h, r)
	if exists(h.path(forwardConfig)) || readFile(t, h.path("ip_forward")) != "1\n" {
		t.Error("state")
	}
	var writes []string
	for _, l := range h.events(t) {
		if strings.HasPrefix(l, "sysctl -q -w ") {
			writes = append(writes, l)
		}
	}
	if strings.Join(writes, "|") != "sysctl -q -w net.ipv4.ip_forward=0|sysctl -q -w net.ipv4.ip_forward=1" {
		t.Errorf("writes %v", writes)
	}
	if globAny(h.path("etc/sysctl.d/.configflow-aio-forward.*")) {
		t.Error("temp left")
	}
}

func TestForwardingConfigRemovedBeforeNoopHelperCannotReportSuccess(t *testing.T) {
	h := newAIOHarness(t, "systemd", true, nil)
	writeFile(t, h.path("ip_forward"), "1\n", 0o644)
	ln, err := exec.LookPath("ln")
	if err != nil {
		t.Fatal(err)
	}
	rm, _ := exec.LookPath("rm")
	writeFile(t, h.path("commands/ln"), "#!/bin/sh\nset -eu\n"+ln+" \"$@\"\n"+rm+" -f \"$FORWARD_CONFIG\"\n", 0o755)
	r := h.run(t, "1")
	failedForwarding(t, h, r)
	if exists(h.path(forwardConfig)) || readFile(t, h.path("ip_forward")) != "1\n" || globAny(h.path("etc/sysctl.d/.configflow-aio-forward.*")) {
		t.Error("state")
	}
}

func TestNativeAIOUninstallPreservesCommittedForwardingForRunningCores(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		if r := h.run(t, "1"); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		persistent := readFile(t, h.path(forwardConfig))
		writeFile(t, h.path("events"), "", 0o644)
		if r := h.run(t, "2"); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if readFile(t, h.path("ip_forward")) != "1\n" || readFile(t, h.path(forwardConfig)) != persistent {
			t.Error("forwarding removed")
		}
		notContains(t, h.eventsText(), "sysctl ")
		notContains(t, h.eventsText(), "prepare-tun")
	})
}

func TestNativeAIOCancelDoesNotPrepareOrEnableForwarding(t *testing.T) {
	h := newAIOHarness(t, "systemd", true, nil)
	record := writeFile(t, h.path("agent/mihomo/linux-gateway.json"), "{\"owner\":\"gateway\"}\n", 0o644)
	if r := h.run(t, "3"); r.code != 0 {
		t.Fatal(r.stderr)
	}
	if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) || exists(h.path("events")) ||
		readFile(t, record) != "{\"owner\":\"gateway\"}\n" {
		t.Error("side effects")
	}
}

func TestNativeAIOInstallAndUninstallRefuseExistingGatewayRecord(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, choice := range []string{"1", "2"} {
			for _, kind := range []string{"file", "symlink"} {
				h := newAIOHarness(t, manager, true, nil)
				record := h.path("agent/mihomo/linux-gateway.json")
				if kind == "symlink" {
					os.Symlink(h.path("missing-gateway-record.json"), record)
				} else {
					writeFile(t, record, "{\"owner\":\"gateway\"}\n", 0o644)
				}
				r := h.run(t, choice)
				if r.code == 0 {
					t.Fatalf("%s %s accepted", choice, kind)
				}
				contains(t, r.stderr, "网关")
				if kind == "symlink" {
					if info, err := os.Lstat(record); err != nil || info.Mode()&os.ModeSymlink == 0 {
						t.Error("symlink replaced")
					}
				} else if readFile(t, record) != "{\"owner\":\"gateway\"}\n" {
					t.Error("record changed")
				}
				for _, service := range []string{"mihomo", "oxidns"} {
					if !exists(h.path("agent/" + service + "/config.json")) {
						t.Error("agent removed")
					}
				}
				if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) ||
					anyLine(h.events(t), func(l string) bool {
						return strings.Contains(l, "stop") || strings.Contains(l, "start") || strings.HasPrefix(l, "sysctl ")
					}) {
					t.Error("side effects")
				}
			}
		}
	})
}

func TestGatewayRecordCreatedDuringInstallBlocksForwardingCommit(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, phase := range []string{"stop", "start"} {
			h := newAIOHarness(t, manager, true, nil)
			r := h.run(t, "1", "RACE_GATEWAY_RECORD="+phase)
			failedForwarding(t, h, r)
			contains(t, r.stderr, "网关")
			if readFile(t, h.path("agent/mihomo/linux-gateway.json")) != "{\"owner\":\"gateway\"}\n" ||
				readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) {
				t.Errorf("%s: state", phase)
			}
			notContains(t, h.eventsText(), "sysctl -q -w net.ipv4.ip_forward=1")
		}
	})
}

func TestNativeAIORefusesForwardingHookSymlinkWithoutChangingTarget(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, nil)
		target := writeFile(t, h.path("administrator-script"), "#!/bin/sh\nexit 71\n", 0o644)
		helper := h.path(forwardHelper)
		os.Symlink(target, helper)
		r := h.run(t, "1")
		if r.code == 0 {
			t.Fatal("accepted")
		}
		contains(t, r.stderr, "符号链接")
		if info, _ := os.Lstat(helper); readFile(t, target) != "#!/bin/sh\nexit 71\n" || info.Mode()&os.ModeSymlink == 0 ||
			readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) {
			t.Error("state")
		}
	})
}

func TestNativeAIOExplicitlyUnmanagedForwardingHasNoHelpersOrEffects(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, true, func(o *AIOOptions) { o.ManageIPv4Forwarding = false })
		source := readFile(t, h.installer)
		notContains(t, source, "enable-ipv4-forwarding.sh")
		notContains(t, source, "99-configflow-aio-forward.conf")
		if r := h.run(t, "1"); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if readFile(t, h.path("ip_forward")) != "0\n" || exists(h.path(forwardConfig)) || exists(h.path(forwardHelper)) {
			t.Error("effects")
		}
		notContains(t, h.eventsText(), "sysctl ")
	})
}

func TestPhysicalCoreDirectoryAliasIsRejectedBeforeDownload(t *testing.T) {
	h := newAIOHarness(t, "systemd", false, nil)
	writeFile(t, h.installer, strings.ReplaceAll(readFile(t, h.installer), h.path("etc/oxidns/config.yaml"), h.path("etc/alias/oxidns.yaml")), 0o644)
	writeFile(t, h.path("etc/mihomo/oxidns.yaml"), "# independent file but shared journal root\n", 0o644)
	os.Symlink(h.path("etc/mihomo"), h.path("etc/alias"))
	r := h.run(t, "1")
	if r.code != 1 {
		t.Fatalf("exit %d", r.code)
	}
	contains(t, r.stderr, "实际位于同一目录")
	if exists(h.path("agent")) {
		t.Error("agent dir created")
	}
}

func TestCoreLocationInsideAgentDirectoryIsRejectedThroughSymlink(t *testing.T) {
	for _, kind := range []string{"config_file", "config_directory", "binary"} {
		h := newAIOHarness(t, "systemd", false, nil)
		protected := h.path("agent/mihomo")
		os.MkdirAll(protected, 0o755)
		var target string
		if kind == "binary" {
			target = writeFile(t, filepath.Join(protected, "mihomo-core"), "#!/bin/sh\nexit 0\n", 0o755)
			link := h.path("commands/mihomo")
			os.Remove(link)
			os.Symlink(target, link)
		} else {
			target = writeFile(t, filepath.Join(protected, "config.yaml"), "# core must survive\n", 0o644)
			link := h.path("etc/mihomo/config.yaml")
			os.Remove(link)
			if kind == "config_file" {
				os.Symlink(target, link)
			} else {
				os.Remove(filepath.Dir(link))
				os.Symlink(protected, filepath.Dir(link))
			}
		}
		before := readFile(t, target)
		r := h.run(t, "1")
		if r.code != 1 {
			t.Fatalf("%s: exit %d", kind, r.code)
		}
		contains(t, r.stderr, "不能放在 Agent 目录内")
		if readFile(t, target) != before || exists(h.path("events")) {
			t.Errorf("%s: changed", kind)
		}
	}
}

func TestNativeAIORefusesLegacyBeforeChanges(t *testing.T) {
	h := newAIOHarness(t, "systemd", false, nil)
	legacy := writeFile(t, h.path("agent/config-mihomo.json"), `{"agent_id":"legacy"}`, 0o644)
	r := h.run(t, "1")
	if r.code != 1 {
		t.Fatalf("exit %d", r.code)
	}
	contains(t, r.stderr, "旧版共享 Agent")
	if readFile(t, legacy) != `{"agent_id":"legacy"}` || exists(h.path("events")) {
		t.Error("changed")
	}
}

func TestSingleInstallerRefusesAIOInstallAndUninstall(t *testing.T) {
	for _, choice := range []string{"1", "2"} {
		h := newAIOHarness(t, "systemd", true, nil)
		script := ok(GenerateGoAgentInstallScript(GoAgentOptions{ServerURL: "https://config.test", AgentName: "single", ServiceType: "mihomo"}))
		script = strings.ReplaceAll(strings.ReplaceAll(script, "/opt/configflow-agent", h.path("agent")), "/dev/tty", "/dev/stdin")
		writeFile(t, h.installer, script, 0o644)
		original := readFile(t, h.path("agent/oxidns/config.json"))
		r := h.run(t, choice)
		if r.code != 1 {
			t.Fatalf("exit %d", r.code)
		}
		contains(t, r.stderr, "同机")
		if readFile(t, h.path("agent/oxidns/config.json")) != original || exists(h.path("events")) {
			t.Error("changed")
		}
	}
}

// renderedAgentUnit expands the installer's Agent service heredoc exactly as
// the shell does.
func renderedAgentUnit(t *testing.T, manager string, hook bool) string {
	script := ok(aioGenerated(t, nil))
	start := map[string]string{"systemd": "cat > \"/etc/systemd/system/$AGENT_UNIT.service\" <<SERVICEEOF\n",
		"openrc": "cat > \"/etc/init.d/$AGENT_UNIT\" <<SERVICEEOF\n"}[manager]
	if strings.Count(script, start) != 1 {
		t.Fatal("heredoc count")
	}
	_, rest, _ := strings.Cut(script, start)
	body, _, _ := strings.Cut(rest, "\nSERVICEEOF\n")
	var assignments []string
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, `IPV4_SYSTEMD_PRE="ExecStartPre`) || strings.HasPrefix(line, `IPV4_OPENRC_PRE="/bin/sh`) {
			assignments = append(assignments, line)
		}
	}
	if len(assignments) != 2 {
		t.Fatalf("assignments %v", assignments)
	}
	lines := []string{"SERVICE=mihomo", "AGENT_UNIT=configflow-agent-mihomo", "AGENT_DIR=/opt/configflow-agent/mihomo",
		`CONFIG_FILE="$AGENT_DIR/config.json"`, `AGENT_BINARY="$AGENT_DIR/configflow-agent"`,
		"AGENT_LOG=/var/log/configflow-agent-mihomo.log", `IPV4_SYSTEMD_PRE=""`, `IPV4_OPENRC_PRE=""`}
	if hook {
		lines = append(lines, assignments...)
	}
	lines = append(lines, "cat <<SERVICEEOF", body, "SERVICEEOF", "")
	out, err := exec.Command("sh", "-c", strings.Join(lines, "\n")).Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestGoAgentUnitPatchFixturesMatchInstallerTemplates(t *testing.T) {
	for _, c := range [][2]string{{"systemd", "service"}, {"openrc", "initd"}} {
		for _, variant := range []string{"plain", "hook"} {
			want := renderedAgentUnit(t, c[0], variant == "hook")
			fixture := fmt.Sprintf("../../../backend/agents/go-agent/routes/testdata/aio-unit-%s-%s.%s", c[0], variant, c[1])
			if readFile(t, fixture) != want {
				t.Errorf("%s differs from the installer template", fixture)
			}
		}
	}
}

var _ = jsonx.Repr
