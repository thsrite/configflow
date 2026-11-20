package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

// O3 review (standing check: a test seam must not hide a production
// parameter). Other tests replace rankingSleep with a no-op; the production
// collector must still pause Python's 0.2 s between slot requests.
func TestReviewO3ProductionCollectorPausesBetweenSlots(t *testing.T) {
	var mu sync.Mutex
	var pauses []time.Duration
	previous := rankingSleep
	rankingSleep = func(_ context.Context, d time.Duration) bool {
		mu.Lock()
		defer mu.Unlock()
		pauses = append(pauses, d)
		return true
	}
	t.Cleanup(func() { rankingSleep = previous })
	fake := newFakeAgent(generate(13, t0, 1, 100))
	useAgent(t, fake.do)
	store := NewRankingStore(t.TempDir())
	if _, err := CollectAgentHistory(ctx, nil, store, historyAgent, []string{"records"}, i64(t0+rDay+120), RankingSlotsPerRound, RankingCoreRetentionDays, 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pauses) == 0 {
		t.Fatal("production collector never paused between slot requests")
	}
	for _, d := range pauses {
		if d != 200*time.Millisecond {
			t.Fatalf("slot pause %v, want Python's 0.2 s", d)
		}
	}
}

// The production pause waits on the Runtime clock and ends at shutdown.
func TestReviewO3SlotPauseUsesRuntimeClockAndStopping(t *testing.T) {
	clock := lifecycletest.NewClock(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	runner := lifecycle.NewRuntime(lifecycle.Options{Clock: clock, Mode: lifecycle.ModeManual})
	runner.Start(context.Background())
	results := make(chan bool, 2)
	if err := runner.Spawn(context.Background(), "slot-pause", func(ctx context.Context) {
		results <- rankingPause(ctx, 200*time.Millisecond)
		results <- rankingPause(ctx, 200*time.Millisecond)
	}); err != nil {
		t.Fatal(err)
	}
	waitPending := func() {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for clock.Pending() != 1 {
			if time.Now().After(deadline) {
				t.Fatal("pause did not use the runtime clock")
			}
			time.Sleep(time.Millisecond)
		}
	}
	receive := func() bool {
		t.Helper()
		select {
		case ok := <-results:
			return ok
		case <-time.After(5 * time.Second):
			t.Fatal("pause did not end")
			return false
		}
	}
	waitPending()
	clock.Advance(199 * time.Millisecond)
	select {
	case <-results:
		t.Fatal("pause ended early")
	default:
	}
	clock.Advance(time.Millisecond)
	if !receive() {
		t.Fatal("completed pause reported stopping")
	}
	waitPending()
	stopped := make(chan struct{})
	go func() { defer close(stopped); runner.Shutdown(context.Background()) }()
	if receive() {
		t.Fatal("pause continued after shutdown started")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not finish")
	}
}
