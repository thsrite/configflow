package routes

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// openRCKeepalived models Alpine's keepalived service: the init script has
// required_files=/etc/keepalived/keepalived.conf, so every rc-service command
// fails once that file is gone, while the daemon keeps running and holding
// the virtual IP. A daemon that dies while OpenRC records it as started is
// "crashed" until zapped. Lab L3-D1.
type openRCKeepalived struct {
	t        *testing.T
	f        *haFixture
	started  bool // OpenRC's recorded state
	alive    bool // the daemon process
	enabled  bool
	killErr  error
	stopFail bool
	pinVIP   bool // something else keeps the virtual IP on the host
	order    []string
}

const openRCPID = 4242

func newOpenRCKeepalived(t *testing.T) *openRCKeepalived {
	t.Helper()
	f := newHAFixture(t)
	f.cfg.ServiceManager = "openrc"
	k := &openRCKeepalived{t: t, f: f}
	oldPID, oldProc, oldKill := haOpenRCPIDFile, haProcRoot, haKill
	t.Cleanup(func() { haOpenRCPIDFile, haProcRoot, haKill = oldPID, oldProc, oldKill })
	root := t.TempDir()
	haOpenRCPIDFile = filepath.Join(root, "keepalived.pid")
	haProcRoot = filepath.Join(root, "proc")
	haKill = func(pid int, signal syscall.Signal) error {
		k.order = append(k.order, fmt.Sprintf("kill %d %d", pid, signal))
		if k.killErr != nil {
			return k.killErr
		}
		if pid == openRCPID && k.alive {
			k.setAlive(false)
		}
		return nil
	}
	haRun = k.run
	return k
}

func (k *openRCKeepalived) setAlive(alive bool) {
	k.alive = alive
	dir := filepath.Join(haProcRoot, fmt.Sprint(openRCPID))
	if alive {
		if err := os.MkdirAll(dir, 0755); err != nil {
			k.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte("keepalived\n"), 0644); err != nil {
			k.t.Fatal(err)
		}
		if err := os.WriteFile(haOpenRCPIDFile, []byte(fmt.Sprintln(openRCPID)), 0644); err != nil {
			k.t.Fatal(err)
		}
		return
	}
	_ = os.RemoveAll(dir) // the pidfile stays behind, like a killed daemon's
}

func (k *openRCKeepalived) run(name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	k.order = append(k.order, command)
	_, statErr := os.Stat(haMainConfigPath())
	configMissing := statErr != nil
	switch command {
	case "ip -j -4 addr show":
		addresses := `{"local":"192.168.0.20","prefixlen":24}`
		if k.alive || k.pinVIP {
			addresses += `,{"local":"192.168.0.2","prefixlen":24}`
		}
		return `[{"ifname":"eth0","addr_info":[` + addresses + `]}]`, nil
	case "rc-update show default":
		if k.enabled {
			return " keepalived | default\n", nil
		}
		return "", nil
	case "rc-update add keepalived default":
		k.enabled = true
		return "", nil
	case "rc-update del keepalived default":
		k.enabled = false
		return "", nil
	case "rc-service keepalived zap":
		k.started = false
		return "", nil
	}
	if !strings.HasPrefix(command, "rc-service keepalived ") {
		k.t.Fatalf("unexpected command: %s", command)
	}
	if configMissing {
		return "", fmt.Errorf("exit status 1: keepalived: %s does not exist", haMainConfigPath())
	}
	switch args[1] {
	case "status":
		switch {
		case k.started && k.alive:
			return " * status: started\n", nil
		case k.started:
			return "", fmt.Errorf("exit status 32:  * status: crashed") // stderr, as on Alpine 3.24
		}
		return " * status: stopped\n", fmt.Errorf("exit status 3")
	case "start", "restart":
		if k.started && !k.alive {
			return "", fmt.Errorf("exit status 1: keepalived has crashed")
		}
		k.started = true
		k.setAlive(true)
	case "stop":
		if k.stopFail || (k.started && !k.alive) {
			return "", fmt.Errorf("exit status 1")
		}
		k.started = false
		k.setAlive(false)
	default:
		k.t.Fatalf("unexpected command: %s", command)
	}
	return "", nil
}

func TestOxiDNSHAOpenRCRemoveStopsKeepalivedBeforeDeletingConfig(t *testing.T) {
	k := newOpenRCKeepalived(t)
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatal(err)
	}
	if !k.alive || !k.started || !k.enabled {
		t.Fatalf("apply did not start keepalived: %+v", k)
	}
	removed, err := removeOxiDNSHA(k.f.cfg)
	if err != nil || !removed {
		t.Fatalf("remove: %v %v (%v)", removed, err, k.order)
	}
	if k.alive || k.started || k.enabled {
		t.Fatalf("keepalived left behind after removal: alive=%v started=%v enabled=%v (%v)", k.alive, k.started, k.enabled, k.order)
	}
	if _, err := os.Stat(haMainConfigPath()); !os.IsNotExist(err) {
		t.Fatal("created keepalived.conf kept")
	}
	if state, err := loadHAState(k.f.cfg); err != nil || state != nil {
		t.Fatalf("journal kept: %+v %v", state, err)
	}
}

