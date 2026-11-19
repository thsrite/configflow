package installer

// Port of backend/test_docker_ha_guide.py: generated guide commands preserve
// data and refuse ambiguous migrations. The commands are rendered by the Go
// port and run with bash in temporary directories against the same fake
// Docker (a Python stub) as the Python tests. The API case
// test_live_environment_requires_admin_and_never_exposes_extra_payload
// (GET /api/agents/<id>/oxidns-ha-environment) belongs to A18.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func guideParams(t *testing.T, tmp, mode, kind string) *jsonx.Object {
	t.Helper()
	root := filepath.Join(tmp, "persistent")
	os.MkdirAll(root, 0o755)
	identity := filepath.Join(root, "host-id")
	if _, err := os.Stat(identity); err != nil {
		writeFile(t, identity, "unique-host-identity-1234567890\n", 0o644)
	}
	data := jsonx.Obj("mode", mode, "server_url", "http://config.test:8000", "agent_name", "dns-one", "agent_ip", "192.168.10.20",
		"data_dir", filepath.Join(root, "new-data"), "network_mode", "host", "ha_host_id_path", identity, "oxidns_port", 8082,
		"container_name", "original-oxidns", "existing_agent_name", "original-dns", "image", "local.test/agent:guide",
		"ha_data_dir", filepath.Join(root, "new-ha"), "backup_dir", filepath.Join(root, "backup"))
	for _, key := range []string{"agent", "core"} {
		source := filepath.Join(root, "old-"+key)
		os.MkdirAll(source, 0o755)
		value := source
		if kind != "bind" {
			value = "old_" + key
		}
		data.Set(key+"_mount", jsonx.Obj("type", kind, "source", value))
	}
	return data
}

func withChanges(base *jsonx.Object, pairs ...any) *jsonx.Object {
	out := base.DeepCopy()
	out.Update(jsonx.Obj(pairs...))
	return out
}

func bashPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is required")
	}
	return path
}

// runCommand is run(script, tmp_path, env).
func runCommand(t *testing.T, script, tmp string, env ...string) runResult {
	t.Helper()
	path := writeFile(t, filepath.Join(tmp, "command.sh"), script, 0o644)
	return runShell(t, bashPath(t), path, runOptions{env: env, timeout: 30 * time.Second})
}

func mustGuide(t *testing.T, request any, enrollment *Enrollment) *jsonx.Object {
	t.Helper()
	result, err := GenerateGuide(request, enrollment)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func wantGuideValueError(t *testing.T, request any) {
	t.Helper()
	if _, err := GenerateGuide(request, nil); err == nil {
		t.Errorf("no error for %s", jsonx.Repr(request))
	} else if _, ok := err.(*ValueError); !ok {
		t.Errorf("%s: %T %v, want ValueError", jsonx.Repr(request), err, err)
	}
}

func TestIdentityCreateOnceAndNeverOverwriteRegularOrSymlink(t *testing.T) {
	tmp := t.TempDir()
	identity := filepath.Join(tmp, "persistent", "host-id")
	command := ok(IdentityCommand(identity))
	first := runCommand(t, command, tmp)
	if first.code != 0 {
		t.Fatal(first.stderr)
	}
	original := readFile(t, identity)
	if len(strings.TrimSpace(original)) != 32 {
		t.Fatalf("identity %q", original)
	}
	if info, _ := os.Stat(identity); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	if runCommand(t, command, tmp).code == 0 {
		t.Fatal("second run succeeded")
	}
	if readFile(t, identity) != original {
		t.Fatal("identity rewritten")
	}
	dangling := filepath.Join(tmp, "link")
	os.Symlink(filepath.Join(tmp, "absent"), dangling)
	if runCommand(t, ok(IdentityCommand(dangling)), tmp).code == 0 {
		t.Fatal("dangling symlink accepted")
	}
	if _, err := os.Lstat(dangling); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tmp, "absent")); err == nil {
		t.Fatal("symlink target created")
	}
	link := filepath.Join(tmp, "valid-link")
	os.Symlink(identity, link)
	if runCommand(t, ok(IdentityCommand(link)), tmp).code == 0 {
		t.Fatal("symlink accepted")
	}
	if readFile(t, identity) != original {
		t.Fatal("identity rewritten through symlink")
	}
}

