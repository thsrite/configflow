package difftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// L1 D1: Python emits an unusable Compose and creates a capability; Go rejects
// before any write. Assert both effects explicitly instead of masking files.
func TestL1CompactColonDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	seed(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	defer stopPython()
	goBase, stopGo := startGo(t, goDir)
	defer stopGo()
	body := haNewBody("install_method", "compose_compact", "agent_name", "l1-colon", "data_dir", "/srv/cf:compact")
	r := guideReq("L1 compact colon", body)
	p := send(t, pyBase, r, login(t, pyBase), nil)
	g := send(t, goBase, r, login(t, goBase), nil)
	if p.status != 200 || g.status != 400 {
		t.Fatalf("Python %d %s; Go %d %s", p.status, p.body, g.status, g.body)
	}
	goBody, err := jsonx.Parse(g.body)
	if err != nil || !jsonx.Equal(goBody, jsonx.Obj("success", false, "message", "持久数据根目录须为规范的绝对路径，不能包含逗号、冒号或换行")) {
		t.Fatalf("Go refusal: %s (%v)", g.body, err)
	}
	pyBody, err := jsonx.Parse(p.body)
	if err != nil {
		t.Fatal(err)
	}
	var compose struct {
		Services map[string]struct {
			Volumes []struct {
				Source string `yaml:"source"`
				Bind   struct {
					CreateHostPath bool `yaml:"create_host_path"`
				} `yaml:"bind"`
			} `yaml:"volumes"`
		} `yaml:"services"`
	}
	if err := yamlx.Unmarshal([]byte(pyBody.(*jsonx.Object).GetString("compose")), &compose); err != nil {
		t.Fatal(err)
	}
	agent, ok := compose.Services["agent"]
	if !ok || len(agent.Volumes) == 0 {
		t.Fatal("Python Compose lost agent volume")
	}
	mount := agent.Volumes[0]
	if mount.Source != "/srv/cf:compact/agent" || !mount.Bind.CreateHostPath {
		t.Fatalf("Python no longer emits the known invalid mount: %#v", mount)
	}
	if files := installationFiles(t, goDir); len(files) != 0 {
		t.Fatalf("Go refusal wrote files: %v", files)
	}
	// O12 excludes the retired resource lock from snapshots, but its differing
	// filesystem effects remain explicit: Python creates it and Go must not.
	if info, err := os.Lstat(filepath.Join(pyDir, "docker-ha-installations", ".issue.lock")); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("Python must create its issue lock as a regular file: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(goDir, "docker-ha-installations", ".issue.lock")); !os.IsNotExist(err) {
		t.Fatalf("Go created retired issue lock: %v", err)
	}
	files := installationFiles(t, pyDir)
	if len(files) != 3 || files["."] != "-rwx------" {
		t.Fatalf("Python must create exactly its private directory, session and index: %v", files)
	}
	sessions, indexes := 0, 0
	for name, value := range files {
		if name == "." {
			continue
		}
		if !strings.HasPrefix(value, "-rw------- ") {
			t.Fatalf("Python installation file %s must have mode 0600: %q", name, value)
		}
		switch {
		case strings.HasSuffix(name, ".json"):
			sessions++
		case strings.HasSuffix(name, ".index"):
			indexes++
		default:
			t.Fatalf("unexpected Python installation file: %s", name)
		}
	}
	if sessions != 1 || indexes != 1 {
		t.Fatalf("Python must create one session and one index: %v", files)
	}
	if !jsonx.Equal(finalAgents(t, pyDir), finalAgents(t, goDir)) {
		t.Fatal("guide generation must not register an agent")
	}
	t.Log("EXPECTED L1 D1: Python 200 + unsafe mount/capability, Go 400 + no writes")
}
