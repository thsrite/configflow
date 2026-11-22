//go:build pyref

package dockerha

// Differential check against the image's former Python installation scripts
// (docker/docker-compose-install.py and docker-compose-compact.py, Agent
// 1.9.2-go). Run once on a test server with the old scripts extracted:
//
//	git show go:docker/docker-compose-install.py > /tmp/old/docker-compose-install.py
//	git show go:docker/docker-compose-compact.py > /tmp/old/docker-compose-compact.py
//	PYREF_SCRIPTS=/tmp/old go test -tags pyref -run Pyref ./dockerha
//
// Both implementations run on copies of the same fixture; refusal messages
// and the resulting files (installation ids normalised) must be equal, and
// an installation made by one must start with the other.

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const pyDriver = `
import importlib.util, os, sys
from pathlib import Path
kind, script, *dirs = sys.argv[1:]
spec = importlib.util.spec_from_file_location('m', script)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
try:
    if kind == 'compact':
        m.run(m.settings_from_environment(), *map(Path, dirs))
    else:
        settings = m.settings_from_environment()
        root, identity_dir, identity_file, action = dirs
        if action == 'prepare':
            m.prepare(settings, Path(root), Path(identity_dir), Path(identity_file))
        else:
            m.run(settings, Path(root) / 'agent', Path(identity_dir), Path(identity_file))
except OSError:
    m.fail('OSERROR')
`

func pyScripts(t *testing.T) string {
	dir := os.Getenv("PYREF_SCRIPTS")
	if dir == "" {
		t.Skip("PYREF_SCRIPTS not set")
	}
	return dir
}

// runPython runs the old script; it returns stdout and the refusal (stderr).
func runPython(t *testing.T, env []string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command("python3", append([]string{"-c", pyDriver}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.Run()
	return stdout.String(), strings.TrimSpace(stderr.String())
}

var hexID = regexp.MustCompile(`[0-9a-f]{32}`)

// snapshot lists files below dir: JSON normalised, ids masked, temporary
// files and the enrollment lock left out.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || path == dir {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		base := filepath.Base(path)
		if strings.HasPrefix(base, ".compose-write-") || strings.HasPrefix(base, ".compose-enroll-") {
			return nil
		}
		if info.IsDir() {
			files[rel+"/"] = info.Mode().Perm().String()
			return nil
		}
		raw, _ := os.ReadFile(path)
		var value any
		text := string(raw)
		if json.Unmarshal(raw, &value) == nil {
			normal, _ := json.Marshal(value)
			text = string(normal)
		}
		files[rel] = info.Mode().Perm().String() + " " + hexID.ReplaceAllString(text, "<id>")
		return nil
	})
	return files
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	if out, err := exec.Command("cp", "-a", from, to).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
}

func envList(values map[string]string) []string {
	var list []string
	for key, value := range values {
		list = append(list, key+"="+value)
	}
	sort.Strings(list)
	return list
}

