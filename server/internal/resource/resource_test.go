package resource

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathsAreJoinedLikeOsPathJoin(t *testing.T) {
	l := New("/app")
	for got, want := range map[string]string{
		l.Path("backend/VERSION"):         "/app/backend/VERSION",
		l.Path("/etc/x"):                  "/etc/x",
		l.Backend("config_template.json"): "/app/backend/config_template.json",
		New("/app/").Path("a"):            "/app/a",
		New("").Path("a"):                 "a",
	} {
		if got != want {
			t.Errorf("got %q want %q", got, want)
		}
	}
}

func TestExistsIsTrueForRegularFilesOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "backend", "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backend", "VERSION"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	l := New(root)
	if !l.Exists("backend/VERSION") || l.Exists("backend/dir") || l.Exists("backend/missing") {
		t.Error("Exists is wrong")
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("CONFIGFLOW_ROOT", "/somewhere")
	if got := FromEnv().Root; got != "/somewhere" {
		t.Errorf("got %q", got)
	}
}
