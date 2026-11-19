package agents_test

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"configflow/internal/httpx/httpxtest"
)

func TestN810InstallerUnsafeAliasesNeverIssueCapabilities(t *testing.T) {
	srv, manager, admin := authenticatedApp(t)
	for _, method := range []string{"compose_compact", "compose_direct"} {
		for _, directory := range []string{"//", "//etc", "//opt", "/etc/", "/./etc", "/opt/../etc"} {
			body := dockerHABody()
			body.Set("agent_ip", "192.0.2.7")
			body.Set("install_method", method)
			body.Set("data_dir", directory)
			srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(body), admin).WantStatus(400)
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unsafe path issued or wrote a capability")
	}
	if len(allAgents(t, srv, manager)) != 0 {
		t.Fatal("unsafe path created an Agent")
	}
}

func TestN810InstallerNonASCIIQueryNumberNeverDefaultsOrIssues(t *testing.T) {
	srv, _, admin := authenticatedApp(t)
	for _, route := range []string{
		"/api/agents/install-script?port=" + url.QueryEscape("１２３"),
		"/api/agents/install-script?deployment_health_timeout=" + url.QueryEscape("٣٠"),
		"/api/agents/docker-agent-compose?mihomo_port=" + url.QueryEscape("８０８０"),
	} {
		response := srv.Get(route, admin).WantStatus(400)
		if response.Object().GetString("message") != "Invalid installer integer" {
			t.Fatal("invalid number defaulted or returned wrong refusal")
		}
	}
	system, err := srv.Repo.System(srv.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(system.RegistrationSessions) != 0 {
		t.Fatal("invalid query issued a registration proof")
	}
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "install-commands")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid query wrote an install snapshot")
	}
}

func TestN810InstallerNumericIdentityNeverIssues(t *testing.T) {
	srv, manager, admin := authenticatedApp(t)
	for _, method := range []string{"script", "compose", "compose_direct", "compose_compact"} {
		for _, address := range []any{3232236039, true, false} {
			body := dockerHABody()
			body.Set("agent_ip", address)
			body.Set("install_method", method)
			srv.Post("/api/agents/docker-oxidns-ha-guide", httpxtest.JSON(body), admin).WantStatus(400)
		}
	}
	if _, err := os.Stat(filepath.Join(srv.Repo.DataDir(), "docker-ha-installations")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("numeric identity wrote capability")
	}
	if len(allAgents(t, srv, manager)) != 0 {
		t.Fatal("numeric identity created Agent")
	}
}
