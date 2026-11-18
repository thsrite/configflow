package logs

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

func TestLogStatPermissionFailureDoesNotBecomeMissingOrFallback(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires the normal unprivileged test user")
	}
	c := newLogClient(t)
	folder := filepath.Join(t.TempDir(), "denied-log")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(folder, "synthetic.log")
	fallback := filepath.Join(c.repo.DataDir(), "app.log")
	before := []byte("fallback must not hide a denied assigned log\n")
	if err := os.WriteFile(fallback, before, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", c.repo.DataDir())
	t.Setenv("LOG_FILE", path)
	LogFilePath = getLogFilePath
	if err := os.Chmod(folder, 0); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(folder, 0700)
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrPermission) {
		t.Fatal("fixture did not deny directory traversal", err)
	}
	ctx := repo.WithRepository(context.Background(), c.repo)
	if _, err := c.repo.GetSystem(ctx); err != nil {
		t.Fatal("database must remain readable", err)
	}
	for _, endpoint := range []string{"/api/logs/info", "/api/logs/tail", "/api/logs/clear"} {
		method := "GET"
		if endpoint == "/api/logs/clear" {
			method = "POST"
		}
		res, err := c.app.Invoke(ctx, httpx.InvokeRequest{Method: method, Path: endpoint})
		if err != nil {
			t.Fatal(err)
		}
		value, err := jsonx.Parse(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		body := value.(*jsonx.Object)
		if res.Status != 500 || body.GetBool("success") || !strings.Contains(body.GetString("error"), "permission denied") || !strings.Contains(body.GetString("error"), path) {
			t.Fatal("permission failure hidden", endpoint, res.Status, string(res.Body))
		}
	}
	after, err := os.ReadFile(fallback)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("clear touched the fallback after denied path", err)
	}
	if err := os.Chmod(folder, 0700); err != nil {
		t.Fatal(err)
	}
	selected, err := getLogFilePath()
	if err != nil || selected != fallback {
		t.Fatal("real missing file no longer falls back", selected, err)
	}
}
