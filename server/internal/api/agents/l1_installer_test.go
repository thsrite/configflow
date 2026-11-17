package agents_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
)

func TestL1CompactColonDoesNotIssueCapability(t *testing.T) {
	srv, manager, admin := authenticatedApp(t)
	body := dockerHABody()
	body.Set("install_method", "compose_compact")
	body.Set("data_dir", "/srv/cf:compact")
	before := allAgents(t, srv, manager)
	srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(body), admin).
		WantStatus(400).WantJSON(jsonx.Obj("success", false, "message", "持久数据根目录须为规范的绝对路径，不能包含逗号、冒号或换行"))
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); !os.IsNotExist(err) {
		t.Fatalf("refusal wrote capability files: %v", err)
	}
	if !reflect.DeepEqual(before, allAgents(t, srv, manager)) {
		t.Fatal("refusal changed the agent registry")
	}
}
