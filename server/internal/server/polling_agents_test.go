package server

import (
	"bytes"
	"context"
	"runtime"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/agents/dnsha"
	"configflow/internal/agents/ipv6"
)

type idleAgentRound struct {
	name string
	run  func(context.Context) error
}

func idleAgentFixture(t testing.TB) (*o10Fixture, []idleAgentRound) {
	t.Helper()
	f := newO10Fixture(t)
	db := f.srv.Repo
	db.SetViewGuard(nil)
	ctx := f.srv.Context()
	m, err := agents.New(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	v6 := ipv6.NewService(m, nil)
	ha := dnsha.New(m, dnsha.Options{})
	follow := dnsfollow.New(m, dnsfollow.Options{})
	return f, []idleAgentRound{
		{"IPv6", func(ctx context.Context) error {
			idle, err := v6.Tick(ctx)
			if !idle {
				t.Fatal("expected idle fixture")
			}
			return err
		}},
		{"DNS-follow", follow.Tick}, {"HA", ha.Reconcile},
	}
}

func TestIdleAgentLoopAllocations(t *testing.T) {
	f, rounds := idleAgentFixture(t)
	db, ctx := f.srv.Repo, f.srv.Context()
	before, err := db.ExportAllJSON(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range rounds {
		if err := tc.run(ctx); err != nil {
			t.Fatal(err)
		}
		var start, end runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&start)
		for range 10 {
			if err := tc.run(ctx); err != nil {
				t.Fatal(err)
			}
		}
		runtime.ReadMemStats(&end)
		t.Logf("%s %.4f MiB/round", tc.name, float64(end.TotalAlloc-start.TotalAlloc)/(10<<20))
	}
	after, err := db.ExportAllJSON(ctx, false)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("idle rounds changed repository", err)
	}
}

func BenchmarkIdleAgentLoops(b *testing.B) {
	f, rounds := idleAgentFixture(b)
	ctx := f.srv.Context()
	for _, tc := range rounds {
		b.Run(tc.name, func(b *testing.B) {
			if err := tc.run(ctx); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if err := tc.run(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
