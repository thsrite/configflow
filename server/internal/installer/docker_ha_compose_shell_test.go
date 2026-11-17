package installer

// Hand-ported shell/runtime scenarios of backend/test_docker_ha_compose.py
// (module A10): the Go-rendered bundle is unpacked, its prepare.sh runs with
// bash against the same fake docker (a Python stub that refuses every
// mutating operation), and its bootstrap.py runs with python3 against a
// loopback enrollment origin. Everything stays in temp directories.

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

var composeTestToken = strings.Repeat("t", 43)

// composeFakeDocker is FAKE_DOCKER of test_docker_ha_compose.py.
const composeFakeDocker = `#!/usr/bin/env python3
import json,os,re,sys
from pathlib import Path
args=sys.argv[1:]; state=json.loads(Path(os.environ['CF_COMPOSE_STATE']).read_text())
with open(os.environ['CF_COMPOSE_EVENTS'],'a') as stream: stream.write(json.dumps(args)+'\n')
if args[:2]==['context','show']: print('default')
elif args[:2]==['context','inspect']: print(state.get('endpoint','unix:///var/run/docker.sock'))
elif args[0]=='info':
    fmt=args[-1]; print('linux' if '.OSType' in fmt else '[]' if '.SecurityOptions' in fmt else 'Arch Linux 6.18')
elif args[:2]==['compose','version']: print(state.get('version','2.20.1'))
elif args[:2]==['container','inspect']:
    item=state.get('containers',{}).get(args[-1]);
    if item is None: sys.exit(1)
    fmt=args[args.index('--format')+1] if '--format' in args else ''
    if '.Labels' in fmt: print(item['labels'].get(re.search(r'Labels "([^"]+)"',fmt)[1],''))
    elif '.Mounts' in fmt:
        target=re.search(r'\.Destination "([^"]+)"',fmt)[1]
        mount=next((m for m in item['volumes'] if m['target']==target),None)
        print('bind|'+mount['source']+'|'+str(not mount.get('read_only',False)).lower() if mount else '')
    else: print('{}')
elif args[:2]==['network','inspect']:
    item=state.get('networks',{}).get(args[-1]);
    if item is None: sys.exit(1)
    fmt=args[args.index('--format')+1] if '--format' in args else ''
    print(item['driver'] if '.Driver' in fmt else item['driver_opts']['parent'] if '.Options' in fmt else
          item['labels'].get(re.search(r'Labels "([^"]+)"',fmt)[1],'') if '.Labels' in fmt else
          '1' if 'len .IPAM' in fmt else item['ipam']['config'][0]['subnet'] if '.Subnet' in fmt else
          item['ipam']['config'][0].get('gateway','') if '.Gateway' in fmt else '{}')
else: sys.exit('unexpected mutating Docker operation '+repr(args))
`

func composeParams(dir string, changes ...any) *jsonx.Object {
	identity := filepath.Join(dir, "host-id")
	if _, err := os.Stat(identity); err != nil {
		os.WriteFile(identity, []byte("unique-compose-host-identity-1234567890\n"), 0o644)
	}
	body := jsonx.Obj("install_method", "compose", "mode", "new", "server_url", "http://panel.example",
		"agent_name", "compose-dns", "agent_ip", "192.168.10.20", "oxidns_port", 8082,
		"data_dir", filepath.Join(dir, "data"), "network_mode", "host", "ha_host_id_path", identity,
		"image", "local.test/agent:compose")
	body.Update(jsonx.Obj(changes...))
	return body
}

func composeCapability(url string) *Enrollment {
	return &Enrollment{URL: url + "/api/agents/docker-oxidns-ha-enroll/" + strings.Repeat("a", 32), Token: composeTestToken}
}

