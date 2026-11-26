package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_gateway_agent_uninstall.py: native uninstall menus
// keep gateway journals.

type uninstallHarness struct {
	tmp, script, directory, agentUnit, events string
	env                                       []string
}

func newUninstallHarness(t *testing.T, manager, mode string) uninstallHarness {
	if mode == "aio" {
		h := newAIOHarness(t, manager, true, nil)
		return uninstallHarness{tmp: h.tmp, script: h.installer, directory: h.path("agent/mihomo"), agentUnit: "configflow-agent-mihomo",
			events: h.path("events"), env: append(h.env, "CONFIGFLOW_INSTALL_LOG_DIR="+h.tmp)}
	}
	h := newNativeHarness(t, manager, "mihomo")
	return uninstallHarness{tmp: h.tmp, script: h.script, directory: filepath.Join(h.tmp, "agent"), agentUnit: "configflow-agent",
		events: filepath.Join(h.tmp, "events"), env: append(h.env, "CONFIGFLOW_INSTALL_LOG_DIR="+h.tmp)}
}

func (h uninstallHarness) run(t *testing.T) runResult {
	return runShell(t, "sh", h.script, runOptions{env: h.env, replace: true, input: "2\n", timeout: 30e9})
}

func modesAndManagers(t *testing.T, f func(t *testing.T, manager, mode string)) {
	for _, mode := range []string{"single", "aio"} {
		for _, manager := range []string{"systemd", "openrc"} {
			t.Run(mode+"/"+manager, func(t *testing.T) { f(t, manager, mode) })
		}
	}
}

func regularFiles(t *testing.T, dir string) map[string]string {
	out := map[string]string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			out[path] = readFile(t, path)
		}
		return nil
	})
	return out
}

func TestGatewayJournalBlocksUninstallBeforeStoppingAgents(t *testing.T) {
	modesAndManagers(t, func(t *testing.T, manager, mode string) {
		for _, kind := range []string{"active", "damaged", "dangling-link"} {
			h := newUninstallHarness(t, manager, mode)
			journal := filepath.Join(h.directory, "linux-gateway.json")
			switch kind {
			case "dangling-link":
				os.Symlink(filepath.Join(h.directory, "missing-journal"), journal)
			case "active":
				writeFile(t, journal, `{"active":true}`, 0o644)
			default:
				writeFile(t, journal, "broken journal", 0o644)
			}
			before := regularFiles(t, filepath.Join(h.tmp, "agent"))
			r := h.run(t)
			if r.code == 0 {
				t.Fatalf("%s: uninstalled", kind)
			}
			contains(t, r.stderr, "网关恢复记录未清理")
			if !exists(journal) {
				t.Error("journal removed")
			}
			sameFiles(t, before)
			if data, err := os.ReadFile(h.events); err == nil && strings.Contains(string(data), "stop") {
				t.Error("agents stopped")
			}
		}
	})
}

func TestGatewayCreatedDuringStopPreservesAgentsAndRestartsThem(t *testing.T) {
	modesAndManagers(t, func(t *testing.T, manager, mode string) {
		h := newUninstallHarness(t, manager, mode)
		journal := filepath.Join(h.directory, "linux-gateway.json")
		name := "rc-service"
		stop, start := h.agentUnit+" stop", h.agentUnit+" start"
		if manager == "systemd" {
			name, stop, start = "systemctl", "stop "+h.agentUnit, "start "+h.agentUnit
		}
		command := filepath.Join(h.tmp, "commands", name)
		writeFile(t, command, readFile(t, command)+"\nif [ \"$*\" = \""+stop+"\" ]; then printf active > \"$GATEWAY_JOURNAL\"; fi\n", 0o755)
		h.env = append(h.env, "GATEWAY_JOURNAL="+journal)
		before := regularFiles(t, filepath.Join(h.tmp, "agent"))
		r := h.run(t)
		if r.code == 0 {
			t.Fatal("uninstalled")
		}
		contains(t, r.stderr, "网关恢复记录")
		if readFile(t, journal) != "active" {
			t.Error("journal")
		}
		sameFiles(t, before)
		events := readFile(t, h.events)
		contains(t, events, stop)
		contains(t, events, start)
		notContains(t, events, "disable "+h.agentUnit)
		notContains(t, events, "del "+h.agentUnit)
	})
}

func TestOrdinaryUninstallWithoutGatewayJournalStillCompletes(t *testing.T) {
	modesAndManagers(t, func(t *testing.T, manager, mode string) {
		h := newUninstallHarness(t, manager, mode)
		if r := h.run(t); r.code != 0 {
			t.Fatal(r.stdout + r.stderr)
		}
		if exists(h.directory) {
			t.Error("directory kept")
		}
		removed := "del " + h.agentUnit
		if manager == "systemd" {
			removed = "disable " + h.agentUnit
		}
		contains(t, readFile(t, h.events), removed)
		if mode == "aio" && exists(filepath.Join(h.tmp, "agent/oxidns")) {
			t.Error("oxidns kept")
		}
	})
}
