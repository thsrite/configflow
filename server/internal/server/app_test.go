package server

import (
	"context"
	"errors"
	"slices"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

func TestNewRegistersStartupAndPeriodicNames(t *testing.T) {
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})
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

	app, err := New(context.Background(), Options{Repo: r, Runtime: rt})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app == nil || app.HTTP == nil || app.Runtime != rt || app.Tasks == nil || app.Notifications == nil || app.Agents == nil {
		t.Fatal("New returned unexpected App")
	}
	t.Cleanup(app.Agents.Close)

	if err := app.Tasks.Validate(); err != nil {
		t.Fatalf("Tasks.Validate: %v", err)
	}
	if err := app.Notifications.Validate(); err != nil {
		t.Fatalf("Notifications.Validate: %v", err)
	}
	if err := app.Notifications.SetAgents(func(context.Context) ([]model.Agent, error) { return nil, nil }); !errors.Is(err, notifications.ErrServiceStarted) {
		t.Fatalf("SetAgents after New = %v, want ErrServiceStarted", err)
	}
	if err := app.Agents.SetPairPublisher(nil); !errors.Is(err, agents.ErrManagerStarted) {
		t.Fatalf("SetPairPublisher after New = %v, want ErrManagerStarted", err)
	}
	stores := app.Agents.DeletedStores()
	var storeNames []string
	for _, s := range stores {
		storeNames = append(storeNames, s.Name)
	}
	if !slices.Equal(storeNames, agents.AgentDeletedStores) {
		t.Fatalf("DeletedStores = %v, want %v", storeNames, agents.AgentDeletedStores)
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