func composeGuide(t *testing.T, body *jsonx.Object, e *Enrollment) *jsonx.Object {
	t.Helper()
	result, err := GenerateGuide(jsonx.DeepCopy(body), e)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// composeUnpack is unpack(result, destination).
func composeUnpack(t *testing.T, result *jsonx.Object, destination string) string {
	t.Helper()
	os.MkdirAll(destination, 0o755)
	archive, _ := base64.StdEncoding.DecodeString(result.GetObject("compose_bundle").GetString("content_base64"))
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range reader.File {
		if filepath.Base(file.Name) != file.Name {
			t.Fatalf("entry %q", file.Name)
		}
		stream, _ := file.Open()
		data, _ := io.ReadAll(stream)
		stream.Close()
		path := filepath.Join(destination, file.Name)
		os.WriteFile(path, data, 0o600)
		os.Chmod(path, os.FileMode(file.ExternalAttrs>>16)&0o777)
	}
	return destination
}

type composeOrigin struct {
	url      string
	mu       sync.Mutex
	status   int
	location string
	identity string
	calls    []string
}

func newComposeOrigin(t *testing.T) *composeOrigin {
	o := &composeOrigin{status: 200, identity: `{"success": true, "agent_id": "agent_` + strings.Repeat("a", 32) + `", "token": "` + strings.Repeat("z", 32) + `"}`}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		o.mu.Lock()
		o.calls = append(o.calls, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization")+" "+string(body))
		status, location, identity := o.status, o.location, o.identity
		o.mu.Unlock()
		if location != "" {
			w.Header().Set("Location", location)
		}
		w.WriteHeader(status)
		io.WriteString(w, identity)
	}))
	t.Cleanup(server.Close)
	o.url = server.URL
	return o
}

func (o *composeOrigin) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.calls)
}

func (o *composeOrigin) setResponse(status int, location string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status, o.location = status, location
}

func (o *composeOrigin) recordedCalls() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.calls...)
}

// composeBootstrapFiles is bootstrap_files(tmp_path, cap).
func composeBootstrapFiles(t *testing.T, dir string, e *Enrollment) (files, data, setup string) {
	files = composeUnpack(t, composeGuide(t, composeParams(dir), e), filepath.Join(dir, "bundle"))
	data = filepath.Join(dir, "runtime-agent")
	os.Mkdir(data, 0o755)
	raw, _ := os.ReadFile(filepath.Join(files, "enrollment.json"))
	var settings map[string]string
	json.Unmarshal(raw, &settings)
	setup = filepath.Join(dir, "setup")
	os.WriteFile(setup, []byte(settings["owner"]), 0o644)
	return
}

// composeExecuteBootstrap is execute_bootstrap(files, data, setup).
func composeExecuteBootstrap(t *testing.T, files, data, setup string) runResult {
	t.Helper()
	runner := "import runpy,sys; m=runpy.run_path(sys.argv[1]); m['bootstrap'](*sys.argv[2:]); print('ENTRYPOINT_ALLOWED')"
	cmd := exec.Command(pythonExe(t), "-c", runner, filepath.Join(files, "bootstrap.py"), filepath.Join(files, "enrollment.json"), data, setup)
	cmd.Env = append(os.Environ(), "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	done := make(chan error, 1)
	cmd.Start()
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		<-done
	}
	return runResult{code: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}
}

func composeReadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestBundleBootstrapFirstEnrollmentThenExpiredOfflineRestart(t *testing.T) {
	dir := t.TempDir()
	origin := newComposeOrigin(t)
	files, data, setup := composeBootstrapFiles(t, dir, composeCapability(origin.url))
	first := composeExecuteBootstrap(t, files, data, setup)
	if first.code != 0 || !strings.Contains(first.stdout, "ENTRYPOINT_ALLOWED") || strings.Contains(first.stdout+first.stderr, composeTestToken) {
		t.Fatalf("first: %+v", first)
	}
	calls := origin.recordedCalls()
	if want := "POST /api/agents/docker-oxidns-ha-enroll/" + strings.Repeat("a", 32) + " Bearer " + composeTestToken + " {}"; len(calls) != 1 || calls[0] != want {
		t.Fatalf("calls %q", calls)
	}
	config := composeReadJSON(t, filepath.Join(data, "config-oxidns.json"))
	if len(config) != 2 || config["agent_id"] != "agent_"+strings.Repeat("a", 32) || config["token"] != strings.Repeat("z", 32) {
		t.Fatalf("config %v", config)
	}
	if info, _ := os.Stat(filepath.Join(data, "config-oxidns.json")); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	config["service_type"], config["server_url"], config["config_version"] = "oxidns", "http://panel.example", "42"
	updated, _ := json.Marshal(config)
	os.WriteFile(filepath.Join(data, "config-oxidns.json"), updated, 0o600)
	origin.setResponse(401, "")
	settings := composeReadJSON(t, filepath.Join(files, "enrollment.json"))
	settings["token"] = "expired"
	raw, _ := json.Marshal(settings)
	os.WriteFile(filepath.Join(files, "enrollment.json"), raw, 0o600)
	if second := composeExecuteBootstrap(t, files, data, setup); second.code != 0 || origin.count() != 1 {
		t.Fatalf("second: %+v calls %d", second, origin.count())
	}
	origin.setResponse(500, "") // the origin "gone": a saved identity must not contact it
	if third := composeExecuteBootstrap(t, files, data, setup); third.code != 0 || origin.count() != 1 {
		t.Fatalf("third: %+v", third)
	}
	if got, _ := os.ReadFile(filepath.Join(data, "config-oxidns.json")); string(got) != string(updated) {
		t.Fatalf("identity changed: %s", got)
	}
	if !strings.Contains(composeBootstrap, "os.execv('/entrypoint.sh', ['/entrypoint.sh', '/usr/bin/supervisord', '-c', '/etc/supervisor/supervisord.conf'])") {
		t.Fatal("entrypoint exec missing")
	}
}

func TestBundleBootstrapExpiredFirstAttemptStaysStoppedAndRetries(t *testing.T) {
	dir := t.TempDir()
	origin := newComposeOrigin(t)
	files, data, setup := composeBootstrapFiles(t, dir, composeCapability(origin.url))
	origin.setResponse(401, "")
	failed := composeExecuteBootstrap(t, files, data, setup)
	if failed.code == 0 || strings.Contains(failed.stdout, "ENTRYPOINT_ALLOWED") {
		t.Fatalf("%+v", failed)
	}
	if _, err := os.Stat(filepath.Join(data, "config-oxidns.json")); err == nil {
		t.Fatal("identity written")
	}
	origin.setResponse(200, "")
	if ok := composeExecuteBootstrap(t, files, data, setup); ok.code != 0 || origin.count() != 2 {
		t.Fatalf("%+v %d", ok, origin.count())
	}
}

func composeSnapshot(dir string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() == ".compose-enrollment.lock" {
			continue
		}
		info, err := os.Stat(filepath.Join(dir, e.Name()))
		if err == nil && info.Mode().IsRegular() {
			data, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			out[e.Name()] = string(data)
		}
	}
	return out
}

