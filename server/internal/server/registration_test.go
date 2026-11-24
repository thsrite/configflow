package server

import (
	"context"
	"slices"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/repo"
)

// goStartups, goPeriodics and goShutdowns are the registrations of the
// production assembly on branch go before O17 W0-W8 (cmd registerStartup +
// server.NewApp, then agent-manager-init, which registered the IPv6 scheduler
// there). Startup order is part of the contract; loops and shutdown hooks are
// compared as sets.
var (
	goStartups = []string{
		"load-config",
		"subscription-cache-prune",
		"agent-manager-init",
		"metrics-load-history",
		"webdav-worker-init",
	}
	goPeriodics = []string{
		"agent-rule-sync",
		"dns-follow",
		"oxidns-ha-publish",
		"metrics-history-flush",
		"oxidns-ranking-history",
		"scheduled-refresh",
		"domain-discovery-auto",
		"router-rule-sync",
		"ipv6-verification-scheduler",
	}
	goShutdowns = []string{
		"agent-content-sync-close",
		"metrics-history-flush-first",
		"metrics-history-flush-last",
		"domain-discovery-cache",
	}
)

// TestNewRegistersEachLifecycleTaskOnce checks that the production assembly
// (server.New with a repository) registers every startup step, periodic loop
// and shutdown hook exactly once (no in-place replacement by a second wiring
// path, e.g. W6 wireAgents and W7 subsystem wiring both registering
// rulesync), with the same names and count as before O17 W0-W8, and that
// running the startup steps does not register anything again.
func TestNewRegistersEachLifecycleTaskOnce(t *testing.T) {
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
	t.Cleanup(app.Agents.Close)

	check := func(stage string, shutdowns []string) {
		t.Helper()
		got := rt.Registration()
		if len(got.Replaced) != 0 {
			t.Errorf("%s: registered more than once: %v", stage, got.Replaced)
		}
		if !slices.Equal(got.Startups, goStartups) {
			t.Errorf("%s: startups = %v, want %v", stage, got.Startups, goStartups)
		}
		assertSameSet(t, stage+": periodics", got.Periodics, goPeriodics)
		assertSameSet(t, stage+": shutdown hooks", got.Shutdowns, shutdowns)
	}
	check("after New", goShutdowns)

	ctx := context.Background()
	rt.RunStartup(ctx)
	// webdav-worker-init registers the auto-backup hook when it runs (as on go).
	check("after RunStartup", append(slices.Clone(goShutdowns), "webdav-auto-backup"))
}

func assertSameSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	g := slices.Clone(got)
	w := slices.Clone(want)
	slices.Sort(g)
	slices.Sort(w)
	if !slices.Equal(g, w) {
		t.Errorf("%s = %v (%d), want %v (%d)", what, got, len(got), want, len(want))
	}
}
