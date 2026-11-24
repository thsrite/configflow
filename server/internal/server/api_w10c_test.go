package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	apisettings "configflow/internal/api/settings"
	"configflow/internal/httpx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/weblistener"
)

// TestW10cProductionHandlersUseInjectedDependencies verifies that production
// server.New wires every W10c handler with explicit dependencies and exposes
// the two-phase settings.Handler for weblistener injection (O17 D5).
func TestW10cProductionHandlersUseInjectedDependencies(t *testing.T) {
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

	if app.Settings == nil {
		t.Fatal("app.Settings is nil")
	}
	if app.Settings.Repo != r || app.Settings.SubStore != app.SubStore {
		t.Fatalf("app.Settings dependencies not wired: repo=%v substore=%v", app.Settings.Repo == r, app.Settings.SubStore == app.SubStore)
	}

	// Two-phase D5 web listener wiring on app.Settings.
	m := &weblistener.Manager{DataDir: r.DataDir()}
	if err := app.Settings.SetWebListener(m); err != nil {
		t.Fatalf("SetWebListener: %v", err)
	}
	if err := app.Settings.SetWebListener(m); !errors.Is(err, apisettings.ErrWebListenerAlreadySet) {
		t.Fatalf("second SetWebListener = %v, want %v", err, apisettings.ErrWebListenerAlreadySet)
	}
	app.Settings.MarkStarted()

	// Handlers serve requests without a process-global repository when the
	// request context carries no repository.
	repo.SetRepository(nil)
	t.Cleanup(func() { repo.SetRepository(nil) })

	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/api/auth/status", 200},
		{http.MethodGet, "/api/version", 200},
		{http.MethodGet, "/api/stats/overview", 200},
		{http.MethodGet, "/api/nodes", 200},
		{http.MethodGet, "/api/subscriptions", 200},
		{http.MethodGet, "/api/aggregations", 200},
		{http.MethodGet, "/api/proxy-groups", 200},
		{http.MethodGet, "/api/setup", 200},
		{http.MethodGet, "/api/tasks", 200},
		{http.MethodGet, "/api/notifications", 200},
		{http.MethodGet, "/api/client-devices", 200},
		{http.MethodGet, "/api/custom-config/linux-gateway", 200},
		{http.MethodGet, "/api/profiles/default/gateway-onboarding", 200},
		{http.MethodGet, "/api/domain-discovery/settings", 200},
		// Uninitialized non-nil weblistener.Manager returns 503 (proving the
		// injected instance is consulted rather than the nil default's 200).
		{http.MethodGet, "/api/settings/web-port", 503},
	} {
		res, err := app.HTTP.Invoke(context.Background(), httpx.InvokeRequest{Method: tc.method, Path: tc.path})
		if err != nil {
			t.Errorf("%s %s: %v", tc.method, tc.path, err)
			continue
		}
		if res.Status != tc.want {
			t.Errorf("%s %s: status = %d, want %d (body=%s)", tc.method, tc.path, res.Status, tc.want, res.Body)
		}
	}
}
