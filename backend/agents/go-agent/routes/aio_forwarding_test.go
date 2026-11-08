package routes

import (
	"agent/preflight"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type aioFixture struct {
	*gatewayFixture
	root        string
	manager     string
	layout      string // "aio" | "standalone"
	loaded      string // the systemd unit view as of the last daemon-reload
	reloads     int
	failReloads int
	hookRuns    int
	hookErr     error
	hookNoop    bool
	showErr     error
	tunCalls    int
	writes      []string
	boot        preflight.SysctlBootState // OpenRC sysctl service state
	bootAdds    int
	bootAddErr  error
}

// Both Shell layouts whose Mihomo Agent can persist IPv4 forwarding.
var aioLayouts = []string{"aio", "standalone"}

func newAIOFixture(t *testing.T, manager string) *aioFixture {
	t.Helper()
	return newAIOLayoutFixture(t, manager, "aio")
}

func newAIOLayoutFixture(t *testing.T, manager, layout string) *aioFixture {
	t.Helper()
	g := newGatewayFixture(t)
	oldUnit, oldOpenRC, oldAgent, oldSysfs := aioSystemdUnitDir, aioOpenRCDir, aioAgentRoot, aioSysfsNetRoot
	oldRun, oldPrepare, oldLookPath := aioRun, aioPrepareTUN, aioLookPath
	t.Cleanup(func() {
		aioSystemdUnitDir, aioOpenRCDir, aioAgentRoot, aioSysfsNetRoot = oldUnit, oldOpenRC, oldAgent, oldSysfs
		aioRun, aioPrepareTUN, aioLookPath = oldRun, oldPrepare, oldLookPath
	})
	aioLookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	root := t.TempDir()
	f := &aioFixture{gatewayFixture: g, root: root, manager: manager, layout: layout}
	aioAgentRoot = filepath.Join(root, "opt/configflow-agent")
	aioSystemdUnitDir = filepath.Join(root, "etc/systemd/system")
	aioOpenRCDir = filepath.Join(root, "etc/init.d")
	aioSysctlDir = filepath.Join(root, "etc/sysctl.d")
	aioSysfsNetRoot = filepath.Join(root, "sys/class/net")
	for _, dir := range []string{f.agentDir(), aioSystemdUnitDir, aioOpenRCDir, aioSysctlDir, aioSysfsNetRoot} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	g.cfg.ServiceManager = manager
	if layout == "standalone" {
		// install-go.sh writes neither native_aio nor agent_service_unit.
		g.cfg.NativeAIO, g.cfg.AgentServiceUnit = false, ""
		g.cfg.AgentConfigFile = filepath.Join(aioAgentRoot, "config-mihomo.json")
	} else {
		g.cfg.NativeAIO, g.cfg.AgentServiceUnit = true, "configflow-agent-mihomo"
		g.cfg.AgentConfigFile = filepath.Join(aioAgentRoot, "mihomo/config.json")
	}
	aioSysctlBoot = func() preflight.SysctlBootState { return f.boot }
	f.writeUnit(t, f.fixtureUnit(t, "plain"))
	f.loaded = f.fixtureUnit(t, "plain")
	write := gatewayWriteSysctl
	gatewayWriteSysctl = func(key, value string) error {
		f.writes = append(f.writes, key+"="+value)
		return write(key, value)
	}
	aioRun = func(name string, args ...string) (string, error) {
		command := name + " " + strings.Join(args, " ")
		switch command {
		case "systemctl show -p ExecStartPre --value " + f.unitName() + ".service":
			if f.showErr != nil {
				return "", f.showErr
			}
			entries := []string{}
			for _, line := range strings.Split(f.loaded, "\n") {
				if value, ok := strings.CutPrefix(line, "ExecStartPre="); ok {
					path := strings.Fields(value)[0]
					entries = append(entries, "{ path="+path+" ; argv[]="+value+" ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }")
				}
			}
			return strings.Join(entries, " ") + "\n", nil
		case "systemctl daemon-reload":
			f.reloads++
			if f.failReloads > 0 {
				f.failReloads--
				return "", fmt.Errorf("simulated daemon-reload failure")
			}
			data, err := os.ReadFile(f.unitPath())
			if err != nil {
				return "", err
			}
			f.loaded = string(data)
			dropins, _ := filepath.Glob(f.unitPath() + ".d/*.conf")
			for _, dropin := range dropins {
				extra, _ := os.ReadFile(dropin)
				f.loaded += "\n" + string(extra)
			}
			return "", nil
		case "rc-update add sysctl boot":
			f.bootAdds++
			if f.bootAddErr != nil {
				return "", f.bootAddErr
			}
			f.boot = preflight.SysctlBootEnabled
			return "", nil
		case "/bin/sh " + f.hookPath():
			// Emulates the hook: a missing config is a no-op, a managed one enables.
			f.hookRuns++
			if f.hookErr != nil {
				return "", f.hookErr
			}
			if !f.hookNoop && aioForwardingConfigManaged() {
				return "", write("net/ipv4/ip_forward", "1")
			}
			return "", nil
		}
		t.Fatalf("unexpected command: %s", command)
		return "", nil
	}
	aioPrepareTUN = func(cfg preflight.Config) preflight.Report {
		f.tunCalls++
		if cfg.NativeAIO != (layout == "aio") || cfg.ServiceType != "mihomo" {
			t.Fatalf("TUN preparation received wrong config: %+v", cfg)
		}
		return preflight.Report{{Name: "TUN 自动准备", Level: "error", Message: "modprobe tun 失败", Fix: "检查内核模块。"}}
	}
	return f
}

func (f *aioFixture) agentDir() string {
	if f.layout == "standalone" {
		return aioAgentRoot
	}
	return filepath.Join(aioAgentRoot, "mihomo")
}

func (f *aioFixture) unitName() string {
	if f.layout == "standalone" {
		return "configflow-agent"
	}
	return "configflow-agent-mihomo"
}

func (f *aioFixture) hookPath() string { return filepath.Join(f.agentDir(), aioForwardingHookName) }

// hookContent is the hook this layout's installer writes.
func (f *aioFixture) hookContent() string {
	if f.layout == "standalone" {
		return aioStandaloneForwardingHook
	}
	return aioForwardingHook
}

func (f *aioFixture) unitPath() string {
	if f.manager == "systemd" {
		return filepath.Join(aioSystemdUnitDir, f.unitName()+".service")
	}
	return filepath.Join(aioOpenRCDir, f.unitName())
}

// Fixtures are the installer-rendered units (a Python test keeps them in sync).
func (f *aioFixture) fixtureUnit(t *testing.T, variant string) string {
	t.Helper()
	prefix := map[string]string{"aio": "aio-unit", "standalone": "standalone-unit"}[f.layout]
	name := "testdata/" + prefix + "-systemd-" + variant + ".service"
	if f.manager == "openrc" {
		name = "testdata/" + prefix + "-openrc-" + variant + ".initd"
	}
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "/opt/configflow-agent", aioAgentRoot)
}

