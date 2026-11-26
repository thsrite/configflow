package server

import (
	"context"
	"errors"
	"slices"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
)

func TestNewRegistersStartupAndPeriodicNames(t *testing.T) {
	rt := lifecycle.NewProcessRuntime(lifecycle.Options{})

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
	if app.Traffic == nil || app.Metrics == nil || app.Monitor == nil || app.Upgrades == nil || app.DNSHA == nil || app.DNSFollow == nil || app.RuleSync == nil || app.IPv6 == nil {
		t.Fatal("New did not construct all Agent-domain services")
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
	if err := app.Monitor.SetHARules(nil); !errors.Is(err, monitor.ErrServiceStarted) {
		t.Fatalf("Monitor.SetHARules after New = %v, want ErrServiceStarted", err)
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
		"ipv6-verification-scheduler",
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
