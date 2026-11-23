package server

import (
	"context"
	"testing"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/refresh"
	"configflow/internal/repo"
)

// TestNewConnectsAgentLoopWakeSources checks, on the production assembly,
// every wake source the o17 runtime-state work connected to the Agent-domain
// loops (design/o17-runtime-state.md 2.6), now that the loops belong to the
// instances server.New registers:
//
//	loop                         config  state  Agent online  rule refresh
//	agent-rule-sync (RuleSync)     x       x         x             x
//	dns-follow (DNSFollow)         x       x         x
//	oxidns-ha-publish (DNSHA)      x       x         x
//	ipv6-verification (IPv6)       x       x
//
// The rule-refresh wake goes through refresh.Default(), the path the refresh
// round (scheduled.go) uses, so it also proves the W7 refresh instance is the
// one installed as default and carries the W6 subscriber.
func TestNewConnectsAgentLoopWakeSources(t *testing.T) {
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
	if refresh.Default() != app.Refresh {
		t.Fatal("refresh.Default() is not the App's refresh Service")
	}

	ctx := repo.WithRepository(context.Background(), r)
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents = append(s.Agents, model.Agent{
			ID: "wake-1", Name: "wake-1", Host: "192.0.2.20", Port: model.Int(8080),
			Token: "synthetic-wake-token", ServiceType: "oxidns", ProfileID: "default",
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	loops := []struct {
		name   string
		signal *lifecycle.Signal
		online bool
		rules  bool
	}{
		{"agent-rule-sync", app.RuleSync.Loop().Wake, true, true},
		{"dns-follow", app.DNSFollow.Loop().Wake, true, false},
		{"oxidns-ha-publish", app.DNSHA.Loop().Wake, true, false},
		{"ipv6-verification-scheduler", app.IPv6.Loop().Wake, false, false},
	}
	clear := func() {
		for _, l := range loops {
			l.signal.Take()
		}
	}
	expect := func(event string, want func(online, rules bool) bool) {
		t.Helper()
		for _, l := range loops {
			if got, w := l.signal.Take(), want(l.online, l.rules); w && !got {
				t.Errorf("%s did not wake %s", event, l.name)
			} else if !w && got {
				t.Errorf("%s unexpectedly woke %s", event, l.name)
			}
		}
	}

	clear()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Name = "wake-1-renamed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expect("config commit", func(bool, bool) bool { return true })

	clear()
	if err := r.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Agents[0].Version = "1.2.3"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	expect("state commit", func(bool, bool) bool { return true })

	clear()
	if _, err := app.Agents.ObserveHeartbeat(ctx, "wake-1", &model.Agent{}); err != nil {
		t.Fatal(err)
	}
	for _, l := range loops {
		if l.online && !l.signal.Take() {
			t.Errorf("Agent coming online did not wake %s", l.name)
		}
	}

	clear()
	refresh.Default().NotifyRulesRefreshed()
	expect("rule-source refresh", func(_, rules bool) bool { return rules })
}
