//go:build halab

package routes

// Real Alpine/OpenRC keepalived for lab L3-D1. Runs as root inside a
// disposable container (integration/oxidns_ha_openrc_lab.sh) on a private
// Docker network; never on a host whose network matters.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func labOpenRCStatus() string {
	out, err := haRun("rc-service", "keepalived", "status")
	if err != nil {
		out += " " + err.Error()
	}
	return out
}

func labKeepalivedPIDs() []int {
	pids := []int{}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err == nil && strings.TrimSpace(string(comm)) == "keepalived" {
			pids = append(pids, pid)
		}
	}
	return pids
}

func labVIPPresent(t *testing.T, vip string) bool {
	t.Helper()
	addresses, err := readHAAddresses()
	if err != nil {
		t.Fatal(err)
	}
	return haVIPPresent(OxiDNSHASpec{VIP: vip}, addresses)
}

func labWait(condition func() bool) bool {
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if condition() {
			return true
		}
	}
	return false
}

func haLabSetup(t *testing.T) (*Config, OxiDNSHASpec) {
	t.Helper()
	if os.Getenv("CONFIGFLOW_HA_LAB") != "1" {
		t.Skip("set CONFIGFLOW_HA_LAB=1 inside a disposable container")
	}
	dir := t.TempDir()
	cfg := &Config{AgentConfigFile: filepath.Join(dir, "config-oxidns.json"), ConfigPath: filepath.Join(dir, "oxidns.yaml"),
		ServiceType: "oxidns", ServiceManager: "openrc"}
	oxidns := "plugins:\n  - tag: main_sequence\n    type: sequence\n    args:\n      - exec: accept\n  - tag: udp_server\n    type: udp_server\n    args:\n      listen: 0.0.0.0:53\n      entry: main_sequence\n"
	if err := os.WriteFile(cfg.ConfigPath, []byte(oxidns), 0600); err != nil {
		t.Fatal(err)
	}
	old := haExecutable
	haExecutable = func() (string, error) { return "/bin/true", nil } // always healthy
	t.Cleanup(func() { haExecutable = old })
	spec := OxiDNSHASpec{SafetyVersion: 1, VIP: os.Getenv("HA_LAB_VIP"), VRID: 53, Priority: 150,
		SelfIP: os.Getenv("HA_LAB_SELF"), PeerIP: os.Getenv("HA_LAB_PEER")}
	return cfg, spec
}

func labApply(t *testing.T, cfg *Config, spec OxiDNSHASpec) {
	t.Helper()
	if err := applyOxiDNSHA(cfg, spec); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if !labWait(func() bool { return labVIPPresent(t, spec.VIP) }) {
		t.Fatalf("keepalived never took the virtual IP (pids %v)", labKeepalivedPIDs())
	}
}

func labRemoveClean(t *testing.T, cfg *Config, spec OxiDNSHASpec) {
	t.Helper()
	if removed, err := removeOxiDNSHA(cfg); err != nil || !removed {
		t.Fatalf("remove: %v %v", removed, err)
	}
	if pids := labKeepalivedPIDs(); len(pids) != 0 {
		t.Fatalf("keepalived still running after removal: %v", pids)
	}
	if labVIPPresent(t, spec.VIP) {
		t.Fatal("virtual IP still on the host after removal")
	}
	if out := labOpenRCStatus(); strings.Contains(out, "started") || strings.Contains(out, "crashed") {
		t.Fatalf("OpenRC state left behind: %s", out)
	}
}

func labKill(t *testing.T) {
	t.Helper()
	for _, pid := range labKeepalivedPIDs() {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	if !labWait(func() bool { return len(labKeepalivedPIDs()) == 0 }) {
		t.Fatal("keepalived survived SIGKILL")
	}
	// keepalived cannot remove its address after SIGKILL; neither can a user
	// who only killed the process. Do it here like the lab cleanup did.
	_, _ = haRun("ip", "addr", "del", os.Getenv("HA_LAB_VIP")+"/24", "dev", "eth0")
}

func TestHALabOpenRCLifecycle(t *testing.T) {
	cfg, spec := haLabSetup(t)
	for round := 1; round <= 2; round++ {
		labApply(t, cfg, spec)
		labRemoveClean(t, cfg, spec)
	}
}

// Files deleted first by an earlier Agent: the init script refuses status and
// stop, the pidfile still names the daemon.
func TestHALabOpenRCRemoveWithConfigAlreadyDeleted(t *testing.T) {
	cfg, spec := haLabSetup(t)
	labApply(t, cfg, spec)
	for _, path := range []string{haConfigPath(), haMainConfigPath()} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := haRun("rc-service", "keepalived", "status"); err == nil {
		t.Fatal("precondition: OpenRC status must fail without keepalived.conf")
	}
	labRemoveClean(t, cfg, spec)
}

func TestHALabOpenRCCrashedRecovers(t *testing.T) {
	cfg, spec := haLabSetup(t)
	labApply(t, cfg, spec)
	labKill(t)
	if out := labOpenRCStatus(); !strings.Contains(out, "crashed") {
		t.Fatalf("precondition: OpenRC must report crashed: %s", out)
	}
	// Re-enable on a crashed service (the lab's "keepalived 启动后未保持运行").
	labApply(t, cfg, spec)
	labKill(t)
	// Disable on a crashed service.
	labRemoveClean(t, cfg, spec)
	labApply(t, cfg, spec)
	labRemoveClean(t, cfg, spec)
}
