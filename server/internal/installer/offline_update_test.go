package installer

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Port of backend/test_agent_offline_update.py: update-agent.sh (shell only,
// coordinator decision 2) with every OS path and service mocked.

type updateHarness struct {
	updater, binary, config, events string
	env                             []string
}

func newUpdateHarness(t *testing.T, tmp string) updateHarness {
	python := pythonExe(t)
	pkg := filepath.Join(tmp, "package")
	agentRoot := filepath.Join(tmp, "agents")
	agent := filepath.Join(agentRoot, "mihomo")
	core := filepath.Join(tmp, "mihomo")
	writeFile(t, filepath.Join(core, "config.yaml"), "tun:\n  enable: true\n", 0o644)
	proc := filepath.Join(tmp, "proc")
	for _, pid := range []string{"42", "43", "84"} {
		writeFile(t, filepath.Join(proc, pid, "stat"), pid+" (mihomo worker) S "+strings.Repeat("0 ", 18)+"12345\n", 0o644)
	}
	binary := writeFile(t, filepath.Join(agent, "configflow-agent"), "#!/bin/sh\nprintf '1.8.30-go\\n'\n", 0o755)
	os.Symlink(binary, filepath.Join(proc, "84", "exe"))
	configPath := filepath.Join(core, "config.yaml")
	agentConfig := `{
  "native_aio": true,
  "service_type": "mihomo",
  "service_manager": "systemd",
  "service_unit": "mihomo",
  "agent_service_unit": "configflow-agent-mihomo",
  "agent_host": "0.0.0.0",
  "agent_port": 8080,
  "config_path": "` + configPath + `",
  "token": "PRIVATE-AGENT-TOKEN"
}`
	agentConfigPath := writeFile(t, filepath.Join(agent, "config.json"), agentConfig, 0o644)
	source, _ := Script("update-agent.sh")
	source = strings.ReplaceAll(strings.ReplaceAll(source, "/opt/configflow-agent", agentRoot), "/proc/", proc+"/")
	updater := writeFile(t, filepath.Join(pkg, "update-agent.sh"), source, 0o644)
	writeFile(t, filepath.Join(pkg, "cli-output.sh"), CLIOutputHelpers, 0o644)
	candidateText := "#!" + python + `
import os, sys
if '-version' in sys.argv:
    print('1.8.31-go')
elif '-check-environment' in sys.argv:
    print('FULL-ENVIRONMENT-EVIDENCE: interface Meta belongs to PID 42')
    os.write(3, '[通过] TUN：Meta · 已确认核心持有\n'.encode())
    if os.environ.get('ENVIRONMENT_FAIL'):
        print('environment-specific-failure', file=sys.stderr)
        sys.exit(7)
else:
    print('FULL-DEPLOYMENT-EVIDENCE: no pending journals')
`
	candidate := writeFile(t, filepath.Join(pkg, "configflow-agent-linux-amd64"), candidateText, 0o755)
	writeFile(t, filepath.Join(pkg, "VERSION"), "1.8.31-go\n", 0o644)
	sum := sha256.Sum256([]byte(candidateText))
	writeFile(t, filepath.Join(pkg, "SHA256SUMS"), hex.EncodeToString(sum[:])+"  "+filepath.Base(candidate)+"\n", 0o644)
	commands := filepath.Join(tmp, "commands")
	state := writeFile(t, filepath.Join(tmp, "state.json"), `{"running": true, "core_pid": 42}`, 0o644)
	events := filepath.Join(tmp, "events")
	for name, body := range map[string]string{
		"id":    "printf '0\\n'",
		"uname": "printf 'x86_64\\n'",
		"sleep": ":",
		"flock": `if [ "$1" = -n ] && [ "$2" = 9 ] && [ "${LOCK_BUSY:-0}" = 1 ]; then exit 1; fi`,
		"curl": `if [ "${HEALTH_FAIL:-0}" = 1 ]; then echo health-specific-failure >&2; exit 7; fi
printf '{"status":"ok"}\n' `,
	} {
		writeFile(t, filepath.Join(commands, name), "#!/bin/sh\n"+body+"\n", 0o755)
	}
	writeFile(t, filepath.Join(commands, "systemctl"), "#!"+python+`
import json, os, pathlib, sys
state_path=pathlib.Path(os.environ['STATE'])
state=json.loads(state_path.read_text())
args=sys.argv[1:]
unit=next((a for a in args if a.endswith('.service')), '')
agent_unit='configflow-agent-mihomo.service'
if args[0] in ('stop','start','restart'):
    with open(os.environ['EVENTS'],'a') as out: out.write(' '.join(args)+'\n')
    assert unit==agent_unit, 'core lifecycle was invoked'
    if args[0]=='start' and os.environ.get('START_FAIL') and '1.8.31-go' in pathlib.Path('`+binary+`').read_text():
        print('start-specific-failure',file=sys.stderr)
        sys.exit(1)
    state['running']=args[0]!='stop'
    if args[0]=='stop' and os.environ.get('CORE_CHANGED'): state['core_pid']=43
    state_path.write_text(json.dumps(state))
    print('RAW-SERVICE-DIAGNOSTIC: '+args[0])
elif args[0]=='is-active':
    sys.exit(0 if unit!=agent_unit or state['running'] else 3)
elif args[0]=='show':
    prop=next(a.split('=',1)[1] for a in args if a.startswith('--property='))
    values={'LoadState':'loaded','ExecStart':'{ path=`+binary+` ; argv[]=`+binary+` -config `+agentConfigPath+` ; ignore_errors=no ; }',
             'ControlGroup':'/system.slice/'+unit,
             'MainPID':str((84 if state['running'] else 0) if unit==agent_unit else state['core_pid'])}
    if os.environ.get('UNKNOWN_HOOK'): values['ExecStop']='custom-hook'
    print(values.get(prop,''))
else: raise AssertionError(args)
`, 0o755)
	drop := append(append([]string{}, reportEnvDrop...), "CONFIGFLOW_REPORT_FD", "CONFIGFLOW_REPORT_NESTED")
	var env []string
	for _, kv := range os.Environ() {
		keep := true
		for _, key := range drop {
			if strings.HasPrefix(kv, key+"=") {
				keep = false
			}
		}
		if keep {
			env = append(env, kv)
		}
	}
	env = append(env, "PATH="+commands+":"+os.Getenv("PATH"), "CONFIGFLOW_LOG_DIR="+filepath.Join(tmp, "logs"),
		"STATE="+state, "EVENTS="+events, "NO_COLOR=1")
	return updateHarness{updater: updater, binary: binary, config: agentConfigPath, events: events, env: env}
}

func eventLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

func TestOfflineUpdateRetainsFullLogAndOnlyRestartsAgent(t *testing.T) {
	for _, verbose := range []bool{false, true} {
		tmp := t.TempDir()
		h := newUpdateHarness(t, tmp)
		configBefore := readFile(t, h.config)
		args := []string{"mihomo"}
		if verbose {
			args = append(args, "--verbose")
		}
		result := runShell(t, "sh", h.updater, runOptions{args: args, env: h.env, replace: true, timeout: 30e9})
		output := result.stdout + result.stderr
		if result.code != 0 {
			t.Fatal(output)
		}
		if readFile(t, h.config) != configBefore {
			t.Error("core config changed")
		}
		contains(t, readFile(t, h.binary), "1.8.31-go")
		if got := strings.Join(eventLines(h.events), "|"); got != "stop configflow-agent-mihomo.service|start configflow-agent-mihomo.service" {
			t.Errorf("events %q", got)
		}
		contains(t, output, "核心持续运行")
		contains(t, output, "42")
		notContains(t, output, "\x1b[")
		notContains(t, output, "PRIVATE-AGENT-TOKEN")
		if strings.Contains(output, "FULL-ENVIRONMENT-EVIDENCE") != verbose {
			t.Error("verbose evidence")
		}
		var logs []string
		filepath.Walk(filepath.Join(tmp, "logs"), func(path string, info os.FileInfo, err error) error {
			if err == nil && info.Name() == "install.log" {
				logs = append(logs, path)
			}
			return nil
		})
		if len(logs) != 1 {
			t.Fatalf("logs %v", logs)
		}
		detail := readFile(t, logs[0])
		for _, needle := range []string{"FULL-ENVIRONMENT-EVIDENCE", "FULL-DEPLOYMENT-EVIDENCE", "RAW-SERVICE-DIAGNOSTIC"} {
			contains(t, detail, needle)
		}
		if info, _ := os.Stat(logs[0]); info.Mode().Perm() != 0o600 {
			t.Error("log mode")
		}
	}
}

func TestOfflineUpdateFailureBoundariesDoNotTouchCore(t *testing.T) {
	for _, scenario := range []string{"START_FAIL", "HEALTH_FAIL", "CORE_CHANGED", "LOCK_BUSY", "UNKNOWN_HOOK", "ENVIRONMENT_FAIL"} {
		t.Run(scenario, func(t *testing.T) {
			h := newUpdateHarness(t, t.TempDir())
			original := readFile(t, h.binary)
			configBefore := readFile(t, h.config)
			result := runShell(t, "sh", h.updater, runOptions{args: []string{"--verbose", "mihomo"},
				env: append(h.env, scenario+"=1"), replace: true, timeout: 30e9})
			output := result.stdout + result.stderr
			if (result.code == 0) != (scenario == "ENVIRONMENT_FAIL") {
				t.Fatalf("exit %d\n%s", result.code, output)
			}
			if readFile(t, h.config) != configBefore {
				t.Error("core config changed")
			}
			if scenario != "ENVIRONMENT_FAIL" {
				if readFile(t, h.binary) != original {
					t.Error("binary replaced")
				}
			} else {
				contains(t, readFile(t, h.binary), "1.8.31-go")
				contains(t, output, "需要单独处理")
			}
			actions := eventLines(h.events)
			for _, line := range actions {
				if line != "stop configflow-agent-mihomo.service" && line != "start configflow-agent-mihomo.service" {
					t.Errorf("action %q", line)
				}
			}
			if (scenario == "LOCK_BUSY" || scenario == "UNKNOWN_HOOK") && len(actions) != 0 {
				t.Errorf("actions %v", actions)
			}
			contains(t, output, "完整日志")
			notContains(t, output, "PRIVATE-AGENT-TOKEN")
		})
	}
}

func TestOfflineUpdateMissingToolStopsWithReadableCauseAndLog(t *testing.T) {
	h := newUpdateHarness(t, t.TempDir())
	original := readFile(t, h.binary)
	writeFile(t, h.updater, strings.Replace(readFile(t, h.updater), "for command_name in systemctl ",
		"for command_name in configflow-missing-command systemctl ", 1), 0o644)
	result := runShell(t, "sh", h.updater, runOptions{env: h.env, replace: true})
	output := result.stdout + result.stderr
	if result.code != 1 {
		t.Fatalf("exit %d\n%s", result.code, output)
	}
	contains(t, output, "缺少命令：configflow-missing-command")
	contains(t, output, "完整日志")
	notContains(t, output, "unbound variable")
	if readFile(t, h.binary) != original {
		t.Error("binary replaced")
	}
	if _, err := os.Stat(h.events); err == nil {
		t.Error("services touched")
	}
}