func (f *aioFixture) writeUnit(t *testing.T, content string) {
	t.Helper()
	mode := os.FileMode(0644)
	if f.manager == "openrc" {
		mode = 0755
	}
	if err := os.WriteFile(f.unitPath(), []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func (f *aioFixture) setForwarding(t *testing.T, value string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(gatewaySysctlRoot, "net/ipv4/ip_forward"), []byte(value), 0644); err != nil {
		t.Fatal(err)
	}
}

func (f *aioFixture) forwarding(t *testing.T) string {
	t.Helper()
	value, err := gatewayReadSysctl("net/ipv4/ip_forward")
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// snapshot captures every file, symlink and mode below the fixture root.
func (f *aioFixture) snapshot(t *testing.T) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(f.root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		value := info.Mode().String()
		if info.Mode()&os.ModeSymlink != 0 {
			target, _ := os.Readlink(path)
			value += " -> " + target
		} else if info.Mode().IsRegular() {
			data, _ := os.ReadFile(path)
			value += " " + string(data)
		}
		result[strings.TrimPrefix(path, f.root)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func (f *aioFixture) assertPersisted(t *testing.T) {
	t.Helper()
	data, err := os.ReadFile(f.hookPath())
	if err != nil || string(data) != f.hookContent() {
		t.Fatalf("hook not installed: %v", err)
	}
	if info, _ := os.Stat(f.hookPath()); info.Mode().Perm() != 0755 {
		t.Fatalf("hook mode %v", info.Mode())
	}
	unit, err := os.ReadFile(f.unitPath())
	if err != nil || string(unit) != f.fixtureUnit(t, "hook") {
		t.Fatalf("unit not identical to installer output with hook:\n%s", unit)
	}
	config, err := os.ReadFile(aioForwardingConfigPath())
	if err != nil || string(config) != aioForwardingContent {
		t.Fatalf("config not committed: %q %v", config, err)
	}
	if info, _ := os.Stat(aioForwardingConfigPath()); info.Mode().Perm() != 0644 {
		t.Fatalf("config mode %v", info.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(aioSysctlDir, ".configflow-aio-forward.*")); len(leftovers) != 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
	if f.forwarding(t) != "1" {
		t.Fatal("forwarding not enabled")
	}
	wantLayout := map[string]string{"aio": "native_aio", "standalone": "standalone"}[f.layout]
	if status := aioForwardingStatus(f.cfg); status["persisted"] != true || status["current"] != "1" || status["layout"] != wantLayout {
		t.Fatalf("status not persisted: %+v", status)
	}
}

func TestAIOForwardingHookMatchesInstallerScript(t *testing.T) {
	data, err := os.ReadFile("../../scripts/aio-ipv4-forwarding.sh")
	if err != nil {
		t.Fatal(err)
	}
	script := string(data)
	parts := strings.SplitN(script, "<<'CONFIGFLOW_AIO_FORWARDING_EOF'\n", 2)
	if len(parts) != 2 {
		t.Fatal("installer hook heredoc not found")
	}
	body := strings.SplitN(parts[1], "\nCONFIGFLOW_AIO_FORWARDING_EOF\n", 2)
	if len(body) != 2 || body[0]+"\n" != aioForwardingHook {
		t.Fatalf("hook differs from aio-ipv4-forwarding.sh:\n%s", body[0])
	}
	marker := regexp.MustCompile(`(?m)^IPV4_MARKER="([^"]+)"$`).FindStringSubmatch(script)
	if marker == nil || marker[1] != aioForwardingMarker {
		t.Fatalf("marker differs: %v", marker)
	}
	if !strings.Contains(script, `printf '%s\nnet.ipv4.ip_forward=1\n' "$IPV4_MARKER" > "$STAGE_DIR/ipv4-forward.conf"`) || aioForwardingContent != aioForwardingMarker+"\nnet.ipv4.ip_forward=1\n" {
		t.Fatal("persisted config content differs from the installer")
	}
	if !strings.Contains(script, `[ -e "$config" ] || exit 0`) || !strings.Contains(aioForwardingHook, "config=/etc/sysctl.d/99-configflow-aio-forward.conf\n") {
		t.Fatal("hook config path differs")
	}
	expand := func(value string) string {
		return strings.ReplaceAll(value, "$AGENT_DIR", filepath.Join(aioAgentRoot, "mihomo"))
	}
	systemd := regexp.MustCompile(`(?m)^\s+IPV4_SYSTEMD_PRE="([^"]+)"$`).FindStringSubmatch(script)
	openrc := regexp.MustCompile(`(?m)^\s+IPV4_OPENRC_PRE="([^"]+)"$`).FindStringSubmatch(script)
	if systemd == nil || expand(systemd[1]) != aioSystemdHookLine() || openrc == nil || "    "+expand(openrc[1]) != aioOpenRCHookLine() {
		t.Fatalf("service hook lines differ: %v %v", systemd, openrc)
	}
	if !strings.Contains(script, `cp "$STAGE_DIR/enable-ipv4-forwarding.sh" "$AGENT_DIR/enable-ipv4-forwarding.sh"`) || filepath.Base(aioForwardingHookPath()) != "enable-ipv4-forwarding.sh" {
		t.Fatal("hook path differs")
	}
}

func TestAIOForwardingFirstPersistMatchesInstallerAndIsIdempotent(t *testing.T) {
	for _, layout := range aioLayouts {
		for _, manager := range []string{"systemd", "openrc"} {
			t.Run(layout+"/"+manager, func(t *testing.T) {
				testAIOForwardingFirstPersist(t, layout, manager)
			})
		}
	}
}

func testAIOForwardingFirstPersist(t *testing.T, layout, manager string) {
	f := newAIOLayoutFixture(t, manager, layout)
	if status := aioForwardingStatus(f.cfg); status["config"] != "absent" || status["hook_script"] != "absent" || status["startup_hook"] != "absent" || status["persisted"] != false || status["current"] != "0" || status["supported"] != true {
		t.Fatalf("wrong initial status: %+v", status)
	}
	result, err := persistAIOForwarding(f.cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if result["tun_prepare"] != nil || f.tunCalls != 0 {
		t.Fatalf("TUN prepared without request: %+v", result)
	}
	f.assertPersisted(t)
	wantReloads := map[string]int{"systemd": 1, "openrc": 0}[manager]
	if f.reloads != wantReloads || f.hookRuns != 1 {
		t.Fatalf("reloads=%d hook runs=%d", f.reloads, f.hookRuns)
	}
	before := f.snapshot(t)
	if _, err := persistAIOForwarding(f.cfg, false); err != nil {
		t.Fatal(err)
	}
	if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("repeated persist changed files")
	}
	if f.reloads != wantReloads {
		t.Fatal("repeated persist reloaded systemd")
	}
	f.assertPersisted(t)
}

func TestAIOForwardingInsertsHookAfterWorkingDirectory(t *testing.T) {
	f := newAIOFixture(t, "systemd")
	// An installer-equivalent unit whose empty placeholder line was removed.
	plain := strings.Replace(f.fixtureUnit(t, "plain"), "/mihomo\n\nExecStart=", "/mihomo\nExecStart=", 1)
	f.writeUnit(t, plain)
	f.loaded = plain
	if _, err := persistAIOForwarding(f.cfg, false); err != nil {
		t.Fatal(err)
	}
	f.assertPersisted(t)
}

func TestAIOForwardingInsertsHookIntoPreHookOpenRCScript(t *testing.T) {
	f := newAIOFixture(t, "openrc")
	// Native AIO scripts written before the installer reserved the hook line.
	plain := strings.Replace(f.fixtureUnit(t, "plain"), "start_pre() {\n    \n", "start_pre() {\n", 1)
	if plain == f.fixtureUnit(t, "plain") {
		t.Fatal("fixture has no reserved hook line")
	}
	f.writeUnit(t, plain)
	if status := aioForwardingStatus(f.cfg); status["startup_hook"] != "absent" {
		t.Fatalf("old script status: %+v", status)
	}
	if _, err := persistAIOForwarding(f.cfg, false); err != nil {
		t.Fatal(err)
	}
	f.assertPersisted(t)
}

func TestAIOForwardingRefusesForeignStateWithoutChanges(t *testing.T) {
	for _, test := range []struct {
		name, manager, want string
		change              func(*testing.T, *aioFixture)
	}{
		{"foreign config", "systemd", "非 ConfigFlow 配置", func(t *testing.T, f *aioFixture) {
			os.WriteFile(aioForwardingConfigPath(), []byte("net.ipv4.ip_forward=1\n"), 0644)
		}},
		{"config symlink", "systemd", "非 ConfigFlow 配置", func(t *testing.T, f *aioFixture) {
			target := filepath.Join(f.root, "managed.conf")
			os.WriteFile(target, []byte(aioForwardingContent), 0644)
			os.Symlink(target, aioForwardingConfigPath())
		}},
		{"modified hook", "openrc", "已被修改", func(t *testing.T, f *aioFixture) {
			os.WriteFile(f.hookPath(), []byte(f.hookContent()+"echo extra\n"), 0755)
		}},
		{"hook symlink", "systemd", "已被修改", func(t *testing.T, f *aioFixture) {
			target := filepath.Join(f.root, "hook.sh")
			os.WriteFile(target, []byte(f.hookContent()), 0755)
			os.Symlink(target, f.hookPath())
		}},
		{"unit symlink", "systemd", "不是普通文件", func(t *testing.T, f *aioFixture) {
			target := filepath.Join(f.root, "unit.service")
			os.Rename(f.unitPath(), target)
			os.Symlink(target, f.unitPath())
		}},
		{"unknown ExecStartPre", "systemd", "未知 ExecStartPre", func(t *testing.T, f *aioFixture) {
			unit := strings.Replace(f.fixtureUnit(t, "plain"), "\nExecStart=", "\nExecStartPre=/bin/true\nExecStart=", 1)
			f.writeUnit(t, unit)
			f.loaded = unit
		}},
		{"drop-in ExecStartPre", "systemd", "附加配置", func(t *testing.T, f *aioFixture) {
			os.MkdirAll(f.unitPath()+".d", 0755)
			os.WriteFile(filepath.Join(f.unitPath()+".d", "override.conf"), []byte("[Service]\nExecStartPre=/bin/true\n"), 0644)
		}},
		{"loaded foreign ExecStartPre", "systemd", "其他位置", func(t *testing.T, f *aioFixture) {
			f.loaded += "\nExecStartPre=/bin/true\n"
		}},
		{"unexpected ExecStart", "systemd", "ExecStart", func(t *testing.T, f *aioFixture) {
			f.writeUnit(t, strings.Replace(f.fixtureUnit(t, "plain"), "-config ", "-verbose -config ", 1))
		}},
		{"systemctl unavailable", "systemd", "ExecStartPre", func(t *testing.T, f *aioFixture) {
			f.showErr = fmt.Errorf("bus unavailable")
		}},
		{"OpenRC changed", "openrc", "服务脚本已被修改", func(t *testing.T, f *aioFixture) {
			f.writeUnit(t, strings.Replace(f.fixtureUnit(t, "plain"), "start_pre() {\n", "start_pre() {\n    echo custom\n", 1))
		}},
		{"sysctl.d symlink", "openrc", "sysctl.d", func(t *testing.T, f *aioFixture) {
			os.Remove(aioSysctlDir)
			os.Symlink(f.root, aioSysctlDir)
		}},
		{"agent directory symlink", "systemd", "安装目录", func(t *testing.T, f *aioFixture) {
			real := filepath.Join(f.root, "real-agent")
			os.Rename(f.agentDir(), real)
			os.Symlink(real, f.agentDir())
		}},
		{"sysctl missing", "systemd", "缺少 sysctl 命令", func(t *testing.T, f *aioFixture) {
			// The hook would fail at every Agent restart while ip_forward is 0.
			aioLookPath = func(name string) (string, error) {
				if name == "sysctl" {
					return "", fmt.Errorf("executable file not found in $PATH")
				}
				return "/bin/" + name, nil
			}
		}},
		{"permission probe", "systemd", "无法写入 IPv4 转发参数", func(t *testing.T, f *aioFixture) {
			gatewayWriteSysctl = func(string, string) error { return fmt.Errorf("read-only file system") }
		}},
	} {
		for _, layout := range aioLayouts {
			t.Run(layout+"/"+test.name, func(t *testing.T) {
				f := newAIOLayoutFixture(t, test.manager, layout)
				test.change(t, f)
				before := f.snapshot(t)
				_, err := persistAIOForwarding(f.cfg, true)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("wrong refusal: %v", err)
				}
				if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) {
					t.Fatalf("refusal changed files:\nbefore %v\nafter  %v", before, after)
				}
				if f.reloads != 0 || f.hookRuns != 0 || f.tunCalls != 0 || f.forwarding(t) != "0" {
					t.Fatalf("refusal had effects: reloads=%d hooks=%d tun=%d", f.reloads, f.hookRuns, f.tunCalls)
				}
			})
		}
	}
}

func TestAIOForwardingDaemonReloadFailureRollsBackUnit(t *testing.T) {
	for _, layout := range aioLayouts {
		t.Run(layout, func(t *testing.T) { testAIOForwardingDaemonReloadRollback(t, layout) })
	}
}

func testAIOForwardingDaemonReloadRollback(t *testing.T, layout string) {
	f := newAIOLayoutFixture(t, "systemd", layout)
	before := f.snapshot(t)
	f.failReloads = 1
	_, err := persistAIOForwarding(f.cfg, false)
	if err == nil || !strings.Contains(err.Error(), "daemon-reload") {
		t.Fatalf("reload failure accepted: %v", err)
	}
	if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Fatal("unit, hook or config left after rollback")
	}
	if f.reloads != 2 || f.loaded != f.fixtureUnit(t, "plain") || f.hookRuns != 0 || f.forwarding(t) != "0" {
		t.Fatalf("rollback did not reload the restored unit: reloads=%d", f.reloads)
	}
	if _, err := persistAIOForwarding(f.cfg, false); err != nil {
		t.Fatal(err)
	}
	f.assertPersisted(t)
}

func TestAIOForwardingHookFailureKeepsCommittedConfigAndNeverDisables(t *testing.T) {
	for _, test := range []struct {
		name    string
		initial string
		change  func(*aioFixture)
		want    string
	}{
		{"hook error", "1", func(f *aioFixture) { f.hookErr = fmt.Errorf("sysctl denied") }, "未能成功开启"},
		{"hook no-op readback", "0", func(f *aioFixture) { f.hookNoop = true }, "不是 1"},
	} {
		for _, layout := range aioLayouts {
			t.Run(layout+"/"+test.name, func(t *testing.T) {
				f := newAIOLayoutFixture(t, "systemd", layout)
				f.setForwarding(t, test.initial)
				test.change(f)
				_, err := persistAIOForwarding(f.cfg, true)
				if err == nil || !strings.Contains(err.Error(), test.want) {
					t.Fatalf("hook failure accepted: %v", err)
				}
				if !aioForwardingConfigManaged() {
					t.Fatal("committed config removed after hook failure")
				}
				if unit, _ := os.ReadFile(f.unitPath()); string(unit) != f.fixtureUnit(t, "hook") {
					t.Fatal("committed unit rolled back after hook failure")
				}
				for _, write := range f.writes {
					if write != "net/ipv4/ip_forward="+test.initial && write != "net/ipv4/ip_forward=1" {
						t.Fatalf("unexpected sysctl write: %v", f.writes)
					}
				}
				if f.forwarding(t) != test.initial || f.tunCalls != 0 {
					t.Fatal("forwarding changed by failed hook or TUN prepared")
				}
				f.hookErr, f.hookNoop = nil, false
				if _, err := persistAIOForwarding(f.cfg, false); err != nil {
					t.Fatal(err)
				}
				f.assertPersisted(t)
			})
		}
	}
}

func TestAIOForwardingConvergesAfterInterruptedSteps(t *testing.T) {
	for _, cell := range [][2]string{{"aio", "systemd"}, {"aio", "openrc"}, {"standalone", "systemd"}, {"standalone", "openrc"}} {
		layout, manager := cell[0], cell[1]
		for _, test := range []struct {
			name  string
			setup func(*testing.T, *aioFixture)
		}{
			{"after hook", func(t *testing.T, f *aioFixture) {
				os.WriteFile(f.hookPath(), []byte(f.hookContent()), 0755)
			}},
			{"after unit before reload", func(t *testing.T, f *aioFixture) {
				os.WriteFile(f.hookPath(), []byte(f.hookContent()), 0755)
				f.writeUnit(t, f.fixtureUnit(t, "hook"))
			}},
			{"after commit", func(t *testing.T, f *aioFixture) {
				os.WriteFile(f.hookPath(), []byte(f.hookContent()), 0755)
				f.writeUnit(t, f.fixtureUnit(t, "hook"))
				f.loaded = f.fixtureUnit(t, "hook")
				os.WriteFile(aioForwardingConfigPath(), []byte(aioForwardingContent), 0644)
			}},
		} {
			t.Run(layout+"/"+manager+"/"+test.name, func(t *testing.T) {
				f := newAIOLayoutFixture(t, manager, layout)
				test.setup(t, f)
				if _, err := persistAIOForwarding(f.cfg, false); err != nil {
					t.Fatal(err)
				}
				f.assertPersisted(t)
				if manager == "systemd" && test.name == "after commit" && f.reloads != 0 {
					t.Fatal("already loaded unit reloaded")
				}
				if manager == "systemd" && test.name != "after commit" && f.reloads != 1 {
					t.Fatalf("unloaded unit not reloaded: %d", f.reloads)
				}
			})
		}
	}
}

func TestAIOForwardingStatusMatrix(t *testing.T) {
	for _, test := range []struct {
		name, manager string
		change        func(*testing.T, *aioFixture)
		want          map[string]interface{}
	}{
		{"foreign config", "systemd", func(t *testing.T, f *aioFixture) {
			os.WriteFile(aioForwardingConfigPath(), []byte("net.ipv4.ip_forward=1\n"), 0644)
		}, map[string]interface{}{"config": "foreign", "persisted": false}},
		{"config symlink", "systemd", func(t *testing.T, f *aioFixture) {
			os.Symlink("/dev/null", aioForwardingConfigPath())
		}, map[string]interface{}{"config": "symlink"}},
		{"managed config only", "systemd", func(t *testing.T, f *aioFixture) {
			os.WriteFile(aioForwardingConfigPath(), []byte(aioForwardingContent), 0644)
		}, map[string]interface{}{"config": "managed", "hook_script": "absent", "persisted": false}},
		{"modified hook", "systemd", func(t *testing.T, f *aioFixture) {
			os.WriteFile(f.hookPath(), []byte("#!/bin/sh\n"), 0755)
		}, map[string]interface{}{"hook_script": "modified"}},
		{"managed hook", "openrc", func(t *testing.T, f *aioFixture) {
			os.WriteFile(f.hookPath(), []byte(f.hookContent()), 0755)
		}, map[string]interface{}{"hook_script": "managed", "startup_hook": "absent"}},
		{"foreign ExecStartPre", "systemd", func(t *testing.T, f *aioFixture) {
			f.loaded += "\nExecStartPre=/bin/true\n"
		}, map[string]interface{}{"startup_hook": "foreign"}},
		{"duplicate hook", "systemd", func(t *testing.T, f *aioFixture) {
			f.loaded = f.fixtureUnit(t, "hook") + "\n" + "ExecStartPre=/bin/sh " + f.hookPath() + "\n"
		}, map[string]interface{}{"startup_hook": "foreign"}},
		{"registered systemd", "systemd", func(t *testing.T, f *aioFixture) {
			f.loaded = f.fixtureUnit(t, "hook")
		}, map[string]interface{}{"startup_hook": "registered"}},
		{"systemctl failure", "systemd", func(t *testing.T, f *aioFixture) {
			f.showErr = fmt.Errorf("bus unavailable")
		}, map[string]interface{}{"startup_hook": "unknown"}},
		{"registered OpenRC", "openrc", func(t *testing.T, f *aioFixture) {
			f.writeUnit(t, f.fixtureUnit(t, "hook"))
		}, map[string]interface{}{"startup_hook": "registered"}},
		{"changed OpenRC", "openrc", func(t *testing.T, f *aioFixture) {
			f.writeUnit(t, strings.Replace(f.fixtureUnit(t, "plain"), "start_pre() {\n", "start_pre() {\n    echo custom\n", 1))
		}, map[string]interface{}{"startup_hook": "unknown"}},
		{"unreadable forwarding", "systemd", func(t *testing.T, f *aioFixture) {
			f.setForwarding(t, "2")
		}, map[string]interface{}{"current": ""}},
		{"layout reversed", "systemd", func(t *testing.T, f *aioFixture) {
			f.cfg.NativeAIO = !f.cfg.NativeAIO
		}, map[string]interface{}{"supported": false, "startup_hook": "unknown"}},
		{"OxiDNS", "systemd", func(t *testing.T, f *aioFixture) {
			f.cfg.ServiceType = "oxidns"
		}, map[string]interface{}{"supported": false}},
		{"not root", "openrc", func(t *testing.T, f *aioFixture) {
			gatewayGeteuid = func() int { return 1000 }
		}, map[string]interface{}{"supported": false}},
		{"container", "systemd", func(t *testing.T, f *aioFixture) {
			f.cfg.ServiceManager = "supervisor"
		}, map[string]interface{}{"supported": false}},
	} {
		for _, layout := range aioLayouts {
			t.Run(layout+"/"+test.name, func(t *testing.T) {
				f := newAIOLayoutFixture(t, test.manager, layout)
				test.change(t, f)
				status := aioForwardingStatus(f.cfg)
				for key, want := range test.want {
					if status[key] != want {
						t.Fatalf("%s=%v want %v: %+v", key, status[key], want, status)
					}
				}
				wantLayout := map[string]interface{}{"aio": "native_aio", "standalone": "standalone"}[layout]
				if status["supported"] == false {
					wantLayout = nil
				}
				if status["layout"] != wantLayout {
					t.Fatalf("layout=%v want %v: %+v", status["layout"], wantLayout, status)
				}
				if status["protocol"] != 1 {
					t.Fatalf("missing protocol: %+v", status)
				}
				if status["supported"] == false && status["unsupported_reason"] == "" {
					t.Fatalf("unsupported without reason: %+v", status)
				}
				if status["startup_hook"] == "unknown" && test.name == "systemctl failure" && status["error"] == nil {
					t.Fatalf("systemctl failure not reported: %+v", status)
				}
			})
		}
	}
}

func TestAIOForwardingUnsupportedInstallationsAreRefused(t *testing.T) {
	for _, change := range []func(*aioFixture){
		func(f *aioFixture) { f.cfg.NativeAIO = false },
		func(f *aioFixture) { f.cfg.ServiceType = "oxidns" },
		func(f *aioFixture) { gatewayGeteuid = func() int { return 1000 } },
		func(f *aioFixture) { f.cfg.AgentConfigFile = filepath.Join(f.root, "config.json") },
	} {
		f := newAIOFixture(t, "systemd")
		change(f)
		before := f.snapshot(t)
		if _, err := persistAIOForwarding(f.cfg, false); err == nil {
			t.Fatal("unsupported installation persisted forwarding")
		}
		if after := f.snapshot(t); fmt.Sprint(after) != fmt.Sprint(before) || len(f.writes) != 0 {
			t.Fatal("unsupported installation changed host")
		}
	}
}

func TestAIOForwardingStatusReportsSystemPersistenceSeparately(t *testing.T) {
	f := newAIOFixture(t, "systemd")
	if status := aioForwardingStatus(f.cfg); status["system_persisted"] != false || status["system_source"] != nil {
		t.Fatalf("system persistence claimed without configuration: %+v", status)
	}
	// The host persisted forwarding itself; ConfigFlow's own persistence is absent.
	aioSystemForwarding = func() (string, bool) { return "/etc/sysctl.d/99-ip-forward.conf:1", true }
	status := aioForwardingStatus(f.cfg)
	if status["system_persisted"] != true || status["system_source"] != "/etc/sysctl.d/99-ip-forward.conf:1" {
		t.Fatalf("system persistence not reported: %+v", status)
	}
	// Only ConfigFlow's own persistence keeps forwarding when a gateway record is removed.
	if status["persisted"] != false || status["config"] != "absent" {
		t.Fatalf("system persistence counted as ConfigFlow's: %+v", status)
	}
}

func TestAIOTUNStatusReadsDeployedConfiguration(t *testing.T) {
	f := newAIOFixture(t, "systemd")
	status := aioTUNStatus(f.cfg)
	if status["configured"] != true || status["device"] != "Meta" || status["present"] != false || status["gateway_marker"] != true || status["error"] != nil {
		t.Fatalf("wrong legacy gateway TUN status: %+v", status)
	}
	os.WriteFile(f.cfg.ConfigPath, []byte("tun:\n  enable: true\n  device: Mihomo\n  auto-route: true\n  auto-redirect: false\n"), 0600)
	os.MkdirAll(filepath.Join(aioSysfsNetRoot, "Mihomo"), 0755)
	status = aioTUNStatus(f.cfg)
	if status["configured"] != false || status["device"] != "Mihomo" || status["present"] != true || status["gateway_marker"] != false {
		t.Fatalf("wrong TUN status: %+v", status)
	}
	os.WriteFile(f.cfg.ConfigPath, []byte("tun: [\n"), 0600)
	if status = aioTUNStatus(f.cfg); status["error"] == nil || status["configured"] != false {
		t.Fatalf("invalid config not reported: %+v", status)
	}
}

func TestLinuxGatewayForwardingHTTP(t *testing.T) {
	for _, layout := range aioLayouts {
		t.Run(layout, func(t *testing.T) { testLinuxGatewayForwardingHTTP(t, layout) })
	}
}

func testLinuxGatewayForwardingHTTP(t *testing.T, layout string) {
	f := newAIOLayoutFixture(t, "openrc", layout)
	call := func(method, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		LinuxGatewayHandler(f.cfg, "forwarding")(recorder, httptest.NewRequest(method, "/api/linux-gateway/forwarding", strings.NewReader(body)))
		return recorder
	}
	if recorder := call(http.MethodGet, ""); recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET accepted: %d", recorder.Code)
	}
	for _, body := range []string{`{"prepare_tun":true,"command":"id"}`, `{}{}`, `{"prepare_tun":"yes"}`, `[]`} {
		if recorder := call(http.MethodPost, body); recorder.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, recorder.Code, recorder.Body.String())
		}
	}
	if f.hookRuns != 0 || aioForwardingConfigManaged() {
		t.Fatal("invalid request changed host")
	}
	recorder := call(http.MethodPost, "")
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
		t.Fatalf("empty body failed: %d %s", recorder.Code, recorder.Body.String())
	}
	forwarding, _ := response["ipv4_forwarding"].(map[string]interface{})
	managed, _ := response["managed"].(map[string]interface{})
	if response["success"] != true || response["message"] != "IPv4 转发已开启并持久化" || forwarding["persisted"] != true || response["tun_prepare"] != nil || managed["preserves_forwarding"] != true {
		t.Fatalf("wrong response: %+v", response)
	}
	f.assertPersisted(t)
	recorder = call(http.MethodPost, `{"prepare_tun":true}`)
	response = nil
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
		t.Fatalf("TUN preparation failure failed the request: %d %s", recorder.Code, recorder.Body.String())
	}
	prepare, _ := response["tun_prepare"].(map[string]interface{})
	if f.tunCalls != 1 || prepare["level"] != "error" || !strings.Contains(fmt.Sprint(prepare["message"]), "检查内核模块") {
		t.Fatalf("TUN preparation not summarized: %+v", response)
	}
	os.Remove(aioForwardingConfigPath())
	os.WriteFile(aioForwardingConfigPath(), []byte("net.ipv4.ip_forward=1\n"), 0644)
	recorder = call(http.MethodPost, `{}`)
	response = nil
	json.Unmarshal(recorder.Body.Bytes(), &response)
	forwarding, _ = response["ipv4_forwarding"].(map[string]interface{})
	if recorder.Code != 409 || response["success"] != false || forwarding["config"] != "foreign" {
		t.Fatalf("foreign config not refused: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestLinuxGatewayStatusReportsForwardingAndTUN(t *testing.T) {
	for _, layout := range aioLayouts {
		t.Run(layout, func(t *testing.T) { testLinuxGatewayStatusReportsForwardingAndTUN(t, layout) })
	}
}

func testLinuxGatewayStatusReportsForwardingAndTUN(t *testing.T, layout string) {
	f := newAIOLayoutFixture(t, "systemd", layout)
	recorder := httptest.NewRecorder()
	LinuxGatewayHandler(f.cfg, "status")(recorder, httptest.NewRequest(http.MethodGet, "/api/linux-gateway/status", nil))
	var response struct {
		Forwarding map[string]interface{} `json:"ipv4_forwarding"`
		TUN        map[string]interface{} `json:"tun"`
		Managed    map[string]interface{} `json:"managed"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil || recorder.Code != 200 {
		t.Fatalf("status failed: %d %s", recorder.Code, recorder.Body.String())
	}
	if response.Forwarding["protocol"] != float64(1) || response.Forwarding["config"] != "absent" || response.TUN["device"] != "Meta" || response.Managed["preserves_forwarding"] != false {
		t.Fatalf("wrong status: %s", recorder.Body.String())
	}
}

func TestCapabilitiesAdvertiseAIOForwardingForMihomoOnly(t *testing.T) {
	for service, want := range map[string]bool{"mihomo": true, "oxidns": false} {
		recorder := httptest.NewRecorder()
		CapabilitiesHandler(&Config{ServiceType: service, ConfigPath: filepath.Join(t.TempDir(), "config.yaml")})(recorder, httptest.NewRequest(http.MethodGet, "/api/capabilities", nil))
		if got := bytes.Contains(recorder.Body.Bytes(), []byte(`"aio_forwarding_protocols":[1]`)); got != want {
			t.Fatalf("%s capability=%v: %s", service, got, recorder.Body.String())
		}
	}
}

// PVE's Alpine LXC template has the OpenRC sysctl service in no runlevel, so
// sysctl.d is never applied at boot; persisting forwarding adds it, like the
// installers do.
func TestAIOForwardingAddsOpenRCSysctlServiceToBoot(t *testing.T) {
	for _, layout := range aioLayouts {
		t.Run(layout, func(t *testing.T) {
			f := newAIOLayoutFixture(t, "openrc", layout)
			f.boot = preflight.SysctlBootDisabled
			status := aioForwardingStatus(f.cfg)
			if status["boot_sysctl"] != "disabled" || status["persisted"] != false || !strings.Contains(fmt.Sprint(status["notice"]), "rc-update add sysctl boot") {
				t.Fatalf("disabled sysctl service not reported: %+v", status)
			}
			if _, err := persistAIOForwarding(f.cfg, false); err != nil {
				t.Fatal(err)
			}
			if f.bootAdds != 1 || f.boot != preflight.SysctlBootEnabled {
				t.Fatalf("sysctl service not added to boot: adds=%d state=%q", f.bootAdds, f.boot)
			}
			f.assertPersisted(t)
			if status := aioForwardingStatus(f.cfg); status["boot_sysctl"] != "enabled" || status["notice"] != nil {
				t.Fatalf("enabled sysctl service still flagged: %+v", status)
			}
			if _, err := persistAIOForwarding(f.cfg, false); err != nil || f.bootAdds != 1 {
				t.Fatalf("repeated persist re-ran rc-update: adds=%d err=%v", f.bootAdds, err)
			}
		})
	}
}

func TestAIOForwardingGivesManualStepsWhenSysctlBootCannotBeSet(t *testing.T) {
	for _, tc := range []struct {
		name  string
		boot  preflight.SysctlBootState
		err   error
		wants []string
	}{
		{"rc-update-fails", preflight.SysctlBootDisabled, fmt.Errorf("permission denied"), []string{"permission denied", "rc-update add sysctl boot", "sysctl net.ipv4.ip_forward"}},
		{"no-sysctl-service", preflight.SysctlBootUnavailable, nil, []string{"/etc/init.d/sysctl", "/etc/local.d/configflow-ip-forward.start", "rc-update add local default"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAIOFixture(t, "openrc")
			f.boot, f.bootAddErr = tc.boot, tc.err
			_, err := persistAIOForwarding(f.cfg, false)
			if err == nil {
				t.Fatal("unapplied boot loader reported as persisted")
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("manual step %q missing: %v", want, err)
				}
			}
			// Forwarding stays on and committed; only the boot step is missing.
			if f.forwarding(t) != "1" || !aioForwardingConfigManaged() {
				t.Fatal("forwarding rolled back after the commit point")
			}
			status := aioForwardingStatus(f.cfg)
			if status["persisted"] != false || status["config"] != "managed" || status["startup_hook"] != "registered" {
				t.Fatalf("status overclaimed persistence: %+v", status)
			}
			if tc.boot == preflight.SysctlBootUnavailable && !strings.Contains(fmt.Sprint(status["notice"]), "rc-update add local default") {
				t.Fatalf("status lacks manual steps: %+v", status)
			}
		})
	}
}

// persistedOpenRCFixture is a host ConfigFlow persisted forwarding on while
// its sysctl service was in a runlevel, which is then missing again, as on
// hosts installed before Agents added it.
func persistedOpenRCFixture(t *testing.T, layout string) *aioFixture {
	t.Helper()
	f := newAIOLayoutFixture(t, "openrc", layout)
	f.boot = preflight.SysctlBootEnabled
	if _, err := persistAIOForwarding(f.cfg, false); err != nil {
		t.Fatal(err)
	}
	f.boot = preflight.SysctlBootDisabled
	if status := aioForwardingStatus(f.cfg); status["persisted"] != false {
		t.Fatalf("missing boot loader still persisted: %+v", status)
	}
	return f
}

func TestAgentStartAddsSysctlServiceForPersistedForwarding(t *testing.T) {
	for _, layout := range aioLayouts {
		t.Run(layout, func(t *testing.T) {
			f := persistedOpenRCFixture(t, layout)
			before := f.hookRuns
			repaired, err := aioRepairSysctlBoot(f.cfg)
			if err != nil || !repaired || f.bootAdds != 1 || f.boot != preflight.SysctlBootEnabled {
				t.Fatalf("not repaired: %v %v adds=%d", repaired, err, f.bootAdds)
			}
			if f.hookRuns != before || f.forwarding(t) != "1" {
				t.Fatal("repair touched forwarding itself")
			}
			if status := aioForwardingStatus(f.cfg); status["persisted"] != true || status["notice"] != nil {
				t.Fatalf("status not restored: %+v", status)
			}
			// Already in a runlevel: nothing runs again.
			if repaired, err := aioRepairSysctlBoot(f.cfg); repaired || err != nil || f.bootAdds != 1 {
				t.Fatalf("repeated repair: %v %v adds=%d", repaired, err, f.bootAdds)
			}
		})
	}
}

func TestAgentStartLeavesHostsWithoutPersistedForwardingAlone(t *testing.T) {
	for _, mode := range []string{"never-persisted", "foreign-config", "hook-not-registered", "oxidns", "systemd"} {
		t.Run(mode, func(t *testing.T) {
			manager := "openrc"
			if mode == "systemd" {
				manager = "systemd"
			}
			f := newAIOFixture(t, manager)
			switch mode {
			case "foreign-config":
				if err := os.WriteFile(aioForwardingConfigPath(), []byte("net.ipv4.ip_forward=1\n"), 0644); err != nil {
					t.Fatal(err)
				}
			case "hook-not-registered":
				// Managed config and hook script, but the service never runs it.
				if err := os.WriteFile(aioForwardingConfigPath(), []byte(aioForwardingContent), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.hookPath(), []byte(f.hookContent()), 0755); err != nil {
					t.Fatal(err)
				}
			case "oxidns":
				f.cfg.ServiceType = "oxidns"
			case "systemd":
				if _, err := persistAIOForwarding(f.cfg, false); err != nil {
					t.Fatal(err)
				}
			}
			if mode != "systemd" {
				f.boot = preflight.SysctlBootDisabled
			}
			repaired, err := aioRepairSysctlBoot(f.cfg)
			if repaired || err != nil || f.bootAdds != 0 {
				t.Fatalf("host changed: %v %v adds=%d", repaired, err, f.bootAdds)
			}
		})
	}
}

func TestAgentStartSysctlRepairFailureOnlyLogs(t *testing.T) {
	f := persistedOpenRCFixture(t, "standalone")
	f.bootAddErr = fmt.Errorf("permission denied")
	repaired, err := aioRepairSysctlBoot(f.cfg)
	if repaired || err == nil || !strings.Contains(err.Error(), "rc-update add sysctl boot") {
		t.Fatalf("failure not reported with the manual step: %v %v", repaired, err)
	}
	// The background entry point swallows the failure; startup is unaffected.
	reapplyForwardingBoot(f.cfg)
	status := aioForwardingStatus(f.cfg)
	if status["persisted"] != false || !strings.Contains(fmt.Sprint(status["notice"]), "rc-update add sysctl boot && rc-update show boot | grep sysctl") {
		t.Fatalf("status lacks the manual command: %+v", status)
	}
	if f.forwarding(t) != "1" || !aioForwardingConfigManaged() {
		t.Fatal("failed repair changed forwarding")
	}
	// A nil configuration never panics the Agent.
	reapplyForwardingBoot(nil)
}
