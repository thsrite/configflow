package installer

// Port of backend/test_docker_ha_guide.py: generated guide commands preserve
// data and refuse ambiguous migrations. The commands are rendered by the Go
// port and run with bash in temporary directories against the same fake
// Docker (a Python stub) as the Python tests. The API case
// test_live_environment_requires_admin_and_never_exposes_extra_payload
// (GET /api/agents/<id>/oxidns-ha-environment) belongs to A18.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"configflow/internal/testjson"
)

func guideParams(t *testing.T, tmp, mode, kind string) map[string]any {
	t.Helper()
	root := filepath.Join(tmp, "persistent")
	os.MkdirAll(root, 0o755)
	identity := filepath.Join(root, "host-id")
	if _, err := os.Stat(identity); err != nil {
		writeFile(t, identity, "unique-host-identity-1234567890\n", 0o644)
	}
	data := map[string]any{
		"mode":                mode,
		"server_url":          "http://config.test:8000",
		"agent_name":          "dns-one",
		"agent_ip":            "192.168.10.20",
		"data_dir":            filepath.Join(root, "new-data"),
		"network_mode":        "host",
		"ha_host_id_path":     identity,
		"oxidns_port":         8082,
		"container_name":      "original-oxidns",
		"existing_agent_name": "original-dns",
		"image":               "local.test/agent:guide",
		"ha_data_dir":         filepath.Join(root, "new-ha"),
		"backup_dir":          filepath.Join(root, "backup"),
	}
	for _, key := range []string{"agent", "core"} {
		source := filepath.Join(root, "old-"+key)
		os.MkdirAll(source, 0o755)
		value := source
		if kind != "bind" {
			value = "old_" + key
		}
		data[key+"_mount"] = map[string]any{"type": kind, "source": value}
	}
	return data
}

func withChanges(base map[string]any, pairs ...any) map[string]any {
	out := cloneMap(base)
	for i := 0; i+1 < len(pairs); i += 2 {
		out[pairs[i].(string)] = pairs[i+1]
	}
	return out
}

func bashPath(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash is required on the test server: %v", err)
	}
	return path
}

// runCommand is run(script, tmp_path, env).
func runCommand(t *testing.T, script, tmp string, env ...string) runResult {
	t.Helper()
	path := writeFile(t, filepath.Join(tmp, "command.sh"), script, 0o644)
	return runShell(t, bashPath(t), path, runOptions{env: env, timeout: 30 * time.Second})
}

func mustGuide(t *testing.T, request any, enrollment *Enrollment) map[string]any {
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
		t.Errorf("no error for %v", request)
	} else if _, ok := err.(*ValueError); !ok {
		t.Errorf("%v: %T %v, want ValueError", request, err, err)
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
	generic := mapStr(mustGuide(t, map[string]any{"action": "inspect"}, nil), "inspection_command")
	contains(t, generic, "docker version")
	contains(t, generic, "docker compose version")
	named := mapStr(mustGuide(t, map[string]any{"action": "inspect", "container_name": "existing"}, nil), "inspection_command")
	contains(t, named, ".Mounts")
	contains(t, named, "agent_name")
	for _, secret := range []string{".Config.Env", `"token"`, `"agent_id"`} {
		notContains(t, named, secret)
	}
	wantGuideValueError(t, map[string]any{"action": "inspect", "container_name": "x;echo bad"})
}

func TestVerificationRejectsInvalidVIP(t *testing.T) {
	for _, vip := range []any{"127.0.0.1", "192.168.1.2;echo", "::1", nil, "0.0.0.0"} {
		wantGuideValueError(t, map[string]any{"action": "verify", "vip": vip})
	}
}

func TestVerificationUsesLiteralIPv4(t *testing.T) {
	got := mapStr(mustGuide(t, map[string]any{"action": "verify", "vip": "192.168.10.8"}, nil), "verification_command")
	if got != strings.ReplaceAll(haVerification, "{{VIP}}", "192.168.10.8") {
		t.Fatalf("%q", got)
	}
}