func TestIdentityPathIsLiteralAndDoesNotExecuteShell(t *testing.T) {
	tmp := t.TempDir()
	marker := filepath.Join(tmp, "must-not-exist")
	identity := filepath.Join(tmp, "host-$(touch must-not-exist)")
	result := runShell(t, bashPath(t), writeFile(t, filepath.Join(tmp, "command.sh"), ok(IdentityCommand(identity)), 0o644),
		runOptions{dir: tmp})
	if result.code != 0 {
		t.Fatal(result.stderr)
	}
	if _, err := os.Stat(identity); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("command substitution executed")
	}
}

func TestIdentityRejectsNonpersistentAmbiguousPaths(t *testing.T) {
	for _, path := range []string{"relative", "/", "/a/../b", "/a//b", "/a,id", "/a:id", "/a\nid"} {
		if _, err := IdentityCommand(path); err == nil {
			t.Errorf("%q accepted", path)
		} else if _, ok := err.(*ValueError); !ok {
			t.Errorf("%q: %T", path, err)
		}
	}
}

func TestInspectionIsAvailableBeforeFullFormAndNeverDumpsCredentials(t *testing.T) {
	generic := mustGuide(t, jsonx.Obj("action", "inspect"), nil).GetString("inspection_command")
	contains(t, generic, "docker version")
	contains(t, generic, "docker compose version")
	named := mustGuide(t, jsonx.Obj("action", "inspect", "container_name", "existing"), nil).GetString("inspection_command")
	contains(t, named, ".Mounts")
	contains(t, named, "agent_name")
	for _, secret := range []string{".Config.Env", `"token"`, `"agent_id"`} {
		notContains(t, named, secret)
	}
	wantGuideValueError(t, jsonx.Obj("action", "inspect", "container_name", "x;echo bad"))
}

func TestVerificationRejectsInvalidVIP(t *testing.T) {
	for _, vip := range []any{"127.0.0.1", "192.168.1.2;echo", "::1", nil, "0.0.0.0"} {
		wantGuideValueError(t, jsonx.Obj("action", "verify", "vip", vip))
	}
}

func TestVerificationUsesLiteralIPv4(t *testing.T) {
	got := mustGuide(t, jsonx.Obj("action", "verify", "vip", "192.168.10.8"), nil).GetString("verification_command")
	if got != strings.ReplaceAll(haVerification, "{{VIP}}", "192.168.10.8") {
		t.Fatalf("%q", got)
	}
}

