package agents_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	apiagents "configflow/internal/api/agents"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

// test_agent_install_commands.py::test_storage_failures_have_actionable_errors
// (module A10). Python monkeypatches the store to raise
// sqlite3.OperationalError; here the database file is not a SQLite
// database (sqlite3.DatabaseError, caught by the same except clauses).
func TestStorageFailuresHaveActionableErrors(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir) // the install command store follows repo.DataDir()
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true, DataDir: dir, Register: httpxtest.Routes(apiagents.Register)})
	admin := srv.Login()
	os.MkdirAll(filepath.Join(dir, "install-commands"), 0o700)
	if err := os.WriteFile(filepath.Join(dir, "install-commands", "snapshots.sqlite3"), []byte(strings.Repeat("not a database ", 64)), 0o600); err != nil {
		t.Fatal(err)
	}
	res := srv.Post("/api/agents/install-command", httpxtest.JSON(jsonx.NewObject()), admin).WantStatus(500)
	if !strings.Contains(res.Object().GetString("message"), "检查数据目录") {
		t.Errorf("save: %s", res.Text())
	}
	res = srv.Get("/api/agents/i/" + strings.Repeat("A", 22)).WantStatus(503)
	if !strings.Contains(res.Text(), "请稍后重试") || res.Header.Get("Content-Type") != "text/plain; charset=utf-8" || res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("Content-Disposition") != "" {
		t.Errorf("load: %s %v", res.Text(), res.Header)
	}
}
