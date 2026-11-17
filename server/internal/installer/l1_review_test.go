package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// L1 review R1: the Mihomo forwarding hook is written after the old Agent is
// stopped, so a failed fresh installation must not leave it behind.
func TestL1ReviewFailedFreshInstallRemovesForwardingHook(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newStandaloneHarness(t, standaloneSetup{manager: manager})
		r := h.run(t, "1", "FAIL_AGENT_START=yes")
		if r.code == 0 {
			t.Fatal("failed Agent start reported success")
		}
		for _, path := range []string{h.hook, h.unit, filepath.Join(h.root, "usr/local/bin/configflow-agent"),
			filepath.Join(h.agentDir, "config-mihomo.json")} {
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Errorf("failed fresh installation left %s (%v)", path, err)
			}
		}
		// This harness's stateless manager reports every unit as running, so
		// compensation tries to restart "configflow-agent"; only file cleanup
		// is asserted here. Stateful service restoration: l1_rollback_test.go.
	})
}

// L1 review R2: directories created by a failed fresh AIO installation are
// removed with everything inside; a pre-existing sibling is untouched.
func TestL1ReviewFailedFreshAIORemovesOnlyCreatedDirectories(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newAIOHarness(t, manager, false, func(o *AIOOptions) {
			o.ManageIPv4Forwarding, o.ManageTun = false, false
		})
		candidate := filepath.Join(h.tmp, "fake-agent")
		writeFile(t, candidate, readFile(t, candidate)+"\n# L1_CANDIDATE\n", 0o755)
		keep := writeFile(t, h.path("agent/keep/user.txt"), "user data\n", 0o600)
		services := map[string]l1Service{}
		for _, kind := range []string{"mihomo", "oxidns"} {
			services["configflow-agent-"+kind] = l1Service{
				Binary: h.path("agent/" + kind + "/configflow-agent"), Config: h.path("agent/" + kind + "/config.json")}
		}
		options, state := l1SeedManager(t, h.tmp, h.path("etc"), manager, "configflow-agent-oxidns", services)
		// The first candidate writes a file of its own, as the real Agent's
		// startup migration journal does.
		writeFile(t, filepath.Join(h.tmp, "commands/sleep"), "#!/bin/sh\nif [ -d \"$L1_AGENT_ROOT/mihomo\" ]; then touch \"$L1_AGENT_ROOT/mihomo/.agent-migration.json\"; fi\nexit 0\n", 0o755)
		r := h.run(t, "1", append(options, "L1_AGENT_ROOT="+h.path("agent"))...)
		if r.code == 0 {
			t.Fatal("failed second candidate reported success")
		}
		l1AssertCandidateFailed(t, state)
		for _, kind := range []string{"mihomo", "oxidns"} {
			if _, err := os.Lstat(h.path("agent/" + kind)); !os.IsNotExist(err) {
				t.Errorf("failed fresh AIO left agent/%s (%v)", kind, err)
			}
		}
		if readFile(t, keep) != "user data\n" {
			t.Error("unrelated file changed")
		}
		contains(t, detailOutput(t, r), "已恢复安装前的 Agent 文件、身份和服务状态")
	})
}

// L1 review R3: a journal left by a killed run is reported, never applied or
// deleted, and the current run still compensates its own failure.
func TestL1ReviewLeftoverBackupIsReported(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		h := newNativeHarness(t, manager, "oxidns")
		binary := writeFile(t, filepath.Join(h.tmp, "local-bin/configflow-agent"), "#!/bin/sh\n# L1_OLD\nexit 0\n", 0o755)
		writeFile(t, filepath.Join(h.tmp, "fake-agent"), nativeFakeAgent+"\n# L1_CANDIDATE\n", 0o755)
		stale := filepath.Join(h.tmp, "agent/.agent-install-backup.KILLED")
		staleConfig := writeFile(t, filepath.Join(stale, "file.2"), `{"agent_id":"killed-run-id","token":"killed-run-token"}`, 0o600)
		writeFile(t, filepath.Join(stale, "path.2"), h.config+"\n", 0o600)
		before := l1CaptureFiles(t, binary, h.config, staleConfig)
		options, state := l1SeedManager(t, h.tmp, filepath.Join(h.tmp, "etc"), manager, "configflow-agent", map[string]l1Service{
			"configflow-agent": {Binary: binary, Config: h.config, Active: true, Enabled: true},
		})
		r := h.run(t, options...)
		if r.code == 0 {
			t.Fatal("failed candidate reported success")
		}
		l1AssertCandidateFailed(t, state)
		l1CheckFiles(t, before)
		log := detailOutput(t, r)
		contains(t, log, "发现以前安装中断或恢复失败时保留的 Agent 备份："+stale)
		contains(t, log, "已恢复安装前的 Agent 文件、身份和服务状态")
		backups, err := filepath.Glob(filepath.Join(h.tmp, "agent/.agent-install-backup.*"))
		if err != nil || len(backups) != 1 || backups[0] != stale {
			t.Fatalf("leftover journal removed or this run's journal kept: %v (%v)", backups, err)
		}
		if strings.Contains(readFile(t, h.config), "killed-run-id") {
			t.Fatal("a leftover journal was applied automatically")
		}
	})
}

// L1 review D5: a rerun whose owned main container is already running holds
// port 53 itself; the probe must not report that as a foreign listener.
// Macvlan installs never probe the host namespace.
func TestL1ReviewOwnRunningOxiDNSIsNotAPortConflict(t *testing.T) {
	for _, mode := range []string{"host", "macvlan"} {
		t.Run(mode, func(t *testing.T) {
			tmp := t.TempDir()
			_, host, guide := newHost(t, tmp, mode)
			if mode == "host" {
				if r := runCommand(t, guide.GetString("install_command"), tmp, host.env...); r.code != 0 {
					t.Fatalf("first install: %d %s %s", r.code, r.stdout, r.stderr)
				}
			}
			setStateField(t, host.state, "port_busy", true)
			r := runCommand(t, guide.GetString("install_command"), tmp, host.env...)
			if r.code != 0 {
				t.Fatalf("own listener treated as a conflict: %d %s %s", r.code, r.stdout, r.stderr)
			}
			notContains(t, r.stderr, "53 端口不可用")
			contains(t, r.stdout, "本次安装的容器已启动或恢复")
		})
	}
}