func TestBundleBootstrapRefusesForeignOrCorruptState(t *testing.T) {
	for _, foreign := range []string{"identity", "marker", "symlink-identity", "symlink-marker", "corrupt-identity", "changed-identity", "wrong-setup"} {
		t.Run(foreign, func(t *testing.T) {
			dir := t.TempDir()
			origin := newComposeOrigin(t)
			e := composeCapability(origin.url)
			files, data, setup := composeBootstrapFiles(t, dir, e)
			path, marker := filepath.Join(data, "config-oxidns.json"), filepath.Join(data, ".docker-ha-installation.json")
			switch {
			case foreign == "changed-identity":
				if r := composeExecuteBootstrap(t, files, data, setup); r.code != 0 {
					t.Fatalf("%+v", r)
				}
				value := composeReadJSON(t, path)
				value["token"] = "another-token-value-1234567890"
				raw, _ := json.Marshal(value)
				os.WriteFile(path, raw, 0o600)
			case foreign == "wrong-setup":
				os.WriteFile(setup, []byte(strings.Repeat("b", 64)), 0o644)
			case foreign == "marker":
				raw, _ := json.Marshal(map[string]string{"url": e.URL, "owner": strings.Repeat("b", 64)})
				os.WriteFile(marker, raw, 0o600)
			case strings.HasPrefix(foreign, "symlink"):
				target := filepath.Join(dir, "outside")
				os.WriteFile(target, []byte("do-not-touch"), 0o644)
				link := marker
				if strings.HasSuffix(foreign, "identity") {
					link = path
				}
				os.Symlink(target, link)
			case foreign == "corrupt-identity":
				os.WriteFile(path, []byte("broken"), 0o600)
			default:
				os.WriteFile(path, []byte(origin.identity), 0o600)
			}
			before, calls := composeSnapshot(data), origin.count()
			failed := composeExecuteBootstrap(t, files, data, setup)
			if failed.code == 0 || strings.Contains(failed.stdout, "ENTRYPOINT_ALLOWED") || origin.count() != calls {
				t.Fatalf("%+v calls %d→%d", failed, calls, origin.count())
			}
			if after := composeSnapshot(data); !composeMapsEqual(before, after) {
				t.Fatalf("files changed: %v → %v", before, after)
			}
		})
	}
}

func composeMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestBundleBootstrapNeverRedirectsScopedBearer(t *testing.T) {
	for _, status := range []int{302, 307} {
		dir := t.TempDir()
		target, source := newComposeOrigin(t), newComposeOrigin(t)
		source.setResponse(status, composeCapability(target.url).URL)
		files, data, setup := composeBootstrapFiles(t, dir, composeCapability(source.url))
		r := composeExecuteBootstrap(t, files, data, setup)
		if r.code == 0 || strings.Contains(r.stdout, "ENTRYPOINT_ALLOWED") || source.count() != 1 || target.count() != 0 ||
			strings.Contains(r.stdout+r.stderr, composeTestToken) {
			t.Fatalf("%d: %+v source %d target %d", status, r, source.count(), target.count())
		}
		if _, err := os.Stat(filepath.Join(data, "config-oxidns.json")); err == nil {
			t.Fatal("identity written")
		}
	}
}

// composePreparation is preparation(tmp_path, body, cap, in_data).
type composePrep struct {
	files, state, events string
	env                  []string
}

func composePreparation(t *testing.T, dir string, body *jsonx.Object, e *Enrollment, inData bool) composePrep {
	t.Helper()
	if e == nil {
		e = composeCapability("http://panel.example")
	}
	destination := filepath.Join(dir, "bundle")
	if inData {
		destination = body.GetString("data_dir")
	}
	files := composeUnpack(t, composeGuide(t, body, e), destination)
	bin := filepath.Join(dir, "bin")
	writeFile(t, filepath.Join(bin, "docker"), composeFakeDocker, 0o755)
	state := writeFile(t, filepath.Join(dir, "docker-state.json"), "{}", 0o644)
	events := filepath.Join(dir, "docker-events")
	env := []string{"PATH=" + bin + ":" + os.Getenv("PATH"), "CF_COMPOSE_STATE=" + state, "CF_COMPOSE_EVENTS=" + events,
		"DOCKER_CONTEXT=", "DOCKER_HOST="}
	return composePrep{files, state, events, env}
}

func (p composePrep) prepare(t *testing.T) runResult {
	t.Helper()
	return runShell(t, "bash", filepath.Join(p.files, "prepare.sh"), runOptions{env: p.env, timeout: 10 * time.Second})
}

