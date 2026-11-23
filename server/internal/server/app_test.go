package server

import (
	"context"
	"slices"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
)

func TestNewRegistersStartupAndPeriodicNames(t *testing.T) {
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})
	t.Cleanup(func() {
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})

	app, err := New(context.Background(), Options{Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app == nil || app.HTTP == nil || app.Runtime != rt || app.Tasks == nil || app.Notifications == nil {
		t.Fatal("New returned unexpected App")
	}
	if err := app.Tasks.Validate(); err != nil {
		t.Fatalf("Tasks.Validate: %v", err)
	}
	if err := app.Notifications.Validate(); err != nil {
		t.Fatalf("Notifications.Validate: %v", err)
	}
	if err := app.Notifications.SetAgents(func(context.Context) ([]model.Agent, error) { return nil, nil }); err != notifications.ErrServiceStarted {
		t.Fatalf("SetAgents after New = %v, want ErrServiceStarted", err)
	}

	got := rt.Names()
	// Every startup step and periodic loop must be registered on the injected Runtime.
	wantRequired := []string{
		"agent-manager-init",
		"agent-rule-sync",
		"dns-follow",
		"domain-discovery-auto",
		"load-config",
		"metrics-history-flush",
		"metrics-load-history",
		"oxidns-ha-publish",
		"oxidns-ranking-history",
		"router-rule-sync",
		"scheduled-refresh",
		"subscription-cache-prune",
		"webdav-worker-init",
	}
	for _, name := range wantRequired {
		if !slices.Contains(got, name) {
			t.Errorf("missing lifecycle task %q in %v", name, got)
		}
	}
}
