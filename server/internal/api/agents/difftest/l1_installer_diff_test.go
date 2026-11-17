package difftest

import (
	"os"
	"path/filepath"
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
	compose, err := yamlx.Load(pyBody.(*jsonx.Object).GetString("compose"))
	if err != nil {
		t.Fatal(err)
	}
	volumes := jsonx.Path(compose, "services", "agent", "volumes").([]any)
	mount := volumes[0].(*jsonx.Object)
	if mount.GetString("source") != "/srv/cf:compact/agent" || jsonx.Path(mount, "bind", "create_host_path") != true {
		t.Fatalf("Python no longer emits the known invalid mount: %v", mount)
	}
	if files := installationFiles(t, goDir); len(files) != 0 {
		t.Fatalf("Go refusal wrote files: %v", files)
	}
	if files := installationFiles(t, pyDir); len(files) != 4 || files[".issue.lock"] == "" {
		t.Fatalf("Python must create exactly its directory, issue lock, session and index: %v", files)
	}
	if !jsonx.Equal(finalAgents(t, pyDir), finalAgents(t, goDir)) {
		t.Fatal("guide generation must not register an agent")
	}
	t.Log("EXPECTED L1 D1: Python 200 + unsafe mount/capability, Go 400 + no writes")
}