func TestPyrefCompact(t *testing.T) {
	scripts := pyScripts(t)
	type scenario struct {
		name string
		env  map[string]string
		prep func(t *testing.T, p CompactPaths)
	}
	foreign := func(t *testing.T, p CompactPaths) { writeFile(t, filepath.Join(p.CoreDir, "config.yaml"), "x", 0o600) }
	scenarios := []scenario{
		{"first install", nil, nil},
		{"foreign data", nil, foreign},
		{"first install without key", map[string]string{"INSTALL_KEY": ""}, nil},
		{"bad role", map[string]string{"SERVICE_TYPE": "oxidns"}, nil},
		{"loopback ip", map[string]string{"AGENT_IP": "127.0.0.1"}, nil},
		{"bad name", map[string]string{"AGENT_NAME": "a b"}, nil},
		{"bad key", map[string]string{"INSTALL_KEY": "abc"}, nil},
		{"lock is a directory", nil, func(t *testing.T, p CompactPaths) { os.Mkdir(filepath.Join(p.AgentDir, compactLock), 0o700) }},
		{"damaged marker", nil, func(t *testing.T, p CompactPaths) { writeFile(t, filepath.Join(p.AgentDir, compactMarker), "{", 0o600) }},
		{"symlinked core directory", nil, func(t *testing.T, p CompactPaths) { os.Remove(p.CoreDir); os.Symlink(p.HADir, p.CoreDir) }},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			o := newOrigin(t)
			env := map[string]string{"SERVICE_TYPE": "oxidns-ha-host", "SERVER_URL": o.server.URL, "AGENT_IP": "192.0.2.21",
				"AGENT_PORT": "8082", "AGENT_NAME": "dns-a", "INSTALL_KEY": testSession + "." + testToken}
			for key, value := range sc.env {
				env[key] = value
			}
			goPaths := compactFixture(t)
			if sc.prep != nil {
				sc.prep(t, goPaths)
			}
			pyRoot := filepath.Join(t.TempDir(), "py")
			copyTree(t, filepath.Dir(goPaths.AgentDir), pyRoot)
			pyPaths := CompactPaths{AgentDir: filepath.Join(pyRoot, "agent"), CoreDir: filepath.Join(pyRoot, "oxidns"), HADir: filepath.Join(pyRoot, "ha")}

			_, pyErr := runPython(t, envList(env), "compact", filepath.Join(scripts, "docker-compose-compact.py"), pyPaths.AgentDir, pyPaths.CoreDir, pyPaths.HADir)
			var goErr error
			settings, err := compactSettings(func(key string) (string, bool) { v, ok := env[key]; return v, ok })
			if err == nil {
				err = RunCompact(context.Background(), settings, goPaths, BundleClient(), &bytes.Buffer{})
			}
			goErr = err
			if message(goErr) != pyErr {
				t.Fatalf("Go %q, Python %q", message(goErr), pyErr)
			}
			if g, p := snapshot(t, filepath.Dir(goPaths.AgentDir)), snapshot(t, pyRoot); !reflect.DeepEqual(g, p) {
				t.Fatalf("files differ\nGo:     %v\nPython: %v", g, p)
			}
		})
	}
}

