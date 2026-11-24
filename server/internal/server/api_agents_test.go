package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"configflow/internal/convert"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

func TestServerWiresAgentAPIHandler(t *testing.T) {
	t.Cleanup(convert.ResetMihomoEnvironmentForTest(t))
	t.Cleanup(convert.ResetMihomoIntegrationsForTest(t))
	t.Cleanup(func() {
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})

	r, err := repo.OpenDefaultWithOptions(t.TempDir(), RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})

	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(app.Agents.Close)

	for _, path := range []string{"/api/agents?summary=1", "/api/agents/latest-version", "/api/agents/gateway-installations?profile_id=default"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.HTTP.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, rec.Code, rec.Body.String())
		}
	}
}