func loadCompose(t *testing.T, text string) *jsonx.Object {
	t.Helper()
	value, err := readCompose(text)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestMigrationComposeReusesExactMountAndAgentIdentity(t *testing.T) {
	for _, kind := range []string{"bind", "volume"} {
		t.Run(kind, func(t *testing.T) {
			data := guideParams(t, t.TempDir(), "migrate", kind)
			result := mustGuide(t, data, nil)
			compose := loadCompose(t, result.GetString("compose"))
			services := compose.GetObject("services")
			main, helper := services.GetObject("agent"), services.GetObject("ha-companion")
			if main.Value("container_name") != data.Value("container_name") {
				t.Fatal("container name")
			}
			found := false
			for _, v := range main.GetList("environment") {
				found = found || v == "AGENT_OXIDNS_NAME=original-dns"
			}
			if !found {
				t.Fatal("agent name environment")
			}
			if main.Value("image") != data.Value("image") || helper.Value("image") != data.Value("image") {
				t.Fatal("image")
			}
			for _, pair := range []struct {
				service *jsonx.Object
				ro      bool
			}{{main, false}, {helper, true}} {
				volumes := pair.service.GetList("volumes")
				v0, v1 := volumes[0].(*jsonx.Object), volumes[1].(*jsonx.Object)
				if v0.Value("source") != data.GetObject("agent_mount").Value("source") || v1.Value("source") != data.GetObject("core_mount").Value("source") ||
					v0.Value("type") != kind || v0.Value("read_only") != pair.ro {
					t.Fatalf("volumes %v", jsonx.Repr(volumes))
				}
			}
			if kind == "volume" && !jsonx.Equal(jsonx.Path(compose, "volumes", "old_agent"), jsonx.Obj("external", true, "name", "old_agent")) {
				t.Fatal("external volume")
			}
			install, rollback := result.GetString("install_command"), result.GetString("rollback_command")
			notContains(t, install, "./oxidns_data")
			notContains(t, install+rollback, "docker rm")
			notContains(t, install+rollback, "down -v")
			for _, s := range []string{"docker rename", "docker cp", "oxidns-ha.json", "自定义启动命令"} {
				contains(t, install, s)
			}
			want := []any{jsonx.Obj("path", data.GetString("backup_dir")+"/runtime.env", "format", "raw")}
			if !jsonx.Equal(main.Value("env_file"), want) {
				t.Fatalf("env_file %v", jsonx.Repr(main.Value("env_file")))
			}
		})
	}
}

func TestGenerationRejectsUnsafeOrAmbiguousFields(t *testing.T) {
	changes := [][]any{
		{"agent_mount", jsonx.Obj("type", "bind", "source", "relative")},
		{"agent_mount", jsonx.Obj("type", "volume", "source", "--option")},
		{"ha_data_dir", "/tmp/x", "backup_dir", "/tmp/x/backup"},
		{"container_name", "bad;name"}, {"existing_agent_name", "bad name"},
		{"image", "repo/image:latest;touch bad"}, {"server_url", "file:///tmp/x"},
		{"oxidns_port", true}, {"create_host_identity", "true"},
	}
	data := guideParams(t, t.TempDir(), "migrate", "bind")
	for _, change := range changes {
		wantGuideValueError(t, withChanges(data, change...))
	}
}

func TestAllGeneratedCommandsAreBashSyntaxAndComposeInterpolationSafe(t *testing.T) {
	data := guideParams(t, t.TempDir(), "migrate", "bind")
	data.Set("server_url", data.GetString("server_url")+"/$NOT_A_VARIABLE")
	result := mustGuide(t, data, nil)
	contains(t, result.GetString("compose"), "$$NOT_A_VARIABLE")
	for _, block := range result.GetList("command_blocks") {
		cmd := exec.Command(bashPath(t), "-n")
		cmd.Stdin = strings.NewReader(block.(*jsonx.Object).GetString("command"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", block.(*jsonx.Object).GetString("id"), err, out)
		}
	}
}

const fakeDocker = `#!/usr/bin/env python3
import json, os, sys
from pathlib import Path
args=sys.argv[1:]; path=Path(os.environ['FAKE_STATE']); state=json.loads(path.read_text())
with open(os.environ['FAKE_EVENTS'],'a') as out: out.write(json.dumps(args)+'\n')
def save(): path.write_text(json.dumps(state))
def lookup(value):
    for ident, c in state['containers'].items():
        if value in (ident,c['name']): return ident,c
    sys.exit(1)
if args[0]=='info': print('linux' if '.OSType' in args[-1] else '[]')
elif args[0]=='image': print('[]' if '.Config.' in ' '.join(args) else '{}')
elif args[0]=='network':
    if args[1]=='create':
        state['network']={'name':args[-1], 'owner':args[args.index('--label')+1].split('=',1)[1],
                          'parent':args[args.index('--opt')+1].split('=',1)[1], 'subnet':args[args.index('--subnet')+1],
                          'gateway':args[args.index('--gateway')+1] if '--gateway' in args else ''};save();print('network-id')
    elif not state.get('network'): sys.exit(1)
    elif '--format' in args:
        fmt=args[args.index('--format')+1];net=state['network']
        print('macvlan' if '.Driver' in fmt else net['parent'] if '.Options' in fmt else net['owner'] if '.Labels' in fmt
              else '1' if 'len ' in fmt else net['subnet'] if '.Subnet' in fmt else net['gateway'])
    else: print('{}')
elif args[0] in ('inspect','container'):
    if args[0]=='container': args=args[1:]
    ident,c=lookup(args[-1]); fmt=args[args.index('--format')+1] if '--format' in args else ''
    if '.Id' in fmt: print(ident)
    elif '.Name' in fmt and '.Type' not in fmt and '.RestartPolicy' not in fmt: print('/'+c['name'])
    elif '.State.Running' in fmt: print(str(c.get('running',False)).lower())
    elif 'len .Mounts' in fmt: print(state.get('mount_count',2))
    elif '.Type' in fmt:
        m=state['agent_mount' if '/opt/configflow-agent' in fmt else 'core_mount']
        print(m['type']+'|'+m['source']+'|true')
    elif '.Config.Labels' in fmt: print(c.get('owner',''))
    elif '.Config.' in fmt: print('[]')
    elif '.Image' in fmt: print('original-image')
    elif '.RestartPolicy' in fmt: print('always:0')
    else: print('{}')
elif args[0]=='exec':
    if 'supervisorctl' in args and args[-2:]==['status','oxidns']:
        print('oxidns FATAL' if state.get('fatal') else 'oxidns RUNNING pid 123, uptime 0:00:10');sys.exit(3 if state.get('fatal') else 0)
    if '-i' in args: sys.stdin.read()
    if state.get('reject_existing'): sys.exit(1)
    print('HEARTBEAT_INTERVAL=42\nDEPLOYMENT_HEALTH_TIMEOUT=30\nAGENT_HOST=0.0.0.0')
elif args[0]=='update':
    _,c=lookup(args[-1]); c['restart']=next(x.split('=',1)[1] for x in args if x.startswith('--restart=')); save()
elif args[0] in ('start','stop'):
    ident,c=lookup(args[-1]); c['running']=args[0]=='start';save();print(ident)
elif args[0]=='rename':
    _,c=lookup(args[1]);c['name']=args[2];save()
elif args[0]=='cp': pass
elif args[0]=='run':
	# Probe only; no installation state is changed.
    if '--network' in args and args[args.index('--network')+1]=='host' and '--rm' in args and state.get('port_busy'):
        print('宿主机的 UDP/TCP 53 端口不可用',file=sys.stderr);sys.exit(1)
    if '--rm' in args: sys.exit(1 if state.get('ha_journal') else 0)
    name=args[args.index('--name')+1]; ident=('b' if name.endswith('-ha') else 'c')*64
    if state.get('fail_before_main') and not name.endswith('-ha'):
        if '--cidfile' in args: Path(args[args.index('--cidfile')+1]).write_text('')
        sys.exit(123)
    state['containers'][ident]={'name':name,'running':True, 'owner':args[args.index('--label')+1].split('=',1)[1] if '--label' in args else ''};save()
    if '--cidfile' in args: Path(args[args.index('--cidfile')+1]).write_text(ident+'\n')
    if state.get('fail_main') and not name.endswith('-ha'):
        state['containers'][ident]['running']=False;save();sys.exit(123)
    print(ident)
else: sys.exit('unexpected fake Docker command: '+repr(args))
`

type fakeHostEnv struct {
	state, events string
	env           []string
}

// fakeHost is fake_host(tmp_path, data, **changes).
func fakeHost(t *testing.T, tmp string, data *jsonx.Object, changes ...any) fakeHostEnv {
	t.Helper()
	pythonExe(t)
	writeFile(t, filepath.Join(tmp, "docker"), fakeDocker, 0o755)
	state := jsonx.Obj("containers", jsonx.Obj(strings.Repeat("a", 64), jsonx.Obj("name", data.Value("container_name"), "running", true)),
		"agent_mount", data.Value("agent_mount"), "core_mount", data.Value("core_mount"))
	state.Update(jsonx.Obj(changes...))
	statePath := filepath.Join(tmp, "state.json")
	writeJSONFile(t, statePath, state)
	events := filepath.Join(tmp, "events")
	return fakeHostEnv{statePath, events, []string{"PATH=" + tmp + ":" + os.Getenv("PATH"), "FAKE_STATE=" + statePath, "FAKE_EVENTS=" + events}}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := jsonx.Default(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data), 0o644)
}

func readJSONFile(t *testing.T, path string) *jsonx.Object {
	t.Helper()
	value, err := jsonx.Parse([]byte(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	return value.(*jsonx.Object)
}

func eventCalls(t *testing.T, path string) [][]string {
	t.Helper()
	text, _ := os.ReadFile(path)
	var calls [][]string
	for _, line := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
		if line == "" {
			continue
		}
		value, err := jsonx.Parse([]byte(line))
		if err != nil {
			t.Fatal(err)
		}
		calls = append(calls, jsonx.Strings(value.([]any)))
	}
	return calls
}

func argIndex(args []string, value string) int {
	for i, a := range args {
		if a == value {
			return i
		}
	}
	return -1
}

func firstCall(calls [][]string, name string) int {
	for i, c := range calls {
		if c[0] == name {
			return i
		}
	}
	return -1
}

func anyCall(calls [][]string, names ...string) bool {
	for _, c := range calls {
		for _, n := range names {
			if c[0] == n {
				return true
			}
		}
	}
	return false
}

func TestMigrationAndRollbackUseCapturedIDsAndRestoreRestartPolicy(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	host := fakeHost(t, tmp, data)
	guide := mustGuide(t, data, nil)
	if result := runCommand(t, guide.GetString("install_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	calls := eventCalls(t, host.events)
	stop, backup, rename, start := firstCall(calls, "stop"), firstCall(calls, "cp"), firstCall(calls, "rename"), firstCall(calls, "run")
	if !(stop >= 0 && stop < backup && backup < rename && rename < start) {
		t.Fatalf("order %d %d %d %d", stop, backup, rename, start)
	}
	if strings.TrimSpace(readFile(t, filepath.Join(data.GetString("backup_dir"), "old-id"))) != strings.Repeat("a", 64) {
		t.Fatal("old-id")
	}
	if !strings.HasPrefix(readFile(t, filepath.Join(data.GetString("backup_dir"), "runtime.env")), "HEARTBEAT_INTERVAL=42") {
		t.Fatal("runtime.env")
	}
	if result := runCommand(t, guide.GetString("rollback_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	old := jsonx.Path(readJSONFile(t, host.state), "containers", strings.Repeat("a", 64))
	if !jsonx.Equal(old, jsonx.Obj("name", data.Value("container_name"), "running", true, "restart", "always")) {
		t.Fatalf("old container %v", jsonx.Repr(old))
	}
}

func TestFailedNewContainerCanRollbackWithoutExecIntoIt(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	host := fakeHost(t, tmp, data, "fail_main", true)
	guide := mustGuide(t, data, nil)
	if runCommand(t, guide.GetString("install_command"), tmp, host.env...).code == 0 {
		t.Fatal("install succeeded")
	}
	if result := runCommand(t, guide.GetString("rollback_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	if jsonx.Path(readJSONFile(t, host.state), "containers", strings.Repeat("a", 64), "running") != true {
		t.Fatal("old container not running")
	}
}

func TestUnsupportedMigrationAbortsBeforeStopOrDataChange(t *testing.T) {
	for _, change := range [][]any{{"mount_count", 3}, {"reject_existing", true}} {
		tmp := t.TempDir()
		data := guideParams(t, tmp, "migrate", "bind")
		host := fakeHost(t, tmp, data, change...)
		guide := mustGuide(t, data, nil)
		if runCommand(t, guide.GetString("install_command"), tmp, host.env...).code == 0 {
			t.Fatalf("%v: install succeeded", change)
		}
		if anyCall(eventCalls(t, host.events), "stop", "update", "rename", "cp", "run") {
			t.Fatalf("%v: changing call made", change)
		}
		if _, err := os.Stat(data.GetString("backup_dir")); err == nil {
			t.Fatalf("%v: backup created", change)
		}
	}
}

func TestRollbackRefusesNameReplacedByUnrelatedContainer(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	host := fakeHost(t, tmp, data)
	guide := mustGuide(t, data, nil)
	if runCommand(t, guide.GetString("install_command"), tmp, host.env...).code != 0 {
		t.Fatal("install failed")
	}
	current := readJSONFile(t, host.state)
	containers := current.GetObject("containers")
	containers.GetObject(strings.Repeat("c", 64)).Set("name", "moved-new")
	containers.Set(strings.Repeat("d", 64), jsonx.Obj("name", data.Value("container_name"), "running", true))
	writeJSONFile(t, host.state, current)
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, guide.GetString("rollback_command"), tmp, host.env...).code == 0 {
		t.Fatal("rollback succeeded")
	}
	if anyCall(eventCalls(t, host.events), "stop", "update", "rename") {
		t.Fatal("changing call made")
	}
}

func TestMigrationPreservesOldEnvironmentWithoutEchoingSecrets(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	script := mustGuide(t, data, nil).GetString("install_command")
	// shlex.split(script) yields the check program as one word: the quoted
	// migrationCheck text.
	contains(t, script, " python3 -c "+q(migrationCheck)+" ")
	original := filepath.Join(tmp, "old-agent")
	os.MkdirAll(original, 0o755)
	writeJSONFile(t, filepath.Join(original, "config-oxidns.json"), jsonx.Obj(
		"agent_name", data.Value("existing_agent_name"), "server_url", data.Value("server_url"), "agent_port", data.Value("oxidns_port"),
		"service_type", "oxidns", "config_path", "/etc/oxidns/config.yaml", "heartbeat_interval", 42))
	source := strings.ReplaceAll(migrationCheck, "/opt/configflow-agent", original)
	cmd := exec.Command(pythonExe(t), "-c", source, data.GetString("existing_agent_name"), data.GetString("server_url"), "8082")
	cmd.Stdin = strings.NewReader(`["HTTPS_PROXY=http://private-user:private$password@proxy:8080", "TZ=UTC"]`)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err, stderr.String())
	}
	contains(t, stdout.String(), "HTTPS_PROXY=http://private-user:private$password@proxy:8080")
	compose := loadCompose(t, mustGuide(t, data, nil).GetString("compose"))
	if jsonx.Path(compose, "services", "agent").(*jsonx.Object).GetList("env_file")[0].(*jsonx.Object).Value("format") != "raw" {
		t.Fatal("env_file format")
	}
	contains(t, stdout.String(), "HEARTBEAT_INTERVAL=42")
	contains(t, stdout.String(), "TZ=UTC")
	if stderr.String() != "" {
		t.Fatal(stderr.String())
	}
	contains(t, script, "cf_runtime_env=$(")
	contains(t, script, `"$cf_runtime_env" > "$cf_backup/runtime.env"`)
	contains(t, script, "umask 077")
}

// newHost is new_host(tmp_path, monkeypatch, mode, **changes): bootstrap_command
// replaced by a counter.
func newHost(t *testing.T, tmp, mode string, changes ...any) (*jsonx.Object, fakeHostEnv, *jsonx.Object) {
	t.Helper()
	data := guideParams(t, tmp, "new", "bind")
	if mode == "macvlan" {
		data.Update(jsonx.Obj("network_mode", mode, "ha_parent", "eth0", "ha_subnet", "192.168.10.0/24", "ha_gateway", "192.168.10.1"))
	}
	host := fakeHost(t, tmp, data, changes...)
	state := readJSONFile(t, host.state)
	state.Set("containers", jsonx.NewObject())
	writeJSONFile(t, host.state, state)
	previous := bootstrapCommand
	bootstrapCommand = func(*Capability, GuideRequest) (string, error) {
		return `printf bootstrap >> "$cf_state/bootstrap-count"` + "\n", nil
	}
	t.Cleanup(func() { bootstrapCommand = previous })
	guide := mustGuide(t, data, &Enrollment{URL: "http://config.test/enroll/same-installation", Token: "private"})
	return data, host, guide
}

func setStateField(t *testing.T, path, key string, value any) {
	t.Helper()
	state := readJSONFile(t, path)
	state.Set(key, value)
	writeJSONFile(t, path, state)
}

func TestNewRetryReusesOnlyOwnedCompanionAndResumesFailedMain(t *testing.T) {
	for _, failure := range []string{"fail_main", "fail_before_main"} {
		t.Run(failure, func(t *testing.T) {
			tmp := t.TempDir()
			data, host, guide := newHost(t, tmp, "host", failure, true)
			install := guide.GetString("install_command")
			if first := runCommand(t, install, tmp, host.env...); first.code == 0 {
				t.Fatal(first.stdout)
			}
			setStateField(t, host.state, failure, false)
			if second := runCommand(t, install, tmp, host.env...); second.code != 0 {
				t.Fatal(second.stderr)
			}
			companions := 0
			for _, call := range eventCalls(t, host.events) {
				if call[0] == "run" && argIndex(call, "--name") >= 0 && strings.HasSuffix(call[argIndex(call, "--name")+1], "-ha") {
					companions++
				}
			}
			if companions != 1 {
				t.Fatalf("%d companion starts", companions)
			}
			running := 0
			for _, c := range readJSONFile(t, host.state).GetObject("containers").Values() {
				if c.(*jsonx.Object).Value("running") == true {
					running++
				}
			}
			if running != 2 {
				t.Fatalf("%d running", running)
			}
			before := len(eventCalls(t, host.events))
			if runCommand(t, install, tmp, host.env...).code != 0 {
				t.Fatal("third run failed")
			}
			for _, call := range eventCalls(t, host.events)[before:] {
				if call[0] == "start" || call[0] == "stop" || call[0] == "rename" || (call[0] == "run" && argIndex(call, "--name") >= 0) {
					t.Fatalf("unexpected %v", call)
				}
				if call[0] == "run" && (argIndex(call, "--network") < 0 || call[argIndex(call, "--network")+1] != "none") {
					t.Fatalf("unexpected run %v", call)
				}
			}
			if _, err := os.Stat(filepath.Join(data.GetString("data_dir"), ".ha-install", "setup")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewRetryRejectsUnrelatedSameNameEvenWithCopiedOwnerLabel(t *testing.T) {
	tmp := t.TempDir()
	_, host, guide := newHost(t, tmp, "host", "fail_before_main", true)
	install := guide.GetString("install_command")
	if runCommand(t, install, tmp, host.env...).code == 0 {
		t.Fatal("first run succeeded")
	}
	state := readJSONFile(t, host.state)
	containers := state.GetObject("containers")
	owned, _ := containers.Pop(strings.Repeat("b", 64))
	containers.Set(strings.Repeat("d", 64), owned)
	writeJSONFile(t, host.state, state)
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, install, tmp, host.env...).code == 0 {
		t.Fatal("second run succeeded")
	}
	if anyCall(eventCalls(t, host.events), "run", "start", "stop", "rename") {
		t.Fatal("changing call made")
	}
}

func TestNewRetryReusesOwnedMacvlanButRejectsChangedNetwork(t *testing.T) {
	tmp := t.TempDir()
	_, host, guide := newHost(t, tmp, "macvlan", "fail_before_main", true)
	install := guide.GetString("install_command")
	if runCommand(t, install, tmp, host.env...).code == 0 {
		t.Fatal("first run succeeded")
	}
	setStateField(t, host.state, "fail_before_main", false)
	if second := runCommand(t, install, tmp, host.env...); second.code != 0 {
		t.Fatal(second.stderr)
	}
	creates := 0
	for _, call := range eventCalls(t, host.events) {
		if len(call) >= 2 && call[0] == "network" && call[1] == "create" {
			creates++
		}
	}
	if creates != 1 {
		t.Fatalf("%d network creates", creates)
	}
	before := len(eventCalls(t, host.events))
	if runCommand(t, install, tmp, host.env...).code != 0 {
		t.Fatal("third run failed")
	}
	tail := eventCalls(t, host.events)[before:]
	proof := false
	for _, call := range tail {
		if call[0] == "run" && argIndex(call, "--network") >= 0 && call[argIndex(call, "--network")+1] == "none" {
			proof = true
		}
		if (len(call) >= 2 && call[0] == "network" && call[1] == "create") || (call[0] == "run" && argIndex(call, "--name") >= 0) {
			t.Fatalf("unexpected %v", call)
		}
	}
	if !proof {
		t.Fatal("no local identity proof")
	}
	state := readJSONFile(t, host.state)
	state.GetObject("network").Set("parent", "other0")
	writeJSONFile(t, host.state, state)
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, install, tmp, host.env...).code == 0 {
		t.Fatal("changed network accepted")
	}
	if anyCall(eventCalls(t, host.events), "run", "start", "stop", "rename") {
		t.Fatal("changing call made")
	}
}

func TestNewRetryRejectsChangedSettingsAndDoesNotRewriteMarker(t *testing.T) {
	tmp := t.TempDir()
	data, host, guide := newHost(t, tmp, "host", "fail_before_main", true)
	if runCommand(t, guide.GetString("install_command"), tmp, host.env...).code == 0 {
		t.Fatal("first run succeeded")
	}
	marker := filepath.Join(data.GetString("data_dir"), ".ha-install", "setup")
	before := readFile(t, marker)
	changed := mustGuide(t, withChanges(data, "oxidns_port", 9092), &Enrollment{URL: "http://config.test/enroll/same-installation"})
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, changed.GetString("install_command"), tmp, host.env...).code == 0 {
		t.Fatal("changed settings accepted")
	}
	if readFile(t, marker) != before {
		t.Fatal("marker rewritten")
	}
	if anyCall(eventCalls(t, host.events), "run", "start", "stop", "rename") {
		t.Fatal("changing call made")
	}
}
