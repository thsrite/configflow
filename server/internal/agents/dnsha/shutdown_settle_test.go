package dnsha

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/lifecycle"
)

func TestStopSignalDuringElectionDoesNotFailAppliedPair(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	if _, err := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), ""); err != nil {
		t.Fatal(err)
	}
	lifecycle.Stop(time.Second)
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Second}); err != nil {
		t.Fatal(err)
	}
	lifecycle.Start(f.ctx)
	t.Cleanup(func() {
		lifecycle.Stop(time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})
	var applies atomic.Int32
	var once sync.Once
	arrived := make(chan struct{})
	f.client.setAfter(func(_ context.Context, r agents.Request, reply *agents.Response) error {
		if r.Method == "PUT" && strings.HasSuffix(r.URL, "/api/oxidns-ha") {
			applies.Add(1)
		}
		if r.Method == "GET" && strings.HasSuffix(r.URL, "/api/oxidns-ha") && applies.Load() >= 2 {
			body := parseObject(reply.Body)
			runtime := body.obj("runtime")
			runtime["vip_present"] = []byte("false")
			body["runtime"] = rawObject(runtime)
			reply.Body = rawObject(body)
			once.Do(func() { close(arrived) })
		}
		return nil
	})
	done := make(chan error, 1)
	ctx := lifecycle.BindRequest(f.ctx)
	go func() {
		for range 12 {
			if _, err := f.s.Advance(ctx, "primary"); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	select {
	case <-arrived:
	case err := <-done:
		t.Fatal("election wait not reached", err)
	case <-time.After(time.Minute):
		t.Fatal("election wait not reached")
	}
	// Twenty seconds of grace keeps the job cancellation deadline ahead of the
	// stop signal. Only the Clock wait's Stopping branch ends this owner.
	lifecycle.Stop(21 * time.Second)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("stop-signal cancellation was classified as a publication failure", err)
		}
	case <-time.After(time.Minute):
		t.Fatal("settle owner did not end")
	}
	a, err := f.s.get(f.ctx, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if a.OxiDNSHAPublication.Status != "applying" || a.OxiDNSHAPublication.Phase != "keepalived" || a.OxiDNSHAState.Status != "succeeded" {
		t.Fatal("stop signal wrote a late failed journal")
	}
	f.client.setAfter(nil)
	lifecycle.Start(f.ctx)
	owner := New(f.m, Options{})
	for range 8 {
		j, err := owner.Advance(lifecycle.BindRequest(f.ctx), "primary")
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == "succeeded" {
			if len(f.uploads()) != 2 {
				t.Fatal("resume uploaded cores again")
			}
			return
		}
	}
	t.Fatal("election journal did not resume")
}