func loadCompose(t *testing.T, text string) map[string]any {
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
			compose := loadCompose(t, mapStr(result, "compose"))
			services := mapObj(compose, "services")
			main, helper := mapObj(services, "agent"), mapObj(services, "ha-companion")
			if main["container_name"] != data["container_name"] {
				t.Fatal("container name")
			}
			found := false
			for _, v := range mapList(main, "environment") {
				found = found || v == "AGENT_OXIDNS_NAME=original-dns"
			}
			if !found {
				t.Fatal("agent name environment")
			}
			if main["image"] != data["image"] || helper["image"] != data["image"] {
				t.Fatal("image")
			}
			for _, pair := range []struct {
				service map[string]any
				ro      bool
			}{{main, false}, {helper, true}} {
				volumes := mapList(pair.service, "volumes")
				v0, v1 := volumes[0].(map[string]any), volumes[1].(map[string]any)
				if v0["source"] != mapObj(data, "agent_mount")["source"] || v1["source"] != mapObj(data, "core_mount")["source"] ||
					v0["type"] != kind || v0["read_only"] != pair.ro {
					t.Fatalf("volumes %v", volumes)
				}
			}
			if kind == "volume" && !testjson.Equal(jsonPath(compose, "volumes", "old_agent"), map[string]any{"external": true, "name": "old_agent"}) {
				t.Fatal("external volume")
			}
			install, rollback := mapStr(result, "install_command"), mapStr(result, "rollback_command")
			notContains(t, install, "./oxidns_data")
			notContains(t, install+rollback, "docker rm")
			notContains(t, install+rollback, "down -v")
			for _, s := range []string{"docker rename", "docker cp", "oxidns-ha.json", "自定义启动命令"} {
				contains(t, install, s)
			}
			want := []any{map[string]any{"path": mapStr(data, "backup_dir") + "/runtime.env", "format": "raw"}}
			if !testjson.Equal(main["env_file"], want) {
				t.Fatalf("env_file %v", main["env_file"])
			}
		})
	}
}