func TestBundlePrepareInitializesMountsAndIdentityOnce(t *testing.T) {
	for _, inData := range []bool{false, true} {
		dir := t.TempDir()
		body := composeParams(dir, "create_host_identity", true)
		os.Remove(body.GetString("ha_host_id_path"))
		p := composePreparation(t, dir, body, nil, inData)
		if r := p.prepare(t); r.code != 0 {
			t.Fatalf("in_data=%v: %+v", inData, r)
		}
		data := body.GetString("data_dir")
		identity, _ := os.ReadFile(body.GetString("ha_host_id_path"))
		if len(strings.TrimSpace(string(identity))) != 32 {
			t.Fatalf("identity %q", identity)
		}
		for _, name := range []string{"agent", "oxidns", "ha", ".ha-compose"} {
			if info, err := os.Stat(filepath.Join(data, name)); err != nil || !info.IsDir() {
				t.Fatalf("%s missing", name)
			}
		}
		if info, _ := os.Stat(filepath.Join(data, ".ha-compose/enrollment.json")); info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode())
		}
		if r := p.prepare(t); r.code != 0 {
			t.Fatalf("again: %+v", r)
		}
		if again, _ := os.ReadFile(body.GetString("ha_host_id_path")); string(again) != string(identity) {
			t.Fatal("identity rewritten")
		}
		events, _ := os.ReadFile(p.events)
		for _, line := range strings.Split(strings.TrimSpace(string(events)), "\n") {
			var args []string
			json.Unmarshal([]byte(line), &args)
			if len(args) > 0 && strings.Contains(" run start stop rm pull ", " "+args[0]+" ") {
				t.Fatalf("mutating docker call %v", args)
			}
		}
	}
}

func TestBundlePrepareReusesOnlyOwnedProjectAndRenewsFiles(t *testing.T) {
	for _, mode := range []string{"host", "macvlan"} {
		dir := t.TempDir()
		body := composeParams(dir, "network_mode", mode, "ha_parent", "eth0", "ha_subnet", "192.168.10.0/24")
		p := composePreparation(t, dir, body, nil, false)
		if r := p.prepare(t); r.code != 0 {
			t.Fatalf("%s: %+v", mode, r)
		}
		text, _ := os.ReadFile(filepath.Join(p.files, "compose.yaml"))
		loaded, err := readCompose(strings.ReplaceAll(string(text), "$$", "$"))
		if err != nil {
			t.Fatal(err)
		}
		compose := loaded
		stored := jsonx.Obj("containers", jsonx.NewObject(), "networks", jsonx.NewObject())
		for name, value := range compose.GetObject("services").All() {
			service := value.(*jsonx.Object)
			service.GetObject("labels").Set("com.docker.compose.project", compose.GetString("name"))
			service.GetObject("labels").Set("com.docker.compose.service", name)
			stored.GetObject("containers").Set(service.GetString("container_name"), service)
		}
		for _, value := range compose.GetObject("networks").All() {
			network := value.(*jsonx.Object)
			network.GetObject("labels").Set("com.docker.compose.project", compose.GetString("name"))
			stored.GetObject("networks").Set(network.GetString("name"), network)
		}
		save := func() {
			data, _ := jsonx.Default(stored)
			os.WriteFile(p.state, data, 0o644)
		}
		save()
		if r := p.prepare(t); r.code != 0 {
			t.Fatalf("%s owned: %+v", mode, r)
		}
		updated := composeCapability("http://panel.example")
		updated.Token = strings.Repeat("u", 43)
		composeUnpack(t, composeGuide(t, body, updated), p.files)
		if r := p.prepare(t); r.code != 0 {
			t.Fatalf("%s renewed: %+v", mode, r)
		}
		if got := composeReadJSON(t, filepath.Join(body.GetString("data_dir"), ".ha-compose/enrollment.json"))["token"]; got != strings.Repeat("u", 43) {
			t.Fatalf("token %v", got)
		}
		first := stored.GetObject("containers").Values()[0].(*jsonx.Object)
		mount := first.GetList("volumes")[0].(*jsonx.Object)
		mount.Set("source", "/foreign/agent")
		save()
		if r := p.prepare(t); r.code == 0 {
			t.Fatalf("%s foreign mount accepted", mode)
		}
		mount.Set("source", body.GetString("data_dir")+"/agent")
		first.GetObject("labels").Set("com.docker.compose.project", "foreign-project")
		save()
		if r := p.prepare(t); r.code == 0 {
			t.Fatalf("%s foreign project accepted", mode)
		}
	}
}

