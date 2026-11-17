package difftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestL1DownloadErrorFormatsAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	root := t.TempDir()
	static := filepath.Join(root, "empty-binaries")
	if err := os.Mkdir(static, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_STATIC_DIR", static)
	pyDir, goDir := filepath.Join(root, "python"), filepath.Join(root, "go")
	seed(t, pyDir)
	copyDir(t, pyDir, goDir)
	p, stopPython := startPython(t, pyDir)
	defer stopPython()
	g, stopGo := startGo(t, goDir)
	defer stopGo()
	for _, test := range []struct{ path, kind string }{
		{"/api/agents/i/UNKNOWNunknownUNKNOWN0", "text/plain; charset=utf-8"},
		{"/api/agents/i/invalid!", "text/plain; charset=utf-8"},
		{"/api/agents/download/configflow-agent-linux-amd64", "application/json"},
		{"/api/agents/download/invalid-filename", "application/json"},
	} {
		r := downloadRequest{name: test.path, method: "GET", path: test.path}
		a, b := sendDownload(t, p, r), sendDownload(t, g, r)
		if a.status != 404 || b.status != 404 || a.header.Get("Content-Type") != test.kind || b.header.Get("Content-Type") != test.kind {
			t.Fatalf("wrong error format %s: Python %d %v Go %d %v", test.path, a.status, a.header, b.status, b.header)
		}
		var differences []string
		if strings.HasPrefix(test.kind, "text/plain") {
			if a.header.Get("Content-Disposition") != `attachment; filename="configflow-install.sh"` || b.header.Get("Content-Disposition") != "" {
				t.Fatal("unexpected attachment-header change")
			}
			differences = compareInstaller(a, b, nil)
		} else {
			differences = compareDownload(a, b)
		}
		if len(differences) != 0 {
			t.Fatalf("%s: %v", test.path, differences)
		}
		t.Logf("Flask body/status/type match: %s (%s)", test.path, test.kind)
	}
}
