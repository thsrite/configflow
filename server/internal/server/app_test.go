package server

import (
	"context"
	"slices"
	"testing"

	"configflow/internal/lifecycle"
)

func TestNewRegistersStartupAndPeriodicNames(t *testing.T) {
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})

	app, err := New(context.Background(), Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app == nil || app.HTTP == nil {
		t.Fatal("New returned nil App or HTTP")
	}

	got := lifecycle.Names()
	// Every startup step and periodic loop must be registered.
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