// TestPyrefCompactCrossStart: an installation made by either side starts
// with the other (offline, without INSTALL_KEY) and the companion accepts it.
func TestPyrefCompactCrossStart(t *testing.T) {
	scripts := pyScripts(t)
	o := newOrigin(t)
	env := map[string]string{"SERVICE_TYPE": "oxidns-ha-host", "SERVER_URL": o.server.URL, "AGENT_IP": "192.0.2.21",
		"AGENT_PORT": "8082", "AGENT_NAME": "dns-a", "INSTALL_KEY": testSession + "." + testToken}
	restart := map[string]string{}
	for key, value := range env {
		restart[key] = value
	}
	restart["INSTALL_KEY"] = ""
	lookup := func(values map[string]string) func(string) (string, bool) {
		return func(key string) (string, bool) { v, ok := values[key]; return v, ok }
	}

	// Python installs, Go restarts.
	p := compactFixture(t)
	if _, msg := runPython(t, envList(env), "compact", filepath.Join(scripts, "docker-compose-compact.py"), p.AgentDir, p.CoreDir, p.HADir); msg != "" {
		t.Fatalf("python install: %s", msg)
	}
	settings, _ := compactSettings(lookup(restart))
	if err := RunCompact(context.Background(), settings, p, BundleClient(), &bytes.Buffer{}); err != nil {
		t.Fatalf("Go restart of a Python installation: %v", err)
	}
	marker, _ := readInstallJSON(filepath.Join(p.AgentDir, compactMarker))
	if _, ok := bindingOwner(marker["binding"].(map[string]any)); !ok {
		t.Fatal("Python binding not canonical")
	}

	// Go installs, Python restarts.
	g := compactFixture(t)
	settings, _ = compactSettings(lookup(env))
	if err := RunCompact(context.Background(), settings, g, BundleClient(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, msg := runPython(t, envList(restart), "compact", filepath.Join(scripts, "docker-compose-compact.py"), g.AgentDir, g.CoreDir, g.HADir); msg != "" {
		t.Fatalf("Python restart of a Go installation: %s", msg)
	}
	if calls := o.recorded(); len(calls) != 2 {
		t.Fatalf("enrollments %v", calls)
	}
}

func TestPyrefDirect(t *testing.T) {
	scripts := pyScripts(t)
	script := filepath.Join(scripts, "docker-compose-install.py")
	type scenario struct {
		name    string
		mode    string
		prep    func(t *testing.T, f directFixture)
		actions []string
	}
	scenarios := []scenario{
		{"file prepare twice and run", "file", nil, []string{"prepare", "prepare", "run", "run"}},
		{"volume prepare and run", "docker-volume", nil, []string{"prepare", "run"}},
		{"run before prepare", "file", nil, []string{"run"}},
		{"unknown file", "file", func(t *testing.T, f directFixture) {
			writeFile(t, filepath.Join(f.paths.DataRoot, "x"), "", 0o600)
		}, []string{"prepare"}},
		{"old data", "file", func(t *testing.T, f directFixture) {
			os.Mkdir(filepath.Join(f.paths.DataRoot, "ha"), 0o700)
			writeFile(t, filepath.Join(f.paths.DataRoot, "ha", "k"), "", 0o600)
		}, []string{"prepare"}},
		{"short identity", "file", func(t *testing.T, f directFixture) {
			os.Chmod(f.paths.IdentityFile, 0o644)
			writeFile(t, f.paths.IdentityFile, "short", 0o644)
		}, []string{"prepare"}},
	}
	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			o := newOrigin(t)
			f := newDirect(t, sc.mode, o.url())
			if sc.prep != nil {
				sc.prep(t, f)
			}
			root := filepath.Dir(f.paths.DataRoot)
			pyRoot := filepath.Join(t.TempDir(), "py")
			copyTree(t, root, pyRoot)
			pyPaths := DirectPaths{DataRoot: filepath.Join(pyRoot, "data"), IdentityDir: filepath.Join(pyRoot, "identity"), IdentityFile: filepath.Join(pyRoot, "machine-id")}
			pyPaths.AgentDir = filepath.Join(pyPaths.DataRoot, "agent")
			env := []string{"CONFIGFLOW_COMPOSE_INSTALL=" + directSettingsText(sc.mode, o.url())}
			for _, action := range sc.actions {
				pyOut, pyErr := runPython(t, env, "direct", script, pyPaths.DataRoot, pyPaths.IdentityDir, pyPaths.IdentityFile, action)
				var out bytes.Buffer
				var err error
				if action == "prepare" {
					err = PrepareDirect(f.settings, f.paths, &out)
				} else {
					err = RunDirect(context.Background(), f.settings, f.paths, BundleClient(), &out)
				}
				if message(err) != pyErr || out.String() != pyOut {
					t.Fatalf("%s: Go %q %q, Python %q %q", action, out.String(), message(err), pyOut, pyErr)
				}
			}
			g, p := snapshot(t, root), snapshot(t, pyRoot)
			// The generated volume identity is random on both sides.
			for _, files := range []map[string]string{g, p} {
				for key := range files {
					if strings.HasPrefix(key, "identity/host-id") || strings.Contains(files[key], "host_id") {
						files[key] = regexp.MustCompile(`"host_id":"[0-9a-f]{64}"`).ReplaceAllString(files[key], `"host_id":"<h>"`)
					}
				}
				if v, ok := files["identity/host-id"]; ok {
					files["identity/host-id"] = v[:4]
				}
			}
			if !reflect.DeepEqual(g, p) {
				t.Fatalf("files differ\nGo:     %v\nPython: %v", g, p)
			}
		})
	}
}