func TestGenerationRejectsUnsafeOrAmbiguousFields(t *testing.T) {
	changes := [][]any{
		{"agent_mount", map[string]any{"type": "bind", "source": "relative"}},
		{"agent_mount", map[string]any{"type": "volume", "source": "--option"}},
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
	data["server_url"] = mapStr(data, "server_url") + "/$NOT_A_VARIABLE"
	result := mustGuide(t, data, nil)
	contains(t, mapStr(result, "compose"), "$$NOT_A_VARIABLE")
	for _, block := range mapList(result, "command_blocks") {
		blk := block.(map[string]any)
		cmd := exec.Command(bashPath(t), "-n")
		cmd.Stdin = strings.NewReader(mapStr(blk, "command"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", mapStr(blk, "id"), err, out)
		}
	}
}

type fakeHostEnv struct {
	state, events string
	env           []string
}

// fakeHost is fake_host(tmp_path, data, **changes).
func fakeHost(t *testing.T, tmp string, data map[string]any, changes ...any) fakeHostEnv {
	t.Helper()
	writeFile(t, filepath.Join(tmp, "docker"), stubScript(t, "guide-docker", ""), 0o755)
	state := map[string]any{
		"containers": map[string]any{
			strings.Repeat("a", 64): map[string]any{"name": data["container_name"], "running": true},
		},
		"agent_mount": data["agent_mount"],
		"core_mount":  data["core_mount"],
	}
	for i := 0; i+1 < len(changes); i += 2 {
		state[changes[i].(string)] = changes[i+1]
	}
	statePath := filepath.Join(tmp, "state.json")
	writeJSONFile(t, statePath, state)
	events := filepath.Join(tmp, "events")
	return fakeHostEnv{statePath, events, []string{"PATH=" + tmp + ":" + os.Getenv("PATH"), "FAKE_STATE=" + statePath, "FAKE_EVENTS=" + events}}
}

func writeJSONFile(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(data), 0o644)
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	value, err := parseJSONMap([]byte(readFile(t, path)))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func eventCalls(t *testing.T, path string) [][]string {
	t.Helper()
	text, _ := os.ReadFile(path)
	var calls [][]string
	for _, line := range strings.Split(strings.TrimRight(string(text), "\n"), "\n") {
		if line == "" {
			continue
		}
		var call []string
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatal(err)
		}
		calls = append(calls, call)
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
	if result := runCommand(t, mapStr(guide, "install_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	calls := eventCalls(t, host.events)
	stop, backup, rename, start := firstCall(calls, "stop"), firstCall(calls, "cp"), firstCall(calls, "rename"), firstCall(calls, "run")
	if !(stop >= 0 && stop < backup && backup < rename && rename < start) {
		t.Fatalf("order %d %d %d %d", stop, backup, rename, start)
	}
	if strings.TrimSpace(readFile(t, filepath.Join(mapStr(data, "backup_dir"), "old-id"))) != strings.Repeat("a", 64) {
		t.Fatal("old-id")
	}
	if !strings.HasPrefix(readFile(t, filepath.Join(mapStr(data, "backup_dir"), "runtime.env")), "HEARTBEAT_INTERVAL=42") {
		t.Fatal("runtime.env")
	}
	if result := runCommand(t, mapStr(guide, "rollback_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	old := jsonPath(readJSONFile(t, host.state), "containers", strings.Repeat("a", 64))
	if !testjson.Equal(old, map[string]any{"name": data["container_name"], "running": true, "restart": "always"}) {
		t.Fatalf("old container %v", old)
	}
}

func TestFailedNewContainerCanRollbackWithoutExecIntoIt(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	host := fakeHost(t, tmp, data, "fail_main", true)
	guide := mustGuide(t, data, nil)
	if runCommand(t, mapStr(guide, "install_command"), tmp, host.env...).code == 0 {
		t.Fatal("install succeeded")
	}
	if result := runCommand(t, mapStr(guide, "rollback_command"), tmp, host.env...); result.code != 0 {
		t.Fatal(result.stderr)
	}
	if jsonPath(readJSONFile(t, host.state), "containers", strings.Repeat("a", 64), "running") != true {
		t.Fatal("old container not running")
	}
}

func TestUnsupportedMigrationAbortsBeforeStopOrDataChange(t *testing.T) {
	for _, change := range [][]any{{"mount_count", 3}, {"reject_existing", true}} {
		tmp := t.TempDir()
		data := guideParams(t, tmp, "migrate", "bind")
		host := fakeHost(t, tmp, data, change...)
		guide := mustGuide(t, data, nil)
		if runCommand(t, mapStr(guide, "install_command"), tmp, host.env...).code == 0 {
			t.Fatalf("%v: install succeeded", change)
		}
		if anyCall(eventCalls(t, host.events), "stop", "update", "rename", "cp", "run") {
			t.Fatalf("%v: changing call made", change)
		}
		if _, err := os.Stat(mapStr(data, "backup_dir")); err == nil {
			t.Fatalf("%v: backup created", change)
		}
	}
}

func TestRollbackRefusesNameReplacedByUnrelatedContainer(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	host := fakeHost(t, tmp, data)
	guide := mustGuide(t, data, nil)
	if runCommand(t, mapStr(guide, "install_command"), tmp, host.env...).code != 0 {
		t.Fatal("install failed")
	}
	current := readJSONFile(t, host.state)
	containers := mapObj(current, "containers")
	mapObj(containers, strings.Repeat("c", 64))["name"] = "moved-new"
	containers[strings.Repeat("d", 64)] = map[string]any{"name": data["container_name"], "running": true}
	writeJSONFile(t, host.state, current)
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, mapStr(guide, "rollback_command"), tmp, host.env...).code == 0 {
		t.Fatal("rollback succeeded")
	}
	if anyCall(eventCalls(t, host.events), "stop", "update", "rename") {
		t.Fatal("changing call made")
	}
}

func TestMigrationPreservesOldEnvironmentWithoutEchoingSecrets(t *testing.T) {
	tmp := t.TempDir()
	data := guideParams(t, tmp, "migrate", "bind")
	script := mapStr(mustGuide(t, data, nil), "install_command")
	// shlex.split(script) yields the check program as one word: the quoted
	// migrationCheck text.
	contains(t, script, " python3 -c "+q(migrationCheck)+" ")
	original := filepath.Join(tmp, "old-agent")
	os.MkdirAll(original, 0o755)
	writeJSONFile(t, filepath.Join(original, "config-oxidns.json"), map[string]any{
		"agent_name":         data["existing_agent_name"],
		"server_url":         data["server_url"],
		"agent_port":         data["oxidns_port"],
		"service_type":       "oxidns",
		"config_path":        "/etc/oxidns/config.yaml",
		"heartbeat_interval": 42,
	})
	source := strings.ReplaceAll(migrationCheck, "/opt/configflow-agent", original)
	cmd := exec.Command(productPython(t), "-c", source, mapStr(data, "existing_agent_name"), mapStr(data, "server_url"), "8082")
	cmd.Stdin = strings.NewReader(`["HTTPS_PROXY=http://private-user:private$password@proxy:8080", "TZ=UTC"]`)
	cmd.Env = append(os.Environ(), "TZ=UTC")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err, stderr.String())
	}
	contains(t, stdout.String(), "HTTPS_PROXY=http://private-user:private$password@proxy:8080")
	compose := loadCompose(t, mapStr(mustGuide(t, data, nil), "compose"))
	if mapList(jsonPath(compose, "services", "agent").(map[string]any), "env_file")[0].(map[string]any)["format"] != "raw" {
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
func newHost(t *testing.T, tmp, mode string, changes ...any) (map[string]any, fakeHostEnv, map[string]any) {
	t.Helper()
	data := guideParams(t, tmp, "new", "bind")
	if mode == "macvlan" {
		data["network_mode"] = mode
		data["ha_parent"] = "eth0"
		data["ha_subnet"] = "192.168.10.0/24"
		data["ha_gateway"] = "192.168.10.1"
	}
	host := fakeHost(t, tmp, data, changes...)
	state := readJSONFile(t, host.state)
	state["containers"] = map[string]any{}
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
	state[key] = value
	writeJSONFile(t, path, state)
}

func TestNewRetryReusesOnlyOwnedCompanionAndResumesFailedMain(t *testing.T) {
	for _, failure := range []string{"fail_main", "fail_before_main"} {
		t.Run(failure, func(t *testing.T) {
			tmp := t.TempDir()
			data, host, guide := newHost(t, tmp, "host", failure, true)
			install := mapStr(guide, "install_command")
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
			for _, c := range mapObj(readJSONFile(t, host.state), "containers") {
				if c.(map[string]any)["running"] == true {
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
			if _, err := os.Stat(filepath.Join(mapStr(data, "data_dir"), ".ha-install", "setup")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNewRetryRejectsUnrelatedSameNameEvenWithCopiedOwnerLabel(t *testing.T) {
	tmp := t.TempDir()
	_, host, guide := newHost(t, tmp, "host", "fail_before_main", true)
	install := mapStr(guide, "install_command")
	if runCommand(t, install, tmp, host.env...).code == 0 {
		t.Fatal("first run succeeded")
	}
	state := readJSONFile(t, host.state)
	containers := mapObj(state, "containers")
	owned := containers[strings.Repeat("b", 64)]
	delete(containers, strings.Repeat("b", 64))
	containers[strings.Repeat("d", 64)] = owned
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
	install := mapStr(guide, "install_command")
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
	mapObj(state, "network")["parent"] = "other0"
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
	if runCommand(t, mapStr(guide, "install_command"), tmp, host.env...).code == 0 {
		t.Fatal("first run succeeded")
	}
	marker := filepath.Join(mapStr(data, "data_dir"), ".ha-install", "setup")
	before := readFile(t, marker)
	changed := mustGuide(t, withChanges(data, "oxidns_port", 9092), &Enrollment{URL: "http://config.test/enroll/same-installation"})
	os.WriteFile(host.events, nil, 0o644)
	if runCommand(t, mapStr(changed, "install_command"), tmp, host.env...).code == 0 {
		t.Fatal("changed settings accepted")
	}
	if readFile(t, marker) != before {
		t.Fatal("marker rewritten")
	}
	if anyCall(eventCalls(t, host.events), "run", "start", "stop", "rename") {
		t.Fatal("changing call made")
	}
}