func TestOxiDNSHAOpenRCRemoveStopsDaemonByPIDFileWhenConfigGone(t *testing.T) {
	k := newOpenRCKeepalived(t)
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatal(err)
	}
	// An earlier Agent deleted both files first and left the daemon running.
	for _, path := range []string{haConfigPath(), haMainConfigPath()} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := removeOxiDNSHA(k.f.cfg)
	if err != nil || !removed {
		t.Fatalf("remove: %v %v (%v)", removed, err, k.order)
	}
	if k.alive || k.started {
		t.Fatalf("daemon or OpenRC state left behind: alive=%v started=%v (%v)", k.alive, k.started, k.order)
	}
	if !strings.Contains(strings.Join(k.order, "\n"), fmt.Sprintf("kill %d %d", openRCPID, syscall.SIGTERM)) {
		t.Fatalf("pidfile fallback not used: %v", k.order)
	}
}

func TestOxiDNSHAOpenRCRemoveFailsWhileKeepalivedRuns(t *testing.T) {
	k := newOpenRCKeepalived(t)
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatal(err)
	}
	k.stopFail, k.killErr = true, fmt.Errorf("operation not permitted")
	if _, err := removeOxiDNSHA(k.f.cfg); err == nil || !strings.Contains(err.Error(), "停用 DNS 高可用前停止 keepalived 失败") {
		t.Fatalf("removal must fail while keepalived runs: %v", err)
	}
	if _, err := os.Stat(haConfigPath()); err != nil {
		t.Fatal("configuration must stay while keepalived could not be stopped")
	}
	if state, err := loadHAState(k.f.cfg); err != nil || state == nil {
		t.Fatalf("journal must be kept for a retry: %v", err)
	}
	k.stopFail, k.killErr = false, nil
	if removed, err := removeOxiDNSHA(k.f.cfg); err != nil || !removed || k.alive {
		t.Fatalf("retry: %v %v alive=%v", removed, err, k.alive)
	}
}

func TestOxiDNSHARemoveFailsWhileVIPStaysOnHost(t *testing.T) {
	k := newOpenRCKeepalived(t)
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatal(err)
	}
	k.pinVIP = true
	if _, err := removeOxiDNSHA(k.f.cfg); err == nil || !strings.Contains(err.Error(), "虚拟 IP 192.168.0.2 仍在本机网卡上") {
		t.Fatalf("removal must report the remaining virtual IP: %v", err)
	}
	if state, err := loadHAState(k.f.cfg); err != nil || state == nil {
		t.Fatalf("journal must be kept for a retry: %v", err)
	}
	k.pinVIP = false
	if removed, err := removeOxiDNSHA(k.f.cfg); err != nil || !removed {
		t.Fatalf("retry after the address is gone: %v %v", removed, err)
	}
}

func TestOxiDNSHAOpenRCStartRecoversCrashedService(t *testing.T) {
	k := newOpenRCKeepalived(t)
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatal(err)
	}
	if removed, err := removeOxiDNSHA(k.f.cfg); err != nil || !removed {
		t.Fatal(err)
	}
	// A user killed a leftover daemon by hand: OpenRC now reports crashed.
	k.started = true
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err != nil {
		t.Fatalf("apply must reset a crashed service: %v (%v)", err, k.order)
	}
	if !k.alive || !k.started {
		t.Fatalf("keepalived not running after recovery: %+v", k)
	}
	if !strings.Contains(strings.Join(k.order, "\n"), "rc-service keepalived zap") {
		t.Fatalf("crashed state not zapped: %v", k.order)
	}
}

func TestOxiDNSHAApplyRollbackStopsBeforeRestoringFiles(t *testing.T) {
	k := newOpenRCKeepalived(t)
	// keepalived starts, then the Agent's own readiness wait fails: the
	// rollback must still stop it although it deletes keepalived.conf.
	start := k.run
	haRun = func(name string, args ...string) (string, error) {
		out, err := start(name, args...)
		if name == "rc-service" && len(args) == 2 && args[1] == "status" && err == nil {
			return out, fmt.Errorf("exit status 3")
		}
		return out, err
	}
	if err := applyOxiDNSHA(k.f.cfg, k.f.spec); err == nil {
		t.Fatal("apply must fail when keepalived does not report running")
	}
	if k.alive {
		t.Fatalf("rollback left keepalived running: %v", k.order)
	}
	if _, err := os.Stat(haMainConfigPath()); !os.IsNotExist(err) {
		t.Fatal("rollback kept the created keepalived.conf")
	}
}

func TestOxiDNSHASystemdRemoveStopsBeforeDeletingConfig(t *testing.T) {
	f := newHAFixture(t)
	if err := applyOxiDNSHA(f.cfg, f.spec); err != nil {
		t.Fatal(err)
	}
	f.addresses = `[{"ifname":"eth0","addr_info":[{"local":"192.168.0.20","prefixlen":24}]}]`
	run := haRun
	stopped := false
	haRun = func(name string, args ...string) (string, error) {
		if name == "systemctl" && len(args) == 2 && args[0] == "stop" {
			stopped = true
			if _, err := os.Stat(haConfigPath()); err != nil {
				t.Errorf("keepalived stopped after its configuration was deleted")
			}
		}
		return run(name, args...)
	}
	if removed, err := removeOxiDNSHA(f.cfg); err != nil || !removed || !stopped || f.running {
		t.Fatalf("remove: %v %v stopped=%v running=%v", removed, err, stopped, f.running)
	}
}