func TestBundlePrepareRefusesExistingDataUnownedResourcesAndInvalidEnvironment(t *testing.T) {
	for _, fault := range []string{"old-data", "extra-package-file", "foreign-container", "foreign-network", "data-link", "identity-link", "old-compose", "remote-context"} {
		t.Run(fault, func(t *testing.T) {
			dir := t.TempDir()
			body := composeParams(dir, "network_mode", "macvlan", "ha_parent", "eth0", "ha_subnet", "192.168.10.0/24")
			p := composePreparation(t, dir, body, nil, fault == "extra-package-file")
			data := body.GetString("data_dir")
			switch fault {
			case "old-data", "extra-package-file":
				os.MkdirAll(data, 0o755)
				os.WriteFile(filepath.Join(data, "do-not-overwrite"), []byte("original"), 0o644)
			case "data-link":
				target := filepath.Join(dir, "target")
				os.Mkdir(target, 0o755)
				os.Symlink(target, data)
			case "identity-link":
				path := body.GetString("ha_host_id_path")
				target := filepath.Join(filepath.Dir(path), "real-id")
				os.Rename(path, target)
				os.Symlink(target, path)
			case "foreign-container":
				os.WriteFile(p.state, []byte(`{"containers": {"`+body.GetString("agent_name")+`-oxidns": {}}}`), 0o644)
			case "foreign-network":
				text, _ := os.ReadFile(filepath.Join(p.files, "compose.yaml"))
				loaded, _ := readCompose(string(text))
				name := jsonx.Path(loaded, "networks", "dns-ha", "name").(string)
				os.WriteFile(p.state, []byte(`{"networks": {"`+name+`": {}}}`), 0o644)
			case "old-compose":
				os.WriteFile(p.state, []byte(`{"version": "2.19.9"}`), 0o644)
			default:
				os.WriteFile(p.state, []byte(`{"endpoint": "ssh://remote.example"}`), 0o644)
			}
			if r := p.prepare(t); r.code == 0 {
				t.Fatalf("accepted: %+v", r)
			}
			if _, err := os.Stat(filepath.Join(data, ".ha-compose/setup")); err == nil {
				t.Fatal("setup written")
			}
		})
	}
}

func TestBundleMissingIdentityDoesNotReserveData(t *testing.T) {
	dir := t.TempDir()
	body := composeParams(dir)
	os.Remove(body.GetString("ha_host_id_path"))
	p := composePreparation(t, dir, body, nil, true)
	if r := p.prepare(t); r.code == 0 {
		t.Fatal("accepted without identity")
	}
	data := body.GetString("data_dir")
	for _, name := range []string{".ha-compose", "agent"} {
		if _, err := os.Stat(filepath.Join(data, name)); err == nil {
			t.Fatalf("%s reserved", name)
		}
	}
	corrected := writeFile(t, filepath.Join(dir, "correct-host-id"), "correct-host-identity-1234567890\n", 0o644)
	body.Set("ha_host_id_path", corrected)
	composeUnpack(t, composeGuide(t, body, composeCapability("http://panel.example")), p.files)
	if r := p.prepare(t); r.code != 0 {
		t.Fatalf("%+v", r)
	}
}

func TestBundleRootLevelIdentityInsideDataDirectoryIsCreatedAndReused(t *testing.T) {
	dir := t.TempDir()
	body := composeParams(dir, "create_host_identity", true)
	body.Set("ha_host_id_path", body.GetString("data_dir")+"/host-id")
	p := composePreparation(t, dir, body, nil, true)
	if r := p.prepare(t); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	original, _ := os.ReadFile(body.GetString("ha_host_id_path"))
	if r := p.prepare(t); r.code != 0 {
		t.Fatalf("again %+v", r)
	}
	if again, _ := os.ReadFile(body.GetString("ha_host_id_path")); string(again) != string(original) || len(original) == 0 {
		t.Fatal("identity changed")
	}
}
