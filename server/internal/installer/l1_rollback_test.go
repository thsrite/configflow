package installer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestL1PreRollbackInstallerLinkIsStale(t *testing.T) {
	s := &Store{DataDir: t.TempDir(), Fingerprint: legacyInstallerFingerprint}
	token, _, err := s.SaveInstallScript(context.Background(), "#!/bin/sh\necho old\n", nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Fingerprint = ""
	if script, ok, err := s.LoadInstallScript(context.Background(), token); err != nil || !ok || script != StaleScript {
		t.Fatalf("pre-D3 link not refused: ok %v, err %v", ok, err)
	}
}

const l1ManagerMock = `
import hashlib, json, os, sys
from pathlib import Path
args=sys.argv[1:]; command=Path(sys.argv[0]).name
path=Path(os.environ['L1_MANAGER_STATE']); state=json.loads(path.read_text())
with open(os.environ['EVENTS'],'a') as out: out.write(command+' '+' '.join(args)+'\n')
def save(): path.write_text(json.dumps(state))
if command=='systemctl':
    action=args[0]
    if action=='show': print('loaded'); sys.exit(0)
    if action=='daemon-reload': sys.exit(0)
    name=args[-1]
elif command=='rc-service': name,action=args[:2]
else:
    action=args[0]
    if action=='-u': sys.exit(0)
    name=args[1]
if name not in state['services']: sys.exit(0)
item=state['services'][name]
if action=='is-active' or action=='status': sys.exit(0 if item['active'] else 3)
if action=='is-enabled':
    print('enabled' if item['enabled'] else 'disabled'); sys.exit(0 if item['enabled'] else 1)
if action in ('enable','add','disable','del'):
    item['enabled']=action in ('enable','add')
    if command=='rc-update':
        link=Path(os.environ['L1_ETC'])/'runlevels/default'/name
        link.parent.mkdir(parents=True,exist_ok=True)
        if link.is_symlink() or link.exists(): link.unlink()
        if item['enabled']: link.symlink_to(Path(os.environ['L1_ETC'])/'init.d'/name)
    save(); sys.exit(0)
if action=='stop': item['active']=False; save(); sys.exit(0)
if action=='start':
    binary=Path(item['binary']); config=Path(item['config'])
    if not binary.exists() or not config.exists(): sys.exit(4)
    if name==os.environ['L1_FAIL_UNIT'] and b'L1_CANDIDATE' in binary.read_bytes():
        item['active']=False; state['candidate_failed']=True; save(); sys.exit(49)
    state['starts'].append({'unit':name,'binary':hashlib.sha256(binary.read_bytes()).hexdigest(),
        'config':hashlib.sha256(config.read_bytes()).hexdigest()})
    item['active']=True; save(); sys.exit(0)
sys.exit('unhandled manager operation '+repr(args))
`

type l1Service struct {
	Binary  string `json:"binary"`
	Config  string `json:"config"`
	Active  bool   `json:"active"`
	Enabled bool   `json:"enabled"`
}

type l1ManagerState struct {
	Services        map[string]l1Service `json:"services"`
	CandidateFailed bool                 `json:"candidate_failed"`
	Starts          []struct {
		Unit   string `json:"unit"`
		Binary string `json:"binary"`
	} `json:"starts"`
}

func l1SeedManager(t *testing.T, tmp, etc, manager, fail string, services map[string]l1Service) ([]string, string) {
	t.Helper()
	state := filepath.Join(tmp, "l1-manager.json")
	raw, err := json.Marshal(map[string]any{"services": services, "starts": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, state, string(raw), 0o600)
	commands := []string{"systemctl"}
	if manager == "openrc" {
		commands = []string{"rc-service", "rc-update"}
		for name, service := range services {
			if service.Enabled {
				link := filepath.Join(etc, "runlevels/default", name)
				if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(etc, "init.d", name), link); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, command := range commands {
		writeFile(t, filepath.Join(tmp, "commands", command), "#!"+pythonExe(t)+" -S\n"+l1ManagerMock, 0o755)
	}
	return []string{"L1_MANAGER_STATE=" + state, "L1_ETC=" + etc, "L1_FAIL_UNIT=" + fail}, state
}

func l1ReadServices(t *testing.T, path string) map[string]l1Service {
	t.Helper()
	return l1ReadState(t, path).Services
}

func l1ReadState(t *testing.T, path string) l1ManagerState {
	t.Helper()
	var state l1ManagerState
	if err := json.Unmarshal([]byte(readFile(t, path)), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func l1AssertCandidateFailed(t *testing.T, path string) {
	t.Helper()
	if !l1ReadState(t, path).CandidateFailed {
		t.Fatal("the injected candidate-start failure was not reached")
	}
}

func l1CheckFiles(t *testing.T, before map[string]string) {
	t.Helper()
	for path, want := range before {
		if got := readFile(t, path); got != want {
			t.Errorf("file not restored: %s", path)
		}
	}
}

func l1CaptureFiles(t *testing.T, paths ...string) map[string]string {
	t.Helper()
	files := make(map[string]string, len(paths))
	for _, path := range paths {
		files[path] = readFile(t, path)
	}
	return files
}

func TestL1SingleFailureRestoresOldIdentityAndService(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, active := range []bool{false, true} {
			t.Run(map[bool]string{true: "running", false: "stopped"}[active], func(t *testing.T) {
				h := newNativeHarness(t, manager, "oxidns")
				binary := writeFile(t, filepath.Join(h.tmp, "local-bin/configflow-agent"), "#!/bin/sh\n# L1_OLD\nexit 0\n", 0o755)
				writeFile(t, filepath.Join(h.tmp, "fake-agent"), nativeFakeAgent+"\n# L1_CANDIDATE\n", 0o755)
				// The new requested name must not replace the old identity if start fails.
				writeFile(t, h.script, strings.ReplaceAll(readFile(t, h.script), "native-test", "replacement-name"), 0o644)
				unit := filepath.Join(h.tmp, "etc/init.d/configflow-agent")
				if manager == "systemd" {
					unit = filepath.Join(h.tmp, "etc/systemd/system/configflow-agent.service")
				}
				before := l1CaptureFiles(t, binary, h.config, unit, h.gate, h.core, h.dropin, h.agentDropin)
				options, state := l1SeedManager(t, h.tmp, filepath.Join(h.tmp, "etc"), manager, "configflow-agent", map[string]l1Service{
					"configflow-agent": {Binary: binary, Config: h.config, Active: active, Enabled: active},
				})
				r := h.run(t, options...)
				if r.code == 0 {
					t.Fatal("failed candidate reported success")
				}
				l1AssertCandidateFailed(t, state)
				l1CheckFiles(t, before)
				s := l1ReadServices(t, state)["configflow-agent"]
				if s.Active != active || s.Enabled != active {
					t.Fatalf("old service state not restored: %+v", s)
				}
				contains(t, detailOutput(t, r), "已恢复安装前的 Agent 文件、身份和服务状态")
			})
		}
	})
}

func TestL1AIOSecondStartFailureRestoresBothOrRemovesNewFiles(t *testing.T) {
	managers(t, func(t *testing.T, manager string) {
		for _, existing := range []bool{false, true} {
			t.Run(map[bool]string{true: "existing", false: "new"}[existing], func(t *testing.T) {
				h := newAIOHarness(t, manager, existing, func(o *AIOOptions) {
					o.ManageIPv4Forwarding, o.ManageTun = false, false
				})
				candidate := filepath.Join(h.tmp, "fake-agent")
				writeFile(t, candidate, readFile(t, candidate)+"\n# L1_CANDIDATE\n", 0o755)
				services := map[string]l1Service{}
				before := map[string]string{}
				var managed []string
				for _, kind := range []string{"mihomo", "oxidns"} {
					binary, config := h.path("agent/"+kind+"/configflow-agent"), h.path("agent/"+kind+"/config.json")
					unit := filepath.Join(h.units, "configflow-agent-"+kind)
					if manager == "systemd" {
						unit += ".service"
					}
					managed = append(managed, binary, config, unit)
					if existing {
						for path, text := range l1CaptureFiles(t, binary, config, unit) {
							before[path] = text
						}
					}
					services["configflow-agent-"+kind] = l1Service{Binary: binary, Config: config, Active: existing, Enabled: existing}
				}
				options, state := l1SeedManager(t, h.tmp, h.path("etc"), manager, "configflow-agent-oxidns", services)
				r := h.run(t, "1", options...)
				if r.code == 0 {
					t.Fatal("failed second candidate reported success")
				}
				l1AssertCandidateFailed(t, state)
				candidateSHA := sha256.Sum256([]byte(readFile(t, candidate)))
				firstStarted := false
				for _, start := range l1ReadState(t, state).Starts {
					if start.Unit == "configflow-agent-mihomo" && start.Binary == hex.EncodeToString(candidateSHA[:]) {
						firstStarted = true
					}
				}
				if !firstStarted {
					t.Fatal("first new AIO service never started before the second failed")
				}
				if existing {
					l1CheckFiles(t, before)
				} else {
					for _, path := range managed {
						if _, err := os.Lstat(path); !os.IsNotExist(err) {
							t.Errorf("new failed installation left %s: %v", path, err)
						}
					}
				}
				for name, s := range l1ReadServices(t, state) {
					if s.Active != existing || s.Enabled != existing {
						t.Errorf("service %s not restored: %+v", name, s)
					}
				}
			})
		}
	})
}

// A second cancellation during cleanup must not interrupt restoring the old files.
func TestL1RollbackIgnoresFurtherTermination(t *testing.T) {
	h := newNativeHarness(t, "systemd", "oxidns")
	binary := writeFile(t, filepath.Join(h.tmp, "local-bin/configflow-agent"), "#!/bin/sh\n# L1_OLD\nexit 0\n", 0o755)
	writeFile(t, filepath.Join(h.tmp, "fake-agent"), nativeFakeAgent+"\n# L1_CANDIDATE\n", 0o755)
	before := l1CaptureFiles(t, binary, h.config)
	options, state := l1SeedManager(t, h.tmp, filepath.Join(h.tmp, "etc"), "systemd", "configflow-agent", map[string]l1Service{
		"configflow-agent": {Binary: binary, Config: h.config, Active: true, Enabled: true},
	})
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.tmp, "commands/cp"), "#!/bin/sh\ncase \"$2\" in */.agent-install-backup.*/file.*) kill -TERM \"$PPID\" ;; esac\nexec "+shlexQuote(cp)+" \"$@\"\n", 0o755)
	r := h.run(t, options...)
	if r.code == 0 {
		t.Fatal("candidate failure lost")
	}
	l1AssertCandidateFailed(t, state)
	l1CheckFiles(t, before)
	if s := l1ReadServices(t, state)["configflow-agent"]; !s.Active || !s.Enabled {
		t.Fatalf("interrupted rollback: %+v", s)
	}
}

func TestL1FailedRollbackKeepsBackupAndStopsFailedService(t *testing.T) {
	h := newNativeHarness(t, "systemd", "oxidns")
	binary := writeFile(t, filepath.Join(h.tmp, "local-bin/configflow-agent"), "#!/bin/sh\n# L1_OLD\nexit 0\n", 0o755)
	oldConfig := readFile(t, h.config)
	writeFile(t, filepath.Join(h.tmp, "fake-agent"), nativeFakeAgent+"\n# L1_CANDIDATE\n", 0o755)
	options, state := l1SeedManager(t, h.tmp, filepath.Join(h.tmp, "etc"), "systemd", "configflow-agent", map[string]l1Service{
		"configflow-agent": {Binary: binary, Config: h.config, Active: true, Enabled: true},
	})
	cp, err := exec.LookPath("cp")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.tmp, "commands/cp"), "#!/bin/sh\ncase \"$2\" in */.agent-install-backup.*/file.*) exit 71 ;; esac\nexec "+shlexQuote(cp)+" \"$@\"\n", 0o755)
	r := h.run(t, options...)
	if r.code == 0 {
		t.Fatal("failed restore reported success")
	}
	l1AssertCandidateFailed(t, state)
	if s := l1ReadServices(t, state)["configflow-agent"]; s.Active || s.Enabled {
		t.Fatalf("failed new service can keep retrying: %+v", s)
	}
	log := detailOutput(t, r)
	contains(t, log, "服务保持停止")
	contains(t, log, "Agent 安装备份已保留")
	notContains(t, log, "已恢复安装前的 Agent 文件、身份和服务状态")
	backups, err := filepath.Glob(filepath.Join(h.tmp, "agent/.agent-install-backup.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("missing recoverable backup: %v (%v)", backups, err)
	}
	paths, err := filepath.Glob(filepath.Join(backups[0], "path.*"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range paths {
		if strings.TrimSuffix(readFile(t, path), "\n") == h.config {
			found = true
			if readFile(t, filepath.Join(backups[0], "file."+strings.TrimPrefix(filepath.Base(path), "path."))) != oldConfig {
				t.Fatal("backup lost the old config/identity")
			}
		}
	}
	if !found {
		t.Fatal("old config not journaled")
	}
}
